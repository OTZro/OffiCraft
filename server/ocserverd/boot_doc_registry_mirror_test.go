package main

// boot_doc_registry_mirror_test.go — the server's half of the boot-document
// registry mirror (T-3201). The twin is
// frontend/src/api/mock.boot-doc-registry.test.ts and the reasoning lives in
// bin/tests/fixtures/boot-doc-registry.tsv.
//
// bootDocRegistry is the AUTHORITY: it is what the server actually serves. The
// cockpit carries its own list so it can give each document a row with a name
// and an icon, and there is no compiler between the two. This half pins the
// authority to the shared table; the other half pins the cockpit to the same
// table. A document added to one side and not the other is then a red test on
// the side that was not updated, rather than a document the owner can never see.

import (
	"bufio"
	"os"
	"strings"
	"testing"
)

const bootDocRegistryFixturePath = "../../bin/tests/fixtures/boot-doc-registry.tsv"

type bootDocFixtureRow struct {
	kind     string
	key      string
	readOnly bool
	hasHead  bool
}

// loadBootDocRegistryFixture parses the shared table. Missing, unreadable or
// empty is FATAL, never a skip: a guard that goes green because it could not
// read its fixture is a lie, and this one would go green on an EMPTY file by
// agreeing that nothing exists.
func loadBootDocRegistryFixture(t *testing.T) []bootDocFixtureRow {
	t.Helper()
	f, err := os.Open(bootDocRegistryFixturePath)
	if err != nil {
		t.Fatalf("open %s: %v", bootDocRegistryFixturePath, err)
	}
	defer f.Close()

	var rows []bootDocFixtureRow
	sc := bufio.NewScanner(f)
	for line := 1; sc.Scan(); line++ {
		text := sc.Text()
		if strings.HasPrefix(text, "#") || strings.TrimSpace(text) == "" {
			continue
		}
		cols := strings.Split(text, "\t")
		if len(cols) != 4 {
			t.Fatalf("%s:%d: want 4 tab-separated columns, got %d", bootDocRegistryFixturePath, line, len(cols))
		}
		if cols[0] == "kind" {
			continue // the header row
		}
		for i, name := range []string{"read_only", "has_head"} {
			switch cols[2+i] {
			case "true", "false":
			default:
				t.Fatalf("%s:%d: %s is %q, want true or false", bootDocRegistryFixturePath, line, name, cols[2+i])
			}
		}
		rows = append(rows, bootDocFixtureRow{
			kind: cols[0], key: cols[1],
			readOnly: cols[2] == "true", hasHead: cols[3] == "true",
		})
	}
	if err := sc.Err(); err != nil {
		t.Fatalf("read %s: %v", bootDocRegistryFixturePath, err)
	}
	if len(rows) == 0 {
		t.Fatalf("%s parsed to zero rows", bootDocRegistryFixturePath)
	}
	return rows
}

func TestBootDocRegistry_MatchesTheSharedTableBothWays(t *testing.T) {
	s := newEventProcServer(t)

	want := map[string]bool{} // address -> read_only
	for _, row := range loadBootDocRegistryFixture(t) {
		addr := row.kind + "/" + row.key
		if _, dup := want[addr]; dup {
			t.Fatalf("%s is listed twice in %s", addr, bootDocRegistryFixturePath)
		}
		want[addr] = row.readOnly
	}

	got := map[string]bool{}
	for _, reg := range bootDocRegistry {
		for _, key := range reg.Keys {
			spec, ok := s.bootDocSpecFor(reg.Kind, key)
			if !ok {
				t.Fatalf("%s/%s is in bootDocRegistry but did not resolve", reg.Kind, key)
			}
			got[reg.Kind+"/"+key] = spec.ReadOnly
		}
	}

	for addr, readOnly := range got {
		fixture, listed := want[addr]
		if !listed {
			t.Errorf("this server serves %s, which %s does not list — add the row in the "+
				"same commit, or the cockpit will have no row for it and nothing will say so",
				addr, bootDocRegistryFixturePath)
			continue
		}
		if fixture != readOnly {
			t.Errorf("%s: the registry says read_only=%v, the shared table says %v",
				addr, readOnly, fixture)
		}
	}
	for addr := range want {
		if _, served := got[addr]; !served {
			t.Errorf("%s lists %s, which this server does not serve", bootDocRegistryFixturePath, addr)
		}
	}
}

// TestBootDocRegistry_HeadPresenceMatchesTheSharedTable pins the THIRD fact the
// table now carries: whether a shipped document has a read-only head.
//
// 🔴 THE FAILURE THIS EXISTS FOR ALREADY HAPPENED (T-6f44). The conformance
// suite asserted 「every boot document has a non-empty read_only_head」 in two
// places. When four documents lost their head by the owner's ruling, three
// copies of that rule were updated and the fourth — in Python, in a suite the
// person editing the seeds said out loud they had not run — was not. It went
// red on documents that were entirely correct, and the message it printed said
// nothing about a rule written down four times.
//
// Both directions on purpose. A head that vanishes is what happened this time;
// a head that appears on a document the table says has none is the same class of
// silent drift (an agent starts reading a machine-written sentence nobody
// decided to send it), and only the second direction catches an accidental
// marker pasted into a seed.
func TestBootDocRegistry_HeadPresenceMatchesTheSharedTable(t *testing.T) {
	s := newEventProcServer(t)

	for _, row := range loadBootDocRegistryFixture(t) {
		spec, ok := s.bootDocSpecFor(row.kind, row.key)
		if !ok {
			t.Fatalf("%s/%s is listed in %s but did not resolve",
				row.kind, row.key, bootDocRegistryFixturePath)
		}
		dto, err := s.foldBootDocDTO(spec)
		if err != nil {
			t.Fatalf("%s/%s: fold: %v", row.kind, row.key, err)
		}
		switch {
		case row.hasHead && dto.ReadOnlyHead == "":
			t.Errorf("%s/%s: %s says it carries a read-only head, the shipped document has none — "+
				"either the marker line was dropped from the seed, or the table row is stale",
				row.kind, row.key, bootDocRegistryFixturePath)
		case !row.hasHead && dto.ReadOnlyHead != "":
			t.Errorf("%s/%s: the shipped document carries a read-only head, %s says it has none — "+
				"a machine-written sentence nobody decided to send is now reaching agents",
				row.kind, row.key, bootDocRegistryFixturePath)
		}
		if dto.ReadOnlyHead != "" && !strings.Contains(dto.Text, dto.ReadOnlyHead) {
			t.Errorf("%s/%s: read_only_head is not a substring of text", row.kind, row.key)
		}
	}
}
