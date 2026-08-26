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
	list atomic.Value // string
	hits int32
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
		w.WriteHeader(404)
	}))
	t.Cleanup(m.Server.Close)
	return m
}

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

// FIRST RUN on this machine: no state file ⇒ the boot drain must stay SILENT and
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
	for _, want := range []string{"chat from boss (#m3): body-m3", "chat from boss (#m4): body-m4"} {
		if !strings.Contains(got, want) {
			t.Fatalf("cold-start backfill missing %q; out = %q", want, got)
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
// times drains chat exactly ONCE — the boot drain sits before the connect loop,
// so no reconnect can re-print history.
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
	// And chat was refetched exactly once (no delta frames were ever sent).
	if h := atomic.LoadInt32(&chatHits); h != 1 {
		t.Fatalf("/api/chat refetched %d times, want 1 (boot drain only)", h)
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
		"the listener connected (so the boot drain has certainly run)")
	cancel()
	<-done

	if strings.Contains(out.String(), "chat from boss") {
		t.Fatalf("first-ever boot must print NO chat history; out = %q", out.String())
	}
	if got := readSeenFile(t, chatSeenPath(cfg)); len(got) != 3 {
		t.Fatalf("first-ever boot must leave a primed baseline, got %v", got)
	}
}
