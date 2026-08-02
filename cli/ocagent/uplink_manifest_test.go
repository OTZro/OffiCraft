package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// manifestUplinkPaths is the runtime half of "the committed list equals the set of
// bodies that were actually confronted": per route, how many JSON uplinks the
// committed manifest hangs on one wire test. See the twin in cli/ocwarden for why the
// static guard cannot answer this, and why this is a per-route multiset rather than a
// count (a count is satisfiable by driving an already-covered producer twice).
//
// Here the other side is genuinely independent: the routes a real test server observed
// while the real producer ran.
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
			WireTest string `json:"wire_test"`
		} `json:"uplinks"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("parse uplinks manifest: %v", err)
	}
	want := map[string]int{}
	for _, one := range doc.Uplinks {
		if one.Kind == "json" && one.WireTest == wireTest {
			want[one.Path]++
		}
	}
	if len(want) == 0 {
		t.Fatalf("cli/uplinks.json commits no JSON uplink to %s, so the join it is "+
			"supposed to close has no committed side — a renamed wire_test path, or a "+
			"test that stopped being anyone's evidence", wireTest)
	}
	return want
}
