package main

import (
	"net/http"
	"testing"
)

// worker_winddown_parity_t170e_test.go — T-170e stage 1. Three wind-down
// protections that staff have and an outsource worker did NOT, each measured
// on the worker side before it was written.
//
// The common cause is one shape: a worker row IS a member row
// (memberFromWorker), but every shared wind-down pass is reached from
// runReconcileTick, whose roster read is ListMembers — and ListMembers is
// `WHERE kind != 'outsource'` by construction. So a pass that guards staff is
// simply never offered a worker unless runOutsourceTick projects one into it,
// the way it already does for the context thresholds.

// ① THE LADDER. 下線 → 加速 → 強制, and 「後者一旦發出我們就不該發出前者」.
// openOwnerOpHandover hand-wrote the four epoch fields instead of going through
// armRefocusEpoch, so it carried no ladder check at all: a 換 model landing on a
// worker already in 加速停止 pushed the stage back to 停止 AND took the deadline
// with it — an agent that was counting down silently stopped counting.
func TestWorkerWindDownLadder_AModelChangeMayNotUndoAnAcceleratedStop(t *testing.T) {
	api := newTasksTestServer(t)
	api.noOutsource = true
	workerID := newActiveOnlineWorker(t, api)

	// 停止 first (the ladder's first rung), then the owner presses 加速停止.
	if rec := postWorker(t, api, workerID, "refocus", nil,
		api.HandleRefocusOutsourceWorkerApiOutsourceWorkersIdRefocusPost); rec.Code != http.StatusOK {
		t.Fatalf("refocus: %d %s", rec.Code, rec.Body.String())
	}
	if rec := postWorker(t, api, workerID, "accelerated-stop", nil,
		api.HandleAcceleratedStopOutsourceWorkerApiOutsourceWorkersIdAcceleratedStopPost); rec.Code != http.StatusOK {
		t.Fatalf("accelerated-stop: %d %s", rec.Code, rec.Body.String())
	}
	api.hub.DrainWardenCommands(ServerSelfHost)
	before, _ := api.dal.GetOutsourceWorker(workerID)
	if before.RefocusOp != refocusOpAcceleratedStop {
		t.Fatalf("setup: refocus_op=%q, want %q — the arm under test is not reached",
			before.RefocusOp, refocusOpAcceleratedStop)
	}
	cfg := api.reconcileConfigLive()
	deadlineBefore := refocusDeadlineOf(before.RefocusSince, cfg, before.RefocusOp)
	if deadlineBefore <= 0 {
		t.Fatalf("setup: 加速停止 must carry a deadline, got %v", deadlineBefore)
	}

	// Now the owner changes the model. On the staff side armRefocusEpoch refuses
	// this outright (winddownStageMayAdvanceTo).
	if rec := postWorker(t, api, workerID, "model",
		map[string]any{"model": "claude-opus-4-8"},
		api.HandleSetOutsourceWorkerModelApiOutsourceWorkersIdModelPost); rec.Code != http.StatusOK {
		t.Fatalf("model: %d %s", rec.Code, rec.Body.String())
	}
	after, _ := api.dal.GetOutsourceWorker(workerID)

	if after.RefocusOp != refocusOpAcceleratedStop {
		t.Fatalf("the ladder ran BACKWARDS: refocus_op %q → %q. 換 model is 停止 and "+
			"may not hand a worker already in 加速停止 back the slower procedure",
			before.RefocusOp, after.RefocusOp)
	}
	if got := refocusDeadlineOf(after.RefocusSince, cfg, after.RefocusOp); got != deadlineBefore {
		t.Fatalf("the 加速停止 deadline MOVED (%v → %v) on a 換 model — the worker was "+
			"counting down to a time that no longer exists", deadlineBefore, got)
	}
	// The owner's change still lands on the row; only the stage refuses to move.
	if after.Model != "claude-opus-4-8" {
		t.Fatalf("model=%q — refusing the ladder move must not drop the owner's "+
			"change", after.Model)
	}

	// POSITIVE CONTROL: an EQUAL-or-higher rung still re-arms. A 換 model onto a
	// worker whose open epoch is a plain 停止 must still re-stamp, or this guard
	// has simply frozen every wind-down instead of ordering them.
	t.Run("equal rung still re-arms", func(t *testing.T) {
		api := newTasksTestServer(t)
		api.noOutsource = true
		id := newActiveOnlineWorker(t, api)
		if rec := postWorker(t, api, id, "refocus", nil,
			api.HandleRefocusOutsourceWorkerApiOutsourceWorkersIdRefocusPost); rec.Code != http.StatusOK {
			t.Fatalf("refocus: %d %s", rec.Code, rec.Body.String())
		}
		api.hub.DrainWardenCommands(ServerSelfHost)
		if rec := postWorker(t, api, id, "model",
			map[string]any{"model": "claude-opus-4-8"},
			api.HandleSetOutsourceWorkerModelApiOutsourceWorkersIdModelPost); rec.Code != http.StatusOK {
			t.Fatalf("model: %d %s", rec.Code, rec.Body.String())
		}
		w, _ := api.dal.GetOutsourceWorker(id)
		if w.RefocusOp != ownerOpModel {
			t.Fatalf("refocus_op=%q, want %q — a same-rung verb must still open its "+
				"own epoch", w.RefocusOp, ownerOpModel)
		}
	})
}
