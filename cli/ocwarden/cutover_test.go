package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

// errCutoverBlocked is what the package-default cutover seam returns inside the
// test binary. Mirrors errHostSeamBlocked: a test that forgets to install its own
// fake gets a loud refusal, never a real launchctl call or a real plist write.
var errCutoverBlocked = errors.New("cutover seam is blocked in the test binary")

func blockedCutoverOps() cutoverOps {
	block := func(what string) error {
		return fmt.Errorf("%w: refusing %s", errCutoverBlocked, what)
	}
	return cutoverOps{
		ppidExe:       func(int) (string, error) { return "", block("ps") },
		run:           func(n string, _ ...string) (string, error) { return "", block("exec " + n) },
		runExit:       func(n string, _ ...string) (int, error) { return -1, block("exec " + n) },
		runInstaller:  func(n string, _ ...string) (string, error) { return "", block("exec " + n) },
		readFile:      func(p string) ([]byte, error) { return nil, block("read " + p) },
		writeFile:     func(p string, _ []byte, _ os.FileMode) error { return block("write " + p) },
		chmod:         func(p string, _ os.FileMode) error { return block("chmod " + p) },
		remove:        func(p string) error { return block("remove " + p) },
		createExcl:    func(p string) (bool, error) { return false, block("create " + p) },
		modTime:       func(p string) (time.Time, error) { return time.Time{}, block("stat " + p) },
		spawnDetached: func(b string, _, _ []string, _ string) error { return block("spawn " + b) },
		sleep:         func(time.Duration) {},
	}
}

// fakeCutover is a recording, fully in-memory cutover seam. Files live in a map,
// launchctl calls are recorded as argv strings and answered from a scripted table,
// so a test can drive every branch without a filesystem or a launchd domain.
type fakeCutover struct {
	files map[string]string
	// runErr answers a call keyed by its full argv joined with spaces. A missing
	// key means success with the (possibly empty) output in runOut.
	runErr map[string]error
	runOut map[string]string
	calls  []string
	// exitCodes answers runExit, keyed by argv. A missing key means exit 0.
	exitCodes map[string]int
	exitErrs  map[string]error
	// pids answers `launchctl print` — consumed one entry per call so a test can
	// script "no pid, then pid 42, then pid 42...". Exhausted → last value repeats.
	pids     []string
	pidIdx   int
	locked   map[string]bool
	modTimes map[string]time.Time
	spawned  []string
}

func newFakeCutover() *fakeCutover {
	return &fakeCutover{
		files:     map[string]string{},
		runErr:    map[string]error{},
		runOut:    map[string]string{},
		locked:    map[string]bool{},
		modTimes:  map[string]time.Time{},
		exitCodes: map[string]int{},
		exitErrs:  map[string]error{},
	}
}

func (f *fakeCutover) ops() cutoverOps {
	// run and runInstaller differ ONLY in their real-world timeout budget, so the
	// fake answers both from one recorder — a test scripting "install fails" must
	// not have to know which of the two the production code happened to pick.
	run := func(name string, args ...string) (string, error) {
		key := name
		for _, a := range args {
			key += " " + a
		}
		f.calls = append(f.calls, key)
		if name == "launchctl" && len(args) > 0 && args[0] == "print" {
			if err, ok := f.runErr[key]; ok {
				return "", err
			}
			pid := f.nextPID()
			if pid == "" {
				return "", errors.New("no such process")
			}
			return "\tpid = " + pid + "\n", nil
		}
		return f.runOut[key], f.runErr[key]
	}
	return cutoverOps{
		ppidExe:      func(int) (string, error) { return f.files["__ppid_exe__"], nil },
		run:          run,
		runInstaller: run,
		runExit: func(name string, args ...string) (int, error) {
			key := name
			for _, a := range args {
				key += " " + a
			}
			f.calls = append(f.calls, key)
			if err, ok := f.exitErrs[key]; ok {
				return -1, err
			}
			return f.exitCodes[key], nil
		},
		readFile: func(p string) ([]byte, error) {
			v, ok := f.files[p]
			if !ok {
				return nil, os.ErrNotExist
			}
			return []byte(v), nil
		},
		writeFile: func(p string, d []byte, _ os.FileMode) error {
			if err, ok := f.runErr["write:"+p]; ok {
				return err
			}
			// Recorded, because for the anchor the rule is about the INODE, not the
			// bytes: rewriting identical bytes still mints a new file and therefore a
			// new TCC identity. "the content is unchanged" cannot see that; "no write
			// happened" can.
			f.calls = append(f.calls, "write "+p)
			f.files[p] = string(d)
			// A written file exists, and modTime is what the anchor gate reads to
			// decide that. Without this the fake would report a file as absent
			// immediately after writing it, and "never replace an existing anchor"
			// could not be exercised at all.
			f.modTimes[p] = time.Now()
			return nil
		},
		chmod: func(p string, _ os.FileMode) error {
			if err, ok := f.runErr["chmod:"+p]; ok {
				return err
			}
			f.calls = append(f.calls, "chmod "+p)
			return nil
		},
		remove: func(p string) error {
			delete(f.files, p)
			delete(f.locked, p)
			delete(f.modTimes, p)
			return nil
		},
		createExcl: func(p string) (bool, error) {
			if f.locked[p] {
				return false, nil
			}
			f.locked[p] = true
			return true, nil
		},
		modTime: func(p string) (time.Time, error) {
			t, ok := f.modTimes[p]
			if !ok {
				return time.Time{}, os.ErrNotExist
			}
			return t, nil
		},
		spawnDetached: func(bin string, args []string, _ []string, _ string) error {
			if err, ok := f.runErr["spawn"]; ok {
				return err
			}
			f.spawned = append(f.spawned, bin+" "+args[0])
			return nil
		},
		sleep: func(time.Duration) {},
	}
}

