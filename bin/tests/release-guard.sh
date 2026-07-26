#!/usr/bin/env bash
# bin/tests/release-guard.sh — HERMETIC unit tests for bin/release (T-588c).
#
# WHAT IS ACTUALLY UNDER TEST
# ---------------------------
# `bin/release publish` exists because the old form built an artifact and then
# PRINTED a `gh release create` line for a human to paste, so every interesting
# failure lived downstream of anything that could observe it: a half-uploaded
# asset set, a release pinned to the wrong commit, a draft nobody can consume, a
# station that never moved onto the new build. The fix is that publish READS BACK
# what GitHub stored and what the station reports, and FAILS NAMING THE ITEM.
#
# That promise is only worth what its failure paths are worth, and a failure path
# nobody exercises is indistinguishable from a missing one. Every assertion below
# is therefore aimed at a NEGATIVE: given a stored release that violates exactly
# one requirement, does the command exit non-zero AND name that requirement? If
# any single check in verify_stored_release / verify_artifacts / settle_station is
# deleted, a case here goes red — that is the whole design.
#
# WHY IT BINDS TO THE REAL FUNCTIONS
# ----------------------------------
# The obvious way to test read-back rules is to re-state them in the test. Then
# the test passes forever while the real rules rot, which is the exact class of
# bug this ticket is about. So this file `source`s bin/release (its dispatch is
# guarded on "am I being executed") and calls verify_stored_release,
# verify_artifacts and settle_station DIRECTLY. There is no second copy of the
# rules anywhere in here.
#
# HERMETICITY — nothing real is touched
# -------------------------------------
#   * `gh` is a PATH shim. It NEVER reaches the network. `gh release create` is
#     the irreversible point of the whole system, so the shim records the call to
#     a tripwire and creates nothing; the dry-run cases assert that tripwire is
#     EMPTY, which is the only way to prove --dry-run cannot publish.
#   * `curl` is a PATH shim serving a canned /api/version + /api/health, so no
#     station — least of all a live one — is contacted.
#   * The end-to-end cases run against a THROWAWAY git repo in mktemp that
#     carries its own bin/build. bin/release cuts its staging worktree from
#     OC_RELEASE_SRC, so this exercises the real staging + packaging + verify +
#     upload + read-back + settle arc without this repo, this worktree, or any
#     npm/go build of the actual product being involved.
#   * Artifacts land in a mktemp OC_RELEASE_OUT, never dist/release/.
#
# `go` IS required (a real Mach-O arm64 binary with real linker flags is the only
# thing that can exercise the version-stamp check, which reads `go version -m`).
# It is resolved by absolute path the same way bin/ci.sh does it, because the
# launchd autodeploy job runs with a minimal PATH.
set -uo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
RELEASE="$HERE/../release"
[[ -f "$RELEASE" ]] || { echo "FATAL: script not found at $RELEASE" >&2; exit 2; }

PASS=0; FAIL=0
ok()   { PASS=$((PASS+1)); printf '  ok   — %s\n' "$1"; }
bad()  { FAIL=$((FAIL+1)); printf '  FAIL — %s\n' "$1"; }
check(){ # check DESC EXPECTED ACTUAL
  if [[ "$2" == "$3" ]]; then ok "$1"; else bad "$1 (want '$2' got '$3')"; fi
}
# named_failure DESC WANT_ITEM RC OUT — the shape EVERY verification failure must
# have. Two halves, deliberately inseparable: a non-zero exit (6, the verify
# code) AND a line naming the item. A non-zero exit with no item name is the old
# world, where the operator knew something broke and not what.
named_failure() {
  local desc="$1" item="$2" rc="$3" out="$4"
  if [[ "$rc" == "0" ]]; then
    bad "$desc — exited 0; the violation was ACCEPTED (out: $(printf '%s' "$out" | tr '\n' '|'))"
    return
  fi
  if [[ "$rc" != "6" ]]; then
    bad "$desc — exited $rc, expected the verify code 6 (out: $(printf '%s' "$out" | tr '\n' '|'))"
    return
  fi
  case "$out" in
    *"VERIFY-FAILED [$item]"*) ok "$desc — exit 6, named [$item]" ;;
    *) bad "$desc — exit 6 but did NOT name [$item] (out: $(printf '%s' "$out" | tr '\n' '|'))" ;;
  esac
}

GO="$(command -v go 2>/dev/null || true)"
if [[ -z "$GO" ]]; then
  for cand in /opt/homebrew/bin/go /usr/local/go/bin/go /usr/local/bin/go; do
    [[ -x "$cand" ]] && { GO="$cand"; break; }
  done
fi
[[ -n "$GO" && -x "$GO" ]] || { echo "FATAL: go not found — release-guard needs it to build a real Mach-O fixture" >&2; exit 2; }

WORK="$(mktemp -d -t oc-release-guard.XXXXXX)"
SHIMDIR="$WORK/shim"; mkdir -p "$SHIMDIR"
GHWIRE="$WORK/.gh-wire"        # every gh invocation, one per line
: > "$GHWIRE"

