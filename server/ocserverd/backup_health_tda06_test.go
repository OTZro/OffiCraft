package main

// backup_health_tda06_test.go — the guards for "a dead backup must not look
// like a healthy one" (T-da06).
//
// 🔴 WHAT THIS FILE IS DEFENDING. The backup engine has always reported every
// outcome — into a log file that nothing on this machine aggregates and the
// cockpit cannot read. So the studio could go three days without a retreat
// point and every surface a human looks at would say nothing at all. The
// defect class is SILENCE, which means the tests have to be written the
// opposite way round from usual: it is not enough to check that the healthy
// path is healthy (silence passes that), each alarm has to be MANUFACTURED and
// then observed on the surface a human reads.
//
// 🔴 THE BRANCH THAT HAD NEVER BEEN VERIFIED. Staleness had exactly one
// exercised path before this ticket: "there is no previous backup at all",
// which is the unavoidable state on first run. The branch that actually
// matters — a previous backup EXISTS but is too old, i.e. the schedule died
// after working for a while — had never been executed by anything. It is
// covered here (TestStale_APreviousBackupThatIsTooOld…) and it is the reason
// the decision was extracted as a pure function: waiting 12 hours is not a
// test strategy.
//
// 🔴 SAFETY. Nothing in this file may depend on the guard it is testing to stay
// harmless. Manufacturing a FAILING backup is the dangerous act, so every test
// runs against a `t.TempDir()` database and a fake settings store; the real
// server root (~/.officraft/server) is never named, never opened, and there is
// no code path here that could reach it even if every assertion below were
// deleted.

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// ── fakes ────────────────────────────────────────────────────────────────────

// fakeSettingStore is the durable seam, in memory. `failGet` / `failPut` make
// the fail-closed paths reachable without corrupting anything real.
type fakeSettingStore struct {
	rows    map[string]string
	failGet bool
	failPut bool
}

func newFakeSettingStore() *fakeSettingStore {
	return &fakeSettingStore{rows: map[string]string{}}
}

func (f *fakeSettingStore) GetSetting(key string) (*string, error) {
	if f.failGet {
		return nil, fmt.Errorf("store unavailable")
	}
	v, ok := f.rows[key]
	if !ok {
		return nil, nil
	}
	return &v, nil
}

func (f *fakeSettingStore) PutSetting(key, value string) error {
	if f.failPut {
		return fmt.Errorf("store unavailable")
	}
	f.rows[key] = value
	return nil
}

// writeBackupFile plants one backup file with a real engine-shaped name, so the
// readers under test (backupFilesIn / backupReasonIn / parseBackupStamp) parse
// it exactly as they would a file the engine wrote.
func writeBackupFile(t *testing.T, dbPath string, at time.Time, reason backupReason) {
	t.Helper()
	dir := backupDirFor(dbPath)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("mkdir backups: %v", err)
	}
	name := backupFileName(at, reason)
	if err := os.WriteFile(filepath.Join(dir, name), []byte("not a real database"), 0o600); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
}

func tempDBPath(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "officraft.db")
}

// ── the decision, exhaustively ───────────────────────────────────────────────

