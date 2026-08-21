package main

// worker_offline_confirm_ted79_test.go — T-ed79 parity #13: a worker walking its
// close-out is no longer cut off by ONE offline sample.
//
// owner 2026-08-21 (rc-7df3deb21b3b) reversed the original card. The card said
// "give staff the worker's behaviour"; the owner ruled the other way, verbatim:
// 「反過來但是不要三分鐘這麼久 他重新連上線應該不需要這麼長」— so the WORKER side
// has to confirm first, and staff is left exactly as it is.
//
// The three faces below are the whole ruling. A window that only collects late
// (face 2) but never cancels (face 3) is a delay, not a confirmation.

import "testing"

// stopWorkerFixture puts an ACTIVE+ONLINE worker into an open 停止 epoch — the
// arm that collects with collectWorkerStop (kill, no respawn) — then takes its
// session away, which is the network blip under test.
func stopWorkerFixture(t *testing.T, api *apiServer, now float64) string {
	t.Helper()
	workerID := newActiveOnlineWorker(t, api)
	w, _ := api.dal.GetOutsourceWorker(workerID)
	w.DesiredState = DesiredStateOffline
	w.StoppingSince = now
	w.StoppedSince = 0.0
	w.RefocusSince = 0.0
	w.RefocusOp = ""
	if err := api.dal.PutOutsourceWorker(*w); err != nil {
		t.Fatalf("open the stop epoch: %v", err)
	}
	api.hub.DrainWardenCommands(ServerSelfHost)
	return workerID
}

func tickHandover(t *testing.T, api *apiServer, workerID string, now float64) int {
	t.Helper()
	w, err := api.dal.GetOutsourceWorker(workerID)
	if err != nil || w == nil {
		t.Fatalf("re-read worker: %v", err)
	}
	api.autoHandoverWorker(*w, now)
	return len(api.hub.DrainWardenCommands(ServerSelfHost))
}

// dropWorkerSession removes every listener the worker holds — the server-side
// view of a disconnect, blip or death alike. They are indistinguishable at this
// instant, and that is the whole reason the window exists.
func dropWorkerSession(t *testing.T, api *apiServer, workerID string) {
	t.Helper()
	l, err := api.hub.Connect(workerID, "") // takeover of whatever listener is held
	if err != nil {
		t.Fatalf("takeover before drop: %v", err)
	}
	api.hub.Disconnect(l)
	if api.hub.IsOnline(workerID) {
		t.Fatalf("fixture: %s still reads online after dropping its session", workerID)
	}
}

// ── face 1: inside the window nothing is collected ──────────────────────────

func TestWorkerHandover_OfflineInsideTheConfirmWindowIsNotCollected(t *testing.T) {
	const now = 100_000.0
	api := newTasksTestServer(t)
	api.noOutsource = true
	workerID := newActiveOnlineWorker(t, api)
	stampWorkerRefocus(t, api, workerID, now-10)
	api.hub.DrainWardenCommands(ServerSelfHost)
	dropWorkerSession(t, api, workerID)

	// The tick that FIRST sees it offline only arms the anchor.
	if got := tickHandover(t, api, workerID, now); got != 0 {
		t.Fatalf("the first offline observation must only arm the confirm window, "+
			"got %d warden frames", got)
	}
	// One cadence tick later, still well inside 90s.
	if got := tickHandover(t, api, workerID, now+30); got != 0 {
		t.Fatalf("30s of continuous offline is inside the confirm window — a worker "+
			"reconnecting from an ordinary backoff is still alive; got %d warden frames",
			got)
	}
	if got := tickHandover(t, api, workerID, now+workerOfflineConfirmGraceSecs-1); got != 0 {
		t.Fatalf("one second SHORT of the window must still not collect, got %d frames", got)
	}
	if fresh, _ := api.dal.GetOutsourceWorker(workerID); fresh.StoppedSince != 0 {
		t.Fatal("nothing inside the confirm window may latch stopped_since")
	}
}

// ── face 2: the window lapsing still collects (it is a delay, never a veto) ──

