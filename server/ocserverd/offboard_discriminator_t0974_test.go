package main

// offboard_discriminator_t0974_test.go — the rule that tells a SOFT offboard
// notice from a HARD one, applied to the notices themselves.
//
// 🔴 THE DEFECT THIS ANSWERS, AND WHY NOTHING CAUGHT IT (measured on the live
// station, 2026-08-20, by the shipping verifier — not a hypothetical):
//
// The document the server staples to every offboard notice opened with the
// discriminator, spelled out for the reader:
//
//	- 通知帶有 `Your deadline is ...`：**硬性**。
//	- 沒有這個標記：**軟性**。
//
// That sentence CONTAINS THE MARKER IT IS DESCRIBING, and the document rides
// inside the very notice being judged. So a SOFT notice — one with no clock on
// it at all — carries the marker once, in the line explaining the rule.
//
//	an agent applying the rule as written  ⇒  every soft notice reads as HARD
//
// The failure direction is the worst one available: "you have room, let your
// sub-agents finish" is read as "you have seconds left", and the agent drops
// in-flight work it was never out of time for. A generation of this developer
// wrote "硬性通知，剩不到一分鐘" into its own hand-off with NO instant quoted
// anywhere — an estimate, not a reading — and left four unanswered cards and
// two unreported delegations behind.
//
// Every existing test around this sentence asserted only that the HARD notice
// CONTAINS the clause. Not one of them ever applied the discriminator to the
// SOFT notice, so the rule was never once evaluated against the thing it
// judges. That is the shape of this whole class of bug: the rule and the rule's
// own statement live in one string, and that string is the object under test.
//
// ⇒ The fix is NOT an exception for "the line that explains the rule". A
// discriminator that needs a carve-out for its own description is matching the
// wrong thing. What actually separates the two notices is not the words — it is
// whether there is A CONCRETE INSTANT. The prose now says so, and this file
// pins it by EVALUATING the rule, not by quoting it.
//
// 🔴 WHAT THIS FILE DOES NOT GUARD — MEASURED, NOT ASSUMED.
//
// Four mutants were run against it. Three go red:
//
//	an example instant pasted into §1's prose         → RED
//	the "?" placeholder restored in the notice        → RED
//	the deadline clause appended UNCONDITIONALLY      → RED
//
// ⚠️ THE THIRD ONE CHANGED SHAPE IN T-3201 and got stronger for it. The clause
// used to be a Go `if finalCall && deadline > 0`, and the mutant that actually
// resembles a mistake — dropping the `finalCall` half — left the whole package
// green, because nothing called the builder with finalCall=false alongside a
// deadline. There is no builder and no flag any more: the clause lives in the
// 加速停止 document's head and NOTHING ELSE HAS ONE, so "appended
// unconditionally" is now spelled "the soft arm was sent the hard document" —
// which is the mutant TestOffboardNotice_ASoftNoticeIgnoresADeadlineItWasHanded
// and TestOffboardDiscriminator_AppliedToTheNoticesThemselves both catch.
//
// 🔴 AND ONE MORE THING THIS FILE DOES NOT GUARD, found by independent review:
// it reads §1 from the SEED (assetRoot("").readSeedFile), while what actually
// reaches an agent is the folded boot DOCUMENT in the database, which the owner
// can rewrite through replace_offboard. The two agree
// only while nobody has overridden it, and this station's override has been
// cleared and reinstated more than once. So a green here is evidence about the
// factory text, NOT about the sentence a live agent receives; checking the live
// document needs a running station and is not attempted here.
//
// The fourth mutant STAYS GREEN, and it is the original defect itself:
//
//	§1 reverted word-for-word to "通知帶有 `Your deadline is ...`：硬性"  → GREEN
//
// That is not an oversight to fix later. The rule this file evaluates is the
// CORRECTED one (marker + instant); the rule an AGENT executes is whatever the
// prose says, and prose is read, not run. A revert to a substring-shaped
// wording produces a document that still contains no instant — so every
// assertion here passes while every soft notice is once again misread.
//
// The tempting patch is to assert the prose does NOT contain a substring-test
// phrasing. That trades this hole for a worse one: a wording whitelist fires a
// FALSE RED on the next legitimate rewrite, and a guard that cries wolf is
// removed, after which nothing is guarded at all.
//
// ⇒ THE HONEST STATE: the §1 wording is held by review and by this comment,
// not by a test. If you are editing §1, the question to answer is not "do the
// tests pass" — it is "would this sentence match ITSELF when it is stapled
// inside the notice being judged?"
//
// ─────────────────────────────────────────────────────────────────────────────
// 🔴 T-6f44 REMOVED THE RULE THIS FILE WAS BUILT AROUND, AND THAT IS THE FIX
// THE COMMENT ABOVE SAID IT COULD NOT MAKE.
//
// Everything above is a description of one defect class: a rule stated as a
// STRING TEST, inside the string it tests. The last paragraph names the residual
// hole exactly — a revert of §1 to substring-shaped wording leaves every
// assertion green — and calls it unfixable without a wording whitelist.
//
// The owner's decision 5 dissolved the premise instead: §1 no longer asks the
// agent to look for `Your deadline is` in the notice. Each document now STATES
// WHICH ONE IT IS (「你讀到的是這一份，就代表**沒有人在對你倒數**」/「**你在倒數
// 中**」), and which document the server sends is decided by `kind` at the send
// site. There is no marker to self-match, so the fourth mutant is gone rather
// than tolerated — a §1 reverted to substring wording is now caught, because the
// substring it would name is asserted absent from BOTH documents below.
//
// What is still worth pinning, and is pinned here, is the same PROPERTY under
// the new mechanism: the soft notice must not read as hard, and the hard one
// must not read as soft — evaluated against the notices the live producer
// builds, never against a rule quoted from the prose.
//
// ⚠️ WHAT WENT WITH IT. The clause `Your deadline is <instant>` was the only
// ENGLISH in either notice and it lived in 加速停止's read-only head next to
// {where}. Decision 4 deleted {where}; the head is now one Chinese sentence
// carrying the instant alone. So a regex over English is no longer the shape of
// any assertion here.

