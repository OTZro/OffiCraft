package main

import (
	"strings"
	"testing"
	"time"
)

func TestToolsCallMissingTaskManualTypeKeyNamesTheField(t *testing.T) {
	srv, secret, _ := newWiredTestServer(t)
	tok, err := mintJWT("owner", "owner", 300, secret, time.Now().Unix(), "")
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name string
		body string
	}{
		{"write_task_learnings", `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"write_task_learnings","arguments":{"text":"probe"}}}`},
		{"patch_task_learnings", `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"patch_task_learnings","arguments":{"edits":[]}}}`},
		{"get_task_manual", `{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"get_task_manual","arguments":{}}}`},
		{"write_task_learnings_empty", `{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"write_task_learnings","arguments":{"type_key":"","text":"probe"}}}`},
		{"patch_task_learnings_empty", `{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"patch_task_learnings","arguments":{"type_key":"","edits":[]}}}`},
		{"get_task_manual_empty", `{"jsonrpc":"2.0","id":6,"method":"tools/call","params":{"name":"get_task_manual","arguments":{"type_key":""}}}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			payload := postMCP(t, srv.URL, tok, tc.body)
			result, ok := payload["result"].(map[string]any)
			if !ok {
				t.Fatalf("missing path argument must be a CallToolResult, got %v", payload)
			}
			if result["isError"] != true {
				t.Fatalf("missing path argument must be an error result: %v", result)
			}
			structured, ok := result["structuredContent"].(map[string]any)
			if !ok {
				t.Fatalf("validation refusal must carry structured content: %v", result)
			}
			errObj, ok := structured["error"].(map[string]any)
			if !ok || errObj["code"] != "validation_error" || errObj["message"] != "field required: type_key" {
				t.Fatalf("missing type_key must name the field, got %v", result)
			}
			text := result["content"].([]any)[0].(map[string]any)["text"].(string)
			if !strings.Contains(text, "type_key") || strings.Contains(text, "unknown field") || strings.Contains(text, "not found") {
				t.Fatalf("raw result must carry the same precise refusal, got %q", text)
			}
		})
	}
}
