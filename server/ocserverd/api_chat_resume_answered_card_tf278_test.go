package main

import (
	"net/http"
	"strings"
	"testing"
)

// ── T-f278: the answer landed and nobody picked it up ────────────────────────
//
// The state this file exists for: the owner answers a reply card, the server
// releases the hold, and the bound step goes BACK to in_progress — the same
// value a step being actively worked carries. Nothing then tracks it. A card
// answered on Monday sat untouched until Wednesday while every board and every
// status field showed a perfectly normal ticket.
//
// The fix is a POINTER on the wake snapshot, not a state change: releaseCardHold
// keeps doing exactly what it does (an answer may well be 不通過／改做, so the
// server must never read one as completion), and the resuming agent is simply
// told which of its steps are sitting on an already-answered card.

const (
	// The fixture's answered step, written out so the size expectation below is
	// a literal and not a re-computation of the code under test.
	answeredStepName = "整合 uplink 通道" // 12 runes
	// step id ("ts-" + 12 hex) + card id ("rc-" + 12 hex) + the name above.
	wantAnsweredCardChars = 15 + 15 + 12
)

// TestResumeSnapshotNamesStepsSittingOnAnAnsweredCard pins the whole signal on
// one snapshot that carries all three shapes at once: a step whose card the
// owner ANSWERED (must be named), a step still WAITING on its card (must not
// be — that one is the owner's turn, not yours), and a step being worked with
// no card at all (must not be). Overview counts and sizes them, and the peek —
// which carries no rows — still reports both, because an agent that has not
// pulled the snapshot yet is exactly the reader this signal was built for.
func TestResumeSnapshotNamesStepsSittingOnAnAnsweredCard(t *testing.T) {
	api := resumeCtxServer(t)

	answeredTask := createAdHocTask(t, api, "m-exec")
	answeredPlan := submitPlan(t, api, answeredTask.ID, "m-exec", []map[string]any{
		{"name": answeredStepName, "dod": "通道跑得起來"},
		{"name": "收尾", "dod": "文件補完"},
	})
	startFirstStep(t, api, answeredTask.ID, "m-exec")
	card := openGateCard(t, api, answeredTask.ID, "m-exec",
		answeredPlan.Steps[0].ID, "要走哪一條路？")
	if rec := answerCard(t, api, card.ID,
		map[string]any{"option_idx": 1}); rec.Code != http.StatusOK {
		t.Fatalf("answer: %d %s", rec.Code, rec.Body.String())
	}

	waitingTask := createAdHocTask(t, api, "m-exec")
	waitingPlan := submitPlan(t, api, waitingTask.ID, "m-exec", []map[string]any{
		{"name": "等 owner 回覆", "dod": "他回了"},
	})
	startFirstStep(t, api, waitingTask.ID, "m-exec")
	openGateCard(t, api, waitingTask.ID, "m-exec", waitingPlan.Steps[0].ID, "還沒回的問題")

	workingTask := createAdHocTask(t, api, "m-exec")
	submitPlan(t, api, workingTask.ID, "m-exec", []map[string]any{
		{"name": "純粹在做", "dod": "做完"},
	})
	startFirstStep(t, api, workingTask.ID, "m-exec")

	// ANTI-VACUITY: the answered step must really be back at in_progress with an
	// answered card bound. That indistinguishability IS the bug — if the fixture
	// stopped reproducing it, every assertion below would be about nothing.
	answeredView := getTaskView(t, api, answeredTask.ID)
	if answeredView.Steps[0].Status != StepStatusInProgress {
		t.Fatalf("the answered step must be back at in_progress (that is the whole "+
			"problem — it looks like work in flight): %+v", answeredView.Steps[0])
	}
	if answeredView.Steps[0].ReplyCardStatus != replyCardStatusAnswered {
		t.Fatalf("the bound card must read answered: %+v", answeredView.Steps[0])
	}

	snap := resumeSnapshot(t, api, "m-exec")
	rows := map[string]resumeTaskDTO{}
	for _, r := range snap.Tasks {
		rows[r.ID] = r
	}
	if len(rows) != 3 {
		t.Fatalf("expected the three seeded tasks on the snapshot, got %d", len(rows))
	}

	named := rows[answeredTask.ID].AnsweredCardSteps
	if len(named) != 1 {
		t.Fatalf("the answered-card step must be named exactly once: %+v", named)
	}
	if named[0].StepID != answeredPlan.Steps[0].ID {
		t.Fatalf("step_id: want %q, got %q", answeredPlan.Steps[0].ID, named[0].StepID)
	}
	if named[0].StepName != answeredStepName {
		t.Fatalf("step_name: want %q, got %q", answeredStepName, named[0].StepName)
	}
	if named[0].CardID != card.ID {
		t.Fatalf("card_id must point at the answered card: want %q, got %q",
			card.ID, named[0].CardID)
	}

	// A card the owner has NOT answered yet is HIS turn — surfacing it here
	// would put the executor's own waiting back on the executor's plate.
	if got := rows[waitingTask.ID].AnsweredCardSteps; len(got) != 0 {
		t.Fatalf("a still-waiting card must not be named: %+v", got)
	}
	if got := rows[workingTask.ID].AnsweredCardSteps; len(got) != 0 {
		t.Fatalf("a plain in_progress step with no card must not be named: %+v", got)
	}

	if snap.Overview.StepsOnAnsweredCard != 1 {
		t.Fatalf("steps_on_answered_card: want 1, got %d",
			snap.Overview.StepsOnAnsweredCard)
	}
	if snap.Overview.StepsOnAnsweredCardChars != wantAnsweredCardChars {
		t.Fatalf("steps_on_answered_card_chars: want %d, got %d",
			wantAnsweredCardChars, snap.Overview.StepsOnAnsweredCardChars)
	}

	// The peek carries no rows, so the overview counts are the ONLY way it can
	// say this — and its headline number must include what the rows will cost.
	peek := peekResumeSize(t, api, "m-exec")
	if peek.Overview != snap.Overview {
		t.Fatalf("peek overview must equal the snapshot's:\n peek=%+v\n full=%+v",
			peek.Overview, snap.Overview)
	}
	otherBlocks := peek.Overview.ChatChars + peek.Overview.TasksDetailChars +
		peek.Overview.RosterChars + peek.Overview.MachinesChars
	if got := peek.EstimatedTotalChars - otherBlocks; got != wantAnsweredCardChars {
		t.Fatalf("estimated_total_chars must carry the answered-card pointers: "+
			"want the other blocks + %d, got them + %d",
			wantAnsweredCardChars, got)
	}
}

