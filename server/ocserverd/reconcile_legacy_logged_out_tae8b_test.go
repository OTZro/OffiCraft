package main

// reconcile_legacy_logged_out_tae8b_test.go — the legacy-warden claude shape
// `{"installed":true,"logged_in":false}` must be read as UNKNOWN, never as a
// reason to pin a member to codex (T-ae8b).
//
// WHY THIS EXISTS. cli/ocwarden/runtimeprobe.go is evidence-only for Claude:
// it has no login probe, so "no credential found" OMITS logged_in instead of
// asserting false. An explicit false therefore dates the reporter — only a
// warden older than v0.5.211-beta.1 emits it — and on that warden the false was
// a GUESS that the spawn-side gate routinely disproves (env-carried keys,
// Bedrock / Vertex managed auth, OC_CLAUDE_CRED_CHECK=0).
//
// Spending that guess persists a runtime, and persistence here is irreversible:
// there is no backfill. Before T-ae8b the blast radius was one row (the
// out-of-box assistant, the only member ever born UNSET). T-ae8b makes every
// hire and every new role born UNSET, so without this guard one un-upgraded
// machine would silently pin EVERY future member on it to codex.

import (
	"io"
	"os"
	"strings"
	"testing"
)

// captureReconcileLog collects what reconcileLog emits while fn runs.
// reconcileLog writes straight to os.Stderr (not through the log package), so
// the only seam is the file itself — swapped for a pipe here rather than adding
// an injectable writer to production code just to be observable.
func captureReconcileLog(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	orig := os.Stderr
	os.Stderr = w
	drained := make(chan string, 1)
	go func() {
		out, _ := io.ReadAll(r)
		drained <- string(out)
	}()
	func() {
		defer func() {
			os.Stderr = orig
			_ = w.Close()
		}()
		fn()
	}()
	out := <-drained
	_ = r.Close()
	return out
}

// legacyLoggedOutRuntimes is verbatim what a pre-v0.5.211-beta.1 warden emits
// on a host where the claude binary resolves but neither presence check found a
// credential. Codex on the same host is installed and measurably logged in, so
// a resolver that reads the false as "no" has a codex to fall through to — that
// is exactly the silent pin this fixture must catch.
const legacyLoggedOutRuntimes = `{"runtimes":{"claude":{"installed":true,"logged_in":false},` +
	`"codex":{"installed":true,"logged_in":true}}}`

// legacyLoggedOutClaudeAbsentRuntimes keeps the codex measurement identical but
// makes claude honestly ABSENT. It is the discriminator for the guard's blast
// radius: this host must still resolve to codex.
const legacyLoggedOutClaudeAbsentRuntimes = `{"runtimes":{"claude":{"installed":false},` +
	`"codex":{"installed":true,"logged_in":true}}}`

// codexMeasuredLoggedOutRuntimes is the asymmetry the guard must NOT flatten.
// `codex login status` is a real command, so codex's false is a MEASUREMENT,
// not an absence of evidence, and it stays a "no".
const codexMeasuredLoggedOutRuntimes = `{"runtimes":{"claude":{"installed":true,"logged_in":true},` +
	`"codex":{"installed":true,"logged_in":false}}}`

