package main

// worker_spawn_session_anchor_t4235_test.go — the DURABLE session anchor on the
// two worker paths that begin a session WITHOUT going through respawnWorkerNow.
//
// 🔴 WHY THIS FILE EXISTS SEPARATELY FROM api_infra_session_anchor_t4235_test.go:
// that file proves the anchor survives a re-exec and that clearSessionBootTS
// zeroes both stores. It says nothing about WHO CALLS IT. Once the anchor became
// durable, a session boundary that forgets to clear it no longer heals on the
// next re-exec — the dead session's anchor sits in the row forever and the fresh
// session adopts it, so the respawn-storm floor (restart_self's minimum liveness)
// waves the newborn through. The two paths pinned here are exactly the ones that
// used to forget:
//
//	B-1  the FSM RESCUE arm (reconcileWorkerLiveness's START) — the ONLY way back
//	     for a worker whose session died on its own (crash, machine reboot, a
//	     report_stopped outside a handover). It dispatches a real START and the
//	     warden opens a brand-new session; there is no kill on that path, so
//	     respawnWorkerNow — the sole clear before this fix — is never reached.
//	B-2  the owner-op FALL-THROUGH (respawnWorkerForOwnerOpNow) — respawnWorkerNow
//	     returns false BEFORE its clear when an ACTIVE worker has no kill target,
//	     and the caller dispatches the start anyway.
//
// Both tests are BEHAVIOURAL, not "was the helper called": they seed a
// predecessor anchor that is hours old, drive the real path, let the replacement
// session connect through the real onFirstConnect, and then ask the consumer the
// field reports hit — restart_self — which must answer 429. Without the fix it
// answers 200, because the newborn reads its predecessor's age.

import (
	"testing"
)

// anchoredWorkerFixture seeds a live task + manual + an ACTIVE worker placed on
// the server-self warden, with a durable session anchor `age` seconds old — the
// predecessor session. Returns the worker row.
func anchoredWorkerFixture(t *testing.T, s *apiServer, id string, age float64) OutsourceWorker {
	t.Helper()
	putTaskFixture(t, s, Task{
		ID: "t-" + id, TypeKey: "review-pr", Title: "x",
		Status: TaskStatusInProgress, Priority: TaskPriorityMid,
		ExecutorKind: TaskExecutorOutsource, ExecutorID: id,
	})
	if err := s.dal.PutTaskManual(TaskManual{TypeKey: "review-pr", Purpose: "p",
		Fields: "[]", Assignee: `{"kind":"outsource","model":"opus"}`}); err != nil {
		t.Fatalf("put manual: %v", err)
	}
	now := nowSecs()
	w := putWorkerFixture(t, s, OutsourceWorker{
		ID: id, Codename: "O-4235", Model: "opus", Effort: "high",
		TaskID: "t-" + id, Status: WorkerStatusActive,
		DesiredState:     DesiredStateOnline,
		DesiredMachineID: ServerSelfHost, // explicit placement (owner ruling 2026-07-25)
		CreatedTS:        now - age, ActivatedTS: now - age,
		SessionBootTS: now - age, // the PREDECESSOR session's anchor
	})
	// Anti-tautology: the predecessor anchor must really be on the durable row,
	// otherwise every assertion below could pass on a server that persists
	// nothing at all.
	if got := mustMember(t, s.dal, id).SessionBootTS; got != now-age {
		t.Fatalf("precondition: the worker projection must carry the anchor; want %v, got %v",
			now-age, got)
	}
	return w
}

// assertRebornSessionIsRefused drives the REAL consumer: the replacement session
// connects (real onFirstConnect) and asks restart_self. Seconds old ⇒ 429.
func assertRebornSessionIsRefused(t *testing.T, s *apiServer, id string) {
	t.Helper()
	defer online(t, s, id)()
	s.onFirstConnect(id)

	if got, ok := gaugeBootTS(s.gauge.Get(id)); !ok || nowSecs()-got > minSelfRestartSecs {
		t.Fatalf("the replacement session must anchor at ITS OWN boot, not its "+
			"predecessor's; got %v ok=%t", got, ok)
	}
	rec := doRestartSelf(s, id, "")
	if rec.Code != 429 {
		t.Fatalf("the replacement session is seconds old and must be refused "+
			"(respawn-storm guard): want 429, got %d %s", rec.Code, rec.Body.String())
	}
}

// ── B-1: the FSM rescue arm ──────────────────────────────────────────────────

