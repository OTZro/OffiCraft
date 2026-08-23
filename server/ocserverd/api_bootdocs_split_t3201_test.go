package main

// api_bootdocs_split_t3201_test.go — the read-only head / editable body split
// (T-3201, second package), and the one assertion this package exists to make:
//
//	RenderDocVars(head) + join + body  ==  the bytes the server sends TODAY
//
// 🔴 THAT EQUALITY IS THE PROOF THAT NOTHING WAS REWRITTEN. This package moved
// six event texts out of Go string literals and into documents an owner can
// edit. The one thing that must not have happened along the way is a word
// changing — the ticket's boundary, verbatim: 「不順手改任何一段文字的內容 ——
// 搬家是這張票，改內容是另一件事」. A reviewer cannot hold two 1.5 KB Chinese
// paragraphs side by side and be sure; a byte comparison can.
//
// The expected values below are hand-written literals, never the constants
// under test. Quoting a constant the server also reads would make the test
// agree with whatever that constant says, including the day someone edits it.

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

// splitSeed reads one seed and cuts it, failing the test if the kind was
// declared split and the seed no longer is.
func splitSeed(t *testing.T, s *apiServer, kind string) (spec bootDocSpec, head, body string) {
	t.Helper()
	spec = s.mustBootDocSpec(kind, bootDocSingletonKey)
	seed, hasSeed, err := s.root.seedBlockMD(spec.SeedFile)
	if err != nil || !hasSeed {
		t.Fatalf("%s: read seed %q: hasSeed=%v err=%v", kind, spec.SeedFile, hasSeed, err)
	}
	head, body, ok := DocSplitHeadBody(seed)
	if !ok {
		t.Fatalf("%s: the seed carries no %s line, so it has no read-only head", kind, docBodyMarker)
	}
	return spec, head, body
}

func mustRender(t *testing.T, spec bootDocSpec, head string, values map[string]string) string {
	t.Helper()
	out, err := RenderDocVars(head, spec.Vars, values)
	if err != nil {
		t.Fatalf("%s: render head: %v", spec.Kind, err)
	}
	return out
}

// ── the verbatim proof ───────────────────────────────────────────────────────

// The offboard document's head IS the soft notice's opening sentence — no
// longer merely equal to what a Go string builder produced beside it, but the
// only place that sentence exists (T-3201, wiring package).
//
// 🔴 THE DIVERGENCE IS GONE, AND THAT IS THE CHANGE. This test used to record
// one: the document said report_stopped unconditionally while the code
// interpolated offboardCloserFor(m), which answered restart_self for a member
// still wanted online. The owner ruled the document right — verbatim
// (c-5b3d8f192a0b): 「我預期是 report_stopped，因為是 server 控制他上下線」 and
// again (rc-5d044f0c1266): 「下線程序為什麼要看到 restart_self」 — so the builder
// and its two closer constants are deleted and the live producer below now
// answers the document's own bytes on BOTH arms. The refocus arm's behaviour
// under the changed verb is pinned by
// TestSelfDrivenOffboard_StoppedReportAfterARestartSelfStampRespawns.
func TestOffboardDoc_HeadPlusBodyIsTodaysSoftNotice(t *testing.T) {
	s := newEventProcServer(t)
	spec, head, body := splitSeed(t, s, docKindOffboard)

	const where = "context 59% (your limits: 55% / 65%)"
	got := mustRender(t, spec, head, map[string]string{"where": where}) + spec.Join + body

	want := "context 59% (your limits: 55% / 65%) — start your close-out: " +
		"work the sequence below, then call report_stopped yourself.\n" + body
	if got != want {
		t.Fatalf("the folded document is not today's soft offboard notice:\n got %q\nwant %q", got, want)
	}
	// …and the same bytes the live producer builds, which is what makes the
	// hand-written literal above a statement about the SERVER and not about
	// this file.
	if live := s.winddownNoticeText(offboardKindSoft, where, 0); got != live {
		t.Fatalf("the live producer sent something else:\n got %q\nlive %q", got, live)
	}
}

func TestAcceleratedStopDoc_HeadPlusBodyIsTodaysFinalNotice(t *testing.T) {
	s := newEventProcServer(t)
	spec, head, body := splitSeed(t, s, docKindAcceleratedStop)

	const where = "close-out (your limits: 55% / 65%)"
	const epoch = 1755870180 // 2026-08-22T14:03:00Z
	deadline := time.Unix(epoch, 0).UTC().Format(time.RFC3339)
	got := mustRender(t, spec, head,
		map[string]string{"where": where, "deadline": deadline}) + spec.Join + body

	want := "close-out (your limits: 55% / 65%) — offboard now: work the sequence " +
		"below, then call report_stopped yourself. Your deadline is " + deadline + ".\n" + body
	if got != want {
		t.Fatalf("the folded document is not today's final offboard notice:\n got %q\nwant %q", got, want)
	}
	if live := s.winddownNoticeText(offboardKindFinal, where, epoch); got != live {
		t.Fatalf("the live producer sent something else:\n got %q\nlive %q", got, live)
	}
}

// 🔴 THE OTHER HALF OF THE VARIABLE MECHANISM, at the send site rather than at
// the write face: a document reaches an agent with NO {name} slot left in it,
// or it does not reach the agent at all.
//
// Both halves are asserted, and the second one is why the first is not enough.
// "No braces in what was sent" is satisfied trivially by a server that sends
// nothing, and "something was sent" is satisfied by a server that ships the
// template — so a notice that cannot be rendered must come back EMPTY (the send
// site omits the key and the agent's client falls back), never as the head with
// its braces still in it. The reachable instance of the second case is a final
// call with no clock: {deadline} is declared, nothing can fill it, and the
// answer must be "".
func TestWindDownNoticeText_SendsNoUnfilledVariableAndRefusesRatherThanShippingOne(t *testing.T) {
	s := newEventProcServer(t)
	for _, c := range []struct {
		name, kind string
		deadline   float64
	}{
		{"soft", offboardKindSoft, 0},
		{"final", offboardKindFinal, 1755870180},
	} {
		t.Run(c.name, func(t *testing.T) {
			got := s.winddownNoticeText(c.kind, "context 59% (your limits: 55% / 65%)", c.deadline)
			if got == "" {
				t.Fatal("nothing was sent at all — every assertion here would pass vacuously")
			}
			if bad := DocVarsIn(got); len(bad) > 0 {
				t.Fatalf("the notice reached the agent with %v still in it: %q", bad, got)
			}
		})
	}
	// A declared name nothing can fill: refused, not shipped as a template.
	if got := s.winddownNoticeText(offboardKindFinal, "close-out", 0); got != "" {
		t.Fatalf("a notice whose {deadline} cannot be filled must not be sent at "+
			"all — it went out as:\n%s", got)
	}
}

