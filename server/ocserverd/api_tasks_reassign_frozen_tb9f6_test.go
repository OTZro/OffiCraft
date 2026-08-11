// T-b9f6 — 凍結不再阻擋轉派 (owner ruling 2026-08-11, verbatim 「我不覺得凍結的東西
// 應該不能轉派 我覺得應該移除凍結不能轉派的限制」).
//
// Removing that 400 rests on ONE invariant: reassigning a frozen task must not
// WAKE anybody. Freezing means "do not advance this"; the reassign only ARRANGES
// who takes over when it thaws.
//
// 🔴 Where that invariant actually lives, and why the mutant discipline the
// ticket asked for had to change shape. The ticket said to prove it by taking
// away "the scheduler's frozen condition" (singular) and watching a test go red.
// There are TWO frozen conditions, independent of each other:
//
//	(A) outsourceAwaitingAssignment  (outsource_sched.go) — candidate collection,
//	    called from the sweep at collection time AND again just before the mint.
//	(B) the outsourceDecide loop      (outsource_sched.go) — admission.
//
// Either one ALONE is enough to keep a frozen task unassigned. So a behavioural
// test — one that drives a tick and asserts nothing got bound — CANNOT go red
// when only one of them is removed, and that is not a defect in the test: they
// are genuinely redundant. Writing a test that appears to catch it would be
// writing a fake.
//
// So each layer is pinned AT ITS OWN LAYER (option (a) of the two Kyle laid out
// on 2026-08-11), and the end-to-end scenario is pinned separately as the thing
// the owner ruling is actually about:
//
//	(A) TestOutsourceAwaitingAssignment_SkipsFrozen  — this file. Before it, layer
//	    (A)'s frozen arm had NO test of its own: every existing frozen assertion
//	    ran through a tick, where layer (B) covered for it.
//	(B) TestOutsourceDecideSkipsFrozenAndSpeclessTypes — outsource_sched_test.go,
//	    already existed and drives outsourceDecide directly.
//	end-to-end: TestReassignFrozenTaskToOutsourceWakesNobody — this file.
//
// ⚠️ Do not "simplify" this by deleting one of the two scheduler conditions
// because "the other one covers it". Each is load-bearing for a different
// caller, and their redundancy is exactly why neither is visible to a
// behavioural test.

package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestOutsourceAwaitingAssignment_SkipsFrozen(t *testing.T) {
	// Layer (A), driven directly — the only way this arm is observable, since
	// layer (B) refuses the same task one step later.
	base := Task{
		ID: "t-1", ExecutorKind: TaskExecutorOutsource, ExecutorID: "",
		Status: TaskStatusNotStarted, Priority: TaskPriorityMid,
	}
	// Positive controls FIRST: without them, a function that returned false for
	// everything would pass the frozen assertion below.
	if !outsourceAwaitingAssignment(base) {
		t.Fatalf("an unassigned, not_started outsource task must be awaiting: %+v", base)
	}
	handover := base
	handover.Status = TaskStatusInProgress
	handover.Lock = TaskLockReassigning
	if !outsourceAwaitingAssignment(handover) {
		t.Fatalf("a reassigning successor slot must be awaiting: %+v", handover)
	}

	// The arm under test, on BOTH shapes a frozen task can arrive in: a fresh
	// one, and one held under the handover lock — the shape T-b9f6 creates and
	// the one that did not exist before frozen tasks could be reassigned.
	frozen := base
	frozen.Priority = TaskPriorityFrozen
	if outsourceAwaitingAssignment(frozen) {
		t.Fatalf("a frozen task must never be awaiting assignment: %+v", frozen)
	}
	frozenHandover := handover
	frozenHandover.Priority = TaskPriorityFrozen
	if outsourceAwaitingAssignment(frozenHandover) {
		t.Fatalf("a frozen reassigning task must never be awaiting: %+v", frozenHandover)
	}
}

func TestReassignFrozenTaskToOutsourceWakesNobody(t *testing.T) {
	// The scenario the owner ruling is about: arrange the handover now, unfreeze
	// later. Nobody may be spawned while it is frozen; the moment it thaws the
	// successor is picked up normally (otherwise "nobody was woken" would also
	// be satisfied by a scheduler that was simply broken).
	api := newTasksTestServer(t)
	api.noOutsource = true // ticks are driven by hand, not by the cadence
	putOutsourceManual(t, api, "review-pr", "claude-sonnet-4-5", 5)
	task := createOutsourceTask(t, api, "review-pr", "先安排、之後再解凍")

	rec := httptest.NewRecorder()
	api.HandleSetTaskPriorityApiTasksTaskIdPriorityPost(rec,
		taskReq(t, "POST", "/x", map[string]any{"priority": "frozen"},
			wireOwnerID, "owner"), task.ID)
	if rec.Code != http.StatusOK {
		t.Fatalf("freeze: %d %s", rec.Code, rec.Body.String())
	}

	// The reassign itself: admitted now (it used to be a 400).
	if rec := reassign(t, api, task.ID, map[string]any{
		"target": map[string]any{"kind": "outsource", "effort": "medium"},
	}, wireOwnerID, "owner"); rec.Code != http.StatusOK {
		t.Fatalf("frozen task must be reassignable: %d %s", rec.Code, rec.Body.String())
	}

	// Frozen ⇒ the scheduler leaves it alone, however many ticks run.
	api.runOutsourceTick(1000.0)
	api.runOutsourceTick(1030.0)
	held, err := api.dal.GetTask(task.ID)
	if err != nil || held == nil {
		t.Fatalf("re-read: %v", err)
	}
	if held.ExecutorID != "" {
		t.Fatalf("a frozen task must wake nobody: executor %q", held.ExecutorID)
	}
	if held.Priority != TaskPriorityFrozen {
		t.Fatalf("the reassign must not thaw the task: priority %q", held.Priority)
	}
	if workers, err := api.dal.ListOutsourceWorkers(); err != nil {
		t.Fatalf("list workers: %v", err)
	} else if len(workers) != 0 {
		t.Fatalf("a frozen task must mint no worker, got %d", len(workers))
	}

	// Thaw ⇒ the arranged handover proceeds on the next tick. This half is the
	// discriminating control: it fails if "nobody was woken" was only true
	// because the fixture could never assign anyone in the first place.
	rec = httptest.NewRecorder()
	api.HandleSetTaskPriorityApiTasksTaskIdPriorityPost(rec,
		taskReq(t, "POST", "/x", map[string]any{"priority": "mid"},
			wireOwnerID, "owner"), task.ID)
	if rec.Code != http.StatusOK {
		t.Fatalf("unfreeze: %d %s", rec.Code, rec.Body.String())
	}
	api.runOutsourceTick(1060.0)
	thawed, err := api.dal.GetTask(task.ID)
	if err != nil || thawed == nil {
		t.Fatalf("re-read: %v", err)
	}
	if thawed.ExecutorID == "" {
		t.Fatalf("an unfrozen, reassigned task must be picked up on the next tick")
	}
}
