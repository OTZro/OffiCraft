package main

// The NEGATIVE half of the patch-publish surface: a patch that wrote nothing
// must announce nothing.
//
// api_insight_patch_publish_test.go pins the positive direction — a patch that
// really changed the doc MUST fan an SSE frame, because a write nobody
// announces leaves every open surface showing stale text. These pin the
// direction nobody was watching: a patch that changed NOTHING must not fan
// either, because a frame nobody has a change for is the same silent lie
// pointing the other way. Every listening cockpit refetches, the refetch
// returns the text it already had, and the only trace is load nobody ordered.
//
// Why this file exists at all, stated plainly: when the persistence gate moved
// from `applied > 0` to `next != current.Text`, the publish call sat INSIDE the
// gate and moved with it — but nothing in the build would have noticed if it
// had not. Hoisting `s.hub.Publish` (lessons, insight) or `s.publishTaskManual`
// (task manual) back out of the gate leaves the whole go suite green, because
// every existing publish test asks only "did a real write fan a frame".
//
// This repo has already been bitten by exactly that blind spot, in this exact
// seam. api_insight_patch_publish_test.go's own header records it: someone
// deleted patch_insight's single hub.Publish line and ran the full go suite
// plus all 1061 conformance cases, and everything stayed GREEN. That was the
// missing-frame direction. This file closes the extra-frame direction, for all
// three anchor-patch seams rather than only insight.
//
// The positive control in every test is load-bearing and must stay first. A
// listener that was never wired, a hub that fans nothing at all, and a
// correctly-silent skipped write are indistinguishable from "pop() returned
// nil" — so each test first proves a REAL patch on the SAME doc, through the
// SAME handler, on the SAME listener, does fan.

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// drainHubFrames empties the listener so the next pop() can only return a frame
// caused by the write made after it.
func drainHubFrames(l *hubListener) {
	for l.pop() != nil {
	}
}

// assertNoFrame fails with the topic of whatever arrived — the topic is what
// tells a maintainer WHICH publish seam leaked past the gate.
func assertNoFrame(t *testing.T, l *hubListener, what string) {
	t.Helper()
	raw := l.pop()
	if raw == nil {
		return
	}
	_, envelope := parseSSEFrame(t, raw)
	t.Errorf("%s fanned an SSE frame (topic=%v): nothing was written, so this announces a "+
		"change that never happened and every listening surface refetches for nothing",
		what, envelope["topic"])
}

func TestCancellingBatchPatchLessonsFansNoFrame(t *testing.T) {
	f := newHistoryFixture(t)
	const role, taskType = seedRoleAssistant, "general"
	const original = "LESSON: widen the anchor until it is unique.\n"
	if err := f.api.dal.PutLessons(Lessons{RoleKey: role, TaskType: taskType, Text: original}); err != nil {
		t.Fatalf("seed lessons: %v", err)
	}
	// Connect AFTER seeding: every frame popped below belongs to a write this
	// test made through the handler.
	listener, err := f.api.hub.Connect("", "")
	if err != nil {
		t.Fatal(err)
	}
	patch := func(body any) {
		t.Helper()
		rec := httptest.NewRecorder()
		f.api.HandlePatchLessonsApiLessonsRoleKeyTaskTypePatchPost(rec,
			f.req(http.MethodPost, "/api/lessons/"+role+"/"+taskType+"/patch", body), role, taskType)
		if rec.Code != http.StatusOK {
			t.Fatalf("patch: status=%d body=%s", rec.Code, rec.Body.String())
		}
	}

	patch(map[string]any{"edits": []any{map[string]any{"old": "unique.", "new": "provably unique."}}})
	if listener.pop() == nil {
		t.Fatal("control: a real lessons patch fanned NO frame — the listener or the publish " +
			"seam is broken, so a missing frame below would prove nothing")
	}
	drainHubFrames(listener)

	patch(cancellingBatch("provably unique", "demonstrably unique"))
	assertNoFrame(t, listener, "a cancelling lessons batch")
}

func TestCancellingBatchPatchInsightFansNoFrame(t *testing.T) {
	f := newHistoryFixture(t)
	const role = seedRoleAssistant
	const original = "INSIGHT: prefer a slow correct split to a fast wrong one.\n"
	if err := f.api.dal.PutInsight(Insight{RoleKey: role, Text: original}); err != nil {
		t.Fatalf("seed insight: %v", err)
	}
	listener, err := f.api.hub.Connect("", "")
	if err != nil {
		t.Fatal(err)
	}
	patch := func(body any) {
		t.Helper()
		rec := httptest.NewRecorder()
		f.api.HandlePatchInsightApiInsightRoleKeyPatchPost(rec,
			f.req(http.MethodPost, "/api/insight/"+role+"/patch", body), role)
		if rec.Code != http.StatusOK {
			t.Fatalf("patch: status=%d body=%s", rec.Code, rec.Body.String())
		}
	}

	patch(map[string]any{"edits": []any{map[string]any{"old": "wrong one", "new": "wrong merge"}}})
	if listener.pop() == nil {
		t.Fatal("control: a real insight patch fanned NO frame — the listener or the publish " +
			"seam is broken, so a missing frame below would prove nothing")
	}
	drainHubFrames(listener)

	patch(cancellingBatch("fast wrong merge", "fast mistaken merge"))
	assertNoFrame(t, listener, "a cancelling insight batch")
}

func TestCancellingBatchPatchTaskLearningsFansNoFrame(t *testing.T) {
	f := newHistoryFixture(t)
	const original = "LEARNING: the fixture seeds one manual per test.\n"
	key := seedManualWithLearnings(t, f.api, original)
	listener, err := f.api.hub.Connect("", "")
	if err != nil {
		t.Fatal(err)
	}

	if status, data := patchLearnings(t, f.api, key, map[string]any{
		"edits": []any{edit("seeds one manual", "seeds exactly one manual")},
	}); status != http.StatusOK {
		t.Fatalf("control edit must land, got %d: %v", status, data)
	}
	if listener.pop() == nil {
		t.Fatal("control: a real learnings patch fanned NO frame — the listener or the publish " +
			"seam is broken, so a missing frame below would prove nothing")
	}
	drainHubFrames(listener)

	if status, data := patchLearnings(t, f.api, key,
		cancellingBatch("exactly one manual", "precisely one manual")); status != http.StatusOK {
		t.Fatalf("cancelling batch must answer 200, got %d: %v", status, data)
	}
	assertNoFrame(t, listener, "a cancelling learnings batch")

	// Belt and braces: the premise these rest on is that the batch really was a
	// no-op. If the doc moved, a frame would be CORRECT and the assertion above
	// would be wrong to demand silence.
	if got := storedLearnings(t, f.api, key); got != "LEARNING: the fixture seeds exactly one manual per test.\n" {
		t.Fatalf("premise broken — the cancelling batch changed the doc: %q", got)
	}
}
