// lib/replyCardCache — the prefill table `lib/threadCommit` awaits before it
// lets messages reach the view. Locked here: the table's NEGATIVE memory (an id
// whose read failed is not asked for again) and its in-flight dedupe (a second
// commit naming an id already in the air does not issue a second GET).

import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import type { ChatMessage, ReplyCard } from "../api/adapter";
import {
  getCachedReplyCard,
  pendingWaitingCardIds,
  prefillWaitingCards,
  resetReplyCardCache,
} from "./replyCardCache";
import { api } from "../api";

function mkWaitingRow(cardId: string): ChatMessage {
  return {
    id: `m-${cardId}`,
    from: "mira",
    to: "owner",
    body: "要幫你寄出這封信嗎？",
    attachments: [],
    ts: 1,
    replyCardId: cardId,
    replyCardStatus: "waiting",
  };
}

function mkCard(id: string): ReplyCard {
  return {
    id,
    from: "mira",
    kind: "decision",
    summary: "s",
    body: "",
    options: [],
    selectMode: "single",
    status: "waiting",
    attachments: [],
    createdTs: 0,
    answeredTs: null,
    chatMessageId: `m-${id}`,
    answer: null,
  } as ReplyCard;
}

/** One macrotask. `prefillWaitingCards` reaches the api client through
 * `await import("../api")`, so its GETs start a turn late. */
const tick = () => new Promise((r) => setTimeout(r, 0));

beforeEach(() => resetReplyCardCache());
afterEach(() => vi.restoreAllMocks());

describe("prefillWaitingCards", () => {
  it("puts every waiting card on the page in hand with one GET each", async () => {
    const get = vi
      .spyOn(api, "getReplyCard")
      .mockImplementation(async (id: string) => mkCard(id));
    const page = [mkWaitingRow("rc-1"), mkWaitingRow("rc-2")];

    await prefillWaitingCards(page);

    expect(get).toHaveBeenCalledTimes(2);
    expect(getCachedReplyCard("rc-1")?.id).toBe("rc-1");
    // In hand → no longer pending, so a second commit costs nothing.
    expect(pendingWaitingCardIds(page)).toEqual([]);
    await prefillWaitingCards(page);
    expect(get).toHaveBeenCalledTimes(2);
  });

  it("gives up on an id whose read FAILED instead of retrying it on every commit", async () => {
    const get = vi
      .spyOn(api, "getReplyCard")
      .mockRejectedValue(new Error("404"));
    const page = [mkWaitingRow("rc-dead")];

    await prefillWaitingCards(page);
    expect(get).toHaveBeenCalledTimes(1);

    // Without a negative memory this id is pending forever: every later commit
    // asks again and waits the full deadline again.
    expect(pendingWaitingCardIds(page)).toEqual([]);
    await prefillWaitingCards(page);
    await prefillWaitingCards(page);
    expect(get).toHaveBeenCalledTimes(1);
  });

  it("attaches to a read already in flight rather than starting a second one", async () => {
    let release!: (c: ReplyCard) => void;
    const get = vi
      .spyOn(api, "getReplyCard")
      .mockImplementation(
        () => new Promise<ReplyCard>((res) => (release = res)),
      );
    const page = [mkWaitingRow("rc-slow")];

    const first = prefillWaitingCards(page);
    // The api client is reached through `await import("../api")`, so the GET
    // starts a microtask late; the second commit must land AFTER the first has
    // registered its in-flight promise, which is the case this guards.
    await tick();
    const second = prefillWaitingCards(page);
    await tick();
    // The deadline bounds the WAIT, not the request — so a duplicate commit
    // used to leave a second un-cancelled GET behind it, without bound.
    expect(get).toHaveBeenCalledTimes(1);

    release(mkCard("rc-slow"));
    await Promise.all([first, second]);
    expect(getCachedReplyCard("rc-slow")?.id).toBe("rc-slow");
  });

  it("does not withhold the rest of the page for one card's failure", async () => {
    vi.spyOn(api, "getReplyCard").mockImplementation(async (id: string) => {
      if (id === "rc-bad") throw new Error("404");
      return mkCard(id);
    });

    await prefillWaitingCards([mkWaitingRow("rc-bad"), mkWaitingRow("rc-ok")]);

    expect(getCachedReplyCard("rc-ok")?.id).toBe("rc-ok");
    expect(getCachedReplyCard("rc-bad")).toBeUndefined();
  });
});
