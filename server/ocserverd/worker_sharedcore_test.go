package main

import (
	"strings"
	"testing"
)

const globalContextH1 = "# Global Context"

// workerCtx builds a worker boot context over a minimal fixture.
func workerCtx(t *testing.T) string {
	t.Helper()
	return workerCtxOn(t, newWorkerTestServer(t))
}

func workerCtxOn(t *testing.T, s *apiServer) string {
	t.Helper()
	w := OutsourceWorker{ID: "ow-t108b", Codename: "O-9", Model: "sonnet", Effort: "medium"}
	task := Task{ID: "tk-t108b", Title: "T-108b fixture", TypeKey: "general",
		Priority: "mid", ExecutorKind: TaskExecutorOutsource, ExecutorID: w.ID}
	putWorkerFixture(t, s, w)
	putTaskFixture(t, s, task)
	ctx, err := s.buildWorkerBootContext(w, task, nil)
	if err != nil {
		t.Fatalf("buildWorkerBootContext: %v", err)
	}
	return ctx
}

// memberCtx builds the default member boot context.
func memberCtx(t *testing.T) (*apiServer, *bootContext) {
	t.Helper()
	s := newWorkerTestServer(t)
	bc, err := s.buildBootContext("", nil, "")
	if err != nil {
		t.Fatalf("buildBootContext: %v", err)
	}
	if bc == nil {
		t.Fatal("buildBootContext returned nil for the default role")
	}
	return s, bc
}

func unfilteredWorkerGlobalContextWant(t *testing.T, s *apiServer, ownerText string) string {
	t.Helper()
	sys, err := s.root.readSeedFile("system_interaction.md")
	if err != nil {
		t.Fatalf("read system_interaction.md: %v", err)
	}
	boot, err := s.root.readSeedFile("boot_sequence.md")
	if err != nil {
		t.Fatalf("read boot_sequence.md: %v", err)
	}
	parts := []string{strings.TrimSpace(sys)}
	if strings.TrimSpace(ownerText) != "" {
		parts = append(parts, "# 使用者自訂（Owner Additions）\n\n"+strings.TrimSpace(ownerText))
	}
	parts = append(parts, strings.TrimSpace(boot))
	return strings.Join(parts, "\n\n")
}

func TestWorkerBootContextStartsWithGlobalContext(t *testing.T) {
	ctx := workerCtx(t)
	if !strings.HasPrefix(ctx, globalContextH1) {
		t.Fatalf("worker boot context must open with Global Context; got %q", ctx[:min(len(ctx), 120)])
	}
	if iCore, iOverlay := strings.Index(ctx, globalContextH1), strings.Index(ctx, "# 外包工作者"); iOverlay < 0 || iCore > iOverlay {
		t.Fatalf("Global Context must precede the worker overlay (core=%d, overlay=%d)", iCore, iOverlay)
	}
}

func TestMemberBootContextStartsWithGlobalContext(t *testing.T) {
	_, bc := memberCtx(t)
	if !strings.HasPrefix(bc.Context, globalContextH1) {
		t.Fatalf("member boot context must open with Global Context; got %q", bc.Context[:min(len(bc.Context), 120)])
	}
}

// The member assembly remains an independent sentinel: this worker-only change
// must not alter the staff fold.
func TestMemberBootContextByteIdenticalToSpecAssembly(t *testing.T) {
	s, bc := memberCtx(t)
	sysSeed, err := s.root.readSeedFile("system_interaction.md")
	if err != nil {
		t.Fatalf("read system_interaction.md: %v", err)
	}
	bootSeed, err := s.root.readSeedFile("boot_sequence.md")
	if err != nil {
		t.Fatalf("read boot_sequence.md: %v", err)
	}
	roleDTO, err := s.foldRoleDefDTO(bc.RoleKey)
	if err != nil || roleDTO == nil {
		t.Fatalf("fold role: %v", err)
	}
	lessons, err := s.foldLessonsDTO(bc.RoleKey, bc.TaskType)
	if err != nil {
		t.Fatalf("fold lessons: %v", err)
	}
	userCtx, err := s.foldUserContextDTO()
	if err != nil {
		t.Fatalf("fold user context: %v", err)
	}
	roleTitle := roleDTO.Name
	if roleTitle == "" {
		roleTitle = roleDTO.Key
	}
	parts := []string{
		strings.TrimSpace(sysSeed),
		"# Role: " + roleTitle + "\n\n" + strings.TrimSpace(roleDTO.DefinitionMD),
		"# Lessons (" + bc.RoleKey + " / " + bc.TaskType + ")\n\n" + strings.TrimSpace(lessons.Text),
	}
	if strings.TrimSpace(userCtx.Text) != "" {
		parts = append(parts, "# 使用者自訂（Owner Additions）\n\n"+strings.TrimSpace(userCtx.Text))
	}
	parts = append(parts, strings.TrimSpace(bootSeed))
	want := strings.Join(parts, "\n\n") + "\n"
	if bc.Context != want {
		t.Fatalf("member boot context drifted from the §2.2 assembly (got %d bytes, want %d)", len(bc.Context), len(want))
	}
}

// TestWorkerGlobalContextMatchesUnfilteredSeedAssembly is the discriminator for
// this change. Reintroducing any worker-only exclusion or rewrite into either
// shared seed makes this equality assertion red.
func TestWorkerGlobalContextMatchesUnfilteredSeedAssembly(t *testing.T) {
	s := newWorkerTestServer(t)
	const ownerMark = "T108B-OWNER-CUSTOM-MARKER"
	if err := s.dal.PutUserContext(UserContext{Text: ownerMark}); err != nil {
		t.Fatalf("put user context: %v", err)
	}
	got, err := s.workerGlobalContext()
	if err != nil {
		t.Fatalf("workerGlobalContext: %v", err)
	}
	want := unfilteredWorkerGlobalContextWant(t, s, ownerMark)
	if got != want {
		t.Fatalf("worker global context must equal the unfiltered seed assembly (got %d bytes, want %d)", len(got), len(want))
	}
}

func TestWorkerGlobalContextSkipsBlankOwnerBlock(t *testing.T) {
	s := newWorkerTestServer(t)
	got, err := s.workerGlobalContext()
	if err != nil {
		t.Fatalf("workerGlobalContext: %v", err)
	}
	if got != unfilteredWorkerGlobalContextWant(t, s, "") {
		t.Fatal("blank owner text must preserve the shared seed assembly without an empty header")
	}
}

func TestWorkerBootContextCarriesRiskLanguage(t *testing.T) {
	ctx := workerCtx(t)
	for _, want := range []string{"風險", "backup-before-destructive", "verify-before-assert", "安全邊界"} {
		if !strings.Contains(ctx, want) {
			t.Errorf("worker boot context is missing risk language %q", want)
		}
	}
}

func TestWorkerOverlayRetainsRoleSpecificSingleTaskRule(t *testing.T) {
	if !strings.Contains(workerCtx(t), "一 worker 綁一任務") {
		t.Error("worker overlay must retain its single-bound-task rule")
	}
}
