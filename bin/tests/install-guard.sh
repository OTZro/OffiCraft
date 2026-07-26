#!/usr/bin/env bash
# bin/tests/install-guard.sh — HERMETIC unit tests for bin/install.sh's
# LIVE-SERVICE GATE (T-eefc).
#
# THE DEFECT UNDER TEST
# ---------------------
# install.sh's pre-existing gates all reason about FILES: is there a binary, a
# database, a plist naming our program, a config that would move. On the
# maintainer's own machine every one of them answers "same paths, same port,
# same config — plain reload" and waves the run through to `launchctl bootout`,
# which drops the RUNNING server and every cockpit/agent attached to it. The
# existing-install prompt even promised the opposite ("keeps serving its old
# code until its next restart"), and --force — documented as being about
# overwriting files — silently authorized the outage.
#
# WHY THE SHIM IS SHAPED THIS WAY (read before "simplifying" it)
# --------------------------------------------------------------
# A launchd label is a singleton in the user's GUI DOMAIN, keyed on UID. It does
# NOT follow $HOME. So the obvious way to make these tests "safe" — point HOME at
# a scratch dir — relocates the files but leaves the job target resolving to the
# REAL station, and the equally obvious fix — set OC_LAUNCHD_LABEL to something
# harmless — silently stops testing the path an actual user walks, because a
# user never sets that variable. A green run under an overridden label proves
# nothing about the default one.
#
# So this suite does neither. The label stays the DEFAULT com.officraft.serve —
# the exact identity a real re-install resolves — and `launchctl` itself is
# replaced on PATH by a stub that IS the oracle: it reports whatever job state a
# case asks for and records every bootout/bootstrap in a tripwire. Nothing in the
# real launchd domain is read or written, no process is signalled, and the
# assertions are about the DEFAULT-label decision. HOME is still redirected, but
# only so the file-side gates have a sandbox to look at — never as the safety
# mechanism.
#
# plutil is deliberately NOT stubbed: it only ever reads a plist this suite
# wrote inside its own temp dir, so the real tool gives a more faithful test at
# zero risk.
set -uo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SCRIPT="$HERE/../install.sh"
[[ -f "$SCRIPT" ]] || { echo "FATAL: install.sh not found at $SCRIPT" >&2; exit 2; }

PASS=0; FAIL=0
ok()   { PASS=$((PASS+1)); printf '  ok   — %s\n' "$1"; }
bad()  { FAIL=$((FAIL+1)); printf '  FAIL — %s\n' "$1"; }
check(){ if [[ "$2" == "$3" ]]; then ok "$1"; else bad "$1 (want '$2' got '$3')"; fi; }

WORK="$(mktemp -d -t oc-install-guard.XXXXXX)"
trap 'rm -rf "$WORK"' EXIT

SHIMDIR="$WORK/shim"
PKG="$WORK/pkg"
FAKEHOME="$WORK/home"
mkdir -p "$SHIMDIR" "$PKG"

# ── the package under test: install.sh + its three sibling binaries ──────────
cp "$SCRIPT" "$PKG/install.sh"
for b in ocserverd ocwarden ocagent; do
  printf '#!/usr/bin/env bash\nexit 0\n' > "$PKG/$b"
  chmod +x "$PKG/$b"
done

# ── PATH shims ───────────────────────────────────────────────────────────────
# uname: pin darwin/arm64 so the platform gate passes on any CI host.
cat > "$SHIMDIR/uname" <<'SH'
#!/usr/bin/env bash
case "${1:-}" in
  -s) echo Darwin ;;
  -m) echo arm64 ;;
  *)  echo Darwin ;;
esac
SH

