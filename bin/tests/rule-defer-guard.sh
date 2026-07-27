#!/usr/bin/env bash
# Keeps the "兩份權威打架" rule from existing twice and drifting (T-c19c).
#
# ── the failure mode this guard exists to stop ────────────────────────────────
# The rule (§4.1 of seeds/system_interaction.md: when two authorities contradict
# each other — doc vs code, two specs, ticket vs reality, a predecessor's ruling
# vs the code in front of you — STOP and open a reply card; it is a MUST-ASK
# category, explicitly outside the agent's own "is this worth asking" judgement)
# is restated, in projection form, in the repo charter (CLAUDE.md §8 and §9(d))
# and in the owner-facing gate taxonomy (docs/guide/best-practices.md §三).
# Before T-c19c CLAUDE.md §8 carried its OWN full statement of what to do, so the
# repo had two independent copies of the same instruction. Two copies drift:
# someone softens one ("use judgement"), the other still says "always stop", and
# every reader obeys whichever copy they happened to open.
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
#   * EACH FILE NAMED IN required_sites BELOW carries at least one marker whose
#     hash is current. This is the load-bearing check: it is per-path, so it
#     cannot be satisfied by markers pasted somewhere else, and it never consults
#     MIN_DEFER_SITES, so it holds even if that knob is turned to zero.
#     (The first version of this file asserted only a COUNT, under the claim "a
#     loop over a set that someone quietly emptied cannot report success". That
#     claim was FALSE and a reviewer falsified it twice: deleting all three real
#     markers and pasting three into a junk file passed, and MIN_DEFER_SITES=0
#     reported "found 0 deferring site(s), at or above the known minimum of 0".
#     Both are red now. The lesson generalises — read what this file asserts, not
#     what its summary says it asserts.)
#   * The total marker count is at or above MIN_DEFER_SITES, and that constant is
#     itself held to a floor written in a second place — lowering it is a
#     deliberate two-place edit, not a one-character knob turn.
#   * Every marker's hex is a prefix (>= 12 chars) of the current seed-block
#     digest. Edit the block and every registered site goes red at once.
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
# not a CI job. Three further limits, each of which changes how you should act:
#   * THE COUNT IS PADDABLE AND ITS MINIMUM IS A KNOB. Outside required_sites,
#     the tally says nothing about WHERE the markers are — n markers in a junk
#     file satisfy it. MIN_DEFER_SITES and its floor are both just numbers in
#     this file; a determined edit lowers them. required_sites is the only part
#     that pins locations, so a deferring site NOT listed there is protected by
#     the weak check alone.
#   * A RED DOES NOT MEAN THE RULE CHANGED. Everything between the begin/end
#     markers is hashed, normative or not. Prose parked inside the block makes a
#     typo fix announce "the rule changed, go re-read" to every site — a false
#     alarm, and repeated false alarms train exactly the reflex ("re-stamp the
#     hash without reading") this guard exists to prevent. Keep non-normative
#     text OUTSIDE the block (that is why the seed's own explanatory comment sits
#     above `:begin`), and read a red as "the block changed — go look at the
#     diff", not as proof of a semantic change.
#   * NOTHING HERE CHECKS THAT A SITE'S CLAIM ABOUT ITSELF IS TRUE. A site saying
#     "this file does not restate the rule" while restating it is invisible to a
#     hash comparison.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
SELF_REL="bin/tests/rule-defer-guard.sh"
SEED="$ROOT/seeds/system_interaction.md"
RULE_ID="rule:conflicting-authorities"
BEGIN="<!-- ${RULE_ID}:begin -->"
END="<!-- ${RULE_ID}:end -->"

# Known-minimum number of deferring markers, and the files that must EACH carry
# at least one current marker. required_sites is the real anti-water-down check;
# the count is a weak backstop for sites not named here. Adding a site: extend
# required_sites and raise MIN_DEFER_SITES. Removing one must be deliberate —
# see MIN_DEFER_SITES_FLOOR below.
MIN_DEFER_SITES=3
required_sites=(
  "CLAUDE.md"                    # §8 關鍵護欄 + §9(d) reviewer checklist
  "docs/guide/best-practices.md" # §三 owner-facing gate taxonomy
)

