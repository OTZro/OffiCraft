package main

// clean's guards, negative-first.
//
// This command runs on the close-out path, under time pressure, and it is the
// thing agents are told to use INSTEAD of `rm -rf`. So the assertions that
// matter are not the happy path — they are:
//   • a path outside my workdir is REFUSED and the target is byte-identical after
//   • one bad argument among good ones moves NOTHING (no half-done state)
//   • nothing is ever deleted, only moved
// A green happy-path test would be satisfied by `os.RemoveAll`, which is exactly
// the implementation this command exists to replace.

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// cleanFixture builds an isolated agents-home with ONE agent workdir and
// returns (cfg, root). Everything the tests create lives under t.TempDir(), so
// a guard that fails open would still not reach anything real.
func cleanFixture(t *testing.T) (Config, string) {
	t.Helper()
	home := t.TempDir()
	cfg := Config{Home: home, ID: "m-test01"}
	root := filepath.Join(home, "m-test01")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("fixture: %v", err)
	}
	return cfg, root
}

func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("fixture: %v", err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("fixture: %v", err)
	}
}

func mustReadFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(b)
}

func run(cfg Config, args ...string) (int, string) {
	var out bytes.Buffer
	rc := cmdClean(cfg, args, &out)
	return rc, out.String()
}

// ── the happy paths, only enough to prove the move actually happens ──────────

func TestCleanQuarantinesAFileRatherThanDeletingIt(t *testing.T) {
	cfg, root := cleanFixture(t)
	target := filepath.Join(root, "tmp", "scratch.log")
	writeFile(t, target, "keep me readable")

	rc, out := run(cfg, target)
	if rc != 0 {
		t.Fatalf("rc = %d, out = %q", rc, out)
	}
	if _, err := os.Lstat(target); !os.IsNotExist(err) {
		t.Fatalf("the target should have moved away, err = %v", err)
	}
	// 🔴 THE assertion: it is still readable. A deletion would pass "the target
	// is gone" too — this is what tells the two apart.
	parked := filepath.Join(root, "trash", "tmp", "scratch.log")
	if got := mustReadFile(t, parked); got != "keep me readable" {
		t.Fatalf("quarantined bytes changed: %q", got)
	}
	if !strings.Contains(out, parked) {
		t.Fatalf("output must say where it went; got %q", out)
	}
}

func TestCleanQuarantinesAFolderWithItsContents(t *testing.T) {
	cfg, root := cleanFixture(t)
	dir := filepath.Join(root, "work", "wt-123")
	writeFile(t, filepath.Join(dir, "a", "b.txt"), "nested")

	if rc, out := run(cfg, dir); rc != 0 {
		t.Fatalf("rc = %d, out = %q", rc, out)
	}
	if got := mustReadFile(t, filepath.Join(root, "trash", "work", "wt-123", "a", "b.txt")); got != "nested" {
		t.Fatalf("nested bytes changed: %q", got)
	}
}

// ── the guards: every one of these must refuse AND leave the target alone ────

func TestCleanRefusesEverythingOutsideMyWorkdir(t *testing.T) {
	cfg, root := cleanFixture(t)
	outsideDir := t.TempDir()

	// A neighbour agent's workdir under the SAME agents home — the most likely
	// real mistake, and the one a naive prefix check gets wrong.
	neighbour := filepath.Join(cfg.Home, "m-other9", "notes.md")
	writeFile(t, neighbour, "not mine")

	// A file reachable only by climbing out with ..
	sibling := filepath.Join(cfg.Home, "sibling.txt")
	writeFile(t, sibling, "not mine either")

	// A symlink INSIDE my root pointing at a file outside it: the path looks
	// local, the bytes are not.
	victim := filepath.Join(outsideDir, "victim.txt")
	writeFile(t, victim, "outside")
	link := filepath.Join(root, "looks-local")
	if err := os.Symlink(victim, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	cases := []struct {
		name, arg, untouched string
	}{
		{"neighbour agent workdir", neighbour, neighbour},
		{"dot-dot escape", filepath.Join(root, "..", "sibling.txt"), sibling},
		{"symlink pointing out", link, victim},
		{"filesystem root", string(filepath.Separator), ""},
		{"my workdir itself", root, ""},
		{"the quarantine dir itself", filepath.Join(root, "trash"), ""},
		{"inside the quarantine dir", filepath.Join(root, "trash", "anything"), ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rc, out := run(cfg, c.arg)
			if rc == 0 {
				t.Fatalf("must refuse %s; rc = 0, out = %q", c.arg, out)
			}
			if !strings.Contains(out, "NOTHING was moved") {
				t.Fatalf("the refusal must say nothing moved; got %q", out)
			}
			if c.untouched != "" {
				if _, err := os.Lstat(c.untouched); err != nil {
					t.Fatalf("a refused run must not touch the target: %v", err)
				}
			}
		})
	}
	// The root itself and the agents home are still whole.
	if _, err := os.Lstat(root); err != nil {
		t.Fatalf("root vanished: %v", err)
	}
}

