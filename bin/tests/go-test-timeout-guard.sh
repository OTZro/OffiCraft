#!/usr/bin/env bash
# The `go test` per-package timeout is a CHOSEN number, and its relationship to
# the CI job that runs it is a CONTRACT. This guard pins that contract.
#
# ── WHAT THE CONTRACT IS, AND WHY IT MATTERS (T-cf93) ────────────────────────
# Two deadlines can end a `make test-go` run, and they end it very differently:
#
#   * The GITHUB JOB deadline (`timeout-minutes:` on the go-checks job in
#     .github/workflows/ci.yml). GitHub kills the whole job. There is no
#     goroutine dump, no name of the test that was still running — the cell is
#     simply cancelled, and the log ends mid-sentence.
#   * GO'S OWN deadline (`go test -timeout …` in the Makefile's test-go recipe).
#     Go prints `panic: test timed out after 15m0s` and dumps EVERY RUNNING
#     GOROUTINE, naming the test that hung. That dump is the only artifact in
#     this system that says WHAT was slow.
#
# So the ordering is load-bearing: GO MUST HIT ITS LIMIT FIRST. Whenever go's
# per-package timeout is >= the job's ceiling, the diagnosable door is unreachable
# and every hang degrades into an un-diagnosable cancelled cell. Nothing else in
# the tree compares those two numbers: they live in two files, in two languages,
# owned by two different habits (one is "make the tests pass", the other is
# "make the cell fit"), and either can move without the other noticing.
#
# ── WHAT IT ASSERTS ──────────────────────────────────────────────────────────
#  T1  The Makefile's `go test` call site(s) actually pass `-timeout`. A missing
#      flag means the number is go's 10m default rather than a decision, and the
#      relation below stops being anybody's property.
#  T2  go's per-package timeout is STRICTLY LESS than the go-checks job ceiling.
#  T3  Both numbers are read from THEIR OWN file — the timeout out of the
#      Makefile, the ceiling out of ci.yml — and NEITHER is written down here.
#      A guard that hard-codes "15" and "25" goes green after someone edits
#      either file, which is the same as having no guard. See the parser
#      controls: they are what prove each extractor really reads its file.
#  T4  The extractors are exercised red AND green on synthetic fixtures BEFORE
#      any claim about the real tree, because "no violation" is also what an
#      extractor that finds nothing at all reports — the vacuous-empty-set
#      green this repo keeps relearning.
#  T5  Three mutants built from the REAL files are each caught: -timeout removed,
#      -timeout raised above the ceiling, ceiling lowered below the timeout.
#  T6  Sentinel: the real tree passes. A guard that is red on a healthy tree gets
#      deleted rather than fixed.
#
# ── WHAT THIS GUARD DOES *NOT* COVER (read before trusting it) ───────────────
#  1. 🔴 THE SUM OVER MODULES. `make test-go` runs `go test ./...` once PER
#     MODULE, sequentially, and each invocation gets the full per-package
#     timeout. The 15m-under-25m budget therefore holds only while AT MOST ONE
#     module ever runs to the limit. This guard compares ONE timeout against ONE
#     ceiling; it does not multiply by the module count and cannot. If a second
#     module grows to fill the timeout the job blows its ceiling and this stays
#     green. Known, deliberate, unguarded — do not read the green as coverage.
#  2. THE REST OF THE CELL'S BUDGET. go-checks also runs lint-go-naming,
#     lint-go-fmt, lint-go-vet, build-go and test-system-interaction-examples.
#     Their cost is in the Makefile's prose budget, not in any assertion here.
#     Strictly-less is necessary, never sufficient.
#  3. OTHER JOBS. Only the job that RUNS test-go is examined. Another cell that
#     grew its own `go test` call site would be found by the call-site scan (T1
#     covers the Makefile, which is the one place a check's HOW is written), but
#     its ceiling is not compared to anything.
#  4. RUNTIME OVERRIDES. `GOFLAGS=-timeout=1h`, a `-timeout` appended by a
#     wrapper, or a job re-declaring its ceiling in a reusable workflow all
#     defeat the intent while keeping the text. Nothing here looks at the
#     environment or at any workflow other than ci.yml.
#  5. WHETHER 15m IS *ENOUGH*. It asserts a relation between two numbers, not
#     that either is well chosen. A suite that legitimately needs 40m fails
#     under this contract and should — but this file cannot tell you that.
#  6. NON-GO SUITES. vitest / playwright / the e2e station have their own
#     ceilings and their own failure modes. Out of scope.
set -uo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT="$(cd "$HERE/../.." && pwd)"
MAKEFILE="$ROOT/Makefile"
CI_YML="$ROOT/.github/workflows/ci.yml"

