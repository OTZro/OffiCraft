package main

// api_doc_sizes_test.go — peek_doc_sizes: the answer costs the same whatever the
// documents hold, and each of the five segments answers to its OWN cap.
//
// 🔴 THE FIXTURES ARE MULTI-BYTE, for the reason the two neighbouring cap files
// state: sizes are counted in RUNES, and an ASCII fixture cannot tell a rune
// count from a byte count.

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// peekDocSizes calls GET /api/doc-sizes and returns the raw body plus the
// decoded overview. The RAW body is what the content test measures — decoding
// first would throw away the one number that test is about.
func peekDocSizes(t *testing.T, api *apiServer) (string, docSizesDTO) {
	t.Helper()
	rec := httptest.NewRecorder()
	api.HandlePeekDocSizesApiDocSizesGet(rec,
		taskReq(t, http.MethodGet, "/api/doc-sizes", nil, "m-exec", "agent"))
	if rec.Code != http.StatusOK {
		t.Fatalf("peek_doc_sizes: %d %s", rec.Code, rec.Body.String())
	}
	var out docSizesDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode doc-sizes response: %v (body %s)", err, rec.Body.String())
	}
	return rec.Body.String(), out
}

// writeEveryCappedDoc puts an n-rune document into all FIVE capped segments and
// returns the fixture text, so a caller can assert on its size.
func writeEveryCappedDoc(t *testing.T, api *apiServer, role, manualKey string, n int) string {
	t.Helper()
	doc := runesDoc(t, n)
	if rec := writeDutyOn(t, api, role, doc); rec.Code != http.StatusOK {
		t.Fatalf("write duty (%d runes): %d %s", n, rec.Code, rec.Body.String())
	}
	if rec := writeInsightOn(t, api, role, doc); rec.Code != http.StatusOK {
		t.Fatalf("write insight (%d runes): %d %s", n, rec.Code, rec.Body.String())
	}
	if rec := writeLearningOn(t, api, role, doc); rec.Code != http.StatusOK {
		t.Fatalf("write lessons (%d runes): %d %s", n, rec.Code, rec.Body.String())
	}
	rec := updateManual(t, api, manualKey, map[string]any{"sop_md": doc, "learnings": doc})
	if rec.Code != http.StatusOK {
		t.Fatalf("write manual sop_md+learnings (%d runes): %d %s", n, rec.Code, rec.Body.String())
	}
	return doc
}

func listTaskManualsBody(t *testing.T, api *apiServer) string {
	t.Helper()
	rec := httptest.NewRecorder()
	api.HandleListTaskManualsApiTaskManualsGet(rec,
		taskReq(t, http.MethodGet, "/api/task-manuals", nil, "m-exec", "agent"),
		HandleListTaskManualsApiTaskManualsGetParams{})
	if rec.Code != http.StatusOK {
		t.Fatalf("list_task_manuals: %d %s", rec.Code, rec.Body.String())
	}
	return rec.Body.String()
}

// TestPeekDocSizesResponseDoesNotGrowWithDocumentContent is the whole reason
// this endpoint exists, pinned the only way that actually proves it: put a much
// LARGER document into every capped segment and require the response to come
// back BYTE-FOR-BYTE THE SAME LENGTH.
//
// 🔴 Asserting "the `text` / `sop_md` field is absent" would NOT prove this. A
// handler that renamed the field, nested the body one level deeper, or returned
// a "preview" prefix would pass that assertion while still charging the caller
// for the library. Length under growth is the property; field absence is one of
// its symptoms.
//
// The two fixture sizes are 10000 and 99999 runes — a ~10x difference, and the
// same number of DIGITS, so an unchanged response length is exact equality and
// needs no tolerance. The two controls stop this from passing vacuously: the
// reported sizes must actually move (the writes landed and are reflected), and
// list_task_manuals — the path this endpoint exists to replace — must grow by
// roughly the document growth on the very same fixture.
func TestPeekDocSizesResponseDoesNotGrowWithDocumentContent(t *testing.T) {
	const small, large = 10000, 99999
	api := capsTestServer(t, maxDocCapChars, maxDocCapChars, maxDocCapChars)
	api.settingsMu.Lock()
	api.docCapCharsManualSop = maxDocCapChars
	api.docCapCharsManualLearnings = maxDocCapChars
	api.settingsMu.Unlock()
	role := seedRoleAssistant
	manualKey := seedManualWithLearnings(t, api, "")

	writeEveryCappedDoc(t, api, role, manualKey, small)
	beforeBody, before := peekDocSizes(t, api)
	beforeOldPath := len(listTaskManualsBody(t, api))

	writeEveryCappedDoc(t, api, role, manualKey, large)
	afterBody, after := peekDocSizes(t, api)
	afterOldPath := len(listTaskManualsBody(t, api))

	if len(afterBody) != len(beforeBody) {
		t.Fatalf("the response grew with the documents: %d -> %d bytes\nbefore=%s\nafter=%s",
			len(beforeBody), len(afterBody), beforeBody, afterBody)
	}

	// CONTROL 1 — the writes really landed and this endpoint really reports
	// them. Without it, a handler that answered a constant would pass above.
	if len(before.Roles) != 1 || len(after.Roles) != 1 ||
		len(before.TaskManuals) != 1 || len(after.TaskManuals) != 1 {
		t.Fatalf("fixture shape changed: before=%+v after=%+v", before, after)
	}
	for _, c := range []struct {
		name          string
		small, larger int
	}{
		{"duty", before.Roles[0].Duty.SizeChars, after.Roles[0].Duty.SizeChars},
		{"insight", before.Roles[0].Insight.SizeChars, after.Roles[0].Insight.SizeChars},
		{"lessons", before.Roles[0].Lessons.SizeChars, after.Roles[0].Lessons.SizeChars},
		{"manual sop", before.TaskManuals[0].Sop.SizeChars, after.TaskManuals[0].Sop.SizeChars},
		{"manual learnings", before.TaskManuals[0].Learnings.SizeChars, after.TaskManuals[0].Learnings.SizeChars},
	} {
		if c.small != small || c.larger != large {
			t.Fatalf("%s must report %d then %d, got %d then %d",
				c.name, small, large, c.small, c.larger)
		}
	}

	// CONTROL 2 — the SAME growth on the SAME fixture, measured through the
	// path this endpoint replaces. If list_task_manuals did not blow up here,
	// the fixture never grew and control 1 alone could be satisfied by luck.
	if afterOldPath-beforeOldPath < large-small {
		t.Fatalf("list_task_manuals should have grown by at least the document growth "+
			"(%d runes) — it grew %d bytes (%d -> %d); the fixture is not exercising the problem",
			large-small, afterOldPath-beforeOldPath, beforeOldPath, afterOldPath)
	}
}

