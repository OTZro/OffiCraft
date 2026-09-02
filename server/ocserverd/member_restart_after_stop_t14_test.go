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
//
// MUTANT RECORD (each `go test . -count=1`, restored from a scratchpad copy —
// never `git checkout --`, this tree carried uncommitted work). The four are
// what an independent review demanded after the first round's assertions turned
// out to be vacuous:
//
//	A  gut clearRestartIntent (member_ownerop_winddown.go)
//	   → RED: TestForceStopAfterAQueuedRestartStillLeavesTheMemberDown,
//	     TestActivateStillCancelsTheStopOutright. Both were GREEN under this
//	     mutant before the middle 重新聚焦 step was added — that is why the step
//	     is there.
//	B  aStopWasEverAskedFor → true (drop the never-活化 protection)
//	   → RED: TestRelocateMember_PlacementOnly,
//	     TestRelocateMember_OfflineRelocateIsNotPending,
//	     TestMemberOwnerOp_StoppedMemberIsNotRevived,
//	     TestUpdateMemberOnAHeldDownMemberLeavesAReceipt,
//	     TestRelocateAHeldDownMemberLeavesAReceipt. The last three were GREEN
//	     under this mutant until their fixtures were made to say which row shape
//	     they build.
//	C  aStopWasEverAskedFor → false (revert the ruling)
//	   → RED: the whole T-14 set, AND the matrix's bucket accounting
//	     ("fell into no bucket"), which is how that accounting is known not to
//	     be a comment that merely restates the loop.
//	D  swap memberRestartQueuedReceipt → memberHeldDownReceipt in both gates
//	   → RED: TestUpdateMemberOnAHeldDownMemberLeavesAReceipt only. The relocate
//	     face stayed GREEN and that is a real finding, recorded there: relocate
//	     runs the event-driven reconcile before returning, so on an already-
//	     converged member consumeRestartAfterStop overwrites the queued receipt
//	     in the same request.

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

// lifecycleActions is the SIX verbs the matrix below drives. It is NOT the closed
// set of lifecycle verbs and this file no longer claims it is.
//
// 🔴 THE SEVENTH VERB IS 加速停止, AND IT IS THE ONE COUNTEREXAMPLE. On its 換手
// arm (HandleAcceleratedStopMember…, `case m.RefocusSince > 0`) it re-stamps the
// handover and leaves desired_state ONLINE — it hurries a handover along rather
// than turning it into a stop — so 重新聚焦 → 加速停止 ends with the member
// RUNNING even though the last action was a 下線 rung. Measured, not assumed.
// That is a literal violation of the ⇔ oracle below, so including the verb would
// make the matrix red.
//
// IT IS EXCLUDED, AND HERE IS THE HOLE THAT LEAVES: every sequence ending in
// 加速停止 on the 換手 arm is unchecked by the matrix, and the one real question
// underneath it — whether 加速停止 pressed on a member mid-換手 should end with
// that member down — is UNANSWERED, not answered "no". Closing it means
// converting that arm into a stop, a behaviour change outside the owner's
// 2026-08-30 [0] ruling, so it is left for the owner rather than decided here.
// What IS pinned about 加速停止: it clears the queued 「起來」 like the other 下線
// rungs (clearRestartIntent), and it keeps its rung on the ladder against every
// 重啟 verb — TestRestartIntentDoesNotSoftenTheWinddownLadder.
//
// Adding a SEVENTH verb here is still the cheap way to get it combined with
// everything else — that property is real; "exhaustive over the lifecycle" is
// what was not.
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
//
// 🔴 THE SEQUENCE HAS THREE STEPS ON PURPOSE, and the two-step version this
// replaced was worth nothing. It ran 重新聚焦 → 強制停止 on a member that was
// online and desired-online, so the 重新聚焦 took the OLD path (it armed a refocus
// epoch) and never set restart_after_stop at all; the 強制停止 then "cleared" a
// flag that was already false, and its assertion held for a reason that had
// nothing to do with the code under test. Proof: gutting clearRestartIntent left
// this test green. The first 強制停止 below is what puts the member on the arm
// where 重新聚焦 records an intent, so the second one has something to cancel.
func TestForceStopAfterAQueuedRestartStillLeavesTheMemberDown(t *testing.T) {
	s := newReconcileTestServer(t)
	putWarden(t, s, "mach-a")
	l := liveMember(t, s, "m-force-after-refocus")

	lifecycleActions[1].do(t, s, "m-force-after-refocus") // 強制停止
	lifecycleActions[2].do(t, s, "m-force-after-refocus") // 重新聚焦 — queues 「起來」

	queued, _ := s.dal.GetMember("m-force-after-refocus")
	if !queued.RestartAfterStop {
		t.Fatal("fixture: 重新聚焦 queued no restart intent, so the 強制停止 below would " +
			"have nothing to cancel and this test would pin nothing")
	}

	lifecycleActions[1].do(t, s, "m-force-after-refocus") // 強制停止 again — 後蓋前

	cancelled, _ := s.dal.GetMember("m-force-after-refocus")
	if cancelled.RestartAfterStop {
		t.Error("強制停止 left the queued start behind it — 「要不要起來」聽最後一個動作的")
	}
	after := settle(t, s, "m-force-after-refocus", l)
	if after.DesiredState != DesiredStateOffline {
		t.Fatalf("…→重新聚焦→強制停止 ended desired=%q. 「refocus -> force stop 跟 force "+
			"stop -> refocus 是不一樣的」 — the last action was a 下線, so the member stays down",
			after.DesiredState)
	}
}

