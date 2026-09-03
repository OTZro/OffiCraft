package main

// routes_manifest_parity_t61_test.go — T-61's gate: the missing leg between
// WHAT THE SERVER ACTUALLY SERVES and conformance/routes_manifest.json.
//
// ── THE GAP THIS CLOSES ─────────────────────────────────────────────────────
//
// routes_manifest.json is a hand-written, committed snapshot. Everything that
// grades authorization reads FROM it: conformance/test_auth_matrix.py fires a
// real request from five identities at every gated row it lists, and
// test_openapi_covers_manifest pins it to spec/openapi.json. So a route that is
// NOT in the manifest is not "failing" the permission suite — it is INVISIBLE
// to it, and nothing anywhere goes red.
//
// Measured on 4cfc4e01 (T-61 recon), the manifest was NOT entirely unpinned —
// half of it was already held, and the ticket's premise needed correcting:
//
//   - The 123 rows carrying an `mcp_tool` ARE pinned to the live server today.
//     assets.go:catalogHashOf hashes exactly the non-MCPExclude rows of THIS
//     table and the server serves that digest as /api/version's catalog_hash;
//     conformance's test_catalog_hash_algorithm recomputes the same digest from
//     the manifest and compares. Add or drop an MCP-visible route without
//     touching the manifest and that comparison already reddens.
//   - The 48 rows with `mcp_tool: null` — the infra surface, every MCPExclude
//     row — are held by NOTHING that reads the server. They are pinned only to
//     other hand-written lists (openapi's operation set, test_rest_happy's HAPPY
//     table). Two hand-written lists agreeing proves they were typed the same
//     day, not that either matches the code.
//
// So the hole is real but it is INFRA-SHAPED: add an MCPExclude route to
// routes.go and no test in this repo notices. That is the class this file ends,
// and it does it for the whole table rather than for the half that was loose,
// because a gate that covers "the rows nobody else covers" needs a second
// hand-written list to know which those are — the exact disease.
//
// ── WHERE THE DENOMINATOR COMES FROM ────────────────────────────────────────
//
// routeSpecs() (routes.go) is not a second snapshot of the surface: it IS the
// surface. server.go:336-353 builds the mux by ranging over it — one
// mux.Handle per row and nothing else registers a route (the only other
// registration is the "/" static fallback). A route the server serves is a row
// here, necessarily; there is no other door. That is why this gate is in Go and
// not another Python list.
//
// ── WHAT THIS GATE DOES NOT DO ──────────────────────────────────────────────
//
// It compares MEMBERSHIP (method+path), not the auth/requires/mcp_tool columns.
// Those are graded by live requests in test_auth_matrix.py: a row whose floor in
// routes.go disagrees with the manifest fails there against the real server, so
// duplicating the comparison here would add a second opinion, not a second gate.
//
// And, like authz_surface_gate_test.go before it, this file cannot judge whether
// an exemption's REASON is honest. A well-keyed entry with a fluent excuse
// passes. What it buys is that adding one MUST appear in the diff, next to a
// name and a ruling, where a reviewer sees it.

import (
	"encoding/json"
	"os"
	"sort"
	"strings"
	"testing"
)

// ── the exemption shape ─────────────────────────────────────────────────────
//
// STRUCTURAL, not procedural. An exemption exists only as the return value of
// exemptRoute(), whose four positional parameters make "I skipped the reason"
// a COMPILE error rather than a review miss. Both the type and the constructor
// live in this _test.go file, so no product code can reach them — the same
// shape the repo used for its test-only exemption setters.
//
// The runtime gate below then refuses a shrug: a reason under
// exemptionReasonFloor characters, or an empty ruling, or an entry that no
// longer matches anything, all redden.
type routeExemption struct {
	method string
	path   string
	reason string
	ruling string
}

// exemptionReasonFloor is a length, not a quality bar. It stops "n/a" and
// "legacy"; it cannot stop a fluent sentence, and it is not meant to.
const exemptionReasonFloor = 40

// exemptRoute is the ONLY way to build a routeExemption. Every field is
// required by the signature.
//
//	method, path — must match a row exactly ({param} names included)
//	reason       — why this route is deliberately outside the permission suite
//	ruling       — who decided (ticket key, or "owner YYYY-MM-DD")
func exemptRoute(method, path, reason, ruling string) routeExemption {
	return routeExemption{method: method, path: path, reason: reason, ruling: ruling}
}

