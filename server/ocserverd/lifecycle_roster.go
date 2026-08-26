package main

// lifecycle_roster.go — T-170e stage 3. THE middle layer the 外包＝正職 fold was
// missing.
//
// Migration 00025's design constitution says it plainly: 外包＝正職 — the only
// difference is an outsource member is minted/released alongside its task. The
// storage, the agent side and the decision FSM were folded in the earlier
// stages. What was left was this layer: the entry filter and the pre-decide
// roster formalities, which existed as FOUR hand-copies of one filter and TWO
// hand-maintained call lists — one in runReconcileTick, one in runOutsourceTick
// — with no mechanism that could make the second list wrong out loud.
//
// The failure that shape produces is not hypothetical and is not a coding
// mistake anybody could have caught by reading either list: a formality added
// to the staff list is INVISIBLE from the worker side, because the worker
// roster never passes through runReconcileTick at all (ListMembers is
// `WHERE kind != 'outsource'` by construction, dal.go). A pass that guards
// staff and a pass that does not exist look identical to a worker. That is how
// a worker ended up with no token-expiry lead and no survived-stop sweep while
// the code implementing both sat in the same package (fixed in stage 1).
//
// Two things live here, and both exist so that the difference between staff and
// outsource can only be spelled in ONE place:
//
//   - lifecyclePolicyFor — the ONE entry filter. 「正職會不會有 instance 存活取決於
//     人物設定有沒有這個角色，外包則是取決於 task 還是不是未完成狀態。其餘的部分應該
//     要統一才對」(owner, 2026-08-26). ShouldExist is that slot and nothing else
//     may branch on kind at the door.
//   - lifecycleRosterPasses — the ONE ordered list of formalities. Each pass
//     declares its own AppliesTo, exactly the way consumeUninstallIntentOnOffline
//     already declared `Kind != KindWarden` inside its own loop. Adding a
//     formality to the list gives it to BOTH producers by construction; giving
//     it to only one requires writing that restriction down as an AppliesTo,
//     where lifecycle_roster_parity_t170e_test.go reads it back by name.

// ── the entry filter ─────────────────────────────────────────────────────────

// LifecyclePolicy is the ONE slot the 正職／外包 difference is allowed to live in.
//
// ⚠️ ShouldExist is a snapshot answer about the row it was built from — it takes
// no arguments because lifecyclePolicyFor already closed over the row. Build a
// fresh policy after any write you want it to see.
//
// 🔴 THE SLOT IS DELIBERATELY ONE FIELD WIDE TODAY. The retirement half of the
// owner's sentence (a worker is RELEASED when its task closes; a member is
// dismissed when its role goes) is NOT wired here, and adding an OnRetire field
// that nothing calls would be a promise the code does not keep. Worker release
// currently runs through ReleaseWorkersForTask off closeTask, and staff
// dismissal through its own handler; converging those two is a behaviour change
// with an owner-visible face (who gets released, and when), so it belongs to a
// step that is allowed to change behaviour. This comment is the record that the
// omission is a decision.
type LifecyclePolicy struct {
	// ShouldExist answers "should this row still have an instance at all?" —
	// the pre-decide entry filter both producers ask before offering a row to
	// any formality below.
	ShouldExist func() bool
}

