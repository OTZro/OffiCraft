package main

import (
	"encoding/json"
	"maps"
	"net/http"
	"net/http/httptest"
	"sort"
	"testing"
)

// The codex sidecar's five uplinks, confronted with the schemas the server
// actually declares for the routes they post to.
//
// The previous version of this file only checked that each body PARSED as JSON and
// that the expected number of requests reached the expected paths. That is the
// shape of the original incident: the commit that tightened 30+ schemas would have
// left every one of those assertions green while all five bodies started coming
// back 422, because "is JSON" and "is the JSON this route accepts" are different
// questions and only the second one is the contract.
//
// So each request is intercepted, the schema is resolved from the ROUTE it was
// sent to (never by naming a DTO here — see frozenRequestSchema), and the body is
// walked against it: undeclared keys are named, and declared values whose wire type
// disagrees with the spec are named. Those two walkers are the same ones the warden
// heartbeat uses, so the sidecar is held to the same standard rather than a laxer
// one that happens to be easier to satisfy.
//
// The payloads come from the real producers. reportTokenUsage and reportRateLimits
// are handed the shape the codex CLI actually emits and build the uplink body
// themselves; a literal typed out here would keep matching the spec after the
// producer drifted, which is exactly how five of these uplinks used to be
// "covered".
func TestCodexUplinkBodiesMatchFrozenRequestSchemas(t *testing.T) {
	type capture struct {
		route string
		body  map[string]any
	}
	var sent []capture
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("%s body is not a JSON object: %v", r.URL.Path, err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		sent = append(sent, capture{route: r.URL.Path, body: body})
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "rc-wire"})
	}))
	defer srv.Close()

	session := &codexSession{base: srv.URL, token: "wire-token", account: "codex:wire"}

	// drive runs one producer and requires it to have actually put a body on the
	// wire. A producer that returns early — a throttle stamp, a missing token, an
	// empty snapshot — sends nothing, and nothing satisfies every comparison below
	// without a single key ever being compared. That is the same "silence looks like
	// success" failure this whole gate exists for, so it is caught per uplink rather
	// than by a total that has to be edited every time one is added.
	// Every send is attributed to the producer run that caused it, so the join below
	// is per uplink rather than per route. Without the attribution, three uplinks on
	// /api/monitoring/telemetry are interchangeable: review added a fourth whose body
	// would 422 in production and paid for it by calling an OLD producer one more
	// time — same route, same total, nothing new ever compared.
	driven := map[string]int{}
	drive := func(name string, produce func()) {
		t.Run(name, func(t *testing.T) {
			before := len(sent)
			produce()
			if len(sent) == before {
				t.Fatalf("%s put no body on the wire, so it was never compared against "+
					"anything — a producer that sends nothing passes every check below", name)
			}
			for _, one := range sent[before:] {
				driven[name+" → "+one.route]++
			}
		})
	}

	// One uplink per subtest, driving the REAL producers. Each subtest name is the
	// evidence cli/uplinks.json points at, and the guard forbids two uplinks naming
	// the same one — so a new uplink cannot pass by borrowing another's assertion.
	drive("identity", session.reportIdentity)
	drive("context-and-tokens", func() {
		session.reportTokenUsage(map[string]any{"tokenUsage": map[string]any{
			"modelContextWindow": float64(100),
			"last":               map[string]any{"totalTokens": float64(1)},
			"total": map[string]any{
				"inputTokens": float64(1), "outputTokens": float64(2),
				"cachedInputTokens": float64(3), "reasoningOutputTokens": float64(4),
			},
		}})
	})
	drive("rate-limits", func() {
		session.reportRateLimits(map[string]any{
			"primary":   map[string]any{"windowDurationMins": float64(300), "usedPercent": float64(1)},
			"secondary": map[string]any{"windowDurationMins": float64(10080), "usedPercent": float64(0)},
		})
	})
	drive("reply-card", func() {
		session.openReplyCard(map[string]any{"header": "wire", "question": "ok"}, "")
	})

	// The join: what the manifest hangs on this test, per route, against the routes
	// the real producers just posted to. Per route rather than a total on purpose —
	// a total is satisfiable by driving an already-covered producer a second time,
	// which review demonstrated with a live uplink that was never once sent.
	got := driven
	want := manifestUplinkPaths(t, "cli/ocwarden/codex_uplink_wire_test.go")
	if !maps.Equal(got, want) {
		t.Fatalf("cli/uplinks.json commits %v to this wire test, but the producers put "+
			"%v on the wire. A committed uplink nobody drove is a row that documents "+
			"coverage it does not have — the incident's own gap.", want, got)
	}

	for _, one := range sent {
		declared := frozenRequestSchema(t, "post", one.route)
		if extra := undeclaredPayloadKeys(one.body, declared); len(extra) > 0 {
			t.Errorf("POST %s carries key(s) the frozen spec does not declare: %v.\n"+
				"The server decodes this route with DisallowUnknownFields, so one such key "+
				"422s the WHOLE body — this uplink would go dark in production while the "+
				"sidecar reported nothing wrong. Body: %v", one.route, extra, one.body)
		}
		if missing := missingRequiredKeys(one.body, declared); len(missing) > 0 {
			t.Errorf("POST %s omits key(s) the frozen spec requires: %v.\n"+
				"The server refuses a body missing a required field, so this uplink is "+
				"already 422ing in production if the spec shipped ahead of the producer — "+
				"and nothing else here would have noticed. Body: %v", one.route, missing, one.body)
		}
		if bad := mistypedPayloadValues(one.body, declared); len(bad) > 0 {
			sort.Strings(bad)
			t.Errorf("POST %s sends declared key(s) with the wrong wire type: %v.\n"+
				"A declared key with an unexpected type is either refused outright or stored "+
				"and then read back as null forever, which looks identical to never having "+
				"reported at all. Body: %v", one.route, bad, one.body)
		}
	}
}
