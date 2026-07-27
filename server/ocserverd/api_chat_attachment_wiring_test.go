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
// There is no runtime choke available here — the non-atomic DAL writes are
// legitimately used by ~40 other call sites — so this is a STRUCTURAL scan
// pinned at one place: inside a function that resolves attachment inputs, the
// per-record writes must not appear. It is deliberately keyed on the CALL
// EXPRESSION, not on a comment or a name, so writing the loop by hand does not
// slip past.
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
