#!/usr/bin/env bash
# curl|bash read-before-execute probe (T-4358)
#
# This is deliberately a two-phase probe while T-5831 owns install.sh:
# before the function wrapper lands, it proves that the harness can observe the
# defect (bash executes --help before a slow HTTP source has finished, and curl
# gets EPIPE).  The T-4358 implementation changes this expectation to require
# complete delivery and curl rc=0, then wires the probe into bin/tests/run.sh.
#
# The server, HOME and script artifact are all temporary.  --help is selected
# because it has no installation side effects; the probe observes only delivery
# order and the writer's status.
set -euo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SCRIPT="$HERE/../install.sh"
[[ -f "$SCRIPT" ]] || { echo "FATAL: install.sh not found at $SCRIPT" >&2; exit 2; }

WORK="$(mktemp -d -t oc-curl-bash-read.XXXXXX)"
trap 'rm -rf "$WORK"' EXIT

# Grow the served artifact to the 150 KB regression size without modifying the
# checkout.  A function wrapper must include this padding INSIDE its body when
# the final regression test is enabled; for the baseline, trailing comments are
# enough to show bash's current incremental top-level execution.
cp "$SCRIPT" "$WORK/install-stream.sh"
SCRIPT_BYTES="$(wc -c < "$WORK/install-stream.sh" | tr -d ' ')"
PAD_BYTES=$((150000 - SCRIPT_BYTES))
if (( PAD_BYTES < 1 )); then PAD_BYTES=1; fi
python3 - "$WORK/install-stream.sh" "$PAD_BYTES" <<'PY'
import sys
path, count = sys.argv[1], int(sys.argv[2])
with open(path, "ab") as f:
    f.write(b"\n# T-4358 slow-delivery padding\n")
    f.write(b"#" * count)
    f.write(b"\n")
PY

python3 - "$WORK/install-stream.sh" <<'PY'
import http.server
import os
import socketserver
import subprocess
import sys
import threading
import time

artifact = sys.argv[1]
payload = open(artifact, "rb").read()
state = {"sent": 0, "finished": False}

class Handler(http.server.BaseHTTPRequestHandler):
    def log_message(self, *_):
        pass
    def do_GET(self):
        if self.path != "/install.sh":
            self.send_error(404)
            return
        self.send_response(200)
        self.send_header("Content-Length", str(len(payload)))
        self.end_headers()
        try:
            for offset in range(0, len(payload), 4096):
                block = payload[offset:offset + 4096]
                self.wfile.write(block)
                self.wfile.flush()
                state["sent"] += len(block)
                time.sleep(len(block) / 5000.0)
        except (BrokenPipeError, ConnectionResetError):
            pass
        finally:
            state["finished"] = state["sent"] >= len(payload)

class Server(socketserver.TCPServer):
    allow_reuse_address = True

server = Server(("127.0.0.1", 0), Handler)
thread = threading.Thread(target=server.serve_forever, daemon=True)
thread.start()
url = f"http://127.0.0.1:{server.server_address[1]}/install.sh"
try:
    # pipefail preserves curl's status: rc=23 is the user-visible broken pipe.
    command = f"curl -fsS {url} | bash -s -- --help"
    completed = subprocess.run(
        ["bash", "-o", "pipefail", "-c", command],
        text=True, stdout=subprocess.PIPE, stderr=subprocess.STDOUT,
        timeout=45,
    )
finally:
    server.shutdown()
    server.server_close()

executed = 'OffiCraft standalone installer' in completed.stdout
print(f"curl_bash_rc={completed.returncode}")
print(f"server_sent={state['sent']}")
print(f"artifact_bytes={len(payload)}")
print(f"script_executed={executed}")

# Baseline positive control.  The T-4358 implementation deliberately flips all
# three conditions below; until then this proves the real network probe sees the
# defect instead of merely assuming it from a synthetic pipe.
if completed.returncode != 23 or state['sent'] >= len(payload) or not executed:
    raise SystemExit('baseline probe lost discrimination: expected early execution and curl EPIPE')
PY
