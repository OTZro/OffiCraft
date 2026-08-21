package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strconv"
	"strings"
	"testing"
)

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

// 🔴 …AND THE CLOSURE IS CHECKED BY MACHINE, because "I read the constants and
// they are all in the list" is precisely the step that failed. everyWindDownCause
// shipped missing refocusOpContextNotice — the value this ticket itself added —
// and a hand-written list can only ever lag the declarations it claims to close
// over. This test re-derives the declarations from the package source and fails
// on the DIFFERENCE, so adding a constant without adding its value is a red
// build rather than a silent hole in two other tests.
//
// The prefixes are the naming convention the three const blocks already use
// (refocusOp* in member_ownerop_winddown.go, memberOp* beside it, ownerOp* in
// worker_spawn.go). A cause added under a FOURTH prefix would still slip past —
// so the failure message names the convention, which is the thing to keep.
func TestWindDownKind_TheClosedSetIsActuallyClosed(t *testing.T) {
	const prefixes = "refocusOp, memberOp, ownerOp"

	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, ".", func(fi os.FileInfo) bool {
		return !strings.HasSuffix(fi.Name(), "_test.go")
	}, 0)
	if err != nil {
		t.Fatalf("parse package source: %v", err)
	}

	declared := map[string]string{} // wire value -> the constant that declares it
	for _, pkg := range pkgs {
		for _, f := range pkg.Files {
			for _, d := range f.Decls {
				gd, ok := d.(*ast.GenDecl)
				if !ok || gd.Tok != token.CONST {
					continue
				}
				for _, spec := range gd.Specs {
					vs, ok := spec.(*ast.ValueSpec)
					if !ok {
						continue
					}
					for i, name := range vs.Names {
						if !strings.HasPrefix(name.Name, "refocusOp") &&
							!strings.HasPrefix(name.Name, "memberOp") &&
							!strings.HasPrefix(name.Name, "ownerOp") {
							continue
						}
						if i >= len(vs.Values) {
							continue
						}
						lit, ok := vs.Values[i].(*ast.BasicLit)
						if !ok || lit.Kind != token.STRING {
							continue
						}
						v, err := strconv.Unquote(lit.Value)
						if err != nil || v == "" {
							continue
						}
						declared[v] = name.Name
					}
				}
			}
		}
	}
	if len(declared) == 0 {
		t.Fatal("found no wind-down cause constants at all — the naming convention " +
			"this test keys on (" + prefixes + ") has moved, and the closure check " +
			"is silently checking nothing")
	}

	covered := map[string]bool{}
	for _, op := range everyWindDownCause {
		covered[op] = true
	}
	for value, constName := range declared {
		if !covered[value] {
			t.Errorf("everyWindDownCause is missing %s = %q. It calls itself the "+
				"closed set of refocus_op values, and a value it omits is a value "+
				"BOTH guards in this file are blind to — that is how a mutant giving "+
				"the first context threshold an undeclared 120 s deadline passed "+
				"them. Add the value in the same edit that adds the constant.",
				constName, value)
		}
	}
}
