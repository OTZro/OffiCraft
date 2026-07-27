package main

import "testing"

// T-e2b2: a chat write that fails MIDWAY must leave nothing behind. The failure
// is injected the only way that needs no fake DB — a meta value json.Marshal
// cannot encode — which lands INSIDE putChatOn, i.e. AFTER the blobs have
// already been written to the transaction. Remove the transaction from
// PutChatWithAttachments (write blobs, then the message, as HEAD did before
// this change) and this test goes red: the blob survives a message that was
// never stored, reachable by nothing (the gallery and the deletion cascade both
// walk from message refs).
func TestPutChatWithAttachments_FailedWriteLeavesNoBlob(t *testing.T) {
	d := newTestDAL(t)
	att := ChatAttachment{ID: "att-rollback", Mime: "text/plain", Data: []byte("payload")}
	msg := ChatMessage{
		ID: "c-rollback", Sender: "mira", Recipient: "owner", Body: "hi", TS: 1,
		Meta: map[string]any{"attachments": []any{attachmentRef(&att)}, "boom": make(chan int)},
	}
	if err := d.PutChatWithAttachments(msg, []ChatAttachment{att}); err == nil {
		t.Fatal("want the unencodable meta to fail the write")
	}
	got, err := d.GetChatAttachment(att.ID)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if got != nil {
		t.Fatal("the blob outlived the message that was supposed to name it")
	}
	stored, err := d.ListChatInvolving("owner", 10)
	if err != nil {
		t.Fatalf("list chat: %v", err)
	}
	if len(stored) != 0 {
		t.Fatalf("no message should exist, got %d", len(stored))
	}
}

// T-e2b2: the same rule across the card face, where a partial write is worse
// than an orphan blob — the companion message's meta.reply_card_id would name a
// card row that does not exist, i.e. a permanently unanswerable ask sitting in
// the owner's chat stream. Split PutReplyCardWithChat back into PutChat +
// PutReplyCard and this goes red on the surviving message.
func TestPutReplyCardWithChat_FailedCardWriteLeavesNoMessage(t *testing.T) {
	d := newTestDAL(t)
	att := ChatAttachment{ID: "att-card-rollback", Mime: "text/plain", Data: []byte("payload")}
	msg := ChatMessage{
		ID: "c-card-rollback", Sender: "mira", Recipient: "owner", Body: "ship it?", TS: 1,
		Meta: map[string]any{"reply_card_id": "rc-rollback", "boom": make(chan int)},
	}
	card := ReplyCard{
		ID: "rc-rollback", FromMember: "mira", Kind: "decision", Summary: "ship it?",
		Options: []string{"yes", "no"}, Status: replyCardStatusWaiting,
		CreatedTS: 1, ChatMessageID: msg.ID,
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
