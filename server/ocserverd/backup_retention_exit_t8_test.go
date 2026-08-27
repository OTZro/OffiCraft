package main

// backup_retention_exit_t8_test.go — T-8: the EXIT from backup retention.
//
// 🔴 THE DEFECT THIS FILE EXISTS FOR. Backups had an entrance and no exit.
// rotateBackups MOVED every evicted file into `trash/`, and `backupTrashFor` had
// exactly one non-test caller — that move. Nothing read the directory, nothing
// walked it, nothing deleted from it. The comment in backup.go said "reclamation
// is the warden's job"; the warden's reaper (cli/ocwarden/trash.go, purgeTrash)
// refuses any path that is not a direct child of the agents root, which the
// server's data directory is not, so it was never asked and would have refused.
// By 2026-08-27 `server/data/trash/` held 278 files / 141.6 GiB, growing ~6.9 GB
// a day. That is a responsibility gap, not a broken component: nothing anywhere
// asserted the hand-off, so nothing could notice it had never been accepted.
//
// 🔴 THE TWO GATES, AND WHY BOTH. Retention is deletion, so it has exactly two
// ways to be wrong and they point in opposite directions:
//
//	TestRetention_EvictedBackupsAreGoneFromDisk
//	    the files past N are REALLY GONE — not moved to a third directory.
//	    Red against a rotation that retires by relocating.
//	TestRetention_HonoursTheConfiguredNAndKeepsTheNewest
//	    the newest N — the CONFIGURED N, not a constant — are REALLY STILL THERE.
//	    Red against a rotation that ignores `backup.retain`, and red against one
//	    that deletes too much.
//
// A implementation that passes only the first is a shredder. One that passes
// only the second is what this ticket is repairing. Neither gate is implied by
// the other and neither is allowed to stand alone.
//
// 🔴 WHAT "GONE" IS MEASURED AS. findUnderDataRoot (backup_test.go) walks the
// ENTIRE directory the database lives in. Checking `backups/` alone would go
// green for a rotation that moved files into `trash/`; checking `backups/` and
// `trash/` would go green for one that invented a third directory. The only
// check whose message ("it is not on disk") cannot be made false by a plausible
// stand-in is one that looks everywhere the engine could have put it.
//
// 🔴 AND IT IS MEASURED AT THE DELETER, NOT ONLY AT THE COMPOSITE. This was
// found by running the mutant rather than by reasoning: the first cut of gate ①
// drove runDatabaseBackup, and a rotation mutated back to MOVING files into
// trash/ still passed it — because reapBackupTrash runs immediately afterwards
// in the same call and drained what the move had just parked there. End state
// identical, contract violated, gate silent. So gate ① now asserts
// rotateBackups DIRECTLY as well: that function's contract is "the bytes are
// gone when I return", and only a check placed on the function that owes the
// contract can catch a version of it that does not keep it.
//
// 🔴 SAFETY. Every path here is rooted at t.TempDir() and derived from the
// database path handed to the engine. The real server root is never named and
// is unreachable by construction, so even a mutant that disables the guard under
// test cannot reach production backups. This matters more here than in any other
// backup test file: this is the one that drives a DELETER.

