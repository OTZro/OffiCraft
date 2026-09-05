#!/usr/bin/env bash
# bin/tests/ocserver-load-guard.sh — HERMETIC unit tests for bin/ocserver's
# BOOTSTRAP→KICKSTART BLAME (T-908d).
#
# THE DEFECT UNDER TEST
# ---------------------
# `launchctl bootstrap` can exit 0 and register NOTHING. `drop_and_load` used to
# read:
#     launchctl bootstrap "$GUI" "$dst" || fail "launchctl bootstrap failed for $dst"
#     launchctl kickstart -k "$GUI/$label" || fail "launchctl kickstart failed for …"
# so in that state the FIRST verb to notice was kickstart, and its exit 113
# ("Could not find service … in domain") was not swallowed — it was REWRITTEN into
# a sentence naming kickstart. The operator was then sent to debug a step that was
# never broken, for all three labels (serve / autodeploy / tunnel) that share this
# code path.
#
# WHY THIS SUITE EXISTS AT ALL (read before "simplifying" it)
# -----------------------------------------------------------
# `drop_and_load` had NO unit coverage: the only thing touching it was a one-line
# grep in install-claude-stamp.sh case 10g, whose own comment calls itself "the
# last unguarded link". A grep for a mention cannot tell live code from dead code
# — the very hole T-ff48 was re-opened by. So the decision under test was lifted
# into `bootstrap_and_confirm`, a TOP-LEVEL function, and bin/ocserver grew the
# `bootstrap-and-confirm` seam (same pattern as `render-serve-plist` and
# `render-config`) so these cases DRIVE the real function against a stubbed
# launchctl and assert on the message the operator actually gets. Case 7 is the
# remaining grep, and it guards only "does install still route through it".
#
# Nothing here can reach a real launchd domain: launchctl is replaced on PATH and
# every invocation is recorded in a tripwire.
set -uo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SCRIPT="$HERE/../ocserver"
[[ -f "$SCRIPT" ]] || { echo "FATAL: bin/ocserver not found at $SCRIPT" >&2; exit 2; }

PASS=0; FAIL=0
ok()   { PASS=$((PASS+1)); printf '  ok   — %s\n' "$1"; }
bad()  { FAIL=$((FAIL+1)); printf '  FAIL — %s\n' "$1"; }
check(){ if [[ "$2" == "$3" ]]; then ok "$1"; else bad "$1 (want '$2' got '$3')"; fi; }

WORK="$(mktemp -d -t oc-ocserver-load-guard.XXXXXX)"
trap 'rm -rf "$WORK"' EXIT
SHIMDIR="$WORK/shim"
mkdir -p "$SHIMDIR"

# launchctl: the oracle, driven entirely by env.
#   SHIM_BOOTSTRAP_RC   exit code for `bootstrap`
#   SHIM_KICKSTART_RC   exit code for `kickstart`
#   SHIM_REGISTER_AFTER how many `print` probes answer "not registered" before the
#                       label shows up; a value of -1 means it NEVER shows up —
#                       the exit-0-registered-nothing state.
# The probe counter lives in a file because each `print` is a fresh process.
cat > "$SHIMDIR/launchctl" <<'SH'
#!/usr/bin/env bash
echo "launchctl $*" >> "$SHIM_TRIPWIRE"
case "${1:-}" in
  bootstrap) exit "${SHIM_BOOTSTRAP_RC:-0}" ;;
  kickstart) exit "${SHIM_KICKSTART_RC:-0}" ;;
  print)
    after="${SHIM_REGISTER_AFTER:-0}"
    [[ "$after" == "-1" ]] && exit 1
    n=0; [[ -f "$SHIM_STATE/.prints" ]] && n="$(cat "$SHIM_STATE/.prints")"
    n=$((n + 1)); echo "$n" > "$SHIM_STATE/.prints"
    (( n > after )) || exit 1
    printf '%s = {\n\tstate = running\n\tpid = 4242\n}\n' "${2:-}"
    exit 0 ;;
esac
exit 0
SH
# sleep: the bounded poll's 25 x 0.2s is not what any case asserts on.
printf '#!/usr/bin/env bash\nexit 0\n' > "$SHIMDIR/sleep"
chmod +x "$SHIMDIR/launchctl" "$SHIMDIR/sleep"

GUI="gui/501"
LABEL="com.officraft.serve.guardns"
TARGET="$GUI/$LABEL"
PLIST="$WORK/$LABEL.plist"
: > "$PLIST"

# load [env-overrides…] — drive the REAL bootstrap_and_confirm through the seam.
load() {
  rm -f "$WORK/.prints"; : > "$WORK/.tripwire"
  OUT="$(env -i PATH="$SHIMDIR:/usr/bin:/bin:/usr/sbin:/sbin" HOME="$WORK" \
    SHIM_TRIPWIRE="$WORK/.tripwire" SHIM_STATE="$WORK" "$@" \
    bash "$SCRIPT" bootstrap-and-confirm "$GUI" "$PLIST" "$TARGET" 2>&1)"
  RC=$?
}
tripwire_has() { grep -qF -e "$1" "$WORK/.tripwire"; }

echo "bin/ocserver bootstrap→kickstart blame — hermetic tests (stubbed launchd)"

# ── 1. the ordinary load: registered on the first probe, kickstart succeeds ──
# The positive control. Without it every assertion below is satisfied by a
# function that refuses to load anything at all.
load SHIM_REGISTER_AFTER=0
check "healthy load: succeeds" "0" "$RC"
case "$OUT" in *WARN*) bad "healthy load: warns about a registration that was there immediately: $OUT";; *) ok "healthy load: no spurious registration warning";; esac
if tripwire_has "launchctl kickstart -k $TARGET"; then
  ok "healthy load: the job is kickstarted (RunAtLoad is not relied on)"
