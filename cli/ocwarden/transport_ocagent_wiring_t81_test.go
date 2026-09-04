package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// T-81, second round. The first round guarded resolveOcAgentBin and start(); an
// independent reviewer then pointed at the ONE line nothing was watching — the line in
// the production deps literal that hands start() its resolver. Setting that single field
// to nil restored the original defect in full (every member on a fresh machine gets a
// dangling symlink and never comes online) and the ENTIRE package still went green.
//
// A test can guard a function and still not guard its caller. This file is the caller's
// guard: it asserts that the deps production actually builds have the seam wired.
func TestBuildSpawnDeps_WiresTheOcAgentResolver(t *testing.T) {
	env := func(string) string { return "" }
	deps := buildSpawnDeps(Config{Base: fxBase}, env, &recRunner{}, fxSocket, "")

	if deps.ResolveOcAgentBin == nil {
		t.Fatal("production SpawnDeps ships without an ocagent resolver — this is the exact " +
			"one-line regression that reintroduces T-81, and start() would refuse every spawn " +
			"on every machine")
	}

	// And it must be a LIVE question, not a captured answer: calling it twice must go
	// back to the filesystem each time. We cannot control $HOME here, so we assert the
	// weaker but still load-bearing property — it answers, and it answers the same shape
	// twice without panicking or memoising a first-call error into permanence.
	p1, _ := deps.ResolveOcAgentBin()
	p2, _ := deps.ResolveOcAgentBin()
	if p1 != p2 {
		t.Errorf("resolver is not deterministic within one filesystem state: %q then %q", p1, p2)
	}
	if !strings.HasSuffix(p1, "ocagent") {
		t.Errorf("resolver answered %q, want a path ending in the ocagent binary name", p1)
	}
}

// TestFileExists_AnswersTheFilesystem and TestNewOcAgentResolver_PassesTheVerdictThrough
// are the second review round's finding, and it was sharper than the first. The wiring
// line used to carry the existence probe written out inline. Changing that one word —
// `func(p string) bool { return true }` — makes the resolver claim every path is there,
// which IS T-81 (a symlink published to nothing), and the whole package stayed green.
//
// Worse, the guard that should have caught it was the one that stepped aside: the old
// t.Skipf fired `if present`, so "always claim present" was exactly the condition that
// silenced it. A guard whose give-up condition points the same way as the defect is not
// a guard.
//
// So behaviour moved off the wiring line into two named functions, and here is one test
// per function.
func TestFileExists_AnswersTheFilesystem(t *testing.T) {
	dir := t.TempDir()
	present := filepath.Join(dir, "here")
	if err := os.WriteFile(present, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if !fileExists(present) {
		t.Errorf("fileExists(%q) = false for a file that is right there", present)
	}
	missing := filepath.Join(dir, "not-here")
	if fileExists(missing) {
		t.Errorf("fileExists(%q) = true for a path that does not exist — this is the "+
			"one-word change that reinstates T-81: the resolver then vouches for a "+
			"binary that is not on disk and the spawn publishes a link to nothing", missing)
	}
}

func TestNewOcAgentResolver_PassesTheVerdictThrough(t *testing.T) {
	exe := func() (string, error) { return "/home/oc/.officraft/warden/ocwarden", nil }

	if path, present := newOcAgentResolver(exe, func(string) bool { return false })(); present {
		t.Errorf("resolver reported present=true at %q while the probe said nothing is there", path)
	}
	if path, present := newOcAgentResolver(exe, func(string) bool { return true })(); !present {
		t.Errorf("resolver reported present=false at %q while the probe said it is there", path)
	}

	// And it is a LIVE question: this is the download landing between two spawns. The
	// probe's answer flips because the world changed, and the resolver must go and look
	// again rather than repeat what it said last time.
	onDisk := false
	r := newOcAgentResolver(exe, func(string) bool { return onDisk })
	if _, first := r(); first {
		t.Error("before the download: probe says nothing is there, resolver said present")
	}
	onDisk = true
	if _, second := r(); !second {
		t.Error("after the download: probe says it is there, resolver still said absent — " +
			"the answer was cached, which is the exact shape of the bug this ticket exists for")
	}
}

// TestBuildSpawnDeps_ResolverVerdictMatchesTheFilesystem asserts the production
// resolver's verdict against an INDEPENDENT stat of the path it just named. It replaces
// an earlier version that called t.Skipf when the binary happened to be present — which
// meant the guard excused itself under exactly the condition a broken probe creates.
// This one asserts in both worlds and never skips.
func TestBuildSpawnDeps_ResolverVerdictMatchesTheFilesystem(t *testing.T) {
	env := func(string) string { return "" }
	deps := buildSpawnDeps(Config{Base: fxBase}, env, &recRunner{}, fxSocket, "")
	if deps.ResolveOcAgentBin == nil {
		t.Fatal("no resolver wired — see TestBuildSpawnDeps_WiresTheOcAgentResolver")
	}
	path, present := deps.ResolveOcAgentBin()
	_, err := os.Stat(path)
	if onDisk := err == nil; present != onDisk {
		t.Errorf("resolver said present=%v for %q, but the filesystem says present=%v (%v). "+
			"A resolver that disagrees with the disk is how a spawn publishes a symlink "+
			"to nothing and the member is deaf with no error anywhere.", present, path, onDisk, err)
	}
	if path == "" {
		t.Error("resolver named no path at all; the refusal message would have nothing to " +
			"point the reader at")
	}
}
