#!/usr/bin/env bash
# e2e_test/seven_gate/actors/stub.sh — the REPLACEABLE agent side.
#
# 🔴 THIS IS NOT AN AGENT AND MUST NEVER BE MISTAKEN FOR ONE. It walks the seven
# steps over REST with a member token, so it proves exactly one thing: that the
# gate reads the server correctly when the seven facts DO land. It proves NOTHING
# about whether a real agent, reading only the boot context, would decide to do
# any of them. That question is the entire reason this harness exists, and it is
# answered only by actors/live.sh.
#
# The actor contract — anything obeying it can be dropped in as $OC_SG_ACTOR:
#   in  (env): OC_SG_BASE OC_SG_AGENT OC_SG_AGENT_TOKEN OC_SG_SCENE_NONCE
#              OC_SG_RUN_DIR OC_SG_OWNER
#   out:       whatever it can make happen on the server. rc is logged, NOT
#              judged — an actor that exits 0 having done nothing must still go
#              red, and it does, because the verdict comes from the server.
#
# 🔴 EVERY CALL GOES THROUGH sg_http (../lib/http.sh) AND NOTHING IS DISCARDED.
# The first baseline of this gate wrote every call as `curl … >/dev/null`, went
# red on ①⑤⑦, and left no way to tell a refused call from an accepted one —
# which is what turned three plain API-contract mistakes into a blind hunt. The
# status code and the body of every call now land in run.log and http.log.
#
# THE THREE CONTRACT FACTS THIS SCRIPT GOT WRONG THE FIRST TIME, all of them
# server rules that were readable in the source the whole time:
#   ⑤ a step goes pending → in_progress → done. `pending → done` is not a legal
#     agent transition (domain.go agentStepTransitions) and answers 409, so the
#     old one-shot "report done" moved nothing.
#   ⑥ a card opened by the executor of an ACTIVE task auto-binds to that task's
#     CURRENT step (api_replycards.go) — and there must BE one: with no step
#     in_progress the create is refused 409. So ⑥ comes while a step is running,
#     and the owner side answers it (run.sh) to release the waiting_owner hold.
#   ⑦ closeout is TERMINAL-tasks-only (api_tasks.go). A task is terminal when
#     its steps derive it there — so the last step must actually reach done
#     first, and the handoff declaration rides THAT call, not the closeout.
#
# OC_SG_SKIP_STEP=<key> makes exactly one step NOT happen (keys as in judge.py:
# report_waking resume_scene create_task submit_plan step_done reply_card
# closeout). That is how a run proves the gate can still fail: a harness only
# ever exercised on a passing run is a harness nobody has seen say no.
set -uo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
. "$HERE/../lib/http.sh"

BASE="${OC_SG_BASE:?}"
AGENT="${OC_SG_AGENT:?}"
TOK="${OC_SG_AGENT_TOKEN:?}"
NONCE="${OC_SG_SCENE_NONCE:?}"
OWNER="${OC_SG_OWNER:-owner}"
RUN_DIR="${OC_SG_RUN_DIR:-}"
SKIP="${OC_SG_SKIP_STEP:-}"

export SG_BASE="$BASE" SG_TOKEN="$TOK" SG_HTTP_TAG="actor:stub" \
       SG_HTTP_LOG="${RUN_DIR:+$RUN_DIR/http.log}"

say() { printf '[actor:stub] %s\n' "$*"; }
skipped() { [[ "$SKIP" == "$1" ]] && { say "SKIPPING $1 (OC_SG_SKIP_STEP)"; return 0; } || return 1; }

