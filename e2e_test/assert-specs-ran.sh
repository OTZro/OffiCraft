#!/usr/bin/env bash
# e2e_test/assert-specs-ran.sh <run_all.log> — the fail-closed half of wiring this
# suite into an automatic gate.
#
# WHY THIS EXISTS
# `run_all.sh` exiting 0 answers "did anything fail?", not "did anything run?".
# Those come apart in exactly the ways a CI wiring breaks: the spec directory gets
# renamed, a grep filter matches nothing, playwright is invoked with a testDir
# that resolves elsewhere. In every one of those the gate goes GREEN while
# guarding nothing — signal absence wearing the costume of signal zero, which is
# the classic way a new gate dies without anybody noticing.
#
# So the gate asserts a FLOOR on the number of specs that actually reported, and
# separately asserts that the one spec we deliberately exclude did not sneak in.
#
# The floor is a floor, not the exact count, on purpose: an exact number goes
# stale the first time someone adds a spec, and a stale exact number teaches the
# next person that this file lies. What the floor has to catch is "0 ran" and
# "a handful ran because a filter swallowed the rest" — it does not need to know
# today's total. (Measured 2026-08-05: 24 specs collected, 23 after excluding the
# real-fleet one. The floor sits well under that so growth never reddens it.)
set -euo pipefail

LOG="${1:?usage: assert-specs-ran.sh <run_all.log>}"
FLOOR=15
EXCLUDED_SPEC_MARKER='machine onboarding'

if [ ! -s "$LOG" ]; then
  echo "[assert-specs-ran] FATAL: '$LOG' is missing or empty — the e2e step produced no output at all." >&2
  exit 1
fi

# playwright's list reporter prints a tally line such as "  23 passed (24.9s)".
PASSED=$(grep -Eo '[0-9]+ passed' "$LOG" | tail -n 1 | grep -Eo '[0-9]+' || true)

if [ -z "${PASSED:-}" ]; then
  echo "[assert-specs-ran] FATAL: no 'N passed' tally in '$LOG' — playwright never reported a result." >&2
  echo "[assert-specs-ran] A green rc with no tally means the specs did not run. Refusing to call that a pass." >&2
  exit 1
fi

if [ "$PASSED" -lt "$FLOOR" ]; then
  echo "[assert-specs-ran] FATAL: only $PASSED spec(s) passed, floor is $FLOOR." >&2
  echo "[assert-specs-ran] Something swallowed the suite (renamed dir, over-broad filter, wrong testDir)." >&2
  exit 1
fi

# The real-fleet spec must stay out: it needs `claude` on PATH, spawns a real
# warden and burns real API quota. If it ran here, the exclusion mechanism broke
# and the bill is the symptom nobody sees in a log.
if grep -qF "$EXCLUDED_SPEC_MARKER" "$LOG"; then
  echo "[assert-specs-ran] FATAL: the real-fleet spec ('$EXCLUDED_SPEC_MARKER') executed." >&2
  echo "[assert-specs-ran] OC_E2E_EXCLUDE_REAL_FLEET was supposed to keep it out of this run." >&2
  exit 1
fi

echo "[assert-specs-ran] ok — $PASSED specs passed (floor $FLOOR), real-fleet spec excluded"
