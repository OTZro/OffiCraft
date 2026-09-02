package main

// single_column_writes_t14_test.go — T-14 項目 6, ONE invariant, for a GROWING
// set of columns:
//
//	a column that has been migrated to a single-column writer must never
//	reappear in PutMember's ON CONFLICT DO UPDATE SET list.
//
// The migration is only half a fix. Writing SetMemberX / AddMemberX and leaving
// the column in the whole-row SET list changes nothing at all: every stale
// snapshot still lands on it, and the suite stays green either way — which is
// exactly how the handover claim nearly shipped broken (T-ffdf). Until this
// file existed, the ONLY thing holding the second half in place was a comment
// in dal.go, and comments do not fail builds.
//
// Data-driven ON PURPOSE, and the shape matters: the guard asserts the DATABASE
// BEHAVIOUR (stamp through the sole writer, run a stale whole-row upsert over
// it, read back), never the TEXT of the SQL. A test that string-matched the
// statement would go red on a reflow and stay green on a semantic change — the
// wrong way round. Migrating the next column is one entry in the table below.

import "testing"

// singleColumnOwnedFields is the registry the guard iterates. Add the entry in
// the SAME commit that removes a column from PutMember's SET list.
var singleColumnOwnedFields = []struct {
	// column names the database column, and is what the failure message must
	// print — a reader who breaks this needs to be told WHICH column, not that
	// "a member upsert regressed".
	column string
	// writer names the sole writer, so the message says where the fix lives.
	writer string
	// stamp moves the column off its zero through that sole writer.
	stamp func(*DAL, string) error
	// want is the value stamp must have left behind. `any` rather than a
	// number because the registry outgrew the numeric anchors it started on —
	// the owner-intent columns (T-55) are strings, and a string column is
	// clobbered by a stale snapshot exactly the same way. Compared with !=, so
	// every entry must carry a COMPARABLE dynamic type.
	want any
	// read pulls the column out of a round-tripped row.
	read func(Member) any
	// stale zeroes the column on a snapshot, imitating every whole-row writer
	// that read the row before the stamp landed.
	stale func(*Member)
}{
	{
		column: "banked_cost",
		writer: "AddMemberBankedCost",
		stamp:  func(d *DAL, id string) error { return d.AddMemberBankedCost(id, 42.5) },
		want:   42.5,
		read:   func(m Member) any { return m.BankedCost },
		stale:  func(m *Member) { m.BankedCost = 0 },
	},
	{
		column: "handover_noticed_ts",
		writer: "SetMemberHandoverNoticedTS",
		stamp:  func(d *DAL, id string) error { return d.SetMemberHandoverNoticedTS(id, 4242) },
		want:   float64(4242),
		read:   func(m Member) any { return m.HandoverNoticedTS },
		stale:  func(m *Member) { m.HandoverNoticedTS = 0 },
	},
	{
		column: "agent_iat_floor",
		writer: "SetMemberAgentIatFloor",
		stamp:  func(d *DAL, id string) error { return d.SetMemberAgentIatFloor(id, 1700) },
		want:   float64(1700),
		read:   func(m Member) any { return m.AgentIatFloor },
		stale:  func(m *Member) { m.AgentIatFloor = 0 },
	},
	{
		column: "desired_machine_id",
		writer: "SetMemberDesiredMachineID",
		stamp: func(d *DAL, id string) error {
			return d.SetMemberDesiredMachineID(id, "m-relocated-here")
		},
		want:  "m-relocated-here",
		read:  func(m Member) any { return m.DesiredMachineID },
		stale: func(m *Member) { m.DesiredMachineID = "" },
	},
	{
		column: "model",
		writer: "SetMemberModel",
		stamp:  func(d *DAL, id string) error { return d.SetMemberModel(id, "opus") },
		want:   "opus",
		read:   func(m Member) any { return m.Model },
		stale:  func(m *Member) { m.Model = "" },
	},
	{
		column: "runtime",
		writer: "SetMemberRuntime",
		stamp:  func(d *DAL, id string) error { return d.SetMemberRuntime(id, RuntimeCodex) },
		want:   RuntimeCodex,
		read:   func(m Member) any { return m.Runtime },
		// "" is the durable "nobody has picked yet", which is exactly the value
		// a snapshot taken before the owner's save carries.
		stale: func(m *Member) { m.Runtime = "" },
	},
	{
		column: "effort",
		writer: "SetMemberEffort",
		stamp:  func(d *DAL, id string) error { return d.SetMemberEffort(id, "max") },
		want:   "max",
		read:   func(m Member) any { return m.Effort },
		stale:  func(m *Member) { m.Effort = "" },
	},
}