// 加速停止 carries a COPY of the offboard body, because the owner ruled two
// documents rather than one with an urgency flag. Two copies of a procedure
// drift; this pins that they start out identical, so the day one is edited it
// is because someone meant to.
func TestAcceleratedStopDoc_ShipsTheSameBodyAsTheOffboardDoc(t *testing.T) {
	s := newEventProcServer(t)
	_, _, offboardBody := splitSeed(t, s, docKindOffboard)
	_, _, acceleratedBody := splitSeed(t, s, docKindAcceleratedStop)
	if offboardBody != acceleratedBody {
		t.Fatalf("the two stop procedures ship different factory bodies:\n 停止 %q\n加速停止 %q",
			offboardBody, acceleratedBody)
	}
}

func TestTaskReassignPredecessorDoc_HeadPlusBodyIsTodaysChatNotice(t *testing.T) {
	s := newEventProcServer(t)
	spec, head, body := splitSeed(t, s, docKindTaskReassignPredecessor)

	got := mustRender(t, spec, head, map[string]string{
		"task_no": "T-7e91", "new_executor_label": "Rei",
	}) + spec.Join + body

	// The literal api_tasks.go used to concatenate, plus the seed FILE's
	// trailing newline — a document is a file and ends with one, a chat row is
	// one message, so the send site trims what it posts the way buildBootContext
	// trims every block it staples.
	want := "[T-7e91] 此任務已轉派給 Rei。請停止推進，改為去跟接手人做交接：" +
		"對方接手後會主動 post_chat 找你，他問目前進度、進行中的事項、有哪些雷要注意，" +
		"你都要答得出來，直到他確認交接完成。交接完成後這張任務就不再是你的了。\n"
	if got != want {
		t.Fatalf("the folded document is not today's reassign notice:\n got %q\nwant %q", got, want)
	}
	// …and the same bytes the live producer builds, which is what makes the
	// hand-written literal above a statement about the SERVER and not about
	// this file.
	if live := s.taskNoticeText(docKindTaskReassignPredecessor, map[string]string{
		"task_no": "T-7e91", "new_executor_label": "Rei",
	}); live != strings.TrimSpace(want) {
		t.Fatalf("the live producer sent something else:\n got %q\nlive %q",
			strings.TrimSpace(want), live)
	}
}

// 🔴 THE NAMED EXCEPTION. Every other document above must equal today's bytes.
// This one must NOT, and that is a ruling rather than a slip: owner, 2026-08-22
// (card rc-8c0045ef7c38), approved rewriting the body into three branches.
// Today's single sentence 「這張任務現在可以開始:請 get_task 讀內容、submit_plan
// 規劃步驟後開始執行。」 hardcodes the assumption that a blocked ticket has not
// started, and there is live evidence of it saying exactly that to a ticket
// already in progress.
//
// The HEAD is still today's bytes, half-width comma and raw English status and
// all — those three were ruled to stay verbatim in the same breath. So this
// test asserts both halves of the ruling: the head unchanged, the body changed.
func TestTaskUnblockedDoc_HeadIsVerbatimAndTheBodyIsTheApprovedRewrite(t *testing.T) {
	s := newEventProcServer(t)
	spec, head, body := splitSeed(t, s, docKindTaskUnblocked)

	gotHead := mustRender(t, spec, head, map[string]string{
		"blocked_task_no": "T-0002", "blocker_task_no": "T-0001",
		"blocker_title": "把票搬進文件", "blocker_status": "done",
	})
	// Verbatim from api_tasks_handoff.go, half-width "," included.
	wantHead := "[T-0002] 擋住這張任務的前置任務 T-0001「把票搬進文件」已經done了,它不再擋著你。"
	if gotHead != wantHead {
		t.Fatalf("the read-only head is not today's sentence:\n got %q\nwant %q", gotHead, wantHead)
	}

	wantBody := "- **還沒開始**：請 get_task 讀內容、submit_plan 規劃步驟後開始執行。\n" +
		"- **已經在進行中**：接著推進，不必重新規劃。\n" +
		"- **優先權是凍結**：先問清楚為什麼被凍結，等能解凍的人解開再動。\n"
	if body != wantBody {
		t.Fatalf("the body is not the approved three-branch rewrite:\n got %q\nwant %q", body, wantBody)
	}
	// And the old single sentence really is gone — otherwise "the rewrite
	// landed" would also be satisfied by a document carrying both.
	if strings.Contains(body, "這張任務現在可以開始") {
		t.Fatal("the pre-rewrite sentence is still in the body; the branches replace it, they do not join it")
	}
	// 🔴 AND THE LIVE PRODUCER ANSWERS IT. Until the wiring package this
	// document and the sentence releaseDependentsOnClose actually posted were
	// two different texts, and the approval above was landed in the seed while
	// the wire kept the old one — a divergence every test here passed over
	// because none of them asked the server what it sends.
	if live := s.taskNoticeText(docKindTaskUnblocked, map[string]string{
		"blocked_task_no": "T-0002", "blocker_task_no": "T-0001",
		"blocker_title": "把票搬進文件", "blocker_status": "done",
	}); live != gotHead+spec.Join+strings.TrimSuffix(body, "\n") {
		t.Fatalf("the live producer sent something else:\n got %q\nlive %q",
			gotHead+spec.Join+strings.TrimSuffix(body, "\n"), live)
	}
}

