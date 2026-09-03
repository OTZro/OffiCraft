package main

// migration_upgrade_path_t64_test.go — T-64: the check that a STATION ALREADY
// RUNNING can boot on this tree.
//
// 🔴 WHY THIS EXISTS, AND WHY ANOTHER ASSERTION WOULD NOT HAVE DONE.
// Three guards already stand on the migration path and all three are real:
// TestMigrationVersionNumbersAreClaimedByExactlyOneSource and
// TestDuplicateVersionFindingsNamesBothSourcesAndTheNumber ask whether one
// number is claimed twice; TestGooseUpOnFreshSQLite asks whether the set applies
// to a BRAND NEW database. On 2026-09-03 four packages were in flight each
// carrying a migration numbered BELOW main's released maximum, and all three
// guards were green on those trees (measured, not reasoned: a probe .sql at
// 00066 and a probe Go registration at 00067 were planted in turn and the three
// named tests were run — PASS every time), while the same binary run against a
// database already at version 69 died with
//
//	found 1 missing migrations before current version 69: version 66: …
//
// exit 1, the server never listening. The reason no assertion caught it is
// structural rather than an oversight: ON A BLANK DATABASE THE CONCEPT OF A
// MISSING MIGRATION DOES NOT EXIST. Twelve test files do build on a
// half-migrated database (goose.UpTo / goose.DownTo), but every one of them
// gets there by applying a CONTIGUOUS PREFIX from empty, and a prefix cannot
// have a hole. So what was absent was not a line of code — it was a POPULATION:
// no test anywhere owned a database that had been migrated by a DIFFERENT
// (earlier, released) migration set. This file builds that population.
//
// WHAT IT DOES.
//  1. Works out which versions main has already RELEASED — see releasedVersions
//     below for where that fact comes from and what it costs.
//  2. Replays exactly those onto a throwaway SQLite file: the result is a
//     database in the state of a station in the field.
//  3. Runs the PRODUCTION runMigrations against it. If a version below that
//     station's version is new in this tree, goose refuses, in its own words,
//     and this test hands that refusal back with the number to use instead.
//
// The error text is goose's, deliberately. A hand-written explanation of what
// goose does is a sentence that goes stale the next time goose changes; the
// tool's own refusal cannot.

import (
	"database/sql"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os/exec"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/pressly/goose/v3"
)

// treeMigrationVersions is every version this working tree declares, from BOTH
// sources — the embedded *.sql files and the Go migrations registered with
// goose.AddNamedMigration*. It reuses the enumerators the duplicate-version
// guard already owns rather than growing a second, quietly divergent scanner;
// "migrations/ only" is exactly the incomplete denominator that guard was
// written to kill.
func treeMigrationVersions(t *testing.T) map[int64]migrationClaim {
	t.Helper()
	out := map[int64]migrationClaim{}
	for _, c := range append(sqlMigrationClaims(t), goMigrationClaims(t)...) {
		out[c.version] = c
	}
	return out
}

// releasedVersions answers "which migration versions has main already shipped",
// computed FROM origin/main at the moment of the check.
//
// 🔴 WHY IT IS COMPUTED AND NOT CHECKED IN. The obvious alternative is a file in
// the tree listing the released numbers. It was rejected on Kyle's ruling
// (2026-09-03) for one reason: such a file goes stale silently. Whoever lands a
// migration has to remember to bump it, and when they forget nothing turns red —
// the guard keeps passing while the thing it guards has quietly moved. That is
// the exact failure shape this whole line of work exists to remove. A value read
// out of origin/main is a fact at the time it is read: a rebase re-derives it
// for free, and nobody has to remember anything.
//
// 🔴 WHAT IT COSTS, STATED PLAINLY. It needs a git repository that can resolve
// origin/main. Where that is not available the check cannot run, and this test
// SKIPS rather than passing — see the skip sites. A skip is not a green: read
// the reason.
func releasedVersions(t *testing.T) (versions map[int64]string, ref string) {
	t.Helper()
	sha, err := gitOut("rev-parse", "origin/main")
	if err != nil {
		t.Skipf("T-64 upgrade-path guard cannot run here: this checkout cannot resolve "+
			"origin/main (%v). It is not a pass — the guard simply had no baseline to "+
			"compare against. It runs in CI, which fetches main for this purpose.", err)
	}
	versions = map[int64]string{}
	// Source ① — the SQL files as main has them.
	files, err := gitOut("ls-tree", "--full-tree", "--name-only", "origin/main", "server/ocserverd/migrations/")
	if err != nil {
		t.Fatalf("list origin/main migrations: %v", err)
	}
	for _, f := range strings.Split(files, "\n") {
		f = strings.TrimSpace(f)
		if !strings.HasSuffix(f, ".sql") {
			continue
		}
		v, err := goose.NumericComponent(path.Base(f))
		if err != nil {
			t.Fatalf("%s on origin/main has no version prefix goose can read: %v", f, err)
		}
		versions[v] = f
	}
	// Source ② — the Go migrations as main has them, read by parsing main's own
	// copies of this package's files. A grep would match this file's prose; an
	// AST walk cannot match a comment.
	goFiles, err := gitOut("ls-tree", "--full-tree", "--name-only", "origin/main", "server/ocserverd/")
	if err != nil {
		t.Fatalf("list origin/main package files: %v", err)
	}
	parsed := 0
	for _, f := range strings.Split(goFiles, "\n") {
		f = strings.TrimSpace(f)
		if !strings.HasSuffix(f, ".go") || strings.HasSuffix(f, "_test.go") {
			continue
		}
		src, err := gitOut("show", "origin/main:"+f)
		if err != nil {
			t.Fatalf("read %s from origin/main: %v", f, err)
		}
		parsed++
		for v, where := range registrationsIn(t, filepath.Base(f), src) {
			versions[v] = where
		}
	}
	// Anti-vacuity: a scan that read nothing finds nothing and is indistinguishable
	// from a clean answer.
	if parsed < 20 {
		t.Fatalf("read only %d non-test .go files from origin/main — that corpus is too small "+
			"to be the real package, so a finding of zero Go migrations would mean nothing", parsed)
	}
	if len(versions) == 0 {
		t.Fatal("origin/main declares zero migrations — impossible, so the scan is wrong, not main")
	}
	return versions, sha
}

