package main

// T-83ef — migration 00059_custom_theme_table.
//
// 🔴 THE ONE ASSERTION THIS FILE EXISTS FOR is the byte-for-byte one. The
// ticket's hard condition for ever retiring the legacy settings row is that the
// moved themes read back IDENTICAL to what was stored, and "identical" has to
// mean bytes — a comparison of decoded structs would pass while key order,
// escaping or number formatting silently changed, and those are exactly the
// differences a later `git diff` of an exported theme would surface as damage.
// TestMigration00059ReassemblesTheLegacyArrayByteForByte re-joins the migrated
// rows and requires the result to equal the original stored string exactly.
//
// ⚠️ THE SCOPE OF THAT GUARANTEE, stated so nobody widens it by accident: it
// holds for values written by THIS SERVER's write path, which marshals the
// array with encoding/json and therefore stores it COMPACT (no whitespace
// between elements). The fixtures below are built the same way — through
// json.Marshal of ThemeBundleDTO values, not by hand-writing JSON text — so the
// test exercises the real producer. A hand-edited settings row carrying spaces
// or newlines between array elements would NOT reassemble byte-identically, and
// nothing in the product writes one.
//
// Down is destructive in the same sense 00047's is, and it is asserted rather
// than described: the table goes, and what the older binary reads afterwards is
// the legacy row it never stopped having.

import (
	"database/sql"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/pressly/goose/v3"
)

// t83efUpTo58 migrates to exactly 58 — the version BEFORE this migration — so a
// test can seed the legacy row into the world 00059 will actually meet. It never
// migrates to head: asserting anything about head would make this test fail on
// the next unrelated migration (the lesson 00047's round-trip test learned).
func t83efUpTo58(t *testing.T, db *sql.DB) {
	t.Helper()
	goose.SetBaseFS(embeddedMigrations)
	if err := goose.SetDialect("sqlite3"); err != nil {
		t.Fatalf("goose dialect: %v", err)
	}
	if err := goose.UpTo(db, "migrations", 58); err != nil {
		t.Fatalf("goose up to 58: %v", err)
	}
}

func t83efUpTo59(t *testing.T, db *sql.DB) {
	t.Helper()
	if err := goose.UpTo(db, "migrations", 59); err != nil {
		t.Fatalf("goose up to 59: %v", err)
	}
}

// t83efSeedLegacyThemes writes the array THE WAY THE SERVER WRITES IT —
// json.Marshal of the DTO slice — and returns the exact string it stored, which
// is the needle every byte-for-byte assertion below compares against.
func t83efSeedLegacyThemes(t *testing.T, db *sql.DB, bundles []ThemeBundleDTO) string {
	t.Helper()
	raw, err := json.Marshal(bundles)
	if err != nil {
		t.Fatalf("marshal fixture: %v", err)
	}
	if _, err := db.Exec(
		`INSERT INTO setting (key, value, updated_at) VALUES ('display.custom_themes', ?, 1)`,
		string(raw)); err != nil {
		t.Fatalf("seed legacy row: %v", err)
	}
	return string(raw)
}

// t83efFixture is three themes that between them carry the properties the
// migration must not disturb: a non-ASCII name (escaping), an optional block
// present on one theme and absent on another (key sets differ per element), and
// a data: URI (the payload that makes the legacy row large in the first place).
func t83efFixture() []ThemeBundleDTO {
	png := "data:image/png;base64,iVBORw0KGgo="
	logo := png
	return []ThemeBundleDTO{
		{
			Id:     "midnight",
			Name:   "午夜",
			Colors: map[string]string{"--color-bg": "#000000", "--color-fg": "#ffffff"},
			Logo:   &logo,
		},
		{
			Id:     "paper",
			Name:   "Paper \"quoted\" & <tagged>",
			Colors: map[string]string{"--color-bg": "rgb(255, 255, 255)"},
		},
		{
			Id:     "zebra",
			Name:   "Zebra",
			Colors: map[string]string{"--color-bg": "hsl(0 0% 50%)"},
		},
	}
}

