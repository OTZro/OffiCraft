// api/docCap.ts — the frontend's ONE copy of the server's document size cap.
//
// ⚠️ THE AUTHORITY FOR THIS RULE IS THE SERVER, NOT THIS FILE.
// `DocCapBlocked` + `contextDocMaxChars` in server/ocserverd/domain.go decide
// whether a write is refused; this module exists only so the cockpit can MARK a
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
// the shape of the DTO: global_context and role_definition are NOT capped at
// all (their restore calls DocCapBlocked nowhere), lessons caps `text`, and
// task_manual caps `learnings` AND `sop_md` — either one over the cap refuses
// the whole restore.

import type { DocumentKind } from "../types";

/** contextDocMaxChars (server/ocserverd/domain.go). The ONE constant — do not
 * inline this number anywhere else. */
export const DOC_CAP_CHARS = 10000;

/** Length in UNICODE CODE POINTS — the unit the server measures in
 * (utf8.RuneCountInString). `String.length` is UTF-16 units and would count an
 * astral character (emoji) TWICE; a byte count would count CJK prose 3× and
 * turn the owner's 10,000-character cap into a ~4,000-character one. Both are
 * the exact mistakes the fixture's multi-byte rows exist to catch. */
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
export function docCapBlocked(before: string, after: string): boolean {
  const n = runeLength(after);
  if (n <= DOC_CAP_CHARS) return false;
  return n >= runeLength(before);
}

/** The wire field names each kind's restore runs the cap over. Empty = this
 * kind's restore is never refused on size (global_context / role_definition). */
export const CAPPED_FIELDS: Record<DocumentKind, readonly string[]> = {
  global_context: [],
  role_definition: [],
  lessons: ["text"],
  task_manual: ["learnings", "sop_md"],
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
 */
export function docCapBlockedFields(
  kind: DocumentKind,
  content: Record<string, string>,
  current: Record<string, string> | undefined
): string[] {
  if (!current) return [];
  return CAPPED_FIELDS[kind].filter(
    (field) =>
      current[field] !== undefined &&
      docCapBlocked(current[field], content[field] ?? "")
  );
}
