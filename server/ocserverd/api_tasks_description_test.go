package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// ── T-e271 任務描述可編輯 guards ─────────────────────────────────────────────
//
// Every case below reads the value back out through a real read path, never
// merely asserting a status code: the ticket exists because a documented
// capability ("agents can edit the task description separately", said in
// update_step_note's own tool description) turned out to name nothing at all,
// and a test that only checked for a 200 would have been just as happy against
// that nothing.

func writeTaskDescription(t *testing.T, api *apiServer, taskID, caller, scope string, body map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	api.HandleUpdateTaskDescriptionApiTasksTaskIdDescriptionPost(rec,
		taskReq(t, "POST", "/api/tasks/"+taskID+"/description", body, caller, scope),
		taskID)
	return rec
}

// readTaskDescription re-reads the description through get_task — the path the
// cockpit and every agent actually use, not the DAL.
func readTaskDescription(t *testing.T, api *apiServer, taskID string) string {
	t.Helper()
	rec := httptest.NewRecorder()
	api.HandleGetTaskApiTasksTaskIdGet(rec,
		taskReq(t, "GET", "/api/tasks/"+taskID, nil, "m-exec", "agent"), taskID)
	if rec.Code != http.StatusOK {
		t.Fatalf("get task: %d %s", rec.Code, rec.Body.String())
	}
	return decodeBody[taskDTO](t, rec).Description
}

func listTaskDescriptionHistory(t *testing.T, api *apiServer, taskID, caller, scope string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	api.HandleListDocumentHistoryApiDocumentHistoryKindKeyGet(rec,
		taskReq(t, "GET", "/api/document-history/task_description/"+taskID, nil, caller, scope),
		docKindTaskDescription, taskID)
	return rec
}

// terminateTask closes a task through the owner's terminate route, so the
// terminal-state cases below face a genuinely closed task rather than a
// hand-poked status column.
func terminateTask(t *testing.T, api *apiServer, taskID string) {
	t.Helper()
	rec := httptest.NewRecorder()
	api.HandleTerminateTaskApiTasksTaskIdTerminatePost(rec,
		taskReq(t, "POST", "/api/tasks/"+taskID+"/terminate", nil, "owner", "owner"),
		taskID)
	if rec.Code != http.StatusOK {
		t.Fatalf("terminate: %d %s", rec.Code, rec.Body.String())
	}
}

// TestTaskDescriptionRoundTripsThroughTheTaskView is the core assertion: the
// corrected wording is what the next reader sees. Dropping the description
// assignment from the handler, or the column from SetTaskDescriptionOn, reddens
// this — both are ways for the write to answer 200 and change nothing.
func TestTaskDescriptionRoundTripsThroughTheTaskView(t *testing.T) {
	api := newTasksTestServer(t)
	task := createAdHocTask(t, api, "m-exec")
	const corrected = "更正:這張票要的是「描述可編輯」,不是步驟備註"

	rec := writeTaskDescription(t, api, task.ID, "m-exec", "agent",
		map[string]any{"description": corrected})
	if rec.Code != http.StatusOK {
		t.Fatalf("write description: %d %s", rec.Code, rec.Body.String())
	}
	if got := decodeBody[taskDTO](t, rec).Description; got != corrected {
		t.Fatalf("response description = %q, want %q", got, corrected)
	}
	if got := readTaskDescription(t, api, task.ID); got != corrected {
		t.Fatalf("description read back = %q, want %q", got, corrected)
	}
}

// Ruling 1: the EXECUTOR may edit; the creator earns no standing from having
// created the task. The counterfactual matters here — m-creator is the verified
// creator of this very task, so a handler that admitted creators would pass a
// test written against an unrelated stranger.
func TestTaskDescriptionCreatorIsNotTheEditor(t *testing.T) {
	api := newTasksTestServer(t)
	putMemberRow(t, api, "m-creator", KindAssistant, "")
	putMemberRow(t, api, "m-exec", KindAssistant, "")

	rec := httptest.NewRecorder()
	api.HandleCreateTaskApiTasksPost(rec, taskReq(t, "POST", "/api/tasks",
		map[string]any{"title": "unit task", "executor_member_id": "m-exec"},
		"owner", "owner"))
	if rec.Code != http.StatusOK {
		t.Fatalf("create task: %d %s", rec.Code, rec.Body.String())
	}
	task := decodeBody[taskCreateResultDTO](t, rec).Task

	// Hand the task to m-creator's twin: reassign is not needed — creating it
	// under an owner token and then having a plain member try to edit is the
	// same shape. Use a plain member who is neither executor nor admin.
	if got := writeTaskDescription(t, api, task.ID, "m-creator", "agent",
		map[string]any{"description": "outsider rewrite"}).Code; got != http.StatusForbidden {
		t.Fatalf("non-executor status = %d, want 403", got)
	}
	if got := readTaskDescription(t, api, task.ID); got != "" {
		t.Fatalf("refused write still landed: %q", got)
	}
	// Positive control: the executor itself passes, so the 403 above is about
	// WHO asked and not about the route being broken for everyone.
	if got := writeTaskDescription(t, api, task.ID, "m-exec", "agent",
		map[string]any{"description": "executor rewrite"}).Code; got != http.StatusOK {
		t.Fatalf("executor status = %d, want 200", got)
	}
	// And admin capability drives any task (§14).
	putMemberRow(t, api, "m-mira", KindAssistant, adminRoleKey)
	if got := writeTaskDescription(t, api, task.ID, "m-mira", "agent",
		map[string]any{"description": "admin rewrite"}).Code; got != http.StatusOK {
		t.Fatalf("admin status = %d, want 200", got)
	}
	if got := readTaskDescription(t, api, task.ID); got != "admin rewrite" {
		t.Fatalf("description read back = %q, want admin rewrite", got)
	}
}

