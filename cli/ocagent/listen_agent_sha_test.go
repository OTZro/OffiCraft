package main

// listen_agent_sha_test.go — the connection line must say which ocagent printed
// it, and must say nothing when it does not know.
//
// 🔴 WHY THE EMPTY CASE IS THE DEFAULT HERE AND THE STAMPED CASE HAS TO BE
// STAGED. buildSHA is filled by bin/build-bindist at link time, so it is EMPTY in
// every test binary — which means the pre-existing suite exercises only the
// unstamped path and cannot see this feature at all. The stamped assertions below
// set the variable themselves for that reason; without that, "the line names the
// agent" would be untested while looking covered.

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"
)

func withBuildSHA(t *testing.T, sha string) {
	t.Helper()
	prev := buildSHA
	buildSHA = sha
	t.Cleanup(func() { buildSHA = prev })
}

// TestConnectOnce_ConnectionLineNamesTheOcagentThatPrintedIt pins the whole
// contract in one printed line: the segment is present, it carries the linked-in
// sha, it comes AFTER the station's, and the prefix has not moved.
//
// The ordering assertion is not cosmetic. The two shas are different clocks — the
// station's changes when the station is deployed, this one only when the process
// restarts — and reading a transcript means telling them apart at a glance. It is
// also the direction of the fleet observation that motivated this: a listener
// whose [agent …] is four changeovers behind the [station …] beside it is exactly
// what nothing could see before.
func TestConnectOnce_ConnectionLineNamesTheOcagentThatPrintedIt(t *testing.T) {
	cfgTempDir = t.TempDir()
	srv := httptest.NewServer(stationHandler("9f3c1ab77e40", true))
	defer srv.Close()

	reported := stationGitSHA(t, srv)
	if reported == "" {
		t.Fatal("fixture broken: the station must self-report a build")
	}
	withBuildSHA(t, "cafe1234beef")

	out := &syncBuf{}
	l := newTestListener(srv, Config{Base: srv.URL, Token: "tok", ID: "kyle"}, out)
	if _, _, _, err := l.connectOnce(context.Background()); err != nil {
		t.Fatalf("connectOnce: %v", err)
	}
	line := connectedLine(t, out.String())

	// Literal, not " [agent " + buildSHA + "]": building the expectation out of
	// the same variable the code under test reads makes both sides move together,
	// and a segment renamed or emptied on both sides would stay green. This repo
	// has already shipped that shape once.
	if !strings.Contains(line, " [agent cafe1234beef]") {
		t.Fatalf("connection line must name the ocagent that printed it, got:\n%s", line)
	}
	if !strings.HasSuffix(line, " [agent cafe1234beef]") {
		t.Fatalf("the agent segment must come LAST of the segments this function "+
			"appends (the stampWriter adds its own [ts=…] outside this writer), got:\n%s", line)
	}
	iStation := strings.Index(line, " [station "+reported+"]")
	iAgent := strings.Index(line, " [agent cafe1234beef]")
	if iStation < 0 || iAgent < iStation {
		t.Fatalf("the peer's sha must be read before the speaker's — station at %d, "+
			"agent at %d:\n%s", iStation, iAgent, line)
	}
	// Same red line the station-sha test defends, re-asserted because this change
	// appends to the same call. Three prefix checks in
	// cli/ocwarden/codex_session.go read the head of this line
	// (actionableCodexListenerLine, codexListenerActions, handleListenerLine);
	// pushing these bytes rightward turns every reconnect into a codex turn, for
	// the whole fleet at once at a changeover, with nothing erroring anywhere.
	if !strings.HasPrefix(strings.TrimSpace(line), "[ocagent] listen: connected") {
		t.Fatalf("the not-actionable prefix must not move; got:\n%s", line)
	}
}

// TestConnectOnce_AnUnstampedOcagentSaysNothingAboutItself is the pair: a build
// with no stamp must emit the line byte-identical to what it was before the
// feature existed. An empty segment (" [agent ]") is worse than none — it looks
// like an answer, and the whole point of the line is telling a reader which
// binary is speaking. Fabricating anything (a runtime lookup of the sha on disk)
// would be worse still: it would report the version this process is NOT running.
func TestConnectOnce_AnUnstampedOcagentSaysNothingAboutItself(t *testing.T) {
	cfgTempDir = t.TempDir()
	srv := httptest.NewServer(stationHandler("9f3c1ab77e40", true))
	defer srv.Close()

	for _, sha := range []string{"", "   ", "\t"} {
		withBuildSHA(t, sha)
		out := &syncBuf{}
		l := newTestListener(srv, Config{Base: srv.URL, Token: "tok", ID: "kyle"}, out)
		if _, _, _, err := l.connectOnce(context.Background()); err != nil {
			t.Fatalf("connectOnce: %v", err)
		}
		line := connectedLine(t, out.String())
		if strings.Contains(line, "[agent") {
			t.Errorf("an unstamped build (%q) must not print the segment at all — "+
				"an empty or blank one reads as an answer; got:\n%s", sha, line)
		}
		if want := " [station 9f3c1ab77e40]"; !strings.HasSuffix(line, want) {
			t.Errorf("with no stamp the line must end exactly as it did before this "+
				"feature (%q); got:\n%s", want, line)
		}
	}
}

// TestConnectOnce_EveryReconnectNamesTheAgentAgain closes the hole review found:
// every assertion above drives connectOnce ONCE, so a segment emitted only on the
// first connect of a process satisfies all of them. Measured: gating it behind a
// package-level `var agentSaid bool` left the whole ocagent suite green and the
// shell guard green, while a real binary against a station that dropped the stream
// printed the segment on the first line and never again.
//
// 🔴 THAT IS THE MOTIVATING SCENARIO, NOT AN EDGE CASE. A station changeover
// reconnects the whole fleet at once — it is exactly when someone reads these
// lines to work out which agents are running which build, and exactly when a
// once-per-process segment has already been spent. The reconnect lines are the
// ones that matter most, so they are the ones pinned here.
func TestConnectOnce_EveryReconnectNamesTheAgentAgain(t *testing.T) {
	cfgTempDir = t.TempDir()
	srv := httptest.NewServer(stationHandler("9f3c1ab77e40", true))
	defer srv.Close()
	withBuildSHA(t, "cafe1234beef")

	for attempt := 1; attempt <= 3; attempt++ {
		out := &syncBuf{}
		l := newTestListener(srv, Config{Base: srv.URL, Token: "tok", ID: "kyle"}, out)
		if _, _, _, err := l.connectOnce(context.Background()); err != nil {
			t.Fatalf("connectOnce #%d: %v", attempt, err)
		}
		line := connectedLine(t, out.String())
		if !strings.HasSuffix(line, " [agent cafe1234beef]") {
			t.Fatalf("connect #%d dropped the agent segment — a listener that names "+
				"itself only once is silent for every reconnect after it, which is "+
				"precisely a station changeover:\n%s", attempt, line)
		}
	}
}
