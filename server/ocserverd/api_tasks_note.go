package main

import (
	"net/http"
	"strconv"
	"unicode/utf8"
)

// ── T-cc3e 步驟備註欄 ────────────────────────────────────────────────────────
//
// Every agent's handover SOP says, verbatim: "把還在進行中的工作寫回 task step
// note（做到哪、下一步接什麼）". Until this endpoint existed that sentence named
// nothing — the step row had no general-purpose note, so the progress an agent
// wrote for its successor had nowhere to land. An outsource worker at 88%
// context said so honestly instead of pretending it had written somewhere, and
// the owner ruled (rc-15cf8df7cb7f, option 2) that the STEP layer gets its own
// note rather than folding this into the editable task description.
//
// Why neither existing note-shaped field could serve:
//   - waiting_reason is bound to ONE status: settable only entering
//     waiting_external, cleared by the status handler on the way out.
//   - the handoff_* fields live on the TASK and are read only on the report
//     that closes it.
//
// Both are moment-locked. A handover lands at an arbitrary moment, so the note
// has to be writable in ANY step status — that generality is the point, and
// TestStepNoteWritableInEveryStepStatus pins it.
//
// Its own endpoint and its own MCP tool, not another parameter on
// update_step_status: charter §14 is intent-per-tool, and writing a note is a
// different intent from reporting a transition. One field with two write paths
// would reintroduce exactly the "which one do I write?" ambiguity this ticket
// exists to remove.
//
// POST /api/tasks/{task_id}/steps/{step_id}/note — wholesale write: the body's
// note replaces whatever was there, "" clears it. Guards mirror the other
// task-driving writes (executor-or-admin 403, unknown task/step 404, terminal
// task 409) so the note is not a side door into a closed task's timeline.
func (s *apiServer) HandleUpdateTaskStepNoteApiTasksTaskIdStepsStepIdNotePost(w http.ResponseWriter, r *http.Request, taskId string, stepId string) {
	var body TaskStepNoteUpdateDTO
	if !decodeJSONBodyRequired(w, r, &body, "note") {
		return
	}
	note := trimString(body.Note)
	// Same ceiling as the task-level handover note (HandleReassignTaskApi...):
	// it is the same kind of writing for the same reader, so it gets the same
	// limit rather than a second number to remember. Runes, not bytes — these
	// notes are written in Chinese.
	if n := utf8.RuneCountInString(note); n > chatBodyMaxChars {
		writeError(w, http.StatusBadRequest, "step note is "+strconv.Itoa(n)+
			" chars, over the "+strconv.Itoa(chatBodyMaxChars)+"-char limit")
		return
	}
	t, err := s.resolveTask(taskId)
	if err != nil {
		writeResolveError(w, err, "task", taskId)
		return
	}
	if !s.callerMayDriveTask(r, *t) {
		writeError(w, http.StatusForbidden, "caller is not the task's executor")
		return
	}
	if TaskIsTerminal(t.Status) {
		writeError(w, http.StatusConflict,
			"task '"+taskId+"' is already closed ("+t.Status+")")
		return
	}
	step, err := s.dal.GetTaskStep(stepId)
	if err != nil {
		internalError(w, err)
		return
	}
	if step == nil || step.TaskID != taskId {
		writeError(w, http.StatusNotFound, "step '"+stepId+"' not found")
		return
	}
	// No step-status check on purpose. Writing a note is legal on a pending step
	// (what this lane is for), an in_progress one (the common case), a step
	// parked in waiting_external or holding a reply card, and on a done or
	// superseded one (correcting the record of what a finished step actually
	// produced). The status machine governs the WORK; the note only describes
	// it.
	step.Note = note
	if err := s.dal.PutTaskStep(*step); err != nil {
		internalError(w, err)
		return
	}
	// Nudge the cockpit so an open task card re-reads the step list. The task
	// ROW is deliberately not rewritten: a note changes neither status nor
	// priority nor progress, and PutTask is a whole-row upsert with no
	// optimistic lock, so writing it here would add a read-modify-write race
	// against every other task handler for the sake of a timestamp.
	s.publishTask(*t, requestTrigger(r))
	writeJSON(w, http.StatusOK, taskStepNoteReceiptDTO{
		TaskID: t.ID, StepID: step.ID, StepStatus: step.Status, Note: step.Note,
	})
}