else
  bad "healthy load: never kickstarted — tripwire: $(cat "$WORK/.tripwire")"
fi

# ── 2. THE DEFECT: bootstrap exits 0, registers NOTHING, kickstart then fails ─
# The message must name the step that really failed. "Does not contain the word
# kickstart" is the assertion that carries the defect: the old code's ONLY output
# here was a sentence naming kickstart.
load SHIM_REGISTER_AFTER=-1 SHIM_KICKSTART_RC=113
check "registered nothing: the load FAILS" "1" "$RC"
case "$OUT" in
  *"registered nothing"*) ok "registered nothing: the failure names the bootstrap that registered no job" ;;
  *) bad "registered nothing: the failure does not name the bootstrap step:
$OUT" ;;
esac
case "$OUT" in
  *"FATAL: launchctl kickstart failed"*) bad "registered nothing: still blames kickstart, the step that was never broken:
$OUT" ;;
  *) ok "registered nothing: does not blame kickstart" ;;
esac
case "$OUT" in
  *"$TARGET"*) ok "registered nothing: names the target launchd does not know" ;;
  *) bad "registered nothing: does not name the target:
$OUT" ;;
esac
case "$OUT" in
  *"$PLIST"*) ok "registered nothing: names the plist to look at" ;;
  *) bad "registered nothing: does not name the plist:
$OUT" ;;
esac

# ── 3. a registration that merely LAGS still loads ───────────────────────────
# The counter-example to the new probe becoming a new way to brick an install.
# launchd's registration can trail a bootstrap that exited 0; a label that shows
# up on a later probe must load exactly as before.
load SHIM_REGISTER_AFTER=3
check "late registration: still loads" "0" "$RC"
case "$OUT" in *"registered nothing"*) bad "late registration: claims nothing was registered, which is a lie here: $OUT";; *) ok "late registration: no false 'registered nothing' claim";; esac

# ── 4. the poll is BOUNDED and its timeout is NON-FATAL ─────────────────────
# A label that never answers the probe but whose kickstart SUCCEEDS was, in the
# end, loaded — launchd was slow, not broken. Failing here would take machines
# that install fine today and stop them installing. The WARN is the whole
# consequence, and the run must not hang waiting either.
load SHIM_REGISTER_AFTER=-1 SHIM_KICKSTART_RC=0
check "unconfirmed registration + working kickstart: still loads" "0" "$RC"
case "$OUT" in
  *"WARN: launchd still does not know $TARGET"*) ok "unconfirmed registration: warns, naming the target it could not confirm" ;;
  *) bad "unconfirmed registration: no warning about the unconfirmed registration:
$OUT" ;;
esac

# ── 5. a CONFIRMED label whose kickstart fails is still kickstart's fault ────
# The new diagnosis must not swallow the old, honest one: here launchd DOES know
# the label, so blaming bootstrap would be the same mistake pointing the other way.
load SHIM_REGISTER_AFTER=0 SHIM_KICKSTART_RC=113
check "registered but kickstart fails: the load FAILS" "1" "$RC"
case "$OUT" in
  *"launchctl kickstart failed for $TARGET"*) ok "registered but kickstart fails: blames kickstart, which really is the broken step" ;;
  *) bad "registered but kickstart fails: does not blame kickstart:
$OUT" ;;
esac
case "$OUT" in
  *"registered nothing"*) bad "registered but kickstart fails: falsely claims the bootstrap registered nothing:
$OUT" ;;
  *) ok "registered but kickstart fails: no false 'registered nothing' claim" ;;
esac

# ── 6. a bootstrap that FAILS outright is unchanged ─────────────────────────
load SHIM_BOOTSTRAP_RC=1
check "bootstrap fails outright: the load FAILS" "1" "$RC"
case "$OUT" in
  *"launchctl bootstrap failed for $PLIST"*) ok "bootstrap fails outright: names the bootstrap and the plist" ;;
  *) bad "bootstrap fails outright: wrong message:
$OUT" ;;
esac
if tripwire_has "launchctl kickstart"; then
  bad "bootstrap fails outright: kickstarted a job that was never bootstrapped"
else
  ok "bootstrap fails outright: no kickstart is attempted"
fi

# ── 7. install still routes every label through the function above ──────────
# HONEST ABOUT ITS STRENGTH: this one IS a grep, and it is the only one here.
# Everything above proves what bootstrap_and_confirm DOES; this proves that
# `install` step 6 still reaches it — and that all three labels do, since they
# share the single drop_and_load call site.
SRC="$(cat "$SCRIPT")"
if [[ "$SRC" == *'bootstrap_and_confirm "$GUI" "$dst" "$GUI/$label"'* ]]; then
  ok "drop_and_load loads through bootstrap_and_confirm — the cases above drive the install's own path"
else
  bad "drop_and_load no longer calls bootstrap_and_confirm — these cases are driving a function the install does not use"
fi
if [[ "$SRC" == *'launchctl kickstart -k "$GUI/$label" || fail'* ]]; then
  bad "drop_and_load still has its own unguarded kickstart||fail — the blame defect is back"
else
  ok "drop_and_load has no second, unguarded kickstart||fail of its own"
fi

echo "ocserver-load-guard tests: $PASS ok, $FAIL failed"
[[ "$FAIL" == "0" ]] || exit 1
exit 0
