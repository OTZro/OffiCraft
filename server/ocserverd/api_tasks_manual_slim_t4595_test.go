package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"unicode/utf8"
)

// ── get_my_task does not send the manual a SECOND time (T-4595) ──────────────
//
// This is NOT "the response is too big". It is the SAME document sent TWICE in
// the SAME session: buildWorkerBootContext already puts the whole manual (Q1
// purpose / Q2 fields / Q3 SOP / learnings) verbatim into the worker's initial
// prompt, and the first thing a worker does on boot is call get_my_task, which
// used to answer with all of it again. Measured on live worker rows the claim
// returned 28k-34k characters of which 92-98% was byte-identical to the boot
// context; one claim reached 98,271 characters and was refused outright by the
// client's tool layer.
//
// The TASK BODY is deliberately untouched by the same change, and these tests
// pin that asymmetry too (TestMyTaskStillServesTheLiveTaskBodyInFull): the task
// is LIVE — an owner editing the ticket after spawn reaches the worker ONLY
// through this response — while the manual is a stable type-level document with
// its own tool.
//
// Discrimination: the mutant these tests exist for is "serve the whole manual
// again", i.e. newTaskManualListItemDTO → newTaskManualDTO in
// HandleGetMyTaskApiSelfTaskGet. Assertions that only check a field EXISTS
// cannot see that mutant (the fat body fills every field the thin one does), so
// the assertions below are: the two authored documents are EMPTY, the omission
// COUNT is right, and their text does not appear ANYWHERE in the response bytes.

const (
	slimSopMD     = "SOP-SENTINEL：先讀這一段再動手。"
	slimLearnings = "LEARNINGS-SENTINEL：前人踩過的坑。"
	slimPurpose   = "PURPOSE-SENTINEL"
)

// seedFatManual seeds a manual whose two authored documents are non-empty and
// individually recognisable in a raw response body.
func seedFatManual(t *testing.T, api *apiServer, typeKey string) TaskManual {
	t.Helper()
	m := TaskManual{
		TypeKey:     typeKey,
		DisplayName: "改一份 PR",
		Purpose:     slimPurpose,
		Fields:      `[{"name":"pr","required":true,"is_key":true}]`,
		SopMD:       slimSopMD,
		Learnings:   slimLearnings,
		Assignee:    `{"kind":"member","member_id":"m-exec"}`,
	}
	if err := api.dal.PutTaskManual(m); err != nil {
		t.Fatalf("seed manual: %v", err)
	}
	return m
}

// claimAs binds a worker to task and performs its get_my_task claim.
func claimAs(t *testing.T, api *apiServer, workerID, taskID string) *httptest.ResponseRecorder {
	t.Helper()
	if err := api.dal.PutOutsourceWorker(OutsourceWorker{
		ID: workerID, Codename: "S-9", Model: "sonnet",
		TaskID: taskID, Status: WorkerStatusAssigned, CreatedTS: 1,
	}); err != nil {
		t.Fatalf("seed worker: %v", err)
	}
	rec := httptest.NewRecorder()
	api.HandleGetMyTaskApiSelfTaskGet(rec,
		taskReq(t, "GET", "/api/self/task", nil, workerID, "agent"))
	if rec.Code != http.StatusOK {
		t.Fatalf("claim: %d %s", rec.Code, rec.Body.String())
	}
	return rec
}

func TestMyTaskServesTheManualLightAndSaysHowMuchItLeftOut(t *testing.T) {
	api := newTasksTestServer(t)
	seedFatManual(t, api, "review-pr")
	created, code := createTypedTask(t, api, "review-pr", "77")
	if code != http.StatusOK {
		t.Fatalf("create: %d", code)
	}
	rec := claimAs(t, api, "ow-slim", created.Task.ID)
	body := rec.Body.String()
	got := decodeBody[myTaskDTO](t, rec)

	if got.Manual == nil {
		t.Fatalf("the manual must NOT vanish — a worker that reads null here " +
			"concludes its type has no manual at all")
	}
	// It still says WHICH manual, so get_task_manual is addressable.
	if got.Manual.TypeKey != "review-pr" {
		t.Fatalf("type_key must survive the trim (it is how the worker asks "+
			"get_task_manual for the rest): %+v", got.Manual)
	}
	// The two authored documents are gone…
	if got.Manual.SopMD != "" || got.Manual.Learnings != "" {
		t.Fatalf("sop_md/learnings must be served EMPTY here — the worker's "+
			"boot context already carries both verbatim: sop_md=%q learnings=%q",
			got.Manual.SopMD, got.Manual.Learnings)
	}
	// …and so is every other byte of them, by whatever route.
	for _, sentinel := range []string{slimSopMD, slimLearnings} {
		if strings.Contains(body, sentinel) {
			t.Fatalf("the manual text is still on this wire (%q found in the "+
				"response body) — that is the second copy this projection exists "+
				"to remove", sentinel)
		}
	}
	// …but the SIZES are reported on the stored row, never as a lying zero.
	wantSop := utf8.RuneCountInString(slimSopMD)
	wantLearn := utf8.RuneCountInString(slimLearnings)
	if got.Manual.SopMDChars != wantSop || got.Manual.LearningsChars != wantLearn {
		t.Fatalf("the light row must still MEASURE the omitted text: "+
			"sop_md_chars=%d (want %d) learnings_chars=%d (want %d)",
			got.Manual.SopMDChars, wantSop, got.Manual.LearningsChars, wantLearn)
	}
	if got.Manual.SopMDCapChars == 0 || got.Manual.LearningsCapChars == 0 {
		t.Fatalf("the caps those sizes are judged against must stay: %+v", got.Manual)
	}
	// The omission is reported as a NUMBER, in the SAME shape steps use.
	if want := wantSop + wantLearn; got.ManualOmittedChars != want {
		t.Fatalf("manual_omitted_chars = %d, want %d (runes of sop_md + learnings)",
			got.ManualOmittedChars, want)
	}
	// The identity fields the worker reads to recognise its own type stay.
	if got.Manual.DisplayName != "改一份 PR" || got.Manual.Purpose != slimPurpose {
		t.Fatalf("type identity must survive the trim: %+v", got.Manual)
	}
}

