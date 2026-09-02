// ③ 的兩半：跳到**最新那一則**，而且版面穩定之後要再校正一次。
//
// 第二半沒有別人在守。舊的「有新訊息」chip 用 smooth scrollIntoView 就結束了，
// 圖片解到真高度、行內回覆卡補撈完把目標推出視窗之外，畫面看起來就像「跳到別的
// 地方去了」。hash jump 那條早就用 ResizeObserver 再置中一次；這支把同一套紀律
// 抽出來共用，所以這裡要證明它真的還在。
import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { scrollToLatest } from "./scrollToLatest";

type ROCallback = () => void;
let observers: Array<{ cb: ROCallback; observed: Element[]; disconnected: boolean }>;

beforeEach(() => {
  vi.useFakeTimers();
  observers = [];
  vi.stubGlobal(
    "ResizeObserver",
    class {
      private rec: { cb: ROCallback; observed: Element[]; disconnected: boolean };
      constructor(cb: ROCallback) {
        this.rec = { cb, observed: [], disconnected: false };
        observers.push(this.rec);
      }
      observe(el: Element) {
        this.rec.observed.push(el);
      }
      disconnect() {
        this.rec.disconnected = true;
      }
      unobserve() {}
    },
  );
});

afterEach(() => {
  vi.useRealTimers();
  vi.unstubAllGlobals();
});

function mkScroller(ids: string[]) {
  const scroller = document.createElement("div");
  const calls: Array<{ id: string | null; args: unknown }> = [];
  for (const id of ids) {
    const row = document.createElement("div");
    row.setAttribute("data-msg-id", id);
    row.scrollIntoView = ((args: unknown) => {
      calls.push({ id: row.getAttribute("data-msg-id"), args });
    }) as Element["scrollIntoView"];
    scroller.appendChild(row);
  }
  return { scroller, calls };
}

describe("scrollToLatest", () => {
  it("捲到最後一則，不是第一則未讀 —— 這正是舊 chip 的 bug", () => {
    const { scroller, calls } = mkScroller(["c1", "c2", "c3"]);
    scrollToLatest(scroller);
    expect(calls).toHaveLength(1);
    expect(calls[0].id).toBe("c3");
  });

  it("不是 smooth —— 校正會重新捲，動畫會被每一次 reflow 打斷重來，看起來像畫面在抽動", () => {
    const { scroller, calls } = mkScroller(["c1"]);
    scrollToLatest(scroller);
    expect(calls[0].args).toEqual({ block: "end" });
  });

  it("版面在捲完之後才長高時會再校正一次", () => {
    const { scroller, calls } = mkScroller(["c1", "c2"]);
    scrollToLatest(scroller);
    expect(calls).toHaveLength(1);
    expect(observers).toHaveLength(1);
    // 觀察的是 in-flow 的子元素，不是 viewport 自己 —— viewport 的框被 flex
    // column 夾死，永遠不會 fire。
    expect(observers[0].observed.length).toBe(scroller.children.length);
    observers[0].cb();
    expect(calls).toHaveLength(2);
    expect(calls[1].id).toBe("c2");
  });

  it("校正有期限，而且 disposer 會提早收掉 —— 不能在使用者自己捲走之後還把他拉回來", () => {
    const { scroller } = mkScroller(["c1"]);
    const stop = scrollToLatest(scroller);
    expect(observers[0].disconnected).toBe(false);
    stop();
    expect(observers[0].disconnected).toBe(true);

    const second = mkScroller(["c1"]);
    scrollToLatest(second.scroller);
    vi.advanceTimersByTime(2600);
    expect(observers[1].disconnected).toBe(true);
  });

  it("沒有任何訊息列時什麼都不做", () => {
    const scroller = document.createElement("div");
    expect(() => scrollToLatest(scroller)()).not.toThrow();
    expect(observers).toHaveLength(0);
  });
});
