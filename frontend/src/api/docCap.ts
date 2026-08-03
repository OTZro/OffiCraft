// api/docCap.ts — the frontend's ONE copy of the server's document size cap.
//
// ⚠️ THE AUTHORITY FOR THIS RULE IS THE SERVER, NOT THIS FILE.
// `DocCapBlocked` in server/ocserverd/domain.go decides whether a write is
// refused, against the live `doc.cap_chars.*` setting for that document
// (T-3aeb made it a setting, T-ae38 split it four ways — the cap is no
// longer a constant on either side, so this module takes it as a parameter); this module exists only so the cockpit can MARK a
// revision as un-restorable BEFORE the owner clicks, instead of letting them
// click and collect an HTTP 400 that reads like a broken system.
//
// It is a TEMPORARY STAND-IN. The right shape is the server returning a
// per-revision `restorable` + `reason` on DocumentHistoryDTO, at which point
// this whole module is deleted and the card reads the flag. That is a wire
// change (spec/openapi.json is frozen — see root CLAUDE.md §13) and is
// currently blocked on owner approval, so until then two implementations of one
// rule exist and are pinned against a SHARED FIXTURE, not against each other:
//   bin/tests/fixtures/doc-cap-cases.tsv   — the table (the shared truth)
//   src/api/docCap.test.ts                 — this side reads it
//   server/ocserverd/doc_cap_mirror_test.go — the other side reads it
// A drift on either side reddens that side's test and names the row.
//
// Guessing was not an option for WHICH FIELD each kind caps, so it is
// transcribed from restoreDocumentHistory (api_document_history.go), not from
// the shape of the DTO: global_context is NOT capped at all (its restore calls
// DocCapBlocked nowhere), lessons and insight cap `text`, role_definition caps
// `definition_md` (T-ae38 — it was uncapped on BOTH doors until then, so
// restoring an old long revision was the way around the edit door), and
// task_manual caps `learnings` AND `sop_md` — either one over the cap refuses
// the whole restore.
//
// T-ae38 also made the cap PER SEGMENT: which of the four numbers applies is a
// property of the kind, transcribed here from the same switch. Judging a Duty
// revision against the Learning cap would mark a 4,000-char role definition as
// restorable when the server refuses it at 1,000.

import type { DocumentKind } from "../types";

/** The four SHIPPED DEFAULTS (server/ocserverd/domain.go:
 * dutyCapCharsDefault + contextDocMaxCharsDefault) — defaults, NOT the caps
 * themselves. Since T-3aeb the live values are the `doc.cap_chars.*` settings,
 * so callers pass them in; these exist only as the fallback for a caller with
 * no server value yet, and as the shared fixture's anchor. Do not inline these
 * numbers anywhere else. */
export interface DocCaps {
  duty: number;
  insight: number;
  learning: number;
  manual: number;
}

export const DOC_CAP_CHARS_DEFAULTS: DocCaps = {
  duty: 1000,
  insight: 15000,
  learning: 15000,
  manual: 15000,
};

/** The single number the shared fixture (bin/tests/fixtures/doc-cap-cases.tsv)
 * anchors its rows to. That table tests the PREDICATE, which takes the cap as a
 * parameter and is unchanged by the four-way split, so it keeps one anchor. */
export const DOC_CAP_CHARS_DEFAULT = DOC_CAP_CHARS_DEFAULTS.learning;

/** Length in UNICODE CODE POINTS — the unit the server measures in
 * (utf8.RuneCountInString). `String.length` is UTF-16 units and would count an
 * astral character (emoji) TWICE; a byte count would count CJK prose 3× and
 * would shrink the owner's cap to roughly a third of the number he signed off
 * on. Both are the exact mistakes the fixture's multi-byte rows exist to
 * catch. */
export function runeLength(s: string): number {
  return [...s].length;
}

/**
 * Mirrors DocCapBlocked: replacing `before` with `after` is refused when the
 * proposal is over the cap AND is not getting shorter. The three branches,
 * boundaries included:
 *   - after ≤ cap                    → allowed (the ordinary case);
 *   - after > cap AND after < before → allowed (an over-cap doc may converge
 *     downward — the escape hatch that keeps existing over-cap docs editable);
 *   - after > cap AND after ≥ before → REFUSED, EQUAL LENGTH INCLUDED.
 */
