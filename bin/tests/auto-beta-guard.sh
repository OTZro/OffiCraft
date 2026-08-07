#!/usr/bin/env bash
# bin/tests/auto-beta-guard.sh — guards the AUTOMATIC BETA path (T-9fe3): the
# auto-beta job in .github/workflows/ci.yml, and the version rule in
# bin/next-beta-tag that feeds it.
#
# WHY A WORKFLOW FILE NEEDS A GUARD AT ALL
# ---------------------------------------
# ⚠️ NOTHING ELSE IN THIS REPO PARSES .github/workflows/*.yml. That is not a
# theoretical gap: a change to this workflow has already gone green through every
# local gate and then produced a GitHub STARTUP FAILURE — zero jobs, no checks
# run, and a commit list that looks exactly like a commit nobody pushed. bin/ci.sh
# cannot catch it because it never reads the file. So the first assertion here is
# simply "it parses", and the parser is a HARD dependency: a guard that skips when
# it cannot find a YAML parser reports the same silence as a guard that found no
# problems.
#
# WHY ruby AND NOT PyYAML
# The hosted macOS runner this suite runs on has NO PyYAML, and this file is
# dispatched by bin/tests/run.sh, which bin/ci-macos-host.sh runs there. Ruby with
# its bundled psych IS present on macOS (/usr/bin/ruby) and on the dev machine, so
# it is the one parser both environments actually have. It is resolved by absolute
# path fallback for the same reason bin/ci.sh resolves go/npm/gitleaks that way —
# a minimal-PATH caller must not be able to turn this into a skip.
#
# 🔴 "NO PARSER FOUND" IS A FAILURE ON PURPOSE. DO NOT TURN IT INTO A SKIP.
# It is the same discipline bin/ci.sh applies to gitleaks and npm: an unrun check
# and a passing check are indistinguishable from the outside, so absence has to be
# loud. If you are here because this reddened on some machine, install ruby — the
# failure message names what is missing and how — rather than making the guard
# tolerate its own absence. A tolerant version of this line would have let the
# startup failure described above through silently, which is the whole reason the
# assertion exists.
# ⚠️ What is NOT claimed anywhere here: that any particular hosted runner image
# has ruby. That is answered by the cloud round itself, not by a local assumption.
#
# WHAT THE ASSERTIONS ARE AIMED AT
# Each one is a way the automatic path could go quietly wrong rather than loudly:
#   W1  a `needs` list that falls behind a newly added gate — the job would then
#       publish a commit only SOME of the checks passed. Written as a set
#       DIFFERENCE in both directions, deliberately not as a list to compare
#       against: an enumeration in the test is a second copy of the answer, and it
#       goes stale in exactly the same way the one in the workflow does.
#       ⚠️ The set differenced against is the GATE jobs, not "all other jobs".
#       That changed with T-5d3b's notify-main-red, and the reason is not taste:
#       notify runs only when a round FAILED, so an auto-beta that needed it would
#       never run — while notify needs auto-beta (a publish failing on main is the
#       silence this whole ticket is about). Requiring both would be a `needs`
#       cycle, which GitHub answers by scheduling ZERO jobs. What W1 is actually
#       protecting is unchanged and still complete in both directions: a gate
#       missing from `needs` reddens, and so does a `needs` entry that is not a
#       gate.
#   W1r THE CLASSIFICATION ITSELF, because W1 is only worth as much as it. A
#       hardcoded list of which jobs are gates would be the same stale second copy
#       W1 refuses to be, so each job DECLARES its role in a marker comment and
#       this asserts every job has exactly one readable marker. A job it cannot
#       classify is a FAILURE THAT NAMES THE LINES — never a silent default into
#       either bucket. ("I could not read it" printed as "nothing to report" is
#       the one thing this file's header has argued against throughout.)
#       The marker is also corroborated so it cannot be a free pass: a not-a-gate
#       must pin itself to refs/heads/main (it produces no verdict a pull request
#       could ever see), and a gate must NOT — otherwise hanging an `if` on a real
#       gate would relabel it out of the required set, which is exactly the bypass
#       W1 exists to stop.
#   W1x THE EXEMPTION ROLL-CALL. W1r stops a job being mislabelled; it does not
#       stop one being honestly labelled a non-gate and thereby leaving the set
#       auto-beta waits for. CLAUDE.md's land criteria answer that with a ruling —
#       the exemption list is a hardcoded roll-call and a third member needs its
#       own owner ruling — and this is what makes that sentence a mechanism rather
#       than prose. The enumeration is the point here, not a smell: it cannot go
#       stale in silence, because the only way past it is to edit it.
#       ⚠️ Corroboration on the GATE side is an ALLOWLIST, not a ref spelling: a
#       gate may carry no job-level `if`, or one that is exactly on a roll-call in
#       this file. Matching one literal spelling was a hole — `github.ref_name ==
#       'main'` and `startsWith(github.ref, …)` both took a real gate off pull
#       requests while this stayed green — and the first fix for that (ban every
#       job-level `if`) reddened `draft != true` too, which is not a bypass.
#       It also refuses `continue-on-error` on ANY job: that field makes a failing
#       job report success outward, which satisfies auto-beta's `needs` AND makes
#       notify-main-red's `failure()` false. Nothing here used to read it.
#   W2  the `if` compared EXACTLY against the canonical condition. Asking whether
#       both halves appear was a hole: `(<canonical>) || github.event_name ==
#       'pull_request'` contains both substrings and puts the only job holding
#       `contents: write` on pull requests, publishing unmerged code as a real
#       prerelease.
#   W3  `contents: write` leaking to another job or to the workflow default: the
#       three gate jobs run untrusted fork code and must not be able to write.
#   W4  GA becoming automatic. The beta→final flip is a human decision (owner
#       ruling), so nothing under `.github/` may even NAME that subcommand — the
#       scope is the whole directory, not just `workflows/`, because a composite
#       action under `.github/actions/` is not a workflow file and slipped past.
#       W4d then refuses the REFERENCE rather than a location (no `uses: ./…`
#       anywhere under `.github/`), because a composite action kept OUTSIDE
#       `.github/` walked past both the grep scope and the earlier directory ban.
#       W4pc and W4dpc prove those greps match a planted example rather than
#       reporting an empty result for free.
#   W5  the publish call losing --no-settle (every unattended run would then fail
#       on a station it cannot reach) or losing --target (it would publish
#       whatever main points at when the runner starts, not the commit that was
#       checked).
#   W6  a shallow checkout: `git worktree add --detach <sha>` and the tag
#       comparison both need real history.
#   W7  the `on:` block, which NOTHING here used to read: a filter there
#       (`paths-ignore: ['**']`) makes every gate skip on every pull request
#       without touching a job, and every assertion above stays green.
#   N*  the version rule itself, driven hermetically with fabricated tag lists.
#
# HERMETICITY: nothing here runs a workflow, contacts GitHub, or invokes gh/git
# tag-mutating commands. The N cases `source` bin/next-beta-tag (its dispatch is
# guarded on "am I being executed") and call its functions with tag lists on
# stdin, so they test the REAL rule rather than a restatement of it.
set -uo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT="$(cd "$HERE/../.." && pwd)"
WF_DIR="$ROOT/.github/workflows"
WF="$WF_DIR/ci.yml"
NBT="$ROOT/bin/next-beta-tag"
JOB="auto-beta"
ROLE_MARKER="oc-job-role"

PASS=0; FAIL=0
ok()   { PASS=$((PASS+1)); printf '  ok   — %s\n' "$1"; }
bad()  { FAIL=$((FAIL+1)); printf '  FAIL — %s\n' "$1"; }
check(){ # check DESC EXPECTED ACTUAL
  if [[ "$2" == "$3" ]]; then ok "$1"; else bad "$1 (want '$2' got '$3')"; fi
}
fatal() { printf '  FAIL — %s\n' "$1"; echo "auto-beta guard: $PASS ok, $((FAIL+1)) failed"; exit 1; }

[[ -f "$WF" ]] || fatal "$WF is missing — the automatic beta path cannot exist without it"
[[ -f "$NBT" ]] || fatal "$NBT is missing — the auto-beta job calls it to compute the tag"

echo "auto-beta path guard (workflow shape + version rule; no network, no gh, no workflow run)"

