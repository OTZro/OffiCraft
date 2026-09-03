// lib/threadCommit — the chat thread's only door (T-48).
//
// The behavioural half of the anti-shift fix is pinned where it is caused:
// `commit` must not let messages reach the view before their WAITING reply
// cards are in hand (ChatArea.anchor-entry.test.tsx asserts that end-to-end,
// and chat-jump-card-shift.ct.spec.tsx measures the pixels). What is pinned
// HERE is the other edge of the same rule — the one that turns a safety
// property into an outage if it is left unbounded.

import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { renderHook, act, waitFor } from "@testing-library/react";
import type { ChatMessage, ReplyCard } from "../api/adapter";

const h = vi.hoisted(() => ({
  getReplyCard: vi.fn<(id: string) => Promise<ReplyCard>>(),
}));

vi.mock("../api", () => ({ api: { getReplyCard: h.getReplyCard } }));

import { useThreadCommit, type Thread } from "./threadCommit";
import {
  REPLY_CARD_PREFILL_DEADLINE_MS,
  getCachedReplyCard,
  resetReplyCardCache,
} from "./replyCardCache";

function waitingRow(id: string, cardId: string): ChatMessage {
  return {
    id,
    from: "m1",
    to: "owner",
    body: "要寄出嗎?",
    ts: 1,
    attachments: [],
    replyCardId: cardId,
    replyCardStatus: "waiting",
  };
}

const page = (...m: ChatMessage[]): Thread => ({
  messages: m,
  hasMore: false,
  gapSuspected: false,
  hasNewer: false,
});

beforeEach(() => {
  resetReplyCardCache();
  h.getReplyCard.mockReset();
});

afterEach(() => {
  vi.useRealTimers();
});

describe("useThreadCommit", () => {
  it("holds a page of messages back until its waiting cards are in hand", async () => {
    let land!: (c: ReplyCard) => void;
    h.getReplyCard.mockImplementation(
      () => new Promise<ReplyCard>((r) => (land = r)),
    );
    const { result } = renderHook(() => useThreadCommit());

    let landed: boolean | undefined;
    act(() => {
      void result.current
        .commit(result.current.takeTicket(), () =>
          page(waitingRow("c-1", "rc-1")),
        )
        .then((ok) => (landed = ok));
    });
    await waitFor(() => expect(h.getReplyCard).toHaveBeenCalledWith("rc-1"));
    // The denominator: the card really is still in the air, and the rows really
    // are still off screen — not "the test asserted after everything settled".
    expect(landed, "the commit resolved before its card did").toBeUndefined();
    expect(result.current.thread.messages).toEqual([]);

    await act(async () => {
      land({ id: "rc-1", status: "waiting" } as ReplyCard);
    });
    await waitFor(() =>
      expect(result.current.thread.messages.map((m) => m.id)).toEqual(["c-1"]),
    );
    // …and the row's card is on hand for the first frame it is painted in,
    // which is the whole point of holding it back.
    expect(getCachedReplyCard("rc-1")).toBeTruthy();
    expect(landed).toBe(true);
  });

  it("still shows the thread when a card fetch never answers at all", async () => {
    // 🔴 AN EMPTY ROOM IS THIS FEATURE'S WORST FAILURE, and it is the failure
    // the fix itself creates if the wait is unbounded: EVERY commit point now
    // awaits the cards, so one `getReplyCard` that hangs would hold the whole
    // conversation off the screen — silently, with nothing on screen to say so.
    // That is the exact shape T-48 has already shipped four times.
    //
    // The deadline turns it back into "the card fills in a frame late", which is
    // merely the defect we started from. Measured against the isolated server:
    // a whole 30-card page fetched concurrently settles in 4.2ms worst of 20
    // runs, so the bound is ~357x the observed worst case and cannot fire on a
    // healthy read.
    vi.useFakeTimers();
    h.getReplyCard.mockImplementation(() => new Promise<ReplyCard>(() => {}));
    const { result } = renderHook(() => useThreadCommit());

    act(() => {
      void result.current.commit(result.current.takeTicket(), () =>
        page(waitingRow("c-1", "rc-1")),
      );
    });
    await vi.advanceTimersByTimeAsync(REPLY_CARD_PREFILL_DEADLINE_MS - 1);
    // Denominator: it really was being held, right up to the deadline. Without
    // this the assertion below would also pass on a commit that never waited.
    expect(
      result.current.thread.messages,
      "the commit must genuinely be waiting before the deadline",
    ).toEqual([]);

    await act(async () => {
      await vi.advanceTimersByTimeAsync(2);
    });
    expect(
      result.current.thread.messages.map((m) => m.id),
      "a card that never answers must not cost the reader the whole room",
    ).toEqual(["c-1"]);
    expect(getCachedReplyCard("rc-1")).toBeUndefined();
  });

  it("drops a page a newer ticket overtook while its cards were in the air", async () => {
    // The generation re-check lives AFTER the prefill await, so a load that
    // started later and committed sooner is not overwritten by the page it
    // precedes.
    const landings: Array<() => void> = [];
    h.getReplyCard.mockImplementation(
      () => new Promise<ReplyCard>((r) => landings.push(() => r({ id: "rc-1" } as ReplyCard))),
    );
    const { result } = renderHook(() => useThreadCommit());

    const older = result.current.takeTicket();
    const newer = result.current.takeTicket();
    let olderOk: boolean | undefined;
    act(() => {
      void result.current
        .commit(older, () => page(waitingRow("c-old", "rc-1")))
        .then((ok) => (olderOk = ok));
    });
    await waitFor(() => expect(landings.length).toBe(1));

    await act(async () => {
      // The newer page carries no waiting card, so it commits at once — which
      // is the interleaving that matters: it lands INSIDE the older page's
      // prefill await.
      await result.current.commit(newer, () => ({
        ...page({ ...waitingRow("c-new", "rc-2"), replyCardId: null, replyCardStatus: null }),
      }));
    });
    await waitFor(() =>
      expect(result.current.thread.messages.map((m) => m.id)).toEqual(["c-new"]),
    );

    await act(async () => {
      landings[0]();
    });
    await waitFor(() => expect(olderOk).toBe(false));
    expect(
      result.current.thread.messages.map((m) => m.id),
      "the overtaken page must not land on top of the newer one",
    ).toEqual(["c-new"]);
  });

  it("clear() empties the thread without waiting on anything", () => {
    h.getReplyCard.mockImplementation(() => new Promise<ReplyCard>(() => {}));
    const { result } = renderHook(() => useThreadCommit());
    act(() => {
      result.current.clear();
    });
    // Synchronous by construction: `clear` takes no parameters, so there are no
    // messages for it to owe cards for — and an await here would paint one extra
    // frame of the conversation the owner has just left.
    expect(result.current.thread.messages).toEqual([]);
    expect(h.getReplyCard).not.toHaveBeenCalled();
  });
});
