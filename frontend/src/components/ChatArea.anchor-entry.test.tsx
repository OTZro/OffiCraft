// T-48 · 進房錨點優先的那條「接線」—— 以及切換對話時它不准鎖住新的那一間。
//
// 為什麼要有這一個檔案(第三輪獨立審查 R3-2):整個「進房就是進到那一則」的功能
// 靠 ChatArea 的一個參數 —— `useChat(member.id, jumpToMsgId)`。把第二個參數拿掉
// (型別合法,因為它是 optional)之後,**2600 支測試 ＋ tsc 全綠**:
//   · `useChat.scrollback.test.ts` 直接呼叫 hook,自己傳 anchor —— 測得到 hook,
//     測不到 ChatArea 有沒有傳;
//   · `ChatArea.unread-jump.test.tsx` 把 `useChat` 整個 mock 掉 —— 那裡根本沒有
//     真的 hook;
//   · e2e 只斷言終態(目標 attached / inViewport),而拿掉錨點優先之後 `loadAround`
//     照樣會把窗換上來 —— 終態一模一樣。
// 中間那條線沒有人接。所以這裡用**真的 ChatArea ＋ 真的 useChat**,只把 api seam
// 換掉,直接量「這間房發出去的第一個請求是什麼」。
//
// 第二件(R3-1)只有把兩者接在一起才量得到:切換對話是 ChatArea 換 `member` prop,
// 而被上一條對話的錨點鎖住的是 useChat 的閂。

import { StrictMode } from "react";
import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { render, act, waitFor, fireEvent } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { ChatArea } from "./ChatArea";
import type { Member } from "../types";
import type { ChatMessage, ReplyCard } from "../api/adapter";

const OWNER = "owner";
const A = "m-aaaaaaaaaaaa";
const B = "m-bbbbbbbbbbbb";

/** Every `GET /api/chat` this room makes, in order, split by which kind it is:
 * a PLAIN newest page (`?with=` and nothing else — the request anchor-first
 * entry exists to not make) or an anchor WINDOW (`?end_id=` / `?start_id=`). */
let plainCalls: string[] = [];
let windowCalls: { withId: string; anchor: { startId?: string; endId?: string } }[] =
  [];

const log: ChatMessage[] = [];
/** Holds the anchor-window pair in flight, so a conversation switch can happen
 * while A's jump is still in the air — the whole of R3-1. */
let holdWindows: null | (() => void) = null;
/** Freezes the forward walk PART-WAY: every window call from the Nth onward
 * parks on `holdWindows` instead of answering. Without it the walk runs from
 * the anchor to the live tail inside one `waitFor` poll, and a test that needs
 * to say something about the reader's viewport MID-corridor has no moment to
 * say it in. */
let holdWindowsFrom = Infinity;
/** Makes the held pair end in a REJECTION rather than a page, i.e. the
 * `"unreachable"` ending of `loadAround` (a 502, a dropped connection). */
let windowsFail = false;
/** 讓「往新」那一頁回一整頁**已經握在手上的列**(滿頁 ⇒ `hasNewer` 仍為 true,
 * 但一列都沒有新的)。這就是重複錨點請求真的會拿回來的東西,也是自動連鎖唯一
 * 可能空轉的形狀。 */
let windowStale = false;
/** Same, for the plain newest page — so 回到最新's own fetch can be left in the
 * air across a conversation switch. */
let holdPlain: null | (() => void) = null;
/** Every `scrollIntoView` this room performs, tagged by WHAT was scrolled and
 * with which option — `block: "end"` is `scrollToLatest`'s signature and
 * nothing else in ChatArea uses it. */
let scrolls: { on: string; block: unknown }[] = [];
/** Every `getReplyCard`, with the one thing that matters: was the row carrying
 * that card ALREADY PAINTED when the read went out? (See the afterEach.) */
let cardReads: { id: string; rowPainted: number }[] = [];
/** The waiting cards a test deliberately put in the thread — the afterEach's
 * denominator. */
let seededWaitingCards: string[] = [];

const CARD: ReplyCard = {
  id: "rc-1",
  from: A,
  kind: "decision",
  summary: "要寄出嗎?",
  body: "",
  options: [
    { text: "寄出", aiPick: true },
    { text: "先不要", aiPick: false },
  ],
  selectMode: "single",
  status: "waiting",
  createdTs: 1,
  attachments: [],
  answeredTs: null,
  expiredTs: null,
  chatMessageId: "",
  answer: null,
  task: null,
};

/** One WAITING 請示卡 row, spliced into `peer`'s stream at `ts`. */
function seedWaitingCard(peer: string, id: string, cardId: string, ts: number) {
  log.push({
    id,
    from: peer,
    to: OWNER,
    body: "要寄出嗎?",
    ts,
    attachments: [],
    replyCardId: cardId,
    replyCardStatus: "waiting",
  });
  seededWaitingCards.push(cardId);
}

function threadOf(peer: string): ChatMessage[] {
  return log.filter((m) => m.from === peer || m.to === peer);
}

vi.mock("../api", () => ({
  api: {
    listChat: async (
      withId: string,
      limit?: number,
      cursor?: { beforeTs: number; beforeId: string },
    ) => {
      if (!cursor) plainCalls.push(withId);
      if (!cursor && holdPlain) {
        await new Promise<void>((r) => {
          const prev = holdPlain;
          holdPlain = () => {
            prev?.();
            r();
          };
        });
      }
      const all = threadOf(withId);
      const size = limit ?? 30;
      if (cursor) {
        return all
          .filter(
            (m) =>
              m.ts < cursor.beforeTs ||
              (m.ts === cursor.beforeTs && m.id < cursor.beforeId),
          )
          .slice(-size);
      }
      return all.slice(-size);
    },
    listChatWindow: async (
      withId: string,
      anchor: { startId?: string; endId?: string },
      limit: number,
    ) => {
      windowCalls.push({ withId, anchor });
      if (windowCalls.length >= holdWindowsFrom) {
        await new Promise<void>((r) => {
          const prev = holdWindows;
          holdWindows = () => {
            prev?.();
            r();
          };
        });
      }
      if (holdWindows) {
        await new Promise<void>((r) => {
          const prev = holdWindows;
          holdWindows = () => {
            prev?.();
            r();
          };
        });
      }
      if (windowsFail) throw new Error("listChatWindow: 502");
      const all = threadOf(withId);
      const at = all.findIndex(
        (m) => m.id === (anchor.endId ?? anchor.startId),
      );
      if (at < 0) return [];
      if (windowStale && anchor.startId) {
        return all.slice(Math.max(0, at - limit + 1), at + 1);
      }
      // Inclusive both ways, mirroring the server: `end_id` is the context
      // ABOVE the anchor, `start_id` the context BELOW.
      return anchor.endId
        ? all.slice(Math.max(0, at - limit + 1), at + 1)
        : all.slice(at, at + limit);
    },
    // 🔴 THE WHOLE OF GUARD B (T-48). Every read of a card records whether that
    // card's ROW WAS ALREADY IN THE DOM when the read went out. A row in the DOM
    // means the thread carrying it has already been COMMITTED — so a read taken
    // at that moment is one the reader is watching happen, and its answer arrives
    // as a card that GROWS under everything below it. `useChat` may not commit a
    // thread carrying a WAITING card until that card is in hand, so on every
    // commit path this must be 0.
    getReplyCard: async (id: string): Promise<ReplyCard> => {
      cardReads.push({
        id,
        rowPainted: document.querySelectorAll(
          `[data-reply-card-id="${id}"]`,
        ).length,
      });
      // 🔴 THE RESPONSE LANDS ON A MACROTASK, AND WITHOUT THAT THIS GUARD
      // MEASURES NOTHING. An `async` mock that returns immediately settles in the
      // SAME microtask drain as the commit that started it, so React has not
      // painted yet either way and `await prefill(…)` vs `void prefill(…)` are
      // literally indistinguishable — measured: the `void` mutant passed all 12
      // tests. No real response can arrive that fast (a fetch resolves from the
      // task queue), so a zero-delay mock is not a simplification of the network,
      // it is a different machine.
      await new Promise((r) => setTimeout(r, 0));
      return { ...CARD, id };
    },
    listChatReads: async () => [],
    markChatRead: async () => {},
    postChat: async () => ({}),
    subscribeEvents: () => () => {},
    getOutsourceWorker: async () => ({}),
  },
}));

