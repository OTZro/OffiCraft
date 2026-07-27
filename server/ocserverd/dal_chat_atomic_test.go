package main

import "testing"

// T-e2b2. The ONE DAL-level guard kept after review Q4: the outcome sweep in
// api_chat_orphan_blob_test.go subsumes the other two (I replayed their defects
// against the sweep alone and it caught all of them), but it cannot reach THIS
// failure class — the sweep aborts a statement the database is executing, while
// this one fails in Go BETWEEN statements: the blob and the companion message
// have already gone to the database, and the card never reaches it at all
// (review W1 — the earlier wording said "before any SQL is issued", which its
// own body comment contradicts ten lines below).
//
// The card face, where a partial write is worse
// than an orphan blob — the companion message's meta.reply_card_id would name a
// card row that does not exist, i.e. a permanently unanswerable ask sitting in
// the owner's chat stream. Split PutReplyCardWithChat back into PutChat +
// PutReplyCard and this goes red on the surviving message.
func TestPutReplyCardWithChat_FailedCardWriteLeavesNoMessage(t *testing.T) {
	d := newTestDAL(t)
	att := ChatAttachment{ID: "att-card-rollback", Mime: "text/plain", Data: []byte("payload")}
	msg := ChatMessage{
		ID: "c-card-rollback", Sender: "mira", Recipient: "owner", Body: "ship it?", TS: 1,
		Meta: map[string]any{"reply_card_id": "rc-rollback"},
	}
	// The failure is planted in the CARD's own marshalling, i.e. the LAST write:
	// the blob and the companion message have already succeeded when it trips.
	// That is the ordering the dangling-ask scenario needs — put it in the
	// message's meta instead and the message never lands, so the test would pass
	// while proving nothing about the message-vs-card window.
	card := ReplyCard{
		ID: "rc-rollback", FromMember: "mira", Kind: "decision", Summary: "ship it?",
		Options: []string{"yes", "no"}, Status: replyCardStatusWaiting,
		CreatedTS: 1, ChatMessageID: msg.ID,
		AnswerAttachments: []any{make(chan int)},
	}
	if err := d.PutReplyCardWithChat(card, msg, []ChatAttachment{att}); err == nil {
		t.Fatal("want the unencodable meta to fail the write")
	}
	if got, err := d.GetReplyCard(card.ID); err != nil || got != nil {
		t.Fatalf("card must not exist: card=%v err=%v", got, err)
	}
	stored, err := d.ListChatInvolving("owner", 10)
	if err != nil {
		t.Fatalf("list chat: %v", err)
	}
	if len(stored) != 0 {
		t.Fatalf("the companion message outlived its card: %d stored", len(stored))
	}
	if got, err := d.GetChatAttachment(att.ID); err != nil || got != nil {
		t.Fatalf("blob must not survive: att=%v err=%v", got, err)
	}
}