func TestDecideBackupHealth_TheWholeTable(t *testing.T) {
	base := time.Date(2026, 8, 2, 12, 0, 0, 0, time.UTC)
	window := backupStaleAfter()

	cases := []struct {
		name          string
		now           time.Time
		baseline      time.Time
		newest        time.Time
		hasNewest     bool
		lastFailureTS float64
		wantCode      string
	}{
		{
			name:      "a recent scheduled backup is healthy",
			now:       base,
			baseline:  base.Add(-30 * 24 * time.Hour),
			newest:    base.Add(-time.Hour),
			hasNewest: true,
			wantCode:  "",
		},
		{
			// 🔴 THE BRANCH NOTHING HAD EVER EXECUTED. A schedule that worked for
			// a while and then died leaves a real, parseable, but old file behind
			// — the state "no previous backup" can never reach.
			name:      "a previous backup exists but is older than the window",
			now:       base,
			baseline:  base.Add(-30 * 24 * time.Hour),
			newest:    base.Add(-window - time.Minute),
			hasNewest: true,
			wantCode:  backupHealthCodeStale,
		},
		{
			name:      "exactly at the window is not yet stale",
			now:       base,
			baseline:  base.Add(-30 * 24 * time.Hour),
			newest:    base.Add(-window),
			hasNewest: true,
			wantCode:  "",
		},
		{
			// "never ran" and "ran and died" are the same fact to whoever reaches
			// for a backup, so this must alarm on its own.
			name:     "nothing has ever landed and the grace window has passed",
			now:      base,
			baseline: base.Add(-window - time.Minute),
			wantCode: backupHealthCodeNeverRan,
		},
		{
			// A fresh install has no retreat point yet, but that is not evidence
			// of a fault. It reports no CODE — and the surface renders that as
			// unknown, never green (see TestReport_… below).
			name:     "nothing has landed yet but the install is new",
			now:      base,
			baseline: base.Add(-time.Minute),
			wantCode: "",
		},
		{
			name:          "the most recent attempt failed",
			now:           base,
			baseline:      base.Add(-30 * 24 * time.Hour),
			newest:        base.Add(-time.Hour),
			hasNewest:     true,
			lastFailureTS: epochOf(base.Add(-time.Minute)),
			wantCode:      backupHealthCodeFailed,
		},
		{
			// A failure OLDER than the newest backup has already been answered by
			// a successful run; keeping it red would train the owner to ignore
			// the light.
			name:          "a failure older than the newest backup is spent",
			now:           base,
			baseline:      base.Add(-30 * 24 * time.Hour),
			newest:        base.Add(-time.Hour),
			hasNewest:     true,
			lastFailureTS: epochOf(base.Add(-2 * time.Hour)),
			wantCode:      "",
		},
		{
			name:          "a failure with nothing ever landed still reports the failure",
			now:           base,
			baseline:      base.Add(-time.Minute),
			lastFailureTS: epochOf(base.Add(-time.Second)),
			wantCode:      backupHealthCodeFailed,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := decideBackupHealth(tc.now, tc.baseline, tc.newest, tc.hasNewest, tc.lastFailureTS, "the attempt failed")
			if got.Code != tc.wantCode {
				t.Fatalf("code = %q, want %q (detail %q)", got.Code, tc.wantCode, got.Detail)
			}
			if tc.wantCode != "" && strings.TrimSpace(got.Detail) == "" {
				t.Errorf("an alarm with no detail tells the reader nothing about what to go and look at")
			}
		})
	}
}

// TestStaleWindowIsDerivedFromTheEngineConstants stops the alarm window from
// drifting into a second hand-written duration. If someone changes the backup
// interval, the window has to follow it — a stale alarm that fires on a
// hard-coded 12h after the interval moved is worse than none, because it is
// confidently wrong.
func TestStaleWindowIsDerivedFromTheEngineConstants(t *testing.T) {
	if got, want := backupStaleAfter(), backupStaleFactor*backupInterval; got != want {
		t.Fatalf("stale window %s is not backupStaleFactor*backupInterval (%s)", got, want)
	}
}

// ── only the SCHEDULE counts as evidence the schedule is alive ───────────────

// TestManualAndPreMigrationBackupsCannotHideADeadSchedule is the one that stops
// this feature from being quietly useless. Both other triggers write into the
// same directory, so counting them means an owner taking a snapshot by hand —
// or merely upgrading the server — resets the alarm on a cadence that has not
// run in a week.
func TestManualAndPreMigrationBackupsCannotHideADeadSchedule(t *testing.T) {
	dbPath := tempDBPath(t)
	now := time.Date(2026, 8, 2, 12, 0, 0, 0, time.UTC)

	writeBackupFile(t, dbPath, now.Add(-time.Minute), backupReasonManual)
	writeBackupFile(t, dbPath, now.Add(-2*time.Minute), backupReasonPreMigration)

	if _, ok := newestScheduledBackup(dbPath); ok {
		t.Fatal("a manual and a pre-migration snapshot were counted as evidence the schedule is alive")
	}

	store := newFakeSettingStore()
	m := newBackupHealthMonitor(store, dbPath)
	// Arm the baseline far enough back that the grace window is over.
	if _, err := m.evaluate(now.Add(-backupStaleAfter() - time.Hour)); err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	st, err := m.evaluate(now)
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	if st.Code != backupHealthCodeNeverRan {
		t.Fatalf("code = %q, want %q — the hand-made snapshots papered over a schedule that never ran", st.Code, backupHealthCodeNeverRan)
	}

	// A SCHEDULED one, however, is exactly the evidence we are looking for.
	writeBackupFile(t, dbPath, now.Add(-time.Minute), backupReasonScheduled)
	st, err = m.evaluate(now)
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	if st.Code != "" {
		t.Fatalf("code = %q after a scheduled backup landed, want healthy", st.Code)
	}
}

