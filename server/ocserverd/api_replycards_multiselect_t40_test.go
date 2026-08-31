package main

import (
	"net/http"
	"reflect"
	"testing"
)

// openUnboundCard mints one plain 請示 with the given options and select_mode
// (empty = let the server default it) and returns the served card.
func openUnboundCard(t *testing.T, api *apiServer, selectMode string,
	options []map[string]any) replyCardDTO {
	t.Helper()
	body := map[string]any{
		"kind": "decision", "summary": "which way?",
		"options": options, "linked_task": nil,
	}
	if selectMode != "" {
		body["select_mode"] = selectMode
	}
	rec := createCardRaw(t, api, "m-exec", body)
	if rec.Code != http.StatusOK {
		t.Fatalf("open card: %d %s", rec.Code, rec.Body.String())
	}
	return decodeBody[replyCardDTO](t, rec)
}

func threeOptions() []map[string]any {
	return []map[string]any{{"text": "A"}, {"text": "B"}, {"text": "C"}}
}

// An EMPTY option list is not an answer. This is its own test because the guard
// it protects changed shape: it used to compare a *int against nil, where
// "absent" was the only way to carry no option. Against a LIST, `[]` decodes to
// a non-nil empty slice, so a nil check passes it — and a card would close, and
// a held task would resume, on a decision the owner never made.
func TestAnswerReplyCardRejectsAnEmptyOptionIdxsList(t *testing.T) {
	api := newTasksTestServer(t)
	card := openUnboundCard(t, api, replyCardSelectModeMulti, threeOptions())

	rec := answerCard(t, api, card.ID, map[string]any{"option_idxs": []int{}})

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("an empty option_idxs list must be refused, got %d %s",
			rec.Code, rec.Body.String())
	}
	if msg := errorMessageOf(t, rec); msg != "answer must carry an option, text, or an attachment" {
		t.Fatalf("refusal message: %q", msg)
	}
	stored, err := api.dal.GetReplyCard(card.ID)
	if err != nil || stored == nil {
		t.Fatalf("reread card: %v %v", stored, err)
	}
	if stored.Status != replyCardStatusWaiting {
		t.Fatalf("a refused answer must leave the card waiting, got %q", stored.Status)
	}
	if stored.AnswerOptionIdxs != nil || stored.AnsweredTS != 0 {
		t.Fatalf("a refused answer must store nothing: %+v", stored)
	}
}

// The owner's CLICK ORDER is not part of the decision: [2,0] and [0,2] say the
// same thing and must land in the database as the same bytes. A reader that
// could tell them apart once read a re-ordered re-answer as a CHANGED one and
// swallowed a delivery.
func TestAnswerReplyCardStoresOptionIdxsDedupedAndAscending(t *testing.T) {
	api := newTasksTestServer(t)
	descending := openUnboundCard(t, api, replyCardSelectModeMulti, threeOptions())
	ascending := openUnboundCard(t, api, replyCardSelectModeMulti, threeOptions())

	if rec := answerCard(t, api, descending.ID,
		map[string]any{"option_idxs": []int{2, 0}}); rec.Code != http.StatusOK {
		t.Fatalf("answer [2,0]: %d %s", rec.Code, rec.Body.String())
	}
	if rec := answerCard(t, api, ascending.ID,
		map[string]any{"option_idxs": []int{0, 2}}); rec.Code != http.StatusOK {
		t.Fatalf("answer [0,2]: %d %s", rec.Code, rec.Body.String())
	}

	a, err := api.dal.GetReplyCard(descending.ID)
	if err != nil {
		t.Fatalf("reread [2,0] card: %v", err)
	}
	b, err := api.dal.GetReplyCard(ascending.ID)
	if err != nil {
		t.Fatalf("reread [0,2] card: %v", err)
	}
	if !reflect.DeepEqual(a.AnswerOptionIdxs, []int{0, 2}) {
		t.Fatalf("[2,0] must store as [0 2], got %v", a.AnswerOptionIdxs)
	}
	if !reflect.DeepEqual(a.AnswerOptionIdxs, b.AnswerOptionIdxs) {
		t.Fatalf("[2,0] and [0,2] must store identically, got %v and %v",
			a.AnswerOptionIdxs, b.AnswerOptionIdxs)
	}

	dup := openUnboundCard(t, api, replyCardSelectModeMulti, threeOptions())
	if rec := answerCard(t, api, dup.ID,
		map[string]any{"option_idxs": []int{1, 1, 0}}); rec.Code != http.StatusOK {
		t.Fatalf("answer [1,1,0]: %d %s", rec.Code, rec.Body.String())
	}
	stored, err := api.dal.GetReplyCard(dup.ID)
	if err != nil {
		t.Fatalf("reread dup card: %v", err)
	}
	if !reflect.DeepEqual(stored.AnswerOptionIdxs, []int{0, 1}) {
		t.Fatalf("duplicates must collapse, got %v", stored.AnswerOptionIdxs)
	}
}

