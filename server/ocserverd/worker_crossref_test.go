package main

// worker_crossref_test.go — T-108b follow-up.
//
// TWO assembly guards live here.
//
// Guard 1 — dangling cross-references in the assembled worker context.
//
// Guard 2 — complete assembly. The worker must receive the shared core; a green
// compile is not evidence that it was actually folded into the final context.

import (
	"regexp"
	"strings"
	"testing"
)

// crossrefWorkerCtx deliberately builds through the production worker assembly
// instead of calling workerGlobalContext directly.
func crossrefWorkerCtx(t *testing.T) string {
	t.Helper()
	s := newWorkerTestServer(t)
	w := OutsourceWorker{ID: "ow-t108b-xref", Codename: "O-8", Model: "sonnet", Effort: "medium"}
	task := Task{ID: "tk-t108b-xref", Title: "T-108b crossref fixture", TypeKey: "general",
		Priority: "mid", ExecutorKind: TaskExecutorOutsource, ExecutorID: w.ID}
	putWorkerFixture(t, s, w)
	putTaskFixture(t, s, task)
	ctx, err := s.buildWorkerBootContext(w, task, nil)
	if err != nil {
		t.Fatalf("buildWorkerBootContext: %v", err)
	}
	return ctx
}

// crossrefMemberCtx is the member-side equivalent.
func crossrefMemberCtx(t *testing.T) string {
	t.Helper()
	s := newWorkerTestServer(t)
	bc, err := s.buildBootContext("", nil, "")
	if err != nil || bc == nil {
		t.Fatalf("buildBootContext: %v", err)
	}
	return bc.Context
}

// sectionRefRe matches an in-document pointer: §0, §4.1, §8b, §10.1c.
var sectionRefRe = regexp.MustCompile(`§(\d+(?:\.\d+)?[a-z]?)`)

// sectionHeadingRe matches the id of an ATX heading: "## 0. …", "### 8b. …",
// "### 10.1c …", "### 5.1 …".
var sectionHeadingRe = regexp.MustCompile(`^#{1,6}\s+(\d+(?:\.\d+)?[a-z]?)[.\s]`)

// appendixRefRe / appendixHeadingRe do the same for 附錄 A / 附錄 B.
var appendixRefRe = regexp.MustCompile(`附錄 ([A-Z])`)
var appendixHeadingRe = regexp.MustCompile(`^#{1,6}\s+附錄 ([A-Z])`)

// headingIDs collects every section id a reader of doc can actually navigate to.
func headingIDs(doc string) map[string]bool {
	ids := map[string]bool{}
	for _, line := range strings.Split(doc, "\n") {
		if m := sectionHeadingRe.FindStringSubmatch(line); m != nil {
			ids[m[1]] = true
		}
		if m := appendixHeadingRe.FindStringSubmatch(line); m != nil {
			ids["附錄 "+m[1]] = true
		}
	}
	return ids
}

// refIDs collects every pointer the doc makes.
func refIDs(doc string) map[string]bool {
	refs := map[string]bool{}
	for _, m := range sectionRefRe.FindAllStringSubmatch(doc, -1) {
		refs[m[1]] = true
	}
	for _, m := range appendixRefRe.FindAllStringSubmatch(doc, -1) {
		refs["附錄 "+m[1]] = true
	}
	return refs
}

// dangling returns the pointers in doc that resolve to nothing in doc.
func dangling(doc string) []string {
	ids := headingIDs(doc)
	var out []string
	for ref := range refIDs(doc) {
		if !ids[ref] {
			out = append(out, ref)
		}
	}
	return out
}

// TestWorkerBootContextHasNoDanglingSectionRefs is the regression guard the
// original change was missing: EVERY §N / 附錄 X pointer the worker can read
// must resolve to a heading the worker can actually reach.
//
// This is deliberately stated as a closure property of the assembled document
// rather than as a blacklist. It also catches plain typos and upstream renames.
func TestWorkerBootContextHasNoDanglingSectionRefs(t *testing.T) {
	ctx := crossrefWorkerCtx(t)

	// Positive control FIRST: a bare "grep found nothing" is worthless if the
	// extraction is broken. Prove both halves of the machinery actually fire on
	// this document before trusting the negative assertion below.
	if got := len(refIDs(ctx)); got < 5 {
		t.Fatalf("ref extraction looks broken: only %d §N refs found in the worker "+
			"context — the negative assertion below would be vacuously true", got)
	}
	if got := len(headingIDs(ctx)); got < 5 {
		t.Fatalf("heading extraction looks broken: only %d headings found in the "+
			"worker context — every ref would falsely look dangling", got)
	}
	// And prove the resolver rejects what it should.
	if d := dangling(ctx + "\n\n見 §9999 的流程。\n"); len(d) == 0 {
		t.Fatal("resolver has no teeth: an injected pointer to §9999 was not reported as dangling")
	}

	if d := dangling(ctx); len(d) > 0 {
		t.Errorf("worker boot context points at sections it does not contain: %v\n"+
			"每一個 §N 指標都必須在外包讀得到的同一份文件裡解析得到。", d)
	}
}

// TestMemberBootContextHasNoDanglingSectionRefs is the paired control. If the
// member fold also had dangling refs, the worker assertion above would be
// measuring a pre-existing defect in the seed rather than worker assembly.
func TestMemberBootContextHasNoDanglingSectionRefs(t *testing.T) {
	if d := dangling(crossrefMemberCtx(t)); len(d) > 0 {
		t.Errorf("member boot context has dangling refs %v — the worker-side "+
			"assertion needs this baseline clean", d)
	}
}

// ── Guard 2: the assembled context must retain both layers ──────────────────

// riskLanguageFloor lists the safety vocabulary that reaching an outsource
// worker IS the motivation for T-108b. Measured on base (42ea399) it was zero
// for all of these; the worker is the only role authorised to do destructive
// work itself.
var riskLanguageFloor = []string{
	"安全邊界",
	"成本煞車",
	"backup-before-destructive",
	"verify-before-assert",
}

// TestWorkerBootContextAssemblesTheSharedCore checks that the shared core is
// really folded in, without turning ordinary seed edits into a byte-for-byte
// baseline.
//
// T-4595 renamed it: there is no overlay any more. An outsource boot context is
// the staff assembly minus the persona slot, so "both required layers" collapsed
// to one — and that makes the size floor and the risk vocabulary carry the whole
// weight, since nothing else would notice the shared core going missing.
func TestWorkerBootContextAssemblesTheSharedCore(t *testing.T) {
	ctx := crossrefWorkerCtx(t)

	// Anything substantially below this floor means the shared core is not being
	// folded in.
	const floor = 30000
	if len(ctx) < floor {
		t.Fatalf("worker boot context is %d bytes, want >= %d.\n"+
			"這幾乎一定表示共用核心（系統互動 ⊕ 使用者自訂 ⊕ 啟動程序）沒有被組進來。",
			len(ctx), floor)
	}

	for _, kw := range riskLanguageFloor {
		if !strings.Contains(ctx, kw) {
			t.Errorf("worker boot context lost risk language %q — confirm the shared core "+
				"is still assembled without worker-only filtering", kw)
		}
	}
}
