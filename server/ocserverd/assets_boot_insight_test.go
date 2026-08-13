package main

import (
	"strings"
	"testing"
)

// The boot context folds Insight — the persona's third block — between Duty
// (# Role) and Learning (# Lessons).
//
// 🔴 THE TWO DIRECTIONS ARE ONE CONTRACT AND BOTH HAVE TO HOLD. A role that
// wrote an insight (or ships a factory seed) must SEE it at boot; a role that
// wrote none and has no seed must NOT grow an orphan header with nothing under
// it. A gate on insight.IsDefault or insight.HasSeed satisfies neither
// reliably — those two answer different questions (did the text come from the
// seed rather than an overlay / does a seed FILE exist for this role at all),
// so one of them is near-constant here and would either emit the section for
// genuinely-empty roles or suppress it for roles that wrote one. The gate is
// the FOLDED TEXT, exactly like the 使用者自訂 block.

// bootRoleWithInsight registers a custom role (deliberately NOT `assistant`, so
// it can carry no factory insight seed) and returns its key.
func bootRoleWithInsight(t *testing.T, s *apiServer, key, insight string) {
	t.Helper()
	if err := s.dal.PutRoleDef(RoleDef{
		RoleKey:      key,
		Name:         "Boot Fold Probe",
		DefinitionMD: "duty body for " + key,
	}); err != nil {
		t.Fatalf("PutRoleDef: %v", err)
	}
	if insight == "" {
		return
	}
	if err := s.dal.PutInsight(Insight{RoleKey: key, Text: insight}); err != nil {
		t.Fatalf("PutInsight: %v", err)
	}
}

func TestBootContextCarriesInsightBetweenRoleAndLessons(t *testing.T) {
	s := newWorkerTestServer(t)
	const key = "r-boot-insight"
	const body = "判準探針：刪除成本不對稱時先問 owner。"
	bootRoleWithInsight(t, s, key, body)

	boot, err := s.buildBootContext(key, nil, "general")
	if err != nil {
		t.Fatalf("buildBootContext: %v", err)
	}
	if boot == nil {
		t.Fatal("buildBootContext resolved no role for a role that exists")
	}
	ctx := boot.Context

	title := "# Insight (" + key + ")"
	if !strings.Contains(ctx, title) {
		t.Fatalf("boot context is missing the insight section %q", title)
	}
	// The section must carry the doc the read face serves, not just a header.
	insight, err := s.foldInsightDTO(key)
	if err != nil {
		t.Fatalf("foldInsightDTO: %v", err)
	}
	if !strings.Contains(ctx, title+"\n\n"+strings.TrimSpace(insight.Text)) {
		t.Fatalf("insight section does not carry the folded doc verbatim")
	}

	// Position: Duty → Insight → Learning. Anchor on all three so a section
	// that merely EXISTS somewhere cannot pass.
	role := strings.Index(ctx, "# Role: ")
	ins := strings.Index(ctx, title)
	lessons := strings.Index(ctx, "# Lessons ("+key+" / general)")
	if role < 0 || ins < 0 || lessons < 0 {
		t.Fatalf("missing an anchor: role=%d insight=%d lessons=%d", role, ins, lessons)
	}
	if !(role < ins && ins < lessons) {
		t.Fatalf("want 角色說明 → 判準 → 長期筆記, got role=%d insight=%d lessons=%d",
			role, ins, lessons)
	}
}

func TestBootContextHasNoOrphanInsightHeaderForAnUnwrittenRole(t *testing.T) {
	s := newWorkerTestServer(t)
	const key = "r-boot-no-insight"
	bootRoleWithInsight(t, s, key, "") // role exists, insight never written

	// Positive control: prove the premise. A role whose insight only LOOKS
	// empty because the fold errored would make the negative assertion below
	// pass for the wrong reason.
	insight, err := s.foldInsightDTO(key)
	if err != nil {
		t.Fatalf("foldInsightDTO: %v", err)
	}
	if strings.TrimSpace(insight.Text) != "" {
		t.Fatalf("fixture is not the empty case: insight text = %q", insight.Text)
	}

	boot, err := s.buildBootContext(key, nil, "general")
	if err != nil {
		t.Fatalf("buildBootContext: %v", err)
	}
	if boot == nil {
		t.Fatal("buildBootContext resolved no role for a role that exists")
	}
	ctx := boot.Context

	// Positive control #2: the fold really assembled (otherwise "no # Insight"
	// is satisfied by an empty string).
	for _, want := range []string{"# Role: ", "# Lessons (" + key + " / general)"} {
		if !strings.Contains(ctx, want) {
			t.Fatalf("boot context did not assemble: missing %q", want)
		}
	}
	if strings.Contains(ctx, "# Insight") {
		t.Fatalf("a role with no insight and no seed grew an orphan # Insight header")
	}
}
