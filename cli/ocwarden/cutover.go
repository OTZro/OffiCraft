// cutover.go — automatic legacy→anchor launchd SHAPE migration (T-ff5d).
//
// THE PROBLEM
// -----------
// The anchor shape shipped (plist ProgramArguments names the never-replaced TCC
// identity anchor `officraft`, which forks the sibling ocwarden), but self-update
// only swaps BINARIES: selfupdate.go replaces selfPath + the sibling ocagent and
// never touches the plist or launchctl. So a machine installed before the anchor
// shape existed keeps its legacy plist (`[…/warden/ocwarden, run]`) FOREVER, and
// every self-update voids that machine's TCC grants because launchd's job leader
// is the very file being replaced. OffiCraft has external users, so "ask them to
// re-run the installer" is not a migration plan.
//
// THE MECHANISM
// -------------
// `ocwarden run` checks its shape once at startup. Self-update is a syscall.Exec
// in-place image swap (same PID), so a machine that self-updates is running this
// code within seconds — no launchd restart needed. On a legacy machine it spawns
// a DETACHED grandchild (`ocwarden cutover-anchor`, setsid → its own session) and
// returns immediately; the grandchild survives its parent's launchd job being
// booted out, which is exactly what the conversion has to do to itself.
//
// The conversion body is ZERO new install logic: it shells out to the existing,
// idempotent `ocwarden install --force`, which deploys the anchor (preserving an
// existing one — never replace, or the TCC grant dies), renders the anchor-shape
// plist, boots out, bootstraps, and health-verifies.
//
// 🔴 WHY THE BACKUP LIVES OUT HERE AND NOT IN runInstall
// -------------------------------------------------------
// runInstall's ordering is genuinely fail-safe for the DESTRUCTIVE launchctl step:
// every fallible preparation (claude/codex resolve, anchor deploy, ocagent
// DOWNLOAD, tokfile, log dir) runs before bootout, so an offline machine never
// begins the conversion at all. Verified line by line, not assumed.
//
// But `writePlist` is not a preparation step — it OVERWRITES the old plist
// unconditionally, and `plutil -lint` runs AFTER the write (install.go: write at
// 936, lint at 939). A lint failure therefore leaves the new plist on disk with
// launchd still running the old shape: nothing is wrong TODAY, and the next
// reboot silently adopts a plist that was never verified. The original design
// only rolled back on verify failure, which cannot see that case.
//
// So the backup is taken HERE, before `install` is invoked at all, and the
// rollback fires on ANY non-zero install exit — which covers all four
// post-writePlist failures (lint, bootstrap, kickstart, verify) with one
// condition instead of four. runInstall itself is untouched.
//
// FAIL-SAFE CONTRACT
// ------------------
// Every failure path must land on "old shape, warden alive". Concretely:
//   - unknown shape        → do nothing (never guess)
//   - preflight fails      → do nothing (the anchor is missing/quarantined)
//   - lock held            → do nothing (a conversion is already in flight)
//   - cutover.failed exists→ do nothing (a previous attempt rolled back; never
//     retry in a loop against a machine that rejected it)
//   - install fails        → restore the backed-up plist, re-bootstrap, verify
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// wardenShape is what launchd is ACTUALLY executing for this job. Deliberately
// tri-state: "the anchor file exists on disk" is NOT the same claim as "launchd
// runs the anchor", and reporting a guess as a verdict is worse than reporting
// nothing (the server reads a missing/unknown shape as unknown and never infers).
type wardenShape string

const (
	shapeAnchor  wardenShape = "anchor"
	shapeLegacy  wardenShape = "legacy"
	shapeUnknown wardenShape = "unknown"
)

