package main

import "testing"

// T-14 #4 — the RELOCATION BACKSTOP now WAITS.
//
// Owner ruling (2026-08-28/29): 「refocus 怎麼做的 這邊就怎麼做」/「三種下線都是
// 同一種方案…他們都是不急著下線，等它自然交接完」. The decideUp relocation arm
// used to robust-STOP a live, mismatched member on the FIRST pass, with no
// 預告 and no chance to hand its context over — the exact behaviour T-b6d9
// removed from the relocate HANDLER and left standing here. It now opens the
// same wind-down epoch the handler opens (armMemberOwnerOpHandover), and the
// refocus arm above collects it on the agent's own stopped report.
//
// The BACKSTOP is not deleted: a member with nothing to hand over (a warden
// row, or one already on the 強制停止 rung) still dies on the first pass, which
// is what keeps this path convergent — see
// TestRelocateBackstopStillKillsWhenThereIsNothingToHandOver.

// The end-to-end shape, through the real reconcileMemberNow (hub observation +
// dispatch + persistence): a live member on the old machine, pinned to a new
// one, must NOT be killed on the first tick. It must be left running with a
// fresh relocate wind-down stamped on its row.
func TestRelocateBackstopOpensAWindDownInsteadOfKilling(t *testing.T) {
	s := newReconcileTestServer(t)
	putWarden(t, s, "mach-old")
	putWarden(t, s, "mach-new")

	m := testAgent("m-h")
	m.DesiredState = DesiredStateOnline
	m.DesiredMachineID = "mach-new"
	putTestMember(t, s, m)
	connectOnline(t, s, "mach-old")               // the old machine's warden could accept a STOP
	connectOnlineMachine(t, s, "m-h", "mach-old") // …but the agent has not handed over yet

	dec := s.reconcileMemberNow("m-h")
	if dec.Command != reconcileCmdNone {
		t.Fatalf("the relocation backstop must not kill on the first pass; got command=%s reason=%q",
			dec.Command, dec.Reason)
	}
	if dec.DispatchUnlanded {
		t.Fatalf("nothing was dispatched, so nothing can be unlanded: %+v", dec)
	}
	fresh, err := s.dal.GetMember("m-h")
	if err != nil || fresh == nil {
		t.Fatalf("re-read member: %v", err)
	}
	if fresh.RefocusSince <= 0 {
		t.Fatalf("want a refocus epoch stamped on the row so the refocus arm owns the move, "+
			"got refocus_since=%v refocus_op=%q", fresh.RefocusSince, fresh.RefocusOp)
	}
	if fresh.RefocusOp != memberOpRelocate {
		t.Fatalf("want refocus_op=%q (the cause the cockpit renders), got %q",
			memberOpRelocate, fresh.RefocusOp)
	}
	// The pin itself is untouched — this arm moves nobody, it only opens the
	// wind-down that will.
	if fresh.DesiredMachineID != "mach-new" {
		t.Fatalf("the owner's pin must survive the arm, got %q", fresh.DesiredMachineID)
	}
}

// …and the move still LANDS: once the agent files its stopped report, the
// refocus arm collects the epoch with a recycle STOP addressed to the machine
// the session is actually on. This is the half that proves "waits" did not
// become "never converges".
func TestRelocateBackstopCollectsOnTheAgentsStoppedReport(t *testing.T) {
	s := newReconcileTestServer(t)
	putWarden(t, s, "mach-old")
	putWarden(t, s, "mach-new")

	m := testAgent("m-i")
	m.DesiredState = DesiredStateOnline
	m.DesiredMachineID = "mach-new"
	putTestMember(t, s, m)
	connectOnline(t, s, "mach-old")
	connectOnlineMachine(t, s, "m-i", "mach-old")

	if dec := s.reconcileMemberNow("m-i"); dec.Command != reconcileCmdNone {
		t.Fatalf("first pass must open a wind-down, not dispatch: %+v", dec)
	}
	fresh, err := s.dal.GetMember("m-i")
	if err != nil || fresh == nil {
		t.Fatalf("re-read member: %v", err)
	}
	// The agent finishes its hand-off (POST /api/self/stopped).
	fresh.StoppedSince = nowSecs()
	putTestMember(t, s, *fresh)

	dec := s.reconcileMemberNow("m-i")
	if dec.Command != reconcileCmdStop {
		t.Fatalf("a completed hand-off must be collected with a STOP, got %s (%s)",
			dec.Command, dec.Reason)
	}
	if dec.StopKind != stopKindRecycle {
		t.Fatalf("the collection belongs to the refocus arm (%q), got %q",
			stopKindRecycle, dec.StopKind)
	}
	if dec.DispatchWarden != "mach-old" {
		t.Fatalf("the STOP must be addressed to the machine the session is ON, got %q",
			dec.DispatchWarden)
	}
}

