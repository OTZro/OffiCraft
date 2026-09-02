package main

// dal_chat_pushdown_t48_test.go — T-48 EQUIVALENCE HARNESS.
//
// Two whole-table reads moved into SQL: the cursorless chat page
// (ListChat + filter + slice in Go → ListChatLatest) and the unread fold
// (ListChat + ListChatReads + UnreadCounts in Go → UnreadCountsFor).
//
// 🔴 EQUIVALENCE IS THE REQUIREMENT, not "looks right". Each test keeps the OLD
// implementation alive as a reference function in this file and drives BOTH
// over the same fixtures, comparing the full result (order, content, count) —
// a hand-written expectation would only pin what the author already believed.

import (
	"os"
	"strings"
	"testing"
)

// listChatLatestReference is VERBATIM the pre-T-48 handler path: read the whole
// table oldest→newest, filter to the participant, filter to the caller, then
// keep the last `limit`. It is the oracle, so it must not be "improved".
func listChatLatestReference(t *testing.T, d *DAL, participant, caller string, limit int) []ChatMessage {
	t.Helper()
	msgs, err := d.ListChat()
	if err != nil {
		t.Fatalf("ListChat: %v", err)
	}
	if participant != "" {
		filtered := msgs[:0]
		for _, m := range msgs {
			if m.Sender == participant || m.Recipient == participant {
				filtered = append(filtered, m)
			}
		}
		msgs = filtered
	}
	if caller != "" {
		filtered := msgs[:0]
		for _, m := range msgs {
			if m.Sender == caller || m.Recipient == caller {
				filtered = append(filtered, m)
			}
		}
		msgs = filtered
	}
	if limit >= 0 {
		if limit == 0 {
			msgs = nil
		} else if len(msgs) > limit {
			msgs = msgs[len(msgs)-limit:]
		}
	}
	return msgs
}

// seedChatPushdownFixture builds a stream with everything the two paths can
// disagree about: several conversation lines, both directions, third-party
// traffic the caller is not in, EQUAL timestamps whose only tie-break is id
// (both orders of insertion), and an id whose lexical order fights its ts.
func seedChatPushdownFixture(t *testing.T, d *DAL) {
	t.Helper()
	msgs := []ChatMessage{
		{ID: "m01", Sender: "m-1", Recipient: "owner", TS: 1},
		{ID: "m02", Sender: "owner", Recipient: "m-1", TS: 2},
		{ID: "m03", Sender: "m-2", Recipient: "owner", TS: 3},
		{ID: "m04", Sender: "m-1", Recipient: "m-2", TS: 4}, // owner not involved
		{ID: "m05", Sender: "m-1", Recipient: "owner", TS: 5},
		// equal ts, ids out of insertion order — the (ts, id) tie-break.
		{ID: "m07", Sender: "m-1", Recipient: "owner", TS: 6},
		{ID: "m06", Sender: "owner", Recipient: "m-1", TS: 6},
		{ID: "m08", Sender: "m-2", Recipient: "m-1", TS: 6}, // equal ts, third line
		{ID: "a99", Sender: "m-1", Recipient: "owner", TS: 7}, // id sorts before m*
		{ID: "m09", Sender: "m-3", Recipient: "owner", TS: 8},
		{ID: "m10", Sender: "owner", Recipient: "m-3", TS: 9},
		{ID: "m11", Sender: "m-1", Recipient: "owner", TS: 10},
	}
	for _, m := range msgs {
		if err := d.PutChat(m); err != nil {
			t.Fatalf("put %s: %v", m.ID, err)
		}
	}
}

func sameChatRows(a, b []ChatMessage) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].ID != b[i].ID || a[i].Sender != b[i].Sender ||
			a[i].Recipient != b[i].Recipient || a[i].Body != b[i].Body ||
			a[i].TS != b[i].TS {
			return false
		}
	}
	return true
}

func chatRowIDs(msgs []ChatMessage) []string {
	out := make([]string, len(msgs))
	for i, m := range msgs {
		out[i] = m.ID
	}
	return out
}

func TestListChatLatestMatchesTheGoFilterItReplaced(t *testing.T) {
	d := newTestDAL(t)
	seedChatPushdownFixture(t, d)

	participants := []string{"", "owner", "m-1", "m-2", "m-3", "nobody"}
	callers := []string{"", "owner", "m-1", "m-2", "nobody"}
	limits := []int{-1, 0, 1, 2, 3, 5, 12, 30, 100}

	cases := 0
	for _, p := range participants {
		for _, c := range callers {
			for _, lim := range limits {
				want := listChatLatestReference(t, d, p, c, lim)
				got, err := d.ListChatLatest(p, c, lim)
				if err != nil {
					t.Fatalf("ListChatLatest(%q,%q,%d): %v", p, c, lim, err)
				}
				if !sameChatRows(want, got) {
					t.Fatalf("ListChatLatest(%q,%q,%d) diverged from the Go path:\n old %v\n new %v",
						p, c, lim, chatRowIDs(want), chatRowIDs(got))
				}
				cases++
			}
		}
	}
	if cases != len(participants)*len(callers)*len(limits) {
		t.Fatalf("case count: %d", cases)
	}
	// The oracle must actually SEE rows, or every comparison above is 0 == 0.
	if n := len(listChatLatestReference(t, d, "m-1", "", -1)); n != 9 {
		t.Fatalf("fixture is not exercising the filters: m-1 line has %d rows, want 9", n)
	}
}

func TestListChatLatestOnAnEmptyTable(t *testing.T) {
	d := newTestDAL(t)
	for _, lim := range []int{-1, 0, 1, 30} {
		got, err := d.ListChatLatest("m-1", "", lim)
		if err != nil {
			t.Fatalf("limit %d: %v", lim, err)
		}
		if len(got) != 0 {
			t.Fatalf("limit %d: want no rows, got %v", lim, chatRowIDs(got))
		}
	}
}

