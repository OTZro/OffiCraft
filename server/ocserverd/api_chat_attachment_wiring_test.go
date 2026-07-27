package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"strings"
	"testing"
)

// T-e2b2 / independent review F1: the atomicity guards in dal_chat_atomic_test.go
// drive the DAL DIRECTLY, so they stay green while a handler quietly goes back
// to "write each blob, then the record" — the reviewer built exactly that mutant
// (all three handlers reverted, PutChatWithAttachments left intact but unused)
// and the whole suite, Go plus conformance, stayed green. A guard nothing calls
// is not a guard.
//
// ⚠️ THIS SCAN IS NOT THE GUARD. It pins a SPELLING, and the reviewer defeated
// it three ways while reintroducing the defect, full suite green each time:
// hoist the loop into a helper (the pre-T-e2b2 shape), write the loop inline
// against the putChatAttachmentOn / putChatOn seams (plain identifiers, not
// selectors, so this scan never sees them), or take a method value. Enumerating
// ways of writing something has no end — that is the T-5047 lesson in the task
// manual, arrived at again the hard way.
//
// The real guard is TestFailedRecordWriteOrphansNoBlob /
// TestFailedAnswerWriteOrphansNoBlob (api_chat_orphan_blob_test.go), which
// assert the OUTCOME — break the record write at the database, then ask the
// database whether a blob appeared — and catch all three of those mutants.
// This scan is kept only as a cheap early signal that names the offending line
// while the outcome guard names only the symptom. Its value is legibility, not
// coverage; never treat its green as evidence.
func TestAttachmentCarryingHandlersWriteAtomically(t *testing.T) {
	// The forbidden per-record writes: each one persists ONE row, so reaching
	// for any of them inside an attachment-carrying function reintroduces the
	// multi-write window.
	forbidden := map[string]bool{
		"PutChatAttachment": true,
		"PutChat":           true,
		"PutReplyCard":      true,
	}
	// The functions that resolve attachment inputs and must therefore persist
	// through an all-or-nothing entry point.
	watched := map[string]string{
		"HandlePostChatApiChatPost":                      "api_chat.go",
		"openReplyCard":                                  "api_replycards.go",
		"applyReplyCardAnswer":                           "api_replycards.go",
		"HandlePostTaskMessageApiTasksTaskIdMessagePost": "api_tasks.go",
	}

	fset := token.NewFileSet()
	pkg, err := parser.ParseDir(fset, ".", func(fi fs.FileInfo) bool {
		return !strings.HasSuffix(fi.Name(), "_test.go")
	}, 0)
	if err != nil {
		t.Fatalf("parse package: %v", err)
	}
	seen := map[string]bool{}
	for _, p := range pkg {
		for _, file := range p.Files {
			for _, decl := range file.Decls {
				fn, ok := decl.(*ast.FuncDecl)
				if !ok {
					continue
				}
				if _, watch := watched[fn.Name.Name]; !watch {
					continue
				}
				seen[fn.Name.Name] = true
				ast.Inspect(fn, func(n ast.Node) bool {
					call, ok := n.(*ast.CallExpr)
					if !ok {
						return true
					}
					sel, ok := call.Fun.(*ast.SelectorExpr)
					if !ok || !forbidden[sel.Sel.Name] {
						return true
					}
					t.Errorf("%s calls %s directly at %s — attachment-carrying "+
						"writes must go through one transactional DAL entry "+
						"point (PutChatWithAttachments / PutReplyCardWithChat / "+
						"PutReplyCardWithAttachments), or a failure leaves blobs "+
						"no record can name",
						fn.Name.Name, sel.Sel.Name, fset.Position(call.Pos()))
					return true
				})
			}
		}
	}
	// A scan that silently stops finding its targets proves nothing — a rename
	// would leave it green forever.
	for name := range watched {
		if !seen[name] {
			t.Errorf("watched function %s not found — this scan is no longer "+
				"looking at anything; update the list", name)
		}
	}
}
