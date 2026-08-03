package main

import (
	"net/http"
)

// ── T-e271 任務描述可編輯 ────────────────────────────────────────────────────
//
// The tool catalogue had no way to change an EXISTING task's description. Every
// neighbouring write refuses the job by construction: create_task takes a
// description only at the moment of birth, submit_plan writes steps, and
// update_task_manual writes the TYPE's manual rather than any one task. So a
// ruling to reword a card — the owner deciding the ticket says the wrong thing —
// had nowhere in the system to land. This endpoint is that place.
//
// Three owner rulings shape it, and each one is visible in the code below:
//
//  1. Only the EXECUTOR may edit (plus admin/owner). Creating a task grants no
//     standing to keep rewriting it afterwards. That is exactly what the
//     existing callerMayDriveTask already decides, so this route reuses it
//     rather than growing a second, subtly different predicate. Whoever changes
//     that gate changes this route with it — deliberately.
//
//  2. A CLOSED task (completed / terminated / duplicated) is editable too, on
//     the same terms. See the reasoned difference at the terminal-state note in
//     HandleUpdateTaskDescription... below — this route's silence about
//     TaskIsTerminal is a decision, not an omission.
//
//  3. The change leaves a trail, through the version-history mechanism that is
//     ALREADY shipped (document_history / SaveWithDocumentHistory), not a second
//     one built for tasks. Kind docKindTaskDescription, key = the task id; the
//     same list and restore routes serve it as serve the global context, role
//     definitions and task manuals.

// docKindTaskDescription is one task's description as a versioned document
// (T-e271). Its key is the TASK id — not the type key: a description belongs to
// one card, unlike the manual series which are keyed by type.
const docKindTaskDescription = "task_description"

// taskDescriptionHistorySnapshot serialises the state a description write
// replaces.
//
// An EMPTY description reads back as "{}" — "there was no document here" —
// which is what retainDocumentVersion already treats as nothing to retain. The
// other kinds express that same fact by their row being absent (a manual that
// does not exist snapshots "{}"); a task row always exists, so the emptiness has
// to be spelled here. Without it, the first edit of the many tasks that were
// created with no description at all would burn one of the three retained slots
// on a revision that says nothing.
func taskDescriptionHistorySnapshot(description string) (string, error) {
	if description == "" {
		return "{}", nil
	}
	return historyJSON(map[string]string{"description": description})
}

// taskDescriptionSnapshotIn is the reader SaveWithDocumentHistory calls from
// INSIDE the write transaction. It re-reads rather than trusting the value the
// handler folded a moment earlier: the retained revision must be the state this
// write actually replaced, otherwise two callers correcting the same card both
// retain the same ancestor and whichever landed between them is unrecoverable.
func taskDescriptionSnapshotIn(taskID string) func(sqlQuerier) (string, error) {
	return func(q sqlQuerier) (string, error) {
		current, ok, err := taskDescriptionOn(q, taskID)
		if err != nil {
			return "", err
		}
		if !ok {
			return "{}", nil
		}
		return taskDescriptionHistorySnapshot(current)
	}
}

func taskDescriptionHistoryStream(taskID, actor string) documentHistoryStream {
	return documentHistoryStream{
		Kind: docKindTaskDescription, Key: taskID, ActorID: actor,
		Snapshot: taskDescriptionSnapshotIn(taskID),
	}
}

// writeTaskDescription performs the versioned write: the revision this text
// replaces and the text itself land in ONE transaction. Reports false when the
// task row vanished mid-write (a concurrent hard delete), which the caller turns
// into a 404 rather than reporting a write that did not happen.
func (s *apiServer) writeTaskDescription(t *Task, actor, description string) (bool, error) {
	now := nowSecs()
	wrote := false
	err := s.dal.SaveWithDocumentHistories(
		[]documentHistoryStream{taskDescriptionHistoryStream(t.ID, actor)},
		func(ex sqlExecer) error {
			ok, err := SetTaskDescriptionOn(ex, t.ID, description, now)
			wrote = ok
			return err
		})
	if err != nil || !wrote {
		return false, err
	}
	t.Description = description
	t.UpdatedTS = now
	return true, nil
}

// POST /api/tasks/{task_id}/description — correct one task's description (MCP
// update_task_description).
//
// PARTIAL update, shaped after update_task_manual: the body's only field is
// description, omitting it is a legal no-op, and an unknown key is refused by
// the strict decoder rather than dropped. An explicit "" DOES clear the text —
// that asymmetry between "absent" and "empty" is why the DTO field is a nullable
// pointer with no default; a defaulted "" would let a body that never mentioned
// the description erase it.
//
// 🔴 TERMINAL STATE — why this route has no TaskIsTerminal guard while its
// neighbours do, stated as a difference in KIND and not as an exemption:
//
//	A closed task's ARTIFACT SET is frozen in both directions (add and remove,
//	admin and owner included — see HandleRemoveTaskArtifact...). What that
//	freeze protects is the OUTCOME: the artifacts are the record of what this
//	task actually produced, and a closed task's account of its own deliverables
//	must stop moving, or "what did this task ship" has no answer that stays put.
//
//	The description is not an outcome. It is the ticket's own TEXT — what the
//	task IS: scope, origin, acceptance. Correcting it changes nothing about what
//	was produced; it changes what the card SAYS it was for. And the need is at
//	its sharpest exactly where the freeze would bite: a ticket that was worded
//	wrongly is usually discovered to be wrong after it closed, and a rule that
//	only lets it be fixed while open leaves the wrong words standing forever, in
//	the permanent record, with no way to correct them.
//
//	So the two rules point opposite ways for the same reason — the closed record
//	should be TRUE. Freezing the deliverable set keeps it true; freezing the
//	description would preserve a known falsehood. The permission ladder does not
//	move either way: the same executor-or-admin gate applies whether the task is
//	open or closed, which is ruling 2 verbatim.
//
// Guard order: 404 unknown task → 403 not the executor → write. There is no
// 409 anywhere on this route.
func (s *apiServer) HandleUpdateTaskDescriptionApiTasksTaskIdDescriptionPost(w http.ResponseWriter, r *http.Request, taskId string) {
	var body TaskDescriptionDTO
	if !decodeJSONBody(w, r, &body) {
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
	// Field not named ⇒ nothing changes. Nothing is versioned either: a write
	// that replaces the text with itself would spend one of the three retained
	// revisions saying the text did not change, and fan an SSE delta for it.
	if body.Description == nil || *body.Description == t.Description {
		s.writeTask(w, *t)
		return
	}
	ok, err := s.writeTaskDescription(t, currentActor(r), *body.Description)
	if err != nil {
		internalError(w, err)
		return
	}
	if !ok {
		writeError(w, http.StatusNotFound, "task '"+taskId+"' not found")
		return
	}
	s.publishTask(*t, requestTrigger(r))
	s.writeTask(w, *t)
}
