package main

// migration_00049_doc_cap_manual_split_test.go — the compatibility half of
// T-30f1, picking up exactly where the 00048 file leaves off.
//
// 00049 COPIES `doc.cap_chars.manual` to `.manual_sop` and `.manual_learnings`
// and deletes it. Every failure mode is silent:
//
//   - the copy never runs        → both new keys read the shipped default, and
//     an SOP or learnings doc already over it becomes shrink-only overnight;
//   - only one half is copied    → the other silently loses the owner's raise;
//   - the old row is left behind → `get_settings` shows a key naming the whole
//     manual next to the two it was split into, which is the naming rule 00048
//     was written to enforce;
//   - Down folds back the SMALLER of the two → downgrading past a legal
//     document, the exact thing the cap floor forbids.
//
// The seeded value is written as contextDocMaxCharsDefault + delta rather than
// as a literal for the reason the 00048 file gives: a fixture equal to the
// default makes a migration that carried nothing look like it carried.

import (
	"path/filepath"
	"strconv"
	"testing"

	"github.com/pressly/goose/v3"
)

func TestMigration00049CopiesTheManualCapToBothHalves(t *testing.T) {
	db, err := openSQLite(filepath.Join(t.TempDir(), "doc-cap-split.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()
	if err := runMigrations(db); err != nil {
		t.Fatalf("goose up: %v", err)
	}
	// The world as 00048 left it — one manual key, raised by the owner.
	if err := goose.DownTo(db, "migrations", 48); err != nil {
		t.Fatalf("down to 48: %v", err)
	}
	raised := strconv.Itoa(contextDocMaxCharsDefault + 5000)
	if _, err := db.Exec(
		`INSERT INTO setting (key, value, updated_at) VALUES ('doc.cap_chars.manual', ?, 1)`,
		raised); err != nil {
		t.Fatalf("seed the pre-00049 manual key: %v", err)
	}

	if err := runMigrations(db); err != nil {
		t.Fatalf("00049 up: %v", err)
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

	for _, key := range []string{"doc.cap_chars.manual_sop", "doc.cap_chars.manual_learnings"} {
		if got := value(key); got == nil || *got != raised {
			t.Fatalf("the owner's raised cap (%s) must arrive at %s intact, got %v",
				raised, key, got)
		}
	}
	// Gone, not merely shadowed: a row left behind is a key naming the whole
	// manual sitting beside its two halves, and settings.go would never read it,
	// so nothing else would ever complain.
	if got := value("doc.cap_chars.manual"); got != nil {
		t.Fatalf("the legacy doc.cap_chars.manual row must be split away, still holds %q", *got)
	}
	// The three journal keys are untouched by this migration.
	for _, key := range []string{
		settingDocCapCharsDuty, settingDocCapCharsInsight, settingDocCapCharsLearning,
	} {
		if got := value(key); got != nil {
			t.Fatalf("%s must still be absent (absent = code default), got %q", key, *got)
		}
	}

	got, err := loadAuthSettings(NewDAL(db), defaultConfig(), func(string) {})
	if err != nil {
		t.Fatalf("loadAuthSettings: %v", err)
	}
	if got.docCapCharsManualSop != contextDocMaxCharsDefault+5000 ||
		got.docCapCharsManualLearnings != contextDocMaxCharsDefault+5000 {
		t.Fatalf("live caps = %d/%d, want the carried-over %s each",
			got.docCapCharsManualSop, got.docCapCharsManualLearnings, raised)
	}
}

// TestMigration00049OnAnInstallThatNeverRaisedTheCap — the common case: no row
// to copy, so both halves read their code default. Without this, the test above
// could pass on a migration that only works when a row happens to exist.
func TestMigration00049OnAnInstallThatNeverRaisedTheCap(t *testing.T) {
	db, err := openSQLite(filepath.Join(t.TempDir(), "doc-cap-split-fresh.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()
	if err := runMigrations(db); err != nil {
		t.Fatalf("goose up: %v", err)
	}
	var n int
	if err := db.QueryRow(
		`SELECT COUNT(*) FROM setting WHERE key LIKE 'doc.cap_chars%'`).Scan(&n); err != nil {
		t.Fatalf("count doc cap rows: %v", err)
	}
	if n != 0 {
		t.Fatalf("a fresh install must hold no doc.cap_chars rows at all, got %d", n)
	}
	got, err := loadAuthSettings(NewDAL(db), defaultConfig(), func(string) {})
	if err != nil {
		t.Fatalf("loadAuthSettings: %v", err)
	}
	if got.docCapCharsManualSop != contextDocMaxCharsDefault ||
		got.docCapCharsManualLearnings != contextDocMaxCharsDefault {
		t.Fatalf("a fresh install must read both manual defaults, got %d/%d",
			got.docCapCharsManualSop, got.docCapCharsManualLearnings)
	}
}

// TestMigration00049DownFoldsBackTheLargerHalf — an older binary reads ONE
// number for both documents, so folding back the smaller would put every doc
// written against the larger cap into shrink-only mode. That is the degradation
// the cap-only-goes-up rule forbids, and a Down that quietly performs it is
// worse than one that refuses.
func TestMigration00049DownFoldsBackTheLargerHalf(t *testing.T) {
	cases := []struct {
		name       string
		sop        int
		learnings  int
		wantFolded int
	}{
		{"sop was raised higher", contextDocMaxCharsDefault + 9000, contextDocMaxCharsDefault + 1000, contextDocMaxCharsDefault + 9000},
		{"learnings was raised higher", contextDocMaxCharsDefault + 1000, contextDocMaxCharsDefault + 9000, contextDocMaxCharsDefault + 9000},
		{"both equal", contextDocMaxCharsDefault + 3000, contextDocMaxCharsDefault + 3000, contextDocMaxCharsDefault + 3000},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			db, err := openSQLite(filepath.Join(t.TempDir(), "doc-cap-down.db"))
			if err != nil {
				t.Fatalf("open: %v", err)
			}
			defer db.Close()
			if err := runMigrations(db); err != nil {
				t.Fatalf("goose up: %v", err)
			}
			for key, v := range map[string]int{
				"doc.cap_chars.manual_sop":       tc.sop,
				"doc.cap_chars.manual_learnings": tc.learnings,
			} {
				if _, err := db.Exec(
					`INSERT INTO setting (key, value, updated_at) VALUES (?, ?, 1)`,
					key, strconv.Itoa(v)); err != nil {
					t.Fatalf("seed %s: %v", key, err)
				}
			}

			if err := goose.DownTo(db, "migrations", 48); err != nil {
				t.Fatalf("down to 48: %v", err)
			}

			var folded string
			if err := db.QueryRow(
				`SELECT value FROM setting WHERE key = 'doc.cap_chars.manual'`).Scan(&folded); err != nil {
				t.Fatalf("the folded-back manual key must exist: %v", err)
			}
			if folded != strconv.Itoa(tc.wantFolded) {
				t.Fatalf("folded manual cap = %s, want the larger half %d", folded, tc.wantFolded)
			}
			var left int
			if err := db.QueryRow(
				`SELECT COUNT(*) FROM setting WHERE key IN
				 ('doc.cap_chars.manual_sop','doc.cap_chars.manual_learnings')`).Scan(&left); err != nil {
				t.Fatalf("count split keys: %v", err)
			}
			if left != 0 {
				t.Fatalf("the split keys must be gone after Down, %d left", left)
			}
		})
	}
}

// TestMigration00049DownOnAnInstallThatNeverRaisedTheCap — nothing to fold, so
// Down must not invent a row. A migration that wrote a value nobody chose would
// pin the older binary's cap to whatever this file happened to default to.
func TestMigration00049DownOnAnInstallThatNeverRaisedTheCap(t *testing.T) {
	db, err := openSQLite(filepath.Join(t.TempDir(), "doc-cap-down-fresh.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()
	if err := runMigrations(db); err != nil {
		t.Fatalf("goose up: %v", err)
	}
	if err := goose.DownTo(db, "migrations", 48); err != nil {
		t.Fatalf("down to 48: %v", err)
	}
	var n int
	if err := db.QueryRow(
		`SELECT COUNT(*) FROM setting WHERE key LIKE 'doc.cap_chars%'`).Scan(&n); err != nil {
		t.Fatalf("count doc cap rows: %v", err)
	}
	if n != 0 {
		t.Fatalf("Down must invent no row when there was nothing to fold, got %d", n)
	}
}
