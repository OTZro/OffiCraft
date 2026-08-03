package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
)

// ── T-e271 節點 3:並行寫入的 race ────────────────────────────────────────────
//
// THE HAZARD, stated concretely rather than as a worry: PutTask is a whole-row
// upsert with no optimistic lock, and every task-writing handler is a
// load-mutate-save — resolveTask (a read on the READ pool) → mutate one field →
// PutTask (a write on the write pool). Nothing links the read to the write, so
// a second writer landing in that window has its change replayed away: the
// upsert asserts EVERY column as the first handler read them.
//
// The description is the field this ticket added, so the question that has to be
// answered with a construction and not an argument is: can a correction be
// silently lost? These tests construct it. The answer at the time they were
// written was YES for a naive implementation and NO for the shipped one, and
// TestTaskDescriptionRaceGuardHasTeeth documents exactly which single line of
// dal_tasks.go decides that.

// setPriorityThroughHandler drives the REAL priority handler — the concurrent
// writer in the hazard above is a real endpoint, not a fixture.
func setPriorityThroughHandler(t *testing.T, api *apiServer, taskID, caller, priority string) int {
	t.Helper()
	rec := httptest.NewRecorder()
	api.HandleSetTaskPriorityApiTasksTaskIdPriorityPost(rec,
		taskReq(t, "POST", "/api/tasks/"+taskID+"/priority",
			map[string]any{"priority": priority}, caller, "agent"),
		taskID)
	return rec.Code
}

func readTaskPriority(t *testing.T, api *apiServer, taskID string) string {
	t.Helper()
	rec := httptest.NewRecorder()
	api.HandleGetTaskApiTasksTaskIdGet(rec,
		taskReq(t, "GET", "/api/tasks/"+taskID, nil, "m-exec", "agent"), taskID)
	if rec.Code != http.StatusOK {
		t.Fatalf("get task: %d %s", rec.Code, rec.Body.String())
	}
	return decodeBody[taskDTO](t, rec).Priority
}

// TestTaskDescriptionSurvivesAWholeRowWriterInterleavedExactly is the
// DETERMINISTIC construction. It does not hope to hit the window — it opens the
// window by hand and drives the description edit through it.
//
// The interleave replays HandleSetTaskPriorityApiTasksTaskIdPriorityPost's own
// sequence, statement for statement (resolveTask → mutate Priority/UpdatedTS →
// dal.PutTask), with the description edit placed between the read and the write.
// That is faithful, not a caricature: resolveTask IS dal.GetTask, and the
// handler holds a snapshot from that call until its PutTask.
//
// Both writes must stand. If the description reverts to "before", a correction
// the caller was told had landed (200 + the new text echoed back) has been
// silently destroyed by an unrelated priority change — the worst shape of this
// bug, because nothing anywhere reports it.
func TestTaskDescriptionSurvivesAWholeRowWriterInterleavedExactly(t *testing.T) {
	api := newTasksTestServer(t)
	task := createAdHocTask(t, api, "m-exec")
	if got := writeTaskDescription(t, api, task.ID, "m-exec", "agent",
		map[string]any{"description": "before"}).Code; got != http.StatusOK {
		t.Fatalf("seed description: %d", got)
	}

	// ① the priority handler's READ. Everything it holds from here is a snapshot.
	stale, err := api.dal.GetTask(task.ID)
	if err != nil || stale == nil {
		t.Fatalf("stale read: %v %v", stale, err)
	}
	if stale.Description != "before" {
		t.Fatalf("precondition: snapshot description = %q, want before", stale.Description)
	}

	// ② the description edit lands INSIDE the window, through the real endpoint.
	if got := writeTaskDescription(t, api, task.ID, "m-exec", "agent",
		map[string]any{"description": "corrected"}).Code; got != http.StatusOK {
		t.Fatalf("interleaved description write: %d", got)
	}

	// ③ the priority handler's WRITE, carrying the description it read in ①.
	stale.Priority = TaskPriorityHigh
	stale.UpdatedTS = nowSecs()
	if err := api.dal.PutTask(*stale); err != nil {
		t.Fatalf("stale whole-row write: %v", err)
	}

	if got := readTaskDescription(t, api, task.ID); got != "corrected" {
		t.Fatalf("LOST UPDATE: description = %q, want corrected — a whole-row "+
			"writer replayed the description it had read before the edit", got)
	}
	// The other direction must also hold: the description edit must not have
	// eaten the priority change. A "fix" that merely swapped which writer loses
	// would pass the assertion above and fail this one.
	if got := readTaskPriority(t, api, task.ID); got != TaskPriorityHigh {
		t.Fatalf("priority = %q, want high — the description write must not "+
			"clobber a concurrent whole-row writer either", got)
	}
}

