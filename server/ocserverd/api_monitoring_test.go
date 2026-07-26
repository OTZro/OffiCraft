package main

// api_monitoring_test.go — foldCommandResult unit coverage: the durable
// last_op* fold of one warden command_result receipt, focused on the
// last_op_reason field (成員啟動失敗原因全鏈可見: the warden's structured
// "<code>: <detail>" refusal cause must survive the fold verbatim, clamp at
// the reason cap, and stay honest-empty for an old-warden receipt that
// carries no reason).

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
)

// foldTestServer is the minimal apiServer a foldCommandResult call needs:
// a real (temp-SQLite) DAL plus a live hub (putMember publishes a member
// delta on every fold).
func foldTestServer(t *testing.T) *apiServer {
	t.Helper()
	return &apiServer{dal: newTestDAL(t), hub: NewHub()}
}

// doIngestTelemetry drives POST /api/monitoring/telemetry with agent-scope
// claims for sub (machine_id claim included when machineClaim != "").
func doIngestTelemetry(api *apiServer, sub, machineClaim, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest("POST", "/api/monitoring/telemetry", strings.NewReader(body))
	claims := map[string]any{"sub": sub, "scope": "agent"}
	if machineClaim != "" {
		claims["machine_id"] = machineClaim
	}
	req = req.WithContext(context.WithValue(req.Context(), claimsContextKey, claims))
	rec := httptest.NewRecorder()
	api.HandleIngestTelemetryApiMonitoringTelemetryPost(rec, req)
	return rec
}

func TestHandleIngestTelemetry_MachineClaimOverridesSelfReport(t *testing.T) {
	api := &apiServer{telemetry: newMemStore(), hub: NewHub()}
	rec := doIngestTelemetry(api, "m-1", "m-claimed",
		`{"machine": "m-self-reported", "hardware": {"cpu_pct": 1}}`)
	if rec.Code != 200 {
		t.Fatalf("ingest: %d %s", rec.Code, rec.Body.String())
	}
	entry := api.telemetry.Get("m-1")
	if got, _ := entry["machine"].(string); got != "m-claimed" {
		t.Fatalf("machine must come from the token claim, got %q", got)
	}
}

func TestHandleIngestTelemetry_NoClaimFallsBackToSelfReport(t *testing.T) {
	api := &apiServer{telemetry: newMemStore(), hub: NewHub()}
	rec := doIngestTelemetry(api, "m-1", "",
		`{"machine": "m-self-reported", "hardware": {"cpu_pct": 1}}`)
	if rec.Code != 200 {
		t.Fatalf("ingest: %d %s", rec.Code, rec.Body.String())
	}
	entry := api.telemetry.Get("m-1")
	if got, _ := entry["machine"].(string); got != "m-self-reported" {
		t.Fatalf("without a machine_id claim the self-report must fold, got %q", got)
	}
}

func TestHandleIngestTelemetry_ClaimFoldsWithoutSelfReport(t *testing.T) {
	// A claim-bearing token attributes the entry even when the payload carries
	// no machine at all.
	api := &apiServer{telemetry: newMemStore(), hub: NewHub()}
	rec := doIngestTelemetry(api, "m-1", "m-claimed", `{"hardware": {"cpu_pct": 1}}`)
	if rec.Code != 200 {
		t.Fatalf("ingest: %d %s", rec.Code, rec.Body.String())
	}
	entry := api.telemetry.Get("m-1")
	if got, _ := entry["machine"].(string); got != "m-claimed" {
		t.Fatalf("machine must fold from the claim alone, got %q", got)
	}
}

func TestHandleIngestTelemetry_RuntimeCapabilities(t *testing.T) {
	api := &apiServer{telemetry: newMemStore(), hub: NewHub()}
	if !api.machineSupportsRuntime("legacy-warden", RuntimeClaude) ||
		api.machineSupportsRuntime("legacy-warden", RuntimeCodex) {
		t.Fatal("an absent capability map must preserve only legacy Claude placement")
	}
	rec := doIngestTelemetry(api, "m-box", "m-box",
		`{"runtimes":{"claude":{"installed":true,"logged_in":true,"version":"2.1.211"},"codex":{"installed":true,"logged_in":false,"version":"0.145.0"}}}`)
	if rec.Code != 200 {
		t.Fatalf("ingest: %d %s", rec.Code, rec.Body.String())
	}
	if !api.machineSupportsRuntime("m-box", RuntimeClaude) {
		t.Fatal("logged-in Claude capability must be eligible")
	}
	if api.machineSupportsRuntime("m-box", RuntimeCodex) {
		t.Fatal("logged-out Codex capability must be ineligible")
	}
	caps := api.machineRuntimeCapabilities("m-box")
	if got := caps[RuntimeCodex].Version; got == nil || *got != "0.145.0" {
		t.Fatalf("Codex version did not round-trip: %#v", caps)
	}
	codexOnly := doIngestTelemetry(api, "m-box", "m-box",
		`{"runtimes":{"codex":{"installed":true,"logged_in":true}}}`)
	if codexOnly.Code != 200 || api.machineSupportsRuntime("m-box", RuntimeClaude) {
		t.Fatal("once a map is reported, a missing Claude entry must fail closed")
	}

	bad := doIngestTelemetry(api, "m-box", "m-box",
		`{"runtimes":{"codex":{"installed":"yes"}}}`)
	if bad.Code != 400 {
		t.Fatalf("wrong-typed capability must be 400: %d %s", bad.Code, bad.Body.String())
	}
}

func TestHandleIngestTelemetry_BinariesFingerprintsFoldAndEcho(t *testing.T) {
	api := &apiServer{telemetry: newMemStore(), hub: NewHub()}
	// A binaries-only heartbeat is a valid telemetry POST (first-class field),
	// and the fingerprints fold onto the entry + echo back.
	rec := doIngestTelemetry(api, "m-1", "m-1",
		`{"binaries": {"ocwarden": "aaa111", "ocagent": "bbb222"}}`)
	if rec.Code != 200 {
		t.Fatalf("ingest: %d %s", rec.Code, rec.Body.String())
	}
	entry := api.telemetry.Get("m-1")
	bins, _ := entry["binaries"].(map[string]any)
	if bins["ocwarden"] != "aaa111" || bins["ocagent"] != "bbb222" {
		t.Fatalf("binaries fold = %v, want the reported fingerprints", bins)
	}
	if !strings.Contains(rec.Body.String(), `"ocwarden":"aaa111"`) {
		t.Fatalf("echo must carry binaries: %s", rec.Body.String())
	}
	// A later hardware-only heartbeat must not clobber the stored fingerprints.
	if rec := doIngestTelemetry(api, "m-1", "m-1", `{"hardware": {"cpu_pct": 1}}`); rec.Code != 200 {
		t.Fatalf("second ingest: %d %s", rec.Code, rec.Body.String())
	}
	entry = api.telemetry.Get("m-1")
	if bins, _ := entry["binaries"].(map[string]any); bins["ocwarden"] != "aaa111" {
		t.Fatalf("binaries must survive a binaries-less report, got %v", entry["binaries"])
	}
	// A non-object binaries is the flat 400 every other object field gets.
	if rec := doIngestTelemetry(api, "m-2", "m-2", `{"binaries": "not-an-object"}`); rec.Code != 400 {
		t.Fatalf("non-object binaries: %d, want 400", rec.Code)
	}
}

