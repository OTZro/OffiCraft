package main

// pacing.go — rate-limit window pacing (the Go twin of
// the retired Python domain/token_pacing.py). Turns the Claude-Code statusLine
// rate_limits payload (the 5-hour and 7-day windows, each carrying
// used_percentage + resets_at) into a pacing view: how much of the QUOTA is
// used vs how much of the TIME has elapsed. used% running ahead of elapsed%
// = burning too fast; at or behind = has headroom.
//
// Honest-null discipline throughout: a value that cannot be measured is nil
// (未量到), NEVER a fabricated 0 — the panel must never render a fake 0%.
// Pure and framework-free; the raw payload is untrusted free-form JSON, so
// every accessor tolerates any shape and never panics.

import (
	"math"
	"strings"
	"time"
)

// WindowSeconds fixes the window lengths, aligned to Claude's rate-limit
// windows.
var WindowSeconds = map[string]float64{
	"five_hour": 5 * 3600,
	"seven_day": 7 * 24 * 3600,
}

// PaceMarginPct: used% must exceed elapsed% by MORE than this to count as
// "burning hot" — a small band so normal jitter around the pace line doesn't
// flip the verdict.
const PaceMarginPct = 5.0

// The pace verdict vocabulary (nil pace = can't judge).
const (
	PaceHot = "hot"
	PaceOK  = "ok"
)

// PaceWindow is one shaped rate-limit window. Nil fields are honest nulls
// (unmeasured); ResetsAt echoes the raw value AS GIVEN (epoch number or ISO
// string; nil when absent).
//
// MeasuredAt is the AGE of UsedPct — the epoch second at which the agent last
// reported this snapshot. It exists because the two percentages in this struct
// are measured against DIFFERENT clocks: UsedPct is a frozen snapshot that
// stops moving the moment the last agent on that account goes away, while
// ElapsedPct is recomputed from `now` on every request. Without MeasuredAt a
// reader cannot tell a live 43% from one taken three days ago — they render
// byte-identically (T-3b90: the owner reported exactly that, and was right).
// Honest-null: nil means "nobody stamped when this was taken", NOT "just now".
type PaceWindow struct {
	UsedPct    *float64 `json:"used_pct"`
	ElapsedPct *float64 `json:"elapsed_pct"`
	Pace       *string  `json:"pace"`
	ResetsAt   any      `json:"resets_at"`
	MeasuredAt *float64 `json:"measured_at"`
}

// round2 mirrors Python round(x, 2) (banker's rounding).
func round2(x float64) float64 {
	return math.RoundToEven(x*100) / 100
}

// asFloat narrows an untrusted JSON value to a float64. JSON decoding yields
// float64 only; the integer cases cover literal Go call sites. A bool is not
// a number here (unlike Python, where bool ⊂ int needed an explicit guard).
func asFloat(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case float32:
		return float64(n), true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	}
	return 0, false
}

// parseResetsAt turns a raw resets_at into epoch seconds, or nil if absent /
// unparseable. Claude's statusLine actually sends a unix-epoch NUMBER; an
// ISO-8601 string is accepted defensively (a naive timestamp reads in local
// time, matching the Python fromisoformat fallback). Garbage or a
// non-positive number → nil (NEVER 0).
func parseResetsAt(value any) *float64 {
	if n, ok := asFloat(value); ok {
		if n > 0 {
			return &n
		}
		return nil
	}
	text, ok := value.(string)
	if !ok || text == "" {
		return nil
	}
	text = strings.TrimSpace(text)
	if t, err := time.Parse(time.RFC3339, text); err == nil {
		epoch := float64(t.UnixNano()) / 1e9
		return &epoch
	}
	if t, err := time.ParseInLocation("2006-01-02T15:04:05", text, time.Local); err == nil {
		epoch := float64(t.UnixNano()) / 1e9
		return &epoch
	}
	return nil
}

// usedPctOrNone shapes a raw used_percentage: non-number / the -1 "not
// measured" sentinel (any negative) → nil.
func usedPctOrNone(value any) *float64 {
	n, ok := asFloat(value)
	if !ok || n < 0 {
		return nil
	}
	rounded := round2(n)
	return &rounded
}