export function docCapBlocked(
  cap: number,
  before: string,
  after: string
): boolean {
  const n = runeLength(after);
  if (n <= cap) return false;
  return n >= runeLength(before);
}

/** The wire field names each kind's restore runs the cap over. Empty = this
 * kind's restore is never refused on size (global_context / task_description). */
export const CAPPED_FIELDS: Record<DocumentKind, readonly string[]> = {
  global_context: [],
  // T-ae38: Duty is capped now, on BOTH doors. It was capped on neither, and
  // one door alone would have been decorative — edit the definition down to
  // 999 and restore a 4,000-char earlier revision and the cap is gone.
  role_definition: ["definition_md"],
  lessons: ["text"],
  // T-3809: insight's restore runs the cap over `text` too, and deliberately —
  // an older, larger revision is still a write, so letting history walk the doc
  // back over the limit would make the cap a suggestion
  // (api_document_history.go, case "insight").
  insight: ["text"],
  // The retired bundle has no restore path left at all (both routes 400 since
  // T-1f39); its entry is kept only because this table is total over
  // DocumentKind. The two split kinds each write back exactly ONE field, and
  // restoreTaskManualField judges the cap on that field alone — an over-cap
  // learnings doc no longer blocks a SOP restore.
  task_manual: ["learnings", "sop_md"],
  task_manual_sop: ["sop_md"],
  task_manual_learnings: ["learnings"],
  // EMPTY on purpose (T-e271): the description has never had a length cap on
  // the create side either, so the server runs no cap on this restore. A cap
  // listed here would mark revisions as unrestorable that the server would
  // happily accept — the cockpit inventing a refusal is worse than not having
  // one, because there is no way for the owner to tell it is imaginary.
  task_description: [],
};

/**
 * Which of a revision's fields would make the server refuse this restore.
 *
 * `current` holds the LIVE document's values under the SAME wire field names.
 * Pass `undefined` (or omit a field) while the live doc has not loaded: the
 * verdict then abstains rather than judging the revision against an empty
 * string, which would mark a perfectly restorable revision as blocked. An
 * abstention is the honest degraded state — the owner can still click, and the
 * server's own 400 surfaces in the dialog exactly as it did before.
 *
 * `cap` abstains the same way and for the same reason (T-3aeb). Falling back to
 * the shipped default while the setting is still loading would be WRONG in the
 * one direction that matters: the cap can only ever be RAISED, so judging by
 * the default can only ever grey out a revision the server would have accepted
 * — the "greyed out with a reason that is not true" failure this module's
 * header calls the worse of the two.
 */
export function docCapBlockedFields(
  kind: DocumentKind,
  content: Record<string, string>,
  current: Record<string, string> | undefined,
  caps: DocCaps | undefined
): string[] {
  const cap = caps && capForKind(kind, caps);
  if (!current || cap === undefined) return [];
  return CAPPED_FIELDS[kind].filter(
    (field) =>
      current[field] !== undefined &&
      docCapBlocked(cap, current[field], content[field] ?? "")
  );
}

/** WHICH of the four caps judges this kind — transcribed from the same switch
 * in restoreDocumentHistory as CAPPED_FIELDS. `undefined` for the kinds that
 * are never refused on size, so a caller cannot accidentally judge them by
 * whichever number happened to be nearest.
 *
 * The task manual's two documents answer to `manual`, NOT to any of the three
 * role-journal segments: they are keyed by type_key, so they are assets of a
 * task TYPE rather than entries in a role's journal. */
export function capForKind(
  kind: DocumentKind,
  caps: DocCaps
): number | undefined {
  switch (kind) {
    case "role_definition":
      return caps.duty;
    case "insight":
      return caps.insight;
    case "lessons":
      return caps.learning;
    case "task_manual":
    case "task_manual_sop":
    case "task_manual_learnings":
      return caps.manual;
    case "global_context":
    case "task_description":
      return undefined;
  }
}
