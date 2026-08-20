// CT story: the three surfaces T-4e95 adds, in their real production DOM shape
// so the loaded office.css — not a mock stylesheet — governs what the guard
// measures.
//
// It passes NO function props across the mount bridge and renders plain markup
// rather than <ChatArea>: the contracts this story exists for are GEOMETRIC and
// PRESENTATIONAL (hover reveal, one-line clipping, the quote row staying inside
// the bubble column), and jsdom can measure none of them. The behavioural half
// is already pinned in ChatArea.reply-to.test.tsx.
export function ChatReplyToStory() {
  const longQuote =
    "這是一段很長的原文，長到足以在任何視窗寬度下超出引用列能容納的寬度，用來確認它會被裁成一行而不是把版面撐開或折行";
  return (
    <div className="chat">
      <div className="chat__body">
        <div className="chat__messages" data-testid="chat-messages">
          <div className="chat__msg" data-msg-id="c-1" data-testid="row-incoming">
            <div className="chat__msg-meta">
              <span className="chat__msg-name">Mira</span>
            </div>
            <div className="chat__msg-line">
              <div className="chat__msg-content">
                <div className="chat__msg-bubble">
                  <div className="chat__msg-text doc-md">{longQuote}</div>
                </div>
              </div>
              <div className="chat__msg-sidemeta">
                <span className="chat__msg-time">10:02</span>
              </div>
              <button
                type="button"
                className="chat__msg-reply"
                aria-label="回覆這則"
                data-testid="reply-entry-incoming"
              >
                <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor">
                  <polyline points="9 17 4 12 9 7" />
                  <path d="M20 18v-2a4 4 0 0 0-4-4H4" />
                </svg>
              </button>
            </div>
          </div>

          <div className="chat__msg chat__msg--me" data-msg-id="c-2" data-testid="row-mine">
            <div className="chat__msg-line">
              <div className="chat__msg-sidemeta">
                <span className="chat__msg-time">10:03</span>
              </div>
              <div className="chat__msg-content">
                <button
                  type="button"
                  className="chat__msg-quote chat__msg-quote--locatable"
                  data-testid="quote-row"
                >
                  <svg
                    className="chat__msg-quote__icon"
                    width="11"
                    height="11"
                    viewBox="0 0 24 24"
                    fill="none"
                    stroke="currentColor"
                  >
                    <polyline points="9 17 4 12 9 7" />
                  </svg>
                  <span className="chat__msg-quote__who">Mira</span>
                  <span className="chat__msg-quote__body">{longQuote}</span>
                </button>
                <div className="chat__msg-bubble">
                  <div className="chat__msg-text doc-md">好，我照這個做</div>
                </div>
              </div>
              <button
                type="button"
                className="chat__msg-reply"
                aria-label="回覆這則"
                data-testid="reply-entry-mine"
              >
                <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor">
                  <polyline points="9 17 4 12 9 7" />
                  <path d="M20 18v-2a4 4 0 0 0-4-4H4" />
                </svg>
              </button>
            </div>
          </div>
        </div>
      </div>

      <footer className="chat__composer">
        <div className="chat__reply-banner" data-testid="chat-reply-banner">
          <svg
            className="chat__reply-banner__icon"
            width="13"
            height="13"
            viewBox="0 0 24 24"
            fill="none"
            stroke="currentColor"
          >
            <polyline points="9 17 4 12 9 7" />
          </svg>
          <span className="chat__reply-banner__text">
            <span className="chat__reply-banner__who">正在回覆 Mira</span>
            <span className="chat__reply-banner__body">{longQuote}</span>
          </span>
          <button
            type="button"
            className="chat__reply-banner__x"
            aria-label="取消回覆"
            data-testid="reply-banner-x"
          >
            <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor">
              <line x1="18" y1="6" x2="6" y2="18" />
              <line x1="6" y1="6" x2="18" y2="18" />
            </svg>
          </button>
        </div>
        <div className="chat__composer-row">
          <textarea className="chat__input" rows={1} defaultValue="" />
          <button type="button" className="chat__send" aria-label="送出" />
        </div>
      </footer>
    </div>
  );
}
