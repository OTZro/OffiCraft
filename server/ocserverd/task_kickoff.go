package main

// task_kickoff.go — T-e77f 「叫開工」: the server tells an outsource executor,
// through a DURABLE chat row, the moment its task stops being un-advanceable.
//
// THE HOLE THIS CLOSES (measured, 2026-08-15, task t-a6fe65399dea / worker
// X-87). The worker booted, read its boot sequence, saw the task was FROZEN and
// correctly refused to advance it — it even said so to the owner. The task was
// later unfrozen. Nothing told the worker. And a codex worker's sidecar opens a
// turn only from an inbound SSE event line (cli/ocwarden/codex_session.go:
// steerOrStart is called from the listener branch alone; the identity heartbeat
// opens no turn), so the worker never looked again. A correct refusal became a
// permanent stall — indistinguishable from the outside from "it was never told".
//
// Both halves matter and only one of them is runtime-specific: a claude worker
// reaches the same dead end from the other side (it also stops at 等解凍 and
// also has nobody to tell it), so the notice is posted for EVERY outsource
// runtime, not only codex.
//
// WHY A REAL CHAT ROW and not a directed SSE frame (hub.PushDirected, the
// task-close nudge's transport): a frame is delivered only to a worker that is
// online at that instant and is gone otherwise — which is precisely the failure
// mode T-8a1e already recorded and T-74f8's dependency release already answered
// with a durable row. A chat row lands in the persistent inbox (so an offline
// or not-yet-booted worker reads it when it boots) AND fans the ordinary "chat"
// delta, which is the event line the codex sidecar turns into a turn. One write
// covers both the offline and the online case; a frame covers neither durably.
//
// SENDER is wireSystemSender ("system"), the same synthetic sender the reassign
// notices and the dependency release use — never the unfreezer's own id. The
// notice is machine-authored, and attributing it to the owner (who is usually
// the one clearing a freeze) would read as an owner DM and invite a reply to a
// person who never wrote it.
//
// This file adds no instruction to anybody's boot document. 「開工前先回報負責人」
// already lives in the shipped boot sequence and was OBSERVED being executed
// (the worker's own 03:11 message to the owner) — the message below only
// repeats it as context, it does not introduce it.

import "strings"

// taskKickoffChange names what just changed, in the recipient's own words. It
// is the first thing the notice says, because "this task is advanceable now" is
// only actionable when the reader knows which fact of the world moved.
type taskKickoffChange string

const (
	kickoffChangeUnfrozen   taskKickoffChange = "這張任務剛剛被「解除凍結」（不再是 frozen）"
	kickoffChangeAssigned   taskKickoffChange = "這張任務剛剛指派給你"
	kickoffChangeUnblocked  taskKickoffChange = "擋住這張任務的前置任務已經結案，依賴解除了"
	kickoffChangeDepsEdited taskKickoffChange = "這張任務的 blocked_by 前置依賴剛剛被移除"
)

// taskKickoffEligible reports whether t is an outsource task in an ADVANCEABLE
// state — the state in which a kickoff notice is a true statement. Every clause
// is a "must not receive" case from the ticket's DoD:
//
//   - member executors are out of scope wholesale (this is the outsource wake
//     problem; a member is a live session that was never asleep);
//   - an unassigned outsource slot has nobody to address;
//   - a terminal task has nothing to advance;
//   - a FROZEN task must never be told to start — telling it to would make the
//     server the thing that overrides the owner's 喊停;
//   - the reassigning lock means the successor still owes the predecessor a
//     handover; claim_task is the way out of it, not a kickoff.
//
// The live-blocker probe is NOT here: it costs a query per blocker, so it runs
// in refreshTaskKickoff after the cheap clauses have already decided.
func taskKickoffEligible(t Task) bool {
	return t.ExecutorKind == TaskExecutorOutsource &&
		t.ExecutorID != "" &&
		!TaskIsTerminal(t.Status) &&
		t.Priority != TaskPriorityFrozen &&
		t.Lock != TaskLockReassigning
}

