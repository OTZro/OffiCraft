package main

// agent_iat_floor_t14_test.go — T-14 項目 4B, ONE invariant:
//
//	the moment a member's NEW session says it is up (POST /api/self/waking),
//	every credential minted for an EARLIER session of that same member stops
//	working.
//
// 🔴 WHY. A member's generations overlap on purpose: the replacement boots and
// connects while the outgoing session is still working its close-out. Every
// server-side effect keyed on the MEMBER id therefore cannot tell which
// generation is speaking — the outgoing session's last words land on its
// successor. The owner's ruling (2026-08-30, rc-fe6451abe579, option 1) is that
// the NEW generation coming up is what ends the old one's authority: 「新的一輪
// 一上線就失效」, with a handover cut in half being the knowingly accepted cost.
//
// The discriminator is the caller's own credential. Every generation boots on a
// token minted for THAT spawn, so its `iat` names the generation speaking.
// report_waking stamps the CALLER'S OWN iat as member.agent_iat_floor, and
// requireAuth refuses any agent-scope token whose iat is STRICTLY LESS THAN it.
// Own-iat rather than now() is what keeps the stamping session from locking
// itself out; strictly-less-than is what makes that exact.
//
// ⚠️ NOT SOLVED, and deliberately so (owner 2026-08-28: 「先不管搶同一秒」):
// `iat` is whole seconds, so two sessions of one member that start inside the
// SAME second are indistinguishable to this gate. Nothing below tests that case
// because nothing below fixes it.
//
// Everything runs against a temp sqlite + httptest server, on the whole wired
// stack (requireAuth → RBAC choke → handler), with tokens the server itself
// minted. Nothing here touches a real machine, warden, or agent.

import (
	"net/http"
	"testing"
	"time"
)

// wakeWith calls the REAL POST /api/self/waking over the wire with `token`,
// which is the only way the floor is ever stamped in production.
func wakeWith(t *testing.T, srvURL, token string) {
	t.Helper()
	st, body := revokeCall(t, "POST", srvURL+"/api/self/waking", token, `{}`)
	if st != http.StatusOK {
		t.Fatalf("POST /api/self/waking: want 200, got %d %s", st, body)
	}
}

// ---------------------------------------------------------------------------
// ① the superseded generation's token is refused
// ---------------------------------------------------------------------------

// TestAgentIatFloor_SupersededSessionTokenIsRefused is the deny half. The
// positive control is inside the test: the SAME token on the SAME request is
// asserted 200 before the new session wakes, so a test that always refused (or
// a probe that never worked) cannot pass.
//
// Mutant: delete the agentIatFloorRefusal call in requireAuth → both AFTER arms
// go back to 200 and this test is red.
func TestAgentIatFloor_SupersededSessionTokenIsRefused(t *testing.T) {
	srv, secret, api := revokeStack(t)
	agent := testAgent("m-t14-superseded")
	putTestMember(t, api, agent)

	now := time.Now().Unix()
	// Generation N booted ten minutes ago; generation N+1 booted just now.
	oldTok, err := mintJWT(agent.ID, "agent", 3600, secret, now-600, "")
	if err != nil {
		t.Fatal(err)
	}
	newTok, err := mintJWT(agent.ID, "agent", 3600, secret, now, "")
	if err != nil {
		t.Fatal(err)
	}

	// BEFORE — the positive control. Nothing below means anything if these fail.
	for _, c := range []liveCall{
		{"the outgoing session reading the roster", "GET", "/api/members", ""},
		{"the outgoing session's close-out report", "POST", "/api/self/stopped", `{}`},
	} {
		if st, body := revokeCall(t, c.method, srv.URL+c.path, oldTok, c.body); st != http.StatusOK {
			t.Fatalf("POSITIVE CONTROL FAILED — %s must be 200 before the replacement "+
				"wakes, got %d %s", c.who, st, body)
		}
	}

	// …and now the replacement reports that it is up.
	wakeWith(t, srv.URL, newTok)

	// AFTER — the same token, the same requests.
	for _, c := range []liveCall{
		{"the superseded session reading the roster", "GET", "/api/members", ""},
		{"the superseded session's close-out report", "POST", "/api/self/stopped", `{}`},
	} {
		st, body := revokeCall(t, c.method, srv.URL+c.path, oldTok, c.body)
		if st != http.StatusUnauthorized {
			t.Fatalf("%s: a credential minted for a generation this member has "+
				"already replaced must be 401 once the replacement has reported "+
				"waking, got %d %s", c.who, st, body)
		}
	}
}

// ---------------------------------------------------------------------------
// ② nothing else is refused — the load-bearing half
// ---------------------------------------------------------------------------

