package main

// backup_cadence_t18c3_test.go — the two halves of T-18c3, deliberately pointed
// in OPPOSITE directions.
//
// 🔴 THE DEFECT. "When was the last backup?" was answered from two different
// populations. backupTick asked newestBackupTime (every file in the directory,
// whatever its reason), while the staleness alarm asked newestScheduledBackup
// (scheduled only). So one pre-migration snapshot — one `ocserverd` upgrade —
// pushed the next scheduled backup out by a full backupInterval, and a run of
// upgrades pushed it past the alarm window. Observed on this machine three
// times in three days, each next-scheduled landing ONE SECOND after the
// displacing pre-migration snapshot aged out:
//
//	20260802-103012-scheduled → 20260803-004908-scheduled  (14h58m)
//	20260803-075043-scheduled → 20260803-222534-scheduled  (14h54m)
//	20260804-042534-scheduled → 20260804-183057-scheduled  (14h05m)
//
// Nothing had ever driven backupTick with a pre-migration file present, which
// is why a defect this mechanical survived in a module that is otherwise
// heavily tested.
//
// 🔴 WHY TWO TESTS, AND WHY THEY MUST DISAGREE. The cheap repair for a false
// alarm is to widen the alarm, and the cheap repair for a noisy guard is to
// delete it — both leave a studio that reports "healthy" while it has no
// retreat point at all, which is the exact failure T-ada9 and T-da06 exist to
// prevent. So:
//
//	TestBackupTick_APreMigrationSnapshotIsNotRoutineCoverage
//	    goes red if the alignment is removed.
//	TestBackupCadence_AStoppedScheduleStillAlarms
//	    goes red if the alarm is muted while fixing the false one.
//
// Each asserts a COMPUTED outcome — a file that did or did not appear, a health
// verdict that was actually derived — never that some constant is referenced.
// Each carries its own positive control, because "no backup was taken" and "no
// alarm fired" are both satisfied by a test that never reached the code.
//
// 🔴 SAFETY. Every path here is rooted at t.TempDir(); the real server root is
// never named and is unreachable by construction, so a mutant that disables the
// very guard under test still cannot touch production backups or trash.

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// backupNamesIn lists this engine's files in a directory, newest first, by name.
func backupNamesIn(t *testing.T, dbPath string) []string {
	t.Helper()
	files, err := backupFilesIn(backupDirFor(dbPath))
	if err != nil && !os.IsNotExist(err) {
		t.Fatalf("list backups: %v", err)
	}
	var names []string
	for _, f := range files {
		names = append(names, f.Name())
	}
	return names
}

func hasBackup(names []string, want string) bool {
	for _, n := range names {
		if n == want {
			return true
		}
	}
	return false
}

