// lib/scrollToLatest.ts — land the chat thread on its NEWEST message and STAY
// there while the layout settles (T-48 ③).
//
// Two defects it exists to close, both measured in the isolated environment:
//
//  1. THE OLD TARGET WAS THE WRONG ROW. The 「有新訊息」 chip scrolled to
//     `newMsgAnchorId` — the FIRST unseen message — so a burst of ten arrivals
//     left the reader on message 1 with five more still below the fold. The
//     divider marks where the unread block STARTS; the jump is for reaching the
//     END of it.
//
//  2. THE LANDING WAS NEVER CORRECTED. The chip used
//     `scrollIntoView({ behavior: "smooth" })` and stopped there. Anything above
//     the target that grows AFTER the scroll — an image decoding to its real
//     height, an inline reply card refetching — pushes the row straight back out
//     of view, and the smooth animation makes the miss look like the scroll
//     simply went somewhere else. The hash-route jump (ChatArea's `jumpToMsgId`
//     reactor) already solved this with a ResizeObserver that re-settles until
//     the layout stops moving; this is that same discipline, extracted so both
//     entry points share it rather than one of them having it.
//
// ⚠️ NOT SMOOTH, deliberately. The correction re-scrolls; an animation would be
// interrupted and restarted by every reflow, which reads as the thread lurching.
// The hash jump is instant for the same reason.

/** How long the landing keeps being re-corrected. Same window as the hash
 * jump's highlight — long enough for images and lazy cards, short enough that
 * an owner who scrolls away afterwards is never yanked back. */
const SETTLE_MS = 2600;

/**
 * Scroll `scroller` so the LAST `[data-msg-id]` row it contains is fully
 * visible, then keep it that way for {@link SETTLE_MS} as content reflows.
 * Returns a disposer; calling it stops the correction early.
 */
export function scrollToLatest(scroller: HTMLElement): () => void {
  const rows = scroller.querySelectorAll<HTMLElement>("[data-msg-id]");
  const latest = rows[rows.length - 1];
  if (!latest) return () => {};
  const settle = () => latest.scrollIntoView({ block: "end" });
  settle();
  // A ResizeObserver on the viewport itself never fires — its box is clamped by
  // the flex column — so watch the in-flow children, whose height is what
  // actually grows. Same choice, and the same reason, as the hash jump's.
  if (typeof ResizeObserver === "undefined") return () => {};
  const ro = new ResizeObserver(settle);
  for (const child of Array.from(scroller.children)) ro.observe(child);
  const timer = window.setTimeout(() => ro.disconnect(), SETTLE_MS);
  return () => {
    window.clearTimeout(timer);
    ro.disconnect();
  };
}

// ─────────────────────────────────────────────────────────────────────────────

/** The only tolerance left in the newest-row test, and it is sized against
 * FRACTIONAL PIXELS, not against anything in the layout. `getBoundingClientRect`
 * returns sub-pixel values and a scroll position lands on a fraction of a device
 * pixel, so a row that is exactly flush measures as ±0.5px off; measured
 * residues on the settled landing are +0.13 (1280px) and -0.50 (390px).
 *
 * 🔴 IT IS NOT ALLOWED TO GROW TO COVER A LAYOUT DISTANCE, and after
 * {@link isLatestRowInView} it cannot need to: the gap, the padding and the
 * zero-height sentinel below the last row are no longer inside the quantity
 * being compared. The number this replaced (`AT_LATEST_PX = 4` in ChatArea) was
 * exactly that mistake — it was measured against the container's bottom, so the
 * 12px flex gap sat inside the distance and 4px could never absorb it, and the
 * comment beside it asserted that it did. Anyone tempted to raise this to clear
 * a gap is re-creating that bug with a bigger number: fix the measurement, not
 * the tolerance. */
const SUBPIXEL_PX = 1;

/**
 * Is the NEWEST message row inside `scroller`'s viewport?
 *
 * 🔴 THIS IS NOT "IS THE BOX SCROLLED TO THE BOTTOM", and the difference is a
 * shipped bug (T-48). The owner's condition for the 回到最新 arrow is 「不在最新
 * 訊息時有個向下箭頭」 — a question about a ROW. The box's own bottom answers a
 * different question, because the box holds things that are not the newest row:
 * `.chat__messages` is a flex column with `gap: 12px` and ChatArea renders a
 * zero-height `endRef` sentinel after the last message, so the newest row's
 * bottom sits 12px ABOVE the scrollable bottom. `scrollToLatest` lands the row
 * flush with the viewport — the honest landing — and a container-bottom test
 * then reported 12px of "still below the fold" and put the arrow back, every
 * single time, on a viewport where the newest message was fully visible
 * (measured, 12/12 runs across both widths: `lastRowBottomGap` 0.13/-0.50 while
 * the container distance read 12/11).
 *
 * Measuring the ROW instead of the BOX also deletes the fact that used to rot:
 * nothing here knows or cares what the gap is, so changing it in CSS cannot
 * silently break the arrow. The e2e probe that pins this runs the same flow with
 * `gap: 40px` forced on, and it is the case that stays green.
 *
 * Returns true for an empty thread — there is no newest row to be out of view.
 */
export function isLatestRowInView(scroller: HTMLElement): boolean {
  const rows = scroller.querySelectorAll<HTMLElement>("[data-msg-id]");
  const latest = rows[rows.length - 1];
  if (!latest) return true;
  // The same row `scrollToLatest` scrolls to — the two must never disagree
  // about which row "the latest" is, which is why they live in one file.
  return (
    latest.getBoundingClientRect().bottom -
      scroller.getBoundingClientRect().bottom <=
    SUBPIXEL_PX
  );
}
