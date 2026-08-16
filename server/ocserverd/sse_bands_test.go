package main

import (
	"encoding/json"
	"strings"
	"testing"
)

func fptr(v float64) *float64 { return &v }

func TestBandFor(t *testing.T) {
	cases := []struct {
		name     string
		pct      *float64
		handover int
		want     string
	}{
		{"nil pct fails safe to none", nil, 50, levelNone},
		{"below handover", fptr(49), 50, levelNone},
		{"at handover", fptr(50), 50, levelHandover},
		{"above handover", fptr(99), 50, levelHandover},
		{"threshold <= 0 disables the band", fptr(99), 0, levelNone},
	}
	for _, c := range cases {
		if got := bandFor(c.pct, c.handover); got != c.want {
			t.Errorf("%s: got %q want %q", c.name, got, c.want)
		}
	}
}

// TestClaudeNoticePct_TracksTheOwnersThreshold is the CORE regression of T-c382.
// The advance notice used to sit on its own hard-wired 40 with no UI, so an
// owner moving the handover threshold moved the handover and nothing else. Every
// assertion here is an ABSOLUTE number on purpose: an assertion written against
// the constant would stay green through exactly the drift being guarded.
func TestClaudeNoticePct_TracksTheOwnersThreshold(t *testing.T) {
	// The owner's own worked example, verbatim: 「例如 65% 的話會從 55% 開始通知」.
	if got, ok := claudeNoticePct(65); !ok || got != 55 {
		t.Fatalf("handover 65 must notify at 55, got %d (ok=%v)", got, ok)
	}
	// Move the threshold: the notice MUST move with it. This is the pair that
	// dies if anyone re-hardwires the notice point.
	if got, ok := claudeNoticePct(90); !ok || got != 80 {
		t.Fatalf("handover 90 must notify at 80, got %d (ok=%v)", got, ok)
	}
	if got, ok := claudeNoticePct(40); !ok || got != 30 {
		t.Fatalf("handover 40 (the UI minimum) must notify at 30, got %d (ok=%v)", got, ok)
	}
	// Kill-switch and degenerate leads produce NO notice rather than one that
	// fires on a barely-used gauge.
	if _, ok := claudeNoticePct(0); ok {
		t.Fatal("a disabled handover band must produce no advance notice")
	}
	if _, ok := claudeNoticePct(handoverNoticeLeadPct); ok {
		t.Fatal("a lead that lands the notice at 0% must produce no notice")
	}
}

// TestCodexNoticeDue pins the OTHER runtime's axis. A codex session hands over
// on compaction count, so a percentage threshold means nothing to it — that was
// the second half of the bug (a codex worker was being warned at 40% of a gauge
// that does not decide anything for it).
func TestCodexNoticeDue(t *testing.T) {
	rec := func(count int) map[string]any { return map[string]any{"compaction_count": count} }
	// Owner's worked example, verbatim: 「例如我設定是 5 那就是在第四輪的 60% 提醒
	// 一次」 — threshold 5 ⇒ round 4 (count == 4) at >= 60%.
	if !codexNoticeDue(rec(4), fptr(60), 5) {
		t.Fatal("threshold 5: round 4 at 60% must be due")
	}
	if codexNoticeDue(rec(4), fptr(59), 5) {
		t.Fatal("threshold 5: round 4 below 60% is not due yet")
	}
	// NOT the round before, and not the handover round itself: one notice, on
	// one round. Firing on the final round would arrive after the decision.
	if codexNoticeDue(rec(3), fptr(99), 5) {
		t.Fatal("two rounds out must stay quiet")
	}
	if codexNoticeDue(rec(5), fptr(99), 5) {
		t.Fatal("the handover round itself must not carry the ADVANCE notice")
	}
	// It really is the owner's threshold, not a constant: change it and the
	// notice round changes with it.
	if !codexNoticeDue(rec(1), fptr(80), 2) {
		t.Fatal("threshold 2 must notify on round 1")
	}
	if codexNoticeDue(rec(4), fptr(80), 2) {
		t.Fatal("threshold 2 must not notify on round 4")
	}
	// Fail-safe inputs.
	if codexNoticeDue(map[string]any{}, fptr(99), 5) {
		t.Fatal("a gauge with no compaction_count must fail safe to quiet")
	}
	if codexNoticeDue(rec(4), nil, 5) {
		t.Fatal("no actionable pct must fail safe to quiet")
	}
}

