package main

// api_chat_reply_to_t4e95_test.go — `reply_to` (T-4e95): a message that quotes
// another message.
//
// The owner's ruling shapes every assertion here, so it is worth stating once:
// the link is an ID AND NOTHING ELSE. No sender, no name, no excerpt of the
// quoted message is copied onto the reply, because that text already exists
// under its own id and a copy beside every reply is a second place for the same
// sentence to live. What that leaves the server responsible for is exactly
// three things, one test each:
//
//	① the link ROUND-TRIPS — post it, read it back, it is still there
//	② a link to a message that does not exist is REFUSED
//	③ a link OUT of this conversation is REFUSED — and this is the one that
//	   matters now that a by-ids read reaches every conversation (T-4e95):
//	   without it, a caller could quote a line out of two other members' thread
//	   and carry it into a conversation it was never part of
//
//	④ …plus the one that is not about the caller's honesty but about the
//	   handler's: the meta map is copied through WHOLESALE, so a caller can put
//	   a reply_to there directly. It must be discarded, or every check above is
//	   decoration.

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"
)

// postedChat drives POST /api/chat as `mira` and returns (status, raw body).
func postedChat(t *testing.T, url string, tok string, body string) (int, string) {
	t.Helper()
	return doRaw(t, "POST", url+"/api/chat", tok, "application/json", []byte(body))
}

// chatFields reads the two fields these tests assert on out of a message JSON.
func chatFields(t *testing.T, raw string) (id string, replyTo string) {
	t.Helper()
	var msg struct {
		ID      string `json:"id"`
		ReplyTo string `json:"reply_to"`
	}
	if err := json.Unmarshal([]byte(raw), &msg); err != nil {
		t.Fatalf("decode chat message: %v — %s", err, raw)
	}
	return msg.ID, msg.ReplyTo
}

// ① ROUND TRIP. Posted, stored, served — and served again on a SECOND read, so
// a handler that merely echoed back what it was handed cannot pass.
func TestChatReplyTo_RoundTripsThroughStorage(t *testing.T) {
	srv, secret, _ := newWiredTestServerWithDB(t)
	tok, _ := mintJWT("mira", "agent", 300, secret, time.Now().Unix(), "")

	status, raw := postedChat(t, srv.URL, tok, `{"to":"owner","body":"the question"}`)
	if status != 200 {
		t.Fatalf("seed post: %d %s", status, raw)
	}
	quotedID, quotedReplyTo := chatFields(t, raw)
	if quotedReplyTo != "" {
		t.Fatalf("a plain post must carry no link, got %q", quotedReplyTo)
	}

	status, raw = postedChat(t, srv.URL, tok,
		`{"to":"owner","body":"the answer","reply_to":"`+quotedID+`"}`)
	if status != 200 {
		t.Fatalf("reply post: %d %s", status, raw)
	}
	replyID, replyTo := chatFields(t, raw)
	if replyTo != quotedID {
		t.Fatalf("the POST response must carry the link, got %q want %q", replyTo, quotedID)
	}

	// The decisive half: read it back off the wire. The POST response is built
	// from the in-memory row the handler just made, so it would still look right
	// if the link were never persisted.
	status, listed := doRaw(t, "GET", srv.URL+"/api/chat?ids="+replyID, tok, "", nil)
	if status != 200 {
		t.Fatalf("re-read: %d %s", status, listed)
	}
	if !strings.Contains(listed, `"reply_to":"`+quotedID+`"`) {
		t.Fatalf("the stored link must survive a re-read: %s", listed)
	}
}

// ② A link to nothing. 400 rather than 404 on purpose: what was not found is a
// FIELD OF THIS REQUEST, not the resource the request addresses.
//
// The second half is the load-bearing one — the message must not be stored
// anyway. A refusal that still writes the row is a refusal in name only.
func TestChatReplyTo_UnknownTargetIsRefusedAndNothingIsStored(t *testing.T) {
	srv, secret, _ := newWiredTestServerWithDB(t)
	tok, _ := mintJWT("mira", "agent", 300, secret, time.Now().Unix(), "")

	status, raw := postedChat(t, srv.URL, tok,
		`{"to":"owner","body":"orphan","reply_to":"c-nosuchmessage"}`)
	if status != 400 {
		t.Fatalf("an unknown reply_to must be refused, got %d %s", status, raw)
	}
	if !strings.Contains(raw, "c-nosuchmessage") {
		t.Fatalf("the refusal must name the id it is about: %s", raw)
	}

	_, listed := doRaw(t, "GET", srv.URL+"/api/chat?with=owner", tok, "", nil)
	if strings.Contains(listed, "orphan") {
		t.Fatalf("a refused post must not be stored: %s", listed)
	}
}