const (
	// cutoverSubcmd is the detached grandchild's hidden subcommand. Deliberately
	// absent from the usage banner: it is an internal step of the migration, not
	// an operator verb, and CI's committed-prebuilt parity dryrun diffs --help.
	cutoverSubcmd = "cutover-anchor"

	// cutoverLockName is an O_EXCL lockfile under the warden dir. Its ONLY job is
	// to keep two conversions from racing; it is removed when the grandchild is
	// done, and a stale one is aged out (staleLockAge) so a machine killed
	// mid-conversion is not locked out forever.
	cutoverLockName = "cutover.lock"

	// cutoverFailedName is the sentinel a rolled-back conversion leaves behind. It
	// is what stops a boot-loop: without it, a machine that rolls back would be
	// running the old shape again, detect "legacy" on the next start, and convert
	// again — forever. Its presence means "this machine tried and rejected the
	// conversion"; removing it by hand is the deliberate way to retry.
	cutoverFailedName = "cutover.failed"

	// plistPrevSuffix names the pre-conversion plist backup. writePlist overwrites
	// unconditionally and CUTOVER.md documents that no backup is kept, so this is
	// the ONLY copy of the old shape once the install starts.
	plistPrevSuffix = ".prev"

	// cutoverInstallBudget bounds the ONE long call: `ocwarden install --force`.
	// It cannot share the 30s budget the probe commands use, and this is not a
	// safety margin — it is a correctness bug the short budget would cause:
	// runInstall's health verify ALONE is up to 36s (30 x 1s to see a pid, then
	// 6 x 1s of stability, install.go), and before that it DOWNLOADS ocagent over
	// whatever link the machine has. Killing the installer at 30s reads as an
	// install failure, so the machine rolls back AND writes the cutover.failed
	// sentinel — which never retries. A short budget therefore does not make the
	// conversion "slower to succeed"; it makes it PERMANENTLY fail on machines
	// that were merely slow, while looking exactly like a machine that refused.
	//
	// The number is DERIVED from install's own bounded steps, not picked — every
	// term below is an existing constant in this package, so a future reader can
	// re-derive it instead of nudging a magic threshold:
	//
	//	claude resolve   <= 2 x claudeProbeBudget (20s)        =  40s
	//	codex resolve    <= 2 x claudeProbeBudget (20s)        =  40s
	//	ocagent download <= selfUpdateHTTPTimeout (60s)        =  60s
	//	bootout poll     <= bootoutPollAttempts x Interval     =   5s
	//	verify           <= 30 x 1s + 6 x 1s                   =  36s
	//	                                                   total ~181s
	//
	// 10 minutes is that worst case with ~3x headroom, and headroom is the right
	// posture here because this budget's ONLY job is to stop an install that has
	// hung forever — it must never be the thing that decides a healthy conversion
	// failed. Overshooting costs one detached process sitting idle; undershooting
	// costs a machine, permanently.
	cutoverInstallBudget = 10 * time.Minute

	// staleLockAge bounds how long a lockfile is honoured. A conversion is a
	// bounded operation (install's own verify caps at ~36s); anything older than
	// this is a corpse from a killed process, not a live conversion.
	staleLockAge = 15 * time.Minute
)

// cutoverOps is the side-effect seam for everything in this file. Production
// binds it to the real OS exactly once (realCutoverOps); the test binary binds a
// fake, so no test can reach launchctl or a real LaunchAgents path. This mirrors
// hostSeam's reasoning: the guard being tested must never be the thing keeping
// the test safe.
type cutoverOps struct {
	// ppidExe returns the executable path of the given pid. The ONLY signal that
	// cannot lie about the running shape.
	ppidExe func(pid int) (string, error)
	run     func(name string, args ...string) (string, error)
	// runExit reports a command's EXIT CODE rather than an opaque error, because
	// the anchor preflight is a specific-code assertion (exit 2 = "rejected the
	// argument", the anchor's zero-side-effect usage path) and "some error" cannot
	// distinguish that from "could not execute at all". A non-nil error means the
	// process never ran; a nil error with a code means it ran and exited with it.
	runExit func(name string, args ...string) (int, error)
	// runInstaller runs the conversion itself. Separate from run because it needs
	// a budget an order of magnitude larger (cutoverInstallBudget) — sharing run's
	// probe budget silently converts "slow machine" into "permanently refused".
	runInstaller func(name string, args ...string) (string, error)
	readFile     func(path string) ([]byte, error)
	writeFile    func(path string, data []byte, perm os.FileMode) error
	remove       func(path string) error
	// createExcl creates path O_EXCL and reports whether THIS call created it.
	createExcl func(path string) (bool, error)
	// modTime is used only to age out a stale lock.
	modTime func(path string) (time.Time, error)
	// spawnDetached starts a setsid'd grandchild that outlives this process's
	// launchd job being booted out, and does NOT wait for it.
	spawnDetached func(bin string, args []string, env []string, logPath string) error
	sleep         func(time.Duration)
}

func realCutoverOps() cutoverOps {
	// Same last-resort tripwire realSysOps/realHostSeam carry, for the same
	// reason: this seam drives launchctl bootout/bootstrap on the canonical label
	// and overwrites the live plist. A test binary must never be able to build it,
	// and "the suite went red afterwards" is not a defence for that.
	refuseInTestBinary("realCutoverOps")
	r := newCmdRunner(30 * time.Second)
	return cutoverOps{
		ppidExe:      psExePath(r),
		run:          r.Run,
		runExit:      realRunExit,
		runInstaller: runInstallerCombined,
		readFile:     os.ReadFile,
		writeFile:    os.WriteFile,
		remove:       os.Remove,
		createExcl: func(path string) (bool, error) {
			f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
			if err != nil {
				if os.IsExist(err) {
					return false, nil
				}
				return false, err
			}
			_ = f.Close()
			return true, nil
		},
		modTime: func(path string) (time.Time, error) {
			fi, err := os.Stat(path)
			if err != nil {
				return time.Time{}, err
			}
			return fi.ModTime(), nil
		},
		spawnDetached: spawnDetachedProcess,
		sleep:         time.Sleep,
	}
}