PASS=0; FAIL=0
ok()  { PASS=$((PASS+1)); printf '  ok   — %s\n' "$1"; }
bad() { FAIL=$((FAIL+1)); printf '  FAIL — %s\n' "$1" >&2; }

WORK="$(mktemp -d -t oc-gotest-timeout.XXXXXX)"
trap 'rm -rf "$WORK"' EXIT

echo "go test timeout ↔ CI job ceiling contract tests"

# ── side A: the Makefile ─────────────────────────────────────────────────────
# COMMAND-POSITION parsing, not `grep -F -- -timeout`. This file, the Makefile's
# own comment block and the recipe's `echo` line all spell `-timeout`, and the
# recipe's echo spells `go test` too. A substring grep matches every one of them
# and is a check with no discriminating power. So: split each line into shell
# segments, strip make's recipe prefixes, skip env-assignment prefixes, and only
# then ask whether the command word is go and its first non-flag argument is
# `test`. Same shape as bin/tests/go-test-nocache-guard.sh, which pins -count=1
# on the same call site.
#
# Emits one TSV record per call site: <label>\t<lineno>\t<timeout-token|->\t<seg>
makefile_go_test_sites() { # FILE [LABEL]
  local file="$1" label="${2:-$1}"
  awk -v LABEL="$label" '
    /^[[:space:]]*#/ { next }
    {
      n = split($0, segs, /(&&|\|\||;|\|)/)
      for (i = 1; i <= n; i++) {
        seg = segs[i]
        sub(/^[[:space:]]+/, "", seg)
        sub(/^[-@+]+/, "", seg)
        gsub(/^[[:space:]]*[({][[:space:]]*/, "", seg)
        sub(/^[[:space:]]+/, "", seg)
        if (seg == "") continue
        m = split(seg, tok, /[[:space:]]+/)
        j = 1
        while (j <= m) {
          t = tok[j]
          if (t == "env") { j++; continue }
          if (t ~ /^[A-Za-z_][A-Za-z0-9_]*=/) { j++; continue }
          break
        }
        if (j > m) continue
        cmd = tok[j]
        gsub(/["'"'"']/, "", cmd)
        sub(/^\$\$/, "$", cmd)          # make doubles every shell $
        isgo = (cmd == "go" || cmd == "$GO" || cmd == "${GO}" || cmd ~ /\/go$/)
        if (!isgo) continue
        sub_cmd = ""
        for (k = j + 1; k <= m; k++) {
          a = tok[k]; gsub(/["'"'"']/, "", a)
          if (a == "") continue
          if (a ~ /^-/) continue
          sub_cmd = a; break
        }
        if (sub_cmd != "test") continue
        # the -timeout argument, in either spelling go accepts
        val = "-"
        for (k = j + 1; k <= m; k++) {
          a = tok[k]; gsub(/["'"'"']/, "", a)
          if (a == "-timeout" || a == "--timeout") {
            if (k + 1 <= m) { val = tok[k+1]; gsub(/["'"'"']/, "", val) }
            break
          }
          if (a ~ /^--?timeout=/) { sub(/^--?timeout=/, "", a); val = a; break }
        }
        printf "%s\t%d\t%s\t%s\n", LABEL, NR, val, seg
      }
    }
  ' "$file"
}

# A make VARIABLE is the honest way to write the number once and use it in the
# recipe twice, so the extractor has to be able to follow one. Resolution is a
# single non-recursive hop against the same file's assignments — enough for
# `GO_TEST_TIMEOUT := 15m`, and it reports failure rather than guessing.
resolve_make_value() { # FILE TOKEN -> literal
  local file="$1" tok="$2" name
  case "$tok" in
    '$('*')'|'${'*'}')
      name="${tok#??}"; name="${name%?}"
      awk -v N="$name" '
        $0 ~ "^" N "[[:space:]]*[:?+]?=" {
          sub("^" N "[[:space:]]*[:?+]?=[[:space:]]*", "")
          sub(/[[:space:]]+$/, "")
          print; exit
        }' "$file"
      ;;
    *) printf '%s\n' "$tok" ;;
  esac
}

