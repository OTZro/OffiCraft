package main

import (
	"fmt"
	"testing"
)

// T-14 項目 6, step 1 — THE PARITY TEST FOR lifecycleTickDriverFor.
//
// 🔴 WHAT THIS FILE IS DEFENDING, said once, plainly.
//
// The merged lifecycle tick (lifecycle_tick.go) runs two halves over two row
// populations: runReconcileTick over dal.ListMembers, runOutsourceTick over
// dal.ListOutsourceWorkers. The only reason a single row cannot enter both
// halves in one tick is a SQL string — ListMembers is `FROM member WHERE kind
// != 'outsource'` (dal.go) — and an outsource worker IS a member row
// (PutOutsourceWorker → PutMember; there is no second table). Lift that clause
// and one ACTIVE desired-online worker row was measured taking a `start` from
// enqueueWardenFrame AND a `start` from notifyWorkerSpawn in the SAME tick.
//
// lifecycleTickDriverFor re-sites that split out of the query and into a named
// total predicate. This test is what makes the re-siting worth doing: it turns
// "exactly one half owns every row" into a universal sentence that is checked
// CELL BY CELL over the whole (Kind × RosterStatus × DesiredState × Activated)
// space, and that names the offending cell — and both halves by their function
// names — when it breaks.
//
// ⚠️ THE TWO SIDES MUST STAY INDEPENDENTLY WRITTEN. The expectation below
// (driverExpectedByPopulation) is deliberately transcribed from the DATA
// SOURCES — which reader hands the row to which half — using raw string
// literals rather than the Kind* constants and without calling
// lifecycleTickDriverFor. If a future edit makes the expectation call the thing
// it is checking, this test becomes a tautology that no mutant can redden, and
// the only wall between the two FSMs goes back to being invisible.

// lifecycleDriverCell is ONE point of the enumerated space. Every field is an
// axis the two halves are known to read: Kind picks the population, RosterStatus
// and Activated are what workerStatusFromMember folds into the worker
// vocabulary (removed⇒released, activated>0⇒active, else assigned), and
// DesiredState is the owner's hold-down.
type lifecycleDriverCell struct {
	kind      string
	roster    string
	activated float64
	desired   string
}

func (c lifecycleDriverCell) String() string {
	act := "activated=0"
	if c.activated > 0 {
		act = "activated>0"
	}
	return fmt.Sprintf("row(kind=%s, roster=%s, %s, desired=%s)",
		c.kind, c.roster, act, c.desired)
}

func (c lifecycleDriverCell) member() Member {
	return Member{
		ID:           "m-driver-parity",
		Name:         "driver-parity",
		Kind:         c.kind,
		RosterStatus: c.roster,
		ActivatedTS:  c.activated,
		DesiredState: c.desired,
	}
}

// driverExpectedByPopulation is the SECOND, INDEPENDENT statement of the split
// — read off the two snapshot readers rather than off lifecycleTickDriverFor:
//
//   - dal.ListMembers  → `FROM member WHERE kind != 'outsource'` → every row
//     whose kind is not the literal "outsource" reaches runReconcileTick, and
//     no row whose kind IS "outsource" ever does.
//   - dal.ListOutsourceWorkers → the kind='outsource' rows, ALL of them:
//     assigned, active and released, held down or not. Rows this half then
//     declines are declined inside its own switch, never handed onward.
//
// Kind is the only axis either reader consults, which is itself the finding
// worth writing down: the other three axes are enumerated because they are what
// the two halves branch on AFTER admission, and this test's job is to pin that
// none of them can ever change WHO admits the row.
func driverExpectedByPopulation(kind string) lifecycleDriver {
	if kind == "outsource" {
		return "runOutsourceTick"
	}
	return "runReconcileTick"
}

func lifecycleDriverCells() []lifecycleDriverCell {
	kinds := []string{KindAssistant, KindWarden, KindOutsource}
	rosters := []string{RosterStatusActive, RosterStatusRemoved}
	desireds := []string{DesiredStateOnline, DesiredStateOffline, DesiredStateUninstall}
	activateds := []float64{0.0, 1_700_000_000.0}

	cells := make([]lifecycleDriverCell, 0, len(kinds)*len(rosters)*len(desireds)*len(activateds))
	for _, k := range kinds {
		for _, r := range rosters {
			for _, d := range desireds {
				for _, a := range activateds {
					cells = append(cells, lifecycleDriverCell{
						kind: k, roster: r, activated: a, desired: d,
					})
				}
			}
		}
	}
	return cells
}

