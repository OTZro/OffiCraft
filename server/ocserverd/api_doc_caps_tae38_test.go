package main

// api_doc_caps_tae38_test.go — T-ae38: Duty / Insight / Learning each answer to
// their OWN cap, and Duty has one at all for the first time.
//
// Two things are being pinned here, and they fail for different reasons:
//
//  1. **Duty is capped on BOTH doors.** The role-update handler and the
//     document-history restore path. Either one alone is decorative: edit the
//     definition down to 999 chars, then restore a 4,000-char earlier revision,
//     and the cap is gone. lessons and insight have always had the restore-side
//     check; role_definition was the one branch of that switch without it.
//
//  2. **The three segments are independent.** Raising one must not move the
//     others, and each must judge its own document by its own number. Before
//     this ticket there was one setting, so every "the cap is N" assertion in
//     the tree was equally true of all of them — which is why the tests below
//     always set the three caps to DIFFERENT values. With three equal numbers
//     every assertion here would pass on the pre-change code too.
//
// 🔴 THE FIXTURES ARE MULTI-BYTE ON PURPOSE. The cap counts RUNES
// (utf8.RuneCountInString); an ASCII fixture cannot tell a rune cap from a byte
// cap, and this file would go green on a server that had silently switched.
//
// 🔴 STRUCTURAL EXEMPTION, deliberately untested-for-refusal: reset_role writes
// a tombstone and folds back to the FILE seed — a path with no cap check on it
// — so no cap can catch shipped content by construction.
// ⚠️ This block used to add "and seeds/role_def_assistant.md is 4,594 runes,
// i.e. over the 1,000 default out of the box". T-e1e3 retired that oversized
// seed and T-795e replaced it again; the shipped Duty now sits far below the
// 1,000 default, so the exemption has NO instance today. No rune count is
// written here on purpose — the seed changes far more often than this comment.
// Either way the tests below always write their own Duty text first, so they
// never depend on the seed's size — that is why neither number is asserted here.

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"
)

// capsTestServer returns a server whose three role-journal caps are set to
// THREE DIFFERENT numbers. Distinct values are the whole discriminating power
// of this file: with one shared cap (the pre-T-ae38 world) "Duty was judged by
// 1200" and "Duty was judged by the shared cap" are indistinguishable.
func capsTestServer(t *testing.T, duty, insight, learning int) *apiServer {
	t.Helper()
	api := newTasksTestServer(t)
	api.settingsMu.Lock()
	api.docCapCharsDuty = duty
	api.docCapCharsInsight = insight
	api.docCapCharsLearning = learning
	api.settingsMu.Unlock()
	return api
}

// runes builds an n-rune multi-byte document (see the file header).
func runesDoc(t *testing.T, n int) string {
	t.Helper()
	s := strings.Repeat("界", n)
	if utf8.RuneCountInString(s) != n {
		t.Fatalf("fixture is %d runes, want %d", utf8.RuneCountInString(s), n)
	}
	return s
}

func ownerReq(t *testing.T, method, path string, body any) *http.Request {
	t.Helper()
	return taskReq(t, method, path, body, "owner", "owner")
}

func writeDutyOn(t *testing.T, api *apiServer, role, text string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	api.HandleUpdateRoleApiRolesRolePost(rec,
		ownerReq(t, http.MethodPost, "/api/roles/"+role, map[string]any{"definition_md": text}), role)
	return rec
}

func writeInsightOn(t *testing.T, api *apiServer, role, text string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	api.HandleReplaceInsightApiInsightRoleKeyPost(rec,
		ownerReq(t, http.MethodPost, "/api/insight/"+role, map[string]any{"text": text}), role)
	return rec
}

func writeLearningOn(t *testing.T, api *apiServer, role, text string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	api.HandleReplaceLessonsApiLessonsRoleKeyPost(rec,
		ownerReq(t, http.MethodPost, "/api/lessons/"+role, map[string]any{"text": text}),
		role)
	return rec
}

// ── (1) Duty has a cap at the EDIT door ──────────────────────────────────────

