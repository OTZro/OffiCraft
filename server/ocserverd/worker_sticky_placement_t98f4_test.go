package main

// worker_sticky_placement_t98f4_test.go — T-98f4 rule 1: 「換手應該在原地,除非
// 我有特別指定要換去別處」.
//
// The suite is deliberately split into TWO halves that fail for opposite
// reasons, because this feature has two ways to be wrong and only one of them
// is obvious:
//
//	(A) 保護沒生效 — a rebirth wanders off the machine it was running on. This
//	    is the reported bug.
//	(B) 保護誤擋 — stickiness turns into a cage: the owner presses 改機器 and
//	    nothing moves, or the machine goes offline and the worker is stranded on
//	    a host that will never come back. Owner called this arm out by name,
//	    and it is the one a naive "just remember the last machine" patch breaks.
//
// Every assertion below states which half it belongs to.

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"
)

// stickyFixture seeds one assigned worker on a live outsource task whose type
// manual pins `manualMachine`, and registers m-other / m-third as extra
// machines. Returns the worker row (LastMachineID still "" — never landed).
func stickyFixture(t *testing.T, s *apiServer, workerID, manualMachine string) OutsourceWorker {
	t.Helper()
	putWardenFixture(t, s, "m-other")
	putWardenFixture(t, s, "m-third")
	task := putTaskFixture(t, s, Task{
		ID: "t-0000000000" + workerID[len(workerID)-2:], TypeKey: "sticky-type",
		Title: "sticky", Status: TaskStatusNotStarted, Priority: TaskPriorityMid,
		ExecutorKind: TaskExecutorOutsource, ExecutorID: workerID,
	})
	if err := s.dal.PutTaskManual(TaskManual{TypeKey: "sticky-type", Purpose: "p",
		Fields:   "[]",
		Assignee: `{"kind":"outsource","model":"opus","machine":"` + manualMachine + `"}`,
	}); err != nil {
		t.Fatalf("put manual: %v", err)
	}
	return putWorkerFixture(t, s, OutsourceWorker{
		ID: workerID, Codename: "O-" + workerID, Model: "opus", Effort: "high",
		TaskID: task.ID, Status: WorkerStatusAssigned,
	})
}

// spawnTarget runs one placement decision and reports which machine (if any)
// actually received the start frame — the only observation that answers "where
// did it go", read off the FIFO rather than off any internal map.
func spawnTarget(t *testing.T, s *apiServer, w OutsourceWorker, machines ...string) string {
	t.Helper()
	s.outsourceMu.Lock()
	delete(s.workerSpawnAt, w.ID)
	s.notifyWorkerSpawn(w, nowSecs())
	s.outsourceMu.Unlock()
	landed := ""
	for _, m := range machines {
		if n := len(s.hub.DrainWardenCommands(m)); n > 0 {
			if landed != "" {
				t.Fatalf("worker %s was dispatched to BOTH %s and %s", w.ID, landed, m)
			}
			landed = m
		}
	}
	return landed
}

// landOn records a GENUINE landing for a fixture: the server dispatched this
// worker to `machine` AND a session then connected from there. Both halves are
// required since the review-round-2 gate — stampLandedMachine now refuses a
// claim the server cannot corroborate (connectionIsTheGenuineArticle), so a
// fixture that only calls the stamp records nothing at all.
func landOn(t *testing.T, s *apiServer, workerID, machine string) {
	t.Helper()
	s.outsourceMu.Lock()
	s.workerSpawnTarget[workerID] = machine
	s.outsourceMu.Unlock()
	s.stampLandedMachine(workerID, machine)
	if got := readWorker(t, s, workerID).LastMachineID; got != machine {
		t.Fatalf("fixture: landing on %s did not stick (got %q)", machine, got)
	}
}

// ── (A) the protection itself ────────────────────────────────────────────────

