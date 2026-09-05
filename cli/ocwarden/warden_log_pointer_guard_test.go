package main

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// ── ② the guard that makes ① mean something ──────────────────────────────────
//
// THE PROBLEM WITH THE E2E AS IT STOOD. Three greps looked for the warden's
// "command reader: enabled (SSE …)" line and passed BOTH log files to grep:
//
//	e2e_test/cross_machine.sh:465-466
//	e2e_test/cross_machine.sh:586
//	e2e_test/lib/oc_lifecycle.sh:1296-1297
//
// grep with two file arguments succeeds if the pattern is in EITHER, so the e2e
// could never tell the two files apart. That is not a weak guard, it is a guard
// with exactly zero discrimination on the only question that matters here: which
// file does the warden actually write its operational lines to? Fixing the four
// product-code pointers while leaving those greps alone would ship a claim that
// no test on the tree can falsify — and the next person to "helpfully" restore
// err.log would meet a fully green suite.
//
// IS NARROWING SAFE, OR DOES IT MANUFACTURE A FALSE RED? Read, not assumed:
//
//   - Every "command reader: …" string in the tree is emitted through `logf` /
//     `t.logf` (transport.go:218,220,295,341,346,357,366,371,377 and
//     main.go:947). There is no other emitter — the assertion below re-derives
//     that from the sources rather than trusting this comment.
//   - `logf` is defined at main.go:879 as a closure over the `out io.Writer`
//     parameter of realMain, and main() passes os.Stdout (main.go:971).
//   - The launchd plist template (install.go:489) maps
//     StandardOutPath → ocwarden.out.log, StandardErrorPath → ocwarden.err.log.
//
// So there is no configuration, namespace, or bootstrap mode in which a
// "command reader:" line can reach err.log: the only two direct os.Stderr
// writers left in this package are the `[ocwarden spawn]` env diagnostics
// (transport.go:594) and the trash refusals (trash.go:222), neither of which
// emits that prefix. Narrowing the e2e greps to out.log is therefore a
// TIGHTENING of a true invariant, not a new risk — and if someone ever routes
// logf to stderr, the derivation assertion below is what goes red first.
//
// MUTANT (the acceptance for ②): put "$…/ocwarden.err.log" back into any one of
// the three e2e greps → TestE2E_CommandReaderGrepTargetsOnlyTheStdoutLog goes
// RED. That is the property the whole item is judged on.

const (
	outLogName = "ocwarden.out.log"
	errLogName = "ocwarden.err.log"
)

// e2eCommandReaderGrepSites are the shell greps that hunt for the warden's SSE
// line. Paths are relative to this package dir (cli/ocwarden), matching the
// existing repo-file test precedent in canon_port_test.go.
var e2eCommandReaderGrepSites = []string{
	"../../e2e_test/cross_machine.sh",
	"../../e2e_test/lib/oc_lifecycle.sh",
}

// commandReaderGrepLine matches a shell line that both mentions the SSE probe
// pattern and/or names a warden log file as a grep operand. We scan the whole
// grep STATEMENT (which spans continuation lines), so the unit is a statement,
// not a line.
var commandReaderPattern = regexp.MustCompile(`command reader: enabled`)

// readRepoFile fails loudly rather than skipping: a guard that quietly does
// nothing when it cannot find its subject is worse than no guard, because the
// suite still reports green.
func readRepoFile(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("FATAL: could not read %s — refusing to run without a working "+
			"log-pointer guard: %v", path, err)
	}
	return string(raw)
}

