#!/usr/bin/env bash
# bin/tests/main-red-notify-guard.sh — the notify-on-red job in
# .github/workflows/ci.yml is an ENUMERATION, and this is what stops the
# enumeration from going stale in silence.
#
# WHY THIS EXISTS (T-5d3b)
# The workflow's notify job depends on `needs: [<every other job>]`. GitHub has
# no wildcard for "all jobs", so adding a FOURTH job and forgetting this line
# does not fail anything: the new job's red simply stops being reported, and the
# workflow stays green-looking in exactly the way this feature exists to prevent.
# A comment asking people to remember is not a mechanism. This is.
#
# WHAT IT ASSERTS
#   1. the notify job exists, exactly once
#   2. its gate is EXACTLY `failure() && github.ref == 'refs/heads/main'`
#      — widened to always() it would message every pull request; widened by
#        dropping the ref test it would message every working branch
#   3. every OTHER job declared in the file appears in its `needs:` list
#   4. the webhook is reached through a repository secret, and this file carries
#      no URL literal anywhere (the URL is the credential: the inlet
#      authenticates on a token inside it, so a literal here is a public one)
#
# WHAT IT DOES NOT ASSERT — read this before trusting a green
#   It never sends anything and never talks to GitHub. It cannot tell you that
#   the secret is set on the repository, that the endpoint is alive, or that a
#   message arrived: the inlet answers an identical silent 200 to every request,
#   so DELIVERY IS ONLY OBSERVABLE AT THE RECEIVING END. This guard covers the
#   shipped CONDITION; the delivery half is verified by actually receiving a
#   message, and that evidence lives on the ticket, not here.
#
#   The job-name scan is line-shaped (two-space keys under `jobs:`), not a YAML
#   parse — deliberately, so the guard has no dependency that could be absent on
#   a runner and turn it into a skip. It therefore assumes this file keeps its
#   ordinary block layout: a job named with a quoted or flow-style key would be
#   missed. Every scan below self-checks against a positive control first, so a
#   scanner that has stopped matching reddens instead of reporting "clean".
set -uo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$HERE/../.." && pwd)"
WF="$REPO_ROOT/.github/workflows/ci.yml"
NOTIFY_JOB="notify-main-red"
SECRET_NAME="OC_MAIN_RED_WEBHOOK_URL"
WANT_IF="    if: failure() && github.ref == 'refs/heads/main'"

PASS=0; FAIL=0
ok()  { PASS=$((PASS+1)); printf '  ok   — %s\n' "$1"; }
bad() { FAIL=$((FAIL+1)); printf '  FAIL — %s\n' "$1"; }

echo "[main-red-notify-guard] $WF"

if [[ ! -f "$WF" ]]; then
  echo "  FAIL — workflow file not found: $WF" >&2
  echo "[main-red-notify-guard] 0 ok, 1 failed"
  exit 1
fi

# ── the jobs region ─────────────────────────────────────────────────────────
# Everything from the top-level `jobs:` key onwards. Restricting to this region
# is what makes the two-space rule mean "job name": `on:` and `concurrency:`
# above it also carry two-space keys (pull_request:, push:, group:).
REGION="$(sed -n '/^jobs:$/,$p' "$WF")"
if [[ -z "$REGION" ]]; then
  bad "no top-level 'jobs:' key found — the scanner is looking at the wrong shape, so nothing below proves anything"
  echo "[main-red-notify-guard] $PASS ok, $FAIL failed"
  exit 1
fi

JOBS="$(printf '%s\n' "$REGION" | grep -E '^  [a-z][a-z0-9-]*:$' | sed -E 's/^  ([a-z0-9-]+):$/\1/')"
JOB_COUNT="$(printf '%s\n' "$JOBS" | grep -c . || true)"

# Positive control for the scan itself. A file with a notify job and nothing to
# notify about is not a state worth shipping, and a scanner that matches zero
# job names would otherwise pass every check below by vacuity.
if [[ "$JOB_COUNT" -ge 2 ]]; then
  ok "job-name scan is alive ($JOB_COUNT jobs found)"
