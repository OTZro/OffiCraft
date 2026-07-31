// lib/wordDiff — the INTRA-LINE half of the diff surface (T-40f0 ④).
//
// What is pinned:
//   1. the tokeniser's granularity, because it is what decides whether Chinese
//      prose lights up usefully or lights up everywhere;
//   2. the segments are lossless — reassembling them gives the line back
//      character for character, so a tint can never eat text;
//   3. the two DELIBERATE non-highlight cases (nothing in common; too long),
//      each of which is a design decision rather than a gap;
//   4. the row pairing, including the shapes that must NOT pair.

import { describe, it, expect } from "vitest";
import { diffWords, pairChangedRows, segmentTokens } from "./wordDiff";
import type { DiffRow } from "./lineDiff";

const rejoin = (segs: { text: string }[]) => segs.map((s) => s.text).join("");
const marked = (segs: { text: string; changed: boolean }[]) =>
  segs.filter((s) => s.changed).map((s) => s.text);

describe("segmentTokens", () => {
  it("keeps latin/digit runs whole and splits CJK per character", () => {
    // Latin whole: renaming foo→foobar must mark one word, not three letters.
    expect(segmentTokens("foo bar42")).toEqual(["foo", " ", "bar42"]);
    // CJK per character: Chinese has no spaces, so per-character IS the word
    // granularity available.
    expect(segmentTokens("講功能")).toEqual(["講", "功", "能"]);
    // Punctuation is its own token, so "、" can be marked on its own.
    expect(segmentTokens("a，b")).toEqual(["a", "，", "b"]);
  });

  it("keeps a whitespace run as one token and an empty line as no tokens", () => {
    expect(segmentTokens("a   b")).toEqual(["a", "   ", "b"]);
    expect(segmentTokens("")).toEqual([]);
  });

  it("does not split a surrogate pair", () => {
    // A per-code-unit split would produce two lone surrogates and render as
    // replacement characters — visible corruption inside a diff.
    expect(segmentTokens("a🙂b")).toEqual(["a", "🙂", "b"]);
  });
});

describe("diffWords", () => {
  it("marks the inserted run on the after side and nothing on the before side", () => {
    const d = diffWords(
      "講功能與取捨，不倒實作細節",
      "講功能與影響、取捨，不倒實作細節"
    );
    expect(d.highlighted).toBe(true);
    expect(marked(d.after)).toEqual(["影響、"]);
    expect(marked(d.before)).toEqual([]);
  });

  it("marks the deleted run on the before side when text was removed", () => {
    const d = diffWords("純回報也開卡，讓他知道進度", "純回報也開卡");
    expect(d.highlighted).toBe(true);
    expect(marked(d.before)).toEqual(["，讓他知道進度"]);
    expect(marked(d.after)).toEqual([]);
  });

  it("is lossless: the segments rejoin into the original lines", () => {
    const before = "  indented foo(bar) — 中文標點，與空白 ";
    const after = "  indented foo(baz) — 中文標點；與空白 ";
    const d = diffWords(before, after);
    expect(rejoin(d.before)).toBe(before);
    expect(rejoin(d.after)).toBe(after);
  });

  it("merges adjacent same-flag tokens into one segment", () => {
    // One span per visible run, not one per token — otherwise a 40-character
    // Chinese edit becomes 40 DOM nodes each with its own rounded corners.
    const d = diffWords("abc", "abc 加上一段中文");
    expect(d.after.filter((s) => s.changed)).toHaveLength(1);
  });

  it("declines to highlight two lines that share nothing", () => {
    // The row colour already says "this whole line changed". Marking every
    // token would make it indistinguishable from a few-character edit, and
    // telling those apart is the entire feature.
    const d = diffWords("舊的第二行", "BRAVO");
    expect(d.highlighted).toBe(false);
    expect(marked(d.before)).toEqual([]);
    expect(marked(d.after)).toEqual([]);
    expect(rejoin(d.before)).toBe("舊的第二行");
  });

  it("does not count shared WHITESPACE as evidence the lines are related", () => {
    // "  " matches "  " in every pair of lines ever written; treating that as
    // common ground would highlight every token of two unrelated indented
    // lines.
    const d = diffWords("  舊的第二行", "  BRAVO");
    expect(d.highlighted).toBe(false);
  });

  it("declines to highlight a pair past the token budget, losslessly", () => {
    // The token LCS is O(n·m) per changed row pair; a minified blob must cost
    // a missing tint, not a hung tab.
    const long = "x ".repeat(500);
    const d = diffWords(long, long + "y");
    expect(d.highlighted).toBe(false);
    expect(rejoin(d.after)).toBe(long + "y");
  });
});

describe("pairChangedRows", () => {
  const row = (
    kind: DiffRow["kind"],
    text: string,
    beforeLine: number | null,
    afterLine: number | null
  ): DiffRow => ({ kind, text, beforeLine, afterLine });

  it("pairs the i-th removed row with the i-th added row of one change block", () => {
    const rows = [
      row("context", "a", 1, 1),
      row("removed", "old1", 2, null),
      row("removed", "old2", 3, null),
      row("added", "new1", null, 2),
      row("added", "new2", null, 3),
      row("context", "z", 4, 4),
    ];
    expect([...pairChangedRows(rows)]).toEqual([
      [3, 1],
      [4, 2],
    ]);
  });

  it("leaves the surplus side of an uneven block unpaired", () => {
    const rows = [
      row("removed", "old", 1, null),
      row("added", "new1", null, 1),
      row("added", "new2", null, 2),
    ];
    // The extra insertion is a whole new line — it has nothing to compare
    // against inside itself.
    expect([...pairChangedRows(rows)]).toEqual([[1, 0]]);
  });

  it("does not pair across a context row that separates two blocks", () => {
    const rows = [
      row("removed", "old", 1, null),
      row("context", "mid", 2, 1),
      row("added", "new", null, 2),
    ];
    // A deletion here and an insertion three lines later are not one edit; the
    // shared context line between them is the proof.
    expect([...pairChangedRows(rows)]).toEqual([]);
  });

  it("does not pair an insertion that comes BEFORE the deletion", () => {
    // lineDiff breaks ties towards the removal, so a replacement is always
    // `-` then `+`. An added-then-removed run is two separate edits.
    const rows = [
      row("added", "new", null, 1),
      row("removed", "old", 1, null),
    ];
    expect([...pairChangedRows(rows)]).toEqual([]);
  });

  it("pairs nothing in an all-context diff", () => {
    expect([...pairChangedRows([row("context", "a", 1, 1)])]).toEqual([]);
  });
});
