package main

// api_insight_seed_te1e3_test.go — the PER-ROLE insight file seed (T-e1e3).
//
// 🔴 READ THIS BEFORE TRUSTING ANY GREEN HERE — WHERE THE DISCRIMINATION LIVES.
// The repo's standing rule is "an assertion must have been RED before the
// change". On this ticket that rule can only be met DEGENERATELY: before
// T-e1e3 there was no insight seed path AT ALL — `seeds/insight_*.md` did not
// exist and FoldInsight took one argument — so every assertion below fails to
// COMPILE on the parent commit rather than failing on behaviour. A red that
// says "undefined: seedInsightMD" proves nothing about whether the fold is
// correct.
//
// ⇒ THE DISCRIMINATING POWER OF THIS FILE IS CARRIED BY MUTANTS, NOT BY ITS
// PRE-CHANGE RED. Three were run against the shipped code (each preceded by
// `go clean -testcache`; restores from a scratchpad backup, never
// `git checkout --`), and each must stay red in the named place:
//
//	① make the seed lookup SHARED instead of per-role (drop the
//	   `insight_<roleKey>.md` interpolation, read one fixed file for every
//	   role) → TestInsightSeed_OtherRolesStayEmpty must go red. This is the
//	   shape this ticket is MOST likely to be got wrong, because it is exactly
//	   what lessons does one function away.
//	② remove the seed fold (FoldInsight ignores hasSeed) →
//	   TestInsightSeed_FreshDatabaseServesTheFactoryDoc must go red.
//	③ make a written doc still read the seed (FoldInsight prefers the seed) →
//	   TestInsightSeed_OverlayWinsOverTheSeed must go red.
//
// Anyone changing this area: re-run all three and update the record. A test
// file whose only claim to correctness is "it is green" is not a guard here.

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

// TestSeedInsightMD_EachSeededRoleReadsItsOwnFile — 🔴 THE PER-ROLE TEST, and
// the reason it looks like this is worth reading before "simplifying" it.
//
// The FIRST version of this test only ever asked about `assistant`, against a
// resolver that gated on the one-entry seed-role roster before touching the
// filename. Mutant ① (swap the interpolated name for one shared file) was then
// UNOBSERVABLE — measured, 0 tests red — because no other role ever reached the
// lookup. "Per-role" cannot be observed in a world with one seeded role.
//
// So this drives seedInsightMDFrom over a world with TWO seeded roles. That is
// the smallest world in which the property has any content, and it is the world
// the repo will really be in the day a second seed ships.
func TestSeedInsightMD_EachSeededRoleReadsItsOwnFile(t *testing.T) {
	const assistantSeed = "# assistant — how I weigh a call\n"
	const testerSeed = "# tester — how I weigh a call\n"
	root := assetRoot(".")
	embedded := fstest.MapFS{
		insightSeedFilename("assistant"): &fstest.MapFile{Data: []byte(assistantSeed)},
		insightSeedFilename("tester"):    &fstest.MapFile{Data: []byte(testerSeed)},
		// A decoy under the name a SHARED lookup would plausibly use.
		"insight.md": &fstest.MapFile{Data: []byte("SHARED — no role may ever read this\n")},
	}

	for roleKey, want := range map[string]string{"assistant": assistantSeed, "tester": testerSeed} {
		got, ok, err := root.seedInsightMDFrom(roleKey, embedded)
		if err != nil || !ok {
			t.Fatalf("%s: ok=%v err=%v, want a seed", roleKey, ok, err)
		}
		if got != want {
			t.Fatalf("role %q read %q, want its OWN seed %q — the lookup is not per-role", roleKey, got, want)
		}
	}

	// A role with no file of its own gets NOTHING — not the decoy, not another
	// role's bytes.
	if got, ok, err := root.seedInsightMDFrom("reviewer", embedded); ok || got != "" || err != nil {
		t.Fatalf("unseeded role resolved to (%q, ok=%v, err=%v); want no seed", got, ok, err)
	}
}

