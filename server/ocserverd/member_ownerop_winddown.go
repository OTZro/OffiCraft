package main

// T-b6d9 — 「所有換手都可以給他機會收尾」 for STAFF members: the twin of the
// outsource-worker rule that landed as T-98f4 rule 2 (server/CLAUDE.md, section
// 「所有 owner 動詞都給收尾機會」).
//
// THE DISCRIMINATOR IS ONE FIELD. The 下線程序 wake an agent prints
// is fanned by cli/ocagent's recycleHook.maybeRecycle, and its gate is hard-
// wired to `desired_state == online ∧ refocus_since > 0` on the member row.
// A verb that does not stamp refocus_since is therefore INVISIBLE to the agent
// — there is no partial credit, no shorter warning, nothing. Before this
// ticket the staff verbs split three ways:
//
//	重啟   (refocus_member / restart_self) — stamps, dispatches NOTHING, and the
//	       reconcile recycle arm waits for report_stopped or the epoch's grace
//	       (recycleGraceFor — which since T-ed79 answers "no clock" for every
//	       cause except 加速停止, the second context threshold).
//	       FULL wind-down. UNCHANGED by this ticket (the sentinel).
//	改機器 (relocate)                       — stamped nothing; reconcileMemberNow
//	       took decideUp's relocate arm and dispatched a robust STOP ON THE
//	       SPOT. Zero warning, zero grace, not even a stopping_since, so the
//	       member simply vanished from the cockpit mid-thought.
//	換模型 (update_member runtime/model/effort) — stamped nothing and dispatched
//	       nothing: a clean 200 while the running session kept the OLD value
//	       until something else happened to respawn it.
//
// Both are now routed through this ONE funnel, exactly as the three outsource
// verbs funnel through respawnWorkerForOwnerOp. The caller writes its change
// onto the row, asks armMemberOwnerOpHandover whether there is anything to
// wind down, and persists ONCE — so the new pin / model and the refocus epoch
// land in the same write, and the single member delta the agent wakes on
// already carries the new values. The 收口 is the pre-existing §4.5 machinery:
// the agent's own report_stopped (→ dispatchRobustStopNow) or decideUp's
// recycle arm at recycleGraceFor(refocus_op). Either way the next tick's plain START re-mints
// the boot frame off the row, which is where the new machine / model now live.
//
// ── memberHasStateToFlush vs workerHasStateToFlush, cell by cell ─────────────
// 🔴 READ THIS FIRST, BECAUSE THE SENTENCE THAT USED TO OPEN IT IS NO LONGER
// TRUE OF THE CODE. It said the staff predicate was "COPIED, not re-derived"
// from the worker one. The COPY IS GONE: the arms that agree — a live session,
// and this epoch's wind-down not already collected — are ONE expression,
// hasUncollectedOnlineOwnerOpState (below), which both predicates call. Neither
// side re-derives anything.
//
// What is left on each side is a SHELL of guards that do NOT agree, and this
// table is the record of why they must not be flattened into the shared call.
// The reason the original sentence gave still applies to the shells: both HIGH
// findings in the worker predicate's review were the SAME boundary drawn wrong,
// so the danger this table guards against is somebody deriving that boundary a
// second time and drawing it differently — not somebody sharing the one that
// was already argued. Sharing the core is the opposite of re-deriving it.
// worker_spawn.go's comment block is still the authority for WHY each arm
// exists; this is the mapping of what differs:
//
//	worker: desired_state == offline → held_down, never winds down
//	  staff: SAME RULE, DIFFERENT PLACE, and that is the one asymmetry a reader
//	  has to hold. On the staff side it is inside the predicate, spelled
//	  aRefocusStampWouldReachTheAgent. On the worker side the predicate does not
//	  carry it at all: respawnWorkerForOwnerOp's FIRST gate answers held_down
//	  for a desired-offline worker and returns BEFORE workerHasStateToFlush is
//	  ever consulted, so a guard inside the predicate would be unreachable.
//	  It is load-bearing on both sides: an explicit 停止 dominates every other
//	  owner verb, and a refocus stamp on a desired-offline row is pure noise —
//	  decideUp is not even reached (decideDown owns it) and the agent's own gate
//	  re-checks desired_state == online, so nothing would ever read the marker.
//	  🔴 THE WORKER SIDE'S COMPENSATION IS PINNED, not assumed:
//	  TestOwnerOp_StoppedWorkerStillOnlyGetsAReceipt drives a desired-offline
//	  worker that is ONLINE — the state in which the shared core answers YES —
//	  through the 換 model face and requires a held_down receipt, no epoch, and
//	  zero frames. Without that test the compensation is decoration.
//
//	worker: !hub.IsOnline → immediate
//	  staff: SAME — and "same" is now literal: this arm lives in
//	  hasUncollectedOnlineOwnerOpState, which both shells call. hub.IsOnline is
//	  the exact same authority reconcileOne feeds into obs.Online, so "the
//	  recycle arm can fire" and "we opened a wind-down" can never disagree.
//
//	worker: Status != active (never claimed its task) → immediate
//	  staff: NO ANALOGUE, deliberately. assigned→active WAS the get_my_task
//	  claim, a lifecycle step a staff member simply does not have; there is no
//	  state in which a live staff session has PROVABLY never been handed work.
//	  Omitting it errs toward winding down — the safe direction. It used to be
//	  priced as "at most one grace CEILING"; since T-ed79 there is no ceiling on
//	  this funnel at all (below), so the price is different, not smaller.
//	  🔴 T-4595 RESOLVED THIS ASYMMETRY THE OTHER WAY: get_my_task is retired
//	  and the flip moved to report_waking, the FIRST boot verb — so "active" no
//	  longer proves a worker was ever handed task content, and the worker arm
//	  was DELETED rather than kept as a stale proof. Both predicates now agree,
//	  and they agree on THIS side of the argument: the safe direction. The cost
//	  is the one this paragraph already priced for staff — a wind-down window
//	  that ends when the session answers report_stopped (T-ed79: and NOT before,
//	  since neither staff owner-verb is on a clock).
//
//	worker: RefocusSince > 0 ∧ StoppedSince > 0 (this epoch already collected)
//	  staff: SHARED, not copied — the epoch scoping included — because the
//	  two-latch hazard it was written for exists here identically:
//	  HandleReportStoppedApiSelfStoppedPost latches StoppedSince on the FIRST
//	  stopped-report whether or not a handover is in flight, and only sets
//	  recycleKill when refocus_since > 0. Read GLOBALLY, an ordinary
//	  deactivate→report_stopped would leave a latch that claims "already
//	  collected" and shoot every later 改機器 / 換模型 on the spot — the
//	  STALE-LATCH HIGH, in staff clothing. Pairing it with RefocusSince > 0 asks the
//	  question actually meant (is THIS epoch's wind-down collected?), and the
//	  stale latch heals itself because arming the next epoch zeroes it.
//	  Dropping the StoppedSince half instead resurrects the 收口-window HIGH: a verb
//	  arriving inside the collect window would open a SECOND wind-down that
//	  dispatches nothing, while the in-flight respawn boots on the OLD value.
//
//	worker: ownerOpDisplacesTheSession deny-list (重啟 skips the wind-down)
//	  staff: N/A — 重啟 is not in this funnel. refocus_member / restart_self ARE
//	  the wind-down (they stamp and return), and activate is a wake, not a
//	  displacement. The staff funnel carries only 改機器 and 換模型, and both act
//	  on a session the owner wants to keep running.
//
// The active+online cell is an honest fallback, not a positive detection: the
// server has zero visibility into an agent's transcript, so any finer test
// (context pct, uptime, message counts) would be a guess dressed as a
// criterion, and guessing wrong silently discards a round of learnings.
// 🔴 THERE IS NO CEILING ON THIS FUNNEL ANY MORE (T-ed79). This used to read
// "the grace is a CEILING — the 收口 fires the instant the agent answers
// report_stopped", which priced the wait as "at most RecycleGrace". Both staff
// owner-verbs are 停止 now (winddownKindFor answers soft), so report_stopped is
// not merely the EARLY exit, it is the ONLY one apart from the owner's
// force-stop. A session with nothing to save still ends in seconds — that half
// is unchanged, and it is what makes the honest fallback affordable — but a
// session that never answers stays up until the owner presses the button.
//
// Cost, recorded honestly: after 改機器 / 換模型 the member keeps running on the
// OLD machine / OLD model until it answers, with the cockpit showing 換手中 for
// that whole time (refocus_since > 0 — the same projection 重新聚焦 already
// uses). That is the trade the owner asked for, and it is now unbounded on
// purpose rather than bounded by RecycleGrace.
//
// Agent-facing surface is unchanged (root CLAUDE.md §9c): same member-topic
// delta, same refetch, same 下線程序 wake out of the same recycleHook. No new
// tool, no new step ⇒ seeds/ needs no companion change.

