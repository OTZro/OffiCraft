package main

// api_tasks_list_current_step_t66_test.go — T-66: the light task list carries
// `current_step_id` / `current_step_name`, so 「這張票現在卡在哪一步」 no longer
// costs a get_task per row.
//
// 🔴 WHAT THIS FILE PINS, and why each part is load-bearing:
//
//   1. AGREEMENT WITH get_task. The list value is computed by a SQL twin
//      (dal.AllTaskCurrentStep, one grouped window query) of the in-memory rule
//      (domain.CurrentStep, which resumeTasksFor also calls). Two definitions of
//      "the current step" is how they drift, so the assertion is not "the list
//      says step 2" — a literal that would still pass if BOTH sides were wrong —
//      but "the list agrees with the step rows get_task returns", derived from
//      the response, not from the fixture's expectations.
//   2. THE TWO EMPTY CASES. "" is a real answer, not a failure: an empty plan
//      and a fully-finished plan both mean THERE IS NO CURRENT STEP. The
//      tempting bug is to fall back to the first row, which would tell an agent
//      to re-do finished work, so both are pinned separately.
//   3. superseded IS TERMINAL. A superseded row is frozen replan history
//      (T-1aea) — it must be SKIPPED even when it sits first in order_idx.
//      Without this case a `status != done` implementation passes everything
//      else here.
//
// The query-count side (this must never become a per-task step read) is a
// different property and lives in api_perf_query_count_test.go, whose
// `task_step` barrel was added by this same ticket.

import (
	"net/http/httptest"
	"testing"
)

// t66Server builds a bare api server on a fresh DAL — the same shape the other
// task tests use.
func t66Server(t *testing.T) *apiServer {
	t.Helper()
	return &apiServer{dal: newTestDAL(t), hub: NewHub()}
}

// t66PutTask seeds one live task.
func t66PutTask(t *testing.T, s *apiServer, id, title string) {
	t.Helper()
	if err := s.dal.PutTask(Task{
		ID: id, Title: title, Status: TaskStatusInProgress,
		Priority: TaskPriorityMid, ExecutorKind: TaskExecutorMember,
		ExecutorID: "m-1", CreatedTS: 1000, UpdatedTS: 1000,
	}); err != nil {
		t.Fatal(err)
	}
}

// t66PutStep seeds one step. dod is deliberately non-empty on every row: the
// light list must carry the step's id and NAME and never its dod, and a test
// whose fixtures had empty dod could not tell the difference.
func t66PutStep(t *testing.T, s *apiServer, taskID, id string, idx int, name, status string) {
	t.Helper()
	if err := s.dal.PutTaskStep(TaskStep{
		ID: id, TaskID: taskID, OrderIdx: idx, Name: name,
		DoD:    "DoD for " + name + " — fat text the light list must not carry",
		Status: status,
	}); err != nil {
		t.Fatal(err)
	}
}

// t66ListRow reads the light list and returns the one row for taskID.
func t66ListRow(t *testing.T, s *apiServer, taskID string) taskListItemDTO {
	t.Helper()
	for _, row := range listTaskRows(t, s, HandleListTasksApiTasksGetParams{}) {
		if row.ID == taskID {
			return row
		}
	}
	t.Fatalf("task %s missing from the light list", taskID)
	return taskListItemDTO{}
}

// t66CurrentStepPerGetTask derives the current step from what GET
// /api/tasks/{id} actually returned — the OTHER endpoint's answer, in its own
// step order, using the shared terminal rule. This is the oracle: it never
// reads the list's own value, so agreement is evidence rather than tautology.
func t66CurrentStepPerGetTask(t *testing.T, s *apiServer, taskID string) (id, name string) {
	t.Helper()
	rec := httptest.NewRecorder()
	s.HandleGetTaskApiTasksTaskIdGet(rec,
		taskReq(t, "GET", "/api/tasks/"+taskID, nil, "owner", "owner"), taskID)
	if rec.Code != 200 {
		t.Fatalf("get_task %s → %d: %s", taskID, rec.Code, rec.Body.String())
	}
	full := decodeBody[taskDTO](t, rec)
	for _, st := range full.Steps {
		if !StepIsTerminal(st.Status) {
			return st.ID, st.Name
		}
	}
	return "", ""
}

