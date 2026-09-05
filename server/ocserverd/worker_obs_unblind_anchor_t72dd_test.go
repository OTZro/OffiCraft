package main

import "testing"

// T-72dd ANCHOR — the same measurement as the decision table, but driven
// through the PRODUCTION entry point (reconcileWorkerLiveness), so the table's
// "FULL" column cannot be an academic claim about a function nobody calls.
//
// The state: an ONLINE worker carrying a live refocus epoch whose agent has
// already filed its dump-done (stopped_since > 0). On the staff side that is
// decideUp's RECYCLE arm and it robust-STOPs the session so the next tick
// respawns. With the blind observation the FSM never sees any of it and decides
// "online: converged" — nothing is dispatched, and the handover never lands.
func TestReconcileWorkerLiveness_RecycleReachesTheSharedCollector_T72dd(t *testing.T) {
	s := newWorkerTestServer(t)
	connectWarden(t, s, ServerSelfHost)
	now := 1_000_000.0
	w := fsmWorkerFixture(t, s, "ow-rc", WorkerStatusAssigned, now-5_000)
	w.DesiredState = DesiredStateOnline
	w.RefocusSince = now - 5_000
	w.RefocusOp = refocusOpRefocus // SOFT / no clock — collected by the dump, not a timer
	w.StoppedSince = now - 10      // the agent said it is done
	putWorkerFixture(t, s, w)

	if _, err := s.hub.Connect("ow-rc", ""); err != nil {
		t.Fatalf("connect worker SSE: %v", err)
	}
	s.outsourceMu.Lock()
	s.workerSpawnTarget["ow-rc"] = ServerSelfHost
	s.reconcileWorkerLiveness(w, now)
	benched := s.workerMachineCoolingOn("ow-rc", ServerSelfHost, now)
	s.outsourceMu.Unlock()

	frames := s.hub.DrainWardenCommands(ServerSelfHost)
	t.Logf("MEASURED: frames=%d benched=%v", len(frames), benched)
	for i, f := range frames {
		rpc, args := decodeWardenFrame(t, f.Frame)
		t.Logf("  frame[%d]: %s %v", i, rpc, args)
	}
	if got := countStops(t, frames); got != 1 {
		t.Fatalf("the agent filed its dump-done on a refocus epoch — the shared "+
			"收口 must robust-stop it exactly once so the respawn can land; "+
			"got %d stop(s) in %d frame(s)", got, len(frames))
	}
	// The hazard this test measured in step 2 (the recycle STOP benching its
	// machine, stranding the worker it just collected) is FIXED in step 3 and
	// pinned in both directions by
	// TestWorkerStopArm_OnlyZombieTakeoverBenchesTheMachine_T72dd. Kept as an
	// assertion here too because this is the end-to-end path.
	if benched {
		t.Fatal("a recycle collect must not bench its machine")
	}
}
