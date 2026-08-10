#!/usr/bin/env bash
# e2e_test/tests_guard/run.sh — HERMETIC unit tests for the T-8aa1 isolation
# layer in e2e_test/lib/oc_lifecycle.sh: the live-fleet guard + the namespace
# allocator (oc_resolve_instance) + the derivation helpers.
#
# WHY bats-free: e2e_test/ has no shell-test harness. This is a tiny, dependency-
# free runner (assert helpers + a PATH shim that stubs EVERY external command the
# guard/allocator touches) so it can run inside bin/ci.sh on ANY host — including
# a LIVE fleet host — WITHOUT touching the real launchctl/tmux/lsof/fleet. The
# stubs return controlled output and NOTHING real is mutated.
# ⚠️ It used to say "NO teardown path is ever exercised", and that is no longer
# true: cases 20b/20e/20f drive the real setup.sh → run_all.sh → teardown.sh
# chain. The narrower property that does hold — and, more to the point, the one
# this file PINS rather than merely asserts — is that teardown reaches the disk
# only through the record-only seam, against a throwaway tree: case 20e pins the
# seam as teardown.sh's only way out, and 19c/20c/20f keep a sentinel in that
# tree and fail if anything deletes it. So it records what it would have removed
# instead of removing it. That is what makes this safe on a live fleet host;
# "no teardown code runs at all" is not, and has not been for a while.
#
# SCOPE — what decides which cases run
# NOTHING discovers anything here. This file IS the suite: every case is a
# literal block in this one script, run top to bottom, and there is no per-file
# collection step that would notice a block that stopped existing. So deleting or
# short-circuiting a case block does not fail — it silently runs less, and
# PASS/FAIL only ever count what was actually reached. `FAIL -eq 0` answers "did
# anything fail?", not "did anything run?", exactly like the rc of a test runner.
# That is why there is a PASS FLOOR at the bottom of this file. Read its comment
# for what it does and does NOT catch — it is a floor, so it catches the suite
# being gutted, not one case going missing.
set -uo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
LIB="$HERE/../lib/oc_lifecycle.sh"
[[ -f "$LIB" ]] || { echo "FATAL: lib not found at $LIB" >&2; exit 2; }

PASS=0; FAIL=0
ok()   { PASS=$((PASS+1)); printf '  ok   — %s\n' "$1"; }
bad()  { FAIL=$((FAIL+1)); printf '  FAIL — %s\n' "$1"; }
check(){ # check DESC EXPECTED ACTUAL
  if [[ "$2" == "$3" ]]; then ok "$1"; else bad "$1 (want '$2' got '$3')"; fi
}

# ── PATH shim: stub every external command the guard/allocator invokes ───────
SHIMDIR="$(mktemp -d -t oc-guard-shim.XXXXXX)"
TRIPWIRE="$SHIMDIR/.tripwire"
: > "$TRIPWIRE"
trap 'rm -rf "$SHIMDIR"' EXIT

cat > "$SHIMDIR/launchctl" <<'SH'
#!/usr/bin/env bash
# Only two verbs matter to the code under test: `print` (read-only detection) and
# `bootout` (MUST never be reached by a guard/allocator — tripwire if it is).
if [[ "$1" == "bootout" ]]; then
  if [[ "${SHIM_ALLOW_TEARDOWN:-0}" == "1" ]]; then
    echo "launchctl $*" >> "$SHIM_TEARDOWN_LOG"
  else
    echo "TRIPWIRE launchctl bootout $*" >> "$SHIM_TRIPWIRE"
  fi
  exit 0
fi
if [[ "$1" == "print" ]]; then
  case "$2" in
    */com.officraft.ocwarden) [[ "${SHIM_WARDEN:-0}" == "1" ]] && exit 0 || exit 1 ;;
    *) exit 1 ;;
  esac
fi
exit 0
SH

cat > "$SHIMDIR/lsof" <<'SH'
#!/usr/bin/env bash
# Answer LISTEN queries: exit 0 (occupied) iff the -iTCP:<port> is in SHIM_LISTEN_PORTS.
port=""
for a in "$@"; do case "$a" in -iTCP:*) port="${a#-iTCP:}";; esac; done
case " ${SHIM_LISTEN_PORTS:-} " in *" $port "*) exit 0 ;; *) exit 1 ;; esac
SH

cat > "$SHIMDIR/tmux" <<'SH'
#!/usr/bin/env bash
# forms used: `-L <sock> ls`  and  `-L <sock> ls -F '#S'`. Sessions in
# SHIM_SESSIONS (newline-sep) belong to the canonical socket 'officraft'.
sock="$2"
if [[ "${3:-}" == "ls" ]]; then
  if [[ "$sock" == "officraft" && -n "${SHIM_SESSIONS:-}" ]]; then
    [[ "${4:-}" == "-F" ]] && printf '%s\n' "$SHIM_SESSIONS"
    exit 0
  fi
  exit 1
fi
exit 0
SH

cat > "$SHIMDIR/ioreg" <<'SH'
#!/usr/bin/env bash
# The hardware-identity anchor (T-e1dd). Emits the one line the guard's awk reads.
# SHIM_HW_UUID drives it; empty = the "cannot read a UUID" case. This is a PATH
# stub on purpose and NOT an env override in the guard itself: production must
# have no way to be told what machine it is on.
printf '    "IOPlatformUUID" = "%s"\n' "${SHIM_HW_UUID-00000000-0000-0000-0000-FEEDFACE0000}"
SH

cat > "$SHIMDIR/ssh" <<'SH'
#!/usr/bin/env bash
# Stands in for the second machine. It EXECUTES the probe command the guard sent,
# in a real shell, against a fake remote $HOME — rather than printing canned
# answers. That difference matters: the probe nests `awk -F\"` and `\$4` inside a
# double-quoted command substitution inside a single-quoted remote command, and a
# canned-answer shim would keep every assertion green while a broken escaping
# silently returned an empty UUID from every real host — which is fail-OPEN.
#
# SHIM_SSH_FAIL   → unreachable host (must be fail-closed)
# SHIM_SSH_SILENT → an ssh that exits 0 having run nothing (ForceCommand, a
#                   restricted shell, an rc file that returns early). Also
#                   fail-closed: a probe that did not run is not a clean host.
[[ "${SHIM_SSH_FAIL:-0}" == "1" ]] && { echo "ssh: connect to host ${*: -2:1} port 22: Operation timed out" >&2; exit 255; }
[[ "${SHIM_SSH_SILENT:-0}" == "1" ]] && exit 0
# SHIM_SSH_NOISE → a line the remote emits before the probe's own output. Two
# flavours matter: ordinary stderr chatter (the ubiquitous known-hosts warning),
# which must be TOLERATED, and marker-shaped output, which must REFUSE.
[[ -n "${SHIM_SSH_NOISE:-}" ]] && printf '%s\n' "$SHIM_SSH_NOISE" >&2
cmd="${!#}"   # last arg = the remote command
[[ "$cmd" == *IOPlatformUUID* ]] || { echo "TRIPWIRE ssh shim got an unexpected remote command: $cmd" >> "$SHIM_TRIPWIRE"; exit 3; }
# A real remote HOME, so `[ -d "$HOME/.officraft/server" ]` is answered by the
# filesystem rather than by a canned string.
rhome="$SHIM_REMOTE_HOME"
mkdir -p "$rhome"
[[ "${SHIM_REMOTE_SERVER_TREE:-0}" == "1" ]] && mkdir -p "$rhome/.officraft/server"
# SHIM_REMOTE_TOOLS=none reproduces the real thing an ssh non-login shell does:
# Homebrew's bin dir is absent, so `tmux` is simply not found. Without this the
# harness guarantees every remote tool resolves and can never catch a probe that
# asks a question the far side cannot answer.
# The probe exports the remote Homebrew bin dir itself (gotcha #2). Rewrite that
# literal to OUR stub dir, or the command resolves the REAL tmux/launchctl of
# whatever machine this suite runs on — which both defeats the fake remote host
# and makes the result depend on the test machine's own fleet state.
rbin="$SHIM_REMOTE_BIN"
[[ "${SHIM_REMOTE_TOOLS:-all}" == "notmux" ]] && rbin="${SHIM_REMOTE_BIN}-notmux"
# Tripwire on the literal, same reason as the IOPlatformUUID one above: if
# OC_REMOTE_PATH_PREFIX ever names a different path this rewrite silently stops
# matching — and since the shim also puts $rbin on PATH unconditionally, every
# assertion would stay green while the real prefix was wrong.
[[ "$cmd" == *"/opt/homebrew/bin"* ]] || { echo "TRIPWIRE ssh shim: the probe command no longer contains the /opt/homebrew/bin literal this shim rewrites — OC_REMOTE_PATH_PREFIX changed: $cmd" >> "$SHIM_TRIPWIRE"; exit 3; }
cmd="${cmd//\/opt\/homebrew\/bin/$rbin}"
# PATH IS THE FIXTURE. Borrowing this host's /usr/bin here made "the tool is
# absent" an accident of the RUNNER rather than something the fixture builds:
# macOS has no /usr/bin/tmux, ubuntu-latest does, so the notmux case leaked the
# runner's real tmux, the probe answered live_agents=0, the guard passed, and
# the suite sailed into the teardown it plants on purpose — green on one OS,
# red on the other, testing nothing on either. $SHIM_REMOTE_BASE carries ONLY
# the generic tools the probe genuinely needs (awk/grep/id); every tool whose
# presence the test is ASSERTING ON lives in $rbin, under the fixture's control.
HOME="$rhome" PATH="$rbin:$SHIM_REMOTE_BASE" /bin/sh -c "$cmd"
SH

# The second machine's own tools, resolved on the far side of the ssh shim. They
# are SEPARATE from the local stubs on purpose: the guard must read the REMOTE
# host's identity, and a shim that answered with the local UUID would hide a guard
# that looks at the wrong machine — the exact bug this remote check exists to fix.
mkdir -p "$SHIMDIR/remote-bin"
cat > "$SHIMDIR/remote-bin/ioreg" <<'SH'
#!/usr/bin/env bash
printf '    "IOPlatformUUID" = "%s"\n' "${SHIM_REMOTE_HW-00000000-0000-0000-0000-FEEDFACE0001}"
SH
cat > "$SHIMDIR/remote-bin/launchctl" <<'SH'
#!/usr/bin/env bash
# Only `print` is reachable from the read-only probe. A bootout here would mean
# the guard is mutating the remote host, which it must never do.
if [[ "$1" == "print" ]]; then
  case "$2" in
    */com.officraft.ocwarden) [[ "${SHIM_REMOTE_WARDEN:-0}" == "1" ]] && exit 0 || exit 1 ;;
    *) exit 1 ;;
  esac
fi
echo "TRIPWIRE remote launchctl called with a non-print verb: $*" >> "$SHIM_TRIPWIRE"
exit 0
SH
cat > "$SHIMDIR/remote-bin/tmux" <<'SH'
#!/usr/bin/env bash
# The relocate target's agent sessions. Separate from the local tmux stub so a
# guard reading the LOCAL session list instead of the remote one is visible.
if [[ "${3:-}" == "ls" ]]; then
  [[ "${SHIM_REMOTE_AGENTS:-0}" == "1" ]] || exit 1
  [[ "${4:-}" == "-F" ]] && printf 'member-m-remote1\n'
  exit 0
