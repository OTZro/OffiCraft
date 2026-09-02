// Mock adapter unread parity (M2-1, count badge): the mock computes
// `member.unreadCount` with the SAME watermark-inverse rule the backend applies
// (server-side unread fold on the roster read), so the mock and http
// adapters agree by construction:
//
//   1. only messages ADDRESSED TO the owner count — an agent↔agent message
//      never counts (AC #1);
//   2. NOTHING about reading clears the count any more (T-48): entering the
//      conversation (listChat / peekChat) leaves the watermark alone, and the
//      ONLY thing that advances it is the explicit markChatRead choke — the
//      mock mirrors the BE, where GET /api/chat stopped writing a watermark on
//      every path (owner ruling 2026-09-02);
//   3. once marked, the count clears past EVERY message at or below the mark
//      (AC #2), and a message that lands AFTER the mark counts again (AC #3);
//   4. the count is presence-independent — the mock's members are all OFFLINE
//      and still carry a count (AC #4);
//   5. the count is PER MESSAGE — three waiting messages read as 3, not "some".
//
// Inbound member→owner messages are injected via the test-only __injectMockChat
// hook (the honest mock never fabricates a member reply on its own).

import { describe, it, expect, beforeEach } from "vitest";
import { mockApi, __resetMock, __injectMockChat } from "./mock";
import { MOCK_OWNER_ID } from "./seeds";

async function miraUnread(): Promise<{
  unreadCount: number;
  lifecycle: string;
}> {
  const members = await mockApi.listMembers();
  const mira = members.find((m) => m.id === "mira");
  if (!mira) throw new Error("mock roster lost mira");
  return { unreadCount: mira.unreadCount, lifecycle: mira.lifecycle };
}

function inbound(from: string, to: string, ts: number) {
  __injectMockChat({
    id: `t-${from}-${ts}`,
    from,
    to,
    body: "hi",
    ts,
    attachments: [],
    replyCardId: null,
  });
}

describe("mock adapter unread parity", () => {
  beforeEach(() => {
    __resetMock();
  });

  it("an OFFLINE member with a message to the owner is unread (AC #4)", async () => {
    inbound("mira", MOCK_OWNER_ID, 1000);
    const mira = await miraUnread();
    expect(mira.lifecycle).toBe("offline"); // presence untouched — separate axis
    expect(mira.unreadCount).toBe(1);
  });

  it("an agent↔agent message never counts for the owner (AC #1)", async () => {
    inbound("mira", "joey", 1000); // coordination between agents, not to owner
    expect((await miraUnread()).unreadCount).toBe(0);
  });

  it("counts per message; listing does NOT clear, mark-read clears ALL (AC #2/#5)", async () => {
    inbound("mira", MOCK_OWNER_ID, 1000);
    inbound("mira", MOCK_OWNER_ID, 1001);
    inbound("mira", MOCK_OWNER_ID, 1002);
    expect((await miraUnread()).unreadCount).toBe(3);
    // T-48: the FE's open-thread call no longer marks anything read. This used
    // to assert 0 here; asserting 3 is what makes the removal load-bearing.
    await mockApi.listChat("mira");
    expect((await miraUnread()).unreadCount).toBe(3);
    await mockApi.markChatRead({ peer: "mira", lastReadTs: 1002 });
    expect((await miraUnread()).unreadCount).toBe(0);
  });

  it("a new message after the mark counts again; a refetch does not swallow it (AC #3)", async () => {
    inbound("mira", MOCK_OWNER_ID, 1000);
    await mockApi.markChatRead({ peer: "mira", lastReadTs: 1000 });
    expect((await miraUnread()).unreadCount).toBe(0);
    inbound("mira", MOCK_OWNER_ID, 2000); // new message lands
    await mockApi.listChat("mira"); // the SSE-driven refetch
    // T-48: the refetch used to consume this silently. It must not.
    expect((await miraUnread()).unreadCount).toBe(1);
  });

  it("the explicit mark-read choke clears identically", async () => {
    inbound("mira", MOCK_OWNER_ID, 1000);
    await mockApi.markChatRead({ peer: "mira", lastReadTs: 1000 });
    expect((await miraUnread()).unreadCount).toBe(0);
  });

  it("peekChat and listChat return the SAME thread and NEITHER clears the count", async () => {
    // The badge-flash seam is now the whole contract: no read door consumes
    // unread state. peekChat and listChat differ in name only (T-48 removed
    // the ?peek= opt-out because there is nothing left to opt out of).
    inbound("mira", MOCK_OWNER_ID, 1000);
    inbound("mira", MOCK_OWNER_ID, 1001);
    const peeked = await mockApi.peekChat("mira");
    expect(peeked).toHaveLength(2);
    expect((await miraUnread()).unreadCount).toBe(2); // still unread
    const listed = await mockApi.listChat("mira");
    expect(listed.map((m) => m.id)).toEqual(peeked.map((m) => m.id));
    expect((await miraUnread()).unreadCount).toBe(2); // STILL unread
    // Only the explicit choke clears.
    await mockApi.markChatRead({ peer: "mira", lastReadTs: 1001 });
    expect((await miraUnread()).unreadCount).toBe(0);
  });
});

