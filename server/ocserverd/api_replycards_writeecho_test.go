package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// The reply-card WRITE endpoints echo a WHOLE card — the same object
// get_reply_card serves, byte for byte.
//
// 🔴 WHY THIS EXISTS. The cockpit's action path now reconciles from the write's
// OWN response instead of waiting for a `reply_card` SSE frame
// (frontend/src/hooks/useReplyCards.tsx, `adoptWrite`): a lost or absent frame
// used to leave an answered card rendering as waiting, so the owner clicked it
// again and hit a 409. That fix is only as good as this promise. If a write ever
// answered with a NARROWER projection — the T-3f31 light row, or anything short
// of the full card — the cockpit would swap a fully-rendered card for a partial
// one, and the client's own tests could not tell: they run against api/mock,
// which returns whole cards by construction.
//
// So the client's premise is pinned HERE, on the real handlers over a real DAL.
//
// 🔴 THE TWO HALVES CATCH DIFFERENT THINGS, AND THE EARLIER VERSION OF THIS NOTE
// GOT THE CREDIT BACKWARDS. It said the identity compare was the point and the
// corpus check was mere hygiene. Measured, it is the other way round for the
// obvious mutant:
//   • `HandleGetReplyCard…` ends in the SAME `s.writeReplyCard` tail, so anything
//     that changes that tail moves the echo AND the GET together and `got != want`
//     can never fire. Making the tail serve the LIGHT row reddens these tests
//     only through the anti-tautology check below (verified: delete those three
//     field assertions and that mutant goes completely green).
//   • The identity compare earns its place on the OTHER failure shape — the write
//     path drifting ALONE. Swapping just the write verbs to the bare
//     newReplyCardDTO (GET untouched) reddens `got != want` on answer and
//     re-answer. That is the realistic regression: someone "optimises" one verb.
// Both are needed; neither is decoration. The identity form is also what keeps
// this from going stale as the DTO grows fields, which a checklist would not.
//
// This is the same promise `?view=full` already had to make for the list wire
// (api_replycards_viewfull_test.go); it now covers the three write verbs too.
// All four surfaces share one tail (`writeReplyCard` → `replyCardDTOOf`), which
// is what makes the promise cheap to keep — but "they happen to share a tail
// today" is not a test, and swapping any one of them for the bare
// newReplyCardDTO reddens nothing without a task-bound card in the corpus (that
// join is the only thing replyCardDTOOf adds — measured in the view=full file).
// Hence every case below answers a card bound to a LIVE task.

// getReplyCardRaw fetches the card through the single-card endpoint — the shape
// the cockpit's own per-card refetch (ChatReplyCard) and its full list rows are
// both built from.
func getReplyCardRaw(t *testing.T, api *apiServer, cardID string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	api.HandleGetReplyCardApiReplyCardsCardIdGet(rec,
		taskReq(t, "GET", "/api/reply-cards/"+cardID, nil, "owner", "owner"), cardID)
	return rec
}

