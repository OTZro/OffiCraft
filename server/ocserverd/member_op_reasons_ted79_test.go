package main

// member_op_reasons_ted79_test.go — T-ed79 parity #4 and #14: a staff member
// that did not move must say why, in the SAME vocabulary a worker uses.
//
// The worker side has carried a closed family of last_op_reason codes since the
// relocate-that-goes-nowhere report: "EVERY non-dispatch now leaves a receipt".
// Staff shared the FIELD (last_op / last_op_reason), the CLEARING seam
// (isPlacementBlockedReason) and the cockpit renderer — and produced exactly two
// of the codes. Everything else came back as a clean 200 with nothing on the row,
// or as a reconcileLog line on the server's stderr that no owner will ever read.
//
// The gates below are the ones an owner actually presses (#4 is the first two:
// 「被按住時沒有為什麼沒動」).

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func heldDownMember(t *testing.T, s *apiServer, id string) {
	t.Helper()
	m := testAgent(id)
	m.DesiredState = DesiredStateOffline // the owner pressed 停止
	m.DesiredMachineID = "mach-a"
	putTestMember(t, s, m)
}

func reasonOf(t *testing.T, s *apiServer, id string) string {
	t.Helper()
	m, err := s.dal.GetMember(id)
	if err != nil || m == nil {
		t.Fatalf("re-read member %s: %v", id, err)
	}
	return m.LastOpReason
}

// ── #4 / G1: a launch-intent edit on a member the owner is holding down ──────

func TestUpdateMemberOnAHeldDownMemberLeavesAReceipt(t *testing.T) {
	s := newReconcileTestServer(t)
	putWarden(t, s, "mach-a")
	heldDownMember(t, s, "m-held")

	rec := httptest.NewRecorder()
	s.HandleUpdateMemberApiMembersMemberIdPatch(rec,
		taskReq(t, "PATCH", "/api/members/m-held",
			map[string]any{"model": "claude-opus-4-9"}, wireOwnerID, "owner"), "m-held")
	if rec.Code != http.StatusOK {
		t.Fatalf("update: %d %s", rec.Code, rec.Body.String())
	}

	got := reasonOf(t, s, "m-held")
	if !strings.HasPrefix(got, spawnReasonHeldDown+":") {
		t.Errorf("last_op_reason = %q, want a %q receipt. The value WAS saved and "+
			"nothing was started, and there are three different ways to be in that "+
			"state — held down, offline, already collected — which all collapse into "+
			"one clean 200. The worker face has written this receipt since the "+
			"reason-code family landed.", got, spawnReasonHeldDown)
	}
}

// ── #4 / G2: a 改機器 on a member the owner is holding down ──────────────────

func TestRelocateAHeldDownMemberLeavesAReceipt(t *testing.T) {
	s := newReconcileTestServer(t)
	putWarden(t, s, "mach-a")
	putWarden(t, s, "mach-b")
	heldDownMember(t, s, "m-heldmove")

	rec := httptest.NewRecorder()
	s.HandleRelocateMemberApiMembersMemberIdRelocatePost(rec,
		taskReq(t, "POST", "/api/members/m-heldmove/relocate",
			map[string]any{"machine_id": "mach-b"}, wireOwnerID, "owner"), "m-heldmove")
	if rec.Code != http.StatusOK {
		t.Fatalf("relocate: %d %s", rec.Code, rec.Body.String())
	}
	if got := reasonOf(t, s, "m-heldmove"); !strings.HasPrefix(got, spawnReasonHeldDown+":") {
		t.Errorf("last_op_reason = %q, want a %q receipt: the pin was stored and "+
			"nothing was moved, and the row says nothing about which of those two "+
			"halves happened", got, spawnReasonHeldDown)
	}
}

// ── #14 / G3: activation_pending must say WHICH pending ─────────────────────