// newCutoverOps is the injection point (same pattern as newHostSeam): production
// leaves it at realCutoverOps, the test binary rebinds it before any test runs.
var newCutoverOps = realCutoverOps

// psExePath reads a pid's executable path via `ps -p <pid> -o comm=`, which on
// macOS prints the full path. A pid that has already exited exits non-zero →
// unknown, never a guess.
func psExePath(r CmdRunner) func(int) (string, error) {
	return func(pid int) (string, error) {
		out, err := r.Run("ps", "-p", strconv.Itoa(pid), "-o", "comm=")
		if err != nil {
			return "", err
		}
		return strings.TrimSpace(out), nil
	}
}

// spawnDetachedProcess starts bin in its OWN SESSION (Setsid) and releases it, so
// it is not in this process's process group and survives `launchctl bootout` of
// this job — measured on macOS 26.5, and the whole reason no one-shot launchd job
// or extra daemon is needed. Its output goes to logPath because the grandchild
// outlives the log stream launchd gave its parent.
func spawnDetachedProcess(bin string, args []string, env []string, logPath string) error {
	cmd := exec.Command(bin, args...)
	cmd.Env = env
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if logPath != "" {
		if f, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644); err == nil {
			cmd.Stdout = f
			cmd.Stderr = f
			defer f.Close()
		}
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}

// detectShape answers what launchd is ACTUALLY running, from the parent process's
// executable path — the one signal that cannot be wrong:
//
//   - parent is the anchor            → anchor  (launchd → officraft → ocwarden)
//   - parent is launchd (pid 1)       → legacy  (launchd → ocwarden run)
//   - anything else (a shell, a test) → unknown (NOT a verdict; never convert)
//
// Deliberately NOT "does ~/.officraft/warden/officraft exist": a machine can have
// the anchor file on disk and still be booted from a legacy plist, which is the
// exact state this migration exists to fix.
func detectShape(ops cutoverOps, ppid int, anchorPath string) wardenShape {
	exe, err := ops.ppidExe(ppid)
	if err != nil || exe == "" {
		return shapeUnknown
	}
	if exe == anchorPath {
		return shapeAnchor
	}
	if ppid == 1 || filepath.Base(exe) == "launchd" {
		return shapeLegacy
	}
	return shapeUnknown
}

// newShapeReporter builds the 30s heartbeat's warden-shape collector: the same
// detectShape verdict the startup hook computes for its own go/no-go decision,
// re-read once per cycle so the FLEET can tell a converted machine from an
// unconverted one. Before this the verdict existed only inside one process's
// startup log line, which is why nothing outside the machine could say whether
// the migration had landed.
//
// It goes through newCutoverOps like every other reader in this file, so the
// test binary's rebound seam covers it too — a heartbeat test can never reach a
// real `ps`.
//
// Re-read rather than cached ON PURPOSE. The cost is one `ps` per cycle, next to
// the five probes collectHardware already shells out for, and a cached verdict
// would have to answer for the case this file exists to create: the conversion
// boots the job out and launchd restarts it, so the process that reports is
// never the one that cached.
//
// An empty anchorPath means the install paths could not be resolved, and the
// answer there is `unknown`, NOT an omitted field and NOT a guess. Omission is
// reserved for builds that predate this release ("this machine has not received
// it yet"), so a build that HAS it must never look like one that has not. And a
// guess would be actively dangerous: detectShape compares the parent exe against
// anchorPath, so an empty one turns every launchd-parented warden — including a
// correctly converted one — into a `legacy` verdict.
func newShapeReporter(anchorPath string, ppid int) func() string {
	return func() string {
		if anchorPath == "" {
			return string(shapeUnknown)
		}
		return string(detectShape(newCutoverOps(), ppid, anchorPath))
	}
}

// anchorPreflight proves the anchor is present, executable and not quarantined,
// with ZERO side effects: `officraft <any arg>` prints usage and exits 2 (see
// cli/officraft/main.go realMain) — it forks nothing. Any other outcome (missing
// file, Gatekeeper kill, wrong arch) means the anchor cannot be launchd's job
// leader, so the conversion must not start.
func anchorPreflight(ops cutoverOps, anchorPath string) error {
	code, err := ops.runExit(anchorPath, "--preflight")
	if err != nil {
		return fmt.Errorf("anchor preflight: cannot execute %s: %w", anchorPath, err)
	}
	if code != 2 {
		return fmt.Errorf("anchor preflight: %s exited %d, want 2 (a build whose anchor does not reject arguments is not the anchor this expects)", anchorPath, code)
	}
	return nil
}