// The owner verbs that funnel through armMemberOwnerOpHandover, named so the
// log tags cannot drift from their call sites.
const (
	memberOpRelocate = "relocate"      // 改機器
	memberOpModel    = "runtime/model" // 換 model / runtime / effort
)

// The remaining causes that stamp refocus_since WITHOUT going through
// armMemberOwnerOpHandover. Together with the two above these are the closed
// set MemberDTO.refocus_op serves: the cockpit needs the cause to say "winding
// down so your change can take effect" instead of "last refocus", which reads
// as history. They are stamped and cleared in lockstep with refocus_since —
// a cause outliving its window would be worse than none.
const (
	refocusOpContextHigh = "context_high" // second context threshold — 加速停止
	// refocusOpContextNotice is the FIRST context threshold (notice_pct): a
	// plain 停止, opened where the agent is only ASKED to wind down. It is named
	// after the setting it fires on so the two cannot drift apart in a reader's
	// head. Before T-ed79 the first threshold sent an SSE band and opened no
	// wind-down at all, so an agent that ignored one frame met the SECOND
	// threshold with no close-out started and 120 seconds to do it in.
	refocusOpContextNotice = "context_notice"
	refocusOpRefocus       = "refocus"      // owner pressed 重新聚焦
	refocusOpRestartSelf   = "restart_self" // the agent asked for its own handover
	// refocusOpTokenExpiry is the TOKEN-LIFETIME cause (T-ed79): the session's
	// agent token is about to expire, so the wind-down is opened while the token
	// still works. It is a plain 停止 — the agent is shown the sequence and
	// collected by its own stopped report or by the owner's force-stop — for the
	// same reason 重新聚焦 is: nothing here is an emergency the owner asked to be
	// cut short, and a countdown would only make the close-out worse.
	//
	// 🔴 WHY IT HAS TO EXIST AT ALL: an expired agent token does not degrade
	// gracefully. Every MCP call the offboard sequence makes — report_stopping,
	// post_chat, the lesson write, report_stopped — goes through the same bearer
	// token, so a session that reaches expiry mid-thought cannot file the
	// hand-off it is being asked for; it can only fail. Renewal used to depend
	// on the agent noticing on its own.
	refocusOpTokenExpiry = "token_expiry"
	// refocusOpAcceleratedStop is the OWNER-PRESSED 加速停止 (T-ed79, owner
	// 2026-08-21 「停止 → 加速停止 → 強制停止」). It is the middle rung of a
	// three-step escalation the owner walks by hand: 停止 asks and waits
	// forever, 加速停止 says "you now have until T", 強制停止 cuts the session
	// off with no sentence at all.
	//
	// 🔴 IT IS A CLOCK THE OWNER ASKED FOR, WHICH IS WHY IT DOES NOT REOPEN THE
	// RULING 下線 CARRIES NO 兜底 (rc-27d1710174dd 「不要兜底：只有你按強制下線
	// 才收它」). That ruling is about the SERVER deciding time is up on its own.
	// Nothing here fires unless the owner presses the button, so the escalation
	// is still his — this only gives his hand a rung between "wait indefinitely"
	// and "kill now", which is the rung he asked for.
	refocusOpAcceleratedStop = "accelerated_stop"
)

