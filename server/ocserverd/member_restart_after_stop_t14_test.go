package main

// member_restart_after_stop_t14_test.go — T-14 項目 7: 「要不要起來」 split out
// of desired_state.
//
// Owner 2026-08-30 (rc-bc1b029a3aa2, option [0]): 「一個重啟的 intention 遇上一個
// 更強硬的下線規則 他的方式是沿用強硬下線規則 但是附加上線規則」, and
// 「refocus -> force stop 跟 force stop -> refocus 是不一樣的」.
//
// The invariant these tests pin, in his words:
//
//	「要不要起來」聽最後一個動作的（後蓋前）
//	「下線用多強」只會往上加（棘輪），不聽順序
//	最後一個動作是重啟或上線 ⇒ 最終在線上；只有最後一個動作是下線 ⇒ 最終離線

import (
	"fmt"
	"net/http/httptest"
	"testing"
)

// ── the fixture: an owner pressing buttons at a member with a live session ────

type lifecycleAction struct {
	name string
	down bool // 下線類 — the class that ends with the member NOT coming back
	do   func(t *testing.T, s *apiServer, id string) *httptest.ResponseRecorder
}

var modelCounter int

// lifecycleActions is the closed set the sequence matrix is built from. Adding a
// seventh verb here is the whole point of the matrix: every length-2 and
// length-3 combination it takes part in is asserted the next time the suite runs.
//
// ⚠️ 加速停止 IS DELIBERATELY ABSENT, and its absence is not an oversight. On its
// 換手 arm (HandleAcceleratedStopMember…, `case m.RefocusSince > 0`) it re-stamps
// the handover and leaves desired_state ONLINE — it hurries a handover along
// rather than turning it into a stop — so 重新聚焦 → 加速停止 ends with the member
// running. Under the ⇔ oracle below that reads as a violation, and closing it
// means converting that arm into a stop: a behaviour change outside the [0]
// ruling this ticket implements. It is a 下線 rung for the purposes of
// 「要不要起來」 (it calls clearRestartIntent) and its RATCHET behaviour is pinned
// by TestRestartIntentDoesNotSoftenTheWinddownLadder below.
var lifecycleActions = []lifecycleAction{
	{name: "下線", down: true, do: func(t *testing.T, s *apiServer, id string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		s.HandleDeactivateMemberApiMembersMemberIdDeactivatePost(rec,
			taskReq(t, "POST", "/api/members/"+id+"/deactivate", nil, wireOwnerID, "owner"), id)
		return rec
	}},
	{name: "強制停止", down: true, do: func(t *testing.T, s *apiServer, id string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		s.HandleForceStopMemberApiMembersMemberIdForceStopPost(rec,
			taskReq(t, "POST", "/api/members/"+id+"/force-stop", nil, wireOwnerID, "owner"), id)
		return rec
	}},
	{name: "重新聚焦", do: func(t *testing.T, s *apiServer, id string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		s.HandleRefocusMemberApiMembersMemberIdRefocusPost(rec,
			taskReq(t, "POST", "/api/members/"+id+"/refocus", nil, wireOwnerID, "owner"), id)
		return rec
	}},
	{name: "改機器", do: func(t *testing.T, s *apiServer, id string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		s.HandleRelocateMemberApiMembersMemberIdRelocatePost(rec,
			taskReq(t, "POST", "/api/members/"+id+"/relocate",
				map[string]any{"machine_id": "mach-b"}, wireOwnerID, "owner"), id)
		return rec
	}},
	{name: "換model", do: func(t *testing.T, s *apiServer, id string) *httptest.ResponseRecorder {
		modelCounter++
		rec := httptest.NewRecorder()
		s.HandleUpdateMemberApiMembersMemberIdPatch(rec,
			taskReq(t, "PATCH", "/api/members/"+id,
				map[string]any{"model": fmt.Sprintf("model-%d", modelCounter)},
				wireOwnerID, "owner"), id)
		return rec
	}},
	{name: "活化", do: func(t *testing.T, s *apiServer, id string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		s.HandleActivateMemberApiMembersMemberIdActivatePost(rec,
			taskReq(t, "POST", "/api/members/"+id+"/activate",
				map[string]any{}, wireOwnerID, "owner"), id)
		return rec
	}},
}

