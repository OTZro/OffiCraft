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

  const wanted = ids.filter((id) => !have.has(id) && !askedRef.current.has(id));
  // A stable key so the effect fires on the SET of wanted ids, not on the array
  // identity a render creates fresh every time.
  const wantedKey = wanted.join(",");

  useEffect(() => {
    if (wantedKey === "") return;
    const batch = wantedKey.split(",").slice(0, BY_IDS_MAX);
    for (const id of batch) askedRef.current.add(id);
    let alive = true;
    void (async () => {
      let rows: ChatMessage[] = [];
      try {
        rows = await api.listChatByIds(batch);
      } catch {
        // ALL OR NOTHING on the wire: one unknown id refuses the whole call, so
        // a throw says nothing about the other ids in the batch. Record the
        // whole batch as missed rather than retrying — a retry loop against a
        // permanent refusal is worse than an honest "earlier message" label.
        rows = [];
      }
      if (!alive) return;
      const byId = new Map(rows.map((m) => [m.id, m]));
      setFetched((prev) => {
        const next = new Map(prev);
        for (const id of batch) next.set(id, byId.get(id) ?? null);
        return next;
      });
    })();
    return () => {
      alive = false;
    };
  }, [wantedKey]);

  return fetched;
}
