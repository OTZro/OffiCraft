// hooks/useReplyCards.tsx — the 等我回覆 page's data AND the nav badge's waiting
// count, from ONE shared source (T-e862 同源化). The WAITING list is the
// always-live pane (mount + every "reply_card" SSE delta), fetched alongside
// the cheap counts. The HANDLED list (answered + expired, merged newest-first
// by their handled stamp) is DEFERRED (owner 已回覆卡預設不載 / 收合狀態下不
// fetch): the collapsed 近期已處理 pane shows its 「· N」 header from the
// counts alone (answered + expired), and the lists are pulled only when the
// owner expands the pane (loadHandled). Reconcile-by-refetch (contract B): a
// delta REFETCHES — the waiting list + counts always, and the handled lists
// only when currently loaded (expanded) — never merges an event payload. The
// answer/re-answer/expire actions do NOT refetch directly — see T-a3e4 step 8
// below.
//
// T-e862 (狀態競態修復):
//  ① REQUEST SEQUENCING. refetchWaiting/refetchHandled are fired concurrently
//     and out of order (a resolve + its own reply_card fan-out + a peer's new
//     card each kick one), each ending in a bare setWaiting(w). With no
//     ordering guard the LAST promise to resolve won — a late-arriving STALE
//     snapshot could clobber a newer, fuller one and silently drop a card
//     (badge said 2, list showed 1, until refresh). Each refetch now stamps a
//     monotonic generation id and only commits if it is still the latest —
//     late stale responses are dropped, killing the last-write-wins.
//  ② SAME SOURCE for badge + list + title. The nav badge (useReplyCardCount)
//     used to ride a SEPARATE count-endpoint fetch on a SEPARATE hook with its
//     OWN SSE subscription, so it and the list sat on two different snapshots
//     from two different instants — the structural crack behind "badge 2 /
//     list 1". The waiting list now lives in ONE app-wide provider; the badge
//     is literally waiting.length off that same authoritative array, and the
//     page title 「待回覆 · N」 reads the same length. One source, one
//     subscription — they cannot disagree.
//
// T-a3e4 step 8 (一次動作只重抓一輪):
//   ONE owner action used to cost TWO complete refetch rounds. The action path
//   (answer/reanswer/expire → refetchAfterAction) and the SSE handler each
//   fired their own refetch for the SAME write, because the server publishes
//   its `reply_card` delta for the owner's own write too. ①'s generation guard
//   dedupes the COMMIT, not the REQUEST — the loser's response was downloaded
//   in full and then thrown away. Measured against a real ocserverd over a
//   25-card waiting pane: one answered card = 48 per-card GETs (24 cards × 2)
//   and 100,952 B, against 25 GETs / 51,599 B for the same pane on mount.
//   An isolation control pinned the cause: a delta the cockpit did NOT cause
//   (someone else opening a card) produced exactly ONE round, so the second
//   round belonged to the local action path, not to a doubled stream.
//
//   ⇒ The actions no longer refetch. 🔴 But they DO reconcile: each action
//   ADOPTS the card its own write returned (`adoptWrite` below), which costs
//   zero requests. The earlier version of this note said the delta was "the
//   single reconcile trigger" for the action path too — that made the pane's
//   correctness depend on an OPTIONAL live event, and with the EventSource down
//   or one frame missed the server had accepted the answer while the pane (and
//   therefore the nav badge) still showed the card as waiting, sending the owner
//   back into it for a 409. Do NOT re-derive that as "the accepted trade": the
//   trade step 8 actually bought was one fewer ROUND, not a lost fallback.
//   The delta remains the reconcile trigger for every write this cockpit did NOT
//   make, and it is sufficient in BOTH adapters — this is the load-bearing fact, so
//   check it before touching either: the http adapter gets the delta from the
//   server (`publishReplyCard` runs AFTER the row is committed and BEFORE the
//   response is flushed, so a delta-triggered read can never precede the
//   write), and the mock fans its OWN `reply_card` topic from inside
//   answer/reanswer/expire (`emitTopic`, called synchronously after it mutates
//   the in-memory card). The old comment here claimed the direct refetch
//   existed "so the mock behaves identically" — that stopped being true when
//   the mock grew emitTopic, and the stale justification is what kept the
//   duplicate alive.
//   ⚠️ `refresh()` is NOT part of this and still refetches unconditionally: its
//   caller (a 409 answer — the card was already handled elsewhere) learned its
//   snapshot is stale from a write it did not make, so there is no delta of its
//   own on the way.
//
// NOTE (follow-up, still NOT in this change): api.listReplyCards is a
// non-atomic N+1 (a light index then a per-id hydrate) — the 25 GETs above are
// one round, not one request. A single snapshot can still be an internally
// skewed slice. The clean fix is an ATOMIC list endpoint (server returns
// full-enough rows in one shot); it is a frozen-wire change (spec first +
// owner sign-off, root CLAUDE.md §13), which is why it is not here.

