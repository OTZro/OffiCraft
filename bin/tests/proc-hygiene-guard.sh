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
CI_LOCK_LIB="$HERE/../lib/ci-lock.sh"
[[ -f "$CI_LOCK_LIB" ]] || { echo "FATAL: ci-lock.sh not found at $CI_LOCK_LIB" >&2; exit 2; }
# shellcheck source=../lib/ci-lock.sh
. "$CI_LOCK_LIB"   # only for its (pid, start time) discriminator — see below

PASS=0; FAIL=0
ok()  { PASS=$((PASS+1)); printf '  ok   — %s\n' "$1"; }
bad() { FAIL=$((FAIL+1)); printf '  FAIL — %s\n' "$1"; }
check(){ if [[ "$2" == "$3" ]]; then ok "$1"; else bad "$1 (want '$2' got '$3')"; fi; }

# ── pid identity: a bare pid is NOT one (T-3e41) ────────────────────────────
# This guard records a busy-loop's pid and asks LATER whether it is still
# running — and its cleanup SIGKILLs on that same answer. `kill -0 <pid>` cannot
# distinguish "my busy-loop is still burning a core" from "my busy-loop exited
# and the kernel handed its number to an unrelated process". Both directions are
# real damage: the first reports a phantom orphan (red for a fake reason, the
# very shape T-3e41 is about), the second sends SIGKILL to somebody else's
# process.
#
# The discriminator is the one this repo already uses for exactly this question,
# reused rather than reinvented: (pid, process START TIME) from
# bin/lib/ci-lock.sh — a recycled pid gets a different start time, the genuine
# process keeps its own. Each busy-loop records both fields ITSELF, while it is
# unambiguously the process in question; recording the start time from out here,
# after the fact, would be asking the same racy question again.
#
# Stricter than ci-lock's use in one deliberate way: ci_lock_holder_alive errs
# toward "still held" when ps cannot answer, because for a mutex a false
# "held" only costs a refusal. Here the answer authorises a SIGKILL, so no
# recorded start time means NOT identified, full stop.
same_proc() { # PID EXPECTED_START → 0 if that pid is still the process we recorded
  local pid="${1:-}" expected="${2:-}"
  [[ -n "$pid" && -n "$expected" ]] || return 1
  _ci_lock_holder_alive "$pid" "$expected"
}
rec_pid()   { awk -F'\t' 'NR==1{print $1}' "$1" 2>/dev/null; }
rec_start() { awk -F'\t' 'NR==1{print $2}' "$1" 2>/dev/null; }

WORK="$(mktemp -d -t oc-proc-hygiene-guard.XXXXXX)"
# Belt to the suspenders: if any assertion below leaves a recorded busy-loop
# alive (a red test), collect it here so the guard itself never leaks a core.
_cleanup() {
  local f p s
  for f in "$WORK"/gpid.*; do
    [[ -f "$f" ]] || continue
    p="$(rec_pid "$f")"; s="$(rec_start "$f")"
    if same_proc "$p" "$s"; then
      kill -KILL "$p" 2>/dev/null || true
    elif [[ -n "$p" && -z "$s" ]] && kill -0 "$p" 2>/dev/null; then
      # A pid with no recorded identity. It may be our busy-loop, or it may be
      # whatever inherited that number afterwards — and there is no way left to
      # tell. Killing on the guess is how an unrelated process dies; say it
      # loudly instead, with the number a human needs to look it up.
      printf '  WARN — recorded pid %s has no start time; NOT killing (identity unprovable)\n' "$p" >&2
    fi
  done
  rm -rf "$WORK"
}
trap '_cleanup' EXIT

# A child that forks a grandchild busy-loop (recording its IDENTITY — pid AND
# start time, written by the grandchild itself while it is unambiguously the
# process in question) then blocks. If run_bounded reaped only the direct child,
# the grandchild would keep burning a core — the exact orphan shape this ticket
# is about.
cat > "$WORK/leaky.sh" <<'LEAKY'
#!/usr/bin/env bash
pidfile="$1"
libsh="$2"
bash -c '
  . "$1" || exit 1
  printf "%s\t%s\n" "$$" "$(_ci_lock_ps "$$" lstart=)" > "$0"
  while :; do :; done
