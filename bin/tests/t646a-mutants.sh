#!/usr/bin/env bash
# bin/tests/t646a-mutants.sh — do T-646a's new guards actually discriminate?
#
# A green test suite proves the code passes its tests. It does NOT prove the
# tests would notice the code being wrong, and those two are told apart only by
# breaking the code on purpose and watching the RIGHT test go red. This script
# is that proof, kept in the tree so it can be re-run rather than believed.
#
# For each mutant it asserts THREE things, because any one of them alone is
# satisfiable while the guard is worthless:
#   ① the named test FAILS with the mutant applied,
#   ② the failure names the mutated claim (a test that fails for an unrelated
#      reason — a compile error, a fixture — is not evidence about this guard),
#   ③ the same test PASSES once the mutant is reverted, so the red came from the
#      mutant and not from a dirty tree.
#
# NOT wired into bin/ci.sh: it rewrites source files and reverts them, which is
# not something a CI lane should be doing to a shared checkout. Run it by hand:
#   bash bin/tests/t646a-mutants.sh
#
# Reverting uses a copy this script made itself, never `git checkout --`, which
# would take somebody else's uncommitted edits with it.
#
# 🔴 Both go test calls carry -count=1, and on THIS script that is load-bearing
# rather than hygiene. A cached "ok <pkg> (cached)" would certify a run that
# never executed — so a mutant could be reported ALIVE because the cached green
# from before it was applied came back, and the revert re-run could go green
# without testing anything. The repo guard that caught this (bin/tests/
# go-test-cache-defeat) is right to treat a bare `go test` as a lie waiting to
# happen.

set -uo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
PKG="$HERE/server/ocserverd"
SRC="$PKG/api_tasks_fields.go"
BACKUP="$(mktemp -t t646a-fields.XXXXXX.go)"
trap 'cp "$BACKUP" "$SRC"; rm -f "$BACKUP"' EXIT

cp "$SRC" "$BACKUP"

PASS=0
FAIL=0
declare -a REPORT=()

# mutate <label> <test-regex> <expected-substring-in-failure> <python-edit>
mutate() {
  local label="$1" test_re="$2" want="$3" edit="$4"

  cp "$BACKUP" "$SRC"
  if ! python3 - "$SRC" <<<"$edit"; then
    echo "FAIL [$label] — the mutant did not apply; it proves nothing about the guard" >&2
    FAIL=$((FAIL + 1))
    REPORT+=("$label|MUTANT-NOT-APPLIED|-")
    return
  fi

  local out
  out="$(cd "$PKG" && go test -count=1 -run "$test_re" ./... 2>&1)"
  local rc=$?

  if [[ $rc -eq 0 ]]; then
    echo "FAIL [$label] — the mutant is ALIVE: $test_re still passes with the guard broken" >&2
    FAIL=$((FAIL + 1))
    REPORT+=("$label|ALIVE|-")
    cp "$BACKUP" "$SRC"
    return
  fi
  if grep -q "build failed\|cannot use\|undefined:" <<<"$out"; then
    echo "FAIL [$label] — red came from a COMPILE error, not from the assertion" >&2
    echo "$out" | head -20 >&2
    FAIL=$((FAIL + 1))
    REPORT+=("$label|COMPILE-ERROR|-")
    cp "$BACKUP" "$SRC"
    return
  fi
  if ! grep -qF "$want" <<<"$out"; then
    echo "FAIL [$label] — red, but not on the mutated claim; wanted a failure naming: $want" >&2
    echo "$out" | grep -- "--- FAIL" | head -10 >&2
    FAIL=$((FAIL + 1))
    REPORT+=("$label|WRONG-ASSERTION|-")
    cp "$BACKUP" "$SRC"
    return
  fi

  local which
  which="$(grep -c -- "--- FAIL" <<<"$out")"

  # ③ revert and re-run: the red must go away, or the tree was already dirty.
  cp "$BACKUP" "$SRC"
  if ! (cd "$PKG" && go test -count=1 -run "$test_re" ./... >/dev/null 2>&1); then
    echo "FAIL [$label] — still red AFTER revert; the red was not the mutant's" >&2
    FAIL=$((FAIL + 1))
    REPORT+=("$label|RED-WITHOUT-MUTANT|-")
    return
  fi

  PASS=$((PASS + 1))
  REPORT+=("$label|KILLED|${which} failing test(s)")
}

# ── M1 whole-body validation ────────────────────────────────────────────────
# 🔴 READ THIS BEFORE CHANGING THE MUTANT. The first attempt here reordered the
# two branches INSIDE resolveTaskTextEdit and the mutant survived — correctly.
# Atomicity on this route is STRUCTURAL: resolve the whole body, then write, so
# no reordering within the resolve can leave a task half-applied. The mutant that
# discriminates has to be the shape of the wrong IMPLEMENTATION, which is the
# naive merge of the two predecessor handlers — validate and write each field in
# turn, so the description lands and then the title is refused. That is what this
# mutant reproduces, and the test's job is to notice it.
mutate "M1 whole-body validation" \
  "TestToolsCallUpdateTaskRejectsTheWholeBodyOnABlankTitle" \
  "half-applied" \
  'import sys