func (f *fakeCutover) nextPID() string {
	if len(f.pids) == 0 {
		return "100"
	}
	if f.pidIdx >= len(f.pids) {
		return f.pids[len(f.pids)-1]
	}
	v := f.pids[f.pidIdx]
	f.pidIdx++
	return v
}

func testPaths() wardenPaths {
	return wardenPaths{
		root:       "/home/u/.officraft",
		home:       "/home/u",
		plistPath:  "/home/u/Library/LaunchAgents/com.officraft.ocwarden.plist",
		logDir:     "/home/u/.officraft/warden/log",
		binPath:    "/home/u/.officraft/warden/ocwarden",
		anchorPath: "/home/u/.officraft/warden/officraft",
		// In a home install this is the SAME path as anchorPath (the anchor is the
		// running ocwarden's sibling), which is exactly why the embedded copy — not
		// this one — is what actually lands on the machines this migration targets.
		anchorSrc: "/home/u/.officraft/warden/officraft",
		guiDomain: "gui/501",
		ocBase:    "http://example.test",
		ocToken:   "tok",
	}
}

const legacyPlist = "<plist>LEGACY [ocwarden run]</plist>"

func TestDetectShape(t *testing.T) {
	p := testPaths()
	for _, tc := range []struct {
		name string
		exe  string
		ppid int
		want wardenShape
	}{
		{"launchd runs the anchor", p.anchorPath, 90, shapeAnchor},
		{"launchd runs ocwarden directly", "/sbin/launchd", 1, shapeLegacy},
		{"started from a shell", "/bin/zsh", 4242, shapeUnknown},
		{"parent already gone", "", 4242, shapeUnknown},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFakeCutover()
			f.files["__ppid_exe__"] = tc.exe
			if got := detectShape(f.ops(), tc.ppid, p.anchorPath); got != tc.want {
				t.Fatalf("detectShape = %q, want %q", got, tc.want)
			}
		})
	}
}

// Distinct bodies so an assertion can tell WHICH anchor is on the machine — "a
// file exists at anchorPath" cannot distinguish "we wrote ours over the
// operator's" from "we left theirs alone", and that is the whole never-replace
// rule.
const (
	buildAnchorBytes    = "<anchor bytes: the copy embedded in this ocwarden>"
	operatorAnchorBytes = "<anchor bytes: the one already on this machine>"
)

// withEmbeddedAnchor rebinds the anchor compiled into ocwarden for one test.
//
// The test binary's anchordist/ holds only .gitkeep, so the real embeddedAnchor()
// is EMPTY here. That is load-bearing in both directions: a test that needs a
// machine to be convertible has to say so out loud, and a test that needs "no
// anchor obtainable anywhere" gets that state by simply not calling this.
func withEmbeddedAnchor(t *testing.T, body string) {
	t.Helper()
	prev := embeddedAnchor
	embeddedAnchor = func() []byte { return []byte(body) }
	t.Cleanup(func() { embeddedAnchor = prev })
}

// anchorAlreadyOnDisk models a machine that already carries an anchor. modTime is
// the existence oracle ensureAnchorPresent reads, so both halves are required.
func anchorAlreadyOnDisk(f *fakeCutover, p wardenPaths, body string) {
	f.files[p.anchorPath] = body
	f.modTimes[p.anchorPath] = time.Now()
}

// ⚠️ THE ANCHOR AND PREFLIGHT GATES MUST BOTH PASS IN EVERY GATE TEST BELOW, AND
// THAT IS NOT BOILERPLATE.
//
// maybeStartAnchorCutover refuses in a fixed order: shape, then sentinel, then
// anchor-present, then preflight, then lock. newFakeCutover leaves exitCodes
// empty, so runExit answers 0 — and anchorPreflight demands exactly 2. It also
// leaves the machine with no anchor and no embedded copy. Without BOTH lines
// below, an earlier gate refuses first and `len(f.spawned) == 0` holds for a
// reason that has nothing to do with the gate the test is named after.
//
// That is not hypothetical: these three tests shipped without the preflight line,
// and independent review deleted the shape guard, the sentinel guard and the lock
// guard OUTRIGHT, one at a time, with the whole suite staying green each time.
// The anchor gate added by the preflight-ordering fix is a second way to make the
// same tests vacuous, so it is satisfied in the same helper rather than left for
// each test to remember.
func passingPreflight(t *testing.T, f *fakeCutover, p wardenPaths) {
	t.Helper()
	withEmbeddedAnchor(t, buildAnchorBytes)
	f.exitCodes[p.anchorPath+" --preflight"] = 2
}

