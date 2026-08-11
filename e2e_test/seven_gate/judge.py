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
                   "image_answer_salt": "...",
                   "image_answer_sha256": "..."}              (written before boot)

                  ⚠️ THE IMAGE ANSWER IS NOT IN THERE IN CLEAR. It used to be,
                  and this directory sits in the repo tree on the same machine
                  the live agent runs on — so ⑨'s own claim ("the number exists
                  in no file the agent can open") was false about the harness's
                  own bundle. Only salt+sha256 is stored now and this file
                  re-derives the match by hashing the digit runs it finds in the
                  agent's messages. That removes the plaintext from disk; it does
                  NOT make the answer unrecoverable to a determined process on
                  the same host (10^6 candidates against a salt that is right
                  there), and no claim here should be read as saying otherwise.
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
import hashlib
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


def _says_answer(body, salt, digest, width=6):
    """Does `body` contain the planted number, without this file ever holding it?

    The number is `width` digits. Every window of that length in the body is
    hashed with the run's salt and compared — so the check is exactly as strong
    as the old `answer in body` was, with the plaintext gone from disk.
    Overlapping windows are deliberate: 「號碼是481902」 and 「4819023」 must both
    be found, and a non-overlapping scan would miss the second."""
    if not digest:
        return False
    for i in range(0, max(0, len(body) - width + 1)):
        chunk = body[i:i + width]
        if not chunk.isdigit():
            continue
        if hashlib.sha256((salt + chunk).encode("utf-8")).hexdigest() == digest:
            return True
    return False


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
    #
    # ⚠️ EARLIEST IS A GUESS, AND IT IS THE ONE PLACE THIS FILE GUESSES. There is
    # no server fact that says "this ticket is the one this round is about": the
    # assignment (assignment.md) deliberately never mentions tickets, so the
    # harness cannot plant a marker the agent would carry into the task without
    # destroying what ③ measures. An agent that opens a scratch/draft ticket
    # first therefore gets ④⑤⑦ judged against the WRONG row — three reds that
    # are the harness's, not the agent's. The alternative (pick whichever of the
    # agent's tasks satisfies ④⑤⑦) is worse: it makes those cells unfalsifiable
    # by construction. So the exposure is kept, named here, written verbatim in
    # CLAUDE.md, and — since it cannot be removed — SAID OUT LOUD in the evidence
    # of every cell it can poison (`_multi` below).
    mine = {}
    for _, t in _iter(samples, "tasks"):
        if t.get("creator_id") == agent:
            prev = mine.get(t.get("id"))
            if prev is None or (t.get("updated_ts") or 0) >= (prev.get("updated_ts") or 0):
                mine[t["id"]] = t
    ordered = sorted(mine.values(), key=lambda t: (t.get("created_ts") or 0, t.get("id")))
    task = ordered[0] if ordered else None
    _multi = ""
    if len(ordered) > 1:
        _multi = (" ⚠️ THIS AGENT OPENED %d TASKS (%s) AND THE GATE JUDGES THE "
                  "EARLIEST — ④⑤⑦ are read from %s only. If those cells look "
                  "wrong, suspect a draft/scratch ticket opened before the real "
                  "one rather than the agent: the gate has no server fact that "
                  "says which ticket this round is about (see CLAUDE.md 〈③ 取最早…〉)."
                  % (len(ordered), ", ".join(t.get("id") or "<no id>" for t in ordered),
                     task.get("id")))
    out.append(("create_task", "開票", task is not None,
                ("task %s (%r) has creator_id=%s" % (task.get("id"), task.get("title"), agent)) + _multi if task
                else "no task on the server carries creator_id=%s — this agent "
                     "never opened a ticket" % agent))

    # ④ 提出計畫 — submit_plan is the only writer of steps[] on a task.
    steps = (task or {}).get("steps") or []
    out.append(("submit_plan", "提出計畫", bool(task) and len(steps) > 0,
                "task %s carries %d plan step(s)" % (task.get("id"), len(steps)) if steps
                else ("task %s has an empty steps[] — no plan was ever submitted"
                      % (task.get("id") if task else "<none: ③ failed>")) + _multi))

    # ⑤ 報一步完成 — was a step reported done WHILE THE TASK WAS STILL RUNNING.
    #
    # 🔴 THE TWO THINGS THIS CELL HAS ALREADY BEEN, AND WHY BOTH FAILED.
    #
    # (1) "does ANY step of this task carry status=done". ⑦ (closeout) is
    #     terminal-tasks-only and a task is terminal only when every non-
    #     superseded step derives it there (domain.go DeriveTaskStatus: allDone →
    #     done), so ⑦ GREEN IMPLIED ⑤ GREEN: no world existed in which this cell
    #     was red while ⑦ was green. Zero discriminating power. MEASURED:
    #     OC_SG_SKIP_STEP=step_done left ⑤ PASS and put the red on ⑦.
    #
    # (2) "the done steps are a PREFIX of the plan, finished in plan order". That
    #     one is worse than zero, because it is red on TWO behaviours the server
    #     produces on purpose:
    #       * REPLAN. submit_plan freezes the nodes it did not re-list into
    #         `superseded` and LEAVES THEM IN PLACE, renumbered 0..n-1
    #         (dal_tasks.go ReplaceTaskPlan), while DeriveTaskStatus and
    #         TaskProgress both SKIP them. A superseded row therefore sits BEFORE
    #         later done rows, and a prefix test fails after any replan — and the
    #         boot context teaches agents to replan (seeds/system_interaction.md:
    #         「重新規劃——用 submit_plan 重交 plan」).
    #       * PARALLEL. SPEC §3.1: every step row is one leaf and parallel items
    #         are separate rows, so nodes in a parallel group finish in whatever
    #         order they finish. An order test calls that "back-filled".
    #     Both reds land on the AGENT for something the model does by design —
    #     the exact disease lib/window.sh's header names.
    #     And it bought almost nothing: with ⑦ green every non-superseded step is
    #     done, so the prefix half is TRUE BY CONSTRUCTION and only the ordering
    #     half was still saying anything.
    #
    # WHAT IT ASKS NOW — a TIME fact, read from the journal:
    #
    #   was there ever a moment at which this task carried a step at `done`
    #   AND HAD NOT YET REPORTED ITS CLOSE-OUT?
    #
    # WHERE THE DISCRIMINATING POWER COMES FROM WHEN ⑦ IS GREEN: ⑦ reads the
    # LAST state of the task and nothing else — it cannot say anything about
    # which states the task passed THROUGH. An agent that leaves every step
    # untouched and then, at the end, flips them all and closes out produces a
    # final snapshot indistinguishable from a green run, and this cell is red on
    # it because no sample ever caught the task mid-flight. That is the same
    # shape ① already relies on (presence=waking is gone seconds later), which is
    # why this file reads a journal instead of a final snapshot.
    #
    # AND IT IS IMMUNE TO BOTH FALSE REDS ABOVE: superseded rows are not `done`
    # so they cannot be mistaken for progress, and no ordering between steps is
    # examined at all — a parallel group finishing backwards still leaves a
    # moment where one of its nodes was done and the task was open.
    #
    # ⚠️ THE HONEST LIMIT, and it is a sampling one: if EVERY step report and the
    # close-out land inside a single collector poll (OC_SG_INTERVAL, 1s by
    # default), the journal never catches the intermediate state and this cell is
    # red for the harness's reason. The failure message says so, in those words,
    # so the reader is not sent after the agent for it. It is not a red anyone has
    # observed: the collector starts before boot and polls throughout, and the
    # path itself puts an owner card round-trip between the steps and the close.
    tid = task.get("id") if task else None
    inflight = None      # (t, done-step names) of the first mid-flight sighting
    ever_done = False    # a done step was seen at all, closed-out or not
    for s in samples:
        for t in s.get("tasks") or []:
            if t.get("id") != tid:
                continue
            d = [x.get("name") for x in (t.get("steps") or [])
                 if x.get("status") == "done"]
            if not d:
                continue
            ever_done = True
            if not t.get("closeout_reported") and inflight is None:
                inflight = (s.get("t"), d)
    if not task or not steps:
        why = ("task %s has no plan to advance — ⑤ cannot be read before ④ lands"
               % (task.get("id") if task else "<none: ③ failed>")) + _multi
    elif not ever_done:
        why = ("no step of task %s ever reached status=done in any sample — the "
               "plan was filed and then nothing moved" % tid) + _multi
    elif inflight is None:
        why = ("task %s was NEVER observed carrying a completed step while it was "
               "still open: in every one of the %d sample(s) that show a step at "
               "done, the close-out had already been reported. Nothing here is a "
               "step reported AS THE WORK WENT — the whole plan appears at the "
               "close. (⚠️ the other way to produce this: every step report AND "
               "the close-out landed inside one collector poll, in which case the "
               "red is the harness's and not the agent's — check journal.ndjson "
               "and the poll interval before reading it as the agent's.)"
               % (tid, len(samples))) + _multi
    else:
        why = ("task %s was observed at t=%s with step(s) %s already at done and "
               "the close-out NOT yet reported — a step was reported complete "
               "while the work was still running, which is a state the final "
               "snapshot (⑦) cannot show"
               % (tid, inflight[0], inflight[1]))
    out.append(("step_done", "報一步完成", inflight is not None, why))

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
                else ("task %s has closeout_reported=false — the work may have "
                      "stopped but no closeout was ever reported"
                      % (task.get("id") if task else "<none: ③ failed>")) + _multi))

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
    # carry a number. The fact is simply: did a message the agent SENT ever
    # contain that number. A text-only agent has no path to those digits but the
    # pixels — PROVIDED the answer is not reachable as text anywhere else, which
    # is the entire validity of this cell and is NOT something a comment can
    # assert. What actually holds it up, and exactly how far each part reaches:
    #
    #   * run.sh 3d scans the planted scene as TEXT ON THE SERVER (/api/chat,
    #     /api/tasks, both reply-card panes, /api/members, the agent's own resume
    #     snapshot) AND every file this run writes under the run dir except the
    #     picture itself, and refuses to start on a single hit. A positive
    #     control runs first so "zero hits" can never quietly mean "the scanner
    #     is broken".
    #   * actors/live.sh scrubs the harness's whole OC_SG_*/SG_* namespace out of
    #     the environment it hands the warden, and PROVES it (lib/scrub.sh
    #     sg_scrub_assert, with its own positive control) before spawning. Before
    #     that existed the answer travelled run.sh → actor → warden → tmux → the
    #     agent's shell, where one `env` would have handed a blind agent a green
    #     indistinguishable from a real one.
    #   * scene.json holds salt+sha256, not the number (see this file's header).
    #
    # WHAT IS STILL NOT COVERED, said plainly: the live agent runs as the same
    # user on the same host, so nothing here stops a process that goes LOOKING —
    # reading the repo tree, or brute-forcing 10^6 candidates against the salt.
    # The construction removes the answer from everything the agent is HANDED; it
    # does not make it unreachable.
    #
    # The answer is REGENERATED PER RUN and never the famous 42: a hard-coded
    # answer is one a model can have memorised, and a cell a model can pass from
    # memory measures nothing.
    salt = (scene.get("image_answer_salt") or "").strip()
    digest = (scene.get("image_answer_sha256") or "").strip().lower()
    width = int(scene.get("image_answer_len") or 6)
    if not digest:
        out.append(("image_answer", "看得到圖", False,
                    "scene.json carries no image_answer_sha256 — the harness "
                    "never planted the picture, so this step could not be "
                    "observed at all. This is a HARNESS red, not an agent red."))
    else:
        seen = next((item for _, item in _iter(samples, "chat")
                     if item.get("from") == agent
                     and _says_answer(item.get("body") or "", salt, digest, width)), None)
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
