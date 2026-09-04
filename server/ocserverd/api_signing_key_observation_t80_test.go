package main

// api_signing_key_observation_t80_test.go — T-80: the station records WHICH
// signing key each machine's credential is actually signed by, so the owner can
// tell whether it is safe to press 「移除」 on a retired key (an act with no
// grace period at all).
//
// 🔴 EVERY TEST HERE ENTERS THROUGH A REAL HTTP REQUEST on the production
// assembly (t62Stack → buildAPIHandler), never by calling requireAuth,
// verifyJWTAnyKey or the DAL setter directly. That is not stylistic. The failure
// this whole file exists to prevent has already happened once in this repo: a
// feature was proved correct by tests that called its internals, and the ONE
// line connecting it to the live path was unguarded — deleting it left 2716
// tests green. Here that line is the observeTokenKey call inside requireAuth,
// and TestAMachineAuthenticatingRecordsTheKeyThatVerifiedItsCredential is the
// test that dies with it.
//
// Nothing here touches a real machine, a real warden or a real key: temp sqlite,
// a temp ring, httptest.

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"
)

// t80Warden plants an ACTIVE warden roster row — the only kind of row this
// feature stamps — and hands back the permanent credential a real warden holds.
//
// KindWarden is the same string machineKind names ("warden"); the in-flight
// 'assistant' → 'staff' kind rename does not touch it, so nothing here is
// keyed on the value being renamed.
func t80Warden(t *testing.T, api *apiServer, id, name string) string {
	t.Helper()
	putTestMember(t, api, Member{
		ID: id, Name: name, Kind: KindWarden, Effort: "medium",
		DesiredState: DesiredStateOffline, RosterStatus: RosterStatusActive,
	})
	// mintWardenToken is the production mint for this credential shape: scope
	// "agent", no exp, no machine_id binding. Going through it rather than
	// hand-rolling a token means a change to how warden credentials are signed
	// reaches these tests.
	m, err := api.dal.GetMember(id)
	if err != nil || m == nil {
		t.Fatalf("seed warden %s: %v %v", id, m, err)
	}
	tok, err := api.mintWardenToken(*m)
	if err != nil {
		t.Fatalf("mintWardenToken %s: %v", id, err)
	}
	return tok
}

// t80Get makes one real request through the built handler. /api/members is an
// ordinary gated route a live warden genuinely calls (see the liveCall list in
// api_auth_machine_revoke_test.go) and it writes nothing, which the
// write-suppression test below depends on.
func t80Get(t *testing.T, url, token string) (int, string) {
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
	var sb strings.Builder
	buf := make([]byte, 4096)
	for {
		n, rerr := resp.Body.Read(buf)
		sb.Write(buf[:n])
		if rerr != nil {
			break
		}
	}
	return resp.StatusCode, sb.String()
}

func t80TokenKeyOf(t *testing.T, dal *DAL, id string) string {
	t.Helper()
	m, err := dal.GetMember(id)
	if err != nil || m == nil {
		t.Fatalf("read back %s: %v %v", id, m, err)
	}
	return m.TokenKeyID
}

// ---------------------------------------------------------------------------
// ① the observation happens at all, and it happens ON THE LIVE PATH
// ---------------------------------------------------------------------------

// TestAMachineAuthenticatingRecordsTheKeyThatVerifiedItsCredential is the
// load-bearing test of this ticket.
//
// A machine makes one ordinary authenticated request. Afterwards the station
// knows which signing key that machine's credential is signed by. Before the
// request it knew nothing — that BEFORE arm is inside the test on purpose, so a
// version of this that always passed (or a probe that never authenticated at
// all) cannot go green.
//
// Mutant: delete the `observeTokenKey(claims, keyID)` call in requireAuth
// (server.go) — the feature is then complete, correct and wired to nothing, and
// this test is the thing that goes red.
func TestAMachineAuthenticatingRecordsTheKeyThatVerifiedItsCredential(t *testing.T) {
	srv, keys, dal, api := t62Stack(t, []byte(interopSecret))
	tok := t80Warden(t, api, "m-box", "box-1")

	if got := t80TokenKeyOf(t, dal, "m-box"); got != "" {
		t.Fatalf("PREMISE FAILED: a machine that has never authenticated must "+
			"carry no observation; got %q", got)
	}

	if st, body := t80Get(t, srv.URL+"/api/members", tok); st != http.StatusOK {
		t.Fatalf("POSITIVE CONTROL FAILED — a live warden credential must pass "+
			"the gate; got %d %s", st, body)
	}

	want := keys.activeKeyID()
	if want == "" {
		t.Fatalf("PREMISE FAILED: the ring must have a signing key")
	}
	if got := t80TokenKeyOf(t, dal, "m-box"); got != want {
		t.Fatalf("after one authenticated request the station must know which "+
			"key verified that machine's credential: member.token_key_id = %q, "+
			"want %q.\nThis is the ONLY source of 「還剩幾台沒換」. If you just "+
			"removed the observeTokenKey call from requireAuth, that is the line "+
			"to restore.", got, want)
	}
}

