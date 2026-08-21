// hooks/useQuotedMessages.ts — resolve the messages a thread QUOTES but does
// not currently hold.
//
// T-4e95 ships the reply link as an ID AND NOTHING ELSE (owner ruling): the
// quoted sender and text are NOT copied onto the reply, because that text
// already exists under its own id and a copy beside every reply is a second
// place for the same sentence to live. The consequence is this hook. A thread
// renders the quote from the messages it already has; when the quoted message
// has scrolled out of the loaded window, it is read back by id.
//
// 🔴 A MISS IS A REAL ANSWER, NOT A RETRY. `null` in the returned map means "we
// asked and did not get it" — the row then shows the honest "an earlier
// message" label rather than a spinner that never resolves. Ids that resolved,
// ids that missed, and ids never asked for are three different states and the
// map keeps them apart: present-with-a-message, present-and-null, absent.

import { useEffect, useRef, useState } from "react";
import { api } from "../api";
import { ApiError } from "../api/errors";
import { createDeltaSink } from "../lib/deltaSink";
import type { ChatMessage } from "../api/adapter";

/** The server refuses more than 20 ids per by-ids call, so batch to that. */
const BY_IDS_MAX = 20;

// The SSE topics that count as "the next event" for the debt below. Same pair
// as useChat's own CHAT_TOPICS, and deliberately NOT widened: a `monitoring` or
// `task` delta is not this surface's event, debt or no debt — the outer topic
// gate stays exactly as narrow as the precedent's (T-929f).
const QUOTE_TOPICS = new Set(["chat", "chat_read"]);

/**
 * Resolve `ids` that `have` does not already carry, one batch per new set.
 *
 * Returns the fetched-quote map only — callers read `have` first and fall back
 * to this. An id already resolved, or already ANSWERED with a miss, is never
 * asked for again, so a thread that re-renders on every SSE delta does not
 * re-issue the same read. The one exception is T-4e95's debt (see `staleRef`):
 * an id whose miss came from a blip rather than from an answer is un-asked
 * ONCE when the next relevant burst arrives.
 */
