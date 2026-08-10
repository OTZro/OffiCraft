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
. "$HERE/lib/http.sh"
. "$HERE/lib/friction.sh"

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
RESPONDER_PID=""
# EXACT PIDs only, never a name pattern: this harness's serve is the same binary
# with the same argv as the live one, so `pkill -f` here would take the fleet
# down with it (root CLAUDE.md §13).
cleanup() {
  [[ -n "$RESPONDER_PID" ]] && kill "$RESPONDER_PID" 2>/dev/null
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
# Every owner-side call goes through sg_http, which writes the status code and
# the body to run.log AND to http.log. Nothing here is allowed to end in
# `>/dev/null` — that is precisely what made the first baseline unreadable.
export SG_BASE="$BASE" SG_TOKEN="$OWNER_TOK" SG_HTTP_TAG="owner" \
       SG_HTTP_LOG="$RUN_DIR/http.log"

# 2. a fresh agent for this run. Fresh on purpose: the whole question is what a
#    NEW agent does with the boot context, and a reused member arrives already
#    knowing things the boot context never taught it.
AGENT_NAME="sg-$STAMP"
AGENT="$(sg_http POST /api/members "{\"name\":\"$AGENT_NAME\",\"role_key\":\"assistant\"}" \
  | python3 -c 'import sys,json;d=json.load(sys.stdin);print(d.get("id") or d.get("member",{}).get("id",""))')"
[[ -n "$AGENT" ]] || { echo "[seven_gate] FATAL: hire failed — cannot judge a run with no agent." >&2; exit 2; }
AGENT_TOK="$(sg_http POST /api/mint "{\"member_id\":\"$AGENT\",\"ttl_days\":1}" \
  | python3 -c 'import sys,json;print(json.load(sys.stdin).get("token",""))')"
[[ -n "$AGENT_TOK" ]] || { echo "[seven_gate] FATAL: mint failed for $AGENT." >&2; exit 2; }
echo "[seven_gate] agent=$AGENT ($AGENT_NAME)"

# 2b. THE OWNER'S INTENT, and ① cannot be observed without it. presence=waking is
#     derived (server/ocserverd/domain.go PresenceState) from desired_state ==
#     online AND a fresh waking_since — BOTH, not either. A freshly hired member
#     is desired_state=offline, so its report_waking stamps waking_since and the
#     projection still reads `offline`: the first baseline's ① went red here,
#     with the report itself answering a clean 200. This is the owner switching
#     the member on, which is the owner's act and no part of the seven steps —
#     it belongs on this side of the actor boundary, not in the actor.
#     Order matters: activate ZEROES waking_since (api_members.go), so it must
#     precede the agent's boot report, never follow it.
#     (the body is captured rather than discarded — see lib/http.sh's header:
#     no server call in this harness may end in >/dev/null.)
ACTIVATED="$(sg_http POST "/api/members/$AGENT/activate" '{}')"
[[ -n "$ACTIVATED" ]] || echo "[seven_gate] WARNING: activate returned an empty body — check the [http] line above."
echo "[seven_gate] owner intent: desired_state=online (waking is derivable from here on)"

# 3. PLANT THE SCENE. ②'s fact cannot be read directly — resume_summary is a GET
#    and stamps nothing — so the scene carries a nonce that ONLY the resume
#    snapshot surfaces, and ② passes iff the agent quotes it back. Planted as an
#    owner→agent chat message BEFORE boot, which is exactly what "接回現場"
#    means: something was already here.
NONCE="sg-nonce-$(od -An -tx1 -N6 /dev/urandom | tr -d ' \n')"
PLANTED="$(sg_http POST /api/chat "$(python3 -c 'import json,sys;print(json.dumps({"to":sys.argv[1],"body":"【上一班留下的現場】本現場標記 "+sys.argv[2]+" — 接回現場後請把它原樣帶回來。"}))' "$AGENT" "$NONCE")")"
[[ -n "$PLANTED" ]] || { echo "[seven_gate] FATAL: planting the scene message produced no response — ② could only ever be red, and red for a HARNESS reason. Read the [http] line above." >&2; exit 2; }

# 3b. SEAT A COLLEAGUE. The six steps above all point at the OWNER — chat to the
#     owner, a card for the owner, a task the owner watches. Talking to another
#     AGENT is a different act with a different recipient and nobody patient on
#     the other end, and the boot context has to teach it too (owner, after the
#     first baseline: 「包含 chat / reply card / task」「他要知道怎麼透過這三個元件
#     跟 owner 溝通」「或是跟其他 agent 溝通」).
#     The peer SPEAKS FIRST, carrying its own nonce, so the fact the gate reads
#     is a REPLY and not a broadcast: something addressed to the colleague that
#     shows the colleague's message was actually read.
PEER_NAME="sg-peer-$STAMP"
PEER="$(sg_http POST /api/members "{\"name\":\"$PEER_NAME\",\"role_key\":\"assistant\"}" \
  | python3 -c 'import sys,json;d=json.load(sys.stdin);print(d.get("id") or d.get("member",{}).get("id",""))')"
[[ -n "$PEER" ]] || { echo "[seven_gate] FATAL: could not hire the peer agent — ⑦ could only ever be red, and red for a HARNESS reason." >&2; exit 2; }
PEER_TOK="$(sg_http POST /api/mint "{\"member_id\":\"$PEER\",\"ttl_days\":1}" \
  | python3 -c 'import sys,json;print(json.load(sys.stdin).get("token",""))')"
[[ -n "$PEER_TOK" ]] || { echo "[seven_gate] FATAL: mint failed for the peer $PEER." >&2; exit 2; }
PEER_NONCE="sg-peer-nonce-$(od -An -tx1 -N6 /dev/urandom | tr -d ' \n')"
# Sent AS THE PEER, from the peer's own token — an owner-sent message would be
# the owner talking, which is the half ② and ⑥ already cover.
PEER_SAID="$(SG_TOKEN="$PEER_TOK" SG_HTTP_TAG="peer" sg_http POST /api/chat \
  "$(python3 -c 'import json,sys;print(json.dumps({"to":sys.argv[1],"body":"我是隔壁的 "+sys.argv[2]+"。我這邊在等你那條線的結果——你看完現場後直接回我一句，把這個記號帶上："+sys.argv[3]}))' "$AGENT" "$PEER_NAME" "$PEER_NONCE")")"
[[ -n "$PEER_SAID" ]] || { echo "[seven_gate] FATAL: the peer's opening message did not land — see the [http] line above." >&2; exit 2; }
echo "[seven_gate] peer=$PEER ($PEER_NAME) spoke first: $PEER_NONCE"


# 3c. PLANT THE PICTURE. ⑨ asks one thing: can it SEE. So a number is drawn into
#     a PNG and that number exists NOWHERE ELSE — not in the body, not in the
#     filename, not in the mime, not in a task, not in any file the agent can
#     open. If it ever leaks into text, an agent that cannot see passes, and
#     THAT PASS IS INDISTINGUISHABLE FROM THE REAL ONE. Hence the scan below.
#     Regenerated per run and never 42: a hard-coded answer is one a model can
#     have memorised, and a cell that can be passed from memory measures nothing.
IMG_ANSWER="$(python3 -c 'import random;print("".join(random.choice("0123456789") for _ in range(6)))')"
IMG_PATH="$RUN_DIR/scene-image.png"
python3 "$HERE/make_answer_png.py" "$IMG_ANSWER" "$IMG_PATH" >/dev/null \
  || { echo "[seven_gate] FATAL: could not draw the scene image." >&2; exit 2; }
# The body names the picture and asks for the number; it must NOT contain it.
# Same for the filename — a filename is text, and text is exactly what this cell
# is trying to rule out as a source.
IMG_POSTED="$(python3 - "$AGENT" "$IMG_PATH" <<'PY'
import base64, json, sys
agent, path = sys.argv[1], sys.argv[2]
print(json.dumps({
    "to": agent,
    "body": "【交接圖】上一班把要用的號碼寫在這張圖上了，沒有留文字版。看一下圖，把上面那個號碼原樣回我。",
    "attachments": [{
        "filename": "handover-note.png",
        "mime": "image/png",
        "data_b64": base64.b64encode(open(path, "rb").read()).decode("ascii"),
    }],
}, ensure_ascii=False))
PY
)"
IMG_RESP="$(SG_HTTP_TAG="owner:image" sg_http POST /api/chat "$IMG_POSTED")"
[[ -n "$IMG_RESP" ]] || { echo "[seven_gate] FATAL: the picture did not land — ⑨ could only ever be red, and red for a HARNESS reason." >&2; exit 2; }
echo "[seven_gate] picture planted (the number is in the pixels only)"