function mkMember(id: string, name: string): Member {
  return {
    id,
    name,
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
}

const alice = mkMember(A, "Alice");
const bruno = mkMember(B, "Bruno");

// 🔴 MOUNTED THE WAY `OfficePage` MOUNTS IT (T-48, R13-5): `key={peerId}`, so a
// room switch below is an unmount + a mount, and a jump WITHIN a room (the same
// peer, a different `jumpToMsgId`) is a prop change on the same instance —
// exactly the two lifetimes the app has.
function view(m: Member, jumpToMsgId?: string) {
  return (
    <I18nProvider>
      <ChatArea
        key={m.id}
        member={m}
        members={[alice, bruno]}
        workers={[]}
        jumpToMsgId={jumpToMsgId}
      />
    </I18nProvider>
  );
}

function bubbles(container: HTMLElement): (string | null)[] {
  return Array.from(container.querySelectorAll(".chat__msg-bubble")).map(
    (n) => n.textContent,
  );
}

/** `count` messages from `peer` to the owner, ids `<peer-tag><i>`. */
function seed(peer: string, tag: string, count: number, tsStart: number) {
  for (let i = 0; i < count; i++) {
    log.push({
      id: `${tag}${i}`,
      from: peer,
      to: OWNER,
      body: `${tag}${i}`,
      ts: tsStart + i,
      attachments: [],
      replyCardId: null,
    });
  }
}

beforeEach(() => {
  log.length = 0;
  plainCalls = [];
  windowCalls = [];
  holdWindows = null;
  holdWindowsFrom = Infinity;
  holdPlain = null;
  windowsFail = false;
  windowStale = false;
  scrolls = [];
  cardReads = [];
  seededWaitingCards = [];
  localStorage.clear();
  Element.prototype.scrollIntoView = vi.fn(function (
    this: Element,
    opt?: boolean | ScrollIntoViewOptions,
  ) {
    scrolls.push({
      on:
        this.getAttribute("data-msg-id") ??
        (typeof this.className === "string" ? this.className : ""),
      block: typeof opt === "object" ? opt?.block : undefined,
    });
  });
  document.hasFocus = () => true;
});

/** 🔴 THE INVARIANT THIS WHOLE GROUP IS HELD TO (T-48, guard B).
 *
 * 請示卡 ride the chat stream as ordinary messages carrying only a card id; the
 * card itself is a SEPARATE fetch. A WAITING card is the one that grows when it
 * lands (options, chips, composer) — an answered/expired one mounts collapsed
 * and never fetches at all. So a waiting card sitting ABOVE a scroll target
 * pushes that target down AFTER the jump has landed on it: measured +254px at
 * 1280 wide.
 *
 * The rule, therefore: if a render put a `replyCardStatus: "waiting"` row on
 * screen, that row's `getReplyCard` must ALREADY HAVE HAPPENED — never after the
 * commit that painted it. It is asserted here rather than inside one test
 * because it must hold of every commit path this file drives, and the way this
 * machinery has failed before is one path nobody remembered to check.
 *
 * ⚠️ BUT SAY WHAT THE DENOMINATOR ACTUALLY IS (independent review F8). Exactly
 * ONE test in this file seeds a waiting card, so for the other tests here this
 * afterEach is vacuously true — it is a live assertion over the ANCHOR-ENTRY
 * path and a standing net over the rest, not evidence about them. The other two
 * commit paths carry their own denominators in
 * `useChat.scrollback.test.ts` ("loadNewer's page has its waiting card in hand
 * BEFORE the row reaches the caller", and the same for `resetToLatest`).
 *
 * ⚠️ It carries its own DENOMINATOR. An invariant over an empty set is green for
 * the wrong reason, and most tests in this file seed no cards at all — so a test
 * that DID seed a waiting card must be seen to have painted it, or the guard
 * says so instead of passing. */
afterEach(() => {
  const late = cardReads.filter((r) => r.rowPainted > 0);
  expect(
    late,
    "a WAITING card was fetched while its row was already on screen — the row will now grow under the reader, which is exactly the +254px shift. The thread must not be committed until lib/replyCardCache has the card.",
  ).toEqual([]);
  if (seededWaitingCards.length > 0) {
    // The denominator: this test really did drive a waiting card onto the
    // screen, so the emptiness above is a result and not an absence.
    expect(
      cardReads.map((r) => r.id).sort(),
      "the seeded waiting card was never read at all — the invariant above would then be vacuous",
    ).toEqual([...seededWaitingCards].sort());
    for (const id of seededWaitingCards) {
      expect(
        document.querySelectorAll(`[data-reply-card-id="${id}"]`).length,
        `the seeded waiting card ${id} never reached the screen`,
      ).toBeGreaterThan(0);
    }
  }
});

describe("ChatArea 進房錨點優先(useChat 的 anchor 參數)", () => {
  it("帶著跳轉目標進房時,這間房發出去的第一個請求就是那一則的視窗,一次最新頁都不打", async () => {
    // 🔴 R3-2 的護欄。ChatArea 沒有把 `jumpToMsgId` 交給 `useChat` 的話,hook 會
    // 照舊在訂閱時撈一頁最新的 —— 一次白跑的 round-trip,以及一格「先看到活尾巴」
    // 的中間畫面給一個正要去別的地方的讀者。而那正是這張票拿掉的東西。
    // 這一條量的是**請求本身**,不是終態:終態(落在那一則)在兩種寫法下都成立。
    seed(A, "a", 80, 100);
    const targetId = "a3"; // 遠比最新 30 則舊

    const { container } = render(view(alice, targetId));
    await waitFor(() =>
      expect(
        container.querySelector(`[data-msg-id="${targetId}"]`),
      ).not.toBeNull(),
    );

    expect(
      plainCalls,
      "帶著錨點進房不准打最新頁 —— 有的話就是 ChatArea 沒把 jumpToMsgId 交給 useChat",
    ).toEqual([]);
    // …而且真的有人去撈那個視窗(兩端各一個,兩端都指著同一個 id)。
    expect(windowCalls.map((c) => c.withId)).toEqual([A, A]);
    expect(windowCalls.map((c) => c.anchor)).toEqual([
      { endId: targetId },
      { startId: targetId },
    ]);
    // 落點也對:只撈了那一則附近的一個視窗,不是整條線。
    expect(bubbles(container)).toContain(targetId);
    expect(bubbles(container)).not.toContain("a79");
  });

  it("跳轉目標上方的等待中請示卡,在那一則落地之前就已經握在手上", async () => {
    // 🔴 GUARD B 的分母,也是整包東西的理由(T-48)。訊息串跟請示卡是**兩次**
    // 抓取:卡片晚到,而「等待中」的卡片一到就長高(選項、chips、輸入框)。
    // 只要它坐在跳轉目標的**上面**,目標就會在跳轉已經落地之後被往下推 ——
    // 1280 寬實測 +254px。
    //
    // 這一條把那個形狀擺出來:錨點在 a40,卡片在 a35(目標上方),然後交給上面
    // 那個共用的 afterEach 去問唯一重要的問題 —— 這張卡是在它那一列**被畫出來
    // 之前**讀的,還是之後?jsdom 量不到 254px(它沒有版面),但它量得到順序,
    // 而順序就是因;像素那一半由 chat-jump-card-shift.ct.spec.tsx 在真的
    // Chromium、1280 寬上量。
    seed(A, "a", 80, 100);
    seedWaitingCard(A, "a35-card", "rc-1", 135.5);
    log.sort((x, y) => x.ts - y.ts);

    const { container } = render(view(alice, "a40"));
    await waitFor(() =>
      expect(container.querySelector('[data-msg-id="a40"]')).not.toBeNull(),
    );
    // 分母的另一半:那張卡真的在目標**上面**,不是隨便畫在哪裡。
    const rows = Array.from(
      container.querySelectorAll("[data-msg-id]"),
    ).map((n) => n.getAttribute("data-msg-id"));
    expect(rows.indexOf("a35-card")).toBeGreaterThanOrEqual(0);
    expect(rows.indexOf("a35-card")).toBeLessThan(rows.indexOf("a40"));
    // 而且它是**展開的**等待中卡片 —— 收合的已回覆卡不會長高,拿它當分母等於
    // 沒有分母。
    expect(
      container.querySelector(".reply-card--collapsed"),
      "等待中的卡片不該是收合的 stub",
    ).toBeNull();
  });

  it("一次捲到底就一路走回最新那一則 —— 一頁落地之後不必再有第二個捲動事件", async () => {
    // 🔴 T-48:往新的連鎖曾經是 edge-triggered 的。捲到錨點視窗底部會撈一頁,
    // 貼上去之後**畫面已經在底部**,不會再有捲動事件,於是沒有人再問一次 ——
    // `hasNewer` 還是 true,卻永遠停在半路。真瀏覽器上是產品自己的 auto-follow
    // 偶爾補出下一個事件才走得完(200 次執行紅 2 次,紅的每一次都精準停在
    // rows=61、空等 10 秒不動、補一次捲動立刻補齊)。
    //
    // jsdom 沒有版面,`scrollIntoView` 不會產生任何捲動事件 —— 也就是說,這裡
    // **必然**是那個「唯一觸發被吃掉」的世界。所以這一條不是機率題:連鎖是
    // level-triggered 才走得到 a79,是 edge-triggered 就停在第一頁。
    seed(A, "a", 80, 100);
    const { container } = render(view(alice, "a3"));
    await waitFor(() =>
      expect(container.querySelector('[data-msg-id="a3"]')).not.toBeNull(),
    );
    const windowSpan = bubbles(container).length;
    expect(windowSpan).toBeLessThan(80);

    // 使用者捲到底,就這一下,之後什麼事件都不再送。
    fireEvent.scroll(container.querySelector(".chat__messages")!);

    await waitFor(() => expect(bubbles(container)).toContain("a79"));
    // 而且每一頁都從**上一頁的最後一列**接下去:同一個錨點不會被自動連鎖重複問。
    const forward = windowCalls
      .filter((c) => c.anchor.startId)
      .map((c) => c.anchor.startId);
    expect(new Set(forward).size).toBe(forward.length);
  });

  it("一頁貼上去把底部推遠時,連鎖照樣走得完 —— 動的是版面,不是人", async () => {
    // 🔴 這一條是 CI run 33794983804(macos-e2e、390 寬)紅的那一格。連鎖曾經在
    // 每一頁落地之後**重新量一次幾何**,而往新的一頁是**貼在下面**的:30 列一
    // 落地,底部就退開一個螢幕以上,於是「還在底部嗎」必然答不是,連鎖當場停手。
    // 它之所以大多數時候還是走得完,只是因為 auto-follow 的 `scrollIntoView`
    // 通常在同一拍先把畫面拉回底部;那個順序一旦沒發生,走廊就死在半路 ——
    // 實測 rows 32 → 61、scrollTop 凍在 2702(貼上去之前的最大值,證明畫面沒被
    // 拉回去)、一個 `?start_id=` 之後五秒內再無請求。
    //
    // jsdom 的每一個長度都是 0,所以舊碼在這裡永遠量到 distance=0 而綠 —— 這條
    // 測試自己鋪一份版面(列高 81、視窗 369,就是那台 390 寬量到的幾何),並且
    // **絕不替畫面捲動**:`scrollTop` 從頭到尾就是人捲到底的那一個值。
    seed(A, "a", 80, 100);
    const { container } = render(view(alice, "a3"));
    await waitFor(() =>
      expect(container.querySelector('[data-msg-id="a3"]')).not.toBeNull(),
    );
    const box = container.querySelector(".chat__messages")! as HTMLElement;
    const ROW = 81;
    const CH = 369;
    Object.defineProperty(box, "clientHeight", { get: () => CH });
    Object.defineProperty(box, "scrollHeight", {
      get: () => box.querySelectorAll(".chat__msg").length * ROW,
    });
    box.scrollTop = box.scrollHeight - CH;
    fireEvent.scroll(box);

    await waitFor(() => expect(bubbles(container)).toContain("a79"));
  });

  it("人往回捲就停 —— 那才是「不是你的意思了」", async () => {
    // 上一條的另一半,而且是同一個判準的另一個方向:把幾何的界拿掉之後,走廊還是
    // 要停得下來。停的訊號是**畫面往上走**(只有人做得到 —— 貼一頁只會把底部往
    // 下推),不是「離底部很遠」。
    seed(A, "a", 80, 100);
    const { container } = render(view(alice, "a3"));
    await waitFor(() =>
      expect(container.querySelector('[data-msg-id="a3"]')).not.toBeNull(),
    );
    const box = container.querySelector(".chat__messages")! as HTMLElement;
    const ROW = 81;
    const CH = 369;
    Object.defineProperty(box, "clientHeight", { get: () => CH });
    Object.defineProperty(box, "scrollHeight", {
      get: () => box.querySelectorAll(".chat__msg").length * ROW,
    });
    box.scrollTop = box.scrollHeight - CH;
    fireEvent.scroll(box);
    await waitFor(() => expect(bubbles(container).length).toBeGreaterThan(32));
    // 人往回捲一整個螢幕:從這裡開始不准再自己往前走。
    box.scrollTop = 0;
    fireEvent.scroll(box);
    const asked = windowCalls.length;
    await act(async () => {
      await new Promise((r) => setTimeout(r, 200));
    });
    expect(
      windowCalls.length,
      "人已經離開底部,走廊不准再自己撈下一頁",
      ).toBe(asked);
    expect(bubbles(container)).not.toContain("a79");
  });

  it("上一趟走廊走完之後再跳一次,新的錨點不准自己往前走 —— 那個 armed 是上一趟的", async () => {
    // 🔴 F-A(獨立審查第四輪)。`forwardWalkArmed` 全檔只有一個清除點:捲動事件
    // 裡「畫面往上走」的那一支。`loadAround`、`jumpToLatest`、`resetToLatest`
    // 都不清。8af92bca 之前這不成立也還沒事 —— 連鎖那時還有一道幾何的界,一個
    // 過期的 armed 撞上去就停了。那道界被拿掉之後,過期的 armed 就變成一條會自
    // 己跑起來的走廊:同一間房(`key={peerId}` 沒變 ⇒ 同一個 instance、同一份
    // session)裡第二次跳轉一落地,`hasNewer` 翻回 true,連鎖立刻出發 —— 而讀
    // 的人一根手指都沒有動過。
    //
    // 而且它看得見:第二次跳轉把 `session.nearBottom` 設成 false,所以走廊自己
    // 撈回來的每一頁都會走 reactor 的「有新訊息到了」那一條 —— 預覽列宣告一則
    // 在一百多列之外的訊息,未讀分隔線跟著重新錨定。那正是這張票要拆掉的謊。
    seed(A, "a", 200, 100);
    const { container, rerender } = render(view(alice, "a100"));
    await waitFor(() =>
      expect(container.querySelector('[data-msg-id="a100"]')).not.toBeNull(),
    );
    const box = container.querySelector(".chat__messages")! as HTMLElement;
    const ROW = 81;
    const CH = 369;
    Object.defineProperty(box, "clientHeight", { get: () => CH });
    Object.defineProperty(box, "scrollHeight", {
      get: () => box.querySelectorAll(".chat__msg").length * ROW,
    });
    // 第一趟:人捲到底,走廊一路走回 a199 —— armed 留在 true。
    box.scrollTop = box.scrollHeight - CH;
    fireEvent.scroll(box);
    await waitFor(() => expect(bubbles(container)).toContain("a199"));

    // 第二趟:從回覆卡/任務卡再跳一次,而且是跳到**更舊**的一則。
    const asked = windowCalls.length;
    rerender(view(alice, "a5"));
    await waitFor(() =>
      expect(container.querySelector('[data-msg-id="a5"]')).not.toBeNull(),
    );
    await act(async () => {
      await new Promise((r) => setTimeout(r, 300));
    });

    expect(
      windowCalls.length - asked,
      "第二次跳轉只該問那一對視窗;多出來的每一次都是上一趟的 armed 在自己走",
    ).toBe(2);
    expect(
      bubbles(container),
      "沒有人捲動,走廊不准把錨點到活尾巴之間的每一頁都拉進 DOM",
    ).not.toContain("a199");
    expect(
      container.querySelector('[data-testid="chat-new-msg-preview"]'),
      "走廊自己撈回來的頁不是「新訊息」,不准貼預覽列",
    ).toBeNull();
  });

  it("走廊還在走的時候跳到畫面上已經有的一則,走廊當場結束 —— 不准把讀的人一路拖回活尾巴", async () => {
    // 🔴 F-A 的第三個清除點,而且是唯一一條只有它守得住的路(第十八輪 B-3)。
    // 上一條(「上一趟走廊走完之後再跳一次」)是一條「或」型護欄:目標不在 DOM
    // 裡,所以 `loadAround` 之前那一次清除、和視窗落地之後找到那一列的這一次
    // 清除,兩者任何一個都夠 —— 實測拿掉其中任何一個,這個檔案照樣全綠。
    // 這一條把目標挑在**已經握在手上的那一段**:根本不必抓,`loadAround` 那一支
    // 清除點連跑都不會跑,只剩 `scrollIntoView` 前面那一次。
    //
    // 而且傷害是實的:跳轉發生在走廊**走到一半**,`hasNewer` 仍為真 —— 一個沒
    // 清掉的 armed 會在下一頁落地時立刻續走,把剛剛才被放到 a80 的讀者一路拖
    // 回 a199。
    seed(A, "a", 200, 100);
    // 進房的一對視窗是第 1、2 通;第 3 通是走廊的第一頁,第 4 通按住 ——
    // 走廊就停在半路,跳轉在這個空檔發生。
    holdWindowsFrom = 4;
    const { container, rerender } = render(view(alice, "a100"));
    await waitFor(() =>
      expect(container.querySelector('[data-msg-id="a100"]')).not.toBeNull(),
    );
    const box = container.querySelector(".chat__messages")! as HTMLElement;
    const ROW = 81;
    const CH = 369;
    Object.defineProperty(box, "clientHeight", { get: () => CH });
    Object.defineProperty(box, "scrollHeight", {
      get: () => box.querySelectorAll(".chat__msg").length * ROW,
    });
    box.scrollTop = box.scrollHeight - CH;
    fireEvent.scroll(box);
    await waitFor(() => expect(windowCalls.length).toBe(4));

    // 跳到 a80 —— 進房那一頁就載進來的一列,還在 DOM 裡。
    expect(container.querySelector('[data-msg-id="a80"]')).not.toBeNull();
    rerender(view(alice, "a80"));
    await waitFor(() =>
      expect(scrolls.some((s) => s.on === "a80")).toBe(true),
    );

    // 放開被按住的那一頁 —— 它一落地就是自動連鎖重新評估的那一拍。
    holdWindowsFrom = Infinity;
    await act(async () => {
      holdWindows?.();
      holdWindows = null;
      await new Promise((r) => setTimeout(r, 200));
    });

    expect(
      windowCalls.length,
      "跳轉已經把視窗交出去了,走廊不准再自己撈下一頁",
    ).toBe(4);
    expect(
      bubbles(container),
      "沒有人捲動,走廊不准把跳轉落點到活尾巴之間的每一頁都拉進 DOM",
    ).not.toContain("a199");
  });

  it("走廊還在走的時候按下回到最新,走廊當場結束 —— 不准在活尾巴的請求後面繼續自己往前撈", async () => {
    // 🔴 F-A 的第四個清除點,而它原本一支測試都沒有(第十八輪 B-4:把
    // `jumpToLatest` 裡那一次清除拿掉,這個檔案全綠)。
    //
    // 傷害看得見:回到最新在 `hasNewer` 為真時是**先抓活尾巴**再落地,那一趟
    // 是一個網路來回。這段空檔裡 `hasNewer` 還是真的,而走廊手上那一頁隨時會
    // 落地 —— 一個沒清掉的 armed 於是在讀者已經說了「帶我去最新」之後,繼續
    // 一頁一頁把中間的歷史撈進來,跟活尾巴那一頁搶同一條線。
    seed(A, "a", 200, 100);
    holdWindowsFrom = 4;
    const { container } = render(view(alice, "a100"));
    await waitFor(() =>
      expect(container.querySelector('[data-msg-id="a100"]')).not.toBeNull(),
    );
    const box = container.querySelector(".chat__messages")! as HTMLElement;
    const ROW = 81;
    const CH = 369;
    Object.defineProperty(box, "clientHeight", { get: () => CH });
    Object.defineProperty(box, "scrollHeight", {
      get: () => box.querySelectorAll(".chat__msg").length * ROW,
    });
    box.scrollTop = box.scrollHeight - CH;
    fireEvent.scroll(box);
    await waitFor(() => expect(windowCalls.length).toBe(4));

    // 活尾巴那一頁按在空中 —— 這就是「回到最新已經按下去、但還沒落地」的那一格。
    holdPlain = () => {};
    const arrow = container.querySelector(
      '[data-testid="chat-jump-latest"]',
    ) as HTMLElement;
    expect(arrow, "錨點視窗裡最新那一則不在畫面上,箭頭必須在").not.toBeNull();
    fireEvent.click(arrow);

    // 放開走廊按住的那一頁,讓自動連鎖有東西可以重新評估。
    holdWindowsFrom = Infinity;
    await act(async () => {
      holdWindows?.();
      holdWindows = null;
      await new Promise((r) => setTimeout(r, 200));
    });

    expect(
      windowCalls.length,
      "回到最新就是走廊的終點,它按下去之後不准再多一頁往新的視窗請求",
    ).toBe(4);

    // 收尾:讓活尾巴那一頁落地,別把一個未完成的請求留給下一條測試。
    await act(async () => {
      holdPlain?.();
      holdPlain = null;
      await new Promise((r) => setTimeout(r, 0));
    });
  });

  it("走廊走到一半、上方的內容長高把底部推遠時,不准當成「人往回捲」而靜靜停掉", async () => {
    // 🔴 上一條(縮短)的鏡像,而且它是 `8af92bca` 在治的那個病本人(第十八輪
    // B-8:把停手判準裡「畫面確實往上走」那一半拿掉、只留下高度差那一半,這個
    // 檔案全綠 —— 沒有任何東西釘住它)。
    //
    // 停手的判準是兩件事的合取:畫面**往上走**了,而且那一段不是箱子自己變矮
    // 造成的。少了前一半,任何「箱子變高」都會被算成一段等量的「人往回捲」:
    // 貼一頁 / 圖片解碼完 / 卡片展開讓 `scrollHeight` 長高 2400,而讀的人一根
    // 手指都沒動,`movedUp - shrank` 就是 +2400 —— 走廊當場被誤判成「人走了」而
    // 停手。停在半路、沒有 spinner、沒有結束標記(CI run 33794983804,rows
    // 32 → 61 之後靜止)。
    seed(A, "a", 200, 100);
    holdWindowsFrom = 4;
    const { container } = render(view(alice, "a100"));
    await waitFor(() =>
      expect(container.querySelector('[data-msg-id="a100"]')).not.toBeNull(),
    );
    const box = container.querySelector(".chat__messages")! as HTMLElement;
    const ROW = 81;
    const CH = 369;
    // 上方長出來的高度,由測試自己控制 —— 就是那張卡片展開的那幾百 px。
    let grown = 0;
    Object.defineProperty(box, "clientHeight", { get: () => CH });
    Object.defineProperty(box, "scrollHeight", {
      get: () => box.querySelectorAll(".chat__msg").length * ROW + grown,
    });
    // 人捲到底 —— 走廊從這裡起跑,而且從此不再有任何人為的捲動。
    box.scrollTop = box.scrollHeight - CH;
    fireEvent.scroll(box);
    await waitFor(() => expect(windowCalls.length).toBe(4));

    // 第一頁已經貼在視窗下方 ⇒ 人離底部一個螢幕以上(補不回來的那一格)。
    // 此刻上方長高 2400,而 `scrollTop` 一動也不動。
    grown = 2400;
    fireEvent.scroll(box);

    // 放開被按住的那一頁,讓連鎖有東西可以重新評估。
    holdWindowsFrom = Infinity;
    await act(async () => {
      holdWindows?.();
      holdWindows = null;
      await new Promise((r) => setTimeout(r, 0));
    });

    // 走廊照樣走得完 —— 沒有人往回捲過。
    await waitFor(() => expect(bubbles(container)).toContain("a199"));
  });

  it("走廊走到一半、上方的內容縮短把畫面往回拉時,不准當成「人往回捲」而靜靜停掉", async () => {
    // 🟠 F-C(獨立審查第四輪;他標明這是推理、沒有重現 —— 這條測試把它變成量得
    // 到的)。停的訊號是方向,但 `scrollTop` 變小不是只有人做得到:視窗**上方**
    // 的內容一縮短(卡片從等待收合成已答、任何 reflow),瀏覽器的 scroll
    // anchoring 就會把 `scrollTop` 往回拉,好讓讀的人那一列不動,並且送出一個跟
    // 「人往回捲」逐字一樣的捲動事件。走廊於是靜靜停手:沒有 spinner、沒有結束
    // 標記 —— 正是 8af92bca 在治的那個病,從另一邊走進來。
    //
    // 🔴 而它只在**走廊走到一半**的時候真的傷得到人,這一點必須寫進佈景裡,否則
    // 這條測試是空的。人還貼在底部時,同一個事件的 `nowNearBottom` 為真,底下那
    // 支「捲到底就往前撈」當場把 armed 重新點起來 —— 誤判被自己補回去了(實測:
    // 第一版這條測試對著錯的判準照樣綠)。一頁貼到視窗**下方**之後就沒有這件事:
    // 人離底部一個螢幕以上,補不回來,走廊就死在半路。所以下面把第二頁按住,在
    // 那個空檔鋪縮短。
    //
    // 分辨得開的地方在高度:被拉回來的那一段,`scrollHeight` 掉了同樣多;人捲動
    // 不會讓箱子變矮。而拿來比的高度必須是**上一次 commit 留下的**那一個,不是
    // 上一個捲動事件看到的 —— 貼一頁不會產生捲動事件,所以後者早就過期了好幾頁,
    // 它漏掉的成長會反過來把要偵測的縮短蓋掉(實測:那個版本連既有的「一路走回
    // 最新」都紅了)。
    seed(A, "a", 200, 100);
    // 進房的一對視窗是第 1、2 通;第 3 通是走廊的第一頁,第 4 通按住。
    holdWindowsFrom = 4;
    const { container } = render(view(alice, "a100"));
    await waitFor(() =>
      expect(container.querySelector('[data-msg-id="a100"]')).not.toBeNull(),
    );
    const box = container.querySelector(".chat__messages")! as HTMLElement;
    const ROW = 81;
    const CH = 369;
    // 上方收合掉的高度,由測試自己控制 —— 就是那張卡片塌掉的那幾百 px。
    let shrunk = 0;
    Object.defineProperty(box, "clientHeight", { get: () => CH });
    Object.defineProperty(box, "scrollHeight", {
      get: () => box.querySelectorAll(".chat__msg").length * ROW - shrunk,
    });
    // 人捲到底 —— 走廊從這裡起跑,而且從此不再有任何人為的捲動。
    box.scrollTop = box.scrollHeight - CH;
    fireEvent.scroll(box);
    await waitFor(() => expect(windowCalls.length).toBe(4));

    // 第一頁已經貼在視窗下方 ⇒ 人離底部一個螢幕以上。此刻上方縮掉 240px,
    // anchoring 把 `scrollTop` 往回拉同樣的 240px。
    shrunk = 240;
    box.scrollTop -= 240;
    fireEvent.scroll(box);

    // 放開被按住的那一頁,讓連鎖有東西可以重新評估。
    holdWindowsFrom = Infinity;
    await act(async () => {
      holdWindows?.();
      holdWindows = null;
      await new Promise((r) => setTimeout(r, 0));
    });

    // 走廊照樣走得完 —— 沒有人離開過底部。
    await waitFor(() => expect(bubbles(container)).toContain("a199"));
  });

  it("那一頁一列新的都沒有時,自動連鎖停下來,不會拿同一個錨點空轉", async () => {
    // ⚠️ level-triggered 的另一半,而且是它唯一會失控的形狀。往新的一頁回來
    // 「滿頁、但整頁都是已經握著的列」(重複錨點請求真的會拿回這個)時:
    // `hasNewer` 仍為 true、commit 仍換上一個新物件 ⇒ 連鎖被重新評估 ⇒
    // 沒有界的話它會用同一個錨點永遠打下去,而畫面上一列都不會多。
    // 界就是「最新那一列的 id 沒變就不再問」。
    seed(A, "a", 80, 100);
    const { container } = render(view(alice, "a3"));
    await waitFor(() =>
      expect(container.querySelector('[data-msg-id="a3"]')).not.toBeNull(),
    );
    // 錨點視窗照常落地(否則 `hasNewer` 根本不會是 true,就沒有連鎖可以量);
    // 從這裡開始,往新的每一頁都回一整頁已經握著的列。
    windowStale = true;
    const entry = windowCalls.length;
    fireEvent.scroll(container.querySelector(".chat__messages")!);
    await waitFor(() => expect(windowCalls.length).toBe(entry + 1));
    await act(async () => {
      await new Promise((r) => setTimeout(r, 200));
    });
    expect(
      windowCalls.length,
      "沒有進展就不准再問 —— 同一個錨點打第二次是空轉的第一圈",
    ).toBe(entry + 1);
    expect(
      windowCalls[windowCalls.length - 1].anchor.startId,
      "而且那一次問的就是上一頁的最後一列",
    ).toBe("a32");
    expect(bubbles(container)).not.toContain("a79");
  });

  it("往新那一頁失敗時,自動連鎖不會忙著重打", async () => {
    // 失敗的結局也是「沒有進展」:`loadNewer` 吞掉錯誤、`thread` 一動也不動,
    // 於是連鎖沒有東西可以重新評估。人再捲一次仍然可以重試 —— 那條路是捲動
    // 事件,它刻意不看這個界。
    seed(A, "a", 80, 100);
    const { container } = render(view(alice, "a3"));
    await waitFor(() =>
      expect(container.querySelector('[data-msg-id="a3"]')).not.toBeNull(),
    );
    windowsFail = true;
    const entry = windowCalls.length;
    fireEvent.scroll(container.querySelector(".chat__messages")!);
    await waitFor(() => expect(windowCalls.length).toBe(entry + 1));
    await act(async () => {
      await new Promise((r) => setTimeout(r, 200));
    });
    expect(windowCalls.length).toBe(entry + 1);
    expect(bubbles(container)).not.toContain("a79");
  });

  it("沒有跳轉目標的一般進房,照舊只打一頁最新的,一個視窗請求都沒有", async () => {
    // 另一半:錨點優先只能從跳轉進得去。把它變成無條件的,每一次進房都會多兩個
    // 請求,而且第一個畫面會是歷史。
    seed(A, "a", 40, 100);

    const { container } = render(view(alice));
    await waitFor(() => expect(bubbles(container)).toContain("a39"));

    expect(plainCalls).toEqual([A]);
    expect(windowCalls).toEqual([]);
  });

  it("上一條對話的錨點還在飛的時候切過去,新的那一間照樣載得起來", async () => {
    // 🔴 R3-1 的護欄(hook 層在 useChat.scrollback.test.ts,這裡量的是真的手勢:
    // ChatArea 換 member prop)。A 的錨點是兩個平行 GET,伺服器一忙就是數百毫秒
    // 到數秒;在那段時間內點另一個人的 roster row —— 量到的原始症狀是 B 的房間
    // 22 秒都還是 0 列,而且 A 落地之後也不會自己好。
    seed(A, "a", 80, 100);
    seed(B, "b", 5, 9000);

    holdWindows = () => {};
    const { container, rerender } = render(view(alice, "a3"));
    await waitFor(() => expect(windowCalls).toHaveLength(2));
    expect(bubbles(container), "前提:A 的錨點還沒落地,房間是空的").toEqual([]);

    await act(async () => {
      rerender(view(bruno));
      await new Promise((r) => setTimeout(r, 20));
    });

    // B 是一般進房,它的最新頁必須真的被撈回來。
    expect(plainCalls).toEqual([B]);
    await waitFor(() => expect(bubbles(container)).toEqual(["b0", "b1", "b2", "b3", "b4"]));

    // …而且 A 的錨點落地之後,不准把 B 的房間換掉。
    await act(async () => {
      const release = holdWindows;
      holdWindows = null;
      release?.();
      await new Promise((r) => setTimeout(r, 30));
    });
    expect(bubbles(container)).toEqual(["b0", "b1", "b2", "b3", "b4"]);
  });

  it("上一條對話的錨點抓失敗,不准把它的橫幅貼到切過去的那一間,也不准把那一間捲到底", async () => {
    // 🔴 第五輪 R5-1。這一族的第五個實例:`setJumpNotice` / `setJumpRetry` 是
    // React state,`endRef` 是 DOM ref —— 兩者都只認**現行**那一間房。
    // `unreachable`(5xx / 連線斷)與 `missing`(404)兩條結局都在 superseded 檢查
    // 之前就 return,所以切對話之後照樣走得到:A 的失敗回呼會在 B 的房間裡掛一條
    // 不屬於 B 的「讀不到那則訊息」橫幅(附一顆按了沒反應的重試鈕,因為 B 沒有
    // 跳轉目標),然後把 B 捲到底。
    // 真人版:從連結進 A 的一則舊訊息 → 那一對 window 請求吃到 502 → 在它回來
    // 之前點 roster 切到 B。
    seed(A, "a", 80, 100);
    seed(B, "b", 5, 9000);

    holdWindows = () => {};
    windowsFail = true;
    const { container, rerender } = render(view(alice, "a3"));
    await waitFor(() => expect(windowCalls).toHaveLength(2));

    await act(async () => {
      rerender(view(bruno));
      await new Promise((r) => setTimeout(r, 20));
    });
    await waitFor(() =>
      expect(bubbles(container)).toEqual(["b0", "b1", "b2", "b3", "b4"]),
    );

    scrolls = [];
    await act(async () => {
      const release = holdWindows;
      holdWindows = null;
      release?.();
      await new Promise((r) => setTimeout(r, 30));
    });

    expect(
      container.querySelector(".chat__jump-miss"),
      "B 的房間不該出現 A 的跳轉失敗通知",
    ).toBeNull();
    expect(
      scrolls.map((s) => s.on),
      "A 的失敗回呼不准去捲 B 的 viewport",
    ).not.toContain("chat__scroll-anchor");
    // …而 B 的內容本身沒有被動到。
    expect(bubbles(container)).toEqual(["b0", "b1", "b2", "b3", "b4"]);
  });

  it("切走再切回同一個人,上一趟的錨點失敗不准把橫幅貼到這一趟,也不准把這一趟捲到底", async () => {
    // 🔴 第六輪 R6-1。這一族的第六個實例,而且它指出了前五個共同的根:
    // **身分被寫成「是哪一個人」,而不變量是「是哪一次造訪」**。當時補的兩道
    // 防線綁的都是 `member.id` 這個字串,A→B→**A** 回到同一個人時字串相等,
    // 兩道同時放行:上一趟的「現在讀不到那則訊息」橫幅(附一顆按了沒反應的重試
    // 鈕,因為這一趟沒有 jumpToMsgId)貼進這一趟,而且這一趟被捲到底。
    //
    // 🔴 R13-5 把「哪一次造訪」交還給 React:A→B→A 是三次 mount,上一趟的回呼
    // 寫的是一個已經被丟掉的 component。這條斷言的是同一個結果,不是同一句守衛。
    // 真人版:從連結進 A 的一則舊訊息 → 那一對 window 請求吃到 502 → 在它回來
    // 之前切到 B,再從 roster 切回 A。
    seed(A, "a", 80, 100);
    seed(B, "b", 5, 9000);

    holdWindows = () => {};
    windowsFail = true;
    const { container, rerender } = render(view(alice, "a3"));
    await waitFor(() => expect(windowCalls).toHaveLength(2));

    await act(async () => {
      rerender(view(bruno));
      await new Promise((r) => setTimeout(r, 20));
    });
    await waitFor(() =>
      expect(bubbles(container)).toEqual(["b0", "b1", "b2", "b3", "b4"]),
    );

    // 再切回 A —— 這一趟是一般進房(沒有錨點),所以它要的是最新一頁。
    await act(async () => {
      rerender(view(alice));
      await new Promise((r) => setTimeout(r, 20));
    });
    await waitFor(() => expect(bubbles(container)).toContain("a79"));

    scrolls = [];
    await act(async () => {
      const release = holdWindows;
      holdWindows = null;
      release?.();
      await new Promise((r) => setTimeout(r, 30));
    });

    expect(
      container.querySelector(".chat__jump-miss"),
      "回到 A 的這一趟不該出現上一趟的跳轉失敗通知",
    ).toBeNull();
    expect(
      scrolls.map((s) => s.on),
      "上一趟的失敗回呼不准去捲這一趟的 viewport",
    ).not.toContain("chat__scroll-anchor");
    // …而這一趟的內容本身沒有被動到。
    expect(bubbles(container)).toContain("a79");
  });

  it("上一條對話按下「回到最新」留下的待辦,不准把帶著錨點進來的新對話捲到活尾巴", async () => {
    // 🔴 第五輪 R5-3 的護欄。`pendingLatestScroll` 這一輪從跨 peer 的 ref 改判
    // 進紀錄,但當時**一條會紅的測試都沒有** —— 把它改回跨 peer,src/components/
    // 1472 支全綠。這條把它釘住。
    // 形狀:A 按下「回到最新」而且必須先抓活尾巴(錨點窗 ⇒ hasNewer) → 那一頁還
    // 在空中就切到 B,而 B 正是**帶著錨點**進來的 → B 的錨點窗落地時,A 留下的
    // 待辦會把 B 捲到活尾巴,也就是這張票要拿掉的那格中間畫面。
    seed(A, "a", 80, 100);
    seed(B, "b", 80, 9000);

    const { container, rerender } = render(view(alice, "a3"));
    await waitFor(() =>
      expect(container.querySelector('[data-msg-id="a3"]')).not.toBeNull(),
    );

    // A 的房間是一個歷史窗 ⇒ 圓形箭頭在,而且按下去必須先抓活尾巴。
    holdPlain = () => {};
    const arrow = container.querySelector<HTMLButtonElement>(
      '[data-testid="chat-jump-latest"]',
    );
    expect(arrow, "前提:錨點窗底下該有「回到最新」的箭頭").not.toBeNull();
    await act(async () => {
      fireEvent.click(arrow!);
      await new Promise((r) => setTimeout(r, 10));
    });
    expect(plainCalls, "前提:回到最新真的去抓了活尾巴").toEqual([A]);

    scrolls = [];
    await act(async () => {
      rerender(view(bruno, "b3"));
      await new Promise((r) => setTimeout(r, 30));
    });
    await waitFor(() =>
      expect(container.querySelector('[data-msg-id="b3"]')).not.toBeNull(),
    );

    expect(
      scrolls.filter((s) => s.block === "end"),
      "B 是帶著錨點進來的 —— 不准被上一條對話的待辦捲到活尾巴",
    ).toEqual([]);
    // 落點仍然是 B 自己的錨點。
    expect(scrolls.some((s) => s.on === "b3" && s.block === "center")).toBe(
      true,
    );
  });

  it("帶錨點進房、視窗還沒落地的時候,不准出現新訊息預覽列,也不准把未讀分隔線錨在任何一列上", async () => {
    // 🔴 第八輪 R8-7 —— 這一族到今天為止 `ChatArea` 這一層的第一張網。
    //
    // 前七輪的護欄全都釘在**某一句守衛**上(hook 層:上一趟的資料有沒有進到
    // `messages`)。那個策略八輪找出九個源頭,每一個都是「又一條沒人守的 async
    // 路徑」;而只要任何一個源頭漏掉,同一串下游後果就整套復活,卻沒有任何一條
    // 測試會紅。這一條反過來斷言**結果**:這一趟是帶著錨點進來的,在它自己的視窗
    // 落地之前,房間必須是空的、沒有新訊息預覽列、沒有未讀分隔線 —— 不管污染是
    // 從哪一條路徑來的。
    //
    // 走的路是 R8-1(第九個實例):A 的 post-send refetch 自己那通最新頁掛在空中,
    // 人切到 B(帶錨點,一頁都沒 commit)再帶著錨點切回 A。那一頁落地時,跳轉
    // 反應器已經把 `initialPositioned` 消耗掉、`prevIds` 設成空集合,所以整批
    // stale 列都算「剛到的」⇒ 預覽列與分隔線一起錨在**上一趟的活尾巴**上,
    // 在一間本該只顯示錨點視窗的房間裡。
    seed(A, "a", 80, 100);
    seed(B, "b", 5, 9000);

    const { container, rerender } = render(view(alice));
    await waitFor(() => expect(bubbles(container)).toContain("a79"));

    // A 的 post-send refetch 那通最新頁留在空中。
    holdPlain = () => {};
    const box = container.querySelector(".chat__input") as HTMLTextAreaElement;
    await act(async () => {
      fireEvent.change(box, { target: { value: "在 A 打的字" } });
      fireEvent.click(container.querySelector(".chat__send") as HTMLElement);
      await new Promise((r) => setTimeout(r, 10));
    });

    // 中間那一間也是帶錨點進來的:一頁都沒有 commit,世代票的水位一步都沒動。
    holdWindows = () => {};
    await act(async () => {
      rerender(view(bruno, "b3"));
      await new Promise((r) => setTimeout(r, 20));
    });
    // 回到 A 的第二趟,一樣帶錨點,房間空的在等自己的視窗。
    await act(async () => {
      rerender(view(alice, "a1"));
      await new Promise((r) => setTimeout(r, 20));
    });
    expect(bubbles(container), "前提:這一趟在等它自己的錨點視窗").toEqual([]);

    await act(async () => {
      const release = holdPlain;
      holdPlain = null;
      release?.();
      await new Promise((r) => setTimeout(r, 30));
    });

    expect(
      container.querySelector('[data-testid="chat-new-msg-preview"]'),
      "錨點視窗還沒落地,不准冒出一條指著上一趟活尾巴的新訊息預覽列",
    ).toBeNull();
    expect(
      container.querySelector(".chat__unread-divider"),
      "未讀分隔線不准錨在上一趟的列上",
    ).toBeNull();
    expect(bubbles(container), "這一趟的房間仍然只等它自己的視窗").toEqual([]);

    // …而這一趟自己的視窗照樣落得下來。
    await act(async () => {
      const release = holdWindows;
      holdWindows = null;
      release?.();
      await new Promise((r) => setTimeout(r, 30));
    });
    await waitFor(() =>
      expect(container.querySelector('[data-msg-id="a1"]')).not.toBeNull(),
    );
  });

  it("StrictMode 的 setup→cleanup→setup 之後,錨點進的那間房照樣刷新得起來", async () => {
    // 🔴 第四輪 R4-2。閂的紀錄本來是**每次 effect 跑**就整份重建一次,而
    // `main.tsx` 用的就是 `<StrictMode>` —— 掛載時是 setup → cleanup → setup。
    // 第一次 setup 之後 `loadAround` 就發車了,收尾放的是它捕捉到的那一份;第二次
    // setup 又把 `anchorPending` 設回 true,而 `jumpFetchedRef` 已經記下這個 id,
    // reactor 直接 early-return —— 沒有第二次 `loadAround` 會來清它。那間房從此
    // 不刷新(SSE burst / focus / visibilitychange 全被擋),畫面看起來卻完全正常。
    // 錨點選在靠近活尾巴的一則:`start_id` 那頁回得短 ⇒ `hasNewer === false`,
    // 所以 `load()` 不會被 `hasNewer` 那道閘擋著,量到的就是 `anchorPending` 本身。
    seed(A, "a", 10, 100);
    const targetId = "a7";

    const { container } = render(<StrictMode>{view(alice, targetId)}</StrictMode>);
    await waitFor(() =>
      expect(
        container.querySelector(`[data-msg-id="${targetId}"]`),
      ).not.toBeNull(),
    );

    plainCalls = [];
    await act(async () => {
      window.dispatchEvent(new Event("focus"));
      await new Promise((r) => setTimeout(r, 20));
    });

    expect(
      plainCalls,
      "錨點落地之後這間房必須回到一般的刷新 —— 空的就是 anchorPending 被留在 true",
    ).toEqual([A]);
  });
});
