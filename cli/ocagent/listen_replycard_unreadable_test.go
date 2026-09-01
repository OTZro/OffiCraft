package main

import (
	"bytes"
	"strings"
	"testing"
)

// T-44. The hazard the renderer's own comment had described for a whole ticket
// cycle, and which then HAPPENED: the owner circled option [0], the listener
// line printed "(empty answer)", and a reader who believed it would have gone
// and asked the same question again on a fresh card.
//
// The premise that makes this assertion sound: the server REFUSES an empty
// answer (applyReplyCardAnswer 400s on no-option/no-text/no-attachment, and
// `option_idxs: []` counts as empty), so an ANSWERED card carrying an answer
// this build renders as nothing is a READ failure here — never "he said
// nothing". This test pins that the line says so.
//
// MUTANT PROTOCOL for this file: change the two reads in renderReplyCardAnswer
// from `answer["option_idxs"]` back to the pre-T-40 `answer["option_idx"]`
// (i.e. simulate an ocagent older than the wire it is reading). The
// picked-options clause then yields nothing, and this test must go RED here.
func TestRenderReplyCardAnswer_UnreadableAnswerIsNotReportedAsEmpty(t *testing.T) {
	// An option-ONLY answer: nothing else can contribute a part, so the whole
	// line depends on the circled-option read — exactly the owner's case.
	srv, _ := replyCardServer(t, 200, `{"id":"rc-t44","from":"kyle","status":"answered",
		"summary":"要不要照做?","options":[{"text":"照做"},{"text":"先別"}],
		"answer":{"option_idxs":[0],"text":"","attachments":[]}}`)
	cfg := Config{Base: srv.URL, Token: "t", ID: "kyle"}
	var out bytes.Buffer
	handleReplyCard(srv.Client(), cfg, replyCardFrame("rc-t44", "kyle"), testReplySeen(t), "owner", &out)
	got := out.String()

	// POSITIVE CONTROL (green with no mutant): the circled option's own wording
	// reaches the reader.
	if !strings.Contains(got, `picked [0] "照做"`) {
		t.Errorf("the circled option's wording did not reach the line.\n"+
			"got: %q\nwant it to contain: %q", got, `picked [0] "照做"`)
	}

	// THE NAMED ASSERTION the mutant must break: whatever goes wrong, the line
	// must never tell the reader the owner gave an empty answer.
	if strings.Contains(got, "(empty answer)") {
		t.Fatalf("the owner CIRCLED an option and the line reported an empty answer — "+
			"his decision was rendered as its opposite.\ngot: %q", got)
	}

	// And when the answer cannot be read, the line must say THAT — a read
	// failure here, pointing at get_reply_card — not silence about the owner.
	if !strings.Contains(got, `picked [0]`) {
		if !strings.Contains(got, "UNREADABLE ANSWER") ||
			!strings.Contains(got, "get_reply_card") {
			t.Fatalf("this build could not read a PRESENT answer and the line did not "+
				"say so: it must name the failure as UNREADABLE and send the reader to "+
				"get_reply_card, because the server refuses an empty answer so this can "+
				"only be a read failure.\ngot: %q", got)
		}
	}
}

// The other half of the split: an answer that is ABSENT is not a parse failure
// and must not be shouted about. Only a PRESENT-but-unreadable answer is loud.
func TestRenderReplyCardAnswer_AbsentAnswerIsNotAParseFailure(t *testing.T) {
	got := renderReplyCardAnswer(map[string]any{
		"id":      "rc-t44-nil",
		"summary": "沒有 answer 欄位的 payload",
	})
	if strings.Contains(got, "UNREADABLE") {
		t.Fatalf("a payload carrying NO answer was reported as a parse failure; "+
			"nothing was there to parse.\ngot: %q", got)
	}
	if !strings.Contains(got, "no answer") {
		t.Fatalf("a payload carrying NO answer should say so plainly.\ngot: %q", got)
	}
}

// The unreadable notice is the fragment a human acts on, so its two actionable
// halves are pinned by name rather than left to prose drift. It must NOT tell
// anyone to touch the updater or upgrade somebody else's agent.
// The notice has to survive being read by a member who knows nothing about this
// bug, on EITHER runtime. This test pins what the sentence must MEAN, not the
// words it happens to use today — an earlier version of it pinned the literal
// "restarting the listener", which is exactly how a wrong instruction gets a
// guard of its own: the assertion protected the phrasing while the phrasing was
// telling half the fleet to do something it must not do.
func TestUnreadableAnswerNotice_IsActionableAndStaysInItsLane(t *testing.T) {
	// (1) It names the failure as this reader's, and points at the authority.
	for _, want := range []string{"UNREADABLE ANSWER", "get_reply_card"} {
		if !strings.Contains(unreadableAnswerNotice, want) {
			t.Errorf("notice is missing the actionable phrase %q: %q", want, unreadableAnswerNotice)
		}
	}

	// (2) It names a recovery verb the reader actually HOLDS. restart_self is an
	// MCP tool on both runtimes and is already this listener's recovery verb for
	// token expiry. Anything not on this list has to earn its place here first.
	recoveryVerbs := []string{"restart_self"}
	named := false
	for _, verb := range recoveryVerbs {
		if strings.Contains(unreadableAnswerNotice, verb) {
			named = true
		}
	}
	if !named {
		t.Errorf("notice names no recovery verb the reader can reach (want one of %v): %q",
			recoveryVerbs, unreadableAnswerNotice)
	}

	// (3) It must not steer the reader outside what they are authorised to do.
	// 🔴 "restart the listener" is HERE, not in the wanted list: a codex member
	// must not start or restart that process — seeds/boot_sequence_codex.md
	// step 3 says 「不要自己啟動 `ocagent listen`、Monitor 或前景迴圈」, the
	// sidecar owns it. The updater and other members' agents are nobody's to
	// touch from this line either.
	for _, forbidden := range []string{"updater", "upgrade", "restart the listener", "restarting the listener"} {
		if strings.Contains(strings.ToLower(unreadableAnswerNotice), strings.ToLower(forbidden)) {
			t.Errorf("notice steers the reader outside its lane (%q): %q", forbidden, unreadableAnswerNotice)
		}
	}
}
