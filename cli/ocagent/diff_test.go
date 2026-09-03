package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

// capturedUpload is one POST /api/chat/attachments the CLI made, in order.
type capturedUpload struct {
	mime     string
	filename string
	body     string
}

// diffServer mints a fresh id per upload and records what arrived, so a test
// can assert BOTH the order of the three posts and that the pair names the ids
// the server actually handed back — the transposition this subcommand exists to
// prevent is invisible to any check that only looks at the last request.
func diffServer(t *testing.T) (*httptest.Server, *[]capturedUpload) {
	t.Helper()
	var seen []capturedUpload
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		q := r.URL.Query()
		seen = append(seen, capturedUpload{
			mime:     q.Get("mime"),
			filename: q.Get("filename"),
			body:     string(body),
		})
		id := "att-" + string(rune('a'+len(seen)-1))
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"id":"` + id + `","mime":"` + q.Get("mime") +
			`","filename":"` + q.Get("filename") + `"}`))
	}))
	t.Cleanup(srv.Close)
	return srv, &seen
}

func TestDiffUploadsBothFilesThenAPairNamingTheirIDs(t *testing.T) {
	srv, seen := diffServer(t)
	beforePath := writeTempFile(t, "old.txt", []byte("alpha\nbravo\n"))
	afterPath := writeTempFile(t, "new.txt", []byte("alpha\nBRAVO\n"))

	var out, errOut bytes.Buffer
	cfg := Config{Base: srv.URL, Token: "tok"}
	if rc := cmdDiff(srv.Client(), cfg, beforePath, afterPath, "", "", &out, &errOut); rc != 0 {
		t.Fatalf("rc = %d, want 0 (%s)", rc, errOut.String())
	}

	if len(*seen) != 3 {
		t.Fatalf("want three uploads (two documents then the pair), got %d", len(*seen))
	}
	// The two documents go up as themselves — no declared type, so the server
	// keeps its own sniffing, exactly as `upload` with no --mime.
	if (*seen)[0].body != "alpha\nbravo\n" || (*seen)[0].mime != "" {
		t.Errorf("first upload = %+v, want the before file's bytes untyped", (*seen)[0])
	}
	if (*seen)[1].body != "alpha\nBRAVO\n" || (*seen)[1].mime != "" {
		t.Errorf("second upload = %+v, want the after file's bytes untyped", (*seen)[1])
	}

	// The pair is typed, and it is a POINTER PAIR: the documents' bytes must not
	// appear in it a second time.
	pair := (*seen)[2]
	if pair.mime != diffAttachmentMime {
		t.Errorf("pair mime = %q, want %q", pair.mime, diffAttachmentMime)
	}
	type side struct {
		AttachmentID string `json:"attachment_id"`
		Label        string `json:"label"`
	}
	var got struct {
		Before side `json:"before"`
		After  side `json:"after"`
	}
	if err := json.Unmarshal([]byte(pair.body), &got); err != nil {
		t.Fatalf("pair body is not JSON: %v (%s)", err, pair.body)
	}
	if got.Before.AttachmentID != "att-a" || got.After.AttachmentID != "att-b" {
		t.Errorf("pair names %q/%q, want the ids the server minted (att-a/att-b) in that order",
			got.Before.AttachmentID, got.After.AttachmentID)
	}
	// Unlabelled columns are the state the owner could not read; the file's own
	// name is the default rather than nothing.
	if got.Before.Label != "old.txt" || got.After.Label != "new.txt" {
		t.Errorf("labels = %q/%q, want the two basenames", got.Before.Label, got.After.Label)
	}

	// stdout mirrors `upload`: the id, then the server's own ref JSON.
	lines := bytes.Split(bytes.TrimRight(out.Bytes(), "\n"), []byte("\n"))
	if len(lines) != 2 || string(lines[0]) != "att-c" {
		t.Errorf("stdout = %q, want the pair's id then its ref JSON", out.String())
	}
}

func TestDiffLabelsOverrideTheFileNames(t *testing.T) {
	srv, seen := diffServer(t)
	var out, errOut bytes.Buffer
	cfg := Config{Base: srv.URL, Token: "tok"}
	rc := cmdDiff(srv.Client(), cfg,
		writeTempFile(t, "a.txt", []byte("x")),
		writeTempFile(t, "b.txt", []byte("y")),
		"9/2 21:12", "目前存檔內容", &out, &errOut)
	if rc != 0 {
		t.Fatalf("rc = %d (%s)", rc, errOut.String())
	}
	type side struct {
		Label string `json:"label"`
	}
	var got struct {
		Before side `json:"before"`
		After  side `json:"after"`
	}
	if err := json.Unmarshal([]byte((*seen)[2].body), &got); err != nil {
		t.Fatal(err)
	}
	if got.Before.Label != "9/2 21:12" || got.After.Label != "目前存檔內容" {
		t.Errorf("labels = %q/%q, want the given headings", got.Before.Label, got.After.Label)
	}
}

// A pair whose sides never uploaded would be a compare attachment that can
// never draw. The run has to stop at the failure, not carry on and mint one.
func TestDiffStopsBeforeMintingAPairWhenASideFails(t *testing.T) {
	var seen int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen++
		if seen == 2 {
			w.WriteHeader(400)
			_, _ = w.Write([]byte(`{"error":{"message":"attachment is empty"}}`))
			return
		}
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"id":"att-a","mime":"text/plain","filename":"a.txt"}`))
	}))
	t.Cleanup(srv.Close)

	var out, errOut bytes.Buffer
	cfg := Config{Base: srv.URL, Token: "tok"}
	rc := cmdDiff(srv.Client(), cfg,
		writeTempFile(t, "a.txt", []byte("x")),
		writeTempFile(t, "b.txt", []byte("y")),
		"", "", &out, &errOut)

	if rc != 4 {
		t.Errorf("rc = %d, want 4 (the server rejected a side)", rc)
	}
	if seen != 2 {
		t.Errorf("%d requests made, want 2 — the pair must not be posted after a side failed", seen)
	}
	if out.Len() != 0 {
		t.Errorf("stdout = %q, want nothing: there is no attachment to name", out.String())
	}
}

func TestDiffRefusesWithoutAToken(t *testing.T) {
	var out, errOut bytes.Buffer
	rc := cmdDiff(http.DefaultClient, Config{Base: "http://unused"},
		"a.txt", "b.txt", "", "", &out, &errOut)
	if rc != 3 {
		t.Errorf("rc = %d, want 3 (no token)", rc)
	}
}
