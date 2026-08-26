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

// ② TOKEN EXPIRY. A worker's session token is minted by the SAME mintAgentToken
// with the SAME agent_token_ttl as a staff member's (worker_spawn.go), so it
// dies exactly the same way — and every step of the close-out is an MCP call on
// that token. tokenExpiryOf refused to answer for anything that was not
// KindAssistant, which silently swept outsource in with warden.
func TestTokenExpiry_AnOutsourceSessionIsDerivableToo(t *testing.T) {
	s := newReconcileTestServer(t)
	ttl := s.agentTokenTTLValue()
	const mintedAt = 1_769_904_000.0

	worker := testAgent("ow-170e-derive")
	worker.Kind = KindOutsource
	worker.SessionBootTS = mintedAt
	if got := tokenExpiryOf(worker, ttl); got != mintedAt+float64(ttl) {
		t.Fatalf("tokenExpiryOf(outsource)=%v, want %v — an outsource worker's token "+
			"is minted by mintAgentToken with the same TTL a staff token is, so it "+
			"expires and its session must be given the same lead", got, mintedAt+float64(ttl))
	}

	// The one kind that really is exempt stays exempt: a warden's credential is
	// minted by mintWardenToken with NO exp claim at all.
	warden := testAgent("mach-170e")
	warden.Kind = KindWarden
	warden.SessionBootTS = mintedAt
	if got := tokenExpiryOf(warden, ttl); got != 0 {
		t.Fatalf("a warden got a derived token expiry of %v — its credential has no "+
			"exp claim at all", got)
	}
}

// …and the pass has to be WIRED on the worker side. The worker roster never
// passes through runReconcileTick (ListMembers excludes it), so a correct
// tokenExpiryOf with no projection in runOutsourceTick is indistinguishable
// from the bug.
func TestTokenExpiry_TheOutsourceCadenceActuallyRunsIt(t *testing.T) {
	api := newTasksTestServer(t)
	api.noOutsource = true
	workerID := newActiveOnlineWorker(t, api)
	now := nowSecs()

	w, _ := api.dal.GetOutsourceWorker(workerID)
	// Anchored so the derived expiry is one second inside the lead.
	w.SessionBootTS = now + tokenExpiryLeadSecs - 1 - float64(api.agentTokenTTLValue())
	if err := api.dal.PutOutsourceWorker(*w); err != nil {
		t.Fatalf("put worker: %v", err)
	}

	api.runOutsourceTick(now)

	got, _ := api.dal.GetOutsourceWorker(workerID)
	if got.RefocusOp != refocusOpTokenExpiry || got.RefocusSince != now {
		t.Fatalf("after the outsource cadence: refocus_op=%q refocus_since=%v, want "+
			"%q at %v — a worker whose token is about to die must be asked to close "+
			"out while the calls that close it out still work",
			got.RefocusOp, got.RefocusSince, refocusOpTokenExpiry, now)
	}
}

// ③ THE PHANTOM 停止中. clearStaleStoppingOnOnline is the survived-stop
// auto-clear: a desired-online member observed ONLINE while still carrying
// stopping_since is provably past that stop. A worker had no equivalent and no
// projection into this one, so the anchor sat on the row forever and the
// cockpit read 停止中 for a worker that was plainly working.
func TestStaleStopping_AnOnlineWorkerIsSweptToo(t *testing.T) {
	api := newTasksTestServer(t)
	api.noOutsource = true
	workerID := newActiveOnlineWorker(t, api)
	now := nowSecs()

	w, _ := api.dal.GetOutsourceWorker(workerID)
	// Quiet for far longer than the close-out window: no gauge record at all, and
	// the anchor itself is ancient.
	w.StoppingSince = now - 10*SoftOffboardGraceSecs
	if err := api.dal.PutOutsourceWorker(*w); err != nil {
		t.Fatalf("put worker: %v", err)
	}

	api.runOutsourceTick(now)

	got, _ := api.dal.GetOutsourceWorker(workerID)
	if got.StoppingSince != 0.0 {
		t.Fatalf("stopping_since=%v on a worker that is desired-online AND observed "+
			"online AND silent for 10x the close-out window — the cockpit shows 停止中 "+
			"forever for a worker that survived its stop", got.StoppingSince)
	}

	// POSITIVE CONTROL: a close-out that is genuinely in flight must NOT be
	// swept. Same shape as staff — the anchor is fresh, so the owner keeps his
	// only signal that the session has begun closing out.
	t.Run("a fresh close-out is left alone", func(t *testing.T) {
		api := newTasksTestServer(t)
		api.noOutsource = true
		id := newActiveOnlineWorker(t, api)
		now := nowSecs()
		w, _ := api.dal.GetOutsourceWorker(id)
		w.StoppingSince = now - 1
		if err := api.dal.PutOutsourceWorker(*w); err != nil {
			t.Fatalf("put worker: %v", err)
		}
		api.runOutsourceTick(now)
		got, _ := api.dal.GetOutsourceWorker(id)
		if got.StoppingSince == 0.0 {
			t.Fatal("a close-out that started one second ago was swept — that erases " +
				"the owner's only signal that the session is winding down")
		}
	})
}
