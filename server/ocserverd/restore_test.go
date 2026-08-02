package main

// restore_test.go — T-a90f.
//
// 🔴 SAFETY, stated first because this file tests a DESTRUCTIVE feature. The
// thing under test is a guard ("only files from this station's own backup
// directory") and mutant-verifying a guard means running with the guard
// removed. A test that relied on the guard to stay harmless would be a bomb,
// not a test (repo rule: a test must not depend on the thing it is testing to
// keep itself safe).
//
// The isolation here is STRUCTURAL, not guard-shaped: every path the restore
// code touches is DERIVED from the dbPath it is handed, and every test hands
// it a t.TempDir(). Nothing reads the configured DSN, $OC_DATABASE_URL, or the
// server root, so a guard failing wide open still cannot reach past the temp
// directory. Grep check for a reviewer: this file names no absolute path and
// no production location.
//
// Each test below pins ONE of the four guards the ticket names, and each is
// written so that REMOVING the guard turns it red (the mutant runs are
// recorded on the task).

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func mustStageableBackup(t *testing.T, dbPath string, at time.Time) string {
	t.Helper()
	db, err := openSQLite(dbPath)
	if err != nil {
		t.Fatalf("reopen fixture: %v", err)
	}
	defer db.Close()
	res, err := runDatabaseBackup(db, dbPath, backupReasonManual, at)
	if err != nil {
		t.Fatalf("seed backup: %v", err)
	}
	return filepath.Base(res.Path)
}

func backupNamesIn(t *testing.T, dbPath string) []string {
	t.Helper()
	files, err := backupFilesIn(backupDirFor(dbPath))
	if err != nil {
		t.Fatalf("list backups: %v", err)
	}
	var out []string
	for _, f := range files {
		out = append(out, f.Name())
	}
	return out
}

// ── Guard 1: a restore always snapshots the state it is about to replace ────

