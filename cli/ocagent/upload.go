package main

import (
	"fmt"
	"io"
)

// ---------------------------------------------------------------------------
// upload: ocagent upload <path> [--mime <type>]
// ---------------------------------------------------------------------------
//
// The SEND side of chat attachments — download's mirror twin. MCP `post_chat`
// can only carry file bytes as base64 inside a tool call, which drags the
// whole payload (inflated 4/3×) through the sending agent's LLM context and
// makes large files impossible. This subcommand is the official path around
// that: it STREAMS the file's bytes to POST /api/chat/attachments (a route
// excluded from the MCP surface — a binary ingest, not a tool) and prints the
// minted attachment id, so the agent then sends the message with a light
// `{id}` ref in post_chat's attachments — no byte ever rides its context.
//
// The request body is streamed straight from disk (an *os.File body; the
// http client sets Content-Length from the flag-checked stat) — never
// buffered in memory — using the agent's own token via the ordinary config
// seam (OC_TOKEN / OC_BASE), the same clean-identity contract as every other
// subcommand.
//
// Naming/typing: the file's BASENAME rides ?filename= (the server stores it
// and serves it back via Content-Disposition on download); --mime rides
// ?mime= when given, else the server sniffs (image magic bytes, fallback
// application/octet-stream). The request Content-Type header is ignored by
// the server (see the route's spec) so none is set here.
//
// Stdout on success (script-capturable, mirrors download's path-only stdout):
//   line 1: the minted attachment id
//   line 2: the server's light-ref JSON {id, mime, filename}
// Every diagnostic goes to stderr.
//
// Exit codes (documented so hooks/scripts can branch):
//   0 success
//   1 transport / filesystem failure (unreadable file, refused, DNS, timeout)
//   2 usage (bad flags / missing <path>) — set by realMain's FlagSet
//   3 auth (no token configured, or the server said 401/403)
//   4 rejected (400 — over the size cap, empty file)
//   5 any other unexpected HTTP status

// cmdUpload implements `ocagent upload`. On success stdout carries the minted
// attachment id then the server's light-ref JSON; diagnostics go to `errOut`.
func cmdUpload(client httpClient, cfg Config, path, mimeType string, out, errOut io.Writer) int {
	if cfg.Token == "" {
		// Fail fast + honestly: without a token the server would 401 anyway, but
		// the local message ("mis-wired launch") beats a bare server status.
		fmt.Fprint(errOut, "[ocagent] upload: no OC_TOKEN configured — cannot make an authed upload.\n")
		return 3
	}
	// The streaming, auth and exit-code contract lives in postAttachment
	// (diff.go) — `diff` posts three attachments and must not carry a second,
	// slightly different copy of it.
	ref, size, code := uploadOneFile(client, cfg, "upload", path, mimeType, errOut)
	if code != 0 {
		return code
	}
	fmt.Fprintf(errOut, "[ocagent] upload: %s (%d bytes, %s) → %s\n",
		ref.Filename, size, ref.Mime, ref.ID)
	fmt.Fprintln(out, ref.ID)
	fmt.Fprintln(out, ref.raw)
	return 0
}
