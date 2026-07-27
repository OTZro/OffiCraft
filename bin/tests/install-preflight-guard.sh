#!/usr/bin/env bash
# bin/tests/install-preflight-guard.sh — HERMETIC unit tests for bin/install.sh's
# TOOL PREFLIGHT (T-7f38).
#
# THE DEFECT UNDER TEST
# ---------------------
# Every member is a `claude` process running inside a tmux session
# (cli/ocwarden/spawn.go: `tmux -L <socket> new-session -d …`, whose launch line
# execs claude). Neither has a fallback. But install.sh never mentioned tmux at
# all: on a machine without it the install went GREEN, the cockpit opened, a hire
# was accepted — and the member then sat at 「waking」 forever while nothing in the
# installer, the server, or the cockpit ever named the cause. The owner's report
# was literally "always waking but no progress, it turns out there is no tmux
# installed".
#
# WHAT THIS SUITE PINS
# --------------------
#   (a) a machine missing a required tool is REFUSED, and refused BEFORE anything
#       is written (no ~/.officraft, no launchd plist);
#   (b) the refusal names the tool AND the command that installs it, and offers
#       NO bypass — a gate with a documented escape hatch is a gate nobody hits;
#   (c) a machine with the tools present installs exactly as before — the
#       sentinel every fail-closed gate needs, because "refuses when it should"
#       is worthless without "does not refuse when it should not";
#   (d) THE COUNTERFACTUAL: the same missing-tool run against a MUTANT install.sh
#       with the `oc_preflight` call sites deleted must SUCCEED. That mutant is
#       the pre-fix installer, so this case simultaneously reproduces the
#       original defect and proves case (a) is testing the check rather than
#       passing for some unrelated reason.
#
# WHY THE SHIM IS SHAPED THIS WAY
# -------------------------------
# Same discipline as install-guard.sh / install-claude-stamp.sh: uname, launchctl
# and lsof are replaced on PATH so nothing in the real launchd domain is read or
# written, HOME is redirected into a temp dir, and the package binaries are
# stubs. tmux and claude are stubbed too and NEVER invoked — the preflight only
# asks whether they resolve, so a stub is a faithful stand-in, and using the real
# ones would make the result depend on what the CI host happens to have.
#
# One honest limit: the claude half of the preflight mirrors ocwarden's own
# resolution order, which ends in two ABSOLUTE paths (/opt/homebrew/bin/claude,
# /usr/local/bin/claude) that no PATH shim can hide. On a host carrying either,
# "no claude anywhere" is not constructible and those cases SKIP rather than
# pretend. The tmux cases have no such hole (bare name, PATH only), which is why
# the counterfactual is built on tmux.
set -uo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SCRIPT="$HERE/../install.sh"
[[ -f "$SCRIPT" ]] || { echo "FATAL: install.sh not found at $SCRIPT" >&2; exit 2; }

PASS=0; FAIL=0
ok()   { PASS=$((PASS+1)); printf '  ok   — %s\n' "$1"; }
bad()  { FAIL=$((FAIL+1)); printf '  FAIL — %s\n' "$1"; }
check(){ if [[ "$2" == "$3" ]]; then ok "$1"; else bad "$1 (want '$2' got '$3')"; fi; }

WORK="$(mktemp -d -t oc-install-preflight.XXXXXX)"
trap 'rm -rf "$WORK"' EXIT

SHIMDIR="$WORK/shim"       # always on PATH: uname/launchctl/lsof
TOOLDIR="$WORK/tools"      # tmux + claude live here, so a case can drop them
PKG="$WORK/pkg"            # the "release tarball": install.sh + three binaries
MUTANT="$WORK/mutant"      # the same package with the preflight CALL removed
FAKEHOME="$WORK/home"
mkdir -p "$SHIMDIR" "$TOOLDIR" "$PKG" "$MUTANT"

cp "$SCRIPT" "$PKG/install.sh"
for b in ocserverd ocwarden ocagent; do
  printf '#!/usr/bin/env bash\nexit 0\n' > "$PKG/$b"
  chmod +x "$PKG/$b"
done

