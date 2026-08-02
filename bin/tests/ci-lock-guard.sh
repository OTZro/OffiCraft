#!/usr/bin/env bash
# bin/tests/ci-lock-guard.sh — pins the per-working-copy CI mutex (T-70c9).
#
# WHAT IS BEING GUARDED
# ---------------------
# Owner ruling (card rc-bbf6a418fc23): one CI run per working copy; a second run
# in the same copy is REFUSED loudly with a non-zero exit; more rounds means more
# copies. The dangerous failure this replaces is NOT "two runs both go red" — it
# is two runs interleaving and one of them emitting `[ci] all green`, the repo's
# land authority, over a tree it never actually validated.
#
# HOW IT IS TESTED, AND WHY NOT END-TO-END
# ----------------------------------------
# This guard does NOT start two real `bin/ci.sh` runs. It cannot: verifying a
# mutant means DISABLING the very lock under test, and a test that then races two
# full CI runs through one clone would corrupt the developer's working tree —
# a test whose safety depends on the thing it is testing is a bomb with a
# schedule, not a test.
#
# Instead it drives the EXACT code path ci.sh uses (`bin/lib/ci-lock.sh`,
# sourced, same functions, same arguments) against THROWAWAY directories, with a
# lightweight stand-in process playing the role of the first run. Everything
# asserted below is an observed rc and observed bytes on stderr, never "some
# error happened".
#
# RESIDUALS — say them plainly.
#  1. The behavioural half proves the LOCK works. It cannot prove ci.sh CALLS
#     it; that half is a static wiring assertion. Deleting the acquire call from
#     ci.sh reddens on the static half; deleting the lock logic reddens on the
#     behavioural half. Neither is redundant, and the static half is the weaker.
#  2. The prologue-ordering check is a MUST-BE-EMPTY scan rather than a
#     line-number comparison against one named anchor, so a NEWLY added write
#     above the acquire reddens even though this file has never heard of it. It
#     is still only as wide as its pattern list: a tree-mutating command whose
#     shape is not in that regex (a helper script that writes, a python one-liner)
#     passes. Widen the pattern rather than adding another anchor.
#  3. Nothing here runs two real CI rounds. See above for why that is deliberate.
#     The end-to-end gap it leaves was closed ONCE, BY HAND, and the result is
#     recorded here rather than automated:
#       2026-08-02 — with the lock held in a real clone, `bash bin/ci.sh` exited
#       rc=1 and its first stderr line was, verbatim,
#       `[ci] REFUSED — this working copy is already running CI.`
#       (evidence: 01-cish-refused.log)
#     It stays manual ON PURPOSE. Automating it means the mutant check would run
#     it with the lock DISABLED, at which point the assertion starts a real full
#     CI round in the developer's clone concurrently with another — the exact
#     corruption this lock exists to prevent, triggered by its own test.
#  4. Both halves are pinned: negative controls (mutate the lock out, go red) AND
#     a sentinel half (section 9 — the lock must NOT have spread to any other
#     entry point, because narrowing conformance/run.sh's parallel-rounds
#     capability would be a regression that every negative control misses).
set -uo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT="$(cd "$HERE/../.." && pwd)"
LIB="$ROOT/bin/lib/ci-lock.sh"
CI="$ROOT/bin/ci.sh"

PASS=0
FAIL=0
ok()  { PASS=$((PASS + 1)); printf '  ok   — %s\n' "$1"; }
bad() { FAIL=$((FAIL + 1)); printf '  FAIL — %s\n' "$1" >&2; }

echo "ci per-working-copy lock guard"

if [[ ! -f "$LIB" ]]; then
  echo "  FAIL — bin/lib/ci-lock.sh is missing; the CI mutex is gone entirely" >&2
  exit 1
fi

WORK="$(mktemp -d -t oc-ci-lock-guard.XXXXXX)"
STANDIN_PIDS=()
cleanup() {
  local p
  for p in ${STANDIN_PIDS+"${STANDIN_PIDS[@]}"}; do
    [[ -n "$p" ]] && kill -TERM "$p" 2>/dev/null
  done
  rm -rf "$WORK"
}
trap cleanup EXIT

