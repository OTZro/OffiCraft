#!/usr/bin/env bash
# Every cloud cell that CALLS ITSELF A GATE must reach its checks through
# bin/run-checks.sh — never through a bare `make`.
#
# ── WHY THIS EXISTS (T-4d88) ─────────────────────────────────────────────────
# T-4d88 put back a protection the deleted macOS subset script used to carry: a
# zero exit says "nothing failed", not "something ran", so every check target
# prints its own `[oc-check-done] <target>` line and bin/run-checks.sh requires
# the marker of every target IT WAS ASKED FOR.
#
# That protection had one hole, and the ticket that built it said so itself:
# NOTHING FORCED A CELL TO USE THE WRAPPER. Change one cell's step back to
# `run: make lint-ts test-frontend-unit` and the markers stop being checked for
# that cell — with no red, no diff in the log's shape, and a green check mark on
# the PR. A protection that can be removed in silence is not a protection; it is
# a habit. This guard is what makes the removal loud.
#
# ── WHAT IT ASSERTS ──────────────────────────────────────────────────────────
#  W1  In a job that declares `# oc-job-role: gate`, no `run:` step may invoke
#      `make` in command position. The wrapper is the only door.
#  W2  At least one gate job actually routes through bin/run-checks.sh. Without
#      this, a file in which the scan matched NOTHING AT ALL — a renamed job
#      key, an indentation change, a parser that quietly stopped seeing steps —
#      would report the same serene green as a file that is genuinely clean.
#  W3  Positive/negative control on the scanner itself, against two synthetic
#      workflows written to a throwaway dir: a gate cell running bare `make`
#      must be flagged EXACTLY ONCE and by name, and a gate cell running
#      `bash bin/run-checks.sh` must not be flagged at all. A scanner that has
#      stopped scanning fails W3 rather than blessing the tree.
#
# ── WHAT IT DELIBERATELY DOES *NOT* ASSERT ───────────────────────────────────
#  1. 🔴 It does NOT enumerate which checks exist, which cell owns which check,
#     or whether the cells collectively cover the Makefile. That consistency
#     assertion is deliberately absent from this repo (owner ruling), and the
#     enumeration it would need is exactly the duplication T-4d88 deleted. This
#     guard only answers: DID THIS CELL COME THROUGH THE DOOR.
#  2. A gate cell that runs no `make` at all is not required to use the wrapper.
#     macos-e2e is such a cell on purpose — it stands a whole station up through
#     e2e_test/run_all.sh and carries its own did-it-run assertion
#     (e2e_test/assert-specs-ran.sh). Requiring the wrapper of every gate would
#     be requiring every gate to be a Makefile target, which is a different (and
#     unmade) decision.
#  3. It reads the workflow, not GitHub. A step could still assemble a `make`
#     call at run time (`bash -c "$CMD"`, a variable, a script that shells out).
#     Same static-matching limit as the other guards here; the wrapper's own
#     contract tests live in bin/tests/run-checks-guard.sh.
#  4. Swapping `bash bin/run-checks.sh lint-ts` for a direct `npx vitest …` is
#     invisible here — that is the "one definition" property, guarded by the
#     Makefile being the only place a check is written down, not by this file.
set -uo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT="$(cd "$HERE/../.." && pwd)"
CI_YML="$ROOT/.github/workflows/ci.yml"

PASS=0; FAIL=0
ok()  { PASS=$((PASS+1)); printf '  ok   — %s\n' "$1"; }
bad() { FAIL=$((FAIL+1)); printf '  FAIL — %s\n' "$1"; }

WORK="$(mktemp -d -t oc-run-checks-entry.XXXXXX)"
trap 'rm -rf "$WORK"' EXIT

# ── the parser ───────────────────────────────────────────────────────────────
# ruby+psych, for the same reason bin/tests/auto-beta-guard.sh uses it: the
# hosted macOS runner has no PyYAML, and a line-oriented grep cannot see a
# `run:` whose script is a block scalar or whose value sits on the next line.
RUBY="$(command -v ruby 2>/dev/null || true)"
if [[ -z "$RUBY" ]]; then
  for cand in /usr/bin/ruby /opt/homebrew/bin/ruby /usr/local/bin/ruby; do
    [[ -x "$cand" ]] && { RUBY="$cand"; break; }
  done
