package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The 啟動程序 seed tells the agent what this listener does when the stream
// drops, and in doing so it QUOTES TWO NUMBERS that live in listen.go: the
// refusal run and the window that ends in the listener killing its own tmux
// session. Those numbers are a HAND COPY, in a file no compiler reads.
//
// 🔴 WHAT THIS TEST IS FOR, and it is narrower than it looks: it is a LIE
// guard, not a coverage guard. It answers one question — if the constants move,
// does the prose still claim the old numbers? — because that failure is silent
// in the direction that matters: an agent that believes a stale number mounts
// the second listener anyway, and the refusal run kills the session it is
// running in.
//
// 🔴 WHAT IT IS EXPLICITLY NOT (owner, 2026-08-19): it does NOT assert that the
// seed says anything at all, and it must never grow back into that. Grepping
// prose for keywords is not coverage — whether a document is understood is not
// a thing a unit test can answer, and a test that forbids substrings mostly
// forbids REWRITING them, so the next person to improve that paragraph gets a
// red build for making it better. A false red is not the safe side; its exit is
// someone loosening the check.
//
// So: no sentence here, nothing fires. State a number that disagrees with the
// code, and this is what says so. That the document is worth having at all is a
// judgement, and it stays a human one.
//
// (The separate question "does the API hand back the boot document intact" is
// already answered, by the byte-identical read-back and reset tests in
// server/ocserverd — not by anything here.)
//
// ⚠️ Go's test cache keys on this package's files, NOT on ../../seeds — a run
// after only the seed changed can be served from cache. CI passes -count=1;
// locally, do the same when this is the thing you are checking.
func TestClaudeBootSeedDoesNotQuoteStaleRefusalBounds(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "seeds", "boot_sequence.md"))
	if err != nil {
		t.Fatalf("read the claude boot seed: %v", err)
	}
	seed := string(raw)

	// Each pair is (the phrasing the seed uses for this number, the number the
	// code actually holds). Absent phrasing = the seed makes no claim = nothing
	// to be wrong about.
	for _, c := range []struct {
		claim string
		true_ string
		what  string
	}{
		{"連續 4 次", fmt.Sprintf("連續 %d 次", sseRefusalMin), "sseRefusalMin"},
		{"跨越 2 分鐘", fmt.Sprintf("跨越 %d 分鐘", int(sseRefusalGrace/time.Minute)), "sseRefusalGrace"},
	} {
		if strings.Contains(seed, c.claim) && c.claim != c.true_ {
			t.Errorf("seeds/boot_sequence.md still says 「%s」 but %s now means 「%s」 — "+
				"fix the seed in the SAME commit, and remember the LIVE 啟動程序 document "+
				"is an owner-editable overlay: editing it never touches this seed, and "+
				"fixing this seed never touches it either, so the correction has to be "+
				"made twice", c.claim, c.what, c.true_)
		}
	}
}