// ── the incident is durable ──────────────────────────────────────────────────

// TestIncidentSurvivesARestartAndKeepsItsStartTime is the "broken for three
// days still reads red on day three" guard. In-memory state would turn every
// restart into a fresh green light, and restarts are exactly what happens
// during an upgrade — the moment someone is most likely to need the retreat.
func TestIncidentSurvivesARestartAndKeepsItsStartTime(t *testing.T) {
	dbPath := tempDBPath(t)
	store := newFakeSettingStore()
	broke := time.Date(2026, 8, 2, 0, 0, 0, 0, time.UTC)

	first := newBackupHealthMonitor(store, dbPath)
	if _, err := first.evaluate(broke.Add(-backupStaleAfter() - time.Hour)); err != nil {
		t.Fatalf("arm: %v", err)
	}
	st, err := first.evaluate(broke)
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	if st.Code != backupHealthCodeNeverRan {
		t.Fatalf("code = %q, want %q", st.Code, backupHealthCodeNeverRan)
	}
	firstSince := st.SinceTS

	// A new process over the same durable store: the incident is still there,
	// still anchored to when it STARTED, not to this tick.
	reborn := newBackupHealthMonitor(store, dbPath)
	later, err := reborn.evaluate(broke.Add(72 * time.Hour))
	if err != nil {
		t.Fatalf("evaluate after restart: %v", err)
	}
	if later.Code != backupHealthCodeNeverRan {
		t.Fatalf("a restart turned the alarm off: code = %q", later.Code)
	}
	if later.SinceTS != firstSince {
		t.Fatalf("incident start moved on restart: %v → %v (the owner would read a three-day outage as brand new)", firstSince, later.SinceTS)
	}

	dto := reborn.report()
	if dto.Status != backupHealthUnhealthy {
		t.Fatalf("status = %q after three days broken, want %q", dto.Status, backupHealthUnhealthy)
	}
}

// TestOnlyAScheduledBackupClearsAnIncident pins the other half: an alarm must
// not be clearable by anything except the thing actually being watched.
func TestOnlyAScheduledBackupClearsAnIncident(t *testing.T) {
	dbPath := tempDBPath(t)
	store := newFakeSettingStore()
	now := time.Date(2026, 8, 2, 12, 0, 0, 0, time.UTC)
	m := newBackupHealthMonitor(store, dbPath)

	m.noteScheduledOutcome(backupResult{Reason: backupReasonScheduled, Skipped: "only 1 MB free"}, nil, now)
	if got := m.report(); got.Status != backupHealthUnhealthy || got.Code != backupHealthCodeFailed {
		t.Fatalf("a skipped scheduled backup must be visible: status=%q code=%q", got.Status, got.Code)
	}

	// A manual backup landing does not answer the question "is the schedule
	// alive?", so it must not clear the alarm.
	writeBackupFile(t, dbPath, now.Add(time.Minute), backupReasonManual)
	if _, err := m.evaluate(now.Add(2 * time.Minute)); err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	if got := m.report(); got.Status != backupHealthUnhealthy {
		t.Fatalf("a manual backup cleared the alarm: status=%q", got.Status)
	}

	// A scheduled one does.
	writeBackupFile(t, dbPath, now.Add(3*time.Minute), backupReasonScheduled)
	m.noteScheduledOutcome(backupResult{Reason: backupReasonScheduled, Path: "x"}, nil, now.Add(3*time.Minute))
	got := m.report()
	if got.Status != backupHealthHealthy || got.Code != "" {
		t.Fatalf("a successful scheduled backup did not clear the alarm: status=%q code=%q", got.Status, got.Code)
	}
	if got.SinceTs != nil {
		t.Errorf("a healthy report still carries an incident start time")
	}
}

