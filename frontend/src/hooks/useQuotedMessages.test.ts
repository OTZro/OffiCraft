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
import { StrictMode, createElement } from "react";
import type { ChatMessage, SseDelta } from "../api/adapter";
import { ApiError } from "../api/errors";

const h = vi.hoisted(() => ({
  listChatByIds: vi.fn<(ids: string[]) => Promise<ChatMessage[]>>(),
  sseHandler: null as ((topic: string, delta?: SseDelta) => void) | null,
}));

vi.mock("../api", () => ({
  api: {
    listChatByIds: h.listChatByIds,
    subscribeEvents: (cb: (topic: string, delta?: SseDelta) => void) => {
      h.sseHandler = cb;
      return () => {
        h.sseHandler = null;
      };
    },
  },
}));

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
  h.sseHandler = null;
});

// A chat delta on the shared downlink — "the next event" the debt waits for.
const aChatDelta: SseDelta = {
  topic: "chat",
  names: { id: "m9", from: "a", to: "b" },
  ids: ["m9", "a", "b"],
};
// A topic this surface does not reconcile on. Debt or no debt, it is not an
// event for us.
const aMonitoringDelta: SseDelta = {
  topic: "monitoring",
  names: {},
  ids: [],
};

async function emit(delta: SseDelta): Promise<void> {
  await act(async () => {
    h.sseHandler?.(delta.topic, delta);
    await Promise.resolve();
    await Promise.resolve();
    await Promise.resolve();
  });
}

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

// ─────────────────────────────────────────────────────────────────────────────
// T-4e95: 「標起來、下一個事件來就補」— the quote line stopped lying forever.
//
// The defect these pin, measured on the real thing rather than reasoned:
// `attempts under a sustained outage: 2`, then, after changing rooms,
// `[after-room-switch] quoteBody="較早的一則訊息"` and `attempts after room
// switch: 2 (was 2) — re-asked=false`. Two failures settled the id as a miss
// and NOTHING ever revisited it — `askedRef`/`fetched` never expire and
// OfficePage mounts <ChatArea> without a key, so the hook does not remount when
// the owner switches conversations. Only a page reload cleared it.
//
// BOTH DIRECTIONS ARE PINNED ON PURPOSE. A test that only proves "the event
// re-asks" is half a guardrail: it stays green against the fake fix of deleting
// the `staleRef.current.size === 0` gate, which would turn every chat delta in
// the company into a fresh by-ids call for every quote on screen. So the
// negative controls below assert that an id with NO debt — one refused with a
// 4xx, and one whose debt was already paid — is NOT re-asked when an event
// lands, and that a topic this surface does not reconcile on releases nothing.
//
// WHAT IS ASSERTED: which ids were passed to `listChatByIds`, how many calls
// happened, and what the returned map holds for that id. No log-string or
// substring matching anywhere.
//
// The rerender() inside the waiting window is deliberate: the mark is a ref, a
// cleanup-only ref write dies under StrictMode's setup→cleanup→setup, and a
// test that merely awaits can go green against a hook that never repaints.
// These render under <StrictMode> for the same reason — the sink's effect has
// `[]` deps, so the double-invoke is its ordinary case, not an exotic one.
const strict = {
  wrapper: ({ children }: { children: React.ReactNode }) =>
    createElement(StrictMode, null, children),
};

