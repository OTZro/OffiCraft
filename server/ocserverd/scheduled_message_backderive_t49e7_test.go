package main

// scheduled_message_backderive_t49e7_test.go — the guards on the calendar
// derivation that replaced `custom`'s scan window (mostRecentCustomSlot).
//
// The window is gone because it could not be made correct: months {1} × days
// {1} occurs once a year and months {2} × days {29} can go eight years between
// occurrences, so any finite number of DAYS answers "no slot" for a slot that
// genuinely happened — which is mostRecentSlot not being monotonic in `now`,
// the one property its own header says the whole feature rests on.
//
// A derivation buys that back, and it costs a different kind of risk: the
// enumeration can be wrong in ways a window could not be. Written in the order
// they would bite:
//
//	the ORDER can be reversed or scrambled, and the result is a SILENTLY STALE
//	  slot — an occurrence that really happened, merely not the most recent one.
//	  Nothing crashes and nothing is out of range.
//	the FEASIBILITY pre-check is a piece of calendar knowledge, and getting it
//	  wrong condemns a legal schedule to never fire, silently.
//	the YEAR BOUND is still a truncation, just a derived one.
//
// So the shape here is a DIFFERENTIAL: a brute-force day-by-day scan over
// eleven years is the semantic definition of "the most recent date this
// schedule names that yields a reading at or before now", and the derivation
// has to agree with it on every case of a randomised corpus. The oracle shares
// customSlotOn with production on purpose — what is under test is WHICH DATE IS
// ASKED, and an oracle that also re-implemented the per-date resolver would be
// testing two things at once and reproducing that resolver's decisions in a
// second copy.
//
// 🔴 A differential over a corpus that contains nothing interesting agrees with
// anything. So the corpus is INSTRUMENTED (see diffCorpusStats) and every
// counter carries a floor: cases the retired 70-day window could not have
// answered, leap-day answers, month-end answers, and honest no-slot answers.

import (
	"math/rand"
	"testing"
	"time"
)

// backDeriveScanDays is how far the brute-force oracle walks. Eleven years,
// which is more than customLeapDayYearsBack so the oracle is never the shorter
// of the two — an oracle that truncated first would agree with a derivation
// that truncated at all.
const backDeriveScanDays = 4200

// bruteForceCustomSlot is the SEMANTIC DEFINITION of the custom cadence's most
// recent slot: walk real calendar dates backwards one day at a time, ask the
// month set and then the day set, and resolve the first date that answers.
//
// It is deliberately the round-1 algorithm with a window so wide it cannot be
// the reason for a difference. Its cost (one pass of ~4200 cheap membership
// tests per call) is what a production implementation could not pay on every
// tick and a test can pay happily.
func bruteForceCustomSlot(s ScheduledMessage, now time.Time) (time.Time, bool) {
	loc, err := time.LoadLocation(s.Timezone)
	if err != nil {
		return time.Time{}, false
	}
	local := now.In(loc)
	anchor := dayAnchor(local)
	for back := 0; back <= backDeriveScanDays; back++ {
		day := anchor.AddDate(0, 0, -back)
		if !intSetContains(s.CustomMonths, int(day.Month())) {
			continue
		}
		if !intSetContains(s.CustomDays, day.Day()) {
			continue
		}
		if slot, ok := customSlotOn(day, s, loc, local); ok {
			return slot, true
		}
	}
	return time.Time{}, false
}

// diffCorpusZones are the zones the corpus draws from: the ones this feature's
// comments name as having cost it a defect, plus two ordinary ones so the
// corpus is not made entirely of pathologies.
var diffCorpusZones = []string{
	"UTC", "Asia/Taipei", "America/New_York", "Europe/London",
	"America/Havana", "America/Santiago", "Pacific/Apia", "America/Nuuk",
	"Antarctica/Troll", "Australia/Lord_Howe", "Africa/Casablanca", "Asia/Kathmandu",
	"Asia/Jerusalem", "Africa/Cairo",
}