// TestBackupTick_APreMigrationSnapshotIsNotRoutineCoverage is the "remove the
// alignment and this goes red" half.
//
// The fixture is the real incident, to the second: a scheduled backup at
// 04:25:34, a pre-migration snapshot at 12:30:56, and a tick at 18:30:00 —
// chosen so the pre-migration file is 5h59m04s old (INSIDE backupInterval, so
// the old population says "not due") while the schedule has been silent for
// 14h04m26s (more than two intervals). The assertion is the file that does or
// does not appear on disk.
func TestBackupTick_APreMigrationSnapshotIsNotRoutineCoverage(t *testing.T) {
	db, dbPath := seedBackupFixture(t, 8)

	lastScheduled := time.Date(2026, 8, 4, 4, 25, 34, 0, time.UTC)
	preMigration := time.Date(2026, 8, 4, 12, 30, 56, 0, time.UTC)
	now := time.Date(2026, 8, 4, 18, 30, 0, 0, time.UTC)

	// The fixture must actually be the awkward shape, or this proves nothing:
	// the pre-migration file has to be too YOUNG to be due on its own.
	if age := now.Sub(preMigration); age >= backupInterval {
		t.Fatalf("fixture is not the defect shape: pre-migration snapshot is %s old, "+
			"which is already due regardless of reason (interval %s)", age, backupInterval)
	}
	if age := now.Sub(lastScheduled); age <= backupInterval {
		t.Fatalf("fixture is not the defect shape: the schedule is only %s behind (interval %s)", age, backupInterval)
	}

	writeBackupFile(t, dbPath, lastScheduled, backupReasonScheduled)
	writeBackupFile(t, dbPath, preMigration, backupReasonPreMigration)

	if !backupTick(db, dbPath, now, nil) {
		t.Fatalf("the cadence took no backup: the schedule had been silent for %s, "+
			"but a pre-migration snapshot %s old was counted as routine coverage — "+
			"a pool that rotateBackups deliberately keeps SEPARATE from routine files",
			now.Sub(lastScheduled).Round(time.Minute), now.Sub(preMigration).Round(time.Minute))
	}

	names := backupNamesIn(t, dbPath)
	want := backupFileName(now, backupReasonScheduled)
	if !hasBackup(names, want) {
		t.Errorf("tick reported it acted but %s is not on disk; directory holds %v", want, names)
	}
	// The pre-migration snapshot is the only retreat from a bad migration. The
	// repair must not have made it collateral.
	if pre := backupFileName(preMigration, backupReasonPreMigration); !hasBackup(names, pre) {
		t.Errorf("the pre-migration snapshot %s is gone; directory holds %v", pre, names)
	}

	// ── positive control ────────────────────────────────────────────────────
	// Without this, "a backup appeared" is also satisfied by a tick that backs
	// up unconditionally — which would fill the disk and rotate the useful
	// history away within one interval.
	_, freshPath := seedBackupFixture(t, 8)
	recentScheduled := now.Add(-time.Hour)
	writeBackupFile(t, freshPath, recentScheduled, backupReasonScheduled)
	writeBackupFile(t, freshPath, preMigration, backupReasonPreMigration)

	if backupTick(db, freshPath, now, nil) {
		t.Error("the cadence backed up while a scheduled backup from one hour ago was already covering it")
	}
	if got, want := len(backupNamesIn(t, freshPath)), 2; got != want {
		t.Errorf("directory holds %d backups after a tick that was not due, want %d: %v",
			got, want, backupNamesIn(t, freshPath))
	}
}

