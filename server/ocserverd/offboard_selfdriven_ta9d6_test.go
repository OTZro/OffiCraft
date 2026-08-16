package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// The sentence itself. The owner cut four differently-worded notices down to
// ONE (「不需要太多不同描述吧, 就請他按照步驟做好下線, 頂多告訴他剩下 120 秒」),
// so what tells the situations apart is the FIELDS, not the tone — and each of
// the two clauses below is load-bearing in a way that is invisible from the
// code:
//
//   - "then call restart_self yourself" blocks BOTH failure directions at once.
//     Without the second half an agent idles until the server cuts it off (dead
//     time the owner explicitly does not want); without the first, it stops
//     mid-work — a predecessor read the old wording as "you are done" and
//     announced its own end of life at 40%.
//   - "You have 120 seconds left." is the ONLY difference between a notice that
//     means "there is room" and one that means "you are out of time".
//
// 🔴 Both were measured to be UNGUARDED before this test existed: deleting
// either clause left the entire ocserverd suite green (228s and 186s, whole
// suite, no cache). A sentence nothing asserts is a sentence the next edit
// silently rewrites.
func TestOffboardNotice_TheApprovedSentence(t *testing.T) {
	const where = "context 62% (your limits: 60% / 75%)"
	doc := "1. 報開始收尾\n2. 給自己留交接"

	soft := offboardNotice(where, false, doc)
	if !strings.Contains(soft, where+" — offboard now: work the sequence below, "+
		"then call restart_self yourself.") {
		t.Fatalf("the soft notice must carry the approved sentence verbatim:\n%s", soft)
	}
	if strings.Contains(soft, "120 seconds") {
		t.Fatalf("the soft notice must NOT start a countdown — that is the whole "+
			"difference between the two:\n%s", soft)
	}
	if !strings.Contains(soft, doc) {
		t.Fatalf("the steps must be the DOCUMENT's, carried verbatim:\n%s", soft)
	}

	final := offboardNotice(where, true, doc)
	if !strings.Contains(final, "then call restart_self yourself. You have 120 seconds left.") {
		t.Fatalf("the final call must say how long is left, right after the same "+
			"sentence:\n%s", final)
	}

	// An empty document degrades to the sentence alone: losing the checklist is
	// survivable, losing the notice is not.
	bare := offboardNotice(where, false, "")
	if !strings.Contains(bare, "offboard now") || strings.Contains(bare, "\n") {
		t.Fatalf("an empty document must leave the sentence intact and alone:\n%q", bare)
	}
}

// Who gets which sentence. This is the judgement the whole ticket turns on —
// soft or final, and when one becomes the other — and BOTH directions of it
// survived the entire server suite before this test existed (independent
// review: forcing every answer to final, then every answer to soft, each left
// `ok ocserverd ~200s`). A judgement nothing asserts is a judgement the next
// edit is free to invert.
func TestOffboardKindOf_WhoGetsWhichSentence(t *testing.T) {
	const t0 = 1_000_000.0
	soft, final := offboardKindSoft, offboardKindFinal

	cases := []struct {
		name   string
		member Member
		now    float64
		want   string
		// carries=false means the member is not being wound down at all.
		carries bool
	}{
		{"下線 just pressed", Member{DesiredState: DesiredStateOffline, StoppingSince: t0}, t0 + 1, soft, true},
		// 🔴 …and it is STILL soft long after any window, because nothing
		// collects it on a clock. A final call here would promise 120 seconds
		// nobody keeps.
		{"下線 an hour later", Member{DesiredState: DesiredStateOffline, StoppingSince: t0}, t0 + 3600, soft, true},
		{"desired offline with no anchor", Member{DesiredState: DesiredStateOffline}, t0, "", false},

		{"重新聚焦 inside its window", Member{DesiredState: DesiredStateOnline, RefocusSince: t0, RefocusOp: refocusOpRefocus}, t0 + SoftOffboardGraceSecs - 1, soft, true},
		// …this one DOES escalate: the recycle clock really is running once the
		// window lapses, so the sentence that says so is true.
		{"重新聚焦 past its window", Member{DesiredState: DesiredStateOnline, RefocusSince: t0, RefocusOp: refocusOpRefocus}, t0 + SoftOffboardGraceSecs, final, true},

		{"context pressure", Member{DesiredState: DesiredStateOnline, RefocusSince: t0, RefocusOp: refocusOpContextHigh}, t0 + 1, final, true},
		{"改機器", Member{DesiredState: DesiredStateOnline, RefocusSince: t0, RefocusOp: memberOpRelocate}, t0 + 1, final, true},
		{"the agent's own restart_self", Member{DesiredState: DesiredStateOnline, RefocusSince: t0, RefocusOp: refocusOpRestartSelf}, t0 + 1, final, true},

		{"online and untouched", Member{DesiredState: DesiredStateOnline}, t0, "", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			kind, carries := offboardKindOf(c.member, c.now)
			if carries != c.carries || kind != c.want {
				t.Fatalf("got (%q, %v), want (%q, %v)", kind, carries, c.want, c.carries)
			}
		})
	}
}

