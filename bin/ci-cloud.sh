#!/usr/bin/env bash
# officraft — the CLOUD-RUNNABLE subset of the quality gate, as ONE entry point.
#
# WHY THIS FILE EXISTS
# bin/ci.sh is the canonical, AUTHORITATIVE land gate and stays that way. But it
# is all-or-nothing — there was no way to say "run everything that a Linux CI
# runner can honestly run" without re-listing the modules, the npm scripts and
# the drift gates somewhere else. GitHub Actions (.github/workflows/ci.yml)
# needs exactly that subset, and the one thing this repo must never grow is a
# SECOND list of what the checks are: a YAML file enumerating them would drift
# from ci.sh the first time something is added, and drift SILENTLY (the missing
# check simply stops running — a green with a hole in it).
#
# So the subset lives HERE, in the repo, in bash, next to ci.sh, and the
# workflow (.github/workflows/ci.yml) does nothing but install a pinned toolchain and call this. Local and
# cloud run the SAME bytes.
#
# NAMING: this was `bin/ci-unit.sh` for exactly one day. The owner then widened
# the scope to "everything that can move to the cloud", which made that name a
# lie — it now runs consistency gates too. Renamed rather than left to rot,
# because a file whose name misdescribes it is the same failure mode as a stale
# doc (§8).
#
# WHAT IS IN:
#   (1) UNIT — the hermetic e2e isolation-guard suite, every Go module under cli/ and server/
#       (`gofmt`, `go vet`, `go build`, then `go test -count=1 ./...`,
#       module set DERIVED from the cli/*/go.mod + server/*/go.mod glob, the same
#       derivation ci.sh uses, so adding a module auto-enrols it in both with no
#       list to keep in sync) + frontend typecheck + vitest.
#   (2) HYGIENE — the tracked-file path denylist. The content-level gitleaks
#       scan deliberately stays local.
#   (3) CONSISTENCY — the regenerate-and-byte-compare drift gates: gen-ocapi
#       (server wire), FE schema.ts (client wire), theme tokens, message keys,
#       font whitelist, plus the two CSS token lints. These are the M1
#       wire-freeze gates.
#       ⚠️ THIS CLASS IS THE TOOLCHAIN-SENSITIVE ONE. Every one of them asserts
#       "regenerating produces byte-identical output", so a generator that
#       behaves differently on a different Node/Go build reddens CI while the
#       CODE IS FINE. That is precisely why the workflow pins go + node to the
#       dev machine's exact versions. If you loosen those pins, expect this
#       class to be where it breaks first.
#   (4) CONFORMANCE — the HTTP-only black-box behaviour suite: it runs against
#       a throwaway ocserverd and SQLite database, so the frozen route/spec
#       surface is checked against live server behaviour without touching prod.
#
# WHAT IS DELIBERATELY OUT, and why (NOT an oversight list):
#   * Playwright CT visual guards — they need a REAL browser and assert
#     real-browser LAYOUT. Font availability and rasterisation differ between
#     macOS and a Linux runner, so this is the textbook false-red; the guard's
#     whole value is that a red means "the layout broke", and a runner would
#     make it mean "the runner has different fonts". Stays in ci.sh (still gated
#     on every land, locally).
#   * gitleaks — the content-level secret scan stays local by design. The
#     tracked-file path denylist runs below because it is safe on Linux.
#   * e2e_test — real-machine end-to-end; it drives launchd/tmux on a real fleet
#     host. Local, pre-release, by design.
#   * the committed-prebuilt parity dryrun — it only applies when a local
#     gitignored prebuilt exists, which is never true on a fresh runner.
#
# -count=1 is load-bearing, exactly as in ci.sh (T-bedc): a cached PASS
# certifies a run that never happened and structurally hides flakes.
#
# Marker: this script prints its OWN completion marker and deliberately never
# emits ci.sh's — that literal is the land authority and bin/tests/
# ci-success-marker.sh requires every non-ci.sh script to be able to emit it
# ZERO times. A green here is a PR-check green, not land authority.
#
#   bash bin/ci-cloud.sh
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

