package main

import (
	"errors"
	"net/http"
)

// ── T-646a 一支工具改一張票的文字 ────────────────────────────────────────────
//
// T-e271 made a description correctable and T-2ebe did the same for the title,
// each behind its own route. Two doors onto one card's text is not merely one
// tool too many: changing BOTH meant two calls, two transactions and two SSE
// deltas, with room for a third party's write to land in the gap, and the rules
// were written down twice — which is how the two came to disagree about
// something neither of them had decided.
//
// They disagreed about WHITESPACE. The title was trimmed before it was stored
// and before it was compared with the current value; the description was not,
// with no reason recorded on either side. The visible cost fell on the compare:
// re-sending a description that differed only by a trailing space read as a
// change, so it spent one of the three retained revisions saying nothing had
// moved and fanned a delta for it. Owner ruling on the fix, card
// rc-0fb94a25a8a8 (2026-08-16, option ①): BOTH fields are trimmed, on both
// sides of the comparison.
//
// 🔴 What was NOT unified, and must not be: the two fields still treat an
// explicit blank differently. A blank title is refused (400); a blank
// description is a real write that clears the text. That asymmetry is an owner
// ruling of its own — card rc-796541192519 (2026-08-11), option ① — and the
// reasoning is at the top of api_tasks_title.go: create_task refuses a blank
// title, so an edit door looser than the create door is a bypass to a state the
// system does not otherwise admit, namely a task-list row with nothing in it. A
// description may legitimately be empty; a title may not. Anyone reading this
// file as "two rules where one would do" should read that card before changing
// it.
//
// WHOLE-BODY VALIDATION. The blank-title check runs over the parsed body BEFORE
// any field is written, so a request naming a blank title alongside a perfectly
// good description writes NEITHER. The alternative — validate-and-write field by
// field — would leave the card half-updated behind a 400, which is the one
// outcome a caller cannot reason about from the status code.
//
// ONE SEAM, THREE DOORS. The two original routes now resolve their body through
// the same taskTextEdit and write through the same transaction as update_task.
// That is the point of the ticket rather than a tidy-up: while each door kept
// its own copy of the rules, a rule could be corrected on one and left standing
// on the other, and the drift this file exists to remove is exactly what that
// produced. The two remain on the HTTP surface for the frontend and any existing
// client; they are off the MCP catalogue, so an agent sees one tool.

// errTaskRowVanished reports a task row hard-deleted by someone else BETWEEN the
// handler's read and this transaction's write. It is returned from inside the
// write function rather than recorded in a flag so the transaction ROLLS BACK:
// a body naming two fields must not commit the first one and then discover the
// row is gone, which would be the half-applied state whole-body validation
// exists to prevent.
var errTaskRowVanished = errors.New("task row vanished mid-write")

// taskTextEdit is a body that has already been validated and reduced to the
// writes it actually implies. A field the body never named, and a field whose
// value already equals what is stored, both arrive here as "not set" — the
// unchanged-value case is dropped HERE rather than at the write so that every
// door gets the same early return, and so a no-op cannot spend one of the three
// retained revisions saying nothing changed.
type taskTextEdit struct {
	setTitle       bool
	title          string
	setDescription bool
	description    string
}

func (e taskTextEdit) empty() bool { return !e.setTitle && !e.setDescription }

// resolveTaskTextEdit validates the whole body against the task as it stands and
// reports the message for a 400, or "" when the body is admissible.
//
// Both values are trimmed BEFORE the equality test, not only before storage.
// Trimming only on the way in would still store the right text but would read a
// stray trailing space as a change — the false edit this ticket set out to
// remove. A description of nothing but whitespace therefore trims to "" and
// CLEARS, which is the same answer an explicit "" gets and deliberately so.
func resolveTaskTextEdit(t Task, title, description *string) (taskTextEdit, string) {
	var e taskTextEdit
	if title != nil {
		v := trimString(*title)
		if v == "" {
			// Same words create_task uses, deliberately: one rule, one sentence.
			return taskTextEdit{}, "title must not be blank"
		}
		if v != t.Title {
			e.setTitle, e.title = true, v
		}
	}
	if description != nil {
		v := trimString(*description)
		if v != t.Description {
			e.setDescription, e.description = true, v
		}
	}
	return e, ""
}

