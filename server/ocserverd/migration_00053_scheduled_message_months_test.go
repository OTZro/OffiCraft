package main

// migration_00053_scheduled_message_months_test.go — the compatibility half of
// T-49e7 round 2.
//
// 🔴 THE FAILURE THIS FILE EXISTS FOR IS TOTALLY SILENT. `custom` gained a
// fourth intersected set, and the month condition is false for the empty set,
// so a column added WITHOUT a backfill stops every `custom` schedule already in
// the database — permanently, with no error, no log line, and a cockpit card
// that looks exactly like a working one. Nobody finds out until somebody
// notices a message that stopped arriving weeks ago.
//
// So 00053 backfills every row carrying a custom choice to all twelve months,
// which is what those rows have always MEANT, and this file holds it to two
// claims that a test reading back the field it just wrote could not make:
//
//	the row is otherwise untouched — every column round-trips field for field
//	  across the upgrade, so the backfill did not quietly renormalise anything
//	  else while it was in there;
//	the DELIVERIES are unchanged — the set of readings the upgraded row fires at
//	  equals the set the PRE-ROUND-2 rule named, computed independently below
//	  rather than by asking the new code what it thinks the old rule was.
//
// Fixtures are UTC on purpose: this file is about the migration, and a DST zone
// would mix in the skip/repeat behaviour that the two tz-specific files already
// own.

import (
	"database/sql"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/pressly/goose/v3"
)

// preRound2Readings enumerates the readings the day × hour × minute rule names
// in [from, to], INDEPENDENTLY of mostRecentSlot.
//
// 🔴 It is a re-implementation and not a call into production with all twelve
// months filled in, because "all twelve months" is precisely what the migration
// is being tested for. Asking the new code to describe the old rule would make
// this test agree with the backfill by construction, whatever the backfill did.
func preRound2Readings(days, hours, minutes []int, from, to time.Time) map[string]bool {
	out := map[string]bool{}
	for day := from; !day.After(to); day = day.AddDate(0, 0, 1) {
		if !intSetContains(days, day.Day()) {
			continue
		}
		for _, h := range hours {
			for _, m := range minutes {
				at := time.Date(day.Year(), day.Month(), day.Day(), h, m, 0, 0, time.UTC)
				if at.Before(from) || at.After(to) {
					continue
				}
				out[slotKey(at)] = true
			}
		}
	}
	return out
}

// slotsReported walks a window and collects every distinct slot mostRecentSlot
// reports — the readings this schedule would deliver at, given a tick fine
// enough to see each of them.
func slotsReported(sm ScheduledMessage, from, to time.Time, step time.Duration) map[string]bool {
	out := map[string]bool{}
	for at := from; !at.After(to); at = at.Add(step) {
		if slot, ok := mostRecentSlot(sm, at); ok && !slot.Before(from) {
			out[slotKey(slot)] = true
		}
	}
	return out
}

// preRound2Row is a scheduled_message row as 00052 could write it: no months,
// because the column did not exist.
type preRound2Row struct {
	id                   string
	cadence              string
	days, hours, minutes string
}