# ── the parser, fail-closed ─────────────────────────────────────────────────
# OC_AUTO_BETA_RUBY is a TEST SEAM: pointing it at a nonexistent path is how the
# "no parser is a FAILURE, never a skip" branch below gets exercised for real.
#
# ⚠️ IT IS NOT HARMLESS, AND AN EARLIER VERSION OF THIS COMMENT CLAIMED IT WAS.
# It said the seam "cannot weaken the guard — anything that is not a working YAML
# parser produces unusable JSON". A reviewer disproved that in one go: a fake
# `ruby` that ignores its input and writes a hand-made JSON blob to the output
# path let a workflow file of literal garbage score 19 ok / rc=0. So the seam CAN
# hand this guard a lie, and the honest statement is narrower: nothing in any CI
# path sets it, and the positive control below makes a fake parser have to work at
# being convincing rather than merely exist.
RUBY="${OC_AUTO_BETA_RUBY:-}"
if [[ -z "$RUBY" ]]; then
  RUBY="$(command -v ruby 2>/dev/null || true)"
  if [[ -z "$RUBY" ]]; then
    for cand in /usr/bin/ruby /opt/homebrew/bin/ruby /usr/local/bin/ruby; do
      [[ -x "$cand" ]] && { RUBY="$cand"; break; }
    done
  fi
fi
if [[ -z "$RUBY" || ! -x "$RUBY" ]]; then
  NO_PARSER_MSG='no YAML parser found, so .github/workflows/ci.yml cannot be PARSED.
    WHAT IS MISSING: a ruby executable. Looked for: `ruby` on PATH, then
      /usr/bin/ruby (Apple system ruby, present on every macOS), then
      /opt/homebrew/bin/ruby, then /usr/local/bin/ruby.
    HOW TO FIX: install ruby (macOS ships one at /usr/bin/ruby; Homebrew:
      `brew install ruby`; Debian/Ubuntu: `apt-get install -y ruby`). psych is
      bundled with ruby, so nothing else is needed. PyYAML is deliberately NOT
      used here — the hosted macOS runner does not have it.
    WHY THIS IS A FAILURE AND NOT A SKIP (deliberate — do not "fix" it by
      skipping): nothing else in this repo parses a workflow file. A skip and a
      clean parse produce the same silence, and what that silence hides is a
      GitHub startup failure — zero jobs, no checks run, and a commit list that
      looks exactly like a commit that passed. That has already happened once.'
  fatal "$NO_PARSER_MSG"
fi

WORK="$(mktemp -d -t oc-auto-beta-guard.XXXXXX)"
trap 'rm -rf "$WORK" 2>/dev/null || true' EXIT
JSON="$WORK/ci.json"

# parse_yaml <in.yml> <out.json> — the ONE parser invocation, used by both the
# real parse and the positive control below. psych is YAML 1.1, so the `on:` key
# comes back as the boolean true; nothing here needs it and the JSON dump just
# carries it as the string "true".
parse_yaml() {
  "$RUBY" -ryaml -rjson -e '
    doc = YAML.safe_load(File.read(ARGV[0]))
    abort "top level is not a mapping" unless doc.is_a?(Hash)
    abort "no jobs mapping" unless doc["jobs"].is_a?(Hash)
    File.write(ARGV[1], JSON.dump(doc))
  ' "$1" "$2" 2>"$WORK/parse.err"
}

# ── W0pc: the parser is REAL — a positive control on the tool itself ─────────
# Every other assertion in this file is downstream of one belief: that whatever
# answered as the parser actually parsed the file. A reviewer broke exactly that
# belief — a fake `ruby` that ignores its input and writes a hand-made JSON blob
# scored 19 ok / rc=0 against a ci.yml containing literal garbage. "It resolved to
# something executable" is not the same claim as "it parses YAML".
#
# So: feed it a document that is definitively NOT valid YAML and require it to
# REFUSE. A parser that accepts this is not a parser, and every verdict it would
# have produced about the real workflow is worthless — hence fatal, not a bad().
# This has to come BEFORE the real parse: an unreal parser must not get the chance
# to answer questions about ci.yml at all.
printf '%s\n' 'this: is: not: valid: yaml: [' '  "unterminated' > "$WORK/invalid.yml"
if parse_yaml "$WORK/invalid.yml" "$WORK/invalid.json"; then
  fatal "W0pc the YAML parser ($RUBY) ACCEPTED a file that is not valid YAML. It is not really parsing, so no verdict in this guard about .github/workflows/ci.yml means anything. (A fake parser behind OC_AUTO_BETA_RUBY looks exactly like this.)"
else
  ok "W0pc the parser REJECTS invalid YAML (it is really parsing, not rubber-stamping)"
fi
# …and the other half of the control: it must ACCEPT a minimal valid workflow.
# Without this, "rejects everything" would pass W0pc and then fail W0 with a
# message blaming ci.yml for a broken parser.
printf '%s\n' 'name: t' 'jobs:' '  a:' '    runs-on: ubuntu-latest' > "$WORK/valid.yml"
if parse_yaml "$WORK/valid.yml" "$WORK/valid.json"; then
  ok "W0pc …and ACCEPTS a minimal valid workflow (it is not simply refusing everything)"
else
  fatal "W0pc the YAML parser ($RUBY) rejected a MINIMAL VALID workflow, so a red W0 below would be the parser's fault and not ci.yml's: $(tr '\n' ' ' < "$WORK/parse.err")"
fi

if parse_yaml "$WF" "$JSON"; then
  ok "W0 .github/workflows/ci.yml PARSES as YAML and carries a jobs mapping"
else
  fatal "W0 .github/workflows/ci.yml does NOT parse (GitHub would report a startup failure: zero jobs, no checks): $(tr '\n' ' ' < "$WORK/parse.err")"
fi

# q — ask one question of the parsed workflow. Each assertion is its own tiny
# query so a red names one property rather than "something in the job is wrong".
q() { JOB="$JOB" python3 "$WORK/q.py" "$JSON" "$@"; }
cat > "$WORK/q.py" <<'PY'
import json, os, re, sys

doc = json.load(open(sys.argv[1]))
what = sys.argv[2]
jobs = doc.get("jobs") or {}
job = os.environ["JOB"]
me = jobs.get(job) or {}

def norm(s):
    # Collapse shell line continuations and whitespace runs so a `run:` block
    # split over several lines compares as the one command it is.
    return re.sub(r"\s+", " ", str(s).replace("\\\n", " ")).strip()

def norm_quotes(cond):
    """Rewrite double-quoted string literals in an expression as single-quoted.

    An exact comparison of a whole `if:` is the right shape — it is the only one
    that can see a `|| …` bolted on beside the canonical condition. But exact ON
    THE RAW TEXT also reddens for a rewrite that changed NOTHING: writing
    `github.event_name == "push"` instead of `'push'` is the same condition, and a
    reviewer measured that false red (36 ok, 1 failed). A guard that reddens for a
    no-op edit teaches people to edit the guard to make it quiet, which is the one
    habit this file cannot afford. So quotes are canonicalised first and the
    STRUCTURE is still compared verbatim.

    ⚠️ This normalises quoting ONLY. Nothing else about the condition is
    forgiven: an added clause, a different operator, a negation, a renamed
    context all still redden, because everything outside a string literal is
    compared byte for byte.
    """
    out, i = [], 0
    while i < len(cond):
        c = cond[i]
        if c != '"':
            out.append(c); i += 1; continue
        j, buf = i + 1, []
        while j < len(cond) and cond[j] != '"':
            if cond[j] == "\\" and j + 1 < len(cond):
                buf.append(cond[j + 1]); j += 2; continue
            buf.append(cond[j]); j += 1
        if j >= len(cond):
            # Unterminated quote: not something to guess at. Hand the original
            # back so the comparison reddens on the real text.
            return cond
        # GitHub escapes a quote inside a single-quoted literal by doubling it.
        out.append("'" + "".join(buf).replace("'", "''") + "'")
        i = j + 1
    return "".join(out)

def unwrap(cond):
    """Strip the optional outer `${{ }}` — both spellings are legal and equal."""
    m = re.fullmatch(r"\$\{\{\s*(.*?)\s*\}\}", cond)
    return m.group(1) if m else cond

def publish_steps():
    return [s for s in (me.get("steps") or [])
            if "bin/release publish" in str(s.get("run", ""))]

def publish_runs():
    return [norm(s.get("run", "")) for s in publish_steps()]

