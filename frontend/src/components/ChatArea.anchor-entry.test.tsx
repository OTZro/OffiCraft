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

import { describe, it, expect, beforeEach, vi } from "vitest";
import { render, act, waitFor } from "@testing-library/react";
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
  localStorage.clear();
  Element.prototype.scrollIntoView = vi.fn();
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
});
