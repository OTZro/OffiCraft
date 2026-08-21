package main

import "testing"

// winddown_kind_single_source_ted79_test.go — the clock and the sentence are
// ONE judgement, asserted through the two readers rather than through the
// judgement function, so it stays red whichever half somebody edits alone.
//
// The two halves are separately reachable, and each half alone is a distinct
// harm:
//
//   - a clock with no sentence: the agent is told to work its sequence with no
//     countdown, and is cut off mid-hand-off with no warning at all;
//   - a sentence with no clock: the agent is told it has until an instant that
//     nothing is counting, so it cuts a hand-off short to beat a deadline that
//     does not exist.
//
// Both were reachable while recycleGraceFor and offboardKindOf each carried
// their own copy of the list (T-ed79).

// everyWindDownCause is the closed set of refocus_op values plus one value no
// constant names. The unknown op is the load-bearing case for "final is the
// POSITIVE condition": under the old fallthrough it would have been clocked,
// which is how a cause nobody ruled on ends up carrying a deadline.
//
// 🔴 This list only guards what it NAMES. It shipped missing
// refocusOpContextNotice — the one value T-ed79 itself added — and with that
// hole a mutant that gave the FIRST context threshold an unannounced 120 s
// deadline (`if refocusOp == refocusOpContextNotice { return cfg.RecycleGrace,
// true }` at the top of recycleGraceFor) passed BOTH tests below. A closed set
// that lags the constants it is closing over is not a guard. Every constant in
// member_ownerop_winddown.go's two const blocks belongs here; add the value in
// the same edit that adds the constant. ownerOpRestart ("restart") is in for a
// different reason: it is the ONE verb ownerOpRevivesStoppedWorker deny-lists
// out of the wind-down, so it never lands in refocus_op today — but that
// deny-list is documented as "a verb added later gets the wind-down by
// default", so the day it stops being denied the ruling must already be pinned.
var everyWindDownCause = []string{
	refocusOpRefocus,
	refocusOpRestartSelf,
	refocusOpContextHigh,
	refocusOpContextNotice,
	memberOpRelocate,
	memberOpModel,
	ownerOpRestart,
	"an_op_no_constant_names",
}

func TestWindDownKind_TheClockAndTheSentenceCannotDisagree(t *testing.T) {
	cfg := defaultReconcileConfig()

	for _, op := range everyWindDownCause {
		m := testAgent("m-ed79-kind")
		m.RefocusSince = 1_000_000.0
		m.RefocusOp = op

		kind, carries := offboardKindOf(m, m.RefocusSince+1)
		if !carries {
			t.Fatalf("%s: a member with a refocus epoch must carry a notice at all", op)
		}
		grace, clocked := recycleGraceFor(op, cfg)
		sentenceSaysFinal := kind == offboardKindFinal

		if sentenceSaysFinal != clocked {
			t.Fatalf("%s: the sentence says kind=%q and the clock says clocked=%v — "+
				"one of them is lying to the agent", op, kind, clocked)
		}
		// The wire field the cockpit renders comes off the same judgement: a
		// countdown on screen that the reconcile tick has no intention of
		// honouring is the same split seen from the owner's side.
		deadline := refocusDeadlineOf(m.RefocusSince, cfg, op)
		if (deadline > 0) != clocked {
			t.Fatalf("%s: clocked=%v but refocus_deadline=%v", op, clocked, deadline)
		}
		if clocked && deadline != m.RefocusSince+grace {
			t.Fatalf("%s: deadline %v is not the grace the tick collects on (%v)",
				op, deadline, m.RefocusSince+grace)
		}
	}
}

// The ruling itself, stated as membership: 加速停止 is the second context
// threshold and nothing else. Every other cause — the owner's buttons, the
// agent's own restart_self, the first context threshold — is a plain 停止.
func TestWindDownKind_OnlyTheSecondContextThresholdIsAccelerated(t *testing.T) {
	for _, op := range everyWindDownCause {
		kind, clocked := winddownKindFor(op)
		wantFinal := op == refocusOpContextHigh
		if (kind == offboardKindFinal) != wantFinal || clocked != wantFinal {
			t.Fatalf("%s: got kind=%q clocked=%v, want final=%v", op, kind, clocked, wantFinal)
		}
	}
}
