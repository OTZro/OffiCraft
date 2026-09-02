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
