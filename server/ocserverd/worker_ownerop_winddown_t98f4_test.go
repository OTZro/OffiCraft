package main

// worker_ownerop_winddown_t98f4_test.go — T-98f4 rule 2:
// 「我建議所有換手都可以給他機會收尾」.
//
// relocate (改機器) / restart (重啟) / 換 model all funnel through
// respawnWorkerForOwnerOp, and all three used to kill the session on the spot:
// no refocus stamp, no SOP 預告, no grace. 換手 has had the full wind-down since
// T-ea82, and from the worker's side the four events are the same one — this
// session ends, a new one continues the task.
//
// The second half of the owner's ask is 「有東西要存才等,沒有就立刻走」, so this
// file also pins the FAST paths. Which cases are fast is a stated criterion, not
// a guess — see workerHasStateToFlush / ownerOpRevivesStoppedWorker.
//
// ─────────────────────────────────────────────────────────────────────────────
// THE DECISION TABLE (written because BOTH HIGH defects in this票 were mis-drawn
// boundaries of the SAME predicate — round 2 missed a state, round 3 drew the
// range too wide). Anyone changing workerHasStateToFlush: find your change in
// this table first. A cell you cannot state an expectation for is where the
// third defect lives.
//
// TWO UPSTREAM GATES run BEFORE the predicate is even consulted, and they
// dominate it (respawnWorkerForOwnerOp):
//
//	G1  desired_state == offline   → held_down receipt, NOTHING starts. An owner
//	                                 停止 outranks every other owner verb.
//	                                 (TestOwnerOp_StoppedWorkerStillOnlyGetsAReceipt)
//	G2  op == restart              → immediate. The session it displaces is under
//	                                 a standing kill order, so a 預告 waits for an
//	                                 answer that is never coming (deny-list, so a
//	                                 NEW verb gets the wind-down by default).
//	                                 (TestOwnerOp_RestartNeverWindsDown)
//
// Then the predicate itself, over its four inputs. `active` is Status ==
// WorkerStatusActive; `online` is hub.IsOnline; `refocus` / `stopped` are the
// row's refocus_since / stopped_since anchors:
//
//	#   active online refocus stopped │ verdict   │ why
//	──────────────────────────────────┼───────────┼──────────────────────────────
//	1     Y      Y       0       0    │ WIND DOWN │ the ordinary case: a running
//	                                  │           │ session, no handover in
//	                                  │           │ progress. Only the agent can
//	                                  │           │ know if it has unsaved work,
//	                                  │           │ so ask it (ceiling, not a
//	                                  │           │ fixed wait).
//	2     Y      Y       0      >0    │ WIND DOWN │ ⚠️ ROUND-3 DEFECT CELL. A
//	                                  │           │ stopped_since with NO epoch is
//	                                  │           │ a leftover from an ordinary
//	                                  │           │ 停止 (workerReportStopped's
//	                                  │           │ else arm) that nothing clears.
//	                                  │           │ It is not a collected
//	                                  │           │ handover. Reading it as one
//	                                  │           │ killed every later verb on
//	                                  │           │ that worker, forever.
//	                                  │           │ (_OrdinaryStopRestartStillWindsDownLater)
//	3     Y      Y      >0       0    │ WIND DOWN │ a grace window is OPEN and NO
//	                                  │           │ kill has gone out yet. Safe to
//	                                  │           │ re-stamp: the new pin/model is
//	                                  │           │ already on the row, so the
//	                                  │           │ pending collect carries it.
//	4     Y      Y      >0      >0    │ IMMEDIATE │ ⚠️ ROUND-2 DEFECT CELL. THIS
//	                                  │           │ epoch is collected: kill+start
//	                                  │           │ already went out with the OLD
//	                                  │           │ values, and the dying session
//	                                  │           │ only looks online until its
//	                                  │           │ warden reaps it. Opening a
//	                                  │           │ second wind-down here
//	                                  │           │ dispatches NOTHING and the
//	                                  │           │ in-flight boot then discards
//	                                  │           │ the epoch.
//	                                  │           │ (_VerbAfterTheCollectIsNotSwallowed)
//	5-8   Y      N     any     any    │ IMMEDIATE │ D6: nothing can hear the 預告
//	                                  │           │ and nothing exists to flush,
//	                                  │           │ so waiting burns the whole
//	                                  │           │ deadline for certain.
//	                                  │           │ (_NoLiveSessionTakesEffectImmediately)
//	9-16  N     any    any     any    │ IMMEDIATE │ assigned (activated_ts == 0)
//	                                  │           │ ⇒ the get_my_task claim never
//	                                  │           │ happened ⇒ provably no task
//	                                  │           │ state to write back.
//	                                  │           │ (_NeverClaimedItsTaskTakesEffectImmediately)
//
// stopping_since is deliberately NOT an input: a worker that has announced it is
// stopping still owes a report_stopped, so it is mid-flush, not done.
//
// Cells 5-8 and 9-16 are collapsed because the earlier conjunct short-circuits —
// that is a claim about the code, and it is why those rows need one test each
// rather than four. Cells 1-4 are each pinned individually, because those four
// are exactly where the boundary has been drawn wrong twice.
// ─────────────────────────────────────────────────────────────────────────────

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// ── the wind-down applies ────────────────────────────────────────────────────