// The ad-hoc case: no manual means no omission, and the count must not invent a
// number for a document that does not exist.
func TestMyTaskWithoutAManualOmitsNothing(t *testing.T) {
	api := newTasksTestServer(t)
	rec := httptest.NewRecorder()
	api.HandleCreateTaskApiTasksPost(rec, taskReq(t, "POST", "/api/tasks",
		map[string]any{"title": "ad-hoc", "executor_member_id": "m-exec"},
		"m-exec", "agent"))
	if rec.Code != http.StatusOK {
		t.Fatalf("create ad-hoc: %d %s", rec.Code, rec.Body.String())
	}
	created := decodeBody[taskCreateResultDTO](t, rec)

	got := decodeBody[myTaskDTO](t, claimAs(t, api, "ow-adhoc", created.Task.ID))
	if got.Manual != nil {
		t.Fatalf("an ad-hoc task has no manual: %+v", got.Manual)
	}
	if got.ManualOmittedChars != 0 {
		t.Fatalf("manual_omitted_chars = %d, want 0 — nothing was left out",
			got.ManualOmittedChars)
	}
}

// 🔴 The half that is deliberately NOT trimmed. get_my_task is LIVE: the boot
// context is a snapshot taken at spawn, so when the owner edits the ticket
// AFTER the worker started, this response is the only place the worker can see
// it. Trimming the task body to match the manual would make that edit
// invisible — the exact trade-off this change refuses to make.
func TestMyTaskStillServesTheLiveTaskBodyInFull(t *testing.T) {
	api := newTasksTestServer(t)
	seedFatManual(t, api, "review-pr")
	created, code := createTypedTask(t, api, "review-pr", "77")
	if code != http.StatusOK {
		t.Fatalf("create: %d", code)
	}
	plan := submitPlan(t, api, created.Task.ID, "m-exec", []map[string]any{
		{"name": "gather", "dod": "DODONE"},
	})
	if rec := reportStepStatus(t, api, created.Task.ID, plan.Steps[0].ID,
		"m-exec", StepStatusInProgress, ""); rec.Code != http.StatusOK {
		t.Fatalf("start step: %d %s", rec.Code, rec.Body.String())
	}
	// The owner rewrites the ticket AFTER the worker's boot context was built.
	rec := httptest.NewRecorder()
	api.HandleUpdateTaskDescriptionApiTasksTaskIdDescriptionPost(rec,
		taskReq(t, "POST", "/api/tasks/"+created.Task.ID+"/description",
			map[string]any{"description": "EDITED-AFTER-SPAWN"},
			wireOwnerID, "owner"), created.Task.ID)
	if rec.Code != http.StatusOK {
		t.Fatalf("edit description: %d %s", rec.Code, rec.Body.String())
	}

	got := decodeBody[myTaskDTO](t, claimAs(t, api, "ow-live", created.Task.ID))
	if got.Task.Description != "EDITED-AFTER-SPAWN" {
		t.Fatalf("the task body must stay LIVE and whole — it is the worker's "+
			"only view of an owner's edit: %q", got.Task.Description)
	}
	if len(got.Task.Steps) != 1 || got.Task.Steps[0].DoD != "DODONE" {
		t.Fatalf("the current step must keep its full content: %+v", got.Task.Steps)
	}
}

// 🔴 The precondition of this whole change: the worker can still GET the manual.
// If get_task_manual were out of reach for an `ow-` caller, trimming it out of
// get_my_task would take the document away rather than de-duplicate it.
func TestWorkerCanStillPullTheWholeManualItself(t *testing.T) {
	api := newTasksTestServer(t)
	seedFatManual(t, api, "review-pr")

	// (a) the route's floor is reachable by a worker. A worker's roster row is
	// kind=outsource with role_key "" → classifyMember gives principalAgent,
	// which outranks the machine floor this route declares.
	var spec *RouteSpec
	for i, rs := range defaultRouteSpecs() {
		if rs.MCPTool == "get_task_manual" {
			spec = &defaultRouteSpecs()[i]
			break
		}
	}
	if spec == nil {
		t.Fatalf("get_task_manual is not on the routes table")
	}
	workerClass := classifyMember(&Member{Kind: KindOutsource, RoleKey: ""})
	if !principalAtLeast(workerClass, spec.Requires) {
		t.Fatalf("get_task_manual requires %q and an outsource worker resolves "+
			"to %q — this whole change is unsafe: STOP", spec.Requires, workerClass)
	}

	// (b) and the handler really answers a worker with the whole document.
	rec := httptest.NewRecorder()
	api.HandleGetTaskManualApiTaskManualsTypeKeyGet(rec,
		taskReq(t, "GET", "/api/task-manuals/review-pr", nil, "ow-slim", "agent"),
		"review-pr")
	if rec.Code != http.StatusOK {
		t.Fatalf("worker pulling its own manual: %d %s", rec.Code, rec.Body.String())
	}
	var full taskManualDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &full); err != nil {
		t.Fatalf("decode manual: %v", err)
	}
	if full.SopMD != slimSopMD || full.Learnings != slimLearnings {
		t.Fatalf("get_task_manual must serve the WHOLE manual (that is the "+
			"route get_my_task now points at): %+v", full)
	}
}
