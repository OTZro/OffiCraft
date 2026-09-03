package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// T-48: the listener files the read receipt itself, because GET /api/chat no
// longer does it as a side effect. Three things must hold: nothing is marked
// before the line is printed, one batch marks EACH sender to its own
// watermark, and both drain entrances (cold-start backfill, live delta after a
// reconnect) file receipts.
// ---------------------------------------------------------------------------

type markCall struct {
	Peer       string  `json:"peer"`
	LastReadTS float64 `json:"last_read_ts"`
}

// markReadServer serves a swappable /api/chat list and RECORDS every
// POST /api/chat/mark-read body. `status` is what the mark-read route answers.
type markReadServer struct {
	*httptest.Server
	mu    sync.Mutex
	list  string
	calls []markCall
	conns int32 // /api/events dials, read atomically
	// bodies are the mark-read request bodies EXACTLY as they came off the
	// wire. calls is the decoded convenience view; the wire test needs the raw
	// text, because an undeclared key is invisible once it has been decoded
	// into markCall's fixed fields.
	bodies []string
	status int
	// beforeMark, if set, runs on the mark-read route before the call is
	// recorded — the hook the "print first" guardrail uses to snapshot what the
	// session had already been told at the moment the receipt was filed.
	beforeMark func()
}

func newMarkReadServer(t *testing.T, list string) *markReadServer {
	t.Helper()
	m := &markReadServer{list: list, status: 200}
	m.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == markReadPath {
			m.mu.Lock()
			hook := m.beforeMark
			st := m.status
			m.mu.Unlock()
			if hook != nil {
				hook()
			}
			var c markCall
			raw, _ := io.ReadAll(r.Body)
			if err := json.Unmarshal(raw, &c); err != nil {
				t.Errorf("mark-read body is not JSON: %v (%s)", err, raw)
			}
			m.mu.Lock()
			m.calls = append(m.calls, c)
			m.bodies = append(m.bodies, string(raw))
			m.mu.Unlock()
			w.WriteHeader(st)
			_, _ = w.Write([]byte(`{"reader_id":"kyle","peer_id":"` + c.Peer + `","last_read_ts":0}`))
			return
		}
		if strings.HasPrefix(r.URL.Path, "/api/chat") {
			m.mu.Lock()
			list := m.list
			m.mu.Unlock()
			w.WriteHeader(200)
			_, _ = w.Write([]byte(chatBody(list)))
			return
		}
		if strings.HasPrefix(r.URL.Path, eventsPath) {
			atomic.AddInt32(&m.conns, 1)
			w.Header().Set("Content-Type", "text/event-stream")
			if fl, ok := w.(http.Flusher); ok {
				fl.Flush()
			}
			return
		}
		w.WriteHeader(404)
	}))
	t.Cleanup(m.Server.Close)
	return m
}

func (m *markReadServer) dials() int32 { return atomic.LoadInt32(&m.conns) }

func (m *markReadServer) setList(s string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.list = s
}

func (m *markReadServer) snapshot() []markCall {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := append([]markCall(nil), m.calls...)
	return out
}

func (m *markReadServer) rawBodies() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]string(nil), m.bodies...)
}

// tsMsg builds one wire message with an explicit ts.
func tsMsg(id, from, to string, ts float64) string {
	return fmt.Sprintf(`{"id":%q,"from":%q,"to":%q,"body":"body-%s","ts":%g}`, id, from, to, id, ts)
}

func markCfg(base, home string) Config {
	return Config{Base: base, Token: "t", ID: "kyle", Home: home}
}

// ---------------------------------------------------------------------------
// GUARDRAIL ① — the receipt trails the print, never leads it.
// ---------------------------------------------------------------------------

