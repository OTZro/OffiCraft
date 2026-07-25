package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type bufferWriteCloser struct{ bytes.Buffer }

func (b *bufferWriteCloser) Close() error { return nil }

func TestBuildCodexLaunchCommandKeepsTokenOutOfArgv(t *testing.T) {
	got := buildCodexLaunchCommand(
		"/opt/officraft/ocwarden",
		"/opt/homebrew/bin/codex",
		"/tmp/member-m-1",
		"/tmp/member-m-1/persona.md",
		"/tmp/member-m-1/.oc-token",
		"m-1",
		"http://127.0.0.1:7755",
		"member-m-1",
		"officraft-e2e",
		"",
		"high",
		nil,
		"",
	)
	for _, want := range []string{
		`OC_TOKEN="$(/bin/cat /tmp/member-m-1/.oc-token)"`,
		"exec /opt/officraft/ocwarden codex-session",
		"--codex-bin /opt/homebrew/bin/codex",
		"--effort high",
		"OC_ID=m-1",
		"OC_TMUX_SOCKET=officraft-e2e",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("launch command missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "Bearer ") {
		t.Fatalf("launch argv must not carry credential material: %s", got)
	}
}

func TestNormalizeCodexEffort(t *testing.T) {
	for input, want := range map[string]string{
		"": "medium", "low": "low", "medium": "medium", "high": "high",
		"extreme": "medium",
	} {
		if got := normalizeCodexEffort(input); got != want {
			t.Errorf("%q: got %q want %q", input, got, want)
		}
	}
}

func TestRequestUserInputBridgeCreatesOneCardPerQuestion(t *testing.T) {
	var payloads []map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer member-token" {
			t.Errorf("authorization = %q", got)
		}
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Errorf("decode card: %v", err)
		}
		payloads = append(payloads, payload)
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "rc-created"})
	}))
	defer server.Close()

	out := &bufferWriteCloser{}
	session := &codexSession{in: out, base: server.URL, token: "member-token"}
	session.handleServerRequest(appServerMessage{
		"id": "server-request-7", "method": "item/tool/requestUserInput",
		"params": map[string]any{"questions": []any{
			map[string]any{
				"id": "q1", "header": "Choose", "question": "Which path?",
				"options": []any{map[string]any{"label": "A"}, map[string]any{"label": "B"}},
			},
			map[string]any{
				"id": "q2", "header": "Credential", "question": "Paste the token",
				"isSecret": true,
			},
		}},
	})
	if len(payloads) != 2 {
		t.Fatalf("created %d cards, want one per question", len(payloads))
	}
	if payloads[0]["bind"] != "" || payloads[1]["bind"] != "none" {
		t.Fatalf("only the first card may auto-bind: %#v", payloads)
	}
	if payloads[1]["kind"] != "action" ||
		!strings.Contains(payloads[1]["body"].(string), "不要把秘密貼進卡片") {
		t.Fatalf("secret request must become a no-secret action card: %#v", payloads[1])
	}
	response, err := io.ReadAll(out)
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(response), &decoded); err != nil {
		t.Fatalf("decode App Server response: %v (%s)", err, response)
	}
	if decoded["id"] != "server-request-7" {
		t.Fatalf("server request id was not echoed exactly: %#v", decoded)
	}
}

func TestReportTokenUsageUsesLatestTurnForContextGauge(t *testing.T) {
	var contextBody map[string]any
	var telemetryBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode %s: %v", r.URL.Path, err)
		}
		switch r.URL.Path {
		case "/api/agent/context":
			contextBody = body
		case "/api/monitoring/telemetry":
			telemetryBody = body
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	session := &codexSession{base: server.URL, token: "member-token", effort: "low"}
	session.reportTokenUsage(map[string]any{
		"tokenUsage": map[string]any{
			"modelContextWindow": float64(1000),
			"last":               map[string]any{"totalTokens": float64(250)},
			"total": map[string]any{
				"inputTokens": float64(1100), "cachedInputTokens": float64(700),
				"outputTokens": float64(50), "reasoningOutputTokens": float64(20),
				"totalTokens": float64(1150),
			},
		},
	})
	if got := contextBody["context_pct"]; got != float64(25) {
		t.Fatalf("context_pct = %#v, want latest-turn 25 (not cumulative 115)", got)
	}
	tokens, _ := telemetryBody["tokens"].(map[string]any)
	if got := tokens["totalTokens"]; got != float64(1150) {
		t.Fatalf("telemetry totalTokens = %#v, want cumulative thread total", got)
	}
}

func TestActionableCodexListenerLineFiltersTransportDiagnostics(t *testing.T) {
	for line, want := range map[string]bool{
		"[ocagent] listen: connected — streaming http://127.0.0.1": false,
		"[ocagent] listen: stream ended: EOF":                      false,
		"[ocagent] chat from owner (id, 1s ago): hello":            true,
		"[ocagent] task T-1 updated · by owner":                    true,
	} {
		if got := actionableCodexListenerLine(line); got != want {
			t.Errorf("%q: got %v want %v", line, got, want)
		}
	}
}

type runtimeProbeRunner struct{}

func (runtimeProbeRunner) Run(name string, args ...string) (string, error) {
	joined := strings.Join(args, " ")
	switch {
	case strings.HasSuffix(name, "codex") && joined == "--version":
		return "codex-cli 0.145.0", nil
	case strings.HasSuffix(name, "codex") && joined == "login status":
		return "Logged in", nil
	default:
		return "", errors.New("unexpected")
	}
}

func TestRuntimeCapabilitiesShape(t *testing.T) {
	codexPath := filepath.Join(t.TempDir(), "codex")
	if err := os.WriteFile(codexPath, []byte("#!/bin/sh\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	env := func(key string) string {
		switch key {
		case "OC_CODEX_BIN":
			return codexPath
		case "HOME":
			return "/tmp"
		default:
			return ""
		}
	}
	got := collectRuntimeCapabilities(env, runtimeProbeRunner{}, map[string]any{})
	codex := got["codex"].(map[string]any)
	if installed, _ := codex["installed"].(bool); !installed {
		t.Fatalf("executable Codex override must report installed: %#v", codex)
	}
	if loggedIn, _ := codex["logged_in"].(bool); !loggedIn {
		t.Fatalf("successful login probe must report logged in: %#v", codex)
	}
	if codex["version"] != "0.145.0" {
		t.Fatalf("unexpected Codex version capability: %#v", codex)
	}
}
