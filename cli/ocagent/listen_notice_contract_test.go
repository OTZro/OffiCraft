package main

import (
	"os"
	"regexp"
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

	// ── THE FOURTH COPY ──────────────────────────────────────────────────────
	// The three heads above are the EXCEPTIONS. The rule they are carved out of
	// is the sidecar's blanket filter, whose head is the common prefix of every
	// transport line this binary prints — and that copy sat outside this list
	// until T-4, held up only indirectly by one behavioural case in ocwarden.
	//
	// Two claims, and both have to hold or the filter is deciding on bytes the
	// producer never emits:
	//   1. it really is the head of what THIS module prints, and
	//   2. the consumer still declares it, verbatim, as its filter head.
	//
	// Move it rightward on the sidecar side and the filter recognises no
	// transport line at all: every retry diagnostic starts becoming a turn on
	// the model, which is precisely the mid-outage noise the owner's ruling
	// (2026-08-30) exists to swallow.
	const transportHead = agentLinePrefix + "listen:"
	for _, head := range []string{noticeDisconnected, noticeConnected, noticeGivingUp} {
		if !strings.HasPrefix(agentLinePrefix+head, transportHead) {
			t.Fatalf("this module now prints %q, which does not start with the "+
				"blanket transport head %q that the sidecar filters on — the "+
				"exceptions and the rule have come apart on THIS side",
				agentLinePrefix+head, transportHead)
		}
	}
	// Matched as a DECLARATION, not as a bare substring: this head is quoted in
	// several prose comments in that same file, so `Contains` alone would keep
	// answering yes long after the code stopped using it.
	decl := regexp.MustCompile(`noticeTransportHead\s*=\s*` +
		regexp.QuoteMeta(`"`+transportHead+`"`))
	if !decl.MatchString(text) {
		t.Errorf("%s no longer declares noticeTransportHead = %q.\n"+
			"That constant is the head of the sidecar's blanket transport filter "+
			"and it is the fourth copy of these bytes across a module boundary "+
			"neither side can import. If it moved, the filter stops recognising "+
			"the lines this module actually prints and every retry diagnostic "+
			"becomes a turn on the model — silently, for the whole life of every "+
			"codex session.", consumer, transportHead)
	}
}
