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

import { StrictMode } from "react";
import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, fireEvent, waitFor, act } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { ChatArea } from "./ChatArea";
import type { Member } from "../types";
import type { ChatMessage } from "../api/adapter";
import {
  resetChatDrafts,
  getChatDraft,
  saveChatDraft,
} from "../lib/chatDraftStore";
// The ACTIVE dictionary, not a copy of its strings. These tests assert on the
// i18n VALUE — a literal "回覆這則" here would go red the day someone re-words
// the button, which is not a defect, and would stay green if the label were
// swapped for a different key, which is.
import { zh } from "../i18n/locales/zh";

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
const m2 = mkMember("m2", "Kyle");

function renderChat() {
  return render(
    <I18nProvider>
      <ChatArea member={m1} />
    </I18nProvider>,
  );
}

/** The SAME tree the app actually mounts. main.tsx wraps the whole app in
 * <StrictMode>, which in dev runs every effect setup → cleanup → setup. That
 * is not a detail: it is a distinct execution order that has already broken
 * this feature once (review D1) in a way production could not reproduce, so
 * the quote path is rendered both ways. */
function renderChatStrict() {
  return render(
    <StrictMode>
      <I18nProvider>
        <ChatArea member={m1} />
      </I18nProvider>
    </StrictMode>,
  );
}

