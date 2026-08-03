package main

// The OTHER half of the silent surface.
//
// A previous round pinned two of insight's three write faces against a missing
// SSE publish: replace_insight (conformance test_sse.py's topic table) and the
// history restore (api_document_history_insight_publish_test.go). patch_insight
// was left with none — a post-land review deleted its single hub.Publish line
// and ran the whole go suite plus all 1061 conformance cases: everything stayed
// GREEN. That matters more than the arithmetic of "two out of three": patch is
// the face agents actually take (it is a live MCP tool, and for a long document
// patching is the ordinary edit), so the invisible-write symptom lands on the
// most-travelled path.
//
// The symptom is the one api_document_history.go's own comment spells out: the
// row is written, the DTO comes back, HTTP 200, no error, no other test goes
// red — and every open surface keeps showing the old text until someone reloads
// by hand.
//
// The replace control is not decoration. Without it, "no frame arrived" could
// just mean the listener was never wired, and the patch assertion would pass
// vacuously in a world where insight writes fan nothing at all.

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPatchingAnInsightDocFansAnInsightDelta(t *testing.T) {
	f := newHistoryFixture(t)
	role := seedRoleAssistant
	const seeded = "INSIGHT: prefer a slow correct split to a fast wrong one."
	const appended = "\nINSIGHT: an anchor-addressed addition."

	// Connect first: every frame popped below belongs to a write this test made.
	listener, err := f.api.hub.Connect("", "")
	if err != nil {
		t.Fatal(err)
	}

	// Positive control FIRST — replace_insight's publish has been pinned since
	// the previous round, so if THIS does not fan, the fixture is broken and the
	// patch assertion below would be measuring nothing.
	rec := httptest.NewRecorder()
	f.api.HandleReplaceInsightApiInsightRoleKeyPost(rec,
		f.req(http.MethodPost, "/api/insight/"+role, map[string]any{"text": seeded}), role)
	if rec.Code != http.StatusOK {
		t.Fatalf("control: replace insight: status=%d body=%s", rec.Code, rec.Body.String())
	}
	raw := listener.pop()
	if raw == nil {
		t.Fatal("control: replacing an insight doc fanned NO frame — the listener or the " +
			"publish seam is broken, so a missing patch frame below would prove nothing")
	}
	if _, envelope := parseSSEFrame(t, raw); envelope["topic"] != "insight" {
		t.Fatalf("control: replace fanned topic=%v, want \"insight\"", envelope["topic"])
	}

	// The real assertion.
	rec = httptest.NewRecorder()
	f.api.HandlePatchInsightApiInsightRoleKeyPatchPost(rec,
		f.req(http.MethodPost, "/api/insight/"+role+"/patch", map[string]any{
			"edits": []map[string]any{{"old": "", "new": appended}},
		}), role)
	if rec.Code != http.StatusOK {
		t.Fatalf("patch insight: status=%d body=%s", rec.Code, rec.Body.String())
	}

	// The write really happened — so a missing frame below is the silent kind of
	// wrong (data changed, screens stale) and not simply "the patch did nothing".
	current, err := f.api.foldInsightDTO(role)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(current.Text, strings.TrimSpace(appended)) {
		t.Fatalf("patch answered 200 but the doc does not carry the edit: %q", current.Text)
	}

	raw = listener.pop()
	if raw == nil {
		t.Fatal("patching an insight doc fanned NO frame: the patch answered 200 and changed " +
			"the database, so nothing else in the build will tell you — every open surface is " +
			"now showing stale text. Restore the hub.Publish in HandlePatchInsight…Post.")
	}
	_, envelope := parseSSEFrame(t, raw)
	if envelope["topic"] != "insight" {
		t.Fatalf("patch fanned topic=%v, want \"insight\" (a topic outside the closed set in "+
			"sseTopics is dropped SILENTLY at the publish seam)", envelope["topic"])
	}
	data, ok := envelope["data"].(map[string]any)
	if !ok {
		t.Fatalf("frame data is not an object: %v", envelope["data"])
	}
	if want := wireOwnerID + "::" + role; data["key"] != want {
		t.Fatalf("frame key = %v, want %q — insight's key is the BARE role_key, with no "+
			"task_type segment", data["key"], want)
	}
}
