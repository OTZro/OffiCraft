// CT story for T-ee17 — the reply card's 任務資訊 row at a REAL card width.
//
// Two rows, because one of them is the control: an over-long task title (the
// shape that made this guard necessary) and a short one. A guard that only
// mounts the long title cannot tell "the title is clipped" from "every title
// is clipped", and the second reading is a different, worse bug.
//
// The rows sit inside `.reply-card` on purpose: the row is a flex child of the
// card, so its available width — the whole thing this guard measures — comes
// from the card's own box and padding. Measuring the row on a bare page would
// hand it the full viewport and the overflow would simply not happen.
import { I18nProvider } from "../../src/i18n";
import { ReplyCardTaskRef } from "../../src/components/ReplyCardBody";

// Long enough to overflow at DESKTOP width too, not just on a phone. A title
// that only bursts the row at 390px would leave the 1040px case asserting
// nothing — and real ticket titles do run this long (this one is a real one,
// with its own tail restored).
const LONG_TITLE =
  "[ACE-7580] SOC2 年度風險評估：review Google Drive 上的 ISMS 文件 + 產出風險評鑑清冊與處理計畫" +
  "，並對照去年度的缺失追蹤表逐項確認關閉狀態、補齊佐證，最後彙整成給稽核方的單一交付包";
const SHORT_TITLE = "補一把字數尺";

export function ReplyTaskRefStory() {
  return (
    <I18nProvider>
      <div className="replies">
        <article className="reply-card" data-testid="card-long">
          <ReplyCardTaskRef
            task={{ id: "t-long", typeKey: "review-pr", title: LONG_TITLE }}
            onJump={() => {}}
          />
        </article>
        <article className="reply-card" data-testid="card-short">
          <ReplyCardTaskRef
            task={{ id: "t-short", typeKey: "", title: SHORT_TITLE }}
            onJump={() => {}}
          />
        </article>
      </div>
    </I18nProvider>
  );
}
