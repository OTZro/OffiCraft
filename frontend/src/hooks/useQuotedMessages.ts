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
  // Ids that have already had their ONE immediate retry after a transient
  // failure. Never cleared, so the immediate retry below fires at most once per
  // id for the life of the hook.
  //
  // ⚠️ THAT IS A BOUND ON THE *IMMEDIATE* RETRY, NOT ON TOTAL WORK, and the
  // sentence here used to overclaim it as "a server that is down costs one
  // repeat and not a loop". Since `staleRef` below exists, a down server costs
  // one read PER BURST that releases the debt: the collector un-asks the marked
  // ids, they go through the ordinary path, the call fails, `fresh` is empty
  // (they are all in this set), so they are re-marked and settled again — one
  // call, no second immediate go. Still not a loop, because nothing here
  // triggers the next attempt: only an inbound event does.
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
  //
  // "Event" is wider than real traffic, though, and the gap is narrower than
  // that sentence alone reads. `SSE_RESYNC_TOPICS` (api/http.ts) contains both
  // `chat` and `chat_read`, and `resyncAll()` fans ONE SYNTHETIC DELTA PER
  // TOPIC to every subscriber — on every SUBSEQUENT EventSource open (a genuine
  // reconnect; the first open is skipped) and on every return to the foreground
  // (`visibilitychange` → visible, and `window` focus). So the debt is also
  // collected by dropping and re-establishing the stream, or by switching away
  // from the tab/app and back, not only by someone actually sending a message.
  //
  // What is left is the case where NONE of those happen: nobody speaks, the
  // connection never drops, and the tab is never left. Then the
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
          // (fa952c5d, second commit).
          //
          // ⚠️ A PREVIOUS VERSION OF THIS PARAGRAPH ARGUED THE SUCCESSOR CASE
          // AWAY AND WAS WRONG: it claimed that once `mountedRef` is false the
          // component is gone and its refs with it, so no successor shares this
          // Set. Under <StrictMode> that is false — during setup→cleanup→setup
          // the flag reads false while the component is very much alive, and
          // `staleRef` is the SAME Set object across all three phases, so the
          // successor is this component itself. The conclusion (debt write goes
          // below the guard) survives; the reason does not. The reason that
          // does hold is simply that a write we cannot see the outcome of is a
          // write we should not make: past the guard, an unmounted — or
          // mid-StrictMode-teardown — instance settles no state, so marking
          // debt for state it will never settle would leave a mark with no
          // matching `null` in `fetched` behind it.
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
      // A blip ⇒ mark, and let the sink pay it on the next event.
      //
      // There is deliberately NO `else staleRef.current.delete(id)` here, and
      // there used to be. It could never delete anything: every id that reaches
      // `staleRef` is also in `askedRef` (the batch adds it before the read and
      // neither the spent nor the blip arm removes it), and the ONE thing that
      // removes an id from `askedRef` — the sink below — does `staleRef.clear()`
      // in the very next statement. So no id can be both marked and wanted at
      // the same time, and a batch id is therefore never in `staleRef` when we
      // get here. It was dead code that no test could see, and reviewers kept
      // reading it as proof that a landed read cancels its own debt; the mark is
      // released by the sink, which is where the clearing genuinely happens.
      if (blip) for (const id of batch) staleRef.current.add(id);
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
        // 🔴 THIS LINE IS THE WHOLE FIX, not bookkeeping. The two statements
        // above are a ref delete and a Set clear — NEITHER RENDERS. Without
        // this bump nothing recomputes `wantedKey`, the re-read never goes out,
        // and the debt has already been thrown away, so every later event hits
        // the `size === 0` return above: the quote line goes back to lying
        // forever. Same reason `attempt` exists for the retry above.
        //
        // ⚠️ THE REAL PAGE HIDES THIS BY ACCIDENT. `useChat` refetches on the
        // same chat delta and repaints <ChatArea>, so a build without this line
        // still LOOKS self-healing in the browser — borrowed repaints, not this
        // hook's own. That is why the guardrail lives in a test that does NOT
        // call rerender() between the emit and the wait
        // (useQuotedMessages.test.ts, "re-asks after a sustained outage…"):
        // a hand-written repaint there makes deleting this line invisible, and
        // for one review round it was.
        setAttempt((a) => a + 1);
      })
    );
    return unsubscribe;
  }, []);

  return fetched;
}
