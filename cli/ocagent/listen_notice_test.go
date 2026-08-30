package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// THE DISCONNECT-NOTICE POLICY (owner, 2026-08-30):
//
//	「應該是在第一次斷線，跟連線回來的時候發訊息給 agent，中間的 retry 我們不需要
//	 降低頻率，但是不需要打攪 agent。」
//
// Three separate claims, and they are easy to conflate:
//  1. the RETRY CADENCE does not move — this file must never touch
//     listenBackoffStart/listenBackoffCap, and the loop must keep re-dialling
//     at exactly the rhythm it had. Silence is about the TRANSCRIPT, not about
//     trying less hard.
//  2. exactly two notices per outage: one when it starts, one when it ends.
//  3. and — the correction the owner approved on top of it — the moment the
//     retry loop actually STOPS must also print, because otherwise 「還在重試」
//     and 「已經放棄」 are the same silence, and the agent cannot tell which of
//     the two it is sitting in. SILENCE MAY MEAN ONLY ONE THING.
//
// Measured before the change, on one station changeover, one member got THREE
// lines for one event: `stream ended`, `connect failed: unexpected status 502`,
// `connected`. The middle one is the one the owner said not to send.
// ---------------------------------------------------------------------------

// noticeLines returns the transcript lines that are disconnect-policy notices,
// i.e. the ones that reach the agent as an interruption.
//
// 🔴 IT MATCHES FROM COLUMN 0, AND THAT IS THE WHOLE POINT. This helper used to
// ask strings.Contains, and independent review walked a one-character change
// past it: `"listen: disconnected — "` → `"net listen: disconnected — "`, both
// Go modules green, every codex member silenced for good. The codex sidecar
// does not read these lines the way Contains does — it TrimSpaces and then
// HasPrefix from the first column (cli/ocwarden/codex_session.go), so anything
// INSERTED ahead of the head breaks it while leaving a Contains test perfectly
// happy. Asking the same question the consumer asks is what closes that gap;
// the needle is therefore a whole prefix, agentLinePrefix included.
func noticeLines(transcript, prefix string) []string {
	var found []string
	for _, line := range strings.Split(transcript, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), prefix) {
			found = append(found, line)
		}
	}
	return found
}

// The three notice prefixes AS THE SIDECAR SEES THEM: from column 0, with the
// binary's own line prefix on the front. Tests below use these and never a bare
// substring, so a head that moves rightward reddens them.
var (
	disconnectedPrefix = agentLinePrefix + noticeDisconnected
	connectedPrefix    = agentLinePrefix + noticeConnected
	givingUpPrefix     = agentLinePrefix + noticeGivingUp
)

// deadStation always refuses the SSE with a 5xx (a station mid-changeover), so
// the run loop stays in one uninterrupted outage for as many attempts as the
// test lets it make.
func deadStation(attempts *atomic.Int32) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, eventsPath) {
			_, _ = w.Write([]byte("[]"))
			return
		}
		attempts.Add(1)
		w.WriteHeader(http.StatusBadGateway)
	}))
}

// ① ONE notice for the whole outage, no matter how many times it re-dials.
func TestListener_OnlyTheFirstFailureOfAnOutageNotifiesTheAgent(t *testing.T) {
	cfgTempDir = t.TempDir()
	var attempts atomic.Int32
	srv := deadStation(&attempts)
	defer srv.Close()

	cfg := Config{Base: srv.URL, Token: "tok", ID: "kyle"}
	out := &syncBuf{}
	l := newTestListener(srv, cfg, out)
	l.winddown = newWindDownHook(srv.Client(), cfg, out)
	l.recycle = newRecycleHook(srv.Client(), cfg, out)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var sleeps atomic.Int32
	l.sleep = func(time.Duration) {
		if sleeps.Add(1) >= 5 {
			cancel()
		}
	}
	done := make(chan int, 1)
	go func() { done <- l.run(ctx) }()
	<-done

	// The cadence is UNTOUCHED: it really did keep re-dialling.
	if got := attempts.Load(); got < 5 {
		t.Fatalf("the retry cadence must not slow down: only %d dials in %d sleeps — "+
			"the fix is meant to quieten the transcript, not to try less hard",
			got, sleeps.Load())
	}

	notices := noticeLines(out.String(), disconnectedPrefix)
	if len(notices) != 1 {
		t.Fatalf("one outage must interrupt the agent exactly ONCE, got %d:\n%s",
			len(notices), out.String())
	}
	// NEGATIVE CONTROL on the other side: the retries themselves must be silent.
	// Pinned by the literal bytes the old code printed, because that is what was
	// actually landing in the transcript.
	// (the needles carry agentLinePrefix now that noticeLines matches from
	// column 0 — without it this control would match nothing and assert nothing)
	for _, banned := range []string{
		agentLinePrefix + "listen: connect failed",
		agentLinePrefix + "listen: stream ended",
	} {
		if extra := noticeLines(out.String(), banned); len(extra) != 0 {
			t.Errorf("a mid-outage retry interrupted the agent (%d× %q):\n%s",
				len(extra), banned, out.String())
		}
	}
}