fi
exit 0
SH
chmod +x "$SHIMDIR"/remote-bin/ioreg "$SHIMDIR"/remote-bin/launchctl "$SHIMDIR"/remote-bin/tmux
# The same stubs MINUS tmux. That is the real shape of gotcha #2: `ioreg`
# (/usr/sbin) and `launchctl` (/bin) are on an ssh non-login PATH, `tmux` is in
# Homebrew and is not — which is why a missing PATH export takes out exactly the
# liveness question and nothing else.
mkdir -p "$SHIMDIR/remote-bin-notmux"
cp "$SHIMDIR"/remote-bin/ioreg "$SHIMDIR"/remote-bin/launchctl "$SHIMDIR/remote-bin-notmux/"
export SHIM_REMOTE_BIN="$SHIMDIR/remote-bin"
# The generic half of the fake remote's PATH: the tools the probe legitimately
# needs that are NOT part of any assertion (awk, grep, id). Resolved through
# `command -v` so this works on both macOS and Linux, and kept deliberately
# small — anything not listed here is, from the probe's point of view, absent,
# which is what lets the fixture DECIDE that a tool cannot be found.
_rbase="$SHIMDIR/remote-base"
mkdir -p "$_rbase"
# `bash` is in that list although nothing in the probe calls it: every stub in
# $rbin carries a `/usr/bin/env bash` shebang, and `env` resolves its argument
# through PATH — a base dir without bash makes the fixture's own stubs
# unrunnable while leaving them perfectly present, which reads downstream as
# "the far side answered nothing" and takes out every remote case at once.
for _t in awk grep id bash; do
  _p="$(command -v "$_t" 2>/dev/null || true)"
  # An ABSOLUTE path, not merely a non-empty answer. `command -v` reports an
  # exported shell function as the bare word, and `ln -sf awk .../remote-base/awk`
  # then makes a symlink pointing at itself: ELOOP, which the far side cannot tell
  # apart from "that tool is not installed" — the exact disguise this whole commit
  # is about, rebuilt one level down in the thing meant to prevent it.
  [[ "$_p" == /* ]] || { echo "tests_guard: cannot build the fake remote PATH — '$_t' did not resolve to an absolute path on this machine (got: '${_p:-<nothing>}')" >&2; exit 2; }
  ln -sf "$_p" "$_rbase/$_t"
done
unset _t _p
# TRIPWIRE, checked before any case runs. If a tool the fixture withholds ever
# becomes reachable through this base dir again, say so loudly HERE — the
# alternative is what actually happened: the affected cases keep passing on the
# OS that happens to lack the tool, and on the OS that has it they fail as an
# unrelated-looking guard bug.
# The list is DERIVED from the stubs rather than written out, so a tool added to
# remote-bin/ later is protected the day it is added. Spelling it out meant the
# protection silently did not extend to anything new — and a guard that covers
# only what someone remembered to list is the shape this suite exists to catch.
# `ssh` is appended because the fake remote must never reach a real one.
# Array glob, not `$(… printf '%s\n' *)`. Unquoted command substitution word-splits
# and RE-GLOBS: with remote-bin/ empty the `*` survives literally and expands
# against the caller's cwd, so the loop iterates repo files, matches nothing, and
# the tripwire passes — checking nothing, silently. Whitespace or a glob character
# in a stub name skips that stub the same quiet way. That is the very failure this
# tripwire exists to catch, one level down, so the empty case is made loud too.
_stubs=("$SHIMDIR"/remote-bin/*)
[[ -e "${_stubs[0]}" ]] || { echo "tests_guard: remote-bin/ is empty — the base-dir tripwire would be checking nothing" >&2; exit 2; }
for _t in "${_stubs[@]##*/}" ssh; do
  if PATH="$_rbase" command -v "$_t" >/dev/null 2>&1; then
    echo "tests_guard: the fake remote base dir resolves '$_t' — the fixture no longer controls whether that tool exists on the far side" >&2
    exit 2
  fi
done
unset _t
export SHIM_REMOTE_BASE="$_rbase"
unset _rbase _stubs

chmod +x "$SHIMDIR"/launchctl "$SHIMDIR"/lsof "$SHIMDIR"/tmux "$SHIMDIR"/ioreg "$SHIMDIR"/ssh

# These stubs are enabled ONLY by the hermetic teardown regression below.  They
# record every mutating surface instead of touching the host, which lets the
# test reject a canonical label/root/token target without ever exercising a
# real warden teardown.
cat > "$SHIMDIR/ocwarden" <<'SH'
#!/usr/bin/env bash
echo "ocwarden namespace=${OC_NAMESPACE:-<unset>} args=$*" >> "$SHIM_TEARDOWN_LOG"
exit 0
SH
cat > "$SHIMDIR/rm" <<'SH'
#!/usr/bin/env bash
if [[ "${SHIM_ALLOW_TEARDOWN:-0}" == "1" ]]; then
  # The BRACE GROUP is load-bearing. Written as three bare printfs with the
  # redirection on the last one only, the first two go to stdout and the log
  # receives a lone newline (0x0a) — no rm target is ever recorded, so every
  # tripwire that greps this log matches nothing and passes unconditionally.
  # Case (18c) is the permanent proof that this records what it claims.
  { printf 'rm'; printf ' <%s>' "$@"; printf '\n'; } >> "$SHIM_TEARDOWN_LOG"
  exit 0
fi
exec /bin/rm "$@"
SH
chmod +x "$SHIMDIR"/ocwarden "$SHIMDIR"/rm
export SHIM_TRIPWIRE="$TRIPWIRE"
export PATH="$SHIMDIR:$PATH"

# run_guard — source the lib + run a guard/allocator snippet in a SUBSHELL with a
# clean, controlled env. Echoes "<exit_code>". Stderr is captured to $GLOG.
GLOG="$SHIMDIR/.glog"
run_snippet() {
  local snippet="$1"; shift
  ( set +e
    # clean the isolation env so each case is deterministic.
    unset OC_NS OC_E2E_ALLOW_CANONICAL OC_E2E_NS OC_E2E_NS_PORT 2>/dev/null || true
    export HOME="${TEST_HOME:-/tmp/oc-guard-home}"
    # SNIPPET_LIB lets a NEGATIVE CONTROL source a deliberately mutated copy of
    # the lib (see 18c/18d) so the tripwires' discriminating power is pinned.
    source "${SNIPPET_LIB:-$LIB}" >/dev/null 2>&1
    eval "$snippet"
  ) >"$GLOG" 2>&1
  echo $?
}

# Discover the CURRENT canonical serve port from the single source of truth
# (same derivation the lib itself does from server/ocserverd/config.go) —
# NOT a hardcoded literal, so this test file doesn't become a drift site of
# its own the next time the port changes (T-b76b follow-up: Kyle's review
# note — hardcoding "7755" here would just be swapping one stale literal for
# another).
CANON_PORT="$(run_snippet 'printf "PORT=%s\n" "$OC_CANONICAL_SERVE_PORT"' >/dev/null; grep '^PORT=' "$GLOG" | cut -d= -f2)"
[[ -n "$CANON_PORT" ]] || { echo "FATAL: could not discover OC_CANONICAL_SERVE_PORT via $LIB" >&2; exit 2; }

echo "[tests_guard] hermetic isolation-layer unit tests"

# ── 1) live warden + CANONICAL mode → guard DIES ─────────────────────────────
rc="$(SHIM_WARDEN=1 run_snippet 'OC_NS=""; oc_live_fleet_guard')"
[[ "$rc" != "0" ]] && ok "live warden + canonical → guard dies (rc=$rc)" || bad "live warden + canonical → guard should die"
grep -q 'LIVE-FLEET GUARD' "$GLOG" && ok "die message names LIVE-FLEET GUARD" || bad "die message missing LIVE-FLEET GUARD marker"

# ── 2) no live fleet + CANONICAL → guard PASSES ──────────────────────────────
rc="$(SHIM_WARDEN=0 SHIM_LISTEN_PORTS="" SHIM_SESSIONS="" run_snippet 'OC_NS=""; oc_live_fleet_guard')"
check "no fleet + canonical → guard passes" "0" "$rc"

# ── 3) live warden + NAMESPACE mode → guard COEXISTS (passes) ─────────────────
rc="$(SHIM_WARDEN=1 run_snippet 'OC_NS="e2eabc123"; oc_live_fleet_guard')"
check "live warden + namespace → guard coexists (returns 0)" "0" "$rc"
grep -q 'coexist' "$GLOG" && ok "namespace-mode guard logs coexistence" || bad "namespace-mode guard should log coexistence"

# ── 4) detection fires on a member-* session on the canonical socket ──────────
rc="$(SHIM_WARDEN=0 SHIM_LISTEN_PORTS="" SHIM_SESSIONS="member-m-abc123" \
      run_snippet 'oc_detect_live_canonical_fleet | grep -q "canonical tmux socket"')"
check "member-* on canonical socket is detected" "0" "$rc"

# ── 5) detection fires on a canonical-port listener (port from CANON_PORT) ───
rc="$(SHIM_WARDEN=0 SHIM_LISTEN_PORTS="$CANON_PORT" SHIM_SESSIONS="" \
      run_snippet "oc_detect_live_canonical_fleet | grep -q 'serve port $CANON_PORT'")"
check "canonical $CANON_PORT listener is detected" "0" "$rc"

# ── 6) detection is EMPTY on a clean host ────────────────────────────────────
rc="$(SHIM_WARDEN=0 SHIM_LISTEN_PORTS="" SHIM_SESSIONS="" \
      run_snippet 'out="$(oc_detect_live_canonical_fleet)"; [[ -z "$out" ]]')"
check "clean host → detection empty" "0" "$rc"

# ── 7) NAMESPACE allocation: every axis is non-canonical ─────────────────────
run_snippet 'oc_resolve_instance
  printf "NS=%s\n" "$OC_NS"
  printf "PORT=%s\n" "${LOCAL_BASE##*:}"
  printf "SERVE=%s\n" "$SERVE_LABEL"
  printf "WARDEN=%s\n" "$WARDEN_LABEL"
  printf "ROOT=%s\n" "$OC_ROOT"
  printf "SOCK=%s\n" "$TMUX_SOCKET_LOCAL"' >/dev/null
NS="$(grep '^NS=' "$GLOG" | cut -d= -f2)"
PORT="$(grep '^PORT=' "$GLOG" | cut -d= -f2)"
SERVE="$(grep '^SERVE=' "$GLOG" | cut -d= -f2)"
WARDEN="$(grep '^WARDEN=' "$GLOG" | cut -d= -f2)"
ROOT="$(grep '^ROOT=' "$GLOG" | cut -d= -f2)"
SOCK="$(grep '^SOCK=' "$GLOG" | cut -d= -f2)"
[[ "$NS" =~ ^[a-z0-9-]{1,16}$ ]] && ok "ns '$NS' matches product charset [a-z0-9-]{1,16}" || bad "ns '$NS' violates charset"
[[ "$PORT" != "$CANON_PORT" && "$PORT" != "8766" && "$PORT" != "8790" && "$PORT" != "8791" && "$PORT" != "8795" ]] \
  && ok "port $PORT is non-canonical/non-reserved" || bad "port $PORT collides with a reserved port"
[[ "$SERVE" == "com.officraft.serve.$NS" ]] && ok "serve label namespaced ($SERVE)" || bad "serve label wrong: $SERVE"
[[ "$WARDEN" == "com.officraft.ocwarden.$NS" && "$WARDEN" != "com.officraft.ocwarden" ]] \
  && ok "warden label namespaced ($WARDEN)" || bad "warden label wrong: $WARDEN"
[[ "$ROOT" == *"/.officraft-$NS" && "$ROOT" != *"/.officraft" ]] \
  && ok "root namespaced ($ROOT)" || bad "root wrong: $ROOT"
[[ "$SOCK" == "officraft-$NS" && "$SOCK" != "officraft" ]] \
  && ok "tmux socket namespaced ($SOCK)" || bad "socket wrong: $SOCK"

# ── 8) CANONICAL escape hatch: axes resolve to the canonical port ─────────────
# T-191d: the port the 0c guard verifies free (SINGLE_PROD_PORTS[0]) and the port
# this run actually OWNS (LOCAL_BASE/PUBLIC_HOST → oc_fresh_install pins serve to
# ${LOCAL_BASE##*:}) MUST be the SAME canonical port. The old bug: the canonical
# branch set SINGLE_PROD_PORTS to the dynamic CANON_PORT but left LOCAL_BASE at a
# hardcoded 8770, so the guard watched one port while the install bound another —
# and this test only checked SINGLE_PROD_PORTS, so it stayed green. Now assert the
# owned port too, AND that it equals the guarded port (the coupling invariant),
# against CANON_PORT (SSOT-derived, never a hardcoded literal of its own).
run_snippet 'export OC_E2E_ALLOW_CANONICAL=1; oc_resolve_instance
  printf "NS=[%s]\n" "$OC_NS"
  printf "PORTS=%s\n" "${SINGLE_PROD_PORTS[*]}"
  printf "GUARD0=%s\n" "${SINGLE_PROD_PORTS[0]}"
  printf "LB=%s\n" "${LOCAL_BASE##*:}"
  printf "PH=%s\n" "${PUBLIC_HOST##*:}"' >/dev/null
C8_GUARD0="$(grep '^GUARD0=' "$GLOG" | cut -d= -f2)"
C8_LB="$(grep '^LB=' "$GLOG" | cut -d= -f2)"
C8_PH="$(grep '^PH=' "$GLOG" | cut -d= -f2)"
[[ "$(grep '^NS=' "$GLOG")" == "NS=[]" ]] && ok "canonical escape hatch → OC_NS empty" || bad "canonical OC_NS not empty: $(grep '^NS=' "$GLOG")"
[[ "$(grep '^PORTS=' "$GLOG")" == "PORTS=$CANON_PORT 8766" ]] && ok "canonical guard ports = $CANON_PORT 8766" || bad "canonical ports wrong: $(grep '^PORTS=' "$GLOG")"
[[ "$C8_LB" == "$CANON_PORT" ]] && ok "canonical LOCAL_BASE port == $CANON_PORT (the port this run OWNS = current canonical)" || bad "canonical LOCAL_BASE port wrong: got '$C8_LB' want '$CANON_PORT' (T-191d: guard watches a port the run does not bind)"
[[ "$C8_PH" == "$CANON_PORT" ]] && ok "canonical PUBLIC_HOST port == $CANON_PORT" || bad "canonical PUBLIC_HOST port wrong: got '$C8_PH' want '$CANON_PORT'"
[[ -n "$C8_LB" && "$C8_LB" == "$C8_GUARD0" ]] && ok "canonical coupling: owned port (LOCAL_BASE $C8_LB) == guard port (SINGLE_PROD_PORTS[0] $C8_GUARD0)" || bad "canonical DECOUPLED: owned port '$C8_LB' != guard port '$C8_GUARD0' — the exact T-191d shape (guard verifies one port, install binds another)"

# ── 9) agent_workdir is namespace-aware (a1_zombie kill-anchor safety) ────────
rc="$(run_snippet 'OC_NS="e2ex"; wd="$(agent_workdir /Users/x mira)"; [[ "$wd" == "/Users/x/.officraft-e2ex/agents/mira" ]]')"
check "agent_workdir namespaced under ns" "0" "$rc"
rc="$(run_snippet 'unset OC_NS; wd="$(agent_workdir /Users/x mira)"; [[ "$wd" == "/Users/x/.officraft/agents/mira" ]]')"
check "agent_workdir canonical when ns unset (zero-diff)" "0" "$rc"

# ── 10) TRIPWIRE: no guard/allocator ever called launchctl bootout ───────────
if [[ -s "$TRIPWIRE" ]]; then bad "launchctl bootout was invoked: $(cat "$TRIPWIRE")"; else ok "no teardown/bootout invoked by any guard/allocator path"; fi

# ── 11) T-d41a: run_all.sh must still PRINT "[run_all] specs exit=<rc>" when a
#        spec fails. This is an OUTPUT assertion, on purpose: the bug it guards
#        is rc-blind. lib/common.sh used to `set -euo pipefail`, and because it
#        is SOURCED, the `-e` leaked into run_all.sh (which deliberately runs
#        `set -uo pipefail` so it can capture rc itself). Under the leaked `-e`
#        the failing playwright subshell killed the script BEFORE `RC=$?` and
#        the echo — the run still exited non-zero with the SAME code, so a
#        rc-only assertion stays green while the diagnostic line is gone.
#        "Failed for the wrong reason" and "correctly reported the failure"
#        share one exit code; only the output tells them apart.
#
#        Fidelity: the preamble (the `set -` line and the `source .../common.sh`
#        line) and the reporting tail (`RC=$?` + the echo) are lifted VERBATIM
#        from run_all.sh, so this reproduces the real interaction against the
#        real lib/common.sh. Only the playwright invocation is stood in for by a
#        subshell that exits 7 (hermetic: no browser, no server, no ports).
RUN_ALL="$HERE/../run_all.sh"
if [[ ! -f "$RUN_ALL" ]]; then
  bad "run_all.sh not found at $RUN_ALL"
else
  # Every one of these four locates a STATEMENT, so every pattern is anchored at
  # column 0 and shaped like the statement. An unanchored `-F` on the literal
  # would also match a COMMENT that merely mentions it, and then the fixture
  # below is reconstructed out of a comment: it echoes nothing and this case
  # fails naming lib/common.sh's `set -e`, which had nothing to do with it.
  # (Measured before this was anchored: one ordinary comment added to run_all.sh
  # mentioning the report line took tests_guard to PASS=152 FAIL=1 rc=1.)
  D41A_SET="$(grep -m1 -E '^set +-' "$RUN_ALL" || true)"
  D41A_SRC="$(grep -m1 -E '^source "\$HERE/lib/common\.sh"' "$RUN_ALL" || true)"
  D41A_RC="$(grep -m1 -E '^RC=\$\?' "$RUN_ALL" || true)"
  D41A_ECHO="$(grep -m1 -E '^echo "\[run_all\] specs exit=' "$RUN_ALL" || true)"
  if [[ -z "$D41A_SET" || -z "$D41A_SRC" || -z "$D41A_RC" || -z "$D41A_ECHO" ]]; then
    bad "run_all.sh no longer has the expected set/source/RC/echo shape — update guard (11)"
  else
    D41A_SH="$SHIMDIR/d41a_run_all_shape.sh"
    {
      echo '#!/usr/bin/env bash'
      echo "$D41A_SET"
      printf 'HERE=%q\n' "$(cd "$HERE/.." && pwd)"
      echo "$D41A_SRC"
      echo '( exit 7 )   # stand-in for the failing `npx playwright test` subshell'
      echo "$D41A_RC"
      echo "$D41A_ECHO"
      echo 'exit $RC'
    } > "$D41A_SH"
    D41A_OUT="$(bash "$D41A_SH" 2>&1)"; D41A_EXIT=$?
    if [[ "$D41A_OUT" == *"[run_all] specs exit=7"* ]]; then
      ok "spec failure still PRINTS '[run_all] specs exit=7' (sourcing common.sh leaks no -e)"
    else
      bad "spec-failure report line MISSING — got output '$D41A_OUT' (rc=$D41A_EXIT). \
lib/common.sh likely re-enabled 'set -e'; it is SOURCED, so -e leaks into run_all.sh \
and kills it before RC=\$? — same exit code, no diagnostic line."
    fi
    # Secondary (NOT the headline): the rc must still propagate. Deliberately
    # asserted after the output check so the output regression is what reddens.
    check "spec failure rc still propagates through run_all.sh" "7" "$D41A_EXIT"
    # And the sourced lib must not silently re-arm errexit in a non -e caller.
    rc="$(bash -c 'set -uo pipefail; source "$1" >/dev/null 2>&1; case $- in *e*) exit 1;; *) exit 0;; esac' _ "$HERE/../lib/common.sh"; echo $?)"
    check "sourcing lib/common.sh does not turn on errexit in a non-'-e' caller" "0" "$rc"
    # Converse: a caller that DID ask for -e must keep it (setup.sh et al).
    rc="$(bash -c 'set -euo pipefail; source "$1" >/dev/null 2>&1; case $- in *e*) exit 0;; *) exit 1;; esac' _ "$HERE/../lib/common.sh"; echo $?)"
    check "sourcing lib/common.sh preserves errexit for callers that set it" "0" "$rc"

    # ADJACENCY (static, complements the behavioural check above). The synthetic
    # script builds the tail adjacent BY CONSTRUCTION, so it is blind to someone
    # inserting a command between `npx playwright test` and `RC=$?` in the real
    # file. `$?` is clobbered by ANY intervening command, so a single line slipped
    # in there silently reports the WRONG rc — the line still prints, so the
    # behavioural assertion stays green. Hence a textual adjacency assertion on
    # the real run_all.sh. Comments/blank lines are NOT tolerated between them:
    # they are harmless to `$?` today, but permitting them is what makes room for
    # a command to be added later without anything reddening.
    D41A_PWLINE="$(grep -nE '^\(.*playwright test *\)' "$RUN_ALL" | head -1 | cut -d: -f1)"
    if [[ -z "$D41A_PWLINE" ]]; then
      bad "cannot locate the 'npx playwright test' line in run_all.sh — update guard (11)"
    else
      D41A_NEXT="$(sed -n "$((D41A_PWLINE+1))p" "$RUN_ALL")"
      D41A_NEXT2="$(sed -n "$((D41A_PWLINE+2))p" "$RUN_ALL")"
      [[ "$D41A_NEXT" =~ ^RC=\$\? ]] \
        && ok "RC=\$? is IMMEDIATELY after the playwright run (rc not clobbered)" \
        || bad "line after 'playwright test' is '$D41A_NEXT', expected 'RC=\$?' — anything in between clobbers \$? and run_all.sh reports the WRONG exit code while still printing the line"
      [[ "$D41A_NEXT2" == *'[run_all] specs exit=$RC'* ]] \
        && ok "the report echo immediately follows RC=\$?" \
        || bad "line after 'RC=\$?' is '$D41A_NEXT2', expected the '[run_all] specs exit=\$RC' echo"
    fi
  fi
fi

# ── 12) T-c5d4 weakness-2: webdist restore must SURFACE a failed/partial delete,
#        not swallow it. teardown.sh used `find … -delete 2>/dev/null` with no rc
#        check — a silent failure leaves a dirty webdist that a later `go build`
#        bakes into the committed bin/ocserverd. oc_restore_webdist_pristine now
#        checks find's rc AND re-asserts only .gitkeep remains, printing a loud
#        WARN on trouble. OUTPUT+rc assertion on purpose: a fail-closed cleanup is
#        rc-blind to a half-delete, so we assert the reason/output, not only rc.
TEARDOWN="$HERE/../teardown.sh"
if ! grep -q 'oc_restore_webdist_pristine' "$TEARDOWN"; then
  bad "teardown.sh no longer calls oc_restore_webdist_pristine — update guard (12)"
elif grep -Eq 'find .*-delete.*2>/dev/null' "$TEARDOWN"; then
  bad "teardown.sh reintroduced 'find … -delete 2>/dev/null' — the stderr swallow that hid the failure (weakness-2)"
else
  ok "teardown.sh delegates webdist cleanup to oc_restore_webdist_pristine, no stderr swallow"
  # positive control: clean, fully-removable content restores quietly, rc 0.
  WT_POS="$(mktemp -d -t oc-webdist-pos.XXXXXX)"
  touch "$WT_POS/.gitkeep" "$WT_POS/index.html"; mkdir -p "$WT_POS/assets"; touch "$WT_POS/assets/app.js"
  POS_OUT="$( ( source "$HERE/../lib/common.sh" >/dev/null 2>&1; oc_restore_webdist_pristine "$WT_POS" ) 2>&1 )"; POS_RC=$?
  check "webdist restore: clean dir returns 0" "0" "$POS_RC"
  POS_LEFT="$(find "$WT_POS" -mindepth 1 -not -name '.gitkeep' | wc -l | tr -d ' ')"
  check "webdist restore: clean dir leaves only .gitkeep" "0" "$POS_LEFT"
  case "$POS_OUT" in
    *WARN*) bad "webdist restore: clean dir must NOT warn (got: $POS_OUT)" ;;
    *restored*) ok "webdist restore: clean dir prints 'restored', no WARN (positive control)" ;;
    *) bad "webdist restore: clean dir unexpected output: $POS_OUT" ;;
  esac
  rm -rf "$WT_POS"
  # negative control: an entry -delete CANNOT remove (dir chmod 000 → EACCES) —
  # the exact failure the old 2>/dev/null swallowed. NOTE: assumes a non-root
  # runner (ci.sh runs as the developer); as root -delete would succeed and this
  # case would REDDEN (fail-closed, never a false green).
  WT_NEG="$(mktemp -d -t oc-webdist-neg.XXXXXX)"
  touch "$WT_NEG/.gitkeep"; mkdir -p "$WT_NEG/locked"; touch "$WT_NEG/locked/app.js"; chmod 000 "$WT_NEG/locked"
  NEG_OUT="$( ( source "$HERE/../lib/common.sh" >/dev/null 2>&1; oc_restore_webdist_pristine "$WT_NEG" ) 2>&1 )"; NEG_RC=$?
  chmod 755 "$WT_NEG/locked" 2>/dev/null || true
  check "webdist restore: un-removable entry returns 1 (not swallowed)" "1" "$NEG_RC"
  case "$NEG_OUT" in
    *WARN*) ok "webdist restore: a FAILED delete emits a loud WARN (weakness-2 mutant reddens)" ;;
    *) bad "webdist restore: FAILED delete produced NO warn — the silent-failure bug (got: $NEG_OUT)" ;;
  esac
  rm -rf "$WT_NEG" 2>/dev/null || true
