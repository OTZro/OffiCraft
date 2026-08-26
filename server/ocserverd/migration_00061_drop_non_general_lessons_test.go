package main

// migration_00061_drop_non_general_lessons_test.go — T-2 step A.
//
// The migration deletes every `lessons` row whose task_type is not 'general'.
// Two failures are possible and they are NOT symmetric:
//
//   - a non-general row survives → the later column drop folds two rows onto
//     one key, which is a mess but a visible, fixable one; and
//   - a 'general' row is deleted → the owner's actual learnings are gone, and
//     nothing in this change kept a copy. That is the irreversible half, so it
//     gets the assertion with the loudest, most specific failure message
//     (TestMigration00061KeepsEveryGeneralLessonByteForByte), and it is the
//     assertion a mutant on the predicate has to be able to turn red.
//
// The fixture is built to make a predicate mistake VISIBLE rather than
// plausible: 'general' sits beside neighbours that any wildcard, prefix or
// case-folding comparison would confuse it with, and one role_key carries
// several task_types — the exact arrangement that makes the column drop lossy
// and the reason this migration runs first.

import (
	"database/sql"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/pressly/goose/v3"
)

// the version this file is about, and the one below it — a DownTo target that
// is the wrong side of the migration under test would seed into a world that
// already had it applied and prove nothing.
const (
	migration00061Version      = 61
	migration00061PriorVersion = migration00061Version - 1
)

// lessonsFixtureRow is one seeded row plus the verdict the predicate must reach
// for it.
type lessonsFixtureRow struct {
	roleKey  string
	taskType string
	text     string
	survives bool // true = 'general', must be present and byte-identical after
}

// migration00061Fixture is the seeded world. Every non-surviving entry is a
// task_type observed or plausible in this table's live shape; every surviving
// entry is exactly 'general'.
func migration00061Fixture() []lessonsFixtureRow {
	return []lessonsFixtureRow{
		// ── the rows that must survive, byte for byte ────────────────────────
		{"assistant", "general", "assistant general lessons\n  二層縮排\ttab\n", true},
		{"r-25debddcf5dd", "general", "", true},              // an empty doc is still a doc
		{"r-cbf8cdbc89ba", "general", "trailing \n\n", true}, // trailing whitespace must not be trimmed

		// ── the rows that must go ────────────────────────────────────────────
		{"assistant", "task-manual-learnings", "x", false},
		{"assistant", "tm-05f7c776d6ff", "y", false},
		{"assistant", "review-pr-seth", "z", false},
		{"r-25debddcf5dd", "default", "shipped shell", false},
		{"r-25debddcf5dd", "tm-05f7c776d6ff", "long doc", false},
		{"r-25debddcf5dd", "t6bd2-authz-probe", "", false}, // empty AND non-general: still goes
		{"r-cbf8cdbc89ba", "review-pr-seth", "w", false},

		// 🔴 near-misses on the spared value. A LIKE 'general%', a prefix match,
		// a case-insensitive compare or a trim would spare at least one of these
		// — a byte-exact <> spares none.
		{"assistant", "general-notes", "prefix neighbour", false},
		{"assistant", "notgeneral", "suffix neighbour", false},
		{"assistant", "General", "case neighbour", false},
		{"assistant", " general", "leading-space neighbour", false},
		{"assistant", "general ", "trailing-space neighbour", false},
	}
}

