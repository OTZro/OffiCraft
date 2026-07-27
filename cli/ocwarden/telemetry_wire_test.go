package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
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

// ── depth (T-90be) ──────────────────────────────────────────────────────────
//
// The check above was TOP-LEVEL ONLY, and the nested blocks declared nothing at
// all: `hardware` was literally {"title": "Hardware"}. Renaming a nested key —
// cpu_pct -> cpu_percent — was accepted (HTTP 200), stored verbatim, and then
// read back as null forever, with the whole suite green. This test's own old
// fixture proved it: it passed `hardware: {"cpu": "M5"}`, a key no consumer has
// ever read, and the guard was happy.
//
// So the spec now declares the nested shape and this walker descends into it.
// Two things keep it honest:
//   - the payloads come from the REAL producers (collectHardware, claudeProber,
//     collectRuntimeCapabilities), not from literals typed in this file. A
//     literal fixture only ever proves the fixture matches the spec, which
//     stays true when the producer is renamed.
//   - the nested declaration is asserted to EXIST (a walker with nothing to
//     descend into silently passes everything).

// ── the VALUE layer (T-aad2) ────────────────────────────────────────────────
//
// Everything above is about KEY NAMES, and that is only half of what the
// declaration says. A producer that keeps the name and changes the TYPE —
// cpu_pct sent as the string "47" instead of the number 47 — walked straight
// through: the nested blocks are open by owner ruling, so the server takes the
// body with a 200 and stores it verbatim, and the reader (which needs a
// float64) then serves null forever. Measured on the real ingest and read paths
// before this guard existed: the resulting machine row was byte-for-byte
// identical to one from a host that has never had a CPU probe.
//
// A rejection at ingest is exactly the fail-closed tightening the owner ruled
// out, so nothing here is refused. This guard is aimed somewhere much narrower:
// OUR OWN producers. It says nothing about what the server accepts from a warden
// in the field, so it costs no tolerance; what it buys is that "we broke our own
// reporter" reddens a build instead of quietly emptying a column.
//
// ⚠️ AND THAT IS ALL IT BUYS — say it plainly, because a guard described more
// broadly than it protects is how this repo has been bitten before. Describing
// it more NARROWLY is its own bug though: it sends the next person to build a
// guard that can never fire. So, measured against the live handler rather than
// assumed, the three declared blocks are covered by three different mechanisms:
//
//	runtimes  — FAIL-CLOSED AT INGEST already, and not by this change: the
//	            handler type-checks installed / logged_in / version per key and
//	            answers a flat 400 (`runtimes.codex.installed must be a
//	            boolean`). A wrong-typed value never reaches the store. This
//	            test is a second, earlier net for our own producers; runtimes is
//	            NOT in the hole, and needs no read-side marker.
//	hardware  — nothing is refused at ingest (owner ruling), so the READ side
//	            carries it: the server names the unreadable key on the wire
//	            (hardware_invalid), and a wrong-typed value from ANY warden,
//	            ours or not, shows up in the cockpit.
//	claude    — THIS TEST AND NOTHING ELSE. `claude: {"version": 9.9}` is
//	            accepted with a 200, stored, and read back as null with nothing
//	            anywhere saying a value was lost. This test only sees payloads
//	            THIS module builds, so an older or third-party warden drifting
//	            there stays invisible at runtime.
//
// That last gap is known and deliberately unfixed (owner ruling: separate
// ticket, not folded into this one). Do not read the green tick below as "the
// value layer is covered" — for claude it means only "our producers are not the
// ones breaking it".

// schemaNode is as much of a JSON-Schema node as this guard needs: the declared
// child properties, the declared value type(s), and whether the node is closed.
type schemaNode struct {
	Properties           map[string]*schemaNode `json:"properties"`
	AdditionalProperties json.RawMessage        `json:"additionalProperties"`
	Type                 string                 `json:"type"`
	AnyOf                []*schemaNode          `json:"anyOf"`
}