# go durations: a sequence of <number><unit>. Anything else is refused loudly —
# a silent 0 here would make every comparison below trivially true.
dur_to_seconds() { # DURATION -> seconds on stdout, rc 1 if unparseable
  awk -v D="$1" '
    BEGIN {
      s = D; total = 0; seen = 0
      while (length(s) > 0) {
        if (match(s, /^[0-9]+(\.[0-9]+)?/) == 0) { exit 1 }
        num = substr(s, 1, RLENGTH); s = substr(s, RLENGTH + 1)
        if (match(s, /^(ns|us|ms|s|m|h)/) == 0) { exit 1 }
        u = substr(s, 1, RLENGTH); s = substr(s, RLENGTH + 1)
        mult = 0
        if (u == "h") mult = 3600
        else if (u == "m") mult = 60
        else if (u == "s") mult = 1
        else if (u == "ms") mult = 0.001
        else if (u == "us") mult = 0.000001
        else if (u == "ns") mult = 0.000000001
        total += num * mult; seen = 1
      }
      if (!seen) exit 1
      printf "%d\n", total
    }'
}

# The whole side-A extractor: Makefile in, seconds out (or MISSING / MULTIPLE /
# UNPARSEABLE:<tok>). One string, so a fixture and the real file go through the
# exact same code path.
makefile_timeout_seconds() { # FILE
  local file="$1" sites vals tok lit secs
  sites="$(makefile_go_test_sites "$file")"
  [[ -n "$(printf '%s' "$sites" | tr -d '[:space:]')" ]] || { echo "NO-CALL-SITE"; return; }
  vals="$(printf '%s\n' "$sites" | awk -F'\t' 'NF { print $3 }' | sort -u)"
  if printf '%s\n' "$vals" | grep -qx -- '-'; then echo "MISSING"; return; fi
  if [[ "$(printf '%s\n' "$vals" | grep -c .)" != "1" ]]; then
    echo "MULTIPLE:$(printf '%s\n' "$vals" | tr '\n' ',')"; return
  fi
  tok="$vals"
  lit="$(resolve_make_value "$file" "$tok")"
  [[ -n "$lit" ]] || { echo "UNRESOLVED:$tok"; return; }
  secs="$(dur_to_seconds "$lit")" || { echo "UNPARSEABLE:$lit"; return; }
  [[ -n "$secs" ]] || { echo "UNPARSEABLE:$lit"; return; }
  echo "$secs"
}

# ── side B: .github/workflows/ci.yml ─────────────────────────────────────────
# ruby+psych, for the same reason bin/tests/auto-beta-guard.sh and
# ci-run-checks-entrypoint-guard.sh use it: the hosted macOS runner has no
# PyYAML, and a line-oriented grep cannot see a `run:` written as a block scalar
# or a `timeout-minutes` that moved by two spaces. And — the point of this
# guard — the JOB is found by asking which job's run steps actually invoke
# test-go in COMMAND POSITION, never by hard-coding the name `go-checks`.
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

SCANNER="$WORK/ci-scan.rb"
cat >"$SCANNER" <<'RUBY_EOF'
require 'yaml'

TARGET = ARGV[1] || 'test-go'
text = File.read(ARGV[0], mode: 'r:UTF-8')
doc = begin
  YAML.safe_load(text, aliases: false)
rescue => e
  warn "PARSE-ERROR #{e.class}: #{e.message}"
  exit 3
end
jobs = (doc && doc['jobs']) || {}

