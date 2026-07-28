package main

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

// T-b36a step 3 (the warden half) — the `dispatched %s OK` line must stop being
// printed for ops whose receipt never reached the server.
//
// The measured shape of the bug: on one host, 8 days of `ocwarden.out.log` held
// 5,805 `dispatched ... OK` lines and ZERO mentions of a receipt, a
// command_result or a non-2xx — because report()'s verdict was discarded by
// start/stop and the success line was printed on the strength of "dispatch
// returned no error". A receipt POST answered 500 produced a log that read
// exactly like a perfect one.
//
// This is NOT the ticket's owner-facing signal (the warden's log has no
// readers; the server-side receipt deadline is the signal). What it removes is
// the COUNTER-signal — a line that actively asserts the opposite of what
// happened.

// receiptFaultDeps builds deps whose op succeeds and whose receipt fails (or
// not, per reportErr) — the exact discrimination under test.
func receiptFaultDeps(reportErr error) (CommandDeps, *int) {
	reports := 0
	return CommandDeps{
		Spawn: func(StartParams) SpawnOutcome { return SpawnOutcome{OK: true, Reason: "spawned"} },
		Stop:  func(string) (bool, bool) { return true, false },
		Report: func(CommandResult) error {
			reports++
			return reportErr
		},
	}, &reports
}

const startFrame = `{"topic":"warden-command","data":{"rpc":"start","args":{` +
	`"member_id":"m-1","persona_context":"p","member_token":"t",` +
	`"session_name":"member-m-1"}}}`

const stopFrame = `{"topic":"warden-command","data":{"rpc":"stop","args":{` +
	`"member_id":"m-1","session_name":"member-m-1"}}}`

func joinLogs(logs *[]string) string { return strings.Join(*logs, "\n") }

// A START that SPAWNED but whose receipt was refused must not be logged as OK.
// Both directions are asserted in one test on purpose: the "no OK line" half
// alone is satisfied by any code path that stops logging entirely, and the
// success control proves the line still fires when it is earned.
func TestHandlePayload_StartWithUndeliveredReceiptIsNotLoggedAsOK(t *testing.T) {
	// Control: a DELIVERED receipt still earns the OK line.
	okDeps, okReports := receiptFaultDeps(nil)
	trOK, okLogs := newTestTransport(okDeps)
	trOK.handlePayload([]byte(startFrame))
	if *okReports != 1 {
		t.Fatalf("the control must have attempted exactly one receipt, got %d", *okReports)
	}
	if !strings.Contains(joinLogs(okLogs), "dispatched start OK") {
		t.Fatalf("a delivered receipt must still log the OK line, got:\n%s", joinLogs(okLogs))
	}

	// The case: same successful spawn, receipt refused by the server.
	badDeps, badReports := receiptFaultDeps(errors.New("command_result POST returned status 500"))
	trBad, badLogs := newTestTransport(badDeps)
	trBad.handlePayload([]byte(startFrame))

	if *badReports != 1 {
		t.Fatalf("the spawn must still have been reported (best-effort), got %d attempts", *badReports)
	}
	got := joinLogs(badLogs)
	if strings.Contains(got, "dispatched start OK") {
		t.Fatalf("a start whose receipt did NOT land must not be logged as OK, got:\n%s", got)
	}
	// Positive requirement — a silent path would satisfy the line above on its
	// own, so the named failure must actually be present, and it must say the
	// op RAN (calling this a refused dispatch would be a second false claim).
	if !strings.Contains(got, "EXECUTED but its receipt did not reach") {
		t.Fatalf("the receipt fault must be named as an EXECUTED op, got:\n%s", got)
	}
	if !strings.Contains(got, "status 500") {
		t.Fatalf("the underlying delivery verdict must survive into the log, got:\n%s", got)
	}
	if strings.Contains(got, "dispatch refused") {
		t.Fatalf("a receipt fault is not a refused dispatch — the op ran, got:\n%s", got)
	}
}

// The same for STOP. Its receipt carries the ladder's verdict (killed / no-op /
// incomplete), so a dropped stop receipt leaves the server with NO version of
// the outcome at all.
func TestHandlePayload_StopWithUndeliveredReceiptIsNotLoggedAsOK(t *testing.T) {
	okDeps, _ := receiptFaultDeps(nil)
	trOK, okLogs := newTestTransport(okDeps)
	trOK.handlePayload([]byte(stopFrame))
	if !strings.Contains(joinLogs(okLogs), "dispatched stop OK") {
		t.Fatalf("a delivered stop receipt must still log the OK line, got:\n%s", joinLogs(okLogs))
	}

	badDeps, badReports := receiptFaultDeps(errors.New("Post \"http://oc/api/telemetry\": dial tcp: connection refused"))
	trBad, badLogs := newTestTransport(badDeps)
	trBad.handlePayload([]byte(stopFrame))

	if *badReports != 1 {
		t.Fatalf("the stop must still have been reported, got %d attempts", *badReports)
	}
	got := joinLogs(badLogs)
	if strings.Contains(got, "dispatched stop OK") {
		t.Fatalf("a stop whose receipt did NOT land must not be logged as OK, got:\n%s", got)
	}
	if !strings.Contains(got, "EXECUTED but its receipt did not reach") {
		t.Fatalf("the receipt fault must be named, got:\n%s", got)
	}
	if !strings.Contains(got, "connection refused") {
		t.Fatalf("the transport cause must survive into the log, got:\n%s", got)
	}
}