// t83efReadRows returns the migrated bundles ordered by order_idx.
func t83efReadRows(t *testing.T, db *sql.DB) (ids []string, bundles []string) {
	t.Helper()
	rows, err := db.Query(`SELECT theme_id, bundle FROM custom_theme ORDER BY order_idx`)
	if err != nil {
		t.Fatalf("select custom_theme: %v", err)
	}
	defer rows.Close()
	for rows.Next() {
		var id, bundle string
		if err := rows.Scan(&id, &bundle); err != nil {
			t.Fatalf("scan: %v", err)
		}
		ids = append(ids, id)
		bundles = append(bundles, bundle)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows: %v", err)
	}
	return ids, bundles
}

func t83efLegacyValue(t *testing.T, db *sql.DB) (string, bool) {
	t.Helper()
	var v string
	err := db.QueryRow(`SELECT value FROM setting WHERE key = 'display.custom_themes'`).Scan(&v)
	if err == sql.ErrNoRows {
		return "", false
	}
	if err != nil {
		t.Fatalf("read legacy row: %v", err)
	}
	return v, true
}

func TestMigration00059ReassemblesTheLegacyArrayByteForByte(t *testing.T) {
	db, err := openSQLite(filepath.Join(t.TempDir(), "t83ef-bytes.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()
	t83efUpTo58(t, db)
	original := t83efSeedLegacyThemes(t, db, t83efFixture())
	t83efUpTo59(t, db)

	ids, bundles := t83efReadRows(t, db)
	if want := []string{"midnight", "paper", "zebra"}; len(ids) != len(want) {
		t.Fatalf("migrated %d themes, want %d (%v)", len(ids), len(want), ids)
	} else {
		for i := range want {
			if ids[i] != want[i] {
				t.Fatalf("order_idx %d is %q, want %q — the cockpit list IS this order", i, ids[i], want[i])
			}
		}
	}

	// The whole point: re-join the rows and require the ORIGINAL BYTES back.
	// json.Marshal of []json.RawMessage emits the same compact array the write
	// path emitted, so any difference here is a difference the migration made.
	raws := make([]json.RawMessage, len(bundles))
	for i, b := range bundles {
		raws[i] = json.RawMessage(b)
	}
	reassembled, err := json.Marshal(raws)
	if err != nil {
		t.Fatalf("reassemble: %v", err)
	}
	if string(reassembled) != original {
		t.Fatalf("reassembled bytes differ from what was stored.\n stored: %s\n  rejoined: %s", original, reassembled)
	}
}

func TestMigration00059LeavesTheLegacyRowInPlace(t *testing.T) {
	db, err := openSQLite(filepath.Join(t.TempDir(), "t83ef-keep.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()
	t83efUpTo58(t, db)
	original := t83efSeedLegacyThemes(t, db, t83efFixture())
	t83efUpTo59(t, db)

	// Up COPIES. If it ever starts moving, the retreat path in Down stops being
	// a retreat and this assertion is what says so.
	got, present := t83efLegacyValue(t, db)
	if !present {
		t.Fatal("display.custom_themes is gone after Up — Up must copy, never move")
	}
	if got != original {
		t.Fatalf("legacy row was rewritten by Up.\n before: %s\n  after: %s", original, got)
	}
}

func TestMigration00059DownDropsTheTableAndLeavesTheLegacyThemesReadable(t *testing.T) {
	db, err := openSQLite(filepath.Join(t.TempDir(), "t83ef-down.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()
	t83efUpTo58(t, db)
	original := t83efSeedLegacyThemes(t, db, t83efFixture())
	t83efUpTo59(t, db)
	if err := goose.DownTo(db, "migrations", 58); err != nil {
		t.Fatalf("goose down to 58: %v", err)
	}

	if _, err := db.Exec(`SELECT 1 FROM custom_theme`); err == nil {
		t.Fatal("custom_theme still exists after Down")
	}
	got, present := t83efLegacyValue(t, db)
	if !present || got != original {
		t.Fatalf("the older binary cannot read its themes back after Down (present=%v)\n want: %s\n  got: %s",
			present, original, got)
	}
}

func TestMigration00059WithNoSavedThemesIsAnEmptyTableNotAnError(t *testing.T) {
	for _, tc := range []struct {
		name  string
		seed  func(*testing.T, *sql.DB)
		about string
	}{
		{
			name:  "key absent",
			seed:  func(*testing.T, *sql.DB) {},
			about: "an install that never saved a theme has no row at all",
		},
		{
			name: "value empty",
			seed: func(t *testing.T, db *sql.DB) {
				if _, err := db.Exec(
					`INSERT INTO setting (key, value, updated_at) VALUES ('display.custom_themes', '', 1)`); err != nil {
					t.Fatalf("seed: %v", err)
				}
			},
			about: "settings load treats \"\" as none saved; so must this",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db, err := openSQLite(filepath.Join(t.TempDir(), "t83ef-empty.db"))
			if err != nil {
				t.Fatalf("open: %v", err)
			}
			defer db.Close()
			t83efUpTo58(t, db)
			tc.seed(t, db)
			t83efUpTo59(t, db)

			var n int
			if err := db.QueryRow(`SELECT COUNT(*) FROM custom_theme`).Scan(&n); err != nil {
				t.Fatalf("count (%s): %v", tc.about, err)
			}
			if n != 0 {
				t.Fatalf("%s: migrated %d rows from nothing", tc.about, n)
			}
		})
	}
}

// TestMigration00059CopiesBytesRatherThanRoundTrippingThroughTheDTO is the test
// that gives the byte-for-byte claim its TEETH, and it exists because the
// obvious alternative implementation — unmarshal each element into
// ThemeBundleDTO, marshal it back out — passes every other assertion in this
// file. It has to: the fixtures there are themselves produced by marshalling
// DTOs, so a DTO round-trip reproduces them exactly and the comparison cannot
// tell the two implementations apart.
//
// What separates them is an element carrying a key the DTO does not declare.
// That is not a hypothetical: the wire has grown fields over time (backgrounds
// and backgroundModes are recent), so a database written by a NEWER binary and
// then migrated by an older one, or a field retired later, both produce exactly
// this shape. A raw byte copy carries the unknown key through; a DTO round-trip
// drops it silently and reports success.
func TestMigration00059CopiesBytesRatherThanRoundTrippingThroughTheDTO(t *testing.T) {
	db, err := openSQLite(filepath.Join(t.TempDir(), "t83ef-unknown.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()
	t83efUpTo58(t, db)

	// Hand-built, COMPACT (no whitespace between elements or after separators) —
	// the same shape encoding/json emits, which is what keeps byte equality a
	// fair test rather than a formatting accident.
	const original = `[{"id":"future","name":"Future","colors":{"--color-bg":"#101010"},"someFieldThisBinaryDoesNotKnow":{"nested":[1,2,3]}}]`
	if _, err := db.Exec(
		`INSERT INTO setting (key, value, updated_at) VALUES ('display.custom_themes', ?, 1)`,
		original); err != nil {
		t.Fatalf("seed: %v", err)
	}
	t83efUpTo59(t, db)

	_, bundles := t83efReadRows(t, db)
	if len(bundles) != 1 {
		t.Fatalf("migrated %d rows, want 1", len(bundles))
	}
	raws := []json.RawMessage{json.RawMessage(bundles[0])}
	reassembled, err := json.Marshal(raws)
	if err != nil {
		t.Fatalf("reassemble: %v", err)
	}
	if string(reassembled) != original {
		t.Fatalf("an unknown field did not survive the move — the migration is decoding instead of copying.\n stored: %s\n rejoined: %s",
			original, reassembled)
	}
}

func TestMigration00059RefusesAValueThatIsNotAThemeArray(t *testing.T) {
	db, err := openSQLite(filepath.Join(t.TempDir(), "t83ef-bad.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()
	t83efUpTo58(t, db)
	if _, err := db.Exec(
		`INSERT INTO setting (key, value, updated_at) VALUES ('display.custom_themes', '{"not":"an array"}', 1)`); err != nil {
		t.Fatalf("seed: %v", err)
	}

	// Failing is the DESIGNED behaviour, not an oversight: the same bytes
	// already hard-fail settings load at boot, so an install carrying them
	// cannot serve either way. Skipping would upgrade it "successfully" with
	// its themes silently dropped — a worse outcome that nobody would notice.
	if err := goose.UpTo(db, "migrations", 59); err == nil {
		t.Fatal("Up accepted a non-array display.custom_themes; it must refuse rather than silently migrate zero themes")
	}
}
