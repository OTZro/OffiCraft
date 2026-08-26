package main

// migration_00061_restore_door_test.go — T-2 step A, the SECOND door.
//
// 00061 deleting every non-general `lessons` row only makes the table
// non-general AT THAT INSTANT. document_history is a separate table and the
// restore route writes straight back into `lessons` from it: given a retained
// revision under the key "<role_key>::<task_type>", the lessons arm of
// restoreDocumentHistory splits the key and hands the task_type it finds to
// putLessonsOn VERBATIM — no arm of that path compares it to 'general'.
//
// So a migration that deletes only the lessons rows leaves a live writer that
// can put a non-general row back, and the whole reason 00061 exists is that no
// such row may survive to the later column drop (where two task_types under one
// role_key fold onto one key). This is the end-to-end proof that the door is
// shut: write a non-general lessons doc through the real handler, run the
// migration, then push the retained revision back through the real restore
// handler, and require that `lessons` still holds no non-general row.
//
// It is deliberately an END-TO-END test through the HTTP handlers rather than a
// SQL-level one: the gap it guards is precisely that the SQL looked complete
// while the handler above it did not.

import (
	"database/sql"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/pressly/goose/v3"
)

// migration00061RestoreWorld returns a fully migrated API server together with
// the handle its DAL writes through, because this test has to drive goose over
// the same database the handlers use.
func migration00061RestoreWorld(t *testing.T) (*apiServer, *sql.DB) {
	t.Helper()
	db, err := openSQLite(filepath.Join(t.TempDir(), "restore-door.db"))
	if err != nil {
		t.Fatalf("open temp sqlite: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if err := runMigrations(db); err != nil {
		t.Fatalf("goose up: %v", err)
	}
	return newAPIServer(NewDAL(db), NewHub(), []byte("restore-door-secret"), 3600,
		assetRoot(t.TempDir())), db
}

// replaceLessonsThroughHandler drives the real write face, which is what
// retains a document_history revision under "<role_key>::<task_type>".
func replaceLessonsThroughHandler(t *testing.T, api *apiServer, role, taskType, text string) {
	t.Helper()
	rec := httptest.NewRecorder()
	api.HandleReplaceLessonsApiLessonsRoleKeyTaskTypePost(rec, taskReq(t, http.MethodPost,
		"/api/lessons/"+role+"/"+taskType, map[string]any{"text": text}, "owner", "owner"),
		role, taskType)
	if rec.Code != http.StatusOK {
		t.Fatalf("replace_lessons(%s, %s): %d %s", role, taskType, rec.Code, rec.Body.String())
	}
}

// rerun00061 re-executes the migration's Up against a database that already has
// it. goose refuses to re-run an applied version, so the cycle goes down to the
// version below it (00061's Down is a declared no-op) and back up — which is
// what actually re-executes the DELETEs.
func rerun00061(t *testing.T, db *sql.DB) {
	t.Helper()
	if err := goose.DownTo(db, "migrations", migration00061PriorVersion); err != nil {
		t.Fatalf("down to %d: %v", migration00061PriorVersion, err)
	}
	if err := runMigrations(db); err != nil {
		t.Fatalf("goose up through %d: %v", migration00061Version, err)
	}
}

// nonGeneralLessonsRows lists every surviving non-general row as "role/task_type".
func nonGeneralLessonsRows(t *testing.T, db *sql.DB) []string {
	t.Helper()
	rows, err := db.Query(`SELECT role_key, task_type FROM lessons WHERE task_type <> 'general'`)
	if err != nil {
		t.Fatalf("scan lessons: %v", err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var role, tt string
		if err := rows.Scan(&role, &tt); err != nil {
			t.Fatalf("scan: %v", err)
		}
		out = append(out, role+"/"+tt)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows: %v", err)
	}
	sort.Strings(out)
	return out
}

// TestMigration00061RestoreCannotPutANonGeneralLessonBack is the regression
// this fix exists for. It reproduces the reviewer's measured scenario exactly:
// two writes of a non-general lessons doc (the second is what retains the
// first), the migration, then the restore.
func TestMigration00061RestoreCannotPutANonGeneralLessonBack(t *testing.T) {
	api, db := migration00061RestoreWorld(t)
	const role, taskType = seedRoleAssistant, "review-pr-seth"
	key := role + "::" + taskType

	replaceLessonsThroughHandler(t, api, role, taskType, "v1 — the revision that gets retained")
	replaceLessonsThroughHandler(t, api, role, taskType, "v2 — the write that retains v1")

	stored, err := api.dal.ListDocumentHistory("lessons", key)
	if err != nil {
		t.Fatalf("list history: %v", err)
	}
	if len(stored) == 0 {
		t.Fatalf("no retained revision under %q — the fixture did not land, so nothing "+
			"below would be measuring anything", key)
	}

	rerun00061(t, db)

	// Sanity: the migration did its stated job on the lessons table itself.
	// Without this the assertion below could pass because nothing ever wrote.
	if left := nonGeneralLessonsRows(t, db); len(left) != 0 {
		t.Fatalf("00061 did not even clear the lessons table: %s", strings.Join(left, ", "))
	}

	rec := httptest.NewRecorder()
	api.HandleRestoreDocumentHistoryApiDocumentHistoryKindKeyIdRestorePost(rec,
		taskReq(t, http.MethodPost, "/api/document-history/lessons/"+key+"/"+
			strconv.FormatInt(stored[0].ID, 10)+"/restore", nil, "owner", "owner"),
		"lessons", key, stored[0].ID)

	if left := nonGeneralLessonsRows(t, db); len(left) != 0 {
		t.Errorf("THE RESTORE DOOR IS OPEN: after 00061 the restore of retained revision %d "+
			"under key %q answered %d and put %d non-general lessons row(s) back: %s. "+
			"00061 exists so that no non-general row survives to the later task_type column "+
			"drop, where two task_types under one role_key fold onto one key — a restore that "+
			"can reseed one makes the whole migration a momentary state rather than a "+
			"guarantee. The migration must delete the matching document_history rows in the "+
			"SAME change, the way DeleteLessonsForRole already cascades onto them",
			stored[0].ID, key, rec.Code, len(left), strings.Join(left, ", "))
	}
}