// declaredTypes is the set of JSON type names this node accepts, flattening the
// `anyOf: [{type: number}, {type: null}]` shape the spec uses for every nullable
// field. Empty = the node declares no type at all (`{"title": "Binaries"}`), and
// an undeclared node is skipped rather than guessed at — the same rule the key
// walker follows for a block with no declared properties.
func (n *schemaNode) declaredTypes() map[string]bool {
	types := map[string]bool{}
	if n.Type != "" {
		types[n.Type] = true
	}
	for _, alt := range n.AnyOf {
		if alt == nil {
			continue
		}
		for name := range alt.declaredTypes() {
			types[name] = true
		}
	}
	return types
}

// jsonTypeOf names a decoded JSON value's type in JSON-Schema vocabulary. The
// input must have been through encoding/json — the producers hand back Go-native
// values (parseBattery returns an int, binaries is a map[string]string) and it is
// the WIRE type, after marshalling, that the server and the spec are talking
// about.
func jsonTypeOf(value any) string {
	switch value.(type) {
	case nil:
		return "null"
	case bool:
		return "boolean"
	case float64:
		return "number"
	case string:
		return "string"
	case []any:
		return "array"
	case map[string]any:
		return "object"
	}
	return "unknown"
}

// mistypedPayloadValues walks payload against the schema and returns dotted
// paths whose value type is not one the spec declares for that path, formatted
// as `path: got <type>, want <types>`. Undeclared keys are the key walker's
// business, not this one's, so they are skipped here; a declared node with no
// declared type is skipped too.
func mistypedPayloadValues(payload map[string]any, node *schemaNode) []string {
	var bad []string
	var walk func(map[string]any, *schemaNode, string)
	walk = func(obj map[string]any, at *schemaNode, prefix string) {
		for key, value := range obj {
			child, declared := at.Properties[key]
			if !declared || child == nil {
				continue // undeclared — reported by undeclaredPayloadKeys instead
			}
			if want := child.declaredTypes(); len(want) > 0 && !want[jsonTypeOf(value)] {
				names := make([]string, 0, len(want))
				for name := range want {
					names = append(names, name)
				}
				sort.Strings(names)
				bad = append(bad, fmt.Sprintf("%s%s: got %s, want %s",
					prefix, key, jsonTypeOf(value), strings.Join(names, "|")))
				continue
			}
			if nested, isObj := value.(map[string]any); isObj && len(child.Properties) > 0 {
				walk(nested, child, prefix+key+".")
			}
		}
	}
	walk(payload, node, "")
	sort.Strings(bad)
	return bad
}

// onTheWire round-trips a payload through encoding/json, so the walkers see the
// values the SERVER sees rather than the Go types the producers happened to
// build them from. Without this an int battery_pct would be judged against the
// wrong vocabulary — and, worse, would look like a defect while being perfectly
// fine on the wire.
func onTheWire(t *testing.T, payload map[string]any) map[string]any {
	t.Helper()
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("the payload does not even marshal: %v", err)
	}
	var wire map[string]any
	if err := json.Unmarshal(raw, &wire); err != nil {
		t.Fatalf("re-decode the marshalled payload: %v", err)
	}
	return wire
}

// closed reports additionalProperties:false — the setting that makes the SERVER
// refuse an undeclared key instead of storing it.
func (n *schemaNode) closed() bool {
	return strings.TrimSpace(string(n.AdditionalProperties)) == "false"
}

func frozenTelemetrySchema(t *testing.T) *schemaNode {
	t.Helper()
	specPath := filepath.Join("..", "..", "spec", "openapi.json")
	raw, err := os.ReadFile(specPath)
	if err != nil {
		t.Fatalf("read frozen spec %s: %v", specPath, err)
	}
	var spec struct {
		Components struct {
			Schemas map[string]*schemaNode `json:"schemas"`
		} `json:"components"`
	}
	if err := json.Unmarshal(raw, &spec); err != nil {
		t.Fatalf("parse frozen spec: %v", err)
	}
	schema, ok := spec.Components.Schemas["AgentTelemetryIngestDTO"]
	if !ok || schema == nil {
		t.Fatal("AgentTelemetryIngestDTO not in the frozen spec")
	}
	if !schema.closed() {
		t.Fatal("AgentTelemetryIngestDTO is not a closed schema — this guard would be vacuous")
	}
	return schema
}

