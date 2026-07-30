// HOTSPOT — T-7526 ①: 更改 ＋ 停止 are laid out SIDE BY SIDE on the identity
// card, on both the member and the outsource panel (they share
// `.mp-identity__buttons` in member-detail.css).
//
// What jsdom cannot see: `flex-direction` and `@media`. The old shape was
// `.mp-identity__actions { flex-direction: column }`, and a unit test can only
// pin the DOM (same parent, 更改 first) — flipping the CSS back to `column`
// leaves every vitest assertion green. This spec measures the real boxes.
//
// BOTH viewports are load-bearing. Wide proves the row exists; narrow (≤720px)
// proves the media query adapted with it — the pre-existing narrow rules
// stretched a COLUMN, and left unchanged they would have crushed the new row
// (owner: 不要在窄螢幕擠成一團).
//
// Mutants (documented in docs/design/worker-panel-parity-mutants.md):
//   R1 `.mp-identity__buttons { flex-direction: column }` → both viewports red.
//   R2 drop the ≤720px `.mp-identity__buttons` block → narrow red only.
import { test, expect } from "@playwright/experimental-ct-react";
import { IdentityActionsRowStory } from "./stories/IdentityActionsRowStory";

const VIEWPORTS = [
  { name: "desktop", width: 1200, height: 900 },
  { name: "narrow", width: 375, height: 780 },
];

for (const vp of VIEWPORTS) {
  test(`${vp.name}: 更改 and 停止 sit on ONE row, neither overflowing the card`, async ({
    mount,
    page,
  }) => {
    await page.setViewportSize({ width: vp.width, height: vp.height });
    const cmp = await mount(<IdentityActionsRowStory />);

    const change = cmp.getByTestId("mp-change");
    const stop = cmp.getByTestId("member-action-stop");
    await expect(change).toBeVisible();
    await expect(stop).toBeVisible();

    const c = (await change.boundingBox())!;
    const s = (await stop.boundingBox())!;
    expect(c, "更改 box").not.toBeNull();
    expect(s, "停止 box").not.toBeNull();

    // SIDE-BY-SIDE A — 停止 starts to the RIGHT of 更改, by more than a
    // sub-pixel nudge. A column puts them at the same x.
    expect(s.x).toBeGreaterThan(c.x + c.width / 2);
    // SIDE-BY-SIDE B — they share a row: the tops line up. A column could never
    // satisfy this (the second box would sit a full button-height below).
    expect(Math.abs(s.y - c.y)).toBeLessThan(4);
    // …and they do not overlap: a row that has run out of space and started
    // stacking things on top of each other is not "side by side" either.
    expect(s.x).toBeGreaterThanOrEqual(c.x + c.width - 1);

    // Both stay inside the card at every width.
    const card = (await cmp.getByTestId("story-identity").boundingBox())!;
    expect(c.x).toBeGreaterThanOrEqual(card.x - 1);
    expect(s.x + s.width).toBeLessThanOrEqual(card.x + card.width + 1);

    if (vp.name === "narrow") {
      // 🔴 NOT CRUSHED (owner: 不要在窄螢幕擠成一團). On a phone the card has no
      // room to keep a name row AND a right-hugging button pair, so the ≤720px
      // rules hand the row the full card width and split it between the two.
      //
      // Without those rules the row still exists — `align-items: stretch` on the
      // column widens it — but `justify-content: flex-end` leaves both buttons
      // huddled in the right margin at their natural widths. Every "same row /
      // inside the card" assertion above stays TRUE there, which is exactly why
      // this one has to measure the SPREAD instead.
      const span = s.x + s.width - c.x;
      expect(span, "the pair spans the card, not the right margin").toBeGreaterThan(
        card.width * 0.7,
      );
      // Evenly, not one fat button beside a sliver.
      expect(Math.abs(c.width - s.width)).toBeLessThan(card.width * 0.2);
    }
    expect(c.width).toBeGreaterThan(48);
    expect(s.width).toBeGreaterThan(48);

    // The page itself never gains a horizontal scrollbar because of the row.
    const overflow = await page.evaluate(
      () =>
        document.documentElement.scrollWidth -
        document.documentElement.clientWidth,
    );
    expect(overflow).toBeLessThanOrEqual(0);
  });
}
