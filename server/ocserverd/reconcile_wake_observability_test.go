package main

import (
	"strings"
	"testing"
)

// T-ba62 — "the wake failed" and "nobody ever woke it" must stop looking the
// same.
//
// waking_since had exactly ONE writer that ever set it: the agent's own
// report_waking. So it was stamped only by agents that successfully booted. An
// agent that never came up left it at zero, PresenceState projected plain
// "offline", and the cockpit rendered a member that was actively failing to
// start identically to one nobody had ever touched. The server KNOWS the
// difference — it dispatched the START — it just never wrote it down.

// A LANDED START stamps waking_since, so the member reads "waking" for the
// WakingTTLSecs window instead of a silent "offline".
func TestReconcile_LandedStartStampsWakingSince(t *testing.T) {
	s := newReconcileTestServer(t)
	putWarden(t, s, "mach-live")
	connectOnline(t, s, "mach-live")

	m := testAgent("m-boot")
	m.DesiredMachineID = "mach-live"
	m.WakingSince = 0
	putTestMember(t, s, m)

	dec := s.reconcileMemberNow("m-boot")
	if dec.Command != reconcileCmdStart {
		t.Fatalf("expected a START decision, got %q (%s)", dec.Command, dec.Reason)
	}
	got, err := s.dal.GetMember("m-boot")
	if err != nil || got == nil {
		t.Fatalf("reload member: %v", err)
	}
	if got.WakingSince <= 0 {
		t.Fatalf("a landed START must stamp waking_since; got %v", got.WakingSince)
	}
	if p := PresenceState(*got, got.WakingSince+1, false); p != MemberPresenceWaking {
		t.Fatalf("a freshly dispatched member must project waking, got %q", p)
	}
}

// The discriminating half: an UNDISPATCHED START (no reachable warden) must NOT
// stamp waking_since. Without this, the stamp would be a lie in the exact case
// the ticket cares about — nothing was sent, so nothing is waking.
func TestReconcile_UnlandedStartDoesNotStampWakingSince(t *testing.T) {
	s := newReconcileTestServer(t)
	putWarden(t, s, "mach-dead") // in the roster, never connected

	m := testAgent("m-boot")
	m.DesiredMachineID = "mach-dead"
	m.WakingSince = 0
	putTestMember(t, s, m)

	dec := s.reconcileMemberNow("m-boot")
	if !dec.DispatchUnlanded {
		t.Fatalf("expected an unlanded dispatch; got %+v", dec)
	}
	got, _ := s.dal.GetMember("m-boot")
	if got.WakingSince != 0 {
		t.Fatalf("an UNDISPATCHED start must not claim the member is waking; got %v", got.WakingSince)
	}
}

// A START that lapses its start_timeout writes a durable last_op receipt — the
// thing the cockpit's 「最近操作」 reads. Previously the lapse existed only as
// exponential backoff inside in-memory reconcile state and one stderr line.
func TestReconcile_StartTimeoutWritesReceipt(t *testing.T) {
	s := newReconcileTestServer(t)
	putWarden(t, s, "mach-live")
	connectOnline(t, s, "mach-live")

	m := testAgent("m-boot")
	m.DesiredMachineID = "mach-live"
	putTestMember(t, s, m)

	// tick 1: dispatch the START.
	now := nowSecs()
	s.reconcileMu.Lock()
	first := s.reconcileTickMemberLocked(m, now)
	s.reconcileMu.Unlock()
	if first.Command != reconcileCmdStart {
		t.Fatalf("expected a START, got %q", first.Command)
	}
	if first.StartTimedOut {
		t.Fatalf("the dispatching tick has not timed out yet")
	}

	// tick 2, past the start window, still not online → the lapse is observed.
	reloaded, _ := s.dal.GetMember("m-boot")
	s.reconcileMu.Lock()
	second := s.reconcileTickMemberLocked(*reloaded, now+s.reconcileCfg.StartTimeout+1)
	s.reconcileMu.Unlock()
	if !second.StartTimedOut {
		t.Fatalf("a lapsed START must be reported as timed out; got %+v", second)
	}

	got, _ := s.dal.GetMember("m-boot")
	if got.LastOp != reconcileCmdStart {
		t.Fatalf("last_op must name the start, got %q", got.LastOp)
	}
	if got.LastOpOK == nil || *got.LastOpOK {
		t.Fatalf("a lapsed START must record last_op_ok=false, got %v", got.LastOpOK)
	}
	// The REASON is the assertion that matters: a receipt with no cause is the
	// same silence in a different shape.
	if !strings.HasPrefix(got.LastOpReason, "wake_timeout:") {
		t.Fatalf("the receipt must carry a wake_timeout reason, got %q", got.LastOpReason)
	}
	for _, want := range []string{"never came online", "claude"} {
		if !strings.Contains(got.LastOpReason, want) {
			t.Errorf("the reason must mention %q so it is actionable, got %q", want, got.LastOpReason)
		}
	}
}

