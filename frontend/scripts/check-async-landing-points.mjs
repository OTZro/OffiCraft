#!/usr/bin/env node
// T-48 — the ASYNC LANDING POINT census for the chat surface.
//
// Every callback reachable from `ChatArea` that can commit AFTER the screen has
// moved on must be on the register below, with a verdict. It is a census, not a
// correctness proof — it cannot tell a guarded commit from an unguarded one.
// What it does is make the POPULATION self-maintaining, which is the thing that
// failed eleven times.
//
// 🔴 WHY IT EXISTS (R9-3). The stale-visit family had, by the ninth review,
// produced eleven instances. The criterion for finding them was fixed twice and
// the table format once, and it still missed:
//
//   * R7-1 (`useChat.loadAround`), R8-1 (`useChat.refetch`) and R9-1
//     (`useAttachmentStaging`) were each named in the inventory BEFORE they were
//     found, classified as safe, and therefore never looked at again;
//   * the census used to be re-derived by grepping for the EXISTING guards. That
//     start point can only ever find paths that already have a guard; a path
//     with no guard at all scores zero hits and passes silently. R9-1 was
//     exactly that, and it passed eight reviews that way;
//   * "how many `await`s between the guard and the commit" cannot describe R9-1
//     either — that path has NO `await`. It is a `FileReader.onload` callback.
//
// So the criterion is NOT `await`. It is: **from the moment this callback is
// queued to the moment it commits, can the screen have moved on?** `await` is
// one syntax for that gap. `setTimeout`, `requestAnimationFrame`,
// `queueMicrotask`, an event listener, a Resize/IntersectionObserver, an SSE
// handler and `FileReader.onload` are others, and every one is scanned below.
//
// HOW IT ENUMERATES. The file set is WALKED, not typed in: it starts at
// `ChatArea.tsx` and follows relative imports transitively through
// `src/components`, `src/hooks` and `src/lib`. A new hook that ChatArea starts
// calling joins the population by itself. Each (file, kind) pair carries a
// COUNT, so a second `setTimeout` in a file that already had one still reddens.
//
// 🔴 WHAT IT DOES NOT DO. It cannot check that a landing point is correctly
// guarded — a verdict is a human's claim, and a wrong claim goes green (three of
// them did). Its whole job is to guarantee that the claim EXISTS and is
// re-examined whenever the code under it moves.
//
// 🔴 WHY IT IS A LINT AND NOT A VITEST FILE (T-48, R13-6). It used to live in
// `src/components/asyncLandingPoints.test.ts`, mixed in with the behaviour
// tests, so "the census is out of date" and "the chat window is broken" were the
// same colour of red. This repo already has a home for a rule that reads source
// text — the `lint-*` family in `bin/ci.sh` — and this is one of those.
//
// The boundary is written down rather than assumed: `src/api` and `src/i18n` are
// excluded from the walk. They hold no per-conversation React state — the
// transport layer's `await`s land in `useChat`/`ChatArea` callbacks, which ARE
// in scope — and including `api/http.ts` would put 126 unrelated `await`s under
// a count that churns on every new endpoint. Disagree by deleting the filter.
//
// Run: `npm run lint:async-landing` (also wired into bin/ci.sh).

import { existsSync, readFileSync } from "node:fs";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";

// ASYNC_LANDING_SRC re-points the scanned tree — the ONLY reason it exists is
// check-async-landing-points.test.ts, which copies the real sources to a temp
// dir, sabotages one and asserts this script goes red. A guard nobody has
// watched fail is not a guard.
const SRC = process.env.ASYNC_LANDING_SRC
  ? resolve(process.env.ASYNC_LANDING_SRC)
  : resolve(dirname(fileURLToPath(import.meta.url)), "..", "src");
const ENTRY = join(SRC, "components", "ChatArea.tsx");
// In scope = under `src/components`, `src/hooks` or `src/lib`, expressed
// RELATIVE to the scanned root so the selftest's temp copy is scanned the same
// way the real tree is (a root-absolute pattern silently matched nothing there,
// which would have made every sabotage below "pass").
const inScope = (file) =>
  /^(components|hooks|lib)[\\/]/.test(file.slice(SRC.length + 1));