def resolves_to(step, token, wanted):
    """Does `token`, as written in a step's `run:`, carry the value `wanted`?

    Accepts BOTH plumbings, because they are the same claim about the value and a
    guard that hardcodes one of them turns the other into a red — which is how a
    guard ends up blocking its own hardening. Interpolated directly into the run
    body (`${{ github.sha }}`), or passed through the step's env (`"$OC_SHA"`
    where env.OC_SHA is `${{ github.sha }}`). Anything else — a branch name, a
    ref expression, an env var bound to something other than `wanted` — is not."""
    token = unquote(token.strip())
    if token == wanted:
        return True
    m = re.fullmatch(r'\$\{?([A-Za-z_][A-Za-z0-9_]*)\}?', token)
    if not m:
        return False
    return str(((step.get("env") or {}).get(m.group(1), ""))).strip() == wanted

def unquote(tok):
    if len(tok) >= 2 and tok[0] == tok[-1] and tok[0] in "\"'":
        return tok[1:-1]
    return tok

def flag_value(cmd, flag):
    """The value token following `flag` in a normalised command line, or ''.

    `${{ … }}` is ONE token even though it contains spaces — an earlier version
    split on whitespace, so `--target ${{ github.sha }}` came back as the string
    `${{`. That made the direct-interpolation shape fail W5, i.e. the assertion
    accepted only the env plumbing, which is the same one-shape-hardcoded defect
    in the other direction. Caught by the mutant that reverts to interpolation and
    must still PASS."""
    m = re.search(re.escape(flag) + r"\s+(\$\{\{.*?\}\}|\"[^\"]*\"|'[^']*'|\S+)", cmd)
    return m.group(1) if m else ""

if what == "job-present":
    print("yes" if job in jobs else "no")

elif what == "if-raw":
    # THE WHOLE CONDITION, normalised only for whitespace, for the optional
    # outer `${{ }}` wrapper, and for the QUOTING of string literals (all three
    # are legal GitHub spellings that mean the same thing — see norm_quotes for
    # the false red the third one produced). Compared VERBATIM by the caller.
    #
    # ⚠️ IT USED TO ASK "are these two substrings present?" AND THAT WAS A HOLE.
    # A reviewer turned the condition into
    #   (github.event_name == 'push' && github.ref == 'refs/heads/main') || github.event_name == 'pull_request'
    # and the guard stayed 35 ok / 0 failed: both substrings were still there, and
    # nothing looked at the BOOLEAN STRUCTURE around them. That mutant makes the
    # one job holding `contents: write` run on pull requests, so an unmerged branch
    # publishes a real prerelease. Substring presence cannot see a disjunction
    # bolted on beside it; an exact comparison can, and it also reddens for `||`,
    # for a third clause, and for a negation. If you need auto-beta to run on
    # another trigger, change WANT_IF in this guard IN THE SAME COMMIT — that edit
    # is the deliberate act this assertion exists to force.
    print(norm_quotes(unwrap(norm(me.get("if", "")))) or "-")

elif what == "on-raw":
    # ── THE TRIGGERS THEMSELVES, which NOTHING in this file used to read ───────
    # Every gate assertion above is downstream of one unexamined belief: that the
    # gate jobs run on a pull request at all. They are filtered by the `on:`
    # block, not by anything inside a job, so a reviewer put
    # `paths-ignore: ['**']` under `on.pull_request` and every gate stopped
    # producing a verdict on every pull request while this guard reported
    # 37 ok, 0 failed. That is the same silence as a skipped gate — a check-runs
    # list with no entry looks exactly like a check that did not go red — and it
    # is reached without touching a single job.
    #
    # Compared as the WHOLE mapping, canonicalised, for the same reason W2 is:
    # asking "is there a pull_request key?" cannot see a filter bolted on beside
    # it, and enumerating the filter keys that can suppress a run (paths,
    # paths-ignore, branches, branches-ignore, types, tags…) is the losing game
    # the gate-`if` rule already lost once.
    # ⚠️ THE HONEST COST: adding a legitimate trigger (workflow_dispatch, a
    # schedule) reddens here and has to be changed in this guard in the SAME
    # commit. That edit is the deliberate act this assertion exists to force —
    # a publish-on-demand button nobody reviewed as one is exactly what
    # auto-beta's `github.event_name == 'push'` half is guarding against.
    #
    # psych is YAML 1.1, so an unquoted `on:` key comes back as the boolean true;
    # a quoted `"on":` stays the string. Both are read, and BOTH PRESENT is a
    # failure rather than a pick — the loser's triggers would go unexamined.
    found = [k for k in ("true", "on") if k in doc]
    if len(found) != 1:
        print("on-keys=%s (want exactly one)" % (",".join(found) or "none"))
    else:
        print(json.dumps(doc[found[0]], sort_keys=True, separators=(",", ":")))

elif what == "write-holders":
    # Every job whose own permissions grant contents: write, plus the workflow
    # default. The answer must be exactly this one job.
    holders = [n for n, j in sorted(jobs.items())
               if str(((j or {}).get("permissions") or {}).get("contents", "")) == "write"]
    print("jobs=%s default=%s" % (",".join(holders) or "-",
                                  (doc.get("permissions") or {}).get("contents", "<unset>")))

elif what == "publish-call-count":
    print(len(publish_runs()))

elif what == "publish-call-shape":
    # Asserted by MEANING, not by the literal text. The earlier version required
    # the substring "--target ${{ github.sha }}", which pinned interpolation-into-
    # the-run-body as the only passing shape — so moving the values into `env:`
    # (the safer plumbing) turned the guard red, i.e. the guard was standing in
    # front of its own hardening. What matters is: the target is the commit that
    # triggered this run, and the tag is the one the compute step produced.
    steps = publish_steps()
    step = steps[0] if steps else {}
    cmd = publish_runs()[0] if steps else ""
    have = []
    if "--no-settle" in cmd:
        have.append("no-settle")
    if resolves_to(step, flag_value(cmd, "--target"), "${{ github.sha }}"):
        have.append("target-sha")
    if resolves_to(step, flag_value(cmd, "--beta"), "${{ steps.tag.outputs.tag }}"):
        have.append("beta-from-tag-step")
    print(",".join(have) or "-")

elif what == "publish-gated-on-freshness":
    # The publish step must be conditional on the staleness check, or a re-run of
    # an OLD main run republishes old code under a HIGHER version number — and the
    # station picks by semver, not by date, so it would move BACKWARDS in behaviour
    # while moving forwards in version.
    steps = publish_steps()
    cond = norm((steps[0] if steps else {}).get("if", ""))
    print("gated" if re.search(r"steps\.\w+\.outputs\.\w+", cond) else (cond or "-"))

elif what == "checkout-with":
    # fetch-depth alone is not the whole requirement: tags have to arrive too, and
    # a tagless checkout is rc=0 + zero tags, which bin/next-beta-tag now refuses.
    outs = []
    for s in (me.get("steps") or []):
        if not str(s.get("uses", "")).startswith("actions/checkout"):
            continue
        w = s.get("with") or {}
        outs.append("depth=%s,tags=%s" % (w.get("fetch-depth", "<unset>"),
                                         w.get("fetch-tags", "<unset>")))
    print(";".join(outs) or "-")

else:
    sys.exit("unknown query %r" % what)
PY

check "W0b the $JOB job exists in the workflow" "yes" "$(q job-present)"

# ── W1r: every job DECLARES whether it is a gate, and the declaration holds ──
# The marker is a comment, so it cannot come from the parsed document — psych
# throws comments away. It is scanned out of the raw text instead, and the two
# views are then required to AGREE on the job set: the parser is the authority on
# which jobs exist, the scan is the authority on what they declared, and any job
# one sees and the other does not means the scan's verdict covers less than it
# claims. That is reported, not shrugged at.
#
# WHY A COMMENT AND NOT A KEY: GitHub rejects an unrecognised key inside a job
# ("Unexpected value"), so a real YAML field would be a workflow that does not
# start. W4b already scans this file as text with comments included, so a comment
# is not a second-class carrier here.
cat > "$WORK/roles.py" <<'PY'
import json, re, sys

RAW, JSONF = sys.argv[1], sys.argv[2]
MARKER = "oc-job-role"
GATE, NOTGATE = "gate", "not-a-gate"

lines = open(RAW, encoding="utf-8").read().splitlines()
doc = json.load(open(JSONF, encoding="utf-8"))
parsed = set(doc.get("jobs") or {})

problems = []
def problem(msg):
    problems.append(msg)