# launchctl: the oracle. SHIM_JOB selects the job state a case is asserting on.
#   absent  → nothing registered under the label (fresh machine)
#   loaded  → registered but NOT running (no pid line) — booting it out is harmless
#   running → registered AND serving (pid line present) — this is the outage case
# Output mimics the real `launchctl print` layout, including the nested
# `state = active` entries that the pid parser must not be confused by.
#
# LABEL-AWARE ON PURPOSE. An earlier version of this stub ignored $2 entirely,
# which quietly weakened every assertion below: a label-blind oracle answers
# "running" no matter WHICH job the script asks about, so the suite could only
# prove "install.sh consults launchctl and obeys the answer" — never "it asks
# about the right label". Since the whole defect is that the default label
# collides with the live station's, that is the property most worth pinning.
# Now anything other than the expected target reads as NOT REGISTERED, so a
# script that resolved the wrong label sees "absent", skips the gate, and the
# live-job cases go red.
cat > "$SHIMDIR/launchctl" <<'SH'
#!/usr/bin/env bash
echo "launchctl $*" >> "$SHIM_TRIPWIRE"
case "${1:-}" in
  print)
    # once booted out, the label really is gone — lets the poll loop exit
    [[ -f "$SHIM_STATE/.booted-out" ]] && exit 1
    # asked about a label that is not the one under test → nothing registered
    [[ "${2:-}" == "$SHIM_EXPECT_TARGET" ]] || exit 1
    case "${SHIM_JOB:-absent}" in
      running)
        cat <<'OUT'
gui/501/com.officraft.serve = {
	active count = 1
	state = running
	program = /path/to/ocserverd
	pid = 4242
	spawn type = daemon
	endpoints = {
		"com.officraft.serve" = {
			state = active
			active count = 1
		}
	}
}
OUT
        exit 0 ;;
      loaded)
        cat <<'OUT'
gui/501/com.officraft.serve = {
	active count = 0
	state = not running
	program = /path/to/ocserverd
}
OUT
        exit 0 ;;
      *) exit 1 ;;
    esac ;;
  bootout)    touch "$SHIM_STATE/.booted-out";   exit 0 ;;
  bootstrap)  touch "$SHIM_STATE/.bootstrapped"; exit 0 ;;
  kickstart)  exit 0 ;;
esac
exit 0
SH

# lsof: two distinct queries.
#   -p <pid>        → the live-service gate's cosmetic "listening on port(s) …"
#   -iTCP:<port>    → the port gate (before bootstrap: free) and the health gate
#                     (after bootstrap: listening). Keyed on the bootstrap flag
#                     so one stub serves both without a call counter.
#
# EXIT CODES MATTER MORE THAN OUTPUT HERE. The real lsof exits NON-ZERO when a
# query matches nothing, including the ordinary case of a running pid that holds
# no listening socket. An earlier stub returned 0 unconditionally for -p, which
# is LOOSER than the real tool — and that single mismatch hid a fatal bug: the
# gate's own port-lookup aborted the whole installer under `set -e -o pipefail`,
# producing exit 1 with a blank screen, and the suite still went green. A stub
# that is more forgiving than the tool it stands in for does not test the code,
# it tests a fiction. SHIM_NO_LISTEN=1 reproduces that real-world state.
# PORT-AWARE ON PURPOSE (default-port change to 7755). The stub used to print a
# HARDCODED `127.0.0.1:8780` for every query, which made it blind to WHICH port
# install.sh probed: a regression that moved the effective default back would
# still see a listener and still read the same line back. Now the stub echoes
# the port it was actually ASKED about, and every invocation is recorded in the
# tripwire — so a case can assert the probe went to the port the default
# resolves to, not merely that a probe happened.
cat > "$SHIMDIR/lsof" <<'SH'
#!/usr/bin/env bash
echo "lsof $*" >> "$SHIM_TRIPWIRE"
QPORT=""
for a in "$@"; do
  case "$a" in
    -p)
      [[ "${SHIM_NO_LISTEN:-0}" == "1" ]] && exit 1
      echo "ocserverd 4242 tester 5u IPv4 0x0 0t0 TCP 127.0.0.1:${SHIM_LIVE_PORT:-7755} (LISTEN)"
      exit 0 ;;
    -iTCP:*) QPORT="${a#-iTCP:}" ;;
  esac
done
if [[ -f "$SHIM_STATE/.bootstrapped" ]]; then
  echo "ocserverd 4242 tester 5u IPv4 0x0 0t0 TCP 127.0.0.1:${QPORT:-7755} (LISTEN)"
  exit 0
fi
exit 1
SH
chmod +x "$SHIMDIR"/uname "$SHIMDIR"/launchctl "$SHIMDIR"/lsof

PLIST_REL="Library/LaunchAgents/com.officraft.serve.plist"