// A machine whose shape cannot be established must be left alone. Guessing
// "probably legacy" would convert machines nobody has evidence about.
func TestUnknownShapeStartsNoConversion(t *testing.T) {
	p := testPaths()
	f := newFakeCutover()
	f.files["__ppid_exe__"] = "/bin/zsh"
	passingPreflight(t, f, p)
	restore := swapCutoverOps(t, f)
	defer restore()

	maybeStartAnchorCutover(p, 4242, func(string, ...any) {})
	if len(f.spawned) != 0 {
		t.Fatalf("converter was started for an unknown shape: %v", f.spawned)
	}
}

// A machine ALREADY ON THE ANCHOR SHAPE must not convert. This case had no test
// at all, and it is the one every healthy machine in the fleet is in after the
// migration: converting again would boot out a working job, re-run install, and
// put a machine that had nothing wrong with it through the one window where it
// can end up with no warden.
func TestAnchorShapeStartsNoConversion(t *testing.T) {
	p := testPaths()
	f := newFakeCutover()
	f.files["__ppid_exe__"] = p.anchorPath
	passingPreflight(t, f, p)
	restore := swapCutoverOps(t, f)
	defer restore()

	maybeStartAnchorCutover(p, 90, func(string, ...any) {})
	if len(f.spawned) != 0 {
		t.Fatalf("an already-converted machine started a second conversion: %v", f.spawned)
	}
	// Nothing may be touched at all — not even the lock. A converted machine that
	// leaves a lockfile behind blocks the retry of a machine that genuinely needs
	// one only if they share a disk, but it also means this path did work it had
	// no business doing.
	if len(f.locked) != 0 {
		t.Fatalf("an already-converted machine took the conversion lock: %v", f.locked)
	}
}

// The sentinel is the only thing standing between a machine that rejected the
// conversion and an every-start boot-loop through the same failure.
func TestRolledBackMachineIsNotRetried(t *testing.T) {
	p := testPaths()
	f := newFakeCutover()
	f.files["__ppid_exe__"] = "/sbin/launchd"
	f.files[p.root+"/warden/"+cutoverFailedName] = "previous attempt rolled back"
	passingPreflight(t, f, p)
	restore := swapCutoverOps(t, f)
	defer restore()

	maybeStartAnchorCutover(p, 1, func(string, ...any) {})
	if len(f.spawned) != 0 {
		t.Fatalf("converter started despite the failure sentinel: %v", f.spawned)
	}
}

// Two wardens must not convert at once. The second one finds the lock held.
func TestHeldLockStartsNoSecondConversion(t *testing.T) {
	p := testPaths()
	f := newFakeCutover()
	f.files["__ppid_exe__"] = "/sbin/launchd"
	f.locked[p.root+"/warden/"+cutoverLockName] = true
	f.modTimes[p.root+"/warden/"+cutoverLockName] = time.Now()
	passingPreflight(t, f, p)
	restore := swapCutoverOps(t, f)
	defer restore()

	maybeStartAnchorCutover(p, 1, func(string, ...any) {})
	if len(f.spawned) != 0 {
		t.Fatalf("a second converter was started while the lock was held: %v", f.spawned)
	}
}

func TestSuccessfulInstallLeavesTheBackupAndNoSentinel(t *testing.T) {
	p := testPaths()
	f := newFakeCutover()
	f.files[p.plistPath] = legacyPlist

	if rc := runCutover(f.ops(), p, p.binPath, func(string, ...any) {}); rc != 0 {
		t.Fatalf("runCutover = %d, want 0", rc)
	}
	if got := f.files[p.plistPath+plistPrevSuffix]; got != legacyPlist {
		t.Fatalf("backup = %q, want the pre-conversion plist", got)
	}
	if _, ok := f.files[p.root+"/warden/"+cutoverFailedName]; ok {
		t.Fatal("a successful conversion must not write the failure sentinel")
	}
}

