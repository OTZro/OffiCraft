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
#   0. the workflow file PARSES as YAML. Not a formality: this ticket shipped a
#      ci.yml whose `run:` block scalar was ended early by a continuation line
#      at column 1. Every local gate and every guard here passed it, because
#      nothing local had ever parsed the file; GitHub answered with a startup
#      failure in which zero jobs ran and the pull request carried zero checks.
#   1. the notify job exists, exactly once
#   2. its gate is `failure() && github.ref == 'refs/heads/main'`
#      — widened to always() it would message every pull request; widened by
#        dropping the ref test it would message every working branch.
#      An outer `${{ }}` wrapper is normalised away first (both spellings are
#      legal GitHub); everything inside is compared verbatim.
#   3. every OTHER job declared in the file appears in its `needs:` list
#   4. the webhook is reached through a repository secret, and this file carries
#      no URL literal outside comments (the URL is the credential: the inlet
#      authenticates on a token inside it, so a literal here is a public one)
#
#   ⚠️ HONESTY ABOUT 4's SECOND HALF. "no URL literal" was ALREADY TRUE before
#   this ticket — the parent commit's ci.yml has no `://` outside comments. It
#   is a PRESERVING invariant (a regression fence for the future), not a
#   property T-5d3b established. Assertions 1, 3 and the first half of 4 are the
#   ones that were false before the change and are true after it.
#
# WHAT IT DOES NOT ASSERT — read this before trusting a green
#   It never sends anything and never talks to GitHub. It cannot tell you that
#   the secret is set on the repository, that the endpoint is alive, or that a
#   message arrived: the inlet answers an identical silent 200 to every request
#   INCLUDING one with a bad token, so TOKEN VALIDITY IS ONLY OBSERVABLE AT THE
#   RECEIVING END. This guard covers the shipped CONDITION; the delivery half is
#   verified by actually receiving a message, and that evidence lives on the
#   ticket, not here.
#
#   The job-name scan is line-shaped (two-space keys under `jobs:`) rather than
#   a walk of the parsed document. That would be a silent hole — a job written
#   in a shape the scan does not know would simply not be counted — so the scan
#   is paired with a DELIBERATELY LOOSE second pass: every two-space-indented,
#   non-comment line in the jobs region must be parseable by the strict pattern.
#   One line that the loose pass sees and the strict pass cannot read is a FAIL,
#   not a shrug. The guard is allowed to not understand a line; it is not
#   allowed to not understand it QUIETLY and still claim coverage.
set -uo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$HERE/../.." && pwd)"
WF="$REPO_ROOT/.github/workflows/ci.yml"
NOTIFY_JOB="notify-main-red"
SECRET_NAME="OC_MAIN_RED_WEBHOOK_URL"
WANT_IF="failure() && github.ref == 'refs/heads/main'"

PASS=0; FAIL=0
ok()  { PASS=$((PASS+1)); printf '  ok   — %s\n' "$1"; }
bad() { FAIL=$((FAIL+1)); printf '  FAIL — %s\n' "$1"; }

echo "[main-red-notify-guard] $WF"

if [[ ! -f "$WF" ]]; then
  echo "  FAIL — workflow file not found: $WF" >&2
  echo "[main-red-notify-guard] 0 ok, 1 failed"
  exit 1
fi

# ── 0. the file parses as YAML ──────────────────────────────────────────────
# A parser that is ABSENT must not look like a file that is CLEAN, so a missing
# parser is a FAIL with instructions, never a skip. Two independent parsers are
# tried because either alone can be missing on a given runner.
YAML_VIA=""
if command -v python3 >/dev/null 2>&1 && python3 -c 'import yaml' >/dev/null 2>&1; then
  YAML_VIA="python3+PyYAML"
elif command -v ruby >/dev/null 2>&1 && ruby -ryaml -e '' >/dev/null 2>&1; then
  YAML_VIA="ruby+psych"
fi

yaml_parse() {
  case "$YAML_VIA" in
    python3+PyYAML) python3 -c 'import sys, yaml; yaml.safe_load(open(sys.argv[1]))' "$WF" 2>&1 ;;
    ruby+psych)     ruby -ryaml -e 'begin; YAML.unsafe_load_file(ARGV[0]); rescue NoMethodError; YAML.load_file(ARGV[0]); end' "$WF" 2>&1 ;;
  esac
}

if [[ -z "$YAML_VIA" ]]; then
  bad "no YAML parser available, so this guard cannot tell a valid workflow from an invalid one — and 'no parser' must never look like 'file is fine'. Install one: \`python3 -m pip install pyyaml\`, or make \`ruby -ryaml\` importable. A ci.yml that does not parse is a GitHub startup failure: zero jobs run, zero checks appear, and nothing notifies."