// RANKING. When the spawn itself was REFUSED and the receipt also failed to
// land, the error must be the spawn's. Folding the two together would bury the
// actionable fact ("claude did not start") under the transport one, and would
// make errReceiptUndelivered mean two different things.
func TestDispatchCommand_SpawnRefusalOutranksTheReceiptFault(t *testing.T) {
	deps := CommandDeps{
		Spawn: func(StartParams) SpawnOutcome {
			return SpawnOutcome{OK: false, Reason: "boot_death: claude exited immediately"}
		},
		Report: func(CommandResult) error { return errors.New("status 500") },
	}
	cmd, err := parseCommandFrame([]byte(startFrame))
	if err != nil || cmd == nil {
		t.Fatalf("fixture frame must parse: %v", err)
	}
	dispatchErr := dispatchCommand(cmd, deps)
	if dispatchErr == nil {
		t.Fatalf("a refused spawn must still be an error")
	}
	if errors.Is(dispatchErr, errReceiptUndelivered) {
		t.Fatalf("a refused spawn must not be reclassified as a receipt fault: %v", dispatchErr)
	}
	if !strings.Contains(dispatchErr.Error(), "boot_death") {
		t.Fatalf("the spawn's own cause must be the reported one, got %v", dispatchErr)
	}
}

// The legacy worker_stop alias rides the same receipt, so it gets the same
// treatment — otherwise an old server reclaiming a worker keeps the silence.
func TestDispatchCommand_WorkerStopSurfacesAnUndeliveredReceipt(t *testing.T) {
	deps := CommandDeps{
		Stop:   func(string) (bool, bool) { return true, false },
		Report: func(CommandResult) error { return errors.New("status 503") },
	}
	frame := `{"topic":"warden-command","data":{"rpc":"worker_stop","args":{` +
		`"worker_id":"ow-1","session_name":"worker-ow-1"}}}`
	cmd, err := parseCommandFrame([]byte(frame))
	if err != nil || cmd == nil {
		t.Fatalf("fixture frame must parse: %v", err)
	}
	dispatchErr := dispatchCommand(cmd, deps)
	if !errors.Is(dispatchErr, errReceiptUndelivered) {
		t.Fatalf("an undelivered worker_stop receipt must surface as the receipt class, got %v", dispatchErr)
	}
}

// A stop that was itself INCOMPLETE keeps its own error even when the receipt
// also failed — the same ranking rule as the start arm, on the other verb.
func TestDispatchCommand_IncompleteWorkerStopOutranksTheReceiptFault(t *testing.T) {
	deps := CommandDeps{
		Stop:   func(string) (bool, bool) { return false, false },
		Report: func(CommandResult) error { return errors.New("status 500") },
	}
	frame := `{"topic":"warden-command","data":{"rpc":"worker_stop","args":{` +
		`"worker_id":"ow-1","session_name":"worker-ow-1"}}}`
	cmd, _ := parseCommandFrame([]byte(frame))
	dispatchErr := dispatchCommand(cmd, deps)
	if dispatchErr == nil || errors.Is(dispatchErr, errReceiptUndelivered) {
		t.Fatalf("an incomplete stop must keep its own error, got %v", dispatchErr)
	}
	if !strings.Contains(dispatchErr.Error(), "incomplete") {
		t.Fatalf("the stop's own verdict must be the reported one, got %v", dispatchErr)
	}
}

// UNINSTALL is untouched: its receipt is already load-bearing (the warden
// refuses to self-exit without a 2xx). A regression that routed it through the
// new receipt class would change what the warden does with its own life.
func TestDispatchCommand_UninstallReceiptContractUnchanged(t *testing.T) {
	exits := 0
	deps := CommandDeps{
		Stop:     func(string) (bool, bool) { return true, false },
		Teardown: func() (bool, string) { return true, "torn down" },
		Exit:     func(int) { exits++ },
		Report:   func(CommandResult) error { return fmt.Errorf("status 500") },
	}
	frame := `{"topic":"warden-command","data":{"rpc":"uninstall","args":{` +
		`"member_id":"w-1","session_name":"member-w-1"}}}`
	cmd, _ := parseCommandFrame([]byte(frame))
	err := dispatchCommand(cmd, deps)
	if err == nil || !strings.Contains(err.Error(), "NOT self-exiting") {
		t.Fatalf("an undelivered uninstall receipt must still block the self-exit, got %v", err)
	}
	if exits != 0 {
		t.Fatalf("the warden must not exit on an undelivered uninstall receipt; exits=%d", exits)
	}
}