// TestBackupCadence_AStoppedScheduleStillAlarms is the other direction: the
// guard against curing a false alarm by silencing the true one.
//
// Both arms are built so that a directory BUSY with pre-migration snapshots is
// exactly what a human would be looking at while the schedule is dead — the
// shape under which muting is most tempting and most damaging.
func TestBackupCadence_AStoppedScheduleStillAlarms(t *testing.T) {
	t.Run("a schedule that stopped is stale no matter how busy the directory looks", func(t *testing.T) {
		dbPath := tempDBPath(t)
		now := time.Date(2026, 8, 5, 9, 0, 0, 0, time.UTC)

		// Silent for well over the alarm window...
		writeBackupFile(t, dbPath, now.Add(-30*time.Hour), backupReasonScheduled)
		// ...while upgrades kept the directory looking alive.
		writeBackupFile(t, dbPath, now.Add(-3*time.Hour), backupReasonPreMigration)
		writeBackupFile(t, dbPath, now.Add(-time.Hour), backupReasonPreMigration)
		writeBackupFile(t, dbPath, now.Add(-20*time.Minute), backupReasonManual)

		monitor := newBackupHealthMonitor(newFakeSettingStore(), dbPath)
		if _, err := monitor.evaluate(now); err != nil {
			t.Fatalf("evaluate: %v", err)
		}
		got := monitor.report()
		if got.Status != backupHealthUnhealthy || got.Code != backupHealthCodeStale {
			t.Fatalf("a schedule silent for 30h reported status=%q code=%q (detail %q); "+
				"want %q/%q — pre-migration and manual snapshots are not evidence the cadence is alive",
				got.Status, got.Code, got.Detail, backupHealthUnhealthy, backupHealthCodeStale)
		}

		// ── positive control ────────────────────────────────────────────────
		// A directory of the same shape whose SCHEDULE is alive must be green,
		// or the assertion above is satisfied by a monitor that alarms always.
		livePath := tempDBPath(t)
		writeBackupFile(t, livePath, now.Add(-time.Hour), backupReasonScheduled)
		writeBackupFile(t, livePath, now.Add(-3*time.Hour), backupReasonPreMigration)
		live := newBackupHealthMonitor(newFakeSettingStore(), livePath)
		if _, err := live.evaluate(now); err != nil {
			t.Fatalf("evaluate (control): %v", err)
		}
		if lg := live.report(); lg.Status != backupHealthHealthy || lg.Code != "" {
			t.Fatalf("a live schedule reported status=%q code=%q (detail %q), want %q with no code",
				lg.Status, lg.Code, lg.Detail, backupHealthHealthy)
		}
	})

	t.Run("a cadence that cannot write is loud immediately and stays loud through an upgrade", func(t *testing.T) {
		db, dbPath := seedBackupFixture(t, 8)
		now := time.Date(2026, 8, 5, 9, 0, 0, 0, time.UTC)

		// The schedule is due and the engine cannot produce anything. Closing
		// the handle makes VACUUM INTO fail for real, inside the temp dir, with
		// nothing stubbed out.
		writeBackupFile(t, dbPath, now.Add(-8*time.Hour), backupReasonScheduled)
		if err := db.Close(); err != nil {
			t.Fatalf("close fixture db: %v", err)
		}

		store := newFakeSettingStore()
		monitor := newBackupHealthMonitor(store, dbPath)
		if _, err := monitor.evaluate(now); err != nil {
			t.Fatalf("evaluate: %v", err)
		}

		if backupTick(db, dbPath, now, monitor) {
			t.Fatal("backupTick reported success against a database it cannot read")
		}
		if got := monitor.report(); got.Status != backupHealthUnhealthy || got.Code != backupHealthCodeFailed {
			t.Fatalf("a scheduled backup that failed reported status=%q code=%q (detail %q); "+
				"want %q/%q — a failure has to be visible NOW, not one stale window later",
				got.Status, got.Code, got.Detail, backupHealthUnhealthy, backupHealthCodeFailed)
		}
		// Nothing was produced, so nothing may look like a retreat point.
		for _, n := range backupNamesIn(t, dbPath) {
			if n != backupFileName(now.Add(-8*time.Hour), backupReasonScheduled) {
				t.Errorf("a failed backup left %s behind in the directory", n)
			}
		}
		if _, err := os.Stat(filepath.Join(backupDirFor(dbPath), backupFileName(now, backupReasonScheduled)+".partial")); err == nil {
			t.Error("a failed backup left a .partial file that a reader could mistake for a snapshot")
		}

		// 🔴 The muting shape, made concrete: an upgrade lands a pre-migration
		// snapshot minutes later. If that counted, the studio would go green
		// while the cadence is still unable to write anything at all.
		writeBackupFile(t, dbPath, now.Add(10*time.Minute), backupReasonPreMigration)
		later := now.Add(20 * time.Minute)
		if _, err := monitor.evaluate(later); err != nil {
			t.Fatalf("evaluate after the upgrade: %v", err)
		}
		if got := monitor.report(); got.Status != backupHealthUnhealthy || got.Code != backupHealthCodeFailed {
			t.Fatalf("a pre-migration snapshot cleared the failure: status=%q code=%q (detail %q), want %q/%q",
				got.Status, got.Code, got.Detail, backupHealthUnhealthy, backupHealthCodeFailed)
		}
	})
}