# reset_fixture <preinstalled|fresh>
#   preinstalled → the re-install shape: binaries + database + a plist that
#                  points at OUR binary (so the ownership gate adopts the label
#                  and the relocation gate sees no move — i.e. every pre-existing
#                  gate says "plain reload", which is exactly the situation the
#                  live-service gate has to catch).
#   fresh        → clean machine.
reset_fixture() {
  rm -rf "$FAKEHOME"
  mkdir -p "$FAKEHOME/Library/LaunchAgents"
  rm -f "$WORK/.booted-out" "$WORK/.bootstrapped" "$WORK/.tripwire"
  : > "$WORK/.tripwire"
  if [[ "$1" == "preinstalled" ]]; then
    mkdir -p "$FAKEHOME/.officraft/bin" "$FAKEHOME/.officraft/server/data"
    printf '#!/usr/bin/env bash\nexit 0\n' > "$FAKEHOME/.officraft/bin/ocserverd"
    chmod +x "$FAKEHOME/.officraft/bin/ocserverd"
    printf 'OLD-BINARY' > "$FAKEHOME/.officraft/bin/ocwarden"
    : > "$FAKEHOME/.officraft/server/data/officraft.db"
    cat > "$FAKEHOME/$PLIST_REL" <<PL
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key><string>com.officraft.serve</string>
  <key>ProgramArguments</key>
  <array>
    <string>$FAKEHOME/.officraft/bin/ocserverd</string>
    <string>serve</string>
  </array>
  <key>RunAtLoad</key><true/>
</dict>
</plist>
PL
  fi
}

# run_install <job-state> [args…] — NOTE: OC_LAUNCHD_LABEL is never set, so the
# run resolves the DEFAULT label, exactly as a real user's re-install does.
# stdin is </dev/null → non-interactive, the `curl | bash` shape.
# The target the script MUST resolve when nobody overrides the label. Every
# assertion about the gate firing is therefore also an assertion that it asked
# launchd about THIS job and no other.
EXPECT_TARGET="gui/$(id -u)/com.officraft.serve"

run_install() {
  local job="$1"; shift
  OUT="$(cd "$WORK" && env -i \
    PATH="$SHIMDIR:/usr/bin:/bin:/usr/sbin:/sbin" \
    HOME="$FAKEHOME" SHIM_JOB="$job" SHIM_TRIPWIRE="$WORK/.tripwire" SHIM_STATE="$WORK" \
    SHIM_EXPECT_TARGET="$EXPECT_TARGET" SHIM_NO_LISTEN="${SHIM_NO_LISTEN:-0}" \
    bash "$PKG/install.sh" "$@" </dev/null 2>&1)"
  RC=$?
}

booted_out() { [[ -f "$WORK/.booted-out" ]] && echo yes || echo no; }
# The tripwire records every launchctl invocation. Asserting ON it is what turns
# "the script did something" into "the script did it TO THE RIGHT JOB".
# `-e` is not optional: needles that START WITH A DASH (an lsof flag such as
# -iTCP:7755) are otherwise parsed by grep as options and the call fails with a
# usage error, which reads as "not found" — a false FAIL that looks like a real
# one.
tripwire_has() { grep -qF -e "$1" "$WORK/.tripwire"; }
# EXACT-LINE variant. tripwire_has is a SUBSTRING match, which silently lies for
# namespaced labels: "launchctl bootout gui/501/com.officraft.serve" is a prefix
# of "…serve.lab", so asking "did it boot out the MAIN station?" answers yes for
# a run that correctly booted out only its own namespaced job. Anything asserting
# about one label NOT being addressed must use this.
tripwire_has_exact() { grep -qxF -e "$1" "$WORK/.tripwire"; }

echo "install.sh live-service gate — hermetic tests (default label, stubbed launchd)"

