// Inline task reply card (SPEC §3.2 內嵌等我回覆卡, M3). Locked here for
// T-cdf4: the reply_card SSE topic is NOT per-card — any card being
// opened/answered fans the same topic to EVERY mounted inline card. A card
// that is already ANSWERED is terminal (only a local 重新決定 changes it, and
// that refetches itself), so it must IGNORE the delta; a still-WAITING card
// must still refetch so it can flip in place. This is the fix for the
// broadcast-storm where 70+ historical cards each refetched on one unrelated
// answer.

import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { render, fireEvent, waitFor, act } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { zh } from "../i18n/locales/zh";
import { TaskReplyCard } from "./TaskReplyCard";
import type { ChatMessage, ReplyCard } from "../api/adapter";
import { api } from "../api";
import {
  __resetMock,
  __injectMockChat,
  __injectMockReplyCard,
} from "../api/mock";

function mkCard(over: Partial<ReplyCard>): ReplyCard {
  return {
    id: "rc-1",
    from: "mira",
    kind: "decision",
    summary: "要幫你寄出這封信嗎？",
    body: "",
    options: ["寄出", "先不要"],
    status: "waiting",
    attachments: [],
    createdTs: Date.now() / 1000 - 600,
    answeredTs: null,
    chatMessageId: "msg-1",
    answer: null,
    ...over,
  };
}

function mkChatMessage(id: string, body: string): ChatMessage {
  return {
    id,
    from: "mira",
    to: "owner",
    body,
    ts: Date.now() / 1000,
    attachments: [],
    replyCardId: null,
    replyCardStatus: null,
  };
}

function renderCard(id = "rc-1") {
  return render(
    <I18nProvider>
      <TaskReplyCard replyCardId={id} />
    </I18nProvider>
  );
}

// Capture the component's own subscribeEvents callback so a test can fire an
// unrelated reply_card delta directly — i.e. WITHOUT mutating the card under
// test — and assert whether the component refetches.
function captureSseCallback(): () => void {
  let cb: ((topic: string) => void) | undefined;
  vi.spyOn(api, "subscribeEvents").mockImplementation((onTopic) => {
    cb = onTopic;
    return () => {};
  });
  return () => cb?.("reply_card");
}

beforeEach(() => {
  __resetMock();
  localStorage.clear();
  window.location.hash = "";
});

afterEach(() => {
  vi.restoreAllMocks();
});

