package main

// dal_custom_themes.go — T-83ef, the access layer for the per-theme table that
// migration 00059 introduced.
//
// The whole reason this seam exists is that the thing it replaces had no
// per-item write at all: custom themes lived as ONE json array inside ONE
// settings value, so "save this theme" was spelled "rewrite every theme,
// including every embedded image". Here the unit is a row, and that is the
// entire point — everything below is deliberately narrow.
//
// 🔴 `Bundle` IS RAW JSON TEXT, AND THAT IS A CONTRACT, NOT AN IMPLEMENTATION
// DETAIL. The migration copies each array element's own bytes so the move can be
// proved byte-for-byte before anything old is retired (see the migration's
// header). This layer keeps that property: it stores and returns the text it is
// given and never decodes it. Decoding belongs to the API layer, where the DTO
// is the wire contract; doing it here would put a lossy round-trip underneath
// the one guarantee the ticket rests on.
//
// 🔴 READ THIS BEFORE WRITING THE ENDPOINTS: WHILE `display.custom_themes` STILL
// EXISTS, IT IS THE TRUTH AND THIS TABLE IS A COPY. Migration 00059 copies once
// and nothing keeps the two in step afterwards, so a write that lands only here
// makes `GET /api/settings` and this table disagree — silently, with the
// settings face still serving the pre-upgrade answer. The change that adds the
// per-theme endpoints has to pick one: retire the legacy row in that same
// package, or write BOTH until it does. It cannot ignore the question, and the
// migration's header carries the precondition for retiring: refuse while the
// settings key customThemeSkipRecordKey exists, because Up skips elements it
// cannot key and that key is the receipt saying something was left behind. (An
// earlier draft of this sentence said "row count must match the legacy array's
// length" — that test can never pass on an install that skipped something, which
// would have locked those installs out of retirement with no way forward.)
//
// ⚠️ THE BYTE-FOR-BYTE GUARANTEE HAS A TIME WINDOW, and it closes here. It holds
// for what the migration wrote; the first write through this layer replaces that
// theme's bytes with whatever the caller marshalled. That is correct and
// intended — but it means the comparison that authorises retiring the legacy row
// has to be run BEFORE the endpoints start writing, not after.

import (
	"database/sql"
	"errors"
	"fmt"
)

// CustomTheme is one saved theme as it is stored: identity, the bundle's JSON
// text, its position in the owner's list, and when it was last written.
//
// UpdatedAt is 0 for rows created by migration 00059 — nothing knows when a
// theme that lived inside a shared settings value was last edited, and stamping
// migrate time would invent an edit that never happened.
type CustomTheme struct {
	ID        string
	Bundle    string
	OrderIdx  int
	UpdatedAt float64
}

