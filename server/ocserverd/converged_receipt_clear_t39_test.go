package main

import (
	"strings"
	"testing"
)

// T-39 — A FAILURE RECEIPT MUST NOT OUTLIVE THE FAILURE IT DESCRIBES.
//
// The cockpit's 「最近操作」 block renders on `lastOp !== "" && lastOpAt != null`
// (AgentDetailPanel.tsx) — no presence check, no age ceiling. So a red
// "上一次操作失敗" line, once written, stands forever: the only clearing arm in
// the whole server (stampWakeObservability's landed-START path) is gated on
// `decision.Command == start`, and a member that has CONVERGED decides
// `Command == none`. "Cleared" and "healthy again" were mutually exclusive — to
// lose the line the member had to break a second time. Field evidence: one
// receipt standing 10.6 days on a member that was online the whole time.
//
// Owner ruling (rc-f2e963132fc5, choice [1]): 「他回來了就把那行字直接拿掉」.
// Not rewritten, not archived — removed. The two tests per side below are the
// two halves of that ruling and neither is optional:
//
//	CLEARED  — converged back online ⇒ the block disappears from the panel.
//	KEPT     — still not online     ⇒ the receipt is untouched, verbatim.
//
// The KEPT half is what stops the fix from becoming "blank the row on every
// tick", which would hide the failures that are still happening.

// convergedOnlineReasonT39 is the decider's own sentence for "desired online and
// it IS online" — the tick these tests are about. Matched as text so the fixture
// guards do not depend on the fix they are guarding.
const convergedOnlineReasonT39 = "online: converged"

// failedMemberReceipt is the row state the panel renders as the red line: a
// server-authored refusal (last_op_ok = non-nil FALSE) with all five columns
// populated.
func failedMemberReceipt(m Member, at float64) Member {
	no := false
	m.LastOp = reconcileCmdStart
	m.LastOpOK = &no
	m.LastOpLog = "boot.log: claude exited 1"
	m.LastOpReason = wakeTimeoutReasonCode + ": the start window elapsed and the agent never came online"
	m.LastOpAt = at
	return m
}

// assertPanelBlockHidden asserts the EXACT condition the cockpit renders on —
// `vm.lastOp !== "" && vm.lastOpAt != null`, with last_op_at 0 mapping to null
// in mappers.ts. Asserting "the reason is empty" would NOT be this: a blank
// reason still leaves the block on screen, just wordless.
func assertPanelBlockHidden(t *testing.T, who, lastOp string, lastOpAt float64,
	lastOpOK *bool, lastOpLog, lastOpReason string) {
	t.Helper()
	if lastOp != "" || lastOpAt != 0 {
		t.Fatalf("%s: the 最近操作 block is STILL ON SCREEN — the panel shows it "+
			"whenever last_op != \"\" && last_op_at != 0; got last_op=%q last_op_at=%v",
			who, lastOp, lastOpAt)
	}
	if lastOpOK != nil {
		t.Errorf("%s: last_op_ok must go back to nil (no receipt yet), got %v", who, *lastOpOK)
	}
	if lastOpLog != "" || lastOpReason != "" {
		t.Errorf("%s: the receipt text must go with the receipt, got log=%q reason=%q",
			who, lastOpLog, lastOpReason)
	}
}

// ── staff member ────────────────────────────────────────────────────────────

