package main

// worker_handover_lessons_t4595_test.go — T-4595.
//
// WHY THIS FILE EXISTS. seeds/system_interaction.md §8b is the handover SOP
// every agent — staff AND outsource worker — is told to run inside the ~120s
// grace window. Step 3 used to spell one literal tool pair: `get_lessons` →
// `replace_lessons`. A worker that obeys it LITERALLY cannot succeed:
//
//   - `fillLessonsIdentityArgs` (api_roles.go) folds a blank `role_key` to the
//     caller's own roster role, and a worker's roster row carries role_key ""
//     (dal_tasks.go memberFromWorker: "role_key stays \"\""), so the fold lands
//     on defaultBootRole == "assistant";
//   - `lessonsWriteAuthz` (api_roles.go) then compares the caller's member
//     RoleKey ("") against that path role ("assistant") and answers 403.
//
// So the worker spends its handover budget on a call that cannot land, and that
// round's learnings are simply gone. Worse, the READ half is ungated, so before
// failing it reads a long-term memory that is NOT its own.
//
// These tests pin the MECHANISM, not the prose: they are what makes the §8b
// rewrite ("staff consolidate their role's lessons, outsource workers
// consolidate their task manual's learnings") a statement about this server
// rather than an opinion. If the authz chain is ever changed so a worker CAN
// write lessons, these go red and the seed sentence should be revisited.

import (
	"strings"
	"testing"
	"time"
)

// t4595WorkerIdentity stands up a wired lessons server plus one outsource
// worker roster row (the exact shape memberFromWorker writes) and returns the
// server URL and that worker's token.
func t4595WorkerIdentity(t *testing.T) (string, string) {
	t.Helper()
	srv, dal, secret := newLessonsTestServer(t)
	const workerID = "ow-t4595"
	if err := dal.PutMember(Member{
		ID: workerID, Kind: KindOutsource, RoleKey: "",
		DesiredState: DesiredStateOnline, RosterStatus: RosterStatusActive,
	}); err != nil {
		t.Fatalf("PutMember(worker): %v", err)
	}
	// Read the row back: the premise of this whole file is that a worker's
	// roster role_key is empty. Assert it from the store, do not assume it.
	got, err := dal.GetMember(workerID)
	if err != nil || got == nil {
		t.Fatalf("GetMember(worker): %v", err)
	}
	if got.RoleKey != "" {
		t.Fatalf("premise broken: an outsource roster row now carries role_key %q; "+
			"re-derive the §8b step-3 argument before trusting it", got.RoleKey)
	}
	tok, err := mintJWT(workerID, "agent", 300, secret, time.Now().Unix(), "")
	if err != nil {
		t.Fatalf("mint worker token: %v", err)
	}
	return srv.URL, tok
}

// TestWorkerCannotWriteLessonsTheHandoverSOPUsedToPrescribe drives the literal
// §8b step-3 pair as a worker. The read must succeed (that is the trap: it
// hands back somebody else's doc), the write must 403.
func TestWorkerCannotWriteLessonsTheHandoverSOPUsedToPrescribe(t *testing.T) {
	url, workerTok := t4595WorkerIdentity(t)

	// Step 3, first half — exactly what the SOP said: `get_lessons`, no
	// arguments (identity comes from the token). It SERVES.
	isErr, code, text := lessonsCall(t, url, workerTok,
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"get_lessons","arguments":{}}}`)
	if isErr {
		t.Fatalf("get_lessons as a worker must still serve (that is the trap); got code=%q", code)
	}
	// And prove it served a doc that is NOT the worker's: the identity fold
	// landed on the default boot role.
	if !strings.Contains(text, `"role_key":"`+defaultBootRole+`"`) {
		t.Fatalf("expected the blank role_key to fold to %q; body=%s", defaultBootRole, text)
	}

	// Step 3, second half — `replace_lessons`. It CANNOT land.
	isErr, code, text = lessonsCall(t, url, workerTok,
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"replace_lessons","arguments":{"text":"handover learnings from this round"}}}`)
	if !isErr {
		t.Fatalf("replace_lessons as a worker must be refused — if this now lands, "+
			"seeds/system_interaction.md §8b step 3 can go back to naming one tool pair; body=%s", text)
	}
	if code != "forbidden" {
		t.Fatalf("expected a forbidden refusal, got code=%q body=%s", code, text)
	}
}

