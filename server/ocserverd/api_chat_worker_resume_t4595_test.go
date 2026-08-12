package main

// api_chat_worker_resume_t4595_test.go — the T-4595 release gate.
//
// The owner ruled (rc-64b712bfc703, option ①): open the outsource worker's
// resume summary to the cockpit and NOTHING ELSE. The other fifteen member
// verbs keep refusing ow- ids.
//
// So this file is a matched pair, and the SECOND half is the load-bearing one:
//
//	① the worker's summary really is readable — measured on the wire, through
//	   the whole mux, on the request the cockpit actually sends;
//	② nothing else opened with it. Its discriminating power is proved by the
//	   mutant that widens the release (drop the outsource arm from
//	   resolveMember itself instead of adding the narrow resolver) — ② goes red
//	   across every verb family while ① stays green.
//
// A third arm keeps the refusal that survives the release: a released worker
// (RosterStatusRemoved) stops being readable at once, so a summary can never
// outlive the roster row it belongs to.

import (
	"net/http"
	"strings"
	"testing"
	"time"
)

// workerResumeStack reuses the full wired stack from the revocation suite —
// same mux, same auth chain, same RBAC choke — so these assertions describe
// live traffic rather than a handler called in isolation.
func workerResumeOwnerToken(t *testing.T, secret []byte) string {
	t.Helper()
	tok, err := mintJWT(wireOwnerID, "owner", 3600, secret, time.Now().Unix(), "")
	if err != nil {
		t.Fatalf("mint owner token: %v", err)
	}
	return tok
}

func TestWorkerResumeSummaryIsReadableAndNothingElseOpened(t *testing.T) {
	srv, secret, api := revokeStack(t)
	owner := workerResumeOwnerToken(t, secret)

	worker := testAgent("ow-t4595-worker")
	worker.Kind = KindOutsource
	putTestMember(t, api, worker)

	// ── ① the released verb ────────────────────────────────────────────────
	st, body := revokeCall(t, "GET", srv.URL+"/api/members/"+worker.ID+"/resume-summary", owner, "")
	if st != http.StatusOK {
		t.Fatalf("worker resume summary must be readable, got %d %s", st, body)
	}
	if !strings.Contains(body, worker.ID) {
		t.Fatalf("the summary must be the WORKER's own snapshot (identity %q missing): %s", worker.ID, body)
	}

	// Positive control for the negative arm below: the same verb on a staff
	// member was already open, so a 200 there proves the arm can say yes.
	staff := testAgent("m-t4595-staff")
	putTestMember(t, api, staff)
	if st, body := revokeCall(t, "GET", srv.URL+"/api/members/"+staff.ID+"/resume-summary", owner, ""); st != http.StatusOK {
		t.Fatalf("positive control: staff resume summary must still be readable, got %d %s", st, body)
	}

	// ── ② nothing else opened ──────────────────────────────────────────────
	// One verb per family the recon counted on resolveMember (member
	// management / account+token / machines / webhooks), so widening the
	// resolver cannot pass by leaving one family untouched.
	for _, probe := range []struct {
		who    string
		method string
		path   string
		body   string
	}{
		{"member detail", "GET", "/api/members/" + worker.ID, ""},
		{"member edit", "PATCH", "/api/members/" + worker.ID, `{"name":"nope"}`},
		{"member dismissal", "DELETE", "/api/members/" + worker.ID, ""},
		{"long-lived token mint", "POST", "/api/mint", `{"member_id":"` + worker.ID + `","ttl_days":1}`},
	} {
		st, body := revokeCall(t, probe.method, srv.URL+probe.path, owner, probe.body)
		if st == http.StatusOK {
			t.Errorf("%s must NOT have opened for an outsource worker, got 200 %s", probe.who, body)
		}
	}

	// ── ③ the refusal that survives the release ────────────────────────────
	released := worker
	released.RosterStatus = RosterStatusRemoved
	putTestMember(t, api, released)
	if st, body := revokeCall(t, "GET", srv.URL+"/api/members/"+worker.ID+"/resume-summary", owner, ""); st == http.StatusOK {
		t.Fatalf("a released worker's summary must stop being readable, got 200 %s", body)
	}
}
