package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// winddown_stopped_report_ted79_test.go — ONE invariant, in the narrow window
// where this ticket created a way to break it:
//
//	an agent's own 「我收完了」 is never thrown away by the server while the
//	agent is still there.
//
// 🔴 WHY THIS WINDOW EXISTS AT ALL. report_stopped latches stopped_since on ANY
// staff member — no refocus epoch required (owner rc-b08d49dc3b03: a
// stopped-report is ALWAYS collected) — and then fires ONE
// dispatchRobustStopNow. That dispatch is best-effort and has no retry, so a
// warden that is unreachable at that instant leaves the row parked at
//
//	desired online ∧ SSE online ∧ stopped_since > 0 ∧ refocus_since == 0
//
// and nothing sweeps it: clearRecycleMarkersOnRespawn skips anything still
// online, and clearStaleStoppingOnOnline only ever zeroes stopping_since.
//
// 🔴 WHAT THIS TICKET ADDED. Before T-ed79 the FIRST context threshold stamped
// nothing at all — it sent one SSE band. Making it open a wind-down turned it
// into a NEW entry point into armRefocusEpoch, which zeroes the wind-down
// anchors by design. So a member parked in the state above, whose context then
// crossed the notice line, had the agent's finished close-out erased by the
// server. That is the harm asserted below, and it is asserted as the STATE
// TRANSITION rather than as any sentence, because the sentence is not what is
// lost.
//
// The precedent for the fix is nine lines up the same file:
// canPromoteToAcceleratedStop already refuses to touch a member with
// stopped_since > 0, and says why — 「the promotion notice would reach a session
// that has said it is finished」. This is the same ruling, on the arm that was
// missed.

// parkedOnAnUncollectedStoppedReport builds the window ABOVE THROUGH THE SERVER
// — the real report_stopped handler on a real online member — rather than by
// hand-setting the two fields. A fixture that sets them directly would still be
// green on the day the handler stops producing them, which is the whole failure
// mode this file exists to avoid.
func parkedOnAnUncollectedStoppedReport(t *testing.T, s *apiServer, id string) Member {
	t.Helper()
	m := testAgent(id)
	m.DesiredMachineID = "mach-gone"
	putTestMember(t, s, m)
	connectOnline(t, s, id) // ocagent still holds the SSE

	rec := httptest.NewRecorder()
	s.HandleReportStoppedApiSelfStoppedPost(rec,
		taskReq(t, "POST", "/api/self/stopped", map[string]any{}, id, "agent"))
	if rec.Code != http.StatusOK {
		t.Fatalf("report_stopped: %d %s", rec.Code, rec.Body.String())
	}

	got, err := s.dal.GetMember(id)
	if err != nil || got == nil {
		t.Fatalf("refetch %s: %v", id, err)
	}
	// The window is only interesting if the SERVER really parks a member here.
	if got.StoppedSince <= 0 {
		t.Fatalf("fixture: report_stopped did not latch stopped_since (%+v) — "+
			"this window may no longer exist, which would make this whole file moot",
			got)
	}
	if got.RefocusSince != 0 {
		t.Fatalf("fixture: report_stopped opened a refocus epoch (%v) — the window "+
			"asserted here is the EPOCH-LESS one", got.RefocusSince)
	}
	if !s.hub.IsOnline(id) {
		t.Fatal("fixture: the member must still be online (the STOP never landed)")
	}
	return *got
}

func TestWindDownKind_TheFirstContextThresholdDoesNotEraseAStoppedReport(t *testing.T) {
	s := newReconcileTestServer(t)
	cfg := s.ctxHighConfig()
	// 🔴 ONE CLOCK. report_stopped stamps with nowSecs() and cannot be injected,
	// so a hand-picked constant here would make stopped_since >= boot_ts true (or
	// false) by accident rather than by the fixture's shape — and that comparison
	// is the whole discriminator under test.
	now := nowSecs()

	parked := parkedOnAnUncollectedStoppedReport(t, s, "m-ed79-parked")

	// Neither reconcile sweep takes the latch away, so it is genuinely still
	// there when the threshold arrives — the premise of everything below.
	members := []Member{parked}
	s.clearRecycleMarkersOnRespawn(members)
	s.clearStaleStoppingOnOnline(members, now)
	if swept, _ := s.dal.GetMember(parked.ID); swept == nil || swept.StoppedSince <= 0 {
		t.Fatalf("premise: a sweep cleared the latch, so this window closes on its "+
			"own: %+v", swept)
	}

	// …and now context crosses the FIRST threshold.
	s.gauge.Set(parked.ID, map[string]any{
		"context_pct": float64(cfg.NoticePct), "context_pct_ts": now - 10,
		"boot_ts": now - 5000,
	})
	pass := []Member{parked}
	s.stampContextHighRecycle(pass, now)

	got, err := s.dal.GetMember(parked.ID)
	if err != nil || got == nil {
		t.Fatalf("refetch: %v", err)
	}

	if got.StoppedSince <= 0 {
		t.Fatalf("the first context threshold ERASED the agent's own stopped "+
			"report (stopped_since %v → %v). The agent said it had finished; the "+
			"server threw that away and re-opened a wind-down on a session that "+
			"was already done. canPromoteToAcceleratedStop refuses exactly this "+
			"and says why — this arm has to make the same ruling.",
			parked.StoppedSince, got.StoppedSince)
	}
	if got.RefocusSince != 0 || got.RefocusOp != "" {
		t.Fatalf("a member that has already reported stopped was put into a NEW "+
			"wind-down (op=%q since=%v). There is nothing left to ask it to do, "+
			"and the epoch is what makes the erasure possible in the first place.",
			got.RefocusOp, got.RefocusSince)
	}
}