// ── unread counts ────────────────────────────────────────────────────────────

func TestUnreadCountsForMatchesTheGoFold(t *testing.T) {
	d := newTestDAL(t)
	seedChatPushdownFixture(t, d)
	// A watermark per shape: none at all (m-3), one BELOW the newest message
	// (m-1, ts 6 — a-99@7 and m11@10 stay unread), one exactly ON a message's
	// ts (m-2, ts 3 — strict > means it counts as read), and one for a peer
	// that never sent anything.
	for _, r := range []ChatRead{
		{ReaderID: "owner", PeerID: "m-1", LastReadTS: 6},
		{ReaderID: "owner", PeerID: "m-2", LastReadTS: 3},
		{ReaderID: "owner", PeerID: "ghost", LastReadTS: 99},
		{ReaderID: "m-1", PeerID: "owner", LastReadTS: 2}, // ANOTHER reader's receipt
	} {
		if _, _, err := d.PutChatRead(r); err != nil {
			t.Fatalf("put read: %v", err)
		}
	}

	for _, reader := range []string{"owner", "m-1", "m-2", "m-3", "nobody"} {
		messages, err := d.ListChat()
		if err != nil {
			t.Fatalf("ListChat: %v", err)
		}
		receipts, err := d.ListChatReads(reader, "")
		if err != nil {
			t.Fatalf("ListChatReads: %v", err)
		}
		want := UnreadCounts(messages, receipts, reader)
		got, err := d.UnreadCountsFor(reader)
		if err != nil {
			t.Fatalf("UnreadCountsFor(%q): %v", reader, err)
		}
		if len(want) != len(got) {
			t.Fatalf("UnreadCountsFor(%q): want %v, got %v", reader, want, got)
		}
		for peer, n := range want {
			if got[peer] != n {
				t.Fatalf("UnreadCountsFor(%q)[%q]: want %d, got %d (full: %v vs %v)",
					reader, peer, n, got[peer], want, got)
			}
		}
	}

	// The oracle must be non-trivial: owner really does have unread, and the
	// on-the-ts watermark really did clear m-2's only message.
	own, err := d.UnreadCountsFor("owner")
	if err != nil {
		t.Fatalf("owner: %v", err)
	}
	if own["m-1"] != 2 {
		t.Fatalf("fixture: owner should have 2 unread from m-1, got %d (%v)", own["m-1"], own)
	}
	if _, present := own["m-2"]; present {
		t.Fatalf("a watermark ON a message's ts must clear it (strict >), got %v", own)
	}
	if own["m-3"] != 1 {
		t.Fatalf("a peer with NO receipt must count every addressed message, got %v", own)
	}
}

// 🔴 THE "只改一處" GUARD. The unread aggregate had TWO copies of the same
// whole-table read (unreadCountsForRequest in api_helpers.go and
// HandleChatUnreadCountApiChatUnreadCountGet in api_chat.go). Porting one and
// leaving the other keeps the full-table read alive with every behavioural test
// still green — nothing observable changes. So this reads the SOURCE and pins
// that neither call site pulls the whole chat table any more.
func TestBothUnreadCallSitesUseTheSQLAggregate(t *testing.T) {
	for _, site := range []struct{ file, fn string }{
		{"api_helpers.go", "func (s *apiServer) unreadCountsForRequest("},
		{"api_chat.go", "func (s *apiServer) HandleChatUnreadCountApiChatUnreadCountGet("},
	} {
		src, err := os.ReadFile(site.file)
		if err != nil {
			t.Fatalf("read %s: %v", site.file, err)
		}
		start := strings.Index(string(src), site.fn)
		if start < 0 {
			t.Fatalf("%s: cannot find %s — the guard has gone stale, fix the anchor", site.file, site.fn)
		}
		body := string(src)[start:]
		if end := strings.Index(body, "\n}\n"); end > 0 {
			body = body[:end]
		}
		if strings.Contains(body, "s.dal.ListChat()") {
			t.Fatalf("%s: %s still reads the WHOLE chat table (s.dal.ListChat()); "+
				"both unread call sites must go through s.dal.UnreadCountsFor (T-48)", site.file, site.fn)
		}
		if !strings.Contains(body, "UnreadCountsFor(") {
			t.Fatalf("%s: %s does not call s.dal.UnreadCountsFor — the SQL aggregate "+
				"must be the ONE way both sites count unread (T-48)", site.file, site.fn)
		}
	}
}

// The same guard for the cursorless chat page: the handler must not pull the
// whole table to serve at most `limit` rows.
func TestChatListingDoesNotReadTheWholeTable(t *testing.T) {
	src, err := os.ReadFile("api_chat.go")
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	start := strings.Index(string(src), "func (s *apiServer) HandleListChatApiChatGet(")
	if start < 0 {
		t.Fatal("cannot find HandleListChatApiChatGet — the guard has gone stale")
	}
	body := string(src)[start:]
	if end := strings.Index(body, "\n}\n"); end > 0 {
		body = body[:end]
	}
	if strings.Contains(body, "s.dal.ListChat()") {
		t.Fatal("HandleListChatApiChatGet reads the WHOLE chat_message table again; " +
			"the cursorless page is s.dal.ListChatLatest (T-48)")
	}
	if strings.Contains(body, "s.dal.PutChatRead(") {
		t.Fatal("HandleListChatApiChatGet writes a read receipt again — GET /api/chat " +
			"has no watermark side effect (T-48, c-d1eea83e57d1)")
	}
}
