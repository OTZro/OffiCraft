package main

// migration_version_scan_t49e7_test.go — T-49e7 round 2: a migration version
// number now has TWO possible claimants, so the check that a number is free has
// to ask both.
//
// 🔴 WHY THIS EXISTS. The written procedure for picking a version number is
// "scan every remote branch's migrations directory for the numbers already
// taken". That query answered the whole question right up until this round,
// which added the first Go migration in this repo
// (goose.AddNamedMigrationContext, migration_00054_reaim_custom_cursors.go): a
// migration that lives in a .go file beside the package, NOT under migrations/,
// so the directory scan cannot see a single character of it. The procedure did
// not become wrong, it became STRUCTURALLY INCOMPLETE — and nothing about
// running it looks any different.
//
// 🔴 WHAT GOOSE ACTUALLY DOES WITH A COLLISION — MEASURED, BECAUSE THE OBVIOUS
// STORY IS NOT THIS VERSION'S STORY. The reason usually given for the numbering
// rule is that goose records ONE row per version, so a duplicate ends with the
// version marked applied while one body of DDL never ran: silent, no log line.
// That is NOT what goose v3.27.2 does, and the difference was measured rather
// than reasoned: planting a second 00054 (a .sql file beside the Go migration)
// makes goose PANIC — `goose: duplicate version 54 detected`, raised from
// Migrations.Less, the comparator sort calls while collecting. Two things follow,
// and they pull in opposite directions:
//
//   - it is loud, so the silent-missing-DDL story should not be repeated; but
//   - it is loud AT THE WORST PLACE: inside goose.Up, which is where a station
//     starts up. The operator gets a panic stack trace during an upgrade, on
//     their machine, about two files they did not write. Nothing says it in the
//     repo, at review time, where the number could still be changed for free.
//
// And it is a guarantee we do not have anyway: sort.Sort promises a sorted
// result, not that it compares any particular pair, so a comparator-side check
// is a detection that happens to fire, not one that must. (That it fires for
// today's 55 migrations is measured; that it fires for every future arrangement
// is not something the sort API says.)
//
// A GAP, by contrast, stays cheap: goose.Up refuses to run past a missing
// version, loudly and before anything is applied, which is why server/CLAUDE.md
// rules that a skipped number is left skipped. So the rule is always max+1, and
// this test is that rule made mechanical — the collision is named HERE, in
// `go test`, with both claimants and the fix, instead of being discovered from a
// stack trace during someone's migration.
//
// 🔴 GOOSE CATCHES THE OTHER HALF EARLIER, WHICH IS WHAT MAKES THIS EASY TO
// MISREAD: register() rejects a second GO registration of an already-registered
// version at init, before any of this runs. It is only the Go-versus-SQL
// direction — the one this round created — that survives until collection.
//
// WHAT THIS FILE DOES NOT DO. It sees THIS working tree. It cannot see a number
// taken on a branch that has not landed here — scanning the remotes is still the
// procedure, and this check is what makes that procedure's blind spot
// impossible to inherit once the branches meet.

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"math"
	"os"
	"path"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/pressly/goose/v3"
)

// The two source names are constants because they appear in the failure message
// — a report that does not say WHICH TWO PLACES claim the number sends the
// reader to grep for a file that, in the Go case, does not exist.
const (
	migrationSourceSQL = "migrations/*.sql (embedded SQL file)"
	migrationSourceGo  = "goose.AddNamedMigration* registration (Go migration, NOT under migrations/)"
)

// migrationClaim is one claim on one version number: which source made it and
// where the reader will find it.
type migrationClaim struct {
	version int64
	source  string
	where   string
}

// sqlMigrationClaims enumerates source ①: the SQL files, read from the SAME
// embedded FS goose is handed in runMigrations rather than from the working
// directory, so the corpus is the one that actually ships.
func sqlMigrationClaims(t *testing.T) []migrationClaim {
	t.Helper()
	files, err := fs.Glob(embeddedMigrations, "migrations/*.sql")
	if err != nil {
		t.Fatalf("glob embedded migrations: %v", err)
	}
	var claims []migrationClaim
	for _, f := range files {
		v, err := goose.NumericComponent(f)
		if err != nil {
			t.Fatalf("%s has no version prefix goose can read: %v — goose itself "+
				"refuses to collect such a file, so this is a real defect, not a scan quirk", f, err)
		}
		claims = append(claims, migrationClaim{version: v, source: migrationSourceSQL, where: f})
	}
	return claims
}

