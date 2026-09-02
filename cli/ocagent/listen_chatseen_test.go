package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

// ---------------------------------------------------------------------------
// T: the persisted chat unread cursor (chat-seen) — cold-start BACKFILL vs the
// first-run SILENT baseline vs a reconnect, which must never re-print.
// ---------------------------------------------------------------------------

// mutableChatServer serves a chat list the test can swap mid-run, and counts
// how many /api/chat refetches landed.
type mutableChatServer struct {
	*httptest.Server
	list  atomic.Value // string
	hits  int32
	conns int32 // /api/events dials
}

func newMutableChatServer(t *testing.T, list string) *mutableChatServer {
	t.Helper()
	m := &mutableChatServer{}
	m.list.Store(list)
	m.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/chat") {
			atomic.AddInt32(&m.hits, 1)
			w.WriteHeader(200)
			_, _ = w.Write([]byte(m.list.Load().(string)))
			return
		}
		if strings.HasPrefix(r.URL.Path, eventsPath) {
			// open, say nothing, close ⇒ the listener re-dials forever, and
			// every dial is one reconnect the drain has to cover.
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

func (m *mutableChatServer) dials() int32 { return atomic.LoadInt32(&m.conns) }

func (m *mutableChatServer) setList(s string) { m.list.Store(s) }

func msgsJSON(ids ...string) string {
	parts := make([]string, 0, len(ids))
	for _, id := range ids {
		parts = append(parts, fmt.Sprintf(`{"id":%q,"from":"boss","to":"kyle","body":"body-%s"}`, id, id))
	}
	return "[" + strings.Join(parts, ",") + "]"
}

func readSeenFile(t *testing.T, path string) []string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("chat-seen unreadable: %v", err)
	}
	var ids []string
	if err := json.Unmarshal(raw, &ids); err != nil {
		t.Fatalf("chat-seen is not a JSON id array: %v (%s)", err, raw)
	}
	return ids
}

func TestChatSeenPath_IsSiblingOfCursor(t *testing.T) {
	cfg := Config{Home: "/h", ID: "M-Kyle"}
	if got, want := chatSeenPath(cfg), filepath.Join("/h", "m-kyle", "chat-seen"); got != want {
		t.Fatalf("chatSeenPath = %q want %q", got, want)
	}
	if filepath.Dir(chatSeenPath(cfg)) != filepath.Dir(cursorPath(cfg)) {
		t.Fatal("chat-seen must live beside sse-cursor")
	}
	if got, want := chatSeenPath(Config{Home: "/h"}), filepath.Join("/h", "anon", "chat-seen"); got != want {
		t.Fatalf("id-less chatSeenPath = %q want %q", got, want)
	}
}

// FIRST RUN on this machine: no state file ⇒ the first drain must stay SILENT and
// leave a primed baseline behind. This is hard-condition #2 (a brand-new session
// must not be washed out by history that predates it).
func TestListenerRun_FirstEver_SilentBaseline_ThenPersists(t *testing.T) {
	home := t.TempDir()
	srv := newMutableChatServer(t, msgsJSON("m1", "m2"))
	cfg := Config{Base: srv.URL, Token: "t", ID: "kyle", Home: home}

	seen := loadChatSeen(chatSeenPath(cfg))
	if seen.primed {
		t.Fatal("a missing state file must yield an UNPRIMED store")
	}
	var out bytes.Buffer
	drainChat(srv.Client(), cfg, seen, &out, !seen.primed)

	if out.Len() != 0 {
		t.Fatalf("first-ever drain must print NOTHING, got %q", out.String())
	}
	got := readSeenFile(t, chatSeenPath(cfg))
	if len(got) != 2 || got[0] != "m1" || got[1] != "m2" {
		t.Fatalf("baseline file = %v want [m1 m2]", got)
	}
}

// COLD START with a baseline on disk: the messages that arrived while no
// listener held a stream MUST print. This is the whole point of the change.
func TestListenerRun_ColdStart_BackfillsWhatArrivedWhileDown(t *testing.T) {
	home := t.TempDir()
	srv := newMutableChatServer(t, msgsJSON("m1", "m2"))
	cfg := Config{Base: srv.URL, Token: "t", ID: "kyle", Home: home}

	// process #1 — first ever: silent baseline.
	var out1 bytes.Buffer
	s1 := loadChatSeen(chatSeenPath(cfg))
	drainChat(srv.Client(), cfg, s1, &out1, !s1.primed)
	if out1.Len() != 0 {
		t.Fatalf("process #1 must be silent, got %q", out1.String())
	}

	// …agent goes down; m3 and m4 arrive with nobody listening.
	srv.setList(msgsJSON("m1", "m2", "m3", "m4"))

	// process #2 — a NEW process, empty memory, but the file is primed.
	var out2 bytes.Buffer
	s2 := loadChatSeen(chatSeenPath(cfg))
	if !s2.primed {
		t.Fatal("a persisted baseline must load PRIMED")
	}
	n := drainChat(srv.Client(), cfg, s2, &out2, !s2.primed)

	if n != 2 {
		t.Fatalf("backfill count = %d want 2", n)
	}
	got := out2.String()
	// COUNT, don't merely detect: `Contains` passes just as happily on a backfill
	// that printed the same line five times, and a duplicate backfill is its own
	// bug (it washes out the context the cap exists to protect).
	for _, want := range []string{"chat from boss (#m3): body-m3", "chat from boss (#m4): body-m4"} {
		if c := strings.Count(got, want); c != 1 {
			t.Fatalf("cold-start backfill printed %q %d times, want exactly 1; out = %q", want, c, got)
		}
	}
	if strings.Contains(got, "#m1") || strings.Contains(got, "#m2") {
		t.Fatalf("backfill must NOT re-print the pre-baseline history; out = %q", got)
	}

	// process #3 — nothing new arrived: silence.
	var out3 bytes.Buffer
	s3 := loadChatSeen(chatSeenPath(cfg))
	if n3 := drainChat(srv.Client(), cfg, s3, &out3, !s3.primed); n3 != 0 || out3.Len() != 0 {
		t.Fatalf("re-open with nothing new: n=%d out=%q, want 0 and silence", n3, out3.String())
	}
}