// Positive control for the test above: a member that comes ONLINE inside the
// window must never get a wake_timeout receipt. Without this pair, a mutant that
// stamps the receipt unconditionally would still be green.
func TestReconcile_OnlineMemberGetsNoTimeoutReceipt(t *testing.T) {
	s := newReconcileTestServer(t)
	putWarden(t, s, "mach-live")
	connectOnline(t, s, "mach-live")

	m := testAgent("m-boot")
	m.DesiredMachineID = "mach-live"
	putTestMember(t, s, m)

	now := nowSecs()
	s.reconcileMu.Lock()
	s.reconcileTickMemberLocked(m, now)
	s.reconcileMu.Unlock()

	connectOnline(t, s, "m-boot") // the agent booted and holds its own SSE

	reloaded, _ := s.dal.GetMember("m-boot")
	s.reconcileMu.Lock()
	dec := s.reconcileTickMemberLocked(*reloaded, now+s.reconcileCfg.StartTimeout+1)
	s.reconcileMu.Unlock()
	if dec.StartTimedOut {
		t.Fatalf("an ONLINE member must never be reported as a timed-out start: %+v", dec)
	}
	got, _ := s.dal.GetMember("m-boot")
	if strings.HasPrefix(got.LastOpReason, "wake_timeout:") {
		t.Fatalf("an online member must not carry a wake_timeout receipt, got %q", got.LastOpReason)
	}
}