PASS=0
FAIL=0
ok() { PASS=$((PASS + 1)); printf '  ok   — %s\n' "$1"; }
bad() { FAIL=$((FAIL + 1)); printf '  FAIL — %s\n' "$1" >&2; }

# A structural failure: report and STOP. Never fall through into loops over a set
# that could not be established.
die_early() {
  bad "$1"
  echo "rule-defer contract tests: $PASS ok, $FAIL failed"
  exit 1
}

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
[[ -f "$SEED" ]] || die_early "seed file missing: $SEED"
grep -qF "$BEGIN" "$SEED" && grep -qF "$END" "$SEED" ||
  die_early "seed block markers not found in seeds/system_interaction.md ('$BEGIN' / '$END') — the rule body has no single source to pin"

BLOCK="$(extract_block "$SEED")"
[[ -n "${BLOCK//[[:space:]]/}" ]] ||
  die_early "seed block is EMPTY between the markers — refusing to pin a hash of nothing"
ok "seed rule block extracted from seeds/system_interaction.md ($(printf '%s\n' "$BLOCK" | wc -l | tr -d ' ') lines)"

# Keep the FULL digest: markers are compared by PREFIX, so a maintainer who
# pastes the whole sha256 is correct rather than reported "stale".
WANT_FULL="$(printf '%s\n' "$BLOCK" | sha256_of_stdin)"
WANT="${WANT_FULL:0:12}"
ok "seed rule block content hash = $WANT (markers match by prefix of the full digest, >= 12 hex chars)"

# ── 2. the knob is itself guarded ────────────────────────────────────────────
# MIN_DEFER_SITES=0 used to make the count check report success on the EMPTY SET
# — the exact fake-guard shape this file's header disclaims. Same technique as
# the run.sh dispatch assertion in step 7: read the constant back out of this
# file by an ANCHORED grep (unanchored would match the prose above, a check that
# can never fail) and hold it to a floor declared in a second place.
MIN_DEFER_SITES_FLOOR=3
DECLARED_MIN="$(grep -E '^MIN_DEFER_SITES=[0-9]+$' "$ROOT/$SELF_REL" | head -n1 | cut -d= -f2)"
if [[ -z "$DECLARED_MIN" ]]; then
  bad "could not read an anchored 'MIN_DEFER_SITES=<n>' declaration out of $SELF_REL — the knob is unguarded"
elif ((DECLARED_MIN < MIN_DEFER_SITES_FLOOR)); then
  bad "MIN_DEFER_SITES=$DECLARED_MIN is below its floor of $MIN_DEFER_SITES_FLOOR — turning this knob down is how a count check degenerates into an assertion about the empty set. If a site really went away, lower the floor too, deliberately."
else
  ok "MIN_DEFER_SITES=$DECLARED_MIN is at or above its hard floor of $MIN_DEFER_SITES_FLOOR"
fi

# ── 3. collect the deferring markers ─────────────────────────────────────────
# Scan TRACKED files only, and skip this guard itself: the marker pattern appears
# here in prose, and a scan that matches its own source is the always-true shape
# this repo keeps relearning.
MARKER_RE="<!-- defers-to: ${RULE_ID}@[0-9a-f]{6,64} -->"
SITES="$(git -C "$ROOT" grep -n -I -E "$MARKER_RE" -- ':!'"$SELF_REL" 2>/dev/null || true)"

SITE_COUNT=0
[[ -n "$SITES" ]] && SITE_COUNT="$(printf '%s\n' "$SITES" | wc -l | tr -d ' ')"

if ((SITE_COUNT >= MIN_DEFER_SITES)); then
  ok "found $SITE_COUNT deferring marker(s), at or above the known minimum of $MIN_DEFER_SITES (weak: says nothing about WHERE — step 5 is the one that pins locations)"
else
  bad "found only $SITE_COUNT deferring marker(s), below the known minimum of $MIN_DEFER_SITES — a restatement lost its marker, or the rule was copied somewhere unregistered"
fi

