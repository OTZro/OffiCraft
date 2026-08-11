// wording_cap_mirror_test.go — the two things maxWordingEntriesPerLang has to
// satisfy, each with a failure message that points at the fix.
//
// The reason this file exists is a diagnosis cost, not a correctness one. When
// the cap sat at or below the whitelist length, what went red was the frontend's
// ThemeSettings.test.tsx forging a pack of "every whitelisted key + 1", and it
// failed with `expected 1 to be 2` — a message with nothing in it pointing at a
// cap, in a file that is about theme identity. Both assertions below therefore
// spell out the actual numbers and the actual edit.
package main

import (
	"os"
	"regexp"
	"strconv"
	"testing"
)

const themeBundleCorePath = "../../frontend/src/lib/themeBundleCore.ts"

var tsWordingCapRe = regexp.MustCompile(`MAX_WORDING_ENTRIES_PER_LANG\s*=\s*(\d+)`)

// tsWordingCap reads the TypeScript twin. Unreadable/unparseable is FATAL, never
// a skip: a mirror test that goes green because it could not find the other side
// is exactly the silence this file exists to remove.
func tsWordingCap(t *testing.T) int {
	t.Helper()
	b, err := os.ReadFile(themeBundleCorePath)
	if err != nil {
		t.Fatalf("read %s: %v — that file holds the TypeScript twin of maxWordingEntriesPerLang, and nothing else compares the two", themeBundleCorePath, err)
	}
	m := tsWordingCapRe.FindSubmatch(b)
	if m == nil {
		t.Fatalf("%s no longer declares `MAX_WORDING_ENTRIES_PER_LANG = <n>` — either it was renamed or it stopped being a literal, and either way this mirror is no longer comparing anything", themeBundleCorePath)
	}
	n, err := strconv.Atoi(string(m[1]))
	if err != nil {
		t.Fatalf("%s: MAX_WORDING_ENTRIES_PER_LANG is not an integer: %v", themeBundleCorePath, err)
	}
	return n
}

func TestMaxWordingEntriesPerLang(t *testing.T) {
	t.Run("matches its TypeScript twin", func(t *testing.T) {
		ts := tsWordingCap(t)
		if maxWordingEntriesPerLang != ts {
			t.Fatalf(`THE TWO WORDING CAPS HAVE DRIFTED.

  server/ocserverd/wording_bundle.go   maxWordingEntriesPerLang     = %d
  frontend/src/lib/themeBundleCore.ts  MAX_WORDING_ENTRIES_PER_LANG = %d

They are one rule with two homes: the client refuses an oversized pack offline
and the server refuses the same pack with a 422. Whichever of the two you just
moved, move the other one to the same number in the same commit.`,
				maxWordingEntriesPerLang, ts)
		}
	})

	t.Run("leaves room above the whitelist", func(t *testing.T) {
		// validateWording measures the RAW submitted map before pruning unknown
		// codes, so a pack that re-words every whitelisted key submits exactly
		// len(messageKeys) entries. A cap at or below that number makes a
		// legitimate, fully-worded pack fail its own validation.
		const wantSpare = 50
		if len(messageKeys) == 0 {
			t.Fatal("messageKeys is empty — gen:msgkeys did not run, so this assertion would pass for the wrong reason")
		}
		if maxWordingEntriesPerLang < len(messageKeys)+wantSpare {
			t.Fatalf(`THE WORDING CAP HAS RUN OUT OF ROOM ABOVE THE MESSAGE-KEY WHITELIST.

  message-key whitelist  %d keys   (frontend/src/i18n/locales/en.ts, via gen:msgkeys)
  wording entry cap      %d        (the most raw entries one language may submit)
  headroom               %d        (want at least %d)

The cap counts RAW submitted entries BEFORE unknown codes are pruned, so a
legitimate pack that re-words EVERY message key submits exactly %d entries. Once
the cap stops clearing the whitelist, that pack is refused by its own validator
and theme wording is broken for everyone — and the test that goes red first is
in a file about theme names, saying nothing about a cap.

THE FIX IS TO RAISE BOTH TWINS TOGETHER, in one commit, to the same number:

  server/ocserverd/wording_bundle.go   maxWordingEntriesPerLang
  frontend/src/lib/themeBundleCore.ts  MAX_WORDING_ENTRIES_PER_LANG

Raising only one of them is red too, in the sibling subtest above.`,
				len(messageKeys), maxWordingEntriesPerLang,
				maxWordingEntriesPerLang-len(messageKeys), wantSpare, len(messageKeys))
		}
	})
}
