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
// So the POPULATION is walked here: every React state/ref declaration in the
// scanned sources must appear below, and which hook each one uses is READ FROM
// THE SOURCE rather than claimed. A new `useState` in them is red until
// somebody writes down which lifetime it has and why.
//
// 🔴 WHAT IT STILL CANNOT DO, written down rather than assumed:
//
//  1. The REASON column is a human's claim, and a wrong claim goes green — the
//     same limit the sibling census carries, demonstrated three times now
//     (R11-10 wrote a new wrong verdict in the same commit that added the
//     warning; R12-4 found another one, `messagesRef`'s, in the commit that
//     added THIS file). What is mechanical here is narrower and worth having
//     on its own: the population, and the hook each member actually uses.
//  2. THE SCOPE IS THE THREE FILES IN `SOURCES` — the component that is REUSED
//     across conversations instead of being remounted, plus the two hooks it
//     calls that are reused with it. The hooks were added in the twelfth round
//     (R12-3): the boundary sentence had always covered them (nothing about
//     `useAttachmentStaging`'s `staged` is remounted on a switch either), and
//     six instances of this family — R9-1, R10-3, R10-4, R11-2, R11-3, R11-4 —
//     live in them, but `SOURCES` had only the component.
//     Still OUT: `AttachmentStrip.preview`, the state behind the real R10-1,
//     which lives in a child component. Widening the walk to every file
//     `ChatArea` imports would enumerate a few hundred declarations in
//     components that have nothing to do with a conversation. A child or hook
//     that is reused the same way (rendered/called without a key that changes
//     with the room) belongs here too, and adding it means widening `SOURCES`.
//  3. WHICH DECLARATION SHAPES THE WALK RECOGNISES is itself a boundary, and it
//     used to be an unwritten one (R12-2: a `useReducer` slot and a `let [x,
//     setX] = useState()` were both invisible, and `tsc` had nothing to say
//     about either). Two mechanical answers rather than a wider promise:
//     `STATE_HOOKS` below lists the hooks that produce cross-visit state and
//     the walk accepts `const`/`let`/`var` in both destructured and plain form;
//     and the last two tests are the reverse assertions — an unknown `use…`
//     name anywhere in these files is RED until it is classified, and a line
//     that calls a state hook without being a registered declaration is RED
//     too. So a shape this file cannot parse fails loudly instead of passing.

import { describe, it, expect } from "vitest";
import { readFileSync } from "node:fs";
import { basename, join, resolve } from "node:path";

const SRC = resolve(__dirname, "..");
const SOURCES = [
  join(SRC, "components", "ChatArea.tsx"),
  join(SRC, "hooks", "useAttachmentStaging.ts"),
  join(SRC, "hooks", "useChat.ts"),
];

/** The hooks that mint state which a conversation switch walks straight past.
 * `useKeyedState` / `useKeyedRecord` are in here because they are the ANSWER,
 * not because they are a risk — a row using one needs no further reason. */
const STATE_HOOKS = [
  "useState",
  "useRef",
  "useReducer",
  "useSyncExternalStore",
  "useKeyedState",
  "useKeyedRecord",
] as const;
type Hook = (typeof STATE_HOOKS)[number];

/** Every OTHER `use…` name these files mention, and why it mints no state of
 * its own that a switch could carry. An unknown name is red (last test but
 * one), so a new hook cannot arrive here unclassified. */
const NON_STATE_HOOKS: Readonly<Record<string, string>> = {
  useEffect: "runs, holds nothing",
  useLayoutEffect: "runs, holds nothing",
  useMemo: "derived from this render's inputs",
  useCallback: "derived from this render's inputs",
  useI18n: "context read",
  useIsMobile: "media query, nothing to do with the conversation",
  useWindowActive: "window focus, nothing to do with the conversation",
  useOwnerDisplayName: "context read",
  useWorkerCodenames: "module-level cache keyed by worker id",
  useQuotedMessageOverlay: "keyed on the visit record it is handed",
  useAttachmentStaging: "scanned in its own right — see SOURCES",
  useChat: "scanned in its own right — see SOURCES",
};

/** A declaration of one of `STATE_HOOKS`: `const [x, setX] = useState(…)`, or
 * `const xRef = useRef(…)`, in const/let/var form. */
