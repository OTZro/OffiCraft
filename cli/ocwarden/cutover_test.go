package main

import (
	"errors"
	"fmt"
	"os"
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
		readFile:      func(p string) ([]byte, error) { return nil, block("read " + p) },
		writeFile:     func(p string, _ []byte, _ os.FileMode) error { return block("write " + p) },
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
	return cutoverOps{
		ppidExe: func(int) (string, error) { return f.files["__ppid_exe__"], nil },
		run: func(name string, args ...string) (string, error) {
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
		},
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
			f.files[p] = string(d)
			return nil
		},
		remove: func(p string) error {
			delete(f.files, p)
			delete(f.locked, p)
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
		guiDomain:  "gui/501",
		ocBase:     "http://example.test",
		ocToken:    "tok",
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

// A machine whose shape cannot be established must be left alone. Guessing
// "probably legacy" would convert machines nobody has evidence about.
func TestUnknownShapeStartsNoConversion(t *testing.T) {
	f := newFakeCutover()
	f.files["__ppid_exe__"] = "/bin/zsh"
	restore := swapCutoverOps(t, f)
	defer restore()

	maybeStartAnchorCutover(testPaths(), 4242, func(string, ...any) {})
	if len(f.spawned) != 0 {
		t.Fatalf("converter was started for an unknown shape: %v", f.spawned)
	}
}

// The sentinel is the only thing standing between a machine that rejected the
// conversion and an every-start boot-loop through the same failure.
func TestRolledBackMachineIsNotRetried(t *testing.T) {
	p := testPaths()
	f := newFakeCutover()
	f.files["__ppid_exe__"] = "/sbin/launchd"
	f.files[p.root+"/warden/"+cutoverFailedName] = "previous attempt rolled back"
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
			origRun := ops.run
			ops.run = func(name string, args ...string) (string, error) {
				key := name
				for _, a := range args {
					key += " " + a
				}
				if key == installArgv {
					f.files[p.plistPath] = "<plist>ANCHOR</plist>"
				}
				return origRun(name, args...)
			}

			if rc := runCutover(ops, p, p.binPath, func(string, ...any) {}); rc != 0 {
				t.Fatalf("runCutover = %d, want 0 (a successful rollback is the contracted outcome)", rc)
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

	if rc := runCutover(f.ops(), p, p.binPath, func(string, ...any) {}); rc == 0 {
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
func TestPreflightFailureStartsNoConversion(t *testing.T) {
	p := testPaths()
	f := newFakeCutover()
	f.files["__ppid_exe__"] = "/sbin/launchd"
	f.exitErrs[p.anchorPath+" --preflight"] = errors.New("cannot execute")
	restore := swapCutoverOps(t, f)
	defer restore()

	maybeStartAnchorCutover(p, 1, func(string, ...any) {})
	if len(f.spawned) != 0 {
		t.Fatalf("converter started despite a failed preflight: %v", f.spawned)
	}
}

func TestLegacyShapeStartsTheDetachedConverter(t *testing.T) {
	p := testPaths()
	f := newFakeCutover()
	f.files["__ppid_exe__"] = "/sbin/launchd"
	f.exitCodes[p.anchorPath+" --preflight"] = 2
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
