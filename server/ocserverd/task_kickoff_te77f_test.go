// T-e77f — 叫開工: the server tells an outsource executor when its task stops
// being un-advanceable.
//
// WHAT THE MEASUREMENT SAID (task t-a6fe65399dea / worker X-87, 2026-08-15).
// The worker booted, saw the task was FROZEN, and correctly refused to advance
// it. The task was unfrozen later. Nothing told the worker, and a codex worker
// opens no further turn without an inbound event — so a CORRECT refusal became a
// permanent stall. "Never told" and "told, judged right, then the world moved"
// look identical from outside; only the second one was real here.
//
// Each of the three transitions therefore gets its own test, and each is written
// so that removing its wiring turns it red — the negative controls in this file
// exist because "no message was posted" is also satisfied by a fixture that
// could never post one.
package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// kickoffsTo returns the kickoff notices addressed to recipient. It matches on
// the sender AND the notice's own opening claim rather than on "any system
// message": the dependency-release path and the reassign path both post
// system-authored task chat, so a bare sender match would count them too and the
// de-duplication assertions would be measuring the wrong thing.
func kickoffsTo(t *testing.T, api *apiServer, recipient string) []ChatMessage {
	t.Helper()
	msgs, err := api.dal.ListChat()
	if err != nil {
		t.Fatalf("list chat: %v", err)
	}
	var out []ChatMessage
	for _, m := range msgs {
		if m.Sender == wireSystemSender && m.Recipient == recipient &&
			strings.Contains(m.Body, "現在可以開始推進了") {
			out = append(out, m)
		}
	}
	return out
}

func ownerSetsPriority(t *testing.T, api *apiServer, taskID, priority string) {
	t.Helper()
	if rec := setPriority(t, api, taskID, wireOwnerID, "owner", priority); rec.Code != http.StatusOK {
		t.Fatalf("set priority %s: %d %s", priority, rec.Code, rec.Body.String())
	}
}

func setDeps(t *testing.T, api *apiServer, taskID string, blockedBy ...string) {
	t.Helper()
	rec := httptest.NewRecorder()
	api.HandleSetTaskDepsApiTasksTaskIdDepsPost(rec,
		taskReq(t, "POST", "/x", map[string]any{"blocked_by": blockedBy},
			wireOwnerID, "owner"), taskID)
	if rec.Code != http.StatusOK {
		t.Fatalf("set deps: %d %s", rec.Code, rec.Body.String())
	}
}

// boundOutsourceTask creates a typed outsource task and runs one tick, so it
// comes back already bound to a freshly minted worker — the state every trigger
// below starts from.
func boundOutsourceTask(t *testing.T, api *apiServer, title string) (Task, string) {
	t.Helper()
	putOutsourceManual(t, api, "review-pr", "claude-sonnet-4-5", 5)
	created := createOutsourceTask(t, api, "review-pr", title)
	api.runOutsourceTick(1000.0)
	bound, err := api.dal.GetTask(created.ID)
	if err != nil || bound == nil {
		t.Fatalf("re-read task: %v", err)
	}
	if bound.ExecutorID == "" {
		t.Fatalf("fixture must bind a worker, got none")
	}
	return *bound, bound.ExecutorID
}

// ── trigger ①: the unfreeze (the only one that would have saved X-87) ────────

func TestKickoffOnUnfreezeReachesTheOutsourceExecutor(t *testing.T) {
	api := newTasksTestServer(t)
	api.noOutsource = true
	task, worker := boundOutsourceTask(t, api, "解凍測試")

	ownerSetsPriority(t, api, task.ID, TaskPriorityFrozen)
	frozenCount := len(kickoffsTo(t, api, worker))

	ownerSetsPriority(t, api, task.ID, TaskPriorityHigh)
	got := kickoffsTo(t, api, worker)
	if len(got) != frozenCount+1 {
		t.Fatalf("an unfreeze must post exactly one kickoff: had %d, now %d",
			frozenCount, len(got))
	}

	body := got[len(got)-1].Body
	for _, want := range []string{
		TaskNo(task.ID),   // which task
		"解除凍結",            // what just changed
		"get_task_manual", // where the procedure lives
		"回報負責人",           // report before starting
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("kickoff must be self-contained, missing %q in:\n%s", want, body)
		}
	}
	if got[len(got)-1].Sender != wireSystemSender {
		t.Fatalf("the kickoff is machine-authored, sender %q",
			got[len(got)-1].Sender)
	}
}