// The four post-writePlist failure paths. The original design rolled back only on
// verify; lint/bootstrap/kickstart are exactly the cases it could not see, and a
// lint failure is the worst of them (nothing looks wrong until the next reboot).
func TestEveryPostWritePlistFailureRollsBackToTheOldShape(t *testing.T) {
	p := testPaths()
	installArgv := p.binPath + " install --force"
	for _, tc := range []struct {
		name  string
		cause string
	}{
		{"plutil lint rejects the rendered plist", "plutil -lint failed"},
		{"launchctl bootstrap fails", "Bootstrap failed: 5"},
		{"launchctl kickstart fails", "kickstart failed"},
		{"verify sees a crash loop", "CRASH-LOOPING"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFakeCutover()
			f.files[p.plistPath] = legacyPlist
			f.runErr[installArgv] = errors.New(tc.cause)
			// The installer got far enough to overwrite the plist — that is the
			// state every one of these failures leaves behind.
			f.files[p.plistPath] = "<plist>ANCHOR</plist>"
			f.files[p.plistPath+plistPrevSuffix] = ""
			f.files[p.plistPath] = legacyPlist

			ops := f.ops()
			// Simulate install having overwritten the plist before it failed.
			// The hook has to sit on the runner the install call ACTUALLY uses;
			// hooking the other one leaves the plist untouched and every
			// assertion below then holds without the rollback ever running.
			// `overwritten` is what makes that mistake fail instead of pass —
			// a comment cannot, and this test was silently vacuous for exactly
			// one commit before the flag existed.
			overwritten := false
			origRun := ops.runInstaller
			ops.runInstaller = func(name string, args ...string) (string, error) {
				key := name
				for _, a := range args {
					key += " " + a
				}
				if key == installArgv {
					f.files[p.plistPath] = "<plist>ANCHOR</plist>"
					overwritten = true
				}
				return origRun(name, args...)
			}

			if rc := runCutover(ops, p, p.binPath, func(string, ...any) {}); rc != 0 {
				t.Fatalf("runCutover = %d, want 0 (a successful rollback is the contracted outcome)", rc)
			}
			if !overwritten {
				t.Fatal("the overwrite hook never fired — this test is not exercising a rollback at all (it is hooked to the wrong runner)")
			}
			// The assertion is the CONTENT, not the existence of a .prev file: a
			// backup that is never put back is not a rollback.
			if got := f.files[p.plistPath]; got != legacyPlist {
				t.Fatalf("plist after rollback = %q, want the pre-conversion shape %q", got, legacyPlist)
			}
			assertCalled(t, f.calls, "launchctl bootstrap "+p.guiDomain+" "+p.plistPath)
			if _, ok := f.files[p.root+"/warden/"+cutoverFailedName]; !ok {
				t.Fatal("a rolled-back machine must carry the sentinel, or it converts again on the next start")
			}
		})
	}
}

// A rollback that cannot bring the old shape back is the ONE state a human has to
// be told about, so it is the only non-zero exit.
func TestFailedRollbackReportsNonZero(t *testing.T) {
	p := testPaths()
	f := newFakeCutover()
	f.files[p.plistPath] = legacyPlist
	f.runErr[p.binPath+" install --force"] = errors.New("bootstrap failed")
	f.runErr["launchctl bootstrap "+p.guiDomain+" "+p.plistPath] = errors.New("Bootstrap failed: 5")

	ops := f.ops()
	overwritten := false
	origRun := ops.runInstaller
	ops.runInstaller = func(name string, args ...string) (string, error) {
		f.files[p.plistPath] = "<plist>ANCHOR</plist>"
		overwritten = true
		return origRun(name, args...)
	}

	defer func() {
		if !overwritten {
			t.Error("the overwrite hook never fired — nothing was rolled back, so this test proves nothing")
		}
	}()
	if rc := runCutover(ops, p, p.binPath, func(string, ...any) {}); rc == 0 {
		t.Fatal("a rollback that did not restore a live warden must exit non-zero")
	}
	if _, ok := f.files[p.root+"/warden/"+cutoverFailedName]; !ok {
		t.Fatal("a failed rollback must still block further attempts")
	}
}

// Without a readable current plist there is nothing to roll back TO, so the
// conversion must refuse rather than proceed blind.
func TestMissingCurrentPlistAbortsBeforeAnyInstall(t *testing.T) {
	p := testPaths()
	f := newFakeCutover()

	if rc := runCutover(f.ops(), p, p.binPath, func(string, ...any) {}); rc == 0 {
		t.Fatal("want a refusal when the current plist cannot be read")
	}
	assertNotCalled(t, f.calls, p.binPath+" install --force")
}

func TestAnchorPreflight(t *testing.T) {
	p := testPaths()
	for _, tc := range []struct {
		name    string
		code    int
		runErr  error
		wantErr bool
	}{
		{name: "anchor rejects the probe argument with exit 2", code: 2},
		{name: "anchor is missing or not executable", runErr: errors.New("no such file"), wantErr: true},
		{name: "anchor exits 0 for an argument it must reject", code: 0, wantErr: true},
		{name: "anchor dies on some other code", code: 126, wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFakeCutover()
			key := p.anchorPath + " --preflight"
			f.exitCodes[key] = tc.code
			if tc.runErr != nil {
				f.exitErrs[key] = tc.runErr
			}
			err := anchorPreflight(f.ops(), p.anchorPath)
			if (err != nil) != tc.wantErr {
				t.Fatalf("anchorPreflight err = %v, wantErr = %v", err, tc.wantErr)
			}
		})
	}
}