' "$pidfile" "$libsh" &
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
python3 "$RB" 2 bash "$WORK/leaky.sh" "$WORK/gpid.2" "$CI_LOCK_LIB" >/dev/null 2>&1; rc=$?
gpid="$(rec_pid "$WORK/gpid.2")"; gstart="$(rec_start "$WORK/gpid.2")"
check "the grandchild busy-loop was actually spawned (positive control)" "1" "$([[ -n "$gpid" ]] && echo 1 || echo 0)"
check "and it recorded its own identity, not just a pid (positive control)" "1" "$([[ -n "$gstart" ]] && echo 1 || echo 0)"
sleep 0.3
if same_proc "$gpid" "$gstart"; then
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
python3 "$RB" 30 bash "$WORK/leaky.sh" "$WORK/gpid.4" "$CI_LOCK_LIB" >/dev/null 2>&1 &
rbpid=$!
for _ in $(seq 1 50); do [[ -s "$WORK/gpid.4" ]] && break; sleep 0.1; done
kill -TERM "$rbpid" 2>/dev/null
wait "$rbpid" 2>/dev/null
gpid="$(rec_pid "$WORK/gpid.4")"; gstart="$(rec_start "$WORK/gpid.4")"
check "the grandchild was spawned before the interrupt (positive control)" "1" "$([[ -n "$gpid" ]] && echo 1 || echo 0)"
sleep 0.3
if same_proc "$gpid" "$gstart"; then
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

# ── 6. a recorded pid is matched by IDENTITY, not by number alone (T-3e41) ───
# Sections 2 and 4 conclude "no orphan survived" from a lookup on a pid recorded
# earlier, and _cleanup SIGKILLs on that same answer. If that lookup is a bare
# `kill -0`, then a busy-loop that exited and had its number handed to an
# unrelated process reads as STILL ALIVE: the guard reports a phantom orphan and
# its cleanup kills a stranger.
#
# This is the deterministic form of that situation — no race to win or lose. It
# asks the discriminator about a pid that certainly EXISTS (this very shell)
# while claiming a start time that certainly is not this shell's, which is
# exactly what a recycled pid looks like from the outside. The claimed value is
# a REAL, well-formed `ps` reading (pid 1, started at boot), so a pass cannot
# come from the expected string being unparseable junk.
#
# Put a bare-pid check back in same_proc and the load-bearing assertion below is
# red on EVERY run, not on the unlucky ones.
self_start="$(_ci_lock_ps "$$" lstart=)"
boot_start="$(_ci_lock_ps 1 lstart=)"
check "this shell's own start time is readable (positive control)" \
      "1" "$([[ -n "$self_start" ]] && echo 1 || echo 0)"
# Without this the discriminating assertion below could pass vacuously — two
# equal start times would make it a comparison of a value against itself.
check "the other process's start time is readable AND differs (positive control)" \
      "1" "$([[ -n "$boot_start" && "$boot_start" != "$self_start" ]] && echo 1 || echo 0)"

if same_proc "$$" "$boot_start"; then verdict=same; else verdict=different; fi
check "a LIVE pid whose start time is not the recorded one is NOT the recorded process (the recycled-pid case)" \
      "different" "$verdict"

if same_proc "$$" "$self_start"; then verdict=same; else verdict=different; fi
check "the same pid with its own start time still matches (the check has not just become 'always no')" \
      "same" "$verdict"

if same_proc "$$" ""; then verdict=same; else verdict=different; fi
check "a pid recorded without a start time is never claimed as identified (nothing is killed on a guess)" \
      "different" "$verdict"

echo
echo "proc hygiene guard: $PASS ok, $FAIL failed"
[[ "$FAIL" == "0" ]] || exit 1
exit 0
