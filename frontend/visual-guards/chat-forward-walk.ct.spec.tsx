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
// ⚠️ SETTLE_MS HAS TO CLEAR THE RETRY THROTTLE, NOT JUST THE NETWORK. Each
// corridor page is a `human: true` ask, so `HUMAN_RETRY_MIN_MS` (400ms) puts a
// floor under it: the runaway walk costs ~400ms per page whatever the server
// does. A settle window that only fits one or two of those can measure a
// still-broken product as a single page and go GREEN — the dangerous direction.
// Keep it comfortably above 3 × HUMAN_RETRY_MIN_MS.
import { test, expect } from "@playwright/experimental-ct-react";
import { ChatForwardWalkStory } from "./stories/ChatForwardWalkStory";
import { TARGET_ID, FORWARD_COUNT_KEY } from "./stories/chatForwardWalkFixtures";

/** Long enough that a corridor which is still running has run several more
 * pages by the time it expires. The pages themselves land in ~10ms here, but
 * the reader-retry throttle costs ~400ms each (see the header), so this is
 * sized against that floor, not against the network. */
const SETTLE_MS = 2000;

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

  // THE GESTURE. Setting `scrollTop` in the page is what a wheel does to the
  // scroller, and Chromium fires the same scroll event for it.
  await box.evaluate((el) => {
    el.scrollTop = el.scrollHeight;
  });

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
    "one gesture must buy ONE page — more means something is continuing the walk without the reader (the auto-follow's own scroll event is the way back in)",
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

  // The second gesture — the reader scrolls through the page they just bought.
  await box.evaluate((el) => {
    el.scrollTop = el.scrollHeight;
  });
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
//   · dropped (before)  : 5 further pushes → 0 scroll events → 1 page at 3s
//   · coalesced (after) : the refused gesture is replayed once when the window
//                         closes → 2 pages, still with 0 events from those
//                         pushes
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
  await page.waitForTimeout(150);
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
  await page.waitForTimeout(30);
  const pushes = await box.evaluate(async (el) => {
    let events = 0;
    const on = () => {
      events += 1;
    };
    el.addEventListener("scroll", on);
    const before = el.scrollTop;
    for (let i = 0; i < 5; i += 1) {
      el.scrollTop = el.scrollHeight;
      await new Promise((r) => setTimeout(r, 20));
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
