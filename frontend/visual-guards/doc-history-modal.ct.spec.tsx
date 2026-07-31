// HOTSPOT — the retained-revision reader's two purely-visual contracts
// (T-1f39). The vitest suite pins the MODEL (which pane, which rows, which
// strings). Neither of the facts below survives jsdom:
//
//   ① IT MUST ACTUALLY OVERLAY. jsdom has no stacking context and no hit
//      testing: a panel painted under the page, or a scrim the editor behind it
//      still receives clicks through, is completely invisible there. Measured
//      as hit testing (what is on top at the panel's own centre, and at a point
//      over the page behind it), not as a CSS string.
//   ② THE MODAL SCROLLS ITSELF. A 40-paragraph revision must scroll inside
//      .doc-hist-modal__body, and the diff's unbreakable token must scroll
//      inside .diff-view__scroll — with the PAGE left unscrollable in both
//      directions (the long-token rule, T-d451). All layout; jsdom is blind.
//
// MUTANTS (each RUN and verified red — see docs/design/T-1f39-…md):
//   drop `position: fixed` from .doc-hist-modal            → ① red (page shows through)
//   drop `overflow-y: auto` from .doc-hist-modal__body     → ② red (page grows)
//   drop `min-height: 0` from .doc-hist-modal__panel       → ② red (body never scrolls)
import { test, expect } from "@playwright/experimental-ct-react";
import { DocumentHistoryModalStory } from "./stories/DocumentHistoryModalStory";

test("the modal covers the page behind it, and the page cannot scroll", async ({
  mount,
  page,
}) => {
  await page.setViewportSize({ width: 1024, height: 800 });
  const cmp = await mount(<DocumentHistoryModalStory />);

  const panel = cmp.locator(".doc-hist-modal__panel");
  await expect(panel).toBeVisible();
  const box = (await panel.boundingBox())!;
  expect(box).not.toBeNull();
  // Inside the viewport on both axes — a panel taller than the screen would
  // put its own footer (where restore lives) out of reach.
  expect(box.x).toBeGreaterThanOrEqual(0);
  expect(box.x + box.width).toBeLessThanOrEqual(1024 + 1);
  expect(box.y).toBeGreaterThanOrEqual(0);
  expect(box.y + box.height).toBeLessThanOrEqual(800 + 1);

  // ① HIT TESTING, the real question: at the panel's centre the topmost node
  // belongs to the modal, and over the page behind it the topmost node is the
  // SCRIM — not the editor, which must not be clickable while this is open.
  const atPanel = await page.evaluate(
    ({ x, y }) =>
      (document.elementFromPoint(x, y) as HTMLElement)?.closest(
        ".doc-hist-modal__panel"
      ) !== null,
    { x: box.x + box.width / 2, y: box.y + box.height / 2 }
  );
  expect(atPanel, "the panel must be the topmost thing at its own centre").toBe(
    true
  );
  const overPage = await page.evaluate(() => {
    const el = document.elementFromPoint(8, 8) as HTMLElement | null;
    return {
      onScrim: el?.classList.contains("doc-hist-modal") ?? false,
      onEditor: el?.closest('[data-testid="page-behind"]') !== null,
    };
  });
  expect(overPage.onScrim, "the scrim must cover the page corner").toBe(true);
  expect(overPage.onEditor, "the editor behind must not be reachable").toBe(false);

  // The 1200px page behind is covered, not extended: nothing about opening a
  // reader may hand the document a scrollbar of its own.
  const pageOverX = await page.evaluate(
    () =>
      document.scrollingElement!.scrollWidth -
      document.scrollingElement!.clientWidth
  );
  expect(pageOverX, `page must not scroll sideways (got +${pageOverX}px)`).toBeLessThanOrEqual(1);
});

test("a long revision scrolls inside the modal body, not the page", async ({
  mount,
  page,
}) => {
  await page.setViewportSize({ width: 1024, height: 800 });
  const cmp = await mount(<DocumentHistoryModalStory />);

  const body = cmp.locator(".doc-hist-modal__body");
  await expect(body).toHaveCSS("overflow-y", "auto");
  // REAL overflow, not merely declared — a body that never overflows would
  // make the `auto` above a vacuous assertion.
  const over = await body.evaluate(
    (n: HTMLElement) => n.scrollHeight - n.clientHeight
  );
  expect(over, `the long revision must overflow the body (got +${over}px)`).toBeGreaterThan(50);
  // …and it is genuinely reachable: scrolling the body moves the body.
  await body.evaluate((n: HTMLElement) => n.scrollBy(0, 200));
  expect(await body.evaluate((n: HTMLElement) => n.scrollTop)).toBeGreaterThan(100);

  // The header (with the pane toggle) and the footer (with restore) stay put
  // while the body scrolls — that is what makes the body the scroller.
  await expect(cmp.locator(".doc-hist-modal__tabs")).toBeInViewport();
  await expect(cmp.locator('[data-testid="doc-history-modal-restore"]')).toBeInViewport();
});

test("360px: the diff pane's unbreakable line scrolls inside the diff", async ({
  mount,
  page,
}) => {
  await page.setViewportSize({ width: 375, height: 800 });
  const cmp = await mount(<DocumentHistoryModalStory />);

  await cmp.locator('[data-testid="doc-history-pane-diff"]').click();
  const scroll = cmp.locator(".diff-view__scroll");
  await expect(scroll).toBeVisible();

  // The owner's symptom class: the PAGE drags sideways.
  const pageOver = await page.evaluate(
    () =>
      document.scrollingElement!.scrollWidth -
      document.scrollingElement!.clientWidth
  );
  expect(pageOver, `page must have no horizontal scroll (got +${pageOver}px)`).toBeLessThanOrEqual(1);

  // Nor does the modal body take it — the diff owns its own horizontal scroll.
  const bodyOver = await cmp
    .locator(".doc-hist-modal__body")
    .evaluate((n: HTMLElement) => n.scrollWidth - n.clientWidth);
  expect(bodyOver, `the modal body must not scroll sideways (got +${bodyOver}px)`).toBeLessThanOrEqual(1);

  const scrollOver = await scroll.evaluate(
    (n: HTMLElement) => n.scrollWidth - n.clientWidth
  );
  expect(
    scrollOver,
    `the long line must scroll INSIDE the diff (got +${scrollOver}px)`
  ).toBeGreaterThan(1);
});