describe("mock adapter getChatUnreadCount (the office red-dot signal)", () => {
  beforeEach(() => {
    __resetMock();
  });

  it("is 0 when nothing is addressed to the owner (no dot)", async () => {
    inbound("mira", "joey", 1000); // agent to agent, never counts
    expect(await mockApi.getChatUnreadCount()).toBe(0);
  });

  it("sums unread across every peer (not per-member)", async () => {
    inbound("mira", MOCK_OWNER_ID, 1000);
    inbound("mira", MOCK_OWNER_ID, 1001);
    inbound("joey", MOCK_OWNER_ID, 1002);
    expect(await mockApi.getChatUnreadCount()).toBe(3);
  });

  it("clears one peer's share on an explicit mark-read, and only that peer's", async () => {
    inbound("mira", MOCK_OWNER_ID, 1000);
    inbound("joey", MOCK_OWNER_ID, 1001);
    await mockApi.listChat("mira"); // reading is not marking (T-48)
    expect(await mockApi.getChatUnreadCount()).toBe(2);
    await mockApi.markChatRead({ peer: "mira", lastReadTs: 1000 });
    expect(await mockApi.getChatUnreadCount()).toBe(1); // joey still unread
  });
});

describe("mock adapter scrollback cursor parity (T-bf82)", () => {
  beforeEach(() => {
    __resetMock();
  });

  it("a before-cursor page returns strictly older messages (id tie-break) oldest→newest", async () => {
    inbound("mira", MOCK_OWNER_ID, 1000); // t-mira-1000
    // Two equal-ts messages — the id must tie-break exactly like the BE.
    __injectMockChat({
      id: "t-a",
      from: "mira",
      to: MOCK_OWNER_ID,
      body: "hi",
      ts: 2000,
      attachments: [],
      replyCardId: null,
    });
    __injectMockChat({
      id: "t-b",
      from: MOCK_OWNER_ID,
      to: "mira",
      body: "hi",
      ts: 2000,
      attachments: [],
      replyCardId: null,
    });
    inbound("mira", MOCK_OWNER_ID, 3000); // t-mira-3000

    // Page back from the newest: everything strictly older, ascending (ts, id).
    const older = await mockApi.listChat("mira", 30, {
      beforeTs: 3000,
      beforeId: "t-mira-3000",
    });
    expect(older.map((m) => m.id)).toEqual(["t-mira-1000", "t-a", "t-b"]);

    // Tie-break: the cursor (2000, "t-b") keeps the equal-ts smaller id "t-a"
    // and never re-serves "t-b".
    const tie = await mockApi.listChat("mira", 30, {
      beforeTs: 2000,
      beforeId: "t-b",
    });
    expect(tie.map((m) => m.id)).toEqual(["t-mira-1000", "t-a"]);

    // The limit keeps the NEWEST slice of the older page.
    const capped = await mockApi.listChat("mira", 2, {
      beforeTs: 3000,
      beforeId: "t-mira-3000",
    });
    expect(capped.map((m) => m.id)).toEqual(["t-a", "t-b"]);
  });

  it("NO page — cursored or not — advances the read watermark (T-48)", async () => {
    inbound("mira", MOCK_OWNER_ID, 1000);
    inbound("mira", MOCK_OWNER_ID, 2000);
    expect((await miraUnread()).unreadCount).toBe(2);

    // Reading old context with a cursor must NOT consume the unread state…
    await mockApi.listChat("mira", 30, { beforeTs: 2000, beforeId: "zzz" });
    expect((await miraUnread()).unreadCount).toBe(2);

    // …and neither does the cursorless open-thread list. It used to auto-mark;
    // that is the write T-48 removed, here and on the server.
    await mockApi.listChat("mira");
    expect((await miraUnread()).unreadCount).toBe(2);
  });
});
