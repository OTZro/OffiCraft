package main

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"
)

// ── T-e271 節點 5:MCP 端到端 ────────────────────────────────────────────────
//
// Everything else about this route is exercised by calling its handler
// directly. That is NOT enough, and the gap is specific rather than
// philosophical: a handler test constructs the request itself, so it proves
// nothing about the four things standing between an agent and that handler —
//
//	① the tool NAME resolving to this route at all (mcpToolIndex, derived from
//	   the route table). A tool missing from the index is a "tool not found"
//	   for every agent while every handler test stays green.
//	② splitToolArguments putting `task_id` in the PATH and `description` in the
//	   BODY. Get that wrong and the handler is reached with an empty id.
//	③ the auth gate + RBAC choke the loopback re-enters, carrying the CALLER's
//	   own token — which is what makes the 403 in this file a statement about
//	   MCP callers and not just about a hand-stamped claims context.
//	④ the REST→MCP result mapping: a 4xx becomes a successful JSON-RPC result
//	   carrying isError, never an RPC error.
//
// An agent's only route into this feature is the path below, so this is the
// only test in the ticket that says the feature is REACHABLE.

// callDescriptionTool drives one real tools/call through the wired mux.
func callDescriptionTool(t *testing.T, url, token, taskID, description string) map[string]any {
	t.Helper()
	args, err := json.Marshal(map[string]any{
		"task_id": taskID, "description": description,
	})
	if err != nil {
		t.Fatal(err)
	}
	return postMCP(t, url, token, `{"jsonrpc":"2.0","id":1,"method":"tools/call",`+
		`"params":{"name":"update_task_description","arguments":`+string(args)+`}}`)
}

// toolResult unwraps the JSON-RPC envelope, insisting there is no RPC-level
// error (a route 4xx must ride as a RESULT with isError, per spec/mcp.md §3).
func toolResult(t *testing.T, payload map[string]any) (map[string]any, bool, string) {
	t.Helper()
	if rpcErr, present := payload["error"]; present {
		t.Fatalf("a route refusal must NOT become a JSON-RPC error: %v", rpcErr)
	}
	result, ok := payload["result"].(map[string]any)
	if !ok {
		t.Fatalf("no result in MCP payload: %v", payload)
	}
	isError, _ := result["isError"].(bool)
	text := ""
	if content, ok := result["content"].([]any); ok && len(content) == 1 {
		if entry, ok := content[0].(map[string]any); ok {
			text, _ = entry["text"].(string)
		}
	}
	return result, isError, text
}

