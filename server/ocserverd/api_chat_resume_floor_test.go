package main

// api_chat_resume_floor_test.go — T-1b09: the studio floor a waking agent lands
// on (roster + machines) inside the wake snapshot.
//
// Owner rulings under test (verbatim, 2026-08-03):
//   - rc-4e98c0481852 "All members and contractors and their online / offline
//     status" — EVERY member and EVERY contractor, offline ones included.
//   - rc-09476f535b59 ① machine list + which one you are on, and deliberately
//     NOT a per-machine grouping of who is where.
//   - 「1000字 多的截斷」 — duty carried as written, capped, `…` marks the cut.
//   - 「之後應該給 duty 就好，不要給 insight / learning」 (2026-08-02).
//
// Values in the fixtures below are chosen so that NO expected value is a
// substring of another (ids m-alpha/m-bravo/ow-charlie, machines m-host-one /
// m-host-two): a substring-tolerant assertion is close to a tautology, and this
// file asserts exact equality everywhere for that reason.

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
)

func floorTestServer(t *testing.T) *apiServer {
	t.Helper()
	s := newTasksTestServer(t)
	// Two machines. Machine rows are warden-kind members: they are the
	// machine block's source AND the thing the roster must never show as a
	// colleague.
	seedMachine(t, s, "m-host-one")
	seedMachine(t, s, "m-host-two")
	return s
}

func putFloorMember(t *testing.T, s *apiServer, m Member) {
	t.Helper()
	if m.RosterStatus == "" {
		m.RosterStatus = RosterStatusActive
	}
	if err := s.dal.PutMember(m); err != nil {
		t.Fatal(err)
	}
}

func resumeFor(t *testing.T, s *apiServer, actor string) resumeSummaryDTO {
	t.Helper()
	rec := httptest.NewRecorder()
	s.HandleResumeSummaryApiResumeSummaryGet(rec, perfReq(actor, "agent"))
	if rec.Code != 200 {
		t.Fatalf("resume-summary → %d: %s", rec.Code, rec.Body.String())
	}
	var out resumeSummaryDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return out
}

func rosterRow(t *testing.T, rows []resumeRosterMemberDTO, id string) resumeRosterMemberDTO {
	t.Helper()
	for _, r := range rows {
		if r.ID == id {
			return r
		}
	}
	t.Fatalf("roster is missing %q; got %d rows: %+v", id, len(rows), rows)
	return resumeRosterMemberDTO{}
}

// TestResumeRosterCarriesEveryMemberAndContractor is the owner's ruling stated
// as a test: an OFFLINE member is still a colleague you may need to reach, and
// a contractor is still someone whose work you may be about to duplicate.
//
// MUTANT: filter the roster loop to online members only (or to
// KindAssistant only) — the offline member / the contractor disappears and
// this test goes red on the exact row it names.
func TestResumeRosterCarriesEveryMemberAndContractor(t *testing.T) {
	s := floorTestServer(t)
	putFloorMember(t, s, Member{ID: "m-alpha", Name: "Alpha", Kind: KindAssistant, RoleKey: "assistant"})
	putFloorMember(t, s, Member{ID: "m-bravo", Name: "Bravo", Kind: KindAssistant, RoleKey: "assistant"})
	putFloorMember(t, s, Member{ID: "ow-charlie", Name: "O-77", Kind: KindOutsource})

	got := resumeFor(t, s, "m-alpha")
	if len(got.Roster) != 3 {
		t.Fatalf("roster count: want exactly 3 (2 members + 1 contractor), got %d: %+v", len(got.Roster), got.Roster)
	}
	// Nobody online in this fixture: presence must still be reported, and the
	// offline rows must still be PRESENT — that is the whole ruling.
	for _, id := range []string{"m-alpha", "m-bravo", "ow-charlie"} {
		if p := rosterRow(t, got.Roster, id).Presence; p == "" {
			t.Fatalf("%s: presence must be reported, got empty", id)
		}
	}
	if k := rosterRow(t, got.Roster, "ow-charlie").Kind; k != KindOutsource {
		t.Fatalf("contractor kind: want %q, got %q", KindOutsource, k)
	}
}

