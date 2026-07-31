// cutovereffect_test.go — the truth table judgeCutoverEffect is, plus the
// sampling that feeds it, plus the incident this whole file answers to.
//
// WHY THESE ASSERTIONS AND NOT "IT RETURNS SOMETHING"
// ---------------------------------------------------
// The defect being retired is a two-valued light that read green while the
// cutover had not taken effect. The only test that protects against its return
// is one that FAILS when a verdict drifts toward green — so every case below
// names the verdict it must NOT be as much as the one it must, and the mutant
// this file is written against is `judgeCutoverEffect` hardcoded to
// effectEffective, which must redden most of the table.
//
// Nothing here touches a real ps/tmux/stat: TestMain (hostseam_test.go) rebinds
// newCutoverOps to blockedCutoverOps for the whole package, and the judge itself
// takes a plain struct.
package main

import (
	"errors"
	"strings"
	"testing"
	"time"
)

// anchorBorn is the instant a machine's anchor identity came into existence in
// these tests. Every carrier age below is expressed relative to it, so a reader
// can see at a glance which side of the identity a carrier was forked on.
var anchorBorn = time.Date(2026, 7, 31, 11, 58, 0, 0, time.UTC)

// probe builds a carrierProbe that is VALID in every respect the case under
// test is not about, so a row only has to state its own deviation. Defaults are
// the green ones on purpose: a mutation that widens a condition then shows up as
// a row that stopped being red, not as a row nobody wrote.
func probe(mutate func(*carrierProbe)) carrierProbe {
	p := carrierProbe{
		shape:            shapeAnchor,
		leaderElapsed:    7200,
		carriers:         []carrier{{pid: 500, elapsed: 60}},
		anchorBirth:      anchorBorn,
		anchorBirthKnown: true,
		sampledAt:        anchorBorn.Add(2 * time.Hour),
		ok:               true,
	}
	if mutate != nil {
		mutate(&p)
	}
	return p
}

