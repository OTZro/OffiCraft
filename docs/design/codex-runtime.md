# Codex runtime — provider adapter design

> Status: implemented and validated end-to-end on isolated `seth-m1`. Claude remains
> the backward-compatible default.

## Goal and compatibility boundary

OffiCraft selects an AI CLI runtime per permanent member and per outsource worker:
`claude | codex`. Existing rows, clients, task manuals, and warden `start` frames that omit
the field fold to `claude`. The existing Claude launch path and legacy Claude telemetry
fields remain intact.

The server stays provider-neutral. It persists the selection, folds the existing
`persona_context`, chooses a runtime-capable machine, and sends one common `start` frame.
The warden owns the provider adapter:

```text
member / outsource runtime
          |
server: persona + JWT + lifecycle
          |
warden start {runtime, model, effort, ...}
          |
     +----+----+
     |         |
  Claude    Codex App Server
  adapter   adapter + ocagent listener
```

## Codex adapter

The Codex correctness path is a warden-managed `codex app-server` plus the existing
`ocagent listen` lifecycle listener. A TUI may attach remotely for observation or manual
interaction, but TUI presence is not a liveness dependency. The App Server and listener
survive TUI disconnects and preserve the same OffiCraft member identity.

The adapter reuses the Claude persona mechanism:

1. write the server-folded context to the member's private `persona.md`;
2. configure the same OffiCraft MCP endpoint and member JWT;
3. pass only a minimal App Server developer instruction that directs Codex to read and
   obey that persona file.

No generated `AGENTS.md`, duplicate prompt fold, or per-member `CODEX_HOME` is introduced.
Codex uses the machine user's existing login/config, just as Claude uses the shared machine
user's existing login.

The adapter uses App Server's stdio JSON-RPC transport. App Server is still an evolving
upstream surface, so protocol/version handling stays isolated in the sidecar and a failed
initialize/thread start terminates the session visibly for normal OffiCraft reconciliation.

## Global context: one policy, runtime-selected boot tails

OffiCraft keeps one canonical Global Context. Governance, identity, MCP, chat, reply cards,
tasks, lessons/manuals, lifecycle semantics, and `ocagent` commands are provider-neutral
and must not be copied into separate Claude and Codex personas. Only the small, read-only
Boot Sequence is runtime-specific, because listener ownership and the readiness boundary
genuinely differ:

```text
shared Global Context
  + role / lessons / owner additions
  + actor boot semantics (member or outsource)
  + runtime boot sequence (Claude or Codex)
```

This is composition across two independent axes, not four persona copies:

- **Actor semantics** remain the existing member vs outsource distinction:
  members report waking and recover their resume snapshot; workers claim their one task.
- **Runtime mechanics** describe only who owns the listener, context reporting, and
  interactive-question behavior.

The Claude member boot sequence preserves current behavior byte-for-behavior: after boot
readiness, the agent starts bare `ocagent listen` with its Monitor tool; Claude `statusLine`
feeds context telemetry; `AskUserQuestion` stays disabled.

The Codex member boot sequence changes only execution ownership:

1. The sidecar starts a boot-only App Server turn. The member performs
   `report_waking` + resume recovery, or the worker performs `get_my_task`, then completes
   that turn.
2. `turn/completed` is the readiness boundary. Only then does the sidecar launch the same
   bare `ocagent listen` child and consume its stdout; the model must not launch a second
   listener.
3. The sidecar converts listener events into the established idle `turn/start` / active
   `turn/steer` policy. Thus SSE presence still means ready/online and false-online during
   boot remains impossible.
4. App Server `thread/tokenUsage/updated` supplies total usage plus
   `modelContextWindow`; the sidecar computes and posts the same context percentage that
   Claude's `statusLine` reports. Existing warning/handover thresholds remain server-owned
   and unchanged.
5. `request_user_input` is disabled and bridged to reply cards as described below.

### Context compaction and refocus

Claude's percentage-based handover remains unchanged. Codex App Server keeps a durable
thread and can compact that thread without ending the session, so a transient context
percentage is not its useful handover signal. The sidecar counts completed
`contextCompaction` items and includes the current count in context telemetry. At three
compactions in one live session, the existing graceful refocus flow runs; a real session
boundary clears the count before the replacement thread reports again. Monitoring renders
Codex as `context N% · ↻ compact x/3`; Claude remains percentage-only.

The cockpit's Global Context editor remains a single shared owner-additions block. The
read-only Claude and Codex Boot Sequence variants are shown together in the preview. For
workers, the common worker overlay refers to a small final runtime boot tail selected at
assembly time; it carries only that runtime's listener owner, interactive-question guard,
and context-telemetry mechanics. Thus no recipient is asked to ignore another provider's
instructions.

## Wake and steering policy

OffiCraft events are typed wake signals, not prompt text supplied by an external actor.
The adapter renders its own fixed instruction from validated event metadata and makes the
agent re-read authoritative state through MCP.

