#!/usr/bin/env bash
# officraft — the MACOS-HOST-SHAPED subset of the quality gate, as ONE entry point.
#
# WHY THIS FILE EXISTS
# bin/ci-cloud.sh is "everything a LINUX runner can honestly run". These gates
# are the other kind: they need macOS userland, and until T-ab2a they ran on
# exactly one developer's Mac. That meant nobody else's pull request was guarded
# by them at all — a contributor could redden them and never find out.
#
# The reason they stayed local used to be "we do not pay for GitHub Actions".
# That premise died when the repo went public (standard runners, macOS included,
# are free here). T-ab2a node 2 then MEASURED each candidate on a hosted macOS
# runner instead of assuming, and this file is exactly the set that came back
# green. Numbers and the excluded one are in the ticket; the short version:
#
#   IN  bin/tests/run.sh          rc=0, 244s  (its Linux reds were BSD/GNU
#                                              mktemp semantics + macOS-shaped
#                                              install.sh fixtures — a Mac has
#                                              neither problem)
#   IN  gitleaks content scan     rc=0,  10s
#   IN  bin/check-officraft-dist  rc=0,   0s
#   IN  frontend `test:ct`        rc=1,  62s AT THE TIME — see below. It is IN now.
#
# `test:ct` WAS the one measured red, and this header used to end with "it stays
# out, real-browser layout is still guarded by one machine only". That is no
# longer true, and the reason it changed is worth stating precisely (T-0fef).
# The runner red was 2 failures out of 207 tests, and BOTH were the same group:
# nav-tabs-narrow.ct.spec.tsx's `nav strip geometry`, asserting how many pixels
# of the 使用說明 label survive clipping (expected >= 36, received 35; expected
# 95, received 90). 205 passed. Independently, pinning a non-macOS CJK webfont
# into the CT harness on the dev Mac reddened exactly the same two and nothing
# else — a Han glyph advances 1em in nearly every CJK font and macOS `system-ui`
# is the ~4% outlier, which was the whole of that group's headroom.
# The owner ruled that group out of existence rather than making it portable
# (docs/guide/mobile.md already documents the clipping as normal, and the strip
# scrolls). With it gone, the suite's font dependence measured to zero across two
# font environments — so the gate moves here instead of living on one laptop.
# Still NOT ubuntu: `bin/ci-cloud.sh` explains why a Linux font stack is a
# different question, and that one has never been measured. Do not infer it.
#
# ⚠️ The old resolution stands for everything else: if a CT guard ever reddens
# HERE and green on a dev Mac, the fix is the font environment, never a looser
# threshold. Relaxing an assertion to please a runner trades a false red for a
# false green.
#
# ONE DEFINITION, NOT TWO: the workflow (.github/workflows/ci.yml) installs a
# pinned toolchain and calls this script and nothing else. A YAML file listing
# these gates would be a second list, and the second list drifts silently — the
# missing gate just stops running, which looks exactly like a green.
#
# THIS IS NOT LAND AUTHORITY. bin/ci.sh is, and still runs all of these plus
# everything that cannot leave the Mac. This script is a cross-check that makes
# the host-shaped gates apply to EVERY pull request instead of one laptop.
# It deliberately does NOT print '[ci] all green' — that string is ci.sh's
# authority marker and bin/tests/ci-success-marker.sh forbids anyone else from
# being able to emit it.
#
# FAIL-CLOSED EVERYWHERE. Every gate below either runs or fails the script. A
# missing tool is a FAILURE, never a skip: "the scanner was not installed" and
# "the scanner found nothing" produce the same silence, and one of them is a
# hole. Same for a renamed script — absence is checked explicitly rather than
# discovered as a no-op.
set -uo pipefail

fail() { echo "[ci-macos-host] FAIL — $*" >&2; exit 1; }

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT" || fail "cannot cd to repo root $ROOT"

# NOT set -e, on purpose: every gate below is explicitly `|| fail` so that each
# failure names WHICH gate died and how to reproduce it, which `set -e` cannot do.
# The cost of that choice is that an uncaught command would pass silently, so
# there must be no uncaught command — hence `|| fail` on the cd too.
#
# Honesty about what that cd check does and does not buy, because the first
# version of this comment overstated it: I claimed an unchecked cd would come
# back GREEN having scanned the wrong tree. An independent reviewer disproved
# that from gitleaks' own source — a --config it cannot read is a fatal error,
# not an empty clean scan, so `|| fail` on the gitleaks line was already catching
# it. And in the Actions invocation the step's cwd is already $ROOT before this
# line runs. So the cd check is defense-in-depth against a caller invoking this
# script from somewhere unexpected, NOT the plugging of a live false-green hole.

# A Linux runner would silently behave differently in every gate below, so the
# script refuses rather than reporting a green that means nothing.
[[ "$(uname -s)" == "Darwin" ]] || fail "this subset is macOS-shaped; refusing to pretend on $(uname -s)"

