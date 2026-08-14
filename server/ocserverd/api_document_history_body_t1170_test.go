package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// T-1170 split one route into two: list_document_history became a CATALOGUE
// (identity + provenance + per-field SIZES, no prose) and get_document_version
// answers the body of one named revision.
//
// historyRow is what the listing used to be, reassembled the way a caller now
// has to: list, then fetch. Every existing test that asserts WHAT was retained
// goes through it, so none of them can quietly start reading the text out of
// the listing again — and all of them exercise the pairing rather than a
// shortcut past it.
type historyRow struct {
	DocumentHistoryDTO
	Content map[string]string
}

func hydrateHistory(t *testing.T, api *apiServer, kind, key, caller, scope string,
	rows []DocumentHistoryDTO) []historyRow {
	t.Helper()
	out := make([]historyRow, 0, len(rows))
	for _, row := range rows {
		path := fmt.Sprintf("/api/document-history/%s/%s/%d", kind, key, row.Id)
		rec := httptest.NewRecorder()
		api.HandleGetDocumentVersionApiDocumentHistoryKindKeyIdGet(rec,
			taskReq(t, http.MethodGet, path, nil, caller, scope), kind, key, row.Id)
		if rec.Code != http.StatusOK {
			t.Fatalf("get version %s: status=%d body=%s", path, rec.Code, rec.Body.String())
		}
		var version DocumentHistoryVersionDTO
		if err := json.Unmarshal(rec.Body.Bytes(), &version); err != nil {
			t.Fatal(err)
		}
		if version.Kind != kind || version.Key != key || version.Id != row.Id {
			t.Fatalf("get version %s echoed a different address: %+v", path, version)
		}
		out = append(out, historyRow{DocumentHistoryDTO: row, Content: version.Content})
	}
	return out
}

// historyRowsFrom is hydrateHistory for callers that already hold the listing
// response.
func historyRowsFrom(t *testing.T, api *apiServer, kind, key, caller, scope string,
	rec *httptest.ResponseRecorder) []historyRow {
	t.Helper()
	var rows []DocumentHistoryDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &rows); err != nil {
		t.Fatalf("decode history listing: %v (%s)", err, rec.Body.String())
	}
	return hydrateHistory(t, api, kind, key, caller, scope, rows)
}

func historyFixtureWithThreeGlobalVersions(t *testing.T) *apiServer {
	t.Helper()
	api := newTasksTestServer(t)
	for _, text := range []string{"one", "two", "three", "four"} {
		rec := httptest.NewRecorder()
		api.HandleReplaceGlobalContextApiGlobalContextPost(rec, taskReq(t, http.MethodPost,
			"/api/global-context", map[string]any{"text": text}, "owner", "owner"))
		if rec.Code != http.StatusOK {
			t.Fatalf("write %q: status=%d body=%s", text, rec.Code, rec.Body.String())
		}
	}
	return api
}

func listGlobalHistory(t *testing.T, api *apiServer, caller, scope string) (int, []byte) {
	t.Helper()
	rec := httptest.NewRecorder()
	api.HandleListDocumentHistoryApiDocumentHistoryKindKeyGet(rec, taskReq(t, http.MethodGet,
		"/api/document-history/global_context/global", nil, caller, scope),
		"global_context", "global")
	return rec.Code, rec.Body.Bytes()
}

// The listing carries no document text at all — asserted on the RAW bytes, not
// on the decoded DTO: a decoder cannot see a field it has no struct tag for, so
// "the DTO has no Content" would stay green on a handler that kept serving one.
func TestListDocumentHistoryCarriesNoDocumentText(t *testing.T) {
	api := historyFixtureWithThreeGlobalVersions(t)
	status, body := listGlobalHistory(t, api, "owner", "owner")
	if status != http.StatusOK {
		t.Fatalf("list history: status=%d body=%s", status, body)
	}
	var rows []map[string]any
	if err := json.Unmarshal(body, &rows); err != nil {
		t.Fatal(err)
	}
	if len(rows) == 0 {
		t.Fatal("empty history — the assertions below would prove nothing")
	}
	// The retained texts really are distinctive, so finding none of them in the
	// bytes is evidence rather than luck.
	for _, retained := range []string{"one", "two", "three"} {
		if strings.Contains(string(body), `"`+retained+`"`) {
			t.Fatalf("the listing carries the retained text %q: %s", retained, body)
		}
	}
	for _, row := range rows {
		if _, present := row["content"]; present {
			t.Fatalf("the listing row still carries a content map: %v", row)
		}
		for _, key := range []string{"id", "created_ts", "actor_id", "tombstoned", "field_chars"} {
			if _, present := row[key]; !present {
				t.Fatalf("listing row is missing %q: %v", key, row)
			}
		}
		chars, ok := row["field_chars"].(map[string]any)
		if !ok {
			t.Fatalf("field_chars is not a map: %v", row["field_chars"])
		}
		if _, sized := chars["tombstoned"]; sized {
			t.Fatalf("tombstoned is a flag, not a document — it must not be sized: %v", chars)
		}
		if got, want := chars["text"], float64(len("three")); got != want {
			// Every fixture text is 3..5 runes; this pins the one that is
			// newest, which is what row ordering guarantees.
			if row["id"] == rows[0]["id"] {
				t.Fatalf("field_chars.text = %v, want %v", got, want)
			}
		}
	}
}