func TestWorkerHandover_OfflineForTheWholeConfirmWindowCollects(t *testing.T) {
	const now = 100_000.0
	api := newTasksTestServer(t)
	api.noOutsource = true
	workerID := newActiveOnlineWorker(t, api)
	stampWorkerRefocus(t, api, workerID, now-10)
	api.hub.DrainWardenCommands(ServerSelfHost)
	dropWorkerSession(t, api, workerID)

	tickHandover(t, api, workerID, now) // arms
	if got := tickHandover(t, api, workerID, now+workerOfflineConfirmGraceSecs); got != 2 {
		t.Fatalf("a worker offline for the WHOLE window is genuinely gone and must be "+
			"collected (stop+start), got %d warden frames — the window is a "+
			"confirmation, not a reprieve", got)
	}
	if fresh, _ := api.dal.GetOutsourceWorker(workerID); fresh.StoppedSince <= 0 {
		t.Fatal("the confirmed-offline collect must latch stopped_since")
	}
}

// ── face 3: a reconnect inside the window CANCELS it ────────────────────────

func TestWorkerHandover_ReconnectInsideTheConfirmWindowCancelsTheCollect(t *testing.T) {
	const now = 100_000.0
	api := newTasksTestServer(t)
	api.noOutsource = true
	workerID := newActiveOnlineWorker(t, api)
	stampWorkerRefocus(t, api, workerID, now-10)
	api.hub.DrainWardenCommands(ServerSelfHost)
	dropWorkerSession(t, api, workerID)

	tickHandover(t, api, workerID, now) // arms the anchor
	if _, err := api.hub.Connect(workerID, ""); err != nil {
		t.Fatalf("reconnect: %v", err)
	}
	if got := tickHandover(t, api, workerID, now+30); got != 0 {
		t.Fatalf("a reconnected worker must not be collected at all, got %d frames", got)
	}
	// …and the blip must not be REMEMBERED: dropping the session again starts a
	// fresh window rather than resuming the old one. Without the reset, this tick
	// is already past now+90 and would collect on the spot.
	dropWorkerSession(t, api, workerID)
	if got := tickHandover(t, api, workerID, now+31); got != 0 {
		t.Fatalf("the window must restart from the NEW disconnect, not from the one a "+
			"reconnect already answered; got %d frames", got)
	}
	if got := tickHandover(t, api, workerID, now+31+workerOfflineConfirmGraceSecs); got != 2 {
		t.Fatalf("the restarted window must still collect when it lapses, got %d frames", got)
	}
}

// ── the same three faces on the 停止 arm, which collects WITHOUT a respawn ───

func TestWorkerStop_OfflineIsConfirmedBeforeTheCloseOutIsCollected(t *testing.T) {
	const now = 100_000.0
	api := newTasksTestServer(t)
	api.noOutsource = true
	workerID := stopWorkerFixture(t, api, now)
	dropWorkerSession(t, api, workerID)

	tickHandover(t, api, workerID, now) // arms
	if got := tickHandover(t, api, workerID, now+30); got != 0 {
		t.Fatalf("a 停止 close-out must not be cut off by a 30s blip, got %d frames", got)
	}
	if got := tickHandover(t, api, workerID, now+workerOfflineConfirmGraceSecs); got != 1 {
		t.Fatalf("the confirmed-gone 停止 collect kills and does NOT respawn — want 1 "+
			"warden frame, got %d", got)
	}
}

// ── the number itself ───────────────────────────────────────────────────────

// The window is ONE constant so a later owner ruling is a one-line edit. It is
// also the floor of what an honest reconnect costs (idle-read watchdog 45s +
// backoff cap 15s + one 30s cadence tick), so a value below it starts cutting
// off workers that are still alive.
func TestWorkerOfflineConfirmGraceIsNinetySeconds(t *testing.T) {
	if workerOfflineConfirmGraceSecs != 90.0 {
		t.Errorf("workerOfflineConfirmGraceSecs = %v, want 90 — the worst-case honest "+
			"reconnect (45s idle-read watchdog + 15s backoff cap + one 30s cadence "+
			"tick). owner 2026-08-21 (rc-7df3deb21b3b) asked for shorter than the 180s "+
			"ZombieConfirmGrace, and 90 is that number with the doubling removed, not "+
			"a round guess.", workerOfflineConfirmGraceSecs)
	}
	if workerOfflineConfirmGraceSecs >= defaultReconcileConfig().ZombieConfirmGrace {
		t.Errorf("the worker confirm window (%v) must be SHORTER than ZombieConfirmGrace "+
			"(%v) — 「不要三分鐘這麼久」", workerOfflineConfirmGraceSecs,
			defaultReconcileConfig().ZombieConfirmGrace)
	}
}
