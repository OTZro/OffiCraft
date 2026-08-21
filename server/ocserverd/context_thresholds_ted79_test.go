package main

import "testing"

// context_thresholds_ted79_test.go — the two context thresholds open two
// DIFFERENT kinds of wind-down, and the first one can become the second.
//
// The first threshold used to open nothing at all: it sent one SSE band and the
// wind-down began only at the second, so an agent that missed that frame met
// the final call with no close-out started and 120 seconds to do it in.
//
// 🔴 The promotion is the part with teeth, and it has two failure modes that
// look like working code:
//
//   - never promoting (the in-flight epoch de-dup swallows the second
//     threshold): the session stays on a stop nothing collects, past the line
//     where the owner wants it collected;
//   - promoting without re-stamping refocus_since: the deadline is computed from
//     the FIRST threshold's stamp, so it is already in the past and the member
//     is collected on the same tick that announced it — a zero-second deadline,
//     which is the thing this ticket exists to remove.

// atPct puts a member online with a gauge reading and runs one auto-stamp pass,
// returning the row as it survives the tick.
func atPct(t *testing.T, s *apiServer, m Member, pct, now float64) Member {
	t.Helper()
	putTestMember(t, s, m)
	s.gauge.Set(m.ID, map[string]any{
		"context_pct": pct, "context_pct_ts": now - 10, "boot_ts": now - 5000,
	})
	members := []Member{m}
	s.stampContextHighRecycle(members, now)
	return members[0]
}

func TestContextThresholds_FirstOpensA停止_SecondOpensThe加速停止(t *testing.T) {
	s := newReconcileTestServer(t)
	cfg := s.ctxHighConfig()
	const now = 1_769_904_000.0 // 2026-02-01T00:00:00Z

	t.Run("first threshold opens a 停止 with no clock", func(t *testing.T) {
		m := testAgent("m-ed79-notice")
		l := connectOnline(t, s, m.ID)
		defer drainListener(l)

		got := atPct(t, s, m, float64(cfg.NoticePct), now)

		if got.RefocusOp != refocusOpContextNotice || got.RefocusSince != now {
			t.Fatalf("first threshold: refocus_op=%q refocus_since=%v, want %q at %v — "+
				"an agent at the notice line must be ASKED to wind down, not left "+
				"to meet the final call with nothing started",
				got.RefocusOp, got.RefocusSince, refocusOpContextNotice, now)
		}
		// …and it is a 停止: no clock, so nothing collects it and the sentence
		// quotes no time.
		obs := obsOf(got.ID, DesiredStateOnline, true)
		obs.RefocusSince = got.RefocusSince
		obs.RefocusOp = got.RefocusOp
		for _, elapsed := range []float64{1, 120, 3600, 365 * 24 * 3600} {
			d := reconcileDecide(obs, newReconcileState(), s.reconcileCfg, now+elapsed)
			if d.Command == reconcileCmdStop {
				t.Fatalf("+%.0fs: the first threshold was collected on a clock (%s)",
					elapsed, d.Reason)
			}
		}
		notice, _ := s.offboardDeltaPayload(got)["offboard_notice"].(string)
		if notice == "" {
			t.Fatal("the first threshold must carry a notice at all")
		}
		assertQuotesNoTime(t, refocusOpContextNotice, notice)
	})

	t.Run("below the first threshold nothing is opened", func(t *testing.T) {
		m := testAgent("m-ed79-below")
		l := connectOnline(t, s, m.ID)
		defer drainListener(l)

		got := atPct(t, s, m, float64(cfg.NoticePct)-1, now)

		if got.RefocusSince != 0 || got.RefocusOp != "" {
			t.Fatalf("a member below the notice line was put into a wind-down: "+
				"op=%q since=%v", got.RefocusOp, got.RefocusSince)
		}
	})

	t.Run("second threshold opens the 加速停止 directly", func(t *testing.T) {
		m := testAgent("m-ed79-high")
		l := connectOnline(t, s, m.ID)
		defer drainListener(l)

		got := atPct(t, s, m, float64(cfg.HandoverPct), now)

		if got.RefocusOp != refocusOpContextHigh || got.RefocusSince != now {
			t.Fatalf("second threshold: refocus_op=%q refocus_since=%v, want %q at %v",
				got.RefocusOp, got.RefocusSince, refocusOpContextHigh, now)
		}
	})
}

