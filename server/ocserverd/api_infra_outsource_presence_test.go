package main

import (
	"strings"
	"testing"
)

// A worker's presence is a pure SSE projection. The list cannot learn about a
// clean connect/disconnect edge from a durable task update, so each edge must
// fan the owner-only outsource_worker delta that owns this projection.
func TestOutsourceWorkerSSEEdgesPublishCanonicalPresence(t *testing.T) {
	api := newTasksTestServer(t)
	api.noOutsource = true
	workerID := newActiveWorker(t, api, false)
	owner, err := api.hub.Connect("", "")
	if err != nil {
		t.Fatalf("connect owner listener: %v", err)
	}
	t.Cleanup(func() { api.hub.Disconnect(owner) })

	worker, err := api.hub.Connect(workerID, "")
	if err != nil {
		t.Fatalf("connect worker listener: %v", err)
	}
	api.onFirstConnect(workerID)
	assertOutsourceWorkerDelta(t, owner.pop(), workerID, "online")
	if got := listWorkersAs(t, api, "owner"); len(got) != 1 || got[0].Presence != "online" {
		t.Fatalf("worker list after connect = %+v; want one online worker", got)
	}

	if !api.hub.Disconnect(worker) {
		t.Fatal("worker disconnect must be the final live SSE edge")
	}
	api.onLastDisconnect(workerID)
	assertOutsourceWorkerDelta(t, owner.pop(), workerID, "offline")
	if got := listWorkersAs(t, api, "owner"); len(got) != 1 || got[0].Presence != "offline" {
		t.Fatalf("worker list after disconnect = %+v; want one offline worker", got)
	}
}

func assertOutsourceWorkerDelta(t *testing.T, frame []byte, workerID, edge string) {
	t.Helper()
	if frame == nil ||
		!strings.Contains(string(frame), `"topic":"outsource_worker"`) ||
		!strings.Contains(string(frame), workerID) {
		t.Fatalf("worker %s edge must publish owner worker delta, got %q", edge, frame)
	}
}
