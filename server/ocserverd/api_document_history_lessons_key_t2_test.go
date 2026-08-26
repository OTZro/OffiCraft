package main

// T-2 step B closed the lessons `task_type` axis. This file guards the DOOR
// that closing it walked past.
//
// Before T-2, historyKeyParts split a lessons key on "::" and returned
// valid=false for anything that was not exactly two non-empty halves. That
// refusal is the ENTIRE argument 00061 wrote down for leaving three malformed
// lessons history rows in place: "documentHistoryAllowed refuses such a key
// with 400 before any restore runs, so none of them can reseed anything".
// T-2 reduced historyKeyParts to `key != ""`, which silently retired that
// refusal — and 00062 copied 00061's "deliberately left alone" wording without
// noticing its premise had gone. A restore under `assistant::` then answered
// 200 and MATERIALISED a `lessons` row keyed `assistant::`: a row no role
// carries, that get_lessons cannot see, that peek_doc_sizes does not list,
// that DeleteLessonsForRole cannot reach, that spends the lessons cap anyway,
// and that grows a history of its own. That is the exact shape of hidden
// drawer this ticket line exists to remove, rebuilt one door over.
//
// 🔴 THE POSITIVE CASE IS FIRST AND IT IS NOT DECORATION. A door can be shut
// too hard, and nothing else in the build would say so: if the whole lessons
// history route broke, every negative assertion below would still pass and
// would read as "the door is closed". TestDocumentHistoryStillServesAPlainLessonsRoleKey
// is what separates "closed to malformed keys" from "closed".

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// malformedLessonsHistoryKeys are the three shapes 00061 and 00062 both spare
// in the table, named here so the refusal and the migrations talk about one
// list. `a::b::c` is not one of the stored shapes — it is the "more than two
// halves" case, present so the rule is tested as "contains ::" rather than as
// "matches one of three literals".
var malformedLessonsHistoryKeys = []struct{ key, why string }{
	{"assistant::", "empty task_type half — the shape 00061 §2 leaves in the table"},
	{"::general", "empty role_key half — same"},
	{"assistant::general", "the old composite shape in full"},
	{"a::b::c", "more than two halves — never valid under any rule"},
}

