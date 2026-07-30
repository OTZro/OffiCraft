package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"
)

// ── the frozen-schema coupling test (T-6b42) ────────────────────────────────
//
// This is the test whose ABSENCE let the Claude reporter go silently dark.
//
// The two ingest schemas in spec/openapi.json declare additionalProperties:false,
// and the server decodes every mutable write with DisallowUnknownFields. So a
// single key this reporter sends that the schema does not declare does not get
// dropped — it rejects the ENTIRE report. Usage, cost, and account identity all
// die together, the reporter still exits 0, and the throttle stamp still advances,
// so nothing anywhere looks wrong.
//
// The reporter and the schema live in different Go modules and cannot import each
// other, which is exactly why this drift was invisible to the compiler. This test
// re-couples them the only way available: it reads the frozen spec off disk and
// checks the real POST bodies against it. No server, no network.

// frozenIngestProperties loads the declared property names of one request schema
// from the frozen spec, and asserts the schema really is closed (a schema that
// tolerated extra keys would make this whole test vacuous).
func frozenIngestProperties(t *testing.T, schemaName string) map[string]bool {
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
	schema, ok := spec.Components.Schemas[schemaName]
	if !ok {
		t.Fatalf("%s not in the frozen spec", schemaName)
	}
	if schema.AdditionalProperties == nil || *schema.AdditionalProperties {
		t.Fatalf("%s is not a closed schema — this guard would be vacuous", schemaName)
	}
	declared := map[string]bool{}
	for name := range schema.Properties {
		declared[name] = true
	}
	return declared
}

func undeclaredKeys(body string, declared map[string]bool) []string {
	var obj map[string]json.RawMessage
	if json.Unmarshal([]byte(body), &obj) != nil {
		return []string{"<body is not a JSON object>"}
	}
	var extra []string
	for key := range obj {
		if !declared[key] {
			extra = append(extra, key)
		}
	}
	sort.Strings(extra)
	return extra
}

// TestContextReportBodiesMatchFrozenIngestSchemas drives the real reporter over
// the payload shapes a live Claude Code session actually produces, and asserts
// every POST body is accepted by the frozen schema. The first case is the exact
// shape observed on the owner's machine (a null context percentage next to a
// genuinely-zero cost) — the shape under which the whole report was being thrown
// away.
func TestContextReportBodiesMatchFrozenIngestSchemas(t *testing.T) {
	contextDeclared := frozenIngestProperties(t, "AgentContextIngestDTO")
	telemetryDeclared := frozenIngestProperties(t, "AgentTelemetryIngestDTO")

	home := writeClaudeJSON(t,
		`{"userID":"acct-1","oauthAccount":{"emailAddress":"e@x.io","organizationName":"Org","organizationUuid":"org-1"}}`)
	today := transcriptToday(t)

	cases := []struct {
		name    string
		payload string
	}{
		{
			// The real observed shape: no usable context percentage, cost a true 0.
			name:    "null pct and zero cost",
			payload: `{"context_window":{"used_percentage":null},"cost":{"total_cost_usd":0}}`,
		},
		{
			name:    "pct only",
			payload: `{"context_window":{"used_percentage":28.93}}`,
		},
		{
			name: "everything measured",
			payload: `{"context_window":{"used_percentage":41.5},
				"cost":{"total_cost_usd":1.25,"total_duration_ms":90000},
				"rate_limits":{"five_hour":{"used_percentage":30,"resets_at":1720000000},
				               "seven_day":{"used_percentage":60,"resets_at":1720500000}},
				"model":{"display_name":"Opus","id":"claude-opus-5"},
				"transcript_path":"` + today + `"}`,
		},
		{
			name:    "nothing measurable at all",
			payload: `{}`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv, posts := contextServer(t)
			cfg := Config{Base: srv.URL, Token: "t", ID: "kyle", Home: t.TempDir()}
			var out, errOut bytes.Buffer
			cmdContextReport(srv.Client(), cfg,
				testEnv(map[string]string{"HOME": home, "OC_HOST": "lab-1"}),
				1000.0, strings.NewReader(tc.payload), &out, &errOut)

			// Identity must be reported for EVERY shape, including the last one where
			// nothing at all was measurable.
			tel := findPost(*posts, "/api/monitoring/telemetry")
			if tel == nil {
				t.Fatalf("no telemetry POST; posts=%v", *posts)
			}
			if extra := undeclaredKeys(tel.body, telemetryDeclared); len(extra) > 0 {
				t.Errorf("telemetry body has keys the frozen schema refuses %v — the whole "+
					"report (usage AND account) would 422; body=%s", extra, tel.body)
			}
			if ctx := findPost(*posts, "/api/agent/context"); ctx != nil {
				if extra := undeclaredKeys(ctx.body, contextDeclared); len(extra) > 0 {
					t.Errorf("context body has keys the frozen schema refuses %v — the gauge "+
						"would 422; body=%s", extra, ctx.body)
				}
			}
		})
	}
}