// undeclaredPayloadKeys walks payload against the schema and returns the
// dotted paths of keys the frozen spec does not declare. It descends wherever
// the schema declares properties for that path; a block that declares none
// (binaries, tokens, command_result, …) is checked at its own level only, so
// this reports drift, not "the spec is less detailed here".
func undeclaredPayloadKeys(payload map[string]any, node *schemaNode) []string {
	var extra []string
	var walk func(map[string]any, *schemaNode, string)
	walk = func(obj map[string]any, at *schemaNode, prefix string) {
		for key, value := range obj {
			child, declared := at.Properties[key]
			if !declared {
				extra = append(extra, prefix+key)
				continue
			}
			if child == nil || len(child.Properties) == 0 {
				continue // nothing declared below → nothing to check below
			}
			if nested, isObj := value.(map[string]any); isObj {
				walk(nested, child, prefix+key+".")
			}
		}
	}
	walk(payload, node, "")
	sort.Strings(extra)
	return extra
}

// nodeAt resolves a dotted path of declared properties, failing the test when
// the path is not declared. Used to state, as an assertion rather than as a
// comment, that the guard above actually has somewhere to descend.
func nodeAt(t *testing.T, root *schemaNode, path string) *schemaNode {
	t.Helper()
	at := root
	for _, step := range strings.Split(path, ".") {
		child, ok := at.Properties[step]
		if !ok || child == nil {
			t.Fatalf("the frozen spec no longer declares %q — the nested guard has "+
				"nothing to descend into there and would pass any rename", path)
		}
		at = child
	}
	return at
}

// realHeartbeat builds the heartbeat payload from the REAL collectors, driven by
// faked shell/fs seams — the same producers a live 30s cycle calls. Renaming a
// key in any of them changes what this returns, which is the whole point: a
// hand-written fixture would keep matching the spec after the producer drifted.
func realHeartbeat(t *testing.T) map[string]any {
	t.Helper()
	runner := fakeRunner{out: fakeProbes}
	hardware := collectHardware(runner, "darwin")
	if len(hardware) == 0 {
		t.Fatal("precondition: the hardware collector produced nothing to check")
	}

	prober, _, _, _ := newTestProber(newProbeRunner())
	claude := prober.collect()
	if len(claude) == 0 {
		t.Fatal("precondition: the claude prober produced nothing to check")
	}

	// Both runtimes must RESOLVE, or collectRuntimeCapabilities reports
	// installed:false and skips logged_in/version entirely — the guard would
	// then never see the keys it exists to protect.
	bin := filepath.Join(t.TempDir(), "codex")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatalf("stage a resolvable codex: %v", err)
	}
	probes := map[string]string{}
	for key, value := range fakeProbes {
		probes[key] = value
	}
	probes[bin+" --version"] = "codex-cli 0.52.0"
	probes[bin+" login status"] = "Logged in"
	env := func(key string) string {
		switch key {
		case "OC_CLAUDE_BIN", "OC_CODEX_BIN":
			return bin
		}
		return ""
	}
	runtimes := collectRuntimeCapabilities(env, fakeRunner{out: probes}, claude)

	heartbeat, err := buildTelemetryPayload("m-1", "lab-1", hardware,
		map[string]string{"ocwarden": "abc123abc123"}, claude, runtimes)
	if err != nil {
		t.Fatalf("buildTelemetryPayload: %v", err)
	}
	return heartbeat
}

