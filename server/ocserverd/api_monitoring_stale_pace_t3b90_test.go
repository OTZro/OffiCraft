package main

// api_monitoring_stale_pace_t3b90_test.go — T-3b90.
//
// The owner opened the monitor page with no codex agent running anywhere and
// found the `seth-m5-codex` account showing 7-day usage 43% with a red 過熱
// badge. The number was not wrong; it was UNDATED. used% is a snapshot that
// freezes when the last agent leaves, elapsed% is recomputed from the wall
// clock on every request, and the verdict compares the two — so the badge was
// lit, and would later have gone out, purely because time passed.
//
// These tests pin the two halves of the fix at the seam the cockpit actually
// reads (GET /api/monitoring), not at an internal variable:
//   1. every served window states WHEN its used% was measured, and
//   2. a window nobody has refreshed lately carries no present-tense verdict —
//      while still serving the number itself.

import (
	"fmt"
	"testing"
)

// staleAccountRow ingests one codex rate-limit report for `codex:seth`, back-dates
// its rate_limits_ts by ageSecs, and returns the account row the cockpit would
// render. usedPct/resetsAt are chosen by the caller so the arithmetic alone
// would read "hot".
func staleAccountRow(t *testing.T, ageSecs float64) map[string]any {
	t.Helper()
	s := &apiServer{dal: newTestDAL(t), hub: NewHub(),
		telemetry: newMemStore(), gauge: newMemStore()}
	m := fullMember("session")
	m.RoleKey = "builder"
	m.Runtime = RuntimeCodex
	if err := s.dal.PutMember(m); err != nil {
		t.Fatalf("seed member: %v", err)
	}
	// 43% used with ~85% of the 7-day window still ahead of us: used% runs far
	// enough ahead of elapsed% that the margin is cleared with room to spare.
	resetAt := nowSecs() + WindowSeconds["seven_day"]*0.85
	body := fmt.Sprintf(
		`{"runtime":"codex","account":"codex:seth","rate_limits":{"seven_day":{"used_percentage":43,"resets_at":%g}}}`,
		resetAt)
	if rec := doIngestTelemetry(s, "session", "m-seth-m5", body); rec.Code != 200 {
		t.Fatalf("ingest: %d %s", rec.Code, rec.Body.String())
	}
	// Back-date the sample. This is exactly what "the agent went away" looks
	// like from the server's side: the entry stops being rewritten, so its
	// rate_limits_ts recedes into the past while `now` keeps advancing.
	entry := s.telemetry.Get("session")
	entry["rate_limits_ts"] = nowSecs() - ageSecs
	s.telemetry.Set("session", entry)

	return accountRow(t, monitoringOf(t,
		doGetMonitoring(s, map[string]any{"sub": "owner", "scope": "owner"})), "codex:seth")
}

func sevenDayOf(t *testing.T, row map[string]any) map[string]any {
	t.Helper()
	w, ok := row["seven_day"].(map[string]any)
	if !ok {
		t.Fatalf("seven_day = %v, want a shaped window", row["seven_day"])
	}
	return w
}

func TestGetMonitoring_StaleUsageIsServedWithoutAPresentTenseVerdict(t *testing.T) {
	// Three days without a single report — the owner's reported situation.
	w := sevenDayOf(t, staleAccountRow(t, 3*24*3600))

	if pace, present := w["pace"]; present && pace != nil {
		t.Errorf("pace = %v, want no verdict on a three-day-old snapshot", pace)
	}
	// The number itself must survive. Dropping it would take away the one
	// thing the card exists for — how much of this week's quota is gone —
	// and would be a different kind of silence, not a fix.
	if w["used_pct"] != 43.0 {
		t.Errorf("used_pct = %v, want the reported 43 to survive staleness", w["used_pct"])
	}
	if w["elapsed_pct"] == nil {
		t.Error("elapsed_pct must still be served")
	}
}

func TestGetMonitoring_UsageWindowStatesWhenItWasMeasured(t *testing.T) {
	before := nowSecs()
	w := sevenDayOf(t, staleAccountRow(t, 3*24*3600))

	measured, ok := w["measured_at"].(float64)
	if !ok {
		t.Fatalf("measured_at = %v, want the epoch second the snapshot was taken", w["measured_at"])
	}
	// It must be the BACK-DATED stamp, not `now`: a fix that reported the
	// serving time would answer "is this old?" with "no" every single time.
	if age := before - measured; age < 2*24*3600 {
		t.Errorf("measured_at is %.0fs old, want the ~3-day-old sample time", age)
	}
}

func TestGetMonitoring_FreshUsageIsStillJudgedHot(t *testing.T) {
	// Positive control, and the reason the fix is not simply "never say hot":
	// the SAME numbers, measured moments ago, must still raise the badge.
	// Without this, deleting the pace verdict entirely would pass.
	w := sevenDayOf(t, staleAccountRow(t, 0))

	if w["pace"] != PaceHot {
		t.Errorf("pace = %v, want %q for a freshly measured window running ahead", w["pace"], PaceHot)
	}
	if w["measured_at"] == nil {
		t.Error("a fresh window states its age too — the reader should never have to assume")
	}
}