cleanup() { rm -rf "$WORK" 2>/dev/null || true; }
trap cleanup EXIT

# ── PATH shims ───────────────────────────────────────────────────────────────
# gh: `release view` prints $GH_VIEW_JSON (or fails when GH_VIEW_RC is set);
# `release create` / `release edit` only ever record themselves.
cat > "$SHIMDIR/gh" <<'SH'
#!/usr/bin/env bash
echo "gh $*" >> "$GH_WIRE"
if [[ "${1:-}" == "release" && "${2:-}" == "view" ]]; then
  [[ "${GH_VIEW_RC:-0}" == "0" ]] || { echo "release not found" >&2; exit "${GH_VIEW_RC}"; }
  printf '%s' "${GH_VIEW_JSON:-}"
  exit 0
fi
if [[ "${1:-}" == "release" && ( "${2:-}" == "create" || "${2:-}" == "edit" ) ]]; then
  exit "${GH_WRITE_RC:-0}"
fi
echo "unexpected gh invocation: $*" >&2
exit 99
SH
# curl: serves the canned station. Only the two URLs bin/release polls exist;
# anything else 404s, so a new unreviewed network call cannot silently pass.
cat > "$SHIMDIR/curl" <<'SH'
#!/usr/bin/env bash
url="${!#}"
case "$url" in
  */api/version) printf '%s' "${STATION_VERSION_JSON:-}"; exit "${STATION_VERSION_RC:-0}" ;;
  */api/health)  printf 'ok';                             exit "${STATION_HEALTH_RC:-0}" ;;
esac
exit 22
SH
chmod +x "$SHIMDIR/gh" "$SHIMDIR/curl"

export GH_WIRE="$GHWIRE"

# release_fn — run ONE function out of the real bin/release, in a subshell so the
# script's `set -e` and its fail_item `exit 6` land on the case and not on this
# harness. OUT captures stdout+stderr together on purpose: the read-back prints
# its machine-readable answer on stdout and its complaints on stderr, and a case
# that only looked at one of them could not tell "named the item" from "said
# nothing".
release_fn() { # release_fn <fn> [args...]
  OUT="$(PATH="$SHIMDIR:$PATH" bash -c '
    set -uo pipefail
    source "$1" || exit $?
    fn="$2"; shift 2
    "$fn" "$@"
  ' _ "$RELEASE" "$@" 2>&1)"
  RC=$?
}
# release_cli — run bin/release as a real command, shims on PATH.
release_cli() {
  OUT="$(PATH="$SHIMDIR:$PATH" bash "$RELEASE" "$@" 2>&1)"
  RC=$?
}

# stored_json — the payload the gh shim will hand back. Defaults describe a
# CORRECT prerelease; each case overrides exactly ONE field, so a red case points
# at one rule rather than at "something in here is wrong".
#
# THE FIXTURE IS MEASURED AGAINST REAL GITHUB, NOT INVENTED (2026-07-26). A shim
# that returns a made-up shape tests the code against the test author's guess, and
# stays green forever while the real payload differs — which is exactly how the
# `file -b` arch check shipped broken (it matched an ordering `file` never emits).
# So this fixture mirrors an actual `gh release view --json
# assets,isDraft,isPrerelease,targetCommitish` run on pkyosx/OffiCraft v0.5.38:
#
#   isDraft False | isPrerelease True | targetCommitish fb89a69aad8c
#   {'name': 'checksums.txt',                          'state': 'uploaded', 'size': 181}
#   {'name': 'install.sh',                             'state': 'uploaded', 'size': 70730}
#   {'name': 'officraft-v0.5.38-darwin-arm64.tar.gz',  'state': 'uploaded', 'size': 16842394}
#
# Confirmed by that run: the asset sub-fields name/state/size exist, `state` is the
# literal string "uploaded", and `size` is a non-zero integer — i.e. the three
# things verify_stored_release actually keys off. Re-measure before changing the
# shape here; do not "tidy" it to match what the code expects.
TAG="v9.9.9-guard"
SHA="0123456789abcdef0123456789abcdef01234567"
TARBALL="officraft-$TAG-darwin-arm64.tar.gz"
stored_json() { # stored_json [key=value ...]  (values are raw JSON)
  python3 - "$@" <<PY
import json, sys
rel = {
  "tagName": "$TAG", "isDraft": False, "isPrerelease": True,
  "targetCommitish": "$SHA",
  "assets": [
    {"name": "$TARBALL", "state": "uploaded", "size": 4096},
    {"name": "install.sh",   "state": "uploaded", "size": 2048},
    {"name": "checksums.txt","state": "uploaded", "size": 128},
  ],
}
for arg in sys.argv[1:]:
    k, _, v = arg.partition("=")
    rel[k] = json.loads(v)
print(json.dumps(rel))
PY
}
# asset_list — same helper for the asset array alone.
assets_json() { python3 -c 'import json,sys; print(json.dumps(json.loads(sys.argv[1])))' "$1"; }

echo "bin/release hermetic tests (no network, no gh, no station, no product build)"