// TestSafeSeedRoleKey_RejectsAnythingThatCouldLeaveTheDirectory — the roleKey
// comes off a URL path segment, and it is interpolated into a filename.
func TestSafeSeedRoleKey_RejectsAnythingThatCouldLeaveTheDirectory(t *testing.T) {
	for _, bad := range []string{
		"", "..", "../role_def_assistant", "a/b", "a\\b", "a\x00b", "rôle", "a b", "a.md",
	} {
		if safeSeedRoleKey(bad) {
			t.Fatalf("safeSeedRoleKey(%q) = true; it must never reach a filename", bad)
		}
	}
	// POSITIVE CONTROL: the real role-key shapes this repo uses must pass, or
	// the check above is satisfied by rejecting everything.
	for _, good := range []string{"assistant", "r-tester", "reviewer", "role_2"} {
		if !safeSeedRoleKey(good) {
			t.Fatalf("safeSeedRoleKey(%q) = false; a real role key was rejected", good)
		}
	}
	// And the traversal attempt really is refused END TO END, not just by the
	// predicate: a "../role_def_assistant" key must not read the Duty seed.
	root := assetRoot(".")
	embedded := fstest.MapFS{"role_def_assistant.md": &fstest.MapFile{Data: []byte("DUTY")}}
	if got, ok, _ := root.seedInsightMDFrom("../role_def_assistant", embedded); ok || got != "" {
		t.Fatalf("traversal key resolved to (%q, ok=%v)", got, ok)
	}
}

// TestInsightSeed_FreshDatabaseServesTheFactoryDoc — acceptance #1. On a
// database with no insight row at all, GET /api/insight/assistant answers the
// FACTORY content (not ""), and is_default marks it as factory rather than as
// something a person wrote.
//
// MUTANT ②: drop the seed fold → this goes red on `text`.
func TestInsightSeed_FreshDatabaseServesTheFactoryDoc(t *testing.T) {
	s := newTasksTestServer(t)
	seed := readShippedAssistantInsightSeed(t, s)

	dto := getInsightDTO(t, s, seedRoleAssistant)

	// ANTI-TAUTOLOGY: a seed that is empty would make every assertion below
	// pass for the wrong reason. Assert the corpus is real first.
	if strings.TrimSpace(seed) == "" {
		t.Fatal("the shipped assistant insight seed is empty — this test would be vacuous")
	}
	if dto.Text != seed {
		t.Fatalf("fresh DB served %d chars, want the %d-char factory seed verbatim", len(dto.Text), len(seed))
	}
	if !dto.IsDefault {
		t.Fatal("is_default must be TRUE for factory content — false says a person wrote it, and the cockpit renders it accordingly")
	}
	if dto.SizeChars != len([]rune(dto.Text)) {
		t.Fatalf("size_chars %d disagrees with the served text (%d runes)", dto.SizeChars, len([]rune(dto.Text)))
	}
}

// TestInsightSeed_OtherRolesStayEmpty — acceptance #2, and 🔴 THE ONE THAT
// MATTERS MOST. Insight seeds are PER-ROLE. If anyone re-shapes the lookup into
// lessons' one-shared-file form, every role ships with the ASSISTANT's
// judgement calls — wrong for a tester, wrong for an engineer, and completely
// invisible until someone reads a role's card.
//
// MUTANT ①: make the lookup shared → this goes red.
func TestInsightSeed_OtherRolesStayEmpty(t *testing.T) {
	s := newTasksTestServer(t)

	// POSITIVE CONTROL FIRST: prove the seed path is live in this fixture at
	// all. Without it, "the other roles are empty" is satisfied by a build
	// where seeds simply do not work — the exact false green this test exists
	// to prevent.
	if got := getInsightDTO(t, s, seedRoleAssistant); strings.TrimSpace(got.Text) == "" {
		t.Fatal("positive control: assistant must read its factory seed here, or the negative assertions below prove nothing")
	}

	for _, roleKey := range []string{"r-tester", "r-engineer", "reviewer"} {
		dto := getInsightDTO(t, s, roleKey)
		if dto.Text != "" {
			t.Fatalf("role %q served %d chars of insight — a seed leaked across roles; every role now boots with the assistant's judgement calls", roleKey, len(dto.Text))
		}
		if !dto.IsDefault {
			t.Fatalf("role %q: is_default must stay true for an unwritten doc", roleKey)
		}
	}
}

