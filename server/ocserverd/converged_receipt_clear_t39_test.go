package main

import (
	"strings"
	"testing"
)

// T-39 — A FAILURE RECEIPT MUST NOT OUTLIVE THE FAILURE IT DESCRIBES.
//
// The cockpit's 「最近操作」 block renders on `lastOp !== "" && lastOpAt != null`
// (AgentDetailPanel.tsx) — no presence check, no age ceiling. So a red
// "上一次操作失敗" line, once written, stands forever: EVERY clearing arm in the
// server (stampWakeObservability's landed-START path, and worker_spawn.go's
// clearWorkerPlacementBlock) is gated on a fresh dispatch — `Command == start` —
// and a member that has CONVERGED decides `Command == none`. Neither of them
// could clear a warden receipt at all. "Cleared" and "healthy again" were mutually exclusive — to
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

	// 🔴 A SEQUENCE, NOT ONE TICK. Picking a single "later" offset is a coin
	// flip: the retry cycle passes through ticks that re-dispatch a START, and on
	// those the whole-row write-back happens to restore a receipt a broken clear
	// had just blanked — so a single-tick fixture can report GREEN for a clear
	// that is in fact destroying the row on every other tick. This walks the
	// back-off, the re-dispatch and the quiet ticks after it, and requires the
	// receipt to be byte-identical at every one of them.
	for _, off := range []float64{5, 15, 30, 60, 120, 300} {
		at := base + s.reconcileCfg.StartTimeout + off
		cur, _ := s.dal.GetMember("m-stuck")
		s.reconcileMu.Lock()
		dec := s.reconcileTickMemberLocked(*cur, at)
		s.reconcileMu.Unlock()
		if strings.Contains(dec.Reason, convergedOnlineReasonT39) {
			t.Fatalf("fixture is blind at +%v: an OFFLINE member must not converge (%q / %s)",
				off, dec.Command, dec.Reason)
		}
		got, _ := s.dal.GetMember("m-stuck")
		// THE INVARIANT: the block never leaves the screen while the member is
		// down. Not "the same bytes forever" — a SECOND lapse legitimately
		// re-stamps a fresh receipt with a new timestamp, and that is the
		// production behaviour, not this change. What must never happen is the row
		// going quiet.
		if !receiptRendersAsFailure(got.LastOp, got.LastOpAt, got.LastOpOK) {
			t.Fatalf("the member is STILL not online, so the cockpit must still be "+
				"showing a failed 最近操作; at tick +%v got op=%q ok=%v at=%v reason=%q",
				off, got.LastOp, got.LastOpOK, got.LastOpAt, got.LastOpReason)
		}
		if got.LastOpReason == "" {
			t.Fatalf("nor may it go wordless; at tick +%v the reason is empty", off)
		}
		// And for as long as nothing NEW has happened, it is the original receipt
		// untouched — byte for byte, timestamp included.
		if off <= 60 && (got.LastOpReason != wantReason || got.LastOpAt != wantAt) {
			t.Fatalf("with no new failure since, the ORIGINAL receipt must be untouched; "+
				"at tick +%v want reason=%q at=%v, got reason=%q at=%v",
				off, wantReason, wantAt, got.LastOpReason, got.LastOpAt)
		}
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

	// A SEQUENCE, for the reason the staff twin explains: one tick can be green
	// by luck of where it lands in the retry cycle.
	for _, off := range []float64{5, 30, 120, 300} {
		cur, _ := s.dal.GetOutsourceWorker("ow-stuck")
		s.outsourceMu.Lock()
		s.reconcileWorkerLiveness(*cur, base+WakingTTLSecs+off)
		s.outsourceMu.Unlock()
		got, _ := s.dal.GetOutsourceWorker("ow-stuck")
		if !receiptRendersAsFailure(got.LastOp, got.LastOpAt, got.LastOpOK) {
			t.Fatalf("the worker is STILL not online, so the cockpit must still be "+
				"showing a failed 最近操作; at tick +%v got op=%q ok=%v at=%v reason=%q",
				off, got.LastOp, got.LastOpOK, got.LastOpAt, got.LastOpReason)
		}
		if got.LastOpReason == "" {
			t.Fatalf("nor may it go wordless; at tick +%v the reason is empty", off)
		}
		if off <= 30 && (got.LastOpReason != wantReason || got.LastOpAt != wantAt) {
			t.Fatalf("with no new failure since, the ORIGINAL receipt must be untouched; "+
				"at tick +%v want reason=%q at=%v, got reason=%q at=%v",
				off, wantReason, wantAt, got.LastOpReason, got.LastOpAt)
		}
	}
}

