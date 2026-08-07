package main

// api_version_tool_t77b4_test.go — the station's own build identity AS AN
// AGENT TOOL. The route is old and public; what is new is that it appears on
// the MCP surface, and routes.go used to keep it off there on purpose ("a
// build-identity probe, not an agent tool").
//
// Two halves, and the second one is the load-bearing one:
//
//	(1) the tool is reachable — on the route table AND in the frozen catalog
//	    tools/list is served from — at the row's own unchanged floor;
//	(2) an ORDINARY member (not owner, not the admin agent) really calls it
//	    and gets a git_sha back, while the same token still gets refused by
//	    check_release. Being listed is not being callable, and without that
//	    negative control "the tool answered me" cannot tell an opening of THIS
//	    row apart from an opening of the whole authz layer.
//
// There is deliberately NO test that check_release and get_version agree, or
// that one can substitute for the other: they answer different questions from
// different sources (this station's running build vs whether GitHub has a
// NEWER release). A test pinning them together would pin a merge this ticket
// exists to refuse.

import (
	"strings"
	"testing"
	"time"
)

// versionToolName is the tool name derived from method+path for /api/version
// (no MCPTool override). Written once so a rename touches this constant
// rather than drifting across assertions.
const versionToolName = "get_version"

// TestVersionToolIsOnTheAgentSurfaceAtItsOwnFloor pins the reversal at the
// table level, and pins what must NOT have moved with it: the principal floor.
// Listing the row is the whole change; if this test ever has to be edited
// because Requires moved, that is a different and much larger decision than
// the one the row's comment records.
func TestVersionToolIsOnTheAgentSurfaceAtItsOwnFloor(t *testing.T) {
	row, ok := mcpToolIndex(defaultRouteSpecs())[versionToolName]
	if !ok {
		t.Fatalf("%s is missing from the route table's tool index", versionToolName)
	}
	// The table is only half of it: tools/list is SERVED from the frozen
	// spec/mcp-catalog.json (assets.go, embed-only), so a row on the table but
	// absent from the catalog is a tool no agent can see.
	if !frozenCatalogCarries(t, versionToolName) {
		t.Fatalf("%s is on the route table but MISSING from spec/mcp-catalog.json "+
			"— agents reach tools only through tools/list, so for them the "+
			"channel does not exist", versionToolName)
	}
	if row.Method != "GET" || row.Path != "/api/version" {
		t.Fatalf("%s is bound to the wrong row: %s %s", versionToolName, row.Method, row.Path)
	}
	if row.Requires != requiresPublic {
		t.Fatalf("the build-identity floor moved to %q — exposing this row was a "+
			"DISCOVERABILITY change; moving the floor is a separate ruling",
			row.Requires)
	}
	// The ops/deploy probes stay OFF the surface, and this has to be checked on
	// the rows rather than on tool names: toolName() derives `get_version` for
	// BOTH /api/version and the bare /version deploy probe, and `get_health`
	// for both health rows. So a name lookup cannot tell "the right row is
	// exposed" from "a second row silently took the name over" — the Path
	// assertion above is what pins that, and this loop pins that the other
	// three rows are still excluded at all.
	for _, probe := range []string{"/api/health", "/health", "/version"} {
		for _, spec := range defaultRouteSpecs() {
			if spec.Path == probe && !spec.MCPExclude {
				t.Fatalf("%s must stay mcp_exclude — one face onto build "+
					"identity and liveness, not four", probe)
			}
		}
	}
}

// mcpToolResult drives one no-argument tools/call and returns its result
// envelope. A transport-level RPC error is a test failure, not a case: every
// outcome this file cares about (answered / refused) rides HTTP 200 inside the
// envelope, with isError carrying the verdict.
func mcpToolResult(t *testing.T, baseURL, token, tool string) map[string]any {
	t.Helper()
	payload := postMCP(t, baseURL, token,
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"`+tool+`","arguments":{}}}`)
	if rpcErr, present := payload["error"]; present {
		t.Fatalf("expected a result envelope for %s, got RPC error: %v", tool, rpcErr)
	}
	res, _ := payload["result"].(map[string]any)
	if res == nil {
		t.Fatalf("no result envelope for %s: %v", tool, payload)
	}
	return res
}

// mcpResultText returns the result's single text content — the raw sub-response
// body, which is where a refusal states its reason.
func mcpResultText(t *testing.T, res map[string]any) string {
	t.Helper()
	items, _ := res["content"].([]any)
	if len(items) == 0 {
		t.Fatalf("result carries no content: %v", res)
	}
	first, _ := items[0].(map[string]any)
	text, _ := first["text"].(string)
	if text == "" {
		t.Fatalf("result's first content item carries no text: %v", res)
	}
	return text
}

// TestOrdinaryMemberReadsTheStationGitShaAndIsStillRefusedByCheckRelease is
// the one that matters. It drives the wired stack a live agent drives (auth
// gate + RBAC choke + in-process loopback) with a token that is neither the
// owner nor the admin agent, and demands a VALUE — the failure this ticket was
// opened on is "the definition exists but the execution path does not", and
// only a returned git_sha rules that out.
//
// The check_release arm is the negative control, and it is not decoration: if
// the whole authz layer had been opened by accident, the get_version arm would
// pass exactly the same way. It also has to fail for the RIGHT reason, so the
// refusal text is asserted, not merely the isError flag.
func TestOrdinaryMemberReadsTheStationGitShaAndIsStillRefusedByCheckRelease(t *testing.T) {
	srv, secret, _ := newWiredTestServer(t)
	// Deliberately not "mira": that member's role is assistant, i.e. the admin
	// agent, and this test's whole subject is the member who is NOT that.
	ordinary, _ := mintJWT("ow-nobody", "agent", 300, secret, time.Now().Unix(), "")

	res := mcpToolResult(t, srv.URL, ordinary, versionToolName)
	if res["isError"] != false {
		t.Fatalf("an ordinary member must be able to read the station's build "+
			"identity, got: %v", res)
	}
	sc, _ := res["structuredContent"].(map[string]any)
	sha, _ := sc["git_sha"].(string)
	if sha == "" {
		t.Fatalf("%s answered without a git_sha — being listed in tools/list is "+
			"not the same as answering, and git_sha is the field the dev SOP "+
			"settles shipping with: %v", versionToolName, res)
	}

	// Negative control, same token, same stack.
	refused := mcpToolResult(t, srv.URL, ordinary, "check_release")
	if refused["isError"] != true {
		t.Fatalf("check_release must still refuse an ordinary member — otherwise "+
			"the get_version arm above proves nothing about WHICH row was "+
			"opened: %v", refused)
	}
	if body := mcpResultText(t, refused); !strings.Contains(body, "not permitted") {
		t.Fatalf("check_release failed for the wrong reason (want the authz "+
			"refusal, got %q) — a refusal that is really a 404 or a GitHub "+
			"timeout would make this control vacuous", body)
	}
}
