// Mock adapter parity for 「回覆這則」 (T-4e95). Offline preview exists so the
// cockpit can be driven without a server; that is only worth anything if the
// mock REFUSES what the server refuses. A mock that accepted any reply_to would
// let someone build and demo a reply the real server rejects — the failure would
// surface later, on a real backend, to somebody else.
//
// Pinned here, against the two refusals server/ocserverd/api_chat.go performs:
//   • the quoted message must EXIST;
//   • it must be in the SAME conversation as the message being posted;
//   • the accepted case really stores the link and serves it back;
//   • the by-ids read is caller-blind and all-or-nothing, like the server's.

import { describe, it, expect, beforeEach } from "vitest";
import { mockApi, __resetMock } from "./mock";

describe("mock 回覆這則 — server parity", () => {
  beforeEach(() => __resetMock());

  it("stores the link on the accepted case and serves it back", async () => {
    const quoted = await mockApi.postChat({ to: "mira", body: "問題" });
    expect(quoted.replyTo ?? null).toBeNull();

    const reply = await mockApi.postChat({
      to: "mira",
      body: "答案",
      replyTo: quoted.id,
    });
    expect(reply.replyTo).toBe(quoted.id);

    const thread = await mockApi.peekChat("mira");
    expect(thread.find((m) => m.id === reply.id)?.replyTo).toBe(quoted.id);
  });

  // Found while writing the test above, and it is this feature's problem rather
  // than a tidy-up: ids used to be `mock-${Date.now()}`, so two posts inside one
  // millisecond shared an id. Nothing pointed AT a message before, so it never
  // surfaced; a reply carries the quoted id and NOTHING else, so an ambiguous id
  // resolves the quote to whichever row sorts first.
  it("mints a distinct id per message even within one millisecond", async () => {
    const posted = await Promise.all(
      Array.from({ length: 5 }, (_, i) =>
        mockApi.postChat({ to: "mira", body: `第 ${i} 則` }),
      ),
    );
    expect(new Set(posted.map((m) => m.id)).size).toBe(posted.length);
  });

  it("refuses a reply_to that names no message", async () => {
    await expect(
      mockApi.postChat({ to: "mira", body: "孤兒", replyTo: "mock-nosuch" }),
    ).rejects.toThrow(/mock-nosuch/);

    // …and the refused message is not in the thread afterwards.
    const thread = await mockApi.peekChat("mira");
    expect(thread.some((m) => m.body === "孤兒")).toBe(false);
  });

  it("refuses a reply_to that points at another conversation", async () => {
    const elsewhere = await mockApi.postChat({ to: "kye", body: "別條線" });

    await expect(
      mockApi.postChat({ to: "mira", body: "側向引用", replyTo: elsewhere.id }),
    ).rejects.toThrow(/another conversation/);
  });

  it("listChatByIds is caller-blind and all-or-nothing, like the server", async () => {
    const a = await mockApi.postChat({ to: "mira", body: "一" });
    const b = await mockApi.postChat({ to: "kye", body: "二" });

    // Two DIFFERENT conversations answered in one call: the by-ids read is not
    // narrowed to any one thread (server parity since T-4e95).
    const rows = await mockApi.listChatByIds([a.id, b.id]);
    expect(rows.map((m) => m.id).sort()).toEqual([a.id, b.id].sort());

    // One unknown id refuses the WHOLE call — a short array would be
    // indistinguishable from "that message is gone".
    await expect(mockApi.listChatByIds([a.id, "mock-gone"])).rejects.toThrow(
      /mock-gone/,
    );
  });
});
