// T-a3e4 node 8 — what the reply-card pane fetch actually puts ON THE WIRE.
//
// This is a ROUND-TRIP ticket, so what has to be pinned is the set of requests
// that leave the browser, not "the adapter returned the right cards": the old
// adapter returned exactly the right cards and still cost one
// GET /api/reply-cards/{id} PER ROW to do it.
//
// Baseline being replaced (measured on a real ocserverd waiting pane, before
// this change): 26 requests / 49,970 B → after: 1 request / 44,183 B.
// 🔴 That is 25 fewer ROUND TRIPS and only 11.6% fewer bytes. The value is the
// latency, not the bandwidth — so these tests count REQUESTS, and there is
// deliberately no byte assertion anywhere (a byte assertion here would suggest
// a saving that is nearly absent).
//
// Anti-tautology: every fixture below has MORE THAN ONE row. With a single-row
// pane "one request" is what the OLD code cost too (1 list + 1 hydrate = 2, but
// a one-row assertion invites `toBeLessThan(3)` sloppiness), so the corpus has
// to be big enough that per-row fan-out is unmistakable.

import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { httpApi } from "./http";

function jsonResponse(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { "Content-Type": "application/json" },
  });
}

/** A FULL card as ?view=full serves it — the same shape GET /{id} returns. */
function fullCard(id: string, summary: string) {
  return {
    id,
    from: "m-a",
    kind: "decision",
    status: "waiting",
    summary,
    body: `the ask body of ${id}`,
    options: [
      { text: "AI 建議:照做", ai_pick: true },
      { text: "先等等", ai_pick: false },
    ],
    select_mode: "single",
    chat_message_id: `cm-${id}`,
    created_ts: 100,
    answered_ts: null,
    expired_ts: null,
    answer: null,
    attachments: [],
    task: null,
  };
}

const PANE = [
  fullCard("rc-1", "first ask"),
  fullCard("rc-2", "second ask"),
  fullCard("rc-3", "third ask"),
];

const fetchMock = vi.fn(async () => jsonResponse(PANE));

function requests(): URL[] {
  const calls = fetchMock.mock.calls as unknown as [Request][];
  return calls.map((c) => new URL(c[0].url));
}

beforeEach(() => {
  fetchMock.mockReset();
  fetchMock.mockImplementation(async () => jsonResponse(PANE));
  vi.stubGlobal("fetch", fetchMock);
  localStorage.setItem("oc_token", "test-owner-jwt");
});

afterEach(() => {
  vi.unstubAllGlobals();
  localStorage.clear();
});

describe("httpApi.listReplyCards · one request per pane (T-a3e4)", () => {
  it("asks for ?view=full ONCE and never hydrates a card one by one", async () => {
    const cards = await httpApi.listReplyCards("waiting");

    const urls = requests();
    // The whole point: ONE round trip for a pane of three.
    expect(urls).toHaveLength(1);
    expect(urls[0].pathname).toBe("/api/reply-cards");
    // The VALUES on the wire — asking for the wrong projection would return
    // light rows and quietly lose every body/options the pane renders.
    expect(urls[0].searchParams.get("status")).toBe("waiting");
    expect(urls[0].searchParams.get("view")).toBe("full");
    // Not one single-card GET. This is the assertion the old adapter failed:
    // it issued one of these per row.
    expect(
      urls.filter((u) => /^\/api\/reply-cards\/rc-/.test(u.pathname)),
    ).toHaveLength(0);

    // ...and the cards are really the FULL shape, in server order. Counting
    // requests alone would be satisfied by an adapter that fetched once and
    // returned husks — or nothing at all.
    expect(cards.map((c) => c.id)).toEqual(["rc-1", "rc-2", "rc-3"]);
    expect(cards[0].body).toBe("the ask body of rc-1");
    expect(cards[0].options).toEqual([
      { text: "AI 建議:照做", aiPick: true },
      { text: "先等等", aiPick: false },
    ]);
    expect(cards[0].chatMessageId).toBe("cm-rc-1");
  });

  it("costs one request for EVERY pane, not just the waiting one", async () => {
    // The 近期已處理 pane is two more listReplyCards calls (answered + expired),
    // and before this change each of those ALSO fanned out per row — that pane
    // is why one expanded 等我回覆 page cost ~51 round trips per delta.
    for (const status of ["waiting", "answered", "expired"] as const) {
      fetchMock.mockClear();
      await httpApi.listReplyCards(status);
      const urls = requests();
      expect(urls).toHaveLength(1);
      expect(urls[0].searchParams.get("status")).toBe(status);
      expect(urls[0].searchParams.get("view")).toBe("full");
    }
  });

  it("an empty pane is still exactly one request", async () => {
    fetchMock.mockImplementation(async () => jsonResponse([]));
    const cards = await httpApi.listReplyCards("expired");
    expect(cards).toEqual([]);
    expect(requests()).toHaveLength(1);
  });
});