// boundWaitingCard opens a card that is bound to a live task and its current
// step — the shape whose DTO exercises the task-reference join.
func boundWaitingCard(t *testing.T, api *apiServer) replyCardDTO {
	t.Helper()
	task := createAdHocTask(t, api, "m-exec")
	view := submitPlan(t, api, task.ID, "m-exec", []map[string]any{
		{"name": "work", "dod": "d"},
	})
	startFirstStep(t, api, task.ID, "m-exec")
	// A real BODY and a real option list on purpose: those are exactly the two
	// things the T-3f31 light row drops, so without them a light projection and a
	// whole card would be indistinguishable and the identity compare below would
	// pin nothing (openPlainCard's fixture leaves body empty).
	rec := createCardRaw(t, api, "m-exec", map[string]any{
		"kind":        "decision",
		"summary":     "which way?",
		"body":        "the long ask body that the light row does not carry",
		"options":     []map[string]any{{"text": "AI 建議:照做"}, {"text": "先等等"}},
		"linked_task": map[string]any{"task_id": task.ID, "step_id": view.Steps[0].ID},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("create card: %d %s", rec.Code, rec.Body.String())
	}
	card := decodeBody[replyCardDTO](t, rec)
	if card.Task == nil {
		t.Fatalf("corpus is not exercising the task join: %+v", card)
	}
	return card
}

// assertEchoIsTheWholeCard is the contract: what the write returned is exactly
// what a fresh get_reply_card returns.
func assertEchoIsTheWholeCard(t *testing.T, api *apiServer, cardID string, echo *httptest.ResponseRecorder) {
	t.Helper()
	if echo.Code != http.StatusOK {
		t.Fatalf("write: %d %s", echo.Code, echo.Body.String())
	}
	fresh := getReplyCardRaw(t, api, cardID)
	if fresh.Code != http.StatusOK {
		t.Fatalf("get_reply_card: %d %s", fresh.Code, fresh.Body.String())
	}
	got := string(compact(t, echo.Body.Bytes()))
	want := string(compact(t, fresh.Body.Bytes()))
	if got != want {
		t.Fatalf("the write echo is not the whole card:\n echo = %s\nfresh = %s", got, want)
	}
	// Anti-tautology: prove the corpus can tell a whole card from the light row.
	// Without this, two identical LIGHT projections would satisfy the compare
	// above and this test would pin the wrong shape.
	dto := decodeBody[replyCardDTO](t, echo)
	if dto.Body == "" || len(dto.Options) == 0 || dto.Task == nil {
		t.Fatalf("corpus cannot distinguish the projections (body/options/task): %+v", dto)
	}
}

func TestAnswerEchoesTheWholeCard(t *testing.T) {
	api := newTasksTestServer(t)
	card := boundWaitingCard(t, api)

	rec := answerCard(t, api, card.ID, map[string]any{
		"option_idxs": []int{0},
		"text":        "就這樣辦",
	})
	assertEchoIsTheWholeCard(t, api, card.ID, rec)

	// The status the cockpit adopts must be the settled one — this is what makes
	// the card leave the waiting pane.
	if dto := decodeBody[replyCardDTO](t, rec); dto.Status != replyCardStatusAnswered {
		t.Fatalf("answer echo status: got %q want answered", dto.Status)
	}
}

func TestReanswerEchoesTheWholeCard(t *testing.T) {
	api := newTasksTestServer(t)
	card := boundWaitingCard(t, api)
	if rec := answerCard(t, api, card.ID, map[string]any{"option_idxs": []int{0}}); rec.Code != http.StatusOK {
		t.Fatalf("seed answer: %d %s", rec.Code, rec.Body.String())
	}

	put := httptest.NewRecorder()
	api.HandleReanswerReplyCardApiReplyCardsCardIdAnswerPut(put,
		taskReq(t, "PUT", "/x", map[string]any{
			"option_idxs": []int{1},
			"text":        "改主意了",
		}, "owner", "owner"), card.ID)
	assertEchoIsTheWholeCard(t, api, card.ID, put)

	// The revision itself has to be IN the echo — that is the value the cockpit
	// renders in place of the previous answer.
	dto := decodeBody[replyCardDTO](t, put)
	if dto.Answer == nil || dto.Answer.Text != "改主意了" {
		t.Fatalf("re-answer echo must carry the new answer: %+v", dto.Answer)
	}
}

func TestExpireEchoesTheWholeCard(t *testing.T) {
	api := newTasksTestServer(t)
	card := boundWaitingCard(t, api)

	rec := expireCardReq(t, api, card.ID, "owner", "owner")
	assertEchoIsTheWholeCard(t, api, card.ID, rec)

	dto := decodeBody[replyCardDTO](t, rec)
	if dto.Status != replyCardStatusExpired {
		t.Fatalf("expire echo status: got %q want expired", dto.Status)
	}
	if dto.ExpiredTS == nil || *dto.ExpiredTS <= 0 {
		t.Fatalf("expire echo must carry the terminal stamp: %+v", dto.ExpiredTS)
	}
}
