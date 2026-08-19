#!/usr/bin/env bash
# bin/tests/agent-build-sha-guard.sh — the ocagent build stamp is one contract
# written down in THREE places, and no Go test can see two of them (T-8f7d).
#
# WHY THIS EXISTS. A listener process holds the image it started with. Measured on
# this fleet: one listener sat on inode 65557770 while the file on disk turned over
# four times, so a FIXED bug was, for that agent, unfixed for its whole life — and
# "ship it and watch the fleet get better" returns a false negative. The
# connection line now names the ocagent that printed it, from a link-time stamp:
#
#   bin/build-bindist       go build -ldflags="… -X main.buildSHA=$OCAGENT_SHA"
#   cli/ocagent/main.go     var buildSHA string
#   cli/ocagent/listen_run.go   prints " [agent <sha>]" when it is non-empty
#
# 🔴 THE MISSING STAMP IS SILENT, AND IT IS THE DESIGNED-FOR CASE TOO. An empty
# buildSHA omits the segment — which is byte-identical to the honest "this build
# carries no stamp" that every `go build ./...` and every test binary produces. So
# dropping the -X flag, renaming the variable, or stamping a different package all
# degrade to exactly the behaviour the feature is supposed to have in a dev build.
# 🔴 AND EVERY GO TEST BINARY IS UNSTAMPED BY CONSTRUCTION, so no Go test can
# distinguish them: the suite stays green with the flag deleted. That is what this
# guard buys, and it is the whole reason it is a shell script.
#
# It also pins the ONE binary that must NOT be stamped, and this guard is the ONLY
# thing that does. cli/officraft is the one committed prebuilt
# (dist/officraft/officraft); its -trimpath -buildvcs=false keep the git revision
# out of the bits so the committed bytes are reproducible from a clean clone.
# 🔴 check-officraft-dist DOES NOT CATCH A STAMP — measured, rc=0 with all three
# stamped. It hashes cli/officraft's SOURCE files and dist/officraft/officraft's
# bytes; stamping changes no source file and it never looks at the bindist copy.
# What stamping actually does is split the SHIPPED anchor from the committed one
# (measured: bindist copy 710a2525… → d8fb53dd… while dist/ and binary.sha256 stay
# 710a2525…), and the bindist copy is what every install receives. Case 4 below
# checks the flag; case 7 checks the bytes, which is the consequence.
#
# 🔴 EMPTY-EXTRACTION IS THE TRAP. If a pattern stops matching because someone
# reformatted a line, the extraction is "" — and a naive comparison of "" against
# "" passes. So each check asserts a NON-EMPTY find before it asserts anything
# about the value, and case 5 plants a real mutant to prove the checks can still
# go red.
set -uo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT="$(cd "$HERE/../.." && pwd)"

BUILD_FILE="$ROOT/bin/build-bindist"
MAIN_FILE="$ROOT/cli/ocagent/main.go"
LINE_FILE="$ROOT/cli/ocagent/listen_run.go"

PASS=0; FAIL=0
ok()  { PASS=$((PASS+1)); printf '  ok   — %s\n' "$1"; }
bad() { FAIL=$((FAIL+1)); printf '  FAIL — %s\n' "$1"; }

# The -X target, taken from the ldflags of the ocagent build line only. Anchored
# on the ocagent directory so the ocwarden/officraft lines below it cannot satisfy
# it, and on `-X` inside an ldflags= so the prose in this file's own comments
# cannot either.
xflag_target() { # MODULE_DIR (e.g. cli/ocagent) -> "main.buildSHA" or empty
                 # Reads $BUILD_FILE, which case 6 overrides per-call to point at
                 # its mutant copy.
  grep -E "cd \"\\\$ROOT/$1\".*go build" "$BUILD_FILE" 2>/dev/null |
    sed -nE 's/.*-X[[:space:]]+([A-Za-z0-9_.]+)=.*/\1/p' | head -1
}

# ── 1. the ocagent build line stamps main.buildSHA ──────────────────────────
TARGET="$(xflag_target cli/ocagent)"
if [[ -z "$TARGET" ]]; then
  bad "bin/build-bindist: the cli/ocagent build line carries no -X <var>= — every listener it produces will omit its own version from the connection line, and it will look exactly like a dev build doing the right thing (no Go test can tell the difference)"
elif [[ "$TARGET" != "main.buildSHA" ]]; then
  bad "bin/build-bindist: the ocagent build stamps '$TARGET', but the variable the connection line reads is main.buildSHA — a stamp on a name nobody reads is the same silence by another route"
