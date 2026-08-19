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
# about the value, and case 6 plants a real mutant to prove the checks can still
# go red.
set -uo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT="$(cd "$HERE/../.." && pwd)"

BUILD_FILE="$ROOT/bin/build-bindist"
MAIN_FILE="$ROOT/cli/ocagent/main.go"
LINE_FILE="$ROOT/cli/ocagent/listen_run.go"

# ruby is resolved the same way bin/tests/go-test-timeout-guard.sh and
# auto-beta-guard.sh do: the hosted macOS runner has system ruby, and case 10 needs
# a language that can read two binaries and search one for the whole of the other.
RUBY="$(command -v ruby 2>/dev/null || true)"
if [[ -z "$RUBY" ]]; then
  for cand in /usr/bin/ruby /opt/homebrew/bin/ruby /usr/local/bin/ruby; do
    [[ -x "$cand" ]] && { RUBY="$cand"; break; }
  done
fi

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

# ── 5. BREADCRUMB, NOT A DEFENCE — the blank-stamp path is guarded in Go ────
# ⚠️ READ THIS BEFORE TRUSTING THIS CASE. It is a bare grep for the characters
# `TrimSpace(buildSHA)`, so it only notices the text disappearing outright.
# Measured: review kept the `TrimSpace(buildSHA)` call and made the segment print
# UNCONDITIONALLY — this case stayed green (guard rc=0) and three Go tests went
# red (rc=1). The blank/missing-stamp behaviour is genuinely covered there, in
# the one place that can see it, which is why nothing is added here: a second
# check over the same behaviour would only make this file look like it is doing
# more than it does. What this line is for is naming the coupling — the segment
# and the [station …] segment next to it must agree on what counts as missing
# (they once did not, and the disagreement printed ' [agent \t]').
if grep -qE 'TrimSpace\(buildSHA\)' "$LINE_FILE"; then
  ok "cli/ocagent/listen_run.go: still spells the stamp read as TrimSpace(buildSHA) — a breadcrumb for the coupling with the [station …] segment, NOT a guard (the Go tests own the blank-stamp behaviour; this grep cannot see it)"
else
  bad "cli/ocagent/listen_run.go: the connection line no longer mentions TrimSpace(buildSHA). This case cannot tell you what broke — go read the Go tests for the blank-stamp path — but the two adjacent segments have stopped agreeing on what counts as a missing value, which is how ' [agent \\t]' shipped once already"
fi

