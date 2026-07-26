// hostseam_test.go — the STRUCTURAL reason a `go test ./cli/ocwarden/...` run can
// never install, teardown, bootout or bootstrap anything on the machine it runs on.
//
// THE DEFECT THIS CLOSES (T-5047 gap 1)
// -------------------------------------
// `ocwarden install` / `ocwarden teardown` already funnelled every side effect
// through the injectable sysOps seam — but they WIRED that seam to the real OS
// themselves (realSysOps() inline, inside installCmd/teardownCmd). The seam was
// therefore only as protective as each individual test's discipline: a test that
// called `realMain([]string{"install"})` — and transport_test.go / namespace_test.go
// already call realMain for `run --once`, so nothing about that is exotic — would
// bootout and bootstrap the LIVE canonical com.officraft.ocwarden job of whoever
// ran the suite. The install tests were safe only because they happened to build
// `installer` by hand with a fake. That is safety by coincidence, and the identical
// shape on the teardown path unloaded this fleet's live warden three times.
//
// THE FIX IS A SEAM, NOT A CHECK
// ------------------------------
// install.go now has ONE production construction point for every real-host effect
// (realHostSeam) reachable only through the package-level var newHostSeam. The
// TestMain below rebinds that var BEFORE any test in this package runs. So:
//
//   - every test, including ones written years from now, gets the fake;
//   - a test does not have to remember to opt in;
//   - and — the claim this actually buys — deleting or breaking a guard IN THE CODE
//     UNDER TEST (installer.guard, the namespace validation, the label derivation)
//     cannot escalate into touching the host, because an entry point that takes its
//     effects from newHostSeam gets the fake no matter what its own logic does.
//
// WHAT THE SEAM DOES **NOT** BUY (learned the hard way, twice)
// -----------------------------------------------------------
// The seam protects entry points that GO THROUGH IT. It cannot protect against an
// entry point that assembles the wiring itself — `sysOps{run: execRunner{…}.Run,
// rename: os.Rename, …}` inline in teardownCmd names neither realSysOps nor
// realHostSeam, so the identifier-based scans stay green and the seam is simply not
// consulted. Independent review built exactly that mutant and the test binary
// issued a real `launchctl bootout gui/<uid>/com.officraft.ocwarden`.
//
// Two things close that, and BOTH are needed:
//   - scanHostSeamSource check (4) pins the STRUCTURE, not just the names: the
//     `sysOps{` and `execRunner{` composite literals may exist in exactly one place
//     each. A hand-assembled seam is now a TestMain refusal before m.Run().
//   - main.go's execRunner.Run opens with refuseInTestBinary. This is the only
//     guard a hand-assembled struct cannot route around, because however the struct
//     was built the subprocess still has to be started there. It fires BEFORE
//     exec.Command, i.e. before the host is touched — not afterwards.
//
// TestHostSeam_StructureIsReported gives the static half a name in `go test -v`
// output. It is a reporting wrapper only; TestMain is the gate.
package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// errHostSeamBlocked is what the test-binary seam returns for the effects that
// have no meaningful in-memory stand-in (exec a candidate binary, HTTP to a
// server). Recorded, never performed.
var errHostSeamBlocked = errors.New("host seam is blocked in the test binary")

// hostSeamFakes records, in order, every seam handed out during the test binary's
// life, so a test can assert on WHAT an entry point tried to do to the host.
var hostSeamFakes []*fakeSys

// hostSeamSeed lets a test pre-populate the in-memory filesystem of the seam an
// entry point is ABOUT to construct (an entry point resolves its own paths, so the
// test cannot reach the fake afterwards). Purely a fixture hook: it can only add
// readable bytes to the fake, never restore a route to the real OS.
var hostSeamSeed func(*fakeSys)

