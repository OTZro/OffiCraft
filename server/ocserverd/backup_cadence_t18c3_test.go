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
	"bytes"
	"log"
	"os"
	"path/filepath"
	"strings"
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

// TestRunDatabaseBackup_AnyRestorableSnapshotIsARetreatPoint is the THIRD
// newestBackupTime call site — the one this ticket deliberately did NOT align —
// asserted ON PURPOSE instead of by accident.
//
// 🔴 WHY THIS EXISTS SEPARATELY. Until now the only thing holding that site in
// place was TestRunDatabaseBackup_ReportsStaleness, and only by coincidence:
// every backup in that test happens to be `manual`, so narrowing this site to
// scheduled-only would have turned it red. But that test's stated purpose is
// the "never ran" alarm, so nobody reading it knows it is also carrying this
// second job — swap its fixture to `scheduled` and the guard disappears with no
// signal whatsoever.
//
// What is asserted here is the sentence runDatabaseBackup's own comment makes:
// "was there ANY restorable snapshot here when I arrived?" A pre-migration file
// from an hour ago IS one — it restores exactly as well as a scheduled one — so
// the run that walks in on it must NOT report a missing retreat point.
func TestRunDatabaseBackup_AnyRestorableSnapshotIsARetreatPoint(t *testing.T) {
	db, dbPath := seedBackupFixture(t, 8)
	now := time.Date(2026, 8, 5, 9, 0, 0, 0, time.UTC)

	// A pre-migration snapshot one hour ago and NOTHING scheduled — exactly the
	// shape the other two call sites (backupTick, backupHealthMonitor) must call
	// stale, and that this one must not.
	writeBackupFile(t, dbPath, now.Add(-time.Hour), backupReasonPreMigration)

	res, err := runDatabaseBackup(db, dbPath, backupReasonScheduled, now, backupRetainDefault)
	if err != nil {
		t.Fatalf("backup: %v", err)
	}
	if res.Stale {
		t.Errorf("a pre-migration snapshot one hour old was reported as no retreat point (StaleAge %q); "+
			"it restores exactly as well as a scheduled one, and this field answers "+
			"\"was there ANY restorable snapshot here\", not \"is the schedule alive\"", res.StaleAge)
	}

	// ── positive control ────────────────────────────────────────────────────
	// Without this, Stale==false is equally satisfied by a call that never looked
	// at the directory at all. The SAME reason, aged past the window, has to flip
	// it — so what was measured above really was that file.
	oldDB, oldPath := seedBackupFixture(t, 8)
	writeBackupFile(t, oldPath, now.Add(-backupStaleFactor*backupInterval-time.Hour), backupReasonPreMigration)

	stale, err := runDatabaseBackup(oldDB, oldPath, backupReasonScheduled, now, backupRetainDefault)
	if err != nil {
		t.Fatalf("backup (control): %v", err)
	}
	if !stale.Stale {
		t.Error("a directory whose newest snapshot is older than the whole alarm window reported a fresh retreat point")
	}
}

// TestLogBackupOutcome_TheStaleLineClaimsNothingAboutTheSchedule pins the
// wording this ticket deliberately changed.
//
// 🔴 WHY AN ASSERTION AND NOT JUST A NAMED CONSTANT. A constant only makes an
// edit visible in the diff; it does not make a wrong edit red, and its value is
// rewritten exactly as easily as the literal was. The property that actually
// matters is checkable: this line's Stale flag counts backups of EVERY reason
// (see runDatabaseBackup), so it cannot support a claim about the SCHEDULE
// specifically — a directory full of pre-migration snapshots satisfies it while
// the cadence is dead. That claim belongs to backupHealthMonitor. So this
// asserts the CLAIM, not the spelling: the line must be emitted, and it must
// not say "schedule".
func TestLogBackupOutcome_TheStaleLineClaimsNothingAboutTheSchedule(t *testing.T) {
	var buf bytes.Buffer
	prevOut, prevFlags := log.Writer(), log.Flags()
	log.SetOutput(&buf)
	log.SetFlags(0)
	t.Cleanup(func() { log.SetOutput(prevOut); log.SetFlags(prevFlags) })

	logBackupOutcome(backupResult{
		Reason:   backupReasonScheduled,
		Stale:    true,
		StaleAge: "30h0m0s",
		Skipped:  "only 1 MB free, want 200 MB (db is 100 MB)",
	}, nil)

	// Isolate the warning line. The other lines legitimately name the reason
	// ("scheduled"), which contains the very word under test — asserting over the
	// whole buffer would fail for a reason that has nothing to do with the claim.
	var warning string
	for _, line := range strings.Split(buf.String(), "\n") {
		if strings.Contains(line, "was stale (30h0m0s)") {
			warning = line
			break
		}
	}
	// Positive control FIRST: "says nothing about the schedule" is trivially
	// satisfied by a line that was never printed at all.
	if warning == "" {
		t.Fatalf("the stale warning was not emitted, so nothing below is under test; log was:\n%s", buf.String())
	}
	if strings.Contains(strings.ToLower(warning), "schedule") {
		t.Errorf("the stale warning makes a claim about the schedule:\n%s\n"+
			"Stale here counts backups of EVERY reason, so a directory full of pre-migration "+
			"snapshots satisfies it while the cadence is dead. Schedule liveness is "+
			"backupHealthMonitor's claim to make, not this line's.", warning)
	}
}