// TestOwnerOp_RelocateWindsDownInsteadOfKilling: 改機器 on a live worker opens
// the window rather than shooting the session. The pin is persisted FIRST, so
// the collect's respawn lands on the new machine — the move still happens, it
// just no longer discards whatever the session had not written down.
func TestOwnerOp_RelocateWindsDownInsteadOfKilling(t *testing.T) {
	api := newTasksTestServer(t)
	api.noOutsource = true
	workerID := newActiveOnlineWorker(t, api)
	l, err := api.hub.Connect(workerID, "") // takeover: read the 預告 off this one
	if err != nil {
		t.Fatalf("connect worker SSE: %v", err)
	}
	t.Cleanup(func() { api.hub.Disconnect(l) })
	seedMachine(t, api, "m-elsewhere")
	connectWarden(t, api, "m-elsewhere")

	rec := postWorker(t, api, workerID, "relocate",
		map[string]any{"machine_id": "m-elsewhere"},
		api.HandleRelocateOutsourceWorkerApiOutsourceWorkersIdRelocatePost)
	if rec.Code != http.StatusOK {
		t.Fatalf("relocate: %d %s", rec.Code, rec.Body.String())
	}
	w, _ := api.dal.GetOutsourceWorker(workerID)
	if w.DesiredMachineID != "m-elsewhere" {
		t.Fatalf("relocate must persist the pin immediately, got %q", w.DesiredMachineID)
	}
	if w.RefocusSince <= 0 {
		t.Fatal("relocate on a live worker must open a wind-down (refocus_since stamped)")
	}
	if got := len(api.hub.DrainWardenCommands(ServerSelfHost)); got != 0 {
		t.Fatalf("the wind-down must not kill the old session yet, got %d frames", got)
	}
	if got := len(api.hub.DrainWardenCommands("m-elsewhere")); got != 0 {
		t.Fatalf("nothing may start on the new machine while the old one winds down, got %d", got)
	}
	nudged := false
	for frame := l.pop(); frame != nil; frame = l.pop() {
		if strings.Contains(string(frame), `"topic":"member"`) &&
			strings.Contains(string(frame), workerID) {
			nudged = true
		}
	}
	if !nudged {
		t.Fatal("relocate must fan the SOP 預告 at the worker's own session")
	}

	// It finishes and says so → the move completes NOW, onto the pinned machine.
	rec = httptest.NewRecorder()
	api.HandleReportStoppedApiSelfStoppedPost(rec,
		taskReq(t, "POST", "/api/self/stopped", nil, workerID, "agent"))
	if rec.Code != http.StatusOK {
		t.Fatalf("report_stopped: %d %s", rec.Code, rec.Body.String())
	}
	if got := len(api.hub.DrainWardenCommands(ServerSelfHost)); got != 1 {
		t.Fatalf("the collect must kill the OLD session (1 stop), got %d frames", got)
	}
	starts := api.hub.DrainWardenCommands("m-elsewhere")
	if len(starts) != 1 {
		t.Fatalf("the collect must start on the NEW machine, got %d frames", len(starts))
	}
	if rpc, args := decodeWardenFrame(t, starts[0].Frame); rpc != reconcileCmdStart ||
		args["member_id"] != workerID {
		t.Fatalf("frame = %s %v, want a start for %s", rpc, args, workerID)
	}
}

