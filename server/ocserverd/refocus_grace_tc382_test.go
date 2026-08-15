package main

// refocus_grace_tc382_test.go — T-c382 guard: the HANDOVER grace is its own
// window, LONGER than the stop grace.
//
// Why this file exists at all. Every pre-existing test of the recycle arm is
// written against the SYMBOL (`cfg.RecycleGrace`, `StoppingTimeoutSecs`), so it
// stays green no matter what the symbol resolves to — including after someone
// folds the two constants back into one. The whole point of the change is a
// NUMBER and a RELATION, and nothing was measuring either. These tests measure
// them, in absolute seconds, on purpose.
//
// Mutants this file is built to kill (each verified to turn it red):
//   - `RecycleGrace: RefocusGraceSecs` → `RecycleGrace: StoppingTimeoutSecs`
//     (the merge-back).
//   - `RefocusGraceSecs = 300.0` → `= 120.0` (the value).
//   - dropping the deadline compare so the recycle NEVER force-stops — the
//     negative control below catches that "we lengthened it" and "we killed the
//     handover outright" are otherwise indistinguishable from a green suite.

import "testing"

// TestRefocusGrace_IsItsOwnLongerWindow pins the SPLIT. A stop abandons the
// work; a handover has to hand it over (baton, learnings merge, in-flight state
// onto the tickets). Sharing one constant made the second window as short as
// the first, which was measured too short on 2026-08-16 (120s: the baton alone
// took ~70s and the learnings merge did not fit).
func TestRefocusGrace_IsItsOwnLongerWindow(t *testing.T) {
	if RefocusGraceSecs <= StoppingTimeoutSecs {
		t.Fatalf("the handover grace must be STRICTLY longer than the stop grace "+
			"(they were one constant before T-c382): refocus=%.0fs stop=%.0fs",
			RefocusGraceSecs, StoppingTimeoutSecs)
	}
	if StoppingTimeoutSecs != 120.0 {
		t.Fatalf("lengthening the handover window must not drag the ORDINARY stop "+
			"window along with it: stop grace is %.0fs, want 120s", StoppingTimeoutSecs)
	}
	if RefocusGraceSecs != 300.0 {
		t.Fatalf("handover grace is %.0fs, want the 300s the owner ruled on "+
			"(rc-0ee2f2ae52bd, 「例如 5 分鐘」)", RefocusGraceSecs)
	}
}

// TestReconcileRecycle_HonoursTheLongerGrace walks the member recycle arm across
// the two deadlines in ABSOLUTE seconds. The old 120s mark must now be inside
// the window (wait), and the new 300s mark must force-stop.
func TestReconcileRecycle_HonoursTheLongerGrace(t *testing.T) {
	cfg := defaultReconcileConfig()
	const refocusAt = 1000.0

	// At the OLD deadline the agent must still be writing, not dead.
	obs := obsOf("m", DesiredStateOnline, true)
	obs.RefocusSince = refocusAt
	if d := reconcileDecide(obs, newReconcileState(), cfg, refocusAt+120.0); d.Command != reconcileCmdNone {
		t.Fatalf("120s into a handover the agent must still have its window "+
			"(this is the whole ticket): %+v", d)
	}

	// One tick short of the new deadline: still waiting.
	obs2 := obsOf("m", DesiredStateOnline, true)
	obs2.RefocusSince = refocusAt
	if d := reconcileDecide(obs2, newReconcileState(), cfg, refocusAt+299.0); d.Command != reconcileCmdNone {
		t.Fatalf("inside the handover grace the arm must wait: %+v", d)
	}

	// NEGATIVE CONTROL — the handover still HAPPENS. Without this, "we gave the
	// agent more room" and "we broke the force-stop so it never fires" both read
	// as a green suite.
	obs3 := obsOf("m", DesiredStateOnline, true)
	obs3.RefocusSince = refocusAt
	d := reconcileDecide(obs3, newReconcileState(), cfg, refocusAt+300.0)
	if d.Command != reconcileCmdStop {
		t.Fatalf("a stuck dump must STILL be force-stopped at the deadline — "+
			"lengthening the grace must not disable the recycle: %+v", d)
	}

	// And an agent that finishes early is collected the INSTANT it says so: the
	// grace is a ceiling, not a duration. A longer ceiling must not slow down the
	// common case.
	obs4 := obsOf("m", DesiredStateOnline, true)
	obs4.RefocusSince = refocusAt
	obs4.AgentStopped = true
	if d := reconcileDecide(obs4, newReconcileState(), cfg, refocusAt+1.0); d.Command != reconcileCmdStop {
		t.Fatalf("report_stopped must collect immediately regardless of the "+
			"ceiling: %+v", d)
	}
}

// TestStopGrace_UnchangedByTheHandoverSplit is the other half of the negative
// control: the ordinary owner-ordered stop path reads its own constant and must
// not have been lengthened along the way.
func TestStopGrace_UnchangedByTheHandoverSplit(t *testing.T) {
	cfg := defaultReconcileConfig()
	if cfg.StopGrace != StoppingTimeoutSecs {
		t.Fatalf("stop_grace must stay on the stop constant: %.0fs vs %.0fs",
			cfg.StopGrace, StoppingTimeoutSecs)
	}
	if cfg.RecycleGrace != RefocusGraceSecs {
		t.Fatalf("recycle_grace must read the HANDOVER constant, not the stop one "+
			"(the T-c382 merge-back mutant): %.0fs vs %.0fs",
			cfg.RecycleGrace, RefocusGraceSecs)
	}

	// A member ordered to stop still gets exactly its 120s, measured absolutely.
	stopping := Member{ID: "m", DesiredState: DesiredStateOffline, StoppingSince: 1000}
	if StoppingTimedOut(stopping, 1000.0+119.0, true) {
		t.Fatal("a stopping member must keep its full 120s window")
	}
	if !StoppingTimedOut(stopping, 1000.0+121.0, true) {
		t.Fatal("the ordinary stop timeout must still fire at 120s — the handover " +
			"split must not have dragged it out to 300s")
	}
}