// TestLegacyLoggedOutFalse_DoesNotPinTheSeedAssistantToCodex is the guard on
// the row that already exists in every install.
func TestLegacyLoggedOutFalse_DoesNotPinTheSeedAssistantToCodex(t *testing.T) {
	s := newReconcileTestServer(t)
	connectOnline(t, s, ServerSelfHost)
	if rec := doIngestTelemetry(s, ServerSelfHost, ServerSelfHost,
		legacyLoggedOutRuntimes); rec.Code != 200 {
		t.Fatalf("telemetry ingest: %d %s", rec.Code, rec.Body.String())
	}
	// Precondition: codex here IS ready, so declining below is a real refusal
	// to guess and not simply the absence of an alternative.
	if caps := s.machineRuntimeCapabilities(ServerSelfHost); !runtimeCapabilityReady(caps[RuntimeCodex]) {
		t.Fatalf("precondition: codex must be ready for this test to discriminate; caps = %#v", caps)
	}
	m := wakeSeedAssistant(t, s)

	decision := s.reconcileOne(m, reconcileState{}, 1000)

	if decision.Command != reconcileCmdStart {
		t.Fatalf("want today's unchanged START, got command %q reason %q",
			decision.Command, decision.Reason)
	}
	if got := storedRuntime(t, s, seedMiraID); got != "" {
		t.Fatalf("roster runtime = %q, want \"\" (still unset): a legacy warden's "+
			"claude logged_in:false means \"no credential evidence found\", NOT "+
			"\"signed out\". Persisting %q off that guess is irreversible and would "+
			"pin a machine that may well run claude (env-carried key, Bedrock/Vertex "+
			"managed auth, OC_CLAUDE_CRED_CHECK=0) to the other runtime with no way back",
			got, got)
	}
	if got := startFrameRuntime(t, s, ServerSelfHost); got != RuntimeClaude {
		t.Fatalf("START frame runtime = %q, want %q: declining to resolve must fall "+
			"through on today's path, not invent a new way to be stuck", got, RuntimeClaude)
	}
}

// TestLegacyLoggedOutFalse_DoesNotPinAHiredMemberToCodex is the amplification
// T-ae8b introduces: every hire is now born UNSET, so the same stale false
// would reach every future member on that machine, not just the seed row.
func TestLegacyLoggedOutFalse_DoesNotPinAHiredMemberToCodex(t *testing.T) {
	s := newReconcileTestServer(t)
	connectOnline(t, s, ServerSelfHost)
	if rec := doIngestTelemetry(s, ServerSelfHost, ServerSelfHost,
		legacyLoggedOutRuntimes); rec.Code != 200 {
		t.Fatalf("telemetry ingest: %d %s", rec.Code, rec.Body.String())
	}
	id := hireMember(t, s, map[string]any{"name": "Yuki"})
	m, err := s.dal.GetMember(id)
	if err != nil || m == nil {
		t.Fatalf("get hired member: %v", err)
	}
	m.DesiredState = DesiredStateOnline
	m.DesiredMachineID = ServerSelfHost
	putTestMember(t, s, *m)

	decision := s.reconcileOne(*m, reconcileState{}, 1000)

	if decision.Command != reconcileCmdStart {
		t.Fatalf("want a dispatched START, got command %q reason %q",
			decision.Command, decision.Reason)
	}
	if got := storedRuntime(t, s, id); got != "" {
		t.Fatalf("hired member roster runtime = %q, want \"\" (still unset): one "+
			"un-upgraded machine must not silently pin every member hired onto it "+
			"to %q, permanently and with no backfill", got, got)
	}
	if got := startFrameRuntime(t, s, ServerSelfHost); got != RuntimeClaude {
		t.Fatalf("START frame runtime = %q, want %q", got, RuntimeClaude)
	}
}

// TestClaudeHonestlyAbsent_StillResolvesToCodex is the guard's blast-radius
// discriminator. A genuinely codex-only box reports installed:false — no
// logged_in at all — and must keep resolving to codex. If this fails, the guard
// swallowed the ticket's whole point.
func TestClaudeHonestlyAbsent_StillResolvesToCodex(t *testing.T) {
	s := newReconcileTestServer(t)
	connectOnline(t, s, ServerSelfHost)
	if rec := doIngestTelemetry(s, ServerSelfHost, ServerSelfHost,
		legacyLoggedOutClaudeAbsentRuntimes); rec.Code != 200 {
		t.Fatalf("telemetry ingest: %d %s", rec.Code, rec.Body.String())
	}
	m := wakeSeedAssistant(t, s)

	s.reconcileOne(m, reconcileState{}, 1000)

	if got := storedRuntime(t, s, seedMiraID); got != RuntimeCodex {
		t.Fatalf("roster runtime = %q, want %q: the unknown-login guard is keyed on "+
			"installed:true + an explicit logged_in:false. An honestly ABSENT claude "+
			"is not that shape and must still resolve to codex", got, RuntimeCodex)
	}
}

