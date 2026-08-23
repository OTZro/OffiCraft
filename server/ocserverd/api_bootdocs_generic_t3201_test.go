package main

// api_bootdocs_generic_t3201_test.go — the ONE route family that reaches every
// editable boot-context document (T-3201), instead of three named routes per
// document.
//
// WHAT IS WORTH TESTING HERE IS NOT "does it fold". The folding, the cap, the
// wipe guard, the read-only refusal and the variable gate all belong to
// replaceBootDoc / resetBootDoc / foldBootDocDTO, which the per-document faces
// have exercised since T-791e and which the event-procedure tests cover kind by
// kind. What is NEW is the ADDRESSING: a pair of path segments now chooses the
// document, so the failure mode this file exists for is a request that resolves
// to the WRONG document — or to none, silently.

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func jsonPost(body string) *http.Request {
	r := httptest.NewRequest(http.MethodPost, "/x", strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	return r
}

func getBootDocHTTP(t *testing.T, s *apiServer, kind, key string) (int, bootDocDTO) {
	t.Helper()
	w := httptest.NewRecorder()
	s.HandleGetBootDocApiBootDocsKindKeyGet(w, httptest.NewRequest(http.MethodGet, "/x", nil), BootDocKind(kind), key)
	var dto bootDocDTO
	if w.Code == http.StatusOK {
		if err := json.Unmarshal(w.Body.Bytes(), &dto); err != nil {
			t.Fatalf("decode %s/%s: %v", kind, key, err)
		}
	}
	return w.Code, dto
}

// ── the enum is the wire's only answer to "which documents exist" ───────────

// 🔴 THE SET IS READ OUT OF THE FROZEN SPEC, NOT WRITTEN DOWN HERE. A list in
// this file would have to be edited by the same commit that adds a document,
// and a list that is edited alongside the thing it guards is not a guard.
//
// This is the server half of the pair that replaced GET /api/boot-docs
// (T-3201, owner's ruling 「加上 enum 並且前端自己寫死」). The listing could not
// go stale, but it also could not make anything FAIL: a cockpit that had never
// heard of a new document simply showed nothing. The enum can — the cockpit
// indexes its row table by it and does not compile without a place to show a
// new value — and this test is what keeps the enum honest about the registry
// that actually serves these documents. Either side drifting reddens here.
func TestBootDocRegistry_MatchesTheBootDocKindEnumInTheFrozenSpec(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "spec", "openapi.json"))
	if err != nil {
		t.Fatal(err)
	}
	var spec struct {
		Components struct {
			Schemas struct {
				BootDocKind struct {
					Enum []string `json:"enum"`
				} `json:"BootDocKind"`
			} `json:"schemas"`
		} `json:"components"`
	}
	if err := json.Unmarshal(raw, &spec); err != nil {
		t.Fatal(err)
	}
	inSpec := map[string]bool{}
	for _, kind := range spec.Components.Schemas.BootDocKind.Enum {
		inSpec[kind] = true
		if !BootDocKind(kind).Valid() {
			t.Errorf("spec/openapi.json lists %q but the generated enum does not know it — regenerate ocapi_gen.go", kind)
		}
	}
	if len(inSpec) == 0 {
		t.Fatal("spec/openapi.json carries no BootDocKind enum — the address vocabulary has no source")
	}
	inRegistry := map[string]bool{}
	for _, reg := range bootDocRegistry {
		inRegistry[reg.Kind] = true
		if !inSpec[reg.Kind] {
			t.Errorf("%s is served by bootDocRegistry but is not in the BootDocKind enum — no caller can address it and no cockpit can show it", reg.Kind)
		}
	}
	for kind := range inSpec {
		if !inRegistry[kind] {
			t.Errorf("the BootDocKind enum lists %s, which no registry row serves — every address it appears in is a 404", kind)
		}
	}
}