// TestDutyCapRefusesAnOverCapEdit — the gap the owner opened this ticket for on
// the Duty side: HandleUpdateRoleApiRolesRolePost ran from body.DefinitionMd
// straight to SQL with no size guard of any kind.
//
// The positive control comes FIRST and is not a formality: a handler that
// refused everything (a 400 for an unrelated reason, a broken fixture) would
// satisfy the refusal assertion alone.
func TestDutyCapRefusesAnOverCapEdit(t *testing.T) {
	const dutyCap = 1200
	api := capsTestServer(t, dutyCap, 9000, 30000)
	role := seedRoleAssistant

	// CONTROL: exactly AT the cap must LAND. The cap is an upper bound, not an
	// exclusive one, and this also proves the door is open at all.
	atCap := runesDoc(t, dutyCap)
	if rec := writeDutyOn(t, api, role, atCap); rec.Code != http.StatusOK {
		t.Fatalf("a Duty write exactly AT the cap must land: status=%d body=%s",
			rec.Code, rec.Body.String())
	}
	folded, err := api.foldRoleDefDTO(role)
	if err != nil || folded == nil {
		t.Fatalf("foldRoleDefDTO: %+v %v", folded, err)
	}
	if folded.DefinitionMD != atCap {
		t.Fatalf("the at-cap write must actually be stored (%d runes live)",
			utf8.RuneCountInString(folded.DefinitionMD))
	}

	// One rune over, and not shorter than what is stored → refused, nothing written.
	over := runesDoc(t, dutyCap+1)
	rec := writeDutyOn(t, api, role, over)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("a Duty write over the cap must be refused, got %d: %s", rec.Code, rec.Body.String())
	}
	// The refusal must name THIS segment's cap. Before T-ae38 there was one
	// number for everything, so a message quoting 9000 or 30000 would mean the
	// write was judged by the wrong document's budget.
	if body := rec.Body.String(); !strings.Contains(body, strconv.Itoa(dutyCap)) {
		t.Fatalf("the refusal must quote the DUTY cap %d, got %s", dutyCap, body)
	}
	if body := rec.Body.String(); strings.Contains(body, "9000") || strings.Contains(body, "30000") {
		t.Fatalf("the Duty refusal quotes another segment's cap: %s", body)
	}
	after, err := api.foldRoleDefDTO(role)
	if err != nil || after == nil {
		t.Fatalf("foldRoleDefDTO: %+v %v", after, err)
	}
	if after.DefinitionMD != atCap {
		t.Fatalf("a refused Duty write must leave the stored doc untouched")
	}
}

// ── (2) Duty has a cap at the RESTORE door too ───────────────────────────────