// TestInsightSeed_OverlayWinsOverTheSeed — acceptance #3. Once the role writes,
// the read is THAT text, and is_default flips off. Same overlay semantics Duty
// and Learning carry.
//
// MUTANT ③: prefer the seed over a written overlay → this goes red.
func TestInsightSeed_OverlayWinsOverTheSeed(t *testing.T) {
	s := newTasksTestServer(t)
	seed := readShippedAssistantInsightSeed(t, s)
	const written = "# Insight — written by the role itself\n\n這是使用者寫的，不是出廠版。\n"

	if written == seed {
		t.Fatal("fixture bug: the written doc must differ from the seed")
	}
	if err := s.dal.PutInsight(Insight{RoleKey: seedRoleAssistant, Text: written}); err != nil {
		t.Fatalf("PutInsight: %v", err)
	}

	dto := getInsightDTO(t, s, seedRoleAssistant)
	if dto.Text != written {
		t.Fatalf("after a write the read must be the WRITTEN doc; got %q", dto.Text)
	}
	if dto.IsDefault {
		t.Fatal("is_default must be FALSE once the role has written — true would label a person's work as factory content")
	}
}

// TestFoldInsight_TheWholeTable pins the three states of the fold as a table,
// so a change that collapses two of them into one has to touch this file.
func TestFoldInsight_TheWholeTable(t *testing.T) {
	const seed = "factory"
	for _, tc := range []struct {
		name      string
		overlay   *Insight
		seedText  string
		hasSeed   bool
		wantText  string
		wantDeflt bool
	}{
		{"unwritten with seed", nil, seed, true, seed, true},
		{"unwritten without seed", nil, "", false, "", true},
		{"tombstoned with seed", &Insight{Text: "old", Tombstoned: true}, seed, true, seed, true},
		{"tombstoned without seed", &Insight{Text: "old", Tombstoned: true}, "", false, "", true},
		{"written with seed", &Insight{Text: "mine"}, seed, true, "mine", false},
		{"written without seed", &Insight{Text: "mine"}, "", false, "mine", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			text, isDefault := FoldInsight(tc.overlay, tc.seedText, tc.hasSeed)
			if text != tc.wantText || isDefault != tc.wantDeflt {
				t.Fatalf("got (%q, %v), want (%q, %v)", text, isDefault, tc.wantText, tc.wantDeflt)
			}
		})
	}
}

// ── helpers ─────────────────────────────────────────────────────────────────

// readShippedAssistantInsightSeed reads the seed through the SAME path the
// handler uses, so the expected value can never be a second copy that drifts
// from the shipped file.
func readShippedAssistantInsightSeed(t *testing.T, s *apiServer) string {
	t.Helper()
	text, ok, err := s.root.seedInsightMD(seedRoleAssistant)
	if err != nil {
		t.Fatalf("seedInsightMD: %v (did bin/build-seedsdist run?)", err)
	}
	if !ok {
		t.Fatal("the assistant has no insight seed — seeds/insight_assistant.md is missing from the staged embed")
	}
	return text
}

func getInsightDTO(t *testing.T, s *apiServer, roleKey string) insightDTO {
	t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/insight/"+roleKey, nil)
	s.HandleGetInsightApiInsightRoleKeyGet(rec, req, roleKey)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET insight %s: status %d body %s", roleKey, rec.Code, rec.Body.String())
	}
	var dto insightDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &dto); err != nil {
		t.Fatalf("decode insightDTO: %v", err)
	}
	return dto
}