// ListCustomThemes returns every saved theme in the owner's list order.
//
// ⚠️ ORDER COMES FROM order_idx, AND THE HONEST STATE OF THAT IS: it is a
// DELIBERATE CHOICE, not a bug fix. Measured on this tree (2026-08-17), the
// column cannot currently disagree with rowid order at all — a new row takes
// MAX(order_idx)+1 while SQLite gives it max(rowid)+1, so append,
// delete-then-re-add (middle row and highest row alike), editing through the
// upsert's conflict path, and VACUUM all leave `ORDER BY order_idx` and
// `ORDER BY rowid` identical. An earlier version of this comment claimed a
// delete-and-re-add would silently reshuffle the owner's list; an independent
// review disproved it, and the claim is gone rather than softened.
//
// The column stays because the list order is a fact the MIGRATION knows and
// writes down, while rowid order is an accident that currently agrees with it —
// and because inserting at a position, or an import that reorders, needs a
// column that means position. TestCustomThemeListOrderComesFromOrderIdxNotRowid
// is what stops this query drifting to `ORDER BY rowid`: it seeds rows whose
// stored positions deliberately contradict their insertion order, which is the
// one state the product cannot reach on its own and the only one that tells the
// two orderings apart.
func (d *DAL) ListCustomThemes() ([]CustomTheme, error) {
	rows, err := d.rdb.Query(
		`SELECT theme_id, bundle, order_idx, updated_at FROM custom_theme ORDER BY order_idx`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []CustomTheme
	for rows.Next() {
		var t CustomTheme
		if err := rows.Scan(&t.ID, &t.Bundle, &t.OrderIdx, &t.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// GetCustomTheme returns one saved theme, or nil when no theme carries that id.
// A missing theme is not an error here: "does this id exist" is a question the
// callers ask on purpose (a PUT deciding create-vs-replace, a DELETE reporting
// 404), and folding it into an error would make them parse one.
func (d *DAL) GetCustomTheme(id string) (*CustomTheme, error) {
	t := CustomTheme{ID: id}
	err := d.rdb.QueryRow(
		`SELECT bundle, order_idx, updated_at FROM custom_theme WHERE theme_id = ?`, id).
		Scan(&t.Bundle, &t.OrderIdx, &t.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &t, nil
}

// PutCustomTheme creates or replaces ONE theme — the write this whole ticket
// exists to make expressible.
//
// 🔴 order_idx IS NOT IN THE UPDATE CLAUSE, AND THAT IS THE POINT. A new theme
// is appended (MAX + 1); an existing theme KEEPS the position it already had, so
// editing a theme's colours does not move it to the bottom of the owner's list.
// The VALUES expression still computes an append position on the conflict path —
// SQLite evaluates it before it discovers the conflict — and it is discarded
// there, which is exactly the behaviour wanted and the reason the column is
// absent from DO UPDATE SET rather than being set to itself.
//
// COALESCE covers the empty table: MAX over no rows is NULL, and NULL would fail
// the NOT NULL column rather than mean "first".
//
// 🔴 IT REFUSES A MISMATCHED PAIR ITSELF RATHER THAN LETTING THE TABLE DO IT, and
// that is not belt-and-braces — it is the difference between a 400 and a 500. See
// checkCustomThemeIDMatchesBundle.
func (d *DAL) PutCustomTheme(id, bundle string) error {
	if err := d.checkCustomThemeIDMatchesBundle(id, bundle); err != nil {
		return err
	}
	_, err := d.wdb.Exec(`
		INSERT INTO custom_theme (theme_id, bundle, order_idx, updated_at)
		VALUES (?, ?, COALESCE((SELECT MAX(order_idx) + 1 FROM custom_theme), 0), ?)
		ON CONFLICT (theme_id) DO UPDATE SET
			bundle = excluded.bundle, updated_at = excluded.updated_at`,
		id, bundle, nowSecs())
	return err
}

// ErrCustomThemeIDMismatch is what a caller gets when the key it asked to file a
// bundle under is not the id inside that bundle. It is a NAMED error because the
// handler above this layer has to turn it into a 400 that says what is wrong —
// `errors.Is` against this, never a string match on a database message.
var ErrCustomThemeIDMismatch = errors.New("custom theme: the bundle's own id does not match the id it is being filed under")

// checkCustomThemeIDMatchesBundle asks THE DATABASE what the bundle's id is, and
// refuses the write when that disagrees with the key.
//
// 🔴 WHY THIS EXISTS EVEN THOUGH THE TABLE ALREADY HAS A CHECK FOR IT. The
// constraint is a table-level guard: it fires at INSERT time, from inside the
// driver, as a failed write. Every write path that reaches this layer for the
// rest of the product's life passes under it — including the per-theme endpoints
// this ticket is being split to make possible. Without this function, the first
// bundle whose id Go and SQLite read differently produces a failed statement at
// runtime, which is a 500 and a log line, when what the caller deserves is a 400
// naming the field.
//
// 🔴 AND THE DISAGREEMENT IS REAL, MEASURED, NOT DEFENSIVE PROGRAMMING. Go's
// decoder and SQLite's json_extract do not agree on every input both accept:
// `{"id":"a","id":"b"}` (duplicate key) reads as "b" in Go and "a" in SQLite;
// `{"id":"a\ud800b"}` (lone surrogate) reads as a U+FFFD substitution in Go and
// as the original bytes in SQLite. A handler that derives the key from the
// decoded DTO — the obvious way to write it — hands this layer a pair the table
// will reject. Migration 00059 hit exactly this and skips such elements; the
// endpoints cannot skip, so they need an answer, and this is it.
//
// It asks SQLite rather than re-deriving in Go on purpose: the question is not
// "what does this bundle say" but "will the constraint agree", and only the same
// reader the constraint uses can answer that.
func (d *DAL) checkCustomThemeIDMatchesBundle(id, bundle string) error {
	if id == "" {
		return fmt.Errorf("%w: the id is empty", ErrCustomThemeIDMismatch)
	}
	var dbID sql.NullString
	if err := d.rdb.QueryRow(`SELECT json_extract(?, '$.id')`, bundle).Scan(&dbID); err != nil {
		// json_extract refuses malformed JSON outright, so this branch is also
		// the "bundle is not JSON" answer — same 400, and naming it here beats a
		// constraint violation surfacing from the driver.
		return fmt.Errorf("%w: the bundle is not readable JSON", ErrCustomThemeIDMismatch)
	}
	if !dbID.Valid {
		return fmt.Errorf("%w: the bundle carries no id", ErrCustomThemeIDMismatch)
	}
	if dbID.String != id {
		return fmt.Errorf("%w: bundle says %q, filed under %q", ErrCustomThemeIDMismatch, dbID.String, id)
	}
	return nil
}

// DeleteCustomTheme removes one theme and reports whether a row was actually
// removed, so a handler can tell 404 from 204 without a second query.
//
// It deliberately does NOT renumber the survivors. Gaps in order_idx are
// harmless — ORDER BY reads a sparse sequence exactly as well as a dense one —
// and renumbering would rewrite every remaining row on every delete, which is
// the whole-set write this ticket removed, reintroduced through a side door.
func (d *DAL) DeleteCustomTheme(id string) (bool, error) {
	res, err := d.wdb.Exec(`DELETE FROM custom_theme WHERE theme_id = ?`, id)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

// CountCustomThemes answers the cap check (maxCustomThemes) without loading
// every bundle — the rows being counted are the ones carrying the embedded
// images, so reading them to measure how many there are would defeat the split.
func (d *DAL) CountCustomThemes() (int, error) {
	var n int
	if err := d.rdb.QueryRow(`SELECT COUNT(*) FROM custom_theme`).Scan(&n); err != nil {
		return 0, err
	}
	return n, nil
}
