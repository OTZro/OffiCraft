#!/usr/bin/env bash
# Proves every `go test` CI runs is an ACTUAL execution, never a cache replay.
#
# ── the bug this guard exists to stop from coming back (T-bedc) ───────────────
# `bin/ci.sh` step 1e ran a bare `"$GO" test ./...`. go caches successful test
# results keyed on the package's inputs, so on any run whose inputs hash the same
# as a previous PASS it prints, verbatim from a real CI log:
#
#     ok  	ocwarden	(cached)
#
# and the run goes green having executed nothing. That is fatal for the land
# authority in two independent ways:
#   * the cache key covers package INPUTS, not the world the tests touch (ports,
#     clocks, launchd, the host fleet, the staged embed assets' effects), so a
#     package that would fail TODAY still reports ok;
#   * it structurally HIDES FLAKES — a suite is executed only on the first commit
#     that changes its inputs, so an intermittent failure is amortised to
#     near-zero observed probability and never gets investigated.
# `-count=1` is go's documented cache defeat. This guard pins it on EVERY go test
# call site in the repo's shell scripts, so removing the flag reddens CI.
#
# ── WHAT THIS GUARD DOES *NOT* COVER (read before trusting it) ────────────────
#  1. NON-SHELL DISPATCHERS. The scan set is tracked SHELL scripts (by .sh
#     extension or sh/bash shebang). A `go test` launched from python, node, a
#     Makefile, or a Go program is invisible to it. As of T-bedc there is no such
#     dispatcher in the CI tree (the only go test call site anywhere is
#     bin/ci.sh's step 1e) — the "call sites found" tally below is printed so a
#     newly-added one is at least visible in the log.
#  2. UNTRACKED SCRIPTS. The set comes from `git ls-files`, so a dispatched but
#     untracked script is not scanned.
#  3. FRAGMENT ASSEMBLY / INDIRECTION. `SUB=test; go $SUB ./...`, or a wrapper
#     function, hides the call site from any static scan. Limit of static
#     matching, same as bin/tests/ci-success-marker.sh's residual 1.
#  4. GOFLAGS / GOCACHE TRICKERY. Someone who sets `GOFLAGS=-count=0` or points
#     GOCACHE at a warm shared dir defeats the intent while keeping the flag.
#     Not statically detectable; nothing here looks at the environment.
#  5. THE OTHER CACHES. `go build`/`go vet` also cache, deliberately and
#     harmlessly: their cache is content-addressed over the compilation itself,
#     so a hit is equivalent to a miss. Only TEST RESULT caching claims a
#     behaviour was observed when it was not, so only `test` is gated.
#  6. NON-GO SUITES. vitest / playwright / pytest have their own caching stories.
#     Out of scope for this file.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
CI="$ROOT/bin/ci.sh"
PASS=0
FAIL=0
ok() { PASS=$((PASS + 1)); printf '  ok   — %s\n' "$1"; }
bad() { FAIL=$((FAIL + 1)); printf '  FAIL — %s\n' "$1" >&2; }

