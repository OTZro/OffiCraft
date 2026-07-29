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