import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useRef,
  useState,
  type ReactNode,
} from "react";
import type { ReplyCard, ReplyCardAnswerInput } from "../api/adapter";
import { api } from "../api";

/** A handled card's pane stamp: answeredTs on an answered card, expiredTs on
 * an expired one (each null on the other kind). */
function handledTs(c: ReplyCard): number {
  return c.status === "expired" ? (c.expiredTs ?? 0) : (c.answeredTs ?? 0);
}

interface UseReplyCards {
  /** Cards still waiting for the owner — server-ordered LONGEST-WAITING FIRST.
   * This IS the single authoritative waiting source: the nav badge counts its
   * length, the page renders it, the title reads its length. */
  waiting: ReplyCard[];
  /** Cards answered OR expired within the last 24h — merged, newest handled
   * first. EMPTY until `loadHandled()` is called (the pane is collapsed by
   * default). */
  handled: ReplyCard[];
  /** Recently-handled (24h) count from the cheap count endpoint (answered +
   * expired) — drives the collapsed 近期已處理 · N header (and its zero-hide)
   * WITHOUT the lists. */
  handledCount: number;
  /** True once the handled lists have actually been fetched (pane expanded). */
  handledLoaded: boolean;
  loading: boolean;
  /** True when the mount fetch REJECTED (500/network; 401 already bounced to
   * login) — so a failed load never masquerades as the ✓ empty state. */
  error: boolean;
  /** Pull the handled lists on demand (the owner expanded the pane). Idempotent
   * and safe to call repeatedly; a repeat just refreshes them. */
  loadHandled: () => void;
  /** Re-pull the panes on demand — the caller learned the local snapshot is
   * stale (T-4166: a 409 answer means the card is already handled or orphaned,
   * so it must stop rendering as if it still waits). */
  refresh: () => Promise<void>;
  /** Answer a WAITING card (the positive close). Resolving means the WRITE
   * landed AND its own response has been adopted — the card has left the waiting
   * pane by then, with or without the `reply_card` delta (see `adoptWrite`). The
   * other panes' full re-read still rides that delta (T-a3e4 step 8). */
  answer: (id: string, input: ReplyCardAnswerInput) => Promise<void>;
  /** Revise an ANSWERED card's answer (重新決定). Same resolve semantics as
   * `answer`. */
  reanswer: (id: string, input: ReplyCardAnswerInput) => Promise<void>;
  /** Mark a WAITING card expired (標為過期 — terminal, not an answer). Same
   * resolve semantics as `answer`. */
  expire: (id: string) => Promise<void>;
}

/** The one shared reply-cards state, driven by the app-wide provider. Both the
 * page (useReplyCards) and the nav badge (useReplyCardCount) read it, so they
 * are the SAME source and can never diverge. */
const ReplyCardsContext = createContext<UseReplyCards | null>(null);

/** The provider that owns the always-live waiting fetch (with request
 * sequencing) and the deferred handled fetch. Mounted app-wide (above the nav
 * badge AND the page) so both share one snapshot and one SSE subscription. */