// TestAgentIatFloor_TheLiveGenerationAndItsNeighboursStillWork is the
// collateral-damage guard. A floor that refused everyone would pass ① and be
// worthless. Three separate arms, each one a way of getting this wrong:
//
//	a) the session that STAMPED the floor is not locked out by its own stamp —
//	   the reason report_waking stores the caller's own iat and not now();
//	b) a token minted AFTER the floor for the same member works — the gate is a
//	   floor, not an allow-list of one;
//	c) another member's older token is untouched — the floor is PER MEMBER, and
//	   a version keyed on one global number would fail here.
func TestAgentIatFloor_TheLiveGenerationAndItsNeighboursStillWork(t *testing.T) {
	srv, secret, api := revokeStack(t)
	live := testAgent("m-t14-live")
	putTestMember(t, api, live)
	bystander := testAgent("m-t14-bystander")
	putTestMember(t, api, bystander)

	now := time.Now().Unix()
	liveTok, err := mintJWT(live.ID, "agent", 3600, secret, now, "")
	if err != nil {
		t.Fatal(err)
	}
	// Minted a minute after the one that stamps the floor.
	laterTok, err := mintJWT(live.ID, "agent", 3600, secret, now+60, "")
	if err != nil {
		t.Fatal(err)
	}
	// The bystander's token is OLDER than the floor the other member sets.
	bystanderTok, err := mintJWT(bystander.ID, "agent", 3600, secret, now-600, "")
	if err != nil {
		t.Fatal(err)
	}

	wakeWith(t, srv.URL, liveTok)

	for who, token := range map[string]string{
		"the session that stamped the floor with its own iat": liveTok,
		"a token minted after the floor for the same member":  laterTok,
		"another member's older token":                        bystanderTok,
	} {
		if st, body := revokeCall(t, "GET", srv.URL+"/api/members", token, ""); st != http.StatusOK {
			t.Fatalf("%s must still be 200, got %d %s", who, st, body)
		}
	}
	// And the live session's own close-out report is still collected — the
	// T-2123 hole (an agent that says it is finished staying online forever)
	// must not be re-opened by this gate.
	if st, body := revokeCall(t, "POST", srv.URL+"/api/self/stopped", liveTok, `{}`); st != http.StatusOK {
		t.Fatalf("the live session's own close-out report must still be 200, got %d %s", st, body)
	}
}

// TestAgentIatFloor_TheWakingCallerIsNeverLockedOutByItsOwnStamp is the arm
// that separates "the caller's own iat" from "now()" — the ② arms above cannot,
// because a token minted a moment before the wake sits either side of the
// server clock by fractions of a second.
//
// The gap here is FIVE MINUTES and it is the realistic one: a token is minted
// when the START is dispatched, and the agent's process has to launch, load its
// boot document and reach report_waking before it is used. A floor taken from
// the server clock at that moment sits five minutes ABOVE the caller's own iat,
// and the very first thing that agent does after reporting itself awake is 401.
//
// Mutant: stamp nowSecs() instead of the caller's iat in stampAgentIatFloor →
// this test is red, and it is the only one that reliably is.
func TestAgentIatFloor_TheWakingCallerIsNeverLockedOutByItsOwnStamp(t *testing.T) {
	srv, secret, api := revokeStack(t)
	agent := testAgent("m-t14-slowboot")
	putTestMember(t, api, agent)

	// Minted five minutes before this session got as far as reporting itself up.
	tok, err := mintJWT(agent.ID, "agent", 3600, secret, time.Now().Unix()-300, "")
	if err != nil {
		t.Fatal(err)
	}
	wakeWith(t, srv.URL, tok)

	if st, body := revokeCall(t, "GET", srv.URL+"/api/members", tok, ""); st != http.StatusOK {
		t.Fatalf("the token that REPORTED the wake must still work after it: a floor "+
			"taken from the server clock rather than from this caller's own iat puts "+
			"the whole mint-to-boot gap above it, so the session locks itself out on "+
			"its first request after saying it is awake. got %d %s", st, body)
	}
}

// ---------------------------------------------------------------------------
// ③ warden is exempt — a SAFETY property, not an optimisation
// ---------------------------------------------------------------------------

// TestAgentIatFloor_WardenPermanentTokenIsExempt pins the one exclusion that
// cannot be left to a coincidence of today's client.
//
// mintWardenToken issues scope="agent" credentials with NO exp for a machine
// member, so a warden token is indistinguishable from an agent token by scope
// alone. cli/ocwarden does not call report_waking today — but that is a fact
// about today's warden, not a contract. If one line there ever raised a floor
// above a credential that can never expire out of the way, every machine
// carrying an older permanent token would go dark PERMANENTLY, with a re-install
// as the only recovery. The gate therefore excludes Kind == machineKind by
// name.
//
// Mutant: drop the machineKind exclusion from agentIatFloorRefusal → the AFTER
// arm here turns 401 and this test is red.
func TestAgentIatFloor_WardenPermanentTokenIsExempt(t *testing.T) {
	srv, secret, api := revokeStack(t)
	putTestMember(t, api, Member{
		ID: "m-t14-box", Name: "t14-box", Kind: KindWarden, Effort: "medium",
		DesiredState: DesiredStateOffline, RosterStatus: RosterStatusActive,
	})

	now := time.Now().Unix()
	// The permanent credential a machine was installed with, ten minutes ago.
	oldPermanent, err := mintJWTWithoutExpiry("m-t14-box", "agent", secret, now-600, "")
	if err != nil {
		t.Fatal(err)
	}
	// A newer credential for the same machine — whatever raises the floor.
	newPermanent, err := mintJWTWithoutExpiry("m-t14-box", "agent", secret, now, "")
	if err != nil {
		t.Fatal(err)
	}

	// Positive control: the old permanent credential works to start with.
	if st, body := revokeCall(t, "GET", srv.URL+"/api/members", oldPermanent, ""); st != http.StatusOK {
		t.Fatalf("POSITIVE CONTROL FAILED — an active machine's permanent credential "+
			"must be 200, got %d %s", st, body)
	}

	// The floor is raised on the machine's own member row, through the same
	// public seam any client would use.
	wakeWith(t, srv.URL, newPermanent)

	if st, body := revokeCall(t, "GET", srv.URL+"/api/members", oldPermanent, ""); st != http.StatusOK {
		t.Fatalf("a machine's PERMANENT credential must never be refused by the "+
			"agent iat floor (it has no exp to expire out of the way — refusing it "+
			"takes the machine off the fleet until someone re-installs it by hand), "+
			"got %d %s", st, body)
	}
}