func TestHandleIngestTelemetry_ClaudeProbeFoldAndEcho(t *testing.T) {
	api := &apiServer{telemetry: newMemStore(), hub: NewHub()}
	// A claude-only heartbeat is a valid telemetry POST (first-class field),
	// and the probe folds onto the entry + echoes back (T-97ee).
	rec := doIngestTelemetry(api, "m-1", "m-1",
		`{"claude": {"version": "2.1.211", "cred_file": true, "sub_readable": false, "keychain": true}}`)
	if rec.Code != 200 {
		t.Fatalf("ingest: %d %s", rec.Code, rec.Body.String())
	}
	entry := api.telemetry.Get("m-1")
	probe, _ := entry["claude"].(map[string]any)
	if probe["version"] != "2.1.211" || probe["cred_file"] != true ||
		probe["sub_readable"] != false || probe["keychain"] != true {
		t.Fatalf("claude fold = %v, want the reported probe", probe)
	}
	if !strings.Contains(rec.Body.String(), `"version":"2.1.211"`) {
		t.Fatalf("echo must carry claude: %s", rec.Body.String())
	}
	// A later hardware-only heartbeat must not clobber the stored probe.
	if rec := doIngestTelemetry(api, "m-1", "m-1", `{"hardware": {"cpu_pct": 1}}`); rec.Code != 200 {
		t.Fatalf("second ingest: %d %s", rec.Code, rec.Body.String())
	}
	entry = api.telemetry.Get("m-1")
	if probe, _ := entry["claude"].(map[string]any); probe["version"] != "2.1.211" {
		t.Fatalf("claude must survive a claude-less report, got %v", entry["claude"])
	}
	// A non-object claude is refused. It is a 422 rather than the flat 400 the
	// UNDECLARED blocks (binaries above) still answer, because declaring the
	// nested shape (T-90be) makes codegen type this field as an object, so the
	// refusal now happens in the decoder instead of in the handler's own
	// asObject check. Both are refusals and both are logged by the warden; the
	// asymmetry is documented on AgentTelemetryIngestDTO in the frozen spec.
	// Only the TYPE of the block moved — its CONTENTS stay permissive, which is
	// pinned by TestHandleIngestTelemetry_UndeclaredNestedKeyStillLands below.
	if rec := doIngestTelemetry(api, "m-2", "m-2", `{"claude": "not-an-object"}`); rec.Code != 422 {
		t.Fatalf("non-object claude: %d, want 422", rec.Code)
	}
}

// TestHandleIngestTelemetry_UndeclaredNestedKeyStillLands is the compatibility
// SENTINEL for the nested declaration (T-90be, owner ruling rc-55861dd893c6).
//
// Declaring hardware/claude/runtimes buys CI a rename check; it must NOT buy
// runtime a rejection. A warden that grows a probe (or an older one that never
// had a key) sends nested keys this spec version has never heard of, and its
// WHOLE report — hardware, binaries, claude and runtimes together — must still
// land. Setting additionalProperties:false on any of these blocks turns exactly
// this request into `422 unknown field`, which is the a7fa594 outage verbatim
// (every machine's telemetry silently null at once). If someone "tightens" the
// spec, this goes red before the fleet does.
func TestHandleIngestTelemetry_UndeclaredNestedKeyStillLands(t *testing.T) {
	api := &apiServer{telemetry: newMemStore(), hub: NewHub()}
	body := `{"hardware": {"cpu_pct": 12, "disk_pct": 41},
		"claude": {"version": "9.9.9", "cred_mtime": 1720000000},
		"runtimes": {"claude": {"installed": true, "sandboxed": true}},
		"binaries": {"ocwarden": "abc123abc123"}}`
	rec := doIngestTelemetry(api, "m-1", "m-1", body)
	if rec.Code != 200 {
		t.Fatalf("a heartbeat carrying undeclared NESTED keys must still land: %d %s",
			rec.Code, rec.Body.String())
	}
	entry := api.telemetry.Get("m-1")
	hw, _ := entry["hardware"].(map[string]any)
	if hw["cpu_pct"] != 12.0 {
		t.Errorf("the declared sibling must survive: cpu_pct = %v, want 12", hw["cpu_pct"])
	}
	if hw["disk_pct"] != 41.0 {
		t.Errorf("an undeclared nested key must be stored, not dropped: hardware = %v", hw)
	}
	probe, _ := entry["claude"].(map[string]any)
	if probe["version"] != "9.9.9" || probe["cred_mtime"] != 1720000000.0 {
		t.Errorf("claude = %v, want the whole probe kept", probe)
	}
	rts, _ := entry["runtimes"].(map[string]any)
	claudeCap, _ := rts["claude"].(map[string]any)
	if claudeCap["installed"] != true || claudeCap["sandboxed"] != true {
		t.Errorf("runtimes.claude = %v, want the whole capability kept", claudeCap)
	}
	if bins, _ := entry["binaries"].(map[string]any); bins["ocwarden"] != "abc123abc123" {
		t.Errorf("the rest of the report must land too: binaries = %v", bins)
	}
}

// ── account_label (T-260e): human-readable account default display ──────────

const teleWithLabel = `{"runtime":"claude","hardware": {"cpu_pct": 1},
	"account": "acct-123/team",
	"account_label": "eva.cheng@gofreight.com(GoFreight)"}`

// labelTestServer seeds one active member ("mira", no admin role so an
// agent-scope GET resolves as a plain agent) and ingests one telemetry report
// carrying both the opaque account key and the human-readable account_label.
func labelTestServer(t *testing.T) *apiServer {
	t.Helper()
	s := &apiServer{dal: newTestDAL(t), hub: NewHub(),
		telemetry: newMemStore(), gauge: newMemStore()}
	m := fullMember("mira")
	m.RoleKey = "builder"
	if err := s.dal.PutMember(m); err != nil {
		t.Fatalf("seed member: %v", err)
	}
	rec := doIngestTelemetry(s, "mira", "m-abc123", teleWithLabel)
	if rec.Code != 200 {
		t.Fatalf("ingest: %d %s", rec.Code, rec.Body.String())
	}
	return s
}

// doGetMonitoring drives GET /api/monitoring with the given verified claims.
func doGetMonitoring(api *apiServer, claims map[string]any) *httptest.ResponseRecorder {
	req := httptest.NewRequest("GET", "/api/monitoring", nil)
	req = req.WithContext(context.WithValue(req.Context(), claimsContextKey, claims))
	rec := httptest.NewRecorder()
	api.HandleGetMonitoringApiMonitoringGet(rec, req)
	return rec
}

func monitoringOf(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	if rec.Code != 200 {
		t.Fatalf("GET /api/monitoring: %d %s", rec.Code, rec.Body.String())
	}
	var d map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &d); err != nil {
		t.Fatalf("body not JSON: %s", rec.Body.String())
	}
	return d
}

func TestHandleIngestTelemetry_AccountLabelFolds(t *testing.T) {
	api := &apiServer{telemetry: newMemStore(), hub: NewHub()}
	rec := doIngestTelemetry(api, "m-1", "", teleWithLabel)
	if rec.Code != 200 {
		t.Fatalf("ingest: %d %s", rec.Code, rec.Body.String())
	}
	entry := api.telemetry.Get("m-1")
	if got, _ := entry["account_label"].(string); got != "eva.cheng@gofreight.com(GoFreight)" {
		t.Fatalf("account_label must fold into the entry, got %q", got)
	}
	// PRIVACY: the ingest echo (agent-readable) must NOT mint an account_label
	// wire field — the label only ever surfaces on the owner-facing fold.
	if strings.Contains(rec.Body.String(), "account_label") {
		t.Fatalf("ingest echo must not carry account_label: %s", rec.Body.String())
	}
}

func TestGetMonitoring_OwnerSeesLabelAsDefaultDisplayName(t *testing.T) {
	s := labelTestServer(t)
	d := monitoringOf(t, doGetMonitoring(s, map[string]any{"sub": "owner", "scope": "owner"}))
	accounts := d["accounts"].([]any)
	if len(accounts) != 1 {
		t.Fatalf("accounts = %v, want 1 row", accounts)
	}
	row := accounts[0].(map[string]any)
	if row["account"] != "acct-123/team" {
		t.Fatalf("account key must stay the stable tag, got %v", row["account"])
	}
	if row["display_name"] != "eva.cheng@gofreight.com(GoFreight)" {
		t.Fatalf("owner default display must be the reported label, got %v", row["display_name"])
	}
	// The session row's account column resolves the same way for the owner.
	sessions := d["sessions"].([]any)
	if len(sessions) != 1 {
		t.Fatalf("sessions = %v, want 1 row", sessions)
	}
	if got := sessions[0].(map[string]any)["account"]; got != "eva.cheng@gofreight.com(GoFreight)" {
		t.Fatalf("owner session account = %v, want the label", got)
	}
}

