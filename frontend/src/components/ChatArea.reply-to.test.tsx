// ChatArea 「回覆這則」 (T-4e95) — the owner asked for LINE-style quote-reply:
// every message gets a reply entry, the composer says who is being answered,
// an x returns it to the ordinary send state, and the sent message shows what
// it answered and can be clicked back to it.
//
// Locked here, one test per promise the AC makes:
//   • EVERY row carries the entry — own messages, incoming, and card rows;
//   • the banner names the quoted sender and shows a slice of what they said;
//   • the x clears ONLY the target — half-typed text survives it;
//   • sending carries the target, and clears it (the NEXT message is not a
//     reply too);
//   • switching targets, cancelling and re-aiming leaves no stale state;
//   • a quote whose target is in the loaded window is clickable and locates it;
//     one that is older renders as an honest label, not a dead button.
//
// The api layer is mocked at the useChat seam, matching the other ChatArea
// tests. GEOMETRY IS NOT TESTED HERE — jsdom has no layout engine, so hover
// reveal and the one-line clipping of the quote row live in the Playwright CT
// (visual-guards/chat-reply-to.ct.spec.tsx). A jsdom test that "checked" those
// would be green against a completely unstyled button.

import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, fireEvent, waitFor } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { ChatArea } from "./ChatArea";
import type { Member } from "../types";
import type { ChatMessage } from "../api/adapter";
import { resetChatDrafts } from "../lib/chatDraftStore";

let messages: ChatMessage[] = [];
const send = vi.fn(() => Promise.resolve());

vi.mock("../hooks/useChat", () => ({
  useChat: () => ({
    messages,
    messagesPeer: "m1",
    peerLastReadTs: 0,
    send,
    markRead: vi.fn(() => Promise.resolve()),
  }),
}));

function mkMember(id: string, name: string): Member {
  return {
    id,
    name,
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
    tmuxSession: `member-${id}`,
    refocusSince: null,
    lastOp: "",
    lastOpOk: null,
    lastOpLog: "",
    lastOpAt: null,
    unreadCount: 0,
  };
}

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
    ...over,
  };
}

const m1 = mkMember("m1", "Mira");

function renderChat() {
  return render(
    <I18nProvider>
      <ChatArea member={m1} />
    </I18nProvider>,
  );
}

const input = (c: HTMLElement) =>
  c.querySelector(".chat__input") as HTMLTextAreaElement;
const banner = (c: HTMLElement) =>
  c.querySelector("[data-testid='chat-reply-banner']");
const replyButtons = (c: HTMLElement) =>
  Array.from(c.querySelectorAll(".chat__msg-reply")) as HTMLButtonElement[];
const rowOf = (c: HTMLElement, id: string) =>
  c.querySelector(`[data-msg-id="${id}"]`) as HTMLElement;

let scrolled: Element[] = [];

