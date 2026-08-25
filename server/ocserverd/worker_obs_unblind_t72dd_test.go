package main

import (
	"fmt"
	"sort"
	"strings"
	"testing"
)

// T-72dd — THE BLAST-RADIUS MEASUREMENT for un-blinding reconcileWorkerLiveness.
//
// reconcileWorkerLiveness already runs workers through the STAFF FSM
// (reconcileDecide), but it hands it a MAIMED observation: Desired is hard-wired
// to online, and RefocusSince / RefocusOp / AgentStopped — the exact three
// fields decideUp's recycle arm reads — are never filled. So the shared收口 that
// T-ed79 gave staff is unreachable for an outsource worker: the FSM is asked the
// question with the answer removed.
//
// Un-blinding it lets the shared FSM make decisions for workers it does not make
// today — not only the recycle arm, but the whole desired=offline branch
// (decideDown). This table is the evidence for that reach: it enumerates the
// worker state combinations that reach the FSM and records, cell by cell, what
// the BLIND observation decides against what the FULL one decides.
//
// 🔴 The table is deliberately WIDER than what the outsource tick can actually
// reach today (outsource_sched.go gates its two call sites), so the cells that
// change are visible even when the caller currently filters them out. The
// reachability of each changed cell is argued in the assertion below.
type unblindCell struct {
	name     string
	w        OutsourceWorker
	online   bool
	st       reconcileState
	blindCmd string
	fullCmd  string
	blindWhy string
	fullWhy  string
}

// blindWorkerObservation is TODAY's observation, transcribed from
// worker_spawn.go verbatim. It is here so the "before" column cannot silently
// drift with the code it is the baseline for.
func blindWorkerObservation(w OutsourceWorker, online bool) memberObservation {
	return memberObservation{
		MemberID:     w.ID,
		Desired:      DesiredStateOnline,
		Online:       online,
		LastOpKind:   canonicalWorkerLastOp(w.LastOp),
		LastOpReason: w.LastOpReason,
	}
}