func TestGetMonitoring_SameAccountKeyFoldsIntoOneRow(t *testing.T) {
	// REGRESSION (T-f694): the accounts fold is a pure string aggregation — two
	// members reporting the SAME account key (e.g. the same uid/org account on a
	// file-creds machine and a Keychain-only machine, now that the plan no
	// longer joins the key) must fold into ONE row with the costs summed.
	s := labelTestServer(t)
	m := fullMember("joey")
	m.RoleKey = "builder"
	if err := s.dal.PutMember(m); err != nil {
		t.Fatalf("seed member: %v", err)
	}
	rec := doIngestTelemetry(s, "joey", "m-other",
		`{"runtime":"claude","hardware": {"cpu_pct": 2}, "cost": 1.5, "account": "acct-123/team"}`)
	if rec.Code != 200 {
		t.Fatalf("ingest: %d %s", rec.Code, rec.Body.String())
	}
	d := monitoringOf(t, doGetMonitoring(s, map[string]any{"sub": "owner", "scope": "owner"}))
	accounts := d["accounts"].([]any)
	if len(accounts) != 1 {
		t.Fatalf("accounts = %v, want the two members' identical keys folded into 1 row", accounts)
	}
	if got := accounts[0].(map[string]any)["account"]; got != "acct-123/team" {
		t.Fatalf("account = %v, want acct-123/team", got)
	}
}

func TestGetMonitoring_OwnerAliasWinsOverLabel(t *testing.T) {
	// 不覆蓋: a display name the owner set by hand ALWAYS beats the reported label.
	s := labelTestServer(t)
	if err := s.dal.PutAccountAlias(AccountAlias{
		Account: "acct-123/team", DisplayName: "Eva 的 Team 帳號"}); err != nil {
		t.Fatalf("seed alias: %v", err)
	}
	d := monitoringOf(t, doGetMonitoring(s, map[string]any{"sub": "owner", "scope": "owner"}))
	row := d["accounts"].([]any)[0].(map[string]any)
	if row["display_name"] != "Eva 的 Team 帳號" {
		t.Fatalf("owner alias must win over the reported label, got %v", row["display_name"])
	}
}

func TestGetMonitoring_AgentNeverSeesLabel(t *testing.T) {
	// PRIVACY: the email-bearing label is owner-facing ONLY. An agent-principal
	// GET /api/monitoring (same route, lower rank) sees the raw stable key and
	// the response body must not contain the label/email anywhere.
	s := labelTestServer(t)
	rec := doGetMonitoring(s, map[string]any{"sub": "mira", "scope": "agent"})
	d := monitoringOf(t, rec)
	row := d["accounts"].([]any)[0].(map[string]any)
	if row["display_name"] != "acct-123/team" {
		t.Fatalf("agent-facing display must fall back to the raw key, got %v", row["display_name"])
	}
	if strings.Contains(rec.Body.String(), "eva.cheng@gofreight.com") ||
		strings.Contains(rec.Body.String(), "GoFreight") {
		t.Fatalf("agent-facing monitoring leaked the label: %s", rec.Body.String())
	}
}

func TestGetMonitoring_AgentStillSeesOwnerAlias(t *testing.T) {
	// The owner-set alias is a deliberate, non-PII display overlay — it stays
	// visible at every rank (pre-existing behaviour, unchanged by T-260e).
	s := labelTestServer(t)
	if err := s.dal.PutAccountAlias(AccountAlias{
		Account: "acct-123/team", DisplayName: "Team 帳號"}); err != nil {
		t.Fatalf("seed alias: %v", err)
	}
	d := monitoringOf(t, doGetMonitoring(s, map[string]any{"sub": "mira", "scope": "agent"}))
	row := d["accounts"].([]any)[0].(map[string]any)
	if row["display_name"] != "Team 帳號" {
		t.Fatalf("agent-facing display must still resolve the owner alias, got %v", row["display_name"])
	}
}

// ── account_label passthrough field (T-a9a7): raw label survives aliasing ───

func TestGetMonitoring_OwnerAccountRowCarriesLabelEvenWithAlias(t *testing.T) {
	// The account row must expose the reporter-supplied label VERBATIM in the
	// dedicated account_label field, and — the whole point of the field — the
	// label must STILL be there after the owner sets an alias (display_name
	// switches to the alias; account_label keeps the real identity).
	s := labelTestServer(t)
	d := monitoringOf(t, doGetMonitoring(s, map[string]any{"sub": "owner", "scope": "owner"}))
	row := d["accounts"].([]any)[0].(map[string]any)
	if row["account_label"] != "eva.cheng@gofreight.com(GoFreight)" {
		t.Fatalf("owner account_label = %v, want the raw reported label", row["account_label"])
	}
	if err := s.dal.PutAccountAlias(AccountAlias{
		Account: "acct-123/team", DisplayName: "Eva 的 Team 帳號"}); err != nil {
		t.Fatalf("seed alias: %v", err)
	}
	d = monitoringOf(t, doGetMonitoring(s, map[string]any{"sub": "owner", "scope": "owner"}))
	row = d["accounts"].([]any)[0].(map[string]any)
	if row["display_name"] != "Eva 的 Team 帳號" {
		t.Fatalf("alias must stay the display, got %v", row["display_name"])
	}
	if row["account_label"] != "eva.cheng@gofreight.com(GoFreight)" {
		t.Fatalf("account_label must survive the alias, got %v", row["account_label"])
	}
}

func TestGetMonitoring_AgentNeverSeesAccountLabelField(t *testing.T) {
	// PRIVACY GATE: the account_label field is owner-facing ONLY. For an
	// agent-principal GET the KEY ITSELF must be absent from the wire body
	// (omitempty on an empty overlay), not just empty.
	s := labelTestServer(t)
	rec := doGetMonitoring(s, map[string]any{"sub": "mira", "scope": "agent"})
	d := monitoringOf(t, rec)
	row := d["accounts"].([]any)[0].(map[string]any)
	if _, present := row["account_label"]; present {
		t.Fatalf("agent-facing account row must not carry account_label: %v", row)
	}
	if strings.Contains(rec.Body.String(), "account_label") {
		t.Fatalf("agent-facing monitoring body must not mention account_label: %s", rec.Body.String())
	}
}

func TestGetMonitoring_SessionAccountNeverServesRawKey(t *testing.T) {
	// T-ba6b: the session row's account column feeds the member detail panel's
	// Claude Account cell — with NO readable name (no alias, no label) it must
	// serve "" (the panel's dash), NEVER the raw credential key. The accounts
	// row keeps its raw-key display_name fallback (it is the aliasing surface).
	s := &apiServer{dal: newTestDAL(t), hub: NewHub(),
		telemetry: newMemStore(), gauge: newMemStore()}
	m := fullMember("mira")
	m.RoleKey = "builder"
	if err := s.dal.PutMember(m); err != nil {
		t.Fatalf("seed member: %v", err)
	}
	rec := doIngestTelemetry(s, "mira", "m-abc123",
		`{"runtime":"claude","hardware": {"cpu_pct": 1}, "account": "acct-123/team"}`)
	if rec.Code != 200 {
		t.Fatalf("ingest: %d %s", rec.Code, rec.Body.String())
	}
	d := monitoringOf(t, doGetMonitoring(s, map[string]any{"sub": "owner", "scope": "owner"}))
	if got := d["sessions"].([]any)[0].(map[string]any)["account"]; got != "" {
		t.Fatalf("unresolvable session account = %v, want \"\"", got)
	}
	row := d["accounts"].([]any)[0].(map[string]any)
	if row["display_name"] != "acct-123/team" {
		t.Fatalf("accounts-row display_name keeps the raw-key fallback, got %v", row["display_name"])
	}
}

func TestGetMonitoring_WorkerReportedLabelResolvesSessionAccount(t *testing.T) {
	// T-ba6b (recon §6-4/§6-6): the label overlay scans the WHOLE telemetry
	// snapshot, so an account_label reported by an OUTSOURCE-WORKER session
	// resolves a member session on the same account (the old fold scanned only
	// roster members and left the raw key).
	s := labelTestServer(t)
	// Strip the member's own label; keep only the account key.
	s.telemetry.Set("mira", map[string]any{"account": "acct-123/team", accountRuntimeKey: RuntimeClaude})
	// A worker entry (NOT a roster member) reports the label for the same key.
	s.telemetry.Set("ow-1", map[string]any{
		"account": "acct-123/team", accountRuntimeKey: RuntimeClaude,
		"account_label": "eva@corp(Corp)", "ts": 99.0})
	d := monitoringOf(t, doGetMonitoring(s, map[string]any{"sub": "owner", "scope": "owner"}))
	if got := d["sessions"].([]any)[0].(map[string]any)["account"]; got != "eva@corp(Corp)" {
		t.Fatalf("worker-reported label must resolve the session account, got %v", got)
	}
}

