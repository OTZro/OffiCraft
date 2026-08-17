package main

// migration_00059_custom_theme_table.go — T-83ef, the storage half of moving
// custom themes OUT of the settings row they have lived in since T-16a1 P2.
//
// 🔴 WHY THIS EXISTS. Every saved theme, INCLUDING every image the owner picked
// (avatars, logo, nav icons, canvas backgrounds — all base64 data: URIs), is
// serialised into ONE settings value under the key `display.custom_themes`, and
// that value is written by whole-array replace. Two consequences, both measured
// rather than argued:
//
//   - Editing ONE theme resends ALL of them. There is no partial write path at
//     all (api_settings.go: json.Marshal(newCustomThemes) → PutSetting).
//   - `GET /api/settings` is 639,270 bytes on the production install and
//     `custom_themes` alone is 626,721 of it — 98%. That figure is not new here;
//     it is recorded verbatim in frontend/src/lib/sharedSnapshot.ts, which
//     exists because five unrelated cockpit consumers were each downloading it.
//     The same weight is what makes the `get_settings` MCP tool unusable for
//     agents that only want a boolean out of it.
//
// One theme per ROW is what makes "write one theme" expressible at all.
//
// 🔴 WHY THE BUNDLE IS STORED AS THE ELEMENT'S RAW JSON, NOT AS COLUMNS. The
// ticket's hard requirement is that the moved data reads back BYTE-FOR-BYTE
// identical before anything old is removed. Copying each array element's own
// bytes verbatim makes that a mechanical fact: re-joining the rows in order
// reproduces the original array string exactly, and a diff proves it. Unpacking
// into columns would round-trip through unmarshal/marshal, where key order,
// whitespace and Unicode escaping are all free to change — the reassembled
// bytes could differ while nothing is actually wrong, and then the one check
// that is supposed to authorise the switch can no longer be run at all.
//
// 🔴 WHY IT IS A GO MIGRATION. Splitting a JSON array requires parsing JSON.
// SQLite cannot, so a .sql migration cannot express this. It is the second Go
// migration in this repo (00054 was the first); Go migrations cannot live under
// migrations/ because that directory is embedded as *.sql — runMigrations in
// migrate.go says so, and `grep -rn AddNamedMigrationContext server/ocserverd`
// is how you find them.
//
// 🔴 THE OLD SETTINGS ROW IS NOT TOUCHED. Up copies; it does not move and it
// does not delete. Both representations exist after this migration, which is
// what makes Down a genuine retreat (drop the table and the older binary reads
// exactly what it read before, because it was never edited) and what lets the
// byte-for-byte comparison be run against a live pair rather than a backup.
// Retiring `display.custom_themes` is a SEPARATE decision on a later change,
// once the new path has actually carried the owner's data.
//
// ORDER IS DATA. The cockpit's theme list is the array's order and there is no
// reordering UI (ThemeSettings.tsx says so), so the index is not cosmetic: lose
// it and the owner's list silently reshuffles on upgrade. `order_idx` carries
// the element's position; it is what the reassembly sorts by.
//
// FAILURE POSTURE. A `display.custom_themes` value that is absent or empty
// migrates zero rows and is not an error — that is what an install with no
// saved themes looks like. A value that is present but is NOT a JSON array
// FAILS the migration rather than being skipped, and that is deliberate: the
// same bytes already hard-fail settings load at boot (loadSettings returns
// "not a valid theme-bundle array"), so such an install cannot serve either
// way. Skipping would turn "this install is broken" into "this install
// upgraded fine and lost its themes", which is strictly worse and invisible.

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/pressly/goose/v3"
)

func init() {
	goose.AddNamedMigrationContext("00059_custom_theme_table.go",
		upCustomThemeTable, downCustomThemeTable)
}

// customThemeTableDDL is the new home: one row per saved theme, keyed by the
// theme's own id (the same slug the wire has always used, ^[a-z0-9][a-z0-9-]{1,63}$).
//
// `bundle` is the theme's JSON OBJECT as text — the element's own bytes on the
// way in from the legacy array, and the marshalled DTO on every write after
// that. `updated_at` is 0 for migrated rows on purpose: nothing knows when the
// owner last edited a theme that lived inside a shared settings value, and
// stamping migrate time would invent a fact that reads like an edit.
const customThemeTableDDL = `
CREATE TABLE custom_theme (
  theme_id   TEXT PRIMARY KEY,
  bundle     TEXT NOT NULL,
  order_idx  INTEGER NOT NULL,
  updated_at REAL NOT NULL DEFAULT 0
)`

// legacyCustomThemesKey is the settings key the array has lived under. It is
// spelled out here rather than referencing settingDisplayCustomThemes because a
// migration must keep describing the schema as it was AT THIS VERSION: if that
// constant is renamed or retired later, this migration must still run the same
// way on an install upgrading from before it existed.
const legacyCustomThemesKey = "display.custom_themes"

func upCustomThemeTable(ctx context.Context, tx *sql.Tx) error {
	if _, err := tx.ExecContext(ctx, customThemeTableDDL); err != nil {
		return err
	}
	var raw string
	err := tx.QueryRowContext(ctx,
		`SELECT value FROM setting WHERE key = ?`, legacyCustomThemesKey).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && raw == "") {
		// No themes were ever saved. The table exists and is empty, which is the
		// same state a fresh install starts in.
		return nil
	}
	if err != nil {
		return err
	}
	var elements []json.RawMessage
	if err := json.Unmarshal([]byte(raw), &elements); err != nil {
		return fmt.Errorf("migration 00059: setting %s is not a JSON array: %w",
			legacyCustomThemesKey, err)
	}
	type row struct {
		id     string
		bundle []byte
		idx    int
	}
	rows := make([]row, 0, len(elements))
	for i, el := range elements {
		// Only the id is decoded. Everything else stays as the bytes that were
		// stored — see the byte-for-byte note at the top of this file.
		var head struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(el, &head); err != nil {
			return fmt.Errorf("migration 00059: %s[%d] is not a JSON object: %w",
				legacyCustomThemesKey, i, err)
		}
		if head.ID == "" {
			return fmt.Errorf("migration 00059: %s[%d] has no id",
				legacyCustomThemesKey, i)
		}
		rows = append(rows, row{id: head.ID, bundle: el, idx: i})
	}
	for _, r := range rows {
		// A duplicate id violates the primary key and fails the migration. The
		// write path has always refused duplicates (validateThemeBundles), so a
		// row carrying two of the same id is corruption, not a supported state,
		// and quietly collapsing them would lose one of the owner's themes.
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO custom_theme (theme_id, bundle, order_idx, updated_at) VALUES (?, ?, ?, 0)`,
			r.id, string(r.bundle), r.idx); err != nil {
			return fmt.Errorf("migration 00059: insert theme %q: %w", r.id, err)
		}
	}
	return nil
}

// downCustomThemeTable drops the table, and that loses nothing the binary below
// this migration could have read: `display.custom_themes` was copied, never
// moved, so the older binary finds its themes exactly where it left them.
//
// ⚠️ The one thing it DOES lose is theme edits made through the new per-theme
// write path while this migration was applied — those rows have no older place
// to be put back into, because the legacy row is not maintained in parallel.
// That is the accepted cost of a one-way copy, and it is why retiring the
// legacy row is a separate, later decision rather than part of this change:
// while both exist, a downgrade costs at most the edits made since the upgrade.
func downCustomThemeTable(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, `DROP TABLE custom_theme`)
	return err
}
