package main

// migration_00068_member_kind_staff_test.go — T-48.
//
// 00068 renames the member kind 'assistant' to 'staff'. SQLite cannot alter a
// CHECK in place, so the migration rebuilds `member`: create member_rebuild with
// the CHECK set {'staff','warden','outsource'}, copy every row through a named
// 35-column INSERT…SELECT that maps 'assistant' -> 'staff' (leaving 'warden' and
// 'outsource' alone), DROP TABLE member, RENAME, and then re-create
//
//	CREATE UNIQUE INDEX idx_member_codename ON member (codename)
//	  WHERE codename IS NOT NULL
//
// which the DROP TABLE destroyed. The Down reverses all of it.
//
// 🔴 WHICH MUTATION EACH TEST IS THE GUARD FOR. Every test below is written
// against one specific way the migration could be wrong; a test that no mutation
// can turn red is decoration, so each is named with its mutant:
//
//	TestMigration00068UpMapsAssistantToStaff
//	  → the CASE is dropped, inverted, or widened: 'assistant' rows left as
//	    'assistant' (or the WHOLE copy replaced by a no-op), or 'warden' /
//	    'outsource' swept up by a blanket `'staff' AS kind`, or rows lost by a
//	    WHERE on the SELECT. The anti-vacuity guard is what stops an empty
//	    fixture from making all of that pass.
//
//	TestMigration00068UpPreservesEveryColumn
//	  → the INSERT column list and the SELECT list drift apart — two same-typed
//	    columns transposed (actual_model/actual_runtime, waking_since/
//	    stopping_since …), a column silently defaulted, a NULL flattened to ''
//	    or 0. Row-count and kind assertions are all blind to this; only a
//	    column-by-column readback sees it.
//
//	TestMigration00068KindCheckSet
//	  → the rebuild's CHECK is not actually changed (still lists 'assistant'),
//	    or is changed to something that admits both, or the column is created
//	    with no CHECK at all. Asserted through the constraint's behaviour, so a
//	    CHECK that exists but does not bind still fails.
//
//	TestMigration00068IndexSurvivesUp  🔴 the important one
//	  → the trailing CREATE UNIQUE INDEX is forgotten (DROP TABLE took it and
//	    RENAME does not bring it back — codenames silently become duplicable), or
//	    it is re-created without UNIQUE, or without the `WHERE codename IS NOT
//	    NULL` clause (which would make every codename-less row collide with every
//	    other one and break the migration or the next insert). Checked
//	    BEHAVIOURALLY: matching sqlite_master.sql as a string would pass on an
//	    index that does not bind and would break on harmless whitespace.
//
//	TestMigration00068DownRestoresThePreUpState
//	  → the Down's CASE is missing or wrong ('staff' left as 'staff' against a
//	    CHECK that no longer admits it, or warden/outsource rewritten), its
//	    column lists drift, or — the quiet one — its own trailing CREATE UNIQUE
//	    INDEX is missing, leaving a rolled-back database that looks fine and
//	    enforces nothing.
//
//	TestMigration00068UpDownUpIsStable
//	  → the round trip is not idempotent: a second Up over a Down'd database
//	    produces a different population than the first (a mapping that is not 1:1
//	    in both directions, or a Down that leaves residue behind).

import (
	"database/sql"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/pressly/goose/v3"
)

const (
	migration00068Version      = 68
	migration00068PriorVersion = migration00068Version - 1
)

// migration00068Columns is the full member column list as of 00068, in the order
// the migration itself names them. Enumerated by hand from the migration's
// CREATE TABLE rather than from pragma_table_info, so that a rebuild which
// forgot a column is a disagreement between this list and the database rather
// than a list that quietly follows the mistake.
func migration00068Columns() []string {
	return []string{
		"id", "name", "kind", "role_key", "model", "effort",
		"desired_state", "desired_machine_id", "waking_since", "stopping_since",
		"stopped_since", "refocus_since", "banked_cost", "last_op", "last_op_ok",
		"last_op_log", "last_op_at", "roster_status", "last_op_reason", "linked_task_id",
		"codename", "created_ts", "released_ts", "activated_ts", "runtime", "last_machine_id",
		"avatar_attachment_id", "actual_model", "actual_runtime", "actual_effort",
		"refocus_op", "session_boot_ts", "forced_stop_at", "handover_noticed_ts",
		"agent_iat_floor",
	}
}

