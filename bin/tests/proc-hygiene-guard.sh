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
# Stricter than ci-lock's own use of it, deliberately, and NOT by delegating:
# _ci_lock_holder_alive answers "still held" when the start time is missing or
# `ps` cannot read it, because for a mutex a false "held" only costs a refusal.
# Here the same answer authorises a SIGKILL and decides whether an orphan is
# reported, so every unreadable case must come back NOT IDENTIFIED. Delegating
# and merely pre-checking the recorded value would leave the ps-failure branch
# in force — a live pid whose `ps` read fails would be declared ours and killed,
# which is the very defect this section removes.
same_proc() { # PID EXPECTED_START → 0 only if that pid is PROVABLY the process we recorded
  local pid="${1:-}" expected="${2:-}" live
  [[ "$pid" =~ ^[0-9]+$ ]] || return 1
  [[ -n "$expected" ]] || return 1
  kill -0 "$pid" 2>/dev/null || return 1
  live="$(_ci_lock_ps "$pid" lstart=)"   # the repo's ps reader: normalised, capped
  [[ -n "$live" ]] || return 1
  [[ "$live" == "$expected" ]]
}
rec_pid()   { awk -F'\t' 'NR==1{print $1}' "$1" 2>/dev/null; }
rec_start() { awk -F'\t' 'NR==1{print $2}' "$1" 2>/dev/null; }

WORK="$(mktemp -d -t oc-proc-hygiene-guard.XXXXXX)"
# Belt to the suspenders: if any assertion below leaves a recorded busy-loop
# alive (a red test), collect it here so the guard itself never leaks a core.
# reap_recorded FILE → SIGKILL the process that record names, but only if it is
# provably still the one we recorded. Split out of the EXIT trap on purpose: a
# trap cannot be called from a test, so while this decision lived inside
# _cleanup, deleting the kill left every assertion green and a busy-loop burning
# a core (observed, not theorised). Section 7 calls this directly.
# Returns 2 when the recorded pid is alive but unprovable — the caller decides
# how loud that is.
reap_recorded() {
  local f="$1" p s
  p="$(rec_pid "$f")"; s="$(rec_start "$f")"
  if same_proc "$p" "$s"; then
    kill -KILL "$p" 2>/dev/null || true
    return 0
  fi
  [[ -n "$p" ]] && kill -0 "$p" 2>/dev/null || return 0
  return 2
}

# backstop_collect PID → collect a process THIS RUN SPAWNED, without using any
# of the machinery under test, and print how it went: identified (the command
# line proved it), still-alive (nothing worked), nothing-to-do (already gone).
#
# Why a second path exists at all: section 7 deliberately spawns a busy-loop
# that only the identity machinery can collect — so when that machinery is
# wrong, which is the case section 7 exists for, the guard used to leave a core
# burning (observed with a mutant). A test must not depend on the thing it tests
# to stay safe. This path shares nothing with same_proc: the pid comes from a
# different reader, and the identity is this run's private mktemp path appearing
# in the process's own command line.
#
# The LAST RESORT at the end fires only when ps could not ANSWER — never when it
# answered and named someone else's process. That distinction is the whole
# licence: a backstop that dies with ps is not a backstop (this repo has already
# been burned by a ps field that does not exist everywhere), but killing a pid
# that ps has positively identified as foreign would be a bare-pid kill, which
# is what this file exists to argue against. Blind, yes; against the evidence,
# no. And it is scoped to a process this run spawned seconds ago in this same
# function, not to a record inherited from an earlier phase.
backstop_collect() {
  local pid="${1:-}" verdict=nothing-to-do
  [[ -n "$pid" ]] && kill -0 "$pid" 2>/dev/null || { printf '%s' "$verdict"; return 0; }
  local live_cmd
  live_cmd="$(ps -p "$pid" -o command= 2>/dev/null)"
  verdict=not-identified
  case "$live_cmd" in
    *"$WORK"*) kill -KILL "$pid" 2>/dev/null; sleep 0.3
               kill -0 "$pid" 2>/dev/null || verdict=identified ;;
  esac
  # LAST RESORT — only when ps could not ANSWER, never when it answered and
  # positively named someone else's process. Blind is the case this exists for
  # (a host whose ps cannot serve the field, which is the dependency this repo
  # has already been burned by); discarding a positive identification would make
  # this a bare-pid kill, the exact thing the file argues against.
  if [[ -z "$live_cmd" ]] && kill -0 "$pid" 2>/dev/null; then
    kill -KILL "$pid" 2>/dev/null; sleep 0.3
    if kill -0 "$pid" 2>/dev/null; then verdict=still-alive; else verdict=collected-blind; fi
  fi
  printf '%s' "$verdict"
}