// 🔴 The all-or-nothing assertion. One bad argument in a list of good ones must
// cost ZERO moves — a command that replaces `rm -rf` may not leave the caller
// guessing which half ran.
func TestCleanMovesNothingWhenAnyArgumentIsRefused(t *testing.T) {
	cfg, root := cleanFixture(t)
	good1 := filepath.Join(root, "tmp", "one.txt")
	good2 := filepath.Join(root, "tmp", "two.txt")
	writeFile(t, good1, "one")
	writeFile(t, good2, "two")
	bad := filepath.Join(cfg.Home, "elsewhere.txt")
	writeFile(t, bad, "elsewhere")

	rc, out := run(cfg, good1, bad, good2)
	if rc == 0 {
		t.Fatalf("must refuse the batch; out = %q", out)
	}
	for _, p := range []string{good1, good2, bad} {
		if _, err := os.Lstat(p); err != nil {
			t.Fatalf("%s must be untouched: %v", p, err)
		}
	}
	if _, err := os.Lstat(filepath.Join(root, "trash")); !os.IsNotExist(err) {
		t.Fatalf("no quarantine directory should have been created, err = %v", err)
	}
}

// ── idempotence: the property that lets an agent re-run this without thinking ─

func TestCleanIsIdempotentAndNeverOverwritesAnEarlierParkedCopy(t *testing.T) {
	cfg, root := cleanFixture(t)
	target := filepath.Join(root, "tmp", "note.txt")

	// (a) a path that is already gone is SUCCESS, not an error.
	rc, out := run(cfg, target)
	if rc != 0 {
		t.Fatalf("missing path must be rc 0; rc = %d, out = %q", rc, out)
	}
	if !strings.Contains(out, "already gone") {
		t.Fatalf("output should say it was already gone; got %q", out)
	}

	// (b) two rounds with the same name park BOTH copies — the first one is not
	// overwritten, because the parked copy is the evidence the agent may still
	// need.
	writeFile(t, target, "first")
	if rc, out := run(cfg, target); rc != 0 {
		t.Fatalf("rc = %d, out = %q", rc, out)
	}
	writeFile(t, target, "second")
	if rc, out := run(cfg, target); rc != 0 {
		t.Fatalf("rc = %d, out = %q", rc, out)
	}
	if got := mustReadFile(t, filepath.Join(root, "trash", "tmp", "note.txt")); got != "first" {
		t.Fatalf("the FIRST parked copy must survive; got %q", got)
	}
	if got := mustReadFile(t, filepath.Join(root, "trash", "tmp", "note.txt-2")); got != "second" {
		t.Fatalf("the second copy must be parked beside it; got %q", got)
	}
}

// The escape that arrives through the EXIT rather than the entrance: the path
// argument is impeccable, but the quarantine directory itself is a symlink, so
// the "move" lands outside the tree. ocwarden refuses to purge a symlinked
// trash for the same reason (cli/CLAUDE.md §5) — the writer has to agree.
func TestCleanRefusesWhenTheQuarantineDirIsASymlink(t *testing.T) {
	cfg, root := cleanFixture(t)
	elsewhere := t.TempDir()
	if err := os.Symlink(elsewhere, filepath.Join(root, "trash")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	target := filepath.Join(root, "tmp", "x.txt")
	writeFile(t, target, "mine")

	rc, out := run(cfg, target)
	if rc == 0 {
		t.Fatalf("must not move into a symlinked quarantine; out = %q", out)
	}
	if _, err := os.Lstat(target); err != nil {
		t.Fatalf("the target must be untouched: %v", err)
	}
	if entries, err := os.ReadDir(elsewhere); err != nil || len(entries) != 0 {
		t.Fatalf("nothing may land outside the tree; entries = %v err = %v", entries, err)
	}
}

// ── identity: without an id there is no "my workdir" to be inside of ─────────

func TestCleanRefusesWithoutAnAgentIdentity(t *testing.T) {
	home := t.TempDir()
	stray := filepath.Join(home, "anon", "x.txt")
	writeFile(t, stray, "x")

	for _, cfg := range []Config{
		{Home: home, ID: ""},
		{Home: "", ID: "m-test01"},
	} {
		rc, out := run(cfg, stray)
		if rc == 0 {
			t.Fatalf("must refuse without a resolvable workdir; out = %q", out)
		}
		if _, err := os.Lstat(stray); err != nil {
			t.Fatalf("nothing may move: %v", err)
		}
	}
}

func TestCleanNeedsAtLeastOnePath(t *testing.T) {
	cfg, _ := cleanFixture(t)
	rc, out := run(cfg)
	if rc == 0 {
		t.Fatalf("no arguments must not be a success; out = %q", out)
	}
	if !strings.Contains(out, "usage:") {
		t.Fatalf("it should print usage; got %q", out)
	}
}

// ── the dispatch seam: an agent that reads --help must be able to FIND it ────

func TestCleanIsDispatchedAndAdvertised(t *testing.T) {
	var out bytes.Buffer
	if rc := realMain([]string{"--help"}, func(string) string { return "" }, nil, &out); rc != 0 {
		t.Fatalf("help rc = %d", rc)
	}
	if !strings.Contains(out.String(), "clean") {
		t.Fatalf("`clean` must appear in --help, or nobody finds it: %q", out.String())
	}

	// And it reaches cmdClean rather than falling through to "unknown
	// subcommand" — proven by the argument-count refusal, which only clean says.
	out.Reset()
	rc := realMain([]string{"clean"}, func(string) string { return "" }, nil, &out)
	if rc != 2 || !strings.Contains(out.String(), "ocagent clean <path>") {
		t.Fatalf("clean must be dispatched; rc = %d out = %q", rc, out.String())
	}
	if strings.Contains(out.String(), "unknown subcommand") {
		t.Fatalf("clean fell through to the default branch: %q", out.String())
	}
}