// diffCorpusStats is the anti-vacuity instrumentation. Each counter names a
// case the derivation would have to get right and the retired window could not,
// so a zero in any of them means the corpus proved less than it looks like it
// proved.
type diffCorpusStats struct {
	cases           int
	agreed          int
	slots           int
	noSlot          int
	beyondOldWindow int // the answer is more than 70 days back — unreachable in round 1
	leapDay         int // the answer is 29 February
	monthEnd        int // the answer is a 31st while `now` is in a month with no 31st
	infeasible      int // no (month, day) pair exists at all
	zones           map[string]bool
}

// randomCustomSchedule draws one schedule: non-empty subsets of all four sets,
// biased towards SMALL month and day sets because those are the sparse
// schedules whose gaps the window could not span.
//
// 🔴 One case in seven is drawn from the SHORT months and the LAST days
// instead, and that arm is not decoration: a uniform draw almost never lands on
// a pair the calendar cannot satisfy (measured: 2 in 2000), so without it the
// corpus would say nothing about the feasibility pre-check or about the leap
// day, and the "no slot" side of the agreement would be a rounding error.
func randomCustomSchedule(rng *rand.Rand, tz string) ScheduledMessage {
	pickFrom := func(vals []int, maxN int) []int {
		n := 1 + rng.Intn(maxN)
		if n > len(vals) {
			n = len(vals)
		}
		seen := map[int]bool{}
		for len(seen) < n {
			seen[vals[rng.Intn(len(vals))]] = true
		}
		out := make([]int, 0, len(seen))
		for v := range seen {
			out = append(out, v)
		}
		return out
	}
	months, days := intRange(1, 12), intRange(1, 31)
	if rng.Intn(7) == 0 {
		months, days = []int{2, 4, 6, 9, 11}, []int{28, 29, 30, 31}
	}
	return ScheduledMessage{
		ID:            "sch-diff",
		Cadence:       ScheduledMessageCadenceCustom,
		Timezone:      tz,
		CustomMonths:  pickFrom(months, 3),
		CustomDays:    pickFrom(days, 3),
		CustomHours:   pickFrom(intRange(0, 23), 4),
		CustomMinutes: pickFrom(intRange(0, 59), 4),
	}
}

// TestBackDerivedCustomSlotMatchesADayByDayScan is the differential itself.
//
// Red when: the candidate enumeration visits dates in the wrong order (a stale
// but plausible slot), skips a date it should have offered, offers one that is
// still ahead of `now`, or truncates before the brute-force scan does.
func TestBackDerivedCustomSlotMatchesADayByDayScan(t *testing.T) {
	rng := rand.New(rand.NewSource(0x49e7))
	st := diffCorpusStats{zones: map[string]bool{}}
	for i := 0; i < 2000; i++ {
		tz := diffCorpusZones[rng.Intn(len(diffCorpusZones))]
		sm := randomCustomSchedule(rng, tz)
		// A `now` anywhere in a twelve-year span, so leap years, century
		// boundaries and every month are all drawn from.
		now := time.Date(2024, time.January, 1, 0, 0, 0, 0, time.UTC).
			Add(time.Duration(rng.Int63n(int64(12 * 365 * 24 * time.Hour))))

		want, wantOK := bruteForceCustomSlot(sm, now)
		got, gotOK := mostRecentSlot(sm, now)

		st.cases++
		st.zones[tz] = true
		if gotOK != wantOK || (wantOK && !got.Equal(want)) {
			t.Fatalf("derivation disagrees with the day-by-day scan\n schedule: %s\n now: %s\n derived: %q (ok=%v)\n scanned: %q (ok=%v)",
				describeSchedule(sm), now.Format(time.RFC3339), slotKey(got), gotOK, slotKey(want), wantOK)
		}
		st.agreed++
		noteDiffCase(&st, sm, now, want, wantOK)
	}
	// Printed whether it passes or fails: "the derivation agreed everywhere" is
	// only worth reading next to what it agreed about.
	t.Logf("differential: %d cases agreed across %d zones — %d slots (%d beyond the retired 70-day window, "+
		"%d leap-day, %d month-end), %d honest no-slot (%d of them an impossible month × day pair)",
		st.agreed, len(st.zones), st.slots, st.beyondOldWindow, st.leapDay, st.monthEnd, st.noSlot, st.infeasible)
	assertDiffCorpusIsWorthAgreeingWith(t, st)
}