// migration00068Fixture is the pre-68 world. EVERY column of every row carries a
// distinctive non-default value, and no two columns of the same row share one —
// that is what makes a transposed pair in the INSERT…SELECT visible instead of
// merely plausible. NULLs (the three nullable columns), CJK text, empty strings
// and non-zero REALs are all represented.
//
// Kinds: three 'assistant' (the population that must move), one 'warden' and one
// 'outsource' (the populations that must not).
func migration00068Fixture() []map[string]any {
	return []map[string]any{
		{
			"id": "m-a1", "name": "Alpha Assistant", "kind": "assistant",
			"role_key": "rk-alpha", "model": "opus-a1", "effort": "high",
			"desired_state": "online", "desired_machine_id": "m-mach-a1",
			"waking_since": 1.5, "stopping_since": 2.25, "stopped_since": 3.125,
			"refocus_since": 4.0625, "banked_cost": 5.5, "last_op": "op-alpha",
			"last_op_ok": int64(1), "last_op_log": "log-alpha", "last_op_at": 6.75,
			"roster_status": "active", "last_op_reason": "reason-alpha",
			"linked_task_id": "t-alpha", "codename": "codename-alpha",
			"created_ts": 7.875, "released_ts": 8.0625, "activated_ts": 9.5,
			"runtime": "claude-alpha", "last_machine_id": "m-last-alpha",
			"avatar_attachment_id": "ava-alpha", "actual_model": "actual-model-alpha",
			"actual_runtime": "actual-runtime-alpha", "actual_effort": "actual-effort-alpha",
			"refocus_op": "refocus-alpha", "session_boot_ts": 10.25,
			"forced_stop_at": 11.125, "handover_noticed_ts": 12.0625,
			"agent_iat_floor": 13.5,
		},
		{
			// The NULL / empty / CJK row: everything nullable is NULL, and the
			// texts are the shapes a careless rebuild would normalise away.
			"id": "m-a2", "name": "第二位助理", "kind": "assistant",
			"role_key": "", "model": "模型-乙", "effort": "",
			"desired_state": "offline", "desired_machine_id": "機器-乙",
			"waking_since": 0.5, "stopping_since": 0.25, "stopped_since": 0.125,
			"refocus_since": 0.0625, "banked_cost": 0.03125, "last_op": "  空白  ",
			"last_op_ok": nil, "last_op_log": "日誌\n第二行\t分隔", "last_op_at": 14.5,
			"roster_status": "removed", "last_op_reason": "",
			"linked_task_id": nil, "codename": nil,
			"created_ts": 15.25, "released_ts": 16.125, "activated_ts": 17.0625,
			"runtime": "codex-乙", "last_machine_id": "",
			"avatar_attachment_id": "ava-乙", "actual_model": "實際模型",
			"actual_runtime": "實際執行器", "actual_effort": "實際力度",
			"refocus_op": "重新聚焦-乙", "session_boot_ts": 18.5,
			"forced_stop_at": 19.25, "handover_noticed_ts": 20.125,
			"agent_iat_floor": 21.0625,
		},
		{
			// A second codename-less row: two NULL codenames must coexist both
			// before and after the rebuild, which is the partial half of the
			// index expressed as data rather than as an assertion.
			"id": "m-a3", "name": "Third Assistant", "kind": "assistant",
			"role_key": "rk-gamma", "model": "model-gamma", "effort": "low",
			"desired_state": "online", "desired_machine_id": "m-mach-gamma",
			"waking_since": 22.5, "stopping_since": 23.25, "stopped_since": 24.125,
			"refocus_since": 25.0625, "banked_cost": 26.03125, "last_op": "op-gamma",
			"last_op_ok": int64(0), "last_op_log": "log-gamma", "last_op_at": 27.5,
			"roster_status": "active", "last_op_reason": "reason-gamma",
			"linked_task_id": "t-gamma", "codename": nil,
			"created_ts": 28.25, "released_ts": 29.125, "activated_ts": 30.0625,
			"runtime": "runtime-gamma", "last_machine_id": "m-last-gamma",
			"avatar_attachment_id": "ava-gamma", "actual_model": "actual-model-gamma",
			"actual_runtime": "actual-runtime-gamma", "actual_effort": "actual-effort-gamma",
			"refocus_op": "refocus-gamma", "session_boot_ts": 31.5,
			"forced_stop_at": 32.25, "handover_noticed_ts": 33.125,
			"agent_iat_floor": 34.0625,
		},
		{
			"id": "m-w1", "name": "The Warden", "kind": "warden",
			"role_key": "rk-warden", "model": "model-warden", "effort": "medium",
			"desired_state": "online", "desired_machine_id": "m-mach-warden",
			"waking_since": 35.5, "stopping_since": 36.25, "stopped_since": 37.125,
			"refocus_since": 38.0625, "banked_cost": 39.03125, "last_op": "op-warden",
			"last_op_ok": int64(1), "last_op_log": "log-warden", "last_op_at": 40.5,
			"roster_status": "active", "last_op_reason": "reason-warden",
			"linked_task_id": "t-warden", "codename": "codename-warden",
			"created_ts": 41.25, "released_ts": 42.125, "activated_ts": 43.0625,
			"runtime": "runtime-warden", "last_machine_id": "m-last-warden",
			"avatar_attachment_id": "ava-warden", "actual_model": "actual-model-warden",
			"actual_runtime": "actual-runtime-warden", "actual_effort": "actual-effort-warden",
			"refocus_op": "refocus-warden", "session_boot_ts": 44.5,
			"forced_stop_at": 45.25, "handover_noticed_ts": 46.125,
			"agent_iat_floor": 47.0625,
		},
		{
			"id": "m-o1", "name": "外包 worker", "kind": "outsource",
			"role_key": "rk-outsource", "model": "model-outsource", "effort": "high",
			"desired_state": "offline", "desired_machine_id": "m-mach-outsource",
			"waking_since": 48.5, "stopping_since": 49.25, "stopped_since": 50.125,
			"refocus_since": 51.0625, "banked_cost": 52.03125, "last_op": "op-outsource",
			"last_op_ok": int64(0), "last_op_log": "log-outsource", "last_op_at": 53.5,
			"roster_status": "removed", "last_op_reason": "reason-outsource",
			"linked_task_id": "t-outsource", "codename": "codename-outsource",
			"created_ts": 54.25, "released_ts": 55.125, "activated_ts": 56.0625,
			"runtime": "runtime-outsource", "last_machine_id": "m-last-outsource",
			"avatar_attachment_id": "ava-outsource", "actual_model": "actual-model-outsource",
			"actual_runtime": "actual-runtime-outsource", "actual_effort": "actual-effort-outsource",
			"refocus_op": "refocus-outsource", "session_boot_ts": 57.5,
			"forced_stop_at": 58.25, "handover_noticed_ts": 59.0625,
			"agent_iat_floor": 60.125,
		},
	}
}