// HARD CONDITION #1, at the run() level: a listener that DROPS and re-dials many
// times drains chat on EVERY dial and still prints each line exactly ONCE — the
// persisted cursor, not the number of drains, is what stops history re-printing.
func TestListenerRun_Reconnects_NeverRePrintHistory(t *testing.T) {
	home := t.TempDir()
	cfg := Config{Base: "", Token: "tok", ID: "kyle", Home: home}

	// Prime the baseline with m1, then let m2 be the one unread line.
	chat := msgsJSON("m1", "m2")
	var conns int32
	var chatHits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/chat") {
			atomic.AddInt32(&chatHits, 1)
			w.WriteHeader(200)
			_, _ = w.Write([]byte(chat))
			return
		}
		if !strings.HasPrefix(r.URL.Path, eventsPath) {
			w.WriteHeader(404)
			return
		}
		// Open, say nothing, close immediately ⇒ the listener re-dials forever.
		atomic.AddInt32(&conns, 1)
		w.Header().Set("Content-Type", "text/event-stream")
		if fl, ok := w.(http.Flusher); ok {
			fl.Flush()
		}
	}))
	defer srv.Close()
	cfg.Base = srv.URL

	// A previous process already baselined on m1 only.
	seedPath := chatSeenPath(cfg)
	if err := os.MkdirAll(filepath.Dir(seedPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(seedPath, []byte(`["m1"]`), 0o644); err != nil {
		t.Fatal(err)
	}

	out := &syncBuf{}
	l := newTestListener(srv, cfg, out)
	l.cursorPath = filepath.Join(home, "cursor")
	l.seen = loadChatSeen(seedPath)
	l.replySeen = loadReplyCardSeen(filepath.Join(home, "replycards-seen"))

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan int, 1)
	go func() { done <- l.run(ctx) }()

	// wait until the boot backfill printed AND the stream has re-dialled a lot.
	waitForCond(t, func() bool {
		return strings.Contains(out.String(), "chat from boss (#m2): body-m2") &&
			atomic.LoadInt32(&conns) >= 5
	}, "boot backfill printed and the stream re-dialled repeatedly")
	cancel()
	<-done

	if got := strings.Count(out.String(), "chat from boss (#m2)"); got != 1 {
		t.Fatalf("m2 printed %d times across %d reconnects — want exactly 1",
			got, atomic.LoadInt32(&conns))
	}
	if strings.Contains(out.String(), "#m1") {
		t.Fatalf("m1 was already baselined and must never print; out = %q", out.String())
	}
	// Negative control on the control: the test only means something if the
	// listener really did re-dial many times.
	if c := atomic.LoadInt32(&conns); c < 5 {
		t.Fatalf("only %d reconnects — this test proved nothing", c)
	}
	// Chat IS refetched on every dial now (the reconnect drain — no delta frames
	// were ever sent, so these are all drains). That is the point: the dedup that
	// keeps m2 at one printed line is the cursor, not a refusal to look.
	if h := atomic.LoadInt32(&chatHits); h < 2 {
		t.Fatalf("/api/chat refetched %d times across %d dials — the reconnect drain never ran",
			h, atomic.LoadInt32(&conns))
	}
}

// HARD CONDITION #3: a corrupt state file behaves exactly like "first run" —
// silent baseline, no crash — and is repaired on the way out.
func TestLoadChatSeen_CorruptOrMissing_ReprimesSilently(t *testing.T) {
	for _, tc := range []struct{ name, body string }{
		{"garbage", "not json at all"},
		{"null", "null"},
		{"wrong-shape", `{"m1":true}`},
		{"truncated", `["m1"`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			home := t.TempDir()
			srv := newMutableChatServer(t, msgsJSON("m1", "m2"))
			cfg := Config{Base: srv.URL, Token: "t", ID: "kyle", Home: home}
			p := chatSeenPath(cfg)
			if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(p, []byte(tc.body), 0o644); err != nil {
				t.Fatal(err)
			}

			s := loadChatSeen(p)
			if s.primed {
				t.Fatal("a corrupt state file must load UNPRIMED")
			}
			var out bytes.Buffer
			drainChat(srv.Client(), cfg, s, &out, !s.primed)
			if out.Len() != 0 {
				t.Fatalf("corrupt-file reboot must be silent, got %q", out.String())
			}
			if got := readSeenFile(t, p); len(got) != 2 {
				t.Fatalf("corrupt file must be rewritten as a real baseline, got %v", got)
			}
		})
	}
}