// lifecyclePolicyFor answers the entry question for ONE row, staff or outsource.
//
// The two arms are the owner's two sentences and nothing more:
//
//   - outsource: the row is alive while its worker is ACTIVE (bound to a task
//     and past its spawn) and the owner has not held it down. A released worker
//     has no task left to be unfinished; an ASSIGNED one has no session yet to
//     wind down.
//   - staff: the row is alive while the roster still carries it. A warden is
//     excluded unless it is being uninstalled — a warden is never an
//     agent-lifecycle spawn/stop candidate, it is the thing that executes them.
//
// 🔴 The staff arm reads roster_status, not the role table. That is the same
// question one indirection later (a member whose role went is soft-removed from
// the roster), and it is the question every pre-existing call site was already
// asking — this function was extracted from them, not written fresh, so it may
// not quietly become stricter.
func lifecyclePolicyFor(m Member) LifecyclePolicy {
	if m.Kind == KindOutsource {
		return LifecyclePolicy{ShouldExist: func() bool {
			return workerStatusFromMember(m.RosterStatus, m.ActivatedTS) == WorkerStatusActive &&
				m.DesiredState != DesiredStateOffline
		}}
	}
	return LifecyclePolicy{ShouldExist: func() bool {
		if m.RosterStatus != RosterStatusActive {
			return false
		}
		if m.Kind == KindWarden && parseDesired(m.DesiredState) != DesiredStateUninstall {
			return false // no warden reconciles another warden's spawn/stop
		}
		return true
	}}
}

// ── the formalities ──────────────────────────────────────────────────────────

// The pass names. They are the vocabulary the parity test speaks, so a pass
// that stops reaching a worker fails by NAME rather than as an anonymous
// "something did not happen".
const (
	lifecyclePassContextHigh     = "context_high_recycle"
	lifecyclePassTokenExpiry     = "token_expiry_winddown"
	lifecyclePassRecycleBreak    = "recycle_loop_break"
	lifecyclePassStaleStopping   = "stale_stopping_clear"
	lifecyclePassUninstallIntent = "uninstall_intent_consume"
)

// lifecycleRosterPass is ONE pre-decide roster formality.
//
// Run mutates its slice IN PLACE (every pass does — the stamp has to be visible
// to the rest of the SAME tick, or the collect is always one tick late), and
// runLifecycleRosterPasses copies the mutations back onto the caller's roster.
type lifecycleRosterPass struct {
	// Name is the stable identifier the parity test asserts on. Never a line
	// number and never a file — those move under pure-comment commits.
	Name string
	// AppliesTo is the pass's OWN declaration of which rows it is for. A pass
	// that is genuinely for everybody says so; a pass that is not has to write
	// the restriction down here, in the one place a reader is looking.
	AppliesTo func(m Member) bool
	// Run is the pass itself.
	Run func(roster []Member, now float64)
}

func lifecycleEveryKind(Member) bool { return true }

// lifecycleRosterPasses is THE list. Order is load-bearing and is the order
// runReconcileTick has always used:
//
//	① context thresholds, then ② token expiry — both stamp passes skip a row
//	that already carries refocus_since, so whichever runs first owns the epoch.
//	Reversed, a session that is BOTH out of context and near token expiry would
//	be stamped token_expiry — a soft, unclocked cause — and the context pass's
//	one promotion arm would then decline it (canPromoteToAcceleratedStop only
//	promotes a context_notice epoch), so the second context threshold would
//	never open its 加速停止 on that member at all.
//
// Then the three clean-up passes, which do not compete for the epoch.
func (s *apiServer) lifecycleRosterPasses() []lifecycleRosterPass {
	return []lifecycleRosterPass{
		{
			Name:      lifecyclePassContextHigh,
			AppliesTo: lifecycleEveryKind,
			Run:       s.stampContextHighRecycle,
		},
		{
			// A worker's session token is minted by mintAgentToken with the same
			// TTL a staff token gets (worker_spawn.go), so it dies the same way —
			// and the whole close-out is MCP calls carrying it.
			Name:      lifecyclePassTokenExpiry,
			AppliesTo: lifecycleEveryKind,
			Run:       s.stampTokenExpiryWinddown,
		},
		{
			// 🔴 STAFF-ONLY, AND THIS IS THE ONE HONEST EXCEPTION IN THE LIST —
			// not a leftover. A worker already has a loop-break, in
			// autoHandoverWorker arm (1), and it asks a DIFFERENT question:
			// "did a session boot AFTER the stamp" (gauge boot_ts > refocus_since)
			// versus this pass's "desired online ∧ not online". Handing workers
			// this pass as well would mean two collectors on one latch, which is
			// exactly the double-kill shape T-72dd removed. Converging the two
			// rules is a behaviour change and needs its own owner-gated step; what
			// this line buys today is that the divergence is WRITTEN DOWN in the
			// list instead of being invisible by omission.
			Name:      lifecyclePassRecycleBreak,
			AppliesTo: func(m Member) bool { return m.Kind != KindOutsource },
			Run:       func(roster []Member, _ float64) { s.clearRecycleMarkersOnRespawn(roster) },
		},
		{
			// The survived-stop auto-clear. Without it on the worker side the
			// anchor sat on the row for the life of the session and the cockpit
			// read 停止中 for a worker that was plainly working.
			Name:      lifecyclePassStaleStopping,
			AppliesTo: lifecycleEveryKind,
			Run:       s.clearStaleStoppingOnOnline,
		},
		{
			// Warden-only, and it always was: the pass's own loop opens with
			// `m.Kind != KindWarden { continue }`. Hoisting that guard up here
			// changes nothing about what runs — it makes the restriction readable
			// from the list, which is the whole mechanism.
			Name:      lifecyclePassUninstallIntent,
			AppliesTo: func(m Member) bool { return m.Kind == KindWarden },
			Run:       func(roster []Member, _ float64) { s.consumeUninstallIntentOnOffline(roster) },
		},
	}
}

