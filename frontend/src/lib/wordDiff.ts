// lib/wordDiff.ts — INTRA-LINE highlighting for a line diff (T-40f0, owner
// 2026-07-31 item ④: 「同一列只改幾個字時，把改到的字標亮」).
//
// WHY A SEPARATE FILE, and why it does not touch lib/lineDiff.ts:
// lineDiff answers "which LINES differ", is the authority for that, and has its
// own test file. This module answers a strictly narrower, presentation-only
// question the shared diff surface asks AFTER lineDiff has already spoken:
// given one `-` line and the one `+` line it was replaced by, which CHARACTERS
// moved. Nothing here can change which rows exist, their order, or their line
// numbers — remove this module and the diff still renders, just without the
// character-level tint.
//
// TOKENS, not characters: a per-character diff on Chinese prose lights up
// almost every glyph (the LCS wanders through incidental 的/了 matches), which
// is noise wearing the costume of precision. The tokeniser therefore keeps
// - latin/digit RUNS whole (renaming `foo` to `foobar` marks one word, not
//   three trailing letters),
// - whitespace runs whole,
// - and every other character — CJK included — on its own, because Chinese has
//   no spaces and per-character IS the word granularity available.

import type { DiffRow } from "./lineDiff";

/** One run of a line, flagged with whether it is part of the change. */
export interface WordSegment {
  text: string;
  changed: boolean;
}

export interface WordDiff {
  /** The `-` line, segmented. */
  before: WordSegment[];
  /** The `+` line, segmented. */
  after: WordSegment[];
  /**
   * FALSE when the pair was deliberately left unhighlighted, in which case both
   * sides are a single unchanged segment. Two cases, both of them honest:
   * the lines share nothing (highlighting every token repeats what the row's
   * own colour already says, at the cost of drowning the cases where a few
   * characters really did move), or the pair is too long to diff at token level.
   */
  highlighted: boolean;
}

/**
 * Token budget per side. The token LCS is O(n·m) cells, and this runs for every
 * changed row PAIR on screen, so a pathological single line (a minified blob, a
 * base64 payload) must not be allowed to freeze the tab. Past the budget the
 * row keeps its whole-row colour and gains no character tint — a missing tint
 * is a smaller lie than a hung surface.
 */
const MAX_TOKENS = 400;

/** Split into diffable tokens: latin/digit runs, whitespace runs, else one
 * character each (CJK, punctuation, emoji — `[\s\S]` with the `u` flag keeps
 * surrogate pairs together). */
export function segmentTokens(text: string): string[] {
  return text.match(/[A-Za-z0-9]+|\s+|[\s\S]/gu) ?? [];
}

/** Suffix-indexed LCS lengths over tokens — same classic table lineDiff uses on
 * lines, kept here so that module stays the single authority for LINE structure
 * and this one owns nothing but the character tint. */
function lcs(a: string[], b: string[]): Uint32Array {
  const width = b.length + 1;
  const table = new Uint32Array((a.length + 1) * width);
  for (let i = a.length - 1; i >= 0; i--) {
    for (let j = b.length - 1; j >= 0; j--) {
      table[i * width + j] =
        a[i] === b[j]
          ? table[(i + 1) * width + (j + 1)] + 1
          : Math.max(table[(i + 1) * width + j], table[i * width + (j + 1)]);
    }
  }
  return table;
}

/** Adjacent segments of the same flag merge, so the DOM carries one span per
 * visible run rather than one per token. */
function pack(parts: WordSegment[]): WordSegment[] {
  const out: WordSegment[] = [];
  for (const part of parts) {
    if (part.text === "") continue;
    const last = out[out.length - 1];
    if (last && last.changed === part.changed) last.text += part.text;
    else out.push({ ...part });
  }
  return out;
}

function whole(before: string, after: string): WordDiff {
  return {
    before: before === "" ? [] : [{ text: before, changed: false }],
    after: after === "" ? [] : [{ text: after, changed: false }],
    highlighted: false,
  };
}

/**
 * Diff two lines at token level. Returns both sides segmented, or an
 * unhighlighted pair (see `WordDiff.highlighted`).
 */
export function diffWords(before: string, after: string): WordDiff {
  const a = segmentTokens(before);
  const b = segmentTokens(after);
  if (a.length > MAX_TOKENS || b.length > MAX_TOKENS) return whole(before, after);

  const table = lcs(a, b);
  const width = b.length + 1;
  const left: WordSegment[] = [];
  const right: WordSegment[] = [];
  let common = 0;
  let i = 0;
  let j = 0;
  while (i < a.length || j < b.length) {
    if (i < a.length && j < b.length && a[i] === b[j]) {
      left.push({ text: a[i], changed: false });
      right.push({ text: b[j], changed: false });
      // Whitespace matching either side is not evidence that the two lines are
      // versions of one another — "  " and "  " match in every pair of lines
      // ever written.
      if (a[i].trim() !== "") common++;
      i++;
      j++;
    } else if (
      i < a.length &&
      (j === b.length || table[(i + 1) * width + j] >= table[i * width + (j + 1)])
    ) {
      left.push({ text: a[i], changed: true });
      i++;
    } else {
      right.push({ text: b[j], changed: true });
      j++;
    }
  }

  // Nothing in common ⇒ this is a replacement, not an edit. The row colour
  // already says so; tinting every token on top of it would make the rows where
  // only a few characters moved indistinguishable from the rows where
  // everything did — and that distinction is the entire feature.
  if (common === 0) return whole(before, after);

  return { before: pack(left), after: pack(right), highlighted: true };
}

/**
 * Which `-` row each `+` row replaced, by index into `rows`.
 *
 * Pairing is POSITIONAL inside one contiguous change block (a run of removed
 * rows immediately followed by a run of added rows — the shape lineDiff emits
 * for a replacement, because it breaks ties towards the removal). The i-th
 * removed row pairs with the i-th added row; a block with unequal sides leaves
 * its surplus rows unpaired, and an unpaired row is a whole line that was only
 * deleted or only inserted, which has nothing to compare against.
 *
 * The map is keyed by the ADDED row index so both directions are one lookup:
 * `paired.get(addedIndex)` and its inverse built by the caller if needed.
 */
export function pairChangedRows(rows: DiffRow[]): Map<number, number> {
  const pairs = new Map<number, number>();
  let at = 0;
  while (at < rows.length) {
    if (rows[at].kind !== "removed") {
      at++;
      continue;
    }
    const removed: number[] = [];
    while (at < rows.length && rows[at].kind === "removed") removed.push(at++);
    const added: number[] = [];
    while (at < rows.length && rows[at].kind === "added") added.push(at++);
    for (let k = 0; k < Math.min(removed.length, added.length); k++) {
      pairs.set(added[k], removed[k]);
    }
  }
  return pairs;
}