// TestACredentialTheGateRefusesRecordsNothing is the other half of ①: the
// observation is evidence, so it must come only from a credential that was
// actually ACCEPTED. A token the ring cannot verify must leave the machine's
// row exactly as it was.
//
// Mutant: move the observeTokenKey call in requireAuth above the refusals (or
// have verifyJWTAnyKey report an id on a failure instead of "") and this goes
// red.
func TestACredentialTheGateRefusesRecordsNothing(t *testing.T) {
	srv, _, dal, api := t62Stack(t, []byte(interopSecret))
	t80Warden(t, api, "m-box", "box-1")

	// Signed by a key that is not on the ring at all — a forgery, from the
	// gate's point of view.
	forged, err := mintJWT("m-box", "agent", 3600, []byte("not-a-ring-key-at-all"),
		time.Now().Unix(), "")
	if err != nil {
		t.Fatal(err)
	}
	if st, _ := t80Get(t, srv.URL+"/api/members", forged); st != http.StatusUnauthorized {
		t.Fatalf("PREMISE FAILED: a token signed by a key outside the ring must "+
			"be refused; got %d", st)
	}
	if got := t80TokenKeyOf(t, dal, "m-box"); got != "" {
		t.Fatalf("a REFUSED credential proves nothing about which key a machine "+
			"is on, yet member.token_key_id = %q", got)
	}

	// The second arm is about ORDER, not about signatures. This credential is
	// perfectly signed by a key on the ring — it fails a LATER gate (the roster
	// says the machine is gone, authz.go revocationRefusal). An observation made
	// before that gate would record it anyway, and the owner's count would
	// include a machine that no longer exists.
	revoked := t80Warden(t, api, "m-gone", "gone-box")
	gone, err := dal.GetMember("m-gone")
	if err != nil || gone == nil {
		t.Fatalf("read m-gone: %v %v", gone, err)
	}
	gone.RosterStatus = RosterStatusRemoved
	if err := dal.PutMember(*gone); err != nil {
		t.Fatalf("soft-delete m-gone: %v", err)
	}
	if st, _ := t80Get(t, srv.URL+"/api/members", revoked); st != http.StatusUnauthorized {
		t.Fatalf("PREMISE FAILED: a deleted machine's credential must be refused; got %d", st)
	}
	if got := t80TokenKeyOf(t, dal, "m-gone"); got != "" {
		t.Fatalf("a credential that VERIFIED but was refused by a later gate "+
			"must not be recorded either: member.token_key_id = %q.\n"+
			"The observation belongs AFTER every refusal in requireAuth.", got)
	}
}

// TestARefusedCredentialNeverNamesAKeyOnTheWire pins the property jwt.go's
// header declares: the refusal says a token did not verify, and never anything
// about the ring. Adding a key id to the returned value must not have leaked one
// into the answer.
//
// Mutant: make verifyJWTAnyKey's error mention the candidate id, or have
// requireAuth put keyID on the response, and this goes red.
func TestARefusedCredentialNeverNamesAKeyOnTheWire(t *testing.T) {
	srv, keys, dal, _ := t62Stack(t, []byte(interopSecret))
	// A ring with several keys, so there is more than one id that could leak.
	for i := 0; i < 2; i++ {
		if _, err := keys.rotate(dal); err != nil {
			t.Fatalf("rotate: %v", err)
		}
	}
	ids := []string{}
	for _, meta := range keys.snapshot() {
		ids = append(ids, meta.ID)
	}
	if len(ids) < 3 {
		t.Fatalf("PREMISE FAILED: want a multi-key ring, got %v", ids)
	}

	forged, err := mintJWT("m-nobody", "agent", 3600, []byte("not-a-ring-key-at-all"),
		time.Now().Unix(), "")
	if err != nil {
		t.Fatal(err)
	}
	st, body := t80Get(t, srv.URL+"/api/members", forged)
	if st != http.StatusUnauthorized {
		t.Fatalf("PREMISE FAILED: forged token must be refused; got %d %s", st, body)
	}
	for _, id := range ids {
		if strings.Contains(body, id) {
			t.Fatalf("the refusal names a signing key (%q) — a refusal must say "+
				"that a token did not verify and nothing about the ring: %s", id, body)
		}
	}
}