export function ReplyCardsProvider({ children }: { children: ReactNode }) {
  const value = useReplyCardsState();
  return (
    <ReplyCardsContext.Provider value={value}>
      {children}
    </ReplyCardsContext.Provider>
  );
}

function useReplyCardsState(): UseReplyCards {
  const [waiting, setWaiting] = useState<ReplyCard[]>([]);
  const [handled, setHandled] = useState<ReplyCard[]>([]);
  const [handledCount, setHandledCount] = useState(0);
  const [handledLoaded, setHandledLoaded] = useState(false);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState(false);
  // Live "is the handled pane loaded" flag for the SSE closure (which depends
  // only on the stable refetchers and must not re-subscribe on each load).
  const handledLoadedRef = useRef(false);
  // ① Monotonic generation ids: every refetch takes a ticket on entry and only
  // commits its result if the ticket is still current. A late stale response
  // (an older refetch resolving AFTER a newer one) fails the check and is
  // dropped — this is what kills the last-write-wins that dropped cards.
  const waitingGenRef = useRef(0);
  const handledGenRef = useRef(0);
  // Live mirror of `waiting`, so adoptWrite below can compute the next array
  // WITHOUT taking `waiting` as a dependency (the action callbacks it feeds are
  // handed to the cards as props; a new identity on every snapshot is churn we
  // do not need). Written wherever `setWaiting` is.
  const waitingRef = useRef<ReplyCard[]>([]);

  // The always-live cheap fetch: the waiting list + the counts. Runs on mount
  // and on every reply_card delta.
  const refetchWaiting = useCallback(async () => {
    const gen = ++waitingGenRef.current;
    try {
      const [w, counts] = await Promise.all([
        api.listReplyCards("waiting"),
        api.getReplyCardCount(),
      ]);
      // Superseded by a newer refetch while we were in flight → drop this
      // (possibly stale) snapshot rather than clobber the fresher one.
      if (gen !== waitingGenRef.current) return;
      setWaiting(w);
      waitingRef.current = w;
      setHandledCount(counts.answered + counts.expired);
      setError(false);
    } catch (e) {
      // Only the latest attempt owns the error surface — a stale rejection must
      // not flip the page into its error state after a newer fetch succeeded.
      if (gen === waitingGenRef.current) setError(true);
      throw e;
    }
  }, []);

  // The deferred handled fetch: only ever runs once the pane is expanded
  // (loadHandled), then re-runs on deltas while it stays loaded.
  const refetchHandled = useCallback(async () => {
    const gen = ++handledGenRef.current;
    const [answered, expired] = await Promise.all([
      api.listReplyCards("answered"),
      api.listReplyCards("expired"),
    ]);
    // Same generation guard as the waiting list — drop a superseded snapshot.
    if (gen !== handledGenRef.current) return;
    setHandled(
      [...answered, ...expired].sort((a, b) => handledTs(b) - handledTs(a))
    );
    setHandledLoaded(true);
    handledLoadedRef.current = true;
  }, []);

  const loadHandled = useCallback(() => {
    refetchHandled().catch((e) =>
      console.warn("useReplyCards: handled load failed", e)
    );
  }, [refetchHandled]);

  useEffect(() => {
    let alive = true;

    refetchWaiting()
      // refetchWaiting owns its own (generation-guarded) error state; here we
      // only swallow the rejection to avoid an unhandled promise and clear the
      // initial loading flag.
      .catch((e) => console.warn("useReplyCards: initial load failed", e))
      .finally(() => {
        if (alive) setLoading(false);
      });

    const unsubscribe = api.subscribeEvents((topic) => {
      if (topic !== "reply_card") return;
      refetchWaiting().catch((e) =>
        console.warn("useReplyCards: SSE refetch failed", e)
      );
      // Keep the handled pane fresh only while it is actually loaded — a
      // collapsed (never-expanded) pane stays unfetched on deltas too.
      if (handledLoadedRef.current) {
        refetchHandled().catch((e) =>
          console.warn("useReplyCards: SSE handled refetch failed", e)
        );
      }
    });

    return () => {
      alive = false;
      unsubscribe();
    };
  }, [refetchWaiting, refetchHandled]);

  // The UNCONDITIONAL re-read, for a caller that learned its snapshot is stale
  // from a write it did not make (the 409 path). NOT used by the actions below
  // — see T-a3e4 step 8 in the header for why they must not re-read.
  // ADOPT-FROM-RESPONSE: the action path's own reconciliation, so correctness
  // never depends on an optional live event (T-a3e4 step 8 follow-up). Step 8
  // was right that the action must not spend a SECOND refetch round; it was
  // wrong to leave the `reply_card` delta as the ONLY reconciler. With the
  // EventSource down or one frame missed, the server had ACCEPTED the answer
  // while the pane kept rendering the card as waiting — the owner clicks it
  // again and eats a 409, and the nav badge (this array's length) stays wrong
  // until reconnect / foreground resync / reload.
  //
  // The write already answers with the fresh card (`answerReplyCard` /
  // `reanswerReplyCard` / `expireReplyCard` all return `ReplyCard`), so this
  // costs ZERO extra requests — step 8's one-round budget is untouched, and the
  // delta still drives the pane for everyone ELSE's writes.
  // ⚠️ This is an adoption of the SERVER's own response for ONE identified card,
  // not a merge of an SSE payload (contract B still holds: deltas refetch, never
  // merge). It deliberately does not re-order or add rows — a card it does not
  // already hold is left to the delta / next refetch.
  const adoptWrite = useCallback((card: ReplyCard) => {
    const terminal = card.status !== "waiting";
    const prev = waitingRef.current;
    if (prev.some((c) => c.id === card.id)) {
      const next = terminal
        ? prev.filter((c) => c.id !== card.id)
        : prev.map((c) => (c.id === card.id ? card : c));
      waitingRef.current = next;
      setWaiting(next);
      // It left the waiting pane, so the 近期已處理 header's count gained it.
      // (Only when it really WAS waiting — a 重新決定 revises an already-handled
      // card and must not re-count it.)
      if (terminal) setHandledCount((n) => n + 1);
    }
    // Keep the handled lists coherent too, but only while they are actually
    // loaded — a collapsed, never-expanded pane stays unfetched (the same gate
    // the SSE path respects).
    if (terminal && handledLoadedRef.current) {
      setHandled((prevHandled) =>
        [...prevHandled.filter((c) => c.id !== card.id), card].sort(
          (a, b) => handledTs(b) - handledTs(a)
        )
      );
    }
  }, []);

  const refresh = useCallback(async () => {
    await refetchWaiting();
    if (handledLoadedRef.current) await refetchHandled();
  }, [refetchWaiting, refetchHandled]);

  const answer = useCallback(
    async (id: string, input: ReplyCardAnswerInput) => {
      adoptWrite(await api.answerReplyCard(id, input));
    },
    [adoptWrite]
  );

  const reanswer = useCallback(
    async (id: string, input: ReplyCardAnswerInput) => {
      adoptWrite(await api.reanswerReplyCard(id, input));
    },
    [adoptWrite]
  );

  const expire = useCallback(
    async (id: string) => {
      adoptWrite(await api.expireReplyCard(id));
    },
    [adoptWrite]
  );

  return {
    waiting,
    handled,
    refresh,
    handledCount,
    handledLoaded,
    loading,
    error,
    loadHandled,
    answer,
    reanswer,
    expire,
  };
}

/** The 等我回覆 page's data — the shared waiting/handled source. MUST be used
 * under a <ReplyCardsProvider>. */
export function useReplyCards(): UseReplyCards {
  const ctx = useContext(ReplyCardsContext);
  if (!ctx) {
    throw new Error("useReplyCards must be used within a <ReplyCardsProvider>");
  }
  return ctx;
}

/** The nav badge's waiting count — literally the length of the SAME
 * authoritative waiting array the page renders (T-e862 同源化), so the badge
 * and the list can never show different numbers. MUST be used under a
 * <ReplyCardsProvider>. */
export function useReplyCardWaitingCount(): number {
  return useReplyCards().waiting.length;
}
