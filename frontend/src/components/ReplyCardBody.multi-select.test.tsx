// T-40: the reply card's options became a SET, and the AI recommendation moved
// off position onto each option's own ai_pick. These are the claims the
// cockpit half of that change rests on, and nothing else in the tree holds
// them: the DTO-parity gate does not cover ReplyCard, the style-ownership gate
// does not own replies.css, and the payload-parity roll-call does not list a
// card's inner fields.
//
// T-40b (owner, card rc-06bc715358c2 + 2026-08-31 follow-ups) splits the two
// card kinds apart again, and every claim below that names "single" is one of
// the halves that changed:
//   • a SINGLE card answers on the CLICK — no send button in the loop — and
//     carries whatever is already typed with it;
//   • a MULTI card still stages behind one send button;
//   • the chip's leading ordinal became a tick box on a MULTI card, which is
//     also how the two kinds are told apart on screen. A SINGLE card kept the
//     1/2/3/4 ordinal (owner 2026-08-31): it answers on the click, so it has no
//     "ticked but not yet sent" state for a radio to show, and the radio that
//     briefly sat there was never seen in its selected form at all.

import { describe, it, expect, vi, afterEach } from "vitest";
import { render, fireEvent } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import {
  ReplyCardAnsweredBody,
  ReplyCardExpiredBody,
  ReplyCardWaitingBody,
} from "./ReplyCardBody";
import type { ReplyCard, ReplyCardAnswerInput } from "../api/adapter";

function mkCard(over: Partial<ReplyCard> = {}): ReplyCard {
  return {
    id: "rc-1",
    from: "mira",
    kind: "decision",
    summary: "這批要走哪幾條線？",
    body: "",
    // ai_pick sits on the SECOND option ON PURPOSE. A reader that still keys
    // off index 0 agrees with a first-option fixture no matter what it does.
    options: [
      { text: "走海運", aiPick: false },
      { text: "走空運", aiPick: true },
      { text: "先擱著", aiPick: false },
    ],
    selectMode: "multi",
    status: "waiting",
    attachments: [],
    createdTs: Date.now() / 1000 - 600,
    answeredTs: null,
    chatMessageId: "msg-1",
    answer: null,
    ...over,
  };
}

/** A card whose options carry NO ai_pick. A MULTI card now OPENS with the
 * ai_pick options ticked, so a test about the staging mechanics themselves
 * ("nothing ticked" → send disabled, tick order, …) has to start from a card
 * that genuinely starts empty. The ai_pick fixtures above stay where the tag or
 * the default tick is what is under test. */
function mkPlainCard(over: Partial<ReplyCard> = {}): ReplyCard {
  return mkCard({
    options: [
      { text: "走海運", aiPick: false },
      { text: "走空運", aiPick: false },
      { text: "先擱著", aiPick: false },
    ],
    ...over,
  });
}

function renderWaiting(card: ReplyCard) {
  const sent: ReplyCardAnswerInput[] = [];
  const u = render(
    <I18nProvider>
      <ReplyCardWaitingBody
        card={card}
        onAnswer={async (input) => {
          sent.push(input);
        }}
      />
    </I18nProvider>,
  );
  const chips = () => [...u.container.querySelectorAll(".reply-option")];
  const marks = () => [
    ...u.container.querySelectorAll(".reply-option__mark"),
  ];
  const send = () => u.container.querySelector(".chat__send") as HTMLButtonElement;
  return { ...u, sent, chips, marks, send };
}

afterEach(() => {
  vi.restoreAllMocks();
});