# 3d. THE LEAK SCAN — the cell's whole validity in one check. Everything the
#     agent can READ AS TEXT is pulled back off the server and searched for the
#     answer; a single hit means a text-only agent could pass, so the run
#     REFUSES rather than producing a green nobody can trust.
#     A POSITIVE CONTROL runs first: the same scanner looks for the scene nonce,
#     which we KNOW is in the text. If the control finds nothing, the scanner is
#     broken and "zero hits" would be meaningless — so that is a refusal too.
scan_scene_text() { # scan_scene_text NEEDLE -> prints hit count
  local needle="$1" hits=0 hay
  for p in "/api/chat?limit=500" "/api/tasks" "/api/reply-cards?status=waiting" \
           "/api/reply-cards?status=answered" "/api/members"; do
    hay="$(SG_HTTP_TAG="owner:leakscan" sg_http GET "$p" 2>/dev/null)"
    hits=$(( hits + $(printf '%s' "$hay" | grep -o -F "$needle" | wc -l | tr -d ' ') ))
  done
  # The agent's own wake snapshot — the one surface assembled FOR it.
  hay="$(SG_TOKEN="$AGENT_TOK" SG_HTTP_TAG="owner:leakscan" sg_http GET /api/resume-summary 2>/dev/null)"
  hits=$(( hits + $(printf '%s' "$hay" | grep -o -F "$needle" | wc -l | tr -d ' ') ))
  printf '%s' "$hits"
}
CONTROL_HITS="$(scan_scene_text "$NONCE")"
if [[ "${CONTROL_HITS:-0}" -lt 1 ]]; then
  echo "[seven_gate] FATAL: the leak scanner's POSITIVE CONTROL found 0 hits for the scene nonce, which IS in the text. The scanner is broken, so a clean answer-scan would mean nothing. Refusing to run." >&2
  exit 2
