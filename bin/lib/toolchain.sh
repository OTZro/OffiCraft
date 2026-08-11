#!/usr/bin/env bash
# officraft — THE single definition of how CI resolves its toolchain binaries.
#
# WHY THIS FILE EXISTS
# The same abspath-fallback block used to be copy-pasted into bin/ci.sh,
# bin/ci-cloud.sh and bin/ci-macos-host.sh — three copies of one rule, which is
# how one of them silently loses a candidate path. T-4d88 moved every check into
# a named Makefile target; the toolchain rule moved with it, into one file that
# every target sources.
#
# THE RULE, and it is the same for every tool here: a MISSING tool is a FAILURE,
# never a skip. "the scanner was not installed" and "the scanner found nothing"
# produce the same silence, and one of them is a hole.
#
# WHY ABSOLUTE PATHS AT ALL: the launchd autodeploy job runs with a minimal PATH
# (/usr/bin:/bin:/usr/sbin:/sbin — no /opt/homebrew/bin), so a bare `go` /
# `npm` / `gitleaks` is command-not-found. `command -v` finds them when PATH is
# rich; the fallbacks are the common brew / stock-install locations.
#
# Each function PRINTS the resolved absolute path on stdout and returns 0, or
# prints a diagnosis on stderr and returns 1. Call it as `GO="$(oc_go)"` under
# `set -e` so a missing tool aborts the caller.

oc_go() {
  local go cand
  go="$(command -v go 2>/dev/null || true)"
  if [[ -z "$go" ]]; then
    for cand in /opt/homebrew/bin/go /usr/local/go/bin/go /usr/local/bin/go; do
      [[ -x "$cand" ]] && { go="$cand"; break; }
    done
  fi
  if [[ -z "$go" || ! -x "$go" ]]; then
    echo "FAIL — go not found. It is a HARD dependency, never a skip. Install: brew install go" >&2
    return 1
  fi
  printf '%s\n' "$go"
}

oc_gofmt() {
  local go gofmt
  go="$(oc_go)" || return 1
  gofmt="$(dirname "$go")/gofmt"
  [[ -x "$gofmt" ]] || gofmt="$(command -v gofmt 2>/dev/null || echo gofmt)"
  printf '%s\n' "$gofmt"
}

oc_npm() {
  local npm cand
  npm="$(command -v npm 2>/dev/null || true)"
  if [[ -z "$npm" ]]; then
    for cand in "$HOME/.asdf/shims/npm" /opt/homebrew/bin/npm /usr/local/bin/npm; do
      [[ -x "$cand" ]] && { npm="$cand"; break; }
    done
  fi
  if [[ -z "$npm" || ! -x "$npm" ]]; then
    echo "FAIL — npm not found. It is a HARD dependency (like go and gitleaks), never a skip." >&2
    return 1
  fi
  printf '%s\n' "$npm"
}

oc_gitleaks() {
  local gl
  gl="$(command -v gitleaks 2>/dev/null || echo /opt/homebrew/bin/gitleaks)"
  if [[ ! -x "$gl" ]]; then
    echo "FAIL — gitleaks not found (install: brew install gitleaks). NOT skipped — an unrun scanner and a clean scan look identical." >&2
    return 1
  fi
  printf '%s\n' "$gl"
}
