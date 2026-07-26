#!/usr/bin/env bash
# Proves the CI completion marker cannot certify a partial log.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
CI="$ROOT/bin/ci.sh"
PASS=0
FAIL=0
ok() { PASS=$((PASS + 1)); printf '  ok   — %s\n' "$1"; }
bad() { FAIL=$((FAIL + 1)); printf '  FAIL — %s\n' "$1" >&2; }

# Exactly one literal marker, and it is ci.sh's final nonempty source line.
validate_source() {
  local source="$1" count last
  count="$(grep -cE '^[[:space:]]*echo "\[ci\] all green"[[:space:]]*$' "$source" || true)"
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

echo "ci success-marker contract tests: $PASS ok, $FAIL failed"
[[ "$FAIL" == "0" ]]