func TestWorkerObservationUnblind_DecisionTable_T72dd(t *testing.T) {
	api := newTasksTestServer(t)
	cfg := api.reconcileConfigLive()
	const now = 1_000_000.0

	// Two FSM states, because the arms that dispatch are paced by st:
	//   fresh   — nothing dispatched yet (the first tick on this worker)
	//   started — a START was dispatched long ago and never produced presence
	fresh := newReconcileState()
	started := newReconcileState()
	started.LastCommand = reconcileCmdStart
	started.LastCommandAt = now - 10_000
	started.OfflineSince = now - 10_000

	base := func(mut func(*OutsourceWorker)) OutsourceWorker {
		w := OutsourceWorker{ID: "ow-tbl", DesiredState: DesiredStateOnline}
		mut(&w)
		return w
	}
	// An epoch far enough back that ANY clocked grace has lapsed.
	old := now - 10_000

	type spec struct {
		name   string
		w      OutsourceWorker
		online bool
		st     reconcileState
	}
	specs := []spec{}
	for _, desired := range []string{DesiredStateOnline, DesiredStateOffline} {
		for _, online := range []bool{true, false} {
			for _, ref := range []struct {
				tag   string
				since float64
				op    string
			}{
				{"noEpoch", 0, ""},
				{"refocus(soft,noClock)", old, refocusOpRefocus},
				{"ctxHigh(final,clocked,lapsed)", old, refocusOpContextHigh},
				{"acceleratedStop(final,clocked,lapsed)", old, refocusOpAcceleratedStop},
			} {
				for _, stopped := range []float64{0, old} {
					for _, stopping := range []float64{0, old} {
						for _, lastOp := range []struct {
							tag    string
							op     string
							reason string
						}{
							{"plainLastOp", "", ""},
							{"clobberedStart", reconcileCmdStart, spawnClobberReasonPrefix + ": ghost"},
						} {
							for stTag, st := range map[string]reconcileState{"fresh": fresh, "startedLongAgo": started} {
								d, r, sp, so, lo := desired, ref, stopped, stopping, lastOp
								specs = append(specs, spec{
									name: fmt.Sprintf("desired=%s/online=%v/%s/dumpDone=%v/stopping=%v/%s/st=%s",
										d, online, r.tag, sp != 0, so != 0, lo.tag, stTag),
									w: base(func(w *OutsourceWorker) {
										w.DesiredState = d
										w.RefocusSince = r.since
										w.RefocusOp = r.op
										w.StoppedSince = sp
										w.StoppingSince = so
										w.LastOp = lo.op
										w.LastOpReason = lo.reason
									}),
									online: online,
									st:     st,
								})
							}
						}
					}
				}
			}
		}
	}
	sort.Slice(specs, func(i, j int) bool { return specs[i].name < specs[j].name })

	var changed []unblindCell
	var b strings.Builder
	fmt.Fprintf(&b, "\n%-4s %-96s %-9s -> %-9s\n", "#", "CELL", "BLIND", "FULL")
	for i, s := range specs {
		blind := blindWorkerObservation(s.w, s.online)
		full := workerObservation(s.w, s.online)
		bd := reconcileDecide(blind, s.st, cfg, now)
		fd := reconcileDecide(full, s.st, cfg, now)
		mark := "  "
		if bd.Command != fd.Command {
			mark = "**"
			changed = append(changed, unblindCell{
				name: s.name, w: s.w, online: s.online, st: s.st,
				blindCmd: bd.Command, fullCmd: fd.Command,
				blindWhy: bd.Reason, fullWhy: fd.Reason,
			})
		}
		fmt.Fprintf(&b, "%s%-2d %-96s %-9s -> %-9s | %s\n", mark, i, s.name, bd.Command, fd.Command, fd.Reason)
	}
	t.Log(b.String())

	// ── the cell-by-cell verdict ────────────────────────────────────────────
	var lines []string
	for _, c := range changed {
		lines = append(lines, fmt.Sprintf("  %s: %s -> %s (%q)", c.name, c.blindCmd, c.fullCmd, c.fullWhy))
	}
	sort.Strings(lines)
	t.Logf("CELLS WHOSE COMMAND CHANGED (%d of %d):\n%s", len(changed), len(specs), strings.Join(lines, "\n"))

	// ── every changed cell must belong to one of THREE justified families ────
	//
	// This is the cell-by-cell verdict, written as an assertion so a later edit
	// that widens the blast radius fails here instead of being discovered in
	// production. A cell that fits none of the three is an UNARGUED change.
	family := func(c unblindCell) string {
		switch {
		// (1) THE COLLECTOR ARRIVES. An online worker inside a wind-down epoch
		// that is finished — the agent filed its dump-done, or a CLOCKED cause
		// (the two 加速停止 arms, and only those) ran out the grace it was told
		// about. Staff have been收口ed this way since T-ed79; the worker was
		// answered "online: converged" because the FSM could not see the epoch,
		// so the handover waited forever for a collector that did not exist.
		// This is the bug.
		case c.blindCmd == reconcileCmdNone && c.fullCmd == reconcileCmdStop &&
			c.w.DesiredState == DesiredStateOnline && c.online && c.w.RefocusSince > 0:
			return "1-recycle-collector-now-reachable"
		// (2) 停止 IS HONOURED. The blind observation SWORE desired=online, so an
		// owner-stopped, already-offline worker was answered with a START — the
		// FSM was being asked to revive a worker the owner held down. It reads
		// "offline: converged" now. Nothing is killed; a wrong START stops being
		// issued.
		case c.blindCmd == reconcileCmdStart && c.fullCmd == reconcileCmdNone &&
			c.w.DesiredState == DesiredStateOffline && !c.online:
			return "2-held-down-no-longer-revived"
		// (3) NO GHOST REAP ON A WORKER THAT IS SUPPOSED TO BE DOWN. Same lie,
		// other arm: a stale last_op start+session_already_exists made decideUp
		// order a zombie-takeover STOP for an offline-desired, offline worker.
		// decideDown converges it instead. A STOP is REMOVED here, never added.
		case c.blindCmd == reconcileCmdStop && c.fullCmd == reconcileCmdNone &&
			c.w.DesiredState == DesiredStateOffline && !c.online:
			return "3-no-takeover-on-a-stopped-worker"
		}
		return ""
	}
	byFamily := map[string]int{}
	for _, c := range changed {
		f := family(c)
		if f == "" {
			t.Fatalf("UNARGUED behaviour change — this cell fits none of the three "+
				"justified families:\n  %s\n  %s -> %s\n  blind: %s\n  full:  %s",
				c.name, c.blindCmd, c.fullCmd, c.blindWhy, c.fullWhy)
		}
		byFamily[f]++
	}
	t.Logf("CHANGED CELLS BY FAMILY: %v", byFamily)

	// 🔴 THE SAFETY ASSERTION, and the reason this test exists rather than a
	// read-through: NO cell may go from "dispatch nothing" to a KILL for a
	// worker whose desired_state is ONLINE and that has no wind-down state at
	// all. That is the shape "un-blinding stopped a living agent" would take.
	for _, c := range changed {
		if c.fullCmd != reconcileCmdStop {
			continue
		}
		noWindDown := c.w.RefocusSince == 0 && c.w.StoppingSince == 0 && c.w.StoppedSince == 0
		if c.w.DesiredState == DesiredStateOnline && noWindDown {
			t.Fatalf("un-blinding turned a healthy, wind-down-free worker into a KILL — "+
				"this is exactly the 'it砍到活著的 agent' case:\n  %s\n  %s", c.name, c.fullWhy)
		}
	}
}