// TestWardenTelemetryPayloadsMatchFrozenSchema covers all three payloads the
// warden POSTs to the telemetry endpoint — the heartbeat, the command receipt and
// the self-update announcement — because a single undeclared key kills whichever
// one carries it. Since T-90be it checks NESTED keys too: a rename inside
// hardware/claude/runtimes lands with a 200 and is then unreadable forever, so
// CI is the only place that can notice.
func TestWardenTelemetryPayloadsMatchFrozenSchema(t *testing.T) {
	declared := frozenTelemetrySchema(t)
	heartbeat := realHeartbeat(t)
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
			t.Errorf("%s payload carries keys the frozen spec does not declare %v.\n"+
				"A TOP-LEVEL undeclared key 422s the whole report. A NESTED one is worse: "+
				"the server answers 200, stores it, and no consumer ever reads it again — "+
				"nothing outside this test can see that. Declare the key in "+
				"spec/openapi.json (and teach the server to read it) or fix the producer.\n"+
				"payload = %#v", name, extra, payload)
		}
	}
	// COVERAGE. A payload that passed the schema by being empty would be
	// worthless, and so would one whose nested blocks are empty: the walker only
	// checks keys that are actually present, so an absent key is invisible to it.
	// These are the keys the server reads by name, listed here so that "the
	// producer stopped emitting it" is as red as "the producer renamed it".
	// (Checked AFTER the drift assertion above, so a rename is reported as drift
	// rather than as a missing key.)
	for _, key := range []string{"machine", "hardware", "binaries", "claude", "runtimes"} {
		if _, present := heartbeat[key]; !present {
			t.Errorf("heartbeat dropped %s; payload = %#v", key, heartbeat)
		}
	}
	blockKeys := map[string][]string{
		"hardware": {"cpu_pct", "ram_pct", "battery_pct", "ac_power"},
		"claude":   {"version", "cred_file", "sub_readable", "keychain"},
	}
	for name, keys := range blockKeys {
		block, _ := heartbeat[name].(map[string]any)
		for _, key := range keys {
			if _, present := block[key]; !present {
				t.Errorf("heartbeat %s carries no %s — the server reads that key by "+
					"name, and a key the fixture never emits is a key this guard "+
					"cannot protect; block = %v", name, key, block)
			}
		}
	}
	runtimes, _ := heartbeat["runtimes"].(map[string]any)
	for _, name := range []string{"claude", "codex"} {
		capability, _ := runtimes[name].(map[string]any)
		// version is omitted when the runtime is not installed, and for claude it
		// is transcribed from the claude probe (covered above); installed and
		// logged_in are what machineSupportsRuntime gates placement on.
		for _, key := range []string{"installed", "logged_in"} {
			if _, present := capability[key]; !present {
				t.Errorf("heartbeat runtimes.%s carries no %s — placement fail-closes "+
					"without it and the machine silently stops accepting that "+
					"runtime; capability = %v", name, key, capability)
			}
		}
	}
	if capability, _ := runtimes["codex"].(map[string]any); capability["version"] == nil {
		t.Errorf("heartbeat runtimes.codex carries no version; capability = %v", capability)
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
	declared := frozenTelemetrySchema(t)
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

// TestWardenTelemetryGuardSeesNestedRenames is the guard's own POSITIVE
// CONTROL. TestWardenTelemetryPayloadsMatchFrozenSchema passing means "today's
// payload matches today's spec" — which is also what a walker that never
// descends would report, and what the old top-level-only walker DID report
// while cpu_pct -> cpu_percent went silently unread in production.
//
// So: take the real heartbeat, rename ONE nested key at each of the three
// depths, and require the guard to name it. It fails if the walker stops at the
// top level, if it stops one level short of runtimes.<rt>.*, or if the spec
// stops declaring the nested shape.
func TestWardenTelemetryGuardSeesNestedRenames(t *testing.T) {
	declared := frozenTelemetrySchema(t)
	// The nested shape must be DECLARED — a walker with nothing to descend into
	// passes every payload, which is the failure mode this whole test exists to
	// rule out.
	for _, path := range []string{"hardware", "claude", "runtimes",
		"runtimes.claude", "runtimes.codex"} {
		if node := nodeAt(t, declared, path); len(node.Properties) == 0 {
			t.Fatalf("%s declares no properties — nothing below it can be checked", path)
		}
	}

	rename := func(payload map[string]any, path []string, from, to string) {
		at := payload
		for _, step := range path {
			next, ok := at[step].(map[string]any)
			if !ok {
				t.Fatalf("precondition: %v is not an object in the real payload", path)
			}
			at = next
		}
		value, present := at[from]
		if !present {
			t.Fatalf("precondition: the real producer no longer emits %v.%s", path, from)
		}
		delete(at, from)
		at[to] = value
	}

	cases := []struct {
		name       string
		path       []string
		from, to   string
		wantReport string
	}{
		{"hardware", nil, "cpu_pct", "cpu_percent", "hardware.cpu_percent"},
		{"claude", nil, "version", "cli_version", "claude.cli_version"},
		{"runtimes", []string{"codex"}, "logged_in", "loggedIn", "runtimes.codex.loggedIn"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			payload := realHeartbeat(t)
			block, ok := payload[tc.name].(map[string]any)
			if !ok {
				t.Fatalf("precondition: heartbeat has no %s block", tc.name)
			}
			if extra := undeclaredPayloadKeys(payload, declared); len(extra) > 0 {
				t.Fatalf("precondition: the untouched payload is already drifting %v", extra)
			}
			rename(block, tc.path, tc.from, tc.to)
			extra := undeclaredPayloadKeys(payload, declared)
			found := false
			for _, key := range extra {
				if key == tc.wantReport {
					found = true
				}
			}
			if !found {
				t.Errorf("renaming %s.%v.%s -> %s went UNREPORTED (guard said %v). A "+
					"nested rename is accepted by the server with a 200, stored, and "+
					"then read as null forever — CI is the only place it can be seen.",
					tc.name, tc.path, tc.from, tc.to, extra)
			}
		})
	}
}

