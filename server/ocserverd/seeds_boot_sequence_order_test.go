package main

import (
	"strings"
	"testing"
)

// The boot-sequence seeds are read TOP-DOWN by an agent that is DOING each step
// as it reads it. Three structural properties follow, and NONE of them fails
// loudly on its own — a rule that sits after the step it was meant to gate is
// indistinguishable from a rule that was never written, and a rule that has
// quietly become false still reads like a rule.
//
//	(A) The runtime-environment note must appear BEFORE step 1. One of its
//	    bullets is an execution detail OF step 1 (report_waking's model id must
//	    be the real value, never a guess); at the bottom of the file, the reader
//	    has already done step 1 by the time they meet it, and skipping it emits
//	    no signal at all.
//	(B) 啟動後任務盤點與排程 is a step, not an aside — so it is item 4 of the
//	    numbered list, and the preamble says 四步, not 三步.
//	(C) The preamble must not say where 掛 SSE sits in the list. It used to
//	    ("掛 SSE 一律排在最後"), and promoting the inventory to step 4 made that
//	    FALSE. The owner's ruling (2026-08-13, verbatim 「掛 SSE 一律排在最後 ->
//	    可以拿掉」) was to DELETE it rather than restate it, and the reason
//	    generalises: step 3 already says 「全部就緒後，才掛 ocagent listen」, so
//	    the preamble clause was a SECOND COPY of that rule — and being a second
//	    copy is exactly why it could drift out of agreement with the body. The
//	    fix for a duplicated rule is to delete the duplicate, not to word it
//	    more carefully.
//
//	    This one is a REGRESSION guard, and that is the point: a helpful reader
//	    who notices the preamble "forgot" to mention SSE will put it back, and
//	    nothing about that edit looks wrong at the time.
//
// ⚠️ SCOPE OF WHAT THESE TESTS ACTUALLY GUARD. They pin the three structural
// facts above and nothing more. They do NOT check that the bullets say
// anything sensible, that the steps are in a workable order, or that any of
// this matches the runtime's real behaviour — a seed can satisfy every
// assertion in this file and still be wrong prose.

func bootSeedFor(t *testing.T, name string) string {
	t.Helper()
	text, err := assetRoot("").readSeedFile(name)
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	return text
}

func TestBootSequenceSeedsPutTheRuntimeNoteBeforeStepOne(t *testing.T) {
	for _, tc := range []struct{ file, header string }{
		{"boot_sequence.md", "## Claude Code 執行環境"},
		{"boot_sequence_codex.md", "## Codex App Server 執行環境"},
	} {
		t.Run(tc.file, func(t *testing.T) {
			text := bootSeedFor(t, tc.file)

			env := strings.Index(text, tc.header)
			step1 := strings.Index(text, "\n1. **報 waking")
			if env < 0 || step1 < 0 {
				t.Fatalf("missing an anchor: env=%d step1=%d", env, step1)
			}
			if env > step1 {
				t.Fatalf("%s: the runtime-environment note sits AFTER step 1 "+
					"(env=%d, step1=%d) — a reader doing step 1 in order never "+
					"reaches it in time, and skipping it is silent", tc.file, env, step1)
			}
			// It must also sit BELOW the preamble, not above it: the preamble
			// is what tells the reader there is an ordered list at all, so a
			// block hoisted above it reads as front-matter and gets skipped.
			//
			// (This deliberately anchors on the PREAMBLE, not on the H1. An
			// earlier draft asserted `h1 < env`, which was near-tautological —
			// the H1 is the first line of the file, so strings.Index returns 0
			// and the comparison could essentially never fail. It looked like a
			// guard and guarded nothing.)
			pre := strings.Index(text, "依序做這四步")
			if pre < 0 {
				t.Fatalf("%s: no preamble anchor found", tc.file)
			}
			if pre > env {
				t.Fatalf("%s: runtime note sits ABOVE the preamble (preamble=%d env=%d) — "+
					"it reads as front-matter there and gets skipped", tc.file, pre, env)
			}
		})
	}
}

func TestBootSequenceSeedsNumberTheInventoryAsStepFour(t *testing.T) {
	for _, file := range []string{"boot_sequence.md", "boot_sequence_codex.md"} {
		t.Run(file, func(t *testing.T) {
			text := bootSeedFor(t, file)

			if !strings.Contains(text, "\n4. **啟動後任務盤點與排程") {
				t.Fatalf("%s: 啟動後任務盤點與排程 is not the 4th numbered item", file)
			}
			// Positive control: items 1..3 are still there, so "item 4 exists"
			// is not satisfied by a renumbered-away list.
			for _, n := range []string{"\n1. **", "\n2. **", "\n3. **"} {
				if !strings.Contains(text, n) {
					t.Fatalf("%s: numbered list lost %q", file, strings.TrimSpace(n))
				}
			}
			// The preamble must agree: promoting the note to a step and leaving
			// the count at 三 makes the file contradict itself.
			// Ban the STALE PHRASES, not the bare substring "三步": the codex
			// preamble legitimately says 「開機這個 turn 只做得完前三步」 now,
			// and a substring ban would have made that true sentence unsayable.
			for _, stale := range []string{"依序做這三步", "三步順序不可換"} {
				if strings.Contains(text, stale) {
					t.Fatalf("%s: preamble still says %q after the promotion to four steps",
						file, stale)
				}
			}
			if !strings.Contains(text, "依序做這四步") || !strings.Contains(text, "四步順序不可換") {
				t.Fatalf("%s: preamble does not say 四步 in both places", file)
			}
		})
	}
}

