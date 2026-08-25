package main

// taskno_prose_guard_test.go — T-5291 round 5. THE ONE FILE that holds the
// retired-sentence blocklist, and therefore the ONE file the whole-tree scan
// below excludes from itself.
//
// 🔴 WHY THIS FILE EXISTS AT ALL. Rounds 1–3 each shipped the same defect
// wearing a new costume: a SECOND, CONTRADICTORY account of what `task_no` is,
// sitting next to the one T-5291 had just corrected. Round 4 found four more
// (three in files this ticket had ALREADY edited, two of those within 20 lines
// of an edit it made, and one in a *.test.tsx). Round 5 found the same shape
// again — a design-table row copied verbatim into a story fixture, and the
// TaskCard header map still claiming "task ids resolved to display task_no".
//
// The guard that was supposed to stop this read ONE file (spec/openapi.json) in
// round 3, and A LIST OF SEVEN ROOTS in round 4. Both are INCLUDE LISTS, and
// both times the next copy turned up just outside the list. Round 5 therefore
// changes the METHOD, not the list: the denominator is the whole working tree
// minus an exclusion list. What nobody thought of is now SCANNED rather than
// skipped. The exact scanned set, and the reason for every exclusion, are
// written out above TestRetiredTaskNoSentencesDoNotLiveAnywhereInTheTree —
// that comment is meant to be literally equal to what the code does, because
// a guard whose stated reach exceeds its real reach is itself an instance of
// the defect this ticket is about.
//
// 🔴 ON THE EXCLUSION. The scan must not read the blocklist literals in this
// file as violations. The exclusion is therefore ONE FILE — this one — and this
// file was created for precisely that reason: it holds the blocklist and
// nothing else of substance. The tempting shortcut (exclude all `*_test.go` /
// `*.test.tsx`) is REJECTED on evidence: round 3's defect was a live reversed
// assertion hiding in a test file, round 4's fourth hit was a lying comment in
// TaskCard.dep-taskno.test.tsx, and round 5's first whole-tree run reported 25
// of its 72 hits inside 12 test files (*.test.tsx, *.test.ts and *_test.go) —
// comments AND `it(...)` names.
// Tests are exactly where these sentences go to hide, so tests are scanned.
//
// The blocklist stays an EXACT-PHRASE list, not semantic judgement — see the
// long note on the spec test below, which argues that case and still holds.

