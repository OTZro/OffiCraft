package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
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

// uploadedRef is the light ref the attachments route mints for a stored blob.
type uploadedRef struct {
	ID       string `json:"id"`
	Mime     string `json:"mime"`
	Filename string `json:"filename"`
	// The response body verbatim — stdout line 2 is the SERVER's JSON, not a
	// re-serialisation of the three fields above, so a field this build does not
	// know about still reaches whoever is reading the output.
	raw string
}

// postAttachment streams one body into POST /api/chat/attachments and returns
// the minted ref. `verb` is only there so a diagnostic names the subcommand the
// reader ran — it used to have a second caller in `diff`, which no longer
// uploads anything (T-59: a comparison is a URL, not a stored blob).
//
// `size` is passed to Content-Length; -1 leaves it unset for an in-memory body.
func postAttachment(
	client httpClient, cfg Config, verb string,
	body io.Reader, size int64, filename, mimeType string,
	errOut io.Writer,
) (uploadedRef, int) {
	query := url.Values{}
	if name := strings.TrimSpace(filename); name != "" && name != "." && name != string(filepath.Separator) {
		query.Set("filename", name)
	}
	if declared := strings.TrimSpace(mimeType); declared != "" {
		query.Set("mime", declared)
	}
	// url.Values.Encode escapes the media type, which matters: a `+` reaches the
	// server as a SPACE when a query is pasted together by hand.
	reqURL := cfg.Base + "/api/chat/attachments?" + query.Encode()

	req, err := http.NewRequest(http.MethodPost, reqURL, body)
	if err != nil {
		fmt.Fprintf(errOut, "[ocagent] %s: bad request for %q: %v\n", verb, filename, err)
		return uploadedRef{}, 1
	}
	if size >= 0 {
		req.ContentLength = size
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+cfg.Token)

	resp, err := client.Do(req)
	if err != nil {
		fmt.Fprintf(errOut, "[ocagent] %s: request failed (network): %v\n", verb, err)
		return uploadedRef{}, 1
	}
	defer resp.Body.Close()

	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	detail := strings.TrimSpace(string(raw))

	switch {
	case resp.StatusCode == http.StatusOK:
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		fmt.Fprintf(errOut, "[ocagent] %s: auth rejected (HTTP %d) for %q: %s\n",
			verb, resp.StatusCode, filename, detail)
		return uploadedRef{}, 3
	case resp.StatusCode == http.StatusBadRequest:
		fmt.Fprintf(errOut, "[ocagent] %s: server rejected %q (HTTP 400): %s\n",
			verb, filename, detail)
		return uploadedRef{}, 4
	default:
		fmt.Fprintf(errOut, "[ocagent] %s: unexpected HTTP %d for %q: %s\n",
			verb, resp.StatusCode, filename, detail)
		return uploadedRef{}, 5
	}

	var ref uploadedRef
	if err := json.Unmarshal(raw, &ref); err != nil || ref.ID == "" {
		fmt.Fprintf(errOut, "[ocagent] %s: 200 but unparseable ref body: %s\n", verb, detail)
		return uploadedRef{}, 5
	}
	ref.raw = detail
	return ref, 0
}

// cmdUpload implements `ocagent upload`. On success stdout carries the minted
// attachment id then the server's light-ref JSON; diagnostics go to `errOut`.
func cmdUpload(client httpClient, cfg Config, path, mimeType string, out, errOut io.Writer) int {
	if cfg.Token == "" {
		// Fail fast + honestly: without a token the server would 401 anyway, but
		// the local message ("mis-wired launch") beats a bare server status.
		fmt.Fprint(errOut, "[ocagent] upload: no OC_TOKEN configured — cannot make an authed upload.\n")
		return 3
	}
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

// uploadOneFile streams a path through postAttachment, reporting the
// filesystem faults as upload's exit code 1.
//
// This is the ONE place in the CLI that turns a path into stored bytes:
// `diff` names addresses and reads no files at all (owner 2026-09-03).
func uploadOneFile(
	client httpClient, cfg Config, verb, path, mimeType string, errOut io.Writer,
) (uploadedRef, int64, int) {
	f, err := os.Open(path)
	if err != nil {
		fmt.Fprintf(errOut, "[ocagent] %s: cannot open %s: %v\n", verb, path, err)
		return uploadedRef{}, 0, 1
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		fmt.Fprintf(errOut, "[ocagent] %s: cannot stat %s: %v\n", verb, path, err)
		return uploadedRef{}, 0, 1
	}
	if info.IsDir() {
		fmt.Fprintf(errOut, "[ocagent] %s: %s is a directory, not a file\n", verb, path)
		return uploadedRef{}, 0, 1
	}
	ref, code := postAttachment(client, cfg, verb, f, info.Size(),
		filepath.Base(path), mimeType, errOut)
	return ref, info.Size(), code
}
