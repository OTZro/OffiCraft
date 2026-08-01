package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// GET /api/members/{id} and GET /api/members serve the SAME declared field
// (MemberDTO.unread_count), so they must serve the same NUMBER. The single-member
// handler used to hand newMemberDTO a literal 0 while the list handler inverted
// the caller's chat_read watermark — and because the cockpit re-reads ONE member
// when a chat delta names it (T-8115), that placeholder made the roster badge a
// one-way ratchet: it could clear (0 happens to be right there) but never rise.
// A new message therefore announced a badge that the refetch immediately erased.
//
// These assertions read the NUMBER OUT OF THE RESPONSE BODY. Asserting that some
// computation was invoked would pass against a handler that computes the count
// and then serves a constant anyway, which is exactly the shape of the bug.
//
// MUTANT: put `0` back as newMemberDTO's last argument in
// HandleGetMemberApiMembersMemberIdGet and every sub-case below goes red.
func TestGetMemberServesTheSameUnreadCountAsTheList(t *testing.T) {
	s := newReconcileTestServer(t)
	peer := testAgent("m-chatty")
	putTestMember(t, s, peer)
	other := testAgent("m-quiet")
	putTestMember(t, s, other)

	getUnread := func(t *testing.T, id string) int {
		t.Helper()
		rec := httptest.NewRecorder()
		s.HandleGetMemberApiMembersMemberIdGet(rec,
			taskReq(t, "GET", "/api/members/"+id, nil, wireOwnerID, "owner"), id)
		if rec.Code != http.StatusOK {
			t.Fatalf("get member %s: %d %s", id, rec.Code, rec.Body.String())
		}
		var dto memberDTO
		if err := json.Unmarshal(rec.Body.Bytes(), &dto); err != nil {
			t.Fatalf("decode member %s: %v (body %s)", id, err, rec.Body.String())
		}
		return dto.UnreadCount
	}
	listUnread := func(t *testing.T, id string) int {
		t.Helper()
		rec := httptest.NewRecorder()
		s.HandleListMembersApiMembersGet(rec,
			taskReq(t, "GET", "/api/members", nil, wireOwnerID, "owner"),
			HandleListMembersApiMembersGetParams{})
		if rec.Code != http.StatusOK {
			t.Fatalf("list members: %d %s", rec.Code, rec.Body.String())
		}
		var rows []memberDTO
		if err := json.Unmarshal(rec.Body.Bytes(), &rows); err != nil {
			t.Fatalf("decode roster: %v", err)
		}
		for _, row := range rows {
			if row.ID == id {
				return row.UnreadCount
			}
		}
		t.Fatalf("member %s not in roster (%d rows)", id, len(rows))
		return -1
	}
	// The contract is PARITY, so every stage checks both endpoints against each
	// other AND against the number we can derive by hand.
	bothMustBe := func(t *testing.T, stage, id string, want int) {
		t.Helper()
		single, list := getUnread(t, id), listUnread(t, id)
		if single != want {
			t.Errorf("%s: GET /api/members/%s served unread_count %d, want %d",
				stage, id, single, want)
		}
		if single != list {
			t.Errorf("%s: single-item (%d) and list (%d) disagree on %s's unread_count "+
				"— same declared field, so this must never happen",
				stage, single, list, id)
		}
	}

	// (1) No chat at all: 0 is the honest answer on both, and it is the ONE value a
	// placeholder also gets right — which is why the later stages exist.
	bothMustBe(t, "no chat", peer.ID, 0)

	// (2) Two peer→owner lines above the (absent) watermark count. An owner→peer
	// send and a peer→somebody-else line must not.
	for i, m := range []ChatMessage{
		{ID: "cu-1", Sender: peer.ID, Recipient: wireOwnerID, Body: "第一則", TS: 3000},
		{ID: "cu-2", Sender: peer.ID, Recipient: wireOwnerID, Body: "第二則", TS: 3001},
		{ID: "cu-3", Sender: wireOwnerID, Recipient: peer.ID, Body: "收到", TS: 3002},
		{ID: "cu-4", Sender: peer.ID, Recipient: other.ID, Body: "同步", TS: 3003},
	} {
		if err := s.dal.PutChat(m); err != nil {
			t.Fatalf("put chat %d: %v", i, err)
		}
	}
	// 🔴 THE REGRESSION: a placeholder 0 serves 0 here while the roster serves 2.
	bothMustBe(t, "two unread", peer.ID, 2)
	// The quiet member stays at 0 — the count is per-peer, not a global total.
	bothMustBe(t, "unrelated member", other.ID, 0)

	// (3) Move the owner's watermark past the FIRST line only. Both endpoints must
	// drop to 1. A handler that returned a constant 2 (or a cached map) passes (2)
	// and fails here, so this is what proves the number is really being computed.
	if _, _, err := s.dal.PutChatRead(ChatRead{
		ReaderID: wireOwnerID, PeerID: peer.ID, LastReadTS: 3000,
	}); err != nil {
		t.Fatalf("put chat read: %v", err)
	}
	bothMustBe(t, "watermark past the first line", peer.ID, 1)

	// (4) Read to the end ⇒ both clear.
	if _, _, err := s.dal.PutChatRead(ChatRead{
		ReaderID: wireOwnerID, PeerID: peer.ID, LastReadTS: 3001,
	}); err != nil {
		t.Fatalf("put chat read: %v", err)
	}
	bothMustBe(t, "fully read", peer.ID, 0)
}

// The unread count is the CALLER's, never the owner's-by-default: a member asking
// about itself must not be handed the owner's badge. Same handler, different
// token — this is the assertion that keeps `unreadCountsForRequest` reading
// currentActor(r) rather than wireOwnerID.
//
// MUTANT: hardcode wireOwnerID as the reader inside unreadCountsForRequest and
// this goes red while the test above stays green.
func TestGetMemberUnreadCountIsPerCaller(t *testing.T) {
	s := newReconcileTestServer(t)
	peer := testAgent("m-writer")
	putTestMember(t, s, peer)

	// One line addressed to the OWNER. For the owner that is 1 unread; for the
	// member itself (who is not the recipient) it is 0.
	if err := s.dal.PutChat(ChatMessage{
		ID: "cu-9", Sender: peer.ID, Recipient: wireOwnerID, Body: "給老闆", TS: 4000,
	}); err != nil {
		t.Fatalf("put chat: %v", err)
	}

	read := func(t *testing.T, sub, scope, id string) int {
		t.Helper()
		rec := httptest.NewRecorder()
		s.HandleGetMemberApiMembersMemberIdGet(rec,
			taskReq(t, "GET", "/api/members/"+id, nil, sub, scope), id)
		if rec.Code != http.StatusOK {
			t.Fatalf("get member as %s: %d %s", sub, rec.Code, rec.Body.String())
		}
		var dto memberDTO
		if err := json.Unmarshal(rec.Body.Bytes(), &dto); err != nil {
			t.Fatalf("decode: %v", err)
		}
		return dto.UnreadCount
	}

	if got := read(t, wireOwnerID, "owner", peer.ID); got != 1 {
		t.Errorf("owner asking about %s: unread_count %d, want 1", peer.ID, got)
	}
	if got := read(t, peer.ID, "agent", peer.ID); got != 0 {
		t.Errorf("%s asking about itself: unread_count %d, want 0 — the count is the "+
			"CALLER's watermark inverse, not the owner's", peer.ID, got)
	}
}
