#!/usr/bin/env bash
# curl|bash read-before-execute guard (T-4358)
#
# THE DEFECT
# ----------
# bash executes a script arriving on a PIPE incrementally: read a chunk, run
# whatever parsed, read the next. So `curl … | bash -s -- --uninstall --dry-run`
# used to print its result and exit while curl still had most of the file in
# flight. The read end closed under the writer, curl's next write failed, and a
# run that SUCCEEDED ended on
#   curl: (23) Failure writing output to destination, passed N returned 0
# The EXIT-time drain (bin/tests/stdin-drain-guard.sh) narrows the window; it
# cannot close it, because the bytes are still on the wire, not in the pipe.
#
# WHAT THE FIX GUARANTEES — stated as exactly what is proven, no wider
# ---------------------------------------------------------------------
# Every statement that ACTS lives in oc_main(), and the LAST line of install.sh
# calls it. It is NOT true that "bash reads the whole file before executing
# anything": the top-level prologue (set -euo pipefail, the from-stdin flag, the
# function definitions, the trap, the mode detection — which really does fork a
# subshell for SELF_DIR) all runs as it is parsed. What IS true, and is all the
# fix needs, is that NONE of the prologue can PRINT or EXIT, so:
#
#   install.sh produces no output and does not terminate until it has been read
#   in full.
#
# That is the property asserted here. Case 3 pins the "cannot print or exit"
# half, which is what makes the weaker statement sufficient.
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
# POSITIVE CONTROLS, and the bug they had. Each probe is also run against a
# mechanically UNWRAPPED copy — the `oc_main() {` line, its closing brace and the
# trailing call removed, which is byte-for-byte the pre-T-4358 top-level
# structure — which MUST fail. The first version of this file asserted only
# "the control exited non-zero and did not deliver everything", and independent
# review broke it exactly the way this file's own attack-surface note predicted:
# injecting a syntax error into the control made it die at rc=2 having never run
# --help at all, and the guard stayed 6 ok / 0 failed. A control that fails for
# an unrelated reason is worth no more than one that passes for an unrelated
# reason. So the control must now ALSO have executed the command (u_ran) and
# must fail with curl's specific broken-pipe status (23), not merely non-zero.
#
# BOTH DOCUMENTED PATHS ARE PROBED. --help is the cheapest early exit, but the
# original bug report, docs/guide/install.md and docs/guide/troubleshooting.md
# all describe --uninstall. Probing only --help left "someone moves the
# --uninstall dispatch back to top level" with no guard at all.
#
# The server, HOME and script artifacts are all temporary; --help has no
# side effects and --uninstall runs --dry-run against an empty fake HOME (so it
# takes the "Already clean." branch and never reaches launchctl).
set -euo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SCRIPT="$HERE/../install.sh"
[[ -f "$SCRIPT" ]] || { echo "FATAL: install.sh not found at $SCRIPT" >&2; exit 2; }

PASS=0; FAIL=0
ok()  { PASS=$((PASS+1)); printf '  ok   — %s\n' "$1"; }
bad() { FAIL=$((FAIL+1)); printf '  FAIL — %s\n' "$1"; }

WORK="$(mktemp -d -t oc-curl-bash-read.XXXXXX)"
trap 'rm -rf "$WORK"' EXIT
mkdir -p "$WORK/home"

echo "== install.sh is fully read before it prints or exits (curl | bash) =="

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

# ── 2. STATIC: the body's own invariants ───────────────────────────────────
# The wrap is only behaviour-preserving while the body keeps behaving like top
# level. install.sh is deliberately NOT re-indented, so body-level statements sit
# at column 0 and nested function bodies are indented — which makes both of these
# greppable. A body-level `local` would fail outside a function (and silently
# scope a variable other functions read); a body-level bare `return` would end
# oc_main early where the original would have run on.
OC_BODY="$WORK/oc_main_body.txt"
awk '/^oc_main\(\) \{$/{inbody=1; next} inbody' "$SCRIPT" > "$OC_BODY"
if [[ ! -s "$OC_BODY" ]]; then
  bad "static: could not extract oc_main's body — the two checks below prove nothing"