# A stand-in for "a CI run that is currently in progress in this copy": it takes
# the real lock through the real function, announces readiness, then parks. Being
# a REAL live process is the point — the staleness logic asks the OS about it.
cat > "$WORK/standin.sh" <<STANDIN
#!/usr/bin/env bash
set -uo pipefail
source "$LIB"
ci_lock_acquire "\$1"
touch "\$1/.standin-ready"
sleep 600
STANDIN
chmod +x "$WORK/standin.sh"

# start_standin COPYDIR — sets STANDIN_PID to the holder's pid.
# It sets a GLOBAL rather than echoing, on purpose: `pid=$(start_standin …)` runs
# the function in a subshell, so the `STANDIN_PIDS+=` bookkeeping cleanup relies
# on would be discarded and every stand-in would leak past this guard.
STANDIN_PID=""
start_standin() {
  local copy="$1" i
  bash "$WORK/standin.sh" "$copy" >/dev/null 2>&1 &
  STANDIN_PID=$!
  STANDIN_PIDS+=("$STANDIN_PID")
  for i in $(seq 1 100); do
    [[ -f "$copy/.standin-ready" ]] && break
    sleep 0.1
  done
}

# try_acquire COPYDIR STDERRFILE → rc of a fresh acquire attempt.
# Run in a SUBSHELL because a refusal calls `exit 1`; the subshell's rc is the
# observed value the assertions below use. No rc is ever inferred.
try_acquire() {
  ( source "$LIB"; ci_lock_acquire "$1" ) 2>"$2" >/dev/null
  return $?
}

# ── 1. positive control: an idle copy CAN be locked ─────────────────────────
# Without this the whole file could pass by refusing everything.
COPY_A="$WORK/copy-a"; mkdir -p "$COPY_A"
try_acquire "$COPY_A" "$WORK/a1.err"; RC_FIRST=$?
if [[ "$RC_FIRST" == "0" ]]; then
  ok "an idle working copy acquires the lock (rc 0)"
else
  bad "an idle working copy acquires the lock — got rc $RC_FIRST / $(cat "$WORK/a1.err")"
fi
# That acquire ran in a subshell with no EXIT trap, so its lock is still on disk.
# Clear it explicitly rather than leaning on the stale-reclaim path — the next
# assertion is about a LIVE holder and must not be reading a reclaimed leftover.
rm -rf "$COPY_A/.ci-lock"

# ── 2. THE RULING: a second round in the SAME copy is refused, non-zero ─────
start_standin "$COPY_A"; HOLDER_A="$STANDIN_PID"
if [[ ! -d "$COPY_A/.ci-lock" ]]; then
  bad "stand-in holds \$COPY/.ci-lock (precondition for every assertion below)"
fi
try_acquire "$COPY_A" "$WORK/a2.err"; RC_SECOND=$?
SECOND_ERR="$(cat "$WORK/a2.err")"

if [[ "$RC_SECOND" != "0" ]]; then
  ok "a second run in the SAME working copy exits NON-ZERO (rc $RC_SECOND)"
else
  bad "a second run in the SAME working copy exits NON-ZERO — got rc 0, i.e. it was ALLOWED"
fi

# The rc alone is not the ruling: "refused" and "blew up" have the same rc. Pin
# that the refusal SAYS it is a refusal, in the exact words a reader will see.
if grep -qF 'REFUSED — this working copy is already running CI.' "$WORK/a2.err"; then
  ok "the refusal is explicit ('REFUSED — this working copy is already running CI.')"
else
  bad "the refusal is explicit — stderr was: $SECOND_ERR"
fi

# It must identify WHICH copy and WHICH process, or the reader cannot act on it.
if grep -qF "working copy : $COPY_A" "$WORK/a2.err"; then
  ok "the refusal names the working copy that is busy"
else
  bad "the refusal names the working copy that is busy — stderr was: $SECOND_ERR"
fi
if grep -qF "held by pid  : $HOLDER_A" "$WORK/a2.err"; then
  ok "the refusal names the holding pid ($HOLDER_A)"
else
  bad "the refusal names the holding pid ($HOLDER_A) — stderr was: $SECOND_ERR"
fi