func TestGetMonitoring_NoLabelReportedOmitsAccountLabel(t *testing.T) {
	// Honest-absent: telemetry that carries only the opaque account key (no
	// account_label) yields an owner-facing row WITHOUT the key — never "".
	s := &apiServer{dal: newTestDAL(t), hub: NewHub(),
		telemetry: newMemStore(), gauge: newMemStore()}
	m := fullMember("mira")
	m.RoleKey = "builder"
	if err := s.dal.PutMember(m); err != nil {
		t.Fatalf("seed member: %v", err)
	}
	rec := doIngestTelemetry(s, "mira", "m-abc123",
		`{"runtime":"claude","hardware": {"cpu_pct": 1}, "account": "acct-123/team"}`)
	if rec.Code != 200 {
		t.Fatalf("ingest: %d %s", rec.Code, rec.Body.String())
	}
	d := monitoringOf(t, doGetMonitoring(s, map[string]any{"sub": "owner", "scope": "owner"}))
	row := d["accounts"].([]any)[0].(map[string]any)
	if _, present := row["account_label"]; present {
		t.Fatalf("label-less report must omit account_label, got %v", row)
	}
}

// runtimeAccountServer reproduces the owner-reported shape: a member's current
// runtime is Claude, but its durable telemetry entry carries an older Codex
// account. The owner alias makes an accidental attribution immediately visible.
func runtimeAccountServer(t *testing.T, memberRuntime, report string) *apiServer {
	t.Helper()
	s := &apiServer{dal: newTestDAL(t), hub: NewHub(), telemetry: newMemStore(), gauge: newMemStore()}
	m := fullMember("kyle")
	m.Runtime = memberRuntime
	if err := s.dal.PutMember(m); err != nil {
		t.Fatalf("seed member: %v", err)
	}
	// BOTH keys are aliased, so every assertion below can tell "withheld" apart
	// from "merely unresolvable" — an empty account cell means the server
	// refused to attribute the key, never that it had no readable name for it.
	for _, alias := range []AccountAlias{
		{Account: "codex:8906abc", DisplayName: "EvaChatGPT"},
		{Account: "claude:uid/org", DisplayName: "EvaClaude"},
	} {
		if err := s.dal.PutAccountAlias(alias); err != nil {
			t.Fatalf("seed alias: %v", err)
		}
	}
	if rec := doIngestTelemetry(s, "kyle", "m-eva-m5", report); rec.Code != 200 {
		t.Fatalf("ingest: %d %s", rec.Code, rec.Body.String())
	}
	return s
}

const codexReportedAccount = `{"runtime":"codex","account":"codex:8906abc","account_label":"ReporterOnly"}`
const claudeReportedAccount = `{"runtime":"claude","account":"claude:uid/org","account_label":"ReporterOnly"}`

// sessionAccount reads the one member row's account cell from the owner-facing
// monitoring fold (the surface the member panel renders).
func sessionAccount(t *testing.T, s *apiServer) any {
	t.Helper()
	d := monitoringOf(t, doGetMonitoring(s, map[string]any{"sub": "owner", "scope": "owner"}))
	return d["sessions"].([]any)[0].(map[string]any)["account"]
}

func TestGetMonitoring_RuntimeAccountNeverBorrowsAnotherRuntime(t *testing.T) {
	s := runtimeAccountServer(t, RuntimeClaude, codexReportedAccount)
	ownerRec := doGetMonitoring(s, map[string]any{"sub": "owner", "scope": "owner"})
	d := monitoringOf(t, ownerRec)
	if got := d["sessions"].([]any)[0].(map[string]any)["account"]; got != "" {
		t.Fatalf("claude session account = %v, want honest empty", got)
	}
	if got := d["machines"].([]any)[0].(map[string]any)["accounts"].([]any); len(got) != 0 {
		t.Fatalf("foreign-runtime account leaked into machine fold: %v", got)
	}
	if got := d["accounts"].([]any); len(got) != 1 || got[0].(map[string]any)["display_name"] != "EvaChatGPT" {
		t.Fatalf("global account overview lost owner alias visibility: %v", got)
	}
	if got := d["accounts"].([]any)[0].(map[string]any)["machine"]; got != "" {
		t.Fatalf("global foreign account must not inherit Claude machine: %v", got)
	}
}

func TestGetMonitoring_RuntimeAccountKeepsCodexAndOwnerGate(t *testing.T) {
	s := runtimeAccountServer(t, RuntimeCodex, codexReportedAccount)
	owner := monitoringOf(t, doGetMonitoring(s, map[string]any{"sub": "owner", "scope": "owner"}))
	if got := owner["sessions"].([]any)[0].(map[string]any)["account"]; got != "EvaChatGPT" {
		t.Fatalf("codex session account = %v, want EvaChatGPT", got)
	}
	if got := owner["accounts"].([]any); len(got) != 1 {
		t.Fatalf("codex account must remain observable, got %v", got)
	}
	// Existing privacy gate: an agent never receives reporter-supplied labels.
	agentRec := doGetMonitoring(s, map[string]any{"sub": "kyle", "scope": "agent"})
	if strings.Contains(agentRec.Body.String(), `"account_label":"ReporterOnly"`) {
		t.Fatalf("agent response leaked reporter-only account label: %s", agentRec.Body.String())
	}
}

// TestHandleIngestTelemetry_AccountPairingIsAtomic pins the ingest half of the
// guarantee: `account`, its provenance stamp and the reporter label are ONE
// unit, and any report that cannot prove the pairing retires it instead of
// leaving a stale one standing for a later report to inherit.
func TestHandleIngestTelemetry_AccountPairingIsAtomic(t *testing.T) {
	api := &apiServer{telemetry: newMemStore(), hub: NewHub()}
	entry := func() map[string]any { return api.telemetry.Get("kyle") }
	ingest := func(body string) {
		t.Helper()
		if rec := doIngestTelemetry(api, "kyle", "", body); rec.Code != 200 {
			t.Fatalf("ingest %s: %d %s", body, rec.Code, rec.Body.String())
		}
	}

	// ① a proven report stamps key + provenance + label together.
	ingest(codexReportedAccount)
	if got, _ := entry()[accountRuntimeKey].(string); got != RuntimeCodex {
		t.Fatalf("account runtime = %q, want codex", got)
	}
	// ② a same-runtime report with no account leaves the pairing alone.
	ingest(`{"runtime":"codex","cost":1}`)
	if got, _ := entry()["account"].(string); got != "codex:8906abc" {
		t.Fatalf("same-runtime heartbeat lost the paired account: %q", got)
	}
	// ③ an account WITHOUT a runtime is unprovable: it is not stored, and it
	// must not leave the previous pairing behind either — that leftover is what
	// a later runtime-only heartbeat used to inherit.
	ingest(`{"cost":1,"account":"unprovable"}`)
	for _, key := range []string{"account", accountRuntimeKey, "account_label"} {
		if v, present := entry()[key]; present {
			t.Fatalf("runtime-less account report left %s = %v standing", key, v)
		}
	}
	// ④ a runtime-only heartbeat on a DIFFERENT runtime retires the pairing:
	// the key belonged to the runtime the actor just left.
	ingest(codexReportedAccount)
	ingest(`{"runtime":"claude"}`)
	if v, present := entry()["account"]; present {
		t.Fatalf("runtime switch kept the prior runtime's account %v", v)
	}
}

