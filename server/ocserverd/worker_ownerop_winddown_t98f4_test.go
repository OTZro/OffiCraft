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
