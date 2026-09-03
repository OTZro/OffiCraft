package main

// verb_population_parity_t65_test.go — T-65 包①: the 動詞 × 人口 matrix.
//
// A staff member and an outsource worker live in the SAME member table, but the
// seven lifecycle verbs are TWO SEPARATE handler families. There is no way to
// feed one handler both populations — every member-side verb resolves through
// s.resolveMember(id, staffOnly), and api_helpers.go's
// `if scope == staffOnly && m.Kind == KindOutsource` 404s an ow- id outright.
// So the matrix is not "one handler, two inputs": it is
//
//	動詞 × 人口 ⇒ each population's OWN handler, same seeded start state,
//	             then COMPARE THE TERMINAL ROW field by field.
//
// Every field that is expected to end up the same is asserted equal, and every
// field that ends up DIFFERENT must appear in knownDivergences with a sentence
// saying why it is different TODAY. An unlisted difference fails by name; a
// listed row whose two sides have converged fails as stale. That is what makes
// this table the mechanical guard for every later T-65 package: converging one
// verb means deleting its rows here, and nothing else can delete them quietly.
//
// 🔴 TWO RULES THIS FILE IS BOUND BY, both learned the expensive way:
//
//  1. Assertions go through the REAL handler seam — the method routes.go's
//     `Handler:` field points at — never the pure helpers underneath
//     (armRefocusEpoch / stopEpochAnchor / respawnWorkerForOwnerOp). T-14 PR ①
//     shipped a parity test over the pure function; both CALL SITES could then
//     have their guards deleted with the whole suite still green.
//  2. Every expected value below is a LITERAL, transcribed by hand from the
//     assignment in the handler. Nothing here calls production code to compute
//     what production code should produce — an expectation derived that way is
//     true by construction and kills no mutant.

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"sort"
	"testing"
)

// ── the observable terminal state ────────────────────────────────────────────

// anchorClass buckets a float anchor relative to the instant the verb ran. The
// three buckets are what the divergences actually turn on: "was it cleared",
// "was it (re)stamped now", and — for 強制停止 — "was a FUTURE anchor pulled
// back to now". A raw float would make every row unwritable as a literal.
type anchorClass string

const (
	anchorZero   anchorClass = "zero"   // cleared / never set
	anchorPast   anchorClass = "past"   // <= the instant the verb ran
	anchorFuture anchorClass = "future" // still ahead of it (an untouched future stamp)
)

// terminalState is the row as both populations project it. GetMember and
// GetOutsourceWorker read the SAME table (migration 00025 folded it), so a
// worker is folded through memberFromWorker and the two are literally
// comparable.
type terminalState struct {
	Status           int
	DesiredState     string
	Stopping         anchorClass
	Stopped          anchorClass
	Refocus          anchorClass
	RefocusOp        string
	Waking           anchorClass
	RestartAfterStop bool
	DesiredMachineID string
}

// parityFields is the compared field set, in a stable order so a failure names
// the same field every run.
var parityFields = []string{
	"http_status", "desired_state", "stopping_since", "stopped_since",
	"refocus_since", "refocus_op", "waking_since", "restart_after_stop",
	"desired_machine_id",
}

func (s terminalState) field(name string) any {
	switch name {
	case "http_status":
		return s.Status
	case "desired_state":
		return s.DesiredState
	case "stopping_since":
		return s.Stopping
	case "stopped_since":
		return s.Stopped
	case "refocus_since":
		return s.Refocus
	case "refocus_op":
		return s.RefocusOp
	case "waking_since":
		return s.Waking
	case "restart_after_stop":
		return s.RestartAfterStop
	case "desired_machine_id":
		return s.DesiredMachineID
	}
	panic("unknown parity field " + name)
}

// ── the known-divergence whitelist ───────────────────────────────────────────

// knownDivergence is ONE cell of the matrix that does not match today, with the
// reason it does not. `why` is the whole point of the row: a divergence with no
// explanation is a bug nobody has looked at yet, and this file refuses to hold
// one silently — where the code carries no explanation the row says exactly
// that, rather than inventing one.
type knownDivergence struct {
	verb  string
	field string
	why   string
}