describe("ReplyCardWaitingBody", () => {
  it("keeps the send button disabled and fires no answer while nothing is ticked and nothing is typed", async () => {
    const { sent, chips, send, getByPlaceholderText } = renderWaiting(mkPlainCard());

    expect(send().disabled).toBe(true);
    fireEvent.click(send());
    expect(sent).toEqual([]);
    // …and the keyboard route is refused too. jsdom does not dispatch a click
    // on a disabled button, so the assertion above only ever exercises the
    // attribute; Enter reaches the submit path itself, which has to refuse an
    // empty answer on its own.
    fireEvent.keyDown(getByPlaceholderText("輸入回覆…"), { key: "Enter" });
    expect(sent).toEqual([]);

    // One tick arms it; un-ticking that same chip disarms it again — "nothing
    // circled" has to stay reachable, because that is the state this button
    // refuses to send. (A MULTI card: on a single one the first click would
    // already have answered.)
    fireEvent.click(chips()[0]);
    expect(send().disabled).toBe(false);
    fireEvent.click(chips()[0]);
    expect(send().disabled).toBe(true);
    fireEvent.click(send());
    expect(sent).toEqual([]);
  });

  it("sends the same answer whichever order the same options were ticked in", async () => {
    const a = renderWaiting(mkPlainCard());
    fireEvent.click(a.chips()[2]);
    fireEvent.click(a.chips()[0]);
    fireEvent.click(a.send());
    a.unmount();

    const b = renderWaiting(mkPlainCard());
    fireEvent.click(b.chips()[0]);
    fireEvent.click(b.chips()[2]);
    fireEvent.click(b.send());
    b.unmount();

    expect(a.sent).toEqual([
      { optionIdxs: [0, 2], text: "", attachments: [] },
    ]);
    expect(a.sent).toEqual(b.sent);
  });

  it("carries the ticked options and the typed text in ONE answer", async () => {
    const { sent, chips, send, getByPlaceholderText } = renderWaiting(
      mkPlainCard(),
    );
    fireEvent.click(chips()[1]);
    fireEvent.change(getByPlaceholderText("輸入回覆…"), {
      target: { value: "空運那條要走 DHL" },
    });
    fireEvent.click(send());

    // ONE call, not two: the options and the free text used to be two separate
    // POSTs, and the second one hit the one-shot close as a 409.
    expect(sent).toEqual([
      { optionIdxs: [1], text: "空運那條要走 DHL", attachments: [] },
    ]);
  });

  it("answers a single card on the click itself and only stages on a multi card", async () => {
    // owner, rc-06bc715358c2: 「單選卡點一下就送出，多選卡才要按送出」. One click,
    // one answer — the send button is not in this loop at all, so a test that
    // pressed it afterwards could not tell a click-to-send card from a staging
    // one.
    const single = renderWaiting(mkCard({ selectMode: "single" }));
    fireEvent.click(single.chips()[2]);
    expect(single.sent).toEqual([
      { optionIdxs: [2], text: "", attachments: [] },
    ]);
    single.unmount();

    const multi = renderWaiting(mkPlainCard());
    fireEvent.click(multi.chips()[0]);
    fireEvent.click(multi.chips()[2]);
    expect(multi.sent, "a multi card must not answer on a tick").toEqual([]);
    fireEvent.click(multi.send());
    expect(multi.sent).toEqual([
      { optionIdxs: [0, 2], text: "", attachments: [] },
    ]);
  });

  it("carries the half-typed text along when a single card is answered by a click", async () => {
    // 「點了就送」 must not be paid for with the words already in the box: the
    // click routes THROUGH the composer, so the draft rides the same answer.
    const { sent, chips, getByPlaceholderText } = renderWaiting(
      mkCard({ selectMode: "single" }),
    );
    fireEvent.change(getByPlaceholderText("輸入回覆…"), {
      target: { value: "但空運要先問報價" },
    });
    fireEvent.click(chips()[1]);
    expect(sent).toEqual([
      { optionIdxs: [1], text: "但空運要先問報價", attachments: [] },
    ]);
  });

  it("keeps refusing a wholly empty answer on a single card, where the send button is still the only text route", async () => {
    // The empty-answer guard is NOT a casualty of click-to-send: a single card
    // whose owner typed nothing and clicked nothing must still be unsendable.
    const { sent, send, getByPlaceholderText } = renderWaiting(
      mkCard({ selectMode: "single" }),
    );
    expect(send().disabled).toBe(true);
    fireEvent.keyDown(getByPlaceholderText("輸入回覆…"), { key: "Enter" });
    expect(sent).toEqual([]);

    // …and a text-only answer on a single card circles nothing.
    fireEvent.change(getByPlaceholderText("輸入回覆…"), {
      target: { value: "都不要" },
    });
    fireEvent.click(send());
    expect(sent).toEqual([{ text: "都不要", attachments: [] }]);
  });

  it("marks a multi card's options with a tick box and a single card's with 1/2/3", async () => {
    // The tick box is the card kind said wordlessly, AND — since accent paint
    // means only 「你選的」 — the shape half of "is this one ticked". A single
    // card has neither job to do: one click answers it, so nothing on it is
    // ever "ticked but not sent", and the ordinal is what the owner reads.
    // Assert the CLASS, which is what replies.css paints from, and the chip's
    // own TEXT. A data-* label alongside would be a decoy: inverting the class
    // alone left an attribute-only assertion green (measured).
    const multi = renderWaiting(mkCard());
    expect(
      multi.marks().map((e) => e.className.includes("reply-option__mark--check")),
    ).toEqual([true, true, true]);
    expect(
      multi.chips().map((e) => e.textContent),
      "no ordinal on a multi card — the tick box carries no text",
    ).toEqual(["走海運", "走空運AI 建議", "先擱著"]);
    expect(multi.container.querySelectorAll(".reply-option__num")).toHaveLength(0);
    multi.unmount();

    const single = renderWaiting(mkCard({ selectMode: "single" }));
    expect(
      [...single.container.querySelectorAll(".reply-option__num")].map(
        (e) => e.textContent,
      ),
      "a single card lists 1/2/3 again",
    ).toEqual(["1", "2", "3"]);
    expect(
      single.chips().map((e) => e.textContent),
      "and the ordinal reads as part of the chip's own line",
    ).toEqual(["1走海運", "2走空運AI 建議", "3先擱著"]);
    expect(single.marks()).toHaveLength(0);
  });

  it("gives multi chips checkbox semantics and leaves single chips plain buttons", async () => {
    const multi = renderWaiting(mkPlainCard());
    expect(multi.chips().map((e) => e.getAttribute("role"))).toEqual([
      "checkbox",
      "checkbox",
      "checkbox",
    ]);
    expect(multi.chips().map((e) => e.getAttribute("aria-checked"))).toEqual([
      "false",
      "false",
      "false",
    ]);
    // aria-pressed is a DIFFERENT promise (a toggle button) and must not ride
    // alongside — a screen reader would be told two things about one widget.
    expect(multi.chips().every((e) => !e.hasAttribute("aria-pressed"))).toBe(
      true,
    );
    fireEvent.click(multi.chips()[2]);
    expect(multi.chips()[2].getAttribute("aria-checked")).toBe("true");
    multi.unmount();

    // A single chip is a button that DOES something when pressed. It is not a
    // radio: `aria-checked` would be announced "false" for the chip's whole
    // life, because the click that would set it also answers the card and
    // takes the chip away. A promise nothing can keep is worse than none.
    const single = renderWaiting(mkCard({ selectMode: "single" }));
    expect(single.chips().every((e) => !e.hasAttribute("role"))).toBe(true);
    expect(single.chips().every((e) => !e.hasAttribute("aria-checked"))).toBe(
      true,
    );
    expect(
      single.container
        .querySelector(".reply-card__options")!
        .hasAttribute("role"),
      "and no radiogroup around them either",
    ).toBe(false);
  });

  it("tags the option that carries ai_pick, and counts the ticks on a multi card", async () => {
    const { chips, getByTestId } = renderWaiting(mkCard());

    // Every chip WHOLE: wording and exactly the tags it earned (a multi card
    // shows no ordinal). The
    // AI tag rides the SECOND option — that is where ai_pick is — and a reader
    // that still tags index 0 disagrees with both of the first two strings.
    expect(chips().map((e) => e.textContent)).toEqual([
      "走海運",
      "走空運AI 建議",
      "先擱著",
    ]);
    // The count opens on the ai_pick default, not on zero.
    expect(getByTestId("reply-selected-count").textContent).toBe("已選 1 項");

    fireEvent.click(chips()[0]);
    fireEvent.click(chips()[2]);
    expect(getByTestId("reply-selected-count").textContent).toBe("已選 3 項");
    // The tags did not move when the ticks did: ai_pick is a property of the
    // OFFER, not of the answer.
    expect(chips().map((e) => e.getAttribute("data-selected"))).toEqual([
      "true",
      "true",
      "true",
    ]);
    expect(chips()[1].textContent).toBe("走空運AI 建議");
  });

  it("opens a multi card with the ai_pick options already ticked, and a single card with nothing", async () => {
    // owner 2026-08-31: 「多選的時候，UI 應該要預設就先把我勾好 AI 建議的」. TWO
    // ai_pick options, neither of them the first, so a reader that pre-ticks
    // index 0 or stops at the first hit disagrees here.
    const twoAi = (over: Partial<ReplyCard> = {}) =>
      mkCard({
        options: [
          { text: "走海運", aiPick: false },
          { text: "走空運", aiPick: true },
          { text: "先擱著", aiPick: true },
        ],
        ...over,
      });
    const multi = renderWaiting(twoAi());
    expect(multi.chips().map((e) => e.getAttribute("data-selected"))).toEqual([
      "false",
      "true",
      "true",
    ]);
    expect(multi.getByTestId("reply-selected-count").textContent).toBe(
      "已選 2 項",
    );
    multi.unmount();

    // 🔴 THE ONE THAT MATTERS. A single card SENDS on a tick, so a pre-ticked
    // single card would be a pre-SENT one — an answer the owner never gave.
    const single = renderWaiting(twoAi({ selectMode: "single" }));
    expect(
      single.chips().every((e) => e.getAttribute("data-selected") === "false"),
      "a single card must open with nothing picked — a tick there IS the answer",
    ).toBe(true);
    expect(single.sent, "and nothing may have been sent by the render").toEqual(
      [],
    );
    single.unmount();

    // A multi card with no recommendation at all opens empty, as it always did.
    const none = renderWaiting(mkPlainCard());
    expect(
      none.chips().every((e) => e.getAttribute("data-selected") === "false"),
    ).toBe(true);
    expect(none.getByTestId("reply-selected-count").textContent).toBe(
      "已選 0 項",
    );
    // …and the empty-answer guard still stands: the send button refuses it.
    expect(none.send().disabled).toBe(true);
    none.unmount();

    // The owner un-ticking the AI's defaults gets back to unsendable — the
    // guard is not bypassed by having started full.
    const undone = renderWaiting(twoAi());
    fireEvent.click(undone.chips()[1]);
    fireEvent.click(undone.chips()[2]);
    expect(undone.getByTestId("reply-selected-count").textContent).toBe(
      "已選 0 項",
    );
    expect(undone.send().disabled).toBe(true);
    fireEvent.click(undone.send());
    expect(undone.sent).toEqual([]);
  });

});

