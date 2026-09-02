// Every ASYNC LANDING POINT reachable from ChatArea must be on this list, with
// a verdict. It is a census, not a correctness proof — it cannot tell a guarded
// commit from an unguarded one. What it does is make the POPULATION
// self-maintaining, which is the thing that failed eleven times.
//
// 🔴 WHY THIS FILE EXISTS (T-48, R9-3). The stale-visit family had, by the
// ninth review, produced eleven instances. The criterion for finding them was
// fixed twice and the table format once, and it still missed:
//
//   * R7-1 (`useChat.loadAround`), R8-1 (`useChat.refetch`) and R9-1
//     (`useAttachmentStaging`) were each named in the inventory BEFORE they
//     were found, classified as safe, and therefore never looked at again;
//   * §5.1 told the next reader to re-derive the population by grepping for the
//     EXISTING guards (`convRef.current !== conv` / `visitRef.current !==`).
//     That start point can only ever find paths that already have a guard. A
//     path with no guard at all, in a file neither `useChat.ts` nor
//     `ChatArea.tsx`, scores zero hits and passes silently. R9-1 was exactly
//     that, and it passed eight reviews that way;
//   * the newest table column — "how many `await`s between the guard and the
//     commit" — cannot describe R9-1 either. That path has NO `await`. It is a
//     `FileReader.onload` callback, and a reader filling in the column by its
//     definition writes `0` and ticks the box.
//
// So the criterion here is NOT `await`. It is: **from the moment this callback
// is queued to the moment it commits, can the screen have changed
// conversations?** `await` is one syntax for that gap. `setTimeout`,
// `requestAnimationFrame`, `queueMicrotask`, an event listener, a
// Resize/IntersectionObserver, an SSE handler and `FileReader.onload` are
// others, and every one of them is scanned below.
//
// HOW IT ENUMERATES. The file set is WALKED, not typed in: it starts at
// `ChatArea.tsx` and follows relative imports transitively through
// `src/components`, `src/hooks` and `src/lib`. A new hook that ChatArea starts
// calling joins the population by itself — nobody has to remember to add it.
// Each (file, kind) pair carries a COUNT, so adding a second `setTimeout` to a
// file that already had one still reddens this test.
//
// 🔴 WHAT IT DOES NOT DO. It cannot check that a landing point is correctly
// guarded — that is what §5.1's per-commit-point table and the S-series mutants
// are for. A verdict below is a human's claim, and a wrong claim goes green
// here (three of them did). Its whole job is to guarantee that the claim EXISTS
// and is re-examined whenever the code under it moves.
//
// The boundary is written down rather than assumed: `src/api` and `src/i18n`
// are excluded from the walk. They hold no per-conversation React state — the
// transport layer's `await`s land in `useChat`/`ChatArea` callbacks, which ARE
// in scope — and including `api/http.ts` would put 126 unrelated `await`s under
// a count that churns on every new endpoint. Disagree by deleting the filter.

import { describe, it, expect } from "vitest";
import { existsSync, readFileSync } from "node:fs";
import { dirname, join, resolve } from "node:path";

const SRC = resolve(__dirname, "..");
const ENTRY = join(SRC, "components", "ChatArea.tsx");
const IN_SCOPE = /\/src\/(components|hooks|lib)\//;