// The other half of the variable mechanism at the TASK send sites, the same
// pair of assertions winddownNoticeText carries: a document reaches an agent
// with no {name} slot left in it, or it does not reach the agent at all.
//
// "No braces in what was sent" is satisfied trivially by a server that sends
// nothing, so both cases are asserted here — a notice whose values are all
// supplied must be non-empty, and a notice missing one must come back "" rather
// than as the template. `{blocker_title}` reads like a real title and names no
// task; an agent cannot tell it was never filled.
func TestTaskNoticeText_SendsNoUnfilledVariableAndRefusesRatherThanShippingOne(t *testing.T) {
	s := newEventProcServer(t)
	full := map[string]map[string]string{
		docKindTaskReassignPredecessor: {"task_no": "T-7e91", "new_executor_label": "Rei"},
		docKindTaskUnblocked: {
			"blocked_task_no": "T-0002", "blocker_task_no": "T-0001",
			"blocker_title": "把票搬進文件", "blocker_status": "done",
		},
	}
	for kind, values := range full {
		t.Run(kind, func(t *testing.T) {
			got := s.taskNoticeText(kind, values)
			if got == "" {
				t.Fatal("nothing was sent at all — every assertion here would pass vacuously")
			}
			if bad := DocVarsIn(got); len(bad) > 0 {
				t.Fatalf("the notice reached the agent with %v still in it: %q", bad, got)
			}
			for missing := range values {
				short := map[string]string{}
				for k, v := range values {
					if k != missing {
						short[k] = v
					}
				}
				if out := s.taskNoticeText(kind, short); out != "" {
					t.Errorf("with no value for {%s} the notice must not be sent at "+
						"all — it went out as:\n%s", missing, out)
				}
			}
		})
	}
}

// ── the overlay reaches the agent ────────────────────────────────────────────

// 🔴 THIS IS THE TICKET'S OWN CLAIM, AND NOTHING ELSE ASSERTED IT. Everything
// above proves the send site ships THE DOCUMENT; none of it proves the send site
// ships the document THE OWNER EDITED. Those differ by exactly one thing — the
// overlay — and every case above reads the seed, so all of them stay green on a
// server that folds nothing and serves the shipped bytes forever. That server
// would answer 200 to every edit, record a revision for each one, and send
// agents the factory text: the precise failure docs/design/boot-documents.md
// warns about, with no surface saying a word.
//
// The edit goes through the REAL write face (replaceBootDoc — the same call the
// cockpit's PUT lands on, head gate, cap and all), never by writing the overlay
// row directly, because a test that installed the row itself would also pass on
// a server whose write face refuses every edit.
//
// The body written in is deliberately NOT the seed's: the whole assertion is
// vacuous if the two happen to agree, so the divergence is checked rather than
// assumed.
func TestEventNoticeText_SendsTheBodyTheOwnerEditedAndNotTheShippedSeed(t *testing.T) {
	const where = "context 59% (your limits: 55% / 65%)"
	const epoch = 1755870180 // 2026-08-22T14:03:00Z
	for _, tc := range []struct {
		kind    string
		trimmed bool
		send    func(s *apiServer) string
	}{
		{docKindOffboard, false, func(s *apiServer) string {
			return s.winddownNoticeText(offboardKindSoft, where, 0)
		}},
		{docKindAcceleratedStop, false, func(s *apiServer) string {
			return s.winddownNoticeText(offboardKindFinal, where, epoch)
		}},
		{docKindTaskReassignPredecessor, true, func(s *apiServer) string {
			return s.taskNoticeText(docKindTaskReassignPredecessor, map[string]string{
				"task_no": "T-7e91", "new_executor_label": "Rei",
			})
		}},
	} {
		t.Run(tc.kind, func(t *testing.T) {
			s := newEventProcServer(t)
			spec, head, seedBody := splitSeed(t, s, tc.kind)

			ownerBody := "這一段是 owner 自己改的，出廠文字裡沒有這句。\n"
			if ownerBody == seedBody {
				t.Fatal("the fixture body equals the shipped one, so this case cannot " +
					"tell an overlay-aware send site from one that ignores overlays")
			}
			w := httptest.NewRecorder()
			s.replaceBootDoc(w, ownerPost("/x"), spec, ownerBody, false)
			if w.Code != http.StatusOK {
				t.Fatalf("the write face refused the edit: %d (%s)", w.Code, w.Body.String())
			}

			values := map[string]string{"where": where}
			if tc.kind == docKindAcceleratedStop {
				values["deadline"] = time.Unix(epoch, 0).UTC().Format(time.RFC3339)
			}
			if tc.kind == docKindTaskReassignPredecessor {
				values = map[string]string{"task_no": "T-7e91", "new_executor_label": "Rei"}
			}
			want := mustRender(t, spec, head, values) + spec.Join + ownerBody
			if tc.trimmed {
				want = strings.TrimSpace(want)
			}
			got := tc.send(s)
			if got != want {
				t.Fatalf("the send site did not carry the owner's edit:\n got %q\nwant %q", got, want)
			}
			// Named separately from the equality above so a failure says WHICH
			// way it went wrong: still shipping the factory body is the specific
			// regression this case exists to catch.
			if strings.Contains(got, strings.TrimSpace(seedBody)) {
				t.Fatalf("the shipped body is still in what was sent — the send site "+
					"is reading the seed, not the fold:\n%s", got)
			}
		})
	}
}