# The jobs region: the top-level `jobs:` key up to the NEXT top-level key. Not to
# end of file — a `defaults:` or `env:` block written after `jobs:` is legal there
# and its two-space keys are not job names. (Same bounding, and the same reason,
# as bin/tests/main-red-notify-guard.sh.)
starts = [i for i, l in enumerate(lines) if l.rstrip() == "jobs:"]
if len(starts) != 1:
    problem("the file has %d top-level 'jobs:' keys (want exactly 1) — the parser "
            "takes one and this scan reads the other, so the markers in the loser "
            "would be attributed to nothing" % len(starts))
    print("PROBLEM:%s" % problems[0])
    sys.exit(0)

region = []
for i in range(starts[0] + 1, len(lines)):
    if lines[i] and not lines[i][0].isspace() and not lines[i].lstrip().startswith("#"):
        break
    region.append((i + 1, lines[i]))          # 1-based line numbers, for naming lines

JOB_KEY = re.compile(r'^  (["\']?)([_A-Za-z][A-Za-z0-9_-]*)\1:\s*(#.*)?$')
MARKER_LINE = re.compile(r'^    #\s*' + MARKER + r':\s*(\S+)\s*$')
# Job-level `continue-on-error` is a four-space key inside a job body. Scanned as
# text ONLY to name the line in a red; whether it is THERE is answered by the
# parser below, so an indentation this pattern misses cannot hide it.
COE = "continue-on-error"
COE_LINE = re.compile(r'^    ' + COE + r':\s*(.*?)\s*(?:#.*)?$')

scanned = []          # job ids in file order
declared_at = {}      # job -> the line its key is on, so a red can name it
markers = {}          # job -> list of (lineno, value)
coe_at = {}           # job -> list of (lineno, raw value) for continue-on-error
current = None
for lineno, text in region:
    if text.strip() == "" or re.match(r'^\s*#', text):
        pass
    elif re.match(r'^  [^ \t]', text):
        m = JOB_KEY.match(text)
        if not m:
            # Loud, not skipped: a job written in a shape this scan cannot read
            # would otherwise simply not be classified, and W1 would difference
            # against a set that quietly excludes it.
            problem("line %d is a two-space key in the jobs region that this scan "
                    "cannot read as a job id, so nothing here classifies the job it "
                    "declares: %s" % (lineno, text.rstrip()))
            current = None
            continue
        current = m.group(2)
        scanned.append(current)
        declared_at.setdefault(current, lineno)
        markers.setdefault(current, [])
        coe_at.setdefault(current, [])
        continue
    mc = COE_LINE.match(text)
    if mc and current is not None:
        coe_at.setdefault(current, []).append((lineno, mc.group(1)))
    if MARKER in text:
        m = MARKER_LINE.match(text)
        if current is None:
            problem("line %d mentions %s but is not inside any job this scan could "
                    "attribute it to: %s" % (lineno, MARKER, text.rstrip()))
        elif not m:
            problem("line %d looks like a %s marker for job '%s' but is not one this "
                    "scan can read (it must be exactly four spaces, '# %s: <role>', "
                    "nothing else on the line): %s"
                    % (lineno, MARKER, current, MARKER, text.rstrip()))
        else:
            markers[current].append((lineno, m.group(1)))

# The two views must describe the same jobs.
if set(scanned) != parsed:
    only_scan = ",".join(sorted(set(scanned) - parsed)) or "-"
    only_parse = ",".join(sorted(parsed - set(scanned))) or "-"
    problem("the raw-text scan and the YAML parser disagree about which jobs exist "
            "(scan-only=%s parser-only=%s) — the classification below would cover "
            "less than it claims" % (only_scan, only_parse))
dupes = [j for j in scanned if scanned.count(j) > 1]
if dupes:
    problem("job(s) declared more than once in the jobs region: %s"
            % ",".join(sorted(set(dupes))))

roles = {}
marked_at = {}        # job -> the line its marker is on
for job in sorted(parsed):
    found = markers.get(job) or []
    if len(found) == 0:
        problem("job '%s' (declared on line %s) carries no %s marker, so this guard "
                "cannot tell whether it is a gate. It is NOT assumed to be either one. "
                "Add '    # %s: %s' or '    # %s: %s' as the first line of its body."
                % (job, declared_at.get(job, "?"), MARKER, MARKER, GATE, MARKER, NOTGATE))
        continue
    if len(found) > 1:
        problem("job '%s' (declared on line %s) carries %d %s markers (lines %s) — "
                "which one is the answer is exactly the question this marker exists to "
                "settle" % (job, declared_at.get(job, "?"), len(found), MARKER,
                            ", ".join(str(n) for n, _ in found)))
        continue
    lineno, value = found[0]
    marked_at[job] = lineno
    if value not in (GATE, NOTGATE):
        problem("job '%s' declares an unrecognised role '%s' on line %d (want '%s' or "
                "'%s') — an unreadable answer is not a pass"
                % (job, value, lineno, GATE, NOTGATE))
        continue
    roles[job] = value

# ── the marker is corroborated, so relabelling is not a free pass ────────────
# A not-a-gate is a job no pull request can ever get a verdict from; a gate is one
# every pull request does. Pinning is the observable half of that, and it cuts the
# way that matters: a real gate cannot be excused from auto-beta's `needs` by
# declaring it a not-a-gate, because it would then also have to stop running on
# pull requests — which reddens here, and is a change nobody makes by accident.
# ⚠️ This is corroboration, NOT the definition. A main-pinned job is not thereby a
# non-gate; the marker is what says so, and a human review of that marker is what
# CLAUDE.md's exemption rule is about.
#
# ⚠️ THE GATE SIDE USED TO BE SPELLING-DEPENDENT, AND THAT WAS A HOLE. It asked
# only whether the ONE literal `github.ref == 'refs/heads/main'` appeared, so
# `if: github.ref_name == 'main'` and `if: startsWith(github.ref, 'refs/heads/main')`
# — two mutants a reviewer actually ran, both 35 ok / 0 failed — took a real gate
# off every pull request while this guard reported the marker as corroborated.
# Enumerating ref spellings is a losing game (there is also github.head_ref,
# github.event_name, contains(), a matrix expression, an env lookup).
#
# THE FIRST ANSWER TO THAT WAS "A GATE MAY CARRY NO JOB-LEVEL `if` AT ALL", AND
# THAT WAS TOO STRONG. It is fail-closed, which is right, but it also reddens for
# conditions that are not bypasses at all — `github.event.pull_request.draft
# != true` is the obvious one, and a rule that forbids a legitimate change
# outright gets edited away by whoever needs it next, which costs more than it
# buys. Measured: that condition scored 35 ok / 2 failed.
#
# So the rule is an ALLOWLIST, not a ban and not a spelling denylist: a gate may
# carry NO job-level `if`, or one that is EXACTLY a member of the roll-call
# below — compared whole, after normalising whitespace, the `${{ }}` wrapper and
# quoting, so no spelling gets in that a reviewer did not read. This is the same
# device as RULED_EXEMPT and WANT_IF, for the same reason: an unlisted condition
# — bypass or not — cannot pass without an edit to THIS LINE, and that edit is
# the deliberate, reviewable act. Fail-closed either way; what changes is that
# the legitimate case now has a door instead of a wall.
# ⚠️ AND WHAT THIS DOES NOT BUY, because the roll-call is only as good as the
# reading: a condition somebody ADDS here is trusted completely. `draft != true`
# does stop a gate running on draft pull requests, and if the land criteria ever
# accept a draft PR as merge-ready that entry is the hole. The mechanism forces
# the conversation; it does not have the conversation.
# Step-level `if:` is untouched — several gates use it and it cannot skip the job.
ALLOWED_GATE_IF = (
)
def canon(expr):
    """Whitespace, `${{ }}` and quoting normalised; everything else verbatim."""
    e = re.sub(r"\s+", " ", str(expr)).strip()
    m = re.fullmatch(r"\$\{\{\s*(.*?)\s*\}\}", e)
    if m:
        e = m.group(1)
    out, i = [], 0
    while i < len(e):
        if e[i] != '"':
            out.append(e[i]); i += 1; continue
        j, buf = i + 1, []
        while j < len(e) and e[j] != '"':
            if e[j] == "\\" and j + 1 < len(e):
                buf.append(e[j + 1]); j += 2; continue
            buf.append(e[j]); j += 1
        if j >= len(e):
            return e
        out.append("'" + "".join(buf).replace("'", "''") + "'")
        i = j + 1
    return "".join(out)