// ── the WORDLESS red block ───────────────────────────────────────────────────
//
// 🔴 `last_op_ok = nil` IS NOT "no receipt". The panel's verdict arm is
// `vm.lastOpOk ? "ok" : "fail"` (AgentDetailPanel.tsx) — nil takes the ✗ branch —
// and the block itself is shown on `lastOp !== "" && lastOpAt != null`, which
// says nothing about ok. So a row carrying last_op + last_op_at with ok = nil is
// painted EXACTLY as red as a false one, with no sentence in it. A gate written
// as "last_op_ok is a non-nil false" therefore does not cover what the owner
// pointed at, which was the line on the screen.
//
// These two tests are the sentinels for that: narrow receiptRendersAsFailure back
// to `lastOpOK != nil && !*lastOpOK` and both go red.

// The staff side. The shape is hand-planted here and that is stated rather than
// hidden: no MEMBER writer produces it today (the member placement clear leaves
// last_op_ok at the false it found). It is pinned as defence in depth, because
// the panel renders it red regardless of who wrote it — and the worker twin below
// shows the server DOES write this shape on the row type that has a producer.
func TestReconcile_ConvergedOnlineClearsTheWordlessRedBlock_T39(t *testing.T) {
	s := newReconcileTestServer(t)
	putWarden(t, s, "mach-live")
	connectOnline(t, s, "mach-live")

	stampedAt := 5_000_000.0
	m := testAgent("m-wordless")
	m.DesiredMachineID = "mach-live"
	m.LastOp = reconcileCmdStart
	m.LastOpOK = nil // ← the whole point: the panel still paints this ✗
	m.LastOpLog = ""
	m.LastOpReason = ""
	m.LastOpAt = stampedAt
	putTestMember(t, s, m)
	connectOnline(t, s, "m-wordless")

	s.reconcileMu.Lock()
	dec := s.reconcileTickMemberLocked(m, stampedAt+600)
	s.reconcileMu.Unlock()
	if dec.Command != reconcileCmdNone || !strings.Contains(dec.Reason, convergedOnlineReasonT39) {
		t.Fatalf("fixture is blind: this tick did not converge (%q / %s)", dec.Command, dec.Reason)
	}

	got, _ := s.dal.GetMember("m-wordless")
	assertPanelBlockHidden(t, "a WORDLESS red block (last_op_ok = nil) is still a red "+
		"block on screen and must be cleared when the member comes back",
		got.LastOp, got.LastOpAt, got.LastOpOK, got.LastOpLog, got.LastOpReason)
}

// The outsource side, and here the shape is PRODUCED BY THE SERVER rather than
// planted: clearWorkerPlacementBlock deliberately writes last_op_ok back to nil
// while leaving last_op = start and last_op_at standing (worker_spawn.go). So a
// worker that was placement-blocked and then dispatched carries a wordless red
// block for as long as it lives, and before T-39 nothing ever took it off.
func TestWorkerLiveness_ConvergedOnlineClearsTheWordlessRedBlock_T39(t *testing.T) {
	s := newWorkerTestServer(t)
	connectWarden(t, s, ServerSelfHost)

	base := 6_000_000.0
	w := fsmWorkerFixture(t, s, "ow-wordless", WorkerStatusAssigned, base-500)
	s.stampWorkerPlacementBlocked(&w, placementReasonUnavailable+
		": machine 'gone' is not an active machine — choose another one", base-400)

	// The dispatch runs the PRODUCTION clear, which is what mints the shape.
	blocked, _ := s.dal.GetOutsourceWorker("ow-wordless")
	s.outsourceMu.Lock()
	s.reconcileWorkerLiveness(*blocked, base)
	s.outsourceMu.Unlock()
	if len(s.hub.DrainWardenCommands(ServerSelfHost)) != 1 {
		t.Fatal("fixture is blind: the tick must dispatch a START")
	}
	planted, _ := s.dal.GetOutsourceWorker("ow-wordless")
	if planted.LastOp != reconcileCmdStart || planted.LastOpOK != nil || planted.LastOpAt <= 0 {
		t.Fatalf("fixture is blind: clearWorkerPlacementBlock no longer mints the "+
			"wordless block; got last_op=%q ok=%v at=%v",
			planted.LastOp, planted.LastOpOK, planted.LastOpAt)
	}

	connectOnline(t, s, "ow-wordless") // the worker booted and holds its own SSE
	s.outsourceMu.Lock()
	s.reconcileWorkerLiveness(*planted, base+60)
	s.outsourceMu.Unlock()

	got, _ := s.dal.GetOutsourceWorker("ow-wordless")
	assertPanelBlockHidden(t, "a WORDLESS red block (last_op_ok = nil, written by "+
		"clearWorkerPlacementBlock itself) must be cleared when the worker comes back",
		got.LastOp, got.LastOpAt, got.LastOpOK, got.LastOpLog, got.LastOpReason)
}

