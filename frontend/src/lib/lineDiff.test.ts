import { describe, it, expect } from "vitest";
import { diffLines, splitLines } from "./lineDiff";

describe("splitLines", () => {
  it("treats a trailing newline as a terminator, not a blank final line", () => {
    expect(splitLines("a\nb\n")).toEqual(["a", "b"]);
  });

  it("keeps a genuinely blank last line", () => {
    expect(splitLines("a\n\n")).toEqual(["a", ""]);
  });

  it("returns no lines for an empty document", () => {
    expect(splitLines("")).toEqual([]);
  });

  it("splits CRLF and LF alike, leaving no carriage returns behind", () => {
    expect(splitLines("a\r\nb\nc")).toEqual(["a", "b", "c"]);
  });
});

describe("diffLines", () => {
  it("reports every line as context and no hunks when the sides are identical", () => {
    const result = diffLines("a\nb\nc", "a\nb\nc");
    expect(result.status).toBe("diffed");
    expect(result.rows).toEqual([
      { kind: "context", text: "a", beforeLine: 1, afterLine: 1 },
      { kind: "context", text: "b", beforeLine: 2, afterLine: 2 },
      { kind: "context", text: "c", beforeLine: 3, afterLine: 3 },
    ]);
    expect(result.hunks).toEqual([]);
  });

  it("emits only the changed middle line as removed-then-added", () => {
    expect(diffLines("a\nb\nc", "a\nB\nc").rows).toEqual([
      { kind: "context", text: "a", beforeLine: 1, afterLine: 1 },
      { kind: "removed", text: "b", beforeLine: 2, afterLine: null },
      { kind: "added", text: "B", beforeLine: null, afterLine: 2 },
      { kind: "context", text: "c", beforeLine: 3, afterLine: 3 },
    ]);
  });

  it("keeps surrounding lines as context for a pure insertion", () => {
    expect(diffLines("a\nc", "a\nb\nc").rows).toEqual([
      { kind: "context", text: "a", beforeLine: 1, afterLine: 1 },
      { kind: "added", text: "b", beforeLine: null, afterLine: 2 },
      { kind: "context", text: "c", beforeLine: 2, afterLine: 3 },
    ]);
  });

  it("keeps surrounding lines as context for a pure deletion", () => {
    expect(diffLines("a\nb\nc", "a\nc").rows).toEqual([
      { kind: "context", text: "a", beforeLine: 1, afterLine: 1 },
      { kind: "removed", text: "b", beforeLine: 2, afterLine: null },
      { kind: "context", text: "c", beforeLine: 3, afterLine: 2 },
    ]);
  });

  it("reports an empty before side as pure addition", () => {
    const result = diffLines("", "x\ny");
    expect(result.beforeLineCount).toBe(0);
    expect(result.afterLineCount).toBe(2);
    expect(result.rows).toEqual([
      { kind: "added", text: "x", beforeLine: null, afterLine: 1 },
      { kind: "added", text: "y", beforeLine: null, afterLine: 2 },
    ]);
  });

  it("reports an emptied after side as pure removal", () => {
    expect(diffLines("x\ny", "").rows).toEqual([
      { kind: "removed", text: "x", beforeLine: 1, afterLine: null },
      { kind: "removed", text: "y", beforeLine: 2, afterLine: null },
    ]);
  });

  it("sees CRLF and LF text with the same content as unchanged", () => {
    const result = diffLines("a\r\nb\r\nc", "a\nb\nc");
    expect(result.rows.every((row) => row.kind === "context")).toBe(true);
    expect(result.hunks).toEqual([]);
  });

  it("sees a newly added trailing newline as unchanged, but a new blank line as added", () => {
    expect(diffLines("a\nb", "a\nb\n").hunks).toEqual([]);
    expect(diffLines("a\nb", "a\nb\n\n").rows).toEqual([
      { kind: "context", text: "a", beforeLine: 1, afterLine: 1 },
      { kind: "context", text: "b", beforeLine: 2, afterLine: 2 },
      { kind: "added", text: "", beforeLine: null, afterLine: 3 },
    ]);
  });

  it("collapses unchanged runs into one hunk bounded by the context radius", () => {
    const before = Array.from({ length: 20 }, (_, i) => `line${i + 1}`).join("\n");
    const after = before.replace("line10", "LINE10");
    const result = diffLines(before, after, { contextRadius: 2 });

    expect(result.hunks).toHaveLength(1);
    const hunk = result.hunks[0];
    expect(hunk.skippedBefore).toBe(7);
    expect(hunk.beforeStart).toBe(8);
    expect(hunk.beforeCount).toBe(5);
    expect(hunk.afterStart).toBe(8);
    expect(hunk.afterCount).toBe(5);
    expect(hunk.rows).toEqual([
      { kind: "context", text: "line8", beforeLine: 8, afterLine: 8 },
      { kind: "context", text: "line9", beforeLine: 9, afterLine: 9 },
      { kind: "removed", text: "line10", beforeLine: 10, afterLine: null },
      { kind: "added", text: "LINE10", beforeLine: null, afterLine: 10 },
      { kind: "context", text: "line11", beforeLine: 11, afterLine: 11 },
      { kind: "context", text: "line12", beforeLine: 12, afterLine: 12 },
    ]);
  });

  // The BOUNDARY of "distant" — the one line where merging flips to splitting.
  // A mutant sweep found this unguarded: widening the merge distance by one
  // left every other case in this file green, because they all sit far away
  // from the threshold. Two changes with exactly 2×radius unchanged lines
  // between them share a hunk (their context regions touch); one line more and
  // they cannot, so the reader gets two hunks and an explicit skipped count
  // instead of an unbroken block that hides how far apart they really were.
  it("merges two changes exactly 2×radius apart and splits them one line further", () => {
    const lines = (n: number) =>
      Array.from({ length: n }, (_, i) => `line${i + 1}`).join("\n");
    const radius = 2;

    // Changed at line 3 and line 3 + (2*radius) + 1 = line 8 ⇒ exactly 2*radius
    // (4) unchanged lines lie between them.
    const merged = diffLines(lines(20), lines(20).replace("line3", "L3").replace("line8", "L8"), {
      contextRadius: radius,
    });
    expect(merged.hunks).toHaveLength(1);
    expect(
      merged.hunks[0].rows.filter((row) => row.kind !== "context")
    ).toHaveLength(4); // both changes, each removed+added

    // One line further apart (line 3 and line 9 ⇒ 5 unchanged between) and the
    // same radius can no longer bridge them.
    const split = diffLines(lines(20), lines(20).replace("line3", "L3").replace("line9", "L9"), {
      contextRadius: radius,
    });
    expect(split.hunks).toHaveLength(2);
    expect(split.hunks[1].skippedBefore).toBeGreaterThan(0);
  });

  it("splits distant changes into separate hunks and counts the lines skipped between them", () => {
    const before = Array.from({ length: 30 }, (_, i) => `line${i + 1}`).join("\n");
    const after = before.replace("line3", "LINE3").replace("line25", "LINE25");
    const result = diffLines(before, after, { contextRadius: 1 });

    expect(result.hunks).toHaveLength(2);
    expect(result.hunks[0].skippedBefore).toBe(1);
    expect(result.hunks[0].beforeStart).toBe(2);
    expect(result.hunks[1].skippedBefore).toBe(19);
    expect(result.hunks[1].beforeStart).toBe(24);
    expect(result.hunks[1].rows.map((row) => row.kind)).toEqual([
      "context",
      "removed",
      "added",
      "context",
    ]);
  });

  it("returns one hunk holding every row when collapsing is turned off", () => {
    const before = Array.from({ length: 20 }, (_, i) => `line${i + 1}`).join("\n");
    const after = before.replace("line10", "LINE10");
    const result = diffLines(before, after, { collapseUnchanged: false });

    expect(result.hunks).toHaveLength(1);
    expect(result.hunks[0].rows).toEqual(result.rows);
    expect(result.hunks[0].skippedBefore).toBe(0);
  });

  it("refuses to diff a side past the max-lines threshold", () => {
    const big = Array.from({ length: 5 }, (_, i) => `line${i + 1}`).join("\n");
    const result = diffLines(big, "line1", { maxLines: 4 });

    expect(result.status).toBe("too-large");
    expect(result.rows).toEqual([]);
    expect(result.hunks).toEqual([]);
    expect(result.beforeLineCount).toBe(5);
    expect(result.afterLineCount).toBe(1);
  });

  it("still diffs when both sides sit exactly on the threshold", () => {
    const result = diffLines("a\nb", "a\nB", { maxLines: 2 });
    expect(result.status).toBe("diffed");
    expect(result.rows.map((row) => row.kind)).toEqual([
      "context",
      "removed",
      "added",
    ]);
  });
});
