// HOTSPOT — 一次手勢就是一頁,而讓它為真的是「不 auto-follow」(T-48,owner
// rc-d2e1b69edc66 ①).
//
// 🔴 WHAT THIS MEASURES THAT jsdom CANNOT. The forward walk used to be
// level-triggered: a scroll started it and a landed page re-asked by itself, all
// the way to the live tail. Deleting that effect is not enough, because a real
// browser's `scrollIntoView` FIRES A SCROLL EVENT. So if the scroll-position
// reactor still auto-follows a forward page, the follow re-enters
// `onMessagesScroll` at `distance: 0`, the `nowNearBottom && hasNewer` branch
// fires, and the corridor runs on with the reader's hands in their lap — same
// behaviour, different name.
//
// The two suites catch DIFFERENT halves of that, and both are load-bearing
// (measured with `if (!hasNewer)` deleted from the follow in ChatArea.tsx):
//   · jsdom catches the PROXIMATE CAUSE. `ChatArea.anchor-entry.test.tsx` goes
//     14 passed / 1 FAILED — 「捲到底只撈一頁,而且那一頁不把畫面拉到底」 asserts
//     on WHICH element was scrolled to, and jsdom records that call even though
//     its `scrollIntoView` moves nothing. Do not delete that assertion on the
//     theory that only CT can see this.
//   · CT catches the CONSEQUENCE, which jsdom structurally cannot: jsdom's
//     `scrollIntoView` emits no scroll event and every length reads 0, so the
//     re-entry never happens there and the request COUNT stays 1 either way.
//     Only a real browser turns one gesture into a corridor.
//
// Measured here, Chromium 1280×720, anchor a100 of 200 (page size 30, so the
// window a100…a129 is three forward pages short of the live tail):
//   · fixed product   : one gesture → 1 forward request, scrollTop unchanged,
//                       reader left a screenful above the new bottom
//   · follow restored : one gesture → 3 requests, i.e. all the way to the live
//                       tail (jsdom, same mutant: request count still 1, and
//                       `ChatArea.anchor-entry.test.tsx` 14 passed / 1 FAILED)
//
// ⚠️ SETTLE_MS IS SIZED AGAINST THE RETRY THROTTLE, NOT THE NETWORK. Each
// corridor page is a `human: true` ask, so `HUMAN_RETRY_MIN_MS` (400ms) puts a
// floor under it: a runaway walk costs ~400ms per page whatever the server
// does, and a settle window has to fit several of those to see one.
// 📏 HONESTLY: this was raised from 1200 to 2000 as INSURANCE, not because 1200
// was measured to go green on a broken product. It was not — a reviewer ran the
// mutant at 1200 and got the same red, same count (3). The earlier note here
// claiming 1200 「會假綠」 was a guess written as a measurement; this is the
// correction. 2000 is kept because the margin is free and the failure it would
// hide is the dangerous direction.
import { test, expect } from "@playwright/experimental-ct-react";
import { ChatForwardWalkStory } from "./stories/ChatForwardWalkStory";
import { TARGET_ID, FORWARD_COUNT_KEY } from "./stories/chatForwardWalkFixtures";

/** Long enough that a corridor which is still running has run several more
 * pages by the time it expires — sized against the ~400ms reader-retry floor,
 * not against the network (see the header, including what was and was not
 * measured about the old 1200). */
const SETTLE_MS = 2000;

/** 🔴 ONE GESTURE IS A BURST OF SCROLL EVENTS, NOT ONE. A wheel flick / trackpad
 * swipe is momentum: the box moves in dozens of decreasing steps a few
 * milliseconds apart, and Chromium fires a `scroll` event for every one of
 * them. A guard that models the gesture as a single `el.scrollTop = …` is BLIND
 * to everything the product does with the 2nd…40th event of the same
 * gesture — which is exactly where 「一次手勢一頁」 dies (measured: the e2e
 * `tests/20_chat_jump_to_origin.spec.js` was red on this while this file was
 * green).
 *
 * Momentum, not a ramp: each step covers a quarter of what is left, so most of
 * the events land in the last stretch of travel — including several inside the
 * NEAR_BOTTOM_PX band, which is the half that matters. The travel target is
 * captured BEFORE the first step, because a real flick's momentum is fixed at
 * the fingertip: a page landing mid-flick makes the box taller, and the reader
 * does NOT get carried to the new bottom for free.
 *
 * Returns the event count so the test can assert the gesture really had the
 * shape of a gesture — if this ever degenerates to one event, the guard is
 * blind again and must fail loudly rather than quietly. */