// goMigrationClaims enumerates source ②: the Go migrations, by PARSING this
// package and reading every goose registration call.
//
// 🔴 WHY THE PARSE AND NOT GOOSE'S OWN REGISTRY, WHICH WOULD OTHERWISE BE THE
// BETTER AUTHORITY. The registry is unexported; the only way to read it is
// goose.CollectMigrations, and that call PANICS on the very input this check
// exists to report (Migrations.Less raises `goose: duplicate version N detected`
// while sorting). An enumeration that dies on a collision cannot report one — it
// takes the whole test binary with it, so every other test in the package
// disappears too. So the registry is used as CORROBORATION (gooseAgreesWithTheParse,
// which recovers that panic and turns it into a finding) and the parse is the
// enumeration.
//
// It is an AST walk and not a grep for a second reason: a comment or a string
// that mentions AddNamedMigrationContext is not a call expression, so this scan
// cannot match its own explanatory prose — a failure mode this repo has hit
// before. What it cannot see is a registration whose name is COMPUTED rather than
// literal; the corroboration below is what turns that into a stated finding
// instead of a silent omission.
func goMigrationClaims(t *testing.T) []migrationClaim {
	t.Helper()
	var claims []migrationClaim
	for v, where := range registrarLocations(t) {
		claims = append(claims, migrationClaim{version: v, source: migrationSourceGo, where: where})
	}
	sort.Slice(claims, func(i, j int) bool { return claims[i].version < claims[j].version })
	return claims
}

// gooseAgreesWithTheParse asks goose to collect the migrations it would actually
// run and reports what it says: either a duplicate it refused to sort past, or
// the set of Go-sourced versions it holds.
//
// The recover is the whole point. goose answers a collision by panicking from a
// sort comparator — loud, but at goose.Up time, on a station, as a stack trace.
// Catching it here turns the same fact into a finding in `go test`, next to the
// numbers, before the branch lands.
func gooseAgreesWithTheParse(t *testing.T) (goVersions map[int64]string, refusal string) {
	t.Helper()
	goose.SetBaseFS(embeddedMigrations) // the same base runMigrations sets
	sqlPaths := map[string]bool{}
	for _, c := range sqlMigrationClaims(t) {
		sqlPaths[c.where] = true
	}
	goVersions = map[int64]string{}
	defer func() {
		if r := recover(); r != nil {
			refusal = fmt.Sprint(r)
		}
	}()
	collected, err := goose.CollectMigrations("migrations", 0, math.MaxInt64)
	if err != nil {
		t.Fatalf("collect migrations: %v", err)
	}
	for _, m := range collected {
		if sqlPaths[m.Source] {
			continue // source ①, already enumerated
		}
		goVersions[m.Version] = m.Source
	}
	return goVersions, ""
}

// registrarLocations parses every non-test .go file in this package and returns
// version -> "file:line" for each goose registration it can read literally.
func registrarLocations(t *testing.T) map[int64]string {
	t.Helper()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read package dir: %v", err)
	}
	fset := token.NewFileSet()
	found := map[int64]string{}
	files := 0
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		files++
		f, err := parser.ParseFile(fset, name, nil, parser.ParseComments)
		if err != nil {
			t.Fatalf("parse %s: %v — a file this scan cannot read is a file it cannot "+
				"clear, so this is a failure and not a skip", name, err)
		}
		ast.Inspect(f, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || !strings.HasPrefix(sel.Sel.Name, "AddNamedMigration") || len(call.Args) == 0 {
				return true
			}
			lit, ok := call.Args[0].(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				return true // a computed name: the registry still sees it, this locator does not
			}
			nameArg, err := strconv.Unquote(lit.Value)
			if err != nil {
				return true
			}
			v, err := goose.NumericComponent(nameArg)
			if err != nil {
				return true
			}
			found[v] = fmt.Sprintf("%s:%d", name, fset.Position(call.Lparen).Line)
			return true
		})
	}
	// Anti-vacuity: a scan over an empty corpus finds nothing and looks exactly
	// like a clean tree.
	if files < 20 {
		t.Fatalf("the AST scan read %d files in this package — that corpus is too small to "+
			"be the real one, so a finding of zero registrations would mean nothing", files)
	}
	return found
}