// TestStageRestore_SnapshotsTheCurrentStateFirst pins the owner's ruling
// (rc-6e177f1e1fe5): the restore button has no undo, so the flow itself must
// leave a way back. The proof is a pre-restore backup that READS BACK — not
// merely a file that exists.
//
// MUTANT: delete the runDatabaseBackup call in stageRestore → no prerestore
// file, this test fails.
func TestStageRestore_SnapshotsTheCurrentStateFirst(t *testing.T) {
	db, dbPath := seedBackupFixture(t, 20)
	source := mustStageableBackup(t, dbPath, time.Date(2026, 8, 1, 10, 0, 0, 0, time.UTC))

	cmd, err := stageRestore(db, dbPath, source, time.Date(2026, 8, 2, 11, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("stage restore: %v", err)
	}
	if cmd.PreRestore == "" {
		t.Fatal("the restore command names no way back")
	}
	if backupReasonIn(cmd.PreRestore) != backupReasonPreRestore {
		t.Fatalf("way back %q is not a prerestore snapshot", cmd.PreRestore)
	}
	// The file must carry the DATA, not merely exist.
	title, rows := readBackSentinel(t, filepath.Join(backupDirFor(dbPath), cmd.PreRestore))
	if title != "備份哨兵" || rows == 0 {
		t.Fatalf("the way back does not read back (title=%q rows=%d)", title, rows)
	}
}

// TestPreRestoreSnapshotSurvivesRoutineRotation pins the pool split. The
// pre-migration pool exists because a shared quota let five routine backups
// evict the one retreat that mattered, minutes after it was taken. A
// pre-restore snapshot is reached for in exactly the same situation — right
// after a restore turned out to be wrong, which is when somebody is taking
// snapshots by hand.
//
// MUTANT: make backupPoolOf return backupPoolRoutine for prerestore → the
// snapshot is rotated away and this test fails.
func TestPreRestoreSnapshotSurvivesRoutineRotation(t *testing.T) {
	db, dbPath := seedBackupFixture(t, 5)
	base := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	source := mustStageableBackup(t, dbPath, base)

	cmd, err := stageRestore(db, dbPath, source, base.Add(time.Hour))
	if err != nil {
		t.Fatalf("stage restore: %v", err)
	}

	// Enough routine snapshots to fill the routine quota several times over —
	// the exact behaviour that destroyed the pre-migration retreat before it
	// had its own pool.
	for i := 0; i < backupRetain*2; i++ {
		if _, err := runDatabaseBackup(db, dbPath, backupReasonManual, base.Add(time.Duration(2+i)*time.Hour)); err != nil {
			t.Fatalf("routine backup %d: %v", i, err)
		}
	}

	for _, name := range backupNamesIn(t, dbPath) {
		if name == cmd.PreRestore {
			return
		}
	}
	t.Fatalf("the pre-restore snapshot %q was rotated away by routine backups", cmd.PreRestore)
}

// ── Guard 2: a restore does not take effect in place ────────────────────────

// TestRestoreTakesEffectOnlyAtBoot is the honesty test behind the UI wording.
// The cockpit tells the owner the studio has to stop for a restore; if
// staging silently swapped the file, that wording would be a lie in the safe
// direction and the real behaviour would be a live database being replaced
// underneath open handles.
//
// MUTANT: make stageRestore rename the staged file over dbPath → the
// mid-flight assertion fails.
func TestRestoreTakesEffectOnlyAtBoot(t *testing.T) {
	db, dbPath := seedBackupFixture(t, 10)
	base := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	source := mustStageableBackup(t, dbPath, base)

	// Diverge the live database from the backup, so "was it replaced?" has an
	// observable answer rather than being a no-op comparison.
	if _, err := db.Exec(`INSERT INTO task (id, title) VALUES ('t-after', 'after the backup')`); err != nil {
		t.Fatalf("diverge live db: %v", err)
	}
	if _, err := stageRestore(db, dbPath, source, base.Add(time.Hour)); err != nil {
		t.Fatalf("stage restore: %v", err)
	}

	// MID-FLIGHT: the order is placed, and the live database still has the row
	// the backup never saw.
	var live int
	if err := db.QueryRow(`SELECT count(*) FROM task WHERE id = 't-after'`).Scan(&live); err != nil {
		t.Fatalf("read live db after staging: %v", err)
	}
	if live != 1 {
		t.Fatal("staging a restore already replaced the live database — the restart is the whole contract")
	}
	db.Close()

	// AT BOOT: nothing holds the file, and now the swap happens.
	applied, err := applyPendingRestore(dbPath, base.Add(2*time.Hour))
	if err != nil {
		t.Fatalf("apply restore: %v", err)
	}
	if applied == nil || applied.Source != source {
		t.Fatalf("apply reported %+v, want a restore of %s", applied, source)
	}
	after, err := openSQLite(dbPath)
	if err != nil {
		t.Fatalf("open restored db: %v", err)
	}
	defer after.Close()
	if err := after.QueryRow(`SELECT count(*) FROM task WHERE id = 't-after'`).Scan(&live); err != nil {
		t.Fatalf("read restored db: %v", err)
	}
	if live != 0 {
		t.Fatal("the boot-time swap did not put the backup back")
	}
}

// TestApplyPendingRestore_IsConsumedNotReplayed: the command file is a
// one-shot. A restore that reapplied on every boot would make the station
// unable to move forward at all.
func TestApplyPendingRestore_IsConsumedNotReplayed(t *testing.T) {
	db, dbPath := seedBackupFixture(t, 5)
	base := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	source := mustStageableBackup(t, dbPath, base)
	if _, err := stageRestore(db, dbPath, source, base.Add(time.Hour)); err != nil {
		t.Fatalf("stage restore: %v", err)
	}
	db.Close()

	if _, err := applyPendingRestore(dbPath, base.Add(2*time.Hour)); err != nil {
		t.Fatalf("first apply: %v", err)
	}
	again, err := applyPendingRestore(dbPath, base.Add(3*time.Hour))
	if err != nil {
		t.Fatalf("second apply reported an error: %v", err)
	}
	if again != nil {
		t.Fatal("the restore command replayed on the next boot")
	}
}

// ── Guard 3: the replaced database is moved aside, never deleted ────────────

// TestApplyPendingRestore_MovesTheReplacedDatabaseToTrash. The replaced file
// is the state the owner had one second before pressing the button. The
// pre-restore snapshot is the clean way back; this is the raw one, and the
// repo rule is that the server moves rather than deletes precisely so a bug
// in this path leaves the bytes findable.
//
// MUTANT: replace the moveIntoTrash loop with os.Remove → this test fails.
func TestApplyPendingRestore_MovesTheReplacedDatabaseToTrash(t *testing.T) {
	db, dbPath := seedBackupFixture(t, 5)
	base := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	source := mustStageableBackup(t, dbPath, base)
	if _, err := db.Exec(`INSERT INTO task (id, title) VALUES ('t-only-live', 'only in the live db')`); err != nil {
		t.Fatalf("diverge live db: %v", err)
	}
	if _, err := stageRestore(db, dbPath, source, base.Add(time.Hour)); err != nil {
		t.Fatalf("stage restore: %v", err)
	}
	db.Close()

	if _, err := applyPendingRestore(dbPath, base.Add(2*time.Hour)); err != nil {
		t.Fatalf("apply: %v", err)
	}

	entries, err := os.ReadDir(backupTrashFor(dbPath))
	if err != nil {
		t.Fatalf("read trash: %v", err)
	}
	var replaced string
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "replaced-") && strings.HasSuffix(e.Name(), filepath.Base(dbPath)) {
			replaced = filepath.Join(backupTrashFor(dbPath), e.Name())
		}
	}
	if replaced == "" {
		t.Fatalf("the replaced database is not in trash/ — entries: %v", entries)
	}
	// It must still be the DATA, not an empty placeholder.
	moved, err := openSQLite(replaced)
	if err != nil {
		t.Fatalf("open the moved-aside database: %v", err)
	}
	defer moved.Close()
	var n int
	if err := moved.QueryRow(`SELECT count(*) FROM task WHERE id = 't-only-live'`).Scan(&n); err != nil {
		t.Fatalf("read the moved-aside database: %v", err)
	}
	if n != 1 {
		t.Fatal("the moved-aside file is not the database that was replaced")
	}
}