# ── the parser ───────────────────────────────────────────────────────────────
# Why a real parser instead of `grep -F 'go test'`: this file, bin/ci.sh, and
# bin/tests/ci-success-marker.sh all MENTION `go test` in prose, in echo strings
# and in log fixtures. An unanchored substring grep matches those and is a check
# with no discriminating power — the exact always-true shape this repo keeps
# relearning. So decide by COMMAND POSITION: split each line into shell segments
# on ; && || | , strip a leading ( or {, skip env-assignment / `env` prefixes,
# and only then ask whether the command word is go and its first non-flag
# argument is `test`. An `echo "... go test ..."` has command word `echo` and is
# correctly not a call site; a comment line is dropped before any of this.
#
# Emits one TSV record per call site: <file>\t<lineno>\t<has_count1: 1|0>\t<line>
go_test_call_sites() { # go_test_call_sites FILE  (relative label = $2, default $1)
  local file="$1" label="${2:-$1}"
  awk -v LABEL="$label" '
    # drop comment-only lines
    /^[[:space:]]*#/ { next }
    {
      raw = $0
      # segment the line on shell command separators
      n = split(raw, segs, /(&&|\|\||;|\|)/)
      for (i = 1; i <= n; i++) {
        seg = segs[i]
        # strip leading subshell/group openers and whitespace
        gsub(/^[[:space:]]*[({][[:space:]]*/, "", seg)
        sub(/^[[:space:]]+/, "", seg)
        if (seg == "") continue
        # tokenise on whitespace
        m = split(seg, tok, /[[:space:]]+/)
        j = 1
        # skip `env` and VAR=value prefixes (e.g. GOTOOLCHAIN=auto PATH=... go test)
        while (j <= m) {
          t = tok[j]
          if (t == "env") { j++; continue }
          if (t ~ /^[A-Za-z_][A-Za-z0-9_]*=/) { j++; continue }
          break
        }
        if (j > m) continue
        cmd = tok[j]
        gsub(/["'"'"']/, "", cmd)          # "$GO" -> $GO
        # is the command word the go toolchain?
        isgo = 0
        if (cmd == "go" || cmd == "$GO" || cmd == "${GO}") isgo = 1
        else if (cmd ~ /\/go$/) isgo = 1   # /opt/homebrew/bin/go, $(dirname x)/go
        if (!isgo) continue
        # first non-flag argument is the subcommand
        sub_cmd = ""
        for (k = j + 1; k <= m; k++) {
          a = tok[k]
          gsub(/["'"'"']/, "", a)
          if (a == "") continue
          if (a ~ /^-/) continue
          sub_cmd = a
          break
        }
        if (sub_cmd != "test") continue
        # does THIS segment carry the cache defeat?
        has = 0
        for (k = j + 1; k <= m; k++) {
          a = tok[k]
          gsub(/["'"'"']/, "", a)
          if (a == "-count=1") has = 1
        }
        printf "%s\t%d\t%d\t%s\n", LABEL, NR, has, seg
      }
    }
  ' "$file"
}

# The scan set: tracked shell scripts (extension or shebang). THIS file is NOT
# excluded — the parser is precise enough that its own prose and fixtures sit in
# non-command position, which is the point of parsing rather than grepping.
shell_sources() {
  local f shebang
  (cd "$ROOT" && git ls-files 2>/dev/null || find . -type f | sed 's|^\./||') \
  | while IFS= read -r f; do
      [[ -f "$ROOT/$f" ]] || continue
      case "$f" in
        *.sh) printf '%s\n' "$f"; continue ;;
      esac
      IFS= read -r shebang < "$ROOT/$f" 2>/dev/null || true
      case "$shebang" in
        '#!'*bash*|'#!'*/sh|'#!'*/sh\ *) printf '%s\n' "$f" ;;
      esac
    done
}

echo "go test cache-defeat contract tests"

WORK="$(mktemp -d -t oc-gotest-nocache.XXXXXX)"
trap 'rm -rf "$WORK"' EXIT

# ── parser red/green controls, FIRST ─────────────────────────────────────────
# Before any repo-wide claim, prove the parser can both FIND a call site and
# JUDGE it. Without these the "no offenders" assertion below could be satisfied
# by a parser that finds nothing at all — the vacuous-empty-set shape.
# Fixtures are written through printf so the literals never occupy command
# position in THIS file.
printf '%s\n' '#!/usr/bin/env bash' 'go test ./...' > "$WORK/bare.sh"
printf '%s\n' '#!/usr/bin/env bash' 'go test -count=1 ./...' > "$WORK/flagged.sh"
printf '%s\n' '#!/usr/bin/env bash' \
  '# a comment about go test ./... that must not count' \
  'echo "[ci] (1/5) golang — gofmt + go vet + go test (cli/* + server/*)"' \
  'printf "%s\n" "FAIL — go test"' \
  '(cd x && go build -o y ./...)' > "$WORK/decoys.sh"

BARE="$(go_test_call_sites "$WORK/bare.sh")"
if [[ "$(printf '%s\n' "$BARE" | grep -c .)" == "1" ]] && [[ "$BARE" == *$'\t'0$'\t'* ]]; then
  ok "parser finds a bare 'go test' call site and marks it UNFLAGGED"
else
  bad "parser finds a bare 'go test' call site and marks it UNFLAGGED (got: ${BARE:-<none>})"
fi

FLAGGED="$(go_test_call_sites "$WORK/flagged.sh")"
if [[ "$(printf '%s\n' "$FLAGGED" | grep -c .)" == "1" ]] && [[ "$FLAGGED" == *$'\t'1$'\t'* ]]; then
  ok "parser finds a 'go test -count=1' call site and marks it FLAGGED"
else
  bad "parser finds a 'go test -count=1' call site and marks it FLAGGED (got: ${FLAGGED:-<none>})"
fi

DECOYS="$(go_test_call_sites "$WORK/decoys.sh" || true)"
if [[ -z "$DECOYS" ]]; then
  ok "comments, echo/printf strings and 'go build' are NOT counted as call sites"