function pngFile(name: string): File {
  return new File([new Uint8Array([137, 80, 78, 71])], name, {
    type: "image/png",
  });
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

  // ── the accessibility surface ──────────────────────────────────────────────
  //
  // r18 F2: a reviewer stripped the aria-label AND title off all three controls,
  // removed the focus hand-off, and blanked the attachment excerpt — and all 666
  // frontend tests stayed green. Every test in this block exists because that
  // mutant survived.

  it("every control this feature adds has an accessible name", () => {
    // All three are ICON-ONLY buttons: without a name they are announced as
    // 「按鈕」 and a screen-reader user has three indistinguishable ones.
    messages = [
      mkMsg({ id: "c-1", from: "m1", to: "owner", body: "他說的" }),
      mkMsg({
        id: "c-2",
        from: "owner",
        to: "m1",
        body: "回它",
        ts: 2,
        replyTo: "c-1",
      }),
    ];
    const { container } = renderChat();

    // Guard against the vacuous version of this test: an empty dictionary value
    // would make every comparison below "" === "" and prove nothing.
    for (const v of [
      zh.chat.replyAction,
      zh.chat.replyCancel,
      zh.chat.replyQuoteJump,
    ]) {
      expect(v.length, "the dictionary value must not be empty").toBeGreaterThan(0);
    }

    const entry = rowOf(container, "c-1").querySelector(".chat__msg-reply")!;
    expect(entry.getAttribute("aria-label")).toBe(zh.chat.replyAction);
    expect(entry.getAttribute("title")).toBe(zh.chat.replyAction);

    // The jump lives on the reply's own row and has a visible label too — but
    // that label is the FIRST thing to be trimmed to an ellipsis when the bubble
    // runs out of room (see the CT guard), so the accessible name may not depend
    // on it surviving.
    const jump = rowOf(container, "c-2").querySelector(
      "[data-testid='msg-quote-jump']",
    )!;
    expect(jump.getAttribute("aria-label")).toBe(zh.chat.replyQuoteJump);
    expect(jump.getAttribute("title")).toBe(zh.chat.replyQuoteJump);

    fireEvent.click(entry);
    const x = container.querySelector(".chat__reply-banner__x")!;
    expect(x.getAttribute("aria-label")).toBe(zh.chat.replyCancel);
    expect(x.getAttribute("title")).toBe(zh.chat.replyCancel);
  });

  it("clicking the entry puts the caret in the composer", () => {
    // The point of the whole control is 「我要回這一則」 — landing the owner
    // anywhere but the input means the next thing they type goes nowhere.
    const { container } = renderChat();
    expect(document.activeElement).not.toBe(input(container));

    fireEvent.click(rowOf(container, "c-1").querySelector(".chat__msg-reply")!);

    expect(document.activeElement).toBe(input(container));
  });

  it("cancelling with the x gives the focus BACK to the composer", () => {
    // r18 F1. The x unmounts itself, and a focused element leaving the document
    // hands focus to <body>: a keyboard user who cancels one reply is dropped at
    // the top of the page and has to Tab through the entire thread to get back
    // to the input. Reproduced before the fix.
    const { container } = renderChat();
    fireEvent.click(rowOf(container, "c-1").querySelector(".chat__msg-reply")!);
    const x = container.querySelector(".chat__reply-banner__x") as HTMLElement;
    // Focus really is on the x first — otherwise "focus ends up in the input"
    // could be satisfied by it having never left.
    x.focus();
    expect(document.activeElement).toBe(x);

    fireEvent.click(x);

    expect(banner(container)).toBeNull();
    expect(document.activeElement).toBe(input(container));
  });

  it("quotes an attachment-only message by its attachment label, never as a blank", () => {
    // Reported in r16 and r17 and not fixed either time. A message with no text
    // has nothing to excerpt, and 「正在回覆 X」 followed by empty space reads as
    // a half-rendered banner rather than as a picture. Both surfaces that quote
    // — the composer banner and the sent reply's quote row — have to say it.
    messages = [
      mkMsg({
        id: "c-att",
        from: "m1",
        to: "owner",
        ts: 1,
        attachments: [
          {
            id: "a1",
            url: "/x",
            filename: "p.png",
            mime: "image/png",
            isImage: true,
          },
        ],
      }),
      mkMsg({
        id: "c-reply",
        from: "owner",
        to: "m1",
        body: "收到",
        ts: 2,
        replyTo: "c-att",
      }),
    ];
    expect(zh.chat.replyQuoteAttachment.length).toBeGreaterThan(0);
    const { container } = renderChat();

    // ① the sent reply's quote row
    const quoteBody = rowOf(container, "c-reply").querySelector(
      ".chat__msg-quote__body",
    )!;
    expect(quoteBody.textContent).toBe(zh.chat.replyQuoteAttachment);

    // ② the composer banner, aiming at the same message
    fireEvent.click(rowOf(container, "c-att").querySelector(".chat__msg-reply")!);
    const bannerBody = container.querySelector(".chat__reply-banner__body")!;
    expect(bannerBody.textContent).toBe(zh.chat.replyQuoteAttachment);
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

  it("resolves an out-of-window quote even when the composer re-renders mid-flight", async () => {
    // T-4e95 review B1. The by-id read is fired from an effect keyed on the set
    // of unresolved ids, and asking marks them in a ref WITHOUT re-rendering. So
    // the very next render — one keystroke is enough — recomputes that key as
    // empty, React tears down the previous effect, and a cleanup that cancelled
    // the in-flight read would drop the answer. The ids are already marked
    // asked, so nothing would ever ask again: the quote sits at "…" forever.
    //
    // The typing below is the WHOLE test. Without it the effect is never torn
    // down and the bug cannot appear — which is exactly why the existing
    // out-of-window test passed while this was broken.
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
    fireEvent.change(input(container), { target: { value: "一" } });
    fireEvent.change(input(container), { target: { value: "一二" } });

    const quote = rowOf(container, "c-9").querySelector(".chat__msg-quote")!;
    await waitFor(() =>
      expect(quote.textContent).toContain("較早的一則訊息"),
    );
  });

  it("offers NO reply entry on a message the owner is not a party to", () => {
    // 🔴 THE AC IS 每一則, BUT A REPLY HAS TO BE SENDABLE. This thread also shows
    // messages the owner is not in: an inter-agent run (Mira→Kyle), and the
    // server-authored 「系統」 line. A reply addresses {owner, peer} and carries
    // the quoted id, and the server refuses a target from another conversation —
    // the owner's own ruling. So an entry on those rows is a button that 400s
    // every time, and the composer only console.warns on failure: the message
    // vanishes, the banner stays, nothing explains it. Reviewed and reproduced
    // before this guard existed.
    messages = [
      mkMsg({ id: "c-1", from: "m1", to: "owner", body: "他問我的" }),
      mkMsg({ id: "c-own", from: "owner", to: "m1", body: "我回的", ts: 2 }),
      mkMsg({ id: "c-ia", from: "m1", to: "m9", body: "我轉給別人的", ts: 3 }),
    ];
    const { container } = renderChat();

    // Positive control: the rows that CAN be replied to still carry one, so a
    // predicate that simply removed every entry would not pass this.
    for (const id of ["c-1", "c-own"]) {
      expect(
        rowOf(container, id).querySelector(".chat__msg-reply"),
        `${id} should keep its entry`,
      ).not.toBeNull();
    }
    // The inter-agent run renders collapsed, so open it before looking — a row
    // that is merely absent would satisfy this assertion without the predicate
    // doing anything.
    fireEvent.click(
      container.querySelector(".chat__inter [aria-expanded]") as HTMLElement,
    );
    const ia = rowOf(container, "c-ia");
    expect(ia, "the inter-agent row is on screen once expanded").not.toBeNull();
    expect(
      ia.querySelector(".chat__msg-reply"),
      "an inter-agent row must not offer a reply that cannot be sent",
    ).toBeNull();
    // NOT covered here: a server-authored 「系統」 line (sender "system"). It is
    // refused by the same rule and the predicate excludes it, but it does not
    // render as a plain row in this harness, so this guard does not witness it.
  });

  it("names the owner's own message with the owner's label, never the raw id", () => {
    // The commonest thing to reply to is your own last line, and nameOf had no
    // owner branch — this is the first display path that feeds the owner's own
    // id into it, so it fell through to the raw "owner" and the banner read
    // 「正在回覆 owner」 next to a topbar that says 「CEO（你）」.
    messages = [
      mkMsg({ id: "c-own", from: "owner", to: "m1", body: "我自己說的那句" }),
    ];
    const { container } = renderChat();

    fireEvent.click(rowOf(container, "c-own").querySelector(".chat__msg-reply")!);
    const text = banner(container)!.textContent ?? "";
    expect(text).not.toContain("owner");
    expect(text).toContain("CEO");
  });

  it("opens a collapsed inter-agent block rather than offering a jump that goes nowhere", async () => {
    // `quoteLocatable` asks the LOADED WINDOW; locateMessage searches the DOM.
    // They are not the same set: an inter-agent run renders as one collapsed
    // block, so a quote pointing into it rendered a live button that did
    // nothing at all — the exact thing the quote line's own comment promises it
    // will not ship.
    messages = [
      mkMsg({ id: "c-ia", from: "m1", to: "m9", body: "被引用的那則", ts: 1 }),
      mkMsg({
        id: "c-own",
        from: "owner",
        to: "m1",
        body: "回覆它",
        ts: 2,
        replyTo: "c-ia",
      }),
    ];
    const { container } = renderChat();

    // Precondition: the target really is out of the document to begin with.
    expect(rowOf(container, "c-ia")).toBeNull();
    const jump = rowOf(container, "c-own").querySelector(
      "[data-testid='msg-quote-jump']",
    ) as HTMLButtonElement;
    expect(jump, "the quote is locatable, so the jump is offered").not.toBeNull();

    fireEvent.click(jump);

    await waitFor(() => expect(rowOf(container, "c-ia")).not.toBeNull());
    // …and it really was asked to come into view. Expanding without scrolling
    // would leave the owner looking at the same screen.
    expect(scrolled).toContain(rowOf(container, "c-ia"));
  });

  it("does not spill a failed send into whichever room is on screen when it fails", async () => {
    // The restore runs after an await and this component is REUSED across
    // peers. Reviewed and reproduced: the previous room's text AND its reply
    // target were restored into the next room and persisted into that room's
    // draft. The target half is worse than untidy — it belongs to another
    // conversation, so the new room's composer then 400s on every send.
    messages = [mkMsg({ id: "c-1", from: "m1", to: "owner", body: "他說的" })];
    let reject: (e: Error) => void = () => {};
    send.mockImplementationOnce(
      () => new Promise((_, r) => (reject = r)) as Promise<void>,
    );

    const { container, rerender } = render(
      <I18nProvider>
        <ChatArea member={m1} />
      </I18nProvider>,
    );
    fireEvent.click(rowOf(container, "c-1").querySelector(".chat__msg-reply")!);
    // A STAGED FILE TOO. Every earlier version of this test sent text only, so
    // writing `attachments: []` into the store passed all of them — the third
    // of the three things a failed send can lose had nothing standing on it.
    fireEvent.change(
      container.querySelector(".chat__file-input") as HTMLInputElement,
      { target: { files: [pngFile("p.png")] } },
    );
    await waitFor(() =>
      expect(container.querySelectorAll(".chat__preview-thumb").length).toBe(1),
    );
    fireEvent.change(input(container), { target: { value: "給 m1 的" } });
    fireEvent.keyDown(input(container), { key: "Enter" });

    rerender(
      <I18nProvider>
        <ChatArea member={m2} />
      </I18nProvider>,
    );
    await act(async () => {
      reject(new Error("nope"));
      await Promise.resolve();
    });

    // The room on screen is untouched…
    expect(input(container).value).toBe("");
    expect(banner(container)).toBeNull();
    expect(getChatDraft("m2")).toBeUndefined();

    // …AND — the half this test used to be missing — the words are still
    // somewhere. Three negative assertions are also all satisfied by throwing
    // the message away, which is what the first version of the guard actually
    // did: the optimistic clear had already deleted m1's draft, so the early
    // return left the text, the attachment and the reply target nowhere at all.
    // A reviewer replaced the whole restore with an unconditional `return` and
    // all 128 ChatArea tests stayed green. This is that missing assertion.
    const kept = getChatDraft("m1");
    expect(kept?.text).toBe("給 m1 的");
    expect(kept?.replyTo).toBe("c-1");
    expect(kept?.attachments).toHaveLength(1);
    expect(kept?.attachments[0].filename).toBe("p.png");
  });

  it("puts a failed send back ON SCREEN when the owner never left the room", async () => {
    // The room's draft is not enough on its own: if the owner is still looking
    // at this conversation, the words have to come back to the composer they
    // vanished from. A reviewer wrote the store and then returned immediately,
    // never restoring the composer — and all 129 tests stayed green, because
    // every one of them switched rooms or unmounted first. This is the case
    // nothing was standing on.
    messages = [mkMsg({ id: "c-1", from: "m1", to: "owner", body: "他說的" })];
    let reject: (e: Error) => void = () => {};
    send.mockImplementationOnce(
      () => new Promise((_, r) => (reject = r)) as Promise<void>,
    );

    const { container } = renderChat();
    fireEvent.click(rowOf(container, "c-1").querySelector(".chat__msg-reply")!);
    fireEvent.change(input(container), { target: { value: "給 m1 的" } });
    fireEvent.keyDown(input(container), { key: "Enter" });

    expect(input(container).value, "optimistically cleared").toBe("");

    await act(async () => {
      reject(new Error("nope"));
      await Promise.resolve();
    });

    expect(input(container).value).toBe("給 m1 的");
    expect(banner(container), "still aimed at what it was answering").not.toBeNull();
  });

  it("fills only what the room does not already hold, field by field", async () => {
    // The first version of the store write was all-or-nothing: it wrote nothing
    // at all if the room held ANYTHING. So a room the owner had gone back to
    // and put one thing into swallowed the rest of the failed message. The rule
    // is per field — the same one the on-screen restore uses.
    messages = [mkMsg({ id: "c-1", from: "m1", to: "owner", body: "他說的" })];
    let reject: (e: Error) => void = () => {};
    send.mockImplementationOnce(
      () => new Promise((_, r) => (reject = r)) as Promise<void>,
    );

    const { container, rerender } = render(
      <I18nProvider>
        <ChatArea member={m1} />
      </I18nProvider>,
    );
    fireEvent.click(rowOf(container, "c-1").querySelector(".chat__msg-reply")!);
    fireEvent.change(input(container), { target: { value: "給 m1 的" } });
    fireEvent.keyDown(input(container), { key: "Enter" });

    rerender(
      <I18nProvider>
        <ChatArea member={m2} />
      </I18nProvider>,
    );
    // m1 is not empty any more — but what it holds is only TEXT.
    saveChatDraft("m1", { text: "我後來又打的", attachments: [] });

    await act(async () => {
      reject(new Error("nope"));
      await Promise.resolve();
    });

    expect(getChatDraft("m1")).toEqual({
      // theirs wins — two texts cannot share one composer
      text: "我後來又打的",
      attachments: [],
      // …but the reply target had nothing to collide with, so it survives
      replyTo: "c-1",
    });
  });

  it("does not clobber a picture the owner staged in that room while the send was away", async () => {
    // The case the comment above the fix names in words — "go back to that room,
    // stage one image and type nothing" — and which nothing was standing on: a
    // reviewer made the attachments field write the snapshot UNCONDITIONALLY and
    // all 1310 tests stayed green. The room's own picture would be overwritten,
    // and since an empty draft is deleted outright there is no way back.
    messages = [mkMsg({ id: "c-1", from: "m1", to: "owner", body: "他說的" })];
    let reject: (e: Error) => void = () => {};
    send.mockImplementationOnce(
      () => new Promise((_, r) => (reject = r)) as Promise<void>,
    );

    const { container, rerender } = render(
      <I18nProvider>
        <ChatArea member={m1} />
      </I18nProvider>,
    );
    fireEvent.click(rowOf(container, "c-1").querySelector(".chat__msg-reply")!);
    fireEvent.change(input(container), { target: { value: "給 m1 的" } });
    fireEvent.keyDown(input(container), { key: "Enter" });

    rerender(
      <I18nProvider>
        <ChatArea member={m2} />
      </I18nProvider>,
    );
    // Back in m1 the owner staged a picture and typed nothing.
    saveChatDraft("m1", {
      text: "",
      attachments: [
        {
          key: "k-theirs",
          dataUri: "data:image/png;base64,AAAA",
          filename: "後來貼的.png",
          mime: "image/png",
          size: 4,
          isImage: true,
        },
      ],
    });

    await act(async () => {
      reject(new Error("nope"));
      await Promise.resolve();
    });

    const kept = getChatDraft("m1");
    // Theirs survives…
    expect(kept?.attachments).toHaveLength(1);
    expect(kept?.attachments[0].filename).toBe("後來貼的.png");
    // …and the fields the room had nothing in still come back.
    expect(kept?.text).toBe("給 m1 的");
    expect(kept?.replyTo).toBe("c-1");
  });

  it("gives the room back its OWN reply target, not the failed send's", async () => {
    // Polarity pin for the replyTo field. Inverting it left all 1310 tests
    // green: every other case has the room holding no target at all, so both
    // polarities produce the same answer there.
    messages = [
      mkMsg({ id: "c-1", from: "m1", to: "owner", body: "他說的" }),
      mkMsg({ id: "c-2", from: "m1", to: "owner", body: "他說的第二句", ts: 2 }),
    ];
    let reject: (e: Error) => void = () => {};
    send.mockImplementationOnce(
      () => new Promise((_, r) => (reject = r)) as Promise<void>,
    );

    const { container, rerender } = render(
      <I18nProvider>
        <ChatArea member={m1} />
      </I18nProvider>,
    );
    fireEvent.click(rowOf(container, "c-1").querySelector(".chat__msg-reply")!);
    fireEvent.change(input(container), { target: { value: "給 m1 的" } });
    fireEvent.keyDown(input(container), { key: "Enter" });

    rerender(
      <I18nProvider>
        <ChatArea member={m2} />
      </I18nProvider>,
    );
    // Back in m1 the owner aimed at a DIFFERENT message and typed something.
    saveChatDraft("m1", {
      text: "我後來又打的",
      attachments: [],
      replyTo: "c-2",
    });

    await act(async () => {
      reject(new Error("nope"));
      await Promise.resolve();
    });

    const kept = getChatDraft("m1");
    expect(kept?.text).toBe("我後來又打的");
    expect(kept?.replyTo, "the room's own aim, not the failed send's").toBe(
      "c-2",
    );
  });

  it("keeps a failed send when the owner has left the conversation entirely", async () => {
    // Same defect, second door: 跳頁 while the send is in flight unmounts the
    // composer, so restoring into component state discards it just as quietly
    // as restoring into the wrong room did. The room's draft is the only place
    // that outlives the component.
    messages = [mkMsg({ id: "c-1", from: "m1", to: "owner", body: "他說的" })];
    let reject: (e: Error) => void = () => {};
    send.mockImplementationOnce(
      () => new Promise((_, r) => (reject = r)) as Promise<void>,
    );

    const { container, unmount } = render(
      <I18nProvider>
        <ChatArea member={m1} />
      </I18nProvider>,
    );
    fireEvent.click(rowOf(container, "c-1").querySelector(".chat__msg-reply")!);
    fireEvent.change(input(container), { target: { value: "給 m1 的" } });
    fireEvent.keyDown(input(container), { key: "Enter" });

    unmount();
    await act(async () => {
      reject(new Error("nope"));
      await Promise.resolve();
    });

    expect(getChatDraft("m1")).toEqual({
      text: "給 m1 的",
      attachments: [],
      replyTo: "c-1",
    });
  });

  it("still settles the quote under <StrictMode> — the dev-only double-invoke", async () => {
    // T-4e95 review D1. The fix for B1 kept a `mounted` ref, and a version of
    // it with ONLY a cleanup was correct under a single mount and broken under
    // StrictMode: setup → cleanup → setup sets the ref false on that first
    // teardown and nothing sets it back, so from mount onwards every by-id
    // answer is discarded and the quote sits at "…" — B1's symptom, restored.
    //
    // 🔴 IT ONLY BREAKS IN DEV, which is why this test exists rather than a
    // comment. main.tsx wraps the app in <StrictMode>; production does not
    // double-invoke, so the broken version shipped looking perfectly fine and
    // was broken for everyone developing against it. Rendering through the
    // ordinary helper cannot see this at all: with the fix reverted, the whole
    // 2239-test suite stayed green.
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
    const { container } = renderChatStrict();

    const quote = rowOf(container, "c-9").querySelector(".chat__msg-quote")!;
    await waitFor(() =>
      expect(quote.textContent).toContain("較早的一則訊息"),
    );
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
    //
    // ⚠️ THIS PAIR IS THE TEST, not the waitFor alone. `waitFor` succeeds if its
    // callback holds on the FIRST frame, so on its own it cannot tell the two
    // states apart — a reviewer swapped the undefined and null arms (which ships
    // both symptoms this hook's header promises never to ship: claiming a miss
    // before asking, and never settling) and all 20 tests here stayed green.
    // The unresolved assertion below is what makes that a red.
    expect(quote.textContent, "not asked yet ⇒ neither a name nor a miss").toContain(
      "\u2026",
    );
    expect(quote.textContent).not.toContain("較早的一則訊息");
    await waitFor(() =>
      expect(quote.textContent).toContain("較早的一則訊息"),
    );
  });

  it("the banner does NOT name anyone while the quoted message is unresolved", async () => {
    // The banner used to fall back to the PEER's name whenever the quote had not
    // come back. That is a claim, not a placeholder: this conversation has only
    // two people, so the fallback is a coin flip printed as a fact — and it sat
    // next to the banner's own second half honestly saying 「較早的一則訊息」.
    // Reproduced before the fix as: 「正在回覆 Mira較早的一則訊息」.
    // The way this really happens: the owner aimed at something, went away, and
    // by the time they come back the target has scrolled out of the loaded
    // window — so the draft restores a target the window cannot resolve.
    messages = [mkMsg({ id: "c-1", from: "m1", to: "owner", body: "他說的" })];
    saveChatDraft("m1", {
      text: "",
      attachments: [],
      replyTo: "c-longgone",
    });
    const { container } = renderChat();

    const b = banner(container)!;
    expect(b).not.toBeNull();
    // Not asked yet.
    expect(b.textContent).toContain("\u2026");
    expect(b.textContent, "never the peer's name").not.toContain("Mira");
    // Asked and missed.
    await waitFor(() => expect(b.textContent).toContain("較早的一則訊息"));
    expect(b.textContent, "still never the peer's name").not.toContain("Mira");
  });

  it("a reply target does not follow the owner into the next conversation", async () => {
    // The peer-switch block clears it in the same render-phase adjustment that
    // swaps the draft, and its comment says MUST — but nothing was standing on
    // that line: deleting it outright left all 241 ChatArea tests green. The
    // failure it prevents is the silent one: a target from the previous room is
    // refused by the server on every send, and the composer's only failure
    // handling is a console.warn, so the message just disappears.
    messages = [mkMsg({ id: "c-1", from: "m1", to: "owner", body: "他說的" })];
    const { container, rerender } = render(
      <I18nProvider>
        <ChatArea member={m1} />
      </I18nProvider>,
    );
    fireEvent.click(rowOf(container, "c-1").querySelector(".chat__msg-reply")!);
    expect(banner(container), "aimed in m1").not.toBeNull();

    rerender(
      <I18nProvider>
        <ChatArea member={m2} />
      </I18nProvider>,
    );
    expect(banner(container), "m2 inherits nothing").toBeNull();

    // Positive control: coming BACK must restore m1's own target, or "clear it
    // always" would pass this test too.
    rerender(
      <I18nProvider>
        <ChatArea member={m1} />
      </I18nProvider>,
    );
    expect(banner(container), "m1 keeps its own").not.toBeNull();
  });
});