// TestResumeRosterExcludesMachineRows: a warden row IS a machine. Showing it
// among colleagues would put two machines in the answer to "who can I ask".
//
// MUTANT: drop the `m.Kind == machineKind` continue in resumeFloorParts — the
// roster grows to 5 and the machine block empties; both halves go red.
func TestResumeRosterExcludesMachineRows(t *testing.T) {
	s := floorTestServer(t)
	putFloorMember(t, s, Member{ID: "m-alpha", Name: "Alpha", Kind: KindAssistant, RoleKey: "assistant"})

	got := resumeFor(t, s, "m-alpha")
	if len(got.Roster) != 1 {
		t.Fatalf("roster must hold ONLY the colleague, got %d rows: %+v", len(got.Roster), got.Roster)
	}
	for _, r := range got.Roster {
		if r.Kind == machineKind {
			t.Fatalf("machine row leaked into the roster: %+v", r)
		}
	}
	if got.Machines == nil {
		t.Fatal("machines block missing")
	}
	if len(got.Machines.List) != 2 {
		t.Fatalf("machine list: want exactly 2, got %d: %+v", len(got.Machines.List), got.Machines.List)
	}
}

// TestResumeRosterOmitsInsightAndOperationalFields pins the field set EXACTLY.
//
// This is the load-bearing guard in this file, and it guards two different
// things at once:
//
//  1. The owner's "duty only, no insight / no learning" ruling. That absence is
//     a DECISION, not a limitation — role insight is readable by any
//     authenticated identity — so nothing but a test stops a future reader from
//     "helpfully" adding it back.
//  2. The cost line. The cheapest way to write this block is to reuse the full
//     member projection, and that projection computes unread counts through a
//     full chat-table scan — on a payload every agent pulls on every wake. If
//     anyone swaps in that projection, `unread_count` (and the operator-log
//     fields) appear in this JSON and this test goes red.
//
// It asserts an EXACT key set rather than a blacklist of forbidden names: a
// blacklist only ever catches the fields someone already thought of.
//
// MUTANT: build the roster from newMemberDTO instead of resumeRosterMemberDTO —
// the key set gains unread_count / last_op_* / desired_* and this goes red.
func TestResumeRosterOmitsInsightAndOperationalFields(t *testing.T) {
	s := floorTestServer(t)
	putFloorMember(t, s, Member{ID: "m-alpha", Name: "Alpha", Kind: KindAssistant, RoleKey: "assistant"})
	// Unread chat exists in this fixture. The full member path would count it;
	// this payload must not even carry a field for it.
	if err := s.dal.PutChat(ChatMessage{ID: "c-floor-1", Sender: "m-alpha", Recipient: "owner", Body: "hi", TS: 10}); err != nil {
		t.Fatal(err)
	}

	rec := httptest.NewRecorder()
	s.HandleResumeSummaryApiResumeSummaryGet(rec, perfReq("m-alpha", "agent"))
	var raw struct {
		Roster []map[string]json.RawMessage `json:"roster"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(raw.Roster) != 1 {
		t.Fatalf("want 1 roster row, got %d", len(raw.Roster))
	}
	want := map[string]bool{
		"id": true, "name": true, "kind": true, "role_name": true,
		"duty": true, "current_task": true, "machine": true, "presence": true,
	}
	for key := range raw.Roster[0] {
		if !want[key] {
			t.Fatalf("unexpected field %q in a roster row — the wake snapshot carries "+
				"duty only (owner 2026-08-02: 不要給 insight / learning), and reusing the "+
				"full member projection would also drag in the unread scan", key)
		}
		delete(want, key)
	}
	if len(want) != 0 {
		t.Fatalf("roster row is missing required fields: %v", want)
	}
}

// TestResumeDutyIsCappedAndMarked — owner 2026-08-03「1000字 多的截斷」plus
// "Append … to let others know this is truncated".
//
// MUTANT: drop the cap in dutyText (return the definition as-is) — the length
// assertion goes red. Drop only the ellipsis — the marker assertion goes red.
// The two are asserted separately so one cannot mask the other.
func TestResumeDutyIsCappedAndMarked(t *testing.T) {
	s := floorTestServer(t)
	long := strings.Repeat("職", resumeDutyPreview+250)
	if err := s.dal.PutRoleDef(RoleDef{RoleKey: "r-verbose", Name: "Verbose Role", DefinitionMD: long}); err != nil {
		t.Fatal(err)
	}
	if err := s.dal.PutRoleDef(RoleDef{RoleKey: "r-terse", Name: "Terse Role", DefinitionMD: "接電話"}); err != nil {
		t.Fatal(err)
	}
	putFloorMember(t, s, Member{ID: "m-alpha", Name: "Alpha", Kind: KindAssistant, RoleKey: "r-verbose"})
	putFloorMember(t, s, Member{ID: "m-bravo", Name: "Bravo", Kind: KindAssistant, RoleKey: "r-terse"})

	got := resumeFor(t, s, "m-alpha")
	verbose := rosterRow(t, got.Roster, "m-alpha").Duty
	// Runes, not bytes: one CJK char is 3 bytes, so a byte-length assertion
	// here would pass for a payload three times over the cap.
	if n := len([]rune(verbose)); n != resumeDutyPreview+1 {
		t.Fatalf("capped duty length: want %d runes (cap + the ellipsis), got %d", resumeDutyPreview+1, n)
	}
	if !strings.HasSuffix(verbose, "…") {
		t.Fatal("a truncated duty must end in … so a reader can tell it was cut")
	}
	// The sentinel: a SHORT duty must come through whole and unmarked,
	// otherwise "everything ends in …" would satisfy the assertion above.
	terse := rosterRow(t, got.Roster, "m-bravo").Duty
	if terse != "接電話" {
		t.Fatalf("short duty must be carried verbatim and unmarked, got %q", terse)
	}
}

// TestResumeContractorCarriesTaskTitleAndMemberDoesNot — owner ruling
// rc-a02d8bc7fe23: 正職給職責、外包給任務標題. A contractor id is minted per task,
// so its task title IS its duty.
//
// MUTANT: drop the title cap — the length assertion goes red.
//
// MUTANT (member half), and the honest limit of it: hoisting the
// contractorTaskTitle call out of the contractor branch so members get one too
// does NOT turn this red on its own — the lookup goes through the outsource
// binding, which a member does not have, so it returns "" anyway. What DOES
// turn it red is the realistic degradation: resolving the title by executor
// (ListOpenTasksByExecutor) AND filling it for everyone — verified, this test
// then reports the member carrying "成員自己的任務標題".
//
// ⚠️ So state the coverage precisely rather than claiming more: this guards
// "members must not be given a task title by a lookup that can find one". It
// does NOT guard against someone switching to the executor-based lookup for
// contractors only — that variant keeps every assertion here green while
// quietly MULTIPLYING the full task-table scans (task.executor_id has no
// index) on the boot path by the contractor count; the path already runs two
// of them for the caller's own tasks. That risk is held by the comment on
// contractorTaskTitle and by review, not by this test.
func TestResumeContractorCarriesTaskTitleAndMemberDoesNot(t *testing.T) {
	s := floorTestServer(t)
	longTitle := strings.Repeat("務", resumeTaskTitlePreview+60)
	if err := s.dal.PutTask(Task{
		ID: "t-floor-1", TypeKey: "tm-x", Title: longTitle, Status: TaskStatusInProgress,
		Priority: TaskPriorityMid, ExecutorKind: TaskExecutorOutsource, ExecutorID: "ow-charlie",
		CreatedTS: 1000, UpdatedTS: 1000,
	}); err != nil {
		t.Fatal(err)
	}
	taskID := "t-floor-1"
	putFloorMember(t, s, Member{ID: "ow-charlie", Name: "O-77", Kind: KindOutsource, LinkedTaskID: &taskID})
	// ⚠️ The member below deliberately carries a task binding too — but be
	// precise about what it buys, because the obvious claim is FALSE and was
	// caught by review re-running the mutant by hand: hoisting the
	// contractorTaskTitle call out of the contractor branch stays GREEN with or
	// without this binding, because that lookup goes through GetOutsourceWorker
	// (`WHERE id = ? AND kind = 'outsource'`), which a member row never matches.
	// What this binding DOES buy is discriminating power against the realistic
	// degradation named in the header: resolving the title by executor
	// (ListOpenTasksByExecutor) AND filling it for everyone. Without a member
	// task row sitting on the executor side, that variant would also come out
	// empty here and stay green.
	memberTaskID := "t-floor-2"
	if err := s.dal.PutTask(Task{
		ID: memberTaskID, TypeKey: "tm-x", Title: "成員自己的任務標題", Status: TaskStatusInProgress,
		Priority: TaskPriorityMid, ExecutorKind: TaskExecutorMember, ExecutorID: "m-alpha",
		CreatedTS: 1000, UpdatedTS: 1000,
	}); err != nil {
		t.Fatal(err)
	}
	putFloorMember(t, s, Member{ID: "m-alpha", Name: "Alpha", Kind: KindAssistant,
		RoleKey: "assistant", LinkedTaskID: &memberTaskID})

	got := resumeFor(t, s, "m-alpha")
	contractor := rosterRow(t, got.Roster, "ow-charlie")
	if n := len([]rune(contractor.CurrentTask)); n != resumeTaskTitlePreview+1 {
		t.Fatalf("contractor task title: want %d runes (cap + ellipsis), got %d (%q)",
			resumeTaskTitlePreview+1, n, contractor.CurrentTask)
	}
	if !strings.HasSuffix(contractor.CurrentTask, "…") {
		t.Fatal("a truncated task title must end in …")
	}
	// A member's duty is stable and answers "is this the right person to ask";
	// its task changes daily and would churn every agent's boot for less signal.
	member := rosterRow(t, got.Roster, "m-alpha")
	if member.CurrentTask != "" {
		t.Fatalf("a member must not carry current_task, got %q", member.CurrentTask)
	}
	if contractor.Duty != "" {
		t.Fatalf("a contractor has no role, so no duty; got %q", contractor.Duty)
	}
}

// TestResumeMachinesYouAreOnIsTheServerBinding — the caller's machine comes
// from the server-recorded binding, never from a name a host reports for
// itself: our hosts report the SAME name as each other, so a hostname-derived
// answer picks the wrong box silently.
//
// MUTANT: resolve you_are_on from anything but the caller's own binding (e.g.
// hardcode the first machine in the list) — this goes red because the fixture
// deliberately puts the caller on the SECOND machine.
func TestResumeMachinesYouAreOnIsTheServerBinding(t *testing.T) {
	s := floorTestServer(t)
	putFloorMember(t, s, Member{
		ID: "m-alpha", Name: "Alpha", Kind: KindAssistant, RoleKey: "assistant",
		LastMachineID: "m-host-two",
	})
	s.telemetry.Set("m-alpha", map[string]any{"machine": "m-host-two"})

	got := resumeFor(t, s, "m-alpha")
	if got.Machines == nil {
		t.Fatal("machines block missing")
	}
	if got.Machines.YouAreOn != "m-host-two" {
		t.Fatalf("you_are_on: want m-host-two (the caller's binding), got %q", got.Machines.YouAreOn)
	}
	ids := map[string]bool{}
	for _, m := range got.Machines.List {
		ids[m.MachineID] = true
	}
	if !ids["m-host-one"] || !ids["m-host-two"] {
		t.Fatalf("machine list must carry both machines, got %+v", got.Machines.List)
	}
}

// TestResumePeekReportsTheFloorItWouldCarry — the peek and the payload are
// assembled by ONE function so their numbers cannot drift. This asserts the
// property that matters: the sizes the peek reports are the sizes of the blocks
// a real pull carries.
//
// MUTANT: compute roster_chars from anything other than the roster actually
// returned (e.g. a constant, or the untruncated duty) — the equality goes red.
func TestResumePeekReportsTheFloorItWouldCarry(t *testing.T) {
	s := floorTestServer(t)
	if err := s.dal.PutRoleDef(RoleDef{RoleKey: "r-verbose", Name: "Verbose Role",
		DefinitionMD: strings.Repeat("職", resumeDutyPreview+250)}); err != nil {
		t.Fatal(err)
	}
	putFloorMember(t, s, Member{ID: "m-alpha", Name: "Alpha", Kind: KindAssistant, RoleKey: "r-verbose"})

	full := resumeFor(t, s, "m-alpha")
	if full.Overview.RosterChars != rosterChars(full.Roster) {
		t.Fatalf("roster_chars must size the roster this payload carries: reported %d, actual %d",
			full.Overview.RosterChars, rosterChars(full.Roster))
	}
	if full.Machines == nil {
		t.Fatal("machines block missing")
	}
	if full.Overview.MachinesChars != machinesChars(*full.Machines) {
		t.Fatalf("machines_chars mismatch: reported %d, actual %d",
			full.Overview.MachinesChars, machinesChars(*full.Machines))
	}
	// The peek must report the SAME numbers without carrying the content.
	rec := httptest.NewRecorder()
	s.HandlePeekResumeSummarySizeApiResumeSummarySizeGet(rec, perfReq("m-alpha", "agent"))
	if rec.Code != 200 {
		t.Fatalf("peek → %d: %s", rec.Code, rec.Body.String())
	}
	var peek resumeSummarySizeDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &peek); err != nil {
		t.Fatalf("decode peek: %v", err)
	}
	if peek.Overview.RosterChars != full.Overview.RosterChars {
		t.Fatalf("peek roster_chars %d != payload roster_chars %d — the peek must not drift",
			peek.Overview.RosterChars, full.Overview.RosterChars)
	}
	if peek.Overview.MachinesChars != full.Overview.MachinesChars {
		t.Fatalf("peek machines_chars %d != payload machines_chars %d",
			peek.Overview.MachinesChars, full.Overview.MachinesChars)
	}
	// And it must stay a PEEK: sizes without content.
	if strings.Contains(rec.Body.String(), "roster\"") {
		t.Fatal("the peek must not carry the roster itself")
	}
}
