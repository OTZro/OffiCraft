package main

import "testing"

// T-8655: relocation DISPATCH OUTCOME at the decision layer. The relocate
// handler exposes the pin immediately, but the recycle STOP that actually moves
// a LIVE member can fail to land when the old-machine warden is unreachable.
// reconcileMemberNow now RETURNS its decision so the handler can surface
// relocation_pending instead of a silent 200 success. These tests pin
// DispatchUnlanded on both edges through the real reconcileMemberNow path
// (hub-driven observation + dispatch); the HTTP-response twin lives in
// api_members_relocate_pending_test.go.
//
// 🔴 WHERE THE STOP COMES FROM CHANGED IN T-14 #4, AND THAT IS WHY BOTH TESTS
// NOW HAND OVER FIRST. Until then a live, mismatched member was robust-STOPped
// on the FIRST tick by decideUp's relocation arm, so `reconcileMemberNow` on a
// freshly-pinned member dispatched immediately and these tests could read the
// outcome off that one call. That arm no longer kills on sight: it opens a
// wind-down and the refocus arm collects it when the agent files its stopped
// report. So the first tick is now — correctly — a dispatch of NOTHING, and
// reading DispatchUnlanded off it would be reading the flag on a tick that
// never tried to deliver anything, which is exactly the false signal this
// ticket exists to remove. The assertion that matters is unchanged and still
// asserted, one tick later: the STOP that DOES go out must report honestly
// whether the old machine's warden took it.

// stagedRelocationHandover pins `memberID` to mach-new while its session runs on
// mach-old, lets the backstop open its wind-down, and then plays the agent's own
// stopped report — leaving the member exactly one tick short of the recycle STOP.
func stagedRelocationHandover(t *testing.T, s *apiServer, memberID string) {
	t.Helper()
	m := testAgent(memberID)
	m.DesiredState = DesiredStateOnline
	m.DesiredMachineID = "mach-new"
	putTestMember(t, s, m)
	connectOnlineMachine(t, s, memberID, "mach-old") // running on the OLD machine

	if dec := s.reconcileMemberNow(memberID); dec.Command != reconcileCmdNone {
		t.Fatalf("the backstop must open a wind-down before anything is dispatched, got %s (%s)",
			dec.Command, dec.Reason)
	}
	fresh, err := s.dal.GetMember(memberID)
	if err != nil || fresh == nil {
		t.Fatalf("re-read %s: %v", memberID, err)
	}
	if fresh.RefocusSince <= 0 {
		t.Fatalf("no wind-down was opened on %s — the rest of this test would be "+
			"asserting the wrong arm", memberID)
	}
	fresh.StoppedSince = nowSecs() // the agent finished its hand-off
	putTestMember(t, s, *fresh)
}

// online member running on mach-old, owner-pinned to mach-new, handed over, with
// the OLD-machine warden OFFLINE → the recycle STOP cannot be delivered → the
// decision is DispatchUnlanded and the command downgrades to none (retry next
// tick).
func TestReconcileMemberNowRelocationUnlanded(t *testing.T) {
	s := newReconcileTestServer(t)
	putWarden(t, s, "mach-new")
	// the old-machine warden is NOT connected → enqueueToWarden("mach-old") fails closed
	stagedRelocationHandover(t, s, "m-a")

	dec := s.reconcileMemberNow("m-a")
	if !dec.DispatchUnlanded {
		t.Fatalf("want DispatchUnlanded=true when the old-machine warden is offline, got false (cmd=%s)", dec.Command)
	}
	if dec.Command != reconcileCmdNone {
		t.Fatalf("an unlanded relocation STOP must downgrade to none, got %s", dec.Command)
	}
}

// same divergence but the OLD-machine warden IS online → the STOP lands, so the
// move is NOT pending and it must route to the RUNNING (old) machine's warden.
func TestReconcileMemberNowRelocationLands(t *testing.T) {
	s := newReconcileTestServer(t)
	putWarden(t, s, "mach-old")
	putWarden(t, s, "mach-new")
	connectOnline(t, s, "mach-old") // old-machine warden online → the STOP can be delivered
	stagedRelocationHandover(t, s, "m-b")

	dec := s.reconcileMemberNow("m-b")
	if dec.DispatchUnlanded {
		t.Fatalf("want DispatchUnlanded=false when the old-machine warden is reachable")
	}
	if dec.Command != reconcileCmdStop {
		t.Fatalf("want relocation STOP dispatched, got %s", dec.Command)
	}
	if dec.DispatchWarden != "mach-old" {
		t.Fatalf("relocation STOP must route to the running-machine warden mach-old, got %q", dec.DispatchWarden)
	}
}
