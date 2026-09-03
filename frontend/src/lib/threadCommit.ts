// lib/threadCommit.ts — THE ONLY DOOR ONTO THE CHAT THREAD (T-48).
//
// 🔴 WHY THIS IS A MODULE AND NOT SEVEN CAREFUL CALL SITES. The reply cards a
// thread carries are fetched SEPARATELY and LATER than the messages, so a
// waiting card above a scroll target grows after the fact and pushes the target
// down (measured +254px at 1280 wide; 0 at 390, where the browser's own scroll
// anchoring absorbs it). The fix is that every commit must have those cards in
// hand BEFORE the messages reach the view — and the way that fix has failed
// before, four times in one night with a green suite, is a hand-written
// `await prefetch(...)` at each of N commit points that the next reader has to
// remember to write.
//
// So the raw `useState<Thread>` setter does not leave this module. `useChat`
// gets three doors and no fourth: `commit` (a ticketed thread write),
// `mergeHistory` (loadOlder's un-ticketed prepend) and `clear` — which takes NO
// PARAMETERS and therefore cannot express a message. "Commit messages to the
// view without awaiting their cards" is not a rule anybody has to keep; it is a
// sentence that cannot be typed.
//
// 🔴 THE HOUSE PRECEDENT IS `lib/conversationLatches.ts`, AND THIS RHYMES WITH
// IT DELIBERATELY. That file made the same call for the same reason: the
// mutable fields left the type, state moved into the closure, and the broken
// form stopped being writable rather than stopping being written. Read its
// header before changing this one.
//
// 🔴 AND FOR THE SAME REASON THERE IS NO BRANDED `CardsReady<Thread>` TYPE.
// Once the setter is unreachable, a brand is a second statement of a fact the
// module shape already makes true — and `as any` walks straight through a
// brand while it reaches nothing here. `conversationLatches` declined the same
// ornament; the guarantee is structural or it is nothing.
//
// 🔴 WHAT IS NOT IN HERE, AND WHY. `forwardExhaustedRef` — the forward walk's
// own stop sign — stays in `useChat`. It answers "did that ask actually go
// out", which is knowledge the loader has and this module does not (see its
// note there). And the effect's `alive` flag stays in the effect: `commit` does
// not know whether the caller's effect is still mounted and must not pretend
// to, so `load()` asks before AND after.

import { useCallback, useRef, useState } from "react";
import type { ChatMessage } from "../api/adapter";
import { prefillWaitingCards } from "./replyCardCache";

// 🔴 IT USED TO CARRY A `peer` FIELD (T-48, R13-3). The hook was mounted once
// and swapped between rooms, so there was a committed frame holding the new
// room's identity beside the old room's messages, and every reader either
// checked `messagesPeer` or was wrong. `ChatArea` is mounted under
// `key={peerId}` now, so one instance of this hook belongs to one room for its
// whole life: there is no second room for these messages to be confused with.
export interface Thread {
  messages: ChatMessage[];
  hasMore: boolean;
  // 🔴 A seam this thread could NOT close (T-b0bb): a newest page did not join
  // onto what we held, and the backfill walk hit MAX_BACKFILL_PAGES (or its own
  // request failed) before reaching a row we already had. Messages are missing
  // from the MIDDLE of `messages` and we do not know which or how many.
  //
  // This exists so that giving up is not silent. It is deliberately STICKY for
  // the life of the conversation view: a later page that joins cleanly does not
  // retroactively deliver the rows we lost, so it must not clear the warning.
  // It resets on a peer switch / remount, and that reset is CORRECT rather than
  // a loss: the effect's setup body clears `messages` first, so the rebuilt
  // thread is a fresh newest window with the hole ABOVE it, not inside it —
  // `loadOlder`'s cursor (messages[0]) then walks back THROUGH the range that
  // was skipped and the rows come back. Scrolling up after a reload / peer
  // switch recovers the messages; the notice going away is not a silent loss of
  // them. (Do not restate the older claim that a reload makes the notice vanish
  // "without the messages having been recovered" — that was measured wrong.)
  //
  // 🟠 NAMED DEBT — THE SERVER-SIDE READ WATERMARK IS NOT FIXED (T-b0bb).
  // What a reload does NOT undo is the read state. While the hole existed the
  // server had already advanced the owner's watermark past it (a no-cursor
  // `listChat` marks up to the newest ts of the page it served), so those rows
  // stay counted as READ: unread does not go back up, and the "以下是未讀"
  // divider will not point at them even after they are scrolled back into view.
  // Nothing on the client can repair that — it needs either a watermark that
  // only advances over rows actually delivered, or a way to rewind it. See
  // server/ocserverd/api_chat_gap_tb0bb_test.go
  // `TestChatWatermarkAdvancesPastMessagesTheCallerNeverReceived`, which is
  // labelled CHARACTERIZATION for exactly this reason: it pins the behaviour we
  // are shipping WITH, not one we fixed.
  //
  // All THREE of backfillSeam's ways to stop short raise this flag: the budget
  // ran out, a cursor request failed, or a cursor page came back EMPTY while we
  // are demonstrably holding older rows (review S1 — see the comment on that
  // branch for why an empty page there is a contradiction and never an ending).
  // Nothing in this file gives up quietly.
  gapSuspected: boolean;
  // 🔴 THIS THREAD IS AN ANCHOR WINDOW, NOT THE LIVE TAIL (T-48 ③). Set when
  // `loadAround` lands a window whose newer half came back FULL (so the stream
  // continues below it), cleared the moment a forward page comes back short or
  // `resetToLatest` replaces the thread.
  //
  // ⚠️ IT ALSO GATES `load()`. A newest-window page cannot be merged into a
  // historical window: the range between the two is unloaded, `pageJoinsThread`
  // correctly says so, and `backfillSeam` would then spend up to
  // MAX_BACKFILL_PAGES round-trips failing to close a seam that is not a defect
  // at all — and end by raising `gapSuspected`, i.e. telling the owner messages
  // were LOST when in truth they were merely not fetched yet. So while this is
  // true the periodic/SSE load is skipped, and the forward walk (`loadNewer`)
  // or the arrow (`resetToLatest`) is what brings the thread back to the tail.
  hasNewer: boolean;
}

