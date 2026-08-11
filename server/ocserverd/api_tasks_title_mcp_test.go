package main

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"
)

// ── T-2ebe:MCP 端到端 ────────────────────────────────────────────────────────
//
// Every other title test calls the handler directly, which proves nothing about
// the four things standing between an agent and that handler:
//
//	① the tool NAME update_task_title resolving to this route at all
//	   (mcpToolIndex, derived from the route table). A tool missing from the
//	   index is "tool not found" for every agent while every handler test stays
//	   green.
//	② splitToolArguments putting `task_id` in the PATH and `title` in the BODY.
//	③ the auth gate + RBAC choke the loopback re-enters carrying the CALLER's own
//	   token — which is what makes the refusal below a statement about MCP
//	   callers rather than a hand-stamped claims context.
//	④ the REST→MCP result mapping: a 4xx rides as a successful JSON-RPC result
//	   carrying isError, never an RPC error.
//
// An agent's only route into this feature is this path, so this is the only test
// that says the capability is REACHABLE.

// callTitleTool drives one real tools/call through the wired mux.
func callTitleTool(t *testing.T, url, token, taskID, title string) map[string]any {
	t.Helper()
	args, err := json.Marshal(map[string]any{"task_id": taskID, "title": title})
	if err != nil {
		t.Fatal(err)
	}
	return postMCP(t, url, token, `{"jsonrpc":"2.0","id":1,"method":"tools/call",`+
		`"params":{"name":"update_task_title","arguments":`+string(args)+`}}`)
}

func readTitleOverREST(t *testing.T, srv, token, taskID string) string {
	t.Helper()
	req, _ := http.NewRequest("GET", srv+"/api/tasks/"+taskID, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out struct {
		Title string `json:"title"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	return out.Title
}

// TestToolsCallUpdateTaskTitleAcceptsTheExecutor is the acceptance half: the
// seeded Mira executes the task and corrects its own ticket's title through the
// tool, and the STORED title changes — read back through a SECOND, independent
// call so the assertion does not rest on the write's own echo.
func TestToolsCallUpdateTaskTitleAcceptsTheExecutor(t *testing.T) {
	srv, secret, _ := newWiredTestServer(t)
	now := time.Now().Unix()
	ownerTok, _ := mintJWT("owner", "owner", 300, secret, now, "")
	miraTok, _ := mintJWT("mira", "agent", 300, secret, now, "")
	taskID := mcpTaskFixture(t, srv.URL, ownerTok)

	_, isError, text := toolResult(t,
		callTitleTool(t, srv.URL, miraTok, taskID, "透過 MCP 更正的標題"))
	if isError {
		t.Fatalf("the executor's own tool call must be accepted: %s", text)
	}
	if !strings.Contains(text, "透過 MCP 更正的標題") {
		t.Fatalf("tool result must echo the stored title: %s", text)
	}
	if got := readTitleOverREST(t, srv.URL, ownerTok, taskID); got != "透過 MCP 更正的標題" {
		t.Fatalf("title read back = %q", got)
	}
}

// TestToolsCallUpdateTaskTitleRefusesANonExecutor is the refusal half, and it
// asserts the REASON: the unified 403 envelope reaches the agent intact through
// the loopback rather than collapsing into a bare failure — and nothing was
// written, which a status-only assertion would not notice.
func TestToolsCallUpdateTaskTitleRefusesANonExecutor(t *testing.T) {
	srv, secret, _ := newWiredTestServer(t)
	now := time.Now().Unix()
	ownerTok, _ := mintJWT("owner", "owner", 300, secret, now, "")
	strangerTok, _ := mintJWT("kyle", "agent", 300, secret, now, "")
	taskID := mcpTaskFixture(t, srv.URL, ownerTok)
	before := readTitleOverREST(t, srv.URL, ownerTok, taskID)
	if before == "" {
		t.Fatal("fixture: the created task must already have a title")
	}

	result, isError, text := toolResult(t,
		callTitleTool(t, srv.URL, strangerTok, taskID, "不該寫進去的標題"))
	if !isError {
		t.Fatalf("a non-executor's tool call must be refused: %s", text)
	}
	structured, ok := result["structuredContent"].(map[string]any)
	if !ok {
		t.Fatalf("a JSON refusal must carry structuredContent: %v", result)
	}
	envelope, ok := structured["error"].(map[string]any)
	if !ok {
		t.Fatalf("refusal must carry the unified error envelope: %v", structured)
	}
	if envelope["code"] != "forbidden" {
		t.Fatalf("error code = %v, want forbidden", envelope["code"])
	}
	if msg, _ := envelope["message"].(string); !strings.Contains(msg, "not the task's executor") {
		t.Fatalf("error message = %q, want the executor-guard reason", msg)
	}
	if got := readTitleOverREST(t, srv.URL, ownerTok, taskID); got != before {
		t.Fatalf("refused tool call still wrote: %q", got)
	}
}

// TestToolsCallUpdateTaskTitleRefusesABlankTitle carries the ticket's one
// deliberate asymmetry onto the surface an agent actually holds: the blank
// refusal must survive the loopback as a 400 result carrying isError, and the
// stored title must be untouched. Its description twin CLEARS on a blank, so a
// copy-paste of that handler would answer 200 here and empty the task-list row.
func TestToolsCallUpdateTaskTitleRefusesABlankTitle(t *testing.T) {
	srv, secret, _ := newWiredTestServer(t)
	now := time.Now().Unix()
	ownerTok, _ := mintJWT("owner", "owner", 300, secret, now, "")
	miraTok, _ := mintJWT("mira", "agent", 300, secret, now, "")
	taskID := mcpTaskFixture(t, srv.URL, ownerTok)
	before := readTitleOverREST(t, srv.URL, ownerTok, taskID)

	for _, blank := range []string{"", "   ", "\t\n"} {
		_, isError, text := toolResult(t, callTitleTool(t, srv.URL, miraTok, taskID, blank))
		if !isError {
			t.Fatalf("a blank title %q must be refused through MCP too: %s", blank, text)
		}
		if !strings.Contains(text, "title") {
			t.Fatalf("the refusal must name the field: %s", text)
		}
		if got := readTitleOverREST(t, srv.URL, ownerTok, taskID); got != before {
			t.Fatalf("a refused blank still wrote: title = %q", got)
		}
	}
}
