package main

// reconcile_runtime_resolution_tb3d0_test.go — placement-time resolution of an
// UNSET member runtime (T-b3d0).
//
// The guarded shape is the one a REAL warden sends. cli/ocwarden/runtimeprobe.go
// ALWAYS emits the claude key; a codex-only box therefore reports
//
//	{"claude": {"installed": false},
//	 "codex":  {"installed": true, "logged_in": true}}
//
// — NOT a map with the claude key missing. The pre-existing coverage
// (TestHandleIngestTelemetry_RuntimeCapabilities) only ever fed the
// missing-key map, which no machine actually produces, so every read that keys
// on "is there a claude entry" looked correct while being wrong on every real
// codex-only host. These tests feed the real shape.

import (
	"testing"
)

// codexOnlyRuntimes is verbatim what cli/ocwarden/runtimeprobe.go emits on a
// host where claude is absent and codex is installed + logged in.
const codexOnlyRuntimes = `{"runtimes":{"claude":{"installed":false},` +
	`"codex":{"installed":true,"logged_in":true,"version":"0.145.0"}}}`

// bothRuntimes is a host that carries a ready claude AND a ready codex.
const bothRuntimes = `{"runtimes":{"claude":{"installed":true,"logged_in":true},` +
	`"codex":{"installed":true,"logged_in":true}}}`

// neitherRuntimes is a host that reported, and has nothing launchable.
const neitherRuntimes = `{"runtimes":{"claude":{"installed":false},` +
	`"codex":{"installed":false}}}`

// wakeSeedAssistant puts the out-of-box seed assistant into desired-online on
// the server-self machine and returns the row as stored.
func wakeSeedAssistant(t *testing.T, s *apiServer) Member {
	t.Helper()
	m, err := s.dal.GetMember(seedMiraID)
	if err != nil || m == nil {
		t.Fatalf("seed assistant missing: %v", err)
	}
	m.DesiredState = DesiredStateOnline
	m.DesiredMachineID = ServerSelfHost
	putTestMember(t, s, *m)
	return *m
}

// storedRuntime reads the runtime column back off the roster row.
func storedRuntime(t *testing.T, s *apiServer, id string) string {
	t.Helper()
	m, err := s.dal.GetMember(id)
	if err != nil || m == nil {
		t.Fatalf("get member %s: %v", id, err)
	}
	return m.Runtime
}

// startFrameRuntime drains the warden FIFO and returns the runtime carried by
// the single START frame (fails the test on any other frame count/kind).
func startFrameRuntime(t *testing.T, s *apiServer, wardenID string) string {
	t.Helper()
	frames := drainFrames(t, s, wardenID)
	if len(frames) != 1 || frames[0].RPC != reconcileCmdStart {
		t.Fatalf("want exactly one START frame, got %#v", frames)
	}
	runtime, _ := frames[0].Args["runtime"].(string)
	return runtime
}

// TestSeedAssistantIsNotBornClaude_CodexOnlyHost is the ticket's case: the
// out-of-box assistant, never given a runtime by anyone, placed on a machine
// that reports the REAL codex-only shape, must be resolved to codex — on the
// wire AND on the roster row — instead of being dispatched as a claude member
// that dies at spawn with claude_bin_unresolved.
func TestSeedAssistantIsNotBornClaude_CodexOnlyHost(t *testing.T) {
	s := newReconcileTestServer(t)
	connectOnline(t, s, ServerSelfHost)
	if rec := doIngestTelemetry(s, ServerSelfHost, ServerSelfHost,
		codexOnlyRuntimes); rec.Code != 200 {
		t.Fatalf("telemetry ingest: %d %s", rec.Code, rec.Body.String())
	}
	m := wakeSeedAssistant(t, s)
	if got := storedRuntime(t, s, seedMiraID); got != "" {
		t.Fatalf("precondition: the seed assistant must carry NO runtime, got %q", got)
	}

	decision := s.reconcileOne(m, reconcileState{}, 1000)

	if decision.Command != reconcileCmdStart {
		t.Fatalf("want a dispatched START, got command %q reason %q",
			decision.Command, decision.Reason)
	}
	if got := storedRuntime(t, s, seedMiraID); got != RuntimeCodex {
		t.Fatalf("roster runtime = %q, want %q", got, RuntimeCodex)
	}
	if got := startFrameRuntime(t, s, ServerSelfHost); got != RuntimeCodex {
		t.Fatalf("START frame runtime = %q, want %q", got, RuntimeCodex)
	}
}