import (
	"bytes"
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
	// round 5. Two more shapes the earlier blocklist could not see, both found
	// by review INSIDE files this ticket had already edited:
	//
	//  · "t-xxxx" is the retired SHORT-CODE SHAPE itself. T-5291 made TaskNo
	//    return the id unchanged, so what the UI prints is a full task id
	//    (`t-e4ae5291a002`), never a four-character code. Every comment, doc
	//    row and test name still drawing the badge/chip as `T-xxxx` is a second
	//    account of what the screen shows. Prose that talks about the HISTORY
	//    must therefore describe the old shape in words ("a truncated short
	//    code") rather than reprint the literal — which is a real cost of an
	//    exact-phrase blocklist, and is accepted deliberately.
	//  · "resolved to display task_no" / "resolved to task_no" are the retired
	//    RESOLUTION-LAYER claim: they say an id gets converted into some other
	//    display value. There is no such step; deriveTaskNo is `return taskId`.
	//    Both orderings are listed because both were in the tree — an
	//    exact-phrase blocklist cannot generalise from one to the other.
	"t-xxxx",
	"resolved to display task_no",
	"resolved to task_no",
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
// So what it really is: a BLOCKLIST OF RETIRED SENTENCES — see the list itself
// for the current entries; a count written here would only go stale. It stops
// the exact prose T-5291 deleted from coming back — by a bad merge, a revert, a
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
// 🔴 THIS IS THE ROUND-5 FIX, AND IT IS A CHANGE OF METHOD. Round 4 widened the
// scan from one file to a LIST OF SEVEN ROOTS. Review then planted mutants and
// measured that the list still missed `CLAUDE.md`, `server/CLAUDE.md`,
// `server/ocserverd/ocapi.yaml` (extension not on the allowlist), and the whole
// of `cli/`, `conformance/` and `e2e_test/`. That is the fifth round in a row
// where the guard's denominator was an INCLUDE LIST and the sentence turned up
// just outside it.
//
// So the denominator is no longer a list of places to look. It is THE WHOLE
// WORKING TREE, minus an exclusion list. An include list fails silently when
// someone thinks of a place nobody listed; an exclusion list fails LOUDLY,
// because the unlisted place is scanned.
//
// WHAT IT SCANS, stated so it is literally equal to what the code does: every
// file under the repository root, at any depth, of any extension, that is not
// inside an excluded directory, is not binary, and is not oversized. That
// includes Markdown, Go, TypeScript, CSS, JSON, YAML, SQL, Python, shell,
// extension-less scripts, and this repo's agent-loaded rule files.
//
// WHAT IT EXCLUDES, and why each one:
//
//	.git                     VCS internals — object storage, not authored prose.
//	node_modules             vendored third-party code — not ours to edit.
//	__pycache__ .pytest_cache .venv .cache .ci-lock
//	                         tool caches and the per-copy CI mutex.
//	scratchpad trash         agent scratch / quarantine (both in .gitignore).
//	dist frontend/dist frontend/dist-paint-guard frontend/test-results
//	e2e_test/seven_gate/runs
//	                         build, screenshot and run OUTPUT.
//	frontend/recon-out       NOT output, despite the name: 3 tracked files, all
//	                         AGENT-AUTHORED recon / independent-review notes
//	                         kept for provenance, beside the .png captures they
//	                         describe. They are therefore NOT scanned, which is
//	                         a real hole in the denominator — measured today
//	                         (round 5) as an EMPTY one: all six blocklist
//	                         phrases score 0 in them, and the words `task_no`,
//	                         `taskno` and 任務編號 do not appear at all. If
//	                         recon notes ever start discussing the field, this
//	                         line is the one to delete. The same shape applies
//	                         to dist/officraft/BUILD.md (tracked, hand-written,
//	                         hidden behind the `dist` rule; also measured at 0).
//	server/ocserverd/{docsdist,seedsdist,webdist,bindist}
//	                         staging COPIES made by bin/build-*dist. Their
//	                         sources — docs/guide/, seeds/, frontend/ — are
//	                         scanned, so a hit is reported at the file a human
//	                         must edit instead of at a generated copy they must
//	                         not. (Round 4's scan reported
//	                         `server/ocserverd/docsdist/glossary.md:157`, which
//	                         sends the reader to the wrong file.)
//	binary files             any file with a NUL byte in its first 8 KiB —
//	                         images, fonts, compiled binaries. Prose cannot hide
//	                         in them and reading them is noise.
//	files over 4 MiB         guards against reading something enormous by
//	                         accident; nothing in this repo's prose is that big.
//	this file                the blocklist literals live here. Checked below:
//	                         the exclusion is only justified while this file
//	                         really does contain every phrase.
//
// The tempting wider exclusion — all `*_test.go` / `*.test.tsx` — is REFUSED on
// evidence: round 3's defect was a live reversed assertion hiding in a test
// file, and round 4's fourth hit was a lying comment in
// TaskCard.dep-taskno.test.tsx. Tests are exactly where these sentences go to
// hide, so tests are scanned.
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

	// Excluded by directory NAME, wherever it appears in the tree.
	skipAnywhere := map[string]bool{
		".git": true, "node_modules": true,
		"__pycache__": true, ".pytest_cache": true, ".venv": true,
		".cache": true, ".ci-lock": true,
		"scratchpad": true, "trash": true,
	}
	// Excluded by exact repo-relative PATH: output and generated copies.
	skipPaths := map[string]bool{
		"dist":                       true,
		"frontend/dist":              true,
		"frontend/dist-paint-guard":  true,
		"frontend/test-results":      true,
		"frontend/recon-out":         true,
		"e2e_test/seven_gate/runs":   true,
		"server/ocserverd/docsdist":  true,
		"server/ocserverd/seedsdist": true,
		"server/ocserverd/webdist":   true,
		"server/ocserverd/bindist":   true,
	}
	const maxFileBytes = 4 << 20

	scanned := 0
	scannedSet := map[string]bool{}
	err := filepath.Walk(repoRoot, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel := filepath.ToSlash(strings.TrimPrefix(
			filepath.Clean(path), filepath.Clean(repoRoot)+string(filepath.Separator)))
		if info.IsDir() {
			if skipAnywhere[info.Name()] || skipPaths[rel] {
				return filepath.SkipDir
			}
			return nil
		}
		if !info.Mode().IsRegular() || info.Size() > maxFileBytes {
			return nil
		}
		if rel == guardFile {
			return nil
		}
		raw, rerr := os.ReadFile(path)
		if rerr != nil {
			return rerr
		}
		head := raw
		if len(head) > 8192 {
			head = head[:8192]
		}
		if bytes.IndexByte(head, 0) >= 0 { // binary — no prose to find
			return nil
		}
		scanned++
		scannedSet[rel] = true
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
					"T-5291 was blocked on five times.\n"+
					"context: …%s…",
					rel, lineAt(off), bad, proseWindow(flat, off, len(bad)))
				at = off + len(bad)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", repoRoot, err)
	}

	// ── non-vacuity ─────────────────────────────────────────────────────────
	// 1. The places review PLANTED MUTANTS IN and found unscanned in round 4,
	//    plus one representative file from each module the include list used to
	//    miss entirely. If the walk stops reaching any of them the guard has
	//    silently narrowed back to an include list, which is the defect this
	//    round exists to end. Each entry is a file, not a directory, so a
	//    rename shows up as a loud failure with the old name in it.
	mustScan := []string{
		"CLAUDE.md",             // agent-loaded, every session
		"server/CLAUDE.md",      // agent-loaded, every session
		"frontend/CLAUDE.md",    // agent-loaded, every session
		"conformance/CLAUDE.md", // agent-loaded, every session
		"frontend/.claude/rules/tasks-and-outsource.md",
		"docs/guide/glossary.md",             // SOURCE, not the docsdist copy
		"docs/design/worker-panel-parity.md", // the design table
		"spec/openapi.json",
		"server/ocserverd/ocapi.yaml", // .yaml — off the old allowlist
		"server/ocserverd/wire.go",
		"frontend/src/components/TaskCard.tsx",
		"frontend/visual-guards/stories/TaskCardSixColTableStory.tsx",
		"cli/ocagent/listen.go",     // cli/ — off the old root list
		"conformance/test_tasks.py", // .py — off the old allowlist
		"e2e_test/lib/common.sh",    // e2e_test/ + .sh — off the old list
		"seeds/boot_sequence.md",
		"bin/build-docsdist", // extension-less script
		"Makefile",           // extension-less
	}
	for _, f := range mustScan {
		if !scannedSet[f] {
			t.Errorf("%s was NOT scanned — the walk no longer reaches it, so "+
				"this guard has narrowed back into an include list", f)
		}
	}
	// 2. A floor on the total. The actual count is not written down here — it
	//    drifts with every added file and a stale number is exactly the kind of
	//    second, contradictory account this whole file exists to stop. The floor
	//    is deliberately far below the real figure so ordinary deletions do not
	//    trip it, while a broken walk (0, or a handful) does.
	if scanned < 500 {
		t.Fatalf("only %d files scanned — expected at least 500; the walk is "+
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