// ── the surface never invents a green light ──────────────────────────────────

// TestReport_UnknownIsNeverGreen covers the three ways the server can fail to
// know, each of which must be distinguishable from "you have a retreat point".
func TestReport_UnknownIsNeverGreen(t *testing.T) {
	t.Run("no watchdog at all", func(t *testing.T) {
		var m *backupHealthMonitor
		if got := m.report(); got.Status != backupHealthUnknown {
			t.Fatalf("status = %q, want %q", got.Status, backupHealthUnknown)
		}
	})

	t.Run("never evaluated", func(t *testing.T) {
		m := newBackupHealthMonitor(newFakeSettingStore(), tempDBPath(t))
		if got := m.report(); got.Status != backupHealthUnknown {
			t.Fatalf("status = %q, want %q", got.Status, backupHealthUnknown)
		}
	})

	t.Run("durable state unreadable", func(t *testing.T) {
		store := newFakeSettingStore()
		store.rows[settingBackupHealth] = "{not json"
		m := newBackupHealthMonitor(store, tempDBPath(t))
		if got := m.report(); got.Status != backupHealthUnknown {
			t.Fatalf("status = %q, want %q — a state we cannot read must not read as healthy", got.Status, backupHealthUnknown)
		}
	})

	t.Run("evaluated, nothing has landed yet", func(t *testing.T) {
		store := newFakeSettingStore()
		m := newBackupHealthMonitor(store, tempDBPath(t))
		if _, err := m.evaluate(time.Now()); err != nil {
			t.Fatalf("evaluate: %v", err)
		}
		got := m.report()
		if got.Status == backupHealthHealthy {
			t.Fatal("a studio with no retreat point at all reported healthy")
		}
		if got.Status != backupHealthUnknown {
			t.Fatalf("status = %q, want %q", got.Status, backupHealthUnknown)
		}
	})
}

// TestReport_HealthyCarriesTheEvidence stops the green light from being a bare
// assertion: it must name the backup it is standing on.
func TestReport_HealthyCarriesTheEvidence(t *testing.T) {
	dbPath := tempDBPath(t)
	now := time.Date(2026, 8, 2, 12, 0, 0, 0, time.UTC)
	writeBackupFile(t, dbPath, now.Add(-time.Hour), backupReasonScheduled)

	m := newBackupHealthMonitor(newFakeSettingStore(), dbPath)
	if _, err := m.evaluate(now); err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	got := m.report()
	if got.Status != backupHealthHealthy {
		t.Fatalf("status = %q, want %q", got.Status, backupHealthHealthy)
	}
	if got.NewestBackupTs == nil {
		t.Fatal("a green light with no backup behind it")
	}
	if got.NewestBackupAgeSecs == nil || *got.NewestBackupAgeSecs < 3500 || *got.NewestBackupAgeSecs > 3700 {
		t.Fatalf("age = %v, want about one hour", got.NewestBackupAgeSecs)
	}
	if got.StaleAfterSecs != backupStaleAfter().Seconds() {
		t.Fatalf("stale window on the wire = %v, want %v", got.StaleAfterSecs, backupStaleAfter().Seconds())
	}
}

// ── end to end: manufacture a real failure, read it off the endpoint ─────────