describe("ChatArea 回覆這則", () => {
  beforeEach(() => {
    resetChatDrafts();
    send.mockClear();
    // jsdom has no layout engine and therefore no scrollIntoView. Stubbed to a
    // recorder, the same way ChatArea.unread-jump.test.tsx does: what these
    // tests pin is WHICH element was asked to come into view, never geometry.
    scrolled = [];
    Element.prototype.scrollIntoView = function (this: Element) {
      scrolled.push(this);
    } as typeof Element.prototype.scrollIntoView;
    messages = [
      mkMsg({ id: "c-1", from: "m1", body: "第一個問題" }),
      mkMsg({ id: "c-2", from: "owner", to: "m1", body: "我的回應", ts: 2 }),
      // A card row and an attachment-only row: both are messages, and the AC
      // says EVERY message gets the entry — these are the two shapes a
      // bubble-corner button could not have covered.
      mkMsg({ id: "c-3", body: "請示", ts: 3, replyCardId: "rc-1" }),
      mkMsg({
        id: "c-4",
        ts: 4,
        attachments: [
          { id: "a1", url: "/x", filename: "p.png", mime: "image/png", isImage: true },
        ],
      }),
    ];
  });

  it("EVERY message row carries a reply entry — incoming, own, card and attachment-only alike", () => {
    const { container } = renderChat();
    expect(replyButtons(container)).toHaveLength(messages.length);
    // …and each one really belongs to a row, so four buttons stacked in one
    // corner could not pass.
    for (const m of messages) {
      expect(rowOf(container, m.id).querySelector(".chat__msg-reply")).toBeTruthy();
    }
  });

  it("the entry sits in the bubble's own corner slot, beside 放大閱讀", () => {
    const { container } = renderChat();
    // Owner 2026-08-20: out on the row it read as belonging to the thread
    // rather than to this message. Bubble-shaped messages carry it in the same
    // corner slot as 放大閱讀 — and the slot is INSIDE the bubble, which is
    // what makes it look like part of the message.
    for (const id of ["c-1", "c-2", "c-4"]) {
      const entry = rowOf(container, id).querySelector(".chat__msg-reply")!;
      expect(entry.closest(".chat__msg-actions")).toBeTruthy();
      expect(entry.closest(".chat__msg-bubble")).toBeTruthy();
    }
    // An INCOMING text bubble carries BOTH controls, in ONE slot — two corners
    // would be two places to look.
    const slot = rowOf(container, "c-1").querySelector(".chat__msg-actions")!;
    expect(slot.querySelector(".chat__msg-reply")).toBeTruthy();
    expect(slot.querySelector(".chat__msg-expand")).toBeTruthy();

    // The declared exception: a reply-card row has no bubble to put a corner
    // on, so its entry stays on the row. Stated as a test so the exception is
    // a decision on record rather than something that quietly happened.
    const cardEntry = rowOf(container, "c-3").querySelector(".chat__msg-reply")!;
    expect(cardEntry.closest(".chat__msg-bubble")).toBeNull();
  });

  it("clicking it names the quoted sender and quotes what they said, above the input", () => {
    const { container } = renderChat();
    expect(banner(container)).toBeNull();

    fireEvent.click(rowOf(container, "c-1").querySelector(".chat__msg-reply")!);

    const b = banner(container)!;
    expect(b).toBeTruthy();
    expect(b.textContent).toContain("Mira");
    expect(b.textContent).toContain("第一個問題");
    // The banner belongs to the COMPOSER, not the thread: it must sit inside
    // the footer, above the input row. Placing it in the message list would
    // satisfy every text assertion above and be the wrong feature.
    expect(b.closest(".chat__composer")).toBeTruthy();
  });

  it("the x cancels the reply and DOES NOT touch what has already been typed", () => {
    const { container } = renderChat();
    fireEvent.change(input(container), { target: { value: "打到一半的字" } });
    fireEvent.click(rowOf(container, "c-1").querySelector(".chat__msg-reply")!);
    expect(banner(container)).toBeTruthy();

    fireEvent.click(container.querySelector(".chat__reply-banner__x")!);

    expect(banner(container)).toBeNull();
    // 🔴 THE WHOLE POINT OF THIS TEST. Cancelling a reply is not cancelling the
    // message; a composer that emptied itself here would throw away work the
    // owner never asked to lose.
    expect(input(container).value).toBe("打到一半的字");
  });

  it("sending carries the target — and the NEXT message is not a reply too", async () => {
    const { container } = renderChat();
    fireEvent.click(rowOf(container, "c-1").querySelector(".chat__msg-reply")!);
    fireEvent.change(input(container), { target: { value: "答案" } });
    fireEvent.click(container.querySelector(".chat__send")!);

    await waitFor(() =>
      expect(send).toHaveBeenCalledWith("答案", undefined, "c-1"),
    );
    expect(banner(container)).toBeNull();

    // The second send is the discriminating half: a target that survived its
    // own send would silently attach itself to everything after it.
    send.mockClear();
    fireEvent.change(input(container), { target: { value: "另一句" } });
    fireEvent.click(container.querySelector(".chat__send")!);
    await waitFor(() =>
      expect(send).toHaveBeenCalledWith("另一句", undefined, undefined),
    );
  });

  it("re-aiming, cancelling and aiming again leaves no stale target behind", async () => {
    const { container } = renderChat();
    fireEvent.click(rowOf(container, "c-1").querySelector(".chat__msg-reply")!);
    fireEvent.click(rowOf(container, "c-2").querySelector(".chat__msg-reply")!);
    expect(banner(container)!.textContent).toContain("我的回應");

    fireEvent.click(container.querySelector(".chat__reply-banner__x")!);
    fireEvent.click(rowOf(container, "c-3").querySelector(".chat__msg-reply")!);
    fireEvent.change(input(container), { target: { value: "第三次" } });
    fireEvent.click(container.querySelector(".chat__send")!);

    // c-3, not c-1 and not c-2: the send must carry the LAST aim, and a
    // cancelled one must not come back.
    await waitFor(() =>
      expect(send).toHaveBeenCalledWith("第三次", undefined, "c-3"),
    );
  });

  it("a sent reply shows what it answered, and the quote clicks back to it", () => {
    messages = [
      ...messages,
      mkMsg({ id: "c-5", from: "owner", to: "m1", body: "答案", ts: 5, replyTo: "c-1" }),
    ];
    const { container } = renderChat();

    const quote = rowOf(container, "c-5").querySelector(".chat__msg-quote")!;
    expect(quote.textContent).toContain("Mira");
    expect(quote.textContent).toContain("第一個問題");
    // It is part of the MESSAGE, not a strip beside it (owner 2026-08-20).
    expect(quote.closest(".chat__msg-bubble")).toBeTruthy();
    // In the loaded window ⇒ the row offers a real jump control, and using it
    // locates the quoted row (the same highlight the 跳到訊息 route uses).
    const jump = quote.querySelector("[data-testid='msg-quote-jump']")!;
    expect(jump).toBeTruthy();
    scrolled = [];
    fireEvent.click(jump);
    expect(rowOf(container, "c-1").className).toContain("chat__msg--located");
    // …and it really asked THAT row to come into view. Without this, a handler
    // that only set the highlight class would pass.
    expect(scrolled).toContain(rowOf(container, "c-1"));
  });

  it("a quote whose target is older than the loaded window says so instead of pretending", async () => {
    messages = [
      mkMsg({
        id: "c-9",
        from: "owner",
        to: "m1",
        body: "回覆很久以前那則",
        ts: 9,
        replyTo: "c-longgone",
      }),
    ];
    const { container } = renderChat();

    const quote = rowOf(container, "c-9").querySelector(".chat__msg-quote")!;
    // NO jump control: an affordance that scrolls nowhere is worse than a line
    // that never offered one.
    expect(quote.querySelector("[data-testid='msg-quote-jump']")).toBeNull();
    // The label is the SETTLED state, reached only after the by-id read has
    // been tried and missed — "not resolved yet" and "asked and not there" are
    // different states and the row must not show the miss before it is one.
    await waitFor(() =>
      expect(quote.textContent).toContain("較早的一則訊息"),
    );
  });
});
