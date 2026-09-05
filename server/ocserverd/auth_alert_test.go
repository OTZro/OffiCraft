package main

import (
	"strings"
	"testing"
	"time"
)

// waitForAuthAlerts polls the assistant's mailbox for alert rows, because the
// dispatch is asynchronous by design. Returns the rows in stream order.
//
// It polls rather than sleeping a fixed amount so a slow machine makes the test
// slower, never redder.
func waitForAuthAlerts(t *testing.T, api *apiServer, want int) []ChatMessage {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	var got []ChatMessage
	for time.Now().Before(deadline) {
		msgs, err := api.dal.ListChat()
		if err != nil {
			t.Fatalf("list chat: %v", err)
		}
		got = got[:0]
		for _, m := range msgs {
			if m.Meta != nil && m.Meta["auth_alert"] != nil {
				got = append(got, m)
			}
		}
		if len(got) >= want {
			return got
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("only %d auth alert(s) landed within the deadline, want %d", len(got), want)
	return nil
}

// TestPasswordAcceptedWithAWrongCodeWarnsTheAssistant — the whole point of the
// alert. This refusal is the ONE on /api/login that carries information: the
// password was right, so it is out. Nothing about the throttle repairs that, so
// the server has to tell someone who can ask the owner to change it.
func TestPasswordAcceptedWithAWrongCodeWarnsTheAssistant(t *testing.T) {
	api := mfaAPI(t)
	armMFA(t, api)

	rec := callJSON(api.HandleLoginApiLoginPost, loginBody(mfaTestPassword, "000000"))
	if rec.Code != 401 {
		t.Fatalf("login with a wrong code = %d, want 401", rec.Code)
	}

	msgs := waitForAuthAlerts(t, api, 1)
	got := msgs[0]
	if got.Recipient != seedMiraID {
		t.Errorf("alert went to %q, want the seeded assistant %q", got.Recipient, seedMiraID)
	}
	if got.Sender != wireSystemSender {
		t.Errorf("alert sender = %q, want %q", got.Sender, wireSystemSender)
	}
	// The body must ask for the ACTION, not merely report an event — the reader
	// is an assistant deciding whether to interrupt the owner.
	if !strings.Contains(got.Body, "更換密碼") {
		t.Errorf("the alert never asks for the password to be changed:\n%s", got.Body)
	}
	// And it must never echo the credential it is reporting on.
	if strings.Contains(got.Body, mfaTestPassword) {
		t.Error("the alert body contains the password it is warning about")
	}
}

// TestAWrongPasswordRaisesNoAlert — the negative half, and the one that keeps
// the alert worth reading. Ordinary guessing is the noise this signal has to
// stand out from; an alert on every failed login is an alert nobody opens.
func TestAWrongPasswordRaisesNoAlert(t *testing.T) {
	api := mfaAPI(t)
	armMFA(t, api)

	for i := 0; i < 5; i++ {
		if rec := callJSON(api.HandleLoginApiLoginPost, loginBody("not-the-password", "000000")); rec.Code != 401 {
			t.Fatalf("wrong-password login = %d, want 401", rec.Code)
		}
	}
	// Give any goroutine that should not exist a chance to lose.
	time.Sleep(200 * time.Millisecond)
	msgs, err := api.dal.ListChat()
	if err != nil {
		t.Fatalf("list chat: %v", err)
	}
	for _, m := range msgs {
		if m.Meta != nil && m.Meta["auth_alert"] != nil {
			t.Fatalf("a WRONG password raised a password-exposed alert: %s", m.Body)
		}
	}
}

// TestAuthAlertIsRateLimitedAndFoldsTheCount — constraint (2). The trigger is
// attacker-controlled, so without this a stranger holding the leaked password
// could bury the assistant (and through her, the owner) under one chat row per
// attempt. One row per window, carrying the number it stands for.
func TestAuthAlertIsRateLimitedAndFoldsTheCount(t *testing.T) {
	api := mfaAPI(t)
	now := time.Now()

	// A burst inside one window: exactly one alert, for the FIRST attempt.
	for i := 0; i < 50; i++ {
		api.noteFactorRefusedAfterCorrectPassword(now.Add(time.Duration(i) * time.Second))
	}
	msgs := waitForAuthAlerts(t, api, 1)
	time.Sleep(200 * time.Millisecond) // let a second one land if it is going to
	msgs = waitForAuthAlerts(t, api, 1)
	if len(msgs) != 1 {
		t.Fatalf("%d alerts for a 50-attempt burst inside one %v window, want 1 — "+
			"the alert is a megaphone an attacker controls", len(msgs), authAlertInterval)
	}
	if n := alertAttempts(t, msgs[0]); n != 1 {
		t.Errorf("first alert reports %d attempts, want 1", n)
	}

	// Once the window has passed, the NEXT attempt reports everything folded
	// into it — the 49 suppressed ones plus itself.
	api.noteFactorRefusedAfterCorrectPassword(now.Add(authAlertInterval + time.Minute))
	msgs = waitForAuthAlerts(t, api, 2)
	if n := alertAttempts(t, msgs[1]); n != 50 {
		t.Errorf("second alert reports %d attempts, want 50 (49 suppressed + this one) — "+
			"a suppressed attempt must be counted, not discarded", n)
	}
	if !strings.Contains(msgs[1].Body, "50 次") {
		t.Errorf("the folded count is not in the sentence the assistant reads:\n%s", msgs[1].Body)
	}
}

func alertAttempts(t *testing.T, m ChatMessage) int {
	t.Helper()
	meta, ok := m.Meta["auth_alert"].(map[string]any)
	if !ok {
		t.Fatalf("alert meta is not an object: %#v", m.Meta["auth_alert"])
	}
	n, ok := meta["attempts"].(float64) // through JSON round-trip
	if !ok {
		t.Fatalf("alert meta has no numeric `attempts`: %#v", meta)
	}
	return int(n)
}

// TestAuthAlertDispatchIsAsynchronous — constraint (1), and the reason
// dispatchAuthAlert has a seam at all.
//
// 🔴 THIS IS A SECURITY TEST, NOT A PERFORMANCE ONE. The alert fires on exactly
// one login branch — the one where the password was RIGHT. Doing its work inline
// would make that branch measurably slower than the other three, which is
// precisely the bit TestFailedLoginsAllCostTheSameWallClock and the identical
// refusal message are spent hiding. Counterfactual: turn `go
// s.dispatchAuthAlert(count)` into a direct call and this fails by name.
func TestAuthAlertDispatchIsAsynchronous(t *testing.T) {
	api := mfaAPI(t)
	// A BOUNDED block, not an unbounded one. A deliverer that waits on a channel
	// would also catch the mutant, but by DEADLOCKING the package until the go
	// test timeout — a red that costs ten minutes and reads like an unrelated
	// hang. This makes the same mutant fail in two seconds, by name, on the
	// assertion that describes the property.
	const stall = 2 * time.Second
	entered := make(chan struct{})
	api.authAlertDeliver = func(int) {
		close(entered)
		time.Sleep(stall)
	}

	start := time.Now()
	api.noteFactorRefusedAfterCorrectPassword(time.Now())
	elapsed := time.Since(start)

	select {
	case <-entered:
	case <-time.After(stall):
		t.Fatal("the delivery never ran at all — the assertion below would be vacuous")
	}
	if elapsed > 100*time.Millisecond {
		t.Errorf("the login path waited %v for the alert to be delivered — that "+
			"latency appears ONLY on the branch where the password was correct, "+
			"which is exactly the bit the identical refusal is hiding", elapsed)
	}
}

// TestAuthAlertSurvivesAMissingAssistant — an install whose roster no longer has
// the seeded assistant has nobody to warn. That must be a logged no-op on a
// background goroutine, never a panic that takes the process down: the trigger
// is reachable by an unauthenticated caller.
func TestAuthAlertSurvivesAMissingAssistant(t *testing.T) {
	api := mfaAPI(t)
	mira, err := api.dal.GetMember(seedMiraID)
	if err != nil || mira == nil {
		t.Fatalf("seed assistant missing before the test even starts: %v", err)
	}
	mira.RosterStatus = RosterStatusRemoved
	if err := api.dal.PutMember(*mira); err != nil {
		t.Fatalf("remove assistant: %v", err)
	}

	api.deliverPasswordExposedAlert(3) // synchronous: the fault must be contained here

	msgs, err := api.dal.ListChat()
	if err != nil {
		t.Fatalf("list chat: %v", err)
	}
	for _, m := range msgs {
		if m.Meta != nil && m.Meta["auth_alert"] != nil {
			t.Fatalf("an alert was addressed to a member who is not in the roster: %+v", m)
		}
	}
}

// TestPasswordExposedAlertBodyPluralisesAndNamesTheWindow — the sentence is the
// whole product here, so pin the two things a reader acts on: how many, and how
// often this will come back.
func TestPasswordExposedAlertBodyPluralisesAndNamesTheWindow(t *testing.T) {
	one := passwordExposedAlertBody(1)
	if !strings.Contains(one, "1 次") {
		t.Errorf("a single attempt does not say so:\n%s", one)
	}
	many := passwordExposedAlertBody(812)
	if !strings.Contains(many, "812 次") {
		t.Errorf("the count is missing from the body:\n%s", many)
	}
	if !strings.Contains(many, "15 分鐘") {
		t.Errorf("the body does not tell the reader how often this repeats "+
			"(authAlertInterval = %v):\n%s", authAlertInterval, many)
	}
}
