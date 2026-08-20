// CT story: the three surfaces T-4e95 adds, in their real production DOM shape
// so the loaded office.css — not a mock stylesheet — governs what the guard
// measures.
//
// It passes NO function props across the mount bridge and renders plain markup
// rather than <ChatArea>: the contracts this story exists for are GEOMETRIC and
// PRESENTATIONAL (hover reveal, one-line clipping, the quote row staying inside
// the bubble column), and jsdom can measure none of them. The behavioural half
// is already pinned in ChatArea.reply-to.test.tsx.
/** `jumpLabel` exists because the label's WIDTH is the thing under test and it
 * is not the same in every language: zh is 「跳到原訊息」 at 69px, en is "Go to
 * the original message" at 154px, and the guard that matters (the jump must not
 * end up under the corner buttons) only fails on the long one. A story hard-
 * wired to Chinese measured a control 85px narrower than half the users see. */
export function ChatReplyToStory({
  jumpLabel = "跳到原訊息",
}: {
  jumpLabel?: string;
} = {}) {
  // 🔴 A LONG SENDER, not "Mira". What makes a long one real is that display
  // names are OWNER-SET FREE TEXT — there is no cap on what he types. (An
  // earlier version of this comment also claimed nameOf()'s raw-id fallback was
  // a reason; it is not. A raw id is 15 characters and the guard was measured
  // not to fire until 33 — so the fallback would sail straight past it.)
  // The story used a four-character name and therefore walked past the overflow
  // it exists to guard — measured: with the guard reverted, the story still
  // passed. This length is the one that actually reproduces the failure (the
  // jump reaching the corner controls) at 390px.
  //
  // It is NOT the only row that matters: a short name is the everyday case and
  // it broke in the opposite direction, so row 3 below pins that half.
  const unresolvedSender = "Eva Rhapsody Inbox (ow-8808ccf51794)";
  const longQuote =
    "這是一段很長的原文，長到足以在任何視窗寬度下超出引用列能容納的寬度，用來確認它會被裁成一行而不是把版面撐開或折行";
  // A SEPARATE, much longer quote for the short-name row. The row below is the
  // only place a positive control must hold at BOTH widths: at 1280 the bubble
  // is wide enough that the sentence above still fits, so a cut never happens
  // and the guard would assert nothing. Measured, not guessed — the first
  // version of that test passed at 390 and failed its own control at 1280.
  const veryLongQuote = longQuote.repeat(4);
  // What nameOf() renders when the roster cannot resolve the sender: 15
  // characters. Too short to trip the corner-collision guard (measured: that
  // one needs 33), and exactly the length a percentage cap silently trimmed.
  const rawIdSender = "ow-8808ccf51794";
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
                <div className="chat__msg-bubble chat__msg-bubble--expandable chat__msg-bubble--acts2">
                  <div className="chat__msg-actions">
                    <button
                      type="button"
                      className="chat__msg-reply"
                      aria-label="回覆這則"
                      data-testid="reply-entry-incoming"
                    >
                      <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
                        <polyline points="9 17 4 12 9 7" />
                        <path d="M20 18v-2a4 4 0 0 0-4-4H4" />
                      </svg>
                    </button>
                    <button
                      type="button"
                      className="chat__msg-expand"
                      aria-label="放大閱讀"
                      data-testid="expand-entry-incoming"
                    >
                      <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
                        <polyline points="14 4 20 4 20 10" />
                      </svg>
                    </button>
                  </div>
                  <div className="chat__msg-text doc-md">{longQuote}</div>
                </div>
              </div>
              <div className="chat__msg-sidemeta">
                <span className="chat__msg-time">10:02</span>
              </div>
            </div>
          </div>

          <div className="chat__msg chat__msg--me" data-msg-id="c-2" data-testid="row-mine">
            <div className="chat__msg-line">
              <div className="chat__msg-sidemeta">
                <span className="chat__msg-time">10:03</span>
              </div>
              <div className="chat__msg-content">
                <div className="chat__msg-bubble chat__msg-bubble--acts1">
                  <div className="chat__msg-actions">
                    <button
                      type="button"
                      className="chat__msg-reply"
                      aria-label="回覆這則"
                      data-testid="reply-entry-mine"
                    >
                      <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
                        <polyline points="9 17 4 12 9 7" />
                        <path d="M20 18v-2a4 4 0 0 0-4-4H4" />
                      </svg>
                    </button>
                  </div>
                  <div className="chat__msg-quote" data-testid="quote-row">
                    <svg
                      className="chat__msg-quote__icon"
                      width="11"
                      height="11"
                      viewBox="0 0 24 24"
                      fill="none"
                      stroke="currentColor"
                      strokeWidth="2"
                      strokeLinecap="round"
                      strokeLinejoin="round"
                    >
                      <polyline points="9 17 4 12 9 7" />
                    </svg>
                    <span className="chat__msg-quote__who">{unresolvedSender}</span>
                    <span className="chat__msg-quote__body">{longQuote}</span>
                    <button
                      type="button"
                      className="chat__msg-quote__jump"
                      data-testid="quote-jump"
                    >
                      <span className="chat__msg-quote__jump-label">{jumpLabel}</span>
                      <svg
                        className="chat__msg-quote__jump-chevron"
                        width="12"
                        height="12"
                        viewBox="0 0 24 24"
                        fill="none"
                        stroke="currentColor"
                        strokeWidth="2"
                        strokeLinecap="round"
                        strokeLinejoin="round"
                      >
                        <path d="m9 18 6-6-6-6" />
                      </svg>
                    </button>
                  </div>
                  <div className="chat__msg-text doc-md">好，我照這個做</div>
                </div>
              </div>
            </div>
          </div>

          {/* 🔴 THE OTHER HALF of the same contract, and the one the long row
            * cannot express: a SHORT, resolved sender name. Both shrinkable
            * items on one row shrink in PROPORTION, so a row tight enough to
            * clip the quoted text was also clipping a four-character name down
            * to a single letter — measured in the running app: 「Mira」 became
            * 「M…」 at 1180px, where there was room to spare. The name is the
            * half that says WHO is being answered; losing it defeats the
            * feature. It is the quoted TEXT that must give way first. */}
          <div className="chat__msg chat__msg--me" data-msg-id="c-3" data-testid="row-mine-short">
            <div className="chat__msg-line">
              <div className="chat__msg-sidemeta">
                <span className="chat__msg-time">10:04</span>
              </div>
              <div className="chat__msg-content">
                <div className="chat__msg-bubble chat__msg-bubble--acts1">
                  <div className="chat__msg-actions">
                    <button
                      type="button"
                      className="chat__msg-reply"
                      aria-label="回覆這則"
                      data-testid="reply-entry-mine-short"
                    >
                      <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
                        <polyline points="9 17 4 12 9 7" />
                        <path d="M20 18v-2a4 4 0 0 0-4-4H4" />
                      </svg>
                    </button>
                  </div>
                  <div className="chat__msg-quote" data-testid="quote-row-short">
                    <svg
                      className="chat__msg-quote__icon"
                      width="11"
                      height="11"
                      viewBox="0 0 24 24"
                      fill="none"
                      stroke="currentColor"
                      strokeWidth="2"
                      strokeLinecap="round"
                      strokeLinejoin="round"
                    >
                      <polyline points="9 17 4 12 9 7" />
                    </svg>
                    <span className="chat__msg-quote__who" data-testid="quote-who-short">
                      Mira
                    </span>
                    <span className="chat__msg-quote__body" data-testid="quote-body-short">
                      {veryLongQuote}
                    </span>
                    <button
                      type="button"
                      className="chat__msg-quote__jump"
                      data-testid="quote-jump-short"
                    >
                      <span className="chat__msg-quote__jump-label">{jumpLabel}</span>
                      <svg
                        className="chat__msg-quote__jump-chevron"
                        width="12"
                        height="12"
                        viewBox="0 0 24 24"
                        fill="none"
                        stroke="currentColor"
                        strokeWidth="2"
                        strokeLinecap="round"
                        strokeLinejoin="round"
                      >
                        <path d="m9 18 6-6-6-6" />
                      </svg>
                    </button>
                  </div>
                  <div className="chat__msg-text doc-md">好，我照這個做</div>
                </div>
              </div>
            </div>
          </div>

          {/* 🔴 THE THIRD DIRECTION, and the one two previous fixes both got
            * wrong: a MIDDLING name with an excerpt too short to absorb
            * anything. Nothing here needs to be cut — the row has room to
            * spare — so any ellipsis on the name is the layout inventing a
            * shortage. A `max-width: 40%` cap did exactly that: the bubble is
            * shrink-to-fit and a percentage max-width is ignored while its
            * intrinsic width is computed, so the bubble was sized to the whole
            * name and the cap clipped it afterwards, at every width including
            * 1600px. This name is a raw member id — what nameOf() renders when
            * the roster cannot resolve the sender. */}
          <div className="chat__msg chat__msg--me" data-msg-id="c-4" data-testid="row-mine-tight">
            <div className="chat__msg-line">
              <div className="chat__msg-sidemeta">
                <span className="chat__msg-time">10:05</span>
              </div>
              <div className="chat__msg-content">
                <div className="chat__msg-bubble chat__msg-bubble--acts1">
                  <div className="chat__msg-actions">
                    <button
                      type="button"
                      className="chat__msg-reply"
                      aria-label="回覆這則"
                      data-testid="reply-entry-mine-tight"
                    >
                      <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
                        <polyline points="9 17 4 12 9 7" />
                        <path d="M20 18v-2a4 4 0 0 0-4-4H4" />
                      </svg>
                    </button>
                  </div>
                  <div className="chat__msg-quote" data-testid="quote-row-tight">
                    <svg
                      className="chat__msg-quote__icon"
                      width="11"
                      height="11"
                      viewBox="0 0 24 24"
                      fill="none"
                      stroke="currentColor"
                      strokeWidth="2"
                      strokeLinecap="round"
                      strokeLinejoin="round"
                    >
                      <polyline points="9 17 4 12 9 7" />
                    </svg>
                    <span className="chat__msg-quote__who" data-testid="quote-who-tight">
                      {rawIdSender}
                    </span>
                    <span className="chat__msg-quote__body" data-testid="quote-body-tight">
                      好
                    </span>
                    <button
                      type="button"
                      className="chat__msg-quote__jump"
                      data-testid="quote-jump-tight"
                    >
                      <span className="chat__msg-quote__jump-label">{jumpLabel}</span>
                      <svg
                        className="chat__msg-quote__jump-chevron"
                        width="12"
                        height="12"
                        viewBox="0 0 24 24"
                        fill="none"
                        stroke="currentColor"
                        strokeWidth="2"
                        strokeLinecap="round"
                        strokeLinejoin="round"
                      >
                        <path d="m9 18 6-6-6-6" />
                      </svg>
                    </button>
                  </div>
                  <div className="chat__msg-text doc-md">好</div>
                </div>
              </div>
            </div>
          </div>

          {/* 🔴 THE COMMONEST SHAPE OF ALL, and the one this story did not have:
            * an INCOMING message that is itself a reply — an agent answering
            * something you said. Every other quote row here sits on one of your
            * own bubbles, which reserve 32px for a single corner button; an
            * incoming one with a body reserves 56px for two (`--acts2`, see
            * ChatArea's `!mine && m.body`). That extra 24px is exactly the
            * margin a jump control eats when it cannot shrink, so the widest
            * failure lived in the one arrangement nothing rendered. */}
          <div className="chat__msg" data-msg-id="c-5" data-testid="row-incoming-quote">
            <div className="chat__msg-meta">
              <span className="chat__msg-name">Mira</span>
            </div>
            <div className="chat__msg-line">
              <div className="chat__msg-content">
                <div className="chat__msg-bubble chat__msg-bubble--expandable chat__msg-bubble--acts2">
                  <div className="chat__msg-actions">
                    <button
                      type="button"
                      className="chat__msg-reply"
                      aria-label="回覆這則"
                      data-testid="reply-entry-incoming-quote"
                    >
                      <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
                        <polyline points="9 17 4 12 9 7" />
                        <path d="M20 18v-2a4 4 0 0 0-4-4H4" />
                      </svg>
                    </button>
                    <button
                      type="button"
                      className="chat__msg-expand"
                      aria-label="放大閱讀"
                      data-testid="expand-entry-incoming-quote"
                    >
                      <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
                        <polyline points="14 4 20 4 20 10" />
                      </svg>
                    </button>
                  </div>
                  <div className="chat__msg-quote" data-testid="quote-row-incoming">
                    <svg
                      className="chat__msg-quote__icon"
                      width="11"
                      height="11"
                      viewBox="0 0 24 24"
                      fill="none"
                      stroke="currentColor"
                      strokeWidth="2"
                      strokeLinecap="round"
                      strokeLinejoin="round"
                    >
                      <polyline points="9 17 4 12 9 7" />
                    </svg>
                    <span className="chat__msg-quote__who" data-testid="quote-who-incoming">
                      CEO
                    </span>
                    <span className="chat__msg-quote__body" data-testid="quote-body-incoming">
                      好
                    </span>
                    <button
                      type="button"
                      className="chat__msg-quote__jump"
                      data-testid="quote-jump-incoming"
                    >
                      <span className="chat__msg-quote__jump-label">{jumpLabel}</span>
                      <svg
                        className="chat__msg-quote__jump-chevron"
                        width="12"
                        height="12"
                        viewBox="0 0 24 24"
                        fill="none"
                        stroke="currentColor"
                        strokeWidth="2"
                        strokeLinecap="round"
                        strokeLinejoin="round"
                      >
                        <path d="m9 18 6-6-6-6" />
                      </svg>
                    </button>
                  </div>
                  <div className="chat__msg-text doc-md">好，我照這個做</div>
                </div>
              </div>
              <div className="chat__msg-sidemeta">
                <span className="chat__msg-time">10:06</span>
              </div>
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
            <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
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
