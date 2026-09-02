package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// The cockpit's 成本歸零 button (T-53, owner ruling rc-7dea0deefa63 option 0
// 「最小、不可逆」). The owner-visible 估計$ is TWO numbers added on the client —
// the durable banked_cost column and the live in-memory telemetry figure — and
// the whole risk in this feature is clearing one of them.
//
// 🔴 MUTANT: delete the `s.dropLiveCost(...)` call from either branch of
// HandleResetCostApiMembersMemberIdCostResetPost and
// TestResetCost_ClearsTheLiveFigureNotJustTheDurableOne (both halves) goes RED.
// A test that only asserts banked_cost == 0 passes that mutant, and the mutant
// ships a button the owner presses to no visible effect.

func doResetCost(t *testing.T, s *apiServer, actorID string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	s.HandleResetCostApiMembersMemberIdCostResetPost(rec,
		taskReq(t, "POST", "/api/members/"+actorID+"/cost/reset", map[string]any{},
			wireOwnerID, "owner"), actorID)
	return rec
}

func costResetBody(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	if rec.Code != http.StatusOK {
		t.Fatalf("reset: %d %s", rec.Code, rec.Body.String())
	}
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode receipt: %v (%s)", err, rec.Body.String())
	}
	return out
}

func costResetServer(t *testing.T) *apiServer {
	t.Helper()
	return &apiServer{dal: newTestDAL(t), hub: NewHub(),
		telemetry: newMemStore(), gauge: newMemStore()}
}

// liveCostOf reads the live half straight out of the telemetry store — the
// value the cockpit would add to the durable half on its very next read. Going
// through the store rather than an HTTP projection is deliberate: it is the
// only way to tell "the figure is gone" apart from "the figure is not being
// rendered right now".
func liveCostOf(s *apiServer, actorID string) (float64, bool) {
	entry := s.telemetry.Get(actorID)
	if entry == nil {
		return 0, false
	}
	v, ok := entry["cost"].(float64)
	return v, ok
}

func TestResetCost_ClearsTheLiveFigureNotJustTheDurableOne(t *testing.T) {
	t.Run("staff member", func(t *testing.T) {
		s := costResetServer(t)
		m := fullMember("seth")
		m.BankedCost = 4.0
		if err := s.dal.PutMember(m); err != nil {
			t.Fatalf("seed member: %v", err)
		}
		if rec := doIngestTelemetry(s, "seth", "m-seth-m5",
			`{"runtime":"claude","account":"seth-m5-claude","cost":1.5}`); rec.Code != 200 {
			t.Fatalf("member ingest: %d %s", rec.Code, rec.Body.String())
		}

		doResetCost(t, s, "seth")

		after, err := s.dal.GetMember("seth")
		if err != nil || after == nil {
			t.Fatalf("re-read member: %v", err)
		}
		if after.BankedCost != 0 {
			t.Errorf("banked_cost = %v, want 0", after.BankedCost)
		}
		// The half that makes this button real. Leave it behind and the number
		// is back on the owner's screen at the next refresh, which he cannot
		// tell apart from the button doing nothing.
		if v, present := liveCostOf(s, "seth"); present {
			t.Errorf("live cost still %v — the durable half alone is not a reset; "+
				"the cockpit adds this back in on its next read", v)
		}
	})

	t.Run("outsource worker", func(t *testing.T) {
		s := costResetServer(t)
		seedWorker(t, s, "ow-7", "S7", 0.25, WorkerStatusActive)
		if rec := doIngestTelemetry(s, "ow-7", "m-seth-m5",
			`{"runtime":"claude","account":"seth-m5-claude","cost":0.5}`); rec.Code != 200 {
			t.Fatalf("worker ingest: %d %s", rec.Code, rec.Body.String())
		}

		doResetCost(t, s, "ow-7")

		after, err := s.dal.GetOutsourceWorker("ow-7")
		if err != nil || after == nil {
			t.Fatalf("re-read worker: %v", err)
		}
		if after.BankedCost != 0 {
			t.Errorf("banked_cost = %v, want 0", after.BankedCost)
		}
		if v, present := liveCostOf(s, "ow-7"); present {
			t.Errorf("live cost still %v — same failure as the member arm, and the "+
				"reason one route serves both kinds", v)
		}
	})
}