// TestOwnerOp_WindDownEndsOnTheDeadlineToo: the wind-down must not be able to
// hang forever on a worker that never answers. The grace deadline
// (StoppingTimeoutSecs) is the ceiling, driven by the tick's in-flight arm.
func TestOwnerOp_WindDownEndsOnTheDeadlineToo(t *testing.T) {
	api := newTasksTestServer(t)
	api.noOutsource = true
	workerID := newActiveOnlineWorker(t, api)

	rec := postWorker(t, api, workerID, "model",
		map[string]any{"model": "claude-opus-4-8"},
		api.HandleSetOutsourceWorkerModelApiOutsourceWorkersIdModelPost)
	if rec.Code != http.StatusOK {
		t.Fatalf("model: %d %s", rec.Code, rec.Body.String())
	}
	api.hub.DrainWardenCommands(ServerSelfHost)
	w, _ := api.dal.GetOutsourceWorker(workerID)

	// Just short of the deadline: still waiting, nothing dispatched.
	api.outsourceMu.Lock()
	api.autoHandoverWorker(*w, w.RefocusSince+StoppingTimeoutSecs-1)
	api.outsourceMu.Unlock()
	if got := len(api.hub.DrainWardenCommands(ServerSelfHost)); got != 0 {
		t.Fatalf("inside the grace window nothing may be dispatched, got %d frames", got)
	}
	// Past it: collected regardless.
	api.outsourceMu.Lock()
	api.autoHandoverWorker(*w, w.RefocusSince+StoppingTimeoutSecs+1)
	api.outsourceMu.Unlock()
	if got := len(api.hub.DrainWardenCommands(ServerSelfHost)); got != 2 {
		t.Fatalf("the deadline must collect (stop+start), got %d frames", got)
	}
}

// ── the fast paths (有東西要存才等,沒有就立刻走) ────────────────────────────

// TestOwnerOp_NoLiveSessionTakesEffectImmediately: nothing can hear the 預告 and
// nothing exists to flush, so waiting would burn the whole deadline for certain.
func TestOwnerOp_NoLiveSessionTakesEffectImmediately(t *testing.T) {
	api := newTasksTestServer(t)
	api.noOutsource = true
	workerID := newActiveWorker(t, api, false) // active, session already gone
	seedMachine(t, api, "m-elsewhere")
	connectWarden(t, api, "m-elsewhere")
	api.hub.DrainWardenCommands(ServerSelfHost)

	rec := postWorker(t, api, workerID, "relocate",
		map[string]any{"machine_id": "m-elsewhere"},
		api.HandleRelocateOutsourceWorkerApiOutsourceWorkersIdRelocatePost)
	if rec.Code != http.StatusOK {
		t.Fatalf("relocate: %d %s", rec.Code, rec.Body.String())
	}
	if w, _ := api.dal.GetOutsourceWorker(workerID); w.RefocusSince != 0 {
		t.Fatal("a worker with no live session must not be made to wait out a wind-down")
	}
	if got := len(api.hub.DrainWardenCommands("m-elsewhere")); got != 1 {
		t.Fatalf("the move must take effect immediately, got %d frames on the new machine", got)
	}
}

