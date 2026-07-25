package main

import (
	"strings"
	"testing"
)

// T-ba62 — "the wake failed" and "nobody ever woke it" must stop looking the
// same.
//
// waking_since had exactly ONE writer that ever set it: the agent's own
// report_waking. So it was stamped only by agents that successfully booted. An
// agent that never came up left it at zero, PresenceState projected plain
// "offline", and the cockpit rendered a member that was actively failing to
// start identically to one nobody had ever touched. The server KNOWS the
// difference — it dispatched the START — it just never wrote it down.

// A LANDED START stamps waking_since, so the member reads "waking" for the
// WakingTTLSecs window instead of a silent "offline".
func TestReconcile_LandedStartStampsWakingSince(t *testing.T) {
	s := newReconcileTestServer(t)
	putWarden(t, s, "mach-live")
	connectOnline(t, s, "mach-live")

	m := testAgent("m-boot")
	m.DesiredMachineID = "mach-live"
	m.WakingSince = 0
	putTestMember(t, s, m)

	dec := s.reconcileMemberNow("m-boot")
	if dec.Command != reconcileCmdStart {
		t.Fatalf("expected a START decision, got %q (%s)", dec.Command, dec.Reason)
	}
	got, err := s.dal.GetMember("m-boot")
	if err != nil || got == nil {
		t.Fatalf("reload member: %v", err)
	}
	if got.WakingSince <= 0 {
		t.Fatalf("a landed START must stamp waking_since; got %v", got.WakingSince)
	}
	if p := PresenceState(*got, got.WakingSince+1, false); p != MemberPresenceWaking {
		t.Fatalf("a freshly dispatched member must project waking, got %q", p)
	}
}

// The discriminating half: an UNDISPATCHED START (no reachable warden) must NOT
// stamp waking_since. Without this, the stamp would be a lie in the exact case
// the ticket cares about — nothing was sent, so nothing is waking.
func TestReconcile_UnlandedStartDoesNotStampWakingSince(t *testing.T) {
	s := newReconcileTestServer(t)
	putWarden(t, s, "mach-dead") // in the roster, never connected

	m := testAgent("m-boot")
	m.DesiredMachineID = "mach-dead"
	m.WakingSince = 0
	putTestMember(t, s, m)

	dec := s.reconcileMemberNow("m-boot")
	if !dec.DispatchUnlanded {
		t.Fatalf("expected an unlanded dispatch; got %+v", dec)
	}
	got, _ := s.dal.GetMember("m-boot")
	if got.WakingSince != 0 {
		t.Fatalf("an UNDISPATCHED start must not claim the member is waking; got %v", got.WakingSince)
	}
}

// A START that lapses its start_timeout writes a durable last_op receipt — the
// thing the cockpit's 「最近操作」 reads. Previously the lapse existed only as
// exponential backoff inside in-memory reconcile state and one stderr line.
func TestReconcile_StartTimeoutWritesReceipt(t *testing.T) {
	s := newReconcileTestServer(t)
	putWarden(t, s, "mach-live")
	connectOnline(t, s, "mach-live")

	m := testAgent("m-boot")
	m.DesiredMachineID = "mach-live"
	putTestMember(t, s, m)

	// tick 1: dispatch the START.
	now := nowSecs()
	s.reconcileMu.Lock()
	first := s.reconcileTickMemberLocked(m, now)
	s.reconcileMu.Unlock()
	if first.Command != reconcileCmdStart {
		t.Fatalf("expected a START, got %q", first.Command)
	}
	if first.StartTimedOut {
		t.Fatalf("the dispatching tick has not timed out yet")
	}

	// tick 2, past the start window, still not online → the lapse is observed.
	reloaded, _ := s.dal.GetMember("m-boot")
	s.reconcileMu.Lock()
	second := s.reconcileTickMemberLocked(*reloaded, now+s.reconcileCfg.StartTimeout+1)
	s.reconcileMu.Unlock()
	if !second.StartTimedOut {
		t.Fatalf("a lapsed START must be reported as timed out; got %+v", second)
	}

	got, _ := s.dal.GetMember("m-boot")
	if got.LastOp != reconcileCmdStart {
		t.Fatalf("last_op must name the start, got %q", got.LastOp)
	}
	if got.LastOpOK == nil || *got.LastOpOK {
		t.Fatalf("a lapsed START must record last_op_ok=false, got %v", got.LastOpOK)
	}
	// The REASON is the assertion that matters: a receipt with no cause is the
	// same silence in a different shape.
	if !strings.HasPrefix(got.LastOpReason, "wake_timeout:") {
		t.Fatalf("the receipt must carry a wake_timeout reason, got %q", got.LastOpReason)
	}
	for _, want := range []string{"never came online", "claude"} {
		if !strings.Contains(got.LastOpReason, want) {
			t.Errorf("the reason must mention %q so it is actionable, got %q", want, got.LastOpReason)
		}
	}
}

