#!/usr/bin/env bash
# officraft — the UNIT-TEST subset of the quality gate, as ONE entry point.
#
# WHY THIS FILE EXISTS
# bin/ci.sh is the canonical, AUTHORITATIVE gate and stays that way: it is the
# only thing whose green certifies a land. But it is all-or-nothing — there was
# no way to say "just run the unit suites" without re-listing the modules and
# the npm scripts somewhere else. GitHub Actions (.github/workflows/unit.yml)
# needs exactly that subset, and the one thing this repo must never grow is a
# SECOND list of what the tests are: a YAML file enumerating modules/scripts
# would drift from ci.sh the first time a module is added, and drift silently
# (the missing module simply stops being tested — a green with a hole in it).
#
# So the subset lives HERE, in the repo, in bash, next to ci.sh, and the
# workflow does nothing but call it. Local and cloud run the SAME bytes.
#
# WHAT IS IN (the two suites the owner asked for — frontend + backend units):
#   * every Go module under cli/ and server/ — `go test -count=1 ./...`,
#     module set DERIVED from the cli/*/go.mod + server/*/go.mod glob (the same
#     derivation ci.sh uses; adding a module auto-enrols it in both, by
#     construction, with no list to keep in sync)
#   * frontend — `npm run typecheck` + `npm test` (vitest run)
#
# WHAT IS DELIBERATELY OUT, and why (this is NOT an oversight list):
#   * the regenerate-and-byte-compare drift gates (gen-ocapi, FE schema.ts,
#     theme tokens, message keys, font whitelist). They are wire-freeze gates,
#     not unit tests, and they are exquisitely sensitive to toolchain version —
#     the one class most likely to be red in the cloud while the code is
#     perfectly fine. ci.sh keeps them; a PR check must not re-litigate them.
#   * gitleaks / path denylist — repo hygiene, not a unit test.
#   * the conformance suite and e2e_test — black-box/integration, not units.
#   * Playwright CT visual guards — they need a REAL browser and assert
#     real-browser layout; that is a rendering guard, not a unit test, and
#     font/rasterisation differences between macOS and a Linux runner are the
#     textbook false-red. Stays in ci.sh (i.e. still gated locally).
#   * gofmt / go vet — style + static analysis, not units. (`go test` compiles
#     the test files anyway, so test-compilation breakage is still caught.)
#
# -count=1 is load-bearing, exactly as in ci.sh (T-bedc): a cached PASS
# certifies a run that never happened and structurally hides flakes.
#
# Marker: this script prints its OWN completion marker and deliberately never
# emits ci.sh's — that literal is the land authority and bin/tests/
# ci-success-marker.sh requires every non-ci.sh script to be able to emit it
# ZERO times. A green here is a PR-check green, not land authority.
#
#   bash bin/ci-unit.sh
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

echo "[ci-unit] start $(date -u '+%Y-%m-%dT%H:%M:%SZ') — $(git -C "$ROOT" rev-parse HEAD 2>/dev/null || echo unknown)"

# --- toolchain resolution (same abspath-fallback discipline as ci.sh: a
# minimal-PATH caller must not silently skip a suite) -------------------------
GO="$(command -v go 2>/dev/null || true)"
if [[ -z "$GO" ]]; then
  for cand in /opt/homebrew/bin/go /usr/local/go/bin/go /usr/local/bin/go; do
    [[ -x "$cand" ]] && { GO="$cand"; break; }
  done
fi
if [[ -z "$GO" || ! -x "$GO" ]]; then
  echo "[ci-unit] FAIL — go not found. It is a HARD dependency, never a skip."
  exit 1
fi
NPM="$(command -v npm 2>/dev/null || true)"
if [[ -z "$NPM" ]]; then
  for cand in "$HOME/.asdf/shims/npm" /opt/homebrew/bin/npm /usr/local/bin/npm; do
    [[ -x "$cand" ]] && { NPM="$cand"; break; }
  done
fi
if [[ -z "$NPM" || ! -x "$NPM" ]]; then
  echo "[ci-unit] FAIL — npm not found. It is a HARD dependency, never a skip."
  exit 1
fi

# --- (1) backend: Go unit suites --------------------------------------------
# Stage the embed assets FIRST (T-e731). seeds/*.md, docs/guide, the prebuilt
# ocwarden/ocagent and spec/mcp-catalog.json are served EMBED-ONLY, and a clean
# checkout carries .gitkeep-only seedsdist/docsdist/bindist — so server/ocserverd's
# unit tests (they boot and read through the real embed) go red on a clean
# checkout unless these run. A CI runner is by definition always a clean
# checkout, so this is not optional here.
echo "[ci-unit] (1/2) backend — staging embed assets, then go test per module"
PATH="$(dirname "$GO"):$PATH" bash "$ROOT/bin/build-seedsdist"
PATH="$(dirname "$GO"):$PATH" bash "$ROOT/bin/build-docsdist"
PATH="$(dirname "$GO"):$PATH" bash "$ROOT/bin/build-bindist"

for gomod in "$ROOT"/cli/*/go.mod "$ROOT"/server/*/go.mod; do
  [[ -f "$gomod" ]] || continue
  mod_dir="$(dirname "$gomod")"
  echo "[ci-unit]   go test ${mod_dir#"$ROOT"/}"
  (cd "$mod_dir" && "$GO" test -count=1 ./...)
done

# --- (2) frontend: typecheck + vitest ----------------------------------------
echo "[ci-unit] (2/2) frontend — npm ci + tsc --noEmit + vitest run"
FE="$ROOT/frontend"
(cd "$FE" && "$NPM" ci --silent)
(cd "$FE" && "$NPM" run --silent typecheck)
(cd "$FE" && "$NPM" test)

echo "[ci-unit] all unit suites green"
