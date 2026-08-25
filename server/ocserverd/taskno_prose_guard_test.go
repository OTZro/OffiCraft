package main

// taskno_prose_guard_test.go — T-5291 round 4. THE ONE FILE that holds the
// retired-sentence blocklist, and therefore the ONE file the repo-wide scan
// below excludes from itself.
//
// 🔴 WHY THIS FILE EXISTS AT ALL (the round-4 finding). Rounds 1–3 each shipped
// the same defect wearing a new costume: a SECOND, CONTRADICTORY account of
// what `task_no` is, sitting next to the one T-5291 had just corrected. Round 4
// found three more —
//
//	frontend/src/api/adapter.ts   TaskDepRefView   "a pure projection of the id"
//	frontend/src/api/adapter.ts   OutsourceWorkerView "display number (T-xxxx)"
//	server/ocserverd/wire.go      taskDepRefDTO    "the pure projection of the id"
//
// — and a fourth in a *.test.tsx. Every one of them was inside a file this
// ticket had ALREADY edited; two were within 20 lines of an edit it made.
//
// The guard that was supposed to stop exactly this (the spec test moved into
// this file below) read ONE file: spec/openapi.json. That is the crack all
// three fell through. So the fix for round 4 is not "delete three more
// sentences" — it is WIDENING THE SCAN DENOMINATOR to every place the sentences
// can actually be written: product code, specs, generated artifacts, docs, and
// the agent rule files.
//
// 🔴 ON THE EXCLUSION. The scan must not read the blocklist literals in this
// file as violations. The exclusion is therefore ONE FILE — this one — and this
// file was created for precisely that reason: it holds the blocklist and
// nothing else of substance. The tempting shortcut (exclude all `*_test.go` /
// `*.test.tsx`) is REJECTED on evidence: round 3's defect was a live reversed
// assertion hiding in a test file, and round 4's fourth hit is a lying comment
// in TaskCard.dep-taskno.test.tsx. Tests are exactly where these sentences go
// to hide, so tests are scanned.
//
// The blocklist stays an EXACT-PHRASE list, not semantic judgement — see the
// long note on the spec test below, which argues that case and still holds.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// retiredTaskNoSentences — the phrases T-5291 deleted, verbatim. A paraphrase
// passes (that is a deliberate, stated limit, not an oversight). Kept in ONE
// place so the spec test and the repo-wide scan can never drift apart.
//
// Lower-cased; both scans compare lower-cased text.
var retiredTaskNoSentences = []string{
	// rounds 1–2 (spec prose)
	"derived from the id",
	"never a lookup key",
	"display number (t-xxxx)",
	// round 4: the wording the three surviving copies actually used. The spec
	// never carried this one, which is why a spec-only scan could not see them.
	"projection of the id",
}

// ── the spec must not carry a SECOND, contradictory account of task_no ────────
//
// T-5291 round 2. The ruling above ("the number IS the id") was written into
// three of the four “task_no“ descriptions in spec/openapi.json and missed
// the fourth — TaskDTO, the one an MCP client / agent actually reads. It still
// said the number "is the display number derived from the id (never a lookup
// key)", i.e. the exact opposite of what the code now does, and it propagated
// verbatim into BOTH generated artifacts (ocapi_gen.go, schema.ts).
//
// A prose contradiction is invisible to every other check in this repo: the
// drift targets only prove the generated copies MATCH the spec, so a wrong
// sentence stays wrong in perfect three-way agreement. This is the one place
// that reads the sentences.
//
// 🔴 WHAT THIS CATCHES, STATED HONESTLY — an earlier version of this comment
// claimed it was "phrase-based rather than an exact-text pin: the point is the
// CLAIM, not the wording". That was an over-claim, and review measured it: with
// TaskDTO reworded to
//
//	``task_no`` is a short human-facing code computed from the task id;
//	it must not be used to look a task up.
//
// this test is GREEN. Same claim, different words, straight past the scan.
//
// So what it really is: a BLOCKLIST OF RETIRED SENTENCES (three when this was
// written; round 4 added a fourth). It stops the
// exact prose T-5291 deleted from coming back — by a bad merge, a revert, a
// copy-paste from an old branch, which is how this repo actually reacquires a
// deleted sentence — and it stops nothing else. A reviewer reading a NEW
// description still has to judge the meaning themselves; no test does that for
// them.
//
// Deliberately NOT upgraded to semantic judgement. Deciding whether an
// arbitrary English sentence contradicts a ruling is a mechanism, and a fuzzy
// one: it would fire on honest history-explaining prose ("it used to be derived
// from the id") and still miss a paraphrase it had not been taught. A guard
// nobody can predict gets disabled. This one is small, exact, and says so.
func TestSpecDoesNotReacquireTheRetiredTaskNoSentences(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "spec", "openapi.json"))
	if err != nil {
		t.Fatalf("read spec: %v", err)
	}
	var doc any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("parse spec: %v", err)
	}
	// The sentences T-5291 deleted, verbatim (the shared list at the top of this
	// file — round 4 stopped it having two copies). NOT a definition of "wrong"
	// — a paraphrase of any of them passes (see the header). Each is checked
	// only on a description that actually talks about task_no, so unrelated
	// prose elsewhere is untouched.
	retired := retiredTaskNoSentences
	seen := 0
	var walk func(node any, path string)
	walk = func(node any, path string) {
		switch v := node.(type) {
		case map[string]any:
			for k, child := range v {
				if k == "description" {
					if s, ok := child.(string); ok && strings.Contains(s, "task_no") {
						seen++
						low := strings.ToLower(s)
						for _, bad := range retired {
							if strings.Contains(low, bad) {
								t.Errorf("%s.description talks about task_no and has "+
									"reacquired the retired sentence %q — TaskNo returns "+
									"the id unchanged (TestTaskNoIsTheIDItself), so this "+
									"is a second, contradictory account of the same "+
									"field, and it is the one MCP clients read.\n"+
									"full text: %s",
									path, bad, s)
							}
						}
					}
				}
				walk(child, path+"."+k)
			}
		case []any:
			for i, child := range v {
				walk(child, path+"["+strconv.Itoa(i)+"]")
			}
		}
	}
	walk(doc, "$")
	// Non-vacuity: if the spec stops mentioning task_no in descriptions at all,
	// this test would pass while measuring nothing. Four is what T-5291 found
	// (OutsourceWorkerDTO, TaskDepRefDTO, TaskListItemDTO, TaskDTO); the floor
	// is asserted, not the exact count, so adding a fifth is not a failure.
	if seen < 4 {
		t.Fatalf("only %d spec description(s) mention task_no — expected at least "+
			"4 (this guard has gone vacuous)", seen)
	}
}