# ── the mutant: identical package, preflight CALL SITES deleted ──────────────
# Only the calls are removed, not the function — that is the smallest edit that
# turns the gate off, and it is exactly what the file looked like before T-7f38.
cp "$PKG"/* "$MUTANT/"
grep -vE '^[[:space:]]*oc_preflight([[:space:]]|$)' "$PKG/install.sh" > "$MUTANT/install.sh"
MUTANT_DELETED=$(( $(grep -cE '^[[:space:]]*oc_preflight([[:space:]]|$)' "$PKG/install.sh") ))
if [[ "$MUTANT_DELETED" -ge 1 ]]; then
  ok "mutant construction: $MUTANT_DELETED oc_preflight call site(s) removed from the copy"
else
  bad "mutant construction: found NO oc_preflight call site to remove — this suite is not testing what it claims"
fi
if bash -n "$MUTANT/install.sh" 2>/dev/null; then
  ok "mutant construction: the mutated install.sh still parses (the mutation is the gate, not a syntax break)"
else
  bad "mutant construction: the mutated install.sh no longer parses — its 'success' would prove nothing"
fi

cat > "$SHIMDIR/uname" <<'SH'
#!/usr/bin/env bash
case "${1:-}" in
  -s) echo Darwin ;;
  -m) echo arm64 ;;
  *)  echo Darwin ;;
esac
SH

cat > "$SHIMDIR/launchctl" <<'SH'
#!/usr/bin/env bash
case "${1:-}" in
  print) exit 1 ;;
  bootstrap) touch "$SHIM_STATE/.bootstrapped"; exit 0 ;;
esac
exit 0
SH

cat > "$SHIMDIR/lsof" <<'SH'
#!/usr/bin/env bash
QPORT=""
for a in "$@"; do
  case "$a" in -iTCP:*) QPORT="${a#-iTCP:}" ;; esac
done
if [[ -f "$SHIM_STATE/.bootstrapped" ]]; then
  echo "ocserverd 4242 tester 5u IPv4 0x0 0t0 TCP 127.0.0.1:${QPORT:-7755} (LISTEN)"
  exit 0
fi
exit 1
SH

printf '#!/usr/bin/env bash\nexit 0\n' > "$TOOLDIR/tmux"
printf '#!/usr/bin/env bash\necho "9.9.9 (Claude Code)"\nexit 0\n' > "$TOOLDIR/claude"
chmod +x "$SHIMDIR"/uname "$SHIMDIR"/launchctl "$SHIMDIR"/lsof "$TOOLDIR"/tmux "$TOOLDIR"/claude

PLIST_REL="Library/LaunchAgents/com.officraft.serve.plist"

reset_fixture() {
  rm -rf "$FAKEHOME"
  mkdir -p "$FAKEHOME/Library/LaunchAgents"
  rm -f "$WORK/.bootstrapped"
}

# run_install <pkgdir> <tools…> — a fresh non-interactive install (the
# `curl | bash` shape) with ONLY the named tools resolvable. Each tool is linked
# into a per-run dir, so "missing" means missing from PATH entirely rather than
# present-but-broken (which is a different failure with a different message).
run_install() {
  local pkgdir="$1"; shift
  local rundir="$WORK/run" t
  rm -rf "$rundir"; mkdir -p "$rundir"
  for t in "$@"; do ln -s "$TOOLDIR/$t" "$rundir/$t"; done
  OUT="$(cd "$WORK" && env -i \
    PATH="$rundir:$SHIMDIR:/usr/bin:/bin:/usr/sbin:/sbin" \
    HOME="$FAKEHOME" SHIM_STATE="$WORK" \
    bash "$pkgdir/install.sh" </dev/null 2>&1)"
  RC=$?
}

wrote_anything() { # did this run touch the machine at all?
  [[ -e "$FAKEHOME/.officraft" || -e "$FAKEHOME/$PLIST_REL" ]]
}

HAS_ABS_CLAUDE=0
[[ -x /opt/homebrew/bin/claude || -x /usr/local/bin/claude ]] && HAS_ABS_CLAUDE=1

echo "install.sh tool preflight — hermetic tests"

# ── 1. tmux missing → refused, and refused before anything is written ────────
reset_fixture
run_install "$PKG" claude
check "no tmux: the install is refused" "1" "$RC"
if wrote_anything; then
  bad "no tmux: the machine was touched anyway (~/.officraft or the plist exists) — the gate fired too late"
else
  ok "no tmux: NOTHING was written (no ~/.officraft, no launchd plist)"
fi
case "$OUT" in
  *"tmux"*) ok "no tmux: the refusal names tmux" ;;
  *) bad "no tmux: the refusal never names tmux:
$OUT" ;;
esac
case "$OUT" in
  *"brew install tmux"*) ok "no tmux: the refusal says exactly how to fix it" ;;
  *) bad "no tmux: the refusal does not say how to fix it:
$OUT" ;;
esac
# The failure the owner actually hit is invisible, so the refusal has to connect
# the missing tool to the symptom — otherwise it is one more line nobody reads.
case "$OUT" in
  *"waking"*|*"tmux"*) ok "no tmux: the refusal explains the consequence (a member is claude inside tmux)" ;;
  *) bad "no tmux: the refusal does not explain why tmux matters:
$OUT" ;;
esac
# NO BYPASS. A gate that advertises its own escape hatch gets escaped, and the
# escapee lands back in exactly the silent 「waking」 state this fixes.
for esc in "--skip" "--no-preflight" "OC_SKIP" "bypass" "ignore this" "at your own risk"; do
  case "$OUT" in
    *"$esc"*) bad "no tmux: the refusal advertises a bypass ('$esc') — it must not teach anyone around itself" ;;
    *) ok "no tmux: the refusal offers no bypass ('$esc' absent)" ;;
  esac
done

# ── 2. THE COUNTERFACTUAL: same run, preflight call removed → it installs ────
# If this went red, case 1 above would be proving nothing: the refusal could be
# coming from something else entirely. Green here means the pre-T-7f38 installer
# really did sail through a tmux-less machine — the defect, reproduced.
reset_fixture
run_install "$MUTANT" claude
check "MUTANT (no preflight call), no tmux: the install succeeds — this is the defect" "0" "$RC"
if [[ -f "$FAKEHOME/$PLIST_REL" ]]; then
  ok "MUTANT, no tmux: a serve plist was written — green install on a machine whose members can never start"
else
  bad "MUTANT, no tmux: no plist was written, so case 1's refusal is NOT attributable to the preflight:
$OUT"
fi

# ── 3. claude missing → refused the same way ────────────────────────────────
if [[ "$HAS_ABS_CLAUDE" == 1 ]]; then
  echo "  skip — /opt/homebrew/bin/claude or /usr/local/bin/claude exists on this host; 'no claude' is not constructible"
else
  reset_fixture
  run_install "$PKG" tmux
  check "no claude: the install is refused" "1" "$RC"
  if wrote_anything; then
    bad "no claude: the machine was touched anyway — the gate fired too late"
  else
    ok "no claude: NOTHING was written"
  fi
  case "$OUT" in
    *"npm install -g @anthropic-ai/claude-code"*) ok "no claude: the refusal says exactly how to fix it" ;;
    *) bad "no claude: the refusal does not say how to fix it:
$OUT" ;;
  esac
fi

# ── 4. both missing → BOTH are named in one pass ────────────────────────────
# Reporting one tool, exiting, and reporting the next one only on the re-run is
# how a five-minute fix becomes three round trips.
if [[ "$HAS_ABS_CLAUDE" == 1 ]]; then
  echo "  skip — cannot construct 'no claude' on this host (see above)"
else
  reset_fixture
  run_install "$PKG"
  check "neither tool: the install is refused" "1" "$RC"
  if [[ "$OUT" == *"tmux"* && "$OUT" == *"claude"* ]]; then
    ok "neither tool: BOTH missing tools are named in one refusal"
  else
    bad "neither tool: the refusal does not name both:
$OUT"
  fi
fi

# ── 5. THE SENTINEL: tools present → the install is NOT blocked ─────────────
# The whole point of a fail-closed gate is that it is invisible when the machine
# is fine. This repo has already shipped a tightening that reddened nothing in CI
# while killing four working paths, so the "does not misfire" half is asserted at
# the same rank as the "does refuse" half, not left to hand-waving.
reset_fixture
run_install "$PKG" tmux claude
check "tools present: the install succeeds" "0" "$RC"
if [[ -f "$FAKEHOME/$PLIST_REL" ]]; then
  ok "tools present: the serve plist was rendered (the install really ran, not just exited 0)"
else
  bad "tools present: no serve plist — the install did not complete:
$OUT"
fi
case "$OUT" in
  *"FATAL"*) bad "tools present: the run printed a FATAL on a perfectly good machine:
$OUT" ;;
  *) ok "tools present: no FATAL printed" ;;
esac

# ── 6. the preflight must not block the ways OUT ────────────────────────────
# Removal and --help have to keep working on the exact machine the gate refuses
# to install on: a user whose tmux-less box is half-configured still needs
# `--uninstall` to clean up, and still needs `--help` to read what is required.
reset_fixture
rundir="$WORK/run"; rm -rf "$rundir"; mkdir -p "$rundir"
OUT="$(cd "$WORK" && env -i PATH="$rundir:$SHIMDIR:/usr/bin:/bin:/usr/sbin:/sbin" \
  HOME="$FAKEHOME" SHIM_STATE="$WORK" \
  bash "$PKG/install.sh" --uninstall --dry-run </dev/null 2>&1)"; RC=$?
check "no tools at all: --uninstall --dry-run still works" "0" "$RC"
OUT="$(cd "$WORK" && env -i PATH="$rundir:$SHIMDIR:/usr/bin:/bin:/usr/sbin:/sbin" \
  HOME="$FAKEHOME" SHIM_STATE="$WORK" \
  bash "$PKG/install.sh" --help </dev/null 2>&1)"; RC=$?
check "no tools at all: --help still prints" "0" "$RC"
# --help IS the file's header comment block (install.sh sed-prints it), so a new
# hard requirement that never reaches it is undocumented from the user's side.
case "$OUT" in
  *"tmux"*) ok "--help documents the tmux requirement (the header block IS the help text)" ;;
  *) bad "--help never mentions tmux — the requirement is invisible to the user:
$OUT" ;;
esac

echo "install.sh tool preflight: $PASS ok, $FAIL failed"
[[ "$FAIL" == "0" ]] || exit 1
exit 0