// registrationsIn parses one file's source and returns version -> "file:line"
// for every literal goose registration in it. Same shape as the duplicate-version
// guard's registrarLocations, but over SOURCE TEXT so it can be pointed at a file
// that only exists inside a git object.
func registrationsIn(t *testing.T, name, src string) map[int64]string {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, name, src, parser.ParseComments)
	if err != nil {
		t.Fatalf("parse %s: %v — a file this scan cannot read is a file it cannot clear", name, err)
	}
	found := map[int64]string{}
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
			return true
		}
		arg, err := strconv.Unquote(lit.Value)
		if err != nil {
			return true
		}
		v, err := goose.NumericComponent(arg)
		if err != nil {
			return true
		}
		found[v] = fmt.Sprintf("%s:%d", name, fset.Position(call.Lparen).Line)
		return true
	})
	return found
}

func gitOut(args ...string) (string, error) {
	out, err := exec.Command("git", args...).Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// releasedOnlyFS is the embedded migrations directory with the files this branch
// ADDS taken out of it — i.e. the migration directory as the released station saw
// it. Handing goose a narrowed FS is how the replay is kept honest: the station
// never had those files, so the replay must not either.
func releasedOnlyFS(t *testing.T, released map[int64]string) fs.FS {
	t.Helper()
	files, err := fs.Glob(embeddedMigrations, "migrations/*.sql")
	if err != nil {
		t.Fatalf("glob embedded migrations: %v", err)
	}
	m := fstest.MapFS{}
	kept := 0
	for _, f := range files {
		v, err := goose.NumericComponent(f)
		if err != nil {
			t.Fatalf("%s has no version prefix: %v", f, err)
		}
		if _, ok := released[v]; !ok {
			continue // added by this branch: the station never had it
		}
		b, err := fs.ReadFile(embeddedMigrations, f)
		if err != nil {
			t.Fatalf("read %s: %v", f, err)
		}
		m[f] = &fstest.MapFile{Data: b}
		kept++
	}
	if kept == 0 {
		t.Fatal("the released-only FS came out empty — the replay would prove nothing")
	}
	return m
}

// TestAStationAtTheReleasedVersionCanUpgradeToThisTree is the check.
//
// RED WHEN: this tree declares a migration whose version is BELOW the highest
// version main has released and which main does not have — the shape that makes
// every station in the field fail to boot after the merge.
//
// It must not run in parallel: it drives goose's package-level base FS, which
// runMigrations also sets.
func TestAStationAtTheReleasedVersionCanUpgradeToThisTree(t *testing.T) {
	tree := treeMigrationVersions(t)
	released, ref := releasedVersions(t)

	var releasedMax int64
	for v := range released {
		if v > releasedMax {
			releasedMax = v
		}
	}
	var treeMax int64
	for v := range tree {
		if v > treeMax {
			treeMax = v
		}
	}

	// A released migration that has vanished from this tree means the baseline and
	// the tree are not talking about the same thing, and every conclusion below
	// would be drawn on sand.
	for v, where := range released {
		if _, ok := tree[v]; !ok {
			t.Fatalf("origin/main (%s) ships migration version %d (%s) and this tree does not "+
				"declare it at all. A released migration cannot be withdrawn — every station "+
				"has already applied it — so this is either a deleted file or a branch that "+
				"needs rebasing onto main.", ref, v, where)
		}
	}

	// ── Finding ①: the arithmetic, over BOTH sources. ────────────────────────────
	// Reported with Errorf, not Fatalf, so the end-to-end replay below still runs
	// and goose gets to state the same fact in its own words.
	var offenders []int64
	for v := range tree {
		if _, ok := released[v]; !ok && v <= releasedMax {
			offenders = append(offenders, v)
		}
	}
	sort.Slice(offenders, func(i, j int) bool { return offenders[i] < offenders[j] })
	for _, v := range offenders {
		t.Errorf("migration version %d (%s) is NEW in this branch but sits at or below %d, the "+
			"highest version origin/main (%s) has released. Every station that has already "+
			"migrated past %d will refuse to boot after this lands: goose stops at a version it "+
			"skipped, before applying anything, and the server exits without listening. "+
			"FIX: renumber it to %d (one above the highest version anyone has), and never reuse "+
			"a skipped number — a hole is deliberate here, it is what makes the refusal loud.",
			v, tree[v].where, releasedMax, ref, releasedMax, treeMax+1)
	}

	// ── Finding ②: the population itself. ────────────────────────────────────────
	// Build a database in the state of a station in the field, then run the real
	// production migration path against it.
	db, err := openSQLite(filepath.Join(t.TempDir(), "released-station.db"))
	if err != nil {
		t.Fatalf("open throwaway db: %v", err)
	}
	defer db.Close()

	goose.SetBaseFS(releasedOnlyFS(t, released))
	if err := goose.SetDialect("sqlite3"); err != nil {
		t.Fatalf("set dialect: %v", err)
	}
	// UpTo(releasedMax) rather than Up(): the version window is the ONLY lever that
	// keeps a Go migration this branch adds out of the replay. Narrowing the FS does
	// not — goose merges its global Go registry in wholesale when the FS carries no
	// .go files (measured in goose v3.27.2, collectGoMigrations' else branch), and
	// Provider's WithExcludeVersions filters filesystem sources only.
	if err := goose.UpTo(db, "migrations", releasedMax); err != nil {
		t.Fatalf("replaying the released migration set failed: %v", err)
	}
	// A Go migration this branch adds BELOW releasedMax is inside that window, so it
	// was applied anyway. Take its bookkeeping row back out: the station never ran
	// it, and goose refuses on the bookkeeping before it touches any schema.
	for _, v := range offenders {
		if tree[v].source != migrationSourceGo {
			continue
		}
		if _, err := db.Exec(`DELETE FROM goose_db_version WHERE version_id = ?`, v); err != nil {
			t.Fatalf("un-apply Go migration %d from the replayed station: %v", v, err)
		}
	}

	if got := stationVersion(t, db); got != releasedMax {
		t.Fatalf("the replayed station sits at version %d, want %d (origin/main %s) — "+
			"the population is not what this test claims it is, so nothing below it counts",
			got, releasedMax, ref)
	}
	// The population must contain the Go migrations too, or the denominator has a
	// hole exactly where this repo's two Go migrations live.
	assertGoMigrationsAreInTheStation(t, db, released)

	// The upgrade itself: the SAME function main.go calls at boot, no test-only
	// re-implementation of it.
	if err := runMigrations(db); err != nil {
		t.Fatalf("a station already at version %d CANNOT boot on this tree.\n"+
			"goose's own refusal: %v\n"+
			"This is the boot path: cmdServe calls runMigrations before it listens, so the "+
			"station exits 1 and never comes up. FIX: renumber the offending migration(s) to "+
			"%d or higher (baseline read from origin/main %s).",
			releasedMax, err, treeMax+1, ref)
	}
}

// assertGoMigrationsAreInTheStation pins that the replayed population covers Go
// migrations and not just migrations/*.sql. Without it the whole construction
// could be blind to half the migration sources and still look correct.
func assertGoMigrationsAreInTheStation(t *testing.T, db *sql.DB, released map[int64]string) {
	t.Helper()
	var want []int64
	for v, where := range released {
		if strings.HasSuffix(where, ".go") || strings.Contains(where, ".go:") {
			want = append(want, v)
		}
	}
	if len(want) == 0 {
		t.Fatal("origin/main declares no Go migrations at all — this repo has had them since " +
			"00054, so the baseline scan is reading only migrations/*.sql and the denominator " +
			"is incomplete")
	}
	sort.Slice(want, func(i, j int) bool { return want[i] < want[j] })
	for _, v := range want {
		var n int
		if err := db.QueryRow(
			`SELECT COUNT(*) FROM goose_db_version WHERE version_id = ? AND is_applied = 1`, v,
		).Scan(&n); err != nil {
			t.Fatalf("read goose bookkeeping for %d: %v", v, err)
		}
		if n == 0 {
			t.Fatalf("Go migration %d (%s) is NOT applied in the replayed station. The replay "+
				"covers migrations/*.sql only, which is the incomplete denominator this repo "+
				"has already been bitten by once", v, released[v])
		}
	}
	t.Logf("replayed station carries the Go migrations too: %v", want)
}

func stationVersion(t *testing.T, db *sql.DB) int64 {
	t.Helper()
	var v sql.NullInt64
	if err := db.QueryRow(`SELECT MAX(version_id) FROM goose_db_version`).Scan(&v); err != nil {
		t.Fatalf("read station version: %v", err)
	}
	if !v.Valid {
		return 0
	}
	return v.Int64
}