// TestContextReportSendsSessionEffort pins the session's OWN reasoning effort onto
// the telemetry body. It was never sent: OC_EFFORT only ever reached the status
// line string, so every Claude session reported a blank effort forever while the
// server, the frozen schema and the monitoring page were all already wired for it
// — and a blank effort is indistinguishable from a session that simply has not
// reported yet, which is why it survived unnoticed until the owner spotted it on
// screen. The wire value is VERBATIM: the status line's "med" abbreviation must
// never leak onto it.
func TestContextReportSendsSessionEffort(t *testing.T) {
	home := writeClaudeJSON(t, `{"userID":"acct-1"}`)

	cases := []struct {
		name   string
		effort string
		want   any
	}{
		{name: "reported verbatim", effort: "medium", want: "medium"},
		{name: "non-default passes through", effort: "high", want: "high"},
		{name: "unset is omitted, never a blank", effort: "", want: nil},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv, posts := contextServer(t)
			cfg := Config{Base: srv.URL, Token: "t", ID: "kyle", Home: t.TempDir()}
			env := map[string]string{"HOME": home, "OC_HOST": "lab-1"}
			if tc.effort != "" {
				env["OC_EFFORT"] = tc.effort
			}
			var out, errOut bytes.Buffer
			cmdContextReport(srv.Client(), cfg, testEnv(env), 1000.0,
				strings.NewReader(`{"context_window":{"used_percentage":41.5}}`), &out, &errOut)

			tel := findPost(*posts, "/api/monitoring/telemetry")
			if tel == nil {
				t.Fatalf("no telemetry POST; posts=%v", *posts)
			}
			var body map[string]any
			if err := json.Unmarshal([]byte(tel.body), &body); err != nil {
				t.Fatalf("telemetry body is not JSON: %v", err)
			}
			if got := body["effort"]; got != tc.want {
				t.Errorf("telemetry effort = %v, want %v; body=%s", got, tc.want, tel.body)
			}
			// The context gauge DTO does not declare effort and refuses undeclared
			// keys, so one stray copy there would 422 the whole gauge POST.
			if ctx := findPost(*posts, "/api/agent/context"); ctx != nil {
				if strings.Contains(ctx.body, "effort") {
					t.Errorf("effort rode the context POST; it would 422; body=%s", ctx.body)
				}
			}
		})
	}
}

// transcriptToday writes a one-row transcript dated today so the tokens source is
// live, and returns its path.
func transcriptToday(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "t.jsonl")
	day := nowUTCDate()
	line := `{"type":"assistant","timestamp":"` + day +
		`T10:00:00Z","message":{"usage":{"input_tokens":7,"output_tokens":3}}}`
	if err := os.WriteFile(path, []byte(line+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// TestContextReportSurfacesRefusedPost: a refused POST must leave a trace on
// STDERR while the status line still goes to stdout untouched and the exit code
// stays 0. The old reporter discarded the status entirely, so hours of 422s were
// indistinguishable from hours of healthy reporting — the throttle stamp kept
// advancing and the process kept exiting 0, which is the failure mode that made
// this bug survive three separate investigations.
func TestContextReportSurfacesRefusedPost(t *testing.T) {
	var refused []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		refused = append(refused, r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(422)
		_, _ = w.Write([]byte(`{"error":{"code":"validation_error",` +
			`"message":"invalid request body: json: unknown field \"agent_id\""}}`))
	}))
	t.Cleanup(srv.Close)

	cfg := Config{Base: srv.URL, Token: "t", ID: "kyle", Home: t.TempDir()}
	var out, errOut bytes.Buffer
	rc := cmdContextReport(srv.Client(), cfg, testEnv(nil), 1000.0,
		strings.NewReader(`{"context_window":{"used_percentage":28.93}}`), &out, &errOut)

	if rc != 0 {
		t.Errorf("rc = %d, want 0 — a refused report must never break the status line", rc)
	}
	if len(refused) != 2 {
		t.Fatalf("expected both POSTs attempted, saw %v", refused)
	}
	diag := errOut.String()
	for _, want := range []string{"/api/agent/context", "/api/monitoring/telemetry", "422"} {
		if !strings.Contains(diag, want) {
			t.Errorf("stderr diagnostic missing %q; got %q", want, diag)
		}
	}
	if !strings.Contains(diag, "unknown field") {
		t.Errorf("stderr must carry the server's own explanation; got %q", diag)
	}
	// stdout stays the status line ALONE — Claude Code renders it verbatim.
	if strings.Contains(out.String(), "FAILED") {
		t.Errorf("diagnostics must not pollute the status line; stdout = %q", out.String())
	}
	if got := stripANSI(out.String()); !strings.Contains(got, "29%") {
		t.Errorf("status line = %q, want the rendered pct", got)
	}
}

// nowUTCDate is the UTC day parseTranscriptTokens filters on.
func nowUTCDate() string { return time.Now().UTC().Format("2006-01-02") }