// ── the receipt is also ZOMBIE FUEL, and clearing it is the point ────────────
//
// last_op is not only rendered — decideUp READS it (obs.LastOpKind +
// obs.LastOpReason) to decide that a START bounced off the warden clobber-guard
// and that the slot is squatted by a presence-deaf zombie, which dispatches a
// robust STOP that kills whatever session is on that machine. So removing the
// receipt removes an INPUT to a destructive decision, and that is a claim this
// change owes a guard rather than a paragraph.
//
// The direction is the safe one and this test is what says so: what is removed is
// STALE fuel. A member that came back proved the slot was not wedged; if it later
// goes down again, the takeover arm must be reached only by a FRESH clobber
// receipt from a NEW dispatch, never by the one left over from before it
// recovered. Without the clear, that leftover sentence sat on the row forever and
// the very first offline tick could act on it.
func TestReconcile_ConvergedClearRemovesStaleZombieFuel_T39(t *testing.T) {
	s := newReconcileTestServer(t)
	putWarden(t, s, "mach-live")
	connectOnline(t, s, "mach-live")

	base := 7_000_000.0
	no := false
	m := testAgent("m-ghost")
	m.DesiredMachineID = "mach-live"
	m.LastOp = reconcileCmdStart
	m.LastOpOK = &no
	m.LastOpReason = spawnClobberReasonPrefix + ": live session refused clobber"
	m.LastOpAt = base
	putTestMember(t, s, m)

	// The member comes back on its own: the receipt goes, and with it the fuel.
	agent := connectOnline(t, s, "m-ghost")
	s.reconcileMu.Lock()
	conv := s.reconcileTickMemberLocked(m, base+60)
	s.reconcileMu.Unlock()
	if !strings.Contains(conv.Reason, convergedOnlineReasonT39) {
		t.Fatalf("fixture is blind: this tick did not converge (%q / %s)", conv.Command, conv.Reason)
	}
	recovered, _ := s.dal.GetMember("m-ghost")
	// Errorf, not Fatalf: the run must CONTINUE to the consequence below, so a
	// regression reports both "the fuel is still there" and "and here is what it
	// makes the server do" rather than only the first.
	if strings.HasPrefix(recovered.LastOpReason, spawnClobberReasonPrefix) {
		t.Errorf("the clobber receipt must go with the rest of the block, got %q",
			recovered.LastOpReason)
	}

	// It falls over again, and the FSM is put in the one state where the takeover
	// arm is genuinely reachable: a START outstanding, offline long enough to be
	// past the second-confirm grace. That is deliberate — without it this half of
	// the test proves nothing, because the converged tick ALSO resets
	// st.LastCommand to none and that alone would keep the arm out of reach. The
	// point is to leave the RECEIPT as the only remaining input, so what the
	// assertions below turn on is the thing this change actually removes.
	s.hub.Disconnect(agent)
	s.reconcileMu.Lock()
	st := s.reconcileStates["m-ghost"]
	st.Phase = reconcilePhaseStarting
	st.LastCommand = reconcileCmdStart
	st.LastCommandAt = base + 61
	st.OfflineSince = base + 61 - s.reconcileCfg.ZombieConfirmGrace - 1
	s.reconcileStates["m-ghost"] = st
	down := s.reconcileTickMemberLocked(*recovered, base+120)
	s.reconcileMu.Unlock()

	if down.Command == reconcileCmdStop || down.StopKind == stopKindZombieTakeover {
		t.Fatalf("a member that RECOVERED and then went down again must not be "+
			"zombie-reaped on a clobber receipt from before it recovered; got "+
			"command=%q stop_kind=%q (%s)", down.Command, down.StopKind, down.Reason)
	}
	if down.ReasonCode != "" && strings.HasPrefix(down.ReasonCode, spawnReasonZombieSuspect) {
		t.Fatalf("nor may it be held in the zombie-suspect grace on stale fuel; got %q",
			down.ReasonCode)
	}
	if down.Command == reconcileCmdStart && down.StopKind != "" {
		t.Fatalf("a fresh START must not carry a stop kind; got %q", down.StopKind)
	}
}