// An empty-but-valid baseline is still a baseline: an agent whose inbox was
// genuinely empty must NOT re-baseline silently on its next boot.
func TestLoadChatSeen_EmptyArrayIsPrimed(t *testing.T) {
	home := t.TempDir()
	srv := newMutableChatServer(t, `[]`)
	cfg := Config{Base: srv.URL, Token: "t", ID: "kyle", Home: home}

	s1 := loadChatSeen(chatSeenPath(cfg))
	var out bytes.Buffer
	drainChat(srv.Client(), cfg, s1, &out, !s1.primed)
	if got := readSeenFile(t, chatSeenPath(cfg)); len(got) != 0 {
		t.Fatalf("empty inbox baseline = %v want []", got)
	}

	srv.setList(msgsJSON("m1"))
	s2 := loadChatSeen(chatSeenPath(cfg))
	if !s2.primed {
		t.Fatal("`[]` on disk IS a baseline — must load PRIMED")
	}
	out.Reset()
	drainChat(srv.Client(), cfg, s2, &out, !s2.primed)
	if !strings.Contains(out.String(), "chat from boss (#m1)") {
		t.Fatalf("first message after an empty baseline must backfill; out = %q", out.String())
	}
}

// The backfill is BOUNDED: over the cap only the newest chatBacklogPrintCap
// lines print, one notice names the drop, and the dropped ones are still
// recorded so they never come back.
func TestDrainChat_BacklogOverCap_TruncatesOldestAndSaysSo(t *testing.T) {
	home := t.TempDir()
	total := chatBacklogPrintCap + 7
	ids := make([]string, 0, total)
	for i := 1; i <= total; i++ {
		ids = append(ids, fmt.Sprintf("m%02d", i))
	}
	srv := newMutableChatServer(t, msgsJSON(ids...))
	cfg := Config{Base: srv.URL, Token: "t", ID: "kyle", Home: home}

	// primed with nothing seen ⇒ the whole list is backlog.
	if err := os.MkdirAll(filepath.Dir(chatSeenPath(cfg)), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(chatSeenPath(cfg), []byte(`[]`), 0o644); err != nil {
		t.Fatal(err)
	}
	s := loadChatSeen(chatSeenPath(cfg))
	var out bytes.Buffer
	if n := drainChat(srv.Client(), cfg, s, &out, false); n != total {
		t.Fatalf("returned count = %d want the full unread count %d", n, total)
	}

	got := out.String()
	lines := strings.Count(got, "[ocagent] chat from ")
	if lines != chatBacklogPrintCap {
		t.Fatalf("printed %d chat lines, want the cap %d", lines, chatBacklogPrintCap)
	}
	if !strings.Contains(got, fmt.Sprintf("至少 %d 則未讀，只補印最新 %d 則", total, chatBacklogPrintCap)) ||
		!strings.Contains(got, "get_chat") {
		t.Fatalf("truncation notice missing or unhelpful; out = %q", got)
	}
	// The SKIPPED count is the number the reader acts on — it is what tells them
	// how much is missing and therefore whether to go and fetch it. Asserting the
	// prefix above leaves it unguarded: an independent review put a constant 999
	// there and every test still passed.
	if !strings.Contains(got, fmt.Sprintf("略過 %d 則較舊", total-chatBacklogPrintCap)) {
		t.Fatalf("truncation notice must state the skipped count %d; out = %q",
			total-chatBacklogPrintCap, got)
	}
	// the NEWEST survive, the OLDEST are dropped.
	if !strings.Contains(got, "#"+ids[total-1]) {
		t.Fatalf("newest message %s must print; out = %q", ids[total-1], got)
	}
	if strings.Contains(got, "#"+ids[0]) {
		t.Fatalf("oldest message %s must be truncated away; out = %q", ids[0], got)
	}
	// dropped-but-announced messages are recorded: the next drain is silent.
	out.Reset()
	s2 := loadChatSeen(chatSeenPath(cfg))
	if n := drainChat(srv.Client(), cfg, s2, &out, false); n != 0 || out.Len() != 0 {
		t.Fatalf("truncated messages must still be marked seen: n=%d out=%q", n, out.String())
	}
}

// A fetch fault must not wipe the baseline (which would re-print everything on
// the next successful drain) and must not crash the listener.
func TestDrainChat_FetchFault_LeavesStateUntouched(t *testing.T) {
	home := t.TempDir()
	fail := int32(0)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if atomic.LoadInt32(&fail) == 1 {
			w.WriteHeader(503)
			return
		}
		w.WriteHeader(200)
		_, _ = w.Write([]byte(msgsJSON("m1", "m2")))
	}))
	defer srv.Close()
	cfg := Config{Base: srv.URL, Token: "t", ID: "kyle", Home: home}

	var out bytes.Buffer
	s := loadChatSeen(chatSeenPath(cfg))
	drainChat(srv.Client(), cfg, s, &out, true)
	before := readSeenFile(t, chatSeenPath(cfg))

	atomic.StoreInt32(&fail, 1)
	s2 := loadChatSeen(chatSeenPath(cfg))
	out.Reset()
	if n := drainChat(srv.Client(), cfg, s2, &out, false); n != 0 || out.Len() != 0 {
		t.Fatalf("faulting drain: n=%d out=%q, want 0 and silence", n, out.String())
	}
	after := readSeenFile(t, chatSeenPath(cfg))
	if len(after) != len(before) {
		t.Fatalf("fault rewrote the baseline: %v → %v", before, after)
	}
}