# ── 4. every marker must pin the CURRENT seed hash ───────────────────────────
CURRENT_FILES=""
STALE=""
while IFS= read -r line; do
  [[ -n "$line" ]] || continue
  file="${line%%:*}"
  rest="${line#*:}"
  lineno="${rest%%:*}"
  got="$(printf '%s\n' "$line" | grep -oE "${RULE_ID}@[0-9a-f]{6,64}" | head -n1)"
  got="${got#*@}"
  if ((${#got} < 12)); then
    STALE="$STALE $file:$lineno(@$got)"
    bad "$file:$lineno pins only ${#got} hex chars ($got) — too few to identify a block; use at least 12 (current: $WANT)"
  elif [[ "${WANT_FULL:0:${#got}}" == "$got" ]]; then
    ok "$file:$lineno defers at the current hash ($got)"
    CURRENT_FILES="$CURRENT_FILES $file"
  else
    STALE="$STALE $file:$lineno(@$got)"
    bad "$file:$lineno pins a STALE hash ($got; want a prefix of $WANT_FULL) — the block in seeds/system_interaction.md §4.1 changed. Re-READ this restatement, confirm it still agrees with the seed, then update the hash here. (A red means the BLOCK changed, not necessarily that the rule's meaning did — look at the diff before re-stamping.)"
  fi
done <<<"$SITES"

# ── 5. the named sites must EACH still carry a current marker ────────────────
# The load-bearing anti-water-down check. Per-path and independent of the count:
# deleting the real markers and pasting the same number into a junk file reddens
# here even though the tally is untouched.
# The two failure branches are DIFFERENT SITUATIONS and must say so. A rename is
# a legitimate refactor, and telling that maintainer his restatement is
# "unregistered" — in anti-water-down wording, without ever naming required_sites
# or this file — accuses him of the wrong thing and leaves him with no idea what
# the correct fix is. A rejection message has to teach the correct repair.
for want_file in "${required_sites[@]}"; do
  if [[ " $CURRENT_FILES " == *" $want_file "* ]]; then
    ok "required site $want_file carries a marker at the current hash"
  elif [[ ! -e "$ROOT/$want_file" ]]; then
    bad "required site $want_file does not exist — was it moved or renamed? If so this is not a violation: update the required_sites list in $SELF_REL to the new path (and check the marker moved with the text). If the file was deleted outright, drop it from required_sites and lower MIN_DEFER_SITES / MIN_DEFER_SITES_FLOOR deliberately."
  else
    bad "required site $want_file exists but carries NO marker at the current hash — the restatement there is unregistered. Re-read it against seeds/system_interaction.md §4.1 and re-stamp it with $WANT. (Parking the marker in some other file does not satisfy this check; the required paths are listed in $SELF_REL.)"
  fi
done

# ── 6. discriminating power, proven in-process (mutant) ──────────────────────
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
  bad "mutant — could not perturb the seed block (the substitution matched nothing); the discriminating-power proof would be vacuous"
else
  MUT_FULL="$(extract_block "$MUT_SEED" | sha256_of_stdin)"
  if [[ "${MUT_FULL:0:12}" != "$WANT" ]]; then
    ok "mutant — a one-character edit inside the seed block moves the hash ($WANT -> ${MUT_FULL:0:12}), so every deferring site would go red"
  else
    bad "mutant — a one-character edit inside the seed block did NOT move the hash; this guard cannot detect the drift it exists for"
  fi
fi

# ── 7. the enforcement wiring must still be there ────────────────────────────
# Same guard-of-the-guard convention as bin/tests/run.sh's marker dispatch: this
# file is worthless if the runner stops caring about its exit code.
RUNNER="$ROOT/bin/tests/run.sh"
if grep -qE '^[[:space:]]*RULEDEFER="\$HERE/rule-defer-guard\.sh"[[:space:]]*$' "$RUNNER" &&
  grep -qE '^[[:space:]]*if run_guard "\$RULEDEFER"; then[[:space:]]*$' "$RUNNER"; then
  ok "bin/tests/run.sh still dispatches this guard through run_guard (exit code accounted through bad()/FAIL)"
else
  bad "bin/tests/run.sh no longer dispatches this guard — it would be decorative"
fi

if [[ -n "$STALE" ]]; then
  echo "  note — stale deferring site(s):$STALE" >&2
fi
echo "rule-defer contract tests: $PASS ok, $FAIL failed"
[[ "$FAIL" == "0" ]]
