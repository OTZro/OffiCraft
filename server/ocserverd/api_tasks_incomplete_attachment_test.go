package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// T-e2b2 / independent review F2: the task-message face was named in the change
// as one of the fixed faces, yet nothing tested it — the reviewer reverted ONLY
// api_tasks.go to the old pre-filter and the entire suite (Go plus 973
// conformance cases) stayed green with the silent drop fully restored there.
// This is that missing coverage: an item carrying neither id nor data_b64 is
// refused, and the message the owner thought carried a file is not posted.
func TestTaskMessageRefusesIncompleteAttachment(t *testing.T) {
	api := newTasksTestServer(t)
	task := createAdHocTask(t, api, "m-exec")

	rec := httptest.NewRecorder()
	api.HandlePostTaskMessageApiTasksTaskIdMessagePost(rec,
		taskReq(t, "POST", "/api/tasks/"+task.ID+"/message",
			map[string]any{
				"body": "see attached",
				"attachments": []any{
					map[string]any{"filename": "ghost.pdf", "mime": "application/pdf"},
				},
			}, wireOwnerID, "owner"),
		task.ID)
	if rec.Code != http.StatusBadRequest ||
		!strings.Contains(rec.Body.String(), "neither id nor data_b64") {
		t.Fatalf("want 400 naming the missing id/data_b64, got %d %s",
			rec.Code, rec.Body.String())
	}
}