// ---------------------------------------------------------------------------
// ② the recorded key is the RIGHT one, and it survives a rotation honestly
// ---------------------------------------------------------------------------

// TestAfterARotationOnlyMachinesThatCameBackReadAsOnTheCurrentKey is the
// question the owner actually asks before pressing 「移除」.
//
// Two machines authenticate on the original key. The ring rotates. One is
// re-credentialled; the other KEEPS CALLING ON ITS OLD TOKEN, which is what a
// machine nobody has touched actually does — warden credentials are permanent
// and the old key still verifies, so the requests keep succeeding. On the wire
// the re-credentialled one reads as on the current key and the untouched one
// still names the OLD key and reads as not current.
//
// 🔴 THAT SECOND MACHINE IS THE LOAD-BEARING HALF, and it is why it keeps making
// requests rather than going quiet: a version that recorded "whichever key is
// signing now" instead of "the key that actually verified" would look correct
// for a machine that never calls again, and would quietly mark this one as
// migrated on its very next heartbeat — telling the owner the fleet had moved
// when not one byte of it had. Mutant: return kr.activeKeyID() from
// verifyJWTAnyKey instead of the verifying candidate's id.
func TestAfterARotationOnlyMachinesThatCameBackReadAsOnTheCurrentKey(t *testing.T) {
	srv, keys, dal, api := t62Stack(t, []byte(interopSecret))
	ownerTok := mintOwnerAt(t, keys, time.Now().Unix())

	movedTok := t80Warden(t, api, "m-moved", "moved-box")
	stuckTok := t80Warden(t, api, "m-stuck", "stuck-box")
	oldKey := keys.activeKeyID()

	for who, tok := range map[string]string{"m-moved": movedTok, "m-stuck": stuckTok} {
		if st, body := t80Get(t, srv.URL+"/api/members", tok); st != http.StatusOK {
			t.Fatalf("POSITIVE CONTROL FAILED — %s must pass the gate before the "+
				"rotation; got %d %s", who, st, body)
		}
	}

	if _, err := keys.rotate(dal); err != nil {
		t.Fatalf("rotate: %v", err)
	}
	newKey := keys.activeKeyID()
	if newKey == oldKey {
		t.Fatalf("PREMISE FAILED: a rotation must move the signing key")
	}

	// Only m-moved re-credentials. Its old token still verifies (that is the
	// whole point of the ring), so the machine must present a token minted
	// SINCE the rotation for the station to see it on the new key — exactly the
	// real-world sequence.
	moved, err := dal.GetMember("m-moved")
	if err != nil || moved == nil {
		t.Fatalf("read m-moved: %v %v", moved, err)
	}
	reissued, err := api.mintWardenToken(*moved)
	if err != nil {
		t.Fatalf("re-mint: %v", err)
	}
	if st, body := t80Get(t, srv.URL+"/api/members", reissued); st != http.StatusOK {
		t.Fatalf("the re-credentialled machine must pass the gate; got %d %s", st, body)
	}

	// m-stuck goes on working, on the credential it has always had. The old key
	// is still on the ring, so this is a 200 — that is the whole point of the
	// ring and the whole reason the owner cannot tell by looking at failures.
	if st, body := t80Get(t, srv.URL+"/api/members", stuckTok); st != http.StatusOK {
		t.Fatalf("PREMISE FAILED: a machine on the OUTGOING key must still be "+
			"served (that is what makes this question hard); got %d %s", st, body)
	}

	rows := t80ListMachines(t, srv.URL, ownerTok)
	movedRow, ok := rows["m-moved"]
	if !ok {
		t.Fatalf("GET /api/machines does not list m-moved: %v", rows)
	}
	stuckRow, ok := rows["m-stuck"]
	if !ok {
		t.Fatalf("GET /api/machines does not list m-stuck: %v", rows)
	}

	if movedRow.TokenKeyID == nil || *movedRow.TokenKeyID != newKey {
		t.Fatalf("the machine that came back must read as signed by the CURRENT "+
			"key: token_key_id = %v, want %q", derefStr(movedRow.TokenKeyID), newKey)
	}
	if movedRow.TokenKeyCurrent == nil || !*movedRow.TokenKeyCurrent {
		t.Fatalf("the machine that came back must read as token_key_current=true, got %v",
			derefBool(movedRow.TokenKeyCurrent))
	}
	if stuckRow.TokenKeyID == nil || *stuckRow.TokenKeyID != oldKey {
		t.Fatalf("the machine that did NOT come back must still name the key it "+
			"was last SEEN on (%q), got %v — anything else would tell the owner "+
			"the fleet had migrated when it has not",
			oldKey, derefStr(stuckRow.TokenKeyID))
	}
	if stuckRow.TokenKeyCurrent == nil || *stuckRow.TokenKeyCurrent {
		t.Fatalf("the machine that did NOT come back must read as "+
			"token_key_current=false, got %v", derefBool(stuckRow.TokenKeyCurrent))
	}
}