func TestJudgeCutoverEffect(t *testing.T) {
	for _, tc := range []struct {
		name string
		p    carrierProbe
		want cutoverEffect
	}{
		{
			// The only green in this file, and it needs all three conditions at
			// once. If a mutation drops any one of them the OTHER rows go red
			// while this one stays green — which is the pair of directions a
			// single-row test cannot give.
			name: "anchor, one carrier younger than the leader and born after the identity",
			p:    probe(nil),
			want: effectEffective,
		},
		{
			// Fail-closed at the front door. `ok` is false whenever ANY operand
			// was unreadable, and a half-read sample must not be judged on the
			// half that was readable — the readable half here is the green one.
			name: "an incomplete sample is never judged",
			p:    probe(func(p *carrierProbe) { p.ok = false }),
			want: effectUnproven,
		},
		{
			// C1. launchd not running the anchor means there is no anchor
			// identity to have inherited, so nothing downstream can prove
			// anything — including the negative, covered separately below.
			name: "still on the old shape",
			p:    probe(func(p *carrierProbe) { p.shape = shapeLegacy }),
			want: effectUnproven,
		},
		{
			name: "the machine could not read its own parent",
			p:    probe(func(p *carrierProbe) { p.shape = shapeUnknown }),
			want: effectUnproven,
		},
		{
			// C3. An empty carrier set makes C2 vacuously true, and a vacuous
			// truth is exactly how "we measured nothing" becomes "all good".
			name: "no carrier to measure is not a pass",
			p:    probe(func(p *carrierProbe) { p.carriers = nil }),
			want: effectUnproven,
		},
		{
			// E(L) unreadable arrives as 0 (processElapsedSecs never guesses), and
			// 0 would make every carrier look older than the leader — the safe
			// direction, but it must be stated rather than fallen into.
			name: "the leader's age is unavailable",
			p:    probe(func(p *carrierProbe) { p.leaderElapsed = 0 }),
			want: effectUnproven,
		},
		{
			name: "a negative leader age is not a leader age",
			p:    probe(func(p *carrierProbe) { p.leaderElapsed = -1 }),
			want: effectUnproven,
		},
		{
			// C2, strictly. Equal ages cannot order the two events, so the
			// boundary belongs on the unproven side; a `>` here instead of `>=`
			// would green-light a carrier that may predate the leader.
			name: "a carrier exactly as old as the leader",
			p: probe(func(p *carrierProbe) {
				p.carriers = []carrier{{pid: 500, elapsed: 7200}}
			}),
			want: effectUnproven,
		},
		{
			// Older than the leader but still younger than the anchor FILE: a
			// legitimate carrier after a `launchctl kickstart -k`. Amber, not red
			// — calling this not_effective would be a false accusation.
			name: "a carrier older than the leader but younger than the identity",
			p: probe(func(p *carrierProbe) {
				p.leaderElapsed = 600
				p.carriers = []carrier{{pid: 500, elapsed: 3600}}
			}),
			want: effectUnproven,
		},
		{
			// forall, not exists: one bad carrier is enough. A mutation to
			// "any carrier is younger" leaves the single-carrier rows green and
			// only this one red.
			name: "one of several carriers is too old",
			p: probe(func(p *carrierProbe) {
				p.leaderElapsed = 3600
				p.carriers = []carrier{{pid: 500, elapsed: 60}, {pid: 501, elapsed: 3600}}
			}),
			want: effectUnproven,
		},
		{
			// THE deterministic negative, and the shape of the real incident: a
			// carrier that already existed before the identity did cannot be
			// holding it, no matter how many times the leader has restarted.
			name: "a carrier predating the anchor identity is proof it did not take",
			p: probe(func(p *carrierProbe) {
				p.carriers = []carrier{{pid: 500, elapsed: 9 * 24 * 3600}}
			}),
			want: effectNotEffective,
		},
		{
			// The negative must not be masked by C2/C3 also failing — it is
			// evaluated first for exactly this case, where the leader's age is
			// unreadable at the same time.
			name: "the negative survives an unreadable leader age",
			p: probe(func(p *carrierProbe) {
				p.leaderElapsed = 0
				p.carriers = []carrier{{pid: 500, elapsed: 9 * 24 * 3600}}
			}),
			want: effectNotEffective,
		},
		{
			// The negative rests on C1: without the anchor running there is no
			// identity whose birth the carrier could predate, so the same old
			// carrier is only unproven.
			name: "an old carrier on a machine still running the old shape",
			p: probe(func(p *carrierProbe) {
				p.shape = shapeLegacy
				p.carriers = []carrier{{pid: 500, elapsed: 9 * 24 * 3600}}
			}),
			want: effectUnproven,
		},
		{
			// B unreadable — every non-darwin build (birthtime_other.go), which is
			// also CI's platform, plus any stat fault. The carrier here IS older
			// than the timestamp sitting in the field, so the ONLY thing standing
			// between this row and a red accusation is the judge refusing to use
			// an unread operand. Deleting that check turns this row
			// not_effective: an unknown B is no evidence, and convicting a machine
			// on no evidence is the mirror image of the false green.
			name: "an identity birth that was never read cannot convict",
			p: probe(func(p *carrierProbe) {
				p.anchorBirthKnown = false
				p.leaderElapsed = 600
				p.carriers = []carrier{{pid: 500, elapsed: 9 * 24 * 3600}}
			}),
			want: effectUnproven,
		},
		{
			// The other direction of the same guard: losing B must cost the
			// NEGATIVE only. It must not block a green that C1+C2+C3 have earned
			// on their own — otherwise no machine could ever read as effective on
			// a platform without a birthtime.
			name: "an unread identity birth still allows a proven green",
			p:    probe(func(p *carrierProbe) { p.anchorBirthKnown = false }),
			want: effectEffective,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := judgeCutoverEffect(tc.p); got != tc.want {
				t.Fatalf("judgeCutoverEffect = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestJudgeCutoverEffect_EvaM5Incident replays the 2026-07-31 incident on
// eva-m5 through the judge, in both of its states. This is the grid the field
// was built for, so it is pinned separately from the truth table above: the
// table proves the conditions are wired, this proves they answer the question
// that was actually asked wrong.
//
// The numbers are the incident's own: the machine completed the cutover at
// 11:58 (when the anchor file came into existence), the monitor read green from
// then on, and every agent was hanging off a tmux server started nine days
// earlier. A hand kill-server at 14:52 is what made it take effect.
func TestJudgeCutoverEffect_EvaM5Incident(t *testing.T) {
	const cutoverAt = "2026-07-31T11:58:00Z"
	born, err := time.Parse(time.RFC3339, cutoverAt)
	if err != nil {
		t.Fatalf("parse %s: %v", cutoverAt, err)
	}

	t.Run("during the incident the verdict is not_effective", func(t *testing.T) {
		at := born.Add(2 * time.Hour) // 13:58, mid-incident, badge still green
		got := judgeCutoverEffect(carrierProbe{
			shape: shapeAnchor,
			// The leader really was young and really was the anchor — that is
			// why the old signal read green.
			leaderElapsed: int(at.Sub(born) / time.Second),
			// The tmux server carrying every agent, started nine days earlier.
			carriers:         []carrier{{pid: 4242, elapsed: 9 * 24 * 3600}},
			anchorBirth:      born,
			anchorBirthKnown: true,
			sampledAt:        at,
			ok:               true,
		})
		if got != effectNotEffective {
			t.Fatalf("incident verdict = %q, want %q — this is the exact machine "+
				"state that showed green for three hours", got, effectNotEffective)
		}
	})

	t.Run("after the carriers restart the verdict is effective", func(t *testing.T) {
		restartedAt := born.Add(2*time.Hour + 54*time.Minute) // 14:52
		at := restartedAt.Add(8 * time.Minute)
		got := judgeCutoverEffect(carrierProbe{
			shape:            shapeAnchor,
			leaderElapsed:    int(at.Sub(born) / time.Second),
			carriers:         []carrier{{pid: 9001, elapsed: int(at.Sub(restartedAt) / time.Second)}},
			anchorBirth:      born,
			anchorBirthKnown: true,
			sampledAt:        at,
			ok:               true,
		})
		if got != effectEffective {
			t.Fatalf("post-restart verdict = %q, want %q — the repair that "+
				"actually worked must be visible as having worked", got, effectEffective)
		}
	})
}

// ── the sampling half ────────────────────────────────────────────────────────
//
// sampleCutoverEffect is where a real ps/tmux would be reached if the seam ever
// broke. Every case below drives it through the fake, and the argv the fake
// records is asserted on in the green case so "it never asked" cannot pass as
// "it asked and got a good answer".

const (
	testAnchorPath = "/home/u/.officraft/warden/officraft"
	testSocket     = "officraft"
)

// psCallKey is the fake's key for the age probe, built from the SAME argv
// definition production uses. A test that hand-wrote this string would drift
// from the code the moment the flags changed — and answering a stale argv
// politely is precisely how the `etimes` defect went green everywhere.
func psCallKey(pid int) string {
	return "ps " + strings.Join(psElapsedArgs(pid), " ")
}

// scriptSample wires a fakeCutover for the cutover-effect probe: the parent exe
// (detectShape's operand), the leader's and carrier's ages, the tmux session
// list and the anchor's birth.
type sampleScript struct {
	parentExe     string
	leaderElapsed string
	sessions      string
	sessionsErr   error
	tmuxPID       string
	carrierAge    string
	carrierAgeErr error
	birth         *time.Time
}

func (s sampleScript) fake() *fakeCutover {
	f := newFakeCutover()
	f.files["__ppid_exe__"] = s.parentExe
	f.runOut[psCallKey(4242)] = s.leaderElapsed
	f.runOut["tmux -L "+testSocket+" list-sessions -F #{session_name}"] = s.sessions
	if s.sessionsErr != nil {
		f.runErr["tmux -L "+testSocket+" list-sessions -F #{session_name}"] = s.sessionsErr
	}
	f.runOut["tmux -L "+testSocket+" display-message -p #{pid}"] = s.tmuxPID
	f.runOut[psCallKey(500)] = s.carrierAge
	if s.carrierAgeErr != nil {
		f.runErr[psCallKey(500)] = s.carrierAgeErr
	}
	if s.birth != nil {
		f.birthTimes[testAnchorPath] = *s.birth
	}
	return f
}

func TestSampleCutoverEffect(t *testing.T) {
	young := anchorBorn.Add(90 * time.Minute)
	for _, tc := range []struct {
		name   string
		script sampleScript
		want   cutoverEffect
	}{
		{
			name: "a fully readable anchor machine with a fresh carrier",
			script: sampleScript{
				parentExe:     testAnchorPath,
				leaderElapsed: "02:00:00",
				sessions:      "member-m-one\nmember-m-two\n",
				tmuxPID:       "500",
				carrierAge:    "01:00",
				birth:         &anchorBorn,
			},
			want: effectEffective,
		},
		{
			// The incident, end to end through the sampler rather than hand-fed
			// to the judge: the carrier is older than the anchor file.
			name: "a carrier older than the anchor file",
			script: sampleScript{
				parentExe:     testAnchorPath,
				leaderElapsed: "02:00:00",
				sessions:      "member-m-one\n",
				tmuxPID:       "500",
				carrierAge:    "09-00:00:00", // nine days
				birth:         &anchorBorn,
			},
			want: effectNotEffective,
		},
		{
			// A clean "no server on this socket" is a POSITIVE zero, not an
			// unreadable one — and zero carriers is still unproven (C3), never a
			// vacuous green.
			name: "no tmux server on the socket",
			script: sampleScript{
				parentExe:     testAnchorPath,
				leaderElapsed: "02:00:00",
				sessionsErr:   errors.New("no server running on /tmp/tmux-501/officraft"),
				birth:         &anchorBorn,
			},
			want: effectUnproven,
		},
		{
			// An enumeration that BROKE is a different claim from "zero
			// sessions", and folding the two would let a broken tmux read as an
			// idle machine.
			name: "an unreadable session enumeration",
			script: sampleScript{
				parentExe:     testAnchorPath,
				leaderElapsed: "02:00:00",
				sessionsErr:   errors.New("connection refused: something unclassifiable"),
				birth:         &anchorBorn,
			},
			want: effectUnproven,
		},
		{
			name: "sessions exist but the carrier's age is unreadable",
			script: sampleScript{
				parentExe:     testAnchorPath,
				leaderElapsed: "02:00:00",
				sessions:      "member-m-one\n",
				tmuxPID:       "500",
				carrierAgeErr: errors.New("ps: no such process"),
				birth:         &anchorBorn,
			},
			want: effectUnproven,
		},
		{
			name: "the leader's own age is unreadable",
			script: sampleScript{
				parentExe:     testAnchorPath,
				leaderElapsed: "not-a-number",
				sessions:      "member-m-one\n",
				tmuxPID:       "500",
				carrierAge:    "01:00",
				birth:         &anchorBorn,
			},
			want: effectUnproven,
		},
		{
			// Sessions that are not member-* are not carriers of agents. A
			// non-empty list must not be counted wholesale.
			name: "only non-member sessions on the socket",
			script: sampleScript{
				parentExe:     testAnchorPath,
				leaderElapsed: "02:00:00",
				sessions:      "scratch\nbuild-box\n",
				tmuxPID:       "500",
				carrierAge:    "01:00",
				birth:         &anchorBorn,
			},
			want: effectUnproven,
		},
		{
			name: "a machine still booted from the old shape",
			script: sampleScript{
				parentExe:     "/sbin/launchd",
				leaderElapsed: "02:00:00",
				sessions:      "member-m-one\n",
				tmuxPID:       "500",
				carrierAge:    "01:00",
				birth:         &anchorBorn,
			},
			want: effectUnproven,
		},
		{
			// The anchor's birthtime is unavailable on every non-darwin build
			// (and CI is one). Losing it must cost the negative only.
			name: "no readable anchor birthtime still reaches a proven green",
			script: sampleScript{
				parentExe:     testAnchorPath,
				leaderElapsed: "02:00:00",
				sessions:      "member-m-one\n",
				tmuxPID:       "500",
				carrierAge:    "01:00",
			},
			want: effectEffective,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := tc.script.fake()
			got := sampleCutoverEffect(f.ops(), testAnchorPath, testSocket, 4242, young)
			if got != tc.want {
				t.Fatalf("sampleCutoverEffect = %q, want %q (calls: %v)", got, tc.want, f.calls)
			}
		})
	}
}

// TestSampleCutoverEffect_UnresolvedInstallPathsAreUnproven pins the two inputs
// that mean "this warden could not work out where it lives". An empty anchor
// path would make detectShape compare a parent exe against "" and answer
// `legacy` for a correctly converted machine; an empty socket would enumerate
// the WRONG tmux server. Both must stop before they measure anything.
func TestSampleCutoverEffect_UnresolvedInstallPathsAreUnproven(t *testing.T) {
	for _, tc := range []struct{ name, anchor, socket string }{
		{"no anchor path", "", testSocket},
		{"no socket", testAnchorPath, ""},
		{"neither", "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFakeCutover()
			f.files["__ppid_exe__"] = testAnchorPath
			got := sampleCutoverEffect(f.ops(), tc.anchor, tc.socket, 4242, anchorBorn)
			if got != effectUnproven {
				t.Fatalf("sampleCutoverEffect = %q, want %q", got, effectUnproven)
			}
			if len(f.calls) != 0 {
				t.Fatalf("nothing may be probed once the paths are unknown, got %v", f.calls)
			}
		})
	}
}

// TestSampleCutoverEffect_ProbesTheInstanceOwnSocket guards the operand a
// namespaced instance gets wrong most easily: the socket is passed in, and every
// tmux call must carry it. A sampler that reached the canonical socket would
// judge the OTHER instance's carriers and report a confident verdict about a
// machine state it never looked at.
func TestSampleCutoverEffect_ProbesTheInstanceOwnSocket(t *testing.T) {
	const ns = "officraft-lab"
	f := newFakeCutover()
	f.files["__ppid_exe__"] = testAnchorPath
	f.runOut[psCallKey(4242)] = "02:00:00"
	f.runOut["tmux -L "+ns+" list-sessions -F #{session_name}"] = "member-m-one\n"
	f.runOut["tmux -L "+ns+" display-message -p #{pid}"] = "500"
	f.runOut[psCallKey(500)] = "01:00"
	f.birthTimes[testAnchorPath] = anchorBorn

	if got := sampleCutoverEffect(f.ops(), testAnchorPath, ns, 4242, anchorBorn.Add(time.Hour)); got != effectEffective {
		t.Fatalf("sampleCutoverEffect = %q, want %q (calls: %v)", got, effectEffective, f.calls)
	}
	for _, call := range f.calls {
		if strings.HasPrefix(call, "tmux ") && !strings.Contains(call, "-L "+ns+" ") {
			t.Fatalf("tmux call %q did not address this instance's socket %q", call, ns)
		}
	}
}

// TestAtoiStrict covers the parser every age passes through. It exists because
// the failure it prevents is silent: a lenient parse of "12 junk" or "-1" hands
// the judge a fabricated age, and a fabricated age is precisely how a false
// green gets manufactured.
func TestAtoiStrict(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want int
	}{
		{"7200", 7200},
		{"0", 0},
		{"", 0},
		{" 7200", 0},
		{"7200 ", 0},
		{"-1", 0},
		{"+1", 0},
		{"12junk", 0},
		{"1.5", 0},
	} {
		if got := atoiStrict(tc.in); got != tc.want {
			t.Errorf("atoiStrict(%q) = %d, want %d", tc.in, got, tc.want)
		}
	}
}

// TestPsProbeArgvIsPinnedToALiteral is the tripwire the `etimes` defect needed
// and did not have.
//
// 🔴 WHY A HAND-WRITTEN LITERAL. Everything else in this file reaches `ps`
// through psElapsedArgs, including the fake — which is correct (it stops the
// two from describing different commands) and is ALSO how the original defect
// survived: production asked for a flag that does not exist on macOS, the fake
// answered it politely, and the whole suite went green while the probe was dead
// on every real machine. A shared definition cannot catch a shared mistake. So
// the argv is written out ONCE, by hand, here — changing the flags without
// coming back to this line reddens.
//
// This test proves the argv is what we think it is. It cannot prove the host
// supports it; that is bin/tests/ps-field-support-guard.sh, which runs the real
// `ps`. Neither half is sufficient: the literal alone is just a second guess,
// and the host guard alone would not notice the code drifting away from the
// flag it verified. See also the note there on why the host half CANNOT live in
// this package — TestMain refuses real exec inside the test binary, by design.
func TestPsProbeArgvIsPinnedToALiteral(t *testing.T) {
	got := "ps " + strings.Join(psElapsedArgs(4242), " ")
	const want = "ps -p 4242 -o etime="
	if got != want {
		t.Fatalf("age probe argv = %q, want %q.\n"+
			"If you changed this on purpose: `etimes` (seconds) is GNU/procps only "+
			"and does NOT exist on macOS, the only platform this warden runs on — "+
			"asking for it makes every machine unreadable forever. Update "+
			"bin/tests/ps-field-support-guard.sh's expectations too, and run it.", got, want)
	}

	// And the argv really is the one that goes out on the wire, not just a
	// helper nobody calls: drive the production reader through a recording fake
	// and assert the recorded call.
	f := newFakeCutover()
	f.runOut[want] = "01:00"
	if _, ok := processElapsedSecs(f.ops().run, 4242); !ok {
		t.Fatalf("processElapsedSecs could not read a scripted %q; calls: %v", want, f.calls)
	}
	if len(f.calls) != 1 || f.calls[0] != want {
		t.Fatalf("processElapsedSecs issued %v, want exactly [%q]", f.calls, want)
	}
}

// TestParseEtime is the truth table for ps's elapsed-time format,
// [[dd-]hh:]mm:ss. The three real shapes come from actual readings on a fleet
// machine; the rejections are the point of the function.
//
// Every rejection below would, if accepted, hand the judge a NUMBER — and a
// number is indistinguishable downstream from a real measurement. "00:99" as 99
// seconds or "1:2:3" as 3723 is not a lenient parse, it is a fabricated age,
// and a fabricated age is how a false verdict gets manufactured.
func TestParseEtime(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want int
		ok   bool
	}{
		// The three shapes ps actually prints (real readings from eva-m5).
		{"05:12", 5*60 + 12, true},
		{"01:48:50", 1*3600 + 48*60 + 50, true},
		{"52-03:47:57", 52*86400 + 3*3600 + 47*60 + 57, true},
		{"00:00", 0, true},
		{"00:01", 1, true},
		{"23:59:59", 23*3600 + 59*60 + 59, true},
		{"1-00:00:00", 86400, true},
		// Rejections.
		{"", 0, false},
		{"7200", 0, false},        // the seconds form this parser must NOT accept
		{"1:2:3", 0, false},       // not zero-padded → not ps output
		{"00:99", 0, false},       // out of range
		{"00:60:00", 0, false},    // out of range
		{"24:00:00", 0, false},    // ps rolls past 24h into days
		{"1-05:12", 0, false},     // a day count without a full h:m:s tail
		{"-01:00:00", 0, false},   // empty day field
		{"a-01:00:00", 0, false},  // non-numeric day field
		{"01:00:00:00", 0, false}, // four fields
		{"01:0a", 0, false},
		{" 05:12", 0, false},
		{"05:12 ", 0, false},
		{"+5:12", 0, false},
	} {
		got, ok := parseEtime(tc.in)
		if ok != tc.ok || got != tc.want {
			t.Errorf("parseEtime(%q) = (%d,%v), want (%d,%v)", tc.in, got, ok, tc.want, tc.ok)
		}
	}
}

// TestTmuxMemberSessionCount pins the distinction the enumeration exists to
// make: "there are zero member sessions here" and "I could not find out" are
// different claims, and only the first one is allowed to influence a verdict.
//
// Asserted on the FUNCTION rather than through sampleCutoverEffect, because
// through the sampler both paths land on `unproven` — so an end-to-end test
// cannot tell them apart, and folding the broken case into the clean zero
// leaves the whole package green (it did). The difference only becomes visible
// downstream later, the day a caller starts trusting a zero.
func TestTmuxMemberSessionCount(t *testing.T) {
	const socket = "officraft"
	key := "tmux -L " + socket + " list-sessions -F #{session_name}"

	t.Run("counts only member sessions", func(t *testing.T) {
		f := newFakeCutover()
		f.runOut[key] = "member-m-one\nscratch\nmember-m-two\nbuild-box\n"
		n, listed := tmuxMemberSessionCount(f.ops().run, socket)
		if n != 2 || !listed {
			t.Fatalf("= (%d,%v), want (2,true)", n, listed)
		}
	})

	t.Run("a clean absence is a POSITIVE zero", func(t *testing.T) {
		f := newFakeCutover()
		f.runErr[key] = errors.New("no server running on /tmp/tmux-501/officraft")
		n, listed := tmuxMemberSessionCount(f.ops().run, socket)
		if n != 0 || !listed {
			t.Fatalf("= (%d,%v), want (0,true) — a tmux that positively reports no "+
				"server HAS answered the question", n, listed)
		}
	})

	t.Run("an unclassifiable failure is NOT zero sessions", func(t *testing.T) {
		f := newFakeCutover()
		f.runErr[key] = errors.New("something nobody has seen before")
		n, listed := tmuxMemberSessionCount(f.ops().run, socket)
		if listed {
			t.Fatalf("= (%d,%v), want listed=false — a broken probe must not be "+
				"reported as an idle machine", n, listed)
		}
	})
}