// mcpTaskFixture creates one task through the REST surface and hands back its
// id, its executor and a creator who is NOT the executor.
func mcpTaskFixture(t *testing.T, srv, ownerTok string) (taskID string) {
	t.Helper()
	body := strings.NewReader(`{"title":"mcp desc task","executor_member_id":"mira"}`)
	req, err := http.NewRequest("POST", srv+"/api/tasks", body)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+ownerTok)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out struct {
		Task struct {
			ID string `json:"id"`
		} `json:"task"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 200 || out.Task.ID == "" {
		t.Fatalf("create task through REST: %d", resp.StatusCode)
	}
	return out.Task.ID
}

// TestToolsCallUpdateTaskDescriptionAcceptsTheExecutor is the acceptance half:
// the seeded Mira executes the task and corrects its own ticket's wording
// through the tool, and the STORED text changes.
func TestToolsCallUpdateTaskDescriptionAcceptsTheExecutor(t *testing.T) {
	srv, secret, _ := newWiredTestServer(t)
	now := time.Now().Unix()
	ownerTok, _ := mintJWT("owner", "owner", 300, secret, now, "")
	miraTok, _ := mintJWT("mira", "agent", 300, secret, now, "")
	taskID := mcpTaskFixture(t, srv.URL, ownerTok)

	_, isError, text := toolResult(t,
		callDescriptionTool(t, srv.URL, miraTok, taskID, "透過 MCP 更正的敘述"))
	if isError {
		t.Fatalf("the executor's own tool call must be accepted: %s", text)
	}
	// The receipt is the task itself — and it must carry the NEW text, so this
	// cannot pass on a call that was merely routed and did nothing.
	if !strings.Contains(text, "透過 MCP 更正的敘述") {
		t.Fatalf("tool result must echo the stored description: %s", text)
	}
	// Read it back through a SECOND, independent call so the assertion does not
	// rest on the write's own echo.
	if got := readDescriptionOverREST(t, srv.URL, ownerTok, taskID); got != "透過 MCP 更正的敘述" {
		t.Fatalf("description read back = %q", got)
	}
}

// TestToolsCallUpdateTaskDescriptionRefusesANonExecutor is the refusal half,
// and it asserts the REASON: the unified 403 envelope reaches the agent intact
// through the loopback rather than collapsing into a bare failure.
func TestToolsCallUpdateTaskDescriptionRefusesANonExecutor(t *testing.T) {
	srv, secret, _ := newWiredTestServer(t)
	now := time.Now().Unix()
	ownerTok, _ := mintJWT("owner", "owner", 300, secret, now, "")
	strangerTok, _ := mintJWT("kyle", "agent", 300, secret, now, "")
	taskID := mcpTaskFixture(t, srv.URL, ownerTok)

	result, isError, text := toolResult(t,
		callDescriptionTool(t, srv.URL, strangerTok, taskID, "不該寫進去的敘述"))
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
	// And nothing was written — a refusal that still wrote would be the worst
	// possible outcome and a status-only assertion would not notice.
	if got := readDescriptionOverREST(t, srv.URL, ownerTok, taskID); got != "" {
		t.Fatalf("refused tool call still wrote: %q", got)
	}
}

// TestToolsCallUpdateTaskDescriptionOnAClosedTask carries ruling ② onto the
// agent-facing surface: the tool an agent actually holds must work after the
// task closes, not only the handler underneath it.
func TestToolsCallUpdateTaskDescriptionOnAClosedTask(t *testing.T) {
	srv, secret, _ := newWiredTestServer(t)
	now := time.Now().Unix()
	ownerTok, _ := mintJWT("owner", "owner", 300, secret, now, "")
	miraTok, _ := mintJWT("mira", "agent", 300, secret, now, "")
	taskID := mcpTaskFixture(t, srv.URL, ownerTok)

	// Close it through the real terminate route (owner-gated).
	req, _ := http.NewRequest("POST", srv.URL+"/api/tasks/"+taskID+"/terminate", nil)
	req.Header.Set("Authorization", "Bearer "+ownerTok)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("terminate: %d", resp.StatusCode)
	}

	_, isError, text := toolResult(t,
		callDescriptionTool(t, srv.URL, miraTok, taskID, "結案後透過 MCP 更正"))
	if isError {
		t.Fatalf("a closed task must still accept the description tool: %s", text)
	}
	if got := readDescriptionOverREST(t, srv.URL, ownerTok, taskID); got != "結案後透過 MCP 更正" {
		t.Fatalf("closed-task description read back = %q", got)
	}

	// The control, on the SAME closed task through the SAME channel: the
	// artifact tool is refused. Without it, "the tool worked" could equally
	// mean the terminal guard is missing everywhere.
	payload := postMCP(t, srv.URL, miraTok,
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"add_task_artifact",`+
			`"arguments":{"task_id":"`+taskID+`","kind":"link","label":"pr","url":"https://example.invalid/1"}}}`)
	if _, artErr, artText := toolResult(t, payload); !artErr {
		t.Fatalf("a closed task's artifact set must stay frozen: %s", artText)
	}
}

func readDescriptionOverREST(t *testing.T, srv, token, taskID string) string {
	t.Helper()
	req, _ := http.NewRequest("GET", srv+"/api/tasks/"+taskID, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out struct {
		Description string `json:"description"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	return out.Description
}