# ═══════════════════════════════════════════════════════════════════════════
# S — READ-BACK: verify_stored_release. One case per DoD item.
# ═══════════════════════════════════════════════════════════════════════════
echo "── S: read-back of what GitHub stored"

export GH_VIEW_RC=0
export GH_VIEW_JSON="$(stored_json)"
release_fn verify_stored_release "$TAG" "$SHA" prerelease
check "S0 a correct prerelease reads back clean" "0" "$RC"
# The function's CONTRACT is its stdout: line 1 = targetCommitish, lines 2+ = the
# asset fingerprint. promote compares two of these to detect a re-upload, so a
# silent change to this shape would break drift detection while every other
# assertion stayed green.
check "S0 line 1 of the contract is the targetCommitish" "$SHA" "$(printf '%s\n' "$OUT" | grep -v '^\[release\]' | sed -n '1p')"
check "S0 the fingerprint is name<TAB>size for all 3 assets, sorted" \
  "checksums.txt	128
install.sh	2048
$TARBALL	4096" \
  "$(printf '%s\n' "$OUT" | grep -v '^\[release\]' | sed -n '2,$p')"

GH_VIEW_RC=1 GH_VIEW_JSON="" release_fn verify_stored_release "$TAG" "$SHA" prerelease
named_failure "S1 gh release view FAILS (no such release)" release-exists "$RC" "$OUT"

GH_VIEW_RC=0 GH_VIEW_JSON="" release_fn verify_stored_release "$TAG" "$SHA" prerelease
named_failure "S2 gh release view returns NOTHING" release-exists "$RC" "$OUT"

GH_VIEW_JSON="$(stored_json 'tagName="v0.0.0-other"')" \
  release_fn verify_stored_release "$TAG" "$SHA" prerelease
named_failure "S3 GitHub answered about a DIFFERENT tag" release-tag "$RC" "$OUT"

GH_VIEW_JSON="$(stored_json 'isDraft=true')" \
  release_fn verify_stored_release "$TAG" "$SHA" prerelease
named_failure "S4 the release is a DRAFT (invisible to every consumer)" release-draft "$RC" "$OUT"

GH_VIEW_JSON="$(stored_json 'isPrerelease=false')" \
  release_fn verify_stored_release "$TAG" "$SHA" prerelease
named_failure "S5 publish stored a FINAL release, not a prerelease" release-channel "$RC" "$OUT"

GH_VIEW_JSON="$(stored_json 'isPrerelease=true')" \
  release_fn verify_stored_release "$TAG" "$SHA" final
named_failure "S6 promote left it a PRERELEASE" release-channel "$RC" "$OUT"

GH_VIEW_JSON="$(stored_json 'targetCommitish=""')" \
  release_fn verify_stored_release "$TAG" "$SHA" prerelease
named_failure "S7 EMPTY targetCommitish (cannot say which commit shipped)" release-target "$RC" "$OUT"

GH_VIEW_JSON="$(stored_json 'targetCommitish="deadbeefdeadbeefdeadbeefdeadbeefdeadbeef"')" \
  release_fn verify_stored_release "$TAG" "$SHA" prerelease
named_failure "S8 the release is bound to the WRONG commit" release-target "$RC" "$OUT"

# The defect that motivated the ticket: the tarball uploaded, install.sh did not,
# and the release existed anyway with a documented install one-liner that 404s.
GH_VIEW_JSON="$(stored_json "assets=$(assets_json '[
  {"name":"'"$TARBALL"'","state":"uploaded","size":4096},
  {"name":"checksums.txt","state":"uploaded","size":128}]')")" \
  release_fn verify_stored_release "$TAG" "$SHA" prerelease
named_failure "S9 an asset is MISSING (half-populated release)" asset-present "$RC" "$OUT"

GH_VIEW_JSON="$(stored_json "assets=$(assets_json '[
  {"name":"'"$TARBALL"'","state":"starter","size":4096},
  {"name":"install.sh","state":"uploaded","size":2048},
  {"name":"checksums.txt","state":"uploaded","size":128}]')")" \
  release_fn verify_stored_release "$TAG" "$SHA" prerelease
named_failure "S10 an asset is present but state!=uploaded (not downloadable)" asset-uploaded "$RC" "$OUT"

GH_VIEW_JSON="$(stored_json "assets=$(assets_json '[
  {"name":"'"$TARBALL"'","state":"uploaded","size":0},
  {"name":"install.sh","state":"uploaded","size":2048},
  {"name":"checksums.txt","state":"uploaded","size":128}]')")" \
  release_fn verify_stored_release "$TAG" "$SHA" prerelease
named_failure "S11 an asset is ZERO BYTES" asset-uploaded "$RC" "$OUT"

GH_VIEW_JSON="$(stored_json "assets=$(assets_json '[
  {"name":"'"$TARBALL"'","state":"uploaded","size":4096},
  {"name":"install.sh","state":"uploaded","size":2048},
  {"name":"checksums.txt","state":"uploaded","size":128},
  {"name":"officraft-v9.9.9-guard-darwin-arm64.tar.gz.OLD","state":"uploaded","size":9}]')")" \
  release_fn verify_stored_release "$TAG" "$SHA" prerelease