p=sys.argv[1]; t=open(p,encoding="utf-8").read()
old="""	edit, bad := resolveTaskTextEdit(*t, title, description)
	if bad != "" {
		writeError(w, http.StatusBadRequest, bad)
		return
	}"""
new="""	edit, bad := resolveTaskTextEdit(*t, title, description)
	if bad != "" {
		if description != nil {
			d := trimString(*description)
			if d != t.Description {
				_, _ = s.writeTaskText(t, currentActor(r), taskTextEdit{setDescription: true, description: d})
			}
		}
		writeError(w, http.StatusBadRequest, bad)
		return
	}"""
assert t.count(old)==1, "M1 anchor"
open(p,"w",encoding="utf-8").write(t.replace(old,new))'

# ── M2 the description is trimmed before it is STORED ───────────────────────
mutate "M2 description trimmed on store" \
  "TestToolsCallUpdateTaskTrimsBothFields" \
  "stored description was not trimmed" \
  'import sys
p=sys.argv[1]; t=open(p,encoding="utf-8").read()
old="""	if description != nil {
		v := trimString(*description)"""
new="""	if description != nil {
		v := *description"""
assert t.count(old)==1, "M2 anchor"
open(p,"w",encoding="utf-8").write(t.replace(old,new))'

# ── M3 the title guard itself ───────────────────────────────────────────────
# The owner ruling that a blank title is refused rather than cleared
# (rc-796541192519 ①). Without this branch a blank flows through as a write.
mutate "M3 blank-title refusal" \
  "TestToolsCallUpdateTaskRefusesABlankTitle" \
  "must be refused through MCP too" \
  'import sys
p=sys.argv[1]; t=open(p,encoding="utf-8").read()
old="""		if v == "" {
			// Same words create_task uses, deliberately: one rule, one sentence.
			return taskTextEdit{}, "title must not be blank"
		}
"""
assert t.count(old)==1, "M3 anchor"
open(p,"w",encoding="utf-8").write(t.replace(old,""))'

# ── M4 only the CHANGED field is versioned ──────────────────────────────────
mutate "M4 per-field history enrolment" \
  "TestToolsCallUpdateTaskVersionsOnlyTheFieldThatChanged" \
  "untouched description was versioned anyway" \
  'import sys
p=sys.argv[1]; t=open(p,encoding="utf-8").read()
old="""	if e.setTitle {
		streams = append(streams, taskTitleHistoryStream(t.ID, actor))
	}
	if e.setDescription {
		streams = append(streams, taskDescriptionHistoryStream(t.ID, actor))
	}"""
new="""	streams = append(streams, taskTitleHistoryStream(t.ID, actor))
	streams = append(streams, taskDescriptionHistoryStream(t.ID, actor))"""
assert t.count(old)==1, "M4 anchor"
open(p,"w",encoding="utf-8").write(t.replace(old,new))'

# ── M5 partial update: an unnamed field is left alone ───────────────────────
# Treat an absent field as an empty one — the mistake a defaulted "" would make.
mutate "M5 unnamed field untouched" \
  "TestToolsCallUpdateTaskLeavesUnnamedFieldsAlone" \
  "disturbed the" \
  'import sys
p=sys.argv[1]; t=open(p,encoding="utf-8").read()
old="""func resolveTaskTextEdit(t Task, title, description *string) (taskTextEdit, string) {
	var e taskTextEdit"""
new="""func resolveTaskTextEdit(t Task, title, description *string) (taskTextEdit, string) {
	var e taskTextEdit
	if description == nil {
		blank := ""
		description = &blank
	}"""
assert t.count(old)==1, "M5 anchor"
open(p,"w",encoding="utf-8").write(t.replace(old,new))'

# ── M6 the comparison is made on the TRIMMED value ──────────────────────────
# Store trimmed (so every read-back assertion still passes) but compare RAW, so a
# resend differing only by whitespace reads as a change and burns a revision.
# This is the half of the trim ruling that no read-back can see.
mutate "M6 compare on the trimmed value" \
  "TestToolsCallUpdateTaskTrimsBothFields" \
  "burned a revision" \
  'import sys
p=sys.argv[1]; t=open(p,encoding="utf-8").read()
old="""	if description != nil {
		v := trimString(*description)
		if v != t.Description {
			e.setDescription, e.description = true, v
		}
	}"""
new="""	if description != nil {
		v := trimString(*description)
		if *description != t.Description {
			e.setDescription, e.description = true, v
		}
	}"""
assert t.count(old)==1, "M6 anchor"
open(p,"w",encoding="utf-8").write(t.replace(old,new))'

echo
if [[ $FAIL -ne 0 ]]; then
  echo "T-646a mutants: $PASS killed, $FAIL NOT killed — the table is withheld, because a" >&2
  echo "partial run printed as a table is exactly how a hole gets quoted as coverage." >&2
  exit 1
fi

printf '%-38s %-8s %s\n' "MUTANT" "VERDICT" "DETAIL"
for row in "${REPORT[@]}"; do
  IFS='|' read -r label verdict detail <<<"$row"
  printf '%-38s %-8s %s\n' "$label" "$verdict" "$detail"
done
echo
echo "T-646a mutants: $PASS/$PASS killed."