// The anti-churn guard on the placement stamp suppresses REPETITION, never news:
// blocked → the machine appears and the START lands → the machine is removed
// again → the second block carries a FRESH last_op_at. Without the clear on the
// landed-start path the second block matches the first and is swallowed, leaving
// the cockpit showing "stalled an hour ago" for a stall happening right now (the
// worker twin is TestNotifyWorkerSpawn_BlockRestampedAfterDispatch).
func TestReconcile_MemberBlockRestampedAfterLandedStart(t *testing.T) {
	s := newReconcileTestServer(t)
	m := testAgent("m-flip")
	m.DesiredMachineID = "mach-flip" // named, but no machine row yet
	putTestMember(t, s, m)

	first := 8000.0
	s.runReconcileTick(first)
	blocked, _ := s.dal.GetMember("m-flip")
	if blocked == nil || !strings.HasPrefix(blocked.LastOpReason, placementReasonUnavailable+":") ||
		blocked.LastOpAt != first {
		t.Fatalf("a pin naming no active machine must stamp a block at %v: %+v", first, blocked)
	}

	// The machine is installed and comes online: the START actually lands.
	putWarden(t, s, "mach-flip")
	connectOnline(t, s, "mach-flip")
	s.runReconcileTick(first + 30)
	frames := drainFrames(t, s, "mach-flip")
	if len(frames) != 1 || frames[0].RPC != reconcileCmdStart ||
		frames[0].Args["member_id"] != "m-flip" {
		t.Fatalf("the machine coming online must dispatch the START: %+v", frames)
	}
	started, _ := s.dal.GetMember("m-flip")
	if started == nil || started.WakingSince <= 0 {
		t.Fatalf("the landed START must stamp waking_since: %+v", started)
	}
	if started.LastOpReason != "" || started.LastOpLog != "" {
		t.Fatalf("a landed START must clear the PLACEMENT block it just resolved, "+
			"got reason=%q log=%q", started.LastOpReason, started.LastOpLog)
	}

	// The agent boots, so the next block starts from a converged state.
	agent := connectOnline(t, s, "m-flip")
	s.runReconcileTick(first + 60)

	// The machine is removed again: the SAME cause, but after a landed START this
	// is news, not repetition.
	s.hub.Disconnect(agent)
	warden, _ := s.dal.GetMember("mach-flip")
	warden.RosterStatus = RosterStatusRemoved
	putTestMember(t, s, *warden)

	third := first + 90
	s.runReconcileTick(third)
	again, _ := s.dal.GetMember("m-flip")
	if again == nil || again.LastOpReason != blocked.LastOpReason {
		t.Fatalf("the second block must carry the same cause: %+v", again)
	}
	if again.LastOpAt != third {
		t.Fatalf("a block AFTER a landed START must re-stamp: last_op_at = %v, want %v "+
			"(keeping the first block's %v means the start never cleared it)",
			again.LastOpAt, third, blocked.LastOpAt)
	}
}

// The clear on the landed-start path is PLACEMENT-scoped: the wake_timeout
// receipt this same function writes explains why an agent will not boot, and the
// wake retry that follows it must not erase its own explanation.
func TestReconcile_WakeTimeoutReceiptSurvivesLandedStart(t *testing.T) {
	s := newReconcileTestServer(t)
	putWarden(t, s, "mach-live")
	connectOnline(t, s, "mach-live")

	m := testAgent("m-boot")
	m.DesiredMachineID = "mach-live"
	m.LastOpLog = "warden accepted the start frame"
	putTestMember(t, s, m)

	now := nowSecs()
	s.reconcileMu.Lock()
	s.reconcileTickMemberLocked(m, now)
	s.reconcileMu.Unlock()

	lapsed := now + s.reconcileCfg.StartTimeout + 1
	reloaded, _ := s.dal.GetMember("m-boot")
	s.reconcileMu.Lock()
	timedOut := s.reconcileTickMemberLocked(*reloaded, lapsed)
	s.reconcileMu.Unlock()
	if !timedOut.StartTimedOut {
		t.Fatalf("the lapsed START must be observed as timed out: %+v", timedOut)
	}
	receipt, _ := s.dal.GetMember("m-boot")
	if !strings.HasPrefix(receipt.LastOpReason, "wake_timeout:") {
		t.Fatalf("expected a wake_timeout receipt on the row, got %q", receipt.LastOpReason)
	}

	// Past the backoff the wake is retried, and this START lands.
	retry := lapsed + 10
	s.reconcileMu.Lock()
	dec := s.reconcileTickMemberLocked(*receipt, retry)
	s.reconcileMu.Unlock()
	if dec.Command != reconcileCmdStart {
		t.Fatalf("expected the wake retry to dispatch a START, got %q (%s)", dec.Command, dec.Reason)
	}
	got, _ := s.dal.GetMember("m-boot")
	if got.WakingSince != retry {
		t.Fatalf("the retried START must stamp waking_since=%v, got %v", retry, got.WakingSince)
	}
	if got.LastOpReason != receipt.LastOpReason || got.LastOpLog != receipt.LastOpLog {
		t.Fatalf("the wake_timeout receipt must survive the retry that follows it: "+
			"reason %q → %q, log %q → %q",
			receipt.LastOpReason, got.LastOpReason, receipt.LastOpLog, got.LastOpLog)
	}
}

