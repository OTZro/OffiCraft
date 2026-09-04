package main

import (
	"os"
	"strings"
	"testing"
)

// T-81. The defect these three guard against was not "the wrong path" — it was a path
// resolved ONCE, while the warden was booting, on a machine where ocagent had not been
// downloaded yet. The warden then symlinked every member's workdir `ocagent` at that
// never-created path. os.Symlink is happy to point at nothing, the tmux window opens,
// claude starts, and the ONLY thing that fails is the bare `ocagent listen` inside the
// agent's own session — where no one is reading. From the outside that member is
// indistinguishable from one that died or ran out of tokens, and nothing anywhere is red.
//
// So there are two separate properties to hold down, and each has its own mutant:
//   - resolve PER SPAWN (revert to eager ⇒ TestStart_ResolvesOcAgentPerSpawn goes red)
//   - refuse VISIBLY when it is not there (drop the gate ⇒ TestStart_RefusesWhenOcAgentMissing
//     goes red)
// Neither test can cover for the other: an eager resolver that still gates would keep
// machines deaf until a restart, and a lazy resolver with no gate would go back to
// publishing a dangling link.

// TestStart_ResolvesOcAgentPerSpawn_NotOncePerProcess pins the timing, which is the whole
// ticket. The seam here answers a DIFFERENT path on its second call — standing in for the
// download landing between two spawns. If the resolution is ever hoisted back to warden
// boot, the second spawn links at the first answer and this goes red.
func TestStart_ResolvesOcAgentPerSpawn_NotOncePerProcess(t *testing.T) {
	const first = "/home/oc/.officraft/warden/ocagent-before-download"
	const second = "/home/oc/.officraft/warden/ocagent"

	calls := 0
	links := map[string]string{}
	run := &recRunner{err: map[string]error{
		"tmux -L officraft has-session -t member-alice": errAbsent(),
		"tmux -L officraft has-session -t member-bob":   errAbsent(),
	}}
	deps := newStartDepsLinks(t, run, map[string]string{}, links)
	deps.ResolveOcAgentBin = func() (string, bool) {
		calls++
		if calls == 1 {
			return first, true
		}
		return second, true
	}

	if out := deps.start(StartParams{MemberID: "alice", MemberToken: fxToken, SessionName: "member-alice"}); !out.OK {
		t.Fatalf("first spawn: outcome = %+v, want ok", out)
	}
	gotFirst := links["/home/oc/.officraft/agents/alice/ocagent"]
	if gotFirst != first {
		t.Fatalf("first spawn linked at %q, want %q", gotFirst, first)
	}

	if out := deps.start(StartParams{MemberID: "bob", MemberToken: fxToken, SessionName: "member-bob"}); !out.OK {
		t.Fatalf("second spawn: outcome = %+v, want ok", out)
	}
	gotSecond := links["/home/oc/.officraft/agents/bob/ocagent"]
	if gotSecond != second {
		t.Errorf("second spawn linked at %q, want %q — the resolver must be asked again "+
			"at spawn time, not read from a value frozen at warden boot", gotSecond, second)
	}
	if calls < 2 {
		t.Errorf("resolver called %d time(s) across two spawns, want one call per spawn", calls)
	}
}

// TestStart_RefusesWhenOcAgentMissing pins the OTHER half: when the resolver reports the
// binary is not there, the spawn must fail with a reason that travels back to the server,
// and must not publish a link or open a session. "Silently deaf" is exactly what the
// absence of this gate looked like.
func TestStart_RefusesWhenOcAgentMissing(t *testing.T) {
	const missing = "/home/oc/cli/ocagent/ocagent"
	links := map[string]string{}
	run := &recRunner{err: map[string]error{"tmux -L officraft has-session -t member-alice": errAbsent()}}
	deps := newStartDepsLinks(t, run, map[string]string{}, links)
	deps.ResolveOcAgentBin = func() (string, bool) { return missing, false }

	out := deps.start(StartParams{MemberID: "alice", MemberToken: fxToken, SessionName: "member-alice"})
	if out.OK {
		t.Fatalf("outcome = %+v, want a refusal — a spawn with no ocagent can never come online", out)
	}
	if !strings.Contains(out.Reason, "ocagent_not_found") {
		t.Errorf("Reason = %q, want it to name ocagent_not_found so the failure is findable", out.Reason)
	}
	if !strings.Contains(out.Reason, missing) {
		t.Errorf("Reason = %q, want the path it looked at (%q) — without it the reader cannot check", out.Reason, missing)
	}
	if len(links) != 0 {
		t.Errorf("published %v, want NO symlink at all — a dangling link is the defect", links)
	}
	if run.sawArgv("tmux", "-L", fxSocket, "new-session") {
		t.Errorf("must refuse BEFORE opening a session; a window that opens and never connects is what this ticket is about")
	}
}

// TestStart_ProceedsWhenOcAgentPresent is the negative control for the gate above: with
// the same construction and only the existence bit flipped, the spawn goes through and
// links at exactly the path the resolver named. Without this, a gate that refused
// unconditionally would still pass the refusal test.
func TestStart_ProceedsWhenOcAgentPresent(t *testing.T) {
	const present = "/home/oc/.officraft/warden/ocagent"
	links := map[string]string{}
	run := &recRunner{err: map[string]error{"tmux -L officraft has-session -t member-alice": errAbsent()}}
	deps := newStartDepsLinks(t, run, map[string]string{}, links)
	deps.ResolveOcAgentBin = func() (string, bool) { return present, true }

	out := deps.start(StartParams{MemberID: "alice", MemberToken: fxToken, SessionName: "member-alice"})
	if !out.OK {
		t.Fatalf("outcome = %+v, want ok", out)
	}
	if got := links["/home/oc/.officraft/agents/alice/ocagent"]; got != present {
		t.Errorf("ocagent symlink target = %q, want %q", got, present)
	}
}

// TestStart_NilResolverKeepsPreT81Construction: the seam is optional. Every start() test
// written before T-81 leaves it nil and must keep passing unchanged — the nil branch is
// the old repoRoot-relative path with no new gate. This is stated as a test rather than
// left implicit because the nil branch is the one a future edit is most likely to delete
// as dead code.
func TestStart_NilResolverKeepsPreT81Construction(t *testing.T) {
	links := map[string]string{}
	run := &recRunner{err: map[string]error{"tmux -L officraft has-session -t member-alice": errAbsent()}}
	deps := newStartDepsLinks(t, run, map[string]string{}, links)
	if deps.ResolveOcAgentBin != nil {
		t.Fatalf("newStartDepsLinks must leave the seam nil for this test to mean anything")
	}
	out := deps.start(StartParams{MemberID: "alice", MemberToken: fxToken, SessionName: "member-alice"})
	if !out.OK {
		t.Fatalf("outcome = %+v, want ok", out)
	}
	if got, want := links["/home/oc/.officraft/agents/alice/ocagent"], ocAgentSymlinkTarget(fxRepoRoot, ""); got != want {
		t.Errorf("nil resolver must keep the pre-T-81 target: got %q want %q", got, want)
	}
}

var _ = os.Stat
