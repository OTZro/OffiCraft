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
// document is covered". One call falsifies it, and the mechanism has nothing to
// do with the task_type axis T-2 removed: the listing iterates the ROSTER, and
// the lessons write face never compares role_key against the roster. The same
// catalogue's replace_lessons description already says so in as many words
// ("it creates a lessons document under a role nobody answers to, answers
// 200"), so the two were contradicting each other on the same page.
//
// 🔴 THE FIX IS A NARROWER CLAIM, NOT A PLUGGED HOLE. Making the write face
// validate the roster is an owner-facing behaviour change; describing the gap
// accurately is not. An honest gap beats a comfortable promise.
func TestPeekDocSizesDescriptionDoesNotPromiseCoverageItCannotGive(t *testing.T) {
	// ① MEASURE the gap end to end.
	api := newTasksTestServer(t)
	const orphan = "no-such-role"
	rec := httptest.NewRecorder()
	api.HandleReplaceLessonsApiLessonsRoleKeyPost(rec,
		taskReq(t, http.MethodPost, "/api/lessons/"+orphan, map[string]any{"text": "ORPHAN"}, "owner", "owner"),
		orphan)
	if rec.Code != http.StatusOK {
		t.Fatalf("writing lessons under a role_key no role carries = %d %s, want 200 — "+
			"if this ever starts refusing, the gap closed and this description may widen again",
			rec.Code, rec.Body.String())
	}
	rec = httptest.NewRecorder()
	api.HandlePeekDocSizesApiDocSizesGet(rec,
		taskReq(t, http.MethodGet, "/api/doc-sizes", nil, "owner", "owner"))
	if rec.Code != http.StatusOK {
		t.Fatalf("doc-sizes = %d %s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), orphan) {
		t.Fatalf("doc-sizes DID list the orphan lessons document — the gap this test "+
			"describes has closed, and the description should be widened rather than "+
			"kept narrow. Body: %s", rec.Body.String())
	}

	// ② ASSERT the words against it.
	desc := loadMcpCatalogTool(t, "peek_doc_sizes").Description
	if !strings.Contains(desc, "cap_chars") {
		t.Fatalf("the peek_doc_sizes description does not mention cap_chars — this search "+
			"is not looking at the text it thinks it is. Got: %q", desc)
	}
	if strings.Contains(desc, "Every capped document is covered") {
		t.Errorf("peek_doc_sizes still promises \"Every capped document is covered\". "+
			"The call above wrote a capped lessons document under %q and this listing "+
			"did not report it. The promise is false, and it REPLACED a warning that "+
			"was merely imprecise.", orphan)
	}
	for _, needed := range []struct{ text, why string }{
		{"KEYED BY ROLE", "the listing's real boundary has to be stated, not implied"},
		{"roster", "the gap belongs to the WRITE side not validating role_key against the roster"},
		{"list_roles", "an agent needs to be told where the roster it is derived from lives"},
	} {
		if !strings.Contains(desc, needed.text) {
			t.Errorf("the peek_doc_sizes description does not contain %q — %s. Got: %q",
				needed.text, needed.why, desc)
		}
	}
}