// The set is bounded by the server's own window: an id that has aged out of
// /api/chat drops out of the file instead of accumulating forever.
func TestDrainChat_PrunesIdsGoneFromTheAuthority(t *testing.T) {
	home := t.TempDir()
	srv := newMutableChatServer(t, msgsJSON("m1", "m2", "m3"))
	cfg := Config{Base: srv.URL, Token: "t", ID: "kyle", Home: home}
	var out bytes.Buffer
	s := loadChatSeen(chatSeenPath(cfg))
	drainChat(srv.Client(), cfg, s, &out, true)
	if got := readSeenFile(t, chatSeenPath(cfg)); len(got) != 3 {
		t.Fatalf("baseline = %v want 3 ids", got)
	}
	srv.setList(msgsJSON("m3", "m4")) // m1/m2 aged out of the window
	s2 := loadChatSeen(chatSeenPath(cfg))
	out.Reset()
	drainChat(srv.Client(), cfg, s2, &out, false)
	got := readSeenFile(t, chatSeenPath(cfg))
	if len(got) != 2 || got[0] != "m3" || got[1] != "m4" {
		t.Fatalf("pruned set = %v want [m3 m4]", got)
	}
	if !strings.Contains(out.String(), "#m4") || strings.Contains(out.String(), "#m3") {
		t.Fatalf("only the genuinely new m4 should print; out = %q", out.String())
	}
}

// HARD CONDITION #2, at the run() level: a machine with NO state file boots
// silent — a listener started for the very first time must not dump history
// into a session that never saw it.
func TestListenerRun_FirstEver_SilentThroughRun(t *testing.T) {
	home := t.TempDir()
	var conns int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/chat") {
			w.WriteHeader(200)
			_, _ = w.Write([]byte(msgsJSON("m1", "m2", "m3")))
			return
		}
		if !strings.HasPrefix(r.URL.Path, eventsPath) {
			w.WriteHeader(404)
			return
		}
		atomic.AddInt32(&conns, 1)
		w.Header().Set("Content-Type", "text/event-stream")
		if fl, ok := w.(http.Flusher); ok {
			fl.Flush()
		}
	}))
	defer srv.Close()
	cfg := Config{Base: srv.URL, Token: "tok", ID: "kyle", Home: home}

	out := &syncBuf{}
	l := newTestListener(srv, cfg, out)
	l.cursorPath = filepath.Join(home, "cursor")
	l.seen = loadChatSeen(chatSeenPath(cfg))
	l.replySeen = loadReplyCardSeen(filepath.Join(home, "replycards-seen"))

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan int, 1)
	go func() { done <- l.run(ctx) }()
	waitForCond(t, func() bool { return atomic.LoadInt32(&conns) >= 3 },
		"the listener connected (so the connect drain has certainly run)")
	cancel()
	<-done

	if strings.Contains(out.String(), "chat from boss") {
		t.Fatalf("a first-ever listener must print NO chat history; out = %q", out.String())
	}
	if got := readSeenFile(t, chatSeenPath(cfg)); len(got) != 3 {
		t.Fatalf("a first-ever listener must leave a primed baseline, got %v", got)
	}
}

// ---------------------------------------------------------------------------
// The CONTROLLED COUNT. Everything above proves the pieces behave; what was
// missing is the differential the fix is actually claimed on — same environment,
// same message, OLD build 0, NEW build 1 — measured at the run() level, where
// the two are wired together.
// ---------------------------------------------------------------------------

// newBackfillRunServer serves a fixed chat list plus an /api/events stream that
// opens and closes immediately, so the listener re-dials forever and the dial
// count can be used as a denominator.
func newBackfillRunServer(t *testing.T, list string, conns *int32) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/chat") {
			w.WriteHeader(200)
			_, _ = w.Write([]byte(list))
			return
		}
		if !strings.HasPrefix(r.URL.Path, eventsPath) {
			w.WriteHeader(404)
			return
		}
		atomic.AddInt32(conns, 1)
		w.Header().Set("Content-Type", "text/event-stream")
		if fl, ok := w.(http.Flusher); ok {
			fl.Flush()
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

// bootOnce runs one whole listener lifecycle against srv and returns what it
// printed. It waits on the DIAL COUNT, never on the output — see the comment on
// the differential test below for why that distinction is the whole point.
func bootOnce(t *testing.T, srv *httptest.Server, cfg Config, conns *int32, seen *chatSeen) string {
	t.Helper()
	before := atomic.LoadInt32(conns)
	out := &syncBuf{}
	l := newTestListener(srv, cfg, out)
	l.cursorPath = filepath.Join(cfg.Home, "cursor")
	l.seen = seen
	l.replySeen = loadReplyCardSeen(filepath.Join(cfg.Home, "replycards-seen"))

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan int, 1)
	go func() { done <- l.run(ctx) }()
	waitForCond(t, func() bool { return atomic.LoadInt32(conns)-before >= 3 },
		"the listener dialled 3 times (so the connect drain has certainly run)")
	cancel()
	<-done
	return out.String()
}

