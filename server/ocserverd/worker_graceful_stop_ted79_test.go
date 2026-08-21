package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
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

// ── 記憶回寫：the 下線預告 has to ASK for it, and only where there is a 手冊 ──

// workerOffboardNoticeOverSSE presses `press` and returns the offboard_notice
// the worker's OWN session actually RECEIVED — read back out of the bytes the
// REAL GET /api/events handler wrote to its connection, not out of the payload
// map the publisher built. That distinction is the whole point of this helper:
// every other assertion in this file reads offboardDeltaPayload directly, which
// is green even if nothing is ever fanned at the worker.
func workerOffboardNoticeOverSSE(t *testing.T, api *apiServer, workerID string,
	press func()) string {
	t.Helper()
	notice, frames := workerOffboardNoticeOverSSEOrEmpty(t, api, workerID, press)
	if notice == "" {
		t.Fatalf("the worker's own session never received an offboard_notice "+
			"(frames=%d)", frames)
	}
	return notice
}

// workerOffboardNoticeOverSSEOrEmpty is the same read, but hands the caller the
// silence instead of failing on it — so a test whose subject IS the missing
// frame can say what was missing rather than reporting a helper timeout.
func workerOffboardNoticeOverSSEOrEmpty(t *testing.T, api *apiServer, workerID string,
	press func()) (string, int) {
	t.Helper()
	w := newFailAfterWrites(1 << 30)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req := httptest.NewRequest(http.MethodGet, "/api/events", nil)
	req = req.WithContext(context.WithValue(ctx, claimsContextKey,
		map[string]any{"sub": workerID, "scope": "agent"}))
	done := make(chan struct{})
	go func() {
		api.HandleEventsApiEventsGet(w, req)
		close(done)
	}()

	notice := ""
	deadline := time.Now().Add(5 * time.Second)
	pressed := false
	for notice == "" {
		if time.Now().After(deadline) {
			break
		}
		select {
		case <-done:
			t.Fatal("the SSE handler returned before the 預告 arrived")
		default:
		}
		// Press only once THIS connection is the live one: the fixture already
		// holds a listener, and pressing before the takeover lands fans the 預告
		// at the session being replaced.
		if !pressed && len(w.written()) >= 1 {
			pressed = true
			press()
		}
		for _, frame := range w.written() {
			if got := offboardNoticeInFrame(t, frame); got != "" {
				notice = got
				break
			}
		}
		time.Sleep(2 * time.Millisecond)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the SSE handler never returned after cancellation")
	}
	t.Logf("frame as received by the worker session:\n%s", notice)
	return notice, len(w.written())
}

// offboardNoticeInFrame pulls offboard_notice out of one SSE wire frame, or ""
// when this frame is not a member delta carrying one.
func offboardNoticeInFrame(t *testing.T, frame []byte) string {
	t.Helper()
	_, data, ok := bytes.Cut(frame, []byte("data: "))
	if !ok {
		return ""
	}
	data, _, _ = bytes.Cut(data, []byte("\n"))
	var env struct {
		Data struct {
			Payload struct {
				OffboardNotice string `json:"offboard_notice"`
			} `json:"payload"`
		} `json:"data"`
	}
	if err := json.Unmarshal(data, &env); err != nil {
		return ""
	}
	return env.Data.Payload.OffboardNotice
}

// A worker doing a TYPED task is asked, in the 下線預告 it actually receives, to
// fold this run's memory back into that type's 任務手冊 — addressed by type_key,
// through the anchored patch verb.
//
// The 通道 is not what is at stake here (openWorkerHandoverGrace has fanned this
// delta since T-ea82, and T-ed79 wired 停止 into it); the SENTENCE is. Without
// it the worker is handed the generic 下線程序, which points at the boot doc's
// 「記憶與學習」 section and never names the manual this particular run has.
func TestWorkerStop_TypedTaskIsAskedToWriteBackToItsManual(t *testing.T) {
	api := newTasksTestServer(t)
	api.noOutsource = true
	workerID := newActiveOnlineWorker(t, api)

	notice := workerOffboardNoticeOverSSE(t, api, workerID, func() {
		postWorker(t, api, workerID, "stop", nil,
			api.HandleStopOutsourceWorkerApiOutsourceWorkersIdStopPost)
	})

	for _, want := range []string{"review-pr", "patch_task_learnings", "get_task_manual"} {
		if !strings.Contains(notice, want) {
			t.Errorf("the 下線預告 a typed worker received never mentions %q — the "+
				"owner's ruling is that an outsource worker going offline writes its "+
				"memory back into ITS 任務手冊, and a sentence that does not name the "+
				"type or the verb is not that instruction:\n%s", want, notice)
		}
	}
}

// …and a worker doing an AD-HOC task is NOT — the exception is not a policy of
// this ticket, it is the criterion the tree already carries: decideTaskCloseNudge
// stays silent on `t.TypeKey == ""` because「an AD-HOC task (no type) has no
// manual to write learnings into」. Asking for a write-back anyway would send the
// worker looking for a manual that does not exist, on the one path where it has
// no way to answer back.
func TestWorkerStop_AdHocTaskIsNotAskedToWriteBackToAManual(t *testing.T) {
	api := newTasksTestServer(t)
	api.noOutsource = true
	workerID := newActiveOnlineWorker(t, api)

	w, err := api.dal.GetOutsourceWorker(workerID)
	if err != nil || w == nil {
		t.Fatalf("read back worker: %v", err)
	}
	task, err := api.dal.GetTask(w.TaskID)
	if err != nil || task == nil {
		t.Fatalf("read back task: %v", err)
	}
	task.TypeKey = "" // 自由代辦: no type, therefore no manual
	if err := api.dal.PutTask(*task); err != nil {
		t.Fatalf("clear type_key: %v", err)
	}

	notice := workerOffboardNoticeOverSSE(t, api, workerID, func() {
		postWorker(t, api, workerID, "stop", nil,
			api.HandleStopOutsourceWorkerApiOutsourceWorkersIdStopPost)
	})

	for _, unwanted := range []string{"patch_task_learnings", "任務手冊"} {
		if strings.Contains(notice, unwanted) {
			t.Errorf("an ad-hoc worker was told to write back to a 任務手冊 it does "+
				"not have (%q appears in the 預告):\n%s", unwanted, notice)
		}
	}
}