var knownDivergences = []knownDivergence{
	// ── 起來 (activate ↔ restart) ──────────────────────────────────────────
	{
		verb: "起來", field: "waking_since",
		why: "正職 activate assigns m.WakingSince = 0.0 (api_members.go, " +
			"HandleActivateMember…) and waking_since is NOT in singleColumnOwnedFields, " +
			"so PutMember carries the clear. 外包 restart deliberately does NOT clear it: " +
			"its 🔴 block says notifyWorkerSpawn stamps a fresh anchor on the re-dispatch, " +
			"and names the residue it leaves (a failed re-dispatch reads 喚醒中 until the " +
			"TTL lapses) as 「the same 正職／外包 divergence T-14 exists to delete」.",
	},
	{
		verb: "起來", field: "stopped_since",
		why: "外包 restart assigns worker.StoppedSince = 0.0 — 「A RESTART STARTS A NEW " +
			"SESSION, SO IT STARTS FROM A CLEAN SHEET」 (T-ed79 #11): the anchor dates the " +
			"session being REPLACED, and the pair (refocus>0 ∧ stopped>0) is read by " +
			"workerHasStateToFlush as 「already collected」. 正職 activate touches neither: " +
			"clearMemberHandoverMarker's own comment states 「activate clears stopping_since " +
			"and waking_since and deliberately clears NEITHER refocus_since nor " +
			"stopped_since」. The two clear sets are complements, not one set of two sizes.",
	},
	{
		verb: "起來", field: "refocus_since",
		why: "Same clear-set complement as stopped_since above: 外包 restart zeroes " +
			"worker.RefocusSince, 正職 activate leaves it and persistMemberWindDownAnchors " +
			"writes back the value it read.",
	},
	{
		verb: "起來", field: "refocus_op",
		why: "The cause travels with its epoch: 外包 restart zeroes worker.RefocusOp " +
			"alongside RefocusSince; 正職 activate writes back what it read.",
	},

	// ── 重新聚焦 (refocus) ─────────────────────────────────────────────────
	{
		verb: "重新聚焦", field: "http_status",
		why: "On a row the owner has already stopped (desired_state=offline) the two " +
			"verbs answer differently BY OWNER RULING 2026-08-30 (「如果我已經到強硬下線的" +
			"狀態下按下 refocus 我只需要在下線後把人帶起來」): 正職 takes the " +
			"aStopWasEverAskedFor branch and answers 200 with a queued 起來, while 外包 " +
			"answers 409 「refocus requires a live worker — this one is stopped (restart it " +
			"first)」. The ruling landed on the STAFF side only; the worker handler still " +
			"carries the pre-ruling refusal.",
	},
	{
		verb: "重新聚焦", field: "restart_after_stop",
		why: "restart_after_stop is a staff-only column in practice: stampRestartIntent " +
			"is called from the member refocus / relocate / update-member handlers and " +
			"from nowhere on the worker side, so 外包 has no way to record 「下線之後把人" +
			"帶起來」 at all. Its 409 above is the same fact seen from the other end.",
	},

	// ── 強制停止 (force-stop) ──────────────────────────────────────────────
	{
		verb: "強制停止", field: "stopping_since",
		why: "外包 force-stop pulls a FUTURE anchor back: `if worker.StoppingSince <= 0.0 " +
			"|| worker.StoppingSince > forcedAt { worker.StoppingSince = forcedAt }`. " +
			"正職 force-stop has only the first arm: `if m.StoppingSince <= 0.0 { " +
			"m.StoppingSince = nowSecs() }`, so a future stamp survives. 🔴 THE CODE " +
			"CARRIES NO EXPLANATION FOR THE EXTRA ARM — no comment on either side mentions " +
			"it, and git blame was not consulted. Reason待補: recorded as observed, " +
			"deliberately NOT rationalised here.",
	},
}

func divergenceIndex() map[[2]string]knownDivergence {
	idx := make(map[[2]string]knownDivergence, len(knownDivergences))
	for _, d := range knownDivergences {
		idx[[2]string{d.verb, d.field}] = d
	}
	return idx
}

// ── the matrix ───────────────────────────────────────────────────────────────

// verbCase is one row of 動詞 × 人口: a start state both populations can be
// seeded into, the two handler calls, and the two LITERAL terminal states.
type verbCase struct {
	verb string
	// note says what the seeded start state is, so a failure reads without
	// scrolling back up to the seeder.
	note string
	// runStaff / runOutsource each seed their own population into the shared
	// start state, call the population's own routes.go handler, and read the row
	// back. They return the terminal state ONLY — no expectation logic.
	runStaff     func(t *testing.T) terminalState
	runOutsource func(t *testing.T) terminalState
	// wantStaff / wantOutsource are hand-transcribed literals. See rule 2 in the
	// file header: nothing below is computed by the code under test.
	wantStaff     terminalState
	wantOutsource terminalState
}