// fakeHostSeam is the test binary's binding of newHostSeam. The mutating half is
// the same in-memory fakeSys the install tests already use (so an entry point runs
// to completion and its intent is fully observable); the exec/network halves refuse.
func fakeHostSeam() hostSeam {
	f := newFakeSys()
	if hostSeamSeed != nil {
		hostSeamSeed(f)
	}
	hostSeamFakes = append(hostSeamFakes, f)
	return hostSeam{
		sys:         f.ops(),
		claudeProbe: func(_, _, _ string) error { return errHostSeamBlocked },
		agentGet: func(_, _ string) getter {
			return func(string) (int, []byte, error) { return 0, nil, errHostSeamBlocked }
		},
		agentProbe: func(string) error { return errHostSeamBlocked },
	}
}

// TestMain is the structural gate: it runs once, before every test in the package,
// and there is no way for an individual test to be reached without passing through
// it. Do NOT add an opt-out.
//
// WHY THE SOURCE SCANS RUN HERE AND NOT AS ORDINARY TESTS
// ------------------------------------------------------
// They used to be plain Test* functions whose own comments claimed they "fail before
// anything executes". That claim was FALSE, and it was falsified by experiment — then
// demonstrated the hard way. Go runs tests in declaration order, file by file in
// sorted filename order: hostseam_test.go sorts before install_test.go, and within
// this very file TestInstallCmd_CannotReachTheRealHost is declared ABOVE both static
// guards. So under the one-line mutant the guards exist to catch (installCmd's
// `newHostSeam()` → `realHostSeam()`, or teardownCmd's `newHostSeam().sys` →
// `realSysOps()`), the run reaches the CannotReachTheRealHost tests — which drive the
// FULL install/teardown at the CANONICAL label on purpose — and constructs the REAL
// seam there. The guards had not run yet and never got the chance.
//
// 🔴 That is not a theoretical ordering argument. During T-5047 verification a mutant
// run on a tree without these gates did exactly this and issued a real
// `launchctl bootout gui/<uid>/com.officraft.ocwarden` against the developer
// machine's LIVE warden: the job was unloaded and had to be re-bootstrapped by hand.
// A detection that speaks after the bootout is not a defence.
//
// TestMain is the one place in a Go package guaranteed to run before any test
// function. Failing here — before m.Run() — is what "before anything executes"
// actually means. Do not move these back out into Test* functions.
func TestMain(m *testing.M) {
	if violations := scanHostSeamSource(); len(violations) > 0 {
		fmt.Fprintf(os.Stderr, "\nFATAL: cli/ocwarden host-seam structure is broken — REFUSING TO RUN ANY TEST.\n"+
			"These checks run in TestMain, before m.Run(), because a test binary in which an entry\n"+
			"point can construct the real seam would act on this machine's LIVE launchd domain the\n"+
			"first time any test drives install or teardown — which the very next tests do, at the\n"+
			"canonical label, on purpose.\n\n")
		for _, v := range violations {
			fmt.Fprintf(os.Stderr, "  - %s\n", v)
		}
		fmt.Fprintln(os.Stderr)
		os.Exit(1)
	}
	newHostSeam = fakeHostSeam
	// The exec runner is rebound for the same reason and at the same moment: the
	// real one now REFUSES inside a test binary (main.go execRunner.Run →
	// refuseInTestBinary), so the telemetry/probe paths that legitimately shell out
	// in production must take their runner from this seam. A test that wants to
	// observe argv still injects its own recording runner locally, exactly as before.
	newCmdRunner = func(time.Duration) CmdRunner { return blockedRunner{} }
	os.Exit(m.Run())
}

// blockedRunner is the test binary's binding of newCmdRunner: it starts no process
// and returns errHostSeamBlocked for every argv. Callers on the telemetry path all
// treat a runner error as "this field is unavailable", so a `run --once` still
// completes end to end without a single subprocess.
type blockedRunner struct{}

func (blockedRunner) Run(name string, args ...string) (string, error) {
	return "", fmt.Errorf("%w: refusing to exec %s", errHostSeamBlocked, name)
}

