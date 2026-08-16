package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// The self-driven offboard: an agent that was told to close out and stop
// itself, does so, and reaches the end of the sequence.
//
// This path had no receiver. Collection was armed only by a refocus epoch —
// something ELSE deciding to take the session — which held while the offboard
// sequence was shown only to a session already being collected. Once the notice
// began telling agents to close out on their own (T-c382) and the sequence
// became a document any session could work (T-c9c0), an agent could finish its
// close-out, report stopped, and have nothing happen: it stayed alive holding a
// session it had already declared finished.
//
// owner 2026-08-16 (card rc-b08d49dc3b03, option ①): 「收掉並重生」.
func TestSelfDrivenOffboard_StoppedReportCollectsAndRespawns(t *testing.T) {
	s := newReconcileTestServer(t)
	putWarden(t, s, "mach-a")

	m := testAgent("m-self")
	m.DesiredMachineID = "mach-a"
	putTestMember(t, s, m)
	connectOnline(t, s, "mach-a")
	session := connectOnlineMachine(t, s, "m-self", "mach-a")

	// Nobody is collecting it: no refocus epoch, desired_state=online. This is
	// the whole point — the agent decided this by itself.
	rec := httptest.NewRecorder()
	s.HandleReportStoppingApiSelfStoppingPost(rec,
		taskReq(t, "POST", "/api/self/stopping", map[string]any{}, "m-self", "agent"))
	if rec.Code != http.StatusOK {
		t.Fatalf("report_stopping: %d %s", rec.Code, rec.Body.String())
	}
	if f := drainFrames(t, s, "mach-a"); len(f) != 0 {
		t.Fatalf("declaring the close-out must not kill anything: %+v", f)
	}

	// …and the cockpit must still show it, which is what the owner watched fail
	// (T-2123): the stale-stopping sweep used to erase a close-out in flight.
	s.runReconcileTick(nowSecs())
	after, _ := s.dal.GetMember("m-self")
	if after == nil || after.StoppingSince <= 0 {
		t.Fatalf("an in-flight close-out must keep its anchor: %+v", after)
	}

	rec = httptest.NewRecorder()
	s.HandleReportStoppedApiSelfStoppedPost(rec,
		taskReq(t, "POST", "/api/self/stopped", map[string]any{}, "m-self", "agent"))
	if rec.Code != http.StatusOK {
		t.Fatalf("report_stopped: %d %s", rec.Code, rec.Body.String())
	}
	stops := drainFrames(t, s, "mach-a")
	if len(stops) != 1 || stops[0].RPC != "stop" {
		t.Fatalf("a self-driven close-out must be collected on its stopped report: %+v", stops)
	}

	// …and a new generation takes its place, which is what the document has
	// been promising all along (「server 原地重生新的你」).
	s.hub.Disconnect(session)
	s.reconcileMemberNow("m-self")
	starts := drainFrames(t, s, "mach-a")
	if len(starts) != 1 || starts[0].RPC != "start" {
		t.Fatalf("the respawn must follow the collect: %+v", starts)
	}
}

// The same report on a member the owner has taken DOWN collects it just as
// promptly — and does NOT bring it back. desired_state is the only thing that
// decides which of the two happens.
func TestSelfDrivenOffboard_StoppedReportOnADesiredOfflineMemberDoesNotRespawn(t *testing.T) {
	s := newReconcileTestServer(t)
	putWarden(t, s, "mach-a")

	m := testAgent("m-down")
	m.DesiredMachineID = "mach-a"
	m.DesiredState = DesiredStateOffline
	m.StoppingSince = nowSecs()
	putTestMember(t, s, m)
	connectOnline(t, s, "mach-a")
	session := connectOnlineMachine(t, s, "m-down", "mach-a")

	rec := httptest.NewRecorder()
	s.HandleReportStoppedApiSelfStoppedPost(rec,
		taskReq(t, "POST", "/api/self/stopped", map[string]any{}, "m-down", "agent"))
	if rec.Code != http.StatusOK {
		t.Fatalf("report_stopped: %d %s", rec.Code, rec.Body.String())
	}
	stops := drainFrames(t, s, "mach-a")
	if len(stops) != 1 || stops[0].RPC != "stop" {
		t.Fatalf("a finished close-out must be collected immediately: %+v", stops)
	}

	s.hub.Disconnect(session)
	s.reconcileMemberNow("m-down")
	if f := drainFrames(t, s, "mach-a"); len(f) != 0 {
		t.Fatalf("a member the owner took down must stay down: %+v", f)
	}
}

