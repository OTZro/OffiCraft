package main

// api_members_restartself_test.go — restart_self (POST /api/self/refocus, the
// T-4c71 self-triggered recycle). The authz faces (machine floor, owner 404,
// offline 409) are pinned black-box in conformance/test_auth_matrix.py; the
// two behaviours that need a LIVE SSE session + a stamped boot_ts — the
// online-positive 200 stamp and the 429 minimum-liveness refusal — are pinned
// here where the harness can drive the hub + gauge directly.

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// doRestartSelf drives POST /api/self/refocus as sub (agent scope), with an
// optional JSON body and no iat on the credential.
func doRestartSelf(api *apiServer, sub, body string) *httptest.ResponseRecorder {
	return doRestartSelfMintedAt(api, sub, body, 0, 0)
}

// doRestartSelfMintedAt is doRestartSelf with the caller's token carrying the
// iat/exp a mint of the given lifetime would produce (epoch seconds, as
// encoding/json hands them to the auth middleware); ttl 0 omits both claims,
// standing in for a token predating them.
func doRestartSelfMintedAt(api *apiServer, sub, body string, iat, ttl float64) *httptest.ResponseRecorder {
	var r *http.Request
	if body == "" {
		r = httptest.NewRequest("POST", "/api/self/refocus", nil)
	} else {
		r = httptest.NewRequest("POST", "/api/self/refocus", strings.NewReader(body))
	}
	claims := map[string]any{"sub": sub, "scope": "agent"}
	if ttl != 0 {
		claims["iat"] = iat
		claims["exp"] = iat + ttl
	}
	r = r.WithContext(context.WithValue(r.Context(), claimsContextKey, claims))
	rec := httptest.NewRecorder()
	api.HandleRestartSelfApiSelfRefocusPost(rec, r)
	return rec
}

// online marks a member online by registering an SSE listener (the sole
// authority for the online projection); returns a cleanup.
func online(t *testing.T, api *apiServer, id string) func() {
	t.Helper()
	l, err := api.hub.Connect(id, "")
	if err != nil {
		t.Fatalf("hub.Connect(%s): %v", id, err)
	}
	return func() { api.hub.Disconnect(l) }
}

func TestRestartSelfStampsRefocusWhenOnlineAndPastLivenessFloor(t *testing.T) {
	api, dal := newGateTestAPI(t)
	putGateMember(t, dal, Member{ID: "rs-ok", Kind: KindAssistant,
		DesiredState: DesiredStateOnline})
	defer online(t, api, "rs-ok")()
	// A session that connected well before the liveness floor.
	api.gauge.Set("rs-ok", map[string]any{"boot_ts": nowSecs() - (minSelfRestartSecs + 100)})

	rec := doRestartSelf(api, "rs-ok", `{"reason":"context near the handover line"}`)
	if rec.Code != 200 {
		t.Fatalf("online + past-floor self-restart: want 200, got %d %s", rec.Code, rec.Body.String())
	}
	m, err := dal.GetMember("rs-ok")
	if err != nil || m == nil {
		t.Fatalf("reload rs-ok: %v", err)
	}
	if m.RefocusSince <= 0.0 {
		t.Fatalf("restart_self must stamp refocus_since; got %v", m.RefocusSince)
	}
	// The other direction of the two-anchor rule: a session token minted moments
	// ago (a mid-session bootstrap re-mint) must not make a long-lived session
	// read as newborn — the OLDER anchor decides, and here that is boot_ts.
	putGateMember(t, dal, Member{ID: "rs-ok2", Kind: KindAssistant,
		DesiredState: DesiredStateOnline})
	defer online(t, api, "rs-ok2")()
	api.gauge.Set("rs-ok2", map[string]any{"boot_ts": nowSecs() - (minSelfRestartSecs + 100)})

	rec = doRestartSelfMintedAt(api, "rs-ok2", "", nowSecs()-5, float64(api.authTokenTTL()))
	if rec.Code != 200 {
		t.Fatalf("old boot_ts + freshly minted token: want 200, got %d %s", rec.Code, rec.Body.String())
	}
	if m2, _ := dal.GetMember("rs-ok2"); m2.RefocusSince <= 0.0 {
		t.Fatalf("restart_self must stamp refocus_since; got %v", m2.RefocusSince)
	}
}

func TestRestartSelfRefusesWithinLivenessFloor(t *testing.T) {
	api, dal := newGateTestAPI(t)
	putGateMember(t, dal, Member{ID: "rs-fresh", Kind: KindAssistant,
		DesiredState: DesiredStateOnline})
	defer online(t, api, "rs-fresh")()
	// A genuinely newborn session: it connected 1 minute ago AND its token was
	// minted 1 minute ago. Both anchors read young, so the storm guard fires —
	// the positive control for the second-source rule below, which must never
	// turn a real respawn storm into a pass.
	api.gauge.Set("rs-fresh", map[string]any{"boot_ts": nowSecs() - 60})

	rec := doRestartSelfMintedAt(api, "rs-fresh", "", nowSecs()-60, float64(api.authTokenTTL()))
	if rec.Code != 429 {
		t.Fatalf("fresh session self-restart: want 429, got %d %s", rec.Code, rec.Body.String())
	}
	m, _ := dal.GetMember("rs-fresh")
	if m.RefocusSince != 0.0 {
		t.Fatalf("a refused self-restart must not stamp refocus_since; got %v", m.RefocusSince)
	}
	// A credential carrying no iat (minted before the claim existed) leaves
	// boot_ts as the only anchor — the refusal must survive that fallback.
	if rec := doRestartSelf(api, "rs-fresh", ""); rec.Code != 429 {
		t.Fatalf("fresh session, iat-less token: want 429, got %d %s", rec.Code, rec.Body.String())
	}
}