jget() { python3 -c 'import sys,json;d=json.load(sys.stdin);
ks=sys.argv[1].split(".")
for k in ks:
    d = d[int(k)] if isinstance(d, list) else d.get(k, "")
    if d == "": break
print(d if isinstance(d,str) else json.dumps(d))' "$1" 2>/dev/null; }

# step_id_at INDEX — re-read the task and pull the id of the Nth plan step.
step_id_at() { sg_http GET "/api/tasks/$TASK" | jget "steps.$1.id"; }

# ① 報到 — the only step whose fact is transient, so nothing else may run first.
# The owner's desired_state=online is already written (run.sh step 2b); without
# it this report stamps waking_since and the presence projection stays offline.
skipped report_waking || sg_step report_waking POST /api/self/waking '{"model":"stub-actor"}' >/dev/null

# ② 接回現場 — pull the resume snapshot and quote the planted nonce back into a
# server-stored message. A real agent does this because the boot context tells
# it to; the stub does it because that is the fact the gate reads.
if ! skipped resume_scene; then
  say "② resume_summary → quote nonce"
  SNAP="$(sg_http GET /api/resume-summary)"
  if ! printf '%s' "$SNAP" | grep -qF "$NONCE"; then
    say "WARNING: the resume snapshot does NOT carry the scene nonce. The plant"
    say "and the snapshot have drifted apart — step ② would go red for a harness"
    say "reason, not an agent reason. Fix run.sh's plant before trusting a red."
  fi
  sg_step resume_scene POST /api/chat "$(python3 -c 'import json,sys;print(json.dumps({"to":sys.argv[1],"body":"接回現場：從 resume_summary 讀回本現場標記 "+sys.argv[2]}))' "$OWNER" "$NONCE")" >/dev/null
fi

# ③ 開票
TASK=""
if ! skipped create_task; then
  TASK="$(sg_step create_task POST /api/tasks "$(python3 -c 'import json,sys;print(json.dumps({"title":"seven-gate probe "+sys.argv[1],"description":"七步關卡的載體任務（stub actor）。scene="+sys.argv[1],"executor_member_id":sys.argv[2]}))' "$NONCE" "$AGENT")" | jget task.id)"
  [[ -n "$TASK" ]] || TASK="$(sg_http GET /api/tasks | python3 -c 'import sys,json;d=json.load(sys.stdin);ts=d.get("tasks",d) if isinstance(d,dict) else d;print(next((t["id"] for t in ts if t.get("creator_id")==sys.argv[1]),""))' "$AGENT")"
  say "   task=$TASK"
fi

# ④ 提出計畫 — two steps: one to finish under ⑤, one to carry ⑥'s card and the
# close under ⑦.
if [[ -n "$TASK" ]] && ! skipped submit_plan; then
  sg_step submit_plan POST "/api/tasks/$TASK/plan" \
    '{"steps":[{"name":"走完七步","dod":"七個 server 事實都在"},{"name":"回報收尾","dod":"closeout_reported=true"}]}' >/dev/null
fi

# ⑤ 報一步完成 — pending → in_progress → done, in that order. The old code sent
# done straight at a pending step and got a 409 it then threw away.
if [[ -n "$TASK" ]] && ! skipped step_done; then
  SID="$(step_id_at 0)"
  if [[ -z "$SID" ]]; then
    say "🔴 ⑤ cannot run: task $TASK has no step 0 (④ did not land) — see the [http] lines above."
  else
    sg_step step_done_start POST "/api/tasks/$TASK/steps/$SID/status" '{"status":"in_progress"}' >/dev/null
    sg_step step_done       POST "/api/tasks/$TASK/steps/$SID/status" '{"status":"done"}' >/dev/null
  fi
fi

# ⑥ 開一張等我回覆卡 — the card auto-binds to the step that is in_progress, so
# the second step is started FIRST. That is not a trick to satisfy the server:
# it is what "我做到這裡，需要你裁示" actually is. The step parks in
# waiting_owner until run.sh's owner side answers, which is why ⑦ waits below.
if [[ -n "$TASK" ]] && ! skipped reply_card; then
  SID2="$(step_id_at 1)"
  [[ -n "$SID2" ]] && sg_step reply_card_start POST "/api/tasks/$TASK/steps/$SID2/status" '{"status":"in_progress"}' >/dev/null
  sg_step reply_card POST /api/reply-cards \
    '{"kind":"decision","summary":"七步關卡:要不要繼續收尾?","body":"這是載體用的請示卡。","options":["繼續收尾","先停在這裡"]}' >/dev/null
fi

# ⑦ 回報收尾 — wait for the owner to answer (the server restores the step to
# in_progress when the card is answered), finish the last step with the handoff
# declared IN THAT CALL (T-74f8 交棒閘), which derives the task to done — and
# only a terminal task may report closeout.
if [[ -n "$TASK" ]] && ! skipped closeout; then
  SID2="$(step_id_at 1)"
  if [[ -z "$SID2" ]]; then
    say "🔴 ⑦ cannot run: task $TASK has no step 1 — see the [http] lines above."
  else
    ST=""
    for _ in $(seq 1 "${OC_SG_CARD_WAIT:-30}"); do
      ST="$(sg_http GET "/api/tasks/$TASK" | jget "steps.1.status")"
      [[ "$ST" == "waiting_owner" ]] || break
      say "   ⑦ waiting for the owner to answer the card (step is waiting_owner)…"
      sleep 2
    done
    # A step only reaches done FROM in_progress. It is already there on the
    # normal path (⑥ started it, the answer restored it); this covers the
    # OC_SG_SKIP_STEP runs, where ⑥ never ran and the step is still pending —
    # so a skipped ⑥ reddens ⑥ ALONE instead of dragging ⑦ down with it and
    # making the verdict point at the wrong step.
    if [[ "$ST" == "pending" ]]; then
      sg_step closeout_start POST "/api/tasks/$TASK/steps/$SID2/status" '{"status":"in_progress"}' >/dev/null
    fi
    sg_step closeout_last_step POST "/api/tasks/$TASK/steps/$SID2/status" \
      '{"status":"done","handoff":"none","handoff_note":"載體 run,無後續"}' >/dev/null
    sg_step closeout POST "/api/tasks/$TASK/closeout" '{}' >/dev/null
  fi
fi

say "done (rc of this script is NOT the verdict — judge.py is)"
