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
	// Codename is built by prefixing the worker id (member.codename is UNIQUE,
	// and the per-actor throttle case seeds two workers at once).
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
// named. This checks PRESENCE only — every case below additionally pins the
// VALUE of each field, and that is not redundant with this:
//
// 🔴 AN INDEPENDENT REVIEW KILLED THIS SUITE WITH A ONE-LINE MUTANT (MUT-B):
// swapping the pct_ts and boot_ts ARGUMENTS at the call site left every
// assertion green, because presence-only checks cannot see two correct keys
// carrying each other's numbers. That mutant is not cosmetic — this whole
// ticket's flagship case (a stale pct) IS the comparison between those two
// numbers, so a reader of the swapped line would conclude the exact opposite of
// the truth. Hence: every case pins both values literally.
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
			wantGate: "no-actionable-pct",
			// now = 2_000_000 → pct_ts 1999995, boot_ts 1950000. Both pinned by
			// VALUE (MUT-B: swapping the two arguments must not stay green).
			wantSubstring: []string{"pct=10 ", "pct_ts=1999995 ", "boot_ts=1950000 "},
		},
		{
			// 🔴 THE STALE-PCT CASE, and the reason gate 1 is worth a line at
			// all: context_pct_ts <= boot_ts, so actionableContextPct returns
			// nil and NOTHING happens — while the cockpit (foldActorRuntime,
			// raw gauge read) still renders 90. Two numbers, one actor.
			name: "quiet — stale pct the cockpit still shows", id: "ow-g1s",
			pct: 90, pctTSOff: -60_000, bootTSOff: -50_000, online: true,
			wantGate: "no-actionable-pct",
			// 🔴 THE ORDER OF THESE TWO IS THE WHOLE CASE: pct_ts (1940000) is
			// OLDER than boot_ts (1950000), which is why the gate is shut. A
			// line that printed them the other way round would tell the reader
			// the report is fresh and the gate is broken.
			wantSubstring: []string{"pct=90 ", "pct_ts=1940000 ", "boot_ts=1950000 "},
		},
		{
			// Over the handover threshold but seconds old → the loop-guard.
			name: "quiet — boot-storm loop-guard", id: "ow-g2",
			pct: 90, pctTSOff: -1, bootTSOff: -10, online: true,
			wantGate: "boot-storm",
			wantSubstring: []string{"pct=90 ", "pct_ts=1999999 ", "boot_ts=1999990 ",
				"boot_secs=10.0", "online=true"},
		},
		{
			// Over the threshold, old enough, but no live session.
			name: "quiet — no live session", id: "ow-g3",
			pct: 90, pctTSOff: -5, bootTSOff: -50_000, online: false,
			wantGate: "offline",
			wantSubstring: []string{"pct=90 ", "pct_ts=1999995 ", "boot_ts=1950000 ",
				"online=false"},
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

// TestContextGateDiag_AChangeOfGateIsNotMadeToWait_T72dd: the window is per
// actor, but a DIFFERENT gate speaks at once.
//
// 🔴 THE TRANSITION IS THE INFORMATIVE MOMENT. An actor crosses from "there is
// no actionable pct" to "it is over the line but there is no session" once, and
// that tick is the one a reader is hunting for. Making it serve out the
// remaining 290 s of the previous gate's window would suppress exactly it.
// Steady state is unaffected: a settled actor keeps taking the same gate, so it
// still speaks once per window.
func TestContextGateDiag_AChangeOfGateIsNotMadeToWait_T72dd(t *testing.T) {
	s := newWorkerTestServer(t)
	connectWarden(t, s, ServerSelfHost)
	now := 2_000_000.0
	// ONLINE on purpose: an offline worker is re-STARTed by the FSM, and a START
	// is a session boundary that resets the window on its own — which would make
	// this test pass without the gate-change rule ever being consulted.
	// boot_ts is recent so the SECOND tick lands on the boot-storm guard.
	s.outsourceMu.Lock()
	gateDiagWorker(t, s, "ow-turn", 10, now-5, now-10, true)
	s.outsourceMu.Unlock()

	out := captureStderr(t, func() { s.runOutsourceTick(now) })
	lines := gateLinesFor(out, "ow-turn")
	if len(lines) != 1 || !strings.Contains(lines[0], "gate=no-actionable-pct ") {
		t.Fatalf("first tick must name gate 1, got %v", lines)
	}

	// 30 s later — deep inside the window — the report crosses the handover
	// threshold, so the actor falls through to the boot-storm guard instead.
	s.gauge.Set("ow-turn", map[string]any{
		"context_pct": 90.0, "context_pct_ts": now + 25, "boot_ts": now - 10,
	})
	out = captureStderr(t, func() { s.runOutsourceTick(now + 30) })
	lines = gateLinesFor(out, "ow-turn")
	if len(lines) != 1 {
		t.Fatalf("a change of gate must speak immediately, got %d lines:\n%s",
			len(lines), out)
	}
	if !strings.Contains(lines[0], "gate=boot-storm ") {
		t.Fatalf("the new line must name the NEW gate, got %q", lines[0])
	}

	// …and the new gate then owns the window like any other.
	out = captureStderr(t, func() { s.runOutsourceTick(now + 60) })
	if n := len(gateLinesFor(out, "ow-turn")); n != 0 {
		t.Fatalf("the new gate must then be throttled like any other, got %d:\n%s", n, out)
	}
}

// TestContextGateDiag_SessionBoundaryResetsTheWindow_T72dd pins the prune added
// in clearSessionBootTS: the throttle cell is session-scoped state, dropped on
// the boundary exactly like the handover-notice claim beside it. Without the
// prune the map is a per-actor-id leak for the life of the process (worker ids
// are minted per task), and a brand-new session inherits its predecessor's
// silence.
func TestContextGateDiag_SessionBoundaryResetsTheWindow_T72dd(t *testing.T) {
	s := newWorkerTestServer(t)
	connectWarden(t, s, ServerSelfHost)
	now := 2_000_000.0
	// ONLINE so the FSM leaves it alone and the ONLY boundary is the explicit
	// one below.
	s.outsourceMu.Lock()
	gateDiagWorker(t, s, "ow-bnd", 10, now-5, now-50_000, true)
	s.outsourceMu.Unlock()

	out := captureStderr(t, func() { s.runOutsourceTick(now) })
	if n := len(gateLinesFor(out, "ow-bnd")); n != 1 {
		t.Fatalf("first tick must speak, got %d", n)
	}
	// Same gate, 30 s later, no boundary → silent (the window works).
	out = captureStderr(t, func() { s.runOutsourceTick(now + 30) })
	if n := len(gateLinesFor(out, "ow-bnd")); n != 0 {
		t.Fatalf("inside the window and on the same gate it must stay quiet, got %d", n)
	}

	// A session boundary (a START dispatch / kill calls this).
	s.clearSessionBootTS("ow-bnd")

	out = captureStderr(t, func() { s.runOutsourceTick(now + 60) })
	if n := len(gateLinesFor(out, "ow-bnd")); n != 1 {
		t.Fatalf("a NEW session must be described again rather than inheriting "+
			"its predecessor's window, got %d lines:\n%s", n, out)
	}
	// The cell is gone from the map, not merely stale — this is the leak half.
	s.clearSessionBootTS("ow-bnd")
	s.ctxGateDiagMu.Lock()
	_, still := s.ctxGateDiagAt["ow-bnd"]
	s.ctxGateDiagMu.Unlock()
	if still {
		t.Fatal("the boundary must REMOVE the cell, not just age it")
	}
}

// TestContextGateDiag_RespawnLoopStaysBounded_T72dd is the cost side of that
// prune, measured rather than assumed.
//
// 🔴 A START DISPATCH IS ITSELF A SESSION BOUNDARY, so an OFFLINE worker the FSM
// keeps re-STARTing resets its own window — it speaks once per START rather than
// once per window. That is deliberate (each START really is a new session, and
// the first description of a new session is the interesting one) but it is NOT
// free, so the rate is pinned here: bounded by the FSM's START pacing, NOT by
// the tick. If someone later makes the respawn path dispatch per tick, this test
// is what says the log just got 10x noisier.
func TestContextGateDiag_RespawnLoopStaysBounded_T72dd(t *testing.T) {
	s := newWorkerTestServer(t)
	connectWarden(t, s, ServerSelfHost)
	now := 2_000_000.0
	s.outsourceMu.Lock()
	gateDiagWorker(t, s, "ow-loop", 10, now-5, now-50_000, false)
	s.outsourceMu.Unlock()

	out := captureStderr(t, func() {
		for i := 0; i < 10; i++ { // ten 30 s ticks = five minutes
			s.runOutsourceTick(now + float64(i)*30.0)
		}
	})
	n := len(gateLinesFor(out, "ow-loop"))
	if n < 1 {
		t.Fatalf("a worker being respawned must still be described, got %d", n)
	}
	if n > 4 {
		t.Fatalf("the respawn loop must stay bounded by the START pacing, not "+
			"emit per tick — got %d lines over 10 ticks:\n%s", n, out)
	}
	t.Logf("offline/respawning actor: %d lines over 10 ticks (online twin: 1)", n)
}

// TestContextGateDiag_AnActorThatPassesEveryGateSaysNothing_T72dd is the other
// direction, and without it the line means less than it looks: "gate skip X"
// has to mean X was ACTUALLY skipped. An independent review pointed out that no
// test held this end down (MUT-D) — a diagnostic that also fired on actors the
// pass acted on would be worse than none.
func TestContextGateDiag_AnActorThatPassesEveryGateSaysNothing_T72dd(t *testing.T) {
	s := newWorkerTestServer(t)
	connectWarden(t, s, ServerSelfHost)
	now := 2_000_000.0
	cfg := s.ctxHighConfig()
	s.outsourceMu.Lock()
	gateDiagWorker(t, s, "ow-pass", float64(cfg.HandoverPct)+1, now-5, now-50_000, true)
	s.outsourceMu.Unlock()

	out := captureStderr(t, func() { s.runOutsourceTick(now) })
	if n := len(gateLinesFor(out, "ow-pass")); n != 0 {
		t.Fatalf("an actor the pass ACTED on must not be reported as skipped, "+
			"got %d lines:\n%s", n, out)
	}
	// …and it really did act (otherwise the assertion above is vacuous).
	got, err := s.dal.GetOutsourceWorker("ow-pass")
	if err != nil || got == nil {
		t.Fatalf("read back: %v", err)
	}
	if got.RefocusSince <= 0 {
		t.Fatal("precondition: this actor must actually reach the stamp")
	}
}
