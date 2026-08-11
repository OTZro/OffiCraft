package main

// migration_00054_reaim_custom_cursors.go — T-49e7 round 2, the data half of
// retiring `custom`'s lookback window.
//
// 🔴 WHY THIS MIGRATION EXISTS. Round 1 scanned back 70 days for a `custom`
// schedule's most recent slot, so a sparse schedule's real past occurrence was
// invisible: months {1} × days {1} answered "no slot" for eleven months of the
// year. currentSlotKey seeds a new schedule's cursor with that answer, so such a
// row was created with last_fired_slot = '' — and slotIsAfterCursor reads an
// empty cursor as "nothing has been delivered, so fire". While the window was
// there that was harmless, because the tick could not see the old slot either.
// The calendar derivation that replaces the window CAN see it, so without this
// migration the first tick after the upgrade would deliver a message for an
// occurrence that may be a year old — and "missed slots are not backfilled" is
// this feature's stated behaviour.
//
// So every live `custom` row whose cursor is empty is re-aimed to the slot that
// is current AT MIGRATE TIME: "everything up to and including now has been dealt
// with", which is exactly what currentSlotKey means and what creation and every
// re-aiming edit already write.
//
// 🔴 THIS MIGRATION AFFECTS 0 ROWS AS OF 2026-08-11. The station's schedule
// table holds one row and it is `daily`; there are no `custom` rows at all
// (read-only check by the owner on that date). It is written anyway because a
// row can be created between that check and this landing, and because the same
// upgrade will run on any other installation. Do NOT rewrite this paragraph as
// "existing data was repaired" — that sentence would be false six months from
// now in a way nobody could check.
//
// 🔴 IT IS A GO MIGRATION, WHICH IS THE FIRST ONE IN THIS REPO, and it has to
// be: the value being written is a wall-clock slot in the schedule's own IANA
// zone, and SQLite cannot compute one. It calls the LIVE derivation
// (currentSlotKey) rather than a frozen copy on purpose — the row is being
// aimed at what the binary that is about to run the tick considers current, so
// a copy frozen at authoring time would aim it somewhere that binary disagrees
// with, which is the very mismatch this migration exists to remove.
//
// SCOPE. `cadence = 'custom'` and `last_fired_slot = ''`, and nothing else:
//
//   - a row that already carries a cursor has already been aimed, by creation
//     or by an edit, and moving it would either re-send or skip;
//   - a row PARKED under another cadence (it keeps its custom sets, owner
//     ruling rc-68c581070e55) is not aimed here, because its cursor belongs to
//     the cadence it is actually running; switching it back to `custom` re-aims
//     it through the normal edit path;
//   - a row whose zone will not load gets currentSlotKey == "" and is left
//     alone. The tick refuses that row anyway (no fallback zone), so writing
//     anything for it would be inventing a fact.

import (
	"context"
	"database/sql"
	"time"

	"github.com/pressly/goose/v3"
)

func init() {
	goose.AddNamedMigrationContext("00054_reaim_custom_cursors.go",
		upReaimCustomCursors, downReaimCustomCursors)
}

func upReaimCustomCursors(ctx context.Context, tx *sql.Tx) error {
	rows, err := tx.QueryContext(ctx, `
		SELECT id, timezone, custom_months, custom_days, custom_hours, custom_minutes
		  FROM scheduled_message
		 WHERE cadence = 'custom' AND last_fired_slot = ''`)
	if err != nil {
		return err
	}
	type aim struct{ id, key string }
	var aims []aim
	now := time.Now()
	for rows.Next() {
		var id, tz, months, days, hours, minutes string
		if err := rows.Scan(&id, &tz, &months, &days, &hours, &minutes); err != nil {
			rows.Close()
			return err
		}
		key := currentSlotKey(ScheduledMessage{
			ID:            id,
			Cadence:       ScheduledMessageCadenceCustom,
			Timezone:      tz,
			CustomMonths:  parseIntSet(months),
			CustomDays:    parseIntSet(days),
			CustomHours:   parseIntSet(hours),
			CustomMinutes: parseIntSet(minutes),
		}, now)
		if key == "" {
			// No slot has ever elapsed for this schedule (or its zone will not
			// load). An empty cursor is the correct state for that row: it
			// fires at the first real occurrence, which is what it would have
			// done anyway.
			continue
		}
		aims = append(aims, aim{id: id, key: key})
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	// Closed BEFORE any write: the write pool is a single connection, and an
	// UPDATE issued while this SELECT is still streaming shares it.
	if err := rows.Close(); err != nil {
		return err
	}
	for _, a := range aims {
		if _, err := tx.ExecContext(ctx,
			`UPDATE scheduled_message SET last_fired_slot = ? WHERE id = ?`, a.key, a.id); err != nil {
			return err
		}
	}
	return nil
}

// downReaimCustomCursors is deliberately a no-op, and the reason is not "there
// is nothing to undo".
//
// Restoring the empty cursor is possible and is the WRONG thing to do: an empty
// cursor on the older binary means "fire at the next occurrence", which is the
// state these rows were in, but this migration only ever runs backwards
// alongside a downgrade to a binary whose lookback window makes the aimed
// cursor harmless anyway. Blanking it would arm exactly the stale delivery the
// Up removed, for any row the old window can in fact see. The cursor keeps
// pointing at a slot that genuinely elapsed, which every version of this code
// reads the same way.
func downReaimCustomCursors(context.Context, *sql.Tx) error { return nil }
