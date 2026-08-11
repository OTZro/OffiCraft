// T-b9f6 — the bind-time re-read is the freeze-race's ONLY defence, so it gets
// its own guard.
//
// Where this came from: T-b9f6 removed the "a frozen task may not be reassigned"
// 400, and the whole safety argument for removing it is "the scheduler refuses
// to wake anybody for a frozen task". An independent review then measured where
// that refusal actually lives and found the implementer's mental model wrong:
//
//   - `outsourceDecide`'s frozen `continue` is UNREACHABLE in production — its
//     only caller feeds it candidates that already passed
//     `outsourceAwaitingAssignment`, so it can never see a frozen one. Writing a
//     behavioural test for it would be writing a test for dead code.
//   - `outsourceAwaitingAssignment` has TWO call sites in `runOutsourceTick`:
//     the collection sweep, and a RE-READ just before the worker is bound. The
//     re-read is not a duplicate of the sweep: the sweep judges a SNAPSHOT, and
//     a handler write (freeze, terminate) can land between the snapshot and the
//     bind. For that window the re-read is the ONLY thing standing between
//     "owner froze it a moment ago" and "a worker is bound to it anyway".
//
// 🔴 Why this test is STRUCTURAL and not behavioural. To reach the window
// behaviourally the test would have to freeze the row BETWEEN `ListTasks` and
// `GetTask` inside one tick; `runOutsourceTick` holds a concrete `*DAL` with no
// seam to interpose on, so the only way to "make it fail" would be a timing race
// — which is exactly the shape this repo has recorded as unreliable (a 60-round
// concurrency test that stayed green under its mutant while a structural
// assertion caught it). So this asserts, on the PARSED source, that the re-read
// and its verdict are still there and still sit before the bind. Deleting the
// re-read check would otherwise be a SILENT drift: every behavioural test stays
// green, because they all run without a racing writer.
//
// ⚠️ What this test does NOT prove: that the re-read is correct, or that the
// window is actually closed under real concurrency. It proves the guard has not
// been deleted or moved after the bind. Read it as a tripwire, not a proof.

package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"
)

func TestOutsourceTick_RereadsAndRejudgesBeforeBinding(t *testing.T) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "outsource_sched.go", nil, 0)
	if err != nil {
		t.Fatalf("parse outsource_sched.go: %v", err)
	}

	var tick *ast.FuncDecl
	for _, d := range file.Decls {
		fn, ok := d.(*ast.FuncDecl)
		if ok && fn.Name.Name == "runOutsourceTick" {
			tick = fn
		}
	}
	if tick == nil {
		t.Fatal("runOutsourceTick not found — this guard's corpus is empty, so a " +
			"green result here would prove nothing (the function was renamed?)")
	}

	// Positions of the three things, in source order. 0 = not seen.
	var decidePos, rereadPos, verdictPos, bindPos token.Pos
	ast.Inspect(tick, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		switch fn := call.Fun.(type) {
		case *ast.Ident:
			if fn.Name == "outsourceDecide" && decidePos == 0 {
				decidePos = call.Pos()
			}
			// The verdict must be re-taken AFTER the decide, on the re-read row.
			if fn.Name == "outsourceAwaitingAssignment" && decidePos != 0 &&
				verdictPos == 0 {
				verdictPos = call.Pos()
			}
		case *ast.SelectorExpr:
			switch fn.Sel.Name {
			case "GetTask":
				if decidePos != 0 && rereadPos == 0 {
					rereadPos = call.Pos()
				}
			case "PutOutsourceWorker", "BindTaskExecutor", "PutTask":
				if decidePos != 0 && bindPos == 0 {
					bindPos = call.Pos()
				}
			}
		}
		return true
	})

	// Corpus self-check: the scan must see the landmark it keys off, otherwise
	// "everything is in order" and "the scanner is dead" look identical.
	if decidePos == 0 {
		t.Fatal("no call to outsourceDecide inside runOutsourceTick — the scan " +
			"lost its landmark; fix the scan before trusting a green run")
	}
	if rereadPos == 0 {
		t.Fatal("runOutsourceTick binds without RE-READING the task after " +
			"outsourceDecide. A freeze (or terminate) that lands between the " +
			"snapshot and the bind would then be ignored and a worker would be " +
			"bound to a task the owner just paused — the exact invariant T-b9f6 " +
			"leaned on when it removed the frozen-reassign refusal.")
	}
	if verdictPos == 0 {
		t.Fatal("the task is re-read after outsourceDecide but its verdict is " +
			"never re-taken (no outsourceAwaitingAssignment on the fresh row). " +
			"Re-reading and then not judging is worse than not re-reading: it " +
			"reads like a guard and stops nothing.")
	}
	if verdictPos < rereadPos {
		t.Fatalf("the re-verdict (pos %d) runs BEFORE the re-read (pos %d) — it "+
			"is judging the snapshot again, not the fresh row", verdictPos, rereadPos)
	}
	if bindPos != 0 && bindPos < verdictPos {
		t.Fatalf("a worker is bound (pos %d) BEFORE the re-read verdict (pos %d) "+
			"— a guard that runs after the write it is meant to prevent is not a "+
			"guard", bindPos, verdictPos)
	}
}
