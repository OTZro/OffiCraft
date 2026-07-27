#!/usr/bin/env bash
# Keeps the "兩份權威打架" rule from existing twice and drifting (T-c19c).
#
# ── the failure mode this guard exists to stop ────────────────────────────────
# The rule (§4.1 of seeds/system_interaction.md: when two authorities contradict
# each other — doc vs code, two specs, ticket vs reality, a predecessor's ruling
# vs the code in front of you — STOP and open a reply card; it is a MUST-ASK
# category, explicitly outside the agent's own "is this worth asking" judgement)
# is restated, in projection form, in at least three other places that a human or
# an agent will actually read: the repo charter (CLAUDE.md §8 and §9(d)) and the
# owner-facing gate taxonomy (docs/guide/best-practices.md §三). Before T-c19c
# CLAUDE.md §8 carried its OWN full statement of what to do, so the repo had two
# independent copies of the same instruction. Two copies drift: someone softens
# one ("use judgement"), the other still says "always stop", and every reader
# obeys whichever copy they happened to open.
#
# The fix has two halves. (1) Editorial: the rule BODY now lives in exactly one
# place, wrapped in begin/end markers in the seed; every other site defers to it
# and keeps only the domain knowledge that is unique to that site. (2) This
# guard: every deferring site carries a marker that pins the CONTENT HASH of the
# seed block, so touching one character of the rule turns every deferring site
# red and forces a human to re-read each restatement before re-stamping it.
#
# ── WHAT THIS GUARD GUARANTEES ───────────────────────────────────────────────
#   * The seed block exists and is extractable. If the begin/end markers are
#     gone, or the block is empty, this guard FAILS — it never degrades into
#     "found nothing, therefore green".
#   * At least MIN_DEFER_SITES deferring markers exist. A loop over a set that
#     someone quietly emptied cannot report success.
#   * Every deferring marker's hash equals the current seed-block hash. Edit the
#     rule and every registered restatement goes red at once.
#
# ── WHAT THIS GUARD DOES *NOT* GUARANTEE (read before trusting a green) ──────
# A GREEN HERE IS NOT A PROOF THAT NO SECOND COPY OF THE RULE EXISTS. This guard
# only knows about restatements that were REGISTERED with a marker; it cannot see
# a fourth copy someone pastes into a new file tomorrow, and it cannot read the
# words at a registered site at all — a site may carry a perfectly current hash
# while its prose contradicts the seed, because the hash pins the SEED's content,
# not the site's fidelity to it. It also covers only files tracked in this repo
# (owner additions / task manuals / role lessons live in the server DB and are
# out of reach). Catching an unregistered or contradictory copy is a REVIEW job,
# not a CI job.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
SEED="$ROOT/seeds/system_interaction.md"
RULE_ID="rule:conflicting-authorities"
BEGIN="<!-- ${RULE_ID}:begin -->"
END="<!-- ${RULE_ID}:end -->"
# Known-minimum number of deferring sites. Raise it when a site is added; if a
# site is legitimately REMOVED, lowering it must be a deliberate, reviewed edit —
# that is the point. Sites at the time of writing: CLAUDE.md §8, CLAUDE.md §9(d),
# docs/guide/best-practices.md §三.
MIN_DEFER_SITES=3

PASS=0
FAIL=0
ok() { PASS=$((PASS + 1)); printf '  ok   — %s\n' "$1"; }
bad() { FAIL=$((FAIL + 1)); printf '  FAIL — %s\n' "$1" >&2; }

sha256_of_stdin() {
  if command -v shasum >/dev/null 2>&1; then
    shasum -a 256 | awk '{print $1}'
  else
    sha256sum | awk '{print $1}'
  fi
}

# Extract the lines STRICTLY between the begin and end markers of $1.
extract_block() {
  awk -v b="$BEGIN" -v e="$END" '
    $0 == b { inb = 1; next }
    $0 == e { inb = 0; next }
    inb     { print }
  ' "$1"
}

# ── 1. the seed block must exist and be non-empty ────────────────────────────
# This is the anti-"assert the empty set" leg: nothing below is allowed to run
# against a block that was never found.
if [[ ! -f "$SEED" ]]; then
  bad "seed file missing: $SEED"
  echo "rule-defer contract tests: $PASS ok, $FAIL failed"
  exit 1
fi

if ! grep -qF "$BEGIN" "$SEED" || ! grep -qF "$END" "$SEED"; then
  bad "seed block markers not found in seeds/system_interaction.md ('$BEGIN' / '$END') — the rule body has no single source to pin"
  echo "rule-defer contract tests: $PASS ok, $FAIL failed"
  exit 1
fi

BLOCK="$(extract_block "$SEED")"
if [[ -z "${BLOCK//[[:space:]]/}" ]]; then
  bad "seed block is EMPTY between the markers — refusing to pin a hash of nothing"
  echo "rule-defer contract tests: $PASS ok, $FAIL failed"
  exit 1