// realRunExit runs a command purely to observe its exit code. Bounded by the same
// budget the self-update probe uses — the anchor's usage path returns instantly,
// so anything slower is a hung or quarantined binary, which must read as "do not
// convert" rather than block the warden's startup indefinitely.
func realRunExit(name string, args ...string) (int, error) {
	ctx, cancel := context.WithTimeout(context.Background(), selfUpdateProbeBudget)
	defer cancel()
	err := exec.CommandContext(ctx, name, args...).Run()
	if err == nil {
		return 0, nil
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return ee.ExitCode(), nil
	}
	return -1, err
}

// maybeStartAnchorCutover is the `ocwarden run` startup hook. It is best-effort
// and NEVER fails the warden: every refusal returns quietly, because a warden
// that will not start is strictly worse than one running the old shape.
func maybeStartAnchorCutover(p wardenPaths, ppid int, logf func(string, ...any)) {
	ops := newCutoverOps()
	wardenDir := filepath.Join(p.root, "warden")

	shape := detectShape(ops, ppid, p.anchorPath)
	if shape != shapeLegacy {
		return
	}
	// A previous attempt rolled back. Converting again would boot-loop the machine
	// through the same failure on every start; the sentinel is removed by hand.
	failedPath := filepath.Join(wardenDir, cutoverFailedName)
	if _, err := ops.readFile(failedPath); err == nil {
		logf("[ocwarden] anchor cutover: skipped — a previous attempt rolled back (%s)", failedPath)
		return
	}
	if err := anchorPreflight(ops, p.anchorPath); err != nil {
		logf("[ocwarden] anchor cutover: skipped — %v", err)
		return
	}
	lockPath := filepath.Join(wardenDir, cutoverLockName)
	if !acquireCutoverLock(ops, lockPath) {
		logf("[ocwarden] anchor cutover: skipped — another conversion holds %s", lockPath)
		return
	}
	env := append(os.Environ(),
		"OC_BASE="+p.ocBase,
		"OC_TOKEN="+p.ocToken,
		"OC_CUTOVER_LOCK="+lockPath,
	)
	if p.namespace != "" {
		env = append(env, "OC_NAMESPACE="+p.namespace)
	}
	logPath := filepath.Join(p.logDir, "cutover.log")
	if err := ops.spawnDetached(p.binPath, []string{cutoverSubcmd}, env, logPath); err != nil {
		_ = ops.remove(lockPath)
		logf("[ocwarden] anchor cutover: could not start converter: %v", err)
		return
	}
	logf("[ocwarden] anchor cutover: legacy shape detected; detached converter started (log: %s)", logPath)
}

// acquireCutoverLock takes the O_EXCL lock, aging out a corpse older than
// staleLockAge so a machine killed mid-conversion is not locked out forever.
func acquireCutoverLock(ops cutoverOps, lockPath string) bool {
	created, err := ops.createExcl(lockPath)
	if err != nil {
		return false
	}
	if created {
		return true
	}
	mt, err := ops.modTime(lockPath)
	if err != nil || time.Since(mt) < staleLockAge {
		return false
	}
	if err := ops.remove(lockPath); err != nil {
		return false
	}
	created, err = ops.createExcl(lockPath)
	return err == nil && created
}

// runInstallerCombined runs `ocwarden install --force` and returns its output
// WHETHER OR NOT IT FAILED.
//
// The shared execRunner deliberately drops stdout on a non-zero exit (it returns
// "" plus an error carrying stderr). For every other caller that is fine — they
// only classify the error. For this one caller it destroys the only account of
// WHY a machine refused the conversion: the installer narrates its progress on
// stdout, so a rolled-back machine's cutover.log read exactly
// "install FAILED (exit status 1)" and nothing else. Offline, quarantined anchor,
// plutil rejecting the plist and a launchd bootstrap failure are then
// indistinguishable — on a user's machine, after the fact, with no other
// telemetry. Observed on a real macOS 26 injection run before this existed.
//
// Same budget as before (cutoverInstallBudget) and the same test-binary refusal
// as execRunner.Run: this starts a process, so it must be reachable only from
// production, never from a test.
func runInstallerCombined(name string, args ...string) (string, error) {
	refuseInTestBinary("runInstallerCombined(" + name + ")")
	ctx, cancel := context.WithTimeout(context.Background(), cutoverInstallBudget)
	defer cancel()
	out, err := exec.CommandContext(ctx, name, args...).CombinedOutput()
	return string(out), err
}
