package main

// T-3809 (node 6, ts-c7887d33a51a) — migration 00047_role_insight round trip.
//
// WHY THIS FILE EXISTS AT ALL. 00047 shipped with ZERO test coverage: nothing
// under server/ocserverd/*_test.go named 00047 or role_insight, while its
// neighbours do have one each (TestMigration00045DeletesOnlyTheRetired-
// TaskManualHistory, TestStepSupersededMigrationRoundTrip for 00016). That gap
// is not a convention, it is an omission — and the Down half of it is worse
// than untested: `ocserverd` has no `migrate down` subcommand (main.go's
// command table lists only "migrate"), so 00047's Down block has NO execution
// path anywhere in the product. This test is its only executor. Delete this
// file and that SQL goes back to never running until the day someone needs it
// to.
//
// AND DOWN IS DELIBERATELY DESTRUCTIVE — the assertions below say so out loud
// rather than let a reader assume Down/Up is lossless. Down drops the table;
// insight text written while it existed is GONE, and re-running Up hands back
// an EMPTY table. That is the migration's own stated design (Up moved no data
// out of anywhere, so an older binary sees precisely the world it left
// behind), and a round-trip test that only checked "the table came back" would
// quietly imply the opposite.

import (
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/pressly/goose/v3"
)

func t3809DumpDocs(t *testing.T, db *sql.DB) (map[string]string, map[string]string) {
	t.Helper()
	duty := map[string]string{}
	rows, err := db.Query(`SELECT role_key, hex(definition_md) FROM role_def ORDER BY role_key`)
	if err != nil {
		t.Fatalf("dump role_def: %v", err)
	}
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			t.Fatalf("scan role_def: %v", err)
		}
		duty[k] = v
	}
	rows.Close()

	learning := map[string]string{}
	rows, err = db.Query(`SELECT role_key || '::' || task_type, hex(text) FROM lessons ORDER BY 1`)
	if err != nil {
		t.Fatalf("dump lessons: %v", err)
	}
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			t.Fatalf("scan lessons: %v", err)
		}
		learning[k] = v
	}
	rows.Close()
	return duty, learning
}

func t3809TableExists(t *testing.T, db *sql.DB, name string) bool {
	t.Helper()
	var n int
	if err := db.QueryRow(
		`SELECT count(*) FROM sqlite_master WHERE type='table' AND name=?`, name,
	).Scan(&n); err != nil {
		t.Fatalf("sqlite_master(%s): %v", name, err)
	}
	return n > 0
}

func t3809Version(t *testing.T, db *sql.DB) int64 {
	t.Helper()
	var v int64
	if err := db.QueryRow(`SELECT MAX(version_id) FROM goose_db_version`).Scan(&v); err != nil {
		t.Fatalf("version: %v", err)
	}
	return v
}