// migration00068WantKindAfterUp is the kind each fixture row must carry once
// 00068 has run. Worked out by hand from what the migration claims, never
// derived from its own CASE expression.
func migration00068WantKindAfterUp(before string) string {
	if before == "assistant" {
		return "staff"
	}
	return before
}

// migration00068Norm renders a scanned or seeded cell as a comparable string,
// carrying its type along so that a NULL flattened to ” or a REAL rounded into
// an INTEGER is a difference rather than a match.
func migration00068Norm(v any) string {
	switch t := v.(type) {
	case nil:
		return "NULL"
	case []byte:
		return fmt.Sprintf("TEXT(%q)", string(t))
	case string:
		return fmt.Sprintf("TEXT(%q)", t)
	case int64:
		return fmt.Sprintf("INT(%d)", t)
	case float64:
		return fmt.Sprintf("REAL(%v)", t)
	default:
		return fmt.Sprintf("%T(%v)", v, v)
	}
}

// migration00068World brings a temp database to the state just BEFORE 00068 and
// seeds the fixture there (kind='assistant' is still legal at that version).
func migration00068World(t *testing.T) *sql.DB {
	t.Helper()
	db, err := openSQLite(filepath.Join(t.TempDir(), "member-kind-staff.db"))
	if err != nil {
		t.Fatalf("open temp sqlite: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if err := runMigrations(db); err != nil {
		t.Fatalf("goose up: %v", err)
	}
	if err := goose.DownTo(db, "migrations", migration00068PriorVersion); err != nil {
		t.Fatalf("down to %d: %v", migration00068PriorVersion, err)
	}

	cols := migration00068Columns()
	placeholders := strings.TrimSuffix(strings.Repeat("?, ", len(cols)), ", ")
	stmt := fmt.Sprintf(`INSERT INTO member (%s) VALUES (%s)`,
		strings.Join(cols, ", "), placeholders)
	for _, row := range migration00068Fixture() {
		args := make([]any, 0, len(cols))
		for _, c := range cols {
			v, ok := row[c]
			if !ok {
				t.Fatalf("fixture row %v declares no value for column %q — every column "+
					"must carry a distinctive value or the rebuild can drop it unnoticed",
					row["id"], c)
			}
			args = append(args, v)
		}
		if _, err := db.Exec(stmt, args...); err != nil {
			t.Fatalf("seed member %v: %v", row["id"], err)
		}
	}

	// ANTI-VACUITY: a fixture that failed to land would let every assertion
	// below pass over an empty table, which is indistinguishable from a working
	// migration. member is empty at this version, so the count must be exact.
	var seeded int
	if err := db.QueryRow(`SELECT COUNT(*) FROM member`).Scan(&seeded); err != nil {
		t.Fatalf("count seeded rows: %v", err)
	}
	if seeded != len(migration00068Fixture()) {
		t.Fatalf("seeded %d member rows, wrote %d — the fixture did not land",
			seeded, len(migration00068Fixture()))
	}
	return db
}

// migration00068ReadAll reads the whole member table as id → column → normalised
// value, by NAME, so a rebuild that reordered the physical columns is not
// mistaken for a rebuild that moved the data.
func migration00068ReadAll(t *testing.T, db *sql.DB) map[string]map[string]string {
	t.Helper()
	cols := migration00068Columns()
	rows, err := db.Query(fmt.Sprintf(`SELECT %s FROM member`, strings.Join(cols, ", ")))
	if err != nil {
		t.Fatalf("read member: %v", err)
	}
	defer rows.Close()
	out := map[string]map[string]string{}
	for rows.Next() {
		cells := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range cells {
			ptrs[i] = &cells[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			t.Fatalf("scan member: %v", err)
		}
		row := map[string]string{}
		for i, c := range cols {
			row[c] = migration00068Norm(cells[i])
		}
		id, _ := cells[0].(string)
		if b, ok := cells[0].([]byte); ok {
			id = string(b)
		}
		if _, dup := out[id]; dup {
			t.Fatalf("member id %q appears twice — the rebuild duplicated a row", id)
		}
		out[id] = row
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows: %v", err)
	}
	return out
}

// migration00068AssertCodenameIndex is the BEHAVIOURAL index check, shared by
// the Up and the Down tests. It asks the database to enforce the two halves of
// the index rather than reading its DDL back as a string:
//
//	UNIQUE  — a second row with the same non-null codename must be REFUSED.
//	PARTIAL — two rows with a NULL codename must both be ACCEPTED.
//
// kind is the legal member kind at the schema version under test, since the
// CHECK set differs either side of the migration. Rows are inserted under a
// caller-supplied id prefix so repeated calls in one database do not collide.
func migration00068AssertCodenameIndex(t *testing.T, db *sql.DB, kind, prefix string) {
	t.Helper()

	var idxs int
	if err := db.QueryRow(
		`SELECT COUNT(*) FROM sqlite_master WHERE type = 'index' AND name = 'idx_member_codename'`,
	).Scan(&idxs); err != nil {
		t.Fatalf("query sqlite_master for idx_member_codename: %v", err)
	}
	if idxs != 1 {
		t.Errorf("idx_member_codename is present %d times, want 1 — DROP TABLE member takes "+
			"the index with it and RENAME does not bring it back, so a rebuild that forgets "+
			"to re-create it leaves codenames silently duplicable", idxs)
	}

	insert := func(id, codename string, null bool) error {
		if null {
			_, err := db.Exec(`INSERT INTO member (id, kind, codename) VALUES (?, ?, NULL)`, id, kind)
			return err
		}
		_, err := db.Exec(`INSERT INTO member (id, kind, codename) VALUES (?, ?, ?)`, id, kind, codename)
		return err
	}

	// UNIQUE half.
	if err := insert(prefix+"-uniq-1", prefix+"-shared-codename", false); err != nil {
		t.Fatalf("insert first row with codename %q: %v (this insert must succeed for the "+
			"duplicate below to mean anything)", prefix+"-shared-codename", err)
	}
	if err := insert(prefix+"-uniq-2", prefix+"-shared-codename", false); err == nil {
		t.Errorf("a SECOND row with codename %q was ACCEPTED — idx_member_codename is not "+
			"enforcing uniqueness. Either it was not re-created after the rebuild's DROP "+
			"TABLE, or it was re-created without UNIQUE. Nothing raises when this breaks; "+
			"the next codename collision is a live data bug instead of an error",
			prefix+"-shared-codename")
	}

	// PARTIAL half, part one: several codename-less rows must coexist. This is
	// the property that matters in production — every staff and warden row has a
	// NULL codename — but on its own it does NOT discriminate, and saying so is
	// the point of the second probe below: SQLite treats NULLs as distinct in ANY
	// unique index, so a mutant that dropped the WHERE clause would still accept
	// these two inserts. Measured, not assumed.
	if err := insert(prefix+"-null-1", "", true); err != nil {
		t.Fatalf("first NULL-codename row rejected: %v", err)
	}
	if err := insert(prefix+"-null-2", "", true); err != nil {
		t.Errorf("a SECOND NULL-codename row was REJECTED (%v) — codename-less members "+
			"must never collide with one another", err)
	}

	// PARTIAL half, part two: the probe that actually separates a partial index
	// from a full one, and still without string-matching any DDL. A partial
	// index does not cover the rows outside its WHERE clause, so SQLite REFUSES
	// to plan a whole-table query that is forced through it ("no query
	// solution"); a full index answers the same query happily. The planner's own
	// verdict is the assertion.
	if rows, err := db.Query(
		`SELECT id FROM member INDEXED BY idx_member_codename ORDER BY codename`); err == nil {
		rows.Close()
		t.Errorf("a WHOLE-TABLE query forced through idx_member_codename was planned " +
			"successfully — the index covers every row, which means it lost its " +
			"`WHERE codename IS NOT NULL` clause and is no longer PARTIAL. (The " +
			"multi-NULL insert above cannot see this: SQLite considers NULLs distinct in " +
			"any unique index.)")
	}
	// Positive control for that probe: the SAME forced plan must SUCCEED once the
	// query stays inside the index's WHERE clause. Without this, an index that
	// was missing or unusable for some entirely different reason would satisfy
	// the refusal above and read as "partial".
	rows, err := db.Query(
		`SELECT id FROM member INDEXED BY idx_member_codename WHERE codename IS NOT NULL ORDER BY codename`)
	if err != nil {
		t.Errorf("a query restricted to codename IS NOT NULL could NOT be planned through "+
			"idx_member_codename (%v) — the index is missing or does not cover the rows it "+
			"is supposed to, so the partiality probe above proves nothing", err)
	} else {
		rows.Close()
	}
}

// ── 1. the mapping ───────────────────────────────────────────────────────────

// TestMigration00068UpMapsAssistantToStaff is the load-bearing assertion of the
// Up: every 'assistant' becomes 'staff', 'warden' and 'outsource' are
// byte-identical, and not one row is gained or lost.
func TestMigration00068UpMapsAssistantToStaff(t *testing.T) {
	db := migration00068World(t)

	// Anti-vacuity, second half: the fixture must actually contain all three
	// kinds, or "warden was untouched" is a claim about nothing.
	kindsSeeded := map[string]int{}
	for _, r := range migration00068Fixture() {
		kindsSeeded[r["kind"].(string)]++
	}
	for _, k := range []string{"assistant", "warden", "outsource"} {
		if kindsSeeded[k] == 0 {
			t.Fatalf("the fixture seeds no %q rows — this test would prove nothing about them", k)
		}
	}

	if err := goose.UpTo(db, "migrations", migration00068Version); err != nil {
		t.Fatalf("goose up through %d: %v", migration00068Version, err)
	}

	got := migration00068ReadAll(t, db)
	if len(got) != len(migration00068Fixture()) {
		t.Fatalf("member holds %d rows after 00068, want %d — the rebuild's INSERT…SELECT "+
			"must copy every row, and a WHERE or a failed copy loses members silently",
			len(got), len(migration00068Fixture()))
	}

	for _, want := range migration00068Fixture() {
		id := want["id"].(string)
		t.Run(id, func(t *testing.T) {
			row, ok := got[id]
			if !ok {
				t.Fatalf("member %q did not survive 00068", id)
			}
			before := want["kind"].(string)
			wantKind := migration00068WantKindAfterUp(before)
			if row["kind"] != migration00068Norm(wantKind) {
				t.Errorf("member %q was kind %q before 00068 and is %s after, want %s — "+
					"the migration maps 'assistant' -> 'staff' value by value and passes "+
					"'warden' / 'outsource' through untouched",
					id, before, row["kind"], migration00068Norm(wantKind))
			}
		})
	}

	// And the population as a whole, which a per-row loop cannot see: no kind
	// may appear that the fixture did not put there.
	var stray int
	if err := db.QueryRow(
		`SELECT COUNT(*) FROM member WHERE kind NOT IN ('staff', 'warden', 'outsource')`,
	).Scan(&stray); err != nil {
		t.Fatalf("count stray kinds: %v", err)
	}
	if stray != 0 {
		t.Errorf("%d member rows carry a kind outside {'staff','warden','outsource'} after 00068", stray)
	}
	var assistants int
	if err := db.QueryRow(`SELECT COUNT(*) FROM member WHERE kind = 'assistant'`).Scan(&assistants); err != nil {
		t.Fatalf("count leftover assistants: %v", err)
	}
	if assistants != 0 {
		t.Errorf("%d rows still carry kind='assistant' after 00068 — the whole point of the "+
			"migration is that this population is renamed, not merely joined by a new name", assistants)
	}
}

// ── 2. everything that is NOT kind ───────────────────────────────────────────

// TestMigration00068UpPreservesEveryColumn is the test that catches a mis-ordered
// INSERT…SELECT column list. The two lists in the migration are long, hand-typed
// and名 for column, so a transposed same-typed pair is both easy to write and
// invisible to every other assertion in this file.
func TestMigration00068UpPreservesEveryColumn(t *testing.T) {
	db := migration00068World(t)
	before := migration00068ReadAll(t, db)
	if len(before) == 0 {
		t.Fatal("no rows seeded — this comparison would prove nothing")
	}

	if err := goose.UpTo(db, "migrations", migration00068Version); err != nil {
		t.Fatalf("goose up through %d: %v", migration00068Version, err)
	}
	after := migration00068ReadAll(t, db)

	for _, seeded := range migration00068Fixture() {
		id := seeded["id"].(string)
		t.Run(id, func(t *testing.T) {
			pre, ok := before[id]
			if !ok {
				t.Fatalf("member %q was not in the pre-Up readback", id)
			}
			post, ok := after[id]
			if !ok {
				t.Fatalf("member %q did not survive 00068", id)
			}
			for _, c := range migration00068Columns() {
				if c == "kind" {
					continue // deliberately changed; covered by its own test
				}
				if post[c] != pre[c] {
					t.Errorf("column %q of member %q changed across the rebuild: got %s, want %s\n\n"+
						"Every column other than kind is copied verbatim. A value that moved to "+
						"a NEIGHBOURING column means the INSERT list and the SELECT list have "+
						"drifted apart; a value that became NULL/''/0 means the column was "+
						"dropped from one of them and took its default.",
						c, id, post[c], pre[c])
				}
			}
			// Cross-check against the hand-written fixture too, so a readback
			// helper that is itself wrong in both directions cannot hide.
			for _, c := range migration00068Columns() {
				if c == "kind" {
					continue
				}
				if want := migration00068Norm(seeded[c]); post[c] != want {
					t.Errorf("column %q of member %q is %s after 00068, want the seeded %s",
						c, id, post[c], want)
				}
			}
		})
	}
}

// ── 3. the CHECK set ─────────────────────────────────────────────────────────

// TestMigration00068KindCheckSet proves the CHECK really changed, from both
// sides: the retired value must be refused and the new one accepted. Asserted on
// err being nil / non-nil only — the message SQLite produces for a CHECK is not
// this migration's contract.
func TestMigration00068KindCheckSet(t *testing.T) {
	db := migration00068World(t)
	if err := goose.UpTo(db, "migrations", migration00068Version); err != nil {
		t.Fatalf("goose up through %d: %v", migration00068Version, err)
	}

	cases := []struct {
		name    string
		kind    string
		wantErr bool
		why     string
	}{
		{"retired assistant is refused", "assistant", true,
			"the rebuilt CHECK lists {'staff','warden','outsource'}; if 'assistant' is still " +
				"insertable the CHECK was not actually rewritten (or was dropped entirely)"},
		{"new staff is accepted", "staff", false,
			"'staff' is the whole point of the migration; refusing it means the new CHECK " +
				"never learned the new name"},
		{"warden still accepted", "warden", false, "warden was never part of the rename"},
		{"outsource still accepted", "outsource", false, "outsource was never part of the rename"},
		{"an unrelated kind is still refused", "manager", true,
			"the CHECK must stay a closed set — a rebuild that dropped it altogether would " +
				"accept everything and pass the two positive cases above"},
	}
	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			id := fmt.Sprintf("m-check-%d", i)
			_, err := db.Exec(`INSERT INTO member (id, kind) VALUES (?, ?)`, id, tc.kind)
			if tc.wantErr && err == nil {
				t.Errorf("INSERT with kind=%q was ACCEPTED, want rejected — %s", tc.kind, tc.why)
			}
			if !tc.wantErr && err != nil {
				t.Errorf("INSERT with kind=%q was REJECTED (%v), want accepted — %s", tc.kind, err, tc.why)
			}
		})
	}
}