SEPARATORS = /(?:\|\||&&|[;|&])/
LEADERS = %w[if then else elif fi do done while until case esac sudo time exec env nohup].freeze
# Echoing the target's NAME is not running it. Command-position tokenisation is
# what separates `bash bin/run-checks.sh … test-go` from a step called
# "explain test-go" or a comment about it.
NON_RUNNERS = %w[echo printf : true false].freeze

def runs_target?(script, target)
  script.to_s.each_line do |line|
    line = line.sub(/#.*\z/, '')
    line.split(SEPARATORS).each do |seg|
      seg = seg.strip.sub(/\A[({]\s*/, '')
      toks = seg.split(/\s+/)
      toks.shift while toks.first && (LEADERS.include?(toks.first) || toks.first =~ /\A[A-Za-z_][A-Za-z0-9_]*=/)
      next if toks.empty?
      cmd = File.basename(toks.first.to_s)
      next if NON_RUNNERS.include?(cmd)
      return true if toks[1..].to_a.include?(target)
    end
  end
  false
end

hits = []
jobs.each do |name, job|
  next unless job.is_a?(Hash)
  steps = job['steps']
  next unless steps.is_a?(Array)
  ran = steps.any? { |s| s.is_a?(Hash) && runs_target?(s['run'], TARGET) }
  next unless ran
  hits << [name, job['timeout-minutes']]
end

if hits.empty?
  puts "NO-JOB"
elsif hits.size > 1
  puts "MULTIPLE-JOBS:" + hits.map(&:first).join(',')
else
  name, tm = hits.first
  if tm.nil?
    puts "NO-CEILING:#{name}"
  else
    puts "#{name}\t#{tm}"
  end
end
RUBY_EOF

ci_job_ceiling() { # FILE [TARGET] -> "<job>\t<minutes>" or a NO-*/MULTIPLE-* word
  "$RUBY" "$SCANNER" "$1" "${2:-test-go}" 2>&1
}

# ── the verdict, one function, used by fixtures / mutants / the real tree ────
# Both numbers come from their OWN file; nothing expected is written down here.
verdict() { # MAKEFILE CI_YML -> "OK <go_s> <job_s> <job>" | "VIOLATION …" | "BROKEN …"
  local mk="$1" ci="$2" go_s job_line job_name job_min job_s
  go_s="$(makefile_timeout_seconds "$mk")"
  case "$go_s" in
    ''|*[!0-9]*) echo "BROKEN makefile-side=$go_s"; return ;;
  esac
  job_line="$(ci_job_ceiling "$ci")"
  case "$job_line" in
    *$'\t'*) : ;;
    *) echo "BROKEN ci-side=$job_line"; return ;;
  esac
  job_name="${job_line%%$'\t'*}"; job_min="${job_line##*$'\t'}"
  case "$job_min" in ''|*[!0-9]*) echo "BROKEN ci-side-minutes=$job_min"; return ;; esac
  job_s=$(( job_min * 60 ))
  if (( go_s < job_s )); then
    echo "OK $go_s $job_s $job_name"
  else
    echo "VIOLATION go=${go_s}s job=${job_s}s($job_name) — go must hit its limit FIRST"
  fi
}

# ═════════════════════════════════════════════════════════════════════════════
# PARSER CONTROLS — red AND green, BEFORE any claim about the real tree.
# Fixtures are written through printf so their literals never sit in command
# position in THIS file, and each shell-ish fixture is single-segment because
# the parser splits on && (the same trap go-test-nocache-guard.sh documents).
# ═════════════════════════════════════════════════════════════════════════════
AND='&&'

# --- A1: a literal -timeout is FOUND and converted --------------------------
printf '%s\n' 'lit:' "	@(cd x $AND go test -count=1 -timeout 7m ./...)" > "$WORK/lit.mk"
GOT="$(makefile_timeout_seconds "$WORK/lit.mk")"
if [[ "$GOT" == "420" ]]; then
  ok "Makefile side: a literal '-timeout 7m' in a make recipe reads as 420s"
else
  bad "Makefile side: a literal '-timeout 7m' should read as 420s (got: $GOT)"