fi

# ── 13) T-191d: a1_zombie's post-teardown "port freed" corroboration must probe
#        the port THIS run OWNED (derived from LOCAL_BASE), not a hardcoded literal.
#        A literal we never bound (the retired 8770) makes the check vacuous — it
#        stays green even when teardown leaks our real listener (retired port is
#        always free). Static assertion on the real a1_zombie_e2e.sh: the owned
#        port must be derived from LOCAL_BASE AND the clean-slate lsof must probe
#        it — reverting to a hardcoded :<port> drops both and reddens here.
A1="$HERE/../a1_zombie_e2e.sh"
if [[ ! -f "$A1" ]]; then
  bad "a1_zombie_e2e.sh not found at $A1 — update guard (13)"
else
  if grep -Fq 'owned_port="${LOCAL_BASE##*:}"' "$A1"; then
    ok "a1_zombie derives owned_port from LOCAL_BASE (not a hardcoded literal)"
  else
    bad "a1_zombie no longer derives owned_port from LOCAL_BASE — post-teardown port check may have re-hardcoded a literal (T-191d regression)"
  fi
  if grep -Fq 'lsof -nP -iTCP:"$owned_port" -sTCP:LISTEN' "$A1"; then
    ok "a1_zombie post-teardown lsof probes the OWNED port (\${LOCAL_BASE##*:}), not a stale constant"
  else
    bad "a1_zombie post-teardown lsof no longer probes \"\$owned_port\" — a vacuous hardcoded-port check would stay green when teardown leaks the real listener (T-191d)"
  fi
fi

# ── 14) T-191d(E): cross_machine.sh's LOCAL_BASE default must be the CURRENT
#        canonical serve port (SSOT-derived), never a literal.
#        cross_machine.sh is CANONICAL BY CONSTRUCTION — it does NOT call
#        oc_resolve_instance (so case (8) structurally cannot see it; that is
#        exactly how this site survived the core package) — and it PINS the
#        seeded oc.toml's serve port to ${LOCAL_BASE##*:}. A stale literal there
#        therefore makes the run BIND one port while oc_lifecycle.sh's live-fleet
#        guard watches OC_CANONICAL_SERVE_PORT: the guard clears a port nobody
#        binds. BEHAVIOURAL, not a grep-for-a-string: the real assignment line is
#        lifted verbatim out of cross_machine.sh and EVALUATED with the real lib
#        sourced, so what is asserted is the value the script actually computes.
CM="$HERE/../cross_machine.sh"
if [[ ! -f "$CM" ]]; then
  bad "cross_machine.sh not found at $CM — update guard (14)"
else
  CM_LINE="$(grep -m1 -E '^LOCAL_BASE="\$\{LOCAL_BASE:-' "$CM" || true)"
  if [[ -z "$CM_LINE" ]]; then
    bad "cross_machine.sh no longer has a 'LOCAL_BASE=\"\${LOCAL_BASE:-…}\"' default line — update guard (14)"
  else
    run_snippet 'unset LOCAL_BASE
'"$CM_LINE"'
      printf "CMLB=%s\n" "${LOCAL_BASE##*:}"' >/dev/null
    C14_LB="$(grep '^CMLB=' "$GLOG" | cut -d= -f2)"
    [[ -n "$C14_LB" && "$C14_LB" == "$CANON_PORT" ]] \
      && ok "cross_machine LOCAL_BASE default port == $CANON_PORT (SSOT-derived, evaluated from the real line)" \
      || bad "cross_machine LOCAL_BASE default port is '$C14_LB', want '$CANON_PORT' — a hardcoded/retired literal makes the canonical run BIND a port the live-fleet guard is not watching (T-191d E)"
    # Discriminating control: prove the assertion above CAN fail. Push the
    # pre-fix shape through the identical evaluation path and require it to
    # disagree with CANON_PORT — otherwise case (14) would be vacuous.
    run_snippet 'unset LOCAL_BASE
      LOCAL_BASE="${LOCAL_BASE:-http://127.0.0.1:8770}"
      printf "CMLB=%s\n" "${LOCAL_BASE##*:}"' >/dev/null
    C14_CTL="$(grep '^CMLB=' "$GLOG" | cut -d= -f2)"
    [[ "$C14_CTL" == "8770" && "$C14_CTL" != "$CANON_PORT" ]] \
      && ok "control: pre-fix shape evaluates to 8770 != $CANON_PORT (case 14 can actually redden)" \
      || bad "control broken: pre-fix shape evaluated to '$C14_CTL' — case (14) may be vacuous"
    # SENTINEL: an explicit LOCAL_BASE= override must still win. fail-closed
    # must be ACCURATE, not merely wide — the legitimate override path is the
    # only way an operator points this at a namespaced/second instance.
    run_snippet 'LOCAL_BASE="http://127.0.0.1:8799"
'"$CM_LINE"'
      printf "CMLB=%s\n" "${LOCAL_BASE##*:}"' >/dev/null
    C14_OVR="$(grep '^CMLB=' "$GLOG" | cut -d= -f2)"
    check "sentinel: explicit LOCAL_BASE override still wins in cross_machine" "8799" "$C14_OVR"
  fi
fi

# ── 15/16) T-191d(D): the TWO prod-port REFUSAL LISTS must cover the CURRENT
#        prod port, derived from the SSOT (server/ocserverd/config.go's
#        defaultPort) — not only retired literals.
#
#        WHY THIS IS THE IMPORTANT ONE: prod is live on the canonical port right
#        now. These harnesses are DESTRUCTIVE (setup/teardown kill listeners and
#        wipe state). A refusal list that enumerates only RETIRED ports is GREEN
#        while protecting nothing: an operator who sets OC_E2E_PORT /
#        OC_CONF_PORT to the CURRENT prod port walks straight into the live
#        station and the guard never speaks. T-a3ba (56f47bc) fixed the code in
#        both files but shipped NO test — nothing anywhere reddened if the
#        SSOT-derived entry were dropped again. These cases are that test.
#
#        The two sites are asserted SEPARATELY on purpose: one shared assertion
#        would redden when EITHER drifts, which actively HIDES the other site
#        being uncovered.
#
#        BEHAVIOURAL: both files are really executed. Neither reaches a side
#        effect — common.sh is pure assignment, and conformance/run.sh's refusal
#        loop sits before the venv/build/bind steps and exits 2 there.
C15_COMMON="$HERE/../lib/common.sh"
C16_CONF="$HERE/../../conformance/run.sh"

# helper: source common.sh with a given OC_E2E_PORT, capture combined output.
c15_run() { OC_E2E_PORT="$1" bash -c 'source "$1" >/dev/null' _ "$C15_COMMON" 2>&1; }

if [[ ! -f "$C15_COMMON" ]]; then
  bad "lib/common.sh not found at $C15_COMMON — update guard (15)"
else
  # (15a) CURRENT prod port refused — reddens iff common.sh's list loses its
  #       SSOT-derived entry. This is the safety hole itself.
  case "$(c15_run "$CANON_PORT")" in
    *"is a PROD port"*) ok "common.sh REFUSES the CURRENT prod port ($CANON_PORT, SSOT-derived)" ;;
    *) bad "common.sh ACCEPTED OC_E2E_PORT=$CANON_PORT — the live prod port. The 'never touch prod' guard is blind to the only port prod actually uses (T-191d D)" ;;
  esac
  # (15b) retired ports stay refused — this is an ADD, not a REPLACE. Some
  #       install may still have 8770 pinned in its oc.toml.
  case "$(c15_run 8770)" in
    *"is a PROD port"*) ok "common.sh still refuses the RETIRED 8770 (added to, not swapped for, the SSOT entry)" ;;
    *) bad "common.sh no longer refuses 8770 — retired defaults must STAY in the list (an install may still pin one)" ;;
  esac
  # (15c) SENTINEL: the legitimate isolated port must still be allowed. A guard
  #       that refuses everything is not safer, it is just broken — this repo
  #       has already shipped an over-wide fail-closed once.
  C15_OK_OUT="$(c15_run 8791)"
  case "$C15_OK_OUT" in
    *"is a PROD port"*) bad "sentinel BROKEN: common.sh refused the legitimate isolated port 8791 — fail-closed must be accurate, not wide" ;;
    *) ok "sentinel: common.sh still ACCEPTS the legitimate isolated port 8791" ;;
  esac
  # (15d) no SILENT degradation: if the SSOT cannot be parsed, common.sh must
  #       FATAL. Degrading to an empty/partial list would delete the guard while
  #       looking exactly like a healthy run. Executed against a throwaway tree
  #       with no server/ocserverd/config.go.
  C15_T="$(mktemp -d -t oc-guard-nossot.XXXXXX)"
  mkdir -p "$C15_T/e2e_test/lib"
  cp "$C15_COMMON" "$C15_T/e2e_test/lib/common.sh"
  C15_NOSSOT="$(OC_E2E_PORT=8791 bash -c 'source "$1" >/dev/null' _ "$C15_T/e2e_test/lib/common.sh" 2>&1)"
  case "$C15_NOSSOT" in
    *"could not parse"*) ok "common.sh FATALs when the SSOT (config.go defaultPort) is unparseable — no silent empty prod-port list" ;;
    *) bad "common.sh did NOT fatal with an unparseable SSOT (got: ${C15_NOSSOT:-<silence>}) — a silently empty PROD_PORTS is a guard that vanished while staying green (T-191d D)" ;;
  esac
  rm -rf "$C15_T" 2>/dev/null || true
fi

if [[ ! -f "$C16_CONF" ]]; then
  bad "conformance/run.sh not found at $C16_CONF — update guard (16)"
else
  # Stubs so the SENTINEL run below cannot proceed past the refusal gate into
  # venv creation / go build / port bind. EVERY stub emits the same marker, so
  # the sentinel proves "the gate was passed" no matter which post-gate step the
  # run happens to reach first.
  #
  # Why all three: which step comes first is ENVIRONMENT-DEPENDENT. With no
  # conformance/.venv the run tries `uv`/`python3` (line ~106); with a .venv
  # already present (e.g. a previous bin/ci.sh run in this same tree) it skips
  # straight to the `lsof` leftover-guard (line ~130). Stubbing only the venv
  # pair made this case flip to INCONCLUSIVE the moment CI had run here once.
  # All of these sit strictly AFTER the prod-port refusal loop, so reaching any
  # of them is proof the legitimate port was let through.
  C16_SHIM="$(mktemp -d -t oc-guard-conf.XXXXXX)"
  for _c in uv python3; do
    printf '#!/usr/bin/env bash\necho "SENTINEL_PAST_PROD_GATE" >&2\nexit 1\n' > "$C16_SHIM/$_c"
    chmod +x "$C16_SHIM/$_c"
  done
  # lsof: exit 0 = "port occupied" so run.sh stops at its leftover guard. This
  # stub CANNOT emit the marker — run.sh calls it as `lsof … >/dev/null 2>&1`,
  # which swallows both streams — so the evidence for that path is run.sh's own
  # post-gate "already in use" FATAL instead. Accepted below.
  printf '#!/usr/bin/env bash\nexit 0\n' > "$C16_SHIM/lsof"
  chmod +x "$C16_SHIM/lsof"
  c16_run() { OC_CONF_PORT="$1" PATH="$C16_SHIM:$PATH" SHIM_LISTEN_PORTS="$1" \
                bash "$C16_CONF" --target go 2>&1; }
  # (16a) CURRENT prod port refused — the twin of (15a), asserted independently.
  case "$(c16_run "$CANON_PORT")" in
    *"is a PROD port"*) ok "conformance/run.sh REFUSES the CURRENT prod port ($CANON_PORT, SSOT-derived)" ;;
    *) bad "conformance/run.sh ACCEPTED OC_CONF_PORT=$CANON_PORT — the live prod port; its refusal list is blind to prod (T-191d D)" ;;
  esac
  # (16b) retired ports stay refused (ADD, not REPLACE).
  case "$(c16_run 8770)" in
    *"is a PROD port"*) ok "conformance/run.sh still refuses the RETIRED 8770" ;;
    *) bad "conformance/run.sh no longer refuses 8770 — retired defaults must STAY in the list" ;;
  esac
  # (16c) SENTINEL: the legitimate conformance port must still get THROUGH the
  #       gate (proved by reaching the stubbed venv step), not be refused.
  C16_OK_OUT="$(c16_run 8795)"
  case "$C16_OK_OUT" in
    *"is a PROD port"*) bad "sentinel BROKEN: conformance/run.sh refused explicit non-prod override 8795" ;;
    *"SENTINEL_PAST_PROD_GATE"*|*"already in use"*) ok "sentinel: conformance/run.sh lets explicit non-prod override 8795 through the prod gate" ;;
    *) bad "sentinel INCONCLUSIVE: 8795 was not refused, but the run never reached the post-gate step — this case may no longer be testing what it claims (got: ${C16_OK_OUT:-<silence>})" ;;
  esac
  # (16d) no SILENT degradation on an unparseable SSOT (twin of 15d). Also pins
  #       the `|| true` that keeps this from dying at the assignment under -e.
  C16_T="$(mktemp -d -t oc-guard-confnossot.XXXXXX)"
  mkdir -p "$C16_T/conformance"
  cp "$C16_CONF" "$C16_T/conformance/run.sh"
  C16_NOSSOT="$(OC_CONF_PORT=8795 bash "$C16_T/conformance/run.sh" --target go 2>&1)"
  case "$C16_NOSSOT" in
    *"could not parse"*) ok "conformance/run.sh FATALs (and SPEAKS) when the SSOT is unparseable — no silent empty refusal list" ;;
    *) bad "conformance/run.sh did NOT print its parse FATAL with an unparseable SSOT (got: ${C16_NOSSOT:-<silence>}) — the guard died silently at the assignment or degraded to an empty list (T-191d D / T-a3ba F2)" ;;
  esac
  # (16e) T-0e4b: the DEFAULT. With OC_CONF_PORT UNSET the port handed to the
  #       daemon must be 0, so the KERNEL allocates it at bind time — that is
  #       the whole reason two conformance runs can now go in parallel. Every
  #       other case here (16a-16d, 15*) pins an EXPLICIT OC_CONF_PORT, so all
  #       of them stay green no matter what the default is: before this case,
  #       reverting the default to a fixed port left the entire suite green and
  #       only a hand-run CONCURRENT pair could tell (CI never runs one).
  #
  #       Asserted on the port run.sh actually hands the daemon — the `port =`
  #       it writes into the throwaway oc.toml that ocserverd binds from
  #       (cfg.Server.Port → net.Listen, server.go's cmdServe) — NOT on run.sh's
  #       source text. A grep for ":-0}" would pin the SPELLING, and would go
  #       silently vacuous the first time someone rewrote the expression.
  #
  #       NOTHING is compiled and NOTHING is bound, so this costs about as much
  #       as 16a-16d: a throwaway tree gets a fake suite venv plus the three
  #       embed-staging sentinels (so build-seedsdist/docsdist/bindist all skip),
  #       and a `go` shim whose "build" emits a stub ocserverd instead of
  #       compiling one. run.sh's FIRST use of that binary is `migrate`, which is
  #       already after the oc.toml write — the stub records the port it was
  #       handed and dies there, so the run never reaches serve/pytest.
  #
  #       Reverting the default to a fixed literal turns this red two ways, and
  #       both are FAILs: normally the recorded port IS that literal; and if a
  #       real listener happens to hold it, run.sh's leftover guard exits first
  #       and NO port is recorded at all — a missing record is never a skip here.
  C16E_T="$(mktemp -d -t oc-guard-confdefault.XXXXXX)"
  C16E_SHIM="$(mktemp -d -t oc-guard-confdefshim.XXXXXX)"
  C16E_SEEN="$C16E_T/handed-port"
  mkdir -p "$C16E_T/conformance/.venv/bin" \
           "$C16E_T/server/ocserverd/seedsdist" \
           "$C16E_T/server/ocserverd/docsdist" \
           "$C16E_T/server/ocserverd/bindist"
  cp "$C16_CONF" "$C16E_T/conformance/run.sh"
  # The prod-port refusal list is parsed out of config.go (the SSOT) — give the
  # throwaway tree the REAL one so the gate behaves as it does in a checkout.
  cp "$HERE/../../server/ocserverd/config.go" "$C16E_T/server/ocserverd/config.go"
  : > "$C16E_T/server/ocserverd/seedsdist/stub.md"
  : > "$C16E_T/server/ocserverd/docsdist/stub.md"
  : > "$C16E_T/server/ocserverd/bindist/ocwarden"
  # Fake suite venv: satisfies both the `-x` test and the `import pytest, httpx`
  # probe, so run.sh neither creates a venv nor installs anything.
  printf '#!/usr/bin/env bash\nexit 0\n' > "$C16E_T/conformance/.venv/bin/python"
  chmod +x "$C16E_T/conformance/.venv/bin/python"
  # The stub "ocserverd": report the port our caller wrote into $OC_CONFIG for us
  # to bind, then fail so run.sh stops at its first use of us (migrate).
  cat > "$C16E_SHIM/ocserverd-stub" <<'SH'