fi
ok "seed rule block extracted from seeds/system_interaction.md ($(printf '%s\n' "$BLOCK" | wc -l | tr -d ' ') lines)"

WANT="$(printf '%s\n' "$BLOCK" | sha256_of_stdin | cut -c1-12)"
ok "seed rule block content hash = $WANT"

# ── 2. the deferring sites must be found, and there must be enough of them ───
# Scan TRACKED files only, and skip this guard itself: the marker pattern appears
# here in prose and in the mutant fixture below, and a scan that matches its own
# source is the always-true shape this repo keeps relearning.
SELF_REL="bin/tests/rule-defer-guard.sh"
MARKER_RE="<!-- defers-to: ${RULE_ID}@[0-9a-f]{6,64} -->"
SITES="$(
  git -C "$ROOT" grep -n -I -E "$MARKER_RE" -- ':!'"$SELF_REL" 2>/dev/null || true
)"

SITE_COUNT=0
[[ -n "$SITES" ]] && SITE_COUNT="$(printf '%s\n' "$SITES" | wc -l | tr -d ' ')"

if (( SITE_COUNT >= MIN_DEFER_SITES )); then
  ok "found $SITE_COUNT deferring site(s), at or above the known minimum of $MIN_DEFER_SITES"
else
  bad "found only $SITE_COUNT deferring site(s), below the known minimum of $MIN_DEFER_SITES — a restatement lost its marker, or the rule was copied somewhere unregistered"
fi

# ── 3. every deferring site must pin the CURRENT seed hash ───────────────────
STALE=""
while IFS= read -r line; do
  [[ -n "$line" ]] || continue
  file="${line%%:*}"
  rest="${line#*:}"
  lineno="${rest%%:*}"
  got="$(printf '%s\n' "$line" | grep -oE "${RULE_ID}@[0-9a-f]{6,64}" | head -n1)"
  got="${got#*@}"
  if [[ "$got" == "$WANT" ]]; then
    ok "$file:$lineno defers at the current hash ($got)"
  else
    STALE="$STALE $file:$lineno(@$got)"
    bad "$file:$lineno pins a STALE hash ($got, want $WANT) — the rule in seeds/system_interaction.md §4.1 changed. Re-READ this restatement, confirm it still agrees with the seed, then update the hash here."
  fi
done <<<"$SITES"

# ── 4. discriminating power, proven in-process (mutant) ──────────────────────
# A hash comparison is only worth anything if a one-character edit to the seed
# actually moves the hash. Prove it on a COPY — never on the real file — so the
# proof costs nothing and cannot leave the tree mutated.
MUT_DIR="$(mktemp -d -t oc-rule-defer.XXXXXX)"
trap 'rm -rf "$MUT_DIR"' EXIT
MUT_SEED="$MUT_DIR/seed.md"
# The perturbation is ONE character appended to the first non-empty line inside
# the block — additive rather than a search-and-replace, so the proof cannot go
# vacuous just because some particular word was edited out of the rule later.
awk -v b="$BEGIN" -v e="$END" '
  $0 == b { inb = 1; print; next }
  $0 == e { inb = 0; print; next }
  inb && !done && NF { print $0 "X"; done = 1; next }
  { print }
' "$SEED" >"$MUT_SEED"

if cmp -s "$SEED" "$MUT_SEED"; then
  bad "mutant — could not perturb the seed block (the substitution matched nothing); the discriminating-power proof below would be vacuous"
else
  MUT_BLOCK="$(extract_block "$MUT_SEED")"
  MUT_HASH="$(printf '%s\n' "$MUT_BLOCK" | sha256_of_stdin | cut -c1-12)"
  if [[ "$MUT_HASH" != "$WANT" ]]; then
    ok "mutant — a one-character edit inside the seed block moves the hash ($WANT -> $MUT_HASH), so every deferring site would go red"
  else
    bad "mutant — a one-character edit inside the seed block did NOT move the hash; this guard cannot detect the drift it exists for"
  fi
fi

# ── 5. the enforcement wiring must still be there ────────────────────────────
# Same guard-of-the-guard convention as bin/tests/run.sh's marker dispatch: this
# file is worthless if the runner stops caring about its exit code.
RUNNER="$ROOT/bin/tests/run.sh"
if grep -qE '^[[:space:]]*RULEDEFER="\$HERE/rule-defer-guard\.sh"[[:space:]]*$' "$RUNNER" \
  && grep -qE '^[[:space:]]*if run_guard "\$RULEDEFER"; then[[:space:]]*$' "$RUNNER"; then
  ok "bin/tests/run.sh still dispatches this guard through run_guard (exit code accounted through bad()/FAIL)"
else
  bad "bin/tests/run.sh no longer dispatches this guard — it would be decorative"
fi

if [[ -n "$STALE" ]]; then
  echo "  note — stale deferring site(s):$STALE" >&2
fi
echo "rule-defer contract tests: $PASS ok, $FAIL failed"
[[ "$FAIL" == "0" ]]
