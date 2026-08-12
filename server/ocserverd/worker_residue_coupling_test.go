package main

import (
	"strings"
	"testing"
)

// TestWorkerSharedCoreStartsWithBothUnfilteredSeeds guards the assembled path,
// independently from workerGlobalContext's unit-level equality test. A future
// worker-only exclusion or rewrite changes this prefix and fails here.
func TestWorkerSharedCoreStartsWithBothUnfilteredSeeds(t *testing.T) {
	s := newWorkerTestServer(t)
	sys, err := s.root.readSeedFile("system_interaction.md")
	if err != nil {
		t.Fatalf("read system_interaction.md: %v", err)
	}
	boot, err := s.root.readSeedFile("boot_sequence.md")
	if err != nil {
		t.Fatalf("read boot_sequence.md: %v", err)
	}
	want := strings.TrimSpace(sys) + "\n\n" + strings.TrimSpace(boot)
	if got := crossrefWorkerCtx(t); !strings.HasPrefix(got, want+"\n\n---\n\n") {
		t.Fatal("worker boot context no longer starts with the two unfiltered shared seeds")
	}
}

// TestWorkerOverlayDescribesRoleDifferencesWithoutClaimingFilteredCore keeps
// the one directly affected overlay statement truthful: the common boot block
// remains present, while the worker's launch role is still described below it.
func TestWorkerOverlayDescribesRoleDifferencesWithoutClaimingFilteredCore(t *testing.T) {
	ctx := crossrefWorkerCtx(t)
	if strings.Contains(ctx, "已從你這份的啟動程序裡拿掉") {
		t.Error("worker overlay falsely claims shared boot steps were removed")
	}
	for _, want := range []string{"共用啟動程序", "你的開機序列以這一節為準", "get_my_task"} {
		if !strings.Contains(ctx, want) {
			t.Errorf("worker overlay is missing role-specific launch guidance %q", want)
		}
	}
}
