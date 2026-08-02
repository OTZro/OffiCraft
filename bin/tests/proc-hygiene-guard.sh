#!/usr/bin/env bash
# bin/tests/proc-hygiene-guard.sh — HERMETIC tests for the harness process
# hygiene primitive bin/tests/lib/run_bounded.py (T-1a54).
#
# The mutation-testing guards spawn scripts-under-test; a mutant that busy-loops
# must be bounded and its WHOLE subtree reaped, or it leaks as an orphan — the
# seth-m5 core-burn (a mutant busy-loop ran ~46h after its worker died). These
# tests pin run_bounded's four load-bearing properties WITHOUT letting a
# busy-loop outlive the test: every busy-loop below is spawned under run_bounded
# (or explicitly killed), and the suite reaps any straggler it recorded on exit.
set -uo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
RB="$HERE/lib/run_bounded.py"
[[ -f "$RB" ]] || { echo "FATAL: run_bounded.py not found at $RB" >&2; exit 2; }

PASS=0; FAIL=0
ok()  { PASS=$((PASS+1)); printf '  ok   — %s\n' "$1"; }
bad() { FAIL=$((FAIL+1)); printf '  FAIL — %s\n' "$1"; }
check(){ if [[ "$2" == "$3" ]]; then ok "$1"; else bad "$1 (want '$2' got '$3')"; fi; }
alive(){ [[ -n "${1:-}" ]] && kill -0 "$1" 2>/dev/null; }

WORK="$(mktemp -d -t oc-proc-hygiene-guard.XXXXXX)"
# Belt to the suspenders: if any assertion below leaves a recorded busy-loop
# alive (a red test), collect it here so the guard itself never leaks a core.
_cleanup() {
  local f g
  for f in "$WORK"/gpid.*; do
    [[ -f "$f" ]] || continue
    g="$(cat "$f" 2>/dev/null || true)"
    alive "$g" && kill -KILL "$g" 2>/dev/null || true
  done
  rm -rf "$WORK"
}
trap '_cleanup' EXIT

# A child that forks a grandchild busy-loop (recording its pid) then blocks. If
# run_bounded reaped only the direct child, the grandchild would keep burning a
# core — the exact orphan shape this ticket is about.
cat > "$WORK/leaky.sh" <<'LEAKY'
#!/usr/bin/env bash
pidfile="$1"
bash -c 'echo $$ > "$0"; while :; do :; done' "$pidfile" &
sleep 30
LEAKY

echo "run_bounded process-hygiene tests"

# ── 1. a ceiling actually bounds a slow child ───────────────────────────────
t0=$(python3 -c 'import time;print(time.time())')
python3 "$RB" 2 sleep 30 >/dev/null 2>&1; rc=$?
t1=$(python3 -c 'import time;print(time.time())')
check "a 30s child under a 2s ceiling is killed with rc 124 (GNU timeout code)" "124" "$rc"
within=$(python3 -c "print(1 if ($t1-$t0) < 6 else 0)")
check "and it returns at the ceiling (~2s), not after the child's 30s" "1" "$within"

# ── 2. the WHOLE subtree is reaped on timeout, not just the direct child ─────
: > "$WORK/gpid.2"
python3 "$RB" 2 bash "$WORK/leaky.sh" "$WORK/gpid.2" >/dev/null 2>&1; rc=$?
gpid="$(cat "$WORK/gpid.2" 2>/dev/null || true)"
check "the grandchild busy-loop was actually spawned (positive control)" "1" "$([[ -n "$gpid" ]] && echo 1 || echo 0)"
sleep 0.3
if alive "$gpid"; then
  bad "the grandchild busy-loop is reaped with its parent (pid $gpid still alive — ORPHAN)"
else
  ok "the grandchild busy-loop is reaped with its parent (no orphan survives the timeout)"
fi

