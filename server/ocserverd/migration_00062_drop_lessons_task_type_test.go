package main

// migration_00062_drop_lessons_task_type_test.go — T-2 step B.
//
// 00062 rebuilds `lessons` without its task_type column and re-addresses every
// retained lessons revision from "<role_key>::general" to the bare role_key.
//
// 🔴 THE TWO DIRECTIONS THIS FILE EXISTS TO SEPARATE. Collapsing
// (role_key, task_type) onto role_key alone is safe ONLY IF each role_key
// already carries at most one row — which is exactly what 00061 established.
// So there are two questions, and answering only one of them proves nothing:
//
//	WITH 00061   → the fold is an IDENTITY: the surviving text is byte-identical
//	               and no row is lost.
//	WITHOUT 00061 → the fold is REFUSED, loudly, with a UNIQUE constraint. It
//	               must not silently keep whichever row happened to win.
//
// Both are measured below against the same fixture, which is what makes the
// pairing meaningful: the only difference between them is whether 00061 ran.
//
// 🔴 A NOTE ON A BELIEF THIS FILE FALSIFIED, kept because the next reader will
// arrive carrying it. 00061's own comment says SQLite's `ALTER TABLE ... DROP
// COLUMN` "does not report that as a conflict; it is a lossy fold". Measured
// here (TestMigration00062SqliteRefusesToDropAPrimaryKeyColumn): SQLite refuses
// to drop a PRIMARY KEY column AT ALL, with "cannot drop PRIMARY KEY column".
// The lossy-fold hazard is real, but it lives in the create/copy/rename rebuild
// that is the only way to do this — not in ALTER — and it is disarmed there by
// using a plain INSERT.

import (
	"database/sql"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/pressly/goose/v3"
)

const (
	migration00062Version      = 62
	migration00062PriorVersion = migration00062Version - 1
)

// migration00062FixtureRow is one seeded pre-00061 lessons row.
type migration00062FixtureRow struct {
	roleKey  string
	taskType string
	text     string
}

// migration00062Fixture deliberately puts SEVERAL task_types under ONE role_key
// — the arrangement that makes the fold lossy — plus roles that carry only
// 'general'. Texts are distinguishable so a fold that kept the wrong row is
// visible rather than merely countable.
func migration00062Fixture() []migration00062FixtureRow {
	return []migration00062FixtureRow{
		{"assistant", "general", "assistant general lessons\n  二層縮排\ttab\n"},
		{"assistant", "review-pr-seth", "assistant REVIEW lessons"},
		{"assistant", "tm-05f7c776d6ff", "assistant MANUAL lessons"},
		{"r-25debddcf5dd", "general", ""},              // an empty doc is still a doc
		{"r-cbf8cdbc89ba", "general", "trailing \n\n"}, // trailing whitespace must not be trimmed
		{"r-中文", "general", "CJK role, general"},
		{"r-中文", "任務型", "CJK role, non-general"},
	}
}

// migration00062HistoryRow is one seeded document_history row.
type migration00062HistoryRow struct {
	kind    string
	key     string
	wantKey string // the key it must carry after 00062
	why     string
}

// migration00062HistoryFixture is the retained-revision world, seeded AFTER
// 00061 has run (these are the rows 00061 leaves behind). wantKey is worked out
// by hand from what 00062 claims, never derived from its predicate.
func migration00062HistoryFixture() []migration00062HistoryRow {
	return []migration00062HistoryRow{
		{"lessons", "assistant::general", "assistant", "the canonical key, re-addressed"},
		{"lessons", "r-25debddcf5dd::general", "r-25debddcf5dd", "another role, same rewrite"},
		{"lessons", "r-中文::general", "r-中文", "CJK role_key, same rewrite"},

		// The three malformed shapes 00061 deliberately spared. 00062 spares
		// them too — a rewrite must not invent an explicable key out of a row
		// nobody can explain.
		{"lessons", "assistant", "assistant", "already bare — nothing to rewrite"},
		{"lessons", "::general", "::general", "empty role_key side — left alone"},
		{"lessons", "assistant::", "assistant::", "empty task_type side — left alone"},
		{"lessons", "", "", "empty key — left alone"},

		// Other kinds are untouched, INCLUDING one carrying a lessons-shaped
		// key: the row a predicate that forgot `document_kind = 'lessons'`
		// would rewrite, and the only entry here that would notice.
		{"insight", "assistant", "assistant", "wrong kind, bare key"},
		{"insight", "assistant::general", "assistant::general", "wrong kind, LESSONS-SHAPED key"},
		{"role_definition", "r-25debddcf5dd::general", "r-25debddcf5dd::general", "wrong kind, lessons-shaped key"},
		{"task_manual_sop", "tm-05f7c776d6ff", "tm-05f7c776d6ff", "wrong kind"},
		{"global_context", "", "", "wrong kind, empty key"},
	}
}

