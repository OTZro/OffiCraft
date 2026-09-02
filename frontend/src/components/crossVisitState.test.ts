// The OTHER half of the population `asyncLandingPoints.test.ts` enumerates.
//
// 🔴 WHY THIS FILE EXISTS (T-48, R11-12). The eleventh review pointed out that
// the machine census next door lists ASYNC LANDING POINTS — await, .then,
// setTimeout, FileReader.onload — and that this family's last three instances
// were not any of those:
//
//   * R9-2  `galleryOpen`       — a plain `useState` that survived a switch;
//   * R10-1 (the real one)      — `AttachmentStrip`'s own `preview`, same shape;
//   * R11-9 `expandedGroups`    — same shape again, holding other rooms' ids.
//
// None of them has an async landing point at all, so none of them could ever
// have gone red over there. The half that was still being watched by hand was
// "state that outlives a visit", and the hand-written census it relied on
// (latch-inventory §2.4) is exactly the kind of list `useKeyedState`'s own
// header says should not exist.
//
// So the POPULATION is walked here: every React state/ref declaration in
// `ChatArea.tsx` must appear below, and which hook each one uses is READ FROM
// THE SOURCE rather than claimed. A new `useState` in this component is red
// until somebody writes down which lifetime it has and why.
//
// 🔴 WHAT IT STILL CANNOT DO, written down rather than assumed:
//
//  1. The REASON column is a human's claim, and a wrong claim goes green — the
//     same limit the sibling census carries, demonstrated twice (R11-10 wrote a
//     new wrong verdict in the same commit that added the warning). What is
//     mechanical here is narrower and worth having on its own: the population,
//     and the hook each member actually uses.
//  2. THE SCOPE IS THIS ONE FILE. `AttachmentStrip.preview` — the state behind
//     the real R10-1 — lives in a child component and would not be caught here.
//     Widening the walk to every file `ChatArea` imports would enumerate a few
//     hundred declarations in components that have nothing to do with a
//     conversation; the honest boundary today is "the component that is REUSED
//     across conversations instead of being remounted", which is this one.
//     A child that is reused the same way (rendered without a key that changes
//     with the room) belongs here too, and adding it means widening `SOURCES`.

import { describe, it, expect } from "vitest";
import { readFileSync } from "node:fs";
import { join, resolve } from "node:path";

const SRC = resolve(__dirname, "..");
const SOURCES = [join(SRC, "components", "ChatArea.tsx")];

type Hook = "useState" | "useRef" | "useKeyedState";

/** Every state/ref declaration in the scanned sources, read from the source. */
function scan(): Map<string, Hook> {
  const found = new Map<string, Hook>();
  const decl =
    /\bconst\s+(?:\[\s*([A-Za-z0-9_]+)\s*,[^\]]*\]|([A-Za-z0-9_]+))\s*=\s*(useState|useRef|useKeyedState)\s*[<(]/g;
  for (const file of SOURCES) {
    const text = readFileSync(file, "utf8");
    for (const m of text.matchAll(decl)) {
      found.set(m[1] ?? m[2], m[3] as Hook);
    }
  }
  return found;
}

/** name → the hook it MUST be declared with, and why that is the right
 * lifetime. A `useKeyedState` row needs no reason: the hook is the answer. */
const REGISTRY: ReadonlyArray<{
  name: string;
  hook: Hook;
  reason?: string;
}> = [
  // ---- per visit: rebuilt when the visit changes, setters bound to it ----
  { name: "wakePending", hook: "useKeyedState" },
  { name: "wakeUndispatched", hook: "useKeyedState" },
  { name: "mdPreview", hook: "useKeyedState" },
  { name: "galleryOpen", hook: "useKeyedState" },
  { name: "expandedGroups", hook: "useKeyedState" },
  { name: "firstUnreadId", hook: "useKeyedState" },
  { name: "latestInView", hook: "useKeyedState" },
  { name: "newMsgPreview", hook: "useKeyedState" },
  { name: "jumpNotice", hook: "useKeyedState" },
  { name: "jumpRetry", hook: "useKeyedState" },
  { name: "highlightMsgId", hook: "useKeyedState" },

  // ---- outlives the visit ON PURPOSE, and here is who takes it over ----
  {
    name: "draft",
    hook: "useState",
    reason:
      "not reset on a switch but SWAPPED to the new peer's stored draft, in the render-phase visitRef block — a keyed slot would blank the words the owner left in the room they are entering",
  },
  {
    name: "replyToId",
    hook: "useState",
    reason:
      "swapped with the draft it belongs to, by the same block, and for the same reason: a restored draft without its reply target sends the message somewhere the owner did not aim it",
  },
  {
    name: "inputRef",
    hook: "useRef",
    reason: "a DOM handle for the one textarea this component renders; its lifetime is the element's, and a switch does not replace the element",
  },
  {
    name: "fileInputRef",
    hook: "useRef",
    reason: "a DOM handle for the hidden file input; same lifetime as the element, which the switch does not replace",
  },
  {
    name: "messagesRef",
    hook: "useRef",
    reason: "a DOM handle for the scroll container; same lifetime as the element, and every reader of it also gates on messagesPeer",
  },
  {
    name: "endRef",
    hook: "useRef",
    reason: "a DOM handle for the bottom sentinel; same lifetime as the element",
  },
  {
    name: "isComposingRef",
    hook: "useRef",
    reason: "mirrors a LIVE DOM event pair (compositionstart/end), not anything about the conversation; clearing it from outside would desync it from the browser's own composition state",
  },
  {
    name: "visitRef",
    hook: "useRef",
    reason: "the visit mirror ITSELF — the one thing that cannot live in the record it mirrors, since it exists to notice that the record changed",
  },
  {
    name: "didMountAttachRestoreRef",
    hook: "useRef",
    reason: "a one-shot whose lifetime is the COMPONENT: in the record it would reset on every switch and replay the mount restore on top of the switch block's own, staging every attachment twice",
  },
  {
    name: "adoptRef",
    hook: "useRef",
    reason: "holds the CURRENT render's adoptAttachments so the per-room registration does not have to be rebuilt every render; it is re-pointed on every render, so there is no stale value to survive anything",
  },
  {
    name: "jumpSettleRef",
    hook: "useRef",
    reason: "the UNMOUNT cleanup reads it; in the record that cleanup would read the new conversation's null and leave the previous one's ResizeObserver and 2.6s timer running past unmount (R5-5)",
  },
];

describe("ChatArea's React state population is enumerated, not remembered", () => {
  it("every declaration in the source is on the register, and vice versa", () => {
    const found = scan();
    expect([...found.keys()].sort()).toEqual(
      REGISTRY.map((r) => r.name).sort(),
    );
  });

  it("uses the hook the register says it uses", () => {
    const found = scan();
    const mismatched = REGISTRY.filter((r) => found.get(r.name) !== r.hook);
    expect(mismatched).toEqual([]);
  });

  it("makes anything that outlives a visit say why", () => {
    // A keyed slot needs no reason — the hook IS the reason, and it is checked
    // above. Everything else is a piece of state the switch walks straight
    // past, which is the shape twelve instances of this family have had.
    const unexplained = REGISTRY.filter(
      (r) => r.hook !== "useKeyedState" && (r.reason ?? "").trim().length < 40,
    );
    expect(unexplained).toEqual([]);
  });
});
