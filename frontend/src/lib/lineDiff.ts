// lib/lineDiff.ts — a hand-rolled, dependency-free line-level diff producing a
// git-style unified model (context / added / removed rows carrying the 1-based
// line number on each side).
//
// Hand-rolled on purpose, like components/Markdown.tsx: the repo ships no diff
// library, and the doc-history view needs exactly one thing — "which lines of
// this revision differ from the previous one" — which a classic LCS fits in
// under 200 lines with no supply-chain surface and no bundle cost.

export type DiffRowKind = "context" | "added" | "removed";

export interface DiffRow {
  kind: DiffRowKind;
  text: string;
  /** 1-based line number on the `before` side, null for an added line. */
  beforeLine: number | null;
  /** 1-based line number on the `after` side, null for a removed line. */
  afterLine: number | null;
}

/** One contiguous block of rows the UI should render, git `@@` style. */
export interface DiffHunk {
  rows: DiffRow[];
  /** Unchanged lines collapsed away immediately before this hunk — the number
   * the `@@`-style separator reports. 0 means the hunk starts at the top. */
  skippedBefore: number;
  /** 1-based first/last-inclusive extents, 0 when the hunk touches neither
   * side (only possible for an empty hunk, which is never emitted). */
  beforeStart: number;
  beforeCount: number;
  afterStart: number;
  afterCount: number;
}

export interface LineDiffResult {
  /** "too-large" means nothing was diffed — see `maxLines`. */
  status: "diffed" | "too-large";
  /** The full ordered edit script. Empty when status is "too-large". */
  rows: DiffRow[];
  /** `rows` grouped for display. With collapsing off this is a single hunk
   * holding every row; with it on, only changes plus their context radius. An
   * identical pair yields no hunks at all. */
  hunks: DiffHunk[];
  beforeLineCount: number;
  afterLineCount: number;
}

export interface LineDiffOptions {
  /** Drop unchanged runs further than `contextRadius` from any change. */
  collapseUnchanged?: boolean;
  contextRadius?: number;
  /** Per-side line ceiling above which the diff is refused. */
  maxLines?: number;
}

const DEFAULT_CONTEXT_RADIUS = 3;

// The LCS table is O(n*m) cells; at 2000 lines per side that is 4M Uint32 cells
// (~16MB, tens of ms), which is already far past any document a human reads in
// this UI. Doubling the limit quadruples both, so the ceiling is deliberately
// low and the caller is told to fall back rather than freeze the tab.
const DEFAULT_MAX_LINES = 2000;

/**
 * Split into lines losslessly. `\r\n` and `\n` both terminate a line, and a
 * trailing terminator does NOT produce a final empty line — but only one such
 * segment is dropped, so a genuinely blank last line ("a\n\n") survives.
 */
export function splitLines(text: string): string[] {
  if (text === "") return [];
  const lines = text.split(/\r\n|\n/);
  if (lines[lines.length - 1] === "") lines.pop();
  return lines;
}

/** Longest-common-subsequence lengths, suffix-indexed: cell (i,j) is the LCS of
 * a[i..] and b[j..]. Suffix form lets the backtrack walk forward, which is the
 * order the rows are emitted in. */
function lcsLengths(a: string[], b: string[]): Uint32Array {
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

function buildRows(a: string[], b: string[]): DiffRow[] {
  const table = lcsLengths(a, b);
  const width = b.length + 1;
  const rows: DiffRow[] = [];
  let i = 0;
  let j = 0;
  while (i < a.length || j < b.length) {
    if (i < a.length && j < b.length && a[i] === b[j]) {
      rows.push({ kind: "context", text: a[i], beforeLine: i + 1, afterLine: j + 1 });
      i++;
      j++;
    } else if (
      i < a.length &&
      (j === b.length || table[(i + 1) * width + j] >= table[i * width + (j + 1)])
    ) {
      // Ties go to the removal so a replaced line reads "-old" then "+new".
      rows.push({ kind: "removed", text: a[i], beforeLine: i + 1, afterLine: null });
      i++;
    } else {
      rows.push({ kind: "added", text: b[j], beforeLine: null, afterLine: j + 1 });
      j++;
    }
  }
  return rows;
}

function makeHunk(rows: DiffRow[], skippedBefore: number): DiffHunk {
  let beforeStart = 0;
  let beforeCount = 0;
  let afterStart = 0;
  let afterCount = 0;
  for (const row of rows) {
    if (row.beforeLine !== null) {
      if (beforeStart === 0) beforeStart = row.beforeLine;
      beforeCount++;
    }
    if (row.afterLine !== null) {
      if (afterStart === 0) afterStart = row.afterLine;
      afterCount++;
    }
  }
  return { rows, skippedBefore, beforeStart, beforeCount, afterStart, afterCount };
}

function collapse(rows: DiffRow[], contextRadius: number): DiffHunk[] {
  const changed = rows.map((row) => row.kind !== "context");
  if (!changed.includes(true)) return [];

  const hunks: DiffHunk[] = [];
  let cursor = 0; // first row not yet emitted or skipped
  let index = 0;
  while (index < rows.length) {
    if (!changed[index]) {
      index++;
      continue;
    }
    const start = Math.max(cursor, index - contextRadius);
    let end = index; // inclusive
    // Absorb the next change too when its leading context would touch this one.
    for (let k = index + 1; k < rows.length; k++) {
      if (changed[k]) {
        end = k;
      } else if (k - end > contextRadius * 2) {
        break;
      }
    }
    end = Math.min(rows.length - 1, end + contextRadius);
    hunks.push(makeHunk(rows.slice(start, end + 1), start - cursor));
    cursor = end + 1;
    index = end + 1;
  }
  return hunks;
}

/** Diff `before` against `after`, line by line. */
export function diffLines(
  before: string,
  after: string,
  options: LineDiffOptions = {}
): LineDiffResult {
  const {
    collapseUnchanged = true,
    contextRadius = DEFAULT_CONTEXT_RADIUS,
    maxLines = DEFAULT_MAX_LINES,
  } = options;

  const a = splitLines(before);
  const b = splitLines(after);

  if (a.length > maxLines || b.length > maxLines) {
    return {
      status: "too-large",
      rows: [],
      hunks: [],
      beforeLineCount: a.length,
      afterLineCount: b.length,
    };
  }

  const rows = buildRows(a, b);
  const hunks = collapseUnchanged
    ? collapse(rows, Math.max(0, contextRadius))
    : rows.length > 0
      ? [makeHunk(rows, 0)]
      : [];

  return {
    status: "diffed",
    rows,
    hunks,
    beforeLineCount: a.length,
    afterLineCount: b.length,
  };
}
