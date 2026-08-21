package main

import (
	"context"
	"net/http/httptest"
	"testing"
)

// refocus_epoch_latch_ted79_test.go — ONE invariant, asserted as STATE and as
// the DECISION that state produces:
//
//	a member that is handed a FRESH refocus epoch never inherits the previous
//	wind-down's latch (stopped_since / stopping_since), from any of the four
//	stamp sites — and a desired-online member whose latch was left behind with
//	no epoch at all does not keep it forever.
//
// WHY it is an invariant and not tidiness: decideUp's recycle arm reads
// AgentStopped = stopped_since > 0, and with a refocus marker present that arm
// robust-stops IN THE SAME TICK — zero grace, no close-out performed. So a
// stale latch does not merely look untidy: it converts the next epoch stamped
// on that member, whatever opened it, into an immediate kill. The second reader
// is the SSE stop gate, which refuses a reconnect once stopped_since is set.
//
// The state assertions and the decision assertion are BOTH here on purpose:
// clearing the field is the mechanism, "not killed on the spot" is the harm,
// and a future change that keeps the field clean by a different route should
// still be free to do so.

// latched is a member that is wanted online and carries a stale wind-down latch
// from a PREVIOUS epoch — the shape any agent leaves behind after it reports
// stopped and is respawned without the anchors being cleared.
func latched(id string) Member {
	m := testAgent(id)
	m.StoppedSince = 111.0
	m.StoppingSince = 110.0
	return m
}

func assertEpochIsClean(t *testing.T, site string, m Member) {
	t.Helper()
	if m.RefocusSince <= 0 {
		t.Fatalf("%s: no epoch was opened at all (refocus_since=%v)", site, m.RefocusSince)
	}
	if m.StoppedSince != 0 {
		t.Fatalf("%s: fresh epoch inherited stopped_since=%v — decideUp's recycle arm "+
			"reads that as 'dump done' and robust-stops this member on the next tick, "+
			"with no grace and no close-out", site, m.StoppedSince)
	}
	if m.StoppingSince != 0 {
		t.Fatalf("%s: fresh epoch inherited stopping_since=%v", site, m.StoppingSince)
	}
}

func TestRefocusEpoch_NoStampSiteInheritsAStaleWindDownLatch(t *testing.T) {
	t.Run("owner presses 重新聚焦", func(t *testing.T) {
		api, dal := newGateTestAPI(t)
		putGateMember(t, dal, latched("m-ed79-refocus"))
		defer online(t, api, "m-ed79-refocus")()

		r := httptest.NewRequest("POST", "/api/members/m-ed79-refocus/refocus", nil)
		r = r.WithContext(context.WithValue(r.Context(), claimsContextKey,
			map[string]any{"sub": "owner", "scope": "owner"}))
		rec := httptest.NewRecorder()
		api.HandleRefocusMemberApiMembersMemberIdRefocusPost(rec, r, "m-ed79-refocus")
		if rec.Code != 200 {
			t.Fatalf("refocus: want 200, got %d %s", rec.Code, rec.Body.String())
		}

		got, _ := dal.GetMember("m-ed79-refocus")
		assertEpochIsClean(t, "POST /members/{id}/refocus", *got)
	})

	t.Run("agent asks for its own handover", func(t *testing.T) {
		api, dal := newGateTestAPI(t)
		putGateMember(t, dal, latched("m-ed79-self"))
		defer online(t, api, "m-ed79-self")()
		api.gauge.Set("m-ed79-self", map[string]any{
			"boot_ts": nowSecs() - (minSelfRestartSecs + 100),
		})

		rec := doRestartSelf(api, "m-ed79-self", "")
		if rec.Code != 200 {
			t.Fatalf("restart_self: want 200, got %d %s", rec.Code, rec.Body.String())
		}

		got, _ := dal.GetMember("m-ed79-self")
		assertEpochIsClean(t, "POST /self/refocus (staff arm)", *got)
	})

	t.Run("context auto-stamp", func(t *testing.T) {
		s := newReconcileTestServer(t)
		putTestMember(t, s, latched("m-ed79-ctx"))
		got, _ := s.dal.GetMember("m-ed79-ctx")
		members := []Member{*got}
		connectOnline(t, s, "m-ed79-ctx")

		now := 10000.0
		s.gauge.Set("m-ed79-ctx", map[string]any{
			"context_pct": 99.0, "context_pct_ts": now - 10, "boot_ts": now - 500,
		})

		s.stampContextHighRecycle(members, now)

		// The in-slice member is what the SAME tick observes; the row is what
		// survives it. Both have to be clean, because both feed a reader.
		assertEpochIsClean(t, "stampContextHighRecycle (in-slice)", members[0])
		after, _ := s.dal.GetMember("m-ed79-ctx")
		assertEpochIsClean(t, "stampContextHighRecycle (row)", *after)
	})

	t.Run("owner-verb funnel", func(t *testing.T) {
		s := newReconcileTestServer(t)
		m := latched("m-ed79-ownerop")
		putTestMember(t, s, m)
		connectOnline(t, s, "m-ed79-ownerop")

		if !s.armMemberOwnerOpHandover(&m, memberOpRelocate) {
			t.Fatalf("armMemberOwnerOpHandover declined to open a wind-down on a live "+
				"member with uncollected state: %+v", m)
		}
		assertEpochIsClean(t, "armMemberOwnerOpHandover", m)
	})
}

