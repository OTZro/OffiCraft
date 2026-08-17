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

// t83efReadRows returns the migrated bundles ordered by order_idx, together with
// the order_idx values themselves.
//
// 🔴 THE INDEXES ARE RETURNED BECAUSE READING BACK IN THE RIGHT ORDER PROVES
// NOTHING ON ITS OWN — measured, not reasoned: a mutant writing order_idx = 0
// for EVERY row left every assertion in this file green. SQLite answers
// `ORDER BY order_idx` on an all-equal column in rowid order, which is insertion
// order, which is the very order the migration inserted them in. So the column
// can be complete garbage and the list still comes back correct. Only asserting
// the VALUES separates "the position was recorded" from "the position happened
// to fall out of the insert order".
//
// ⚠️ AND DO NOT DRESS THAT UP AS A USER-VISIBLE BUG, because the obvious story
// is false. An earlier version of this comment said the owner's list would
// silently reshuffle once a theme was deleted and re-added; an independent
// review disproved it and the measurement was repeated here (2026-08-17):
// order_idx and rowid move together under every write path that exists today
// (append, delete-then-re-add of a middle row, of the highest row, edit through
// the upsert's conflict path, and after VACUUM), so the two orderings cannot
// currently diverge at all. What this assertion protects is the MIGRATION's own
// contract — the array position is a fact this code knows and must write down —
// not a bug anyone can reproduce in the product today.
func t83efReadRows(t *testing.T, db *sql.DB) (ids []string, bundles []string, idxs []int) {
	t.Helper()
	rows, err := db.Query(`SELECT theme_id, bundle, order_idx FROM custom_theme ORDER BY order_idx`)
	if err != nil {
		t.Fatalf("select custom_theme: %v", err)
	}
	defer rows.Close()
	for rows.Next() {
		var id, bundle string
		var idx int
		if err := rows.Scan(&id, &bundle, &idx); err != nil {
			t.Fatalf("scan: %v", err)
		}
		ids = append(ids, id)
		bundles = append(bundles, bundle)
		idxs = append(idxs, idx)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows: %v", err)
	}
	return ids, bundles, idxs
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

	ids, bundles, idxs := t83efReadRows(t, db)
	if want := []string{"midnight", "paper", "zebra"}; len(ids) != len(want) {
		t.Fatalf("migrated %d themes, want %d (%v)", len(ids), len(want), ids)
	} else {
		for i := range want {
			if ids[i] != want[i] {
				t.Fatalf("position %d is %q, want %q — the cockpit list IS this order", i, ids[i], want[i])
			}
			// The VALUE, not just the resulting sequence — see t83efReadRows.
			if idxs[i] != i {
				t.Fatalf("theme %q carries order_idx %d, want %d: the array position was not recorded, so the list order is currently an accident of insertion order",
					ids[i], idxs[i], i)
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

	_, bundles, _ := t83efReadRows(t, db)
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

// TestMigration00059SkipsUnkeyableElementsInsteadOfBrickingTheUpgrade pins the
// distinction the first draft of this migration got wrong.
//
// An element with no id, a blank id, a duplicate id, or one that is not an
// object at all PARSES — `loadAuthSettings` unmarshals the row into
// []ThemeBundleDTO and is perfectly happy with every one of them. So an install
// carrying such an element STARTS TODAY. Failing the migration on it would mean
// the station does not come up after an upgrade, caused by data the product
// refuses to write but has never refused to hold. The element is skipped and
// stays in the legacy row, which is still there.
//
// 🔴 THIS IS ALSO WHY THE RETIREMENT PRECONDITION EXISTS. A skip is only safe
// while the legacy row holds what was skipped, so this test asserts BOTH halves:
// the good themes moved, AND the legacy row is untouched.
func TestMigration00059SkipsUnkeyableElementsInsteadOfBrickingTheUpgrade(t *testing.T) {
	db, err := openSQLite(filepath.Join(t.TempDir(), "t83ef-skip.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()
	t83efUpTo58(t, db)

	// good, no id, blank id, duplicate of the first, not an object, good.
	const original = `[{"id":"good-one","name":"A"},` +
		`{"name":"no id at all"},` +
		`{"id":"","name":"blank id"},` +
		`{"id":"good-one","name":"duplicate"},` +
		`12345,` +
		`{"id":"good-two","name":"B"}]`
	if _, err := db.Exec(
		`INSERT INTO setting (key, value, updated_at) VALUES ('display.custom_themes', ?, 1)`,
		original); err != nil {
		t.Fatalf("seed: %v", err)
	}

	// The whole point: this SUCCEEDS.
	t83efUpTo59(t, db)

	ids, _, idxs := t83efReadRows(t, db)
	want := []string{"good-one", "good-two"}
	if len(ids) != len(want) {
		t.Fatalf("migrated %v, want %v", ids, want)
	}
	for i := range want {
		if ids[i] != want[i] {
			t.Fatalf("position %d is %q, want %q", i, ids[i], want[i])
		}
	}
	// The positions are the ORIGINAL array indexes, not 0 and 1: a skip must not
	// renumber what survived, or the surviving themes quietly change places.
	if idxs[0] != 0 || idxs[1] != 5 {
		t.Fatalf("order_idx after skips is %v, want [0 5] — the original array positions", idxs)
	}
	got, present := t83efLegacyValue(t, db)
	if !present || got != original {
		t.Fatalf("what was skipped must still be in the legacy row (present=%v, unchanged=%v)",
			present, got == original)
	}
}

// TestMigration00059TableRefusesAnIdThatDisagreesWithItsBundle pins the CHECK
// constraint, which is the only thing making the id ONE fact rather than two.
// The migration itself cannot violate it (it derives the key from the bundle),
// so this drives the constraint directly — the way every write after this
// migration will meet it.
func TestMigration00059TableRefusesAnIdThatDisagreesWithItsBundle(t *testing.T) {
	db, err := openSQLite(filepath.Join(t.TempDir(), "t83ef-check.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()
	t83efUpTo58(t, db)
	t83efUpTo59(t, db)

	// 🔴 EACH CASE USES ITS OWN theme_id, and that is not tidiness. With a shared
	// id they share a primary key: remove the CHECK and the first case's row
	// lands, after which the second case is refused for being a DUPLICATE and
	// reports PASS while proving nothing. Measured — the mutant that drops the
	// constraint reddened two of these three, and the third stayed green for
	// exactly that reason.
	for _, tc := range []struct {
		name, id, bundle string
	}{
		{"key disagrees with the bundle's own id", "blue", `{"id":"red"}`},
		{"bundle is not JSON", "green", `not json at all`},
		{"empty key", "", `{"id":""}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := db.Exec(
				`INSERT INTO custom_theme (theme_id, bundle, order_idx) VALUES (?, ?, 0)`,
				tc.id, tc.bundle)
			if err == nil {
				t.Fatalf("accepted %s — the id would then disagree with the id it is filed under, which is how \"delete theme X\" deletes something else",
					tc.name)
			}
		})
	}
	// The control: a well-formed pair still goes in. Without this, a CHECK that
	// refused everything would pass the three cases above.
	if _, err := db.Exec(
		`INSERT INTO custom_theme (theme_id, bundle, order_idx) VALUES ('blue', '{"id":"blue"}', 0)`); err != nil {
		t.Fatalf("the constraint refuses a legitimate row: %v", err)
	}
}

// TestMigration00059ByteForByteWindowClosesOnTheFirstWriteThroughTheDAL puts a
// mechanical carrier under a TIME CONSTRAINT that was, until this test, only
// prose in two file headers.
//
// The byte-for-byte guarantee is about what the MIGRATION wrote. It says nothing
// about the table five minutes later, because the first per-theme write replaces
// that theme's bytes with whatever the caller marshalled — correctly, and by
// design. The consequence is operational and easy to get wrong: the comparison
// that authorises retiring `display.custom_themes` has to be run BEFORE the
// endpoints start writing. Afterwards it will differ, and differing will mean
// nothing.
//
// So this test asserts both halves in order: identical immediately after the
// migration, and NOT identical after one ordinary write.
func TestMigration00059ByteForByteWindowClosesOnTheFirstWriteThroughTheDAL(t *testing.T) {
	db, err := openSQLite(filepath.Join(t.TempDir(), "t83ef-window.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()
	t83efUpTo58(t, db)
	original := t83efSeedLegacyThemes(t, db, t83efFixture())
	t83efUpTo59(t, db)

	d := NewDAL(db)
	rejoin := func() string {
		list, err := d.ListCustomThemes()
		if err != nil {
			t.Fatalf("list: %v", err)
		}
		raws := make([]json.RawMessage, len(list))
		for i, ct := range list {
			raws[i] = json.RawMessage(ct.Bundle)
		}
		b, err := json.Marshal(raws)
		if err != nil {
			t.Fatalf("reassemble: %v", err)
		}
		return string(b)
	}

	// Half one: what the DAL reads is what the migration wrote — the two layers
	// agree, so the comparison can be run through either.
	if got := rejoin(); got != original {
		t.Fatalf("straight after the migration the DAL does not read back the stored bytes.\n stored: %s\n    got: %s",
			original, got)
	}

	// Half two: one ordinary write and the window is shut.
	if err := d.PutCustomTheme("paper", `{"id":"paper","name":"edited"}`); err != nil {
		t.Fatalf("put: %v", err)
	}
	if got := rejoin(); got == original {
		t.Fatal("the reassembly still equals the legacy row after a theme was rewritten — then this comparison cannot tell 'nothing was lost in the move' from 'nothing has happened since', and using it to authorise retiring the legacy row would be unsound")
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