// TestPeekDocSizesReportsEachSegmentAgainstItsOwnCap sets all FIVE caps to five
// DIFFERENT numbers and requires each document to come back beside its own.
//
// 🔴 Five distinct numbers is the entire discriminating power here, for the
// reason api_doc_caps_tae38_test.go states about its three: while the caps are
// equal, "lessons was reported against the lessons cap" and "lessons was
// reported against a shared cap" are the same sentence, and a handler that read
// one accessor five times would pass. The five documents are five different
// SIZES for the mirrored reason — it also catches a field swap, which equal
// sizes would hide.
func TestPeekDocSizesReportsEachSegmentAgainstItsOwnCap(t *testing.T) {
	const (
		dutyCap, insightCap, learningCap   = 1200, 9000, 30000
		manualSopCap, manualLearningsCap   = 41000, 52000
		dutySize, insightSize, lessonsSize = 111, 222, 333
		manualSopSize, manualLearningsSize = 444, 555
	)
	api := capsTestServer(t, dutyCap, insightCap, learningCap)
	api.settingsMu.Lock()
	api.docCapCharsManualSop = manualSopCap
	api.docCapCharsManualLearnings = manualLearningsCap
	api.settingsMu.Unlock()
	role := seedRoleAssistant
	manualKey := seedManualWithLearnings(t, api, "")

	if rec := writeDutyOn(t, api, role, runesDoc(t, dutySize)); rec.Code != http.StatusOK {
		t.Fatalf("write duty: %d %s", rec.Code, rec.Body.String())
	}
	if rec := writeInsightOn(t, api, role, runesDoc(t, insightSize)); rec.Code != http.StatusOK {
		t.Fatalf("write insight: %d %s", rec.Code, rec.Body.String())
	}
	if rec := writeLearningOn(t, api, role, runesDoc(t, lessonsSize)); rec.Code != http.StatusOK {
		t.Fatalf("write lessons: %d %s", rec.Code, rec.Body.String())
	}
	if rec := updateManual(t, api, manualKey, map[string]any{
		"sop_md":    runesDoc(t, manualSopSize),
		"learnings": runesDoc(t, manualLearningsSize),
	}); rec.Code != http.StatusOK {
		t.Fatalf("write manual: %d %s", rec.Code, rec.Body.String())
	}

	_, out := peekDocSizes(t, api)
	if len(out.Roles) != 1 || len(out.TaskManuals) != 1 {
		t.Fatalf("fixture shape: %+v", out)
	}
	for _, c := range []struct {
		name     string
		got      docSizeDTO
		wantSize int
		wantCap  int
	}{
		{"role definition", out.Roles[0].Duty, dutySize, dutyCap},
		{"insight", out.Roles[0].Insight, insightSize, insightCap},
		{"lessons", out.Roles[0].Lessons, lessonsSize, learningCap},
		{"manual sop", out.TaskManuals[0].Sop, manualSopSize, manualSopCap},
		{"manual learnings", out.TaskManuals[0].Learnings, manualLearningsSize, manualLearningsCap},
	} {
		if c.got.SizeChars != c.wantSize || c.got.CapChars != c.wantCap {
			t.Fatalf("%s: want size=%d cap=%d, got size=%d cap=%d",
				c.name, c.wantSize, c.wantCap, c.got.SizeChars, c.got.CapChars)
		}
	}

	// The addressing keys are the only other thing the response carries, and a
	// caller needs them to go read whichever document turned out to be full.
	if out.Roles[0].RoleKey != role {
		t.Fatalf("role_key: want %q, got %q", role, out.Roles[0].RoleKey)
	}
	if out.TaskManuals[0].TypeKey != manualKey {
		t.Fatalf("type_key: want %q, got %q", manualKey, out.TaskManuals[0].TypeKey)
	}
}
