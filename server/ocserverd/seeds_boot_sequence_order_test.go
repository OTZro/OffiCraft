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
//	(A) The runtime-environment note must appear BEFORE step 1. Its bullets
//	    constrain HOW every step is carried out (no interactive terminal menus —
//	    ask the owner with a reply card instead); at the bottom of the file, the
//	    reader has already worked the list by the time they meet it, and skipping
//	    it emits no signal at all.
//	(B) 接續工作 is a step, not an aside — so it is item 4 of the numbered list.
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
// ⚠️ RETIRED HERE, AND WHY — the owner rewrote both boot sequences by hand
// (2026-08-15) and three assertions this file used to make are claims his layout
// deliberately contradicts. They are recorded rather than silently dropped,
// because a guard that vanishes reads exactly like a guard that never existed:
//
//   - "the 執行環境 note must sit BELOW the preamble" — it is now its own
//     top-level section ABOVE 啟動程序. The note is no longer front-matter that
//     gets skipped; it is the first thing read. (A) keeps the half that still
//     bites: it must precede step 1.
//   - "the preamble says 四步" and "四步順序不可換 / 假 online must survive" — the
//     owner removed the step COUNT from both files on purpose (T-4762: a count
//     goes stale every time the list changes and nothing turns red). The
//     preamble now says 依序做這幾步 … 不可更改順序, so the ordering rule is still
//     pinned below; the arithmetic is not.
//   - the codex preamble's 「開機這個 turn 只做得完前三步」 — deleted with the count.
//     The boot-turn boundary is still asserted, but where a reader actually
//     meets it: in step 3 itself.
//
// ⚠️ SCOPE OF WHAT THESE TESTS ACTUALLY GUARD. They pin the three structural
// facts above and nothing more. They do NOT check that the bullets say
// anything sensible, that the steps are in a workable order, or that any of
// this matches the runtime's real behaviour — a seed can satisfy every
// assertion in this file and still be wrong prose.

// bootSeedPreambleAnchor is the one clause both preambles still share after the
// owner removed the step count — it is what identifies the preamble LINE, so
// every assertion below aims at that line rather than at wording anywhere else.
const bootSeedPreambleAnchor = "依序做這幾步"

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
		{"boot_sequence.md", "# Claude Code 執行環境"},
		{"boot_sequence_codex.md", "# Codex App Server 執行環境"},
	} {
		t.Run(tc.file, func(t *testing.T) {
			text := bootSeedFor(t, tc.file)

			env := strings.Index(text, tc.header)
			step1 := strings.Index(text, "\n1. 報 waking")
			if env < 0 || step1 < 0 {
				t.Fatalf("missing an anchor: env=%d step1=%d", env, step1)
			}
			if env > step1 {
				t.Fatalf("%s: the runtime-environment note sits AFTER step 1 "+
					"(env=%d, step1=%d) — a reader doing step 1 in order never "+
					"reaches it in time, and skipping it is silent", tc.file, env, step1)
			}
			// Positive control: the numbered list really is below, so "env note
			// comes first" is not satisfied by a file that lost its steps.
			if !strings.Contains(text, bootSeedPreambleAnchor) {
				t.Fatalf("%s: no preamble anchor found", tc.file)
			}
		})
	}
}