// elapsedPct back-computes how much of the window has elapsed from its END
// time (resets_at): start = resets_at - windowSec; elapsed% clamped to
// [0,100]. resets_at missing / unparseable → nil (NEVER 0).
func elapsedPct(resetsAt any, windowSec, now float64) *float64 {
	resetEpoch := parseResetsAt(resetsAt)
	if resetEpoch == nil || windowSec <= 0 {
		return nil
	}
	start := *resetEpoch - windowSec
	elapsed := (now - start) / windowSec * 100.0
	rounded := round2(math.Max(0.0, math.Min(100.0, elapsed)))
	return &rounded
}

// paceVerdict: "hot" when used% runs MORE than PaceMarginPct ahead of
// elapsed% (strict >), else "ok"; either input missing → nil (can't judge).
//
// STALENESS IS A THIRD WAY TO BE UNABLE TO JUDGE (T-3b90). The verdict compares
// a frozen used% against an elapsed% that keeps advancing with the wall clock,
// so once the snapshot stops being refreshed the comparison stops describing
// anything that is happening: the two operands drift apart on their own, and
// "hot" appears and disappears purely as a function of TIME PASSING. The owner
// hit the far end of that — an account with no agent running for days still
// showed a red "過熱" badge, which the clock alone would have cleared two days
// later. A judgement nobody's behaviour can change is not a warning; it is a
// clock read out in alarm colours. So: snapshot older than freshSecs (or of
// unknown age) → nil, "can't judge". The NUMBER is still served — it may well
// still be true, and withholding it would cost the owner this week's usage.
// Only the present-tense verdict is withheld.
func paceVerdict(usedPct, elapsedPct, measuredAt *float64, now, freshSecs float64) *string {
	if usedPct == nil || elapsedPct == nil {
		return nil
	}
	if measuredAt == nil || now-*measuredAt > freshSecs {
		return nil
	}
	verdict := PaceOK
	if *usedPct > *elapsedPct+PaceMarginPct {
		verdict = PaceHot
	}
	return &verdict
}

// ShapeWindow shapes one raw rate-limit window. A non-object raw → nil;
// individually unmeasurable fields stay nil but the window object is still
// returned (partial is allowed, so the panel can show what it has).
//
// measuredAt is when the caller last received this snapshot (nil = unknown);
// freshSecs is how long a snapshot may go unrefreshed and still support a
// present-tense pace verdict. Both flow straight through to paceVerdict.
func ShapeWindow(raw any, windowSec, now float64, measuredAt *float64, freshSecs float64) *PaceWindow {
	obj, ok := raw.(map[string]any)
	if !ok {
		return nil
	}
	resetsAt := obj["resets_at"]
	used := usedPctOrNone(obj["used_percentage"])
	elapsed := elapsedPct(resetsAt, windowSec, now)
	return &PaceWindow{
		UsedPct:    used,
		ElapsedPct: elapsed,
		Pace:       paceVerdict(used, elapsed, measuredAt, now, freshSecs),
		ResetsAt:   resetsAt,
		MeasuredAt: measuredAt,
	}
}

// ShapeWindows shapes the 5h + 7d windows from a raw rate_limits value.
// rate_limits missing / not an object → both windows nil (未量到). Never
// panics.
//
// measuredAt is PER WINDOW, not per account: the fold picks each window
// independently (later resets_at wins), so the 5h and 7d numbers on one card
// can come from different reports at different times. Collapsing them to one
// account-wide stamp would let a fresh 5h window vouch for a frozen 7d one —
// which is the exact confusion T-3b90 was filed about.
func ShapeWindows(rateLimits any, now float64, measuredAt map[string]float64, freshSecs float64) map[string]*PaceWindow {
	out := map[string]*PaceWindow{"five_hour": nil, "seven_day": nil}
	obj, ok := rateLimits.(map[string]any)
	if !ok {
		return out
	}
	for key, windowSec := range WindowSeconds {
		var stamp *float64
		if ts, ok := measuredAt[key]; ok && ts > 0 {
			stamp = &ts
		}
		out[key] = ShapeWindow(obj[key], windowSec, now, stamp, freshSecs)
	}
	return out
}