// TestOwnerOp_NeverClaimedItsTaskTakesEffectImmediately: assigned (activated_ts
// == 0) means the worker has provably never been handed its task content — the
// claim IS the assigned→active flip — so there is no task state to write back.
func TestOwnerOp_NeverClaimedItsTaskTakesEffectImmediately(t *testing.T) {
	api := newTasksTestServer(t)
	api.noOutsource = true
	seedLiveWorkerEnv(t, api)
	workerID := assignOneWorker(t, api) // stays 'assigned'
	if _, err := api.hub.Connect(workerID, ""); err != nil {
		t.Fatalf("connect worker SSE: %v", err) // ONLINE, but never claimed
	}
	api.workerSpawnTarget[workerID] = ServerSelfHost
	api.hub.DrainWardenCommands(ServerSelfHost)

	// Pin it somewhere real so the relocate has a destination to dispatch to.
	seedMachine(t, api, "m-elsewhere")
	connectWarden(t, api, "m-elsewhere")
	rec := postWorker(t, api, workerID, "relocate",
		map[string]any{"machine_id": "m-elsewhere"},
		api.HandleRelocateOutsourceWorkerApiOutsourceWorkersIdRelocatePost)
	if rec.Code != http.StatusOK {
		t.Fatalf("relocate: %d %s", rec.Code, rec.Body.String())
	}
	if w, _ := api.dal.GetOutsourceWorker(workerID); w.RefocusSince != 0 {
		t.Fatal("a worker that never claimed its task has nothing to flush — no wind-down")
	}
	if got := len(api.hub.DrainWardenCommands("m-elsewhere")); got != 1 {
		t.Fatalf("the move must take effect immediately, got %d frames", got)
	}
}

// TestOwnerOp_RestartNeverWindsDown: 重啟 acts on a worker the owner ALREADY
// stopped — 停止 dispatched the kill, so the session it would displace is under
// a standing kill order. Fanning an SOP 預告 at it and then waiting out the
// deadline for an answer that is never coming is exactly the pointless wait the
// rule exists to prevent. This is the ONE deny-listed verb; a new verb added
// later gets the wind-down by default.
func TestOwnerOp_RestartNeverWindsDown(t *testing.T) {
	api := newTasksTestServer(t)
	api.noOutsource = true
	workerID := newActiveOnlineWorker(t, api)

	postWorker(t, api, workerID, "stop", nil,
		api.HandleStopOutsourceWorkerApiOutsourceWorkersIdStopPost)
	api.hub.DrainWardenCommands(ServerSelfHost)
	// The fixture's SSE listener is still up (the warden has not reaped the
	// session yet), so this is precisely the race where a state-only criterion
	// would mistake a dying session for a working one.
	if !api.hub.IsOnline(workerID) {
		t.Fatal("fixture: the session must still look online for this test to bite")
	}

	rec := postWorker(t, api, workerID, "restart", nil,
		api.HandleRestartOutsourceWorkerApiOutsourceWorkersIdRestartPost)
	if rec.Code != http.StatusOK {
		t.Fatalf("restart: %d %s", rec.Code, rec.Body.String())
	}
	if w, _ := api.dal.GetOutsourceWorker(workerID); w.RefocusSince != 0 {
		t.Fatal("restart must not open a wind-down on an already-stopped worker")
	}
	sawStart := false
	for _, f := range api.hub.DrainWardenCommands(ServerSelfHost) {
		if rpc, _ := decodeWardenFrame(t, f.Frame); rpc == reconcileCmdStart {
			sawStart = true
		}
	}
	if !sawStart {
		t.Fatal("restart must re-dispatch immediately")
	}
}

// TestOwnerOp_StoppedWorkerStillOnlyGetsAReceipt: the pre-existing
// desired_state=offline branch point outranks everything, wind-down included —
// an owner 停止 must never be overturned by another owner verb.
func TestOwnerOp_StoppedWorkerStillOnlyGetsAReceipt(t *testing.T) {
	api := newTasksTestServer(t)
	api.noOutsource = true
	workerID := newActiveOnlineWorker(t, api)
	w, _ := api.dal.GetOutsourceWorker(workerID)
	w.DesiredState = DesiredStateOffline
	if err := api.dal.PutOutsourceWorker(*w); err != nil {
		t.Fatalf("hold down: %v", err)
	}
	api.hub.DrainWardenCommands(ServerSelfHost)

	rec := postWorker(t, api, workerID, "model",
		map[string]any{"model": "claude-opus-4-8"},
		api.HandleSetOutsourceWorkerModelApiOutsourceWorkersIdModelPost)
	if rec.Code != http.StatusOK {
		t.Fatalf("model: %d %s", rec.Code, rec.Body.String())
	}
	after, _ := api.dal.GetOutsourceWorker(workerID)
	if after.RefocusSince != 0 {
		t.Fatal("a stopped worker must not be woken into a wind-down")
	}
	if !strings.HasPrefix(after.LastOpReason, spawnReasonHeldDown) {
		t.Fatalf("receipt = %q, want %s", after.LastOpReason, spawnReasonHeldDown)
	}
	if got := len(api.hub.DrainWardenCommands(ServerSelfHost)); got != 0 {
		t.Fatalf("a stopped worker must not be started, got %d frames", got)
	}
}