// TestSticky_RebirthStaysPutWhenTheManualMoved is the reported bug, verbatim:
// the worker was born on m-other because the 手冊 said so, it actually ran
// there, and then the 手冊 was edited. Its NEXT rebirth must not follow the
// 手冊 — 「手冊上的機器只對 outsource worker 第一次起來生效」.
func TestSticky_RebirthStaysPutWhenTheManualMoved(t *testing.T) {
	s := newWorkerTestServer(t)
	w := stickyFixture(t, s, "ow-s01", "m-other")
	connectWarden(t, s, ServerSelfHost)
	connectWarden(t, s, "m-other")
	connectWarden(t, s, "m-third")

	// FIRST boot: nothing has landed yet, so the manual decides (this is also
	// the (B)-side guard against over-applying stickiness — see the dedicated
	// first-boot test below).
	if got := spawnTarget(t, s, w, ServerSelfHost, "m-other", "m-third"); got != "m-other" {
		t.Fatalf("first boot must follow the 手冊: got %q, want m-other", got)
	}
	// It came up there — the session's own machine claim is the record.
	s.stampLandedMachine(w.ID, "m-other")

	// The owner now edits the 手冊 to point somewhere else. That is a statement
	// about where the NEXT worker is born, not about where this one lives.
	if err := s.dal.PutTaskManual(TaskManual{TypeKey: "sticky-type", Purpose: "p",
		Fields:   "[]",
		Assignee: `{"kind":"outsource","model":"opus","machine":"m-third"}`,
	}); err != nil {
		t.Fatalf("re-put manual: %v", err)
	}
	if got := spawnTarget(t, s, readWorker(t, s, w.ID), ServerSelfHost, "m-other", "m-third"); got != "m-other" {
		t.Fatalf("rebirth after a 手冊 edit: got %q, want m-other (換手應該在原地)", got)
	}
}

// TestSticky_RebirthStaysPutWhenTheTaskRowMoved is the same rule through the
// OTHER durable configured arm (the task row / 發包 target), so a fix that only
// beats the manual does not pass.
func TestSticky_RebirthStaysPutWhenTheTaskRowMoved(t *testing.T) {
	s := newWorkerTestServer(t)
	putWardenFixture(t, s, "m-other")
	putWardenFixture(t, s, "m-third")
	connectWarden(t, s, ServerSelfHost)
	connectWarden(t, s, "m-other")
	connectWarden(t, s, "m-third")
	task := putTaskFixture(t, s, Task{
		ID: "t-0000000098a1", Title: "dispatched", Status: TaskStatusNotStarted,
		Priority: TaskPriorityMid, ExecutorKind: TaskExecutorOutsource,
		ExecutorID: "ow-s02", OutsourceMachine: "m-other", OutsourceDispatched: true,
	})
	w := putWorkerFixture(t, s, OutsourceWorker{
		ID: "ow-s02", Codename: "O-s02", Model: "opus", TaskID: task.ID,
		Status: WorkerStatusAssigned,
	})
	if got := spawnTarget(t, s, w, ServerSelfHost, "m-other", "m-third"); got != "m-other" {
		t.Fatalf("first boot must follow the task row: got %q, want m-other", got)
	}
	s.stampLandedMachine(w.ID, "m-other")

	task.OutsourceMachine = "m-third"
	if err := s.dal.PutTask(task); err != nil {
		t.Fatalf("re-put task: %v", err)
	}
	if got := spawnTarget(t, s, readWorker(t, s, w.ID), ServerSelfHost, "m-other", "m-third"); got != "m-other" {
		t.Fatalf("rebirth after a task-row edit: got %q, want m-other", got)
	}
}

// TestSticky_LandingIsTheObservedHostNotTheDispatchTarget pins WHICH signal the
// stickiness is built on. server→warden has no ack, so a dispatch is only an
// intent: sticking to the dispatch target would make ONE failed boot a
// permanent home. Only a session that actually connected counts.
func TestSticky_LandingIsTheObservedHostNotTheDispatchTarget(t *testing.T) {
	s := newWorkerTestServer(t)
	w := stickyFixture(t, s, "ow-s03", "m-other")
	connectWarden(t, s, ServerSelfHost)
	connectWarden(t, s, "m-other")
	connectWarden(t, s, "m-third")

	// Dispatch it at m-other and let the boot FAIL (no session ever connects —
	// we simply never stamp a landing).
	if got := spawnTarget(t, s, w, ServerSelfHost, "m-other", "m-third"); got != "m-other" {
		t.Fatalf("setup: got %q, want m-other", got)
	}
	if lm := readWorker(t, s, w.ID).LastMachineID; lm != "" {
		t.Fatalf("a dispatch that never booted must not count as a landing, got %q", lm)
	}
	// The owner fixes the 手冊 to a machine that works. Because m-other was
	// never a landing, the configured chain is still in charge and the worker
	// moves — a failed boot must never become a permanent address.
	if err := s.dal.PutTaskManual(TaskManual{TypeKey: "sticky-type", Purpose: "p",
		Fields:   "[]",
		Assignee: `{"kind":"outsource","model":"opus","machine":"m-third"}`,
	}); err != nil {
		t.Fatalf("re-put manual: %v", err)
	}
	if got := spawnTarget(t, s, readWorker(t, s, w.ID), ServerSelfHost, "m-other", "m-third"); got != "m-third" {
		t.Fatalf("after a never-booted dispatch the 手冊 still decides: got %q, want m-third", got)
	}
}

