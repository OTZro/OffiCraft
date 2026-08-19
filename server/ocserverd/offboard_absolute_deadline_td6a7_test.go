package main

import (
	"regexp"
	"strings"
	"testing"
	"time"
	_ "time/tzdata" // so the zones below exist on a host with no tz database
)

// T-d6a7 — the final call quotes an ABSOLUTE deadline, and it quotes the SAME
// one the cockpit shows.
//
// 🔴 The old sentence was a hardcoded "You have 120 seconds left." while the
// deadline runs from the FIRST stamp, and this notice is REPLAYED whenever the
// member row is rewritten. Measured on a live station: two notices 46 s apart,
// both claiming 120 s — the second told an agent it had 120 s when it had ~74.
// Nothing failed and nothing went red; it was a silently generous number.
//
// 🔴 And the intuitive fix is the one thing this must NOT do. The client
// de-dupes by comparing the whole sentence verbatim (cli/ocagent listen_hooks),
// so printing the seconds REMAINING makes every replay a different string, the
// de-dupe stops matching, and an agent working its close-out is woken and
// re-fed the whole document on every write to its row. So both halves are
// pinned: there IS a time, and it does NOT move with the wall clock.

// A countdown has a SHAPE, not a vocabulary.
//
// 🔴 The first version of this guard was a list of literals — "seconds left",
// "seconds remaining", "you have" — and an independent review walked straight
// through it: adding "Time remaining: 74s." to the no-clock arm, a value that
// moves with the wall clock and is exactly the bug, left every test in the tree
// green. A whitelist of yesterday's wordings only ever catches yesterday's
// wording. What these arms may not contain is a NUMBER ATTACHED TO A UNIT OF
// TIME, in any phrasing and in either language.
//
// ⚠️ The unit is what the pattern turns on, never the digit, because a real
// notice is full of legitimate numbers: "context 35% (your limits: 40% / 50%)",
// "compaction round 5 (your limits: round 5 / round 6)", and the numbered steps
// of the 下線程序 document carried below the sentence. Both directions are
// pinned by TestTimeShapeGuardReadsRealNoticesCorrectly below — a guard that
// fired on a real notice would be worse than no guard, since the fix would be
// to weaken it.
var (
	timeShapeASCII = regexp.MustCompile(`(?i)\b\d+\s*(s|sec|secs|second|seconds|m|min|mins|minute|minutes|h|hr|hour|hours)\b`)
	timeShapeCJK   = regexp.MustCompile(`[0-9０-９〇零一二三四五六七八九十兩百千]+\s*(秒|分鐘|分鍾|小時)`)
	deadlineWords  = regexp.MustCompile(`(?i)deadline|截止|死線`)
)

// quotesTimeOfAnyShape returns the offending fragment when a notice quotes a
// SPAN of time. The clocked arms' deadline clause names an INSTANT, not a span,
// so it does not match here — it has its own literal assertion.
func quotesTimeOfAnyShape(notice string) (string, bool) {
	if m := timeShapeASCII.FindString(notice); m != "" {
		return m, true
	}
	if m := timeShapeCJK.FindString(notice); m != "" {
		return m, true
	}
	return "", false
}

// quotesADeadline returns the offending fragment when a notice names a deadline
// at all — the other half of "no time word of any kind on the arms nobody is
// counting".
func quotesADeadline(notice string) (string, bool) {
	if m := deadlineWords.FindString(notice); m != "" {
		return m, true
	}
	return "", false
}

// assertQuotesNoTime is the whole no-clock rule in one call, used by every
// no-clock arm in this package so that widening the rule widens all of them.
func assertQuotesNoTime(t *testing.T, arm, notice string) {
	t.Helper()
	if frag, yes := quotesADeadline(notice); yes {
		t.Fatalf("%s named a deadline (%q) nobody will honour:\n%s", arm, frag, notice)
	}
	if frag, yes := quotesTimeOfAnyShape(notice); yes {
		t.Fatalf("%s started a countdown nobody is counting (%q) — a span goes "+
			"stale on every replay and breaks the client's verbatim de-dupe:\n%s",
			arm, frag, notice)
	}
}