import (
	"regexp"
	"strings"
	"testing"
	"time"
)

// discriminator is what a HARD notice must carry and a soft one must not: a
// concrete instant. It is still expressed as an INSTANT rather than as words,
// for the reason the header gives — matching the words is the defect — but it no
// longer looks for an English marker, because there is none: 加速停止's head is
// 「你的結束時刻是 <RFC3339>。」 and 〈停止〉's document has no head at all.
var discriminator = regexp.MustCompile(
	`你的結束時刻是 \d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}Z。`)

// sniffedMarker is the string §1 used to tell agents to look for. It must appear
// in NEITHER document: while it is in one of them, an agent reading that document
// has two contradicting rules for the same question, and the string-matching one
// is the one that silently stops working when the other document is edited.
const sniffedMarker = "Your deadline is"

// TestOffboardDiscriminator_AppliedToTheNoticesThemselves is the guard that was
// missing. It runs the rule over BOTH notices AND over the document alone.
func TestOffboardDiscriminator_AppliedToTheNoticesThemselves(t *testing.T) {
	doc, err := assetRoot("").readSeedFile(offboardSeedMD)
	if err != nil {
		t.Fatalf("read the 〈停止〉 seed: %v", err)
	}
	// DENOMINATOR FIRST. A comparison whose two sides are both empty reports
	// "the same" and tells you nothing; the shipping verifier lost a round to
	// exactly that (a shell-eaten sed extracted "" from both revisions and the
	// diff came back clean). So prove the seed is really here before drawing any
	// conclusion from what it does or does not contain.
	if len(doc) < 200 || !strings.Contains(doc, "## 1.") {
		t.Fatalf("the seed did not load — every assertion below would pass "+
			"vacuously (%d bytes)", len(doc))
	}
	accelDoc, err := assetRoot("").readSeedFile(acceleratedStopSeedMD)
	if err != nil {
		t.Fatalf("read the 加速停止 seed: %v", err)
	}
	if len(accelDoc) < 200 || !strings.Contains(accelDoc, "## 1.") {
		t.Fatalf("the 加速停止 seed did not load (%d bytes)", len(accelDoc))
	}

	const where = "context 62% (your limits: 60% / 75%)"
	const deadline = 1_787_000_000.0 // 2026-08-17T20:53:20Z

	// The LIVE producer on both arms, which is what makes "soft reads as soft"
	// a statement about the server rather than about arguments this file chose:
	// the soft arm is sent 〈停止〉, the final call 加速停止, and only the second
	// of the two documents carries the clause.
	s := newReconcileTestServer(t)
	soft := s.winddownNoticeText(offboardKindSoft, 0)
	hard := s.winddownNoticeText(offboardKindFinal, deadline)
	if soft == "" || hard == "" {
		t.Fatal("the fixture must render both notices — an empty one would pass " +
			"every assertion below vacuously")
	}

	// ── the two that matter ──────────────────────────────────────────────────
	if discriminator.MatchString(soft) {
		t.Errorf("A SOFT notice must NOT read as hard. The rule matched %q "+
			"inside a notice that carries no clock at all — which is the "+
			"original defect: the line stating the rule is itself being "+
			"matched.\n\n%s",
			discriminator.FindString(soft), soft)
	}
	if !discriminator.MatchString(hard) {
		t.Errorf("A HARD notice must read as hard, and this one does not — so "+
			"the rule and the sentence have drifted apart:\n\n%s", hard)
	}

	// ── the regression itself, stated directly ───────────────────────────────
	// The document travels inside its notice. If the rule matches a document ON
	// ITS OWN, it matches every notice that carries it and the discriminator is
	// worthless no matter what the two cases above say.
	if discriminator.MatchString(doc) {
		t.Errorf("the 〈停止〉 document must not carry an instant — it rides "+
			"inside every soft notice, so one here makes every soft notice read "+
			"as hard. Found %q", discriminator.FindString(doc))
	}

	// 🔴 THE FOURTH MUTANT, NOW CAUGHT (T-6f44). The header above records it as
	// permanently green: 「§1 reverted word-for-word to 通知帶有 `Your deadline
	// is ...`：硬性」. It is caught now, and not by a wording whitelist — by the
	// one string that rule cannot be written without. While that marker is in
	// either document, an agent is carrying two rules for one question and the
	// string one breaks silently the day the other document is edited.
	for _, d := range []struct{ name, text string }{
		{"停止", doc},
		{"加速停止", accelDoc},
	} {
		if strings.Contains(d.text, sniffedMarker) {
			t.Errorf("%s still tells the agent to look for %q. Decision 5 replaced "+
				"that with each document saying which one it is — a string test "+
				"against ANOTHER document's editable first line stops working the "+
				"day that line is rewritten, and no test goes red", d.name, sniffedMarker)
		}
	}

	// …and the replacement really is there, in both, and says opposite things.
	// Without this the block above is satisfied by deleting §1 altogether, which
	// would leave the reader with no rule at all — the exact degeneration the
	// old version of this test guarded against from the other side.
	if !strings.Contains(doc, "沒有人在對你倒數") {
		t.Error("〈停止〉 no longer tells the agent it is NOT being counted down")
	}
	if !strings.Contains(accelDoc, "你在倒數中") {
		t.Error("加速停止 no longer tells the agent it IS being counted down")
	}
	if strings.Contains(doc, "你在倒數中") || strings.Contains(accelDoc, "沒有人在對你倒數") {
		t.Error("a stop procedure claims to be the other kind — the send site picks " +
			"the document by kind, so the document must agree with why it was sent")
	}
}