// TestSeedAssistantResolvesToClaude_WhenClaudeIsReady — the same unset member
// on a box that has a ready claude keeps claude. Resolution follows the
// machine; it is not a blanket switch to codex.
func TestSeedAssistantResolvesToClaude_WhenClaudeIsReady(t *testing.T) {
	s := newReconcileTestServer(t)
	connectOnline(t, s, ServerSelfHost)
	if rec := doIngestTelemetry(s, ServerSelfHost, ServerSelfHost,
		bothRuntimes); rec.Code != 200 {
		t.Fatalf("telemetry ingest: %d %s", rec.Code, rec.Body.String())
	}
	m := wakeSeedAssistant(t, s)

	decision := s.reconcileOne(m, reconcileState{}, 1000)

	if decision.Command != reconcileCmdStart {
		t.Fatalf("want a dispatched START, got command %q reason %q",
			decision.Command, decision.Reason)
	}
	if got := storedRuntime(t, s, seedMiraID); got != RuntimeClaude {
		t.Fatalf("roster runtime = %q, want %q", got, RuntimeClaude)
	}
	if got := startFrameRuntime(t, s, ServerSelfHost); got != RuntimeClaude {
		t.Fatalf("START frame runtime = %q, want %q", got, RuntimeClaude)
	}
}

// TestUnsetRuntime_NoHeartbeatKeepsTodaysBehaviour — a machine that has never
// reported has no capabilities to read. The adjudicated choice is to LET IT
// THROUGH on the existing claude path rather than invent a new way to be
// stuck: nothing is persisted, and the wire carries claude exactly as it does
// today.
func TestUnsetRuntime_NoHeartbeatKeepsTodaysBehaviour(t *testing.T) {
	s := newReconcileTestServer(t)
	connectOnline(t, s, ServerSelfHost)
	m := wakeSeedAssistant(t, s)

	decision := s.reconcileOne(m, reconcileState{}, 1000)

	if decision.Command != reconcileCmdStart {
		t.Fatalf("want a dispatched START, got command %q reason %q",
			decision.Command, decision.Reason)
	}
	if got := storedRuntime(t, s, seedMiraID); got != "" {
		t.Fatalf("nothing may be persisted without capabilities, got %q", got)
	}
	if got := startFrameRuntime(t, s, ServerSelfHost); got != RuntimeClaude {
		t.Fatalf("START frame runtime = %q, want %q", got, RuntimeClaude)
	}
}

// TestExplicitClaudeChoiceIsNeverOverridden — resolution fills an ABSENT
// choice; it must not rewrite a runtime the owner actually picked, even on a
// codex-only box (that host's refusal is the placement gate's job, and the
// owner's stated intent must survive to be seen).
func TestExplicitClaudeChoiceIsNeverOverridden(t *testing.T) {
	s := newReconcileTestServer(t)
	connectOnline(t, s, ServerSelfHost)
	if rec := doIngestTelemetry(s, ServerSelfHost, ServerSelfHost,
		codexOnlyRuntimes); rec.Code != 200 {
		t.Fatalf("telemetry ingest: %d %s", rec.Code, rec.Body.String())
	}
	m := wakeSeedAssistant(t, s)
	m.Runtime = RuntimeClaude
	putTestMember(t, s, m)

	s.reconcileOne(m, reconcileState{}, 1000)

	if got := storedRuntime(t, s, seedMiraID); got != RuntimeClaude {
		t.Fatalf("an explicit choice was rewritten to %q", got)
	}
}

// TestUnsetRuntime_NothingReadyPersistsNothing — a host that reported and has
// NEITHER runtime launchable gives resolution nothing to choose from. No guess
// is frozen onto the roster row; the member falls through on today's path
// (NormalizeRuntime("") == claude, which machineSupportsRuntime's deliberately
// permissive Claude arm still admits), and the spawn-time refusal is what the
// owner sees — now naming the Codex option too.
func TestUnsetRuntime_NothingReadyPersistsNothing(t *testing.T) {
	s := newReconcileTestServer(t)
	connectOnline(t, s, ServerSelfHost)
	if rec := doIngestTelemetry(s, ServerSelfHost, ServerSelfHost,
		neitherRuntimes); rec.Code != 200 {
		t.Fatalf("telemetry ingest: %d %s", rec.Code, rec.Body.String())
	}
	m := wakeSeedAssistant(t, s)

	decision := s.reconcileOne(m, reconcileState{}, 1000)

	if decision.Command != reconcileCmdStart {
		t.Fatalf("want today's unchanged START, got command %q reason %q",
			decision.Command, decision.Reason)
	}
	if got := storedRuntime(t, s, seedMiraID); got != "" {
		t.Fatalf("a guess was persisted: %q", got)
	}
	if got := startFrameRuntime(t, s, ServerSelfHost); got != RuntimeClaude {
		t.Fatalf("START frame runtime = %q, want %q", got, RuntimeClaude)
	}
}