// ── 4. 🔴 the index ──────────────────────────────────────────────────────────

// TestMigration00068IndexSurvivesUp is the most important test in this file.
// `DROP TABLE member` destroys idx_member_codename and `RENAME` does not bring
// it back, so the Up has to re-create it explicitly — and if it does not,
// NOTHING raises: codenames simply become duplicable and the next collision is a
// live data bug. Checked behaviourally, plus existence by name.
func TestMigration00068IndexSurvivesUp(t *testing.T) {
	db := migration00068World(t)

	// Negative control: the guarantee exists BEFORE the migration, so a green
	// result below is about the rebuild preserving it rather than about the
	// index never having been there.
	migration00068AssertCodenameIndex(t, db, "assistant", "pre")

	if err := goose.UpTo(db, "migrations", migration00068Version); err != nil {
		t.Fatalf("goose up through %d: %v", migration00068Version, err)
	}
	migration00068AssertCodenameIndex(t, db, "staff", "post")
}

// ── 5. the Down ──────────────────────────────────────────────────────────────

// TestMigration00068DownRestoresThePreUpState is exact rather than plausible:
// 'staff' and 'assistant' are the same population under two names, so the
// mapping is 1:1 in both directions and no row becomes unrepresentable. The
// Down also does DROP TABLE, so it owes the index back too.
func TestMigration00068DownRestoresThePreUpState(t *testing.T) {
	db := migration00068World(t)
	before := migration00068ReadAll(t, db)
	if len(before) == 0 {
		t.Fatal("no rows seeded — this round trip would prove nothing")
	}

	if err := goose.UpTo(db, "migrations", migration00068Version); err != nil {
		t.Fatalf("up: %v", err)
	}
	if err := goose.DownTo(db, "migrations", migration00068PriorVersion); err != nil {
		t.Fatalf("down: %v", err)
	}
	after := migration00068ReadAll(t, db)

	if len(after) != len(before) {
		t.Fatalf("Down produced %d member rows, want %d", len(after), len(before))
	}
	for _, seeded := range migration00068Fixture() {
		id := seeded["id"].(string)
		t.Run(id, func(t *testing.T) {
			post, ok := after[id]
			if !ok {
				t.Fatalf("member %q did not survive the round trip", id)
			}
			for _, c := range migration00068Columns() {
				if post[c] != before[id][c] {
					t.Errorf("column %q of member %q is %s after Up→Down, want the pre-Up %s — "+
						"kind must be mapped back to 'assistant' and every other column is "+
						"copied verbatim in both directions",
						c, id, post[c], before[id][c])
				}
			}
		})
	}

	// The quiet failure: a Down that forgets its own CREATE UNIQUE INDEX leaves
	// a database that looks fine and enforces nothing. 'assistant' is the legal
	// kind again at this version.
	migration00068AssertCodenameIndex(t, db, "assistant", "post-down")
}

