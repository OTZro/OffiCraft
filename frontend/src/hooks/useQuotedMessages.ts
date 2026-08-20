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
import type { ChatMessage } from "../api/adapter";

/** The server refuses more than 20 ids per by-ids call, so batch to that. */
const BY_IDS_MAX = 20;

/**
 * Resolve `ids` that `have` does not already carry, one batch per new set.
 *
 * Returns the fetched-quote map only — callers read `have` first and fall back
 * to this. Nothing is refetched: an id already resolved (or already missed)
 * is never asked for again, so a thread that re-renders on every SSE delta
 * does not re-issue the same read.
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
          if (!mountedRef.current) return;
          if (spent.length > 0) {
            setFetched((prev) => {
              const next = new Map(prev);
              for (const id of spent) next.set(id, null);
              return next;
            });
          }
          setAttempt((a) => a + 1);
          return;
        }
        rows = [];
      }
      if (!mountedRef.current) return;
      const byId = new Map(rows.map((m) => [m.id, m]));
      setFetched((prev) => {
        const next = new Map(prev);
        for (const id of batch) next.set(id, byId.get(id) ?? null);
        return next;
      });
    })();
  }, [wantedKey]);

  return fetched;
}