// TestManufacturedFailureIsVisibleOnTheEndpoint is the acceptance test written
// the way the ticket demanded it: do not assert that a code path exists, and do
// not accept a log line as proof. Break a backup FOR REAL (a closed database
// handle makes VACUUM INTO fail exactly as a broken one would), then read the
// endpoint the cockpit reads.
//
// 🔴 The database here is created in t.TempDir(). There is no path from this
// test to the live server root even with every guard below deleted — the
// dangerous act (manufacturing a failure) must not depend on the thing being
// tested to stay harmless.
func TestManufacturedFailureIsVisibleOnTheEndpoint(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "officraft.db")
	db, err := openSQLite(dbPath)
	if err != nil {
		t.Fatalf("open temp db: %v", err)
	}
	if _, err := db.Exec(`CREATE TABLE probe (id INTEGER PRIMARY KEY)`); err != nil {
		t.Fatalf("seed temp db: %v", err)
	}

	store := newFakeSettingStore()
	monitor := newBackupHealthMonitor(store, dbPath)
	api := &apiServer{backupHealth: monitor}
	now := time.Now()

	// Healthy first, so the assertions below cannot be satisfied by a surface
	// that is simply always red.
	if !backupTick(db, dbPath, now, monitor) {
		t.Fatal("the first tick should have taken a backup")
	}
	if got := getBackupHealthDTO(t, api); got.Status != backupHealthHealthy {
		t.Fatalf("after a successful scheduled backup the endpoint says %q (%s)", got.Status, got.Detail)
	}

	// Now break it for real.
	if err := db.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	if backupTick(db, dbPath, now.Add(backupInterval+time.Minute), monitor) {
		t.Fatal("a backup on a closed database reported success")
	}

	got := getBackupHealthDTO(t, api)
	if got.Status != backupHealthUnhealthy {
		t.Fatalf("a REAL failed backup left the cockpit reading %q — this is the silent failure the ticket exists to remove", got.Status)
	}
	if got.Code != backupHealthCodeFailed {
		t.Fatalf("code = %q, want %q", got.Code, backupHealthCodeFailed)
	}
	if got.SinceTs == nil {
		t.Error("the failure carries no start time")
	}
}

// TestNeverRanIsVisibleOnTheEndpoint covers the other half of the ticket: an
// installation whose cadence never started at all. It reaches the surface
// through the same endpoint, because to the person reaching for a backup the
// two failures are one.
func TestNeverRanIsVisibleOnTheEndpoint(t *testing.T) {
	store := newFakeSettingStore()
	monitor := newBackupHealthMonitor(store, tempDBPath(t))
	api := &apiServer{backupHealth: monitor}

	armed := time.Now().Add(-backupStaleAfter() - time.Hour)
	if _, err := monitor.evaluate(armed); err != nil {
		t.Fatalf("arm: %v", err)
	}
	if _, err := monitor.evaluate(time.Now()); err != nil {
		t.Fatalf("evaluate: %v", err)
	}

	got := getBackupHealthDTO(t, api)
	if got.Status != backupHealthUnhealthy || got.Code != backupHealthCodeNeverRan {
		t.Fatalf("a schedule that never ran reads status=%q code=%q — it must alarm as loudly as one that failed", got.Status, got.Code)
	}
}

// TestHealthyBackupNeverNagsTheOwner is the false-alarm guard. The whole
// mechanism is worthless if it cries wolf: a studio whose cadence is working
// must report healthy on every pass, no matter how often the watchdog runs.
func TestHealthyBackupNeverNagsTheOwner(t *testing.T) {
	dbPath := tempDBPath(t)
	now := time.Date(2026, 8, 2, 12, 0, 0, 0, time.UTC)
	writeBackupFile(t, dbPath, now.Add(-time.Hour), backupReasonScheduled)

	monitor := newBackupHealthMonitor(newFakeSettingStore(), dbPath)
	api := &apiServer{backupHealth: monitor}
	for i := 0; i < 20; i++ {
		if _, err := monitor.evaluate(now.Add(time.Duration(i) * backupWatchdogCadence)); err != nil {
			t.Fatalf("evaluate: %v", err)
		}
		if got := getBackupHealthDTO(t, api); got.Status != backupHealthHealthy {
			t.Fatalf("pass %d reported %q (%s) on a working backup", i, got.Status, got.Detail)
		}
	}
}

func getBackupHealthDTO(t *testing.T, api *apiServer) BackupHealthDTO {
	t.Helper()
	rec := httptest.NewRecorder()
	api.HandleGetBackupHealthApiBackupHealthGet(rec, httptest.NewRequest(http.MethodGet, "/api/backup-health", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/backup-health = %d, body %s", rec.Code, rec.Body.String())
	}
	var dto BackupHealthDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &dto); err != nil {
		t.Fatalf("decode: %v (body %s)", err, rec.Body.String())
	}
	return dto
}

// ── wiring: serve really arms it ─────────────────────────────────────────────