// A drain that prints NOTHING files NOTHING: the silent first-run baseline is
// exactly the case that produced the bug (a listener merely coming up lit the
// ✓ on messages nobody had seen).
func TestDrainChat_SilentBaseline_FilesNoReadReceipt(t *testing.T) {
	now := float64(time.Now().Unix())
	srv := newMarkReadServer(t, "["+tsMsg("m1", "boss", "kyle", now-30)+"]")
	seen := loadChatSeen("")
	var out bytes.Buffer

	drainChat(srv.Client(), markCfg(srv.URL, t.TempDir()), seen, &out, true, nil)

	if out.Len() != 0 {
		t.Fatalf("precondition: a silent baseline must print nothing, got %q", out.String())
	}
	if calls := srv.snapshot(); len(calls) != 0 {
		t.Fatalf("a drain that printed nothing filed %d read receipt(s): %+v — "+
			"the ✓ would then mean 'a listener connected', not 'someone read it'", len(calls), calls)
	}
}

// A fetch fault prints nothing and must likewise leave no receipt behind.
func TestDrainChat_FetchFault_FilesNoReadReceipt(t *testing.T) {
	var marks int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == markReadPath {
			marks++
		}
		w.WriteHeader(500)
	}))
	defer srv.Close()
	var out bytes.Buffer
	drainChat(srv.Client(), markCfg(srv.URL, t.TempDir()), loadChatSeen(""), &out, false, nil)
	if marks != 0 {
		t.Fatalf("a failed chat refetch filed %d read receipt(s), want 0", marks)
	}
}

// ORDER, not just presence: at the moment the receipt is filed the line it
// claims must ALREADY be on the session's stream. A process killed between the
// fetch and the print must leave no receipt, and the only way to pin that from
// the outside is to look at what had been printed when the POST landed.
func TestDrainChat_ReadReceiptIsFiledOnlyAfterTheLineIsPrinted(t *testing.T) {
	now := float64(time.Now().Unix())
	srv := newMarkReadServer(t, "["+tsMsg("m1", "boss", "kyle", now-30)+"]")
	out := &syncBuf{}
	var printedWhenMarked string
	srv.mu.Lock()
	srv.beforeMark = func() { printedWhenMarked = out.String() }
	srv.mu.Unlock()

	drainChat(srv.Client(), markCfg(srv.URL, t.TempDir()), loadChatSeen(""), out, false, nil)

	if len(srv.snapshot()) != 1 {
		t.Fatalf("precondition: want exactly one receipt, got %+v", srv.snapshot())
	}
	if !strings.Contains(printedWhenMarked, "chat from boss (#m1") {
		t.Fatalf("the read receipt was filed while the session had only seen %q — "+
			"the line must be printed BEFORE its receipt is filed", printedWhenMarked)
	}
}

// ---------------------------------------------------------------------------
// GUARDRAIL ② — one batch, several senders, each to its OWN watermark.
// ---------------------------------------------------------------------------

func TestDrainChat_MultipleSenders_EachMarkedToItsOwnWatermark(t *testing.T) {
	now := float64(time.Now().Unix())
	list := "[" + strings.Join([]string{
		tsMsg("a1", "alice", "kyle", now-90),
		tsMsg("b1", "bob", "kyle", now-80),
		tsMsg("a2", "alice", "kyle", now-70), // alice's newest
		tsMsg("c1", "carol", "other", now-5), // not for me: never printed, never marked
	}, ",") + "]"
	srv := newMarkReadServer(t, list)
	var out bytes.Buffer

	drainChat(srv.Client(), markCfg(srv.URL, t.TempDir()), loadChatSeen(""), &out, false, nil)

	calls := srv.snapshot()
	sort.Slice(calls, func(i, j int) bool { return calls[i].Peer < calls[j].Peer })
	want := []markCall{
		{Peer: "alice", LastReadTS: now - 70},
		{Peer: "bob", LastReadTS: now - 80},
	}
	if len(calls) != len(want) {
		t.Fatalf("filed %d receipts %+v, want exactly %+v — one per sender in the batch",
			len(calls), calls, want)
	}
	for i := range want {
		if calls[i] != want[i] {
			t.Fatalf("receipt #%d = %+v want %+v — bob must not be advanced to alice's ts "+
				"(nor alice pinned to her older message); full set %+v", i, calls[i], want[i], calls)
		}
	}
}

