#!/usr/bin/env bash
# Keeps the retired <Lightbox> image overlay from coming back (T-f014).
#
# ── the failure mode this guard exists to stop ────────────────────────────────
# The cockpit had TWO full-size overlays for the same job. `MarkdownPreviewOverlay`
# (`.md-preview*`) grew a header with the filename, a share link, a download and
# a close button; `Lightbox` (`.chat__lightbox*`) was a bare backdrop with an ×.
# Which one a user got depended on which call site their click travelled through.
# Worse, the divergence rotted silently: `AttachmentStrip` stopped reading its
# `onOpenImage` prop, so FIVE call sites went on passing a handler into a
# component that ignored it while mounting a second overlay that could never
# open. Nothing was red — an unused prop and an unreachable component are both
# perfectly type-correct.
#
# T-f014 deleted the component and its stylesheet block. This guard is what stops
# the next person from re-introducing a second image surface: reach for a
# `<Lightbox>` again and CI says no, in the same breath as pointing at the shell
# that already exists.
#
# ── WHAT THIS GUARD ASSERTS ──────────────────────────────────────────────────
#   1. `<Lightbox` appears ZERO times in production frontend source.
#   2. No stylesheet under frontend/ declares a `.chat__lightbox*` RULE — the
#      block whose 40 lines were deleted from office.css. Scoped to rule
#      declarations (a line whose first non-space is the selector) rather than
#      any mention, because the surrounding prose has to stay free to explain
#      what was retired and why; the assertion is about shipped CSS.
#   3. The corpus it searched is NON-EMPTY and really is the frontend tree. A
#      grep over a mistyped path returns zero matches and would otherwise be
#      reported as a pass — the classic "green because nothing was checked".
#   4. POSITIVE CONTROL: a planted `<Lightbox .../>` in a scratch copy of the
#      tree is found, AT THE PLANTED PATH AND LINE. Asserting only "the scan
#      failed" is not enough — a scan that failed for an unrelated reason (bad
#      regex, missing tool, wrong directory) would score as a working guard.
#      This repo has shipped that bug twice, so the control matches the exact
#      `path:line` it planted, not merely a non-zero exit.
#
# ── HOW IT SEARCHES (and why not a file list) ────────────────────────────────
# The scan is a glob over the whole tree, minus node_modules/dist. It is
# deliberately NOT an enumerated list of files: a list is a promise that it is
# complete, it silently stops covering files added after it was written, and the
# next reader trusts it and skips looking. The only thing named by path here is
# the ONE surviving overlay, and that is named so a violation can point at it.
#
# ── WHAT A GREEN DOES NOT MEAN ───────────────────────────────────────────────
# It does not mean there is only one image preview surface. Someone can write a
# second overlay under a different name and different classes tomorrow; this
# guard only knows the two names the retired one used. Nor does it check that
# the surviving overlay is any good — that is what the vitest suite and the CT
# visual guards are for. It is a tripwire on a specific regression, not a proof
# of the design.
set -uo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT="$(cd "$HERE/../.." && pwd)"
FE="$ROOT/frontend"

PASS=0; FAIL=0
ok()  { PASS=$((PASS+1)); printf '  ok   — %s\n' "$1"; }
bad() { FAIL=$((FAIL+1)); printf '  FAIL — %s\n' "$1"; }

# The one overlay that survived — named so a failure can say where to go instead.
SURVIVOR="frontend/src/components/MarkdownPreviewOverlay.tsx"

# scan_component DIR — every `<Lightbox` occurrence in DIR's production sources,
# as `relative/path:line:text`. Production = .ts/.tsx that is not a test.
# `grep -H` is load-bearing in both scans: grep omits the filename when xargs
# hands it a single file, which would break the path:line positive controls for
# a reason that has nothing to do with what is being asserted.
scan_component() {
  local dir="$1"
  ( cd "$dir" && \
    find . -type d \( -name node_modules -o -name dist -o -name .git \) -prune -o \
         -type f \( -name '*.ts' -o -name '*.tsx' \) \
         ! -name '*.test.ts' ! -name '*.test.tsx' ! -name '*.spec.ts' ! -name '*.spec.tsx' \
         -print0 \
    | xargs -0 grep -H -n -F '<Lightbox' 2>/dev/null )
}

# scan_class DIR — every `.chat__lightbox*` RULE DECLARATION in DIR's stylesheets.
scan_class() {
  local dir="$1"
  ( cd "$dir" && \
    find . -type d \( -name node_modules -o -name dist -o -name .git \) -prune -o \
         -type f -name '*.css' -print0 \
    | xargs -0 grep -H -n -E '^[[:space:]]*\.chat__lightbox' 2>/dev/null )
}