/** The three doors, and nothing else. */
export interface ThreadCommit {
  /** The committed thread, for rendering. */
  thread: Thread;
  /** The live MIRROR — what `threadRef.current` used to be. Read-only by
   * construction (there is no setter): a state read inside an await window is a
   * stale closure, and every loader in `useChat` compares against this. */
  current(): Thread;
  /** Take a generation ticket. Tickets are a MONOTONIC CLOCK, not a latch: a
   * ticket taken later must outrank one taken earlier, so nothing resets them
   * mid-conversation. They start at zero with the hook, which since R13-5 means
   * with the conversation. */
  takeTicket(): number;
  /** Empty the thread, synchronously.
   *
   * 🔴 IT TAKES NO PARAMETERS, AND THAT IS THE WHOLE POINT. A `clear` that could
   * be handed messages would be a second, un-awaited way onto the screen — the
   * exact hole this module exists to close. It also stays SYNCHRONOUS: an await
   * here would paint one extra frame of the conversation being left, and there
   * are no messages in an empty thread whose cards could be missing. */
  clear(): void;
  /** Commit a thread write under generation `seq`.
   *
   * Four steps, fixed:
   *   ① run `next` against the mirror to get the candidate;
   *   ② await every WAITING card the candidate carries;
   *   ③ re-check the ticket — return false if a newer commit landed;
   *   ④ take the generation, advance the mirror, hand `next` to React.
   *
   * Resolves TRUE when the write landed, FALSE when it was superseded. The
   * caller must branch on it: "superseded" and "committed" are different
   * outcomes and collapsing them is how a jump reports a message that is sitting
   * right there as 「跳轉被打斷」. */
  commit(seq: number, next: (prev: Thread) => Thread): Promise<boolean>;
  /** `loadOlder`'s prepend: no ticket, because a history page is additive and
   * cannot supersede or be superseded by a newest-window load — it is written
   * through an updater precisely so a commit landing inside its await window is
   * present in React's `prev` and survives. Same card guarantee as `commit`;
   * expect it to fetch nothing in practice, since history pages are almost
   * entirely answered/expired cards, which are never prefetched. */
  mergeHistory(next: (prev: Thread) => Thread): Promise<void>;
}

const EMPTY: Thread = {
  messages: [],
  hasMore: true,
  gapSuspected: false,
  hasNewer: false,
};

export function useThreadCommit(): ThreadCommit {
  const [thread, setThread] = useState<Thread>(EMPTY);
  // The live mirror, advanced on every render exactly as `threadRef` was.
  const mirror = useRef(thread);
  mirror.current = thread;
  const issued = useRef(0);
  const committed = useRef(0);

  const current = useCallback(() => mirror.current, []);
  const takeTicket = useCallback(() => (issued.current += 1), []);

  const clear = useCallback(() => {
    // 🔴 THE MIRROR IS NOT ADVANCED HERE, ON PURPOSE. `useChat`'s effect calls
    // this and then `load()` in the same tick, and `load()` reads the mirror to
    // decide whether it is looking at an anchor window. Advancing here would
    // change that answer — a behaviour change wearing a tidy-up's clothes, on a
    // path whose whole existing note is about the mirror being one commit
    // behind. The next render advances it, as it always did.
    setThread(EMPTY);
  }, []);

  const commit = useCallback(
    async (seq: number, next: (prev: Thread) => Thread): Promise<boolean> => {
      const candidate = next(mirror.current);
      await prefillWaitingCards(candidate.messages);
      // 🔴 THE GENERATION IS TAKEN AFTER THE AWAIT, NEVER BEFORE IT. Taking it
      // first would mark this load as the newest while it is still waiting on a
      // card, so a load that started later, finished sooner and committed would
      // be judged superseded by a page it PRECEDES — the out-of-order commit
      // measured on the unguarded backfill (75 rows, newest five at the top of
      // the conversation). Every commit point in `useChat` used to spell this
      // out for itself; there is one copy now, and it is here.
      if (seq < committed.current) return false;
      committed.current = seq;
      // Advance the mirror NOW rather than at the next render: every consumer
      // reads it as "the freshest thread", and a walk that asks twice from the
      // same anchor because the mirror had not caught up is a measured defect,
      // not a theoretical one (the duplicate `?start_id=` pair, 8ms apart).
      mirror.current = candidate;
      // …and React gets the UPDATER, not `candidate`. The two answer different
      // questions: the mirror wants the value now, React wants the merge
      // computed against whatever it actually holds — an un-ticketed history
      // page that landed inside this await window is in `prev` and absent from
      // `candidate`, and committing the object would silently eat it (measured:
      // 30 loaded rows vanished).
      setThread(next);
      return true;
    },
    [],
  );

  const mergeHistory = useCallback(
    async (next: (prev: Thread) => Thread): Promise<void> => {
      await prefillWaitingCards(next(mirror.current).messages);
      // No ticket, and no mirror write: `loadOlder` never had either. A history
      // page cannot supersede anything, and writing the mirror from a path with
      // no generation of its own would let it clobber a newer commit that landed
      // inside this await window — React's `prev` is what keeps this correct,
      // and the next render squares the mirror with it.
      setThread(next);
    },
    [],
  );

  return { thread, current, takeTicket, clear, commit, mergeHistory };
}