// ── the ruling is made on the RE-READ row, not on the tick snapshot ──────────
//
// 🔴 THIS IS THE DESTRUCTIVE FAILURE MODE, not merely a stale one. Both clears
// are whole-row writes, and the HTTP faces (activate / relocate / deactivate,
// and the warden receipt fold) write these rows WITHOUT holding the reconcile
// lock — which is the hazard every other stamp in these two files re-reads for.
// A clear that decides from the tick's snapshot and then blanks whatever it
// finds would delete a receipt written microseconds earlier by an owner action.
//
// The fixtures below are the seam that makes that observable without a race: the
// tick entry points take the row BY VALUE, so handing them a snapshot that
// disagrees with the database is exactly "the row moved after the tick loaded
// it". The stored row carries a SUCCESS receipt — which must never be cleared
// under any circumstances — while the snapshot claims a failure.

func TestReconcile_ConvergedClearRulesOnTheFreshRowNotTheSnapshot_T39(t *testing.T) {
	s := newReconcileTestServer(t)
	putWarden(t, s, "mach-live")
	connectOnline(t, s, "mach-live")

	at := 8_000_000.0
	yes := true
	stored := testAgent("m-race")
	stored.DesiredMachineID = "mach-live"
	stored.LastOp = reconcileCmdStart
	stored.LastOpOK = &yes // the row as it stands NOW: a success
	stored.LastOpLog = "boot.log: ready"
	stored.LastOpReason = "started cleanly"
	stored.LastOpAt = at
	putTestMember(t, s, stored)
	connectOnline(t, s, "m-race")

	// What the tick loaded a moment earlier: the FAILURE that has since been
	// superseded on the row.
	no := false
	stale := stored
	stale.LastOpOK = &no
	stale.LastOpReason = "wake_timeout: superseded"

	s.reconcileMu.Lock()
	s.reconcileTickMemberLocked(stale, at+600)
	s.reconcileMu.Unlock()

	got, _ := s.dal.GetMember("m-race")
	if got.LastOpOK == nil || !*got.LastOpOK || got.LastOpReason != "started cleanly" ||
		got.LastOpAt != at || got.LastOp != reconcileCmdStart {
		t.Fatalf("the clear must rule on the RE-READ row: a receipt written after the "+
			"tick loaded its snapshot must not be destroyed (and a SUCCESS receipt "+
			"never at all); want ok=true reason=%q at=%v, got op=%q ok=%v reason=%q at=%v",
			"started cleanly", at, got.LastOp, got.LastOpOK, got.LastOpReason, got.LastOpAt)
	}
}

func TestWorkerLiveness_ConvergedClearRulesOnTheFreshRowNotTheSnapshot_T39(t *testing.T) {
	s := newWorkerTestServer(t)
	connectWarden(t, s, ServerSelfHost)

	at := 9_000_000.0
	yes := true
	w := fsmWorkerFixture(t, s, "ow-race", WorkerStatusAssigned, at-500)
	w.LastOp = reconcileCmdStart
	w.LastOpOK = &yes
	w.LastOpLog = "boot.log: ready"
	w.LastOpReason = "started cleanly"
	w.LastOpAt = at
	putWorkerFixture(t, s, w)
	connectOnline(t, s, "ow-race")

	no := false
	stale := w
	stale.LastOpOK = &no
	stale.LastOpReason = spawnReasonWakeTimeout + ": superseded"

	s.outsourceMu.Lock()
	s.reconcileWorkerLiveness(stale, at+600)
	s.outsourceMu.Unlock()

	got, _ := s.dal.GetOutsourceWorker("ow-race")
	if got.LastOpOK == nil || !*got.LastOpOK || got.LastOpReason != "started cleanly" ||
		got.LastOpAt != at || got.LastOp != reconcileCmdStart {
		t.Fatalf("the clear must rule on the RE-READ row: a receipt written after the "+
			"tick loaded its snapshot must not be destroyed (and a SUCCESS receipt "+
			"never at all); want ok=true reason=%q at=%v, got op=%q ok=%v reason=%q at=%v",
			"started cleanly", at, got.LastOp, got.LastOpOK, got.LastOpReason, got.LastOpAt)
	}
}
