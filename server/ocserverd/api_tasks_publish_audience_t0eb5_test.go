package main

// api_tasks_publish_audience_t0eb5_test.go — T-0eb5: a task delta reaches its
// EXECUTOR and the owner cockpit, and NOBODY else. The creator was removed from
// the audience by owner ruling 2026-08-08 (card rc-0994e949872e, option ①).
//
// The guard is deliberately pinned on publishTask — the chokepoint every task
// write funnels through — and NOT on Hub.Publish: Hub's routing is unchanged by
// this ticket, so a Hub-level test would stay green with the creator still in
// the audience (it would be true of the target population no matter what we
// did here, i.e. worthless as acceptance evidence).
//
// Both directions are asserted in the SAME test, and this is the point: the
// negative ("the creator no longer receives") is indistinguishable from "the
// whole fan-out broke" unless the positive ("the executor still receives")
// holds in the same run.

import (
	"strings"
	"testing"
)

// sawTaskFrame drains a listener and reports whether ANY task frame arrived.
// Draining (rather than peeking one frame) keeps the assertion honest if an
// unrelated topic is ever fanned to the same connection.
func sawTaskFrame(l *hubListener) bool {
	for {
		frame := l.pop()
		if frame == nil {
			return false
		}
		if strings.Contains(string(frame), `"topic":"task"`) ||
			strings.Contains(string(frame), `"topic": "task"`) {
			return true
		}
	}
}

func TestPublishTaskAudienceExcludesCreator(t *testing.T) {
	api := newTasksTestServer(t)

	owner, _ := api.hub.Connect("", "")       // the cockpit connection (memberID "")
	exec, _ := api.hub.Connect("m-exec", "")  // the executor
	creator, _ := api.hub.Connect("m-creator", "")
	bystander, _ := api.hub.Connect("m-other", "") // related to neither

	// A task that was dispatched away: creator ≠ executor. This is exactly the
	// shape that produced the reported noise (a 發包'd ticket still fanning
	// every step to the person who filed it).
	api.publishTask(Task{
		ID:         "t-audience-probe",
		Title:      "audience probe",
		Status:     TaskStatusInProgress,
		Priority:   TaskPriorityMid,
		ExecutorID: "m-exec",
		CreatorID:  "m-creator",
	}, triggerServer)

	// POSITIVE CONTROL — without this the negative below proves nothing: a
	// wholly broken fan-out would satisfy "the creator got nothing" too.
	if !sawTaskFrame(exec) {
		t.Fatal("the EXECUTOR must still receive the task delta (positive control) — if this fails, the negative assertion below is meaningless")
	}
	if !sawTaskFrame(owner) {
		t.Fatal("the owner/dashboard connection must stay 全量 (second positive control)")
	}

	// THE ASSERTION THIS FILE EXISTS FOR.
	if sawTaskFrame(creator) {
		t.Fatal("the CREATOR must receive NO task delta (owner ruling rc-0994e949872e option ①) — creators pull with list_tasks instead")
	}
	// A third party was never in the audience; if this one ever fires, the
	// creator assertion above is not measuring what it claims to.
	if sawTaskFrame(bystander) {
		t.Fatal("an unrelated agent must receive nothing")
	}
}

// TestPublishTaskAudienceCreatorIsAlsoExecutor guards the boundary case the
// removal must NOT break: when the creator kept the work (creator == executor,
// the ordinary self-filed ticket), they still receive their own task deltas —
// they receive them AS THE EXECUTOR. Without this case, "drop the creator"
// could be implemented as "drop anyone who is the creator" and the everyday
// path would go silent while the test above stayed green.
func TestPublishTaskAudienceCreatorIsAlsoExecutor(t *testing.T) {
	api := newTasksTestServer(t)

	self, _ := api.hub.Connect("m-self", "")
	bystander, _ := api.hub.Connect("m-other", "")

	api.publishTask(Task{
		ID:         "t-self-filed",
		Title:      "self-filed",
		Status:     TaskStatusInProgress,
		Priority:   TaskPriorityMid,
		ExecutorID: "m-self",
		CreatorID:  "m-self",
	}, triggerServer)

	if !sawTaskFrame(self) {
		t.Fatal("a self-filed task must still reach its executor (who happens to be the creator)")
	}
	if sawTaskFrame(bystander) {
		t.Fatal("an unrelated agent must receive nothing")
	}
}
