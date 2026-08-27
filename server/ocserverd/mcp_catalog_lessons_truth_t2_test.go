package main

// Two agent-facing descriptions that T-2 either left saying the OPPOSITE of
// what the server does, or upgraded from an honest warning into a promise the
// server breaks. Both are pinned here the only way a description can honestly
// be pinned: the test MEASURES the behaviour first and asserts the words
// against the measurement, so neither half can drift alone.
//
// The catalogue read here is spec/mcp-catalog.json — the frozen bytes
// tools/list serves, generated from spec/openapi.json's x-mcp.legacy.descriptor
// (bin/gen-mcp-catalog). Asserting on the generated artifact rather than on the
// source is deliberate: an agent reads the artifact.

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

type mcpCatalogTool struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	InputSchema struct {
		Properties map[string]struct {
			Description string `json:"description"`
		} `json:"properties"`
	} `json:"inputSchema"`
}

func loadMcpCatalogTool(t *testing.T, name string) mcpCatalogTool {
	t.Helper()
	raw, err := os.ReadFile("../../spec/mcp-catalog.json")
	if err != nil {
		t.Fatalf("read frozen catalog: %v", err)
	}
	var catalog struct {
		Tools []mcpCatalogTool `json:"tools"`
	}
	if err := json.Unmarshal(raw, &catalog); err != nil {
		t.Fatalf("parse frozen catalog: %v", err)
	}
	for _, tool := range catalog.Tools {
		if tool.Name == name {
			return tool
		}
	}
	t.Fatalf("tool %q is not in the frozen catalog", name)
	return mcpCatalogTool{}
}

// TestListDocumentHistoryKeyDescriptionMatchesTheLessonsDoor.
//
// Before this test the `key` description told agents, verbatim, that "for
// lessons the key is 'role_key::task_type' and anything without both halves is
// 400" and that "A MALFORMED lessons key - one missing its '::' - is 400".
// T-2 inverted BOTH sentences: the bare role_key is now the only correct shape,
// and a key carrying "::" is the one that is refused. A description that is
// exactly backwards is worse than none — it sends an agent that got a 400 to
// debug in the opposite direction.
func TestListDocumentHistoryKeyDescriptionMatchesTheLessonsDoor(t *testing.T) {
	// ① MEASURE. The words are asserted against these two answers, not against
	// a reading of the handler.
	api := newTasksTestServer(t)
	answer := func(key string) int {
		rec := httptest.NewRecorder()
		api.HandleListDocumentHistoryApiDocumentHistoryKindKeyGet(rec,
			taskReq(t, http.MethodGet, "/api/document-history/lessons/"+key, nil, "owner", "owner"),
			"lessons", key)
		return rec.Code
	}
	if got := answer(seedRoleAssistant); got != http.StatusOK {
		t.Fatalf("a BARE lessons key answered %d, want 200 — the measurement this test's "+
			"assertions rest on is not what it thinks", got)
	}
	if got := answer(seedRoleAssistant + "::"); got != http.StatusBadRequest {
		t.Fatalf("a lessons key carrying '::' answered %d, want 400 — same", got)
	}

	desc := loadMcpCatalogTool(t, "list_document_history").InputSchema.Properties["key"].Description
	if desc == "" {
		t.Fatal("the list_document_history `key` parameter has no description at all")
	}
	// Positive control for the substring search below: if this stops matching,
	// the searches are looking at the wrong string and every "not found" result
	// under it is meaningless.
	if !strings.Contains(desc, "system_interaction") {
		t.Fatalf("the `key` description does not mention system_interaction — this search "+
			"cannot find text it is looking at, so its negative results prove nothing. Got: %q", desc)
	}

	for _, banned := range []struct{ text, why string }{
		// Anchored on the TEACHING form, not on the bare shape string: the new
		// text names the old shape on purpose, to say it is retired.
		{"the key is 'role_key::task_type'", "names the retired composite shape as the one to use"},
		{"anything without both halves is 400", "the bare key is now the CORRECT one and answers 200"},
		{"one missing its '::' - is 400", "exactly inverted: '::' is what gets refused now"},
	} {
		if strings.Contains(desc, banned.text) {
			t.Errorf("the list_document_history `key` description still contains %q — %s. "+
				"This is agent-facing text and it teaches the OPPOSITE of what the two "+
				"measurements above just observed.", banned.text, banned.why)
		}
	}
	if !strings.Contains(desc, "bare role_key") {
		t.Errorf("the `key` description never says a lessons key is the BARE role_key. "+
			"Removing the wrong sentence is only half the fix — an agent still has to "+
			"learn the shape from somewhere. Got: %q", desc)
	}
}