// ── 6. stability ─────────────────────────────────────────────────────────────

// TestMigration00068UpDownUpIsStable runs the round trip twice: the state after
// the second Up must equal the state after the first. A mapping that is not 1:1
// in both directions, or a Down that leaves residue, diverges here even when
// each single direction looks right on its own.
func TestMigration00068UpDownUpIsStable(t *testing.T) {
	db := migration00068World(t)

	if err := goose.UpTo(db, "migrations", migration00068Version); err != nil {
		t.Fatalf("first up: %v", err)
	}
	firstUp := migration00068ReadAll(t, db)
	if len(firstUp) == 0 {
		t.Fatal("no rows after the first Up — this round trip would prove nothing")
	}

	if err := goose.DownTo(db, "migrations", migration00068PriorVersion); err != nil {
		t.Fatalf("down: %v", err)
	}
	if err := goose.UpTo(db, "migrations", migration00068Version); err != nil {
		t.Fatalf("second up: %v", err)
	}
	secondUp := migration00068ReadAll(t, db)

	// Flattened and sorted so the diff names the whole population, not the first
	// cell that happens to differ.
	flatten := func(m map[string]map[string]string) []string {
		out := make([]string, 0, len(m))
		for id, row := range m {
			cells := make([]string, 0, len(row))
			for _, c := range migration00068Columns() {
				cells = append(cells, c+"="+row[c])
			}
			out = append(out, id+"\x00"+strings.Join(cells, "\x00"))
		}
		sort.Strings(out)
		return out
	}
	got, want := flatten(secondUp), flatten(firstUp)
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("UP→DOWN→UP DID NOT LAND WHERE THE FIRST UP DID.\n got (%d rows): %q\nwant (%d rows): %q",
			len(got), got, len(want), want)
	}

	// The index must survive the second Up as well — it is re-created by every
	// direction, so a rebuild that only remembers it once shows up here.
	migration00068AssertCodenameIndex(t, db, "staff", "second-up")
}