// A failed preflight means the anchor cannot be launchd's job leader, so nothing
// may be converted — and nothing may be spawned either.
//
// The machine is given a REAL anchor here (a quarantined one, which is what an
// unexecutable-but-present anchor models). Without it the run refuses at the
// anchor gate instead and this test would pass without the preflight existing at
// all — hence the assertCalled below, which is the only thing proving the refusal
// came from the gate this test is named after.
func TestPreflightFailureStartsNoConversion(t *testing.T) {
	p := testPaths()
	f := newFakeCutover()
	f.files["__ppid_exe__"] = "/sbin/launchd"
	anchorAlreadyOnDisk(f, p, operatorAnchorBytes)
	f.exitErrs[p.anchorPath+" --preflight"] = errors.New("cannot execute")
	restore := swapCutoverOps(t, f)
	defer restore()

	maybeStartAnchorCutover(p, 1, func(string, ...any) {})
	assertCalled(t, f.calls, p.anchorPath+" --preflight")
	if len(f.spawned) != 0 {
		t.Fatalf("converter started despite a failed preflight: %v", f.spawned)
	}
}

// 🔴 THE POPULATION TEST. This is the machine the whole migration exists for: a
// legacy plist and NO anchor file, because it was installed before the anchor
// shape shipped and self-update never deploys one.
//
// It is asserted as an OUTCOME (the converter actually started), not as "the gate
// returned true": the shipped bug was precisely a gate that reported a truthful
// refusal forever, so a test that stops at the gate's own verdict would have
// passed against the broken build too.
//
// Remove the ensureAnchorPresent call from maybeStartAnchorCutover and this goes
// red — verified, not assumed.
func TestMachineWithNoAnchorFileStillConverts(t *testing.T) {
	p := testPaths()
	f := newFakeCutover()
	f.files["__ppid_exe__"] = "/sbin/launchd"
	withEmbeddedAnchor(t, buildAnchorBytes)
	f.exitCodes[p.anchorPath+" --preflight"] = 2
	restore := swapCutoverOps(t, f)
	defer restore()

	// Precondition, stated rather than assumed: nothing is on this machine yet.
	if _, ok := f.files[p.anchorPath]; ok {
		t.Fatal("fixture error: this test must start from a machine with NO anchor")
	}

	maybeStartAnchorCutover(p, 1, func(string, ...any) {})

	want := p.binPath + " " + cutoverSubcmd
	if len(f.spawned) != 1 || f.spawned[0] != want {
		t.Fatalf("spawned = %v, want exactly [%q] — an anchorless machine must still convert", f.spawned, want)
	}
	if got := f.files[p.anchorPath]; got != buildAnchorBytes {
		t.Fatalf("anchor on disk = %q, want the embedded copy %q", got, buildAnchorBytes)
	}
	// Executable regardless of the launchd job's umask; a 0644 anchor cannot be a
	// job leader and would fail the preflight for reasons unrelated to its bytes.
	assertCalled(t, f.calls, "chmod "+p.anchorPath)
}

// The other side of the gate: with no anchor on disk, no usable source and no
// embedded copy, there is nothing to prove anything about — so the conversion
// must still refuse. Making the anchorless machine convertible must not turn into
// "convert on faith".
func TestNoObtainableAnchorStartsNoConversion(t *testing.T) {
	p := testPaths()
	f := newFakeCutover()
	f.files["__ppid_exe__"] = "/sbin/launchd"
	// Deliberately NO withEmbeddedAnchor: the test binary's anchordist holds only
	// .gitkeep, so this is a genuinely empty embedded copy.
	f.exitCodes[p.anchorPath+" --preflight"] = 2
	restore := swapCutoverOps(t, f)
	defer restore()

	maybeStartAnchorCutover(p, 1, func(string, ...any) {})
	if len(f.spawned) != 0 {
		t.Fatalf("converter started with no anchor obtainable anywhere: %v", f.spawned)
	}
	if _, ok := f.files[p.anchorPath]; ok {
		t.Fatal("an empty anchor source must never be written to disk")
	}
}

// TCC identifies the anchor by its BYTES, and installFixedAnchor preserves
// whatever it finds rather than replacing it. Rewriting an existing anchor would
// change the inode and therefore the machine's identity — the exact thing the
// anchor shape exists to keep stable.
func TestExistingAnchorIsNeverReplaced(t *testing.T) {
	p := testPaths()
	f := newFakeCutover()
	f.files["__ppid_exe__"] = "/sbin/launchd"
	anchorAlreadyOnDisk(f, p, operatorAnchorBytes)
	withEmbeddedAnchor(t, buildAnchorBytes)
	f.exitCodes[p.anchorPath+" --preflight"] = 2
	restore := swapCutoverOps(t, f)
	defer restore()

	maybeStartAnchorCutover(p, 1, func(string, ...any) {})
	if got := f.files[p.anchorPath]; got != operatorAnchorBytes {
		t.Fatalf("anchor on disk = %q, want the pre-existing %q — the machine's TCC identity was rewritten", got, operatorAnchorBytes)
	}
	// 🔴 The load-bearing assertion is NO WRITE, not "same content". In a home
	// install anchorSrc IS anchorPath, so a version that dropped the exists-check
	// would read the machine's own anchor and write those exact bytes back — byte
	// comparison stays green while the inode, and therefore the TCC identity,
	// changes underneath it. Verified: with the exists-check removed, the content
	// assertion above still passes and only this one goes red.
	assertNotCalled(t, f.calls, "write "+p.anchorPath)
	if len(f.spawned) != 1 {
		t.Fatalf("spawned = %v, want the conversion to proceed on an already-anchored machine", f.spawned)
	}
}