named_failure "S12 an UNEXPECTED extra asset (somebody uploaded behind us)" asset-extra "$RC" "$OUT"

# An empty expected-target means "whatever GitHub has, as long as it has one" —
# the shape promote uses before it knows the target. It must still reject blanks
# (S7) but accept any concrete value.
GH_VIEW_JSON="$(stored_json 'targetCommitish="somebranchsha"')" \
  release_fn verify_stored_release "$TAG" "" prerelease
check "S13 an empty EXPECTED target accepts any concrete stored target" "0" "$RC"

# ═══════════════════════════════════════════════════════════════════════════
# T — SETTLE: the station is actually running the release.
# ═══════════════════════════════════════════════════════════════════════════
echo "── T: settle (the live station moved onto this commit)"
SHORT="${SHA:0:7}"
export OC_RELEASE_SITE="http://127.0.0.1:1"   # unreachable on purpose; curl is shimmed
export OC_RELEASE_SETTLE_TRIES=2 OC_RELEASE_SETTLE_SLEEP=0

STATION_VERSION_JSON="{\"git_sha\":\"$SHORT\"}" STATION_HEALTH_RC=0 \
  release_fn settle_station "$SHA" "$SHORT"
check "T0 station reports the target git_sha and healthy → settles" "0" "$RC"

STATION_VERSION_JSON='{"git_sha":"aaaaaaa"}' \
  release_fn settle_station "$SHA" "$SHORT"
named_failure "T1 station is on a DIFFERENT commit" station-settle "$RC" "$OUT"

STATION_VERSION_JSON="{\"git_sha\":\"$SHORT\"}" STATION_HEALTH_RC=7 \
  release_fn settle_station "$SHA" "$SHORT"
named_failure "T2 station is on the right commit but /api/health is down" station-health "$RC" "$OUT"

# The prefix comparison is `$SHA == $seen*`, so a SHORT-ENOUGH answer would match
# by accident — "0" prefixes this sha. The >=7 length floor is the only thing
# stopping a truncated or placeholder field from reading as a successful deploy.
STATION_VERSION_JSON='{"git_sha":"0"}' \
  release_fn settle_station "$SHA" "$SHORT"
named_failure "T3 a 1-char git_sha that PREFIX-MATCHES is still rejected" station-settle "$RC" "$OUT"

STATION_VERSION_JSON='{}' release_fn settle_station "$SHA" "$SHORT"
named_failure "T4 /api/version has no git_sha field at all" station-settle "$RC" "$OUT"

STATION_VERSION_JSON='not json at all' release_fn settle_station "$SHA" "$SHORT"
named_failure "T5 /api/version is not JSON" station-settle "$RC" "$OUT"

STATION_VERSION_RC=7 STATION_VERSION_JSON='' release_fn settle_station "$SHA" "$SHORT"
named_failure "T6 /api/version cannot be reached at all" station-settle "$RC" "$OUT"

unset OC_RELEASE_SITE OC_RELEASE_SETTLE_TRIES OC_RELEASE_SETTLE_SLEEP

# ═══════════════════════════════════════════════════════════════════════════
# A — PRE-UPLOAD artifact verification.
# ═══════════════════════════════════════════════════════════════════════════
# These run against a hand-built OUT dir, so each case can violate exactly one
# property. The Go fixture is real: `go version -m` reads the LINKER FLAGS, which
# is the only evidence that the tag/commit a release CLAIMS is the tag/commit
# compiled into it — a grep over the tarball would "pass" on an unstamped binary
# because README.md inside the tarball also contains the tag.
echo "── A: pre-upload artifact verification"
GOSRC="$WORK/gosrc"; mkdir -p "$GOSRC"
cat > "$GOSRC/main.go" <<'GO'
package main

var appVersion = "0.0.0"
var buildSHA = "unknown"

func main() { println(appVersion, buildSHA) }
GO
( cd "$GOSRC" && "$GO" mod init ocfixture >/dev/null 2>&1 )
build_fixture() { # build_fixture <out> [ldflags]
  ( cd "$GOSRC" && "$GO" build -ldflags "${2:-}" -o "$1" . ) \
    || { echo "FATAL: go build of the fixture failed" >&2; exit 2; }
}
STAMPED="$WORK/ocserverd.stamped"
build_fixture "$STAMPED" "-X main.appVersion=$TAG -X main.buildSHA=$SHORT"
PLAIN="$WORK/ocserverd.plain"
build_fixture "$PLAIN" ""