const DECL = new RegExp(
  String.raw`\b(?:const|let|var)\s+(?:\[\s*([A-Za-z0-9_]+)\s*,[^\]]*\]|([A-Za-z0-9_]+))\s*=\s*(` +
    STATE_HOOKS.join("|") +
    String.raw`)\s*[<(]`,
  "g",
);
/** Any hook-shaped call or type application, comments included. */
const ANY_HOOK = /\buse[A-Z][A-Za-z0-9_]*\s*[<(]/g;
/** A source line that is prose, not code. */
const isComment = (line: string) => /^\s*(\/\/|\/?\*)/.test(line);

/** Every state/ref declaration in the scanned sources, read from the source,
 * keyed `<file>:<name>` so two files may hold same-named refs. */
function scan(): Map<string, Hook> {
  const found = new Map<string, Hook>();
  for (const file of SOURCES) {
    const text = readFileSync(file, "utf8");
    for (const m of text.matchAll(DECL)) {
      found.set(`${basename(file)}:${m[1] ?? m[2]}`, m[3] as Hook);
    }
  }
  return found;
}

/** name → the hook it MUST be declared with, and why that is the right
 * lifetime. A keyed row needs no reason: the hook is the answer. */
const REGISTRY: ReadonlyArray<{
  file: string;
  name: string;
  hook: Hook;
  reason?: string;
}> = [
  // ================= ChatArea.tsx =================
  // ---- the visit record itself, and the slots keyed on it ----
  { file: "ChatArea.tsx", name: "session", hook: "useKeyedRecord" },
  { file: "ChatArea.tsx", name: "wakePending", hook: "useKeyedState" },
  { file: "ChatArea.tsx", name: "wakeUndispatched", hook: "useKeyedState" },
  { file: "ChatArea.tsx", name: "mdPreview", hook: "useKeyedState" },
  { file: "ChatArea.tsx", name: "galleryOpen", hook: "useKeyedState" },
  { file: "ChatArea.tsx", name: "expandedGroups", hook: "useKeyedState" },
  { file: "ChatArea.tsx", name: "firstUnreadId", hook: "useKeyedState" },
  { file: "ChatArea.tsx", name: "latestInView", hook: "useKeyedState" },
  { file: "ChatArea.tsx", name: "newMsgPreview", hook: "useKeyedState" },
  { file: "ChatArea.tsx", name: "jumpNotice", hook: "useKeyedState" },
  { file: "ChatArea.tsx", name: "jumpRetry", hook: "useKeyedState" },
  { file: "ChatArea.tsx", name: "highlightMsgId", hook: "useKeyedState" },

  // ---- outlives the visit ON PURPOSE, and here is who takes it over ----
  {
    file: "ChatArea.tsx",
    name: "draft",
    hook: "useState",
    reason:
      "not reset on a switch but SWAPPED to the new peer's stored draft, in the render-phase visitRef block — a keyed slot would blank the words the owner left in the room they are entering",
  },
  {
    file: "ChatArea.tsx",
    name: "replyToId",
    hook: "useState",
    reason:
      "swapped with the draft it belongs to, by the same block, and for the same reason: a restored draft without its reply target sends the message somewhere the owner did not aim it",
  },
  {
    file: "ChatArea.tsx",
    name: "inputRef",
    hook: "useRef",
    reason:
      "a DOM handle for the one textarea this component renders; its lifetime is the element's, and a switch does not replace the element",
  },
  {
    file: "ChatArea.tsx",
    name: "fileInputRef",
    hook: "useRef",
    reason:
      "a DOM handle for the hidden file input; same lifetime as the element, which the switch does not replace",
  },
  {
    file: "ChatArea.tsx",
    name: "messagesRef",
    hook: "useRef",
    reason:
      "a DOM handle for the scroll container, and the ELEMENT is what carries the room: it is rendered only inside the `shownMessages.length > 0` branch, so on a frame where messagesPeer is still the previous room the node does not exist and every reader stops at its own `if (!el)`. It is NOT true that every reader gates on messagesPeer — three do not (onMessagesScroll, which would otherwise markRead the previous room's newest ts; the entry-scroll effect; jumpToLatest), which is what R12-4 caught this row claiming",
  },
  {
    file: "ChatArea.tsx",
    name: "endRef",
    hook: "useRef",
    reason:
      "a DOM handle for the bottom sentinel; same lifetime as the element",
  },
  {
    file: "ChatArea.tsx",
    name: "isComposingRef",
    hook: "useRef",
    reason:
      "mirrors a LIVE DOM event pair (compositionstart/end), not anything about the conversation; clearing it from outside would desync it from the browser's own composition state",
  },
  {
    file: "ChatArea.tsx",
    name: "visitRef",
    hook: "useRef",
    reason:
      "the visit mirror ITSELF — the one thing that cannot live in the record it mirrors, since it exists to notice that the record changed",
  },
  {
    file: "ChatArea.tsx",
    name: "didMountAttachRestoreRef",
    hook: "useRef",
    reason:
      "a one-shot whose lifetime is the COMPONENT: in the record it would reset on every switch and replay the mount restore on top of the switch block's own, staging every attachment twice",
  },
  {
    file: "ChatArea.tsx",
    name: "adoptRef",
    hook: "useRef",
    reason:
      "holds the CURRENT render's adoptAttachments so the per-room registration does not have to be rebuilt every render; it is re-pointed on every render, so there is no stale value to survive anything",
  },
  {
    file: "ChatArea.tsx",
    name: "jumpSettleRef",
    hook: "useRef",
    reason:
      "the UNMOUNT cleanup reads it; in the record that cleanup would read the new conversation's null and leave the previous one's ResizeObserver and 2.6s timer running past unmount (R5-5)",
  },

  // ================= useAttachmentStaging.ts =================
  // Reused exactly as `ChatArea` is: a conversation switch swaps the `target`
  // argument and this hook's state survives it untouched.
  {
    file: "useAttachmentStaging.ts",
    name: "staged",
    hook: "useState",
    reason:
      "holds EVERY room's rows on purpose — a read that finishes seconds later, in another room, has to be kept somewhere until the foreign-landing effect files it in that room's own draft. Each row is STAMPED with its target and the exposed `pendingAttachments` is the slice for the room on screen, so there is no unfiltered value for a reader to reach (R9-1)",
  },
  {
    file: "useAttachmentStaging.ts",
    name: "attachError",
    hook: "useState",
    reason:
      "the rejection notice must OUTLIVE the visit, because the room that raised it may not be on screen when the read finishes (R11-4/R12-1); it carries its target the way a staged row does, and the exposed value is null while that target is not the one on screen",
  },
  {
    file: "useAttachmentStaging.ts",
    name: "targetRef",
    hook: "useRef",
    reason:
      "the live target, read by the deferred FileReader callback so a file is stamped with the room the owner was looking at when they PICKED it; re-pointed every render, so there is no stale value to survive anything",
  },
  {
    file: "useAttachmentStaging.ts",
    name: "keepElsewhereRef",
    hook: "useRef",
    reason:
      "mirrors the keepElsewhere callback for the same reason: the commit runs inside a FileReader callback that outlives the render it was created in, and an unmount is exactly when it is needed (R10-4)",
  },
  {
    file: "useAttachmentStaging.ts",
    name: "mountedRef",
    hook: "useRef",
    reason:
      "the one thing the DATA cannot say about itself — 'there is no composer left for this file to land in' (R10-4). Its lifetime is the COMPONENT, not the conversation, and it is read at exactly one place: the commit",
  },

  // ================= useChat.ts =================
  // Reused the same way: `withId` changes, the hook does not remount.
  { file: "useChat.ts", name: "conv", hook: "useKeyedRecord" },
  {
    file: "useChat.ts",
    name: "thread",
    hook: "useState",
    reason:
      "the loaded window is REPLACED by the load that answers for the new peer, not blanked by the switch, and it carries its own `peer` beside the messages so every reader can tell whose window it is holding — `ChatArea`'s `shownMessages` is derived from exactly that comparison",
  },
  {
    file: "useChat.ts",
    name: "peerRead",
    hook: "useState",
    reason:
      "the peer's read watermark, stored WITH the peer it belongs to for the same reason `thread` is: the value answers whose it is, instead of every reader having to remember to ask",
  },
  {
    file: "useChat.ts",
    name: "threadRef",
    hook: "useRef",
    reason:
      "live mirror of `thread` for the async loaders — a state read after an await is a stale closure — and re-pointed on every render, so it has no value of its own to survive a switch",
  },
  {
    file: "useChat.ts",
    name: "loadSeqRef",
    hook: "useRef",
    reason:
      "a MONOTONIC load clock, deliberately NOT per conversation (see the note beside `conv`): a ticket taken later must outrank one taken earlier even ACROSS a peer switch, and resetting it would let a stale in-flight load out-rank a fresh one",
  },
  {
    file: "useChat.ts",
    name: "committedSeqRef",
    hook: "useRef",
    reason:
      "the other half of that same global clock — the last generation allowed to commit; per conversation it would forget that a newer load already landed, which is the race it exists to lose",
  },
  {
    file: "useChat.ts",
    name: "entryAnchorRef",
    hook: "useRef",
    reason:
      "mirrors the entry anchor for the subscription effect's SETUP body only; as a dependency it would re-subscribe the whole SSE sink every time the route keeps the msgId in the hash",
  },
  {
    file: "useChat.ts",
    name: "convRef",
    hook: "useRef",
    reason:
      "answers 'is the record I captured still the live one?' from inside an async callback — the record cannot mirror itself — and it is re-pointed every render. It exists to ANSWER that question, never to reach a latch (latch-inventory §3 rule 2)",
  },
];

describe("the reused chat surface's React state population is enumerated, not remembered", () => {
  it("every declaration in the sources is on the register, and vice versa", () => {
    const found = scan();
    expect([...found.keys()].sort()).toEqual(
      REGISTRY.map((r) => `${r.file}:${r.name}`).sort(),
    );
  });

  it("uses the hook the register says it uses", () => {
    const found = scan();
    const mismatched = REGISTRY.filter(
      (r) => found.get(`${r.file}:${r.name}`) !== r.hook,
    );
    expect(mismatched).toEqual([]);
  });

  it("makes anything that outlives a visit say why", () => {
    // A keyed slot needs no reason — the hook IS the reason, and it is checked
    // above. Everything else is a piece of state the switch walks straight
    // past, which is the shape twelve instances of this family have had.
    const keyed: Hook[] = ["useKeyedState", "useKeyedRecord"];
    const unexplained = REGISTRY.filter(
      (r) => !keyed.includes(r.hook) && (r.reason ?? "").trim().length < 40,
    );
    expect(unexplained).toEqual([]);
  });

  it("refuses to be walked past by a hook it has never heard of", () => {
    // R12-2: the walk used to recognise three hook names and say nothing about
    // the rest, so a cross-visit `useReducer` slot was invisible to it AND to
    // tsc. An unfamiliar `use…` here is a shape nobody has classified yet.
    const unclassified = new Map<string, string>();
    for (const file of SOURCES) {
      for (const m of readFileSync(file, "utf8").matchAll(ANY_HOOK)) {
        const name = m[0].replace(/\s*[<(]$/, "");
        if ((STATE_HOOKS as readonly string[]).includes(name)) continue;
        if (name in NON_STATE_HOOKS) continue;
        unclassified.set(name, basename(file));
      }
    }
    expect([...unclassified]).toEqual([]);
  });

  it("refuses a state hook that is called anywhere but a registered declaration", () => {
    // The other half of R12-2: the register can only hold what the declaration
    // pattern above can parse. Any CODE line that calls a state hook and is not
    // such a declaration is a shape this file would otherwise skip in silence.
    const stray: string[] = [];
    for (const file of SOURCES) {
      const lines = readFileSync(file, "utf8").split("\n");
      lines.forEach((line, i) => {
        if (isComment(line)) return;
        for (const m of line.matchAll(ANY_HOOK)) {
          const name = m[0].replace(/\s*[<(]$/, "");
          if (!(STATE_HOOKS as readonly string[]).includes(name)) continue;
          DECL.lastIndex = 0;
          if (!DECL.test(line)) {
            stray.push(`${basename(file)}:${i + 1} ${line.trim()}`);
          }
        }
      });
    }
    expect(stray).toEqual([]);
  });
});
