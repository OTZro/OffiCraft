package main

// onboarding_contract_test.go — the SERVER half of a paired contract with the
// cockpit (T-8115).
//
// 🔴 WHAT THE COCKPIT IS ALLOWED TO ASSUME, AND WHY IT NEEDS A GUARD HERE.
// frontend/src/components/OnboardingBanner.tsx treats `onboarding: null` on a
// settings read as TERMINAL — it stops polling on the spot. That is only
// honest because of an invariant that lives entirely in THIS file's subject:
//
//     by the time POST /api/auth/set-password's 200 can reach any client, the
//     onboarding report row already exists (state "running"), UNLESS onboarding
//     is not going to run at all — and it is never retried, because
//     set-password is one-shot.
//
// Without that, a client could read null during a real first run, conclude
// "never ran", and go silent in the ONE situation the banner exists for: the
// verdict that lands ~30 s later (wardenOnlineWait). Before T-8115 the cockpit
// paid for that uncertainty by polling a 639 kB payload every 3 s for three
// minutes on EVERY cockpit open, on an install where the value is permanently
// null. The right fix was to make the invisible dependency a covered one,
// which is what these two tests are.
//
// The invariant rests on `kickFirstRunOnboardingWith` claiming the slot
// SYNCHRONOUSLY — before it starts its goroutine, therefore before the handler
// returns, therefore before net/http flushes the response. Move that write into
// the goroutine (or after the handler returns) and the cockpit's rule silently
// becomes a lie. That is what these guards are for.
//
// ⚠️ If you change either side, change both. The cockpit's half is pinned by
// frontend/src/components/OnboardingBanner.null-poll.test.tsx.

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

// TestOnboardingClaimIsPersistedBeforeKickReturns is the DETERMINISTIC half.
//
// The runner is held on a channel at the first seam the background goroutine
// reaches, so that goroutine provably cannot be the thing that wrote the row:
// if a report exists when the kick returns, the kick itself wrote it.
func TestOnboardingClaimIsPersistedBeforeKickReturns(t *testing.T) {
	t.Setenv("OC_NO_ONBOARDING", "")
	s := newReconcileTestServer(t)

	held := make(chan struct{})
	released := make(chan struct{})
	run := fakeOnboarding(s, bootstrapResultDTO{OK: true}, nil, true)
	// wardenInstalled is the first seam runFirstRunOnboarding consults that we
	// control; parking here keeps the goroutine short of EVERY report write.
	run.wardenInstalled = func() bool {
		close(held)
		<-released
		return true
	}

	s.kickFirstRunOnboardingWith(run)

	// The kick has RETURNED. Nothing a client could do can have happened
	// earlier than this point, so this is exactly what the cockpit sees.
	report := s.onboardingReport()
	if report == nil {
		t.Fatal("the onboarding slot must be claimed SYNCHRONOUSLY, before the " +
			"kick returns — the cockpit reads a null report as \"never ran\" and " +
			"stops polling (see OnboardingBanner.tsx isTerminal). A claim that " +
			"only lands from the background goroutine leaves a window in which a " +
			"real first run looks like an install that never onboarded, and the " +
			"~30s failure verdict is never shown to anyone.")
	}
	if report.State != onboardingStateRunning {
		t.Fatalf("the synchronous claim must be the RUNNING report, got %q "+
			"(a terminal state here means the background goroutine wrote it, "+
			"which is the very thing this test exists to rule out)", report.State)
	}
	if len(report.Steps) != 0 {
		t.Fatalf("the claim must carry no steps yet, got %d", len(report.Steps))
	}

	<-held
	close(released)
}

// TestSetPasswordLeavesNoNullOnboardingWindow is the same invariant at the
// wire, where the cockpit actually stands: the FIRST GET /api/settings a client
// can possibly make after its set-password 200 must not report a null
// onboarding.
//
// The first read is the whole assertion — a later one proves nothing, because
// the background goroutine has had time to write by then. Repeated over fresh
// servers so a claim moved off the synchronous path cannot pass by winning one
// scheduling race.
func TestSetPasswordLeavesNoNullOnboardingWindow(t *testing.T) {
	t.Setenv("OC_NO_ONBOARDING", "")
	const rounds = 20
	for i := 0; i < rounds; i++ {
		_, srv, _, claim := newSettingsTestServer(t, "")

		status, data := doJSON(t, "POST", srv.URL+"/api/auth/set-password", "",
			`{"password":"first-run-pass","claim_token":"`+claim+`"}`)
		if status != 200 {
			t.Fatalf("round %d: set-password: %d %v", i, status, data)
		}
		token, _ := data["token"].(string)
		if token == "" {
			t.Fatalf("round %d: set-password returned no token", i)
		}

		// The very next thing the cockpit does. No sleep, no retry.
		raw := getSettingsRaw(t, srv.URL, token)
		if raw["onboarding"] == nil {
			t.Fatalf("round %d: the FIRST settings read after set-password "+
				"reported onboarding=null. The cockpit reads that as \"onboarding "+
				"never ran\" and stops polling for good (OnboardingBanner.tsx "+
				"isTerminal), so the first-run failure verdict that lands ~30s "+
				"later would never be shown. The report must be claimed before "+
				"this response can exist — see kickFirstRunOnboardingWith.", i)
		}
	}
}

// getSettingsRaw reads GET /api/settings as a raw map so a null `onboarding`
// stays distinguishable from an absent one (a typed DTO would collapse both).
func getSettingsRaw(t *testing.T, base, token string) map[string]any {
	t.Helper()
	req, err := http.NewRequest("GET", base+"/api/settings", strings.NewReader(""))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		t.Fatalf("GET /api/settings: %d %s", resp.StatusCode, body)
	}
	var parsed map[string]any
	if err := json.Unmarshal(body, &parsed); err != nil {
		t.Fatalf("GET /api/settings: non-JSON body: %s", body)
	}
	if _, present := parsed["onboarding"]; !present {
		t.Fatalf("GET /api/settings carries no `onboarding` key at all — the " +
			"cockpit's whole first-run channel reads that field; its absence is " +
			"a wire break, not a null report")
	}
	return parsed
}
