package main

// auth_refusal_wire_t14_test.go — T-14 項目 4B, the CROSS-MODULE half.
//
// 🔴 WHAT THIS FILE EXISTS FOR. The kill signal that ends a superseded session
// is not a function call and not a shared type — it is TWO STRING LITERALS on
// the wire: the response header name, and the marker value in it. The server
// writes them (authz.go); cli/ocagent reads them (listen.go). CLAUDE.md §13:
// the four Go modules cannot import each other, so each side keeps its own
// `const`. Two copies, no compiler between them.
//
// Before this file, NOTHING pinned them together. Change the server's two
// literals and leave ocagent's untouched and BOTH modules' full suites stay
// green — while the line is dead end to end. What the owner would see:
//
//	the old session never dies. Its tmux is still up, still holding a model
//	session, re-dialling every ≤15s; the server log shows a 401 every 15s; and
//	the cockpit shows nothing at all under that member, because its presence
//	now belongs to the SUCCESSOR. Every test, both modules, green.
//
// So this gate reads the literals OUT OF the ocagent source — parsed, not
// transcribed (same technique and same reason as notificationIDField in
// api_chat_id_identity_ta828_test.go) — and compares them to the server's own
// constants. A constant written down here would be a THIRD copy agreeing with
// itself; the point is to compare against the file the other process compiles.
//
// ── what this does NOT cover ────────────────────────────────────────────────
//
// That ocagent ACTS on the marker (folds the refusal into its fail-closed
// self-terminate) is cli/ocagent's own test — TestListener_… in listen_test.go.
// This file only pins that the two sides are still SAYING THE SAME WORDS. If
// ocagent stops reading the header entirely, this stays green.

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strconv"
	"testing"
)

// ocagentAuthRefusalConsts names the two ocagent constants that must mirror the
// server's. The NAMES are allowed to be transcribed — a rename is a compile
// error on their side and a FATAL here, both loud. The VALUES are what may not
// be transcribed: those are the wire.
var ocagentAuthRefusalConsts = []struct {
	name   string
	server string // the server-side constant this one must equal
	what   string
}{
	{"authRefusalHeader", authRefusalHeader, "the response header the refusal rides on"},
	{"refusalAgentSuperseded", refusalAgentSuperseded, "the marker value that means 'your generation is over'"},
}

// TestAuthRefusalWire_ServerAndOcagentNameTheSameHeaderAndMarker is the gate.
func TestAuthRefusalWire_ServerAndOcagentNameTheSameHeaderAndMarker(t *testing.T) {
	got := ocagentStringConsts(t, ocagentListenSource)
	for _, want := range ocagentAuthRefusalConsts {
		theirs, ok := got[want.name]
		if !ok {
			t.Fatalf("%s no longer declares a string const %q — that is %s, and "+
				"this gate can no longer tell whether the two modules agree. "+
				"Either it was renamed (re-point ocagentAuthRefusalConsts) or "+
				"the client half was deleted, in which case the superseded "+
				"session has nothing left to read and never dies.",
				ocagentListenSource, want.name, want.what)
		}
		if theirs != want.server {
			t.Fatalf("THE KILL SIGNAL IS BROKEN END TO END. %s (%s) has drifted "+
				"between the two modules:\n"+
				"  server/ocserverd authz.go %s = %q\n"+
				"  %s %s = %q\n"+
				"Both suites stay green on this: neither module can see the "+
				"other's copy. What actually happens is that a superseded "+
				"session is never told to stop — its tmux and model session "+
				"stay up re-dialling every ≤15s forever, invisible on the "+
				"cockpit because the member's presence belongs to the successor.",
				want.name, want.what,
				want.name, want.server,
				ocagentListenSource, want.name, theirs)
		}
	}
}

// ocagentStringConsts parses `path` and returns every top-level const declared
// with a plain string literal, by name. Anything else (a computed value, a
// concatenation, an iota) is deliberately NOT returned: this gate compares wire
// literals, and a value it cannot read literally must FATAL as "missing" rather
// than be silently skipped into a pass.
func ocagentStringConsts(t *testing.T, path string) map[string]string {
	t.Helper()
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	out := map[string]string{}
	for _, d := range file.Decls {
		gd, ok := d.(*ast.GenDecl)
		if !ok || gd.Tok != token.CONST {
			continue
		}
		for _, s := range gd.Specs {
			vs, ok := s.(*ast.ValueSpec)
			if !ok || len(vs.Names) != len(vs.Values) {
				continue
			}
			for i, n := range vs.Names {
				lit, ok := vs.Values[i].(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING {
					continue
				}
				v, err := strconv.Unquote(lit.Value)
				if err != nil {
					continue
				}
				out[n.Name] = v
			}
		}
	}
	return out
}
