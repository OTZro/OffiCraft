package main

// pacing_test.go — case-for-case port of
// the retired Python tests/domain/test_token_pacing.py. Discipline under test: a value
// that cannot be measured is nil (未量到), NEVER a fabricated 0; elapsed% is a
// now-vs-resets_at back-computation; and used% running more than the margin
// ahead of elapsed% reads as hot.

import "testing"

// shapeFresh / shapeFreshAll shape a window whose snapshot was taken RIGHT NOW.
// Every assertion in this file predates T-3b90 and is about the arithmetic
// (honest nulls, back-computed elapsed%, the margin), not about age — so they
// each pin the fresh case, which is the one where the pace verdict is still
// meaningful. Age itself is covered by TestShapeWindowStaleSnapshot* below.
func shapeFresh(raw any, windowSec, now float64) *PaceWindow {
	return ShapeWindow(raw, windowSec, now, &now, telemetryFreshSecs)
}

func shapeFreshAll(rateLimits any, now float64) map[string]*PaceWindow {
	return ShapeWindows(rateLimits, now,
		map[string]float64{"five_hour": now, "seven_day": now}, telemetryFreshSecs)
}

func TestShapeWindowsMissingRateLimitsIsNilNotZero(t *testing.T) {
	got := shapeFreshAll(nil, 1_000_000.0)
	if got["five_hour"] != nil || got["seven_day"] != nil {
		t.Fatalf("missing rate_limits must shape both windows nil, got %+v", got)
	}
	if shapeFreshAll("nope", 1_000_000.0)["five_hour"] != nil {
		t.Fatal("non-object rate_limits must shape nil")
	}
	// A window whose raw value is not an object → nil.
	got = shapeFreshAll(map[string]any{"five_hour": 5.0}, 1_000_000.0)
	if got["five_hour"] != nil {
		t.Fatalf("non-object window must shape nil, got %+v", got["five_hour"])
	}
}

func TestShapeWindowUsedPctUnmeasuredStaysNil(t *testing.T) {
	now := 1_000_000.0
	win := WindowSeconds["five_hour"]
	// used_percentage absent / sentinel -1 / non-number → used_pct nil, but
	// the window object still returns (partial is allowed).
	w := shapeFresh(map[string]any{"resets_at": now + 9000}, win, now)
	if w == nil || w.UsedPct != nil {
		t.Fatalf("absent used_percentage must stay nil, got %+v", w)
	}
	if w := shapeFresh(map[string]any{"used_percentage": -1.0}, win, now); w.UsedPct != nil {
		t.Fatalf("sentinel -1 must stay nil, got %v", *w.UsedPct)
	}
	if w := shapeFresh(map[string]any{"used_percentage": "x"}, win, now); w.UsedPct != nil {
		t.Fatalf("non-number must stay nil, got %v", *w.UsedPct)
	}
	if w := shapeFresh(map[string]any{"used_percentage": true}, win, now); w.UsedPct != nil {
		t.Fatalf("a bool is not a measurement, got %v", *w.UsedPct)
	}
}

func TestShapeWindowElapsedPctBackcomputedFromResetsAt(t *testing.T) {
	now := 1_000_000.0
	win := WindowSeconds["five_hour"]
	// Half the window remains → resets_at = now + win/2 → elapsed = 50%.
	w := shapeFresh(map[string]any{"used_percentage": 10.0, "resets_at": now + win/2}, win, now)
	if w == nil || w.ElapsedPct == nil || *w.ElapsedPct != 50.0 {
		t.Fatalf("elapsed must back-compute to 50%%, got %+v", w)
	}
	// resets_at missing / unparseable → elapsed nil (never 0).
	if w := shapeFresh(map[string]any{"used_percentage": 10.0}, win, now); w.ElapsedPct != nil {
		t.Fatalf("missing resets_at must leave elapsed nil, got %v", *w.ElapsedPct)
	}
	garbage := map[string]any{"used_percentage": 10.0, "resets_at": "garbage"}
	if w := shapeFresh(garbage, win, now); w.ElapsedPct != nil {
		t.Fatalf("garbage resets_at must leave elapsed nil, got %v", *w.ElapsedPct)
	}
	// resets_at is echoed AS GIVEN (even when unparseable).
	if w := shapeFresh(garbage, win, now); w.ResetsAt != "garbage" {
		t.Fatalf("resets_at must echo as given, got %v", w.ResetsAt)
	}
}

func TestShapeWindowPaceHotWhenUsedRunsAhead(t *testing.T) {
	now := 1_000_000.0
	win := WindowSeconds["five_hour"]
	resetsAt := now + win/2 // elapsed = 50%
	// used far ahead of elapsed (> margin) → hot.
	hot := shapeFresh(map[string]any{"used_percentage": 80.0, "resets_at": resetsAt}, win, now)
	if hot == nil || hot.Pace == nil || *hot.Pace != PaceHot {
		t.Fatalf("80%% used at 50%% elapsed must be hot, got %+v", hot)
	}
	// used at/behind pace → ok.
	ok := shapeFresh(map[string]any{"used_percentage": 50.0, "resets_at": resetsAt}, win, now)
	if ok == nil || ok.Pace == nil || *ok.Pace != PaceOK {
		t.Fatalf("on-pace usage must be ok, got %+v", ok)
	}
	// Exactly on the margin boundary is NOT hot (strict >).
	edge := shapeFresh(
		map[string]any{"used_percentage": 50.0 + PaceMarginPct, "resets_at": resetsAt}, win, now)
	if edge == nil || edge.Pace == nil || *edge.Pace != PaceOK {
		t.Fatalf("margin boundary must be ok, got %+v", edge)
	}
}

