package main

import (
	"bytes"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// announceEnv builds the env lookup these tests hand the commands: nothing set
// unless the test says so, which is exactly the shape of the machine the owner
// complained about (a normal install has neither variable and no config file).
func announceEnv(kv map[string]string) func(string) string {
	return func(k string) string { return kv[k] }
}

// TestAnnouncementNamesTheDatabaseAndWhereItCameFrom is the acceptance the
// owner actually asked for, in his words: the complaint was that it acts on the
// real database "silently". So the assertion is not "it printed something" — it
// is that BOTH halves are on screen before anything happens: which config file
// (or that there is none, and where it looked) and which database, each with
// the source of that answer.
func TestAnnouncementNamesTheDatabaseAndWhereItCameFrom(t *testing.T) {
	dir := t.TempDir()
	db := filepath.Join(dir, "explicit.db")

	cases := []struct {
		name       string
		env        map[string]string
		wantConfig string
		wantSource string
	}{
		{
			// The dangerous one. No config file, no override: the resolution is
			// entirely implicit and that is precisely when it must be loudest.
			name:       "nothing set at all",
			env:        map[string]string{},
			wantConfig: "config file = none",
			wantSource: "the built-in default",
		},
		{
			name:       "database overridden by env",
			env:        map[string]string{envDatabaseURL: "sqlite:///" + db},
			wantConfig: "config file = none",
			wantSource: "$" + envDatabaseURL,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var out bytes.Buffer
			_, dsn, rc := announceResolution("migrate", announceEnv(tc.env), &out)
			if rc != 0 {
				t.Fatalf("rc=%d, want 0 — a missing config file is not an error (owner ruling rc-d961ee5e790c [0]: do not block, say it out loud). out=%s", rc, out.String())
			}
			got := out.String()
			if !strings.Contains(got, tc.wantConfig) {
				t.Errorf("no config-file line matching %q in:\n%s", tc.wantConfig, got)
			}
			if !strings.Contains(got, "database    = "+dsn) {
				t.Errorf("the database line does not carry the DSN that was actually returned (%q) in:\n%s", dsn, got)
			}
			if !strings.Contains(got, tc.wantSource) {
				t.Errorf("no source attribution %q in:\n%s — a path with no source reads as \"that is what I asked for\", which is the misreading this whole change exists to stop", tc.wantSource, got)
			}
		})
	}
}

// TestAnnouncementIsNotAnError pins the half of the owner's ruling that is easy
// to lose in a later "tighten this up" pass: the FIRST plan was to refuse, and
// it was withdrawn because a normal install has no config file by design and
// two documented rescue commands would have been blocked. If someone turns this
// back into a refusal, this goes red and names the ruling.
func TestAnnouncementIsNotAnError(t *testing.T) {
	var out bytes.Buffer
	_, _, rc := announceResolution("mfa-disable", announceEnv(map[string]string{}), &out)
	if rc != 0 {
		t.Fatalf("announceResolution refused with rc=%d when no config file is present. That is the plan the owner REJECTED on rc-d961ee5e790c: install.sh:1174 names the no-config outcome CFG_SRC=\"none\", and docs/guide/mobile.md tells the user to run `ocserverd mfa-disable` on the machine when their authenticator is gone — refusing here breaks the rescue path. out=%s", rc, out.String())
	}
	got := out.String()
	if strings.Contains(got, "FATAL") {
		t.Errorf("the announcement printed FATAL for the ordinary no-config case:\n%s", got)
	}
}

// TestEveryDSNResolutionIsAnnounced is the call-site guard, and it is the one
// that keeps this change true a year from now. Bundling loadConfig + resolveDSN
// + the announcement into one helper only helps if nobody resolves a DSN any
// other way — a test that only exercises the helper would stay green while a
// new subcommand quietly opened the real database without a word.
//
// So: outside this file's own helper (and outside tests), no non-test file in
// this package may call resolveDSN. AST-walked rather than grepped, so that a
// mention inside a comment or a string does not count and a real call cannot
// hide behind formatting.
func TestEveryDSNResolutionIsAnnounced(t *testing.T) {
	const home = "config_announce_t74.go"
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read package dir: %v", err)
	}
	fset := token.NewFileSet()
	var offenders []string
	scanned, controlHits := 0, 0
	for _, e := range entries {
		n := e.Name()
		if e.IsDir() || !strings.HasSuffix(n, ".go") || strings.HasSuffix(n, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, n, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", n, err)
		}
		scanned++
		ast.Inspect(f, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			id, ok := call.Fun.(*ast.Ident)
			if !ok {
				return true
			}
			if id.Name != "resolveDSN" {
				return true
			}
			if n == home {
				controlHits++
				return true
			}
			offenders = append(offenders, n+":"+fset.Position(call.Pos()).String())
			return true
		})
	}
	// Anti-vacuity, both directions: an empty file list, or a walk that cannot
	// see the one call we KNOW is there, would make this trivially green.
	if scanned < 40 {
		t.Fatalf("only %d non-test .go files scanned — too few to be this package, so a clean result would mean nothing", scanned)
	}
	if controlHits == 0 {
		t.Fatalf("positive control failed: the AST walk found ZERO resolveDSN calls in %s, where announceResolution definitely calls it. The walk is broken, not the package.", home)
	}
	if len(offenders) > 0 {
		t.Errorf("resolveDSN is called outside %s: %v\n\nEvery command must learn its database THROUGH announceResolution, because that is the only thing that also says so out loud. If you need the DSN somewhere new, call announceResolution — do not resolve it quietly.", home, offenders)
	}
}
