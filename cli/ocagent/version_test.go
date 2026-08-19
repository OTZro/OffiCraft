package main

import (
	"bytes"
	"errors"
	"runtime/debug"
	"strings"
	"testing"
)

// TestVersionSubcommand asserts the `version` alias set prints a NON-EMPTY build
// identifier and exits 0 — the operational contract Seth needs ("easily distinguish
// if the cli is the right version"). Runs via realMain so the dispatch wiring is
// covered, not just printVersion in isolation.
func TestVersionSubcommand(t *testing.T) {
	for _, arg := range []string{"version", "--version", "-v"} {
		t.Run(arg, func(t *testing.T) {
			var out bytes.Buffer
			rc := realMain([]string{arg}, func(string) string { return "" }, strings.NewReader(""), &out)
			if rc != 0 {
				t.Fatalf("%s: exit code = %d, want 0", arg, rc)
			}
			got := out.String()
			if !strings.Contains(got, "ocagent") {
				t.Errorf("%s: output missing binary name; got:\n%s", arg, got)
			}
			// self-hash must always be present and non-empty (it never depends on VCS
			// stamping), which is the always-available identity line.
			if !strings.Contains(got, "self-hash:") {
				t.Errorf("%s: output missing self-hash line; got:\n%s", arg, got)
			}
		})
	}
}

// TestPrintVersionReadsVCS asserts a stamped build's vcs.revision is surfaced, using
// an injected BuildInfo so the assertion doesn't depend on how the test binary was
// built (worktree test runs are unstamped).
func TestPrintVersionReadsVCS(t *testing.T) {
	var out bytes.Buffer
	bi := func() (*debug.BuildInfo, bool) {
		return &debug.BuildInfo{Settings: []debug.BuildSetting{
			{Key: "vcs.revision", Value: "deadbeefcafe"},
			{Key: "vcs.time", Value: "2026-07-10T00:00:00Z"},
			{Key: "vcs.modified", Value: "false"},
		}}, true
	}
	exe := func() (string, error) { return "/proc/self/fake", nil }
	read := func(string) ([]byte, error) { return []byte("binary-bytes"), nil }
	printVersion(&out, bi, exe, read)
	got := out.String()
	if !strings.Contains(got, "deadbeefcafe") {
		t.Errorf("vcs.revision not surfaced; got:\n%s", got)
	}
	// self-hash of the fixed bytes must be a stable non-empty prefix.
	if !strings.Contains(got, "self-hash:") || strings.Contains(got, "self-hash:    \n") {
		t.Errorf("self-hash empty; got:\n%s", got)
	}
}

// TestSelfHashDeterministic asserts identical bytes hash identically (the byte-parity
// oracle a human relies on) and unavailable-executable degrades gracefully.
func TestSelfHashDeterministic(t *testing.T) {
	read := func(string) ([]byte, error) { return []byte("same-bytes"), nil }
	exe := func() (string, error) { return "x", nil }
	a := selfHash(exe, read)
	b := selfHash(exe, read)
	if a != b {
		t.Errorf("self-hash not deterministic: %q vs %q", a, b)
	}
	if len(a) != selfHashPrefixLen {
		t.Errorf("self-hash prefix len = %d, want %d", len(a), selfHashPrefixLen)
	}
}

// TestPrintVersion_ReportsWhetherThisBuildWasStamped covers the build.sha line,
// which shipped with no test at all: deleting the Fprintf that prints it left the
// whole package green and the shell guard green.
//
// 🔴 IT IS ALSO AN ORACLE, NOT JUST A DISPLAY. bin/tests/agent-build-sha-guard.sh
// asks a freshly built ocagent for this value and compares it to the tree's sha,
// precisely because grepping the binary CANNOT do that job — Go auto-embeds
// vcs.revision (the full sha) when built from a clone, and the short sha is a
// prefix of it, so a substring search matches an unstamped build too. That guard
// is only as good as this line, so the two renderings are pinned here.
//
// Both go through the same TrimSpace as the connection line: the two must not
// disagree about what counts as absent, which is a disagreement no reader would
// find until they were already confused about something else.
func TestPrintVersion_ReportsWhetherThisBuildWasStamped(t *testing.T) {
	for _, tc := range []struct {
		name  string
		stamp string
		want  string
	}{
		{"stamped", "cafe1234beef", "  build.sha:    cafe1234beef"},
		{"unstamped", "", "  build.sha:    unstamped (not built by bin/build-bindist)"},
		{"blank is not a stamp", "  \t ", "  build.sha:    unstamped (not built by bin/build-bindist)"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			prev := buildSHA
			buildSHA = tc.stamp
			t.Cleanup(func() { buildSHA = prev })

			var out bytes.Buffer
			printVersion(&out, func() (*debug.BuildInfo, bool) { return nil, false },
				func() (string, error) { return "", errors.New("no exe") },
				func(string) ([]byte, error) { return nil, errors.New("no read") })

			if !strings.Contains(out.String(), tc.want) {
				t.Errorf("version output must contain %q; got:\n%s", tc.want, out.String())
			}
		})
	}
}

// TestBuildSHAOrUnstamped_NeverGuessesFromVCSMetadata is the case the guard's
// oracle rests on. build.sha answers ONE question — did bin/build-bindist stamp
// this binary — and a build with no stamp is unstamped no matter how much VCS
// metadata Go embedded alongside it.
//
// 🔴 Review made this concrete: with buildSHAOrUnstamped falling back to
// vcs.revision, `ocagent version` reported the correct sha while buildSHA was
// empty, so the connection line printed no [agent …] segment at all — and the
// shell guard, which asks version for exactly this value, went green on a fleet
// that could not name itself. Both defences blind at once, from one plausible
// two-line "improvement".
func TestBuildSHAOrUnstamped_NeverGuessesFromVCSMetadata(t *testing.T) {
	prev := buildSHA
	buildSHA = ""
	t.Cleanup(func() { buildSHA = prev })

	withRevision := func() (*debug.BuildInfo, bool) {
		return &debug.BuildInfo{Settings: []debug.BuildSetting{
			{Key: "vcs.revision", Value: "0123456789abcdef0123456789abcdef01234567"},
			{Key: "vcs.modified", Value: "false"},
		}}, true
	}
	if got := buildSHAOrUnstamped(withRevision); got != "unstamped (not built by bin/build-bindist)" {
		t.Errorf("build.sha = %q with an EMPTY stamp and vcs.revision present. It must "+
			"stay unstamped: the connection line reads the same empty buildSHA and "+
			"prints nothing, so any other answer here makes `ocagent version` and the "+
			"line disagree — and the build-sha guard trusts this value to tell it "+
			"whether bin/build-bindist ran at all", got)
	}
}