- Idle thread: a durable pending wake starts a new turn.
- Active thread: steer the active turn, matching Claude's Monitor delivery semantics.
- Listener or App Server exit: terminate the sidecar. The listener child is killed/reaped,
  SSE presence drops honestly, and existing OffiCraft reconciliation restarts the selected
  runtime. Durable server state and the existing `ocagent` cursor remain authoritative.

This matches the existing Claude mechanism at the architectural level: one private persona,
one lifecycle listener, one provider launch adapter, and MCP as the authority.

### User-input requests become OffiCraft ask cards

Codex command approvals and Codex user-input requests are separate mechanisms.
`approval_policy = "never"` removes approval prompts, but the App Server protocol may still
emit `item/tool/requestUserInput`. OffiCraft must bridge that request into the existing
reply-card mechanism instead of waiting for an unattended TUI:

1. Prefer prevention: start Codex in default collaboration mode with
   `features.default_mode_request_user_input=false`, and instruct it to use OffiCraft
   `create_reply_card` whenever owner input is required. This is the Codex equivalent of
   Claude's `--disallowedTools AskUserQuestion`; the explicit feature override pins the
   current default against version/config drift.
2. If App Server nevertheless emits `item/tool/requestUserInput`, the sidecar validates and
   bounds the question payload, then opens one OffiCraft reply card per question as that
   member. Options map directly up to the existing four-option limit; the first card uses
   normal automatic task/step binding and any additional simultaneous cards use
   `bind="none"`. This preserves the existing one-question-per-card convention.
3. Card creation uses the existing automatic current-task/current-step binding. Therefore
   the task enters `waiting_owner` exactly as it does for Claude; no Codex-only card table,
   status, or UI is introduced.
4. The sidecar immediately completes the App Server request with a fixed deferred marker
   and instructs the active Codex turn to yield. It MUST NOT leave the JSON-RPC request
   pending, because that request belongs to one App Server client connection and would be
   fragile across reconnects.
5. The owner's answer or expiry arrives through the existing directed `reply_card` SSE
   delta. The durable wake starts the next idle Codex turn, which reads the authoritative
   card(s) with `get_reply_card` and continues once the required answers are settled, or
   replans after expiry.
6. If card creation fails, return an explicit non-waiting failure answer to Codex; never
   wait for TUI input. A sidecar restart creates a fresh App Server thread, while cards
   already committed remain authoritative OffiCraft state.

For a request marked `isSecret`, OffiCraft creates an `action` card asking the owner to
complete the sensitive step through an appropriate machine-local or provider flow. The
card must not solicit or persist the secret value itself.

MCP `mcpServer/elicitation/request` remains disabled/declined unless it is deliberately
given a separate product mapping. It must never silently become a TUI dependency.

## Capability, installation, and placement

Install resolves and stamps both `OC_CLAUDE_BIN` and `OC_CODEX_BIN` when available. An
installation is usable when at least one provider resolves; selecting a missing or
logged-out provider fails that spawn clearly without breaking the other provider.

Warden telemetry adds a provider-neutral `runtimes` map:

```json
{
  "claude": {"installed": true, "logged_in": true, "version": "…"},
  "codex": {"installed": true, "logged_in": true, "version": "…"}
}
```

The map contains readiness only—never tokens, credential values, or credential paths.
Legacy Claude probe fields stay for existing clients. Codex placement always requires an
explicit `installed == true` and rejects an explicit `logged_in == false`. During a rolling
upgrade only, a completely absent capability map preserves legacy Claude placement; after
any map is reported, Claude follows the same explicit readiness rule. Null login state
remains eligible. Placement is an explicit decision (owner ruling 2026-07-25): a placement
that is offline or lacks the selected runtime is NOT substituted by another host, and
there is no automatic placement to fall back on — a machine nobody named is no placement
at all. Either way no `start` is dispatched; the stall is named on the row the cockpit
reads (`last_op_reason` — `no_machine_selected`, and `machine_unavailable` for an
outsource worker whose named machine cannot take it), and reconcile retries after
telemetry or placement changes.

## Launch policy

The selected adapter receives the shared launch knobs:

- `model`: provider-specific free string; blank uses that provider's default.
- `effort`: exact shared vocabulary `low | medium | high`; omitted uses `medium`.
- Codex sandbox: `danger-full-access`.
- Codex approvals: `never`.

For a blank Codex model, the sidecar tells the boot turn to omit `report_waking.model`.
The model must not guess its own identifier and persist that guess, because the persisted
value becomes an explicit override on the next wake and would replace the machine's Codex
default. An explicit OffiCraft model is reported back verbatim.

These settings intentionally favor capability for this trusted-machine deployment. They do
not expand OffiCraft authorization: member identity, MCP scope, task governance, and
server-side validation remain unchanged.

## Attribution

OffiCraft-authored Codex commits use the human git author only and do not add a Codex
co-author trailer. Existing Claude attribution behavior is unchanged.