# ── 1. THE DEFECT: live job + piped/non-interactive run → fail CLOSED ────────
# Every pre-existing gate passes here (same binary, same port, no config move).
# Only the new gate stands between this run and a bootout of the live service.
reset_fixture preinstalled
run_install running
check "live job + non-interactive: aborts" "1" "$RC"
check "live job + non-interactive: never boots the service out" "no" "$(booted_out)"
case "$OUT" in *"A LIVE OffiCraft service is running"*) ok "live job: says WHAT it detected";; *) bad "live job: says WHAT it detected ($OUT)";; esac
case "$OUT" in *DISCONNECTED*) ok "live job: says what the CONSEQUENCE is (clients dropped)";; *) bad "live job: names the consequence";; esac
case "$OUT" in *"--restart-live"*) ok "live job: says what the operator CAN DO (--restart-live)";; *) bad "live job: offers --restart-live";; esac
case "$OUT" in *"OC_LAUNCHD_LABEL"*) ok "live job: offers the install-alongside escape hatch";; *) bad "live job: offers the install-alongside escape hatch";; esac

# Aborting must leave the machine byte-for-byte untouched — the gate runs BEFORE
# the binaries are copied, so a declined run cannot even have half-installed.
check "live job + non-interactive: leaves the old binaries untouched" "OLD-BINARY" "$(cat "$FAKEHOME/.officraft/bin/ocwarden")"

# THE LABEL ITSELF. Everything above would pass just as happily if install.sh
# interrogated some other job and got lucky; this pins that the job it asked
# about is the DEFAULT label — the one the live station actually runs under.
if tripwire_has "launchctl print $EXPECT_TARGET"; then
  ok "live job: install.sh interrogated the DEFAULT label ($EXPECT_TARGET)"
else
  bad "live job: install.sh interrogated the DEFAULT label ($EXPECT_TARGET) — tripwire: $(cat "$WORK/.tripwire")"
fi

# ── 1b. the gate must survive a live pid that holds NO listening socket ──────
# Real lsof exits non-zero for such a pid (a job still starting, crash-looping,
# or bound only to a unix socket). The port list is decoration; if looking it up
# can abort the run, the gate dies before printing anything and the operator
# gets exit 1 against a blank screen — strictly worse than the bug being fixed.
reset_fixture preinstalled
SHIM_NO_LISTEN=1 run_install running
unset SHIM_NO_LISTEN
check "live job w/o listening socket: still aborts" "1" "$RC"
check "live job w/o listening socket: never boots the service out" "no" "$(booted_out)"
case "$OUT" in *"A LIVE OffiCraft service is running"*) ok "live job w/o listening socket: STILL EXPLAINS ITSELF (does not die silently)";; *) bad "live job w/o listening socket: gate produced no explanation (output was: '$OUT')";; esac
if [[ -n "$OUT" ]]; then ok "live job w/o listening socket: output is not empty"; else bad "live job w/o listening socket: output was EMPTY — the gate aborted before it could speak"; fi

# ── 2. --force must NOT authorize an outage ─────────────────────────────────
# The conflation that made this reachable: --force is documented as overwriting
# FILES. Letting it also drop every live connection is the bug, not the feature.
reset_fixture preinstalled
run_install running --force
check "live job + --force: still aborts (force is about files, not uptime)" "1" "$RC"
check "live job + --force: never boots the service out" "no" "$(booted_out)"
case "$OUT" in *"--force alone does NOT authorize"*) ok "live job + --force: explains why --force was not enough";; *) bad "live job + --force: explains why --force was not enough";; esac

# ── 3. the explicit override works (a gate, not a wall) ─────────────────────
reset_fixture preinstalled
run_install running --force --restart-live
check "live job + --restart-live: proceeds" "0" "$RC"
check "live job + --restart-live: boots the old job out" "yes" "$(booted_out)"
case "$OUT" in *"--restart-live given"*) ok "live job + --restart-live: announces the restart";; *) bad "live job + --restart-live: announces the restart";; esac
# and it boots out THAT job, by its full target — not merely "some job".
if tripwire_has "launchctl bootout $EXPECT_TARGET"; then
  ok "live job + --restart-live: bootout targeted the DEFAULT label ($EXPECT_TARGET)"
else
  bad "live job + --restart-live: bootout targeted the DEFAULT label — tripwire: $(cat "$WORK/.tripwire")"
fi
if tripwire_has "launchctl bootstrap"; then
  ok "live job + --restart-live: the service is bootstrapped back up (not left down)"
else
  bad "live job + --restart-live: the service is bootstrapped back up"
fi