// The other half: the body of a NAMED version is really reachable, and it is
// the text that version held.
func TestGetDocumentVersionServesTheRetainedText(t *testing.T) {
	api := historyFixtureWithThreeGlobalVersions(t)
	status, body := listGlobalHistory(t, api, "owner", "owner")
	if status != http.StatusOK {
		t.Fatalf("list history: status=%d body=%s", status, body)
	}
	var rows []DocumentHistoryDTO
	if err := json.Unmarshal(body, &rows); err != nil {
		t.Fatal(err)
	}
	got := hydrateHistory(t, api, "global_context", "global", "owner", "owner", rows)
	if len(got) != 3 {
		t.Fatalf("retained versions = %d, want 3", len(got))
	}
	if got[0].Content["text"] != "three" || got[2].Content["text"] != "one" {
		t.Fatalf("fetched bodies = %+v, want three..one newest first", got)
	}
	for _, row := range got {
		if row.FieldChars["text"] != len([]rune(row.Content["text"])) {
			t.Fatalf("the listing's size disagrees with the fetched body: %+v", row)
		}
	}
}

// Reading a version is the same permission as reading the catalogue that names
// it — and the refusals are the SAME refusals, because both routes go through
// documentHistoryAllowed. The paired control is what makes this discriminate:
// if the new route answered everything alike, the accept arm would still pass.
func TestGetDocumentVersionSharesTheListingsGateAndRefusals(t *testing.T) {
	api := historyFixtureWithThreeGlobalVersions(t)
	_, body := listGlobalHistory(t, api, "owner", "owner")
	var rows []DocumentHistoryDTO
	if err := json.Unmarshal(body, &rows); err != nil {
		t.Fatal(err)
	}
	if len(rows) == 0 {
		t.Fatal("empty history — nothing to address")
	}
	id := rows[0].Id

	get := func(kind, key string, versionID int64, caller, scope string) (int, string) {
		t.Helper()
		rec := httptest.NewRecorder()
		api.HandleGetDocumentVersionApiDocumentHistoryKindKeyIdGet(rec,
			taskReq(t, http.MethodGet, "/api/document-history/x", nil, caller, scope),
			kind, key, versionID)
		return rec.Code, rec.Body.String()
	}
	listStatus := func(kind, key, caller, scope string) int {
		t.Helper()
		rec := httptest.NewRecorder()
		api.HandleListDocumentHistoryApiDocumentHistoryKindKeyGet(rec,
			taskReq(t, http.MethodGet, "/api/document-history/x", nil, caller, scope), kind, key)
		return rec.Code
	}

	// A plain agent reads both faces — reading history has always been open at
	// the machine floor, and this ticket did not move it.
	if status, out := get("global_context", "global", id, "m-agent", "agent"); status != http.StatusOK {
		t.Fatalf("agent get version = %d %s, want 200", status, out)
	}
	if status := listStatus("global_context", "global", "m-agent", "agent"); status != http.StatusOK {
		t.Fatalf("agent list = %d, want 200 (the control for the line above)", status)
	}

	for _, refusal := range []struct {
		name string
		kind string
		key  string
	}{
		{"unknown kind", "no_such_kind", "global"},
		{"retired kind", docKindTaskManual, "tm-x"},
		{"malformed lessons key", "lessons", "role-only"},
	} {
		wantSame := listStatus(refusal.kind, refusal.key, "owner", "owner")
		if wantSame != http.StatusBadRequest {
			t.Fatalf("%s: the listing stopped refusing (%d) — this comparison is vacuous",
				refusal.name, wantSame)
		}
		if status, out := get(refusal.kind, refusal.key, id, "owner", "owner"); status != wantSame {
			t.Fatalf("%s: get version = %d %s, want the listing's %d",
				refusal.name, status, out, wantSame)
		}
	}

	// The address is the whole triple: an id that is not a version OF THIS
	// document is a 404, never some other document's text.
	if status, out := get("global_context", "global", id+9999, "owner", "owner"); status != http.StatusNotFound {
		t.Fatalf("unknown version id = %d %s, want 404", status, out)
	}
}
