// cutover_run.go — the detached converter (`ocwarden cutover-anchor`, T-ff5d).
//
// This is the grandchild spawned by maybeStartAnchorCutover. It runs in its own
// session, so `launchctl bootout` of the warden job it came from does not take it
// with it — that is the whole reason the conversion can replace the job that
// started it without a one-shot launchd job or a second daemon.
//
// It does FOUR things and no install logic of its own:
//
//  1. back up the current plist to <plist>.prev            ← BEFORE install runs
//  2. exec `ocwarden install --force`                       ← the existing installer
//  3. on non-zero exit: roll back to the backed-up plist
//  4. release the lock; on rollback, drop the cutover.failed sentinel
//
// 🔴 STEP 1 IS BEFORE STEP 2 ON PURPOSE, AND STEP 3 KEYS OFF THE WHOLE INSTALL.
// install's `writePlist` overwrites unconditionally and lints AFTER writing, so
// by the time ANY post-writePlist step fails the old shape is already gone from
// disk. Backing up inside the installer would be too late for the lint case, and
// rolling back only on verify failure — the original design — leaves a machine
// whose next reboot silently adopts an unverified plist. Taking the copy out here
// and treating "install exited non-zero" as the single rollback trigger covers
// lint, bootstrap, kickstart and verify with one condition, and leaves runInstall
// completely untouched.
package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"
)

// cutoverCmd is the `ocwarden cutover-anchor` entry point. It always returns 0
// for a rollback that SUCCEEDED — the machine is back on its old shape with a
// live warden, which is the contracted outcome, not a failure of this process.
// Non-zero is reserved for "the rollback itself did not restore a live warden",
// the one state a human has to know about.
func cutoverCmd(env func(string) string, out io.Writer) int {
	logf := func(format string, a ...any) {
		fmt.Fprintf(out, "%s [cutover] "+format+"\n",
			append([]any{time.Now().UTC().Format(time.RFC3339)}, a...)...)
	}
	exe, err := os.Executable()
	if err != nil {
		logf("FATAL: cannot resolve own binary: %v", err)
		return 1
	}
	p, err := resolvePaths(env, exe, os.Getuid())
	if err != nil {
		logf("FATAL: %v", err)
		return 1
	}
	ops := newCutoverOps()
	defer releaseCutoverLock(ops, env)

	return runCutover(ops, p, exe, logf)
}

// releaseCutoverLock drops the lock the parent took. Best-effort: a leaked lock
// ages out via staleLockAge, so failing to remove it costs one delayed retry
// rather than a permanently wedged machine.
func releaseCutoverLock(ops cutoverOps, env func(string) string) {
	if lock := env("OC_CUTOVER_LOCK"); lock != "" {
		_ = ops.remove(lock)
	}
}

// runCutover is the testable core: every effect goes through ops, so no test can
// reach a real launchctl or a real ~/Library/LaunchAgents path.
func runCutover(ops cutoverOps, p wardenPaths, exe string, logf func(string, ...any)) int {
	prevPath := p.plistPath + plistPrevSuffix
	target := p.guiDomain + "/" + p.labelOrDefault()

	// ---- 1. back up the old plist, BEFORE the installer can overwrite it -----
	old, err := ops.readFile(p.plistPath)
	if err != nil {
		// No readable current plist means there is no old shape to preserve and
		// nothing to roll back to. Refuse rather than convert blind.
		logf("ABORT: cannot read current plist %s: %v (nothing to roll back to)", p.plistPath, err)
		return 1
	}
	if err := ops.writeFile(prevPath, old, 0o644); err != nil {
		logf("ABORT: cannot write plist backup %s: %v", prevPath, err)
		return 1
	}
	logf("backed up current plist -> %s (%d bytes)", prevPath, len(old))

	// ---- 2. the conversion itself: the existing, idempotent installer --------
	logf("running: %s install --force", exe)
	installOut, installErr := ops.runInstaller(exe, "install", "--force")
	if installOut != "" {
		logf("install output:\n%s", installOut)
	}
	if installErr == nil {
		logf("SUCCESS: converted to anchor shape; plist backup kept at %s", prevPath)
		return 0
	}

	// ---- 3. rollback: any non-zero install exit ------------------------------
	// Covers all four post-writePlist failures (plutil lint, bootstrap, kickstart,
	// verify) plus every pre-writePlist one — for those the restore is a no-op
	// write of identical bytes, which is cheap and strictly safer than trying to
	// guess how far the installer got.
	// ---- 3a. did the installer actually CHANGE anything? --------------------
	// The sentinel's one and only justification is stopping a boot-loop: a machine
	// whose plist WAS replaced would otherwise detect "legacy" on the next start
	// and convert again forever. That risk does not exist when the installer died
	// before it modified anything — and treating those two the same turns a
	// TRANSIENT environment condition (the machine happened to be offline, so the
	// ocagent download failed) into a PERMANENT property of the machine: it is
	// excluded from the migration for good, by a network blip.
	//
	// 🔴 The test for "did anything change" is the FACT (the on-disk plist no
	// longer matches the backup), deliberately NOT "which step failed". A step
	// index silently picks the wrong side the moment someone inserts a step, and
	// nothing would go red. An unreadable plist counts as CHANGED — if we cannot
	// establish that the machine is untouched, we must not claim it is.
	if current, readErr := ops.readFile(p.plistPath); readErr == nil && string(current) == string(old) {
		// Nothing to restore and nothing to boot-loop. Note this also skips a
		// bootout→bootstrap cycle that would otherwise be run for no reason at
		// all — needlessly opening the one window where a machine can end up with
		// no warden.
		logf("install FAILED (%v) but nothing was modified — machine is untouched, leaving no sentinel so the next start retries", installErr)
		return 0
	}

	logf("install FAILED (%v) — rolling back to the pre-conversion shape", installErr)
	if rbErr := rollback(ops, p, old, target, logf); rbErr != nil {
		// The machine may now have no warden. Say so loudly and locally: this
		// process cannot rely on telemetry, because telemetry is the thing that
		// just failed to come back up.
		logf("🔴 ROLLBACK FAILED: %v", rbErr)
		writeCutoverSentinel(ops, p, fmt.Sprintf("install failed: %v\nrollback FAILED: %v", installErr, rbErr), logf)
		return 1
	}
	logf("rollback complete: launchd is running the pre-conversion shape again")
	writeCutoverSentinel(ops, p, fmt.Sprintf("install failed: %v\nrolled back successfully", installErr), logf)
	return 0
}