// M-① sentinel (staff). Remove the converged clear and this goes red on the
// named assertion "the member is back online, so the failed-op receipt must be
// gone".
func TestReconcile_ConvergedOnlineMemberClearsStaleFailureReceipt_T39(t *testing.T) {
	s := newReconcileTestServer(t)
	putWarden(t, s, "mach-live")
	connectOnline(t, s, "mach-live")

	stampedAt := 1_000_000.0
	m := testAgent("m-back")
	m.DesiredMachineID = "mach-live"
	m = failedMemberReceipt(m, stampedAt)
	putTestMember(t, s, m)
	connectOnline(t, s, "m-back") // the agent came back on its own

	s.reconcileMu.Lock()
	dec := s.reconcileTickMemberLocked(m, stampedAt+600)
	s.reconcileMu.Unlock()
	if dec.Command != reconcileCmdNone || !strings.Contains(dec.Reason, convergedOnlineReasonT39) {
		t.Fatalf("fixture is blind: this tick did not converge (%q / %s)", dec.Command, dec.Reason)
	}

	got, err := s.dal.GetMember("m-back")
	if err != nil || got == nil {
		t.Fatalf("re-read member: %v", err)
	}
	assertPanelBlockHidden(t, "the member is back online, so the failed-op receipt must be gone",
		got.LastOp, got.LastOpAt, got.LastOpOK, got.LastOpLog, got.LastOpReason)
}

// M-② sentinel (staff). Widen the clear to "unconditional" — drop the converged
// gate — and this goes red on the named assertion "the member is STILL not
// online, so its failure receipt must survive verbatim". Without this half the
// change would be pinned only on its benefit and not on its cost.
func TestReconcile_StillOfflineMemberKeepsItsFailureReceipt_T39(t *testing.T) {
	s := newReconcileTestServer(t)
	putWarden(t, s, "mach-live")
	connectOnline(t, s, "mach-live")

	m := testAgent("m-stuck")
	m.DesiredMachineID = "mach-live"
	putTestMember(t, s, m)

	// tick 1 dispatches the START; tick 2, past the start window with no
	// presence, writes the real wake_timeout receipt (not a hand-planted one).
	base := 2_000_000.0
	s.reconcileMu.Lock()
	s.reconcileTickMemberLocked(m, base)
	s.reconcileMu.Unlock()
	reloaded, _ := s.dal.GetMember("m-stuck")
	s.reconcileMu.Lock()
	lapse := s.reconcileTickMemberLocked(*reloaded, base+s.reconcileCfg.StartTimeout+1)
	s.reconcileMu.Unlock()
	if !lapse.StartTimedOut {
		t.Fatalf("fixture is blind: the START never lapsed (%+v)", lapse)
	}
	// NAMED ASSERTION (the KEPT half, tick of the failure): the member is not
	// online, so the tick that OBSERVES the lapse must leave its receipt standing
	// — a clear that does not ask whether the member came back blanks the very
	// sentence this same tick just wrote.
	stamped, _ := s.dal.GetMember("m-stuck")
	if !strings.HasPrefix(stamped.LastOpReason, wakeTimeoutReasonCode+":") {
		t.Fatalf("the member is STILL not online, so the tick that observed the lapse "+
			"must leave the wake_timeout receipt standing; got last_op=%q ok=%v reason=%q at=%v",
			stamped.LastOp, stamped.LastOpOK, stamped.LastOpReason, stamped.LastOpAt)
	}
	wantReason, wantAt := stamped.LastOpReason, stamped.LastOpAt

	// A later tick with the member STILL offline. It is not converged, so
	// nothing here is allowed to touch the receipt.
	s.reconcileMu.Lock()
	dec := s.reconcileTickMemberLocked(*stamped, base+s.reconcileCfg.StartTimeout+120)
	s.reconcileMu.Unlock()
	if strings.Contains(dec.Reason, convergedOnlineReasonT39) {
		t.Fatalf("fixture is blind: an OFFLINE member must not converge (%q / %s)",
			dec.Command, dec.Reason)
	}

	got, _ := s.dal.GetMember("m-stuck")
	if got.LastOpReason != wantReason || got.LastOpAt != wantAt ||
		got.LastOp != reconcileCmdStart || got.LastOpOK == nil || *got.LastOpOK {
		t.Fatalf("the member is STILL not online, so its failure receipt must survive "+
			"verbatim; want op=%s ok=false reason=%q at=%v, got op=%q ok=%v reason=%q at=%v",
			reconcileCmdStart, wantReason, wantAt,
			got.LastOp, got.LastOpOK, got.LastOpReason, got.LastOpAt)
	}
}

