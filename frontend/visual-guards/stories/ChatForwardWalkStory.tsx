// CT story (T-48): the REAL <ChatArea> + the REAL useChat, entered at an ANCHOR
// so the thread is a window from the middle of the history with `hasNewer` true.
//
// 🔴 WHY THIS ONE IS NOT JUST A JSDOM TEST. The rule being guarded is 「one
// gesture, one page」, and the mechanism that breaks it is invisible to jsdom:
// in a real browser `scrollIntoView` FIRES A SCROLL EVENT of its own, so an
// auto-follow on a landed forward page re-enters `onMessagesScroll` at
// `distance: 0` and asks for the next page with nobody having touched anything
// — the level-triggered corridor this ticket removed, running under a different
// name.
//
// The same story also carries the SECOND thing only a browser can show (review
// #20): a scroller already pinned at its limit emits no `scroll` event, so a
// reader whose gesture the retry throttle refused cannot simply「scroll again」
// — the refused gesture has to be replayed for them. jsdom's lengths all read
// 0, so a swallowed gesture is always repeatable there and that defect shipped.
//
// jsdom is NOT blind to the cause: with the follow's `if (!hasNewer)` deleted,
// `ChatArea.anchor-entry.test.tsx` goes 14 passed / 1 FAILED, because it asserts
// on which element was scrolled to and jsdom records the call. What jsdom cannot
// produce is the CONSEQUENCE — its `scrollIntoView` emits no event and every
// length reads 0, so the request count stays 1 there while Chromium walks the
// thread to the live tail. Both assertions are load-bearing; neither replaces
// the other.
//
// The api seam is patched in place (house pattern — see ChatJumpCardShiftStory):
// `api` IS `mockApi` under CT's default VITE_USE_MOCK.
import { I18nProvider } from "../../src/i18n";
import { ChatArea } from "../../src/components/ChatArea";
import { api } from "../../src/api";
import type { ChatMessage } from "../../src/api/adapter";
import type { Member } from "../../src/types";
import {
  OWNER,
  PEER,
  TARGET_ID,
  TOTAL,
  FORWARD_COUNT_KEY,
} from "./chatForwardWalkFixtures";
import "../../src/components/office.css";

const log: ChatMessage[] = [];
for (let i = 0; i < TOTAL; i += 1) {
  log.push({
    id: `a${i}`,
    from: PEER,
    to: OWNER,
    body: `第 ${i} 則訊息 —— 一句普通長度的聊天內容,好讓每一列的高度都是真的。`,
    ts: 100 + i,
    attachments: [],
    replyCardId: null,
  });
}

api.listChat = async (
  _withId: string,
  limit?: number,
  cursor?: { beforeTs: number; beforeId: string },
) => {
  const size = limit ?? 30;
  if (cursor) {
    return log
      .filter(
        (m) =>
          m.ts < cursor.beforeTs ||
          (m.ts === cursor.beforeTs && m.id < cursor.beforeId),
      )
      .slice(-size);
  }
  return log.slice(-size);
};
api.listChatWindow = async (
  _withId: string,
  anchor: { startId?: string; endId?: string },
  limit: number,
) => {
  if (anchor.startId) {
    const w = window as unknown as Record<string, number>;
    w[FORWARD_COUNT_KEY] = (w[FORWARD_COUNT_KEY] ?? 0) + 1;
  }
  const at = log.findIndex((m) => m.id === (anchor.endId ?? anchor.startId));
  if (at < 0) return [];
  return anchor.endId
    ? log.slice(Math.max(0, at - limit + 1), at + 1)
    : log.slice(at, at + limit);
};
api.listChatReads = async () => [];
api.markChatRead = async () => undefined as never;
api.subscribeEvents = () => () => {};

const peer: Member = {
  id: PEER,
  name: "Alice",
  role: "assistant",
  status: "online",
  lifecycle: "online",
  model: "opus",
  effort: "medium",
  kind: "staff",
  desiredMachineId: "",
  machine: null,
  account: null,
  contextPct: null,
  estimatedCost: null,
  bankedCost: null,
  tmuxSession: "",
  refocusSince: null,
  lastOp: "",
  lastOpOk: null,
  lastOpLog: "",
  lastOpAt: null,
  unreadCount: 0,
};

export function ChatForwardWalkStory() {
  return (
    <I18nProvider>
      {/* `.chat` is a height:100% flex column and the CT mount point has no
        * height of its own — without a bounded box the scroller is unbounded,
        * every row is on screen at once and there is no 「bottom」 to reach. */}
      <div
        style={{ height: 720, display: "flex", background: "var(--color-main-bg)" }}
      >
        <ChatArea
          key={peer.id}
          member={peer}
          members={[peer]}
          workers={[]}
          jumpToMsgId={TARGET_ID}
        />
      </div>
    </I18nProvider>
  );
}