// scanHostSeamSource is the whole static half of the guarantee, as a pure function
// over this package's non-test sources so TestMain can run it before m.Run(). It
// returns one human-readable violation per problem found, empty when the structure
// holds. Both halves are here because both are preconditions for the test binary
// being safe at all, and neither is safe to discover late:
//
//	(1) SINGLE CONSTRUCTION POINT — realSysOps() may appear exactly once in the
//	    non-test sources (realHostSeam's body). TestMain can only protect what it
//	    can rebind; an inline realSysOps() in an entry point (the pre-T-5047 shape)
//	    is unrebindable.
//	(2) NO DIRECT realHostSeam() CALL — realHostSeam may be MENTIONED as its own
//	    declaration and as newHostSeam's initialiser, and CALLED nowhere. This is
//	    the hole the first version of this file left open: with the mutant in place
//	    realSysOps() still appears exactly once and `var newHostSeam` is still
//	    there, so (1) alone stays green while the entry point is wired straight to
//	    the real OS again.
//	(3) THE RUNTIME BACKSTOP IS STILL WIRED — refuseInTestBinary must still open the
//	    body of the two real-seam constructors AND of execRunner.Run, because (1),
//	    (2) and (4) are source scans and a source scan is exactly what a bad edit
//	    can defeat.
//	(4) NO HAND-ASSEMBLED HOST WIRING — `sysOps{` and `execRunner{` composite
//	    literals may appear in exactly one place each (realSysOps's return, and
//	    newCmdRunner's initialiser). This is the check whose ABSENCE independent
//	    review exploited: (1) and (2) pin two IDENTIFIERS, so a mutant that writes
//	    `sysOps{run: execRunner{…}.Run, rename: os.Rename, …}` inline in teardownCmd
//	    mentions NEITHER name, keeps every scan above green, and reaches the live
//	    launchd domain. (1)/(2) guard the front door of a house with no walls; this
//	    is the wall, and refuseInTestBinary on execRunner.Run (3) is the runtime
//	    proof that even a wall with a hole in it cannot let a test binary exec.
func scanHostSeamSource() []string {
	var out []string
	entries, err := os.ReadDir(".")
	if err != nil {
		return []string{fmt.Sprintf("cannot read package sources to verify the host seam: %v (fail closed)", err)}
	}
	total := 0
	// (4) counters for the two host-wiring composite literals, keyed by the file
	// they are allowed to live in.
	litTotals := map[string]int{"sysOps{": 0, "execRunner{": 0}
	litHome := map[string]string{"sysOps{": "install.go", "execRunner{": "main.go"}
	litWhere := map[string]string{
		"sysOps{":     "install.go's realSysOps (the ONE place the real OS is wired)",
		"execRunner{": "main.go's `var newCmdRunner` initialiser (the ONE place the real exec runner is built)",
	}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		raw, err := os.ReadFile(name)
		if err != nil {
			out = append(out, fmt.Sprintf("cannot read %s to verify the host seam: %v (fail closed)", name, err))
			continue
		}
		src := string(raw)

		// (1) single construction point.
		if n := countRealSysOpsCalls(src); n > 0 {
			if name != "install.go" {
				out = append(out, fmt.Sprintf("%s calls realSysOps() directly (%d time(s)) — the real host must be wired ONLY in install.go's realHostSeam, reachable through newHostSeam, or the test binary cannot rebind it", name, n))
			}
			total += n
		}

		// (2) realHostSeam is never called.
		for i, line := range strings.Split(src, "\n") {
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, "//") {
				continue
			}
			// The two legitimate mentions, neither of which is a call site.
			if strings.HasPrefix(trimmed, "func realHostSeam()") ||
				strings.HasPrefix(trimmed, "var newHostSeam = realHostSeam") {
				continue
			}
			if strings.Contains(trimmed, "realHostSeam()") {
				out = append(out, fmt.Sprintf("%s:%d calls realHostSeam() directly:\n\t\t%s\n\t  Every entry point must go through newHostSeam() — calling realHostSeam bypasses the var TestMain rebinds, so the test binary is wired to the LIVE launchd domain again and no assertion can prevent the damage, only report it afterwards.",
					name, i+1, trimmed))
			}
		}

		// (4) no hand-assembled host wiring anywhere else.
		for lit := range litTotals {
			n := countCodeOccurrences(src, lit)
			if n > 0 && name != litHome[lit] {
				out = append(out, fmt.Sprintf("%s builds a %s composite literal (%d time(s)) — real host wiring may only be assembled in %s. Assembling it by hand is how a mutant reaches launchctl without ever naming realSysOps or realHostSeam, which is exactly how this fleet's live warden was booted out during T-5047 verification.",
					name, lit, n, litWhere[lit]))
			}
			litTotals[lit] += n
		}
	}
	if total != 1 {
		out = append(out, fmt.Sprintf("realSysOps() appears %d time(s) across the non-test sources, want exactly 1 (realHostSeam)", total))
	}
	for lit, n := range litTotals {
		if n != 1 {
			out = append(out, fmt.Sprintf("%s composite literals appear %d time(s) across the non-test sources, want exactly 1 (%s)", lit, n, litWhere[lit]))
		}
	}
	raw, err := os.ReadFile("install.go")
	if err != nil {
		return append(out, fmt.Sprintf("cannot read install.go to verify the host seam: %v (fail closed)", err))
	}
	if !strings.Contains(string(raw), "var newHostSeam = realHostSeam") {
		out = append(out, "install.go must keep `var newHostSeam = realHostSeam` — a func (not a var) cannot be rebound by TestMain and the whole structural guarantee evaporates")
	}
	// (3) the runtime backstop must stay wired at BOTH real-seam entry points.
	for _, fn := range []string{"func realSysOps() sysOps {\n\trefuseInTestBinary(", "func realHostSeam() hostSeam {\n\trefuseInTestBinary("} {
		if !strings.Contains(string(raw), fn) {
			out = append(out, fmt.Sprintf("install.go lost the runtime backstop: expected the body of %q to open with refuseInTestBinary(...) — that call is what makes constructing the real seam under `go test` impossible rather than merely detectable",
				strings.SplitN(fn, " {", 2)[0]))
		}
	}
	// (3, cont.) …and at the PROCESS choke point, which is the only guard a
	// hand-assembled sysOps cannot route around: whatever built the struct, the
	// subprocess still has to be started in execRunner.Run.
	mainRaw, err := os.ReadFile("main.go")
	if err != nil {
		return append(out, fmt.Sprintf("cannot read main.go to verify the exec choke point: %v (fail closed)", err))
	}
	if !strings.Contains(string(mainRaw), "func (r execRunner) Run(name string, args ...string) (string, error) {\n\trefuseInTestBinary(") {
		out = append(out, "main.go lost the exec choke point: execRunner.Run's body must OPEN with refuseInTestBinary(...). The two identifier scans above can be defeated by an inline `sysOps{run: execRunner{…}.Run, …}` literal (independent review did exactly that, and the test binary issued a real launchctl bootout against this machine's live warden). The refusal on the exec syscall is what turns that from after-the-fact detection into prevention")
	}
	if !strings.Contains(string(mainRaw), "var newCmdRunner = func(timeout time.Duration) CmdRunner { return execRunner{timeout: timeout} }") {
		out = append(out, "main.go must keep `var newCmdRunner` as the single rebindable construction point for the real exec runner — production code that needs a real subprocess takes it from there, and TestMain rebinds it so the refusal above is never hit by legitimate test traffic")
	}
	return out
}