// 強制下線 leaves a mark. It is the one offboard path that sends no notice, so
// what it kills leaves exactly what a session with nothing to say leaves —
// no hand-off, no fresh step note. This column is the difference, and the
// reader who needs it is the generation that comes after, so the next boot
// must NOT clear it.
func TestForceStop_RecordsThatTheSessionWasCutOff(t *testing.T) {
	s := newReconcileTestServer(t)
	putWarden(t, s, "mach-a")

	m := testAgent("m-cut")
	m.DesiredMachineID = "mach-a"
	putTestMember(t, s, m)
	connectOnline(t, s, "mach-a")
	connectOnlineMachine(t, s, "m-cut", "mach-a")

	before, _ := s.dal.GetMember("m-cut")
	if before.ForcedStopAt != 0 {
		t.Fatalf("a member that was never force-stopped must carry 0: %+v", before)
	}

	rec := httptest.NewRecorder()
	s.HandleForceStopMemberApiMembersMemberIdForceStopPost(rec,
		taskReq(t, "POST", "/api/members/m-cut/force-stop", map[string]any{},
			wireOwnerID, "owner"), "m-cut")
	if rec.Code != http.StatusOK {
		t.Fatalf("force-stop: %d %s", rec.Code, rec.Body.String())
	}
	cut, _ := s.dal.GetMember("m-cut")
	if cut.ForcedStopAt <= 0 {
		t.Fatalf("the force-stop must be recorded: %+v", cut)
	}

	// The next generation boots — and must still be able to see that its
	// predecessor was cut off rather than allowed to finish. report_waking
	// clears every OTHER lifecycle anchor on this row.
	rec = httptest.NewRecorder()
	s.HandleReportWakingApiSelfWakingPost(rec,
		taskReq(t, "POST", "/api/self/waking", map[string]any{}, "m-cut", "agent"))
	if rec.Code != http.StatusOK {
		t.Fatalf("report_waking: %d %s", rec.Code, rec.Body.String())
	}
	woke, _ := s.dal.GetMember("m-cut")
	if woke.ForcedStopAt != cut.ForcedStopAt {
		t.Fatalf("the next boot must not erase the record: %v → %v",
			cut.ForcedStopAt, woke.ForcedStopAt)
	}

	// 🔴 …and the assertion above passes for a reason that is NOT the one that
	// protects this column: report_waking rewrites a row it just read, so it
	// carries the right value either way. Both halves of that were measured —
	// zeroing it in the handler AND letting the upsert carry it each left the
	// check green. What actually protects it is PutMember declining to write
	// the column at all, and the shape that finds out is a STALE snapshot:
	// any writer holding a member value from before the force-stop. That is
	// how the avatar pointer and the session anchor lost data before they got
	// their own seams.
	stale := *before
	stale.Name = "renamed by a writer holding an old snapshot"
	if err := s.dal.PutMember(stale); err != nil {
		t.Fatalf("stale put: %v", err)
	}
	survived, _ := s.dal.GetMember("m-cut")
	if survived.ForcedStopAt != cut.ForcedStopAt {
		t.Fatalf("a stale snapshot must not erase the force-stop record: %v → %v",
			cut.ForcedStopAt, survived.ForcedStopAt)
	}
	if survived.Name != stale.Name {
		t.Fatalf("…while the rest of that write must land normally: %+v", survived)
	}
}