// The SECOND threshold has to answer the same way, for the same reason — and
// this half is what makes the skip safe to state as a rule instead of a patch:
// the promotion path was ALREADY refusing a stopped-reported member
// (canPromoteToAcceleratedStop), so declining to open a fresh epoch here loses
// nothing the server was ever willing to do.
func TestWindDownKind_TheSecondContextThresholdDoesNotEraseAStoppedReport(t *testing.T) {
	s := newReconcileTestServer(t)
	cfg := s.ctxHighConfig()
	now := nowSecs()

	parked := parkedOnAnUncollectedStoppedReport(t, s, "m-ed79-parked-high")

	s.gauge.Set(parked.ID, map[string]any{
		"context_pct": float64(cfg.HandoverPct), "context_pct_ts": now - 10,
		"boot_ts": now - 5000,
	})
	pass := []Member{parked}
	s.stampContextHighRecycle(pass, now)

	got, err := s.dal.GetMember(parked.ID)
	if err != nil || got == nil {
		t.Fatalf("refetch: %v", err)
	}
	if got.StoppedSince <= 0 {
		t.Fatalf("the second context threshold erased the stopped report "+
			"(stopped_since %v → %v)", parked.StoppedSince, got.StoppedSince)
	}
	if got.RefocusOp == refocusOpContextHigh && got.RefocusSince == now {
		t.Fatalf("a member that has already reported stopped was put on the " +
			"加速停止 clock — the deadline sentence would reach a session that has " +
			"said it is finished, which canPromoteToAcceleratedStop already refuses")
	}
}

// 🔴 THE OTHER SIDE OF THE SAME LINE, and the reason the guard above tests
// boot_ts instead of just `stopped_since > 0`.
//
// activate clears stopping_since and waking_since but NOT stopped_since, so
// 下線 → 活化 puts a brand-new session online carrying the PREVIOUS
// generation's report and no epoch. (If activate is ever changed to clear
// stopped_since too, this state stops being server-reachable and this test
// becomes a fixture nothing can produce — re-examine it in that edit rather
// than leaving it green.) That latch is not this session's close-out
// and must not silence its thresholds: a bare `stopped_since > 0` skip would
// exclude such a member from BOTH context thresholds for the rest of its life —
// it would never hand over again, at any pct. The threshold must still fire,
// and armRefocusEpoch must still scrub the stale latch (which is what
// TestRefocusEpoch_NoStampSiteInheritsAStaleWindDownLatch pins from the other
// direction).
func TestWindDownKind_APredecessorsLatchDoesNotSilenceTheThresholds(t *testing.T) {
	s := newReconcileTestServer(t)
	cfg := s.ctxHighConfig()
	now := nowSecs()

	m := testAgent("m-ed79-inherited")
	// The shape 下線 → 活化 leaves behind: a previous generation's report, no
	// epoch, and a session that booted AFTER it.
	m.StoppedSince = now - 10_000
	putTestMember(t, s, m)
	connectOnline(t, s, m.ID)
	s.gauge.Set(m.ID, map[string]any{
		"context_pct": float64(cfg.NoticePct), "context_pct_ts": now - 10,
		"boot_ts": now - 5_000,
	})

	pass := []Member{m}
	s.stampContextHighRecycle(pass, now)

	got, err := s.dal.GetMember(m.ID)
	if err != nil || got == nil {
		t.Fatalf("refetch: %v", err)
	}
	if got.RefocusOp != refocusOpContextNotice || got.RefocusSince != now {
		t.Fatalf("a PREDECESSOR's latch silenced the first context threshold "+
			"(op=%q since=%v). This member would never hand over again, at any "+
			"pct, for the rest of its life — worse than the erasure the boot_ts "+
			"test exists to prevent.", got.RefocusOp, got.RefocusSince)
	}
	if got.StoppedSince != 0 {
		t.Fatalf("the fresh epoch inherited the predecessor's latch "+
			"(stopped_since=%v) — decideUp reads that as 'dump done' and "+
			"robust-stops on the next tick with no close-out", got.StoppedSince)
	}
}
