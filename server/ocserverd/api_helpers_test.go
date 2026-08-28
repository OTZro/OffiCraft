package main

// api_helpers_test.go — the two member-API target resolvers.
//
// resolveMember is now the OPEN one: it folds only "no row" and "soft-removed".
// resolveStaffMember is the same plus the kind='outsource' refusal, and the
// verbs that need that refusal ask for it BY NAME (owner ruling 2026-08-28:
// 其他真的要過濾要明確指定).
//
// 🔑 WHY OPENING THE ITEM DOOR GIVES NOTHING AWAY: GET /api/members already
// LISTS outsource rows to the same principal (ListMembersIncludingOutsource,
// the P7 convergence rc-2786636f30e5). The item door refusing what the list
// door hands out was two doors onto one row disagreeing about who may open it —
// and the cockpit paid for that disagreement with one guaranteed 404 plus one
// whole-roster refetch on every contractor chat line.

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestResolveMember_ReadsOutsource pins the widened READ door, and pins the
// write door NEXT TO IT so the pair can never drift into "everything opened".
// Mutant: putting the kind='outsource' arm back into resolveMember → the read
// half goes red; pointing PATCH at resolveMember → the write half goes red.
func TestResolveMember_ReadsOutsource(t *testing.T) {
	api := newTasksTestServer(t)
	api.noOutsource = true
	workerID := assignOneWorker(t, api)

	if _, err := api.resolveMember(workerID); err != nil {
		t.Fatalf("resolveMember(%s) must RESOLVE an outsource row now, got %v",
			workerID, err)
	}
	if _, err := api.resolveStaffMember(workerID); !errors.Is(err, errNotFound) {
		t.Fatalf("resolveStaffMember(%s) must still refuse an outsource row, got %v",
			workerID, err)
	}

	// The read door answers — this is the 404 the cockpit used to eat on every
	// contractor chat line, together with a whole-roster refetch.
	rec := httptest.NewRecorder()
	api.HandleGetMemberApiMembersMemberIdGet(rec,
		taskReq(t, "GET", "/api/members/"+workerID, nil, wireOwnerID, "owner"), workerID)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/members/{ow-}: want 200, got %d %s", rec.Code, rec.Body.String())
	}
	if dto := decodeBody[memberDTO](t, rec); dto.ID != workerID || dto.Kind != KindOutsource {
		t.Fatalf("read DTO = %+v, want the worker's own row", dto)
	}

	// ...and the write door does NOT.
	rec = httptest.NewRecorder()
	api.HandleUpdateMemberApiMembersMemberIdPatch(rec,
		taskReq(t, "PATCH", "/api/members/"+workerID,
			map[string]any{"name": "hijack"}, wireOwnerID, "owner"), workerID)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("PATCH /api/members/{ow-}: want 404, got %d %s", rec.Code, rec.Body.String())
	}
}

// TestStaffOnlyVerbsStillRefuseOutsource is the other half of the ticket, and
// the half nobody would notice breaking: each of these verbs asks for
// resolveStaffMember BY NAME, and a future edit that "tidies" one back to
// resolveMember opens it silently — an agent that can be force-stopped through
// two different funnels, or handed a staff boot document, does not report it.
//
// 🔴 Deliberately a TABLE over the whole set rather than one test per verb: a
// new member verb that copies the wrong resolver is caught by adding one row.
func TestStaffOnlyVerbsStillRefuseOutsource(t *testing.T) {
	api := newTasksTestServer(t)
	api.noOutsource = true
	workerID := assignOneWorker(t, api)

	cases := []struct {
		name string
		call func(*httptest.ResponseRecorder)
	}{
		{"activate", func(rec *httptest.ResponseRecorder) {
			api.HandleActivateMemberApiMembersMemberIdActivatePost(rec,
				taskReq(t, "POST", "/api/members/"+workerID+"/activate", nil, wireOwnerID, "owner"), workerID)
		}},
		{"deactivate", func(rec *httptest.ResponseRecorder) {
			api.HandleDeactivateMemberApiMembersMemberIdDeactivatePost(rec,
				taskReq(t, "POST", "/api/members/"+workerID+"/deactivate", nil, wireOwnerID, "owner"), workerID)
		}},
		{"dismiss", func(rec *httptest.ResponseRecorder) {
			api.HandleDismissMemberApiMembersMemberIdDelete(rec,
				taskReq(t, "DELETE", "/api/members/"+workerID, nil, wireOwnerID, "owner"), workerID)
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			c.call(rec)
			if rec.Code != http.StatusNotFound {
				t.Fatalf("%s on an ow- id must stay 404, got %d %s",
					c.name, rec.Code, rec.Body.String())
			}
		})
	}
}

