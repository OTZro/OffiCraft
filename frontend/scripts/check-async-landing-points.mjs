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
// 🔴 IT CARRIES TWO MORE CENSUSES, BECAUSE IT ALREADY WALKS THE GRAPH (T-48,
// R14-3.1 / R14-1.6). Both are the same shape of question — "is this claim still
// true of the WHOLE chat surface, not of the one file somebody remembered?" —
// and both were being kept by prose:
//
//   * MODULE_STATE. `key={peerId}` deleted per-conversation component state, so
//     the only place the twelve-instance defect family can come back is state
//     that lives OUTSIDE a component. `chatDraftStore.ts` says in its header
//     that everything surviving a room switch now lives there, and
//     `chatDraftStore.test.ts` checks that sentence against ONE file — the file
//     that already obeys it. The instance that actually happened was
//     `liveComposers`, a second module-level per-room table grown in
//     `ChatArea.tsx`, and a one-file census cannot see that. This one reads
//     every file in the walk.
//   * useQuotedMessageOverlay's SINGLE CALLER. The full-screen overlay gave up
//     its own visit stamp on the grounds that ChatArea is its only caller and
//     ChatArea is keyed. That was true, checked by hand, and guarded by a
//     comment: a second caller whose own key is a card id would put room A's
//     message over room B, which is R8-3's original shape.
//
// Run: `npm run lint:async-landing` (also wired into bin/ci.sh).

import { existsSync, readFileSync, readdirSync } from "node:fs";
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

/** MODULE-LEVEL MUTABLE STATE, across the whole walk (T-48, R14-3.1).
 *
 * A module-level table outlives every mount, so it is the one place a value can
 * still cross from one conversation into another now that `key={peerId}` takes
 * the component state with the room. Each row says WHAT KEYS IT — and "keyed by
 * peer" is the only answer that makes a per-conversation table safe. The
 * deleted `liveComposers` (a second peer-keyed table, in `ChatArea.tsx`, not in
 * the store) is the shape this exists to catch coming back, under any name and
 * in any file the chat surface imports.
 *
 * WHAT COUNTS AS STATE: a top-level `let`/`var`, and a top-level
 * `new Map/Set/WeakMap/WeakSet()` constructed EMPTY — something written to
 * later. A container built FROM a constant (`new Set(THEME_TOKENS)`) is a
 * lookup table, not state, and is deliberately not registered; construct one
 * empty and it joins the census. */
const MODULE_STATE = [
  { file: "hooks/useAttachmentStaging.ts", name: "pendingAttachmentSeq", verdict: "a monotonic key mint for staged rows; carries no per-conversation meaning and is never read back" },
  { file: "hooks/useWorkerCodenames.ts", name: "cache", verdict: "keyed by the globally-unique `ow-` worker id, which names one worker in every room alike" },
  { file: "hooks/useWorkerCodenames.ts", name: "inflight", verdict: "the same `ow-` ids, de-duplicating requests; not per conversation" },
  { file: "hooks/useWorkerCodenames.ts", name: "listeners", verdict: "repaint callbacks for that global cache; each unsubscribes on unmount" },
  { file: "lib/chatDraftStore.ts", name: "drafts", verdict: "keyed by PEER — the one table that survives a room switch on purpose, because a draft is what the owner has composed and not yet sent" },
  { file: "lib/chatDraftStore.ts", name: "attachErrors", verdict: "keyed by PEER, and dropped when the office page unmounts (R14-2.1) so a refusal cannot outlive the surface that raised it" },
  { file: "lib/chatDraftStore.ts", name: "listeners", verdict: "keyed by PEER; a write notifies only that room's subscribers" },
  { file: "lib/escapeLayers.ts", name: "listening", verdict: "one boolean saying whether the shared keydown listener is attached; layers deregister on unmount" },
  { file: "lib/sharedSnapshot.ts", name: "registry", verdict: "the set of global settings snapshots to reset on auth change; global by definition, not per conversation" },
];

/** The ONE caller `useQuotedMessageOverlay` is allowed to have (T-48, R14-1.6).
 * The hook dropped its own visit stamp because `ChatArea` is keyed, so its
 * state dies with the room. That reasoning is about the CALLER, not the hook: a
 * second caller mounted under a card id (`ChatReplyCard`, `ReplyComposer`) does
 * not unmount on a room switch, and would paint room A's message full-screen
 * over room B. Adding a caller here is a decision to re-check that, not a
 * formality. */
