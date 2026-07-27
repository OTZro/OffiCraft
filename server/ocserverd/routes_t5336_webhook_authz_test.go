package main

// routes_t5336_webhook_authz_test.go — T-5336: the four webhook CRUD rows moved
// from the machine FLOOR to requires=admin_agent (owner 2026-07-27).
//
// WHY THIS EXISTS. Every one of those four responses is a WebhookEndpointDTO,
// and that DTO carries the endpoint's PLAINTEXT `token` — the entire credential
// of the public /in inlet. On the machine floor ANY agent token could LIST a
// member's endpoints and walk away with every inlet secret. `MCPExclude: true`
// on those rows kept them out of the MCP tool list, which is a discoverability
// fact about one client, not an authz gate: plain REST was always open. The
// spec/wire.go description nevertheless said the token "is NEVER on any public
// or agent-facing wire" — false as written, and rewritten in the same change.
//
// THREE ARMS, one test each, over the LIVE wired stack (real DB, real auth
// gate), so nothing here is a route-table tautology:
//
//	(a) TestT5336ArmA_MachineFloorReallyAdmitsAPlainAgent — the "before" fact.
//	    It does NOT probe the webhook rows (they are fixed now); it proves the
//	    thing that made the old declaration dangerous: requires=principalMachine
//	    genuinely lets a plain agent token through the choke, demonstrated on a
//	    row that IS still at that floor. Paired with the route-table assertion
//	    that these four rows USED to declare that same floor, that is what "a
//	    plain agent could call all four" means. ⚠️ This arm has no discriminating
//	    power over the fix itself — it stays green whatever the webhook rows
//	    declare. That is deliberate and stated, not an oversight.
//	(b) TestT5336ArmB_AdminAgentStillDrivesAllFourWebhookVerbs — the fix did not
//	    just wall the feature off: the admin 助理 completes a real CRUD round
//	    trip and really receives the token.
//	(c) TestT5336ArmC_PlainAgentIsRefusedOnAllFourWebhookVerbs — a plain agent
//	    is a flat 403 on all four, and the assertion is on the STATUS CODE (plus
//	    the envelope code), never on "the body does not contain the token" — a
//	    404 or an empty list would satisfy that weaker phrasing and neither is
//	    what the ruling asked for.
//
// COUNTERFACTUAL (run by hand, 2026-07-27): reverting the four Requires values
// back to principalMachine turns arm (c) red on all four verbs (200/200/404/404
// instead of 403) while (a) and (b) stay green.

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// t5336WebhookRows is the exact set the ruling moved, written out by hand — a
// list derived from the table under test could not disagree with it.
var t5336WebhookRows = [][2]string{
	{"GET", "/api/members/{member_id}/webhooks"},
	{"POST", "/api/members/{member_id}/webhooks"},
	{"PATCH", "/api/members/{member_id}/webhooks/{endpoint_id}"},
	{"DELETE", "/api/members/{member_id}/webhooks/{endpoint_id}"},
}

func t5336RouteIndex(t *testing.T) map[[2]string]RouteSpec {
	t.Helper()
	specs := defaultRouteSpecs()
	if len(specs) == 0 {
		t.Fatalf("empty route table — every assertion below would be vacuous")
	}
	index := make(map[[2]string]RouteSpec, len(specs))
	for _, s := range specs {
		index[[2]string{s.Method, s.Path}] = s
	}
	return index
}

// t5336WebhookFixture stands up the wired stack with two agent-scope identities: the
// seeded Mira (role_key "assistant" ⇒ admin_agent) and a plain agent whose sub
// is not on the roster (deny-by-default ⇒ principalAgent). Returns the server
// URL, the admin token and the plain-agent token.
func t5336WebhookFixture(t *testing.T) (string, string, string) {
	t.Helper()
	srv, secret, _ := newWiredTestServer(t)
	now := time.Now().Unix()
	adminTok, err := mintJWT("mira", "agent", 300, secret, now, "")
	if err != nil {
		t.Fatalf("mint admin token: %v", err)
	}
	plainTok, err := mintJWT("kyle-t5336", "agent", 300, secret, now, "")
	if err != nil {
		t.Fatalf("mint plain-agent token: %v", err)
	}
	// Premise control: the two tokens really do classify differently. Without
	// this, a fixture regression (Mira losing her role_key, say) would make
	// arms (b) and (c) agree by accident.
	if got := classifyMember(&Member{ID: "mira", RoleKey: adminRoleKey}); got != principalAdminAgent {
		t.Fatalf("fixture premise: seeded mira must classify as %q, got %q", principalAdminAgent, got)
	}
	if got := classifyMember(nil); got != principalAgent {
		t.Fatalf("fixture premise: an unknown sub must classify as %q, got %q", principalAgent, got)
	}
	return srv.URL, adminTok, plainTok
}

// ── arm (a) — the "before" fact: the machine floor really is wide open ───────

