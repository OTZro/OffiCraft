package main

// T-ed79 #6 — the outsource twin of the member side's at-least-once robust STOP.
//
// The member producer arms RobustStopPendingAt on every out-of-band robust STOP
// and the cadence re-pushes it while the member is STILL ONLINE past stop_retry
// (reconcile.go, dispatchRobustStopNow + the arm at the top of reconcileDecide).
// The worker producer had only half of that: a kill the fail-closed gate REFUSED
// was parked and re-fired, but a kill the warden ACCEPTED was forgotten the
// instant the frame hit the FIFO — and a frame on a FIFO is not proof the
// session died (worker_spawn.go's own note: an empty backlog means "collected",
// not "delivered"). These tests pin the added half, and both directions of it.

import (
	"testing"
)

// connectWorkerOn puts a worker id online carrying a machine claim — the fact
// the retry reads to tell "the session we killed is still there" apart from "a
// respawn put this worker back somewhere else".
func connectWorkerOn(t *testing.T, s *apiServer, workerID, machineID string) *hubListener {
	t.Helper()
	l, err := s.hub.Connect(workerID, machineID)
	if err != nil {
		t.Fatalf("hub connect worker %s@%s: %v", workerID, machineID, err)
	}
	t.Cleanup(func() { s.hub.Disconnect(l) })
	return l
}

// stoppedLiveWorker is the shared fixture: a worker the owner has stopped, whose
// session is STILL ON the machine the kill was addressed to. The two worlds this
// file separates differ in exactly one input — whether that session is still
// there — so the fixture takes it as a parameter rather than baking it in.
func stoppedLiveWorker(t *testing.T, s *apiServer, id string, sessionOn string) OutsourceWorker {
	t.Helper()
	putWardenFixture(t, s, ServerSelfHost)
	connectWarden(t, s, ServerSelfHost)
	w := putWorkerFixture(t, s, OutsourceWorker{
		ID: id, Codename: "O-" + id, Model: "opus", Effort: "high",
		TaskID: "t-stop", Status: WorkerStatusAssigned,
		DesiredState: DesiredStateOffline, // owner-explicit 停止
	})
	if sessionOn != "" {
		connectWorkerOn(t, s, id, sessionOn)
	}
	s.outsourceMu.Lock()
	s.workerSpawnTarget[id] = ServerSelfHost
	s.outsourceMu.Unlock()
	return w
}

// 🔴 THE MUTANT GUARD. The kill went out, the warden took the frame, and the
// worker is STILL ONLINE on that same machine a full stop_retry later ⇒ the one
// thing that can be true is that the kill never took effect. The member side
// pushes a second STOP here; the worker side must too.
func TestStopWorkerNow_KillThatNeverTookIsReDispatched(t *testing.T) {
	s := newWorkerTestServer(t)
	w := stoppedLiveWorker(t, s, "ow-live", ServerSelfHost)

	base := nowSecs()
	s.outsourceMu.Lock()
	s.stopWorkerNow(w)
	s.outsourceMu.Unlock()
	if got := s.hub.DrainWardenCommands(ServerSelfHost); len(got) != 1 {
		t.Fatalf("SENTINEL: the first kill must reach the warden's FIFO, got %d frames", len(got))
	}

	// Inside the window nothing is owed — a warden is allowed its kill ladder.
	s.runOutsourceTick(base + 1)
	if got := s.hub.DrainWardenCommands(ServerSelfHost); len(got) != 0 {
		t.Fatalf("a re-push INSIDE stop_retry is frame spam, got %d frames", len(got))
	}

	s.runOutsourceTick(base + defaultReconcileConfig().StopRetry + 1)
	frames := s.hub.DrainWardenCommands(ServerSelfHost)
	if len(frames) != 1 {
		t.Fatalf("the worker is still online on %q a full stop_retry after the kill was "+
			"handed over — that kill did not take, and nothing re-pushed it: got %d frames, "+
			"want 1. A frame accepted onto a FIFO is not a dead session; the member side "+
			"re-dispatches here and the worker side must not be the one place a lost STOP "+
			"leaves a 殘活 session forever", ServerSelfHost, len(frames))
	}
	if rpc, args := decodeWardenFrame(t, frames[0].Frame); rpc != reconcileCmdStop ||
		args["member_id"] != "ow-live" {
		t.Errorf("the re-push must be the same worker STOP: rpc = %q args = %v", rpc, args)
	}
}

// 🔴 THE OTHER DIRECTION. Same fixture, one input flipped: the session is GONE.
// The kill took, so there is nothing to re-push — and a retry that cannot tell
// these two worlds apart would re-fire at a dead machine every stop_retry for as
// long as the row exists.
func TestStopWorkerNow_KillThatTookIsNotReDispatched(t *testing.T) {
	s := newWorkerTestServer(t)
	w := stoppedLiveWorker(t, s, "ow-gone", "") // no session on the box

	base := nowSecs()
	s.outsourceMu.Lock()
	s.stopWorkerNow(w)
	s.outsourceMu.Unlock()
	if got := s.hub.DrainWardenCommands(ServerSelfHost); len(got) != 1 {
		t.Fatalf("SENTINEL: the first kill must reach the warden's FIFO, got %d frames", len(got))
	}

	for _, at := range []float64{
		base + defaultReconcileConfig().StopRetry + 1,
		base + 4*defaultReconcileConfig().StopRetry,
	} {
		s.runOutsourceTick(at)
		if got := s.hub.DrainWardenCommands(ServerSelfHost); len(got) != 0 {
			t.Fatalf("the worker went offline — the kill took, and a STOP re-pushed at "+
				"t=%v anyway is a row that never stops flinging frames, got %d", at, len(got))
		}
	}
}

// 🔴 THE HAZARD THE RETRY MUST NOT CREATE. A restart in place is stop-then-start
// on the SAME machine: the worker id is online there again a minute later, and a
// retry reading raw presence would shoot the FRESH session. The parked-kill path
// already drops itself on a START toward its own target (notifyWorkerSpawn); the
// landed-kill retry has to obey the same rule.
func TestRestartInPlace_DoesNotReDispatchTheStopAtTheFreshSession(t *testing.T) {
	s := newWorkerTestServer(t)
	putWardenFixture(t, s, "m-inplace")
	connectWarden(t, s, "m-inplace")
	w := blockedSpawnFixture(t, s, "t-0000000000f6", "ow-again", "m-inplace")
	connectWorkerOn(t, s, "ow-again", "m-inplace")

	base := nowSecs()
	s.outsourceMu.Lock()
	s.workerSpawnTarget[w.ID] = "m-inplace"
	s.stopWorkerSessionOrPark("m-inplace", w.ID, base)
	dispatched := s.notifyWorkerSpawn(readWorker(t, s, w.ID), base)
	s.outsourceMu.Unlock()
	if !dispatched {
		t.Fatal("SENTINEL: the fixture must reach a real re-START dispatch")
	}
	if got := s.hub.DrainWardenCommands("m-inplace"); len(got) != 2 {
		t.Fatalf("SENTINEL: want the kill + the re-start on the FIFO, got %d frames", len(got))
	}

	s.runOutsourceTick(base + defaultReconcileConfig().StopRetry + 1)
	if got := s.hub.DrainWardenCommands("m-inplace"); len(got) != 0 {
		t.Fatalf("the re-START landed on the very machine the kill was aimed at, so the "+
			"session online there now is the NEW one — re-pushing the old kill kills the "+
			"restart the owner just asked for, got %d frames", len(got))
	}
}