func TestUpdateMember_RuntimeRoundTripsAndValidates(t *testing.T) {
	api := newTasksTestServer(t)
	if err := api.dal.PutMember(fullMember("mira")); err != nil {
		t.Fatalf("put member: %v", err)
	}
	rec := httptest.NewRecorder()
	api.HandleUpdateMemberApiMembersMemberIdPatch(rec,
		taskReq(t, "PATCH", "/api/members/mira",
			map[string]any{"runtime": RuntimeCodex}, wireOwnerID, "owner"), "mira")
	if rec.Code != http.StatusOK {
		t.Fatalf("Codex PATCH: %d %s", rec.Code, rec.Body.String())
	}
	if got := decodeBody[memberDTO](t, rec).Runtime; got != RuntimeCodex {
		t.Fatalf("runtime = %q, want codex", got)
	}

	rec = httptest.NewRecorder()
	api.HandleUpdateMemberApiMembersMemberIdPatch(rec,
		taskReq(t, "PATCH", "/api/members/mira",
			map[string]any{"runtime": "unknown"}, wireOwnerID, "owner"), "mira")
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("unknown runtime: want 422, got %d %s", rec.Code, rec.Body.String())
	}
}

// TestGetMember_WorkerSelfReadResolves (T-ea82): the ONE exception to the ow-
// 404 — a worker reading its OWN row (the ocagent recycle/wind-down hooks'
// refetch) gets the member DTO, desired_state + refocus_since included; the
// same worker targeting ANOTHER ow- id stays 404. Mutant: dropping the
// self-read fallback in HandleGetMember → the self case 404s (red).
func TestGetMember_WorkerSelfReadResolves(t *testing.T) {
	api := newTasksTestServer(t)
	api.noOutsource = true
	workerID := assignOneWorker(t, api)
	w, _ := api.dal.GetOutsourceWorker(workerID)
	w.RefocusSince = 1234.5
	if err := api.dal.PutOutsourceWorker(*w); err != nil {
		t.Fatalf("stamp refocus: %v", err)
	}

	rec := httptest.NewRecorder()
	api.HandleGetMemberApiMembersMemberIdGet(rec,
		taskReq(t, "GET", "/api/members/"+workerID, nil, workerID, "agent"), workerID)
	if rec.Code != http.StatusOK {
		t.Fatalf("self-read GET /api/members/{ow-}: want 200, got %d %s",
			rec.Code, rec.Body.String())
	}
	dto := decodeBody[memberDTO](t, rec)
	if dto.ID != workerID || dto.Kind != KindOutsource {
		t.Fatalf("self-read DTO = %+v, want the worker's own row", dto)
	}
	if dto.RefocusSince != 1234.5 || dto.DesiredState != DesiredStateOnline {
		t.Fatalf("self-read must expose refocus_since/desired_state (the recycle-hook "+
			"fields), got refocus=%v desired=%q", dto.RefocusSince, dto.DesiredState)
	}

	// Another worker's id from the same token stays the pre-fold 404.
	otherID := "ow-" + newHexID(6)
	if err := api.dal.PutOutsourceWorker(OutsourceWorker{
		ID: otherID, Codename: "S-" + otherID, TaskID: "t-x",
		Status: WorkerStatusAssigned, DesiredState: DesiredStateOnline,
	}); err != nil {
		t.Fatalf("seed other worker: %v", err)
	}
	// 🔴 This used to assert 404 and now asserts 200 — a DELIBERATE change, not
	// a relaxed test. Reading another agent's roster row was never contained by
	// this door: GET /api/members lists every row, outsource included, to the
	// same principal. The item door refusing it only cost the cockpit a wasted
	// request; it withheld nothing.
	rec = httptest.NewRecorder()
	api.HandleGetMemberApiMembersMemberIdGet(rec,
		taskReq(t, "GET", "/api/members/"+otherID, nil, workerID, "agent"), otherID)
	if rec.Code != http.StatusOK {
		t.Fatalf("cross-worker GET is a read and now resolves, got %d %s",
			rec.Code, rec.Body.String())
	}
}
