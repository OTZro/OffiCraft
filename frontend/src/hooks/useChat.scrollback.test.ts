// useChat scrollback (T-bf82) — black-box pins on the history-paging seam.
//
//   1. loadOlder() pages backwards with the composite keyset cursor (the
//      current OLDEST message's (ts, id)) and PREPENDS the page; a short page
//      flips hasMore to false and further calls are no-ops.
//   2. Concurrency lock: overlapping loadOlder calls fire ONE cursor request.
//   3. SSE/refetch reconciliation MERGES the refetched newest page into the
//      thread by id (loaded history stays in front) — never a whole-array
//      replace, which would eat the scrollback the owner just loaded.
//   4. hasMore derives honestly from the FIRST landed page too: a thread
//      shorter than one page has no history to load.
//   5. T-48 ③ — the ANCHOR WINDOW, the same seam walked in the other
//      direction: loadAround() opens a window around one message id
//      (`?end_id=` for the context above, `?start_id=` for the context below),
//      loadNewer() walks FORWARDS out of it a page at a time, and while the
//      thread is such a window the ordinary newest-page refresh is suppressed
//      (merging the live tail onto a historical window would draw an unfetched
//      range as contiguous). resetToLatest() is the way back.

import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { renderHook, act, waitFor } from "@testing-library/react";
import type { ChatCursor, ChatMessage } from "../api/adapter";

const h = vi.hoisted(() => {
  return {
    listChatWindow:
      vi.fn<
        (
          withId: string,
          anchor: { startId?: string; endId?: string },
          limit: number,
        ) => Promise<unknown[]>
      >(),
    listChat:
      vi.fn<
        (
          withId: string,
          limit?: number,
          before?: ChatCursor,
        ) => Promise<unknown[]>
      >(),
    listChatReads: vi.fn(async () => [] as unknown[]),
    markChatRead: vi.fn(async () => ({
      readerId: "owner",
      peerId: "b",
      lastReadTs: 1,
    })),
    postChat: vi.fn(async () => ({}) as unknown),
    sseHandler: null as ((topic: string) => void) | null,
  };
});

vi.mock("../api", () => ({
  api: {
    listChat: h.listChat,
    listChatWindow: h.listChatWindow,
    listChatReads: h.listChatReads,
    markChatRead: h.markChatRead,
    postChat: h.postChat,
    subscribeEvents: (cb: (topic: string) => void) => {
      h.sseHandler = cb;
      return () => {
        h.sseHandler = null;
      };
    },
  },
}));

import { useChat } from "./useChat";

function mkMsg(id: string, from: string, to: string, ts: number): ChatMessage {
  return {
    id,
    from,
    to,
    body: `msg ${id}`,
    ts,
    attachments: [],
    replyCardId: null,
  };
}

/** `count` messages b↔owner with ids `${prefix}0..` and ascending ts from
 * `tsStart` — a full server page when count === 30. */
function page(prefix: string, tsStart: number, count: number): ChatMessage[] {
  return Array.from({ length: count }, (_, i) =>
    mkMsg(`${prefix}${i}`, "b", "owner", tsStart + i),
  );
}

let hasFocusSpy: ReturnType<typeof vi.spyOn>;

beforeEach(() => {
  h.listChat.mockReset().mockResolvedValue([]);
  h.listChatWindow.mockReset().mockResolvedValue([]);
  h.listChatReads.mockClear();
  h.markChatRead.mockClear();
  h.sseHandler = null;
  hasFocusSpy = vi.spyOn(document, "hasFocus").mockReturnValue(true);
});

afterEach(() => {
  hasFocusSpy.mockRestore();
});