// claudeLoginUnknownRuntimes is the shape a warden emits (after T-b3d0's
// runtimeprobe fix) on a host where claude RESOLVES but neither presence check
// found a credential: logged_in is ABSENT — unknown, not false. Codex on the
// same host is installed and measurably logged in, so if unknown were read as
// "signed out" this member would be resolved to codex and frozen there.
const claudeLoginUnknownRuntimes = `{"runtimes":{"claude":{"installed":true},` +
	`"codex":{"installed":true,"logged_in":true}}}`

// TestSeedAssistantPrefersClaude_WhenClaudeLoginIsUnknown is the owner ruling
// pinned to the placement outcome (「Installed 不知道不應該標記」).
//
// Claude is installed; whether it is logged in was never measured — the warden
// has no claude login probe, only two presence checks. An unknown must not be
// spent as a "no": resolution is IRREVERSIBLE (it writes the runtime column and
// no backfill exists), so a guess here is permanent. Claude wins, and if the
// host really is signed out the owner sees it at spawn — where the refusal
// names the Codex option — instead of silently getting a different runtime.
func TestSeedAssistantPrefersClaude_WhenClaudeLoginIsUnknown(t *testing.T) {
	s := newReconcileTestServer(t)
	connectOnline(t, s, ServerSelfHost)
	if rec := doIngestTelemetry(s, ServerSelfHost, ServerSelfHost,
		claudeLoginUnknownRuntimes); rec.Code != 200 {
		t.Fatalf("telemetry ingest: %d %s", rec.Code, rec.Body.String())
	}
	// Precondition: codex on this host IS ready, so choosing claude below is a
	// real preference and not the only option left.
	if caps := s.machineRuntimeCapabilities(ServerSelfHost); !runtimeCapabilityReady(caps[RuntimeCodex]) {
		t.Fatalf("precondition: codex must be ready for this test to discriminate; caps = %#v", caps)
	}
	m := wakeSeedAssistant(t, s)

	decision := s.reconcileOne(m, reconcileState{}, 1000)

	if decision.Command != reconcileCmdStart {
		t.Fatalf("want a dispatched START, got command %q reason %q",
			decision.Command, decision.Reason)
	}
	if got := storedRuntime(t, s, seedMiraID); got != RuntimeClaude {
		t.Fatalf("roster runtime = %q, want %q. An UNKNOWN claude login was read "+
			"as a no and this member was permanently written to a different "+
			"runtime — the exact irreversible guess the owner ruled out.",
			got, RuntimeClaude)
	}
	if got := startFrameRuntime(t, s, ServerSelfHost); got != RuntimeClaude {
		t.Fatalf("START frame runtime = %q, want %q", got, RuntimeClaude)
	}
}

// TestSeedAssistantStillPicksCodex_WhenClaudeIsNotInstalled is the negative
// control for the test above, and the regression guard for this ticket's whole
// reason to exist. Absence of EVIDENCE about login must be permissive; absence
// of the BINARY is a measurement and must stay decisive. If the fix above ever
// widened into "always prefer claude", the codex-only box — the machine T-b3d0
// exists to serve — would go back to being born claude and dying at spawn.
func TestSeedAssistantStillPicksCodex_WhenClaudeIsNotInstalled(t *testing.T) {
	s := newReconcileTestServer(t)
	connectOnline(t, s, ServerSelfHost)
	if rec := doIngestTelemetry(s, ServerSelfHost, ServerSelfHost,
		codexOnlyRuntimes); rec.Code != 200 {
		t.Fatalf("telemetry ingest: %d %s", rec.Code, rec.Body.String())
	}
	// Precondition: claude is REPORTED (installed:false), not missing from the
	// map — the shape a real codex-only warden sends.
	caps := s.machineRuntimeCapabilities(ServerSelfHost)
	claude, reported := caps[RuntimeClaude]
	if !reported || claude.Installed == nil || *claude.Installed {
		t.Fatalf("precondition: want a reported claude entry with installed:false, got %#v", caps)
	}
	m := wakeSeedAssistant(t, s)

	decision := s.reconcileOne(m, reconcileState{}, 1000)

	if decision.Command != reconcileCmdStart {
		t.Fatalf("want a dispatched START, got command %q reason %q",
			decision.Command, decision.Reason)
	}
	if got := storedRuntime(t, s, seedMiraID); got != RuntimeCodex {
		t.Fatalf("roster runtime = %q, want %q. claude is MEASURED absent on this "+
			"box; treating that like an unknown re-breaks the codex-only machine "+
			"this ticket exists for.", got, RuntimeCodex)
	}
	if got := startFrameRuntime(t, s, ServerSelfHost); got != RuntimeCodex {
		t.Fatalf("START frame runtime = %q, want %q", got, RuntimeCodex)
	}
}
