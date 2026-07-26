package main

// sse_warden_requeue_test.go — T-e0e3 O1: the warden-command band must not LOSE
// frames when a write fails midway. DrainWardenCommands empties the whole FIFO up
// front, so the failing frame and every frame behind it exist only in the
// handler's local slice; returning without putting them back discarded them
// silently, with nothing written to the row, the log, or the queue. That is how a
// dispatched START could reach no warden at all and leave no trace — the class of
// failure behind "怎麼改機器都起不來" with a silent cockpit.
//
// Driven against the REAL handler loop (not the hub primitive alone — that is
// covered in hub_test.go), because the loss lived in the handler's control flow.
//
// File placement follows the entrenched sibling convention for stream-loop
// concerns (sse_takeover_test.go, sse_writedeadline_test.go) rather than
// mirroring api_infra.go — flagged as a deliberate divergence.

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

// failOnMarkerConn is a ResponseWriter+Flusher that writes everything happily
// EXCEPT a payload containing failMarker, which errors — the shape of a socket
// that dies partway through a burst. Writes are recorded so the test can prove
// which frames actually left.
type failOnMarkerConn struct {
	mu         sync.Mutex
	hdr        http.Header
	failMarker []byte
	written    [][]byte
	failed     chan struct{}
	failOnce   sync.Once
}

func newFailOnMarkerConn(marker string) *failOnMarkerConn {
	return &failOnMarkerConn{hdr: http.Header{}, failMarker: []byte(marker),
		failed: make(chan struct{})}
}

func (c *failOnMarkerConn) Header() http.Header { return c.hdr }
func (c *failOnMarkerConn) WriteHeader(int)     {}
func (c *failOnMarkerConn) Flush()              {}

func (c *failOnMarkerConn) Write(p []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.failMarker) > 0 && bytes.Contains(p, c.failMarker) {
		c.failOnce.Do(func() { close(c.failed) })
		return 0, errors.New("connection died mid-burst")
	}
	frame := make([]byte, len(p))
	copy(frame, p)
	c.written = append(c.written, frame)
	return len(p), nil
}

func (c *failOnMarkerConn) sawWritten(needle string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, f := range c.written {
		if bytes.Contains(f, []byte(needle)) {
			return true
		}
	}
	return false
}

// runWardenBand connects a WARDEN through the real events handler with the given
// writer, having pre-queued frames, and waits for the handler to return (or for
// the write failure, whichever the case). Returns once the handler goroutine is
// done so the FIFO can be inspected without racing it.
func runWardenBand(t *testing.T, wardenID string, frames []string,
	w *failOnMarkerConn) *apiServer {
	t.Helper()
	api, dal := newGateTestAPI(t)
	putGateMember(t, dal, Member{ID: wardenID, Kind: KindWarden,
		DesiredState: DesiredStateOnline})
	for _, f := range frames {
		api.hub.EnqueueWardenCommand(wardenID, []byte(f))
	}

	ctx, cancel := context.WithCancel(context.Background())
	req := httptest.NewRequest("GET", "/api/events", nil)
	claims := map[string]any{"sub": wardenID, "scope": "agent"}
	req = req.WithContext(context.WithValue(ctx, claimsContextKey, claims))

	done := make(chan struct{})
	go func() {
		api.HandleEventsApiEventsGet(w, req)
		close(done)
	}()

	select {
	case <-w.failed:
		// The handler returns on its own after the failed write.
	case <-time.After(3 * time.Second):
		// No failure expected in the sentinel case — let the drain happen, then
		// cancel so the handler unwinds.
	}
	cancel()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("handler goroutine did not return")
	}
	return api
}

// TestEventsHandler_FailedWardenWriteRequeuesUndeliveredFramesInOrder: three
// queued frames, the write of the SECOND fails. The first is gone (delivered),
// and frames two and three must be back in the FIFO IN THEIR ORIGINAL ORDER.
//
// The discriminator is the hub's own backlog (PendingWardenCommands), NOT "did a
// write happen": a test that judged this by writes would be satisfied by a
// handler that wrote nothing and dropped everything. Order is then asserted
// frame-by-frame, because a re-queue that restores the right COUNT in the wrong
// ORDER fails in a shape indistinguishable from success by a count assertion —
// and out-of-order warden commands are actively dangerous (a `stop` that
// overtakes its own `start` reaps the session that start was creating).
func TestEventsHandler_FailedWardenWriteRequeuesUndeliveredFramesInOrder(t *testing.T) {
	w := newFailOnMarkerConn("frame-BBB")
	api := runWardenBand(t, "w-req", []string{"frame-AAA", "frame-BBB", "frame-CCC"}, w)

	// Discriminator: the backlog itself.
	if got := api.hub.PendingWardenCommands("w-req"); got != 2 {
		t.Fatalf("want the 2 undelivered frames back in the FIFO, got %d pending "+
			"(0 = the silent loss this fixes)", got)
	}
	// Only now inspect CONTENT — the drain here is verification of the queue that
	// the assertion above already established, not the discriminator.
	back := api.hub.DrainWardenCommands("w-req")
	want := []string{"frame-BBB", "frame-CCC"}
	if len(back) != len(want) {
		t.Fatalf("want %d restored frames, got %d (%q)", len(want), len(back), back)
	}
	for i := range want {
		if string(back[i]) != want[i] {
			t.Fatalf("restored frame[%d] = %q, want %q — the undelivered tail must "+
				"keep its original order: %q", i, back[i], want[i], back)
		}
	}
	// Sanity on the delivered side: the first frame DID go out, so it must not be
	// re-queued (that would be a duplicate START, not a lost one).
	if !w.sawWritten("frame-AAA") {
		t.Error("precondition: the first frame should have been written")
	}
	for _, f := range back {
		if bytes.Contains(f, []byte("frame-AAA")) {
			t.Errorf("a frame that WAS delivered must not be re-queued: %q", back)
		}
	}
}

// TestEventsHandler_SuccessfulWardenDrainClearsTheQueue is the SENTINEL for the
// obvious over-correction: if fear of loss turned the drain into "never remove
// anything", every warden would be re-sent the same START forever. A healthy
// connection must still empty the FIFO exactly once.
func TestEventsHandler_SuccessfulWardenDrainClearsTheQueue(t *testing.T) {
	w := newFailOnMarkerConn("") // nothing ever fails
	api := runWardenBand(t, "w-ok", []string{"frame-AAA", "frame-BBB", "frame-CCC"}, w)

	if got := api.hub.PendingWardenCommands("w-ok"); got != 0 {
		t.Fatalf("a fully delivered drain must leave the FIFO EMPTY, got %d pending "+
			"(non-zero = infinite re-delivery)", got)
	}
	for _, f := range []string{"frame-AAA", "frame-BBB", "frame-CCC"} {
		if !w.sawWritten(f) {
			t.Errorf("frame %s was never written", f)
		}
	}
}