// ② SILENCE MEANS ONLY ONE THING: when the loop stops retrying, say so.
// Without this line the transcript reads `斷線 → 沉默`, and an agent that is
// being retried for and an agent that has been abandoned look identical.
func TestListener_GivingUpIsAnnouncedSoSilenceIsNeverAmbiguous(t *testing.T) {
	cfgTempDir = t.TempDir()
	var attempts atomic.Int32
	srv := deadStation(&attempts)
	defer srv.Close()

	cfg := Config{Base: srv.URL, Token: "tok", ID: "kyle"}
	out := &syncBuf{}
	l := newTestListener(srv, cfg, out)
	l.winddown = newWindDownHook(srv.Client(), cfg, out)
	l.recycle = newRecycleHook(srv.Client(), cfg, out)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	l.sleep = func(time.Duration) { cancel() } // one retry, then the loop is torn down
	done := make(chan int, 1)
	go func() { done <- l.run(ctx) }()
	<-done

	if got := len(noticeLines(out.String(), disconnectedPrefix)); got != 1 {
		t.Fatalf("the outage itself must still be announced once, got %d:\n%s",
			got, out.String())
	}
	if got := len(noticeLines(out.String(), givingUpPrefix)); got != 1 {
		t.Fatalf("the retry loop stopped and said nothing — 「還在重試」 and 「已經放棄」 "+
			"are now the same silence; got %d give-up lines:\n%s", got, out.String())
	}
}

// ③ NEGATIVE CONTROL for the whole change: an outage that RECOVERS must still
// produce both endpoint notices. The failure mode of a quietening change is
// quietening everything.
func TestListener_DisconnectAndRecoveryBothStillNotify(t *testing.T) {
	cfgTempDir = t.TempDir()
	var dials atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, eventsPath) {
			_, _ = w.Write([]byte("[]"))
			return
		}
		// dial 1 dies mid-flight (the outage), dials 2+ succeed (the recovery).
		if dials.Add(1) == 1 {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		w.Header().Set(wireStationSHAHeader, "abc123")
		w.WriteHeader(http.StatusOK)
		if fl, ok := w.(http.Flusher); ok {
			fl.Flush()
		}
		_, _ = w.Write([]byte(": connected\n\n"))
		if fl, ok := w.(http.Flusher); ok {
			fl.Flush()
		}
		<-r.Context().Done() // hold the recovered stream open: ONE outage, not two
	}))
	defer srv.Close()

	cfg := Config{Base: srv.URL, Token: "tok", ID: "kyle"}
	out := &syncBuf{}
	l := newTestListener(srv, cfg, out)
	l.winddown = newWindDownHook(srv.Client(), cfg, out)
	l.recycle = newRecycleHook(srv.Client(), cfg, out)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan int, 1)
	go func() { done <- l.run(ctx) }()
	waitForCond(t, func() bool {
		return len(noticeLines(out.String(), connectedPrefix)) > 0
	}, "the listener to come back up after the outage")
	cancel()
	<-done

	if got := len(noticeLines(out.String(), disconnectedPrefix)); got != 1 {
		t.Errorf("the first disconnect must reach the agent exactly once, got %d:\n%s",
			got, out.String())
	}
	if got := len(noticeLines(out.String(), connectedPrefix)); got != 1 {
		t.Errorf("coming back must reach the agent exactly once, got %d:\n%s",
			got, out.String())
	}
	// The outage is over, so the loop stopping afterwards is NOT ambiguous and
	// must not add a give-up line to a healthy shutdown.
	if got := len(noticeLines(out.String(), givingUpPrefix)); got != 0 {
		t.Errorf("a shutdown while CONNECTED has nothing ambiguous to resolve and "+
			"must stay quiet, got %d:\n%s", got, out.String())
	}
}

