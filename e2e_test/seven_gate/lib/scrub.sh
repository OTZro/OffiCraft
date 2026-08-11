#!/usr/bin/env bash
# e2e_test/seven_gate/lib/scrub.sh — WHAT THE AGENT IS NOT HANDED.
#
# 🔴 THE BUG THIS FILE EXISTS TO MAKE IMPOSSIBLE. The answers to ②⑧⑨ travelled,
# in clear, all the way into the real agent's own shell:
#
#   run.sh 5    exports OC_SG_SCENE_NONCE / OC_SG_PEER_NONCE / OC_SG_IMAGE_ANSWER
#               as a command prefix to the actor (the stub NEEDS them — it is
#               told the picture's number instead of reading it);
#   live.sh     started the warden with `env -u OC_WARDEN_TOKFILE …` — ONE
#               variable unset, the three answers still in the environment;
#   ocwarden    exec.Command without cmd.Env ⇒ the child inherits os.Environ();
#   tmux        new-session inherits it in turn (MEASURED on a throwaway socket:
#               a value exported before `tmux -L … new-session -d 'env > f'`
#               appears in f).
#
# So a live agent that never opened the picture could have typed `env`, read the
# number, and produced a green THAT LOOKS EXACTLY LIKE THE REAL ONE — the single
# failure mode judge.py ⑨ names as fatal to itself. Same for ②'s scene nonce and
# ⑧'s peer nonce, and `SG_TOKEN` (the OWNER's token) was riding along too.
#
# THE FIX IS A CONSTRUCTION, NOT A HABIT. It is deliberately NOT a list of three
# secret names — the next secret would be added to run.sh by someone who never
# reads this file. What is removed is the harness's ENTIRE namespace: every
# exported variable named OC_SG_* or SG_*. The warden needs none of them (it is
# handed OC_BASE / OC_TOKEN / OC_ID / OC_CLAUDE_BIN / OC_CODEX_BIN / OC_NAMESPACE
# explicitly), so the safe set is "nothing of ours" rather than "these three".
#
# 🔴 AND IT PROVES ITSELF BEFORE IT IS TRUSTED. `sg_scrub_assert` builds the exact
# child environment and looks in it — with a POSITIVE CONTROL first, because a
# scrubber that removes nothing and a scrubber whose scan is broken both report
# "0 hits". Guarded hermetically by tests_guard case (26), including a mutant
# that severs the derivation and must go red.
#
# ⚠️ WHAT THIS DOES NOT DO, said plainly: the agent runs as the SAME USER on the
# SAME HOST. Nothing here stops a process that goes looking — the run directory
# is in the repo tree and readable. This closes the path where the answer is
# HANDED to the agent; it does not make the answer unreachable.

# The harness's own namespaces. Two prefixes, space separated. Anything the
# warden or the agent legitimately needs is passed by NAME on the env line in
# actors/live.sh, so widening this can only ever be safe in the wrong direction.
: "${SG_SCRUB_PREFIXES:=OC_SG_ SG_}"

# sg_scrub_names — every EXPORTED variable in this process that belongs to the
# harness. Prints one name per line, sorted (so two calls are comparable).
sg_scrub_names() {
  local n p
  compgen -e 2>/dev/null | sort | while IFS= read -r n; do
    for p in $SG_SCRUB_PREFIXES; do
      case "$n" in
        "$p"*) printf '%s\n' "$n"; break ;;
      esac
    done
  done
}

# _sg_scrub_filter — stdin is one variable NAME per line; print (space
# separated) the ones that belong to the harness namespace.
#
# 🔴 WHY THIS IS A NAMED FUNCTION AND NOT AN INLINE LOOP. It used to live inside
# the `$( )` on the names_left= line below, as a single-line
# `case … ) … ; break ;; esac`. bash 3.2 — the /bin/bash on every stock macOS —
# CANNOT PARSE THAT, and the parse happens at EXPANSION time, so `bash -n` sees
# nothing and the file loads fine right up to the moment the function is called.
# MEASURED on this machine (3.2.57 vs 5.3.9): under 3.2 the substitution died
# with `syntax error near unexpected token 'newline'`, then `set -u` hit
# `n: unbound variable`, and sg_scrub_assert returned 1 — i.e. actors/live.sh
# refuses to spawn, ONE HOP BEFORE THE MONEY IS SPENT, and the failure looks
# exactly like a machine that never came online. Which bash actors/live.sh gets
# is decided by PATH (`#!/usr/bin/env bash`): a Mac without Homebrew bash, or
# any trimmed launchd/cron PATH, gets 3.2.
sg_scrub_filter() {
  local n p
  while IFS= read -r n; do
    for p in $SG_SCRUB_PREFIXES; do
      case "$n" in
        "$p"*) printf '%s ' "$n"; break ;;
      esac
    done
  done
}

# sg_scrub_env — run a command with the harness namespace removed.
#   sg_scrub_env env FOO=bar /path/to/ocwarden run
# Everything the child IS meant to get is spelled out by the caller after this.
sg_scrub_env() {
  local n
  local -a args
  args=()
  while IFS= read -r n; do
    [[ -n "$n" ]] && args+=( -u "$n" )
  done < <(sg_scrub_names)
  env ${args[@]+"${args[@]}"} "$@"
}

# sg_scrub_assert NEEDLE… — refuse unless the scrub really removes them.
#
# Two questions, and the order matters:
#   1. POSITIVE CONTROL — are the needles in this process's environment AT ALL?
#      If none of them is, the check below is vacuous and "clean" would mean
#      nothing, so that is a refusal too (this is the same shape as run.sh 3d's
#      leak-scan control, and for the same reason).
#   2. Is any needle — or any harness-namespaced NAME — still present in the
#      environment the child would actually get?
# rc 0 = the child environment is clean and the check was capable of saying so.
# rc 1 = refuse: nothing may be spawned.
sg_scrub_assert() {
  local needle after before control=0 leaked=0 names_left
  before="$(printenv 2>/dev/null)"
  after="$(sg_scrub_env printenv 2>/dev/null)"
  for needle in "$@"; do
    [[ -n "$needle" ]] || continue
    printf '%s' "$before" | grep -qF -- "$needle" && control=$(( control + 1 ))
  done
  if [[ "$control" -lt 1 ]]; then
    echo "[seven_gate] FATAL: the environment scrub's POSITIVE CONTROL found none of the harness secrets in this process's own environment. Either they were never passed, or this check cannot see them — and in both cases a clean scan below would prove nothing. Refusing to spawn." >&2
    return 1
  fi
  for needle in "$@"; do
    [[ -n "$needle" ]] || continue
    if printf '%s' "$after" | grep -qF -- "$needle"; then
      leaked=$(( leaked + 1 ))
      echo "[seven_gate] FATAL: a harness secret SURVIVES into the environment the agent would inherit. run.sh → actor → warden → tmux is one inheritance chain, so this value would be one \`env\` away from the agent, and for ②⑧⑨ that is an answer — a green from a blind agent looks exactly like a real one." >&2
    fi
  done
  names_left="$(printf '%s' "$after" | sed -n 's/^\([A-Za-z_][A-Za-z0-9_]*\)=.*/\1/p' \
                | sg_scrub_filter)"
  if [[ -n "$names_left" ]]; then
    leaked=$(( leaked + 1 ))
    echo "[seven_gate] FATAL: harness-namespaced variables survive into the child environment: $names_left — the scrub is a list again, not a namespace." >&2
  fi
  [[ "$leaked" -eq 0 ]]
}
