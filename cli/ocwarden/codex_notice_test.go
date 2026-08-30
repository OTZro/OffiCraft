package main

import "testing"

// ---------------------------------------------------------------------------
// THE SAME OWNER RULING, ON THE OTHER RUNTIME (2026-08-30):
//
//	「應該是在第一次斷線，跟連線回來的時候發訊息給 agent，中間的 retry 我們不需要
//	 降低頻率，但是不需要打攪 agent。」
//
// The two runtimes sat at OPPOSITE extremes of it and neither was what he asked
// for. A claude member reads ocagent's stdout directly, so EVERY transport line
// was an interruption (too loud). A codex member reads it through this sidecar,
// whose forwarding filter drops every line beginning `[ocagent] listen:` — so a
// codex member was told about its transport EXACTLY ONCE, at boot, and every
// disconnect and every reconnect after that was silent (too quiet).
//
// The middle — the thing he actually asked for — is: forward the two endpoint
// notices and the give-up line, and nothing else.
//
// 🔴 WHAT MUST SURVIVE THIS: the once-per-session post-boot wake (T-51b0). It is
// the only thing that continues a codex boot after SSE comes up, it fires on the
// same `connected` prefix these notices now ride, and when it is missing nothing
// errors and nothing reports it — the agent simply never starts, which looks
// exactly like an agent with nothing to do. The pre-existing tests in
// codex_session_test.go pin it; this file must not be allowed to weaken them.
// ---------------------------------------------------------------------------

const connectedLineFixture = "[ocagent] listen: connected — streaming http://127.0.0.1 " +
	"(⇒ online while held) [same station] [station abc123]"

func TestCodexForwardsOnlyTheTwoEndpointNoticesAndTheGiveUp(t *testing.T) {
	for line, want := range map[string]bool{
		// The endpoints the owner named, plus the give-up line that keeps
		// silence unambiguous. These MUST reach the model.
		"[ocagent] listen: disconnected — connect failed: unexpected status 502": true,
		"[ocagent] listen: giving up — context cancelled":                        true,
		connectedLineFixture: true,
		// Everything between the endpoints stays out of the transcript.
		"[ocagent] listen: connect failed: unexpected status 502": false,
		"[ocagent] listen: stream ended: EOF":                     false,
		"[ocagent] listen: connect refused: 409":                  false,
		// Negative control: ordinary events were always forwarded and still are,
		// so the assertions above are about these lines and not about all lines.
		"[ocagent] chat from owner (#CM-9F2A11, 1s ago): hello": true,
		"[ocagent] task T-1 updated · by owner":                 true,
	} {
		if got := actionableCodexListenerLine(line); got != want {
			t.Errorf("%q: forwarded=%v want %v", line, got, want)
		}
	}
}

// The boot connect must NOT arrive twice. It opens the once-only post-boot wake,
// and now that the connected line is also a forwardable notice, the naive
// composition would open two turns for one event.
func TestCodexBootConnectWakesOnceAndIsNotAlsoForwarded(t *testing.T) {
	var turns []string
	st := &codexListenerState{}
	feed := func(line string) {
		st.handleListenerLine(line, func() {}, func(text string) { turns = append(turns, text) })
	}

	feed(connectedLineFixture)
	if len(turns) != 1 || turns[0] != codexPostBootWake {
		t.Fatalf("the boot connect must open exactly the post-boot wake and nothing "+
			"else; turns=%q", turns)
	}

	// A RECONNECT is the owner's second endpoint. It must reach the agent — as
	// the notice, never as a second boot wake.
	feed(connectedLineFixture)
	if len(turns) != 2 {
		t.Fatalf("a reconnect must tell the agent it is back; turns=%q", turns)
	}
	if turns[1] == codexPostBootWake {
		t.Fatalf("a reconnect re-opened the BOOT wake; that boot is long over and the "+
			"turn interrupts real work. turns=%q", turns)
	}
	if turns[1] != connectedLineFixture {
		t.Fatalf("the reconnect notice must be the listener's own line; turns=%q", turns)
	}

	feed("[ocagent] listen: disconnected — connect failed: unexpected status 502")
	feed("[ocagent] listen: connect failed: unexpected status 502") // a mid-outage retry
	feed("[ocagent] listen: giving up — context cancelled")
	if len(turns) != 4 {
		t.Fatalf("one disconnect + one give-up must land and the mid-outage retry must "+
			"not; turns=%q", turns)
	}
}
