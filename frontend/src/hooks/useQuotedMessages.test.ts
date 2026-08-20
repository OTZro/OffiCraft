// useQuotedMessages — a refusal and a blip are not the same answer (T-4e95 r15).
//
// `null` in this hook's map is a SETTLED state: the row stops waiting and prints
// 「較早的一則訊息」. Writing it on ANY throw meant one dropped connection made
// every quote in that batch say so for the rest of the session — `askedRef`
// never forgets, and OfficePage mounts <ChatArea> without a key, so the hook
// does not remount when the owner changes rooms. Only a page reload cleared it.
//
// What is pinned here:
//   • a transient failure gets ONE more go, and the quote resolves;
//   • a 4xx (the all-or-nothing 404 an unknown id causes) settles immediately —
//     it will not change on a retry, and a retry loop against a permanent
//     refusal is worse than an honest label;
//   • the retry is bounded: a server that keeps failing costs one repeat, not a
//     loop.

import { describe, it, expect, vi, beforeEach } from "vitest";
import { renderHook, waitFor, act } from "@testing-library/react";
import type { ChatMessage } from "../api/adapter";
import { ApiError } from "../api/errors";

const h = vi.hoisted(() => ({
  listChatByIds: vi.fn<(ids: string[]) => Promise<ChatMessage[]>>(),
}));

vi.mock("../api", () => ({ api: { listChatByIds: h.listChatByIds } }));

import { useQuotedMessages } from "./useQuotedMessages";

function mkMsg(id: string): ChatMessage {
  return {
    id,
    from: "m1",
    to: "owner",
    body: "他說的",
    ts: 1,
    attachments: [],
    replyCardId: null,
    replyCardStatus: null,
    replyTo: null,
  };
}

const EMPTY = new Map<string, ChatMessage>();

beforeEach(() => {
  h.listChatByIds.mockReset();
});

describe("useQuotedMessages", () => {
  it("asks again after a BLIP, and the quote resolves", async () => {
    h.listChatByIds
      .mockRejectedValueOnce(new Error("network"))
      .mockResolvedValueOnce([mkMsg("c-1")]);

    const { result, rerender } = renderHook(() =>
      useQuotedMessages(["c-1"], EMPTY),
    );

    await waitFor(() => expect(h.listChatByIds).toHaveBeenCalledTimes(2));
    rerender();
    await waitFor(() => expect(result.current.get("c-1")?.id).toBe("c-1"));
    // …and never as a settled miss along the way.
    expect(result.current.get("c-1")).not.toBeNull();
  });

  it("does NOT ask again after a 4xx — that refusal will not change", async () => {
    // The server refuses the WHOLE call with 404 when any id names no message.
    h.listChatByIds.mockRejectedValue(
      new ApiError("http 404 for GET /api/chat", 404, "not_found", ""),
    );

    const { result } = renderHook(() => useQuotedMessages(["c-gone"], EMPTY));

    await waitFor(() => expect(result.current.get("c-gone")).toBeNull());
    // Give a stray retry a chance to show up before claiming there was none.
    await act(async () => {
      await Promise.resolve();
    });
    expect(h.listChatByIds).toHaveBeenCalledTimes(1);
  });

  it("settles the ids that already spent their retry, even when the batch also holds fresh ones", async () => {
    // A batch can hold BOTH kinds at once. The first version of the retry handed
    // the extra go to the fresh ids and returned — leaving the spent ones in
    // `askedRef` (never asked again) and absent from the map (never settled),
    // and absent renders as 「…」: a spinner that never resolves, the one thing
    // this file's header opens by promising it does not do.
    //
    // ⚠️ THE SEQUENCE IS THE TEST, and it took two tries to build. A mixed batch
    // only forms when a NEW id becomes wanted on the very render the retry
    // triggers — my first attempt just let c-A retry alone (measured call
    // sequence [c-A],[c-A],[c-B],[c-B]) and passed under its own mutant. Holding
    // the rejection and firing it in the SAME act() as the ids change is what
    // merges them: [c-A],[c-A,c-B],[c-B]. The realistic shape is c-B scrolling
    // out of the loaded window at that moment, which is what `have` models here.
    let rejectFirst: (e: Error) => void = () => {};
    h.listChatByIds
      .mockImplementationOnce(() => new Promise((_, r) => (rejectFirst = r)))
      .mockRejectedValue(new Error("down"));

    const withB = new Map([["c-B", mkMsg("c-B")]]);
    const { result, rerender } = renderHook(
      ({ have }: { have: Map<string, ChatMessage> }) =>
        useQuotedMessages(["c-A", "c-B"], have),
      { initialProps: { have: withB } },
    );
    await waitFor(() => expect(h.listChatByIds).toHaveBeenCalledTimes(1));

    await act(async () => {
      rerender({ have: EMPTY });
      rejectFirst(new Error("blip"));
      await Promise.resolve();
      await Promise.resolve();
    });
    await act(async () => {
      await Promise.resolve();
      await Promise.resolve();
    });

    // The batch really was mixed — say so, or a later change could quietly turn
    // this back into two single-id batches and the test would still pass.
    expect(h.listChatByIds).toHaveBeenCalledWith(["c-A", "c-B"]);
    // Both must reach a SETTLED answer, and the answer must be the MISS. An
    // earlier version asked only `has()`, which a reviewer satisfied by settling
    // to a fabricated message object — green, and the row would then quote text
    // that was never fetched.
    expect(result.current.get("c-A"), "c-A must settle as a miss").toBeNull();
    expect(result.current.get("c-B"), "c-B must settle as a miss").toBeNull();
  });

  it("retries a blip ONCE, then settles — a server that is down is not a loop", async () => {
    h.listChatByIds.mockRejectedValue(new Error("still down"));

    const { result } = renderHook(() => useQuotedMessages(["c-1"], EMPTY));

    await waitFor(() => expect(result.current.get("c-1")).toBeNull());
    expect(h.listChatByIds).toHaveBeenCalledTimes(2);
  });
});