// read_only is what tells a cockpit whether to render an editor at all. It has
// to be true for exactly the documents whose write faces refuse — a document
// served as editable whose save is a 405 costs the owner the edit he typed.
//
// The listing used to answer this in one row per document; the per-document
// read answers it now, so the sweep walks the registry instead.
func TestGetBootDoc_ReadOnlyMatchesTheWriteFacesRefusal(t *testing.T) {
	s := newEventProcServer(t)
	sawReadOnly := false
	for _, reg := range bootDocRegistry {
		for _, key := range reg.Keys {
			code, dto := getBootDocHTTP(t, s, reg.Kind, key)
			if code != http.StatusOK {
				t.Fatalf("%s/%s: status = %d", reg.Kind, key, code)
			}
			rec := httptest.NewRecorder()
			s.HandleResetBootDocApiBootDocsKindKeyResetPost(rec, ownerPost("/x"), BootDocKind(reg.Kind), key)
			refused := rec.Code == http.StatusMethodNotAllowed
			if refused != dto.ReadOnly {
				t.Errorf("%s/%s: the read says read_only=%v, the reset face answers %d",
					reg.Kind, key, dto.ReadOnly, rec.Code)
			}
			sawReadOnly = sawReadOnly || dto.ReadOnly
		}
	}
	// Positive control: without it every document answering read_only=false
	// would pass on a build where the refusal had been removed entirely.
	if !sawReadOnly {
		t.Error("no document reports read_only — the refusal this test measures is not reachable")
	}
}

// ── addressing: the pair chooses the document, or nothing at all ─────────────

func TestGetBootDoc_UnknownKindAndKeyAre404ThatNameTheKeys(t *testing.T) {
	s := newEventProcServer(t)

	code, _ := getBootDocHTTP(t, s, "no_such_kind", bootDocSingletonKey)
	if code != http.StatusNotFound {
		t.Errorf("unknown kind: status = %d, want 404", code)
	}

	// A REAL kind with a key it does not serve. This is the case a bare "not
	// found" leaves unresolvable: boot_sequence serves two keys and neither is
	// guessable from the kind, so the refusal has to name them.
	w := httptest.NewRecorder()
	s.HandleGetBootDocApiBootDocsKindKeyGet(w, httptest.NewRequest(http.MethodGet, "/x", nil),
		docKindBootSequence, "opus")
	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404: %s", w.Code, w.Body.String())
	}
	for _, key := range []string{bootSequenceKeyClaude, bootSequenceKeyCodex} {
		if !strings.Contains(w.Body.String(), key) {
			t.Errorf("the refusal does not name %q: %s", key, w.Body.String())
		}
	}
}

// The generic face and the named face are two doors onto ONE document. If they
// ever fold differently, the cockpit and an agent are reading different text
// while both believe they hold 系統互動.
func TestGetBootDoc_AgreesWithTheNamedRouteForTheSameDocument(t *testing.T) {
	s := newEventProcServer(t)

	named := httptest.NewRecorder()
	s.HandleGetSystemInteractionApiSystemInteractionGet(named,
		httptest.NewRequest(http.MethodGet, "/api/system-interaction", nil))
	if named.Code != http.StatusOK {
		t.Fatalf("named route: %d", named.Code)
	}
	code, generic := getBootDocHTTP(t, s, docKindSystemInteraction, systemInteractionDocKey)
	if code != http.StatusOK {
		t.Fatalf("generic route: %d", code)
	}
	var namedDTO bootDocDTO
	if err := json.Unmarshal(named.Body.Bytes(), &namedDTO); err != nil {
		t.Fatal(err)
	}
	if generic != namedDTO {
		t.Errorf("the two faces of one document disagree:\ngeneric=%+v\nnamed  =%+v", generic, namedDTO)
	}
}

// Every key of every kind must resolve to ITS OWN document. The failure this
// pins is the one a generic route makes possible and named routes cannot: a
// resolver that ignores the key and answers the kind's first document for all
// of them — which for boot_sequence means serving codex agents the claude
// checklist, and nothing that never boots reports it.
func TestGetBootDoc_EveryAddressResolvesToItsOwnDocument(t *testing.T) {
	s := newEventProcServer(t)
	for _, reg := range bootDocRegistry {
		for _, key := range reg.Keys {
			code, dto := getBootDocHTTP(t, s, reg.Kind, key)
			if code != http.StatusOK {
				t.Errorf("%s/%s: status = %d", reg.Kind, key, code)
				continue
			}
			if dto.Kind != reg.Kind || dto.Key != key {
				t.Errorf("asked for %s/%s, got %s/%s", reg.Kind, key, dto.Kind, dto.Key)
			}
			if dto.Text == "" {
				t.Errorf("%s/%s folded to empty text", reg.Kind, key)
			}
		}
	}
}