// 🔴 THE HAND LIST IS A CLOSED LOOP UNTIL THIS TEST EXISTS.
//
// migration00068Columns() is enumerated by hand ON PURPOSE (see its own comment):
// a rebuild that forgot a column then disagrees with the list instead of quietly
// following the mistake. That catches one direction and one only —
// "the migration dropped a column THE LIST KNOWS ABOUT".
//
// It cannot catch the other direction, and that is the one that loses data:
// SOMEBODY ADDS A 36th COLUMN TO member IN AN EARLIER MIGRATION. The list does
// not know about it, the INSERT…SELECT below does not name it, and every
// assertion in this file compares the list against itself — so the rebuild drops
// the column for every existing row and nothing goes red. The real schema had
// never entered the comparison at all.
//
// That is not hypothetical here. Merge order for this package is serialised and
// this rebuild is deliberately LAST (a migration numbered below the database's
// current version refuses to start the server at all), so every column added by
// anything that lands first passes through this rebuild. As of 2026-09-03 there
// is exactly one such change in flight: #387 adds member.restart_after_stop.
//
// So this test is the one place the LIVE schema meets the list.
func TestMigration00068ColumnListMatchesTheLiveSchema(t *testing.T) {
	db := migration00068World(t)

	rows, err := db.Query(`SELECT name FROM pragma_table_info('member')`)
	if err != nil {
		t.Fatalf("pragma_table_info(member): %v", err)
	}
	defer rows.Close()
	live := map[string]bool{}
	var liveOrder []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatalf("scan column name: %v", err)
		}
		live[name] = true
		liveOrder = append(liveOrder, name)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate columns: %v", err)
	}
	if len(liveOrder) == 0 {
		t.Fatalf("pragma_table_info returned no columns for member — the query, not the schema, is wrong")
	}

	listed := map[string]bool{}
	for _, c := range migration00068Columns() {
		listed[c] = true
	}

	var missingFromList, missingFromSchema []string
	for _, c := range liveOrder {
		if !listed[c] {
			missingFromList = append(missingFromList, c)
		}
	}
	for _, c := range migration00068Columns() {
		if !live[c] {
			missingFromSchema = append(missingFromSchema, c)
		}
	}

	// The message has to tell whoever tripped it WHAT BREAKS and WHAT TO DO.
	// "not equal" alone gets ignored: the person who added the column has no
	// reason to think this file is any of their business.
	if len(missingFromList) > 0 {
		t.Errorf("member has %d column(s) this migration does not know about: %s\n"+
			"⇒ You added a column to `member`. Update migration00068Columns() AND the "+
			"INSERT…SELECT in migrations/%05d_member_kind_assistant_to_staff.sql (both "+
			"directions, Up and Down) — otherwise that rebuild COPIES THE TABLE WITHOUT "+
			"YOUR COLUMN and every existing row silently loses its value.",
			len(missingFromList), strings.Join(missingFromList, ", "), migration00068Version)
	}
	if len(missingFromSchema) > 0 {
		t.Errorf("migration00068Columns() names %d column(s) that member does not have: %s\n"+
			"⇒ Either the column was dropped by an earlier migration and this list is stale, "+
			"or the name is misspelled. The rebuild would fail on it.",
			len(missingFromSchema), strings.Join(missingFromSchema, ", "))
	}
}
