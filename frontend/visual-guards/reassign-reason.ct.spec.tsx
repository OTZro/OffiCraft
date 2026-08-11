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
// MUTANT (measured, both directions): give .confirm-modal__error
// `white-space: nowrap` → 2 red (both widths), each naming the hidden overflow.
// 🔴 The FIRST version of this file asserted only bounding rects and that
// mutant passed 2/2 — the rects do not move, the text just gets clipped. The
// assertion with the discriminating power is the content-overflow one below;
// keep it, and do not "simplify" back to rects.
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

    // (2) 🔴 the sentence WRAPS — measured as content overflow, not as rects.
    // Rects alone have NO discriminating power here and that is measured, not
    // assumed: under `white-space: nowrap` at 390px every box keeps its exact
    // geometry (error 304px inside a 350px modal, page scroll 0) while the text
    // runs 380px past the element's own content box and is simply cut off. The
    // first version of this guard asserted only rects and passed that mutant.
    const overflowIn = await page.evaluate(() => {
      const of = (sel: string) => {
        const el = document.querySelector(sel) as HTMLElement;
        return el.scrollWidth - el.clientWidth;
      };
      const ofY = (sel: string) => {
        const el = document.querySelector(sel) as HTMLElement;
        return el.scrollHeight - el.clientHeight;
      };
      return {
        error: of(".confirm-modal__error"),
        box: of(".confirm-modal__box"),
        modal: of(".confirm-modal"),
        errorY: ofY(".confirm-modal__error"),
      };
    });
    expect(
      overflowIn.error,
      `[${width}px] the reason must wrap, not run off its own box (+${overflowIn.error}px hidden)`
    ).toBeLessThanOrEqual(1);
    expect(overflowIn.box, `[${width}px] modal box`).toBeLessThanOrEqual(1);
    expect(overflowIn.modal, `[${width}px] modal root`).toBeLessThanOrEqual(1);
    // 🔴 …and VERTICALLY too. Independent review measured this: with only the
    // horizontal assertion, `max-height: 22px; overflow-y: hidden` clipped two
    // of the three lines and this file stayed 2/2 GREEN. Vertical is the
    // direction this change actually pushes on (10 characters became 3 lines),
    // so leaving it out was the same mistake as the rect version, one axis over.
    expect(
      overflowIn.errorY,
      `[${width}px] the reason must not be clipped vertically (+${overflowIn.errorY}px hidden)`
    ).toBeLessThanOrEqual(1);

    // …and it also stays inside the modal box's rect (the plain containment
    // half — cheap, and it catches a reason placed outside the panel).
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