// runLifecycleRosterPasses runs every formality, in order, over one roster
// snapshot. Rows a pass does not apply to are not shown to it at all.
//
// The sub-slice + copy-back is not decoration: the passes mutate their slice in
// place and then persist, and the caller's snapshot has to end the call holding
// what was stamped — otherwise the rest of the same tick decides off the
// pre-stamp row and every collect is one cadence period late.
func (s *apiServer) runLifecycleRosterPasses(roster []Member, now float64) {
	for _, p := range s.lifecycleRosterPasses() {
		idx := make([]int, 0, len(roster))
		sub := make([]Member, 0, len(roster))
		for i := range roster {
			if p.AppliesTo(roster[i]) {
				idx = append(idx, i)
				sub = append(sub, roster[i])
			}
		}
		if len(sub) == 0 {
			continue
		}
		p.Run(sub, now)
		for j, i := range idx {
			roster[i] = sub[j]
		}
	}
}

// runWorkerLifecyclePasses is the OUTSOURCE producer's single door into the
// list above, and the only place the worker⇄member projection is spelled.
//
// It used to be ~40 lines inlined in runOutsourceTick plus a SECOND hand-copy
// in the test helper workerTickPass — and the hand-copy had ALREADY drifted:
// it projected into stampContextHighRecycle only, so every test that drove it
// was measuring a tick that had not run the token-expiry or survived-stop pass
// since stage 1 wired them. That drift is the reason this is a function.
//
// Only the four wind-down fields are folded back, never the whole projection:
// workerFromMember re-derives Status from activated_ts, and round-tripping a
// row through it here would let an unrelated derivation change ride in on a
// context stamp.
//
// Callers hold s.outsourceMu.
func (s *apiServer) runWorkerLifecyclePasses(workers []OutsourceWorker, now float64) {
	roster := make([]Member, 0, len(workers))
	index := make([]int, 0, len(workers))
	for i := range workers {
		m := memberFromWorker(workers[i])
		if !lifecyclePolicyFor(m).ShouldExist() {
			continue
		}
		roster = append(roster, m)
		index = append(index, i)
	}
	if len(roster) == 0 {
		return
	}
	s.runLifecycleRosterPasses(roster, now)
	for j, i := range index {
		workers[i].RefocusSince = roster[j].RefocusSince
		workers[i].RefocusOp = roster[j].RefocusOp
		workers[i].StoppingSince = roster[j].StoppingSince
		workers[i].StoppedSince = roster[j].StoppedSince
	}
}