// ── Guard 4: a restored station does not command the live world ────────────

// TestDisarmAfterRestore_WritesTheDurableStateAndDropsQueuedCommands.
//
// The subtle half is WHY the row is written rather than read: the restored
// file's own copy says "armed", because it was taken by an armed station.
//
// MUTANT: drop the PutSetting call → the setting stays absent and this fails.
func TestDisarmAfterRestore_WritesTheDurableStateAndDropsQueuedCommands(t *testing.T) {
	dal := newTestDAL(t)
	// The restored state's idea of the world: armed, with a machine command
	// still queued for a warden that may no longer exist.
	if err := dal.PutSetting(settingCommandDisarmed, "false"); err != nil {
		t.Fatalf("seed armed state: %v", err)
	}
	if err := dal.PutWardenCommand(WardenCommand{
		WardenID: "m-stale", Verb: "update", MemberID: "m-ghost",
		Frame: []byte(`{"verb":"update"}`), EnqueuedTS: 1,
	}); err != nil {
		t.Fatalf("seed queued command: %v", err)
	}

	dropped, err := disarmAfterRestore(dal, time.Unix(1000, 0))
	if err != nil {
		t.Fatalf("disarm: %v", err)
	}
	if dropped != 1 {
		t.Fatalf("dropped %d queued commands, want 1", dropped)
	}

	got, err := dal.GetSetting(settingCommandDisarmed)
	if err != nil || got == nil {
		t.Fatalf("disarm state not written (err=%v)", err)
	}
	if *got != "true" {
		t.Fatalf("disarm state = %q, want true", *got)
	}
	left, err := dal.ListWardenCommands()
	if err != nil {
		t.Fatalf("list warden commands: %v", err)
	}
	if len(left) != 0 {
		t.Fatalf("%d stale machine command(s) survived the restore", len(left))
	}
}

// TestDisarmedStationRefusesToCommand pins the choke points themselves: the
// durable state is only worth anything if the dispatch paths honour it.
//
// MUTANT: remove the commandingDisarmed check in enqueueToWarden (or in
// autoUpdateEnabled) → the matching sub-assertion fails.
func TestDisarmedStationRefusesToCommand(t *testing.T) {
	s := &apiServer{hub: NewHub()}
	s.updaterAutoUpdate = true

	// 🔴 The warden must be genuinely REACHABLE, or this test proves nothing:
	// enqueueToWarden already refuses an offline warden, so an offline fixture
	// makes the assertion below pass whether the disarm check is there or not.
	// (Found by mutant-verifying it: with the disarm check deleted the first
	// version of this test stayed green.)
	if _, err := s.hub.Connect("m-warden", "m-warden"); err != nil {
		t.Fatalf("connect the fixture warden: %v", err)
	}

	s.commandDisarmed = false
	if s.autoUpdateEnabled() != true {
		t.Fatal("an armed station with auto-update on must report it enabled — otherwise the disarmed assertion below proves nothing")
	}
	if !s.enqueueToWarden("m-someone", "m-warden", []byte(`{"verb":"stop"}`)) {
		t.Fatal("an ARMED station must dispatch to a reachable warden — otherwise the disarmed assertion below proves nothing")
	}

	s.commandDisarmed = true
	if s.enqueueToWarden("m-someone", "m-warden", []byte(`{"verb":"stop"}`)) {
		t.Fatal("a disarmed station dispatched a frame to a warden")
	}
	if s.autoUpdateEnabled() {
		t.Fatal("a disarmed station still arms its own unattended upgrade")
	}
	if _, _, timedOut := s.runWardenTeardownHere("/nonexistent/ocwarden"); timedOut {
		t.Fatal("teardown-here should be refused before it runs, not time out")
	}
	if _, err := s.runWardenInstallHere(Member{ID: "m-x"}, "/nonexistent/ocwarden", "http://127.0.0.1:1"); err == nil {
		t.Fatal("a disarmed station installed a warden on its own host")
	}
}