// …and the classification has to reach the wire, because the sentence is
// composed from it. 下線 must never carry the countdown clause.
func TestOffboardDeltaPayload_下線NeverCarriesACountdown(t *testing.T) {
	s := newReconcileTestServer(t)
	m := testAgent("m-quiet")
	m.DesiredState = DesiredStateOffline
	m.StoppingSince = nowSecs() - 10*SoftOffboardGraceSecs // long past any window
	putTestMember(t, s, m)

	payload := s.offboardDeltaPayload(m)
	notice, ok := payload["offboard_notice"].(string)
	if !ok || notice == "" {
		t.Fatalf("下線 must carry the offboard notice: %+v", payload)
	}
	if strings.Contains(notice, "120 seconds") {
		t.Fatalf("nothing collects 下線 on a clock, so nothing may claim one:\n%s", notice)
	}
	if !strings.Contains(notice, "then call restart_self yourself") {
		t.Fatalf("the approved sentence must survive:\n%s", notice)
	}
}

// The sequence the notice tells the agent to work must actually be workable.
// Step 1 is report_stopping, and the notice ends 「then call restart_self
// yourself」 — so an agent that has declared its close-out must still be able
// to make that call. It could not: report_stopping makes PresenceState project
// `stopping`, the endpoint gated on `online`, and once close-out anchors
// stopped being swept every tick the refusal lasted the whole soft window.
func TestRestartSelf_WorksWhileTheAgentIsClosingOut(t *testing.T) {
	s := newReconcileTestServer(t)
	putWarden(t, s, "mach-a")
	m := testAgent("m-closing")
	m.DesiredMachineID = "mach-a"
	putTestMember(t, s, m)
	connectOnline(t, s, "mach-a")
	connectOnlineMachine(t, s, "m-closing", "mach-a")
	// A mature session: past the respawn-storm floor, which is a separate guard.
	s.gauge.Set("m-closing", map[string]any{"boot_ts": nowSecs() - 10*minSelfRestartSecs})

	rec := httptest.NewRecorder()
	s.HandleReportStoppingApiSelfStoppingPost(rec,
		taskReq(t, "POST", "/api/self/stopping", map[string]any{}, "m-closing", "agent"))
	if rec.Code != http.StatusOK {
		t.Fatalf("report_stopping: %d %s", rec.Code, rec.Body.String())
	}

	rec = httptest.NewRecorder()
	s.HandleRestartSelfApiSelfRefocusPost(rec,
		taskReq(t, "POST", "/api/self/refocus", map[string]any{}, "m-closing", "agent"))
	if rec.Code != http.StatusOK {
		t.Fatalf("an agent doing exactly what the notice told it must not be "+
			"refused: %d %s", rec.Code, rec.Body.String())
	}
	after, _ := s.dal.GetMember("m-closing")
	if after.RefocusSince <= 0 {
		t.Fatalf("the self-restart must open a handover epoch: %+v", after)
	}

	// The owner's 重新聚焦 has the same gate and the same reason to survive it.
	rec = httptest.NewRecorder()
	s.HandleRefocusMemberApiMembersMemberIdRefocusPost(rec,
		taskReq(t, "POST", "/api/members/m-closing/refocus", map[string]any{},
			wireOwnerID, "owner"), "m-closing")
	if rec.Code != http.StatusOK {
		t.Fatalf("重新聚焦 must still reach an agent that is mid-hand-off: %d %s",
			rec.Code, rec.Body.String())
	}
}

// The pair the owner sets is 「第一次通知 / 最後通牒」, and a pair whose first
// number is not below its second is not a pair — the notice would fire at or
// after the handover it is supposed to precede, i.e. never. The handler refuses
// it rather than silently reordering, because a silently corrected setting is
// one the owner cannot see he got wrong.
//
// 🔴 The claude half of that refusal was UNGUARDED: disabling it left the whole
// ocserverd suite green (240s, no cache). Measured, not assumed — the codex half
// below is asserted for the same reason, since the two are separate checks and a
// test for one says nothing about the other.
func TestSettingsPair_NoticeMustBeStrictlyBelowTheFinalCall(t *testing.T) {
	patch := func(t *testing.T, api *apiServer, body map[string]any) *httptest.ResponseRecorder {
		t.Helper()
		rec := httptest.NewRecorder()
		api.HandleUpdateSettingsApiSettingsPatch(rec,
			taskReq(t, http.MethodPatch, "/api/settings", body, "owner", "owner"))
		return rec
	}

	t.Run("claude: equal is refused, and so is inverted", func(t *testing.T) {
		s := newReconcileTestServer(t)
		for _, body := range []map[string]any{
			{"notice_pct": 65, "handover_pct": 65},
			{"notice_pct": 75, "handover_pct": 65},
		} {
			rec := patch(t, s, body)
			if rec.Code != http.StatusUnprocessableEntity {
				t.Fatalf("%v must be refused, got %d %s", body, rec.Code, rec.Body.String())
			}
			if !strings.Contains(rec.Body.String(), "notice_pct") {
				t.Fatalf("the refusal must name the field: %s", rec.Body.String())
			}
		}
		// …and a real pair still lands, so the check is a guard and not a wall.
		if rec := patch(t, s, map[string]any{"notice_pct": 60, "handover_pct": 75}); rec.Code != http.StatusOK {
			t.Fatalf("a valid pair must be accepted: %d %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("codex: rounds obey the same rule", func(t *testing.T) {
		s := newReconcileTestServer(t)
		rec := patch(t, s, map[string]any{"codex_notice_round": 6, "codex_compaction_threshold": 6})
		if rec.Code != http.StatusUnprocessableEntity {
			t.Fatalf("equal rounds must be refused, got %d %s", rec.Code, rec.Body.String())
		}
		if rec := patch(t, s, map[string]any{"codex_notice_round": 5, "codex_compaction_threshold": 6}); rec.Code != http.StatusOK {
			t.Fatalf("a valid round pair must be accepted: %d %s", rec.Code, rec.Body.String())
		}
	})
}

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
