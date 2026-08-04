// HOTSPOT — T-5e79 (owner 2026-08-04 截圖): on the role-journal Insight card the
// 「預設」 pill and the 「編輯」 button opposite it both had their two CJK glyphs
// broken onto TWO stacked lines that spilled out of the fixed-height box —
// the pill looked like a burst capsule with a character above and below it.
//
// MEASURED on the pre-fix sheet (this same story, real Chromium):
//   width  badge lines  badge text height / box 19px   編輯 lines / text height
//   320    2            29.5px, 6px above + 4.5px below   2   32px in a 30px box
//   360    2            29.5px, 6px above + 4.5px below   2   32px in a 30px box
//   375    2            29.5px, 6px above + 4.5px below   2   32px in a 30px box
//   390    2            29.5px, 6px above + 4.5px below   2   32px in a 30px box
//   414+   1            13px, inside the pill             1   16px, inside
// So this is a NARROW-WIDTH-ONLY defect: it is structurally invisible at desktop
// widths, and it is also structurally invisible to the vitest/jsdom suite, which
// applies no layout engine — `insight-default-badge` is in the DOM either way.
//
// WHAT IS ASSERTED IS GEOMETRY THE OWNER CAN SEE, not CSS property strings:
//   (1) the label occupies exactly ONE line box, and
//   (2) every one of its line boxes stays INSIDE the element's own border box.
// A `white-space` value read back from getComputedStyle would be satisfied by a
// property that is set but out-cascaded, or by a fix that stops the wrap and
// still bursts the box; these two do not.
//
// Both widths are exercised on purpose. The narrow ones are where the owner
// saw it; the wide one is the control that says the fix did not simply move the
// breakage somewhere else.
//
// MUTANT (documented, and re-run for this ticket): delete `white-space: nowrap`
// from `.set-badge` in settings.css → assertion (1) for the badge reddens at
// 375/390 with `expected 1, received 2`. Same for `.doc-btn` → the 編輯
// assertions redden at the same widths. See the ticket report for the run output.
import { test, expect } from "@playwright/experimental-ct-react";
import type { Locator } from "@playwright/test";
import { InsightBadgeNarrowStory } from "./stories/InsightBadgeNarrowStory";

/** Line boxes + vertical spill of an element's own text, measured with a Range
 * (one client rect per line box) against the element's border box. */
async function textGeometry(el: Locator) {
  return await el.evaluate((node) => {
    const box = node.getBoundingClientRect();
    const range = document.createRange();
    range.selectNodeContents(node);
    const rects = Array.from(range.getClientRects());
    return {
      lines: rects.length,
      spillAbove: box.top - Math.min(...rects.map((r) => r.top)),
      spillBelow: Math.max(...rects.map((r) => r.bottom)) - box.bottom,
    };
  });
}

// 375 / 390 = the phone widths the rest of this suite treats as the owner's
// (nav-tabs-narrow, worker-detail-header-label, …) and where the defect was
// measured. 320 = the narrowest phone still in use, and the only width where
// the fix needed member-detail.css's ≤359px wrap valve to stay inside the card.
// 1040 = the desktop content column's max width — the control that says the
// fix did not move the breakage somewhere else.
for (const width of [320, 375, 390, 1040]) {
  test(`width ${width}: 預設 badge and 編輯 button keep their labels on one line, inside their own box`, async ({
    mount,
    page,
  }) => {
    await page.setViewportSize({ width, height: 900 });
    const cmp = await mount(<InsightBadgeNarrowStory />);

    // The badge only renders for a role whose insight is BOTH is_default and
    // non-empty; if this ever stops resolving, the guard must fail loudly
    // rather than silently measure nothing.
    const badge = cmp.getByTestId("insight-default-badge");
    await expect(badge).toBeVisible();
    await expect(badge).toHaveText("預設");

    const badgeGeo = await textGeometry(badge);
    expect(badgeGeo.lines, "預設 badge label line boxes").toBe(1);
    expect(badgeGeo.spillAbove, "預設 label spilling above the pill").toBeLessThanOrEqual(0.5);
    expect(badgeGeo.spillBelow, "預設 label spilling below the pill").toBeLessThanOrEqual(0.5);

    const editLabel = cmp.locator(".doc-btn--edit span");
    await expect(editLabel).toHaveText("編輯");
    const editGeo = await textGeometry(editLabel);
    expect(editGeo.lines, "編輯 button label line boxes").toBe(1);

    // The button's own 30px box must still contain the label it was sized for.
    const editBtn = cmp.locator(".doc-btn--edit");
    const btnBox = (await editBtn.boundingBox())!;
    const labelBox = (await editLabel.boundingBox())!;
    expect(labelBox.height, "編輯 label height vs its 30px button").toBeLessThanOrEqual(
      btnBox.height
    );

    // The no-shrink rules must not have bought the fix with an overflow
    // somewhere else: the header row, the card, and the page all stay put.
    const spill = await page.evaluate(() => {
      const head = document.querySelector(".mp-lessons__head")!;
      const card = document.querySelector(".mp-insight")!;
      return {
        head: head.scrollWidth - head.clientWidth,
        card: card.scrollWidth - card.clientWidth,
        page:
          document.documentElement.scrollWidth -
          document.documentElement.clientWidth,
      };
    });
    expect(spill.head, "header row horizontal overflow").toBeLessThanOrEqual(1);
    expect(spill.card, "insight card horizontal overflow").toBeLessThanOrEqual(1);
    expect(spill.page, "page horizontal overflow").toBeLessThanOrEqual(1);
  });
}