// A FIXED epoch, so the expected sentence can be a LITERAL. Computing `want`
// from the same expression production uses is how a rendering bug hides: both
// sides move together and the test stays green regardless. That is precisely
// what happened to the timezone — the old expectation mirrored
// `time.Unix(...).Format(time.RFC3339)`, so it agreed with the implementation's
// implicit LOCAL zone and would have agreed with any other zone just as
// happily. The deadline is machine-read, by an agent that need not be running
// on this host, so it is rendered in UTC and asserted as a Z-suffixed literal.
const (
	deadlineEpochSince = 1_769_904_000 // 2026-02-01T00:00:00Z
	deadlineEpochGrace = 120           // the recycle clock that actually collects
	deadlineEpochWant  = "2026-02-01T00:02:00Z"
)

func TestFinalCallQuotesAnAbsoluteDeadlineFromTheSameSourceAsTheCockpit(t *testing.T) {
	s := newReconcileTestServer(t)
	cfg := s.reconcileCfg

	noticeFrom := func(t *testing.T, op string, since float64) (string, Member) {
		t.Helper()
		m := testAgent("m-deadline")
		m.RefocusSince = since
		m.RefocusOp = op
		putTestMember(t, s, m)
		payload := s.offboardDeltaPayload(m)
		notice, ok := payload["offboard_notice"].(string)
		if !ok || notice == "" {
			t.Fatalf("%s must carry a notice at all: %+v", op, payload)
		}
		return notice, m
	}

	// THE CLOCKED ARMS: an absolute deadline, equal to the one on the wire.
	for _, op := range []string{refocusOpContextHigh, "restart_self", "relocate"} {
		t.Run("clocked: "+op, func(t *testing.T) {
			grace, clocked := recycleGraceFor(op, cfg)
			if !clocked {
				t.Fatalf("the fixture is wrong: %q is not a clocked arm", op)
			}
			if grace != deadlineEpochGrace {
				t.Fatalf("the fixture is stale: the recycle clock is %.0fs, so the "+
					"expected instant below is no longer %s", grace, deadlineEpochWant)
			}

			notice, m := noticeFrom(t, op, deadlineEpochSince)
			if !strings.Contains(notice, "Your deadline is "+deadlineEpochWant+".") {
				t.Fatalf("%s must quote the absolute deadline %s:\n%s",
					op, deadlineEpochWant, notice)
			}

			// ONE source of truth: the sentence and the cockpit field must name the
			// same instant. A second expression here is exactly how the old 120
			// drifted away from the clock that actually collects.
			dto := s.newMemberDTO(m, "", "", 0)
			if got := int64(dto.RefocusDeadline); got != deadlineEpochSince+deadlineEpochGrace {
				t.Fatalf("the wire says epoch %d and the sentence says %s — two "+
					"sources of truth", got, deadlineEpochWant)
			}

			// …and NO duration anywhere: a duration is what goes stale on replay and
			// what breaks the client's verbatim de-dupe.
			if frag, yes := quotesTimeOfAnyShape(notice); yes {
				t.Fatalf("%s quotes a DURATION (%q), which goes stale on every "+
					"replay:\n%s", op, frag, notice)
			}
		})
	}

	// THE UNCLOCKED ARM (control in the other direction): no time at all —
	// neither a duration nor a deadline, because nothing is coming to collect.
	t.Run("no clock: "+refocusOpRefocus, func(t *testing.T) {
		for _, age := range []float64{1, SoftOffboardGraceSecs + 1, 10 * SoftOffboardGraceSecs} {
			notice, _ := noticeFrom(t, refocusOpRefocus, nowSecs()-age)
			assertQuotesNoTime(t, "重新聚焦", notice)
		}
	})
}

// …and the instant is rendered in UTC no matter what timezone the SERVER
// PROCESS happens to run in.
//
// 🔴 The implementation formatted a LOCAL-zone time, so the characters in the
// sentence depended on the host's TZ env — and the test agreed with it, because
// it computed its expectation with the same expression. Two things are wrong
// with a local rendering: the reader is an AGENT, which need not be running on
// the server's host, and the premise of the whole fix is that one epoch renders
// to ONE constant string. Pinning time.Local here is what makes the assertion
// independent of wherever this suite is run.
func TestDeadlineIsRenderedInUTCWhateverTheServerProcessTimezone(t *testing.T) {
	for _, zone := range []string{"Asia/Taipei", "America/Los_Angeles", "UTC"} {
		t.Run(zone, func(t *testing.T) {
			loc, err := time.LoadLocation(zone)
			if err != nil {
				t.Fatalf("load %s: %v", zone, err)
			}
			saved := time.Local
			time.Local = loc
			defer func() { time.Local = saved }()

			notice := offboardNotice("context 50% (your limits: 40% / 50%)",
				offboardCloserRestartSelf, true, deadlineEpochSince+deadlineEpochGrace, "")
			if !strings.Contains(notice, "Your deadline is "+deadlineEpochWant+".") {
				t.Fatalf("a server running in %s must still quote %s:\n%s",
					zone, deadlineEpochWant, notice)
			}
		})
	}
}