describe("useChat scrollback (loadOlder / hasMore)", () => {
  it("loadOlder pages back from the oldest (ts, id) and prepends; a short page ends the history", async () => {
    const newest = page("n", 1000, 30);
    h.listChat.mockResolvedValueOnce(newest);
    const { result } = renderHook(() => useChat("b"));
    await waitFor(() => expect(result.current.messages).toHaveLength(30));
    expect(result.current.hasMore).toBe(true); // a full page → may be more

    // The older page (short: 2 < 30) — history exhausted after this.
    const older = [mkMsg("o1", "b", "owner", 500), mkMsg("o2", "owner", "b", 600)];
    h.listChat.mockResolvedValueOnce(older);
    await act(async () => {
      await result.current.loadOlder();
    });

    // The cursor is the pre-load OLDEST message's (ts, id), page size 30.
    expect(h.listChat).toHaveBeenLastCalledWith("b", 30, {
      beforeTs: 1000,
      beforeId: "n0",
    });
    // Prepended in front, order intact.
    expect(result.current.messages.slice(0, 3).map((m) => m.id)).toEqual([
      "o1",
      "o2",
      "n0",
    ]);
    expect(result.current.messages).toHaveLength(32);
    expect(result.current.hasMore).toBe(false);

    // Exhausted → a further loadOlder never hits the wire.
    const calls = h.listChat.mock.calls.length;
    await act(async () => {
      await result.current.loadOlder();
    });
    expect(h.listChat.mock.calls.length).toBe(calls);
  });

  it("a FIRST page shorter than the window means no history (hasMore=false)", async () => {
    h.listChat.mockResolvedValueOnce([mkMsg("c1", "b", "owner", 1000)]);
    const { result } = renderHook(() => useChat("b"));
    await waitFor(() => expect(result.current.messages).toHaveLength(1));
    expect(result.current.hasMore).toBe(false);
  });

  it("overlapping loadOlder calls are concurrency-locked to ONE cursor request", async () => {
    h.listChat.mockResolvedValueOnce(page("n", 1000, 30));
    const { result } = renderHook(() => useChat("b"));
    await waitFor(() => expect(result.current.messages).toHaveLength(30));

    let release!: (v: ChatMessage[]) => void;
    h.listChat.mockImplementationOnce(
      () => new Promise((res) => (release = res)),
    );
    await act(async () => {
      const first = result.current.loadOlder();
      const second = result.current.loadOlder(); // in-flight → no-op
      await second;
      release([mkMsg("o1", "b", "owner", 1)]);
      await first;
    });
    // Initial load + exactly ONE cursor page.
    expect(h.listChat).toHaveBeenCalledTimes(2);
    expect(result.current.messages[0].id).toBe("o1");
  });

  it("an SSE refetch MERGES the newest page — loaded history survives in front", async () => {
    const newest = page("n", 1000, 30);
    h.listChat.mockResolvedValueOnce(newest);
    const { result } = renderHook(() => useChat("b"));
    await waitFor(() => expect(result.current.messages).toHaveLength(30));

    h.listChat.mockResolvedValueOnce([mkMsg("o1", "b", "owner", 1)]);
    await act(async () => {
      await result.current.loadOlder();
    });
    expect(result.current.messages).toHaveLength(31);

    // A new message lands → SSE "chat" → the refetched newest page slides
    // (n1..n29 + fresh). The prepended o1 (and the slid-out n0) must survive.
    const slid = [...newest.slice(1), mkMsg("fresh", "b", "owner", 2000)];
    h.listChat.mockResolvedValueOnce(slid);
    act(() => h.sseHandler?.("chat"));
    await waitFor(() =>
      expect(
        result.current.messages[result.current.messages.length - 1].id,
      ).toBe("fresh"),
    );
    const ids = result.current.messages.map((m) => m.id);
    expect(ids).toHaveLength(32); // o1 + n0 + n1..n29 + fresh — nothing eaten
    expect(ids[0]).toBe("o1");
    expect(ids[1]).toBe("n0");
    // No duplicates from the merge.
    expect(new Set(ids).size).toBe(ids.length);
  });

  it("switching peers resets the scrollback window (hasMore re-derives)", async () => {
    h.listChat.mockImplementation(async (withId: string) =>
      withId === "b" ? page("n", 1000, 30) : [mkMsg("z1", "z", "owner", 1)],
    );
    const { result, rerender } = renderHook(
      ({ id }: { id: string }) => useChat(id),
      { initialProps: { id: "b" } },
    );
    await waitFor(() => expect(result.current.messages).toHaveLength(30));
    expect(result.current.hasMore).toBe(true);

    rerender({ id: "z" });
    await waitFor(() => expect(result.current.messagesPeer).toBe("z"));
    await waitFor(() =>
      expect(result.current.messages.map((m) => m.id)).toEqual(["z1"]),
    );
    expect(result.current.hasMore).toBe(false);
  });
});

