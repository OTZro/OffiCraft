// A successful reply-card WRITE must reconcile the cockpit BY ITSELF — with no
// event stream at all (T-a3e4 step 8 follow-up).
//
// The defect this pins: T-a3e4 step 8 removed the action-path refetch and left
// the `reply_card` delta as the ONLY reconcile trigger for answer / expire (等我
// 回覆 page + nav badge) and for the inline chat card's first answer. That is
// correct only while the stream is up. With the EventSource disconnected or one
// frame missed, the server has ACCEPTED the answer while the cockpit still
// renders the card as waiting: the owner clicks the already-handled card again
// and hits a 409, and the nav waiting badge stays wrong until reconnect /
// foreground resync / reload.
//
// 🔴 WHY THE CHECKED-IN TESTS CANNOT SEE THIS. The mock adapter fans its own
// `reply_card` topic SYNCHRONOUSLY from inside answerReplyCard / expireReplyCard
// (`emitTopic`), so every existing test gets the delta for free and the
// event-less case is never exercised. This file removes exactly one thing —
// the event subscription — and changes nothing else: still the REAL mock
// adapter, still a real click on a real rendered card.
//
// 🔴 AND IT ASSERTS PIXELS, NOT CALL COUNTS, on purpose — the mirror image of
// `useReplyCards.one-round.test.tsx`. Its budget does NOT tolerate zero (the
// assertion is `=== 1`; deleting the SSE reconcile branch so the action path
// costs zero rounds reddens 3 of its tests — measured, do not repeat the earlier
// version of this note, which claimed zero satisfied it). The gap is RANGE: that
// budget measures how many rounds a write costs WITH THE STREAM UP, and the
// pre-fix code did cost exactly one there — so it has nothing to say about the
// stream being down. Cost and correctness need one witness each; neither
// assertion can stand in for the other.
//
// The fix these tests demand costs no extra round trip: the write's OWN response
// already carries the fresh card (`answerReplyCard` / `expireReplyCard` /
// `reanswerReplyCard` all return `ReplyCard`), so the action path adopts it. The
// one-round budget is therefore untouched — both files are green together.

import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { render, fireEvent, waitFor } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { RepliesPage } from "../components/RepliesPage";
import { ChatReplyCard } from "../components/ChatReplyCard";
import { ReplyCardsProvider } from "./useReplyCards";
import { __resetMock, __injectMockReplyCard } from "../api/mock";
import { api } from "../api";
import type { ReplyCard } from "../api/adapter";

function mkCard(over: Partial<ReplyCard> = {}): ReplyCard {
  return {
    id: "rc-1",
    from: "mira",
    kind: "decision",
    summary: "要不要切到新的排程器?",
    body: "細節",
    options: ["切過去", "先不要"],
    status: "waiting",
    attachments: [],
    task: null,
    expiredTs: null,
    createdTs: Date.now() / 1000 - 25 * 60,
    answeredTs: null,
    chatMessageId: "msg-1",
    answer: null,
    ...over,
  };
}

/** The whole point of this file: the cockpit is subscribed to NOTHING. Installed
 * before render, so the provider's mount effect gets the no-op too. */
function killEventStream() {
  return vi.spyOn(api, "subscribeEvents").mockImplementation(() => () => {});
}

/** A stream that is alive at mount and whose LAST frame we fire by hand — the
 * shape of an EventSource that dies right after delivering one delta. Returns
 * the captured handler. */
function captureEventStream() {
  let fire: ((topic: string) => void) | null = null;
  vi.spyOn(api, "subscribeEvents").mockImplementation((onTopic) => {
    fire = (topic: string) => onTopic(topic);
    return () => {
      fire = null;
    };
  });
  return {
    deliver: (topic: string) => {
      if (!fire) throw new Error("nobody subscribed");
      fire(topic);
    },
  };
}

/** Makes the NEXT `listReplyCards("waiting")` hang, and hands back its resolver
 * so the test decides exactly when that (by then stale) snapshot lands. Every
 * other call goes to the real mock adapter. */
function hangNextWaitingRead() {
  const real = api.listReplyCards.bind(api);
  let release: ((rows: ReplyCard[]) => void) | null = null;
  let armed = true;
  vi.spyOn(api, "listReplyCards").mockImplementation((status) => {
    if (armed && status === "waiting") {
      armed = false;
      return new Promise<ReplyCard[]>((resolve) => {
        release = resolve;
      });
    }
    return real(status);
  });
  return {
    inFlight: () => release !== null,
    /** Land the in-flight read with a snapshot of the test's choosing. */
    landWith: (rows: ReplyCard[]) => {
      if (!release) throw new Error("no waiting read is in flight");
      release(rows);
    },
  };
}

function renderPage() {
  return render(
    <I18nProvider>
      <ReplyCardsProvider>
        <RepliesPage />
      </ReplyCardsProvider>
    </I18nProvider>
  );
}

beforeEach(() => {
  __resetMock();
  window.location.hash = "";
});

afterEach(() => {
  vi.restoreAllMocks();
});