// TestOwnerOp_VerbAfterTheCollectIsNotSwallowed (T-98f4 review round 2, HIGH):
// the window between 「收口已latch」 and 「新 session 還沒連上」 must not eat an
// owner verb.
//
// The shape: the collect has ALREADY dispatched kill + start for this epoch
// (carrying the OLD pin), but the dying session is still hub.IsOnline because
// its warden has not reaped it yet. Without the StoppedSince arm in
// workerHasStateToFlush, the second verb takes the wind-down branch: it zeroes
// the collected latch, re-stamps refocus_since — and dispatches NOTHING. The
// in-flight OLD-config start then boots, boot_ts beats the new refocus_since,
// autoHandoverWorker's loop-break clears the epoch, and the owner's second
// 改機器 reaches no session at all: the cockpit shows m-third while the worker
// lives on m-elsewhere forever (the FSM rescue is gated on !IsOnline, so
// nothing heals it). That is the owner's original complaint coming back in
// through a new door, so the collected window takes the IMMEDIATE path.
func TestOwnerOp_VerbAfterTheCollectIsNotSwallowed(t *testing.T) {
	api := newTasksTestServer(t)
	api.noOutsource = true
	workerID := newActiveOnlineWorker(t, api)
	seedMachine(t, api, "m-elsewhere")
	connectWarden(t, api, "m-elsewhere")
	seedMachine(t, api, "m-third")
	connectWarden(t, api, "m-third")

	// 1) 改機器 → wind-down opens (no kill yet).
	if rec := postWorker(t, api, workerID, "relocate",
		map[string]any{"machine_id": "m-elsewhere"},
		api.HandleRelocateOutsourceWorkerApiOutsourceWorkersIdRelocatePost); rec.Code != http.StatusOK {
		t.Fatalf("relocate #1: %d %s", rec.Code, rec.Body.String())
	}
	// 2) the worker answers → the collect latches stopped_since and dispatches
	//    kill + start with the pin as it stood THEN (m-elsewhere).
	rec := httptest.NewRecorder()
	api.HandleReportStoppedApiSelfStoppedPost(rec,
		taskReq(t, "POST", "/api/self/stopped", nil, workerID, "agent"))
	if rec.Code != http.StatusOK {
		t.Fatalf("report_stopped: %d %s", rec.Code, rec.Body.String())
	}
	if w, _ := api.dal.GetOutsourceWorker(workerID); w.StoppedSince <= 0 {
		t.Fatal("fixture: the collect must have latched stopped_since")
	}
	api.hub.DrainWardenCommands(ServerSelfHost)
	api.hub.DrainWardenCommands("m-elsewhere")
	// The in-flight session has not booted yet, and the dying one still looks
	// online — this is exactly the window the bug lived in.
	if !api.hub.IsOnline(workerID) {
		t.Fatal("fixture: the old session must still look online for this test to bite")
	}

	// 3) the owner changes his mind mid-flight. It must GO OUT, not be swallowed.
	if rec := postWorker(t, api, workerID, "relocate",
		map[string]any{"machine_id": "m-third"},
		api.HandleRelocateOutsourceWorkerApiOutsourceWorkersIdRelocatePost); rec.Code != http.StatusOK {
		t.Fatalf("relocate #2: %d %s", rec.Code, rec.Body.String())
	}
	if w, _ := api.dal.GetOutsourceWorker(workerID); w.DesiredMachineID != "m-third" {
		t.Fatalf("pin = %q, want m-third", w.DesiredMachineID)
	}
	starts := api.hub.DrainWardenCommands("m-third")
	if len(starts) != 1 {
		t.Fatalf("a verb landing after the collect must dispatch NOW, got %d frames on "+
			"the new machine (a second wind-down would dispatch nothing and the "+
			"in-flight old-config start would then discard the epoch)", len(starts))
	}
	if rpc, args := decodeWardenFrame(t, starts[0].Frame); rpc != reconcileCmdStart ||
		args["member_id"] != workerID {
		t.Fatalf("frame = %s %v, want a start for %s", rpc, args, workerID)
	}
	// The collected latch must survive: zeroing it is what re-opened the window.
	if w, _ := api.dal.GetOutsourceWorker(workerID); w.StoppedSince <= 0 {
		t.Fatal("the already-collected latch must not be zeroed by a later owner verb")
	}
}

