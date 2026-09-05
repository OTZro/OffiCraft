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
	for _, r := range migration00061HistoryFixture() {
		seedHistoryRow(t, db, "lessons", r.key)
	}
	for _, r := range migration00061OtherKindFixture() {
		seedHistoryRow(t, db, r.kind, r.key)
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
	var seededHistory int
	if err := db.QueryRow(`SELECT COUNT(*) FROM document_history`).Scan(&seededHistory); err != nil {
		t.Fatalf("count seeded history rows: %v", err)
	}
	wantHistory := len(migration00061HistoryFixture()) + len(migration00061OtherKindFixture())
	if seededHistory != wantHistory {
		t.Fatalf("seeded %d document_history rows, wrote %d — the history fixture did not "+
			"land, so nothing below would be measuring anything", seededHistory, wantHistory)
	}
	return &sqlDBForMigration61{t: t, db: db}
}

func seedHistoryRow(t *testing.T, db *sql.DB, kind, key string) {
	t.Helper()
	if _, err := db.Exec(
		`INSERT INTO document_history (document_kind, document_key, content_json, created_ts, actor_id)
		 VALUES (?, ?, ?, 1.0, 'owner')`,
		kind, key, `{"text":"retained revision of `+kind+`","tombstoned":"false"}`); err != nil {
		t.Fatalf("seed history (%s, %q): %v", kind, key, err)
	}
}

// ── document_history fixtures — the SECOND irreversible face ─────────────────

// historyFixtureRow is one seeded document_history row under
// document_kind = 'lessons', plus the verdict the predicate must reach for it.
type historyFixtureRow struct {
	key      string
	survives bool
	why      string
}

// migration00061HistoryFixture is the retained-revision world. The verdicts are
// NOT derived from the migration's own predicate — each one is the answer
// historyKeyParts (SplitN on the FIRST "::") plus documentHistoryAllowed would
// give for that key, worked out by hand, which is the only reason this fixture
// can disagree with a wrong predicate.
func migration00061HistoryFixture() []historyFixtureRow {
	return []historyFixtureRow{
		// ── must survive: the task_type the restore would write IS 'general' ─
		{"assistant::general", true, "the canonical spared key"},
		{"r-25debddcf5dd::general", true, "another role, same spared task_type"},
		{"r-中文::general", true, "CJK role_key, spared task_type"},

		// ── must survive: the restore door refuses these keys outright, so
		// they cannot reseed anything and an irreversible DELETE must not take
		// them. Each is an INVALID key by historyKeyParts' own rule.
		{"assistant", true, "no separator at all — historyKeyParts returns valid=false"},
		{"::general", true, "empty role_key side — historyKeyParts returns valid=false"},
		{"assistant::", true, "empty task_type side — historyKeyParts returns valid=false"},
		{"", true, "empty key — historyKeyParts returns valid=false"},

		// ── must go: the restore would write a non-general task_type ─────────
		{"assistant::review-pr-seth", false, "the reviewer's measured key"},
		{"assistant::tm-05f7c776d6ff", false, "a per-manual task_type"},
		{"r-cbf8cdbc89ba::default", false, "the shipped-shell task_type"},
		{"r-中文::任務型", false, "CJK on both sides"},

		// 🔴 near-misses on the spared value, the history twin of the lessons
		// fixture above. A LIKE 'general%', a prefix match, a case-insensitive
		// compare or a trim would spare at least one of these.
		{"assistant::general-notes", false, "prefix neighbour"},
		{"assistant::notgeneral", false, "suffix neighbour"},
		{"assistant::General", false, "case neighbour"},
		{"assistant:: general", false, "leading-space neighbour"},
		{"assistant::general ", false, "trailing-space neighbour"},

		// 🔴 THE REAL AMBIGUITY: "::" inside the role_key half. These are the
		// rows that separate "mirror the parse" from "mirror the construction".
		// The restore splits at the FIRST "::", so 'a::b::general' restores
		// task_type "b::general" — NOT 'general' — and must go, even though a
		// last-separator split would have called it general and spared it.
		{"a::b::general", false, "role_key contains the separator; parsed task_type is \"b::general\""},
		{"a::general::x", false, "'general' sits in the role_key half; parsed task_type is \"general::x\""},
	}
}

// otherKindFixtureRow is a retained revision under a document_kind this
// migration must not touch.
type otherKindFixtureRow struct {
	kind string
	key  string
}