// rollback puts the old plist back and makes launchd actually read it. Writing
// the file is not enough: launchd caches a job's configuration at bootstrap, so
// an un-booted-out label keeps the NEW settings until the next login — measured
// on macOS 26.5 and 15.7.7, where both KeepAlive respawn and `launchctl kickstart
// -k` were shown to keep serving the stale configuration. Only bootout→bootstrap
// re-reads it.
func rollback(ops cutoverOps, p wardenPaths, old []byte, target string, logf func(string, ...any)) error {
	if err := ops.writeFile(p.plistPath, old, 0o644); err != nil {
		return fmt.Errorf("restore plist %s: %w", p.plistPath, err)
	}
	logf("restored %s from backup", p.plistPath)

	// Tolerate a bootout error: "not currently loaded" is the expected outcome
	// when the failure happened before or during bootstrap.
	_, _ = ops.run("launchctl", "bootout", target)
	if !cutoverWaitGone(ops, target) {
		logf("WARN: %s still registered after bootout; bootstrapping anyway", target)
	}
	if _, err := ops.run("launchctl", "bootstrap", p.guiDomain, p.plistPath); err != nil {
		return fmt.Errorf("re-bootstrap old shape: %w", err)
	}
	if _, err := ops.run("launchctl", "kickstart", "-k", target); err != nil {
		return fmt.Errorf("kickstart old shape: %w", err)
	}
	if err := cutoverVerifyAlive(ops, target); err != nil {
		return fmt.Errorf("old shape did not come back: %w", err)
	}
	return nil
}

// cutoverWaitGone mirrors install's bootoutUntilGone: bootout is ASYNC, and a
// bootstrap issued while the dying registration lingers fails with "Bootstrap
// failed: 5". Kept local rather than shared so the rollback path cannot be
// broken by a future change to the install path's bounds.
func cutoverWaitGone(ops cutoverOps, target string) bool {
	for k := 0; k < bootoutPollAttempts; k++ {
		if _, err := ops.run("launchctl", "print", target); err != nil {
			return true
		}
		ops.sleep(bootoutPollInterval)
	}
	return false
}

// cutoverVerifyAlive requires the restored job to hold ONE pid across a settle
// window. "Saw a pid once" is not proof: a warden that cannot start is respawned
// by KeepAlive under a different pid, which looks alive on a single sample.
func cutoverVerifyAlive(ops cutoverOps, target string) error {
	var pid string
	for k := 0; k < 30; k++ {
		if pid = cutoverPID(ops, target); pid != "" {
			break
		}
		ops.sleep(time.Second)
	}
	if pid == "" {
		return fmt.Errorf("%s reported no live pid within 30s", target)
	}
	for k := 0; k < 6; k++ {
		ops.sleep(time.Second)
		if now := cutoverPID(ops, target); now != pid {
			return fmt.Errorf("%s is crash-looping (pid %s -> %s)", target, pid, orNone(now))
		}
	}
	return nil
}

func cutoverPID(ops cutoverOps, target string) string {
	out, err := ops.run("launchctl", "print", target)
	if err != nil {
		return ""
	}
	if m := launchctlPIDRe.FindStringSubmatch(out); m != nil {
		return m[1]
	}
	return ""
}

// writeCutoverSentinel records that this machine tried the conversion and ended
// up back on the old shape. maybeStartAnchorCutover refuses to start while it
// exists — without it, a rolled-back machine detects "legacy" on its very next
// start and converts again, forever.
func writeCutoverSentinel(ops cutoverOps, p wardenPaths, reason string, logf func(string, ...any)) {
	path := filepath.Join(p.root, "warden", cutoverFailedName)
	body := fmt.Sprintf("%s\n%s\n\nRemove this file to allow another attempt.\n",
		time.Now().UTC().Format(time.RFC3339), reason)
	if err := ops.writeFile(path, []byte(body), 0o644); err != nil {
		logf("WARN: could not write %s: %v (the next start will retry the conversion)", path, err)
		return
	}
	logf("wrote %s — further conversion attempts are blocked until it is removed", path)
}