# ── 3. the child's exit code is passed through untouched ────────────────────
# run_bounded's OWN stderr is the only witness when it dies instead of relaying
# the child's code. These two lines used to be `2>/dev/null`, so when run_bounded
# crashed (T-3e41) the screen showed nothing but "want '7' got '1'" — the
# traceback naming the faulty line was thrown away at exactly the moment it was
# needed, and the red became indistinguishable from a real failure. Keep it and
# print it on failure.
RBERR="$WORK/rb.err"
show_rb_stderr() {
  if [[ -s "$RBERR" ]]; then
    printf '         run_bounded said on its own stderr:\n'
    sed 's/^/         | /' "$RBERR"
  else
    printf '         (run_bounded printed nothing on stderr)\n'
  fi
}
passthrough() { # <want-code> <label>
  local want="$1" label="$2" got
  python3 "$RB" 10 bash -c "exit $want" >/dev/null 2>"$RBERR"; got=$?
  check "$label" "$want" "$got"
  [[ "$want" == "$got" ]] || show_rb_stderr
}
passthrough 0 "exit 0 is passed through"
passthrough 7 "a non-zero exit (7) is passed through"

# ── 4. on SIGTERM the subtree still dies — how the framework reaps mid-run ───
# bin/tests/run.sh's EXIT/INT/TERM trap sends SIGTERM to the in-flight
# run_bounded; run_bounded must group-kill its subtree before dying. Same leaky
# child, but interrupted instead of timed out.
: > "$WORK/gpid.4"
python3 "$RB" 30 bash "$WORK/leaky.sh" "$WORK/gpid.4" >/dev/null 2>&1 &
rbpid=$!
for _ in $(seq 1 50); do [[ -s "$WORK/gpid.4" ]] && break; sleep 0.1; done
kill -TERM "$rbpid" 2>/dev/null
wait "$rbpid" 2>/dev/null
gpid="$(cat "$WORK/gpid.4" 2>/dev/null || true)"
check "the grandchild was spawned before the interrupt (positive control)" "1" "$([[ -n "$gpid" ]] && echo 1 || echo 0)"
sleep 0.3
if alive "$gpid"; then
  bad "SIGTERM to run_bounded reaps the subtree (pid $gpid still alive — ORPHAN)"
else
  ok "SIGTERM to run_bounded reaps the subtree (a framework interrupt leaves no orphan)"
fi

# ── 5. the pgid is DERIVED, not looked up — a getpgid() that always fails
#      cannot corrupt the passthrough (T-3e41) ────────────────────────────────
# Section 3 above only catches this bug when the race happens to be lost, which
# was roughly 1 run in 5–17 — a test that is red by luck is also green by luck.
# This one is deterministic: it replaces os.getpgid with a version that ALWAYS
# raises the same ProcessLookupError the kernel reports for a child that already
# exited. Code that asks the kernel for the pgid dies with rc 1 on EVERY run;
# code that derives it (pgid == pid, guaranteed by start_new_session) never calls
# it at all and still relays the child's 7. Put the old line back and this goes
# red immediately, not eventually.
mkdir -p "$WORK/hostile"
cat > "$WORK/hostile/sitecustomize.py" <<'HOSTILE'
import os


def _always_esrch(pid):
    raise ProcessLookupError(3, "No such process")


os.getpgid = _always_esrch
HOSTILE
mutant_live="$(PYTHONPATH="$WORK/hostile" python3 -c '
import os
try:
    os.getpgid(os.getpid())
except ProcessLookupError:
    print(1)
else:
    print(0)
' 2>/dev/null)"
check "the always-failing getpgid mutant is actually in effect (positive control)" "1" "$mutant_live"
PYTHONPATH="$WORK/hostile" python3 "$RB" 10 bash -c 'exit 7' >/dev/null 2>"$RBERR"; rc=$?
check "the exit code survives a getpgid() that always fails (pgid is derived, not looked up)" "7" "$rc"
[[ "$rc" == "7" ]] || show_rb_stderr

echo
echo "proc hygiene guard: $PASS ok, $FAIL failed"
[[ "$FAIL" == "0" ]] || exit 1
exit 0