// 🔴 THE PLACEHOLDER CANNOT REACH AN AGENT ANY MORE, BECAUSE THE POSITION
// CLAUSE CANNOT (T-6f44, decision 4: 「{where} 不中文化，直接砍掉」). This used to
// pin that a missing gauge value was OMITTED rather than printed as a literal
// "?" — 「context ?% (your limits: 55% / 65%)」 on every refocus-triggered
// close-out, an arm that is not fired by a percentage at all.
//
// 〈停止〉 no longer has a read-only head, so the notice IS the document and
// carries no position at all: 「你在 59%」 has nothing to do with how to close
// out. The defect this test was written for is therefore structurally gone
// rather than fixed — which is a stronger state, and worth pinning as itself.
//
// ⚠️ THE `where` STRING IS STILL COMPOSED UPSTREAM (offboardNoticeFor builds it
// and hands it to winddownNoticeText, which now discards it). It is dead text —
// the 定稿 calls for deleting the code that builds it — and that deletion lives
// in the wind-down files, outside this change. Until then the assertion that
// matters is the one below: whatever that string says, no agent sees it.
//
// BOTH RUNTIMES are still swept, because they read different gauge keys and the
// claim is that NEITHER can leak a placeholder now.
func TestOffboardNotice_NoQuestionMarkForAMissingPercentage(t *testing.T) {
	// BOTH RUNTIMES, because they read DIFFERENT gauge keys and therefore need
	// two separate fallbacks in the source. Running this on claude alone is how
	// the codex arm went on printing "compaction round ?" through a whole
	// ticket that was ABOUT the question mark — an independent review measured
	// it: restoring the literal "?" on the codex arm left the entire package
	// green.
	//
	// ⚠️ WHOLE-STRING comparison (owner ruling 2026-08-20, c-2502de439aaa:
	// 「你如果要比對 context 就是比對一整份要一模一樣」). offboardNoticeFor is
	// deterministic and the document is empty on this server, so the complete
	// expected value is computable — and it pins the ABSENCE of the placeholder
	// together with everything else the sentence must carry, which two keyword
	// assertions could not.
	for _, c := range []struct {
		name    string
		runtime string
	}{
		{name: "claude sends no position at all", runtime: RuntimeClaude},
		{name: "codex sends no position at all", runtime: RuntimeCodex},
	} {
		t.Run(c.name, func(t *testing.T) {
			s := newReconcileTestServer(t)
			// The two runtimes' limits are set HERE rather than taken from the
			// test server's zero values, because codex's zero values render as
			// "round -1 / round 0" — pinning that would make the expected value
			// a bug report. The claude pair is the shipped default.
			// Under settingsMu because the getters read it under RLock; CI does not
			// run -race today, so an unlocked write here would simply never be caught.
			s.settingsMu.Lock()
			s.codexNoticeRound, s.codexCompactionThreshold = 3, 4
			s.settingsMu.Unlock()
			m := testAgent("m-nopct")
			m.Runtime = c.runtime
			m.RefocusSince = nowSecs()
			m.RefocusOp = refocusOpRefocus
			putTestMember(t, s, m)
			// NO gauge entry at all — which is the refocus arm's NORMAL state,
			// not an edge case: that arm is not triggered by a percentage or a
			// round count, so there has never been one to report.
			//
			// ⚠️ WHOLE-STRING comparison still (owner ruling 2026-08-20,
			// c-2502de439aaa). The expected value is now the DOCUMENT, with
			// nothing composed around it — which is the whole claim.
			want := mustFoldText(t, s, s.offboardSpec())
			got := s.offboardNoticeFor(m, offboardKindSoft)
			if got != want {
				t.Fatalf("the soft notice is not the document alone:\n got %q\nwant %q", got, want)
			}
			// Named separately so a failure says WHICH way it went wrong: a
			// placeholder, or a position clause of any kind, reaching an agent.
			if strings.Contains(got, "?") || strings.Contains(got, "your limits:") {
				t.Fatalf("a position clause reached the agent:\n%s", got)
			}
		})
	}
}

