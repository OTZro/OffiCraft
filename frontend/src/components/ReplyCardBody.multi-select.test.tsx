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
//   • the chip's leading ordinal became a tick box (multi) / radio (single),
//     which is also how the two kinds are told apart on screen;
//   • a line above the options says what a click is about to DO, because on a
//     single card a click cannot be taken back.

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
    const { sent, chips, send, getByPlaceholderText } = renderWaiting(mkCard());

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
    const a = renderWaiting(mkCard());
    fireEvent.click(a.chips()[2]);
    fireEvent.click(a.chips()[0]);
    fireEvent.click(a.send());
    a.unmount();

    const b = renderWaiting(mkCard());
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
    const { sent, chips, send, getByPlaceholderText } = renderWaiting(mkCard());
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

    const multi = renderWaiting(mkCard());
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

  it("says what kind of card this is and what a click will do", async () => {
    const multi = renderWaiting(mkCard());
    expect(multi.getByTestId("reply-mode-hint").textContent).toBe(
      "可以選多個，勾好按送出",
    );
    multi.unmount();

    const single = renderWaiting(mkCard({ selectMode: "single" }));
    // The consequence, not just the kind: on a single card the click IS the
    // answer and a reply card is one-shot.
    expect(single.getByTestId("reply-mode-hint").textContent).toBe(
      "點一下就送出",
    );
  });

  it("marks the options with a tick box on a multi card and a radio on a single one", async () => {
    // The shape IS the second, wordless statement of the card's kind — and it
    // replaced the 1/2/3 ordinal, which said nothing about either kind or
    // selection.
    // Assert the CLASS, which is what replies.css paints the shape from. A
    // data-* label alongside it would be a decoy: inverting the class alone
    // left an attribute-only assertion green (measured).
    const shapes = (marks: Element[]) =>
      marks.map((e) =>
        e.className.includes("reply-option__mark--check")
          ? "check"
          : e.className.includes("reply-option__mark--radio")
            ? "radio"
            : "none",
      );
    const multi = renderWaiting(mkCard());
    expect(shapes(multi.marks())).toEqual(["check", "check", "check"]);
    expect(
      multi.chips().map((e) => e.textContent),
      "the ordinal is gone — the mark carries no text",
    ).toEqual(["走海運", "走空運AI 建議", "先擱著"]);
    multi.unmount();

    const single = renderWaiting(mkCard({ selectMode: "single" }));
    expect(shapes(single.marks())).toEqual(["radio", "radio", "radio"]);
  });

  it("gives the chips the checked semantics of the control they now look like", async () => {
    const multi = renderWaiting(mkCard());
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

    const single = renderWaiting(mkCard({ selectMode: "single" }));
    expect(single.chips().map((e) => e.getAttribute("role"))).toEqual([
      "radio",
      "radio",
      "radio",
    ]);
    expect(
      single.container.querySelector(".reply-card__options")!.getAttribute("role"),
      "single-select radios need the group that makes them one choice",
    ).toBe("radiogroup");
  });

  it("tags the option that carries ai_pick, and counts the ticks on a multi card", async () => {
    const { chips, getByTestId } = renderWaiting(mkCard());

    // Every chip WHOLE: number, wording, and exactly the tags it earned. The
    // AI tag rides the SECOND option — that is where ai_pick is — and a reader
    // that still tags index 0 disagrees with both of the first two strings.
    expect(chips().map((e) => e.textContent)).toEqual([
      "走海運",
      "走空運AI 建議",
      "先擱著",
    ]);
    expect(getByTestId("reply-selected-count").textContent).toBe("已選 0 項");

    fireEvent.click(chips()[0]);
    fireEvent.click(chips()[2]);
    expect(getByTestId("reply-selected-count").textContent).toBe("已選 2 項");
    // The tags did not move when the ticks did: ai_pick is a property of the
    // OFFER, not of the answer.
    expect(chips().map((e) => e.getAttribute("data-selected"))).toEqual([
      "true",
      "false",
      "true",
    ]);
    expect(chips()[1].textContent).toBe("走空運AI 建議");
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

  it("reviews the original options without offering a control or a claim about clicking", async () => {
    // 查看當初選項 BEFORE 重新決定 is a statement about a decision already
    // taken. 「點一下就送出」 there would be false, and a live-looking tick box
    // would be an affordance that does nothing.
    const { container, getByText, queryByTestId } = renderAnswered(answered());
    fireEvent.click(getByText("查看當初選項"));

    expect(queryByTestId("reply-mode-hint")).toBeNull();
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
  it("says nothing about clicking on a card nobody can answer any more", async () => {
    const { queryByTestId, container } = render(
      <I18nProvider>
        <ReplyCardExpiredBody
          card={mkCard({ status: "expired", selectMode: "single" })}
        />
      </I18nProvider>,
    );
    expect(queryByTestId("reply-mode-hint")).toBeNull();
    expect(
      [...container.querySelectorAll(".reply-option")].every(
        (e) => (e as HTMLButtonElement).disabled,
      ),
    ).toBe(true);
  });
});