// servedButUnlisted: routes the server serves ON PURPOSE without a manifest row
// (and therefore with no permission-matrix cell).
//
// EMPTY ON 4cfc4e01, and that is a measurement, not an aspiration: every one of
// the 171 served rows is listed. An entry here is a route nobody's permission
// test will ever touch again.
var servedButUnlisted = []routeExemption{}

// listedButUnserved: manifest rows kept ON PURPOSE for a path the server no
// longer serves. Also empty on 4cfc4e01.
var listedButUnserved = []routeExemption{}

// ── corpus floors ───────────────────────────────────────────────────────────
//
// This repo has shipped "assert empty, then range over the empty set" before,
// so both sides prove they were populated BEFORE any verdict is read. The
// floors are far below the real counts (171 rows on 4cfc4e01) — they catch a
// scanner that stopped seeing, not growth or shrinkage.
const (
	servedRowFloor   = 100
	manifestRowFloor = 100
)

type manifestRow struct {
	Method string `json:"method"`
	Path   string `json:"path"`
}

func loadRoutesManifest(t *testing.T) []manifestRow {
	t.Helper()
	raw, err := os.ReadFile("../../conformance/routes_manifest.json")
	if err != nil {
		t.Fatalf("cannot read conformance/routes_manifest.json: %v", err)
	}
	var rows []manifestRow
	if err := json.Unmarshal(raw, &rows); err != nil {
		t.Fatalf("conformance/routes_manifest.json is not a JSON array of rows: %v", err)
	}
	return rows
}

func routeKey(method, path string) string {
	return strings.ToUpper(method) + " " + path
}

// TestEveryServedRouteIsInThePermissionManifest is the gate. Both directions,
// every offender named.
func TestEveryServedRouteIsInThePermissionManifest(t *testing.T) {
	specs := defaultRouteSpecs()
	rows := loadRoutesManifest(t)

	// (0) Populated corpora first — otherwise "no difference" is the answer an
	// empty set gives too.
	if len(specs) < servedRowFloor {
		t.Fatalf("routeSpecs returned %d rows, below the floor of %d — the route "+
			"table is the denominator of this gate and it looks truncated; every "+
			"verdict below would be about a table nobody built",
			len(specs), servedRowFloor)
	}
	if len(rows) < manifestRowFloor {
		t.Fatalf("routes_manifest.json carries %d rows, below the floor of %d — "+
			"the manifest looks truncated, and a truncated manifest makes this "+
			"gate shout about routes that are fine",
			len(rows), manifestRowFloor)
	}

	served := map[string]bool{}
	for _, s := range specs {
		k := routeKey(s.Method, s.Path)
		if served[k] {
			t.Errorf("route table declares %s twice — the mux registration in "+
				"server.go would panic on the second one", k)
		}
		served[k] = true
	}
	listed := map[string]bool{}
	for _, r := range rows {
		k := routeKey(r.Method, r.Path)
		if listed[k] {
			t.Errorf("routes_manifest.json lists %s twice", k)
		}
		listed[k] = true
	}

	// (1) Exemptions are checked BEFORE they are honoured: a stale one is a
	// silent hole, so it reddens rather than quietly excusing nothing.
	unlisted := exemptionIndex(t, servedButUnlisted, served,
		"served-but-unlisted", "the route table no longer serves it")
	unserved := exemptionIndex(t, listedButUnserved, listed,
		"listed-but-unserved", "the manifest no longer lists it")

	// (2) The comparison itself.
	var missing, stale []string
	for k := range served {
		if !listed[k] && !unlisted[k] {
			missing = append(missing, k)
		}
	}
	for k := range listed {
		if !served[k] && !unserved[k] {
			stale = append(stale, k)
		}
	}
	sort.Strings(missing)
	sort.Strings(stale)

	for _, k := range missing {
		t.Errorf("SERVED BUT NOT IN THE PERMISSION MANIFEST: %s\n"+
			"    the server routes this (routes.go builds the mux from that row) but "+
			"conformance/routes_manifest.json has no line for it, so "+
			"test_auth_matrix.py never fires a single request at it from any "+
			"identity. It is not failing the permission suite; it is not IN it.\n"+
			"    Fix: add the row to conformance/routes_manifest.json (and the "+
			"spec/openapi.json operation + happy row the conformance suite will "+
			"then demand). Deliberately outside? Add an exemptRoute(...) entry to "+
			"servedButUnlisted in this file, with a reason and a ruling.", k)
	}
	for _, k := range stale {
		t.Errorf("IN THE PERMISSION MANIFEST BUT NOT SERVED: %s\n"+
			"    conformance/routes_manifest.json declares this route and the "+
			"permission matrix grades cells for it, but the server's route table "+
			"has no such row — the cells are graded against a path that answers "+
			"404, so whatever they prove, it is not about this server.\n"+
			"    Fix: drop the row from conformance/routes_manifest.json, or add "+
			"an exemptRoute(...) entry to listedButUnserved in this file.", k)
	}

	t.Logf("compared %d served routes against %d manifest rows "+
		"(exemptions: %d served-but-unlisted, %d listed-but-unserved)",
		len(served), len(listed), len(servedButUnlisted), len(listedButUnserved))
}

