package main

import "testing"

// T-72dd FOUNDATION: does an outsource row actually accept a
// write through the STAFF path (putMember(memberFromWorker(w))) and read back
// through the WORKER path (dal.GetOutsourceWorker)?
func TestWorkerRowTakesTheStaffWritePath_T72dd(t *testing.T) {
	api := newTasksTestServer(t)
	api.noOutsource = true
	workerID := newActiveOnlineWorker(t, api)

	// ── NEGATIVE CONTROL (a): do not write — the read must show the OLD value.
	pre, err := api.dal.GetOutsourceWorker(workerID)
	if err != nil || pre == nil {
		t.Fatalf("read back before the write: %v", err)
	}
	t.Logf("CONTROL-a (no write yet): refocus_since=%v refocus_op=%q stopped_since=%v desired=%q",
		pre.RefocusSince, pre.RefocusOp, pre.StoppedSince, pre.DesiredState)
	if pre.RefocusSince != 0 || pre.RefocusOp != "" {
		t.Fatalf("control-a broken: the fixture already carries an epoch "+
			"(since=%v op=%q) — a later PASS would prove nothing",
			pre.RefocusSince, pre.RefocusOp)
	}

	// ── THE WRITE, through the staff path.
	m := memberFromWorker(*pre)
	m.RefocusSince = 1234.5
	m.RefocusOp = refocusOpRefocus
	m.StoppedSince = 777.25
	if err := api.putMember(m, "t72dd-probe"); err != nil {
		t.Fatalf("putMember(memberFromWorker(w)): %v", err)
	}

	// ── THE READ, through the worker path (NOT the member path).
	got, err := api.dal.GetOutsourceWorker(workerID)
	if err != nil || got == nil {
		t.Fatalf("GetOutsourceWorker after the staff write: %v", err)
	}
	t.Logf("AFTER staff write, read via dal.GetOutsourceWorker: refocus_since=%v refocus_op=%q stopped_since=%v desired=%q",
		got.RefocusSince, got.RefocusOp, got.StoppedSince, got.DesiredState)
	if got.RefocusSince != 1234.5 || got.RefocusOp != refocusOpRefocus || got.StoppedSince != 777.25 {
		t.Fatalf("the staff write did NOT land on the worker row: "+
			"since=%v op=%q stopped=%v", got.RefocusSince, got.RefocusOp, got.StoppedSince)
	}

	// ── NEGATIVE CONTROL (b): a row that does not exist must read as nil.
	ghost, err := api.dal.GetOutsourceWorker("ow-no-such-worker")
	t.Logf("CONTROL-b (unknown id): row=%v err=%v", ghost, err)
	if err == nil && ghost != nil {
		t.Fatalf("control-b broken: GetOutsourceWorker invented a row for an unknown id")
	}

	// ── The 預告 half: can a refocus stamp reach an outsource projection at all?
	if !aRefocusStampWouldReachTheAgent(m) {
		t.Fatalf("aRefocusStampWouldReachTheAgent said NO for desired=%q", m.DesiredState)
	}
	proj := memberFromWorker(*got)
	kind, carries := offboardKindOf(proj, nowSecs())
	payload := api.offboardDeltaPayload(proj)
	notice, hasNotice := payload["offboard_notice"].(string)
	t.Logf("PROJECTION: kind=%q carries=%v hasNotice=%v desired=%q forced_stop_at=%v",
		kind, carries, hasNotice, proj.DesiredState, proj.ForcedStopAt)
	t.Logf("NOTICE(first 160): %.160s", notice)
	if !hasNotice {
		t.Fatalf("a refocus-stamped OUTSOURCE projection carried NO 預告 — payload=%v", payload)
	}

	// ── NEGATIVE CONTROL (c) for the notice: a worker with no epoch at all
	// must NOT carry one, or the assertion above passes for everything.
	clean := memberFromWorker(*pre)
	if _, ok := api.offboardDeltaPayload(clean)["offboard_notice"]; ok {
		t.Fatalf("control-c broken: an un-stamped worker also carries a notice")
	}
	t.Logf("CONTROL-c (no epoch): no offboard_notice — the notice check discriminates")
}

// T-72dd: is an EMPTY desired_state a state a worker row can actually hold?
// It matters because un-blinding routes "" to decideDown (the switch's default
// arm), so a "" row would stop being spawned.
func TestWorkerEmptyDesiredStateRoundTrips_T72dd(t *testing.T) {
	api := newTasksTestServer(t)
	api.noOutsource = true
	w := OutsourceWorker{ID: "ow-empty", Codename: "E-1", Model: "claude-sonnet-4-5",
		Effort: "medium", TaskID: "", Status: WorkerStatusAssigned}
	if err := api.dal.PutOutsourceWorker(w); err != nil {
		t.Fatalf("put: %v", err)
	}
	got, err := api.dal.GetOutsourceWorker("ow-empty")
	if err != nil || got == nil {
		t.Fatalf("read back: %v", err)
	}
	t.Logf("EMPTY desired_state written -> read back as %q (column DEFAULT did NOT rescue it: %v)",
		got.DesiredState, got.DesiredState == "")
	t.Logf("reconcileDecide routing for %q: goes to decideDown (switch default arm)",
		got.DesiredState)
}