// A single-select card accepts ONE circled option. Silently keeping the first of
// two would record a decision the owner did not make, and the card would look
// perfectly well-formed afterwards.
func TestAnswerReplyCardRejectsTwoIndicesOnASingleSelectCard(t *testing.T) {
	api := newTasksTestServer(t)
	card := openUnboundCard(t, api, replyCardSelectModeSingle, threeOptions())

	rec := answerCard(t, api, card.ID, map[string]any{"option_idxs": []int{0, 2}})

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("two indices on a single-select card must be refused, got %d %s",
			rec.Code, rec.Body.String())
	}
	if msg := errorMessageOf(t, rec); msg != "this card is single-select: option_idxs may carry at most one index" {
		t.Fatalf("refusal message: %q", msg)
	}
	stored, err := api.dal.GetReplyCard(card.ID)
	if err != nil || stored == nil {
		t.Fatalf("reread card: %v %v", stored, err)
	}
	if stored.Status != replyCardStatusWaiting || stored.AnswerOptionIdxs != nil {
		t.Fatalf("a refused answer must store nothing: %+v", stored)
	}

	// The same card takes ONE index.
	if rec := answerCard(t, api, card.ID,
		map[string]any{"option_idxs": []int{2}}); rec.Code != http.StatusOK {
		t.Fatalf("one index must be accepted: %d %s", rec.Code, rec.Body.String())
	}
	stored, _ = api.dal.GetReplyCard(card.ID)
	if !reflect.DeepEqual(stored.AnswerOptionIdxs, []int{2}) {
		t.Fatalf("single-select answer: %v", stored.AnswerOptionIdxs)
	}

	// A MULTI card of the same shape takes both — proving the refusal above is
	// the select_mode gate and not a blanket ban on two indices.
	multi := openUnboundCard(t, api, replyCardSelectModeMulti, threeOptions())
	if rec := answerCard(t, api, multi.ID,
		map[string]any{"option_idxs": []int{0, 2}}); rec.Code != http.StatusOK {
		t.Fatalf("two indices on a multi card must be accepted: %d %s",
			rec.Code, rec.Body.String())
	}
}

func TestAnswerReplyCardRejectsAnOutOfRangeIndex(t *testing.T) {
	api := newTasksTestServer(t)
	card := openUnboundCard(t, api, replyCardSelectModeMulti, threeOptions())

	for _, idxs := range [][]int{{3}, {-1}, {0, 3}} {
		rec := answerCard(t, api, card.ID, map[string]any{"option_idxs": idxs})
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("%v must be refused, got %d %s", idxs, rec.Code, rec.Body.String())
		}
		if msg := errorMessageOf(t, rec); msg != "option_idxs out of range" {
			t.Fatalf("%v refusal message: %q", idxs, msg)
		}
	}
	stored, _ := api.dal.GetReplyCard(card.ID)
	if stored.Status != replyCardStatusWaiting {
		t.Fatalf("a refused answer must leave the card waiting, got %q", stored.Status)
	}
}

