#!/usr/bin/env bash
# e2e_test/seven_gate/actors/live.sh — THE REAL AGENT END.
#
# 🔴 THIS ONE COSTS MONEY. It onboards a machine, runs a REAL `ocwarden run`, and
# the warden spawns a REAL claude process. That is the whole point — the stub
# proves the gate can READ the seven facts, and only this file can answer the
# question the gate exists for: does an agent that has read ONLY the boot
# context DECIDE to do the seven things? So it is DEFAULT-OFF twice over:
# run.sh's default actor is the stub, and this file refuses to do anything at
# all unless OC_SG_LIVE_AGENT is exactly "1" (strict, as e2e_test/CLAUDE.md
# requires of every spend switch — `true`/`yes`/`1 ` all fall to "did not run,
# did not spend").
#
#   OC_SG_LIVE_AGENT=1 OC_SG_ACTOR=actors/live.sh bash e2e_test/seven_gate/run.sh
#
# 🔴 NEVER RUN BY THE AUTHOR OF THIS FILE. It was written against the API
# contracts and against e2e_test/tests/05_machine_onboarding_spawn.live-agent.spec.js,
# which is the SAME chain (onboard → `ocwarden run` → activate → tmux+claude) and
# is the only place in this repo where that chain has actually been executed.
# The first person to press the button should expect to debug it, and every call
# it makes writes its status and body to http.log so that debugging is reading,
# not guessing.
#
# ── WHAT THIS FILE MAY AND MAY NOT DO ───────────────────────────────────────
#
# MAY: everything the OWNER does — hand the agent a job, put a machine under it,
# answer it, ask it the two friction questions. That is the counterparty, and a
# real agent cannot walk a task path with nobody on the other end.
#
# MAY NOT: anything the AGENT is supposed to decide. It does not open the
# ticket, does not file the plan, does not report a step, does not open the
# card, does not answer the colleague, does not report the close-out. If the
# agent does not do those, the run is red, and a red run here is the ANSWER,
# not a bug in this file.
# It also does not write the friction answers — see 〈friction〉 below.
#
# THE COLLEAGUE (⑦) NEEDS NOTHING FROM THIS FILE. run.sh seats a second member
# and has it speak first, BEFORE boot, so the colleague's message is simply part
# of the scene the agent wakes into — exactly like the owner's. Whether the
# agent answers a peer instead of only ever talking upward is the thing being
# measured, so nothing here nudges it.
#
# ── WHY THE JUDGE STILL CANNOT BE FOOLED ────────────────────────────────────
# This actor holds OC_SG_OWNER_TOKEN. It cannot forge one judged fact with it:
# ① is a self-report keyed to the CALLER's token (owner reporting waking stamps
# the OWNER), ② and ⑥ are matched on from == the agent, ③ on
# creator_id == the agent, and ④⑤⑦ hang off THAT task. The verdict is
# judge.py's, from the journal, exactly as for the stub.
set -uo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SG="$HERE/.."
. "$SG/lib/http.sh"
. "$SG/lib/friction.sh"
. "$SG/lib/window.sh"

say() { printf '[actor:live] %s\n' "$*"; }
die() { printf '[actor:live] FATAL: %s\n' "$*" >&2; exit 2; }

# ── the spend switch — FIRST, before anything else is even read. A bare
#    invocation must refuse cleanly, not die on a missing variable. ───────────
if [[ "${OC_SG_LIVE_AGENT:-}" != "1" ]]; then
  die "OC_SG_LIVE_AGENT is not exactly \"1\" (got '${OC_SG_LIVE_AGENT:-<unset>}').
This actor spawns a REAL claude and spends REAL API quota, so it does nothing
without a deliberate, exact opt-in. Nothing has been started and nothing has
been spent. Re-run with:
  OC_SG_LIVE_AGENT=1 OC_SG_ACTOR=actors/live.sh bash e2e_test/seven_gate/run.sh"
fi

BASE="${OC_SG_BASE:?}"
AGENT="${OC_SG_AGENT:?}"
OWNER_TOK="${OC_SG_OWNER_TOKEN:?live.sh needs the owner side; run.sh passes it}"
RUN_DIR="${OC_SG_RUN_DIR:?}"

export SG_BASE="$BASE" SG_TOKEN="$OWNER_TOK" SG_HTTP_TAG="actor:live/owner" \
       SG_HTTP_LOG="$RUN_DIR/http.log"

