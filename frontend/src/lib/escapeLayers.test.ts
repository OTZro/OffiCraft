// escapeLayers — the app's single Esc dispatcher. These cases pin the property
// the surfaces depend on: the key reaches the INNERMOST layer and nobody else,
// no matter what order the layers registered in.

import { describe, it, expect, vi, afterEach } from "vitest";
import { pushEscapeLayer, escapeLayerCount } from "./escapeLayers";

function pressEscape() {
  window.dispatchEvent(new KeyboardEvent("keydown", { key: "Escape", cancelable: true }));
}

const opened: Array<() => void> = [];
function open(onEscape: () => void, element: HTMLElement | null = null) {
  const release = pushEscapeLayer({ onEscape, element: () => element });
  opened.push(release);
  return release;
}

/** A chain of attached nodes, outermost first. */
function nest(depth: number): HTMLElement[] {
  const nodes: HTMLElement[] = [];
  let parent: HTMLElement = document.body;
  for (let i = 0; i < depth; i += 1) {
    const el = document.createElement("div");
    parent.appendChild(el);
    nodes.push(el);
    parent = el;
  }
  return nodes;
}

afterEach(() => {
  while (opened.length) opened.pop()!();
  document.body.innerHTML = "";
});

describe("pushEscapeLayer", () => {
  it("delivers Escape to the innermost layer, whatever order they registered in", () => {
    // React runs CHILD effects before PARENT effects, so a nested surface that
    // mounts in the same commit as its host registers FIRST. Ordering by
    // registration would hand Esc to the outer one here.
    const [outer, inner] = nest(2);
    const onOuter = vi.fn();
    const onInner = vi.fn();
    open(onInner, inner);
    open(onOuter, outer);

    pressEscape();

    expect(onInner).toHaveBeenCalledTimes(1);
    expect(onOuter).not.toHaveBeenCalled();
  });

  it("reaches the deepest of three nested layers registered inside-out", () => {
    // Ordering by registration inverts this case completely: the OUTERMOST
    // layer takes the key and the innermost can never receive it.
    const [outer, middle, inner] = nest(3);
    const calls: string[] = [];
    open(() => calls.push("inner"), inner);
    open(() => calls.push("middle"), middle);
    open(() => calls.push("outer"), outer);

    pressEscape();

    expect(calls).toEqual(["inner"]);
  });

  it("uncovers the next layer out once the inner one is released", () => {
    const [outer, inner] = nest(2);
    const onOuter = vi.fn();
    const onInner = vi.fn();
    open(onOuter, outer);
    const releaseInner = open(onInner, inner);

    pressEscape();
    releaseInner();
    pressEscape();

    expect(onInner).toHaveBeenCalledTimes(1);
    expect(onOuter).toHaveBeenCalledTimes(1);
  });

  it("gives Escape to the later of two layers that are not nested in one another", () => {
    // Siblings have no containment answer, so recency decides: the dialog the
    // user opened second is the one in front of them.
    const first = document.createElement("div");
    const second = document.createElement("div");
    document.body.append(first, second);
    const onFirst = vi.fn();
    const onSecond = vi.fn();
    open(onFirst, first);
    open(onSecond, second);

    pressEscape();

    expect(onSecond).toHaveBeenCalledTimes(1);
    expect(onFirst).not.toHaveBeenCalled();
  });

  it("falls back to registration order for a layer with no element", () => {
    const onFirst = vi.fn();
    const onSecond = vi.fn();
    open(onFirst);
    open(onSecond);

    pressEscape();

    expect(onSecond).toHaveBeenCalledTimes(1);
    expect(onFirst).not.toHaveBeenCalled();
  });

  it("ignores an Escape a focused control already spent", () => {
    // An inline edit cancels itself and calls preventDefault. The key is used
    // up: it must not ALSO close the surface that control sits in.
    const layer = vi.fn();
    open(layer);

    pressEscape();
    expect(layer).toHaveBeenCalledTimes(1);

    const spent = new KeyboardEvent("keydown", { key: "Escape", cancelable: true });
    spent.preventDefault();
    window.dispatchEvent(spent);

    expect(layer).toHaveBeenCalledTimes(1);
  });

  it("ignores keys other than Escape", () => {
    const layer = vi.fn();
    open(layer);

    window.dispatchEvent(new KeyboardEvent("keydown", { key: "Enter" }));

    expect(layer).not.toHaveBeenCalled();
  });

  it("keeps the remaining layer when a buried one is released first", () => {
    // A surface can be torn down out of order (a route change unmounts the host
    // while its overlay is still up).
    const buried = vi.fn();
    const kept = vi.fn();
    const releaseBuried = open(buried);
    open(kept);

    releaseBuried();
    pressEscape();

    expect(kept).toHaveBeenCalledTimes(1);
    expect(buried).not.toHaveBeenCalled();
  });

  it("is a no-op once every layer is released", () => {
    const layer = vi.fn();
    const release = open(layer);
    release();

    pressEscape();

    expect(layer).not.toHaveBeenCalled();
    expect(escapeLayerCount()).toBe(0);
  });

  it("survives a release called twice without dropping another layer", () => {
    // React 18 StrictMode runs an effect's cleanup more than once; a double
    // release must not splice out an unrelated layer.
    const survivor = vi.fn();
    const release = open(vi.fn());
    open(survivor);

    release();
    release();
    pressEscape();

    expect(survivor).toHaveBeenCalledTimes(1);
    expect(escapeLayerCount()).toBe(1);
  });

  it("reads the layer's handler at dispatch time, so a re-render keeps its slot", () => {
    const lower = vi.fn();
    const latest = vi.fn();
    open(lower);
    const layer = { onEscape: vi.fn(), element: () => null };
    opened.push(pushEscapeLayer(layer));

    layer.onEscape = latest;
    pressEscape();

    expect(latest).toHaveBeenCalledTimes(1);
    expect(lower).not.toHaveBeenCalled();
  });
});