# It must point at the SUPPORTED remedy (another copy), not at a workaround.
if grep -qF 'use another COPY' "$WORK/a2.err"; then
  ok "the refusal points at the supported remedy: another working copy"
else
  bad "the refusal points at the supported remedy: another working copy — stderr was: $SECOND_ERR"
fi

# ── 3. the refusal must NOT ship its own bypass ─────────────────────────────
# A guard that prints its own escape hatch is a suggestion. Scan the LIBRARY's
# non-comment source (not just this one message) for the shapes an escape hatch
# takes: a skip/force/bypass env var read, or prose offering to skip the check.
LIB_CODE="$WORK/lib-code.txt"
grep -vE '^[[:space:]]*#' "$LIB" > "$LIB_CODE"
if grep -qiE '(OC_[A-Z_]*(SKIP|FORCE|NOLOCK|IGNORE|BYPASS)|SKIP_CI_LOCK|FORCE_CI_LOCK|--force|--no-lock)' "$LIB_CODE"; then
  bad "the lock has NO bypass switch — found one: $(grep -inE '(OC_[A-Z_]*(SKIP|FORCE|NOLOCK|IGNORE|BYPASS)|SKIP_CI_LOCK|FORCE_CI_LOCK|--force|--no-lock)' "$LIB_CODE" | head -3 | tr '\n' ' ')"
else
  ok "the lock has NO bypass switch in its code"
fi
if grep -qiE 'rm -rf|rmdir|delete the lock|remove the lock' "$WORK/a2.err"; then
  bad "the refusal message does not teach the reader to delete the lock"
else
  ok "the refusal message does not teach the reader to delete the lock"
fi

# ── 4. a REFUSED run must not steal the holder's lock ───────────────────────
# ci_lock_release is ownership-guarded. If it were not, the refused attempt's
# exit path would delete the live run's lock and the NEXT attempt would sail
# through — a lock that unlocks itself on contention.
if [[ -d "$COPY_A/.ci-lock" ]]; then
  ok "the refused attempt left the holder's lock intact"
else
  bad "the refused attempt DELETED the holder's lock — contention now unlocks the copy"
fi

# ── 5. cross-clone parallelism must SURVIVE (the owner-endorsed shape) ──────
# The lock is per working copy on purpose. A machine-global lock would pass every
# assertion above and silently destroy the only parallelism that works.
COPY_B="$WORK/copy-b"; mkdir -p "$COPY_B"
try_acquire "$COPY_B" "$WORK/b1.err"; RC_B=$?
if [[ "$RC_B" == "0" ]]; then
  ok "a DIFFERENT working copy locks fine while copy-a is held (cross-clone stays parallel)"
else
  bad "a DIFFERENT working copy locks fine while copy-a is held — got rc $RC_B / $(cat "$WORK/b1.err"); the lock is machine-global, not per-copy"
fi

# ── 6. crash recovery: a lock from a DEAD pid is taken over ─────────────────
# ctrl-C / kill -9 / power loss must not wedge a clone forever.
#
# This one has a REAL-WORLD witness, not just this synthetic fixture: on
# 2026-08-02 a harness wall-clock timeout SIGTERM'd a lock holder mid-run and
# left the lock directory behind. The next acquire printed, verbatim,
# `[ci] stale CI lock from pid 79309 (no longer running) — taking it over`
# and returned 0. Crash recovery is observed behaviour, not a design claim.
# (evidence: stale-lock-real-owner.txt / stale-reclaim-live.txt)
COPY_C="$WORK/copy-c"; mkdir -p "$COPY_C"
mkdir "$COPY_C/.ci-lock"
DEAD_PID=$( bash -c 'echo $$' )   # a pid that has already exited
printf 'pid\t%s\nstarted\t2026-01-01T00:00:00Z\nroot\t%s\ncmd\tbash bin/ci.sh\npstart\tThu Jan  1 00:00:00 2026\n' \
  "$DEAD_PID" "$COPY_C" > "$COPY_C/.ci-lock/owner"
try_acquire "$COPY_C" "$WORK/c1.err"; RC_C=$?
if [[ "$RC_C" == "0" ]]; then
  ok "a lock left by a DEAD pid is reclaimed automatically (no permanent deadlock)"
