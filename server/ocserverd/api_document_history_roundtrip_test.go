package main

// Round trips through the REAL handlers for the three document families the
// global-context test does not reach: a role definition, a lessons doc (whose
// history key is the role::task_type pair), and a task manual (whose revision
// must be all four content fields as one coherent version, never a mixture of
// fields captured at different times).

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

type historyFixture struct {
	t   *testing.T
	api *apiServer
}

func newHistoryFixture(t *testing.T) historyFixture {
	t.Helper()
	return historyFixture{t: t, api: newTasksTestServer(t)}
}

func (f historyFixture) req(method, path string, body any) *http.Request {
	return taskReq(f.t, method, path, body, "owner", "owner")
}

func (f historyFixture) list(kind, key string) []DocumentHistoryDTO {
	f.t.Helper()
	rec := httptest.NewRecorder()
	f.api.HandleListDocumentHistoryApiDocumentHistoryKindKeyGet(rec,
		f.req(http.MethodGet, "/api/document-history/"+kind+"/"+key, nil), kind, key)
	if rec.Code != http.StatusOK {
		f.t.Fatalf("list %s/%s: status=%d body=%s", kind, key, rec.Code, rec.Body.String())
	}
	var history []DocumentHistoryDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &history); err != nil {
		f.t.Fatal(err)
	}
	return history
}

func (f historyFixture) restore(kind, key string, id int64) {
	f.t.Helper()
	rec := httptest.NewRecorder()
	f.api.HandleRestoreDocumentHistoryApiDocumentHistoryKindKeyIdRestorePost(rec,
		f.req(http.MethodPost, "/api/document-history/"+kind+"/"+key+"/"+"restore", nil), kind, key, id)
	if rec.Code != http.StatusOK {
		f.t.Fatalf("restore %s/%s: status=%d body=%s", kind, key, rec.Code, rec.Body.String())
	}
}

func TestDocumentHistoryRoundTripsARoleDefinition(t *testing.T) {
	f := newHistoryFixture(t)
	role := seedRoleAssistant
	for _, definition := range []string{"first", "second", "third"} {
		rec := httptest.NewRecorder()
		f.api.HandleUpdateRoleApiRolesRolePost(rec,
			f.req(http.MethodPost, "/api/roles/"+role, map[string]any{"definition_md": definition}), role)
		if rec.Code != http.StatusOK {
			t.Fatalf("update role %q: status=%d body=%s", definition, rec.Code, rec.Body.String())
		}
	}

	// Two, not three: the first write customized a seed role that had no
	// overlay row yet, and "no overlay" is the default state rather than a
	// version of the document.
	history := f.list("role_definition", role)
	if len(history) != 2 {
		t.Fatalf("retained %d revisions, want 2: %+v", len(history), history)
	}
	if history[0].Content["definition_md"] != "second" {
		t.Fatalf("newest revision = %+v, want the replaced definition \"second\"", history[0].Content)
	}

	f.restore("role_definition", role, history[0].Id)
	current, err := f.api.foldRoleDefDTO(role)
	if err != nil || current == nil {
		t.Fatalf("fold role: %+v, %v", current, err)
	}
	if current.DefinitionMD != "second" {
		t.Fatalf("restored definition = %q, want second", current.DefinitionMD)
	}
	// The restore is itself a write, so the document it replaced is retained.
	after := f.list("role_definition", role)
	if after[0].Content["definition_md"] != "third" {
		t.Fatalf("restore did not retain the definition it replaced: %+v", after[0].Content)
	}
}

func TestDocumentHistoryRoundTripsLessonsUnderItsPairKey(t *testing.T) {
	f := newHistoryFixture(t)
	role, taskType := seedRoleAssistant, "tm-history"
	for _, text := range []string{"lesson one", "lesson two", "lesson three"} {
		rec := httptest.NewRecorder()
		f.api.HandleReplaceLessonsApiLessonsRoleKeyTaskTypePost(rec,
			f.req(http.MethodPost, "/api/lessons/"+role+"/"+taskType, map[string]any{"text": text}),
			role, taskType)
		if rec.Code != http.StatusOK {
			t.Fatalf("replace lessons %q: status=%d body=%s", text, rec.Code, rec.Body.String())
		}
	}

	key := role + "::" + taskType
	history := f.list("lessons", key)
	if len(history) == 0 || history[0].Content["text"] != "lesson two" {
		t.Fatalf("retained lessons history = %+v, want the replaced \"lesson two\" newest", history)
	}

	f.restore("lessons", key, history[0].Id)
	current, err := f.api.foldLessonsDTO(role, taskType)
	if err != nil {
		t.Fatal(err)
	}
	if current.Text != "lesson two" {
		t.Fatalf("restored lessons = %q, want \"lesson two\"", current.Text)
	}

	// The pair key is what addresses the document: a key that names no task
	// type addresses nothing and must be refused rather than read as the role.
	rec := httptest.NewRecorder()
	f.api.HandleListDocumentHistoryApiDocumentHistoryKindKeyGet(rec,
		f.req(http.MethodGet, "/api/document-history/lessons/"+role, nil), "lessons", role)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("list lessons history without a task type: status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestDocumentHistoryRestoresATaskManualAsOneCoherentRevision(t *testing.T) {
	f := newHistoryFixture(t)
	rec := httptest.NewRecorder()
	f.api.HandleCreateTaskManualApiTaskManualsPost(rec,
		f.req(http.MethodPost, "/api/task-manuals", map[string]any{"display_name": "History"}))
	if rec.Code != http.StatusOK {
		t.Fatalf("create manual: status=%d body=%s", rec.Code, rec.Body.String())
	}
	var created taskManualDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	typeKey := created.TypeKey

	update := func(body map[string]any) {
		t.Helper()
		rec := httptest.NewRecorder()
		f.api.HandleUpdateTaskManualApiTaskManualsTypeKeyPost(rec,
			f.req(http.MethodPost, "/api/task-manuals/"+typeKey, body), typeKey)
		if rec.Code != http.StatusOK {
			t.Fatalf("update manual %+v: status=%d body=%s", body, rec.Code, rec.Body.String())
		}
	}
	// One version where every field says "v1", then a write that touches only
	// one field. Restoring the v1 revision must bring back all four together.
	update(map[string]any{"purpose": "purpose v1", "sop_md": "sop v1", "learnings": "learnings v1"})
	update(map[string]any{"learnings": "learnings v2"})

	history := f.list("task_manual", typeKey)
	if len(history) == 0 {
		t.Fatal("task manual kept no history")
	}
	v1 := history[0].Content
	if v1["purpose"] != "purpose v1" || v1["sop_md"] != "sop v1" || v1["learnings"] != "learnings v1" {
		t.Fatalf("newest revision = %+v, want every field as of v1", v1)
	}

	update(map[string]any{"purpose": "purpose v3", "sop_md": "sop v3"})
	f.restore("task_manual", typeKey, history[0].Id)

	restored, err := f.api.dal.GetTaskManual(typeKey)
	if err != nil || restored == nil {
		t.Fatalf("read manual: %+v, %v", restored, err)
	}
	if restored.Purpose != "purpose v1" || restored.SopMD != "sop v1" || restored.Learnings != "learnings v1" {
		t.Fatalf("restored manual = %+v, want all four fields back at v1", *restored)
	}
}