echo "[ci-cloud] start $(date -u '+%Y-%m-%dT%H:%M:%SZ') — $(git -C "$ROOT" rev-parse HEAD 2>/dev/null || echo unknown)"

# --- toolchain resolution (same abspath-fallback discipline as ci.sh: a
# minimal-PATH caller must not silently skip a suite) -------------------------
GO="$(command -v go 2>/dev/null || true)"
if [[ -z "$GO" ]]; then
  for cand in /opt/homebrew/bin/go /usr/local/go/bin/go /usr/local/bin/go; do
    [[ -x "$cand" ]] && { GO="$cand"; break; }
  done
fi
if [[ -z "$GO" || ! -x "$GO" ]]; then
  echo "[ci-cloud] FAIL — go not found. It is a HARD dependency, never a skip."
  exit 1
fi
GOFMT="$(dirname "$GO")/gofmt"
[[ -x "$GOFMT" ]] || GOFMT="$(command -v gofmt 2>/dev/null || echo gofmt)"
NPM="$(command -v npm 2>/dev/null || true)"
if [[ -z "$NPM" ]]; then
  for cand in "$HOME/.asdf/shims/npm" /opt/homebrew/bin/npm /usr/local/bin/npm; do
    [[ -x "$cand" ]] && { NPM="$cand"; break; }
  done
fi
if [[ -z "$NPM" || ! -x "$NPM" ]]; then
  echo "[ci-cloud] FAIL — npm not found. It is a HARD dependency, never a skip."
  exit 1
fi
FE="$ROOT/frontend"

# ===========================================================================
# (1/4) UNIT
# ===========================================================================
# Stage the embed assets FIRST (T-e731). seeds/*.md, docs/guide, the prebuilt
# ocwarden/ocagent and spec/mcp-catalog.json are served EMBED-ONLY, and a clean
# checkout carries .gitkeep-only seedsdist/docsdist/bindist — so server/ocserverd's
# unit tests (they boot and read through the real embed) go red on a clean
# checkout unless these run. A CI runner is by definition always a clean
# checkout, so this is not optional here.
echo "[ci-cloud] (1/4) unit — hermetic isolation guard, staging embed assets, then Go + frontend"
echo "[ci-cloud]   e2e_test isolation-guard unit tests (hermetic)"
bash "$ROOT/e2e_test/tests_guard/run.sh"
PATH="$(dirname "$GO"):$PATH" bash "$ROOT/bin/build-seedsdist"
PATH="$(dirname "$GO"):$PATH" bash "$ROOT/bin/build-docsdist"
PATH="$(dirname "$GO"):$PATH" bash "$ROOT/bin/build-bindist"

for gomod in "$ROOT"/cli/*/go.mod "$ROOT"/server/*/go.mod; do
  [[ -f "$gomod" ]] || continue
  mod_dir="$(dirname "$gomod")"
  module="${mod_dir#"$ROOT"/}"
  binary="$(basename "$mod_dir")"
  echo "[ci-cloud]   gofmt + vet + build + test $module"
  if ! grep -qE "^module ${binary}$" "$mod_dir/go.mod"; then
    echo "[ci-cloud] FAIL — naming: $module/go.mod must declare module $binary"
    exit 1
  fi
  (
    cd "$mod_dir"
    unformatted="$("$GOFMT" -l . 2>/dev/null || true)"
    if [[ -n "$unformatted" ]]; then
      echo "[ci-cloud] FAIL — gofmt: unformatted golang files in $module:"
      printf '  %s\n' $unformatted
      exit 1
    fi
    "$GO" vet ./...
    "$GO" build -o "$binary" ./...
    "$GO" test -count=1 ./...
  )
done

