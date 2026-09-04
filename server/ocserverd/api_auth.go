package main

// api_auth.go — the credential seams (handlers.handle_login / handle_mint /
// handle_bootstrap): the ONE public business entry (login), the owner-gated
// long-lived agent mint, and the agent boot seam (context fold + member JWT).

import (
	"fmt"
	"net/http"
	"time"
)

// maxAgentTTLSecs caps every long-lived agent token
// (service.config.MAX_AGENT_TTL_SECS — 400 days).
const maxAgentTTLSecs int64 = 400 * 86400

// invalidCredentialsMsg is the ONE refusal text /api/login answers with, for
// every cause. It names both factors so the cockpit can point the owner at the
// right field without the server disclosing which half actually failed.
const invalidCredentialsMsg = "invalid password or code"

// mintAgentToken is the ONE agent-scope boot-JWT mint under both spawn paths:
// scope="agent", sub=the member/worker id, machine_id = the boot host claim
// (omitted when empty). Members bind their durable desired machine; workers
// bind the warden actually picked at dispatch time.
func (s *apiServer) mintAgentToken(sub, machineID string, ttl int64) (string, error) {
	return mintJWT(sub, "agent", ttl, s.keys.signingSecret(), time.Now().Unix(), machineID)
}

// mintMemberToken mints a member's boot JWT (service.boot.mint_member_token):
// machine_id = desired_machine_id.
func (s *apiServer) mintMemberToken(m Member, ttl int64) (string, error) {
	return s.mintAgentToken(m.ID, m.DesiredMachineID, ttl)
}

// mintWardenToken mints the permanent machine credential used only by warden
// installation paths. It intentionally cannot accept an arbitrary member: a
// permanent token for an agent or outsource worker would bypass their TTL and
// the 400-day ceiling.
func (s *apiServer) mintWardenToken(m Member) (string, error) {
	if m.Kind != machineKind {
		return "", fmt.Errorf("%w: permanent credentials are warden-only", errInvalidToken)
	}
	return mintJWTWithoutExpiry(m.ID, "agent", s.keys.signingSecret(), time.Now().Unix(), "")
}