else
  bad "a lock left by a DEAD pid is reclaimed automatically — got rc $RC_C / $(cat "$WORK/c1.err")"
fi

# ── 7. PID REUSE, both directions ───────────────────────────────────────────
# `kill -0` alone cannot answer "is this still the run that took the lock?" on a
# busy box: pids recycle. Both directions have to be pinned or the recycle case
# is an unspecified hole.
#
# 7a. recycled INTO an unrelated process ⇒ must read as STALE, not as held.
#     Otherwise a clone stays wedged until someone deletes a directory by hand.
#     The fixture is a LIVE pid (so `kill -0` says "alive") whose recorded start
#     time is not its own — exactly what a recycled pid looks like. A lock that
#     asked only `kill -0` passes every other assertion in this file and fails
#     here, which is the point.
COPY_D="$WORK/copy-d"; mkdir -p "$COPY_D"
sleep 600 & REUSED_PID=$!
STANDIN_PIDS+=("$REUSED_PID")
mkdir "$COPY_D/.ci-lock"
printf 'pid\t%s\nstarted\t2026-01-01T00:00:00Z\nroot\t%s\ncmd\tbash bin/ci.sh\npstart\tThu Jan  1 00:00:00 2026\n' \
  "$REUSED_PID" "$COPY_D" > "$COPY_D/.ci-lock/owner"
try_acquire "$COPY_D" "$WORK/d1.err"; RC_D=$?
if [[ "$RC_D" == "0" ]]; then
  ok "a LIVE pid whose start time is not the recorded one reads as stale (pid reuse does not wedge a clone)"
else
  bad "a LIVE pid that merely EXISTS still holds the lock — pid reuse wedges the clone; got: $(cat "$WORK/d1.err")"
fi

# 7b. the converse: a genuinely live holder whose command line DOES still match
#     must keep the lock. This is the assertion that stops 7a from being "fixed"
#     by simply believing every lock is stale.
COPY_E="$WORK/copy-e"; mkdir -p "$COPY_E"
start_standin "$COPY_E"; HOLDER_E="$STANDIN_PID"
try_acquire "$COPY_E" "$WORK/e1.err"; RC_E=$?
if [[ "$RC_E" != "0" ]] && grep -qF "held by pid  : $HOLDER_E" "$WORK/e1.err"; then
  ok "a genuinely live holder KEEPS the lock (staleness is not blanket-assumed)"
else
  bad "a genuinely live holder KEEPS the lock — got rc $RC_E / $(cat "$WORK/e1.err")"
fi

# 7c. a holder whose COMMAND LINE CHANGES must still hold it. Two real hazards in
#     one fixture, both found by hand while building this:
#       * an agent harness launches CI through a shell -c whose argument is a
#         whole script, so `ps -o command=` is multi-line and kilobytes long —
#         written verbatim into the tab-per-line owner record it corrupts it;
#       * bash EXEC-OPTIMISES its final command, so this holder reports
#         `bash -c …` at acquire time and `sleep 600` seconds later.
#     Both broke an earlier draft that discriminated on the command line, and both
#     broke it in the DANGEROUS direction: live holder misread as stale, second
#     run admitted. The plain-`bash bin/ci.sh` shape in 7b cannot see either.
COPY_F="$WORK/copy-f"; mkdir -p "$COPY_F"
MULTILINE_BODY="$(printf 'source %q\nci_lock_acquire "$1"\ntouch "$1/.standin-ready"\n# padding to make this command line long and multi-line\nsleep 600\n' "$LIB")"
bash -c "$MULTILINE_BODY" _ "$COPY_F" >/dev/null 2>&1 &
HOLDER_F=$!
STANDIN_PIDS+=("$HOLDER_F")
for _i in $(seq 1 100); do [[ -f "$COPY_F/.standin-ready" ]] && break; sleep 0.1; done
OWNER_LINES="$(wc -l < "$COPY_F/.ci-lock/owner" | tr -d '[:space:]')"
if [[ "$OWNER_LINES" == "5" ]]; then
  ok "the owner record stays one-field-per-line even for a multi-line command line"
else
  bad "the owner record stays one-field-per-line even for a multi-line command line — got $OWNER_LINES lines"