echo "[ci-cloud]   npm ci (frontend)"
(cd "$FE" && "$NPM" ci --silent)
echo "[ci-cloud]   tsc --noEmit (frontend typecheck)"
(cd "$FE" && "$NPM" run --silent typecheck)
echo "[ci-cloud]   vitest run (frontend unit suite)"
(cd "$FE" && "$NPM" test)

# ===========================================================================
# (2/4) HYGIENE — tracked-file path denylist
# ===========================================================================
echo "[ci-cloud] (2/4) hygiene — tracked-file path denylist"
denylist_hits="$(
  git ls-files -z | tr '\0' '\n' | grep -iE \
    -e '(^|/)scratchpad/' \
    -e '\.bak$' \
    -e '\.pem$' \
    -e '\.key$' \
    -e '\.secret$' \
    -e '(^|/)oc\.toml$' \
    -e '(^|/)oc\.lock$' \
    | { grep -vE '\.py$' || true; }
  git ls-files -z | tr '\0' '\n' | grep -iE '_token' | grep -vE '\.py$' || true
)"
denylist_hits="$(printf '%s\n' "$denylist_hits" | grep -vE '^$' | sort -u || true)"
if [[ -n "$denylist_hits" ]]; then
  echo "[ci-cloud] FAIL — forbidden files are tracked (path denylist):"
  printf '  %s\n' $denylist_hits
  exit 1
fi

# ===========================================================================
# (3/4) CONSISTENCY — regenerate-and-byte-compare drift gates
# ===========================================================================
# Every gate below is the same shape: run the generator against its frozen
# source, require the COMMITTED generated artifact to come back byte-identical.
# Several regenerate IN PLACE, so those snapshot the committed bytes first and
# restore them on failure — a red must not leave a mutated worktree behind.
echo "[ci-cloud] (3/4) consistency — wire freeze + generated-artifact drift gates"

# 2a. gen-ocapi drift — the wire-freeze gate on the SERVER's REST surface.
# server/ocserverd/ocapi_gen.go is generated from the frozen spec/openapi.json.
echo "[ci-cloud]   gen-ocapi drift: regenerate ocapi_gen.go from spec/openapi.json + diff committed"
FRESH_OCAPI="$(mktemp -t oc-fresh-ocapi.XXXXXX)"
PATH="$(dirname "$GO"):$PATH" "$ROOT/bin/gen-ocapi" "$FRESH_OCAPI" >/dev/null
if ! diff -u "$ROOT/server/ocserverd/ocapi_gen.go" "$FRESH_OCAPI"; then
  echo "[ci-cloud] FAIL — gen-ocapi drift: server/ocserverd/ocapi_gen.go is STALE vs spec/openapi.json."
  echo "[ci-cloud] wire 已凍結 (M1): spec-first — regenerate + commit: bash bin/gen-ocapi"
  rm -f "$FRESH_OCAPI"
  exit 1
fi
rm -f "$FRESH_OCAPI"

# 2b. CSS colour-token lint + token ROLE lint (T-16a1 / T-081b). Pure static
# lints over the committed CSS — no generator, no toolchain sensitivity.
echo "[ci-cloud]   css colour-token lint (no raw literals outside theme.css — T-16a1)"
(cd "$FE" && "$NPM" run --silent lint:tokens)
echo "[ci-cloud]   token role lint (the T-081b splits stay split — T-081b)"
(cd "$FE" && "$NPM" run --silent lint:token-roles)

