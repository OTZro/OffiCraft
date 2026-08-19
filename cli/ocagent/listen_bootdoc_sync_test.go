package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The claude 啟動程序 seed tells the agent, in prose, what this listener does
// when the stream drops: it reconnects by itself, and mounting a SECOND one is
// not a spare tyre — the duplicate is refused with a pre-stream 409 and, once
// the refusal run crosses sseRefusalMin AND sseRefusalGrace, that listener
// self-terminates by killing its own tmux session, which is the agent's.
//
// WHY THIS TEST EXISTS (T-c5ea). The prose names the two bounds as "4 次" and
// "2 分鐘". They are a HAND COPY of the constants below, in a file no compiler
// reads, and the failure is silent in the direction that matters: an agent that
// believes a stale number mounts the second listener anyway. The incident that
// opened the ticket was the other half of the same gap — the seed said nothing
// at all about reconnection, so a contractor "fixed" a routine EOF by hanging a
// second listener.
//
// ⚠️ Go's test cache keys on this package's files, NOT on ../../seeds — a run
// after only the seed changed can be served from cache. CI passes -count=1;
// locally, do the same when this is the thing you are checking.
func TestClaudeBootSeedNamesTheRefusalBounds(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "seeds", "boot_sequence.md"))
	if err != nil {
		t.Fatalf("read the claude boot seed: %v", err)
	}
	seed := string(raw)

	// The claims themselves must be there. Without this the two number checks
	// below would pass happily over a seed that says nothing at all — which is
	// the state T-c5ea found. Each carries its OWN consequence: a shared message
	// would tell a reader the wrong thing about whichever claim actually broke.
	for _, c := range []struct{ marker, lost string }{
		{"自己重連", "an agent that does not know the listener reconnects reads a routine " +
			"EOF as a fault"},
		{"第二條", "…and the repair it reaches for — a second listener — is refused and " +
			"self-terminates its own tmux session"},
		{"replay", "a reconnect looks identical whether or not anything was missed, so " +
			"an agent that is not told there is no replay will read `connected` as " +
			"`nothing was lost` and never re-read"},
	} {
		if !strings.Contains(seed, c.marker) {
			t.Errorf("the claude boot seed no longer says %q — %s", c.marker, c.lost)
		}
	}

	if sseRefusalMin != 4 {
		t.Errorf("sseRefusalMin is %d but seeds/boot_sequence.md says 「連續 4 次以上」 — "+
			"update the seed in the SAME commit (and remember the live 啟動程序 "+
			"document is an owner-editable OVERLAY: editing it never touches this "+
			"seed, and fixing this seed never touches it either, so the correction "+
			"has to be made twice)", sseRefusalMin)
	}
	if sseRefusalGrace != 120*time.Second {
		t.Errorf("sseRefusalGrace is %v but seeds/boot_sequence.md says 「跨越 2 分鐘」 — "+
			"same two-copy rule as above", sseRefusalGrace)
	}
}