// liveMember seeds one online, desired-online staff member on mach-a and returns
// the hub listener that makes it online, so the caller can end the session.
func liveMember(t *testing.T, s *apiServer, id string) *hubListener {
	t.Helper()
	m := testAgent(id)
	m.DesiredState = DesiredStateOnline
	m.DesiredMachineID = "mach-a"
	m.Model = "model-base"
	putTestMember(t, s, m)
	l, err := s.hub.Connect(id, "mach-a")
	if err != nil {
		t.Fatalf("connect %s: %v", id, err)
	}
	return l
}

// settle ends the session and lets the reconcile run to a fixed point — the
// instant the stop the owner asked for is actually finished, which is where a
// queued 重啟 is spent. Returns the member's desired_state afterwards.
func settle(t *testing.T, s *apiServer, id string, l *hubListener) Member {
	t.Helper()
	s.hub.Disconnect(l)
	for i := 0; i < 3; i++ {
		m, err := s.dal.GetMember(id)
		if err != nil || m == nil {
			t.Fatalf("get member %s: %v", id, err)
		}
		s.reconcileOne(*m, newReconcileState(), nowSecs()+float64(i))
	}
	m, _ := s.dal.GetMember(id)
	return *m
}

// ── the two cells the ticket exists for ──────────────────────────────────────

// 強制停止 → 重新聚焦 used to answer 409 and leave the member down forever. The
// owner's sentence for this exact case: 「我們強制下線以後已經不需要退回軟下線，
// 如果我已經到強硬下線的狀態下按下 refocus 我只需要在下線後把人帶起來」.
func TestRefocusAfterForceStopQueuesTheStartInsteadOf409(t *testing.T) {
	s := newReconcileTestServer(t)
	putWarden(t, s, "mach-a")
	l := liveMember(t, s, "m-refocus-after-force")

	lifecycleActions[1].do(t, s, "m-refocus-after-force") // 強制停止
	rec := lifecycleActions[2].do(t, s, "m-refocus-after-force")
	if rec.Code != 200 {
		t.Fatalf("重新聚焦 after 強制停止: want 200 (「不需要錯誤 而是讓他繼續下去」), got %d %s",
			rec.Code, rec.Body.String())
	}

	mid, _ := s.dal.GetMember("m-refocus-after-force")
	if !mid.RestartAfterStop {
		t.Fatal("重新聚焦 recorded no restart intent — the owner's 「起來」 was dropped again")
	}
	if mid.DesiredState != DesiredStateOffline || mid.ForcedStopAt <= 0 || mid.StoppingSince <= 0 {
		t.Fatalf("the 強制停止 was softened by a 重新聚焦 landing on it: desired=%q forced_stop_at=%v "+
			"stopping_since=%v — 「沿用強硬下線規則」 means the stop is untouched",
			mid.DesiredState, mid.ForcedStopAt, mid.StoppingSince)
	}
	if !forcedEpochLive(*mid) {
		t.Fatal("the forced epoch stopped reading as forced — 重新聚焦 must add the " +
			"上線規則, not downgrade the 下線規則")
	}

	after := settle(t, s, "m-refocus-after-force", l)
	if after.DesiredState != DesiredStateOnline {
		t.Fatalf("after the 強制停止 landed the member is desired=%q — the queued 重啟 "+
			"was never spent, which is the whole 「下線後把人帶起來」", after.DesiredState)
	}
	if after.RestartAfterStop {
		t.Error("the queued start was not consumed — it would fire again after the NEXT 下線")
	}
	if forcedEpochLive(after) {
		t.Error("the forced epoch outlived the restart — the fresh session boots inside " +
			"the previous one's cut-off window")
	}
}

