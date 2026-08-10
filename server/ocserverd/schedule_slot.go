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
// zone actually HAS. Two steps is the worst real case (today's slot still ahead,
// and yesterday a date the zone deleted outright — Pacific/Apia skipped 30
// December 2011 for the date-line move); the third is headroom that keeps the
// loop bounded.
const dailyLookbackDays = 3

// weeklyLookbackDays bounds the weekly search. Seven days finds the previous
// occurrence of the weekday; fourteen is what it takes when THAT occurrence
// landed on a date its zone deleted, so the answer is a week earlier again.
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

// dstGapScanMinutes bounds the search for the first wall clock that EXISTS after
// one the zone skipped. Every gap in the tz database is an hour or less (Lord
// Howe's is thirty minutes), so two hours is headroom, and the bound is what
// makes a DELETED CALENDAR DAY — Pacific/Apia skipped 30 December 2011 outright
// for the date-line move — run out of candidates and report "no slot on this
// date", which sends the cadence loop back another day. (A schedule set inside
// the last two hours of such a day would find its first existing reading on the
// following one; that is one occurrence landing on the neighbouring date, not a
// lost or duplicated one, and it is not worth a special case.)
// The +1-minute scan matches svc-automation's _first_existing_instant.
const dstGapScanMinutes = 120

// slotAt builds the slot for (year, month, day) at s's hour:minute in loc.
//
// Two different absences are handled two DIFFERENT ways, and the asymmetry is
// deliberate — see the design doc, which records the reasoning for each:
//
//	THE DATE DOES NOT EXIST (31 February) → the occurrence is DROPPED.
//	  Owner ruling rc-aeef15360ab5, RFC 5545: the day the owner asked for is not
//	  in that month at all, so there is nothing to move it to that would not be a
//	  different day of the owner's month.
//	THE WALL CLOCK DOES NOT EXIST (the hour a spring-forward skips) → the
//	  occurrence MOVES FORWARD to the first reading the zone does have.
//	  The day IS there; it is merely an hour short. Dropping it means the owner
//	  gets nothing that day and nothing says so, which is the exact failure this
//	  feature exists to prevent. An hour late is late; it is not silence.
//
// ⚠️ Neither absence announces itself: time.Date NORMALISES a reading it cannot
// honour rather than refusing it, in both directions and by an amount of its own
// choosing. 31 February 2026 becomes 3 March; 00:30 on 2026-03-08 in
// America/Havana becomes 03-07 23:30 (backwards, into the previous day); 02:15
// on 2026-10-04 in Australia/Lord_Howe becomes 02:45 (forwards, thirty minutes,
// same date — which a day-only check waves straight through). So nothing here
// trusts time.Date's output: every candidate is READ BACK, component by
// component, and only an exact match counts as the reading that was asked for.
//
// 🔴 The slot is always CONSTRUCTED from (year, month, day, hour, minute, zone)
// and never derived from an offset of `now`. That is what makes it deterministic
// across the autumn side too: when a wall clock occurs TWICE, both passes
// construct the same instant, so the cursor sees the same slot and the second
// pass delivers nothing. An "elapsed since" style computation would produce two
// instants there and an ordering test alone would not catch it.
func slotAt(year int, month time.Month, day int, s ScheduledMessage, loc *time.Location) (time.Time, bool) {
	// Whether the DATE exists is a calendar question, asked zone-free — UTC has
	// no transitions, so nothing here can be perturbed by one.
	wall := time.Date(year, month, day, s.Hour, s.Minute, 0, 0, time.UTC)
	if wall.Year() != year || wall.Month() != month || wall.Day() != day {
		return time.Time{}, false
	}
	for shift := 0; shift <= dstGapScanMinutes; shift++ {
		want := wall.Add(time.Duration(shift) * time.Minute)
		slot := time.Date(want.Year(), want.Month(), want.Day(),
			want.Hour(), want.Minute(), 0, 0, loc)
		if slot.Year() == want.Year() && slot.Month() == want.Month() &&
			slot.Day() == want.Day() && slot.Hour() == want.Hour() &&
			slot.Minute() == want.Minute() {
			return slot, true
		}
	}
	return time.Time{}, false
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
