#!/usr/bin/env python3
"""e2e_test/seven_gate/judge.py — the step-by-step verdict.

PURE. Reads ONE run directory (an evidence bundle written by collect.py) and
decides, per step, whether the SERVER held the fact that step is supposed to
leave behind. It opens no socket, mutates nothing, and never talks to the agent.
That is deliberate and it is the whole point of the split:

  * an agent asked "did you open the card?" answers yes — always, and for free;
  * a server row either carries the card or it does not.

So the judge's only input is what the server was observed to hold. If a step's
fact is absent, the step FAILED, no matter how the run narrated itself.

INPUT — one directory containing:
  scene.json      {"agent_id": "...", "scene_nonce": "...",
                   "peer_id": "...", "peer_nonce": "...",
                   "image_answer": "..."}                     (written before boot)
  journal.ndjson  one JSON object per poll:
                  {"t": <epoch float>, "member": <MemberDTO|null>,
                   "chat": [ChatMessageDTO...], "tasks": [TaskDTO...],
                   "reply_cards": [ReplyCardDTO...]}

WHY A JOURNAL AND NOT A FINAL SNAPSHOT: one of the facts is TRANSIENT.
`presence` returns to online/offline, so a run that only looks at the end cannot
tell "reported waking" from "never booted". The journal is a time series, so a
fact that existed for three seconds is still evidence. A step whose fact is
durable (a task row, a card row) is read from the journal all the same — one
reader, one shape.

EXIT — 0 iff EVERY step passed; 1 otherwise, with the FIRST failing step named on
the last line. Every step prints its own line regardless, because "which step"
is the answer the caller actually needs; a bare red tells them nothing.
"""
import json
import os
import sys

# The steps, in the fixed order. `key` is the stable identifier a caller greps
# for; `zh` is what the owner calls it.
#
# The directory is still called seven_gate and the path now has NINE cells. The
# owner added two after the first baseline: 「跟其他 agent 溝通」 (the six steps
# before it exercise chat / reply card / task only ever TOWARDS THE OWNER) and
# 「看得到圖」 (whether it can read a picture at all). The name is historical;
# THIS LIST is the contract, and no other file may keep a copy of it.
STEPS = [
    ("report_waking", "報到"),
    ("resume_scene", "接回現場"),
    ("create_task", "開票"),
    ("submit_plan", "提出計畫"),
    ("step_done", "報一步完成"),
    ("reply_card", "開一張等我回覆卡"),
    ("closeout", "回報收尾"),
    ("peer_message", "回覆另一個 agent"),
    ("image_answer", "看得到圖"),
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

    # ⑧ 回覆另一個 agent — the three channels an agent has are chat, the reply
    # card and the task, and every step above exercises them only TOWARDS THE
    # OWNER. Talking to a COLLEAGUE is a different act: a different recipient,
    # and nobody with an owner's patience on the other end. So the harness seats
    # a second member, has that member speak FIRST (carrying its own nonce), and
    # reads one fact: a server-stored message SENT BY the agent and ADDRESSED TO
    # that member.
    #
    # Two conditions, and they are different claims:
    #   * to == peer     — it talked to a colleague at all (never the owner,
    #                      never itself). This is the half the owner asked for.
    #   * nonce quoted   — it was a REPLY: the colleague's message was read and
    #                      answered, not merely broadcast past. Same read-back
    #                      trick as ②, and the same honest limit: it proves the
    #                      content came back, not which tool fetched it.
    peer = (scene.get("peer_id") or "").strip()
    peer_nonce = (scene.get("peer_nonce") or "").strip()
    if not peer:
        out.append(("peer_message", "回覆另一個 agent", False,
                    "scene.json carries no peer_id — the harness never seated a "
                    "second agent, so this step could not be observed at all. "
                    "This is a HARNESS red, not an agent red: fix the plant."))
    else:
        addressed = [item for _, item in _iter(samples, "chat")
                     if item.get("from") == agent and item.get("to") == peer]
        replied = next((m for m in addressed
                        if peer_nonce and peer_nonce in (m.get("body") or "")), None)
        hit = replied or (addressed[0] if addressed else None)
        if replied is not None:
            why = ("chat message %s runs %s → %s and quotes the peer's nonce — "
                   "the colleague was read AND answered"
                   % (replied.get("id"), agent, peer))
        elif hit is not None:
            why = ("chat message %s runs %s → %s but does NOT carry the peer's "
                   "nonce %r — the agent spoke to the colleague without showing "
                   "it read what the colleague said"
                   % (hit.get("id"), agent, peer, peer_nonce))
        else:
            why = ("no server-stored message runs %s → %s — the agent never "
                   "said anything to the other agent (talking to the owner does "
                   "not count: that is what steps ②⑥ already read)"
                   % (agent, peer))
        out.append(("peer_message", "回覆另一個 agent", replied is not None, why))

    # ⑨ 看得到圖 — can it SEE? An image was planted before boot whose pixels
    # carry a number, and that number exists NOWHERE else: not in the message
    # body, not in the filename, not in the mime, not in any task, plan or file
    # the agent can open (run.sh scans the whole planted scene and refuses to
    # start if it finds it in text, with a positive control so that "zero hits"
    # can never quietly mean "the scanner is broken").
    #
    # So the fact is simply: did a message the agent SENT ever contain the
    # number. A text-only agent cannot produce it — it has no path to those
    # digits but the pixels. THIS is the one cell where the failure and the
    # success would look identical if the answer ever leaked into text, which is
    # why the leak scan lives in the harness and not in a comment.
    #
    # The answer is REGENERATED PER RUN and never the famous 42: a hard-coded
    # answer is one a model can have memorised, and a cell a model can pass from
    # memory measures nothing.
    answer = (scene.get("image_answer") or "").strip()
    if not answer:
        out.append(("image_answer", "看得到圖", False,
                    "scene.json carries no image_answer — the harness never "
                    "planted the picture, so this step could not be observed at "
                    "all. This is a HARNESS red, not an agent red."))
    else:
        seen = next((item for _, item in _iter(samples, "chat")
                     if item.get("from") == agent
                     and answer in (item.get("body") or "")), None)
        out.append(("image_answer", "看得到圖", seen is not None,
                    "chat message %s from %s carries the number that exists only "
                    "in the planted image's pixels" % (seen.get("id"), agent) if seen
                    else "no message from %s ever carried the number drawn in the "
                         "planted image — nothing shows the picture was opened "
                         "and read (the number appears in no text the agent can "
                         "reach, so it cannot be produced any other way)" % agent))
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
