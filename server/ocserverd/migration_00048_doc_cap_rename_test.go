package main

// migration_00048_doc_cap_rename_test.go — the compatibility half of T-ae38.
//
// The live install had `doc.cap_chars` = 15000. Migration 00048 RENAMES that row
// to `doc.cap_chars.manual`, and the three role-journal keys stay ABSENT so they
// read their code-side defaults. Every failure mode here is silent:
//
//   - the UPDATE never runs        → the manual cap silently drops 15000 → 10000,
//     and four already-over-10000 manuals become shrink-only overnight;
//   - the old row is left BEHIND   → the DB holds both keys, `get_settings` shows
//     an unsuffixed one again, and the whole reason for renaming is undone;
//   - the value is not carried     → same as the first, with a value nobody chose.
//
// None of those produce an error anywhere, which is why this is a test and not a
// comment in the .sql file.

import (
	"path/filepath"
	"testing"

	"github.com/pressly/goose/v3"
)

func TestMigration00048RenamesTheSharedDocCapToManual(t *testing.T) {
	db, err := openSQLite(filepath.Join(t.TempDir(), "doc-cap-rename.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()
	if err := runMigrations(db); err != nil {
		t.Fatalf("goose up: %v", err)
	}
	// The world as 00047 left it — one shared key, raised by the owner.
	if err := goose.DownTo(db, "migrations", 47); err != nil {
		t.Fatalf("down to 47: %v", err)
	}
	if _, err := db.Exec(
		`INSERT INTO setting (key, value, updated_at) VALUES ('doc.cap_chars', '15000', 1)`); err != nil {
		t.Fatalf("seed the pre-00048 shared key: %v", err)
	}

	if err := runMigrations(db); err != nil {
		t.Fatalf("00048 up: %v", err)
	}

	value := func(key string) *string {
		var v string
		switch err := db.QueryRow(`SELECT value FROM setting WHERE key = ?`, key).Scan(&v); err {
		case nil:
			return &v
		default:
			return nil
		}
	}

	if got := value(settingDocCapCharsManual); got == nil || *got != "15000" {
		t.Fatalf("the owner's raised cap must arrive at %s intact, got %v",
			settingDocCapCharsManual, got)
	}
	// The old key must be GONE, not merely shadowed. A row left behind is the
	// unsuffixed key this ticket exists to remove — and settings.go would never
	// read it, so nothing else would ever complain.
	if got := value("doc.cap_chars"); got != nil {
		t.Fatalf("the legacy doc.cap_chars row must be renamed away, still holds %q", *got)
	}
	// The three journal keys stay ABSENT so they read their code defaults. A
	// migration that helpfully seeded them with 15000 would hand Insight and
	// Learning a number the owner never chose for them, and Duty a 15× one.
	for _, key := range []string{
		settingDocCapCharsDuty, settingDocCapCharsInsight, settingDocCapCharsLearning,
	} {
		if got := value(key); got != nil {
			t.Fatalf("%s must be absent after the migration (absent = code default), got %q", key, *got)
		}
	}

	// And the loader turns that state into the four live caps the owner named.
	got, err := loadAuthSettings(NewDAL(db), defaultConfig(), func(string) {})
	if err != nil {
		t.Fatalf("loadAuthSettings: %v", err)
	}
	if got.docCapCharsManual != 15000 {
		t.Fatalf("manual cap = %d, want the carried-over 15000", got.docCapCharsManual)
	}
	if got.docCapCharsDuty != dutyCapCharsDefault {
		t.Fatalf("duty cap = %d, want the shipped %d", got.docCapCharsDuty, dutyCapCharsDefault)
	}
	if got.docCapCharsInsight != contextDocMaxCharsDefault ||
		got.docCapCharsLearning != contextDocMaxCharsDefault {
		t.Fatalf("insight/learning = %d/%d, want the shipped %d each",
			got.docCapCharsInsight, got.docCapCharsLearning, contextDocMaxCharsDefault)
	}
}

// TestMigration00048OnAnInstallThatNeverRaisedTheCap — the common case: the key
// was never written, so there is nothing to rename and all four read defaults.
// Without this, the test above could pass on a migration that only works when a
// row happens to exist.
func TestMigration00048OnAnInstallThatNeverRaisedTheCap(t *testing.T) {
	db, err := openSQLite(filepath.Join(t.TempDir(), "doc-cap-fresh.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()
	if err := runMigrations(db); err != nil {
		t.Fatalf("goose up: %v", err)
	}
	got, err := loadAuthSettings(NewDAL(db), defaultConfig(), func(string) {})
	if err != nil {
		t.Fatalf("loadAuthSettings: %v", err)
	}
	if got.docCapCharsDuty != dutyCapCharsDefault ||
		got.docCapCharsInsight != contextDocMaxCharsDefault ||
		got.docCapCharsLearning != contextDocMaxCharsDefault ||
		got.docCapCharsManual != contextDocMaxCharsDefault {
		t.Fatalf("a fresh install must read all four defaults, got %d/%d/%d/%d",
			got.docCapCharsDuty, got.docCapCharsInsight,
			got.docCapCharsLearning, got.docCapCharsManual)
	}
}
