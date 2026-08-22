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

// The three documents that cannot be split, and why — asserted so the reason is
// checkable rather than only written down. A later package that finds a way to
// split one of them (or gets the owner's ruling to move the bytes) deletes its
// line here.
func TestBootDocRegistry_TheThreeUnsplittableKindsAreStillUnsplit(t *testing.T) {
	s := newEventProcServer(t)
	for _, kind := range []string{
		docKindTaskCloseout,
		docKindTaskTakeoverWithPredecessor,
		docKindTaskTakeoverFresh,
	} {
		t.Run(kind, func(t *testing.T) {
			spec := s.mustBootDocSpec(kind, bootDocSingletonKey)
			if spec.Split {
				t.Fatal("this kind is now declared split — if the owner ruled that its " +
					"bytes may move, say so here; if not, a prefix split reorders what " +
					"an agent reads")
			}
			seed, _, err := s.root.seedBlockMD(spec.SeedFile)
			if err != nil {
				t.Fatal(err)
			}
			// The premise of the finding: a variable really does sit outside a
			// leading run of facts, so no prefix cut leaves the body clean.
			head, _, _ := strings.Cut(seed, "。")
			if len(DocVarsIn(strings.TrimPrefix(seed, head))) == 0 {
				t.Fatal("no variable survives past the opening sentence any more — this " +
					"kind may now be splittable, and leaving it whole is no longer the " +
					"conservative choice")
			}
		})
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

// ── the write face: the head is immutable, the body has no variables ─────────

func TestReplaceBootDoc_ChangingTheReadOnlyHeadIsRefusedAndNothingIsWritten(t *testing.T) {
	s := newEventProcServer(t)
	spec, head, body := splitSeed(t, s, docKindOffboard)
	before, err := s.foldBootDocDTO(spec)
	if err != nil {
		t.Fatal(err)
	}
	for _, probe := range []struct{ name, text string }{
		{"head edited", DocJoinHeadBody(head+" 我加了一句", body)},
		{"head replaced", DocJoinHeadBody("我自己的開頭", body)},
		// Dropping the line is the same offence spelled differently: a
		// document with no boundary has no read-only half left.
		{"marker removed", head + "\n\n" + body},
	} {
		t.Run(probe.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			s.replaceBootDoc(w, ownerPost("/x"), spec, probe.text, false)
			if w.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want %d (%s)", w.Code, http.StatusBadRequest, w.Body.String())
			}
			after, err := s.foldBootDocDTO(spec)
			if err != nil {
				t.Fatal(err)
			}
			if after.Text != before.Text || !after.IsDefault {
				t.Errorf("the refused write moved the document: is_default %v→%v, %d→%d chars",
					before.IsDefault, after.IsDefault, before.SizeChars, after.SizeChars)
			}
		})
	}
	// Positive control: the SAME body under the SAME head is accepted, so the
	// refusals above are the head gate and not a face that refuses everything.
	w := httptest.NewRecorder()
	s.replaceBootDoc(w, ownerPost("/x"), spec, DocJoinHeadBody(head, body+"\n多寫一行\n"), false)
	if w.Code != http.StatusOK {
		t.Fatalf("editing only the body: status = %d, want 200 (%s)", w.Code, w.Body.String())
	}
}

func TestReplaceBootDoc_AVariableInTheEditableBodyIsRefused(t *testing.T) {
	s := newEventProcServer(t)
	spec, head, body := splitSeed(t, s, docKindOffboard)
	before, err := s.foldBootDocDTO(spec)
	if err != nil {
		t.Fatal(err)
	}
	// {where} is a name this document DOES declare — and it is still refused
	// below the line, because nothing fills a variable there. A test that used
	// an undeclared name would pass on a server that only checked the spelling.
	w := httptest.NewRecorder()
	s.replaceBootDoc(w, ownerPost("/x"), spec,
		DocJoinHeadBody(head, body+"\n你現在的狀況是 {where}。\n"), false)
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
