#!/usr/bin/env bash
# bin/tests/namespace-mirror-guard.sh — the BASH half of the cross-module
# namespace mirror confrontation (T-5047).
#
# WHAT IS BEING GUARDED
# ---------------------
# The namespace→(root, launchd label) derivation is written out by hand FOUR
# times, in three languages, across two Go modules and two shell scripts:
#
#   cli/ocwarden/namespace.go       ← guarded by namespace_mirror_test.go
#   server/ocserverd/onboarding.go  ← guarded by onboarding_mirror_test.go
#   bin/install.sh                  ← guarded HERE
#   bin/ocserver                    ← guarded HERE
#
# Everything is checked against ONE shared table, fixtures/namespace-axes.tsv, so
# a drift names the copy that drifted. Checking the copies against each other
# would only ever report that they differ.
#
# WHY THE SHELL COPIES GET A DIFFERENT TREATMENT
# ----------------------------------------------
# The Go copies are FUNCTIONS, so their tests call them and compare results. The
# shell copies are two variable assignments in the middle of a 1200-line
# installer, and the install/uninstall guard suites already exercise them
# END TO END for one namespace (see install-guard.sh §10 / uninstall-guard.sh).
# What those suites cannot see is the CHARSET: it is a regex literal, not a
# derivation, and a copy that is looser than the others admits a namespace the
# other components will refuse — one component builds a path or a label the
# others do not recognise, which is precisely the split-brain this ticket exists
# to remove. So this guard pins the two shell derivations structurally and the
# charset in all four copies, and leaves behaviour to the suites that run the
# script.
set -uo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT="$(cd "$HERE/../.." && pwd)"
TABLE="$HERE/fixtures/namespace-axes.tsv"
[[ -f "$TABLE" ]] || { echo "FATAL: shared namespace table not found at $TABLE" >&2; exit 2; }

PASS=0; FAIL=0
ok()   { PASS=$((PASS+1)); printf '  ok   — %s\n' "$1"; }
bad()  { FAIL=$((FAIL+1)); printf '  FAIL — %s\n' "$1"; }

echo "namespace mirror — four hand-transcribed copies against one table"

# ── the charset, in all FOUR copies ─────────────────────────────────────────
CHARSET="$(sed -n 's/^# charset	//p' "$TABLE" | head -1)"
if [[ -z "$CHARSET" ]]; then
  echo "FATAL: $TABLE carries no '# charset<TAB><regex>' line — the charset is unpinned" >&2
  exit 2
fi
ok "shared table declares the charset: $CHARSET"

# The Go copies verify the regex by VALUE in their own tests (they can call
# namespaceShape.String()); here we can only check the source text, which is why
# the Go side is the authoritative check and this is the shell-reachable one.
for f in cli/ocwarden/namespace.go server/ocserverd/config.go bin/install.sh bin/ocserver; do
  if grep -qF -- "$CHARSET" "$ROOT/$f"; then
    ok "$f carries the shared charset literally"
  else
    bad "$f does NOT contain '$CHARSET' — a namespace one component accepts and another rejects is a split-brain install"
  fi
done

# ── the two shell derivations ───────────────────────────────────────────────
# Structural, not behavioural: both scripts must derive root and label from the
# SAME namespace variable rather than from two independently-set knobs. The end-
# to-end behaviour for a concrete namespace is covered by install-guard.sh §10
# and uninstall-guard.sh; what a grep can add is that the derivation exists at
# all and is keyed off one variable — the property that makes the axes unable to
# disagree.
# check_derivation <label-for-humans> <file> <pattern> <count> <consequence>
# COUNT is asserted, not just presence: install and uninstall each need their own
# derivation, and "one of the two is left" is the one-way-door failure.
check_derivation() {
  local what="$1" f="$2" pat="$3" want="$4" why="$5"
  local n
  n="$(grep -cE -- "$pat" "$ROOT/$f")"
  if [[ "$n" == "$want" ]]; then
    ok "$f: $what derived from the namespace key ($n site(s))"
  else
    bad "$f: $what — found $n site(s) matching /$pat/, want $want. $why"
  fi
}

# install.sh: the install path uses NS_DASH/NS_DOT, the uninstall path
# ns_dash/ns_dot. Removal must be the exact inverse of install or --namespace is
# a one-way door, so BOTH halves are required.
check_derivation "install-path root"  bin/install.sh '\.officraft\$NS_DASH' 1 \
  "A namespaced install would write into the MAIN instance's ~/.officraft."
check_derivation "install-path label" bin/install.sh 'com\.officraft\.serve\$NS_DOT' 1 \
  "A namespaced install would boot out the MAIN instance's launchd job."
check_derivation "uninstall-path root"  bin/install.sh '\.officraft\$ns_dash' 1 \
  "--uninstall --namespace would remove the MAIN instance instead."
check_derivation "uninstall-path label" bin/install.sh 'com\.officraft\.serve\$ns_dot' 1 \
  "--uninstall --namespace would boot out the MAIN instance's job."

# bin/ocserver derives the label from SERVE_LABEL_BASE rather than the literal,
# and carries the same pair TWICE (install + uninstall) — the same inverse
# requirement as install.sh, which is why the count is 2 and not "at least one".
check_derivation "root (install+uninstall)"  bin/ocserver '\.officraft\$NS_DASH/server' 2 \
  "From-source installs would collide in the MAIN instance's root, or become unremovable."
check_derivation "label (install+uninstall)" bin/ocserver 'SERVE_LABEL_BASE\$NS_DOT' 2 \
  "From-source installs would fight the MAIN instance for its launchd label."
# …and the base the suffix is applied to must still be the shared literal, or the
# check above would pass while pointing at some other job entirely.
if grep -qE '^readonly SERVE_LABEL_BASE="com\.officraft\.serve"$' "$ROOT/bin/ocserver"; then
  ok "bin/ocserver: SERVE_LABEL_BASE is still com.officraft.serve"
else
  bad "bin/ocserver: SERVE_LABEL_BASE is no longer com.officraft.serve — the namespaced label now suffixes something else"
fi

# ── the table itself must still contain the two rows that matter ────────────
# A table that lost its empty-namespace row would let every check above pass
# while the "main instance is byte-identical" claim went unverified.
if grep -qE '^<empty>	<empty>	com\.officraft\.ocwarden	officraft$' "$TABLE"; then
  ok "the table still pins the EMPTY namespace to the historical literals"
else
  bad "the table lost its empty-namespace row — the main-instance claim is unverified everywhere"
fi
if [[ "$(grep -cvE '^[[:space:]]*(#.*)?$' "$TABLE")" -ge 2 ]]; then
  ok "the table carries at least one namespaced row as well"
else
  bad "the table has no namespaced row — nothing proves the suffixing works"
fi

echo "namespace-mirror tests: $PASS ok, $FAIL failed"
[[ "$FAIL" == "0" ]] || exit 1
exit 0