// The freeze itself must stay silent: the notice claims the task is advanceable,
// and a frozen one is not. Without this, "notify on any priority write" would
// pass the test above.
func TestKickoffIsNeverPostedToAStillFrozenTask(t *testing.T) {
	api := newTasksTestServer(t)
	api.noOutsource = true
	task, worker := boundOutsourceTask(t, api, "凍結中不得收到")

	before := len(kickoffsTo(t, api, worker))
	ownerSetsPriority(t, api, task.ID, TaskPriorityFrozen)
	if got := len(kickoffsTo(t, api, worker)); got != before {
		t.Fatalf("freezing must post no kickoff: had %d, now %d", before, got)
	}
	// A blocker closing while the task is STILL frozen is the same claim from
	// the other direction: one of its two reasons to wait went away, not both.
	blocker := createAdHocTask(t, api, "m-front")
	setDeps(t, api, task.ID, blocker.ID)
	rec := httptest.NewRecorder()
	api.HandleTerminateTaskApiTasksTaskIdTerminatePost(rec,
		taskReq(t, "POST", "/x", nil, wireOwnerID, "owner"), blocker.ID)
	if rec.Code != http.StatusOK {
		t.Fatalf("terminate blocker: %d %s", rec.Code, rec.Body.String())
	}
	if got := len(kickoffsTo(t, api, worker)); got != before {
		t.Fatalf("a frozen task must stay silent when unblocked: had %d, now %d",
			before, got)
	}
}

// ── trigger ②: the assignment ────────────────────────────────────────────────

func TestKickoffOnAssignmentReachesTheFreshlyBoundWorker(t *testing.T) {
	api := newTasksTestServer(t)
	api.noOutsource = true
	putOutsourceManual(t, api, "review-pr", "claude-sonnet-4-5", 5)
	created := createOutsourceTask(t, api, "review-pr", "指派測試")

	api.runOutsourceTick(1000.0)
	bound, err := api.dal.GetTask(created.ID)
	if err != nil || bound == nil || bound.ExecutorID == "" {
		t.Fatalf("tick must bind a worker: %+v %v", bound, err)
	}
	got := kickoffsTo(t, api, bound.ExecutorID)
	if len(got) != 1 {
		t.Fatalf("the bind must post exactly one kickoff, got %d", len(got))
	}
	if !strings.Contains(got[0].Body, "指派給你") {
		t.Fatalf("the notice must name the change that happened:\n%s", got[0].Body)
	}
}

// ── trigger ③: the dependency release ────────────────────────────────────────

func TestKickoffOnDependencyReleaseReachesTheOutsourceExecutor(t *testing.T) {
	api := newTasksTestServer(t)
	api.noOutsource = true
	task, worker := boundOutsourceTask(t, api, "依賴解除測試")

	blocker := createAdHocTask(t, api, "m-front")
	setDeps(t, api, task.ID, blocker.ID)
	blocked := len(kickoffsTo(t, api, worker))

	rec := httptest.NewRecorder()
	api.HandleTerminateTaskApiTasksTaskIdTerminatePost(rec,
		taskReq(t, "POST", "/x", nil, wireOwnerID, "owner"), blocker.ID)
	if rec.Code != http.StatusOK {
		t.Fatalf("terminate blocker: %d %s", rec.Code, rec.Body.String())
	}
	got := kickoffsTo(t, api, worker)
	if len(got) != blocked+1 {
		t.Fatalf("a blocker closing must post one kickoff: had %d, now %d",
			blocked, len(got))
	}
	if !strings.Contains(got[len(got)-1].Body, "依賴解除") {
		t.Fatalf("the notice must name the change:\n%s", got[len(got)-1].Body)
	}
}

// The other way a blocker goes away: nobody closed it, somebody decided it no
// longer blocks.
func TestKickoffOnDepsEditReachesTheOutsourceExecutor(t *testing.T) {
	api := newTasksTestServer(t)
	api.noOutsource = true
	task, worker := boundOutsourceTask(t, api, "依賴改寫測試")

	blocker := createAdHocTask(t, api, "m-front")
	setDeps(t, api, task.ID, blocker.ID)
	blocked := len(kickoffsTo(t, api, worker))

	setDeps(t, api, task.ID) // wholesale replacement with an empty list
	if got := len(kickoffsTo(t, api, worker)); got != blocked+1 {
		t.Fatalf("clearing the last blocker must post one kickoff: had %d, now %d",
			blocked, got)
	}
}

// ── de-duplication ───────────────────────────────────────────────────────────