// seedLegacyLessonsHistoryRow puts a row into document_history the way a
// pre-T-2 station holds one: an untouched legacy key, written past the API so
// the test does not depend on a write face that may itself refuse the key.
func seedLegacyLessonsHistoryRow(t *testing.T, api *apiServer, key, text string) int64 {
	t.Helper()
	res, err := api.dal.wdb.Exec(`
		INSERT INTO document_history (document_kind, document_key, content_json, created_ts, actor_id)
		VALUES ('lessons', ?, ?, 1.0, 'owner')`, key, `{"text":`+jsonQuote(text)+`,"tombstoned":"false"}`)
	if err != nil {
		t.Fatal(err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func jsonQuote(s string) string { return `"` + s + `"` }

// TestDocumentHistoryStillServesAPlainLessonsRoleKey is the POSITIVE control
// for the refusal below, and it runs first on purpose: a refusal that also
// refuses the ordinary key is not a fix, it is an outage that every negative
// assertion in this file would report as success.
func TestDocumentHistoryStillServesAPlainLessonsRoleKey(t *testing.T) {
	api := newTasksTestServer(t)
	owner := func(method, path string, body any) *http.Request {
		return taskReq(t, method, path, body, "owner", "owner")
	}
	for _, text := range []string{"first lesson", "second lesson"} {
		rec := httptest.NewRecorder()
		api.HandleReplaceLessonsApiLessonsRoleKeyPost(rec,
			owner(http.MethodPost, "/api/lessons/"+seedRoleAssistant, map[string]any{"text": text}),
			seedRoleAssistant)
		if rec.Code != http.StatusOK {
			t.Fatalf("seed lessons write %q: %d %s", text, rec.Code, rec.Body.String())
		}
	}

	rec := httptest.NewRecorder()
	api.HandleListDocumentHistoryApiDocumentHistoryKindKeyGet(rec,
		owner(http.MethodGet, "/api/document-history/lessons/"+seedRoleAssistant, nil),
		"lessons", seedRoleAssistant)
	if rec.Code != http.StatusOK {
		t.Fatalf("listing the history of a PLAIN lessons role key = %d %s, want 200 — "+
			"the malformed-key refusal must not have closed the ordinary door",
			rec.Code, rec.Body.String())
	}
	stored, err := api.dal.ListDocumentHistory("lessons", seedRoleAssistant)
	if err != nil || len(stored) == 0 {
		t.Fatalf("retained revisions for %q = %+v, %v, want at least one", seedRoleAssistant, stored, err)
	}

	rec = httptest.NewRecorder()
	api.HandleRestoreDocumentHistoryApiDocumentHistoryKindKeyIdRestorePost(rec,
		owner(http.MethodPost, "/api/document-history/lessons/"+seedRoleAssistant+"/restore", nil),
		"lessons", seedRoleAssistant, stored[len(stored)-1].ID)
	if rec.Code != http.StatusOK {
		t.Fatalf("restoring a revision of a PLAIN lessons role key = %d %s, want 200 — "+
			"same reason: the ordinary door has to stay open",
			rec.Code, rec.Body.String())
	}
	live, err := api.dal.GetLessons(seedRoleAssistant)
	if err != nil || live == nil {
		t.Fatalf("lessons after restore = %+v, %v, want the row to exist", live, err)
	}
	if live.Text != "first lesson" {
		t.Fatalf("lessons text after restoring the FIRST revision = %q, want %q",
			live.Text, "first lesson")
	}
}

// TestDocumentHistoryRefusesAMalformedLessonsKey is the negative half. Each
// shape must be refused on BOTH faces — a list that answers 200 with an empty
// array is already a lie about a key that names nothing, and a restore that
// answers 200 writes the hidden drawer.
func TestDocumentHistoryRefusesAMalformedLessonsKey(t *testing.T) {
	for _, tc := range malformedLessonsHistoryKeys {
		t.Run(tc.key, func(t *testing.T) {
			api := newTasksTestServer(t)
			owner := func(method, path string, body any) *http.Request {
				return taskReq(t, method, path, body, "owner", "owner")
			}
			id := seedLegacyLessonsHistoryRow(t, api, tc.key, "GHOST TEXT")

			rec := httptest.NewRecorder()
			api.HandleListDocumentHistoryApiDocumentHistoryKindKeyGet(rec,
				owner(http.MethodGet, "/api/document-history/lessons/"+tc.key, nil),
				"lessons", tc.key)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("listing the history of malformed lessons key %q (%s) = %d %s, want 400 — "+
					"a lessons key carrying '::' names nothing since T-2 removed the task_type axis",
					tc.key, tc.why, rec.Code, rec.Body.String())
			}

			rec = httptest.NewRecorder()
			api.HandleRestoreDocumentHistoryApiDocumentHistoryKindKeyIdRestorePost(rec,
				owner(http.MethodPost, "/api/document-history/lessons/"+tc.key+"/restore", nil),
				"lessons", tc.key, id)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("restoring malformed lessons key %q (%s) = %d %s, want 400",
					tc.key, tc.why, rec.Code, rec.Body.String())
			}

			ghost, err := api.dal.GetLessons(tc.key)
			if err != nil {
				t.Fatal(err)
			}
			if ghost != nil {
				t.Fatalf("restoring malformed lessons key %q MATERIALISED a lessons row %+v — "+
					"a row no role carries, invisible to get_lessons and peek_doc_sizes, "+
					"unreachable by DeleteLessonsForRole, and spending the lessons cap anyway. "+
					"This is the hidden drawer T-2 exists to remove.", tc.key, ghost)
			}
		})
	}
}

// TestDocumentHistoryRefusalIsScopedToLessons is the other over-reach control:
// "::" is only meaningless for LESSONS. Other kinds have never split on it, so
// a key containing "::" must keep whatever answer that kind already gave.
func TestDocumentHistoryRefusalIsScopedToLessons(t *testing.T) {
	api := newTasksTestServer(t)
	rec := httptest.NewRecorder()
	api.HandleListDocumentHistoryApiDocumentHistoryKindKeyGet(rec,
		taskReq(t, http.MethodGet, "/api/document-history/role_definition/a::b", nil, "owner", "owner"),
		"role_definition", "a::b")
	if rec.Code != http.StatusOK {
		t.Fatalf("listing role_definition history for a key containing '::' = %d %s, want 200 "+
			"with an empty list — the lessons refusal must not leak onto other kinds",
			rec.Code, rec.Body.String())
	}
}