async function flick(box: import("@playwright/test").Locator): Promise<number> {
  return box.evaluate(async (el) => {
    let events = 0;
    const on = () => {
      events += 1;
    };
    el.addEventListener("scroll", on);
    const target = el.scrollHeight - el.clientHeight;
    for (let i = 0; i < 60; i += 1) {
      const remaining = target - el.scrollTop;
      if (remaining <= 0) break;
      el.scrollTop += Math.max(1, remaining * 0.25);
      await new Promise((r) => setTimeout(r, 4));
    }
    await new Promise((r) => setTimeout(r, 30));
    el.removeEventListener("scroll", on);
    return events;
  });
}

/** A flick has to be a flick. Chromium coalesces `scroll` to one event per
 * frame, so a ~250ms flick delivers ~15 of them however many times the loop
 * above writes `scrollTop` — measured 19. 12 is under that and far over the 1
 * the old guard used, so it fails only when the shape is actually lost. */
const MIN_FLICK_EVENTS = 12;

test("one gesture buys exactly one forward page, and the page is not followed", async ({
  mount,
  page,
}) => {
  await page.setViewportSize({ width: 1280, height: 720 });
  const cmp = await mount(<ChatForwardWalkStory />);

  await expect(cmp.locator(`[data-msg-id="${TARGET_ID}"]`)).toBeVisible();
  const box = cmp.locator(".chat__messages");
  const rowsBefore = await cmp.locator(".chat__msg").count();
  // The anchor window really is a window: more history below, and the live tail
  // is not in it. Without this the test could pass on an empty walk.
  await expect(cmp.locator('[data-msg-id="a199"]')).toHaveCount(0);
  // The entry anchor itself issues ONE `?start_id=` (the window below the
  // target), so the walk is counted as a delta from there, never from zero.
  const forwardCalls = () =>
    page.evaluate(
      (k) => (window as never as Record<string, number>)[k] ?? 0,
      FORWARD_COUNT_KEY,
    );
  const entry = await forwardCalls();
  expect(entry, "the anchor entry's own forward window").toBe(1);

  // THE GESTURE — ONE flick, i.e. one burst of dozens of scroll events.
  const flickEvents = await flick(box);
  expect(
    flickEvents,
    "一次 flick 必須是幾十個捲動事件 —— 只有一個的話這道護欄看不見「同一個手勢的第 2…40 個事件」",
  ).toBeGreaterThanOrEqual(MIN_FLICK_EVENTS);

  await expect
    .poll(async () => cmp.locator(".chat__msg").count())
    .toBeGreaterThan(rowsBefore);
  const afterFirst = await box.evaluate((el) => ({
    scrollTop: el.scrollTop,
    scrollHeight: el.scrollHeight,
    clientHeight: el.clientHeight,
  }));

  await page.waitForTimeout(SETTLE_MS);
  expect(
    (await forwardCalls()) - entry,
    "one flick must buy ONE page — more means something is continuing the walk without the reader (the auto-follow's own scroll event, or a replay armed by the SAME gesture's later events, is the way back in)",
  ).toBe(1);

  // …and the reason it stopped: the viewport was left where the reader put it,
  // a screenful above the new bottom, instead of being followed down to it.
  const settled = await box.evaluate((el) => ({
    scrollTop: el.scrollTop,
    distance: el.scrollHeight - el.scrollTop - el.clientHeight,
  }));
  expect(settled.scrollTop).toBe(afterFirst.scrollTop);
  expect(
    settled.distance,
    "the appended page must sit BELOW the fold — it is what the reader has to scroll through to ask for the next one",
  ).toBeGreaterThan(afterFirst.clientHeight);
  await expect(cmp.locator('[data-msg-id="a199"]')).toHaveCount(0);

  // The second gesture — the reader flicks through the page they just bought.
  expect(await flick(box)).toBeGreaterThanOrEqual(MIN_FLICK_EVENTS);
  await expect.poll(async () => (await forwardCalls()) - entry).toBe(2);
});

