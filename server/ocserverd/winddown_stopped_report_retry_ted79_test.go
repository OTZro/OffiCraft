package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// winddown_stopped_report_retry_ted79_test.go — the OTHER half of the window
// winddown_stopped_report_ted79_test.go describes.
//
// That file pins that the server never THROWS AWAY an agent's own 「我收完了」.
// This one pins that somebody eventually comes to COLLECT it.
//
// THE HARM. report_stopped latches stopped_since and fires ONE
// dispatchRobustStopNow. That dispatch is raw and best-effort: enqueueToWarden
// is fail-closed on an unreachable warden, so if the machine's warden holds no
// live SSE downstream at that instant the frame is simply dropped. The member is
// then parked at
//
//	desired online ∧ SSE online ∧ stopped_since > 0 ∧ refocus_since == 0
//
// and NOTHING re-dispatches: decideUp's recycle arm is gated on
// refocus_since > 0, so this member takes the converged path; decideDown's soft
// arm returns decisionNone for the whole window. The agent believes it is done,
// the server has recorded that it is done, and no arm of the reconcile will ever
// kill and respawn it. It sits there forever.
//
// 🔴 WHY THE FIX IS NOT "let stopped_since open the recycle arm". That would be
// the mirror image of the harm the first commit on this branch removed: a member
// carrying a PREDECESSOR's latch (下線 → 活化 leaves stopped_since set with no
// epoch — see TestWindDownKind_APredecessorsLatchDoesNotSilenceTheThresholds)
// would be robust-stopped on its first tick with no close-out at all. The retry
// therefore keys on the DISPATCH EVENT, not on the member's latch: only a robust
// STOP that was actually dispatched and never landed is re-sent.

// parkedOnALostRobustStop drives the real report_stopped handler against a
// member whose warden is NOT reachable, so the robust STOP is genuinely dropped
// by the fail-closed enqueue gate rather than by a hand-set fixture field.
func parkedOnALostRobustStop(t *testing.T, s *apiServer, id, machine string) Member {
	t.Helper()
	putWarden(t, s, machine) // active warden member, but NO live SSE downstream
	m := testAgent(id)
	m.DesiredMachineID = machine
	putTestMember(t, s, m)
	connectOnline(t, s, id)

	rec := httptest.NewRecorder()
	s.HandleReportStoppedApiSelfStoppedPost(rec,
		taskReq(t, "POST", "/api/self/stopped", map[string]any{}, id, "agent"))
	if rec.Code != http.StatusOK {
		t.Fatalf("report_stopped: %d %s", rec.Code, rec.Body.String())
	}
	if frames := drainFrames(t, s, machine); len(frames) != 0 {
		t.Fatalf("fixture: the robust STOP was NOT lost (%+v) — this test needs the "+
			"unreachable-warden drop to have actually happened", frames)
	}
	got, err := s.dal.GetMember(id)
	if err != nil || got == nil {
		t.Fatalf("refetch %s: %v", id, err)
	}
	if got.StoppedSince <= 0 {
		t.Fatalf("fixture: report_stopped did not latch stopped_since (%+v)", got)
	}
	if got.RefocusSince != 0 {
		t.Fatalf("fixture: an epoch was opened (%v) — the window here is the "+
			"EPOCH-LESS one, which is exactly the one no arm collects", got.RefocusSince)
	}
	if !s.hub.IsOnline(id) {
		t.Fatal("fixture: the agent must still hold its SSE (the STOP never landed)")
	}
	return *got
}

func TestRobustStop_ALostStoppedReportCollectIsReDispatched(t *testing.T) {
	s := newReconcileTestServer(t)
	cfg := s.reconcileConfigLive()
	now := nowSecs()

	parked := parkedOnALostRobustStop(t, s, "m-ed79-lost-stop", "mach-ed79-a")

	// The warden comes back. Nothing else about the member changes: it is still
	// online, still latched, still epoch-less.
	connectOnline(t, s, "mach-ed79-a")

	// One tick INSIDE stop_retry must not re-spam.
	s.runReconcileTick(now + 1)
	if frames := drainFrames(t, s, "mach-ed79-a"); len(frames) != 0 {
		t.Fatalf("inside stop_retry the collect must not be re-dispatched: %+v", frames)
	}

	// …and one tick PAST it must re-send the robust STOP.
	s.runReconcileTick(now + cfg.StopRetry + 1)
	frames := drainFrames(t, s, "mach-ed79-a")
	sawStop := false
	for _, f := range frames {
		if f.RPC == reconcileCmdStop {
			sawStop = true
		}
	}
	if !sawStop {
		t.Fatalf("the robust STOP that collects %s was dispatched ONCE, lost on an "+
			"unreachable warden, and never re-sent (frames past stop_retry: %+v). "+
			"The agent reported it had finished, the server recorded that it had "+
			"finished, and nothing will ever kill and respawn it — it is parked "+
			"alive on a session it already closed out. stopped_since=%v",
			parked.ID, frames, parked.StoppedSince)
	}
}
