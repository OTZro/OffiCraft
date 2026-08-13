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

// 🔴 TWO fixtures, and the pair is what makes the gate observable. They sit on
// OPPOSITE corners of is_default × has_seed:
//
//	assistant  — factory seed, never written  ⇒ is_default=true,  has_seed=true
//	r-boot-*   — written overlay, no seed file ⇒ is_default=false, has_seed=false
//
// So every wrong gate loses exactly one of them: `!IsDefault` drops assistant,
// `IsDefault` drops the written role, `HasSeed` drops the written role. Only
// TrimSpace(Text) != "" keeps both. One fixture alone lets some of those
// mutants survive — measured, not reasoned: the first draft of this test used
// only the written role, and the `!IsDefault` mutant stayed green.
//
// ⚠️ NOTHING GUARDS THE PAIR. That is a statement of intent, not a protected
// property: delete either fixture and no assertion anywhere goes red, the test
// simply stops discriminating. If you touch this table, re-run the mutants
// listed above by hand — there is no mechanism that will do it for you.
func TestBootContextCarriesInsightBetweenRoleAndLessons(t *testing.T) {
	const written = "判準探針：刪除成本不對稱時先問 owner。"
	for _, tc := range []struct {
		name    string
		key     string
		overlay string
	}{
		{"seeded role that never wrote one", seedRoleAssistant, ""},
		{"custom role that wrote one", "r-boot-insight", written},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := newWorkerTestServer(t)
			if tc.key != seedRoleAssistant {
				bootRoleWithInsight(t, s, tc.key, tc.overlay)
			}

			insight, err := s.foldInsightDTO(tc.key)
			if err != nil {
				t.Fatalf("foldInsightDTO: %v", err)
			}
			// Premise: this fixture really does have an insight to show. If the
			// factory seed ever stops shipping, this fails HERE rather than
			// silently turning the assertions below vacuous.
			if strings.TrimSpace(insight.Text) == "" {
				t.Fatalf("fixture %q has no insight text — the assertions below would prove nothing", tc.key)
			}

			boot, err := s.buildBootContext(tc.key, nil, "general")
			if err != nil {
				t.Fatalf("buildBootContext: %v", err)
			}
			if boot == nil {
				t.Fatal("buildBootContext resolved no role for a role that exists")
			}
			ctx := boot.Context

			title := "# Insight (" + tc.key + ")"
			// The section must carry the doc the read face serves, not just a
			// header (is_default=%v has_seed=%v for this fixture).
			if !strings.Contains(ctx, title+"\n\n"+strings.TrimSpace(insight.Text)) {
				t.Fatalf("boot context does not carry %q with the folded doc verbatim "+
					"(is_default=%v has_seed=%v)", title, insight.IsDefault, insight.HasSeed)
			}

			// Position: Duty → Insight → Learning. Anchor on all three so a
			// section that merely EXISTS somewhere cannot pass.
			role := strings.Index(ctx, "# Role: ")
			ins := strings.Index(ctx, title)
			lessons := strings.Index(ctx, "# Lessons ("+tc.key+" / general)")
			if role < 0 || ins < 0 || lessons < 0 {
				t.Fatalf("missing an anchor: role=%d insight=%d lessons=%d", role, ins, lessons)
			}
			if !(role < ins && ins < lessons) {
				t.Fatalf("want 角色說明 → 判準 → 長期筆記, got role=%d insight=%d lessons=%d",
					role, ins, lessons)
			}
		})
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
