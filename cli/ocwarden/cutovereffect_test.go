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
		shape:         shapeAnchor,
		leaderElapsed: 7200,
		carriers:      []carrier{{pid: 500, elapsed: 60}},
		anchorBirth:   anchorBorn,
		sampledAt:     anchorBorn.Add(2 * time.Hour),
		ok:            true,
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
			// B unreadable (every non-darwin platform, and any stat fault) arrives
			// as the zero time. Comparing against it would make EVERY carrier look
			// born-after-the-identity, so the guard must skip the negative
			// entirely rather than let a zero B answer the question.
			name: "an unreadable identity birth loses the negative, not the caution",
			p: probe(func(p *carrierProbe) {
				p.anchorBirth = time.Time{}
				p.leaderElapsed = 600
				p.carriers = []carrier{{pid: 500, elapsed: 9 * 24 * 3600}}
			}),
			want: effectUnproven,
		},
		{
			// The other direction of the same guard: a zero B must not block a
			// green that C1+C2+C3 have earned on their own.
			name: "an unreadable identity birth still allows a proven green",
			p:    probe(func(p *carrierProbe) { p.anchorBirth = time.Time{} }),
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
			carriers:    []carrier{{pid: 4242, elapsed: 9 * 24 * 3600}},
			anchorBirth: born,
			sampledAt:   at,
			ok:          true,
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
			shape:         shapeAnchor,
			leaderElapsed: int(at.Sub(born) / time.Second),
			carriers:      []carrier{{pid: 9001, elapsed: int(at.Sub(restartedAt) / time.Second)}},
			anchorBirth:   born,
			sampledAt:     at,
			ok:            true,
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
	f.runOut["ps -p 4242 -o etimes="] = s.leaderElapsed
	f.runOut["tmux -L "+testSocket+" list-sessions -F #{session_name}"] = s.sessions
	if s.sessionsErr != nil {
		f.runErr["tmux -L "+testSocket+" list-sessions -F #{session_name}"] = s.sessionsErr
	}
	f.runOut["tmux -L "+testSocket+" display-message -p #{pid}"] = s.tmuxPID
	f.runOut["ps -p 500 -o etimes="] = s.carrierAge
	if s.carrierAgeErr != nil {
		f.runErr["ps -p 500 -o etimes="] = s.carrierAgeErr
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
				leaderElapsed: "7200",
				sessions:      "member-m-one\nmember-m-two\n",
				tmuxPID:       "500",
				carrierAge:    "60",
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
				leaderElapsed: "7200",
				sessions:      "member-m-one\n",
				tmuxPID:       "500",
				carrierAge:    "777600", // nine days
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
				leaderElapsed: "7200",
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
				leaderElapsed: "7200",
				sessionsErr:   errors.New("connection refused: something unclassifiable"),
				birth:         &anchorBorn,
			},
			want: effectUnproven,
		},
		{
			name: "sessions exist but the carrier's age is unreadable",
			script: sampleScript{
				parentExe:     testAnchorPath,
				leaderElapsed: "7200",
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
				carrierAge:    "60",
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
				leaderElapsed: "7200",
				sessions:      "scratch\nbuild-box\n",
				tmuxPID:       "500",
				carrierAge:    "60",
				birth:         &anchorBorn,
			},
			want: effectUnproven,
		},
		{
			name: "a machine still booted from the old shape",
			script: sampleScript{
				parentExe:     "/sbin/launchd",
				leaderElapsed: "7200",
				sessions:      "member-m-one\n",
				tmuxPID:       "500",
				carrierAge:    "60",
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
				leaderElapsed: "7200",
				sessions:      "member-m-one\n",
				tmuxPID:       "500",
				carrierAge:    "60",
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
	f.runOut["ps -p 4242 -o etimes="] = "7200"
	f.runOut["tmux -L "+ns+" list-sessions -F #{session_name}"] = "member-m-one\n"
	f.runOut["tmux -L "+ns+" display-message -p #{pid}"] = "500"
	f.runOut["ps -p 500 -o etimes="] = "60"
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