// Positive control for the test above: a member that comes ONLINE inside the
// window must never get a wake_timeout receipt. Without this pair, a mutant that
// stamps the receipt unconditionally would still be green.
func TestReconcile_OnlineMemberGetsNoTimeoutReceipt(t *testing.T) {
	s := newReconcileTestServer(t)
	putWarden(t, s, "mach-live")
	connectOnline(t, s, "mach-live")

	m := testAgent("m-boot")
	m.DesiredMachineID = "mach-live"
	putTestMember(t, s, m)

	now := nowSecs()
	s.reconcileMu.Lock()
	s.reconcileTickMemberLocked(m, now)
	s.reconcileMu.Unlock()

	connectOnline(t, s, "m-boot") // the agent booted and holds its own SSE

	reloaded, _ := s.dal.GetMember("m-boot")
	s.reconcileMu.Lock()
	dec := s.reconcileTickMemberLocked(*reloaded, now+s.reconcileCfg.StartTimeout+1)
	s.reconcileMu.Unlock()
	if dec.StartTimedOut {
		t.Fatalf("an ONLINE member must never be reported as a timed-out start: %+v", dec)
	}
	got, _ := s.dal.GetMember("m-boot")
	if strings.HasPrefix(got.LastOpReason, "wake_timeout:") {
		t.Fatalf("an online member must not carry a wake_timeout receipt, got %q", got.LastOpReason)
	}
}

// The anti-churn guard on the placement stamp suppresses REPETITION, never news:
// blocked → the machine appears and the START lands → the machine is removed
// again → the second block carries a FRESH last_op_at. Without the clear on the
// landed-start path the second block matches the first and is swallowed, leaving
// the cockpit showing "stalled an hour ago" for a stall happening right now (the
// worker twin is TestNotifyWorkerSpawn_BlockRestampedAfterDispatch).
func TestReconcile_MemberBlockRestampedAfterLandedStart(t *testing.T) {
	s := newReconcileTestServer(t)
	m := testAgent("m-flip")
	m.DesiredMachineID = "mach-flip" // named, but no machine row yet
	putTestMember(t, s, m)

	first := 8000.0
	s.runReconcileTick(first)
	blocked, _ := s.dal.GetMember("m-flip")
	if blocked == nil || !strings.HasPrefix(blocked.LastOpReason, placementReasonUnavailable+":") ||
		blocked.LastOpAt != first {
		t.Fatalf("a pin naming no active machine must stamp a block at %v: %+v", first, blocked)
	}

	// The machine is installed and comes online: the START actually lands.
	putWarden(t, s, "mach-flip")
	connectOnline(t, s, "mach-flip")
	s.runReconcileTick(first + 30)
	frames := drainFrames(t, s, "mach-flip")
	if len(frames) != 1 || frames[0].RPC != reconcileCmdStart ||
		frames[0].Args["member_id"] != "m-flip" {
		t.Fatalf("the machine coming online must dispatch the START: %+v", frames)
	}

	// The agent boots, so the next block starts from a converged state.
	agent := connectOnline(t, s, "m-flip")
	s.runReconcileTick(first + 60)

	// The machine is removed again: the SAME cause, but after a landed START this
	// is news, not repetition.
	s.hub.Disconnect(agent)
	warden, _ := s.dal.GetMember("mach-flip")
	warden.RosterStatus = RosterStatusRemoved
	putTestMember(t, s, *warden)

	third := first + 90
	s.runReconcileTick(third)
	again, _ := s.dal.GetMember("m-flip")
	if again == nil || again.LastOpReason != blocked.LastOpReason {
		t.Fatalf("the second block must carry the same cause: %+v", again)
	}
	if again.LastOpAt != third {
		t.Fatalf("a block AFTER a landed START must re-stamp: last_op_at = %v, want %v "+
			"(keeping the first block's %v means the start never cleared it)",
			again.LastOpAt, third, blocked.LastOpAt)
	}
}
