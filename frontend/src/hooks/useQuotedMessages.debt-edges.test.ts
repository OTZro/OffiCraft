// useQuotedMessages — the debt-collection edges the shipped test file never
// drove (T-4e95).
//
// The main file (useQuotedMessages.test.ts) pins the spine: a blip is marked,
// the next `chat` delta pays it, a 4xx never is. These are the faces beside it:
//   (1) a burst carrying ONLY `chat_read` — QUOTE_TOPICS names it, but every
//       other test pays the debt with a `chat` delta, so the second half of
//       that Set had no witness;
//   (2) MORE THAN ONE debt at once, and a debt whose id is not on screen when
//       the paying burst lands (the owner switched conversations — the exact
//       situation the fix's own comment says askedRef plus a keyless <ChatArea>
//       made permanent before);
//   (3) blip -> heal -> blip -> heal: the SECOND cycle, i.e. whether a heal
//       that ITSELF fails re-marks the debt or drops it on the floor;
//   (4) the reconnect resync — `resyncAll` (api/http.ts) fans one synthetic
//       delta per topic in SSE_RESYNC_TOPICS, synchronously, on a reconnect and
//       on a return to the foreground, and two of those topics are ours. So an
//       outage that ends by re-establishing the socket pays the debt with no
//       new message at all; every other test hands the sink a NAMED delta, and
//       a resync delta names nothing.
//
// 🔴 NO rerender() BETWEEN AN EMIT AND ITS WAIT, anywhere in this file. Paying
// the debt is `askedRef.delete` + `staleRef.clear()`, neither of which
// repaints; the sink's own `setAttempt` is the only thing that makes the
// re-read go out. A hand-repaint there keeps the test green against a sink
// that clears the debt and never asks again — measured, it hid exactly that
// mutant for a full review round.

import { describe, it, expect, vi, beforeEach } from "vitest";
import { renderHook, waitFor, act } from "@testing-library/react";
import { StrictMode, createElement } from "react";
import type { ChatMessage, SseDelta } from "../api/adapter";

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

import { SSE_RESYNC_TOPICS, toSseDelta } from "../api/http";
import { useQuotedMessages } from "./useQuotedMessages";