func noteDiffCase(st *diffCorpusStats, sm ScheduledMessage, now, slot time.Time, ok bool) {
	if !ok {
		st.noSlot++
		if !monthDayPairIsPossible(sm) {
			st.infeasible++
		}
		return
	}
	st.slots++
	loc, err := time.LoadLocation(sm.Timezone)
	if err != nil {
		return
	}
	local := slot.In(loc)
	nowLocal := now.In(loc)
	if nowLocal.Sub(local) > 70*24*time.Hour {
		st.beyondOldWindow++
	}
	if local.Month() == time.February && local.Day() == 29 {
		st.leapDay++
	}
	if local.Day() == 31 && time.Date(nowLocal.Year(), nowLocal.Month(), 31, 12, 0, 0, 0, time.UTC).Day() != 31 {
		st.monthEnd++
	}
}

// monthDayPairIsPossible is the corpus's own reading of feasibility, written
// out rather than borrowed from production: this counter exists to say the
// corpus CONTAINED impossible schedules, and borrowing the judgement under test
// to count them would make the floor agree with whatever that judgement says.
func monthDayPairIsPossible(sm ScheduledMessage) bool {
	for _, m := range sm.CustomMonths {
		for _, d := range sm.CustomDays {
			for y := 2024; y < 2036; y++ {
				at := time.Date(y, time.Month(m), d, 12, 0, 0, 0, time.UTC)
				if at.Year() == y && int(at.Month()) == m && at.Day() == d {
					return true
				}
			}
		}
	}
	return false
}

func assertDiffCorpusIsWorthAgreeingWith(t *testing.T, st diffCorpusStats) {
	t.Helper()
	for _, c := range []struct {
		got  int
		min  int
		what string
	}{
		{st.cases, 1000, "cases were drawn at all"},
		{len(st.zones), 10, "distinct zones appeared in the corpus"},
		{st.slots, 500, "cases produced a slot — a corpus of nothing-to-report agrees with anything"},
		{st.noSlot, 10, "cases honestly produced NO slot, so the agreement is not just about the happy path"},
		{st.beyondOldWindow, 100, "answers sat more than 70 days back — the retired window could not have produced these, " +
			"so without them this corpus does not exercise the change at all"},
		{st.leapDay, 3, "answers were 29 February"},
		{st.monthEnd, 10, "answers were a 31st reported from a month that has none"},
		{st.infeasible, 5, "cases named a (month, day) pair no calendar can satisfy"},
	} {
		if c.got < c.min {
			t.Fatalf("the differential corpus is too thin to mean anything: only %d %s (want at least %d)", c.got, c.what, c.min)
		}
	}
}

// TestBackDerivedCustomSlotIsMonotonicInNow is the property the window could
// not hold and this ticket exists for.
//
// 🔴 The failure it pins is not "the slot went backwards" — that one was
// already impossible. It is "the slot VANISHED": at day+70 the window reported
// 1 January and at day+71 it reported nothing, and a schedule cannot un-fire.
// The tzsweep next door checks the same rule in every shipped zone, but only
// inside windows minutes to days wide; a once-a-year schedule needs a walk
// measured in months, which is what this test contributes.
func TestBackDerivedCustomSlotIsMonotonicInNow(t *testing.T) {
	schedules := []struct {
		name string
		sm   ScheduledMessage
	}{
		{"once a year", ScheduledMessage{CustomMonths: []int{1}, CustomDays: []int{1},
			CustomHours: []int{9}, CustomMinutes: []int{0}}},
		{"month-end across a February", ScheduledMessage{CustomMonths: []int{1, 2}, CustomDays: []int{31},
			CustomHours: []int{0, 12}, CustomMinutes: []int{0}}},
		{"leap day only", ScheduledMessage{CustomMonths: []int{2}, CustomDays: []int{29},
			CustomHours: []int{9}, CustomMinutes: []int{0}}},
		{"quarterly", ScheduledMessage{CustomMonths: []int{3, 6, 9, 12}, CustomDays: []int{1, 15},
			CustomHours: []int{8}, CustomMinutes: []int{30}}},
	}
	steps := 0
	for _, tz := range []string{"UTC", "Asia/Taipei", "America/Havana", "Pacific/Apia", "Antarctica/Troll"} {
		for _, sc := range schedules {
			sm := sc.sm
			sm.ID, sm.Cadence, sm.Timezone = "sch-mono", ScheduledMessageCadenceCustom, tz
			var prev time.Time
			had := false
			for at := time.Date(2026, time.January, 1, 3, 0, 0, 0, time.UTC); at.Year() < 2029; at = at.Add(12 * time.Hour) {
				steps++
				slot, ok := mostRecentSlot(sm, at)
				if !ok {
					if had {
						t.Fatalf("%s %s: the slot %s reported before %s is gone by then — a schedule cannot un-fire",
							tz, sc.name, slotKey(prev), at.Format(time.RFC3339))
					}
					continue
				}
				if had && slot.Before(prev) {
					t.Fatalf("%s %s: at %s the slot went BACK from %s to %s",
						tz, sc.name, at.Format(time.RFC3339), slotKey(prev), slotKey(slot))
				}
				if slot.After(at) {
					t.Fatalf("%s %s: at %s the reported slot %s has not happened yet",
						tz, sc.name, at.Format(time.RFC3339), slotKey(slot))
				}
				prev, had = slot, true
			}
		}
	}
	if steps < 10000 {
		t.Fatalf("only %d readings were taken — too few for a clean monotonicity result to mean anything", steps)
	}
}