// THE CONVERGENCE HALF. A mismatched member for which no wind-down can be
// opened has nothing to wait FOR, and a tick that asks for a stamp nobody
// writes re-decides identically every 30s forever — a live session stranded on
// the wrong machine with no path off it. So the original first-pass STOP is
// still there, reached exactly when the gates refuse.
//
// The refusal used here is the ladder rule: the member is already on the
// 強制停止 rung (forcedEpochLive), which armRefocusEpoch may not walk back down.
func TestRelocateBackstopStillKillsWhenThereIsNothingToHandOver(t *testing.T) {
	s := newReconcileTestServer(t)
	putWarden(t, s, "mach-old")
	putWarden(t, s, "mach-new")
	connectOnline(t, s, "mach-old")

	now := nowSecs()
	m := testAgent("m-j")
	m.DesiredState = DesiredStateOnline
	m.DesiredMachineID = "mach-new"
	// 強制停止 already pressed on this row → winddownStageOf == winddownStageForced,
	// so a relocate epoch (rank 停止) may not be stamped over it.
	m.StoppingSince = now
	m.ForcedStopAt = now
	putTestMember(t, s, m)
	connectOnlineMachine(t, s, "m-j", "mach-old")

	dec := s.reconcileMemberNow("m-j")
	if dec.Command != reconcileCmdStop {
		t.Fatalf("with no wind-down available the backstop must still kill on the first "+
			"pass, or the member never converges; got %s (%s)", dec.Command, dec.Reason)
	}
	if dec.StopKind != stopKindRelocate {
		t.Fatalf("the fallback belongs to the relocation arm (%q), got %q",
			stopKindRelocate, dec.StopKind)
	}
	if dec.DispatchWarden != "mach-old" {
		t.Fatalf("the STOP must still be addressed to the running machine, got %q",
			dec.DispatchWarden)
	}
	fresh, err := s.dal.GetMember("m-j")
	if err != nil || fresh == nil {
		t.Fatalf("re-read member: %v", err)
	}
	if fresh.RefocusOp == memberOpRelocate {
		t.Fatalf("no relocate epoch may be stamped on a member already on the 強制停止 rung")
	}
}

// memberOwnerOpHandoverArmable is a PROBE — it has to answer exactly what
// armMemberOwnerOpHandover would do, or the decider chooses the wrong arm. It is
// written as a call to the real gates rather than a re-listing of them; this
// pins that it stayed that way, across the inputs that separate the answers.
func TestMemberOwnerOpHandoverArmable_AgreesWithTheRealArm(t *testing.T) {
	s := newReconcileTestServer(t)
	now := nowSecs()

	live := testAgent("m-k")
	live.DesiredState = DesiredStateOnline
	putTestMember(t, s, live)
	connectOnline(t, s, "m-k")

	warden := Member{
		ID: "m-l", Name: "m-l", Kind: KindWarden,
		DesiredState: DesiredStateOnline, RosterStatus: RosterStatusActive,
	}
	putTestMember(t, s, warden)
	connectOnline(t, s, "m-l")

	forced := testAgent("m-m")
	forced.DesiredState = DesiredStateOnline
	forced.StoppingSince = now
	forced.ForcedStopAt = now
	putTestMember(t, s, forced)
	connectOnline(t, s, "m-m")

	offlineRow := testAgent("m-n") // never connected → nothing to flush
	offlineRow.DesiredState = DesiredStateOnline
	putTestMember(t, s, offlineRow)

	for _, row := range []Member{live, warden, forced, offlineRow} {
		probe := s.memberOwnerOpHandoverArmable(row, memberOpRelocate)
		real := row // armMemberOwnerOpHandover MUTATES
		got := s.armMemberOwnerOpHandover(&real, memberOpRelocate)
		if probe != got {
			t.Fatalf("%s: probe said %v, the real arm said %v — the probe has stopped "+
				"asking the same question", row.ID, probe, got)
		}
		if !probe && real.RefocusSince != row.RefocusSince {
			t.Fatalf("%s: a refused arm must leave the row alone", row.ID)
		}
	}
	// …and the probe must not have mutated anything it was shown.
	if live.RefocusSince != 0 || warden.RefocusSince != 0 {
		t.Fatalf("the probe mutated its input")
	}
}
