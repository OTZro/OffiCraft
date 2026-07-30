package main

// The refusal faces of the two document-history routes. Each of these used to
// be reachable with no test at all, and three of them (corrupt row, foreign
// revision id, over-cap restore) fail in a direction that LOOKS like success:
// an empty list, a restore of the wrong document, or a document written past
// the 10,000-character cap the write faces refuse to cross.

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestListDocumentHistoryRefusesToRenderACorruptRevision(t *testing.T) {
	api := newTasksTestServer(t)
	rec := httptest.NewRecorder()
	api.HandleReplaceGlobalContextApiGlobalContextPost(rec, taskReq(t, http.MethodPost,
		"/api/global-context", map[string]any{"text": "one"}, "owner", "owner"))
	if rec.Code != http.StatusOK {
		t.Fatalf("seed write: %d %s", rec.Code, rec.Body.String())
	}
	if err := api.dal.SaveWithDocumentHistory("global_context", "global", "owner",
		func(sqlQuerier) (string, error) { return `{"text": not json`, nil },
		func(ex sqlExecer) error {
			return putUserContextOn(ex, UserContext{Text: "two"})
		}); err != nil {
		t.Fatal(err)
	}

	rec = httptest.NewRecorder()
	api.HandleListDocumentHistoryApiDocumentHistoryKindKeyGet(rec, taskReq(t, http.MethodGet,
		"/api/document-history/global_context/global", nil, "owner", "owner"), "global_context", "global")
	if rec.Code < 500 {
		t.Fatalf("listing a corrupt revision = %d %s, want a server error rather than a shorter list",
			rec.Code, rec.Body.String())
	}
}

func TestRestoreDocumentHistoryRefusesARevisionOfAnotherDocument(t *testing.T) {
	api := newTasksTestServer(t)
	owner := func(method, path string, body any) *http.Request {
		return taskReq(t, method, path, body, "owner", "owner")
	}
	for _, text := range []string{"one", "two"} {
		rec := httptest.NewRecorder()
		api.HandleReplaceGlobalContextApiGlobalContextPost(rec,
			owner(http.MethodPost, "/api/global-context", map[string]any{"text": text}))
		if rec.Code != http.StatusOK {
			t.Fatalf("seed write %q: %d %s", text, rec.Code, rec.Body.String())
		}
	}
	stored, err := api.dal.ListDocumentHistory("global_context", "global")
	if err != nil || len(stored) == 0 {
		t.Fatalf("seed history = %+v, %v", stored, err)
	}
	globalRevision := stored[0].ID

	// Same id, addressed under a different kind and under a different key of
	// the same kind: neither may resolve to the global-context revision.
	for _, addr := range []struct{ kind, key string }{
		{"role_definition", seedRoleAssistant},
		{"global_context", "not-the-global-document"},
	} {
		rec := httptest.NewRecorder()
		api.HandleRestoreDocumentHistoryApiDocumentHistoryKindKeyIdRestorePost(rec,
			owner(http.MethodPost, "/api/document-history/"+addr.kind+"/"+addr.key+"/restore", nil),
			addr.kind, addr.key, globalRevision)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("restore %s/%s with a foreign revision id = %d %s, want 404",
				addr.kind, addr.key, rec.Code, rec.Body.String())
		}
	}
	current, err := api.foldUserContextDTO()
	if err != nil {
		t.Fatal(err)
	}
	if current.Text != "two" {
		t.Fatalf("a refused restore changed the live document to %q", current.Text)
	}
}

func TestRestoreDocumentHistoryRefusesToReviveAnOverCapRevision(t *testing.T) {
	api := newTasksTestServer(t)
	role, taskType := seedRoleAssistant, "tm-cap"
	oversized := strings.Repeat("x", contextDocMaxChars+50)

	// The oversized document predates the cap (the cap never truncates what is
	// already stored), and the write that replaced it retained it as a version.
	if err := api.dal.PutLessons(Lessons{RoleKey: role, TaskType: taskType, Text: oversized}); err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	api.HandleReplaceLessonsApiLessonsRoleKeyTaskTypePost(rec, taskReq(t, http.MethodPost,
		"/api/lessons/"+role+"/"+taskType, map[string]any{"text": "short again"}, "owner", "owner"),
		role, taskType)
	if rec.Code != http.StatusOK {
		t.Fatalf("shrinking write: %d %s", rec.Code, rec.Body.String())
	}

	stored, err := api.dal.ListDocumentHistory("lessons", role+"::"+taskType)
	if err != nil || len(stored) == 0 {
		t.Fatalf("history = %+v, %v", stored, err)
	}
	rec = httptest.NewRecorder()
	api.HandleRestoreDocumentHistoryApiDocumentHistoryKindKeyIdRestorePost(rec, taskReq(t, http.MethodPost,
		"/api/document-history/lessons/"+role+"::"+taskType+"/restore", nil, "owner", "owner"),
		"lessons", role+"::"+taskType, stored[0].ID)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("restoring an over-cap revision = %d %s, want 400 — restore may not do what the "+
			"write faces refuse", rec.Code, rec.Body.String())
	}
	// Which 400: the cap's, not some other refusal that happens to share it.
	if !strings.Contains(rec.Body.String(), "size limit") {
		t.Fatalf("refusal = %s, want the document size limit", rec.Body.String())
	}
	current, err := api.foldLessonsDTO(role, taskType)
	if err != nil {
		t.Fatal(err)
	}
	if current.Text != "short again" {
		t.Fatalf("refused restore still wrote: live text is %d chars", len(current.Text))
	}
}

