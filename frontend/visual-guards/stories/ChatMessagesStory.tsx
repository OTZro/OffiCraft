// CT story: the chat message list (`.chat__body > .chat__messages`) in its real
// production DOM shape, carrying a message whose body is an UNBREAKABLE wide
// code block (the exact content class that first exposed a horizontal overflow —
// see office.css T-84c8). The story passes NO function props across the mount
// bridge; it renders the real class names so the loaded office.css — not a mock
// — governs the layout the guard measures.
//
// The guard (chat-no-hscroll.ct.spec.tsx, T-55ad) asserts that `.chat__messages`
// is NOT a horizontal pan surface: a phone user must only scroll up/down, while
// the wide code block keeps its OWN in-block horizontal scroll (the <pre>).
export function ChatMessagesStory() {
  const longCodeLine =
    "const veryLongUnbreakableLine = someFunction(argumentOne, argumentTwo, argumentThree, argumentFour, argumentFive) + anotherCallThatKeepsGoingWithoutAnyWhitespaceToBreakOn;";
  return (
    <div className="chat__body">
      <div className="chat__messages" data-testid="chat-messages">
        <div className="chat__msg">
          <div className="chat__msg-bubble">
            <div className="chat__msg-text doc-md">
              <pre data-testid="chat-code">
                <code>{longCodeLine}</code>
              </pre>
            </div>
          </div>
        </div>
        <div className="chat__msg" data-testid="chat-image-row">
          <div className="chat__msg-bubble" data-testid="chat-image-bubble">
            <div className="chat__msg-attachments">
              <span className="chat__msg-attachment">
                <img
                  className="chat__msg-image"
                  data-testid="chat-image"
                  alt="wide attachment"
                  width={1200}
                  height={600}
                  src="data:image/svg+xml,%3Csvg xmlns='http://www.w3.org/2000/svg' width='1200' height='600'%3E%3C/svg%3E"
                />
              </span>
            </div>
          </div>
        </div>
      </div>
    </div>
  );
}