// ── The gate between a caller-supplied string and the filesystem ────────────

// TestValidBackupFileName_RefusesAnythingButOwnBackups. "Restore from this
// file" with a caller-composed path is how a restore endpoint turns into an
// arbitrary-file read and an arbitrary-file overwrite.
func TestValidBackupFileName_RefusesAnythingButOwnBackups(t *testing.T) {
	bad := []string{
		"",
		"../../etc/passwd",
		"/etc/passwd",
		"subdir/officraft-20260801-000000-manual.db",
		"officraft-20260801-000000-manual.db/../../x",
		"notabackup.db",
		"officraft-nope.db",
		"officraft-20260801-000000-manual.txt",
	}
	for _, name := range bad {
		if err := validBackupFileName(name); err == nil {
			t.Errorf("accepted %q as a backup file name", name)
		}
	}
	if err := validBackupFileName("officraft-20260801-000000-manual.db"); err != nil {
		t.Errorf("rejected a real backup name: %v", err)
	}
}

// TestStageRestore_RefusesACorruptSource: restoring a corrupt file replaces a
// working database with a broken one and reports success. It is refused at
// STAGING, while the live database is still untouched and nothing is ordered.
func TestStageRestore_RefusesACorruptSource(t *testing.T) {
	db, dbPath := seedBackupFixture(t, 5)
	base := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	source := mustStageableBackup(t, dbPath, base)

	// Corrupt the backup in place — the shape of a truncated copy.
	if err := os.WriteFile(filepath.Join(backupDirFor(dbPath), source), []byte("not a database"), 0o600); err != nil {
		t.Fatalf("corrupt the source: %v", err)
	}
	if _, err := stageRestore(db, dbPath, source, base.Add(time.Hour)); err == nil {
		t.Fatal("staged a restore from a corrupt backup")
	}
	if _, err := os.Stat(restoreCommandPath(dbPath)); err == nil {
		t.Fatal("a restore was ordered from a source that failed its integrity check")
	}
}

// TestApplyPendingRestore_WithoutAStagedFileChangesNothing: the unhappy path
// must leave the station exactly as it was — not half-swapped, and not
// retrying forever on every boot.
func TestApplyPendingRestore_WithoutAStagedFileChangesNothing(t *testing.T) {
	db, dbPath := seedBackupFixture(t, 5)
	base := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	source := mustStageableBackup(t, dbPath, base)
	if _, err := stageRestore(db, dbPath, source, base.Add(time.Hour)); err != nil {
		t.Fatalf("stage restore: %v", err)
	}
	db.Close()
	// Simulate the staged file going missing between the order and the boot.
	if err := os.Rename(restoreStagedPath(dbPath), restoreStagedPath(dbPath)+".gone"); err != nil {
		t.Fatalf("hide the staged file: %v", err)
	}

	applied, err := applyPendingRestore(dbPath, base.Add(2*time.Hour))
	if err == nil {
		t.Fatal("a restore with no staged file reported success")
	}
	if applied != nil {
		t.Fatal("a restore with no staged file reported that it applied one")
	}
	if _, err := os.Stat(dbPath); err != nil {
		t.Fatalf("the live database was moved aside despite the restore not happening: %v", err)
	}
	if _, err := os.Stat(restoreCommandPath(dbPath)); err == nil {
		t.Fatal("the unusable command file survived — it would retry on every boot")
	}
}