// memberHasStateToFlush answers the one question the rule turns on: is there
// anything for this member to wind down, or should the owner's verb take
// effect immediately?
//
// The ANSWER is hasUncollectedOnlineOwnerOpState, the one expression the worker
// twin calls too. What is written here is the staff SHELL — two guards that the
// worker side does not have in this function, each for a reason the cell-by-cell
// mapping above states. Do not flatten them into the shared call: the kind guard
// has no worker analogue at all, and the worker's desired-offline equivalent is
// its caller's first gate, which returns before this question is asked.
func (s *apiServer) memberHasStateToFlush(m Member) bool {
	// Staff only. A warden runs no ocagent and would never read the marker;
	// an outsource row has its own funnel (respawnWorkerForOwnerOp) and never
	// reaches these handlers anyway (resolveMember folds kind=outsource onto
	// errNotFound). Both are refused here rather than relied upon upstream.
	if m.Kind != KindAssistant {
		return false
	}
	if !aRefocusStampWouldReachTheAgent(m) {
		return false
	}
	return hasUncollectedOnlineOwnerOpState(m.RefocusSince, m.StoppedSince, s.hub.IsOnline(m.ID))
}

// aRefocusStampWouldReachTheAgent is the server half of a CROSS-LAYER contract
// (root CLAUDE.md §9c; T-ccc7). The agent prints the 下線程序 wake
// from cli/ocagent/listen_hooks.go maybeRecycle, whose FIRST condition is
// `desired_state == online`. So stamping refocus_since on a member the server
// has already decided should be offline is not a weaker signal — it is NO
// signal: the agent returns early and prints nothing, while reconcile only
// reads RefocusSince on the decideUp arm a desired-offline member never takes.
// The stamp is then stranded: activate does not clear it, so the marker
// outlives the stop and the next wake can be robust-stopped on an epoch that
// expired while nobody was listening.
//
// Every site that stamps a member's refocus epoch must satisfy this, and all of
// them now say so BY NAME.
//
// 🔴 Two of them used not to. POST /members/{id}/refocus and POST /self/refocus
// were protected by a CORRELATION: they gated on PresenceState, which stops
// projecting online once StoppingSince > 0, and every path that sets desired
// offline sets that anchor in the same write. Adding the explicit check was
// measured to be dead code at the time (T-ccc7), and that measurement was
// true — of that code.
//
// It stopped being true the moment those gates had to change: an agent working
// its offboard sequence reports stopping FIRST (step 1), which made
// PresenceState project `stopping` and had both endpoints refusing the very
// caller the notice tells them to be (T-a9d6). Moving them onto the live-session
// fact removed the correlation, and with it — silently — the invariant that had
// been riding on it. The existing T-ccc7 tests caught that within one run, which
// is the whole reason this predicate is named rather than implied: a protection
// that holds by coincidence disappears the instant somebody replaces the
// coincidence, and nothing about the edit looks like it touched the invariant.
// The third site, the context-high auto-stamp in reconcile.go, had no proxy at
// all and stamped members on their way offline until T-ccc7.
func aRefocusStampWouldReachTheAgent(m Member) bool {
	return m.DesiredState == DesiredStateOnline
}

