#!/usr/bin/env python3
"""e2e_test/seven_gate/lib/varcheck.py — catch an unbound-variable typo WITHOUT
spawning an agent.

🔴 WHY THIS EXISTS, AND WHAT IT COST. actors/live.sh runs under `set -u`. On the
first real-agent run it died on line 213 with

    actors/live.sh: line 213: OC_SG_LIVE_WAITs: unbound variable

— a single letter. The refactor that moved the wait defaults into lib/window.sh
rewrote `${OC_SG_LIVE_WAIT:-1800}s` as `$OC_SG_LIVE_WAIT` + a stray `s`, gluing
the unit onto the NAME. The damage was not the crash: the agent had ALREADY BEEN
SPAWNED and ① had passed, so the actor died mid-run, its trap killed the tmux
session, and ②..⑨ all went red. The verdict read 「the agent did nothing」 when
the truth was 「the harness killed it」 — and the run had spent real money to
produce exactly zero information.

CI was green through all of it, because CI never executes live.sh: the file only
runs on a real run, which is the one thing CI must never do. So the guard cannot
be "run it" — it has to be a check that walks every variable REFERENCE in the
file while spawning nothing and spending nothing. That is this script.

THE RULE. Under `set -u`, a reference is safe if it either
  * carries its own default / alternate / error — ${V:-x} ${V-x} ${V:=x} ${V+x}
    ${V:?x} — those can never be an *unbound* surprise; or
  * names something the file assigns, or something lib/window.sh exports, or a
    variable the caller is contractually required to pass, or a shell builtin.
Anything else is a NAME NOBODY EVER SETS, i.e. a typo, i.e. a run that dies
half-way. That is precisely the class that cost the money above.

SCOPE — point it at the seven_gate harness scripts, which is where the money
is. It does NOT do full bash quote tracking: a file whose content is mostly
QUOTED SHELL SNIPPETS ABOUT OTHER SCRIPTS (tests_guard/run.sh is exactly that —
sed programs, fixture bodies, message text naming variables) will over-report,
because `$x` inside a single-quoted fixture looks identical to a real expansion.
Escaped dollars are handled; single-quote spans are deliberately NOT stripped —
doing so would swallow the rest of a line after an apostrophe in ordinary prose
("⑦'s fact"), and a checker that HIDES a real reference is worse than one that
names an extra. Widen the file list only with that trade in mind.

LIMITS, stated so nobody reads more into a green than is there:
  * NAME-level only. It proves every referenced name is bound somewhere; it does
    NOT prove the value is sane, the logic is right, or the run would succeed.
  * `eval`, indirect expansion (${!x}) and names built by string concatenation
    are invisible to it. None appear in these files today.
It is a floor under one specific, expensive, repeat-prone mistake — not a
substitute for the real run.

  python3 varcheck.py <script.sh> [more.sh …]     rc 0 = clean, 1 = unbound
"""
import re
import sys

# Referenced as $NAME or ${NAME}. ${NAME:-…} and friends are handled separately:
# the modifier is what makes them safe, so they are stripped out first.
REF = re.compile(r'\$\{([A-Za-z_][A-Za-z0-9_]*)\}|\$([A-Za-z_][A-Za-z0-9_]*)')
# Any ${NAME<modifier>…} — the modifier means "unbound is impossible here".
GUARDED = re.compile(r'\$\{([A-Za-z_][A-Za-z0-9_]*)[:#%/^,!*@\[-]')
GUARDED_ALT = re.compile(r'\$\{([A-Za-z_][A-Za-z0-9_]*)[+?=]')

ASSIGN = [
    # An assignment anywhere a command may start: line head, or after ; && || ( {
    re.compile(r'(?:^\s*|[;&|(){]\s*)([A-Za-z_][A-Za-z0-9_]*)='),
    re.compile(r'\b(?:local|declare|typeset|readonly|export)\s+([A-Za-z_][A-Za-z0-9_]*)'),
    re.compile(r'\bfor\s+([A-Za-z_][A-Za-z0-9_]*)\s+in'),
    # `read x`, `read -r x`, `while IFS= read -r x` — the loop variable is bound.
    re.compile(r'\bread\s+(?:-[A-Za-z]+\s+)*([A-Za-z_][A-Za-z0-9_]*)'),
    re.compile(r'^\s*:\s*"\$\{([A-Za-z_][A-Za-z0-9_]*):='),
]
# `export A B C` / `local a b` bind every name on the line.
MULTI = re.compile(r'\b(?:export|local|declare|readonly)\s+(.*)$')
# `read -r A B C` binds every name on the line.
MULTI_READ = re.compile(r'\bread\s+(?:-[A-Za-z]+\s+)*(.*)$')
# `: "${VAR:?msg}"` is a REQUIREMENT, not a typo risk: the script has declared
# that the caller must supply VAR and exits loudly if not. Treat as bound.
REQUIRED = re.compile(r'\$\{([A-Za-z_][A-Za-z0-9_]*):\?')

