package main

// worker_model_novalue_change_ted79_test.go — T-ed79 parity #3: saving a
// worker's model WITHOUT changing it must not cost it a session.
//
// The staff face has compared old against new since T-b6d9 (the three LAUNCH
// INTENTS on HandleUpdateMember). The worker face did not compare at all: any
// POST …/model on a live worker opened a wind-down and ended in kill+respawn,
// so re-saving the same values threw a round of work away for nothing. Nothing
// in the code said why the two differed, and nothing pinned either.

import (
	"net/http"
	"testing"
)

func setWorkerModelBody(t *testing.T, api *apiServer, workerID string, body map[string]any) {
	t.Helper()
	rec := postWorker(t, api, workerID, "model", body,
		api.HandleSetOutsourceWorkerModelApiOutsourceWorkersIdModelPost)
	if rec.Code != http.StatusOK {
		t.Fatalf("set model: %d %s", rec.Code, rec.Body.String())
	}
}

// TestSetWorkerModel_SameValuesDoNotRecycleTheSession: the model, runtime and
// effort the worker is ALREADY running on are re-sent — the shape a cockpit
// dialog produces every time the owner opens it, changes one unrelated thing
// and presses save. Nothing may be recycled.
func TestSetWorkerModel_SameValuesDoNotRecycleTheSession(t *testing.T) {
	api := newTasksTestServer(t)
	api.noOutsource = true
	workerID := newActiveOnlineWorker(t, api)
	before, _ := api.dal.GetOutsourceWorker(workerID)
	api.hub.DrainWardenCommands(ServerSelfHost)

	setWorkerModelBody(t, api, workerID, map[string]any{
		"model":   before.Model,
		"runtime": before.Runtime,
		"effort":  before.Effort,
	})

	after, _ := api.dal.GetOutsourceWorker(workerID)
	if after.RefocusSince != 0 {
		t.Errorf("re-saving the SAME model opened a wind-down (refocus_since=%v). The "+
			"staff face compares old against new before it recycles anything; storing "+
			"an identical value and then taking the session down for it is pure waste.",
			after.RefocusSince)
	}
	if got := len(api.hub.DrainWardenCommands(ServerSelfHost)); got != 0 {
		t.Errorf("a no-op model save dispatched %d warden frames, want 0", got)
	}
	if after.Model != before.Model || after.Runtime != before.Runtime ||
		after.Effort != before.Effort {
		t.Errorf("a no-op save must still leave the row exactly as it was: "+
			"%q/%q/%q, want %q/%q/%q", after.Model, after.Runtime, after.Effort,
			before.Model, before.Runtime, before.Effort)
	}
}

// …and the omitted-field shape, which is the same question asked differently:
// a body that names ONLY effort must not be read as "model → blank".
func TestSetWorkerModel_OmittedFieldsAreNotAChange(t *testing.T) {
	api := newTasksTestServer(t)
	api.noOutsource = true
	workerID := newActiveOnlineWorker(t, api)
	before, _ := api.dal.GetOutsourceWorker(workerID)
	api.hub.DrainWardenCommands(ServerSelfHost)

	setWorkerModelBody(t, api, workerID, map[string]any{"effort": before.Effort})

	after, _ := api.dal.GetOutsourceWorker(workerID)
	if after.RefocusSince != 0 {
		t.Errorf("a body that changes nothing opened a wind-down (refocus_since=%v)",
			after.RefocusSince)
	}
	if got := len(api.hub.DrainWardenCommands(ServerSelfHost)); got != 0 {
		t.Errorf("a no-op model save dispatched %d warden frames, want 0", got)
	}
}

// The other direction, so the compare cannot be "satisfied" by never recycling:
// a REAL change still winds the worker down, exactly as before.
func TestSetWorkerModel_ARealChangeStillWindsDown(t *testing.T) {
	api := newTasksTestServer(t)
	api.noOutsource = true
	workerID := newActiveOnlineWorker(t, api)
	api.hub.DrainWardenCommands(ServerSelfHost)

	setWorkerModelBody(t, api, workerID, map[string]any{"model": "claude-opus-4-9"})

	after, _ := api.dal.GetOutsourceWorker(workerID)
	if after.Model != "claude-opus-4-9" {
		t.Fatalf("the new model must persist, got %q", after.Model)
	}
	if after.RefocusSince <= 0 {
		t.Error("a genuine model change must still open the wind-down that takes it live")
	}
}
