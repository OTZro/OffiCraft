package main

// schedule_slot.go — T-f059 定期訊息: the slot arithmetic, kept as PURE
// functions with no DAL, no HTTP and no ambient clock so every rule below is
// testable by handing it a struct and a `now`.
//
// The whole feature rests on one question: "what is the most recent slot of
// this schedule that has already elapsed?" Answer it, name that slot with a
// stable string, and compare against the string already delivered — that is the
// entire fire/skip decision (see migrations/00050 for why the cursor is a slot
// identifier rather than a clock reading).

import (
	"fmt"
	"time"

	// 🔴 Embed the IANA tz database into the binary. Without this,
	// time.LoadLocation reads /usr/share/zoneinfo from the HOST: present on
	// macOS and on ordinary Linux, absent in a slim container — and the failure
	// mode there is not "the scheduler broke". It is "every timezone silently
	// resolves to something else and the messages keep arriving, at the wrong
	// hour". Nothing alarms on that. ~450 KB of binary buys host independence.
	// This is the first and only tz-database dependency in the repo.
	_ "time/tzdata"
)

// slotKeyLayout is the canonical rendering of a slot, e.g.
// `2026-08-10T09:00+08:00`. The zone OFFSET is part of the identity on purpose:
// the same wall-clock reading on the two sides of a DST transition is two
// different instants, and a cursor that could not tell them apart would either
// resend or skip one. `-07:00` (numeric, never `Z`) keeps the rendering uniform
// so a UTC schedule reads `+00:00` rather than switching shape.
const slotKeyLayout = "2006-01-02T15:04-07:00"

// monthlyLookbackMonths bounds how far back a monthly schedule searches for a
// month that actually CONTAINS its day_of_month.
//
// 🔴 Looking back only ONE step is the bug this constant exists to prevent, and
// it fails silently: with day_of_month=31 and now = 1 March, March's 31st has
// not arrived and February has no 31st at all, so the correct answer is 31
// JANUARY — two months back. A shorter search finds nothing, returns "no slot",
// and the schedule simply never fires, with nothing to observe.
// TestMostRecentSlotSkipsMonthsWithoutTheDay pins that two-months case; the rest
// is bounded headroom, not a claim that any real schedule needs twelve.
const monthlyLookbackMonths = 12

// dailyLookbackDays bounds how far back a daily schedule searches for a date its
// zone actually HAS. It counts STEPS BACK from today, so the search covers today
// plus this many earlier dates — four in all. Two steps is the worst real case
// (today's slot still ahead, and yesterday a date the zone deleted outright —
// Pacific/Apia skipped 30 December 2011 for the date-line move); the rest is
// headroom that keeps the loop bounded.
const dailyLookbackDays = 3

// weeklyLookbackDays bounds the weekly search, again as steps back from today
// (fifteen dates in all). Seven days finds the previous occurrence of the
// weekday; fourteen is what it takes when THAT occurrence landed on a date its
// zone deleted, so the answer is a week earlier again.
const weeklyLookbackDays = 14

