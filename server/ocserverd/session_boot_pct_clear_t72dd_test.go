package main

import "testing"

// T-72dd 殘值清理 — a session boundary must drop the session's context report
// along with its anchor.
//
// 🔴 SCOPE, VERBATIM FROM THE TICKET: this is NOT a fix for the original
// symptom. It is neither confirmed nor excluded as a cause. The reason to
// change it is narrower and does not depend on that question at all: TWO
// READERS OF ONE GAUGE DISAGREE ABOUT WHAT THE NUMBER IS.
//
//   - actionableContextPct (sse_bands.go) — the threshold/gate reader — refuses
//     a pct whose context_pct_ts is not strictly newer than boot_ts.
//   - foldActorRuntime (wire.go) — the cockpit / get_monitoring reader — takes
//     gauge["context_pct"] raw, with no such test.
//
// clearSessionBootTS drops boot_ts and compaction_count and leaves context_pct
// / context_pct_ts standing, so between a boundary and the next report the two
// readers answer different questions with different numbers. That is wrong on
// its own terms.

// bootBoundaryGauge is the gauge entry a live session leaves behind: an anchor,
// a codex round count, and a context report that is newer than the anchor
// (i.e. one the gate reader WOULD have honoured a moment ago).
func bootBoundaryGauge(bootTS float64) map[string]any {
	return map[string]any{
		"boot_ts":          bootTS,
		"compaction_count": 3,
		"context_pct":      88.0,
		"context_pct_ts":   bootTS + 10,
	}
}

// TestClearSessionBootTS_DropsTheContextReportToo_T72dd: the session's pct and
// its stamp are session-scoped state exactly like boot_ts and
// compaction_count, and must leave on the same boundary.
func TestClearSessionBootTS_DropsTheContextReportToo_T72dd(t *testing.T) {
	s := newWorkerTestServer(t)
	now := 2_000_000.0
	s.outsourceMu.Lock()
	gateDiagWorker(t, s, "ow-clr", 88, now+10, now, true)
	s.outsourceMu.Unlock()
	s.gauge.Set("ow-clr", bootBoundaryGauge(now))

	s.clearSessionBootTS("ow-clr")

	entry := s.gauge.Get("ow-clr")
	for _, key := range []string{"boot_ts", "compaction_count", "context_pct", "context_pct_ts"} {
		if _, still := entry[key]; still {
			t.Errorf("a session boundary must drop %q — gauge still holds %v",
				key, entry[key])
		}
	}
}

// TestClearSessionBootTS_CockpitAndGateReadTheSameNumber_T72dd is the reason
// the change is being made: after the boundary the two readers must agree.
// Before this change the cockpit reader answered 88 while the gate reader
// answered "no number at all".
func TestClearSessionBootTS_CockpitAndGateReadTheSameNumber_T72dd(t *testing.T) {
	s := newWorkerTestServer(t)
	now := 2_000_000.0
	s.outsourceMu.Lock()
	gateDiagWorker(t, s, "ow-agree", 88, now+10, now, true)
	s.outsourceMu.Unlock()
	s.gauge.Set("ow-agree", bootBoundaryGauge(now))

	// Before the boundary the two readers already agree — the report is newer
	// than the anchor, so both see 88. (Stated so the assertion after the
	// boundary cannot be satisfied by a gauge that was empty all along.)
	entry := s.gauge.Get("ow-agree")
	if got := foldActorRuntime(nil, entry, 0, RuntimeClaude).contextPct; got == nil || *got != 88.0 {
		t.Fatalf("precondition: the cockpit reader must see 88, got %v", got)
	}
	if got := actionableContextPct(entry, true); got == nil || *got != 88.0 {
		t.Fatalf("precondition: the gate reader must see 88, got %v", got)
	}

	s.clearSessionBootTS("ow-agree")

	entry = s.gauge.Get("ow-agree")
	cockpit := foldActorRuntime(nil, entry, 0, RuntimeClaude).contextPct
	gate := actionableContextPct(entry, true)
	if gate != nil {
		t.Fatalf("the gate reader must have no number after a boundary, got %v", *gate)
	}
	if cockpit != nil {
		t.Fatalf("THE TWO READERS MUST NOT DISAGREE: the gate reader has no "+
			"number after the boundary, but the cockpit still shows %v", *cockpit)
	}
}