// Ruling 2: a CLOSED task's description is still editable, and by the same
// people. The artifact-set control in the same case is what makes this a
// statement about a REASONED difference rather than an oversight — the two
// writes face the same terminal task and only one of them is frozen.
func TestTaskDescriptionEditableOnAClosedTaskWhileArtifactsAreFrozen(t *testing.T) {
	api := newTasksTestServer(t)
	task := createAdHocTask(t, api, "m-exec")
	terminateTask(t, api, task.ID)

	const corrected = "結案後才發現票面寫錯,照樣改得動"
	if got := writeTaskDescription(t, api, task.ID, "m-exec", "agent",
		map[string]any{"description": corrected}).Code; got != http.StatusOK {
		t.Fatalf("closed-task description status = %d, want 200", got)
	}
	if got := readTaskDescription(t, api, task.ID); got != corrected {
		t.Fatalf("closed-task description read back = %q, want %q", got, corrected)
	}

	// The control: the SAME caller on the SAME closed task cannot touch the
	// deliverable set. If someone ever "harmonises" the two by adding a
	// terminal gate here, the description case above goes red; if someone
	// removes the artifact freeze instead, this half goes red.
	rec := httptest.NewRecorder()
	api.HandleAddTaskArtifactApiTasksTaskIdArtifactPost(rec,
		taskReq(t, "POST", "/api/tasks/"+task.ID+"/artifact",
			map[string]any{"kind": "link", "label": "pr", "url": "https://example.invalid/pr/1"},
			"m-exec", "agent"),
		task.ID)
	if rec.Code != http.StatusConflict {
		t.Fatalf("closed-task artifact status = %d, want 409 (the frozen set)", rec.Code)
	}
}

// Ruling 3: the trail rides the ALREADY-shipped document-history mechanism, so
// the generic list route serves it. The retained revision must be the text the
// write replaced — not the new text, which is the mistake a snapshot taken
// after the write would make.
func TestTaskDescriptionEditRetainsThePreviousTextInSharedHistory(t *testing.T) {
	api := newTasksTestServer(t)
	task := createAdHocTask(t, api, "m-exec")

	for _, text := range []string{"first wording", "second wording", "third wording"} {
		if got := writeTaskDescription(t, api, task.ID, "m-exec", "agent",
			map[string]any{"description": text}).Code; got != http.StatusOK {
			t.Fatalf("write %q: status %d", text, got)
		}
	}

	rec := listTaskDescriptionHistory(t, api, task.ID, "m-exec", "agent")
	if rec.Code != http.StatusOK {
		t.Fatalf("list history: %d %s", rec.Code, rec.Body.String())
	}
	history := decodeBody[[]DocumentHistoryDTO](t, rec)
	// Two revisions, not three: the FIRST write replaced an empty description,
	// and an empty document is nothing to retain (the same rule every other
	// kind gets from its row being absent).
	if len(history) != 2 {
		t.Fatalf("history length = %d, want 2: %+v", len(history), history)
	}
	// Newest first (ORDER BY id DESC), and each entry holds the text it
	// replaced.
	if got := history[0].Content["description"]; got != "second wording" {
		t.Fatalf("newest revision = %q, want %q", got, "second wording")
	}
	if got := history[1].Content["description"]; got != "first wording" {
		t.Fatalf("oldest revision = %q, want %q", got, "first wording")
	}
	if history[0].ActorId != "m-exec" {
		t.Fatalf("revision actor = %q, want m-exec", history[0].ActorId)
	}
}