// POST /api/login — exchange the owner password (and, once enrolled, a TOTP
// code) for an owner-scoped JWT. Verified ONLY against the DB-stored argon2id
// hash (settings.go); the B1 oc.toml plaintext fallback is gone (B2).
//
// EVERY refusal on this route is the SAME flat 401 with the same message — no
// set password, wrong password, missing code and wrong code are indistinguishable
// (the first-run state is only ever disclosed by the B3 /api/auth/status
// endpoint, and `mfa_required` by the same one). Naming which factor failed
// would confirm a correct password to an attacker who has only guessed one
// half.
//
// 🔴 THE UX COST IS REAL AND ACCEPTED: an owner who fat-fingers the 6-digit
// code is told "invalid password or code", not "invalid code". The cockpit
// covers this by wording its inline error to name both fields, which is honest
// without the server disclosing anything.
//
// 🔴 AND EVERY ONE OF THEM COSTS THE SAME WALL-CLOCK, which is the other half
// of the same property. An identical SENTENCE served at a distinguishable SPEED
// discloses exactly what the sentence refuses to: 「密碼對、碼錯」 does one
// argon2id plus a TOTP verification while 「密碼錯」 stops after the argon2id.
// Every refusal below therefore waits until the instant stamped on the way in
// plus throttleFailureFloor (throttle.go). A SUCCESS does not wait at all.
func (s *apiServer) HandleLoginApiLoginPost(w http.ResponseWriter, r *http.Request) {
	// Stamped BEFORE any work, because the floor is a deadline measured from
	// here — not a sleep added to whatever the handler happened to spend.
	started := time.Now()
	var body LoginDTO
	if !decodeJSONBodyRequired(w, r, &body, "password") {
		return
	}
	// Server CONFIGURATION is settled before any credential work: a missing
	// signing secret is not a credential fact, so it must not take an in-flight
	// slot, must not burn a TOTP step, must not pay the refusal floor, and must
	// not be answered with the credential refusal. It used to sit after the whole
	// verification, which made it a distinguishable refusal reached THROUGH the
	// credential path; settling it here makes it a fact about the SERVER, which
	// GET /api/auth/status already tells anyone who asks.
	if len(s.keys.signingSecret()) == 0 {
		writeError(w, http.StatusUnauthorized, "auth not configured")
		return
	}
	// The brake sits BEFORE argon2id on purpose: at ~19 MiB and ~16-18 ms a
	// verification (measured on one Darwin box — the time is hardware-specific,
	// the memory is a parameter in password.go), the hash is itself the cheapest
	// denial-of-service on this server. begin RESERVES an in-flight slot, which is what stops a concurrent
	// burst running N argon2id verifications at once — and it is also what turns
	// the floor below from a per-request latency into a rate limit, because the
	// slot is held for the whole of that wait.
	release, wait, blocked := s.loginThrottle.begin()
	if blocked {
		writeThrottled(w, wait)
		return
	}
	defer release()

	hash := s.authPasswordHash()
	if hash == "" || !verifyPassword(body.Password, hash) {
		s.holdFailureFloor(started)
		writeError(w, http.StatusUnauthorized, invalidCredentialsMsg)
		return
	}
	// Second factor, when one is armed. verifyAndSpendTOTP is a no-op that
	// answers true while MFA is off, so this is the whole branch.
	code := ""
	if body.Code != nil {
		code = *body.Code
	}
	factorOK, err := s.verifyAndSpendTOTP(code, time.Now().Unix())
	if err != nil {
		// The floor could not be persisted, so the code was not really spent.
		// Failing closed here keeps a code from being replayable across the
		// restart that a storage fault tends to be followed by.
		internalError(w, err)
		return
	}
	if !factorOK {
		// 🔴 THE PASSWORD WAS RIGHT. This is the one refusal on this route that
		// is evidence of something rather than of nothing: whoever sent it holds
		// the owner's password and only lacks the phone. No throttle repairs a
		// leaked password, so the answer is to TELL SOMEONE — the assistant, who
		// asks the owner to change it. Dispatched asynchronously and counted, so
		// neither the DB write nor a flood of them can be felt from outside; see
		// noteFactorRefusedAfterCorrectPassword.
		s.noteFactorRefusedAfterCorrectPassword(time.Now())
		s.holdFailureFloor(started)
		writeError(w, http.StatusUnauthorized, invalidCredentialsMsg)
		return
	}
	ttl := s.ownerTokenTTLValue()
	token, err := mintJWT(wireOwnerID, "owner", ttl, s.keys.signingSecret(), time.Now().Unix(), "")
	if err != nil {
		// A mint failure is a SERVER fault, not a credential one, so it spends no
		// floor and raises no alert: nothing was guessed wrong here. The TOTP
		// step is already spent either way (it had to be, to be single-use), so
		// the owner waits for the next tick — unavoidable, and the 500 says so
		// rather than pretending the credentials were the problem.
		internalError(w, err)
		return
	}
	// Only now is the credential PROVEN all the way to a usable token — and this
	// return spends NO floor. The owner who knows their password never waits;
	// the whole cost of this brake falls on people who get it wrong.
	writeJSON(w, http.StatusOK, tokenDTO{
		Token:     token,
		TokenType: "bearer",
		ExpiresIn: ttl,
		OwnerID:   wireOwnerID,
	})
}

// POST /api/mint — owner-gated (route table requires="owner") mint of a
// long-lived AGENT token for an existing member; ttl capped at 400 days.
func (s *apiServer) HandleMintApiMintPost(w http.ResponseWriter, r *http.Request) {
	var body MintRequestDTO
	if !decodeJSONBodyRequired(w, r, &body, "member_id", "ttl_days") {
		return
	}
	m, err := s.resolveMember(body.MemberId, staffOnly)
	if err != nil {
		writeResolveError(w, err, "member", body.MemberId)
		return
	}
	ttl := int64(body.TtlDays) * 86400
	if ttl > maxAgentTTLSecs {
		ttl = maxAgentTTLSecs
	}
	// The mint here deliberately carries NO machine_id claim (lifecycle.md
	// §1.3 mint table: /api/mint — machine_id "none").
	token, err := mintJWT(m.ID, "agent", ttl, s.keys.signingSecret(), time.Now().Unix(), "")
	if err != nil {
		internalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, tokenDTO{
		Token:     token,
		TokenType: "bearer",
		ExpiresIn: ttl,
		OwnerID:   m.ID,
	})
}

