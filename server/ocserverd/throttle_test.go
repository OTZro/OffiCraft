package main

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

// TestThrottleConstantsAreAbsolute — the throttle tests used to be
// SELF-REFERENTIAL: they wrote their expectations as `throttleBaseDelay`,
// `2*throttleBaseDelay`, `throttleMaxDelay`, so changing a constant moved both
// sides of the assertion together and every stated security property went
// unguarded (raising the cap to 50 minutes stayed green).
//
// The schedule those constants belonged to is gone, but the trap is not, so
// this pins the two numbers that are left in ABSOLUTE terms. Both are quoted as
// arithmetic in the prose around them, and a silent edit to either changes what
// the front door actually costs an attacker.
func TestThrottleConstantsAreAbsolute(t *testing.T) {
	if throttleFailureFloor != 3*time.Second {
		t.Errorf("failure floor = %v, want 3s — the owner's ruling, and the number "+
			"the '~1.3 guesses a second' arithmetic rests on", throttleFailureFloor)
	}
	if throttleMaxInFlight != 4 {
		t.Errorf("in-flight cap = %d, want 4 (bounds argon2id memory at ~76 MiB)", throttleMaxInFlight)
	}
	// The front door's ceiling, written as the arithmetic rather than as prose:
	// the cap divided by the floor.
	if perSecond := float64(throttleMaxInFlight) / throttleFailureFloor.Seconds(); perSecond > 1.5 {
		t.Errorf("front-door ceiling = %.2f guesses/s, want <= 1.5", perSecond)
	}
}

// TestFailureFloorDefaultsToProductionWhenUnset — the override field must fail
// SAFE. A zero value that meant "no floor" would be one forgotten line away
// from a server with no brake and nothing red to say so.
func TestFailureFloorDefaultsToProductionWhenUnset(t *testing.T) {
	var s apiServer
	if got := s.failureFloor(); got != throttleFailureFloor {
		t.Errorf("a server that never set the override has floor %v, want the "+
			"production %v — the zero value must mean the constant, not 'off'",
			got, throttleFailureFloor)
	}
	s.credentialFailureFloor = 5 * time.Millisecond
	if got := s.failureFloor(); got != 5*time.Millisecond {
		t.Errorf("override ignored: floor = %v, want 5ms", got)
	}
}

// TestHoldFailureFloorIsADeadlineNotASleep — the property the whole design
// rests on. `sleep(floor)` would make a refusal cost `work + floor`, so every
// difference in `work` still shows on the wire; `wait until start+floor` makes
// every refusal cost the same however much work it did.
//
// Asserted as: a call that has ALREADY burned most of the floor still returns
// at about the same total, and one that has burned all of it does not wait at
// all. Both bounds are far from the scheduler's noise.
func TestHoldFailureFloorIsADeadlineNotASleep(t *testing.T) {
	const floor = 200 * time.Millisecond
	s := &apiServer{credentialFailureFloor: floor}

	// Nothing spent yet: the whole floor is owed.
	started := time.Now()
	s.holdFailureFloor(started)
	if elapsed := time.Since(started); elapsed < floor {
		t.Errorf("a fresh refusal returned after %v, want at least the floor %v", elapsed, floor)
	}

	// Half the floor already spent: the REMAINDER is owed, not another whole
	// floor. An additive sleep would land near 1.5x here.
	started = time.Now().Add(-floor / 2)
	call := time.Now()
	s.holdFailureFloor(started)
	total := time.Since(started)
	if total < floor {
		t.Errorf("total wall-clock %v < floor %v — the deadline was not honoured", total, floor)
	}
	if waited := time.Since(call); waited > floor {
		t.Errorf("waited a further %v after half the floor was already spent — "+
			"that is sleep(floor), not wait-until(start+floor)", waited)
	}

	// Already past the deadline: no wait at all.
	call = time.Now()
	s.holdFailureFloor(time.Now().Add(-2 * floor))
	if waited := time.Since(call); waited > floor/2 {
		t.Errorf("waited %v on a refusal that had already outlived the floor", waited)
	}
}

