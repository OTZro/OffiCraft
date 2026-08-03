package main

import (
	"bytes"
	"fmt"
	"io"
	"time"
)

// ---------------------------------------------------------------------------
// Event timestamps on the transcript (T-7fb2).
//
// WHY: without a stamp an agent reading its own transcript only knows WHEN IT
// PROCESSED a line, never when the thing happened. That gap is not academic —
// a reconnect delivers queued frames long after they occurred, so "I saw the
// disconnect notice at X" bounds nothing. The tester who asked for this could
// pin a deploy cutover to 2 seconds and still could not say how the SSE drop
// related to it, purely because the line carried no time of its own.
//
// TWO DIFFERENT CLOCKS, SAID OUT LOUD. A frame-derived line stamps the SERVER's
// own `ts` (spec/sse.md §3: every frame carries one) — that is when the event
// HAPPENED. A line with no frame behind it (connect, stream ended, the
// mis-wire notice) can only stamp THIS machine's clock at emission, which is a
// different quantity. Rather than let one syntax silently mean two things, the
// local form says `local`. A reader must never have to guess which clock a
// number came from.
//
// WHERE THE SUFFIX GOES — and why not the front. `cli/ocwarden` classifies
// ocagent's stdout by PREFIX (`[ocagent] listen:`, `[ocagent] listen:
// connected`) and has tests pinning those literals; a leading stamp would
// silently break that classification, which is the one way this change could
// damage something outside itself. The suffix rides the FIRST line of the
// write, so a multi-line event block (a chat body, a reply card with options)
// gets exactly ONE stamp on its header line and its indented continuation
// lines stay untouched — preserving the existing invariant (see
// renderMessageBody) that only an event's first line begins at column 0.
// ---------------------------------------------------------------------------

// eventStamper decides WHICH clock the next transcript line reports. dispatch
// parks the frame's server timestamp here for the duration of one frame; every
// line printed while it is parked belongs to that frame. Outside a frame it is
// zero and the stamper falls back to the local clock, labelled as such.
//
// Single-goroutine by construction: the SSE read loop calls dispatch one frame
// at a time (onData), so park/clear can never interleave.
type eventStamper struct {
	clock   func() time.Time // injectable; real is time.Now
	frameTS float64          // server ts of the frame being dispatched; 0 ⇒ none
}

// enter parks a frame's server timestamp; the returned func clears it. A frame
// missing `ts` (older server, malformed) parks nothing and the line falls back
// to the local clock rather than to a wrong number.
func (s *eventStamper) enter(frame map[string]any) func() {
	if s == nil {
		return func() {}
	}
	ts, _ := frame["ts"].(float64)
	s.frameTS = ts
	return func() { s.frameTS = 0 }
}

// suffix renders the bracketed stamp for the line about to be written.
func (s *eventStamper) suffix() string {
	if s == nil {
		return ""
	}
	if s.frameTS != 0 {
		return fmt.Sprintf("[ts=%.3f]", s.frameTS)
	}
	now := time.Now
	if s.clock != nil {
		now = s.clock
	}
	return fmt.Sprintf("[ts=%.3f local]", float64(now().UnixNano())/1e9)
}

// stampWriter appends one stamp to the first line of every Write. It is the
// single choke point: the eleven-odd `fmt.Fprintf(out, "[ocagent] …")` call
// sites in listen.go / listen_hooks.go / listen_run.go are untouched, so a new
// print site cannot forget to stamp itself.
type stampWriter struct {
	inner io.Writer
	stamp func() string
}

func (w *stampWriter) Write(p []byte) (int, error) {
	suffix := ""
	if w.stamp != nil {
		suffix = w.stamp()
	}
	if suffix == "" {
		return w.inner.Write(p)
	}

	// Insert before the FIRST newline (the header line of an event block); with
	// no newline at all, append at the end.
	var buf bytes.Buffer
	if i := bytes.IndexByte(p, '\n'); i >= 0 {
		buf.Write(p[:i])
		buf.WriteByte(' ')
		buf.WriteString(suffix)
		buf.Write(p[i:])
	} else {
		buf.Write(p)
		buf.WriteByte(' ')
		buf.WriteString(suffix)
	}

	if _, err := w.inner.Write(buf.Bytes()); err != nil {
		return 0, err
	}
	// io.Writer contract: never report more than len(p).
	return len(p), nil
}