// ④ The reconnect line ANSWERS 「是不是換了一台」 instead of leaving the reader
// to diff two shas by eye. The material was already on the line (T-5b83 put the
// station sha there); what was missing is the verdict.
func TestConnectOnce_ReconnectSaysWhetherTheStationChanged(t *testing.T) {
	cfgTempDir = t.TempDir()
	var sha atomic.Value
	sha.Store("aaaa1111")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, eventsPath) {
			_, _ = w.Write([]byte("[]"))
			return
		}
		w.Header().Set(wireStationSHAHeader, sha.Load().(string))
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(": connected\n\n"))
	}))
	defer srv.Close()

	l := newTestListener(srv, Config{Base: srv.URL, Token: "tok", ID: "kyle"}, nil)
	connect := func() string {
		out := &syncBuf{}
		l.out = out
		if _, _, _, err := l.connectOnce(context.Background()); err != nil {
			t.Fatalf("connectOnce: %v", err)
		}
		return connectedLine(t, out.String())
	}

	// FIRST connect of the process: there is no previous station, so there is
	// nothing truthful to say. It must not guess, and it must not claim "same".
	first := connect()
	if strings.Contains(first, "same station") || strings.Contains(first, "new station") {
		t.Fatalf("the first connect has nothing to compare against and must claim "+
			"neither; got:\n%s", first)
	}

	same := connect()
	if !strings.Contains(same, "[same station]") {
		t.Fatalf("a reconnect to the SAME build must say so outright — the reader "+
			"must not have to diff two shas by eye; got:\n%s", same)
	}

	sha.Store("bbbb2222")
	changed := connect()
	if !strings.Contains(changed, "[new station — was aaaa1111]") {
		t.Fatalf("a reconnect onto a DIFFERENT build is a changeover and must say "+
			"so, naming what it left; got:\n%s", changed)
	}
	if strings.Contains(changed, "[same station]") {
		t.Fatalf("a changeover must not also claim the station is unchanged; got:\n%s", changed)
	}
}

// ⑤ THE GIVE-UP LINE MUST BE PRINTED BEFORE THE KILL, NOT AFTER.
// The fail-closed refusal path self-terminates: it kills its OWN tmux session,
// and suicide.go says outright that a successful kill SIGHUPs this process and
// never returns. A give-up line printed after that call is dead code exactly
// where it is needed, and seeds/boot_sequence.md promises the whole fleet that
// the absence of that line means the listener is still retrying — so on this
// path the fleet-wide promise was false.
//
// This test encodes "the kill never returns" the only way a test can: it looks
// at the transcript AT THE MOMENT selfTerminate fires. Whatever is not there
// yet is, on a real host, never printed at all.
func TestListener_FailClosedSaysItGaveUpBeforeItKillsItself(t *testing.T) {
	cfgTempDir = t.TempDir()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, eventsPath) {
			_, _ = w.Write([]byte("[]"))
			return
		}
		w.WriteHeader(409) // the stop-gate refusal: authoritative, not bad luck
		_, _ = w.Write([]byte(`{"error":{"code":"conflict","message":"stop in effect"}}`))
	}))
	defer srv.Close()

	cfg := Config{Base: srv.URL, Token: "tok", ID: "kyle"}
	out := &syncBuf{}
	l := newTestListener(srv, cfg, out)
	l.winddown = newWindDownHook(srv.Client(), cfg, out)
	l.recycle = newRecycleHook(srv.Client(), cfg, out)
	l.refusalGraceSpan = 0 // count bound only — no wall-clock wait in tests

	var atKill string
	var kills int
	l.selfTerminate = func() {
		kills++
		atKill = out.String() // everything the agent will EVER see on this path
	}

	done := make(chan int, 1)
	go func() { done <- l.run(context.Background()) }()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("the listener never reached its fail-closed exit")
	}

	if kills != 1 {
		t.Fatalf("the fail-closed path must self-terminate exactly once, got %d", kills)
	}
	if got := len(noticeLines(atKill, givingUpPrefix)); got != 1 {
		t.Fatalf("the give-up line was not in the transcript when the process was "+
			"killed, so on a real host it is never printed at all: 「還在重試」 and "+
			"「已經放棄」 stay the same silence on the one path that ends the "+
			"listener for good. got %d give-up lines at kill time:\n%s", got, atKill)
	}
	if got := len(noticeLines(atKill, disconnectedPrefix)); got != 1 {
		t.Errorf("the outage that led here must still have been announced once, "+
			"got %d:\n%s", got, atKill)
	}
}
