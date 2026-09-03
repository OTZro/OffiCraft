// lib/replyCardCache.ts — the reply cards a thread is ALLOWED to be committed
// with (T-48).
//
// 🔴 THE DEFECT, MEASURED. A 請示卡 rides the chat stream as an ordinary message
// carrying only `meta.reply_card_id`; `ChatReplyCard` then fetches the card
// itself. So the message list lands FIRST and the cards land LATER — and a
// WAITING card is the one that grows when it lands (options, chips, composer),
// while answered/expired ones mount collapsed and never fetch at all. A waiting
// card sitting ABOVE a scroll target therefore pushes that target down after the
// jump has already landed on it: measured +254px at 1280 wide. (At 390 the
// browser's own scroll anchoring absorbs it, which is why a 390-only guard
// proves nothing — see visual-guards/chat-jump-card-shift.ct.spec.tsx.)
//
// This module is HALF the fix. It makes a card's value available on the FIRST
// frame the row is painted; `lib/threadCommit.ts` is the other half — the choke
// point that makes committing messages without first awaiting `prefillWaitingCards`
// unwritable. Neither alone is sufficient: a cache nobody waits for is still
// empty at commit time, and an await whose result nothing reads pays the round
// trip for nothing.
//
// 🔴 WHAT IT DELIBERATELY DOES NOT PREFETCH. Answered and expired cards. That is
// the owner rule 已回覆卡預設不載 (see ChatReplyCard's `initialStatus`): those
// mount COLLAPSED and fire zero `getReplyCard`s, so a chat history of dozens of
// settled cards costs nothing. Prefetching them would re-introduce exactly the
// request storm that rule removed, to fix a shift they do not cause — a
// collapsed stub is already its final height.

import type { ChatMessage, ReplyCard } from "../api/adapter";

// Keyed by CARD ID, which is globally unique (`rc-…`) and names the same card in
// every room — the same property that makes useWorkerCodenames' cache safe. A
// card is small and immutable-per-status; the table is bounded in practice by
// how many cards one session actually looks at.
const cards = new Map<string, ReplyCard>();

// 🔴 THE TWO TABLES THAT KEEP A BAD ID FROM COSTING FOREVER (T-48, review F4).
// `cards` alone has no memory of failure: an id whose GET rejected, or whose
// GET was still in the air when the deadline fired, never lands in it — so the
// NEXT commit asks for it again, waits the full deadline again, and leaves a
// second un-cancelled GET behind it (the AbortController bounds the WAIT, not
// the REQUEST — see below). A room whose one card 404s therefore paid 1500ms on
// every commit, and its in-flight GETs accumulated without bound.
//
//   · `inFlight` — one promise per id. A second commit naming the same id
//     ATTACHES to that promise instead of issuing a second GET, so the number
//     of outstanding requests is bounded by the number of distinct ids, not by
//     the number of commits.
//   · `abandoned` — ids whose fetch has actually FAILED. They are not asked for
//     again for the life of the page: the card simply loads the way it did
//     before this module existed (the row grows a frame late), which is the
//     defect we started from and strictly better than holding every future
//     commit for 1500ms. A DEADLINE is NOT a failure and does not land here —
//     that request is still in the air and may yet fill the cache.
const inFlight = new Map<string, Promise<void>>();
const abandoned = new Set<string>();

// 🔴 THE LIVENESS DEADLINE, AND WHY IT IS NOT OPTIONAL. Every commit point in
// `useChat` now awaits this function, so a `getReplyCard` that never answers
// would hold the WHOLE THREAD off the screen — an empty room, which is this
// feature's worst failure and the exact shape T-48 has already shipped four
// times. The deadline turns that into "the card fills in a frame late", which is
// the defect we started from and strictly better than a blank thread.
//
// MEASURED, not guessed (isolated e2e server on :8791, 2026-09-04):
//   · GET /api/reply-cards/{id}, 600 samples: p50 0.7ms, p95 1.4ms, max 2.5ms
//   · a whole 30-card page fetched concurrently, 20 runs: p50 3.3ms, max 4.2ms
// 1500ms is ~357× the measured worst case for a full page. The margin is that
// large on purpose: the number is a LIVENESS floor, not a latency budget, and
// the measurement is loopback + local SQLite, which is the fastest this call
// will ever be. Nothing reads it as a threshold and no behaviour changes at
// 1499ms; the only thing it decides is when a stuck fetch stops holding the
// thread hostage.
export const REPLY_CARD_PREFILL_DEADLINE_MS = 1500;