// TestTaskListCurrentStepAgreesWithGetTask is the core pin: for a plan with a
// finished head, a superseded row and two live rows, the list's pointer is the
// SAME step get_task shows as current.
func TestTaskListCurrentStepAgreesWithGetTask(t *testing.T) {
	s := t66Server(t)
	const taskID = "t-cur0000000001"
	t66PutTask(t, s, taskID, "有計畫的票")
	// order_idx ascending; the first two are terminal, so the answer is s3.
	t66PutStep(t, s, taskID, "ts-c1", 1, "盤點現況", StepStatusDone)
	t66PutStep(t, s, taskID, "ts-c2", 2, "舊的做法(已被改寫)", StepStatusSuperseded)
	t66PutStep(t, s, taskID, "ts-c3", 3, "實作", StepStatusInProgress)
	t66PutStep(t, s, taskID, "ts-c4", 4, "驗收", StepStatusPending)

	wantID, wantName := t66CurrentStepPerGetTask(t, s, taskID)
	// 語料自證:oracle 必須真的指到東西,否則下面的等式在兩邊都空時恆真。
	if wantID == "" || wantName == "" {
		t.Fatalf("語料不合格:get_task 沒有當前步驟可比(id=%q name=%q)", wantID, wantName)
	}
	if wantID != "ts-c3" {
		t.Fatalf("oracle 本身就錯了:預期 ts-c3(第一個非終態),得到 %q — "+
			"superseded 是凍結的改寫歷史,不是工作中的節點", wantID)
	}

	row := t66ListRow(t, s, taskID)
	if row.CurrentStepID != wantID || row.CurrentStepName != wantName {
		t.Fatalf("list 的當前步驟和 get_task 不一致:list=(%q, %q) get_task=(%q, %q)",
			row.CurrentStepID, row.CurrentStepName, wantID, wantName)
	}
	// 名字要是真的名字,不是 id 的複製品。
	if row.CurrentStepName != "實作" {
		t.Fatalf("current_step_name 應為步驟名 '實作',得到 %q", row.CurrentStepName)
	}
}

// TestTaskListCurrentStepIsEmptyWithoutAPlan pins empty case #1: a task with no
// steps at all. Both fields must be "" — the honest 「還沒有計畫」.
func TestTaskListCurrentStepIsEmptyWithoutAPlan(t *testing.T) {
	s := t66Server(t)
	const taskID = "t-cur0000000002"
	t66PutTask(t, s, taskID, "還沒排計畫的票")

	wantID, wantName := t66CurrentStepPerGetTask(t, s, taskID)
	if wantID != "" || wantName != "" {
		t.Fatalf("oracle 不合格:沒有步驟卻算出當前步驟 (%q, %q)", wantID, wantName)
	}
	row := t66ListRow(t, s, taskID)
	if row.CurrentStepID != "" || row.CurrentStepName != "" {
		t.Fatalf("計畫為空時兩格都該是空字串,得到 (%q, %q) — "+
			"不能拿第一步當預設,那會是憑空發明的工作",
			row.CurrentStepID, row.CurrentStepName)
	}
	// 反恆真:同一次回應裡,一張有計畫的票必須仍然講得出當前步驟,
	// 否則「兩格是空的」也可能只是欄位整條壞掉。
	const liveID = "t-cur0000000003"
	t66PutTask(t, s, liveID, "有計畫的票")
	t66PutStep(t, s, liveID, "ts-d1", 1, "動工", StepStatusInProgress)
	if got := t66ListRow(t, s, liveID); got.CurrentStepID != "ts-d1" {
		t.Fatalf("對照組壞了:有計畫的票也沒有當前步驟(%q)— 這一跑什麼都沒證明",
			got.CurrentStepID)
	}
}

