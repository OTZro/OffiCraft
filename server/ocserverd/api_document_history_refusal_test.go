package main

// The refusal faces of the two document-history routes. Each of these used to
// be reachable with no test at all, and three of them (corrupt row, foreign
// revision id, over-cap restore) fail in a direction that LOOKS like success:
// an empty list, a restore of the wrong document, or a document written past
// the character cap (10,000 by default; a setting since T-3aeb) the write faces refuse to cross.

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestListDocumentHistoryRefusesToRenderACorruptRevision(t *testing.T) {
	api := newTasksTestServer(t)
	rec := httptest.NewRecorder()
	api.HandleReplaceGlobalContextApiGlobalContextPost(rec, taskReq(t, http.MethodPost,
		"/api/global-context", map[string]any{"text": "one"}, "owner", "owner"))
	if rec.Code != http.StatusOK {
		t.Fatalf("seed write: %d %s", rec.Code, rec.Body.String())
	}
	if err := api.dal.SaveWithDocumentHistory("global_context", "global", "owner",
		func(sqlQuerier) (string, error) { return `{"text": not json`, nil },
		func(ex sqlExecer) error {
			return putUserContextOn(ex, UserContext{Text: "two"})
		}); err != nil {
		t.Fatal(err)
	}

	rec = httptest.NewRecorder()
	api.HandleListDocumentHistoryApiDocumentHistoryKindKeyGet(rec, taskReq(t, http.MethodGet,
		"/api/document-history/global_context/global", nil, "owner", "owner"), "global_context", "global")
	if rec.Code < 500 {
		t.Fatalf("listing a corrupt revision = %d %s, want a server error rather than a shorter list",
			rec.Code, rec.Body.String())
	}
}

func TestRestoreDocumentHistoryRefusesARevisionOfAnotherDocument(t *testing.T) {
	api := newTasksTestServer(t)
	owner := func(method, path string, body any) *http.Request {
		return taskReq(t, method, path, body, "owner", "owner")
	}
	for _, text := range []string{"one", "two"} {
		rec := httptest.NewRecorder()
		api.HandleReplaceGlobalContextApiGlobalContextPost(rec,
			owner(http.MethodPost, "/api/global-context", map[string]any{"text": text}))
		if rec.Code != http.StatusOK {
			t.Fatalf("seed write %q: %d %s", text, rec.Code, rec.Body.String())
		}
	}
	stored, err := api.dal.ListDocumentHistory("global_context", "global")
	if err != nil || len(stored) == 0 {
		t.Fatalf("seed history = %+v, %v", stored, err)
	}
	globalRevision := stored[0].ID

	// Same id, addressed under a different kind and under a different key of
	// the same kind: neither may resolve to the global-context revision.
	for _, addr := range []struct{ kind, key string }{
		{"role_definition", seedRoleAssistant},
		{"global_context", "not-the-global-document"},
	} {
		rec := httptest.NewRecorder()
		api.HandleRestoreDocumentHistoryApiDocumentHistoryKindKeyIdRestorePost(rec,
			owner(http.MethodPost, "/api/document-history/"+addr.kind+"/"+addr.key+"/restore", nil),
			addr.kind, addr.key, globalRevision)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("restore %s/%s with a foreign revision id = %d %s, want 404",
				addr.kind, addr.key, rec.Code, rec.Body.String())
		}
	}
	current, err := api.foldUserContextDTO()
	if err != nil {
		t.Fatal(err)
	}
	if current.Text != "two" {
		t.Fatalf("a refused restore changed the live document to %q", current.Text)
	}
}

