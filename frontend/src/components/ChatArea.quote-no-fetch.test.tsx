// ChatArea — rendering a quote reaches for NOTHING (T-4e95, owner ruling
// 2026-08-21).
//
// 🔴 THIS FILE IS THE WITNESS THAT THE STATE MACHINE IS REALLY GONE, and it is
// the only test in the tree that a re-introduced lookup cannot satisfy by
// happening to return the right answer.
//
// What it replaces: the wire used to carry the quoted message's ID alone, so
// <ChatArea> resolved the rest — from the loaded window if it was there, and
// otherwise with a by-ids read (`useQuotedMessages`, deleted with this change).
// That read could fail; a failure was drawn as a placeholder that was sometimes
// a lie; the lie was repaid when the next SSE event arrived. Every one of those
// behaviours draws the SAME PIXELS whether it is right or wrong, which is why
// each of the ~600 lines of tests that grew around them could pass while the
// feature was broken in the browser.
//
// Every other reply-to test asserts what is ON SCREEN. This one asserts what
// was NOT DONE to put it there, because "the fetch is gone" is not a pixel.
//
// IT LIVES IN ITS OWN FILE ON PURPOSE. The api client is replaced here by a
// recording proxy, and that proxy is not a working client — <ChatReplyCard>
// would break against it. So the fixtures here carry no reply-card row, and the
// mock stays out of the main reply-to file, which needs a real one.

import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, fireEvent, act } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { ChatArea } from "./ChatArea";
import type { Member } from "../types";
import type { ChatMessage } from "../api/adapter";
import { resetChatDrafts } from "../lib/chatDraftStore";
import { zh } from "../i18n/locales/zh";

let messages: ChatMessage[] = [];

vi.mock("../hooks/useChat", () => ({
  useChat: () => ({
    messages,
    messagesPeer: "m1",
    peerLastReadTs: 0,
    send: vi.fn(() => Promise.resolve()),
    markRead: vi.fn(() => Promise.resolve()),
  }),
}));

/** Every property the component tree pulls off the api client, RECORDED rather
 * than refused so a failure can name the call that broke the rule. */
const apiCalls: string[] = [];
vi.mock("../api", () => ({
  USE_MOCK: true,
  api: new Proxy(
    {},
    {
      get(_t, prop) {
        apiCalls.push(String(prop));
        return () => Promise.resolve([]);
      },
    },
  ),
}));

const member: Member = {
  id: "m1",
  name: "Mira",
  role: "assistant",
  status: "online",
  lifecycle: "online",
  model: "opus",
  effort: "medium",
  kind: "assistant",
  desiredMachineId: "",
  machine: null,
  account: null,
  contextPct: null,
  estimatedCost: null,
  bankedCost: null,
  tmuxSession: "member-m1",
  refocusSince: null,
  lastOp: "",
  lastOpOk: null,
  lastOpLog: "",
  lastOpAt: null,
  unreadCount: 0,
};

function mkMsg(over: Partial<ChatMessage> & { id: string }): ChatMessage {
  return {
    from: "m1",
    to: "owner",
    body: "",
    ts: 1,
    attachments: [],
    replyCardId: null,
    replyCardStatus: null,
    replyTo: null,
    replyToChat: null,
    ...over,
  };
}

const rowOf = (c: HTMLElement, id: string) =>
  c.querySelector(`[data-msg-id="${id}"]`) as HTMLElement;

describe("ChatArea: a quote costs no request", () => {
  beforeEach(() => {
    resetChatDrafts();
    apiCalls.length = 0;
    Element.prototype.scrollIntoView = function () {} as typeof Element.prototype.scrollIntoView;
  });

  it("renders a present quote AND a missing one without touching the api", async () => {
    // Both shapes are on screen at once. The second is the one the deleted
    // machine used to chase: `replyTo` set, no `replyToChat`, and — crucially —
    // the quoted id is NOT in the loaded window, which is exactly the condition
    // that used to trigger the by-ids read.
    messages = [
      mkMsg({ id: "c-1", from: "m1", to: "owner", body: "他說的" }),
      mkMsg({
        id: "c-2",
        from: "owner",
        to: "m1",
        body: "有的",
        ts: 2,
        replyTo: "c-1",
        replyToChat: { id: "c-1", from: "m1", fromName: "", content: "他說的" },
      }),
      mkMsg({
        id: "c-3",
        from: "owner",
        to: "m1",
        body: "沒有的",
        ts: 3,
        replyTo: "c-longgone",
        replyToChat: null,
      }),
    ];

    const { container } = render(
      <I18nProvider>
        <ChatArea member={member} />
      </I18nProvider>,
    );

    // Both rows are painted, so this is not vacuously true of a blank screen.
    expect(
      rowOf(container, "c-2").querySelector(".chat__msg-quote__body")?.textContent,
    ).toBe("他說的");
    expect(
      rowOf(container, "c-3").querySelector(".chat__msg-quote")?.textContent,
    ).toContain(zh.chat.replyQuoteGone);

    // A keystroke re-renders the thread — that is what used to recompute the
    // read's effect key and fire it. Then let every effect and microtask settle,
    // so a read scheduled and not yet sent still counts.
    fireEvent.change(
      container.querySelector(".chat__input") as HTMLTextAreaElement,
      { target: { value: "一" } },
    );
    await act(async () => {
      await Promise.resolve();
    });

    expect(
      apiCalls,
      "rendering a quote — present or missing — must reach for nothing",
    ).toEqual([]);
  });
});