// An anchor THIS RUN wrote that then fails the preflight must be taken back off
// the machine. installFixedAnchor never replaces an anchor it finds, so a broken
// copy left behind here would be adopted permanently as this machine's identity —
// the failure would outlive the run that caused it.
func TestUnusableAnchorWrittenByThisRunIsRemoved(t *testing.T) {
	p := testPaths()
	f := newFakeCutover()
	f.files["__ppid_exe__"] = "/sbin/launchd"
	withEmbeddedAnchor(t, buildAnchorBytes)
	f.exitErrs[p.anchorPath+" --preflight"] = errors.New("killed by Gatekeeper")
	restore := swapCutoverOps(t, f)
	defer restore()

	maybeStartAnchorCutover(p, 1, func(string, ...any) {})
	if len(f.spawned) != 0 {
		t.Fatalf("converter started despite an anchor that cannot execute: %v", f.spawned)
	}
	if body, ok := f.files[p.anchorPath]; ok {
		t.Fatalf("the unusable anchor this run wrote was left on the machine (%q); install would preserve it forever", body)
	}
}

// The mirror of the case above, and the reason the cleanup is conditional: an
// anchor we did NOT write is the machine's existing identity. Deleting it because
// a probe failed would destroy the very thing the anchor shape protects, and
// would hand the next install a clean slate to mint a NEW identity on.
func TestPreexistingAnchorSurvivesAFailedPreflight(t *testing.T) {
	p := testPaths()
	f := newFakeCutover()
	f.files["__ppid_exe__"] = "/sbin/launchd"
	anchorAlreadyOnDisk(f, p, operatorAnchorBytes)
	f.exitErrs[p.anchorPath+" --preflight"] = errors.New("killed by Gatekeeper")
	restore := swapCutoverOps(t, f)
	defer restore()

	maybeStartAnchorCutover(p, 1, func(string, ...any) {})
	if got := f.files[p.anchorPath]; got != operatorAnchorBytes {
		t.Fatalf("anchor on disk = %q, want the untouched pre-existing %q", got, operatorAnchorBytes)
	}
	if len(f.spawned) != 0 {
		t.Fatalf("converter started despite a failed preflight: %v", f.spawned)
	}
}

// A machine that already rejected the conversion must be touched by NOTHING —
// including the anchor gate. The sentinel check has to stay in front of the
// materialisation, or every start of a rolled-back machine writes a file.
func TestRolledBackMachineIsNotEvenGivenAnAnchor(t *testing.T) {
	p := testPaths()
	f := newFakeCutover()
	f.files["__ppid_exe__"] = "/sbin/launchd"
	f.files[p.root+"/warden/"+cutoverFailedName] = "previous attempt rolled back"
	withEmbeddedAnchor(t, buildAnchorBytes)
	f.exitCodes[p.anchorPath+" --preflight"] = 2
	restore := swapCutoverOps(t, f)
	defer restore()

	maybeStartAnchorCutover(p, 1, func(string, ...any) {})
	if _, ok := f.files[p.anchorPath]; ok {
		t.Fatal("a machine carrying the failure sentinel had an anchor written to it")
	}
	if len(f.spawned) != 0 {
		t.Fatalf("converter started despite the failure sentinel: %v", f.spawned)
	}
}

func TestLegacyShapeStartsTheDetachedConverter(t *testing.T) {
	p := testPaths()
	f := newFakeCutover()
	f.files["__ppid_exe__"] = "/sbin/launchd"
	passingPreflight(t, f, p)
	restore := swapCutoverOps(t, f)
	defer restore()

	maybeStartAnchorCutover(p, 1, func(string, ...any) {})
	want := p.binPath + " " + cutoverSubcmd
	if len(f.spawned) != 1 || f.spawned[0] != want {
		t.Fatalf("spawned = %v, want exactly [%q]", f.spawned, want)
	}
}

func swapCutoverOps(t *testing.T, f *fakeCutover) func() {
	t.Helper()
	prev := newCutoverOps
	ops := f.ops()
	newCutoverOps = func() cutoverOps { return ops }
	return func() { newCutoverOps = prev }
}

func assertCalled(t *testing.T, calls []string, want string) {
	t.Helper()
	for _, c := range calls {
		if c == want {
			return
		}
	}
	t.Fatalf("expected call %q; got %v", want, calls)
}

func assertNotCalled(t *testing.T, calls []string, unwanted string) {
	t.Helper()
	for _, c := range calls {
		if c == unwanted {
			t.Fatalf("call %q must not have happened; got %v", unwanted, calls)
		}
	}
}

