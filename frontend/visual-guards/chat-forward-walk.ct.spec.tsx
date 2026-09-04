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
 * Momentum, not a ramp: each step covers a quarter of what is LEFT OF THE
 * BUDGET, and the budget is fixed at the fingertip before the first step,
 * because that is what a real flick is: a fixed amount of travel handed to the
 * compositor. A page landing mid-flick makes the box taller, and the reader does
 * NOT get carried to the new bottom for free.
 *
 * 🔴 THE BUDGET IS COUNTED DOWN, NOT RE-READ FROM THE BOX (T-48, independent
 * review #22 F-1). It used to be `target - el.scrollTop` re-measured every
 * step, and that is NOT a fixed momentum: when a reflow SHRINKS the box the
 * browser clamps `scrollTop` back — measured in Chromium under CDP CPU throttle
 * x10, the box went 15631 → 10110px and `scrollTop` 9941 → 7344 (the earlier
 * 「9932」 here was a neighbouring event's value, review #23 F-5) — and this loop
 * then cheerfully re-covered the 2600px it had already delivered, walking the
 * reader through the whole appended page and into a SECOND request. That is the
 * guard's own hand on the box, not the product's: with the budget counted down
 * instead, the shipped product is green at CPU throttle x1/x4/x10/x20 (8 runs
 * each, 32 total), where the re-measuring version was red 31 times in 40 at x10
 * alone. The behaviour that version was accidentally exercising — a clamp
 * buying a page — is guarded on purpose now, in
 * `ChatArea.anchor-entry.test.tsx` 「版面縮回去把捲動位置往回夾的那個事件,不
 * 准買到一頁」.
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
    let budget = el.scrollHeight - el.clientHeight - el.scrollTop;
    for (let i = 0; i < 60; i += 1) {
      if (budget <= 0) break;
      const step = Math.max(1, budget * 0.25);
      budget -= step;
      el.scrollTop += step;
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
// they were actually SHOWN — a different newest row, AND the page carrying it
// already written into the box (`pageUnseenRef`, cleared by the layout effect of
// the commit that carries it; it used to be a 64ms timeout, review #22 F-1).
// Replaying all of them turns one flick — which is DOZENS of events — into two
// pages. And the replay itself only lands while the reader is still in the
// bottom band (F-6).
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

// 🔴 THE SIGHT GATE, ON ITS OWN, IN A REAL BROWSER (T-48, independent review
// #23 F-1). The two mechanisms this fix is built on — `pageUnseenIdRef` (the
// sight gate) and `notClampedBack` (a scroll event is not a gesture) — were
// each measured to be INVISIBLE to this file: deleting either one on its own
// left the tests above green 20 runs each, 40 with both deleted. Everything
// this suite could still see was coming from a THIRD patch (the F-6 probe), so
// the product's two headline mechanisms were carried by jsdom alone — and this
// ticket's whole 22-round lesson is that jsdom's lengths all read 0.
//
// THE ISOLATION IS THE POINT, and each clause below exists to take one other
// guard out of the answer:
//   · `pageLatencyMs: 650` — the 400ms retry throttle is measured from the ASK.
//     A page that lands at 40ms puts the gap INSIDE the window, the throttle
//     answers first, and whatever the gate would have said is never asked. At
//     650ms the gap is on the far side of the window and the gate is the only
//     thing left standing there.
//   · a SYNTHETIC `scroll` (`dispatchEvent`, `scrollTop` untouched) — a clamp
//     lowers `scrollTop`, so `notClampedBack` (`>=`) passes an event that did
//     not move the box. The direction rule is therefore NOT what this measures.
//   · the immediate path, never the trailing replay — so the F-6 probe
//     (「is the reader still at the bottom 400ms later」) is not in the answer
//     either. It is the one that was masking everything.
//   · the loop stops the instant `scrollHeight` grows — after the rows are
//     written the reader is a screenful above the bottom and no event of any
//     kind would be served, so the window measured is exactly the gap.
// Mutant `if (pageUnseenIdRef.current !== null) return;` deleted ⇒ red here,
// on the 「一次手勢一頁」 count. See the report for the denominator.
test("那一頁還沒被寫進箱子時,同一次手勢的下一個事件不准再買一頁", async ({
  mount,
  page,
}) => {
  await page.setViewportSize({ width: 1280, height: 720 });
  const cmp = await mount(<ChatForwardWalkStory pageLatencyMs={650} />);
  await expect(cmp.locator(`[data-msg-id="${TARGET_ID}"]`)).toBeVisible();
  // The gap being measured is 「commit returned, React has not written the rows
  // yet」. On an idle machine that is a frame; throttled it is wide enough to
  // aim at. This is CDP, never host load.
  const cdp = await page.context().newCDPSession(page);
  await cdp.send("Emulation.setCPUThrottlingRate", { rate: 20 });

  const box = cmp.locator(".chat__messages");
  const forwardCalls = () =>
    page.evaluate(
      (k) => (window as never as Record<string, number>)[k] ?? 0,
      FORWARD_COUNT_KEY,
    );
  const entry = await forwardCalls();
  expect(entry, "the anchor entry's own forward window").toBe(1);

  const probe = await box.evaluate(async (el, key: string) => {
    const w = window as never as Record<string, number>;
    const served = w[key] ?? 0;
    const t0 = Date.now();
    let fired = 0;
    let landedAt = 0;
    let heightAtCommit = 0;
    let topAtCommit = 0;
    let movedDuringGap = 0;
    // 🔴 A TIMER CANNOT SEE THIS GAP, AND THAT IS ITSELF A MEASUREMENT. React
    // schedules its render on a MessageChannel, and a channel message outranks
    // every `setTimeout` — a 2ms poll placed here read `distance` 5140, i.e.
    // the rows were already written by the time it ran. So the probe rides the
    // SAME queue: a message posted before React posts its own runs before it,
    // which is exactly the window in which the mirror has moved and the box has
    // not.
    await new Promise<void>((done) => {
      const ch = new MessageChannel();
      ch.port1.onmessage = () => {
        if (Date.now() - t0 > 5000) return done();
        if ((w[key] ?? 0) === served) return ch.port2.postMessage(0);
        if (landedAt === 0) {
          landedAt = Date.now() - t0;
          heightAtCommit = el.scrollHeight;
          topAtCommit = el.scrollTop;
        }
        if (el.scrollHeight !== heightAtCommit) {
          movedDuringGap = el.scrollTop - topAtCommit;
          return done();
        }
        // A SYNTHETIC event: `scrollTop` is not touched, so the direction rule
        // (`notClampedBack`, a `>=`) passes it and the sight gate is the only
        // thing left that can refuse it.
        el.dispatchEvent(new Event("scroll"));
        fired += 1;
        ch.port2.postMessage(0);
      };
      // The gesture: one push to the limit. It buys page 1 and starts the 400ms
      // clock. Posted after, so the loop is already in the queue when it lands.
      el.scrollTop = el.scrollHeight;
      ch.port2.postMessage(0);
    });
    return {
      fired,
      landedAt,
      movedDuringGap,
      distanceAtCommit: heightAtCommit - topAtCommit - el.clientHeight,
    };
  }, FORWARD_COUNT_KEY);

  // 🔴 THE GUARD MUST FAIL LOUDLY RATHER THAN VACUOUSLY. If the gap ever closes
  // before a single event lands in it, this test proves nothing and has to say
  // so — the same reason `MIN_FLICK_EVENTS` exists above.
  expect(
    probe.fired,
    "commit 與「列被寫進箱子」之間那道縫裡一個事件都沒送到 —— 這條護欄什麼都沒量到",
  ).toBeGreaterThanOrEqual(1);
  expect(
    probe.movedDuringGap,
    "這些是合成事件,箱子一格都不准動 —— 動了的話量到的就是方向規則,不是 sight gate",
  ).toBe(0);
  expect(
    probe.distanceAtCommit,
    "前提:那一頁 commit 的當下,讀的人還壓在底部帶裡 —— 否則這些事件根本進不了那個分支",
  ).toBeLessThanOrEqual(80);
  expect(
    probe.landedAt,
    "那一頁必須在 400ms 的節流窗口之外落地,否則擋下這些事件的是節流,不是 sight gate",
  ).toBeGreaterThan(400);

  await page.waitForTimeout(SETTLE_MS);
  expect(
    (await forwardCalls()) - entry,
    "那一頁還沒被寫進箱子,它自己的手勢尾巴卻買到了第二頁 —— 一次手勢兩頁",
  ).toBe(1);
});

// 🔴 THE CLAMP, ON ITS OWN, IN A REAL BROWSER (T-48, independent review #23
// F-1 — and the measurement review #23 F-4 asked for).
//
// `notClampedBack` was the fix's other headline mechanism and, like the sight
// gate, deleting it left every other test in this file green (20 runs). jsdom
// was carrying it alone, on geometry faked with `Object.defineProperty` — and
// this ticket's whole lesson is that jsdom's lengths all read 0.
//
// What a browser does, measured here (900px pinned column, no CPU throttle):
//   entry state, reader parked on the jump target, `hasNewer` true, no gesture
//   ever made:                       scrollTop 4984, box 10491, distance 4964
//   30 rows below the fold collapse: box 5346 ⇒ the LIMIT is now 4803, below
//                                    where the reader is standing
//   ⇒ Chromium fires exactly ONE `scroll` event: scrollTop 4984 → 4803, i.e.
//     BACKWARDS, and it lands at distance 0 — inside the 80px bottom band.
// Nobody touched the box. Without the direction rule that event is a gesture
// and it buys a page, which is this ticket's 「零手勢就撈」 with no reader in it.
//
// THE ISOLATION (F-1's lesson — a guard that shares a mechanism measures
// nothing): no gesture has been made, so `humanRetryAtRef` is still 0 and the
// 400ms throttle is not in the answer; no page has been bought, so the sight
// gate is not owed anything and is not in the answer; the path is the immediate
// one, so the F-6 probe is not in the answer either. The one thing that can
// refuse this event is the direction rule.
//
// 📏 `overflow-anchor: none` IS SET ON PURPOSE, and it is a second measurement.
// With anchoring left on, collapsing rows makes Chromium move `scrollTop` by the
// same amount to hold the reader's view still — measured 15631→10451 with
// `scrollTop` 9948→4768 and distance 5140 on BOTH sides, i.e. anchoring hides
// the clamp instead of causing it. Turning it off is what makes the shrink
// reach the limit rather than chase it.
test("版面自己縮矮把讀的人夾回極限的那個事件,不准買到一頁", async ({
  mount,
  page,
}) => {
  await page.setViewportSize({ width: 1280, height: 720 });
  // Pinned column: the default story's shrink-to-fit reflow makes the box
  // change size on its own (fix3 §2.4), and a guard about what a SHRINK does
  // cannot have a second uninvited shrink in it.
  const cmp = await mount(<ChatForwardWalkStory widthPx={900} />);
  await expect(cmp.locator(`[data-msg-id="${TARGET_ID}"]`)).toBeVisible();
  const box = cmp.locator(".chat__messages");
  const forwardCalls = () =>
    page.evaluate(
      (k) => (window as never as Record<string, number>)[k] ?? 0,
      FORWARD_COUNT_KEY,
    );
  // Let the anchor entry finish completely: from here on, every forward request
  // is one this test caused.
  await page.waitForTimeout(900);
  const entry = await forwardCalls();
  expect(entry, "the anchor entry's own forward window").toBe(1);

  const probe = await box.evaluate(async (el) => {
    const rec: number[][] = [];
    const on = () =>
      rec.push([
        el.scrollTop,
        el.scrollHeight,
        el.scrollHeight - el.scrollTop - el.clientHeight,
      ]);
    el.addEventListener("scroll", on);
    (el as HTMLElement & { __t48rec?: number[][] }).__t48rec = rec;
    el.style.overflowAnchor = "none";
    const before = {
      top: el.scrollTop,
      h: el.scrollHeight,
      dist: el.scrollHeight - el.scrollTop - el.clientHeight,
    };
    // The reflow: rows below the fold collapse — a reply card resolving to
    // something shorter, an image landing smaller than its placeholder. Nothing
    // here touches `scrollTop`; only the browser does, and only to clamp.
    const top0 = el.scrollTop;
    const rows = Array.from(el.querySelectorAll(".chat__msg")) as HTMLElement[];
    let collapsed = 0;
    for (let i = rows.length - 1; i >= 0; i -= 1) {
      if (el.scrollHeight - el.clientHeight < top0 - 30) break;
      rows[i].style.marginBottom = `-${rows[i].offsetHeight + 12}px`;
      collapsed += 1;
    }
    // Just long enough for the clamp's own event, and NOT long enough for a
    // page bought off it to land — the numbers asserted below have to be the
    // clamp's, identically in both arms of the mutant.
    await new Promise((r) => setTimeout(r, 30));
    return { before, collapsed, clamp: rec[0], events: rec.length };
  });

  // The guard must fail loudly rather than vacuously: if the browser stopped
  // clamping, or stopped telling anyone, this test proves nothing.
  expect(
    probe.events,
    "版面縮矮之後瀏覽器一個 scroll 事件都沒送 —— 這條護欄什麼都沒量到",
  ).toBe(1);
  expect(
    probe.clamp[0],
    "那一夾必須把 scrollTop 往回帶 —— 這正是它跟「讀的人往下推」唯一的差別",
  ).toBeLessThan(probe.before.top);
  expect(
    probe.clamp[2],
    "那一夾必須落在底部帶裡,否則這個事件根本進不了買下一頁的那個分支",
  ).toBeLessThanOrEqual(80);

  await page.waitForTimeout(SETTLE_MS);
  expect(
    (await forwardCalls()) - entry,
    "版面自己縮回去就買到了一頁 —— 沒有人碰過那個箱子",
  ).toBe(0);

  // 🟠 THE PRICE OF SWALLOWING IT, MEASURED AND PINNED (review #23 F-4).
  // Swallowing the clamp is right — nobody asked — but it leaves the reader
  // pinned ON the limit, where further downward pushes move nothing and
  // therefore fire NOTHING. So the next page costs them TWO gestures (up, then
  // down) instead of one. That is the price of this rule, it is recoverable,
  // and it is written down here so a future change to it is loud, not silent.
  const pushEvents = await box.evaluate(async (el) => {
    const rec = (el as HTMLElement & { __t48rec?: number[][] }).__t48rec!;
    const n0 = rec.length;
    for (let i = 0; i < 5; i += 1) {
      el.scrollTop = el.scrollHeight;
      await new Promise((r) => setTimeout(r, 25));
    }
    return rec.length - n0;
  });
  expect(
    pushEvents,
    "夾回極限之後往下推仍然會發事件 —— F-4 的取捨變了,規則文件要跟著改",
  ).toBe(0);
});

// 🔴 THE SAME GESTURE, MADE WITH A REAL INPUT DEVICE (T-48, independent review
// #23, bonus item — 「the thing that could end this argument does not exist」).
//
// Every other test in this file models the flick by WRITING `scrollTop` from a
// script, and two rounds of review have now turned on which script model is
// faithful: re-measuring the box each step (review #22's guard, red 13/20 on
// HEAD) or counting a fixed budget down (fix3's, green 32/32 on the same HEAD
// code). fix3 won that argument on INTERNAL CONSISTENCY — the budget model
// matches what its own comment promises a flick is — and internal consistency
// is not evidence. `page.mouse.wheel` is: it goes through Chromium's real input
// pipeline, real event coalescing, real scroll animation, and no script decides
// how far the box travels.
//
// Measured here (1280×720, no CPU throttle): one momentum flick of decreasing
// wheel deltas ⇒ 29 real `scroll` events over 442ms, ONE forward page, and the
// box grew 10491 → 15631px MID-FLICK while `scrollTop` finished at 9948 — i.e.
// the reader was left 5140px above the new bottom and the remaining wheel
// deltas did NOT carry them down to it. That is the budget model's prediction
// and not the re-measuring one's, from an input the guard does not author.
//
// ⚠️ WHAT THIS DOES NOT SETTLE: it is still a synthesised wheel, not a finger on
// a trackpad, and it has no overscroll bounce. It settles the question of
// whether the guard's own hand was on the box, which is the one that was open.
test("同一個手勢用真的滾輪做一次 —— 仍然只買一頁", async ({ mount, page }) => {
  await page.setViewportSize({ width: 1280, height: 720 });
  const cmp = await mount(<ChatForwardWalkStory />);
  await expect(cmp.locator(`[data-msg-id="${TARGET_ID}"]`)).toBeVisible();
  const box = cmp.locator(".chat__messages");
  const forwardCalls = () =>
    page.evaluate(
      (k) => (window as never as Record<string, number>)[k] ?? 0,
      FORWARD_COUNT_KEY,
    );
  await page.waitForTimeout(600);
  const entry = await forwardCalls();
  expect(entry, "the anchor entry's own forward window").toBe(1);

  await box.hover();
  await box.evaluate((el) => {
    const w = window as never as Record<string, unknown>;
    const rec: number[] = [];
    w.__t48wheelEvents = rec;
    el.addEventListener("scroll", () => rec.push(el.scrollTop));
  });
  const before = await box.evaluate((el) => ({
    h: el.scrollHeight,
    ch: el.clientHeight,
  }));

  // ONE flick: momentum, i.e. decreasing deltas, and the total travel is
  // decided before the first one — but by the LOOP, not by re-reading the box,
  // because a real fingertip cannot re-read it either.
  let budget = await box.evaluate(
    (el) => el.scrollHeight - el.clientHeight - el.scrollTop,
  );
  for (let i = 0; i < 60 && budget > 0; i += 1) {
    const step = Math.max(1, budget * 0.25);
    budget -= step;
    await page.mouse.wheel(0, step);
  }

  const events = await page.evaluate(
    () => (window as never as Record<string, number[]>).__t48wheelEvents.length,
  );
  expect(
    events,
    "真滾輪的一次 flick 必須是幾十個捲動事件 —— 只有一個的話這道護欄看不見「同一個手勢的第 2…40 個事件」",
  ).toBeGreaterThanOrEqual(MIN_FLICK_EVENTS);

  await page.waitForTimeout(SETTLE_MS);
  expect(
    (await forwardCalls()) - entry,
    "真的滾輪做的一次 flick 買到了不只一頁 —— 有人在讀的人手放開之後還在往下走",
  ).toBe(1);

  const settled = await box.evaluate((el) => ({
    h: el.scrollHeight,
    distance: el.scrollHeight - el.scrollTop - el.clientHeight,
  }));
  expect(
    settled.h,
    "前提:那一頁是在 flick 還在走的時候落地的,箱子當場長高 —— 否則這條測的不是同一件事",
  ).toBeGreaterThan(before.h);
  expect(
    settled.distance,
    "貼上來的那一頁必須留在視野下方 —— 讀的人沒有被自己的動量帶下去",
  ).toBeGreaterThan(before.ch);
});