// 改機器 / 換 model after a stop used to answer a clean 200, store the value and
// do nothing. Owner: 「change model / machine 只是帶起來的方式不一樣而已」.
func TestRelocateAndModelChangeAfterAStopBringTheMemberBackUp(t *testing.T) {
	for _, tc := range []struct {
		verb   lifecycleAction
		assert func(t *testing.T, m Member)
	}{
		{lifecycleActions[3], func(t *testing.T, m Member) {
			if m.DesiredMachineID != "mach-b" {
				t.Fatalf("came back up on %q, not the machine the owner picked", m.DesiredMachineID)
			}
		}},
		{lifecycleActions[4], func(t *testing.T, m Member) {
			if m.Model == "model-base" {
				t.Fatal("came back up on the OLD model — 帶起來的方式 was not applied")
			}
		}},
	} {
		t.Run(tc.verb.name, func(t *testing.T) {
			s := newReconcileTestServer(t)
			putWarden(t, s, "mach-a")
			putWarden(t, s, "mach-b")
			id := "m-" + tc.verb.name
			l := liveMember(t, s, id)

			lifecycleActions[1].do(t, s, id) // 強制停止
			rec := tc.verb.do(t, s, id)
			if rec.Code != 200 {
				t.Fatalf("%s after 強制停止: want 200, got %d %s", tc.verb.name, rec.Code, rec.Body.String())
			}
			mid, _ := s.dal.GetMember(id)
			if !mid.RestartAfterStop {
				t.Fatalf("%s stored its value and forgot the intent — the owner still has to "+
					"press 活化, which is the bug", tc.verb.name)
			}
			after := settle(t, s, id, l)
			if after.DesiredState != DesiredStateOnline {
				t.Fatalf("%s after 強制停止 left the member desired=%q", tc.verb.name, after.DesiredState)
			}
			tc.assert(t, after)
		})
	}
}

// 🔴 NEGATIVE CONTROL. The fix must not become "everything comes back up".
// 重新聚焦 → 強制停止 is the owner's own contrast case and it must still end DOWN.
func TestForceStopAfterRefocusStillLeavesTheMemberDown(t *testing.T) {
	s := newReconcileTestServer(t)
	putWarden(t, s, "mach-a")
	l := liveMember(t, s, "m-force-after-refocus")

	lifecycleActions[2].do(t, s, "m-force-after-refocus") // 重新聚焦
	lifecycleActions[1].do(t, s, "m-force-after-refocus") // 強制停止

	after := settle(t, s, "m-force-after-refocus", l)
	if after.DesiredState != DesiredStateOnline && after.RestartAfterStop {
		t.Fatal("強制停止 left a queued start behind it")
	}
	if after.DesiredState != DesiredStateOffline {
		t.Fatalf("重新聚焦 → 強制停止 ended desired=%q. 「refocus -> force stop 跟 force "+
			"stop -> refocus 是不一樣的」 — the last action was a 下線, so the member stays down",
			after.DesiredState)
	}
}

// ── the exhaustive matrix ────────────────────────────────────────────────────

// Every length-2 and length-3 sequence over lifecycleActions, each asserting the
// ONE invariant: 最後一個動作是下線 ⇔ 最終離線.
//
// 🔴 EXHAUSTIVE ON PURPOSE, not a table of the cells that were interesting in
// 2026-09. The defect this ticket fixes was one cell of a 3×3 grid that nobody
// had a reason to look at; the next verb added to this lifecycle gets all of its
// combinations checked by appending ONE entry to lifecycleActions.
func TestActionSequencesEndOnlineExactlyWhenTheLastActionIsNotADown(t *testing.T) {
	s := newReconcileTestServer(t)
	putWarden(t, s, "mach-a")
	putWarden(t, s, "mach-b")

	var seqs [][]int
	for a := range lifecycleActions {
		for b := range lifecycleActions {
			seqs = append(seqs, []int{a, b})
			for c := range lifecycleActions {
				seqs = append(seqs, []int{a, b, c})
			}
		}
	}
	for i, seq := range seqs {
		name := ""
		for _, k := range seq {
			if name != "" {
				name += "→"
			}
			name += lifecycleActions[k].name
		}
		id := fmt.Sprintf("m-seq%03d", i)
		l := liveMember(t, s, id)
		codes := make([]int, 0, len(seq))
		for _, k := range seq {
			codes = append(codes, lifecycleActions[k].do(t, s, id).Code)
		}
		after := settle(t, s, id, l)
		lastIsDown := lifecycleActions[seq[len(seq)-1]].down
		wantOffline := lastIsDown
		gotOffline := after.DesiredState == DesiredStateOffline
		if gotOffline != wantOffline {
			t.Errorf("%s (HTTP %v): desired_state=%q restart_after_stop=%v — "+
				"last action is 下線=%v, so the member must end %s. "+
				"「要不要起來」聽最後一個動作的（後蓋前）",
				name, codes, after.DesiredState, after.RestartAfterStop, lastIsDown,
				map[bool]string{true: "OFFLINE", false: "ONLINE"}[wantOffline])
		}
	}
}