fi
try_acquire "$COPY_F" "$WORK/f1.err"; RC_F=$?
if [[ "$RC_F" != "0" ]] && grep -qF "held by pid  : $HOLDER_F" "$WORK/f1.err"; then
  ok "a live holder whose command line is multi-line AND changes under exec keeps the lock"
else
  bad "a live holder whose command line is multi-line AND changes under exec keeps the lock — got rc $RC_F / $(cat "$WORK/f1.err")"
fi

# ── 8. wiring: bin/ci.sh actually uses this ─────────────────────────────────
# The weaker half, stated as such at the top of this file.
if grep -qE '^ci_lock_acquire "\$ROOT"[[:space:]]*$' "$CI"; then
  ok "bin/ci.sh acquires the working-copy lock"
else
  bad "bin/ci.sh does NOT acquire the working-copy lock — the mutex exists but nothing takes it"
fi
if grep -qE '^source "\$ROOT/bin/lib/ci-lock\.sh"[[:space:]]*$' "$CI"; then
  ok "bin/ci.sh sources bin/lib/ci-lock.sh"
else
  bad "bin/ci.sh sources bin/lib/ci-lock.sh"
fi
if grep -qE "^trap 'ci_lock_release' EXIT[[:space:]]*\$" "$CI"; then
  ok "bin/ci.sh releases the lock on EXIT (no leak on a normal or failing run)"
else
  bad "bin/ci.sh releases the lock on EXIT — without it every run wedges its own clone"
fi

# Ordering is load-bearing: acquiring AFTER the first in-place write means the
# race this lock exists to stop has already happened by the time the lock is
# taken.
#
# Expressed as a MUST-BE-EMPTY query over everything ci.sh does before the
# acquire, NOT as "acquire's line number is below anchor X's line number". The
# line-number-versus-one-anchor shape was the first draft and it is the classic
# enumerating guard: it only ever knows about the anchor it was told about, so
# someone adding a NEW tree-mutating command above the acquire passes it
# silently. Here the prologue must contain NO write-class command at all, so a
# newly added one reddens whether or not this file has heard of it.
ACQ_LINE="$(grep -nE '^ci_lock_acquire "\$ROOT"' "$CI" | head -1 | cut -d: -f1)"
if [[ -z "$ACQ_LINE" ]]; then
  bad "ci.sh's prologue does no tree-mutating work before the lock — cannot check, no acquire call found"
else
  # Everything above the acquire, comments and blank lines removed.
  PROLOGUE="$WORK/ci-prologue.sh"
  head -n "$((ACQ_LINE - 1))" "$CI" | grep -vE '^[[:space:]]*(#|$)' > "$PROLOGUE"
  # Write-class shapes: output/append redirection, the file-mutating coreutils,
  # in-place sed, and the toolchain drivers ci.sh uses that all write into the
  # tree. Deliberately broad — a false positive here is a 10-second read of one
  # line, a false negative is the race back.
  #
  # ⚠️ The go alternative is spelled `go[[:space:]]+(build|test)` and NOT the
  # obvious `go build|go test`. This is not style. bin/tests/go-test-nocache-guard.sh
  # scans every shell script for `go test` call sites missing -count=1, and it
  # reddened on the two lines below when they carried the literal.
  #
  # Whose bug that is, stated accurately so the next person does not re-diagnose
  # it: NOT this file's. That guard's own header (:53-61) says it uses a real
  # parser "instead of grep -F 'go test'" precisely so that string constants and
  # prose do NOT count as call sites, and it has decoy fixtures pinning comments,
  # echo/printf strings and `go build`. But its segmentation step
  # (`split(raw, segs, /(&&|\|\||;|\|)/)`) is NOT quote-aware, so a `|` inside a
  # SINGLE-QUOTED argument — which is all the `|` below are, regex alternation
  # handed to grep, never a shell pipeline — is treated as a pipeline separator
  # and the next fragment lands in command position. Its decoys cover quoted
  # strings WITHOUT a `|`; they do not cover this one. So its implemented scope
  # is narrower than its stated scope.
  #
  # DELIBERATELY NOT FIXED HERE. Widening another guard is outside this ticket's
  # scope; the note exists so the gap is on record instead of being silently
  # worked around once per person, which is how a gap survives forever.
  PROLOGUE_WRITES="$(grep -nE '(^|[^0-9<>&])>>?[[:space:]]*[^&]|(^|[[:space:]])(mkdir|cp|mv|rm|touch|ln|install|tee|sed -i|npm|npx|go[[:space:]]+(build|test)|bash "\$ROOT/bin/build)' "$PROLOGUE" || true)"
  if [[ -z "$PROLOGUE_WRITES" ]]; then
    ok "ci.sh does NO write-class work before taking the lock (zero-hit scan of its whole prologue)"
  else
    bad "ci.sh writes to the tree BEFORE taking the lock — the race already happened: $(printf '%s | ' $PROLOGUE_WRITES)"
  fi
  # Sentinel for the scan itself: an enumerating guard that matches nothing is
  # indistinguishable from one that is switched off. Prove the pattern DOES fire
  # on a prologue that mutates the tree.
  cp "$PROLOGUE" "$WORK/ci-prologue-mutant.sh"
  printf '%s\n' 'bash "$ROOT/bin/build-seedsdist"' >> "$WORK/ci-prologue-mutant.sh"
  if grep -qE '(^|[^0-9<>&])>>?[[:space:]]*[^&]|(^|[[:space:]])(mkdir|cp|mv|rm|touch|ln|install|tee|sed -i|npm|npx|go[[:space:]]+(build|test)|bash "\$ROOT/bin/build)' "$WORK/ci-prologue-mutant.sh"; then
    ok "sentinel — the prologue scan actually fires when a tree-mutating command is added above the acquire"
  else
    bad "sentinel — the prologue scan does NOT fire on an added tree-mutating command; the assertion above is vacuous"
  fi
