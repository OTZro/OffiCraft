package main

// clean — the ONE entry an agent uses to get rid of a file or a folder it made.
//
// It exists because the offboard document used to spell out the PROCEDURE
// ("mv it into <workdir>/trash/, do not rm it yourself"), and a procedure
// written in prose is a second source of truth that nothing keeps in step: move
// the quarantine directory, change who reclaims it, decide something should be
// kept — and that paragraph becomes a lie with nothing to redden. Owner
// 2026-08-16: 「收拾程序我在想是不是應該改成 ocagent 的 command... 例如
// `ocagent clean <path>` 不要把實作暴露出來」, and 2026-08-20 on the scope:
// 「可以指定 file / folder, 用來取代 rm -rf」.
//
// 🔴 IT DELETES NOTHING. "Replaces rm -rf" is about the ENTRY, not the effect:
// the owner's own contract for this command says quarantine/move, never rm. So
// the target is RENAMED under <root>/trash/ and stays readable — a wrong path
// costs a move, not the file. Nothing in here may grow an os.RemoveAll of a
// caller-named path; the guard test asserts that by name.
//
// WHAT IT IS NOT
//
//   - It does NOT collect branches, worktrees or processes. Those are different
//     resources and ocagent has ZERO knowledge of which worktree or which pid
//     belongs to this agent — nothing registers them anywhere. Guessing is not
//     available to a command that moves things, so the offboard document keeps
//     describing those three by hand and says why (owner 2026-08-20, scope
//     ruling above).
//   - It does NOT scan. Every path is named by the caller; there is no "find my
//     junk" mode, because "what is junk" is exactly the judgement a command
//     cannot make on the agent's behalf.

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// quarantineDirName is the ONE place the quarantine location is decided. The
// offboard document deliberately no longer names it: whoever moves it moves it
// here and every agent follows on the next binary, which is the entire point of
// turning the procedure into a command.
const quarantineDirName = "trash"

// cleanRoot resolves the ONLY tree this command may touch: this agent's own
// workdir. Same derivation as cursorPath / seenPath / stampPath (listen.go,
// contextreport.go) — one expression for "where my files live", not four.
//
// An empty id is a REFUSAL, not a fallback. cursorPath degrades to "anon"
// because losing a dedup cursor costs a refetch; here the same fallback would
// point two identity-less sessions at ONE quarantine tree and let either move
// the other's files. The cheap failure and the expensive one are not the same
// call.
func cleanRoot(cfg Config) (string, error) {
	if strings.TrimSpace(cfg.ID) == "" {
		return "", errors.New("no agent id (OC_ID / OC_TOKEN): cannot tell which workdir is mine")
	}
	if strings.TrimSpace(cfg.Home) == "" {
		return "", errors.New("no agent home (OC_AGENT_HOME): cannot tell which workdir is mine")
	}
	root := filepath.Join(cfg.Home, strings.ToLower(cfg.ID))
	// Resolve the root through symlinks ONCE, so a symlinked home (a real shape
	// on macOS, where /tmp is a link to /private/tmp) does not make every target
	// look like an escape.
	if resolved, err := filepath.EvalSymlinks(root); err == nil {
		root = resolved
	}
	return filepath.Clean(root), nil
}

// canonicalise resolves a path through symlinks WITHOUT requiring it to exist:
// it walks up to the deepest ancestor that does exist, resolves that, and
// re-attaches the missing tail. EvalSymlinks alone cannot be used because it
// fails on a missing leaf — and a missing leaf is ordinary here (an idempotent
// re-run, or a quarantine directory that has not been created yet).
//
// What it still catches is the escape that matters: a symlink anywhere in the
// part that DOES exist.
func canonicalise(abs string) string {
	abs = filepath.Clean(abs)
	existing := abs
	var tail []string
	for {
		if _, err := os.Lstat(existing); err == nil {
			break
		}
		parent := filepath.Dir(existing)
		if parent == existing {
			// Walked to the filesystem root without finding anything that exists.
			break
		}
		tail = append([]string{filepath.Base(existing)}, tail...)
		existing = parent
	}
	if resolved, err := filepath.EvalSymlinks(existing); err == nil {
		existing = resolved
	}
	return filepath.Clean(filepath.Join(append([]string{existing}, tail...)...))
}