// TestAMachineThatHasNeverAuthenticatedIsNotCountedEitherWay keeps the third
// state distinguishable. "never seen" is not "still on the old key": one means
// the owner has no information, the other means he has information and it says
// no. Folding either into the other is how a removal gets pressed too early or
// never at all.
func TestAMachineThatHasNeverAuthenticatedIsNotCountedEitherWay(t *testing.T) {
	srv, keys, _, api := t62Stack(t, []byte(interopSecret))
	ownerTok := mintOwnerAt(t, keys, time.Now().Unix())
	t80Warden(t, api, "m-silent", "silent-box")

	rows := t80ListMachines(t, srv.URL, ownerTok)
	row, ok := rows["m-silent"]
	if !ok {
		t.Fatalf("GET /api/machines does not list m-silent: %v", rows)
	}
	if row.TokenKeyID != nil {
		t.Fatalf("a machine this station has never authenticated must report a "+
			"null token_key_id, got %q", *row.TokenKeyID)
	}
	if row.TokenKeyCurrent != nil {
		t.Fatalf("…and no verdict at all on whether it is on the current key, "+
			"got %v", *row.TokenKeyCurrent)
	}
}

// ---------------------------------------------------------------------------
// ③ the observation must not cost a database write per request
// ---------------------------------------------------------------------------