// 🔴 THE ROW THE MARKER RELEASE LEFT BEHIND. docBodyMarker arrived with the
// split and no migration rewrote the overlays already in the database, so an
// installation that had edited one of these documents before that release is
// holding a stored text with no marker in it at all. Nothing else in the tree
// sees that shape: the write face refuses to CREATE one (asserted below), so
// every write-side guard is looking the other way, and DocRendered's no-marker
// branch hands the text back unchanged.
//
// For a NOTICE that is the whole document minus its head — the instructions with
// the facts sliced off — and it goes out NON-EMPTY, which is worse than a fault
// that returns "": every downstream "we did not send it" fallback reads a
// non-empty notice as a delivered one and stays disarmed. On the 加速停止 arm
// the sliced-off half is the only place the deadline appears, so an agent under
// a running clock is handed a notice quoting no instant, and 下線程序 §1 tells it
// to read that as a soft wind-down.
//
// The row is seeded DIRECTLY here, and that is the honest fixture rather than a
// shortcut: the write face cannot produce this shape, which is exactly why it
// survived unnoticed. The refusal is asserted first so this stays true.
func TestEventNoticeText_ASplitKindStoredWithNoMarkerIsNotSentAtAll(t *testing.T) {
	const where = "context 59% (your limits: 55% / 65%)"
	const epoch = 1755870180
	for _, tc := range []struct {
		kind string
		send func(s *apiServer) string
	}{
		{docKindOffboard, func(s *apiServer) string {
			return s.winddownNoticeText(offboardKindSoft, where, 0)
		}},
		{docKindAcceleratedStop, func(s *apiServer) string {
			return s.winddownNoticeText(offboardKindFinal, where, epoch)
		}},
		{docKindTaskReassignPredecessor, func(s *apiServer) string {
			return s.taskNoticeText(docKindTaskReassignPredecessor, map[string]string{
				"task_no": "T-7e91", "new_executor_label": "Rei",
			})
		}},
		// The FOURTH Split notice. The gate it walks into is kind-agnostic
		// (`spec.Split && !split`), so leaving this row out changed no
		// behaviour — but the table above reads as an exhaustive list of the
		// Split kinds, and a list that says "all of them" while missing one is
		// the shape this whole ticket is about.
		{docKindTaskUnblocked, func(s *apiServer) string {
			return s.taskNoticeText(docKindTaskUnblocked, map[string]string{
				"blocked_task_no": "T-7e91",
				"blocker_task_no": "T-3201",
				"blocker_title":   "擋著你的那張",
				"blocker_status":  "done",
			})
		}},
	} {
		t.Run(tc.kind, func(t *testing.T) {
			s := newEventProcServer(t)
			spec, _, body := splitSeed(t, s, tc.kind)

			// Positive control: the notice IS sent while the document is whole,
			// so the "" below is this fixture's doing and not a dead server.
			if tc.send(s) == "" {
				t.Fatal("nothing was sent even before the marker was removed")
			}

			// The write face cannot make this row — which is why seeding it
			// directly is the only way to test the shape an old release left.
			//
			// 🔴 HOW IT CANNOT CHANGED, AND THE CHECK CHANGED WITH IT (T-3201).
			// It used to REFUSE the marker-less document: 400 for an editable
			// kind, 405 for a read-only one, because no caller may write those
			// at all. The wire no longer carries a whole document, so there is
			// no refusal left to assert for the editable kinds — what is
			// asserted instead is that the very same text, sent the only way it
			// CAN be sent, still comes out of the store WITH a head on it. That
			// is the stronger claim: not "this shape is rejected" but "this
			// shape cannot be expressed". A read-only kind is still 405, and
			// asserting a flat outcome for every kind would have made the
			// fourth Split kind unaddable here.
			w := httptest.NewRecorder()
			s.replaceBootDoc(w, ownerPost("/x"), spec, body, false)
			if spec.ReadOnly {
				if w.Code != http.StatusMethodNotAllowed {
					t.Fatalf("a read-only document accepted a write: %d (%s)", w.Code, w.Body.String())
				}
			} else {
				if w.Code != http.StatusOK {
					t.Fatalf("the write face refused a variable-free body: %d (%s)", w.Code, w.Body.String())
				}
				written, err := s.foldBootDocDTO(spec)
				if err != nil {
					t.Fatal(err)
				}
				if _, _, split := DocSplitHeadBody(written.Text); !split {
					t.Fatalf("a write produced a marker-less document — this shape is "+
						"reachable from the cockpit again:\n%q", written.Text)
				}
			}

			if err := s.dal.PutBootDocument(BootDocument{
				Kind: spec.Kind, Key: spec.Key, Text: body,
			}); err != nil {
				t.Fatal(err)
			}
			// The row really is there and really has no marker — otherwise the
			// "" below would be measuring nothing.
			dto, err := s.foldBootDocDTO(spec)
			if err != nil || dto == nil || dto.Text != body {
				t.Fatalf("fixture did not land: %+v %v", dto, err)
			}
			if _, _, split := DocSplitHeadBody(dto.Text); split {
				t.Fatal("the fixture still carries a marker, so it is not the pre-marker shape")
			}

			if got := tc.send(s); got != "" {
				t.Fatalf("a document with no read-only head must not be sent at all — "+
					"on the member arm a non-empty notice disarms offboardFallback, and "+
					"on the arms with no fallback it simply misleads. It went out as:\n%s", got)
			}
		})
	}
}

// 🔴 AND THE BOOT FOLDS KEEP THE LENIENT BRANCH, deliberately. The refusal above
// belongs to notices, where the head IS the sentence; a boot document's head is
// its title line, and a reader that gets the body without it still boots. Making
// the fold refuse would turn one stale overlay into agents that cannot start —
// far worse than the hole it closes. This case is the fence around that
// asymmetry, so tightening the notice path later cannot quietly take the boot
// path with it.
func TestSystemInteractionText_AMarkerLessOverlayStillBoots(t *testing.T) {
	s := newEventProcServer(t)
	spec := s.systemInteractionSpec()
	const stored = "# Global Context（AI 工作室 · 成員 boot context）\n\n舊版沒有分隔線的內容。\n"
	if err := s.dal.PutBootDocument(BootDocument{
		Kind: spec.Kind, Key: spec.Key, Text: stored,
	}); err != nil {
		t.Fatal(err)
	}
	got, err := s.systemInteractionText()
	if err != nil {
		t.Fatal(err)
	}
	if got != stored {
		t.Fatalf("the boot fold must hand a marker-less overlay back unchanged:\n got %q\nwant %q",
			got, stored)
	}
}

// 🔴 WHY THE OTHER WIRED DOCUMENT HAS NO CASE ABOVE, asserted rather than left
// as a note in a report: 〈解除阻擋〉 is READ-ONLY, so no write face can put an
// overlay under it and "the owner's edit reaches the agent" is not a path that
// exists for it today. The read-only gate itself is pinned per face elsewhere;
// what is pinned HERE is the consequence at the send site, which no other case
// reaches — a refused edit must leave the notice byte-for-byte the shipped one,
// not merely leave the stored document alone.
//
// The day the owner rules that this document may be edited, this case goes red
// on its first assertion and the row belongs in the table above.
func TestTaskUnblockedDoc_NoWriteFaceCanPutAnOverlayUnderTheSendSite(t *testing.T) {
	s := newEventProcServer(t)
	spec, head, body := splitSeed(t, s, docKindTaskUnblocked)
	values := map[string]string{
		"blocked_task_no": "T-0002", "blocker_task_no": "T-0001",
		"blocker_title": "把票搬進文件", "blocker_status": "done",
	}
	before := s.taskNoticeText(docKindTaskUnblocked, values)

	w := httptest.NewRecorder()
	s.replaceBootDoc(w, ownerPost("/x"), spec, "我改了本體。\n", false)
	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("the replace face accepted an edit to a read-only document: %d (%s) "+
			"— it now HAS an owner-edit path and owes the overlay case above a row",
			w.Code, w.Body.String())
	}
	after := s.taskNoticeText(docKindTaskUnblocked, values)
	if after != before {
		t.Fatalf("a refused edit moved the notice:\n before %q\n after %q", before, after)
	}
	if after != mustRender(t, spec, head, values)+spec.Join+strings.TrimSuffix(body, "\n") {
		t.Fatalf("the notice is not the shipped document:\n%s", after)
	}
}