PIN = re.compile(r"github\.ref\s*==\s*['\"]refs/heads/main['\"]")
for job, role in sorted(roles.items()):
    cond = str(((doc.get("jobs") or {}).get(job) or {}).get("if", ""))
    pinned = bool(PIN.search(cond))
    if role == NOTGATE and not pinned:
        problem("job '%s' declares itself %s, but its `if` does not pin "
                "github.ref == 'refs/heads/main' (found: %s) — a job that can run on "
                "a pull request produces a verdict there, and this label would "
                "quietly take it out of the set auto-beta must wait for"
                % (job, NOTGATE, cond.strip() or "(no if:)"))
    if role == GATE and cond.strip() and canon(cond) not in ALLOWED_GATE_IF:
        problem("job '%s' declares itself a %s and carries a job-level `if` that is "
                "not on the reviewed roll-call (found: %s; the roll-call holds: %s). "
                "A gate must produce a verdict on every pull request, so a condition "
                "on whether it runs is where the bypass lives, whatever it is spelled "
                "with (github.ref, ref_name, startsWith(), event_name, head_ref, an "
                "env lookup, …) — which is why this is an allowlist and not a list of "
                "bad spellings. A gate that skips on pull requests is still a gate. "
                "If the condition is genuinely not a bypass (skipping DRAFT pull "
                "requests is the case this door was opened for), add it verbatim to "
                "ALLOWED_GATE_IF in this guard in the SAME commit — that edit is the "
                "review. If this job is genuinely not a check, mark it %s — and then "
                "W1x will ask for the owner ruling that costs."
                % (job, GATE, cond.strip(),
                   ", ".join("`%s`" % c for c in ALLOWED_GATE_IF) or "(nothing yet)",
                   NOTGATE))

# ── no job may excuse itself from failing (continue-on-error) ────────────────
# ⚠️ NOTHING IN THIS FILE READ THIS FIELD, and one line walked through the gap:
# a reviewer added `continue-on-error: true` to a gate and scored 35 ok / 0 failed.
# GitHub's own description of the job-level field is "prevents a workflow run from
# failing when a job fails", so the job reports success outward — which satisfies
# auto-beta's `needs` (it publishes off a red trunk) AND makes notify-main-red's
# `failure()` false (nobody is told). Both defences fail at once, silently, from
# one line that reads like a kindness to a flaky test.
# It is banned on EVERY job, not only gates: auto-beta's own failure is the
# "merged and reachable drifting apart" silence notify-main-red exists to break,
# and notify's failure is the notification not arriving.
# The PARSER answers whether it is there (an indentation the text scan misses
# cannot hide it); the text scan only supplies the line number.
# An explicit `continue-on-error: false` is allowed — it is the default, stated.
for job in sorted(parsed):
    spec = (doc.get("jobs") or {}).get(job) or {}
    if COE not in spec:
        continue
    val = spec[COE]
    if val is False:
        continue
    where = ", ".join(str(n) for n, _ in coe_at.get(job, [])) or "?"
    problem("job '%s' carries a job-level `%s: %s` (line %s) — that makes the job "
            "report SUCCESS outward when it fails, which (a) satisfies auto-beta's "
            "`needs` so a red trunk still publishes a real prerelease, and (b) makes "
            "notify-main-red's failure() false so nobody is told. Both defences go "
            "down together and neither of them reddens. Remove it; if a specific "
            "step is allowed to fail, put the field on THAT step (step-level "
            "continue-on-error cannot make the job lie about its own conclusion)."
            % (job, COE, val, where))

# ── the exemption list is a DELIBERATE enumeration, and that IS the point ────
# Everything above stops a job from being MISclassified. It does not stop a job
# from being HONESTLY classified as a non-gate and thereby leaving auto-beta's
# required set — declare a new job not-a-gate, pin it to main, and both rules
# above are satisfied while the trunk quietly stops waiting for it.
#
# CLAUDE.md answers that with a ruling, not a rule of thumb: the exemption list is
# a hardcoded roll-call, and a third member needs its OWN owner ruling. Until now
# that sentence was prose with nothing behind it. This is the mechanism. Yes, it is
# an enumeration, and yes, this file argues against those everywhere else — the
# difference is what an enumeration COSTS. W1's list would go stale silently and
# be wrong for free; this one cannot go stale in silence, because the only way to
# add a member is to edit this line, which is exactly the deliberate act the ruling
# is asking for. Shaped as "this query returns nothing", so the red names the job
# and its lines rather than a count.
RULED_EXEMPT = ("notify-main-red", "auto-beta")
for job in sorted(j for j, r in roles.items() if r == NOTGATE):
    if job in RULED_EXEMPT:
        continue
    print("EXEMPTFAIL:job '%s' (declared on line %s, marker on line %s) declares itself "
          "%s, which takes it out of the set auto-beta waits for. The jobs ruled exempt "
          "are exactly: %s. A third one needs its OWN owner ruling (CLAUDE.md land "
          "criteria) — the analogy to the existing two is explicitly not enough. If you "
          "have that ruling, add the name to RULED_EXEMPT in this guard and to the "
          "exemption list in CLAUDE.md in the SAME commit."
          % (job, declared_at.get(job, "?"), marked_at.get(job, "?"), NOTGATE,
             ", ".join(RULED_EXEMPT)))

for p in problems:
    print("PROBLEM:%s" % p)
if problems:
    sys.exit(0)

gates = sorted(j for j, r in roles.items() if r == GATE)
declared = set((((doc.get("jobs") or {}).get("auto-beta") or {}).get("needs")) or [])
print("GATES:%s" % (",".join(gates) or "-"))
print("NEEDSDIFF:missing=%s extra=%s" % (
    ",".join(sorted(set(gates) - declared)) or "-",
    ",".join(sorted(declared - set(gates))) or "-"))
PY

ROLES_OUT="$(python3 "$WORK/roles.py" "$WF" "$JSON" 2>&1)"
ROLES_RC=$?
ROLE_PROBLEMS="$(printf '%s\n' "$ROLES_OUT" | sed -n 's/^PROBLEM://p')"
if [[ "$ROLES_RC" != "0" ]]; then
  fatal "W1r the job-role scan itself failed to run, so nothing below classifies anything: $(printf '%s' "$ROLES_OUT" | tr '\n' ' ')"
elif [[ -n "$ROLE_PROBLEMS" ]]; then
  bad "W1r every job must DECLARE whether it is a gate, readably — these could not be classified, and are NOT being defaulted into a bucket:$(printf '%s\n' "$ROLE_PROBLEMS" | sed 's/^/ | /' | tr '\n' ' ')"
else
  ok "W1r every job carries exactly one readable $ROLE_MARKER marker, the parser and the text scan agree on the job set, every marker is corroborated (a not-a-gate pins refs/heads/main; a gate carries either no job-level if or one on the reviewed roll-call), and no job excuses itself with continue-on-error ($(printf '%s\n' "$ROLES_OUT" | sed -n 's/^GATES:/gates: /p'))"
fi

# ── W1x: the not-a-gate set is exactly the two jobs that were ruled exempt ──
# Deliberately a roll-call. See the note beside RULED_EXEMPT above for why this one
# enumeration earns its keep where W1's would not.
EXEMPT_HITS="$(printf '%s\n' "$ROLES_OUT" | sed -n 's/^EXEMPTFAIL://p')"
EXEMPT_N="$(printf '%s\n' "$EXEMPT_HITS" | grep -c . || true)"
if [[ "$EXEMPT_N" == "0" ]]; then
  ok "W1x the jobs declaring themselves not-a-gate are exactly the ones ruled exempt (a third would need its own ruling)"
else
  bad "W1x $EXEMPT_N job(s) declare themselves not-a-gate without a ruling saying they may:$(printf '%s\n' "$EXEMPT_HITS" | sed 's/^/ | /' | tr '\n' ' ')"
fi

# ── W1: needs covers every GATE job, and nothing else ───────────────────────
# Only reachable when the classification above stood up; otherwise the gate set is
# not known and a verdict here would be invented.
if [[ -n "$ROLE_PROBLEMS" ]]; then
  bad "W1 $JOB needs EXACTLY the declared gate jobs (both directions) — NOT CHECKED, because the job roles could not be established (see W1r). This is not a pass."
else
  check "W1 $JOB needs EXACTLY the declared gate jobs (both directions)" \
    "missing=- extra=-" "$(printf '%s\n' "$ROLES_OUT" | sed -n 's/^NEEDSDIFF://p')"
fi