const QUOTED_OVERLAY_CALLERS = ["components/ChatArea.tsx"];

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

/** Top-level mutable state declared in `file`. Top level = column 0: the same
 * cheap, deliberate reading `chatDraftStore.test.ts` used, and nesting a table
 * inside a function makes it per-call rather than per-module anyway. */
function scanModuleState(file) {
  const names = [];
  for (const line of readFileSync(file, "utf8").split("\n")) {
    if (/^\s*(\/\/|\*|\/\*)/.test(line)) continue;
    const mutable = line.match(/^(?:export\s+)?(?:let|var)\s+([A-Za-z_$][\w$]*)/);
    if (mutable) {
      names.push(mutable[1]);
      continue;
    }
    const container = line.match(
      /^(?:export\s+)?const\s+([A-Za-z_$][\w$]*)\s*(?::[^=]*)?=\s*new (?:Map|Set|WeakMap|WeakSet)\b.*\(\s*\)\s*;?\s*$/,
    );
    if (container) names.push(container[1]);
  }
  return names;
}

/** Every source file under the scanned root — NOT just the walk. A new caller of
 * the quoted-message overlay is exactly the file nobody has imported from
 * ChatArea yet, so the walk is the wrong population for that one question. */
function allSourceFiles(dir) {
  const out = [];
  for (const e of readdirSync(dir, { withFileTypes: true })) {
    const full = join(dir, e.name);
    if (e.isDirectory()) {
      out.push(...allSourceFiles(full));
      continue;
    }
    if (!/\.tsx?$/.test(e.name) || /\.test\.tsx?$/.test(e.name)) continue;
    out.push(full);
  }
  return out;
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

// 5. Module-level mutable state, both directions, over the SAME walk. A table
//    that outlives every mount is the only place left for one room's value to
//    reach another; an unregistered one is an unanswered question about which
//    room it belongs to.
const foundState = [];
for (const file of files) {
  for (const name of scanModuleState(file)) {
    foundState.push(`${file.slice(SRC.length + 1)} | ${name}`);
  }
}
const regState = MODULE_STATE.map((r) => `${r.file} | ${r.name}`);
for (const r of MODULE_STATE) {
  if (r.verdict.trim().length < 20) {
    problems.push(`${r.file} | ${r.name}: module state with nothing said about what keys it is not registered`);
  }
}
const newState = foundState.filter((r) => !regState.includes(r)).sort();
const goneState = regState.filter((r) => !foundState.includes(r)).sort();
if (newState.length > 0) {
  problems.push(
    `module-level state the register does not know about — say what keys it, and if that key is not the PEER say why one room's value may reach another:\n${newState.map((r) => `    + ${r}`).join("\n")}`,
  );
}
if (goneState.length > 0) {
  problems.push(
    `registered module-level state that no longer exists:\n${goneState.map((r) => `    - ${r}`).join("\n")}`,
  );
}

// 6. The quoted-message overlay's caller list, over the WHOLE tree. Its state
//    is protected by its caller's key and by nothing else.
const overlayCallers = [];
for (const file of allSourceFiles(SRC)) {
  const rel = file.slice(SRC.length + 1).split("\\").join("/");
  if (rel === "hooks/useQuotedMessageOverlay.tsx") continue;
  const code = readFileSync(file, "utf8")
    .split("\n")
    .filter((l) => !/^\s*(\/\/|\*|\/\*)/.test(l))
    .join("\n");
  if (code.includes("useQuotedMessageOverlay(")) overlayCallers.push(rel);
}
overlayCallers.sort();
if (
  JSON.stringify(overlayCallers) !== JSON.stringify([...QUOTED_OVERLAY_CALLERS].sort())
) {
  problems.push(
    `useQuotedMessageOverlay's callers changed:\n    calling:    ${overlayCallers.join(", ") || "(nobody)"}\n    registered: ${QUOTED_OVERLAY_CALLERS.join(", ")}\n  The overlay keeps NO room stamp of its own — it relies on its caller being unmounted by a room switch. A caller keyed on anything but the peer paints one room's message over another (R8-3).`,
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
  `[async-landing] ok — ${files.length} files walked from ChatArea, ${found.size} (file, shape) pairs and ${foundState.length} module-level state declarations on the register, ${overlayCallers.length} quoted-overlay caller(s)`,
);
