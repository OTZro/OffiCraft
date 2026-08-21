package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// worker_graceful_stop_ted79_test.go — 外包的 停止 becomes a close-out
// (owner 2026-08-21 「往正職靠：外包那顆改成優雅停止，強制殺移到第三顆按鈕」).
//
// The staff ruling this aligns to: 停止 = the offboard sequence, no deadline,
// collected by the agent's own report_stopped or by the owner's force-stop
// (rc-27d1710174dd). The worker's 停止 did none of that: it flipped
// desired_state, cleared any in-flight refocus, stamped forced_stop_at +
// stopping_since and killed the session outright.

// 🔴 THE CELL MOST LIKELY TO BREAK, which is why it is the FIRST test in the
// file rather than a corollary of the others.
//
// workerReportStopped's collect arm is gated on
// `DesiredState == online ∧ RefocusSince > 0`. A 停止 epoch satisfies NEITHER.
// Everything else about a graceful worker stop can be right and this one gate
// still leaves the worker parked alive forever on a session it has already
// closed out — strictly worse than killing it on the spot, which is what the
// verb used to do.
func TestWorkerStop_ReportStoppedCollectsTheStopEpoch(t *testing.T) {
	api := newTasksTestServer(t)
	api.noOutsource = true
	workerID := newActiveOnlineWorker(t, api)

	postWorker(t, api, workerID, "stop", nil,
		api.HandleStopOutsourceWorkerApiOutsourceWorkersIdStopPost)
	// Whatever the stop itself dispatched is not what this test is about.
	api.hub.DrainWardenCommands(ServerSelfHost)

	rec := httptest.NewRecorder()
	api.HandleReportStoppedApiSelfStoppedPost(rec,
		taskReq(t, http.MethodPost, "/api/self/stopped", nil, workerID, "agent"))
	if rec.Code != http.StatusOK {
		t.Fatalf("report_stopped: %d %s", rec.Code, rec.Body.String())
	}

	frames := api.hub.DrainWardenCommands(ServerSelfHost)
	sawKill, sawStart := false, false
	for _, f := range frames {
		rpc, _ := decodeWardenFrame(t, f.Frame)
		// P5b convergence: workers ride the MEMBER verbs (`start` / `stop`),
		// addressed to the worker's own id — `worker_start` is the retired name
		// the receipt fold still recognises, and asserting on it here would make
		// this test green on zero frames.
		switch rpc {
		case reconcileCmdStop:
			sawKill = true
		case reconcileCmdStart:
			sawStart = true
		}
	}
	if !sawKill {
		t.Fatalf("the worker said it had finished its close-out and NOTHING came "+
			"to collect it (frames=%d). workerReportStopped's collect arm requires "+
			"desired online + refocus_since > 0, and a 停止 epoch is neither — so a "+
			"gracefully stopped worker is parked alive on a session it already "+
			"closed out, which is worse than the kill this verb used to do.",
			len(frames))
	}
	if sawStart {
		t.Fatal("a 停止 is held down: the collect must kill WITHOUT re-spawning")
	}
	w, err := api.dal.GetOutsourceWorker(workerID)
	if err != nil || w == nil {
		t.Fatalf("read back: %v", err)
	}
	if w.StoppedSince <= 0 {
		t.Fatalf("the collect must latch stopped_since: %+v", w)
	}
	if w.DesiredState != DesiredStateOffline {
		t.Fatalf("a collected 停止 stays held down, desired=%q", w.DesiredState)
	}
}