// THE DIFFERENTIAL, COUNTED. Two arms differing in exactly one bit — whether the
// persisted cursor came up primed — run against the same server, the same
// baseline file and the same single unread message. The old build prints it
// ZERO times (that is the bug: silently marked read); the new build prints it
// EXACTLY once (not "at least once" — a backfill that repeats is its own bug).
//
// 🔴 WHY THIS WAITS ON THE DIAL COUNT AND NOT ON THE OUTPUT: the old arm's
// expected result is silence, and you cannot wait for silence. A test that
// blocks until the backfill appears turns the old arm into a TIMEOUT — which is
// a chain-collapse, not evidence: a timeout is what a deadlock, a mis-wired
// fake, or a hung dial all look like too. Waiting on dials gives every arm the
// same bounded, positively-observed moment to be judged at, so the failure that
// reports is `0 != 1` on a named count.
//
// This is the guard that was missing: reverting the drain to the pre-fix
// unconditional-silent form used to redden nothing but a 3-second timeout.
func TestListenerRun_ColdStartBackfill_CountedOldBuildVsNew(t *testing.T) {
	for _, tc := range []struct {
		name     string
		oldBuild bool // old build ⇒ the cold-start drain is unconditionally silent
		want     int
	}{
		{"old-build-swallows-it", true, 0},
		{"new-build-prints-it-once", false, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			home := t.TempDir()
			var conns int32
			srv := newBackfillRunServer(t, msgsJSON("m1", "m2"), &conns)
			cfg := Config{Base: srv.URL, Token: "tok", ID: "kyle", Home: home}

			// identical starting state in BOTH arms: a baseline holding m1, with
			// m2 as the one message that arrived while nobody was listening.
			p := chatSeenPath(cfg)
			if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(p, []byte(`["m1"]`), 0o644); err != nil {
				t.Fatal(err)
			}
			seen := loadChatSeen(p)
			if !seen.primed {
				t.Fatal("both arms must start from a primed baseline")
			}
			if tc.oldBuild {
				// The pre-fix world in one bit: the cursor never survived the
				// process, so the cold-start drain had nothing to diff against and ran
				// silent every single time.
				seen.primed = false
			}

			got := strings.Count(bootOnce(t, srv, cfg, &conns, seen), "chat from boss (#m2)")
			if got != tc.want {
				t.Fatalf("%s: m2 printed %d times across %d dials, want %d",
					tc.name, got, atomic.LoadInt32(&conns), tc.want)
			}
		})
	}
}

// The same differential across two REAL process lifecycles, with nothing
// hand-set: boot once on a virgin home (silent baseline), let messages arrive
// while nothing is listening, boot again from what the first boot left on disk.
// Every message is counted, so a backfill that prints twice fails here too.
//
// This closes the bypass that let the drain wiring rot unnoticed: the
// cold-start test above it calls drainChat directly and therefore cannot see a
// mistake in how run() decides silence.
func TestListenerRun_ColdStart_AcrossTwoBoots_PrintsEachExactlyOnce(t *testing.T) {
	home := t.TempDir()
	cfg := Config{Token: "tok", ID: "kyle", Home: home}

	// --- boot #1: nothing on disk. Must be silent, must leave a baseline. ---
	var conns1 int32
	srv1 := newBackfillRunServer(t, msgsJSON("m1", "m2"), &conns1)
	cfg.Base = srv1.URL
	out1 := bootOnce(t, srv1, cfg, &conns1, loadChatSeen(chatSeenPath(cfg)))
	if n := strings.Count(out1, "chat from boss"); n != 0 {
		t.Fatalf("boot #1 (first ever) printed %d chat lines across %d dials, want 0; out = %q",
			n, atomic.LoadInt32(&conns1), out1)
	}
	if got := readSeenFile(t, chatSeenPath(cfg)); len(got) != 2 {
		t.Fatalf("boot #1 must leave a baseline of 2, got %v", got)
	}

	// --- the agent is down; m3 and m4 arrive with nobody holding a stream. ---
	var conns2 int32
	srv2 := newBackfillRunServer(t, msgsJSON("m1", "m2", "m3", "m4"), &conns2)
	cfg.Base = srv2.URL

	// --- boot #2: a genuinely new process, reading only what boot #1 wrote. ---
	out2 := bootOnce(t, srv2, cfg, &conns2, loadChatSeen(chatSeenPath(cfg)))
	for _, id := range []string{"m3", "m4"} {
		if n := strings.Count(out2, "chat from boss (#"+id+")"); n != 1 {
			t.Fatalf("boot #2: %s printed %d times across %d dials, want exactly 1; out = %q",
				id, n, atomic.LoadInt32(&conns2), out2)
		}
	}
	for _, id := range []string{"m1", "m2"} {
		if strings.Contains(out2, "#"+id) {
			t.Fatalf("boot #2 re-printed pre-baseline history %s; out = %q", id, out2)
		}
	}

	// --- boot #3: nothing new arrived. Silence again. ---
	var conns3 int32
	srv3 := newBackfillRunServer(t, msgsJSON("m1", "m2", "m3", "m4"), &conns3)
	cfg.Base = srv3.URL
	out3 := bootOnce(t, srv3, cfg, &conns3, loadChatSeen(chatSeenPath(cfg)))
	if n := strings.Count(out3, "chat from boss"); n != 0 {
		t.Fatalf("boot #3 (nothing new) printed %d chat lines across %d dials, want 0; out = %q",
			n, atomic.LoadInt32(&conns3), out3)
	}
}

