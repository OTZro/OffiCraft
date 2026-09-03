package main

// dal_member_patch_t63_test.go — the guards for the member table's PATCH door.
//
// Three properties are load-bearing here and each one has a mutant that turns a
// named assertion red:
//
//  1. A column you did not name is not written. Delete a field from the caller
//     and the column must keep its value; add one to the statement and the
//     round-trip below stops matching.
//  2. forced_stop_at only moves forward, on BOTH paths that write it. Clear
//     forwardOnly on its constructor and the table test names the column.
//  3. The property table is the only place a column is classified. Clear
//     insertOnly on any column and the classification test names it.
//
// Property 3 is the one that decides whether "adding a monotone column is one
// line" is true or merely intended, so it asserts the whole classification, not
// a spot check.

import (
	"reflect"
	"testing"
)

// TestPatchMemberWritesOnlyTheNamedColumns is the reason this door exists: a
// writer that names one column must not carry anything else, whatever it happens
// to be holding.
//
// The assertion compares the WHOLE row before and after, with the one named
// column patched into the expectation. A column that leaks into the statement
// fails here no matter which column it is — the alternative, listing the columns
// we thought to check, is exactly the fixture shape that stops noticing.
func TestPatchMemberWritesOnlyTheNamedColumns(t *testing.T) {
	d := newTestDAL(t)
	seed := fullMember("m-1")
	seed.ForcedStopAt = 100
	seed.AgentIatFloor = 50
	seed.HandoverNoticedTS = 25
	if err := d.PutMember(seed); err != nil {
		t.Fatalf("seed: %v", err)
	}
	before, err := d.GetMember("m-1")
	if err != nil || before == nil {
		t.Fatalf("read seed back: %v %v", before, err)
	}
	// The seed really landed — without this, every assertion below would also
	// pass on a row that was never written the way we think it was.
	if before.ForcedStopAt != 100 || before.AgentIatFloor != 50 || before.BankedCost != seed.BankedCost {
		t.Fatalf("seed did not land: forced_stop_at=%v agent_iat_floor=%v banked_cost=%v",
			before.ForcedStopAt, before.AgentIatFloor, before.BankedCost)
	}

	if err := d.PatchMember("m-1", mfName("renamed")); err != nil {
		t.Fatalf("patch: %v", err)
	}
	after, err := d.GetMember("m-1")
	if err != nil || after == nil {
		t.Fatalf("read back: %v %v", after, err)
	}

	want := *before
	want.Name = "renamed"
	if !reflect.DeepEqual(memberComparable(want), memberComparable(*after)) {
		t.Fatalf("a one-column patch changed more than the column it named.\n"+
			"before: %#v\nafter:  %#v\n"+
			"PatchMember must put ONLY the named fields into the UPDATE statement.",
			memberComparable(want), memberComparable(*after))
	}
}

// TestPatchMemberKeepsTwoConcurrentSingleColumnWritersApart is the failure the
// whole-row door produced and this one cannot: two writers holding snapshots
// taken at the same moment, each changing a different column.
func TestPatchMemberKeepsTwoConcurrentSingleColumnWritersApart(t *testing.T) {
	d := newTestDAL(t)
	if err := d.PutMember(fullMember("m-1")); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := d.PatchMember("m-1", mfName("writer A")); err != nil {
		t.Fatalf("writer A: %v", err)
	}
	if err := d.PatchMember("m-1", mfDesiredState("offline")); err != nil {
		t.Fatalf("writer B: %v", err)
	}
	got, err := d.GetMember("m-1")
	if err != nil || got == nil {
		t.Fatalf("read back: %v %v", got, err)
	}
	if got.Name != "writer A" {
		t.Fatalf("writer B clobbered writer A's column: name = %q, want %q",
			got.Name, "writer A")
	}
	if got.DesiredState != "offline" {
		t.Fatalf("writer B's own column did not land: desired_state = %q", got.DesiredState)
	}
}

