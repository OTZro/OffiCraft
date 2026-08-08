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
# separately asserts that the default-OFF live-agent class did not sneak in.
#
# The floor is a floor, not the exact count, on purpose: an exact number goes
# stale the first time someone adds a spec, and a stale exact number teaches the
# next person that this file lies. What the floor has to catch is "0 ran" and
# "a handful ran because a filter swallowed the rest" — it does not need to know
# today's total. (Measured 2026-08-05 with `playwright test --list`: 23 collected
# by default, 24 once the live-agent class is requested. The floor sits well under
# that so growth never reddens it.)
#
# WHAT IT DOES NOT ASSERT — the scope of the claim
# This is TEXT MATCHING over a log. What it asserts is that "the reporter SAID N
# specs passed", NOT that N specs really ran. Anything that emits a line shaped
# like a reporter tally satisfies it: a hand-written file, a log replayed from an
# earlier run, output from a different tree. Its credibility is exactly the
# credibility of the log it is handed and no higher — so it closes the "the gate
# was wired to nothing" hole above, and closes nothing about a log that lies.
# When its output is quoted as land evidence, quote it at that width: "the
# reporter reported this", plus wherever the log came from.
#
# WHO EXERCISES A CHANGE TO THIS FILE
# Nothing local. `bin/ci.sh` never reaches this script — its only caller anywhere
# is the `macos-e2e` job in .github/workflows/ci.yml. Do not take that on this
# file's word; the callers are a query, and the answer moves:
#   git grep -nF assert-specs-ran.sh
# What matters is which hits are INVOCATIONS rather than prose — a mention in a
# comment is not a caller. (This sentence used to assert a hit count under bin/
# and tests_guard/ instead, and a comment added in the very same commit falsified
# it on the spot.) So for an edit HERE the land authority going green really
# is no evidence — that run never executed this file. Acceptance is the
# `macos-e2e` job on the PR and its log.
#
# NOTE the asymmetry, it is not the same for its neighbour: a change to
# `run_all.sh` IS partly covered locally, because tests_guard executes that file
# in a throwaway tree and pins its wiring shape. See that file's own header.
set -euo pipefail

LOG="${1:?usage: assert-specs-ran.sh <run_all.log>}"
FLOOR=15
# Matches the CLASS by the filename suffix that puts a spec in it, not the title
# of the one spec that happens to exist today (T-c329). A title is prose: it gets
# reworded, and the guard then watches for a string nobody writes any more while
# reporting nothing wrong. The suffix is the same predicate playwright.config.js
# ignores on, so the guard and the config can never disagree about who is in the
# class. Verified against a real list-reporter log: every reported spec line
# carries its filename, so this substring really does appear when such a spec runs.
LIVE_AGENT_MARKER='.live-agent.spec.js'

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

# The live-agent class must stay out UNLESS this run explicitly asked for it.
# Those specs need `claude` on PATH, spawn a real agent and burn real API quota —
# and the bill is the symptom nobody sees in a log.
#
# The condition matters as much as the check (T-c329): the caller who DELIBERATELY
# opted in with OC_E2E_LIVE_AGENT=1 must not be met by a guard calling their own
# choice a failure. A guard that reddens on the one path it was never meant to
# police is a guard people learn to ignore — and one that cites a flag the caller
# never set is simply lying to them.
if [ "${OC_E2E_LIVE_AGENT:-}" = "1" ]; then
  echo "[assert-specs-ran] ok — $PASSED specs passed (floor $FLOOR) as REPORTED by the playwright reporter (text matching over $LOG, not proof they ran); the live-agent class was explicitly requested, so its specs belong in this log"
  exit 0
fi

if grep -qF "$LIVE_AGENT_MARKER" "$LOG"; then
  echo "[assert-specs-ran] FATAL: a spec of the live-agent class ('*$LIVE_AGENT_MARKER') executed without being asked for." >&2
  echo "[assert-specs-ran] That class is default-OFF and only runs with OC_E2E_LIVE_AGENT=1, which this run did not set." >&2
  echo "[assert-specs-ran] It spawns a real agent and spends real API quota, so treat this as a broken default, not a flake." >&2
  exit 1
fi

echo "[assert-specs-ran] ok — $PASSED specs passed (floor $FLOOR) as REPORTED by the playwright reporter (text matching over $LOG, not proof they ran), live-agent class stayed out"