// ── the sequence matrix: six of the seven verbs, 加速停止 excluded ───────────

// Every length-2 and length-3 sequence over the SIX verbs in lifecycleActions
// (252 of them), each asserting the one invariant: 最後一個動作是下線 ⇔ 最終離線.
//
// 🔴 HOW MANY OF THOSE ACTUALLY EXERCISE ANYTHING. Counted, because "252
// sequences" is a number that flatters and 252 was doing exactly that. The four
// buckets below are DISJOINT and sum to 252:
//
//	80  contain no 下線 verb at all — the oracle's right-hand side is trivially
//	    true for them; they pin the 上線 side only.
//	22  contain a 下線 verb and end in 活化, which flips desired_state online
//	    unconditionally; they pin that 活化 still wins, not that the queue works.
//	66  put a 重啟 verb behind a 下線 and reach the new code — the queue being
//	    set, carried, and spent. THIS is the matrix's real yield.
//	84  end on a 下線 rung; they pin 後蓋前 in the cancelling direction.
//
// (Counted the overlapping way instead, 42 sequences end in 活化 — 20 of them are
// already inside the first bucket. Both framings are in the assertion below.)
//
// The counts are ASSERTED below rather than written here and left to rot: a
// change that quietly stops the queue from being exercised turns this red
// instead of leaving a comment claiming coverage that has evaporated.
//
// Not exhaustive over the lifecycle — see lifecycleActions for the seventh verb
// and the hole its exclusion leaves.
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
	noDown, endsActivate, reachesQueue, endsDown, activateTail := 0, 0, 0, 0, 0
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
		// Which bucket this sequence is in, decided by what the ROW did, not by
		// reading the verb names: queued is true only if the new column was
		// actually set at some point during the sequence.
		beforeSettle, _ := s.dal.GetMember(id)
		queued := beforeSettle.RestartAfterStop || beforeSettle.DesiredState == DesiredStateOnline &&
			hasDownVerb(seq) && !lifecycleActions[seq[len(seq)-1]].down
		last := lifecycleActions[seq[len(seq)-1]]
		if last.name == "活化" {
			activateTail++
		}
		switch {
		case !hasDownVerb(seq):
			noDown++
		case last.name == "活化":
			endsActivate++
		case last.down:
			endsDown++
		case queued:
			reachesQueue++
		default:
			t.Errorf("%s fell into no bucket — the coverage accounting has a hole", name)
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
	t.Logf("coverage: %d sequences = %d no-下線 + %d 活化-tail-with-下線 + %d reach-the-queue "+
		"+ %d end-on-a-下線 (%d end in 活化 counted the overlapping way)",
		len(seqs), noDown, endsActivate, reachesQueue, endsDown, activateTail)
	if noDown != 80 || endsActivate != 22 || reachesQueue != 66 || endsDown != 84 ||
		activateTail != 42 || noDown+endsActivate+reachesQueue+endsDown != len(seqs) {
		t.Errorf("the matrix's coverage shape changed: no-下線=%d (want 80), 活化-tail-with-"+
			"下線=%d (want 22), reaches-the-queue=%d (want 66), ends-on-a-下線=%d (want 84), "+
			"活化-tail overall=%d (want 42). Either a verb was added (update these numbers "+
			"AND the comment above) or the queue stopped being exercised by sequences that "+
			"used to reach it — which would leave the ⇔ oracle passing on trivia",
			noDown, endsActivate, reachesQueue, endsDown, activateTail)
	}
}

// hasDownVerb reports whether a sequence contains any 下線 rung at all. A
// sequence without one can never make the oracle's right-hand side true, which
// is why those are counted separately rather than presented as coverage.
func hasDownVerb(seq []int) bool {
	for _, k := range seq {
		if lifecycleActions[k].down {
			return true
		}
	}
	return false
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
//
// 🔴 THE MIDDLE STEP IS LOAD-BEARING for the same reason as above: without a
// 重新聚焦 to queue an intent first, 活化's clearRestartIntent has nothing to
// clear and the flag assertion is vacuous (gutting clearRestartIntent left the
// two-step version green).
func TestActivateStillCancelsTheStopOutright(t *testing.T) {
	s := newReconcileTestServer(t)
	putWarden(t, s, "mach-a")
	l := liveMember(t, s, "m-activate")
	defer s.hub.Disconnect(l)

	lifecycleActions[1].do(t, s, "m-activate") // 強制停止
	lifecycleActions[2].do(t, s, "m-activate") // 重新聚焦 — queues 「起來」
	queued, _ := s.dal.GetMember("m-activate")
	if !queued.RestartAfterStop {
		t.Fatal("fixture: nothing was queued, so the assertion below would be vacuous")
	}

	lifecycleActions[5].do(t, s, "m-activate") // 活化

	after, _ := s.dal.GetMember("m-activate")
	if after.DesiredState != DesiredStateOnline || after.StoppingSince != 0 {
		t.Fatalf("活化 no longer cancels the stop: desired=%q stopping_since=%v",
			after.DesiredState, after.StoppingSince)
	}
	if after.RestartAfterStop {
		t.Error("活化 SPENT nothing — it left the queued start on the row, where it " +
			"would fire a second time after the owner's next 下線")
	}
}
