package main

import "testing"

// receipt_core_sites_t170e_test.go — T-170e stage 2, BLOCK-1 follow-up.
//
// op_receipt_single_source_t170e_test.go pins the two IN-MEMORY shells
// (stampMemberOpReceipt / stampWorkerOpReceipt). This file pins the four stamps
// that RE-READ AND PERSIST — the ones a later review found still writing the
// five receipt columns out by hand, in four more places, while the comment on
// stampOpReceipt claimed to be "the only spelling of it":
//
//	stampMemberOpBlocked        (reconcile.go)
//	stampMemberPlacementBlocked (reconcile.go)
//	stampWorkerPlacementBlocked (worker_spawn.go)
//	stampReceiptMissing         (receipt_watch.go — TWICE in one function,
//	                             once per row kind, differing only in LastOp)
//
// 🔴 EVERY ASSERTION HERE IS AN ABSOLUTE VALUE, AND THAT IS THE POINT. A parity
// test ("the member stamp equals the worker stamp") stays green for every change
// to a shared core, because both sides move together. Pinning each site to
// literal expectations is what makes a change to stampOpReceipt redden ALL of
// them at once, which is the only evidence that they really are one
// implementation rather than five copies that agree today.
//
// These tests deliberately assert the FIELDS, not the reason text: the reason
// sentences belong to their own sites and have their own tests. What is shared —
// and therefore what is pinned here — is the shape of a receipt: the verb, a
// non-nil FALSE ok, a CLEARED log, the reason, and the timestamp.

// wantReceipt asserts the five receipt columns against absolute values.
func wantReceipt(t *testing.T, where string,
	gotOp string, gotOK *bool, gotLog, gotReason string, gotAt float64,
	op, reason string, now float64) {
	t.Helper()
	if gotOp != op {
		t.Errorf("%s: last_op = %q, want %q", where, gotOp, op)
	}
	if gotOK == nil || *gotOK {
		t.Errorf("%s: last_op_ok = %v, want a non-nil false — a receipt is never a success",
			where, gotOK)
	}
	if gotLog != "" {
		t.Errorf("%s: last_op_log = %q, want \"\" — the previous op's log must not be "+
			"left standing under a new receipt", where, gotLog)
	}
	if gotReason != reason {
		t.Errorf("%s: last_op_reason = %q, want %q", where, gotReason, reason)
	}
	if gotAt != now {
		t.Errorf("%s: last_op_at = %v, want %v", where, gotAt, now)
	}
}

func TestStampMemberOpBlocked_WritesTheFiveReceiptFields(t *testing.T) {
	const reason = spawnReasonZombieSuspect + ": a session is holding the seat"
	const now = 1_769_904_321.0

	s := newReconcileTestServer(t)
	m := testAgent("m-170e-blocked")
	m.LastOp, m.LastOpOK, m.LastOpLog, m.LastOpReason, m.LastOpAt = "", nil, "stale log", "stale", 0
	putTestMember(t, s, m)

	s.stampMemberOpBlocked(m.ID, reason, now)

	got, err := s.dal.GetMember(m.ID)
	if err != nil || got == nil {
		t.Fatalf("re-read member: %v", err)
	}
	wantReceipt(t, "stampMemberOpBlocked",
		got.LastOp, got.LastOpOK, got.LastOpLog, got.LastOpReason, got.LastOpAt,
		reconcileCmdStart, reason, now)
}

func TestStampMemberPlacementBlocked_WritesTheFiveReceiptFields(t *testing.T) {
	const now = 1_769_904_322.0

	s := newReconcileTestServer(t)
	m := testAgent("m-170e-unplaced")
	m.DesiredMachineID = "" // the no-machine variant, so the reason is derivable here
	m.LastOp, m.LastOpOK, m.LastOpLog, m.LastOpReason, m.LastOpAt = "", nil, "stale log", "stale", 0
	putTestMember(t, s, m)

	s.stampMemberPlacementBlocked(&m, now)

	got, err := s.dal.GetMember(m.ID)
	if err != nil || got == nil {
		t.Fatalf("re-read member: %v", err)
	}
	// The reason is the site's own sentence; the receipt SHAPE is the core's.
	// Pinned by prefix on the code (which is contract) and absolutely on the
	// other four columns.
	reason := got.LastOpReason
	if reason == "" || len(reason) < len(placementReasonNoMachine)+1 ||
		reason[:len(placementReasonNoMachine)+1] != placementReasonNoMachine+":" {
		t.Fatalf("last_op_reason = %q, want the %s: sentence", reason, placementReasonNoMachine)
	}
	wantReceipt(t, "stampMemberPlacementBlocked",
		got.LastOp, got.LastOpOK, got.LastOpLog, got.LastOpReason, got.LastOpAt,
		reconcileCmdStart, reason, now)
}