// TestSticky_ConnectStampsTheLandingFromTheTokenClaim drives the REAL SSE
// handler, because a projection nobody calls is not a defence: the placement
// tests above stamp landings by hand, and would all stay green if the wiring
// were removed.
func TestSticky_ConnectStampsTheLandingFromTheTokenClaim(t *testing.T) {
	s := newWorkerTestServer(t)
	w := stickyFixture(t, s, "ow-s04", "m-other")
	connect := func(id, machine string) {
		req := httptest.NewRequest("GET", "/api/events", nil)
		claims := map[string]any{"sub": id, "scope": "agent", "machine_id": machine}
		ctx, cancel := context.WithCancel(
			context.WithValue(req.Context(), claimsContextKey, claims))
		cancel()
		s.HandleEventsApiEventsGet(httptest.NewRecorder(), req.WithContext(ctx))
	}

	// The server dispatched it to m-other, so the claim it echoes back is
	// corroborated (connectionIsTheGenuineArticle).
	s.outsourceMu.Lock()
	s.workerSpawnTarget[w.ID] = "m-other"
	s.outsourceMu.Unlock()
	connect(w.ID, "m-other")
	if got := readWorker(t, s, w.ID).LastMachineID; got != "m-other" {
		t.Fatalf("last landing after connect = %q, want m-other", got)
	}
	// A connection with NO machine claim means "unknown", never "nowhere":
	// erasing a known landing here would silently hand the worker back to the
	// 手冊 (the exact bug, one layer down).
	connect(w.ID, "")
	if got := readWorker(t, s, w.ID).LastMachineID; got != "m-other" {
		t.Fatalf("a blank claim must not erase a known landing, got %q", got)
	}
	// Moving hosts re-stamps — once the server has actually dispatched there.
	s.outsourceMu.Lock()
	s.workerSpawnTarget[w.ID] = "m-third"
	s.outsourceMu.Unlock()
	connect(w.ID, "m-third")
	if got := readWorker(t, s, w.ID).LastMachineID; got != "m-third" {
		t.Fatalf("landing after a move = %q, want m-third", got)
	}
	// Staff members are OUT of scope: they carry a durable desired_machine_id
	// that already pins them, and a second weaker anchor would only disagree.
	if err := s.dal.PutMember(Member{ID: "g-staff", Name: "staff",
		Kind: KindAssistant, DesiredState: DesiredStateOnline,
		RosterStatus: RosterStatusActive}); err != nil {
		t.Fatalf("put staff: %v", err)
	}
	staff, err := s.dal.GetMember("g-staff")
	if err != nil || staff == nil {
		t.Fatalf("read staff: %+v %v", staff, err)
	}
	staff.DesiredMachineID = "m-other" // corroborated, so only kind keeps it out
	if err := s.dal.PutMember(*staff); err != nil {
		t.Fatalf("pin staff: %v", err)
	}
	connect("g-staff", "m-other")
	m, err := s.dal.GetMember("g-staff")
	if err != nil || m == nil {
		t.Fatalf("re-read staff: %+v %v", m, err)
	}
	if m.LastMachineID != "" {
		t.Fatalf("staff member must not carry a landing anchor, got %q", m.LastMachineID)
	}
}

// ── (B) the protection must not become a cage ────────────────────────────────

// TestSticky_FirstBootStillTakesTheConfiguredMachine: a worker that has never
// landed anywhere must behave EXACTLY as before. Guards against a fix that
// applies stickiness where there is no history to be sticky about.
func TestSticky_FirstBootStillTakesTheConfiguredMachine(t *testing.T) {
	s := newWorkerTestServer(t)
	w := stickyFixture(t, s, "ow-s05", "m-third")
	connectWarden(t, s, ServerSelfHost)
	connectWarden(t, s, "m-other")
	connectWarden(t, s, "m-third")
	if got := spawnTarget(t, s, w, ServerSelfHost, "m-other", "m-third"); got != "m-third" {
		t.Fatalf("first boot: got %q, want m-third (the 手冊)", got)
	}
}