// TestLifecycleTickDriver_EveryRowHasExactlyOneDriver is the universal sentence:
// over the whole enumerated space, exactly one half claims each row.
//
// The two claims are TRANSCRIPTIONS OF THE GUARDS AS THEY ARE WRITTEN at the
// head of each producer's row loop — reconcile.go's
// `lifecycleTickDriverFor(m) != driverReconcile { continue }` and
// outsource_sched.go's `lifecycleTickDriverFor(memberFromWorker(w)) !=
// driverOutsource { continue }`. If either guard is reworded in production, the
// transcription here has to move with it or this test stops describing the code.
func TestLifecycleTickDriver_EveryRowHasExactlyOneDriver(t *testing.T) {
	cells := lifecycleDriverCells()
	if len(cells) != 36 {
		t.Fatalf("the enumerated space is %d cells, expected 36 "+
			"(3 kinds × 2 roster states × 3 desired states × 2 activated states) — "+
			"an axis was added or dropped without this count moving, which means "+
			"the parity claim below no longer covers what it says it covers", len(cells))
	}

	for _, c := range cells {
		m := c.member()
		got := lifecycleTickDriverFor(m)

		// The guard runReconcileTick asks, verbatim.
		claimedByReconcile := got == driverReconcile
		// The guard runOutsourceTick asks, verbatim.
		claimedByOutsource := got == driverOutsource

		switch {
		case claimedByReconcile && claimedByOutsource:
			t.Errorf("%s is claimed by BOTH runReconcileTick and runOutsourceTick — "+
				"exactly one half must drive a row (lifecycleTickDriverFor, "+
				"lifecycle_roster.go). Two halves on one row is the double-dispatch "+
				"the `WHERE kind != 'outsource'` clause in dal.ListMembers is "+
				"currently the only thing preventing.", c)
		case !claimedByReconcile && !claimedByOutsource:
			t.Errorf("%s is claimed by NEITHER runReconcileTick nor runOutsourceTick "+
				"(lifecycleTickDriverFor returned %q) — exactly one half must drive a "+
				"row. A row no half claims is never started, never stopped and never "+
				"collected, and nothing else in the tick would say so.",
				c, string(got))
		}

		if want := driverExpectedByPopulation(c.kind); got != want {
			t.Errorf("%s is claimed by %s, but %s must drive it — "+
				"lifecycleTickDriverFor disagrees with the population that actually "+
				"reaches each half (dal.ListMembers is `WHERE kind != 'outsource'`; "+
				"dal.ListOutsourceWorkers is every kind='outsource' row). This step is "+
				"a pure re-siting of that split and is not permitted to move a row to "+
				"the other half.",
				c, string(got), string(want))
		}
	}
}

// TestLifecycleTickDriver_IsTotalOverTheKindVocabulary pins the OTHER way this
// can rot: a fourth member kind added to domain.go's closed set. The switch
// above enumerates the three kinds that exist; this one asserts the driver
// answers for the vocabulary itself, so a new kind cannot silently inherit
// whichever half the fall-through happens to name without somebody deciding.
func TestLifecycleTickDriver_IsTotalOverTheKindVocabulary(t *testing.T) {
	vocabulary := []string{KindAssistant, KindWarden, KindOutsource}
	for _, k := range vocabulary {
		got := lifecycleTickDriverFor(Member{ID: "m-total", Kind: k})
		if got != driverReconcile && got != driverOutsource {
			t.Errorf("lifecycleTickDriverFor(kind=%s) = %q, which is neither "+
				"runReconcileTick nor runOutsourceTick — the driver must be TOTAL: "+
				"every row has exactly one half, and driverNone is a failure value, "+
				"not an answer.", k, string(got))
		}
	}
	// driverNone must never be a driver's name. Referenced here so the constant
	// cannot be quietly deleted or aliased onto a real half.
	if driverNone == driverReconcile || driverNone == driverOutsource {
		t.Fatalf("driverNone (%q) collides with a real half — the NEITHER arm of the "+
			"parity test above would then be unreachable and a non-exhaustive driver "+
			"would read as a legitimate claim.", string(driverNone))
	}
}