// 🔴 THE CAP CORNER, PINNED BECAUSE THE COMMENT ABOVE reportChatRead USED TO GET
// IT WRONG (found by the T-48 independent review).
//
// chatBacklogPrintCap drops the OLDEST lines and prints the newest. The receipt
// is a WATERMARK — "everything at or below this ts is read" — so for a sender
// who still has a surviving printed line, THE DROPPED OLDER ONES ARE SWEPT IN
// TOO. They are marked read although their bodies never reached the session.
//
// That is a genuine hole in this ticket's premise ("printed, therefore read"),
// and it is pinned rather than quietly fixed because the alternative — reporting
// the OLDEST printed line's ts — would leave the newest printed lines unread and
// re-print them forever. The user is told: the drain prints a 略過 N 則 line.
//
// The one case that really files nothing is a sender ALL of whose lines were
// dropped: no surviving line, no entry, no receipt.
func TestDrainChat_BacklogCap_SweepsDroppedOlderLinesOfASurvivingSender(t *testing.T) {
	now := float64(time.Now().Unix())
	msgs := make([]string, 0, chatBacklogPrintCap+6)
	// alice speaks only at the very start ⇒ every one of her lines is dropped.
	msgs = append(msgs, tsMsg("a-old", "alice", "kyle", now-500))
	// bob's oldest line is dropped too, but he also has the newest line of all.
	msgs = append(msgs, tsMsg("b-old", "bob", "kyle", now-499))
	for i := 0; i < chatBacklogPrintCap; i++ {
		msgs = append(msgs, tsMsg(fmt.Sprintf("b-%d", i), "bob", "kyle", now-float64(100-i)))
	}
	srv := newMarkReadServer(t, "["+strings.Join(msgs, ",")+"]")
	var out bytes.Buffer

	drainChat(srv.Client(), markCfg(srv.URL, t.TempDir()), loadChatSeen(""), &out, false, nil)

	if !strings.Contains(out.String(), "略過") {
		t.Fatalf("a drain over the cap must announce what it skipped; got:\n%s", out.String())
	}
	calls := srv.snapshot()
	if len(calls) != 1 || calls[0].Peer != "bob" {
		t.Fatalf("receipts = %+v, want exactly one for bob — alice had every line "+
			"dropped, so she has no surviving line and must get no receipt at all", calls)
	}
	// The pin: bob's watermark is his NEWEST printed line, which is strictly
	// above his dropped one ⇒ the dropped line is covered by it.
	wantTS := now - float64(100-(chatBacklogPrintCap-1))
	if calls[0].LastReadTS != wantTS {
		t.Fatalf("bob's watermark = %v, want %v (his newest printed line)", calls[0].LastReadTS, wantTS)
	}
	if calls[0].LastReadTS <= now-499 {
		t.Fatalf("watermark %v does not cover bob's dropped line at %v — if this ever "+
			"becomes true the comment above reportChatRead must change with it",
			calls[0].LastReadTS, now-499)
	}
}

// A sender whose message carries no usable ts has no watermark to report, so it
// is skipped rather than reported as 0 (which would be a silent no-op anyway).
func TestDrainChat_MessageWithoutTs_FilesNoReceipt(t *testing.T) {
	srv := newMarkReadServer(t, `[{"id":"m1","from":"boss","to":"kyle","body":"hi"}]`)
	var out bytes.Buffer
	drainChat(srv.Client(), markCfg(srv.URL, t.TempDir()), loadChatSeen(""), &out, false, nil)
	if !strings.Contains(out.String(), "#m1") {
		t.Fatalf("precondition: the line must still print, got %q", out.String())
	}
	if calls := srv.snapshot(); len(calls) != 0 {
		t.Fatalf("a ts-less message filed %+v, want no receipt", calls)
	}
}

// ---------------------------------------------------------------------------
// GUARDRAIL ③ — both entrances into a printing drain file receipts.
// ---------------------------------------------------------------------------

