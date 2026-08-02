#!/usr/bin/env bash
# bin/lib/ci-lock.sh — the per-WORKING-COPY mutex for bin/ci.sh.
#
# WHY THIS EXISTS
# ---------------
# Two concurrent `bash bin/ci.sh` runs IN THE SAME CLONE fight over shared,
# in-place state: frontend/node_modules (step 4's `npm ci` deletes and refills
# it under the other run's feet), bin/../*dist staging (build-seedsdist /
# build-docsdist / build-bindist all write the same paths), and FIVE committed
# generated files that step 4b1/4b2/4b3 regenerate IN PLACE and then byte-compare
# against a backup they took themselves. Interleave two runs and those steps
# compare A's backup against B's regeneration.
#
# The failure direction is NOT fixed. Sometimes it reddens (a diff that is real
# noise); sometimes it goes GREEN on a tree the run never actually validated,
# because the other run happened to restore the file before the compare. The
# false GREEN is the shape worth killing: `[ci] all green` is this repo's land
# authority, and an authority that can be emitted by a run whose gates raced is
# not an authority at all.
#
# THE RULE (owner ruling, T-70c9 / card rc-bbf6a418fc23):
#   One run per working copy. A second run in the same copy is REFUSED, loudly,
#   with a non-zero exit. Want more rounds at once? Use more copies.
#
# WHY THE LOCK IS BOUND TO THE CLONE, NOT TO THE MACHINE
# ------------------------------------------------------
# Nothing in the list above is machine-global — every one of those paths lives
# under the clone's own ROOT. Two INDEPENDENT clones share none of them, and
# concurrent full-CI runs across separate clones have been observed to complete
# green with a ~7-minute overlap. (Honest boundary: that observation is ONE pair,
# ONE time. It is evidence that cross-clone is the right shape, not proof that
# any degree of cross-clone parallelism is safe — see docs/dev/README.md.) So the
# lock path is derived from ROOT: `$ROOT/.ci-lock`. A machine-global lock would
# have thrown away the only kind of parallelism that works.
#
# DELIBERATELY NOT DONE: making a single clone worktree-safe. The owner ruled
# that out explicitly. This file makes the collision LOUD; it does not make it
# survivable.
#
# NO BYPASS SWITCH — ON PURPOSE
# -----------------------------
# There is no environment variable, no flag, no "I know what I'm doing" escape
# hatch, and the refusal message must never grow one. An escape hatch on a guard
# like this is used by exactly the person who should not be using it, under
# exactly the deadline pressure that makes them not read the reason.
#
# STALE LOCKS / CRASH RECOVERY, AND PID REUSE
# -------------------------------------------
# A lock left behind by a run that was ctrl-C'd, `kill -9`'d, or lost to a crash
# must not wedge the clone forever, so `ci_lock_acquire` takes over a lock whose
# holder is gone. "Gone" cannot be answered by `kill -0` alone: pids RECYCLE, and
# a recycled pid makes the check wrong in BOTH directions —
#   * recycled to an unrelated process  ⇒ read as "still held" ⇒ the clone is
#     wedged until a human deletes a directory;
#   * the real holder is alive           ⇒ must never be read as stale, or a
#     second run walks in and we are back to the racing-CI bug.
# The discriminator is the holder's PROCESS START TIME (`ps -o lstart=`), recorded
# when the lock is taken and compared on every later probe. (pid, start time) is
# effectively unique: a recycled pid gets a different start time, so it reads as
# stale; the genuine holder keeps its own, so it reads as held.
#
# NOT the command line, which was the first thing tried and is WRONG: a process
# can change it. Observed while building this — bash exec-optimises its final
# command, so a holder launched as `bash -c '…; sleep 600'` reports `bash -c …`
# at acquire time and `sleep 600` a moment later, and a cmdline comparison then
# declares the LIVE holder stale. That is the dangerous direction. The command
# line is still recorded, but ONLY as human-readable text in the refusal message;
# nothing decides anything from it.
#
# RESIDUAL: `lstart` has one-second resolution, so a pid recycled into a process
# that started in the same clock second as the original holder would still read
# as held. That is a wedged clone (the safe direction), not a second run.
# If a lock ever survives all of that, the manual recovery is documented in
# docs/dev/README.md — deliberately NOT in the refusal message, because a
# refusal that ships its own workaround is a suggestion, not a guard.