# ── preconditions, all of them refusals BEFORE anything is created ──────────
# Headless: this script must never reach a prompt. Everything it needs is
# resolved here and a missing piece is a loud refusal, never a wait.
# The runtime this run is pinned to (run.sh read it back off the member row, so
# it is the server's stored value, not a wish). Only the SELECTED runtime's
# binary is required: demanding claude on a codex run would refuse a run that
# has no use for claude — and the refusal would look like a codex problem.
RUNTIME="${OC_SG_RUNTIME:-claude}"
CLAUDE_BIN="${OC_CLAUDE_BIN:-$(command -v claude 2>/dev/null)}"
CODEX_BIN="${OC_CODEX_BIN:-$(command -v codex 2>/dev/null)}"
case "$RUNTIME" in
  claude)
    [[ -n "$CLAUDE_BIN" && -x "$CLAUDE_BIN" ]] \
      || die "runtime=claude but no claude binary (set OC_CLAUDE_BIN, or put claude on PATH) — the warden would spawn nothing." ;;
  codex)
    [[ -n "$CODEX_BIN" && -x "$CODEX_BIN" ]] \
      || die "runtime=codex but no codex binary (set OC_CODEX_BIN, or put codex on PATH) — the warden would spawn nothing."
    # The warden's own codex preflight (cli/ocwarden/spawn.go) refuses the spawn
    # when `codex login status` fails, and that refusal surfaces late, as a wake
    # timeout that reads like the agent's fault. Ask the same question here,
    # before anything is created, so a logged-out codex is a loud refusal.
    "$CODEX_BIN" login status >/dev/null 2>&1 \
      || die "runtime=codex but \`codex login status\` fails on this host — the warden's spawn preflight would refuse, and it would surface as a wake timeout that looks like the agent's fault. Log codex in first." ;;
  *)
    die "unsupported runtime '$RUNTIME' — the server's own vocabulary is claude|codex (api_helpers.go ValidRuntime)." ;;
esac
say "runtime=$RUNTIME  claude=${CLAUDE_BIN:-<none>}  codex=${CODEX_BIN:-<none>}"
command -v tmux >/dev/null 2>&1 || die "tmux is not on PATH — the warden spawns agents into tmux sessions."

REPO_ROOT="$(cd "$SG/../.." && pwd)"
OCWARDEN="${OC_SG_OCWARDEN:-$RUN_DIR/ocwarden}"
if [[ ! -x "$OCWARDEN" ]]; then
  GO_BIN="${OC_E2E_GO:-$(command -v go 2>/dev/null)}"
  [[ -n "$GO_BIN" ]] || die "no ocwarden at $OCWARDEN and no go on PATH to build one."
  say "building ocwarden → $OCWARDEN"
  (cd "$REPO_ROOT/cli/ocwarden" && "$GO_BIN" build -o "$OCWARDEN" .) \
    || die "ocwarden build failed."
fi

TMUX_SOCKET="${OC_SG_TMUX_SOCKET:-officraft}"   # cli/ocwarden/tmux.go tmuxSocket
SESSION="member-$(printf '%s' "$AGENT" | tr '[:upper:]' '[:lower:]')"

# ── teardown: EXACT names, EXACT pids, nothing pattern-matched ──────────────
# root CLAUDE.md §13: `pkill -f` / `killall` are banned here because the live
# fleet runs the same binaries with the same argv. We kill the ONE tmux session
# whose name we minted and the ONE warden pid we hold. run.sh's own trap takes
# the server down.
WARDEN_PID=""
live_cleanup() {
  if [[ -n "$SESSION" ]]; then
    say "killing tmux session $SESSION on socket $TMUX_SOCKET (exact name)"
    tmux -L "$TMUX_SOCKET" kill-session -t "$SESSION" 2>/dev/null
  fi
  if [[ -n "$WARDEN_PID" ]]; then
    say "killing warden pid $WARDEN_PID (exact pid)"
    kill "$WARDEN_PID" 2>/dev/null
  fi
}
trap live_cleanup EXIT

# ── 1. the job. Posted BEFORE the agent boots, so it is part of the scene the
#      agent wakes into. The wording lives in assignment.md and is sent
#      verbatim; read that file's header for why it must not name the seven
#      steps. ─────────────────────────────────────────────────────────────────
ASSIGNMENT="$(sed -n '/^## 交辦原文/,$p' "$SG/assignment.md" | sed '/^## /d' | sed '/./,$!d')"
[[ -n "$ASSIGNMENT" ]] || die "assignment.md yielded no 交辦原文 — refusing to boot an agent with nothing to do."
POSTED="$(sg_http POST /api/chat \
  "$(python3 -c 'import json,sys;print(json.dumps({"to":sys.argv[1],"body":sys.argv[2]}))' "$AGENT" "$ASSIGNMENT")")"