// TestGetMonitoring_RuntimelessAccountCannotDegradeIntoOlderRuntime is the
// end-to-end sequence blocker 2 named: a proven pairing, then a report whose
// runtime went missing. "Missing runtime" must never degrade into "some older
// runtime" — the panel shows nothing rather than the key the actor used before.
func TestGetMonitoring_RuntimelessAccountCannotDegradeIntoOlderRuntime(t *testing.T) {
	s := runtimeAccountServer(t, RuntimeClaude, claudeReportedAccount)
	if got := sessionAccount(t, s); got != "EvaClaude" {
		t.Fatalf("proven Claude pairing must be served, got %v", got)
	}
	// The actor has moved to Codex; this report carries the new key but lost
	// its runtime field (older / partial reporter). Unprovable in, nothing out.
	if rec := doIngestTelemetry(s, "kyle", "m-eva-m5",
		`{"cost":1,"account":"codex:8906abc"}`); rec.Code != 200 {
		t.Fatalf("runtime-less account report: %d %s", rec.Code, rec.Body.String())
	}
	if got := sessionAccount(t, s); got != "" {
		t.Fatalf("runtime-less report degraded into the older runtime's account: %v", got)
	}
	if v, present := s.telemetry.Get("kyle")["account"]; present {
		t.Fatalf("unprovable report left an inheritable account %v behind", v)
	}
}

// TestGetMonitoring_RuntimeSwitchHeartbeatRetiresPriorAccount covers the other
// half of the same sequence: the actor announces its new runtime before the
// member row catches up. The old key must not keep being served under the
// lagging row.
func TestGetMonitoring_RuntimeSwitchHeartbeatRetiresPriorAccount(t *testing.T) {
	s := runtimeAccountServer(t, RuntimeClaude, claudeReportedAccount)
	if rec := doIngestTelemetry(s, "kyle", "m-eva-m5", `{"runtime":"codex"}`); rec.Code != 200 {
		t.Fatalf("codex heartbeat: %d %s", rec.Code, rec.Body.String())
	}
	if got := sessionAccount(t, s); got != "" {
		t.Fatalf("account survived the runtime the actor left: %v", got)
	}
}

// TestGetMonitoring_OwnerAliasVisibleToEveryCallerRank pins the contract
// blocker 1 questioned (resolveAccountDisplay ①, account_display.go): the
// owner's hand-set alias is readable by EVERY caller rank; only the
// reporter-supplied account_label is owner-gated PII (T-260e). Runtime-aware
// attribution decides WHICH key an actor owns — it must never narrow WHO may
// read the alias of a key that is displayed.
func TestGetMonitoring_OwnerAliasVisibleToEveryCallerRank(t *testing.T) {
	s := runtimeAccountServer(t, RuntimeCodex, codexReportedAccount)
	agentRec := doGetMonitoring(s, map[string]any{"sub": "kyle", "scope": "agent"})
	d := monitoringOf(t, agentRec)
	if got := d["sessions"].([]any)[0].(map[string]any)["account"]; got != "EvaChatGPT" {
		t.Fatalf("agent-facing session account = %v, want owner alias EvaChatGPT", got)
	}
	if got := d["accounts"].([]any)[0].(map[string]any)["display_name"]; got != "EvaChatGPT" {
		t.Fatalf("agent-facing accounts display_name = %v, want owner alias EvaChatGPT", got)
	}
	// The owner-only half of the same contract holds in the very same body.
	if strings.Contains(agentRec.Body.String(), "ReporterOnly") {
		t.Fatalf("agent response leaked the reporter-only label: %s", agentRec.Body.String())
	}
}

func TestGetMonitoring_RuntimeHeartbeatCannotReattributePairedAccount(t *testing.T) {
	s := runtimeAccountServer(t, RuntimeClaude, codexReportedAccount)
	// Counterfactual: the same actor later sends only a Claude runtime heartbeat.
	// Its account remains stamped Codex, so the heartbeat must not borrow it.
	if rec := doIngestTelemetry(s, "kyle", "m-eva-m5", `{"runtime":"claude"}`); rec.Code != 200 {
		t.Fatalf("claude heartbeat: %d %s", rec.Code, rec.Body.String())
	}
	d := monitoringOf(t, doGetMonitoring(s, map[string]any{"sub": "owner", "scope": "owner"}))
	if got := d["sessions"].([]any)[0].(map[string]any)["account"]; got != "" {
		t.Fatalf("runtime-only Claude heartbeat reattributed Codex account: %v", got)
	}
}

// TestFoldCommandResult_WorkerReceiptFoldsOntoWorkerRow (T-9ccf): a receipt
// keyed on worker_id (a worker has NO roster member) must fold the last-op
// fields onto the durable outsource_worker row — the worker twin of the member
// fold, and the server half of the O-19 visibility fix.
func TestFoldCommandResult_WorkerReceiptFoldsOntoWorkerRow(t *testing.T) {
	s := foldTestServer(t)
	w := OutsourceWorker{ID: "ow-1", Codename: "O-7", Model: "opus", Effort: "high",
		TaskID: "t-1", Status: WorkerStatusAssigned, CreatedTS: 100}
	if err := s.dal.PutOutsourceWorker(w); err != nil {
		t.Fatalf("seed worker: %v", err)
	}
	reason := `session_already_exists: tmux session "worker-ow-1" is already live (clobber-guard refused to stomp it)`
	s.foldCommandResult(map[string]any{
		"worker_id": "ow-1",
		"rpc":       "worker_start",
		"ok":        false,
		"reason":    reason,
		"log":       reason,
		"at":        "2026-07-13T08:00:00Z",
	}, "w-test")

	got, err := s.dal.GetOutsourceWorker("ow-1")
	if err != nil || got == nil {
		t.Fatalf("get worker: %v %v", got, err)
	}
	if got.LastOp != "worker_start" || got.LastOpOK == nil || *got.LastOpOK {
		t.Fatalf("fold must record a failed worker_start, got %+v", got)
	}
	if got.LastOpReason != reason {
		t.Fatalf("worker last_op_reason must persist verbatim:\n got %q\nwant %q", got.LastOpReason, reason)
	}
	if got.LastOpAt == 0 {
		t.Fatalf("worker last_op_at must be stamped, got 0")
	}
	// The fold must NOT disturb lifecycle columns.
	if got.Status != WorkerStatusAssigned || got.Codename != "O-7" {
		t.Fatalf("fold must leave lifecycle untouched, got %+v", got)
	}
}

// TestFoldCommandResult_WorkerReceiptUnknownWorkerIgnored: a worker receipt for
// an unknown worker id is a safe no-op (never a panic / 500), mirroring the
// unknown-member branch.
func TestFoldCommandResult_WorkerReceiptUnknownWorkerIgnored(t *testing.T) {
	s := foldTestServer(t)
	s.foldCommandResult(map[string]any{
		"worker_id": "ow-nope", "rpc": "worker_start", "ok": true,
	}, "w-test")
}

func TestFoldCommandResult_ReasonPersistedVerbatim(t *testing.T) {
	s := foldTestServer(t)
	m := fullMember("mira")
	m.LastOp, m.LastOpOK, m.LastOpLog, m.LastOpReason, m.LastOpAt = "", nil, "", "", 0
	if err := s.dal.PutMember(m); err != nil {
		t.Fatalf("seed member: %v", err)
	}

	reason := `session_already_exists: tmux session "member-mira" is already live (clobber-guard refused to stomp it)`
	s.foldCommandResult(map[string]any{
		"member_id": "mira",
		"rpc":       "start",
		"ok":        false,
		"reason":    reason,
		"log":       reason,
		"at":        "2026-07-13T08:00:00Z",
	}, "w-test")

	got, err := s.dal.GetMember("mira")
	if err != nil || got == nil {
		t.Fatalf("get: %v %v", got, err)
	}
	if got.LastOp != "start" || got.LastOpOK == nil || *got.LastOpOK {
		t.Fatalf("fold must record a failed start, got %+v", got)
	}
	if got.LastOpReason != reason {
		t.Fatalf("last_op_reason must persist verbatim:\n got %q\nwant %q", got.LastOpReason, reason)
	}
}

func TestFoldCommandResult_ReasonClampedAtCap(t *testing.T) {
	s := foldTestServer(t)
	if err := s.dal.PutMember(fullMember("mira")); err != nil {
		t.Fatalf("seed member: %v", err)
	}
	long := "mkdir_failed: " + strings.Repeat("x", 2*commandResultReasonMax)
	s.foldCommandResult(map[string]any{
		"member_id": "mira", "rpc": "start", "ok": false, "reason": long,
	}, "w-test")
	got, _ := s.dal.GetMember("mira")
	if len(got.LastOpReason) != commandResultReasonMax {
		t.Fatalf("reason must clamp to %d bytes, got %d", commandResultReasonMax, len(got.LastOpReason))
	}
	if !strings.HasPrefix(got.LastOpReason, "mkdir_failed: ") {
		t.Fatalf("clamp must keep the head (the structured code), got %q", got.LastOpReason[:32])
	}
}