// TestTaskDescriptionSurvivesConcurrentPriorityWrites is the same hazard without
// a hand-placed window: two goroutines drive the two REAL endpoints against one
// task, repeatedly. It is the honest complement to the deterministic case —
// deterministic proof that the window can be exploited, plus evidence that the
// scheduler actually lands in it under ordinary contention.
//
// The assertion is the invariant, not a count: after every round the stored
// description must be one the description endpoint actually wrote. Reverting to
// an OLDER value is the lost update; there is no legal path to it.
func TestTaskDescriptionSurvivesConcurrentPriorityWrites(t *testing.T) {
	api := newTasksTestServer(t)
	task := createAdHocTask(t, api, "m-exec")

	const rounds = 60
	priorities := []string{TaskPriorityHigh, TaskPriorityMid, TaskPriorityLow}
	for round := 0; round < rounds; round++ {
		want := fmt.Sprintf("wording-%d", round)
		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			if code := writeTaskDescription(t, api, task.ID, "m-exec", "agent",
				map[string]any{"description": want}).Code; code != http.StatusOK {
				t.Errorf("round %d description write: %d", round, code)
			}
		}()
		go func() {
			defer wg.Done()
			if code := setPriorityThroughHandler(t, api, task.ID, "m-exec",
				priorities[round%len(priorities)]); code != http.StatusOK {
				t.Errorf("round %d priority write: %d", round, code)
			}
		}()
		wg.Wait()

		if got := readTaskDescription(t, api, task.ID); got != want {
			t.Fatalf("round %d LOST UPDATE: description = %q, want %q — the "+
				"concurrent priority write replayed a stale description",
				round, got, want)
		}
	}
}

// TestTaskDescriptionRaceGuardHasTeeth names the guard so the next reader can
// find it, and states the counterfactual that was actually run rather than
// merely asserted.
//
// THE GUARD: dal_tasks.go PutTask's ON CONFLICT DO UPDATE list does NOT contain
// `description = excluded.description`. The column is therefore SINGLE-WRITER —
// only SetTaskDescriptionOn (and the initial INSERT of create_task) ever writes
// it — so no whole-row upsert can replay a stale copy of it. That is the whole
// protection: not a lock, not a retry, an ownership boundary.
//
// COUNTERFACTUAL (run by hand, see the evidence log): adding
// `description = excluded.description` back to that ON CONFLICT list turns both
// tests above red — the deterministic one on its first assertion and the
// concurrent one within the first rounds. Without that, the two tests above
// would be indistinguishable from tests that pass because nothing was ever
// concurrent.
//
// This test itself asserts the boundary structurally, so the guard cannot be
// removed quietly even by someone who never runs the counterfactual: PutTask
// must not name the description column in its conflict clause.
func TestTaskDescriptionRaceGuardHasTeeth(t *testing.T) {
	raw, err := os.ReadFile("dal_tasks.go")
	if err != nil {
		t.Fatalf("read dal_tasks.go: %v", err)
	}
	// Anchored on the SYMBOL, never a line number: the enclosing text of
	// PutTask, cut at the next top-level func.
	const anchor = "func (d *DAL) PutTask(t Task) error {"
	start := strings.Index(string(raw), anchor)
	if start < 0 {
		t.Fatal("PutTask not found in dal_tasks.go — this guard is anchored on " +
			"the symbol, not a line number; re-point it if the function moved")
	}
	rest := string(raw)[start+len(anchor):]
	if end := strings.Index(rest, "\nfunc "); end >= 0 {
		rest = rest[:end]
	}
	body := rest
	if strings.Contains(body, "description = excluded.description") {
		t.Fatal("PutTask's ON CONFLICT list writes the description column again. " +
			"That makes description a shared-write column, and every " +
			"load-mutate-save handler will replay a stale copy of it over a " +
			"concurrent correction (T-e271 node 3). The description is written " +
			"ONLY by SetTaskDescriptionOn and by the create INSERT.")
	}
	// A live positive control on the reader itself: a column that IS in the
	// conflict list must be found. Without this, a broken funcBodyIn/
	// containsToken would make the assertion above vacuously green.
	if !strings.Contains(body, "priority = excluded.priority") {
		t.Fatal("source-reading control failed: PutTask's conflict list should " +
			"still carry priority — the assertion above cannot be trusted")
	}
}