// TestPeekDocSizesDescriptionDoesNotPromiseCoverageItCannotGive.
//
// T-2 rewrote this description's LIMITATION paragraph into "Every capped
// document is covered". One call falsified it, and the mechanism had nothing to
// do with the task_type axis T-2 removed: the listing iterates the ROSTER, and
// neither the lessons nor the insight write face compared role_key against it.
// T-2 answered by NARROWING THE CLAIM rather than plugging the hole, and this
// test's first version measured that gap and pinned the honest wording. Its own
// failure message said what to do next: "if this ever starts refusing, the gap
// closed and this description may widen again."
//
// 🔴 IT STARTED REFUSING. The T-2 acceptance follow-up gated the LESSONS write
// face, so this test now measures the SPLIT the description had to grow:
//
//	lessons  → refused (404) unless the role_key has a reader
//	insight  → still ungated: 200, and still absent from this listing
//
// Both halves are measured here BEFORE any wording is asserted, because the
// value of this file is that the sentence and the behaviour are checked against
// each other rather than each against a reader's memory. The insight half is
// the load-bearing one now: it is the ONLY thing keeping the description's
// remaining gap paragraph honest, and if insight is ever gated too, THIS is the
// assertion that says so and sends the next person to the wording.
func TestPeekDocSizesDescriptionDoesNotPromiseCoverageItCannotGive(t *testing.T) {
	api := newTasksTestServer(t)
	const orphan = "no-such-role"

	// ① LESSONS — the gap that CLOSED. A write under a role_key with no reader
	//    is refused outright, so it can no longer produce an unlistable
	//    document.
	rec := httptest.NewRecorder()
	api.HandleReplaceLessonsApiLessonsRoleKeyPost(rec,
		taskReq(t, http.MethodPost, "/api/lessons/"+orphan, map[string]any{"text": "ORPHAN"}, "owner", "owner"),
		orphan)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("writing LESSONS under a role_key nothing reads = %d %s, want 404 — "+
			"if this goes back to 200 the gap has REOPENED and the description's "+
			"\"the lessons write face no longer has that gap\" is a false promise again",
			rec.Code, rec.Body.String())
	}

	// Positive control on the same face: a role the roster DOES carry still
	// writes. Without it, a route broken end to end would read as "gated".
	rec = httptest.NewRecorder()
	api.HandleReplaceLessonsApiLessonsRoleKeyPost(rec,
		taskReq(t, http.MethodPost, "/api/lessons/assistant", map[string]any{"text": "ROSTER ROLE"}, "owner", "owner"),
		"assistant")
	if rec.Code != http.StatusOK {
		t.Fatalf("positive control: a ROSTER role's lessons must still be writable, got %d %s",
			rec.Code, rec.Body.String())
	}

	// ② INSIGHT — the gap that REMAINS, and which the description now calls out
	//    by name as insight-only. Measured, not assumed.
	rec = httptest.NewRecorder()
	api.HandleReplaceInsightApiInsightRoleKeyPost(rec,
		taskReq(t, http.MethodPost, "/api/insight/"+orphan, map[string]any{"text": "ORPHAN"}, "owner", "owner"),
		orphan)
	if rec.Code != http.StatusOK {
		t.Fatalf("writing INSIGHT under a role_key no role carries = %d %s, want 200 — "+
			"if this has started refusing too, the description's INSIGHT-ONLY paragraph "+
			"is now stale and must be rewritten (or removed) in the same commit",
			rec.Code, rec.Body.String())
	}
	rec = httptest.NewRecorder()
	api.HandlePeekDocSizesApiDocSizesGet(rec,
		taskReq(t, http.MethodGet, "/api/doc-sizes", nil, "owner", "owner"))
	if rec.Code != http.StatusOK {
		t.Fatalf("doc-sizes = %d %s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), orphan) {
		t.Fatalf("doc-sizes DID list the orphan document — the remaining gap this "+
			"description states has closed, and the wording should be widened rather "+
			"than kept narrow. Body: %s", rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "assistant") {
		t.Fatalf("doc-sizes does not list `assistant`, which was just written above — "+
			"this listing is not reporting what it claims to, so the orphan check "+
			"above proves nothing. Body: %s", rec.Body.String())
	}

	// ③ ASSERT the words against what ① and ② just observed.
	desc := loadMcpCatalogTool(t, "peek_doc_sizes").Description
	if !strings.Contains(desc, "cap_chars") {
		t.Fatalf("the peek_doc_sizes description does not mention cap_chars — this search "+
			"is not looking at the text it thinks it is. Got: %q", desc)
	}
	if strings.Contains(desc, "Every capped document is covered") {
		t.Errorf("peek_doc_sizes still promises \"Every capped document is covered\". "+
			"The insight call above wrote a capped document under %q and this listing "+
			"did not report it. The promise is false, and it REPLACED a warning that "+
			"was merely imprecise.", orphan)
	}
	for _, needed := range []struct{ text, why string }{
		{"KEYED BY ROLE", "the listing's real boundary has to be stated, not implied"},
		{"roster", "the remaining gap belongs to the WRITE side not validating role_key against the roster"},
		{"list_roles", "an agent needs to be told where the roster it is derived from lives"},
		{"INSIGHT-ONLY", "the gap is no longer symmetric — saying \"lessons (or insight)\" would now be false about lessons, which ① just measured as refused"},
	} {
		if !strings.Contains(desc, needed.text) {
			t.Errorf("the peek_doc_sizes description does not contain %q — %s. Got: %q",
				needed.text, needed.why, desc)
		}
	}
	// The retired symmetric phrasing must be gone, not merely supplemented.
	if strings.Contains(desc, "write lessons (or insight) under a role_key no role carries") {
		t.Errorf("the description still says an admin can write LESSONS under a role_key " +
			"no role carries. ① measured that as a 404. This is agent-facing text " +
			"teaching the opposite of the shipped behaviour.")
	}
}