// TestRepeatedRequestsOnAnUnchangedKeyCostNoFurtherWrites is a real constraint,
// not a micro-optimisation: this observation runs on EVERY authenticated request
// on every gated route, and the write pool is ONE connection wide
// (server/CLAUDE.md §7). A write per request would serialise the whole server
// behind a bookkeeping column.
//
// It asks the DATABASE what happened (sqlite total_changes() on the write
// connection) rather than counting calls to a fake, for the reason
// single_column_writes_t14_test.go gives: a test that watched the code path
// would go green on a rewrite that still wrote every time.
//
// Mutant: drop the memo check in noteTokenKeyObservation (call
// SetMemberTokenKeyID unconditionally) and this goes red.
func TestRepeatedRequestsOnAnUnchangedKeyCostNoFurtherWrites(t *testing.T) {
	srv, keys, dal, api := t62Stack(t, []byte(interopSecret))
	tok := t80Warden(t, api, "m-box", "box-1")

	// The FIRST request is the one that legitimately writes.
	if st, body := t80Get(t, srv.URL+"/api/members", tok); st != http.StatusOK {
		t.Fatalf("POSITIVE CONTROL FAILED — got %d %s", st, body)
	}
	if got := t80TokenKeyOf(t, dal, "m-box"); got == "" {
		t.Fatalf("PREMISE FAILED: the first request must have recorded something")
	}

	before := t80TotalChanges(t, dal)
	const requests = 25
	for i := 0; i < requests; i++ {
		if st, body := t80Get(t, srv.URL+"/api/members", tok); st != http.StatusOK {
			t.Fatalf("request %d: got %d %s", i, st, body)
		}
	}
	after := t80TotalChanges(t, dal)
	if after != before {
		t.Fatalf("%d further requests on the SAME key changed %d database rows, "+
			"want 0.\nThe write pool is one connection wide; only a CHANGE of "+
			"observed key may reach the database. If you removed the memo check "+
			"in noteTokenKeyObservation, that is the line to restore.",
			requests, after-before)
	}

	// …and the suppression is not "it never writes again": a genuine change
	// must still land. Without this arm the test above would pass on a version
	// that recorded nothing at all after the first request.
	if _, err := keys.rotate(dal); err != nil {
		t.Fatalf("rotate: %v", err)
	}
	m, err := dal.GetMember("m-box")
	if err != nil || m == nil {
		t.Fatalf("read m-box: %v %v", m, err)
	}
	reissued, err := api.mintWardenToken(*m)
	if err != nil {
		t.Fatalf("re-mint: %v", err)
	}
	if st, body := t80Get(t, srv.URL+"/api/members", reissued); st != http.StatusOK {
		t.Fatalf("re-credentialled request: got %d %s", st, body)
	}
	if got, want := t80TokenKeyOf(t, dal, "m-box"), keys.activeKeyID(); got != want {
		t.Fatalf("a CHANGED key must still be recorded: token_key_id = %q, want %q",
			got, want)
	}
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

// t80TotalChanges reads sqlite's total_changes() on the WRITE connection — the
// count of rows inserted, updated or deleted through it since it was opened.
// Reads do not move it, so it measures exactly the thing this feature must not
// do on every request.
func t80TotalChanges(t *testing.T, d *DAL) int64 {
	t.Helper()
	var n int64
	if err := d.wdb.QueryRow(`SELECT total_changes()`).Scan(&n); err != nil {
		t.Fatalf("total_changes(): %v", err)
	}
	return n
}

type t80MachineRow struct {
	MachineID       string  `json:"machine_id"`
	TokenKeyID      *string `json:"token_key_id"`
	TokenKeyCurrent *bool   `json:"token_key_current"`
}

func t80ListMachines(t *testing.T, base, ownerTok string) map[string]t80MachineRow {
	t.Helper()
	st, body := t80Get(t, base+"/api/machines", ownerTok)
	if st != http.StatusOK {
		t.Fatalf("GET /api/machines: want 200, got %d %s", st, body)
	}
	var rows []t80MachineRow
	if err := json.Unmarshal([]byte(body), &rows); err != nil {
		t.Fatalf("GET /api/machines: %v (%s)", err, body)
	}
	out := map[string]t80MachineRow{}
	for _, r := range rows {
		out[r.MachineID] = r
	}
	return out
}

func derefStr(p *string) any {
	if p == nil {
		return nil
	}
	return *p
}

func derefBool(p *bool) any {
	if p == nil {
		return nil
	}
	return *p
}

// TestTheRefusalFromTheRingNeverNamesAKeyIsTheContractOfVerifyJWTAnyKey covers
// what the wire cannot see. requireAuth flattens every cause into one "invalid
// token", so the wire test above proves nothing has leaked TO A CLIENT but
// cannot see the error verifyJWTAnyKey itself hands back — and jwt.go's header
// declares that error must never say WHICH key failed. This is the one
// assertion in this file made below the HTTP surface, because the property
// being asserted is a property of that return value and of nothing else.
//
// Mutant: wrap the per-candidate error with its id before assigning lastErr and
// this goes red.
func TestTheRefusalFromTheRingNeverNamesAKeyIsTheContractOfVerifyJWTAnyKey(t *testing.T) {
	dal := newTestDAL(t)
	keys, err := loadKeyring(dal, []byte(interopSecret))
	if err != nil {
		t.Fatalf("loadKeyring: %v", err)
	}
	for i := 0; i < 2; i++ {
		if _, err := keys.rotate(dal); err != nil {
			t.Fatalf("rotate: %v", err)
		}
	}
	ids := []string{}
	for _, meta := range keys.snapshot() {
		ids = append(ids, meta.ID)
	}
	if len(ids) < 3 {
		t.Fatalf("PREMISE FAILED: want a multi-key ring, got %v", ids)
	}

	now := time.Now().Unix()
	forged, err := mintJWT("m-nobody", "agent", 3600, []byte("not-a-ring-key-at-all"), now, "")
	if err != nil {
		t.Fatal(err)
	}
	claims, keyID, err := verifyJWTAnyKey(keys, forged, now)
	if err == nil {
		t.Fatalf("PREMISE FAILED: a forged token must not verify (claims %v)", claims)
	}
	if keyID != "" {
		t.Fatalf("a FAILED verification must report no key at all, got %q — a key "+
			"id is evidence that a credential was accepted, and this one was not", keyID)
	}
	for _, id := range ids {
		if strings.Contains(err.Error(), id) {
			t.Fatalf("the refusal names key %q. The error must say that a token "+
				"did not verify and NOTHING about the ring — jwt.go's header "+
				"declares this, and a per-key error is what it forbids: %v", id, err)
		}
	}
}