// ── the write faces reach the same guards through the new address ───────────

func TestReplaceBootDocRoute_WritesThroughTheAddressAndReadsBack(t *testing.T) {
	s := newEventProcServer(t)
	const kind = docKindAcceleratedStop

	_, before := getBootDocHTTP(t, s, kind, bootDocSingletonKey)
	if before.ReadOnlyHead == "" {
		t.Fatalf("%s serves no read-only head — this test's fixture assumption is stale", kind)
	}
	const body = "照這份走。"

	w := httptest.NewRecorder()
	s.HandleReplaceBootDocApiBootDocsKindKeyPost(w,
		jsonPost(`{"body":`+mustJSONString(body)+`}`), BootDocKind(kind), bootDocSingletonKey)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", w.Code, w.Body.String())
	}

	code, after := getBootDocHTTP(t, s, kind, bootDocSingletonKey)
	if code != http.StatusOK {
		t.Fatalf("read back: %d", code)
	}
	// 🔴 THE READ-BACK IS NOT BYTE-IDENTICAL TO WHAT WAS SENT ANY MORE, and that
	// is the ruling rather than a regression: the wire carries the body, the
	// server stores the SHIPPED head above it. What must be byte-identical is
	// the BODY — the field the write face takes and the read face answers with.
	if after.Body != body {
		t.Errorf("read back body %q, wrote %q", after.Body, body)
	}
	if after.ReadOnlyHead != before.ReadOnlyHead {
		t.Errorf("the read-only head moved:\n before=%q\n after =%q", before.ReadOnlyHead, after.ReadOnlyHead)
	}
	if after.Text != DocJoinHeadBody(before.ReadOnlyHead, body) {
		t.Errorf("stored text is not head ⊕ body:\n%q", after.Text)
	}
	if after.IsDefault {
		t.Error("is_default is still true after a write")
	}

	// And the OTHER documents did not move — the address wrote one of them.
	for _, kindOther := range eventProcKinds() {
		if kindOther == kind {
			continue
		}
		_, dto := getBootDocHTTP(t, s, kindOther, bootDocSingletonKey)
		if !dto.IsDefault {
			t.Errorf("writing %s also moved %s", kind, kindOther)
		}
	}

	// reset through the same address puts the shipped text back.
	rec := httptest.NewRecorder()
	s.HandleResetBootDocApiBootDocsKindKeyResetPost(rec, ownerPost("/x"), BootDocKind(kind), bootDocSingletonKey)
	if rec.Code != http.StatusOK {
		t.Fatalf("reset: %d %s", rec.Code, rec.Body.String())
	}
	_, restored := getBootDocHTTP(t, s, kind, bootDocSingletonKey)
	if restored.Text != before.Text || !restored.IsDefault {
		t.Errorf("reset did not restore the shipped text (is_default=%v)", restored.IsDefault)
	}
}

func TestReplaceBootDocRoute_ReadOnlyKindRefusesThroughTheGenericAddress(t *testing.T) {
	s := newEventProcServer(t)
	for _, kind := range readOnlyEventProcKinds() {
		t.Run(kind, func(t *testing.T) {
			w := httptest.NewRecorder()
			s.HandleReplaceBootDocApiBootDocsKindKeyPost(w,
				jsonPost(`{"body":"換掉"}`), BootDocKind(kind), bootDocSingletonKey)
			if w.Code != http.StatusMethodNotAllowed {
				t.Errorf("replace: status = %d, want 405 (%s)", w.Code, w.Body.String())
			}
			rec := httptest.NewRecorder()
			s.HandleResetBootDocApiBootDocsKindKeyResetPost(rec, ownerPost("/x"), BootDocKind(kind), bootDocSingletonKey)
			if rec.Code != http.StatusMethodNotAllowed {
				t.Errorf("reset: status = %d, want 405 (%s)", rec.Code, rec.Body.String())
			}
			_, dto := getBootDocHTTP(t, s, kind, bootDocSingletonKey)
			if !dto.IsDefault || !dto.ReadOnly {
				t.Errorf("after two refusals: is_default=%v read_only=%v", dto.IsDefault, dto.ReadOnly)
			}
		})
	}
}