fi
LEAK_HITS="$(scan_scene_text "$IMG_ANSWER")"
if [[ "${LEAK_HITS:-0}" -ne 0 ]]; then
  echo "[seven_gate] FATAL: the image's number appears $LEAK_HITS time(s) in TEXT the agent can read. ⑨ would be passable without ever opening the picture, and that pass would look exactly like a real one. Refusing to run." >&2
  exit 2
fi
echo "[seven_gate] leak scan: answer 0 hits in readable text (positive control: scene nonce $CONTROL_HITS hit(s) — the scanner works)"

python3 -c 'import json,sys;json.dump({"agent_id":sys.argv[1],"scene_nonce":sys.argv[2],"stamp":sys.argv[3],"peer_id":sys.argv[5],"peer_nonce":sys.argv[6],"image_answer":sys.argv[7]},open(sys.argv[4],"w"),ensure_ascii=False,indent=2)' \
  "$AGENT" "$NONCE" "$STAMP" "$RUN_DIR/scene.json" "$PEER" "$PEER_NONCE" "$IMG_ANSWER"
echo "[seven_gate] scene planted: $NONCE"

# 4. collector FIRST — ①'s presence=waking is gone within seconds of the agent
#    mounting SSE, so a collector started after the actor reads a green run red.
python3 "$HERE/collect.py" --base "$BASE" --token-file "$E2E/.state/owner.tok" \
  --agent "$AGENT" --run-dir "$RUN_DIR" --interval "${OC_SG_INTERVAL:-1}" \
  --seconds "${OC_SG_MAX_SECONDS:-900}" >>"$RUN_DIR/collect.log" 2>&1 &
COLLECTOR_PID=$!
sleep 2

