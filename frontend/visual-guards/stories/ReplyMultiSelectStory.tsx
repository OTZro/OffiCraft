// CT story for T-40 — a MULTI-select reply card at a real card width.
//
// The waiting body grew two things this guard is about: a staged-selection
// state on the chips (a background + border, no geometry of its own) and a new
// 已選 N 項 line between the chips and the send row. Both live inside the
// card's flex column, so the question a real browser answers and jsdom cannot
// is whether the count line and the send row still sit INSIDE the card at a
// phone width — an option label is agent free text and can be long enough to
// push a row sideways.
//
// The chips carry a deliberately long label for that reason: a story with short
// options would leave the narrow case asserting nothing.
import { I18nProvider } from "../../src/i18n";
import { ReplyCardWaitingBody } from "../../src/components/ReplyCardBody";
import type { ReplyCard } from "../../src/api/adapter";

const MULTI_CARD: ReplyCard = {
  id: "rc-multi",
  from: "mira",
  kind: "decision",
  summary: "這批貨要走哪幾條線？可以複選。",
  body: "",
  options: [
    {
      text: "走海運：三十天到,但這一季的預算表上只剩這條線還沒被排滿,而且倉庫那邊已經先幫我們留了位子",
      aiPick: false,
    },
    { text: "走空運", aiPick: true },
    { text: "先擱著", aiPick: false },
  ],
  selectMode: "multi",
  status: "waiting",
  attachments: [],
  createdTs: Date.now() / 1000 - 600,
  answeredTs: null,
  chatMessageId: "msg-multi",
  answer: null,
  task: null,
};

// The SINGLE-select twin. T-40b gave the two kinds different chip marks (radio
// vs tick box) and a different hint line, and both of those are new rows/boxes
// that have to fit a phone card — the multi story alone would leave the single
// card's own geometry unmeasured.
const SINGLE_CARD: ReplyCard = {
  ...MULTI_CARD,
  id: "rc-single",
  summary: "這批貨要走哪一條線？只能選一條。",
  selectMode: "single",
  chatMessageId: "msg-single",
};

export function ReplyMultiSelectStory() {
  return (
    <I18nProvider>
      <div className="replies">
        <article className="reply-card" data-testid="card-multi">
          <ReplyCardWaitingBody card={MULTI_CARD} onAnswer={async () => {}} />
        </article>
        <article className="reply-card" data-testid="card-single">
          <ReplyCardWaitingBody card={SINGLE_CARD} onAnswer={async () => {}} />
        </article>
      </div>
    </I18nProvider>
  );
}
