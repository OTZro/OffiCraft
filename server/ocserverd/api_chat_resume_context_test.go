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
// whole packing rewrite.
//
// The fixture is arranged so the arithmetic is decisive, not marginal. The loud
// line is the OWNER's, whose bodies are carried in full, so 40 of them alone
// exceed the budget several times over. The two quiet lines are third parties
// (collapsed, and therefore cheap) whose messages are all OLDER than every loud
// one, so newest-first packing reaches them last. Packed with NO floor, the
// budget is spent on the owner line and at most ONE cheap message from the six
// quiet ones survives — so requiring BOTH quiet lines to keep three is a
// four-message margin, not a one-message coincidence.
//
// MUTANT: drop the reserve pass in resumeChatBlock (the `for _, p := range
// peers` loop that pre-keeps the newest resumeChatPeerFloor of each line) → the
// quiet-line assertion goes red. The positive controls below are what stop this
// from passing because nothing was packed at all, or because the fix was "starve
// whoever talks most".
func TestResumeChat_BudgetLeavesEveryLineItsFloor(t *testing.T) {
	api := resumeCtxServer(t)
	chunk := strings.Repeat("字", 700)

	// Two quiet third-party lines, OLDEST in the stream.
	for _, peer := range []string{"m-quiet", "m-peer"} {
		for i := 0; i < resumeChatPeerFloor; i++ {
			putChat(t, api, "c-"+peer+strconv.Itoa(i), peer, "m-exec", chunk, float64(1+i), nil)
		}
	}
	// The loud line: the owner, carried in full, far more than the budget holds.
	for i := 0; i < resumeChatPerPeerFetch; i++ {
		putChat(t, api, "c-l"+strconv.Itoa(i), wireOwnerID, "m-exec", chunk, float64(100+i), nil)
	}

	snap := resumeSnapshot(t, api, "m-exec")

	byLine := map[string]int{}
	for _, m := range snap.Chat {
		byLine[m.From]++
	}
	for _, peer := range []string{"m-quiet", "m-peer"} {
		if byLine[peer] < resumeChatPeerFloor {
			t.Fatalf("every line keeps its floor: want >= %d from %s, got %d (lines=%v)",
				resumeChatPeerFloor, peer, byLine[peer], byLine)
		}
	}
	// Positive control A: the budget really did bind — otherwise "the quiet lines
	// survived" is just "nothing was dropped".
	if byLine[wireOwnerID] >= resumeChatPerPeerFetch {
		t.Fatalf("the loud line must have been squeezed by the budget, got all %d (lines=%v)",
			byLine[wireOwnerID], byLine)
	}
	// Positive control B: the loud line kept its own floor too; the fix must not
	// be "starve whoever talks most".
	if byLine[wireOwnerID] < resumeChatPeerFloor {
		t.Fatalf("the loud line keeps a floor as well, got %d", byLine[wireOwnerID])
	}
	// Positive control C: the cut is REPORTED. A budget that drops messages
	// silently is the failure this whole format exists to remove.
	if !snap.ChatEarlierOmitted.Omitted {
		t.Fatalf("messages were dropped, so the cut must be reported: %+v",
			snap.ChatEarlierOmitted)
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

	want := len([]rune(full.GeneratedAt)) + len([]rune(full.ChatEarlierOmitted.Hint)) +
		resumeOverrunWireChars(full.ChatBudgetOverrun)
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

// ── ⑨ the OVER-BUDGET marker: size, not absence ─────────────────────────────

// resumeOverrunWireChars re-derives what the overrun marker costs a reader, from
// the WIRE VALUES only. It is deliberately NOT resumeChatOverrunChars — an
// assertion written against the production accountant is true by construction
// and survives every mutant of it (the same rule the estimate test above
// already runs on).
func resumeOverrunWireChars(o resumeChatBudgetOverrunDTO) int {
	if !o.Over {
		return 0
	}
	return len([]rune(o.Note)) +
		len(strconv.Itoa(o.BudgetChars)) +
		len(strconv.Itoa(o.BlockChars)) +
		len(strconv.Itoa(o.OverByChars))
}

// overBudgetSnapshot seeds `lines` SEPARATE conversation lines with `perLine`
// messages each, so the reserved floors alone outgrow the budget, and returns
// the resulting snapshot.
//
// 🔴 The fixture is the fault itself, not a stand-in for it. Every line's
// messages are SHORT — well inside every per-message bound — so nothing here is
// collapsed for length; the block is over budget PURELY because reserved
// messages are charged to the budget and then never evicted by it. That is the
// mechanism the marker exists to make visible, so a fixture that overran for any
// other reason would be testing the wrong thing.
//
// 🔴 `perLine` IS A PARAMETER BECAUSE FIXING IT AT THE FLOOR HID A CONTRADICTION
// FOR A WHOLE ROUND. With perLine == resumeChatPeerFloor there are no
// non-reserved messages at all, so nothing can be refused and
// chat_earlier_omitted is structurally ALWAYS false — the fixture could only
// ever produce a tidy overrun with nothing missing, which is the RARE case. Set
// perLine above the floor and the ordinary case appears: see
// TestResumeChat_OverBudgetAndCutAreSimultaneous for why it is the ordinary one.
// A fixture that can only reach the happy shape of a fault is not a test of the
// fault; an independent review found the resulting false cockpit label, not this
// suite.
func overBudgetSnapshot(t *testing.T, lines, perLine int) (*apiServer, resumeSummaryDTO) {
	t.Helper()
	api := newTasksTestServer(t)
	// The subject's own line is not one of the `lines` — it is not seeded at all.
	// Exactly at the collapse threshold, so NOTHING here is folded: this
	// overrun must be attributable to the floor and to nothing else.
	body := strings.Repeat("字", resumeChatOtherPreview)
	ts := 1.0
	for i := 0; i < lines; i++ {
		peer := "m-line" + strconv.Itoa(i)
		if err := api.dal.PutMember(Member{
			ID: peer, Name: "線" + strconv.Itoa(i), Kind: "assistant",
			RosterStatus: RosterStatusActive,
		}); err != nil {
			t.Fatalf("seed member %s: %v", peer, err)
		}
		for j := 0; j < perLine; j++ {
			ts++
			putChat(t, api, "c-"+peer+"-"+strconv.Itoa(j), peer, "m-exec", body, ts, nil)
		}
	}
	if err := api.dal.PutMember(Member{
		ID: "m-exec", Name: "阿執", Kind: "assistant", RosterStatus: RosterStatusActive,
	}); err != nil {
		t.Fatalf("seed subject: %v", err)
	}
	return api, resumeSnapshot(t, api, "m-exec")
}

// TestResumeChat_OverBudgetIsMarkedAndTheFloorIsNotEvicted is the discriminating
// test for the marker the owner ruled onto this payload (rc-b1fb7f1be05d,
// 2026-08-13, option ①: 「超出預算時在快照上標一行,不改任何行為。」).
//
// It asserts BOTH directions, because only the pair has any discrimination:
//   - over budget  → the marker is up, with figures that agree with each other
//     and with the block, AND every RESERVED message is still carried (the
//     ruling was "change no behaviour", so a marker that arrived alongside a
//     silent eviction of the floor would be the wrong change wearing the right
//     label).
//
// 🔴 THE NAME IS NARROW ON PURPOSE — it used to be
// "…AndNothingIsDropped", which over-claims. In THIS fixture every line holds
// exactly its floor, so there are no non-reserved messages and nothing CAN be
// dropped; that is a property of the fixture, not of an overrun. The general
// case is the opposite and lives next door in
// TestResumeChat_OverBudgetAndCutAreSimultaneous. A test name that generalises
// its own fixture is how "over budget ⇒ nothing missing" became a belief, and
// then a cockpit label.
//   - inside budget → the marker is DOWN and every field is at its zero value.
//     An orphan line on the ordinary snapshot is the failure mode of a marker.
//
// MUTANT A: make resumeChatBudgetOverrun return Over unconditionally (drop the
// `if blockChars <= resumeChatBudgetChars` guard) → the inside-budget half goes
// red; the over-budget half stays green.
// MUTANT B: make it always return the zero value → the over-budget half goes
// red; the inside-budget half stays green.
// Neither mutant can be hidden by the other half, which is why both are here.
func TestResumeChat_OverBudgetIsMarkedAndTheFloorIsNotEvicted(t *testing.T) {
	// ── inside budget: no marker, and no orphan fields ──────────────────────
	_, small := overBudgetSnapshot(t, 1, resumeChatPeerFloor)
	if small.Overview.ChatChars > resumeChatBudgetChars {
		t.Fatalf("fixture bug: the small snapshot must be INSIDE the budget, "+
			"chat_chars=%d budget=%d", small.Overview.ChatChars, resumeChatBudgetChars)
	}
	if small.ChatBudgetOverrun.Over {
		t.Fatalf("a snapshot inside its budget must not raise the overrun marker: %+v",
			small.ChatBudgetOverrun)
	}
	if small.ChatBudgetOverrun != (resumeChatBudgetOverrunDTO{}) {
		t.Fatalf("a marker that is down carries NO figures and NO text — an "+
			"orphan line is how a marker stops being read: %+v", small.ChatBudgetOverrun)
	}

	// ── over budget: marker up, arithmetic sound, nothing dropped ───────────
	//
	// The line count is derived, not guessed: each line reserves
	// resumeChatPeerFloor short messages, so this is comfortably past the
	// budget rather than one message either side of it.
	const lines = 40
	_, big := overBudgetSnapshot(t, lines, resumeChatPeerFloor)
	o := big.ChatBudgetOverrun
	if !o.Over {
		t.Fatalf("%d conversation lines each reserving %d messages overruns the "+
			"budget, so the marker must be up: chat_chars=%d %+v",
			lines, resumeChatPeerFloor, big.Overview.ChatChars, o)
	}
	if o.BudgetChars != resumeChatBudgetChars {
		t.Fatalf("the marker must report the ceiling it was measured against: "+
			"want %d, got %d", resumeChatBudgetChars, o.BudgetChars)
	}
	if o.BlockChars <= o.BudgetChars {
		t.Fatalf("an overrun means the block cost MORE than the budget, got "+
			"block=%d budget=%d", o.BlockChars, o.BudgetChars)
	}
	if o.OverByChars != o.BlockChars-o.BudgetChars {
		t.Fatalf("over_by_chars must be block - budget: %d - %d = %d, got %d",
			o.BlockChars, o.BudgetChars, o.BlockChars-o.BudgetChars, o.OverByChars)
	}
	// 🔴 THE BEHAVIOUR-UNCHANGED HALF. The reserved messages of every line are
	// still all here — the marker reports the overrun, it does not resolve it.
	if want := lines * resumeChatPeerFloor; len(big.Chat) != want {
		t.Fatalf("the ruling was to MARK the overrun and change no behaviour, so "+
			"every reserved message must still be carried: want %d, got %d",
			want, len(big.Chat))
	}
	// 🔴 SIZE, NOT ABSENCE. The marker must not be readable as a third kind of
	// missing material: no body here was folded, and the payload says so.
	for _, m := range big.Chat {
		if m.BodyOmittedChars != 0 {
			t.Fatalf("fixture bug: this overrun must come from the FLOOR, not from "+
				"collapsed bodies — %s reports %d folded",
				m.ID, m.BodyOmittedChars)
		}
	}
	// The note has to say, in the payload, what it is NOT — otherwise a reader
	// files it beside the other two markers and hunts for material that was
	// never missing. The POINTER is on this list for the same reason: the long
	// explanation was deleted from the note (see resumeChatOverrunNote), so the
	// note has to say where it went or the deletion is just a loss.
	//
	// ⚠️ WHAT THIS LIST DOES NOT GUARD — do not read it as more than it is. It
	// checks that each token is PRESENT, nothing else. A note replaced wholesale
	// by "SIZE over_by_chars budget seeds/system_interaction.md" satisfies every
	// entry and says nothing at all (an independent review demonstrated exactly
	// that, and it stayed green). It catches "the pointer is GONE"; it cannot
	// catch "the pointer is MEANINGLESS". Making it catch the second is a new
	// mechanism, not a longer list, and it is deliberately not in this round.
	for _, want := range []string{"SIZE", "over_by_chars", "budget", "seeds/system_interaction.md"} {
		if !strings.Contains(o.Note, want) {
			t.Fatalf("the overrun note must mention %q so it can be read correctly: %q",
				want, o.Note)
		}
	}
	// 🔴 AND IT HAS TO BE SHORT. This marker fires only when the block is
	// ALREADY over its budget, so every rune it spends is spent at the most
	// expensive moment there is — the first draft of this note was 860 runes,
	// 14% of the overrun it was reporting, which is a notice demonstrating the
	// problem it describes. The owner's ruling was 「標一行」. The bound is a
	// number rather than a vibe so that re-growing it is a red test and not a
	// judgement call in review.
	// ⚠️ The note is EXACTLY 200 runes, and this bound is `> 200`. Zero headroom
	// is deliberate, not an accident of drafting: the budget is the whole point
	// of this marker, so the note gets a hard ceiling rather than a comfortable
	// one, and any word added to it must displace a word. If you need room, take
	// it from the note — do not raise this number without saying why in the
	// commit, because "it only needs a few more" is how 200 becomes 860 again.
	if n := len([]rune(o.Note)); n > 200 {
		t.Fatalf("the overrun note must stay ≤200 runes — it is paid exactly "+
			"when the payload is already too big — got %d: %q", n, o.Note)
	}
}

// TestResumeChat_OverBudgetMarkerIsBilledToTheSizeEstimate pins the half a
// reader of peek_resume_summary_size depends on: the marker's own text and
// figures are runes that ride the payload, so estimated_total_chars must
// include them. A marker that is invisible to the peek makes the peek
// understate the payload by exactly the amount the marker was raised to report.
//
// MUTANT: drop the resumeChatOverrunChars term from overview.ChatChars in
// resumeSnapshotParts → the first comparison goes red. The zero-contribution
// half below is what stops "add a constant" from passing.
func TestResumeChat_OverBudgetMarkerIsBilledToTheSizeEstimate(t *testing.T) {
	api, big := overBudgetSnapshot(t, 40, resumeChatPeerFloor)
	if !big.ChatBudgetOverrun.Over {
		t.Fatalf("fixture bug: this snapshot must be over budget, %+v",
			big.ChatBudgetOverrun)
	}
	marker := resumeOverrunWireChars(big.ChatBudgetOverrun)
	if marker <= 0 {
		t.Fatalf("a raised marker costs a reader real runes, got %d", marker)
	}
	// chat_chars = the packer's block + the header + the cut hint + the marker.
	// Stated as a subtraction so the assertion names the term under test.
	rest := big.Overview.ChatChars - marker
	if rest != big.ChatBudgetOverrun.BlockChars+
		len([]rune(big.GeneratedAt))+
		len([]rune(big.ChatEarlierOmitted.Hint)) {
		t.Fatalf("chat_chars must carry the overrun marker's own cost: "+
			"chat_chars=%d marker=%d block=%d header=%d hint=%d",
			big.Overview.ChatChars, marker, big.ChatBudgetOverrun.BlockChars,
			len([]rune(big.GeneratedAt)), len([]rune(big.ChatEarlierOmitted.Hint)))
	}
	// The peek is the reader this number exists for, and it must land on the
	// SAME total — including the marker — because both go through the one
	// assembly. (The peek/full overview equality itself is pinned in ⑧; this is
	// the headline number on an OVER-BUDGET payload, which ⑧'s fixture is not.)
	peek := peekResumeSize(t, api, "m-exec")
	if peek.Overview != big.Overview {
		t.Fatalf("peek overview must equal the snapshot's on an over-budget "+
			"payload too:\n peek=%+v\n full=%+v", peek.Overview, big.Overview)
	}
	wantEstimate := big.Overview.ChatChars + big.Overview.TasksDetailChars +
		big.Overview.RosterChars + big.Overview.MachinesChars
	if peek.EstimatedTotalChars != wantEstimate {
		t.Fatalf("estimated_total_chars: want %d, got %d",
			wantEstimate, peek.EstimatedTotalChars)
	}

	// And a snapshot INSIDE its budget is billed nothing for a marker it does
	// not carry — the term must be conditional, not a constant.
	_, small := overBudgetSnapshot(t, 1, resumeChatPeerFloor)
	if got := resumeOverrunWireChars(small.ChatBudgetOverrun); got != 0 {
		t.Fatalf("a marker that is down costs nothing, got %d", got)
	}
}

// TestResumeChatBudgetOverrun_IsStrictlyGreater pins the boundary on its own,
// at the one pure function that decides it. A block that lands EXACTLY on the
// budget spent every rune it was allowed and overspent none, so it is not an
// overrun — marking it there would put a line on a snapshot with nothing to
// report, and the ordinary snapshot is the one that must stay quiet.
//
// MUTANT: relax `blockChars <= resumeChatBudgetChars` to `<` → the first half
// goes red. Widen it to `<= budget+1` → the second half goes red.
func TestResumeChatBudgetOverrun_IsStrictlyGreater(t *testing.T) {
	for _, at := range []int{0, 1, resumeChatBudgetChars - 1, resumeChatBudgetChars} {
		if got := resumeChatBudgetOverrun(at); got != (resumeChatBudgetOverrunDTO{}) {
			t.Fatalf("%d runes is within a %d budget, so no marker: %+v",
				at, resumeChatBudgetChars, got)
		}
	}
	one := resumeChatBudgetOverrun(resumeChatBudgetChars + 1)
	if !one.Over || one.OverByChars != 1 || one.BlockChars != resumeChatBudgetChars+1 {
		t.Fatalf("one rune past the budget IS an overrun, by exactly one: %+v", one)
	}
	if one.Note == "" {
		t.Fatalf("a raised marker must carry its own explanation: %+v", one)
	}
	if resumeChatOverrunChars(one) != resumeOverrunWireChars(one) {
		t.Fatalf("the production accountant and the wire re-derivation must "+
			"agree: %d vs %d", resumeChatOverrunChars(one), resumeOverrunWireChars(one))
	}
}

// TestResumeChat_OverBudgetAndCutAreSimultaneous pins the state every piece of
// wording on this payload has to survive, and that the previous cockpit label
// did not: OVER BUDGET and MATERIAL GENUINELY ABSENT, both true at once.
//
// 🔴 THEY ARE NOT ALTERNATIVES, THEY ARE THE ORDINARY CASE, and the mechanism is
// two lines of resumeChatBlock that disagree with each other on purpose:
//
//	reserve pass:  used += all[i].cost                      // UNCONDITIONAL
//	fill pass:     if used+all[i].cost <= budget { keep }   // CONDITIONAL
//
// So the moment the floors alone carry `used` past the budget, the fill pass can
// never succeed again: EVERY non-reserved message is refused, `dropped` is set,
// and chat_earlier_omitted is raised. An overrun with nothing missing requires
// every line to hold exactly its floor and not one message more — which is the
// exception, not the rule, and was the only shape the fixture could produce
// before perLine became a parameter.
//
// What this earns: any sentence on this payload that says "nothing is missing"
// is FALSE here. The surviving form is the narrow one every server-side copy
// already uses — nothing was dropped TO MAKE ROOM — and the cockpit labels are
// held to it by the wording guard in
// ResumeSummaryCard.payload-parity.test.tsx.
//
// MUTANT: make the reserve pass charge nothing (drop `used += all[i].cost`) →
// the overrun stops happening and the first assertion goes red. Make the fill
// pass unconditional → the cut stops being raised and the second goes red.
func TestResumeChat_OverBudgetAndCutAreSimultaneous(t *testing.T) {
	// Above the floor, so there ARE non-reserved messages for the budget to
	// refuse. 40 lines × 6 = 240 seeded, floors reserve 40 × 3 = 120.
	const lines, perLine = 40, resumeChatPeerFloor * 2
	_, snap := overBudgetSnapshot(t, lines, perLine)

	if !snap.ChatBudgetOverrun.Over {
		t.Fatalf("%d lines × %d reserve %d messages, which is past the budget: %+v",
			lines, perLine, lines*resumeChatPeerFloor, snap.ChatBudgetOverrun)
	}
	if !snap.ChatEarlierOmitted.Omitted {
		t.Fatalf("once the floors alone exceed the budget the fill pass can never "+
			"succeed, so whole messages ARE left out and the cut must say so: %+v",
			snap.ChatEarlierOmitted)
	}
	// 🔴 The two are not the same fact wearing two hats. The cut is raised
	// because messages were REFUSED; the overrun is raised because what was
	// KEPT costs more than the budget. Both numbers are checked so this cannot
	// pass on a snapshot that merely happens to set both booleans.
	if want := lines * resumeChatPeerFloor; len(snap.Chat) != want {
		t.Fatalf("exactly the reserved messages survive — the floors, and nothing "+
			"the fill pass could add: want %d of %d seeded, got %d",
			want, lines*perLine, len(snap.Chat))
	}
	if snap.ChatBudgetOverrun.BlockChars <= snap.ChatBudgetOverrun.BudgetChars {
		t.Fatalf("the kept block must still cost more than the budget: %+v",
			snap.ChatBudgetOverrun)
	}
	// Anti-vacuity: the seeded corpus really was bigger than what came back, so
	// "messages are absent" is a fact about this payload and not a flag someone
	// set. Half of what was seeded is gone.
	if len(snap.Chat) >= lines*perLine {
		t.Fatalf("fixture bug: nothing was actually left out (%d seeded, %d carried)",
			lines*perLine, len(snap.Chat))
	}
	// And nothing was folded for LENGTH either, so the only two things this
	// payload reports are the two under test.
	for _, m := range snap.Chat {
		if m.BodyOmittedChars != 0 {
			t.Fatalf("fixture bug: %s reports %d folded — this case must isolate "+
				"the cut and the overrun", m.ID, m.BodyOmittedChars)
		}
	}
}