// The ledger is task.kickoff_notified_to (migrations/00056): it holds the
// executor id the notice was last posted to, and it is CLEARED whenever the task
// is observed non-advanceable. So a repeated write of the same advanceable state
// is silent, and a genuine re-entry into advanceable is not.
func TestKickoffLedgerSuppressesRepeatsButNotFreshTransitions(t *testing.T) {
	api := newTasksTestServer(t)
	api.noOutsource = true
	task, worker := boundOutsourceTask(t, api, "去重測試")

	baseline := len(kickoffsTo(t, api, worker))
	if baseline != 1 {
		t.Fatalf("fixture: the bind should have posted one kickoff, got %d", baseline)
	}

	// Repeats that must NOT re-post. The deps writes are the load-bearing ones:
	// they re-enter the seam with an eligible task every single time, so the
	// ledger is the ONLY thing standing between them and a second notice.
	api.runOutsourceTick(1030.0)
	api.runOutsourceTick(1060.0)
	ownerSetsPriority(t, api, task.ID, TaskPriorityMid)
	ownerSetsPriority(t, api, task.ID, TaskPriorityLow)
	setDeps(t, api, task.ID)
	setDeps(t, api, task.ID)
	setDeps(t, api, task.ID)
	if got := len(kickoffsTo(t, api, worker)); got != baseline {
		t.Fatalf("re-running the same advanceable state must stay silent: "+
			"had %d, now %d", baseline, got)
	}

	// A genuine freeze→unfreeze cycle must notify again — twice, to show the
	// ledger resets per transition rather than once ever.
	for i := 1; i <= 2; i++ {
		ownerSetsPriority(t, api, task.ID, TaskPriorityFrozen)
		ownerSetsPriority(t, api, task.ID, TaskPriorityHigh)
		if got := len(kickoffsTo(t, api, worker)); got != baseline+i {
			t.Fatalf("unfreeze #%d must notify: want %d, got %d",
				i, baseline+i, got)
		}
	}
}

// ── who must never receive one ───────────────────────────────────────────────

func TestKickoffIsNeverPostedToAMemberExecutor(t *testing.T) {
	api := newTasksTestServer(t)
	api.noOutsource = true
	if err := api.dal.PutMember(Member{
		ID: "m-staff", Name: "Rei", Kind: "assistant",
		RosterStatus: RosterStatusActive,
	}); err != nil {
		t.Fatalf("seed member: %v", err)
	}
	task := createAdHocTask(t, api, "m-staff")

	ownerSetsPriority(t, api, task.ID, TaskPriorityFrozen)
	ownerSetsPriority(t, api, task.ID, TaskPriorityHigh)
	if got := kickoffsTo(t, api, "m-staff"); len(got) != 0 {
		t.Fatalf("a member executor must receive no kickoff, got %d:\n%s",
			len(got), got[0].Body)
	}
}

func TestKickoffIsNeverPostedToAReleasedWorker(t *testing.T) {
	api := newTasksTestServer(t)
	api.noOutsource = true
	task, worker := boundOutsourceTask(t, api, "已退場的 worker")

	ownerSetsPriority(t, api, task.ID, TaskPriorityFrozen)
	if _, err := api.dal.ReleaseWorkersForTask(task.ID, 2000.0); err != nil {
		t.Fatalf("release worker: %v", err)
	}
	before := len(kickoffsTo(t, api, worker))

	ownerSetsPriority(t, api, task.ID, TaskPriorityHigh)
	if got := len(kickoffsTo(t, api, worker)); got != before {
		t.Fatalf("a released worker must receive no kickoff: had %d, now %d",
			before, got)
	}
}

// taskKickoffEligible carries the cheap refusals. Driven directly because two of
// them (terminal, reassigning) are unreachable through the priority route, which
// refuses a terminal task at the door — a behavioural test there would be a test
// for an unreachable branch.
func TestTaskKickoffEligible(t *testing.T) {
	live := Task{
		ID: "t-1", ExecutorKind: TaskExecutorOutsource, ExecutorID: "ow-1",
		Status: TaskStatusInProgress, Priority: TaskPriorityMid,
	}
	if !taskKickoffEligible(live) {
		t.Fatalf("positive control: a live outsource task must be eligible: %+v", live)
	}
	for _, tc := range []struct {
		name string
		mut  func(*Task)
	}{
		{"a member executor", func(x *Task) { x.ExecutorKind = TaskExecutorMember }},
		{"an unassigned slot", func(x *Task) { x.ExecutorID = "" }},
		{"a done task", func(x *Task) { x.Status = TaskStatusDone }},
		{"a terminated task", func(x *Task) { x.Status = TaskStatusTerminated }},
		{"a frozen task", func(x *Task) { x.Priority = TaskPriorityFrozen }},
		{"a handover hold", func(x *Task) { x.Lock = TaskLockReassigning }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			x := live
			tc.mut(&x)
			if taskKickoffEligible(x) {
				t.Fatalf("%s must not be eligible: %+v", tc.name, x)
			}
		})
	}
}