# ── W2: the trigger condition is EXACTLY the canonical one ─────────────────
# Not "both halves are mentioned somewhere in it" — see the note in the `if-raw`
# query for the mutant that walked through that. One authority, exact comparison,
# which is this repo's existing style for a condition that must not drift
# (bin/tests/main-red-notify-guard.sh's WANT_IF does the same for notify).
WANT_IF="github.event_name == 'push' && github.ref == 'refs/heads/main'"
check "W2 $JOB's if is EXACTLY \`$WANT_IF\` (a bolted-on \`|| …\` would publish from a pull request)" \
  "$WANT_IF" "$(q if-raw)"

# ── W7: the workflow's TRIGGERS are exactly the canonical ones ──────────────
# The one assertion that reads `on:`. Everything else in this file reasons about
# what the jobs DO once they are scheduled; this is what decides whether they are
# scheduled at all. A filter here (`paths-ignore: ['**']` is the cheap one) makes
# every gate skip on every pull request with no job touched and no assertion
# above reddening — see the `on-raw` query for the measurement.
WANT_ON='{"pull_request":null,"push":{"branches":["main"]}}'
check "W7 the workflow's \`on:\` is EXACTLY $WANT_ON (a path/branch filter here makes every gate skip, and no other assertion reads it)" \
  "$WANT_ON" "$(q on-raw)"

# ── W3: the elevated permission is scoped to this job ──────────────────────
check "W3 contents:write is held by $JOB ALONE and the workflow default stays read" \
  "jobs=$JOB default=read" "$(q write-holders)"

# ── W4: GA is never automated ──────────────────────────────────────────────
# Scanned as TEXT over every workflow file, comments included, rather than
# through the parser: a mention in a comment is one edit away from being a step.
#
# ⚠️ TWO RULES, because the first one alone was NOT what it looked like. Banning
# the subcommand's NAME is what this started as, and a reviewer walked straight
# past it: `gh release edit '<tag>' --prerelease=false --latest` automates GA
# perfectly without ever typing that word — 19 ok, rc=0. So the second rule bans
# the flags that MOVE THE LATEST POINTER, which is the thing actually being
# protected. Neither rule is a claim that no workflow can ever reach GA by some
# other route (a script the workflow calls, `gh api`, a curl to the REST API);
# what they buy is that the two shapes anyone would actually reach for both redden.
#
# ⚠️ THE SCOPE USED TO BE `.github/workflows` AND THAT WAS A HOLE — a third shape
# nobody had listed. A reviewer added `.github/actions/finalise/action.yml` whose
# composite step runs `bin/release promote "$OC_TAG"`, referenced it from auto-beta
# as `uses: ./.github/actions/finalise`, and scored 35 ok / 0 failed: a composite
# action is not a workflow file, so all three rules below looked straight past it —
# while the step ran inside the one job holding `contents: write`. The scope is
# therefore the WHOLE of `.github/`, and W4d below refuses local actions outright.
GA_SCAN_DIR="$ROOT/.github"
GA_HITS="$(grep -rilE 'promote' "$GA_SCAN_DIR" 2>/dev/null || true)"
if [[ -z "$GA_HITS" ]]; then
  ok "W4 nothing under .github/ names the beta→final flip subcommand (workflows AND any composite action)"
else
  bad "W4 a file under .github/ names the beta→final flip subcommand — GA must never be automated: $(printf '%s ' $GA_HITS)"
fi
# `gh release edit` is legitimate for nothing this workflow needs to do, so the
# flags are matched rather than the command: --prerelease=false and --latest each
# promote a beta to GA on their own, and --draft=false publishes a hidden one.
GA_FLAG_RX='--prerelease=false|--latest([[:space:]]|$)|--draft=false'
GA_FLAG_HITS="$(grep -rlE -- "$GA_FLAG_RX" "$GA_SCAN_DIR" 2>/dev/null || true)"
if [[ -z "$GA_FLAG_HITS" ]]; then
  ok "W4b nothing under .github/ carries a flag that moves the Latest pointer (--prerelease=false / --latest / --draft=false)"
else
  bad "W4b a file under .github/ carries a GA-promoting flag (--prerelease=false / --latest / --draft=false) — that flips a beta to GA without ever naming the subcommand: $(printf '%s ' $GA_FLAG_HITS)"
fi

# ── W4pc: the two greps above are ALIVE ─────────────────────────────────────
# W4 and W4b both report "clean" as an EMPTY result, and an empty result is what a
# grep that matches nothing looks like — a mistyped pattern, a directory that moved,
# a `-r` that stopped recursing. So the same two invocations are pointed at a
# fixture that definitely contains both shapes, and are required to FIND them.
# Without this, "no hits under .github/" is a claim with no evidence behind it.
GA_PC_DIR="$WORK/ga-pc/nested"
mkdir -p "$GA_PC_DIR"
printf '%s\n' 'run: bash bin/release promote "$OC_TAG"' > "$GA_PC_DIR/action.yml"
printf '%s\n' 'run: gh release edit "$T" --prerelease=false --latest' > "$GA_PC_DIR/other.yml"
GA_PC_A="$(grep -rilE 'promote' "$WORK/ga-pc" 2>/dev/null | grep -c . || true)"
GA_PC_B="$(grep -rlE -- "$GA_FLAG_RX" "$WORK/ga-pc" 2>/dev/null | grep -c . || true)"
check "W4pc the W4/W4b scans find a planted GA automation in a nested dir (so an empty result over .github/ means something)" \
  "subcommand=1 flags=1" "subcommand=$GA_PC_A flags=$GA_PC_B"

# ── W4d: nothing under .github/ REFERENCES a local action or workflow ────────
# The composite-action bypass works because a local action is a SECOND home for
# steps: its own file, its own `run:` bodies, and — being reached through `uses:`
# — no obligation to look like anything this guard recognises. W4 and W4b now
# grep it, but "we grep it too" is a weaker promise than "it cannot be reached",
# because the next shape in such a file (a `gh api` PATCH, a node action, a
# script it shells out to) is invisible to a grep again.
#
# ⚠️ THIS USED TO BAN THE DIRECTORY `.github/actions/`, AND THAT WAS THE WRONG
# TARGET — it paid the whole price and bought almost nothing. A local action does
# not have to live under `.github/`: `action.yml` at the repo root referenced as
# `uses: ./`, or `tools/finalise/action.yml` as `uses: ./tools/finalise`, is an
# ordinary composite action that GitHub runs happily, that W4/W4b (scoped to
# `.github/`) never read, and that the directory ban never saw. Measured: that
# exact shape scored 37 ok / 0 failed while a step running `bin/release promote`
# sat inside auto-beta's `contents: write` job.
#
# So what is pinned is the REFERENCE, not a location: no file under `.github/`
# may point a `uses:` at a path inside this repo (`./…`, `../…`). That is the one
# thing every local action, composite or not, wherever it is kept, has to do to
# be reached — and it is also how a local REUSABLE WORKFLOW is called, which is a
# second way to add jobs W1's set difference cannot see. Adding one is a
# reasonable thing to want; it has to redden here and be decided, not arrive as a
# refactor.
# ⚠️ WHAT THIS STILL DOES NOT CLOSE, stated so nobody reads it wider: a `run:`
# step in ci.yml can call any script in the repo, and that script can do anything
# — GA included. W4/W4b/W4d cover the shapes that add a NEW HOME FOR STEPS; they
# are not a proof that no workflow can reach GA.
USES_LOCAL_RX='uses:[[:space:]]*["'"'"']?\.\.?/'
USES_LOCAL_HITS="$(grep -rnE -- "$USES_LOCAL_RX" "$GA_SCAN_DIR" 2>/dev/null || true)"
if [[ -z "$USES_LOCAL_HITS" ]]; then
  ok "W4d nothing under .github/ references a local action or reusable workflow (\`uses: ./…\`) — steps live only in the files W1/W2/W3 actually read, wherever a local action might be kept"
else
  bad "W4d a file under .github/ references a local action or reusable workflow with \`uses: ./…\` — that is a second home for steps, running inside auto-beta's \`contents: write\` job, whose contents W4/W4b can only see if it happens to sit under .github/ AND happens to use a shape they grep for. Adding one is a decision to make deliberately (and this guard has to be reworked to read the target) — not a refactor that silently widens what auto-beta may run: $(printf '%s\n' "$USES_LOCAL_HITS" | tr '\n' ' ')"
fi