# ── 6. POSITIVE CONTROL (text side): the checks above can still go red ─────────────────
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

    # ── 7. the produced ocagent really carries THIS tree's sha ──────────────
    # 🔴 ASK THE BINARY, DO NOT GREP IT. This was `strings … | grep -c "$HEAD_SHA"`
    # and it was DECORATION in the tree shape CI uses. cli/ocagent is built without
    # -trimpath/-buildvcs=false, so Go auto-embeds `vcs.revision=<full sha>` — and
    # the short sha is a PREFIX of it, so in a plain clone the grep matches any
    # ocagent built from this tree, stamped or not. Review measured a second
    # unstamped build and a HEAD~5 stamp both passing while the shipped binary
    # reported `unstamped` and `a0d998a2`.
    # 🔴 IT LOOKED LOAD-BEARING TO ME BECAUSE I MEASURED IT IN A GIT WORKTREE,
    # where .git is a file, Go embeds no vcs settings, and the grep really does
    # return 0. One tree shape, generalised into a claim about all of them — and
    # the shape I did not test is the one actions/checkout produces.
    # `ocagent version` prints build.sha from the linked-in variable and nothing
    # else can supply it, so exact equality against it cannot be satisfied by
    # metadata that merely happens to contain the same characters.
    # ⚠️ THIS IS THE EMBED'S INPUT — the same gap case 10 closes one level up. The
    # ocagent a machine runs is the copy `go:embed all:bindist` (assets.go) baked
    # into ocserverd and serveBinary hands out (api_machines.go); this checks the
    # file build-bindist just wrote, before anything embeds it. The OUTPUT is
    # pinned in bin/release's verify_artifacts, which reads the packaged ocagent's
    # -ldflags with `go version -m` on the artifact about to be published. It lives
    # there rather than here because a real ocserverd only exists at release time —
    # not because of cost. See the comment beside it for the full reasoning.
    AGENT_BIN="$ROOT/server/ocserverd/bindist/ocagent"
    if [[ ! -f "$AGENT_BIN" ]]; then
      bad "bin/build-bindist produced no ocagent at $AGENT_BIN"
    # ⚠️ grep -c, NOT grep -q. Under `set -o pipefail`, grep -q exits the moment it
    # matches, strings takes SIGPIPE, and the PIPELINE reports 141 — so the
    # success case reads as failure. Measured here: -q said "not found" on a
    # binary where -c said 3.
    elif REPORTED="$("$AGENT_BIN" version 2>/dev/null | awk '/build\.sha:/{print $2}')" &&
         [[ "$REPORTED" == "$HEAD_SHA" ]]; then
      ok "the ocagent it produced REPORTS this tree's sha ($HEAD_SHA) from its own version command — asked, not grepped, so no auto-embedded vcs metadata can satisfy it"
    else
      bad "the ocagent bin/build-bindist just produced reports build.sha=\"${REPORTED:-<none>}\", not this tree's sha ($HEAD_SHA). Every listener from this build will print no [agent …] segment, or name a different build — and both are byte-indistinguishable from a dev build behaving correctly, which is the failure this whole feature exists to remove. Causes seen: an empty sha lookup (no .git), a second unstamped go build overwriting the first, or a stamp fed from the wrong revision"
    fi

    # ── 9. the shipped anchors still equal the committed one ────────────────
    # The consequence case 4 is really about. check-officraft-dist cannot see this
    # (measured rc=0 with the anchor stamped): it compares dist/officraft/officraft
    # against its manifest, and never looks at the copy that actually ships.
    # 🔴 BOTH STAGED COPIES, and anchordist is the one that reaches machines.
    # An earlier version compared only bindist/officraft and its message claimed
    # "every install receives the bindist copy" — wrong: nothing serves that file.
    # build-bindist copies it on to cli/ocwarden/anchordist, which anchor_embed.go
    # go:embeds into ocwarden, and `ocwarden install` materialises THAT. Review
    # rebuilt anchordist separately and got a shipped anchor of aa576b9e… against a
    # committed 710a2525… with the guard still printing 10 ok.
    ANCHOR_COMMITTED="$ROOT/dist/officraft/officraft"
    if [[ ! -f "$ANCHOR_COMMITTED" ]]; then
      bad "cannot compare the shipped anchors: $ANCHOR_COMMITTED is missing"
    else
      B="$(shasum -a 256 "$ANCHOR_COMMITTED" | cut -d' ' -f1)"
      for staged in "$ROOT/server/ocserverd/bindist/officraft" "$ROOT/cli/ocwarden/anchordist/officraft"; do
        if [[ ! -f "$staged" ]]; then
          bad "cannot compare the shipped anchor against the committed one: $staged is missing"
          continue
        fi
        A="$(shasum -a 256 "$staged" | cut -d' ' -f1)"
        if [[ "$A" == "$B" ]]; then
          ok "$(basename "$(dirname "$staged")")/officraft is byte-identical to the committed dist/officraft/officraft (${A:0:12}…)"
        else
          bad "the anchor staged at $staged (${A:0:12}…) is not the committed one (${B:0:12}…). cli/ocwarden/anchordist is go:embedded into ocwarden and is what 'ocwarden install' materialises, so a divergence there means the TCC anchor machines actually run stopped being the one reviewers can reproduce — and check-officraft-dist returns 0 on exactly this, because it only ever compares the committed file to its own manifest"
        fi
      done

      # ── 10. the anchor is INSIDE the ocwarden that ships ──────────────────
      # 🔴 CASE 9 COMPARES THE EMBED'S INPUT, NOT ITS OUTPUT. It hashes files on
      # disk; what reaches a machine is the copy go:embed baked into ocwarden, and
      # the only thing connecting the two is the ORDER of two builds in
      # bin/build-bindist. Review reordered them — ocwarden built before the anchor
      # was staged — and measured a shipped ocwarden 1.77MB smaller (exactly the
      # anchor), embeddedAnchor() returning nil, `ocwarden install` refusing on
      # every new machine, and this guard printing 11 ok.
      # ⚠️ IT ALSO HID ITSELF: this script runs build-bindist up to three times, and
      # a later run staged the anchor the first run had missed. CI runs it once.
      # go:embed stores bytes verbatim, so the committed anchor's own bytes must be
      # findable inside the binary. That is the output, not the input.
      # 🔴 AND IT MUST BE THE WHOLE ANCHOR, NOT A SAMPLE OF IT. This read a 4096-byte
      # slice from the anchor's middle, which is a shape test, not an identity test:
      # measured, adding -X main.buildSHA to the cli/officraft line changes 100 bytes
      # out of 1,742,642 and NONE of them are in that window, so the probe returned
      # true for an anchor that was NOT the committed one. Case 9 catches that
      # particular mutant by hash, but the cell case 10 claims for itself — the
      # embed's OUTPUT — was green on a shipped ocwarden carrying the wrong anchor.
      # Containment of the ENTIRE file is the identity claim, and it costs 0.054s.
      WARDEN_BIN="$ROOT/server/ocserverd/bindist/ocwarden"
      if [[ -z "$RUBY" ]]; then
        bad "cannot check whether the shipped ocwarden embeds the anchor: no ruby (the same dependency the ci.yml side already needs)"
      elif [[ ! -f "$WARDEN_BIN" ]]; then
        bad "bin/build-bindist produced no ocwarden at $WARDEN_BIN"
      elif "$RUBY" -e '
            anchor = File.binread(ARGV[0])
            warden = File.binread(ARGV[1])
            exit(anchor.bytesize > 0 && warden.include?(anchor) ? 0 : 1)
          ' "$ANCHOR_COMMITTED" "$WARDEN_BIN"; then
        ok "the ocwarden that ships contains the committed anchor IN FULL (all $(wc -c < "$ANCHOR_COMMITTED" | tr -d ' ') bytes, found inside the binary) — the embed's output, not the order the build stages its input in"
      else
        bad "the ocwarden bin/build-bindist just produced does NOT contain the committed anchor's bytes in full. cli/ocwarden/anchordist is go:embedded into it and 'ocwarden install' refuses without an anchor, so the cockpit one-liner fails on every new machine — and staging the file next to the build is not the same as the build having read it. Check the ORDER in bin/build-bindist: the anchor must be staged BEFORE ocwarden is compiled"
      fi

    # ⚠️ THE REMAINING CASES RUN LAST ON PURPOSE. They build twice more — once
    # refused, once to repair — and a LATER BUILD STAGES WHAT AN EARLIER ONE
    # MISSED. Cases 9 and 10 must read the FIRST build's output or this script
    # hides the defect it is looking for: measured, with the anchor staged after
    # ocwarden was compiled, the repair rebuild below fixed it and case 10 went
    # green on a shipped binary that had no anchor in it. CI runs build-bindist
    # once, so the first build is the one that matters.
    # ── 8. an empty sha must be FATAL, not a silent unstamped build ─────────
    if OCAGENT_SHA=" " bash "$ROOT/bin/build-bindist" >/dev/null 2>&1; then
      bad "bin/build-bindist accepted a BLANK OCAGENT_SHA and built anyway — that is the tarball/git-archive case, and it ships a fleet that cannot say which build it is while every text check here stays green"
    else
      ok "a blank OCAGENT_SHA is FATAL — a build that cannot name itself does not ship"
    fi
    # 🔴 AND IT MUST NOT HAVE DELETED ANYTHING ON ITS WAY OUT. bindist/.gitkeep is
    # TRACKED, and `rm -rf "$BINDIST"` used to run BEFORE the sha check, so a
    # refused build left the working tree with a deleted tracked file and only an
    # error message to explain it. That fix shipped with nothing guarding it:
    # review reverted it and this suite printed 11 ok, because THIS SCRIPT is both
    # what triggers the deletion (the failing build above) and what repairs it (the
    # rebuild below). Asserted here, before the repair, or it cannot be seen.
    if [[ -n "$(git -C "$ROOT" status --porcelain -- server/ocserverd/bindist/.gitkeep 2>/dev/null)" ]]; then
      bad "the refused build deleted the TRACKED file server/ocserverd/bindist/.gitkeep. A build that declines to run must not modify the tree on its way out — resolve the sha before anything destructive happens, not after"
    else
      ok "a refused build leaves the tree alone — the tracked bindist/.gitkeep survives it"
    fi
    bash "$ROOT/bin/build-bindist" >/dev/null 2>&1 || true
    fi
  fi
fi

echo "ocagent build-sha stamp contract tests: $PASS ok, $FAIL failed"
[[ "$FAIL" == "0" ]]
