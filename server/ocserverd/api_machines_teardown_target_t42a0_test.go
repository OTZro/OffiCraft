// api_machines_teardown_target_t42a0_test.go — the sentinels for T-42a0:
// POST /api/machines/{machine_id}/teardown-here must refuse to act when it is
// pointed at a machine that is not this server host.
//
// THE DEFECT
// ----------
// runWardenTeardownHere builds `ocwarden teardown [--canonical]` with an env
// addressed by HOME / uid / OC_NAMESPACE. There is no machine selector on that
// path and the handler never passed one: {machine_id} was read ONLY to look up
// a roster row and then to soft-delete it. So `POST /api/machines/m-box/
// teardown-here` tore down THIS server host's warden and marked m-box removed
// — one click, two live daemons wrecked, and (since T-9cf8) m-box's
// credentials revoked as well. The MCP tool that drives it advertised "tear
// this machine's warden down", i.e. exactly the misuse.
//
// SAFETY OF THESE TESTS (read before adding a case here)
// ------------------------------------------------------
// A test for a guard must not depend on that guard for its own safety: the
// only honest way to check a guard is to delete it and watch this file go red,
// and at that moment the handler runs the subprocess. So the system operation
// is behind a seam in BOTH directions:
//   - the package default in the test binary is refuseToExecOcwarden
//     (TestMain, update_check_test.go) — the real exec is never wired in;
//   - every case here additionally binds withRecordedOcwarden, so the
//     mutant-run assertion "zero ocwarden invocations" is observable.
//
// Direct calls to execOcwarden from any _test.go file: zero, by construction.
package main

import (
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
)

// newForeignTeardownServer stands up a server holding the server-local machine
// (as dbseed.go seeds it) plus one ordinary remote warden, with both the binary
// resolution and the exec seam faked.
func newForeignTeardownServer(t *testing.T) (*apiServer, *[]recordedOcwardenRun) {
	t.Helper()
	s := newMachinesTestServer(t)
	s.binCacheDir = filepath.Join(t.TempDir(), "cache-bin")
	s.ocwardenFS = fstest.MapFS{"ocwarden": {Data: []byte("fake warden — never exec'd")}}
	// exit 0 = a teardown that WOULD confirm and WOULD soft-delete: the fake is
	// deliberately the most destructive one, so a missing guard cannot hide
	// behind an unconfirmed run.
	runs := withRecordedOcwarden(t, 0)
	putTestMember(t, s, Member{
		ID: ServerSelfHost, Name: "this server", Kind: KindWarden, Effort: "medium",
		DesiredState: DesiredStateOffline, RosterStatus: RosterStatusActive,
	})
	putTestMember(t, s, Member{
		ID: "m-remote", Name: "remote", Kind: KindWarden, Effort: "medium",
		DesiredState: DesiredStateOffline, RosterStatus: RosterStatusActive,
	})
	return s, runs
}

