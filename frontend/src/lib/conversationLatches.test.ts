// The lease record behind every per-conversation latch in useChat. These pin
// the properties the rest of the fix RESTS on — the ones that used to be
// comments, and that four reviews in a row found somebody had stopped obeying.

import { describe, it, expect } from "vitest";
import { openLatches } from "./conversationLatches";

describe("openLatches", () => {
  it("a latch is never visible to, nor takeable by, another conversation", () => {
    const a = openLatches("a", true);
    expect(a.isHeld("a", "entryAnchor")).toBe(true);
    expect(a.isHeld("b", "entryAnchor")).toBe(false);
    expect(a.acquire("b", "loadingOlder")).toBe(null);
    // …and refusing to hand B a handle did not disturb A's own latch.
    expect(a.isHeld("a", "loadingOlder")).toBe(false);
    const mine = a.acquire("a", "loadingOlder");
    expect(mine).not.toBe(null);
    expect(a.isHeld("a", "loadingOlder")).toBe(true);
  });

  it("entering WITHOUT an anchor holds nothing at all", () => {
    const l = openLatches("a", false);
    for (const name of [
      "entryAnchor",
      "anchorFetch",
      "loadStale",
      "loadingOlder",
      "loadingNewer",
    ] as const) {
      expect(l.isHeld("a", name)).toBe(false);
    }
  });

  it("a same-direction mutex refuses the second holder and frees on the handle", () => {
    const l = openLatches("a", false);
    const first = l.acquire("a", "loadingOlder");
    expect(first).not.toBe(null);
    expect(l.acquire("a", "loadingOlder")).toBe(null);
    // The other direction is a different latch.
    expect(l.acquire("a", "loadingNewer")).not.toBe(null);
    first!();
    expect(l.isHeld("a", "loadingOlder")).toBe(false);
    expect(l.acquire("a", "loadingOlder")).not.toBe(null);
  });

  it("dropping an anchor lease ends the entry-anchor window, on every ending", () => {
    const l = openLatches("a", true);
    const release = l.acquire("a", "anchorFetch")!;
    expect(l.isHeld("a", "anchorFetch")).toBe(true);
    expect(l.isHeld("a", "entryAnchor")).toBe(true);
    release();
    expect(l.isHeld("a", "anchorFetch")).toBe(false);
    // R3-3: the superseded branch used to keep this set "because the caller
    // re-schedules", and the caller only cleared it on an EMPTY thread.
    expect(l.isHeld("a", "entryAnchor")).toBe(false);
  });

  it("anchor leases nest — the COUNT clears on the last one out, the entry window on the FIRST", () => {
    // ⚠️ The name used to say "only the last one out ends the window", which is
    // the opposite of what this record does and of what the test asserted
    // (R5-4: it never mentioned `entryAnchor` at all). "The window" in this
    // codebase's vocabulary is the ENTRY-ANCHOR window, and that one ends with
    // the FIRST lease dropped — deliberately, because `load()`'s gate is
    // `entryAnchor OR anchorFetch > 0`, so a still-held count keeps the door
    // shut anyway and nothing observes the difference. Both halves are pinned
    // here so the next reader does not "fix" the implementation to match a name.
    const l = openLatches("a", true);
    const first = l.acquire("a", "anchorFetch")!;
    const second = l.acquire("a", "anchorFetch")!;
    first();
    expect(l.isHeld("a", "anchorFetch")).toBe(true);
    expect(l.isHeld("a", "entryAnchor")).toBe(false);
    second();
    expect(l.isHeld("a", "anchorFetch")).toBe(false);
    expect(l.isHeld("a", "entryAnchor")).toBe(false);
  });

  it("a handle is spent once — a double release cannot take the count below zero", () => {
    // 🔴 R4-1's damage, made unreachable. Releasing a lease this call never
    // took drove `anchorFetching` to -1, which disabled the `> 0` gate for the
    // rest of the session. A handle is idempotent, and there is no other door.
    const l = openLatches("a", false);
    const outer = l.acquire("a", "anchorFetch")!;
    const inner = l.acquire("a", "anchorFetch")!;
    inner();
    inner();
    inner();
    expect(l.isHeld("a", "anchorFetch")).toBe(true);
    outer();
    expect(l.isHeld("a", "anchorFetch")).toBe(false);
    // Still zero, not negative: one more acquire is still visible.
    const again = l.acquire("a", "anchorFetch")!;
    expect(l.isHeld("a", "anchorFetch")).toBe(true);
    again();
  });

  it("the load debt is re-statable, and settling it clears it once", () => {
    // Not a mutex: the holder is the load that FAILED, the payer is the next
    // load that LANDS, and a second failure must not be refused.
    const l = openLatches("a", false);
    const first = l.acquire("a", "loadStale")!;
    const second = l.acquire("a", "loadStale")!;
    expect(l.isHeld("a", "loadStale")).toBe(true);
    second();
    expect(l.isHeld("a", "loadStale")).toBe(false);
    first();
    expect(l.isHeld("a", "loadStale")).toBe(false);
  });
});