// TestTaskListCurrentStepIsEmptyWhenEveryStepIsFinished pins empty case #2:
// every step has reached a TERMINAL state (done, or superseded). Falling back to
// the first row here would point an agent at work that is already finished.
func TestTaskListCurrentStepIsEmptyWhenEveryStepIsFinished(t *testing.T) {
	s := t66Server(t)
	const taskID = "t-cur0000000004"
	t66PutTask(t, s, taskID, "全部做完的票")
	t66PutStep(t, s, taskID, "ts-e1", 1, "盤點現況", StepStatusDone)
	t66PutStep(t, s, taskID, "ts-e2", 2, "舊的做法(已被改寫)", StepStatusSuperseded)
	t66PutStep(t, s, taskID, "ts-e3", 3, "實作", StepStatusDone)

	wantID, wantName := t66CurrentStepPerGetTask(t, s, taskID)
	if wantID != "" || wantName != "" {
		t.Fatalf("oracle 不合格:全部終態卻算出當前步驟 (%q, %q)", wantID, wantName)
	}
	// 語料自證:這張票真的有步驟(不是空計畫走錯了分支)。
	row := t66ListRow(t, s, taskID)
	if row.ProgressTotal == 0 {
		t.Fatalf("語料不合格:這一跑的票根本沒有步驟,測不到「全部完成」這個情境")
	}
	if row.CurrentStepID != "" || row.CurrentStepName != "" {
		t.Fatalf("全部完成時兩格都該是空字串,得到 (%q, %q) — "+
			"回退到第一步會叫人重做已完成的工作",
			row.CurrentStepID, row.CurrentStepName)
	}
}

// TestTaskListCurrentStepRuleIsTheSharedOne asserts the single-source-of-truth
// claim directly: dal.AllTaskCurrentStep (SQL) and domain.CurrentStep (memory)
// must give the same answer for every task in one population — including the
// two empty cases. If someone re-implements the rule in SQL and it drifts, this
// is what reddens.
func TestTaskListCurrentStepRuleIsTheSharedOne(t *testing.T) {
	s := t66Server(t)
	fixtures := map[string][][3]string{
		// taskID → [stepID, name, status]
		"t-cur0000000010": {{"ts-a1", "第一步", StepStatusDone}, {"ts-a2", "第二步", StepStatusPending}},
		"t-cur0000000011": {{"ts-b1", "唯一一步", StepStatusWaitingOwner}},
		"t-cur0000000012": {{"ts-f1", "改寫掉的", StepStatusSuperseded}, {"ts-f2", "真的在做", StepStatusInProgress}},
		"t-cur0000000013": {{"ts-g1", "做完了", StepStatusDone}},
		"t-cur0000000014": nil, // no plan at all
	}
	for id, steps := range fixtures {
		t66PutTask(t, s, id, "票 "+id)
		for i, st := range steps {
			t66PutStep(t, s, id, st[0], i+1, st[1], st[2])
		}
	}

	sqlSide, err := s.dal.AllTaskCurrentStep()
	if err != nil {
		t.Fatalf("AllTaskCurrentStep: %v", err)
	}
	nonEmpty := 0
	for id := range fixtures {
		steps, err := s.dal.ListTaskSteps(id)
		if err != nil {
			t.Fatal(err)
		}
		wantID, wantName := CurrentStep(steps)
		got := sqlSide[id] // absent = zero value = ("", ""), which is the rule's own answer
		if got.ID != wantID || got.Name != wantName {
			t.Errorf("%s: SQL 與記憶體版本的「當前步驟」不一致:SQL=(%q, %q) domain=(%q, %q)",
				id, got.ID, got.Name, wantID, wantName)
		}
		if wantID != "" {
			nonEmpty++
		}
	}
	// 反恆真:如果每一格都空,上面的等式全部是 ""=="",什麼都沒證明。
	if nonEmpty < 3 {
		t.Fatalf("語料不合格:只有 %d 張票算得出當前步驟,兩邊都空的等式證明不了任何事", nonEmpty)
	}
	// 而且 SQL 那一邊不能替沒有當前步驟的票憑空生一列出來。
	if _, ok := sqlSide["t-cur0000000013"]; ok {
		t.Errorf("全部完成的票不該出現在 AllTaskCurrentStep 的結果裡")
	}
	if _, ok := sqlSide["t-cur0000000014"]; ok {
		t.Errorf("沒有計畫的票不該出現在 AllTaskCurrentStep 的結果裡")
	}
}
