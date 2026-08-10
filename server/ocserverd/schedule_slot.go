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
// 🔴 Looking back only ONE month is the bug this constant exists to prevent, and
// it fails silently: with day_of_month=31 and now = 15 February, the correct
// answer is 31 JANUARY — January is the nearest month that has a 31st, February
// having been skipped entirely per RFC 5545. A one-month search finds nothing,
// returns "no slot", and the schedule simply never fires, with nothing to
// observe. Two months would suffice for every real case (28/29/30/31 are never
// more than two months from a month that contains them), so twelve is
// deliberately generous headroom; its only job is to keep the loop bounded.
const monthlyLookbackMonths = 12

// mostRecentSlot returns the latest slot of s at or before now, computed as
// WALL-CLOCK TIME IN s.Timezone — never in the host's zone, never in UTC.
//
// ok=false means "no slot exists", which happens in exactly two ways, both of
// which the caller must treat as "do not deliver":
//   - s.Timezone will not load (see the 🔴 in ValidateScheduledMessageTimezone:
//     there is NO fallback zone, here or anywhere else in this feature);
//   - a monthly schedule whose day_of_month appears in no month within
//     monthlyLookbackMonths, or a cadence outside the closed set.
func mostRecentSlot(s ScheduledMessage, now time.Time) (time.Time, bool) {
	loc, err := time.LoadLocation(s.Timezone)
	if err != nil {
		return time.Time{}, false
	}
	local := now.In(loc)
	switch s.Cadence {
	case ScheduledMessageCadenceDaily:
		if slot := slotOn(local, s, loc); !slot.After(local) {
			return slot, true
		}
		// Today's slot has not arrived yet, so the most recent one is
		// yesterday's — computed from yesterday's DATE rather than by
		// subtracting 24h, so a DST shift moves the instant and leaves the wall
		// clock where the owner set it.
		return slotOn(local.AddDate(0, 0, -1), s, loc), true

	case ScheduledMessageCadenceWeekly:
		// Eight days covers it: today may or may not be the right weekday, and
		// if it is but its slot is still ahead, the answer is the same weekday
		// one week back.
		for back := 0; back <= 7; back++ {
			day := local.AddDate(0, 0, -back)
			if int(day.Weekday()) != s.DayOfWeek {
				continue
			}
			if slot := slotOn(day, s, loc); !slot.After(local) {
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

// slotOn stamps s's hour:minute onto the calendar date of day, in loc.
func slotOn(day time.Time, s ScheduledMessage, loc *time.Location) time.Time {
	return time.Date(day.Year(), day.Month(), day.Day(), s.Hour, s.Minute, 0, 0, loc)
}

// monthlySlot builds the slot for s.DayOfMonth within (year, month), reporting
// exists=false when that month has no such day.
//
// ⚠️ The check is NOT redundant: time.Date NORMALISES an out-of-range day
// instead of refusing it, so asking for 31 February 2026 yields 3 March 2026 —
// a perfectly valid time, in the wrong month, that would deliver the message
// three days late every February and look entirely normal doing it. Reading the
// components back and requiring them to be the ones asked for is what turns
// that silent rollover into the RFC 5545 "this month is skipped" the design
// calls for.
func monthlySlot(year int, month time.Month, s ScheduledMessage, loc *time.Location) (time.Time, bool) {
	slot := time.Date(year, month, s.DayOfMonth, s.Hour, s.Minute, 0, 0, loc)
	if slot.Year() != year || slot.Month() != month || slot.Day() != s.DayOfMonth {
		return time.Time{}, false
	}
	return slot, true
}

// slotKey renders a slot as the identifier stored in last_fired_slot. The same
// slot must always render to the same string — that equality IS the
// "already delivered" test.
func slotKey(slot time.Time) string {
	return slot.Format(slotKeyLayout)
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