func TestBootSequenceSeedsNumberTheInventoryAsStepFour(t *testing.T) {
	for _, file := range []string{"boot_sequence.md", "boot_sequence_codex.md"} {
		t.Run(file, func(t *testing.T) {
			text := bootSeedFor(t, file)

			if !strings.Contains(text, "\n4. 接續工作") {
				t.Fatalf("%s: 接續工作 is not the 4th numbered item", file)
			}
			// Positive control: items 1..3 are still there, so "item 4 exists"
			// is not satisfied by a renumbered-away list.
			for _, n := range []string{"\n1. ", "\n2. ", "\n3. "} {
				if !strings.Contains(text, n) {
					t.Fatalf("%s: numbered list lost %q", file, strings.TrimSpace(n))
				}
			}
			// The preamble must not reintroduce a step COUNT. It went stale every
			// time the list changed and nothing turned red; the owner removed it
			// (T-4762) and the ordering rule below carries what it was for.
			for _, stale := range []string{"依序做這三步", "依序做這四步", "三步順序不可換", "四步順序不可換"} {
				if strings.Contains(text, stale) {
					t.Fatalf("%s: the preamble states a step count again (%q) — it is the "+
						"clause that goes false whenever the list changes, and nothing "+
						"reports it. Say 依序做這幾步 and let the list carry the number.",
						file, stale)
				}
			}
			if !strings.Contains(text, bootSeedPreambleAnchor) || !strings.Contains(text, "不可更改順序") {
				t.Fatalf("%s: preamble lost the ordering rule", file)
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
		if strings.Contains(line, bootSeedPreambleAnchor) {
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
			for _, want := range []string{bootSeedPreambleAnchor, "不可更改順序"} {
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
// 🔴 WHY THIS IS CODEX-ONLY AND WHY IT MATTERS. Under codex, the third step
// hands control back to the App Server sidecar, which only then takes over
// `ocagent listen`; step 4's own precondition is "the sidecar's SSE is already
// connected". So step 4 is NOT reachable inside the boot turn; an agent reading
// the list as one continuous turn would either stall waiting for an SSE that
// cannot arrive yet, or skip the step. The claude seed has no such boundary (its
// step 3 hangs the listener in the background via Monitor and step 4 follows in
// the same turn), so this is asserted for codex ONLY — asserting it for both
// would force false text into the claude seed.
func TestCodexSeedPutsTheInventoryStepAfterTheBootTurn(t *testing.T) {
	text := bootSeedFor(t, "boot_sequence_codex.md")

	// 🔴 BIND THE NUMBER TO THE FACT. The boundary sentence is a POSITIONAL
	// claim — 「你在那一輪才做第 4 步」 is true only while the step that ENDS the
	// boot turn really is item 3. Pinning the sentence as a literal string pins
	// nothing about the list: swapping the contents of items 2 and 3 moves the
	// boundary and every other assertion in this file stays green. So find the
	// step by NUMBER and require the boundary to be inside it.
	step3 := strings.Index(text, "\n3. ")
	step4 := strings.Index(text, "\n4. 接續工作")
	if step3 < 0 || step4 < 0 || step3 >= step4 {
		t.Fatalf("codex seed: cannot locate steps 3 and 4 (step3=%d step4=%d)", step3, step4)
	}
	// The step a reader is standing on must carry the boundary — the preamble
	// alone is not enough, because working the list top-down they meet the step,
	// not the intro. Under codex, step 3 hands control back and the sidecar only
	// then takes over SSE, so step 4 is not reachable in this turn; the step has
	// to say so, or the reader stalls waiting for an SSE that cannot arrive yet.
	for _, want := range []string{"交回 sidecar", "你在那一輪才做第 4 步"} {
		if !strings.Contains(text[step3:step4], want) {
			t.Fatalf("codex step 3 does not say the boot turn ENDS there and step 4 "+
				"belongs to the next one (missing %q)", want)
		}
	}
	// The claude seed must NOT carry this boundary — it has none, and copying it
	// there would be false: claude hangs the listener in the background and does
	// step 4 in the same turn.
	claude := bootSeedFor(t, "boot_sequence.md")
	for _, codexOnly := range []string{"你在那一輪才做第 4 步", "sidecar"} {
		if strings.Contains(claude, codexOnly) {
			t.Fatalf("the claude seed grew codex-only text (%q); under claude the "+
				"listener is hung in the background by the session itself and step 4 "+
				"follows in the same turn — there is no sidecar in that picture",
				codexOnly)
		}
	}
	// And the codex seed must keep the REASON, not just the rule: the owner's
	// 2026-08-15 ruling is that a codex session has to know it is a headless
	// session the sidecar started, because that is WHY the listener is not its
	// to hang. A bare prohibition is the kind an agent talks itself out of.
	for _, want := range []string{"headless", "sidecar 持有你的生命週期"} {
		if !strings.Contains(text, want) {
			t.Fatalf("codex seed no longer explains that it is a sidecar-started "+
				"headless session (missing %q) — the step-3 prohibition loses its "+
				"reason and reads as arbitrary", want)
		}
	}
}