// duplicateVersionFindings is the judgement itself, kept as a pure function over
// claims so it can be driven with a corpus that HAS a collision — the live tree
// is expected never to have one, and a detector only ever exercised on clean
// input is indistinguishable from a detector that returns nil.
func duplicateVersionFindings(claims []migrationClaim) []string {
	byVersion := map[int64][]migrationClaim{}
	for _, c := range claims {
		byVersion[c.version] = append(byVersion[c.version], c)
	}
	var versions []int64
	for v, cs := range byVersion {
		if len(cs) > 1 {
			versions = append(versions, v)
		}
	}
	sort.Slice(versions, func(i, j int) bool { return versions[i] < versions[j] })
	var findings []string
	for _, v := range versions {
		cs := byVersion[v]
		sort.Slice(cs, func(i, j int) bool {
			if cs[i].source != cs[j].source {
				return cs[i].source < cs[j].source
			}
			return cs[i].where < cs[j].where
		})
		var parts []string
		for _, c := range cs {
			parts = append(parts, fmt.Sprintf("%s in %s", c.source, c.where))
		}
		findings = append(findings, fmt.Sprintf(
			"migration version %d is claimed %d times: %s. Left alone this is discovered inside "+
				"goose.Up — measured on v3.27.2, Migrations.Less panics with `goose: duplicate "+
				"version %d detected` while sorting — so the first person to see it is whoever "+
				"restarts a station after the upgrade, and what they see is a stack trace about "+
				"two files they did not write. Take max+1 for one of them; a SKIPPED number is "+
				"loud in the right place (goose.Up refuses before applying anything) and is the "+
				"outcome this repo deliberately prefers",
			v, len(cs), strings.Join(parts, " AND "), v))
	}
	return findings
}