#!/usr/bin/env bash
grep -Eo '^[[:space:]]*port[[:space:]]*=[[:space:]]*[0-9]+' "${OC_CONFIG:-/dev/null}" \
  | grep -oE '[0-9]+' | head -1 > "$C16E_SEEN_PATH"
exit 1
SH
  chmod +x "$C16E_SHIM/ocserverd-stub"
  # `go` shim: only `build -o <path> .` matters — emit the stub, never compile.
  cat > "$C16E_SHIM/go" <<'SH'
#!/usr/bin/env bash
if [[ "${1:-}" == "build" ]]; then
  out=""
  while [[ $# -gt 0 ]]; do
    if [[ "$1" == "-o" ]]; then out="${2:-}"; shift; fi
    shift
  done
  [[ -n "$out" ]] || exit 1
  cp "$C16E_STUB_PATH" "$out"
  exit 0
fi
exit 0
SH
  chmod +x "$C16E_SHIM/go"
  : > "$C16E_SEEN"
  # `env -u OC_CONF_PORT` — the whole point is the UNSET default, so strip it
  # even if this host's environment happens to carry one.
  C16E_OUT="$(env -u OC_CONF_PORT PATH="$C16E_SHIM:$PATH" \
                C16E_SEEN_PATH="$C16E_SEEN" C16E_STUB_PATH="$C16E_SHIM/ocserverd-stub" \
                bash "$C16E_T/conformance/run.sh" --target go 2>&1)"
  C16E_PORT="$(tr -d '[:space:]' < "$C16E_SEEN" 2>/dev/null || true)"
  case "$C16E_PORT" in
    0) ok "conformance/run.sh's DEFAULT (OC_CONF_PORT unset) hands the daemon port 0 — the kernel allocates at bind, so concurrent runs cannot contend (T-0e4b)" ;;
    "") bad "conformance/run.sh never got as far as handing the daemon a port with OC_CONF_PORT unset, so this case cannot vouch for the default — treat as red, not skipped (run said: ${C16E_OUT:-<silence>})" ;;
    *) bad "conformance/run.sh's DEFAULT handed the daemon FIXED port $C16E_PORT, not 0 — a hardcoded default serialises the suite: two concurrent runs contend for that one port and the second dies on the leftover guard (T-0e4b)" ;;
  esac
  rm -rf "$C16_T" "$C16_SHIM" "$C16E_T" "$C16E_SHIM" 2>/dev/null || true
fi

# ── 17) T-191d: teardown.sh's closing "prod — untouched" reassurance must NAME
#        the port prod is actually on, derived from the SSOT.
#        This is the MESSAGE-level form of the (15)/(16) defect: the line used to
#        read "prod :8770/:8766 — not managed by this harness (untouched)", which
#        named a RETIRED officraft default and a foreign product's port while the
#        live one went unmentioned. An operator reading it was told the real
#        station had been spared by a sentence that had never heard of it — a
#        reassurance pointing at the wrong port is worse than no reassurance.
#        BEHAVIOURAL: the real echo line is lifted VERBATIM from teardown.sh and
#        EVALUATED with the real lib/common.sh sourced, so what is asserted is
#        the string the operator actually sees. Evaluating one echo has no side
#        effects — none of teardown.sh's kill/rm steps are reached.
TD="$HERE/../teardown.sh"
if [[ ! -f "$TD" ]]; then
  bad "teardown.sh not found at $TD — update guard (17)"
else
  TD_LINE="$(grep -m1 -E '^echo "\[teardown\] prod ' "$TD" || true)"
  if [[ -z "$TD_LINE" ]]; then
    bad "teardown.sh no longer has an 'echo \"[teardown] prod …\"' line — update guard (17) (or the operator lost the reassurance entirely)"
  else
    C17_OUT="$(OC_E2E_PORT=8791 bash -c 'source "$1" >/dev/null 2>&1; '"$TD_LINE" _ "$C15_COMMON" 2>&1)"
    case "$C17_OUT" in
      *":$CANON_PORT"*|*" $CANON_PORT"*)
        ok "teardown.sh's 'prod untouched' line NAMES the current prod port ($CANON_PORT, SSOT-derived)" ;;
      *) bad "teardown.sh's 'prod untouched' line does NOT name the current prod port $CANON_PORT (got: ${C17_OUT:-<silence>}) — reassurance that names only retired ports tells the operator the live station was spared without ever mentioning it (T-191d)" ;;
    esac
    case "$C17_OUT" in
      *8770*) ok "teardown.sh's line still lists the RETIRED 8770 (added to, not swapped for, the current port)" ;;
      *) bad "teardown.sh's line dropped the retired 8770 — retired ports stay listed (got: $C17_OUT)" ;;
    esac
    # Discriminating control: the pre-fix literal, pushed through the identical
    # evaluation path, must NOT satisfy the assertion above — else (17) is vacuous.
    C17_CTL="$(OC_E2E_PORT=8791 bash -c 'source "$1" >/dev/null 2>&1; echo "[teardown] prod :8770/:8766 — not managed by this harness (untouched)"' _ "$C15_COMMON" 2>&1)"
    case "$C17_CTL" in
      *"$CANON_PORT"*) bad "control broken: the pre-fix literal line already contains $CANON_PORT — case (17) may be vacuous" ;;
      *) ok "control: the pre-fix literal line never mentions $CANON_PORT (case 17 can actually redden)" ;;
    esac
  fi
fi

# ── 18) T-2257: namespaced teardown must propagate OC_NAMESPACE to ocwarden ─
#
# `oc_resolve_instance` correctly derived a namespaced label/root, but the
# lifecycle helper then ran bare `ocwarden teardown`.  A child process without
# OC_NAMESPACE silently resolves the canonical label and token, so a harmless
# E2E cleanup could unload the live fleet's warden.  This invokes the REAL
# oc_teardown_bounded call chain, but every mutation is shimmed above.  The
# recording shims are tripwires: canonical launchd label, canonical root, or
# canonical token in any recorded target turns the test red.  The direct child
# env assertion is deliberately what makes removing propagation red, rather
# than merely checking the parent's OC_NS variable.
TEARDOWN_LOG="$SHIMDIR/.teardown-log"
: > "$TEARDOWN_LOG"
TEST_HOME="$SHIMDIR/ns-teardown-home"
export SHIM_ALLOW_TEARDOWN=1 SHIM_TEARDOWN_LOG="$TEARDOWN_LOG" TEST_HOME
rc="$(run_snippet '
  OC_E2E_NS="e2eproof"; OC_E2E_NS_PORT=8808
  oc_resolve_instance
  HOME_DIR="$HOME"; GUI="gui/501"; BACKUP_DIR="$HOME/backups"
  # SHIMDIR itself is intentionally not exported to the hermetic child; resolve
  # the fake through the exported PATH exactly as the harness resolves tools.
  OCWARDEN="$(command -v ocwarden)"
  mkdir -p "$HOME/.officraft/warden" "$OC_ROOT/warden"
  printf canonical > "$HOME/.officraft/warden/exec-warden.tok"
  printf isolated > "$OC_ROOT/warden/exec-warden.tok"
  oc_teardown_bounded "namespace-regression"
')"
check "namespaced teardown helper completes through hermetic shims" "0" "$rc"

if grep -Fqx 'ocwarden namespace=e2eproof args=teardown' "$TEARDOWN_LOG"; then
  ok "namespaced teardown passes OC_NAMESPACE=e2eproof to the ocwarden child"
else
  bad "namespaced teardown did NOT pass its namespace to ocwarden (log: $(tr '\n' '|' < "$TEARDOWN_LOG")) — bare teardown falls back to canonical warden"
fi

if grep -Eq 'com\.officraft\.ocwarden([[:space:]]|$)' "$TEARDOWN_LOG"; then
  bad "namespaced teardown touched canonical warden label (log: $(tr '\n' '|' < "$TEARDOWN_LOG"))"
else
  ok "namespaced teardown never targets canonical warden label"
fi
if grep -Fq "$TEST_HOME/.officraft/warden/exec-warden.tok" "$TEARDOWN_LOG"; then
  bad "namespaced teardown touched canonical warden token (log: $(tr '\n' '|' < "$TEARDOWN_LOG"))"
else
  ok "namespaced teardown never targets canonical warden token"
fi
if grep -Fq "$TEST_HOME/.officraft/" "$TEARDOWN_LOG"; then
  bad "namespaced teardown touched canonical officraft root (log: $(tr '\n' '|' < "$TEARDOWN_LOG"))"
else
  ok "namespaced teardown never targets canonical officraft root"
fi
# This one does NOT test the recording shims — it catches a SHIM BYPASS: code
# that deletes through an absolute /bin/rm (or any path that dodges $PATH) never
# reaches the recorder above, so the three log tripwires would stay silent while
# the file really vanished. The sentinel is the only assertion that survives
# that class of escape. It is NOT the tripwire for MUT-D — (18c) is.
if [[ -f "$TEST_HOME/.officraft/warden/exec-warden.tok" ]] \
   && [[ "$(cat "$TEST_HOME/.officraft/warden/exec-warden.tok")" == "canonical" ]]; then
  ok "canonical token sentinel remains intact after namespaced teardown (no shim bypass)"
else
  bad "canonical token sentinel was changed or removed by namespaced teardown"
fi

# ── 18c) PERMANENT NEGATIVE CONTROL for (18): the tripwires must actually fire ─
#
# The tripwires above are grep-for-absence assertions: they pass when the log is
# EMPTY, which is also what a broken recorder produces. That is not a theory —
# the shim's redirection was wrong on arrival (only the last of three printfs was
# redirected, so every entry was a bare 0x0a) and all three tripwires plus the
# sentinel were structurally incapable of failing. The suite reported 56/56 while
# testing nothing.
#
# So: replay the literal 2026-07-25 incident. A mutated copy of the lib gets the
# two incident deletions injected into oc_teardown_bounded —
#   rm -f  "$HOME_DIR/.officraft/warden/exec-warden.tok"
#   rm -rf "$HOME_DIR/.officraft/warden"
# — and the SAME tripwire greps are then required to MATCH. If (18)'s recorder
# ever regresses, this case reddens immediately.
# The mutant lives in a MIRROR TREE (e2e_test/lib/ under a scratch root whose
# server/ is symlinked to the real one) because the lib derives its repo root
# from BASH_SOURCE and FATALs when it cannot parse config.go's defaultPort.
MUTROOT="$SHIMDIR/mutd-tree"
mkdir -p "$MUTROOT/e2e_test/lib"
ln -sfn "$HERE/../../server" "$MUTROOT/server"
MUTLIB="$MUTROOT/e2e_test/lib/oc_lifecycle.sh"
awk '
  /^oc_teardown_bounded\(\)/ { inbounded = 1 }
  { print }
  inbounded && !injected && /^  oc_assert_teardown_instance$/ {
    print "  rm -f \"$HOME_DIR/.officraft/warden/exec-warden.tok\""
    print "  rm -rf \"$HOME_DIR/.officraft/warden\""
    injected = 1
  }
  END { if (!injected) exit 3 }
' "$LIB" > "$MUTLIB"
if [[ $? -ne 0 ]]; then
  bad "could not build the MUT-D mutant: the 'oc_assert_teardown_instance' anchor inside oc_teardown_bounded moved — update guard (18c)"
else
  MUT_LOG="$SHIMDIR/.teardown-log-mutd"
  : > "$MUT_LOG"
  MUT_HOME="$SHIMDIR/mutd-home"
  rc="$(SNIPPET_LIB="$MUTLIB" SHIM_TEARDOWN_LOG="$MUT_LOG" TEST_HOME="$MUT_HOME" run_snippet '
    OC_E2E_NS="e2eproof"; OC_E2E_NS_PORT=8808
    oc_resolve_instance
    HOME_DIR="$HOME"; GUI="gui/501"; BACKUP_DIR="$HOME/backups"
    OCWARDEN="$(command -v ocwarden)"
    mkdir -p "$HOME/.officraft/warden" "$OC_ROOT/warden" "$HOME/backups"
    printf canonical > "$HOME/.officraft/warden/exec-warden.tok"
    oc_teardown_bounded "mutd-negative-control"
  ')"
  check "MUT-D control: the mutated teardown still completes (mutation is reachable)" "0" "$rc"
  [[ "$rc" == "0" ]] || { echo "  ---- MUT-D control GLOG ----"; cat "$GLOG"; }
  if grep -Fq "$MUT_HOME/.officraft/warden/exec-warden.tok" "$MUT_LOG"; then
    ok "MUT-D control: the canonical-token tripwire FIRES on the 2026-07-25 incident (grep is not vacuous)"
  else
    bad "MUT-D control: the canonical-token tripwire stayed SILENT while the incident deletion ran (log: $(tr '\n' '|' < "$MUT_LOG")) — the rm recorder is broken again and case (18) is testing nothing"
  fi
  if grep -Fq "$MUT_HOME/.officraft/" "$MUT_LOG"; then
    ok "MUT-D control: the canonical-root tripwire FIRES on the 2026-07-25 incident"
  else
    bad "MUT-D control: the canonical-root tripwire stayed SILENT while 'rm -rf ~/.officraft/warden' ran (log: $(tr '\n' '|' < "$MUT_LOG")) — case (18) is vacuous"
  fi
fi