// TestFSMRescueStartClearsTheDeadSessionsAnchor — the worker's session died on
// its own (offline, desired online, no handover in flight). The scheduler hands
// it to reconcileWorkerLiveness every tick and the FSM's START is its ONLY way
// back. That START opens a brand-new session, so it must not leave the dead
// one's anchor behind.
func TestFSMRescueStartClearsTheDeadSessionsAnchor(t *testing.T) {
	s := newWorkerTestServer(t)
	connectWarden(t, s, ServerSelfHost)
	w := anchoredWorkerFixture(t, s, "ow-b1", minSelfRestartSecs+7200)

	// The session is GONE (never connected on this hub) — the rescue shape.
	s.outsourceMu.Lock()
	s.reconcileWorkerLiveness(w, nowSecs())
	s.outsourceMu.Unlock()

	// Anti-tautology: the rescue must really have dispatched a START. Without
	// this, "the anchor is 0" could just mean nothing happened at all.
	frames := s.hub.DrainWardenCommands(ServerSelfHost)
	if len(frames) != 1 {
		t.Fatalf("the FSM rescue must dispatch exactly 1 start, got %d", len(frames))
	}
	if rpc, args := decodeWardenFrame(t, frames[0].Frame); rpc != reconcileCmdStart ||
		args["member_id"] != "ow-b1" {
		t.Fatalf("rescue frame = %s %v, want start ow-b1", rpc, args)
	}

	if got := mustMember(t, s.dal, "ow-b1").SessionBootTS; got != 0 {
		t.Fatalf("a rescue START begins a NEW session, so the dead session's "+
			"durable anchor must be dropped; got %v — the newborn inherits its "+
			"predecessor's age and the respawn-storm floor never fires again", got)
	}
	assertRebornSessionIsRefused(t, s, "ow-b1")
}

// ── B-2: the owner-op fall-through ───────────────────────────────────────────

// TestOwnerOpFallThroughClearsThePreviousSessionsAnchor — 改機器 / 重啟 / 換 model
// landing on an ACTIVE worker whose kill target cannot be resolved (spawn memory
// empty after a re-exec AND no live SSE claim). respawnWorkerNow defers and
// returns false BEFORE its own clear; the caller then dispatches the start
// anyway, so the anchor has to be dropped on that dispatch.
func TestOwnerOpFallThroughClearsThePreviousSessionsAnchor(t *testing.T) {
	s := newWorkerTestServer(t)
	connectWarden(t, s, ServerSelfHost)
	w := anchoredWorkerFixture(t, s, "ow-b2", minSelfRestartSecs+7200)

	// The fall-through shape: ACTIVE, no spawn memory, no live SSE claim.
	s.outsourceMu.Lock()
	if got := s.resolveWorkerKillTarget("ow-b2"); got != "" {
		s.outsourceMu.Unlock()
		t.Fatalf("precondition: this fixture must have NO kill target, got %q", got)
	}
	s.respawnWorkerForOwnerOpNow(w, ownerOpRelocate)
	s.outsourceMu.Unlock()

	// Anti-tautology: the fall-through must really have attempted the start
	// (that is the whole point of the deferral arm), and it must be a START —
	// a kill would mean we are testing a different path.
	frames := s.hub.DrainWardenCommands(ServerSelfHost)
	if len(frames) != 1 {
		t.Fatalf("the owner-op fall-through must still attempt 1 start, got %d", len(frames))
	}
	if rpc, args := decodeWardenFrame(t, frames[0].Frame); rpc != reconcileCmdStart ||
		args["member_id"] != "ow-b2" {
		t.Fatalf("fall-through frame = %s %v, want start ow-b2", rpc, args)
	}

	if got := mustMember(t, s.dal, "ow-b2").SessionBootTS; got != 0 {
		t.Fatalf("the owner verb's start begins a NEW session, so the previous "+
			"session's durable anchor must be dropped; got %v", got)
	}
	assertRebornSessionIsRefused(t, s, "ow-b2")
}

// ── the reverse direction: the clear must not out-reach its cause ────────────

// TestARefusedStartLeavesTheLivingSessionsAnchorAlone is the positive control
// for both tests above. Dropping the anchor is only correct when a start really
// LANDED; a refused dispatch (no online warden here) leaves the session that is
// still running exactly as it was — clearing there would fail OPEN and hand a
// living session a free restart_self.
func TestARefusedStartLeavesTheLivingSessionsAnchorAlone(t *testing.T) {
	s := newWorkerTestServer(t) // deliberately NO warden connected
	age := minSelfRestartSecs + 7200
	w := anchoredWorkerFixture(t, s, "ow-b3", age)
	want := mustMember(t, s.dal, "ow-b3").SessionBootTS

	s.outsourceMu.Lock()
	dispatched := s.notifyWorkerSpawn(w, nowSecs())
	s.outsourceMu.Unlock()
	if dispatched {
		t.Fatal("precondition: with no online warden the dispatch must fail closed")
	}

	if got := mustMember(t, s.dal, "ow-b3").SessionBootTS; got != want {
		t.Fatalf("a REFUSED start begins no session, so the running session's "+
			"anchor must be untouched: want %v, got %v", want, got)
	}
}