// TestOwnerOp_OrdinaryStopRestartStillWindsDownLater (T-98f4 review round 3,
// HIGH — the hole the round-2 fix opened): the 「已收攏」 fast path must be
// EPOCH-SCOPED, not a global read of stopped_since.
//
// stopped_since is latched in TWO places, and only one of them is a handover:
// collectWorkerHandover latches it as the 收口 of a refocus epoch, but
// workerReportStopped's else arm ALSO latches it for a report that arrives
// outside any handover — an ordinary 停止 where the worker politely says it has
// finished. Nothing clears that one: clearWorkerRefocus is only reachable while
// refocus_since > 0, and the restart handler writes desired_state and nothing
// else. So the latch outlives the whole stop→restart cycle.
//
// Read globally, that latch says "already collected" forever: every later 改機器
// / 換 model on that worker is shot on the spot — no 預告, no grace, whatever
// the session had not written down is gone — and it repeats for the rest of the
// worker's life. That is the exact failure rule 2 exists to abolish, re-entering
// through a broader and far more ordinary door than the one round 2 closed.
func TestOwnerOp_OrdinaryStopRestartStillWindsDownLater(t *testing.T) {
	api := newTasksTestServer(t)
	api.noOutsource = true
	workerID := newActiveOnlineWorker(t, api)

	// An ORDINARY 停止 → the worker reports it finished → 重啟. No handover
	// anywhere in here: refocus_since is never stamped.
	postWorker(t, api, workerID, "stop", nil,
		api.HandleStopOutsourceWorkerApiOutsourceWorkersIdStopPost)
	rec := httptest.NewRecorder()
	api.HandleReportStoppedApiSelfStoppedPost(rec,
		taskReq(t, "POST", "/api/self/stopped", nil, workerID, "agent"))
	if rec.Code != http.StatusOK {
		t.Fatalf("report_stopped: %d %s", rec.Code, rec.Body.String())
	}
	w, _ := api.dal.GetOutsourceWorker(workerID)
	if w.RefocusSince != 0 || w.StoppedSince <= 0 {
		t.Fatalf("fixture: this test needs a NON-handover latch "+
			"(refocus=%v stopped=%v)", w.RefocusSince, w.StoppedSince)
	}
	if rec := postWorker(t, api, workerID, "restart", nil,
		api.HandleRestartOutsourceWorkerApiOutsourceWorkersIdRestartPost); rec.Code != http.StatusOK {
		t.Fatalf("restart: %d %s", rec.Code, rec.Body.String())
	}
	api.hub.DrainWardenCommands(ServerSelfHost)
	if w, _ := api.dal.GetOutsourceWorker(workerID); w.StoppedSince <= 0 {
		t.Fatal("fixture: the stale latch must survive the restart for this test to bite")
	}
	if !api.hub.IsOnline(workerID) {
		t.Fatal("fixture: the worker must be online for the wind-down to be owed")
	}

	// Now the owner changes the model. This worker is running and has never been
	// through a handover, so it is owed the FULL wind-down.
	if rec := postWorker(t, api, workerID, "model",
		map[string]any{"model": "claude-opus-4-8"},
		api.HandleSetOutsourceWorkerModelApiOutsourceWorkersIdModelPost); rec.Code != http.StatusOK {
		t.Fatalf("model: %d %s", rec.Code, rec.Body.String())
	}
	after, _ := api.dal.GetOutsourceWorker(workerID)
	if after.RefocusSince <= 0 {
		t.Fatal("a worker whose only stopped_since came from an ORDINARY stop must " +
			"still get its 收尾 — a stale latch is not a collected handover")
	}
	if got := len(api.hub.DrainWardenCommands(ServerSelfHost)); got != 0 {
		t.Fatalf("the wind-down must not kill the session yet, got %d frames", got)
	}
	// The fresh epoch also HEALS the stale latch, so the worker is not stuck
	// re-deciding this every time.
	if after.StoppedSince != 0 {
		t.Fatalf("a new epoch must clear the stale latch, got stopped_since=%v",
			after.StoppedSince)
	}
}