# make_out — assemble a well-formed OUT dir, then let the caller break one thing.
AOUT="$WORK/out"
make_out() { # make_out <serverd-binary> [member-to-omit]
  rm -rf "$AOUT" "$WORK/pkg"; mkdir -p "$AOUT" "$WORK/pkg/officraft-$TAG-darwin-arm64"
  local pkg="$WORK/pkg/officraft-$TAG-darwin-arm64" skip="${2:-}"
  cp "$1" "$pkg/ocserverd"
  cp "$STAMPED" "$pkg/ocwarden"; cp "$STAMPED" "$pkg/ocagent"
  printf '#!/bin/sh\n' > "$pkg/install.sh"; printf 'MIT\n' > "$pkg/LICENSE"
  printf '# OffiCraft %s\n' "$TAG" > "$pkg/README.md"
  [[ -n "$skip" ]] && rm -f "$pkg/$skip"
  tar -C "$WORK/pkg" -czf "$AOUT/$TARBALL" "officraft-$TAG-darwin-arm64"
  cp "$pkg/install.sh" "$AOUT/install.sh" 2>/dev/null || printf '#!/bin/sh\n' > "$AOUT/install.sh"
  ( cd "$AOUT" && shasum -a 256 "$TARBALL" install.sh > checksums.txt )
}
verify_out() { # verify_out — call the real verify_artifacts against $AOUT
  OUT="$(PATH="$SHIMDIR:$PATH" OC_RELEASE_OUT="$AOUT" bash -c '
    set -uo pipefail
    source "$1" || exit $?
    verify_artifacts "$2" "$3" "$4" "$5"
  ' _ "$RELEASE" "$TAG" "$SHA" "$SHORT" "officraft-$TAG-darwin-arm64" 2>&1)"
  RC=$?
}

make_out "$STAMPED"
verify_out
check "A0 a correct artifact set verifies clean" "0" "$RC"

make_out "$STAMPED"; rm -f "$AOUT/install.sh"
verify_out
named_failure "A1 an expected artifact is missing from the output dir" artifact-present "$RC" "$OUT"

make_out "$STAMPED"; : > "$AOUT/checksums.txt"
verify_out
named_failure "A2 an artifact is empty" artifact-nonempty "$RC" "$OUT"

# A tarball missing ocwarden is a broken install, not a cosmetic defect: the
# installer lays down a warden that does not exist.
make_out "$STAMPED" ocwarden
verify_out
named_failure "A3 the tarball is MISSING a member (ocwarden)" tarball-members "$RC" "$OUT"

# …and the other direction: a stray file that sneaked into the package is equally
# a wrong tarball. The member list is asserted EXACTLY, not as a subset.
make_out "$STAMPED"
printf 'oops\n' > "$WORK/pkg/officraft-$TAG-darwin-arm64/scratch.log"
tar -C "$WORK/pkg" -czf "$AOUT/$TARBALL" "officraft-$TAG-darwin-arm64"
( cd "$AOUT" && shasum -a 256 "$TARBALL" install.sh > checksums.txt )
verify_out
named_failure "A3b the tarball grew a STRAY member" tarball-members "$RC" "$OUT"

make_out "$STAMPED"
printf 'not a binary' > "$WORK/pkg/officraft-$TAG-darwin-arm64/ocagent"
tar -C "$WORK/pkg" -czf "$AOUT/$TARBALL" "officraft-$TAG-darwin-arm64"
( cd "$AOUT" && shasum -a 256 "$TARBALL" install.sh > checksums.txt )
verify_out
named_failure "A4 a shipped binary is not a darwin/arm64 Mach-O" artifact-arch "$RC" "$OUT"

make_out "$PLAIN"
verify_out
named_failure "A5 ocserverd carries NO version stamp" version-stamp "$RC" "$OUT"

build_fixture "$WORK/wrongver" "-X main.appVersion=v0.0.0-wrong -X main.buildSHA=$SHORT"
make_out "$WORK/wrongver"
verify_out
named_failure "A6 ocserverd is stamped with the WRONG appVersion" version-stamp "$RC" "$OUT"

build_fixture "$WORK/wrongsha" "-X main.appVersion=$TAG -X main.buildSHA=beefbee"
make_out "$WORK/wrongsha"
verify_out
named_failure "A7 ocserverd is stamped with the WRONG buildSHA" version-stamp "$RC" "$OUT"

# A checksums file covering ONE file is how install.sh's sha256 step starts
# silently verifying nothing.
make_out "$STAMPED"
( cd "$AOUT" && shasum -a 256 "$TARBALL" > checksums.txt )
verify_out
named_failure "A8 checksums.txt covers only 1 of the 2 downloads" checksums-scope "$RC" "$OUT"

make_out "$STAMPED"
python3 - "$AOUT/checksums.txt" <<'PY'
import sys
p = sys.argv[1]
lines = open(p).read().splitlines()
lines[0] = "0" * 64 + lines[0][64:]
open(p, "w").write("\n".join(lines) + "\n")
PY
verify_out
named_failure "A9 checksums.txt digests do NOT match the files" checksums-match "$RC" "$OUT"

# ═══════════════════════════════════════════════════════════════════════════
# C — CLI gates. These must all fail BEFORE any build or upload.
# ═══════════════════════════════════════════════════════════════════════════
echo "── C: argument gates"
: > "$GHWIRE"
release_cli; check "C0 no subcommand → usage, exit 2" "2" "$RC"
release_cli publish --beta v1.0.0; check "C1 publish without --target → exit 2" "2" "$RC"
release_cli publish --target HEAD; check "C2 publish without --beta → exit 2" "2" "$RC"
release_cli publish --beta 1.0.0 --target HEAD
check "C3 a tag without the v prefix → exit 2" "2" "$RC"
release_cli promote nope; check "C4 promote with a non-tag → exit 2" "2" "$RC"
release_cli promote v1.0.0 v2.0.0; check "C5 promote with two tags → exit 2" "2" "$RC"
release_cli publish --beta v1.0.0 --target HEAD --bogus
check "C6 an unknown publish flag → exit 2" "2" "$RC"

