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

import (
	"database/sql"
	"errors"
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
// ⚠️ ORDER COMES FROM order_idx AND MUST KEEP DOING SO. There is no reordering
// UI, so the list order IS the data, and it is tempting to believe insertion
// order is enough — it is not, and that is measured rather than assumed: with
// every order_idx equal, SQLite answers this query in rowid order, which looks
// correct until a theme is deleted and re-added, at which point the owner's list
// silently reshuffles. The migration test pins the stored values for that reason.
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
func (d *DAL) PutCustomTheme(id, bundle string) error {
	_, err := d.wdb.Exec(`
		INSERT INTO custom_theme (theme_id, bundle, order_idx, updated_at)
		VALUES (?, ?, COALESCE((SELECT MAX(order_idx) + 1 FROM custom_theme), 0), ?)
		ON CONFLICT (theme_id) DO UPDATE SET
			bundle = excluded.bundle, updated_at = excluded.updated_at`,
		id, bundle, nowSecs())
	return err
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