// TestPatchMemberUnknownIDIsACleanNoOp pins the answer for an id that names no
// row, because the second half of this migration turns 52 whole-row callers into
// patch callers and one of them will eventually run against a member dismissed
// between its read and its write.
//
// The answer is: no error, no row created. It matches every single-column setter
// beside it, and it is the reason PatchMember is an UPDATE rather than an upsert
// — an upsert would answer by MINTING a member whose other columns are whatever
// the schema defaults to.
func TestPatchMemberUnknownIDIsACleanNoOp(t *testing.T) {
	d := newTestDAL(t)
	if err := d.PatchMember("m-never-existed", mfName("ghost")); err != nil {
		t.Fatalf("patching an unknown id must not error, got %v", err)
	}
	got, err := d.GetMember("m-never-existed")
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if got != nil {
		t.Fatalf("patching an unknown id must NOT create a row, got %#v.\n"+
			"PatchMember is an UPDATE on purpose: a patch that can create rows "+
			"lets a caller naming two columns mint a member whose other columns "+
			"are schema defaults.", *got)
	}
	// Positive control: the same call against a row that DOES exist lands, so
	// the assertion above is not passing because PatchMember writes nothing.
	if err := d.PutMember(fullMember("m-1")); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := d.PatchMember("m-1", mfName("ghost")); err != nil {
		t.Fatalf("patch: %v", err)
	}
	if m, err := d.GetMember("m-1"); err != nil || m == nil || m.Name != "ghost" {
		t.Fatalf("positive control: the same patch must land on an existing row, got %v %v", m, err)
	}
}

// TestForcedStopAtOnlyMovesForwardOnEveryPath covers the owner's rule for a
// 「只能往前」 column (rc-78cb22a6de94) on BOTH doors that write it.
//
// SetMemberForcedStopAt used to be a plain `forced_stop_at = ?` while the
// whole-row door used max(), so the two disagreed about the same column: a
// backdated stamp through the setter erased a force-stop the gate had already
// recorded. Both paths now go through the column's own forwardOnly declaration.
//
// MUTANT: drop `forwardOnly: true` from mfForcedStopAt and both sub-tests go red
// naming forced_stop_at.
func TestForcedStopAtOnlyMovesForwardOnEveryPath(t *testing.T) {
	paths := []struct {
		name  string
		write func(d *DAL, id string, ts float64) error
	}{
		{"whole-row PutMember", func(d *DAL, id string, ts float64) error {
			m := fullMember(id)
			m.ForcedStopAt = ts
			return d.PutMember(m)
		}},
		{"SetMemberForcedStopAt", func(d *DAL, id string, ts float64) error {
			return d.SetMemberForcedStopAt(id, ts)
		}},
	}
	for _, p := range paths {
		t.Run(p.name, func(t *testing.T) {
			d := newTestDAL(t)
			seed := fullMember("m-1")
			seed.ForcedStopAt = 500
			if err := d.PutMember(seed); err != nil {
				t.Fatalf("seed: %v", err)
			}
			if m, err := d.GetMember("m-1"); err != nil || m == nil || m.ForcedStopAt != 500 {
				t.Fatalf("seed did not land: %v %v", m, err)
			}

			// A stale writer carrying an older stamp — or none at all.
			if err := p.write(d, "m-1", 100); err != nil {
				t.Fatalf("stale write: %v", err)
			}
			m, err := d.GetMember("m-1")
			if err != nil || m == nil {
				t.Fatalf("read back: %v %v", m, err)
			}
			if m.ForcedStopAt != 500 {
				t.Fatalf("member.forced_stop_at walked BACKWARDS through %s: 500 → %v.\n"+
					"forced_stop_at is declared forwardOnly (mfForcedStopAt in "+
					"dal_member_patch.go); every writer must go through that "+
					"declaration so a stale snapshot cannot erase a force-stop.",
					p.name, m.ForcedStopAt)
			}

			// Forward still moves, so the guard above is not passing because the
			// column became unwritable.
			if err := p.write(d, "m-1", 900); err != nil {
				t.Fatalf("forward write: %v", err)
			}
			m, err = d.GetMember("m-1")
			if err != nil || m == nil {
				t.Fatalf("read back: %v %v", m, err)
			}
			if m.ForcedStopAt != 900 {
				t.Fatalf("member.forced_stop_at must still move forward through %s: got %v, want 900",
					p.name, m.ForcedStopAt)
			}
		})
	}
}