// The installer must NOT run on the short probe budget. This is not a tuning
// preference: runInstall's health verify alone is up to 36s (30x1s to see a pid,
// then 6x1s of stability), so a 30s budget kills a HEALTHY conversion, the kill
// reads as an install failure, and the machine both rolls back and writes the
// never-retried cutover.failed sentinel. A machine that was merely slow would
// then be indistinguishable from one that rejected the conversion — permanently.
func TestInstallDoesNotRunOnTheProbeBudget(t *testing.T) {
	p := testPaths()
	f := newFakeCutover()
	f.files[p.plistPath] = legacyPlist

	ops := f.ops()
	ops.run = func(name string, args ...string) (string, error) {
		if len(args) > 0 && args[0] == "install" {
			t.Fatalf("install went through the probe-budget runner (%v); it must use runInstaller", args)
		}
		return "", nil
	}
	installed := 0
	ops.runInstaller = func(name string, args ...string) (string, error) {
		installed++
		return "", nil
	}

	if rc := runCutover(ops, p, p.binPath, func(string, ...any) {}); rc != 0 {
		t.Fatalf("runCutover = %d, want 0", rc)
	}
	if installed != 1 {
		t.Fatalf("runInstaller called %d times, want 1", installed)
	}
}

// The budget itself has to clear the worst case it exists to cover. 36s is
// runInstall's verify alone; the conversion also downloads ocagent first.
func TestInstallBudgetClearsTheInstallerWorstCase(t *testing.T) {
	const verifyWorstCase = 36 * time.Second
	if cutoverInstallBudget <= verifyWorstCase {
		t.Fatalf("cutoverInstallBudget = %v, must exceed the %v verify alone (plus an ocagent download)", cutoverInstallBudget, verifyWorstCase)
	}
}

// DIRECTION ①: the installer died before it modified anything (the field case is
// an offline machine failing the ocagent download). Nothing can boot-loop, so the
// permanent sentinel must NOT be written — otherwise a network blip permanently
// excludes that machine from the migration.
func TestFailureBeforeAnythingIsModifiedLeavesNoSentinel(t *testing.T) {
	p := testPaths()
	f := newFakeCutover()
	f.files[p.plistPath] = legacyPlist
	// The installer fails at the download step: the plist is never overwritten.
	f.runErr[p.binPath+" install --force"] = errors.New("download ocagent: dial tcp: no route to host")

	if rc := runCutover(f.ops(), p, p.binPath, func(string, ...any) {}); rc != 0 {
		t.Fatalf("runCutover = %d, want 0 (an untouched machine is not a failure)", rc)
	}
	if _, ok := f.files[p.root+"/warden/"+cutoverFailedName]; ok {
		t.Fatal("a machine that was never modified must not be marked as having rejected the conversion")
	}
	// Rolling back an untouched machine would boot out a healthy warden for no
	// reason — the one window where a machine can end up with no warden at all.
	assertNotCalled(t, f.calls, "launchctl bootout "+p.guiDomain+"/"+p.labelOrDefault())
}

// DIRECTION ②: the installer got far enough to replace the plist. Here the
// sentinel IS required — without it the machine detects "legacy" on its next
// start and converts again, forever.
func TestFailureAfterThePlistWasReplacedLeavesTheSentinel(t *testing.T) {
	p := testPaths()
	installArgv := p.binPath + " install --force"
	f := newFakeCutover()
	f.files[p.plistPath] = legacyPlist
	f.runErr[installArgv] = errors.New("Bootstrap failed: 5")

	ops := f.ops()
	origRun := ops.runInstaller
	ops.runInstaller = func(name string, args ...string) (string, error) {
		f.files[p.plistPath] = "<plist>ANCHOR</plist>"
		return origRun(name, args...)
	}

	if rc := runCutover(ops, p, p.binPath, func(string, ...any) {}); rc != 0 {
		t.Fatalf("runCutover = %d, want 0", rc)
	}
	if _, ok := f.files[p.root+"/warden/"+cutoverFailedName]; !ok {
		t.Fatal("a machine whose plist was replaced must carry the sentinel, or it converts again on every start")
	}
}

// The consequence the user actually feels, asserted end-to-end rather than as an
// internal file: after a transient failure the machine is STILL CONVERTIBLE. A
// test that stops at "no sentinel was written" would still pass if some other
// gate had quietly excluded the machine.
func TestTransientFailureLeavesTheMachineStillConvertible(t *testing.T) {
	p := testPaths()
	f := newFakeCutover()
	f.files[p.plistPath] = legacyPlist
	f.runErr[p.binPath+" install --force"] = errors.New("download ocagent: dial tcp: no route to host")

	if rc := runCutover(f.ops(), p, p.binPath, func(string, ...any) {}); rc != 0 {
		t.Fatalf("runCutover = %d, want 0", rc)
	}

	// Next warden start, same machine, network is back.
	f.files["__ppid_exe__"] = "/sbin/launchd"
	passingPreflight(t, f, p)
	delete(f.files, p.root+"/warden/"+cutoverLockName)
	f.locked = map[string]bool{}
	restore := swapCutoverOps(t, f)
	defer restore()

	maybeStartAnchorCutover(p, 1, func(string, ...any) {})
	if len(f.spawned) == 0 {
		t.Fatal("a machine that merely failed to reach the network must still be convertible on the next start")
	}
}

