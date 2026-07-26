package main

import (
	"fmt"
	"io"
	"os"
	"strings"
	"testing"
)

func fixedTeardownPaths() teardownPaths {
	return teardownPaths{
		tokfile:   "/h/.officraft/warden/exec-warden.tok",
		plistPath: "/h/Library/LaunchAgents/com.officraft.ocwarden.plist",
		guiDomain: "gui/501",
	}
}

// The confirmed-teardown launchctl surface reuses install_test.go's labelGoneRunFn:
// `bootout` succeeds and the follow-up `launchctl print` poll exits non-zero
// (= launchd reports the label gone — bootoutUntilGone's first probe confirms).

// assertBootoutThenConfirmPoll pins the launchctl sequence a confirmed teardown
// must issue: an EXACT-label bootout first, then at least one EXACT-label
// `launchctl print` confirm probe — and nothing that is not launchctl.
func assertBootoutThenConfirmPoll(t *testing.T, f *fakeSys) {
	t.Helper()
	if len(f.runs) < 2 {
		t.Fatalf("expected bootout + >=1 confirm poll, got %v", f.runs)
	}
	if f.runs[0].name != "launchctl" ||
		strings.Join(f.runs[0].args, " ") != "bootout gui/501/com.officraft.ocwarden" {
		t.Fatalf("first run must be the exact-label bootout, got %v", f.runs[0])
	}
	if f.runs[1].name != "launchctl" ||
		strings.Join(f.runs[1].args, " ") != "print gui/501/com.officraft.ocwarden" {
		t.Fatalf("second run must be the exact-label confirm poll, got %v", f.runs[1])
	}
	assertNoForbiddenProcessKill(t, f)
}

func TestResolveTeardownPaths(t *testing.T) {
	p, err := resolveTeardownPaths(envFn(map[string]string{"HOME": "/h"}), 501)
	if err != nil {
		t.Fatal(err)
	}
	if p.tokfile != "/h/.officraft/warden/exec-warden.tok" {
		t.Errorf("tokfile = %q", p.tokfile)
	}
	if p.plistPath != "/h/Library/LaunchAgents/com.officraft.ocwarden.plist" {
		t.Errorf("plistPath = %q", p.plistPath)
	}
	if p.guiDomain != "gui/501" {
		t.Errorf("guiDomain = %q", p.guiDomain)
	}
	if _, err := resolveTeardownPaths(envFn(map[string]string{}), 1); err == nil {
		t.Error("expected error when HOME unset")
	}
}

// TestRunTeardown_BootoutExactLabelThenConfirmThenRemove: teardown boots out ONLY
// the exact label, polls `launchctl print` until launchd confirms the label gone
// (bootout is async), and removes exactly the tokfile + plist — never a
// pattern-kill.
func TestRunTeardown_BootoutExactLabelThenConfirmThenRemove(t *testing.T) {
	f := newFakeSys()
	f.runFn = labelGoneRunFn
	i := &installer{out: io.Discard, sys: f.ops()}
	p := fixedTeardownPaths()
	if ok := i.runTeardown(p); !ok {
		t.Fatal("runTeardown must report ok=true for a confirmed teardown")
	}
	assertBootoutThenConfirmPoll(t, f)
	if len(f.removed) != 2 || f.removed[0] != p.tokfile || f.removed[1] != p.plistPath {
		t.Fatalf("removed = %v, want [tokfile plist]", f.removed)
	}
}

// TestRunTeardown_Idempotent: an already-torn-down install (bootout non-zero =
// not-loaded, the confirm poll's first probe reports the label gone, files already
// gone) still returns success.
func TestRunTeardown_Idempotent(t *testing.T) {
	f := newFakeSys()
	f.runFn = func(name string, args ...string) (string, error) {
		return "", fmt.Errorf("Boot-out failed: 3: No such process") // not loaded / not found
	}
	p := fixedTeardownPaths()
	f.removeErr[p.tokfile] = os.ErrNotExist
	f.removeErr[p.plistPath] = os.ErrNotExist
	i := &installer{out: io.Discard, sys: f.ops()}
	if ok := i.runTeardown(p); !ok {
		t.Fatal("teardown must be idempotent (fully-absent install is ok=true)")
	}
	if f.slept != 0 {
		t.Errorf("an already-gone label must confirm on the first probe with zero sleeps, slept=%d", f.slept)
	}
}

func TestRunTeardown_DryRunTouchesNothing(t *testing.T) {
	f := newFakeSys()
	i := &installer{out: io.Discard, dryRun: true, sys: f.ops()}
	if ok := i.runTeardown(fixedTeardownPaths()); !ok {
		t.Fatal("dry-run teardown must report ok=true")
	}
	if len(f.runs) != 0 || len(f.removed) != 0 {
		t.Errorf("dry-run teardown mutated something: runs=%v removed=%v", f.ranNames(), f.removed)
	}
}

// ---------------------------------------------------------------------------
// doTeardown — the PURE core (used by BOTH the CLI and the uninstall RPC). It
// returns (ok, log) WITHOUT exiting or calling errf.
// ---------------------------------------------------------------------------

