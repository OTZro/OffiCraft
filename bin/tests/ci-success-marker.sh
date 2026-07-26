#!/usr/bin/env bash
# Proves the CI completion marker cannot certify a partial log.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
CI="$ROOT/bin/ci.sh"
PASS=0
FAIL=0
ok() { PASS=$((PASS + 1)); printf '  ok   — %s\n' "$1"; }
bad() { FAIL=$((FAIL + 1)); printf '  FAIL — %s\n' "$1" >&2; }

# How many times can this source emit the authoritative string, by ANY construct?
#
# Matching only the literal `echo "[ci] all green"` form made the marker
# FORGEABLE: printf, a heredoc body, a variable assignment or a nested script
# could emit the same bytes mid-run and be invisible to the guard. A forged
# marker emitted just before a silent early exit passes the exit code, the
# tail -n 1 log rule AND the old guard at once.
#
# So count the literal STRING in every non-comment line, emitter-agnostic, after
# deleting quote characters (\042 " and \047 ') so a split-argument emitter —
# echo "[ci]" "all green" — normalises onto the same literal. Occurrences, not
# lines, so two forgeries on one line still count as two.
#
# Known residual: a string assembled from fragments that never sit adjacent in
# the source (A='[ci]'; B='all green'; echo "$A $B") cannot be caught by any
# source-level scan; that is a limit of static matching, not of this rule.
marker_occurrences() {
  grep -vE '^[[:space:]]*#' "$1" | tr -d '\042\047' | grep -oF '[ci] all green' | wc -l | tr -d '[:space:]'
}

# Exactly one emittable marker, and it is ci.sh's final nonempty source line.
validate_source() {
  local source="$1" count last
  count="$(marker_occurrences "$source")"
  [[ "$count" == "1" ]] || return 1
  last="$(awk 'NF { line=$0 } END { print line }' "$source")"
  [[ "$last" == 'echo "[ci] all green"' ]]
}

# A captured run is successful only if its final line is the exact authority.
accepts_log() { [[ "$(tail -n 1 "$1")" == "[ci] all green" ]]; }

echo "ci success-marker contract tests"
if validate_source "$CI"; then ok "ci.sh has one final exact success marker"; else bad "ci.sh has one final exact success marker"; fi

WORK="$(mktemp -d -t oc-ci-marker-tests.XXXXXX)"
trap 'rm -rf "$WORK"' EXIT

printf '%s\n' '[ci] (5/5) conformance suite' '[ci] all green' > "$WORK/good.log"
if accepts_log "$WORK/good.log"; then ok "completed log is accepted"; else bad "completed log is accepted"; fi

# Broad grep would accept this nested marker; final-line matching must not.
printf '%s\n' '[tests_guard] all green' '[ci] (1/5) golang' '[ci] FAIL — go test' > "$WORK/partial.log"
if accepts_log "$WORK/partial.log"; then bad "partial log after nested green is rejected"; else ok "partial log after nested green is rejected"; fi

# Mutant: adding later work after the marker is the original false-green bug.
cp "$CI" "$WORK/marker-midway.sh"
printf '%s\n' '' false >> "$WORK/marker-midway.sh"
if validate_source "$WORK/marker-midway.sh"; then bad "mutant with marker before later work is rejected"; else ok "mutant with marker before later work is rejected"; fi

# Mutant: duplicate authorities must also be rejected.
cp "$CI" "$WORK/duplicate-marker.sh"
printf '%s\n' 'echo "[ci] all green"' >> "$WORK/duplicate-marker.sh"
if validate_source "$WORK/duplicate-marker.sh"; then bad "mutant with duplicate success marker is rejected"; else ok "mutant with duplicate success marker is rejected"; fi

# ── forgeability fixtures (T-d3e3 D2) ───────────────────────────────────────
# Each forgery is spliced in BEFORE ci.sh's final line, so the legitimate echo
# stays last and the tail -n 1 rule cannot be what rejects it — only the
# emitter-agnostic count can. This is the dangerous shape: a second emitter
# fires mid-run, a later step exits 0 early, and the log's last line is a
# forged authority.
forge() { # forge NAME LINE... → path to a ci.sh mutant with LINEs spliced in
  local out="$WORK/$1.sh"; shift
  sed '$d' "$CI" > "$out"
  printf '%s\n' "$@" >> "$out"
  tail -n 1 "$CI" >> "$out"
  printf '%s' "$out"
}
reject() { # reject DESC FIXTURE
  if validate_source "$2"; then bad "$1"; else ok "$1"; fi
}

reject "forged printf marker is rejected" \
  "$(forge forge-printf "printf '[ci] all green\\n'")"
reject "forged split-argument echo marker is rejected" \
  "$(forge forge-split 'echo "[ci]" "all green"')"
reject "forged variable-built marker is rejected" \
  "$(forge forge-var 'OK_MARKER="[ci] all green"' 'echo "$OK_MARKER"')"
reject "forged heredoc marker is rejected" \
  "$(forge forge-heredoc "cat <<'FORGED'" '[ci] all green' 'FORGED')"

# Sentinel: the hardened rule must not be so strict that the REAL ci.sh fails —
# a single legitimate `echo "[ci] all green"` as the final line still passes.
if validate_source "$CI"; then ok "sentinel — the legitimate single echo form still PASSES"; else bad "sentinel — the legitimate single echo form still PASSES"; fi

# Nested suites' own markers are DIFFERENT authorities, not forgeries: a source
# that also emits them must still validate (regression twin of the log-side
# partial.log case above).
NESTED="$(forge nested-markers 'echo "[tests_guard] all green"' 'echo "[conformance] all green"')"
if validate_source "$NESTED"; then ok "nested [tests_guard]/[conformance] markers are not mistaken for the CI marker"; else bad "nested [tests_guard]/[conformance] markers are not mistaken for the CI marker"; fi

echo "ci success-marker contract tests: $PASS ok, $FAIL failed"
[[ "$FAIL" == "0" ]]