// THE PROMOTION, and the re-stamp that keeps it from being a zero-second
// deadline.
func TestContextThresholds_NoticeIsPromotedToTheAcceleratedStop(t *testing.T) {
	s := newReconcileTestServer(t)
	cfg := s.ctxHighConfig()
	const (
		noticeAt = 1_769_904_000.0
		// Long enough that a deadline measured from the FIRST stamp is not just
		// wrong but already spent several times over.
		promoteAt = noticeAt + 900
	)

	m := testAgent("m-ed79-promote")
	l := connectOnline(t, s, m.ID)
	defer drainListener(l)

	first := atPct(t, s, m, float64(cfg.NoticePct), noticeAt)
	if first.RefocusOp != refocusOpContextNotice {
		t.Fatalf("fixture: the first pass must open the notice epoch, got %q", first.RefocusOp)
	}

	// The agent has started its close-out — it reported stopping, which the
	// promotion must not erase.
	first.StoppingSince = noticeAt + 60
	promoted := atPct(t, s, first, float64(cfg.HandoverPct), promoteAt)

	if promoted.RefocusOp != refocusOpContextHigh {
		t.Fatalf("refocus_op = %q, want %q — the de-dup on an in-flight epoch "+
			"swallowed the second threshold, so the session stays on a stop "+
			"nothing collects", promoted.RefocusOp, refocusOpContextHigh)
	}
	if promoted.RefocusSince != promoteAt {
		t.Fatalf("refocus_since = %v, want %v — promoting without re-stamping "+
			"leaves the deadline at the FIRST threshold's stamp, which is already "+
			"in the past: the agent is told its deadline and collected in the same "+
			"tick", promoted.RefocusSince, promoteAt)
	}
	if promoted.StoppingSince != noticeAt+60 {
		t.Fatalf("stopping_since = %v — the promotion erased the close-out already "+
			"in flight; it is the SAME wind-down, not a new one", promoted.StoppingSince)
	}

	// The harm, measured rather than inferred: at the moment of promotion the
	// member must NOT be collectable, and it must still have its whole grace.
	obs := obsOf(promoted.ID, DesiredStateOnline, true)
	obs.RefocusSince = promoted.RefocusSince
	obs.RefocusOp = promoted.RefocusOp
	if d := reconcileDecide(obs, newReconcileState(), s.reconcileCfg, promoteAt); d.Command == reconcileCmdStop {
		t.Fatalf("collected on the very tick that promoted it — a zero-second "+
			"deadline (%s)", d.Reason)
	}
	grace, clocked := recycleGraceFor(refocusOpContextHigh, s.reconcileCfg)
	if !clocked {
		t.Fatal("fixture: 加速停止 must be the clocked arm")
	}
	if d := reconcileDecide(obs, newReconcileState(), s.reconcileCfg, promoteAt+grace-1); d.Command == reconcileCmdStop {
		t.Fatalf("collected before its full grace elapsed (%s)", d.Reason)
	}
	if d := reconcileDecide(obs, newReconcileState(), s.reconcileCfg, promoteAt+grace); d.Command != reconcileCmdStop {
		t.Fatalf("must be collected once the promoted grace elapses, got %q (%s)",
			d.Command, d.Reason)
	}
}