// TestServeArmsTheBackupWatchdog covers the CALL SITE, not the function.
//
// 🔴 WHY THIS EXISTS SEPARATELY. Every test above drives the monitor directly,
// so deleting the two lines in cmdServe that arm it would leave all of them
// green while the running server watched nothing at all — a permanently grey
// indicator that looks like a UI bug, not like a missing watchdog. That is
// precisely the "silently removable self-check" shape this repo has been bitten
// by before (see the journal-mode call-site test next door).
//
// It drives the real boot by HOLDING THE PORT serve wants: boot runs through
// open → pre-migration backup → goose → read pool → seed → THIS ARMING, then
// exits 1 on the bind. The baseline row is written synchronously by
// armBackupHealth, so finding it in the database afterwards is proof the call
// site ran — no goroutine to race.
func TestServeArmsTheBackupWatchdog(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "serve-backup-health.db")

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("probe listen: %v", err)
	}
	defer ln.Close()
	port := ln.Addr().(*net.TCPAddr).Port

	cfgPath := filepath.Join(dir, "oc.toml")
	if err := os.WriteFile(cfgPath, []byte(fmt.Sprintf("[server]\nport = %d\n", port)), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	var out strings.Builder
	rc := cmdServe(envOf(map[string]string{
		"OC_CONFIG":          cfgPath,
		"OC_DATABASE_URL":    "sqlite:///" + dbPath,
		"OC_NO_OPEN_BROWSER": "1",
	}), true, true, &out)
	if rc != 1 {
		t.Fatalf("the held port must make serve exit 1 (boot ran, bind failed), got %d\n%s", rc, out.String())
	}

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("reopen db: %v", err)
	}
	defer db.Close()

	var baseline string
	if err := db.QueryRow(`SELECT value FROM setting WHERE key = ?`, settingBackupWatchdogBaseline).Scan(&baseline); err != nil {
		t.Fatalf("serve did not arm the backup watchdog (no %s row): %v\n%s", settingBackupWatchdogBaseline, err, out.String())
	}
	var verdict string
	if err := db.QueryRow(`SELECT value FROM setting WHERE key = ?`, settingBackupHealth).Scan(&verdict); err != nil {
		t.Fatalf("serve armed the watchdog but recorded no verdict (no %s row): %v", settingBackupHealth, err)
	}
	var st backupHealthState
	if err := json.Unmarshal([]byte(verdict), &st); err != nil {
		t.Fatalf("the recorded verdict is not readable: %v (%s)", err, verdict)
	}
	if st.CheckedTS <= 0 {
		t.Fatalf("the recorded verdict has no evaluation time: %+v", st)
	}
}

// ── review round 1: the branches nothing was executing ───────────────────────

// TestRepeatedFailuresKeepTheFirstOnesStartTime is the guard an independent
// review found MISSING: the anchor in noteScheduledOutcome had no test at all,
// so it could be deleted and the whole suite stayed green.
//
// 🔴 WHY IT MATTERS MORE THAN IT LOOKS. The ordinary production shape of a
// broken backup is not one failure — it is the cadence failing again every six
// hours. Without this anchor every retry re-stamps the incident, so the card
// says "failing for a few seconds" forever and the owner can never tell a
// three-day outage from a blip. The user guide promises the opposite in so many
// words.
func TestRepeatedFailuresKeepTheFirstOnesStartTime(t *testing.T) {
	dbPath := tempDBPath(t)
	m := newBackupHealthMonitor(newFakeSettingStore(), dbPath)
	broke := time.Date(2026, 8, 2, 0, 0, 0, 0, time.UTC)
	fail := backupResult{Reason: backupReasonScheduled, Skipped: "only 1 MB free"}

	m.noteScheduledOutcome(fail, nil, broke)
	first := m.report()
	if first.SinceTs == nil {
		t.Fatal("the first failure recorded no start time")
	}

	// Three days of the cadence retrying and failing.
	for i := 1; i <= 12; i++ {
		m.noteScheduledOutcome(fail, nil, broke.Add(time.Duration(i)*6*time.Hour))
	}
	later := m.report()
	if later.SinceTs == nil {
		t.Fatal("the incident lost its start time")
	}
	if *later.SinceTs != *first.SinceTs {
		t.Fatalf("each retry re-stamped the incident (%v → %v): a three-day outage would read as brand new every six hours", *first.SinceTs, *later.SinceTs)
	}
	// And a watchdog pass in between must not move it either.
	if _, err := m.evaluate(broke.Add(80 * time.Hour)); err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	if got := m.report(); got.SinceTs == nil || *got.SinceTs != *first.SinceTs {
		t.Fatalf("a watchdog pass re-stamped the incident: %v", got.SinceTs)
	}
}

