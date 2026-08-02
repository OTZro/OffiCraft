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
	// (recording what the lane is for before it starts), an in_progress one (the
	// common case), a step parked in waiting_external, one holding a reply card,
	// and a done or superseded one. The status machine governs the WORK; the
	// note only describes it.
	//
	// Note that the TASK-level terminal gate above still applies: once every
	// step is done the task auto-closes, so a done step is writable while its
	// task is still open and not after. That is the same line the artifact set
	// draws — a closed task's record stops moving — and the tool description
	// says so rather than promising a write that would 409.
	ok, err := s.dal.SetTaskStepNote(step.ID, note)
	if err != nil {
		internalError(w, err)
		return
	}
	if !ok {
		// The step existed a moment ago and does not now — a concurrent
		// submit_plan deleted it. Honest 404 beats resurrecting the row.
		writeError(w, http.StatusNotFound, "step '"+stepId+"' not found")
		return
	}
	step.Note = note
	// Move updated_ts so the cockpit actually shows this. The SSE task delta
	// carries only id/status/priority and the list it refreshes carries no
	// steps, so a card the owner ALREADY has open re-reads its step-bearing
	// detail only when updated_ts changes. Without this bump the owner watching
	// a live handover would see nothing until he collapsed and re-expanded the
	// card — the one case this ticket exists to serve.
	now := nowSecs()
	if err := s.dal.TouchTaskUpdatedTS(t.ID, now); err != nil {
		internalError(w, err)
		return
	}
	t.UpdatedTS = now
	s.publishTask(*t, requestTrigger(r))
	writeJSON(w, http.StatusOK, taskStepNoteReceiptDTO{
		TaskID: t.ID, StepID: step.ID, StepStatus: step.Status, Note: step.Note,
	})
}