elif YAML_ERR="$(yaml_parse)"; then
  ok "workflow file parses as YAML ($YAML_VIA)"
else
  bad "workflow file is NOT valid YAML ($YAML_VIA) — GitHub answers this with a startup failure: zero jobs scheduled, zero checks on the PR, and no notification. Parser said: $(printf '%s' "$YAML_ERR" | tr '\n' ' ' | tail -c 300)"
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

# Two passes over the same lines:
#   loose  — ANY two-space-indented, non-blank, non-comment line. Inside the
#            jobs region such a line can only be a job key.
#   strict — GitHub's job id grammar [_a-zA-Z][a-zA-Z0-9_-]*, optionally quoted,
#            with a trailing comment or a flow-style value after the colon.
# Every loose hit must be a strict hit. The gap between them is the set of lines
# this guard cannot read, and it is reported rather than dropped.
JOB_SCAN_AWK="$(cat <<'AWK'
function strip(s) { sub(/[[:space:]]+#.*$/, "", s); sub(/^[[:space:]]*#.*$/, "", s); return s }
/^  [^ \t#]/ {
  line = strip($0)
  if (line ~ /^[[:space:]]*$/) next
  if (line ~ /^  ["']?[_a-zA-Z][a-zA-Z0-9_-]*["']?:/) {
    id = line
    sub(/^  ["']?/, "", id)
    sub(/["']?:.*$/, "", id)
    print "JOB\t" id
  } else {
    print "UNPARSED\t" $0
  }
}
AWK
)"
SCAN="$(printf '%s\n' "$REGION" | awk "$JOB_SCAN_AWK")"
JOBS="$(printf '%s\n' "$SCAN" | sed -n 's/^JOB\t//p')"
UNPARSED="$(printf '%s\n' "$SCAN" | sed -n 's/^UNPARSED\t//p')"
JOB_COUNT="$(printf '%s\n' "$JOBS" | grep -c . || true)"

if [[ -n "$UNPARSED" ]]; then
  bad "the job-name scan cannot read these lines in the jobs region, so it does NOT claim to cover the jobs they declare:$(printf '%s\n' "$UNPARSED" | sed 's/^/ | /' | tr '\n' ' ')"
else
  ok "every two-space key in the jobs region is readable as a job id (nothing silently uncovered)"
fi

# Positive control for the scan itself. A file with a notify job and nothing to
# notify about is not a state worth shipping, and a scanner that matches zero
# job names would otherwise pass every check below by vacuity. ⚠️ This control
# only catches a scan that has died ENTIRELY — it says nothing about a scan that
# misses exactly the newly added job, which is why the loose/strict pass above
# exists and is the check that actually covers that failure.
if [[ "$JOB_COUNT" -ge 2 ]]; then
  ok "job-name scan is alive ($JOB_COUNT jobs found)"
else
  bad "job-name scan found $JOB_COUNT jobs — the scan is broken (or the file has no gate jobs left); every check below would pass vacuously"
  echo "[main-red-notify-guard] $PASS ok, $FAIL failed"
  exit 1
fi

# ── 1. the notify job exists, exactly once ──────────────────────────────────
NOTIFY_DECLS="$(printf '%s\n' "$JOBS" | grep -cxF "$NOTIFY_JOB" || true)"
if [[ "$NOTIFY_DECLS" == "1" ]]; then
  ok "$NOTIFY_JOB is declared exactly once"
else
  bad "$NOTIFY_JOB is declared $NOTIFY_DECLS times (want exactly 1) — a red trunk would tell nobody, or tell them twice"
fi

# The notify job's own block: from its key to the next two-space key.
NOTIFY_BLOCK="$(printf '%s\n' "$REGION" | awk -v job="  ${NOTIFY_JOB}:" '
  $0 == job { inblock = 1; next }
  inblock && /^  [^ \t#]/ { inblock = 0 }
  inblock { print }
')"

# ── 2. the gate is the shipped condition ────────────────────────────────────
IF_LINE="$(printf '%s\n' "$NOTIFY_BLOCK" | grep -E '^    if:' || true)"
GATE="$(printf '%s' "$IF_LINE" | sed -E 's/^[[:space:]]*if:[[:space:]]*//; s/[[:space:]]*$//')"
# `if: failure() && ...` and `if: ${{ failure() && ... }}` are the same condition
# to GitHub. Only that wrapper is normalised — the condition itself still has to
# match byte for byte, so dropping either the failure() or the ref test reddens.
if [[ "$GATE" == '${{'*'}}' ]]; then
  GATE="$(printf '%s' "$GATE" | sed -E 's/^\$\{\{[[:space:]]*//; s/[[:space:]]*\}\}$//')"
fi
if [[ "$GATE" == "$WANT_IF" ]]; then
  ok "gate is exactly: $WANT_IF"
else
  bad "gate is not exactly \`$WANT_IF\` — widened to always() it messages every pull request; without the ref test it messages every working branch. Found: ${IF_LINE:-(no if: line at all)}"
fi

# ── 3. every other job is in `needs:` ───────────────────────────────────────
# Both legal spellings are read: the flow list `needs: [a, b]` and the block
# list (`needs:` followed by `- a` lines). A trailing `# comment` is stripped
# BEFORE membership is tested — otherwise `needs: [a] # TODO re-add b, c` reads
# as covering b and c, and the guard reports a count it invented.
NEEDS_AWK="$(cat <<'AWK'
function strip(s) { sub(/[[:space:]]+#.*$/, "", s); sub(/^[[:space:]]*#.*$/, "", s); return s }
function emit(s) { gsub(/^["']|["']$/, "", s); if (s != "") print s }
/^    needs:/ {
  line = strip($0)
  sub(/^    needs:[[:space:]]*/, "", line)
  if (line ~ /^[[:space:]]*$/) { inlist = 1; next }
  gsub(/[][,]/, " ", line)
  n = split(line, a, /[[:space:]]+/)
  for (i = 1; i <= n; i++) emit(a[i])
  next
}
inlist {
  line = strip($0)
  if (line ~ /^[[:space:]]*$/) next
  if (line ~ /^[[:space:]]+-[[:space:]]*/) {
    sub(/^[[:space:]]+-[[:space:]]*/, "", line)
    emit(line)
  } else {
    inlist = 0
  }
}
AWK
)"
HAS_NEEDS="$(printf '%s\n' "$NOTIFY_BLOCK" | grep -cE '^    needs:' || true)"
if [[ "$HAS_NEEDS" == "0" ]]; then
  bad "$NOTIFY_JOB has no \`needs:\` line — it would run before the gates it is reporting on"
else
  NEEDS="$(printf '%s\n' "$NOTIFY_BLOCK" | awk "$NEEDS_AWK")"
  NEEDS_COUNT="$(printf '%s\n' "$NEEDS" | grep -c . || true)"
  MISSING=""
  COVERED=0
  while IFS= read -r job; do
    [[ -n "$job" ]] || continue
    [[ "$job" == "$NOTIFY_JOB" ]] && continue
    if printf '%s\n' "$NEEDS" | grep -qxF "$job"; then
      COVERED=$((COVERED+1))
    else
      MISSING="$MISSING $job"
    fi
  done <<< "$JOBS"

  if [[ "$NEEDS_COUNT" == "0" ]]; then
    bad "the \`needs:\` line is present but no entry could be read out of it — the guard cannot claim 'nothing missing' from a list it did not parse. Found: $(printf '%s\n' "$NOTIFY_BLOCK" | grep -E '^    needs:' | head -1)"
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

# Comments are excluded on both shapes: a whole-line comment, and a trailing
# comment after real YAML. The second one matters — every author of every other
# job in this file is otherwise one `# see https://…` away from being accused of
# leaking a credential by a guard that is not about their job at all.
url_hits() {
  grep -nE '://' \
    | grep -vE '^[0-9]+:[[:space:]]*#' \
    | sed -E 's/[[:space:]]+#.*$//' \
    | grep -E '://' || true
}
# Positive control covers BOTH layers: the scheme pattern must still match a
# real literal, AND the comment filters must still drop a documentation link.
CTRL="$(printf 'x: https://example.invalid/in?t=tok\n      - name: n  # see https://example.invalid/doc\n# https://example.invalid/whole-line\n' | url_hits)"
CTRL_N="$(printf '%s\n' "$CTRL" | grep -c . || true)"
if [[ "$CTRL_N" == "1" && "$CTRL" == *"in?t=tok"* ]]; then
  URL_HITS="$(url_hits < "$WF")"
  if [[ -z "$URL_HITS" ]]; then
    ok "no URL literal outside comments (a preserving invariant — it held before this change too)"
  else
    bad "URL literal(s) outside comments — a URL in this public file is a public credential: $URL_HITS"
  fi
else
  bad "the URL-literal scan failed its own positive control (expected exactly the one non-comment literal, got $CTRL_N) — a clean result here would be meaningless"
fi

echo "[main-red-notify-guard] $PASS ok, $FAIL failed"
[[ "$FAIL" == "0" ]] || exit 1
exit 0