// ── outsource worker ────────────────────────────────────────────────────────

// M-① sentinel (outsource). The worker arm is a SEPARATE function on a separate
// row: the staff fix does not reach it.
func TestWorkerLiveness_ConvergedOnlineWorkerClearsStaleFailureReceipt_T39(t *testing.T) {
	s := newWorkerTestServer(t)
	connectWarden(t, s, ServerSelfHost)

	now := 3_000_000.0
	w := fsmWorkerFixture(t, s, "ow-back", WorkerStatusAssigned, now-500)
	no := false
	w.LastOp = reconcileCmdStart
	w.LastOpOK = &no
	w.LastOpLog = "boot.log: claude exited 1"
	w.LastOpReason = spawnReasonWakeTimeout + ": the start window elapsed with no session"
	w.LastOpAt = now - 400
	putWorkerFixture(t, s, w)
	connectOnline(t, s, "ow-back") // the worker's own session came back

	s.outsourceMu.Lock()
	s.reconcileWorkerLiveness(w, now)
	s.outsourceMu.Unlock()

	got, err := s.dal.GetOutsourceWorker("ow-back")
	if err != nil || got == nil {
		t.Fatalf("re-read worker: %v", err)
	}
	assertPanelBlockHidden(t, "the worker is back online, so the failed-op receipt must be gone",
		got.LastOp, got.LastOpAt, got.LastOpOK, got.LastOpLog, got.LastOpReason)
}

// M-② sentinel (outsource).
func TestWorkerLiveness_StillOfflineWorkerKeepsItsFailureReceipt_T39(t *testing.T) {
	s := newWorkerTestServer(t)
	connectWarden(t, s, ServerSelfHost)

	base := 4_000_000.0
	w := fsmWorkerFixture(t, s, "ow-stuck", WorkerStatusAssigned, base-500)

	s.outsourceMu.Lock()
	s.reconcileWorkerLiveness(w, base)
	s.outsourceMu.Unlock()
	if len(s.hub.DrainWardenCommands(ServerSelfHost)) != 1 {
		t.Fatal("fixture is blind: the first tick must dispatch a START")
	}
	s.outsourceMu.Lock()
	s.reconcileWorkerLiveness(w, base+WakingTTLSecs+1)
	s.outsourceMu.Unlock()

	// NAMED ASSERTION (the KEPT half, tick of the failure) — the worker twin.
	stamped, _ := s.dal.GetOutsourceWorker("ow-stuck")
	if stamped == nil || !strings.HasPrefix(stamped.LastOpReason, spawnReasonWakeTimeout+":") {
		t.Fatalf("the worker is STILL not online, so the tick that observed the lapse "+
			"must leave the wake_timeout receipt standing; got %+v", stamped)
	}
	wantReason, wantAt := stamped.LastOpReason, stamped.LastOpAt

	// Still offline on a later tick: the receipt is the only record of the boot
	// that failed and it must still be on screen.
	s.outsourceMu.Lock()
	s.reconcileWorkerLiveness(*stamped, base+WakingTTLSecs+120)
	s.outsourceMu.Unlock()

	got, _ := s.dal.GetOutsourceWorker("ow-stuck")
	if got.LastOpReason != wantReason || got.LastOpAt != wantAt ||
		got.LastOp != reconcileCmdStart || got.LastOpOK == nil || *got.LastOpOK {
		t.Fatalf("the worker is STILL not online, so its failure receipt must survive "+
			"verbatim; want op=%s ok=false reason=%q at=%v, got op=%q ok=%v reason=%q at=%v",
			reconcileCmdStart, wantReason, wantAt,
			got.LastOp, got.LastOpOK, got.LastOpReason, got.LastOpAt)
	}
}