// TestSticky_OwnerRelocateStillMoves is the arm the owner named: 改機器 must
// win over the last landing, always and immediately. If this reddens, the
// feature has locked the owner out of his own worker.
func TestSticky_OwnerRelocateStillMoves(t *testing.T) {
	s := newWorkerTestServer(t)
	w := stickyFixture(t, s, "ow-s06", "m-other")
	connectWarden(t, s, ServerSelfHost)
	connectWarden(t, s, "m-other")
	connectWarden(t, s, "m-third")
	landOn(t, s, w.ID, "m-other")

	// The owner pins m-third by hand.
	moved := readWorker(t, s, w.ID)
	moved.DesiredMachineID = "m-third"
	if err := s.dal.PutOutsourceWorker(moved); err != nil {
		t.Fatalf("pin: %v", err)
	}
	if got := spawnTarget(t, s, readWorker(t, s, w.ID), ServerSelfHost, "m-other", "m-third"); got != "m-third" {
		t.Fatalf("owner relocate: got %q, want m-third (the pin outranks the last landing)", got)
	}
	// And once it lands there, the new host is what sticks — 「一旦我手動改到其他
	// 電腦上,再次換手應該活在其他電腦上」. Even after the pin is dropped, the
	// worker stays on m-third rather than snapping back to the 手冊's m-other.
	landOn(t, s, w.ID, "m-third")
	unpinned := readWorker(t, s, w.ID)
	unpinned.DesiredMachineID = ""
	if err := s.dal.PutOutsourceWorker(unpinned); err != nil {
		t.Fatalf("unpin: %v", err)
	}
	if got := spawnTarget(t, s, readWorker(t, s, w.ID), ServerSelfHost, "m-other", "m-third"); got != "m-third" {
		t.Fatalf("after the hand-move: got %q, want m-third", got)
	}
}

// TestSticky_OfflineLastMachineFallsThroughToTheConfiguredOne is the second arm
// the owner named: a machine going down must not strand the worker on it. The
// last landing is a PREFERENCE (soft), unlike the owner's pin (hard).
func TestSticky_OfflineLastMachineFallsThroughToTheConfiguredOne(t *testing.T) {
	s := newWorkerTestServer(t)
	w := stickyFixture(t, s, "ow-s07", "m-third")
	connectWarden(t, s, ServerSelfHost)
	connectWarden(t, s, "m-third")
	// It last ran on m-other, which is now OFFLINE (never connected here).
	landOn(t, s, w.ID, "m-other")

	if got := spawnTarget(t, s, readWorker(t, s, w.ID), ServerSelfHost, "m-other", "m-third"); got != "m-third" {
		t.Fatalf("offline last landing: got %q, want m-third (機器下線 must still move it)", got)
	}
	// SENTINEL: bring m-other back and stickiness resumes — the fall-through is
	// a reaction to unavailability, not a permanent forfeit of the landing.
	connectWarden(t, s, "m-other")
	if got := spawnTarget(t, s, readWorker(t, s, w.ID), ServerSelfHost, "m-other", "m-third"); got != "m-other" {
		t.Fatalf("last landing back online: got %q, want m-other", got)
	}
}

// TestSticky_BenchedLastMachineFallsThroughToTheConfiguredOne: the other way a
// machine stops being dispatchable (a failed boot benches it for this worker).
// Same soft semantics — otherwise a single bad boot pins the worker to a
// standstill for the whole cooldown even though a configured machine is free.
func TestSticky_BenchedLastMachineFallsThroughToTheConfiguredOne(t *testing.T) {
	s := newWorkerTestServer(t)
	w := stickyFixture(t, s, "ow-s08", "m-third")
	connectWarden(t, s, ServerSelfHost)
	connectWarden(t, s, "m-other")
	connectWarden(t, s, "m-third")
	landOn(t, s, w.ID, "m-other")

	s.outsourceMu.Lock()
	s.benchWorkerMachine(w.ID, "m-other", nowSecs())
	s.outsourceMu.Unlock()
	if got := spawnTarget(t, s, readWorker(t, s, w.ID), ServerSelfHost, "m-other", "m-third"); got != "m-third" {
		t.Fatalf("benched last landing: got %q, want m-third", got)
	}
}

