package main

// api_chat_resume_context_test.go — the wake snapshot's CONTEXT format: names
// beside ids, a timestamp a reader can read, reply cards folded onto the
// message that opened them, per-line budget packing with a floor, and the two
// DIFFERENT ways material can be missing (a message that is here with text
// collapsed away, versus messages that are not here at all).
//
// 🔴 Every assertion below was checked for DISCRIMINATION: the mutant that
// breaks it, and the fact that the mutant does not take a neighbouring
// assertion down first. Where an assertion has no mutant of its own that is
// said out loud rather than implied.

import (
	"encoding/json"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// resumeCtxServer seeds a roster with real NAMES — the fixture used by every
// test in this file. newTasksTestServer does not seed members, so without this
// every from_name/to_name would be "" and the naming assertions would be
// vacuously satisfiable by a server that resolves nothing.
func resumeCtxServer(t *testing.T) *apiServer {
	t.Helper()
	api := newTasksTestServer(t)
	for id, name := range map[string]string{
		"m-exec":  "阿執",
		"m-peer":  "小佩",
		"m-loud":  "大聲",
		"m-quiet": "安靜",
	} {
		if err := api.dal.PutMember(Member{
			ID: id, Name: name, Kind: "assistant", RosterStatus: RosterStatusActive,
		}); err != nil {
			t.Fatalf("seed member %s: %v", id, err)
		}
	}
	return api
}

func putChat(t *testing.T, api *apiServer, id, from, to, body string, ts float64, meta map[string]any) {
	t.Helper()
	if err := api.dal.PutChat(ChatMessage{
		ID: id, Sender: from, Recipient: to, Body: body, TS: ts, Meta: meta,
	}); err != nil {
		t.Fatalf("put chat %s: %v", id, err)
	}
}

func chatByID(t *testing.T, snap resumeSummaryDTO, id string) chatMessageDTO {
	t.Helper()
	for _, m := range snap.Chat {
		if m.ID == id {
			return m
		}
	}
	t.Fatalf("message %q is not in the snapshot (%d carried)", id, len(snap.Chat))
	return chatMessageDTO{}
}

// displayTimeRe is the ONE shape this file accepts: full date, full time, and a
// zone offset. It is written out here rather than reusing resumeTimeLayout on
// purpose — a test that formats with the production layout constant would pass
// no matter what that constant said.
var displayTimeRe = regexp.MustCompile(`^\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2} [+-]\d{2}:\d{2}$`)

// ── ① the reply card folds onto the message that opened it ───────────────────

// TestResumeChat_CardFoldsOntoTheMessageThatOpenedIt pins that a carded message
// carries its options AND its answer inline, that an uncarded message carries
// nothing, and that the card is NOT also repeated as a top-level section.
//
// MUTANT: drop the `d.Card = &chatInlineReplyCardDTO{…}` assignment in
// resumeChatMessageDTO — the first sub-assertion below goes red and the
// uncarded / no-top-level-section ones stay green (they assert absence).
func TestResumeChat_CardFoldsOntoTheMessageThatOpenedIt(t *testing.T) {
	api := resumeCtxServer(t)

	idx := 1
	if err := api.dal.PutReplyCard(ReplyCard{
		ID: "rc-fold", FromMember: "m-exec", Kind: "decision",
		Summary: "ship or hold", Body: "which one",
		Options: []string{"ship it", "hold for review"},
		Status:  replyCardStatusAnswered, CreatedTS: 100, AnsweredTS: 200,
		ChatMessageID: "c-card", AnswerOptionIdx: &idx, AnswerText: "hold, we found a leak",
	}); err != nil {
		t.Fatalf("put card: %v", err)
	}
	putChat(t, api, "c-card", "m-exec", wireOwnerID, "請示：要出還是等", 100,
		map[string]any{"reply_card_id": "rc-fold"})
	putChat(t, api, "c-plain", "m-exec", wireOwnerID, "順帶一提", 101, nil)

	snap := resumeSnapshot(t, api, "m-exec")

	carded := chatByID(t, snap, "c-card")
	if carded.Card == nil {
		t.Fatalf("the message that opened the card must carry it inline: %+v", carded)
	}
	if got := strings.Join(carded.Card.Options, "|"); got != "ship it|hold for review" {
		t.Fatalf("inline card must carry the options as offered, got %q", got)
	}
	if carded.Card.AnswerOptionIdx == nil || *carded.Card.AnswerOptionIdx != 1 {
		t.Fatalf("inline card must carry which option was picked: %+v", carded.Card.AnswerOptionIdx)
	}
	if carded.Card.AnswerText != "hold, we found a leak" {
		t.Fatalf("inline card must carry the free text, got %q", carded.Card.AnswerText)
	}
	if carded.Card.AnsweredTS != 200 {
		t.Fatalf("inline card must carry answered_ts, got %v", carded.Card.AnsweredTS)
	}
	if !displayTimeRe.MatchString(carded.Card.AnsweredAtDisplay) {
		t.Fatalf("answered_at_display must be a full date+time+offset, got %q",
			carded.Card.AnsweredAtDisplay)
	}

	// A message with no card carries no card — the fold must not fabricate one.
	if plain := chatByID(t, snap, "c-plain"); plain.Card != nil {
		t.Fatalf("a message with no card must not carry one: %+v", plain.Card)
	}

	// 🔴 And the card is folded IN PLACE, not additionally hoisted into a second
	// top-level section. A `cards` array would carry the same decision twice in
	// one payload, which is the thing this design refuses.
	raw := resumeSnapshotRaw(t, api, "m-exec")
	var body map[string]json.RawMessage
	if err := json.Unmarshal([]byte(raw), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if _, ok := body["cards"]; ok {
		t.Fatalf("the snapshot must not carry a second top-level cards section: %v", raw)
	}
}

// TestResumeChat_OnlyTheSubjectsOwnCardsAreFolded pins the SCOPE ruling: cards
// the subject INITIATED. A card the subject merely answered belongs to whoever
// asked, and folding it here would put someone else's decision in this agent's
// wake.
//
// MUTANT: drop the `c.FromMember == subject` guard in resumeChatMessageDTO —
// this goes red alone (the test above only ever seeds the subject's own card).
func TestResumeChat_OnlyTheSubjectsOwnCardsAreFolded(t *testing.T) {
	api := resumeCtxServer(t)

	if err := api.dal.PutReplyCard(ReplyCard{
		ID: "rc-theirs", FromMember: "m-peer", Kind: "decision",
		Summary: "their ask", Options: []string{"yes", "no"},
		Status: replyCardStatusWaiting, CreatedTS: 100, ChatMessageID: "c-theirs",
	}); err != nil {
		t.Fatalf("put card: %v", err)
	}
	putChat(t, api, "c-theirs", "m-peer", "m-exec", "請你決定", 100,
		map[string]any{"reply_card_id": "rc-theirs"})

	snap := resumeSnapshot(t, api, "m-exec")
	m := chatByID(t, snap, "c-theirs")
	if m.Card != nil {
		t.Fatalf("another member's card must not be folded into this wake: %+v", m.Card)
	}
	// The read-time status join is unchanged and still answers "is there a card
	// here at all" — so the absence above is a scope decision, not a data gap.
	if m.ReplyCardStatus != replyCardStatusWaiting {
		t.Fatalf("reply_card_status must still join, got %q", m.ReplyCardStatus)
	}
}

// ── ② names AND ids, both ────────────────────────────────────────────────────

// TestResumeChat_CarriesNamesBesideIds pins that the display name RIDES WITH the
// id and never replaces it — a reply still has to be addressed to the id.
//
// MUTANT: drop the two `d.FromName/d.ToName` assignments — the name assertions
// go red while the id assertions stay green, which is the point of asserting
// both halves in the same test.
func TestResumeChat_CarriesNamesBesideIds(t *testing.T) {
	api := resumeCtxServer(t)
	putChat(t, api, "c-peer", "m-peer", "m-exec", "hi", 100, nil)
	putChat(t, api, "c-owner", wireOwnerID, "m-exec", "hi", 101, nil)

	snap := resumeSnapshot(t, api, "m-exec")

	fromPeer := chatByID(t, snap, "c-peer")
	if fromPeer.From != "m-peer" || fromPeer.To != "m-exec" {
		t.Fatalf("the ids must be untouched: from=%q to=%q", fromPeer.From, fromPeer.To)
	}
	if fromPeer.FromName != "小佩" || fromPeer.ToName != "阿執" {
		t.Fatalf("both names must resolve: from_name=%q to_name=%q",
			fromPeer.FromName, fromPeer.ToName)
	}

	// 🔴 The owner has NO roster row, so it can only be named by a special case.
	// Without one this reads "" and the human on the other end of the most
	// important line in the payload is the one nobody can name.
	fromOwner := chatByID(t, snap, "c-owner")
	if fromOwner.From != wireOwnerID {
		t.Fatalf("owner id must be untouched, got %q", fromOwner.From)
	}
	if fromOwner.FromName != resumeOwnerDisplayName {
		t.Fatalf("the owner must be named, got %q", fromOwner.FromName)
	}
}

// ── ③ ts_display carries the date and the zone ───────────────────────────────

// TestResumeChat_TSDisplayAlwaysCarriesDateAndZone pins the shape AND the
// no-clever-same-day-elision rule.
//
// MUTANT (shape): change resumeTimeLayout to drop the offset → red here.
// MUTANT (elision): add "same day as generated_at ⇒ time only" → the two
// same-day messages below stop matching, red here, and nothing else moves.
func TestResumeChat_TSDisplayAlwaysCarriesDateAndZone(t *testing.T) {
	api := resumeCtxServer(t)
	// Two messages on the SAME calendar day, one of them "now" — the exact case
	// a date-eliding optimisation would target.
	now := nowSecs()
	putChat(t, api, "c-early", "m-peer", "m-exec", "早", now-3600, nil)
	putChat(t, api, "c-now", "m-peer", "m-exec", "現在", now, nil)

	snap := resumeSnapshot(t, api, "m-exec")
	for _, id := range []string{"c-early", "c-now"} {
		m := chatByID(t, snap, id)
		if !displayTimeRe.MatchString(m.TSDisplay) {
			t.Fatalf("%s ts_display must be date+time+offset, got %q", id, m.TSDisplay)
		}
		// ts itself is untouched — the rendered field is IN ADDITION.
		if m.TS == 0 {
			t.Fatalf("%s must keep its epoch ts", id)
		}
	}
}

// ── ④ the snapshot header ────────────────────────────────────────────────────

// TestResumeSummary_GeneratedAtAnchorsTheTimestamps pins the header a reader
// needs to turn any ts_display into "how long ago".
//
// MUTANT: stop setting snap.GeneratedAt → red here alone.
func TestResumeSummary_GeneratedAtAnchorsTheTimestamps(t *testing.T) {
	api := resumeCtxServer(t)
	putChat(t, api, "c-1", "m-peer", "m-exec", "hi", nowSecs(), nil)

	snap := resumeSnapshot(t, api, "m-exec")
	if !displayTimeRe.MatchString(snap.GeneratedAt) {
		t.Fatalf("generated_at must be date+time+offset, got %q", snap.GeneratedAt)
	}
	// It must be on the wire under that name, not merely on the decoded struct.
	if !strings.Contains(resumeSnapshotRaw(t, api, "m-exec"), `"generated_at"`) {
		t.Fatalf("generated_at must be a wire field")
	}
}

// ── ⑤ who gets collapsed and who does not ───────────────────────────────────

// TestResumeChat_OwnerLineAndSelfHandoffAreNeverCollapsed pins the two
// exemptions together with the rule they are exemptions FROM, so a mutant that
// collapses everything and a mutant that collapses nothing each go red here.
//
// MUTANT (collapse everything): drop resumeChatCarriesFullBody's two arms →
// the owner and self-handoff assertions go red.
// MUTANT (collapse nothing): make it return true → the third-party assertions
// go red, including body_omitted_chars.
func TestResumeChat_OwnerLineAndSelfHandoffAreNeverCollapsed(t *testing.T) {
	api := resumeCtxServer(t)
	long := strings.Repeat("交", 1000)

	putChat(t, api, "c-owner", wireOwnerID, "m-exec", long, 100, nil)
	putChat(t, api, "c-toowner", "m-exec", wireOwnerID, long, 101, nil)
	putChat(t, api, "c-baton", "m-exec", "m-exec", long, 102, nil) // the hand-off baton
	putChat(t, api, "c-third", "m-peer", "m-exec", long, 103, nil)

	snap := resumeSnapshot(t, api, "m-exec")

	for _, id := range []string{"c-owner", "c-toowner", "c-baton"} {
		m := chatByID(t, snap, id)
		if len([]rune(m.Body)) != 1000 {
			t.Fatalf("%s must be carried in full, got %d runes", id, len([]rune(m.Body)))
		}
		if m.BodyOmittedChars != 0 {
			t.Fatalf("%s carries everything, so body_omitted_chars must be 0, got %d",
				id, m.BodyOmittedChars)
		}
	}

	third := chatByID(t, snap, "c-third")
	if got := len([]rune(third.Body)); got != resumeChatOtherPreview+1 {
		t.Fatalf("another agent's body must collapse to the lead + ellipsis, got %d runes", got)
	}
	if want := 1000 - resumeChatOtherPreview; third.BodyOmittedChars != want {
		t.Fatalf("body_omitted_chars must say how much was folded away: want %d, got %d",
			want, third.BodyOmittedChars)
	}
	// The folded text is recoverable, so a short third-party message is NOT
	// marked as collapsed — otherwise the marker would stop meaning anything.
	putChat(t, api, "c-short", "m-peer", "m-exec", "短", 104, nil)
	if m := chatByID(t, resumeSnapshot(t, api, "m-exec"), "c-short"); m.BodyOmittedChars != 0 {
		t.Fatalf("a body that fits must report 0 omitted, got %d", m.BodyOmittedChars)
	}
}

// ── ⑥ the budget, and the floor that keeps a quiet line alive ───────────────

// TestResumeChat_BudgetLeavesEveryLineItsFloor is the one that pays for the
// whole packing rewrite. The loud line is seeded so that, packed newest-first
// with no floor, it consumes the ENTIRE budget before the quiet line — whose
// messages are all older — is ever reached.
//
// MUTANT: drop the reserve pass in resumeChatBlock (the `for _, p := range
// peers` loop that pre-keeps the newest resumeChatPeerFloor of each line) → the
// quiet-line assertion goes red. The positive controls below (the loud line was
// really squeezed; the budget really bound) are what stop this from passing
// because nothing was packed at all.
func TestResumeChat_BudgetLeavesEveryLineItsFloor(t *testing.T) {
	api := resumeCtxServer(t)
	chunk := strings.Repeat("字", 700)

	// Quiet line: OLDEST in the stream, so newest-first packing reaches it last.
	for i := 0; i < 3; i++ {
		putChat(t, api, "c-q"+strconv.Itoa(i), "m-quiet", "m-exec", chunk, float64(1+i), nil)
	}
	// Loud line: far more than the budget can hold, and all newer.
	for i := 0; i < resumeChatPerPeerFetch; i++ {
		putChat(t, api, "c-l"+strconv.Itoa(i), "m-loud", "m-exec", chunk, float64(100+i), nil)
	}

	snap := resumeSnapshot(t, api, "m-exec")

	quiet, loud := 0, 0
	for _, m := range snap.Chat {
		switch m.From {
		case "m-quiet":
			quiet++
		case "m-loud":
			loud++
		}
	}
	if quiet < resumeChatPeerFloor {
		t.Fatalf("every line keeps its floor: want >= %d from the quiet peer, got %d (loud=%d)",
			resumeChatPeerFloor, quiet, loud)
	}
	// Positive control A: the budget really did bind — otherwise "the quiet line
	// survived" is just "nothing was dropped".
	if loud >= resumeChatPerPeerFetch {
		t.Fatalf("the loud line must have been squeezed by the budget, got all %d", loud)
	}
	// Positive control B: the loud line kept its own floor too; the fix must not
	// be "starve whoever talks most".
	if loud < resumeChatPeerFloor {
		t.Fatalf("the loud line keeps a floor as well, got %d", loud)
	}
}

// ── ⑦ the merged stream is one chronological stream, not per-peer blocks ────

// TestResumeChat_MergedStreamIsChronological pins the risk the per-line packing
// introduced: the block is now collected line by line, so chronological order
// is something the assembly has to produce rather than inherit.
//
// MUTANT: drop the sort.SliceStable in resumeChatBlock → the served array comes
// out grouped by peer and this goes red. The DAL half below is red instead if
// the outer `ORDER BY ts, id` is removed from ListChatPerPeerInvolving.
func TestResumeChat_MergedStreamIsChronological(t *testing.T) {
	api := resumeCtxServer(t)
	// Interleave two lines so grouping and ordering are DISTINGUISHABLE — with
	// one line, or with two non-overlapping ones, both orders look identical.
	putChat(t, api, "c-a1", "m-peer", "m-exec", "a1", 1, nil)
	putChat(t, api, "c-b1", "m-quiet", "m-exec", "b1", 2, nil)
	putChat(t, api, "c-a2", "m-peer", "m-exec", "a2", 3, nil)
	putChat(t, api, "c-b2", "m-quiet", "m-exec", "b2", 4, nil)

	snap := resumeSnapshot(t, api, "m-exec")
	if len(snap.Chat) != 4 {
		t.Fatalf("expected all four seeded messages, got %d", len(snap.Chat))
	}
	got := make([]string, 0, 4)
	for _, m := range snap.Chat {
		got = append(got, m.ID)
	}
	if want := "c-a1,c-b1,c-a2,c-b2"; strings.Join(got, ",") != want {
		t.Fatalf("the merged stream must be chronological, want %s got %s",
			want, strings.Join(got, ","))
	}

	// The DAL half of the same guarantee, asserted at its own source.
	msgs, err := api.dal.ListChatPerPeerInvolving("m-exec", resumeChatPerPeerFetch)
	if err != nil {
		t.Fatalf("per-peer read: %v", err)
	}
	for i := 1; i < len(msgs); i++ {
		if msgs[i].TS < msgs[i-1].TS {
			t.Fatalf("the per-line read must merge oldest→newest: %v after %v",
				msgs[i].TS, msgs[i-1].TS)
		}
	}
}

// ── the cut marker: truncation, and NOT the same thing as collapse ──────────

// TestResumeChat_CutMarkerIsActionableAndDistinctFromCollapse pins that
// chat_earlier_omitted tells a reader how to fetch what is missing, and that it
// is not silently conflated with body_omitted_chars.
//
// MUTANT: stop setting cut.Omitted/cut.Hint → the omitted case goes red. The
// nothing-omitted case is the positive control that stops "always true" from
// passing.
func TestResumeChat_CutMarkerIsActionableAndDistinctFromCollapse(t *testing.T) {
	api := resumeCtxServer(t)

	// Nothing to cut: one short message, well inside every bound.
	putChat(t, api, "c-only", "m-peer", "m-exec", "短", 1, nil)
	clean := resumeSnapshot(t, api, "m-exec")
	if clean.ChatEarlierOmitted.Omitted {
		t.Fatalf("a snapshot that carries everything must not claim a cut: %+v",
			clean.ChatEarlierOmitted)
	}
	if clean.ChatEarlierOmitted.Hint != "" {
		t.Fatalf("no cut means no hint, got %q", clean.ChatEarlierOmitted.Hint)
	}

	// Now overflow the budget so whole messages are left out.
	chunk := strings.Repeat("字", 700)
	for i := 0; i < resumeChatPerPeerFetch; i++ {
		putChat(t, api, "c-x"+strconv.Itoa(i), "m-loud", "m-exec", chunk, float64(100+i), nil)
	}
	cut := resumeSnapshot(t, api, "m-exec").ChatEarlierOmitted
	if !cut.Omitted {
		t.Fatalf("messages were dropped, so the cut must be reported: %+v", cut)
	}
	// 🔴 The hint has to be usable by a reader that has nothing else loaded, so
	// it must name the tool AND the exact parameter pairing AND the failure a
	// first attempt hits.
	for _, want := range []string{"get_chat", "with", "before_ts", "before_id", "422"} {
		if !strings.Contains(cut.Hint, want) {
			t.Fatalf("the cut hint must mention %q so it can be acted on: %q", want, cut.Hint)
		}
	}
	// 🔴 Collapse and truncation must not be described with the same words. The
	// hint has to say, in its own text, that it is NOT the per-message marker —
	// before this split both showed up as a bare "…" and a reader could not tell
	// "I have this message, shortened" from "I do not have this message".
	if !strings.Contains(cut.Hint, "body_omitted_chars") {
		t.Fatalf("the cut hint must distinguish itself from the collapse marker: %q", cut.Hint)
	}
}

// ── ⑧ the size estimate covers what the format actually carries ─────────────

// TestResumeSummary_EstimateCountsEverythingTheChatBlockCarries re-derives
// chat_chars from the WIRE VALUES, field by field, over a payload that exercises
// every contribution at once: a collapsed body, a full body, names, rendered
// timestamps, a folded card, and the header.
//
// 🔴 It deliberately does NOT call resumeChatMessageChars — an assertion written
// against the production accountant is true by construction and would survive
// every mutant of it.
//
// MUTANT: drop ANY one term from resumeChatMessageChars, or drop the
// generated_at / cut-hint terms from the overview, → red here. The peek/full
// equality below is red instead if the two ever stop sharing the assembly.
func TestResumeSummary_EstimateCountsEverythingTheChatBlockCarries(t *testing.T) {
	api := resumeCtxServer(t)

	idx := 0
	if err := api.dal.PutReplyCard(ReplyCard{
		ID: "rc-size", FromMember: "m-exec", Kind: "decision",
		Summary: "s", Options: []string{"選項一", "選項二"},
		Status: replyCardStatusAnswered, CreatedTS: 100, AnsweredTS: 150,
		ChatMessageID: "c-card", AnswerOptionIdx: &idx, AnswerText: "就這樣",
	}); err != nil {
		t.Fatalf("put card: %v", err)
	}
	putChat(t, api, "c-card", "m-exec", wireOwnerID, "請示", 100,
		map[string]any{"reply_card_id": "rc-size"})
	putChat(t, api, "c-long", "m-peer", "m-exec", strings.Repeat("長", 900), 101, nil)

	full := resumeSnapshot(t, api, "m-exec")
	peek := peekResumeSize(t, api, "m-exec")

	// The peek and the snapshot go through the SAME assembly, so their overviews
	// cannot drift. (Unchanged contract, restated because the format moved.)
	if peek.Overview != full.Overview {
		t.Fatalf("peek overview must equal the snapshot's:\n peek=%+v\n full=%+v",
			peek.Overview, full.Overview)
	}

	want := len([]rune(full.GeneratedAt)) + len([]rune(full.ChatEarlierOmitted.Hint))
	sawCard, sawCollapse := false, false
	for _, m := range full.Chat {
		want += len([]rune(m.Body)) +
			len([]rune(m.FromName)) + len([]rune(m.ToName)) +
			len([]rune(m.TSDisplay)) +
			len(strconv.Itoa(m.BodyOmittedChars))
		if m.BodyOmittedChars > 0 {
			sawCollapse = true
		}
		if m.Card != nil {
			sawCard = true
			for _, o := range m.Card.Options {
				want += len([]rune(o))
			}
			want += len([]rune(m.Card.AnswerText)) + len([]rune(m.Card.AnsweredAtDisplay))
			if m.Card.AnswerOptionIdx != nil {
				want += len(strconv.Itoa(*m.Card.AnswerOptionIdx))
			}
		}
	}
	// Anti-vacuity: if the payload did not actually exercise a card and a
	// collapse, this whole comparison proves much less than it looks like.
	if !sawCard || !sawCollapse {
		t.Fatalf("fixture must exercise a folded card AND a collapsed body (card=%v collapse=%v)",
			sawCard, sawCollapse)
	}
	if full.Overview.ChatChars != want {
		t.Fatalf("chat_chars must count everything the block carries: want %d, got %d",
			want, full.Overview.ChatChars)
	}
	// And the peek's single headline number is still that plus the other blocks.
	wantEstimate := full.Overview.ChatChars + full.Overview.TasksDetailChars +
		full.Overview.RosterChars + full.Overview.MachinesChars
	if peek.EstimatedTotalChars != wantEstimate {
		t.Fatalf("estimated_total_chars: want %d, got %d", wantEstimate, peek.EstimatedTotalChars)
	}
}
