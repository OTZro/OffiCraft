package main

import (
	"strings"
	"testing"
)

// The boot-sequence seeds are read TOP-DOWN by an agent that is DOING each step
// as it reads it. Two structural properties follow, and neither of them fails
// loudly on its own — a rule that sits after the step it was meant to gate is
// indistinguishable from a rule that was never written.
//
//	(A) The runtime-environment note must appear BEFORE step 1. One of its
//	    bullets is an execution detail OF step 1 (report_waking's model id must
//	    be the real value, never a guess); at the bottom of the file, the reader
//	    has already done step 1 by the time they meet it, and skipping it emits
//	    no signal at all.
//	(B) 啟動後任務盤點與排程 is a step, not an aside — so it is item 4 of the
//	    numbered list, and the preamble says 四步, not 三步.

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
			// It must also sit below the H1 + preamble, not above them.
			h1 := strings.Index(text, "# 啟動程序")
			if h1 < 0 || h1 > env {
				t.Fatalf("%s: runtime note is not under the H1 (h1=%d env=%d)",
					tc.file, h1, env)
			}
		})
	}
}

func TestBootSequenceSeedsNumberTheInventoryAsStepFour(t *testing.T) {
	for _, file := range []string{"boot_sequence.md", "boot_sequence_codex.md"} {
		t.Run(file, func(t *testing.T) {
			text := bootSeedFor(t, file)

			if !strings.Contains(text, "\n4. **啟動後任務盤點與排程（僅 member）。**") {
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
			if strings.Contains(text, "三步") {
				t.Fatalf("%s: preamble still says 三步 after the promotion to four steps", file)
			}
			if !strings.Contains(text, "依序做這四步") || !strings.Contains(text, "四步順序不可換") {
				t.Fatalf("%s: preamble does not say 四步 in both places", file)
			}
		})
	}
}
