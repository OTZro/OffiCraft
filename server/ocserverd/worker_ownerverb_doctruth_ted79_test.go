package main

// worker_ownerverb_doctruth_ted79_test.go — T-ed79 parity #7: the wire text for
// the worker owner verbs has to describe the verb this server actually has.
//
// Since T-98f4 all three verbs that move a LIVE worker (改機器 / 換 model / 換手)
// funnel through respawnWorkerForOwnerOp, which opens a graceful wind-down and
// leaves the kill to the 收口. The relocate description still promised the verb
// it had BEFORE that — "kills the current session, and clears pacing so the next
// scheduler tick re-spawns" — which is what an owner reads before pressing it,
// and what an agent's MCP tool list is told. Nothing in the suite read that text,
// so it stayed false through the change that made it false.
//
// This is a DOC-TRUTH gate, and it is narrow on purpose: it does not judge the
// prose, it asks two mechanical questions of the two verbs whose behaviour the
// funnel decides. 換手 is not listed — its description was already written
// against the graceful shape (T-ea82) and it never carried the claim.

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

// The claims a verb on the graceful funnel can no longer make. Matched
// lower-cased; each one asserts an UNCONDITIONAL immediate kill.
var gracefulFunnelForbiddenClaims = []string{
	"kills the current session",
	"kills+respawns",
	"kill+respawn",
	"kills the session",
}

// …and the shape it must actually name. Any ONE of these is enough: the point is
// that a reader is told the session gets to finish, not which word we used.
var gracefulFunnelRequiredMarkers = []string{
	"wind-down", "winds down", "hand-over", "handover", "收口", "report_stopped",
}

func TestWorkerOwnerVerbWireTextDescribesTheGracefulFunnel(t *testing.T) {
	raw, err := os.ReadFile("../../spec/openapi.json")
	if err != nil {
		t.Fatalf("read spec/openapi.json: %v", err)
	}
	var doc struct {
		Paths map[string]map[string]struct {
			Description string `json:"description"`
		} `json:"paths"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("parse spec/openapi.json: %v", err)
	}
	if len(doc.Paths) == 0 {
		t.Fatalf("no paths parsed — this gate would judge nothing")
	}
	for _, path := range []string{
		"/api/outsource-workers/{id}/relocate",
		"/api/outsource-workers/{id}/model",
	} {
		op, ok := doc.Paths[path]["post"]
		if !ok {
			t.Errorf("POST %s is not in spec/openapi.json at all", path)
			continue
		}
		desc := strings.ToLower(op.Description)
		if strings.TrimSpace(desc) == "" {
			t.Errorf("POST %s has an EMPTY description — the checks below would be "+
				"vacuously green", path)
			continue
		}
		for _, claim := range gracefulFunnelForbiddenClaims {
			if strings.Contains(desc, claim) {
				t.Errorf("POST %s still promises %q. Since T-98f4 this verb goes through "+
					"respawnWorkerForOwnerOp: a LIVE worker with anything to flush gets a "+
					"wind-down and keeps its session until its own report_stopped (or the "+
					"owner's force-stop). The text an owner reads before pressing the "+
					"button, and the text an agent's tool list carries, describes a verb "+
					"this server no longer has.", path, claim)
			}
		}
		named := false
		for _, marker := range gracefulFunnelRequiredMarkers {
			if strings.Contains(desc, marker) {
				named = true
				break
			}
		}
		if !named {
			t.Errorf("POST %s never names the wind-down (looked for one of %v). "+
				"Whether the session gets to finish is the single most consequential "+
				"thing about this verb; a description that omits it is not shorter, it "+
				"is missing the answer.", path, gracefulFunnelRequiredMarkers)
		}
	}
}
