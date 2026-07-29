package main

import (
	"bytes"
	"errors"
	"os"
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

func withFakeChild(t *testing.T, c child) *string {
	t.Helper()
	var path string
	previous := startChild
	startChild = func(got string) (child, error) { path = got; return c, nil }
	t.Cleanup(func() { startChild = previous })
	return &path
}

func TestRealMainStartsOnlySiblingOcwarden(t *testing.T) {
	c := newFakeChild()
	close(c.release)
	path := withFakeChild(t, c)
	previous := executable
	executable = func() (string, error) { return "/opt/officraft/officraft", nil }
	t.Cleanup(func() { executable = previous })

	if got := realMain(nil, &bytes.Buffer{}); got != 0 {
		t.Fatalf("exit = %d, want 0", got)
	}
	if *path != "/opt/officraft/ocwarden" {
		t.Fatalf("child path = %q, want adjacent ocwarden", *path)
	}
}

func TestRealMainRejectsArgumentsBeforeStartingAChild(t *testing.T) {
	started := false
	previous := startChild
	startChild = func(string) (child, error) { started = true; return nil, errors.New("must not run") }
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
