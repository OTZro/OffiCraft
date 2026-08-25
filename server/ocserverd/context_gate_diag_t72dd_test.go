package main

import (
	"strings"
	"testing"
)

// T-72dd 補觀測 — the three quiet gates of stampContextHighRecycle must each
// leave ONE line, and the line must be throttled.
//
// 🔴 THESE ARE RUN-IT TESTS, NOT READ-THE-CODE TESTS. Every assertion below
// reads what a REAL runOutsourceTick actually wrote to stderr (captureStderr),
// because the thing the ticket is buying is "有跑但靜默跳過" being
// distinguishable from "根本沒被呼叫" IN THE LOG — and that is a property of the
// bytes on the stream, not of the source.

// gateDiagWorker seeds an ACTIVE, desired-online worker with an explicit gauge
// so each gate can be aimed at individually. `online` decides whether its SSE
// is up (gate 3 is the only one that reads it).
func gateDiagWorker(
	t *testing.T, s *apiServer, id string,
	pct, pctTS, bootTS float64, online bool,
) {
	t.Helper()
	putTaskFixture(t, s, Task{
		ID: "t-" + id, TypeKey: "review-pr", Title: "x",
		Status: TaskStatusNotStarted, Priority: TaskPriorityMid,
		ExecutorKind: TaskExecutorOutsource, ExecutorID: id,
	})
	if err := s.dal.PutTaskManual(TaskManual{TypeKey: "review-pr", Purpose: "p",
		Fields: "[]", Assignee: `{"kind":"outsource","model":"opus"}`}); err != nil {
		t.Fatalf("put manual: %v", err)
	}
	// Codename is derived from the id (member.codename is UNIQUE, and the
	// per-actor throttle case seeds two workers at once).
	putWorkerFixture(t, s, OutsourceWorker{
		ID: id, Codename: "O-" + id, Model: "opus", Effort: "high",
		TaskID: "t-" + id, Status: WorkerStatusActive, CreatedTS: bootTS,
		DesiredMachineID: ServerSelfHost,
		DesiredState:     DesiredStateOnline,
	})
	if online {
		if _, err := s.hub.Connect(id, ""); err != nil {
			t.Fatalf("connect worker SSE: %v", err)
		}
	}
	s.gauge.Set(id, map[string]any{
		"context_pct": pct, "context_pct_ts": pctTS, "boot_ts": bootTS,
	})
	s.workerSpawnTarget[id] = ServerSelfHost
}

// gateLinesFor returns the diagnostic lines the capture holds for one actor.
func gateLinesFor(out, id string) []string {
	lines := []string{}
	for _, ln := range strings.Split(out, "\n") {
		if strings.Contains(ln, "recycle: gate skip "+id+" ") {
			lines = append(lines, ln)
		}
	}
	return lines
}

// mustHaveAllFiveFields fails unless the line carries every field the ticket
// named. They are asserted as a SET, not as a format string: what matters is
// that whoever reads the log can answer all five questions from one line.
func mustHaveAllFiveFields(t *testing.T, line string) {
	t.Helper()
	for _, key := range []string{"pct=", "pct_ts=", "boot_ts=", "boot_secs=", "online="} {
		if !strings.Contains(line, key) {
			t.Errorf("the gate line must carry %s — got %q", key, line)
		}
	}
}