// migration00061World brings a temp database to the state just BEFORE 00061,
// seeds the fixture, and returns the handle. Every caller gets its own file.
func migration00061World(t *testing.T) *sqlDBForMigration61 {
	t.Helper()
	db, err := openSQLite(filepath.Join(t.TempDir(), "lessons-general-only.db"))
	if err != nil {
		t.Fatalf("open temp sqlite: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if err := runMigrations(db); err != nil {
		t.Fatalf("goose up: %v", err)
	}
	if err := goose.DownTo(db, "migrations", migration00061PriorVersion); err != nil {
		t.Fatalf("down to %d (the world 00061 runs against): %v", migration00061PriorVersion, err)
	}
	for _, r := range migration00061Fixture() {
		if _, err := db.Exec(
			`INSERT INTO lessons (role_key, task_type, text, tombstoned) VALUES (?, ?, ?, 0)`,
			r.roleKey, r.taskType, r.text); err != nil {
			t.Fatalf("seed (%s, %s): %v", r.roleKey, r.taskType, err)
		}
	}
	// 🔴 ANTI-VACUITY. A fixture that failed to land would let every assertion
	// below pass over an empty table, which is indistinguishable from a
	// migration that worked.
	var seeded int
	if err := db.QueryRow(`SELECT COUNT(*) FROM lessons`).Scan(&seeded); err != nil {
		t.Fatalf("count seeded rows: %v", err)
	}
	if seeded != len(migration00061Fixture()) {
		t.Fatalf("seeded %d rows, wrote %d — the fixture did not land, so nothing below "+
			"would be measuring anything", seeded, len(migration00061Fixture()))
	}
	return &sqlDBForMigration61{t: t, db: db}
}

// TestMigration00061DeletesEveryNonGeneralLesson is DoD ①.
func TestMigration00061DeletesEveryNonGeneralLesson(t *testing.T) {
	w := migration00061World(t)
	w.applyMigration()
	w.assertNoNonGeneralRowsRemain()
}

// TestMigration00061KeepsEveryGeneralLessonByteForByte is DoD ② — the one that
// guards the irreversible direction, and the named assertion a predicate mutant
// must be able to turn red.
func TestMigration00061KeepsEveryGeneralLessonByteForByte(t *testing.T) {
	w := migration00061World(t)
	w.applyMigration()
	w.assertGeneralRowsAreIntact()
}

// TestMigration00061IsIdempotentAcrossADownUpCycle is DoD ③: the Up body run a
// SECOND time against the world it already produced must neither error nor
// reach a different outcome. goose refuses to re-run an applied version, so the
// cycle goes through Down (which this migration declares a no-op) and back up —
// that is what actually re-executes the DELETE.
func TestMigration00061IsIdempotentAcrossADownUpCycle(t *testing.T) {
	w := migration00061World(t)
	w.applyMigration()
	w.assertNoNonGeneralRowsRemain()
	w.assertGeneralRowsAreIntact()

	if err := goose.DownTo(w.db, "migrations", migration00061PriorVersion); err != nil {
		t.Fatalf("down to %d for the re-run: %v", migration00061PriorVersion, err)
	}
	w.applyMigration()

	w.assertNoNonGeneralRowsRemain()
	w.assertGeneralRowsAreIntact()
}

// ── the fixture handle and its assertions ────────────────────────────────────

type sqlDBForMigration61 struct {
	t  *testing.T
	db *sql.DB
}

func (w *sqlDBForMigration61) applyMigration() {
	w.t.Helper()
	if err := runMigrations(w.db); err != nil {
		w.t.Fatalf("goose up through %d: %v", migration00061Version, err)
	}
}

// assertNoNonGeneralRowsRemain — DoD ①.
func (w *sqlDBForMigration61) assertNoNonGeneralRowsRemain() {
	w.t.Helper()
	rows, err := w.db.Query(`SELECT role_key, task_type FROM lessons WHERE task_type <> 'general'`)
	if err != nil {
		w.t.Fatalf("scan for surviving non-general rows: %v", err)
	}
	defer rows.Close()
	var left []string
	for rows.Next() {
		var role, tt string
		if err := rows.Scan(&role, &tt); err != nil {
			w.t.Fatalf("scan: %v", err)
		}
		left = append(left, role+"/"+tt)
	}
	if err := rows.Err(); err != nil {
		w.t.Fatalf("rows: %v", err)
	}
	if len(left) > 0 {
		sort.Strings(left)
		w.t.Errorf("00061 left %d non-general lessons row(s) behind: %s — each one is a "+
			"row that will collide with its role's surviving key when the task_type column "+
			"is dropped, which is the collision this migration runs first to prevent",
			len(left), strings.Join(left, ", "))
	}
	// 🔴 The scan above asks the table with the SAME predicate the migration
	// uses, so on its own it could only ever agree with the migration's own idea
	// of "non-general". Each doomed row is therefore ALSO looked up by its exact
	// composite key, which is a question the predicate does not get to answer.
	for _, r := range migration00061Fixture() {
		if r.survives {
			continue
		}
		var n int
		if err := w.db.QueryRow(
			`SELECT COUNT(*) FROM lessons WHERE role_key = ? AND task_type = ?`,
			r.roleKey, r.taskType).Scan(&n); err != nil {
			w.t.Fatalf("look up (%s, %s): %v", r.roleKey, r.taskType, err)
		}
		if n != 0 {
			w.t.Errorf("the lessons row (role_key=%q, task_type=%q) survived 00061 — "+
				"task_type is not 'general', so it must be gone", r.roleKey, r.taskType)
		}
	}
}

// assertGeneralRowsAreIntact — DoD ②, the irreversible direction.
func (w *sqlDBForMigration61) assertGeneralRowsAreIntact() {
	w.t.Helper()
	for _, r := range migration00061Fixture() {
		if !r.survives {
			continue
		}
		var text string
		err := w.db.QueryRow(
			`SELECT text FROM lessons WHERE role_key = ? AND task_type = ?`,
			r.roleKey, r.taskType).Scan(&text)
		if err != nil {
			w.t.Errorf("the 'general' lessons row for role %q is GONE after 00061 (%v). "+
				"This is the unrecoverable failure: the migration keeps no copy, so a "+
				"'general' row deleted here cannot be brought back by rolling the "+
				"migration back. The predicate must spare task_type = 'general' exactly",
				r.roleKey, err)
			continue
		}
		if text != r.text {
			w.t.Errorf("the 'general' lessons row for role %q came back CHANGED after 00061: "+
				"want %q, got %q. This migration is a DELETE and must not rewrite a single "+
				"byte of a surviving document", r.roleKey, r.text, text)
		}
	}
	// The spared set must also be exactly the general set — a predicate that
	// spared MORE than 'general' is caught by assertNoNonGeneralRowsRemain, and
	// this count is what keeps the two halves from being satisfiable by an empty
	// table.
	var kept int
	if err := w.db.QueryRow(`SELECT COUNT(*) FROM lessons WHERE task_type = 'general'`).Scan(&kept); err != nil {
		w.t.Fatalf("count surviving general rows: %v", err)
	}
	want := 0
	for _, r := range migration00061Fixture() {
		if r.survives {
			want++
		}
	}
	if kept != want {
		w.t.Errorf("00061 left %d rows with task_type = 'general', want %d — the surviving "+
			"set is not the general set", kept, want)
	}
}