// A cursor that cannot be written must SAY SO. Without this line the failure is
// indistinguishable from a healthy run — the drain prints, the count is right,
// and only the NEXT boot silently swallows everything, which is the exact bug
// this file exists to prevent.
func TestChatSeen_PersistFailure_AnnouncesInsteadOfRelapsingSilently(t *testing.T) {
	home := t.TempDir()
	cfg := Config{Token: "t", ID: "kyle", Home: home}
	// occupy the parent path with a FILE so MkdirAll can never succeed.
	if err := os.WriteFile(filepath.Join(home, "kyle"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	srv := newMutableChatServer(t, msgsJSON("m1", "m2"))
	cfg.Base = srv.URL

	var out bytes.Buffer
	s := loadChatSeen(chatSeenPath(cfg))
	drainChat(srv.Client(), cfg, s, &out, !s.primed)

	// the write really did fail — otherwise this test proves nothing.
	if _, err := os.ReadFile(chatSeenPath(cfg)); err == nil {
		t.Fatal("negative control: the state file was written after all")
	}
	if s.primed {
		t.Fatal("a failed write must not claim a baseline")
	}
	got := out.String()
	if !strings.Contains(got, "chat-seen 寫不進去") {
		t.Fatalf("an unwritable cursor must be announced; out = %q", got)
	}
	if !strings.Contains(got, chatSeenPath(cfg)) {
		t.Fatalf("the warning must name the path it could not write; out = %q", got)
	}
	if !strings.Contains(got, "get_chat") {
		t.Fatalf("the warning must tell the reader what to do instead; out = %q", got)
	}
	// The SAME sentence must hold on the other branch too (see the test below),
	// so it may not claim this one's direction.
	if !strings.Contains(got, "沒被記下來") || !strings.Contains(got, "可能少印或重印") {
		t.Fatalf("the warning must claim only what holds on both branches; out = %q", got)
	}

	// ONCE per process: drainChat also runs on every inbound delta, and a
	// repeated warning would drown the very context the cap protects.
	out.Reset()
	drainChat(srv.Client(), cfg, s, &out, false)
	if strings.Contains(out.String(), "chat-seen 寫不進去") {
		t.Fatalf("the warning must not repeat within one process; out = %q", out.String())
	}
}

// THE OTHER FAILURE BRANCH. Above, the cursor was NEVER written and the next
// boot swallows the window. Here it was written once and only LATER became
// unwritable — and the next boot does the OPPOSITE: it loads the stale baseline
// and re-prints, every boot, until someone fixes the permission.
//
// 🔴 This test exists because the warning line originally asserted the
// swallow branch as if it were the only one. It is not, and the two point in
// opposite directions, so the line may only name the loss and the range. Both
// branches assert the SAME sentence: that is the property under test.
func TestChatSeen_PersistFailure_AfterAGoodWrite_RePrintsInsteadOfSwallowing(t *testing.T) {
	home := t.TempDir()
	srv := newMutableChatServer(t, msgsJSON("m1"))
	cfg := Config{Base: srv.URL, Token: "t", ID: "kyle", Home: home}

	// a healthy baseline lands first — this is what makes it the OTHER branch.
	var out bytes.Buffer
	s0 := loadChatSeen(chatSeenPath(cfg))
	drainChat(srv.Client(), cfg, s0, &out, !s0.primed)
	if !s0.primed {
		t.Fatal("setup: the first write must succeed")
	}

	// the FILE goes read-only; its directory stays writable, so MkdirAll still
	// succeeds and only the write itself fails. (A read-only *directory* would
	// not reproduce this: writing an existing file inside one still succeeds.)
	if err := os.Chmod(chatSeenPath(cfg), 0o400); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(chatSeenPath(cfg), 0o600) })

	srv.setList(msgsJSON("m1", "m2"))
	out.Reset()
	s1 := loadChatSeen(chatSeenPath(cfg))
	drainChat(srv.Client(), cfg, s1, &out, !s1.primed)
	if c := strings.Count(out.String(), "chat from boss (#m2)"); c != 1 {
		t.Fatalf("this process must still surface m2 once, got %d; out = %q", c, out.String())
	}
	if !strings.Contains(out.String(), "chat-seen 寫不進去") {
		t.Fatalf("a failed write must be announced on this branch too; out = %q", out.String())
	}

	// THE NEXT PROCESS: m2 comes back — the failure here is duplication, not
	// silence. Pinning it stops anyone re-asserting "silently marked read".
	out.Reset()
	s2 := loadChatSeen(chatSeenPath(cfg))
	if !s2.primed {
		t.Fatal("the stale baseline is still on disk, so this boot loads PRIMED")
	}
	drainChat(srv.Client(), cfg, s2, &out, !s2.primed)
	if c := strings.Count(out.String(), "chat from boss (#m2)"); c != 1 {
		t.Fatalf("next boot must RE-print m2 (not swallow it), got %d; out = %q", c, out.String())
	}

	// and the sentence must be true of BOTH branches: it may name the loss and
	// the range, never one direction.
	warning := out.String()
	if !strings.Contains(warning, "沒被記下來") || !strings.Contains(warning, "可能少印或重印") {
		t.Fatalf("the warning must claim only what holds on both branches; out = %q", warning)
	}
	if strings.Contains(warning, "靜默當成已讀") {
		t.Fatalf("the warning must NOT assert the swallow branch — it is false here; out = %q", warning)
	}
	if !strings.Contains(warning, "get_chat") {
		t.Fatalf("the warning must still say what to do instead; out = %q", warning)
	}
}

// Negative control on the warning: a healthy write says nothing at all.
func TestChatSeen_PersistSuccess_SaysNothing(t *testing.T) {
	home := t.TempDir()
	srv := newMutableChatServer(t, msgsJSON("m1", "m2"))
	cfg := Config{Base: srv.URL, Token: "t", ID: "kyle", Home: home}
	var out bytes.Buffer
	s := loadChatSeen(chatSeenPath(cfg))
	drainChat(srv.Client(), cfg, s, &out, !s.primed)
	if !s.primed {
		t.Fatal("a healthy write must prime")
	}
	if strings.Contains(out.String(), "chat-seen") {
		t.Fatalf("a healthy drain must not mention the cursor at all; out = %q", out.String())
	}
}