# ── W4dpc: and W4d's grep is ALIVE ──────────────────────────────────────────
# W4d reports "clean" as an EMPTY result, exactly like W4/W4b — and exactly like a
# pattern that stopped matching. Same treatment: point the SAME invocation at a
# fixture that definitely carries the shape, in a nested directory, and require it
# to find both spellings a local reference actually takes.
USES_PC_DIR="$WORK/uses-pc/nested"
mkdir -p "$USES_PC_DIR"
printf '%s\n' '      - uses: ./tools/finalise' > "$USES_PC_DIR/wf.yml"
printf '%s\n' "      - uses: './.github/actions/finalise'" \
              '      - uses: ./.github/workflows/reusable.yml' > "$USES_PC_DIR/wf2.yml"
USES_PC_N="$(grep -rlE -- "$USES_LOCAL_RX" "$WORK/uses-pc" 2>/dev/null | grep -c . || true)"
check "W4dpc the W4d scan finds planted local \`uses: ./…\` references in a nested dir (so an empty result over .github/ means something)" \
  "2" "$USES_PC_N"

# ── W4c: the job universe this guard reasons about is really the whole one ───
# W1's difference is computed over the jobs in ci.yml, so "the trunk is green"
# means "every job IN THIS FILE passed". A SECOND workflow file would carry checks
# that auto-beta's `needs` cannot even name (GitHub has no cross-file `needs`), and
# a reviewer proved the gap by dropping in a second workflow whose job just
# `exit 1`s — this guard stayed green. Rather than pretend to cover it, pin the
# precondition that makes W1's scope equal the real scope: ci.yml is the only
# workflow. Adding a second one reddens here and forces the decision to be made
# deliberately (either fold the job into ci.yml, or rework this guard) instead of
# silently widening what "green" is allowed to leave out.
WF_FILES="$(cd "$WF_DIR" && ls -1 2>/dev/null | LC_ALL=C sort | tr '\n' ' ' | sed 's/ $//')"
check "W4c ci.yml is the ONLY workflow file (W1's job universe is one file wide)" \
  "ci.yml" "$WF_FILES"

# ── W5: the publish invocation's shape ─────────────────────────────────────
check "W5 exactly one step invokes bin/release publish" "1" "$(q publish-call-count)"
check "W5 that call carries --no-settle, --target = the triggering SHA, --beta = the computed tag" \
  "no-settle,target-sha,beta-from-tag-step" "$(q publish-call-shape)"
check "W5b the publish step is gated on the staleness check (a re-run of an old main run must not republish)" \
  "gated" "$(q publish-gated-on-freshness)"

# ── W6: the checkout is deep AND brings tags ───────────────────────────────
check "W6 $JOB checks out with fetch-depth: 0 AND fetch-tags: true (worktree add <sha> + the tag union need both)" \
  "depth=0,tags=True" "$(q checkout-with)"

# ═══════════════════════════════════════════════════════════════════════════
# N — the version rule in bin/next-beta-tag, driven with fabricated tag lists.
# ═══════════════════════════════════════════════════════════════════════════
echo "── N: the next-beta-tag version rule"

# nbt — run ONE function out of the real bin/next-beta-tag with TAGS on stdin, in
# a subshell so its `set -e` / die-with-exit lands on the case and not here.
# stdout and stderr are captured together on purpose: the chosen BASE is reported
# on stderr, and it is the only observable that can tell semver ordering apart
# from string ordering (see N1).
nbt() { # nbt <tags-newline-separated> <fn> [args...]
  local tags="$1"; shift
  OUT="$(printf '%s\n' "$tags" | bash -c '
    set -uo pipefail
    source "$1" || exit $?
    fn="$2"; shift 2
    "$fn" "$@"
  ' _ "$NBT" "$@" 2>&1)"
  RC=$?
}
nbt_next() { # nbt_next <tags> — sets RC, TAG (stdout only) and BASE
  local tags="$1"
  TAG="$(printf '%s\n' "$tags" | bash -c '
    set -uo pipefail
    source "$1" || exit $?
    nbt_next_beta_tag
  ' _ "$NBT" 2>"$WORK/nbt.err")"
  RC=$?
  BASE="$(sed -n 's/^\[next-beta-tag\] base: \([^ ]*\).*/\1/p' "$WORK/nbt.err")"
}

# N0 — the shape this repo actually uses, and the reason the rule is patch+1
# rather than beta+1: the base is itself a -beta.1, and `-beta.N` sorts BELOW the
# same X.Y.Z under semver, so anything that did not move the patch could not come
# out greater than its own base.
nbt_next "v0.5.77-beta.1
v0.5.78-beta.1
v0.5.9
py-final"
check "N0 the repo's own shape: base is the greatest beta, next bumps the patch" "0" "$RC"
check "N0 …base chosen"      "v0.5.78-beta.1" "$BASE"
check "N0 …next tag"         "v0.5.79-beta.1" "$TAG"

# N1 — THE case that separates semver ordering from string ordering. As strings,
# "v1.0.0-beta.9" > "v1.0.0-beta.10"; under semver, numeric prerelease identifiers
# compare NUMERICALLY, so beta.10 wins. The computed tag is v1.0.1-beta.1 either
# way, which is exactly why this asserts the BASE: a `sort -V`-free string
# comparison would pass an assertion on the tag alone and be wrong.
nbt_next "v1.0.0-beta.9
v1.0.0-beta.10"
check "N1 beta.10 outranks beta.9 (numeric, not lexical, prerelease compare)" "v1.0.0-beta.10" "$BASE"
check "N1 …and the next tag is the patch bump off it" "v1.0.1-beta.1" "$TAG"

# N2 — a candidate set with NO prerelease at all. A release outranks any
# prerelease of the same X.Y.Z, and the patch numbers must compare numerically:
# as strings "v0.5.9" > "v0.5.78".
nbt_next "v0.5.7
v0.5.8
v0.5.9
v0.5.78"
check "N2 releases only: greatest is by NUMERIC patch, not string order" "v0.5.78" "$BASE"
check "N2 …and the next tag is a beta above it" "v0.5.79-beta.1" "$TAG"

# N3 — an EMPTY candidate set is a hard failure, not "start from v0.0.1-beta.1".
# The realistic cause is a query that broke, and the friendly fallback would
# republish a version from the beginning of history over a repo with a hundred
# releases. Non-semver tags are ignored, so a list of only those is empty too.
nbt "py-final
not-a-version" nbt_next_beta_tag
if [[ "$RC" == "0" ]]; then
  bad "N3 an empty candidate set must FAIL (it exited 0 and printed: $(printf '%s' "$OUT" | tr '\n' '|'))"
else
  case "$OUT" in
    *"no semver tag"*) ok "N3 an empty candidate set FAILS and says why (rc=$RC)" ;;
    *) bad "N3 an empty candidate set fails but does not name the reason (out: $(printf '%s' "$OUT" | tr '\n' '|'))" ;;
  esac
fi

# N4 — the collision refusal, tested directly. See bin/next-beta-tag's honest note
# on reachability: the rule cannot currently produce a taken name, so this binds
# the FUNCTION rather than pretending to reach it through the computation.
nbt "v0.5.79-beta.1
v0.5.78-beta.1" nbt_assert_tag_absent v0.5.79-beta.1
if [[ "$RC" == "0" ]]; then
  bad "N4 a computed tag that already exists must be REFUSED (it exited 0)"
else
  case "$OUT" in
    *"ALREADY EXISTS"*) ok "N4 a tag that already exists is REFUSED, not incremented or overwritten (rc=$RC)" ;;
    *) bad "N4 refused but did not say the tag already exists (out: $(printf '%s' "$OUT" | tr '\n' '|'))" ;;
  esac
fi
# …and the sentinel in the other direction, so N4 is not passing on any error.
nbt "v0.5.78-beta.1
v0.5.77-beta.1" nbt_assert_tag_absent v0.5.79-beta.1
check "N4b sentinel — an unused tag passes the collision check" "0" "$RC"

# ═══════════════════════════════════════════════════════════════════════════
# S — WHO ASSEMBLED THE CANDIDATE SET. Everything above feeds a fabricated tag
# list straight into nbt_compute, so none of it asks the question that matters
# most: is the set complete?
#
# WHY THIS BLOCK EXISTS (it was MISSING, and three mutants walked through the
# gap): an independent review turned `gh release list … || nbt_die` into
# `|| true`, did the same to `git tag --list`, and then deleted the GitHub source
# altogether — all three passed the suite green. The two-source union and the
# "either source failing is fatal" rule are the properties bin/next-beta-tag's
# header spends a page arguing for, and nothing was holding them. Worse, when the
# set is truncated BOTH defences fail at once and silently: the base comes from
# half the world, and nbt_assert_tag_absent then checks the computed tag against
# that same half.
#
# These cases drive nbt_collect_candidate_tags for real — a throwaway git repo via
# OC_RELEASE_SRC, and a `gh` PATH shim. No network, no real releases.
echo "── S: the candidate set is assembled from BOTH sources, or it fails"