// ③ A link OUT of this conversation. Two shapes, and the first is the one a
// naive check misses: a message the caller REALLY IS one end of, but in a
// different thread. "Am I allowed to see it" is not the question — "is it in
// THIS conversation" is.
func TestChatReplyTo_ForeignConversationIsRefused(t *testing.T) {
	srv, secret, db := newWiredTestServerWithDB(t)
	tok, _ := mintJWT("mira", "agent", 300, secret, time.Now().Unix(), "")
	dal := NewDAL(db)

	// (a) mira's OWN message, but to a different peer.
	if err := dal.PutChat(ChatMessage{
		ID: "c-otherthread", Sender: "mira", Recipient: "kye",
		Body: "a line in another thread", TS: 1.0,
	}); err != nil {
		t.Fatalf("seed other thread: %v", err)
	}
	// (b) a message strictly between two other members.
	if err := dal.PutChat(ChatMessage{
		ID: "c-bystanders", Sender: "m-1", Recipient: "m-2",
		Body: "not mira's business", TS: 2.0,
	}); err != nil {
		t.Fatalf("seed bystanders: %v", err)
	}

	for _, target := range []string{"c-otherthread", "c-bystanders"} {
		status, raw := postedChat(t, srv.URL, tok,
			`{"to":"owner","body":"quoting sideways","reply_to":"`+target+`"}`)
		if status != 400 {
			t.Fatalf("reply_to=%s must be refused, got %d %s", target, status, raw)
		}
		if !strings.Contains(raw, target) {
			t.Fatalf("the refusal must name the id: %s", raw)
		}
	}

	// …and the honest case still works, so the check above is not just
	// refusing everything.
	status, raw := postedChat(t, srv.URL, tok, `{"to":"owner","body":"mine"}`)
	if status != 200 {
		t.Fatalf("control post: %d %s", status, raw)
	}
	mineID, _ := chatFields(t, raw)
	status, raw = postedChat(t, srv.URL, tok,
		`{"to":"owner","body":"quoting my own thread","reply_to":"`+mineID+`"}`)
	if status != 200 {
		t.Fatalf("a same-conversation reply must be accepted, got %d %s", status, raw)
	}
}

// ④ THE FORGED LINK. `meta` is copied through wholesale — that is documented
// behaviour and other keys rely on it — so the ONE key the server owns has to be
// removed on the way in. Without this, ② and ③ are decoration: a caller that
// wants an unvalidated link just puts it in meta instead.
func TestChatReplyTo_ACallerSuppliedMetaLinkIsDiscarded(t *testing.T) {
	srv, secret, _ := newWiredTestServerWithDB(t)
	tok, _ := mintJWT("mira", "agent", 300, secret, time.Now().Unix(), "")

	status, raw := postedChat(t, srv.URL, tok,
		`{"to":"owner","body":"forged","meta":{"reply_to":"c-forged","keepme":"yes"}}`)
	if status != 200 {
		t.Fatalf("the post itself is legal: %d %s", status, raw)
	}
	id, replyTo := chatFields(t, raw)
	if replyTo != "" {
		t.Fatalf("a caller-supplied meta.reply_to must not become the link, got %q", replyTo)
	}
	if strings.Contains(raw, "c-forged") {
		t.Fatalf("the forged value must not survive anywhere on the message: %s", raw)
	}

	// Read it back too — and check the SIBLING key survived, so "delete the
	// forged link" cannot be satisfied by dropping the whole meta map.
	status, listed := doRaw(t, "GET", srv.URL+"/api/chat?ids="+id, tok, "", nil)
	if status != 200 {
		t.Fatalf("re-read: %d %s", status, listed)
	}
	if strings.Contains(listed, "c-forged") {
		t.Fatalf("the forged link must not be stored: %s", listed)
	}
	if !strings.Contains(listed, "keepme") {
		t.Fatalf("only the reply_to key is the server's — the rest of meta must "+
			"still ride through: %s", listed)
	}
}

// ⑤ THE AGENT DOOR IS THE SAME DOOR (owner ruling, rc-67f1f1263daf: 「能：agent
// 發訊也可以指定回覆對象」). Every test above already posts AS AN AGENT, which is
// the behavioural half. This is the DISCOVERABILITY half: an agent only ever
// learns a parameter exists by reading the tool schema, so a reply_to the agent
// cannot see is a reply_to the agent does not have.
func TestChatReplyTo_ThePostChatToolSchemaAdvertisesIt(t *testing.T) {
	desc := postChatInputSchemaDescriptionOfReplyTo(t)
	if desc == "" {
		t.Fatal("post_chat's inputSchema must carry reply_to — the owner ruled " +
			"that agents may specify a reply target, and a parameter absent " +
			"from the schema is a parameter no agent will ever send")
	}
	for _, promise := range []string{"SAME CONVERSATION", "DISCARDED"} {
		if !strings.Contains(desc, promise) {
			t.Fatalf("the schema must state %q — the two refusals an agent will "+
				"actually hit are the two this parameter documents", promise)
		}
	}
}

// postChatInputSchemaDescriptionOfReplyTo reads reply_to's description out of
// the FROZEN MCP catalog — the bytes tools/list actually serves an agent, not
// the Go struct. Same reasoning as idsPropertyDescription: the promise an agent
// reads and the behaviour the tests above pin have to be the same sentence, or
// one of them drifts silently.
func postChatInputSchemaDescriptionOfReplyTo(t *testing.T) string {
	t.Helper()
	raw, err := os.ReadFile("../../spec/mcp-catalog.json")
	if err != nil {
		t.Fatalf("read frozen catalog: %v", err)
	}
	var catalog struct {
		Tools []struct {
			Name        string `json:"name"`
			InputSchema struct {
				Properties map[string]struct {
					Description string `json:"description"`
				} `json:"properties"`
			} `json:"inputSchema"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(raw, &catalog); err != nil {
		t.Fatalf("parse frozen catalog: %v", err)
	}
	for _, tool := range catalog.Tools {
		if tool.Name == "post_chat" {
			return tool.InputSchema.Properties["reply_to"].Description
		}
	}
	t.Fatalf("post_chat is missing from spec/mcp-catalog.json")
	return ""
}