// TestContextGateDiag_EachQuietGateLeavesOneLine_T72dd walks all three gates
// through a real tick and demands the line — the DoD's 「實跑看得到那一行」.
func TestContextGateDiag_EachQuietGateLeavesOneLine_T72dd(t *testing.T) {
	cases := []struct {
		name          string
		id            string
		pct           float64
		pctTSOff      float64 // relative to now
		bootTSOff     float64 // relative to now
		online        bool
		wantGate      string
		wantSubstring []string
	}{
		{
			// Below both thresholds: actionableContextPct hands back a number
			// and neither rule fires.
			name: "quiet — under both thresholds", id: "ow-g1",
			pct: 10, pctTSOff: -5, bootTSOff: -50_000, online: true,
			wantGate:      "no-actionable-pct",
			wantSubstring: []string{"pct=10 "},
		},
		{
			// 🔴 THE STALE-PCT CASE, and the reason gate 1 is worth a line at
			// all: context_pct_ts <= boot_ts, so actionableContextPct returns
			// nil and NOTHING happens — while the cockpit (foldActorRuntime,
			// raw gauge read) still renders 90. Two numbers, one actor.
			name: "quiet — stale pct the cockpit still shows", id: "ow-g1s",
			pct: 90, pctTSOff: -60_000, bootTSOff: -50_000, online: true,
			wantGate:      "no-actionable-pct",
			wantSubstring: []string{"pct=90 "},
		},
		{
			// Over the handover threshold but seconds old → the loop-guard.
			name: "quiet — boot-storm loop-guard", id: "ow-g2",
			pct: 90, pctTSOff: -1, bootTSOff: -10, online: true,
			wantGate:      "boot-storm",
			wantSubstring: []string{"pct=90 ", "boot_secs=10.0", "online=true"},
		},
		{
			// Over the threshold, old enough, but no live session.
			name: "quiet — no live session", id: "ow-g3",
			pct: 90, pctTSOff: -5, bootTSOff: -50_000, online: false,
			wantGate:      "offline",
			wantSubstring: []string{"pct=90 ", "online=false"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := newWorkerTestServer(t)
			connectWarden(t, s, ServerSelfHost)
			now := 2_000_000.0
			s.outsourceMu.Lock()
			gateDiagWorker(t, s, tc.id, tc.pct, now+tc.pctTSOff, now+tc.bootTSOff, tc.online)
			s.outsourceMu.Unlock()

			out := captureStderr(t, func() { s.runOutsourceTick(now) })
			lines := gateLinesFor(out, tc.id)
			if len(lines) != 1 {
				t.Fatalf("one gate line per skipped actor per tick, got %d:\n%s",
					len(lines), out)
			}
			line := lines[0]
			mustHaveAllFiveFields(t, line)
			if !strings.Contains(line, "gate="+tc.wantGate+" ") {
				t.Errorf("want gate=%s, got %q", tc.wantGate, line)
			}
			for _, want := range tc.wantSubstring {
				if !strings.Contains(line, want) {
					t.Errorf("want %q in the line, got %q", want, line)
				}
			}
		})
	}
}

// TestContextGateDiag_ThrottledToOnePerActorPer5Min_T72dd is the other half of
// the DoD: 「節流真的有效」— many consecutive ticks, ONE line.
//
// 🔴 THE NUMBERS HERE ARE THE PRODUCTION ONES. The tick cadence is 30 s, so ten
// ticks is five minutes of a real fleet; without the throttle this actor alone
// would contribute ten lines to the log, and the serve.log this ticket was
// found in already had 1.26 million.
func TestContextGateDiag_ThrottledToOnePerActorPer5Min_T72dd(t *testing.T) {
	s := newWorkerTestServer(t)
	connectWarden(t, s, ServerSelfHost)
	now := 2_000_000.0
	s.outsourceMu.Lock()
	gateDiagWorker(t, s, "ow-thr", 10, now-5, now-50_000, true)
	s.outsourceMu.Unlock()

	out := captureStderr(t, func() {
		for i := 0; i < 10; i++ {
			s.runOutsourceTick(now + float64(i)*30.0)
		}
	})
	if n := len(gateLinesFor(out, "ow-thr")); n != 1 {
		t.Fatalf("ten ticks inside the 5-minute window must yield exactly ONE "+
			"line, got %d:\n%s", n, out)
	}

	// …and the throttle is a WINDOW, not a latch: once it has expired the gate
	// says so again, otherwise a permanently-stuck actor would go silent after
	// its first line and we would be back where we started.
	out = captureStderr(t, func() {
		s.runOutsourceTick(now + ctxGateDiagThrottleSecs + 1)
	})
	if n := len(gateLinesFor(out, "ow-thr")); n != 1 {
		t.Fatalf("past the window the gate must speak again, got %d lines:\n%s", n, out)
	}
}

// TestContextGateDiag_ThrottleIsPerActor_T72dd: one actor's line must not
// silence another's — the throttle key is the actor, so a quiet fleet still
// reports every member of it.
func TestContextGateDiag_ThrottleIsPerActor_T72dd(t *testing.T) {
	s := newWorkerTestServer(t)
	connectWarden(t, s, ServerSelfHost)
	now := 2_000_000.0
	s.outsourceMu.Lock()
	gateDiagWorker(t, s, "ow-a", 10, now-5, now-50_000, true)
	gateDiagWorker(t, s, "ow-b", 10, now-5, now-50_000, true)
	s.outsourceMu.Unlock()

	out := captureStderr(t, func() { s.runOutsourceTick(now) })
	for _, id := range []string{"ow-a", "ow-b"} {
		if n := len(gateLinesFor(out, id)); n != 1 {
			t.Fatalf("%s must get its own line, got %d:\n%s", id, n, out)
		}
	}
}