func TestStampWorkerPlacementBlocked_WritesTheFiveReceiptFields(t *testing.T) {
	const reason = spawnReasonNoSecret + ": no secret is configured for this runtime"
	const now = 1_769_904_323.0

	s := newReconcileTestServer(t)
	w := OutsourceWorker{
		ID: "ow-170e-blocked", Codename: "W1", Runtime: RuntimeClaude,
		Model: "opus", Effort: "medium", TaskID: "t-170e",
		Status: WorkerStatusActive, CreatedTS: 1.0, DesiredState: "online",
		LastOp: "", LastOpOK: nil, LastOpLog: "stale log", LastOpReason: "stale", LastOpAt: 0,
	}
	if err := s.dal.PutOutsourceWorker(w); err != nil {
		t.Fatalf("seed worker: %v", err)
	}

	s.stampWorkerPlacementBlocked(&w, reason, now)

	got, err := s.dal.GetOutsourceWorker(w.ID)
	if err != nil || got == nil {
		t.Fatalf("re-read worker: %v", err)
	}
	wantReceipt(t, "stampWorkerPlacementBlocked",
		got.LastOp, got.LastOpOK, got.LastOpLog, got.LastOpReason, got.LastOpAt,
		reconcileCmdStart, reason, now)
}

// 🔴 The two stampReceiptMissing arms are the literal shape the owner named:
// the same five lines written twice inside ONE function, once per row kind,
// differing only in which struct they come off. Both are pinned, separately and
// absolutely — and note the verb is p.RPC, NOT a START: a lapsed watch names the
// call it was waiting on. That is why the shared core takes the verb as a
// parameter instead of hard-coding reconcileCmdStart.

func TestStampReceiptMissing_MemberArmWritesTheFiveReceiptFields(t *testing.T) {
	const now = 1_769_904_324.0
	p := pendingReceipt{RPC: reconcileCmdStop, Warden: "mach-a", Deadline: now}

	s := newReconcileTestServer(t)
	m := testAgent("m-170e-lapsed")
	m.LastOp, m.LastOpOK, m.LastOpLog, m.LastOpReason, m.LastOpAt = "", nil, "stale log", "stale", 0
	putTestMember(t, s, m)

	s.stampReceiptMissing(m.ID, p, now)

	got, err := s.dal.GetMember(m.ID)
	if err != nil || got == nil {
		t.Fatalf("re-read member: %v", err)
	}
	wantReceipt(t, "stampReceiptMissing/member",
		got.LastOp, got.LastOpOK, got.LastOpLog, got.LastOpReason, got.LastOpAt,
		reconcileCmdStop, receiptMissingReason(p), now)
}

func TestStampReceiptMissing_WorkerArmWritesTheFiveReceiptFields(t *testing.T) {
	const now = 1_769_904_325.0
	// NOT a START on purpose: the paragraph above claims the verb is p.RPC, and a
	// START seed cannot demonstrate that — a core with the verb hard-coded to
	// reconcileCmdStart would still pass. This arm was seeded with a START until
	// t-170e stage 2; it stayed green under a verb-hard-coding mutant while the
	// member arm went red, which is how the gap was found.
	p := pendingReceipt{RPC: reconcileCmdStop, Warden: "mach-b", Deadline: now}

	s := newReconcileTestServer(t)
	w := OutsourceWorker{
		ID: "ow-170e-lapsed", Codename: "W2", Runtime: RuntimeClaude,
		Model: "opus", Effort: "medium", TaskID: "t-170e",
		Status: WorkerStatusActive, CreatedTS: 1.0, DesiredState: "online",
		LastOp: "", LastOpOK: nil, LastOpLog: "stale log", LastOpReason: "stale", LastOpAt: 0,
	}
	if err := s.dal.PutOutsourceWorker(w); err != nil {
		t.Fatalf("seed worker: %v", err)
	}

	s.stampReceiptMissing(w.ID, p, now)

	got, err := s.dal.GetOutsourceWorker(w.ID)
	if err != nil || got == nil {
		t.Fatalf("re-read worker: %v", err)
	}
	wantReceipt(t, "stampReceiptMissing/worker",
		got.LastOp, got.LastOpOK, got.LastOpLog, got.LastOpReason, got.LastOpAt,
		reconcileCmdStop, receiptMissingReason(p), now)
}