// The receipt is the ONLY record of what was destroyed: no snapshot is kept, no
// undo route exists, and spend has no per-charge ledger behind it. If it
// answered with the post-reset state it would say nothing at all.
func TestResetCost_ReceiptCarriesWhatWasDestroyedNotTheZeroesLeftBehind(t *testing.T) {
	s := costResetServer(t)
	m := fullMember("seth")
	m.BankedCost = 4.0
	if err := s.dal.PutMember(m); err != nil {
		t.Fatalf("seed member: %v", err)
	}
	if rec := doIngestTelemetry(s, "seth", "m-seth-m5",
		`{"runtime":"claude","account":"seth-m5-claude","cost":1.5}`); rec.Code != 200 {
		t.Fatalf("member ingest: %d %s", rec.Code, rec.Body.String())
	}

	got := costResetBody(t, doResetCost(t, s, "seth"))

	if got["member_id"] != "seth" {
		t.Errorf("member_id = %v, want seth", got["member_id"])
	}
	if got["cleared_cost"] != 1.5 {
		t.Errorf("cleared_cost = %v, want the 1.5 that was destroyed", got["cleared_cost"])
	}
	if got["cleared_banked_cost"] != 4.0 {
		t.Errorf("cleared_banked_cost = %v, want the 4.0 that was destroyed",
			got["cleared_banked_cost"])
	}
}

// null means "there was nothing to clear on that half", NOT "zero was cleared".
// That distinction is what lets the cockpit keep its existing "both null → dash"
// rule: after a reset the 估計$ cell reads 未量到 rather than 花了 0 元, with no
// display-side special case and no 'was reset' flag.
func TestResetCost_NothingToClearAnswersNullRatherThanZero(t *testing.T) {
	s := costResetServer(t)
	quiet := fullMember("quiet")
	// fullMember is a fully-populated fixture and carries a banked figure; this
	// test is about an actor with NOTHING measured, so zero it explicitly.
	quiet.BankedCost = 0
	if err := s.dal.PutMember(quiet); err != nil {
		t.Fatalf("seed member: %v", err)
	}

	got := costResetBody(t, doResetCost(t, s, "quiet"))

	if got["cleared_cost"] != nil {
		t.Errorf("cleared_cost = %v, want null — nothing was measured, and 0 would "+
			"read as 'zero was cleared'", got["cleared_cost"])
	}
	if got["cleared_banked_cost"] != nil {
		t.Errorf("cleared_banked_cost = %v, want null", got["cleared_banked_cost"])
	}
}

// Pressing it twice is not an error, and the second press must not invent a
// figure that the first one already destroyed.
func TestResetCost_IsIdempotent(t *testing.T) {
	s := costResetServer(t)
	m := fullMember("seth")
	m.BankedCost = 4.0
	if err := s.dal.PutMember(m); err != nil {
		t.Fatalf("seed member: %v", err)
	}

	costResetBody(t, doResetCost(t, s, "seth"))
	second := costResetBody(t, doResetCost(t, s, "seth"))

	if second["cleared_banked_cost"] != nil || second["cleared_cost"] != nil {
		t.Errorf("second reset destroyed something: %v — the first one already took it", second)
	}
}

func TestResetCost_UnknownActorIs404(t *testing.T) {
	s := costResetServer(t)
	if rec := doResetCost(t, s, "nobody"); rec.Code != http.StatusNotFound {
		t.Errorf("unknown actor: %d %s, want 404", rec.Code, rec.Body.String())
	}
}

// A released worker's spend deliberately STAYS on the account card
// (TestGetMonitoring_ReleasedWorkerSpendStaysInTheAccount pins that), and this
// button deliberately does not reach it — a removed roster row is refused by
// every other outsource write door too. Pinned so that widening it later is a
// decision somebody makes on purpose rather than a side effect.
func TestResetCost_ReleasedWorkerIsRefused(t *testing.T) {
	s := costResetServer(t)
	seedWorker(t, s, "ow-gone", "S9", 3.0, WorkerStatusReleased)

	if rec := doResetCost(t, s, "ow-gone"); rec.Code != http.StatusNotFound {
		t.Errorf("released worker: %d %s, want 404", rec.Code, rec.Body.String())
	}
	after, err := s.dal.GetOutsourceWorker("ow-gone")
	if err != nil || after == nil {
		t.Fatalf("re-read worker: %v", err)
	}
	if after.BankedCost != 3.0 {
		t.Errorf("banked_cost = %v, want 3.0 untouched — a refused call writes nothing",
			after.BankedCost)
	}
}
