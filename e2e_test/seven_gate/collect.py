#!/usr/bin/env python3
"""e2e_test/seven_gate/collect.py — the server-fact collector.

Polls the ISOLATED server (:8791 by default, owner token) and appends one JSON
object per poll to <run-dir>/journal.ndjson. It decides nothing; judge.py does.
Keeping the two apart is what makes the verdict testable without a server.

It runs for the WHOLE run, starting BEFORE the agent boots, because ①'s fact
(presence=waking) exists only for as long as the agent has not mounted SSE. A
collector started late reads a green run as red.

  python3 collect.py --base http://127.0.0.1:8791 --token-file .state/owner.tok \\
                     --agent m-xxxx --run-dir runs/<stamp> --interval 1 --seconds 300

Stops early and cleanly on SIGTERM/SIGINT (run.sh terminates it once the actor
is done) — the journal is flushed per line, so a killed collector still leaves
every sample it took.
"""
import argparse
import json
import os
import signal
import ssl
import sys
import time
import urllib.error
import urllib.request

_stop = False


def _on_signal(_sig, _frame):
    global _stop
    _stop = True


def get(base, token, path):
    req = urllib.request.Request(base.rstrip("/") + path,
                                 headers={"Authorization": "Bearer " + token})
    ctx = ssl.create_default_context()
    try:
        with urllib.request.urlopen(req, timeout=10, context=ctx) as resp:
            return json.loads(resp.read().decode("utf-8"))
    except urllib.error.HTTPError as exc:
        return {"_http_error": exc.code, "_path": path}
    except Exception as exc:  # noqa: BLE001 — a poll failure must not end the run
        return {"_error": str(exc), "_path": path}


def _list(payload, *keys):
    """Unwrap {"members": [...]} / {"tasks": [...]} / a bare list, tolerantly —
    a shape surprise must degrade to 'no facts this poll', never to a crash that
    silently ends collection mid-run."""
    if isinstance(payload, list):
        return payload
    if isinstance(payload, dict):
        for k in keys:
            v = payload.get(k)
            if isinstance(v, list):
                return v
    return []


def sample(base, token, agent):
    member = None
    m = get(base, token, "/api/members/" + agent)
    if isinstance(m, dict) and m.get("id"):
        member = m
    chat = _list(get(base, token, "/api/chat?member_id=" + agent), "messages", "chat")
    cards = _list(get(base, token, "/api/reply-cards"), "cards", "reply_cards", "items")
    tasks = []
    for t in _list(get(base, token, "/api/tasks"), "tasks", "items"):
        tid = t.get("id")
        if not tid:
            continue
        # The list DTO carries neither steps[] nor closeout_reported, and those
        # are ④⑤⑦'s entire evidence — so every task is re-read in full.
        full = get(base, token, "/api/tasks/" + tid)
        tasks.append(full if isinstance(full, dict) and full.get("id") else t)
    return {"t": round(time.time(), 3), "member": member, "chat": chat,
            "tasks": tasks, "reply_cards": cards}


def main(argv=None):
    ap = argparse.ArgumentParser()
    ap.add_argument("--base", default=os.environ.get("OC_E2E_BASE", "http://127.0.0.1:8791"))
    ap.add_argument("--token-file", required=True)
    ap.add_argument("--agent", required=True)
    ap.add_argument("--run-dir", required=True)
    ap.add_argument("--interval", type=float, default=1.0)
    ap.add_argument("--seconds", type=float, default=900.0)
    args = ap.parse_args(argv)

    signal.signal(signal.SIGTERM, _on_signal)
    signal.signal(signal.SIGINT, _on_signal)

    with open(args.token_file, encoding="utf-8") as fh:
        token = fh.read().strip()
    os.makedirs(args.run_dir, exist_ok=True)
    path = os.path.join(args.run_dir, "journal.ndjson")
    deadline = time.time() + args.seconds
    n = 0
    with open(path, "a", encoding="utf-8") as journal:
        while not _stop and time.time() < deadline:
            journal.write(json.dumps(sample(args.base, token, args.agent),
                                     ensure_ascii=False) + "\n")
            journal.flush()
            n += 1
            time.sleep(args.interval)
    print("[collect] wrote %d sample(s) → %s" % (n, path))
    return 0


if __name__ == "__main__":
    sys.exit(main())