// ── fixtures ─────────────────────────────────────────────────────────────────

const (
	parityMachineA = ServerSelfHost
	parityMachineB = "m-parity-b"
	parityPast     = 1000.0 // seeded anchors sit far in the past
	parityFuture   = 4.0e9  // …and the 強制停止 case needs one far in the future
)

// newParityServer is ONE server that must hold BOTH populations. Whether that
// works at all was the first open question of this package: the staff fixtures
// come from reconcile_test.go (seedOutOfBox roles + a real docs root) and the
// worker fixtures from worker_lifecycle_test.go (a seeded+connected warden and
// an outsource manual). They are combined here rather than run on two servers so
// that the two arms of a row cannot silently diverge on their ENVIRONMENT.
func newParityServer(t *testing.T) *apiServer {
	t.Helper()
	api := newReconcileTestServer(t)
	api.noOutsource = true // no background outsource tick racing the handler calls
	seedLiveWorkerEnv(t, api)
	seedMachine(t, api, parityMachineB)
	return api
}

// seedParityMember plants a staff member in the shared start state. The four
// wind-down anchors go through putTestMember's second write (their sole writer,
// T-55) — a whole-row PutMember would silently drop them and the assertion would
// be made against a state that was never planted.
func seedParityMember(t *testing.T, api *apiServer, id string, mutate func(*Member)) {
	t.Helper()
	m := testAgent(id)
	m.DesiredMachineID = parityMachineA
	m.Model = "claude-sonnet-4-5"
	if mutate != nil {
		mutate(&m)
	}
	putTestMember(t, api, m)
	connectOnlineMachine(t, api, id, parityMachineA)
}

// seedParityWorker plants the outsource twin. newActiveOnlineWorker already
// builds an active + online worker pinned to parityMachineA; the anchors it does
// NOT carry are planted afterwards through seedWorkerAnchors, the same sole
// writer, for the same reason.
func seedParityWorker(t *testing.T, api *apiServer, mutate func(*OutsourceWorker)) string {
	t.Helper()
	id := newActiveOnlineWorker(t, api)
	w, err := api.dal.GetOutsourceWorker(id)
	if err != nil || w == nil {
		t.Fatalf("seed worker: %v", err)
	}
	if mutate != nil {
		mutate(w)
	}
	if err := api.dal.PutOutsourceWorker(*w); err != nil {
		t.Fatalf("put worker: %v", err)
	}
	seedWorkerAnchors(t, api, *w)
	return id
}

func classify(v, at float64) anchorClass {
	switch {
	case v <= 0.0:
		return anchorZero
	case v > at:
		return anchorFuture
	default:
		return anchorPast
	}
}

// memberTerminal / workerTerminal read the row back and bucket its anchors
// against the instant the READ happens — deliberately AFTER the handler has
// returned. Sampling `now` before the call instead makes every anchor the
// handler stamps land in the FUTURE bucket, which is a harness artefact that
// looks exactly like a behaviour change.
func memberTerminal(t *testing.T, api *apiServer, id string, code int) terminalState {
	t.Helper()
	at := nowSecs()
	m, err := api.dal.GetMember(id)
	if err != nil || m == nil {
		t.Fatalf("read back member %s: %v", id, err)
	}
	return terminalState{
		Status:           code,
		DesiredState:     m.DesiredState,
		Stopping:         classify(m.StoppingSince, at),
		Stopped:          classify(m.StoppedSince, at),
		Refocus:          classify(m.RefocusSince, at),
		RefocusOp:        m.RefocusOp,
		Waking:           classify(m.WakingSince, at),
		RestartAfterStop: m.RestartAfterStop,
		DesiredMachineID: m.DesiredMachineID,
	}
}

func workerTerminal(t *testing.T, api *apiServer, id string, code int) terminalState {
	t.Helper()
	at := nowSecs()
	w, err := api.dal.GetOutsourceWorker(id)
	if err != nil || w == nil {
		t.Fatalf("read back worker %s: %v", id, err)
	}
	// A worker row IS a member row; fold it so the two sides are the same type.
	m := memberFromWorker(*w)
	return terminalState{
		Status:           code,
		DesiredState:     m.DesiredState,
		Stopping:         classify(m.StoppingSince, at),
		Stopped:          classify(m.StoppedSince, at),
		Refocus:          classify(m.RefocusSince, at),
		RefocusOp:        m.RefocusOp,
		Waking:           classify(m.WakingSince, at),
		RestartAfterStop: m.RestartAfterStop,
		DesiredMachineID: m.DesiredMachineID,
	}
}

