package main

// api_chat_reply_to_chat_t4e95_test.go — `reply_to_chat` (T-4e95, owner ruling
// 2026-08-21): the quoted message, shipped WITH the reply, on every read.
//
// WHAT THIS REPLACED, AND WHY THE TESTS LOOK LIKE THIS
// The wire used to carry the quoted message's ID and nothing else, and the
// browser went and fetched the rest when it did not already have it. That fetch
// could fail; a failed fetch was rendered as a placeholder that was sometimes a
// lie; the lie was repaid on the next inbound event. Three behaviours, all of
// which draw the SAME PIXELS whether they are right or wrong — which is why
// twenty rounds of review kept finding new holes in them and no test could see
// any of it.
//
// The replacement has one behaviour, so these tests are about proving there is
// no second one:
//
//	① a reply into ANOTHER conversation carries its quote (the door the owner
//	   opened, and the quote crossing it)
//	② the quote is built on EVERY read door — listing, history page, by-ids,
//	   POST echo, wake snapshot — because a door that forgot would look exactly
//	   like a message whose original is gone
//	③ it is built even when the quoted message is RIGHT THERE in the same
//	   payload: the "already loaded, skip it" optimisation must not exist
//	④ a target that cannot be read leaves the quote ABSENT while `reply_to`
//	   stays — a settled state, not an error and not a retry
//	⑤ the SERVER decides how much of the body is a quote, and collapses it to
//	   one line
//	⑥ an attachment-only original quotes as "" — legal, not a failure

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// replyQuote is the decoded `reply_to_chat` of one message, plus the `reply_to`
// beside it — the two are only meaningful together and every assertion here
// reads both.
type replyQuoteView struct {
	ID          string             `json:"id"`
	ReplyTo     string             `json:"reply_to"`
	ReplyToChat *chatReplyQuoteDTO `json:"reply_to_chat"`
}

// decodeReplyQuote reads one message object out of a raw JSON body.
func decodeReplyQuote(t *testing.T, raw string) replyQuoteView {
	t.Helper()
	var v replyQuoteView
	if err := json.Unmarshal([]byte(raw), &v); err != nil {
		t.Fatalf("decode message: %v — %s", err, raw)
	}
	return v
}

// decodeReplyQuotes reads a message ARRAY and returns it keyed by id.
func decodeReplyQuotes(t *testing.T, raw string) map[string]replyQuoteView {
	t.Helper()
	var rows []replyQuoteView
	if err := json.Unmarshal([]byte(raw), &rows); err != nil {
		t.Fatalf("decode message list: %v — %s", err, raw)
	}
	out := map[string]replyQuoteView{}
	for _, r := range rows {
		out[r.ID] = r
	}
	return out
}