func TestDocumentHistoryRoutesRefuseUnknownKindsAndBlankKeys(t *testing.T) {
	api := newTasksTestServer(t)
	for _, addr := range []struct{ kind, key string }{
		{"member_avatar", "m-1"}, // a real document, but not one this feature versions
		{"global_context", ""},
		{"lessons", seedRoleAssistant},        // no task type — addresses nothing
		{"lessons", "::" + seedRoleAssistant}, // blank role half
		{"lessons", seedRoleAssistant + "::"}, // blank task-type half
	} {
		rec := httptest.NewRecorder()
		api.HandleListDocumentHistoryApiDocumentHistoryKindKeyGet(rec, taskReq(t, http.MethodGet,
			"/api/document-history/"+addr.kind+"/"+addr.key, nil, "owner", "owner"), addr.kind, addr.key)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("list %q/%q = %d %s, want 400", addr.kind, addr.key, rec.Code, rec.Body.String())
		}
	}
}

// Reading history is open to every authenticated caller; restoring is a write,
// so it takes the same floor as writing the document itself.
func TestRestoreDocumentHistoryKeepsEachDocumentsWriteFloor(t *testing.T) {
	api := newTasksTestServer(t)
	rec := httptest.NewRecorder()
	api.HandleReplaceGlobalContextApiGlobalContextPost(rec, taskReq(t, http.MethodPost,
		"/api/global-context", map[string]any{"text": "one"}, "owner", "owner"))
	if rec.Code != http.StatusOK {
		t.Fatalf("seed write: %d %s", rec.Code, rec.Body.String())
	}
	rec = httptest.NewRecorder()
	api.HandleReplaceGlobalContextApiGlobalContextPost(rec, taskReq(t, http.MethodPost,
		"/api/global-context", map[string]any{"text": "two"}, "owner", "owner"))
	if rec.Code != http.StatusOK {
		t.Fatalf("second write: %d %s", rec.Code, rec.Body.String())
	}
	stored, err := api.dal.ListDocumentHistory("global_context", "global")
	if err != nil || len(stored) == 0 {
		t.Fatalf("history = %+v, %v", stored, err)
	}

	// A plain agent may READ the history of the global context …
	rec = httptest.NewRecorder()
	api.HandleListDocumentHistoryApiDocumentHistoryKindKeyGet(rec, taskReq(t, http.MethodGet,
		"/api/document-history/global_context/global", nil, "m-agent", "agent"), "global_context", "global")
	if rec.Code != http.StatusOK {
		t.Fatalf("agent list = %d %s, want 200", rec.Code, rec.Body.String())
	}
	var visible []DocumentHistoryDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &visible); err != nil {
		t.Fatal(err)
	}
	if len(visible) == 0 {
		t.Fatal("agent read an empty history — the positive half of this test proves nothing")
	}

	// … but not restore it: replacing the global context is governance.
	rec = httptest.NewRecorder()
	api.HandleRestoreDocumentHistoryApiDocumentHistoryKindKeyIdRestorePost(rec, taskReq(t, http.MethodPost,
		"/api/document-history/global_context/global/restore", nil, "m-agent", "agent"),
		"global_context", "global", stored[0].ID)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("agent restore of the global context = %d %s, want 403", rec.Code, rec.Body.String())
	}
	current, err := api.foldUserContextDTO()
	if err != nil {
		t.Fatal(err)
	}
	if current.Text != "two" {
		t.Fatalf("a forbidden restore still wrote: live text = %q", current.Text)
	}
}