_cleanup() {
  local f p s unprovable=0
  for f in "$WORK"/gpid.*; do
    [[ -f "$f" ]] || continue
    p="$(rec_pid "$f")"; s="$(rec_start "$f")"
    if ! reap_recorded "$f"; then
      # That number is alive but we cannot prove it is still OUR busy-loop —
      # the start time was never recorded, or no longer matches, or ps will not
      # answer. Both available moves are bad: SIGKILL on a guess is how an
      # unrelated process dies (the defect this section removes), and staying
      # quiet is how a core burns for two days (the incident this file exists
      # for). So do neither quietly: refuse the guess AND make the run RED, with
      # the ps line a human needs. A leak that turns the guard red gets acted
      # on; a leak reported to stderr under a green run does not.
      unprovable=1
      printf '  FAIL — recorded pid %s is alive but NOT provably ours; refusing to kill on a guess\n' "$p" >&2
      printf '         ps says: %s\n' "$(_ci_lock_ps "$p" command= 2>/dev/null)" >&2
      printf '         recorded start: [%s]  live start: [%s]\n' \
             "$s" "$(_ci_lock_ps "$p" lstart= 2>/dev/null)" >&2
    fi
  done
  rm -rf "$WORK"
  # An EXIT trap's own `exit` overrides the status the script was leaving with —
  # that is what makes this reachable at all, since cleanup runs after the
  # summary line has already been printed.
  [[ "$unprovable" == "0" ]] || exit 1
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
blockfor="${3:-30}"   # a parameter, NOT a sed-rewrite by the caller: a caller
                      # that rewrites this text gets a silent no-op the day the
                      # line drifts, and then leaks the child it thought it had
                      # shortened.
bash -c '
  . "$1" || exit 1
  printf "%s\t%s\n" "$$" "$(_ci_lock_ps "$$" lstart=)" > "$0"
  while :; do :; done
' "$pidfile" "$libsh" &
sleep "$blockfor"
LEAKY

# ── trap self-test hook (T-3e41) ────────────────────────────────────────────
# Section 8 drives _cleanup as a FUNCTION. That cannot see whether the function
# is still wired to EXIT, nor whether it acts on the REAL $WORK: rewire the trap
# to a no-op, or make _cleanup return early for anything but a fixture path, and
# all assertions stay green while two 100%-CPU orphans outlive the guard
# (observed, not theorised). Testing the wiring needs a real process to really
# exit — so on request this script sets up ONE record in its own real $WORK,
# prints the pid it is leaving behind, and exits immediately, letting whatever
# is actually installed on EXIT do whatever it actually does. Section 9 runs
# this and judges it from outside.
#
# The record is PREPARED BY THE CALLER and names a process the CALLER owns.
# A process this mode backgrounded for itself would be no good in either
# direction, and the reason is worth writing down because the obvious guess is
# wrong: a background job inherits the write end of a `$(…)` pipe, so a caller
# reading the child's output BLOCKS until that job exits (measured: a 5s sleep
# made the substitution take 5.19s, and `disown` changes nothing — it is a
# file-descriptor property, not a job-table one). Without the substitution the
# job simply outlives the child instead. So: hang, or leak. Either way the
# record must name a process the caller owns and can collect.
#
# Keyed on ARGV, never on the environment. An inherited variable would let a
# normal CI run enter this mode by accident — zero assertions, no output, exit
# 0, and bin/tests/run.sh printing "ok" — a bypass switch on a land gate, which
# bin/lib/ci-lock.sh's own header says must never exist. argv cannot be
# inherited. It is also the recursion's base case: §9 runs `bash "$0" --flag`,
# and a child that did not see the flag would run the whole suite again.
if [[ "${1:-}" == "--trap-selftest" ]]; then
  [[ -n "${2:-}" ]] || { echo "FATAL: --trap-selftest needs a record file" >&2; exit 2; }
  cp "$2" "$WORK/gpid.selftest" 2>/dev/null || exit 3
  exit 0
fi

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
python3 "$RB" 2 bash "$WORK/leaky.sh" "$WORK/gpid.2" "$CI_LOCK_LIB" 30 >/dev/null 2>&1; rc=$?
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
python3 "$RB" 30 bash "$WORK/leaky.sh" "$WORK/gpid.4" "$CI_LOCK_LIB" 30 >/dev/null 2>&1 &
rbpid=$!
for _ in $(seq 1 50); do [[ -s "$WORK/gpid.4" ]] && break; sleep 0.1; done
kill -TERM "$rbpid" 2>/dev/null
wait "$rbpid" 2>/dev/null
gpid="$(rec_pid "$WORK/gpid.4")"; gstart="$(rec_start "$WORK/gpid.4")"
check "the grandchild was spawned before the interrupt (positive control)" "1" "$([[ -n "$gpid" ]] && echo 1 || echo 0)"
# Without this, an empty start time would make same_proc below answer "not ours"
# for a busy-loop that is very much alive, and the ORPHAN check would pass by
# knowing nothing. Section 2 has the same control for the same reason.
check "and it recorded its own identity before the interrupt (positive control)" "1" "$([[ -n "$gstart" ]] && echo 1 || echo 0)"
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
# while claiming start times that certainly are not this shell's, which is
# exactly what a recycled pid looks like from the outside.
#
# The claimed values are FABRICATED, the way bin/tests/ci-lock-guard.sh already
# fabricates one for the same question. Reading a real one off another process
# (pid 1 was tried) makes the test depend on the host: inside a PID namespace
# the shell running this guard IS pid 1, so the comparison becomes a value
# against itself and the guard reddens for a reason that has nothing to do with
# run_bounded — the exact anti-pattern this ticket is about.
#
# Put a bare-pid check back in same_proc and the load-bearing assertions below
# are red on EVERY run, not on the unlucky ones.
FAKE_START="Thu Jan  1 00:00:00 2026"
self_start="$(_ci_lock_ps "$$" lstart=)"
# Also the host-capability check this guard now depends on: no `ps -o lstart=`,
# no discriminator. Better to say so here than to let every assertion below
# quietly answer "not ours".
check "this shell's own start time is readable (ps -o lstart= is supported here)" \
      "1" "$([[ -n "$self_start" ]] && echo 1 || echo 0)"
check "the fabricated start time is not this shell's (positive control against a vacuous compare)" \
      "1" "$([[ "$FAKE_START" != "$self_start" ]] && echo 1 || echo 0)"

if same_proc "$$" "$FAKE_START"; then verdict=same; else verdict=different; fi
check "a LIVE pid whose start time is not the recorded one is NOT the recorded process (the recycled-pid case)" \
      "different" "$verdict"

# Resolution matters, not just "some difference". A comparison truncated to the
# minute or the day still passes the assertion above while treating two
# processes that started seconds apart as the same one — and seconds is exactly
# the window a recycled pid arrives in. This feeds it a value that differs from
# the real one ONLY in the seconds field.
sec_shifted="$(printf '%s' "$self_start" | awk '{ if (match($0, /[0-9][0-9]:[0-9][0-9]:[0-9][0-9]/)) { t=substr($0,RSTART,RLENGTH); s=substr(t,7,2)+0; s=(s+30)%60; nt=substr(t,1,6) sprintf("%02d", s); print substr($0,1,RSTART-1) nt substr($0,RSTART+RLENGTH) } else print "" }')"
check "a seconds-only variant of this shell's start time could be built (positive control)" \
      "1" "$([[ -n "$sec_shifted" && "$sec_shifted" != "$self_start" ]] && echo 1 || echo 0)"
if same_proc "$$" "$sec_shifted"; then verdict=same; else verdict=different; fi
check "a start time differing ONLY in the seconds is judged NOT the recorded process (the comparison keeps its resolution)" \
      "different" "$verdict"

# The same weakening can hide in the DATE half instead of the time half — a
# comparison of only the clock time reads two processes started an exact day
# apart as one, and "normalise away a BSD/GNU date-format difference" is a
# plausible edit that would do it.
day_shifted="$(printf '%s' "$self_start" | awk '{ if (match($0, /[0-9]{4}$/)) print substr($0,1,RSTART-1) (substr($0,RSTART)+1); else print "" }')"
check "a date-only variant of this shell's start time could be built (positive control)" \
      "1" "$([[ -n "$day_shifted" && "$day_shifted" != "$self_start" ]] && echo 1 || echo 0)"
if same_proc "$$" "$day_shifted"; then verdict=same; else verdict=different; fi
check "a start time differing ONLY in the date is judged NOT the recorded process (the comparison keeps the date too)" \
      "different" "$verdict"

if same_proc "$$" "$self_start"; then verdict=same; else verdict=different; fi
check "the same pid with its own start time still matches (the check has not just become 'always no')" \
      "same" "$verdict"

if same_proc "$$" ""; then verdict=same; else verdict=different; fi
check "a pid recorded without a start time is never claimed as identified (nothing is killed on a guess)" \
      "different" "$verdict"

# And the failure mode this guard must not inherit from the lock library it
# borrows from: there, a `ps` that cannot answer means "assume still held",
# because a mutex pays for that with a refusal. Here the same answer authorises
# a SIGKILL. Deterministic: put a `ps` that always fails at the front of PATH.
mkdir -p "$WORK/psfail"
printf '#!/bin/sh\nexit 1\n' > "$WORK/psfail/ps"
chmod +x "$WORK/psfail/ps"
_saved_path="$PATH"
PATH="$WORK/psfail:$PATH"
if [[ -n "$(_ci_lock_ps "$$" lstart=)" ]]; then psfail_live=0; else psfail_live=1; fi
if same_proc "$$" "$self_start"; then verdict=same; else verdict=different; fi
PATH="$_saved_path"
check "the always-failing ps shim is actually in effect (positive control)" "1" "$psfail_live"
check "a pid whose start time cannot be READ is not claimed as identified either (no SIGKILL on an unreadable process)" \
      "different" "$verdict"

# ── 7. the ORPHAN checks are not vacuous — a real leak IS reported ───────────
# Sections 2 and 4 only ever assert the ABSENCE of an orphan, and an absence is
# what a broken detector reports too. Reviewers demonstrated this: swap the two
# fields this guard reads back from the record and all assertions stay green
# while two busy-loops burn a core each. So: deliberately break the reaping,
# and require the detector to SEE the leak it is supposed to see.
mkdir -p "$WORK/nokill"
cat > "$WORK/nokill/sitecustomize.py" <<'NOKILL'
import os


def _noop(pgid, sig):
    return None


os.killpg = _noop
NOKILL
# Same leaky child, but its blocking parent exits on its own in 2s, so the only
# thing this section can leave behind is the busy-loop it is about — which it
# kills below, by identity, before moving on.
: > "$WORK/gpid.7"
PYTHONPATH="$WORK/nokill" python3 "$RB" 1 bash "$WORK/leaky.sh" "$WORK/gpid.7" "$CI_LOCK_LIB" 2 >/dev/null 2>&1
gpid="$(rec_pid "$WORK/gpid.7")"; gstart="$(rec_start "$WORK/gpid.7")"
check "the leaked busy-loop recorded its identity (positive control)" \
      "1" "$([[ -n "$gpid" && -n "$gstart" ]] && echo 1 || echo 0)"
sleep 0.3
if same_proc "$gpid" "$gstart"; then verdict=reported; else verdict=missed; fi
check "a busy-loop that really DID survive is reported as still ours (negative control: the ORPHAN checks can still see one)" \
      "reported" "$verdict"
# Cleaned up through the SAME code path the EXIT trap uses, so this also pins
# that the reaping decision still kills: delete the kill from reap_recorded and
# this assertion is the one that notices, instead of a core burning unnoticed
# under a green run (which is exactly what happened while the kill lived inside
# the trap where no test could reach it).
reap_recorded "$WORK/gpid.7" || true
sleep 0.3
if same_proc "$gpid" "$gstart"; then verdict=alive; else verdict=gone; fi
check "and the reaping path actually kills it (the deliberate leak is gone, by identity)" "gone" "$verdict"

# BACKSTOP — this section must not depend on the machinery it is testing to
# clean up after itself. When the identity code is wrong (the case this section
# exists for) the assertion above goes red AND the busy-loop it deliberately
# spawned survives, burning a core; observed with the field-swap mutant, where
# the trap could not collect it either. So collect it here through a path that
# shares nothing with what is under test: the pid read straight out of the file
# with a different reader, and an identity check that is not same_proc — this
# run's private $WORK path, which mktemp made unique and which appears in the
# process's own command line. That is proof enough to kill a process this
# section spawned two seconds ago inside this same function; the refuse-to-guess
# policy governs records inherited from earlier phases, not this one.
IFS=$'\t' read -r backstop_pid _ < "$WORK/gpid.7" 2>/dev/null || backstop_pid=""
check "the backstop read a pid out of the record (positive control: an empty read collects nothing, silently)" \
      "1" "$([[ "$backstop_pid" =~ ^[0-9]+$ ]] && echo 1 || echo 0)"
                                    # pure bash: a broken `cut` used to make this
                                    # empty, and then the deliberate leak below
                                    # was never collected
backstop="$(backstop_collect "$backstop_pid")"
if kill -0 "$backstop_pid" 2>/dev/null; then leftover=yes; else leftover=no; fi
check "section 7 leaves no busy-loop behind, whatever its own assertions did" "no" "$leftover"

# On a healthy run the backstop has nothing to do — reap_recorded got there
# first — so the assertion above cannot tell a working backstop from a deleted
# one. Give it its own subject: a busy-loop that is never offered to the
# identity path at all, so the backstop is the only thing that can collect it,
# and require it to do so by IDENTITY rather than by the last resort.
: > "$WORK/rec-backstop"
bash -c '
  . "$1" || exit 1
  printf "%s\t%s\n" "$$" "$(_ci_lock_ps "$$" lstart=)" > "$0"
  while :; do :; done
' "$WORK/rec-backstop" "$CI_LOCK_LIB" &
bsfix=$!
disown "$bsfix" 2>/dev/null || true
for _ in $(seq 1 50); do [[ -s "$WORK/rec-backstop" ]] && break; sleep 0.1; done
check "the backstop fixture busy-loop is running (positive control)" \
      "1" "$(kill -0 "$bsfix" 2>/dev/null && echo 1 || echo 0)"
IFS=$'\t' read -r bsfix_pid _ < "$WORK/rec-backstop" 2>/dev/null || bsfix_pid=""
bsverdict="$(backstop_collect "$bsfix_pid")"
check "the backstop identifies a busy-loop by this run's own WORK path and collects it" \
      "identified" "$bsverdict"
kill -KILL "$bsfix" 2>/dev/null   # unconditional: our own pid from $!

# The last resort has its own state, and nothing above reaches it: on a healthy
# host `ps` answers, so the identity branch always gets there first. Reproduce
# the host this exists for — `ps` present but unable to answer — and require the
# process to be collected anyway. Without this, deleting the last resort (or
# emptying the pid reader) leaves every assertion green while the ps-broken host
# leaks a core, which is exactly the round-3 finding this was written to close.
: > "$WORK/rec-blind"
bash -c '
  . "$1" || exit 1
  printf "%s\t%s\n" "$$" "$(_ci_lock_ps "$$" lstart=)" > "$0"
  while :; do :; done
' "$WORK/rec-blind" "$CI_LOCK_LIB" &
blindfix=$!
disown "$blindfix" 2>/dev/null || true
for _ in $(seq 1 50); do [[ -s "$WORK/rec-blind" ]] && break; sleep 0.1; done
cp "$WORK/rec-blind" "$WORK/gpid.shadow-d" 2>/dev/null || true
check "the blind-collect fixture busy-loop is running (positive control)" \
      "1" "$(kill -0 "$blindfix" 2>/dev/null && echo 1 || echo 0)"
IFS=$'\t' read -r blind_pid _ < "$WORK/rec-blind" 2>/dev/null || blind_pid=""
_saved_path="$PATH"
PATH="$WORK/psfail:$PATH"          # the same always-failing ps shim section 6 uses
blindverdict="$(backstop_collect "$blind_pid")"
PATH="$_saved_path"
check "with a ps that cannot answer, the backstop still collects what this run spawned" \
      "collected-blind" "$blindverdict"
if kill -0 "$blindfix" 2>/dev/null; then verdict=alive; else verdict=gone; fi
check "and that busy-loop is really gone (not merely reported as collected)" "gone" "$verdict"
kill -KILL "$blindfix" 2>/dev/null
rm -f "$WORK/gpid.shadow-d"

# ── 8. the EXIT trap's decision itself, driven from a test ───────────────────
# Section 7 covers reap_recorded. The trap around it — which records it walks,
# whether it inverts the answer, and the `exit 1` that turns an uncollectable
# survivor into a red run — was covered by nothing: deleting any of those left
# 24 green assertions and, in one case, two 100%-CPU orphans outliving the
# guard. A trap cannot be called, but the function it runs can: drive _cleanup
# in a SUBSHELL over a fixture directory, so its `exit 1` becomes an observable
# status and its `rm -rf` only removes the fixture.
FIXA="$WORK/fixture-unprovable"; mkdir -p "$FIXA"
sleep 20 & standin=$!
disown "$standin" 2>/dev/null || true
# A record naming a process that is alive but demonstrably NOT the one recorded.
printf '%s\t%s\n' "$standin" "$FAKE_START" > "$FIXA/gpid.fixture"
cp "$FIXA/gpid.fixture" "$WORK/gpid.shadow-a"   # so the LIVE trap can collect it if we are killed here
( WORK="$FIXA"; _cleanup ) >/dev/null 2>&1; crc=$?
check "an alive-but-unprovable survivor makes the exit trap fail the run (rc 1)" "1" "$crc"
if kill -0 "$standin" 2>/dev/null; then verdict=alive; else verdict=killed; fi
check "and the trap did NOT kill it on the guess (positive control: it was still there to check)" "alive" "$verdict"
kill -KILL "$standin" 2>/dev/null

FIXB="$WORK/fixture-ours"; mkdir -p "$FIXB"
: > "$FIXB/gpid.fixture"
# A real busy-loop of ours, recorded the same way the leaky child records itself.
bash -c '
  . "$1" || exit 1
  printf "%s\t%s\n" "$$" "$(_ci_lock_ps "$$" lstart=)" > "$0"
  while :; do :; done
' "$FIXB/gpid.fixture" "$CI_LOCK_LIB" &
fixpid=$!
disown "$fixpid" 2>/dev/null || true
for _ in $(seq 1 50); do [[ -s "$FIXB/gpid.fixture" ]] && break; sleep 0.1; done
cp "$FIXB/gpid.fixture" "$WORK/gpid.shadow-b"   # same reason: the trap's glob is not recursive
check "the fixture busy-loop recorded its identity (positive control)" \
      "1" "$([[ -n "$(rec_start "$FIXB/gpid.fixture")" ]] && echo 1 || echo 0)"
( WORK="$FIXB"; _cleanup ) >/dev/null 2>&1; crc=$?
sleep 0.3
if kill -0 "$fixpid" 2>/dev/null; then verdict=alive; else verdict=killed; fi
check "the exit trap collects a survivor it CAN identify" "killed" "$verdict"
check "and says nothing is wrong when it could collect everything (rc 0)" "0" "$crc"
kill -KILL "$fixpid" 2>/dev/null   # backstop: our own pid from $!, no identity code involved
rm -f "$WORK"/gpid.shadow-*        # the shadow records below are no longer needed

# ── 9. the trap is actually WIRED, and acts on the real $WORK ────────────────
# Section 8 proves what _cleanup decides. It cannot prove that _cleanup still
# runs on exit, or that it runs on this run's own $WORK — rewire the trap to a
# no-op, point it at INT instead of EXIT, or make _cleanup return early unless
# $WORK looks like a fixture, and every assertion above stays green while real
# orphans outlive the guard. Only a real exit can show that. This runs the
# script again in a mode that leaves ONE record behind and exits immediately,
# then judges what its EXIT trap did from out here.
# (a) a record the child CAN identify: its exit must collect the process.
: > "$WORK/rec-wire-ours"
bash -c '
  . "$1" || exit 1
  printf "%s\t%s\n" "$$" "$(_ci_lock_ps "$$" lstart=)" > "$0"
  while :; do :; done
' "$WORK/rec-wire-ours" "$CI_LOCK_LIB" &
wirepid=$!
disown "$wirepid" 2>/dev/null || true
for _ in $(seq 1 50); do [[ -s "$WORK/rec-wire-ours" ]] && break; sleep 0.1; done
cp "$WORK/rec-wire-ours" "$WORK/gpid.shadow-c" 2>/dev/null || true   # the live trap's
                                    # glob is not recursive and rec-wire-* is
                                    # deliberately not gpid.*; without this, a
                                    # signal landing here leaks this busy-loop
check "the wiring fixture busy-loop is running and recorded its identity (positive control)" \
      "1" "$([[ -n "$(rec_start "$WORK/rec-wire-ours")" ]] && kill -0 "$wirepid" 2>/dev/null && echo 1 || echo 0)"
bash "$0" --trap-selftest "$WORK/rec-wire-ours" >/dev/null 2>&1; strc=$?
sleep 0.5
if kill -0 "$wirepid" 2>/dev/null; then verdict=alive; else verdict=collected; fi
check "the EXIT trap is really installed and really runs on the run's own WORK dir (a child's exit collects it)" \
      "collected" "$verdict"
check "and that child exited 0, having had nothing it could not collect" "0" "$strc"
kill -KILL "$wirepid" 2>/dev/null   # unconditional: our own pid from $!, no identity code

# (b) a record the child CANNOT identify: its exit must refuse to kill, and go red.
# NOTE the file name — deliberately not gpid.*, so this run's own trap does not
# adopt the stand-in and redden this run at exit. We collect it ourselves below.
sleep 20 &
wirestandin=$!
disown "$wirestandin" 2>/dev/null || true
printf '%s\t%s\n' "$wirestandin" "$FAKE_START" > "$WORK/rec-wire-unprovable"
check "the stand-in for the unidentifiable record is running (positive control)" \
      "1" "$(kill -0 "$wirestandin" 2>/dev/null && echo 1 || echo 0)"
bash "$0" --trap-selftest "$WORK/rec-wire-unprovable" >/dev/null 2>&1; strc2=$?
check "a child that cannot identify its survivor exits NON-ZERO through the trap" "1" "$strc2"
if kill -0 "$wirestandin" 2>/dev/null; then verdict=alive; else verdict=killed; fi
check "and it did not kill the stand-in on a guess" "alive" "$verdict"
kill -KILL "$wirestandin" 2>/dev/null

# ── 10. the trap is still wired HERE, at the end ────────────────────────────
# Section 9 can only see the trap as it was when the self-test child exited,
# hundreds of lines above. Re-arm it to a no-op after that point and every
# assertion still passes while real orphans outlive the guard. No runtime probe
# can catch that — a script cannot observe its own exit — but the installed trap
# is readable as data right up to the last line.
# The self-test mode is entered from ARGV only. If an ENVIRONMENT variable
# could do it, a normal CI run that happened to inherit it would take the early
# exit: zero assertions, no output, exit 0 — and bin/tests/run.sh would print
# "ok" for a suite that ran nothing. That is a bypass switch on a land gate, the
# thing bin/lib/ci-lock.sh's header says must never exist. Checked as source
# text because the failure is the ABSENCE of a run: there is no output to
# inspect, and a runtime probe would have to re-run the whole suite to see it.
# The needle is assembled at runtime so this line is not itself a hit — the
# first version of this check failed against its own grep pattern.
env_needle="$(printf 'OC_%s' 'PH')"
check "the self-test mode is keyed on argv, never on an environment variable" \
      "0" "$(grep -c "$env_needle" "$0" || true)"

trap_now="$(trap -p EXIT 2>/dev/null)"
check "the EXIT trap still names _cleanup at the end of the run (nothing disarmed it later)" \
      "1" "$(case "$trap_now" in *_cleanup*) echo 1 ;; *) echo 0 ;; esac)"

echo
echo "proc hygiene guard: $PASS ok, $FAIL failed"
[[ "$FAIL" == "0" ]] || exit 1
exit 0
