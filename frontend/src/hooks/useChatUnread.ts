// hooks/useChatUnread.ts — the 辦公室 nav unread signal: the owner's TOTAL
// unread chat count, kept live. Deliberately SEPARATE from useChat/useMembers:
// the badge mounts app-wide (App's nav bar) and must stay cheap — it rides the
// dedicated count endpoint and refetches on the deltas that can MOVE that total,
// without ever pulling the message list or roster. The nav renders the count as
// a badge when it is > 0 (>99 → "99+"), nothing at 0. This is a different signal
// from the 等我回覆 waiting-card badge — they never merge.

import { useEffect, useState } from "react";
import { api } from "../api";
import { createDeltaSink } from "../lib/deltaSink";
import { burstMovesNoOwnerUnread } from "../lib/ownerUnread";

// The SSE topics that can change the office total — the SINGLE source of truth
// for "what makes this badge move". The server total is Σ unread over the LIVE
// set = non-removed members ∪ live outsource workers (api_chat.go
// HandleChatUnreadCount's live[] filter). So the total moves on a new message /
// read (chat / chat_read) AND when the live SET itself changes — a member
// removed/added ("member") or a worker spawned/released ("outsource_worker").
// Missing either lifecycle topic left the parent badge stale behind the 正職/
// 外包 sub-tabs (which useMembers/useOutsourceWorkers DO subscribe to) until a
// manual reload — the bug in T-b86c. This is exported so the test asserts the
// wiring against THIS set (fail-closed: adding a topic here is one edit and the
// test picks it up), not a hand-copied list. NOTE (T-b86c residual, tracked
// separately): a NEW backend topic that changes the live set but is not added
// here would re-stale this badge silently — no test on either side goes red.
export const OFFICE_TOTAL_TOPICS = new Set([
  "chat",
  "chat_read",
  "member",
  "outsource_worker",
]);

export function useChatUnread(): number {
  const [count, setCount] = useState(0);

  useEffect(() => {
    let alive = true;

    const refetch = () => {
      api
        .getChatUnreadCount()
        .then((n) => {
          if (alive) setCount(n);
        })
        .catch((e) => console.warn("useChatUnread: fetch failed", e));
    };

    refetch();
    // This total is ONE number over the whole live set, so there is no "just the
    // item that changed" variant of it — but there ARE two things to remove.
    //
    // (a) DUPLICATES: a resync fans all four of these topics at once, which used
    //     to be four identical count requests for one reconnect. One decision
    //     per burst.
    //
    // (b) 🔴 REQUESTS THAT CANNOT CHANGE THE ANSWER (T-b17f). This total is
    //     Σ `UnreadCounts(…, owner)` over the live set (api_chat.go:873), so a
    //     `chat` line NOT addressed to the owner, or a `chat_read` receipt whose
    //     READER is not the owner, cannot move it by a single unit — the server
    //     would hand back the number we already hold. Agent↔agent traffic is
    //     ordinary here, and before this line every such message cost one
    //     `GET /api/chat/unread-count`, which runs a full `ListChat()` table
    //     scan plus a members and a workers list read. See `lib/ownerUnread.ts`
    //     for why the predicate is exactly `to` / `reader`.
    //
    // The gate is deliberately narrow: it fires only when EVERY topic of ours in
    // this burst is chat/chat_read. `member` / `outsource_worker` change the
    // LIVE SET itself (a removed member drops their leftovers out of the sum),
    // so a burst carrying either still refetches whatever else it also carried.
    const unsubscribe = api.subscribeEvents(
      createDeltaSink((batch) => {
        const mine = [...batch.topics].filter((t) =>
          OFFICE_TOTAL_TOPICS.has(t)
        );
        if (mine.length === 0) return;
        if (burstMovesNoOwnerUnread(batch, mine)) return;
        refetch();
      })
    );

    return () => {
      alive = false;
      unsubscribe();
    };
  }, []);

  return count;
}