// exemptionIndex validates one exemption list and returns its keys. An entry
// that names nothing in `corpus` is STALE and reddens: it is an excuse standing
// over a hole that moved.
func exemptionIndex(
	t *testing.T,
	list []routeExemption,
	corpus map[string]bool,
	label string,
	goneMeans string,
) map[string]bool {
	t.Helper()
	index := map[string]bool{}
	for _, e := range list {
		k := routeKey(e.method, e.path)
		if index[k] {
			t.Errorf("%s exemption for %s is declared twice", label, k)
		}
		index[k] = true
		if len(strings.TrimSpace(e.reason)) < exemptionReasonFloor {
			t.Errorf("%s exemption for %s carries a %d-character reason; this gate "+
				"asks for at least %d. The length is not the point — a route "+
				"outside the permission suite needs a sentence a reviewer can "+
				"disagree with.",
				label, k, len(strings.TrimSpace(e.reason)), exemptionReasonFloor)
		}
		if strings.TrimSpace(e.ruling) == "" {
			t.Errorf("%s exemption for %s names no ruling — put the ticket key or "+
				"\"owner YYYY-MM-DD\" that decided it, so the next reader can go "+
				"read the decision instead of re-deriving it", label, k)
		}
		if !corpus[k] {
			t.Errorf("STALE %s exemption: %s — %s, so this entry now excuses "+
				"nothing while still reading like a decision someone made. Delete "+
				"it. (reason on file: %q)", label, k, goneMeans, e.reason)
		}
	}
	return index
}

// TestRouteManifestParityGateIsNotVacuous is the gate's own positive control.
// It proves the comparison HAS teeth by running it over a doctored copy of the
// two corpora: a served row the manifest does not carry, and a manifest row the
// server does not serve. If either synthetic difference goes unnoticed here, the
// real comparison above is decoration.
func TestRouteManifestParityGateIsNotVacuous(t *testing.T) {
	served := map[string]bool{}
	for _, s := range defaultRouteSpecs() {
		served[routeKey(s.Method, s.Path)] = true
	}
	listed := map[string]bool{}
	for _, r := range loadRoutesManifest(t) {
		listed[routeKey(r.Method, r.Path)] = true
	}
	if len(served) == 0 || len(listed) == 0 {
		t.Fatal("empty corpus — the control below would pass vacuously")
	}

	const ghost = "GET /api/t61-route-that-does-not-exist"
	if served[ghost] || listed[ghost] {
		t.Fatalf("%s is a real route now; pick another synthetic key", ghost)
	}

	// direction 1: served, unlisted.
	if diff := onlyIn(withKey(served, ghost), listed); len(diff) != 1 || diff[0] != ghost {
		t.Errorf("the served-but-unlisted comparison did not report a route that is "+
			"served and unlisted: got %v", diff)
	}
	// direction 2: listed, unserved.
	if diff := onlyIn(withKey(listed, ghost), served); len(diff) != 1 || diff[0] != ghost {
		t.Errorf("the listed-but-unserved comparison did not report a route that is "+
			"listed and unserved: got %v", diff)
	}
	// and the undoctored corpora must NOT produce that finding, or the control
	// above would fire for any input at all.
	if diff := onlyIn(served, withKey(listed, ghost)); t61Contains(diff, ghost) {
		t.Errorf("the comparison reports %s as served-only without it being served", ghost)
	}
}

func withKey(set map[string]bool, key string) map[string]bool {
	out := map[string]bool{key: true}
	for k := range set {
		out[k] = true
	}
	return out
}

func onlyIn(a, b map[string]bool) []string {
	var out []string
	for k := range a {
		if !b[k] {
			out = append(out, k)
		}
	}
	sort.Strings(out)
	return out
}

func t61Contains(list []string, want string) bool {
	for _, v := range list {
		if v == want {
			return true
		}
	}
	return false
}