// TestWardenTelemetryValueTypesMatchFrozenSchema is the VALUE-layer twin of
// TestWardenTelemetryPayloadsMatchFrozenSchema: same real producers, same frozen
// spec, but it asks what TYPE each declared value arrives as instead of whether
// the key is known.
//
// The failure it exists to catch is a producer-side type regression — a probe
// parser that starts returning its number as a string, an `installed` flag that
// becomes "true". None of that is refused anywhere: the block is open, the
// server stores it, and the column it feeds goes blank. Nothing else in this
// repo can see it.
//
// It is deliberately a check on OUR OWN payloads and nothing else. The server
// stays as permissive as the owner ruling requires (rc-55861dd893c6); this is a
// regression test for the warden, not a tightening of the wire.
func TestWardenTelemetryValueTypesMatchFrozenSchema(t *testing.T) {
	declared := frozenTelemetrySchema(t)

	// COVERAGE FIRST. A walker whose leaves declare no type passes everything,
	// which is indistinguishable from "the payload is fine". These are the exact
	// leaves the server reads by name, asserted to carry a declared type before
	// anything is judged against them.
	for path, want := range map[string]string{
		"hardware.cpu_pct":          "number",
		"hardware.ram_pct":          "number",
		"hardware.battery_pct":      "number",
		"hardware.ac_power":         "boolean",
		"claude.version":            "string",
		"claude.cred_file":          "boolean",
		"claude.sub_readable":       "boolean",
		"claude.keychain":           "boolean",
		"runtimes.claude.installed": "boolean",
		"runtimes.codex.installed":  "boolean",
		"runtimes.codex.logged_in":  "boolean",
		"runtimes.codex.version":    "string",
	} {
		types := nodeAt(t, declared, path).declaredTypes()
		if !types[want] {
			t.Errorf("the frozen spec no longer declares %s as %s (got %v) — this "+
				"guard would pass any value there", path, want, types)
		}
	}

	heartbeat := onTheWire(t, realHeartbeat(t))
	if bad := mistypedPayloadValues(heartbeat, declared); len(bad) > 0 {
		t.Errorf("the heartbeat sends values the frozen spec does not declare %v.\n"+
			"Nothing refuses this: the nested blocks are open by owner ruling, so "+
			"the server answers 200 and stores it, and the reader — which needs the "+
			"declared type — serves null forever. Fix the producer, or change the "+
			"spec and teach the server to read the new type.\npayload = %#v",
			bad, heartbeat)
	}
}

