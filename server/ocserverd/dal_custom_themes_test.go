package main

// T-83ef — the per-theme write seam.
//
// These tests are about ONE property above all others: writing a theme must
// touch that theme and nothing else. The thing being replaced could not do
// that, and every regression this file can catch is a slide back towards it —
// a write that rewrites its neighbours, a write that moves the list order, a
// delete that renumbers everyone.

import (
	"testing"
)

func t83efPut(t *testing.T, d *DAL, id, bundle string) {
	t.Helper()
	if err := d.PutCustomTheme(id, bundle); err != nil {
		t.Fatalf("put %s: %v", id, err)
	}
}

func t83efList(t *testing.T, d *DAL) []CustomTheme {
	t.Helper()
	got, err := d.ListCustomThemes()
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	return got
}

func TestCustomThemeWriteTouchesOnlyThatTheme(t *testing.T) {
	d := newTestDAL(t)
	t83efPut(t, d, "alpha", `{"id":"alpha","name":"A"}`)
	t83efPut(t, d, "beta", `{"id":"beta","name":"B"}`)
	t83efPut(t, d, "gamma", `{"id":"gamma","name":"C"}`)

	before := t83efList(t, d)
	t83efPut(t, d, "beta", `{"id":"beta","name":"B edited"}`)
	after := t83efList(t, d)

	if len(after) != 3 {
		t.Fatalf("after editing one theme the list holds %d, want 3", len(after))
	}
	for i := range after {
		if after[i].ID != before[i].ID {
			t.Fatalf("position %d changed from %q to %q — editing a theme must not reorder the list",
				i, before[i].ID, after[i].ID)
		}
		if after[i].OrderIdx != before[i].OrderIdx {
			t.Fatalf("theme %q moved from order_idx %d to %d",
				after[i].ID, before[i].OrderIdx, after[i].OrderIdx)
		}
		if after[i].ID == "beta" {
			continue
		}
		// The neighbours: byte-identical, because they were not the write.
		if after[i].Bundle != before[i].Bundle {
			t.Fatalf("theme %q was rewritten by a write aimed at beta.\n before: %s\n  after: %s",
				after[i].ID, before[i].Bundle, after[i].Bundle)
		}
		if after[i].UpdatedAt != before[i].UpdatedAt {
			t.Fatalf("theme %q got a new updated_at from a write aimed at beta", after[i].ID)
		}
	}
	got, err := d.GetCustomTheme("beta")
	if err != nil || got == nil {
		t.Fatalf("get beta: %v (nil=%v)", err, got == nil)
	}
	if want := `{"id":"beta","name":"B edited"}`; got.Bundle != want {
		t.Fatalf("beta bundle is %s, want %s", got.Bundle, want)
	}
}

func TestCustomThemeAppendsNewButKeepsExistingPosition(t *testing.T) {
	d := newTestDAL(t)
	t83efPut(t, d, "first", `{"id":"first"}`)
	t83efPut(t, d, "second", `{"id":"second"}`)

	// Re-writing the FIRST theme must not send it to the bottom. This is the
	// single most likely bug in an upsert that also has to append: putting
	// order_idx in the DO UPDATE SET clause looks harmless and quietly reorders
	// the owner's list on every colour edit.
	t83efPut(t, d, "first", `{"id":"first","name":"still first"}`)
	t83efPut(t, d, "third", `{"id":"third"}`)

	got := t83efList(t, d)
	want := []string{"first", "second", "third"}
	if len(got) != len(want) {
		t.Fatalf("list holds %d themes, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i].ID != want[i] {
			t.Fatalf("position %d is %q, want %q", i, got[i].ID, want[i])
		}
	}
}