# ── 4. no job registered → gate stays silent (fresh install must not regress) ─
reset_fixture fresh
run_install absent
check "no job: fresh install succeeds" "0" "$RC"
case "$OUT" in *"A LIVE OffiCraft service"*) bad "no job: gate must stay silent";; *) ok "no job: gate stays silent";; esac

# ── 5. registered but NOT running → gate stays silent ───────────────────────
# Nothing is serving, so bootout costs nobody a session. Gating here would train
# the operator to type y reflexively, which is how a real gate stops working.
reset_fixture preinstalled
run_install loaded --force
check "loaded-but-stopped job: proceeds without prompting" "0" "$RC"
case "$OUT" in *"A LIVE OffiCraft service"*) bad "loaded-but-stopped: gate must stay silent";; *) ok "loaded-but-stopped: gate stays silent";; esac
case "$OUT" in *"No OffiCraft service is currently running"*) ok "loaded-but-stopped: existing-install prompt tells the truth about uptime";; *) bad "loaded-but-stopped: existing-install prompt tells the truth about uptime";; esac

# ── 6. --foreground never boots a job out, so the gate must not fire ────────
reset_fixture preinstalled
run_install running --force --foreground
check "--foreground + live job: live-service gate does not fire" "no" "$(booted_out)"
case "$OUT" in *"A LIVE OffiCraft service"*) bad "--foreground: gate must not fire (nothing is booted out)";; *) ok "--foreground: gate does not fire";; esac

# ── 7. the misleading reassurance is gone ──────────────────────────────────
# The old text promised "keeps serving its old code until its next restart" on a
# path that restarts it immediately. Consent given against a false description of
# the harm is not consent, so this exact sentence must never come back.
# Matched on an EMITTED line (not a comment), so install.sh may keep explaining
# in prose why the old sentence was wrong without tripping its own guard.
if grep -qE '^[^#]*keeps serving its old' "$SCRIPT"; then
  bad "install.sh no longer promises a running server is left alone (that promise was false on the launchd path)"
else
  ok "install.sh no longer promises a running server is left alone"
fi

# ── 8. interactive shape: a tty must default to NO ──────────────────────────
# `script` allocates a pty so `[[ -t 0 ]]` is genuinely true — this exercises the
# prompt branch rather than asserting it exists. Skipped (loudly) if the host's
# script(1) does not take the BSD form, so a missing pty can never read as a pass.
# The answer is delayed rather than piped straight in: script(1) forwards stdin
# and closes the pty as soon as it drains, so an immediately-available answer can
# reach the child before `read` is blocking on it — the child then sees EOF,
# takes the default, and the case passes for entirely the wrong reason. Waiting
# until the prompt is genuinely on screen is what makes this a test of the
# prompt rather than a test of EOF handling. The trailing sleep keeps the pty
# open long enough for the rest of the run to finish.
run_interactive() {
  local job="$1" answer="$2"; shift 2
  OUT="$(cd "$WORK" && { sleep 0.6; printf '%s\n' "$answer"; sleep 1.2; } | env \
    PATH="$SHIMDIR:/usr/bin:/bin:/usr/sbin:/sbin" \
    HOME="$FAKEHOME" SHIM_JOB="$job" SHIM_TRIPWIRE="$WORK/.tripwire" SHIM_STATE="$WORK" \
    SHIM_EXPECT_TARGET="$EXPECT_TARGET" SHIM_NO_LISTEN="${SHIM_NO_LISTEN:-0}" \
    script -q /dev/null bash "$PKG/install.sh" "$@" 2>&1)"
  RC=$?
}
reset_fixture preinstalled
run_interactive running n --force
if [[ "$OUT" == *"Restart the running service?"* ]]; then
  check "interactive + 'n': aborts" "1" "$RC"
  check "interactive + 'n': never boots the service out" "no" "$(booted_out)"
  case "$OUT" in *"the running service was NOT touched"*) ok "interactive + 'n': confirms nothing was touched";; *) bad "interactive + 'n': confirms nothing was touched";; esac

  # bare Enter = decline. The default must be the safe one.
  reset_fixture preinstalled
  run_interactive running "" --force
  check "interactive + bare Enter: defaults to NO" "1" "$RC"
  check "interactive + bare Enter: never boots the service out" "no" "$(booted_out)"

  # and 'y' really does proceed — otherwise the above could pass on a script
  # that simply always aborts.
  reset_fixture preinstalled
  run_interactive running y --force
  check "interactive + 'y': proceeds" "0" "$RC"
  check "interactive + 'y': boots the old job out" "yes" "$(booted_out)"
