// HOTSPOT — 跳過去之後,目標自己又被擠下去 (T-48).
//
// 請示卡 ride the chat stream as ordinary messages carrying only a card id; the
// CARD is a second, later fetch. That fetch used to be the thing that GREW —
// summary, body, option chips, composer — so a 待回覆 card sitting ABOVE a jump
// target pushed that target down AFTER the jump had landed on it, and the
// reader watched the line they asked for slide out from under them.
//
// 🔴 WHAT THIS GUARD PINS SINCE 2026-09-04 (owner `c-6f054c1cb481`). The fix is
// no longer "fetch the card before committing the messages" — it is that EVERY
// card, waiting ones included, renders COLLAPSED: one row built from what the
// carrying message already carries (the summary IS the message body, the status
// IS its hint), so there is no fetch to be late with and no interior to grow
// into. This spec therefore asserts the STRONGER thing: the target does not
// move AND the page never asked for the card at all.
//
// jsdom cannot reach any of this: no layout engine, every rect is 0.
//
// 🔴 WHERE THE CARD IS MATTERS, AND THAT CORRECTS THE BRIEF THIS WAS WRITTEN
// FROM (which said the shift is +254px at 1280 and 0 at 390 "because browser
// scroll anchoring absorbs it"). Measured in this story on the pre-fix code
// (card 70px on the first frame → 591px once it landed):
//
//   · card IN VIEW above the target, 1280×800 : target y 282.4 → 803.8  (+521px)
//   · card IN VIEW above the target,  390×844 : target y 303.4 → 503.5  (+200px)
//   · card OFF-SCREEN above the viewport, 1280: target y 282.4 → 282.4  (0px),
//     with scrollTop compensating exactly (4910 → 5783 against +873px of growth)
//
// So 390 is NOT immune — it shifts too, by less, because the same card renders
// shorter at phone width. What IS immune is a card placed above the fold, where
// Chrome's `overflow-anchor: auto` compensates the growth perfectly. That is the
// placement a guard must avoid, and it is the one a careless fixture falls into:
// this spec's first cut put the card five rows higher and the mutant PASSED.
// Both widths are therefore asserted, and the story keeps the card adjacent to
// the target on purpose.
import { test, expect } from "@playwright/experimental-ct-react";
import { ChatJumpCardShiftStory } from "./stories/ChatJumpCardShiftStory";
import { TARGET_ID } from "./stories/chatJumpCardShiftFixtures";

// The bar the fix has to clear. Sub-pixel layout noise is real; a few hundred
// pixels of shift is not, and nothing in between is acceptable either — a jump
// target that moves at all has moved.
const SUBPIXEL_PX = 1.5;

for (const [label, width, height] of [
  ["1280", 1280, 800],
  ["390", 390, 844],
] as const) {
  test(`${label}px: the jump target does not move, and the waiting card is never fetched`, async ({
    mount,
    page,
  }) => {
    await page.setViewportSize({ width, height });
    const cmp = await mount(<ChatJumpCardShiftStory />);

    const target = cmp.locator(`[data-msg-id="${TARGET_ID}"]`);
    await expect(target).toBeVisible();
    const first = (await target.boundingBox())!;

    // The card's ROW is on screen and sits ABOVE the target — the only position
    // from which it could push it.
    const card = cmp.locator('[data-testid="chat-reply-card"]');
    await expect(card).toBeVisible();
    const cardFirst = (await card.boundingBox())!;
    expect(
      cardFirst.y,
      "the card must sit ABOVE the jump target, or it cannot push it",
    ).toBeLessThan(first.y);
    // …and its BOTTOM EDGE — the seam everything below it rides on — must be IN
    // VIEW. A card whose growth happens entirely above the fold is compensated
    // EXACTLY by Chrome's scroll anchoring (measured: +873px of growth,
    // scrollTop 4910 → 5783, target unmoved), which is a real behaviour and a
    // useless fixture — the mutant passes there. Measured, so it is a fact and
    // not a caution.
    const port = (await cmp.locator(".chat__messages").boundingBox())!;
    const cardBottom = cardFirst.y + cardFirst.height;
    expect(
      cardBottom,
      "the card's bottom edge must be inside the scroller's viewport, or scroll anchoring absorbs the whole growth and this guard measures nothing",
    ).toBeGreaterThanOrEqual(port.y);
    expect(cardBottom).toBeLessThanOrEqual(port.y + port.height);

    // The card's fetch would have been 120ms behind the thread in this story;
    // 500ms leaves it no excuse.
    await page.waitForTimeout(500);

    // (1) CORE red→green.
    const after = (await target.boundingBox())!;
    expect(
      Math.abs(after.y - first.y),
      `the jump target moved ${(after.y - first.y).toFixed(1)}px — the reader watched the line they jumped to slide away`,
    ).toBeLessThanOrEqual(SUBPIXEL_PX);

    // (2) …and it did not merely stay still by being off screen.
    expect(after.y).toBeGreaterThanOrEqual(port.y - 1);
    expect(after.y).toBeLessThanOrEqual(port.y + port.height + 1);

    // (3) THE STRONGER FORM: nothing was fetched, so nothing could arrive late.
    // This is the assertion that would survive somebody re-introducing a
    // compensator: a fix that absorbs the growth still fetches, this one does
    // not ask at all.
    expect(
      await page.evaluate(() => window.__cardFetches ?? -1),
      "a collapsed card must not fetch — the row is built from what the message already carries",
    ).toBe(0);
    expect(
      (await card.boundingBox())!.height,
      "the collapsed row must not have changed height either",
    ).toBeCloseTo(cardFirst.height, 0);

    // THE DENOMINATOR, taken at the END so it can only ever explain a failure of
    // (1)-(3) and never pre-empt one. Without it this test would pass on a story
    // whose card had nothing to grow into — i.e. on a fixture that measures
    // nothing. Expanding it on purpose proves the growth was real and that what
    // (1) pinned is the WITHHOLDING of it, not its absence.
    await expect(cmp.locator(".reply-card--collapsed")).toHaveCount(1);
    await cmp.locator('[data-testid="chat-reply-card-expand"]').click();
    await expect(cmp.locator(".reply-card--collapsed")).toHaveCount(0);
    await expect
      .poll(async () => (await card.boundingBox())!.height, { timeout: 5000 })
      .toBeGreaterThan(150);
  });
}
