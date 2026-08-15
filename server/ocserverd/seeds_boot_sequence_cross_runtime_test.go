// One runtime's boot document must never carry the OTHER runtime's listener
// instruction (T-51b0, restored on the T-99a6 review).
//
// 🔴 WHY THIS ONE CAME BACK WHEN THE REST OF THAT FILE DID NOT. The owner's
// ruling (2026-08-15) was that guards pinning HIS WORDING are worthless: he
// rewrites those documents by hand and every edit turned tests red with nothing
// broken. That ruling stands, and the four wording guards stay deleted.
//
// This assertion is a different kind. It pins no phrasing and forbids no way of
// writing anything — it only says a document must not contain the instruction
// belonging to the OTHER runtime, which is a fact about the runtimes, not about
// prose. The review demonstrated the gap by pasting codex's "the sidecar owns
// the listener, do not start it yourself" into the CLAUDE seed: the whole
// ocserverd suite stayed green. A claude agent reading that hands control back
// to a sidecar that does not exist for it, and then never comes online — and an
// agent that never comes online cannot report that anything is wrong.
//
// The sibling guard in worker_spawn_test.go is NOT this: it checks that the
// right FILE reaches the right runtime. It is alive (flipping the runtime→seed
// mapping reds a dozen assertions) and it says nothing about what is written
// inside the file it delivered.
package main

import (
	"strings"
	"testing"
)

func TestNeitherBootSeedCarriesTheOtherRuntimesListenerInstruction(t *testing.T) {
	claude, err := assetRoot("").readSeedFile("boot_sequence.md")
	if err != nil {
		t.Fatalf("read boot_sequence.md: %v", err)
	}
	codex, err := assetRoot("").readSeedFile("boot_sequence_codex.md")
	if err != nil {
		t.Fatalf("read boot_sequence_codex.md: %v", err)
	}

	// Positive control FIRST: each document must still carry ITS OWN
	// instruction, so the two absences below cannot be satisfied by a pair of
	// documents that say nothing about the listener at all.
	if !strings.Contains(claude, "Monitor") {
		t.Fatal("the claude seed no longer tells its reader to hold the listener " +
			"under Monitor — the exclusions below would then be vacuous")
	}
	if !strings.Contains(codex, "sidecar") {
		t.Fatal("the codex seed no longer mentions the sidecar that owns its " +
			"listener — the exclusions below would then be vacuous")
	}

	// A claude session has no sidecar. Any sentence about one reached it by
	// being copied from the other document.
	if strings.Contains(claude, "sidecar") {
		t.Error("the claude boot document talks about a sidecar. Under claude the " +
			"session hangs `ocagent listen` itself; a reader that hands control " +
			"back to a sidecar waits forever for something that will never call " +
			"it, and an agent that never comes online reports nothing.")
	}

	// And the reverse: codex must not be told to mount the listener itself. Its
	// own text legitimately names Monitor inside a PROHIBITION, so the probe is
	// claude's positive instruction, not the bare word.
	for _, claudeOnly := range []string{"用內建 Monitor 工具在背景掛住", "在背景掛住 `ocagent listen`"} {
		if strings.Contains(codex, claudeOnly) {
			t.Errorf("the codex boot document carries claude's own listener "+
				"instruction (%q). Two listeners on one identity is what the "+
				"server refuses with 409, so the second one never connects.",
				claudeOnly)
		}
	}
}
