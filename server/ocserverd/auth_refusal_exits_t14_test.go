package main

// auth_refusal_exits_t14_test.go — T-14 項目 4B, the NEGATIVE half, over EVERY
// exit rather than one sample.
//
// 🔴 WHY THIS IS THE DANGEROUS DIRECTION. X-OC-Auth-Refusal: agent-superseded
// is an instruction to a live process to KILL ITSELF and the tmux + model
// session under it. On the one refusal it belongs to that is correct: the floor
// only ever rises, so that credential can never come back. On ANY OTHER 401 it
// is catastrophic, and the worst one is the most ordinary:
//
//	"missing credentials" is the station-just-restarted 401 — the token is not
//	loaded yet. Put the marker there and every HEALTHY agent on every machine
//	that is mid-restart self-terminates at once, killing the model sessions
//	underneath them. And the suite stays green.
//
// The previous guard sampled ONE ordinary 401 (a garbage token, which reaches
// only the verifyJWT exit) while its comment claimed the mutant "set it on the
// other 401s too" would be caught. It would not have been: marking
// missing-credentials, permanentCredentialRefusal and revocationRefusal all at
// once left the whole suite green.
//
// So this file probes EVERY 401 exit requireAuth has, one request each, and
// asserts by name that none of them carries the marker. It is held EXHAUSTIVE
// by a structural gate: the number of 401 exits is counted out of requireAuth's
// own AST, and a new exit that nobody wrote a probe for is a FATAL, not a
// silent gap.
//
// It also pins the marker to ONE WRITE SITE across the whole server module —
// the runtime probes can only see exits they can reach, and a marker set on a
// 401 outside requireAuth (api_auth.go, the MFA paths, the claim-code paths,
// shareSigGate) would never appear on any request this file makes.

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// refusalExitStack is revokeStack with the two knobs the exit table needs:
// the secret requireAuth is given (empty ⇒ the "auth not configured" exit) and
// ownerIatFloor (non-nil ⇒ the owner revocation exit, which revokeStack wires
// as nil and therefore cannot reach).
func refusalExitStack(t *testing.T, secret []byte, ownerIatFloor func() int64) (*httptest.Server, *apiServer) {
	t.Helper()
	db, err := openSQLite(filepath.Join(t.TempDir(), "refusal-exits.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if err := runMigrations(db); err != nil {
		t.Fatalf("goose up: %v", err)
	}
	dal := NewDAL(db)
	if err := seedOutOfBox(dal); err != nil {
		t.Fatalf("seed: %v", err)
	}
	api := newAPIServer(dal, NewHub(), []byte(interopSecret), 3600, "../..")
	h, err := buildHandler(specsFor(api), secret, dal.GetMember, ownerIatFloor)
	if err != nil {
		t.Fatalf("buildHandler: %v", err)
	}
	api.loopback = h
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return srv, api
}

// probeRefusal makes ONE real request through the whole mux and reports the
// status, the body, and whatever the response says under authRefusalHeader.
func probeRefusal(t *testing.T, url, token string) (int, string, string) {
	t.Helper()
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		t.Fatal(err)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return resp.StatusCode, string(body), resp.Header.Get(authRefusalHeader)
}

// TestAuthRefusalMarker_NoOtherRequireAuthExitCarriesIt walks every 401 exit
// requireAuth has EXCEPT the agent-floor one, and demands each be unmarked.
func TestAuthRefusalMarker_NoOtherRequireAuthExitCarriesIt(t *testing.T) {
	secret := []byte(interopSecret)
	now := time.Now().Unix()

	// The default stack: real secret, an owner floor set far in the future so
	// the owner exit is reachable on demand.
	srv, api := refusalExitStack(t, secret, func() int64 { return now + 86400 })

	// A live, ordinary agent — the subject for the exp-less probe.
	agent := testAgent("m-t14-exit-agent")
	putTestMember(t, api, agent)

	// A warden row the roster has soft-deleted — the revocation exit.
	putTestMember(t, api, Member{
		ID: "m-t14-exit-deadwarden", Name: "dead warden", Kind: machineKind,
		RosterStatus: RosterStatusRemoved, DesiredState: DesiredStateOffline,
	})

	// mint takes the ttl explicitly. It used to hardcode 3600, and that was a
	// live bug in THIS FILE: mintJWT computes exp = iat + ttl, so the
	// "old owner token" probe below (iat now-86400) was minted ALREADY EXPIRED
	// and 401'd at verifyJWT — one exit earlier than the row it was written
	// for. It passed anyway, because a 401 is a 401. The ownerIatFloor control
	// further down is what caught it.
	mint := func(sub, scope string, iat, ttl int64) string {
		t.Helper()
		tok, err := mintJWT(sub, scope, ttl, secret, iat, "")
		if err != nil {
			t.Fatal(err)
		}
		return tok
	}
	// oldOwner is signed and UNEXPIRED, and its iat is below the floor — the
	// only way to land on the ownerIatFloor branch and not on verifyJWT.
	oldOwner := func() string { return mint("owner", "owner", now-86400, 2*86400) }
	mintNoExp := func(sub, scope string) string {
		t.Helper()
		tok, err := mintJWTWithoutExpiry(sub, scope, secret, now, "")
		if err != nil {
			t.Fatal(err)
		}
		return tok
	}

	// A stack whose requireAuth holds NO secret — the one exit that cannot be
	// reached by any token on a configured station.
	unconfigured, _ := refusalExitStack(t, nil, nil)

	type exit struct {
		exit string // the requireAuth branch, by its own body text
		why  string // what a marker here would do to a live, healthy fleet
		url  string
		// body is how the probe PROVES which branch it landed on. Three exits
		// answer the same "invalid token" and cannot be told apart this way;
		// each of those has its own separate control below, because "it 401'd"
		// is not evidence that it 401'd HERE.
		body  string
		token string
	}
	exits := []exit{
		{
			exit: `"auth not configured"`,
			why: "the station is up but has no signing secret yet — every agent " +
				"in the fleet is hitting this at once, and every one of them is healthy",
			url: unconfigured.URL + "/api/members", body: "auth not configured",
			token: mint(agent.ID, "agent", now, 3600),
		},
		{
			exit: `"missing credentials" (no token presented)`,
			why: "THE RESTART EXIT: the agent's token is not loaded yet. A marker " +
				"here self-terminates every healthy agent on every restarting machine, " +
				"and takes each one's model session down with it",
			url: srv.URL + "/api/members", body: "missing credentials", token: "",
		},
		{
			exit: `verifyJWT failed → "invalid token"`,
			why: "an unverifiable token — which is also what a secret rotation in " +
				"flight looks like from here; it resolves on its own, a self-kill does not",
			url: srv.URL + "/api/members", body: "invalid token", token: "not-a-token",
		},
		{
			exit: `ownerIatFloor → "invalid token"`,
			why: "the owner changed their password. That refusal belongs to a human " +
				"at a browser; it must never reach an agent's reconnect loop as a kill order",
			url: srv.URL + "/api/members", body: "invalid token",
			token: oldOwner(),
		},
		{
			exit: `permanentCredentialRefusal → "invalid token"`,
			why: "an exp-less credential that is not an active warden's. It is refused " +
				"for being permanent, not for being superseded — a re-mint fixes it",
			url: srv.URL + "/api/members", body: "invalid token",
			token: mintNoExp(agent.ID, "agent"),
		},
		{
			exit: `revocationRefusal → machine-revoked`,
			why: "the MACHINE was deleted. The agent on it is told to stop by the " +
				"machine lifecycle, not by a credential-generation marker aimed at ocagent",
			url: srv.URL + "/api/members", body: machineRevokedMsg("m-t14-exit-deadwarden"),
			token: mint("m-t14-exit-deadwarden", "agent", now, 3600),
		},
	}

	for _, e := range exits {
		t.Run(e.exit, func(t *testing.T) {
			st, body, mark := probeRefusal(t, e.url, e.token)
			if st != http.StatusUnauthorized {
				t.Fatalf("POSITIVE CONTROL FAILED — this probe was meant to land on "+
					"the %s exit and be a 401; it got %d. The probe, not the product, "+
					"is what needs fixing: an exit nothing reaches is an exit nothing "+
					"guards.", e.exit, st)
			}
			if !strings.Contains(body, e.body) {
				t.Fatalf("this probe was meant to land on the %s exit, but the "+
					"refusal reads %s — it is being refused somewhere EARLIER, so "+
					"the exit it was written for is going unprobed while this row "+
					"looks green. Fix the probe.", e.exit, body)
			}
			if mark != "" {
				t.Fatalf("the %s exit carries %s: %q.\n"+
					"That header is an order to a live ocagent to KILL ITS OWN tmux "+
					"session and the model session under it, and it is only ever "+
					"correct on the agent-iat-floor refusal (which can never resolve). "+
					"Here: %s.",
					e.exit, authRefusalHeader, mark, e.why)
			}
		})
	}

	// The two exits that answer the same "invalid token" as the verifyJWT one
	// need their own proof that they were reached AT ALL — body text cannot
	// tell those three apart, and a probe that quietly fails signature
	// verification would sit in the table forever looking green while the
	// branch it names is never executed. Each control re-sends the SAME token
	// against a stack where only that branch's cause is removed, and demands it
	// stop being a 401.
	if st, body, _ := probeRefusal(t, srv.URL+"/api/members", oldOwner()); st != http.StatusUnauthorized {
		t.Fatalf("setup: the owner-floor probe is not even a 401: %d %s", st, body)
	}
	noFloor, _ := refusalExitStack(t, secret, nil)
	if st, body, _ := probeRefusal(t, noFloor.URL+"/api/members", oldOwner()); st == http.StatusUnauthorized {
		t.Fatalf("CONTROL FAILED — the same owner token is STILL 401 with "+
			"ownerIatFloor removed (%s), so the ownerIatFloor row in the table "+
			"above never reaches the ownerIatFloor branch; it is being refused "+
			"earlier and that exit is unguarded.", body)
	}
	if st, body, _ := probeRefusal(t, srv.URL+"/api/members", mintNoExp(agent.ID, "agent")); st != http.StatusUnauthorized {
		t.Fatalf("setup: the exp-less probe is not even a 401: %d %s", st, body)
	}
	if st, body, _ := probeRefusal(t, srv.URL+"/api/members", mint(agent.ID, "agent", now, 3600)); st == http.StatusUnauthorized {
		t.Fatalf("CONTROL FAILED — the SAME subject with an exp is also 401 (%s), "+
			"so the exp-less row above is not reaching permanentCredentialRefusal "+
			"and that exit is unguarded.", body)
	}

	// EXHAUSTIVENESS. The table above is only "every exit" for as long as
	// requireAuth has exactly the exits it had when the table was written.
	// Count them out of the source rather than trusting this comment.
	got := requireAuth401ExitCount(t)
	if want := len(exits) + 1; got != want { // +1: the marked agent-floor exit
		t.Fatalf("requireAuth now has %d exits that answer 401, and this file "+
			"probes %d of them plus the one marked exit (%d). A 401 exit with no "+
			"probe is exactly the gap this file exists to close — the last one to "+
			"go unprobed was 'missing credentials', where a marker kills every "+
			"healthy agent on a restarting machine. Add a row to `exits` for the "+
			"new branch (or, if you REMOVED one, delete its row).", got, len(exits), want)
	}
}

// requireAuthSource / serverModuleDir locate the code the structural gates read.
const requireAuthSource = "server.go"
const serverModuleDir = "."

// requireAuth401ExitCount counts, out of requireAuth's own AST, the calls that
// answer http.StatusUnauthorized. Counted, not written down: a number in a
// comment is exactly the thing that goes stale when someone adds a branch.
func requireAuth401ExitCount(t *testing.T) int {
	t.Helper()
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, requireAuthSource, nil, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", requireAuthSource, err)
	}
	var fn *ast.FuncDecl
	for _, d := range file.Decls {
		if f, ok := d.(*ast.FuncDecl); ok && f.Name.Name == "requireAuth" && f.Body != nil {
			fn = f
		}
	}
	if fn == nil {
		t.Fatalf("no func requireAuth in %s — the gate this file guards has moved; "+
			"re-point requireAuthSource by hand", requireAuthSource)
	}
	n := 0
	ast.Inspect(fn.Body, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		id, ok := call.Fun.(*ast.Ident)
		if !ok || id.Name != "writeError" {
			return true
		}
		for _, a := range call.Args {
			if sel, ok := a.(*ast.SelectorExpr); ok && sel.Sel.Name == "StatusUnauthorized" {
				n++
			}
		}
		return true
	})
	if n == 0 {
		t.Fatalf("requireAuth answers no 401 through writeError — either the refusal " +
			"shape changed or this counter stopped matching it; re-check by hand " +
			"rather than letting the exhaustiveness check pass on zero")
	}
	return n
}

