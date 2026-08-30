package main

import (
	"os"
	"strings"
	"testing"
)

// The three transport notices are a contract between TWO Go modules that cannot
// import each other: this one prints the lines, and cli/ocwarden's codex sidecar
// matches them with HasPrefix from column 0 before deciding whether a codex
// member ever hears about its own transport.
//
// 🔴 WHY A FILE-READING TEST AND NOT A SHARED CONSTANT. There is no shared
// package — `ocagent` and `ocwarden` are separate modules with no replace
// directive — so "spell it once" is not available, and the contract is
// physically two copies of the same bytes. Two copies with nothing tying them
// together is exactly the failure this test exists for: independent review
// changed ONE side and both suites stayed green while every codex member went
// silent about its transport for the rest of its session. Nothing anywhere
// would have reported it.
//
// So this asks the only question that spans the gap: do the heads this module
// PRINTS still exist, verbatim, in the file that CONSUMES them? It is a weaker
// statement than a compiler would make and it is the strongest one available
// here — and it is far stronger than the nothing that was there before.
//
// A rename on either side reddens this. A head that moves rightward is caught
// by the HasPrefix assertions in listen_notice_test.go, which read the bytes
// this listener really wrote.
func TestListenNoticePrefixesMatchTheSidecarConsumer(t *testing.T) {
	const consumer = "../ocwarden/codex_session.go"

	source, err := os.ReadFile(consumer)
	if err != nil {
		t.Fatalf("cannot read the sidecar that consumes these prefixes (%s): %v — "+
			"if that file moved, this contract check moved with it and must be "+
			"repointed, not deleted", consumer, err)
	}
	text := string(source)

	for _, head := range []string{noticeDisconnected, noticeConnected, noticeGivingUp} {
		want := agentLinePrefix + head
		if !strings.Contains(text, `"`+want+`"`) {
			t.Errorf("%s no longer contains the literal %q.\n"+
				"This module prints that head; the codex sidecar matches it with "+
				"HasPrefix from column 0. Half of the contract has just been "+
				"changed: codex members will stop being told about this transport "+
				"event, silently, for the whole life of every session.", consumer, want)
		}
	}
}