// ── the 加速停止 deadline has to reach the worker, not just the row ──────────

// deadlineClause is the literal offboardNotice appends for a final call. The
// tests below match on the CLAUSE, not on a formatted time, because the point
// is whether the sentence declares a deadline at all.
const deadlineClause = "Your deadline is "

// 🔴 THE HARM THIS WHOLE TICKET EXISTS TO PREVENT, on the outsource half.
//
// 加速停止 puts the worker on a clock — autoHandoverWorker collects it at
// stopping_since + grace ("stop-accelerated-deadline"). Every other assertion
// about that clock reads the ROW or the payload map the publisher builds, and
// all of them are green while the worker's own session receives nothing: the
// handler used to publish only on the owner-only `outsource_worker` topic, whose
// payload is {id, codename, status}. The last sentence the worker had heard was
// the 停止 SOFT notice — "no deadline" — while the clock was already running.
//
// So this reads the bytes the REAL GET /api/events handler wrote to the worker's
// OWN connection. A guard that verifies the composing functions cannot see this:
// what broke was DELIVERY, not the judgement.
func TestWorkerAcceleratedStop_TellsTheWorkerAboutTheClockItStarted(t *testing.T) {
	api := newTasksTestServer(t)
	api.noOutsource = true
	workerID := newActiveOnlineWorker(t, api)

	// Open the wind-down first, on the fixture's connection: the rung under test
	// is the ESCALATION, and the SOFT notice that opening fans is not what this
	// test reads.
	postWorker(t, api, workerID, "stop", nil,
		api.HandleStopOutsourceWorkerApiOutsourceWorkersIdStopPost)

	notice, frames := workerOffboardNoticeOverSSEOrEmpty(t, api, workerID, func() {
		rec := postWorker(t, api, workerID, "accelerated-stop", nil,
			api.HandleAcceleratedStopOutsourceWorkerApiOutsourceWorkersIdAcceleratedStopPost)
		if rec.Code != http.StatusOK {
			t.Errorf("加速停止: %d %s", rec.Code, rec.Body.String())
		}
	})
	if notice == "" {
		t.Fatalf("加速停止 started a clock the worker was never told about: its own "+
			"session received %d frame(s) and NOT ONE offboard_notice. The last "+
			"thing this worker heard is the 停止 SOFT sentence, which says there is "+
			"no deadline — and autoHandoverWorker will collect it at "+
			"stopping_since + grace anyway. That is a clock with no sentence: the "+
			"worker is cut off mid-hand-off with no warning at all.", frames)
	}
	if !strings.Contains(notice, deadlineClause) {
		t.Fatalf("the worker DID receive a 預告, but it is still the SOFT one — no "+
			"deadline clause, while the 加速停止 clock runs. The sentence and the "+
			"clock have to agree on the wire, not merely in winddownKindFor:\n%s",
			notice)
	}
	w, err := api.dal.GetOutsourceWorker(workerID)
	if err != nil || w == nil {
		t.Fatalf("read back: %v", err)
	}
	// The clause has to quote the deadline THIS press anchored, or the worker is
	// working to a time nothing counts.
	grace, clocked := recycleGraceFor(w.RefocusOp, api.reconcileConfigLive())
	if !clocked {
		t.Fatal("加速停止 must put the stop epoch on a clock")
	}
	want := time.Unix(int64(w.StoppingSince+grace), 0).UTC().Format(time.RFC3339)
	if !strings.Contains(notice, deadlineClause+want+".") {
		t.Fatalf("the 預告 quotes a deadline that is not the one the collect uses "+
			"(want %q, the anchor this press re-stamped plus the grace):\n%s",
			want, notice)
	}
}

// …and the OTHER half, which is the one a careless fix breaks: a plain 停止 runs
// NO clock (owner rc-27d1710174dd), so its sentence must still quote no time.
// Making 加速停止 speak by fanning a final notice from every outsource write
// would grow a deadline on this arm too — a countdown in the agent's head that
// nothing is counting, the same bug seen from the other side.
func TestWorkerStop_SentenceOnTheWireQuotesNoDeadline(t *testing.T) {
	api := newTasksTestServer(t)
	api.noOutsource = true
	workerID := newActiveOnlineWorker(t, api)

	notice := workerOffboardNoticeOverSSE(t, api, workerID, func() {
		postWorker(t, api, workerID, "stop", nil,
			api.HandleStopOutsourceWorkerApiOutsourceWorkersIdStopPost)
	})
	if strings.Contains(notice, deadlineClause) {
		t.Fatalf("a plain 停止 is collected by the worker's own report_stopped or by "+
			"強制停止 — nothing counts a clock on this arm, so quoting a deadline "+
			"promises what nobody keeps:\n%s", notice)
	}
}
