#!/usr/bin/env bash
# e2e_test/seven_gate/lib/carrier.sh — the run must outlive the session that
# started it, and it must NEVER die silently.
#
# 🔴 WHAT HAPPENED (2026-08-10, a run that had already spent money). run.sh was
# started as a background command by an agent session. That session was collected
# by a relocate, and the carrier was killed WITH it — the log simply stops
# mid-poll, with no error, no teardown line, no verdict. The agent it had spawned
# did NOT die (agents live in tmux, detached), so the outcome was the worst
# available combination: nobody judged, nobody tore down, and a real agent kept
# burning quota against a server nobody was watching.
#
# And the part that made it expensive rather than merely annoying: THE WAITER
# NEVER LEARNED. Whoever was watching the run was waiting for the rc of the outer
# invocation, and that rc is written by the SHELL THAT DIED — so it was never
# written at all. A dead run and a running run looked identical (silence) until a
# timeout hours later.
#
# So this file is TWO things, and the second matters more than the first:
#
#   1. DETACH. The carrier puts itself in its OWN session (setsid) so that the
#      death of the process/pipeline/process-group that started it does not reach
#      it. Crucially it does this ITSELF — `git grep -nE 'nohup|setsid|disown'`
#      over seven_gate/ used to be ZERO HITS, i.e. survival depended entirely on
#      the CALLER remembering `nohup`. "Depends on a human remembering" is the
#      exact thing this gate exists to delete.
#
#   2. A TERMINAL SIGNAL, ALWAYS. However the carrier ends — clean, refusal,
#      TERM, HUP, INT, a broken stdout pipe, even an untrappable KILL — a single
#      file appears with its rc in it (`<run dir>/outer.rc`, plus a one-line
#      `outer.status` saying WHY). Because if detach ever fails for a reason
#      nobody predicted, the failure mode must be A VISIBLE DEATH, not a silent
#      one. A waiter that polls one file can then always tell "still running"
#      from "over, and here is the code".
#
# THREE LAYERS, deliberately overlapping, because each covers what the previous
# one cannot:
#   * the EXIT trap                → every ordinary end, including `exit 2` refusals
#   * the signal traps (TERM/HUP/INT/QUIT/PIPE) → deaths bash can see; each one
#     records its own reason and then exits THROUGH the EXIT trap, so teardown
#     still runs
#   * the WATCHDOG                 → SIGKILL, which no trap can catch. A tiny
#     recorded child watches the carrier's pid and, if the carrier vanishes with
#     no rc file, writes one (reason=vanished). This is the layer that makes
#     "died with no signal" structurally impossible rather than merely unlikely.
#
# ⚠️ LIMITS, so nobody reads more into a green than is there:
#   * A TRAP IS NOT INSTANT. bash runs a caught signal's handler only when the
#     foreground command it is waiting on returns — so a TERM delivered while the
#     carrier is inside setup.sh or the actor is recorded when THAT returns, not
#     at the instant of the signal. The promise is "a signal always appears with
#     the right rc and reason", not "it appears immediately". The instantaneous
#     case is SIGKILL, and that one is the watchdog's.
#   * The watchdog polls, so its answer arrives within OC_SG_WATCHDOG_INTERVAL.
#   * If the carrier AND its watchdog are killed in the same volley (a group kill
#     that reaches the new session — e.g. someone kills this run's session
#     deliberately), nothing is left to write. Detachment is what puts the run
#     out of reach of the CALLER's group kill; it is not armour against a kill
#     aimed at the run itself.
#
# ⚠️ WHAT THIS FILE MUST NOT BECOME. It kills nothing and it names no session.
# The isolation rules in lib/ownedkill.sh (own socket + exact recorded names,
# fail-closed) are untouched by anything here; the watchdog's only verb is
# `kill -0` (a liveness probe, not a signal) against ONE pid — this shell's own,
# recorded by this shell at arming time.

