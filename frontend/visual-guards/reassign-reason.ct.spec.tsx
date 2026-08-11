// GUARD (T-b9f6) — the 轉派 refusal reason must FIT, at phone and desktop width.
//
// Contract: the dialog now prints the server's own sentence instead of a fixed
// 10-character string. The longest of those sentences is ~110 characters, so
// the error box has to wrap inside the modal instead of pushing past it or
// giving the page a sideways scrollbar.
//
// jsdom can't see this: it has no layout engine, so "the text is present" is the
// only thing the vitest suite can assert — a box overflowing its modal looks
// identical there. This measures real layout in real Chromium at 390 (phone) and
// 1280 (desktop).
//
// MUTANT (verified): give .confirm-modal__error `white-space: nowrap` → the
// long sentence stops wrapping → the containment assertion goes red at 390.
//
// The sentence lives in its own module (the avatarKindImages precedent): a CT
// story module that exports BOTH a component and a value trips the component
// transform ("Identifier … has already been declared").
import { test, expect } from "@playwright/experimental-ct-react";
import { TaskReassignErrorStory } from "./stories/TaskReassignErrorStory";
import { LONGEST_REFUSAL } from "./stories/reassignRefusals";

for (const width of [1280, 390]) {
  test(`width ${width}: the server's refusal reason wraps inside the dialog`, async ({
    mount,
    page,
  }) => {
    await page.setViewportSize({ width, height: 900 });
    const cmp = await mount(<TaskReassignErrorStory />);

    // Drive a real submit: 轉外包 → pick the first machine → confirm.
    await cmp.getByTestId("reassign-kind-outsource").click();
    const machine = page.locator('[data-testid^="reassign-machine-"]').first();
    await expect(machine).toBeVisible();
    await machine.click();
    await cmp.getByTestId("reassign-confirm").click();

    // (1) the reason itself is on screen — not our generic fallback. Without
    // this, the containment assertions below would pass on an empty box.
    const err = page.locator(".confirm-modal__error");
    await expect(err).toBeVisible();
    await expect(err).toHaveText(LONGEST_REFUSAL);

    // (2) the error box stays inside the modal box at both widths.
    const box = (await page.locator(".confirm-modal__box").boundingBox())!;
    const errBox = (await err.boundingBox())!;
    expect(box, "modal box").not.toBeNull();
    expect(errBox, "error box").not.toBeNull();
    expect(errBox.x).toBeGreaterThanOrEqual(box.x - 1);
    expect(errBox.x + errBox.width).toBeLessThanOrEqual(box.x + box.width + 1);

    // (3) …and the page never gains a horizontal scrollbar because of it.
    const overflow = await page.evaluate(
      () =>
        document.scrollingElement!.scrollWidth -
        document.scrollingElement!.clientWidth
    );
    expect(
      overflow,
      `[${width}px] page must have no horizontal scroll (got +${overflow}px)`
    ).toBeLessThanOrEqual(1);
  });
}