fi

# --- A2: a make VARIABLE is resolved, not echoed back ------------------------
printf '%s\n' 'MYTO := 12m' 'var:' "	@(cd x $AND \"\$\$GO\" test -count=1 -timeout \$(MYTO) ./...)" > "$WORK/var.mk"
GOT="$(makefile_timeout_seconds "$WORK/var.mk")"
if [[ "$GOT" == "720" ]]; then
  ok "Makefile side: '-timeout \$(MYTO)' with 'MYTO := 12m' resolves to 720s"
else
  bad "Makefile side: '-timeout \$(MYTO)' with 'MYTO := 12m' should resolve to 720s (got: $GOT)"
fi

# --- A3: the RED direction — a call site with no -timeout is reported --------
printf '%s\n' 'bare:' "	@(cd x $AND go test -count=1 ./...)" > "$WORK/bare.mk"
GOT="$(makefile_timeout_seconds "$WORK/bare.mk")"
if [[ "$GOT" == "MISSING" ]]; then
  ok "Makefile side: a 'go test' call site with NO -timeout is reported MISSING"
else
  bad "Makefile side: a 'go test' call site with NO -timeout must be MISSING (got: $GOT)"
fi

# --- A4: decoys — the exact strings a substring grep would swallow -----------
printf '%s\n' \
  '# a comment about go test -timeout 99m that must not count' \
  'decoy:' \
  '	@echo "[test-go] go test -count=1 -timeout 99m $$dir"' \
  '	@printf "%s\n" "go test -timeout 99m"' \
  "	@(cd x $AND go build -o y ./...)" > "$WORK/decoy.mk"
GOT="$(makefile_timeout_seconds "$WORK/decoy.mk")"
if [[ "$GOT" == "NO-CALL-SITE" ]]; then
  ok "Makefile side: comments, echo/printf strings and 'go build' are NOT call sites"
else
  bad "Makefile side: comments/echo/printf/'go build' must not be call sites (got: $GOT)"
fi

# --- A5: garbage is refused, not silently zeroed ----------------------------
printf '%s\n' 'junk:' "	@(cd x $AND go test -timeout soon ./...)" > "$WORK/junk.mk"
GOT="$(makefile_timeout_seconds "$WORK/junk.mk")"
if [[ "$GOT" == UNPARSEABLE:* ]]; then
  ok "Makefile side: an unparseable duration is refused ($GOT), never read as 0"
else
  bad "Makefile side: an unparseable duration must be refused (got: $GOT)"
fi

# --- B1: the ci.yml side finds the RIGHT job, among decoys ------------------
# Two jobs with DIFFERENT ceilings; only one of them runs test-go, and a third
# only mentions it in an echo. A parser that grabbed the first timeout-minutes
# it saw, or that grepped for the word, returns 5 or 99 instead of 25.
cat >"$WORK/two-jobs.yml" <<'YML_EOF'
name: fixture
on: [push]
jobs:
  decoy-first:
    runs-on: macos-15
    timeout-minutes: 5
    steps:
      - run: bash bin/run-checks.sh lint-ts test-frontend-unit
  talks-about-it:
    runs-on: macos-15
    timeout-minutes: 99
    steps:
      - run: echo "next we would run test-go"
  the-real-one:
    runs-on: macos-15
    timeout-minutes: 25
    steps:
      - run: bash bin/run-checks.sh lint-go-vet build-go test-go
YML_EOF
GOT="$(ci_job_ceiling "$WORK/two-jobs.yml")"
if [[ "$GOT" == $'the-real-one\t25' ]]; then
  ok "ci.yml side: picks the job that RUNS test-go (the-real-one, 25) past two decoy jobs"
else
  bad "ci.yml side: should pick 'the-real-one' with 25 (got: ${GOT//$'\t'/ })"
fi

# --- B2: the RED direction — no job runs it ---------------------------------
cat >"$WORK/no-job.yml" <<'YML_EOF'
name: fixture
on: [push]
jobs:
  only-decoy:
    runs-on: macos-15
    timeout-minutes: 5
    steps:
      - run: echo "test-go is not run here"
