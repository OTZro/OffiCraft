package main

// The 初始版本 read is an AGENT TOOL (T-40f0, owner ruling rc-b7d29de0eb9c
// 「開放,照你 7/30 那句話一律給」).
//
// This route first landed MCPExclude on the implementer's judgement that the
// seam carries nothing new for an agent. The owner overruled it from the same
// 2026-07-30 policy the restore row cites (rc-b5fd1135e2dd): the split between
// tool and no-tool is the VERB — reads are given, writes are not — and it is
// not re-decided per route by how useful each read looks.
//
// So what has to stay true is a pair, and BOTH halves need a test or the pair
// is only half pinned:
//
//  1. The READ is on the tool surface. Restoring the MCPExclude makes this
//     file's first two tests fail — the whole point of a positive assertion
//     here is that "the tool is absent" is otherwise nobody's failure: every
//     existing catalog guard only fires in the other direction (a table tool
//     the frozen catalog forgot).
//  2. Opening the read opened NO WRITE. The destructive siblings of this row
//     (restore, and the reset that shares the seed's own 404 set) must still be
//     off the tool surface, and one agent tools/call must leave the document
//     byte-identical. "Looking is not restoring" is the entire safety claim of
//     T-40f0, and it now has to hold on a second channel.

import (
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"
)

const seedToolName = "get_document_seed"

// routeSpecFor finds one row of the live table by method+path. It fails loudly
// rather than returning a zero RouteSpec: a renamed path would otherwise turn
// every assertion below into a comparison against an empty struct, which is the
// silent-pass mode this file exists to avoid.
func routeSpecFor(t *testing.T, method, path string) RouteSpec {
	t.Helper()
	for _, spec := range defaultRouteSpecs() {
		if spec.Method == method && spec.Path == path {
			return spec
		}
	}
	t.Fatalf("no %s %s row on the routes table — the path moved and this test "+
		"would have stopped discriminating", method, path)
	return RouteSpec{}
}