// ai_pick is now a per-option flag, so "which one does the AI suggest" is a
// question the card answers by itself. A single-select card may answer it at
// most once: two recommendations on a card that accepts one choice is a question
// with no honest reading.
func TestCreateReplyCardEnforcesTheAiPickBudget(t *testing.T) {
	api := newTasksTestServer(t)
	two := []map[string]any{{"text": "A", "ai_pick": true}, {"text": "B", "ai_pick": true}}

	rec := createCardRaw(t, api, "m-exec", map[string]any{
		"kind": "decision", "summary": "which way?", "options": two,
		"select_mode": replyCardSelectModeSingle, "linked_task": nil,
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("two ai_picks on a single-select card must be refused, got %d %s",
			rec.Code, rec.Body.String())
	}
	if msg := errorMessageOf(t, rec); msg != "a single-select card may mark at most one option ai_pick" {
		t.Fatalf("refusal message: %q", msg)
	}

	multi := openUnboundCard(t, api, replyCardSelectModeMulti, two)
	if !reflect.DeepEqual(multi.Options, []ReplyCardOption{
		{Text: "A", AIPick: true}, {Text: "B", AIPick: true}}) {
		t.Fatalf("a multi card keeps both ai_picks: %+v", multi.Options)
	}
}

// select_mode defaults to single, is served back on every card, and is a closed
// set at the door (a 400, not the decoder's 422 — the same posture kind has).
func TestCreateReplyCardSelectMode(t *testing.T) {
	api := newTasksTestServer(t)

	defaulted := openUnboundCard(t, api, "", threeOptions())
	if defaulted.SelectMode != replyCardSelectModeSingle {
		t.Fatalf("an omitted select_mode must default to single, got %q", defaulted.SelectMode)
	}
	stored, _ := api.dal.GetReplyCard(defaulted.ID)
	if stored.SelectMode != replyCardSelectModeSingle {
		t.Fatalf("the default must be persisted, got %q", stored.SelectMode)
	}

	asked := openUnboundCard(t, api, replyCardSelectModeMulti, threeOptions())
	if asked.SelectMode != replyCardSelectModeMulti {
		t.Fatalf("select_mode=multi must be kept, got %q", asked.SelectMode)
	}

	rec := createCardRaw(t, api, "m-exec", map[string]any{
		"kind": "decision", "summary": "which way?", "options": threeOptions(),
		"select_mode": "many", "linked_task": nil,
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("an unknown select_mode must be a 400, got %d %s", rec.Code, rec.Body.String())
	}
	if msg := errorMessageOf(t, rec); msg != "select_mode must be 'single' or 'multi'" {
		t.Fatalf("refusal message: %q", msg)
	}
}

// The light list row is the agent-facing contract, so a multi-select answer must
// show EVERY circled option there. Reporting only the first would tell the asker
// the owner chose less than it did, with nothing malformed to notice.
func TestReplyCardListItemDigestCarriesEveryCircledOption(t *testing.T) {
	api := newTasksTestServer(t)
	card := openUnboundCard(t, api, replyCardSelectModeMulti, threeOptions())
	if rec := answerCard(t, api, card.ID,
		map[string]any{"option_idxs": []int{2, 0}}); rec.Code != http.StatusOK {
		t.Fatalf("answer: %d %s", rec.Code, rec.Body.String())
	}
	stored, _ := api.dal.GetReplyCard(card.ID)

	row, err := api.replyCardListItemOf(*stored)
	if err != nil {
		t.Fatalf("list item: %v", err)
	}
	if row.Answer == nil {
		t.Fatal("an answered row must carry the digest")
	}
	if !reflect.DeepEqual(row.Answer.OptionIdxs, []int{0, 2}) {
		t.Fatalf("digest indices: %v", row.Answer.OptionIdxs)
	}
	if !reflect.DeepEqual(row.Answer.Options, []string{"A", "C"}) {
		t.Fatalf("digest must carry every circled option's wording, got %v", row.Answer.Options)
	}
}

// The 00065 rebuild is where the "options[0] is the AI pick" convention is
// cashed in — the one and only time it is ever executed — so a card written
// under the OLD schema must come back with ai_pick on its first option, [3]
// where it held 3, NULL where it held NULL, and select_mode 'single'.
func TestReplyCardMultiSelectMigrationCarriesLegacyRowsForward(t *testing.T) {
	dal := newTestDAL(t)
	rows := []struct {
		id, options string
		answerIdx   any
	}{
		{"rc-legacy-answered", `["甲","乙"]`, 1},
		{"rc-legacy-unanswered", `["甲","乙"]`, nil},
		{"rc-legacy-nooptions", `[]`, nil},
	}
	for _, r := range rows {
		if _, err := dal.wdb.Exec(`INSERT INTO reply_card
			(id, kind, status, created_ts, options, select_mode, answer_option_idxs, summary)
			VALUES (?, 'decision', 'waiting', 1, ?, 'single', ?, 's')`,
			r.id, migrateLegacyOptions(t, dal, r.options),
			migrateLegacyAnswerIdx(t, dal, r.answerIdx)); err != nil {
			t.Fatalf("seed %s: %v", r.id, err)
		}
	}

	answered, err := dal.GetReplyCard("rc-legacy-answered")
	if err != nil {
		t.Fatalf("get answered: %v", err)
	}
	if !reflect.DeepEqual(answered.Options,
		[]ReplyCardOption{{Text: "甲", AIPick: true}, {Text: "乙"}}) {
		t.Fatalf("legacy options must carry ai_pick on index 0 only: %+v", answered.Options)
	}
	if !reflect.DeepEqual(answered.AnswerOptionIdxs, []int{1}) {
		t.Fatalf("legacy answer 1 must become [1], got %v", answered.AnswerOptionIdxs)
	}
	if answered.SelectMode != replyCardSelectModeSingle {
		t.Fatalf("a legacy card is single-select, got %q", answered.SelectMode)
	}

	unanswered, err := dal.GetReplyCard("rc-legacy-unanswered")
	if err != nil {
		t.Fatalf("get unanswered: %v", err)
	}
	if unanswered.AnswerOptionIdxs != nil {
		t.Fatalf("a legacy NULL answer must stay nil, got %v", unanswered.AnswerOptionIdxs)
	}

	none, err := dal.GetReplyCard("rc-legacy-nooptions")
	if err != nil {
		t.Fatalf("get nooptions: %v", err)
	}
	if len(none.Options) != 0 {
		t.Fatalf("a card with no options stays empty: %+v", none.Options)
	}
}

// migrateLegacyOptions runs the 00065 Up expression for one legacy options
// value, so the test exercises the SQL the migration ships rather than a Go
// re-implementation of it.
func migrateLegacyOptions(t *testing.T, dal *DAL, legacy string) string {
	t.Helper()
	var out string
	if err := dal.wdb.QueryRow(`SELECT json_group_array(json_object(
		'text', j.value,
		'ai_pick', json(CASE WHEN j.key = 0 THEN 'true' ELSE 'false' END)))
		FROM json_each(?) AS j`, legacy).Scan(&out); err != nil {
		t.Fatalf("carry options forward: %v", err)
	}
	return out
}

// migrateLegacyAnswerIdx runs the 00065 Up expression for one legacy
// answer_option_idx value (NULL stays NULL; 3 becomes [3]).
func migrateLegacyAnswerIdx(t *testing.T, dal *DAL, legacy any) any {
	t.Helper()
	var out any
	if err := dal.wdb.QueryRow(
		`SELECT CASE WHEN ? IS NULL THEN NULL ELSE json_array(?) END`,
		legacy, legacy).Scan(&out); err != nil {
		t.Fatalf("carry answer idx forward: %v", err)
	}
	return out
}