SWORK="$WORK/src-tags"; SSHIM="$WORK/src-shim"
mkdir -p "$SWORK" "$SSHIM"
(
  cd "$SWORK"
  git init -q .
  git config user.email guard@example.invalid
  git config user.name guard
  git commit -q --allow-empty -m fixture
) >/dev/null 2>&1 || fatal "S setup: could not build the fixture git repo"
git -C "$SWORK" tag v0.1.0 >/dev/null 2>&1 || fatal "S setup: could not tag the fixture repo"

# The gh shim answers from env, so each case controls rc and payload independently.
# It NEVER reaches the network; `gh` is not even consulted for anything else.
cat > "$SSHIM/gh" <<'SH'
#!/usr/bin/env bash
printf '%s' "${FAKE_GH_OUT:-}"
[[ -n "${FAKE_GH_OUT:-}" ]] && printf '\n'
exit "${FAKE_GH_RC:-0}"
SH
chmod +x "$SSHIM/gh"

# collect — run the REAL nbt_collect_candidate_tags (optionally piped into the
# real nbt_next_beta_tag) against the fixture repo and the shimmed gh.
collect() { # collect <src-dir> [--next]
  local src="$1" mode="${2:-}"
  OUT="$(PATH="$SSHIM:$PATH" OC_RELEASE_SRC="$src" OC_RELEASE_GH_REPO="guard/fixture" \
    FAKE_GH_OUT="${FAKE_GH_OUT:-}" FAKE_GH_RC="${FAKE_GH_RC:-0}" \
    bash -c '
      set -uo pipefail
      source "$1" || exit $?
      if [[ "${2:-}" == "--next" ]]; then
        nbt_collect_candidate_tags | nbt_next_beta_tag
      else
        nbt_collect_candidate_tags
      fi
    ' _ "$NBT" "$mode" 2>&1)"
  RC=$?
}

# S0 — the sentinel: both sources answer, and the result is really the UNION.
# Without this the failure cases below could all be passing for the wrong reason.
FAKE_GH_RC=0 FAKE_GH_OUT="v0.2.0" collect "$SWORK"
check "S0 both sources answer → exits 0" "0" "$RC"
check "S0 …and the candidate set is the UNION of both (git v0.1.0 + gh v0.2.0)" \
  "v0.1.0
v0.2.0" "$(printf '%s\n' "$OUT" | grep -v '^$')"

# S1 — a tag that exists ONLY on the GitHub side must move the base. This is the
# case that kills "delete the gh source entirely": with git alone the base would
# be v0.1.0, so the answer names which sources were really consulted.
FAKE_GH_RC=0 FAKE_GH_OUT="v9.9.0" collect "$SWORK" --next
check "S1 a release tag that exists ONLY on the gh side decides the base" "0" "$RC"
case "$OUT" in
  *"base: v9.9.0"*"v9.9.1-beta.1"*) ok "S1 …base=v9.9.0 from the gh-only tag, next=v9.9.1-beta.1" ;;
  *) bad "S1 …base=v9.9.0 from the gh-only tag, next=v9.9.1-beta.1 (out: $(printf '%s' "$OUT" | tr '\n' '|'))" ;;
esac

# S1b — and the mirror, so S1 is not just "gh wins always": a git-only tag that is
# greater must decide it too.
git -C "$SWORK" tag v0.3.0 >/dev/null 2>&1 || fatal "S setup: could not add the second fixture tag"
FAKE_GH_RC=0 FAKE_GH_OUT="v0.2.0" collect "$SWORK" --next
case "$OUT" in
  *"base: v0.3.0"*) ok "S1b …and a git-only tag that is greater decides it as well" ;;
  *) bad "S1b …and a git-only tag that is greater decides it as well (out: $(printf '%s' "$OUT" | tr '\n' '|'))" ;;
esac

# S2 — gh exits non-zero. Fatal, and the message has to name gh, or the operator
# goes hunting the wrong source.
#
# The payload is deliberately NON-EMPTY (and well-formed), which is the only way
# this case reaches the rc rule ON ITS OWN: with an empty payload the very next
# rule ("returned rc=0 but ZERO releases") catches it too, and the two become
# indistinguishable from outside. Output from a command that failed is not an
# answer. (Same trick, same reason, as release-guard.sh's S1b.)
FAKE_GH_RC=1 FAKE_GH_OUT="v0.2.0" collect "$SWORK"
if [[ "$RC" == "0" ]]; then
  bad "S2 gh exiting non-zero must be FATAL (it exited 0: $(printf '%s' "$OUT" | tr '\n' '|'))"
else
  case "$OUT" in
    *"gh release list failed"*) ok "S2 gh exiting non-zero is FATAL and names gh (rc=$RC)" ;;
    *) bad "S2 gh exiting non-zero is fatal but does not name gh (out: $(printf '%s' "$OUT" | tr '\n' '|'))" ;;
  esac
fi

# S3 — THE SILENT ONE. gh exits ZERO with nothing to say. Falling back to the git
# tags alone is rc=0 and a wrong number nobody questions, so this must be fatal.
FAKE_GH_RC=0 FAKE_GH_OUT="" collect "$SWORK"
if [[ "$RC" == "0" ]]; then
  bad "S3 gh answering rc=0 with ZERO releases must be FATAL, not a silent single-source fallback (it exited 0: $(printf '%s' "$OUT" | tr '\n' '|'))"
else
  case "$OUT" in
    *"ZERO releases"*) ok "S3 gh answering rc=0 with ZERO releases is FATAL (no silent single-source fallback) (rc=$RC)" ;;
    *) bad "S3 gh rc=0 + empty is fatal but does not say why (out: $(printf '%s' "$OUT" | tr '\n' '|'))" ;;
  esac
fi

# S4 — the same silence on the git side, which is the shape a checkout without
# tags produces on a runner: rc=0, zero tags, and the whole version computed off
# GitHub releases alone.
SNOTAGS="$WORK/src-notags"; mkdir -p "$SNOTAGS"
(
  cd "$SNOTAGS"
  git init -q .
  git config user.email guard@example.invalid
  git config user.name guard
  git commit -q --allow-empty -m fixture
) >/dev/null 2>&1 || fatal "S setup: could not build the tagless fixture repo"
FAKE_GH_RC=0 FAKE_GH_OUT="v0.2.0" collect "$SNOTAGS"
if [[ "$RC" == "0" ]]; then
  bad "S4 git answering rc=0 with ZERO tags must be FATAL (a checkout without tags looks exactly like this) (it exited 0: $(printf '%s' "$OUT" | tr '\n' '|'))"
else
  case "$OUT" in
    *"ZERO tags"*) ok "S4 git answering rc=0 with ZERO tags is FATAL, and points at fetch-tags (rc=$RC)" ;;
    *) bad "S4 git rc=0 + no tags is fatal but does not say why (out: $(printf '%s' "$OUT" | tr '\n' '|'))" ;;
  esac
fi

# S5 — the git source erroring outright (here: not a repository at all).
# ⚠️ Unlike S2 this case CANNOT isolate its rule: a failing `git tag --list` prints
# nothing, so the empty-answer rule (S4's) would catch it as well. Both fail under
# messages naming the git source, so from outside the two are indistinguishable —
# recorded rather than papered over, because there is no way to make git exit
# non-zero AND emit a tag list.
FAKE_GH_RC=0 FAKE_GH_OUT="v0.2.0" collect "$WORK/definitely-not-a-repo"
if [[ "$RC" == "0" ]]; then
  bad "S5 git exiting non-zero must be FATAL (it exited 0)"
else
  case "$OUT" in
    *"could not list git tags"*) ok "S5 git exiting non-zero is FATAL and names the git source (rc=$RC)" ;;
    *) bad "S5 git exiting non-zero is fatal but does not name the git source (out: $(printf '%s' "$OUT" | tr '\n' '|'))" ;;
  esac
fi

echo "auto-beta guard: $PASS ok, $FAIL failed"
[[ "$FAIL" == "0" ]] || exit 1
exit 0
