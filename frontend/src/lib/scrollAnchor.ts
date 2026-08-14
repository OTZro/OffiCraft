// T-4e39 — keep the row the user just clicked where the user just clicked it.
//
// The cockpit scrolls in an INNER container (`.tasks` carries overflow-y:auto;
// `document.scrollHeight` equals the window height at every measured width), so
// the scroll to correct for is that container's `scrollTop`, not the page's.
// `scrollIntoView` is deliberately not used: its block/nearest handling differs
// between engines and it re-targets every scrollport on the ancestor chain,
// which is a different action from "put this one row back".

/** A vertical span in viewport coordinates. */
export type Span = { top: number; bottom: number };

/**
 * How much to add to the scroll container's `scrollTop` (positive = content
 * moves up) so that, after a reflow, `el`:
 *   1. sits back at `prevTop` — the viewport y it occupied when it was clicked;
 *   2. shows as much of its own bottom as `view` has room for, never by pushing
 *      its top edge above `view.top`.
 *
 * Rule 2 loses to rule 1's top edge on purpose: an element taller than the
 * viewport cannot be fully revealed, and the half worth keeping is the top —
 * that is where the control the user pressed and the first line of the text are.
 */
export function anchorDelta(el: Span, view: Span, prevTop: number): number {
  let delta = el.top - prevTop;
  const top = el.top - delta;
  const bottom = el.bottom - delta;
  if (bottom > view.bottom) {
    delta += Math.min(bottom - view.bottom, top - view.top);
  } else if (top < view.top) {
    delta -= view.top - top;
  }
  return delta;
}

/**
 * The nearest ancestor that actually scrolls `el` vertically, or the document
 * scrolling element when nothing on the chain does.
 *
 * "Actually scrolls" needs BOTH halves: an `overflow-y:auto` box whose content
 * fits absorbs no scrolling, and treating it as the scrollport would silently
 * make every correction a no-op.
 */
export function scrollParent(el: Element): Element {
  const doc = el.ownerDocument;
  const root = doc.scrollingElement ?? doc.documentElement;
  let node: Element | null = el.parentElement;
  while (node && node !== root) {
    const style = node.ownerDocument.defaultView?.getComputedStyle(node);
    const overflowY = style?.overflowY ?? "";
    if (
      (overflowY === "auto" || overflowY === "scroll" || overflowY === "overlay") &&
      node.scrollHeight > node.clientHeight
    ) {
      return node;
    }
    node = node.parentElement;
  }
  return root;
}

/** The visible span of a scrollport, in viewport coordinates. */
export function viewportSpanOf(container: Element): Span {
  const doc = container.ownerDocument;
  const root = doc.scrollingElement ?? doc.documentElement;
  if (container === root) {
    return { top: 0, bottom: doc.defaultView?.innerHeight ?? root.clientHeight };
  }
  const r = container.getBoundingClientRect();
  return { top: r.top, bottom: r.top + container.clientHeight };
}

/**
 * Put `el` back at `prevTop` and reveal what fits. Returns the delta applied,
 * so callers (and tests) can see whether anything moved.
 */
export function keepAnchored(el: Element, prevTop: number): number {
  const container = scrollParent(el);
  const r = el.getBoundingClientRect();
  const delta = anchorDelta(
    { top: r.top, bottom: r.bottom },
    viewportSpanOf(container),
    prevTop
  );
  if (delta !== 0) container.scrollTop += delta;
  return delta;
}