// refreshTaskKickoff is the ONE entry point every trigger site calls, and it
// drives the de-duplication ledger in BOTH directions:
//
//	advanceable   + kickoff_notified_to != executor → post the notice, stamp it;
//	advanceable   + kickoff_notified_to == executor → nothing (the suppression);
//	NOT advanceable                                 → clear the stamp.
//
// The clear is what keeps this "once per transition" rather than "once ever": a
// task frozen again drops its stamp, so the NEXT unfreeze notifies again. A task
// that merely gets written repeatedly while staying advanceable (a cadence tick,
// a priority nudge high→mid, a second deps write) keeps its stamp and stays
// quiet. Reassignment resets it for free: the stamp holds an executor ID, so a
// new executor never matches an old stamp.
//
// Best-effort throughout — it runs at the tail of writes that have already
// landed, and a notice that failed must never fail the transition it follows.
// It persists t when the stamp moves (and only then), so a caller that has
// already written the row is not forced into a second write.
func (s *apiServer) refreshTaskKickoff(t *Task, change taskKickoffChange, trigger string) {
	if t == nil {
		return
	}
	if !taskKickoffEligible(*t) {
		s.clearTaskKickoffStamp(t)
		return
	}
	blockers, err := s.dal.ListTaskDeps(t.ID)
	if err != nil {
		// Unreadable deps ⇒ say nothing. A kickoff is a claim about the world
		// ("nothing is blocking you"), and the unsafe direction here is asserting
		// it without having been able to check.
		outsourceLog("kickoff %s: dep read failed, no notice sent: %v", t.ID, err)
		return
	}
	if s.hasLiveBlocker(blockers) {
		s.clearTaskKickoffStamp(t)
		return
	}
	worker, err := s.dal.GetOutsourceWorker(t.ExecutorID)
	if err != nil {
		outsourceLog("kickoff %s: worker read failed, no notice sent: %v", t.ID, err)
		return
	}
	// A worker that is gone or released has left; the task is waiting for the
	// scheduler to mint a successor, and THAT mint is the trigger that will
	// notify. Clearing the stamp here is deliberate: the successor must get its
	// own notice even if the departed worker already had one.
	if worker == nil || worker.Status == WorkerStatusReleased {
		s.clearTaskKickoffStamp(t)
		return
	}
	if t.KickoffNotifiedTo == t.ExecutorID {
		return
	}
	s.postTaskChat(*t, wireSystemSender, t.ExecutorID,
		s.taskKickoffBody(*t, change), trigger)
	t.KickoffNotifiedTo = t.ExecutorID
	if err := s.dal.PutTask(*t); err != nil {
		// The notice DID go out; only the ledger is behind, so the next
		// transition on this task would re-post it. Loud, because a duplicate
		// notice is otherwise indistinguishable from a correct one.
		outsourceLog("kickoff %s: notice posted but the ledger did NOT advance (%v) "+
			"— a later transition may re-post it", t.ID, err)
	}
}

// clearTaskKickoffStamp drops the ledger entry (and persists) when the task is
// observed non-advanceable. No-op when there is nothing to clear, so the common
// path writes nothing.
func (s *apiServer) clearTaskKickoffStamp(t *Task) {
	if t.KickoffNotifiedTo == "" {
		return
	}
	t.KickoffNotifiedTo = ""
	if err := s.dal.PutTask(*t); err != nil {
		outsourceLog("kickoff %s: ledger clear failed (the next transition may "+
			"stay silent): %v", t.ID, err)
	}
}

// taskKickoffBody writes the notice. It has to stand alone: the reader may be a
// worker whose only wake-up is this very message, so everything it needs to act
// is IN it — which task, what changed, where the procedure lives, and who to
// report to before starting.
func (s *apiServer) taskKickoffBody(t Task, change taskKickoffChange) string {
	var b strings.Builder
	b.WriteString("[" + TaskNo(t.ID) + "] " + string(change) +
		"，你負責的任務「" + t.Title + "」現在可以開始推進了。")
	b.WriteString("\n\n1. `get_task(\"" + t.ID + "\")` 讀任務內容與步驟。")
	if t.TypeKey != "" {
		label := t.TypeKey
		if m, err := s.dal.GetTaskManual(t.TypeKey); err == nil && m != nil {
			label = manualDisplayLabel(m.DisplayName, t.TypeKey)
		}
		b.WriteString("\n2. `get_task_manual(\"" + t.TypeKey + "\")` 讀任務手冊「" +
			label + "」，照手冊的 SOP 做。")
	} else {
		b.WriteString("\n2. 這張任務沒有綁任務類型，沒有手冊可讀：照任務描述與步驟 DoD 做。")
	}
	b.WriteString("\n3. 開工前先 post_chat 回報負責人：你已接手、現在要進行什麼、下一個回報點在哪。")
	return b.String()
}