// migration00061OtherKindFixture pins the blast radius. The `insight` entry
// carrying a LESSONS-SHAPED key is the load-bearing one: it is the row that a
// predicate which forgot `document_kind = 'lessons'` would delete, and no other
// entry here would notice that mistake.
func migration00061OtherKindFixture() []otherKindFixtureRow {
	return []otherKindFixtureRow{
		{"insight", "assistant"},
		{"insight", "assistant::review-pr-seth"}, // lessons-shaped key, WRONG kind
		{"global_context", ""},
		{"role_definition", "assistant"},
		{"role_definition", "r-25debddcf5dd::review-pr-seth"}, // lessons-shaped key, WRONG kind
		{"task_manual_sop", "tm-05f7c776d6ff"},
		{"task_manual_learnings", "tm-05f7c776d6ff"},
		{"task_description", "t-2a0602536344"},
		{"task_title", "t-2a0602536344"},
		{"system_interaction", "claude"},
		{"boot_sequence", "claude"},
	}
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

// TestMigration00061ClearsEveryRestorableNonGeneralLessonsHistoryRow is DoD ④ —
// the second door. A lessons row deleted above can be written straight back by
// the document-history restore route, so the retained revisions that would
// reseed a non-general task_type go in the SAME migration, the way
// DeleteLessonsForRole already cascades onto them in one transaction. The
// end-to-end proof that this is not theoretical lives in
// TestMigration00061RestoreCannotPutANonGeneralLessonBack.
func TestMigration00061ClearsEveryRestorableNonGeneralLessonsHistoryRow(t *testing.T) {
	w := migration00061World(t)
	w.applyMigration()
	w.assertNoRestorableNonGeneralHistoryRemains()
}

// TestMigration00061KeepsEveryGeneralLessonsHistoryRow is DoD ⑤ — the
// irreversible direction on the history table, and the named assertion a mutant
// that widens the history predicate has to be able to turn red.
func TestMigration00061KeepsEveryGeneralLessonsHistoryRow(t *testing.T) {
	w := migration00061World(t)
	w.applyMigration()
	w.assertGeneralHistoryRowsAreIntact()
}

// TestMigration00061TouchesNoOtherDocumentKind is DoD ⑥ — the blast radius.
func TestMigration00061TouchesNoOtherDocumentKind(t *testing.T) {
	w := migration00061World(t)
	w.applyMigration()
	w.assertOtherDocumentKindsUntouched()
}

// TestMigration00061IsIdempotentAcrossADownUpCycle is DoD ③: the Up body run a
// SECOND time against the world it already produced must neither error nor
// reach a different outcome. goose refuses to re-run an applied version, so the
// cycle goes through Down (which this migration declares a no-op) and back up —
// that is what actually re-executes the DELETE.
func TestMigration00061IsIdempotentAcrossADownUpCycle(t *testing.T) {
	w := migration00061World(t)
	w.applyMigration()
	w.assertEverything()

	if err := goose.DownTo(w.db, "migrations", migration00061PriorVersion); err != nil {
		t.Fatalf("down to %d for the re-run: %v", migration00061PriorVersion, err)
	}
	w.applyMigration()

	w.assertEverything()
}

// assertEverything is the whole DoD in one call, for the idempotency cycle:
// re-running must reach the SAME outcome on both tables, not merely not error.
func (w *sqlDBForMigration61) assertEverything() {
	w.t.Helper()
	w.assertNoNonGeneralRowsRemain()
	w.assertGeneralRowsAreIntact()
	w.assertNoRestorableNonGeneralHistoryRemains()
	w.assertGeneralHistoryRowsAreIntact()
	w.assertOtherDocumentKindsUntouched()
}

// ── the fixture handle and its assertions ────────────────────────────────────

type sqlDBForMigration61 struct {
	t  *testing.T
	db *sql.DB
}

// 🔴 UpTo(61), NOT the whole ladder. 00062 drops the task_type COLUMN, so
// running past this migration would leave every assertion in this file querying
// a column that no longer exists — and the failure would read as a broken test
// rather than as "you are asking 00061's question of a later world". This file
// is about 00061 in isolation; 00062 has its own.
func (w *sqlDBForMigration61) applyMigration() {
	w.t.Helper()
	if err := goose.UpTo(w.db, "migrations", migration00061Version); err != nil {
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

// ── document_history assertions ──────────────────────────────────────────────

func (w *sqlDBForMigration61) historyRowExists(kind, key string) bool {
	w.t.Helper()
	var n int
	if err := w.db.QueryRow(
		`SELECT COUNT(*) FROM document_history WHERE document_kind = ? AND document_key = ?`,
		kind, key).Scan(&n); err != nil {
		w.t.Fatalf("look up history (%s, %q): %v", kind, key, err)
	}
	return n > 0
}

// assertNoRestorableNonGeneralHistoryRemains is the named assertion for the
// direction "a restore can still reseed a non-general row". Every key here is
// looked up by its EXACT value, not re-derived from the migration's predicate,
// so it is a question the predicate does not get to answer.
func (w *sqlDBForMigration61) assertNoRestorableNonGeneralHistoryRemains() {
	w.t.Helper()
	var left []string
	for _, r := range migration00061HistoryFixture() {
		if r.survives {
			continue
		}
		if w.historyRowExists("lessons", r.key) {
			left = append(left, r.key+" ("+r.why+")")
		}
	}
	if len(left) > 0 {
		sort.Strings(left)
		w.t.Errorf("NON-GENERAL LESSONS HISTORY ROWS ARE NOT CLEARED: 00061 left %d "+
			"restorable non-general document_history row(s) behind: %s. Each one is a live "+
			"path back: the lessons arm of restoreDocumentHistory splits document_key on the "+
			"first \"::\" and writes the task_type it finds into `lessons` verbatim, with "+
			"nothing on that path comparing it to 'general'. While any of these exist the "+
			"migration is a momentary state, not the guarantee the later task_type column "+
			"drop depends on", len(left), strings.Join(left, ", "))
	}
}

// assertGeneralHistoryRowsAreIntact is the named assertion for the IRREVERSIBLE
// direction on this second table: a wrongly deleted retained revision is gone
// for good, exactly like a wrongly deleted 'general' lessons row.
func (w *sqlDBForMigration61) assertGeneralHistoryRowsAreIntact() {
	w.t.Helper()
	for _, r := range migration00061HistoryFixture() {
		if !r.survives {
			continue
		}
		if !w.historyRowExists("lessons", r.key) {
			w.t.Errorf("A SPARED LESSONS HISTORY ROW IS GONE: document_key %q was deleted by "+
				"00061 and must not have been (%s). This is the unrecoverable direction on the "+
				"history table: the migration keeps no copy, and its Down is a declared no-op, "+
				"so a retained revision deleted here cannot be brought back. The predicate must "+
				"delete only keys whose task_type — everything after the FIRST \"::\", the way "+
				"historyKeyParts reads it — is non-empty and not 'general'", r.key, r.why)
		}
	}
	// The spared set must be EXACTLY the expected one — without this count a
	// predicate that deleted nothing at all under 'lessons' would satisfy every
	// per-key lookup above.
	var kept int
	if err := w.db.QueryRow(
		`SELECT COUNT(*) FROM document_history WHERE document_kind = 'lessons'`).Scan(&kept); err != nil {
		w.t.Fatalf("count surviving lessons history rows: %v", err)
	}
	want := 0
	for _, r := range migration00061HistoryFixture() {
		if r.survives {
			want++
		}
	}
	if kept != want {
		w.t.Errorf("00061 left %d document_history rows under kind 'lessons', want %d — the "+
			"surviving set is not the spared set", kept, want)
	}
}

// assertOtherDocumentKindsUntouched pins the blast radius: this migration names
// one document_kind and every other series must come through untouched.
func (w *sqlDBForMigration61) assertOtherDocumentKindsUntouched() {
	w.t.Helper()
	for _, r := range migration00061OtherKindFixture() {
		if !w.historyRowExists(r.kind, r.key) {
			w.t.Errorf("00061 DELETED A ROW OF ANOTHER DOCUMENT KIND: (document_kind=%q, "+
				"document_key=%q) is gone. This migration carries a ruling about `lessons` and "+
				"nothing else; `document_kind = 'lessons'` is an exact equality precisely so no "+
				"other series can be reached, and a lessons-SHAPED key under a different kind "+
				"must still be spared", r.kind, r.key)
		}
	}
	var other int
	if err := w.db.QueryRow(
		`SELECT COUNT(*) FROM document_history WHERE document_kind <> 'lessons'`).Scan(&other); err != nil {
		w.t.Fatalf("count non-lessons history rows: %v", err)
	}
	if other != len(migration00061OtherKindFixture()) {
		w.t.Errorf("00061 changed the non-lessons document_history population: %d rows, want %d",
			other, len(migration00061OtherKindFixture()))
	}
}
