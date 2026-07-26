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
//   - and — the whole claim — deleting or breaking a guard IN THE CODE UNDER TEST
//     (installer.guard, the namespace validation, the label derivation) cannot
//     escalate into touching the host, because the code that would touch the host
//     is not bound into the test binary at all.
//
// TestHostSeam_SingleConstructionPoint machine-checks the "one production
// construction point" half: reintroduce an inline realSysOps() in any entry point
// and it goes red — without executing anything.
package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
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
	os.Exit(m.Run())
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
//	    body of both real-seam constructors, because (1) and (2) are source scans and
//	    a source scan is exactly what a bad edit can defeat.
func scanHostSeamSource() []string {
	var out []string
	entries, err := os.ReadDir(".")
	if err != nil {
		return []string{fmt.Sprintf("cannot read package sources to verify the host seam: %v (fail closed)", err)}
	}
	total := 0
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
	}
	if total != 1 {
		out = append(out, fmt.Sprintf("realSysOps() appears %d time(s) across the non-test sources, want exactly 1 (realHostSeam)", total))
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
	return out
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

// TestHostSeam_SingleConstructionPoint is the DRIFT GUARD for the structural claim.
// TestMain can only protect what it can rebind, so the real OS must be wired in
// exactly ONE place in the non-test sources. This scans the package's own source:
// reintroducing `realSysOps()` inside installCmd/teardownCmd (the pre-T-5047 shape)
// makes it fail — with zero execution and zero risk, which is the only way to run a
// counterfactual for this particular bug on a live machine.
// It is a REPORTING wrapper over scanHostSeamSource, not the enforcement point:
// TestMain already refused to reach this function if the scan found anything. It
// stays as a named test so the property has a name in `go test -v` output and in
// cli/CLAUDE.md, and so it still reports if TestMain's gate is ever weakened.
func TestHostSeam_SingleConstructionPoint(t *testing.T) {
	for _, v := range scanHostSeamSource() {
		t.Errorf("host seam structure violated: %s", v)
	}
}

// TestHostSeam_RealHostSeamIsNeverCalledDirectly closes the hole the version of
// this file that shipped first left open.
//
// The counterfactual that exposed it: change installCmd's `host := newHostSeam()`
// to `host := realHostSeam()`. Everything above stays GREEN — realSysOps() still
// appears exactly once, `var newHostSeam` is still there — while the entry point
// once again wires itself straight to the real OS. The runtime tests DO catch it,
// but only by noticing afterwards that no seam was constructed, i.e. AFTER the
// entry point has already run a real install against the machine. On the verb
// that boots out a live launchd job, "we detected it afterwards" is not a
// defence; install.go's own comment already says NEVER call realHostSeam from an
// entry point, and until now nothing enforced that sentence.
//
// So: realHostSeam may be MENTIONED once as its own declaration and once as the
// initialiser of newHostSeam, and CALLED nowhere.
//
// ⚠️ WHERE THIS IS ENFORCED, AND WHY IT MOVED
// This scan lives in scanHostSeamSource, called from TestMain BEFORE m.Run(). The
// version of this file that shipped first ran it as an ordinary Test* function and
// claimed in this comment that it "fails before anything executes". That was false:
// this function is declared BELOW TestInstallCmd_CannotReachTheRealHost, Go runs
// tests in declaration order, and under the very mutant described above the real
// seam is constructed in that earlier test — so on a machine with a live warden the
// bootout happens first and this guard never runs. That is not a thought experiment:
// it happened during T-5047 verification and unloaded this machine's live warden.
// See TestMain for the full account. This function is now a reporting wrapper only;
// the gate is TestMain, and realSysOps/realHostSeam additionally refuse at runtime.
func TestHostSeam_RealHostSeamIsNeverCalledDirectly(t *testing.T) {
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