describe("useChat anchor window (loadAround / loadNewer / resetToLatest)", () => {
  // 🔴 THE DEFECT THIS CLOSES. The thread only ever held the newest window, so
  // 跳到原訊息 could reach a message only if it happened to be in it. There was
  // no forward cursor at all — `before_ts`/`before_id` walks one way — so the
  // cockpit looked in the DOM, missed, and scrolled to the bottom.
  it("loadAround opens ONE window around the id — two requests, one each way, never the whole history", async () => {
    h.listChat.mockResolvedValueOnce(page("n", 9000, 30));
    const { result } = renderHook(() => useChat("b"));
    await waitFor(() => expect(result.current.messages).toHaveLength(30));

    const above = page("a", 100, 30); // full → history may continue above
    const below = [
      mkMsg("a29", "b", "owner", 129),
      mkMsg("t1", "b", "owner", 130),
    ]; // short → the live tail is inside this page
    h.listChatWindow.mockResolvedValueOnce(above).mockResolvedValueOnce(below);

    let found: boolean | undefined;
    await act(async () => {
      found = await result.current.loadAround("a29");
    });

    expect(found).toBe(true);
    // Both ends INCLUSIVE and both anchored on the SAME id: the context above
    // it and the context below it.
    expect(h.listChatWindow.mock.calls[0]).toEqual(["b", { endId: "a29" }, 30]);
    expect(h.listChatWindow.mock.calls[1]).toEqual([
      "b",
      { startId: "a29" },
      30,
    ]);
    expect(h.listChatWindow).toHaveBeenCalledTimes(2);
    // The target really is in the thread — that is the whole promise.
    expect(result.current.messages.map((m) => m.id)).toContain("a29");
    // The two pages are ONE window, de-duplicated on the shared anchor.
    expect(result.current.messages).toHaveLength(31);
    expect(new Set(result.current.messages.map((m) => m.id)).size).toBe(31);
    // A full page above ⇒ more history may exist; a short page below ⇒ this
    // window already reaches the live tail.
    expect(result.current.hasMore).toBe(true);
    expect(result.current.hasNewer).toBe(false);
  });

  it("a full page below means the live tail is BEYOND the window — and the newest-page refresh is suppressed while it is", async () => {
    // Without the suppression an SSE burst fetches the newest 30 rows and
    // merges them onto a window from the distant past: the unfetched range
    // between the two is drawn as contiguous, and the seam machinery reports
    // it as LOST messages.
    h.listChat.mockResolvedValueOnce(page("n", 9000, 30));
    const { result } = renderHook(() => useChat("b"));
    await waitFor(() => expect(result.current.messages).toHaveLength(30));

    h.listChatWindow
      .mockResolvedValueOnce(page("a", 100, 30))
      .mockResolvedValueOnce(page("a", 129, 30));
    await act(async () => {
      await result.current.loadAround("a0");
    });
    expect(result.current.hasNewer).toBe(true);

    const before = h.listChat.mock.calls.length;
    await act(async () => {
      h.sseHandler?.("chat");
      await new Promise((r) => setTimeout(r, 20));
    });
    expect(h.listChat.mock.calls.length).toBe(before);
  });

  it("loadNewer walks FORWARDS from the newest loaded row; a short page reaches the tail and hands the thread back to the ordinary refresh", async () => {
    h.listChat.mockResolvedValueOnce(page("n", 9000, 30));
    const { result } = renderHook(() => useChat("b"));
    await waitFor(() => expect(result.current.messages).toHaveLength(30));

    h.listChatWindow
      .mockResolvedValueOnce(page("a", 100, 30))
      .mockResolvedValueOnce(page("a", 129, 30));
    await act(async () => {
      await result.current.loadAround("a0");
    });
    const newestLoaded =
      result.current.messages[result.current.messages.length - 1].id;

    // A SHORT forward page — the anchor row we already hold plus two more.
    h.listChatWindow.mockResolvedValueOnce([
      mkMsg(newestLoaded, "b", "owner", 158),
      mkMsg("t1", "b", "owner", 159),
      mkMsg("t2", "b", "owner", 160),
    ]);
    const held = result.current.messages.length;
    await act(async () => {
      await result.current.loadNewer();
    });

    // The cursor is the newest loaded row, walking towards the NEWEST.
    expect(h.listChatWindow).toHaveBeenLastCalledWith(
      "b",
      { startId: newestLoaded },
      30,
    );
    // Appended, and the shared anchor row is not duplicated.
    expect(result.current.messages).toHaveLength(held + 2);
    expect(result.current.messages.map((m) => m.id).slice(-2)).toEqual([
      "t1",
      "t2",
    ]);
    expect(result.current.hasNewer).toBe(false);

    // …and now that the thread IS the live tail again, the refresh resumes.
    h.listChat.mockResolvedValueOnce([mkMsg("t2", "b", "owner", 160)]);
    const before = h.listChat.mock.calls.length;
    await act(async () => {
      h.sseHandler?.("chat");
      await new Promise((r) => setTimeout(r, 20));
    });
    expect(h.listChat.mock.calls.length).toBe(before + 1);
  });

  it("an id no message carries is reported as NOT FOUND and leaves the thread alone", async () => {
    // The server answers 404 rather than an empty page precisely so this stays
    // distinguishable from "a real window that happens to be empty".
    h.listChat.mockResolvedValueOnce(page("n", 9000, 30));
    const { result } = renderHook(() => useChat("b"));
    await waitFor(() => expect(result.current.messages).toHaveLength(30));

    h.listChatWindow.mockRejectedValue(new Error("404"));
    let found: boolean | undefined;
    await act(async () => {
      found = await result.current.loadAround("c-nope");
    });

    expect(found).toBe(false);
    expect(result.current.messages).toHaveLength(30);
    expect(result.current.hasNewer).toBe(false);
  });

  it("resetToLatest REPLACES the anchor window with the live newest one", async () => {
    h.listChat.mockResolvedValueOnce(page("n", 9000, 30));
    const { result } = renderHook(() => useChat("b"));
    await waitFor(() => expect(result.current.messages).toHaveLength(30));

    h.listChatWindow
      .mockResolvedValueOnce(page("a", 100, 30))
      .mockResolvedValueOnce(page("a", 129, 30));
    await act(async () => {
      await result.current.loadAround("a0");
    });
    expect(result.current.hasNewer).toBe(true);

    const live = page("z", 9000, 30);
    h.listChat.mockResolvedValueOnce(live);
    await act(async () => {
      await result.current.resetToLatest();
    });

    // REPLACED, not concatenated: the range between the historical window and
    // the live tail was never fetched, and a thread that draws it as contiguous
    // is the lie this whole seam exists to avoid.
    expect(result.current.messages.map((m) => m.id)).toEqual(
      live.map((m) => m.id),
    );
    expect(result.current.hasNewer).toBe(false);
  });
});