// The guard's OTHER direction, and it is not optional: a shape guard that fires
// on a real notice is worse than no guard at all, because the repair for a
// false positive is to weaken it. Every sentence this server actually composes
// is dense with numbers — percentages, thresholds, compaction rounds, and the
// numbered steps of the 下線程序 document — and none of them is a span of time.
func TestTimeShapeGuardReadsRealNoticesCorrectly(t *testing.T) {
	s := newReconcileTestServer(t)

	notices := map[string]string{}
	for _, op := range []string{refocusOpRefocus, refocusOpContextHigh, "restart_self", "relocate"} {
		m := testAgent("m-shape")
		m.RefocusSince = nowSecs() - 1
		m.RefocusOp = op
		putTestMember(t, s, m)
		notice, _ := s.offboardDeltaPayload(m)["offboard_notice"].(string)
		if notice == "" {
			t.Fatalf("fixture: %s must carry a notice", op)
		}
		notices["refocus_op="+op] = notice
	}
	down := testAgent("m-shape-down")
	down.DesiredState = DesiredStateOffline
	down.StoppingSince = nowSecs() - 1
	putTestMember(t, s, down)
	notice, _ := s.offboardDeltaPayload(down)["offboard_notice"].(string)
	if notice == "" {
		t.Fatal("fixture: 下線 must carry a notice")
	}
	notices["下線"] = notice
	// The codex arm's `where` counts ROUNDS, not seconds — the one place a bare
	// "round 5 / round 6" could be mistaken for a clock.
	notices["codex rounds"] = offboardNotice(
		"compaction round 5 (your limits: round 5 / round 6)",
		offboardCloserRestartSelf, false, 0, s.offboardText())

	for name, n := range notices {
		if frag, yes := quotesTimeOfAnyShape(n); yes {
			t.Fatalf("the shape guard misfires on a REAL notice (%s): it read %q "+
				"as a span of time:\n%s", name, frag, n)
		}
	}

	// …and it still catches every shape the review walked through the old
	// literal whitelist with.
	for _, countdown := range []string{
		"Time remaining: 74s.",
		"You have 120 seconds left.",
		"about 2 minutes left",
		"~3 h to go",
		"剩 90 秒。",
		"還有 2 分鐘",
	} {
		if _, yes := quotesTimeOfAnyShape(notices["下線"] + " " + countdown); !yes {
			t.Fatalf("the shape guard misses a countdown written as %q", countdown)
		}
	}
}

// The replay property, asserted directly: the same epoch must produce the same
// sentence no matter how much later it is composed. This is the assertion a
// seconds-remaining implementation cannot satisfy, and it is the reason the fix
// is an absolute deadline rather than a live countdown.
func TestFinalCallSentenceIsStableAcrossReplaysWithinOneEpoch(t *testing.T) {
	s := newReconcileTestServer(t)
	m := testAgent("m-replay")
	m.RefocusSince = nowSecs() - 5
	m.RefocusOp = refocusOpContextHigh
	putTestMember(t, s, m)

	// ⚠️ The precondition is only that a notice EXISTS. It used to require the
	// literal "Your deadline is " prefix, which meant any countdown-shaped
	// implementation — the very thing this test exists to reject — failed here
	// as a fixture error and never reached the property below. The property is
	// what must fire first.
	first, ok := s.offboardDeltaPayload(m)["offboard_notice"].(string)
	if !ok || first == "" {
		t.Fatalf("the fixture must carry a notice at all: %+v", s.offboardDeltaPayload(m))
	}

	// A LATER composition of the SAME epoch. The row is untouched; only the wall
	// clock has moved, which is exactly the situation the live station was in
	// when it sent two notices 46 s apart both claiming 120 seconds.
	time.Sleep(1100 * time.Millisecond)
	second, _ := s.offboardDeltaPayload(m)["offboard_notice"].(string)

	if first != second {
		t.Fatalf("the sentence moved with the wall clock, so the client's verbatim "+
			"de-dupe will never match again:\nfirst:  %s\nsecond: %s", first, second)
	}

	// …and only now, once the property has had its chance to fire: the arm under
	// test really is the clocked one.
	if !strings.Contains(first, "Your deadline is ") {
		t.Fatalf("the fixture must be on the clocked arm (it carries no deadline):\n%s", first)
	}
}