// migration00062World brings a temp database to the state just BEFORE 00061 and
// seeds the lessons fixture there (task_type still exists at that version).
func migration00062World(t *testing.T) *sql.DB {
	t.Helper()
	db, err := openSQLite(filepath.Join(t.TempDir(), "drop-task-type.db"))
	if err != nil {
		t.Fatalf("open temp sqlite: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if err := runMigrations(db); err != nil {
		t.Fatalf("goose up: %v", err)
	}
	if err := goose.DownTo(db, "migrations", migration00061PriorVersion); err != nil {
		t.Fatalf("down to %d: %v", migration00061PriorVersion, err)
	}
	for _, r := range migration00062Fixture() {
		if _, err := db.Exec(
			`INSERT INTO lessons (role_key, task_type, text, tombstoned) VALUES (?, ?, ?, 0)`,
			r.roleKey, r.taskType, r.text); err != nil {
			t.Fatalf("seed (%s, %s): %v", r.roleKey, r.taskType, err)
		}
	}
	// ANTI-VACUITY: a fixture that failed to land would let the assertions pass
	// over an empty table, which is indistinguishable from a working migration.
	var seeded int
	if err := db.QueryRow(`SELECT COUNT(*) FROM lessons`).Scan(&seeded); err != nil {
		t.Fatalf("count seeded rows: %v", err)
	}
	if seeded != len(migration00062Fixture()) {
		t.Fatalf("seeded %d rows, wrote %d — the fixture did not land", seeded, len(migration00062Fixture()))
	}
	return db
}

// lessonsAfterFold reads the post-00062 table as "role_key\x00text".
func lessonsAfterFold(t *testing.T, db *sql.DB) []string {
	t.Helper()
	rows, err := db.Query(`SELECT role_key, text FROM lessons`)
	if err != nil {
		t.Fatalf("scan lessons: %v", err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var role, text string
		if err := rows.Scan(&role, &text); err != nil {
			t.Fatalf("scan: %v", err)
		}
		out = append(out, role+"\x00"+text)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows: %v", err)
	}
	sort.Strings(out)
	return out
}

// ── DIRECTION 1: with 00061, the fold is an identity ─────────────────────────

// TestMigration00062FoldIsIdentityAfter00061 is the load-bearing assertion of
// this migration: every surviving row keeps its text BYTE FOR BYTE and no row
// is lost. This is the named assertion a mutant on the copy step has to be able
// to turn red.
func TestMigration00062FoldIsIdentityAfter00061(t *testing.T) {
	db := migration00062World(t)
	if err := goose.UpTo(db, "migrations", migration00062Version); err != nil {
		t.Fatalf("goose up through %d (00061 ran first, so the fold must be an identity): %v",
			migration00062Version, err)
	}

	want := []string{}
	for _, r := range migration00062Fixture() {
		if r.taskType == "general" {
			want = append(want, r.roleKey+"\x00"+r.text)
		}
	}
	sort.Strings(want)
	got := lessonsAfterFold(t, db)

	if len(want) == 0 {
		t.Fatal("the fixture declares no surviving rows — this test would prove nothing")
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("THE FOLD WAS NOT AN IDENTITY.\n got (%d rows): %q\nwant (%d rows): %q\n\n"+
			"00061 leaves exactly one row per role_key, so dropping task_type must keep "+
			"every row and every byte of its text. A row missing here means the copy "+
			"collapsed two keys onto one; a text that differs means it kept the wrong one.",
			len(got), got, len(want), want)
	}
}

// ── DIRECTION 2: without 00061, the fold is REFUSED ──────────────────────────

// TestMigration00062RefusesToFoldDuplicateRoleKeys is the other half of the
// pair, and the reason the copy is a plain INSERT. Skip 00061 and the same
// fixture must stop the migration rather than silently pick a winner.
func TestMigration00062RefusesToFoldDuplicateRoleKeys(t *testing.T) {
	db := migration00062World(t)

	// Mark 00061 as applied WITHOUT running it — the "someone skipped step A"
	// world, reachable by a hand-edited database or a partial upgrade. goose's
	// own version table is the only place that decision lives.
	if _, err := db.Exec(
		`INSERT INTO goose_db_version (version_id, is_applied, tstamp)
		 VALUES (?, 1, CURRENT_TIMESTAMP)`, migration00061Version); err != nil {
		t.Fatalf("mark 00061 applied without running it: %v", err)
	}

	err := goose.UpTo(db, "migrations", migration00062Version)
	if err == nil {
		t.Fatalf("00062 SILENTLY FOLDED DUPLICATE KEYS. The fixture has three task_types " +
			"under role_key \"assistant\" and two under \"r-中文\"; collapsing them onto " +
			"role_key alone loses documents and the caller does not get to choose which " +
			"survives. The copy must be a plain INSERT so SQLite refuses — `INSERT OR " +
			"REPLACE` / `OR IGNORE` would answer 0 here and keep whichever row happened to " +
			"be last")
	}
	if !strings.Contains(err.Error(), "UNIQUE constraint failed") {
		t.Errorf("00062 failed with %q, want a UNIQUE constraint violation — a different "+
			"failure means the refusal came from somewhere other than the fold this test "+
			"is about", err.Error())
	}

	// goose runs a migration in a transaction, so the refusal must also leave
	// the table intact rather than half-rebuilt.
	var cols int
	if err := db.QueryRow(
		`SELECT COUNT(*) FROM pragma_table_info('lessons') WHERE name = 'task_type'`).Scan(&cols); err != nil {
		t.Fatalf("pragma_table_info: %v", err)
	}
	if cols != 1 {
		t.Errorf("after the refusal `lessons` has %d task_type columns, want 1 — a failed "+
			"migration must roll back, not leave the table half-dropped", cols)
	}
}

// TestMigration00062SqliteRefusesToDropAPrimaryKeyColumn pins the fact the
// migration's comment leans on, so that a future reader who wants to "simplify"
// the rebuild into an ALTER TABLE finds out here rather than in production.
func TestMigration00062SqliteRefusesToDropAPrimaryKeyColumn(t *testing.T) {
	db := migration00062World(t)
	_, err := db.Exec(`ALTER TABLE lessons DROP COLUMN task_type`)
	if err == nil {
		t.Fatal("SQLite accepted DROP COLUMN on a PRIMARY KEY column — the rebuild in " +
			"00062 was written because it does not, and if that changed the migration's " +
			"stated reasoning needs rewriting rather than quietly outliving its evidence")
	}
	if !strings.Contains(err.Error(), "cannot drop PRIMARY KEY column") {
		t.Errorf("DROP COLUMN failed with %q, want \"cannot drop PRIMARY KEY column\"", err.Error())
	}
}

// ── document_history re-addressing ───────────────────────────────────────────

// TestMigration00062ReAddressesLessonsHistory covers the second table: the
// rewrite, what it deliberately spares, and the blast radius.
func TestMigration00062ReAddressesLessonsHistory(t *testing.T) {
	db := migration00062World(t)
	if err := goose.UpTo(db, "migrations", migration00061Version); err != nil {
		t.Fatalf("goose up through %d: %v", migration00061Version, err)
	}
	// Seeded AFTER 00061 on purpose: these are the rows 00061 leaves behind,
	// and seeding them before it would just re-test 00061's own predicate.
	for _, r := range migration00062HistoryFixture() {
		if _, err := db.Exec(
			`INSERT INTO document_history (document_kind, document_key, content_json, created_ts, actor_id)
			 VALUES (?, ?, ?, 1.0, 'owner')`,
			r.kind, r.key, `{"text":"retained revision","tombstoned":"false"}`); err != nil {
			t.Fatalf("seed history (%s, %q): %v", r.kind, r.key, err)
		}
	}
	var seeded int
	if err := db.QueryRow(`SELECT COUNT(*) FROM document_history`).Scan(&seeded); err != nil {
		t.Fatalf("count seeded history: %v", err)
	}
	if seeded != len(migration00062HistoryFixture()) {
		t.Fatalf("seeded %d history rows, wrote %d — the fixture did not land",
			seeded, len(migration00062HistoryFixture()))
	}

	if err := goose.UpTo(db, "migrations", migration00062Version); err != nil {
		t.Fatalf("goose up through %d: %v", migration00062Version, err)
	}

	// Compared as a MULTISET, not key by key: document_history is append-only
	// and legitimately holds several revisions under one (kind, key), so
	// "exactly one row here" would be the wrong question — and it is reachable
	// in this very fixture, where "assistant::general" rewrites onto the
	// already-bare "assistant". A multiset comparison also catches a DELETE or a
	// duplicated row, which a per-key existence check would not.
	want := []string{}
	for _, r := range migration00062HistoryFixture() {
		want = append(want, r.kind+"\x00"+r.wantKey)
	}
	sort.Strings(want)

	got := []string{}
	hrows, err := db.Query(`SELECT document_kind, document_key FROM document_history`)
	if err != nil {
		t.Fatalf("scan history: %v", err)
	}
	for hrows.Next() {
		var kind, key string
		if err := hrows.Scan(&kind, &key); err != nil {
			t.Fatalf("scan: %v", err)
		}
		got = append(got, kind+"\x00"+key)
	}
	hrows.Close()
	sort.Strings(got)

	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("THE LESSONS HISTORY WAS NOT RE-ADDRESSED AS DECLARED.\n got: %q\nwant: %q\n\n"+
			"A lessons revision left under the old composite key is UNREACHABLE: "+
			"list_document_history asks for the bare role_key, so the owner's undo path "+
			"disappears with no error anywhere. A row that MOVED and should not have "+
			"(another document_kind, or one of the malformed shapes 00061 spared) is the "+
			"same mistake pointing the other way.", got, want)
	}
}

// TestMigration00062DownRestoresThePreUpState is why this migration ships a real
// Down rather than 00061's no-op: it deletes nothing, so the rollback can be
// exact instead of plausible.
func TestMigration00062DownRestoresThePreUpState(t *testing.T) {
	db := migration00062World(t)
	if err := goose.UpTo(db, "migrations", migration00061Version); err != nil {
		t.Fatalf("goose up through %d: %v", migration00061Version, err)
	}
	before := map[string]string{}
	rows, err := db.Query(`SELECT role_key, task_type, text FROM lessons`)
	if err != nil {
		t.Fatalf("read pre-Up lessons: %v", err)
	}
	for rows.Next() {
		var role, tt, text string
		if err := rows.Scan(&role, &tt, &text); err != nil {
			t.Fatalf("scan: %v", err)
		}
		before[role+"\x00"+tt] = text
	}
	rows.Close()
	if len(before) == 0 {
		t.Fatal("no rows survived 00061 — this round trip would prove nothing")
	}

	if err := goose.UpTo(db, "migrations", migration00062Version); err != nil {
		t.Fatalf("up: %v", err)
	}
	if err := goose.DownTo(db, "migrations", migration00062PriorVersion); err != nil {
		t.Fatalf("down: %v", err)
	}

	after := map[string]string{}
	rows, err = db.Query(`SELECT role_key, task_type, text FROM lessons`)
	if err != nil {
		t.Fatalf("read post-Down lessons: %v", err)
	}
	for rows.Next() {
		var role, tt, text string
		if err := rows.Scan(&role, &tt, &text); err != nil {
			t.Fatalf("scan: %v", err)
		}
		after[role+"\x00"+tt] = text
	}
	rows.Close()

	if len(after) != len(before) {
		t.Fatalf("Down produced %d rows, want %d", len(after), len(before))
	}
	for k, want := range before {
		if got, ok := after[k]; !ok || got != want {
			t.Errorf("Down did not restore %q: got %q (present=%v), want %q",
				strings.ReplaceAll(k, "\x00", "/"), got, ok, want)
		}
	}
}