// TestThrottleBeginReservesUnderConcurrency — the gate must admit at most
// throttleMaxInFlight callers at once. A gate that only inspects state reserves
// nothing, so a burst walks straight through it: N guesses per floor instead of
// one, and N concurrent argon2id verifications at ~19 MiB each.
func TestThrottleBeginReservesUnderConcurrency(t *testing.T) {
	var th credentialThrottle
	const goroutines = 200

	var start, done sync.WaitGroup
	start.Add(1)
	var mu sync.Mutex
	admitted := 0
	releases := []func(){}
	for i := 0; i < goroutines; i++ {
		done.Add(1)
		go func() {
			defer done.Done()
			start.Wait()
			release, _, blocked := th.begin()
			if blocked {
				return
			}
			mu.Lock()
			admitted++
			releases = append(releases, release)
			mu.Unlock()
		}()
	}
	start.Done()
	done.Wait()

	// Nothing released yet, so the pool must be exactly full — never more.
	if admitted != throttleMaxInFlight {
		t.Fatalf("%d of %d concurrent callers admitted, want exactly %d — the gate "+
			"is not reserving, so a burst bypasses the floor entirely",
			admitted, goroutines, throttleMaxInFlight)
	}
	// Releasing frees the slots again.
	for _, r := range releases {
		r()
	}
	if _, _, blocked := th.begin(); blocked {
		t.Error("still blocked after every slot was released — releases leak")
	}
}

// TestThrottleReleaseIsIdempotent — a defer plus an explicit call must not
// double-free, or the pool drifts upward and the cap silently stops holding.
func TestThrottleReleaseIsIdempotent(t *testing.T) {
	var th credentialThrottle
	release, _, blocked := th.begin()
	if blocked {
		t.Fatal("fresh throttle blocked")
	}
	for i := 0; i < 5; i++ {
		release()
	}
	// Exactly the pool size must be available — not more.
	got := 0
	for i := 0; i < throttleMaxInFlight+3; i++ {
		if _, _, b := th.begin(); !b {
			got++
		}
	}
	if got != throttleMaxInFlight {
		t.Errorf("after %d releases of ONE slot the pool admitted %d, want %d", 5, got, throttleMaxInFlight)
	}
}

// TestThrottleZeroValueAdmits — a fresh throttle must not refuse anyone. There
// is no history for it to carry any more, so this is the whole of its idle
// state.
func TestThrottleZeroValueAdmits(t *testing.T) {
	var th credentialThrottle
	if _, _, blocked := th.begin(); blocked {
		t.Fatal("a fresh throttle must not block")
	}
}

// TestWriteThrottledShape pins the wire face: 429, a Retry-After the client can
// act on without parsing prose, and the SAME error envelope as every other
// refusal (code derived from the status, so the closed vocabulary is unchanged).
//
// The cockpit branches on exactly this (LoginPage.tsx, ProfileDropdown.tsx), so
// it survives §0 unchanged even though the deadline it used to report is gone.
func TestWriteThrottledShape(t *testing.T) {
	rec := httptest.NewRecorder()
	writeThrottled(rec, 42*time.Second)

	if rec.Code != http.StatusTooManyRequests {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusTooManyRequests)
	}
	if got := rec.Header().Get("Retry-After"); got != "42" {
		t.Errorf("Retry-After = %q, want %q", got, "42")
	}
	body := decodeBody[struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}](t, rec)
	if want := errorCodeForStatus(http.StatusTooManyRequests); body.Error.Code != want {
		t.Errorf("envelope code = %q, want %q (the status-derived code)", body.Error.Code, want)
	}
	if body.Error.Message == "" {
		t.Error("envelope message is empty")
	}
}

// TestWriteThrottledRoundsUpAndFloorsAtOne — a sub-second wait must never
// render as "Retry-After: 0", which a client reads as "go ahead now".
func TestWriteThrottledRoundsUpAndFloorsAtOne(t *testing.T) {
	for _, tc := range []struct {
		wait time.Duration
		want string
	}{
		{0, "1"},
		{1 * time.Millisecond, "1"},
		{throttleBurstWait, "1"},
		{1500 * time.Millisecond, "2"},
		{5 * time.Minute, "300"},
	} {
		rec := httptest.NewRecorder()
		writeThrottled(rec, tc.wait)
		if got := rec.Header().Get("Retry-After"); got != tc.want {
			t.Errorf("wait %v → Retry-After %q, want %q", tc.wait, got, tc.want)
		}
	}
}
