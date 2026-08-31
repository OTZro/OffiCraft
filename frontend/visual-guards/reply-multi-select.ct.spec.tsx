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

    // (2b) the T-40b rows: the hint line above the options and the chip mark
    // that replaced the 1/2/3 ordinal. MEASURE THE MARK ITSELF — `.reply-option`
    // is `width: 100%`, so an assertion that stopped at the chip would certify
    // nothing about the box inside it (the mistake this file already records
    // once for the label).
    const hint = card.getByTestId("reply-mode-hint");
    await expect(hint).toBeVisible();
    const hintBox = (await hint.boundingBox())!;
    expect(hintBox.x + hintBox.width).toBeLessThanOrEqual(right + 1);
    expect(hintBox.height, "the hint must actually occupy a row").toBeGreaterThan(0);
    // …and it sits ABOVE the first chip: a warning under the thing it warns
    // about is read after the click it was meant to prevent.
    const firstChipBox = (await card.locator(".reply-option").first().boundingBox())!;
    expect(firstChipBox.y).toBeGreaterThanOrEqual(hintBox.y + hintBox.height);

    for (const mark of await card.locator(".reply-option__mark").all()) {
      const box = (await mark.boundingBox())!;
      expect(box.width, "the mark must be a real box, not a collapsed span").toBeGreaterThan(10);
      expect(box.height).toBeGreaterThan(10);
      expect(box.x + box.width).toBeLessThanOrEqual(right + 1);
    }

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

// ── T-40b ──────────────────────────────────────────────────────────────────
// Three paint facts jsdom cannot state, each one an owner report:
//
//   * 「現在AI建議的，跟我選的，根本看不出來」 — `.reply-option--ai` and
//     `.reply-option--selected` painted the SAME `--color-accent-cta-bg`
//     background. Accent now means ONE thing: what you chose.
//   * 「我UI也看不出來是單選還是多選」 — the chip mark's SHAPE is the answer,
//     and a shape is a computed border-radius, not a class name.
//
// MUTANTS (all measured here):
//   * put `background: var(--color-accent-cta-bg)` back on `.reply-option--ai`
//     → the "AI chip paints like a plain chip" assertion red.
//   * swap the `--check`/`--radio` border-radius rules → the shape pair red at
//     both cards.
//   * delete the `.reply-tag--ai` span from ReplyCardBody → the tag assertion
//     red. (The option TEXT deliberately never contains the words 「AI 建議」,
//     so `toContainText` on the chip could never stand in for the tag — that
//     exact false green is on this repo's record.)
test("the AI pick is told by its tag alone, and the card kind by the mark's shape", async ({
  mount,
  page,
}) => {
  await page.setViewportSize({ width: 390, height: 1200 });
  const cmp = await mount(<ReplyMultiSelectStory />);
  const multi = cmp.getByTestId("card-multi");
  const single = cmp.getByTestId("card-single");

  const bg = (loc) => loc.evaluate((el) => getComputedStyle(el).backgroundColor);
  const radius = (loc) => loc.evaluate((el) => getComputedStyle(el).borderTopLeftRadius);

  // The story's ai_pick sits on the SECOND option; chip 0 carries nothing.
  const aiChip = multi.locator(".reply-option").nth(1);
  const plainChip = multi.locator(".reply-option").nth(2);
  await expect(
    aiChip.locator(".reply-tag--ai"),
    "the tag is the ONLY carrier of the recommendation",
  ).toHaveCount(1);
  await expect(
    aiChip.locator(".reply-option__text"),
    "the option wording must not spell the tag out — that made a tag assertion pass with the tag deleted",
  ).not.toContainText("AI 建議");
  expect(
    await bg(aiChip),
    "the AI pick must paint exactly like an ordinary chip",
  ).toBe(await bg(plainChip));

  // …and the accent paint still belongs to the owner's own tick.
  await plainChip.click();
  await expect(plainChip).toHaveAttribute("data-selected", "true");
  expect(
    await bg(plainChip),
    "ticking is what accent means now",
  ).not.toBe(await bg(aiChip));

  // Shape: square-ish tick box on multi, circle on single.
  const multiMark = multi.locator(".reply-option__mark").first();
  const singleMark = single.locator(".reply-option__mark").first();
  const multiBox = (await multiMark.boundingBox())!;
  const singleBox = (await singleMark.boundingBox())!;
  const multiR = parseFloat(await radius(multiMark));
  const singleR = parseFloat(await radius(singleMark));
  expect(multiR, "a tick box is not a circle").toBeLessThan(multiBox.width / 2 - 1);
  expect(singleR, "a radio is a circle").toBeGreaterThanOrEqual(singleBox.width / 2 - 0.5);
});

// 🔴 GREYSCALE. Accent now means one thing — what YOU chose — which puts the
// whole "did my tap take?" signal on colour unless the mark says it in SHAPE
// too. Measured on the owner's cockpit BEFORE this change: `--ai` and
// `--ai --selected` had the same background, the same text colour, no outline,
// no box-shadow and no ::before/::after — they differed ONLY by border alpha
// 0.35 → 1.0. Someone who cannot separate those two oranges had no signal at
// all, and 「按下去無效」 was an honest report.
//
// So: the ticked mark must carry a GLYPH — a real ::after box — that the
// unticked one does not.
//
// MUTANT: delete the `.reply-option__mark--on::after` rules (leaving --on as a
// pure background/border swap) → this test red at both card kinds; the paint
// test above stays green, which is why the two are separate.
test("ticked and unticked are told apart by shape, not only by colour", async ({
  mount,
  page,
}) => {
  await page.setViewportSize({ width: 390, height: 1200 });
  const cmp = await mount(<ReplyMultiSelectStory />);

  const glyphArea = (loc) =>
    loc.evaluate((el) => {
      const a = getComputedStyle(el, "::after");
      return (parseFloat(a.width) || 0) * (parseFloat(a.height) || 0);
    });

  // The MULTI card through the real interaction: tick it and watch the glyph
  // appear.
  const multiMark = cmp
    .getByTestId("card-multi")
    .locator(".reply-option")
    .first()
    .locator(".reply-option__mark");
  expect(await glyphArea(multiMark), "an unticked tick box draws no glyph").toBe(0);
  await cmp.getByTestId("card-multi").locator(".reply-option").first().click();
  expect(
    await glyphArea(multiMark),
    "a ticked tick box must draw a glyph — colour cannot be the only signal",
  ).toBeGreaterThan(0);

  // The RADIO's ON state cannot be reached by clicking here: a single card
  // ANSWERS on the click, so its lit mark belongs to the standing answer's
  // review (`currentIdxs`). Apply that same class and measure the rule the
  // review path uses — the alternative, asserting nothing about the radio, is
  // how the shape signal would come back colour-only on exactly the card kind
  // that now answers in one tap.
  const singleMark = cmp
    .getByTestId("card-single")
    .locator(".reply-option")
    .first()
    .locator(".reply-option__mark");
  expect(await glyphArea(singleMark), "an unpicked radio draws no dot").toBe(0);
  await singleMark.evaluate((el) => el.classList.add("reply-option__mark--on"));
  expect(
    await glyphArea(singleMark),
    "a picked radio must draw its inner dot",
  ).toBeGreaterThan(0);
});