// writeTaskText performs the versioned write of every field the edit names, in
// ONE transaction: the revisions being replaced and the new values land
// together. Reports false when the task row vanished mid-write, which the caller
// turns into a 404 rather than reporting a write that did not happen.
//
// A history stream is enrolled ONLY for a field that is actually changing.
// Enrolling both unconditionally would retain a revision for the untouched field
// as well, and the retained set is three deep — a caller correcting only the
// title would silently push the description's oldest recoverable wording off the
// end.
func (s *apiServer) writeTaskText(t *Task, actor string, e taskTextEdit) (bool, error) {
	now := nowSecs()
	streams := make([]documentHistoryStream, 0, 2)
	if e.setTitle {
		streams = append(streams, taskTitleHistoryStream(t.ID, actor))
	}
	if e.setDescription {
		streams = append(streams, taskDescriptionHistoryStream(t.ID, actor))
	}
	err := s.dal.SaveWithDocumentHistories(streams, func(ex sqlExecer) error {
		if e.setTitle {
			ok, err := SetTaskTitleOn(ex, t.ID, e.title, now)
			if err != nil {
				return err
			}
			if !ok {
				return errTaskRowVanished
			}
		}
		if e.setDescription {
			ok, err := SetTaskDescriptionOn(ex, t.ID, e.description, now)
			if err != nil {
				return err
			}
			if !ok {
				return errTaskRowVanished
			}
		}
		return nil
	})
	if errors.Is(err, errTaskRowVanished) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if e.setTitle {
		t.Title = e.title
	}
	if e.setDescription {
		t.Description = e.description
	}
	t.UpdatedTS = now
	return true, nil
}

// updateTaskText is the body of all three doors onto a task's text.
//
// Guard order: 404 unknown task → 403 not the executor → 400 bad body → write.
// The body check sits AFTER the permission gate on purpose: a caller who may not
// touch this task should learn that, not be handed a critique of a body it was
// never entitled to submit.
//
// There is no TaskIsTerminal guard, and its absence is a decision rather than an
// omission — the reasoning is at the terminal-state note in
// api_tasks_description.go. In short: a closed task's ARTIFACTS are the record
// of what it produced and must stop moving, while its TEXT is what the ticket
// says it was for, and a ticket is usually found to be worded wrongly only after
// it closed. Freezing the artifacts keeps the closed record true; freezing the
// text would preserve a known falsehood.
//
// NO LENGTH CAP on either field, matching create_task, which has never capped
// them. A ceiling applied at the edit door and nowhere else would mean an
// already-long value can only ever be made shorter, so a correction that does not
// shrink it would be refused here while the very same words entered freely
// through create. If a cap is ever wanted it belongs on BOTH doors at once,
// sized so no stored value is already over it — a new protection over an existing
// wire, which is the owner's call.
func (s *apiServer) updateTaskText(w http.ResponseWriter, r *http.Request, taskID string, title, description *string) {
	t, err := s.resolveTask(taskID)
	if err != nil {
		writeResolveError(w, err, "task", taskID)
		return
	}
	if !s.callerMayDriveTask(r, *t) {
		writeError(w, http.StatusForbidden, "caller is not the task's executor")
		return
	}
	edit, bad := resolveTaskTextEdit(*t, title, description)
	if bad != "" {
		writeError(w, http.StatusBadRequest, bad)
		return
	}
	// Nothing named, or nothing actually different ⇒ nothing changes, nothing is
	// versioned, nothing fans.
	if edit.empty() {
		s.writeTask(w, *t)
		return
	}
	ok, err := s.writeTaskText(t, currentActor(r), edit)
	if err != nil {
		internalError(w, err)
		return
	}
	if !ok {
		writeError(w, http.StatusNotFound, "task '"+taskID+"' not found")
		return
	}
	s.publishTask(*t, requestTrigger(r))
	s.writeTask(w, *t)
}

// POST /api/tasks/{task_id} — correct this task's title, description, or both
// (MCP update_task).
//
// PARTIAL: the body's fields are nullable pointers with no default, so "absent"
// and "present but empty" stay distinguishable. A defaulted "" would let a body
// that never mentioned the description erase it. Unknown keys are refused by the
// strict decoder rather than dropped.
func (s *apiServer) HandleUpdateTaskApiTasksTaskIdPost(w http.ResponseWriter, r *http.Request, taskId string) {
	var body TaskFieldsDTO
	if !decodeJSONBody(w, r, &body) {
		return
	}
	s.updateTaskText(w, r, taskId, body.Title, body.Description)
}