[[ -n "$POSTED" ]] || die "posting the assignment produced no response — see the [http] line above."
say "assignment posted to $AGENT (verbatim from assignment.md)"

# ── 2. a machine for it to run on ───────────────────────────────────────────
MACHINE_JSON="$(sg_http POST /api/machines '{"display_name":"seven-gate-live"}')" \
  || die "machine onboard was refused — see the [http] line above."
MACHINE="$(printf '%s' "$MACHINE_JSON" | python3 -c 'import sys,json;print(json.load(sys.stdin).get("machine_id",""))')"
MACHINE_TOK="$(printf '%s' "$MACHINE_JSON" | python3 -c 'import sys,json;print(json.load(sys.stdin).get("token",""))')"
[[ -n "$MACHINE" && -n "$MACHINE_TOK" ]] || die "onboard answered without a machine_id/token."
say "machine=$MACHINE"

# ── 3. a REAL warden, holding the SSE. `run`, not launchd install: this process
#      is ours, we hold its pid, and we kill exactly it. ────────────────────────
say "starting ocwarden run (this is the process that will spawn claude)"
# BOTH bins are handed over, not just the selected one: the warden resolves them
# independently (transport.go resolveClaudeBin / resolveCodexBin) and a machine
# may legitimately carry either or both. Passing only the one we happened to pin
# would make the warden fall back to PATH for the other — which under launchd is
# exactly the empty PATH that caused the historical boot-death.
env -u OC_WARDEN_TOKFILE \
    OC_BASE="$BASE" OC_TOKEN="$MACHINE_TOK" OC_ID="$MACHINE" \
    OC_CLAUDE_BIN="$CLAUDE_BIN" OC_CODEX_BIN="$CODEX_BIN" \
    "$OCWARDEN" run >>"$RUN_DIR/warden.log" 2>&1 &
WARDEN_PID=$!
say "warden pid=$WARDEN_PID → $RUN_DIR/warden.log"

ONLINE=""
for _ in $(seq 1 "$OC_SG_MACHINE_WAIT"); do
  sleep 1
  if sg_http GET /api/machines | python3 -c '
import sys, json
rows = json.load(sys.stdin)
rows = rows if isinstance(rows, list) else rows.get("machines", [])
sys.exit(0 if any(r.get("machine_id") == sys.argv[1] and r.get("online") for r in rows) else 1)
' "$MACHINE" 2>/dev/null; then ONLINE=1; break; fi
done
[[ -n "$ONLINE" ]] || die "the warden never came online — read $RUN_DIR/warden.log and the [http] lines. Nothing has been spawned, so nothing has been spent."
say "machine online"

# ── 4. put the agent on that machine. run.sh already wrote desired_state=online
#      (that is what makes ①'s waking projection derivable at all); this call
#      adds the PLACEMENT, which is what makes the reconcile actually dispatch a
#      START to the warden we just started. Owner-scope, so it is the owner's
#      act, not the agent's. ────────────────────────────────────────────────────
sg_http POST "/api/members/$AGENT/activate" "{\"machine_id\":\"$MACHINE\"}" >/dev/null \
  || die "activate onto $MACHINE was refused — see the [http] line above. Without a placement the warden is never told to start anything."

# ── 5. the spawn. From here on the agent is on its own: nothing below asks it to
#      do anything, and nothing below does anything on its behalf. ─────────────
SPAWNED=""
for _ in $(seq 1 "$OC_SG_SPAWN_WAIT"); do
  sleep 1
  if tmux -L "$TMUX_SOCKET" has-session -t "$SESSION" 2>/dev/null; then SPAWNED=1; break; fi
done
if [[ -z "$SPAWNED" ]]; then
  die "no tmux session '$SESSION' appeared — the warden accepted the START but nothing spawned. Read $RUN_DIR/warden.log."
fi
PANE_PID="$(tmux -L "$TMUX_SOCKET" list-panes -t "$SESSION" -F '#{pane_pid}' 2>/dev/null | head -1)"
say "spawned: tmux session $SESSION pane_pid=${PANE_PID:-<unknown>}"
[[ -n "$PANE_PID" ]] && ps -p "$PANE_PID" -o command= 2>/dev/null | sed 's/^/[actor:live] pane: /'

