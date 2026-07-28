package main

// api_chat_share_link_tool_test.go — the share-link mint seam AS AN AGENT
// TOOL. The route itself is old; what is new is that it appears on the MCP
// surface, and routes.go used to keep it off there on purpose ("a UI
// convenience seam, not an agent tool").
//
// So these tests pin the three halves that reversal has to get right:
//
//	(1) the tool is reachable — on the route table AND in the frozen catalog
//	    tools/list is served from — and mints the SAME credential the cockpit
//	    gets, so no second signing path was invented (sharesig.go stays the
//	    only one);
//	(2) it mints for the callers the reversal exists for, all the way down to
//	    the row's own floor — not just for the admin agent the other fixtures
//	    happen to use;
//	(3) the credential still grants exactly one blob read and nothing else —
//	    a tampered sig, another blob's sig, and a bare request are all 401.
//
// There is deliberately NO expiry test: share sigs have no expiry by design
// (sharesig.go: "No expiry, no revocation, no stored state"). A test asserting
// one would be asserting a mechanism this repo does not have.

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"
)

// shareLinkToolName is the tool the routes table now exposes. Written once so
// a rename has to touch this constant rather than drift across assertions.
const shareLinkToolName = "get_chat_attachment_share_link"

// TestShareLinkToolIsOnTheAgentSurfaceAtAnUnchangedFloor pins the reversal at
// the table level, and pins the thing that MUST NOT have moved with it: the
// principal floor. Exposing the row is a discoverability change; if this test
// ever has to be edited because Requires changed, that is a different (and
// much larger) decision than the one this row's comment records.
func TestShareLinkToolIsOnTheAgentSurfaceAtAnUnchangedFloor(t *testing.T) {
	row, ok := mcpToolIndex(defaultRouteSpecs())[shareLinkToolName]
	if !ok {
		t.Fatalf("%s is missing from the route table's tool index", shareLinkToolName)
	}
	// The table is only half of it: tools/list is SERVED from the frozen
	// spec/mcp-catalog.json (assets.go, embed-only), so a row that is on the
	// table but absent from the catalog is a tool no agent can see. Checking
	// only the table leaves that whole half green.
	if !frozenCatalogCarries(t, shareLinkToolName) {
		t.Fatalf("%s is on the route table but MISSING from spec/mcp-catalog.json "+
			"— agents reach tools only through tools/list, so the mint seam "+
			"does not exist for them", shareLinkToolName)
	}
	if row.Method != "GET" || row.Path != "/api/chat/attachments/{attachment_id}/share-link" {
		t.Fatalf("%s is bound to the wrong row: %s %s", shareLinkToolName, row.Method, row.Path)
	}
	if row.Requires != principalMachine {
		t.Fatalf("share-link floor moved to %q — exposing this row was a "+
			"DISCOVERABILITY change; moving the floor is a separate ruling",
			row.Requires)
	}
	// The blob GET stays off the tool surface: bytes belong on the streaming
	// seam (ocagent download), never inside a JSON tool result.
	if _, exposed := mcpToolIndex(defaultRouteSpecs())["get_chat_attachment"]; exposed {
		t.Fatal("the blob GET must stay mcp_exclude — bytes never ride a tool result")
	}
}

// TestShareLinkMintedThroughMcpReadsTheBlobWithoutCredentials is the load-
// bearing one: an agent uploads a deliverable, mints its link through the same
// wired stack a live agent uses (auth gate + RBAC choke + loopback), and the
// returned URL then serves the bytes to a caller carrying NO credentials at
// all. That last hop is the whole point — it is what lets an agent hand a file
// to someone who will not sign in to the cockpit.
func TestShareLinkMintedThroughMcpReadsTheBlobWithoutCredentials(t *testing.T) {
	srv, secret, _ := newWiredTestServer(t)
	agentTok, _ := mintJWT("mira", "agent", 300, secret, time.Now().Unix(), "")

	payload := []byte("the deliverable bytes")
	attID, _ := uploadBlob(t, srv.URL, agentTok,
		"?filename=report.txt&mime=text/plain", payload)["id"].(string)
	if !strings.HasPrefix(attID, "att-") {
		t.Fatalf("upload must mint an att- id, got %q", attID)
	}

	url := shareLinkVia(t, srv.URL, agentTok, attID)

	// It is the EXISTING credential, not a second signing path — and it is
	// server-relative by contract (only the client knows the public origin,
	// which this exact-match pins as a side effect).
	if want := "/api/chat/attachment/" + attID + "?sig=" + shareSigFor(secret, attID); url != want {
		t.Fatalf("minted link must be the sharesig.go credential\n got: %s\nwant: %s", url, want)
	}

	status, served := doRaw(t, "GET", srv.URL+url, "", "", nil)
	if status != 200 || served != string(payload) {
		t.Fatalf("a credential-less holder of the link must read the blob: %d %q", status, served)
	}
}