// The partial-update shape (after update_task_manual): an ABSENT field changes
// nothing and versions nothing, while an explicit "" clears. Collapsing the two
// — a `default: ""` on the DTO, say — would let a body that never mentioned the
// description erase it.
func TestTaskDescriptionAbsentFieldIsANoOpButEmptyStringClears(t *testing.T) {
	api := newTasksTestServer(t)
	task := createAdHocTask(t, api, "m-exec")
	if got := writeTaskDescription(t, api, task.ID, "m-exec", "agent",
		map[string]any{"description": "the standing text"}).Code; got != http.StatusOK {
		t.Fatalf("seed write status = %d", got)
	}

	// Absent: unchanged, and no new revision.
	if got := writeTaskDescription(t, api, task.ID, "m-exec", "agent",
		map[string]any{}).Code; got != http.StatusOK {
		t.Fatalf("absent-field status = %d, want 200", got)
	}
	if got := readTaskDescription(t, api, task.ID); got != "the standing text" {
		t.Fatalf("absent field changed the text: %q", got)
	}
	// Re-writing the SAME text is a no-op too — it must not spend one of the
	// three retained slots recording that nothing changed.
	if got := writeTaskDescription(t, api, task.ID, "m-exec", "agent",
		map[string]any{"description": "the standing text"}).Code; got != http.StatusOK {
		t.Fatalf("same-text status = %d, want 200", got)
	}
	rec := listTaskDescriptionHistory(t, api, task.ID, "m-exec", "agent")
	if n := len(decodeBody[[]DocumentHistoryDTO](t, rec)); n != 0 {
		t.Fatalf("no-op writes retained %d revisions, want 0", n)
	}

	// Explicit "": cleared, and THAT is a real change, so it versions.
	if got := writeTaskDescription(t, api, task.ID, "m-exec", "agent",
		map[string]any{"description": ""}).Code; got != http.StatusOK {
		t.Fatalf("clear status = %d, want 200", got)
	}
	if got := readTaskDescription(t, api, task.ID); got != "" {
		t.Fatalf("explicit empty string did not clear: %q", got)
	}
	rec = listTaskDescriptionHistory(t, api, task.ID, "m-exec", "agent")
	history := decodeBody[[]DocumentHistoryDTO](t, rec)
	if len(history) != 1 || history[0].Content["description"] != "the standing text" {
		t.Fatalf("clear did not retain what it erased: %+v", history)
	}
}

// An unknown key is refused rather than dropped — the update_task_manual
// posture, and the reason the whole strict-decoder guard exists: a caller who
// reaches for `text` must be told, not silently ignored while believing the
// correction landed.
func TestTaskDescriptionUnknownKeyIsRefused(t *testing.T) {
	api := newTasksTestServer(t)
	task := createAdHocTask(t, api, "m-exec")
	if got := writeTaskDescription(t, api, task.ID, "m-exec", "agent",
		map[string]any{"text": "wrong field name"}).Code; got != http.StatusUnprocessableEntity {
		t.Fatalf("unknown key status = %d, want 422", got)
	}
	if got := readTaskDescription(t, api, task.ID); got != "" {
		t.Fatalf("refused body still wrote: %q", got)
	}
}

// Restoring an earlier wording goes through the SAME per-task gate as writing
// one. Without this, the generic restore route would be a side door that let
// any agent put text back onto a task it may not edit.
func TestTaskDescriptionRestoreIsGatedLikeTheEdit(t *testing.T) {
	api := newTasksTestServer(t)
	putMemberRow(t, api, "m-exec", KindAssistant, "")
	putMemberRow(t, api, "m-other", KindAssistant, "")
	task := createAdHocTask(t, api, "m-exec")
	for _, text := range []string{"original wording", "replacement wording"} {
		if got := writeTaskDescription(t, api, task.ID, "m-exec", "agent",
			map[string]any{"description": text}).Code; got != http.StatusOK {
			t.Fatalf("write %q: status %d", text, got)
		}
	}
	rec := listTaskDescriptionHistory(t, api, task.ID, "m-exec", "agent")
	history := decodeBody[[]DocumentHistoryDTO](t, rec)
	if len(history) != 1 {
		t.Fatalf("history length = %d, want 1: %+v", len(history), history)
	}
	id := history[0].Id

	restore := func(caller, scope string) int {
		r := httptest.NewRecorder()
		api.HandleRestoreDocumentHistoryApiDocumentHistoryKindKeyIdRestorePost(r,
			taskReq(t, "POST", "/api/document-history/task_description/"+task.ID+"/x/restore",
				nil, caller, scope),
			docKindTaskDescription, task.ID, id)
		return r.Code
	}
	if got := restore("m-other", "agent"); got != http.StatusForbidden {
		t.Fatalf("stranger restore status = %d, want 403", got)
	}
	if got := readTaskDescription(t, api, task.ID); got != "replacement wording" {
		t.Fatalf("refused restore still landed: %q", got)
	}
	if got := restore("m-exec", "agent"); got != http.StatusOK {
		t.Fatalf("executor restore status = %d, want 200", got)
	}
	if got := readTaskDescription(t, api, task.ID); got != "original wording" {
		t.Fatalf("restore read back = %q, want original wording", got)
	}
}

// An unknown task is a 404 on both faces — the route must not mint history for
// a task that does not exist, nor report a write that never happened.
func TestTaskDescriptionUnknownTaskIs404(t *testing.T) {
	api := newTasksTestServer(t)
	if got := writeTaskDescription(t, api, "t-nope", "m-exec", "agent",
		map[string]any{"description": "into the void"}).Code; got != http.StatusNotFound {
		t.Fatalf("unknown task status = %d, want 404", got)
	}
}