// The three documents that could not be split are split now, each on its own
// owner ruling — so what is pinned here is the RULING, not the flag: a build
// that dropped Split on any of them would go on rendering identical bytes
// (DocRendered cuts at the marker whether or not the kind declared one), and
// the two things that would quietly stop are the send-site refusal in
// eventNoticeText and the head gate on the write face. Both are behavioural
// cases below and in TestReplaceBootDoc_ChangingTheReadOnlyHeadIsRefusedAndNothingIsWritten;
// this one names the declaration they all rest on.
func TestBootDocRegistry_TheThreeFormerlyUnsplittableKindsAreSplitByRuling(t *testing.T) {
	s := newEventProcServer(t)
	for _, kind := range []string{
		docKindTaskCloseout,
		docKindTaskTakeoverWithPredecessor,
		docKindTaskTakeoverFresh,
	} {
		t.Run(kind, func(t *testing.T) {
			spec := s.mustBootDocSpec(kind, bootDocSingletonKey)
			if !spec.Split {
				t.Fatal("this kind is declared unsplit again — the owner ruled it split " +
					"(rc-0c36d8739b8f for the two 接手程序, rc-812aa13fb165 for 任務收尾); " +
					"undeclaring it silently disarms the send-site refusal and the head gate")
			}
			seed, _, err := s.root.seedBlockMD(spec.SeedFile)
			if err != nil {
				t.Fatal(err)
			}
			_, body, ok := DocSplitHeadBody(seed)
			if !ok {
				t.Fatal("declared split, but the seed carries no marker line")
			}
			// The premise of every one of the three rulings: what stopped them
			// being split was a variable outside the leading run of facts, and
			// none survives below the line now.
			if bad := DocVarsIn(body); len(bad) > 0 {
				t.Fatalf("the editable body still names %v — nothing fills a variable there", bad)
			}
		})
	}
}

// 🔴 THE {note} SLOT IS GONE FROM BOTH 接手程序, AND THE HANDOVER NOTE IS NOT.
// owner, rc-0c36d8739b8f, verbatim: 「拿掉 —— 交接備註只留在任務上」. The reassign
// writes HandoverNote/HandoverNoteTS/HandoverNoteBy onto the task and wire.go
// puts it in the DTO, so the successor still reads it with get_task; what was
// removed is the SECOND copy, which is also what made these two documents
// unsplittable. The task-side copy is asserted at the send site by
// TestReassignMemberToMemberHandsOver, so "the note is gone" cannot be
// satisfied here by a build that lost it everywhere.
func TestTaskTakeoverDocs_HeadPlusBodyIsTodaysChatNoticeWithoutTheHandoverNote(t *testing.T) {
	for _, tc := range []struct {
		kind, want string
		values     map[string]string
	}{{
		kind:   docKindTaskTakeoverFresh,
		values: map[string]string{"task_no": "T-7e91", "title": "把票搬進文件"},
		want: "[T-7e91] 你接手了任務「把票搬進文件」。請先讀任務內容，準備好後由你自己呼叫 " +
			"claim_task（認領）解除轉派鎖再開始執行；任務狀態一律照步驟推導，不必也不能自己報。\n",
	}, {
		kind: docKindTaskTakeoverWithPredecessor,
		values: map[string]string{
			"task_no": "T-7e91", "title": "把票搬進文件",
			"predecessor_label": "Ken", "old_executor_id": "m-old",
		},
		want: "[T-7e91] 你接手了任務「把票搬進文件」。你的前任是 Ken（id `m-old`）。" +
			"請先跟他確認交接完成（直接 post_chat 給他，問清楚目前進度與進行中的事項），" +
			"確認後再由你自己呼叫 claim_task（認領）解除轉派鎖——只有你這個新負責人動得了；" +
			"任務狀態一律照步驟推導，不必也不能自己報。\n",
	}} {
		t.Run(tc.kind, func(t *testing.T) {
			s := newEventProcServer(t)
			spec, head, body := splitSeed(t, s, tc.kind)

			// The literal api_tasks.go used to concatenate, minus the 交接備註
			// paragraph, plus the seed FILE's trailing newline.
			if got := mustRender(t, spec, head, tc.values) + spec.Join + body; got != tc.want {
				t.Fatalf("the folded document is not today's takeover notice minus the note:"+
					"\n got %q\nwant %q", got, tc.want)
			}
			if strings.Contains(body, "交接備註") {
				t.Fatal("the 交接備註 paragraph is still in the body — the owner ruled it out " +
					"of the notice, and while it is here the document has a variable after " +
					"its instructions and cannot be split at all")
			}
			// …and the same bytes the live producer builds, which is what makes
			// the hand-written literal above a statement about the SERVER.
			if live := s.taskNoticeText(tc.kind, tc.values); live != strings.TrimSpace(tc.want) {
				t.Fatalf("the live producer sent something else:\n got %q\nlive %q",
					strings.TrimSpace(tc.want), live)
			}
		})
	}
}

// 〈任務收尾〉 is the one of the three the owner allowed to be REWRITTEN
// (rc-812aa13fb165: 「允許改寫，但逐句先給我看」), because both {type_key} and
// {manual_label} sat in the middle of its instructions. The two names moved up
// into the head and the clauses that quoted them now point at it. Compared
// whole, because the sentence this document must NOT have lost is the one this
// very ticket is a sample of — 「不要用 write_task_learnings 做整份取代」.
//
// 🔴 THIS DOCUMENT HAS NO SEND SITE YET. decideTaskCloseNudge is a pure
// function with no server to fold an overlay through, so the sentence an agent
// receives is still the Go literal in sse_bands.go and diverges from the
// document from here on. Nothing below asserts a live producer for that reason
// — pinning the seed is all this package can honestly claim.
func TestTaskCloseoutDoc_IsTheApprovedRewriteWithBothNamesMovedIntoTheHead(t *testing.T) {
	s := newEventProcServer(t)
	spec, head, body := splitSeed(t, s, docKindTaskCloseout)

	gotHead := mustRender(t, spec, head, map[string]string{
		"task_no": "T-7d40", "status": "done",
		"type_key": "review-pr", "manual_label": "審查 PR（review-pr）",
	})
	wantHead := "任務 T-7d40 已結束（done）。這一趟的學習經驗要回到「審查 PR（review-pr）」" +
		"這本任務手冊，它的 type_key 是 `review-pr`。"
	if gotHead != wantHead {
		t.Fatalf("the read-only head is not the approved sentence:\n got %q\nwant %q", gotHead, wantHead)
	}

	wantBody := "請處理收尾事項：若這一趟有值得留下的經驗（踩坑、更好做法），先用 get_task_manual 讀現況，" +
		"再用 patch_task_learnings（type_key 就用上面那一行給的值）只把改動的那一段送回" +
		"**上面指名的那本**任務手冊：改既有段落就用它的唯一錨點，第一次寫或要新增就用空錨點追加。" +
		"不要用 write_task_learnings 做整份取代 —— 讀取後到寫入之間別人新增的內容會被無聲蓋掉；" +
		"用 `ocagent clean <path>` 移除這個任務的暫存檔/資料夾、收掉臨時 branch/worktree 與跑著的臨時程序；" +
		"最後用 report_task_closeout 回報後續已處理完。\n"
	if body != wantBody {
		t.Fatalf("the body is not the approved rewrite:\n got %q\nwant %q", body, wantBody)
	}
	// Join is "\n" so the head really is 「上面那一行」 the body tells the agent
	// to read back — with "" the two would render as one line and the
	// instruction would point at nothing.
	if spec.Join != "\n" {
		t.Fatalf("join = %q — the body says 「type_key 就用上面那一行給的值」, which is only "+
			"true while the head renders as its own line", spec.Join)
	}
}

