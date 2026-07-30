#!/usr/bin/env bash
# curl|bash read-before-execute guard (T-4358)
#
# THE DEFECT
# ----------
# bash executes a script arriving on a PIPE incrementally: read a chunk, run
# whatever parsed, read the next. So `curl … | bash -s -- --help` used to print
# the help and exit while curl still had most of the file in flight. The read end
# closed under the writer, curl's next write failed, and a run that SUCCEEDED
# ended on
#   curl: (23) Failure writing output to destination, passed N returned 0
# The EXIT-time drain (bin/tests/stdin-drain-guard.sh) narrows the window; it
# cannot close it, because the bytes are still on the wire, not in the pipe.
#
# THE FIX THIS GUARDS
# -------------------
# Every statement that ACTS lives in oc_main(), and the LAST line of install.sh
# calls it. A function definition is ONE command, so bash cannot execute any of
# it until it has read the closing brace — i.e. the whole file. Delivery
# therefore completes before the first byte of output.
#
# WHY THE PADDING GOES INSIDE THE FUNCTION BODY
# ---------------------------------------------
# The probe grows the served artifact to 150 KB so a slow transfer has something
# to still be delivering. Appending that padding to the END of the file would
# test nothing: bash would reach `oc_main "$@"`, run the whole installer and
# exit with 70 KB of trailing comments still unread — exactly the old failure,
# reproduced by the harness rather than by install.sh. So the padding is spliced
# in BEFORE the closing brace, where it is part of the command bash must finish
# reading. The splice asserts the file's shape first, so a future edit that moves
# the call off the last line fails here loudly instead of silently defusing this.
#
# POSITIVE CONTROL. Case 2 runs the same probe against a mechanically UNWRAPPED
# copy — the `oc_main() {` line, its closing brace and the trailing call removed,
# which is byte-for-byte the pre-T-4358 top-level structure — and requires it to
# FAIL. Without it a probe that stopped discriminating (a fast server, a
# too-small artifact) would report case 1 green for the wrong reason.
#
# The server, HOME and script artifacts are all temporary, and --help is used
# because it has no installation side effects: the probe observes only delivery
# order and the writer's status.
set -euo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SCRIPT="$HERE/../install.sh"
[[ -f "$SCRIPT" ]] || { echo "FATAL: install.sh not found at $SCRIPT" >&2; exit 2; }

PASS=0; FAIL=0
ok()  { PASS=$((PASS+1)); printf '  ok   — %s\n' "$1"; }
bad() { FAIL=$((FAIL+1)); printf '  FAIL — %s\n' "$1"; }

WORK="$(mktemp -d -t oc-curl-bash-read.XXXXXX)"
trap 'rm -rf "$WORK"' EXIT

echo "== install.sh is fully read before it executes (curl | bash) =="

# ── 1. STATIC: the body is a function and the call is the LAST line ─────────
# This is the property the whole fix rests on, and it is also the precondition
# the splice below depends on, so it is asserted before anything is measured.
# No mapfile: stock macOS /bin/bash is 3.2 and does not have it.
TAIL_PENULT="$(grep -vE '^[[:space:]]*$' "$SCRIPT" | tail -2 | head -1)"
TAIL_LAST="$(grep -vE '^[[:space:]]*$' "$SCRIPT" | tail -1)"
if [[ "$TAIL_LAST" == 'oc_main "$@"' && "$TAIL_PENULT" == '}' ]]; then
  ok "static: install.sh ends with the oc_main definition's closing brace, then the call"
else
  bad "static: install.sh must end with '}' then 'oc_main \"\$@\"' — found: $TAIL_PENULT / $TAIL_LAST. The body is no longer wrapped, or the call is no longer last, and curl|bash executes it half-delivered again"
fi
if grep -qxF 'oc_main() {' "$SCRIPT"; then
  ok "static: oc_main() is defined"
else
  bad "static: no 'oc_main() {' line in install.sh"
fi

# The splice below needs the shape asserted above. Explode-with-a-traceback is a
# worse report than the two FAILs already printed, so stop here instead: the
# static findings ARE the finding.
if [[ "$FAIL" != "0" ]]; then
  echo
  echo "curl|bash read-before-execute guard: $PASS ok, $FAIL failed (probe skipped — install.sh no longer has the shape it splices into)"
  exit 1
fi