// Every capped document, one case per capped FIELD: the task manual carries two
// of them (learnings and sop_md) behind a single check, so a case that only
// exercised one would leave the other free to revive an over-cap document.
func TestRestoreDocumentHistoryRefusesToReviveAnOverCapRevision(t *testing.T) {
	oversized := strings.Repeat("x", contextDocMaxCharsDefault+50)
	const role, taskType = seedRoleAssistant, "tm-cap"

	for _, doc := range []struct {
		name string
		// seed stores an over-cap document (it predates the cap — the cap never
		// truncates what is already stored), then shrinks it through the real
		// write face, which retains the over-cap version. It returns the
		// document's history address and the live text that must survive.
		seed func(*testing.T, *apiServer) (kind, key string)
		live func(*testing.T, *apiServer) string
	}{
		{
			name: "lessons",
			seed: func(t *testing.T, api *apiServer) (string, string) {
				if err := api.dal.PutLessons(Lessons{RoleKey: role, TaskType: taskType, Text: oversized}); err != nil {
					t.Fatal(err)
				}
				rec := httptest.NewRecorder()
				api.HandleReplaceLessonsApiLessonsRoleKeyTaskTypePost(rec, taskReq(t, http.MethodPost,
					"/api/lessons/"+role+"/"+taskType, map[string]any{"text": "short again"}, "owner", "owner"),
					role, taskType)
				if rec.Code != http.StatusOK {
					t.Fatalf("shrinking write: %d %s", rec.Code, rec.Body.String())
				}
				return "lessons", role + "::" + taskType
			},
			live: func(t *testing.T, api *apiServer) string {
				current, err := api.foldLessonsDTO(role, taskType)
				if err != nil {
					t.Fatal(err)
				}
				return current.Text
			},
		},
		{
			name: "task manual learnings",
			seed: func(t *testing.T, api *apiServer) (string, string) {
				return seedOverCapManual(t, api, TaskManual{Learnings: oversized},
					map[string]any{"learnings": "short again"})
			},
			live: func(t *testing.T, api *apiServer) string { return liveManual(t, api).Learnings },
		},
		{
			name: "task manual sop_md",
			seed: func(t *testing.T, api *apiServer) (string, string) {
				return seedOverCapManual(t, api, TaskManual{SopMD: oversized},
					map[string]any{"sop_md": "short again"})
			},
			live: func(t *testing.T, api *apiServer) string { return liveManual(t, api).SopMD },
		},
	} {
		t.Run(doc.name, func(t *testing.T) {
			api := newTasksTestServer(t)
			kind, key := doc.seed(t, api)
			stored, err := api.dal.ListDocumentHistory(kind, key)
			if err != nil || len(stored) == 0 {
				t.Fatalf("history = %+v, %v", stored, err)
			}

			rec := httptest.NewRecorder()
			api.HandleRestoreDocumentHistoryApiDocumentHistoryKindKeyIdRestorePost(rec, taskReq(t, http.MethodPost,
				"/api/document-history/"+kind+"/"+key+"/restore", nil, "owner", "owner"),
				kind, key, stored[0].ID)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("restoring an over-cap %s revision = %d %s, want 400 — restore may not do what "+
					"the write faces refuse", doc.name, rec.Code, rec.Body.String())
			}
			// Which 400: the cap's, not some other refusal that happens to share it.
			if !strings.Contains(rec.Body.String(), "size limit") {
				t.Fatalf("%s refusal = %s, want the document size limit", doc.name, rec.Body.String())
			}
			if live := doc.live(t, api); live != "short again" {
				t.Fatalf("refused %s restore still wrote: live text is %d chars", doc.name, len(live))
			}
		})
	}
}

const overCapManualKey = "tm-over-cap"

func seedOverCapManual(t *testing.T, api *apiServer, oversized TaskManual, shrink map[string]any) (string, string) {
	t.Helper()
	oversized.TypeKey, oversized.DisplayName = overCapManualKey, "Over cap"
	oversized.Fields, oversized.Assignee, oversized.UpdatedTS = "[]", "{}", nowSecs()
	if err := api.dal.PutTaskManual(oversized); err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	api.HandleUpdateTaskManualApiTaskManualsTypeKeyPost(rec, taskReq(t, http.MethodPost,
		"/api/task-manuals/"+overCapManualKey, shrink, "owner", "owner"), overCapManualKey)
	if rec.Code != http.StatusOK {
		t.Fatalf("shrinking write: %d %s", rec.Code, rec.Body.String())
	}
	return "task_manual", overCapManualKey
}

func liveManual(t *testing.T, api *apiServer) TaskManual {
	t.Helper()
	m, err := api.dal.GetTaskManual(overCapManualKey)
	if err != nil || m == nil {
		t.Fatalf("read manual: %+v, %v", m, err)
	}
	return *m
}

func TestDocumentHistoryRoutesRefuseUnknownKindsAndBlankKeys(t *testing.T) {
	api := newTasksTestServer(t)
	for _, addr := range []struct{ kind, key string }{
		{"member_avatar", "m-1"}, // a real document, but not one this feature versions
		{"global_context", ""},
		{"lessons", seedRoleAssistant},        // no task type — addresses nothing
		{"lessons", "::" + seedRoleAssistant}, // blank role half
		{"lessons", seedRoleAssistant + "::"}, // blank task-type half
	} {
		rec := httptest.NewRecorder()
		api.HandleListDocumentHistoryApiDocumentHistoryKindKeyGet(rec, taskReq(t, http.MethodGet,
			"/api/document-history/"+addr.kind+"/"+addr.key, nil, "owner", "owner"), addr.kind, addr.key)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("list %q/%q = %d %s, want 400", addr.kind, addr.key, rec.Code, rec.Body.String())
		}
	}
}