# The legacy form must be REFUSED, never reinterpreted. Silently treating
# `bin/release v0.1.0` as `publish --beta v0.1.0 --target HEAD` would keep the
# words and change the deed: it would really upload, and really pick a commit the
# operator never named.
release_cli v0.1.0
check "C7 the removed 'bin/release <tag>' form exits 2" "2" "$RC"
case "$OUT" in
  *REMOVED*"publish --beta v0.1.0 --target"*) ok "C7 …and names the replacement command" ;;
  *) bad "C7 …and names the replacement command (out: $OUT)" ;;
esac
check "C8 NOTHING in the argument gates ever invoked gh" "" "$(cat "$GHWIRE")"

# ═══════════════════════════════════════════════════════════════════════════
# E — END TO END. Real staging worktree, real packaging, real read-back.
# ═══════════════════════════════════════════════════════════════════════════
# The fixture repo carries its own bin/build and bin/build-release, and
# bin/release runs `$STAGE/bin/build` — a path inside the staging worktree it cut
# from OC_RELEASE_SRC. So this drives the genuine control flow (including the
# --sign builder choice) with a build that finishes in a second.
echo "── E: end-to-end publish against a fixture repo"
SRC="$WORK/src"; mkdir -p "$SRC/bin" "$SRC/gosrc"
cp "$GOSRC/main.go" "$SRC/gosrc/main.go"
cp "$GOSRC/go.mod"  "$SRC/gosrc/go.mod"
printf 'MIT\n' > "$SRC/LICENSE"
printf '#!/bin/sh\necho fixture installer\n' > "$SRC/bin/install.sh"
# The fixture build records the codesigning env it was handed. That is the
# behavioural assertion for "default = not signed": not "the artifact has no
# signature" (which a gate BELOW the keychain probe would also produce) but
# "publish asked for no signing at all", observed at the build seam itself.
cat > "$SRC/bin/build" <<'SH'
#!/usr/bin/env bash
set -euo pipefail
R="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
echo "ENABLE=${OC_CODESIGN_ENABLE:-unset} REQUIRE=${OC_CODESIGN_REQUIRE:-unset}" > "$BUILD_WIRE"
echo "builder=bin/build" >> "$BUILD_WIRE"
echo "OC_APP_VERSION=${OC_APP_VERSION:-unset}" >> "$BUILD_WIRE"
SHORT="$(git -C "$R" rev-parse --short HEAD)"
mkdir -p "$R/.deploy" "$R/server/ocserverd/bindist"
( cd "$R/gosrc" && "$GO_BIN" build \
    -ldflags "-X main.appVersion=${OC_APP_VERSION} -X main.buildSHA=${SHORT}" \
    -o "$R/.deploy/ocserverd" . )
( cd "$R/gosrc" && "$GO_BIN" build -o "$R/server/ocserverd/bindist/ocwarden" . )
cp "$R/server/ocserverd/bindist/ocwarden" "$R/server/ocserverd/bindist/ocagent"
SH
cat > "$SRC/bin/build-release" <<'SH'
#!/usr/bin/env bash
set -euo pipefail
R="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
export OC_CODESIGN_REQUIRE=1
bash "$R/bin/build"
echo "builder=bin/build-release" >> "$BUILD_WIRE"
SH
chmod +x "$SRC/bin/build" "$SRC/bin/build-release" "$SRC/bin/install.sh"
(
  cd "$SRC"
  git init -q .
  git config user.email guard@example.invalid
  git config user.name  guard
  git add -A
  git -c commit.gpgsign=false commit -qm "fixture"
) || { echo "FATAL: could not build the fixture repo" >&2; exit 2; }
E_SHA="$(git -C "$SRC" rev-parse HEAD)"
E_SHORT="$(git -C "$SRC" rev-parse --short HEAD)"
E_TAG="v9.9.9-e2e"
E_TARBALL="officraft-$E_TAG-darwin-arm64.tar.gz"
EOUT="$WORK/e2e-out"
BUILD_WIRE="$WORK/.build-wire"

