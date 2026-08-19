package main

import (
	"strings"
	"testing"
	"time"
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

// durationWords are the shapes a countdown takes. A guard that only looked for
// the literal "120 seconds" would pass again the moment someone wrote "2
// minutes remaining" instead.
var durationWords = []string{"seconds left", "seconds remaining", "minutes left", "you have"}

func TestFinalCallQuotesAnAbsoluteDeadlineFromTheSameSourceAsTheCockpit(t *testing.T) {
	s := newReconcileTestServer(t)
	cfg := s.reconcileCfg

	noticeFor := func(t *testing.T, op string, age float64) (string, Member) {
		t.Helper()
		m := testAgent("m-deadline")
		m.RefocusSince = nowSecs() - age
		m.RefocusOp = op
		putTestMember(t, s, m)
		payload := s.offboardDeltaPayload(m)
		notice, ok := payload["offboard_notice"].(string)
		if !ok || notice == "" {
			t.Fatalf("%s at +%.0fs must carry a notice at all: %+v", op, age, payload)
		}
		return notice, m
	}

	// THE CLOCKED ARMS: an absolute deadline, equal to the one on the wire.
	for _, op := range []string{refocusOpContextHigh, "restart_self", "relocate"} {
		t.Run("clocked: "+op, func(t *testing.T) {
			notice, m := noticeFor(t, op, 1)

			grace, clocked := recycleGraceFor(op, cfg)
			if !clocked {
				t.Fatalf("the fixture is wrong: %q is not a clocked arm", op)
			}
			want := time.Unix(int64(m.RefocusSince+grace), 0).Format(time.RFC3339)
			if !strings.Contains(notice, "Your deadline is "+want+".") {
				t.Fatalf("%s must quote the absolute deadline %s:\n%s", op, want, notice)
			}

			// ONE source of truth: the sentence and the cockpit field must name the
			// same instant. A second expression here is exactly how the old 120
			// drifted away from the clock that actually collects.
			dto := s.newMemberDTO(m, "", "", 0)
			if got := time.Unix(int64(dto.RefocusDeadline), 0).Format(time.RFC3339); got != want {
				t.Fatalf("the wire says %s and the sentence says %s — two sources of truth", got, want)
			}

			// …and NO duration anywhere: a duration is what goes stale on replay and
			// what breaks the client's verbatim de-dupe.
			low := strings.ToLower(notice)
			for _, w := range durationWords {
				if strings.Contains(low, w) {
					t.Fatalf("%s quotes a DURATION (%q), which goes stale on every replay:\n%s",
						op, w, notice)
				}
			}
		})
	}

	// THE UNCLOCKED ARM (control in the other direction): no time at all —
	// neither a duration nor a deadline, because nothing is coming to collect.
	t.Run("no clock: "+refocusOpRefocus, func(t *testing.T) {
		for _, age := range []float64{1, SoftOffboardGraceSecs + 1, 10 * SoftOffboardGraceSecs} {
			notice, _ := noticeFor(t, refocusOpRefocus, age)
			if strings.Contains(notice, "Your deadline is") {
				t.Fatalf("重新聚焦 at +%.0fs quotes a deadline nobody will honour:\n%s", age, notice)
			}
			low := strings.ToLower(notice)
			for _, w := range durationWords {
				if strings.Contains(low, w) {
					t.Fatalf("重新聚焦 at +%.0fs started a countdown nobody is counting (%q):\n%s",
						age, w, notice)
				}
			}
		}
	})
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

	first, ok := s.offboardDeltaPayload(m)["offboard_notice"].(string)
	if !ok || !strings.Contains(first, "Your deadline is ") {
		t.Fatalf("the fixture must be on the clocked arm (it carries no deadline):\n%s", first)
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
}
