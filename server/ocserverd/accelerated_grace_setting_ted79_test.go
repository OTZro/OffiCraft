package main

import (
	"strconv"
	"testing"
)

// accelerated_grace_setting_ted79_test.go — the 加速停止 grace is ONE adjustable
// number, and every face that speaks it speaks the same one (owner 2026-08-21:
// 「120 秒這個設定可以調整，統一在第二門檻跟加速停止使用」).
//
// The hazard this guards is NOT "the setting does not save". It is the shape
// T-ed79 spent its whole length removing: two faces answering the same question
// from two places. A grace that only the reconcile tick reads collects the
// member at one instant while the notice and the cockpit quote another; a grace
// that only the notice reads promises a hand-off window nothing honours. Both
// are silent — nothing errors, the numbers simply disagree — so each face is
// asserted through the SAME change, with the shipped default as the control.
//
// 🔴 THE CONTROL IS THE POINT. Asserting only "after the PATCH the grace is 300"
// passes for a face that hard-codes 300 as much as for one that reads the
// setting, and asserting only the default passes for one that hard-codes 120.
// Each face is read twice — once at the default, once after the write — and the
// pair has to MOVE, which is what tells "reads the setting" apart from "happens
// to agree today".
//
// 🔴 Measured before this test was written: with reconcileConfigLive in place
// but the read sites still on the raw s.reconcileCfg field, this file FAILS on
// the changed-value half of every face below while the default half passes —
// which is the exact "the knob saves and nothing reads it" defect.

// acceleratedGraceFaces reads every face that quotes the grace for one member,
// so a face that answered from its own copy shows up as a disagreement here
// rather than as a wrong number in production.
type acceleratedGraceFaces struct {
	// clock: the grace recycleGraceFor hands the reconcile arm that actually
	// collects the member.
	clock float64
	// collectedAt: the FIRST instant reconcileDecide dispatches the robust stop.
	// Derived by probing the decider rather than by reading the config, so a
	// decider that ignores the config is caught here and not only in review.
	collectedAt float64
	// deadline: the wire field the cockpit renders (MemberDTO.refocus_deadline).
	deadline float64
	// notice: the sentence the agent is handed on its own stream.
	notice string
}

func readAcceleratedGraceFaces(t *testing.T, s *apiServer, m Member) acceleratedGraceFaces {
	t.Helper()
	cfg := s.reconcileConfigLive()
	grace, clocked := recycleGraceFor(m.RefocusOp, cfg)
	if !clocked {
		t.Fatalf("refocus_op=%q is not on a clock at all — this fixture is meant "+
			"to exercise a CLOCKED cause", m.RefocusOp)
	}
	obs := obsOf(m.ID, DesiredStateOnline, true)
	obs.RefocusSince = m.RefocusSince
	obs.RefocusOp = m.RefocusOp

	// Probe the decider for the first second it collects. Bounded by the widest
	// grace the PATCH face accepts, so a decider that never collects fails with
	// a readable message instead of looping.
	collectedAt := 0.0
	for elapsed := 1.0; elapsed <= maxAcceleratedGraceSecs+2; elapsed++ {
		d := reconcileDecide(obs, newReconcileState(), cfg, m.RefocusSince+elapsed)
		if d.Command == reconcileCmdStop {
			collectedAt = m.RefocusSince + elapsed
			break
		}
	}
	if collectedAt == 0.0 {
		t.Fatalf("refocus_op=%q: nothing ever collected the member — a clocked "+
			"cause with no collection is the unannounced-clock bug inverted",
			m.RefocusOp)
	}

	dto := s.newMemberDTO(m, "", "", 0)
	notice, _ := s.offboardDeltaPayload(m)["offboard_notice"].(string)
	return acceleratedGraceFaces{
		clock:       grace,
		collectedAt: collectedAt,
		deadline:    dto.RefocusDeadline,
		notice:      notice,
	}
}

// patchAcceleratedGrace writes the setting through the PATCH face's own
// storage+snapshot pair, so the test exercises what the endpoint does rather
// than a field poke that would pass even if the endpoint never wrote the DB.
func patchAcceleratedGrace(t *testing.T, s *apiServer, secs int) {
	t.Helper()
	if !acceleratedGraceInRange(secs) {
		t.Fatalf("fixture value %d is outside the accepted range", secs)
	}
	s.settingsMu.Lock()
	defer s.settingsMu.Unlock()
	if err := s.dal.PutSetting(settingAcceleratedGraceSecs, strconv.Itoa(secs)); err != nil {
		t.Fatalf("put setting: %v", err)
	}
	s.acceleratedGraceSecs = secs
}