func TestActionableContextPct(t *testing.T) {
	fresh := map[string]any{"context_pct": 45.0, "context_pct_ts": 20.0, "boot_ts": 10.0}
	if got := actionableContextPct(fresh, true); got == nil || *got != 45.0 {
		t.Fatalf("fresh pct must be actionable: %v", got)
	}
	stale := map[string]any{"context_pct": 45.0, "context_pct_ts": 5.0, "boot_ts": 10.0}
	if got := actionableContextPct(stale, true); got != nil {
		t.Fatalf("a pct reported at/before boot_ts is stale: %v", *got)
	}
	if got := actionableContextPct(stale, false); got == nil || *got != 45.0 {
		t.Fatal("stale_guard=false reverts to always-use-pct")
	}
	noAnchor := map[string]any{"context_pct": 45.0}
	if actionableContextPct(noAnchor, true) != nil {
		t.Fatal("missing freshness anchors must fail safe to nil")
	}
	if actionableContextPct(nil, true) != nil {
		t.Fatal("missing record must fail safe to nil")
	}
}

func TestDecideHandoverNotice(t *testing.T) {
	cfg := defaultSseContextHigh()
	cfg.HandoverPct = 65 // the owner's own setting, and his worked example
	rec := func(pct float64, extra map[string]any) map[string]any {
		r := map[string]any{"context_pct": pct, "context_pct_ts": 20.0, "boot_ts": 10.0}
		for k, v := range extra {
			r[k] = v
		}
		return r
	}

	t.Run("claude fires at the derived point, not before", func(t *testing.T) {
		if sig := decideHandoverNotice("m-1", RuntimeClaude, rec(54, nil), cfg, 5); sig != nil {
			t.Fatalf("54%% is below the 55%% notice point: %+v", sig)
		}
		sig := decideHandoverNotice("m-1", RuntimeClaude, rec(55, nil), cfg, 5)
		if sig == nil {
			t.Fatal("55%% with handover 65%% must notify")
		}
		if sig.Topic != "context-high" || sig.To != "m-1" || sig.Level != "warn" ||
			float64(sig.Pct) != 55.0 {
			t.Fatalf("signal envelope: %+v", sig)
		}
	})

	t.Run("the notice says what the ceiling is and what to do", func(t *testing.T) {
		sig := decideHandoverNotice("m-1", RuntimeClaude, rec(55, nil), cfg, 5)
		if sig == nil {
			t.Fatal("expected a notice")
		}
		// The ceiling, in the message. An agent cannot read its own context %,
		// so a notice that omits the number leaves it unable to pace itself.
		if !strings.Contains(sig.Reason, "65%") {
			t.Fatalf("the notice must name the ceiling: %q", sig.Reason)
		}
		// Owner 2026-08-16, the three things he requires done before a handover.
		// Asserted individually so dropping ONE is red — a single "is it
		// non-empty" check would pass a message that lost two of them.
		for _, want := range []string{"chat", "task", "learning"} {
			if !strings.Contains(strings.ToLower(sig.Reason), want) {
				t.Fatalf("the notice must tell the agent to handle %q: %q", want, sig.Reason)
			}
		}
		// And it must NOT read as "you are being stopped now" — the whole point
		// of an advance notice is that there is still room to work.
		if !strings.Contains(sig.Reason, "not being stopped yet") {
			t.Fatalf("the notice must say the agent is not being stopped yet: %q", sig.Reason)
		}
	})

	t.Run("codex is judged on rounds, never on the claude percentage", func(t *testing.T) {
		// 55% would fire for claude. For codex on round 2 of 5 it must not:
		// its lifecycle has nothing to do with that number.
		quiet := rec(55, map[string]any{"compaction_count": 2})
		if sig := decideHandoverNotice("w-1", RuntimeCodex, quiet, cfg, 5); sig != nil {
			t.Fatalf("codex must not inherit the claude percentage rule: %+v", sig)
		}
		due := rec(60, map[string]any{"compaction_count": 4})
		sig := decideHandoverNotice("w-1", RuntimeCodex, due, cfg, 5)
		if sig == nil {
			t.Fatal("codex round 4 of 5 at 60%% must notify")
		}
		if !strings.Contains(sig.Reason, "compaction round 5") {
			t.Fatalf("a codex notice must name ITS ceiling (rounds), not a pct: %q", sig.Reason)
		}
	})

	t.Run("fails safe when the gauge cannot be trusted", func(t *testing.T) {
		stale := map[string]any{"context_pct": 99.0, "context_pct_ts": 5.0, "boot_ts": 10.0}
		if sig := decideHandoverNotice("m-1", RuntimeClaude, stale, cfg, 5); sig != nil {
			t.Fatalf("a predecessor session's pct must not notify: %+v", sig)
		}
		if sig := decideHandoverNotice("m-1", RuntimeClaude, nil, cfg, 5); sig != nil {
			t.Fatalf("no gauge must not notify: %+v", sig)
		}
		off := cfg
		off.HandoverPct = 0
		if sig := decideHandoverNotice("m-1", RuntimeClaude, rec(99, nil), off, 5); sig != nil {
			t.Fatalf("the kill-switch must silence the notice too: %+v", sig)
		}
	})
}