// bootSeedPreamble returns the one-line preamble (the paragraph that states the
// step count and the ordering rule), so the assertions below cannot be
// satisfied or broken by wording anywhere else in the file.
func bootSeedPreamble(t *testing.T, file string) string {
	t.Helper()
	for _, line := range strings.Split(bootSeedFor(t, file), "\n") {
		if strings.Contains(line, "依序做這四步") {
			return line
		}
	}
	t.Fatalf("%s: no preamble line found", file)
	return ""
}

func TestBootSequencePreambleNeverSaysWhereHangingSSESits(t *testing.T) {
	for _, file := range []string{"boot_sequence.md", "boot_sequence_codex.md"} {
		t.Run(file, func(t *testing.T) {
			pre := bootSeedPreamble(t, file)

			// Deleted on the owner's ruling, and it is the kind of sentence a
			// well-meaning reader adds back. Step 3 already carries this rule;
			// a second copy in the preamble is what let it drift into being
			// false when the inventory became step 4.
			for _, banned := range []string{"最後", "排在"} {
				if strings.Contains(pre, banned) {
					t.Fatalf("%s: the preamble is talking about WHERE 掛 SSE sits (%q). "+
						"That clause was deleted deliberately — step 3 already says "+
						"「全部就緒後，才掛 ocagent listen」, and the preamble copy is what "+
						"went stale when 任務盤點 became step 4. Delete it again; do not "+
						"reword it.\npreamble: %s", file, banned, pre)
				}
			}
			// The half the owner kept must survive: deleting the whole
			// sentence would satisfy the ban above and lose the ordering rule
			// with it.
			for _, want := range []string{"依序做這四步", "四步順序不可換", "假 online"} {
				if !strings.Contains(pre, want) {
					t.Fatalf("%s: preamble lost %q — the ban above must not be "+
						"satisfied by deleting the sentence.\npreamble: %s", file, want, pre)
				}
			}
		})
	}
}

// The codex seed MUST say that the inventory step falls OUTSIDE the boot turn.
//
// 🔴 WHY THIS IS CODEX-ONLY AND WHY IT MATTERS. Under codex, the third step IS
// "finish this boot turn" — the App Server sidecar only takes over `ocagent
// listen` after the turn ends, and step 4's own precondition is "the sidecar's
// SSE is already connected". So step 4 is NOT reachable inside the boot turn;
// an agent reading the list as one continuous turn would either stall waiting
// for an SSE that cannot arrive yet, or skip the step. The claude seed has no
// such boundary (its step 3 hangs the listener in the background via Monitor
// and step 4 follows in the same turn), so this is asserted for codex ONLY —
// asserting it for both would force false text into the claude seed.
func TestCodexSeedPutsTheInventoryStepAfterTheBootTurn(t *testing.T) {
	text := bootSeedFor(t, "boot_sequence_codex.md")

	// The step itself must carry the caveat — the preamble alone is not enough,
	// because a reader working the list top-down meets the step, not the intro.
	step4 := strings.Index(text, "\n4. **啟動後任務盤點與排程")
	if step4 < 0 {
		t.Fatal("codex seed: no step 4 found")
	}
	tail := text[step4:]
	if !strings.Contains(tail, "不在 boot turn 之內") {
		t.Fatal("codex step 4 does not say it falls OUTSIDE the boot turn. " +
			"Under codex the previous step ENDS the turn and the sidecar only " +
			"then takes over SSE, so this step cannot run in the same turn — " +
			"a reader working the list straight through would wait for an SSE " +
			"that cannot arrive yet.")
	}
	// And the preamble must not claim all four fit in the boot turn.
	pre := bootSeedPreamble(t, "boot_sequence_codex.md")
	if !strings.Contains(pre, "開機這個 turn 只做得完前三步") {
		t.Fatalf("codex preamble does not scope the boot turn to the first three "+
			"steps.\npreamble: %s", pre)
	}
	// The claude seed must NOT carry this caveat — it has no such boundary, and
	// copying it there would be false.
	if strings.Contains(bootSeedFor(t, "boot_sequence.md"), "不在 boot turn 之內") {
		t.Fatal("the claude seed grew the codex-only boot-turn caveat; under claude " +
			"the listener is hung in the background and step 4 follows in the same turn")
	}
}