else
  if grep -nE '^local[[:space:]]' "$OC_BODY" > "$WORK/local.hits"; then
    bad "static: body-level 'local' in oc_main (fails outside a function / changes scope): $(tr '\n' ' ' < "$WORK/local.hits")"
  else
    ok "static: no body-level 'local' in oc_main (every variable stays global, as at top level)"
  fi
  if grep -nE '^return([[:space:]]|$)' "$OC_BODY" > "$WORK/return.hits"; then
    bad "static: body-level 'return' in oc_main — at top level this was a no-op or an error, now it ends the installer early: $(tr '\n' ' ' < "$WORK/return.hits")"
  else
    ok "static: no body-level 'return' in oc_main ('exit' is still the only way out)"
  fi
fi

# ── 3. STATIC: the prologue may not print or exit ──────────────────────────
# This is the half that makes "no output and no exit before the file is read"
# true. Everything before `oc_main() {` runs as it is parsed, so a single `echo`
# or `exit` added there re-opens the defect for every path — and it would look
# entirely reasonable in review. Walks the prologue, skips function BODIES (their
# statements run only when called), and flags output/exit verbs left at top level.
awk '
  /^oc_main\(\) \{$/ { exit }
  /^[a-z_][a-z0-9_]*\(\)[[:space:]]*\{/ { infunc=1; next }
  infunc && /^\}/ { infunc=0; next }
  infunc { next }
  /^[[:space:]]*(echo|printf|cat|exit|read|sed|curl)([[:space:]]|$)/ { print NR": "$0 }
' "$SCRIPT" > "$WORK/prologue.hits" || true
if [[ -s "$WORK/prologue.hits" ]]; then
  bad "static: the prologue before oc_main can print or exit — that runs while curl is still sending, which is the whole defect: $(tr '\n' ' ' < "$WORK/prologue.hits")"
else
  ok "static: nothing before oc_main prints or exits (so nothing is observable until the file is read in full)"
fi

# ── 4. STATIC: the overturned claim must not come back ─────────────────────
# THIS TICKET GOT THE SAME SHAPE WRONG THREE TIMES: change a thing, miss the
# place that describes the thing. Twice it was caught by a human reading the
# tree, and the third time the miss was the sentence this very package had just
# proved false, sitting in bin/tests/run.sh. "I swept for it" is not a method —
# it is a memory. So the sweep is now a grep that must return ZERO lines, and it
# runs on every CI.
#
# The claim being banned is that bash defers ALL execution until EOF. It does
# not: install.sh's prologue runs as it is parsed (see case 3). Lines that REFUTE
# it are the point of this file, so the filter drops refutation markers.
#
# TWO SCOPE EXCLUSIONS, both stated rather than quietly applied:
#   - docs/*-evidence/ would hold captured CI logs from past runs: immutable
#     records of what was printed at the time, not claims this tree makes, so
#     editing them would be falsifying evidence. NOTE (T-bf93): no such
#     directory exists in this tree any more — the one that did
#     (docs/T-081b-evidence/) was deleted as a one-off evidence pile. The
#     exclusion is kept deliberately, for the next time one appears; it
#     currently excludes nothing, and that is not a claim that it does.
#   - THIS FILE, which necessarily contains the banned phrases as its own pattern
#     list — the first run of this check flagged itself. A rule cannot be written
#     without naming what it forbids.
# Everything else under bin/ and docs/ is in scope. Verified by mutation: putting
# the claim back into bin/tests/run.sh turns this red.
CLAIM_HITS="$WORK/claim.hits"
grep -rnE 'before it executes|before it starts executing|才會開始動作|才會開始執行|讀完整份才' \
  "$HERE/.." "$HERE/../../docs" 2>/dev/null \
  | grep -v '/docs/[^/]*-evidence/' \
  | grep -v 'curl-bash-read-before-execute-guard.sh' \
  | grep -vE 'NOT true|is FALSE|NOT "fully read' \
  > "$CLAIM_HITS" || true
if [[ -s "$CLAIM_HITS" ]]; then
  bad "static: the overturned claim ('read the whole file before it executes') is asserted somewhere again — the guarantee is 'no output and no exit until fully read'. Offending lines: $(sed 's#.*/##' "$CLAIM_HITS" | tr '\n' ' ')"
else
  ok "static: nothing in bin/ or docs/ re-asserts the overturned 'executes only after the whole file is read' claim"
fi

# The splice below needs the shape asserted above. Explode-with-a-traceback is a
# worse report than the FAILs already printed, so stop here instead: the static
# findings ARE the finding.
if [[ "$FAIL" != "0" ]]; then
  echo
  echo "curl|bash read-before-execute guard: $PASS ok, $FAIL failed (probes skipped — install.sh no longer has the shape they splice into)"
  exit 1
fi

# ── the probes ──────────────────────────────────────────────────────────────
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
# the end where the old structure left it — bash acts long before EOF
unwrapped = [l for l in body if l != b"oc_main() {"]
emit(work / "unwrapped.sh", unwrapped + pad(unwrapped, 150000))
PY

python3 - "$WORK" <<'PY'
import http.server, socketserver, subprocess, sys, threading, time, pathlib

work = pathlib.Path(sys.argv[1])
home = work / "home"

def probe(artifact, args, marker):
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
    env = {"PATH": "/usr/bin:/bin:/usr/sbin:/sbin", "HOME": str(home),
           "OC_LAUNCHD_LABEL": "com.officraft.serve.readbeforeexecguard"}
    try:
        # pipefail preserves curl's status: rc=23 is the user-visible broken pipe.
        done = subprocess.run(
            ["bash", "-o", "pipefail", "-c",
             f"curl -fsS {url} | bash -s -- {args}"],
            text=True, stdout=subprocess.PIPE, stderr=subprocess.STDOUT,
            timeout=120, env=env)
    finally:
        server.shutdown(); server.server_close()
    return done.returncode, state["sent"], len(payload), marker in done.stdout


results = []
for path, args, marker in (
    ("--help", "--help", "OffiCraft standalone installer"),
    ("--uninstall --dry-run", "--uninstall --dry-run", "Already clean."),
):
    rc, sent, total, ran = probe(work / "wrapped.sh", args, marker)
    print(f"WRAPPED [{path}] curl_rc={rc} sent={sent} total={total} executed={ran}")
    u_rc, u_sent, u_total, u_ran = probe(work / "unwrapped.sh", args, marker)
    print(f"CONTROL [{path}] curl_rc={u_rc} sent={u_sent} total={u_total} executed={u_ran}")

    results.append((f"{path}: the writer delivers the WHOLE file before bash acts on any of it",
                    sent == total, f"server sent {sent} of {total} B"))
    results.append((f"{path}: curl exits 0 — no broken pipe on a successful run",
                    rc == 0, f"curl|bash rc={rc}"))
    results.append((f"{path}: the command still actually ran (the case is live, not merely quiet)",
                    ran, f"executed={ran}"))
    # u_ran is REQUIRED: without it a control that died of an unrelated error
    # (a syntax error, a missing binary) reads as a working positive control.
    # 23 OR 56, and the "or" is load-bearing: 23 is CURLE_WRITE_ERROR and 56 is
    # CURLE_RECV_ERROR, and THIS PROJECT HAS OBSERVED BOTH for this one condition
    # — bin/install.sh, docs/guide/install.md and docs/guide/troubleshooting.md
    # all quote the symptom as "curl: (23|56)". Pinning 23 alone would assert that
    # 56 is not a broken pipe, contradicting our own documentation, and would show
    # up as a FLAKY positive control rather than as a real regression.
    results.append((f"{path}: positive control — the UNWRAPPED shape reaches the command AND breaks the pipe",
                    (u_rc in (23, 56) and u_sent < u_total and u_ran),
                    f"control rc={u_rc} (want 23 or 56), sent {u_sent} of {u_total} B, executed={u_ran} (want True)"))

pathlib.Path(sys.argv[1], "results.tsv").write_text(
    "".join(f"{'PASS' if okv else 'FAIL'}\t{name}\t{detail}\n" for name, okv, detail in results))
PY

while IFS=$'\t' read -r verdict name detail; do
  if [[ "$verdict" == "PASS" ]]; then ok "$name ($detail)"; else bad "$name ($detail)"; fi
done < "$WORK/results.tsv"

echo
echo "curl|bash read-before-execute guard: $PASS ok, $FAIL failed"
[[ "$FAIL" == "0" ]] || exit 1