// TestPutMemberNeverOverwritesSingleColumnOwnedFields is the automatic guard.
//
// Mutant for any row: put `<column> = excluded.<column>` back into PutMember's
// DO UPDATE SET and this test goes red NAMING that column.
func TestPutMemberNeverOverwritesSingleColumnOwnedFields(t *testing.T) {
	// A deleted row is the one mutation the loop below cannot see: the guard
	// would pass by iterating less. Bump this deliberately when the registry
	// grows.
	if len(singleColumnOwnedFields) != 7 {
		t.Fatalf("singleColumnOwnedFields has %d entries, expected 7. Adding a "+
			"column? Bump this number. REMOVING one? That means a column went "+
			"BACK into PutMember's DO UPDATE SET — say why in the commit",
			len(singleColumnOwnedFields))
	}

	for _, f := range singleColumnOwnedFields {
		t.Run(f.column, func(t *testing.T) {
			d := newTestDAL(t)
			id := "m-" + f.column
			seed := fullMember(id)
			f.stale(&seed) // born at the zero the column's INSERT carries
			if err := d.PutMember(seed); err != nil {
				t.Fatalf("seed member: %v", err)
			}
			if err := f.stamp(d, id); err != nil {
				t.Fatalf("%s: %v", f.writer, err)
			}

			// The whole-row writer: a snapshot taken BEFORE the stamp, which is
			// every snapshot, since nothing but the sole writer moves the value.
			stale := seed
			f.stale(&stale)
			stale.Name = "renamed by an unrelated write"
			if err := d.PutMember(stale); err != nil {
				t.Fatalf("whole-row upsert: %v", err)
			}

			after, err := d.GetMember(id)
			if err != nil || after == nil {
				t.Fatalf("read back: %v %v", after, err)
			}
			if got := f.read(*after); got != f.want {
				// The TYPES are printed because `want`/`read` are `any` and a
				// mismatched pair compares unequal on type alone: float64(0)
				// against int64(0) prints "0 → 0" and reads as a database
				// regression when it is really a wrong registry entry. Comparing
				// as `any` is STRICTER than the float64 it replaced, never
				// looser — verified by seeding both of those pairs.
				t.Fatalf("member.%s was clobbered by a whole-row upsert: %#v (%T) → %#v (%T).\n"+
					"%s is the SOLE writer of this column; it must stay OUT of "+
					"PutMember's ON CONFLICT DO UPDATE SET list (dal.go). If you "+
					"just added `%s = excluded.%s` back, that is the line to remove.",
					f.column, f.want, f.want, got, got, f.writer, f.column, f.column)
			}
			// Positive control: the upsert really ran, so the assertion above
			// is not passing because nothing was written at all.
			if after.Name != "renamed by an unrelated write" {
				t.Fatalf("the upsert itself must have landed; got name %q", after.Name)
			}
		})
	}
}

// TestAddMemberBankedCostAccumulatesAndIsRowScoped covers what the registry
// entry above cannot: the writer's own semantics.
//
// It ADDS rather than sets, in SQL, because the banking edges overlap — an SSE
// last-disconnect can race a kill funnel on the same actor. A Go-side
// read-modify-write would let one edge's spend vanish, and vanishing spend is
// the failure nobody reports (the number just looks low).
func TestAddMemberBankedCostAccumulatesAndIsRowScoped(t *testing.T) {
	d := newTestDAL(t)
	seed := fullMember("m-1")
	seed.BankedCost = 0
	if err := d.PutMember(seed); err != nil {
		t.Fatalf("seed: %v", err)
	}
	neighbour := fullMember("m-2")
	neighbour.BankedCost = 9
	if err := d.PutMember(neighbour); err != nil {
		t.Fatalf("seed neighbour: %v", err)
	}

	for _, delta := range []float64{1.25, 2.5} {
		if err := d.AddMemberBankedCost("m-1", delta); err != nil {
			t.Fatalf("add %v: %v", delta, err)
		}
	}
	got, err := d.GetMember("m-1")
	if err != nil || got == nil {
		t.Fatalf("read back: %v %v", got, err)
	}
	if got.BankedCost != 3.75 {
		t.Fatalf("banked_cost = %v, want 3.75 — the writer must ACCUMULATE, "+
			"not overwrite", got.BankedCost)
	}
	if other, _ := d.GetMember("m-2"); other == nil || other.BankedCost != 9 {
		t.Fatalf("a bank must touch ONLY its own row, m-2 = %+v", other)
	}

	// A missing row is a clean no-op, like every other single-column setter.
	if err := d.AddMemberBankedCost("m-nope", 1); err != nil {
		t.Fatalf("banking an unknown id must be a silent no-op, got %v", err)
	}
}