function mkMsg(id: string): ChatMessage {
  return {
    id,
    from: "m1",
    to: "owner",
    body: `body-of-${id}`,
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

// A read-watermark delta and NOTHING else. This is what the peer opening the
// conversation produces — no new message, so no `chat` topic in the burst.
const aChatReadDelta: SseDelta = {
  topic: "chat_read",
  names: { reader: "m1" },
  ids: ["m1"],
};
const aChatDelta: SseDelta = {
  topic: "chat",
  names: { id: "m9", from: "a", to: "b" },
  ids: ["m9", "a", "b"],
};
const aTaskDelta: SseDelta = { topic: "task", names: {}, ids: [] };

async function emit(delta: SseDelta): Promise<void> {
  await act(async () => {
    h.sseHandler?.(delta.topic, delta);
    await Promise.resolve();
    await Promise.resolve();
    await Promise.resolve();
    await Promise.resolve();
  });
}

const strict = {
  wrapper: ({ children }: { children: React.ReactNode }) =>
    createElement(StrictMode, null, children),
};

describe("useQuotedMessages: a chat_read-ONLY burst is a payment", () => {
  it("pays the debt when the only topic in the burst is chat_read", async () => {
    h.listChatByIds.mockRejectedValue(new Error("down"));

    const { result, rerender } = renderHook(
      () => useQuotedMessages(["c-1"], EMPTY),
      strict,
    );
    await waitFor(() => expect(result.current.get("c-1")).toBeNull());
    rerender();
    const callsWhileDown = h.listChatByIds.mock.calls.length;
    // Denominator: the outage really did cost calls, so "no further calls"
    // below is a statement about behaviour and not about a dead mock.
    expect(callsWhileDown).toBeGreaterThan(0);

    // A topic this surface does not reconcile on pays nothing — the negative
    // half, so a mutant that widens the gate to "any topic" is not green here.
    await emit(aTaskDelta);
    expect(h.listChatByIds.mock.calls.length).toBe(callsWhileDown);
    expect(result.current.get("c-1")).toBeNull();

    h.listChatByIds.mockReset().mockResolvedValue([mkMsg("c-1")]);
    await emit(aChatReadDelta);
    // 🔴 NO rerender() HERE — see this file's header.
    await waitFor(() => expect(result.current.get("c-1")?.id).toBe("c-1"));
    expect(h.listChatByIds).toHaveBeenCalledWith(["c-1"]);
  });
});

describe("useQuotedMessages: more than one debt, and a debt off screen", () => {
  it("pays TWO debted ids in the same batch on one burst", async () => {
    h.listChatByIds.mockRejectedValue(new Error("down"));

    const { result, rerender } = renderHook(
      () => useQuotedMessages(["c-A", "c-B"], EMPTY),
      strict,
    );
    await waitFor(() => expect(result.current.get("c-A")).toBeNull());
    await waitFor(() => expect(result.current.get("c-B")).toBeNull());
    rerender();

    h.listChatByIds.mockReset().mockResolvedValue([mkMsg("c-A"), mkMsg("c-B")]);
    await emit(aChatDelta);
    await waitFor(() => expect(result.current.get("c-A")?.id).toBe("c-A"));
    expect(result.current.get("c-B")?.id).toBe("c-B");
    // Both ids in ONE call, not two — the batching the header promises.
    expect(h.listChatByIds.mock.calls).toEqual([[["c-A", "c-B"]]]);
  });

  it("a debt whose id is OFF SCREEN when the burst lands is still re-read on return", async () => {
    // Room A: c-A is quoted and the read blips out.
    h.listChatByIds.mockRejectedValue(new Error("down"));
    const { result, rerender } = renderHook(
      ({ ids }: { ids: string[] }) => useQuotedMessages(ids, EMPTY),
      { initialProps: { ids: ["c-A"] }, ...strict },
    );
    await waitFor(() => expect(result.current.get("c-A")).toBeNull());
    const callsInRoomA = h.listChatByIds.mock.calls.length;
    expect(callsInRoomA).toBeGreaterThan(0);

    // The owner switches conversation. <ChatArea> has no key, so this hook does
    // NOT remount — the quoted id simply stops being asked for.
    h.listChatByIds.mockReset().mockResolvedValue([]);
    rerender({ ids: [] });
    // The paying burst lands while c-A is nowhere on screen. NO hand-repaint
    // after the emit — see this file's header.
    await emit(aChatDelta);

    // Back to room A.
    h.listChatByIds.mockReset().mockResolvedValue([mkMsg("c-A")]);
    rerender({ ids: ["c-A"] });
    await waitFor(() => expect(result.current.get("c-A")?.id).toBe("c-A"));
    expect(h.listChatByIds).toHaveBeenCalledWith(["c-A"]);
  });
});

describe("useQuotedMessages: blip → heal → blip → heal", () => {
  it("a heal that ITSELF fails re-marks the debt, and the next burst still pays", async () => {
    h.listChatByIds.mockRejectedValue(new Error("down"));

    const { result, rerender } = renderHook(
      () => useQuotedMessages(["c-1"], EMPTY),
      strict,
    );
    await waitFor(() => expect(result.current.get("c-1")).toBeNull());
    rerender();

    // First heal attempt — the server is STILL down.
    const before1 = h.listChatByIds.mock.calls.length;
    await emit(aChatDelta);
    await waitFor(() =>
      expect(h.listChatByIds.mock.calls.length).toBeGreaterThan(before1),
    );
    expect(result.current.get("c-1")).toBeNull();

    // Second heal attempt — the server is back. If the failed heal had dropped
    // the debt, nothing here would ask again and the row would lie forever,
    // which is the whole defect this feature exists to end.
    h.listChatByIds.mockReset().mockResolvedValue([mkMsg("c-1")]);
    await emit(aChatDelta);
    await waitFor(() => expect(result.current.get("c-1")?.id).toBe("c-1"));
    expect(h.listChatByIds).toHaveBeenCalledWith(["c-1"]);
  });
});

// THE REAL RECOVERY PATH.
//
// The shipped comment states the residual gap as 「如果那條線之後再也沒有任何
// 事件,就還是不會補」. That is true of MESSAGES, and it under-states the fix,
// because the transport itself produces an event when a dropped connection
// comes back: http.ts `resyncAll` fans ONE synthetic delta per topic in
// SSE_RESYNC_TOPICS — 13 of them, SYNCHRONOUSLY, so the delta sink folds them
// into a single burst — on es.onopen AND on a return to the foreground. Two of
// those topics are `chat` and `chat_read`.
//
// So an outage that ends with the socket reconnecting pays the debt with no
// new message at all. Nothing tested this shape: every existing test hands the
// sink a NAMED delta, and a resync delta names NOTHING (ids: []).
async function emitResync(): Promise<void> {
  await act(async () => {
    // Synchronously, exactly as resyncAll does — one burst, not thirteen — and
    // over the REAL topic list, so dropping `chat`/`chat_read` from it is
    // visible here.
    for (const topic of SSE_RESYNC_TOPICS) {
      h.sseHandler?.(topic, toSseDelta(topic, null));
    }
    await Promise.resolve();
    await Promise.resolve();
    await Promise.resolve();
    await Promise.resolve();
  });
}

describe("useQuotedMessages: a reconnect resync pays the debt with no new message", () => {
  it("heals on the synthetic resync burst alone", async () => {
    h.listChatByIds.mockRejectedValue(new Error("socket dropped"));

    const { result, rerender } = renderHook(
      () => useQuotedMessages(["c-1"], EMPTY),
      strict,
    );
    await waitFor(() => expect(result.current.get("c-1")).toBeNull());
    rerender();
    expect(h.listChatByIds.mock.calls.length).toBeGreaterThan(0);

    h.listChatByIds.mockReset().mockResolvedValue([mkMsg("c-1")]);
    await emitResync();
    await waitFor(() => expect(result.current.get("c-1")?.id).toBe("c-1"));
    // ONE re-read for the whole 13-topic burst, not one per topic — the
    // coalescing the delta sink exists for.
    expect(h.listChatByIds.mock.calls).toEqual([[["c-1"]]]);
  });
});