# ── 18d/18e) oc_assert_teardown_instance must actually GATE both call sites ────
#
# The guard shipped with no failing-without-it coverage: replacing BOTH of its
# call sites with `:` left the suite fully green, so the one thing standing
# between a stale variable and the canonical warden was untested. These two cases
# drive a MIXED axis set (namespace selected, but WARDEN_LABEL/OC_ROOT still
# canonical — exactly the shape the 2026-07-25 incident had) through each entry
# point separately, and require it to DIE before any mutation. Asserted per call
# site on purpose: one combined case would stay green while either site lost its
# guard.
c18_mixed() { # c18_mixed NAME ENTRYPOINT
  local log="$SHIMDIR/.teardown-log-$1" entry="$2"
  : > "$log"
  SHIM_TEARDOWN_LOG="$log" TEST_HOME="$SHIMDIR/mixed-home-$1" run_snippet '
    OC_E2E_NS="e2eproof"; OC_E2E_NS_PORT=8808
    oc_resolve_instance
    HOME_DIR="$HOME"; GUI="gui/501"; BACKUP_DIR="$HOME/backups"
    OCWARDEN="$(command -v ocwarden)"
    mkdir -p "$HOME/backups" "$OC_ROOT/warden"
    # STALE canonical axes left behind by a partial/aborted resolve.
    WARDEN_LABEL="com.officraft.ocwarden"
    OC_ROOT="$HOME/.officraft"
    '"$entry"
}
for _entry in 'oc_teardown_bounded "mixed-axes"' 'oc_teardown_warden'; do
  _name="${_entry%% *}"
  rc="$(c18_mixed "$_name" "$_entry")"
  if [[ "$rc" != "0" ]]; then
    ok "$_name DIES on a namespaced run whose WARDEN_LABEL/OC_ROOT are still canonical (rc=$rc)"
  else
    bad "$_name ACCEPTED a namespaced run with canonical WARDEN_LABEL/OC_ROOT — the teardown target guard is absent or bypassed at this call site; this is the exact 2026-07-25 shape"
  fi
  grep -q 'TEARDOWN TARGET GUARD' "$GLOG" \
    && ok "$_name refusal names TEARDOWN TARGET GUARD" \
    || bad "$_name died without the TEARDOWN TARGET GUARD marker (got: $(tr '\n' '|' < "$GLOG" | tail -c 300))"
  # BEFORE ANY MUTATION, asserted as "the recorder saw NOTHING". Not just "no
  # canonical label": oc_teardown_bounded's own call site is what makes the
  # refusal precede the .dump backup and the serve/autodeploy/tunnel bootouts.
  # If only the nested oc_teardown_warden guard survives, the run still dies —
  # but only AFTER four bootouts, and only this assertion notices.
  _mut="$(grep -cE 'launchctl bootout|^rm <' "$SHIMDIR/.teardown-log-$_name" 2>/dev/null || true)"
  if [[ "${_mut:-0}" != "0" ]]; then
    bad "$_name mutated $_mut host resource(s) BEFORE refusing — the guard must run before the backup/bootout/delete sequence (log: $(tr '\n' '|' < "$SHIMDIR/.teardown-log-$_name"))"
  else
    ok "$_name refused before booting out or deleting anything"
  fi
done
unset SHIM_ALLOW_TEARDOWN SHIM_TEARDOWN_LOG TEST_HOME

# ── 19) T-e1dd: cross_machine's preflight — prod-host guard + gate ORDERING ───
#
# cross_machine.sh acked destructiveness before STAGE 1 and isolation before
# STAGE 3, with `rm -rf "$SERVER_ROOT"` in between — so the invocation printed in
# its own header deleted the server root and was refused 141 lines later. Nothing
# could catch it: the gates and the deletion were top-level code in a destructive
# script, so the only way to exercise them was to run it for real.
#
# Everything below runs against oc_cross_machine_preflight as a FUNCTION, on a
# throwaway $HOME (TEST_HOME) whose contents this file creates, with the recording
# shims installed. This file makes ZERO direct calls to rm/launchctl/tmux against
# any real resource — that is a stated requirement of the ticket, because the
# mutants below deliberately disable the guard being tested and a test that leaned
# on that guard for its own safety would destroy the host at exactly that moment.
E1DD_LOG="$SHIMDIR/.teardown-log-e1dd"
export SHIM_ALLOW_TEARDOWN=1 SHIM_TEARDOWN_LOG="$E1DD_LOG"

# The known production hardware UUID, read from the lib rather than duplicated —
# a second copy of this constant would be a drift site of exactly the kind the
# port literal already taught us about (T-b76b).
PROD_UUID="$(run_snippet 'printf "UUID=%s\n" "${OC_PROD_HOST_HW_UUIDS[0]}"' >/dev/null; grep '^UUID=' "$GLOG" | cut -d= -f2)"
[[ -n "$PROD_UUID" ]] || bad "could not read OC_PROD_HOST_HW_UUIDS from the lib — the prod-host identity guard has no pinned station"
DISPOSABLE_UUID="11111111-2222-3333-4444-555555555555"

# e1dd_home KIND — build a throwaway $HOME. KIND: clean | residue
e1dd_home() {
  local kind="$1" h="$SHIMDIR/e1dd-home-$1"
  rm -rf "$h" 2>/dev/null || true          # shim rm: recorded, never touches the host
  mkdir -p "$h/Library/LaunchAgents" "$h/backups" "$h/bin"
  printf '#!/bin/sh\nexit 0\n' > "$h/bin/ocserver"; chmod +x "$h/bin/ocserver"
  [[ "$kind" == "residue" ]] && mkdir -p "$h/.officraft/server/data"
  printf '%s' "$h"
}

# e1dd_pre HOME BODY — run BODY with the preflight's required globals wired to a
# throwaway home. Both acks and a disposable machine identity default ON, so each
# case turns exactly ONE thing off.
e1dd_pre() {
  local home="$1" body="$2"
  : > "$E1DD_LOG"
  # `-` not `:-` on the UUID vars: an EMPTY value is a case in its own right (the
  # "cannot read a hardware UUID, identity check has gone dark" path), and `:-`
  # would silently substitute the default and make that case inexpressible.
  TEST_HOME="$home" OC_CROSS_MACHINE_YES="${E1DD_YES:-1}" \
  REQUIRE_ISOLATION_CONFIRMED="${E1DD_ISO:-1}" SHIM_HW_UUID="${E1DD_UUID-$DISPOSABLE_UUID}" \
  SHIM_REMOTE_HW="${E1DD_REMOTE_HW-$DISPOSABLE_UUID}" \
  SHIM_REMOTE_SERVER_TREE="${E1DD_REMOTE_TREE:-0}" SHIM_SSH_FAIL="${E1DD_SSH_FAIL:-0}" \
  SHIM_SSH_SILENT="${E1DD_SSH_SILENT:-0}" SHIM_REMOTE_WARDEN="${E1DD_REMOTE_WARDEN:-0}" \
  SHIM_REMOTE_AGENTS="${E1DD_REMOTE_AGENTS:-0}" SHIM_SSH_NOISE="${E1DD_SSH_NOISE:-}" \
  SHIM_REMOTE_TOOLS="${E1DD_REMOTE_TOOLS:-all}" \
  SHIM_REMOTE_HOME="$SHIMDIR/remote-home-$$-${E1DD_REMOTE_TREE:-0}" \
  SHIM_WARDEN=0 SHIM_LISTEN_PORTS="" SHIM_SESSIONS="" \
  OC_CLAUDE_BIN="$home/bin/ocserver" run_snippet '
    SECOND_MACHINE="a-disposable-relocate-target"
    HOME_DIR="$HOME"; GUI="gui/501"; BACKUP_DIR="$HOME/backups"
    OC_ROOT="$HOME/.officraft"; SERVER_ROOT="${OC_SERVER_ROOT:-$OC_ROOT/server}"
    DB_PATH="$SERVER_ROOT/data/officraft.db"; OCSERVER="$HOME/bin/ocserver"
    OCWARDEN="$(command -v ocwarden)"; TMUX_SOCKET_LOCAL="$OC_CANONICAL_TMUX_SOCKET"
    SERVE_LABEL="com.officraft.serve"; AUTODEPLOY_LABEL="com.officraft.autodeploy"
    TUNNEL_LABEL="com.officraft.tunnel"; WARDEN_LABEL="com.officraft.ocwarden"
    '"$body"
}

H_CLEAN="$(e1dd_home clean)"; H_RESIDUE="$(e1dd_home residue)"

# 19a) DETECTION — the two questions, asked separately.
rc="$(TEST_HOME="$H_CLEAN" SHIM_HW_UUID="$PROD_UUID" \
      run_snippet 'oc_detect_prod_host | grep -q "^identity:"')"
check "identity guard fires on a known production hardware UUID (even with a clean disk)" "0" "$rc"
rc="$(TEST_HOME="$H_RESIDUE" SHIM_HW_UUID="$DISPOSABLE_UUID" \
      run_snippet 'oc_detect_prod_host | grep -q "^residue:"')"
check "residue guard fires on an existing server tree (even on an unknown machine)" "0" "$rc"
rc="$(TEST_HOME="$H_CLEAN" SHIM_HW_UUID="$DISPOSABLE_UUID" \
      run_snippet 'out="$(oc_detect_prod_host)"; [[ -z "$out" ]]')"
check "detection is EMPTY on a disposable machine with no server tree" "0" "$rc"

# The case the pre-T-e1dd shape could not see, and the reason the identity guard
# exists at all: a production station whose server is STOPPED and whose disk is
# still bare (freshly provisioned) — every liveness signal silent.
rc="$(TEST_HOME="$H_CLEAN" SHIM_HW_UUID="$PROD_UUID" SHIM_WARDEN=0 SHIM_LISTEN_PORTS="" SHIM_SESSIONS="" \
      run_snippet 'OC_NS=""; oc_live_fleet_guard && oc_detect_prod_host | grep -q "^identity:"')"
check "a production station with NOTHING running is still recognised as production" "0" "$rc"

# 19a') ADDRESSING — the guard must not be steerable by OC_SERVER_ROOT. That env
# is what the teardown deletes; if the detector derived its paths from it, the
# override would aim the guard at an empty directory while the deletion still
# landed on the real tree. This is the difference between a guard and a flag.
rc="$(TEST_HOME="$H_RESIDUE" OC_SERVER_ROOT="$SHIMDIR/decoy" SHIM_HW_UUID="$DISPOSABLE_UUID" \
      run_snippet 'SERVER_ROOT="$OC_SERVER_ROOT"; oc_detect_prod_host | grep -q "^residue:"')"
check "OC_SERVER_ROOT cannot steer the prod-host guard away from the real tree" "0" "$rc"

# 19b) THE GATE — refusals happen, and they happen BEFORE any mutation. Each case
#      runs the REAL preflight → teardown chain; the recorder proves how far
#      execution actually got.
e1dd_gate() { # e1dd_gate DESC HOME MARKER
  local desc="$1" home="$2" marker="$3"
  local rc; rc="$(e1dd_pre "$home" 'oc_cross_machine_preflight
    oc_teardown_bounded "e1dd-should-not-reach"')"
  [[ "$rc" != "0" ]] && ok "$desc → refuses (rc=$rc)" || bad "$desc → should refuse, returned 0"
  grep -q "$marker" "$GLOG" && ok "$desc → refusal names $marker" \
    || bad "$desc → refusal lacks $marker (got: $(tr '\n' '|' < "$GLOG" | tail -c 300))"
  local mut; mut="$(grep -cE 'launchctl bootout|^rm <' "$E1DD_LOG" 2>/dev/null || true)"
  [[ "${mut:-0}" == "0" ]] && ok "$desc → nothing was booted out or deleted first" \
    || bad "$desc → mutated ${mut} resource(s) BEFORE refusing (log: $(tr '\n' '|' < "$E1DD_LOG"))"
}

E1DD_ISO=0 e1dd_gate "missing isolation ack" "$H_CLEAN" "REQUIRE_ISOLATION_CONFIRMED"
E1DD_YES=0 e1dd_gate "missing destructiveness ack" "$H_CLEAN" "DESTRUCTIVE"
E1DD_UUID="$PROD_UUID" e1dd_gate "a known production station" "$H_CLEAN" "PROD-HOST GUARD (identity)"
e1dd_gate "a host carrying a server tree" "$H_RESIDUE" "PROD-HOST GUARD (residue)"

# Both acks set is the maximum an operator can assert. It must not be enough on a
# production station — otherwise the guard is advisory and this ticket's failure
# mode is one env var away from coming back.
rc="$(E1DD_UUID="$PROD_UUID" e1dd_pre "$H_CLEAN" 'oc_cross_machine_preflight')"
[[ "$rc" != "0" ]] && ok "both acks set still cannot run on a known production station (rc=$rc)" \
  || bad "both acks set BYPASSED the identity guard — the guard is only advisory"

# 19b') WHAT EACH REFUSAL MAY SAY. These two are opposites on purpose:
#   • the RESIDUE refusal MUST offer a way forward — its most likely reader is
#     someone re-running on their own throwaway VM, and a refusal that only says
#     "no" reads as a broken tool, which is how workarounds get invented.
#   • the IDENTITY refusal must NEVER name a way to clear the obstacle. Its
#     reader is standing on a production station; "delete this and retry" IS the
#     disaster this whole ticket is about.
e1dd_pre "$H_RESIDUE" 'oc_cross_machine_preflight' >/dev/null
if grep -Eq 'PROD-HOST GUARD \(residue\).*(rebuild|delete)' "$GLOG"; then
  ok "the residue refusal tells the operator how to proceed"
else
  bad "the residue refusal gives no way forward — it will read as a broken tool (got: $(tr '\n' '|' < "$GLOG" | tail -c 300))"
fi
E1DD_UUID="$PROD_UUID" e1dd_pre "$H_CLEAN" 'oc_cross_machine_preflight' >/dev/null
# The pattern hunts for an INSTRUCTION TO THE READER, not for the word "delete":
# the message is allowed — required, even — to say what the suite deletes. What it
# must never do is tell the person standing on that station what to remove or
# which flag to set in order to continue.
if grep -Eqi 'PROD-HOST GUARD \(identity\).*(rm -rf|delete (\$?HOME|~|the tree|it) |set [A-Z_]+=1|bypass|--force|override this|skip this)' "$GLOG"; then
  bad "the identity refusal tells someone standing on a production station how to clear the obstacle: $(grep -i 'identity' "$GLOG" | tail -c 300)"
else
  ok "the identity refusal offers no way to clear it — only 'run somewhere else'"
fi
# The REMOTE guard has its own three messages and the same rule applies, per
# branch. This case is deliberately the ambiguous one — a known station that is
# ALSO running its warden — because the danger is branch ORDER: if liveness were
# checked first, a machine known by hardware UUID to be production would be handed
# the liveness message's "retire it yourself" remedy.
E1DD_REMOTE_HW="$PROD_UUID" E1DD_REMOTE_WARDEN=1 \
  e1dd_pre "$H_CLEAN" 'oc_cross_machine_preflight' >/dev/null
if grep -q 'PROD-HOST GUARD (remote): the second machine .* is a known production' "$GLOG"; then
  ok "a production SECOND_MACHINE that is also live gets the IDENTITY refusal, not the liveness one"
else
  bad "the liveness branch preempted identity on a known production station — that message tells its reader how to clear the obstacle (got: $(tr '\n' '|' < "$GLOG" | tail -c 300))"
fi
if grep -Eqi 'is a known production officraft station.*(retire it yourself|launchctl bootout|clear ~/\.officraft)' "$GLOG"; then
  bad "the remote identity refusal names a way to clear the obstacle on a production station"
else
  ok "the remote identity refusal offers no way to clear it either"
fi

# 19b'') THE SECOND MACHINE gets three questions of its own. STAGE 5b deletes its
# ENTIRE ~/.officraft — more than this suite deletes locally — so guarding only
# the local host leaves the cheaper mistake available: from a genuinely clean
# throwaway VM, naming a production station as SECOND_MACHINE passes every local
# gate. The refusal must also happen HERE, in the preflight, not at STAGE 5b,
# which is after the local host has been torn down and reinstalled.
E1DD_REMOTE_HW="$PROD_UUID" e1dd_gate "a production SECOND_MACHINE" "$H_CLEAN" "PROD-HOST GUARD (remote)"
E1DD_REMOTE_TREE=1 e1dd_gate "a SECOND_MACHINE carrying a server tree" "$H_CLEAN" "PROD-HOST GUARD (remote)"

E1DD_REMOTE_WARDEN=1 e1dd_gate "a SECOND_MACHINE running a warden" "$H_CLEAN" "live fleet node"
# Agents outlive their warden (booted out for maintenance, crashed, launchd gave
# up, started by hand). STAGE 5b kill-sessions them explicitly, so warden
# registration alone is a guard that looks complete and is not.
E1DD_REMOTE_AGENTS=1 e1dd_gate "a SECOND_MACHINE with live agent sessions but no warden" "$H_CLEAN" "agents are running there right now"

# MARKER DILUTION. A remote rc file that prints a marker-shaped line lands BEFORE
# the probe's own output, so "take the first/any match" would read the wrong
# answer. Every marker is counted, and more than one answer to a question means
# the probe cannot be trusted — including for `hw=`, where a wrong-but-non-empty
# value would ALSO suppress the go-dark warning.
E1DD_REMOTE_HW="$PROD_UUID" E1DD_SSH_NOISE="hw=not-a-real-uuid" \
  e1dd_gate "a probe diluted by a stray hw= line" "$H_CLEAN" "did not come back with exactly one answer"
E1DD_REMOTE_TREE=1 E1DD_SSH_NOISE="server_tree=0" \
  e1dd_gate "a probe diluted by a stray server_tree= line" "$H_CLEAN" "did not come back with exactly one answer"

# …but ordinary ssh chatter is NOT marker-shaped and must be tolerated. A guard
# that refused every host emitting a known-hosts warning would be turned off
# within a day, which is a slower way of having no guard.
E1DD_SSH_NOISE="Warning: Permanently added 'tgt' (ED25519) to the list of known hosts." \
  e1dd_pre "$H_CLEAN" 'oc_cross_machine_preflight' >/dev/null
grep -q 'prod-host guard OK (remote)' "$GLOG" \
  && ok "ordinary ssh chatter does not trip the marker parse" \
  || bad "a known-hosts warning made the remote probe unparseable — every real host emits that (got: $(tr '\n' '|' < "$GLOG" | tail -c 200))"

