package main

import "testing"

// op_receipt_single_source_t170e_test.go — T-170e stage 2 ⑤.
//
// stampMemberOpReceipt and stampWorkerOpReceipt were two hand-written copies of
// ONE decision: what an owner verb's in-memory receipt looks like when the
// handler is about to persist the row itself. Both wrote the same five fields
// with the same five values; the only difference was the struct they were
// handed. The pair is now one core (stampOpReceipt) with a one-line shell each.
//
// 🔴 THESE TWO TESTS ARE DELIBERATELY NOT ONE PARITY TEST. A test that only
// asserted "the member receipt equals the worker receipt" would stay GREEN for
// every change to the shared core — both sides move together, which is exactly
// what a shared core guarantees. Each side is therefore pinned to ABSOLUTE
// values, so a change to the one core reddens BOTH of them. That is the whole
// evidence that the two really are one implementation and not two copies that
// happen to agree today.
//
// The five fields are the receipt CONTRACT the DTO renders (last_op /
// last_op_ok / last_op_log / last_op_reason / last_op_at). last_op_ok is
// three-valued on both rows: nil means "no receipt folded yet", and a stamp
// must leave a non-nil FALSE — a receipt is a refusal or a deferral, never a
// success.

func TestStampMemberOpReceipt_WritesTheFiveReceiptFields(t *testing.T) {
	const reason = "held_down: the 改機器 was saved, nothing was started"
	const now = 1_769_904_321.0

	m := testAgent("m-170e-receipt")
	m.LastOp, m.LastOpOK, m.LastOpLog, m.LastOpReason, m.LastOpAt = "", nil, "stale log", "stale", 0

	stampMemberOpReceipt(&m, reason, now)

	if m.LastOp != reconcileCmdStart {
		t.Errorf("last_op = %q, want %q", m.LastOp, reconcileCmdStart)
	}
	if m.LastOpOK == nil || *m.LastOpOK {
		t.Errorf("last_op_ok = %v, want a non-nil false — a receipt is never a success", m.LastOpOK)
	}
	if m.LastOpLog != "" {
		t.Errorf("last_op_log = %q, want \"\" — the previous op's log must not be "+
			"left standing under a new receipt", m.LastOpLog)
	}
	if m.LastOpReason != reason {
		t.Errorf("last_op_reason = %q, want %q", m.LastOpReason, reason)
	}
	if m.LastOpAt != now {
		t.Errorf("last_op_at = %v, want %v", m.LastOpAt, now)
	}
}

func TestStampWorkerOpReceipt_WritesTheFiveReceiptFields(t *testing.T) {
	const reason = "session_alive: this worker was still running"
	const now = 1_769_904_321.0

	w := OutsourceWorker{ID: "ow-170e-receipt"}
	w.LastOp, w.LastOpOK, w.LastOpLog, w.LastOpReason, w.LastOpAt = "", nil, "stale log", "stale", 0

	stampWorkerOpReceipt(&w, reason, now)

	if w.LastOp != reconcileCmdStart {
		t.Errorf("last_op = %q, want %q", w.LastOp, reconcileCmdStart)
	}
	if w.LastOpOK == nil || *w.LastOpOK {
		t.Errorf("last_op_ok = %v, want a non-nil false — a receipt is never a success", w.LastOpOK)
	}
	if w.LastOpLog != "" {
		t.Errorf("last_op_log = %q, want \"\" — the previous op's log must not be "+
			"left standing under a new receipt", w.LastOpLog)
	}
	if w.LastOpReason != reason {
		t.Errorf("last_op_reason = %q, want %q", w.LastOpReason, reason)
	}
	if w.LastOpAt != now {
		t.Errorf("last_op_at = %v, want %v", w.LastOpAt, now)
	}
}