# regen_pair_gate <label> <npm-script> <file-a> <file-b> — snapshot the two
# committed generated files, re-run the generator, require both byte-identical,
# and RESTORE the snapshots before failing. One helper instead of three
# copy-pasted blocks: the three gates below differ only in their inputs, and
# three near-identical 15-line blocks is exactly how one of them silently loses
# its restore during a later edit.
regen_pair_gate() {
  local label="$1" script="$2" a="$3" b="$4"
  local bak_a bak_b
  bak_a="$(mktemp -t oc-regen-a.XXXXXX)"; bak_b="$(mktemp -t oc-regen-b.XXXXXX)"
  cp "$a" "$bak_a"; cp "$b" "$bak_b"
  (cd "$FE" && "$NPM" run --silent "$script" >/dev/null)
  if ! diff -u "$bak_a" "$a" || ! diff -u "$bak_b" "$b"; then
    echo "[ci-cloud] FAIL — $label drift: the generated files are STALE vs their source."
    echo "[ci-cloud] regenerate + commit: (cd frontend && npm run $script) then git add both generated files"
    cp "$bak_a" "$a"; cp "$bak_b" "$b"
    rm -f "$bak_a" "$bak_b"
    exit 1
  fi
  rm -f "$bak_a" "$bak_b"
}

echo "[ci-cloud]   theme-token whitelist drift: regenerate from theme.css + diff committed (T-16a1 P2)"
regen_pair_gate "theme-token whitelist" gen:tokens \
  "$FE/src/styles/themeTokens.generated.ts" "$ROOT/server/ocserverd/theme_colornames_gen.go"

echo "[ci-cloud]   message-key whitelist drift: regenerate from locales/en.ts + diff committed (T-16a1 P3)"
regen_pair_gate "message-key whitelist" gen:msgkeys \
  "$FE/src/i18n/messageKeys.generated.ts" "$ROOT/server/ocserverd/message_keys_gen.go"

echo "[ci-cloud]   font whitelist drift: regenerate from themeFonts.source.json + diff committed (T-16a1 P4)"
regen_pair_gate "font whitelist" gen:fonts \
  "$FE/src/styles/themeFonts.generated.ts" "$ROOT/server/ocserverd/theme_fonts_gen.go"

# 2c. FE contract drift — the wire-freeze gate on the CLIENT surface.
echo "[ci-cloud]   contract drift: regenerate schema from spec/openapi.json + diff committed"
FRESH_TS="$(mktemp -t oc-fresh-schema.XXXXXX)"
(cd "$FE" && npx --no-install openapi-typescript "$ROOT/spec/openapi.json" -o "$FRESH_TS")
if ! diff -u "$FE/src/api/generated/schema.ts" "$FRESH_TS"; then
  echo "[ci-cloud] FAIL — contract drift: frontend/src/api/generated/schema.ts is STALE vs spec/openapi.json."
  echo "[ci-cloud] regenerate + commit: (cd frontend && npm run gen:api)"
  rm -f "$FRESH_TS"
  exit 1
fi
rm -f "$FRESH_TS"

# ===========================================================================
# (4/4) CONFORMANCE — black-box HTTP behaviour guard
# ===========================================================================
# Keep the fast import guard explicit here as well as the full runner below:
# a black-box violation should fail before we spend time building the isolated
# server. The full runner then verifies the frozen route/spec surface against
# live behaviour on a throwaway server and SQLite database.
echo "[ci-cloud] (4/4) conformance — black-box lint + isolated HTTP behaviour suite"
conf_hits="$(grep -RInE --include='*.py' \
  '^[[:space:]]*(import|from)[[:space:]]+(backend|service|dal|domain|plumbing)([.[:space:]]|$)' \
  "$ROOT/conformance" || true)"
if [[ -n "$conf_hits" ]]; then
  echo "[ci-cloud] FAIL — conformance black-box violation (suite must stay HTTP-only):"
  printf '  %s\n' "$conf_hits"
  echo "[ci-cloud] conformance tests speak ONLY HTTP to \$OC_TARGET_URL (see conformance/CLAUDE.md)."
  exit 1
fi

if ! GOTOOLCHAIN="${GOTOOLCHAIN:-auto}" PATH="$(dirname "$GO"):$PATH" \
    "$ROOT/conformance/run.sh" --target go; then
  echo "[ci-cloud] FAIL — conformance suite went red. Reproduce: bash conformance/run.sh --target go"
  exit 1
fi

echo "[ci-cloud] all cloud-runnable gates green"