// TestSticky_UnreachableLastLandingWithNoAlternativeNamesItself: when the
// sticky machine is down and the configuration names nothing else, the worker
// stalls (correct — no machine may be invented) but the RECEIPT must accuse the
// machine that is actually offline. Saying "no machine is selected" would be a
// false statement that sends the owner to the wrong screen.
func TestSticky_UnreachableLastLandingWithNoAlternativeNamesItself(t *testing.T) {
	s := newWorkerTestServer(t)
	w := stickyFixture(t, s, "ow-s09", "") // 手冊 names nothing
	connectWarden(t, s, ServerSelfHost)
	landOn(t, s, w.ID, "m-other") // offline

	if got := spawnTarget(t, s, readWorker(t, s, w.ID), ServerSelfHost, "m-other", "m-third"); got != "" {
		t.Fatalf("unreachable landing with no alternative must dispatch nothing, got %q", got)
	}
	reason := readWorker(t, s, w.ID).LastOpReason
	if !strings.HasPrefix(reason, placementReasonUnavailable) {
		t.Fatalf("receipt = %q, want a %s verdict naming m-other", reason, placementReasonUnavailable)
	}
	if !strings.Contains(reason, "m-other") {
		t.Fatalf("receipt must name the machine that is down, got %q", reason)
	}
}

// TestSticky_GhostConnectionNeverRewritesTheLanding (review round 2, MEDIUM) is
// the BOTH-ARMS test of the 正身 gate on the landing stamp.
//
// The stamp is durable and it steers every future rebirth, so trusting any
// connection at all was the wrong criterion: "連上了" is not proof, "連上了 而且
// 確實是派到這裡的" is — precisely the check identitySweepOnConnect runs on the
// very next line, and for the very same reason (a wanderer's claim carries no
// authority). Sticky workers commonly have DesiredMachineID == "" and a server
// re-exec empties workerSpawnTarget too, so a residual ocagent on an old host
// is not even swept — its connection's only lasting effect on the fleet would
// be to move the worker's address to the ghost.
//
// Arm 1 (the one a naive gate breaks): a LEGITIMATE first landing — blank pin,
// dispatch target set — must still stamp.
// Arm 2 (the defect): a connection from a machine the server never sent this
// worker to must leave the known landing alone.
func TestSticky_GhostConnectionNeverRewritesTheLanding(t *testing.T) {
	s := newWorkerTestServer(t)
	w := stickyFixture(t, s, "ow-s10", "m-other")
	connect := func(id, machine string) {
		req := httptest.NewRequest("GET", "/api/events", nil)
		claims := map[string]any{"sub": id, "scope": "agent", "machine_id": machine}
		ctx, cancel := context.WithCancel(
			context.WithValue(req.Context(), claimsContextKey, claims))
		cancel()
		s.HandleEventsApiEventsGet(httptest.NewRecorder(), req.WithContext(ctx))
	}

	// Arm 1 — no pin at all (the ordinary sticky-worker shape), but the server
	// really did dispatch the start to m-other. This MUST land.
	if got := readWorker(t, s, w.ID).DesiredMachineID; got != "" {
		t.Fatalf("fixture: this arm needs a blank pin, got %q", got)
	}
	s.outsourceMu.Lock()
	s.workerSpawnTarget[w.ID] = "m-other"
	s.outsourceMu.Unlock()
	connect(w.ID, "m-other")
	if got := readWorker(t, s, w.ID).LastMachineID; got != "m-other" {
		t.Fatalf("a legitimate first landing must still stamp: got %q, want m-other", got)
	}

	// Arm 2 — a residual session on m-third connects. The server never sent this
	// worker there and nothing pins it there, so the claim is uncorroborated.
	connect(w.ID, "m-third")
	if got := readWorker(t, s, w.ID).LastMachineID; got != "m-other" {
		t.Fatalf("a ghost connection rewrote the landing to %q — the next rebirth "+
			"would follow it", got)
	}

	// Arm 1b — the OTHER corroboration source: the owner's pin. Server restart
	// amnesia (spawn target forgotten) must not block a pinned worker's landing.
	s.outsourceMu.Lock()
	delete(s.workerSpawnTarget, w.ID)
	s.outsourceMu.Unlock()
	pinned := readWorker(t, s, w.ID)
	pinned.DesiredMachineID = "m-third"
	if err := s.dal.PutOutsourceWorker(pinned); err != nil {
		t.Fatalf("pin: %v", err)
	}
	connect(w.ID, "m-third")
	if got := readWorker(t, s, w.ID).LastMachineID; got != "m-third" {
		t.Fatalf("a landing on the owner's own pin must stamp: got %q, want m-third", got)
	}
}
