#!/usr/bin/env python3
"""e2e_test/seven_gate/judge.py — the seven-step verdict.

PURE. Reads ONE run directory (an evidence bundle written by collect.py) and
decides, per step, whether the SERVER held the fact that step is supposed to
leave behind. It opens no socket, mutates nothing, and never talks to the agent.
That is deliberate and it is the whole point of the split:

  * an agent asked "did you open the card?" answers yes — always, and for free;
  * a server row either carries the card or it does not.

So the judge's only input is what the server was observed to hold. If a step's
fact is absent, the step FAILED, no matter how the run narrated itself.

INPUT — one directory containing:
  scene.json      {"agent_id": "...", "scene_nonce": "..."}   (written before boot)
  journal.ndjson  one JSON object per poll:
                  {"t": <epoch float>, "member": <MemberDTO|null>,
                   "chat": [ChatMessageDTO...], "tasks": [TaskDTO...],
                   "reply_cards": [ReplyCardDTO...]}

WHY A JOURNAL AND NOT A FINAL SNAPSHOT: two of the seven facts are TRANSIENT.
`presence` returns to online/offline, so a run that only looks at the end cannot
tell "reported waking" from "never booted". The journal is a time series, so a
fact that existed for three seconds is still evidence. A step whose fact is
durable (a task row, a card row) is read from the journal all the same — one
reader, one shape.

EXIT — 0 iff all seven passed; 1 otherwise, with the FIRST failing step named on
the last line. Every step prints its own line regardless, because "which step"
is the answer the caller actually needs; a bare red tells them nothing.
"""
import json
import os
import sys

# The seven steps, in the fixed order. `key` is the stable identifier a caller
# greps for; `zh` is what the owner calls it.
STEPS = [
    ("report_waking", "報到"),
    ("resume_scene", "接回現場"),
    ("create_task", "開票"),
    ("submit_plan", "提出計畫"),
    ("step_done", "報一步完成"),
    ("reply_card", "開一張等我回覆卡"),
    ("closeout", "回報收尾"),
]


def load_bundle(run_dir):
    with open(os.path.join(run_dir, "scene.json"), encoding="utf-8") as fh:
        scene = json.load(fh)
    samples = []
    path = os.path.join(run_dir, "journal.ndjson")
    with open(path, encoding="utf-8") as fh:
        for lineno, line in enumerate(fh, 1):
            line = line.strip()
            if not line:
                continue
            try:
                samples.append(json.loads(line))
            except json.JSONDecodeError as exc:
                raise SystemExit(
                    "[seven_gate] FATAL: journal.ndjson line %d is not JSON (%s). "
                    "A truncated journal must not be judged — it would read as "
                    "'the agent did nothing'." % (lineno, exc)
                )
    return scene, samples


def _iter(samples, key):
    for s in samples:
        for item in s.get(key) or []:
            yield s, item


