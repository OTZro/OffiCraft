// CT story for T-48 的兩個底部元件（回到最新箭頭 ①、新訊息預覽列 ②），用的是
// production 的 DOM 形狀與 production 的 office.css —— 這裡要量的東西 jsdom 一
// 個都量不到：絕對定位落在哪、圓不圓、兩行有沒有被裁、換內容時高度會不會跳、
// 預覽列排在回覆橫幅上面還是下面、換 theme 之後顏色有沒有跟著換。
//
// 🔴 元件是 REAL 的（<ChatJumpLatestButton> / <ChatNewMsgPreview>），只有外殼是
// 手寫的：外殼要照 <ChatArea> 的實際巢狀（.chat > .chat__body > .chat__messages，
// 箭頭是 .chat__body 的絕對定位子元素；預覽列是 .chat__composer 的第一個子元素，
// 回覆橫幅緊接在後）。外殼一旦寫錯，量到的是一個不會出貨的版面。
import { ChatJumpLatestButton } from "../../src/components/ChatJumpLatestButton";
import { ChatNewMsgPreview } from "../../src/components/ChatNewMsgPreview";
import { I18nProvider, useI18n } from "../../src/i18n";
import { LONG_BODY, LONG_WHO } from "./chatBottomAffordanceFixtures";

function Shell({ children }: { children: React.ReactNode }) {
  // .chat 是 height:100% 的 flex column，CT 的掛載容器沒有高度，所以外面補一個。
  //
  // 🔴 兩層底色不是裝飾，是對比度量測的前提。預覽列的底是半透明的
  // （`color-mix(--color-card 60%, transparent)`），箭頭浮在訊息面板上，兩者最後
  // 被畫成什麼顏色取決於**下面疊了什麼**。CT 的掛載點沒有任何底色，量到的會是
  // 瀏覽器的白畫布 —— 一個永遠不會出貨的組合。app shell 實際疊的是
  // `--color-main-bg` over `--color-bg`，這裡照抄那兩層。
  return (
    <I18nProvider>
      <div style={{ height: 600, background: "var(--color-bg)" }}>
        <div
          style={{
            height: "100%",
            display: "flex",
            background: "var(--color-main-bg)",
          }}
        >
          {children}
        </div>
      </div>
    </I18nProvider>
  );
}

/**
 * 完整外殼：訊息串 + 底部兩個元件 + 回覆橫幅，用 props 選要畫哪些。
 * 兩個元件同時 `true` 只有測試會這樣要 —— production 的互斥由
 * `lib/chatBottomAffordance` 保證（jsdom 已釘），這裡是為了在同一次量測裡
 * 比較兩者的位置關係時能一起看到。
 */
export function ChatBottomAffordanceStory({
  arrow = true,
  preview = false,
  banner = false,
  who = LONG_WHO,
  body = LONG_BODY,
}: {
  arrow?: boolean;
  preview?: boolean;
  banner?: boolean;
  who?: string;
  body?: string;
} = {}) {
  return (
    <Shell>
      <div className="chat" style={{ flex: 1, minWidth: 0 }}>
        <div className="chat__body">
          <div className="chat__messages" data-testid="chat-messages">
            {Array.from({ length: 12 }, (_, i) => (
              <div className="chat__msg" data-msg-id={`c-${i}`} key={i}>
                <div className="chat__msg-line">
                  <div className="chat__msg-content">
                    <div className="chat__msg-bubble">
                      <div className="chat__msg-text doc-md">訊息 {i}</div>
                    </div>
                  </div>
                </div>
              </div>
            ))}
          </div>
          {arrow && <ChatJumpLatestButton onClick={() => {}} />}
        </div>
        <footer className="chat__composer">
          {preview && (
            <ChatNewMsgPreview
              who={who}
              body={body}
              onJump={() => {}}
              onDismiss={() => {}}
            />
          )}
          {banner && (
            <div className="chat__reply-banner" data-testid="chat-reply-banner">
              <span className="chat__reply-banner__text">
                <span className="chat__reply-banner__who">正在回覆 Mira → 韓立</span>
                <span className="chat__reply-banner__body">{body}</span>
              </span>
              <button
                type="button"
                className="chat__reply-banner__x"
                aria-label="取消回覆"
                data-testid="reply-banner-x"
              >
                ×
              </button>
            </div>
          )}
          <div className="chat__composer-row" data-testid="composer-row">
            <textarea className="chat__input" rows={1} defaultValue="" />
          </div>
        </footer>
      </div>
    </Shell>
  );
}

