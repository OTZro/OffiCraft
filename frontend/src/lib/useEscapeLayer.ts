// lib/useEscapeLayer.ts — the React binding for the Esc layer stack.
//
// Replaces the `window.addEventListener("keydown", …)` every dismissible
// surface used to write for itself. A component that calls this receives Esc
// ONLY while it is the innermost registered layer; see escapeLayers.ts for why
// nesting — not registration order — is what decides that.

import { useEffect, useRef } from "react";
import type { RefObject } from "react";
import { pushEscapeLayer } from "./escapeLayers";

/**
 * @param onEscape what this surface does when Esc reaches it. Its identity may
 *   change every render — the layer keeps its slot regardless.
 * @param ref the surface's root element. Nesting is read off it, so a surface
 *   that omits it can only be ordered by registration time and will lose to a
 *   sibling that registered later. Pass it whenever the surface can contain
 *   another one.
 * @param active whether the surface is currently on screen. A surface that
 *   mounts only when open can leave this at its default; one that stays mounted
 *   around a nullable open-state passes that state here so it holds no layer
 *   while closed.
 */
export function useEscapeLayer(
  onEscape: () => void,
  ref?: RefObject<HTMLElement | null>,
  active = true,
): void {
  const handler = useRef(onEscape);
  useEffect(() => {
    handler.current = onEscape;
  });
  useEffect(() => {
    if (!active) return;
    // The element is read at DISPATCH time, not here: the ref is still null
    // while this effect runs on a surface whose root mounts with it.
    return pushEscapeLayer({
      onEscape: () => handler.current(),
      element: () => ref?.current ?? null,
    });
  }, [active, ref]);
}