func TestActivatePendingNamesItsCause(t *testing.T) {
	s := newReconcileTestServer(t)
	putWarden(t, s, "mach-gone") // a machine row, but its warden holds no live SSE
	m := testAgent("m-nowhere")
	m.DesiredState = DesiredStateOffline
	m.DesiredMachineID = "mach-gone"
	putTestMember(t, s, m)

	rec := httptest.NewRecorder()
	s.HandleActivateMemberApiMembersMemberIdActivatePost(rec,
		taskReq(t, "POST", "/api/members/m-nowhere/activate", nil, wireOwnerID, "owner"),
		"m-nowhere")
	if rec.Code != http.StatusOK {
		t.Fatalf("activate: %d %s", rec.Code, rec.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body["activation_pending"] != true {
		t.Fatalf("fixture: this activate was supposed to end pending, body=%s",
			rec.Body.String())
	}
	got := reasonOf(t, s, "m-nowhere")
	if strings.TrimSpace(got) == "" {
		t.Errorf("activation_pending=true with an EMPTY last_op_reason. The flag says " +
			"'nothing has been dispatched yet' and the code itself lists at least four " +
			"different states that reach it (backoff, circuit-open, an unbuildable " +
			"frame, an unreachable warden). One bit cannot answer 'why'.")
	}
}

// ── #14 / G4: the diagnoses that only ever reached stderr ────────────────────

func tickWithState(t *testing.T, s *apiServer, id string, st reconcileState, now float64) {
	t.Helper()
	m, _ := s.dal.GetMember(id)
	s.reconcileMu.Lock()
	s.reconcileStates[id] = st
	s.reconcileTickMemberLocked(*m, now)
	s.reconcileMu.Unlock()
}

func TestStalledWakeDiagnosesLandOnTheRowNotOnlyOnStderr(t *testing.T) {
	for _, tc := range []struct {
		name string
		code string
		st   func() reconcileState
	}{
		{
			name: "circuit open",
			code: spawnReasonCircuitOpen,
			st: func() reconcileState {
				st := newReconcileState()
				st.CircuitOpen = true
				st.CircuitCooldownUntil = 9_000_000.0
				return st
			},
		},
		{
			name: "backoff",
			code: spawnReasonBackoff,
			st: func() reconcileState {
				st := newReconcileState()
				st.BackoffUntil = 9_000_000.0
				return st
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := newReconcileTestServer(t)
			putWarden(t, s, "mach-a")
			m := testAgent("m-stalled")
			m.DesiredState = DesiredStateOnline
			m.DesiredMachineID = "mach-a"
			putTestMember(t, s, m)

			tickWithState(t, s, "m-stalled", tc.st(), 1_000_000.0)

			got := reasonOf(t, s, "m-stalled")
			if !strings.HasPrefix(got, tc.code+":") {
				t.Errorf("last_op_reason = %q, want a %q receipt. This member wants to "+
					"be online and the tick knows exactly why it is not being started; "+
					"today that sentence goes to the server's stderr and the cockpit "+
					"shows an unexplained grey row — the exact blank the worker "+
					"reason-code family was written to remove.", got, tc.code)
			}
		})
	}
}

// …and the other direction: a member that IS converged owes nobody an
// explanation, so the tick must not start writing receipts at every member.
func TestAConvergedMemberIsNotStampedWithAReason(t *testing.T) {
	s := newReconcileTestServer(t)
	putWarden(t, s, "mach-a")
	m := testAgent("m-fine")
	m.DesiredState = DesiredStateOnline
	m.DesiredMachineID = "mach-a"
	putTestMember(t, s, m)
	connectOnlineMachine(t, s, "m-fine", "mach-a")

	tickWithState(t, s, "m-fine", newReconcileState(), 1_000_000.0)

	if got := reasonOf(t, s, "m-fine"); strings.TrimSpace(got) != "" {
		t.Errorf("a converged online member was stamped %q — 'online: converged' is "+
			"not a stall and owes no receipt; stamping it would turn every healthy "+
			"member into a permanent SSE event stream", got)
	}
}

// 🔴 The single-slot precedence rule. wake_timeout says the start was dispatched
// and the agent never came up; the very next tick is the BACK-OFF that follows
// it. Both want the one last_op_reason slot, and the retry's description must
// not erase the previous attempt's diagnosis.
func TestBackoffDoesNotEraseTheWakeTimeoutDiagnosis(t *testing.T) {
	s := newReconcileTestServer(t)
	putWarden(t, s, "mach-a")
	m := testAgent("m-lapsed")
	m.DesiredState = DesiredStateOnline
	m.DesiredMachineID = "mach-a"
	m.LastOp = reconcileCmdStart
	m.LastOpReason = wakeTimeoutReasonCode + ": dispatched, never came up"
	putTestMember(t, s, m)

	st := newReconcileState()
	st.BackoffUntil = 9_000_000.0
	tickWithState(t, s, "m-lapsed", st, 1_000_000.0)

	if got := reasonOf(t, s, "m-lapsed"); !strings.HasPrefix(got, wakeTimeoutReasonCode+":") {
		t.Errorf("last_op_reason = %q — the back-off receipt overwrote the wake_timeout "+
			"diagnosis. 'we are waiting to retry' is a description of the wait; "+
			"'the start went out and nothing came up' is the only sentence that says "+
			"what went wrong, and it is the one the retry loop would blank on every "+
			"tick.", got)
	}
}