// TestT3809Migration00047DownLeavesDutyAndLearningUntouched exercises the Down
// block of 00047 on a database that carries real Duty, real Learning AND real
// Insight content. Three things are asserted, and the third is the one that
// makes the first two non-vacuous: the table has to actually have been there,
// with rows in it, for "dropping it did not disturb the neighbours" to mean
// anything.
func TestT3809Migration00047DownLeavesDutyAndLearningUntouched(t *testing.T) {
	db, err := openSQLite(filepath.Join(t.TempDir(), "t3809-node6-down.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()
	if err := runMigrations(db); err != nil {
		t.Fatalf("goose up: %v", err)
	}
	if got := t3809Version(t, db); got != 47 {
		t.Fatalf("expected schema version 47 after up, got %d", got)
	}

	// Real content in all three blocks. Multi-byte + emoji + newline + tab on
	// purpose: a Down that "only drops a table" must not rewrite text either.
	const dutyA = "DUTY α: 負責出貨 widget\n第二行 — em dash, \"quotes\", \\backslash, 🚚"
	const dutyB = "DUTY β: reviews\ttabs\tand trailing spaces   "
	const learnA = "LEARNING α: node 20 / port 7755 / 怪脾氣：--no-sandbox"
	for _, r := range []struct{ k, d string }{{"r-alpha", dutyA}, {"r-beta", dutyB}} {
		if _, err := db.Exec(
			`INSERT INTO role_def (role_key, name, definition_md) VALUES (?,?,?)`,
			r.k, r.k, r.d); err != nil {
			t.Fatalf("seed role_def: %v", err)
		}
	}
	if _, err := db.Exec(
		`INSERT INTO lessons (role_key, task_type, text) VALUES (?,?,?)`,
		"r-alpha", "general", learnA); err != nil {
		t.Fatalf("seed lessons: %v", err)
	}
	if _, err := db.Exec(
		`INSERT INTO role_insight (role_key, text) VALUES (?,?)`,
		"r-alpha", "INSIGHT: a wrong split costs more than a slow one."); err != nil {
		t.Fatalf("seed role_insight: %v", err)
	}

	// Non-vacuity: the thing Down is about to drop must exist and be populated.
	if !t3809TableExists(t, db, "role_insight") {
		t.Fatal("role_insight missing before Down — the Down assertions below would be vacuous")
	}
	var insightRows int
	if err := db.QueryRow(`SELECT count(*) FROM role_insight`).Scan(&insightRows); err != nil {
		t.Fatalf("count role_insight: %v", err)
	}
	if insightRows != 1 {
		t.Fatalf("expected 1 seeded insight row before Down, got %d", insightRows)
	}

	dutyBefore, learnBefore := t3809DumpDocs(t, db)
	if len(dutyBefore) != 2 || len(learnBefore) != 1 {
		t.Fatalf("baseline is not what it should be: duty=%d learning=%d", len(dutyBefore), len(learnBefore))
	}

	// ── DOWN 47 → 46 ────────────────────────────────────────────────────────
	if err := goose.DownTo(db, "migrations", 46); err != nil {
		t.Fatalf("goose down to 46: %v", err)
	}
	if got := t3809Version(t, db); got != 46 {
		t.Fatalf("expected schema version 46 after down, got %d", got)
	}
	if t3809TableExists(t, db, "role_insight") {
		t.Fatal("Down did not drop role_insight")
	}
	dutyAfter, learnAfter := t3809DumpDocs(t, db)
	for k, v := range dutyBefore {
		if dutyAfter[k] != v {
			t.Fatalf("Duty changed across Down for %s:\n before=%s\n  after=%s", k, v, dutyAfter[k])
		}
	}
	if len(dutyAfter) != len(dutyBefore) {
		t.Fatalf("Duty row count changed across Down: %d → %d", len(dutyBefore), len(dutyAfter))
	}
	for k, v := range learnBefore {
		if learnAfter[k] != v {
			t.Fatalf("Learning changed across Down for %s:\n before=%s\n  after=%s", k, v, learnAfter[k])
		}
	}
	if len(learnAfter) != len(learnBefore) {
		t.Fatalf("Learning row count changed across Down: %d → %d", len(learnBefore), len(learnAfter))
	}

	// ── UP again 46 → 47 ────────────────────────────────────────────────────
	// The migration's own header says the insight text written while the table
	// existed is LOST with it. Assert that plainly rather than let a reader
	// assume Down/Up is lossless.
	if err := runMigrations(db); err != nil {
		t.Fatalf("goose up again: %v", err)
	}
	if got := t3809Version(t, db); got != 47 {
		t.Fatalf("expected schema version 47 after re-up, got %d", got)
	}
	if !t3809TableExists(t, db, "role_insight") {
		t.Fatal("re-Up did not recreate role_insight")
	}
	var after int
	if err := db.QueryRow(`SELECT count(*) FROM role_insight`).Scan(&after); err != nil {
		t.Fatalf("count role_insight after re-up: %v", err)
	}
	if after != 0 {
		t.Fatalf("re-Up should give an EMPTY role_insight (Down is destructive by design), got %d rows", after)
	}
	dutyEnd, learnEnd := t3809DumpDocs(t, db)
	for k, v := range dutyBefore {
		if dutyEnd[k] != v {
			t.Fatalf("Duty changed across Down+Up for %s", k)
		}
	}
	for k, v := range learnBefore {
		if learnEnd[k] != v {
			t.Fatalf("Learning changed across Down+Up for %s", k)
		}
	}
}