// The harm itself, on the shortest path from stamp to decision: the member the
// owner just refocused must not be robust-stopped by the very next tick.
func TestRefocusEpoch_AStaleLatchDoesNotTurnAFreshEpochIntoAnImmediateKill(t *testing.T) {
	api, dal := newGateTestAPI(t)
	putGateMember(t, dal, latched("m-ed79-kill"))
	defer online(t, api, "m-ed79-kill")()

	r := httptest.NewRequest("POST", "/api/members/m-ed79-kill/refocus", nil)
	r = r.WithContext(context.WithValue(r.Context(), claimsContextKey,
		map[string]any{"sub": "owner", "scope": "owner"}))
	rec := httptest.NewRecorder()
	api.HandleRefocusMemberApiMembersMemberIdRefocusPost(rec, r, "m-ed79-kill")
	if rec.Code != 200 {
		t.Fatalf("refocus: want 200, got %d %s", rec.Code, rec.Body.String())
	}
	m, _ := dal.GetMember("m-ed79-kill")

	obs := obsOf(m.ID, DesiredStateOnline, true)
	obs.RefocusSince = m.RefocusSince
	obs.RefocusOp = m.RefocusOp
	obs.AgentStopped = m.StoppedSince > 0.0

	d := reconcileDecide(obs, newReconcileState(), defaultReconcileConfig(), m.RefocusSince+1)
	if d.Command == reconcileCmdStop {
		t.Fatalf("the tick right after the stamp collected the member: %s — the agent "+
			"was given zero seconds of the close-out it was just told to work", d.Reason)
	}
}

// The other half of block 1: a latch left behind with NO epoch. Nothing used to
// clear it, because the loop-break skipped every row with refocus_since <= 0.
func TestRecycleLoopBreak_ClearsAWindDownLatchThatHasNoEpoch(t *testing.T) {
	t.Run("desired online, session gone — the latch is cleared", func(t *testing.T) {
		s := newReconcileTestServer(t)
		m := testAgent("m-ed79-noepoch")
		m.StoppedSince = 111.0
		m.StoppingSince = 110.0
		putTestMember(t, s, m)
		members := []Member{m}

		s.clearRecycleMarkersOnRespawn(members)

		if members[0].StoppedSince != 0 || members[0].StoppingSince != 0 {
			t.Fatalf("no-epoch latch survived the loop-break: stopped_since=%v "+
				"stopping_since=%v — the next epoch stamped on this member is an "+
				"immediate kill", members[0].StoppedSince, members[0].StoppingSince)
		}
		after, _ := s.dal.GetMember("m-ed79-noepoch")
		if after.StoppedSince != 0 || after.StoppingSince != 0 {
			t.Fatalf("row kept the latch: stopped_since=%v stopping_since=%v",
				after.StoppedSince, after.StoppingSince)
		}
	})

	// The gate that keeps a close-out IN FLIGHT untouched: a live session. This
	// is the whole answer to "does the new branch cut a hand-off short" — while
	// the socket is up, the anchors the agent stamped are its own and stay.
	t.Run("still connected — a close-out in flight keeps its anchors", func(t *testing.T) {
		s := newReconcileTestServer(t)
		m := testAgent("m-ed79-inflight")
		m.StoppingSince = 110.0
		putTestMember(t, s, m)
		connectOnline(t, s, "m-ed79-inflight")
		members := []Member{m}

		s.clearRecycleMarkersOnRespawn(members)

		if members[0].StoppingSince != 110.0 {
			t.Fatalf("cleared stopping_since=%v on a member whose session is still "+
				"live — that member is mid-close-out, not respawn-pending",
				members[0].StoppingSince)
		}
	})
}