// ── the ratchet, which this change must NOT touch ───────────────────────────

// 「下線用多強」只會往上加（棘輪），不聽順序. Pinned on the 換手 arm of 加速停止,
// which is where the ladder is actually READABLE (winddownStageOf ranks a cause
// carried by a refocus epoch; the 下線 arm expresses 加速停止 through
// stopping_since + refocus_op and ranks as stage 0).
//
// Splitting 「要不要起來」 out of desired_state must leave this rule exactly where
// it was: a 重啟 verb landing on a member already in 加速停止 still may not hand
// it back the slower procedure.
func TestRestartIntentDoesNotSoftenTheWinddownLadder(t *testing.T) {
	s := newReconcileTestServer(t)
	putWarden(t, s, "mach-a")
	putWarden(t, s, "mach-b")
	l := liveMember(t, s, "m-ladder")
	defer s.hub.Disconnect(l)

	lifecycleActions[2].do(t, s, "m-ladder") // 重新聚焦 — opens the 換手 epoch
	rec := httptest.NewRecorder()
	s.HandleAcceleratedStopMemberApiMembersMemberIdAcceleratedStopPost(rec,
		taskReq(t, "POST", "/api/members/m-ladder/accelerated-stop", nil, wireOwnerID, "owner"),
		"m-ladder")
	if rec.Code != 200 {
		t.Fatalf("fixture: 加速停止 want 200, got %d %s", rec.Code, rec.Body.String())
	}
	before, _ := s.dal.GetMember("m-ladder")
	if got := winddownStageOf(*before); got != winddownStageAccelerated {
		t.Fatalf("fixture: stage after 加速停止 = %d, want %d", got, winddownStageAccelerated)
	}

	// 重新聚焦 is stage 停止 and is REFUSED outright — the pre-existing shape of
	// the rule, and the one this ticket deliberately did not widen.
	if rec := lifecycleActions[2].do(t, s, "m-ladder"); rec.Code != 409 {
		t.Errorf("重新聚焦 after 加速停止: want 409, got %d %s — 「下線用多強」只會往上加；"+
			"an agent that was told it is counting down silently stopped counting",
			rec.Code, rec.Body.String())
	}
	// 改機器 / 換 model still SAVE (the owner's value is never lost) and still do
	// not move the stage.
	for _, verb := range []lifecycleAction{lifecycleActions[3], lifecycleActions[4]} {
		if rec := verb.do(t, s, "m-ladder"); rec.Code != 200 {
			t.Fatalf("%s after 加速停止: want 200, got %d %s", verb.name, rec.Code, rec.Body.String())
		}
	}
	after, _ := s.dal.GetMember("m-ladder")
	if after.RefocusOp != refocusOpAcceleratedStop {
		t.Errorf("a 重啟 verb pushed the wind-down cause back to %q — the deadline the "+
			"agent was told about is gone with it", after.RefocusOp)
	}
	if got := winddownStageOf(*after); got != winddownStageAccelerated {
		t.Errorf("the ladder went from 加速停止 back to stage %d", got)
	}
}

// 活化 is the ONE exception to the ratchet — it cancels the stop outright rather
// than queueing a start behind it — and this change must not tidy it away.
func TestActivateStillCancelsTheStopOutright(t *testing.T) {
	s := newReconcileTestServer(t)
	putWarden(t, s, "mach-a")
	l := liveMember(t, s, "m-activate")
	defer s.hub.Disconnect(l)

	lifecycleActions[1].do(t, s, "m-activate") // 強制停止
	lifecycleActions[5].do(t, s, "m-activate") // 活化

	after, _ := s.dal.GetMember("m-activate")
	if after.DesiredState != DesiredStateOnline || after.StoppingSince != 0 {
		t.Fatalf("活化 no longer cancels the stop: desired=%q stopping_since=%v",
			after.DesiredState, after.StoppingSince)
	}
	if after.RestartAfterStop {
		t.Error("活化 left a queued start behind — it would fire again after the next 下線")
	}
}
