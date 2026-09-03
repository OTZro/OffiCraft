package main

import (
	"bytes"
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
// diff: ocagent diff <before> <after> [--label-before X] [--label-after Y]
// ---------------------------------------------------------------------------
//
// The agent-facing half of the compare attachment (T-59, owner 2026-09-03:
// 「可以指定兩個文件位置，就可以跳出我們這個 diff 的畫面」). Point it at two
// files, get back one attachment id to hang on a message, a reply card or a
// task artifact; the owner clicks it and lands in the compare screen.
//
// WHY A SUBCOMMAND RATHER THAN "the agent writes the JSON itself". The pointer
// pair is three uploads that have to happen in order, and the two ids it names
// only exist after the first two land. An agent doing that by hand types the
// ids into a JSON literal from the output of two earlier commands — a step that
// is easy, boring and silently wrong when it goes wrong (the pair is accepted,
// and one side simply never resolves). The friction is also the thing that
// decides whether a feature is used at all, and this one is worth nothing if
// agents find it annoying.
//
// The BYTES ARE NEVER COPIED into the pair: it stores the two blob ids, so the
// two documents stay individually openable and nothing is stored twice.
//
// Stdout mirrors `upload` so the two are scriptable the same way:
//   line 1: the compare attachment's id
//   line 2: its light-ref JSON {id, mime, filename}
// Exit codes are upload's, unchanged: 0 ok, 1 transport/filesystem,
// 2 usage, 3 auth, 4 rejected by the server, 5 anything else.

const diffAttachmentMime = "application/vnd.officraft.diff"

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
// the minted ref. Extracted from cmdUpload so `diff` cannot grow a second,
// slightly different copy of the auth, query-building and exit-code contract —
// `verb` is only there so a diagnostic names the subcommand the reader ran.
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

// uploadOneFile streams a path through postAttachment, reporting the
// filesystem faults as upload's exit code 1.
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

// cmdDiff implements `ocagent diff`. Three uploads: the two documents, then the
// pair that names them.
func cmdDiff(
	client httpClient, cfg Config,
	beforePath, afterPath, beforeLabel, afterLabel string,
	out, errOut io.Writer,
) int {
	if cfg.Token == "" {
		fmt.Fprint(errOut, "[ocagent] diff: no OC_TOKEN configured — cannot make an authed upload.\n")
		return 3
	}

	before, beforeSize, code := uploadOneFile(client, cfg, "diff", beforePath, "", errOut)
	if code != 0 {
		return code
	}
	after, afterSize, code := uploadOneFile(client, cfg, "diff", afterPath, "", errOut)
	if code != 0 {
		return code
	}
	fmt.Fprintf(errOut, "[ocagent] diff: %s (%d bytes) → %s, %s (%d bytes) → %s\n",
		filepath.Base(beforePath), beforeSize, before.ID,
		filepath.Base(afterPath), afterSize, after.ID)

	// The label is what the compare screen writes above each column, so it
	// defaults to the file's own name rather than to nothing: two unlabelled
	// columns are the state the owner already complained about being unable to
	// read.
	label := func(given, path string) string {
		if trimmed := strings.TrimSpace(given); trimmed != "" {
			return trimmed
		}
		return filepath.Base(path)
	}
	pair, err := json.Marshal(map[string]any{
		"before": map[string]string{
			"attachment_id": before.ID,
			"label":         label(beforeLabel, beforePath),
		},
		"after": map[string]string{
			"attachment_id": after.ID,
			"label":         label(afterLabel, afterPath),
		},
	})
	if err != nil {
		fmt.Fprintf(errOut, "[ocagent] diff: cannot build the pair: %v\n", err)
		return 1
	}

	name := filepath.Base(beforePath) + " → " + filepath.Base(afterPath)
	ref, code := postAttachment(client, cfg, "diff",
		bytes.NewReader(pair), int64(len(pair)), name, diffAttachmentMime, errOut)
	if code != 0 {
		return code
	}
	fmt.Fprintf(errOut, "[ocagent] diff: %s → %s\n", name, ref.ID)
	fmt.Fprintln(out, ref.ID)
	fmt.Fprintln(out, ref.raw)
	return 0
}