describe("ReplyCardAnsweredBody", () => {
  function answered(over: Partial<ReplyCard> = {}) {
    return mkCard({
      status: "answered",
      answeredTs: Date.now() / 1000 - 60,
      answer: { optionIdxs: [0, 2], text: "", attachments: [] },
      ...over,
    });
  }

  function renderAnswered(card: ReplyCard) {
    const sent: ReplyCardAnswerInput[] = [];
    const u = render(
      <I18nProvider>
        <ReplyCardAnsweredBody
          card={card}
          onReanswer={async (input) => {
            sent.push(input);
          }}
        />
      </I18nProvider>,
    );
    return { ...u, sent };
  }

  it("lists every circled option on the final answer row", async () => {
    const { getByTestId, getAllByTestId } = renderAnswered(answered());
    expect(
      getAllByTestId("reply-answer-option").map((e) => e.textContent),
    ).toEqual(["走海運", "先擱著"]);
    // 你選的 always; AI 建議 only because one of the circled options carries
    // ai_pick. Neither of the two circled indices is 0, so a positional reader
    // would print this row without the AI tag.
    expect(getByTestId("final-answer").textContent).toBe("你選的走海運先擱著");
  });

  it("drops the AI 建議 tag when no circled option carries ai_pick", async () => {
    const { getByTestId } = renderAnswered(
      answered({ answer: { optionIdxs: [0], text: "", attachments: [] } }),
    );
    expect(getByTestId("final-answer").textContent).toBe("你選的走海運");
  });

  it("seeds 重新決定 from the standing answer and revises it in one send", async () => {
    const { sent, container, getByText } = renderAnswered(answered());
    fireEvent.click(getByText("查看當初選項"));
    fireEvent.click(getByText("重新決定"));

    const chips = [...container.querySelectorAll(".reply-option")];
    // Edit mode opens on the answer that is standing — 「改掉三個裡的一個」 must
    // not start from nothing — and the 目前 tag keeps naming that same answer.
    expect(chips.map((e) => e.getAttribute("data-selected"))).toEqual([
      "true",
      "false",
      "true",
    ]);
    expect(chips.map((e) => e.textContent)).toEqual([
      "走海運目前",
      "走空運AI 建議",
      "先擱著目前",
    ]);

    fireEvent.click(chips[1]);
    fireEvent.click(container.querySelector(".chat__send")!);
    expect(sent).toEqual([
      { optionIdxs: [0, 1, 2], text: "", attachments: [] },
    ]);
  });

  it("re-decides a single card on the click, replacing the standing answer outright", async () => {
    const { sent, container, getByText } = renderAnswered(
      answered({
        selectMode: "single",
        answer: { optionIdxs: [0], text: "", attachments: [] },
      }),
    );
    fireEvent.click(getByText("查看當初選項"));
    fireEvent.click(getByText("重新決定"));
    const chips = [...container.querySelectorAll(".reply-option")];
    // Nothing is staged when edit mode opens on a single card — a seeded set
    // would arm the send button to re-submit the answer already standing — and
    // the click that follows IS the new decision.
    expect(chips.map((e) => e.getAttribute("data-selected"))).toEqual([
      "false",
      "false",
      "false",
    ]);
    fireEvent.click(chips[1]);
    expect(sent).toEqual([{ optionIdxs: [1], text: "", attachments: [] }]);
  });

  it("reviews the original options without offering a control", async () => {
    // 查看當初選項 BEFORE 重新決定 is a statement about a decision already
    // taken: a live-looking tick box would be an affordance that does nothing.
    const { container, getByText } = renderAnswered(answered());
    fireEvent.click(getByText("查看當初選項"));

    const chips = [...container.querySelectorAll(".reply-option")];
    expect(chips.every((e) => (e as HTMLButtonElement).disabled)).toBe(true);
    expect(chips.every((e) => !e.hasAttribute("role"))).toBe(true);
    expect(chips.every((e) => !e.hasAttribute("aria-checked"))).toBe(true);
    // …but it still SHOWS which ones were circled: the mark lights on the
    // standing answer, not on nothing.
    expect(
      [...container.querySelectorAll(".reply-option__mark")].map((e) =>
        e.className.includes("reply-option__mark--on"),
      ),
    ).toEqual([true, false, true]);
  });
});

describe("ReplyCardExpiredBody", () => {
  it("offers no control on a card nobody can answer any more", async () => {
    const { container } = render(
      <I18nProvider>
        <ReplyCardExpiredBody
          card={mkCard({ status: "expired", selectMode: "single" })}
        />
      </I18nProvider>,
    );
    expect(
      [...container.querySelectorAll(".reply-option")].every(
        (e) => (e as HTMLButtonElement).disabled,
      ),
    ).toBe(true);
    expect(
      [...container.querySelectorAll(".reply-option__num")].map(
        (e) => e.textContent,
      ),
      "an expired single card still lists its options 1/2/3",
    ).toEqual(["1", "2", "3"]);
  });
});
