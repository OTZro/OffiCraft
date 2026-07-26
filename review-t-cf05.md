APPROVE

# Independent review — T-cf05 / `1c20ef6`

## Scope checked

- All JSON DTO write handlers route through `decodeJSONBody` / `decodeJSONBodyRequired`; the only deliberate non-DTO write exceptions are multipart attachment upload, the public free-form webhook inlet, and the JSON-RPC MCP envelope. MCP tool writes loop back through the same REST handlers.
- OpenAPI named object schemas are closed and retain explicit open map fields (for example task `inputs`).
- Mutable MCP catalog schemas are closed recursively for DTO objects and keep only explicitly free-form maps open.

## Resolution of prior findings

Both blocking findings are fixed in the current uncommitted follow-up:

- The shared strict decoder now requires EOF after the first JSON value, and the regression test proves a second JSON value returns 422.
- All nested DTO objects below closed mutable MCP input schemas now declare `additionalProperties: false`. Intentional free-form maps remain open (`post_chat.meta`, `ingest_agent_context.rate_limits`, `create_task.inputs`, and task-manual `assignee` maps). The new catalog-plus-loopback test checks `create_task.target` and rejects its unknown nested field through the actual MCP route.

## Documentation

No additional README or `docs/guide` / `docs/dev` change is required by the behavior change: the wire contracts are the appropriate documentation surface and `spec/mcp.md` now accurately states the nested-object rule.

## Verification

- `go test . -count=1` passed in `server/ocserverd`.
- `git diff --check` passed.
- Catalog inspection confirmed closed nested `create_task.target` semantics and preserved open `create_task.inputs` map semantics.