// ── ① the quote crosses the conversation boundary ────────────────────────────
//
// The companion of TestChatReplyTo_QuotingAnotherConversationIsAccepted, which
// pins that the POST is allowed. This pins the half that makes it USEFUL: the
// owner steps into two other members' thread by quoting a line out of it, and
// what comes back has to actually say what that line was. A server that stored
// the link and then declined to resolve a foreign target would leave the owner
// holding a reply that points at something they cannot see.
func TestReplyToChat_CrossesTheConversationBoundary(t *testing.T) {
	srv, secret, db := newWiredTestServerWithDB(t)
	tok, _ := mintJWT("mira", "agent", 300, secret, time.Now().Unix(), "")

	if err := NewDAL(db).PutChat(ChatMessage{
		ID: "c-bystanders", Sender: "m-1", Recipient: "m-2",
		Body: "我覺得那個 leak 在 warden", TS: 1.0,
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	status, raw := postedChat(t, srv.URL, tok,
		`{"to":"owner","body":"這句可以展開講嗎","reply_to":"c-bystanders"}`)
	if status != 200 {
		t.Fatalf("cross-conversation reply: %d %s", status, raw)
	}
	got := decodeReplyQuote(t, raw)
	if got.ReplyToChat == nil {
		t.Fatalf("the quote must ride along even when the original belongs to a "+
			"conversation the replier is in neither end of: %s", raw)
	}
	if got.ReplyToChat.ID != "c-bystanders" || got.ReplyToChat.From != "m-1" {
		t.Fatalf("the quote must name the original and its sender, got %+v",
			*got.ReplyToChat)
	}
	if got.ReplyToChat.Content != "我覺得那個 leak 在 warden" {
		t.Fatalf("the quote must carry what was said, got %q", got.ReplyToChat.Content)
	}
}

// ── ② every read door builds it ──────────────────────────────────────────────
//
// 🔴 THIS IS THE "一律組出" TEST and it is deliberately one test over four
// doors rather than four tests over one door each. The failure it exists to
// catch is a door someone forgot — and a per-door test file makes exactly that
// failure invisible, because the forgotten door has no file. Listing every door
// in one table means adding a fifth door without adding a row here is the only
// way to escape it, and that is a visible omission in a diff.
//
// MUTANT: move the `dto.ReplyToChat = …` line out of servedChatMessageDTO into
// any single handler — this test goes red on the three it is no longer on.
func TestReplyToChat_IsBuiltOnEveryReadDoor(t *testing.T) {
	srv, secret, db := newWiredTestServerWithDB(t)
	tok, _ := mintJWT("mira", "agent", 300, secret, time.Now().Unix(), "")

	if err := NewDAL(db).PutChat(ChatMessage{
		ID: "c-target", Sender: "owner", Recipient: "mira",
		Body: "這個你先做", TS: 1.0,
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	// The POST echo is the first door, and it is answered by the same call that
	// creates the row every other door then reads.
	status, posted := postedChat(t, srv.URL, tok,
		`{"to":"owner","body":"好，我接","reply_to":"c-target"}`)
	if status != 200 {
		t.Fatalf("reply post: %d %s", status, posted)
	}
	replyID := decodeReplyQuote(t, posted).ID

	doors := []struct {
		name string
		read func() map[string]replyQuoteView
	}{
		{"POST echo", func() map[string]replyQuoteView {
			v := decodeReplyQuote(t, posted)
			return map[string]replyQuoteView{v.ID: v}
		}},
		{"GET /api/chat?with=", func() map[string]replyQuoteView {
			_, raw := doRaw(t, "GET", srv.URL+"/api/chat?with=owner", tok, "", nil)
			return decodeReplyQuotes(t, raw)
		}},
		{"GET /api/chat?ids=", func() map[string]replyQuoteView {
			_, raw := doRaw(t, "GET", srv.URL+"/api/chat?ids="+replyID, tok, "", nil)
			return decodeReplyQuotes(t, raw)
		}},
		{"GET /api/chat?before_ts= (history page)", func() map[string]replyQuoteView {
			// A cursor far in the future, so the page is everything.
			_, raw := doRaw(t, "GET",
				srv.URL+"/api/chat?with=owner&before_ts=99999999999&before_id=zzzz",
				tok, "", nil)
			return decodeReplyQuotes(t, raw)
		}},
	}

	for _, door := range doors {
		rows := door.read()
		row, ok := rows[replyID]
		if !ok {
			t.Fatalf("%s did not carry the reply at all (%d rows)", door.name, len(rows))
		}
		if row.ReplyTo != "c-target" {
			t.Fatalf("%s: reply_to must survive, got %q", door.name, row.ReplyTo)
		}
		if row.ReplyToChat == nil {
			t.Fatalf("%s: reply_to_chat must be built HERE too — a door that "+
				"skips it is indistinguishable on screen from an original that "+
				"is really gone", door.name)
		}
		if row.ReplyToChat.Content != "這個你先做" {
			t.Fatalf("%s: the quote must carry the original body, got %q",
				door.name, row.ReplyToChat.Content)
		}
	}
}

// ── ② (cont.) the wake snapshot is a read door too ───────────────────────────
//
// Separate test because it is a separate handler with its OWN projection
// (resumeChatMessageDTO), which is exactly the shape of bug the r18 review found
// for `reply_to` itself: the REST path was guarded, the wake path shared a
// helper with it and LOOKED guarded, and a mutant in the wake projection alone
// left the whole package green.
//
// It also pins the thing only this door has: the snapshot resolves DISPLAY
// NAMES, so the quote carries `from_name` here and "" everywhere else.
//
// MUTANT: delete the `d.ReplyToChat = s.chatReplyQuote(…)` line from
// resumeChatMessageDTO — only this test goes red.
func TestReplyToChat_IsBuiltInTheWakeSnapshot(t *testing.T) {
	api := resumeCtxServer(t)

	putChat(t, api, "c-asked", "m-peer", "m-exec", "要出還是等？", 100, nil)
	putChat(t, api, "c-answer", "m-exec", "m-peer", "等，我還在追一個 leak", 101,
		map[string]any{chatReplyToMetaKey: "c-asked"})

	snap := resumeSnapshot(t, api, "m-exec")
	answer := chatByID(t, snap, "c-answer")
	if answer.ReplyToChat == nil {
		t.Fatalf("a waking agent must read WHAT the reply answered, not just " +
			"that it answered something — reply_to_chat is absent")
	}
	if answer.ReplyToChat.Content != "要出還是等？" {
		t.Fatalf("the quote must carry the original body, got %q",
			answer.ReplyToChat.Content)
	}
	// The name half — this is the ONE read that resolves display names, and the
	// quote follows chatMessageDTO's own convention on the same payload.
	if answer.ReplyToChat.FromName != "小佩" {
		t.Fatalf("on the read that resolves names, the quote must carry the "+
			"sender's name beside the id, got from_name=%q from=%q",
			answer.ReplyToChat.FromName, answer.ReplyToChat.From)
	}
	// A message that answers nothing claims no quote — without this half a
	// projection that stamped every row would pass.
	if asked := chatByID(t, snap, "c-asked"); asked.ReplyToChat != nil {
		t.Fatalf("a message that replies to nothing must carry no quote, got %+v",
			*asked.ReplyToChat)
	}

	// …and it is on the WIRE under its documented name. The struct assertions
	// above survive a field renamed in the JSON tag.
	raw := resumeSnapshotRaw(t, api, "m-exec")
	if !strings.Contains(raw, `"reply_to_chat"`) {
		t.Fatalf("reply_to_chat must be on the wire under that name: %s", raw)
	}
}

// ── ③ no "it is already in this payload" optimisation ────────────────────────
//
// 🔴 THE POINT OF THIS TEST IS THAT IT LOOKS REDUNDANT. Both messages are in
// the same listing, so a reader could resolve the quote itself, and skipping the
// build here would save a query and change nothing a user can see — today. It is
// forbidden anyway, and the owner ruled it so: an optimisation that fires
// SOMETIMES means the client needs a fallback for when it does not, and that
// fallback is the entire machine this redesign deleted.
//
// MUTANT: return nil from chatReplyQuote when the target is in the same batch —
// this test goes red and, by construction, nothing else in the package does.
func TestReplyToChat_IsBuiltEvenWhenTheOriginalIsInTheSamePayload(t *testing.T) {
	srv, secret, db := newWiredTestServerWithDB(t)
	tok, _ := mintJWT("mira", "agent", 300, secret, time.Now().Unix(), "")

	if err := NewDAL(db).PutChat(ChatMessage{
		ID: "c-right-there", Sender: "owner", Recipient: "mira",
		Body: "one row above the reply", TS: 1.0,
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	status, posted := postedChat(t, srv.URL, tok,
		`{"to":"owner","body":"answering it","reply_to":"c-right-there"}`)
	if status != 200 {
		t.Fatalf("reply post: %d %s", status, posted)
	}
	replyID := decodeReplyQuote(t, posted).ID

	_, raw := doRaw(t, "GET", srv.URL+"/api/chat?with=owner", tok, "", nil)
	rows := decodeReplyQuotes(t, raw)
	if _, ok := rows["c-right-there"]; !ok {
		t.Fatalf("precondition: the original must be in the same payload — %s", raw)
	}
	row := rows[replyID]
	if row.ReplyToChat == nil {
		t.Fatalf("the quote must be built even though the original is right " +
			"there in the same response — a conditional build is a second code " +
			"path with no visible difference when it is wrong")
	}
	if row.ReplyToChat.Content != "one row above the reply" {
		t.Fatalf("got quote content %q", row.ReplyToChat.Content)
	}
}

// ── ④ a target that cannot be read: absent quote, surviving link ─────────────
//
// The state a real station reaches when the quoted message is cleared or the
// member that held it is gone. The link is stamped straight into meta here
// rather than posted, because the POST door refuses an unknown target on
// purpose (that refusal is guarded in api_chat_reply_to_t4e95_test.go ②) — this
// is about a link that WAS valid when it was made.
//
// Three assertions, and all three are load-bearing: the message is still served
// (a missing original must not take the conversation down), `reply_to` is still
// on it (so a reader can say "this was a reply and its original is gone" rather
// than silently drawing an ordinary message), and the quote is absent rather
// than fabricated or blank-but-present.
//
// MUTANT: make chatReplyQuote return an empty &chatReplyQuoteDTO{} instead of
// nil when the target misses — this test goes red on the absence assertion.
func TestReplyToChat_AbsentWhenTheOriginalIsGone(t *testing.T) {
	srv, secret, db := newWiredTestServerWithDB(t)
	tok, _ := mintJWT("mira", "agent", 300, secret, time.Now().Unix(), "")

	if err := NewDAL(db).PutChat(ChatMessage{
		ID: "c-orphaned-reply", Sender: "mira", Recipient: "owner",
		Body: "answering something that is no longer here", TS: 5.0,
		Meta: map[string]any{chatReplyToMetaKey: "c-longgone"},
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	status, raw := doRaw(t, "GET", srv.URL+"/api/chat?with=owner", tok, "", nil)
	if status != 200 {
		t.Fatalf("a missing quote target must not fail the whole read: %d %s",
			status, raw)
	}
	row, ok := decodeReplyQuotes(t, raw)["c-orphaned-reply"]
	if !ok {
		t.Fatalf("the message itself must still be served: %s", raw)
	}
	if row.ReplyTo != "c-longgone" {
		t.Fatalf("reply_to must survive its target — it is what lets a reader "+
			"say 'this was a reply' at all, got %q", row.ReplyTo)
	}
	if row.ReplyToChat != nil {
		t.Fatalf("a target that cannot be read must leave the quote ABSENT, "+
			"not present-and-empty: %+v", *row.ReplyToChat)
	}
}

// ── ⑤ the SERVER decides how long a quote is, and flattens it ────────────────
//
// Both halves in one test because they are one decision: what a quote LINE is.
// The length used to live in the browser (ChatArea's QUOTE_EXCERPT_CHARS) and
// the wire carried the whole body, so every client shortened it itself — two
// copies of a display rule, neither of them wrong when they disagreed.
//
// The exact cut is asserted by RUNE COUNT rather than by a literal string, so
// this stays true for a body that is not ASCII — and the body here is not.
//
// 🔴 THE LENGTH IS WRITTEN OUT HERE AS A LITERAL, and the first version of this
// test did not do that — it compared against `chatReplyQuoteMaxChars + 1`. That
// assertion MOVES WITH THE THING IT CHECKS: a mutant changing the constant from
// 60 to 40 changed both sides of the comparison and the whole package stayed
// green (measured, not feared). A wire promise about a fixed length cannot be
// guarded by a test that reads the length off the wire's own definition.
//
// MUTANT: change chatReplyQuoteMaxChars, or replace the strings.Fields collapse
// with strings.TrimSpace — this test goes red on the count / on the newline
// respectively.
func TestReplyToChat_ContentIsShortenedAndFlattenedByTheServer(t *testing.T) {
	srv, secret, db := newWiredTestServerWithDB(t)
	tok, _ := mintJWT("mira", "agent", 300, secret, time.Now().Unix(), "")

	// 92 runes of CJK across three lines, with a run of spaces in the middle.
	//
	// 🔴 THE BLANK LINE IS EARLY ON PURPOSE. It has to survive INTO the 60 runes
	// that are kept, or the collapse assertion below is satisfied by the cut
	// rather than by the collapse — measured: with the newline at rune 90 a
	// mutant swapping the whitespace collapse for a plain TrimSpace left the
	// whole package green, because everything past rune 60 (newline included)
	// was thrown away either way.
	long := strings.Repeat("長", 10) + "\n\n" + strings.Repeat("話", 30) +
		"   " + strings.Repeat("短", 50)
	if err := NewDAL(db).PutChat(ChatMessage{
		ID: "c-verbose", Sender: "owner", Recipient: "mira", Body: long, TS: 1.0,
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	status, raw := postedChat(t, srv.URL, tok,
		`{"to":"owner","body":"tl;dr?","reply_to":"c-verbose"}`)
	if status != 200 {
		t.Fatalf("reply post: %d %s", status, raw)
	}
	q := decodeReplyQuote(t, raw).ReplyToChat
	if q == nil {
		t.Fatalf("no quote: %s", raw)
	}
	// ① ONE LINE. A quote row is a pointer, not a rendering — a multi-line
	// excerpt would push the reader's layout around for nothing.
	if strings.ContainsAny(q.Content, "\n\r") {
		t.Fatalf("the quote must be collapsed to one line, got %q", q.Content)
	}
	// ② CUT BY THE SERVER, at 60 runes. The 60 is spelled out — see the note
	// above about why reading chatReplyQuoteMaxChars here guards nothing.
	const wantQuoteRunes = 60
	if chatReplyQuoteMaxChars != wantQuoteRunes {
		t.Fatalf("the quote length is a WIRE PROMISE (%d runes) and changing it "+
			"changes what every reader draws — if that is deliberate, change it "+
			"here too, on purpose. got chatReplyQuoteMaxChars=%d",
			wantQuoteRunes, chatReplyQuoteMaxChars)
	}
	runes := []rune(q.Content)
	if len(runes) != wantQuoteRunes+1 { // + the ellipsis standing in for the cut
		t.Fatalf("the server must cut the quote to %d runes + an ellipsis, got "+
			"%d runes (%q)", wantQuoteRunes, len(runes), q.Content)
	}
	if runes[len(runes)-1] != '…' {
		t.Fatalf("a cut quote must say it was cut, got %q", q.Content)
	}
	// ③ …and the whole body is NOT on the wire. Without this, a server that
	// shipped everything and let the client trim would still pass ① and ② if it
	// also happened to ship a trimmed copy. The tail run is what to look for:
	// it is past the cut, so its presence anywhere in the response means the
	// untrimmed body rode along.
	if strings.Contains(raw, strings.Repeat("短", 50)) {
		t.Fatalf("the untrimmed body must not ride along: %s", raw)
	}
}

// ── ⑥ an attachment-only original quotes as "" ───────────────────────────────
//
// "" is an ORDINARY value here, not a failure and not a placeholder for one —
// the way a missing original is said is the absence of the whole object (④).
// Pinned because the obvious "helpful" change is to substitute some 「（附件）」
// text server-side, which would make an empty quote and a missing quote look
// alike again.
func TestReplyToChat_AnAttachmentOnlyOriginalQuotesAsEmpty(t *testing.T) {
	srv, secret, db := newWiredTestServerWithDB(t)
	tok, _ := mintJWT("mira", "agent", 300, secret, time.Now().Unix(), "")

	if err := NewDAL(db).PutChat(ChatMessage{
		ID: "c-photo", Sender: "owner", Recipient: "mira", Body: "", TS: 1.0,
		Meta: map[string]any{"attachments": []any{
			map[string]any{"id": "a-1", "mime": "image/png", "filename": "x.png"},
		}},
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	status, raw := postedChat(t, srv.URL, tok,
		`{"to":"owner","body":"這張是哪來的","reply_to":"c-photo"}`)
	if status != 200 {
		t.Fatalf("reply post: %d %s", status, raw)
	}
	q := decodeReplyQuote(t, raw).ReplyToChat
	if q == nil {
		t.Fatalf("a text-less original is still an original — the quote must be " +
			"PRESENT with an empty content, because absence means 'gone'")
	}
	if q.Content != "" {
		t.Fatalf("nothing may be invented for a body-less message, got %q", q.Content)
	}
	if q.ID != "c-photo" || q.From != "owner" {
		t.Fatalf("the quote must still name the original and its sender, got %+v", *q)
	}
}