// Reading history is open to every authenticated caller; restoring is a write,
// so it takes the same floor as writing the document itself.
func TestRestoreDocumentHistoryKeepsEachDocumentsWriteFloor(t *testing.T) {
	api := newTasksTestServer(t)
	rec := httptest.NewRecorder()
	api.HandleReplaceGlobalContextApiGlobalContextPost(rec, taskReq(t, http.MethodPost,
		"/api/global-context", map[string]any{"text": "one"}, "owner", "owner"))
	if rec.Code != http.StatusOK {
		t.Fatalf("seed write: %d %s", rec.Code, rec.Body.String())
	}
	rec = httptest.NewRecorder()
	api.HandleReplaceGlobalContextApiGlobalContextPost(rec, taskReq(t, http.MethodPost,
		"/api/global-context", map[string]any{"text": "two"}, "owner", "owner"))
	if rec.Code != http.StatusOK {
		t.Fatalf("second write: %d %s", rec.Code, rec.Body.String())
	}
	stored, err := api.dal.ListDocumentHistory("global_context", "global")
	if err != nil || len(stored) == 0 {
		t.Fatalf("history = %+v, %v", stored, err)
	}

	// A plain agent may READ the history of the global context …
	rec = httptest.NewRecorder()
	api.HandleListDocumentHistoryApiDocumentHistoryKindKeyGet(rec, taskReq(t, http.MethodGet,
		"/api/document-history/global_context/global", nil, "m-agent", "agent"), "global_context", "global")
	if rec.Code != http.StatusOK {
		t.Fatalf("agent list = %d %s, want 200", rec.Code, rec.Body.String())
	}
	var visible []DocumentHistoryDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &visible); err != nil {
		t.Fatal(err)
	}
	if len(visible) == 0 {
		t.Fatal("agent read an empty history — the positive half of this test proves nothing")
	}

	// … but not restore it: replacing the global context is governance.
	rec = httptest.NewRecorder()
	api.HandleRestoreDocumentHistoryApiDocumentHistoryKindKeyIdRestorePost(rec, taskReq(t, http.MethodPost,
		"/api/document-history/global_context/global/restore", nil, "m-agent", "agent"),
		"global_context", "global", stored[0].ID)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("agent restore of the global context = %d %s, want 403", rec.Code, rec.Body.String())
	}
	current, err := api.foldUserContextDTO()
	if err != nil {
		t.Fatal(err)
	}
	if current.Text != "two" {
		t.Fatalf("a forbidden restore still wrote: live text = %q", current.Text)
	}
}

// Every restore must fan a delta on the SAME topic the document's ordinary
// writes use, or a cockpit that reconciles on SSE keeps showing the version it
// just replaced. A topic outside the closed vocabulary is dropped at the
// publish seam, silently, with no error anywhere.
func TestRestoreDocumentHistoryFansTheDocumentsOwnTopic(t *testing.T) {
	for _, tc := range []struct{ name, kind, key, topic string }{
		{"global context", "global_context", "global", "global_context"},
		{"role definition", "role_definition", seedRoleAssistant, "role_def"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			api := newTasksTestServer(t)
			// Two writes so there is a revision to restore.
			switch tc.kind {
			case "global_context":
				for _, text := range []string{"one", "two"} {
					rec := httptest.NewRecorder()
					api.HandleReplaceGlobalContextApiGlobalContextPost(rec, taskReq(t, http.MethodPost,
						"/api/global-context", map[string]any{"text": text}, "owner", "owner"))
					if rec.Code != http.StatusOK {
						t.Fatalf("seed write: %d %s", rec.Code, rec.Body.String())
					}
				}
			case "role_definition":
				for _, definition := range []string{"one", "two"} {
					rec := httptest.NewRecorder()
					api.HandleUpdateRoleApiRolesRolePost(rec, taskReq(t, http.MethodPost,
						"/api/roles/"+tc.key, map[string]any{"definition_md": definition}, "owner", "owner"), tc.key)
					if rec.Code != http.StatusOK {
						t.Fatalf("seed write: %d %s", rec.Code, rec.Body.String())
					}
				}
			}
			stored, err := api.dal.ListDocumentHistory(tc.kind, tc.key)
			if err != nil || len(stored) == 0 {
				t.Fatalf("history = %+v, %v", stored, err)
			}

			listener, err := api.hub.Connect("", "")
			if err != nil {
				t.Fatal(err)
			}
			rec := httptest.NewRecorder()
			api.HandleRestoreDocumentHistoryApiDocumentHistoryKindKeyIdRestorePost(rec, taskReq(t, http.MethodPost,
				"/api/document-history/"+tc.kind+"/"+tc.key+"/restore", nil, "owner", "owner"),
				tc.kind, tc.key, stored[0].ID)
			if rec.Code != http.StatusOK {
				t.Fatalf("restore: %d %s", rec.Code, rec.Body.String())
			}
			frame := listener.pop()
			if len(frame) == 0 {
				t.Fatalf("restoring %s fanned no SSE frame at all", tc.kind)
			}
			_, envelope := parseSSEFrame(t, frame)
			if envelope["topic"] != tc.topic {
				t.Fatalf("restore fanned topic %v, want %q — the cockpit listens on that one",
					envelope["topic"], tc.topic)
			}
		})
	}
}