func hasUncollectedOnlineOwnerOpState(refocusSince, stoppedSince float64, online bool) bool {
	return online && !(refocusSince > 0.0 && stoppedSince > 0.0)
}

// winddownKindFor is THE judgement about a wind-down cause, and the only one.
// The clock (recycleGraceFor) and the sentence (offboardKindOf) both read it,
// so the two cannot disagree about the same member — a clock nobody announces,
// or a countdown nobody is counting, is now a change to ONE line rather than
// two files that happen to agree.
//
// 🔴 FINAL IS THE POSITIVE CONDITION, and the default is SOFT. It used to be
// the other way round — everything fell through to "on the clock" and 重新聚焦
// was carved out as the single exception — which meant every new cause arrived
// carrying a deadline by accident, including ones the owner had ruled must
// carry none. Owner model (T-ed79): the only 加速停止 is the one context
// pressure opens at the SECOND threshold; 停止 is everything else — the agent
// is shown the sequence and collected by its own stopped report or by the owner
// pressing force-stop. Adding a cause to the final set is now something you
// have to TYPE, on this line, where the ruling is written down.
//
// (force-stop is not a kind here at all: it sends nothing and removes the
// member on the spot — see HandleForceStopMember.)
func winddownKindFor(op string) (kind string, clocked bool) {
	// TWO causes are 加速停止, and they are the two the owner named: the one
	// context pressure opens at the SECOND threshold, and the one he presses
	// himself. They share ONE grace (stop.accelerated_grace_secs, folded onto
	// cfg.RecycleGrace by reconcileConfigLive) because they are the same verb
	// with two triggers — 「統一在第二門檻跟加速停止使用」 — so an owner tuning
	// the number can never end up with the automatic and the manual arm
	// counting different seconds.
	if op == refocusOpContextHigh || op == refocusOpAcceleratedStop {
		return offboardKindFinal, true
	}
	return offboardKindSoft, false
}