// TestAnUnusableSettingsStoreIsNeverGreen exercises the store-failure paths.
// The fake's failGet/failPut existed but nothing ever set them, so the comment
// claiming they made the fail-closed paths reachable was false — and an
// unreachable safety path is not a safety path.
func TestAnUnusableSettingsStoreIsNeverGreen(t *testing.T) {
	t.Run("cannot read the verdict", func(t *testing.T) {
		store := newFakeSettingStore()
		store.failGet = true
		m := newBackupHealthMonitor(store, tempDBPath(t))
		if got := m.report(); got.Status != backupHealthUnknown {
			t.Fatalf("status = %q with an unreadable store, want %q", got.Status, backupHealthUnknown)
		}
	})

	t.Run("cannot write the verdict", func(t *testing.T) {
		store := newFakeSettingStore()
		store.failPut = true
		// Arming must not take the server down over it — same trade as the
		// pre-migration backup hook — and it must not claim health either.
		m := armBackupHealth(store, tempDBPath(t), time.Now())
		if got := m.report(); got.Status == backupHealthHealthy {
			t.Fatal("a server that could not record any verdict reported healthy")
		}
	})
}

// TestACorruptBaselineIsReArmedRatherThanHalfRead pins the parse. fmt.Sscanf
// accepted a numeric PREFIX, so a truncated or appended-to row would parse and
// be believed — and the baseline is the clock "never ran" is measured against.
func TestACorruptBaselineIsReArmedRatherThanHalfRead(t *testing.T) {
	for _, raw := range []string{"", "junk", "1785600000junk", "-1", "0"} {
		store := newFakeSettingStore()
		store.rows[settingBackupWatchdogBaseline] = raw
		m := newBackupHealthMonitor(store, tempDBPath(t))
		now := time.Date(2026, 8, 2, 12, 0, 0, 0, time.UTC)
		got, err := m.baselineAt(now)
		if err != nil {
			t.Fatalf("baselineAt(%q): %v", raw, err)
		}
		if !got.Equal(now) {
			t.Fatalf("baseline %q was half-read as %v instead of being re-armed at %v", raw, got, now)
		}
	}
}

// TestACorruptVerdictRecoversRatherThanFreezing pins the honest boundary
// documented in evaluate(): a verdict row we cannot read is replaced by one
// derived from the filesystem, which is the ground truth for staleness. Pinning
// it matters because the behaviour is a CHOICE — preserving the unreadable row
// would freeze the light forever, since nothing could then clear it.
func TestACorruptVerdictRecoversRatherThanFreezing(t *testing.T) {
	dbPath := tempDBPath(t)
	store := newFakeSettingStore()
	store.rows[settingBackupHealth] = "{not json"
	now := time.Date(2026, 8, 2, 12, 0, 0, 0, time.UTC)

	m := newBackupHealthMonitor(store, dbPath)
	if got := m.report(); got.Status != backupHealthUnknown {
		t.Fatalf("an unreadable verdict must read unknown, got %q", got.Status)
	}

	// Ground truth says the schedule is dead: no scheduled backup at all, well
	// past the grace window. The next pass must therefore ALARM, not go green.
	if _, err := m.evaluate(now.Add(-backupStaleAfter() - time.Hour)); err != nil {
		t.Fatalf("arm: %v", err)
	}
	if _, err := m.evaluate(now); err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	if got := m.report(); got.Status != backupHealthUnhealthy || got.Code != backupHealthCodeNeverRan {
		t.Fatalf("recovery from a corrupt verdict lost the alarm: status=%q code=%q", got.Status, got.Code)
	}
}