// TestDutyCapRefusesAnOverCapRestore is the branch that makes the edit-door
// check worth anything. It is written as the bypass itself: shrink the doc to
// under the cap, then try to walk it back over via history. If only one door is
// guarded, this restore lands and the cap is a suggestion.
func TestDutyCapRefusesAnOverCapRestore(t *testing.T) {
	const dutyCap = 1000
	api := capsTestServer(t, dutyCap, 9000, 30000)
	role := seedRoleAssistant

	// An over-cap Duty exists in the DB — exactly the day-one situation, since
	// Duty was uncapped until this ticket. Written through the DAL, not the
	// handler: the handler now refuses it, and "what is already stored is never
	// truncated" is the rule being exercised.
	oversized := runesDoc(t, dutyCap+3000)
	if err := api.dal.PutRoleDef(RoleDef{RoleKey: role, Name: "Assistant", DefinitionMD: oversized}); err != nil {
		t.Fatal(err)
	}

	// Shrink it — allowed even from over-cap, and it retains the long version.
	if rec := writeDutyOn(t, api, role, runesDoc(t, 40)); rec.Code != http.StatusOK {
		t.Fatalf("an over-cap Duty must still be allowed to get SHORTER: %d %s",
			rec.Code, rec.Body.String())
	}
	stored, err := api.dal.ListDocumentHistory("role_definition", role)
	if err != nil || len(stored) == 0 {
		t.Fatalf("the shrinking write must retain the long revision: %+v %v", stored, err)
	}

	restore := func() *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		id := stored[0].ID
		api.HandleRestoreDocumentHistoryApiDocumentHistoryKindKeyIdRestorePost(rec,
			ownerReq(t, http.MethodPost, "/api/document-history/role_definition/"+role+"/"+
				strconv.FormatInt(id, 10)+"/restore", nil),
			"role_definition", role, id)
		return rec
	}

	// THE ASSERTION THIS FILE EXISTS FOR — and it has to name WHY the write was
	// refused, not just that it was.
	//
	// 🔴 A bare `!= 400` here is only discriminating by ACCIDENT of the current
	// code shape: restoreDocumentHistory's role_definition branch has exactly ONE
	// error that the handler turns into a 400 (errDocumentHistoryCap); everything
	// else falls through to internalError, a 500. So today "it was 400" happens to
	// imply "the cap stopped it" — a guarantee that lives in the PRODUCTION code,
	// not in this test. The day someone adds a second 400-answering branch to that
	// case (a bad revision payload, a tombstone rule, an authz refusal moved
	// inward), this test keeps passing while asserting nothing about the cap, and
	// nothing anywhere would say so. The edit-door test above already judges its
	// refusal by content (it quotes the Duty cap and rules out the other two
	// segments' numbers); the restore door is judged the same way here.
	//
	// The named error is the thing to match: errDocumentHistoryCap is what the
	// cap check returns and what the handler renders into the body, so this binds
	// to the cap branch specifically rather than to a message someone might
	// reword. Status and reason are checked TOGETHER on purpose — split in two,
	// deleting the cap check makes the restore land with a 200 and only the
	// status half would report it.
	if rec := restore(); rec.Code != http.StatusBadRequest ||
		!strings.Contains(rec.Body.String(), errDocumentHistoryCap.Error()) {
		t.Fatalf("restoring an over-cap Duty revision must be refused BY THE CAP: "+
			"want %d saying %q, got %d: %s "+
			"(edit-door-only means: edit to 999, restore 4000, cap gone)",
			http.StatusBadRequest, errDocumentHistoryCap.Error(), rec.Code, rec.Body.String())
	}
	live, err := api.foldRoleDefDTO(role)
	if err != nil || live == nil {
		t.Fatalf("foldRoleDefDTO: %+v %v", live, err)
	}
	if utf8.RuneCountInString(live.DefinitionMD) != 40 {
		t.Fatalf("a refused restore must not have written anything (%d runes live)",
			utf8.RuneCountInString(live.DefinitionMD))
	}

	// CONTROL: the restore itself works — raise the Duty cap above the revision
	// and the SAME restore lands. Without this, the refusal above could equally
	// mean the restore path is broken for role_definition altogether.
	api.settingsMu.Lock()
	api.docCapCharsDuty = dutyCap + 5000
	api.settingsMu.Unlock()
	if rec := restore(); rec.Code != http.StatusOK {
		t.Fatalf("after raising the Duty cap the same restore must land, got %d: %s",
			rec.Code, rec.Body.String())
	}
	live, err = api.foldRoleDefDTO(role)
	if err != nil || live == nil {
		t.Fatalf("foldRoleDefDTO: %+v %v", live, err)
	}
	if live.DefinitionMD != oversized {
		t.Fatalf("the restore must actually put the revision back (%d runes live)",
			utf8.RuneCountInString(live.DefinitionMD))
	}
}

// ── (3) the three segments are independent ───────────────────────────────────