func TestFoldCommandResult_NoReasonFoldsEmpty(t *testing.T) {
	// Old-warden compat: a receipt WITHOUT a reason key must fold "" — and
	// OVERWRITE any stale prior reason (the reason always describes THIS op).
	s := foldTestServer(t)
	m := fullMember("mira")
	m.LastOpReason = "spawn_exec_failed: stale prior cause"
	if err := s.dal.PutMember(m); err != nil {
		t.Fatalf("seed member: %v", err)
	}
	s.foldCommandResult(map[string]any{
		"member_id": "mira", "rpc": "stop", "ok": true, "log": "session=member-mira: stopped",
	}, "w-test")
	got, _ := s.dal.GetMember("mira")
	if got.LastOpReason != "" {
		t.Fatalf("a reason-less receipt must fold an empty reason, got %q", got.LastOpReason)
	}
	if got.LastOp != "stop" || got.LastOpLog == "" {
		t.Fatalf("the rest of the fold must be untouched, got %+v", got)
	}
}

// ── T-9adc: no-op stop receipts must not pollute last_op ─────────────────────

// TestFoldCommandResult_NoopStopDoesNotPolluteLastOp: an idempotent no-op stop
// receipt (ok=true, reason no_such_session — the warden had no session and no
// member process; identity sweeps and mis-routed stops produce exactly these)
// must NOT overwrite the member's last_op* — get_member keeps showing what
// actually happened, not a forged "successfully stopped" (the 2026-07-20
// incident's misleading observation surface).
func TestFoldCommandResult_NoopStopDoesNotPolluteLastOp(t *testing.T) {
	s := foldTestServer(t)
	m := fullMember("mira")
	ok := true
	m.LastOp, m.LastOpOK, m.LastOpLog, m.LastOpReason, m.LastOpAt =
		"start", &ok, "spawned", "", 1_000.0
	if err := s.dal.PutMember(m); err != nil {
		t.Fatalf("seed member: %v", err)
	}
	s.foldCommandResult(map[string]any{
		"member_id": "mira",
		"rpc":       "stop",
		"ok":        true,
		"reason":    "no_such_session: stop was a no-op (no session, no member process on this warden)",
		"log":       "session=member-mira: no_such_session",
		"at":        "2026-07-20T04:30:00Z",
	}, "w-test")
	got, err := s.dal.GetMember("mira")
	if err != nil || got == nil {
		t.Fatalf("get: %v %v", got, err)
	}
	if got.LastOp != "start" {
		t.Fatalf("no-op stop must NOT overwrite last_op, got %q", got.LastOp)
	}
	if got.LastOpOK == nil || !*got.LastOpOK || got.LastOpLog != "spawned" ||
		got.LastOpAt != 1_000.0 {
		t.Fatalf("no-op stop must leave the whole last_op* block untouched, got %+v", got)
	}
}

// TestFoldCommandResult_FailedStopAlwaysFolds (guard): only the ok=true no-op
// is skipped — a FAILED stop folds even if its reason ever carried the no-op
// code (failure must stay visible; the honest-partial contract is untouched).
func TestFoldCommandResult_FailedStopAlwaysFolds(t *testing.T) {
	s := foldTestServer(t)
	m := fullMember("mira")
	m.LastOp = "start"
	if err := s.dal.PutMember(m); err != nil {
		t.Fatalf("seed member: %v", err)
	}
	s.foldCommandResult(map[string]any{
		"member_id": "mira", "rpc": "stop", "ok": false,
		"reason": "no_such_session: contradictory failed no-op (defensive)",
	}, "w-test")
	got, _ := s.dal.GetMember("mira")
	if got.LastOp != "stop" || got.LastOpOK == nil || *got.LastOpOK {
		t.Fatalf("a failed stop must fold regardless of reason, got %+v", got)
	}
}

// TestFoldCommandResult_RealStopStillFolds (guard): a genuine kill's receipt
// (ok=true, reason "stopped") keeps folding exactly as before.
func TestFoldCommandResult_RealStopStillFolds(t *testing.T) {
	s := foldTestServer(t)
	m := fullMember("mira")
	m.LastOp = "start"
	if err := s.dal.PutMember(m); err != nil {
		t.Fatalf("seed member: %v", err)
	}
	s.foldCommandResult(map[string]any{
		"member_id": "mira", "rpc": "stop", "ok": true, "reason": "stopped",
	}, "w-test")
	got, _ := s.dal.GetMember("mira")
	if got.LastOp != "stop" || got.LastOpOK == nil || !*got.LastOpOK {
		t.Fatalf("a real stop receipt must keep folding, got %+v", got)
	}
}

// TestFoldWorkerCommandResult_NoopStopSkipped: the worker-row twin — an
// identity-sweep no-op stop for an outsource worker must not overwrite the
// worker's last_op* either.
func TestFoldWorkerCommandResult_NoopStopSkipped(t *testing.T) {
	s := foldTestServer(t)
	if err := s.dal.PutOutsourceWorker(OutsourceWorker{
		ID: "ow-7", Codename: "O-7", Model: "opus", Effort: "high",
		TaskID: "t-1", Status: WorkerStatusAssigned, CreatedTS: 1.0,
		LastOp: "start", LastOpAt: 500.0,
	}); err != nil {
		t.Fatalf("seed worker: %v", err)
	}
	s.foldCommandResult(map[string]any{
		"worker_id": "ow-7", "rpc": "stop", "ok": true,
		"reason": "no_such_session: stop was a no-op (no session, no member process on this warden)",
	}, "w-test")
	got, err := s.dal.GetOutsourceWorker("ow-7")
	if err != nil || got == nil {
		t.Fatalf("get worker: %v %v", got, err)
	}
	if got.LastOp != "start" || got.LastOpAt != 500.0 {
		t.Fatalf("no-op stop must not pollute the worker last_op, got %+v", got)
	}
}

// ---------------------------------------------------------------------------
// Hardware freshness (T-b36a)
//
// Telemetry is only cleared when a member is DISMISSED, never when it goes
// away, and nothing on the wire has ever said how old a hardware sample is. So
// a machine that reported once and then went dark kept serving that sample
// forever — a confident "47%" sitting next to an offline badge. These pin that
// a sample past telemetryFreshSecs reads as NO DATA (the same honest nulls a
// machine that never reported hardware serves), and that a fresh one is
// untouched.
// ---------------------------------------------------------------------------

// freshnessServer seeds one member reporting from host m-abc123, then returns
// the server plus a hook that rewrites how long ago its hardware was sampled.
func freshnessServer(t *testing.T, hw string) (*apiServer, func(ageSecs float64)) {
	t.Helper()
	s := &apiServer{dal: newTestDAL(t), hub: NewHub(),
		telemetry: newMemStore(), gauge: newMemStore()}
	m := fullMember("mira")
	m.RoleKey = "builder"
	if err := s.dal.PutMember(m); err != nil {
		t.Fatalf("seed member: %v", err)
	}
	if rec := doIngestTelemetry(s, "mira", "m-abc123",
		`{"runtime":"claude","hardware":`+hw+`}`); rec.Code != 200 {
		t.Fatalf("ingest: %d %s", rec.Code, rec.Body.String())
	}
	return s, func(ageSecs float64) {
		entry := s.telemetry.Get("mira")
		entry["hardware_ts"] = nowSecs() - ageSecs
		s.telemetry.Set("mira", entry)
	}
}

func machineRow(t *testing.T, s *apiServer, machine string) map[string]any {
	t.Helper()
	d := monitoringOf(t, doGetMonitoring(s, map[string]any{"sub": "owner", "scope": "owner"}))
	for _, raw := range d["machines"].([]any) {
		row := raw.(map[string]any)
		if row["machine"] == machine {
			return row
		}
	}
	t.Fatalf("no machine row for %q in %v", machine, d["machines"])
	return nil
}