// TestDoTeardown_OK_BootoutConfirmedThenRemove_ReturnsTrueAndLog: a clean teardown
// boots out the EXACT label, CONFIRMS via the `launchctl print` poll that launchd
// reports the label gone, removes exactly tokfile + plist, and returns
// (true, non-empty log).
func TestDoTeardown_OK_BootoutConfirmedThenRemove_ReturnsTrueAndLog(t *testing.T) {
	f := newFakeSys()
	f.runFn = labelGoneRunFn
	p := fixedTeardownPaths()
	ok, log := doTeardown(f.ops(), false, p)
	if !ok {
		t.Fatalf("doTeardown ok = false, want true (confirmed teardown); log:\n%s", log)
	}
	assertBootoutThenConfirmPoll(t, f)
	if len(f.removed) != 2 || f.removed[0] != p.tokfile || f.removed[1] != p.plistPath {
		t.Fatalf("removed = %v, want [tokfile plist]", f.removed)
	}
	if !strings.Contains(log, "teardown complete") || !strings.Contains(log, p.tokfile) {
		t.Fatalf("log must transcribe the steps, got:\n%s", log)
	}
	if !strings.Contains(log, "CONFIRMED gone") {
		t.Fatalf("log must state the bootout was CONFIRMED, got:\n%s", log)
	}
}

// TestDoTeardown_LingeringLabel_NotConfirmed_ReturnsFalse: when launchd keeps
// reporting the label registered for the whole bounded poll (`launchctl print`
// keeps succeeding), the bootout is NOT confirmed → ok=false, even though the
// file removals succeed (best-effort cleanup still runs). This is the CONFIRM half
// the server's CONFIRM-THEN-REMOVE relies on: an unconfirmed teardown must never
// read as done, or handle_teardown_here would soft-delete a machine whose daemon
// may still be running.
func TestDoTeardown_LingeringLabel_NotConfirmed_ReturnsFalse(t *testing.T) {
	f := newFakeSys()
	// Default fake runFn: every launchctl call succeeds — `print` succeeding means
	// the label is STILL registered, so the bounded poll exhausts.
	p := fixedTeardownPaths()
	ok, log := doTeardown(f.ops(), false, p)
	if ok {
		t.Fatalf("a lingering label must make ok=false; log:\n%s", log)
	}
	if !strings.Contains(log, "NOT confirmed") || !strings.Contains(log, "INCOMPLETE") {
		t.Fatalf("an unconfirmed teardown must say so in the log, got:\n%s", log)
	}
	// Bounded: exactly bootout + bootoutPollAttempts print probes — never unbounded.
	if want := 1 + bootoutPollAttempts; len(f.runs) != want {
		t.Fatalf("poll must be bounded: runs=%d, want %d", len(f.runs), want)
	}
	if f.slept != bootoutPollAttempts {
		t.Errorf("slept=%d, want %d (one bounded sleep per probe)", f.slept, bootoutPollAttempts)
	}
	// Best-effort cleanup still removed the artifacts.
	if len(f.removed) != 2 {
		t.Errorf("best-effort removal must still run, removed=%v", f.removed)
	}
	assertNoForbiddenProcessKill(t, f)
}

// TestDoTeardown_Idempotent_AllAbsent_StillOK: bootout not-loaded + the first
// confirm probe reporting the label gone + both files already gone (os.ErrNotExist)
// is a fully-idempotent success — ok=true.
func TestDoTeardown_Idempotent_AllAbsent_StillOK(t *testing.T) {
	f := newFakeSys()
	f.runFn = func(name string, args ...string) (string, error) {
		return "", fmt.Errorf("Boot-out failed: 3: No such process")
	}
	p := fixedTeardownPaths()
	f.removeErr[p.tokfile] = os.ErrNotExist
	f.removeErr[p.plistPath] = os.ErrNotExist
	ok, _ := doTeardown(f.ops(), false, p)
	if !ok {
		t.Fatal("an already-absent install must be an idempotent ok=true")
	}
}

// TestDoTeardown_RemoveFailure_ReturnsFalse: a real (non-not-exist) removal failure on a
// required artifact makes ok=false — even when the bootout IS confirmed — the
// uninstall RPC uses this to refuse self-exit.
func TestDoTeardown_RemoveFailure_ReturnsFalse(t *testing.T) {
	f := newFakeSys()
	f.runFn = labelGoneRunFn // bootout confirmed — the failure below is the sole cause
	p := fixedTeardownPaths()
	f.removeErr[p.plistPath] = fmt.Errorf("permission denied")
	ok, log := doTeardown(f.ops(), false, p)
	if ok {
		t.Fatalf("a stubborn plist removal must make ok=false; log:\n%s", log)
	}
	if !strings.Contains(log, "INCOMPLETE") {
		t.Fatalf("an incomplete teardown must say so in the log, got:\n%s", log)
	}
}