// TestAuthRefusalMarker_IsWrittenInExactlyOnePlace is the half the runtime
// probes structurally cannot do. There are 401s all over this module —
// api_auth.go, the MFA verify paths, the machine claim-code paths,
// api_settings.go, shareSigGate — and no request made by the test above passes
// through any of them. A marker set on one of those would be invisible to
// every probe and green in every suite.
//
// So: across the whole server module, `Set(authRefusalHeader, …)` may appear
// EXACTLY ONCE, and it must be inside requireAuth.
func TestAuthRefusalMarker_IsWrittenInExactlyOnePlace(t *testing.T) {
	entries, err := os.ReadDir(serverModuleDir)
	if err != nil {
		t.Fatalf("read %s: %v", serverModuleDir, err)
	}
	type site struct{ file, fn string }
	var sites []site
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, filepath.Join(serverModuleDir, name), nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		for _, d := range file.Decls {
			fn, ok := d.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			ast.Inspect(fn.Body, func(node ast.Node) bool {
				call, ok := node.(*ast.CallExpr)
				if !ok {
					return true
				}
				sel, ok := call.Fun.(*ast.SelectorExpr)
				if !ok || sel.Sel.Name != "Set" || len(call.Args) == 0 {
					return true
				}
				if id, ok := call.Args[0].(*ast.Ident); ok && id.Name == "authRefusalHeader" {
					sites = append(sites, site{name, fn.Name.Name})
				}
				return true
			})
		}
	}
	if len(sites) != 1 {
		t.Fatalf("%s is written at %d places in the server module (%v). It must be "+
			"written at exactly ONE, inside requireAuth's agent-iat-floor branch. "+
			"Every other 401 in this module — a station with no secret, an MFA "+
			"failure, a bad claim code, a bad share signature — is a refusal that "+
			"can RESOLVE, and marking one tells a healthy ocagent to kill its own "+
			"tmux and model session instead of retrying. None of the wire probes "+
			"in this file can see a marker set outside requireAuth, which is why "+
			"this is counted from the source.",
			authRefusalHeader, len(sites), sites)
	}
	if sites[0].fn != "requireAuth" {
		t.Fatalf("%s is written in %s (%s), not in requireAuth. The marker means "+
			"'this member's generation is over, stop retrying forever' — a fact only "+
			"the agent-iat-floor branch knows.", authRefusalHeader, sites[0].fn, sites[0].file)
	}
}
