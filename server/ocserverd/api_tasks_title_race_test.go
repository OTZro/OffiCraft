package main

import (
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync"
	"testing"
)

// ── T-2ebe:並行寫入的 race ───────────────────────────────────────────────────
//
// THE HAZARD, stated as a construction rather than a worry: PutTask is a
// whole-row upsert with no optimistic lock, and every task-writing handler is a
// load-mutate-save — resolveTask (a read) → mutate one field → PutTask (a
// write). Nothing links the read to the write, so a second writer landing in
// that window has its change replayed away: the upsert asserts EVERY column as
// the first handler read them.
//
// The title is the column this ticket made editable, and the ONLY thing standing
// between a title correction and that replay is the removal of
// `title = excluded.title` from PutTask's ON CONFLICT list. These tests exist to
// make that removal load-bearing rather than incidental.

// TestTaskTitleSurvivesAWholeRowWriterInterleavedExactly is the DETERMINISTIC
// construction. It does not hope to hit the window — it opens the window by hand
// and drives the title edit through it.
//
// The interleave replays HandleSetTaskPriorityApiTasksTaskIdPriorityPost's own
// sequence, statement for statement (resolveTask → mutate Priority/UpdatedTS →
// dal.PutTask), with the title edit placed between the read and the write. That
// is faithful, not a caricature: resolveTask IS dal.GetTask, and the handler
// holds that snapshot until its PutTask.
func TestTaskTitleSurvivesAWholeRowWriterInterleavedExactly(t *testing.T) {
	api := newTasksTestServer(t)
	task := createAdHocTask(t, api, "m-exec")
	if got := postTaskTitle(t, api, task.ID, "m-exec", "agent",
		map[string]any{"title": "before"}).Code; got != http.StatusOK {
		t.Fatalf("seed title: %d", got)
	}

	// ① the priority handler's READ. Everything it holds from here is a snapshot.
	stale, err := api.dal.GetTask(task.ID)
	if err != nil || stale == nil {
		t.Fatalf("stale read: %v %v", stale, err)
	}
	if stale.Title != "before" {
		t.Fatalf("precondition: snapshot title = %q, want before", stale.Title)
	}

	// ② the title correction lands INSIDE the window, through the real endpoint.
	if got := postTaskTitle(t, api, task.ID, "m-exec", "agent",
		map[string]any{"title": "corrected"}).Code; got != http.StatusOK {
		t.Fatalf("interleaved title write: %d", got)
	}

	// ③ the priority handler's WRITE, carrying the title it read in ①.
	stale.Priority = TaskPriorityHigh
	stale.UpdatedTS = nowSecs()
	if err := api.dal.PutTask(*stale); err != nil {
		t.Fatalf("stale whole-row write: %v", err)
	}

	if got := readTaskTitle(t, api, task.ID); got != "corrected" {
		t.Fatalf("LOST UPDATE: title = %q, want corrected — a whole-row writer "+
			"replayed the title it had read before the edit", got)
	}
	// The other direction must also hold: the title edit must not have eaten the
	// priority change. A "fix" that merely swapped which writer loses would pass
	// the assertion above and fail this one.
	if got := readTaskPriority(t, api, task.ID); got != TaskPriorityHigh {
		t.Fatalf("priority = %q, want high — the title write must not clobber a "+
			"concurrent whole-row writer either", got)
	}
}

// TestTaskTitleSurvivesConcurrentPriorityWrites is the same hazard without a
// hand-placed window: two goroutines drive the two REAL endpoints against one
// task, repeatedly. The assertion is the invariant, not a count — after every
// round the stored title must be one the title endpoint actually wrote, and
// reverting to an OLDER value has no legal path.
func TestTaskTitleSurvivesConcurrentPriorityWrites(t *testing.T) {
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
			if code := postTaskTitle(t, api, task.ID, "m-exec", "agent",
				map[string]any{"title": want}).Code; code != http.StatusOK {
				t.Errorf("round %d title write: %d", round, code)
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

		if got := readTaskTitle(t, api, task.ID); got != want {
			t.Fatalf("round %d LOST UPDATE: title = %q, want %q — the concurrent "+
				"priority write replayed a stale title", round, got, want)
		}
	}
}

// TestTaskTitleRaceGuardHasTeeth names the guard so the next reader can find it.
//
// THE GUARD: dal_tasks.go PutTask's ON CONFLICT DO UPDATE list does NOT contain
// `title = excluded.title`. The column is therefore SINGLE-WRITER — only
// SetTaskTitleOn and the initial INSERT of create_task ever write it — so no
// whole-row upsert can replay a stale copy over a correction. That is the whole
// protection: not a lock, not a retry, an ownership boundary.
//
// The two tests above are the behavioural half; this one asserts the boundary
// structurally, so the guard cannot be removed quietly by someone who never runs
// the counterfactual.
func TestTaskTitleRaceGuardHasTeeth(t *testing.T) {
	raw, err := os.ReadFile("dal_tasks.go")
	if err != nil {
		t.Fatalf("read dal_tasks.go: %v", err)
	}
	// Anchored on the SYMBOL, never a line number.
	// T-52917b re-pointed this: PutTask is now a one-line delegation and its
	// 33-column upsert lives in putTaskOn, so that the very same statement can
	// run inside CreateTaskMintingID's transaction. The guard follows the SQL.
	const anchor = "func putTaskOn(ex sqlExecer, t Task) error {"
	start := strings.Index(string(raw), anchor)
	if start < 0 {
		t.Fatal("putTaskOn not found in dal_tasks.go — this guard is anchored on " +
			"the symbol, not a line number; re-point it if the function moved")
	}
	body := string(raw)[start+len(anchor):]
	if end := strings.Index(body, "\nfunc "); end >= 0 {
		body = body[:end]
	}
	if strings.Contains(body, "title = excluded.title") {
		t.Fatal("PutTask's ON CONFLICT list writes the title column again. That " +
			"makes title a shared-write column, and every load-mutate-save " +
			"handler will replay a stale copy of it over a concurrent " +
			"correction (T-2ebe). The title is written ONLY by SetTaskTitleOn " +
			"and by the create INSERT.")
	}
	// A live positive control on the reader itself: a column that IS in the
	// conflict list must be found, otherwise a broken slice would make the
	// assertion above vacuously green.
	if !strings.Contains(body, "priority = excluded.priority") {
		t.Fatal("source-reading control failed: PutTask's conflict list should " +
			"still carry priority — the assertion above cannot be trusted")
	}
}