// grepStatementsMentioningCommandReader splits src into shell statements
// (honouring `\` line continuations) and returns the ones that actually GREP for
// the warden's command-reader line.
//
// The `grep` requirement is not decoration. Both scripts also carry the same
// string in a comment and in a fail_stage message; those mention the line
// without reading any file, and judging them would produce noise that trains the
// next reader to ignore this guard. Only a statement that runs grep can be
// pointed at the wrong file, so only a statement that runs grep is judged.
func grepStatementsMentioningCommandReader(src string) []string {
	var stmts []string
	var cur strings.Builder
	flush := func() {
		s := cur.String()
		cur.Reset()
		trimmed := strings.TrimSpace(s)
		if strings.HasPrefix(trimmed, "#") {
			return // a comment, not a probe
		}
		if commandReaderPattern.MatchString(s) && strings.Contains(s, "grep") {
			stmts = append(stmts, s)
		}
	}
	for _, line := range strings.Split(src, "\n") {
		cur.WriteString(line)
		if strings.HasSuffix(strings.TrimRight(line, " \t"), `\`) {
			cur.WriteString(" ")
			continue
		}
		flush()
	}
	flush()
	return stmts
}

// TestE2E_CommandReaderGrepTargetsOnlyTheStdoutLog is the discrimination guard.
// Every e2e probe for the warden's SSE line must read ocwarden.out.log and must
// NOT also be handed ocwarden.err.log — because being handed both is what made
// the probe unable to answer the question it appears to be asking.
func TestE2E_CommandReaderGrepTargetsOnlyTheStdoutLog(t *testing.T) {
	total := 0
	for _, path := range e2eCommandReaderGrepSites {
		src := readRepoFile(t, path)
		stmts := grepStatementsMentioningCommandReader(src)
		if len(stmts) == 0 {
			t.Errorf("FATAL: %s no longer contains any 'command reader: enabled' probe "+
				"— either the e2e stopped checking that the warden connects, or this "+
				"guard has gone stale and is silently guarding nothing", path)
			continue
		}
		for _, stmt := range stmts {
			total++
			if !strings.Contains(stmt, outLogName) {
				t.Errorf("%s: an SSE probe that does not read %s cannot see the line it "+
					"is looking for — every 'command reader:' string in cli/ocwarden is "+
					"written through logf → os.Stdout.\nstatement: %s",
					path, outLogName, strings.TrimSpace(stmt))
			}
			if strings.Contains(stmt, errLogName) {
				t.Errorf("%s: this probe is ALSO handed %s. grep over two files passes on "+
					"either, so the probe cannot distinguish the file that carries the "+
					"warden's operational lines from the one that carries only spawn "+
					"env-var names — which leaves the product-code log pointers "+
					"unguarded.\nstatement: %s",
					path, errLogName, strings.TrimSpace(stmt))
			}
		}
	}
	if total < 3 {
		t.Errorf("expected at least the 3 known SSE probes (cross_machine.sh x2, "+
			"oc_lifecycle.sh x1), found %d — the guard has lost sight of its subjects", total)
	}
}

// TestCommandReaderLinesAreWrittenToStdoutNotStderr re-derives, from the sources
// rather than from a comment, the fact that everything above rests on: the
// "command reader:" emitter is logf, logf writes to realMain's `out`, and main()
// passes os.Stdout. If someone re-points logf at stderr, this is the test that
// goes red — and it is the honest place to discover that narrowing the e2e greps
// has become wrong.
func TestCommandReaderLinesAreWrittenToStdoutNotStderr(t *testing.T) {
	// (1) No direct os.Stderr write in this package carries the command-reader
	// prefix. Any that did would be a second, invisible emitter.
	for _, path := range []string{"transport.go", "main.go", "trash.go", "spawn.go"} {
		src := readRepoFile(t, path)
		for _, line := range strings.Split(src, "\n") {
			code := line
			if i := strings.Index(line, "//"); i >= 0 {
				code = line[:i] // comments talk ABOUT stderr; only code writes to it
			}
			if strings.Contains(code, "os.Stderr") && strings.Contains(code, "command reader") {
				t.Errorf("%s: a 'command reader' line is written straight to os.Stderr — "+
					"it would land in %s, and the e2e narrowing in this file's sibling "+
					"test becomes a false red.\nline: %s", path, errLogName, strings.TrimSpace(line))
			}
		}
	}

	// (2) main() hands realMain os.Stdout as its `out` writer — the sink logf
	// closes over.
	mainSrc := readRepoFile(t, "main.go")
	if !regexp.MustCompile(`realMain\(os\.Args\[1:\],\s*os\.Getenv,\s*os\.Stdout\)`).MatchString(mainSrc) {
		t.Errorf("main.go no longer hands realMain os.Stdout — the whole premise that "+
			"warden operational lines land in %s needs re-deriving before the e2e "+
			"greps and the product-code pointers can keep naming it", outLogName)
	}

	// (3) logf writes to that `out`, not to a stderr handle.
	if !regexp.MustCompile(`logf := func\(format string, args \.\.\.any\) \{\s*\n\s*fmt\.Fprintf\(out,`).MatchString(mainSrc) {
		t.Errorf("main.go's logf no longer writes to realMain's `out` writer — re-derive "+
			"which file warden operational lines reach before trusting %s", outLogName)
	}

	// (4) The plist still maps stdout → out.log (and stderr → err.log).
	installSrc := readRepoFile(t, "install.go")
	if !strings.Contains(installSrc, "<key>StandardOutPath</key><string>%[6]s/"+outLogName+"</string>") {
		t.Errorf("install.go's plist no longer routes StandardOutPath to %s — the "+
			"receipts and the e2e greps both name it", outLogName)
	}
}

// TestCutoverDoc_DoesNotSendReadersToTheEmptyLog: cli/ocwarden/CUTOVER.md is the
// human runbook for this exact symptom, and it carried the same false sentence
// as the product code ("var/log/ocwarden.err.log shows command reader:"). Left
// alone it would keep teaching the wrong file by hand long after the code stopped.
func TestCutoverDoc_DoesNotSendReadersToTheEmptyLog(t *testing.T) {
	// SENTENCE-scoped, not paragraph- or window-scoped. CUTOVER.md is a numbered
	// runbook whose "paragraphs" run to hundreds of lines, and the claim at issue
	// is one sentence long: "`var/log/ocwarden.err.log` shows `command reader:
	// enabled (SSE …)`". Judging anything larger buries the sentence at fault in a
	// wall of unrelated prose.
	//
	// ⚠️ KNOWN LIMITS — MEASURED, NOT ESTIMATED. READ BEFORE RELYING ON THIS.
	//
	// The corrected doc must still be ABLE to name err.log — the most useful
	// sentence the runbook can carry is "it is out.log, NOT err.log" — so a flat
	// rule of "err.log must never appear near 'command reader'" would forbid the
	// correction itself. Hence the negation-aware heuristic below. It is weak, and
	// the following three bypasses were each confirmed by appending the sentence
	// to CUTOVER.md and watching this test stay GREEN — they are not hypotheticals:
	//
	//  1. THE NEGATOR LIST IS A LOOPHOLE, NOT A TEST OF MEANING. A false claim
	//     that happens to contain a negator word is waved through:
	//       "The command reader: line is written only to ocwarden.err.log, so
	//        tail that file."
	//     ("only" ⇒ exempt.)
	//
	//  2. SUBSTRING MATCHING MAKES IT WORSE THAN IT LOOKS. The negator "not "
	//     matches inside "cannot ", so a purely AFFIRMATIVE false claim is exempt:
	//       "You cannot see the command reader: handshake anywhere except
	//        ocwarden.err.log."
	//     Nothing about that sentence denies the association; it asserts it.
	//
	//  3. 🔴 THE BIGGEST HOLE: a sentence that never says the literal string
	//     "command reader" is skipped outright by the `continue` below — it is
	//     never judged at all. So a paraphrase of the ORIGINAL BUG passes:
	//       "To confirm the warden's SSE handshake landed, tail
	//        var/log/ocwarden.err.log and look for the enabled-SSE line."
	//     That is the same false instruction this whole change removed, worded
	//     differently, and this guard cannot see it.
	//
	// These holes are DELIBERATELY LEFT OPEN. Papering over them by bolting on
	// more match strings ("SSE handshake", "enabled-SSE", …) would tighten the
	// three sentences printed above while leaving the fourth phrasing — and every
	// phrasing after it — just as invisible, and would advertise a coverage the
	// rule does not have. Prose cannot be pinned by substring matching, and
	// pretending otherwise is the same species of confident-but-wrong claim the
	// commit this test ships with exists to delete.
	//
	// ⇒ Treat this as a DOCUMENTATION CANARY for the one exact sentence that was
	// wrong (which it does catch — mutant M7), NOT as a proof that CUTOVER.md is
	// free of log-pointer lies. The load-bearing guards are its two siblings in
	// this file, which read code, not prose.
	src := readRepoFile(t, "CUTOVER.md")
	// Collapse markdown's hard-wrapped lines so a sentence split across lines is
	// judged as one sentence (the original defect was wrapped mid-claim).
	flat := strings.Join(strings.Fields(strings.ReplaceAll(src, "\n", " ")), " ")
	negators := []string{"never", "not ", "only", "no `command reader"}
	for _, sentence := range strings.Split(flat, ". ") {
		if !strings.Contains(sentence, errLogName) || !strings.Contains(sentence, "command reader") {
			continue
		}
		denied := false
		for _, n := range negators {
			if strings.Contains(strings.ToLower(sentence), n) {
				denied = true
				break
			}
		}
		if denied {
			continue
		}
		t.Errorf("CUTOVER.md associates %s with the 'command reader:' line without "+
			"denying it. That stream is stdout (logf → realMain's `out` → os.Stdout "+
			"→ the plist's StandardOutPath → %s); err.log has never carried the "+
			"line.\nsentence: %s", errLogName, outLogName, strings.TrimSpace(sentence))
	}
}