func TestCustomThemeDeleteRemovesOneAndReportsWhetherItExisted(t *testing.T) {
	d := newTestDAL(t)
	t83efPut(t, d, "keep-a", `{"id":"keep-a"}`)
	t83efPut(t, d, "drop", `{"id":"drop"}`)
	t83efPut(t, d, "keep-b", `{"id":"keep-b"}`)

	removed, err := d.DeleteCustomTheme("drop")
	if err != nil {
		t.Fatalf("delete: %v", err)
	}
	if !removed {
		t.Fatal("delete of an existing theme reported that nothing was removed")
	}
	// The distinction a handler needs to answer 404 rather than a cheerful 204.
	removed, err = d.DeleteCustomTheme("drop")
	if err != nil {
		t.Fatalf("second delete: %v", err)
	}
	if removed {
		t.Fatal("delete of an absent theme reported a removal")
	}

	got := t83efList(t, d)
	want := []string{"keep-a", "keep-b"}
	if len(got) != len(want) {
		t.Fatalf("list holds %d themes, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i].ID != want[i] {
			t.Fatalf("position %d is %q, want %q", i, got[i].ID, want[i])
		}
	}
	// The survivors keep the order_idx values they had. Renumbering them would
	// be a whole-set write hiding inside a single delete — the exact shape this
	// ticket removed.
	if got[0].OrderIdx != 0 || got[1].OrderIdx != 2 {
		t.Fatalf("survivors were renumbered: order_idx %d and %d, want 0 and 2",
			got[0].OrderIdx, got[1].OrderIdx)
	}
}

// TestCustomThemeListOrderComesFromOrderIdxNotRowid is the ONLY thing standing
// between `ORDER BY order_idx` and someone simplifying it to `ORDER BY rowid`.
//
// 🔴 IT HAS TO CHEAT TO SAY ANYTHING, and that is the finding, not a weakness of
// the test. Through the product's own write path the two orderings cannot
// disagree — order_idx is MAX+1 and rowid is max+1, so they move together
// through every sequence of appends, deletes, re-adds and edits (measured; an
// independent review demonstrated the equivalence, and swapping this query to
// `ORDER BY rowid` left every other test in this file green). The only state
// that separates them is one the product cannot reach on its own, so the rows
// are seeded DIRECTLY with positions that contradict their insertion order.
//
// That is exactly the situation the column exists for: a future insert-at-
// position or a reordering import produces this state legitimately, and the day
// it does, this query has to already be reading the column rather than the
// accident that agreed with it.
func TestCustomThemeListOrderComesFromOrderIdxNotRowid(t *testing.T) {
	d := newTestDAL(t)
	// Inserted last-to-first by position: rowid order is the REVERSE of the
	// order these rows claim to be in.
	for _, seed := range []struct {
		id  string
		idx int
	}{{"third", 2}, {"second", 1}, {"first", 0}} {
		if _, err := d.wdb.Exec(
			`INSERT INTO custom_theme (theme_id, bundle, order_idx, updated_at) VALUES (?, ?, ?, 0)`,
			seed.id, `{"id":"`+seed.id+`"}`, seed.idx); err != nil {
			t.Fatalf("seed %s: %v", seed.id, err)
		}
	}
	got := t83efList(t, d)
	want := []string{"first", "second", "third"}
	if len(got) != len(want) {
		t.Fatalf("list holds %d themes, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i].ID != want[i] {
			t.Fatalf("position %d is %q, want %q — the list is being read in insertion order, not stored order",
				i, got[i].ID, want[i])
		}
	}
}

func TestCustomThemeGetAndCountOnAnEmptyTable(t *testing.T) {
	d := newTestDAL(t)
	got, err := d.GetCustomTheme("nobody")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got != nil {
		t.Fatalf("an id nobody carries returned %+v, want nil", *got)
	}
	n, err := d.CountCustomThemes()
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 0 {
		t.Fatalf("count is %d on an empty table, want 0", n)
	}
	// COALESCE in the append expression: MAX over no rows is NULL, and NULL
	// would fail the NOT NULL column rather than mean "first".
	t83efPut(t, d, "only", `{"id":"only"}`)
	list := t83efList(t, d)
	if len(list) != 1 || list[0].OrderIdx != 0 {
		t.Fatalf("first theme into an empty table: got %+v, want one row at order_idx 0", list)
	}
}

func TestCustomThemeBundleIsStoredAsGivenWithoutDecoding(t *testing.T) {
	d := newTestDAL(t)
	// A key the DTO does not declare, and formatting encoding/json would not
	// produce. Both survive only if this layer treats the bundle as text —
	// which is what keeps the migration's byte-for-byte guarantee intact
	// underneath every later write.
	const raw = `{"id":"odd", "name":"spaced","fieldTheDTODoesNotKnow":1}`
	t83efPut(t, d, "odd", raw)
	got, err := d.GetCustomTheme("odd")
	if err != nil || got == nil {
		t.Fatalf("get: %v (nil=%v)", err, got == nil)
	}
	if got.Bundle != raw {
		t.Fatalf("bundle came back changed.\n stored: %s\n    got: %s", raw, got.Bundle)
	}
}