// TestGetMonitoring_StaleHardwareReadsAsNoData: the counterfactual. A machine
// that reported 47% CPU and then went dark must stop presenting that number.
func TestGetMonitoring_StaleHardwareReadsAsNoData(t *testing.T) {
	s, age := freshnessServer(t,
		`{"cpu_pct": 47, "ram_pct": 61, "battery_pct": 88, "ac_power": true}`)
	age(telemetryFreshSecs + 1) // reported, then went away

	row := machineRow(t, s, "m-abc123")
	for _, key := range []string{"cpu_pct", "ram_pct", "battery_pct", "ac_power"} {
		if got, present := row[key]; !present || got != nil {
			t.Errorf("%s = %v, want null — a sample older than %vs is not a live "+
				"measurement and must read as no data, not as the last value",
				key, got, telemetryFreshSecs)
		}
	}
	// The machine itself must NOT vanish: "this host exists but nobody has
	// measured it lately" is exactly the honest state we want on screen.
	if row["agents"] == nil {
		t.Errorf("the machine row itself must survive a stale sample; got %v", row)
	}
}

// TestGetMonitoring_FreshHardwareStillServed is the SENTINEL: the TTL must not
// be so eager that it kills healthy data. One heartbeat cadence of age (30s) is
// the NORMAL steady state — every machine on screen is usually this old.
func TestGetMonitoring_FreshHardwareStillServed(t *testing.T) {
	s, age := freshnessServer(t, `{"cpu_pct": 47, "ram_pct": 61, "ac_power": false}`)
	// Straight off the real ingest path, with NOTHING rewritten: a sample that
	// just arrived must be served. (This is what goes red if the ingest handler
	// stops stamping hardware_ts — an unstamped sample is fail-closed stale, so
	// dropping the stamp would black out every healthy machine.)
	if got := machineRow(t, s, "m-abc123")["cpu_pct"]; got != 47.0 {
		t.Fatalf("freshly ingested cpu_pct = %v, want 47", got)
	}
	for _, seconds := range []float64{0, 30, telemetryFreshSecs - 1} {
		age(seconds)
		row := machineRow(t, s, "m-abc123")
		if row["cpu_pct"] != 47.0 {
			t.Errorf("age %vs: cpu_pct = %v, want 47 — a healthy machine that has "+
				"missed at most two heartbeats must never flicker to no-data",
				seconds, row["cpu_pct"])
		}
		if row["ac_power"] != false {
			t.Errorf("age %vs: ac_power = %v, want false (a real false, not dropped)",
				seconds, row["ac_power"])
		}
	}
}

// TestGetMonitoring_UnstampedHardwareIsNotFresh: fail-closed. An entry carrying
// hardware with no hardware_ts has an UNKNOWN sample age, and unknown age must
// never be presented as a live reading.
func TestGetMonitoring_UnstampedHardwareIsNotFresh(t *testing.T) {
	s, _ := freshnessServer(t, `{"cpu_pct": 47}`)
	entry := s.telemetry.Get("mira")
	delete(entry, "hardware_ts")
	s.telemetry.Set("mira", entry)

	if got := machineRow(t, s, "m-abc123")["cpu_pct"]; got != nil {
		t.Errorf("cpu_pct = %v, want null — hardware of unknown age is not fresh", got)
	}
}

// TestGetMonitoring_LaterReportWithoutHardwareCannotRefreshIt: the freshness
// verdict is about the HARDWARE SAMPLE, not about the entry. A command_result
// receipt or an identity-only heartbeat advances entry["ts"] while carrying no
// hardware at all — reading the entry ts would let it resurrect an arbitrarily
// old CPU number, which is the same lie in a new costume.
func TestGetMonitoring_LaterReportWithoutHardwareCannotRefreshIt(t *testing.T) {
	s, age := freshnessServer(t, `{"cpu_pct": 47}`)
	age(telemetryFreshSecs + 1)
	// A hardware-less report lands NOW: entry["ts"] jumps to the present.
	if rec := doIngestTelemetry(s, "mira", "m-abc123",
		`{"runtime":"claude","cost": 1.5}`); rec.Code != 200 {
		t.Fatalf("ingest: %d %s", rec.Code, rec.Body.String())
	}
	entry := s.telemetry.Get("mira")
	if ts, _ := entry["ts"].(float64); nowSecs()-ts > 5 {
		t.Fatalf("precondition: the entry ts must have been refreshed, got %v", ts)
	}
	if got := machineRow(t, s, "m-abc123")["cpu_pct"]; got != nil {
		t.Errorf("cpu_pct = %v, want null — a report carrying no hardware must not "+
			"make old hardware look freshly measured", got)
	}
}

// TestHandleIngestTelemetry_StampsHardwareSampleTime: the ingest half of the
// freshness contract. hardware and hardware_ts move together, and a report that
// carries no hardware must leave the previous sample's stamp ALONE (it did not
// measure anything, so it has nothing to vouch for).
func TestHandleIngestTelemetry_StampsHardwareSampleTime(t *testing.T) {
	api := &apiServer{telemetry: newMemStore(), hub: NewHub()}
	if rec := doIngestTelemetry(api, "m-1", "m-1",
		`{"hardware": {"cpu_pct": 1}}`); rec.Code != 200 {
		t.Fatalf("ingest: %d %s", rec.Code, rec.Body.String())
	}
	stamp, ok := api.telemetry.Get("m-1")["hardware_ts"].(float64)
	if !ok || nowSecs()-stamp > 5 {
		t.Fatalf("hardware_ts = %v (ok=%v), want the sample time", stamp, ok)
	}
	// Rewind, then send a report with NO hardware block.
	entry := api.telemetry.Get("m-1")
	entry["hardware_ts"] = 1000.0
	api.telemetry.Set("m-1", entry)
	if rec := doIngestTelemetry(api, "m-1", "m-1", `{"cost": 2.5}`); rec.Code != 200 {
		t.Fatalf("ingest: %d %s", rec.Code, rec.Body.String())
	}
	if got, _ := api.telemetry.Get("m-1")["hardware_ts"].(float64); got != 1000.0 {
		t.Errorf("hardware_ts = %v, want 1000 untouched — a hardware-less report "+
			"must not vouch for a sample it did not take", got)
	}
}

// ---------------------------------------------------------------------------
// Per-machine sample stamps on the wire (T-b36a step 2b)
//
// Nulling an expired sample fixed the confident-wrong number, but it left two
// different worlds looking identical on screen: "this box has never reported
// hardware" and "this box reported, then went away an hour ago". The second is
// the one an operator has to act on. The stamp is what tells them apart, and it
// is the reason the fold keeps the timestamp of a sample whose VALUES it refuses
// to serve.
// ---------------------------------------------------------------------------

// TestGetMonitoring_StaleHardwareKeepsItsStamp: expired values, surviving stamp.
func TestGetMonitoring_StaleHardwareKeepsItsStamp(t *testing.T) {
	s, age := freshnessServer(t, `{"cpu_pct": 47, "ram_pct": 61}`)
	age(telemetryFreshSecs + 600)

	row := machineRow(t, s, "m-abc123")
	if got := row["cpu_pct"]; got != nil {
		t.Errorf("cpu_pct = %v, want null (the sample expired)", got)
	}
	ts, ok := row["hardware_ts"].(float64)
	if !ok {
		t.Fatalf("hardware_ts = %v, want the sample time — without it an expired "+
			"machine is indistinguishable from one that never reported hardware, "+
			"which is the whole reason the numbers could be trusted too long",
			row["hardware_ts"])
	}
	if age := nowSecs() - ts; age < telemetryFreshSecs {
		t.Errorf("hardware_ts is %.0fs old, want the ORIGINAL sample time (~%.0fs) — "+
			"a stamp that advances on read would say the expired numbers are fresh",
			age, telemetryFreshSecs+600)
	}
}

// TestGetMonitoring_NeverReportedHardwareHasNoStamp is the other half: a machine
// with no sample must not get a fabricated one.
func TestGetMonitoring_NeverReportedHardwareHasNoStamp(t *testing.T) {
	s := &apiServer{dal: newTestDAL(t), hub: NewHub(),
		telemetry: newMemStore(), gauge: newMemStore()}
	m := fullMember("mira")
	m.RoleKey = "builder"
	if err := s.dal.PutMember(m); err != nil {
		t.Fatalf("seed member: %v", err)
	}
	if rec := doIngestTelemetry(s, "mira", "m-abc123",
		`{"runtime":"claude","cost":1.5}`); rec.Code != 200 {
		t.Fatalf("ingest: %d %s", rec.Code, rec.Body.String())
	}
	row := machineRow(t, s, "m-abc123")
	if got, present := row["hardware_ts"]; !present || got != nil {
		t.Errorf("hardware_ts = %v, want null — no sample means no sample time", got)
	}
	if got, present := row["runtime_capabilities_stale"]; !present || got != nil {
		t.Errorf("runtime_capabilities_stale = %v, want null — a machine that never "+
			"probed is not 'fresh' and not 'stale', it is unknown", got)
	}
}

