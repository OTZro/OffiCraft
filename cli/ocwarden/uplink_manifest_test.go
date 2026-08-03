package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// manifestUplinkPaths is the runtime half of "the committed list equals the set of
// bodies that were actually confronted": it returns, per route, how many JSON uplinks
// the committed manifest hangs on one wire test.
//
// The static guard (bin/uplink-guard.py) cannot answer this. Everything it validates,
// it validates in one pass, so any compared-vs-committed total it computes is the same
// set counted twice — an earlier version carried exactly such a total under nine lines
// of comment calling it the load-bearing check, and instrumenting its failure branch
// showed it was never once reached.
//
// It is a per-route MULTISET, not a count. A bare count is satisfiable by driving an
// already-covered producer a second time: independent review added a real new uplink,
// its row, and one extra `drive("brand-new", session.reportIdentity)` — the total
// balanced and a body that would 422 in production was never once sent.
//
// What each caller compares this against differs, and the difference matters:
//   - codex_uplink_wire_test.go: routes of the bodies REAL producers put on an
//     httptest server this run. Strongest form.
//   - cli/ocagent/telemetry_wire_test.go: same — routes observed by its test server.
//   - telemetry_wire_test.go (this module): the routes of the payloads it walks, two
//     of which are still hand-written literals rather than producer output. That
//     weakness is pre-existing and separately recorded; the join still catches a row
//     committed with nothing to walk, but it does NOT prove a producer emitted it.
func manifestUplinkPaths(t *testing.T, wireTest string) map[string]int {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "uplinks.json"))
	if err != nil {
		t.Fatalf("read uplinks manifest: %v", err)
	}
	var doc struct {
		Uplinks []struct {
			ID       string `json:"id"`
			Kind     string `json:"kind"`
			Path     string `json:"path"`
			WireCase string `json:"wire_case"`
			WireTest string `json:"wire_test"`
		} `json:"uplinks"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("parse uplinks manifest: %v", err)
	}
	want := map[string]int{}
	for _, one := range doc.Uplinks {
		if one.Kind == "json" && one.WireTest == wireTest {
			// Keyed by "which producer run, then which route" where the manifest says
			// so. A per-ROUTE count alone is satisfiable by driving an already-covered
			// producer a second time: review added a real uplink whose body would 422
			// in production, plus one extra call to an old producer, and the totals
			// balanced on the route it shared. The case name is what makes the two
			// sides per-uplink instead of per-route.
			key := one.Path
			if one.WireCase != "" {
				key = one.WireCase + " → " + one.Path
			}
			want[key]++
		}
	}
	// Zero means this test is no longer the evidence for anything, which makes every
	// comparison below it vacuous rather than passing. It is a failure, not a floor.
	if len(want) == 0 {
		t.Fatalf("cli/uplinks.json commits no JSON uplink to %s, so the join it is "+
			"supposed to close has no committed side — a renamed wire_test path, or a "+
			"test that stopped being anyone's evidence", wireTest)
	}
	return want
}