// The same guard from the other side: a receipt this function never wrote — a
// warden's own refused-start result, folded onto the row by foldCommandResult —
// is not the server's to erase either.
func TestReconcile_LandedStartKeepsWardenRefusalReceipt(t *testing.T) {
	s := newReconcileTestServer(t)
	putWarden(t, s, "mach-live")
	connectOnline(t, s, "mach-live")

	m := testAgent("m-boot")
	m.DesiredMachineID = "mach-live"
	m.LastOp = reconcileCmdStart
	refused := false
	m.LastOpOK = &refused
	m.LastOpReason = "session_already_exists: tmux session member-m-boot is already running"
	m.LastOpLog = "warden: refused to spawn over a live session"
	putTestMember(t, s, m)

	dec := s.reconcileMemberNow("m-boot")
	if dec.Command != reconcileCmdStart {
		t.Fatalf("expected a START decision, got %q (%s)", dec.Command, dec.Reason)
	}
	got, _ := s.dal.GetMember("m-boot")
	if got.WakingSince <= 0 {
		t.Fatalf("the landed START must stamp waking_since; got %v", got.WakingSince)
	}
	if got.LastOpReason != m.LastOpReason || got.LastOpLog != m.LastOpLog {
		t.Fatalf("a warden's refused-start receipt must survive the retry: "+
			"reason %q → %q, log %q → %q",
			m.LastOpReason, got.LastOpReason, m.LastOpLog, got.LastOpLog)
	}
}

// The stamp is a whole-row write on the TICK'S SNAPSHOT, so it re-reads first:
// the HTTP faces write member rows without holding reconcileMu, and persisting
// the snapshot would silently revert a relocate that landed mid-tick — on
// desired_machine_id, the field the placement work is about.
func TestReconcile_WakeStampPreservesConcurrentRelocate(t *testing.T) {
	s := newReconcileTestServer(t)
	for _, mach := range []string{"mach-a", "mach-b"} {
		putWarden(t, s, mach)
		connectOnline(t, s, mach)
	}

	m := testAgent("m-move")
	m.DesiredMachineID = "mach-a"
	putTestMember(t, s, m)
	stale := m // the snapshot the cadence tick loaded

	// The relocate face repins the member while the tick is in flight.
	moved, _ := s.dal.GetMember("m-move")
	moved.DesiredMachineID = "mach-b"
	putTestMember(t, s, *moved)

	now := nowSecs()
	s.reconcileMu.Lock()
	dec := s.reconcileTickMemberLocked(stale, now)
	s.reconcileMu.Unlock()
	if dec.Command != reconcileCmdStart {
		t.Fatalf("expected a START decision, got %q (%s)", dec.Command, dec.Reason)
	}
	got, _ := s.dal.GetMember("m-move")
	if got.WakingSince != now {
		t.Fatalf("the landed START must stamp waking_since=%v, got %v", now, got.WakingSince)
	}
	if got.DesiredMachineID != "mach-b" {
		t.Fatalf("the stamp must not revert a relocate that landed mid-tick: "+
			"desired_machine_id = %q, want %q", got.DesiredMachineID, "mach-b")
	}
}