func postMember(t *testing.T, api *apiServer, id, op string, body any,
	h func(http.ResponseWriter, *http.Request, string)) int {
	t.Helper()
	rec := httptest.NewRecorder()
	h(rec, taskReq(t, "POST", "/api/members/"+id+"/"+op, body, wireOwnerID, "owner"), id)
	return rec.Code
}

// ── the cases ────────────────────────────────────────────────────────────────

func parityCases() []verbCase {
	return []verbCase{
		{
			verb: "起來",
			note: "seed: desired offline, all four anchors + waking_since stamped in the past, " +
				"live session. 正職 → POST /activate, 外包 → POST /restart.",
			runStaff: func(t *testing.T) terminalState {
				api := newParityServer(t)
				seedParityMember(t, api, "m-parity-up", func(m *Member) {
					m.DesiredState = DesiredStateOffline
					m.StoppingSince = parityPast
					m.StoppedSince = parityPast
					m.RefocusSince = parityPast
					m.RefocusOp = refocusOpRefocus
					m.WakingSince = parityPast
				})
				code := postMember(t, api, "m-parity-up", "activate", nil,
					api.HandleActivateMemberApiMembersMemberIdActivatePost)
				return memberTerminal(t, api, "m-parity-up", code)
			},
			runOutsource: func(t *testing.T) terminalState {
				api := newParityServer(t)
				id := seedParityWorker(t, api, func(w *OutsourceWorker) {
					w.DesiredState = DesiredStateOffline
					w.StoppingSince = parityPast
					w.StoppedSince = parityPast
					w.RefocusSince = parityPast
					w.RefocusOp = refocusOpRefocus
					w.WakingSince = parityPast
				})
				code := postWorker(t, api, id, "restart", nil,
					api.HandleRestartOutsourceWorkerApiOutsourceWorkersIdRestartPost)
				return workerTerminal(t, api, id, code.Code)
			},
			// 正職 activate: m.StoppingSince = 0.0; m.WakingSince = 0.0;
			// m.DesiredState = DesiredStateOnline; clearRestartIntent(m).
			// stopped_since / refocus_since / refocus_op are written back as read.
			wantStaff: terminalState{
				Status: http.StatusOK, DesiredState: DesiredStateOnline,
				Stopping: anchorZero, Stopped: anchorPast,
				Refocus: anchorPast, RefocusOp: refocusOpRefocus,
				Waking: anchorZero, RestartAfterStop: false,
				DesiredMachineID: parityMachineA,
			},
			// 外包 restart: worker.DesiredState = DesiredStateOnline;
			// worker.RefocusSince = 0.0; worker.RefocusOp = "";
			// worker.StoppingSince = 0.0; worker.StoppedSince = 0.0.
			// waking_since is deliberately left alone (its own 🔴 note).
			wantOutsource: terminalState{
				Status: http.StatusOK, DesiredState: DesiredStateOnline,
				Stopping: anchorZero, Stopped: anchorZero,
				Refocus: anchorZero, RefocusOp: "",
				Waking: anchorPast, RestartAfterStop: false,
				DesiredMachineID: parityMachineA,
			},
		},
		{
			verb: "停止",
			note: "seed: desired online, live session, an OPEN 換手 epoch. 正職 → " +
				"POST /deactivate, 外包 → POST /stop. 🟢 POSITIVE CONTROL ROW: both " +
				"sides stamp the epoch through the SAME pure function (stopEpochAnchor), " +
				"so if THIS row goes red the harness is broken, not the subject.",
			runStaff: func(t *testing.T) terminalState {
				api := newParityServer(t)
				seedParityMember(t, api, "m-parity-stop", func(m *Member) {
					m.RefocusSince = parityPast
					m.RefocusOp = refocusOpRefocus
				})
				code := postMember(t, api, "m-parity-stop", "deactivate", nil,
					api.HandleDeactivateMemberApiMembersMemberIdDeactivatePost)
				return memberTerminal(t, api, "m-parity-stop", code)
			},
			runOutsource: func(t *testing.T) terminalState {
				api := newParityServer(t)
				id := seedParityWorker(t, api, func(w *OutsourceWorker) {
					w.RefocusSince = parityPast
					w.RefocusOp = refocusOpRefocus
				})
				code := postWorker(t, api, id, "stop", nil,
					api.HandleStopOutsourceWorkerApiOutsourceWorkersIdStopPost)
				return workerTerminal(t, api, id, code.Code)
			},
			// 正職: desired offline + clearMemberHandoverMarker + clearRestartIntent
			// + StoppingSince = stopEpochAnchor(...) → now (no forced epoch live).
			wantStaff: terminalState{
				Status: http.StatusOK, DesiredState: DesiredStateOffline,
				Stopping: anchorPast, Stopped: anchorZero,
				Refocus: anchorZero, RefocusOp: "",
				Waking: anchorZero, RestartAfterStop: false,
				DesiredMachineID: parityMachineA,
			},
			// 外包: desired offline; RefocusSince = 0.0; RefocusOp = "";
			// StoppingSince = stopEpochAnchor(memberFromWorker(...)) → the same now.
			wantOutsource: terminalState{
				Status: http.StatusOK, DesiredState: DesiredStateOffline,
				Stopping: anchorPast, Stopped: anchorZero,
				Refocus: anchorZero, RefocusOp: "",
				Waking: anchorZero, RestartAfterStop: false,
				DesiredMachineID: parityMachineA,
			},
		},
		{
			verb: "加速停止",
			note: "seed: an OPEN 下線 epoch (desired offline + stopping_since in the past), " +
				"live session, worker Status=active. Both → POST /accelerated-stop.",
			runStaff: func(t *testing.T) terminalState {
				api := newParityServer(t)
				seedParityMember(t, api, "m-parity-accel", func(m *Member) {
					m.DesiredState = DesiredStateOffline
					m.StoppingSince = parityPast
				})
				code := postMember(t, api, "m-parity-accel", "accelerated-stop", nil,
					api.HandleAcceleratedStopMemberApiMembersMemberIdAcceleratedStopPost)
				return memberTerminal(t, api, "m-parity-accel", code)
			},
			runOutsource: func(t *testing.T) terminalState {
				api := newParityServer(t)
				id := seedParityWorker(t, api, func(w *OutsourceWorker) {
					w.DesiredState = DesiredStateOffline
					w.StoppingSince = parityPast
				})
				code := postWorker(t, api, id, "accelerated-stop", nil,
					api.HandleAcceleratedStopOutsourceWorkerApiOutsourceWorkersIdAcceleratedStopPost)
				return workerTerminal(t, api, id, code.Code)
			},
			// Both: the desired-offline arm re-stamps its anchor from THIS press
			// (m.StoppingSince = now / worker.StoppingSince = nowSecs()) and writes
			// RefocusOp = refocusOpAcceleratedStop. 正職 additionally calls
			// clearRestartIntent — which is a no-op on a row that carries no queued
			// 起來, so the two terminal rows agree here.
			wantStaff: terminalState{
				Status: http.StatusOK, DesiredState: DesiredStateOffline,
				Stopping: anchorPast, Stopped: anchorZero,
				Refocus: anchorZero, RefocusOp: refocusOpAcceleratedStop,
				Waking: anchorZero, RestartAfterStop: false,
				DesiredMachineID: parityMachineA,
			},
			wantOutsource: terminalState{
				Status: http.StatusOK, DesiredState: DesiredStateOffline,
				Stopping: anchorPast, Stopped: anchorZero,
				Refocus: anchorZero, RefocusOp: refocusOpAcceleratedStop,
				Waking: anchorZero, RestartAfterStop: false,
				DesiredMachineID: parityMachineA,
			},
		},
		{
			verb: "強制停止",
			note: "seed: desired online, live session, stopping_since stamped in the " +
				"FUTURE — the one start state that separates the two force-stop bodies.",
			runStaff: func(t *testing.T) terminalState {
				api := newParityServer(t)
				seedParityMember(t, api, "m-parity-force", func(m *Member) {
					m.StoppingSince = parityFuture
				})
				code := postMember(t, api, "m-parity-force", "force-stop", nil,
					api.HandleForceStopMemberApiMembersMemberIdForceStopPost)
				return memberTerminal(t, api, "m-parity-force", code)
			},
			runOutsource: func(t *testing.T) terminalState {
				api := newParityServer(t)
				id := seedParityWorker(t, api, func(w *OutsourceWorker) {
					w.StoppingSince = parityFuture
				})
				code := postWorker(t, api, id, "force-stop", nil,
					api.HandleForceStopOutsourceWorkerApiOutsourceWorkersIdForceStopPost)
				return workerTerminal(t, api, id, code.Code)
			},
			// 正職: `if m.StoppingSince <= 0.0 { m.StoppingSince = nowSecs() }` —
			// the future stamp is NOT touched.
			wantStaff: terminalState{
				Status: http.StatusOK, DesiredState: DesiredStateOffline,
				Stopping: anchorFuture, Stopped: anchorZero,
				Refocus: anchorZero, RefocusOp: "",
				Waking: anchorZero, RestartAfterStop: false,
				DesiredMachineID: parityMachineA,
			},
			// 外包: `if worker.StoppingSince <= 0.0 || worker.StoppingSince > forcedAt
			// { worker.StoppingSince = forcedAt }` — the second arm pulls it back.
			wantOutsource: terminalState{
				Status: http.StatusOK, DesiredState: DesiredStateOffline,
				Stopping: anchorPast, Stopped: anchorZero,
				Refocus: anchorZero, RefocusOp: "",
				Waking: anchorZero, RestartAfterStop: false,
				DesiredMachineID: parityMachineA,
			},
		},
		{
			verb: "重新聚焦",
			note: "seed: the owner has ALREADY stopped this row (desired offline + " +
				"stopping_since in the past) and the session is still live. Both → refocus.",
			runStaff: func(t *testing.T) terminalState {
				api := newParityServer(t)
				seedParityMember(t, api, "m-parity-refocus", func(m *Member) {
					m.DesiredState = DesiredStateOffline
					m.StoppingSince = parityPast
				})
				code := postMember(t, api, "m-parity-refocus", "refocus", nil,
					api.HandleRefocusMemberApiMembersMemberIdRefocusPost)
				return memberTerminal(t, api, "m-parity-refocus", code)
			},
			runOutsource: func(t *testing.T) terminalState {
				api := newParityServer(t)
				id := seedParityWorker(t, api, func(w *OutsourceWorker) {
					w.DesiredState = DesiredStateOffline
					w.StoppingSince = parityPast
				})
				code := postWorker(t, api, id, "refocus", nil,
					api.HandleRefocusOutsourceWorkerApiOutsourceWorkersIdRefocusPost)
				return workerTerminal(t, api, id, code.Code)
			},
			// 正職: !aRefocusStampWouldReachTheAgent && aStopWasEverAskedFor →
			// stampRestartIntent(m) and a 200. The stop keeps its stage and anchors.
			wantStaff: terminalState{
				Status: http.StatusOK, DesiredState: DesiredStateOffline,
				Stopping: anchorPast, Stopped: anchorZero,
				Refocus: anchorZero, RefocusOp: "",
				Waking: anchorZero, RestartAfterStop: true,
				DesiredMachineID: parityMachineA,
			},
			// 外包: `if worker.DesiredState == DesiredStateOffline` → 409, row untouched.
			wantOutsource: terminalState{
				Status: http.StatusConflict, DesiredState: DesiredStateOffline,
				Stopping: anchorPast, Stopped: anchorZero,
				Refocus: anchorZero, RefocusOp: "",
				Waking: anchorZero, RestartAfterStop: false,
				DesiredMachineID: parityMachineA,
			},
		},
		{
			verb: "改機器",
			note: "seed: desired online, live session, pinned to machine A; both are " +
				"relocated to machine B. 🟢 desired_machine_id is a POSITIVE CONTROL: " +
				"both sides write it through the SAME sole writer, SetMemberDesiredMachineID.",
			runStaff: func(t *testing.T) terminalState {
				api := newParityServer(t)
				seedParityMember(t, api, "m-parity-move", nil)
				code := postMember(t, api, "m-parity-move", "relocate",
					map[string]any{"machine_id": parityMachineB},
					api.HandleRelocateMemberApiMembersMemberIdRelocatePost)
				return memberTerminal(t, api, "m-parity-move", code)
			},
			runOutsource: func(t *testing.T) terminalState {
				api := newParityServer(t)
				id := seedParityWorker(t, api, nil)
				code := postWorker(t, api, id, "relocate",
					map[string]any{"machine_id": parityMachineB},
					api.HandleRelocateOutsourceWorkerApiOutsourceWorkersIdRelocatePost)
				return workerTerminal(t, api, id, code.Code)
			},
			// Both arm the wind-down through the SAME predicate
			// (hasUncollectedOnlineOwnerOpState: online ∧ ¬(refocus>0 ∧ stopped>0)) and
			// stamp the epoch through the SAME armRefocusEpoch with op="relocate".
			wantStaff: terminalState{
				Status: http.StatusOK, DesiredState: DesiredStateOnline,
				Stopping: anchorZero, Stopped: anchorZero,
				Refocus: anchorPast, RefocusOp: memberOpRelocate,
				Waking: anchorZero, RestartAfterStop: false,
				DesiredMachineID: parityMachineB,
			},
			wantOutsource: terminalState{
				Status: http.StatusOK, DesiredState: DesiredStateOnline,
				Stopping: anchorZero, Stopped: anchorZero,
				Refocus: anchorPast, RefocusOp: ownerOpRelocate,
				Waking: anchorZero, RestartAfterStop: false,
				DesiredMachineID: parityMachineB,
			},
		},
		{
			verb: "換 model",
			note: "seed: desired online, live session, model=claude-sonnet-4-5; both are " +
				"moved to claude-opus-4-8. 正職 → PATCH /api/members/{id} (the model face " +
				"routes.go points at), 外包 → POST /api/outsource-workers/{id}/model.",
			runStaff: func(t *testing.T) terminalState {
				api := newParityServer(t)
				seedParityMember(t, api, "m-parity-model", nil)
				rec := httptest.NewRecorder()
				api.HandleUpdateMemberApiMembersMemberIdPatch(rec,
					taskReq(t, "PATCH", "/api/members/m-parity-model",
						map[string]any{"model": "claude-opus-4-8"}, wireOwnerID, "owner"),
					"m-parity-model")
				return memberTerminal(t, api, "m-parity-model", rec.Code)
			},
			runOutsource: func(t *testing.T) terminalState {
				api := newParityServer(t)
				id := seedParityWorker(t, api, nil)
				code := postWorker(t, api, id, "model",
					map[string]any{"model": "claude-opus-4-8"},
					api.HandleSetOutsourceWorkerModelApiOutsourceWorkersIdModelPost)
				return workerTerminal(t, api, id, code.Code)
			},
			// Both gate on 「a launch intent actually changed」 and then open the same
			// wind-down with op = "runtime/model" (memberOpModel == ownerOpModel).
			wantStaff: terminalState{
				Status: http.StatusOK, DesiredState: DesiredStateOnline,
				Stopping: anchorZero, Stopped: anchorZero,
				Refocus: anchorPast, RefocusOp: memberOpModel,
				Waking: anchorZero, RestartAfterStop: false,
				DesiredMachineID: parityMachineA,
			},
			wantOutsource: terminalState{
				Status: http.StatusOK, DesiredState: DesiredStateOnline,
				Stopping: anchorZero, Stopped: anchorZero,
				Refocus: anchorPast, RefocusOp: ownerOpModel,
				Waking: anchorZero, RestartAfterStop: false,
				DesiredMachineID: parityMachineA,
			},
		},
	}
}