# ── knobs ────────────────────────────────────────────────────────────────────
# OC_SG_NO_DETACH=1  — stay in the caller's session (for tests that WANT the
#                      historical behaviour, and for anyone debugging under a
#                      supervisor that already provides detachment).
# OC_SG_DETACHED=1   — set by the detach itself; the marker that says "you are
#                      already the detached copy, do not re-exec".
: "${OC_SG_NO_DETACH:=0}"
: "${OC_SG_WATCHDOG_INTERVAL:=2}"

SG_CARRIER_RC_FILE=""
SG_CARRIER_RC_WRITTEN=0
SG_CARRIER_REASON="exit"
SG_CARRIER_WATCHDOG_PID=""

# sg_carrier_detach SCRIPT [ARGS…] — re-exec SCRIPT in a NEW SESSION.
#
# Returns (and the caller carries on unchanged) when the re-exec must not happen:
# already detached, explicitly disabled, or no python3. Otherwise it does not
# return — the process is replaced.
#
# WHY python3 AND NOT `setsid`: macOS ships no setsid(1) (this harness's primary
# host), and `nohup` only ignores SIGHUP — it leaves the process in the caller's
# process group, so a `kill -- -<pgid>` (what a supervisor collecting a session
# does) still takes it. os.setsid() is the thing that actually changes the answer.
# python3 is already a hard dependency of every other file in this directory.
#
# THE RELAY. The python parent does NOT exit immediately: it forks, and the
# ORIGINAL pid stays behind to wait for the detached child and re-exit with its
# status. So `bash run.sh; echo $?` keeps working exactly as before for a caller
# that lives — the foreground contract is unchanged — while a caller that DIES
# only loses the relay. The real run, in its own session, carries on and writes
# its own terminal signal.
sg_carrier_detach() {
  if [[ "${OC_SG_DETACHED:-0}" == "1" ]]; then
    return 0
  fi
  if [[ "$OC_SG_NO_DETACH" == "1" ]]; then
    echo "[carrier] OC_SG_NO_DETACH=1 — staying in the caller's session. If the caller dies, this run dies with it; the terminal signal is then the only thing left." >&2
    return 0
  fi
  if ! command -v python3 >/dev/null 2>&1; then
    echo "[carrier] WARNING: no python3 — cannot detach. The run continues IN the caller's session (it can be killed with it); the terminal signal still covers every death bash can see." >&2
    return 0
  fi
  export OC_SG_DETACHED=1
  exec python3 -c '
import os, sys
argv = sys.argv[1:]
pid = os.fork()
if pid:
    # The relay: still the caller`s child, so `wait` and `$?` behave as before.
    try:
        _, st = os.waitpid(pid, 0)
    except BaseException:
        os._exit(0)
    os._exit(os.WEXITSTATUS(st) if os.WIFEXITED(st) else 128 + os.WTERMSIG(st))
os.setsid()          # new session + new process group: a group kill cannot reach us
os.execvp(argv[0], argv)
' "${BASH:-bash}" "$@"
}

# sg_carrier_write RC — write the terminal signal, exactly once.
# Writes are best-effort-but-loud-order: temp file + rename so a reader can never
# see a half-written rc, with a direct write as the fallback.
sg_carrier_write() {
  local rc="${1:-0}"
  [[ -n "$SG_CARRIER_RC_FILE" ]] || return 0
  [[ "$SG_CARRIER_RC_WRITTEN" == "1" ]] && return 0
  SG_CARRIER_RC_WRITTEN=1
  local tmp="$SG_CARRIER_RC_FILE.$$.tmp"
  if ! { printf '%s\n' "$rc" > "$tmp" && mv -f "$tmp" "$SG_CARRIER_RC_FILE"; } 2>/dev/null; then
    printf '%s\n' "$rc" > "$SG_CARRIER_RC_FILE" 2>/dev/null || true
  fi
  printf '%s rc=%s reason=%s pid=%s\n' \
    "$(date -u '+%Y-%m-%dT%H:%M:%SZ')" "$rc" "$SG_CARRIER_REASON" "$$" \
    >> "${SG_CARRIER_RC_FILE%.rc}.status" 2>/dev/null || true
  return 0
}

