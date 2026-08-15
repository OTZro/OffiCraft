package main

// api_chat_resume_context_test.go — the wake snapshot's CONTEXT format: names
// beside ids, a timestamp a reader can read, reply cards folded onto the
// message that opened them, global newest-first budget packing under a real
// ceiling, and the two
// DIFFERENT ways material can be missing (a message that is here with text
// collapsed away, versus messages that are not here at all).
//
// 🔴 Every assertion below was checked for DISCRIMINATION: the mutant that
// breaks it, and the fact that the mutant does not take a neighbouring
// assertion down first. Where an assertion has no mutant of its own that is
// said out loud rather than implied.

import (
	"encoding/json"
	"fmt"
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

// TestResumeChat_BudgetIsACeilingAndTheBoundaryIsTight is THE assertion this
// change exists to make true, and it is stated on the number a caller actually
// pays: overview.chat_chars — the messages, their folded cards, the snapshot
// header AND the cut hint, i.e. everything resumeSnapshotParts bills.
//
// 🔴 Before 2026-08-13 this could not hold and did not: every conversation
// line reserved its newest few messages, those were charged to the budget and
// then never evicted by it, so the block grew with the number of lines and
// nothing bounded it. Measured on one real member: 80,140 chat_chars across 496
// messages against a 12,000 ceiling. The floor is gone (owner: 「不要管每條對話
// 線」) and the ceiling is now real.
//
// TWO HALVES, because either alone is satisfiable by a bug:
//   - CEILING: chat_chars <= the live chat.budget_chars setting (T-c9b4 made it
//     a setting; it was the constant 8000). An empty chat block passes
//     this trivially, hence the anti-vacuity checks.
//   - TIGHT: one more message would have exceeded it. This is what stops a
//     packer that "fixes" the ceiling by simply serving less — the block has to
//     be at the ceiling, not merely under it.
func TestResumeChat_BudgetIsACeilingAndTheBoundaryIsTight(t *testing.T) {
	api := resumeCtxServer(t)
	// Uniform, modest messages from several peers so the stream is long enough
	// to overrun the budget many times over. Uniform size is what makes "one
	// more message" a well-defined quantity below.
	//
	// 🔴 UNIFORM IS A PRECONDITION OF HALF 2, NOT A CONVENIENCE, and the peer
	// list is chosen to make it TRUE rather than merely claimed. Half 2 reads
	// the cost of the OLDEST CARRIED message and calls it the cost of the next
	// one back; that substitution is only sound when every message in the
	// corpus bills the same. Two things used to break it silently:
	//   - the OWNER's line is exempt from collapsing (resumeChatCarriesFullBody),
	//     so an owner message bills its whole 200-rune body while a third
	//     party's bills the 120-rune preview — a ~1.5x difference;
	//   - a peer id with no roster row resolves to an EMPTY from_name, so it
	//     bills 2 runes less than a named one.
	// A mixed corpus makes half 2 depend on WHICH class of message the pack
	// happened to stop after: with 165 runes of slack left, a 153-rune reading
	// off a collapsed message declares the block "not at the ceiling" while the
	// message actually rejected cost 234 and the packer was in fact full. That
	// is a false alarm about the packer, and it moves whenever anything outside
	// the array changes size (the cut hint, say). So: only ROSTERED, NON-OWNER
	// peers with equal-length names, every body the same length, and the
	// uniformity is asserted below instead of assumed.
	chunk := strings.Repeat("字", 200)
	peers := []string{"m-quiet", "m-peer", "m-loud"}
	ts := 1.0
	for i := 0; i < 120; i++ {
		for _, peer := range peers {
			putChat(t, api, fmt.Sprintf("c-%s-%d", peer, i), peer, "m-exec", chunk, ts, nil)
			ts++
		}
	}

	// The budget is a setting now, so the ceiling is asserted against what THIS
	// server is actually running with — the same accessor the packer reads.
	budget := api.chatBudget()

	snap := resumeSnapshot(t, api, "m-exec")

	// ── anti-vacuity ────────────────────────────────────────────────────────
	// The corpus is far bigger than the budget, so the packer must have had to
	// choose. Without these, "chat_chars <= budget" is satisfied by serving
	// nothing at all.
	if len(snap.Chat) == 0 {
		t.Fatalf("fixture bug: the packer served nothing, so the ceiling proves nothing")
	}
	if len(snap.Chat) >= 120*len(peers) {
		t.Fatalf("fixture bug: nothing was left out (%d seeded, %d carried) — "+
			"the budget never bound", 120*len(peers), len(snap.Chat))
	}
	if !snap.ChatEarlierOmitted.Omitted || snap.ChatEarlierOmitted.Hint == "" {
		t.Fatalf("messages were dropped, so the cut must be reported with its hint: %+v",
			snap.ChatEarlierOmitted)
	}

	// ── half 1: the CEILING holds, on the number the caller pays ────────────
	if snap.Overview.ChatChars > budget {
		t.Fatalf("chat_chars must never exceed the budget: got %d, budget %d "+
			"(messages=%d)", snap.Overview.ChatChars, budget, len(snap.Chat))
	}

	// ── half 2: the boundary is TIGHT ───────────────────────────────────────
	// Re-derive what ONE more message would have cost, from the WIRE VALUES of
	// the oldest message that made it in (every message here is the same size,
	// so the next one back costs the same). Deliberately not asking the
	// production accountant: an assertion written against resumeChatMessageChars
	// is true by construction and survives every mutant of it.
	wireCost := func(m chatMessageDTO) int {
		return len([]rune(m.Body)) +
			len([]rune(m.FromName)) + len([]rune(m.ToName)) +
			len([]rune(m.TSDisplay)) +
			len(strconv.Itoa(m.BodyOmittedChars))
	}
	oldest := snap.Chat[0]
	oneMore := wireCost(oldest)
	// The substitution above is only legal on a uniform corpus, so CHECK it on
	// the wire instead of trusting the fixture. A future edit to the peer list
	// or the body that reintroduces a mixed corpus fails HERE, naming the
	// reason — rather than surfacing as an unexplained "not at the ceiling".
	for _, m := range snap.Chat {
		if got := wireCost(m); got != oneMore {
			t.Fatalf("fixture bug: the corpus is not uniform, so \"one more message\" "+
				"is not a well-defined size: %s costs %d, %s costs %d",
				oldest.ID, oneMore, m.ID, got)
		}
	}
	if oneMore <= 0 {
		t.Fatalf("fixture bug: a message that costs nothing cannot pin a boundary")
	}
	if snap.Overview.ChatChars+oneMore <= budget {
		t.Fatalf("the block must be AT the ceiling, not merely under it: "+
			"chat_chars=%d + one more message (%d) = %d, still <= budget %d",
			snap.Overview.ChatChars, oneMore, snap.Overview.ChatChars+oneMore,
			budget)
	}
}

// TestResumeChat_PackingStopsAtTheFirstMessageThatDoesNotFit pins the SHAPE of
// the walk, which the ceiling test above cannot see.
//
// 🔴 WHY THIS NEEDS ITS OWN TEST — stated because the gap was real and measured.
// The obvious mutant (turn the `break` into a `continue`, i.e. skip the message
// that does not fit and keep looking for a smaller older one) leaves the ceiling
// test GREEN: the block is still under budget and still at the boundary, only
// now with a HOLE in it. Verified by running that mutant against the whole
// TestResumeChat_ suite before this test existed — every case passed.
//
// The owner's ruling is a PREFIX, not a knapsack: 「只管從最新一則訊息往前推,
// 直到超出我們 budget 上限前最後一則」. A stream with gaps at unpredictable places
// is worse than a shorter one — the reader cannot tell an exchange that did not
// happen from one that was skipped over because it happened to be cheap.
//
// The fixture is built so the two behaviours DIVERGE: a big message sits exactly
// at the point where the budget runs out, and two cheap ones sit OLDER than it.
// Uniform-size corpora cannot tell them apart (if the next one does not fit,
// none of the older equal-sized ones do either), which is exactly why the
// ceiling test — whose corpus is uniform by design — is blind to this.
func TestResumeChat_PackingStopsAtTheFirstMessageThatDoesNotFit(t *testing.T) {
	api := resumeCtxServer(t)

	// Everything is on the OWNER's line so nothing is collapsed for length —
	// this case is about the packer's walk, not about body folding.
	putChat(t, api, "c-tiny-1", wireOwnerID, "m-exec", "短", 1, nil)
	putChat(t, api, "c-tiny-2", wireOwnerID, "m-exec", "短", 2, nil)
	putChat(t, api, "c-wall", wireOwnerID, "m-exec", strings.Repeat("牆", 4000), 3, nil)
	for i := 0; i < 30; i++ {
		putChat(t, api, fmt.Sprintf("c-mid-%02d", i), wireOwnerID, "m-exec",
			strings.Repeat("中", 150), float64(10+i), nil)
	}

	snap := resumeSnapshot(t, api, "m-exec")

	got := map[string]bool{}
	for _, m := range snap.Chat {
		got[m.ID] = true
	}

	// Anti-vacuity, both directions: the walk really did stop somewhere in the
	// middle. Without these, "the tiny ones are absent" is also satisfied by a
	// packer that served nothing, and "some mid ones are present" by one that
	// served everything.
	if len(snap.Chat) == 0 {
		t.Fatalf("fixture bug: nothing was served")
	}
	if got["c-wall"] {
		t.Fatalf("fixture bug: the wall message FIT, so it cannot mark a stop "+
			"point (served %d messages)", len(snap.Chat))
	}

	// 🔴 THE ASSERTION: nothing OLDER than the wall came through. A skip-and-
	// continue packer would have taken these two — they are cheap — and left a
	// hole where the wall is.
	for _, id := range []string{"c-tiny-1", "c-tiny-2"} {
		if got[id] {
			t.Fatalf("%s is older than the message that did not fit, so the walk "+
				"must have stopped before it: served=%d", id, len(snap.Chat))
		}
	}

	// And what WAS served is a contiguous run of the newest messages — no gaps
	// anywhere inside it either.
	seenNewer := false
	for i := 29; i >= 0; i-- {
		id := fmt.Sprintf("c-mid-%02d", i)
		if got[id] {
			seenNewer = true
			continue
		}
		if !seenNewer {
			continue // still walking back through the tail that did not fit
		}
		// A message absent while a NEWER one is present is fine only if every
		// older one is absent too — which is what the loop shape enforces from
		// here on.
		for j := i; j >= 0; j-- {
			if got[fmt.Sprintf("c-mid-%02d", j)] {
				t.Fatalf("gap in the served stream: c-mid-%02d is absent but the "+
					"older c-mid-%02d is present", i, j)
			}
		}
		break
	}
}

// TestResumeChat_ACollapseThatSavesNothingIsNotMade pins the other half of the
// "this payload is too big" ruling (owner 2026-08-13: 「省不到就不要折」).
//
// Folding is not free: the payload pays the ellipsis plus the digits of
// body_omitted_chars, and the reader pays a mark beside the message and a
// possible get_chat to get the text back. A body barely over the preview buys a
// handful of runes for all of that.
//
// 🔴 THE CASE IS DELIBERATELY THE BREAK-EVEN ONE, because that is the only place
// the rule is observable. MUTANT: delete the resumeChatCollapseIsWorthIt guard
// in resumeChatMessageDTO → the break-even message comes back folded and this
// goes red. The over-threshold message is the positive control: without it,
// "nothing was folded" would also pass on a build that never folds at all.
func TestResumeChat_ACollapseThatSavesNothingIsNotMade(t *testing.T) {
	api := resumeCtxServer(t)

	// Break-even: omitted == 2, and the marker costs 1 (the …) + 1 digit = 2.
	// Equal is not a saving, so this body must be carried WHOLE.
	breakEven := strings.Repeat("字", resumeChatOtherPreview+2)
	// One rune more: omitted == 3 > 2, so this one is worth folding. It is the
	// positive control — it proves the collapse machinery is still alive.
	worthIt := strings.Repeat("字", resumeChatOtherPreview+3)

	putChat(t, api, "c-even", "m-peer", "m-exec", breakEven, 1, nil)
	putChat(t, api, "c-worth", "m-peer", "m-exec", worthIt, 2, nil)

	snap := resumeSnapshot(t, api, "m-exec")

	even := chatByID(t, snap, "c-even")
	if even.BodyOmittedChars != 0 {
		t.Fatalf("a fold that breaks even must not be made: body_omitted_chars=%d",
			even.BodyOmittedChars)
	}
	if even.Body != breakEven {
		t.Fatalf("an unfolded body must be carried verbatim: got %d runes, want %d",
			len([]rune(even.Body)), len([]rune(breakEven)))
	}

	// Positive control: the machinery still folds when folding actually saves.
	worth := chatByID(t, snap, "c-worth")
	if worth.BodyOmittedChars != 3 {
		t.Fatalf("a fold that saves must still be made: body_omitted_chars=%d, want 3",
			worth.BodyOmittedChars)
	}
}

// ── ⑦ the merged stream is one chronological stream, not per-peer blocks ────

// TestResumeChat_MergedStreamIsChronological pins that the served block is ONE
// chronological stream.
//
// ⚠️ It used to guard the re-sort the per-line packing needed. That packing is
// gone (2026-08-13) and with it the re-sort: the read is a single global
// newest-N and the packer keeps a contiguous suffix of it, so order is now
// INHERITED rather than produced. This test therefore moved down a layer — the
// DAL half is where the mutant lives now (`ORDER BY ts DESC` re-sorted back to
// ascending in ListChatInvolving), and the served half is a cheap end-to-end
// restatement of it rather than an independent guard. Said out loud instead of
// leaving a comment claiming a discrimination it no longer has.
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
	msgs, err := api.dal.ListChatInvolving("m-exec", resumeChatFetch)
	if err != nil {
		t.Fatalf("snapshot read: %v", err)
	}
	for i := 1; i < len(msgs); i++ {
		if msgs[i].TS < msgs[i-1].TS {
			t.Fatalf("the snapshot read must come back oldest→newest: %v after %v",
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

	// Now overflow the budget so whole messages are left out. Each of these is
	// from a third party, so its body is collapsed to the preview first — the
	// count has to be big enough that the COLLAPSED corpus still overruns.
	chunk := strings.Repeat("字", 700)
	for i := 0; i < 100; i++ {
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

// 🔴 THESE TWO STRINGS ARE RENDERED AS MARKDOWN, SO THEIR SYNTAX IS PART OF
// THEIR CONTRACT.
//
// The cockpit draws `note` and the cut `hint` through the same Markdown
// renderer a chat body goes through (ResumeSummaryCard rule 4), because the
// only formatting plain text has — line breaks, `**`, backticks — was being
// collapsed into a wall of prose when they were printed as bare text nodes.
// That fix has a cost this test exists to hold: a line that ACCIDENTALLY looks
// like markup now renders as markup. Open a line with "1. " or "- " and the
// human sees a list item the agent does not; leave a backtick unpaired and the
// rest of the paragraph turns into code on screen while the agent reads it as
// prose. Both are silent — the bytes are still verbatim, and the parity suite
// on the other side uses a synthetic fixture, so nothing goes red.
//
// The guard lives HERE, at the source, rather than as a golden copy of these
// strings in a front-end test: a copy is one more thing to drift, and the
// property being asserted is a property of the TEXT, not of the renderer.
func TestResumeProse_CarriesNoAccidentalMarkdown(t *testing.T) {
	for _, tc := range []struct {
		name string
		text string
	}{
		{"resumeNote", resumeNote},
		{"resumeChatCutHint", resumeChatCutHint},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// Paired inline markers. An odd count means one of them opens a run
			// that never closes, and everything after it changes appearance.
			if n := strings.Count(tc.text, "`"); n%2 != 0 {
				t.Errorf("unpaired backtick (%d of them): a code run is left "+
					"open and the rest of the text renders as code", n)
			}
			if n := strings.Count(tc.text, "**"); n%2 != 0 {
				t.Errorf("unpaired ** (%d of them): the emphasis run never "+
					"closes", n)
			}
			// Block-level openers. Markdown reads these at the START of a line
			// only, which is exactly why they are easy to introduce by accident
			// when re-wrapping a sentence.
			for i, line := range strings.Split(tc.text, "\n") {
				trimmed := strings.TrimLeft(line, " \t")
				for _, bad := range []struct {
					prefix, becomes string
				}{
					{"- ", "a list item"},
					{"* ", "a list item"},
					{"+ ", "a list item"},
					{"> ", "a block quote"},
					{"# ", "a heading"},
					{"|", "a table row"},
				} {
					if strings.HasPrefix(trimmed, bad.prefix) {
						t.Errorf("line %d opens with %q, which renders as %s "+
							"for the human while the agent reads it as prose: %q",
							i+1, bad.prefix, bad.becomes, line)
					}
				}
				// "1. " / "12) " — an ordered list. Checked separately because
				// the marker is a number, not a fixed prefix.
				if m := orderedListOpener.FindString(trimmed); m != "" {
					t.Errorf("line %d opens with %q, which renders as an "+
						"ordered list item: %q", i+1, m, line)
				}
			}
			// The bullet these notes actually use is U+00B7, which markdown does
			// not know — that is why it was chosen. If this ever fails, the text
			// switched to a real list marker and the check above is the one that
			// will have caught it.
			if strings.Contains(tc.text, "·") && !strings.Contains(tc.text, "\n· ") {
				t.Errorf("the middle dot is used but never at the start of a " +
					"line — check the bullet convention is still intact")
			}
		})
	}
}

// "1. ", "2) ", "12. " at the head of a line.
var orderedListOpener = regexp.MustCompile(`^[0-9]{1,9}[.)] `)