import (
	"database/sql"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

// armBackupRetain writes the `backup.retain` settings row into a fixture
// database, creating the table with the DDL of migration 00002_settings.sql.
//
// ⚠️ The DDL is a COPY, and a copy can drift from the schema the server actually
// migrates to. TestLiveBackupRetain_ReadsTheRealSettingTable below is what pins
// it: it runs the REAL migrations and reads back through the same function, so a
// schema change that made this helper a fiction turns that test red instead of
// letting every test in this file quietly fall back to the default.
func armBackupRetain(t *testing.T, db *sql.DB, n int) {
	t.Helper()
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS setting (
		key        TEXT PRIMARY KEY,
		value      TEXT NOT NULL,
		updated_at REAL NOT NULL DEFAULT 0.0
	)`); err != nil {
		t.Fatalf("create setting table: %v", err)
	}
	if _, err := db.Exec(
		`INSERT INTO setting (key, value, updated_at) VALUES (?, ?, 0.0)
		 ON CONFLICT (key) DO UPDATE SET value = excluded.value`,
		settingBackupRetain, strconv.Itoa(n)); err != nil {
		t.Fatalf("write %s=%d: %v", settingBackupRetain, n, err)
	}
	// Positive control, stated as its own failure: if the row cannot be read
	// back through the engine's own reader, every retention assertion below
	// would silently be measuring backupRetainDefault instead of n — the exact
	// shape of a test that goes green while proving nothing.
	if got := liveBackupRetain(db); got != n {
		t.Fatalf("liveBackupRetain read %d after %s was set to %d — the engine is not reading the row this test wrote, so nothing below would be testing the setting",
			got, settingBackupRetain, n)
	}
}

// ── GATE ①: what is past N is GONE ──────────────────────────────────────────

// TestRetention_EvictedBackupsAreGoneFromDisk is the ticket's headline: rotation
// removes the bytes.
//
// It drives BOTH pools in one fixture, because they are two independent quotas
// and a fix applied to one loop and not the other would otherwise pass.
//
// 🔴 The negative half of the pair is asserted too — the newest file is STILL
// on disk. Without it, "nothing matched anywhere under the data root" is
// satisfied by a rotation that deleted the whole directory, which is a way of
// passing this gate that would destroy the studio.
func TestRetention_EvictedBackupsAreGoneFromDisk(t *testing.T) {
	t.Run("rotateBackups itself leaves nothing on disk", rotateDeletesInPlace)

	db, dbPath := seedBackupFixture(t, 10)
	const keep = 2
	base := time.Date(2026, 8, 27, 0, 0, 0, 0, time.UTC)

	var routine, premigration []string
	for i := 0; i < keep+3; i++ {
		at := base.Add(time.Duration(i) * time.Hour)
		res, err := runDatabaseBackup(db, dbPath, backupReasonScheduled, at, keep)
		if err != nil {
			t.Fatalf("scheduled backup %d: %v", i, err)
		}
		routine = append(routine, filepath.Base(res.Path))

		res, err = runDatabaseBackup(db, dbPath, backupReasonPreMigration, at.Add(time.Minute), keep)
		if err != nil {
			t.Fatalf("pre-migration backup %d: %v", i, err)
		}
		premigration = append(premigration, filepath.Base(res.Path))
	}

	for _, pool := range []struct {
		name  string
		files []string
	}{{"routine", routine}, {"premigration", premigration}} {
		evicted := pool.files[:len(pool.files)-keep]
		survivors := pool.files[len(pool.files)-keep:]

		// Positive control FIRST: this fixture only proves something if the
		// quota actually overflowed.
		if len(evicted) == 0 {
			t.Fatalf("%s pool: the fixture evicted nothing, so it proves nothing about deletion", pool.name)
		}
		for _, name := range evicted {
			if where := findUnderDataRoot(t, dbPath, name); where != "" {
				t.Errorf("%s pool: %s is past the retention limit of %d but is STILL ON DISK at %s — retention must DELETE the bytes, not relocate them",
					pool.name, name, keep, where)
			}
		}
		for _, name := range survivors {
			if where := findUnderDataRoot(t, dbPath, name); where == "" {
				t.Errorf("%s pool: %s is one of the newest %d and is GONE — a retention that deletes everything is not a retention",
					pool.name, name, keep)
			}
		}
	}
}

// rotateDeletesInPlace pins the contract ON THE DELETER: when rotateBackups
// returns, the files it retired are gone from the disk, full stop.
//
// 🔴 It calls rotateBackups directly and NOTHING ELSE, which is the entire point.
// Driving runDatabaseBackup instead lets reapBackupTrash — which runs in the
// same call — clean up after a rotation that merely relocated, so the composite
// cannot tell "deleted" from "moved somewhere that is drained a microsecond
// later". Two functions, two contracts; a rotation that hands its evictions to
// someone else to destroy has not kept its own.
func rotateDeletesInPlace(t *testing.T) {
	db, dbPath := seedBackupFixture(t, 5)
	base := time.Date(2026, 8, 27, 0, 0, 0, 0, time.UTC)

	// Written WITHOUT rotation, so this test owns the whole population: each
	// snapshot is taken with a retain high enough that nothing is evicted yet.
	var made []string
	for i := 0; i < 4; i++ {
		res, err := runDatabaseBackup(db, dbPath, backupReasonScheduled, base.Add(time.Duration(i)*time.Hour), maxBackupRetain)
		if err != nil {
			t.Fatalf("seed backup %d: %v", i, err)
		}
		made = append(made, filepath.Base(res.Path))
	}
	if got := len(namesIn(t, backupDirFor(dbPath))); got != len(made) {
		t.Fatalf("fixture holds %d backups before rotation, want %d — nothing was rotated yet, so this test has not started from a known state", got, len(made))
	}

	deleted, err := rotateBackups(dbPath, 2)
	if err != nil {
		t.Fatalf("rotateBackups: %v", err)
	}
	if len(deleted) != 2 {
		t.Fatalf("rotateBackups reported retiring %d files, want 2 — the fixture did not overflow, so nothing below proves anything", len(deleted))
	}
	for _, name := range deleted {
		if where := findUnderDataRoot(t, dbPath, name); where != "" {
			t.Errorf("rotateBackups reported retiring %s, but it is STILL ON DISK at %s — retirement must DELETE the bytes; handing them to another directory is not deletion",
				name, where)
		}
	}
	// The complement, so "deleted everything" cannot pass as "deleted the right
	// two".
	for _, name := range made[len(made)-2:] {
		if where := findUnderDataRoot(t, dbPath, name); where == "" {
			t.Errorf("rotateBackups deleted %s, which is one of the newest 2 it was told to keep", name)
		}
	}
}

// TestRetention_DrainsTheTrashBacklogItsPredecessorLeft is the other half of
// "the bytes are gone": switching rotation to delete stops the growth, but the
// 141.6 GiB that the old move-based rotation already parked in `trash/` would
// sit there forever, because nothing on this machine reads that directory.
//
// 🔴 The reaper's reach is asserted, not assumed. The fixture plants four things
// beside the retired backups that a careless drain would take with it: a
// hand-made snapshot, a `.partial`, an unrelated file and a subdirectory. Each
// is named in its own failure line, so a widened reach says WHICH widening.
func TestRetention_DrainsTheTrashBacklogItsPredecessorLeft(t *testing.T) {
	db, dbPath := seedBackupFixture(t, 5)
	trash := backupTrashFor(dbPath)
	if err := os.MkdirAll(trash, 0o700); err != nil {
		t.Fatalf("mkdir trash: %v", err)
	}

	// Exactly the shape the old rotation left behind: this engine's own files,
	// under their own names, in trash/.
	backlog := []string{
		"officraft-20260801-040000-scheduled.db",
		"officraft-20260802-040000-premigration.db",
		"officraft-20260803-040000-manual.db",
	}
	for _, name := range backlog {
		if err := os.WriteFile(filepath.Join(trash, name), []byte("retired backup"), 0o600); err != nil {
			t.Fatalf("plant %s: %v", name, err)
		}
	}
	bystanders := []string{
		"officraft.db.bak-pre-v0.5.39",                      // hand-made, wrong prefix
		"officraft-20260804-040000-premigration.db.partial", // killed VACUUM, wrong suffix
		"notes.md", // somebody else's
	}
	for _, name := range bystanders {
		if err := os.WriteFile(filepath.Join(trash, name), []byte("not rotation's"), 0o600); err != nil {
			t.Fatalf("plant bystander %s: %v", name, err)
		}
	}
	if err := os.MkdirAll(filepath.Join(trash, "T-7526"), 0o700); err != nil {
		t.Fatalf("mkdir trash subdir: %v", err)
	}

	res, err := runDatabaseBackup(db, dbPath, backupReasonManual, time.Date(2026, 8, 27, 12, 0, 0, 0, time.UTC), backupRetainDefault)
	if err != nil {
		t.Fatalf("backup: %v", err)
	}
	if res.Reaped != len(backlog) {
		t.Errorf("the run reported reaping %d trash files, want %d", res.Reaped, len(backlog))
	}
	for _, name := range backlog {
		if where := findUnderDataRoot(t, dbPath, name); where != "" {
			t.Errorf("the legacy trash backlog was NOT reclaimed: %s is still at %s — this is the 141.6 GiB the ticket exists to release", name, where)
		}
	}
	for _, name := range bystanders {
		if _, err := os.Stat(filepath.Join(trash, name)); err != nil {
			t.Errorf("the trash drain removed %s, which this engine never created: %v", name, err)
		}
	}
	if _, err := os.Stat(filepath.Join(trash, "T-7526")); err != nil {
		t.Errorf("the trash drain removed a subdirectory: %v", err)
	}
	if _, err := os.Stat(trash); err != nil {
		t.Errorf("the trash drain removed the trash directory itself: %v", err)
	}
}

// ── GATE ②: the newest N — the CONFIGURED N — are still there ───────────────

// TestRetention_HonoursTheConfiguredNAndKeepsTheNewest is the "N is settable"
// half of the ticket, and it is deliberately asserted through the FULL path a
// cockpit PATCH takes: the value goes into the `backup.retain` settings row, and
// the engine is driven by its own triggers rather than by a number this test
// hands it.
//
// 🔴 The configured N (8) is NOT backupRetainDefault (5), and that gap is the
// whole discriminating power of this test. A rotation that reads N from a
// hard-wired constant keeps 5, so the 6th, 7th and 8th newest files — which this
// test names one by one — go missing, and the failure says exactly that.
//
// 🔴 It also asserts the OVER-eviction direction (nothing older than N survived),
// so an implementation that "fixes" this by keeping everything is red too.
func TestRetention_HonoursTheConfiguredNAndKeepsTheNewest(t *testing.T) {
	db, dbPath := seedBackupFixture(t, 10)
	const configured = 8
	if configured == backupRetainDefault {
		t.Fatalf("this test is worthless unless the configured N differs from the shipped default (%d)", backupRetainDefault)
	}
	armBackupRetain(t, db, configured)

	base := time.Date(2026, 8, 27, 0, 0, 0, 0, time.UTC)
	var made []string
	for i := 0; i < configured+3; i++ {
		// backupTick is used on purpose: it is the trigger that runs unattended,
		// and it reads N the same way every other trigger does. A test that
		// called rotateBackups directly would prove the sorter works and say
		// nothing about whether the setting reaches it.
		if !backupTick(db, dbPath, base.Add(time.Duration(i)*backupInterval), nil) {
			t.Fatalf("tick %d took no backup", i)
		}
		files, err := backupFilesIn(backupDirFor(dbPath))
		if err != nil {
			t.Fatalf("list after tick %d: %v", i, err)
		}
		made = append(made, files[0].Name())
	}

	survivors := made[len(made)-configured:]
	evicted := made[:len(made)-configured]

	for _, name := range survivors {
		if where := findUnderDataRoot(t, dbPath, name); where == "" {
			t.Errorf("backup.retain is %d, but %s — one of the newest %d — was DELETED; retention is not reading the setting",
				configured, name, configured)
		}
	}
	for _, name := range evicted {
		if where := findUnderDataRoot(t, dbPath, name); where != "" {
			t.Errorf("backup.retain is %d, but %s is older than that and is still on disk at %s",
				configured, name, where)
		}
	}
	// And the whole picture: exactly N files, no more.
	kept, err := backupFilesIn(backupDirFor(dbPath))
	if err != nil {
		t.Fatalf("list backups: %v", err)
	}
	if len(kept) != configured {
		t.Errorf("backups/ holds %d files, want the configured %d", len(kept), configured)
	}
	// A survivor must still be restorable — keeping the right names while
	// corrupting the bytes passes everything above.
	if _, rows := readBackSentinel(t, filepath.Join(backupDirFor(dbPath), survivors[len(survivors)-1])); rows == 0 {
		t.Errorf("the newest surviving backup %s carries no rows", survivors[len(survivors)-1])
	}
}

// TestLiveBackupRetain_ReadsTheRealSettingTable pins armBackupRetain's copied
// DDL to the schema the server actually migrates to. Without it, a rename in
// migration 00002 would make every fixture in this file fall back to
// backupRetainDefault — and the gate above, which asserts a value that is NOT
// the default, is the only reason that would be noticed at all. This test makes
// it noticed HERE, with a message that names the cause.
func TestLiveBackupRetain_ReadsTheRealSettingTable(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "server", "data", "officraft.db")
	db, err := openSQLite(dbPath)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()
	if err := runMigrations(db); err != nil {
		t.Fatalf("goose up: %v", err)
	}

	// Absent row = the shipped default. This is the upgrade case: an install
	// that has never touched the knob must behave exactly as it did before the
	// knob existed.
	if got := liveBackupRetain(db); got != backupRetainDefault {
		t.Errorf("with no %s row, liveBackupRetain = %d, want the shipped default %d", settingBackupRetain, got, backupRetainDefault)
	}

	dal := NewDAL(db)
	if err := dal.PutSetting(settingBackupRetain, "7"); err != nil {
		t.Fatalf("PutSetting: %v", err)
	}
	if got := liveBackupRetain(db); got != 7 {
		t.Errorf("after the settings store wrote %s=7, liveBackupRetain = %d — the cockpit's write and the engine's read are not the same row", settingBackupRetain, got)
	}

	// Out of range in either direction falls back rather than becoming a
	// deletion instruction. (serve refuses to boot on such a row —
	// loadAuthSettings — so this path is reachable only from the CLI triggers.)
	for _, bad := range []string{"0", "999", "", "five"} {
		if err := dal.PutSetting(settingBackupRetain, bad); err != nil {
			t.Fatalf("PutSetting %q: %v", bad, err)
		}
		if got := liveBackupRetain(db); got != backupRetainDefault {
			t.Errorf("%s=%q gave retention %d; an unusable row must fall back to %d, never widen this engine's reach", settingBackupRetain, bad, got, backupRetainDefault)
		}
	}
}