// ── the matrix test ──────────────────────────────────────────────────────────

// TestVerbPopulationParityMatrix is the guard. For every 動詞 × 人口 cell it
// pins the terminal row against a hand-written literal (which is what kills a
// behaviour mutant on either side), then compares the two literals field by
// field: fields that agree MUST also agree in the two observed rows, and fields
// that disagree MUST carry a knownDivergences row explaining today's difference.
func TestVerbPopulationParityMatrix(t *testing.T) {
	idx := divergenceIndex()
	seen := map[[2]string]bool{}

	for _, c := range parityCases() {
		c := c
		t.Run(c.verb, func(t *testing.T) {
			gotStaff := c.runStaff(t)
			gotOutsource := c.runOutsource(t)

			// ① each side against its own literal — the behaviour mutant killer.
			if gotStaff != c.wantStaff {
				t.Errorf("正職 %s terminal state changed.\n  start: %s\n   want: %+v\n    got: %+v",
					c.verb, c.note, c.wantStaff, gotStaff)
			}
			if gotOutsource != c.wantOutsource {
				t.Errorf("外包 %s terminal state changed.\n  start: %s\n   want: %+v\n    got: %+v",
					c.verb, c.note, c.wantOutsource, gotOutsource)
			}

			// ② the matrix itself.
			for _, f := range parityFields {
				key := [2]string{c.verb, f}
				want := c.wantStaff.field(f)
				other := c.wantOutsource.field(f)
				d, listed := idx[key]
				if listed {
					seen[key] = true
				}
				switch {
				case want != other && !listed:
					t.Errorf("UNDOCUMENTED DIVERGENCE %s|%s: 正職 ends %v, 外包 ends %v.\n"+
						"  Either the two handlers were meant to converge and one of them "+
						"regressed, or this is a real difference that must be added to "+
						"knownDivergences with a sentence saying why it is different today.",
						c.verb, f, want, other)
				case want == other && listed:
					t.Errorf("STALE WHITELIST ROW %s|%s: both populations now end %v, so this "+
						"divergence is CLOSED. Delete the knownDivergences row (it still says: %s)",
						c.verb, f, want, d.why)
				case want == other:
					// The parity claim, asserted on the OBSERVED rows and not only on
					// the two literals — belt and braces, and it is the assertion that
					// still fires if both literals are edited in lockstep.
					if gs, go_ := gotStaff.field(f), gotOutsource.field(f); gs != go_ {
						t.Errorf("PARITY BROKEN %s|%s: 正職 ends %v, 外包 ends %v. These two "+
							"are meant to be the same and nothing in knownDivergences allows "+
							"them to differ.", c.verb, f, gs, go_)
					}
				}
			}
		})
	}

	// Every whitelist row must belong to a cell this table actually exercises —
	// otherwise a row could be kept alive by a verb that no longer runs.
	var orphans []string
	for k := range idx {
		if !seen[k] {
			orphans = append(orphans, fmt.Sprintf("%s|%s", k[0], k[1]))
		}
	}
	sort.Strings(orphans)
	if len(orphans) > 0 {
		t.Errorf("knownDivergences rows that no matrix cell covers: %v — a whitelist row "+
			"whose verb is not run is documentation, not a guard", orphans)
	}
}