describe("reply-card writes reconcile without any event stream", () => {
  it("answering a waiting card removes it from the pane with the stream DOWN", async () => {
    __injectMockReplyCard(mkCard({ id: "rc-1", summary: "第一張" }));
    __injectMockReplyCard(mkCard({ id: "rc-2", summary: "第二張" }));
    __injectMockReplyCard(mkCard({ id: "rc-3", summary: "第三張" }));

    killEventStream();

    const { findAllByTestId, queryAllByTestId } = renderPage();
    const cards = await findAllByTestId("waiting-card");
    expect(cards).toHaveLength(3);

    fireEvent.click(cards[0].querySelectorAll(".reply-option")[0]);

    // The write landed (the mock accepted it and flipped the card to answered).
    // The cockpit must not keep rendering it as waiting — that is what sends the
    // owner back into an already-handled card for a 409. The nav badge reads the
    // length of this SAME array (T-e862 同源化), so this assertion covers it too.
    await waitFor(() =>
      expect(queryAllByTestId("waiting-card")).toHaveLength(2)
    );
  });

  it("expiring a waiting card removes it from the pane with the stream DOWN", async () => {
    __injectMockReplyCard(mkCard({ id: "rc-1", summary: "第一張" }));
    __injectMockReplyCard(mkCard({ id: "rc-2", summary: "第二張" }));

    killEventStream();

    const { findAllByTestId, findByTestId, queryAllByTestId } = renderPage();
    const cards = await findAllByTestId("waiting-card");
    expect(cards).toHaveLength(2);

    fireEvent.click(cards[0].querySelector('[data-testid="expire-card"]')!);
    const confirm = await findByTestId("expire-confirm-btn");
    fireEvent.click(confirm);

    await waitFor(() =>
      expect(queryAllByTestId("waiting-card")).toHaveLength(1)
    );
  });

  it("an in-flight PRE-WRITE snapshot cannot paint the answered card back", async () => {
    // 🔴 The reason adopting is not enough on its own, and it is NOT an exotic
    // race: the only precondition is "a delta arrived shortly before the click",
    // i.e. the stream was alive and then dropped — which is the ordinary shape of
    // an EventSource dying (the last frame it delivered left a refetch in
    // flight). The outcome is identical to the original blocker: the card is
    // waiting again, the badge is wrong again, and with the stream down there is
    // no newer refetch to correct it. T-e862's generation guard does not help —
    // it only drops a snapshot once a NEWER one exists.
    __injectMockReplyCard(mkCard({ id: "rc-1", summary: "第一張" }));
    __injectMockReplyCard(mkCard({ id: "rc-2", summary: "第二張" }));

    const stream = captureEventStream();
    const { findAllByTestId, queryAllByTestId } = renderPage();
    const cards = await findAllByTestId("waiting-card");
    expect(cards).toHaveLength(2);

    // A peer's delta kicks a refetch — and that read never comes back before the
    // owner acts (slow response, then the stream drops).
    const read = hangNextWaitingRead();
    stream.deliver("reply_card");
    await waitFor(() => expect(read.inFlight()).toBe(true));

    fireEvent.click(cards[0].querySelectorAll(".reply-option")[0]);
    await waitFor(() =>
      expect(queryAllByTestId("waiting-card")).toHaveLength(1)
    );

    // Now the pre-write snapshot lands: rc-1 still listed as waiting, because it
    // was read before the answer was accepted.
    read.landWith([mkCard({ id: "rc-1" }), mkCard({ id: "rc-2" })]);

    await waitFor(() =>
      expect(queryAllByTestId("waiting-card")).toHaveLength(1)
    );
    expect(
      queryAllByTestId("waiting-card").map((el) => el.id)
    ).toEqual(["reply-card-rc-2"]);
  });

  it("...and that same snapshot's NEW card still arrives", async () => {
    // 🔴 This is the half that rejects the cheap fix. Bumping the generation (or
    // otherwise DISCARDING the in-flight snapshot) turns the test above green,
    // and throws away everything else that snapshot carried — including a card a
    // peer just opened. With the stream down no later refetch brings it back, so
    // that trade is one silent failure for another: the owner is simply never
    // told there is a card waiting for them. The hold is therefore per-id.
    __injectMockReplyCard(mkCard({ id: "rc-1", summary: "第一張" }));
    __injectMockReplyCard(mkCard({ id: "rc-2", summary: "第二張" }));

    const stream = captureEventStream();
    const { findAllByTestId, queryAllByTestId } = renderPage();
    expect(await findAllByTestId("waiting-card")).toHaveLength(2);

    const read = hangNextWaitingRead();
    stream.deliver("reply_card");
    await waitFor(() => expect(read.inFlight()).toBe(true));

    const cards = await findAllByTestId("waiting-card");
    fireEvent.click(cards[0].querySelectorAll(".reply-option")[0]);
    await waitFor(() =>
      expect(queryAllByTestId("waiting-card")).toHaveLength(1)
    );

    // The same stale snapshot ALSO carries a card someone else just opened.
    read.landWith([
      mkCard({ id: "rc-1" }),
      mkCard({ id: "rc-2" }),
      mkCard({ id: "rc-3", summary: "別人剛開的" }),
    ]);

    await waitFor(() =>
      expect(queryAllByTestId("waiting-card")).toHaveLength(2)
    );
    expect(
      queryAllByTestId("waiting-card").map((el) => el.id).sort()
    ).toEqual(["reply-card-rc-2", "reply-card-rc-3"]);
  });

  it("an inline chat card flips to answered in place with the stream DOWN", async () => {
    // The third site (ChatReplyCard.doAnswer). Same write, same missing delta:
    // the card the owner just answered keeps showing its option chips.
    __injectMockReplyCard(mkCard({ id: "rc-inline", summary: "要寄出嗎?" }));

    killEventStream();

    const { container } = render(
      <I18nProvider>
        <ChatReplyCard
          replyCardId="rc-inline"
          fallbackSummary="要寄出嗎?"
          initialStatus={null}
        />
      </I18nProvider>
    );
    await waitFor(() =>
      expect(container.querySelector(".reply-option")).toBeTruthy()
    );

    fireEvent.click(container.querySelectorAll(".reply-option")[0]);

    await waitFor(() =>
      expect(container.querySelector(".reply-card__answer-text")).toBeTruthy()
    );
  });
});