// armRefocusEpoch is the ONE way a refocus epoch is opened. It MUTATES m and
// persists nothing: every caller folds it into its own single putMember, so the
// stamp and whatever else that write carries are one row write and one delta.
//
// 🔴 The two zeroed anchors are the whole reason this is a named function and
// not four hand-written lines. A NEW epoch must never inherit the PREVIOUS
// wind-down's latch, and the reader that latch feeds is destructive:
//
//   - decideUp's recycle arm reads AgentStopped = stopped_since > 0 and, with a
//     refocus marker present, robust-stops the member ON THE SPOT — zero grace,
//     no close-out. A stale stopped_since therefore turns the very next epoch
//     stamped on that member into an immediate kill, in the same tick, whatever
//     opened it.
//
// 🔴 A SECOND bullet used to stand here — "the SSE stop gate (api_infra.go)
// refuses a reconnect once stopped_since is set, so a stale latch also rejects
// the NEXT close-out's reconnect". It is FALSE inside this function's range and
// always was: that gate requires `desired_state == offline`, while every caller
// of armRefocusEpoch is behind aRefocusStampWouldReachTheAgent, which is
// `DesiredState == DesiredStateOnline`. The gate can never see one of these
// rows. The remaining bullet is real, and it is enough on its own — one true
// destructive reader justifies the shared function; a second, invented one only
// teaches the next reader a protection that will not be there when they rely on
// it.
//
// Three of the four stamp sites used to write refocus_since/refocus_op alone
// (POST /members/{id}/refocus, restart_self's staff arm, and the context
// auto-stamp); only the owner-verb funnel below cleared the anchors. Sharing
// one function is what makes "a fresh epoch starts from a clean sheet" a
// property of the operation rather than of whoever remembered it.
func armRefocusEpoch(m *Member, op string, now float64) bool {
	if !winddownStageMayAdvanceTo(*m, op) {
		return false
	}
	m.RefocusSince = now
	m.RefocusOp = op
	m.StoppingSince = 0.0
	m.StoppedSince = 0.0
	return true
}