// ─── a backup stamped in the future ──────────────────────────────────────────
//
// 🔴 THE DEFECT THIS HALF EXISTS FOR. Every reader of this directory answers
// "how old is the newest backup?" by subtracting a filename from a clock, and
// `now.Sub(future)` is NEGATIVE. Negative passes BOTH thresholds in the quiet
// direction: `age < backupInterval` (the cadence's "already covered") is true
// for every negative value, and `age > backupStaleAfter()` (the alarm) is false
// for every negative value. So ONE file stamped in the future makes backupTick
// stop taking backups AND makes the monitor stay green — a total, silent backup
// outage with a healthy cockpit. That is an UNDER-report, strictly worse than
// the over-report the rest of this file is about, because nothing calls out at
// all. An NTP correction, a restored VM snapshot, or a dead RTC all produce it.
//
// 🔴 BOTH SIDES ARE TESTED, THROUGH ONE IMPLEMENTATION. This ticket's thesis is
// that "when did the schedule last run?" must not have two approximate answers,
// so the repair is a single filter inside newestScheduledBackup rather than a
// future-check bolted onto each consumer. Coverage is still per-side: the two
// tests below drive backupTick and the monitor SEPARATELY, so removing the
// filter turns both red and neither one alone is the whole guard.

// TestBackupTick_AFutureStampedBackupDoesNotStarveTheCadence is the cadence
// side: a backup must still be taken.
func TestBackupTick_AFutureStampedBackupDoesNotStarveTheCadence(t *testing.T) {
	db, dbPath := seedBackupFixture(t, 8)
	now := time.Date(2026, 8, 5, 9, 0, 0, 0, time.UTC)

	// The clock stepped back five hours: the last scheduled backup carries a
	// stamp the machine has not reached yet. Nothing else is in the directory,
	// so the cadence is starving if it counts this file.
	future := now.Add(5 * time.Hour)
	writeBackupFile(t, dbPath, future, backupReasonScheduled)

	if !backupTick(db, dbPath, now, nil) {
		t.Fatalf("the cadence took no backup: a scheduled file stamped %s in the future was "+
			"counted as %s of coverage, because now.Sub(future) is negative and every "+
			"\"< interval\" test passes for negative values. Backups stop and nothing says so.",
			future.Sub(now), now.Sub(future))
	}
	if want := backupFileName(now, backupReasonScheduled); !hasBackup(backupNamesIn(t, dbPath), want) {
		t.Errorf("tick reported it acted but %s is not on disk; directory holds %v",
			want, backupNamesIn(t, dbPath))
	}

	// ── the OTHER failure mode: it must not now back up every single tick ────
	// 🔴 This is why the repair SKIPS a future stamp instead of treating it as
	// infinitely old. "Infinitely old" would back up here too — and then again
	// at the next tick, and the next, for as long as the clock is behind, because
	// the bogus file is still the newest one. That fills the disk and rotates the
	// real history out of the routine pool inside one interval. Skipping
	// converges: the snapshot just written is stamped `now`, is not in the
	// future, and is the answer from here on. Cost of the whole incident: ONE
	// extra snapshot.
	if backupTick(db, dbPath, now.Add(15*time.Minute), nil) {
		t.Errorf("the cadence backed up AGAIN 15 minutes later; the future-stamped file is still "+
			"the newest name in the directory, so a repair that calls it \"very old\" loops "+
			"every tick until the clock catches up. Directory now holds %v", backupNamesIn(t, dbPath))
	}
}

// TestBackupHealth_AFutureStampedBackupIsNeverEvidenceOfALiveSchedule is the
// alarm side: the cockpit must not be green.
func TestBackupHealth_AFutureStampedBackupIsNeverEvidenceOfALiveSchedule(t *testing.T) {
	dbPath := tempDBPath(t)
	now := time.Date(2026, 8, 5, 9, 0, 0, 0, time.UTC)

	// The real schedule died 30 hours ago; a clock step left one file stamped
	// ahead of now. Counting it reports a cadence that is not running.
	writeBackupFile(t, dbPath, now.Add(-30*time.Hour), backupReasonScheduled)
	writeBackupFile(t, dbPath, now.Add(5*time.Hour), backupReasonScheduled)

	monitor := newBackupHealthMonitor(newFakeSettingStore(), dbPath)
	if _, err := monitor.evaluate(now); err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	if got := monitor.report(); got.Status != backupHealthUnhealthy || got.Code != backupHealthCodeStale {
		t.Fatalf("a schedule silent for 30h reported status=%q code=%q (detail %q); want %q/%q — "+
			"the only fresher-looking file is stamped in the FUTURE, and `age > staleAfter` is "+
			"false for every negative age, so counting it makes a dead cadence look green",
			got.Status, got.Code, got.Detail, backupHealthUnhealthy, backupHealthCodeStale)
	}

	// ── positive control ────────────────────────────────────────────────────
	// The same shape with a real recent backup must be green, or the assertion
	// above is satisfied by a monitor that alarms unconditionally.
	livePath := tempDBPath(t)
	writeBackupFile(t, livePath, now.Add(-30*time.Hour), backupReasonScheduled)
	writeBackupFile(t, livePath, now.Add(-time.Hour), backupReasonScheduled)
	live := newBackupHealthMonitor(newFakeSettingStore(), livePath)
	if _, err := live.evaluate(now); err != nil {
		t.Fatalf("evaluate (control): %v", err)
	}
	if lg := live.report(); lg.Status != backupHealthHealthy || lg.Code != "" {
		t.Fatalf("a live schedule reported status=%q code=%q (detail %q), want %q with no code",
			lg.Status, lg.Code, lg.Detail, backupHealthHealthy)
	}
}

