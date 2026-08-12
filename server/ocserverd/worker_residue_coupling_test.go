package main

import (
	"strings"
	"testing"
)

// TestWorkerSharedCoreStartsWithTheUnfilteredSystemSeed guards the assembled
// path, independently from workerSharedHead's unit-level equality test. A
// future worker-only exclusion or rewrite changes this prefix and fails here.
//
// T-4595 narrowed WHAT this prefix is. It used to be 系統互動 + 啟動程序 glued
// together, because a worker got all three shared blocks grouped at the top.
// The boot sequence is now the recency-authoritative TAIL for workers too —
// same slot as staff — so only the system-interaction seed leads. The tail's
// placement is asserted separately (TestBothBootContextsUseTheSameFourSlots).
func TestWorkerSharedCoreStartsWithTheUnfilteredSystemSeed(t *testing.T) {
	s := newWorkerTestServer(t)
	sys, err := s.root.readSeedFile("system_interaction.md")
	if err != nil {
		t.Fatalf("read system_interaction.md: %v", err)
	}
	want := strings.TrimSpace(sys)
	if got := crossrefWorkerCtx(t); !strings.HasPrefix(got, want+"\n\n") {
		t.Fatal("worker boot context no longer starts with the unfiltered system-interaction seed")
	}
}

// TestWorkerLaunchGuidanceIsTheSharedOneNotAReplacement — T-4595.
//
// The overlay used to carry a full REPLACEMENT boot sequence of its own,
// introduced as authoritative ("你的開機序列以這一節為準"), which (a) claimed
// report_waking was not in a worker's boot sequence — false, the handler routes
// an outsource caller through workerReportWaking on the same endpoint — and (b)
// re-stated the shared three steps, so the two copies could drift apart with
// nothing to notice. The overlay is gone.
//
// What must remain true of the assembled document: the shared boot sequence is
// present, it still tells the worker to pick up its one task with get_my_task,
// and nothing claims the shared steps were removed or overridden.
func TestWorkerLaunchGuidanceIsTheSharedOneNotAReplacement(t *testing.T) {
	ctx := crossrefWorkerCtx(t)
	if strings.Contains(ctx, "已從你這份的啟動程序裡拿掉") {
		t.Error("worker boot context falsely claims shared boot steps were removed")
	}
	for _, want := range []string{
		bootSequenceH1, // the shared 啟動程序 block itself
		"get_my_task",  // the one worker-specific step: 領工
	} {
		if !strings.Contains(ctx, want) {
			t.Errorf("worker boot context is missing launch guidance %q", want)
		}
	}
}
