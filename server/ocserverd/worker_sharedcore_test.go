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

// unfilteredWorkerGlobalContextWant rebuilds the expected worker global context
// from the seeds on disk.
//
// bootSeedFile is spelled out by the CALLER, never derived from the production
// helper: a want that called bootSequenceSeedName would move in lockstep with
// the code under test and could never disagree with it.
func unfilteredWorkerGlobalContextWant(t *testing.T, s *apiServer, ownerText, bootSeedFile string) string {
	t.Helper()
	sys, err := s.root.readSeedFile("system_interaction.md")
	if err != nil {
		t.Fatalf("read system_interaction.md: %v", err)
	}
	boot, err := s.root.readSeedFile(bootSeedFile)
	if err != nil {
		t.Fatalf("read %s: %v", bootSeedFile, err)
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
// T-108b. Reintroducing any worker-only exclusion or rewrite into either shared
// seed makes this equality assertion red.
//
// NOTE the runtime axis, added by this change: this asserts the assembly for a
// CLAUDE worker, and it is only about the Claude boot seed. It used to read as
// "a worker always gets boot_sequence.md" — which was the regression, not the
// contract. The codex half lives in
// TestWorkerGlobalContextFollowsTheWorkersRuntime.
func TestWorkerGlobalContextMatchesUnfilteredSeedAssembly(t *testing.T) {
	s := newWorkerTestServer(t)
	const ownerMark = "T108B-OWNER-CUSTOM-MARKER"
	if err := s.dal.PutUserContext(UserContext{Text: ownerMark}); err != nil {
		t.Fatalf("put user context: %v", err)
	}
	got, err := s.workerGlobalContext(RuntimeClaude)
	if err != nil {
		t.Fatalf("workerGlobalContext: %v", err)
	}
	want := unfilteredWorkerGlobalContextWant(t, s, ownerMark, "boot_sequence.md")
	if got != want {
		t.Fatalf("worker global context must equal the unfiltered seed assembly (got %d bytes, want %d)", len(got), len(want))
	}
}

func TestWorkerGlobalContextSkipsBlankOwnerBlock(t *testing.T) {
	s := newWorkerTestServer(t)
	got, err := s.workerGlobalContext(RuntimeClaude)
	if err != nil {
		t.Fatalf("workerGlobalContext: %v", err)
	}
	if got != unfilteredWorkerGlobalContextWant(t, s, "", "boot_sequence.md") {
		t.Fatal("blank owner text must preserve the shared seed assembly without an empty header")
	}
}

// TestWorkerGlobalContextFollowsTheWorkersRuntime pins BOTH directions of the
// runtime split at the shared-core seam: a codex worker must be assembled from
// boot_sequence_codex.md, a claude worker (and an unset runtime, which
// NormalizeRuntime folds to claude) from boot_sequence.md.
//
// The expected seed file names are literals here on purpose — see
// unfilteredWorkerGlobalContextWant.
func TestWorkerGlobalContextFollowsTheWorkersRuntime(t *testing.T) {
	for _, tc := range []struct {
		name     string
		runtime  string
		bootSeed string
	}{
		{"codex", RuntimeCodex, "boot_sequence_codex.md"},
		{"claude", RuntimeClaude, "boot_sequence.md"},
		{"unset defaults to claude", "", "boot_sequence.md"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := newWorkerTestServer(t)
			got, err := s.workerGlobalContext(tc.runtime)
			if err != nil {
				t.Fatalf("workerGlobalContext(%q): %v", tc.runtime, err)
			}
			want := unfilteredWorkerGlobalContextWant(t, s, "", tc.bootSeed)
			if got != want {
				t.Fatalf("runtime %q must be assembled from %s (got %d bytes, want %d)",
					tc.runtime, tc.bootSeed, len(got), len(want))
			}
		})
	}
}

// TestWorkerBootContextCarriesItsOwnRuntimeBootSequence is the END-TO-END half:
// the seam above proves workerGlobalContext honours the argument it is handed,
// this proves the SPAWN path actually hands it the worker's own runtime. Without
// it, buildWorkerBootContext could pass a constant and the seam test would stay
// green.
//
// The discriminating evidence is content, not a file name: the Claude boot
// sequence tells the agent to run a bare `ocagent listen` under the built-in
// Monitor, while the codex runtime tail (worker_spawn.go) tells it NOT to start
// a listener because the App Server sidecar owns it. Those two must never be in
// one boot context — that pair is the reported bug.
func TestWorkerBootContextCarriesItsOwnRuntimeBootSequence(t *testing.T) {
	s := newWorkerTestServer(t)
	claudeBoot, err := s.root.readSeedFile("boot_sequence.md")
	if err != nil {
		t.Fatalf("read boot_sequence.md: %v", err)
	}
	codexBoot, err := s.root.readSeedFile("boot_sequence_codex.md")
	if err != nil {
		t.Fatalf("read boot_sequence_codex.md: %v", err)
	}
	claudeOnly, codexOnly := distinctiveLine(t, claudeBoot, codexBoot), distinctiveLine(t, codexBoot, claudeBoot)

	for _, tc := range []struct {
		name          string
		runtime       string
		wantSubstr    string
		notWantSubstr string
	}{
		{"codex worker reads the codex boot sequence", RuntimeCodex, codexOnly, claudeOnly},
		{"claude worker reads the claude boot sequence", RuntimeClaude, claudeOnly, codexOnly},
		{"unset runtime reads the claude boot sequence", "", claudeOnly, codexOnly},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := newWorkerTestServer(t)
			w := OutsourceWorker{ID: "ow-t4595", Codename: "O-7", Runtime: tc.runtime,
				Model: "sonnet", Effort: "medium"}
			task := Task{ID: "tk-t4595", Title: "T-4595 fixture", TypeKey: "general",
				Priority: "mid", ExecutorKind: TaskExecutorOutsource, ExecutorID: w.ID}
			putWorkerFixture(t, s, w)
			putTaskFixture(t, s, task)
			ctx, err := s.buildWorkerBootContext(w, task, nil)
			if err != nil {
				t.Fatalf("buildWorkerBootContext: %v", err)
			}
			if !strings.Contains(ctx, tc.wantSubstr) {
				t.Errorf("runtime %q: boot context is missing its own boot sequence (looked for %q)",
					tc.runtime, tc.wantSubstr)
			}
			if strings.Contains(ctx, tc.notWantSubstr) {
				t.Errorf("runtime %q: boot context carries the OTHER runtime's boot sequence (found %q)",
					tc.runtime, tc.notWantSubstr)
			}
		})
	}
}

// distinctiveLine returns a long line present in doc but absent from other — the
// probe the runtime assertions above use. It FAILS rather than returning "" when
// the two seeds have grown identical: an empty probe would make both a Contains
// and a NotContains assertion trivially satisfiable, i.e. a silently dead test.
func distinctiveLine(t *testing.T, doc, other string) string {
	t.Helper()
	best := ""
	for _, line := range strings.Split(doc, "\n") {
		line = strings.TrimSpace(line)
		if len(line) < 24 || strings.Contains(other, line) {
			continue
		}
		if len(line) > len(best) {
			best = line
		}
	}
	if best == "" {
		t.Fatal("the two boot-sequence seeds share every substantial line; there is nothing left to tell them apart")
	}
	return best
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