/**
 * 三條預覽列，內容長短天差地遠，放在同一個 composer 裡（同寬度，高度才可比）。
 *
 * 🔴 這是「更新內容不堆疊」的另一半：不堆疊代表同一條列會被反覆換掉內容，於是
 * 「高度不隨內容變」不再只是好看的問題 —— 每來一則訊息輸入框就會在打字的人手底下
 * 上下跳一次。空 body 那條是真的會發生的（純附件訊息）。
 */
export function NewMsgPreviewHeightStory() {
  const cases: Array<[string, string, string]> = [
    ["short", "Mira", "好"],
    ["long", LONG_WHO, LONG_BODY],
    ["empty", "Mira", ""],
  ];
  return (
    <Shell>
      <div className="chat" style={{ flex: 1, minWidth: 0 }}>
        <div className="chat__body">
          <div className="chat__messages" />
        </div>
        <footer className="chat__composer">
          {cases.map(([id, who, body]) => (
            <div key={id} data-testid={`preview-case-${id}`}>
              <ChatNewMsgPreview
                who={who}
                body={body}
                onJump={() => {}}
                onDismiss={() => {}}
              />
            </div>
          ))}
          <div className="chat__composer-row">
            <textarea className="chat__input" rows={1} defaultValue="" />
          </div>
        </footer>
      </div>
    </Shell>
  );
}

/**
 * ③ 跳轉提示列（`.chat__jump-miss`），兩句話各畫一條，放在同一個 composer 裡。
 *
 * 🔴 為什麼要量它：這條列是**寫在 composer 上面**的，而它的兩句話長度差很多
 * （英文那句更長）。折行本身不是罪，但它會把輸入框往下推 —— 而且這條列的出現
 * 時機是「跳轉剛落空」，正是使用者準備打字的那一刻。jsdom 量不到任何一格：
 * 沒有版面、沒有 @media、offsetHeight 永遠 0。
 *
 * DOM 形狀照抄 ChatArea 的實際輸出（composer 的第一個子元素，一個 span + 一顆
 * ×），文字由 I18nProvider 的真字串提供，不是手打的假字。
 */
export function ChatJumpNoticeStory({
  text,
}: {
  /** 真正的出貨文案，由 spec 從 locale 模組直接餵進來 —— 兩個語系的長度差很多，
   * 而**最長的那一句才是會現形的那一句**。手打的假字或只量預設語系，等於量一個
   * 一定塞得下的 fixture（LONG_BODY 那格已經踩過一次）。 */
  text: string;
}) {
  return (
    <Shell>
      <div className="chat" style={{ flex: 1, minWidth: 0 }}>
        <div className="chat__body">
          <div className="chat__messages" />
        </div>
        <footer className="chat__composer">
          <JumpMissRow text={text} />
          <div className="chat__composer-row" data-testid="composer-row">
            <textarea className="chat__input" rows={1} defaultValue="" />
          </div>
        </footer>
      </div>
    </Shell>
  );
}

function JumpMissRow({ text }: { text: string }) {
  const { t } = useI18n();
  return (
    <div className="chat__jump-miss" role="status" data-testid="jump-miss">
      <span data-testid="jump-miss-text">{text}</span>
      <button
        type="button"
        className="chat__jump-miss__x"
        aria-label={t.chat.jumpTargetMissingDismiss}
        title={t.chat.jumpTargetMissingDismiss}
        data-testid="jump-miss-x"
      >
        ×
      </button>
    </div>
  );
}