/** The shapes that put a gap between "queued" and "commits". */
const KINDS: ReadonlyArray<readonly [string, RegExp]> = [
  ["await", /\bawait\b/g],
  [".then/.catch/.finally", /\.(then|catch|finally)\s*\(/g],
  ["setTimeout/setInterval", /\bset(Timeout|Interval)\s*\(/g],
  ["queueMicrotask", /\bqueueMicrotask\s*\(/g],
  ["requestAnimationFrame", /\brequestAnimationFrame\s*\(/g],
  ["addEventListener", /\baddEventListener\s*\(/g],
  ["Observer", /\bnew (Resize|Intersection|Mutation)Observer\b/g],
  ["FileReader/Image handler", /\bnew FileReader\b|\.(onload|onerror|onloadend)\s*=/g],
  ["JSX onLoad/onError", /\bon(Load|Error)=\{/g],
  ["subscribe", /\bsubscribe[A-Za-z]*\s*\(/g],
  // 🔴 THE CATCH-ALL (T-48, R10-5 B4). The nine shapes above were the ones this
  // codebase happened to use, and the tenth review showed the list was CLOSED
  // against nothing: `requestIdleCallback`, `window.onmessage =`,
  // `new Promise(` and `new BroadcastChannel(...).onmessage =` could all be
  // added to `ChatArea` itself and the census stayed green. This row is written
  // to catch a shape nobody has used yet, so it is deliberately broad, and the
  // lookaheads only keep it from double-counting the rows above it.
  [
    "other async primitive",
    /\.on(?!load\b|error\b|loadend\b)[a-z]+\s*=|\bnew (?!Resize|Intersection|Mutation)[A-Z]\w*Observer\b|\brequestIdleCallback\s*\(|\bnew (Promise|BroadcastChannel|MessageChannel|Worker|EventSource|WebSocket|SharedWorker)\s*\(/g,
  ],
];

/** file → kind → count, each with the verdict its author signed.
 *
 * A verdict answers ONE question: when this callback commits, can the screen be
 * showing a different conversation than the one that queued it — and if so,
 * what stops the write from landing in the wrong room? */
const REGISTRY: ReadonlyArray<{
  file: string;
  kind: string;
  count: number;
  verdict: string;
}> = [
  // ─── Mounted under `key={m.id}` — a conversation switch UNMOUNTS them ───
  // ⚠️ The key is the ONLY thing saving these: none of them carries a visit
  // guard of its own. Hoisting any of them above the keyed row reopens R9-1's
  // shape (§2.4's child-component warning).
  { file: "components/AttachmentStrip.tsx", kind: ".then/.catch/.finally", count: 2, verdict: "share-link copy; keyed row, and the id is globally unique" },
  { file: "components/AttachmentStrip.tsx", kind: "setTimeout/setInterval", count: 1, verdict: "the 已複製 flash; keyed row, cleared on unmount" },
  { file: "components/ChatReplyCard.tsx", kind: "await", count: 4, verdict: "refetch / doAnswer / doReanswer; keyed row" },
  { file: "components/ChatReplyCard.tsx", kind: ".then/.catch/.finally", count: 2, verdict: "the same three calls' arms; keyed row, unmounted by the switch" },
  { file: "components/ChatReplyCard.tsx", kind: "subscribe", count: 1, verdict: "card SSE; unsubscribed in the effect cleanup" },
  { file: "components/ReplyCardBody.tsx", kind: "await", count: 2, verdict: "answer submit; keyed row / keyed card" },
  { file: "components/ReplyComposer.tsx", kind: "await", count: 1, verdict: "send; keyed row / keyed card, and its staging declares \"remounts-per-conversation\"" },

  // ─── ChatArea itself — NOT remounted on a switch (OfficePage passes no key) ──
  { file: "components/ChatArea.tsx", kind: "await", count: 2, verdict: "submit(); guarded by `visitRef.current !== sendVisit` before every setter, with the draft SAVE deliberately before it (§3 rule 4)" },
  { file: "components/ChatArea.tsx", kind: ".then/.catch/.finally", count: 3, verdict: "loadAround + the wake button's two arms; `visitRef.current !== firedFor` is the first line of each (S7, P1)" },
  { file: "components/ChatArea.tsx", kind: "setTimeout/setInterval", count: 2, verdict: "highlight clear + centring settle; both write through `useKeyedState` setters and are cleared in the effect cleanup" },
  { file: "components/ChatArea.tsx", kind: "Observer", count: 1, verdict: "centring ResizeObserver; disconnected in the same cleanup" },
  { file: "hooks/useChat.ts", kind: "await", count: 17, verdict: "every commit point has `convRef.current !== conv` immediately before it (§5.1; S2/S5/S6/S8/S9/S10)" },
  { file: "hooks/useChat.ts", kind: ".then/.catch/.finally", count: 2, verdict: "load()'s arms; the effect's own `alive` flag, re-asked after every await" },
  { file: "hooks/useChat.ts", kind: "addEventListener", count: 2, verdict: "focus / visibilitychange; removed in the same effect's cleanup, deps [withId, refetchReads, conv]" },
  { file: "hooks/useChat.ts", kind: "subscribe", count: 1, verdict: "the SSE delta sink; unsubscribed in the same cleanup" },
  { file: "hooks/useQuotedMessageOverlay.tsx", kind: "await", count: 1, verdict: "open(); `visitRef.current !== firedFor` on both arms, visit token REQUIRED by the type (S11)" },
  { file: "hooks/useAttachmentStaging.ts", kind: "FileReader/Image handler", count: 2, verdict: "🔴 R9-1 lived here with ZERO guard and no `await` to hint at one. Now: the ROW carries its room. Every staged file is stamped at pick time and the composer renders only the rows for the room on screen, so a late landing has nowhere wrong to go; a file for a room not on screen — or for no mounted composer at all (R10-4) — is handed to that room's draft, never dropped (S12/S13/S14/S18/S19/S20)" },

  // ─── Rendered by ChatArea without a key — they survive a switch ───
  { file: "components/ChatGalleryPanel.tsx", kind: ".then/.catch/.finally", count: 2, verdict: "gallery fetch; `alive` + deps [member.id]. Since R9-2 the panel also closes on a switch, so it remounts per visit" },
  { file: "components/ChatGalleryPanel.tsx", kind: "addEventListener", count: 1, verdict: "Escape layer; removed on unmount" },
  { file: "components/ChatGalleryPanel.tsx", kind: "subscribe", count: 1, verdict: "gallery SSE; unsubscribed on unmount" },
  { file: "components/Avatar.tsx", kind: "JSX onLoad/onError", count: 1, verdict: "<img onError>; records the URL that failed, which can never match the next room's avatar, and the [personal, theme] effect clears it" },
  { file: "components/MarkdownPreviewOverlay.tsx", kind: "await", count: 1, verdict: "share-link copy; the overlay is keyed on the visit (R10-1) so it cannot outlive the room that opened it" },
  { file: "components/MarkdownPreviewOverlay.tsx", kind: ".then/.catch/.finally", count: 5, verdict: "blob fetch + copy; writes only this overlay's own state, and the overlay dies with the visit (R10-1)" },
  { file: "components/MarkdownPreviewOverlay.tsx", kind: "setTimeout/setInterval", count: 2, verdict: "the 已複製 flash timers; write only this overlay's own state, and the overlay dies with the visit (R10-1)" },
  { file: "components/MarkdownPreviewOverlay.tsx", kind: "addEventListener", count: 13, verdict: "keydown pager, wheel/touch/gesture zoom, resize, pointermove; all removed on unmount, and the overlay unmounts with the visit (R10-1)" },
  { file: "components/MarkdownPreviewOverlay.tsx", kind: "JSX onLoad/onError", count: 1, verdict: "<img onLoad> sizing; writes only this overlay's own layout" },
  // 🔴 THE `mdPreview` EXEMPTION IS GONE, AND ITS EPITAPH IS THE POINT (R10-1).
  // Those five rows used to point at an exemption reading: `.md-preview` is
  // `position: fixed; inset: 0` with a backdrop, so while it is open nothing
  // can change the peer and these writers cannot be outlived by a switch. It
  // even named its own precondition honestly ("EVERY gesture that can change
  // the peer is blocked by it") and admitted nobody had driven the hash-routing
  // path. The tenth review drove it: open A's preview, change `member`, and the
  // header says Bruno while the overlay still shows A's filename and A's
  // content. The premise was already false when it was written.
  //
  // The fix is not in this file and not in that overlay: `ChatArea` now holds
  // `mdPreview` in `useKeyedState(session, …)`, so the overlay cannot outlive
  // the visit and all five rows below are structurally safe without one of
  // their callbacks being touched.
  //
  // ⚠️ READ THIS BEFORE TRUSTING ANY VERDICT HERE. That exemption was careful,
  // explicit about its own assumptions, and WRONG — and this test had nothing
  // to say about it, because a verdict is a human's claim and a wrong claim is
  // still a claim. What this file guarantees is that a claim EXISTS for every
  // landing point and gets re-read whenever the code under it moves. It does
  // not, and cannot, check that the claim is true. That has now cost one live
  // bug, so it is a demonstrated limit rather than a theoretical one.

  // ─── Global / conversation-independent ───
  { file: "hooks/sharedServerSettings.ts", kind: "addEventListener", count: 2, verdict: "storage + auth invalidation; global, not per conversation" },
  { file: "hooks/useIsMobile.ts", kind: "addEventListener", count: 1, verdict: "matchMedia breakpoint; global" },
  { file: "hooks/useWindowActive.ts", kind: "addEventListener", count: 3, verdict: "focus/blur/visibility; global" },
  { file: "hooks/useOwnerName.tsx", kind: ".then/.catch/.finally", count: 4, verdict: "the owner's own nickname; global, one provider" },
  { file: "hooks/useWorkerCodenames.ts", kind: ".then/.catch/.finally", count: 2, verdict: "module-level cache keyed by globally-unique `ow-` ids; setTick only asks for a repaint" },
  { file: "lib/deltaSink.ts", kind: "queueMicrotask", count: 1, verdict: "one coalescing decision per burst; the sink is torn down with its subscription" },
  { file: "lib/escapeLayers.ts", kind: "addEventListener", count: 1, verdict: "the shared Escape stack; layers deregister on unmount" },
  { file: "lib/hashRoute.ts", kind: "addEventListener", count: 1, verdict: "hashchange; it is the thing that CHANGES the visit, not something that outlives one" },
  { file: "lib/hashRoute.ts", kind: "subscribe", count: 1, verdict: "route subscribers; same" },
  { file: "lib/scrollToLatest.ts", kind: "setTimeout/setInterval", count: 1, verdict: "settle timer; writes scrollTop on an element handed in by the caller, and the caller clears it" },
  { file: "lib/scrollToLatest.ts", kind: "Observer", count: 1, verdict: "ResizeObserver on that same element; disconnected by the same caller" },
  { file: "lib/shareLink.ts", kind: "await", count: 3, verdict: "returns a value to its caller; commits nothing itself. The count was 2 while this census counted LINES — one line here holds two awaits (R10-5 B5)" },
  { file: "lib/sharedSnapshot.ts", kind: ".then/.catch/.finally", count: 1, verdict: "single-flight settings snapshot; global generation, not per conversation" },
];

function walkFromChatArea(): string[] {
  const resolveSpec = (fromFile: string, spec: string): string | null => {
    if (!spec.startsWith(".")) return null;
    const base = join(dirname(fromFile), spec);
    for (const c of [base, `${base}.ts`, `${base}.tsx`, join(base, "index.ts"), join(base, "index.tsx")]) {
      if (existsSync(c) && !/\.(css|json|svg)$/.test(c)) return c;
    }
    return null;
  };
  const seen = new Set<string>();
  const queue = [ENTRY];
  while (queue.length > 0) {
    const file = queue.shift() as string;
    if (seen.has(file)) continue;
    seen.add(file);
    // 🔴 EVERY IMPORT SYNTAX, NOT JUST THE PRETTY ONE (T-48, R10-5 B1/B2/B3).
    // The walk used to match `from "…"` only, so a side-effect import
    // (`import "./x"`), a single-quoted specifier and a dynamic `import("./x")`
    // each dropped a file out of the population SILENTLY. B3 is the one that
    // matters: the day somebody `React.lazy`-splits `MarkdownPreviewOverlay`,
    // its 22 landing points would have left the census with nothing going red.
    for (const m of readFileSync(file, "utf8").matchAll(
      /(?:\bfrom|\bimport)\s*\(?\s*['"]([^'"]+)['"]/g,
    )) {
      const r = resolveSpec(file, m[1]);
      if (r && !r.includes(".test.") && IN_SCOPE.test(r)) queue.push(r);
    }
  }
  return [...seen].sort();
}

function scan(): Map<string, number> {
  const found = new Map<string, number>();
  for (const file of walkFromChatArea()) {
    const lines = readFileSync(file, "utf8").split("\n");
    for (const [kind, re] of KINDS) {
      let n = 0;
      for (const line of lines) {
        const t = line.trim();
        // Prose about a landing point is not a landing point.
        if (t.startsWith("//") || t.startsWith("*")) continue;
        // 🔴 OCCURRENCES, NOT LINES (T-48, R10-5 B5). The header promised that
        // "adding a second `setTimeout` to a file that already had one still
        // reddens this test", and that was only true on a NEW line — a second
        // one appended to a counted line was free. `lib/shareLink.ts` had
        // already walked into it: one line there holds two `await`s.
        n += line.match(re)?.length ?? 0;
      }
      if (n > 0) found.set(`${file.slice(SRC.length + 1)} | ${kind}`, n);
    }
  }
  return found;
}

describe("async landing points reachable from ChatArea", () => {
  it("are all on the register, with the count they have today", () => {
    const found = scan();
    const registered = new Map(
      REGISTRY.map((r) => [`${r.file} | ${r.kind}`, r.count]),
    );
    const asRows = (m: Map<string, number>) =>
      [...m].map(([k, n]) => `${k} | ${n}`).sort();
    // One comparison, both directions: a NEW landing point with no verdict, a
    // count that grew, and a register row describing code that no longer exists
    // all read out of the same diff.
    expect(asRows(found)).toEqual(asRows(registered));
  });

  it("carry a verdict each — an entry with nothing said about it is not an entry", () => {
    expect(REGISTRY.filter((r) => r.verdict.trim().length < 20)).toEqual([]);
  });

  it("keep every shape they have ever scanned for", () => {
    // Deleting a KIND is the one edit that shrinks the population without
    // adding a single row to the diff's register — every file that only ever
    // landed through that shape simply stops being counted, and both directions
    // of the comparison above agree. So the list's LENGTH is pinned too, and
    // growing it is deliberate work: the number below moves only with a new row
    // and the verdicts it drags in.
    expect(KINDS.length).toBe(11);
    expect(KINDS.map(([k]) => k)).toContain("other async primitive");
  });

  it("start from a file set that is walked, not typed in", () => {
    // The bug this guards: §5.1 used to tell the next reader to derive the
    // population by grepping for the guards that already exist, which can only
    // find paths that already have one. The walk starts at ChatArea and follows
    // imports, so a hook it newly calls is in the population before anybody
    // remembers to say so.
    const files = walkFromChatArea();
    expect(files).toContain(join(SRC, "hooks", "useAttachmentStaging.ts"));
    expect(files.length).toBeGreaterThan(30);
  });
});