// TestResumeSnapshotSaysNothingWhenNoAnswerIsWaiting is the OFF case: the same
// server, one task being worked normally and one card the owner still owes an
// answer on. Every part of the signal must read empty — a pointer that fires on
// the ordinary shape of a working day is noise, and noise is ignored.
func TestResumeSnapshotSaysNothingWhenNoAnswerIsWaiting(t *testing.T) {
	api := resumeCtxServer(t)

	task := createAdHocTask(t, api, "m-exec")
	plan := submitPlan(t, api, task.ID, "m-exec", []map[string]any{
		{"name": "動手做", "dod": "做完"},
		{"name": "問一下", "dod": "問到了"},
	})
	startFirstStep(t, api, task.ID, "m-exec")
	openGateCard(t, api, task.ID, "m-exec", plan.Steps[1].ID, "順便問的問題")

	snap := resumeSnapshot(t, api, "m-exec")
	if len(snap.Tasks) != 1 {
		t.Fatalf("expected the one seeded task, got %d", len(snap.Tasks))
	}
	if got := snap.Tasks[0].AnsweredCardSteps; len(got) != 0 {
		t.Fatalf("nothing is answered — the row must name no step: %+v", got)
	}
	if snap.Overview.StepsOnAnsweredCard != 0 ||
		snap.Overview.StepsOnAnsweredCardChars != 0 {
		t.Fatalf("overview must stay silent: %+v", snap.Overview)
	}
	peek := peekResumeSize(t, api, "m-exec")
	otherBlocks := peek.Overview.ChatChars + peek.Overview.TasksDetailChars +
		peek.Overview.RosterChars + peek.Overview.MachinesChars
	if peek.EstimatedTotalChars != otherBlocks {
		t.Fatalf("with nothing to point at, the estimate must not grow: "+
			"want %d, got %d", otherBlocks, peek.EstimatedTotalChars)
	}
}

// TestResumeProseNamesTheAnsweredCardSignal: a field nobody was told about is a
// field nobody reads. The wake snapshot's own note is where an agent learns what
// its task rows mean, and the peek's note is where it learns what the size
// number is made of — both must name this signal, or it ships invisible.
func TestResumeProseNamesTheAnsweredCardSignal(t *testing.T) {
	for _, tc := range []struct {
		name, text, field string
	}{
		{"resumeNote", resumeNote, "answered_card_steps"},
		{"resumeNote/overview", resumeNote, "steps_on_answered_card"},
		{"peekNote", peekNote, "steps_on_answered_card_chars"},
		{"peekNote/count", peekNote, "steps_on_answered_card"},
	} {
		if !strings.Contains(tc.text, tc.field) {
			t.Errorf("%s must name %q — an unexplained field is an unread field",
				tc.name, tc.field)
		}
	}
}
