// doc_cap_mirror_test.go — the server's half of the document-cap mirror
// confrontation (T-7d33). See frontend/src/api/docCap.test.ts for the twin and
// bin/tests/fixtures/doc-cap-cases.tsv for the reasoning; the short version:
//
// DocCapBlocked is the AUTHORITY — it is what actually refuses a write with an
// HTTP 400. The cockpit now carries a hand-transcribed copy in TypeScript so it
// can grey out a revision the server would refuse BEFORE the owner clicks it.
// There is no compiler between the two, and a divergence produces no error
// anywhere: it produces a cockpit that lies about which revisions are
// restorable, in one direction or the other, silently.
//
// Both copies are checked against the SAME table, so a drift names the side that
// drifted rather than merely reporting that two things differ. Deliberately NOT
// a mock of one side inside the other's test — that would only prove the mock
// agrees with itself.
package main

import (
	"bufio"
	"os"
	"strconv"
	"strings"
	"testing"
)

const docCapCasesPath = "../../bin/tests/fixtures/doc-cap-cases.tsv"

type docCapRow struct {
	line    int
	name    string
	before  int
	after   int
	blocked bool
	fill    string
}

// loadDocCapCases parses the shared table. Missing/unparseable = FATAL, never
// skip: a guard that goes green when it could not read its fixture is a lie.
func loadDocCapCases(t *testing.T) ([]docCapRow, int) {
	t.Helper()
	f, err := os.Open(docCapCasesPath)
	if err != nil {
		t.Fatalf("open %s: %v — this table is the only thing keeping domain.go's DocCapBlocked aligned with frontend/src/api/docCap.ts", docCapCasesPath, err)
	}
	defer f.Close()

	cap := 0
	var rows []docCapRow
	sc := bufio.NewScanner(f)
	// The astral rows build 12k-rune strings of 4-byte runes; the default 64 KiB
	// token limit is about the FILE's lines, which stay short, but be explicit.
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for n := 1; sc.Scan(); n++ {
		line := sc.Text()
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(line, "# cap\t") {
			cap, err = strconv.Atoi(strings.TrimSpace(strings.TrimPrefix(line, "# cap\t")))
			if err != nil {
				t.Fatalf("%s:%d: unparseable `# cap` line: %v", docCapCasesPath, n, err)
			}
			continue
		}
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		cols := strings.Split(line, "\t")
		if len(cols) != 5 {
			t.Fatalf("%s:%d: want 5 tab-separated columns, got %d: %q", docCapCasesPath, n, len(cols), line)
		}
		if cols[0] == "case" {
			continue // the header row
		}
		before, err := strconv.Atoi(cols[1])
		if err != nil {
			t.Fatalf("%s:%d: before_runes: %v", docCapCasesPath, n, err)
		}
		after, err := strconv.Atoi(cols[2])
		if err != nil {
			t.Fatalf("%s:%d: after_runes: %v", docCapCasesPath, n, err)
		}
		blocked, err := strconv.ParseBool(cols[3])
		if err != nil {
			t.Fatalf("%s:%d: blocked: %v", docCapCasesPath, n, err)
		}
		if len([]rune(cols[4])) != 1 {
			t.Fatalf("%s:%d: fill must be exactly ONE code point, got %q", docCapCasesPath, n, cols[4])
		}
		rows = append(rows, docCapRow{line: n, name: cols[0], before: before, after: after, blocked: blocked, fill: cols[4]})
	}
	if err := sc.Err(); err != nil {
		t.Fatalf("read %s: %v", docCapCasesPath, err)
	}
	if cap == 0 {
		t.Fatalf("%s carries no `# cap<TAB><n>` line — the threshold would go untested", docCapCasesPath)
	}
	if len(rows) < 5 {
		t.Fatalf("%s carries %d rows — too few to prove anything", docCapCasesPath, len(rows))
	}
	return rows, cap
}

// TestDocCapBlocked_MatchesTheSharedTable — the confrontation.
func TestDocCapBlocked_MatchesTheSharedTable(t *testing.T) {
	rows, cap := loadDocCapCases(t)

	// The threshold itself is on the table, so the number is not a third copy
	// — the table moves when the shipped default moves.
	if contextDocMaxCharsDefault != cap {
		t.Errorf("contextDocMaxCharsDefault = %d, shared table says %d — the two implementations are now capping at different sizes", contextDocMaxCharsDefault, cap)
	}

	sawMultiByte := false
	for _, r := range rows {
		if len(r.fill) > 1 {
			sawMultiByte = true
		}
		before := strings.Repeat(r.fill, r.before)
		after := strings.Repeat(r.fill, r.after)
		if got := DocCapBlocked(cap, before, after); got != r.blocked {
			t.Errorf("%s:%d %s: DocCapBlocked(before=%d×%q, after=%d×%q) = %v, table says %v — server/ocserverd has drifted from the shared rule (the cockpit's frontend/src/api/docCap.ts copy still follows the table, so the two now disagree about which revisions are restorable)",
				docCapCasesPath, r.line, r.name, r.before, r.fill, r.after, r.fill, got, r.blocked)
		}
	}
	// Without a multi-byte row the unit (runes vs bytes vs UTF-16 units) is
	// untested and swapping it passes silently — the exact defect these rows
	// exist to catch, so their ABSENCE must fail too.
	if !sawMultiByte {
		t.Fatal("the shared table has no multi-byte fill row — the rune-vs-byte unit is untested")
	}
}