// A member dismissed after the tick snapshotted it is GONE: the stamp must not
// resurrect its row. m-live is the control — the same tick still stamps a member
// that is still on the roster.
func TestReconcile_WakeStampSkipsMemberRemovedMidTick(t *testing.T) {
	s := newReconcileTestServer(t)
	putWarden(t, s, "mach-live")
	connectOnline(t, s, "mach-live")

	live := testAgent("m-live")
	live.DesiredMachineID = "mach-live"
	putTestMember(t, s, live)
	gone := testAgent("m-gone")
	gone.DesiredMachineID = "mach-live"
	putTestMember(t, s, gone)

	dismissed, _ := s.dal.GetMember("m-gone")
	dismissed.RosterStatus = RosterStatusRemoved
	putTestMember(t, s, *dismissed)

	now := nowSecs()
	s.reconcileMu.Lock()
	liveDec := s.reconcileTickMemberLocked(live, now)
	goneDec := s.reconcileTickMemberLocked(gone, now)
	s.reconcileMu.Unlock()
	if liveDec.Command != reconcileCmdStart || goneDec.Command != reconcileCmdStart {
		t.Fatalf("both snapshots must decide a START: live=%q gone=%q",
			liveDec.Command, goneDec.Command)
	}

	stillHere, _ := s.dal.GetMember("m-live")
	if stillHere.WakingSince != now {
		t.Fatalf("a roster-active member must still be stamped: waking_since = %v, want %v",
			stillHere.WakingSince, now)
	}
	removed, _ := s.dal.GetMember("m-gone")
	if removed.RosterStatus != RosterStatusRemoved || removed.WakingSince != 0 {
		t.Fatalf("a member removed after the snapshot must not be written back: %+v", removed)
	}
}

// ── T-b3d0 follow-up: the LAST sentence must not be a claude-only dead end ──

// lapseAStartOn dispatches a START for a claude member pinned to machineID and
// then lets the start window lapse, returning the row as the owner would read
// it on 「最近操作」.
func lapseAStartOn(t *testing.T, s *apiServer, machineID, runtimes string) Member {
	t.Helper()
	putWarden(t, s, machineID)
	connectOnline(t, s, machineID)
	if rec := doIngestTelemetry(s, machineID, machineID, runtimes); rec.Code != 200 {
		t.Fatalf("telemetry ingest for %s: %d %s", machineID, rec.Code, rec.Body.String())
	}

	m := testAgent("m-boot-" + machineID)
	m.DesiredMachineID = machineID
	m.Runtime = RuntimeClaude
	putTestMember(t, s, m)

	now := nowSecs()
	s.reconcileMu.Lock()
	dispatched := s.reconcileTickMemberLocked(m, now)
	s.reconcileMu.Unlock()
	if dispatched.Command != reconcileCmdStart {
		t.Fatalf("%s: want a dispatched START, got %q (%s)",
			machineID, dispatched.Command, dispatched.Reason)
	}

	reloaded, _ := s.dal.GetMember(m.ID)
	s.reconcileMu.Lock()
	lapsed := s.reconcileTickMemberLocked(*reloaded, now+s.reconcileCfg.StartTimeout+1)
	s.reconcileMu.Unlock()
	if !lapsed.StartTimedOut {
		t.Fatalf("%s: the START must lapse its window, got %+v", machineID, lapsed)
	}
	got, err := s.dal.GetMember(m.ID)
	if err != nil || got == nil {
		t.Fatalf("reload %s: %v", m.ID, err)
	}
	return *got
}