// TestStaffCanStillWriteLessonsTheHandoverSOPPrescribes is the positive
// control. Without it, the 403 above could come from a broken fixture (a
// mis-minted token, an unwired route) rather than from the worker's missing
// role, and the seed rewrite would be justified by nothing.
func TestStaffCanStillWriteLessonsTheHandoverSOPPrescribes(t *testing.T) {
	srv, dal, secret := newLessonsTestServer(t)
	const staffID = "m-t4595staff"
	const staffRole = "r-t4595staff"
	seedLessonsOverlay(t, dal, staffRole, "general", "staff baseline\n")
	if err := dal.PutMember(Member{
		ID: staffID, Kind: KindAssistant, RoleKey: staffRole,
		DesiredState: DesiredStateOnline, RosterStatus: RosterStatusActive,
	}); err != nil {
		t.Fatalf("PutMember(staff): %v", err)
	}
	tok, err := mintJWT(staffID, "agent", 300, secret, time.Now().Unix(), "")
	if err != nil {
		t.Fatalf("mint staff token: %v", err)
	}
	if isErr, code, text := lessonsCall(t, srv.URL, tok,
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"replace_lessons","arguments":{"text":"staff handover learnings"}}}`); isErr {
		t.Fatalf("a staff member must still be able to run §8b step 3; code=%q body=%s", code, text)
	}
}

// ── the seed side of the same fix ────────────────────────────────────────────
//
// The tests above prove WHY §8b step 3 had to change; these two prove it
// actually did, in the one document both audiences read. Nothing else covers
// this: the parity equality
// (TestWorkerBootContextIsTheStaffFoldMinusThePersona) compares the two folds
// against each other, so a seed edit moves BOTH sides and stays green, and the
// §2.2 byte-identity test re-reads the same seed from disk. Reverting the seed
// wording is therefore invisible to every other guard in this package.

// handoverSectionOf returns §8b's handover SOP — the region these two sentences
// have to live in — cut out of an assembled boot context. Failing loudly when
// the anchors move is the point: an assertion over a region that silently
// became the whole document proves nothing.
func handoverSectionOf(t *testing.T, doc string) string {
	t.Helper()
	const start = "## 8b."
	const end = "## 9."
	i := strings.Index(doc, start)
	j := strings.Index(doc, end)
	if i < 0 || j < 0 || i >= j {
		t.Fatalf("cannot locate the §8b handover section (start=%d end=%d) — "+
			"re-derive these guards before trusting them", i, j)
	}
	return doc[i:j]
}

// TestHandoverStepThreeIsTrueForBothAudiences — T-4595.
//
// §8b step 3 used to spell ONE literal tool pair (get_lessons →
// replace_lessons), which a worker cannot execute at all (see the 403 proved
// above). The owner's ruling was NOT to write a second, outsource-only
// instruction — it was to make the shared sentence true for both readers.
//
// So the assertion is: BOTH arms are named, in the handover section, in the
// document BOTH audiences receive.
func TestHandoverStepThreeIsTrueForBothAudiences(t *testing.T) {
	s := newWorkerTestServer(t)
	_, staff := memberCtx(t)
	for _, tc := range []struct{ who, doc string }{
		{"outsource", workerCtxOn(t, s)},
		{"staff", staff.Context},
	} {
		t.Run(tc.who, func(t *testing.T) {
			sec := handoverSectionOf(t, tc.doc)
			for _, want := range []string{
				"`get_lessons` → `replace_lessons`",         // the staff arm
				"`get_task_manual` → `write_task_learnings`", // the outsource arm
			} {
				if !strings.Contains(sec, want) {
					t.Errorf("§8b step 3 no longer names %s — it must be true for BOTH "+
						"audiences, because both read this one sentence", want)
				}
			}
			// And it must not go back to prescribing the lessons pair
			// unconditionally: that is the exact wording that sent every worker
			// into a guaranteed 403 with its handover budget.
			if strings.Contains(sec, "**用 lessons 工具整併長期教訓**") {
				t.Error("§8b step 3 is back to one unconditional tool pair; a worker " +
					"obeying it literally spends its ~120s grace on a call that 403s " +
					"and loses that round's learnings")
			}
		})
	}
}

// TestHandoverTellsTheTakerToHaveReadTheManualsLearnings — T-4595, owner's own
// wording, pinned VERBATIM at his request.
//
// §10.2 already says 先讀手冊, but it hangs off the PLANNING action, so a task
// that was planned by someone else and then handed over never routes anyone
// past it — and a manual's learnings are precisely "what previous people got
// wrong at THIS KIND of task", which does not care whether you are the first or
// the third person on it. Hence the handover section, and hence 確認你讀過
// (have read) rather than 去讀 (go read): a second task of the same type in one
// session needs no second fetch, a fresh session after a handover does.
func TestHandoverTellsTheTakerToHaveReadTheManualsLearnings(t *testing.T) {
	const verbatim = "動手前，確認你讀過它那本手冊的學習經驗（`get_task_manual`）。"
	s := newWorkerTestServer(t)
	_, staff := memberCtx(t)
	for _, tc := range []struct{ who, doc string }{
		{"outsource", workerCtxOn(t, s)},
		{"staff", staff.Context},
	} {
		t.Run(tc.who, func(t *testing.T) {
			if !strings.Contains(handoverSectionOf(t, tc.doc), verbatim) {
				t.Errorf("the handover section does not carry the owner's sentence "+
					"verbatim:\n%s\n(it is deliberately 確認你讀過, not 去讀, and it "+
					"belongs to 接手／換手 — not under 節點規劃, which is the gap it fills)",
					verbatim)
			}
		})
	}
}
