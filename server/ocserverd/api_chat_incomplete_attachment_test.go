package main

import (
	"strings"
	"testing"
	"time"
)

// T-e2b2 (owner rc-3a589dfec503, 2026-07-27): an attachment item that carries
// NEITHER id NOR data_b64 is refused — on every face that takes attachments.
//
// This lives in its own test, not in the fault table of
// TestHandlePostChatApiChatPost, because the decisive case is the one WITH body
// text: without text the message is empty anyway and the pre-T-e2b2 code
// already answered 400 (a different message, for a different reason), so a
// table row alone cannot tell the two worlds apart — and a sibling row failing
// first would abort the run before this one is even reached.
//
// What each mutant proves (measured, review finding F6 corrected the earlier
// overclaim): removing ONLY the refusal in resolveChatAttachmentInputs turns
// these red with a 400 carrying the WRONG reason ("attachment is empty");
// restoring the full pre-change shape — the refusal gone AND the chat handler's
// pre-filter back — turns the chat case red with a 200 and a posted message
// whose named file is absent, which is the defect itself. The task-message face
// has its own test (api_tasks_incomplete_attachment_test.go); it is not covered
// here.
func TestIncompleteAttachmentIsRefusedOnEveryFace(t *testing.T) {
	srv, secret, _ := newWiredTestServer(t)
	now := time.Now().Unix()
	agentTok, _ := mintJWT("mira", "agent", 300, secret, now, "")
	const ghost = `{"filename":"ghost.pdf","mime":"application/pdf"}`

	for _, tc := range []struct{ name, path, body string }{
		{"chat", "/api/chat",
			`{"to":"owner","body":"see attached","attachments":[` + ghost + `]}`},
		{"reply card", "/api/reply-cards",
			`{"kind":"decision","summary":"ship it?","options":["yes","no"],"attachments":[` + ghost + `]}`},
	} {
		// The answer face is covered by TestReplyCardAnswerRefusesIncompleteAttachment
		// below — it takes a live card id, so it cannot ride this table.
		status, resp := doRaw(t, "POST", srv.URL+tc.path, agentTok,
			"application/json", []byte(tc.body))
		if status != 400 || !strings.Contains(resp, "neither id nor data_b64") {
			t.Errorf("%s: want 400 naming the missing id/data_b64, got %d %s",
				tc.name, status, resp)
		}
	}

	// Nothing was posted: the refusal is not a message that quietly lost its
	// attachment.
	status, resp := doRaw(t, "GET", srv.URL+"/api/chat?with=owner", agentTok, "", nil)
	if status != 200 || strings.Contains(resp, "see attached") {
		t.Fatalf("a refused post must leave no message: %d %s", status, resp)
	}
}

// T-e2b2 / review R2: the reply-card ANSWER face kept the pre-filter this
// ticket exists to delete — measured before the fix, the SAME input answered
// 400 on the question side and 200 on the answer side, with the named file
// simply gone from the answer. That is the ticket's founding complaint (one
// mechanism, two opposite answers) surviving on a fourth face, inside a
// function this change had already edited.
//
// The id-only case is refused with its own message rather than dropped: this
// face decodes inline bytes and has never resolved {id} refs. Whether it should
// GAIN ref support is a separate owner question, not something to smuggle in
// under a bug fix.
func TestReplyCardAnswerRefusesIncompleteAttachment(t *testing.T) {
	srv, secret, _ := newWiredTestServer(t)
	now := time.Now().Unix()
	agentTok, _ := mintJWT("mira", "agent", 300, secret, now, "")
	ownerTok, _ := mintJWT(wireOwnerID, "owner", 300, secret, now, "")

	open := func() string {
		status, resp := doRaw(t, "POST", srv.URL+"/api/reply-cards", agentTok,
			"application/json",
			[]byte(`{"kind":"decision","summary":"ship it?","options":["yes","no"]}`))
		if status != 200 {
			t.Fatalf("open card: %d %s", status, resp)
		}
		return replyCardIDFromJSON(t, resp)
	}

	for _, tc := range []struct{ name, item, want string }{
		{"neither id nor bytes", `{"filename":"ghost.pdf"}`, "neither id nor data_b64"},
		{"id-only ref", `{"id":"att-whatever"}`, "must carry data_b64"},
	} {
		card := open()
		status, resp := doRaw(t, "POST", srv.URL+"/api/reply-cards/"+card+"/answer",
			ownerTok, "application/json",
			[]byte(`{"option_idx":0,"text":"see attached","attachments":[`+tc.item+`]}`))
		if status != 400 || !strings.Contains(resp, tc.want) {
			t.Errorf("%s: want 400 %q, got %d %s", tc.name, tc.want, status, resp)
			continue
		}
		// The card must still be answerable — a refused answer is not a
		// half-answered card.
		status, resp = doRaw(t, "GET", srv.URL+"/api/reply-cards/"+card, ownerTok, "", nil)
		if status != 200 || !strings.Contains(resp, `"status":"waiting"`) {
			t.Errorf("%s: card must stay waiting after a refused answer, got %d %s",
				tc.name, status, resp)
		}
	}
}