# 4b. THE OWNER ON THE OTHER END OF ⑥'s CARD. A card opened by the executor of an
#     active task AUTO-BINDS to that task's current step and parks it in
#     waiting_owner (api_replycards.go inferCardTaskStep → armStepWithCard), and
#     waiting_owner has exactly ONE exit: the owner answers. Without someone on
#     that end, ⑥ succeeding is what makes ⑦ impossible — the step can never
#     move again, the task can never close, and closeout is terminal-only. That
#     is not a harness convenience; it is who the counterparty is. It answers
#     only cards this run's agent opened, on this run's isolated server.
#     It answers CARDS. It never answers the friction questions — those are the
#     agent's own words or they are nothing (see 〈friction〉 in CLAUDE.md).
(
  SG_HTTP_TAG="owner:cards"
  while :; do
    _cards="$(sg_http GET '/api/reply-cards?status=waiting')" || _cards=""
    for _cid in $(printf '%s' "$_cards" | python3 -c '
import sys, json
try:
    d = json.load(sys.stdin)
except Exception:
    sys.exit(0)
rows = d.get("cards", d) if isinstance(d, dict) else d
for c in rows if isinstance(rows, list) else []:
    if c.get("from") == sys.argv[1] and c.get("id"):
        print(c["id"])
' "$AGENT" 2>/dev/null); do
      sg_http POST "/api/reply-cards/$_cid/answer" \
        '{"option_idx":0,"text":"（七步關卡的 owner 端）就照你列的第一個選項辦，做完照常回報收尾。"}' \
        || true
    done
    sleep "${OC_SG_ANSWER_INTERVAL:-2}"
  done
) &
RESPONDER_PID=$!
echo "[seven_gate] owner card-responder pid=$RESPONDER_PID (answers ONLY $AGENT's cards)"

# 5. the actor. Its rc is recorded and deliberately not acted on.
#    OC_SG_OWNER_TOKEN is the COUNTERPARTY's token, and it is in the contract
#    because a real agent needs an owner on the other end: someone has to ask it
#    to do something, and someone has to put the friction questions to it
#    afterwards. It CANNOT forge a single judged fact — ① is a self-report keyed
#    to the caller's own token, ②'s message and ⑥'s card are matched on
#    from==agent, ③'s task on creator_id==agent, and ④⑤⑦ hang off THAT task —
#    so an actor holding it still cannot make a red run look green.
OC_SG_BASE="$BASE" OC_SG_AGENT="$AGENT" OC_SG_AGENT_TOKEN="$AGENT_TOK" \
OC_SG_SCENE_NONCE="$NONCE" OC_SG_RUN_DIR="$RUN_DIR" OC_SG_OWNER="owner" \
OC_SG_OWNER_TOKEN="$OWNER_TOK" OC_SG_PEER="$PEER" OC_SG_PEER_NONCE="$PEER_NONCE" \
OC_SG_IMAGE_ANSWER="$IMG_ANSWER" \
  bash "$ACTOR" 2>&1 | tee "$RUN_DIR/actor.log"
echo "[seven_gate] actor rc=${PIPESTATUS[0]} (recorded, not judged)"

# 6. one last settle, then stop collecting and judge what the server held.
sleep "${OC_SG_SETTLE:-3}"
kill "$RESPONDER_PID" 2>/dev/null; wait "$RESPONDER_PID" 2>/dev/null
RESPONDER_PID=""
kill "$COLLECTOR_PID" 2>/dev/null; wait "$COLLECTOR_PID" 2>/dev/null
COLLECTOR_PID=""
python3 "$HERE/judge.py" "$RUN_DIR"; RC=$?
printf '%s\n' "$RC" > "$RUN_DIR/rc"

# 7. the friction questions, verbatim from the one file that holds them. Asked on
#    green runs too — the gate knows whether the fact landed, never whether the
#    agent got there by guessing.
echo
sg_friction_questions "$HERE/friction.md"
echo "[seven_gate] ↑ ask these two, verbatim; paste the answers into $RUN_DIR/friction.txt"
echo "[seven_gate] artifacts: $RUN_DIR (run.log actor.log collect.log http.log journal.ndjson scene.json verdict.json rc)"
echo "[seven_gate] every server call this run made, with its HTTP status and body: $RUN_DIR/http.log"
exit "$RC"