# Unreachable second machine → fail CLOSED. A host whose identity cannot be
# established is not thereby safe to wipe; "ssh failed, carry on" would be the
# same shape as the bug this ticket fixes.
E1DD_SSH_FAIL=1 e1dd_gate "an unreachable SECOND_MACHINE" "$H_CLEAN" "could not establish what machine"

# …and the quieter version of the same thing: ssh exits 0 having run nothing
# (ForceCommand, restricted shell, an rc file that returns early). An empty probe
# is NOT a clean host. This is the failure mode the first version of this guard
# got wrong, and it is the one that looks exactly like success.
E1DD_SSH_SILENT=1 e1dd_gate "a SECOND_MACHINE whose probe returned nothing" "$H_CLEAN" "could not establish what machine"

# The ssh failure message must carry ssh's own diagnosis — "fix ssh access" is
# useless standing next to "Permission denied (publickey)".
E1DD_SSH_FAIL=1 e1dd_pre "$H_CLEAN" 'oc_cross_machine_preflight' >/dev/null
grep -q 'ssh said:.*Operation timed out' "$GLOG" \
  && ok "the unreachable-host refusal repeats what ssh actually said" \
  || bad "the unreachable-host refusal swallowed ssh's diagnosis (got: $(tr '\n' '|' < "$GLOG" | tail -c 200))"

# GO-DARK WARNINGS. A check that cannot run must SAY so; the danger is the "guard
# OK" line continuing to claim "not a known production station" when the identity
# question was never actually asked.
E1DD_UUID="" e1dd_pre "$H_CLEAN" 'oc_cross_machine_preflight' >/dev/null
grep -q 'IDENTITY check is INACTIVE' "$GLOG" \
  && ok "an unreadable local hardware UUID is announced, not silently skipped" \
  || bad "the local identity check went dark silently (got: $(tr '\n' '|' < "$GLOG" | tail -c 200))"
E1DD_REMOTE_HW="" e1dd_pre "$H_CLEAN" 'oc_cross_machine_preflight' >/dev/null
grep -q 'remote IDENTITY check is INACTIVE' "$GLOG" \
  && ok "an unreadable remote hardware UUID is announced, not silently skipped" \
  || bad "the remote identity check went dark silently (got: $(tr '\n' '|' < "$GLOG" | tail -c 200))"

# The probe must read the REMOTE machine's identity, not this one's. A guard that
# looked at the local UUID would pass every case above and still be looking at the
# wrong machine — which is the entire bug this remote check was added for.
E1DD_UUID="$PROD_UUID" E1DD_REMOTE_HW="$DISPOSABLE_UUID" \
  e1dd_pre "$H_CLEAN" 'oc_prod_host_remote_guard "$SECOND_MACHINE"' >/dev/null
grep -q 'prod-host guard OK (remote)' "$GLOG" \
  && ok "the remote guard reads the REMOTE uuid (a production LOCAL uuid does not trip it)" \
  || bad "the remote guard tripped on the LOCAL machine's identity — it is probing the wrong host"

# A QUESTION THE FAR SIDE CANNOT ANSWER IS NOT A "NO". An ssh non-login shell has
# no Homebrew bin dir, so `tmux` is not found there — this script's own gotcha #2,
# and the reason every other remote command goes through the PATH-exporting
# wrapper. A not-found tool produces no output, which for a liveness check reads
# as "nothing running": fail-OPEN, on the default second machine.
E1DD_REMOTE_TOOLS=notmux E1DD_REMOTE_AGENTS=1 \
  e1dd_gate "a SECOND_MACHINE where the probe's tools are not on PATH" "$H_CLEAN" "did not come back with exactly one answer"
# …and pin WHICH question went unanswered — ALL FOUR COUNTS, not just the one
# that is meant to be zero. Matching `live_agents: 0` alone still passed when the
# probe answered NOTHING AT ALL (0,0,0,0): a shim that fell silent took the whole
# case with it and every assertion stayed green, because "refused" and "refused
# for the intended reason" are not the same claim. Requiring the other three to
# be 1 says the far side was alive and answering, and exactly one question could
# not be answered — which is the only state this case is about.
E1DD_REMOTE_TOOLS=notmux E1DD_REMOTE_AGENTS=1 \
  e1dd_pre "$H_CLEAN" 'oc_cross_machine_preflight' >/dev/null
if grep -q 'hw: 1, server_tree: 1, live_warden: 1, live_agents: 0' "$GLOG"; then
  ok "the notmux refusal is the LIVENESS question going unanswered, with the other three answered"
else
  bad "the notmux case refused for the wrong reason — either tmux was answerable after all, or the probe answered nothing at all (expected 'hw: 1, server_tree: 1, live_warden: 1, live_agents: 0'; got: $(tr '\n' '|' < "$GLOG" | tail -c 300))"
fi

# BRANCH ORDER, second half. Only the liveness message names a remedy, so it must
# come LAST — a host with BOTH a server tree and a running warden is more
# incriminating than one with either alone, and must not be handed the more
# permissive message. This is the shape of an UNLISTED production server, which is
# exactly the case residue exists to catch.
E1DD_REMOTE_TREE=1 E1DD_REMOTE_WARDEN=1 \
  e1dd_pre "$H_CLEAN" 'oc_cross_machine_preflight' >/dev/null
if grep -q 'PROD-HOST GUARD (remote): the second machine .* carries an officraft server tree' "$GLOG"; then
  ok "a SECOND_MACHINE with BOTH a server tree and a live warden gets the residue refusal, not the remedy-bearing one"
else
  bad "the liveness branch preempted residue — the more incriminating host got the more permissive message (got: $(tr '\n' '|' < "$GLOG" | tail -c 300))"
fi

# 19c) SENTINEL — a genuinely disposable host must still be able to run. A guard
# that refuses everything passes every refusal test above and is useless.
rc="$(e1dd_pre "$H_CLEAN" 'oc_cross_machine_preflight')"
check "a disposable host with both acks PASSES the preflight" "0" "$rc"

# 19c') THE ORDERING IS A PROPERTY OF cross_machine.sh, and nothing above pins it
# there. Every assertion so far runs a preflight→teardown chain THIS FILE builds,
# so moving the preflight call BELOW the teardown call in cross_machine.sh would
# leave the whole section green — reinstating exactly this ticket's bug. Line
# numbers are weak evidence in general, but this is straight-line top-level script
# code, where source order IS execution order.
CM="$HERE/../cross_machine.sh"
_pre_ln="$(grep -n '^oc_cross_machine_preflight$' "$CM" | head -1 | cut -d: -f1)"
_td_ln="$(grep -n '^oc_teardown_bounded ' "$CM" | head -1 | cut -d: -f1)"
if [[ -z "$_pre_ln" || -z "$_td_ln" ]]; then
  bad "cross_machine.sh no longer has a top-level oc_cross_machine_preflight and/or oc_teardown_bounded call (pre=${_pre_ln:-none} td=${_td_ln:-none}) — this ordering pin has gone blind"
elif [[ "$_pre_ln" -lt "$_td_ln" ]]; then
  ok "cross_machine.sh calls the preflight (line $_pre_ln) BEFORE the teardown (line $_td_ln)"
else
  bad "cross_machine.sh calls the teardown (line $_td_ln) before the preflight (line $_pre_ln) — this is the T-e1dd bug"
fi
# …and no destructive top-level statement may sit ahead of the preflight call.
_e1dd_early_scan() { # _e1dd_early_scan FILE STOP_LINE
  awk -v stop="$2" 'NR<stop && $0 !~ /^[[:space:]]*#/ && /(^|[^-[:alnum:]_])(rm -rf|rm -f|launchctl bootout|kill-session|kill-server|pkill|killall|oc_teardown_)/' "$1" \
    | wc -l | tr -d ' '
}
_early="$(_e1dd_early_scan "$CM" "$_pre_ln")"
check "no destructive statement in cross_machine.sh runs before the preflight" "0" "${_early:-0}"
# CONTROL for the scan above. It is a grep-for-absence: a pattern broken by a
# future edit produces 0 hits forever and the assertion is permanently, silently
# green — indistinguishable from a clean file. Run the SAME function over a
# fixture that is deliberately dirty.
_fixture="$SHIMDIR/early-scan-fixture.sh"
{ printf '# rm -rf /commented-out — must not count\n'
  printf 'rm -rf /x\n'
  printf 'launchctl bootout gui/501/com.officraft.serve\n'
  printf 'oc_teardown_bounded "hoisted"\n'
  printf 'oc_cross_machine_preflight\n'; } > "$_fixture"
check "early-scan control: the scan reddens on a fixture with 3 destructive statements (and skips the comment)" \
  "3" "$(_e1dd_early_scan "$_fixture" 5)"

# 19d) MUTANTS — one edit each. Without them every assertion above could be
# vacuously true. The mutant lib needs a tree whose ../../server resolves, because
# the lib derives the canonical port from server/ocserverd/config.go and FATALs if
# it cannot (same construction as the MUT-D control above).
# e1dd_mutant NAME SED_EXPR HOME UUID [ISO_ACK]
#
# The ack defaults are passed EXPLICITLY, not inherited from ambient state. An
# earlier version relied on a `E1DD_ISO=0` prefix on the CALL reaching e1dd_pre
# through the function body — it does not, so the ack mutant ran fully acked and
# asserted rc==0 for a configuration that already returns rc==0 unmutated. It was
# green, and it would have stayed green with the ack check deleted outright: a
# mutant that proves nothing is worse than no mutant, because it reads as proof.
e1dd_mutant() {
  local name="$1" expr="$2" home="$3" uuid="$4" iso="${5:-1}" remote_hw="${6:-$DISPOSABLE_UUID}"
  local root="$SHIMDIR/mut-$name-tree" lib="$SHIMDIR/mut-$name-tree/e2e_test/lib/oc_lifecycle.sh"
  mkdir -p "$root/e2e_test/lib"; ln -sfn "$HERE/../../server" "$root/server"
  sed "$expr" "$LIB" > "$lib"
  if cmp -s "$lib" "$LIB"; then
    bad "MUT-$name did not change the lib — the mutation anchor moved; a vacuous mutant proves nothing"
    return
  fi
  # CONTROL: the same configuration against the UNMUTATED lib must REFUSE. Without
  # this, "the mutant proceeds" is not evidence — it is also what a configuration
  # that was always going to proceed looks like.
  local ctl; ctl="$(E1DD_ISO="$iso" E1DD_UUID="$uuid" E1DD_REMOTE_HW="$remote_hw" e1dd_pre "$home" 'oc_cross_machine_preflight')"
  if [[ "$ctl" == "0" ]]; then
    bad "MUT-$name control: the UNMUTATED lib already accepted this configuration — the mutant below proves nothing about that check"
    return
  fi
  local rc; rc="$(SNIPPET_LIB="$lib" E1DD_ISO="$iso" E1DD_UUID="$uuid" E1DD_REMOTE_HW="$remote_hw" e1dd_pre "$home" 'oc_cross_machine_preflight')"
  [[ "$rc" == "0" ]] \
    && ok "MUT-$name: unmutated refuses (rc=$ctl), with that check removed the run PROCEEDS — the live case is pinned to it" \
    || bad "MUT-$name: still refused (rc=$rc) — the live assertion is NOT pinned to this check (glog: $(tr '\n' '|' < "$GLOG" | tail -c 200))"
}
# 1. identity: drop the hardware-UUID comparison → the production-station case must go green-lit.
# Replaced with a no-op rather than deleted: that line is the entire body of the
# `for u in ...` loop, and deleting it leaves an empty loop body — a syntax error,
# which makes the lib fail to load and the mutant "refuse" for the wrong reason.
e1dd_mutant identity 's/.*identity: this machine.*/      : ;/' "$H_CLEAN" "$PROD_UUID"
# 2. residue: drop the server-tree check → the residue case must go green-lit.
e1dd_mutant residue 's|^  \[\[ -d "$server_root" \]\] .*$|  : ;|' "$H_RESIDUE" "$DISPOSABLE_UUID"
# 3. the isolation ack — run UNACKED (iso=0), so the control refuses and the
#    mutant proceeds. This is the check whose ORDER is the whole ticket.
e1dd_mutant ack '/^  \[\[ "${REQUIRE_ISOLATION_CONFIRMED:-0}" == "1" \]\] || die \\$/,+1d' "$H_CLEAN" "$DISPOSABLE_UUID" 0
# 4. the remote prod-host guard — a production SECOND_MACHINE must be refused.
e1dd_mutant remote 's/^  oc_prod_host_remote_guard .*$/  : ;/' "$H_CLEAN" "$DISPOSABLE_UUID" 1 "$PROD_UUID"

# 19e) This test file must not be able to destroy anything itself — the ticket's
# hardest constraint, because the mutants above deliberately disable the guard
# under test. Counted, not eyeballed: an absolute path or a kill-by-name dodges
# the PATH shim and would reach the real host. Matches in THIS block's own
# assertion strings are excluded by anchoring on a command position.
_e1dd_direct="$(grep -cE '^[[:space:]]*(/bin/rm|/usr/bin/killall|killall|pkill)[[:space:]]' "$HERE/run.sh" || true)"
check "this test file makes no direct call to a real destructive command" "0" "${_e1dd_direct:-0}"

unset SHIM_ALLOW_TEARDOWN SHIM_TEARDOWN_LOG TEST_HOME E1DD_ISO E1DD_YES

# ── 20) T-ff8a: a setup that REFUSED must not be torn down ────────────────────
#
# run_all.sh armed `trap cleanup EXIT` BEFORE running setup.sh, and cleanup ran
# teardown.sh unconditionally — while teardown.sh's step 4 is
# `rm -rf "$REPO_ROOT/var/data"`. setup.sh's three prod guards (oc.toml port,
# [storage].dsn, the leftover-listener check) all `exit 2` BEFORE setup has
# created anything, and each of those refusals then went out through the EXIT
# trap into that rm. The guards could stop the START and had no say over the
# FINISH: refusing to touch a DB was followed, one trap later, by deleting it —
# and the more suspicious the configuration, the more certain the deletion,
# because refusing is what fires the trap.
#
# Everything below drives the REAL run_all.sh → setup.sh → teardown.sh chain
# inside a THROWAWAY repo tree built by this file, with the deletion seam
# (oc_e2e_destroy) pointed at a recording impl. Nothing real is removed even when
# the guard is deliberately broken — which it is, twice, below. That is a
# requirement and not a nicety: a test whose own safety depends on the guard it
# is testing destroys the host at precisely the moment the guard regresses.
FF8A_ROOT="$SHIMDIR/ff8a-repo"
FF8A_E2E="$FF8A_ROOT/e2e_test"
FF8A_REC="$SHIMDIR/.ff8a-destroy-record"
FF8A_SENTINEL="$FF8A_ROOT/var/data/officraft.db"
mkdir -p "$FF8A_E2E/lib" "$FF8A_ROOT/server/ocserverd" "$FF8A_ROOT/var/data"
# common.sh derives the prod-port refusal set from config.go and FATALs if it
# cannot parse it, so the throwaway tree carries a copy (same construction as the
# mutant trees in 19d). A COPY, not a symlink: the mutants below rewrite it.
cp "$HERE/../../server/ocserverd/config.go" "$FF8A_ROOT/server/ocserverd/config.go"
cp "$HERE/../lib/common.sh" "$FF8A_E2E/lib/common.sh"
cp "$HERE/../setup.sh" "$HERE/../teardown.sh" "$HERE/../run_all.sh" "$FF8A_E2E/"
# An oc.toml on the WRONG port — the first of setup's three prod guards, chosen
# because it fires earliest and needs no ports, no npm and no go toolchain.
printf '[server]\nport = 19999\n\n[storage]\ndsn = "sqlite:///var/data/e2e.db"\n' > "$FF8A_ROOT/oc.toml"
printf 'PRETEND-THIS-IS-A-REAL-DB\n' > "$FF8A_SENTINEL"

ff8a_run() { # ff8a_run SCRIPT [LIB_OVERRIDE] — echoes "<rc>", output → $SHIMDIR/.ff8a-out
  local script="$1" lib="${2:-}"
  [[ -n "$lib" ]] && cp "$lib" "$FF8A_E2E/lib/common.sh"
  : > "$FF8A_REC"
  ( OC_E2E_DESTROY_RECORD="$FF8A_REC" OC_E2E_DESTROY_IMPL=oc_e2e_destroy_record_only \
    bash "$FF8A_E2E/$script" ) > "$SHIMDIR/.ff8a-out" 2>&1
  echo $?
}
# Line count, NOT `grep -c . || echo 0`: grep exits 1 on an empty file, so the
# `||` fired and the function echoed "0" TWICE — every numeric comparison then
# saw "0\n0", which is neither zero nor a number. The record is always truncated
# before a run, so a plain wc is both simpler and total.
ff8a_recorded() { wc -l < "$FF8A_REC" | tr -d ' '; }

# 20a) POSITIVE CONTROL FIRST. Every headline assertion below is an assertion of
# ABSENCE ("the deletion record is empty"), which is exactly the shape that
# passes when the recorder is broken, the script never ran, or the path moved.
# So: prove the recorder records, by running the REAL teardown.sh on purpose.
rc="$(ff8a_run teardown.sh)"
FF8A_POS="$(ff8a_recorded)"
[[ "$FF8A_POS" -gt 0 ]] \
  && ok "positive control: the real teardown.sh records $FF8A_POS deletion target(s) — the record can be non-empty"