// TestShareLinkGrantsExactlyOneBlobAndNothingElse walks the deny side. Each
// case is a way the credential could over-grant if the gate were sloppy, and
// each must stay 401.
func TestShareLinkGrantsExactlyOneBlobAndNothingElse(t *testing.T) {
	srv, secret, _ := newWiredTestServer(t)
	agentTok, _ := mintJWT("mira", "agent", 300, secret, time.Now().Unix(), "")

	mine, _ := uploadBlob(t, srv.URL, agentTok, "", []byte("mine"))["id"].(string)
	other, _ := uploadBlob(t, srv.URL, agentTok, "", []byte("someone else's"))["id"].(string)

	mineURL := shareLinkVia(t, srv.URL, agentTok, mine)
	parts := strings.SplitN(mineURL, "sig=", 2)
	if len(parts) != 2 || parts[1] == "" {
		// Unguarded indexing here would turn "the mint dropped the sig" into a
		// panic inside the deny table instead of a readable failure.
		t.Fatalf("minted link carries no sig: %q", mineURL)
	}
	mineSig := parts[1]

	// A tampered sig: same blob, last character flipped.
	tampered := mineSig[:len(mineSig)-1] + "X"
	if tampered == mineSig { // the flip has to actually flip
		tampered = mineSig[:len(mineSig)-1] + "Y"
	}

	// ⚠️ Discriminating power is NOT uniform across this table. The first five
	// each fail under a real product mutation (proven: adding ShareSig to the
	// mint row turns "sig on the mint route" red; a permissive verifyShareSig
	// turns "tampered"/"another blob's" red). The LAST one cannot currently
	// fire — a credential-less request to any gated row is already 401 from
	// requireAuth, and even flagging ShareSig on /api/chat keeps it 401
	// (no {attachment_id} to sign, so the HMAC never matches). It is kept as
	// executable documentation that a sig never generalises into a token, and
	// it is labelled here so nobody reads its green as evidence.
	for name, target := range map[string]string{
		"tampered sig":          "/api/chat/attachment/" + mine + "?sig=" + tampered,
		"another blob's sig":    "/api/chat/attachment/" + other + "?sig=" + mineSig,
		"empty sig":             "/api/chat/attachment/" + mine + "?sig=",
		"no credential at all":  "/api/chat/attachment/" + mine,
		"sig on the mint route": "/api/chat/attachments/" + mine + "/share-link?sig=" + mineSig,
		"sig on an unrelated row (non-discriminating, see comment)": "/api/chat?sig=" + mineSig,
	} {
		if status, body := doRaw(t, "GET", srv.URL+target, "", "", nil); status != 401 {
			t.Errorf("%s must be 401, got %d %s", name, status, body)
		}
	}
}

// TestShareLinkToolRefusesToMintIntoTheVoid — an unknown blob id surfaces the
// route's own 404 as an isError result. A link to a blob that does not exist
// is worse than an error: the agent would hand out a URL that 401s forever
// (the sig would verify, the blob lookup would not).
func TestShareLinkToolRefusesToMintIntoTheVoid(t *testing.T) {
	srv, secret, _ := newWiredTestServer(t)
	agentTok, _ := mintJWT("mira", "agent", 300, secret, time.Now().Unix(), "")

	res := shareLinkCall(t, srv.URL, agentTok, "att-doesnotexist")
	if res["isError"] != true {
		t.Fatalf("unknown blob id must surface the REST 404: %v", res)
	}
	sc, _ := res["structuredContent"].(map[string]any)
	errObj, _ := sc["error"].(map[string]any)
	if errObj["code"] != "not_found" {
		t.Fatalf("want a not_found envelope, got %v", res)
	}
}