// winddownStageRankOf ranks ONE cause on the owner's three-step ladder
// (2026-08-24, verbatim): 「下線 → 加速 → 強制。後者一旦發出我們就不該發出前者」.
//
// 🔴 THE RANK IS DERIVED, NOT LISTED. It reads winddownKindFor — already THE
// judgement about a cause — rather than carrying a second table of causes
// beside it. A second list would be a second truth source: adding a cause to
// one and not the other produces a member whose stage the ladder and the clock
// disagree about, and nothing would report it. The ladder therefore has exactly
// as many causes as winddownKindFor does, forever, without anyone maintaining
// that.
//
// Stage 3 (強制停止) is deliberately NOT a cause: force-stop sends nothing and
// is a property of the MEMBER (forcedEpochLive), not of a refocus op — which is
// why the member-level reading below is a separate function.
func winddownStageRankOf(op string) int {
	if kind, _ := winddownKindFor(op); kind == offboardKindFinal {
		return winddownStageAccelerated
	}
	return winddownStageStop
}

const (
	// winddownStageNone is "no wind-down is open on this member" — below every
	// real stage, so the very first stamp of an epoch always advances.
	winddownStageNone        = 0
	winddownStageStop        = 1 // 停止
	winddownStageAccelerated = 2 // 加速停止
	winddownStageForced      = 3 // 強制停止
)

// winddownStageOf reads how far along the ladder this member ALREADY is.
func winddownStageOf(m Member) int {
	if forcedEpochLive(m) {
		return winddownStageForced
	}
	if m.RefocusSince <= 0.0 {
		return winddownStageNone
	}
	return winddownStageRankOf(m.RefocusOp)
}

// winddownStageMayAdvanceTo answers the owner's rule: a stamp that would move
// this member BACKWARDS down the ladder is refused.
//
// 🔴 EQUAL RANK IS ALLOWED, and that is not an oversight. The rule he wrote is
// about a LOWER stage arriving after a higher one; re-stamping the same stage
// is a re-arm (a fresh epoch on a clean sheet), which several callers do on
// purpose and which takes nothing away from the agent. Refusing it would turn
// this guard into a behaviour change nobody asked for, on paths that are not
// what he was describing.
//
// What this actually stops: 重新聚焦 / restart_self / 換機器 / 換 model landing
// on a member that is already in 加速停止 — each of which used to succeed, push
// the stage back to 停止, AND clear the deadline with it, so an agent counting
// down silently stopped counting.
func winddownStageMayAdvanceTo(m Member, op string) bool {
	return winddownStageRankOf(op) >= winddownStageOf(m)
}

// armMemberOwnerOpHandover stamps a FRESH refocus epoch on the member when
// there is state to flush, and reports whether it did. It MUTATES m and
// persists nothing: the caller folds this into its own single putMember so the
// owner's change and the epoch are one atomic write and one delta.
//
// The stale wind-down anchors are cleared with the stamp — a new epoch never
// inherits an old latch, which is also what makes the "already collected" arm
// above self-healing.
func (s *apiServer) armMemberOwnerOpHandover(m *Member, op string) bool {
	if !s.memberHasStateToFlush(*m) {
		return false
	}
	// The ladder rule applies to the owner-verb funnel too: 換機器 / 換 model are
	// 停止, so neither may land on a member that is already in 加速停止 and hand
	// it back the slower procedure. Reporting false here is what makes the
	// caller fold nothing into its write — the owner's change still applies,
	// the wind-down stage simply does not move backwards with it.
	if !armRefocusEpoch(m, op, nowSecs()) {
		reconcileLog("recycle: %s %s — wind-down NOT re-opened: member is already "+
			"further along the ladder (下線 → 加速 → 強制)", op, m.ID)
		return false
	}
	if grace, clocked := recycleGraceFor(op, s.reconcileConfigLive()); clocked {
		reconcileLog("recycle: %s %s — wind-down opened (collect on stopped-report or +%.0fs)",
			op, m.ID, grace)
	} else {
		reconcileLog("recycle: %s %s — wind-down opened (collect on stopped-report or force-stop; no clock)",
			op, m.ID)
	}
	return true
}