# npm is resolved by absolute-path fallback for the same reason ci.sh does it: a
# minimal-PATH caller must not turn gate 4 into a silent skip. Resolved up here
# rather than next to its use so the script refuses BEFORE spending 4 minutes on
# gate 1 only to discover it cannot run the last one.
NPM="$(command -v npm 2>/dev/null || true)"
if [[ -z "$NPM" ]]; then
  for cand in "$HOME/.asdf/shims/npm" /opt/homebrew/bin/npm /usr/local/bin/npm; do
    [[ -x "$cand" ]] && { NPM="$cand"; break; }
  done
fi
[[ -n "$NPM" && -x "$NPM" ]] || fail "npm not found. It is a HARD dependency of the test:ct gate, never a skip."
FE="$ROOT/frontend"

# ── 1. the bin/ guard suites ────────────────────────────────────────────────
# Dispatcher for the bin/ suites, including macOS-shaped install.sh fixtures.
echo "[ci-macos-host] (1/4) bin/ guard suites — bin/tests/run.sh"
[[ -x "$ROOT/bin/tests/run.sh" ]] || fail "bin/tests/run.sh missing or not executable (renamed? then this gate stopped running)"
bash "$ROOT/bin/tests/run.sh" || fail "bin/tests/run.sh went red. Reproduce: bash bin/tests/run.sh"

# ── 2. content-level secret scan ────────────────────────────────────────────
# The tracked-file PATH denylist already runs on Linux; this is the other half —
# scanning file CONTENTS. Resolved by absolute path fallback for the same reason
# ci.sh does it: a minimal PATH turns the call into exit 127, and a
# command-not-found that is treated as "clean" is the worst possible outcome for
# a secret scanner.
echo "[ci-macos-host] (2/4) gitleaks content scan"
GITLEAKS="$(command -v gitleaks 2>/dev/null || echo /opt/homebrew/bin/gitleaks)"
[[ -x "$GITLEAKS" ]] || fail "gitleaks not found (install: brew install gitleaks). NOT skipped — an unrun scanner and a clean scan look identical."
[[ -f "$ROOT/.gitleaks.toml" ]] || fail ".gitleaks.toml missing — refusing to scan with default rules and call it a pass"
"$GITLEAKS" dir . --no-banner --config .gitleaks.toml || fail "gitleaks found candidate secrets (or errored). Do NOT 'fix' this by allowlisting without reading the finding."

# ── 3. TCC identity anchor ──────────────────────────────────────────────────
# The one owner-approved committed binary. Its manifest binds a reviewable
# source snapshot to the checked-in executable, so this fails closed whenever
# the source moves without an explicit binary refresh.
echo "[ci-macos-host] (3/4) TCC identity anchor — bin/check-officraft-dist"
[[ -x "$ROOT/bin/check-officraft-dist" ]] || fail "bin/check-officraft-dist missing or not executable (renamed? then this gate stopped running)"
"$ROOT/bin/check-officraft-dist" || fail "the committed TCC anchor no longer matches its source manifest. See dist/officraft/BUILD.md"

# ── 4. real-browser layout + paint guards ───────────────────────────────────
# `npm run test:ct` is TWO Playwright configs (see bin/ci.sh step 4c): the CT
# visual guards against a dev server, then the paint guards against a REAL
# `vite build` output served over HTTP. The build is part of the gate, not
# setup — the paint guards measure the shipped artifact's first frames, and
# nothing else in this repo type-checks or exercises the pre-React inline
# script against a real dist/.
#
# Deliberately LAST: it is the newest gate here, and a failing gate aborts the
# ones after it, so the three that were already load-bearing keep reporting for
# themselves.
#
# Browser resolution mirrors ci.sh: `playwright install chromium` is a no-op when
# the pinned revision is cached, and the `|| true` keeps an offline probe from
# failing the run — a genuinely absent browser then fails the test run itself
# (HARD, same discipline as npm/gitleaks: never a silent skip).
echo "[ci-macos-host] (4/4) frontend test:ct — real-browser CT layout guards + paint guards"
[[ -f "$FE/package.json" ]] || fail "frontend/package.json missing — this gate cannot have run"
(cd "$FE" && "$NPM" ci --silent) || fail "npm ci (frontend) failed. Reproduce: (cd frontend && npm ci)"
export PLAYWRIGHT_BROWSERS_PATH="${PLAYWRIGHT_BROWSERS_PATH:-$HOME/Library/Caches/ms-playwright}"
(cd "$FE" && npx --no-install playwright install chromium >/dev/null 2>&1 || true)
(cd "$FE" && "$NPM" run --silent test:ct) || fail "frontend test:ct went red. Reproduce: (cd frontend && npm run test:ct). If it is green on a dev Mac and red only here, the answer is the font environment — NOT a looser threshold."

echo "[ci-macos-host] all macos-host-shaped gates green"