// TestShareLinkToolMintsForEveryPrincipalDownToTheFloor drives the tool as the
// callers this change actually exists for. The other tests here mint as
// "mira", and mira is the SEEDED ASSISTANT — role_key "assistant", which
// classifyMember ranks admin_agent, two rungs above the row's floor. Every
// behavioural assertion above therefore proves only that an admin agent can
// mint, while the point of the reversal is that a plain member agent, an
// outsource worker and a warden can.
//
// Without this test, raising Requires to admin_agent — which would lock all
// three out and thereby remove the very capability this change adds — leaves
// every behavioural test in the file green, and is caught only by a string
// compare against the same struct literal the mutation edited.
func TestShareLinkToolMintsForEveryPrincipalDownToTheFloor(t *testing.T) {
	srv, secret, _ := newWiredTestServer(t)
	now := time.Now().Unix()

	// An off-roster sub is a plain agent (classifyMember: a nil row is never a
	// capability) — the shape an ow- outsource worker also takes.
	plainAgent, _ := mintJWT("ow-nobody", "agent", 300, secret, now, "")
	// The seeded server-self warden is kind=warden → principalMachine, the
	// actual floor of the row.
	warden, _ := mintJWT(ServerSelfHost, "agent", 300, secret, now, "")

	for name, tok := range map[string]string{
		"plain agent / outsource worker": plainAgent,
		"warden (the floor itself)":      warden,
	} {
		att, _ := uploadBlob(t, srv.URL, tok, "", []byte("bytes for "+name))["id"].(string)
		if att == "" {
			t.Fatalf("%s: upload failed — cannot test minting without a blob", name)
		}
		if want := "/api/chat/attachment/" + att + "?sig=" + shareSigFor(secret, att); shareLinkVia(t, srv.URL, tok, att) != want {
			t.Errorf("%s must be able to mint a share link at this row's floor", name)
		}
	}
}

// frozenCatalogCarries reports whether spec/mcp-catalog.json — the file
// tools/list is served from — advertises a tool by name.
func frozenCatalogCarries(t *testing.T, name string) bool {
	t.Helper()
	raw, err := os.ReadFile("../../spec/mcp-catalog.json")
	if err != nil {
		t.Fatalf("read frozen catalog: %v", err)
	}
	var catalog struct {
		Tools []struct {
			Name string `json:"name"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(raw, &catalog); err != nil {
		t.Fatalf("parse frozen catalog: %v", err)
	}
	for _, tool := range catalog.Tools {
		if tool.Name == name {
			return true
		}
	}
	return false
}

// shareLinkCall drives one tools/call of the mint tool and returns its result
// envelope (never a transport error — that is a test failure, not a case).
func shareLinkCall(t *testing.T, baseURL, token, attachmentID string) map[string]any {
	t.Helper()
	args, err := json.Marshal(map[string]string{"attachment_id": attachmentID})
	if err != nil {
		t.Fatal(err)
	}
	payload := postMCP(t, baseURL, token,
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"`+
			shareLinkToolName+`","arguments":`+string(args)+`}}`)
	if rpcErr, present := payload["error"]; present {
		t.Fatalf("expected a result envelope, got RPC error: %v", rpcErr)
	}
	res, _ := payload["result"].(map[string]any)
	if res == nil {
		t.Fatalf("no result envelope: %v", payload)
	}
	return res
}

// shareLinkVia mints a link through MCP and returns its url, failing the test
// on anything other than a clean mint.
func shareLinkVia(t *testing.T, baseURL, token, attachmentID string) string {
	t.Helper()
	res := shareLinkCall(t, baseURL, token, attachmentID)
	if res["isError"] != false {
		t.Fatalf("minting %s must succeed: %v", attachmentID, res)
	}
	sc, _ := res["structuredContent"].(map[string]any)
	url, _ := sc["url"].(string)
	if url == "" {
		t.Fatalf("mint result carries no url: %v", res)
	}
	return url
}