func TestT5336ArmA_MachineFloorReallyAdmitsAPlainAgent(t *testing.T) {
	url, _, plainTok := t5336WebhookFixture(t)

	// A row that is STILL at principalMachine today. GET /api/members is the
	// canonical one (and update_member sits at the same floor deliberately —
	// see the T-5336 note on that row in routes.go).
	index := t5336RouteIndex(t)
	roster, ok := index[[2]string{"GET", "/api/members"}]
	if !ok || roster.Requires != principalMachine {
		t.Fatalf("this arm needs a live requires=%q row to demonstrate the floor; "+
			"GET /api/members declares %q — pick another floor row and re-derive",
			principalMachine, roster.Requires)
	}
	if status, body := get(t, url+"/api/members", plainTok); status != 200 {
		t.Fatalf("the machine floor must admit a PLAIN agent (that is what made "+
			"the old webhook declaration dangerous); got %d %s", status, body)
	}

	// And the four webhook rows are no longer at that floor — the fact this arm
	// exists to contrast with. (Stated as a contrast, not as the arm's teeth:
	// the teeth for the new floor are arms (b) and (c) over the live wire.)
	for _, key := range t5336WebhookRows {
		spec, ok := index[key]
		if !ok {
			t.Fatalf("%s %s is not in the route table at all", key[0], key[1])
		}
		if spec.Requires == principalMachine {
			t.Errorf("%s %s is back at the %q floor — that floor admits the very "+
				"plain agent this test just walked through GET /api/members, and "+
				"the response body carries the endpoint's plaintext inlet token",
				key[0], key[1], principalMachine)
		}
	}
}

// ── arm (b) — the fix keeps the feature working for the admin 助理 ───────────

func TestT5336ArmB_AdminAgentStillDrivesAllFourWebhookVerbs(t *testing.T) {
	url, adminTok, _ := t5336WebhookFixture(t)
	base := url + "/api/members/mira/webhooks"

	// CREATE
	status, created := doJSON(t, "POST", base, adminTok,
		`{"endpoint_id":"t5336-hook","purpose":"admin round trip"}`)
	if status != 200 {
		t.Fatalf("admin_agent CREATE: want 200, got %d %v", status, created)
	}
	token, _ := created["token"].(string)
	if token == "" {
		t.Fatalf("admin_agent must actually RECEIVE the token (walling the admin "+
			"out of it would be a different ruling): %v", created)
	}

	// LIST
	status, listBody := get(t, base, adminTok)
	if status != 200 {
		t.Fatalf("admin_agent LIST: want 200, got %d %s", status, listBody)
	}
	var rows []map[string]any
	if err := json.Unmarshal([]byte(listBody), &rows); err != nil {
		t.Fatalf("LIST body is not a JSON array: %v %s", err, listBody)
	}
	if len(rows) != 1 || rows[0]["endpoint_id"] != "t5336-hook" {
		t.Fatalf("admin_agent LIST must serve the endpoint just created, got %s", listBody)
	}

	// UPDATE
	status, patched := doJSON(t, "PATCH", base+"/t5336-hook", adminTok,
		`{"status":"disabled","purpose":"admin patched"}`)
	if status != 200 {
		t.Fatalf("admin_agent PATCH: want 200, got %d %v", status, patched)
	}
	if patched["status"] != "disabled" || patched["purpose"] != "admin patched" {
		t.Fatalf("admin_agent PATCH did not apply: %v", patched)
	}

	// DELETE
	status, deleted := doJSON(t, "DELETE", base+"/t5336-hook", adminTok, "")
	if status != 200 {
		t.Fatalf("admin_agent DELETE: want 200, got %d %v", status, deleted)
	}
	status, listBody = get(t, base, adminTok)
	if status != 200 || strings.Contains(listBody, "t5336-hook") {
		t.Fatalf("admin_agent DELETE must really revoke it, got %d %s", status, listBody)
	}
}

// ── arm (c) — the plain agent is refused on ALL FOUR, by STATUS CODE ─────────

func TestT5336ArmC_PlainAgentIsRefusedOnAllFourWebhookVerbs(t *testing.T) {
	url, adminTok, plainTok := t5336WebhookFixture(t)
	base := url + "/api/members/mira/webhooks"

	// Seed a REAL endpoint through the admin face, so the PATCH/DELETE probes
	// below aim at something that genuinely exists: a 403 on a missing endpoint
	// would be indistinguishable from a 404 the choke never even reached.
	if status, created := doJSON(t, "POST", base, adminTok,
		`{"endpoint_id":"t5336-target","purpose":"deny-face target"}`); status != 200 {
		t.Fatalf("seed endpoint via admin: want 200, got %d %v", status, created)
	}

	cases := []struct {
		name, method, path, body string
	}{
		{"list", "GET", base, ""},
		{"create", "POST", base, `{"endpoint_id":"t5336-sneaky"}`},
		{"update", "PATCH", base + "/t5336-target", `{"status":"disabled"}`},
		{"delete", "DELETE", base + "/t5336-target", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			status, data := doJSON(t, tc.method, tc.path, plainTok, tc.body)
			if status != 403 {
				t.Fatalf("plain agent %s %s: want 403, got %d %v — the response "+
					"shape is NOT the point here, the refusal is: this DTO carries "+
					"the endpoint's plaintext inlet token",
					tc.method, tc.path, status, data)
			}
			env, _ := data["error"].(map[string]any)
			if env == nil || env["code"] != "forbidden" {
				t.Fatalf("plain agent %s %s: want the standard forbidden envelope, got %v",
					tc.method, tc.path, data)
			}
		})
	}

	// The refusal really was authz and not a broken fixture: the SAME endpoint
	// is still there and still readable by the admin, and the plain agent's
	// "create" never landed.
	status, listBody := get(t, base, adminTok)
	if status != 200 {
		t.Fatalf("control: admin LIST after the deny faces: want 200, got %d %s", status, listBody)
	}
	if !strings.Contains(listBody, "t5336-target") {
		t.Fatalf("control: the plain agent's DELETE must NOT have landed: %s", listBody)
	}
	if strings.Contains(listBody, "t5336-sneaky") {
		t.Fatalf("control: the plain agent's CREATE must NOT have landed: %s", listBody)
	}
}
