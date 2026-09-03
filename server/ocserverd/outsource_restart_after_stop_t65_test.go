package main

import "testing"

// ── T-65 包② — the queued 「起來」 on the outsource face ────────────────────────
//
// 🔴 READ THE FIRST TEST BEFORE THE OTHERS. Everything else in this file writes
// restart_after_stop through a handler and reads it back through
// GetOutsourceWorker; the first test is the one that says WHY that round-trip is
// not free, and it is the only one that would still fail if the projection
// silently dropped the column while every handler was written correctly.

// TestOutsourceProjectionCarriesRestartAfterStop pins the write half AND the
// read half of the OutsourceWorker ↔ Member projection for restart_after_stop.
//
// The hazard is asymmetric with every other field in that projection, and the
// asymmetry is the whole reason this test exists. A column memberFromWorker
// forgets is normally merely NOT REFRESHED. restart_after_stop is not one of
// those: mfRestartAfterStop is deliberately NOT insertOnly (dal_member_patch.go
// spells out why — it must land in the same write as desired_state), so
// memberWholeRow carries it into PutMember's UPDATE SET list and every one of
// the 13 non-test PutOutsourceWorker call sites WRITES it. With the projection
// blind, each of those writes stored `false` over whatever a handler had just
// stamped — no error, no red, and the owner's 重新聚焦 on a stopped worker would
// answer 200 and then never come up.
//
// Mutant (T-65 包② DoD): delete `RestartAfterStop: w.RestartAfterStop` from
// memberFromWorker and this test goes red on the "erased by a later worker
// write" assertion below.
func TestOutsourceProjectionCarriesRestartAfterStop(t *testing.T) {
	api := newTasksTestServer(t)
	id := newActiveOnlineWorker(t, api)

	w, err := api.dal.GetOutsourceWorker(id)
	if err != nil || w == nil {
		t.Fatalf("seed read: %v", err)
	}
	// ── write half: the flag reaches the row through PutOutsourceWorker ───────
	w.RestartAfterStop = true
	if err := api.dal.PutOutsourceWorker(*w); err != nil {
		t.Fatalf("put worker: %v", err)
	}
	m, err := api.dal.GetMember(id)
	if err != nil || m == nil {
		t.Fatalf("read member: %v", err)
	}
	if !m.RestartAfterStop {
		t.Fatal("memberFromWorker dropped restart_after_stop: the member row the " +
			"worker projects reads false right after a worker write that set it")
	}
	// ── read half: it comes back out on the worker vocabulary ────────────────
	back, err := api.dal.GetOutsourceWorker(id)
	if err != nil || back == nil {
		t.Fatalf("read back: %v", err)
	}
	if !back.RestartAfterStop {
		t.Fatal("workerFromMember dropped restart_after_stop: the stored intent is " +
			"invisible to every worker-side reader, so nothing can ever spend it")
	}
	// ── the silent-erase shape, stated as its own assertion ──────────────────
	// A SECOND worker write that says nothing about the flag must not clear it.
	// This is the assertion the mutant kills, and it is separate on purpose: the
	// two above can both pass on a projection that carries the field only one
	// way.
	untouched, err := api.dal.GetOutsourceWorker(id)
	if err != nil || untouched == nil {
		t.Fatalf("read for re-put: %v", err)
	}
	untouched.LastOpLog = "an unrelated worker write"
	if err := api.dal.PutOutsourceWorker(*untouched); err != nil {
		t.Fatalf("re-put worker: %v", err)
	}
	after, err := api.dal.GetOutsourceWorker(id)
	if err != nil || after == nil {
		t.Fatalf("read after re-put: %v", err)
	}
	if !after.RestartAfterStop {
		t.Fatal("restart_after_stop was ERASED by an unrelated worker write — the " +
			"whole-row upsert carries this column, so a projection that does not " +
			"round-trip it does not merely forget the intent, it deletes it")
	}
}
