package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// ── T-cc3e 步驟備註欄 guards ─────────────────────────────────────────────────
//
// These assert the VALUE that came back out, never merely that a field is
// declared: the ticket exists because a documented note field turned out not to
// exist, and a test that only reads the schema would have passed just as
// happily against that nothing. Every case below writes a distinctive string
// and reads it back through the real read path (get_task's step view).

// writeStepNote posts one note write as the given caller and returns the
// recorder, so each case asserts its own status code.
func writeStepNote(t *testing.T, api *apiServer, taskID, stepID, caller, note string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	api.HandleUpdateTaskStepNoteApiTasksTaskIdStepsStepIdNotePost(rec,
		taskReq(t, "POST", "/api/tasks/"+taskID+"/steps/"+stepID+"/note",
			map[string]any{"note": note}, caller, "agent"),
		taskID, stepID)
	return rec
}

// readStepNote re-reads one step through the task view — the path a successor
// session actually uses (get_task), not the DAL.
func readStepNote(t *testing.T, api *apiServer, taskID, stepID string) string {
	t.Helper()
	rec := httptest.NewRecorder()
	api.HandleGetTaskApiTasksTaskIdGet(rec,
		taskReq(t, "GET", "/api/tasks/"+taskID, nil, "m-exec", "agent"), taskID)
	if rec.Code != http.StatusOK {
		t.Fatalf("get task: %d %s", rec.Code, rec.Body.String())
	}
	for _, st := range decodeBody[taskDTO](t, rec).Steps {
		if st.ID == stepID {
			return st.Note
		}
	}
	t.Fatalf("step %s missing from the task view", stepID)
	return ""
}

// TestStepNoteRoundTripsThroughTheTaskView is the核心 assertion: what a
// handover writes is what the next session reads. Dropping `note` from the
// persisted column list, from the DTO projection, or from the handler's assign
// reddens this — each of those is a way for the write to look successful and
// still leave the reader with nothing, which is precisely the failure this
// ticket was opened about.
func TestStepNoteRoundTripsThroughTheTaskView(t *testing.T) {
	api := newTasksTestServer(t)
	task := createAdHocTask(t, api, "m-exec")
	view := submitPlan(t, api, task.ID, "m-exec", []map[string]any{
		{"name": "one", "dod": "d1"},
		{"name": "two", "dod": "d2"},
	})
	stepID := view.Steps[0].ID
	const note = "跑完 conformance，紅在 auth matrix 第 3 case；下一步接前端 i18n"

	rec := writeStepNote(t, api, task.ID, stepID, "m-exec", note)
	if rec.Code != http.StatusOK {
		t.Fatalf("write note: %d %s", rec.Code, rec.Body.String())
	}
	// The receipt must echo what was STORED — the caller has to be able to
	// confirm the landing without a second round trip.
	if got := decodeBody[taskStepNoteReceiptDTO](t, rec).Note; got != note {
		t.Fatalf("receipt note = %q, want %q", got, note)
	}
	if got := readStepNote(t, api, task.ID, stepID); got != note {
		t.Fatalf("note read back = %q, want %q", got, note)
	}
	// A note belongs to ONE step: the sibling must not have picked it up.
	if got := readStepNote(t, api, task.ID, view.Steps[1].ID); got != "" {
		t.Fatalf("sibling step note = %q, want empty", got)
	}
}