// mostRecentSlot returns the latest slot of s at or before now, computed as
// WALL-CLOCK TIME IN s.Timezone — never in the host's zone, never in UTC.
//
// 🔴 The whole feature depends on this function being MONOTONIC in `now`: a
// later `now` must never yield an earlier slot. Nothing about slot identity
// enforces that, so the arithmetic below has to earn it — see dayAnchor for the
// trap that broke it, and slotAt for what a slot is built out of.
// runScheduledMessageTick nonetheless refuses to move the cursor backwards, so a
// future non-monotonic answer costs a delivery rather than duplicating one.
//
// ok=false means "no slot exists", which happens in exactly these ways, all of
// which the caller must treat as "do not deliver":
//   - s.Timezone will not load (see the 🔴 in ValidateScheduledMessageTimezone:
//     there is NO fallback zone, here or anywhere else in this feature);
//   - no date within the cadence's lookback exists in both the calendar and the
//     zone (a 31st in February; a calendar day the zone deleted outright);
//   - a cadence outside the closed set.
func mostRecentSlot(s ScheduledMessage, now time.Time) (time.Time, bool) {
	loc, err := time.LoadLocation(s.Timezone)
	if err != nil {
		return time.Time{}, false
	}
	local := now.In(loc)
	switch s.Cadence {
	case ScheduledMessageCadenceDaily:
		anchor := dayAnchor(local)
		for back := 0; back <= dailyLookbackDays; back++ {
			day := anchor.AddDate(0, 0, -back)
			if slot, exists := slotOn(day, s, loc); exists && !slot.After(local) {
				return slot, true
			}
		}
		return time.Time{}, false

	case ScheduledMessageCadenceWeekly:
		anchor := dayAnchor(local)
		for back := 0; back <= weeklyLookbackDays; back++ {
			day := anchor.AddDate(0, 0, -back)
			if int(day.Weekday()) != s.DayOfWeek {
				continue
			}
			if slot, exists := slotOn(day, s, loc); exists && !slot.After(local) {
				return slot, true
			}
		}
		return time.Time{}, false

	case ScheduledMessageCadenceMonthly:
		year, month := local.Year(), local.Month()
		for back := 0; back <= monthlyLookbackMonths; back++ {
			slot, exists := monthlySlot(year, month, s, loc)
			if exists && !slot.After(local) {
				return slot, true
			}
			// Step back one calendar month. Done on (year, month) directly
			// rather than via AddDate, which would normalise 31 March minus one
			// month into 3 March and start skipping months.
			month--
			if month < time.January {
				month = time.December
				year--
			}
		}
		return time.Time{}, false
	}
	return time.Time{}, false
}

// dayAnchor names t's calendar DATE — year, month, day, with the zone dropped —
// as a noon-UTC instant, so day arithmetic can be done on it.
//
// 🔴 Day arithmetic is done here and never on a local time. `local.AddDate(0,0,-1)`
// reads like "yesterday" but is not: when yesterday's midnight is an hour the
// zone skipped (America/Santiago 2026-09-06, America/Havana 2026-03-08), the
// result normalises BACKWARDS into the day before, so "yesterday" quietly
// becomes the day before yesterday — the cursor walks backwards and the tick
// redelivers slots it already sent. UTC has no transitions, so the same
// arithmetic on this anchor is pure calendar counting. Noon, not midnight,
// because midnight is the reading zones actually skip.
func dayAnchor(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 12, 0, 0, 0, time.UTC)
}

// slotOn stamps s's hour:minute onto the calendar date of day, in loc.
func slotOn(day time.Time, s ScheduledMessage, loc *time.Location) (time.Time, bool) {
	return slotAt(day.Year(), day.Month(), day.Day(), s, loc)
}

// monthlySlot builds the slot for s.DayOfMonth within (year, month), reporting
// exists=false when that month has no such day.
func monthlySlot(year int, month time.Month, s ScheduledMessage, loc *time.Location) (time.Time, bool) {
	return slotAt(year, month, s.DayOfMonth, s, loc)
}