else
  echo "  skip — no usable pty via script(1); interactive prompt branch NOT verified here"
fi

# ── 9. the DEFAULT port a fresh install actually resolves is 7755 ───────────
# Not a string check on install.sh — this walks the real fresh-install path with
# NO config anywhere (env -i, temp HOME, CWD=$WORK which holds no oc.toml), so
# the port can only come from DEFAULT_PORT. Two independent witnesses, each its
# own assertion so a break names itself:
#   (a) the PORT GATE probe — install.sh asks lsof about the effective port
#       before writing anything; the port-aware stub records exactly which one.
#   (b) the SETUP LINK the operator is told to open at the end.
# Plus a negative: the retired 8780 must appear nowhere in the run's output. The
# negative alone would be satisfied by an installer that resolved some third
# port, and the positives alone would be satisfied by one that printed 7755
# while probing something else — together they pin the port end to end.
reset_fixture fresh
run_install absent
check "fresh install: succeeds on the default path" "0" "$RC"
if tripwire_has "-iTCP:7755"; then
  ok "fresh install: the port gate probed 7755 (the default is what gets gated)"
else
  bad "fresh install: the port gate did NOT probe 7755 — tripwire: $(cat "$WORK/.tripwire")"
fi
case "$OUT" in *"http://127.0.0.1:7755/"*) ok "fresh install: the setup link the operator is handed is on 7755";; *) bad "fresh install: setup link is not on 7755 (output was: '$OUT')";; esac
case "$OUT" in *8780*) bad "fresh install: the retired 8780 still appears in the run's output ('$OUT')";; *) ok "fresh install: no trace of the retired 8780 in the output";; esac

# ── 10. --namespace: isolation BY CONSTRUCTION (T-5047) ─────────────────────
# WHY THESE CASES EXIST. Before them, the 39 cases above passed identically with
# the namespace derivation DELETED — mutate ROOT_DIR back to "$HOME/.officraft"
# and LABEL back to "com.officraft.serve" (i.e. a namespaced install silently
# targets the MAIN instance's root and boots out the MAIN instance's job, the
# exact disaster the flag exists to prevent) and the whole suite stayed green.
# A guard suite that cannot see that is not guarding the flag, it is guarding
# the code that happened to be written first.
#
# The stub is LABEL-AWARE (see its header), which is what makes this testable:
# SHIM_EXPECT_TARGET is set to the MAIN label while the run asks about the
# NAMESPACED one, so a correct run sees "nothing registered" for its own job and
# the tripwire records which label it actually addressed.
NS="lab"
NS_PORT="7756"
MAIN_TARGET="gui/$(id -u)/com.officraft.serve"
NS_TARGET="gui/$(id -u)/com.officraft.serve.$NS"

# run_install_ns <job-state> <expect-target> [args…] — same as run_install but
# lets a case choose WHICH label the launchctl oracle is willing to answer for.
run_install_ns() {
  local job="$1" expect="$2"; shift 2
  OUT="$(cd "$WORK" && env -i \
    PATH="$SHIMDIR:/usr/bin:/bin:/usr/sbin:/sbin" \
    HOME="$FAKEHOME" SHIM_JOB="$job" SHIM_TRIPWIRE="$WORK/.tripwire" SHIM_STATE="$WORK" \
    SHIM_EXPECT_TARGET="$expect" SHIM_NO_LISTEN="${SHIM_NO_LISTEN:-0}" \
    bash "$PKG/install.sh" "$@" </dev/null 2>&1)"
  RC=$?
}

# 10a. THE DISASTER CASE. A LIVE MAIN station is running, and an operator installs
# a SECOND instance by name. The namespaced run must address its OWN label and
# its OWN root — it must not so much as ask launchd about the main job, let alone
# boot it out. This is the case that goes red the moment the derivation is lost.
reset_fixture preinstalled
run_install_ns running "$MAIN_TARGET" --namespace "$NS" --port "$NS_PORT"
check "ns install alongside a LIVE main station: succeeds" "0" "$RC"
if tripwire_has_exact "launchctl bootout $MAIN_TARGET"; then
  bad "ns install BOOTED OUT THE MAIN STATION ($MAIN_TARGET) — tripwire: $(cat "$WORK/.tripwire")"