// The read row declares the tool NAME, and the frozen catalog advertises it.
// Both halves are needed: agents reach tools only through tools/list, so a
// table-only name is a tool that does not exist for them, and a catalog-only
// name is one that cannot be called.
func TestDocumentSeedReadIsOnTheAgentToolSurface(t *testing.T) {
	spec := routeSpecFor(t, http.MethodGet, "/api/document-history/{kind}/{key}/seed")
	if spec.MCPExclude {
		t.Fatalf("the 初始版本 read is MCPExclude again — owner ruling "+
			"rc-b7d29de0eb9c opened it (%s %s)", spec.Method, spec.Path)
	}
	if spec.MCPTool != seedToolName {
		t.Fatalf("seed read MCPTool = %q, want %q", spec.MCPTool, seedToolName)
	}
	if got := spec.toolName(); got != seedToolName {
		t.Fatalf("derived tool name = %q, want %q", got, seedToolName)
	}
	if _, ok := mcpToolIndex(defaultRouteSpecs())[seedToolName]; !ok {
		t.Fatalf("%q is not in the table-derived tool index — tools/call cannot "+
			"route it", seedToolName)
	}

	raw, err := os.ReadFile("../../spec/mcp-catalog.json")
	if err != nil {
		t.Fatalf("read frozen catalog: %v", err)
	}
	var catalog struct {
		Tools []struct {
			Name        string `json:"name"`
			Description string `json:"description"`
			InputSchema struct {
				Properties map[string]any `json:"properties"`
				Required   []string       `json:"required"`
			} `json:"inputSchema"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(raw, &catalog); err != nil {
		t.Fatalf("parse frozen catalog: %v", err)
	}
	var found bool
	for _, tool := range catalog.Tools {
		if tool.Name != seedToolName {
			continue
		}
		found = true
		for _, arg := range []string{"kind", "key"} {
			if _, ok := tool.InputSchema.Properties[arg]; !ok {
				t.Errorf("catalog %q advertises no %q argument — the route's path "+
					"template needs it", seedToolName, arg)
			}
		}
		if len(tool.InputSchema.Required) != 2 {
			t.Errorf("catalog %q required = %v, want both path params",
				seedToolName, tool.InputSchema.Required)
		}
		// The description is what an agent decides from, so it must say the two
		// things that stop it being mistaken for the restore: read-only, and
		// putting the default back is not its job.
		if !strings.Contains(tool.Description, "唯讀") {
			t.Errorf("catalog %q description must say it is read-only: %q",
				seedToolName, tool.Description)
		}
		if !strings.Contains(tool.Description, "不提供 restore") {
			t.Errorf("catalog %q description must say restoring the default is "+
				"NOT an agent tool, as list_document_history's does: %q",
				seedToolName, tool.Description)
		}
	}
	if !found {
		t.Fatalf("%q is missing from spec/mcp-catalog.json — agents reach tools "+
			"only through tools/list, so it does not exist for them", seedToolName)
	}
}

// Opening the read must not have opened its destructive siblings. Asserted
// POSITIVELY on the rows themselves (the row exists AND is excluded), because
// "no restore tool in the catalog" would also pass if the restore route had
// been deleted, or renamed, or never registered.
func TestDocumentSeedToolOpenedNoDestructiveSibling(t *testing.T) {
	restore := routeSpecFor(t, http.MethodPost, "/api/document-history/{kind}/{key}/{id}/restore")
	if !restore.MCPExclude {
		t.Fatalf("restore is on the tool surface — owner ruling rc-b5fd1135e2dd "+
			"keeps WRITING a version back off it (MCPTool=%q)", restore.MCPTool)
	}
	index := mcpToolIndex(defaultRouteSpecs())
	if _, ok := index[seedToolName]; !ok {
		t.Fatalf("control assertion: %q must be a tool here, or the checks below "+
			"prove nothing about a surface that gained the read", seedToolName)
	}
	for name, spec := range index {
		if spec.Method == http.MethodGet {
			continue
		}
		if strings.Contains(spec.Path, "/api/document-history/") {
			t.Errorf("tool %q writes the document-history surface (%s %s) — no "+
				"write verb of this family belongs to agents",
				name, spec.Method, spec.Path)
		}
	}
}

// The agent channel end to end: an AGENT-scope token calls the tool through the
// real loopback (same gate, same RBAC choke, same handler) and gets the shipped
// default back — and the live document is byte-identical afterwards, with no
// revision retained. Looking is not restoring, measured as an outcome rather
// than as "the handler has no write in it".
func TestDocumentSeedToolReadsTheDefaultAndWritesNothing(t *testing.T) {
	srv, secret, _ := newWiredTestServer(t)
	now := time.Now().Unix()
	agentTok, _ := mintJWT("kyle", "agent", 300, secret, now, "")
	ownerTok, _ := mintJWT("owner", "owner", 300, secret, now, "")

	// Put the role somewhere the FILE seed is not, so "returned the seed" and
	// "returned the live document" cannot be the same bytes.
	const rewrite = "owner's rewrite that the seed must not resemble"
	writeResult := postMCP(t, srv.URL, ownerTok,
		`{"jsonrpc":"2.0","id":0,"method":"tools/call","params":{"name":"update_role",`+
			`"arguments":{"role":"assistant","definition_md":"`+rewrite+`"}}}`,
	)["result"].(map[string]any)
	if writeResult["isError"] != false {
		t.Fatalf("role write: %v", writeResult)
	}
	liveBefore := roleDefinitionText(t, srv.URL, ownerTok)
	if liveBefore != rewrite {
		t.Fatalf("live definition = %q, want the rewrite", liveBefore)
	}
	historyBefore := documentHistoryCount(t, srv.URL, ownerTok)

	payload := postMCP(t, srv.URL, agentTok,
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"`+seedToolName+
			`","arguments":{"kind":"role_definition","key":"assistant"}}}`)
	if err, present := payload["error"]; present {
		t.Fatalf("an agent must reach this read (machine floor), got RPC error: %v", err)
	}
	result := payload["result"].(map[string]any)
	if result["isError"] != false {
		t.Fatalf("agent tools/call %s: %v", seedToolName, result)
	}
	structured, ok := result["structuredContent"].(map[string]any)
	if !ok {
		t.Fatalf("a JSON object body must carry structuredContent: %v", result)
	}
	content, ok := structured["content"].(map[string]any)
	if !ok {
		t.Fatalf("seed result carries no content map: %v", structured)
	}
	// Same field name a retained revision uses — that is what lets one reader
	// compare either against the live document.
	seedText, ok := content["definition_md"].(string)
	if !ok {
		t.Fatalf("seed content must carry definition_md: %v", content)
	}
	if seedText == "" {
		t.Fatalf("the assistant role ships a file seed; empty means the tool " +
			"answered from the live document, not from the file")
	}
	if seedText == rewrite {
		t.Fatalf("the tool returned the LIVE definition, not the shipped default")
	}

	// Nothing moved.
	if after := roleDefinitionText(t, srv.URL, ownerTok); after != rewrite {
		t.Fatalf("reading the default through MCP CHANGED the document: %q -> %q",
			rewrite, after)
	}
	if after := documentHistoryCount(t, srv.URL, ownerTok); after != historyBefore {
		t.Fatalf("reading the default retained a revision: history %d -> %d "+
			"(a read that writes history is a write)", historyBefore, after)
	}
}

func roleDefinitionText(t *testing.T, url, token string) string {
	t.Helper()
	result := postMCP(t, url, token,
		`{"jsonrpc":"2.0","id":8,"method":"tools/call","params":{"name":"get_role",`+
			`"arguments":{"role":"assistant"}}}`)["result"].(map[string]any)
	if result["isError"] != false {
		t.Fatalf("get_role: %v", result)
	}
	text, _ := result["structuredContent"].(map[string]any)["definition_md"].(string)
	return text
}

// Counted over the wire through the sibling read tool's own route, so the
// number comes from the same place the cockpit's list does.
func documentHistoryCount(t *testing.T, url, token string) int {
	t.Helper()
	payload := postMCP(t, url, token,
		`{"jsonrpc":"2.0","id":9,"method":"tools/call","params":{"name":"list_document_history",`+
			`"arguments":{"kind":"role_definition","key":"assistant"}}}`)
	result := payload["result"].(map[string]any)
	if result["isError"] != false {
		t.Fatalf("list_document_history: %v", result)
	}
	text := result["content"].([]any)[0].(map[string]any)["text"].(string)
	var versions []any
	if err := json.Unmarshal([]byte(text), &versions); err != nil {
		t.Fatalf("history body is not a top-level array: %v %s", err, text)
	}
	return len(versions)
}