# _ci_lock_ps PID FIELD → one ps field for a pid, NORMALISED onto a single short
# line (empty if ps cannot answer).
#
# Normalising is load-bearing, not cosmetic. The owner record is tab-separated,
# one field per LINE, so any tab or newline inside a value silently corrupts it —
# and a corrupt record is misread as "no holder", i.e. it fails in the direction
# that lets a second run in. macOS `ps` also renders an embedded newline in
# `command=` as a literal backslash escape and can return kilobytes for a
# harness-launched run, hence the length cap too. BOTH the record and every later
# comparison go through this one function, so the two sides can never disagree
# about their own formatting.
_ci_lock_ps() {
  ps -p "$1" -o "$2" 2>/dev/null | tr '\n\t' '  ' | sed 's/  */ /g; s/^ //; s/ $//' | cut -c1-200
}

# _ci_lock_holder_alive PID EXPECTED_START → 0 if that pid is still the run that
# took the lock. Two questions, both required:
#   1. does the pid exist at all (`kill -0`)?
#   2. is it the SAME INSTANCE of that pid, i.e. does its process start time still
#      equal the one recorded when the lock was taken?
# (2) is the pid-reuse discriminator. See the header for why start time and not
# the command line — a command line can change under you; a start time cannot.
_ci_lock_holder_alive() {
  local pid="$1" expected="$2" live
  [[ "$pid" =~ ^[0-9]+$ ]] || return 1
  kill -0 "$pid" 2>/dev/null || return 1
  # If we never recorded a start time, or ps cannot answer, fall back to the
  # kill -0 answer above. Erring toward "held" is the safe direction: the cost is
  # a refusal the user can resolve, versus two runs corrupting one tree.
  [[ -n "$expected" ]] || return 0
  live="$(_ci_lock_ps "$pid" lstart=)"
  [[ -n "$live" ]] || return 0
  [[ "$live" == "$expected" ]]
}

# _ci_lock_read_owner DIR → prints "pid<TAB>started<TAB>root<TAB>cmd"
# The owner file is written immediately after mkdir, but "immediately" is not
# "atomically": a competing run can lose the mkdir race and read the directory
# while it is still empty. Poll briefly rather than concluding anything from one
# empty read — concluding "stale" there would hand the lock to a second run while
# the first is alive, which is the exact outcome this file exists to prevent.
_ci_lock_read_owner() {
  local dir="$1" i
  for i in $(seq 1 20); do
    if [[ -s "$dir/owner" ]]; then
      cat "$dir/owner"
      return 0
    fi
    sleep 0.1
  done
  return 1
}