// TestMigration00053BackfillsExistingCustomRowsToEveryMonth is the whole ticket
// for anyone who already has schedules.
//
// Red when: the backfill is dropped or narrowed (the rows land with no months
// and stop firing), or it reaches rows it should not (a schedule that was never
// `custom` suddenly carries a set), or the ADD COLUMN disturbs another column.
func TestMigration00053BackfillsExistingCustomRowsToEveryMonth(t *testing.T) {
	db, err := openSQLite(filepath.Join(t.TempDir(), "sched-months.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()

	// The world as 00052 left it. UpTo 52, never all the way — running the
	// migration under test before the fixtures exist would test nothing.
	goose.SetBaseFS(embeddedMigrations)
	if err := goose.SetDialect("sqlite3"); err != nil {
		t.Fatalf("dialect: %v", err)
	}
	if err := goose.UpTo(db, "migrations", 52); err != nil {
		t.Fatalf("up to 52: %v", err)
	}

	rows := []preRound2Row{
		// The owner's "every 20 minutes" — the case 00052 was written for.
		{"sch-old-20min", ScheduledMessageCadenceCustom,
			"1,2,3,4,5,6,7,8,9,10,11,12,13,14,15,16,17,18,19,20,21,22,23,24,25,26,27,28,29,30,31",
			"0,1,2,3,4,5,6,7,8,9,10,11,12,13,14,15,16,17,18,19,20,21,22,23", "0,20,40"},
		// A sparse one, including a 31st so February's missing day is in play.
		{"sch-old-sparse", ScheduledMessageCadenceCustom, "1,15,31", "9", "0"},
		// A row SWITCHED AWAY from `custom`: it keeps its sets unread (owner
		// ruling rc-68c581070e55), so it carries a custom choice under another
		// cadence and must be backfilled too — otherwise switching back later
		// silently drops the months.
		{"sch-old-parked", ScheduledMessageCadenceDaily, "1,15", "9", "0"},
		// And one that was never `custom` at all: it must stay empty, because
		// giving it months would put a set on a row that never chose one.
		{"sch-old-daily", ScheduledMessageCadenceDaily, "", "", ""},
	}
	for _, r := range rows {
		if _, err := db.Exec(`
			INSERT INTO scheduled_message
			  (id, member_id, label, body, cadence, day_of_week, day_of_month, hour, minute,
			   custom_days, custom_hours, custom_minutes, timezone, status,
			   last_fired_slot, last_fired_ts, created_ts)
			VALUES (?, 'mira', 'label '||?, 'body '||?, ?, 3, 17, 9, 5, ?, ?, ?, 'UTC', 'enabled',
			        '2026-01-15T09:00+00:00', 1786000000.5, 1785000000.25)`,
			r.id, r.id, r.id, r.cadence, r.days, r.hours, r.minutes); err != nil {
			t.Fatalf("seed %s: %v", r.id, err)
		}
	}

	before := map[string]ScheduledMessage{}
	for _, r := range rows {
		before[r.id] = readScheduledMessageRowPre53(t, db, r.id)
	}

	if err := goose.UpTo(db, "migrations", 53); err != nil {
		t.Fatalf("00053 up: %v", err)
	}
	dal := NewDAL(db)

	everyMonth := canonicalIntSet(intRange(1, 12))
	for _, r := range rows {
		t.Run(r.id, func(t *testing.T) {
			got, err := dal.GetScheduledMessage(r.id)
			if err != nil || got == nil {
				t.Fatalf("read back: %v %v", got, err)
			}

			// (1) The months themselves.
			wantMonths := everyMonth
			if r.days == "" {
				wantMonths = ""
			}
			if canonicalIntSet(got.CustomMonths) != wantMonths {
				t.Fatalf("months are [%s], want [%s]. A live custom row with no months stops "+
					"firing forever, and nothing anywhere says so",
					canonicalIntSet(got.CustomMonths), wantMonths)
			}

			// (2) Everything else, field for field. The months are the only
			// column this migration may move.
			// Both sides come out of the DAL, whose read path canonicalises the
			// sets, so a plain DeepEqual over the whole struct is exact: only
			// custom_months is allowed to differ, and it is copied across first.
			was := before[r.id]
			was.CustomMonths = got.CustomMonths
			if !reflect.DeepEqual(was, *got) {
				t.Fatalf("the upgrade changed a column other than custom_months\n before: %+v\n after:  %+v",
					was, *got)
			}

			// (3) The round trip is still identity: writing the upgraded row
			// back and re-reading it changes nothing.
			if err := dal.PutScheduledMessage(*got); err != nil {
				t.Fatalf("rewrite: %v", err)
			}
			again, err := dal.GetScheduledMessage(r.id)
			if err != nil || again == nil {
				t.Fatalf("second read: %v %v", again, err)
			}
			if !reflect.DeepEqual(*got, *again) {
				t.Fatalf("the upgraded row is not a fixed point of save→read\n %+v\n %+v", *got, *again)
			}
		})
	}

	// (4) The deliveries. Two months of readings, at a one-minute tick so no
	// reading of the dense schedule can slip between samples, compared against
	// the pre-round-2 rule computed independently.
	from := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, time.March, 1, 0, 0, 0, 0, time.UTC)
	for _, r := range rows {
		if r.cadence != ScheduledMessageCadenceCustom {
			continue
		}
		t.Run(r.id+"/deliveries", func(t *testing.T) {
			got, err := dal.GetScheduledMessage(r.id)
			if err != nil || got == nil {
				t.Fatalf("read back: %v %v", got, err)
			}
			want := preRound2Readings(parseIntSet(r.days), parseIntSet(r.hours), parseIntSet(r.minutes), from, to)
			if len(want) == 0 {
				t.Fatal("the oracle named no readings — the comparison below would be vacuous")
			}
			have := slotsReported(*got, from, to, time.Minute)
			for key := range want {
				if !have[key] {
					t.Fatalf("%s is a reading this schedule fired at before the upgrade and does not "+
						"after it — the backfill did not reach this row", key)
				}
			}
			for key := range have {
				if !want[key] {
					t.Fatalf("%s is a reading this schedule did NOT fire at before the upgrade. "+
						"The month set may only ever narrow a schedule, never widen one", key)
				}
			}
		})
	}
}

// readScheduledMessageRowPre53 reads a row from the pre-00053 table, whose
// SELECT list cannot mention custom_months.
func readScheduledMessageRowPre53(t *testing.T, db *sql.DB, id string) ScheduledMessage {
	t.Helper()
	var m ScheduledMessage
	var days, hours, minutes string
	err := db.QueryRow(`
		SELECT id, member_id, label, body, cadence, day_of_week, day_of_month, hour, minute,
		       custom_days, custom_hours, custom_minutes, timezone, status,
		       last_fired_slot, last_fired_ts, created_ts
		  FROM scheduled_message WHERE id = ?`, id).
		Scan(&m.ID, &m.MemberID, &m.Label, &m.Body, &m.Cadence,
			&m.DayOfWeek, &m.DayOfMonth, &m.Hour, &m.Minute,
			&days, &hours, &minutes, &m.Timezone,
			&m.Status, &m.LastFiredSlot, &m.LastFiredTS, &m.CreatedTS)
	if err != nil {
		t.Fatalf("read pre-53 row %s: %v", id, err)
	}
	m.CustomDays, m.CustomHours, m.CustomMinutes = parseIntSet(days), parseIntSet(hours), parseIntSet(minutes)
	return m
}