// TestEachSegmentIsJudgedByItsOwnCap presses each of the three documents to a
// size that is legal under its OWN cap and illegal under at least one of the
// others. On a server with one shared cap this cannot pass whichever number
// that cap holds — that is the discriminating property, and it is why the three
// caps here are deliberately far apart.
func TestEachSegmentIsJudgedByItsOwnCap(t *testing.T) {
	const dutyCap, insightCap, learningCap = 1000, 5000, 20000
	api := capsTestServer(t, dutyCap, insightCap, learningCap)
	role := seedRoleAssistant

	// Each write is comfortably over the SMALLER caps and under its own.
	if rec := writeDutyOn(t, api, role, runesDoc(t, dutyCap)); rec.Code != http.StatusOK {
		t.Fatalf("Duty at its own cap must land: %d %s", rec.Code, rec.Body.String())
	}
	if rec := writeInsightOn(t, api, role, runesDoc(t, insightCap)); rec.Code != http.StatusOK {
		t.Fatalf("Insight at its own cap (%d, over the Duty cap) must land: %d %s",
			insightCap, rec.Code, rec.Body.String())
	}
	if rec := writeLearningOn(t, api, role, runesDoc(t, learningCap)); rec.Code != http.StatusOK {
		t.Fatalf("Learning at its own cap (%d, over both others) must land: %d %s",
			learningCap, rec.Code, rec.Body.String())
	}

	// And each is still a cap: one rune over its OWN number is refused, even
	// though that size is legal under a LARGER sibling.
	if rec := writeDutyOn(t, api, role, runesDoc(t, dutyCap+1)); rec.Code != http.StatusBadRequest {
		t.Fatalf("Duty must be refused above %d even though Insight/Learning allow it: %d",
			dutyCap, rec.Code)
	}
	if rec := writeInsightOn(t, api, role, runesDoc(t, insightCap+1)); rec.Code != http.StatusBadRequest {
		t.Fatalf("Insight must be refused above %d even though Learning allows it: %d",
			insightCap, rec.Code)
	}
	if rec := writeLearningOn(t, api, role, runesDoc(t, learningCap+1)); rec.Code != http.StatusBadRequest {
		t.Fatalf("Learning must be refused above %d: %d", learningCap, rec.Code)
	}
}

// TestReadFacesReportTheirOwnSegmentsCap — the numbers an agent sizes its next
// edit against. Duty carried NEITHER field before T-ae38, which is the concrete
// cost the ticket names: an agent that had just condensed its own role
// definition had to ask someone else to measure the doc.
func TestReadFacesReportTheirOwnSegmentsCap(t *testing.T) {
	const dutyCap, insightCap, learningCap = 1000, 5000, 20000
	api := capsTestServer(t, dutyCap, insightCap, learningCap)
	role := seedRoleAssistant

	duty := runesDoc(t, 37)
	if rec := writeDutyOn(t, api, role, duty); rec.Code != http.StatusOK {
		t.Fatalf("seed duty: %d %s", rec.Code, rec.Body.String())
	}
	got, err := api.foldRoleDefDTO(role)
	if err != nil || got == nil {
		t.Fatalf("foldRoleDefDTO: %+v %v", got, err)
	}
	// SizeChars must be RUNES: `duty` is 37 runes / 111 bytes, so a byte count
	// cannot masquerade as a pass here.
	if got.SizeChars != 37 {
		t.Fatalf("RoleDefDTO.size_chars must be RUNES: got %d want 37 (bytes=%d)",
			got.SizeChars, len(duty))
	}
	if got.CapChars != dutyCap {
		t.Fatalf("RoleDefDTO.cap_chars must be the DUTY cap %d, got %d", dutyCap, got.CapChars)
	}

	insight, err := api.foldInsightDTO(role)
	if err != nil {
		t.Fatal(err)
	}
	if insight.CapChars != insightCap {
		t.Fatalf("InsightDTO.cap_chars must be the INSIGHT cap %d, got %d", insightCap, insight.CapChars)
	}
	lessons, err := api.foldLessonsDTO(role)
	if err != nil {
		t.Fatal(err)
	}
	if lessons.CapChars != learningCap {
		t.Fatalf("LessonsDTO.cap_chars must be the LEARNING cap %d, got %d", learningCap, lessons.CapChars)
	}
}

// ── (4) the standing rule holds identically for all three ────────────────────

