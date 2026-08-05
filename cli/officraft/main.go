package main

import (
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
)

var forwardedSignals = []os.Signal{syscall.SIGTERM, syscall.SIGINT, syscall.SIGHUP}

type child interface {
	Signal(os.Signal) error
	Wait() (*os.ProcessState, error)
}

var executable = os.Executable

var startChild = func(path string, argv []string) (child, error) {
	return os.StartProcess(path, argv, &os.ProcAttr{
		Files: []*os.File{os.Stdin, os.Stdout, os.Stderr},
		Env:   os.Environ(),
	})
}

var notify = signal.Notify
var stop = signal.Stop

func main() {
	os.Exit(realMain(os.Args[1:], os.Stdout))
}

func realMain(args []string, out io.Writer) int {
	if len(args) != 0 {
		fmt.Fprintln(out, "usage: officraft")
		return 2
	}
	exe, err := executable()
	if err != nil {
		fmt.Fprintf(out, "[officraft] FATAL: cannot resolve own path: %v\n", err)
		return 1
	}
	path := filepath.Join(filepath.Dir(exe), "ocwarden")
	// argv is built here, not inside the seam, so a test can see it: dropping the
	// "run" subcommand would start ocwarden in a mode nobody intends, and every
	// test stubs the seam out, so nothing else would notice.
	c, err := startChild(path, []string{path, "run"})
	if err != nil {
		fmt.Fprintf(out, "[officraft] FATAL: cannot start sibling ocwarden: %v\n", err)
		return 1
	}

	signals := make(chan os.Signal, len(forwardedSignals))
	notify(signals, forwardedSignals...)
	defer stop(signals)
	go func() {
		for sig := range signals {
			_ = c.Signal(sig)
		}
	}()

	state, err := c.Wait()
	if err != nil {
		fmt.Fprintf(out, "[officraft] FATAL: wait for ocwarden: %v\n", err)
		return 1
	}
	return exitStatus(state)
}

func exitStatus(state *os.ProcessState) int {
	if state == nil {
		return 0
	}
	if status, ok := state.Sys().(syscall.WaitStatus); ok {
		return exitStatusFromWait(status)
	}
	return state.ExitCode()
}

func exitStatusFromWait(status syscall.WaitStatus) int {
	if status.Signaled() {
		return 128 + int(status.Signal())
	}
	return status.ExitStatus()
}

// MUTANT 3/4 for T-ab2a — reverted next commit. Proves the TCC anchor check reddens
// when this module source moves without an explicit binary refresh.
