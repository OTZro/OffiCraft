// onboarding_mirror_test.go — the server's half of the cross-module mirror
// confrontation (T-5047). See cli/ocwarden/namespace_mirror_test.go for the full
// reasoning; the short version:
//
// onboarding.go derives the warden's launchd label and tokfile path to answer
// ONE question — "does this host already carry a warden?" — and that answer is
// the safety interlock standing between first-run onboarding and installing a
// second warden over a live one. The derivation is a hand-transcribed copy of
// cli/ocwarden/namespace.go, in a different Go module, with no compiler between
// them. A one-character divergence does not produce a wrong string; it produces
// "no warden here" for a host that has one.
//
// Both copies are checked against the SAME table, so a drift names the side that
// drifted rather than merely reporting that two things differ.
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
	label      string
}

// loadNSAxes parses the shared table. Missing/unparseable = FATAL, never skip:
// a guard that goes green when it could not read its fixture is a lie.
func loadNSAxes(t *testing.T) []nsAxisRow {
	t.Helper()
	f, err := os.Open(nsAxesPath)
	if err != nil {
		t.Fatalf("open %s: %v — this table is the only thing keeping onboarding.go aligned with cli/ocwarden/namespace.go", nsAxesPath, err)
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
		rows = append(rows, nsAxisRow{line: n, ns: unempty(cols[0]), rootSuffix: unempty(cols[1]), label: cols[2]})
	}
	if err := sc.Err(); err != nil {
		t.Fatalf("read %s: %v", nsAxesPath, err)
	}
	if len(rows) < 2 {
		t.Fatalf("%s carries %d rows — too few to prove anything", nsAxesPath, len(rows))
	}
	return rows
}

// TestOnboardingNamespaceAxes_MatchTheSharedTable — the confrontation.
func TestOnboardingNamespaceAxes_MatchTheSharedTable(t *testing.T) {
	const home = "/Users/fixture"
	sawEmpty := false
	for _, r := range loadNSAxes(t) {
		if r.ns == "" {
			sawEmpty = true
		}
		if got := wardenLaunchdLabel(r.ns); got != r.label {
			t.Errorf("%s:%d wardenLaunchdLabel(%q) = %q, table says %q — server/ocserverd has drifted from cli/ocwarden's wardenLabelFor. The warden registers the table's label; this server would ask launchd about %q, be told 'not loaded', and conclude the host has NO warden — then install a second one over the live job.",
				nsAxesPath, r.line, r.ns, got, r.label, got)
		}
		wantRoot := filepath.Join(home, ".officraft"+r.rootSuffix)
		if got := officraftRootPath(home, r.ns); got != wantRoot {
			t.Errorf("%s:%d officraftRootPath(%q) = %q, table says %q", nsAxesPath, r.line, r.ns, got, wantRoot)
		}
		// The tokfile is the OTHER half of the interlock (the file probe used when
		// launchd cannot answer). Derived from root by the table's contract.
		wantTok := filepath.Join(wantRoot, "warden", "exec-warden.tok")
		if got := wardenTokfilePath(home, r.ns); got != wantTok {
			t.Errorf("%s:%d wardenTokfilePath(%q) = %q, table says %q — the interlock would stat a path nobody writes and answer 'no warden here'", nsAxesPath, r.line, r.ns, got, wantTok)
		}
	}
	if !sawEmpty {
		t.Fatal("the shared table has no empty-namespace row — the main-instance claim is untested")
	}
}

// TestOnboardingNamespaceCharset_MatchesTheSharedTable — the fourth axis, see the
// cli twin. config.go's namespaceShape is what decides which namespaces this
// server will even boot with; a copy looser than the warden's admits an instance
// the warden cannot serve.
func TestOnboardingNamespaceCharset_MatchesTheSharedTable(t *testing.T) {
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
		t.Fatalf("%s carries no `# charset<TAB><regex>` line", nsAxesPath)
	}
	if got := namespaceShape.String(); got != want {
		t.Errorf("server/ocserverd namespaceShape = %q, shared table says %q", got, want)
	}
}