else
  ok "bin/build-bindist: the cli/ocagent build stamps main.buildSHA at link time"
fi

# ── 2. the value it stamps is derived, not a literal ────────────────────────
if grep -qE '\-X main\.buildSHA=\$\{?[A-Za-z_][A-Za-z0-9_]*' "$BUILD_FILE"; then
  ok "bin/build-bindist: the stamped value comes from a variable, not a literal baked into the script"
else
  bad "bin/build-bindist: -X main.buildSHA is not fed from a shell variable — a hard-coded sha would name the same commit forever, which is worse than naming none"
fi

# ── 3. the variable exists in the ocagent main package ─────────────────────
if grep -qE '^var buildSHA string' "$MAIN_FILE"; then
  ok "cli/ocagent/main.go: 'var buildSHA string' is there for -X to fill (a -X against a name that does not exist is silently ignored by the linker)"
else
  bad "cli/ocagent/main.go: no 'var buildSHA string' — the linker DISCARDS a -X for an unknown symbol without warning, so the build would keep succeeding and every line would stay unstamped"
fi

# ── 4. cli/officraft must NOT be stamped ───────────────────────────────────
ANCHOR_TARGET="$(xflag_target cli/officraft)"
if [[ -n "$ANCHOR_TARGET" ]]; then
  bad "bin/build-bindist: the cli/officraft build carries -X $ANCHOR_TARGET=. That binary is the ONE committed prebuilt (dist/officraft/officraft); its -trimpath -buildvcs=false exist to keep the git revision out of the bits so check-officraft-dist can compare them from a clean clone. Stamping it changes the anchor's identity every commit — measured: check-officraft-dist rc=1"
else
  ok "bin/build-bindist: the cli/officraft anchor is NOT stamped — its bytes stay reproducible for bin/check-officraft-dist"
fi

# ── 5. the connection line reads the variable, and omits the segment when empty
if grep -qE 'TrimSpace\(buildSHA\)' "$LINE_FILE"; then
  ok "cli/ocagent/listen_run.go: the connection line reads buildSHA through TrimSpace, so a blank stamp takes the same not-known path as no stamp"
else
  bad "cli/ocagent/listen_run.go: the connection line does not read TrimSpace(buildSHA) — either it ignores the stamp, or a whitespace-only one prints as ' [agent \\t]', which reads as an answer"
fi

# ── 6. POSITIVE CONTROL: the checks above can still go red ─────────────────
# Without this, a reformat that broke every pattern would print all green. The
# mutant is one character on a copy; nothing in the tree is touched.
MUT="$(mktemp -t oc-agent-sha-guard.XXXXXX)"
trap 'rm -f "$MUT"' EXIT
sed -E 's/-X main\.buildSHA=/-X main.buildSHAX=/' "$BUILD_FILE" > "$MUT"
if ! grep -q 'main.buildSHAX=' "$MUT"; then
  bad "positive control: the mutant generator changed nothing — it could not find the -X to rename, so checks 1 and 2 are not actually exercised"
else
  MUT_TARGET="$(BUILD_FILE="$MUT" xflag_target cli/ocagent)"
  if [[ "$MUT_TARGET" == "main.buildSHA" ]]; then
    bad "positive control: a renamed -X target still extracted as main.buildSHA — check 1 cannot go red and is decoration"
  else
    ok "positive control: renaming the -X target one character is detected (extracted '$MUT_TARGET'), so check 1 is load-bearing"
  fi
fi

# ═════════════════════════════════════════════════════════════════════════════
# 🔴 BUILD IT AND LOOK AT THE BINARY. Everything above is text matching, and text
# matching cannot see the three ways this feature dies silently while every
# pattern above still matches:
#   * a tree with no .git — the sha lookup came back empty and the build shipped
#     an unstamped fleet (this is now fatal in build-bindist, and case 6 proves it
#     stays fatal);
#   * a SECOND, unstamped `go build` for ocagent appended after the stamped one —
#     the last write wins on disk, while a first-match text scan never sees it;
#   * a stamp fed from the wrong revision (HEAD~5, a stale variable) — shape-valid,
#     confidently naming a build this is not.
# All three were green against the text checks alone. So the last cases run the
# real bin/build-bindist and read the bytes it produced.
# ⚠️ SIDE EFFECT, ON PURPOSE: this rebuilds server/ocserverd/bindist/ (gitignored
# build output, which build-bindist rewrites from scratch anyway). ~1s. Anything
# cheaper would be back to grepping the script that is the thing under test.
echo
if ! command -v git >/dev/null 2>&1 || ! git -C "$ROOT" rev-parse --short HEAD >/dev/null 2>&1; then
  bad "cannot resolve HEAD: this guard has to compare the stamp against the tree being built, and without a sha it would silently check nothing"