// 🔴 THE COST OF A NAME NOTHING FILLS IS THE WHOLE NOTICE, NOT A BLANK — and
// nobody is told. RenderDocVars refuses a declared name with no value (a value
// that is present but EMPTY renders empty, which is a different thing);
// eventNoticeText turns that refusal into ""; and every send site posts nothing
// rather than posting "". So the most natural-looking fix at a send site —
// "this branch has nothing for {x}, just leave it out" — does not produce a
// notice with a gap in it. It produces no notice at all, no error, and no
// surface anywhere that says a message was owed.
//
// Both halves are asserted because neither is worth much alone: the empty
// answer is meaningless without a positive control that the same call sends
// something when every value is supplied, and "the notice is empty" says
// nothing about whether the send site would post it anyway.
func TestTaskTakeoverNotice_AValueNothingSuppliesEmptiesTheNoticeAndTheSuccessorIsSentNothing(t *testing.T) {
	s := newEventProcServer(t)
	for kind, values := range map[string]map[string]string{
		docKindTaskTakeoverFresh: {"task_no": "T-7e91", "title": "把票搬進文件"},
		docKindTaskTakeoverWithPredecessor: {
			"task_no": "T-7e91", "title": "把票搬進文件",
			"predecessor_label": "Ken", "old_executor_id": "m-old",
		},
	} {
		t.Run(kind, func(t *testing.T) {
			if s.taskNoticeText(kind, values) == "" {
				t.Fatal("nothing was sent even with every value supplied — every assertion " +
					"below would pass vacuously")
			}
			for missing := range values {
				short := map[string]string{}
				for k, v := range values {
					if k != missing {
						short[k] = v
					}
				}
				if out := s.taskNoticeText(kind, short); out != "" {
					t.Errorf("with no value for {%s} the notice must be empty — a blank "+
						"substitution reads as a real fact and names the wrong thing. "+
						"It came back as:\n%s", missing, out)
				}
				// A value that IS supplied and empty is a different case and is
				// NOT refused — so the case above is about the missing KEY, not
				// about the empty string, and a send site cannot dodge the
				// refusal by discovering that distinction by accident.
				blank := map[string]string{}
				for k, v := range values {
					blank[k] = v
				}
				blank[missing] = ""
				if out := s.taskNoticeText(kind, blank); out == "" {
					t.Errorf("an empty VALUE for {%s} must still render — only an absent "+
						"key is a fault", missing)
				}
			}
		})
	}

	// …and the consequence at the real send site: the reassign posts the
	// successor NOTHING when its notice cannot be rendered. The document is put
	// into the one unrenderable shape a live installation can actually hold (an
	// overlay written before docBodyMarker existed — the write face refuses to
	// create it, which is why it is seeded directly), because the missing-value
	// arm above is unreachable from today's call site by construction: every
	// declared name is supplied there, and the day one is not, this is what the
	// successor gets.
	api := newTasksTestServer(t)
	putActiveMember(t, api, "m-old", "Ken", KindAssistant)
	putActiveMember(t, api, "m-new", "Rei", KindAssistant)
	spec := api.mustBootDocSpec(docKindTaskTakeoverWithPredecessor, bootDocSingletonKey)
	seed, _, err := api.root.seedBlockMD(spec.SeedFile)
	if err != nil {
		t.Fatal(err)
	}
	_, body, ok := DocSplitHeadBody(seed)
	if !ok {
		t.Fatal("the seed carries no marker line")
	}
	if err := api.dal.PutBootDocument(BootDocument{
		Kind: spec.Kind, Key: spec.Key, Text: body,
	}); err != nil {
		t.Fatal(err)
	}
	task := createAdHocTask(t, api, "m-old")
	if rec := reassign(t, api, task.ID, memberTarget("m-new"), wireOwnerID, "owner"); rec.Code != http.StatusOK {
		t.Fatalf("reassign: %d %s", rec.Code, rec.Body.String())
	}
	msgs, err := api.dal.ListChat()
	if err != nil {
		t.Fatal(err)
	}
	var toOld, toNew *ChatMessage
	for i := range msgs {
		switch msgs[i].Recipient {
		case "m-old":
			toOld = &msgs[i]
		case "m-new":
			toNew = &msgs[i]
		}
	}
	// The predecessor's own notice is a different document and must still have
	// gone out — otherwise "nothing was posted" would be a broken handler
	// rather than the refusal under test.
	if toOld == nil {
		t.Fatal("the predecessor's notice went missing too — this is not the refusal under test")
	}
	if toNew != nil {
		t.Fatalf("a notice that could not be rendered must not be posted at all; the "+
			"successor received:\n%s", toNew.Body)
	}
}

// ── the two boot documents whose head is their own title line ────────────────

