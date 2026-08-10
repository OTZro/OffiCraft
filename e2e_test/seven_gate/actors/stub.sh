#!/usr/bin/env bash
# e2e_test/seven_gate/actors/stub.sh — the REPLACEABLE agent side.
#
# 🔴 THIS IS NOT AN AGENT AND MUST NEVER BE MISTAKEN FOR ONE. It walks the seven
# steps over REST with a member token, so it proves exactly one thing: that the
# gate reads the server correctly when the seven facts DO land. It proves NOTHING
# about whether a real agent, reading only the boot context, would decide to do
# any of them. That question is the entire reason this harness exists, and it is
# answered only by actors/live.sh (not written yet — see CLAUDE.md 〈界線〉).
#
# The actor contract — anything obeying it can be dropped in as $OC_SG_ACTOR:
#   in  (env): OC_SG_BASE OC_SG_AGENT OC_SG_AGENT_TOKEN OC_SG_SCENE_NONCE
#              OC_SG_RUN_DIR OC_SG_OWNER
#   out:       whatever it can make happen on the server. rc is logged, NOT
#              judged — an actor that exits 0 having done nothing must still go
#              red, and it does, because the verdict comes from the server.
#
# OC_SG_SKIP_STEP=<key> makes exactly one step NOT happen (keys as in judge.py:
# report_waking resume_scene create_task submit_plan step_done reply_card
# closeout). That is how a run proves the gate can still fail: a harness only
# ever exercised on a passing run is a harness nobody has seen say no.
set -uo pipefail

BASE="${OC_SG_BASE:?}"
AGENT="${OC_SG_AGENT:?}"
TOK="${OC_SG_AGENT_TOKEN:?}"
NONCE="${OC_SG_SCENE_NONCE:?}"
OWNER="${OC_SG_OWNER:-owner}"
SKIP="${OC_SG_SKIP_STEP:-}"

say() { printf '[actor:stub] %s\n' "$*"; }
skipped() { [[ "$SKIP" == "$1" ]] && { say "SKIPPING $1 (OC_SG_SKIP_STEP)"; return 0; } || return 1; }

api() { # api METHOD PATH [JSON]
  local m="$1" p="$2" d="${3:-}"
  if [[ -n "$d" ]]; then
    curl -sS -X "$m" "$BASE$p" -H "Authorization: Bearer $TOK" \
         -H 'Content-Type: application/json' -d "$d"
  else
    curl -sS -X "$m" "$BASE$p" -H "Authorization: Bearer $TOK"
  fi
}
jget() { python3 -c 'import sys,json;d=json.load(sys.stdin);
ks=sys.argv[1].split(".")
for k in ks:
    d = d[int(k)] if isinstance(d, list) else d.get(k, "")
    if d == "": break
print(d if isinstance(d,str) else json.dumps(d))' "$1"; }

# ① 報到 — the only step whose fact is transient, so nothing else may run first.
skipped report_waking || { say "① report_waking"; api POST /api/self/waking '{"model":"stub-actor"}' >/dev/null; }

# ② 接回現場 — pull the resume snapshot and quote the planted nonce back into a
# server-stored message. A real agent does this because the boot context tells
# it to; the stub does it because that is the fact the gate reads.
if ! skipped resume_scene; then
  say "② resume_summary → quote nonce"
  SNAP="$(api GET /api/resume-summary)"
  if ! printf '%s' "$SNAP" | grep -qF "$NONCE"; then
    say "WARNING: the resume snapshot does NOT carry the scene nonce. The plant"
    say "and the snapshot have drifted apart — step ② would go red for a harness"
    say "reason, not an agent reason. Fix run.sh's plant before trusting a red."
  fi
  api POST /api/chat "$(python3 -c 'import json,sys;print(json.dumps({"to":sys.argv[1],"body":"接回現場：從 resume_summary 讀回本現場標記 "+sys.argv[2]}))' "$OWNER" "$NONCE")" >/dev/null
fi

# ③ 開票
TASK=""
if ! skipped create_task; then
  say "③ create_task"
  TASK="$(api POST /api/tasks "$(python3 -c 'import json,sys;print(json.dumps({"title":"seven-gate probe "+sys.argv[1],"description":"七步關卡的載體任務（stub actor）。scene="+sys.argv[1],"executor_member_id":sys.argv[2]}))' "$NONCE" "$AGENT")" | jget task.id)"
  [[ -n "$TASK" ]] || TASK="$(api GET /api/tasks | python3 -c 'import sys,json;d=json.load(sys.stdin);ts=d.get("tasks",d) if isinstance(d,dict) else d;print(next((t["id"] for t in ts if t.get("creator_id")==sys.argv[1]),""))' "$AGENT")"
  say "   task=$TASK"
fi

# ④ 提出計畫
if [[ -n "$TASK" ]] && ! skipped submit_plan; then
  say "④ submit_plan"
  api POST "/api/tasks/$TASK/plan" '{"steps":[{"name":"走完七步","dod":"七個 server 事實都在"},{"name":"回報收尾","dod":"closeout_reported=true"}]}' >/dev/null
fi

# ⑤ 報一步完成
if [[ -n "$TASK" ]] && ! skipped step_done; then
  say "⑤ update_step_status(done)"
  SID="$(api GET "/api/tasks/$TASK" | jget steps.0.id)"
  [[ -n "$SID" ]] && api POST "/api/tasks/$TASK/steps/$SID/status" '{"status":"done"}' >/dev/null
fi

# ⑥ 開一張等我回覆卡
if ! skipped reply_card; then
  say "⑥ create_reply_card"
  api POST /api/reply-cards '{"kind":"decision","summary":"七步關卡:要不要繼續收尾?","body":"這是載體用的請示卡。","options":["繼續收尾","先停在這裡"]}' >/dev/null
fi

# ⑦ 回報收尾
if [[ -n "$TASK" ]] && ! skipped closeout; then
  say "⑦ report_task_closeout"
  SID="$(api GET "/api/tasks/$TASK" | jget steps.1.id)"
  [[ -n "$SID" ]] && api POST "/api/tasks/$TASK/steps/$SID/status" '{"status":"done","handoff":"none","handoff_note":"載體 run,無後續"}' >/dev/null
  api POST "/api/tasks/$TASK/closeout" '{}' >/dev/null
fi

say "done (rc of this script is NOT the verdict — judge.py is)"