// TestStepNoteWritableInEveryStepStatus pins the ticket's whole reason to
// exist. The two note-shaped fields that already existed are each locked to one
// moment — waiting_reason to waiting_external, the handoff fields to the
// closing report — so a handover landing at any other moment had nowhere to
// write. If someone later "tidies up" by gating this route on a step status,
// this reddens.
func TestStepNoteWritableInEveryStepStatus(t *testing.T) {
	api := newTasksTestServer(t)
	task := createAdHocTask(t, api, "m-exec")
	view := submitPlan(t, api, task.ID, "m-exec", []map[string]any{
		{"name": "pending lane", "dod": "d1"},
		{"name": "working lane", "dod": "d2"},
		{"name": "parked lane", "dod": "d3"},
		{"name": "finished lane", "dod": "d4"},
	})
	pending, working := view.Steps[0], view.Steps[1]
	parked, finished := view.Steps[2], view.Steps[3]

	// working → in_progress; parked → waiting_external; finished → done.
	if rec := reportStepStatus(t, api, task.ID, working.ID, "m-exec", "in_progress", ""); rec.Code != http.StatusOK {
		t.Fatalf("start working lane: %d %s", rec.Code, rec.Body.String())
	}
	for _, s := range []string{"in_progress", "waiting_external"} {
		reason := ""
		if s == "waiting_external" {
			reason = "waiting on a third party"
		}
		if rec := reportStepStatus(t, api, task.ID, parked.ID, "m-exec", s, reason); rec.Code != http.StatusOK {
			t.Fatalf("park lane %s: %d %s", s, rec.Code, rec.Body.String())
		}
	}
	for _, s := range []string{"in_progress", "done"} {
		if rec := reportStepStatus(t, api, task.ID, finished.ID, "m-exec", s, ""); rec.Code != http.StatusOK {
			t.Fatalf("finish lane %s: %d %s", s, rec.Code, rec.Body.String())
		}
	}

	for _, tc := range []struct{ name, stepID, note string }{
		{"pending", pending.ID, "not started; picks up after the merge lane"},
		{"in_progress", working.ID, "half way — schema regenerated, handler next"},
		{"waiting_external", parked.ID, "note and waiting_reason are different fields"},
		{"done", finished.ID, "finished; recording what it actually produced"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if rec := writeStepNote(t, api, task.ID, tc.stepID, "m-exec", tc.note); rec.Code != http.StatusOK {
				t.Fatalf("write in %s: %d %s", tc.name, rec.Code, rec.Body.String())
			}
			if got := readStepNote(t, api, task.ID, tc.stepID); got != tc.note {
				t.Fatalf("%s note = %q, want %q", tc.name, got, tc.note)
			}
		})
	}
	// waiting_reason is a SEPARATE field: writing a note must not have clobbered
	// the parked lane's reason, and the note must not be the reason.
	rec := httptest.NewRecorder()
	api.HandleGetTaskApiTasksTaskIdGet(rec,
		taskReq(t, "GET", "/api/tasks/"+task.ID, nil, "m-exec", "agent"), task.ID)
	for _, st := range decodeBody[taskDTO](t, rec).Steps {
		if st.ID == parked.ID && st.WaitingReason != "waiting on a third party" {
			t.Fatalf("waiting_reason = %q, want it untouched by the note write",
				st.WaitingReason)
		}
	}
}

// TestStepNoteIsWholesaleAndClearable — it is a current-state note, not an
// append-only log: the second write replaces the first, and "" empties it.
func TestStepNoteIsWholesaleAndClearable(t *testing.T) {
	api := newTasksTestServer(t)
	task := createAdHocTask(t, api, "m-exec")
	view := submitPlan(t, api, task.ID, "m-exec", []map[string]any{
		{"name": "one", "dod": "d1"},
	})
	stepID := view.Steps[0].ID
	for _, want := range []string{"first pass", "second pass replaces it", ""} {
		if rec := writeStepNote(t, api, task.ID, stepID, "m-exec", want); rec.Code != http.StatusOK {
			t.Fatalf("write %q: %d %s", want, rec.Code, rec.Body.String())
		}
		if got := readStepNote(t, api, task.ID, stepID); got != want {
			t.Fatalf("note = %q, want %q", got, want)
		}
	}
}

