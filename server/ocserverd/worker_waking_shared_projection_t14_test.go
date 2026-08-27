package main

import "testing"

// T-14 item 1 — the outsource 「喚醒中」 must be the STAFF projection, not a
// second implementation of it. Two facts make that true, and each has a test
// here:
//
//   - the wake anchor is DURABLE and stamped at the spawn dispatch (the member
//     rule, stampWakeObservability: "a LANDED START stamps waking_since"), so a
//     server re-exec cannot forget that a worker is waking;
//   - the projection a worker's row reads back is PresenceState itself — the
//     same function the staff roster calls — so a divergence is a code change,
//     not a one-line edit at a call site.

// TestNotifyWorkerSpawn_StampsDurableWakingAnchor: the dispatch must leave the
// worker's row readable as "waking" BY THE MEMBER PROJECTION. Before T-14 the
// anchor lived only in the in-memory workerSpawnAt map and memberFromWorker
// wrote a hardcoded zero, so PresenceState — reached for outsource rows through
// the resume roster — called a freshly dispatched worker offline.
func TestNotifyWorkerSpawn_StampsDurableWakingAnchor(t *testing.T) {
	s := newWorkerTestServer(t)
	connectWarden(t, s, ServerSelfHost)
	now := nowSecs()
	task := putTaskFixture(t, s, Task{
		ID: "t-000000000014", TypeKey: "review-pr", Title: "Review",
		Status: TaskStatusNotStarted, Priority: TaskPriorityMid,
		ExecutorKind: TaskExecutorOutsource, ExecutorID: "ow-14",
	})
	if err := s.dal.PutTaskManual(TaskManual{TypeKey: "review-pr", Purpose: "p",
		Fields: "[]", Assignee: `{"kind":"outsource","model":"opus"}`}); err != nil {
		t.Fatalf("put manual: %v", err)
	}
	w := putWorkerFixture(t, s, OutsourceWorker{
		ID: "ow-14", Codename: "O-14", Model: "opus", Effort: "high",
		TaskID: task.ID, Status: WorkerStatusAssigned,
		DesiredState:     DesiredStateOnline,
		DesiredMachineID: ServerSelfHost,
		// Born long ago: the CreatedTS fallback the old projection leaned on is
		// stale here, so only a REAL dispatch stamp can read waking.
		CreatedTS: now - 10*WakingTTLSecs,
	})

	s.outsourceMu.Lock()
	dispatched := s.notifyWorkerSpawn(w, now)
	s.outsourceMu.Unlock()
	if !dispatched {
		t.Fatalf("notifyWorkerSpawn must dispatch (warden %s is online)", ServerSelfHost)
	}

	fresh, err := s.dal.GetOutsourceWorker("ow-14")
	if err != nil || fresh == nil {
		t.Fatalf("get worker: %+v (%v)", fresh, err)
	}
	if got := PresenceState(memberFromWorker(*fresh), now+1, false); got != MemberPresenceWaking {
		t.Fatalf("a just-dispatched worker read by the MEMBER projection = %q, want %q "+
			"(the durable waking anchor is what makes the two kinds one projection)",
			got, MemberPresenceWaking)
	}
}

// TestListOutsourceWorkers_WakingSurvivesReexec: the cockpit presence cell must
// survive a server re-exec. The old projection anchored waking on the in-memory
// workerSpawnAt map with a CreatedTS fallback, so a worker dispatched before a
// restart — and long past its own birth — dropped straight to 「離線」 while its
// wake was still in flight.
func TestListOutsourceWorkers_WakingSurvivesReexec(t *testing.T) {
	api := newTasksTestServer(t)
	api.noOutsource = true
	workerID := assignOneWorker(t, api)
	now := nowSecs()

	// The state a pre-restart dispatch leaves behind: a durable wake anchor on
	// the row, written through the MEMBER face (the only writer that existed
	// before T-14 carried the column onto the worker vocabulary).
	m, err := api.dal.GetMember(workerID)
	if err != nil || m == nil {
		t.Fatalf("get member %s: %+v (%v)", workerID, m, err)
	}
	m.WakingSince = now
	m.CreatedTS = now - 10*WakingTTLSecs // born long ago: no CreatedTS fallback
	if err := api.dal.PutMember(*m); err != nil {
		t.Fatalf("stamp waking anchor: %v", err)
	}

	// The re-exec: the in-memory spawn maps are reborn EMPTY.
	api.outsourceMu.Lock()
	api.workerSpawnAt = map[string]float64{}
	api.workerSpawnTarget = map[string]string{}
	api.outsourceMu.Unlock()

	rows := listWorkersAs(t, api, wireOwnerID)
	if len(rows) != 1 || rows[0].Presence != MemberPresenceWaking {
		t.Fatalf("after re-exec a worker with a fresh durable wake anchor must read %q, got %+v",
			MemberPresenceWaking, rows)
	}
}