// countCodeOccurrences counts occurrences of lit on CODE lines only. The comments
// in this package discuss `sysOps{` and `execRunner{` at length on purpose (this
// very function's doc comment does), so a scan that counted comment text would be
// impossible to keep green while documenting itself — the pattern-(a) always-true
// assertion this codebase has now been bitten by twice.
func countCodeOccurrences(src, lit string) int {
	n := 0
	for _, line := range strings.Split(src, "\n") {
		t := strings.TrimSpace(line)
		if strings.HasPrefix(t, "//") {
			continue
		}
		n += strings.Count(t, lit)
	}
	return n
}

// lastHostSeam returns the fake handed to the most recent entry-point call.
func lastHostSeam(t *testing.T) *fakeSys {
	t.Helper()
	if len(hostSeamFakes) == 0 {
		t.Fatal("no host seam was constructed — the entry point never asked for one, so this test proves nothing")
	}
	return hostSeamFakes[len(hostSeamFakes)-1]
}

// TestInstallCmd_CannotReachTheRealHost drives the FULL CLI dispatch
// (`realMain{"install","--force"}`) at the CANONICAL instance — no OC_NAMESPACE, so
// the launchd label is exactly com.officraft.ocwarden, the live production warden's
// label — and proves the run is entirely absorbed by the injected seam.
//
// --force is deliberate: it DISABLES installer.guard, i.e. this is the run with the
// guard already out of the way. The assertions below are therefore about the seam
// and nothing else, which is the point — "we added a check" would not survive here.
//
// HOME is a t.TempDir() so that the filesystem blast radius is zero even if this
// test itself is wrong; the LAUNCHD LABEL is NOT sandboxed, because a launchd label
// is a singleton in the uid's gui domain and does not follow HOME. The label under
// test is the live one, and the recorded bootout target proves it.
func TestInstallCmd_CannotReachTheRealHost(t *testing.T) {
	hostSeamFakes = nil
	home := t.TempDir()
	agentSrc := filepath.Join(home, "ocagent-src")
	selfExe, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable: %v", err)
	}
	if resolved, rerr := filepath.EvalSymlinks(selfExe); rerr == nil {
		selfExe = resolved
	}
	// Seed the two files install READS (its own binary as the copy source, and the
	// OC_AGENT_BIN override) INSIDE the fake, so the run proceeds all the way to
	// launchctl. Note what this proves on its own: even reading the test binary
	// off disk goes through the seam.
	hostSeamSeed = func(f *fakeSys) {
		f.existing[selfExe] = []byte("ocwarden-bytes")
		f.existing[agentSrc] = []byte("ocagent-bytes")
	}
	defer func() { hostSeamSeed = nil }()
	// A real (harmless) executable inside the sandbox, so the install-time runtime
	// resolution (resolveClaudeBin → isExecutableFile, a read-only real-OS probe
	// that deliberately stays outside the mutating seam) finds a candidate and the
	// run reaches launchctl. The seam still refuses to EXEC it (claudeProbe).
	claudeStub := filepath.Join(home, "claude")
	if werr := os.WriteFile(claudeStub, []byte("#!/bin/sh\nexit 0\n"), 0o755); werr != nil {
		t.Fatalf("write claude stub: %v", werr)
	}
	var out strings.Builder

	rc := realMain([]string{"install", "--force"}, envFn(map[string]string{
		"HOME":          home,
		"OC_BASE":       "http://127.0.0.1:7755",
		"OC_TOKEN":      fakeJWT("m-canonical"),
		"OC_AGENT_BIN":  agentSrc,
		"OC_CLAUDE_BIN": claudeStub,
	}), &out)

	f := lastHostSeam(t)

	// 1. This really was the dangerous path: the run addressed the LIVE canonical
	//    label in the real gui domain. If this assertion ever fails the test has
	//    stopped covering the thing it exists for.
	wantTarget := "gui/" + itoa(os.Getuid()) + "/" + wardenLabel
	var bootedOut bool
	for _, r := range f.runs {
		if r.name == "launchctl" && len(r.args) >= 2 && r.args[0] == "bootout" && r.args[1] == wantTarget {
			bootedOut = true
		}
	}
	if !bootedOut {
		t.Fatalf("install did not even attempt `launchctl bootout %s` (rc=%d) — this test no longer exercises the canonical-instance path:\nruns=%v\nout=%s",
			wantTarget, rc, f.runs, out.String())
	}

	// 2. …and every one of those attempts landed in the fake. Only launchctl/plutil
	//    are legitimate subprocess names at all, and none of them ran for real.
	assertNoForbiddenProcessKill(t, f)

	// 3. Nothing was written outside the sandbox: every path the run wrote or
	//    renamed is under the temp HOME, and the real one is untouched.
	realHome := os.Getenv("HOME")
	for p := range f.writes {
		if realHome != "" && strings.HasPrefix(p, realHome+string(os.PathSeparator)) {
			t.Fatalf("install addressed a path under the REAL home: %s", p)
		}
	}
	if realHome != "" {
		if _, err := os.Stat(filepath.Join(realHome, ".officraft", "warden", "exec-warden.tok.tmp")); err == nil {
			t.Fatalf("install left a real artifact under %s/.officraft/warden — the seam leaked", realHome)
		}
	}

	// 4. POSITIVE marker (lesson: a sentinel that only speaks when it fails cannot
	//    be told apart from a broken one). The plist the run RENDERED is observable
	//    in the fake, which is only possible because the fake absorbed the write.
	plist := filepath.Join(home, "Library", "LaunchAgents", wardenLabel+".plist")
	if _, ok := f.writes[plist]; !ok {
		if _, ok := f.writes[plist+".tmp"]; !ok {
			t.Errorf("expected the rendered plist to be captured by the fake at %s; writes=%v", plist, keysOf(f.writes))
		}
	}
}