// The ticket's hardest AC, at the exit T-b3d0 missed. The spawn-time refusal
// already names the third exit (switch this member to Codex) — and then this
// stamp overwrites it the moment the window lapses. On a machine that MEASURED
// no claude, the sentence the owner is left holding must name that exit too.
//
// The two halves below are the same code path fed two machines that differ only
// in what they reported, and they must not produce the same sentence:
//
//	mach-codex-only → {"claude":{"installed":false},"codex": ready}
//	mach-has-claude → both installed and logged in
//
// MUTANT: restore the old single sentence (delete the codex-only arm of
// wakeTimeoutReason) → the codex-only half goes RED on the whole-string compare
// and the has-claude half stays green.
func TestReconcile_WakeTimeoutNamesTheRuntimeSwitchOnACodexOnlyMachine(t *testing.T) {
	s := newReconcileTestServer(t)

	codexOnly := lapseAStartOn(t, s, "mach-codex-only", codexOnlyRuntimes)
	hasClaude := lapseAStartOn(t, s, "mach-has-claude", bothRuntimes)

	wantCodexOnly := "wake_timeout: the START was dispatched but the agent never came " +
		"online within the start window — machine 'mach-codex-only' reports no Claude " +
		"Code installed, so this member cannot boot there. Fix any one: set this " +
		"member's 執行環境 to Codex (that machine has it ready); or install Claude Code " +
		"on that machine (warden log: ocwarden.err.log)"
	if codexOnly.LastOpReason != wantCodexOnly {
		t.Errorf("a machine that reported NO claude must leave the owner a sentence "+
			"naming the runtime switch.\n got: %q\nwant: %q",
			codexOnly.LastOpReason, wantCodexOnly)
	}

	// The other world, and the reason the arm is conditional: this machine HAS
	// claude, so sending its owner off to change the member's 執行環境 would be an
	// active misdirection. It keeps today's sentence, verbatim.
	wantHasClaude := "wake_timeout: the START was dispatched but the agent never came " +
		"online within the start window — check that claude runs and is logged in on " +
		"the target machine (warden log: ocwarden.err.log)"
	if hasClaude.LastOpReason != wantHasClaude {
		t.Errorf("a machine that HAS claude must keep the machine-side advice.\n got: %q\nwant: %q",
			hasClaude.LastOpReason, wantHasClaude)
	}

	// The fixture discriminates only if the two really diverge — an assertion
	// pair that cannot tell the two worlds apart is the failure mode this guard
	// exists to avoid.
	if codexOnly.LastOpReason == hasClaude.LastOpReason {
		t.Fatalf("the two machines produced the SAME sentence (%q) — the fixture "+
			"cannot tell a codex-only box from one that has claude", codexOnly.LastOpReason)
	}
}

// A machine that has never reported capabilities is not evidence of anything.
// The stamp must keep today's sentence rather than guess at a runtime switch
// nobody has shown is available — the same permissive-on-unknown rule T-b3d0
// adopted for resolution.
func TestReconcile_WakeTimeoutKeepsTodaysSentenceWithoutCapabilities(t *testing.T) {
	s := newReconcileTestServer(t)
	putWarden(t, s, "mach-quiet")
	connectOnline(t, s, "mach-quiet")

	m := testAgent("m-quiet")
	m.DesiredMachineID = "mach-quiet"
	putTestMember(t, s, m)

	now := nowSecs()
	s.reconcileMu.Lock()
	s.reconcileTickMemberLocked(m, now)
	s.reconcileMu.Unlock()
	reloaded, _ := s.dal.GetMember("m-quiet")
	s.reconcileMu.Lock()
	s.reconcileTickMemberLocked(*reloaded, now+s.reconcileCfg.StartTimeout+1)
	s.reconcileMu.Unlock()

	got, _ := s.dal.GetMember("m-quiet")
	want := "wake_timeout: the START was dispatched but the agent never came online " +
		"within the start window — check that claude runs and is logged in on the " +
		"target machine (warden log: ocwarden.err.log)"
	if got.LastOpReason != want {
		t.Fatalf("an unreported machine must not be read as a measurement.\n got: %q\nwant: %q",
			got.LastOpReason, want)
	}
}

// This is a MESSAGE change and nothing else: the receipt's verb, ok flag,
// timestamp and the waking anchor it clears are untouched on the new arm, so a
// wake that fails today still fails the same way — no new gate, nothing that
// used to succeed now blocked.
func TestReconcile_CodexOnlyWakeTimeoutChangesOnlyTheSentence(t *testing.T) {
	s := newReconcileTestServer(t)
	got := lapseAStartOn(t, s, "mach-codex-only", codexOnlyRuntimes)

	if got.LastOp != reconcileCmdStart {
		t.Errorf("last_op must still name the start, got %q", got.LastOp)
	}
	if got.LastOpOK == nil || *got.LastOpOK {
		t.Errorf("a lapsed START must still record last_op_ok=false, got %v", got.LastOpOK)
	}
	if got.WakingSince != 0 {
		t.Errorf("the stale waking anchor must still be cleared, got %v", got.WakingSince)
	}
}
