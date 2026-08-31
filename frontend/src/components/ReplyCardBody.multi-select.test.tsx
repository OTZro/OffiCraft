// T-40: the reply card's options became a SET, and the AI recommendation moved
// off position onto each option's own ai_pick. These are the three claims the
// cockpit half of that change rests on, and nothing else in the tree holds
// them: the DTO-parity gate does not cover ReplyCard, the style-ownership gate
// does not own replies.css, and the payload-parity roll-call does not list a
// card's inner fields.

import { describe, it, expect, vi, afterEach } from "vitest";
import { render, fireEvent } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import {
  ReplyCardAnsweredBody,
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
  const send = () => u.container.querySelector(".chat__send") as HTMLButtonElement;
  return { ...u, sent, chips, send };
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
    // refuses to send.
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

  it("replaces the tick on a single card and adds to it on a multi card", async () => {
    const single = renderWaiting(mkCard({ selectMode: "single" }));
    fireEvent.click(single.chips()[0]);
    fireEvent.click(single.chips()[2]);
    fireEvent.click(single.send());
    expect(single.sent).toEqual([
      { optionIdxs: [2], text: "", attachments: [] },
    ]);
    single.unmount();

    const multi = renderWaiting(mkCard());
    fireEvent.click(multi.chips()[0]);
    fireEvent.click(multi.chips()[2]);
    fireEvent.click(multi.send());
    expect(multi.sent).toEqual([
      { optionIdxs: [0, 2], text: "", attachments: [] },
    ]);
  });

  it("tags the option that carries ai_pick, and counts the ticks on a multi card", async () => {
    const { chips, getByTestId } = renderWaiting(mkCard());

    // Every chip WHOLE: number, wording, and exactly the tags it earned. The
    // AI tag rides the SECOND option — that is where ai_pick is — and a reader
    // that still tags index 0 disagrees with both of the first two strings.
    expect(chips().map((e) => e.textContent)).toEqual([
      "1走海運",
      "2走空運AI 建議",
      "3先擱著",
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
    expect(chips()[1].textContent).toBe("2走空運AI 建議");
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
      "1走海運目前",
      "2走空運AI 建議",
      "3先擱著目前",
    ]);

    fireEvent.click(chips[1]);
    fireEvent.click(container.querySelector(".chat__send")!);
    expect(sent).toEqual([
      { optionIdxs: [0, 1, 2], text: "", attachments: [] },
    ]);
  });
});