// POST /api/bootstrap — assemble an agent's boot package (admin-gated on the
// route table). With member_id (a warden spawn) the response carries a fresh
// member JWT; a UI preview (no member_id) gets token: null (lifecycle.md §2.3).
func (s *apiServer) HandleBootstrapApiBootstrapPost(w http.ResponseWriter, r *http.Request) {
	var body BootstrapRequestDTO
	if !decodeJSONBody(w, r, &body) {
		return
	}
	var member *Member
	if body.MemberId != nil {
		m, err := s.resolveMember(*body.MemberId, staffOnly)
		if err != nil {
			writeResolveError(w, err, "member", *body.MemberId)
			return
		}
		member = m
	}
	boot, err := s.buildBootContext(strOrEmpty(body.Role), member)
	if err != nil {
		internalError(w, err)
		return
	}
	if boot == nil {
		roleKey := resolveBootRoleKey(strOrEmpty(body.Role), member)
		writeError(w, http.StatusNotFound, "role '"+roleKey+"' not found")
		return
	}
	var token *string
	if member != nil && len(s.keys.signingSecret()) > 0 {
		minted, err := s.mintMemberToken(*member, s.agentTokenTTLValue())
		if err != nil {
			internalError(w, err)
			return
		}
		token = &minted
	}
	writeJSON(w, http.StatusOK, bootstrapDTO{
		Role:    boot.RoleKey,
		Name:    boot.Name,
		Context: boot.Context,
		Token:   token,
	})
}

// ── T-80: which key is each machine's credential actually signed by ──────────

// noteTokenKeyObservation records, against the identity that just
// authenticated, WHICH signing key verified its credential — and it is the only
// producer of member.token_key_id.
//
// 🔴 THE VALUE IS THIS STATION'S OWN OBSERVATION, AND THE DESIGN LEANS ON THAT.
// keyID comes from verifyJWTAnyKey: the key whose HMAC actually matched. There
// is no claim, no header field and no heartbeat block through which a machine
// could tell us which key it holds, and there must not be one. The question this
// answers — "is every machine off the outgoing key, i.e. is it safe to press
// remove" — gates an IMMEDIATE, un-grace-periodded revocation, so an answer a
// machine could assert would be an answer a stale or hostile machine could
// assert. A machine that has not authenticated since the rotation simply keeps
// its old value, which reads honestly as "nothing has proved this one moved".
//
// 🔴 ONLY A CHANGE REACHES THE DATABASE, AND THE MEMO IS THE ONLY THING THAT
// MAKES THAT TRUE. This runs on EVERY authenticated request on every gated
// route, and the write pool is ONE connection wide (server/CLAUDE.md §7) — a
// write per request would serialise the whole server behind a bookkeeping
// column. The memo is process-local and lossy on purpose: a restart costs one
// redundant write per machine and nothing else.
//
// 🔑 THERE IS EXACTLY ONE SUPPRESSION, DELIBERATELY. An earlier shape had two —
// this memo AND a `if m.TokenKeyID != keyID` re-check before the write — and the
// second one made the first unguarded: removing the memo left every test green,
// because the DB comparison still absorbed the repeat. Two representations of
// "have we already recorded this" is the same-fact-twice shape this repo keeps
// getting bitten by, and here it hid which of them was load-bearing. The memo is
// now it, and TestRepeatedRequestsOnAnUnchangedKeyCostNoFurtherWrites dies
// without it.
//
// Only warden rows are stamped (Kind == machineKind). Agents and outsource
// workers reach the same gate, but their credentials are short-lived and
// re-minted by the server itself, so they are never what stands between the
// owner and a removal. They still take a memo slot, which is what keeps the
// roster read below off the common path for them too.
func (s *apiServer) noteTokenKeyObservation(claims map[string]any, keyID string) {
	if s == nil || s.dal == nil || keyID == "" {
		return
	}
	sub, _ := claims["sub"].(string)
	if sub == "" {
		return
	}
	s.tokenKeyObsMu.Lock()
	seen, ok := s.tokenKeyObs[sub]
	s.tokenKeyObsMu.Unlock()
	if ok && seen == keyID {
		return
	}
	m, err := s.dal.GetMember(sub)
	if err != nil {
		// A read failure is not evidence of anything. Leave the memo alone so
		// the next request retries, exactly as the roster-revocation seam
		// declines to treat a lookup error as a verdict (server/CLAUDE.md §2).
		return
	}
	if m == nil || m.Kind != machineKind {
		// Not a machine: remember the answer so this lookup does not repeat on
		// every request of an ordinary agent's session.
		s.rememberTokenKey(sub, keyID)
		return
	}
	if err := s.dal.SetMemberTokenKeyID(sub, keyID); err != nil {
		// Do not memo a write that did not land, or the observation is lost
		// until the next rotation.
		return
	}
	s.rememberTokenKey(sub, keyID)
}

func (s *apiServer) rememberTokenKey(sub, keyID string) {
	s.tokenKeyObsMu.Lock()
	defer s.tokenKeyObsMu.Unlock()
	if s.tokenKeyObs == nil {
		s.tokenKeyObs = map[string]string{}
	}
	s.tokenKeyObs[sub] = keyID
}