// isUnder reports whether path is strictly inside root (the root itself is not).
func isUnder(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	if err != nil || rel == "." {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// resolveInsideRoot canonicalises one caller-named path and proves it lands
// strictly inside root.
//
// 🔴 The subtle half is that the target may NOT EXIST (idempotent re-runs), and
// EvalSymlinks fails on a missing leaf. So it resolves the DEEPEST EXISTING
// ANCESTOR and re-appends the missing tail: that still catches the attack that
// matters — a symlink somewhere in the existing part pointing out of the tree —
// while letting `clean` be re-run against something already gone.
//
// "Strictly inside" excludes the root itself: `ocagent clean <my workdir>`
// would quarantine the agent's whole home INTO its own quarantine directory,
// which is both nonsense and unrecoverable in one step.
func resolveInsideRoot(root, arg string) (string, error) {
	if strings.TrimSpace(arg) == "" {
		return "", errors.New("empty path")
	}
	abs, err := filepath.Abs(arg)
	if err != nil {
		return "", fmt.Errorf("cannot resolve: %w", err)
	}
	full := canonicalise(abs)

	rel, err := filepath.Rel(root, full)
	if err != nil {
		return "", fmt.Errorf("outside my workdir: %s", arg)
	}
	if rel == "." {
		return "", fmt.Errorf("that is my workdir itself, not something in it: %s", arg)
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("outside my workdir: %s", arg)
	}

	// The quarantine directory is not cleanable: moving trash into trash nests
	// forever, and the whole point of quarantine is that it is the one place
	// this command does not touch.
	first := strings.Split(rel, string(filepath.Separator))[0]
	if first == quarantineDirName {
		return "", fmt.Errorf("that is already in %s/: %s", quarantineDirName, arg)
	}
	return full, nil
}

// quarantineDest picks where one target lands under <root>/trash/, PRESERVING
// its path relative to the root so the move stays readable ("what was this?"
// is answerable a day later). A destination that already exists gets -2, -3, …
// rather than being overwritten — re-running clean must never destroy the
// evidence the previous run parked.
func quarantineDest(root, target string) (string, error) {
	rel, err := filepath.Rel(root, target)
	if err != nil {
		return "", err
	}
	base := filepath.Join(root, quarantineDirName, rel)
	dest := base
	for i := 2; ; i++ {
		if _, err := os.Lstat(dest); os.IsNotExist(err) {
			break
		}
		if i > 1000 {
			return "", fmt.Errorf("too many quarantined copies of %s", rel)
		}
		dest = fmt.Sprintf("%s-%d", base, i)
	}

	// 🔴 The landing site has to be proven inside the tree, not assumed.
	//
	// resolveInsideRoot guards the ENTRANCE; this guards the EXIT, and the two
	// are separate escapes. Checking only `<root>/trash` itself is NOT enough:
	// `dest` is `<root>/trash/<rel>`, and both MkdirAll and Rename follow every
	// intermediate component — so a symlink one level deeper (say
	// `<root>/trash/tmp -> /somewhere/else`) sends the file out of the tree
	// while the command prints an in-tree path and exits 0. That shape is
	// reachable in two ordinary steps: clean a directory that itself contains
	// an outward symlink (it is parked under trash/ intact), then later clean a
	// real path whose rel traverses that parked name.
	//
	// So: create the parent first, then resolve it and require it to still be
	// under root. This mirrors what ocwarden does before purging trash
	// (cli/ocwarden/trash.go compares EvalSymlinks on both sides) — one half
	// checking with Lstat and the other with a real resolution would be two
	// different definitions of the same directory.
	parent := filepath.Dir(dest)
	// 🔴 Check BEFORE creating. MkdirAll follows symlinks too, so validating
	// afterwards is already too late: it would have created a directory on the
	// far side of the link. canonicalise gives the resolved path without needing
	// it to exist first, which is exactly the case here.
	if !isUnder(root, canonicalise(parent)) {
		return "", fmt.Errorf("the %s/ path leaves my workdir", quarantineDirName)
	}
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return "", fmt.Errorf("cannot open quarantine: %w", err)
	}
	// And re-check what actually exists now: between the two calls the tail may
	// have been created as a symlink, which the pre-check could not have seen.
	if resolved, err := filepath.EvalSymlinks(parent); err != nil || !isUnder(root, resolved) {
		return "", fmt.Errorf("the %s/ path leaves my workdir", quarantineDirName)
	}
	return dest, nil
}

// cmdClean is the whole command: validate EVERY path, then move.
//
// 🔴 The two phases are not cosmetic. This runs on the close-out path, under
// time pressure, and it replaces `rm -rf` — so a run that half-succeeds is the
// worst outcome available: the agent cannot tell what happened and neither can
// the next one. Every refusal therefore costs zero moves.
func cmdClean(cfg Config, args []string, out io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(out, "[ocagent] clean: at least one <path> argument is required")
		fmt.Fprintln(out, "usage: ocagent clean <path>...")
		return 2
	}

	root, err := cleanRoot(cfg)
	if err != nil {
		fmt.Fprintf(out, "[ocagent] clean: %v\n", err)
		return 2
	}

	// ── phase 1: validate all, move none ────────────────────────────────────
	// Each entry keeps the caller's spelling beside the resolved path: the
	// output has to name the path THEY typed, and pairing them here means the
	// two can never drift apart if the all-or-nothing gate below is ever
	// relaxed (index-matching against args would silently mislabel every line
	// after the first refusal).
	type target struct{ arg, full string }
	targets := make([]target, 0, len(args))
	var refusals []string
	for _, a := range args {
		full, err := resolveInsideRoot(root, a)
		if err != nil {
			refusals = append(refusals, fmt.Sprintf("  %s — %v", a, err))
			continue
		}
		targets = append(targets, target{arg: a, full: full})
	}
	if len(refusals) > 0 {
		fmt.Fprintf(out, "[ocagent] clean: refused, NOTHING was moved (my workdir is %s)\n", root)
		for _, r := range refusals {
			fmt.Fprintln(out, r)
		}
		return 2
	}

	// ── phase 2: move ───────────────────────────────────────────────────────
	rc := 0
	for _, t := range targets {
		if _, err := os.Lstat(t.full); os.IsNotExist(err) {
			// Idempotent: already gone is the state the caller asked for.
			fmt.Fprintf(out, "[ocagent] clean: %s — already gone\n", t.arg)
			continue
		}
		dest, err := quarantineDest(root, t.full)
		if err != nil {
			fmt.Fprintf(out, "[ocagent] clean: %s — %v\n", t.arg, err)
			rc = 1
			continue
		}
		if err := os.Rename(t.full, dest); err != nil {
			fmt.Fprintf(out, "[ocagent] clean: %s — not moved: %v\n", t.arg, err)
			rc = 1
			continue
		}
		fmt.Fprintf(out, "[ocagent] clean: %s → %s\n", t.arg, dest)
	}
	return rc
}