/** The shapes that put a gap between "queued" and "commits". */
const KINDS = [
  ["await", /\bawait\b/g],
  [".then/.catch/.finally", /\.(then|catch|finally)\s*\(/g],
  ["setTimeout/setInterval", /\bset(Timeout|Interval)\s*\(/g],
  ["queueMicrotask", /\bqueueMicrotask\s*\(/g],
  ["requestAnimationFrame", /\brequestAnimationFrame\s*\(/g],
  ["addEventListener", /\baddEventListener\s*\(/g],
  ["Observer", /\bnew (Resize|Intersection|Mutation)Observer\b/g],
  [
    "FileReader/Image handler",
    /\bnew FileReader\b|\.(onload|onerror|onloadend)\s*=/g,
  ],
  ["JSX onLoad/onError", /\bon(Load|Error)=\{/g],
  ["subscribe", /\bsubscribe[A-Za-z]*\s*\(/g],
  // 🔴 THE CATCH-ALL (R10-5 B4). The nine shapes above were the ones this
  // codebase happened to use, and the list was CLOSED against nothing:
  // `requestIdleCallback`, `window.onmessage =`, `new Promise(` and
  // `new BroadcastChannel(...).onmessage =` could all be added to `ChatArea` and
  // the census stayed green. This row is written to catch a shape nobody has
  // used yet, so it is deliberately broad; the lookaheads only keep it from
  // double-counting the rows above it.
  [
    "other async primitive",
    /\.on(?!load\b|error\b|loadend\b)[a-z]+\s*=|\bnew (?!Resize|Intersection|Mutation)[A-Z]\w*Observer\b|\brequestIdleCallback\s*\(|\bnew (Promise|BroadcastChannel|MessageChannel|Worker|EventSource|WebSocket|SharedWorker)\s*\(/g,
  ],
];

/** The whole list of shapes, pinned. Deleting a KIND is the one edit that
 * shrinks the population without adding a single row to the register — every
 * file that only ever landed through that shape simply stops being counted, and
 * both directions of the comparison below agree. Swapping one shape out for
 * another keeps the LENGTH the same, so the list itself is pinned, not its
 * length (R11-11). */
const KIND_NAMES = [
  "await",
  ".then/.catch/.finally",
  "setTimeout/setInterval",
  "queueMicrotask",
  "requestAnimationFrame",
  "addEventListener",
  "Observer",
  "FileReader/Image handler",
  "JSX onLoad/onError",
  "subscribe",
  "other async primitive",
];

/** file → kind → count, each with the verdict its author signed.
 *
 * A verdict answers ONE question: when this callback commits, can the screen be
 * showing something other than what queued it — and if so, what stops the write
 * from landing in the wrong place? */
const REGISTRY = [
  // ─── Mounted under a key that changes with the thing they belong to — the
  //     row, the card, or (since R13-5) the conversation itself. A switch
  //     UNMOUNTS them, so React drops every write these make. ───
  // ⚠️ The key is the ONLY thing saving these: none of them carries a guard of
  // its own. Hoisting any of them above the keyed element reopens R9-1's shape.
  { file: "components/AttachmentStrip.tsx", kind: ".then/.catch/.finally", count: 2, verdict: "share-link copy; keyed row, and the id is globally unique" },
  { file: "components/AttachmentStrip.tsx", kind: "setTimeout/setInterval", count: 1, verdict: "the 已複製 flash; keyed row, cleared on unmount" },
  { file: "components/ChatReplyCard.tsx", kind: "await", count: 4, verdict: "refetch / doAnswer / doReanswer; keyed row" },
  { file: "components/ChatReplyCard.tsx", kind: ".then/.catch/.finally", count: 2, verdict: "the same three calls' arms; keyed row, unmounted by the switch" },
  { file: "components/ChatReplyCard.tsx", kind: "subscribe", count: 1, verdict: "card SSE; unsubscribed in the effect cleanup" },
  { file: "components/ReplyCardBody.tsx", kind: "await", count: 2, verdict: "answer submit; keyed row / keyed card" },
  { file: "components/ReplyComposer.tsx", kind: "await", count: 1, verdict: "send; keyed row / keyed card, and its staging declares \"remounts-per-conversation\"" },

  // ─── ChatArea itself. Since R13-5 `OfficePage` mounts it under
  //     `key={peerId}`, so a conversation switch unmounts it and every setter
  //     below writes into a component React has discarded. That is what
  //     `lint-chat-area-key` keeps true; there is no per-callback guard left. ──
  { file: "components/ChatArea.tsx", kind: "await", count: 2, verdict: "submit(); the room's own draft is written to the store BEFORE the on-screen restore, so a failed send survives even when nobody is looking (§3 rule 4)" },
  { file: "components/ChatArea.tsx", kind: ".then/.catch/.finally", count: 3, verdict: "loadAround + the wake button's two arms; all three write only this room's state, and this room is this mount" },
  { file: "components/ChatArea.tsx", kind: "setTimeout/setInterval", count: 2, verdict: "highlight clear + centring settle; cleared in the effect cleanup, and the state they write dies with the room" },
  { file: "components/ChatArea.tsx", kind: "Observer", count: 1, verdict: "centring ResizeObserver; disconnected in the same cleanup" },
  { file: "hooks/useChat.ts", kind: "await", count: 17, verdict: "one hook instance per room (R13-3), so a commit from a room the owner has left lands in a discarded component; the generation clock is what orders the commits WITHIN a room" },
  { file: "hooks/useChat.ts", kind: ".then/.catch/.finally", count: 2, verdict: "load()'s arms; the effect's own `alive` flag, re-asked after every await" },
  { file: "hooks/useChat.ts", kind: "addEventListener", count: 2, verdict: "focus / visibilitychange; removed in the same effect's cleanup" },
  { file: "hooks/useChat.ts", kind: "subscribe", count: 1, verdict: "the SSE delta sink; unsubscribed in the same cleanup" },
  { file: "hooks/useQuotedMessageOverlay.tsx", kind: "await", count: 1, verdict: "open(); its state dies with the room that opened it (R13-5), and `busyRef` keeps one click to one request" },
  { file: "hooks/useAttachmentStaging.ts", kind: "FileReader/Image handler", count: 2, verdict: "🔴 R9-1 lived here with ZERO guard and no `await` to hint at one. Now the commit NAMES ITS SLOT: `updateChatDraftAttachments(peer, …)` writes into the draft of the room the file was picked for, whatever is on screen and whether or not any composer is mounted. R9-1, R10-4 and R11-2 have no shape left to happen in (S12/S13/S14/S18/S19/S20)" },
  { file: "hooks/useAttachmentStaging.ts", kind: "subscribe", count: 1, verdict: "the one `subscribe` callback both `useSyncExternalStore` calls share, onto this peer's draft slice; React unsubscribes on unmount, and a per-mount caller gets `peerId === null` and subscribes to nothing at all" },
  { file: "lib/chatDraftStore.ts", kind: "subscribe", count: 1, verdict: "the per-peer draft subscription; each composer unsubscribes on unmount, and a write is delivered to whoever is showing that peer — which is the point" },

  // ─── Rendered by ChatArea without a key of their own — they live and die
  //     with ChatArea, which lives and dies with the conversation. ───
  { file: "components/ChatGalleryPanel.tsx", kind: ".then/.catch/.finally", count: 2, verdict: "gallery fetch; `alive` + deps [member.id], and the panel dies with the room" },
  { file: "components/ChatGalleryPanel.tsx", kind: "addEventListener", count: 1, verdict: "Escape layer; removed on unmount" },
  { file: "components/ChatGalleryPanel.tsx", kind: "subscribe", count: 1, verdict: "gallery SSE; unsubscribed on unmount" },
  { file: "components/Avatar.tsx", kind: "JSX onLoad/onError", count: 1, verdict: "<img onError>; records the URL that failed, which can never match another avatar, and the [personal, theme] effect clears it" },
  // ⚠️ WHO UNMOUNTS THIS OVERLAY — four mount points, two owners, and the
  // eleventh review caught this line claiming one of them for all four (R11-10).
  // `ChatArea` mounts it three times from `mdPreview`; `AttachmentStrip` mounts
  // it a fourth time from its own `preview`, and a strip is not inside a
  // conversation at all — two of its three call sites are a reply card and the
  // task-artifacts popover. That one is bounded instead by the list it renders:
  // the preview is looked up in `attachments` every render, so it cannot survive
  // the list it was opened from (R11-1).
  { file: "components/MarkdownPreviewOverlay.tsx", kind: "await", count: 1, verdict: "share-link copy; writes only this overlay's own state, and the overlay dies with whatever opened it" },
  { file: "components/MarkdownPreviewOverlay.tsx", kind: ".then/.catch/.finally", count: 5, verdict: "blob fetch + copy; writes only this overlay's own state, and the overlay dies with whatever opened it" },
  { file: "components/MarkdownPreviewOverlay.tsx", kind: "setTimeout/setInterval", count: 2, verdict: "the 已複製 flash timers; write only this overlay's own state" },
  { file: "components/MarkdownPreviewOverlay.tsx", kind: "addEventListener", count: 13, verdict: "keydown pager, wheel/touch/gesture zoom, resize, pointermove; all removed on unmount, and the overlay unmounts with whatever opened it" },
  { file: "components/MarkdownPreviewOverlay.tsx", kind: "JSX onLoad/onError", count: 1, verdict: "<img onLoad> sizing; writes only this overlay's own layout" },
  // ⚠️ READ THIS BEFORE TRUSTING ANY VERDICT HERE. The five rows above used to
  // point at an exemption reading: `.md-preview` is `position: fixed; inset: 0`
  // with a backdrop, so while it is open nothing can change the peer. It was
  // careful, explicit about its own assumptions, and WRONG — the site routes on
  // the hash, so back/forward changed the peer under the open overlay, and this
  // census had nothing to say about it because a verdict is a human's claim.
  // That has now cost one live bug, so it is a demonstrated limit rather than a
  // theoretical one.

  // ─── Global / conversation-independent ───
  { file: "hooks/sharedServerSettings.ts", kind: "addEventListener", count: 2, verdict: "storage + auth invalidation; global, not per conversation" },
  { file: "hooks/useIsMobile.ts", kind: "addEventListener", count: 1, verdict: "matchMedia breakpoint; global" },
  { file: "hooks/useWindowActive.ts", kind: "addEventListener", count: 3, verdict: "focus/blur/visibility; global" },
  { file: "hooks/useOwnerName.tsx", kind: ".then/.catch/.finally", count: 4, verdict: "the owner's own nickname; global, one provider" },
  { file: "hooks/useWorkerCodenames.ts", kind: ".then/.catch/.finally", count: 2, verdict: "module-level cache keyed by globally-unique `ow-` ids; setTick only asks for a repaint" },
  { file: "lib/deltaSink.ts", kind: "queueMicrotask", count: 1, verdict: "one coalescing decision per burst; the sink is torn down with its subscription" },
  { file: "lib/escapeLayers.ts", kind: "addEventListener", count: 1, verdict: "the shared Escape stack; layers deregister on unmount" },
  { file: "lib/hashRoute.ts", kind: "addEventListener", count: 1, verdict: "hashchange; it is the thing that CHANGES the conversation, not something that outlives one" },
  { file: "lib/hashRoute.ts", kind: "subscribe", count: 1, verdict: "route subscribers; same" },
  { file: "lib/scrollToLatest.ts", kind: "setTimeout/setInterval", count: 1, verdict: "settle timer; writes scrollTop on an element handed in by the caller, and the caller clears it" },
  { file: "lib/scrollToLatest.ts", kind: "Observer", count: 1, verdict: "ResizeObserver on that same element; disconnected by the same caller" },
  { file: "lib/shareLink.ts", kind: "await", count: 3, verdict: "returns a value to its caller; commits nothing itself. The count was 2 while this census counted LINES — one line here holds two awaits (R10-5 B5)" },
  { file: "lib/sharedSnapshot.ts", kind: ".then/.catch/.finally", count: 1, verdict: "single-flight settings snapshot; global generation, not per conversation" },
];

function walkFromChatArea() {
  const resolveSpec = (fromFile, spec) => {
    if (!spec.startsWith(".")) return null;
    const base = join(dirname(fromFile), spec);
    for (const c of [
      base,
      `${base}.ts`,
      `${base}.tsx`,
      join(base, "index.ts"),
      join(base, "index.tsx"),
    ]) {
      if (existsSync(c) && !/\.(css|json|svg)$/.test(c)) return c;
    }
    return null;
  };
  const seen = new Set();
  const queue = [ENTRY];
  while (queue.length > 0) {
    const file = queue.shift();
    if (seen.has(file)) continue;
    seen.add(file);
    // 🔴 EVERY IMPORT SYNTAX, NOT JUST THE PRETTY ONE (R10-5 B1/B2/B3). The walk
    // used to match `from "…"` only, so a side-effect import (`import "./x"`), a
    // single-quoted specifier and a dynamic `import("./x")` each dropped a file
    // out of the population SILENTLY.
    for (const m of readFileSync(file, "utf8").matchAll(
      /(?:\bfrom|\bimport)\s*\(?\s*['"]([^'"]+)['"]/g,
    )) {
      const r = resolveSpec(file, m[1]);
      if (r && !r.includes(".test.") && inScope(r)) queue.push(r);
    }
  }
  return [...seen].sort();
}

function scan(files) {
  const found = new Map();
  for (const file of files) {
    const lines = readFileSync(file, "utf8").split("\n");
    for (const [kind, re] of KINDS) {
      let n = 0;
      for (const line of lines) {
        const t = line.trim();
        // Prose about a landing point is not a landing point.
        if (t.startsWith("//") || t.startsWith("*")) continue;
        // 🔴 OCCURRENCES, NOT LINES (R10-5 B5): a second `setTimeout` appended
        // to an already-counted line used to be free.
        n += line.match(re)?.length ?? 0;
      }
      if (n > 0) found.set(`${file.slice(SRC.length + 1)} | ${kind}`, n);
    }
  }
  return found;
}

const problems = [];

// 1. The shapes scanned for are the shapes that have ever been scanned for.
const kinds = KINDS.map(([k]) => k);
if (JSON.stringify(kinds) !== JSON.stringify(KIND_NAMES)) {
  problems.push(
    `the scanned SHAPES changed:\n  scanning: ${kinds.join(", ")}\n  pinned:   ${KIND_NAMES.join(", ")}`,
  );
}

// 2. Every registered row says something.
for (const r of REGISTRY) {
  if (r.verdict.trim().length < 20) {
    problems.push(
      `${r.file} | ${r.kind}: an entry with nothing said about it is not an entry`,
    );
  }
}

// 3. The population is WALKED, not typed in.
const files = walkFromChatArea();
if (!files.includes(join(SRC, "hooks", "useAttachmentStaging.ts"))) {
  problems.push(
    "the walk no longer reaches hooks/useAttachmentStaging.ts — it is derived from ChatArea's imports, and R9-1 lived in a file nobody had typed in",
  );
}
if (files.length <= 30) {
  problems.push(
    `the walk reached only ${files.length} files; it used to reach more than 30, so something stopped being followed`,
  );
}

// 4. Both directions at once: a NEW landing point with no verdict, a count that
//    grew, and a register row describing code that no longer exists all read out
//    of the same comparison.
const found = scan(files);
const registered = new Map(REGISTRY.map((r) => [`${r.file} | ${r.kind}`, r.count]));
const rows = (m) => [...m].map(([k, n]) => `${k} | ${n}`).sort();
const foundRows = rows(found);
const regRows = rows(registered);
const missing = foundRows.filter((r) => !regRows.includes(r));
const stale = regRows.filter((r) => !foundRows.includes(r));
if (missing.length > 0) {
  problems.push(
    `landing points in the code that the register does not match:\n${missing.map((r) => `    + ${r}`).join("\n")}`,
  );
}
if (stale.length > 0) {
  problems.push(
    `register rows that no longer match the code:\n${stale.map((r) => `    - ${r}`).join("\n")}`,
  );
}

if (problems.length > 0) {
  console.error("[async-landing] FAIL — the chat surface's async census is out of date.");
  console.error(
    "Each row answers: when this callback commits, can the screen have moved on — and what stops the write from landing in the wrong place?",
  );
  for (const p of problems) console.error(`  ${p}`);
  console.error(
    "Fix by updating REGISTRY in frontend/scripts/check-async-landing-points.mjs, with a verdict for anything new.",
  );
  process.exit(1);
}

console.log(
  `[async-landing] ok — ${files.length} files walked from ChatArea, ${found.size} (file, shape) pairs, all on the register`,
);