# sg_carrier_die NAME RC — a signal handler. Records WHY, then exits through the
# EXIT trap so the caller's own cleanup (collector, responder, teardown) still
# runs. Never write the rc here: one writer, one place.
sg_carrier_die() {
  SG_CARRIER_REASON="signal:${1:-?}"
  echo "[carrier] caught ${1:-?} — tearing down and writing the terminal signal." >&2
  exit "${2:-143}"
}

# sg_carrier_arm RC_FILE — install the signal traps and remember where the
# terminal signal goes. The EXIT trap is NOT installed here on purpose: the
# caller already owns EXIT (teardown lives there), so it calls sg_carrier_write
# from its own cleanup. Two EXIT traps cannot coexist in bash — the second
# silently replaces the first, which is how a teardown gets deleted by accident.
sg_carrier_arm() {
  SG_CARRIER_RC_FILE="${1:?sg_carrier_arm needs the rc file path}"
  trap 'sg_carrier_die TERM 143' TERM
  trap 'sg_carrier_die HUP  129' HUP
  trap 'sg_carrier_die INT  130' INT
  trap 'sg_carrier_die QUIT 131' QUIT
  # A broken stdout (the caller's pipe went away) used to kill this script with
  # no trace at all — the same silent death, arriving through a different door.
  trap 'sg_carrier_die PIPE 141' PIPE
  return 0
}

# sg_carrier_watchdog — the SIGKILL layer.
#
# 🔴 WHY IT IS NOT REDUNDANT. Every trap above is a promise bash can only keep if
# bash is still alive to keep it. `kill -9`, an OOM kill, or a supervisor that
# escalates past TERM leaves NO trap running — and that is precisely the death
# that produced today's incident-shaped silence. So one child, spawned before any
# of the expensive work, holds the answer: it polls the carrier's liveness and,
# the moment the carrier is gone WITHOUT having written its rc, writes it.
#
# It probes with `kill -0` — a permission/liveness check that sends no signal —
# against ONE pid, this shell's own. It never kills anything, so it has no
# interaction with lib/ownedkill.sh's ownership rules.
sg_carrier_watchdog() {
  [[ -n "$SG_CARRIER_RC_FILE" ]] || return 0
  local rc_file="$SG_CARRIER_RC_FILE" carrier=$$
  (
    # Detached from the run's stdout on purpose: holding the log pipe open would
    # keep `tee` alive after the run is over.
    while kill -0 "$carrier" 2>/dev/null; do
      sleep "$OC_SG_WATCHDOG_INTERVAL"
    done
    if [[ ! -f "$rc_file" ]]; then
      printf '%s\n' 137 > "$rc_file.watchdog.tmp" 2>/dev/null \
        && mv -f "$rc_file.watchdog.tmp" "$rc_file" 2>/dev/null
      printf '%s rc=%s reason=%s pid=%s\n' \
        "$(date -u '+%Y-%m-%dT%H:%M:%SZ')" 137 "vanished:carrier-pid-$carrier-died-untrapped" "$carrier" \
        >> "${rc_file%.rc}.status" 2>/dev/null || true
    fi
  ) >/dev/null 2>&1 &
  SG_CARRIER_WATCHDOG_PID=$!
  return 0
}

# sg_carrier_watchdog_stop — reap the watchdog once the rc file exists. Called
# from the caller's cleanup AFTER sg_carrier_write, so the watchdog always sees a
# written rc and stays quiet. Exact recorded pid only, like everything else here.
sg_carrier_watchdog_stop() {
  [[ -n "$SG_CARRIER_WATCHDOG_PID" ]] || return 0
  kill "$SG_CARRIER_WATCHDOG_PID" 2>/dev/null
  wait "$SG_CARRIER_WATCHDOG_PID" 2>/dev/null
  SG_CARRIER_WATCHDOG_PID=""
  return 0
}