// TestWardenTelemetryValueGuardSeesRetypedValues is that guard's own POSITIVE
// CONTROL, and it is the reason the guard is worth having: a green
// TestWardenTelemetryValueTypesMatchFrozenSchema is also exactly what a walker
// that never compares anything reports.
//
// So: take the real heartbeat, change ONE nested value's type at each depth, and
// require the guard to name it. It fails if the walker never descends, if the
// spec stops declaring types, or if the comparison is vacuous.
func TestWardenTelemetryValueGuardSeesRetypedValues(t *testing.T) {
	declared := frozenTelemetrySchema(t)
	cases := []struct {
		name       string
		path       []string
		key        string
		bad        any
		wantReport string
	}{
		{"hardware number as string", []string{"hardware"}, "cpu_pct", "47",
			"hardware.cpu_pct: got string, want null|number"},
		{"hardware bool as string", []string{"hardware"}, "ac_power", "yes",
			"hardware.ac_power: got string, want boolean|null"},
		{"claude string as number", []string{"claude"}, "version", 9.9,
			"claude.version: got number, want null|string"},
		{"runtime bool as string", []string{"runtimes", "codex"}, "installed", "true",
			"runtimes.codex.installed: got string, want boolean|null"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			payload := onTheWire(t, realHeartbeat(t))
			if bad := mistypedPayloadValues(payload, declared); len(bad) > 0 {
				t.Fatalf("precondition: the untouched payload already mistypes %v", bad)
			}
			at := payload
			for _, step := range tc.path {
				next, isObj := at[step].(map[string]any)
				if !isObj {
					t.Fatalf("precondition: %v is not an object in the real payload", tc.path)
				}
				at = next
			}
			if _, present := at[tc.key]; !present {
				t.Fatalf("precondition: the real producer no longer emits %v.%s",
					tc.path, tc.key)
			}
			at[tc.key] = tc.bad
			bad := mistypedPayloadValues(payload, declared)
			found := false
			for _, report := range bad {
				if report == tc.wantReport {
					found = true
				}
			}
			if !found {
				t.Errorf("retyping %v.%s went UNREPORTED (guard said %v). A wrongly-"+
					"typed value is accepted with a 200, stored, and then read as "+
					"null forever — CI is the only place it can be seen.",
					tc.path, tc.key, bad)
			}
		})
	}
}

// TestWardenTelemetryValueGuardIgnoresUndeclaredKeys keeps the value guard
// inside the owner ruling. `additionalProperties` stays true so a warden that
// grows a probe still lands its whole report; an undeclared key has no declared
// type, so this guard must have no opinion about its value either. Judging one
// would re-import the intolerance the ruling rejected through the back door.
func TestWardenTelemetryValueGuardIgnoresUndeclaredKeys(t *testing.T) {
	declared := frozenTelemetrySchema(t)
	payload := onTheWire(t, realHeartbeat(t))
	hardware, _ := payload["hardware"].(map[string]any)
	hardware["disk_pct"] = "n/a"
	hardware["thermal"] = map[string]any{"nominal": true}
	if bad := mistypedPayloadValues(payload, declared); len(bad) > 0 {
		t.Errorf("an undeclared nested key was judged on its value %v — the spec "+
			"declares nothing about it, so there is nothing for it to violate", bad)
	}
}

// TestFrozenTelemetryNestedBlocksStayOpen is the COMPATIBILITY sentinel for the
// owner ruling (rc-55861dd893c6): declare the shape, but keep accepting keys
// the spec has not heard of.
//
// The tempting "improvement" is additionalProperties:false, because then the
// server rejects a rename at runtime instead of only in CI. That refusal is not
// per-key: DisallowUnknownFields fails the WHOLE body, so one undeclared nested
// key nulls hardware, binaries, claude and runtimes together on every machine at
// once — measured, and identical to the a7fa594 outage. The two failure modes
// are not symmetric: a rename caught only by CI costs one red build, while a
// closed nested schema costs the fleet's telemetry the moment any warden version
// differs from the spec in either direction.
func TestFrozenTelemetryNestedBlocksStayOpen(t *testing.T) {
	declared := frozenTelemetrySchema(t)
	for _, path := range []string{"hardware", "claude", "runtimes",
		"runtimes.claude", "runtimes.codex"} {
		if nodeAt(t, declared, path).closed() {
			t.Errorf("%s has additionalProperties:false. Declaring the shape is for CI; "+
				"closing it makes the SERVER 422 the entire heartbeat over one nested "+
				"key it has not heard of — every machine's telemetry going null at "+
				"once, which is the outage this declaration was written to avoid.", path)
		}
	}
	// The top level is a different question and stays closed: those keys are the
	// DTO's own fields, the server has always refused unknown ones there, and
	// every producer in this module is checked against that list above.
	if !declared.closed() {
		t.Error("the top-level DTO must stay closed")
	}
}
