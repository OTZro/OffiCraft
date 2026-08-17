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
// 🔴 WHILE BOTH EXIST, THE LEGACY ROW IS THE TRUTH — and whoever writes the
// per-theme endpoints has to know that before writing them. This migration is a
// one-way copy: nothing keeps the two representations in step afterwards. So a
// write that lands ONLY in `custom_theme` makes `GET /api/settings` and the new
// table disagree, silently, with the settings face still serving the pre-upgrade
// answer. Two ways out, and the choice belongs to the change that adds the
// endpoints, not to this one: either that change retires the legacy row in the
// same package (so there is only one truth), or its write path maintains BOTH
// until it does. The same file carries the retirement precondition below.
//
// 🔴 RETIREMENT PRECONDITION, because Up is deliberately lossy-tolerant (see the
// failure posture): before any later change deletes `display.custom_themes`, it
// MUST verify that `custom_theme` holds as many rows as the legacy array holds
// elements. Up SKIPS elements it cannot key (below) rather than failing, which
// is safe only for as long as the legacy row is still there to hold them.
//
// ORDER IS RECORDED, NOT DERIVED. `order_idx` carries each element's position in
// the legacy array, and the reassembly sorts by it.
//
// ⚠️ BE ACCURATE ABOUT WHAT THAT BUYS TODAY, because the obvious justification is
// false and was measured false: with the current write path, `ORDER BY order_idx`
// and `ORDER BY rowid` CANNOT diverge. Both a new row's order_idx (MAX+1) and its
// rowid (max+1) move the same way, so delete-then-re-add, editing through the
// upsert's conflict path, and VACUUM all leave the two orderings identical
// (measured on this tree, 2026-08-17, all four cases). The column is kept because
// the list order is a FACT THIS MIGRATION KNOWS and rowid order is an accident
// that currently happens to agree — and because inserting at a position, or an
// import that reorders, needs a column that means position. Do NOT write that
// dropping it would reshuffle the owner's list; today it would not.
//
// FAILURE POSTURE — and it distinguishes two cases that a first draft of this
// migration wrongly treated alike:
//
//   - THE VALUE IS NOT A JSON ARRAY → the migration FAILS. This adds no new
//     blast radius: such an install ALREADY cannot start. loadAuthSettings
//     unmarshals this row into []ThemeBundleDTO and returns
//     "not a valid theme-bundle array", and server.go answers that with
//     `FATAL: load settings` and rc 1. It is dead either way, and failing loudly
//     during migrate is the better of two deaths.
//
//   - AN ELEMENT CANNOT BE KEYED (no id, blank id, not an object, or an id
//     another element already used) → that element is SKIPPED and the migration
//     succeeds. 🔴 This is the case the first draft got wrong, and the review
//     that caught it was right about why: those rows PARSE, so such an install
//     BOOTS FINE TODAY. Failing the migration would turn "your themes are a bit
//     odd" into "your station does not come up after the upgrade", which is a
//     far worse outcome than the one being prevented — and it would be caused by
//     data the product refuses to WRITE but has never refused to HOLD.
//
//     Skipping loses nothing WHILE THE LEGACY ROW IS STILL THERE, which is
//     exactly why the retirement precondition above is not optional. Every skip
//     is printed during migrate so an upgrade that dropped something says so.

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
//
// 🔴 THE CHECK IS THE ONLY THING THAT MAKES `theme_id` AND `bundle` ONE FACT
// RATHER THAN TWO. The id is stored twice by construction — once as the key,
// once inside the JSON — and nothing about the Go types stops a caller writing
// `PutCustomTheme("blue", {"id":"red"})`. Measured before this constraint
// existed: that write was accepted, so was a bundle that was not JSON at all,
// and so was an empty theme_id. The rows this migration creates always satisfy
// it (the key is DERIVED from the bundle's own id), so the constraint costs
// nothing here; it exists for every write that comes after, and it is free to
// add only while the table is empty. Losing it means the cockpit can be served a
// theme whose id disagrees with the id it is filed under, which is precisely the
// shape that makes "delete theme X" delete something else.
const customThemeTableDDL = `
CREATE TABLE custom_theme (
  theme_id   TEXT PRIMARY KEY,
  bundle     TEXT NOT NULL,
  order_idx  INTEGER NOT NULL,
  updated_at REAL NOT NULL DEFAULT 0,
  CHECK (theme_id <> ''
         AND json_valid(bundle)
         AND json_extract(bundle, '$.id') = theme_id)
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
	seen := make(map[string]bool, len(elements))
	for i, el := range elements {
		// Only the id is decoded. Everything else stays as the bytes that were
		// stored — see the byte-for-byte note at the top of this file.
		var head struct {
			ID string `json:"id"`
		}
		// Every `continue` below is a SKIP, not a failure, and the reason is the
		// same in all three: an element shaped like this parses, so the install
		// carrying it starts today. See the failure posture at the top.
		if err := json.Unmarshal(el, &head); err != nil {
			skipCustomThemeElement(i, "not a JSON object")
			continue
		}
		if head.ID == "" {
			skipCustomThemeElement(i, "no usable id")
			continue
		}
		if seen[head.ID] {
			// The write path has always refused duplicates
			// (validateThemeBundles), so this is corruption rather than a
			// supported state — but the primary key would turn it into a failed
			// migration, and a station that will not start is a worse answer than
			// a theme left behind in the legacy row.
			skipCustomThemeElement(i, "id "+head.ID+" already used by an earlier element")
			continue
		}
		seen[head.ID] = true
		rows = append(rows, row{id: head.ID, bundle: el, idx: i})
	}
	for _, r := range rows {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO custom_theme (theme_id, bundle, order_idx, updated_at) VALUES (?, ?, ?, 0)`,
			r.id, string(r.bundle), r.idx); err != nil {
			return fmt.Errorf("migration 00059: insert theme %q: %w", r.id, err)
		}
	}
	return nil
}

// skipCustomThemeElement prints what was left behind. It is not decoration: a
// skip is only safe because the legacy row still holds the element, and an
// upgrade that quietly dropped something the owner can see in the cockpit would
// be indistinguishable from one that moved everything. goose's own output goes
// to stdout, so this lands in the same place as the `OK 00059_...` line.
func skipCustomThemeElement(i int, why string) {
	fmt.Printf("[migration 00059] SKIPPED %s[%d]: %s — it stays in the legacy row\n",
		legacyCustomThemesKey, i, why)
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