[[ "$FF8A_POS" -gt 0 ]] \
  || bad "positive control FAILED: the real teardown.sh recorded NOTHING (rc=$rc). Every 'record is empty' assertion below would be vacuously green (out: $(tail -c 300 "$SHIMDIR/.ff8a-out"))"
grep -Fq "$FF8A_ROOT/var/data" "$FF8A_REC" \
  && ok "positive control: the recorded target set includes \$REPO_ROOT/var/data (the destructive one)" \
  || bad "positive control: teardown recorded deletions but NOT \$REPO_ROOT/var/data — the record is not watching the dangerous path (got: $(tr '\n' '|' < "$FF8A_REC"))"

# 20b) THE HEADLINE. setup refuses (prod guard, exit 2) → the EXIT trap must
# delete NOTHING.
rc="$(ff8a_run run_all.sh)"
[[ "$rc" != "0" ]] \
  && ok "run_all with a prod-guard-refusing setup exits non-zero (rc=$rc)" \
  || bad "run_all returned 0 despite setup refusing — the fixture is not reproducing the case"
grep -q 'oc.toml port' "$SHIMDIR/.ff8a-out" \
  && ok "…and it refused for the intended reason (setup's oc.toml port prod guard)" \
  || bad "run_all failed for some OTHER reason than the prod guard — this case is testing the wrong path (out: $(tail -c 400 "$SHIMDIR/.ff8a-out"))"
FF8A_N="$(ff8a_recorded)"
check "SETUP REFUSED → the teardown deletion record is EMPTY" "0" "$FF8A_N"
[[ "$FF8A_N" == "0" ]] || bad "  …recorded targets were: $(tr '\n' '|' < "$FF8A_REC")"
# Belt and braces on the filesystem itself: with a recording impl the sentinel
# survives either way, so this is NOT the headline — it is the check that the
# fixture never handed a real path to a real rm.
[[ -f "$FF8A_SENTINEL" ]] \
  && ok "the throwaway var/data sentinel is untouched (this test file deletes nothing real)" \
  || bad "the throwaway var/data sentinel was DELETED — the recording impl is not in force and this test is destructive"

# 20c) SENTINEL — an ARMED run must still be torn down. A gate that never lets
# the teardown run passes 20b and leaks a serve + a DB on every real run.
: > "$FF8A_REC"
FF8A_ARMED_OUT="$( ( OC_E2E_DESTROY_RECORD="$FF8A_REC" OC_E2E_DESTROY_IMPL=oc_e2e_destroy_record_only \
    bash -c 'source "$1/lib/common.sh" >/dev/null 2>&1
             oc_e2e_arm_teardown
             oc_e2e_teardown_on_exit "$1"' _ "$FF8A_E2E" ) 2>&1 )"
FF8A_ARMED_N="$(ff8a_recorded)"
[[ "$FF8A_ARMED_N" -gt 0 ]] \
  && ok "sentinel: an ARMED run DOES tear down ($FF8A_ARMED_N target(s) recorded) — the gate is not a permanent 'no'" \
  || bad "sentinel: an armed run tore down NOTHING — the gate refuses everything, which leaks a serve and a DB on every real run (out: $(tail -c 300 <<<"$FF8A_ARMED_OUT"))"

# 20d) THE ORDERING IN setup.sh. The arming must sit AFTER the last refusal gate
# and BEFORE the first mutation; nothing above can see that, because 20b drives
# the chain through whatever order the file happens to have. Straight-line
# top-level script code, so source order IS execution order.
FF8A_SETUP="$HERE/../setup.sh"
_arm_ln="$(grep -n '^oc_e2e_arm_teardown$' "$FF8A_SETUP" | head -1 | cut -d: -f1)"
if [[ -z "$_arm_ln" ]]; then
  bad "setup.sh has no top-level 'oc_e2e_arm_teardown' call — the teardown can never be armed, or the anchor moved"
else
  # the first mutation must be BELOW the arming…
  _early_mut="$(awk -v arm="$_arm_ln" 'NR<arm && $0 !~ /^[[:space:]]*#/ && /(^|[^-[:alnum:]_])(rm -rf|rm -f|nohup|go build)/' "$FF8A_SETUP" | wc -l | tr -d ' ')"
  check "no mutation in setup.sh happens before the arming" "0" "${_early_mut:-0}"
  _mut_ln="$(grep -nE '^[[:space:]]*(rm -rf|rm -f|nohup|go build)' "$FF8A_SETUP" | head -1 | cut -d: -f1)"
  # …and the PRE-CREATION prod guards must be above it. NOT "every exit 2 is
  # above the arming": setup's 2e TOCTOU re-check also exits 2 and it runs AFTER
  # the builds, when the run genuinely owns things and MUST be torn down. The
  # property is narrower and exact — the arming sits in the gap between the last
  # guard that precedes any mutation and the first mutation itself, so no refusal
  # is stranded on the armed side of a run that created nothing.
  _guards_above="$(awk -v arm="$_arm_ln" 'NR<arm && $0 !~ /^[[:space:]]*#/ && /exit 2/' "$FF8A_SETUP" | wc -l | tr -d ' ')"
  [[ "${_guards_above:-0}" -ge 3 ]] \
    && ok "setup.sh's ${_guards_above} pre-creation prod-guard refusals all sit ABOVE the arming (≥3: oc.toml port, storage.dsn, leftover listener)" \
    || bad "only ${_guards_above:-0} 'exit 2' refusals precede the arming — a prod guard has moved below it and its refusal would arm a teardown for a run that created nothing"
  _stranded="$(awk -v arm="$_arm_ln" -v mut="${_mut_ln:-0}" 'NR>arm && NR<mut && $0 !~ /^[[:space:]]*#/ && /exit 2/' "$FF8A_SETUP" | wc -l | tr -d ' ')"
  check "no refusal sits between the arming and the first mutation" "0" "${_stranded:-0}"
fi
# CONTROL for both scans above — they are greps for absence, so a pattern broken
# by a later edit yields 0 forever and both assertions go permanently, silently
# green. Run the SAME scans over a deliberately dirty fixture.
_ff8a_fix="$SHIMDIR/ff8a-order-fixture.sh"
{ printf '# exit 2 — a comment, must not count\n'
  printf 'rm -rf "$REPO_ROOT/var/data"\n'
  printf 'oc_e2e_arm_teardown\n'
  printf 'exit 2\n'
  printf 'nohup serve &\n'; } > "$_ff8a_fix"
check "ordering-scan control: the early-mutation scan reddens on a fixture whose rm is above the arming (and skips the comment)" \
  "1" "$(awk -v arm=3 'NR<arm && $0 !~ /^[[:space:]]*#/ && /(^|[^-[:alnum:]_])(rm -rf|rm -f|nohup|go build)/' "$_ff8a_fix" | wc -l | tr -d ' ')"
check "ordering-scan control: the stranded-refusal scan reddens on a fixture whose 'exit 2' sits between the arming and the mutation" \
  "1" "$(awk -v arm=3 -v mut=5 'NR>arm && NR<mut && $0 !~ /^[[:space:]]*#/ && /exit 2/' "$_ff8a_fix" | wc -l | tr -d ' ')"
check "ordering-scan control: the guards-above scan counts 0 on a fixture with no refusal above the arming" \
  "0" "$(awk -v arm=3 'NR<arm && $0 !~ /^[[:space:]]*#/ && /exit 2/' "$_ff8a_fix" | wc -l | tr -d ' ')"

# 20e) THE SEAM MUST BE THE ONLY WAY OUT of teardown.sh. A raw `rm` reintroduced
# there is invisible to every assertion above: the record stays empty and the
# deletion happens anyway — the exact "the record says nothing was deleted"
# false green this whole case is built on.
FF8A_TEARDOWN="$HERE/../teardown.sh"
_raw_rm="$(grep -cE '^[[:space:]]*rm[[:space:]]+-' "$FF8A_TEARDOWN" || true)"
check "teardown.sh has NO raw rm — every delete goes through the recorded seam" "0" "${_raw_rm:-0}"
check "raw-rm scan control: the same scan finds the 2 raw rms in a dirty fixture" "2" \
  "$(printf '# rm -rf /commented\nrm -rf /x\n  rm -f /y\noc_e2e_destroy /z\n' > "$_ff8a_fix"; grep -cE '^[[:space:]]*rm[[:space:]]+-' "$_ff8a_fix" || true)"
# …and run_all.sh's trap must reach teardown through the GATE, not directly.
FF8A_RUNALL="$HERE/../run_all.sh"
# NON-COMMENT lines only, on both scans. A plain `grep -F oc_e2e_teardown_on_exit`
# matched the COMMENT above the trap — so restoring the old ungated cleanup body
# left this assertion green while the name it was looking for survived only as
# prose. Verified against the real mutant: the loose form stayed green, this one
# does not. The comment is where the name is MOST likely to linger, which makes
# it the worst possible thing to accept as evidence.
_gated="$(grep -cE '^[^#]*oc_e2e_teardown_on_exit' "$FF8A_RUNALL" || true)"
[[ "${_gated:-0}" -gt 0 ]] \
  && ok "run_all.sh's EXIT trap goes through oc_e2e_teardown_on_exit (the gate), in CODE not a comment" \
  || bad "run_all.sh no longer calls oc_e2e_teardown_on_exit outside a comment — the trap has gone back to being ungated (T-ff8a regression)"
_direct_td="$(grep -cE '^[^#]*bash "\$HERE/teardown\.sh"' "$FF8A_RUNALL" || true)"
check "run_all.sh does not invoke teardown.sh directly (bypassing the gate)" "0" "${_direct_td:-0}"
# CONTROL for both: the SAME two scans over a fixture carrying the pre-T-ff8a
# cleanup body, with the gate's name present only as prose.
{ printf '# oc_e2e_teardown_on_exit — named in a comment only\n'
  printf 'cleanup() { bash "$HERE/teardown.sh" || true; }\n'; } > "$_ff8a_fix"
check "trap-scan control: the gate scan counts 0 when the name appears only in a comment" \
  "0" "$(grep -cE '^[^#]*oc_e2e_teardown_on_exit' "$_ff8a_fix" || true)"
check "trap-scan control: the direct-call scan counts 1 on the pre-T-ff8a cleanup body" \
  "1" "$(grep -cE '^[^#]*bash "\$HERE/teardown\.sh"' "$_ff8a_fix" || true)"

# 20f) MUTANTS — without them 20b is satisfied by a chain that was never going to
# delete anything. One edit each, against the THROWAWAY tree's copy of the lib.
ff8a_mutant() { # ff8a_mutant NAME SED_EXPR
  local name="$1" expr="$2" mut="$SHIMDIR/ff8a-mut-$1.sh"
  sed "$expr" "$HERE/../lib/common.sh" > "$mut"
  if cmp -s "$mut" "$HERE/../lib/common.sh"; then
    bad "MUT-$name did not change lib/common.sh — the mutation anchor moved; a vacuous mutant proves nothing"
    return
  fi
  local rc; rc="$(ff8a_run run_all.sh "$mut")"
  local n; n="$(ff8a_recorded)"
  [[ "$n" -gt 0 ]] \
    && ok "MUT-$name: with that check removed, a REFUSED setup deletes $n target(s) — 20b is pinned to it" \
    || bad "MUT-$name: a refused setup still deleted nothing (rc=$rc) — 20b is NOT pinned to this check, it would pass without it"
  cp "$HERE/../lib/common.sh" "$FF8A_E2E/lib/common.sh"
}
# 1. the gate itself: make the armed-check answer yes unconditionally — this is
#    literally the pre-T-ff8a behaviour ("the trap runs regardless").
ff8a_mutant gate 's|^oc_e2e_teardown_armed() .*$|oc_e2e_teardown_armed() { return 0; }|'
# 2. the gate's USE: have the trap helper run teardown without consulting it. The
#    check can survive as a function and still be wired to nothing.
ff8a_mutant use 's|^  if ! oc_e2e_teardown_armed; then$|  if false; then|'
[[ -f "$FF8A_SENTINEL" ]] \
  && ok "after both mutants, the throwaway sentinel is STILL there — breaking the guard cannot make this test destructive" \
  || bad "a mutant DELETED the throwaway sentinel — this test file relies on the guard it is testing for its own safety"

# ── 21) T-42bb: the seven_gate VERDICT — it must go red, and name the step ────
#
# seven_gate/judge.py decides whether a seven-step run happened, reading ONLY
# what the server was observed to hold. The thing that can silently rot in it is
# not "does a good run pass" — a judge that returns PASS unconditionally does
# that too. It is "does a run MISSING one step go red, and does it say WHICH".
# So the shape below is: one green fixture as the control, then SEVEN mutants,
# one per step, each removing exactly that step's fact from the bundle. Each must
# exit 1 AND name its own step on the last line. A mutant that reddens the wrong
# step is as bad as one that stays green — the caller acts on the name.
#
# HERMETIC: no server, no network. The bundle is a handful of JSON objects
# written here, which is the whole reason collect.py (I/O) and judge.py (pure)
# are separate files.
SG_DIR="$HERE/../seven_gate"
SG_WORK="$SHIMDIR/seven-gate"
mkdir -p "$SG_WORK"

# The full-green bundle, as a python emitter so a mutant is one deleted key.
# `python3` here is the same text-tool use lib/common.sh already makes of it.
cat > "$SG_WORK/mk.py" <<'PY'
import json, os, sys
drop, out = sys.argv[1], sys.argv[2]
AG, NONCE = "m-sg", "sg-nonce-deadbeef"
PEER, PEER_NONCE = "m-sg-peer", "sg-peer-nonce-feedface"
IMG_ANSWER = "481902"   # the number that, in a real run, exists only in pixels
step0 = {"id": "s1", "name": "走完七步", "status": "done" if drop != "step_done" else "todo"}
task = {"id": "T-1", "creator_id": AG, "title": "probe", "created_ts": 100,
        "updated_ts": 200, "status": "done",
        "steps": [] if drop == "submit_plan" else [step0],
        "closeout_reported": drop != "closeout"}
samples = [
    {"t": 1.0, "member": {"id": AG, "presence": "offline"}, "chat": [], "tasks": [], "reply_cards": []},
    {"t": 2.0,
     "member": {"id": AG, "presence": "online" if drop == "report_waking" else "waking"},
     "chat": [], "tasks": [], "reply_cards": []},
    {"t": 9.0, "member": {"id": AG, "presence": "online"},
     "chat": ([] if drop == "resume_scene" else
              [{"id": "c1", "from": AG, "to": "owner", "body": "接回現場：" + NONCE}])
             # ⑦'s fact: agent → PEER, quoting what the peer said. The mutant
             # drops the whole message; note it is a DIFFERENT recipient from
             # c1, so a judge that only checked `from == agent` would keep
             # passing on c1 alone and this mutant would go green.
             + ([] if drop == "peer_message" else
                [{"id": "c2", "from": AG, "to": PEER,
                  "body": "收到：" + PEER_NONCE}])
             # ⑨'s fact: the agent SAID the number that only the picture carries.
             # The mutant is the real-world "the picture had no answer in it / the
             # agent never opened it" case — the message simply never appears.
             + ([] if drop == "image_answer" else
                [{"id": "c3", "from": AG, "to": "owner",
                  "body": "圖上的號碼是 " + IMG_ANSWER}]),
     "tasks": [] if drop == "create_task" else [task],
     "reply_cards": [] if drop == "reply_card" else
                    [{"id": "rc-1", "from": AG, "status": "waiting"}]},
]
os.makedirs(out, exist_ok=True)
json.dump({"agent_id": AG, "scene_nonce": NONCE,
           "peer_id": PEER, "peer_nonce": PEER_NONCE,
           "image_answer": IMG_ANSWER},
          open(out + "/scene.json", "w"))
with open(out + "/journal.ndjson", "w") as fh:
    for s in samples:
        fh.write(json.dumps(s, ensure_ascii=False) + "\n")
PY

sg_judge() { # sg_judge DROP -> prints "<rc>|<last line>"
  local drop="$1" dir="$SG_WORK/b-$1"
  rm -rf "$dir"
  python3 "$SG_WORK/mk.py" "$drop" "$dir" >/dev/null 2>&1 || { echo "9|fixture-build-failed"; return; }
  local outp rc
  outp="$(python3 "$SG_DIR/judge.py" "$dir" 2>&1)"; rc=$?
  printf '%s|%s\n' "$rc" "$(printf '%s\n' "$outp" | tail -n 1)"
}

# 21a) the control: nothing dropped → green, and the marker is EXACT. Without
# this the seven mutants below are satisfied by a judge that fails everything.
_sg="$(sg_judge none)"
check "seven_gate: a complete run exits 0" "0" "${_sg%%|*}"
check "seven_gate: a complete run's last line is the exact marker" \
  "[seven_gate] all green" "${_sg#*|}"