func TestRestartSelfRefusesWhenOffline(t *testing.T) {
	api, dal := newGateTestAPI(t)
	putGateMember(t, dal, Member{ID: "rs-off", Kind: KindAssistant,
		DesiredState: DesiredStateOnline})
	// No hub connection → not online. boot_ts old enough that the liveness
	// floor would pass, proving the 409 is the ONLINE gate, not the floor.
	api.gauge.Set("rs-off", map[string]any{"boot_ts": nowSecs() - (minSelfRestartSecs + 100)})

	rec := doRestartSelf(api, "rs-off", "")
	if rec.Code != 409 {
		t.Fatalf("offline self-restart: want 409, got %d %s", rec.Code, rec.Body.String())
	}
	m, _ := dal.GetMember("rs-off")
	if m.RefocusSince != 0.0 {
		t.Fatalf("a refused self-restart must not stamp refocus_since; got %v", m.RefocusSince)
	}
}

// This replaces TestRestartSelfMissingBootTsFailsOpen (T-4235). That test drove
// a state production cannot reach — it went online through hub.Connect alone,
// leaving the gauge empty, whereas a real agent only ever becomes online by
// completing the SSE first-connect edge, and that edge stamps boot_ts. It was
// green on the unreachable branch while the reachable one was broken. The
// reachable shape is here instead: the station re-execs, the gauge is reborn
// empty, the agent's reconnect stamps a brand-new boot_ts, and only the token's
// iat still remembers when this session actually began.
func TestRestartSelfAfterAStationRestartTrustsTheTokenIat(t *testing.T) {
	api, dal := newGateTestAPI(t)
	putGateMember(t, dal, Member{ID: "rs-reexec", Kind: KindAssistant,
		DesiredState: DesiredStateOnline})
	defer online(t, api, "rs-reexec")()
	// The re-exec: the gauge is a fresh in-memory store (restart amnesia is
	// contract), and the agent's reconnect runs the real first-connect edge,
	// which stamps boot_ts = now on a session that is hours old.
	api.gauge = newMemStore()
	api.onFirstConnect("rs-reexec")
	bootTS, ok := gaugeBootTS(api.gauge.Get("rs-reexec"))
	if !ok || nowSecs()-bootTS >= minSelfRestartSecs {
		t.Fatalf("fixture: the reconnect must leave a boot_ts inside the floor; got %v (ok=%t)", bootTS, ok)
	}
	// A station whose token TTL is 7 days, so a 2h-old session token is a
	// perfectly ordinary unexpired START credential.
	api.tokenTTL = 7 * 86400

	rec := doRestartSelfMintedAt(api, "rs-reexec", "", nowSecs()-7200, float64(api.authTokenTTL()))
	if rec.Code != 200 {
		t.Fatalf("a session whose token is 2h old must not read as newborn: want 200, got %d %s",
			rec.Code, rec.Body.String())
	}
	m, _ := dal.GetMember("rs-reexec")
	if m.RefocusSince <= 0.0 {
		t.Fatalf("an admitted self-restart must stamp refocus_since; got %v", m.RefocusSince)
	}
}

// The floor's second source is the caller's iat, and POST /api/mint hands out
// agent-scope tokens for an existing member lasting up to 400 days. An old one
// of those reports an age of months at every call, so trusting its iat would
// disable this member's floor permanently — the storm guard would be gone for
// exactly the caller holding the longest-lived credential.
func TestRestartSelfIgnoresTheIatOfALongLivedMintedToken(t *testing.T) {
	api, dal := newGateTestAPI(t)
	putGateMember(t, dal, Member{ID: "rs-mint", Kind: KindAssistant,
		DesiredState: DesiredStateOnline})
	defer online(t, api, "rs-mint")()
	// A genuinely newborn session, called with a mint token issued 100 days ago
	// for 400 days.
	api.gauge.Set("rs-mint", map[string]any{"boot_ts": nowSecs() - 30})

	rec := doRestartSelfMintedAt(api, "rs-mint", "", nowSecs()-100*86400, 400*86400)
	if rec.Code != 429 {
		t.Fatalf("a long-lived mint token must not vouch for session age: want 429, got %d %s",
			rec.Code, rec.Body.String())
	}
	if m, _ := dal.GetMember("rs-mint"); m.RefocusSince != 0.0 {
		t.Fatalf("a refused self-restart must not stamp refocus_since; got %v", m.RefocusSince)
	}
}

func TestRestartSelfOwnerHasNoRosterRow404(t *testing.T) {
	api, _ := newGateTestAPI(t)
	// The owner's sub carries no roster row: self-op is agent-only by
	// construction → resolveSelf 404 before any gate.
	rec := doRestartSelf(api, "owner", "")
	if rec.Code != 404 {
		t.Fatalf("owner self-restart: want 404, got %d %s", rec.Code, rec.Body.String())
	}
}
