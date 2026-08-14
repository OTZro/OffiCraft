import { describe, it, expect } from "vitest";
import { anchorDelta } from "./scrollAnchor";

// The browser-side half of T-4e39 (which container scrolls, and by how much a
// real note moves) is measured in visual-guards/taskcard-note-anchor.ct.spec.tsx
// — jsdom has no layout. What is worth pinning here is the arithmetic: given
// where the row ended up and where it must be, how far does the scrollport move.
const view = { top: 0, bottom: 800 };

describe("anchorDelta", () => {
  it("returns the row to the y it was clicked at when a reflow pushed it down", () => {
    expect(anchorDelta({ top: 500, bottom: 560 }, view, 200)).toBe(300);
  });

  it("returns the row to the y it was clicked at when a reflow pulled it up", () => {
    expect(anchorDelta({ top: 120, bottom: 180 }, view, 200)).toBe(-80);
  });

  it("scrolls further so a row whose bottom fell past the fold is fully shown", () => {
    // pinned at 600, 300 tall ⇒ bottom 900, 100 past the fold, and there is
    // 600 of headroom above, so all 100 can be recovered.
    expect(anchorDelta({ top: 600, bottom: 900 }, view, 600)).toBe(100);
  });

  it("keeps the row's top edge on screen rather than reveal its whole bottom", () => {
    // 900 tall — taller than the viewport. Revealing the bottom would need 200,
    // but only 100 of headroom exists; the top edge wins.
    expect(anchorDelta({ top: 100, bottom: 1000 }, view, 100)).toBe(100);
  });

  it("pulls a row back down when the anchor sits above the scrollport", () => {
    expect(anchorDelta({ top: -50, bottom: 10 }, view, -50)).toBe(-50);
  });

  it("does nothing when the row neither moved nor overflows", () => {
    expect(anchorDelta({ top: 200, bottom: 260 }, view, 200)).toBe(0);
  });

  it("measures against the scrollport's own box, not the window's", () => {
    // an inner container that starts 120px down and ends at 700
    const inner = { top: 120, bottom: 700 };
    expect(anchorDelta({ top: 600, bottom: 760 }, inner, 600)).toBe(60);
    expect(anchorDelta({ top: 100, bottom: 200 }, inner, 100)).toBe(-20);
  });
});