// TestTeardownHere_NamingAnotherMachineIsRefused is the primary sentinel.
//
// MUTANT (this is how the guard is verified): make teardownHereRefusal return
// "" for a non-self target. Then this test goes red on all three of its
// independent claims — the 409, the "nothing was executed" claim, and the
// roster row surviving — because the handler falls through to the subprocess
// and the CONFIRM-THEN-REMOVE write. Nothing real is destroyed while that
// mutant runs: the fake runner absorbs it.
//
// The guard is ONE branch, not two: an earlier shape wrote the two refusals as
// consecutive ifs, which made the second condition provably always true (an
// independent review replaced it with `if true` and nothing went red). See
// teardownHereRefusal.
func TestTeardownHere_NamingAnotherMachineIsRefused(t *testing.T) {
	s, runs := newForeignTeardownServer(t)

	rec := postTeardownHereFor(t, s, "m-remote")

	if rec.Code != http.StatusConflict {
		t.Fatalf("teardown-here aimed at another machine: want 409, got %d %s "+
			"— the verb cannot reach that host, so a 200 means it tore down THIS "+
			"host instead and reported success", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"code":"conflict"`) {
		t.Fatalf("refusal must ride the conflict envelope, got %s", rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "m-remote") {
		t.Fatalf("the refusal must name the machine it was asked about, got %s",
			rec.Body.String())
	}
	// The guard has to precede the subprocess. A 409 written after the local
	// daemon was booted out would satisfy the status assertion and still be the
	// whole incident.
	if len(*runs) != 0 {
		t.Fatalf("a refused teardown must never exec ocwarden, got %d run(s): %+v "+
			"— every one of those would have hit THIS host's launchd job",
			len(*runs), *runs)
	}
	// The named machine must be untouched: the pre-T-42a0 behaviour marked it
	// removed, which under T-9cf8 also revokes its credentials.
	m, err := s.dal.GetMember("m-remote")
	if err != nil || m == nil {
		t.Fatalf("get m-remote: %v", err)
	}
	if m.RosterStatus != RosterStatusActive {
		t.Fatalf("a refused teardown must not touch the named machine's roster row, "+
			"got roster=%q", m.RosterStatus)
	}
	// ...and so must the server-local one, which is what actually would have
	// been torn down.
	self, err := s.dal.GetMember(ServerSelfHost)
	if err != nil || self == nil {
		t.Fatalf("get server-self: %v", err)
	}
	if self.RosterStatus != RosterStatusActive {
		t.Fatalf("the server-local machine must be untouched, got roster=%q",
			self.RosterStatus)
	}
}

// TestTeardownHere_RefusalPointsAtTheRealPathAndOffersNoBypass is DoD (c): a
// refusal that reads like an obstacle gets routed around. This one has to send
// the caller to the verbs that actually do what they meant, and must not hint
// that some flag, parameter or alternate route would let this endpoint through.
func TestTeardownHere_RefusalPointsAtTheRealPathAndOffersNoBypass(t *testing.T) {
	s, _ := newForeignTeardownServer(t)
	body := postTeardownHereFor(t, s, "m-remote").Body.String()

	// The two legitimate destinations, both of which really do act on another
	// machine (uninstall is executed by the TARGET's own warden).
	//
	// The route strings are checked against the ACTUAL route table below rather
	// than only as substrings: an independent review caught this message naming
	// `/api/machines/{machine_id}/uninstall` when the real path parameter is
	// `{member_id}`. A substring assertion cannot see that — it happily pins
	// whatever typo the message already contains — and the entire value of this
	// message is that its directions are precise.
	for _, want := range []string{
		"POST /api/machines/{member_id}/uninstall",
		"DELETE /api/machines/{member_id}",
		"install_warden_on_server_host",
		"install --force",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("the refusal must name %q so the caller has somewhere to go; got %s",
				want, body)
		}
	}
	routed := map[string]bool{}
	for _, spec := range routeSpecs(&ServerInterfaceWrapper{}) {
		routed[spec.Method+" "+spec.Path] = true
	}
	for _, cited := range []string{
		"POST /api/machines/{member_id}/uninstall",
		"DELETE /api/machines/{member_id}",
	} {
		if !routed[cited] {
			t.Fatalf("the refusal cites %q, which is not a route this server serves "+
				"— a refusal whose directions do not resolve sends the caller in a "+
				"circle; route table has no such method+path", cited)
		}
	}
	// Vocabulary that would turn the refusal into instructions for defeating it.
	// `install --force` is asserted above and is NOT such a hint: it is a
	// different endpoint's normal mode of operation, not a way past this check.
	for _, forbidden := range []string{
		"bypass", "override", "skip the check", "disable the check",
		"ignore this", "force=", "?force", "unsafe", "confirm=",
	} {
		if strings.Contains(strings.ToLower(body), forbidden) {
			t.Fatalf("the refusal must not teach a way around itself, found %q in: %s",
				forbidden, body)
		}
	}
}

// TestTeardownHere_UnknownMachineStillResolvesFirst keeps the guard from
// swallowing the 404 the black-box conformance suite asserts: that suite only
// ever aims this route at an unknown machine id (test_auth_matrix.py
// "m-conf-missing"), so if this guard ran before resolveMachine, a hidden
// suite would go red instead of this package.
func TestTeardownHere_UnknownMachineStillResolvesFirst(t *testing.T) {
	s, runs := newForeignTeardownServer(t)

	rec := postTeardownHereFor(t, s, "m-conf-missing")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("an unknown machine id must still be a 404 (conformance "+
			"test_auth_matrix.py pins it), got %d %s", rec.Code, rec.Body.String())
	}
	if len(*runs) != 0 {
		t.Fatalf("an unresolved target must never exec ocwarden, got %d", len(*runs))
	}
}

// TestTeardownHere_ServerLocalRefusalIsUnchanged is the collateral guard. The
// T-9cf8 refusal and this one are different sentences for different reasons,
// and they must not collapse into each other: a caller told "cannot reach
// m-remote" when they aimed at the server host would go looking for the wrong
// fix. (The T-9cf8 behaviour itself is owned by
// TestTeardownHereRefusesTheServerLocalMachine in
// api_auth_machine_revoke_test.go; this only pins that T-42a0 did not shadow it.)
func TestTeardownHere_ServerLocalRefusalIsUnchanged(t *testing.T) {
	s, runs := newForeignTeardownServer(t)

	rec := postTeardownHereFor(t, s, ServerSelfHost)
	if rec.Code != http.StatusConflict {
		t.Fatalf("server-self teardown-here: want 409, got %d %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), serverSelfUndeletableMsg) {
		t.Fatalf("server-self must still get the T-9cf8 sentence %q, got %s",
			serverSelfUndeletableMsg, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "cannot reach") {
		t.Fatalf("the T-42a0 refusal must not shadow the T-9cf8 one — they send the "+
			"caller to different places; got %s", rec.Body.String())
	}
	if len(*runs) != 0 {
		t.Fatalf("a refused teardown must never exec ocwarden, got %d", len(*runs))
	}
}

// TestTeardownHere_CoreStillSpellsItsOwnTarget keeps T-2257 alive at the level
// where it is still true. The argv/env contract (`--canonical` on a canonical
// instance, OC_NAMESPACE and no flag on a namespaced one) belongs to
// runWardenTeardownHere, and that function is exactly what the handler no
// longer reaches. Driving the core directly is not a workaround: it is the
// honest home for a contract about how this host addresses ITSELF, which was
// always the only thing this code path could express.
func TestTeardownHere_CoreStillSpellsItsOwnTarget(t *testing.T) {
	t.Run("canonical instance", func(t *testing.T) {
		s, runs := newForeignTeardownServer(t)
		s.namespace = ""
		s.runWardenTeardownHere("/fake/ocwarden")
		if len(*runs) != 1 {
			t.Fatalf("want exactly one ocwarden invocation, got %d", len(*runs))
		}
		if got := strings.Join((*runs)[0].args, " "); got != "teardown --canonical" {
			t.Fatalf("canonical argv = %q, want %q — a bare `teardown` is REFUSED by "+
				"the CLI (exit 1)", got, "teardown --canonical")
		}
		if ns, ok := envValue((*runs)[0].env, "OC_NAMESPACE"); ok && ns != "" {
			t.Fatalf("canonical teardown must not export OC_NAMESPACE, got %q", ns)
		}
	})

	t.Run("namespaced instance", func(t *testing.T) {
		s, runs := newForeignTeardownServer(t)
		s.namespace = "e2eproof"
		s.runWardenTeardownHere("/fake/ocwarden")
		if len(*runs) != 1 {
			t.Fatalf("want exactly one ocwarden invocation, got %d", len(*runs))
		}
		if got := strings.Join((*runs)[0].args, " "); got != "teardown" {
			t.Fatalf("namespaced argv = %q, want a bare %q — the CLI refuses "+
				"--canonical together with OC_NAMESPACE", got, "teardown")
		}
		ns, ok := envValue((*runs)[0].env, "OC_NAMESPACE")
		if !ok || ns != "e2eproof" {
			t.Fatalf("namespaced teardown must export OC_NAMESPACE=e2eproof, got %q "+
				"(present=%v)", ns, ok)
		}
	})
}

// TestTeardownHere_TestBinaryNeverWiresTheRealExec is the sentinel for the
// safety property this whole file rests on (see the header): the mutant runs
// that verify the guards above execute the handler with the guards gone, so the
// package default must not be the real system operation.
//
// MUTANT: drop the `runOcwarden = refuseToExecOcwarden` line from TestMain →
// red. Nothing here calls execOcwarden; it compares function identity.
func TestTeardownHere_TestBinaryNeverWiresTheRealExec(t *testing.T) {
	exit, log, timedOut := runOcwarden("/fake/ocwarden", []string{"teardown"}, nil)
	if exit == 0 || timedOut || !strings.Contains(log, "refuseToExecOcwarden") {
		t.Fatalf("the package default runner in the TEST BINARY must be the refusing "+
			"fake, got exit=%d timedOut=%v log=%q — with the real execOcwarden wired "+
			"in, any test that forgets withRecordedOcwarden boots this machine's own "+
			"warden out of launchd", exit, timedOut, log)
	}
}