// The promotion added no wording: the line that already sat at the top of the
// file became the head. What a reader gets must therefore still open with that
// exact line, and must never contain the marker.
func TestBootContextDocs_RenderWithoutTheMarkerAndKeepTheirTitleLine(t *testing.T) {
	s := newEventProcServer(t)
	for _, tc := range []struct {
		kind, key, wantHead string
	}{
		{docKindSystemInteraction, systemInteractionDocKey,
			"# Global Context（AI 工作室 · 成員 boot context）"},
		{docKindBootSequence, bootSequenceKeyClaude, "# Claude Code 執行環境"},
		{docKindBootSequence, bootSequenceKeyCodex, "# Codex App Server 執行環境"},
	} {
		t.Run(tc.kind+"/"+tc.key, func(t *testing.T) {
			spec := s.mustBootDocSpec(tc.kind, tc.key)
			seed, _, err := s.root.seedBlockMD(spec.SeedFile)
			if err != nil {
				t.Fatal(err)
			}
			head, body, ok := DocSplitHeadBody(seed)
			if !ok {
				t.Fatalf("the seed carries no %s line", docBodyMarker)
			}
			if head != tc.wantHead {
				t.Fatalf("head = %q, want %q — promoting the title was supposed to add "+
					"no wording at all", head, tc.wantHead)
			}
			rendered := DocRendered(seed, spec.Join)
			if strings.Contains(rendered, docBodyMarker) {
				t.Fatal("the marker line survived into what a reader gets")
			}
			if rendered != tc.wantHead+"\n\n"+body {
				t.Fatalf("the rendered document is not the title, a blank line and the body:\n%q", rendered)
			}
		})
	}
}

// ── the write face: the head is UNSENDABLE, the body has no variables ────────

// 🔴 THIS USED TO ASSERT A REFUSAL AND NOW ASSERTS THAT THERE IS NOTHING TO
// REFUSE (T-3201, owner's ruling 「沒有人有任何方式可以回寫」). Three probes stood
// here — head edited, head replaced, marker removed — each a whole document
// sent back with the read-only half tampered with, each answered 400. The wire
// no longer has a field that can carry a head, so none of the three is a
// request anybody can make: what is left to measure is that whatever a caller
// puts in the body, the STORED document still carries the shipped head above
// the line.
//
// The probes are therefore the same three strings, sent as BODY text — the
// exact payloads a caller who still believed he was sending a whole document
// would produce. Every one of them is accepted (it is just text) and every one
// of them lands under the shipped head, which is the thing worth pinning: the
// old protocol's payload can no longer reach the head even by accident.
//
// 接手程序（有前任） rides along as a second kind: it is the one of the three
// documents split by T-3201's last package that an owner may actually edit, so
// it is the one where this is newly load-bearing — and the join reads
// spec.Split, which a build that undeclared the split would walk straight past.
func TestReplaceBootDoc_NoWriteCanChangeTheReadOnlyHead(t *testing.T) {
	for _, kind := range []string{docKindOffboard, docKindTaskTakeoverWithPredecessor} {
		t.Run(kind, func(t *testing.T) {
			replaceBootDocHeadIsUnsendableCase(t, kind)
		})
	}
}

func replaceBootDocHeadIsUnsendableCase(t *testing.T, kind string) {
	t.Helper()
	s := newEventProcServer(t)
	spec, head, body := splitSeed(t, s, kind)
	// ⚠️ THE PROBES DO NOT CARRY THE REAL HEAD, and that is a fact about a
	// DIFFERENT gate rather than a weaker test. These heads name {variables} —
	// that is what a head is for — and a variable anywhere in the body is
	// refused by the body rule, which would answer 400 for a reason that has
	// nothing to do with the head. So each probe is a head-SHAPED string with
	// no braces in it: the payload of the old whole-document protocol, in the
	// only form that gets far enough to prove the point.
	for _, probe := range []struct{ name, text string }{
		{"an edited head sent as body", DocJoinHeadBody("我自己的開頭 我加了一句", body)},
		{"someone else's head sent as body", DocJoinHeadBody("我自己的開頭", body)},
		{"a whole document with no marker", "我自己的開頭\n\n" + body},
	} {
		t.Run(probe.name, func(t *testing.T) {
			s := newEventProcServer(t)
			w := httptest.NewRecorder()
			s.replaceBootDoc(w, ownerPost("/x"), spec, probe.text, false)
			if w.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200 — body text is body text (%s)", w.Code, w.Body.String())
			}
			after, err := s.foldBootDocDTO(spec)
			if err != nil {
				t.Fatal(err)
			}
			if after.ReadOnlyHead != head {
				t.Fatalf("the stored read-only head is not the shipped one:\n got %q\nwant %q",
					after.ReadOnlyHead, head)
			}
			if after.Body != probe.text {
				t.Fatalf("the body stored is not what was sent:\n got %q\nsent %q", after.Body, probe.text)
			}
			if after.Text != DocJoinHeadBody(head, probe.text) {
				t.Fatalf("the stored document is not head ⊕ body:\n%q", after.Text)
			}
		})
	}
	// Control: an ordinary body edit is accepted and lands the same way, so the
	// assertions above are not a face that stores the head no matter what
	// because it refuses to store anything.
	s2 := newEventProcServer(t)
	w := httptest.NewRecorder()
	s2.replaceBootDoc(w, ownerPost("/x"), spec, body+"\n多寫一行\n", false)
	if w.Code != http.StatusOK {
		t.Fatalf("editing only the body: status = %d, want 200 (%s)", w.Code, w.Body.String())
	}
	after, err := s2.foldBootDocDTO(spec)
	if err != nil {
		t.Fatal(err)
	}
	if after.ReadOnlyHead != head || after.Body != body+"\n多寫一行\n" {
		t.Fatalf("an ordinary edit did not land as head ⊕ body:\n%q", after.Text)
	}
}