// 🔴 What decides "this date is not in this zone" is THE DATE HAVING NO READING
// AT ALL — never how far a forward walk got, and never a guess at how large a
// DST gap can be. Both earlier versions got this from the wrong place:
//
//	v1 bounded the walk at 120 MINUTES and called that headroom on the grounds
//	that gaps are an hour at most. Antarctica/Troll needs exactly 120 every
//	March — the bound fitted with nothing to spare — and Antarctica/Casey's 180
//	did not fit at all, so those occurrences were dropped in silence.
//	v2 bounded the walk at THE END OF THE CALENDAR DAY and read "the walk ran
//	out" as "the zone does not have this date". That inference is false. A
//	spring-forward can land ON midnight and take the whole tail of the day with
//	it: America/Nuuk, Scoresbysund and their aliases jump 23:00 → 00:00 every
//	March, so those dates have no reading from 23:00 onward while plainly
//	existing. Reading that as a missing date sent the cadence loop back a day, to
//	the slot the cursor was already parked on — so the tick skipped without
//	delivering, without erroring and without logging. Same silence, one day a
//	year, in a zone nobody tests.
//
// So the two absences are asked as two SEPARATE questions, in this order:
//
//	Does the zone have this DATE at all?  → firstReadingOn. No reading anywhere
//	  in the 1440 wall readings means the zone deleted the date outright
//	  (Pacific/Apia, 30 December 2011, the date-line move). The occurrence is
//	  dropped and the cadence loop steps back a day. THIS is what decides the day
//	  boundary now — nothing else does.
//	Does the zone have this WALL CLOCK?  → the +1-minute walk, which matches
//	  svc-automation's _first_existing_instant. It is NOT bounded by the day: it
//	  may cross midnight onto the next date, because the reading it is looking
//	  for is simply the next one the zone has.
//
// Neither question is answerable from the other's result, which is exactly what
// both earlier versions assumed. The design doc carries the measured counts
// behind each reversal, stamped with the tzdata release and the date they were
// taken — they are a snapshot of one tzdata release, not a property of zones,
// so they are deliberately NOT restated here where nothing would ever re-check
// them.

// firstReadingOn returns the earliest wall reading loc actually HAS on this
// calendar date, and false when it has none — the test for "this zone does not
// have this date".
//
// Cost is one probe on an ordinary date, since 00:00 exists; the full 1440 are
// walked only for a date that really was deleted.
func firstReadingOn(year int, month time.Month, day int, loc *time.Location) (time.Time, bool) {
	start := time.Date(year, month, day, 0, 0, 0, 0, time.UTC)
	for want := start; want.Day() == day && want.Month() == month && want.Year() == year; want = want.Add(time.Minute) {
		if slot, ok := readBack(want, loc); ok {
			return slot, true
		}
	}
	return time.Time{}, false
}

// readBack constructs want's wall reading in loc and returns it only if loc
// genuinely has that reading.
//
// ⚠️ The read-back is not an optional check: time.Date NORMALISES a reading it
// cannot honour rather than refusing it, in both directions and by an amount of
// its own choosing. 31 February 2026 becomes 3 March; 00:30 on 2026-03-08 in
// America/Havana becomes 03-07 23:30 (backwards, into the previous day); 02:15
// on 2026-10-04 in Australia/Lord_Howe becomes 02:45 (forwards, thirty minutes,
// same date — which a day-only check waves straight through). So every component
// is compared, and only an exact match counts as the reading that was asked for.
func readBack(want time.Time, loc *time.Location) (time.Time, bool) {
	slot := time.Date(want.Year(), want.Month(), want.Day(),
		want.Hour(), want.Minute(), 0, 0, loc)
	ok := slot.Year() == want.Year() && slot.Month() == want.Month() && slot.Day() == want.Day() &&
		slot.Hour() == want.Hour() && slot.Minute() == want.Minute()
	return slot, ok
}

