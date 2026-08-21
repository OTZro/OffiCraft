// Mock adapter parity for 「回覆這則」 (T-4e95). Offline preview exists so the
// cockpit can be driven without a server; that is only worth anything if the
// mock REFUSES what the server refuses. A mock that accepted any reply_to would
// let someone build and demo a reply the real server rejects — the failure would
// surface later, on a real backend, to somebody else.
//
// Pinned here, against what server/ocserverd/api_chat.go actually does:
//   • the quoted message must EXIST — the ONE refusal;
//   • it does NOT have to be in the same conversation (owner ruling,
//     2026-08-21). This was a refusal on both sides until that date, and a mock
//     that still refused it would preview a product the server no longer is;
//   • the accepted case really stores the link and serves it back;
//   • EVERY read attaches `replyToChat`, built from the log at read time,
//     unconditionally — which is the whole of the current design.

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

  it("ACCEPTS a reply_to that points at another conversation", async () => {
    // The reversal, and the reason the whole redesign exists: the owner quotes a
    // line out of another conversation to step into it. This test read
    // `.rejects.toThrow(/another conversation/)` until 2026-08-21.
    const elsewhere = await mockApi.postChat({ to: "kye", body: "別條線" });

    const reply = await mockApi.postChat({
      to: "mira",
      body: "側向引用",
      replyTo: elsewhere.id,
    });
    expect(reply.replyTo).toBe(elsewhere.id);
    // …and the quote crossed with it, or the reply is a pointer to nothing the
    // reader can see.
    expect(reply.replyToChat?.content).toBe("別條線");
  });

  it("attaches replyToChat on EVERY read, with no condition", async () => {
    // 🔴 SERVER PARITY IS THE POINT OF THIS TEST. The server builds the quote in
    // servedChatMessageDTO, which every read door goes through; the mock builds
    // it in mockServedChatMessage for the same reason. A mock that only attached
    // it sometimes would make offline preview show a bug — a quote flickering
    // between present and absent — that the real product does not have.
    const quoted = await mockApi.postChat({ to: "mira", body: "被引用的那句" });
    const reply = await mockApi.postChat({
      to: "mira",
      body: "答案",
      replyTo: quoted.id,
    });

    // ① the POST echo
    expect(reply.replyToChat).toEqual({
      id: quoted.id,
      from: "owner",
      fromName: "",
      content: "被引用的那句",
    });
    // ② the listing, and ③ the read-only peek — both doors, both unconditional,
    // and note the quoted message is IN both windows: "it is already here" is
    // not a reason to skip it.
    for (const [door, rows] of [
      ["listChat", await mockApi.listChat("mira")],
      ["peekChat", await mockApi.peekChat("mira")],
    ] as const) {
      const row = rows.find((m) => m.id === reply.id)!;
      expect(row, `${door} must carry the reply`).toBeTruthy();
      expect(row.replyToChat?.content, `${door} must carry the quote`).toBe(
        "被引用的那句",
      );
      // …and a message that replies to nothing claims no quote, or a mock that
      // stamped every row would pass the line above.
      expect(rows.find((m) => m.id === quoted.id)?.replyToChat ?? null).toBeNull();
    }
  });

  it("shortens and flattens the quote content the way the server does", async () => {
    // The length is the SERVER's (chatReplyQuoteMaxChars = 60) and the mock
    // holds the only other copy of it. Asserted by rune count, not by a literal,
    // so it stays true for the CJK this studio is mostly written in.
    const quoted = await mockApi.postChat({
      to: "mira",
      body: "長".repeat(90) + "\n\n" + "話".repeat(30),
    });
    const reply = await mockApi.postChat({
      to: "mira",
      body: "tl;dr",
      replyTo: quoted.id,
    });
    const content = reply.replyToChat!.content;
    expect(content).not.toMatch(/[\n\r]/);
    expect([...content]).toHaveLength(61); // 60 + the ellipsis
    expect(content.endsWith("\u2026")).toBe(true);
  });
});