fi
if [[ -z "$RUBY" ]]; then
  echo "FATAL: no ruby found, so .github/workflows/ci.yml cannot be PARSED." >&2
  echo "  macOS ships one at /usr/bin/ruby; Homebrew: brew install ruby." >&2
  echo "  A guard that cannot parse must be loud, not silently green." >&2
  exit 2
fi

SCANNER="$WORK/scan.rb"
cat >"$SCANNER" <<'RUBY_EOF'
require 'yaml'

path = ARGV[0]
# Read as UTF-8 EXPLICITLY. ci.yml's comments contain non-ASCII, and ruby picks
# its default external encoding from the locale — under the C/POSIX locale a
# hosted runner hands you US-ASCII and the very first regex match dies with
# "invalid byte sequence". That failure is loud here (the guard reports it as a
# parse error), but it is a failure about the environment, not about the tree.
text = File.read(path, mode: 'r:UTF-8')

# Which jobs call themselves a gate? The role marker is a COMMENT, so it does
# not survive the parse — read it off the raw lines and bind it to the job key
# it sits under (the nearest preceding two-space-indented mapping key).
gates = []
current = nil
text.each_line.with_index do |line, i|
  if (m = line.match(/\A  ([A-Za-z0-9_.-]+):\s*(#.*)?\z/))
    current = m[1]
  elsif (m = line.match(/\A\s*#\s*oc-job-role:\s*(\S+)\s*\z/))
    gates << current if m[1] == 'gate' && current
  end
end
gates.uniq!

doc = begin
  YAML.safe_load(text, aliases: false)
rescue => e
  warn "PARSE-ERROR #{e.class}: #{e.message}"
  exit 3
end

jobs = (doc && doc['jobs']) || {}

# Command-position tokenisation. A substring grep for "make" would match prose
# in a comment, the word "makefile" in a step name, and this guard's own text —
# so split each run: script into command segments and look only at the head of
# each one.
SEPARATORS = /(?:\|\||&&|[;|&])/
LEADERS = %w[if then else elif fi do done while until case esac sudo time exec
             nohup command env set eval source .].freeze

def segments(script)
  out = []
  script.to_s.each_line do |line|
    l = line.strip
    next if l.empty? || l.start_with?('#')
    l.split(SEPARATORS).each do |seg|
      seg = seg.strip.sub(/\A[({]\s*/, '')
      toks = seg.split(/\s+/)
      toks.shift while toks.first &&
                       (LEADERS.include?(toks.first) ||
                        toks.first =~ /\A[A-Za-z_][A-Za-z0-9_]*=/)
      next if toks.empty?
      out << [toks, seg]
    end
  end
  out
end

gates.each do |job|
  puts "GATE\t#{job}"
  steps = (jobs[job] || {})['steps'] || []
  steps.each_with_index do |step, idx|
    next unless step.is_a?(Hash)
    script = step['run']
    next if script.nil?
    segments(script).each do |toks, seg|
      head = File.basename(toks.first.to_s)
      puts "BAREMAKE\t#{job}\t#{idx}\t#{seg}" if head == 'make'
      puts "ROUTED\t#{job}\t#{idx}" if toks.any? { |t| t.end_with?('bin/run-checks.sh') }
    end
  end
end
RUBY_EOF

scan() { # scan <workflow.yml> <out-records>
  "$RUBY" "$SCANNER" "$1" >"$2" 2>"$2.err"
}

# ── W1/W2: the real workflow ─────────────────────────────────────────────────
if [[ ! -f "$CI_YML" ]]; then
  bad "cannot find .github/workflows/ci.yml — this guard has nothing to check, which is a failure, not a pass"
else
  REC="$WORK/ci.records"
  if ! scan "$CI_YML" "$REC"; then
    bad "ci.yml could not be parsed: $(head -n 1 "$REC.err")"
  else
    gate_count=$(grep -c $'^GATE\t' "$REC" || true)
    if [[ "$gate_count" -eq 0 ]]; then
      bad "found ZERO jobs declaring '# oc-job-role: gate' in ci.yml — either every gate lost its marker or this scan stopped seeing them; a green here would be a green over an empty set"
    else
      ok "scan sees $gate_count gate job(s) in .github/workflows/ci.yml"

      bare="$(grep $'^BAREMAKE\t' "$REC" || true)"
      if [[ -n "$bare" ]]; then
        while IFS=$'\t' read -r _ job idx seg; do
          bad "gate job '$job' (step $idx) calls make DIRECTLY: \`$seg\` — a gate must reach its checks through bin/run-checks.sh, which is the only thing that requires each check's own [oc-check-done] end marker. A bare make removes that protection with no red anywhere."
        done <<<"$bare"
      else
        ok "no gate job calls make directly — every make in a gate goes through bin/run-checks.sh"
      fi

      routed=$(grep -c $'^ROUTED\t' "$REC" || true)
      if [[ "$routed" -eq 0 ]]; then
        bad "not one gate job routes through bin/run-checks.sh — the wrapper has no callers left in ci.yml, so the rule above passed over an empty set"
      else
        ok "sentinel — $routed gate step(s) route through bin/run-checks.sh, so the scan is looking at something real"
      fi
    fi
  fi
fi

# ── W3: does the scanner actually discriminate? ──────────────────────────────
# Two synthetic workflows differing ONLY in the one line that matters. Without
# this, a scanner that silently matched nothing (a regex that stopped fitting,
# a psych upgrade that changed a shape) would report the tree clean forever.
mk_fixture() { # mk_fixture <path> <run-line>
  cat >"$1" <<FIXTURE_EOF
name: fixture
on:
  pull_request:
jobs:
  fixture-gate:
    # oc-job-role: gate
    runs-on: macos-15
    steps:
      - uses: actions/checkout@v5
      - run: $2
  fixture-not-a-gate:
    # oc-job-role: not-a-gate
    runs-on: macos-15
    steps:
      - run: make some-target
FIXTURE_EOF
}

mk_fixture "$WORK/mutant.yml" "make lint-ts test-frontend-unit"
mk_fixture "$WORK/clean.yml" "bash bin/run-checks.sh lint-ts test-frontend-unit"

if scan "$WORK/mutant.yml" "$WORK/mutant.records"; then
  hits="$(grep $'^BAREMAKE\t' "$WORK/mutant.records" || true)"
  n=$([[ -z "$hits" ]] && echo 0 || printf '%s\n' "$hits" | wc -l | tr -d ' ')
  if [[ "$n" -eq 1 ]] && printf '%s' "$hits" | grep -q 'fixture-gate'; then
    ok "control — a gate cell running a bare \`make\` is flagged, exactly once and by name (fixture-gate)"
  else
    bad "control — a gate cell running a bare \`make\` produced $n finding(s) instead of exactly 1 naming fixture-gate; this scan cannot be trusted to catch the real thing"
  fi
else
  bad "control — the fixture workflow could not be parsed: $(head -n 1 "$WORK/mutant.records.err")"
fi

if scan "$WORK/clean.yml" "$WORK/clean.records"; then
  hits="$(grep $'^BAREMAKE\t' "$WORK/clean.records" || true)"
  if [[ -z "$hits" ]]; then
    ok "control — a gate cell going through bin/run-checks.sh is NOT flagged (the rule is not simply always-red)"
  else
    bad "control — a gate cell going through bin/run-checks.sh was flagged anyway: ${hits//$'\n'/ / } — a guard that cries wolf is how a real red gets ignored"
  fi
else
  bad "control — the clean fixture workflow could not be parsed: $(head -n 1 "$WORK/clean.records.err")"
fi

# The not-a-gate half of both fixtures runs a bare `make` on purpose: this guard
# is scoped to jobs that CLAIM to be gates, and must not spread beyond that.
if [[ -f "$WORK/clean.records" ]] && ! grep -q 'fixture-not-a-gate' "$WORK/clean.records"; then
  ok "scope — a job that does not declare itself a gate is not scanned (that job's shape is auto-beta-guard's subject, not this one's)"
else
  bad "scope — a non-gate job appeared in this scan; the marker binding is reading the wrong job key"
fi

printf 'ci run-checks entrypoint guard: %d ok, %d failed\n' "$PASS" "$FAIL"
[[ "$FAIL" -eq 0 ]]