export function useQuotedMessages(
  ids: string[],
  have: Map<string, ChatMessage>,
): Map<string, ChatMessage | null> {
  const [fetched, setFetched] = useState<Map<string, ChatMessage | null>>(
    () => new Map(),
  );
  // Ids already asked for (resolved OR missed OR in flight). Kept in a ref so
  // asking does not itself re-run the effect.
  const askedRef = useRef<Set<string>>(new Set());
  // Ids that have already had their ONE retry after a transient failure. Bounds
  // the retry below to a single extra attempt per id, so a server that is down
  // costs one repeat and not a loop.
  const retriedRef = useRef<Set<string>>(new Set());
  // 🔴 "THE QUOTE LINE LIES FOREVER AFTER A SUSTAINED OUTAGE" (T-4e95). The one
  // retry above bounds the cost of a down server, and that bound was the bug:
  // TWO failures settled the id as `null` — a SETTLED miss — and nothing ever
  // revisited it. Measured on the real thing, not reasoned: `attempts under a
  // sustained outage: 2`, then `[after-room-switch] quoteBody="較早的一則訊息"`
  // with `attempts after room switch: 2 (was 2) — re-asked=false`. Changing
  // rooms does not help because `askedRef`/`fetched` never expire AND OfficePage
  // mounts <ChatArea> without a key, so this hook does not remount when the
  // owner switches who they are talking to. Only a full page reload cleared it.
  //
  // The fix is the owner's ruling of 2026-08-20, verbatim: 「不要重試，只要
  // 『標起來、下一個事件來就補』就好（改動更小）」. NO retry loop, NO backoff,
  // NO timer — the retry count above is untouched. This set is the mark: ids
  // whose miss came from a BLIP rather than from an answer. A relevant SSE burst
  // un-asks exactly those ids once, and the ordinary path re-reads them.
  //
  // A 4xx never lands here. The all-or-nothing 404 an unknown id causes IS an
  // answer, and re-asking an answer on every chat delta would be a poll.
  //
  // ⚠️ THE GAP THIS LEAVES OPEN, VERBATIM AND ON PURPOSE:
  // 「下一個事件來就補」意味著:如果那條線之後再也沒有任何事件,就還是不會補。
  // If no further chat / chat_read delta ever arrives on this connection, the
  // row keeps saying 「較早的一則訊息」 until something remounts or the page
  // reloads. That residual is a KNOWN, ACCEPTED trade made by the owner in
  // exchange for a smaller change. Do not read this as "the lying quote line is
  // fixed"; read it as "the line now self-heals on the next event instead of
  // never".
  //
  // ⚠️ StrictMode: this ref is only ever written from the fetch's outcome and
  // from the sink — NEVER from a cleanup. A ref written only on the way out gets
  // stuck off forever under setup→cleanup→setup, which is how the precedent
  // (fa952c5d) broke itself once; `mountedRef` below is the one that needs the
  // setup-body write, and it has one.
  const staleRef = useRef<Set<string>>(new Set());
  // Bumped when a transient failure un-asks a batch. It is part of the effect
  // key ON PURPOSE: un-asking alone changes no state, so without this the very
  // next render can recompute an IDENTICAL key, React sees unchanged deps, and
  // the retry we just enabled never fires.
  const [attempt, setAttempt] = useState(0);

  const wanted = ids.filter((id) => !have.has(id) && !askedRef.current.has(id));
  // A stable key so the effect fires on the SET of wanted ids, not on the array
  // identity a render creates fresh every time.
  const wantedKey = wanted.length === 0 ? "" : `${attempt}|${wanted.join(",")}`;

  // Mounted, NOT "these deps are still current". The difference is the whole of
  // T-4e95 review B1: marking ids asked does not re-render, so the very next
  // render (a keystroke in the composer is enough) recomputes `wantedKey` as ""
  // — the deps changed, React runs the PREVIOUS effect's cleanup, and a cleanup
  // that cancelled the in-flight read threw the answer away. The ids were
  // already in `askedRef`, so nothing ever asked again and the quote sat at "…"
  // forever, which is precisely the "spinner that never resolves" this file's
  // header promises it does not do. `askedRef` is what prevents duplicate
  // requests; the only thing left worth guarding is writing state after unmount.
  //
  // 🔴 THE SETUP BODY IS LOAD-BEARING, not ceremony. <StrictMode> (main.tsx)
  // double-invokes an effect in dev: setup → cleanup → setup. A cleanup-only
  // version sets this false on that first teardown and nothing ever sets it
  // back, so from mount onwards EVERY read is discarded and the quote sits at
  // "…" — the exact symptom B1 was fixed to remove, reintroduced in dev only
  // (production does not double-invoke, so it would have shipped looking fine
  // and been broken for everyone developing it). Found by review, reproduced
  // both ways before and after.
  //
  // It has a witness now, which it did not at first: reverting this line left
  // all 2239 tests green, so the paragraph above was the only thing standing
  // between the bug and a re-introduction. ChatArea.reply-to.test.tsx renders
  // the quote path inside <StrictMode> and goes red on exactly that revert.
  const mountedRef = useRef(true);
  useEffect(() => {
    mountedRef.current = true;
    return () => {
      mountedRef.current = false;
    };
  }, []);

  useEffect(() => {
    if (wantedKey === "") return;
    const batch = wantedKey.split("|")[1].split(",").slice(0, BY_IDS_MAX);
    for (const id of batch) askedRef.current.add(id);
    void (async () => {
      let rows: ChatMessage[] = [];
      // Did this batch end as a BLIP (a miss we have no answer for) rather than
      // as an answer? Only a blip is worth marking; see staleRef.
      let blip = false;
      try {
        rows = await api.listChatByIds(batch);
      } catch (e) {
        // 🔴 A REFUSAL AND A BLIP ARE NOT THE SAME ANSWER, and this used to
        // treat them as one. `null` in the map is a SETTLED state — "we asked
        // and it is not there" — and writing it on any throw meant one dropped
        // connection made every quote in that batch say 「較早的一則訊息」 for
        // the rest of the session: `askedRef` never forgets, and OfficePage
        // mounts <ChatArea> without a key, so the hook does not even remount
        // when the owner changes rooms. Only a full page reload cleared it.
        // The paragraph that used to be here argued a throw "says nothing
        // about the other ids" and then recorded a definitive negative for all
        // of them — it contradicted itself in three lines.
        //
        // ⚠️ THE RETRY IS IMMEDIATE, AND THAT IS ITS LIMIT. There is no backoff:
        // the extra attempt goes out on the next render, so a blip measured at
        // more than a few tens of milliseconds is already spent by the time the
        // server is back (a reviewer clocked the whole budget burning in 56ms).
        // What this buys is the common case — one dropped request — not an
        // outage. Said out loud because the previous version of this comment
        // could be read as promising recovery, and a timer here would be a new
        // moving part in code that has already shipped two effect-ordering bugs.
        //
        // ALL OR NOTHING is still true, and it is what makes 4xx definitive:
        // one unknown id refuses the WHOLE call with a 404, and that refusal
        // will not change on a retry. Anything else — a 5xx, a dropped socket,
        // an offline laptop — is a blip, and a blip earns exactly ONE more go
        // (see retriedRef). Not zero, because the label is a lie until then;
        // not unbounded, because a server that is down would spin.
        const definitive = e instanceof ApiError && e.status >= 400 && e.status < 500;
        const fresh = batch.filter((id) => !retriedRef.current.has(id));
        if (!definitive && fresh.length > 0) {
          for (const id of fresh) {
            retriedRef.current.add(id);
            askedRef.current.delete(id);
          }
          // 🔴 THE REST OF THE BATCH STILL NEEDS AN ANSWER. A batch can hold
          // both kinds at once — an id that has already had its one retry, next
          // to one that has not — and the first version of this branch handed
          // the retry to the fresh ones and then simply returned. The spent
          // ones were left in `askedRef` (so never asked again) and absent from
          // `fetched` (so never settled), and absent renders as 「…」: the
          // spinner that never resolves, which this file's header opens by
          // promising it does not do. Found by review with the sequence
          // [A] → [A,B] → [B]; it needs a newly-quoted id to arrive on the very
          // render the retry triggers, which is narrow but real (an SSE delta,
          // or scrolling up for history, landing on that frame).
          const spent = batch.filter((id) => retriedRef.current.has(id) && !fresh.includes(id));
          // 🔴 THE DEBT WRITE GOES BELOW THIS GUARD, NEVER ABOVE IT. `staleRef`
          // outlives the effect instance that writes it, and the precedent
          // shipped exactly this bug once by guarding only its `.then` arm and
          // letting the `.catch` arm mark debt for a torn-down instance
          // (fa952c5d, second commit). The narrow "writes the debt onto a
          // SUCCESSOR" version of that is not reachable here — the flag that
          // gates it is keyed to the COMPONENT (`mountedRef`, `[]` deps), and
          // once it is false this component is gone and its refs with it, so
          // there is no successor sharing this Set. What the guard does buy is
          // the plain one: an unmounted hook writes no state and no debt.
          if (!mountedRef.current) return;
          if (spent.length > 0) {
            // These ids are about to be SETTLED as a miss with no answer behind
            // them — mark, do not retry. The sink below un-asks them once, on
            // the next relevant burst.
            for (const id of spent) staleRef.current.add(id);
            setFetched((prev) => {
              const next = new Map(prev);
              for (const id of spent) next.set(id, null);
              return next;
            });
          }
          setAttempt((a) => a + 1);
          return;
        }
        // A definitive refusal is an ANSWER; anything else that reaches here is
        // an id that has spent its one retry and still has nothing behind it.
        blip = !definitive;
        rows = [];
      }
      if (!mountedRef.current) return;
      // Landed OR definitively refused ⇒ whatever we owed on these ids is paid
      // off. A blip ⇒ mark, and let the sink pay it on the next event.
      for (const id of batch) {
        if (blip) staleRef.current.add(id);
        else staleRef.current.delete(id);
      }
      const byId = new Map(rows.map((m) => [m.id, m]));
      setFetched((prev) => {
        const next = new Map(prev);
        for (const id of batch) next.set(id, byId.get(id) ?? null);
        return next;
      });
    })();
  }, [wantedKey]);

  // T-4e95: the debt's ONLY collector. No timer, no backoff, no retry loop —
  // the next relevant burst un-asks the marked ids exactly once and the ordinary
  // path above re-reads them.
  useEffect(() => {
    const unsubscribe = api.subscribeEvents(
      createDeltaSink((batch) => {
        if (![...batch.topics].some((t) => QUOTE_TOPICS.has(t))) return;
        // 🔴 NO DEBT ⇒ NO RE-READ. This is not an optimisation, it is the
        // contract: `askedRef` is what stops a thread that re-renders on every
        // SSE delta from re-issuing the same read (see this hook's header), and
        // deleting this line would turn every chat delta in the company into a
        // fresh by-ids call for every quote on screen. Only an id whose miss has
        // no answer behind it may be un-asked, and only once per mark.
        if (staleRef.current.size === 0) return;
        for (const id of staleRef.current) askedRef.current.delete(id);
        staleRef.current.clear();
        // Un-asking alone changes no state, so the very next render could
        // recompute an IDENTICAL `wantedKey`, React would see unchanged deps,
        // and the re-read we just enabled would never fire. Same reason
        // `attempt` exists for the retry above.
        setAttempt((a) => a + 1);
      })
    );
    return unsubscribe;
  }, []);

  return fetched;
}
