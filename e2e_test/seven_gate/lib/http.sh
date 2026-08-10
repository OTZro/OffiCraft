#!/usr/bin/env bash
# e2e_test/seven_gate/lib/http.sh — every server call this harness makes goes
# through here, and NONE of them may throw the response away.
#
# 🔴 WHY THIS FILE EXISTS. The first baseline run of the gate went red on three
# steps (①⑤⑦) and the run left NO WAY TO TELL WHY: every call was written
# `curl … >/dev/null`, so a 409 and a 200 looked identical on disk. The actor
# said "① report_waking", the judge said "the fact is not there", and the log
# between them was silent. Two completely different diseases —
#
#   (a) THE CALL FAILED      — the server refused it (4xx/5xx) or never
#                              answered (curl rc≠0). Nothing was ever written.
#   (b) THE CALL SUCCEEDED   — HTTP 200, and the fact STILL is not on the
#       BUT THE FACT DID NOT  server (wrong target, wrong field, a write that
#       LAND                  landed somewhere else).
#
# — have the same appearance once the body is gone, and (b) is the far more
# dangerous one, because it is the shape a WRONG API CONTRACT takes. So the
# rule here is absolute: the METHOD, the PATH, the HTTP STATUS and the RESPONSE
# BODY of every single call are written to the log. A reader then separates (a)
# from (b) at a glance — (a) shows a non-2xx line naming the call, (b) shows
# 2xx everywhere and a red verdict.
#
# CONTRACT
#   in  (env): SG_BASE      base URL of the ISOLATED server
#              SG_TOKEN     bearer token this caller acts as
#              SG_HTTP_TAG  who is calling (owner / actor:stub / actor:live …)
#              SG_HTTP_LOG  optional file every call line is ALSO appended to
#   out:       the response body on STDOUT (so callers can still parse it)
#              one log line per call on STDERR (so it lands in run.log/actor.log)
#   rc:        0 = curl ok AND 2xx · 7 = curl itself failed · 8 = non-2xx
#
# WHAT IS BANNED IS `curl … >/dev/null`, NOT `sg_http … >/dev/null`. By the time
# sg_http returns, the status and the body are already in the log; a caller that
# does not need the body may drop the STDOUT copy. What may never happen again is
# a call whose response never reached a log at all — which is why nothing outside
# this file is allowed to invoke curl directly.
#
# The rc is REAL — callers are expected to branch on it. It is not the verdict
# (judge.py is), but a step whose call was refused should say so at the moment
# it happens rather than let the judge discover the absence ten minutes later.

# _sg_http_oneline — bodies are small here; they are flattened, never truncated.
# A truncated body is how the one line that mattered gets cut off.
#
# 🔴 EXCEPT TOKENS, WHICH ARE REDACTED. `POST /api/mint` and `POST /api/machines`
# answer with a real bearer JWT, and "log the whole body" would otherwise write
# live credentials into run.log and http.log — caught by bin/ci.sh's gitleaks
# gate the first time this ran, which is exactly the gate's job. Redacting costs
# nothing diagnostically: nobody debugs a 409 by reading a JWT. The shape of the
# response (which field, present or absent) survives; only the secret goes.
_sg_http_oneline() {
  printf '%s' "$1" \
    | tr '\n\r\t' '   ' \
    | sed -E -e 's/("(token|access_token|exec_token|password)"[[:space:]]*:[[:space:]]*")[^"]*"/\1<REDACTED>"/g' \
             -e 's/eyJ[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+/<REDACTED-JWT>/g'
}

_sg_http_note() { # _sg_http_note LEVEL TEXT
  local line="$2"
  printf '%s\n' "$line" >&2
  [[ -n "${SG_HTTP_LOG:-}" ]] && printf '%s\n' "$line" >> "$SG_HTTP_LOG"
  return 0
}

# sg_http METHOD PATH [JSON_BODY]
sg_http() {
  local method="$1" path="$2" body="${3:-}"
  local tag="${SG_HTTP_TAG:-http}"
  local out err code crc resp cerr

  : "${SG_BASE:?sg_http needs SG_BASE}"
  : "${SG_TOKEN:?sg_http needs SG_TOKEN}"

  out="$(mktemp "${TMPDIR:-/tmp}/sg-http-body.XXXXXX")"
  err="$(mktemp "${TMPDIR:-/tmp}/sg-http-err.XXXXXX")"
  if [[ -n "$body" ]]; then
    code="$(curl -sS -o "$out" -w '%{http_code}' -X "$method" "$SG_BASE$path" \
            -H "Authorization: Bearer $SG_TOKEN" \
            -H 'Content-Type: application/json' --data-binary "$body" 2>"$err")"
    crc=$?
  else
    code="$(curl -sS -o "$out" -w '%{http_code}' -X "$method" "$SG_BASE$path" \
            -H "Authorization: Bearer $SG_TOKEN" 2>"$err")"
    crc=$?
  fi
  resp="$(cat "$out" 2>/dev/null)"
  cerr="$(cat "$err" 2>/dev/null)"
  rm -f "$out" "$err"

  if [[ "$crc" -ne 0 ]]; then
    _sg_http_note fail "[http] 🔴 CALL FAILED (no answer) — $tag $method $path — curl rc=$crc: $(_sg_http_oneline "$cerr")"
    printf '%s' "$resp"
    return 7
  fi
  case "$code" in
    2??)
      _sg_http_note ok "[http] $tag $method $path -> HTTP $code  body: $(_sg_http_oneline "$resp")"
      printf '%s' "$resp"
      return 0
      ;;
    *)
      _sg_http_note fail "[http] 🔴 CALL REFUSED — $tag $method $path -> HTTP $code  body: $(_sg_http_oneline "$resp")"
      printf '%s' "$resp"
      return 8
      ;;
  esac
}

# sg_step — the actor-side wrapper that turns a call's rc into a visible verdict
# line for ONE of the seven steps. It never aborts the run: every remaining step
# is still attempted, because "① failed and everything after it was skipped"
# hides which of the later steps would independently have worked.
sg_step() { # sg_step STEPKEY METHOD PATH [JSON]
  local key="$1"; shift
  local resp rc
  resp="$(sg_http "$@")"; rc=$?
  if [[ "$rc" -eq 0 ]]; then
    _sg_http_note ok "[step] $key: the call was ACCEPTED (whether the fact landed is judge.py's call)"
  else
    _sg_http_note fail "[step] 🔴 $key: the call did NOT succeed (see the [http] line above) — this step cannot possibly be green"
  fi
  printf '%s' "$resp"
  return "$rc"
}
