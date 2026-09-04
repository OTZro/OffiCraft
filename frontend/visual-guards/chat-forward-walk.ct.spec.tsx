// HOTSPOT — 一次手勢就是一頁,而讓它為真的是「不 auto-follow」(T-48,owner
// rc-d2e1b69edc66 ①).
//
// 🔴 THIS IS THE ONE ASSERTION jsdom STRUCTURALLY CANNOT MAKE. The forward walk
// used to be level-triggered: a scroll started it and a landed page re-asked by
// itself, all the way to the live tail. Deleting that effect is not enough,
// because a real browser's `scrollIntoView` FIRES A SCROLL EVENT. So if the
// scroll-position reactor still auto-follows a forward page, the follow re-
// enters `onMessagesScroll` at `distance: 0`, the `nowNearBottom && hasNewer`
// branch fires, and the corridor runs on with the reader's hands in their lap —
// same behaviour, different name. jsdom's `scrollIntoView` is a no-op that emits
// no event and reports every length as 0, so the vitest suite is green against
// BOTH products (measured: ChatArea.anchor-entry.test.tsx passes 14/14 with the
// follow restored).
//
// Measured here, Chromium 1280×720, anchor a100 of 200:
//   · fixed product   : one gesture → 1 forward request, scrollTop unchanged,
//                       reader left a screenful above the new bottom
//   · follow restored : one gesture → the walk runs itself to the live tail
import { test, expect } from "@playwright/experimental-ct-react";
import { ChatForwardWalkStory } from "./stories/ChatForwardWalkStory";
import { TARGET_ID, FORWARD_COUNT_KEY } from "./stories/chatForwardWalkFixtures";

/** Long enough that a corridor which is still running has run several more
 * pages by the time it expires — the walk's own pages land in ~10ms here. */
const SETTLE_MS = 1200;

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