// TestBackupHealth_ABaselineFromTheFutureStillReachesNeverRan is the FOURTH
// subtraction — the one that is NOT a filename.
//
// `never ran` is `now.Sub(baseline) > backupStaleAfter()`, and baseline is a
// durable row (backup.watchdog_baseline_ts) written once when the watchdog first
// armed. If the clock steps back AFTER that row is written, the subtraction goes
// negative and the never-ran alarm can never fire again on this installation —
// on a machine that, by construction of this branch, has no backup at all.
func TestBackupHealth_ABaselineFromTheFutureStillReachesNeverRan(t *testing.T) {
	dbPath := tempDBPath(t) // no backups at all: this is the never-ran branch
	now := time.Date(2026, 8, 5, 9, 0, 0, 0, time.UTC)

	monitor := newBackupHealthMonitor(newFakeSettingStore(), dbPath)
	// Arm the baseline while the clock is 40 hours ahead...
	if _, err := monitor.evaluate(now.Add(40 * time.Hour)); err != nil {
		t.Fatalf("arm baseline: %v", err)
	}
	// ...then the correction lands and the clock steps back to `now`.
	if _, err := monitor.evaluate(now); err != nil {
		t.Fatalf("evaluate after the step back: %v", err)
	}
	// Nothing can alarm yet — the re-armed grace window has just started. That
	// is the honest answer, and it is `unknown`, never green.
	if got := monitor.report(); got.Status == backupHealthHealthy {
		t.Fatalf("a studio with no backup at all reported %q", got.Status)
	}

	// 🔴 `later` is chosen to separate the two worlds: it is past the window
	// measured from the RE-ARMED baseline (20h > 12h), and still BEFORE the
	// window measured from the bogus future one (now+40h), so a baseline left in
	// the future is stuck saying nothing.
	later := now.Add(20 * time.Hour)
	if later.Sub(now) <= backupStaleAfter() || later.After(now.Add(40*time.Hour)) {
		t.Fatalf("fixture no longer separates the two baselines (window %s)", backupStaleAfter())
	}
	if _, err := monitor.evaluate(later); err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	if got := monitor.report(); got.Code != backupHealthCodeNeverRan {
		t.Fatalf("no backup has ever landed and the watch window is over, but the verdict is "+
			"status=%q code=%q (detail %q); want %q. A baseline left in the future makes "+
			"now.Sub(baseline) negative forever, and `> staleAfter` is false for every "+
			"negative value — so this alarm could never fire again.",
			got.Status, got.Code, got.Detail, backupHealthCodeNeverRan)
	}
}

// TestRunDatabaseBackup_AFutureStampIsNotARecentRetreatPoint is the THIRD call
// site. Its population is deliberately untouched (newestBackupTime, every
// reason), but the same negative age fools it: it would report "there was a
// recent retreat point" about a directory whose newest name is fiction. This
// field gates nothing — it feeds one log line — so it fails quietly rather than
// starving anything, which is exactly why it needed asserting.
func TestRunDatabaseBackup_AFutureStampIsNotARecentRetreatPoint(t *testing.T) {
	db, dbPath := seedBackupFixture(t, 8)
	now := time.Date(2026, 8, 5, 9, 0, 0, 0, time.UTC)

	writeBackupFile(t, dbPath, now.Add(5*time.Hour), backupReasonPreMigration)

	res, err := runDatabaseBackup(db, dbPath, backupReasonScheduled, now, backupRetainDefault)
	if err != nil {
		t.Fatalf("backup: %v", err)
	}
	if !res.Stale {
		t.Errorf("a directory whose newest file is stamped 5h in the FUTURE was reported as "+
			"holding a recent retreat point (Stale=%v, StaleAge %q); `age > window` is false "+
			"for every negative age, and this field's entire content is recency",
			res.Stale, res.StaleAge)
	}
	// It is a distinct fact, so it gets a distinct reason: there IS a file (so
	// not "no previous backup") and its age is not a duration.
	if res.StaleAge == "no previous backup" {
		t.Errorf("a future-stamped file was reported as %q; a file that exists and may well "+
			"restore is not an empty directory", res.StaleAge)
	}
}