// TestOverCapDocsMayOnlyGetShorter_AllThreeSegments — the rule that predates
// this ticket ("what is already over the cap is never truncated, but its next
// update may only make it smaller") applied to lessons and the manual. Duty had
// no cap, so it had no such rule; Insight and Learning shared one number, so
// "the rule holds for both" was never actually distinguishable from "the rule
// holds for the one shared cap".
//
// Each segment gets the SAME three-step story, at THREE DIFFERENT caps:
//
//	over-cap doc  →  same length again      REFUSED (equal is not converging)
//	over-cap doc  →  longer                 REFUSED
//	over-cap doc  →  shorter (still over)   ALLOWED — the escape hatch every
//	                                        already-over-cap doc depends on
func TestOverCapDocsMayOnlyGetShorter_AllThreeSegments(t *testing.T) {
	const dutyCap, insightCap, learningCap = 1000, 5000, 20000
	role := seedRoleAssistant

	segments := []struct {
		name string
		cap  int
		// seed installs an over-cap doc WITHOUT going through the capped door
		// (the door now refuses it — that is the point).
		seed  func(t *testing.T, api *apiServer, text string)
		write func(t *testing.T, api *apiServer, text string) *httptest.ResponseRecorder
		read  func(t *testing.T, api *apiServer) string
	}{
		{
			name: "duty", cap: dutyCap,
			seed: func(t *testing.T, api *apiServer, text string) {
				if err := api.dal.PutRoleDef(RoleDef{RoleKey: role, Name: "Assistant", DefinitionMD: text}); err != nil {
					t.Fatal(err)
				}
			},
			write: func(t *testing.T, api *apiServer, text string) *httptest.ResponseRecorder {
				return writeDutyOn(t, api, role, text)
			},
			read: func(t *testing.T, api *apiServer) string {
				got, err := api.foldRoleDefDTO(role)
				if err != nil || got == nil {
					t.Fatalf("foldRoleDefDTO: %+v %v", got, err)
				}
				return got.DefinitionMD
			},
		},
		{
			name: "insight", cap: insightCap,
			seed: func(t *testing.T, api *apiServer, text string) {
				if err := api.dal.PutInsight(Insight{RoleKey: role, Text: text}); err != nil {
					t.Fatal(err)
				}
			},
			write: func(t *testing.T, api *apiServer, text string) *httptest.ResponseRecorder {
				return writeInsightOn(t, api, role, text)
			},
			read: func(t *testing.T, api *apiServer) string {
				got, err := api.foldInsightDTO(role)
				if err != nil {
					t.Fatal(err)
				}
				return got.Text
			},
		},
		{
			name: "learning", cap: learningCap,
			seed: func(t *testing.T, api *apiServer, text string) {
				if err := api.dal.PutLessons(Lessons{RoleKey: role, Text: text}); err != nil {
					t.Fatal(err)
				}
			},
			write: func(t *testing.T, api *apiServer, text string) *httptest.ResponseRecorder {
				return writeLearningOn(t, api, role, text)
			},
			read: func(t *testing.T, api *apiServer) string {
				got, err := api.foldLessonsDTO(role)
				if err != nil {
					t.Fatal(err)
				}
				return got.Text
			},
		},
	}

	for _, seg := range segments {
		t.Run(seg.name, func(t *testing.T) {
			api := capsTestServer(t, dutyCap, insightCap, learningCap)
			stored := runesDoc(t, seg.cap+500)
			seg.seed(t, api, stored)

			// Not truncated on arrival: the cap only ever refuses a WRITE.
			if got := seg.read(t, api); utf8.RuneCountInString(got) != seg.cap+500 {
				t.Fatalf("%s: an over-cap doc must never be truncated, %d runes live",
					seg.name, utf8.RuneCountInString(got))
			}

			// Equal length is NOT converging — refused, boundary included.
			equal := runesDoc(t, seg.cap+500)
			if rec := seg.write(t, api, equal); rec.Code != http.StatusBadRequest {
				t.Fatalf("%s: an equal-length rewrite of an over-cap doc must be refused, got %d: %s",
					seg.name, rec.Code, rec.Body.String())
			}

			// Longer — refused.
			if rec := seg.write(t, api, runesDoc(t, seg.cap+900)); rec.Code != http.StatusBadRequest {
				t.Fatalf("%s: growing an over-cap doc must be refused, got %d", seg.name, rec.Code)
			}

			// Shorter but STILL over the cap — allowed. This is the escape
			// hatch; without it every doc that is over-cap today (every Duty
			// longer than 1000, on day one) would be frozen forever.
			shrunk := runesDoc(t, seg.cap+200)
			if rec := seg.write(t, api, shrunk); rec.Code != http.StatusOK {
				t.Fatalf("%s: an over-cap doc must be free to converge DOWNWARD, got %d: %s",
					seg.name, rec.Code, rec.Body.String())
			}
			if got := seg.read(t, api); got != shrunk {
				t.Fatalf("%s: the shrinking write must actually be stored (%d runes live)",
					seg.name, utf8.RuneCountInString(got))
			}
		})
	}
}