// TestCodexMeasuredLoggedOut_StaysANo — the guard is claude-only by design.
// `codex login status` is a real probe, so its false is evidence; extending the
// grace to codex would turn a measurement into a shrug.
func TestCodexMeasuredLoggedOut_StaysANo(t *testing.T) {
	s := newReconcileTestServer(t)
	connectOnline(t, s, ServerSelfHost)
	if rec := doIngestTelemetry(s, ServerSelfHost, ServerSelfHost,
		codexMeasuredLoggedOutRuntimes); rec.Code != 200 {
		t.Fatalf("telemetry ingest: %d %s", rec.Code, rec.Body.String())
	}
	caps := s.machineRuntimeCapabilities(ServerSelfHost)
	if runtimeCapabilityReady(caps[RuntimeCodex]) {
		t.Fatalf("a MEASURED codex logged_in:false must not read as ready; caps = %#v", caps)
	}
	m := wakeSeedAssistant(t, s)

	s.reconcileOne(m, reconcileState{}, 1000)

	if got := storedRuntime(t, s, seedMiraID); got != RuntimeClaude {
		t.Fatalf("roster runtime = %q, want %q: the unknown-login guard must stay keyed on "+
			"the CLAUDE entry. Codex's logged_in:false comes from `codex login status`, a "+
			"real command, so it is evidence and must keep counting as \"not ready\" — a "+
			"guard that shrugs at any runtime's explicit false turns a measurement into a "+
			"shrug and stops resolving hosts it should resolve (%q here means the resolver "+
			"declined instead of picking claude)", got, RuntimeClaude, got)
	}
}

// TestLegacyLoggedOutFalse_SaysWhyItDeclined — a silent refusal is as bad as a
// silent acceptance. Declining to auto-resolve is invisible on the wire (the
// START looks exactly like a no-heartbeat START), so the operator's only thread
// back to the cause is this line. It must carry WHY (the shape dates the
// reporter, and the false is not a measurement) and WHAT NEXT (upgrade that
// warden, or set 執行環境 by hand).
func TestLegacyLoggedOutFalse_SaysWhyItDeclined(t *testing.T) {
	s := newReconcileTestServer(t)
	connectOnline(t, s, ServerSelfHost)
	if rec := doIngestTelemetry(s, ServerSelfHost, ServerSelfHost,
		legacyLoggedOutRuntimes); rec.Code != 200 {
		t.Fatalf("telemetry ingest: %d %s", rec.Code, rec.Body.String())
	}
	m := wakeSeedAssistant(t, s)

	logged := captureReconcileLog(t, func() {
		s.reconcileOne(m, reconcileState{}, 1000)
	})

	for _, want := range []struct{ needle, why string }{
		{"logged_in:false", "names the shape that triggered it, so the reader can match it to the telemetry"},
		{"v0.5.211-beta.1", "dates the reporter — this is the whole reason the false is not trusted"},
		{"NOT \"signed out\"", "says what the false actually means, which is the misreading being prevented"},
		{"upgrade", "next step 1: upgrade that machine's warden"},
		{"執行環境", "next step 2: set this member's runtime by hand"},
	} {
		if !strings.Contains(logged, want.needle) {
			t.Fatalf("the decline said nothing about %q (%s).\nGot:\n%s", want.needle, want.why, logged)
		}
	}
	if !strings.Contains(logged, seedMiraID) || !strings.Contains(logged, ServerSelfHost) {
		t.Fatalf("the decline must name WHICH member and WHICH machine.\nGot:\n%s", logged)
	}
}