// ONE DIRECTION ONLY: an epoch the owner (or the agent) opened is never turned
// into an accelerated stop behind their back, however high the gauge reads.
func TestContextThresholds_AManuallyOpenedStopIsNeverPromoted(t *testing.T) {
	s := newReconcileTestServer(t)
	cfg := s.ctxHighConfig()
	const (
		opened = 1_769_904_000.0
		later  = opened + 900
	)

	for _, op := range []string{
		refocusOpRefocus, refocusOpRestartSelf, memberOpRelocate, memberOpModel,
	} {
		t.Run(op, func(t *testing.T) {
			m := testAgent("m-ed79-manual-" + sanitizeMemberIDForTest(op))
			m.RefocusSince = opened
			m.RefocusOp = op
			l := connectOnline(t, s, m.ID)
			defer drainListener(l)

			got := atPct(t, s, m, float64(cfg.HandoverPct)+10, later)

			if got.RefocusOp != op || got.RefocusSince != opened {
				t.Fatalf("a %s wind-down was auto-promoted to op=%q since=%v — the "+
					"owner asked for a stop with no clock and would not see it become "+
					"one", op, got.RefocusOp, got.RefocusSince)
			}
		})
	}
}

// The claim the promotion rests on, verified rather than read off a comment:
// the write that changes refocus_op is itself what carries the FINAL sentence
// to the agent's own stream. If it did not, the promotion would be silent and
// an escalation frame would have to come back.
func TestContextThresholds_PromotionDeltaCarriesTheFinalSentence(t *testing.T) {
	s := newReconcileTestServer(t)
	cfg := s.ctxHighConfig()
	const (
		noticeAt  = 1_769_904_000.0
		promoteAt = noticeAt + 900
	)

	m := testAgent("m-ed79-delta")
	l := connectOnline(t, s, m.ID)

	first := atPct(t, s, m, float64(cfg.NoticePct), noticeAt)
	drainListener(l) // the notice epoch's own delta is not what this asserts

	promoted := atPct(t, s, first, float64(cfg.HandoverPct), promoteAt)
	if promoted.RefocusOp != refocusOpContextHigh {
		t.Fatalf("fixture: the promotion did not happen (op=%q)", promoted.RefocusOp)
	}

	notice := ""
	for {
		raw := l.pop()
		if raw == nil {
			break
		}
		_, envelope := parseSSEFrame(t, raw)
		data, _ := envelope["data"].(map[string]any)
		if data == nil {
			continue
		}
		payload, _ := data["payload"].(map[string]any)
		if payload == nil {
			continue
		}
		if text, ok := payload["offboard_notice"].(string); ok {
			notice = text
		}
	}
	if notice == "" {
		t.Fatal("the promotion fanned no sentence at all: the agent was last told " +
			"there is no countdown, and is now on a 120s clock it has not been " +
			"told about")
	}
	// The whole composed sentence, from the same fixed inputs: 50% is what the
	// gauge holds at the promotion (the fixture's thresholds are 40% / 50%; the
	// comment used to say 65%, which matched neither the fixture nor the want),
	// and the deadline is the promoted stamp plus the grace —
	// 2026-02-01T00:17:00Z (noticeAt + 900 + 120).
	want := "context 50% (your limits: 40% / 50%)" +
		" — offboard now: work the sequence below, then call restart_self" +
		" yourself. Your deadline is 2026-02-01T00:17:00Z."
	if cfg.NoticePct != 40 || cfg.HandoverPct != 50 {
		t.Fatalf("stale fixture: the sentence names 40%%/50%%, server is %d%%/%d%%",
			cfg.NoticePct, cfg.HandoverPct)
	}
	if got := composedSentence(notice); got != want {
		t.Fatalf("promotion sentence:\n got: %q\nwant: %q", got, want)
	}
}

// drainListener empties a listener's queue and returns how many frames it held.
func drainListener(l *hubListener) int {
	n := 0
	for l.pop() != nil {
		n++
	}
	return n
}

// sanitizeMemberIDForTest keeps the per-case member ids unique AND valid: an op
// like "runtime/model" would otherwise put a slash in a member id.
func sanitizeMemberIDForTest(op string) string {
	out := []rune(op)
	for i, r := range out {
		if r == '/' || r == '_' {
			out[i] = '-'
		}
	}
	return string(out)
}
