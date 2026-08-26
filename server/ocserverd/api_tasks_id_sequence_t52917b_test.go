package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strconv"
	"sync"
	"testing"
)

// ── T-52917b:遞增票號 ─────────────────────────────────────────────────────────
//
// 🔴 THE ASSERTION THAT MATTERS IS THE ROW COUNT, NOT "ARE THERE DUPLICATES".
//
// task.id is a TEXT PRIMARY KEY and PutTask is an `ON CONFLICT (id) DO UPDATE`
// upsert. Put those two together and a minting collision can NEVER show up as
// two rows sharing an id — the second create silently OVERWRITES the first and
// the API still answers 200. The damage is a MISSING ROW, so the only assertion
// that can see it counts rows. A test that scanned the returned ids for
// duplicates would be evergreen: the id column physically cannot hold one.
//
// (Even on a driver where the second write ERRORED instead of overwriting, a
// UNIQUE index is a damage DETECTOR, not a fallback — the row is still lost.
// Either way: count the rows.)
//
// The property under test is UNIQUENESS, not contiguity. A gap (a burned
// number from a rolled-back transaction) is fine; two tasks called T-7 is not.

var taskSeqIDRe = regexp.MustCompile(`^T-[1-9][0-9]*$`)

// TestConcurrentCreatesMintDistinctIncrementalIDs drives N REAL create_task
// requests through the REAL handler concurrently and then asks the database how
// many task rows exist.
//
// N is deliberately small (32). This is a correctness probe, not a load test.
func TestConcurrentCreatesMintDistinctIncrementalIDs(t *testing.T) {
	const n = 32
	api := newTasksTestServer(t)

	var mu sync.Mutex
	ids := make([]string, 0, n)
	codes := make([]int, 0, n)

	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start // release them together so the mint window is actually contested
			rec := httptest.NewRecorder()
			api.HandleCreateTaskApiTasksPost(rec, taskReq(t, "POST", "/api/tasks",
				map[string]any{
					"title":              "concurrent " + strconv.Itoa(i),
					"executor_member_id": "m-exec",
				}, "m-exec", "agent"))
			mu.Lock()
			defer mu.Unlock()
			codes = append(codes, rec.Code)
			if rec.Code == http.StatusOK {
				var out taskCreateResultDTO
				if err := json.Unmarshal(rec.Body.Bytes(), &out); err == nil {
					ids = append(ids, out.Task.ID)
				}
			}
		}(i)
	}
	close(start)
	wg.Wait()

	for _, c := range codes {
		if c != http.StatusOK {
			t.Fatalf("a create returned %d, want 200 — every request must succeed "+
				"before the row count means anything", c)
		}
	}

	// ① 🔴 THE LOAD-BEARING ASSERTION. One create ⇒ one durable row. A minting
	// collision cannot show as a duplicate id (upsert on a TEXT PRIMARY KEY), so
	// it shows here and nowhere else.
	var rows int
	if err := api.dal.rdb.QueryRow(`SELECT COUNT(*) FROM task`).Scan(&rows); err != nil {
		t.Fatalf("count task rows: %v", err)
	}
	if rows != n {
		t.Fatalf("ROW COUNT %d, want %d — %d create_task calls all answered 200 but "+
			"%d task row(s) are missing: two creates minted the SAME id and the "+
			"upsert overwrote the first one", rows, n, n, n-rows)
	}

	// ② the ids the API HANDED BACK must also be n distinct values. If the API
	// returned the same id twice while the table happens to hold n rows, the
	// caller was lied to about which task is theirs.
	seen := map[string]bool{}
	for _, id := range ids {
		if seen[id] {
			t.Fatalf("create_task returned id %q twice", id)
		}
		seen[id] = true
	}
	if len(seen) != n {
		t.Fatalf("distinct returned ids = %d, want %d", len(seen), n)
	}

	// ③ the FORMAT: T-<遞增整數>, not "t-" + random hex.
	nums := map[int]bool{}
	for id := range seen {
		if !taskSeqIDRe.MatchString(id) {
			t.Fatalf("task id %q is not T-<遞增整數>", id)
		}
		v, err := strconv.Atoi(id[2:])
		if err != nil {
			t.Fatalf("task id %q: %v", id, err)
		}
		if nums[v] {
			t.Fatalf("number %d minted twice", v)
		}
		nums[v] = true
	}
	if len(nums) != n {
		t.Fatalf("distinct numbers = %d, want %d", len(nums), n)
	}
}