# Shell/bash builtins and ambient environment a script may always reference.
BUILTIN = {
    "IFS", "PATH", "HOME", "PWD", "OLDPWD", "TMPDIR", "SHELL", "USER", "LOGNAME",
    "LANG", "LC_ALL", "TERM", "HOSTNAME", "RANDOM", "SECONDS", "LINENO",
    "BASH_SOURCE", "BASH_VERSION", "FUNCNAME", "PIPESTATUS", "REPLY", "OPTARG",
    "OPTIND", "PPID", "UID", "EUID", "BASHPID",
}


def collect_assigned(text):
    names = set()
    for line in text.splitlines():
        for pat in ASSIGN:
            for m in pat.finditer(line):
                names.add(m.group(1))
        m = MULTI.search(line)
        if m:
            for tok in m.group(1).split():
                tok = tok.split("=")[0].strip()
                if re.fullmatch(r'[A-Za-z_][A-Za-z0-9_]*', tok):
                    names.add(tok)
        m = MULTI_READ.search(line)
        if m:
            for tok in m.group(1).split():
                if tok.startswith("<") or tok.startswith("#"):
                    break
                if re.fullmatch(r'[A-Za-z_][A-Za-z0-9_]*', tok):
                    names.add(tok)
        names |= set(REQUIRED.findall(line))
    return names


def sourced_names(text, base_dir):
    """Names bound by the files this script sources. Resolution is by BASENAME
    searched up the tree, not by literally expanding the path variables: the
    scripts say `. "$SG/lib/window.sh"` and `. "$E2E/lib/common.sh"`, and a
    checker that could not follow those would flag every knob they define and be
    deleted within the day. Over-reaching here is safe — the worst case is that
    a real name is considered bound, never that a typo is invented."""
    import os
    names = set()
    roots = []
    d = base_dir
    for _ in range(4):
        roots += [d, os.path.join(d, "lib")]
        d = os.path.dirname(d)
    for m in re.finditer(r'^\s*(?:\.|source)\s+"?([^"\s]+)"?', text, re.M):
        leaf = os.path.basename(m.group(1).rstrip('"'))
        if not leaf or leaf.startswith("$"):
            continue
        for root in roots:
            cand = os.path.join(root, leaf)
            if os.path.isfile(cand):
                with open(cand, encoding="utf-8") as fh:
                    names |= collect_assigned(fh.read())
                break
    return names


def check(path):
    import os
    with open(path, encoding="utf-8") as fh:
        text = fh.read()
    base_dir = os.path.dirname(os.path.abspath(path))
    known = collect_assigned(text) | sourced_names(text, base_dir) | BUILTIN

    problems = []
    for lineno, line in enumerate(text.splitlines(), 1):
        code = line.split("#", 1)[0] if not line.lstrip().startswith("#") else ""
        if not code:
            continue
        # A backslash-escaped dollar is never an expansion — it is the script
        # TALKING ABOUT a variable (error messages, sed programs, fixtures).
        code = code.replace(r"\$", "")
        guarded = set(GUARDED.findall(code)) | set(GUARDED_ALT.findall(code))
        for m in REF.finditer(code):
            name = m.group(1) or m.group(2)
            if name in guarded or name in known:
                continue
            problems.append((lineno, name, line.strip()))
    return problems


def main(argv):
    if len(argv) < 2:
        print("usage: varcheck.py <script.sh> […]", file=sys.stderr)
        return 2
    bad = 0
    for path in argv[1:]:
        for lineno, name, src in check(path):
            bad += 1
            print("%s:%d: UNBOUND '%s' — referenced with no default and assigned "
                  "nowhere. Under `set -u` this kills the script AT THIS LINE, "
                  "mid-run.\n    %s" % (path, lineno, name, src))
    if bad:
        print("\nvarcheck: %d unbound reference(s). On actors/live.sh this class "
              "of typo has already burned one paid run: the agent was spawned, "
              "the actor then died here, and the verdict blamed the agent."
              % bad)
        return 1
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv))