# 21b) ONE MUTANT PER STEP — that step's fact removed from the bundle each time. Both halves are
# asserted per mutant: rc must be 1 (green would mean the gate cannot say no)
# and the last line must name THAT step (a red pointing elsewhere sends the
# reader to the wrong place, which costs more than no red at all).
sg_mutant() { # sg_mutant KEY ZH
  local key="$1" zh="$2" res rc last
  res="$(sg_judge "$key")"; rc="${res%%|*}"; last="${res#*|}"
  check "seven_gate: with 「${zh}」 missing, the verdict is RED" "1" "$rc"
  case "$last" in
    *"failed at step"*"$key"*) ok "seven_gate: the RED names 「${zh}」 ($key) — $last" ;;
    *) bad "seven_gate: 「${zh}」 was missing but the verdict named something else: $last" ;;
  esac
}
sg_mutant report_waking 報到
sg_mutant resume_scene  接回現場
sg_mutant create_task   開票
sg_mutant submit_plan   提出計畫
sg_mutant step_done     報一步完成
sg_mutant reply_card    開一張等我回覆卡
sg_mutant closeout      回報收尾
sg_mutant peer_message  回覆另一個-agent
sg_mutant image_answer  看得到圖

# 21c) an EMPTY journal must not read as a pass. This is the failure mode a
# collector crash produces, and "no evidence" answering green is the one bug
# that would make every future run meaningless.
rm -rf "$SG_WORK/b-empty"; mkdir -p "$SG_WORK/b-empty"
printf '{"agent_id":"m-sg","scene_nonce":"n"}' > "$SG_WORK/b-empty/scene.json"
: > "$SG_WORK/b-empty/journal.ndjson"
python3 "$SG_DIR/judge.py" "$SG_WORK/b-empty" >/dev/null 2>&1
check "seven_gate: an EMPTY journal is RED, not green" "1" "$?"

# 21d) the friction wording is the load-bearing part of the follow-up and it
# lives in exactly ONE file. Pinned verbatim, because the way this stops working
# is someone "tidying" it into 「順不順」 — which returns a pleasantry every time
# and therefore returns nothing.
SG_FRICTION="$SG_DIR/friction.md"
check "seven_gate: friction Q1 is verbatim" "1" \
  "$(grep -cF '哪一步你猶豫了／翻回去重讀了／用猜的？' "$SG_FRICTION" || true)"
check "seven_gate: friction Q2 is verbatim" "1" \
  "$(grep -cF '你有沒有做出後來才發現做錯的事？' "$SG_FRICTION" || true)"
# The banned phrasings may be NAMED in the prose that explains why they are
# banned, so the scan is for a QUESTION — the phrase followed by a question mark.
_sg_bad="$(grep -cE '(順不順|順利嗎|有沒有問題|還可以嗎)[？?]' "$SG_FRICTION" || true)"
check "seven_gate: friction asks none of the pleasantry questions" "0" "${_sg_bad:-0}"
# run.sh must READ that file rather than carry its own copy of the questions —
# two copies drift, and the one that drifts is the one that gets asked.
_sg_reads="$(grep -cF 'friction.md' "$SG_DIR/run.sh" || true)"
[[ "${_sg_reads:-0}" -gt 0 ]] \
  && ok "seven_gate: run.sh sources the questions from friction.md (no second copy)" \
  || bad "seven_gate: run.sh no longer reads friction.md — the questions have been copied, and copies drift"

# 21e) the default actor spawns nothing. This file cannot prove what a live
# actor costs, but it CAN pin that the default is not one: run.sh's fallback
# actor must be the stub, and the stub must not reach for a claude binary.
_sg_def="$(grep -cE 'OC_SG_ACTOR:-\$HERE/actors/stub\.sh' "$SG_DIR/run.sh" || true)"
[[ "${_sg_def:-0}" -gt 0 ]] \
  && ok "seven_gate: run.sh's default actor is the stub (no agent spawned unless asked)" \
  || bad "seven_gate: run.sh's default actor is no longer the stub — a bare run may now burn API quota"
_sg_claude="$(grep -cE '(^|[^a-z])claude([^a-z]|$)' "$SG_DIR/actors/stub.sh" || true)"
check "seven_gate: the stub actor never invokes claude" "0" "${_sg_claude:-0}"

# 21f) NO SERVER CALL MAY BE MADE OUTSIDE THE ONE LOGGING HELPER. This is the
# bug that cost the first baseline: every call was `curl … >/dev/null`, so the
# three that the server REFUSED (a 409 each) looked exactly like the ones it
# accepted, and "the call failed" was indistinguishable from "the call worked
# and the fact still is not there" — which is the shape a wrong API contract
# takes. lib/http.sh writes the method, path, HTTP STATUS and BODY of every
# call; the invariant that keeps it true is that nothing else reaches curl.
# A reminder in a comment would not survive; this will.
# Comment lines are stripped first: these files EXPLAIN the banned shape in
# their headers, and a scan that cannot tell the rule from its own description
# reddens on the documentation — the fastest way to get a guard deleted.
_sg_code_only() { grep -v '^[[:space:]]*#' "$1"; }
for _sg_caller in "$SG_DIR/run.sh" "$SG_DIR"/actors/*.sh; do
  _sg_curl="$(_sg_code_only "$_sg_caller" | grep -cE '(^|[^[:alnum:]_])curl([^[:alnum:]_]|$)' || true)"
  check "seven_gate: $(basename "$_sg_caller") makes no raw curl call (every call goes through lib/http.sh, which logs status + body)" \
    "0" "${_sg_curl:-0}"
done
# …and the helper itself must not throw a response away. `-o <file>` + `-w
# %{http_code}` is the shape that keeps both halves; a curl in here piped to
# /dev/null would restore the blindness at its source.
_sg_helper_null="$(_sg_code_only "$SG_DIR/lib/http.sh" | grep -cE 'curl[^#]*>[[:space:]]*/dev/null' || true)"
check "seven_gate: lib/http.sh never sends a curl response to /dev/null" "0" "${_sg_helper_null:-0}"
# "log the whole body" and "never write a credential to disk" are both true only
# because the helper redacts. /api/mint and /api/machines answer with live
# bearer JWTs; the first run of this harness wrote three of them into run.log and
# http.log and bin/ci.sh's gitleaks gate caught it. Pinned as a behaviour, not a
# grep for the sed: a fixture body is pushed through the real function.
_sg_redact="$(SG_HTTP_LOG="" bash -c '. "$1"/lib/http.sh; _sg_http_oneline "{\"token\":\"eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiJtLTEifQ.c2lnbmF0dXJl\",\"id\":\"m-1\"}"' _ "$SG_DIR" 2>/dev/null)"
case "$_sg_redact" in
  *eyJ*) bad "seven_gate: lib/http.sh logs a bearer JWT verbatim — the harness would write live credentials to run.log/http.log (got: $_sg_redact)" ;;
  *REDACTED*id*m-1*) ok "seven_gate: lib/http.sh redacts credentials but keeps the rest of the body (got: $_sg_redact)" ;;
  *) bad "seven_gate: lib/http.sh's redaction ate the body — a redacted log that shows nothing else is as blind as no log (got: $_sg_redact)" ;;
esac
_sg_helper_code="$(grep -cF '%{http_code}' "$SG_DIR/lib/http.sh" || true)"
[[ "${_sg_helper_code:-0}" -ge 1 ]] \
  && ok "seven_gate: lib/http.sh captures the HTTP status code (a body without a status cannot separate a refusal from a no-op)" \
  || bad "seven_gate: lib/http.sh no longer captures %{http_code} — a refused call and an accepted one are indistinguishable again"

# 21g) the LIVE actor is default-off, and its opt-in is STRICT. e2e_test/CLAUDE.md
# records what the loose version cost: an EXCLUDE-shaped flag set in only one
# place meant every laptop spawned real agents and paid for them. The switch
# must be an INCLUDE flag compared exactly, so every typo lands on "did not run,
# did not spend".
if [[ -f "$SG_DIR/actors/live.sh" ]]; then
  _sg_live_optin="$(grep -cE 'OC_SG_LIVE_AGENT.*!=[[:space:]]*"1"' "$SG_DIR/actors/live.sh" || true)"
  [[ "${_sg_live_optin:-0}" -ge 1 ]] \
    && ok "seven_gate: live.sh refuses unless OC_SG_LIVE_AGENT is EXACTLY \"1\" (strict include-flag, not an exclude-flag)" \
    || bad "seven_gate: live.sh's spend opt-in is not a strict '!= \"1\"' refusal — a typo could now spawn a real agent and spend real quota"
  # It must ask the questions from the ONE file, never carry its own copy —
  # same reason 21d pins run.sh.
  _sg_live_fr="$(grep -cE 'sg_friction_questions|friction\.md' "$SG_DIR/actors/live.sh" || true)"
  [[ "${_sg_live_fr:-0}" -ge 1 ]] \
    && ok "seven_gate: live.sh takes the friction questions from friction.md (no second copy)" \
    || bad "seven_gate: live.sh no longer reads friction.md — the questions have been copied, and copies drift"
  # And it must not author the answers. The banned shape is a friction.txt
  # written from a string this file made up; the allowed one is the agent's own
  # messages. Pinned as: live.sh never claims an answer the agent did not send.
  _sg_live_verbatim="$(grep -cE '載體不代寫' "$SG_DIR/actors/live.sh" "$SG_DIR/friction.md" 2>/dev/null | awk -F: '{s+=$2} END {print s+0}')"
  [[ "${_sg_live_verbatim:-0}" -ge 1 ]] \
    && ok "seven_gate: the no-ghostwriting rule for friction answers is stated where the writer of friction.txt will read it" \
    || bad "seven_gate: nothing states that the harness must not write the friction answers itself"
fi

# ── 22) T-42bb: the collection window must outlast the actor ──────────────────
#
# THE BUG. collect.py used to be started with `--seconds 900` while
# actors/live.sh would wait 30 + 120 + 1800 + 300 ≈ 2250s. On DEFAULTS the
# collector stopped sampling ~22 minutes before the actor stopped working, so
# every fact that landed after that instant was invisible to judge.py — and the
# verdict it produced was 「回報收尾 FAIL」: A RED NAMING THE AGENT FOR THE
# HARNESS'S OWN GAP. The person who hit it worked around it by knowing to raise
# OC_SG_MAX_SECONDS; the next person would not have known that flag existed.
#
# So what is pinned here is the RELATION, not a number: whatever the knobs are
# set to, the collector's window must be >= the actor's budget. A future knob
# that lengthens the actor lengthens the budget, and this keeps holding with
# nobody remembering it.
SG_WINDOW="$SG_DIR/lib/window.sh"
sg_window_probe() { # sg_window_probe <file> [env assignments…] -> "budget|window|rc"
  local f="$1"; shift
  env "$@" bash -c '
    . "$1" || exit 9
    b="$(sg_actor_budget_secs)"; w="$(sg_collect_seconds)"
    sg_assert_collection_window >/dev/null 2>&1; rc=$?
    printf "%s|%s|%s\n" "$b" "$w" "$rc"' _ "$f"
}

# 22a) the shipped defaults hold — and the window is genuinely bigger, not equal
# by accident of both being zero.
_w="$(sg_window_probe "$SG_WINDOW")"
_wb="${_w%%|*}"; _rest="${_w#*|}"; _ww="${_rest%%|*}"; _wrc="${_rest##*|}"
check "seven_gate: the shipped defaults satisfy the collection-window invariant" "0" "$_wrc"
[[ "${_wb:-0}" -gt 0 && "${_ww:-0}" -gt "${_wb:-0}" ]] \
  && ok "seven_gate: collector window ${_ww}s strictly exceeds the actor budget ${_wb}s (not equal-by-accident)" \
  || bad "seven_gate: window=${_ww:-?} budget=${_wb:-?} — the window must strictly exceed a non-zero budget"

# 22b) it must track the ACTOR, not a constant: stretch the longest actor wait
# and the window has to grow with it. This is the half a hardcoded number fails.
_w2="$(sg_window_probe "$SG_WINDOW" OC_SG_LIVE_WAIT=99999)"
_w2b="${_w2%%|*}"; _r2="${_w2#*|}"; _w2w="${_r2%%|*}"; _w2rc="${_r2##*|}"
check "seven_gate: a much longer live wait still satisfies the invariant" "0" "$_w2rc"
[[ "${_w2w:-0}" -gt "${_ww:-0}" ]] \
  && ok "seven_gate: stretching OC_SG_LIVE_WAIT grew the collector window (${_ww}s → ${_w2w}s) — it is derived, not fixed" \
  || bad "seven_gate: OC_SG_LIVE_WAIT grew but the collector window did not (${_ww}s → ${_w2w}s) — the derivation is severed"

# 22c) THE MUTANT. Sever the derivation — put the old independent constant back —
# and the invariant must go RED. Without this, a sg_collect_seconds that returned
# a huge constant would satisfy 22a/22b's rc check and this case would guard
# nothing. The mutant is the exact historical bug, not an invented one.
SG_WMUT="$SHIMDIR/window-mutant.sh"
sed -e 's|^  echo \$(( \$(sg_actor_budget_secs) + OC_SG_SETTLE + OC_SG_COLLECT_MARGIN ))$|  echo 900|' \
    "$SG_WINDOW" > "$SG_WMUT"
if ! grep -qE '^  echo 900$' "$SG_WMUT"; then
  bad "seven_gate: the window mutant did not apply — the derivation line moved, so case 22c is testing nothing (fix the sed)"
else
  _wm="$(sg_window_probe "$SG_WMUT")"
  _wmrc="${_wm##*|}"
  check "seven_gate: with the derivation severed (collector window back to a constant 900), the invariant goes RED" "1" "$_wmrc"
fi

# 22d) the defaults have ONE home. A second `:-<default>` for any of these knobs
# in run.sh or the actors is a second constant, and two constants a human keeps
# in sync is the shape that produced the bug.
for _knob in OC_SG_LIVE_WAIT OC_SG_MACHINE_WAIT OC_SG_SPAWN_WAIT OC_SG_FRICTION_WAIT OC_SG_CARD_WAIT OC_SG_SETTLE; do
  _dupes="$(grep -h -oE "\\\$\{$_knob:-[^}]*\}" "$SG_DIR/run.sh" "$SG_DIR"/actors/*.sh 2>/dev/null | wc -l | tr -d ' ')"
  check "seven_gate: $_knob has no second default outside lib/window.sh" "0" "${_dupes:-0}"
done

echo "[tests_guard] PASS=$PASS FAIL=$FAIL"
[[ "$FAIL" -eq 0 ]] || exit 1

# ── PASS FLOOR ──────────────────────────────────────────────────────────────
# See SCOPE at the top: there is no discovery here, so a case block that stops
# existing takes its assertions with it and everything still reports green. This
# floor is what makes that loud ONCE ENOUGH OF IT IS GONE — see the measured
# cells below for where that threshold actually sits.
#
# A FLOOR, not the exact count, on purpose — the same reasoning that
# e2e_test/assert-specs-ran.sh writes out for the spec tally: an exact number
# goes stale the first time someone adds a case, and a stale exact number teaches
# the next person that this file lies, which is worse than not asserting at all.
# What the floor has to catch is "the suite got gutted", not "today's total
# changed". It sits well under the current count so growth never reddens it, and
# it does NOT need updating when cases are added.
#
# WHAT IT CATCHES — state the size of the hole, not just that one exists.
# Measured 2026-08-08: PASS=153 against a floor of 100. So roughly A THIRD of the
# assertions — 53 of them — can evaporate silently and this still goes green.
# (That figure moves as cases are added; it is recorded as a measurement on a
# day, not as the current state. Recompute it, do not trust it.)
#
# And it is VOLUME-SHAPED, not importance-shaped: it counts assertions and does
# not care WHICH ones went. The highest-value blocks are among the smallest —
# case 11 (the rc-propagation shape of run_all.sh) and 20e (teardown's only way
# out) — so deleting either one on its own sits comfortably inside the tolerance
# and this file will tell you everything is fine. Nothing here watches at
# case-name or block granularity; an exact count that would is refused above for
# a reason that costs more than it saves.
# Mutants, each restored from a scratchpad copy with the sha256 re-checked:
#   * floor raised to an unreachable 9999            → PASS=153 FAIL=0, rc=1, named.
#   * the whole 19x/20x half of the file deleted     → PASS=66  FAIL=0, rc=1, named.
#   * ONE case block (19a, five assertions) deleted  → PASS=148 FAIL=0, rc=0 — GREEN.
#
# THE SUCCESS MARKER IS PRINTED FROM INSIDE THIS BLOCK, from the floor's passing
# branch and nowhere else — that is the only reason bin/ci.sh's `tail -n 1`
# check says anything about the floor. It used to sit on its own line after the
# `fi`, and then deleting this whole block while leaving that last line behind
# printed the marker with no floor evaluated at all: MEASURED, floor block
# deleted and the trailing echo kept → PASS=153 FAIL=0 rc=0, last line
# `[tests_guard] all green`, `bin/ci.sh` all green. Keep it in the branch.
PASS_FLOOR=100
if [[ "$PASS" -lt "$PASS_FLOOR" ]]; then
  echo "[tests_guard] FATAL: only $PASS assertion(s) ran, floor is $PASS_FLOOR." >&2
  echo "[tests_guard] FAIL=0 with a collapsed PASS count means cases went missing, not that they passed." >&2
  exit 1
else
  echo "[tests_guard] all green"
fi