// TestOffboardNotice_ASoftNoticeIgnoresADeadlineItWasHanded closes the gap the
// mutant table above names: the soft arm must drop a deadline it is GIVEN, not
// merely go without one because no caller supplies it.
//
// 🔴 WHY THIS CANNOT BE LEFT TO THE CALLER. offboardNoticeFor hands
// winddownDeadlineOf(...) to BOTH arms and lets `kind` decide, so a soft arm
// whose member happens to carry a live refocus epoch reaches the renderer with
// a positive deadline. Since T-3201 the clause is not a Go condition at all —
// it is a slot the 〈停止〉 head does not have — and this test is what pins
// that a deadline handed to the soft arm changes NOTHING it sends. The failure
// it refuses is the send site picking the document by anything but `kind`.
//
// ⚠️ IT COMPARES THE WHOLE STRING, NOT A KEYWORD. Owner ruling 2026-08-20
// (c-cdcaabeaf159 / c-2502de439aaa): 「你如果要比對 context 就是比對一整份要
// 一模一樣，比對部分的關鍵詞增加測試時間卻沒有得到我們想要的測試效果」. A
// substring assertion passes when the wording around it changed meaning and
// fails when a harmless rewording moved the same meaning; a whole-string
// comparison against a value built from the SAME inputs is a real assertion
// about what the function computes.
func TestOffboardNotice_ASoftNoticeIgnoresADeadlineItWasHanded(t *testing.T) {
	const (
		deadline = 1787000000.0
		where    = "context 30% (your limits: 55% / 65%)"
		doc      = "§1 …"
	)
	s := newReconcileTestServer(t)

	got := s.winddownNoticeText(offboardKindSoft, deadline)
	// 〈停止〉 has no read-only head since T-6f44, so the soft notice IS the
	// document — which makes "a deadline handed to the soft arm changes nothing"
	// structural rather than conditional: there is no slot for it to land in.
	want := mustFoldText(t, s, s.offboardSpec())
	if got != want {
		t.Fatalf("soft notice handed a deadline:\n got %q\nwant %q", got, want)
	}
	// Positive control on the same deadline: the clause is not simply
	// unreachable — ask for the final call and the whole sentence becomes the
	// other document, deadline and all.
	_, finalBody, _ := DocSplitHeadBody(mustFoldText(t, s, s.acceleratedStopSpec()))
	gotFinal := s.winddownNoticeText(offboardKindFinal, deadline)
	wantFinal := "你的結束時刻是 " +
		time.Unix(int64(deadline), 0).UTC().Format(time.RFC3339) +
		"。\n" + finalBody
	if gotFinal != wantFinal {
		t.Fatalf("final call:\n got %q\nwant %q", gotFinal, wantFinal)
	}
}

// mustFoldText folds one document the way the send site does, so a test that
// needs only the owner-editable half reads the SAME bytes an agent is sent.
func mustFoldText(t *testing.T, s *apiServer, spec bootDocSpec) string {
	t.Helper()
	dto, err := s.foldBootDocDTO(spec)
	if err != nil || dto == nil {
		t.Fatalf("fold %s: %v", spec.Kind, err)
	}
	return dto.Text
}