// TestDoTeardown_DryRun_TouchesNothing: dry-run mutates nothing and reports ok=true.
func TestDoTeardown_DryRun_TouchesNothing(t *testing.T) {
	f := newFakeSys()
	ok, log := doTeardown(f.ops(), true, fixedTeardownPaths())
	if !ok {
		t.Fatal("dry-run doTeardown must report ok=true")
	}
	if len(f.runs) != 0 || len(f.removed) != 0 {
		t.Errorf("dry-run doTeardown mutated something: runs=%v removed=%v", f.ranNames(), f.removed)
	}
	if !strings.Contains(log, "DRYRUN") {
		t.Fatalf("dry-run log must mark itself, got:\n%s", log)
	}
}

// ---------------------------------------------------------------------------
// SECOND-HEIGHT GUARD (T-2257): the validation must be CALLED, not merely correct
// ---------------------------------------------------------------------------

// scanTeardownGuardSource is the teardown analogue of hostseam_test.go's
// scanHostSeamSource: a pure source scan over teardown.go, returning one
// human-readable violation per problem and empty when the structure holds.
//
// WHY a source scan and not one more behavioural test. Every other guard test in
// this package (TestValidateTeardownTarget_*, TestTeardownRefusal_*) drives
// validateTeardownTarget DIRECTLY, so all of them prove the function is CORRECT and
// none of them prove anything CALLS it. Delete the one line
//
//	if err := validateTeardownTarget(env, canonicalExplicit); err != nil { … }
//
// from teardownCmd and the entire CLI guard is gone while exactly one test in the
// package turns red (TestRealMain_BareTeardownRefusesBeforeAnyHostOperation). A
// whole safety layer resting on a single deletable test is the shape this repo
// keeps getting bitten by, so the property gets pinned at a second height, in a
// different medium.
//
// THE PROPERTY: inside teardownCmd's body, on CODE lines only, the
// validateTeardownTarget call must appear and must come BEFORE the first
// resolveTeardownPaths call — "fail closed before any path is derived" is the
// contract, and a guard that ran after path resolution would already have named
// the canonical instance. Comment lines are stripped for the same reason
// countCodeOccurrences strips them: this file discusses the call at length above,
// and a scan satisfied by its own documentation is an always-true assertion.
func scanTeardownGuardSource() []string {
	raw, err := os.ReadFile("teardown.go")
	if err != nil {
		return []string{fmt.Sprintf("cannot read teardown.go to verify the teardown guard: %v (fail closed)", err)}
	}
	lines := strings.Split(string(raw), "\n")
	start := -1
	for i, l := range lines {
		if strings.HasPrefix(l, "func teardownCmd(") {
			start = i
			break
		}
	}
	if start < 0 {
		return []string{"teardown.go no longer declares `func teardownCmd(` — the CLI entry point this guard protects has moved or been renamed; re-point this scan at it rather than deleting it"}
	}
	end := len(lines)
	for i := start + 1; i < len(lines); i++ {
		if strings.HasPrefix(lines[i], "}") {
			end = i
			break
		}
	}
	validateAt, resolveAt := -1, -1
	for i := start + 1; i < end; i++ {
		t := strings.TrimSpace(lines[i])
		if strings.HasPrefix(t, "//") {
			continue
		}
		if validateAt < 0 && strings.Contains(t, "validateTeardownTarget(env, canonicalExplicit)") {
			validateAt = i
		}
		if resolveAt < 0 && strings.Contains(t, "resolveTeardownPaths(") {
			resolveAt = i
		}
	}
	var out []string
	if validateAt < 0 {
		out = append(out, "teardownCmd's body no longer calls validateTeardownTarget(env, canonicalExplicit) on any code line. The explicit-target guard (T-2257) is the ONLY thing standing between a bare `ocwarden teardown` and this host's canonical warden — on 2026-07-25 that exact shape booted out a live warden and deleted its plist and exec token unrecoverably, leaving 7 agents unsupervised. The unit tests around validateTeardownTarget all call it directly and stay green with the call site deleted, which is why this scan exists")
	}
	if validateAt >= 0 && resolveAt >= 0 && validateAt > resolveAt {
		out = append(out, fmt.Sprintf("teardownCmd calls resolveTeardownPaths (line %d) BEFORE validateTeardownTarget (line %d). The contract is fail-closed BEFORE any path is derived: a guard that runs after resolution has already resolved the canonical instance's plist and token paths, and the next edit that moves a side effect up between them is unguarded", resolveAt+1, validateAt+1))
	}
	return out
}

// TestTeardownGuard_IsCalledFromTheCLIEntryPoint is the ONLY runner of
// scanTeardownGuardSource — it is enforcement, not a reporting wrapper, and there
// must not be a second copy of it (see cli/CLAUDE.md on the pair of identical
// no-op host-seam tests independent review defeated by replacing both bodies with
// panic).
func TestTeardownGuard_IsCalledFromTheCLIEntryPoint(t *testing.T) {
	for _, v := range scanTeardownGuardSource() {
		t.Errorf("teardown guard structure is broken: %s", v)
	}
}