// 🔴 THE TWO GATES CHANGED UNITS AND THE CHANGE IS INVISIBLE WITHOUT THIS TEST.
// Both used to compare whole document against whole document, because that was
// the only thing a caller could send. The body-only wire split the question, and
// each gate now measures the one thing its own question is about:
//
//   - the WIPE guard asks whether this write emptied the document of everything
//     the caller can put in it, so it judges the BODY. Judged on the joined text
//     it would answer "nothing was emptied" for a caller who just erased the
//     whole of his own half — the head survives every write, so no write can
//     produce an empty document again and the guard would be retired by
//     accident, on a face nobody would think to look at.
//
//   - the CAP asks whether the document that gets STORED fits its ceiling, so it
//     judges the joined text. That is the number size_chars reports, the number
//     the cockpit shows against cap_chars and the number docCapRefusal quotes;
//     measured on the body, all three would say different things about one
//     document and the owner would be refused at a length his screen called fine.
//
// Both halves are asserted with their opposites, because either gate reading the
// other's unit passes the loose half of this test on its own.
func TestReplaceBootDoc_TheWipeGuardJudgesTheBodyAndTheCapJudgesTheStoredDocument(t *testing.T) {
	t.Run("the wipe guard judges the body", func(t *testing.T) {
		s := newEventProcServer(t)
		spec, head, _ := splitSeed(t, s, docKindOffboard)

		w := httptest.NewRecorder()
		s.replaceBootDoc(w, ownerPost("/x"), spec, "", false)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("emptying the body must be refused without allow_shrink; got %d (%s)",
				w.Code, w.Body.String())
		}
		// The control that makes the refusal above mean "the BODY was judged":
		// what would have been stored is NOT empty — it is the head and the
		// separator — so a guard reading the stored document would have let it
		// through with nothing to say.
		stored, err := s.bootDocStoredText(spec, "")
		if err != nil {
			t.Fatal(err)
		}
		if strings.TrimSpace(stored) == "" {
			t.Fatal("fixture: an emptied body still stores the head, or this control proves nothing")
		}
		if !strings.HasPrefix(stored, head) {
			t.Fatalf("an emptied body did not keep the shipped head:\n%q", stored)
		}

		// …and allow_shrink is still the way through, on the same unit.
		rec := httptest.NewRecorder()
		s.replaceBootDoc(rec, ownerPost("/x"), spec, "", true)
		if rec.Code != http.StatusOK {
			t.Fatalf("allow_shrink must let the wipe through; got %d (%s)", rec.Code, rec.Body.String())
		}
		after, err := s.foldBootDocDTO(spec)
		if err != nil {
			t.Fatal(err)
		}
		if after.Body != "" || after.ReadOnlyHead != head {
			t.Fatalf("an allowed wipe should empty the body and keep the head; body=%q head kept=%v",
				after.Body, after.ReadOnlyHead == head)
		}
	})

	t.Run("the cap judges the stored document", func(t *testing.T) {
		s := newEventProcServer(t)
		spec, head, _ := splitSeed(t, s, docKindOffboard)
		before, err := s.foldBootDocDTO(spec)
		if err != nil {
			t.Fatal(err)
		}
		overhead := utf8.RuneCountInString(DocJoinHeadBody(head, ""))
		if overhead <= 0 {
			t.Fatal("fixture: this document has no read-only half, so the two units coincide")
		}
		// A body that fits the cap on its own and does NOT fit once the head is
		// joined on. This is the exact window a body-measuring cap would let
		// through, and it is why the window has to be non-empty for the test to
		// mean anything.
		body := strings.Repeat("字", spec.Cap-overhead+1)
		if utf8.RuneCountInString(body) > spec.Cap {
			t.Fatalf("fixture: the probe (%d runes) is over the %d cap on its own",
				utf8.RuneCountInString(body), spec.Cap)
		}
		if utf8.RuneCountInString(DocJoinHeadBody(head, body)) <= spec.Cap {
			t.Fatal("fixture: the probe fits even once stored, so nothing is being measured")
		}
		w := httptest.NewRecorder()
		s.replaceBootDoc(w, ownerPost("/x"), spec, body, false)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("a body that only fits WITHOUT the head must be refused; got %d (%s)",
				w.Code, w.Body.String())
		}
		after, err := s.foldBootDocDTO(spec)
		if err != nil {
			t.Fatal(err)
		}
		if after.Text != before.Text || !after.IsDefault {
			t.Errorf("the refused write moved the document: is_default %v→%v", before.IsDefault, after.IsDefault)
		}
		// The control: one rune shorter fits once stored, and is accepted. Without
		// it a cap that refused everything would pass the half above.
		shorter := strings.Repeat("字", spec.Cap-overhead)
		rec := httptest.NewRecorder()
		s.replaceBootDoc(rec, ownerPost("/x"), spec, shorter, false)
		if rec.Code != http.StatusOK {
			t.Fatalf("a body that fits exactly once stored must be accepted; got %d (%s)",
				rec.Code, rec.Body.String())
		}
		wrote, err := s.foldBootDocDTO(spec)
		if err != nil {
			t.Fatal(err)
		}
		if wrote.SizeChars != spec.Cap {
			t.Fatalf("the accepted document is %d runes, want exactly the %d cap — the "+
				"boundary this pair measures is the STORED size", wrote.SizeChars, spec.Cap)
		}
	})
}

func TestReplaceBootDoc_AVariableInTheEditableBodyIsRefused(t *testing.T) {
	s := newEventProcServer(t)
	spec, _, body := splitSeed(t, s, docKindOffboard)
	before, err := s.foldBootDocDTO(spec)
	if err != nil {
		t.Fatal(err)
	}
	// {where} is a name this document DOES declare — and it is still refused
	// below the line, because nothing fills a variable there. A test that used
	// an undeclared name would pass on a server that only checked the spelling.
	w := httptest.NewRecorder()
	s.replaceBootDoc(w, ownerPost("/x"), spec, body+"\n你現在的狀況是 {where}。\n", false)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d (%s)", w.Code, http.StatusBadRequest, w.Body.String())
	}
	after, err := s.foldBootDocDTO(spec)
	if err != nil {
		t.Fatal(err)
	}
	if after.Text != before.Text || !after.IsDefault {
		t.Errorf("the refused write moved the document: is_default %v→%v", before.IsDefault, after.IsDefault)
	}
}

// Every split kind that is variable-validated must ship a body with no
// variables at all — the factory text has to satisfy the rule the write face
// enforces, or the first owner edit is refused for something he did not write.
func TestBootDocRegistry_EverySplitSeedHasAVariableFreeBody(t *testing.T) {
	s := newEventProcServer(t)
	for _, reg := range bootDocRegistry {
		if !reg.Split {
			continue
		}
		for _, key := range reg.Keys {
			t.Run(reg.Kind+"/"+key, func(t *testing.T) {
				spec := s.mustBootDocSpec(reg.Kind, key)
				seed, _, err := s.root.seedBlockMD(spec.SeedFile)
				if err != nil {
					t.Fatal(err)
				}
				head, body, ok := DocSplitHeadBody(seed)
				if !ok {
					t.Fatalf("declared split, but the seed carries no %s line", docBodyMarker)
				}
				if spec.Vars == nil {
					return // opted out of the syntax entirely — see doc_vars.go
				}
				if bad := DocVarsIn(body); len(bad) > 0 {
					t.Errorf("the editable body names %v; nothing fills a variable there", bad)
				}
				if bad := DocVarsUndeclared(head, spec.Vars); len(bad) > 0 {
					t.Errorf("the head uses %v, which the kind does not declare", bad)
				}
			})
		}
	}
}