// TestMemberColumnPropertiesAreDeclaredInOnePlace asserts the classification
// itself, so "adding a monotone column is ONE LINE" is a fact rather than an
// intention: the only way to change what a column is, is to change its
// constructor, and doing so lands here NAMING the column.
//
// It asserts the WHOLE classification rather than spot-checking, because the
// failure this replaces is a column silently joining or leaving the whole-row
// writer's update — which nothing else notices.
//
// MUTANT (insert-only): clear insertOnly on any column ⇒ red naming it, and the
// behavioural twin TestPutMemberNeverOverwritesSingleColumnOwnedFields goes red
// too.
// MUTANT (forward-only): clear forwardOnly on forced_stop_at or agent_iat_floor
// ⇒ red naming it.
func TestMemberColumnPropertiesAreDeclaredInOnePlace(t *testing.T) {
	// A whole-row writer carries every column on INSERT and only the
	// non-insertOnly ones onto an existing row.
	wantInsertOnly := map[string]bool{
		"id": true, "runtime": true, "model": true, "effort": true,
		"desired_machine_id": true, "banked_cost": true,
		"last_op": true, "last_op_ok": true, "last_op_log": true,
		"last_op_reason": true, "last_op_at": true,
		"avatar_attachment_id": true, "handover_noticed_ts": true,
		"agent_iat_floor": true,
	}
	// Columns that only ever move forward. agent_iat_floor is BOTH: it is
	// insert-only today AND monotone, and declaring the monotonicity now is what
	// makes the max() already true on the day it is allowed onto an existing row.
	wantForwardOnly := map[string]bool{
		"forced_stop_at": true, "agent_iat_floor": true,
	}

	fields := memberWholeRow(fullMember("m-1"))
	gotInsertOnly := map[string]bool{}
	gotForwardOnly := map[string]bool{}
	seen := map[string]bool{}
	for _, f := range fields {
		if seen[f.col] {
			t.Fatalf("column %q is declared twice in memberWholeRow", f.col)
		}
		seen[f.col] = true
		if f.insertOnly {
			gotInsertOnly[f.col] = true
		}
		if f.forwardOnly {
			gotForwardOnly[f.col] = true
		}
	}

	for col := range wantInsertOnly {
		if !gotInsertOnly[col] {
			t.Errorf("member.%s lost its insertOnly declaration ⇒ a whole-row "+
				"writer will now carry a STALE value of it onto an existing row. "+
				"Its single-column setter is meant to be the only writer that "+
				"moves it; its constructor is in dal_member_patch.go.", col)
		}
	}
	for col := range gotInsertOnly {
		if !wantInsertOnly[col] {
			t.Errorf("member.%s became insertOnly ⇒ it LEFT the whole-row "+
				"writer's update. That is a real behaviour change (52 callers "+
				"stop writing it); say why, then bump this list.", col)
		}
	}
	for col := range wantForwardOnly {
		if !gotForwardOnly[col] {
			t.Errorf("member.%s lost its forwardOnly declaration ⇒ a stale or "+
				"zero value can now walk it BACKWARDS. It is a 「只能往前」 "+
				"column (owner rc-78cb22a6de94); the declaration is on its "+
				"constructor in dal_member_patch.go.", col)
		}
	}
	for col := range gotForwardOnly {
		if !wantForwardOnly[col] {
			t.Errorf("member.%s became forwardOnly. If it really only ever moves "+
				"forward that is a good change — bump this list and say so.", col)
		}
	}

	// The whole-row door's update set is derived from the flags, not from a
	// hand-kept SET list. 35 columns minus the 14 insert-only ones is the 21 the
	// old ON CONFLICT DO UPDATE SET wrote.
	if got, want := len(fields), 35; got != want {
		t.Errorf("memberWholeRow projects %d columns, want %d — it must track "+
			"memberColumns, which the INSERT binds positionally against", got, want)
	}
	if got, want := len(updatableMemberFields(fields)), 21; got != want {
		t.Errorf("a whole-row write now updates %d columns, want %d", got, want)
	}
}

// memberComparable flattens the two pointer fields into values so a whole row
// can be compared with DeepEqual. It FLATTENS rather than drops them: nilling
// them out would make the comparison blind to exactly the two columns whose
// NULL rules the patch door had to carry over (last_op_ok's three-valued state
// and linked_task_id's nil-means-unbound), so a leak into either would pass.
type comparableMember struct {
	Row          Member
	LastOpOK     string // "nil" | "true" | "false" — the three-valued state
	LinkedTaskID string // "nil" | the id
}

func memberComparable(m Member) comparableMember {
	c := comparableMember{LastOpOK: "nil", LinkedTaskID: "nil"}
	if m.LastOpOK != nil {
		if *m.LastOpOK {
			c.LastOpOK = "true"
		} else {
			c.LastOpOK = "false"
		}
	}
	if m.LinkedTaskID != nil {
		c.LinkedTaskID = *m.LinkedTaskID
	}
	m.LastOpOK = nil
	m.LinkedTaskID = nil
	c.Row = m
	return c
}
