#!/usr/bin/env bash
# e2e_test/seven_gate/run.sh — one seven-step run, end to end.
#
#   setup (isolated :8791) → hire the agent → PLANT the scene → start the
#   collector → run the actor → stop the collector → judge → emit the friction
#   questions → teardown
#
# Read seven_gate/CLAUDE.md before changing anything here.
#
#   bash e2e_test/seven_gate/run.sh                          # stub actor, current seeds
#   OC_SEEDS_SRC=/tmp/candidate-seeds bash …/run.sh          # candidate boot context
#   OC_SG_SKIP_STEP=reply_card bash …/run.sh                 # prove the gate can say no
#   OC_SG_ACTOR=actors/live.sh bash …/run.sh                 # 🔴 real agent — burns quota
#
# 🔴 DEFAULT-OFF ON THE ONE THING THAT COSTS MONEY: the default actor is the
# stub, which spawns nothing. Nothing in this file starts a claude process, and
# an actor that would must say so in its own header. This script is NOT wired
# into run_all.sh and NOT into bin/ci.sh — the JUDGE is what CI guards
# (tests_guard case 21), and the judge needs no server.
set -uo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
E2E="$HERE/.."
. "$E2E/lib/common.sh"

ACTOR="${OC_SG_ACTOR:-$HERE/actors/stub.sh}"
[[ "$ACTOR" = /* ]] || ACTOR="$HERE/$ACTOR"
[[ -f "$ACTOR" ]] || { echo "[seven_gate] FATAL: no actor at $ACTOR" >&2; exit 2; }

STAMP="$(date -u '+%Y%m%dT%H%M%SZ')"
RUN_DIR="${OC_SG_RUN_DIR:-$HERE/runs/$STAMP}"
mkdir -p "$RUN_DIR"
LOG="$RUN_DIR/run.log"
exec > >(tee -a "$LOG") 2>&1
echo "[seven_gate] run $STAMP → $RUN_DIR  (actor=$(basename "$ACTOR")  OC_SEEDS_SRC=${OC_SEEDS_SRC:-<repo seeds/>})"

COLLECTOR_PID=""
cleanup() {
  [[ -n "$COLLECTOR_PID" ]] && kill "$COLLECTOR_PID" 2>/dev/null
  bash "$E2E/teardown.sh" || true
}
trap cleanup EXIT

# 1. isolated server. setup.sh re-stages seedsdist through bin/build-seedsdist on
#    every run, and that script honours OC_SEEDS_SRC — so a candidate boot
#    context is one env var away and the tracked seeds/ is never touched.
bash "$E2E/setup.sh"
. "$E2E/.state/env"
BASE="${OC_E2E_BASE}"
OWNER_TOK="$(cat "$E2E/.state/owner.tok")"
oapi() { curl -sS -X "$1" "$BASE$2" -H "Authorization: Bearer $OWNER_TOK" \
              -H 'Content-Type: application/json' ${3:+-d "$3"}; }

# 2. a fresh agent for this run. Fresh on purpose: the whole question is what a
#    NEW agent does with the boot context, and a reused member arrives already
#    knowing things the boot context never taught it.
AGENT_NAME="sg-$STAMP"
AGENT="$(oapi POST /api/members "{\"name\":\"$AGENT_NAME\",\"role_key\":\"assistant\"}" \
  | python3 -c 'import sys,json;d=json.load(sys.stdin);print(d.get("id") or d.get("member",{}).get("id",""))')"
[[ -n "$AGENT" ]] || { echo "[seven_gate] FATAL: hire failed — cannot judge a run with no agent." >&2; exit 2; }
AGENT_TOK="$(oapi POST /api/mint "{\"member_id\":\"$AGENT\",\"ttl_days\":1}" \
  | python3 -c 'import sys,json;print(json.load(sys.stdin).get("token",""))')"
[[ -n "$AGENT_TOK" ]] || { echo "[seven_gate] FATAL: mint failed for $AGENT." >&2; exit 2; }
echo "[seven_gate] agent=$AGENT ($AGENT_NAME)"

# 3. PLANT THE SCENE. ②'s fact cannot be read directly — resume_summary is a GET
#    and stamps nothing — so the scene carries a nonce that ONLY the resume
#    snapshot surfaces, and ② passes iff the agent quotes it back. Planted as an
#    owner→agent chat message BEFORE boot, which is exactly what "接回現場"
#    means: something was already here.
NONCE="sg-nonce-$(od -An -tx1 -N6 /dev/urandom | tr -d ' \n')"
oapi POST /api/chat "$(python3 -c 'import json,sys;print(json.dumps({"to":sys.argv[1],"body":"【上一班留下的現場】本現場標記 "+sys.argv[2]+" — 接回現場後請把它原樣帶回來。"}))' "$AGENT" "$NONCE")" >/dev/null
python3 -c 'import json,sys;json.dump({"agent_id":sys.argv[1],"scene_nonce":sys.argv[2],"stamp":sys.argv[3]},open(sys.argv[4],"w"),ensure_ascii=False,indent=2)' \
  "$AGENT" "$NONCE" "$STAMP" "$RUN_DIR/scene.json"
echo "[seven_gate] scene planted: $NONCE"

# 4. collector FIRST — ①'s presence=waking is gone within seconds of the agent
#    mounting SSE, so a collector started after the actor reads a green run red.
python3 "$HERE/collect.py" --base "$BASE" --token-file "$E2E/.state/owner.tok" \
  --agent "$AGENT" --run-dir "$RUN_DIR" --interval "${OC_SG_INTERVAL:-1}" \
  --seconds "${OC_SG_MAX_SECONDS:-900}" >>"$RUN_DIR/collect.log" 2>&1 &
COLLECTOR_PID=$!
sleep 2

# 5. the actor. Its rc is recorded and deliberately not acted on.
OC_SG_BASE="$BASE" OC_SG_AGENT="$AGENT" OC_SG_AGENT_TOKEN="$AGENT_TOK" \
OC_SG_SCENE_NONCE="$NONCE" OC_SG_RUN_DIR="$RUN_DIR" OC_SG_OWNER="owner" \
  bash "$ACTOR" 2>&1 | tee "$RUN_DIR/actor.log"
echo "[seven_gate] actor rc=${PIPESTATUS[0]} (recorded, not judged)"

# 6. one last settle, then stop collecting and judge what the server held.
sleep "${OC_SG_SETTLE:-3}"
kill "$COLLECTOR_PID" 2>/dev/null; wait "$COLLECTOR_PID" 2>/dev/null
COLLECTOR_PID=""
python3 "$HERE/judge.py" "$RUN_DIR"; RC=$?

# 7. the friction questions, verbatim from the one file that holds them. Asked on
#    green runs too — the gate knows whether the fact landed, never whether the
#    agent got there by guessing.
echo
sed -n '/^## 逐字問句/,/^## 怎麼用/p' "$HERE/friction.md" | sed '/^## /d;/^$/d'
echo "[seven_gate] ↑ ask these two, verbatim; paste the answers into $RUN_DIR/friction.txt"
echo "[seven_gate] artifacts: $RUN_DIR (run.log actor.log collect.log journal.ndjson scene.json verdict.json)"
exit "$RC"