// ── the SAME sentences must not live anywhere else in the tree either ────────
//
// 🔴 THIS IS THE ROUND-4 FIX. The test above reads one file. The three defects
// round 4 blocked were in three OTHER files, and they had been there, in a
// ticket that edited those very files, for three review rounds. Widening the
// denominator is the only move that makes the guard proportional to where the
// sentence can actually be written.
//
// WHAT IT SCANS: product code, spec, generated artifacts, design docs, and the
// agent-loaded prose (`.claude/rules/`, `seeds/`) — those last because an
// agent loads them EVERY session, so a false sentence there does not merely
// mislead a human reader, it gets acted on.
//
// WHAT IT EXCLUDES: this file, and only this file. See the header for why the
// obvious wider exclusions (all test files) are refused.
//
// WHY IT NORMALISES WHITESPACE FIRST: two of the three round-4 defects were
// WRAPPED across lines by the comment formatter —
//
//	// ... TaskNo is the pure projection of the
//	// id, so it is filled even when ...
//
// A line-oriented `grep` cannot see that, which is a large part of why they
// survived. So each line is stripped of its comment marker and its indentation,
// the lines are joined, and runs of whitespace collapse to one space before the
// phrases are matched. The reported line number is the line the phrase STARTS
// on.
func TestRetiredTaskNoSentencesDoNotLiveAnywhereInTheTree(t *testing.T) {
	repoRoot := filepath.Join("..", "..")

	// The one file allowed to contain the phrases: the blocklist itself.
	const guardFile = "server/ocserverd/taskno_prose_guard_test.go"

	roots := []string{
		"frontend/src",
		"frontend/visual-guards",
		"frontend/.claude",
		"server/ocserverd",
		"spec",
		"docs/design",
		// seeds/ is agent-loaded runtime prose — the same hazard class as
		// .claude/rules/: an agent acts on it. (It is scanned for the retired
		// ENGLISH sentences only; the stale Chinese sentence T-5291 round 4 found
		// there is recorded in api_bootdocs_split_t3201_test.go and needs an
		// owner ruling, not a blocklist entry.)
		"seeds",
	}
	exts := map[string]bool{
		".go": true, ".ts": true, ".tsx": true, ".js": true, ".jsx": true,
		".json": true, ".md": true, ".css": true, ".html": true,
	}
	skipDirs := map[string]bool{
		"node_modules": true, ".git": true, "dist": true, "build": true,
		"coverage": true, ".cache": true, ".next": true, "test-results": true,
	}

	scanned := 0
	perRoot := map[string]int{}
	for _, root := range roots {
		abs := filepath.Join(repoRoot, root)
		if _, err := os.Stat(abs); err != nil {
			t.Fatalf("scan root %s is gone (%v) — this guard's denominator has "+
				"silently shrunk; fix the list, do not delete the root", root, err)
		}
		err := filepath.Walk(abs, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if info.IsDir() {
				if skipDirs[info.Name()] {
					return filepath.SkipDir
				}
				return nil
			}
			if !exts[strings.ToLower(filepath.Ext(path))] {
				return nil
			}
			rel := filepath.ToSlash(strings.TrimPrefix(
				filepath.Clean(path), filepath.Clean(abs)))
			relRepo := root + rel
			if relRepo == guardFile {
				return nil
			}
			raw, rerr := os.ReadFile(path)
			if rerr != nil {
				return rerr
			}
			scanned++
			perRoot[root]++
			flat, lineAt := flattenProse(string(raw))
			for _, bad := range retiredTaskNoSentences {
				at := 0
				for {
					i := strings.Index(flat[at:], bad)
					if i < 0 {
						break
					}
					off := at + i
					t.Errorf("%s:%d carries the retired sentence %q.\n"+
						"TaskNo returns the id UNCHANGED (TestTaskNoIsTheIDItself) "+
						"and the spec says so — this is a second, contradictory "+
						"account of the same field, which is the exact defect "+
						"T-5291 was blocked on four times.\n"+
						"context: …%s…",
						relRepo, lineAt(off), bad, proseWindow(flat, off, len(bad)))
					at = off + len(bad)
				}
			}
			return nil
		})
		if err != nil {
			t.Fatalf("walk %s: %v", root, err)
		}
	}

	// ── non-vacuity ─────────────────────────────────────────────────────────
	// 1. Every root must have contributed files. A typo in a root path, or a
	//    directory move, would otherwise silently scan nothing and stay green.
	for _, root := range roots {
		if perRoot[root] == 0 {
			t.Fatalf("scan root %s matched 0 files — this guard has gone vacuous "+
				"for that root", root)
		}
	}
	// 2. A floor on the total. Measured at ~600 files when this was written; the
	//    floor is deliberately far below so ordinary deletions do not trip it,
	//    but a broken walk (0, or a handful) does.
	if scanned < 200 {
		t.Fatalf("only %d files scanned — expected at least 200; the walk is "+
			"broken and this guard is measuring almost nothing", scanned)
	}
	// 3. The excluded file must actually contain every phrase. If it does not,
	//    the blocklist has drifted away from the text it is supposed to pin (or
	//    the exclusion is now shielding something else), and the scan above is
	//    quietly weaker than it reads.
	guardRaw, err := os.ReadFile(filepath.Join(repoRoot, guardFile))
	if err != nil {
		t.Fatalf("read the excluded guard file %s: %v", guardFile, err)
	}
	guardLow := strings.ToLower(string(guardRaw))
	for _, bad := range retiredTaskNoSentences {
		if !strings.Contains(guardLow, bad) {
			t.Errorf("the blocklist entry %q does not appear in %s — the ONE "+
				"excluded file no longer justifies its exclusion", bad, guardFile)
		}
	}
}