func TestDecideTokenExpirySignalRepeatsUntilRestart(t *testing.T) {
	const now int64 = 20_000
	claims := map[string]any{"exp": float64(now + tokenExpiryWarningWindow)}
	oldSession := map[string]any{"boot_ts": float64(now - int64(minSelfRestartSecs) - 1)}
	member := &Member{ID: "m-expiry", Kind: KindAssistant}

	signal, last := decideTokenExpirySignal("m-expiry", claims, member, oldSession, now, 0)
	if signal == nil {
		t.Fatal("an eligible agent at the 30-minute boundary must be reminded")
	}
	if signal.Topic != tokenExpiryTopic || signal.To != "m-expiry" ||
		signal.ExpiresIn != tokenExpiryWarningWindow || !strings.Contains(signal.Reason, "restart_self") {
		t.Fatalf("signal = %+v", signal)
	}
	if last != now {
		t.Fatalf("first reminder timestamp = %d, want %d", last, now)
	}
	if signal, repeatedLast := decideTokenExpirySignal(
		"m-expiry", claims, member, oldSession, now+tokenExpiryReminderInterval-1, last); signal != nil || repeatedLast != last {
		t.Fatalf("reminder must stay quiet before cadence: signal=%+v last=%d", signal, repeatedLast)
	}
	if signal, repeatedLast := decideTokenExpirySignal(
		"m-expiry", claims, member, oldSession, now+tokenExpiryReminderInterval, last); signal == nil || repeatedLast != now+tokenExpiryReminderInterval {
		t.Fatalf("unhandled expiry must repeat on cadence: signal=%+v last=%d", signal, repeatedLast)
	}

	far := map[string]any{"exp": float64(now + tokenExpiryWarningWindow + 1)}
	if signal, _ := decideTokenExpirySignal("m-expiry", far, member, oldSession, now, 0); signal != nil {
		t.Fatalf("far-from-expiry token must stay quiet: %+v", signal)
	}
	if got, want := tokenExpiryNextCheck(far, now), now+1; got != want {
		t.Fatalf("far token must recheck at the exact 30-minute boundary: got %d want %d", got, want)
	}
	if got, want := tokenExpiryNextCheck(claims, now), now+tokenExpiryReminderInterval; got != want {
		t.Fatalf("pending token must use reminder cadence: got %d want %d", got, want)
	}
	freshSession := map[string]any{"boot_ts": float64(now - 1)}
	if signal, _ := decideTokenExpirySignal("m-expiry", claims, member, freshSession, now, 0); signal != nil {
		t.Fatalf("notification must wait until restart_self is usable: %+v", signal)
	}
	member.RefocusSince = float64(now - 1)
	if signal, _ := decideTokenExpirySignal("m-expiry", claims, member, oldSession, now, 0); signal != nil {
		t.Fatalf("agent already in handover must not be reminded: %+v", signal)
	}
	member.RefocusSince = 0
	member.Kind = KindWarden
	if signal, _ := decideTokenExpirySignal("m-expiry", claims, member, oldSession, now, 0); signal != nil {
		t.Fatalf("warden must not receive restart_self reminder: %+v", signal)
	}
}