// TestGetMonitoring_FreshHardwareCarriesAStampAndItsZeroes is the SENTINEL for
// the honest-zero problem: 0 and false are real measurements, and the easiest
// way to break this fold is to treat them as "no data".
func TestGetMonitoring_FreshHardwareCarriesAStampAndItsZeroes(t *testing.T) {
	s, _ := freshnessServer(t,
		`{"cpu_pct": 0, "ram_pct": 0, "battery_pct": 0, "ac_power": false}`)
	row := machineRow(t, s, "m-abc123")
	for _, key := range []string{"cpu_pct", "ram_pct", "battery_pct"} {
		if row[key] != 0.0 {
			t.Errorf("%s = %v, want 0 — a measured zero is data, not absence", key, row[key])
		}
	}
	if row["ac_power"] != false {
		t.Errorf("ac_power = %v, want false (a real false, not dropped)", row["ac_power"])
	}
	ts, ok := row["hardware_ts"].(float64)
	if !ok || nowSecs()-ts > 5 {
		t.Errorf("hardware_ts = %v, want the just-taken sample time", row["hardware_ts"])
	}
}

// runtimeCapabilityServer seeds one member that reported a capability probe from
// host m-abc123, plus a hook that rewrites how long ago it was probed.
func runtimeCapabilityServer(t *testing.T) (*apiServer, func(ageSecs float64)) {
	t.Helper()
	s := &apiServer{dal: newTestDAL(t), hub: NewHub(),
		telemetry: newMemStore(), gauge: newMemStore()}
	// The capability probe rides the WARDEN's own heartbeat, and a warden member
	// IS the machine (same id) — that keying is what machineRuntimeCapabilities
	// reads, so the fixture has to be a warden, not an agent sitting on the host.
	warden := fullMember("m-abc123")
	warden.Kind = "warden"
	warden.RoleKey = ""
	warden.DesiredMachineID = "m-abc123"
	if err := s.dal.PutMember(warden); err != nil {
		t.Fatalf("seed warden: %v", err)
	}
	if rec := doIngestTelemetry(s, "m-abc123", "m-abc123",
		`{"runtimes":{"claude":{"installed":true,"logged_in":true,"version":"2.1.211"},
			"codex":{"installed":true,"logged_in":false,"version":"0.52.0"}}}`); rec.Code != 200 {
		t.Fatalf("ingest: %d %s", rec.Code, rec.Body.String())
	}
	if len(s.machineRuntimeCapabilities("m-abc123")) != 2 {
		t.Fatalf("precondition: the probe did not land on the machine (%v)",
			s.machineRuntimeCapabilities("m-abc123"))
	}
	return s, func(ageSecs float64) {
		entry := s.telemetry.Get("m-abc123")
		entry["runtimes_ts"] = nowSecs() - ageSecs
		s.telemetry.Set("m-abc123", entry)
	}
}

func capabilityOf(t *testing.T, row map[string]any, runtime string) map[string]any {
	t.Helper()
	caps, _ := row["runtime_capabilities"].(map[string]any)
	capability, _ := caps[runtime].(map[string]any)
	if capability == nil {
		t.Fatalf("no runtime_capabilities.%s in %v", runtime, row)
	}
	return capability
}

// TestGetMonitoring_StaleRuntimeCapabilitiesAreMarkedNotBlanked: a machine that
// probed and then went dark must not present its old readiness as current — but
// the values stay, because they are the only explanation an operator has for a
// worker stuck on machine_unavailable. Marked, not deleted.
func TestGetMonitoring_StaleRuntimeCapabilitiesAreMarkedNotBlanked(t *testing.T) {
	s, age := runtimeCapabilityServer(t)
	age(telemetryFreshSecs + 1)

	row := machineRow(t, s, "m-abc123")
	if row["runtime_capabilities_stale"] != true {
		t.Errorf("runtime_capabilities_stale = %v, want true — past the window this "+
			"map is a memory, and rendering it plain is a second field that lies "+
			"the way the hardware numbers used to", row["runtime_capabilities_stale"])
	}
	if _, ok := row["runtime_capabilities_ts"].(float64); !ok {
		t.Errorf("runtime_capabilities_ts = %v, want the probe time",
			row["runtime_capabilities_ts"])
	}
	if got := capabilityOf(t, row, "codex")["logged_in"]; got != false {
		t.Errorf("codex logged_in = %v, want the reported false to SURVIVE — it is "+
			"the only surface that explains why codex work will not place here", got)
	}
}

// TestGetMonitoring_FreshRuntimeCapabilitiesAreNotStale is the sentinel: a
// machine heartbeating normally must never be marked stale, and its honest
// false must arrive as false.
func TestGetMonitoring_FreshRuntimeCapabilitiesAreNotStale(t *testing.T) {
	s, age := runtimeCapabilityServer(t)
	// Straight off the real ingest path, with NOTHING rewritten: a probe that
	// just arrived must read as current. This is what goes red if the ingest
	// handler stops stamping runtimes_ts — an unstamped map is fail-closed
	// stale, so dropping the stamp would mark every healthy machine out of date.
	if got := machineRow(t, s, "m-abc123")["runtime_capabilities_stale"]; got != false {
		t.Fatalf("freshly ingested runtime_capabilities_stale = %v, want false", got)
	}
	for _, seconds := range []float64{0, 30, telemetryFreshSecs - 1} {
		age(seconds)
		row := machineRow(t, s, "m-abc123")
		if row["runtime_capabilities_stale"] != false {
			t.Errorf("age %vs: runtime_capabilities_stale = %v, want false — a machine "+
				"that has missed at most two heartbeats is not out of date",
				seconds, row["runtime_capabilities_stale"])
		}
		if got := capabilityOf(t, row, "codex")["logged_in"]; got != false {
			t.Errorf("age %vs: codex logged_in = %v, want false", seconds, got)
		}
		if got := capabilityOf(t, row, "claude")["installed"]; got != true {
			t.Errorf("age %vs: claude installed = %v, want true", seconds, got)
		}
	}
}

// TestGetMonitoring_UnstampedRuntimeCapabilitiesAreNotFresh: fail-closed, the
// same reading hardware gets. A map whose age is unknown is not current.
func TestGetMonitoring_UnstampedRuntimeCapabilitiesAreNotFresh(t *testing.T) {
	s, _ := runtimeCapabilityServer(t)
	entry := s.telemetry.Get("m-abc123")
	delete(entry, "runtimes_ts")
	s.telemetry.Set("m-abc123", entry)

	row := machineRow(t, s, "m-abc123")
	if row["runtime_capabilities_stale"] != true {
		t.Errorf("runtime_capabilities_stale = %v, want true — unknown age is not freshness",
			row["runtime_capabilities_stale"])
	}
}

// TestGetMonitoring_ReportWithoutRuntimesCannotRefreshThem: the verdict is about
// the PROBE, not about the entry. A hardware-only heartbeat advances the entry
// ts while carrying no capability probe at all.
func TestGetMonitoring_ReportWithoutRuntimesCannotRefreshThem(t *testing.T) {
	s, age := runtimeCapabilityServer(t)
	age(telemetryFreshSecs + 1)
	if rec := doIngestTelemetry(s, "m-abc123", "m-abc123",
		`{"hardware":{"cpu_pct":47}}`); rec.Code != 200 {
		t.Fatalf("ingest: %d %s", rec.Code, rec.Body.String())
	}
	row := machineRow(t, s, "m-abc123")
	if row["cpu_pct"] != 47.0 {
		t.Fatalf("precondition: the new hardware sample must be served, got %v", row["cpu_pct"])
	}
	if row["runtime_capabilities_stale"] != true {
		t.Errorf("runtime_capabilities_stale = %v, want true — a report carrying no "+
			"capability probe must not make an old one look freshly measured",
			row["runtime_capabilities_stale"])
	}
}