func TestShapeWindowPaceNilWhenEitherInputMissing(t *testing.T) {
	now := 1_000_000.0
	win := WindowSeconds["five_hour"]
	// used present but elapsed unmeasurable → pace can't be judged → nil.
	w := shapeFresh(map[string]any{"used_percentage": 90.0}, win, now)
	if w == nil || w.Pace != nil {
		t.Fatalf("unjudgeable pace must stay nil, got %+v", w)
	}
}

// ─── T-3b90: a snapshot has an age, and a stale one cannot be judged ─────────

func TestShapeWindowCarriesTheSnapshotsAge(t *testing.T) {
	now := 1_000_000.0
	win := WindowSeconds["seven_day"]
	took := now - 3600 // measured an hour ago
	w := ShapeWindow(
		map[string]any{"used_percentage": 43.0, "resets_at": now + win/2},
		win, now, &took, telemetryFreshSecs)
	if w == nil || w.MeasuredAt == nil {
		t.Fatalf("a stamped snapshot must carry its age, got %+v", w)
	}
	if *w.MeasuredAt != took {
		t.Fatalf("measured_at must be the stamp as given, want %v got %v", took, *w.MeasuredAt)
	}
	// Unknown age stays honest-null — NEVER back-filled with `now`, which would
	// dress an unstamped snapshot up as a live reading.
	if u := ShapeWindow(
		map[string]any{"used_percentage": 43.0, "resets_at": now + win/2},
		win, now, nil, telemetryFreshSecs); u.MeasuredAt != nil {
		t.Fatalf("unknown age must stay nil, got %v", *u.MeasuredAt)
	}
}

func TestShapeWindowStaleSnapshotIsNotJudgedButIsStillServed(t *testing.T) {
	now := 1_000_000.0
	win := WindowSeconds["seven_day"]
	// The reported shape of T-3b90: used% far ahead of elapsed% — which WOULD
	// read hot — but nobody has refreshed the number in days.
	raw := map[string]any{"used_percentage": 43.0, "resets_at": now + win*0.85}
	old := now - 3*24*3600
	stale := ShapeWindow(raw, win, now, &old, telemetryFreshSecs)
	if stale == nil {
		t.Fatal("a stale window must still be served")
	}
	if stale.Pace != nil {
		t.Fatalf("a stale snapshot cannot support a present-tense verdict, got %q", *stale.Pace)
	}
	// The number itself survives — withholding it would cost the owner the one
	// thing the card is for (how much of this week's quota is gone).
	if stale.UsedPct == nil || *stale.UsedPct != 43.0 {
		t.Fatalf("used%% must survive staleness, got %+v", stale.UsedPct)
	}
	if stale.ElapsedPct == nil {
		t.Fatal("elapsed% must survive staleness")
	}
	// Positive control: the SAME numbers, freshly measured, still read hot —
	// so the nil above is about age, not about the arithmetic.
	fresh := ShapeWindow(raw, win, now, &now, telemetryFreshSecs)
	if fresh.Pace == nil || *fresh.Pace != PaceHot {
		t.Fatalf("the same numbers, freshly measured, must still be hot, got %+v", fresh.Pace)
	}
	// Unknown age is treated as stale, not as fresh (fail-closed).
	if u := ShapeWindow(raw, win, now, nil, telemetryFreshSecs); u.Pace != nil {
		t.Fatalf("unknown age must not support a verdict, got %q", *u.Pace)
	}
	// Boundary: exactly at the window is still judgeable (strict >).
	edge := now - telemetryFreshSecs
	if e := ShapeWindow(raw, win, now, &edge, telemetryFreshSecs); e.Pace == nil {
		t.Fatal("a snapshot exactly at the freshness edge must still be judgeable")
	}
}

func TestShapeWindowsStampEachWindowSeparately(t *testing.T) {
	now := 1_000_000.0
	// The 5h window was refreshed just now; the 7d window is days old. One
	// account-wide stamp would let the fresh one vouch for the frozen one.
	old := now - 3*24*3600
	got := ShapeWindows(map[string]any{
		"five_hour": map[string]any{"used_percentage": 10.0, "resets_at": now + 1800},
		"seven_day": map[string]any{"used_percentage": 43.0, "resets_at": now + 100000},
	}, now, map[string]float64{"five_hour": now, "seven_day": old}, telemetryFreshSecs)

	if got["five_hour"].MeasuredAt == nil || *got["five_hour"].MeasuredAt != now {
		t.Fatalf("5h stamp must be its own, got %+v", got["five_hour"].MeasuredAt)
	}
	if got["seven_day"].MeasuredAt == nil || *got["seven_day"].MeasuredAt != old {
		t.Fatalf("7d stamp must be its own, got %+v", got["seven_day"].MeasuredAt)
	}
	if got["five_hour"].Pace == nil {
		t.Fatal("the fresh window must still be judged")
	}
	if got["seven_day"].Pace != nil {
		t.Fatalf("the stale window must not be judged, got %q", *got["seven_day"].Pace)
	}
	// A zero/absent stamp is unknown age, not the epoch-as-a-measurement.
	none := ShapeWindows(map[string]any{
		"five_hour": map[string]any{"used_percentage": 10.0, "resets_at": now + 1800},
	}, now, map[string]float64{"five_hour": 0}, telemetryFreshSecs)
	if none["five_hour"].MeasuredAt != nil {
		t.Fatalf("a zero stamp must read as unknown, got %v", *none["five_hour"].MeasuredAt)
	}
}