// COLD START through run(): a primed cursor means the first connect drain — the one that
// sits before the connect loop — BACKFILLS what arrived while nobody was
// listening. Those lines reach the session, so that path must mark too.
func TestListenerRun_ColdStartBackfill_FilesReadReceipts(t *testing.T) {
	home := t.TempDir()
	now := float64(time.Now().Unix())
	srv := newMarkReadServer(t, "["+strings.Join([]string{
		tsMsg("m1", "boss", "kyle", now-300),
		tsMsg("m2", "alice", "kyle", now-100),
	}, ",")+"]")
	cfg := markCfg(srv.URL, home)

	// A previous process baselined on m1 only; m1+m2 arrived while it was down.
	seedPath := chatSeenPath(cfg)
	if err := os.MkdirAll(filepath.Dir(seedPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(seedPath, []byte(`["m1"]`), 0o644); err != nil {
		t.Fatal(err)
	}

	out := &syncBuf{}
	l := newTestListener(srv.Server, cfg, out)
	l.cursorPath = filepath.Join(home, "cursor")
	l.seen = loadChatSeen(seedPath)
	l.replySeen = loadReplyCardSeen(filepath.Join(home, "replycards-seen"))
	if !l.seen.primed {
		t.Fatal("precondition: a persisted baseline must load PRIMED")
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan int, 1)
	go func() { done <- l.run(ctx) }()
	waitForCond(t, func() bool { return strings.Contains(out.String(), "chat from alice (#m2") },
		"the cold-start drain backfilled m2")
	cancel()
	<-done

	if strings.Contains(out.String(), "#m1") {
		t.Fatalf("precondition: m1 was already baselined and must not re-print; out = %q", out.String())
	}
	calls := srv.snapshot()
	if len(calls) != 1 || calls[0].Peer != "alice" || calls[0].LastReadTS != now-100 {
		t.Fatalf("the cold-start drain filed %+v, want exactly one {alice, %g} — "+
			"a backfilled line the session actually read must light the ✓", calls, now-100)
	}
}

// RECONNECT: after the cold-start drain, a live `chat` delta drives another drain
// through dispatch. That path prints too, so it must mark too — and it must
// mark the NEW sender's own watermark, not re-file the earlier drain's.
func TestListener_ChatDeltaAfterReconnect_FilesReadReceipt(t *testing.T) {
	home := t.TempDir()
	now := float64(time.Now().Unix())
	srv := newMarkReadServer(t, "["+tsMsg("m1", "boss", "kyle", now-300)+"]")
	cfg := markCfg(srv.URL, home)

	// A previous process already baselined on m1, so the first connect drain prints it…
	seedPath := chatSeenPath(cfg)
	if err := os.MkdirAll(filepath.Dir(seedPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(seedPath, []byte(`[]`), 0o644); err != nil {
		t.Fatal(err)
	}

	out := &syncBuf{}
	l := newTestListener(srv.Server, cfg, out)
	l.cursorPath = filepath.Join(home, "cursor")
	l.seen = loadChatSeen(seedPath)
	l.replySeen = loadReplyCardSeen(filepath.Join(home, "replycards-seen"))

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan int, 1)
	go func() { done <- l.run(ctx) }()
	waitForCond(t, func() bool { return strings.Contains(out.String(), "chat from boss (#m1") },
		"the cold-start drain printed m1")

	// Stop the loop BEFORE staging the delta. drainChat is single-goroutine by
	// construction (every real caller runs on the listen loop), and since the
	// reconnect path drains chat too, a delta dispatched from the test goroutine
	// while the loop is still re-dialling would be a second concurrent drain of
	// the same window — a race this test invented, not one the listener can have.
	cancel()
	<-done

	// …then the delta, on the only goroutine left, exactly as dispatch sees it.
	srv.setList("[" + strings.Join([]string{
		tsMsg("m1", "boss", "kyle", now-300),
		tsMsg("m2", "alice", "kyle", now-10),
	}, ",") + "]")
	l.dispatch([]byte(`{"topic":"chat","data":{"id":"m2"}}`))

	if !strings.Contains(out.String(), "chat from alice (#m2") {
		t.Fatalf("precondition: the delta drain must print m2, got %q", out.String())
	}
	calls := srv.snapshot()
	var got []markCall
	for _, c := range calls {
		if c.Peer == "alice" {
			got = append(got, c)
		}
	}
	if len(got) != 1 || got[0].LastReadTS != now-10 {
		t.Fatalf("the live-delta drain filed %+v for alice, want exactly one at ts %g "+
			"(all receipts: %+v)", got, now-10, calls)
	}
}

// ---------------------------------------------------------------------------
// A receipt that does not land says so — once — instead of leaving a silently
// dark ✓ with no error anywhere.
// ---------------------------------------------------------------------------

func TestDrainChat_MarkReadRejected_WarnsOncePerProcess(t *testing.T) {
	now := float64(time.Now().Unix())
	srv := newMarkReadServer(t, "["+tsMsg("m1", "boss", "kyle", now-30)+"]")
	srv.mu.Lock()
	srv.status = 422
	srv.mu.Unlock()
	cfg := markCfg(srv.URL, t.TempDir())
	seen := loadChatSeen("")
	var out bytes.Buffer

	drainChat(srv.Client(), cfg, seen, &out, false, nil)
	if c := strings.Count(out.String(), "mark-read"); c != 1 {
		t.Fatalf("a rejected receipt warned %d times, want exactly 1; out = %q", c, out.String())
	}
	// 🔴 THE WARNING MUST NOT WEAR THE TRANSPORT HEAD. A codex member does not
	// read this pane: cli/ocwarden/codex_session.go filters every line starting
	// `[ocagent] listen:` out of the model's turn as transport diagnostics. This
	// warning is the only sign that a ✓ is永遠不會亮 — spelled with that head it
	// would be swallowed on the codex side and printed on the claude side, with
	// both suites still green. So the first column is asserted, not just the count.
	for _, line := range strings.Split(strings.TrimSpace(out.String()), "\n") {
		if !strings.Contains(line, "mark-read") {
			continue
		}
		if !strings.HasPrefix(line, agentLinePrefix) {
			t.Fatalf("the warning must start at column 0 with %q; got %q", agentLinePrefix, line)
		}
		if strings.HasPrefix(line, agentLinePrefix+"listen:") {
			t.Fatalf("the warning must NOT start with %q — the codex sidecar drops that "+
				"prefix as transport noise, so the warning would reach a codex member "+
				"never at all; got %q", agentLinePrefix+"listen:", line)
		}
	}

	srv.setList("[" + strings.Join([]string{
		tsMsg("m1", "boss", "kyle", now-30),
		tsMsg("m2", "boss", "kyle", now-20),
	}, ",") + "]")
	out.Reset()
	drainChat(srv.Client(), cfg, seen, &out, false, nil)
	if strings.Contains(out.String(), "mark-read") {
		t.Fatalf("the warning must not repeat every drain; second drain out = %q", out.String())
	}
}

// ---------------------------------------------------------------------------
// THE WIRE TEST — the receipt body against the frozen MarkChatReadDTO.
// ---------------------------------------------------------------------------

// TestDrainChat_MarkReadBodyMatchesFrozenSchema drives the REAL producer
// (drainChat → reportChatRead → postJSON) and confronts the bodies a real test
// server caught with the schema frozen in spec/openapi.json.
//
// It has to be the producer's own body, never one assembled here: MarkChatReadDTO
// declares additionalProperties:false and the server decodes with
// DisallowUnknownFields, so ONE key this listener sends that the schema does not
// declare rejects the whole receipt. The listener already prints before it
// reports and warns at most once per process, so a permanently-422 receipt looks
// from the outside exactly like a healthy one after the first line — the ✓ simply
// never lights. A hand-built body compared here would agree with itself and say
// nothing about that.
func TestDrainChat_MarkReadBodyMatchesFrozenSchema(t *testing.T) {
	declared := frozenIngestProperties(t, "MarkChatReadDTO")

	now := float64(time.Now().Unix())
	// Two senders, so the loop below confronts more than one produced body.
	srv := newMarkReadServer(t, "["+strings.Join([]string{
		tsMsg("a1", "alice", "kyle", now-90),
		tsMsg("b1", "bob", "kyle", now-80),
		tsMsg("a2", "alice", "kyle", now-70),
	}, ",")+"]")
	var out bytes.Buffer

	drainChat(srv.Client(), markCfg(srv.URL, t.TempDir()), loadChatSeen(""), &out, false, nil)

	bodies := srv.rawBodies()
	if len(bodies) != 2 {
		t.Fatalf("precondition: want one receipt per sender (2), got %d: %q", len(bodies), bodies)
	}

	// Which uplinks this test actually walked against the frozen schema — not
	// which ones it meant to. Joined below against the manifest's own commitment.
	walked := map[string]int{}
	for _, body := range bodies {
		walked[markReadPath]++
		if bad := schemaViolations(body, declared); len(bad) > 0 {
			t.Errorf("mark-read body has keys the frozen schema refuses %v — the receipt "+
				"would 422 and the sender's ✓ would stay dark forever, with at most one "+
				"warning line ever printed; body=%s", bad, body)
		}
	}

	want := manifestUplinkPaths(t, "cli/ocagent/listen_markread_test.go")
	for route, rows := range want {
		if rows != 1 {
			t.Fatalf("cli/uplinks.json commits %d uplinks to %s through this wire test. "+
				"This join compares route SETS, so it cannot tell them apart — give the "+
				"second one its own assertion, or split the wire test.", rows, route)
		}
	}
	seen := map[string]int{}
	for route := range walked {
		seen[route] = 1
	}
	if !maps.Equal(seen, want) {
		t.Errorf("cli/uplinks.json commits %v to this wire test but the producer posted to "+
			"%v — a committed uplink nobody compared is the gap this manifest exists to close.",
			want, walked)
	}
}

// The reconnect drain is a THIRD drain entrance, and the rule the other two obey
// holds here too: it prints, so it marks — and the receipt carries the message's
// own ts, not the earlier drain's. Nothing is dispatched: a chat delta would let
// the pre-T-48 path take the credit, and this test would then prove nothing.
func TestListenerRun_ReconnectBackfill_PrintsThenFilesReadReceipt(t *testing.T) {
	home := t.TempDir()
	now := float64(time.Now().Unix())
	srv := newMarkReadServer(t, "["+tsMsg("m1", "boss", "kyle", now-300)+"]")
	cfg := markCfg(srv.URL, home)

	seedPath := chatSeenPath(cfg)
	if err := os.MkdirAll(filepath.Dir(seedPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(seedPath, []byte(`["m1"]`), 0o644); err != nil {
		t.Fatal(err)
	}

	out := &syncBuf{}
	l := newTestListener(srv.Server, cfg, out)
	l.cursorPath = filepath.Join(home, "cursor")
	l.seen = loadChatSeen(seedPath)
	l.replySeen = loadReplyCardSeen(filepath.Join(home, "replycards-seen"))

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan int, 1)
	go func() { done <- l.run(ctx) }()
	waitForCond(t, func() bool { return srv.dials() >= 3 },
		"the listener is up and re-dialling with nothing owed")
	if len(srv.snapshot()) != 0 {
		t.Fatalf("precondition: nothing was owed, so nothing may be marked yet: %+v", srv.snapshot())
	}

	// alice writes into the outage gap; no delta is ever fanned.
	srv.setList("[" + strings.Join([]string{
		tsMsg("m1", "boss", "kyle", now-300),
		tsMsg("m2", "alice", "kyle", now-10),
	}, ",") + "]")
	// Bounded on DIALS, not on the line — see the sibling outage test for why a
	// missing-drain build must fail on a count rather than on a timeout.
	staged := srv.dials()
	waitForCond(t, func() bool { return srv.dials() >= staged+3 },
		"three more reconnects happened after alice's message landed in the gap")
	cancel()
	<-done

	if n := strings.Count(out.String(), "chat from alice (#m2"); n != 1 {
		t.Fatalf("the reconnect drain printed alice's message %d times, want exactly 1; out = %q",
			n, out.String())
	}

	var got []markCall
	for _, c := range srv.snapshot() {
		if c.Peer == "alice" {
			got = append(got, c)
		}
	}
	if len(got) != 1 || got[0].LastReadTS != now-10 {
		t.Fatalf("the reconnect drain filed %+v for alice, want exactly one at ts %g "+
			"(all receipts: %+v)", got, now-10, srv.snapshot())
	}
}

// ---------------------------------------------------------------------------
// GUARDRAIL ④ — "printed" is not "delivered" when somebody else has to carry
// the line the rest of the way (T-48, the ack protocol).
//
// A claude member reads these lines directly, so printing IS delivery. A codex
// member reads them through the ocwarden sidecar, which turns each line into an
// App Server turn — and that turn can be REFUSED. The listener used to file the
// receipt and record the id regardless, so a refused message was marked read,
// dropped from the unread window, and never printed again: gone, with every
// party believing it had landed.
// ---------------------------------------------------------------------------

func ackEnv(v string) func(string) string {
	return func(key string) string {
		if key == listenAckEnv {
			return v
		}
		return ""
	}
}

// The claude path must not move by one byte: no marker on stdout, no wait for
// anything, receipt filed exactly as before.
func TestDrainChat_WithoutTheAckEnv_PrintsNoMarkerAndStillFilesTheReceipt(t *testing.T) {
	now := float64(time.Now().Unix())
	srv := newMarkReadServer(t, "["+tsMsg("m1", "boss", "kyle", now-30)+"]")
	seen := loadChatSeen("")
	seen.primed = true
	var out bytes.Buffer

	// stdin is deliberately a reader that would BLOCK FOREVER if anything read
	// it: the claude path must never wait for an answer nobody is going to send.
	gate := newAckGate(ackEnv(""), blockingReader{})
	if gate != nil {
		t.Fatalf("OC_LISTEN_ACK is unset, so there is no consumer to ack — the gate " +
			"must not exist at all")
	}

	drainChat(srv.Client(), markCfg(srv.URL, t.TempDir()), seen, &out, false, gate)

	if strings.Contains(out.String(), noticeBatch) {
		t.Errorf("the claude transcript grew a protocol line it has no reader for: %q",
			out.String())
	}
	if calls := srv.snapshot(); len(calls) != 1 || calls[0].Peer != "boss" {
		t.Errorf("the claude path stopped filing its own receipts: %+v", calls)
	}
	if !seen.m["m1"] {
		t.Error("the claude path stopped recording what it printed; the next drain " +
			"will print it all over again")
	}
}

// A gate exists only when the parent process asked for one.
func TestNewAckGate_OnlyOnTheParentsExplicitRequest(t *testing.T) {
	for value, want := range map[string]bool{"1": true, "": false, "0": false, "true": false} {
		got := newAckGate(ackEnv(value), strings.NewReader("")) != nil
		if got != want {
			t.Errorf("OC_LISTEN_ACK=%q ⇒ gate=%v, want %v — a gate nobody answers "+
				"hangs the drain, and a missing gate marks undelivered mail read",
				value, got, want)
		}
	}
}

// The ack path: marker out, verdict in, and only THEN the receipt.
func TestDrainChat_AckedBatch_FilesTheReceiptAndRecordsTheIDs(t *testing.T) {
	now := float64(time.Now().Unix())
	srv := newMarkReadServer(t, "["+tsMsg("m1", "boss", "kyle", now-30)+"]")
	seen := loadChatSeen("")
	seen.primed = true
	var out bytes.Buffer

	gate := newAckGate(ackEnv("1"), strings.NewReader("ack 1\n"))
	drainChat(srv.Client(), markCfg(srv.URL, t.TempDir()), seen, &out, false, gate)

	if !strings.Contains(out.String(), agentLinePrefix+noticeBatch+" 1") {
		t.Fatalf("the listener never told the sidecar where the batch ended, so no "+
			"ack can ever arrive and this drain hangs forever; out = %q", out.String())
	}
	if calls := srv.snapshot(); len(calls) != 1 {
		t.Errorf("an acked batch must still file its receipt: %+v", calls)
	}
	if !seen.m["m1"] {
		t.Error("an acked batch must be recorded, or it re-prints forever")
	}
}

// 🔴 THE WHOLE POINT. No ack ⇒ no receipt, no seen id, and the SAME message on
// the next drain.
// A consumer that goes quiet must not take this member's hearing with it: the
// wait for a verdict happens on the listener's ONLY thread, so an unbounded one
// turns a single unanswerable batch into a member that receives nothing ever
// again — and nothing anywhere says so.
func TestDrainChat_AckThatNeverArrives_TimesOutSaysSoAndKeepsTheMessageUnread(t *testing.T) {
	now := float64(time.Now().Unix())
	srv := newMarkReadServer(t, "["+tsMsg("m1", "boss", "kyle", now-30)+"]")
	cfg := markCfg(srv.URL, t.TempDir())
	seen := loadChatSeen(chatSeenPath(cfg))
	seen.primed = true
	var out bytes.Buffer

	// A pipe nobody ever writes to: the consumer is alive (stdin is open) and
	// simply never answers. A closed stdin is the OTHER case and already covered.
	answers, quiet := io.Pipe()
	defer quiet.Close()
	gate := newAckGate(ackEnv("1"), answers)
	gate.wait = 20 * time.Millisecond

	done := make(chan struct{})
	go func() {
		defer close(done)
		drainChat(srv.Client(), cfg, seen, &out, false, gate)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the drain never returned: an unanswered batch blocked the listener " +
			"forever, which is this member going permanently deaf")
	}

	if calls := srv.snapshot(); len(calls) != 0 {
		t.Errorf("a message whose delivery was never confirmed was marked read: %+v", calls)
	}
	if seen.m["m1"] {
		t.Error("an unconfirmed message was recorded as surfaced, so no later drain " +
			"can print it again")
	}
	if !strings.Contains(out.String(), "等不到") {
		t.Errorf("the timeout must say so — a silent one is indistinguishable from a "+
			"delivery that worked; out = %q", out.String())
	}
	// The warning must NOT wear the transport head, or the sidecar swallows it
	// and the one party who could go and look never hears about it.
	for _, line := range strings.Split(strings.TrimSpace(out.String()), "\n") {
		if strings.Contains(line, "等不到") && strings.HasPrefix(line, agentLinePrefix+"listen:") {
			t.Errorf("the timeout warning must not start with %q; got %q",
				agentLinePrefix+"listen:", line)
		}
	}
}

func TestDrainChat_UnackedBatch_LeavesTheMessageUnreadUnseenAndReprintable(t *testing.T) {
	now := float64(time.Now().Unix())
	list := "[" + tsMsg("m1", "boss", "kyle", now-30) + "]"
	srv := newMarkReadServer(t, list)
	home := t.TempDir()
	cfg := markCfg(srv.URL, home)
	seen := loadChatSeen(chatSeenPath(cfg))
	seen.primed = true
	var out bytes.Buffer

	// An answer for a DIFFERENT batch must not be mistaken for this one's.
	gate := newAckGate(ackEnv("1"), strings.NewReader("ack 7\nnack 1\n"))
	drainChat(srv.Client(), cfg, seen, &out, false, gate)

	if !strings.Contains(out.String(), "chat from boss (#m1") {
		t.Fatalf("precondition: the line must be printed before anyone can fail to "+
			"deliver it; out = %q", out.String())
	}
	if calls := srv.snapshot(); len(calls) != 0 {
		t.Errorf("a message that never reached the agent was marked read: %+v — the "+
			"sender now sees a ✓ for something nobody was ever shown", calls)
	}
	if seen.m["m1"] {
		t.Fatal("an undelivered message was recorded as surfaced; the seen set is " +
			"rebuilt from the refetched window, so this drops it for good and no " +
			"later drain can ever print it again")
	}

	// The recovery this exists for: the next drain prints it again.
	var out2 bytes.Buffer
	gate2 := newAckGate(ackEnv("1"), strings.NewReader("ack 1\n"))
	seen2 := loadChatSeen(chatSeenPath(cfg))
	if n := drainChat(srv.Client(), cfg, seen2, &out2, false, gate2); n != 1 {
		t.Fatalf("the next drain reported %d unread, want 1", n)
	}
	if !strings.Contains(out2.String(), "chat from boss (#m1") {
		t.Fatalf("the undelivered message never came back; out = %q", out2.String())
	}
	if calls := srv.snapshot(); len(calls) != 1 {
		t.Errorf("the re-delivered batch was acked, so its receipt must now be "+
			"filed exactly once: %+v", calls)
	}
}

// blockingReader never returns from Read: any wait on it is a hang, which is
// what the claude-path assertion above needs in order to mean anything.
type blockingReader struct{}

func (blockingReader) Read([]byte) (int, error) { select {} }
