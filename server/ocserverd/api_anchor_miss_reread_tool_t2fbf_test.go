package main

// An anchor-miss refusal ENDS with an instruction: re-read <tool> and re-anchor.
// If that names the wrong tool the caller obeys it, re-reads a document that
// cannot contain the anchor it missed, re-anchors against that text and misses
// again — and nothing anywhere reports a problem. A misdirection has no
// symptom of its own; it looks exactly like an agent that keeps getting the
// anchor wrong.
//
// ApplyDocEdits takes the tool name as an argument precisely so each face can
// answer for its own document. These tests pin the ANSWER at each face, on
// BOTH refusal arms (0 hits and >1 hits).
//
// Every assertion is two-sided: it requires this face's tool AND rejects the
// other two faces' tools. A one-sided "contains get_task_manual" would still
// pass if the message named every tool it could think of, and — more to the
// point — the defect being pinned here was one face serving ANOTHER face's
// name, which only an absence assertion can see.

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// rereadToolNames is the closed set these faces choose from; each assertion
// wants exactly one of them, so the other two are derived rather than repeated
// per call site (a hand-written "must not contain" list rots one face at a
// time).
var rereadToolNames = []string{"get_lessons", "get_insight", "get_task_manual"}

// assertNamesOnlyReadTool: the refusal must send the caller to want, and must
// not mention any sibling document's read tool.
func assertNamesOnlyReadTool(t *testing.T, face, msg, want string) {
	t.Helper()
	if !strings.Contains(msg, "re-read ("+want+")") {
		t.Fatalf("%s: refusal must send the caller to %s, got: %q", face, want, msg)
	}
	for _, other := range rereadToolNames {
		if other == want {
			continue
		}
		if strings.Contains(msg, other) {
			t.Fatalf("%s: refusal names %s — that document cannot hold the missed anchor, "+
				"so the caller re-anchors against the wrong text and misses again, silently. got: %q",
				face, other, msg)
		}
	}
}

// TestPatchLessonsAnchorMissNamesGetLessons pins the lessons face on both arms.
func TestPatchLessonsAnchorMissNamesGetLessons(t *testing.T) {
	srv, dal, secret := newLessonsTestServer(t)
	ownerTok, _ := mintJWT("owner", "owner", 300, secret, time.Now().Unix(), "")
	seedLessonsOverlay(t, dal, "assistant", "general", "dup marker\nmiddle\ndup marker\n")

	status, data := patchLessons(t, srv.URL, ownerTok, "assistant", "general",
		`{"edits":[{"old":"an anchor that is simply not there","new":"x"}]}`)
	if status != http.StatusBadRequest {
		t.Fatalf("a missing anchor must be a flat 400, got %d: %v", status, data)
	}
	assertNamesOnlyReadTool(t, "patch_lessons/missing", errMessage(data), "get_lessons")

	status, data = patchLessons(t, srv.URL, ownerTok, "assistant", "general",
		`{"edits":[{"old":"dup marker","new":"resolved"}]}`)
	if status != http.StatusBadRequest {
		t.Fatalf("an ambiguous anchor must be a flat 400, got %d: %v", status, data)
	}
	assertNamesOnlyReadTool(t, "patch_lessons/ambiguous", errMessage(data), "get_lessons")
}

// TestPatchTaskLearningsAnchorMissNamesGetTaskManual pins the manual-learnings
// face on both arms. This is the face that carried the defect: it reached for a
// lessons-named convenience wrapper as "the shared engine", so every agent
// patching a manual's learnings was told to go and re-read its ROLE's lessons.
func TestPatchTaskLearningsAnchorMissNamesGetTaskManual(t *testing.T) {
	api := newTasksTestServer(t)
	key := seedManualWithLearnings(t, api, "dup marker\nmiddle\ndup marker\n")

	status, data := patchLearnings(t, api, key, map[string]any{
		"edits": []any{edit("an anchor that is simply not there", "x")},
	})
	if status != http.StatusBadRequest {
		t.Fatalf("a missing anchor must be a flat 400, got %d: %v", status, data)
	}
	assertNamesOnlyReadTool(t, "patch_task_learnings/missing", errMessage(data), "get_task_manual")

	status, data = patchLearnings(t, api, key, map[string]any{
		"edits": []any{edit("dup marker", "resolved")},
	})
	if status != http.StatusBadRequest {
		t.Fatalf("an ambiguous anchor must be a flat 400, got %d: %v", status, data)
	}
	assertNamesOnlyReadTool(t, "patch_task_learnings/ambiguous", errMessage(data), "get_task_manual")
}

// TestPatchInsightAnchorMissNamesGetInsight pins the insight face on both arms.
func TestPatchInsightAnchorMissNamesGetInsight(t *testing.T) {
	f := newHistoryFixture(t)
	role := seedRoleAssistant

	rec := httptest.NewRecorder()
	f.api.HandleReplaceInsightApiInsightRoleKeyPost(rec,
		f.req(http.MethodPost, "/api/insight/"+role,
			map[string]any{"text": "dup marker\nmiddle\ndup marker\n"}), role)
	if rec.Code != http.StatusOK {
		t.Fatalf("seed insight: status=%d body=%s", rec.Code, rec.Body.String())
	}

	patch := func(old string) string {
		t.Helper()
		rec := httptest.NewRecorder()
		f.api.HandlePatchInsightApiInsightRoleKeyPatchPost(rec,
			f.req(http.MethodPost, "/api/insight/"+role+"/patch", map[string]any{
				"edits": []map[string]any{{"old": old, "new": "x"}},
			}), role)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("an unresolved anchor must be a flat 400, got %d: %s", rec.Code, rec.Body.String())
		}
		var data map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &data); err != nil {
			t.Fatal(err)
		}
		return errMessage(data)
	}

	assertNamesOnlyReadTool(t, "patch_insight/missing",
		patch("an anchor that is simply not there"), "get_insight")
	assertNamesOnlyReadTool(t, "patch_insight/ambiguous", patch("dup marker"), "get_insight")
}
