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
import { describe, it, expect, beforeEach, vi } from "vitest";
import { render, act, waitFor, fireEvent } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { ChatArea } from "./ChatArea";
import type { Member } from "../types";
import type { ChatMessage } from "../api/adapter";

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
/** Makes the held pair end in a REJECTION rather than a page, i.e. the
 * `"unreachable"` ending of `loadAround` (a 502, a dropped connection). */
let windowsFail = false;
/** Same, for the plain newest page — so 回到最新's own fetch can be left in the
 * air across a conversation switch. */
let holdPlain: null | (() => void) = null;
/** Every `scrollIntoView` this room performs, tagged by WHAT was scrolled and
 * with which option — `block: "end"` is `scrollToLatest`'s signature and
 * nothing else in ChatArea uses it. */
let scrolls: { on: string; block: unknown }[] = [];

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
      // Inclusive both ways, mirroring the server: `end_id` is the context
      // ABOVE the anchor, `start_id` the context BELOW.
      return anchor.endId
        ? all.slice(Math.max(0, at - limit + 1), at + 1)
        : all.slice(at, at + limit);
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

function view(m: Member, jumpToMsgId?: string) {
  return (
    <I18nProvider>
      <ChatArea
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
  holdPlain = null;
  windowsFail = false;
  scrolls = [];
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
    // 🔴 第五輪 R5-1。這一族的第五個實例,而且住在上一輪治不到的那一半:
    // `setJumpNotice` / `setJumpRetry` 是 React state,`useKeyedRecord` 管不到,
    // 而 `endRef` 是 DOM ref —— 永遠指著**現行**那一間房。
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
    // **身分被寫成「是哪一個人」,而不變量是「是哪一次造訪」**。
    // 上一顆補的兩道防線綁的都是 `member.id` 這個字串 ——
    // `ChatArea` 的 `peerIdRef.current !== firedFor`,以及 `useKeyedState` 的
    // key 比對 —— A→B→**A** 回到同一個人時字串相等,**兩道同時放行**:
    // 上一趟的「現在讀不到那則訊息」橫幅(附一顆按了沒反應的重試鈕,因為這一趟
    // 沒有 jumpToMsgId)貼進這一趟,而且這一趟被捲到底。
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