// slotAt builds the slot for (year, month, day) at s's hour:minute in loc.
//
// Two different absences are handled two DIFFERENT ways, and the asymmetry is
// deliberate — see the design doc, which records the reasoning for each:
//
//	THE DATE DOES NOT EXIST — either the month has no such day (31 February) or
//	  the zone deleted the date outright (Pacific/Apia, 30 December 2011) → the
//	  occurrence is DROPPED.
//	  Owner ruling rc-aeef15360ab5, RFC 5545: the day the owner asked for is not
//	  there at all, so there is nothing to move it to that would not be a
//	  different day of the owner's month.
//	THE WALL CLOCK DOES NOT EXIST (the reading a spring-forward skips) → the
//	  occurrence MOVES FORWARD to the first reading the zone does have, EVEN IF
//	  THAT READING IS ON THE NEXT DATE.
//	  The date IS there; it is merely short of some readings, and when the gap
//	  lands on midnight the ones it is short of are the last of the day. Dropping
//	  it means the owner gets nothing that day and nothing says so, which is the
//	  exact failure this feature exists to prevent. Half an hour late is late; it
//	  is not silence.
//
// 🔴 The slot is always CONSTRUCTED from (year, month, day, hour, minute, zone)
// and never derived from an offset of `now`. That is what makes it deterministic
// across the autumn side too: when a wall clock occurs TWICE, both passes
// construct the same instant, so the cursor sees the same slot and the second
// pass delivers nothing. An "elapsed since" style computation would produce two
// instants there and an ordering test alone would not catch it.
func slotAt(year int, month time.Month, day int, s ScheduledMessage, loc *time.Location) (time.Time, bool) {
	// Does the MONTH have this day? A calendar question, asked zone-free — UTC
	// has no transitions, so nothing here can be perturbed by one.
	wall := time.Date(year, month, day, s.Hour, s.Minute, 0, 0, time.UTC)
	if wall.Year() != year || wall.Month() != month || wall.Day() != day {
		return time.Time{}, false
	}
	// Does the ZONE have this date? Asked BEFORE the walk and independently of
	// it, so that "the walk found nothing on this date" can no longer be
	// mistaken for "the zone has no such date".
	if _, exists := firstReadingOn(year, month, day, loc); !exists {
		return time.Time{}, false
	}
	// Does the zone have this WALL CLOCK? If not, the occurrence takes the next
	// reading the zone does have.
	for want := wall; want.Day() == day && want.Month() == month && want.Year() == year; want = want.Add(time.Minute) {
		if slot, ok := readBack(want, loc); ok {
			return slot, true
		}
	}
	// The gap ran off the end of the date (America/Nuuk, 23:00 → 00:00): the
	// next reading is the first one of the following date. Should the zone not
	// have that date either — which no tzdata release has ever done back to back
	// — the occurrence is dropped rather than wandering further.
	next := time.Date(year, month, day+1, 12, 0, 0, 0, time.UTC)
	return firstReadingOn(next.Year(), next.Month(), next.Day(), loc)
}

// slotKey renders a slot as the identifier stored in last_fired_slot. The same
// slot must always render to the same string — that equality IS the
// "already delivered" test.
func slotKey(slot time.Time) string {
	return slot.Format(slotKeyLayout)
}

// slotIsAfterCursor reports whether slot is STRICTLY LATER than the slot the
// cursor names — the fire/skip test.
//
// 🔴 This is deliberately an ordering test and not string inequality. The cursor
// is a rendered instant (`2026-09-06T09:00-03:00`), and inequality answers
// "different from last time", which is not the question: a slot computation that
// ever moves BACKWARDS produces a string that differs from the cursor and
// therefore fires, redelivering something already sent — and a duplicate
// delivery is indistinguishable, in the chat log, from a correct one. Comparing
// instants makes the cursor a ratchet: the worst a future non-monotonic answer
// can do is skip a delivery, which is at least discoverable.
//
// An empty or unparseable cursor means "no slot has been delivered", so the
// schedule fires: that is the state of a row written before the cursor existed,
// and refusing to fire on it would strand the schedule forever.
func slotIsAfterCursor(slot time.Time, cursor string) bool {
	previous, err := time.Parse(slotKeyLayout, cursor)
	if err != nil {
		return true
	}
	return slot.After(previous)
}

// currentSlotKey is the cursor value for "everything up to and including now has
// already been dealt with" — what creation seeds last_fired_slot with so a new
// schedule does not fire the slot it was born after, and what an edit re-aims
// the cursor to so a re-aimed schedule does not fire the slot it crossed. An
// empty string means no slot exists yet, which fires at the next real one.
func currentSlotKey(s ScheduledMessage, now time.Time) string {
	slot, ok := mostRecentSlot(s, now)
	if !ok {
		return ""
	}
	return slotKey(slot)
}

// describeSchedule is the log identity of one schedule — id plus the aimed slot
// in words, so a skipped-delivery line says which schedule and which aim.
func describeSchedule(s ScheduledMessage) string {
	return fmt.Sprintf("%s (%s %02d:%02d %s)", s.ID, s.Cadence, s.Hour, s.Minute, s.Timezone)
}