else
  HEAD_SHA="$(git -C "$ROOT" rev-parse --short HEAD)"
  BUILD_LOG="$(mktemp -t oc-agent-sha-build.XXXXXX)"
  trap 'rm -f "$MUT" "$BUILD_LOG"' EXIT
  if ! bash "$ROOT/bin/build-bindist" >"$BUILD_LOG" 2>&1; then
    bad "bin/build-bindist FAILED, so nothing below could be checked (last lines: $(tail -3 "$BUILD_LOG" | tr '\n' ' '))"
  else
    ok "bin/build-bindist runs clean, so the assertions below are about real output"

    # ── 5. the produced ocagent really carries THIS tree's sha ──────────────
    AGENT_BIN="$ROOT/server/ocserverd/bindist/ocagent"
    if [[ ! -f "$AGENT_BIN" ]]; then
      bad "bin/build-bindist produced no ocagent at $AGENT_BIN"
    # ⚠️ grep -c, NOT grep -q. Under `set -o pipefail`, grep -q exits the moment it
    # matches, strings takes SIGPIPE, and the PIPELINE reports 141 — so the
    # success case reads as failure. Measured here: -q said "not found" on a
    # binary where -c said 3.
    elif [[ "$(strings -a "$AGENT_BIN" 2>/dev/null | grep -c -- "$HEAD_SHA")" -gt 0 ]]; then
      ok "the ocagent it produced carries THIS tree's sha ($HEAD_SHA) in its bytes — the stamp is not merely written in the script, it survived -s -w and landed"
    else
      bad "the ocagent bin/build-bindist just produced does NOT contain this tree's sha ($HEAD_SHA). Every listener from this build will print no [agent …] segment, or name a different build — and both are byte-indistinguishable from a dev build behaving correctly, which is the failure this whole feature exists to remove. Causes seen: an empty sha lookup (no .git), a second unstamped go build overwriting the first, or a stamp fed from the wrong revision"
    fi

    # ── 6. an empty sha must be FATAL, not a silent unstamped build ─────────
    if OCAGENT_SHA=" " bash "$ROOT/bin/build-bindist" >/dev/null 2>&1; then
      bad "bin/build-bindist accepted a BLANK OCAGENT_SHA and built anyway — that is the tarball/git-archive case, and it ships a fleet that cannot say which build it is while every text check here stays green"
    else
      ok "a blank OCAGENT_SHA is FATAL — a build that cannot name itself does not ship"
    fi
    bash "$ROOT/bin/build-bindist" >/dev/null 2>&1 || true

    # ── 7. the shipped anchor still equals the committed one ────────────────
    # The consequence case 4 is really about. check-officraft-dist cannot see this
    # (measured rc=0 with the anchor stamped): it compares dist/officraft/officraft
    # against its manifest, and never looks at the copy that actually ships.
    ANCHOR_BINDIST="$ROOT/server/ocserverd/bindist/officraft"
    ANCHOR_COMMITTED="$ROOT/dist/officraft/officraft"
    if [[ ! -f "$ANCHOR_BINDIST" || ! -f "$ANCHOR_COMMITTED" ]]; then
      bad "cannot compare the shipped anchor against the committed one (missing $ANCHOR_BINDIST or $ANCHOR_COMMITTED)"
    else
      A="$(shasum -a 256 "$ANCHOR_BINDIST" | cut -d' ' -f1)"
      B="$(shasum -a 256 "$ANCHOR_COMMITTED" | cut -d' ' -f1)"
      if [[ "$A" == "$B" ]]; then
        ok "the anchor bin/build-bindist ships is byte-identical to the committed dist/officraft/officraft (${A:0:12}…) — it is still reproducible from a clean clone"
      else
        bad "the anchor that SHIPS (${A:0:12}…) is not the committed one (${B:0:12}…). Every install receives the bindist copy, so the TCC anchor people run stopped being the one reviewers can reproduce — and check-officraft-dist returns 0 on exactly this, because it only ever compares the committed file to its own manifest"
      fi
    fi
  fi
fi

echo "ocagent build-sha stamp contract tests: $PASS ok, $FAIL failed"
[[ "$FAIL" == "0" ]]