// TestMigrationVersionNumbersAreClaimedByExactlyOneSource is the check itself.
//
// Red when: two migrations claim one version number — whether both are SQL
// files, both are Go registrations, or (the direction this round created, and
// the one that survives until goose is already running) one of each.
func TestMigrationVersionNumbersAreClaimedByExactlyOneSource(t *testing.T) {
	sqlClaims := sqlMigrationClaims(t)
	goClaims := goMigrationClaims(t)
	located := registrarLocations(t)
	gooseGo, gooseRefusal := gooseAgreesWithTheParse(t)

	// 🔴 POSITIVE CONTROL — both enumerations must HIT something that is in this
	// tree today, or "no duplicates" would be a statement about an empty set.
	// The two named versions are landed migrations, and the whole point of the
	// numbering rule is that a landed number never moves.
	if !claimsContain(sqlClaims, 53) {
		t.Fatalf("the SQL enumeration did not find version 53 (00053_scheduled_message_custom_months.sql), "+
			"so it is not reading the migrations that ship. It found: %s", claimSummary(sqlClaims))
	}
	if !claimsContain(goClaims, 54) {
		t.Fatalf("the Go enumeration did not find version 54 (00054_reaim_custom_cursors.go, this "+
			"repo's first Go migration), so it is blind to exactly the source this check exists "+
			"for. It found: %s", claimSummary(goClaims))
	}
	if where := located[54]; !strings.Contains(where, "00054") {
		t.Fatalf("the AST locator placed version 54 at %q, want the file that registers it — "+
			"without a location the failure message sends the reader to grep migrations/, which "+
			"is the one place a Go migration is guaranteed not to be", where)
	}

	// 🔴 NEGATIVE CONTROL — a version neither source claims must come back
	// unclaimed. 38 is the permanent gap on origin/main (server/CLAUDE.md: PR #12
	// landed as 00040, so nobody will ever fill 00038) and 99999 has never
	// existed. If either shows up, the enumerations are answering "yes" to
	// everything and the duplicate check below is worthless.
	for _, absent := range []int64{38, 99999} {
		if claimsContain(sqlClaims, absent) || claimsContain(goClaims, absent) {
			t.Fatalf("version %d is claimed by one of the enumerations, but nothing in this repo "+
				"declares it — an enumeration that answers yes to an unused number cannot tell a "+
				"collision from a coincidence", absent)
		}
	}

	// 🔴 CORROBORATION — goose's own view of the same two sources. Two ways it can
	// disagree with the parse, and both are findings rather than things to smooth
	// over.
	if gooseRefusal != "" {
		t.Errorf("goose refuses to collect these migrations at all: %s\n\nThat refusal is raised "+
			"from a sort comparator inside goose.Up, which means without this test the first "+
			"person to see it is whoever restarts a station after the upgrade — a panic stack "+
			"trace about two files they did not write. Take max+1 for one of them; a skipped "+
			"number is the outcome this repo prefers", gooseRefusal)
	} else {
		for v, source := range gooseGo {
			if _, ok := located[v]; !ok {
				t.Errorf("goose holds a Go migration for version %d (%s) that the parse could not "+
					"find. Either it is registered through a computed name or the scan has gone "+
					"blind; either way a future collision on that number would be reported "+
					"without naming where to look", v, source)
			}
		}
		for _, c := range goClaims {
			if _, ok := gooseGo[c.version]; !ok {
				t.Errorf("the parse read a registration of version %d at %s that goose does not "+
					"hold. A claim goose will not honour is not a migration, and a claim it holds "+
					"under a different number is worse", c.version, c.where)
			}
		}
	}

	all := append(append([]migrationClaim{}, sqlClaims...), goClaims...)
	for _, finding := range duplicateVersionFindings(all) {
		t.Error(finding)
	}
}

// TestDuplicateVersionFindingsNamesBothSourcesAndTheNumber drives the judgement
// with a corpus that HAS a collision, so that the clean answer above is a
// measurement rather than a function that cannot say anything else.
func TestDuplicateVersionFindingsNamesBothSourcesAndTheNumber(t *testing.T) {
	// Disjoint: nothing to report. Without this arm a detector that always
	// reported would also pass the arm below.
	clean := []migrationClaim{
		{version: 53, source: migrationSourceSQL, where: "migrations/00053_a.sql"},
		{version: 54, source: migrationSourceGo, where: "00054_b.go"},
	}
	if got := duplicateVersionFindings(clean); len(got) != 0 {
		t.Fatalf("distinct versions reported %v, want nothing", got)
	}

	collide := []migrationClaim{
		{version: 53, source: migrationSourceSQL, where: "migrations/00053_a.sql"},
		{version: 54, source: migrationSourceSQL, where: "migrations/00054_late_merge.sql"},
		{version: 54, source: migrationSourceGo, where: "00054_reaim_custom_cursors.go"},
	}
	findings := duplicateVersionFindings(collide)
	if len(findings) != 1 {
		t.Fatalf("one collision produced %d findings: %v", len(findings), findings)
	}
	for _, want := range []string{
		"version 54",
		migrationSourceSQL,
		"migrations/00054_late_merge.sql",
		migrationSourceGo,
		"00054_reaim_custom_cursors.go",
	} {
		if !strings.Contains(findings[0], want) {
			t.Fatalf("the finding does not mention %q, so a reader cannot tell which two places "+
				"claim the number: %s", want, findings[0])
		}
	}
	if strings.Contains(findings[0], "version 53") {
		t.Fatalf("version 53 is claimed once and must not be accused: %s", findings[0])
	}
}

func claimsContain(claims []migrationClaim, version int64) bool {
	for _, c := range claims {
		if c.version == version {
			return true
		}
	}
	return false
}

func claimSummary(claims []migrationClaim) string {
	var out []string
	for _, c := range claims {
		out = append(out, path.Base(c.where))
	}
	sort.Strings(out)
	return strings.Join(out, ", ")
}