func TestDirectedFrameText(t *testing.T) {
	frame, err := directedFrameText(wardenCommandTopic, wardenCommandFrame{
		RPC:  "start",
		Args: wardenStartArgs{MemberID: "m-1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	text := string(frame)
	if strings.Contains(text, "id: ") || !strings.HasPrefix(text, "data: ") ||
		!strings.HasSuffix(text, "\n\n") {
		t.Fatalf("directed frames are bare data: events with no id line: %q", text)
	}
	var envelope struct {
		Topic string `json:"topic"`
		Data  struct {
			RPC  string         `json:"rpc"`
			Args map[string]any `json:"args"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(strings.TrimPrefix(text, "data: "))), &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Topic != "warden-command" || envelope.Data.RPC != "start" {
		t.Fatalf("envelope: %+v", envelope)
	}
	want := []string{"member_id", "persona_context", "member_token", "role",
		"task_type", "runtime", "model", "effort", "session_name"}
	if len(envelope.Data.Args) != len(want) {
		t.Fatalf("start args keys: %v", envelope.Data.Args)
	}
	for _, k := range want {
		if _, ok := envelope.Data.Args[k]; !ok {
			t.Fatalf("start args missing %q: %v", k, envelope.Data.Args)
		}
	}
}

// ── task-close nudge band (§8) ───────────────────────────────────────────────

func TestDecideTaskCloseNudge(t *testing.T) {
	base := Task{ID: "t-7d40aabbccdd", TypeKey: "review-pr", ExecutorID: "m-exec"}

	// Both terminal statuses nudge (a terminated run's lessons count too).
	for _, status := range []string{TaskStatusDone, TaskStatusTerminated} {
		task := base
		task.Status = status
		sig := decideTaskCloseNudge(task, "審查 PR（review-pr）")
		if sig == nil {
			t.Fatalf("%s must nudge", status)
		}
		if sig.Topic != taskCloseTopic || sig.To != "m-exec" ||
			sig.TaskID != task.ID || sig.TaskNo != "T-7d40" ||
			sig.Type != "review-pr" || sig.Status != status {
			t.Fatalf("%s signal fields: %+v", status, sig)
		}
		if !strings.Contains(sig.Reason, "T-7d40") ||
			!strings.Contains(sig.Reason, "write_task_learnings") {
			t.Fatalf("reason must name the task and the tool: %q", sig.Reason)
		}
		// T-fa76: the sentence shows the display label, but the MCP
		// ADDRESSING string stays the raw type_key.
		if !strings.Contains(sig.Reason, "「審查 PR（review-pr）」") ||
			!strings.Contains(sig.Reason, "type_key=`review-pr`") {
			t.Fatalf("reason must carry display label AND raw key: %q", sig.Reason)
		}
	}

	// A blank label (manual deleted / lookup failed) falls back to the key.
	fallback := base
	fallback.Status = TaskStatusDone
	if sig := decideTaskCloseNudge(fallback, ""); sig == nil ||
		!strings.Contains(sig.Reason, "「review-pr」") {
		t.Fatalf("blank label must fall back to the raw key: %+v", sig)
	}

	// An ad-hoc task (no type) has no manual to write into — never nudges.
	adhoc := base
	adhoc.Status = TaskStatusDone
	adhoc.TypeKey = ""
	if decideTaskCloseNudge(adhoc, "") != nil {
		t.Fatal("ad-hoc close must stay quiet")
	}

	// A non-terminal status never nudges.
	open := base
	open.Status = TaskStatusInProgress
	if decideTaskCloseNudge(open, "") != nil {
		t.Fatal("non-terminal status must stay quiet")
	}

	// An unassigned task has nobody to remind.
	unassigned := base
	unassigned.Status = TaskStatusTerminated
	unassigned.ExecutorID = ""
	if decideTaskCloseNudge(unassigned, "") != nil {
		t.Fatal("unassigned close must stay quiet")
	}
}

func TestTaskCloseFrameIsABareDirectedEvent(t *testing.T) {
	task := Task{ID: "t-7d40aabbccdd", TypeKey: "review-pr",
		ExecutorID: "m-exec", Status: TaskStatusDone}
	frame, err := directedFrameText(taskCloseTopic, decideTaskCloseNudge(task, ""))
	if err != nil {
		t.Fatal(err)
	}
	text := string(frame)
	if strings.Contains(text, "id: ") || !strings.HasPrefix(text, "data: ") {
		t.Fatalf("directed frames are bare data: events with no id line: %q", text)
	}
	var envelope struct {
		Topic string          `json:"topic"`
		Data  taskCloseSignal `json:"data"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(strings.TrimPrefix(text, "data: "))), &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Topic != "task-close" || envelope.Data.To != "m-exec" ||
		envelope.Data.TaskID != task.ID || envelope.Data.Status != "done" {
		t.Fatalf("envelope: %+v", envelope)
	}
}
