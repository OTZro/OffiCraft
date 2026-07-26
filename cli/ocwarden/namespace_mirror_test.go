// namespace_mirror_test.go — the warden's half of the cross-module mirror
// confrontation (T-5047).
//
// cli/ocwarden and server/ocserverd are SEPARATE Go modules with no import path
// between them, and both derive the warden's launchd label and data root from a
// namespace. Those two derivations are hand-transcribed copies of each other,
// and the consequence of a one-character divergence is not a wrong string: the
// server asks launchd "is com.officraft.ocwarden.lab loaded?", gets "no" because
// the warden actually registered something else, concludes the host carries no
// warden, and installs a second one over the live job. That is how this fleet
// lost its warden three times.
//
// Neither side can test the other, so BOTH are tested against one shared table —
// bin/tests/fixtures/namespace-axes.tsv. Change this module's derivation and only
// this test goes red; change the server's and only the server's does. The failure
// therefore names the copy that drifted, which "assert A == B" never can.
package main

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// nsAxesPath is the shared table, reached by relative path because a Go module
// boundary is exactly what this test exists to reach across.
const nsAxesPath = "../../bin/tests/fixtures/namespace-axes.tsv"

type nsAxisRow struct {
	line       int
	ns         string
	rootSuffix string
	label      string
	socket     string
}

// loadNSAxes parses the shared table. A parse failure is a FATAL, never a skip:
// a mirror guard that quietly passes when it cannot find its own fixture is
// worse than no guard, because the green tick then means "nothing was checked".
func loadNSAxes(t *testing.T) []nsAxisRow {
	t.Helper()
	f, err := os.Open(nsAxesPath)
	if err != nil {
		t.Fatalf("open %s: %v — the shared namespace table is the ONLY thing keeping this module's derivation aligned with server/ocserverd; if it moved, fix the path, do not delete the test", nsAxesPath, err)
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
		rows = append(rows, nsAxisRow{
			line: n, ns: unempty(cols[0]), rootSuffix: unempty(cols[1]),
			label: cols[2], socket: cols[3],
		})
	}
	if err := sc.Err(); err != nil {
		t.Fatalf("read %s: %v", nsAxesPath, err)
	}
	if len(rows) < 2 {
		t.Fatalf("%s carries %d rows — a table with no empty-namespace row and no namespaced row proves nothing", nsAxesPath, len(rows))
	}
	return rows
}

// TestNamespaceAxes_MatchTheSharedTable is the confrontation itself.
func TestNamespaceAxes_MatchTheSharedTable(t *testing.T) {
	const home = "/Users/fixture"
	sawEmpty := false
	for _, r := range loadNSAxes(t) {
		if r.ns == "" {
			sawEmpty = true
		}
		if got := wardenLabelFor(r.ns); got != r.label {
			t.Errorf("%s:%d wardenLabelFor(%q) = %q, table says %q — cli/ocwarden has drifted from the shared derivation; server/ocserverd's wardenLaunchdLabel still answers %q, so the server would not recognise the job this warden registers",
				nsAxesPath, r.line, r.ns, got, r.label, r.label)
		}
		wantRoot := filepath.Join(home, ".officraft"+r.rootSuffix)
		if got := officraftRootFor(home, r.ns); got != wantRoot {
			t.Errorf("%s:%d officraftRootFor(%q) = %q, table says %q", nsAxesPath, r.line, r.ns, got, wantRoot)
		}
		if got := tmuxSocketFor(r.ns); got != r.socket {
			t.Errorf("%s:%d tmuxSocketFor(%q) = %q, table says %q", nsAxesPath, r.line, r.ns, got, r.socket)
		}
		// The tokfile is DERIVED from root in the table's contract, so this also
		// pins that the two cannot disagree inside this module.
		wantTok := filepath.Join(wantRoot, "warden", "exec-warden.tok")
		if got := tokfileFor(home, r.ns); got != wantTok {
			t.Errorf("%s:%d tokfileFor(%q) = %q, table says %q", nsAxesPath, r.line, r.ns, got, wantTok)
		}
	}
	if !sawEmpty {
		t.Fatal("the shared table has no empty-namespace row — the 'main instance is byte-identical to the historical literal' claim is then untested")
	}
}

// TestNamespaceCharset_MatchesTheSharedTable pins the FOURTH axis the comments
// have always claimed and nothing has ever checked: the charset regex is written
// out by hand in four places (this file's namespace.go, server/ocserverd/config.go,
// bin/install.sh, bin/ocserver). A looser copy anywhere admits a namespace the
// other three reject — i.e. one component builds a path or label the others will
// not recognise, which is the divergence this whole file is about.
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
		t.Errorf("cli/ocwarden namespaceShape = %q, shared table says %q — a namespace one component accepts and another rejects is a split-brain install", got, want)
	}
}