// ───────────────────────────────────────────────────────────────────────────
// THE CROSS-MODULE CONTRACT: anchorPreflight's "exit 2" is the ANCHOR's answer
// ───────────────────────────────────────────────────────────────────────────
//
// anchorPreflight refuses to convert unless `officraft --preflight` exits
// EXACTLY 2. That number is produced in a different Go module (cli/officraft),
// with no import between them and no compiler to notice if the two sides part
// company. The failure it hides is invisible and fleet-wide: add a flag to the
// anchor, `--preflight` stops meaning "reject the argument", every machine's
// preflight fails, every machine silently skips the migration FOREVER, and
// nothing anywhere goes red — the refusal is fail-closed, so there is no
// symptom other than a migration that never happens.
//
// WHY THIS TEST BUILDS AND RUNS THE REAL ANCHOR instead of asserting `2`
// ---------------------------------------------------------------------
// Because two assertions against the same number are not a contract. A fixture
// table (the namespace-axes.tsv pattern) would be one source, but each side
// still asserts against a COPY of the value, and the anchor could grow a flag
// that changes what `--preflight` MEANS while the exit code stays 2. So this
// compares the two ACTUAL values: the real anchor, built from source at test
// time, is fed to the real anchorPreflight. There is no literal on either side
// of the comparison — the anchor binary IS the source of truth, and drift is
// whatever makes the real preflight stop succeeding.
//
// It runs a process, and that is a deliberate, bounded exception to this
// package's "tests never exec" posture: the binary is freshly built into
// t.TempDir(), the argv is the anchor's ZERO-SIDE-EFFECT usage path (it prints
// usage and forks nothing — that property is exactly what is being verified),
// and nothing here touches launchd, HOME, the plist or the real seam. It does
// NOT call realRunExit; it supplies its own runner, which is the same rule every
// other test in this package follows.

// goTool locates the toolchain that is already running this test.
func goTool(t *testing.T) string {
	t.Helper()
	if path, err := exec.LookPath("go"); err == nil {
		return path
	}
	path := filepath.Join(runtime.GOROOT(), "bin", "go")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("cannot locate the go toolchain to build the anchor: %v", err)
	}
	return path
}

// buildRealAnchor compiles cli/officraft — the actual TCC identity anchor the
// production plist names — and returns the path to the binary.
func buildRealAnchor(t *testing.T) string {
	t.Helper()
	src := filepath.Join("..", "officraft")
	if _, err := os.Stat(filepath.Join(src, "main.go")); err != nil {
		t.Fatalf("the anchor's source is not where this contract expects it (%s): %v.\n"+
			"anchorPreflight's whole go/no-go decision is the exit code of THAT program; "+
			"if it moved, this contract needs re-pointing, not skipping", src, err)
	}
	out := filepath.Join(t.TempDir(), "officraft")
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, goTool(t), "build", "-o", out, ".")
	cmd.Dir = src
	if combined, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build the real anchor: %v\n%s", err, combined)
	}
	return out
}

// realExitCodeRunner is a runExit that actually runs the command, so
// anchorPreflight is exercised against a real process instead of a script.
func realExitCodeRunner(t *testing.T) func(string, ...string) (int, error) {
	t.Helper()
	return func(name string, args ...string) (int, error) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		err := exec.CommandContext(ctx, name, args...).Run()
		if err == nil {
			return 0, nil
		}
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			return exit.ExitCode(), nil
		}
		return -1, err
	}
}

func TestAnchorPreflightAgreesWithTheRealAnchorBinary(t *testing.T) {
	anchor := buildRealAnchor(t)
	ops := newFakeCutover().ops()
	ops.runExit = realExitCodeRunner(t)

	if err := anchorPreflight(ops, anchor); err != nil {
		t.Fatalf("the REAL anchor no longer satisfies the preflight this fleet gates its "+
			"migration on: %v\n"+
			"Nothing else can catch this. anchorPreflight is fail-closed, so a machine whose "+
			"anchor answers differently does not error — it silently declines to convert, "+
			"forever, with no symptom. Either cli/officraft changed how it answers an "+
			"argument, or cutover.go changed what it demands; the two must be brought back "+
			"together deliberately.", err)
	}

	// NEGATIVE CONTROL. Without it the assertion above would also pass if
	// anchorPreflight accepted anything at all, and the real runner would be
	// unproven too. A stand-in that exits 0 — the shape of an anchor that
	// ACCEPTED the argument instead of rejecting it — must be refused.
	accepting := filepath.Join(t.TempDir(), "accepting-anchor")
	if err := os.WriteFile(accepting, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("stage the negative control: %v", err)
	}
	if err := anchorPreflight(ops, accepting); err == nil {
		t.Fatal("a binary that ACCEPTS the probe argument must fail the preflight — " +
			"otherwise the check above proves only that anchorPreflight returns nil")
	}
}