else
  bad "job-name scan found $JOB_COUNT jobs — the scan is broken (or the file has no gate jobs left); every check below would pass vacuously"
  echo "[main-red-notify-guard] $PASS ok, $FAIL failed"
  exit 1
fi

# ── 1. the notify job exists, exactly once ──────────────────────────────────
NOTIFY_DECLS="$(printf '%s\n' "$REGION" | grep -cE "^  ${NOTIFY_JOB}:$" || true)"
if [[ "$NOTIFY_DECLS" == "1" ]]; then
  ok "$NOTIFY_JOB is declared exactly once"
else
  bad "$NOTIFY_JOB is declared $NOTIFY_DECLS times (want exactly 1) — a red trunk would tell nobody, or tell them twice"
fi

# The notify job's own block: from its key to the next two-space key.
NOTIFY_BLOCK="$(printf '%s\n' "$REGION" | awk -v job="  ${NOTIFY_JOB}:" '
  $0 == job { inblock = 1; next }
  inblock && /^  [a-zA-Z0-9_-]+:$/ { inblock = 0 }
  inblock { print }
')"

# ── 2. the gate is exactly the shipped condition ────────────────────────────
if printf '%s\n' "$NOTIFY_BLOCK" | grep -qxF "$WANT_IF"; then
  ok "gate is exactly: ${WANT_IF#    }"
else
  bad "gate is not exactly \`${WANT_IF#    }\` — widened to always() it messages every pull request; without the ref test it messages every working branch. Found: $(printf '%s\n' "$NOTIFY_BLOCK" | grep -E '^    if:' || echo '(no if: line at all)')"
fi

# ── 3. every other job is in `needs:` ───────────────────────────────────────
NEEDS_LINE="$(printf '%s\n' "$NOTIFY_BLOCK" | grep -E '^    needs:' || true)"
if [[ -z "$NEEDS_LINE" ]]; then
  bad "$NOTIFY_JOB has no \`needs:\` line — it would run before the gates it is reporting on"
else
  MISSING=""
  COVERED=0
  while IFS= read -r job; do
    [[ -n "$job" ]] || continue
    [[ "$job" == "$NOTIFY_JOB" ]] && continue
    if printf '%s' "$NEEDS_LINE" | grep -qE "(\[|[[:space:],])${job}([],[:space:],]|$)"; then
      COVERED=$((COVERED+1))
    else
      MISSING="$MISSING $job"
    fi
  done <<< "$JOBS"

  if [[ "$COVERED" -eq 0 ]]; then
    bad "the needs-membership scan matched none of the $JOB_COUNT jobs — the scan is broken, so 'nothing missing' would be meaningless"
  elif [[ -n "$MISSING" ]]; then
    bad "these jobs are NOT in $NOTIFY_JOB's needs:$MISSING — their red would be reported to nobody"
  else
    ok "every other job ($COVERED) is in $NOTIFY_JOB's needs"
  fi
fi

# ── 4. the webhook is a secret, and there is no URL literal in the file ─────
if printf '%s\n' "$NOTIFY_BLOCK" | grep -qF "secrets.${SECRET_NAME}"; then
  ok "webhook is read from repository secret ${SECRET_NAME}"
else
  bad "$NOTIFY_JOB does not read secrets.${SECRET_NAME} — the destination has to come from a secret, because the URL is itself the credential"
fi

# Non-comment lines only: the commentary above legitimately discusses URLs. The
# pattern is checked against a synthetic positive control first, so a scheme
# regex that has stopped matching cannot report a clean file.
if printf 'x: https://example.invalid/in?t=tok\n' | grep -qE '://'; then
  URL_HITS="$(grep -nE '://' "$WF" | grep -vE '^[0-9]+:[[:space:]]*#' || true)"
  if [[ -z "$URL_HITS" ]]; then
    ok "no URL literal on any non-comment line"
  else
    bad "URL literal(s) on non-comment lines — a URL in this public file is a public credential: $URL_HITS"
  fi
else
  bad "the URL-literal pattern failed its own positive control — a clean result here would be meaningless"
fi

echo "[main-red-notify-guard] $PASS ok, $FAIL failed"
[[ "$FAIL" == "0" ]] || exit 1
exit 0