e2e() { # e2e [extra publish args...] — env overrides come from the caller
  : > "$GHWIRE"; : > "$BUILD_WIRE"
  rm -rf "$EOUT"
  OUT="$(PATH="$SHIMDIR:$PATH" \
    OC_RELEASE_SRC="$SRC" OC_RELEASE_OUT="$EOUT" \
    OC_RELEASE_GH_REPO="guard/fixture" \
    OC_RELEASE_SITE="http://127.0.0.1:1" \
    OC_RELEASE_SETTLE_TRIES=2 OC_RELEASE_SETTLE_SLEEP=0 \
    BUILD_WIRE="$BUILD_WIRE" GO_BIN="$GO" \
    bash "$RELEASE" publish --beta "$E_TAG" --target "$E_SHA" "$@" 2>&1)"
  RC=$?
}
e2e_stored() { # e2e_stored [stored_json overrides...] — what the shim will report
  GH_VIEW_JSON="$(python3 - "$E_TAG" "$E_SHA" "$E_TARBALL" "$@" <<'PY'
import json, sys
tag, sha, tarball = sys.argv[1:4]
rel = {"tagName": tag, "isDraft": False, "isPrerelease": True, "targetCommitish": sha,
       "assets": [{"name": n, "state": "uploaded", "size": 4096}
                  for n in (tarball, "install.sh", "checksums.txt")]}
for arg in sys.argv[4:]:
    k, _, v = arg.partition("=")
    rel[k] = json.loads(v)
print(json.dumps(rel))
PY
)"
  export GH_VIEW_JSON
}

# E0 — --dry-run. The single most important negative in this file: it builds and
# verifies for real and MUST NOT be able to publish. `gh release create` is the
# irreversible point of the whole system, so this is asserted on the gh tripwire.
export GH_VIEW_RC=0
e2e_stored
STATION_VERSION_JSON="{\"git_sha\":\"$E_SHORT\"}" e2e --dry-run
check "E0 --dry-run exits 0" "0" "$RC"
check "E0 --dry-run invoked gh EXACTLY ZERO times" "" "$(cat "$GHWIRE")"
check "E0 --dry-run still produced the real artifacts" "yes" \
  "$([[ -s "$EOUT/$E_TARBALL" && -s "$EOUT/install.sh" && -s "$EOUT/checksums.txt" ]] && echo yes || echo no)"
case "$OUT" in *"artifacts verified"*) ok "E0 --dry-run ran the real pre-upload verification" ;;
  *) bad "E0 --dry-run ran the real pre-upload verification (out: $(printf '%s' "$OUT" | tail -c 400))" ;; esac
check "E0 default publish asked for NO signing" \
  "ENABLE=unset REQUIRE=unset" "$(sed -n '1p' "$BUILD_WIRE")"
check "E0 default publish used bin/build, not bin/build-release" \
  "builder=bin/build" "$(sed -n '2p' "$BUILD_WIRE")"
check "E0 the tag reached the build as OC_APP_VERSION" \
  "OC_APP_VERSION=$E_TAG" "$(sed -n '3p' "$BUILD_WIRE")"

# E1 — the happy full arc.
STATION_VERSION_JSON="{\"git_sha\":\"$E_SHORT\"}" STATION_HEALTH_RC=0 e2e
check "E1 a fully correct publish exits 0" "0" "$RC"
check "E1 gh release create was called exactly once" "1" \
  "$(grep -c 'release create' "$GHWIRE" || true)"
case "$(grep 'release create' "$GHWIRE")" in
  *"--prerelease"*"--target $E_SHA"*) ok "E1 upload was --prerelease and --target the named sha" ;;
  *) bad "E1 upload was --prerelease and --target the named sha ($(grep 'release create' "$GHWIRE"))" ;;
esac

# E2/E3 — THE POINT OF THE TICKET. The upload succeeded; the world is still
# wrong; the command must fail anyway and say which item. Before T-588c both of
# these were a green release nobody checked.
STATION_VERSION_JSON="{\"git_sha\":\"$E_SHORT\"}" e2e_stored 'isDraft=true'
STATION_VERSION_JSON="{\"git_sha\":\"$E_SHORT\"}" e2e
named_failure "E2 upload OK but GitHub stored a DRAFT" release-draft "$RC" "$OUT"
check "E2 …and it did upload first (this is a post-upload failure)" "1" \
  "$(grep -c 'release create' "$GHWIRE" || true)"

e2e_stored
STATION_VERSION_JSON='{"git_sha":"aaaaaaa"}' e2e
named_failure "E3 upload+read-back OK but the STATION never moved" station-settle "$RC" "$OUT"

e2e_stored "targetCommitish=\"$(printf '%040d' 0)\""
STATION_VERSION_JSON="{\"git_sha\":\"$E_SHORT\"}" e2e
named_failure "E4 upload OK but the release is bound to another commit" release-target "$RC" "$OUT"

# E5 — --sign is the ONLY way to sign, and it routes through bin/build-release.
e2e_stored
STATION_VERSION_JSON="{\"git_sha\":\"$E_SHORT\"}" e2e --sign
check "E5 --sign exits 0 on a provisioned fixture" "0" "$RC"
check "E5 --sign routed through bin/build-release" "builder=bin/build-release" \
  "$(sed -n '4p' "$BUILD_WIRE")"
check "E5 --sign made the identity MANDATORY (REQUIRE=1)" \
  "ENABLE=unset REQUIRE=1" "$(sed -n '1p' "$BUILD_WIRE")"