fi

# ── 9. SENTINEL HALF: the lock must not touch any OTHER entry point ─────────
# The mutants above are all negative controls ("delete the guard, does it go
# red"). Without this half, a lock that had quietly been bolted onto
# conformance/run.sh as well — destroying the verified 4-rounds-in-parallel
# conformance capability — would pass every assertion in this file.
# bin/ci.sh is the ONLY entry point that may take this lock.
# Scan set = tracked files PLUS untracked-not-ignored ones, so the assertion is
# just as true before this ticket's files are committed as after. Paths stay
# repo-relative (grep is run from ROOT) so the expected list below can be
# literal instead of machine-dependent.
LOCK_CALLERS="$( cd "$ROOT" && { git ls-files 2>/dev/null; git ls-files --others --exclude-standard 2>/dev/null; } \
  | sort -u \
  | while IFS= read -r f; do
      [[ -f "$f" ]] || continue
      grep -lE '(^|[^_[:alnum:]])ci_lock_acquire' "$f" 2>/dev/null
    done | sort -u )"
EXPECTED_CALLERS="bin/ci.sh
bin/lib/ci-lock.sh
bin/tests/ci-lock-guard.sh"
if [[ "$LOCK_CALLERS" == "$EXPECTED_CALLERS" ]]; then
  ok "only bin/ci.sh takes the lock (conformance/run.sh and every other entry point are untouched)"
else
  bad "an entry point other than bin/ci.sh takes the CI lock — expected [$(printf '%s ' $EXPECTED_CALLERS)] got [$(printf '%s ' $LOCK_CALLERS)]"
fi
if grep -qE 'ci-lock|ci_lock' "$ROOT/conformance/run.sh"; then
  bad "conformance/run.sh is free of the CI lock — its parallel-rounds capability must not be narrowed by this ticket"
else
  ok "conformance/run.sh is free of the CI lock (its parallel-rounds capability is untouched)"
fi

# The lock dir lives INSIDE the clone (that is what makes it per-copy), so it has
# to be ignored or every run stamps its own provenance line DIRTY.
if grep -qE '^\.ci-lock/?$' "$ROOT/.gitignore"; then
  ok ".ci-lock is gitignored (a run must not dirty its own tree)"
else
  bad ".ci-lock is gitignored — otherwise ci.sh's provenance stamp reports DIRTY on every run"
fi

echo "ci per-working-copy lock guard: $PASS ok, $FAIL failed"
[[ "$FAIL" == "0" ]]
