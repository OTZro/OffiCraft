package main

import (
	"bytes"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"syscall"
	"testing"
)

type fakeChild struct {
	signals chan os.Signal
	release chan struct{}
	state   *os.ProcessState
	err     error
}

func newFakeChild() *fakeChild {
	return &fakeChild{signals: make(chan os.Signal, len(forwardedSignals)), release: make(chan struct{})}
}

func (c *fakeChild) Signal(sig os.Signal) error { c.signals <- sig; return nil }

func (c *fakeChild) Wait() (*os.ProcessState, error) {
	<-c.release
	return c.state, c.err
}

type launchRecord struct {
	path string
	argv []string
}

func withFakeChild(t *testing.T, c child) *launchRecord {
	t.Helper()
	rec := &launchRecord{}
	previous := startChild
	startChild = func(got string, gotArgv []string) (child, error) {
		rec.path, rec.argv = got, gotArgv
		return c, nil
	}
	t.Cleanup(func() { startChild = previous })
	return rec
}

func TestRealMainStartsOnlySiblingOcwarden(t *testing.T) {
	c := newFakeChild()
	close(c.release)
	rec := withFakeChild(t, c)
	previous := executable
	executable = func() (string, error) { return "/opt/officraft/officraft", nil }
	t.Cleanup(func() { executable = previous })

	if got := realMain(nil, &bytes.Buffer{}); got != 0 {
		t.Fatalf("exit = %d, want 0", got)
	}
	if rec.path != "/opt/officraft/ocwarden" {
		t.Fatalf("child path = %q, want adjacent ocwarden", rec.path)
	}
	// The subcommand is load-bearing: a bare ocwarden is not the warden loop, and
	// every test here stubs the launch out, so nothing else would catch its loss.
	if want := []string{"/opt/officraft/ocwarden", "run"}; !slices.Equal(rec.argv, want) {
		t.Fatalf("child argv = %q, want %q", rec.argv, want)
	}
}

func TestRealMainRejectsArgumentsBeforeStartingAChild(t *testing.T) {
	started := false
	previous := startChild
	startChild = func(string, []string) (child, error) { started = true; return nil, errors.New("must not run") }
	t.Cleanup(func() { startChild = previous })

	var out bytes.Buffer
	if got := realMain([]string{"/tmp/other"}, &out); got != 2 {
		t.Fatalf("exit = %d, want 2", got)
	}
	if started {
		t.Fatal("arguments must never select a child process")
	}
	if got := out.String(); got != "usage: officraft\n" {
		t.Fatalf("refusal = %q, want a no-bypass usage message", got)
	}
}

func TestRealMainForwardsTerminationToTheSibling(t *testing.T) {
	c := newFakeChild()
	withFakeChild(t, c)
	previousExecutable := executable
	executable = func() (string, error) { return "/opt/officraft/officraft", nil }
	t.Cleanup(func() { executable = previousExecutable })
	registered := make(chan chan<- os.Signal, 1)
	previousNotify, previousStop := notify, stop
	notify = func(ch chan<- os.Signal, _ ...os.Signal) { registered <- ch }
	stop = func(chan<- os.Signal) {}
	t.Cleanup(func() { notify, stop = previousNotify, previousStop })

	done := make(chan int, 1)
	go func() { done <- realMain(nil, &bytes.Buffer{}) }()
	(<-registered) <- syscall.SIGTERM
	if got := <-c.signals; got != syscall.SIGTERM {
		t.Fatalf("signal = %v, want SIGTERM", got)
	}
	close(c.release)
	<-done
}

func TestExitStatusKeepsChildSignalCause(t *testing.T) {
	if got := exitStatus(nil); got != 0 {
		t.Fatalf("nil state = %d, want 0", got)
	}
	if got := exitStatusFromWait(syscall.WaitStatus(0)); got != 0 {
		t.Fatalf("clean exit = %d, want 0", got)
	}
	if got := exitStatusFromWait(syscall.WaitStatus(3 << 8)); got != 3 {
		t.Fatalf("exit 3 = %d, want 3", got)
	}
	if got := exitStatusFromWait(syscall.WaitStatus(int(syscall.SIGKILL))); got != 128+int(syscall.SIGKILL) {
		t.Fatalf("SIGKILL exit = %d, want %d", got, 128+int(syscall.SIGKILL))
	}
}

// The anchor's entire TCC purpose depends on keeping its own process identity:
// launchd's job leader is the responsible process for the whole tree, and an
// exec keeps the pid while replacing the code identity — the fix would vanish
// and every behavioural test here would stay green, because they all stub
// startChild out. This scan is the only thing standing in the way, so it does
// not look for one spelling in one file.
//
// It parses EVERY non-test file in the package and rejects:
//   - any reference to an exec-family selector (syscall.Exec, unix.Exec,
//     syscall.ForkExec, ...) — as a reference, not as a call, so taking a
//     method value or aliasing it into a variable is caught too;
//   - any SYS_EXEC* identifier, which is how the raw-syscall route spells it;
//   - any import outside the anchor's tiny allowlist, which is what a new file
//     would need to reach exec by some route this list has not imagined.
//
// Parsing (rather than grepping) also means the guard can never be satisfied by
// its own prose: comments are not part of the AST.
func TestLauncherForksInsteadOfExecing(t *testing.T) {
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate launcher source")
	}
	dir := filepath.Dir(thisFile)
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, dir, func(fi os.FileInfo) bool {
		return !strings.HasSuffix(fi.Name(), "_test.go")
	}, 0)
	if err != nil {
		t.Fatalf("parse launcher package: %v", err)
	}

	allowedImports := map[string]bool{
		"fmt": true, "io": true, "os": true, "os/signal": true,
		"path/filepath": true, "syscall": true,
	}
	execSelectors := map[string]bool{
		"Exec": true, "Execve": true, "Execveat": true, "ForkExec": true, "StartProcessExec": true,
	}

	forks := false
	for _, pkg := range pkgs {
		for name, file := range pkg.Files {
			for _, imp := range file.Imports {
				path := strings.Trim(imp.Path.Value, `"`)
				if !allowedImports[path] {
					t.Fatalf("%s imports %q, which is outside the anchor's allowlist — "+
						"a new dependency is how an exec sneaks back in; widen the list "+
						"deliberately if the anchor really needs it", name, path)
				}
			}
			ast.Inspect(file, func(n ast.Node) bool {
				switch node := n.(type) {
				case *ast.SelectorExpr:
					if id, isIdent := node.X.(*ast.Ident); isIdent {
						if execSelectors[node.Sel.Name] {
							t.Fatalf("%s references %s.%s — the anchor must FORK its child, "+
								"never exec: an exec keeps the pid but hands the TCC "+
								"identity to the binary being replaced, which is the whole "+
								"thing this launcher exists to prevent",
								name, id.Name, node.Sel.Name)
						}
						if id.Name == "os" && node.Sel.Name == "StartProcess" {
							forks = true
						}
					}
				case *ast.Ident:
					if strings.HasPrefix(node.Name, "SYS_EXEC") {
						t.Fatalf("%s names %s — the raw-syscall route to exec is barred "+
							"for the same reason as syscall.Exec", name, node.Name)
					}
				}
				return true
			})
		}
	}
	if !forks {
		t.Fatal("no os.StartProcess call anywhere in the anchor package — " +
			"without it this guard proves nothing (the launcher may not be starting a child at all)")
	}
}
