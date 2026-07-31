// lib/escapeLayers.ts — one Esc key, one owner: the INNERMOST layer.
//
// The bug this replaces (T-esc): every dismissible surface bound its own
// `window` keydown listener, so ONE Esc press was delivered to ALL of them and
// each decided for itself whether to close. A nested pair (task artifacts
// popover + the attachment preview overlay opened from inside it) therefore
// closed together, and the "did a child overlay open?" flag the popover
// consulted was already false by the time its own listener ran — DOM listeners
// fire in registration order, so the overlay had already unmounted and reported
// `false` upward. Whether the popover survived came down to which listener the
// browser happened to call first, which is why its guard reddened on roughly a
// third of runs and read as flake.
//
// The fix is structural: there is exactly ONE window keydown listener in the
// whole app — `escapeLayerOwnership.test.ts` holds that line, and it is the
// only thing that does, because a second listener reintroduces the bug while
// every other test in the suite stays green.
//
// WHICH layer gets the key is decided by DOM CONTAINMENT, not by registration
// order: the top layer is the one that has no other layer nested inside it.
// That is the fact the user is looking at. Registration order is only a proxy
// for it, and a broken one — React runs CHILD effects before PARENT effects, so
// a nested surface that mounts in the SAME commit as its host registers FIRST.
// Ordering by registration inverts that case completely: with three nested
// surfaces the OUTERMOST one takes Esc and the innermost can never receive it.
// "Nested surfaces are always opened by a later interaction" is not an
// invariant — nothing maintains it, and deep links open surfaces pre-nested.
//
// Registration order survives only as the tie-break between layers that are NOT
// nested in one another (two sibling dialogs): there the later one is on top.

type Layer = {
  /** Read at dispatch time, so a layer can keep its slot across renders while
   * its handler identity changes. */
  onEscape: () => void;
  /** This surface's root node, read live (the ref is populated after the
   * registering effect runs). `null` drops the layer to the registration-order
   * tie-break rather than guessing. */
  element: () => HTMLElement | null;
};

const layers: Layer[] = [];
let listening = false;

/** The layer with no other layer nested inside it — see the containment rule. */
function topLayer(): Layer | undefined {
  let top: Layer | undefined;
  let topEl: HTMLElement | null = null;
  for (const layer of layers) {
    const el = layer.element();
    if (top === undefined) {
      top = layer;
      topEl = el;
      continue;
    }
    if (topEl && el && topEl !== el) {
      if (topEl.contains(el)) {
        top = layer; // nested inside the incumbent ⇒ above it
        topEl = el;
        continue;
      }
      if (el.contains(topEl)) continue; // the incumbent is the inner one
    }
    top = layer; // unrelated (or unknown) ⇒ the later registration is on top
    topEl = el;
  }
  return top;
}

function handleKeyDown(e: KeyboardEvent) {
  if (e.key !== "Escape") return;
  // A focused control that already spent this Esc on itself (an inline edit
  // cancelling) calls preventDefault. The key is used up: it must NOT also
  // close the surface that control is sitting in.
  if (e.defaultPrevented) return;
  topLayer()?.onEscape();
}

/** Registers a layer. The returned function removes it — call it on unmount
 * (including a forced unmount: the caller's effect cleanup is the only path, so
 * there is no way to leave a dead layer behind). */
export function pushEscapeLayer(layer: Layer): () => void {
  layers.push(layer);
  if (!listening) {
    window.addEventListener("keydown", handleKeyDown);
    listening = true;
  }
  let released = false;
  return () => {
    if (released) return;
    released = true;
    const i = layers.lastIndexOf(layer);
    if (i !== -1) layers.splice(i, 1);
    if (layers.length === 0 && listening) {
      window.removeEventListener("keydown", handleKeyDown);
      listening = false;
    }
  };
}

/** Test-only introspection: how many layers are currently registered. */
export function escapeLayerCount(): number {
  return layers.length;
}
