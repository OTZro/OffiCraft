// HOTSPOT — 跳過去之後,目標自己又被擠下去 (T-48).
//
// 請示卡 ride the chat stream as ordinary messages carrying only a card id; the
// CARD is a second, later fetch. A WAITING card is the one that GROWS when it
// lands (summary, body, option chips, the composer) — an answered/expired one
// mounts collapsed and never fetches at all. So a waiting card sitting ABOVE a
// jump target pushes that target down AFTER the jump has already landed on it,
// and the reader watches the line they asked for slide out from under them.
//
// jsdom cannot reach any of this: no layout engine, every rect is 0. The
// ORDERING half of the same fix (the card is fetched BEFORE the commit, never
// after it) is pinned in jsdom by ChatArea.anchor-entry.test.tsx's shared
// afterEach; this file is the half that measures what the reader sees.
//
// 🔴 WHAT MAKES OR UNMAKES THIS GUARD IS **WHERE THE CARD IS**, NOT THE WIDTH —
// and that corrects the brief this was written from, which said the shift is
// +254px at 1280 and 0 at 390 "because browser scroll anchoring absorbs it".
// Measured here, in this story, on the pre-fix code (card 70px on the first
// frame → 591px once it lands):
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
  test(`${label}px: the jump target does not move once the waiting card fills in`, async ({
    mount,
    page,
  }) => {
    await page.setViewportSize({ width, height });
    const cmp = await mount(<ChatJumpCardShiftStory />);

    const target = cmp.locator(`[data-msg-id="${TARGET_ID}"]`);
    await expect(target).toBeVisible();
    const first = (await target.boundingBox())!;

    // The card's ROW is on screen and sits ABOVE the target — the only position
    // from which it can push it. Asserted on the FIRST frame, and deliberately
    // WITHOUT any claim about its height: on the broken code the card is still
    // empty at this moment (measured 70px at 1280, 51px at 390), so a height
    // assertion here would fail BEFORE the shift assertion below and the guard
    // would be reporting the wrong thing.
    const card = cmp.locator('[data-testid="chat-reply-card"]');
    await expect(card).toBeVisible();
    const cardFirst = (await card.boundingBox())!;
    expect(
      cardFirst.y,
      "the card must sit ABOVE the jump target, or it cannot push it",
    ).toBeLessThan(first.y);
    // …and its BOTTOM EDGE — the seam that everything below it rides on — must
    // be IN VIEW. A card whose growth happens entirely above the fold is
    // compensated EXACTLY by Chrome's scroll anchoring (measured: +873px of
    // growth, scrollTop 4910 → 5783, target unmoved), which is a real behaviour
    // and a useless fixture — the mutant passes there. This spec's first cut put
    // the card five rows higher and did exactly that. Measured, so it is a fact
    // and not a caution.
    const port = (await cmp.locator(".chat__messages").boundingBox())!;
    const cardBottom = cardFirst.y + cardFirst.height;
    expect(
      cardBottom,
      "the card's bottom edge must be inside the scroller's viewport, or scroll anchoring absorbs the whole growth and this guard measures nothing",
    ).toBeGreaterThanOrEqual(port.y);
    expect(cardBottom).toBeLessThanOrEqual(port.y + port.height);

    // The card's own fetch is 120ms behind the thread in this story; 500ms
    // leaves it no excuse.
    await page.waitForTimeout(500);

    const after = (await target.boundingBox())!;
    // (1) CORE red→green.
    expect(
      Math.abs(after.y - first.y),
      `the jump target moved ${(after.y - first.y).toFixed(1)}px after the reply card filled in — the reader watched the line they jumped to slide away`,
    ).toBeLessThanOrEqual(SUBPIXEL_PX);

    // The DENOMINATOR, taken at the END, where it is true in BOTH worlds — so it
    // can only ever explain a failure of (1), never pre-empt one. Without it this
    // test would pass on a story that never drew a waiting card at all. Both
    // halves: the card is EXPANDED (a collapsed 已回覆 stub is already its final
    // height and could never produce this shift), and it is a TALL surface — i.e.
    // there really was growth available to push the target with.
    await expect(
      cmp.locator(".reply-card--collapsed"),
      "the card under test must be the expanded WAITING interior",
    ).toHaveCount(0);
    expect(
      (await card.boundingBox())!.height,
      "a waiting card is a tall surface — a short one would make the shift unmeasurable",
    ).toBeGreaterThan(150);

    // (2) …and the target did not merely stay still by being off screen.
    expect(after.y).toBeGreaterThanOrEqual(port.y - 1);
    expect(after.y).toBeLessThanOrEqual(port.y + port.height + 1);
  });
}
