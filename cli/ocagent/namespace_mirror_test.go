// namespace_mirror_test.go — ocagent's half of the cross-module namespace mirror
// confrontation (T-5047).
//
// WHY THIS FILE EXISTS
// -------------------
// config.go's fallbackAgentsHome derives THIS INSTANCE's agents root from
// OC_NAMESPACE, and it is a hand-transcribed copy of cli/ocwarden/namespace.go's
// officraftRootFor (+ the charset regex) because ocagent and ocwarden are separate
// Go modules with no import path between them. Until T-5047 this derivation point
// had NO namespace in it at all — it was a hard-wired ~/.officraft/agents — so a
// namespaced ocagent that lost OC_AGENT_HOME resolved its state directory into the
// MAIN instance's tree. It was described as "an axis that only exists in the Go
// copy"; it was in fact a derivation point missing its namespace, which is why it
// now gets the same treatment as every other copy: confronted against the ONE
// shared table, so a drift reddens THIS copy by name rather than merely reporting
// that two copies differ.
//
// The table is bin/tests/fixtures/namespace-axes.tsv, reached by relative path
// because a Go module boundary is precisely what this test exists to reach across.
package main

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const nsAxesPath = "../../bin/tests/fixtures/namespace-axes.tsv"

type nsAxisRow struct {
	line       int
	ns         string
	rootSuffix string
}

// loadNSAxes parses the shared table. A parse failure is FATAL, never a skip: a
// mirror guard that quietly passes when it cannot find its own fixture is worse
// than no guard, because the green tick then means "nothing was checked".
func loadNSAxes(t *testing.T) []nsAxisRow {
	t.Helper()
	f, err := os.Open(nsAxesPath)
	if err != nil {
		t.Fatalf("open %s: %v — the shared namespace table is the ONLY thing keeping this module's derivation aligned with cli/ocwarden and server/ocserverd; if it moved, fix the path, do not delete the test", nsAxesPath, err)
	}
	defer f.Close()

	unempty := func(s string) string {
		if s == "<empty>" {
			return ""
		}
		return s
	}
	var rows []nsAxisRow
	sc := bufio.NewScanner(f)
	for n := 1; sc.Scan(); n++ {
		line := sc.Text()
		if strings.TrimSpace(line) == "" || strings.HasPrefix(strings.TrimSpace(line), "#") {
			continue
		}
		cols := strings.Split(line, "\t")
		if len(cols) != 4 {
			t.Fatalf("%s:%d: want 4 tab-separated columns, got %d: %q", nsAxesPath, n, len(cols), line)
		}
		rows = append(rows, nsAxisRow{line: n, ns: unempty(cols[0]), rootSuffix: unempty(cols[1])})
	}
	if err := sc.Err(); err != nil {
		t.Fatalf("read %s: %v", nsAxesPath, err)
	}
	if len(rows) < 2 {
		t.Fatalf("%s carries %d rows — a table with no empty-namespace row and no namespaced row proves nothing", nsAxesPath, len(rows))
	}
	return rows
}

// TestAgentsHomeFallback_MatchesTheSharedTable is the confrontation itself: for
// every namespace in the table, the fallback lands under that namespace's root.
func TestAgentsHomeFallback_MatchesTheSharedTable(t *testing.T) {
	const home = "/Users/fixture"
	homeDir := func() (string, error) { return home, nil }
	sawEmpty := false
	for _, r := range loadNSAxes(t) {
		if r.ns == "" {
			sawEmpty = true
		}
		want := filepath.Join(home, ".officraft"+r.rootSuffix, "agents")
		got := fallbackAgentsHome(func(k string) string {
			if k == envNamespaceKey {
				return r.ns
			}
			return ""
		}, homeDir)
		if got != want {
			t.Errorf("%s:%d fallbackAgentsHome(ns=%q) = %q, table says %q — ocagent has drifted from the shared derivation, so a namespaced agent would keep its state in a DIFFERENT instance's tree than the warden that spawned it",
				nsAxesPath, r.line, r.ns, got, want)
		}
	}
	if !sawEmpty {
		t.Fatal("the shared table has no empty-namespace row — the 'main instance is byte-identical to the historical literal' claim is then untested")
	}
}

// TestNamespaceCharset_MatchesTheSharedTable pins the charset copy. A looser copy
// here admits a namespace the other components reject; and because this copy JOINS
// the value into a path, a loose one is not merely inconsistent — `../x` would put
// the agent's state outside every instance root.
func TestNamespaceCharset_MatchesTheSharedTable(t *testing.T) {
	raw, err := os.ReadFile(nsAxesPath)
	if err != nil {
		t.Fatalf("read %s: %v", nsAxesPath, err)
	}
	want := ""
	for _, line := range strings.Split(string(raw), "\n") {
		if strings.HasPrefix(line, "# charset\t") {
			want = strings.TrimPrefix(line, "# charset\t")
			break
		}
	}
	if want == "" {
		t.Fatalf("%s carries no `# charset<TAB><regex>` line — the charset half of the mirror is unpinned", nsAxesPath)
	}
	if got := namespaceShape.String(); got != want {
		t.Errorf("cli/ocagent namespaceShape = %q, shared table says %q", got, want)
	}
}

// TestAgentsHomeFallback_RefusesRatherThanFoldingBack is the NEGATIVE control, and
// the one that distinguishes this fix from "we added a suffix". Every rejected
// namespace must yield "" — NOT the main instance's ~/.officraft/agents. Folding a
// malformed namespace back onto the main instance is the failure mode namespace.go
// calls out as worse than a hard error, and `../x` would additionally escape the
// instance root altogether.
func TestAgentsHomeFallback_RefusesRatherThanFoldingBack(t *testing.T) {
	const home = "/Users/fixture"
	homeDir := func() (string, error) { return home, nil }
	mainRoot := filepath.Join(home, ".officraft", "agents")
	for _, ns := range []string{"Bad.NS", "../x", "..", "with space", "UPPER", "0123456789abcdefg", "a/b"} {
		got := fallbackAgentsHome(func(k string) string {
			if k == envNamespaceKey {
				return ns
			}
			return ""
		}, homeDir)
		if got == mainRoot {
			t.Errorf("OC_NAMESPACE=%q folded back to the MAIN instance's %q — a malformed namespace must never silently share the main instance's tree", ns, mainRoot)
		}
		if got != "" {
			t.Errorf("OC_NAMESPACE=%q derived %q; a rejected namespace must derive nothing at all (the agent then keeps no dedup state, which costs a refetch and nothing else)", ns, got)
		}
	}
}

// TestAgentsHomeFallback_UnresolvableHomeDegrades pins the pre-existing tolerated
// degradation, so the namespace work above cannot have turned it into a panic or a
// relative path that looks absolute.
func TestAgentsHomeFallback_UnresolvableHomeDegrades(t *testing.T) {
	got := fallbackAgentsHome(func(k string) string {
		if k == envNamespaceKey {
			return "lab"
		}
		return ""
	},
		func() (string, error) { return "", os.ErrNotExist })
	if got != "" {
		t.Errorf("fallbackAgentsHome with an unresolvable home = %q, want \"\"", got)
	}
}