// TestOwnerOp_SecondVerbDuringAnOpenWindowReStampsAndStillCollects pins cell 3
// of the decision table: refocus > 0 ∧ stopped == 0 — a grace window that is
// OPEN and not yet collected.
//
// It is the neighbour of the round-2 defect cell and the reason that fix had to
// be an AND rather than "refocus > 0 ⇒ immediate": nothing has been dispatched
// yet here, so re-stamping is harmless and the owner still gets his 收尾. What
// makes it safe is that the new pin is persisted BEFORE the window is (re)opened,
// so whichever collect eventually fires carries the LATEST value — checked below
// by letting the worker answer and watching where it actually lands.
func TestOwnerOp_SecondVerbDuringAnOpenWindowReStampsAndStillCollects(t *testing.T) {
	api := newTasksTestServer(t)
	api.noOutsource = true
	workerID := newActiveOnlineWorker(t, api)
	seedMachine(t, api, "m-elsewhere")
	connectWarden(t, api, "m-elsewhere")
	seedMachine(t, api, "m-third")
	connectWarden(t, api, "m-third")

	if rec := postWorker(t, api, workerID, "relocate",
		map[string]any{"machine_id": "m-elsewhere"},
		api.HandleRelocateOutsourceWorkerApiOutsourceWorkersIdRelocatePost); rec.Code != http.StatusOK {
		t.Fatalf("relocate #1: %d %s", rec.Code, rec.Body.String())
	}
	first, _ := api.dal.GetOutsourceWorker(workerID)
	if first.RefocusSince <= 0 || first.StoppedSince != 0 {
		t.Fatalf("fixture: cell 3 needs an OPEN, uncollected window "+
			"(refocus=%v stopped=%v)", first.RefocusSince, first.StoppedSince)
	}
	api.hub.DrainWardenCommands(ServerSelfHost)

	// The owner changes his mind while the window is still open. Still a
	// wind-down — and still no kill.
	if rec := postWorker(t, api, workerID, "relocate",
		map[string]any{"machine_id": "m-third"},
		api.HandleRelocateOutsourceWorkerApiOutsourceWorkersIdRelocatePost); rec.Code != http.StatusOK {
		t.Fatalf("relocate #2: %d %s", rec.Code, rec.Body.String())
	}
	second, _ := api.dal.GetOutsourceWorker(workerID)
	if second.RefocusSince <= 0 {
		t.Fatal("a verb landing inside an OPEN window must keep the 收尾 open")
	}
	if second.DesiredMachineID != "m-third" {
		t.Fatalf("pin = %q, want m-third", second.DesiredMachineID)
	}
	for _, target := range []string{ServerSelfHost, "m-elsewhere", "m-third"} {
		if got := len(api.hub.DrainWardenCommands(target)); got != 0 {
			t.Fatalf("nothing may be dispatched while the window is open "+
				"(%s got %d frames)", target, got)
		}
	}

	// It answers → the collect fires ONCE and carries the LATEST pin.
	rec := httptest.NewRecorder()
	api.HandleReportStoppedApiSelfStoppedPost(rec,
		taskReq(t, "POST", "/api/self/stopped", nil, workerID, "agent"))
	if rec.Code != http.StatusOK {
		t.Fatalf("report_stopped: %d %s", rec.Code, rec.Body.String())
	}
	if got := len(api.hub.DrainWardenCommands("m-elsewhere")); got != 0 {
		t.Fatalf("the superseded destination must receive nothing, got %d frames", got)
	}
	starts := api.hub.DrainWardenCommands("m-third")
	if len(starts) != 1 {
		t.Fatalf("the collect must start on the LATEST pin, got %d frames", len(starts))
	}
	if rpc, args := decodeWardenFrame(t, starts[0].Frame); rpc != reconcileCmdStart ||
		args["member_id"] != workerID {
		t.Fatalf("frame = %s %v, want a start for %s", rpc, args, workerID)
	}
}