// TestSequentialCreatesMintAscendingIDs pins the plain, uncontended case: the
// first task on a fresh database is T-1 and each next one is larger.
func TestSequentialCreatesMintAscendingIDs(t *testing.T) {
	api := newTasksTestServer(t)
	prev := 0
	for i := 0; i < 3; i++ {
		task := createAdHocTask(t, api, "m-exec")
		if !taskSeqIDRe.MatchString(task.ID) {
			t.Fatalf("task id %q is not T-<遞增整數>", task.ID)
		}
		v, _ := strconv.Atoi(task.ID[2:])
		if i == 0 && v != 1 {
			t.Fatalf("first task on a fresh db is %q, want T-1", task.ID)
		}
		if v <= prev {
			t.Fatalf("id %q did not ascend past %d", task.ID, prev)
		}
		prev = v
	}
}

// TestLegacyRandomHexTaskIDsStayUsable is the 舊票不動 guard: a task whose id is
// the OLD "t-" + 12 random hex must still be readable and drivable, and minting
// a new task must not disturb it.
func TestLegacyRandomHexTaskIDsStayUsable(t *testing.T) {
	api := newTasksTestServer(t)
	const legacy = "t-72dd79b666d0"
	now := nowSecs()
	if err := api.dal.PutTask(Task{
		ID: legacy, Title: "legacy task", Status: TaskStatusNotStarted,
		Priority: TaskPriorityMid, ExecutorKind: TaskExecutorMember,
		ExecutorID: "m-exec", CreatedTS: now, UpdatedTS: now,
	}); err != nil {
		t.Fatalf("seed legacy task: %v", err)
	}

	// readable by its exact id
	got, err := api.dal.GetTask(legacy)
	if err != nil || got == nil {
		t.Fatalf("GetTask(%q) = %v, %v — a legacy task must stay readable", legacy, got, err)
	}
	if got.ID != legacy {
		t.Fatalf("id came back as %q, want %q — legacy ids must not be rewritten", got.ID, legacy)
	}

	// readable through the REAL get_task endpoint too, by the same exact id
	if view := getTaskView(t, api, legacy); view.ID != legacy {
		t.Fatalf("get_task answered id %q, want %q", view.ID, legacy)
	}

	// drivable: a plan lands on it through the real endpoint
	view := submitPlan(t, api, legacy, "m-exec", []map[string]any{
		{"name": "step one", "dod": "done"},
	})
	if len(view.Steps) != 1 {
		t.Fatalf("legacy task took %d steps, want 1 — it must stay drivable", len(view.Steps))
	}

	// and DRIVEN: a step transition through the real endpoint moves it. This is
	// the half a read-only check would miss — the write path resolves the task by
	// the same byte-exact id and must not care what shape it is.
	if code := reportStepStatus(t, api, legacy, view.Steps[0].ID,
		"m-exec", StepStatusInProgress, "").Code; code != http.StatusOK {
		t.Fatalf("driving a legacy task's step answered %d, want 200", code)
	}
	driven := getTaskView(t, api, legacy)
	if driven.Steps[0].Status != StepStatusInProgress {
		t.Fatalf("legacy task step is %q, want %q — a legacy task must stay drivable",
			driven.Steps[0].Status, StepStatusInProgress)
	}

	// and minting a NEW task leaves it alone
	fresh := createAdHocTask(t, api, "m-exec")
	if fresh.ID == legacy {
		t.Fatalf("new mint collided with the legacy id")
	}
	still, err := api.dal.GetTask(legacy)
	if err != nil || still == nil || still.Title != "legacy task" {
		t.Fatalf("legacy task damaged by a new mint: %v %v", still, err)
	}
}