else
  ok "ns install never boots out the main station ($MAIN_TARGET)"
fi
if tripwire_has "launchctl bootstrap"; then
  ok "ns install did register a job (the case is not passing by doing nothing)"
else
  bad "ns install never bootstrapped anything — tripwire: $(cat "$WORK/.tripwire")"
fi
if tripwire_has "$NS_TARGET"; then
  ok "ns install addressed its OWN label ($NS_TARGET)"
else
  bad "ns install never addressed $NS_TARGET — tripwire: $(cat "$WORK/.tripwire")"
fi
# The MAIN instance's files must be untouched: the preinstalled fixture's marker
# binary is the witness. Under a lost NS_DASH this gets overwritten.
check "ns install leaves the MAIN instance's binaries untouched" "OLD-BINARY" "$(cat "$FAKEHOME/.officraft/bin/ocwarden" 2>/dev/null)"
if [[ -d "$FAKEHOME/.officraft-$NS" ]]; then
  ok "ns install created its own root (~/.officraft-$NS)"
else
  bad "ns install did NOT create ~/.officraft-$NS — it installed somewhere else"
fi
if [[ -f "$FAKEHOME/Library/LaunchAgents/com.officraft.serve.$NS.plist" ]]; then
  ok "ns install wrote its own plist (com.officraft.serve.$NS.plist)"
else
  bad "ns install did not write com.officraft.serve.$NS.plist — plists: $(ls "$FAKEHOME/Library/LaunchAgents" 2>&1)"
fi

# 10b. the namespaced instance OWNS ITS CONFIG — written under its own root,
# carrying the namespace and the port it was asked for. Without this the migrate
# below would create the MAIN instance's database.
NS_CFG="$FAKEHOME/.officraft-$NS/server/oc.toml"
if [[ -f "$NS_CFG" ]]; then
  ok "ns install wrote its own config ($NS_CFG)"
  case "$(cat "$NS_CFG")" in *"namespace = \"$NS\""*) ok "ns config carries namespace = \"$NS\"";; *) bad "ns config lacks the namespace: $(cat "$NS_CFG")";; esac
  case "$(cat "$NS_CFG")" in *"port = $NS_PORT"*) ok "ns config carries port = $NS_PORT";; *) bad "ns config lacks port $NS_PORT: $(cat "$NS_CFG")";; esac
else
  bad "ns install wrote no config under its own root"
fi
# and the port GATE probed the namespaced port, not the main instance's 7755.
if tripwire_has "-iTCP:$NS_PORT"; then
  ok "ns install gated the port it was given ($NS_PORT)"
else
  bad "ns install did not probe $NS_PORT — tripwire: $(cat "$WORK/.tripwire")"
fi
case "$OUT" in *"http://127.0.0.1:$NS_PORT/"*) ok "ns install hands the operator a link on $NS_PORT";; *) bad "ns install setup link is not on $NS_PORT (output: '$OUT')";; esac

# 10c. a stray oc.toml in the CWD must NOT be able to redirect a run that asked
# for a specific instance by name. (The main instance deliberately still honours
# it — that is the relocation gate's job — but "I asked for instance lab" is not
# a question the file next to me gets to answer.)
reset_fixture fresh
printf '[server]\nport = 9999\n' > "$WORK/oc.toml"
run_install_ns absent "$MAIN_TARGET" --namespace "$NS" --port "$NS_PORT"
rm -f "$WORK/oc.toml"
check "ns install ignores a stray ./oc.toml: succeeds" "0" "$RC"
if tripwire_has "-iTCP:9999"; then
  bad "ns install was REDIRECTED by a stray ./oc.toml (probed 9999)"
else
  ok "ns install was not redirected by a stray ./oc.toml"
fi

