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
	if len(frames) != 1 {
		t.Fatalf("the agent filed its dump-done on a refocus epoch — the shared "+
			"收口 must robust-stop it so the respawn can land; got %d frames", len(frames))
	}
	rpc, args := decodeWardenFrame(t, frames[0].Frame)
	if rpc != reconcileCmdStop || args["member_id"] != "ow-rc" {
		t.Fatalf("frame = %s %v, want stop ow-rc", rpc, args)
	}
	// 🔴 RECORDED, NOT ASSERTED AS DESIRABLE: reconcileWorkerLiveness's STOP arm
	// is written for the ZOMBIE TAKEOVER and benches the target machine. A
	// recycle STOP is not a ghost reap and must not bench anything — the respawn
	// is supposed to land on the SAME machine. Owner ruling required (T-72dd
	// step 3); this line measures it so the follow-up cannot lose it.
	t.Logf("HAZARD (measured, not fixed here): the recycle STOP went through the "+
		"zombie-takeover arm — machine benched=%v", benched)
}