# ci_lock_acquire ROOT — take the working copy's CI lock or REFUSE and exit 1.
#
# On success it exports nothing the rest of CI has to know about; it only records
# the two shell variables ci_lock_release needs. `mkdir` is the arbiter: it is
# atomic on every filesystem this repo is cloned onto, and unlike a lockfile
# created with `>` it cannot be won by two processes at once.
ci_lock_acquire() {
  local root="$1"
  local dir="$root/.ci-lock"
  local self_cmd self_start owner pid started held_root held_cmd held_start attempt

  self_cmd="$(_ci_lock_ps "$$" command=)"
  self_start="$(_ci_lock_ps "$$" lstart=)"

  for attempt in 1 2; do
    if mkdir "$dir" 2>/dev/null; then
      # We own it. Record the flag BEFORE writing the owner file so a crash in
      # between still releases the directory we created (and only that one).
      _CI_LOCK_DIR="$dir"
      _CI_LOCK_HELD=1
      printf 'pid\t%s\nstarted\t%s\nroot\t%s\ncmd\t%s\npstart\t%s\n' \
        "$$" "$(date -u '+%Y-%m-%dT%H:%M:%SZ')" "$root" "$self_cmd" "$self_start" \
        > "$dir/owner"
      return 0
    fi

    # mkdir lost. Either a live run holds it, or a dead run left it behind.
    if ! owner="$(_ci_lock_read_owner "$dir")"; then
      # Held for >2s with no owner record at all: the only way to reach this is a
      # run that died between mkdir and the write above. Treat as stale.
      pid=""; started="unknown"; held_root="$root"; held_cmd=""; held_start=""
    else
      pid="$(printf '%s\n' "$owner"      | awk -F'\t' '$1=="pid"     {print $2}')"
      started="$(printf '%s\n' "$owner"  | awk -F'\t' '$1=="started" {print $2}')"
      held_root="$(printf '%s\n' "$owner"| awk -F'\t' '$1=="root"    {print $2}')"
      held_cmd="$(printf '%s\n' "$owner" | awk -F'\t' '$1=="cmd"     {print $2}')"
      held_start="$(printf '%s\n' "$owner"| awk -F'\t' '$1=="pstart"  {print $2}')"
    fi

    if _ci_lock_holder_alive "$pid" "$held_start"; then
      # ── the loud refusal ──────────────────────────────────────────────────
      # Everything a reader needs to act: WHICH copy is busy, WHICH process, and
      # what to do instead. No bypass, by design.
      echo "[ci] REFUSED — this working copy is already running CI."                >&2
      echo "[ci]   working copy : $held_root"                                       >&2
      echo "[ci]   held by pid  : ${pid:-unknown}  (started $started UTC)"          >&2
      echo "[ci]   command      : ${held_cmd:-unknown}"                             >&2
      echo "[ci]"                                                                   >&2
      echo "[ci] One CI run per working copy. Two runs in the SAME copy overwrite"  >&2
      echo "[ci] each other's node_modules, staged dist assets and regenerated"     >&2
      echo "[ci] files mid-compare — which can go RED for a fake reason and can"    >&2
      echo "[ci] just as easily go GREEN on a tree that was never validated."       >&2
      echo "[ci]"                                                                   >&2
      echo "[ci] To run another round at the same time, use another COPY:"          >&2
      echo "[ci]   git clone <this repo> /path/to/another-copy"                     >&2
      echo "[ci]   cd /path/to/another-copy && bash bin/ci.sh"                      >&2
      echo "[ci] Concurrent runs in SEPARATE clones are the supported way."         >&2
      exit 1
    fi

    # Stale. Move it aside first so two racing reclaimers cannot both delete and
    # both recreate: whoever's `mv` succeeds clears the path, and the retry's
    # `mkdir` is still the single atomic arbiter for who actually gets the lock.
    echo "[ci] stale CI lock from pid ${pid:-unknown} (no longer running) — taking it over" >&2
    mv "$dir" "$dir.stale.$$" 2>/dev/null && rm -rf "$dir.stale.$$"
  done

  echo "[ci] REFUSED — could not take the CI lock at $dir after reclaiming a stale one." >&2
  echo "[ci] Another run took it in the meantime. Re-run, or use another copy."          >&2
  exit 1
}

# ci_lock_release — drop the lock, but ONLY if this shell is the one that made it.
# Guarded by _CI_LOCK_HELD (set only on a winning mkdir) so a REFUSED run's exit
# path can never delete the live holder's lock. Always returns 0: this runs from
# an EXIT trap and must not perturb the script's exit status.
ci_lock_release() {
  if [[ "${_CI_LOCK_HELD:-0}" == "1" && -n "${_CI_LOCK_DIR:-}" ]]; then
    rm -rf "$_CI_LOCK_DIR"
    _CI_LOCK_HELD=0
  fi
  return 0
}