# 10d. --port is REQUIRED with --namespace. Inheriting the default 7755 would
# either collide with the main instance at the port gate or, worse, look
# installed while fighting it for the socket.
reset_fixture fresh
run_install_ns absent "$MAIN_TARGET" --namespace "$NS"
check "--namespace without --port: refused (exit 2)" "2" "$RC"
case "$OUT" in *"requires --port"*) ok "--namespace without --port: says what is missing";; *) bad "--namespace without --port: unhelpful message ('$OUT')";; esac
if [[ -e "$FAKEHOME/.officraft" ]]; then bad "--namespace without --port still touched the MAIN root"; else ok "--namespace without --port touched nothing"; fi

# 10e. --port ALONE is refused too — otherwise it reads as "change the main
# instance's port", which is a relocation and not this flag's job.
reset_fixture fresh
run_install_ns absent "$MAIN_TARGET" --port "$NS_PORT"
check "--port without --namespace: refused (exit 2)" "2" "$RC"

# 10f. a MALFORMED namespace must be a hard error, never a silent fold back to
# the main instance's root and label — that fold is strictly worse than the error.
reset_fixture preinstalled
run_install_ns running "$MAIN_TARGET" --namespace "BAD_NS!" --port "$NS_PORT"
check "malformed --namespace: refused (exit 2)" "2" "$RC"
check "malformed --namespace: did NOT fall back to the main instance" "OLD-BINARY" "$(cat "$FAKEHOME/.officraft/bin/ocwarden" 2>/dev/null)"
check "malformed --namespace: never boots anything out" "no" "$(booted_out)"
case "$OUT" in *"[a-z0-9-]{1,16}"*) ok "malformed --namespace: names the accepted charset";; *) bad "malformed --namespace: does not name the charset ('$OUT')";; esac

# 10g. re-running the SAME instance with a DIFFERENT port is a relocation, not a
# reload: nothing may change until the operator says so deliberately.
reset_fixture fresh
run_install_ns absent "$MAIN_TARGET" --namespace "$NS" --port "$NS_PORT"
check "ns install (first run): succeeds" "0" "$RC"
printf 'MARKER-NS' > "$FAKEHOME/.officraft-$NS/bin/ocwarden"
run_install_ns absent "$MAIN_TARGET" --namespace "$NS" --port "7799" --force
check "ns re-install with a different port: refused (exit 1)" "1" "$RC"
# --force is passed so the existing-install prompt cannot be what stopped the
# run: the refusal under test has to be the PORT one.
check "ns port change: the refusal is not just the existing-install prompt" "MARKER-NS" "$(cat "$FAKEHOME/.officraft-$NS/bin/ocwarden")"
case "$OUT" in *"relocation, not a reload"*) ok "ns port change: explains it is a relocation";; *) bad "ns port change: unclear message ('$OUT')";; esac
case "$(cat "$NS_CFG")" in *"port = $NS_PORT"*) ok "ns port change: the existing config was NOT rewritten";; *) bad "ns port change: config was mutated to $(cat "$NS_CFG")";; esac

# 10h. THE EMPTY NAMESPACE CHANGES NOTHING — the sentinel. Every case above would
# also pass on a build that broke the MAIN path outright, so pin that a plain
# fresh install still lands on ~/.officraft / com.officraft.serve and creates NO
# namespaced root.
reset_fixture fresh
run_install absent
check "no --namespace: fresh install still succeeds" "0" "$RC"
if [[ -d "$FAKEHOME/.officraft" ]]; then ok "no --namespace: still installs to ~/.officraft"; else bad "no --namespace: ~/.officraft was not created"; fi
if compgen -G "$FAKEHOME/.officraft-*" >/dev/null; then bad "no --namespace: a namespaced root appeared out of nowhere"; else ok "no --namespace: no namespaced root is created"; fi
if [[ -f "$FAKEHOME/$PLIST_REL" ]]; then ok "no --namespace: still uses com.officraft.serve.plist"; else bad "no --namespace: default plist missing"; fi
if [[ -e "$FAKEHOME/.officraft/server/oc.toml" ]]; then bad "no --namespace: an instance config was invented for the MAIN instance"; else ok "no --namespace: no instance config is invented (main reads OC_CONFIG/./oc.toml as before)"; fi

echo "install-guard tests: $PASS ok, $FAIL failed"
[[ "$FAIL" == "0" ]] || exit 1
exit 0