else
  bad "comments, echo/printf strings and 'go build' are NOT counted as call sites (got: $DECOYS)"
fi

# ── the live repo-wide scan ──────────────────────────────────────────────────
SCAN_SET="$(shell_sources)"
SCANNED="$(printf '%s\n' "$SCAN_SET" | grep -c . || true)"
ALL_SITES="$(while IFS= read -r f; do
  [[ -n "$f" ]] || continue
  go_test_call_sites "$ROOT/$f" "$f"
done <<< "$SCAN_SET")"
SITE_COUNT="$(printf '%s\n' "$ALL_SITES" | grep -c . || true)"

# The enumeration must be NON-EMPTY. An empty scan would make the offender check
# below permanently, vacuously green — that is the second always-true pathology
# this repo keeps hitting (assert the set is empty, then loop over the set).
if [[ "$SITE_COUNT" -ge 1 ]]; then
  ok "scan found $SITE_COUNT go test call site(s) across $SCANNED shell script(s) — enumeration is not vacuous"
else
  bad "scan found NO go test call site across $SCANNED shell script(s) — the offender check below would be vacuously green"
fi

# …and specifically it must contain the one that matters: ci.sh's step 1e. A
# rename/refactor that moves the gate elsewhere breaks this and demands a look.
if printf '%s\n' "$ALL_SITES" | grep -q '^bin/ci\.sh	'; then
  ok "scan covers bin/ci.sh (the CI go-test gate, step 1e)"
else
  bad "scan covers bin/ci.sh (the CI go-test gate, step 1e) — sites seen: $(printf '%s\n' "$ALL_SITES" | cut -f1 | sort -u | tr '\n' ' ')"
fi

OFFENDERS="$(printf '%s\n' "$ALL_SITES" | awk -F'\t' 'NF && $3 == 0 { printf "%s:%s ", $1, $2 }')"
if [[ -z "$OFFENDERS" ]]; then
  ok "every go test call site passes -count=1 (no run can be served from go's test cache)"
else
  bad "go test WITHOUT -count=1 — these can report 'ok <pkg> (cached)' and certify a run that never executed: $OFFENDERS"
fi

# ── mutant: strip the flag from the REAL ci.sh ────────────────────────────────
# One mutant, one change: delete ` -count=1` from bin/ci.sh and nothing else.
# The scan must turn that file into an offender. This is the reverse direction of
# the sentinel below, and it is what makes the green above mean something.
sed 's/ -count=1//' "$CI" > "$WORK/mutant-ci.sh"
if ! diff -q "$CI" "$WORK/mutant-ci.sh" >/dev/null; then
  ok "mutant generator actually changed bin/ci.sh (the flag was there to remove)"
else
  bad "mutant generator changed NOTHING — bin/ci.sh carries no -count=1 to strip, so the mutant proves nothing"
fi
MUTANT_SITES="$(go_test_call_sites "$WORK/mutant-ci.sh" "mutant-ci.sh")"
MUTANT_OFFENDERS="$(printf '%s\n' "$MUTANT_SITES" | awk -F'\t' 'NF && $3 == 0 { printf "%s:%s ", $1, $2 }')"
if [[ -n "$MUTANT_OFFENDERS" ]]; then
  ok "mutant — bin/ci.sh with -count=1 removed is CAUGHT ($MUTANT_OFFENDERS)"
else
  bad "mutant — bin/ci.sh with -count=1 removed is NOT caught; this guard cannot detect the regression it exists for"
fi

# ── sentinel: the real tree must PASS ────────────────────────────────────────
# A guard that is red on a healthy tree gets deleted, not fixed. The real ci.sh
# — the thing CI actually runs — must be clean under the same checker.
CI_SITES="$(go_test_call_sites "$CI" "bin/ci.sh")"
CI_OFFENDERS="$(printf '%s\n' "$CI_SITES" | awk -F'\t' 'NF && $3 == 0 { printf "%s:%s ", $1, $2 }')"
if [[ -z "$CI_OFFENDERS" && -n "$CI_SITES" ]]; then
  ok "sentinel — the real bin/ci.sh has go test call site(s) and ALL carry -count=1"
else
  bad "sentinel — the real bin/ci.sh must have go test call site(s), all flagged (sites: ${CI_SITES:-<none>} offenders: ${CI_OFFENDERS:-<none>})"
fi

echo "go test cache-defeat contract tests: $PASS ok, $FAIL failed"
[[ "$FAIL" == "0" ]]