YML_EOF
GOT="$(ci_job_ceiling "$WORK/no-job.yml")"
if [[ "$GOT" == "NO-JOB" ]]; then
  ok "ci.yml side: a workflow where nothing RUNS test-go reports NO-JOB, not a number"
else
  bad "ci.yml side: a workflow where nothing runs test-go must report NO-JOB (got: $GOT)"
fi

# --- B3: a job that runs it with no ceiling at all --------------------------
cat >"$WORK/no-ceiling.yml" <<'YML_EOF'
name: fixture
on: [push]
jobs:
  uncapped:
    runs-on: macos-15
    steps:
      - run: bash bin/run-checks.sh test-go
YML_EOF
GOT="$(ci_job_ceiling "$WORK/no-ceiling.yml")"
if [[ "$GOT" == "NO-CEILING:uncapped" ]]; then
  ok "ci.yml side: a test-go job with NO timeout-minutes is reported, not treated as infinite-and-fine"
else
  bad "ci.yml side: a test-go job with no timeout-minutes must be reported (got: $GOT)"
fi

# --- C: the verdict function itself, both ways, on synthetic pairs ----------
# Without this the mutants below could all be "caught" by a verdict() that
# always says VIOLATION, and the sentinel could be met by one that never does.
printf '%s\n' 'green:' "	@(cd x $AND go test -timeout 1m ./...)" > "$WORK/v-green.mk"
printf '%s\n' 'red:'   "	@(cd x $AND go test -timeout 30m ./...)" > "$WORK/v-red.mk"
cat >"$WORK/v.yml" <<'YML_EOF'
name: fixture
on: [push]
jobs:
  cell:
    runs-on: macos-15
    timeout-minutes: 25
    steps:
      - run: bash bin/run-checks.sh test-go
YML_EOF
GOT="$(verdict "$WORK/v-green.mk" "$WORK/v.yml")"
if [[ "$GOT" == "OK 60 1500 cell" ]]; then
  ok "verdict: 1m timeout under a 25m ceiling is OK (and both numbers came from their own file)"
else
  bad "verdict: 1m under 25m should be 'OK 60 1500 cell' (got: $GOT)"
fi
GOT="$(verdict "$WORK/v-red.mk" "$WORK/v.yml")"
if [[ "$GOT" == VIOLATION* ]]; then
  ok "verdict: 30m timeout under a 25m ceiling is a VIOLATION"
else
  bad "verdict: 30m under 25m must be a VIOLATION (got: $GOT)"
fi

# ═════════════════════════════════════════════════════════════════════════════
# THE REAL TREE
# ═════════════════════════════════════════════════════════════════════════════
REAL_GO="$(makefile_timeout_seconds "$MAKEFILE")"
REAL_CI="$(ci_job_ceiling "$CI_YML")"

case "$REAL_GO" in
  ''|*[!0-9]*) bad "Makefile: test-go must pass a parseable -timeout to go test (got: $REAL_GO)" ;;
  *)           ok  "Makefile: the go test call site passes -timeout — ${REAL_GO}s, a chosen number rather than go's 10m default" ;;
esac
case "$REAL_CI" in
  *$'\t'*) ok "ci.yml: the job that RUNS test-go is '${REAL_CI%%$'\t'*}', ceiling ${REAL_CI##*$'\t'} min — found by parsing, not by name" ;;
  *)       bad "ci.yml: could not identify the job that runs test-go and its timeout-minutes (got: $REAL_CI)" ;;
esac

REAL_VERDICT="$(verdict "$MAKEFILE" "$CI_YML")"
if [[ "$REAL_VERDICT" == OK\ * ]]; then
  read -r _ g j n <<<"$REAL_VERDICT"
  ok "sentinel — go's per-package timeout (${g}s) is STRICTLY LESS than the $n job ceiling (${j}s): go hits its limit first and dumps goroutines"
else
  bad "sentinel — the real tree must satisfy the contract: $REAL_VERDICT"
fi