// TestVerbPopulationParityWhitelistIsExplained keeps the whitelist honest: a row
// with no `why` is a divergence nobody has looked at, and the whole value of the
// table is that each surviving difference carries its reason.
func TestVerbPopulationParityWhitelistIsExplained(t *testing.T) {
	for _, d := range knownDivergences {
		if d.verb == "" || d.field == "" {
			t.Errorf("knownDivergences row with an empty verb/field: %+v", d)
		}
		if len(d.why) < 40 {
			t.Errorf("knownDivergences %s|%s has no real explanation (%q). A divergence "+
				"without a reason is a bug that has not been looked at yet; if the code "+
				"carries no explanation, say exactly that.", d.verb, d.field, d.why)
		}
	}
}

// TestAcceleratedStopWorkerHasAnExtraLifecycleGate is the one divergence the
// matrix above CANNOT express as a shared start state: 加速停止 on the worker
// side additionally requires `worker.Status != WorkerStatusActive` to be false,
// and a staff member has no Status column to be non-active in. It is asserted
// one-sidedly rather than dropped, because "no shared start state" is not the
// same as "not a divergence".
func TestAcceleratedStopWorkerHasAnExtraLifecycleGate(t *testing.T) {
	api := newParityServer(t)
	id := seedParityWorker(t, api, func(w *OutsourceWorker) {
		w.DesiredState = DesiredStateOffline
		w.StoppingSince = parityPast
		w.Status = WorkerStatusAssigned // online, open epoch — only Status refuses
	})
	rec := postWorker(t, api, id, "accelerated-stop", nil,
		api.HandleAcceleratedStopOutsourceWorkerApiOutsourceWorkersIdAcceleratedStopPost)
	if rec.Code != http.StatusConflict {
		t.Fatalf("加速停止 on a non-active but ONLINE worker with an open stop epoch = %d %s, "+
			"want 409. The worker handler gates on `worker.Status != WorkerStatusActive || "+
			"!s.hub.IsOnline(...)`; the staff twin gates on liveness ALONE, so this arm is "+
			"外包-only and has no member analogue.", rec.Code, rec.Body.String())
	}
	// NEGATIVE CONTROL: the same worker with Status=active is admitted, so the
	// 409 above is the Status gate and not some other refusal on the way in.
	api2 := newParityServer(t)
	id2 := seedParityWorker(t, api2, func(w *OutsourceWorker) {
		w.DesiredState = DesiredStateOffline
		w.StoppingSince = parityPast
	})
	if rec := postWorker(t, api2, id2, "accelerated-stop", nil,
		api2.HandleAcceleratedStopOutsourceWorkerApiOutsourceWorkersIdAcceleratedStopPost); rec.Code != http.StatusOK {
		t.Fatalf("control: the SAME state with Status=active must be admitted, got %d %s",
			rec.Code, rec.Body.String())
	}
}