// TestCustomFeasibilityPrecheckAgreesWithTheCalendar drives every one of the
// 12 × 31 (month, day) pairs through the production path and compares against
// the day-by-day scan.
//
// 🔴 This is the guard on a piece of hand-written calendar knowledge shared with
// the write seam (maxDaysInMonth). Getting it wrong in one direction refuses a
// legal schedule at write time; in the other it makes this function walk its
// full year bound producing nothing. Both are silent, and the one that matters —
// February counting as 29 — is exactly the case a "days in month" helper written
// from memory gets wrong.
func TestCustomFeasibilityPrecheckAgreesWithTheCalendar(t *testing.T) {
	now := time.Date(2029, time.December, 31, 23, 0, 0, 0, time.UTC)
	possible, impossible := 0, 0
	for m := 1; m <= 12; m++ {
		for d := 1; d <= 31; d++ {
			sm := ScheduledMessage{
				ID: "sch-feas", Cadence: ScheduledMessageCadenceCustom, Timezone: "UTC",
				CustomMonths: []int{m}, CustomDays: []int{d},
				CustomHours: []int{9}, CustomMinutes: []int{0},
			}
			want, wantOK := bruteForceCustomSlot(sm, now)
			got, gotOK := mostRecentSlot(sm, now)
			if gotOK != wantOK || (wantOK && !got.Equal(want)) {
				t.Fatalf("month %d day %d: derived %q (ok=%v), the day-by-day scan says %q (ok=%v)",
					m, d, slotKey(got), gotOK, slotKey(want), wantOK)
			}
			if wantOK {
				possible++
			} else {
				impossible++
			}
		}
	}
	// Both sides have to be non-empty or the comparison above is a comparison
	// with a constant. 2, 4, 6, 9 and 11 each refuse at least one day.
	if possible < 300 || impossible < 5 {
		t.Fatalf("the pair grid produced %d possible and %d impossible pairs — one side is empty, so this test compared nothing",
			possible, impossible)
	}
	// And the one pair the whole rule is drawn around: a leap-year-only
	// schedule is LEGAL, so 29 February must be on the possible side.
	leap := ScheduledMessage{ID: "sch-leap", Cadence: ScheduledMessageCadenceCustom, Timezone: "UTC",
		CustomMonths: []int{2}, CustomDays: []int{29}, CustomHours: []int{9}, CustomMinutes: []int{0}}
	if _, ok := mostRecentSlot(leap, now); !ok {
		t.Fatal("months {2} × days {29} produced no slot at the end of 2029 — a leap-year-only schedule is " +
			"deliberately legal (see ValidateScheduledMessageCustomSets), and the previous 29 February was 2028")
	}
}