// flattenProse strips each line's indentation and leading comment marker, joins
// the lines with single spaces, collapses whitespace runs, and lower-cases the
// result — so a sentence the formatter wrapped across lines still matches. It
// returns the flattened text and a lookup from an offset in it back to the
// 1-based SOURCE line the offset falls on.
func flattenProse(src string) (string, func(int) int) {
	lines := strings.Split(src, "\n")
	var b strings.Builder
	starts := make([]int, 0, len(lines))
	nums := make([]int, 0, len(lines))
	for i, ln := range lines {
		s := strings.TrimSpace(ln)
		for _, marker := range []string{"<!--", "//", "/*", "*/", "*", "#"} {
			if strings.HasPrefix(s, marker) {
				s = strings.TrimSpace(strings.TrimPrefix(s, marker))
				break
			}
		}
		s = strings.Join(strings.Fields(s), " ")
		starts = append(starts, b.Len())
		nums = append(nums, i+1)
		b.WriteString(s)
		b.WriteString(" ")
	}
	flat := strings.ToLower(b.String())
	lineAt := func(off int) int {
		// last line whose start is <= off
		lo, hi := 0, len(starts)-1
		ans := 1
		for lo <= hi {
			mid := (lo + hi) / 2
			if starts[mid] <= off {
				ans = nums[mid]
				lo = mid + 1
			} else {
				hi = mid - 1
			}
		}
		return ans
	}
	return flat, lineAt
}

// proseWindow returns a little context around a hit, for a message a reader can
// act on without opening the file first.
func proseWindow(flat string, off, n int) string {
	lo := off - 60
	if lo < 0 {
		lo = 0
	}
	hi := off + n + 60
	if hi > len(flat) {
		hi = len(flat)
	}
	return flat[lo:hi]
}