/** The card, if this session has already read it. */
export function getCachedReplyCard(id: string): ReplyCard | undefined {
  return cards.get(id);
}

/** Record the newest known truth for a card.
 *
 * Called by `ChatReplyCard` from BOTH its read path and its write paths
 * (`commitCard`, which adopts an answer/re-answer's own response). That second
 * half is not a nicety: without it a remount — a jump, a scroll that recreates
 * the row, a conversation revisit — would seed itself from a PRE-ANSWER copy and
 * put the option chips back over an answered card, which is the very shape
 * ChatReplyCard's `readGenRef` exists to prevent one layer down. */
export function putReplyCard(card: ReplyCard): void {
  cards.set(card.id, card);
}

/** Forget everything. Tests only — the product never invalidates this table,
 * because a card id names one card for the life of the page. */
export function resetReplyCardCache(): void {
  cards.clear();
  inFlight.clear();
  abandoned.clear();
}

/** Which cards this page of messages needs BEFORE it may be painted: the
 * WAITING ones (they are the only ones that grow on arrival) that are neither
 * already in hand nor already given up on. Exported for the guards, which must
 * be able to say what the denominator was.
 *
 * An id may still be IN FLIGHT and appear here — that is correct, and `prefill`
 * attaches to the existing request rather than starting a second one. */
export function pendingWaitingCardIds(messages: ChatMessage[]): string[] {
  const ids: string[] = [];
  const seen = new Set<string>();
  for (const m of messages) {
    if (m.replyCardStatus !== "waiting") continue;
    const id = m.replyCardId;
    if (!id || cards.has(id) || abandoned.has(id) || seen.has(id)) continue;
    seen.add(id);
    ids.push(id);
  }
  return ids;
}

/** Have every WAITING card on this page in hand, or give up after the deadline.
 *
 * Fetches concurrently and settles rather than races: one card's 404 must not
 * withhold the other twenty-nine, and it must not reject either — a rejection
 * here would propagate into a commit point and lose the whole page.
 *
 * ⚠️ THE ABORT SIGNAL BOUNDS THE WAIT, NOT THE REQUEST. `Api.getReplyCard(id)`
 * takes no `AbortSignal` (api/adapter.ts), so the in-flight GETs are not
 * cancelled — they simply stop being waited on, and if they land later they
 * still populate the cache for the next render, which is free. Saying this
 * plainly rather than letting the AbortController imply cancellation: the
 * controller is the deadline token, and that is all it is. What keeps those
 * un-cancelled requests from PILING UP is `inFlight`, not the controller: a
 * commit naming an id that is already in the air waits on the same promise. */
export async function prefillWaitingCards(
  messages: ChatMessage[],
): Promise<void> {
  const ids = pendingWaitingCardIds(messages);
  if (ids.length === 0) return;
  // 🔴 THE API CLIENT IS REACHED LAZILY, AND THAT IS NOT A STYLE CHOICE.
  // `src/test/setup.ts` imports this module (to drop the table between tests),
  // and a setup file runs BEFORE a test file's own `vi.mock("../api")` is
  // registered — so a STATIC `import { api } from "../api"` here would pull the
  // api layer into the module registry first and every test file's api mock
  // would silently stop applying to the prefill. Measured: the mocked
  // `getReplyCard` was bypassed and the REAL mock backend answered 404, which
  // looks exactly like "the prefill did not run". That hazard is written down in
  // setup.ts's own header; this is what obeying it looks like.
  const { api } = await import("../api");
  const ac = new AbortController();
  const timer = setTimeout(
    () => ac.abort(),
    REPLY_CARD_PREFILL_DEADLINE_MS,
  );
  const all = Promise.allSettled(
    ids.map((id) => {
      const already = inFlight.get(id);
      if (already) return already;
      const read = api
        .getReplyCard(id)
        .then((card) => {
          cards.set(card.id, card);
        })
        .catch((e) => {
          // Give up on this id for the life of the page. Recorded BEFORE the
          // rejection is re-thrown so `allSettled` still sees a settled
          // promise and the other cards on the page are unaffected.
          abandoned.add(id);
          throw e;
        })
        .finally(() => {
          inFlight.delete(id);
        });
      inFlight.set(id, read);
      return read;
    }),
  );
  const deadline = new Promise<void>((resolve) => {
    ac.signal.addEventListener("abort", () => resolve(), { once: true });
  });
  try {
    await Promise.race([all.then(() => undefined), deadline]);
  } finally {
    clearTimeout(timer);
  }
}