# ── 6. WAIT. This poll is a stopping condition, NOT a verdict — judge.py is the
#      verdict and it runs after this script exits. We stop early when the LAST
#      of the seven facts (⑦) is on the server, because continuing to bill for a
#      finished run is pure waste; otherwise we stop at the deadline and let the
#      judge report whatever did and did not land. ──────────────────────────────
DEADLINE=$(( $(date +%s) + $OC_SG_LIVE_WAIT ))
say "waiting for the agent to walk the path (deadline in $OC_SG_LIVE_WAITs; this is a stopping condition, not a verdict)"
while [[ "$(date +%s)" -lt "$DEADLINE" ]]; do
  sleep "${OC_SG_LIVE_POLL:-20}"
  MINE="$(sg_http GET /api/tasks | python3 -c '
import sys, json
d = json.load(sys.stdin)
rows = d if isinstance(d, list) else d.get("tasks", [])
print("\n".join(t["id"] for t in rows if t.get("creator_id") == sys.argv[1] and t.get("id")))
' "$AGENT" 2>/dev/null)"
  [[ -n "$MINE" ]] || { say "   …no task from the agent yet"; continue; }
  DONE=""
  for tid in $MINE; do
    if sg_http GET "/api/tasks/$tid" | python3 -c 'import sys,json;sys.exit(0 if json.load(sys.stdin).get("closeout_reported") else 1)' 2>/dev/null; then
      DONE="$tid"; break
    fi
  done
  if [[ -n "$DONE" ]]; then say "   ⑦'s fact is on the server (task $DONE) — stopping the wait"; break; fi
  say "   …agent has task(s) [$(printf '%s' "$MINE" | tr '\n' ' ')], close-out not reported yet"
done

# ── 7. friction. The two questions are put to the agent VERBATIM, straight out
#      of friction.md (lib/friction.sh is the only reader, so there is no second
#      copy to drift). Asked on a green run too.
#
#      🔴 THE HARNESS DOES NOT WRITE THE ANSWERS. friction.txt gets the agent's
#      own messages, byte for byte, or it gets a line saying the agent did not
#      answer. A summarised, tidied or invented answer would make this file the
#      author of the only part of the run that is not a server fact, and the
#      whole point of asking is that a human reads what the agent actually said.
FRICTION_TXT="$RUN_DIR/friction.txt"
ASK_TS="$(python3 -c 'import time;print(time.time())')"
QCOUNT=0
while IFS= read -r q; do
  [[ -n "$q" ]] || continue
  QCOUNT=$((QCOUNT + 1))
  sg_http POST /api/chat \
    "$(python3 -c 'import json,sys;print(json.dumps({"to":sys.argv[1],"body":sys.argv[2]}))' "$AGENT" "$q")" >/dev/null \
    || say "🔴 could not deliver a friction question — see the [http] line above."
  say "asked verbatim: $q"
done < <(sg_friction_questions "$SG/friction.md")
[[ "$QCOUNT" -gt 0 ]] || die "no friction questions were extracted — refusing to record an unasked follow-up."

say "collecting the agent's own answers (up to $OC_SG_FRICTION_WAITs)"
FR_DEADLINE=$(( $(date +%s) + $OC_SG_FRICTION_WAIT ))
ANSWERS=""
while [[ "$(date +%s)" -lt "$FR_DEADLINE" ]]; do
  sleep 15
  ANSWERS="$(sg_http GET "/api/chat?member_id=$AGENT" | python3 -c '
import sys, json
d = json.load(sys.stdin)
rows = d if isinstance(d, list) else d.get("messages", d.get("chat", []))
out = [m for m in rows
       if m.get("from") == sys.argv[1] and float(m.get("ts") or 0) > float(sys.argv[2])]
for m in out:
    print("--- %s  (chat %s)" % (m.get("ts"), m.get("id")))
    print(m.get("body") or "")
' "$AGENT" "$ASK_TS" 2>/dev/null)"
  [[ -n "$ANSWERS" ]] && break
done

{
  printf '# friction — %s\n' "$(date -u '+%Y-%m-%dT%H:%M:%SZ')"
  printf '# 問題逐字取自 seven_gate/friction.md，答案逐字取自 agent 自己發出的訊息。\n'
  printf '# 載體不代寫、不摘要、不評分。\n\n'
  printf '## 問了什麼（逐字）\n\n'
  sg_friction_questions "$SG/friction.md"
  printf '\n## agent 自己說了什麼（逐字）\n\n'
  if [[ -n "$ANSWERS" ]]; then
    printf '%s\n' "$ANSWERS"
  else
    printf '（沒有收到回答。等了 %ss，agent 在提問後沒有發出任何訊息。這一格空著就是空著——\n載體不會替它回答。）\n' "$OC_SG_FRICTION_WAIT"
  fi
} > "$FRICTION_TXT"
say "friction.txt written → $FRICTION_TXT"

say "done (rc of this script is NOT the verdict — judge.py is)"