describe("useQuotedMessages: a blip-miss is marked and paid on the next event", () => {
  it("re-asks after a sustained outage once an event arrives, and the quote resolves", async () => {
    h.listChatByIds.mockRejectedValue(new Error("down"));

    const { result, rerender } = renderHook(
      () => useQuotedMessages(["c-1"], EMPTY),
      strict,
    );

    // The outage settles it as the honest miss — that half is unchanged.
    await waitFor(() => expect(result.current.get("c-1")).toBeNull());
    rerender();
    const callsWhileDown = h.listChatByIds.mock.calls.length;

    // A topic this surface does not reconcile on is not our event: the debt
    // stays owed and nothing is asked.
    await emit(aMonitoringDelta);
    expect(h.listChatByIds.mock.calls.length).toBe(callsWhileDown);
    expect(result.current.get("c-1")).toBeNull();

    // The server comes back, and the next chat delta pays the debt.
    //
    // 🔴 NO rerender() BETWEEN THE EMIT AND THE WAIT, ON PURPOSE. Paying the
    // debt is `askedRef.delete` + `staleRef.clear()`, neither of which repaints
    // — the hook's own `setAttempt` in the sink is the ONLY thing that makes
    // `wantedKey` recompute and the re-read go out. A rerender() here does that
    // repaint BY HAND and the test then passes against a sink that clears the
    // debt and never asks again (verified: removing that `setAttempt` left all
    // 7 tests green while the rerender() was here). The real page hides the
    // same hole by accident — `useChat` refetches on that same chat delta and
    // repaints <ChatArea> — so this assertion is the only place the hook's own
    // ability to repaint itself is visible at all.
    h.listChatByIds.mockReset().mockResolvedValue([mkMsg("c-1")]);
    await emit(aChatDelta);

    await waitFor(() => expect(result.current.get("c-1")?.id).toBe("c-1"));
    expect(h.listChatByIds).toHaveBeenCalledWith(["c-1"]);
  });

  it("does NOT re-ask on an event when the miss was a 4xx — a refusal is an answer", async () => {
    h.listChatByIds.mockRejectedValue(
      new ApiError("http 404 for GET /api/chat", 404, "not_found", ""),
    );

    const { result, rerender } = renderHook(
      () => useQuotedMessages(["c-gone"], EMPTY),
      strict,
    );

    await waitFor(() => expect(result.current.get("c-gone")).toBeNull());
    rerender();
    const callsAfterRefusal = h.listChatByIds.mock.calls.length;

    await emit(aChatDelta);
    await emit(aChatDelta);
    rerender();

    expect(h.listChatByIds.mock.calls.length).toBe(callsAfterRefusal);
    expect(result.current.get("c-gone")).toBeNull();
  });

  it("does NOT re-ask on a LATER event once the debt has been paid", async () => {
    h.listChatByIds.mockRejectedValue(new Error("down"));

    const { result, rerender } = renderHook(
      () => useQuotedMessages(["c-1"], EMPTY),
      strict,
    );
    await waitFor(() => expect(result.current.get("c-1")).toBeNull());
    rerender();

    h.listChatByIds.mockReset().mockResolvedValue([mkMsg("c-1")]);
    await emit(aChatDelta);
    rerender();
    await waitFor(() => expect(result.current.get("c-1")?.id).toBe("c-1"));

    // Debt paid. Every further chat delta must find nothing owed — otherwise a
    // resolved quote would be re-read on every message anyone sends anywhere.
    const callsAfterRecovery = h.listChatByIds.mock.calls.length;
    await emit(aChatDelta);
    await emit(aChatDelta);
    rerender();

    expect(h.listChatByIds.mock.calls.length).toBe(callsAfterRecovery);
    expect(result.current.get("c-1")?.id).toBe("c-1");
  });

  it("spends the WHOLE mark on one event, including ids nothing re-reads", async () => {
    // `staleRef.current.clear()` releases the whole mark, not just the ids that
    // happen to still be quoted. Deleting it is invisible in the happy path —
    // the re-read succeeds either way — so this pins the tail: c-2 goes out of
    // the loaded window before the event lands, so no read ever covers it. If
    // the mark survived, c-2 would keep the debt non-empty forever and EVERY
    // later chat delta would un-ask the whole marked set again, re-issuing a
    // read for c-1, which is still on screen. The cost is a wasted by-ids call
    // per message anyone sends anywhere, not a wrong answer — which is exactly
    // why nothing else here can see it.
    h.listChatByIds.mockRejectedValue(new Error("down"));

    const { result, rerender } = renderHook(
      ({ ids }: { ids: string[] }) => useQuotedMessages(ids, EMPTY),
      { ...strict, initialProps: { ids: ["c-1", "c-2"] } },
    );

    await waitFor(() => expect(result.current.get("c-1")).toBeNull());
    expect(result.current.get("c-2")).toBeNull();

    // c-2 scrolls out of the loaded window: it is still marked, but nothing
    // will ask for it again.
    rerender({ ids: ["c-1"] });

    h.listChatByIds.mockReset().mockResolvedValue([mkMsg("c-1")]);
    await emit(aChatDelta);
    await waitFor(() => expect(result.current.get("c-1")?.id).toBe("c-1"));
    expect(h.listChatByIds).toHaveBeenCalledWith(["c-1"]);
    const callsAfterCollection = h.listChatByIds.mock.calls.length;

    await emit(aChatDelta);
    await emit(aChatDelta);
    rerender({ ids: ["c-1"] });

    expect(h.listChatByIds.mock.calls.length).toBe(callsAfterCollection);
    expect(result.current.get("c-1")?.id).toBe("c-1");
  });
});
