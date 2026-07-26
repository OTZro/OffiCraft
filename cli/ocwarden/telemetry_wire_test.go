package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"
)

// ── the frozen-schema coupling test (T-6b42) ────────────────────────────────
//
// The server declares AgentTelemetryIngestDTO with additionalProperties:false and
// decodes every mutable write with DisallowUnknownFields. One undeclared key does
// not get dropped — it rejects the WHOLE report. For the warden that means the
// entire machine row (hardware, binary fingerprints, claude probe, runtime
// capabilities) goes null at once, and the 30s producer loop used to discard the
// verdict, so a warden whose every heartbeat was refused looked exactly like a
// healthy one.
//
// The warden and the spec are separate Go modules and cannot import each other,
// which is why this drift was invisible to the compiler. This test reads the
// frozen spec off disk and checks the real payloads against it.

func frozenTelemetryProperties(t *testing.T) map[string]bool {
	t.Helper()
	specPath := filepath.Join("..", "..", "spec", "openapi.json")
	raw, err := os.ReadFile(specPath)
	if err != nil {
		t.Fatalf("read frozen spec %s: %v", specPath, err)
	}
	var spec struct {
		Components struct {
			Schemas map[string]struct {
				Properties           map[string]json.RawMessage `json:"properties"`
				AdditionalProperties *bool                      `json:"additionalProperties"`
			} `json:"schemas"`
		} `json:"components"`
	}
	if err := json.Unmarshal(raw, &spec); err != nil {
		t.Fatalf("parse frozen spec: %v", err)
	}
	schema, ok := spec.Components.Schemas["AgentTelemetryIngestDTO"]
	if !ok {
		t.Fatal("AgentTelemetryIngestDTO not in the frozen spec")
	}
	if schema.AdditionalProperties == nil || *schema.AdditionalProperties {
		t.Fatal("AgentTelemetryIngestDTO is not a closed schema — this guard would be vacuous")
	}
	declared := map[string]bool{}
	for name := range schema.Properties {
		declared[name] = true
	}
	return declared
}

func undeclaredPayloadKeys(payload map[string]any, declared map[string]bool) []string {
	var extra []string
	for key := range payload {
		if !declared[key] {
			extra = append(extra, key)
		}
	}
	sort.Strings(extra)
	return extra
}

// TestWardenTelemetryPayloadsMatchFrozenSchema covers all three payloads the
// warden POSTs to the telemetry endpoint — the heartbeat, the command receipt and
// the self-update announcement — because a single undeclared key kills whichever
// one carries it.
func TestWardenTelemetryPayloadsMatchFrozenSchema(t *testing.T) {
	declared := frozenTelemetryProperties(t)

	heartbeat, err := buildTelemetryPayload("m-1", "lab-1",
		map[string]any{"cpu": "M5"},
		map[string]string{"ocwarden": "abc123abc123"},
		map[string]any{"version": "1.2.3"},
		map[string]any{"claude": map[string]any{"installed": true}})
	if err != nil {
		t.Fatalf("buildTelemetryPayload: %v", err)
	}
	cases := map[string]map[string]any{
		"heartbeat": heartbeat,
		"command_result": {"command_result": map[string]any{
			"member_id": "m-7", "rpc": "stop", "ok": true,
		}},
		"self_update": {"self_update": map[string]any{
			"binary": "ocwarden", "old_hash": "a", "new_hash": "b",
		}},
	}
	for name, payload := range cases {
		if extra := undeclaredPayloadKeys(payload, declared); len(extra) > 0 {
			t.Errorf("%s payload has keys the frozen schema refuses %v — the whole report "+
				"would 422; payload = %#v", name, extra, payload)
		}
	}
	// The heartbeat must still actually carry its measurements (a payload that
	// passed the schema by being empty would be worthless).
	for _, key := range []string{"machine", "hardware", "binaries", "claude", "runtimes"} {
		if _, present := heartbeat[key]; !present {
			t.Errorf("heartbeat dropped %s; payload = %#v", key, heartbeat)
		}
	}
}

// TestRunLogsRefusedTelemetry: a server REFUSAL must reach the log. The producer
// loop has always computed the verdict and thrown it away, so a warden reporting
// into a 422 every 30 seconds — leaving every machine row in the cockpit null —
// was indistinguishable from a healthy one for as long as it ran. A transport
// fault (status 0, i.e. the server is simply down) stays quiet by design.
func TestRunLogsRefusedTelemetry(t *testing.T) {
	cfg := Config{Base: "http://x", Token: "t", ID: "m-1"}
	collect := func() map[string]any { return map[string]any{"cpu": "M5"} }
	machine := func() string { return "lab-1" }
	noSleep := func(context.Context, time.Duration) bool { return true }

	refuse := func(string, map[string]any) (int, map[string]any) {
		return 422, map[string]any{"error": map[string]any{
			"code":    "validation_error",
			"message": `invalid request body: json: unknown field "agent_id"`,
		}}
	}
	var out bytes.Buffer
	run(context.Background(), cfg, collect, machine, refuse, nil, nil, noSleep, 1, &out)
	log := out.String()
	if !strings.Contains(log, "422") || !strings.Contains(log, "unknown field") {
		t.Errorf("a refused heartbeat must log the status AND the server's reason; got %q", log)
	}
	if !strings.Contains(log, "NOT stored") {
		t.Errorf("log must say the report did not land; got %q", log)
	}

	// A server that is merely unreachable must NOT spam the log.
	down := func(string, map[string]any) (int, map[string]any) { return 0, nil }
	var quiet bytes.Buffer
	run(context.Background(), cfg, collect, machine, down, nil, nil, noSleep, 1, &quiet)
	if quiet.Len() != 0 {
		t.Errorf("an unreachable server is expected, not a refusal; log = %q", quiet.String())
	}

	// And a stored report says nothing either.
	okPost := func(string, map[string]any) (int, map[string]any) { return 200, map[string]any{} }
	var silent bytes.Buffer
	run(context.Background(), cfg, collect, machine, okPost, nil, nil, noSleep, 1, &silent)
	if silent.Len() != 0 {
		t.Errorf("a stored report must stay quiet; log = %q", silent.String())
	}
}

// TestCodexTelemetryPayloadsMatchFrozenSchema is the sentinel for the OTHER
// runtime: the Codex sidecar reports through the same endpoint and must stay
// unaffected by the Claude-side fix. Its keys are asserted against the same
// frozen schema, including the runtime-specific ones (codex reports `effort` and
// its own camelCase token names; claude reports neither).
func TestCodexTelemetryPayloadsMatchFrozenSchema(t *testing.T) {
	declared := frozenTelemetryProperties(t)
	cases := map[string]map[string]any{
		"identity": {"runtime": "codex", "account": "codex:abc", "account_label": "ChatGPT"},
		"token usage": {"runtime": "codex", "effort": "medium",
			"account": "codex:abc", "account_label": "ChatGPT",
			"tokens": map[string]any{"inputTokens": 1, "cachedInputTokens": 2}},
		"rate limits": {"runtime": "codex", "account": "codex:abc", "account_label": "ChatGPT",
			"rate_limits": map[string]any{"five_hour": map[string]any{
				"used_percentage": 10.0, "resets_at": 1720000000.0}}},
	}
	for name, payload := range cases {
		if extra := undeclaredPayloadKeys(payload, declared); len(extra) > 0 {
			t.Errorf("codex %s payload has keys the frozen schema refuses %v", name, extra)
		}
	}
}