# count_files DIR — how many files the scan above actually looked at.
count_files() {
  local dir="$1"
  ( cd "$dir" && \
    find . -type d \( -name node_modules -o -name dist -o -name .git \) -prune -o \
         -type f \( -name '*.ts' -o -name '*.tsx' -o -name '*.css' \) -print \
    | wc -l | tr -d ' ' )
}

echo "[lightbox-retired-guard] frontend tree: $FE"

# ── (0) the corpus is real ───────────────────────────────────────────────────
if [[ -d "$FE/src/components" ]]; then
  ok "frontend/src/components exists (the tree being scanned is the real one)"
else
  bad "frontend/src/components is missing — every scan below would be a vacuous pass"
fi
FILES="$(count_files "$FE")"
if [[ "${FILES:-0}" -ge 100 ]]; then
  ok "scan corpus is $FILES files (non-empty)"
else
  bad "scan corpus is only ${FILES:-0} files — the find expression is not reaching the tree"
fi
if [[ -f "$ROOT/$SURVIVOR" ]]; then
  ok "the surviving overlay is present at $SURVIVOR"
else
  bad "$SURVIVOR is missing — retiring Lightbox with no replacement leaves NO image preview"
fi

# ── (1) zero <Lightbox in production source ──────────────────────────────────
HITS="$(scan_component "$FE")"
if [[ -z "$HITS" ]]; then
  ok "no <Lightbox in production frontend source"
else
  bad "<Lightbox is back in production source — use $SURVIVOR instead:"
  printf '%s\n' "$HITS" | sed 's/^/         /'
fi

# ── (2) zero chat__lightbox anywhere under frontend/ ─────────────────────────
CLASSHITS="$(scan_class "$FE")"
if [[ -z "$CLASSHITS" ]]; then
  ok "no .chat__lightbox rule declared in any frontend stylesheet"
else
  bad ".chat__lightbox styling is back — that block was deleted with the component:"
  printf '%s\n' "$CLASSHITS" | sed 's/^/         /'
fi

# ── (3) positive control ─────────────────────────────────────────────────────
# Plant a violation in a scratch copy and require the scan to name THAT path and
# THAT line. A control that only asserts "something failed" passes when the scan
# is broken in a way that fails on everything.
WORK="$(mktemp -d -t oc-lightbox-guard.XXXXXX)"
trap 'rm -rf "$WORK"' EXIT
mkdir -p "$WORK/src/components"
cat >"$WORK/src/components/Planted.tsx" <<'EOF'
// line 1
// line 2
export function Planted() {
  return <Lightbox src={null} onClose={() => {}} />;
}
EOF
# Pad the scratch tree so the corpus check would pass on it too, keeping the
# control a test of the MATCHING, not of the sizing.
for i in $(seq 1 4); do : > "$WORK/src/components/filler$i.ts"; done
cat >"$WORK/src/components/planted.css" <<'EOF'
.chat__lightbox { position: fixed; }
EOF

CTRL="$(scan_component "$WORK")"
if [[ "$CTRL" == "./src/components/Planted.tsx:4:"* ]]; then
  ok "positive control: the planted <Lightbox is reported at ./src/components/Planted.tsx:4"
else
  bad "positive control did not name the planted path:line — the component scan is not matching what it claims (got: ${CTRL:-<nothing>})"
fi

CTRLCLASS="$(scan_class "$WORK")"
if [[ "$CTRLCLASS" == *"./src/components/planted.css:1:"* ]]; then
  ok "positive control: the planted chat__lightbox rule is reported at ./src/components/planted.css:1"
else
  bad "positive control did not name the planted stylesheet path:line — the class scan is not matching what it claims (got: ${CTRLCLASS:-<nothing>})"
fi

# NEGATIVE control: the same scratch tree with the violations removed must come
# back clean. Without this, a scan that reports every file would satisfy both
# positive controls above and still be worthless.
rm "$WORK/src/components/Planted.tsx" "$WORK/src/components/planted.css"
if [[ -z "$(scan_component "$WORK")" && -z "$(scan_class "$WORK")" ]]; then
  ok "negative control: a clean tree reports nothing"
else
  bad "negative control: a clean tree still reported hits — the scan matches indiscriminately"
fi

echo
echo "[lightbox-retired-guard] $PASS ok, $FAIL failed"
[[ "$FAIL" == "0" ]]