// 🔴 THE GESTURE THE THROTTLE ATE, AND WHY ONLY CHROMIUM CAN SEE IT
// (independent review #20). The forward retry is rate-limited to one ask per
// 400ms. When that window merely DROPPED the second gesture, the reader was
// stranded: by the time gesture ② is refused, `scrollTop` is already sitting on
// the scroll limit, and a browser emits `scroll` ONLY WHEN THE BOX MOVES. So
// "just scroll again" is not something the reader can do — every further
// downward push is a no-op on an already-pinned scroller and fires nothing at
// all, right across the window and past it.
//
// Measured here, Chromium 1280×720, anchor a100 of 200:
//   · dropped (before)  : 4 further pushes → 0 scroll events → 1 page at 3s
//   · replayed (after)  : the refused gesture is replayed once when the window
//                         closes → 2 pages, still with 0 events from those
//                         pushes
// ⚠️ NOT EVERY REFUSED GESTURE EARNS THIS, and the sibling test above is the
// other half: a refusal is replayed only when the reader is asking about a row
// they were actually SHOWN (a different newest row, and at least PAGE_SEEN_MIN_MS
// after that page was committed). Replaying all of them turns one flick — which
// is DOZENS of events — into two pages.
// jsdom cannot produce this: its lengths all read 0, so a swallowed gesture is
// always repeatable there and the defect passed review #19 unseen.
test("窗口內被吞掉的那次手勢會在窗口結束時補送一次 —— 因為捲到極限之後再捲不會有事件", async ({
  mount,
  page,
}) => {
  await page.setViewportSize({ width: 1280, height: 720 });
  const cmp = await mount(<ChatForwardWalkStory />);

  await expect(cmp.locator(`[data-msg-id="${TARGET_ID}"]`)).toBeVisible();
  const box = cmp.locator(".chat__messages");
  const forwardCalls = () =>
    page.evaluate(
      (k) => (window as never as Record<string, number>)[k] ?? 0,
      FORWARD_COUNT_KEY,
    );
  const entry = await forwardCalls();
  expect(entry, "the anchor entry's own forward window").toBe(1);

  // 手勢① —— 一頁進來,而且要等它真的畫出來:重點在於手勢② 是「捲到那一頁的新
  // 底部」,那一下必須真的移動捲軸才會有事件。
  const rowsBefore = await cmp.locator(".chat__msg").count();
  const t0 = Date.now();
  await box.evaluate((el) => {
    el.scrollTop = el.scrollHeight;
  });
  await expect
    .poll(async () => cmp.locator(".chat__msg").count(), {
      intervals: [5, 5, 10, 10, 20, 20, 50],
    })
    .toBeGreaterThan(rowsBefore);
  expect((await forwardCalls()) - entry).toBe(1);

  // 手勢② 還在同一個 400ms 窗口裡 —— 它是真的手勢:剛貼上的那一頁在視野下方,
  // 所以這一下真的移動了捲軸、真的送出事件,然後被窗口攔下。
  // 手勢② 是讀的人**看到**那一頁之後再推一次,所以它離那一頁落地要有真的反應
  // 時間(120ms;產品端擋的是 64ms 內、還沒畫出來就到的事件)。同時窗口是從
  // 「問出去」那一刻起算的,頁回得越慢餘裕越少 —— 量過:頁 200ms 才回來時,原
  // 本寫死的 150ms 讓手勢② 落在 407ms,窗口外,測到的就不是被吞掉的那一次了。
  await page.waitForTimeout(120);
  const gestureAt = Date.now() - t0;
  expect(
    gestureAt,
    "手勢② 必須落在窗口內,否則這條測的就不是被吞掉的那次手勢",
  ).toBeLessThan(400);
  await box.evaluate((el) => {
    el.scrollTop = el.scrollHeight;
  });

  // 之後往下捲 5 次。捲軸已經壓在極限上,所以這 5 次既不動也不送事件 —— 這正是
  // 「人再捲一次就是重試」在最常見的狀態下不成立的原因,量出來給後人看。
  //
  // ⚠️ 這一段停在窗口關閉之前:窗口一關,補送的那一頁就會把版面撐高,之後的推按
  // 當然又推得動了 —— 那是修好的樣子,不是這一段要量的東西。
  await page.waitForTimeout(20);
  const pushes = await box.evaluate(async (el) => {
    let events = 0;
    const on = () => {
      events += 1;
    };
    el.addEventListener("scroll", on);
    const before = el.scrollTop;
    for (let i = 0; i < 4; i += 1) {
      el.scrollTop = el.scrollHeight;
      await new Promise((r) => setTimeout(r, 12));
    }
    el.removeEventListener("scroll", on);
    return { events, moved: el.scrollTop - before };
  });
  expect(
    pushes,
    "捲到極限之後往下捲既不動也不送事件 —— 這條若不再成立,這個測試就不是在量那個缺陷",
  ).toEqual({ events: 0, moved: 0 });

  // 三秒之後必須有第 2 頁,而且只有第 2 頁:補送是一次,不是一條走廊。
  await page.waitForTimeout(3000);
  expect(
    (await forwardCalls()) - entry,
    "被吞掉的那次手勢消失了 —— 讀的人壓在底部捲不動,靜默停在一頁",
  ).toBe(2);
});