# ═════════════════════════════════════════════════════════════════════════════
# MUTANTS, built from the REAL files. Each is one change and nothing else.
# ═════════════════════════════════════════════════════════════════════════════
REAL_TOKEN="$(makefile_go_test_sites "$MAKEFILE" | awk -F'\t' 'NF { print $3; exit }')"

# M1 — strip -timeout from the Makefile's call site.
sed -E 's/ --?timeout[= ][^ ]+//g' "$MAKEFILE" > "$WORK/m1.mk"
if ! diff -q "$MAKEFILE" "$WORK/m1.mk" >/dev/null; then
  M1="$(verdict "$WORK/m1.mk" "$CI_YML")"
  if [[ "$M1" == BROKEN*MISSING* || "$M1" == BROKEN*NO-CALL-SITE* ]]; then
    ok "mutant M1 — Makefile with -timeout removed is CAUGHT ($M1)"
  else
    bad "mutant M1 — Makefile with -timeout removed was NOT caught (verdict: $M1)"
  fi
else
  bad "mutant M1 generator changed NOTHING — the Makefile carries no -timeout to strip, so the mutant proves nothing"
fi

# M2 — raise the timeout above the job ceiling. If the recipe reads the value
# from a make variable the assignment is what moves; if it is a literal, the
# literal is. Either way the guard must go red, and the guard finds out WHICH
# by re-reading the file rather than by assuming.
mutate_timeout() { # SRC DST NEWVAL
  local src="$1" dst="$2" new="$3" name
  case "$REAL_TOKEN" in
    '$('*')'|'${'*'}')
      name="${REAL_TOKEN#??}"; name="${name%?}"
      awk -v N="$name" -v V="$new" '
        $0 ~ "^" N "[[:space:]]*[:?+]?=" { print N " := " V; next } { print }' "$src" > "$dst"
      ;;
    *)
      sed -E "s/(--?timeout[= ])$REAL_TOKEN/\1$new/g" "$src" > "$dst"
      ;;
  esac
}
mutate_timeout "$MAKEFILE" "$WORK/m2.mk" "30m"
if ! diff -q "$MAKEFILE" "$WORK/m2.mk" >/dev/null; then
  M2="$(verdict "$WORK/m2.mk" "$CI_YML")"
  if [[ "$M2" == VIOLATION* ]]; then
    ok "mutant M2 — timeout raised to 30m (above the job ceiling) is CAUGHT ($M2)"
  else
    bad "mutant M2 — timeout raised to 30m was NOT caught (verdict: $M2)"
  fi
else
  bad "mutant M2 generator changed NOTHING — cannot prove the guard notices a raised timeout"
fi

# M3 — lower the CI job's ceiling under go's timeout. The job block is located
# by the NAME THE PARSER FOUND, so this mutant follows a renamed job instead of
# quietly editing nothing.
REAL_JOB="${REAL_CI%%$'\t'*}"
awk -v JOB="$REAL_JOB" '
  $0 ~ "^  " JOB ":[[:space:]]*(#.*)?$" { injob = 1; print; next }
  injob && /^  [A-Za-z0-9_.-]+:/ { injob = 0 }
  injob && /^[[:space:]]*timeout-minutes:[[:space:]]*[0-9]+[[:space:]]*$/ {
    sub(/[0-9]+[[:space:]]*$/, "1"); print; next
  }
  { print }
' "$CI_YML" > "$WORK/m3.yml"
if ! diff -q "$CI_YML" "$WORK/m3.yml" >/dev/null; then
  M3="$(verdict "$MAKEFILE" "$WORK/m3.yml")"
  if [[ "$M3" == VIOLATION* ]]; then
    ok "mutant M3 — the $REAL_JOB job's timeout-minutes lowered to 1 is CAUGHT ($M3)"
  else
    bad "mutant M3 — a job ceiling below go's timeout was NOT caught (verdict: $M3)"
  fi
else
  bad "mutant M3 generator changed NOTHING — could not find $REAL_JOB's timeout-minutes to lower"
fi

echo "go test timeout ↔ CI job ceiling contract tests: $PASS ok, $FAIL failed"
[[ "$FAIL" == "0" ]]