# E6 — a failed upload must NOT be followed by a read-back that "passes" against
# a stale/foreign release. It has to stop at the upload.
e2e_stored
GH_WRITE_RC=1 STATION_VERSION_JSON="{\"git_sha\":\"$E_SHORT\"}" e2e
check "E6 a failed gh release create fails the publish" "1" "$RC"
check "E6 …and never proceeds to the read-back" "0" \
  "$(grep -c 'release view' "$GHWIRE" || true)"
unset GH_WRITE_RC

# E7 — the staging worktree is a THROWAWAY and must be cleaned up, or the next
# publish of any tag dies on `git worktree add` refusing an existing path.
check "E7 no staging worktree is left registered in the fixture repo" "1" \
  "$(git -C "$SRC" worktree list | wc -l | tr -d '[:space:]')"

# ═══════════════════════════════════════════════════════════════════════════
# P — PROMOTE. Rebuilds nothing; must detect a re-upload under the flip.
# ═══════════════════════════════════════════════════════════════════════════
echo "── P: promote"
promote() {
  : > "$GHWIRE"
  OUT="$(PATH="$SHIMDIR:$PATH" OC_RELEASE_GH_REPO="guard/fixture" \
    bash "$RELEASE" promote "$@" 2>&1)"
  RC=$?
}

# The gh shim answers every `release view` identically, so a promote whose
# before/after agree is the happy path.
export GH_VIEW_JSON="$(stored_json 'isPrerelease=false')"
promote "$TAG"
named_failure "P0 promote refuses a tag that is ALREADY final" release-channel "$RC" "$OUT"

# Happy promote needs the pre-flip view to be a prerelease and the post-flip view
# to be final — two different answers from one shim, so it switches on a counter.
cat > "$SHIMDIR/gh" <<'SH'
#!/usr/bin/env bash
echo "gh $*" >> "$GH_WIRE"
if [[ "${1:-}" == "release" && "${2:-}" == "view" ]]; then
  n="$(grep -c 'release view' "$GH_WIRE")"
  if [[ "$n" == "1" ]]; then printf '%s' "${GH_VIEW_BEFORE:-}"; else printf '%s' "${GH_VIEW_AFTER:-}"; fi
  exit 0
fi
if [[ "${1:-}" == "release" && ( "${2:-}" == "create" || "${2:-}" == "edit" ) ]]; then
  exit "${GH_WRITE_RC:-0}"
fi
echo "unexpected gh invocation: $*" >&2
exit 99
SH
chmod +x "$SHIMDIR/gh"
unset GH_VIEW_JSON

export GH_VIEW_BEFORE="$(stored_json)"
export GH_VIEW_AFTER="$(stored_json 'isPrerelease=false')"
promote "$TAG"
check "P1 a clean promote exits 0" "0" "$RC"
check "P1 …and flipped it with exactly one gh release edit" "1" \
  "$(grep -c 'release edit' "$GHWIRE" || true)"

promote --dry-run "$TAG"
check "P2 --dry-run promote exits 0" "0" "$RC"
check "P2 --dry-run promote NEVER flips anything" "0" \
  "$(grep -c 'release edit' "$GHWIRE" || true)"

# THE promote-specific invariant: promote is a metadata-only flip, so if the
# asset set moved under it, somebody re-uploaded and the promoted bytes are not
# the bytes anyone tested. That is a failure, not a warning.
GH_VIEW_AFTER="$(stored_json 'isPrerelease=false' "assets=$(assets_json '[
  {"name":"'"$TARBALL"'","state":"uploaded","size":999999},
  {"name":"install.sh","state":"uploaded","size":2048},
  {"name":"checksums.txt","state":"uploaded","size":128}]')")" \
  promote "$TAG"
named_failure "P3 an asset was RE-UPLOADED under the flip (size changed)" promote-assets "$RC" "$OUT"

# The target moving under a metadata-only flip is caught INSIDE the post-flip
# read-back, because promote passes the pre-flip target as the expected one — so
# the item named is release-target, with both values in the message. Asserting the
# real name here (rather than a promote-specific one) is deliberate: the moment a
# case is written against a nicer-sounding item that nothing emits, it stops being
# a test of the code and becomes a test of the wish.
GH_VIEW_AFTER="$(stored_json 'isPrerelease=false' 'targetCommitish="ffffffffffffffffffffffffffffffffffffffff"')" \
  promote "$TAG"
named_failure "P4 the target commit MOVED under the flip" release-target "$RC" "$OUT"
case "$OUT" in
  *"ffffffffffffffffffffffffffffffffffffffff"*"$SHA"*) ok "P4 …naming both the stored and the expected commit" ;;
  *) bad "P4 …naming both the stored and the expected commit (out: $(printf '%s' "$OUT" | tr '\n' '|'))" ;;
esac

GH_VIEW_AFTER="$(stored_json 'isPrerelease=true')" promote "$TAG"
named_failure "P5 the flip did not take (still a prerelease afterwards)" release-channel "$RC" "$OUT"

GH_WRITE_RC=1 promote "$TAG"
check "P6 a failed gh release edit fails the promote" "1" "$RC"
unset GH_WRITE_RC

echo "release guard: $PASS ok, $FAIL failed"
[[ "$FAIL" == "0" ]] || exit 1
exit 0