// TestBackDerivedCustomSlotSpansTheCenturyLeapGap is the case that fixes
// customLeapDayYearsBack at nine rather than five.
//
// 2100 is not a leap year, so 29 February 2096 is followed by 29 February 2104 —
// eight years with no occurrence. A schedule that names only that date reports
// the 2096 occurrence for the whole of that gap, and the year bound is what
// decides whether it can still see it at the far end.
func TestBackDerivedCustomSlotSpansTheCenturyLeapGap(t *testing.T) {
	sm := ScheduledMessage{
		ID: "sch-century", Cadence: ScheduledMessageCadenceCustom, Timezone: "UTC",
		CustomMonths: []int{2}, CustomDays: []int{29},
		CustomHours: []int{9}, CustomMinutes: []int{0},
	}
	for _, at := range []string{
		"2097-06-01T00:00:00Z",
		"2100-03-01T00:00:00Z",
		"2103-12-31T23:59:00Z",
	} {
		now, err := time.Parse(time.RFC3339, at)
		if err != nil {
			t.Fatalf("parse %s: %v", at, err)
		}
		slot, ok := mostRecentSlot(sm, now)
		if !ok || slotKey(slot) != "2096-02-29T09:00+00:00" {
			t.Fatalf("at %s the slot is %q (ok=%v), want 2096-02-29T09:00+00:00 — 2100 is not a leap year, "+
				"so the gap between two occurrences of this schedule is eight years", at, slotKey(slot), ok)
		}
	}
}

// TestPlantedBugControlEnumeratesTheSameCandidatesAsProduction pins the copy the
// tzsweep's positive control carries (mostRecentSlotWith) to production.
//
// 🔴 A copy drifts, and this one drifting is expensive in a way that is hard to
// see: the control asserts that the sweep finds THE PLANTED BUG, and a control
// whose own enumeration is wrong reports findings of its own instead, satisfies
// "findings > 0", and passes while the bug it planted quietly stops being
// detected. Driven with the REAL resolver the copy must be production, exactly.
func TestPlantedBugControlEnumeratesTheSameCandidatesAsProduction(t *testing.T) {
	rng := rand.New(rand.NewSource(0xc0de))
	agreed := 0
	for i := 0; i < 600; i++ {
		tz := diffCorpusZones[rng.Intn(len(diffCorpusZones))]
		sm := randomCustomSchedule(rng, tz)
		now := time.Date(2024, time.January, 1, 0, 0, 0, 0, time.UTC).
			Add(time.Duration(rng.Int63n(int64(12 * 365 * 24 * time.Hour))))
		want, wantOK := mostRecentSlot(sm, now)
		got, gotOK := mostRecentSlotWith(sm, now, customSlotOn)
		if gotOK != wantOK || (wantOK && !got.Equal(want)) {
			t.Fatalf("the sweep's control copy has drifted from mostRecentSlot\n schedule: %s\n now: %s\n copy: %q (ok=%v)\n production: %q (ok=%v)",
				describeSchedule(sm), now.Format(time.RFC3339), slotKey(got), gotOK, slotKey(want), wantOK)
		}
		agreed++
	}
	if agreed < 500 {
		t.Fatalf("only %d cases were compared", agreed)
	}
	// Positive control: the two are NOT the same function, so the comparison
	// above is capable of failing. Pointing the copy at the forward-searching
	// resolver — the bug the sweep plants — must make them disagree.
	disagreements := 0
	sm := ScheduledMessage{
		ID: "sch-drift", Cadence: ScheduledMessageCadenceCustom, Timezone: "America/Havana",
		CustomMonths: intRange(1, 12), CustomDays: intRange(1, 31),
		CustomHours: []int{0}, CustomMinutes: []int{30},
	}
	for at := time.Date(2026, time.March, 8, 4, 0, 0, 0, time.UTC); at.Before(time.Date(2026, time.March, 9, 4, 0, 0, 0, time.UTC)); at = at.Add(time.Minute) {
		want, wantOK := mostRecentSlot(sm, at)
		got, gotOK := mostRecentSlotWith(sm, at, forwardSearchingSlotOn)
		if gotOK != wantOK || (wantOK && !got.Equal(want)) {
			disagreements++
		}
	}
	if disagreements == 0 {
		t.Fatal("driving the copy through the forward-searching resolver produced the same answers as production " +
			"across America/Havana's spring transition — the comparison above cannot distinguish two resolvers, " +
			"so its agreement proves nothing")
	}
	t.Logf("copy fidelity: %d cases agreed with production, and %d readings disagree when the copy is pointed "+
		"at the forward-searching resolver", agreed, disagreements)
}