// ---------------------------------------------------------------------------
// T-48: /api/events has NO replay, so a chat message fanned while this listener
// held no stream is gone from the stream forever. Before the reconnect drain,
// the only thing that could surface it was the NEXT chat delta — so if nobody
// spoke again, the agent was never told anyone had called, and nothing errored.
// ---------------------------------------------------------------------------

// THE THING THIS CHANGE BUYS. A message arrives in the outage window and NO chat
// delta is ever dispatched afterwards; the reconnect drain is the only thing that
// can surface it, and it must — exactly once.
func TestListenerRun_MessageArrivingDuringTheOutage_PrintsOnReconnect(t *testing.T) {
	home := t.TempDir()
	srv := newMutableChatServer(t, msgsJSON("m1"))
	cfg := Config{Base: srv.URL, Token: "tok", ID: "kyle", Home: home}

	// A previous process already baselined on m1: nothing is owed at boot.
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
	if strings.Contains(out.String(), "chat from boss") {
		t.Fatalf("precondition: nothing was owed at boot, got %q", out.String())
	}

	// m2 is fanned INTO THE GAP. Note what is deliberately absent from here on:
	// no l.dispatch, no chat frame on the wire. The reconnect drain is the only
	// path left, which is precisely the case that used to go dark.
	//
	// 🔴 WAIT ON THE DIAL COUNT, NOT ON THE LINE. A build with no reconnect drain
	// never prints m2 at all, and you cannot wait for something that never comes:
	// waiting on the output turns that build's failure into a 3-second TIMEOUT,
	// which is also what a deadlock, a hung dial or a mis-wired fake look like.
	// Dials give every build the same bounded, positively-observed moment to be
	// judged at, so the regression reports as `0 != 1` on a named count.
	srv.setList(msgsJSON("m1", "m2"))
	staged := srv.dials()
	waitForCond(t, func() bool { return srv.dials() >= staged+3 },
		"three more reconnects happened after the message landed in the gap")
	cancel()
	<-done

	if got := strings.Count(out.String(), "chat from boss (#m2): body-m2"); got != 1 {
		t.Fatalf("a message that arrived during the outage printed %d times across %d "+
			"reconnects — want exactly 1; out = %q", got, srv.dials(), out.String())
	}
	if strings.Contains(out.String(), "#m1") {
		t.Fatalf("the pre-baselined m1 must never print; out = %q", out.String())
	}
}

// THE SILENT-BASELINE INVARIANT WHEN THE FIRST DRAIN FAULTS. Silence is decided
// from the persisted cursor, and the case where that is load-bearing is a first
// connect drain that could not FETCH — it left the store unprimed, so the NEXT
// connect drain is the first one ever to see this member's inbox. It must prime
// silently: a brand-new session must not be washed out by history that predates
// it.
func TestListenerRun_FirstEverWithAFaultedBootDrain_ConnectDrainStaysSilent(t *testing.T) {
	home := t.TempDir()
	var chatHits, conns int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/chat") {
			if atomic.AddInt32(&chatHits, 1) == 1 {
				w.WriteHeader(500) // the FIRST connect drain faults ⇒ store stays unprimed
				return
			}
			w.WriteHeader(200)
			_, _ = w.Write([]byte(msgsJSON("m1", "m2", "m3")))
			return
		}
		if !strings.HasPrefix(r.URL.Path, eventsPath) {
			w.WriteHeader(404)
			return
		}
		atomic.AddInt32(&conns, 1)
		w.Header().Set("Content-Type", "text/event-stream")
		if fl, ok := w.(http.Flusher); ok {
			fl.Flush()
		}
	}))
	defer srv.Close()
	cfg := Config{Base: srv.URL, Token: "tok", ID: "kyle", Home: home}

	out := &syncBuf{}
	l := newTestListener(srv, cfg, out)
	l.cursorPath = filepath.Join(home, "cursor")
	l.seen = loadChatSeen(chatSeenPath(cfg))
	if l.seen.primed {
		t.Fatal("precondition: a virgin home must load UNPRIMED")
	}
	l.replySeen = loadReplyCardSeen(filepath.Join(home, "replycards-seen"))

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan int, 1)
	go func() { done <- l.run(ctx) }()
	waitForCond(t, func() bool { return atomic.LoadInt32(&conns) >= 3 },
		"the listener dialled 3 times, so several connect drains have run")
	cancel()
	<-done

	if strings.Contains(out.String(), "chat from boss") {
		t.Fatalf("the connect drain of a first-ever listener must print NO history; out = %q",
			out.String())
	}
	if got := readSeenFile(t, chatSeenPath(cfg)); len(got) != 3 {
		t.Fatalf("the connect drain must still leave a primed baseline, got %v", got)
	}
}

