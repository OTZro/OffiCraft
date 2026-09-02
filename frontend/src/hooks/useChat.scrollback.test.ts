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
import type { JumpOutcome } from "./useChat";

/** A promise the test resolves by hand — the only way to hold a request in
 * flight and land something else on top of it, which is what every race below
 * is about. */
function deferred<T>() {
  let resolve!: (v: T) => void;
  const promise = new Promise<T>((r) => {
    resolve = r;
  });
  return { promise, resolve };
}

/** Let the microtask queue (and the SSE sink's own tick) drain. */
const settle = () => new Promise((r) => setTimeout(r, 20));

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

    let outcome: JumpOutcome | undefined;
    await act(async () => {
      outcome = await result.current.loadAround("a29");
    });

    expect(outcome).toBe("found");
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
    let outcome: JumpOutcome | undefined;
    await act(async () => {
      outcome = await result.current.loadAround("c-nope");
    });

    expect(outcome).toBe("missing");
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

  // ───────────────────────────────────────────────────────────────────────────
  // ANCHOR-FIRST ENTRY (T-48, owner ruling). Arriving through a jump link no
  // longer loads the live tail and then throws it away.
  // ───────────────────────────────────────────────────────────────────────────

  it("entering AT an anchor fetches the window around it and never a newest page first", async () => {
    // 🔴 The measured defect: entry fired `GET /api/chat?with=` and the anchor
    // window replaced it tens of ms later. One wasted round-trip, and a real
    // intermediate screen showing the live tail to a reader on their way
    // somewhere else — the screen every mark-read patch downstream exists to
    // hold back.
    const above = page("a", 100, 30); // full → history continues above
    const below = [mkMsg("a29", "b", "owner", 129), mkMsg("t1", "b", "owner", 130)];
    h.listChatWindow.mockResolvedValueOnce(above).mockResolvedValueOnce(below);

    const { result } = renderHook(() => useChat("b", "a29"));
    // The subscription is up (receipts were pulled) and yet NOTHING asked for
    // the newest page.
    await waitFor(() => expect(h.listChatReads).toHaveBeenCalled());
    expect(h.listChat).not.toHaveBeenCalled();

    let outcome: JumpOutcome | undefined;
    await act(async () => {
      outcome = await result.current.loadAround("a29");
    });
    expect(outcome).toBe("found");
    expect(result.current.messages.map((m) => m.id)).toContain("a29");
    // Still not one newest page: the FIRST request this room made was the
    // window around the target, and it was the only kind it needed.
    expect(h.listChat).not.toHaveBeenCalled();

    // …and the hold-off is released the moment the anchor lands, or the room
    // would never refresh again (this window already reaches the live tail).
    await act(async () => {
      h.sseHandler?.("chat");
      await settle();
    });
    expect(h.listChat).toHaveBeenCalledTimes(1);
  });

  it("entering WITHOUT an anchor is the ordinary entry, unchanged: one newest page and no window request", async () => {
    // The other half of the ruling, and the one worth a guard of its own: the
    // anchor-first path must be reachable ONLY from a jump. Every ordinary
    // entry — the overwhelming majority — has to be byte-for-byte what it was.
    h.listChat.mockResolvedValueOnce(page("n", 9000, 30));
    const { result } = renderHook(() => useChat("b"));
    await waitFor(() => expect(result.current.messages).toHaveLength(30));

    expect(h.listChat).toHaveBeenCalledTimes(1);
    expect(h.listChat.mock.calls[0]).toEqual(["b"]);
    expect(h.listChatWindow).not.toHaveBeenCalled();
    expect(result.current.hasNewer).toBe(false);

    // and the refresh is live from the start.
    await act(async () => {
      h.sseHandler?.("chat");
      await settle();
    });
    expect(h.listChat).toHaveBeenCalledTimes(2);
  });

  it("an SSE burst arriving DURING the anchor fetch does not overtake it with a newest page", async () => {
    // 🔴 F3's cause, not its symptom. A newest-page load started after the
    // anchor takes a HIGHER generation ticket and can commit first; the anchor
    // is then dropped as superseded, and the reader is told the message was
    // probably cleared. Holding the ordinary refresh for the two round-trips is
    // cheaper than any way of apologising afterwards.
    const above = deferred<ChatMessage[]>();
    const below = deferred<ChatMessage[]>();
    h.listChatWindow
      .mockReturnValueOnce(above.promise)
      .mockReturnValueOnce(below.promise);

    const { result } = renderHook(() => useChat("b", "a0"));
    await waitFor(() => expect(h.listChatReads).toHaveBeenCalled());

    let pending!: Promise<JumpOutcome>;
    act(() => {
      pending = result.current.loadAround("a0");
    });
    await act(async () => {
      h.sseHandler?.("chat");
      await settle();
    });
    expect(h.listChat).not.toHaveBeenCalled();

    above.resolve(page("a", 100, 30));
    below.resolve([mkMsg("a0", "b", "owner", 100), mkMsg("t1", "b", "owner", 131)]);
    let outcome: JumpOutcome | undefined;
    await act(async () => {
      outcome = await pending;
    });
    expect(outcome).toBe("found");
  });

  it("回到最新 out of an anchor-first entry hands the room back to the LIVE refresh", async () => {
    // The second line that may not break: an anchor window must never be a dead
    // end. If the hold-off outlived the anchor this conversation would stop
    // refreshing for the rest of the session — silently, and only for the
    // people who arrived through a jump link.
    h.listChatWindow
      .mockResolvedValueOnce(page("a", 100, 30))
      .mockResolvedValueOnce(page("a", 129, 30)); // full → live tail is below
    const { result } = renderHook(() => useChat("b", "a0"));
    await act(async () => {
      await result.current.loadAround("a0");
    });
    expect(result.current.hasNewer).toBe(true);

    const live = page("z", 9000, 30);
    h.listChat.mockResolvedValueOnce(live);
    await act(async () => {
      await result.current.resetToLatest();
    });
    expect(result.current.messages.map((m) => m.id)).toEqual(
      live.map((m) => m.id),
    );

    const before = h.listChat.mock.calls.length;
    await act(async () => {
      h.sseHandler?.("chat");
      await settle();
    });
    expect(
      h.listChat.mock.calls.length,
      "after 回到最新 the ordinary refresh must be live again",
    ).toBe(before + 1);
  });

  it("回到最新 while the anchor is STILL IN THE AIR does not leave the room without a refresh", async () => {
    // The nastiest shape of the same line, and the one the tidy version misses:
    // the anchor never settles because the owner overtook it, so the flag that
    // holds the refresh off is not cleared by the anchor landing — it has to be
    // cleared by 回到最新 itself, or this room never fetches again and looks
    // merely quiet while doing it.
    const above = deferred<ChatMessage[]>();
    const below = deferred<ChatMessage[]>();
    h.listChatWindow
      .mockReturnValueOnce(above.promise)
      .mockReturnValueOnce(below.promise);
    const { result } = renderHook(() => useChat("b", "a0"));
    await waitFor(() => expect(h.listChatReads).toHaveBeenCalled());
    let pending!: Promise<JumpOutcome>;
    act(() => {
      pending = result.current.loadAround("a0");
    });

    const live = page("z", 9000, 30);
    h.listChat.mockResolvedValueOnce(live);
    await act(async () => {
      await result.current.resetToLatest();
    });
    above.resolve(page("a", 100, 30));
    below.resolve(page("a", 129, 30));
    await act(async () => {
      expect(await pending).toBe("superseded");
    });

    const before = h.listChat.mock.calls.length;
    h.listChat.mockResolvedValueOnce(live);
    await act(async () => {
      h.sseHandler?.("chat");
      await settle();
    });
    expect(
      h.listChat.mock.calls.length,
      "an anchor that never landed must not silence this room for good",
    ).toBe(before + 1);
  });

  it("a MID-SESSION jump is not overtaken by an SSE burst either", async () => {
    // The entry hold-off does not cover this one: the room is already the live
    // tail (nothing pending) and the owner jumps from inside it — 請示卡's
    // 跳到原訊息 while the conversation is open. A newest-page load starting
    // inside those two round-trips takes a higher ticket and commits first, and
    // the jump is then reported to the reader as a message that is not there.
    h.listChat.mockResolvedValueOnce(page("n", 9000, 30));
    const { result } = renderHook(() => useChat("b"));
    await waitFor(() => expect(result.current.messages).toHaveLength(30));
    const before = h.listChat.mock.calls.length;

    const above = deferred<ChatMessage[]>();
    const below = deferred<ChatMessage[]>();
    h.listChatWindow
      .mockReturnValueOnce(above.promise)
      .mockReturnValueOnce(below.promise);
    let pending!: Promise<JumpOutcome>;
    act(() => {
      pending = result.current.loadAround("a0");
    });
    await act(async () => {
      h.sseHandler?.("chat");
      await settle();
    });
    expect(
      h.listChat.mock.calls.length,
      "no newest page may start while the anchor pair is in the air",
    ).toBe(before);

    above.resolve(page("a", 100, 30));
    below.resolve([mkMsg("a0", "b", "owner", 100), mkMsg("t1", "b", "owner", 131)]);
    let outcome: JumpOutcome | undefined;
    await act(async () => {
      outcome = await pending;
    });
    expect(outcome).toBe("found");
    expect(result.current.messages.map((m) => m.id)).toContain("a0");
  });

  // ───────────────────────────────────────────────────────────────────────────
  // The three ways this seam used to fail silently.
  // ───────────────────────────────────────────────────────────────────────────

  it("an id that exists but belongs to ANOTHER conversation is a miss, not an empty room", async () => {
    // 🔴 F1 — a reachable 200, not a defensive branch. The server resolves the
    // anchor WITHOUT the participant filter on purpose ("a window anchored
    // outside it simply comes back empty, which is the honest answer"), so a
    // real message id from a DIFFERENT thread answers both calls 200 + [].
    // Adopting that window writes `messages: []`: the room goes blank, the miss
    // notice does not light, and the console says nothing either.
    h.listChat.mockResolvedValueOnce(page("n", 9000, 30));
    const { result } = renderHook(() => useChat("b"));
    await waitFor(() => expect(result.current.messages).toHaveLength(30));

    h.listChatWindow.mockResolvedValueOnce([]).mockResolvedValueOnce([]);
    let outcome: JumpOutcome | undefined;
    await act(async () => {
      outcome = await result.current.loadAround("c-someone-elses");
    });

    expect(outcome).toBe("missing");
    // The thread the owner was reading is left exactly as it was.
    expect(result.current.messages).toHaveLength(30);
    expect(result.current.messages.map((m) => m.id)).toEqual(
      page("n", 9000, 30).map((m) => m.id),
    );
    expect(result.current.hasNewer).toBe(false);
  });

  it("being OVERTAKEN is reported as superseded, never as a missing message", async () => {
    // 🔴 F3. `loadAround` used to answer false for three different facts — 404,
    // a failed request, and "a newer load committed while we were in the air".
    // ChatArea has one branch for false, so the third one put 「找不到那則訊息,
    // 可能已經被清掉了」 on screen about a message that is still there, with the
    // fetch latch already spent: no retry, no button, jump silently abandoned.
    h.listChat.mockResolvedValueOnce(page("n", 9000, 30));
    const { result } = renderHook(() => useChat("b"));
    await waitFor(() => expect(result.current.messages).toHaveLength(30));

    const above = deferred<ChatMessage[]>();
    const below = deferred<ChatMessage[]>();
    h.listChatWindow
      .mockReturnValueOnce(above.promise)
      .mockReturnValueOnce(below.promise);
    let pending!: Promise<JumpOutcome>;
    act(() => {
      pending = result.current.loadAround("a0");
    });

    // The owner presses 回到最新 while the anchor pair is still in the air.
    const live = page("z", 9000, 30);
    h.listChat.mockResolvedValueOnce(live);
    await act(async () => {
      await result.current.resetToLatest();
    });

    above.resolve(page("a", 100, 30));
    below.resolve(page("a", 129, 30));
    let outcome: JumpOutcome | undefined;
    await act(async () => {
      outcome = await pending;
    });

    expect(outcome).toBe("superseded");
    // …and the window that lost the race did not land on top of the winner.
    expect(result.current.messages.map((m) => m.id)).toEqual(
      live.map((m) => m.id),
    );
    expect(result.current.hasNewer).toBe(false);
  });

  it("a forward page that lands AFTER 回到最新 is dropped, not appended under the live tail", async () => {
    // 🔴 F2. `loadingNewerRef` is a same-direction mutex and nothing else, so
    // the forward walk was the one loader in this file with no generation
    // ticket. Measured on the unguarded code:
    //
    //   len 60 head ['z0','z1','z2'] tail ['mid27','mid28','mid29']
    //   hasNewer true gapSuspected false
    //
    // 30 history rows drawn BELOW the newest ones, no gap notice — and
    // `hasNewer` back to true, which re-arms the anchor gate on load() and pins
    // mayMarkRead false: that conversation stops marking itself read for good.
    h.listChatWindow
      .mockResolvedValueOnce(page("a", 100, 30))
      .mockResolvedValueOnce(page("a", 129, 30));
    const { result } = renderHook(() => useChat("b", "a0"));
    await act(async () => {
      await result.current.loadAround("a0");
    });
    expect(result.current.hasNewer).toBe(true);

    const forward = deferred<ChatMessage[]>();
    h.listChatWindow.mockReturnValueOnce(forward.promise);
    let walking!: Promise<void>;
    act(() => {
      walking = result.current.loadNewer();
    });

    // Scrolled to the bottom of the window, saw it was not the latest, pressed
    // the arrow — the most natural gesture order there is.
    const live = page("z", 9000, 30);
    h.listChat.mockResolvedValueOnce(live);
    await act(async () => {
      await result.current.resetToLatest();
    });

    forward.resolve(page("mid", 158, 30));
    await act(async () => {
      await walking;
    });

    expect(result.current.messages.map((m) => m.id)).toEqual(
      live.map((m) => m.id),
    );
    expect(
      result.current.hasNewer,
      "a stale forward page must not re-arm the anchor gate",
    ).toBe(false);
    expect(result.current.gapSuspected).toBe(false);
  });
});
