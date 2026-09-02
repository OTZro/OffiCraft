// useChat — "reading requires looking" (badge-flash fix) black-box pins.
//
// The chat thread must stay fresh through BOTH window states, and since T-48
// it does so through ONE door: `listChat` marks nothing read, so a
// backgrounded window loads exactly like an active one — messages keep
// flowing, unread keeps counting — and the badge clears only when ChatArea
// calls markRead, which it does when the owner is really looking. (A separate
// `peekChat` existed for the background case while a cursorless list still
// advanced the watermark; that side effect is gone and so is the second door.)

import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { renderHook, act, waitFor } from "@testing-library/react";
import type { ChatMessage } from "../api/adapter";

const h = vi.hoisted(() => {
  return {
    listChat: vi.fn<(withId: string, limit?: number) => Promise<unknown[]>>(),
    listChatReads: vi.fn(async (_peer: string) => [] as unknown[]),
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

import { OWNER_ID } from "../lib/ownerUnread";
import { useChat } from "./useChat";

function mkMsg(id: string, from: string, to: string, ts: number): ChatMessage {
  return { id, from, to, body: `msg ${id}`, ts, attachments: [], replyCardId: null };
}

let hasFocusSpy: ReturnType<typeof vi.spyOn>;

beforeEach(() => {
  h.listChat.mockReset().mockResolvedValue([]);
  h.listChatReads.mockReset().mockResolvedValue([]);
  h.markChatRead.mockClear();
  h.sseHandler = null;
  // jsdom is "visible" by default; drive activity through hasFocus.
  hasFocusSpy = vi.spyOn(document, "hasFocus").mockReturnValue(true);
});

afterEach(() => {
  hasFocusSpy.mockRestore();
});

describe("useChat load routing (active vs background)", () => {
  it("an ACTIVE window loads through listChat", async () => {
    h.listChat.mockResolvedValue([mkMsg("c1", "b", "owner", 1000)]);
    const { result } = renderHook(() => useChat("b"));
    await waitFor(() => expect(result.current.messages).toHaveLength(1));
    expect(h.listChat).toHaveBeenCalledWith("b");
    expect(result.current.messagesPeer).toBe("b");
  });

  it("a BACKGROUNDED window loads through the SAME listChat — messages still flow", async () => {
    hasFocusSpy.mockReturnValue(false);
    h.listChat.mockResolvedValue([mkMsg("c1", "b", "owner", 1000)]);
    const { result } = renderHook(() => useChat("b"));
    await waitFor(() => expect(result.current.messages).toHaveLength(1));
    // The thread stays fresh with no watermark side effect — the load never
    // had one to skip, so there is no second door to take.
    expect(h.listChat).toHaveBeenCalledWith("b");
  });

  it("an SSE 'chat' event refetches in EITHER window state", async () => {
    const { result } = renderHook(() => useChat("b"));
    await waitFor(() => expect(h.listChat).toHaveBeenCalledTimes(1));

    // Background → the refetch still runs, and still consumes no unread state.
    hasFocusSpy.mockReturnValue(false);
    h.listChat.mockResolvedValue([mkMsg("c2", "b", "owner", 2000)]);
    act(() => h.sseHandler?.("chat"));
    await waitFor(() => expect(h.listChat).toHaveBeenCalledTimes(2));
    // The new inbound message still landed (訊息更新不能斷).
    await waitFor(() => expect(result.current.messages).toHaveLength(1));
    expect(result.current.messages[0].id).toBe("c2");

    hasFocusSpy.mockReturnValue(true);
    act(() => h.sseHandler?.("chat"));
    await waitFor(() => expect(h.listChat).toHaveBeenCalledTimes(3));
  });

  it("returning to the foreground re-loads the thread", async () => {
    hasFocusSpy.mockReturnValue(false);
    renderHook(() => useChat("b"));
    await waitFor(() => expect(h.listChat).toHaveBeenCalledTimes(1));

    hasFocusSpy.mockReturnValue(true);
    act(() => {
      window.dispatchEvent(new Event("focus"));
    });
    await waitFor(() => expect(h.listChat).toHaveBeenCalledTimes(2));
    expect(h.listChat).toHaveBeenCalledWith("b");
  });

  it("a blur that leaves the window inactive does NOT trigger a load", async () => {
    renderHook(() => useChat("b"));
    await waitFor(() => expect(h.listChat).toHaveBeenCalledTimes(1));

    hasFocusSpy.mockReturnValue(false);
    act(() => {
      window.dispatchEvent(new Event("blur"));
      document.dispatchEvent(new Event("visibilitychange"));
    });
    // No additional load was fired by the deactivation itself.
    expect(h.listChat).toHaveBeenCalledTimes(1);
  });

  it("switching peers resets the thread and re-loads for the new peer", async () => {
    h.listChat.mockImplementation(async (withId: string) =>
      withId === "b"
        ? [mkMsg("c1", "b", "owner", 1000)]
        : [mkMsg("c9", "z", "owner", 2000)],
    );
    const { result, rerender } = renderHook(
      ({ id }: { id: string }) => useChat(id),
      { initialProps: { id: "b" } },
    );
    await waitFor(() => expect(result.current.messages).toHaveLength(1));
    expect(result.current.messagesPeer).toBe("b");

    rerender({ id: "z" });
    await waitFor(() => expect(result.current.messagesPeer).toBe("z"));
    await waitFor(() =>
      expect(result.current.messages.map((m) => m.id)).toEqual(["c9"]),
    );
  });

  // T-4e95 review r12 — the POST is the send; the refresh after it is not.
  it("a send whose POST succeeded RESOLVES even when the refresh behind it fails", async () => {
    // `refetch` calls listChat unguarded, so a blip on the refresh used to
    // reject send() — and the caller cannot tell that apart from "the message
    // never left". ChatArea acts on that: it restores the message into the
    // room's DRAFT, which outlives the page, so the owner returns to a composer
    // holding a line that is already in the thread and Enter sends it twice.
    h.listChat.mockResolvedValueOnce([]); // the initial load
    h.listChat.mockRejectedValueOnce(new Error("network blip")); // the post-send refresh
    const { result } = renderHook(() => useChat("b"));
    await waitFor(() => expect(h.listChat).toHaveBeenCalledTimes(1));

    await act(async () => {
      // Must NOT throw. `.rejects` would pass on a resolve-with-error too, so
      // the assertion is the await itself completing.
      await result.current.send("已經送出去的話");
    });

    expect(h.postChat).toHaveBeenCalledTimes(1);
  });

  // T-4e95 r16 — the hook's own half of the reply link. `send`'s third argument
  // is the only way a reply target reaches the wire, and dropping it here left
  // all 2258 tests green: the banner still shows, the send still succeeds, and
  // the server stores an ordinary message. ChatArea's tests cannot see it —
  // they mock this very hook.
  it("passes the reply target through to postChat, and omits it when there is none", async () => {
    h.listChat.mockResolvedValue([]);
    const { result } = renderHook(() => useChat("b"));
    await waitFor(() => expect(h.listChat).toHaveBeenCalledTimes(1));

    await act(async () => {
      await result.current.send("回你這句", undefined, "c-1");
    });
    expect(h.postChat).toHaveBeenLastCalledWith(
      expect.objectContaining({ to: "b", body: "回你這句", replyTo: "c-1" }),
    );

    // The other direction: an ordinary message must not carry one.
    await act(async () => {
      await result.current.send("普通訊息");
    });
    expect(h.postChat).toHaveBeenLastCalledWith(
      expect.objectContaining({ replyTo: undefined }),
    );
  });

  // The OTHER half of the same contract, and the one nothing was standing on:
  // every restore this feature does rests on "a send that really failed
  // rejects". A reviewer pulled the POST itself inside the try/catch added
  // above and all 2248 tests stayed green — a message that never left would
  // then vanish with no restore, no draft and not even a console.warn. This is
  // the assertion that makes that a red.
  // T-48 ④ 已讀勾. `peerLastReadTs` is the ONLY input to the outgoing rows'
  // 已讀 tick, and until now nothing in the repo exercised how it is derived —
  // every ChatArea test hard-codes it to 0. So the hook could read the wrong
  // ROW of the right table and stay green forever, which is exactly what it
  // did: `?with=<peer>` is `WHERE peer_id = <peer>`, and the receipt this hook
  // needs has `peer_id = owner`.
  it("reads the peer's watermark off the receipts about the OWNER, not about the peer", async () => {
    h.listChat.mockResolvedValue([mkMsg("c1", "owner", "b", 1000)]);
    h.listChatReads.mockImplementation(async (peer: string) => {
      if (peer === OWNER_ID) {
        return [
          // The row that matters: the PEER read the owner's messages to 4242.
          { readerId: "b", peerId: OWNER_ID, lastReadTs: 4242 },
          // Another member's watermark against the owner — same query, wrong
          // reader. Picking the first row instead of matching readerId is a
          // mutant this fixture reddens.
          { readerId: "z", peerId: OWNER_ID, lastReadTs: 9999 },
        ];
      }
      // What `?with=<peer>` really answers: the OWNER's own watermark for this
      // conversation, plus the (X,X) self row polling used to mint. Neither is
      // "how far the peer has read me"; reading either is the bug.
      return [
        { readerId: OWNER_ID, peerId: "b", lastReadTs: 1000 },
        { readerId: "b", peerId: "b", lastReadTs: 7777 },
      ];
    });

    const { result } = renderHook(() => useChat("b"));
    await waitFor(() => expect(result.current.peerLastReadTs).toBe(4242));
    expect(h.listChatReads).toHaveBeenCalledWith(OWNER_ID);
    expect(h.listChatReads).not.toHaveBeenCalledWith("b");
  });

  it("a peer with no receipt against the owner has read nothing", async () => {
    h.listChat.mockResolvedValue([mkMsg("c1", "owner", "b", 1000)]);
    h.listChatReads.mockResolvedValue([
      { readerId: "z", peerId: OWNER_ID, lastReadTs: 9999 },
    ]);
    const { result } = renderHook(() => useChat("b"));
    await waitFor(() => expect(h.listChat).toHaveBeenCalledTimes(1));
    expect(result.current.peerLastReadTs).toBe(0);
  });

  it("切走再切回,上一間房晚到的已讀水位不准畫成這一間的已讀勾", async () => {
    // 🔴 T-48 R8-2。這一格以前被文件豁免掉,理由是「同一個人的資料只是舊一格」——
    // 那個前提不成立:訂閱 effect 一進房就打 `void refetchReads()`,而
    // `peerLastReadTs` 是同一支 useState(ChatArea 換人不會 remount),閉包捕獲的
    // `withId` 是**那一間**的人。所以進 B、reads 還在路上、馬上切回 A,B 那通落地
    // 就把 B 的水位寫進 A 的房間 —— 不是退一格,是憑別人的水位在 A 的訊息上點亮
    // 已讀勾,而且要等下一則 chat_read delta 或下一次進房才會蓋回來。
    // 一次手滑就到得了,是這一族裡最好觸發的一條。
    h.listChat.mockResolvedValue([mkMsg("c1", "owner", "a", 1000)]);
    let landB!: (rows: unknown[]) => void;
    h.listChatReads
      .mockImplementationOnce(async () => [
        { readerId: "a", peerId: OWNER_ID, lastReadTs: 100 },
      ])
      .mockImplementationOnce(
        () =>
          new Promise<unknown[]>((r) => {
            landB = r;
          }),
      )
      .mockImplementation(async () => [
        { readerId: "a", peerId: OWNER_ID, lastReadTs: 100 },
      ]);

    const { result, rerender } = renderHook(({ id }) => useChat(id), {
      initialProps: { id: "a" },
    });
    await waitFor(() => expect(result.current.peerLastReadTs).toBe(100));

    await act(async () => {
      rerender({ id: "b" });
      await new Promise((r) => setTimeout(r, 10));
    });
    await act(async () => {
      rerender({ id: "a" });
      await new Promise((r) => setTimeout(r, 10));
    });
    expect(result.current.peerLastReadTs, "前提:回到 A 的這一趟拿到的是 A 的水位").toBe(100);

    await act(async () => {
      landB([
        { readerId: "b", peerId: OWNER_ID, lastReadTs: 999999 },
        { readerId: "a", peerId: OWNER_ID, lastReadTs: 100 },
      ]);
      await new Promise((r) => setTimeout(r, 10));
    });
    expect(result.current.peerLastReadTs, "A 的房間不准顯示 B 的已讀水位").toBe(
      100,
    );
  });

  it("a send whose POST FAILED still rejects — the caller must be able to tell", async () => {
    h.listChat.mockResolvedValueOnce([]); // the initial load
    h.postChat.mockRejectedValueOnce(new Error("server said no"));
    const { result } = renderHook(() => useChat("b"));
    await waitFor(() => expect(h.listChat).toHaveBeenCalledTimes(1));

    await expect(result.current.send("沒送出去的話")).rejects.toThrow(
      "server said no",
    );
  });
});
