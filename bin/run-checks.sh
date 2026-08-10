#!/usr/bin/env bash
# Run named Makefile check targets AND prove each of them reached its own end.
#
# ── WHY THIS EXISTS (T-4d88) ─────────────────────────────────────────────────
# A ZERO EXIT SAYS "NOTHING FAILED", NOT "SOMETHING RAN". A make target whose
# recipe is emptied — deleted in an edit, commented out, cut short by an early
# `exit 0` — succeeds instantly and silently. rc cannot tell that apart from a
# check that ran and passed, and neither can a human reading a log for a line
# that is simply absent.
#
# This protection is not new here. Before T-4d88 the cloud macOS cell piped its
# script through `tee` and then grepped for that script's final marker, for
# exactly this reason. T-4d88 deleted the script; the marker went with it and the
# protection lost its home. This file is where it lives now.
#
# ── HOW ──────────────────────────────────────────────────────────────────────
# Every check target in the Makefile ends by printing its OWN end marker:
#
#     [oc-check-done] <target>
#
# printed as the LAST clause of the recipe's single shell command, so anything
# that stops the recipe early — a failure, an early exit, a deleted body — takes
# the marker with it. This wrapper runs `make <targets>`, tees the output, and
# then requires the marker of EVERY target it was asked for. A missing one is a
# non-zero exit naming the target, not a warning.
#
# ── WHY THE CALLER PASSES THE TARGETS ONCE, AND ONLY ITS OWN ─────────────────
# The expected markers are DERIVED from this invocation's own arguments. There is
# deliberately no list of "all the checks" anywhere in here or in the workflow:
# such a list is a second enumeration of what CI runs, it drifts from the real
# one, and killing that duplication is the whole of T-4d88. A caller declares
# which checks it owns exactly once — in the command it runs — and is held to
# precisely that set, not to anybody else's.
#
# ⚠️ WHAT THIS IS NOT: it is NOT a check that the cloud cells collectively cover
# every target in the Makefile. That is a different (and deliberately absent)
# assertion. This one answers only: "did THIS invocation's checks each run to
# their own end?"
#
# ── WHAT IT DOES NOT COVER ───────────────────────────────────────────────────
#  1. A recipe gutted with the marker line LEFT BEHIND still prints it. The
#     marker proves the recipe reached its end, never that the end was worth
#     reaching. It is the same class of statement as the suite markers this repo
#     already relies on.
#  2. Prerequisite targets pulled in by make are not asserted — only the targets
#     named on the command line. A caller asserts what it asked for.
#  3. Nothing here forces a caller to use this wrapper instead of bare `make`.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

if [[ $# -eq 0 ]]; then
  echo "FAIL — bin/run-checks.sh needs at least one Makefile target: an empty round would assert nothing." >&2
  exit 2
fi

LOG="$(mktemp -t oc-run-checks.XXXXXX)"
trap 'rm -f "$LOG"' EXIT

set +e
make -C "$ROOT" "$@" 2>&1 | tee "$LOG"
rc="${PIPESTATUS[0]}"
set -e
[[ "$rc" == "0" ]] || exit "$rc"

missing=()
for target in "$@"; do
  grep -qFx "[oc-check-done] $target" "$LOG" || missing+=("$target")
done

if [[ ${#missing[@]} -gt 0 ]]; then
  echo "FAIL — make exited 0 but these checks never reached their own end marker: ${missing[*]}" >&2
  echo "A target whose recipe was emptied, commented out or cut short by an early exit succeeds" >&2
  echo "instantly and silently. Each check prints '[oc-check-done] <target>' as the last clause of" >&2
  echo "its recipe; a missing one means that check did NOT run to completion, whatever rc says." >&2
  exit 1
fi

echo "[run-checks] all $# check(s) reported their own end marker: $*"