// TestStepNoteSurvivesAReplan — a replan keeps done steps as history, and the
// note is the most valuable thing on a finished step (what it actually
// produced). Losing it on submit_plan would silently destroy exactly the
// handover record this field exists to carry.
func TestStepNoteSurvivesAReplan(t *testing.T) {
	api := newTasksTestServer(t)
	task := createAdHocTask(t, api, "m-exec")
	view := submitPlan(t, api, task.ID, "m-exec", []map[string]any{
		{"name": "one", "dod": "d1"},
		{"name": "two", "dod": "d2"},
	})
	doneStep := view.Steps[0].ID
	for _, s := range []string{"in_progress", "done"} {
		if rec := reportStepStatus(t, api, task.ID, doneStep, "m-exec", s, ""); rec.Code != http.StatusOK {
			t.Fatalf("drive done %s: %d %s", s, rec.Code, rec.Body.String())
		}
	}
	const note = "produced the spec diff; regenerated all three files"
	if rec := writeStepNote(t, api, task.ID, doneStep, "m-exec", note); rec.Code != http.StatusOK {
		t.Fatalf("write note: %d %s", rec.Code, rec.Body.String())
	}
	submitPlan(t, api, task.ID, "m-exec", []map[string]any{
		{"name": "rethought", "dod": "d3"},
	})
	if got := readStepNote(t, api, task.ID, doneStep); got != note {
		t.Fatalf("note after replan = %q, want it kept as %q", got, note)
	}
}

// TestStepNoteRefusesTheWrongCaller — the same executor-or-admin gate as every
// other task-driving write. Dropping callerMayDriveTask from the handler
// reddens this; the assertion names the REASON, not just the failure, so a
// 403 arriving for some unrelated cause cannot pass for the guard working.
func TestStepNoteRefusesTheWrongCaller(t *testing.T) {
	api := newTasksTestServer(t)
	task := createAdHocTask(t, api, "m-exec")
	view := submitPlan(t, api, task.ID, "m-exec", []map[string]any{
		{"name": "one", "dod": "d1"},
	})
	stepID := view.Steps[0].ID

	rec := writeStepNote(t, api, task.ID, stepID, "m-someone-else", "not my task")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("stranger write: %d %s, want 403", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "not the task's executor") {
		t.Fatalf("403 body = %s, want it to name the executor guard", rec.Body.String())
	}
	// And the refusal must be real, not cosmetic: nothing landed.
	if got := readStepNote(t, api, task.ID, stepID); got != "" {
		t.Fatalf("note after refused write = %q, want empty", got)
	}
}

// TestStepNoteRefusesAnUnknownStep — a step id belonging to another task is a
// 404, not a silent write onto whatever row matched.
func TestStepNoteRefusesAnUnknownStep(t *testing.T) {
	api := newTasksTestServer(t)
	mine := createAdHocTask(t, api, "m-exec")
	submitPlan(t, api, mine.ID, "m-exec", []map[string]any{{"name": "one", "dod": "d1"}})
	other := createAdHocTask(t, api, "m-exec")
	otherView := submitPlan(t, api, other.ID, "m-exec", []map[string]any{{"name": "x", "dod": "d"}})

	rec := writeStepNote(t, api, mine.ID, otherView.Steps[0].ID, "m-exec", "wrong task")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("cross-task write: %d %s, want 404", rec.Code, rec.Body.String())
	}
	if got := readStepNote(t, api, other.ID, otherView.Steps[0].ID); got != "" {
		t.Fatalf("other task's step note = %q, want empty", got)
	}
}

// TestStepNoteRefusesOverTheCharLimit — the same ceiling as the task-level
// handover note, counted in RUNES: a 3,000-character Chinese note is well
// inside the limit and must be accepted, which a byte-based count would reject.
func TestStepNoteRefusesOverTheCharLimit(t *testing.T) {
	api := newTasksTestServer(t)
	task := createAdHocTask(t, api, "m-exec")
	view := submitPlan(t, api, task.ID, "m-exec", []map[string]any{{"name": "one", "dod": "d1"}})
	stepID := view.Steps[0].ID

	legal := strings.Repeat("備", 3000) // 9,000 bytes, 3,000 runes
	if rec := writeStepNote(t, api, task.ID, stepID, "m-exec", legal); rec.Code != http.StatusOK {
		t.Fatalf("3,000-rune CJK note: %d %s, want 200", rec.Code, rec.Body.String())
	}
	over := strings.Repeat("備", chatBodyMaxChars+1)
	rec := writeStepNote(t, api, task.ID, stepID, "m-exec", over)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("over-cap note: %d %s, want 400", rec.Code, rec.Body.String())
	}
	// The refusal must leave the previous note intact, not half-apply.
	if got := readStepNote(t, api, task.ID, stepID); got != legal {
		t.Fatalf("note after refused over-cap write changed; want the legal one kept")
	}
}
