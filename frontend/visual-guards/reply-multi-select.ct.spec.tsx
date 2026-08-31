// T-40 — the multi-select waiting card's new rows must stay INSIDE the card.
//
// WHY A REAL BROWSER: this is layout. jsdom applies no stylesheet, so the
// jsdom tests in ReplyCardBody.multi-select.test.tsx prove the count line and
// the send button are RENDERED and say nothing about whether they FIT. Every
// assertion below is a measured rectangle.
//
// MUTANTS (all measured here, none assumed):
//   * `flex: 1` → `flex: none` on `.reply-option__text` (replies.css): the
//     label reaches x+width 605px against a card whose right edge is 320px →
//     (1) red at 320, green at 1280. That is the shape this guard exists for.
//   * move `<ReplySelectionCount>` below the composer in ReplyCardBody: the
//     count line lands 74px (1280) / 74px (320) ABOVE where the send row
//     starts → (2)'s ordering pair red at BOTH widths.
//   * drop the `background` from `.reply-option--selected`: the ticked chip
//     repaints to nothing → the paint test red (its box assertions stay green,
//     which is the point of pairing them).
//
// ⚠️ Two mutants tried here were GREEN and are recorded so nobody re-certifies
// against them: `white-space: nowrap` on `.reply-card__selcount` (「已選 0 項」
// is far too short to wrap at 320px, so the line never widens), and measuring
// only `.reply-option` for the label mutant (`.reply-option` is `width: 100%`,
// so its own box is pinned to the card whatever the text inside it does).
import { test, expect } from "@playwright/experimental-ct-react";
import { ReplyMultiSelectStory } from "./stories/ReplyMultiSelectStory";

// Narrow and wide: the count line is the only new block-level row, and a row
// that fits a desktop card can still burst a phone one.
const WIDTHS = [320, 1280];

for (const viewport of WIDTHS) {
  test(`${viewport}px: the chips, the 已選 count and the send row all sit inside the card`, async ({
    mount,
    page,
  }) => {
    await page.setViewportSize({ width: viewport, height: 900 });
    const cmp = await mount(<ReplyMultiSelectStory />);

    const card = cmp.getByTestId("card-multi");
    const cardBox = (await card.boundingBox())!;
    const right = cardBox.x + cardBox.width;

    // (1) the long option label wraps inside its chip rather than widening it.
    // Measure the LABEL, not only the chip: `.reply-option` is `width: 100%`,
    // so its own box is pinned to the card no matter what the text does — a
    // guard that stopped at the chip would certify nothing about the wrapping.
    for (const chip of await card.locator(".reply-option").all()) {
      const box = (await chip.boundingBox())!;
      expect(box.x + box.width).toBeLessThanOrEqual(right + 1);
      const label = (await chip.locator(".reply-option__text").boundingBox())!;
      expect(label.x + label.width).toBeLessThanOrEqual(right + 1);
    }

    // (2) the count line — the row this change added — stays inside too.
    const count = card.getByTestId("reply-selected-count");
    await expect(count).toBeVisible();
    const countBox = (await count.boundingBox())!;
    expect(countBox.x + countBox.width).toBeLessThanOrEqual(right + 1);
    // …and it sits BETWEEN the chips and the send row, which is the only place
    // a count of what you are about to send means anything.
    const lastChipBox = (await card.locator(".reply-option").last().boundingBox())!;
    const sendBox = (await card.locator(".chat__send").boundingBox())!;
    expect(countBox.y).toBeGreaterThanOrEqual(lastChipBox.y + lastChipBox.height);
    expect(sendBox.y).toBeGreaterThanOrEqual(countBox.y + countBox.height);

    // (3) nothing dragged the page sideways.
    const overflow = await page.evaluate(
      () => document.documentElement.scrollWidth - document.documentElement.clientWidth,
    );
    expect(overflow).toBeLessThanOrEqual(0);
  });
}

test("ticking a chip changes only its paint, never its box", async ({
  mount,
  page,
}) => {
  // The staged state is a border-colour + background swap. If it ever grows the
  // chip (a thicker border, padding), every chip below it moves as the owner
  // ticks — which is how a multi-select list becomes unclickable.
  await page.setViewportSize({ width: 390, height: 900 });
  const cmp = await mount(<ReplyMultiSelectStory />);
  const card = cmp.getByTestId("card-multi");
  const chip = card.locator(".reply-option").first();

  const before = (await chip.boundingBox())!;
  const bgBefore = await chip.evaluate((el) => getComputedStyle(el).backgroundColor);
  await chip.click();
  await expect(chip).toHaveAttribute("data-selected", "true");
  const after = (await chip.boundingBox())!;
  const bgAfter = await chip.evaluate((el) => getComputedStyle(el).backgroundColor);

  expect(after.width).toBeCloseTo(before.width, 1);
  expect(after.height).toBeCloseTo(before.height, 1);
  expect(after.y).toBeCloseTo(before.y, 1);
  // …and it DID repaint: a guard that only checked the box would stay green
  // with the selected state painting nothing at all.
  expect(bgAfter).not.toBe(bgBefore);
});