describe("TaskReplyCard", () => {
  // T-0b78. The header control used to write #office/chat/<id>/msg/<id> and
  // walk the reader off the task board, leaving ChatArea to hunt the row in
  // already-painted DOM (and land on the newest message when it was not there).
  // It now takes the SAME exit the chat bubble's 看原訊息 takes: fetch that one
  // message, show it whole, do not move the reader.
  it("the header jump opens the ask in full and leaves the route where it was", async () => {
    __injectMockChat(
      mkChatMessage("msg-1", "整段原訊息，長到只有全文才看得完"),
    );
    __injectMockReplyCard(mkCard({}));
    const { findByTestId, getByText } = renderCard();
    await findByTestId("task-reply-card");

    fireEvent.click(getByText("看原訊息"));

    await waitFor(() =>
      expect(document.querySelector(".md-preview")?.textContent).toContain(
        "只有全文才看得完",
      ),
    );
    expect(window.location.hash).toBe("");
  });

  it("the header jump says on screen that the ask could not be read", async () => {
    __injectMockReplyCard(mkCard({ chatMessageId: "msg-gone" }));
    const { findByTestId, getByText } = renderCard();
    await findByTestId("task-reply-card");

    fireEvent.click(getByText("看原訊息"));

    expect((await findByTestId("msg-quote-error")).textContent).toBe(
      zh.chat.replyQuoteOpenFailed,
    );
    expect(document.querySelector(".md-preview")).toBeNull();
    expect(window.location.hash).toBe("");
  });

  it("an ALREADY-answered card ignores an unrelated reply_card SSE delta (no refetch storm)", async () => {
    __injectMockReplyCard(
      mkCard({
        status: "answered",
        answeredTs: Date.now() / 1000 - 60,
        answer: { optionIdx: 0, text: "", attachments: [] },
      })
    );
    const fireDelta = captureSseCallback();
    const getSpy = vi.spyOn(api, "getReplyCard");
    const { findByTestId } = renderCard();
    // Mount does its one unconditional refetch (initial card shape) — an
    // answered card renders collapsed.
    await findByTestId("task-reply-card-expand");
    expect(getSpy).toHaveBeenCalledTimes(1);

    // Some OTHER card is opened/answered elsewhere → the non-scoped reply_card
    // topic fans to this already-answered card. It is terminal — it must NOT
    // refetch (pre-fix this fired a getReplyCard; that was the storm).
    fireDelta();
    await Promise.resolve();
    expect(getSpy).toHaveBeenCalledTimes(1);
  });

  it("a still-WAITING card DOES refetch on a reply_card SSE delta (flip path preserved)", async () => {
    __injectMockReplyCard(mkCard({}));
    const fireDelta = captureSseCallback();
    const getSpy = vi.spyOn(api, "getReplyCard");
    const { findAllByText } = renderCard();
    // Mount's unconditional refetch.
    await findAllByText("寄出");
    expect(getSpy).toHaveBeenCalledTimes(1);

    // A waiting card must still react to the delta (it may have just been
    // answered on another surface and needs to flip in place).
    await act(async () => {
      fireDelta();
    });
    await waitFor(() => expect(getSpy).toHaveBeenCalledTimes(2));
  });

  // ── lazy-load: answered-hinted cards default NOT loaded (owner 已回覆卡預設不載) ─
  function renderHinted(initialStatus: "waiting" | "answered") {
    return render(
      <I18nProvider>
        <TaskReplyCard
          replyCardId="rc-1"
          initialStatus={initialStatus}
          fallbackSummary="核准步驟"
        />
      </I18nProvider>
    );
  }

  it("an ANSWERED-hinted card does NOT fetch on mount — collapsed stub (step-name fallback)", async () => {
    __injectMockReplyCard(
      mkCard({
        status: "answered",
        answeredTs: Date.now() / 1000 - 60,
        answer: { optionIdx: 0, text: "", attachments: [] },
      })
    );
    const getSpy = vi.spyOn(api, "getReplyCard");
    const { findByTestId } = renderHinted("answered");

    const stub = await findByTestId("task-reply-card-expand");
    expect(stub.textContent).toContain("已回覆");
    expect(stub.textContent).toContain("核准步驟"); // the fallback, not the card
    expect(getSpy).not.toHaveBeenCalled();
  });

  it("expanding an ANSWERED-hinted card fetches it once and shows the answer", async () => {
    __injectMockReplyCard(
      mkCard({
        status: "answered",
        answeredTs: Date.now() / 1000 - 60,
        answer: { optionIdx: 0, text: "", attachments: [] },
      })
    );
    const getSpy = vi.spyOn(api, "getReplyCard");
    const { findByTestId } = renderHinted("answered");

    fireEvent.click(await findByTestId("task-reply-card-expand"));
    const final = await findByTestId("final-answer");
    expect(final.textContent).toContain("寄出");
    expect(getSpy).toHaveBeenCalledTimes(1);
  });

  it("a WAITING-hinted card loads eagerly on mount (waiting 卡照常)", async () => {
    __injectMockReplyCard(mkCard({}));
    const getSpy = vi.spyOn(api, "getReplyCard");
    const { findAllByText } = renderHinted("waiting");

    await findAllByText("寄出");
    expect(getSpy).toHaveBeenCalledTimes(1);
  });

  it("a collapsed ANSWERED-hinted card ignores an unrelated reply_card SSE delta WITHOUT fetching (seeded statusRef)", async () => {
    __injectMockReplyCard(
      mkCard({
        status: "answered",
        answeredTs: Date.now() / 1000 - 60,
        answer: { optionIdx: 0, text: "", attachments: [] },
      })
    );
    const fireDelta = captureSseCallback();
    const getSpy = vi.spyOn(api, "getReplyCard");
    const { findByTestId } = renderHinted("answered");
    await findByTestId("task-reply-card-expand");
    expect(getSpy).not.toHaveBeenCalled();

    fireDelta();
    await Promise.resolve();
    expect(getSpy).not.toHaveBeenCalled();
  });
});