// THE SAME INVARIANT ON THE THIRD PATH. Boot and connect both decide silence
// from the persisted cursor; the live chat delta must too, and the case that
// separates them is identical to the connect one: a drain that FAULTED leaves
// the store unprimed, and if a delta beats the next reconnect then the delta
// drain is the first thing ever to see this member's inbox.
//
// A delta is a NUDGE ("something arrived"), never a licence to print whatever a
// refetch happens to return. Deciding silence from "I am a delta, deltas print"
// is deciding it from the CALLER, and the caller is exactly what cannot know
// whether this machine has a baseline to diff against.
func TestListenerDispatch_ChatDeltaOnAnUnprimedStore_StaysSilent(t *testing.T) {
	home := t.TempDir()
	var chatHits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/api/chat") {
			w.WriteHeader(404)
			return
		}
		if atomic.AddInt32(&chatHits, 1) == 1 {
			w.WriteHeader(500) // the first drain faults ⇒ the store stays unprimed
			return
		}
		w.WriteHeader(200)
		_, _ = w.Write([]byte(msgsJSON("m1", "m2", "m3")))
	}))
	defer srv.Close()
	cfg := Config{Base: srv.URL, Token: "tok", ID: "kyle", Home: home}

	var out bytes.Buffer
	l := newTestListener(srv, cfg, &out)
	l.seen = loadChatSeen(chatSeenPath(cfg))

	// the first drain, faulting: it prints nothing and records nothing.
	l.drainChatNow()
	if l.seen.primed {
		t.Fatal("precondition: a drain that could not fetch must leave the store UNPRIMED")
	}
	if out.Len() != 0 {
		t.Fatalf("precondition: a faulted drain prints nothing, got %q", out.String())
	}

	// …and now a chat delta arrives before any reconnect could re-drain.
	l.dispatch([]byte(`{"topic":"chat","data":{"id":"m3"}}`))

	if strings.Contains(out.String(), "chat from boss") {
		t.Fatalf("a delta drain on an unprimed store must print NO history; out = %q", out.String())
	}
	if got := readSeenFile(t, chatSeenPath(cfg)); len(got) != 3 {
		t.Fatalf("the delta drain must still leave a primed baseline, got %v", got)
	}
}

// ---------------------------------------------------------------------------
// THE DRAIN HANGS OFF THE CONNECT, AND OFF NOTHING ELSE (owner, 2026-09-02:
// 「啟動的時候好像不用做，就連上 SSE 的時候統一做就好」).
// ---------------------------------------------------------------------------

// A listener whose API answers but whose STREAM will not open must drain no chat
// at all. That state is broken, and an inbox printed into a session that is about
// to receive no events is not a rescue — it makes a deaf machine look like a
// working one.
//
// 🔴 THIS IS THE GUARD FOR THE REMOVAL, AND IT IS OBSERVED POSITIVELY IN BOTH
// ARMS. Nothing else in this file can see a re-added boot drain: every other
// run()-level test lets the stream open, and a boot drain there is invisible
// because the connect drain that follows finds the same window already covered
// and prints nothing. Here the stream NEVER opens, so the boot drain is the only
// thing that could act — and each arm names a fact it would leave behind rather
// than asking for silence alone (a message printed / a baseline file created),
// so the failure reports as a concrete difference and not as a timeout.
func TestListenerRun_APIUpButStreamNeverOpens_DrainsNoChat(t *testing.T) {
	for _, tc := range []struct{ name, baseline string }{
		{"primed-inbox-is-not-printed", `["m1"]`}, // a boot drain would print m2
		{"virgin-home-is-not-baselined", ""},      // a boot drain would create the file
	} {
		t.Run(tc.name, func(t *testing.T) {
			home := t.TempDir()
			var chatHits, dials int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if strings.HasPrefix(r.URL.Path, "/api/chat") {
					atomic.AddInt32(&chatHits, 1)
					w.WriteHeader(200)
					_, _ = w.Write([]byte(msgsJSON("m1", "m2")))
					return
				}
				if !strings.HasPrefix(r.URL.Path, eventsPath) {
					w.WriteHeader(404)
					return
				}
				// The stream is the ONLY thing that is down: a 5xx is retried
				// forever, so this listener dials, fails, and never connects.
				atomic.AddInt32(&dials, 1)
				w.WriteHeader(503)
			}))
			defer srv.Close()
			cfg := Config{Base: srv.URL, Token: "tok", ID: "kyle", Home: home}

			seenPath := chatSeenPath(cfg)
			if tc.baseline != "" {
				if err := os.MkdirAll(filepath.Dir(seenPath), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(seenPath, []byte(tc.baseline), 0o644); err != nil {
					t.Fatal(err)
				}
			}

			out := &syncBuf{}
			l := newTestListener(srv, cfg, out)
			l.cursorPath = filepath.Join(home, "cursor")
			l.seen = loadChatSeen(seenPath)
			l.replySeen = loadReplyCardSeen(filepath.Join(home, "replycards-seen"))

			ctx, cancel := context.WithCancel(context.Background())
			done := make(chan int, 1)
			go func() { done <- l.run(ctx) }()
			waitForCond(t, func() bool { return atomic.LoadInt32(&dials) >= 3 },
				"the listener dialled 3 times and never got a stream")
			cancel()
			<-done

			if n := atomic.LoadInt32(&chatHits); n != 0 {
				t.Fatalf("/api/chat was refetched %d times without a single open stream, "+
					"want 0 — the chat drain hangs off the connect, not off process start", n)
			}
			if strings.Contains(out.String(), "chat from boss") {
				t.Fatalf("a listener that never connected printed chat; out = %q", out.String())
			}
			if tc.baseline == "" {
				if _, err := os.Stat(seenPath); !os.IsNotExist(err) {
					t.Fatalf("a listener that never connected wrote a chat baseline (%v) — "+
						"the silent baseline belongs to the first CONNECT, and writing it "+
						"here would mark this member's whole inbox read in a session that "+
						"never received a single event", err)
				}
				return
			}
			if got := readSeenFile(t, seenPath); len(got) != 1 || got[0] != "m1" {
				t.Fatalf("the seeded baseline moved to %v without a single open stream", got)
			}
		})
	}
}