// 停止 no longer kills on the spot, and no longer stamps the anchor that keeps
// the notice silent — the two halves of the owner's ruling, asserted together
// because either one alone is still a broken stop: a silent graceful stop is a
// worker waiting for an instruction it was never given, and a speaking
// immediate kill is a sentence delivered to a session that no longer exists.
func TestWorkerStop_IsAGracefulCloseOutThatTheWorkerCanHear(t *testing.T) {
	api := newTasksTestServer(t)
	api.noOutsource = true
	workerID := newActiveOnlineWorker(t, api)
	l, err := api.hub.Connect(workerID, "") // takeover of the fixture listener
	if err != nil {
		t.Fatalf("connect worker SSE: %v", err)
	}
	t.Cleanup(func() { api.hub.Disconnect(l) })

	postWorker(t, api, workerID, "stop", nil,
		api.HandleStopOutsourceWorkerApiOutsourceWorkersIdStopPost)

	if got := len(api.hub.DrainWardenCommands(ServerSelfHost)); got != 0 {
		t.Fatalf("停止 is a close-out request, not a kill — it must dispatch NOTHING "+
			"and let the worker work its offboard sequence, got %d frame(s)", got)
	}
	w, err := api.dal.GetOutsourceWorker(workerID)
	if err != nil || w == nil {
		t.Fatalf("read back: %v", err)
	}
	if w.ForcedStopAt != 0 {
		t.Fatalf("停止 must not stamp forced_stop_at (%v) — that anchor belongs to "+
			"強制停止, and its two reasons (keep the notice silent, record that this "+
			"session was CUT OFF) are both false of a close-out the worker is being "+
			"asked to work", w.ForcedStopAt)
	}
	if w.StoppingSince <= 0 {
		t.Fatalf("the stop epoch needs an anchor — the notice rides "+
			"`desired offline ∧ stopping_since > 0`: %+v", w)
	}
	m := memberFromWorker(*w)
	if forcedEpochLive(m) {
		t.Fatalf("a graceful 停止 must NOT read as a live forced epoch "+
			"(forced_stop_at=%v stopping_since=%v) — that is the predicate that "+
			"silences the notice", m.ForcedStopAt, m.StoppingSince)
	}
	notice, ok := api.offboardDeltaPayload(m)["offboard_notice"].(string)
	if !ok || notice == "" {
		t.Fatalf("a worker asked to stop gracefully must be SHOWN the 下線程序 — "+
			"the delta carried no offboard_notice at all (%+v)",
			api.offboardDeltaPayload(m))
	}
}

// The MIDDLE rung has to be reachable from the new 停止, or the ladder the owner
// asked for (停止 → 加速停止 → 強制停止) has a hole in the middle for outsource:
// 加速停止 used to 409 on desired_state=offline, which was right while 停止 killed
// on the spot and is a dead button the moment it stops. Both halves are asserted
// — the endpoint accepting the stop epoch, and something actually collecting on
// the deadline it promises — because an accepted press that no clock honours is
// the same silent lie as no press at all.
func TestWorkerStop_AcceleratedStopEscalatesTheStopEpochAndIsHonoured(t *testing.T) {
	api := newTasksTestServer(t)
	api.noOutsource = true
	workerID := newActiveOnlineWorker(t, api)

	postWorker(t, api, workerID, "stop", nil,
		api.HandleStopOutsourceWorkerApiOutsourceWorkersIdStopPost)
	rec := postWorker(t, api, workerID, "accelerated-stop", nil,
		api.HandleAcceleratedStopOutsourceWorkerApiOutsourceWorkersIdAcceleratedStopPost)
	if rec.Code != http.StatusOK {
		t.Fatalf("加速停止 must escalate a 停止 that is already open — 停止 IS the "+
			"wind-down this rung escalates now, and refusing here leaves the owner "+
			"with 停止 → (409) → 強制停止: %d %s", rec.Code, rec.Body.String())
	}
	w, err := api.dal.GetOutsourceWorker(workerID)
	if err != nil || w == nil {
		t.Fatalf("read back: %v", err)
	}
	if w.RefocusOp != refocusOpAcceleratedStop {
		t.Fatalf("the cause must be written so the SENTENCE and the CLOCK read the "+
			"same winddownKindFor answer, got %q", w.RefocusOp)
	}
	if _, clocked := recycleGraceFor(w.RefocusOp, api.reconcileConfigLive()); !clocked {
		t.Fatal("加速停止 must put the stop epoch on a clock")
	}
	grace := api.reconcileConfigLive().RecycleGrace
	api.hub.DrainWardenCommands(ServerSelfHost)

	// Inside the grace: nothing is collected — the worker still has the time it
	// was just quoted.
	api.outsourceMu.Lock()
	api.autoHandoverWorker(*w, w.StoppingSince+grace-1)
	api.outsourceMu.Unlock()
	if got := len(api.hub.DrainWardenCommands(ServerSelfHost)); got != 0 {
		t.Fatalf("inside the 加速停止 grace nothing may be collected, got %d frame(s)", got)
	}

	// Past it: collected — killed, and NOT re-spawned.
	api.outsourceMu.Lock()
	api.autoHandoverWorker(*w, w.StoppingSince+grace)
	api.outsourceMu.Unlock()
	frames := api.hub.DrainWardenCommands(ServerSelfHost)
	sawKill, sawStart := false, false
	for _, f := range frames {
		switch rpc, _ := decodeWardenFrame(t, f.Frame); rpc {
		case reconcileCmdStop:
			sawKill = true
		case reconcileCmdStart:
			sawStart = true
		}
	}
	if !sawKill {
		t.Fatalf("the 加速停止 deadline the owner started — and the worker was told "+
			"about — arrived and nothing collected the worker (frames=%d)", len(frames))
	}
	if sawStart {
		t.Fatal("a 停止 is held down: the deadline collect must kill WITHOUT re-spawning")
	}
}