func TestAcceleratedGrace_OneSettingMovesEveryClockedFace(t *testing.T) {
	const now = 1_769_904_000.0 // 2026-02-01T00:00:00Z
	const changed = 300

	s := newReconcileTestServer(t)
	m := testAgent("m-ed79-grace")
	l := connectOnline(t, s, m.ID)
	defer drainListener(l)
	m.RefocusSince = now
	m.RefocusOp = refocusOpContextHigh
	putTestMember(t, s, m)

	before := readAcceleratedGraceFaces(t, s, m)
	if before.clock != float64(acceleratedGraceSecsDefault) {
		t.Fatalf("shipped default: clock=%v, want %v — the knob must not change "+
			"what an install that never touched it does",
			before.clock, acceleratedGraceSecsDefault)
	}
	if before.collectedAt != now+float64(acceleratedGraceSecsDefault) {
		t.Fatalf("shipped default: collected at %v, want %v",
			before.collectedAt, now+float64(acceleratedGraceSecsDefault))
	}
	if before.deadline != now+float64(acceleratedGraceSecsDefault) {
		t.Fatalf("shipped default: refocus_deadline=%v, want %v",
			before.deadline, now+float64(acceleratedGraceSecsDefault))
	}

	patchAcceleratedGrace(t, s, changed)
	after := readAcceleratedGraceFaces(t, s, m)

	if after.clock != changed {
		t.Errorf("clock: %v after the setting moved to %d — recycleGraceFor is "+
			"not reading the setting", after.clock, changed)
	}
	if after.collectedAt != now+changed {
		t.Errorf("the tick collects at %v, want %v — the reconcile arm is "+
			"collecting on a grace the owner no longer set",
			after.collectedAt, now+changed)
	}
	if after.deadline != now+changed {
		t.Errorf("refocus_deadline=%v, want %v — the cockpit renders a countdown "+
			"the tick has no intention of honouring", after.deadline, now+changed)
	}
	// The three numbers above are the same number seen three ways: state that as
	// an identity too, so a future face that reads its own copy has to disagree
	// with THIS assertion and not merely with an arithmetic one.
	if after.collectedAt != after.deadline {
		t.Errorf("the tick collects at %v while the wire says %v — the clock and "+
			"the sentence came apart", after.collectedAt, after.deadline)
	}
	if before.notice == "" || after.notice == "" {
		t.Fatal("a clocked cause must carry a notice at both graces")
	}
	if before.notice == after.notice {
		t.Errorf("the agent is handed the SAME sentence at %ds and at %ds: %q — "+
			"the notice quotes a deadline, so an unchanged sentence means it is "+
			"quoting a number the tick no longer uses",
			acceleratedGraceSecsDefault, changed, after.notice)
	}
}

// The knob says HOW LONG, never WHO. A soft cause must stay uncollected at any
// value the PATCH face accepts — otherwise turning the grace down would quietly
// put a deadline on the arms the owner ruled carry none.
func TestAcceleratedGrace_NeverPutsAClockOnASoftCause(t *testing.T) {
	s := newReconcileTestServer(t)
	const now = 1_769_904_000.0

	for _, secs := range []int{minAcceleratedGraceSecs, 300, maxAcceleratedGraceSecs} {
		patchAcceleratedGrace(t, s, secs)
		cfg := s.reconcileConfigLive()
		for _, op := range everyWindDownCause {
			if _, clocked := winddownKindFor(op); clocked {
				continue
			}
			if grace, clocked := recycleGraceFor(op, cfg); clocked {
				t.Errorf("grace=%d: soft cause %q became clocked (%v)", secs, op, grace)
			}
			obs := obsOf("m-ed79-soft", DesiredStateOnline, true)
			obs.RefocusSince = now
			obs.RefocusOp = op
			for _, elapsed := range []float64{1, 120, 3600, 365 * 24 * 3600} {
				d := reconcileDecide(obs, newReconcileState(), cfg, now+elapsed)
				if d.Command == reconcileCmdStop {
					t.Errorf("grace=%d: soft cause %q collected at +%.0fs (%s)",
						secs, op, elapsed, d.Reason)
				}
			}
		}
	}
}

// The PATCH face and the boot-time loader must accept exactly the same values.
// They used to be able to disagree elsewhere in this file's neighbourhood
// (outsource_max_parallel), and the failure mode was a value that saved fine and
// then refused to boot — with no warning at save time.
func TestAcceleratedGrace_SaveFaceAndLoadFaceAgree(t *testing.T) {
	s := newReconcileTestServer(t)
	for _, secs := range []int{
		minAcceleratedGraceSecs - 1, minAcceleratedGraceSecs,
		acceleratedGraceSecsDefault, maxAcceleratedGraceSecs,
		maxAcceleratedGraceSecs + 1, 0, -1,
	} {
		accepted := acceleratedGraceInRange(secs)
		if err := s.dal.PutSetting(settingAcceleratedGraceSecs, strconv.Itoa(secs)); err != nil {
			t.Fatalf("put setting: %v", err)
		}
		_, err := loadAuthSettings(s.dal, Config{}, func(string) {})
		if accepted && err != nil {
			t.Errorf("%d: the save face accepts it and the loader refuses to boot: %v",
				secs, err)
		}
		if !accepted && err == nil {
			t.Errorf("%d: the save face refuses it and the loader boots on it", secs)
		}
	}
}