def judge(scene, samples):
    """-> list of (key, zh, passed, evidence_or_reason). No I/O, no ordering
    assumptions beyond 'the task the agent created is the task steps 4/5/7 are
    read from' — which is what binds those three to THIS run instead of to any
    task that happens to exist on the server."""
    agent = scene["agent_id"]
    nonce = scene["scene_nonce"]
    out = []

    # ① 報到 — report_waking stamps waking_since, and presence_state() derives
    # "waking" from it. Transient by construction: mounting SSE flips it to
    # online. If this is missed, fix the collector's sampling, do NOT relax this
    # into "presence was ever non-offline" — that passes on an agent that never
    # reported and was merely listening.
    hit = next((s for s in samples
                if (s.get("member") or {}).get("presence") == "waking"), None)
    out.append(("report_waking", "報到", hit is not None,
                "member %s observed presence=waking at t=%s" % (agent, hit["t"]) if hit
                else "no sample ever showed member %s at presence=waking — the "
                     "server was never told this agent booted" % agent))

    # ② 接回現場 — resume_summary is a GET and stamps NOTHING server-side, so
    # there is no row to read. What IS readable is the consequence: the scene
    # nonce was planted, before boot, where only the resume snapshot surfaces
    # it, and the agent has to quote it back into a server-stored message. An
    # agent that skipped the resume cannot produce the nonce.
    hit = next((item for _, item in _iter(samples, "chat")
                if item.get("from") == agent and nonce in (item.get("body") or "")), None)
    out.append(("resume_scene", "接回現場", hit is not None,
                "chat message %s from %s quotes the scene nonce" % (hit.get("id"), agent) if hit
                else "no server-stored message from %s ever quoted the scene nonce "
                     "%r — nothing shows the prior scene was read back" % (agent, nonce)))

    # ③ 開票 — a task row whose creator_id is the agent. Earliest wins, and it
    # is THE task ④⑤⑦ are judged on.
    mine = {}
    for _, t in _iter(samples, "tasks"):
        if t.get("creator_id") == agent:
            prev = mine.get(t.get("id"))
            if prev is None or (t.get("updated_ts") or 0) >= (prev.get("updated_ts") or 0):
                mine[t["id"]] = t
    task = None
    if mine:
        task = sorted(mine.values(), key=lambda t: (t.get("created_ts") or 0, t.get("id")))[0]
    out.append(("create_task", "開票", task is not None,
                "task %s (%r) has creator_id=%s" % (task.get("id"), task.get("title"), agent) if task
                else "no task on the server carries creator_id=%s — this agent "
                     "never opened a ticket" % agent))

    # ④ 提出計畫 — submit_plan is the only writer of steps[] on a task.
    steps = (task or {}).get("steps") or []
    out.append(("submit_plan", "提出計畫", bool(task) and len(steps) > 0,
                "task %s carries %d plan step(s)" % (task.get("id"), len(steps)) if steps
                else "task %s has an empty steps[] — no plan was ever submitted"
                     % (task.get("id") if task else "<none: ③ failed>")))

    # ⑤ 報一步完成 — at least one of that plan's steps reached done.
    done = [s for s in steps if s.get("status") == "done"]
    out.append(("step_done", "報一步完成", len(done) > 0,
                "step %r on task %s reached status=done" % (done[0].get("name"), task.get("id")) if done
                else "no step of task %s ever reached status=done — the plan was "
                     "filed and then nothing moved"
                     % (task.get("id") if task else "<none: ③ failed>")))

    # ⑥ 開一張等我回覆卡 — a reply card whose `from` is the agent. The agent has
    # to DECIDE to open it; a card the harness opened on its behalf proves
    # nothing, which is why the initiator is checked and not merely the count.
    card = next((item for _, item in _iter(samples, "reply_cards")
                 if item.get("from") == agent), None)
    out.append(("reply_card", "開一張等我回覆卡", card is not None,
                "reply card %s was opened by %s (status=%s)"
                % (card.get("id"), agent, card.get("status")) if card
                else "no reply card on the server carries from=%s — the agent "
                     "never asked the owner anything" % agent))

    # ⑦ 回報收尾 — closeout_reported is the server's own record that the agent
    # called report_task_closeout, distinct from the task merely being done.
    closed = bool(task) and bool(task.get("closeout_reported"))
    out.append(("closeout", "回報收尾", closed,
                "task %s has closeout_reported=true (status=%s)"
                % (task.get("id"), task.get("status")) if closed
                else "task %s has closeout_reported=false — the work may have "
                     "stopped but no closeout was ever reported"
                     % (task.get("id") if task else "<none: ③ failed>")))
    return out


def main(argv):
    if len(argv) != 2:
        print("usage: judge.py <run-dir>", file=sys.stderr)
        return 2
    run_dir = argv[1]
    scene, samples = load_bundle(run_dir)
    verdicts = judge(scene, samples)
    print("[seven_gate] judging %s — %d server sample(s), agent=%s"
          % (run_dir, len(samples), scene["agent_id"]))
    if not samples:
        print("[seven_gate] NOTE: the journal is EMPTY. Every step below fails "
              "for want of evidence, which is not the same as the agent having "
              "done nothing — check the collector first.")
    failed = None
    for i, (key, zh, passed, why) in enumerate(verdicts, 1):
        print("[seven_gate] step%d %-14s %s %s — %s"
              % (i, key, zh, "PASS" if passed else "FAIL", why))
        if not passed and failed is None:
            failed = (i, key, zh, why)
    with open(os.path.join(run_dir, "verdict.json"), "w", encoding="utf-8") as fh:
        json.dump([{"step": i, "key": k, "zh": z, "passed": p, "evidence": w}
                   for i, (k, z, p, w) in enumerate(verdicts, 1)], fh,
                  ensure_ascii=False, indent=2)
    if failed:
        print("[seven_gate] RED — failed at step%d %s (%s): %s"
              % (failed[0], failed[1], failed[2], failed[3]))
        return 1
    print("[seven_gate] all green")
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv))