// TestTeardownCmd_CannotReachTheRealHost is the same proof for the inverse verb —
// the one that actually cost this fleet three live-warden unloads. `ocwarden
// teardown` takes NO flags and NO identity: whatever HOME says, the label it boots
// out is the canonical singleton. Structurally absorbed, exactly like install.
func TestTeardownCmd_CannotReachTheRealHost(t *testing.T) {
	hostSeamFakes = nil
	home := t.TempDir()
	var out strings.Builder

	realMain([]string{"teardown"}, envFn(map[string]string{"HOME": home}), &out)

	f := lastHostSeam(t)
	wantTarget := "gui/" + itoa(os.Getuid()) + "/" + wardenLabel
	var bootedOut bool
	for _, r := range f.runs {
		if r.name == "launchctl" && len(r.args) >= 2 && r.args[0] == "bootout" && r.args[1] == wantTarget {
			bootedOut = true
		}
	}
	if !bootedOut {
		t.Fatalf("teardown did not attempt `launchctl bootout %s` — the test no longer covers the canonical path:\nruns=%v\nout=%s",
			wantTarget, f.runs, out.String())
	}
	assertNoForbiddenProcessKill(t, f)
}

// TestHostSeam_StructureIsReported is the single named REPORTING WRAPPER over
// scanHostSeamSource. It is NOT the enforcement point and it is NOT coverage: the
// gate is TestMain, which runs the same scan before m.Run() and os.Exit(1)s on any
// violation, so by the time this function is reachable the scan is known empty and
// the loop body below never executes. It exists only so the property has a NAME in
// `go test -v` output and in cli/CLAUDE.md, and so something still speaks if
// TestMain's gate is ever weakened or removed.
//
// ⚠️ DO NOT ADD A SECOND ONE. There used to be two functions here —
// TestHostSeam_SingleConstructionPoint and
// TestHostSeam_RealHostSeamIsNeverCalledDirectly — with BYTE-IDENTICAL bodies,
// both looping over the same already-empty slice. Independent review replaced both
// loop bodies with panic(), ran the whole package, and got `ok` with both tests
// reported as PASS: two green lines in the log, zero executed statements, and a
// reader could reasonably have counted them as two independent checks. Each
// property enforced by the scan is documented in scanHostSeamSource's own doc
// comment (1)–(4); that is where a new property goes, not into a new no-op test.
func TestHostSeam_StructureIsReported(t *testing.T) {
	for _, v := range scanHostSeamSource() {
		t.Errorf("host seam structure violated: %s", v)
	}
}

// countRealSysOpsCalls counts CALL SITES of realSysOps() in Go source: comment
// lines (which discuss the seam at length, on purpose) and the function's own
// declaration are not call sites and must not be counted, or the guard would be
// impossible to keep green while documenting itself.
func countRealSysOpsCalls(src string) int {
	n := 0
	for _, line := range strings.Split(src, "\n") {
		t := strings.TrimSpace(line)
		if strings.HasPrefix(t, "//") || strings.HasPrefix(t, "func realSysOps()") {
			continue
		}
		n += strings.Count(t, "realSysOps()")
	}
	return n
}

// itoa avoids pulling strconv into this file's import list for one call.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	if neg {
		return "-" + string(b)
	}
	return string(b)
}

func keysOf(m map[string][]byte) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