# ── the probe ───────────────────────────────────────────────────────────────
# Splices padding in before the FINAL two lines (`}` and the call), then serves
# the result at 5 KB/s and reports what the writer and the reader each saw.
python3 - "$SCRIPT" "$WORK" <<'PY'
import sys, pathlib
src, work = pathlib.Path(sys.argv[1]), pathlib.Path(sys.argv[2])
lines = src.read_bytes().split(b"\n")
# drop the trailing empty element produced by the final newline
if lines and lines[-1] == b"": lines.pop()
tail = lines[-2:]
assert tail[0] == b"}" and tail[1] == b'oc_main "$@"', tail
body, tail = lines[:-2], lines[-2:]

def emit(path, ls):
    path.write_bytes(b"\n".join(ls) + b"\n")

def pad(ls, target):
    text = b"\n".join(ls) + b"\n"
    need = target - len(text) - len(b"\n# T-4358 slow-delivery padding\n\n")
    return [b"# T-4358 slow-delivery padding", b"#" * max(need, 1)]

# wrapped: padding INSIDE oc_main, so bash must read it to finish the definition
emit(work / "wrapped.sh", body + pad(body + tail, 150000) + tail)
# unwrapped positive control: pre-T-4358 shape (no function, no call), padding at
# the end where the old structure left it — bash runs --help long before EOF
unwrapped = [l for l in body if l != b"oc_main() {"]
emit(work / "unwrapped.sh", unwrapped + pad(unwrapped, 150000))
PY

python3 - "$WORK" <<'PY'
import http.server, socketserver, subprocess, sys, threading, time, pathlib

work = pathlib.Path(sys.argv[1])

def probe(artifact):
    payload = artifact.read_bytes()
    state = {"sent": 0}

    class Handler(http.server.BaseHTTPRequestHandler):
        def log_message(self, *_): pass
        def do_GET(self):
            if self.path != "/install.sh":
                self.send_error(404); return
            self.send_response(200)
            self.send_header("Content-Length", str(len(payload)))
            self.end_headers()
            try:
                for off in range(0, len(payload), 4096):
                    block = payload[off:off + 4096]
                    self.wfile.write(block); self.wfile.flush()
                    state["sent"] += len(block)
                    time.sleep(len(block) / 5000.0)
            except (BrokenPipeError, ConnectionResetError):
                pass

    class Server(socketserver.TCPServer):
        allow_reuse_address = True

    server = Server(("127.0.0.1", 0), Handler)
    threading.Thread(target=server.serve_forever, daemon=True).start()
    url = f"http://127.0.0.1:{server.server_address[1]}/install.sh"
    try:
        # pipefail preserves curl's status: rc=23 is the user-visible broken pipe.
        done = subprocess.run(
            ["bash", "-o", "pipefail", "-c", f"curl -fsS {url} | bash -s -- --help"],
            text=True, stdout=subprocess.PIPE, stderr=subprocess.STDOUT, timeout=120)
    finally:
        server.shutdown(); server.server_close()
    return done.returncode, state["sent"], len(payload), \
        "OffiCraft standalone installer" in done.stdout

rc, sent, total, ran = probe(work / "wrapped.sh")
print(f"WRAPPED curl_rc={rc} sent={sent} total={total} executed={ran}")
w_ok = (rc == 0 and sent == total and ran)

u_rc, u_sent, u_total, u_ran = probe(work / "unwrapped.sh")
print(f"CONTROL curl_rc={u_rc} sent={u_sent} total={u_total} executed={u_ran}")
u_broke = (u_rc != 0 and u_sent < u_total)

lines = []
lines.append(("the writer delivers the WHOLE file before bash acts on any of it",
              sent == total, f"server sent {sent} of {total} B"))
lines.append(("curl exits 0 — no broken pipe on a successful --help",
              rc == 0, f"curl|bash rc={rc}"))
lines.append(("--help still actually ran (the case is live, not merely quiet)",
              ran, f"executed={ran}"))
lines.append(("positive control: the UNWRAPPED shape still breaks the pipe",
              u_broke, f"control rc={u_rc}, sent {u_sent} of {u_total} B"))
pathlib.Path(sys.argv[1], "results.tsv").write_text(
    "".join(f"{'PASS' if okv else 'FAIL'}\t{name}\t{detail}\n" for name, okv, detail in lines))
PY

while IFS=$'\t' read -r verdict name detail; do
  if [[ "$verdict" == "PASS" ]]; then ok "$name ($detail)"; else bad "$name ($detail)"; fi
done < "$WORK/results.tsv"

echo
echo "curl|bash read-before-execute guard: $PASS ok, $FAIL failed"
[[ "$FAIL" == "0" ]] || exit 1
