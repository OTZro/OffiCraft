#!/usr/bin/env bash
# Shared definition for the tracked-file hygiene denylist.
#
# Both the local run (bin/ci.sh) and the pull request's cloud round call this
# function through the Makefile's scan-tracked-paths target. Keep the
# rules here: duplicating the list lets a newly forbidden path be blocked in one
# caller while silently passing in another.

tracked_path_denylist_hits() {
  local hits
  hits="$(
    git ls-files -z | tr '\0' '\n' | grep -iE \
      -e '(^|/)scratchpad/' \
      -e '\.bak$' \
      -e '\.pem$' \
      -e '\.key$' \
      -e '\.secret$' \
      -e '(^|/)oc\.toml$' \
      -e '(^|/)oc\.lock$' \
      | { grep -vE '\.py$' || true; }
    # `_token` is source-exempt so legitimate Python test files never trip it.
    git ls-files -z | tr '\0' '\n' | grep -iE '_token' | grep -vE '\.py$' || true
  )"
  printf '%s\n' "$hits" | grep -vE '^$' | sort -u || true
}
