package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestDocumentHistoryRestoreKeepsCurrentDocumentAndRestoresSnapshot(t *testing.T) {
	api := newTasksTestServer(t)
	for _, text := range []string{"one", "two", "three", "four"} {
		rec := httptest.NewRecorder()
		api.HandleReplaceGlobalContextApiGlobalContextPost(rec, taskReq(t, http.MethodPost,
			"/api/global-context", map[string]any{"text": text}, "owner", "owner"))
		if rec.Code != http.StatusOK {
			t.Fatalf("write %q: status=%d body=%s", text, rec.Code, rec.Body.String())
		}
	}

	rec := httptest.NewRecorder()
	api.HandleListDocumentHistoryApiDocumentHistoryKindKeyGet(rec, taskReq(t, http.MethodGet,
		"/api/document-history/global_context/global", nil, "owner", "owner"), "global_context", "global")
	if rec.Code != http.StatusOK {
		t.Fatalf("list history: status=%d body=%s", rec.Code, rec.Body.String())
	}
	var history []DocumentHistoryDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &history); err != nil {
		t.Fatal(err)
	}
	if len(history) != 3 || history[0].Content["text"] != "three" || history[2].Content["text"] != "one" {
		t.Fatalf("retained history = %+v, want three versions from three through one", history)
	}

	rec = httptest.NewRecorder()
	api.HandleRestoreDocumentHistoryApiDocumentHistoryKindKeyIdRestorePost(rec, taskReq(t, http.MethodPost,
		"/api/document-history/global_context/global/restore", nil, "owner", "owner"), "global_context", "global", history[1].Id)
	if rec.Code != http.StatusOK {
		t.Fatalf("restore: status=%d body=%s", rec.Code, rec.Body.String())
	}
	current, err := api.foldUserContextDTO()
	if err != nil {
		t.Fatal(err)
	}
	if current.Text != "two" {
		t.Fatalf("restored text = %q, want two", current.Text)
	}
	stored, err := api.dal.ListDocumentHistory("global_context", "global")
	if err != nil {
		t.Fatal(err)
	}
	if len(stored) != 3 {
		t.Fatalf("history after restore = %d versions, want 3", len(stored))
	}
	var restoredCurrent map[string]string
	if err := json.Unmarshal([]byte(stored[0].ContentJSON), &restoredCurrent); err != nil {
		t.Fatal(err)
	}
	if restoredCurrent["text"] != "four" {
		t.Fatalf("restore did not retain the replaced current document: %+v", restoredCurrent)
	}
}

func TestDocumentHistoryRestorePreservesOverlayTombstones(t *testing.T) {
	api := newTasksTestServer(t)
	ownerReq := func(method, path string, body any) *http.Request {
		return taskReq(t, method, path, body, "owner", "owner")
	}
	list := func(kind, key string) []DocumentHistoryDTO {
		t.Helper()
		rec := httptest.NewRecorder()
		api.HandleListDocumentHistoryApiDocumentHistoryKindKeyGet(rec,
			ownerReq(http.MethodGet, "/api/document-history/"+kind+"/"+key, nil), kind, key)
		if rec.Code != http.StatusOK {
			t.Fatalf("list %s/%s: status=%d body=%s", kind, key, rec.Code, rec.Body.String())
		}
		var history []DocumentHistoryDTO
		if err := json.Unmarshal(rec.Body.Bytes(), &history); err != nil {
			t.Fatal(err)
		}
		return history
	}
	restore := func(kind, key string, id int64) {
		t.Helper()
		rec := httptest.NewRecorder()
		api.HandleRestoreDocumentHistoryApiDocumentHistoryKindKeyIdRestorePost(rec,
			ownerReq(http.MethodPost, "/api/document-history/"+kind+"/"+key+"/restore", nil), kind, key, id)
		if rec.Code != http.StatusOK {
			t.Fatalf("restore %s/%s: status=%d body=%s", kind, key, rec.Code, rec.Body.String())
		}
	}

	// A subsequent write records the reset's persisted tombstone, then restore
	// must preserve it rather than materializing a non-default overlay.
	for _, text := range []string{"custom", "later"} {
		rec := httptest.NewRecorder()
		api.HandleReplaceGlobalContextApiGlobalContextPost(rec,
			ownerReq(http.MethodPost, "/api/global-context", map[string]any{"text": text}))
		if rec.Code != http.StatusOK {
			t.Fatalf("global write %q: %d %s", text, rec.Code, rec.Body.String())
		}
		if text == "custom" {
			rec = httptest.NewRecorder()
			api.HandleResetGlobalContextApiGlobalContextResetPost(rec, ownerReq(http.MethodPost, "/api/global-context/reset", nil))
			if rec.Code != http.StatusOK {
				t.Fatalf("global reset: %d %s", rec.Code, rec.Body.String())
			}
		}
	}
	globalHistory := list("global_context", "global")
	if globalHistory[0].Content["tombstoned"] != "true" {
		t.Fatalf("global reset snapshot = %+v, want tombstoned=true", globalHistory[0].Content)
	}
	restore("global_context", "global", globalHistory[0].Id)
	global, err := api.dal.GetUserContext()
	if err != nil || global == nil || !global.Tombstoned {
		t.Fatalf("restored global overlay = %+v, %v; want tombstone", global, err)
	}

	// A custom role can also carry a tombstone in historical data (for example,
	// a later version of the product may make its deletion restorable). Restore
	// must not turn that state into a live overlay.
	role := "r-history"
	if err := api.dal.PutRoleDef(RoleDef{RoleKey: role, Name: "later", DefinitionMD: "later"}); err != nil {
		t.Fatal(err)
	}
	tombstonedRole, err := roleDefHistorySnapshot(&RoleDef{RoleKey: role, Tombstoned: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := api.dal.SaveWithDocumentHistory("role_definition", role, tombstonedRole, "owner", func(ex sqlExecer) error {
		return putRoleDefOn(ex, RoleDef{RoleKey: role, Name: "later", DefinitionMD: "later"})
	}); err != nil {
		t.Fatal(err)
	}
	roleHistory := list("role_definition", role)
	if roleHistory[0].Content["tombstoned"] != "true" {
		t.Fatalf("role reset snapshot = %+v, want tombstoned=true", roleHistory[0].Content)
	}
	restore("role_definition", role, roleHistory[0].Id)
	roleOverlay, err := api.dal.GetRoleDef(role)
	if err != nil || roleOverlay == nil || !roleOverlay.Tombstoned {
		t.Fatalf("restored role overlay = %+v, %v; want tombstone", roleOverlay, err)
	}

	if err := api.dal.PutLessons(Lessons{RoleKey: role, TaskType: seedLessonsTaskType, Tombstoned: true}); err != nil {
		t.Fatal(err)
	}
	lessonsSnapshot, err := lessonsHistorySnapshot(&Lessons{RoleKey: role, TaskType: seedLessonsTaskType, Tombstoned: true})
	if err != nil {
		t.Fatal(err)
	}
	var lessonsContent map[string]string
	if err := json.Unmarshal([]byte(lessonsSnapshot), &lessonsContent); err != nil || !historyTombstoned(lessonsContent) {
		t.Fatalf("lessons tombstone snapshot = %s, %v; want preserved tombstone", lessonsSnapshot, err)
	}
}
