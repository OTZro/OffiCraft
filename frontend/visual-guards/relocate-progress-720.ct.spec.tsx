// HOTSPOT — T-e0e3: the 改機器 control gained two things that only exist as
// LAYOUT: a wider in-progress button label (「更換中…」 replaces 「編輯」) and a
// notice line under it (timeout / failure). Both live inside the 機器 label row
// (`.mp-field__head`: flex, space-between), i.e. the row that also holds the 機器
// label itself — so "does the row still hold at the mobile breakpoint" is a
// question jsdom structurally cannot answer (no flex, no @media, every width 0).
// The vitest suite covers the state machine; this covers the pixels.
//
// 720px is the repo's mobile breakpoint (frontend/CLAUDE.md).
//
// It also writes the screenshots the owner asked to see. Path is overridable so
// nothing lands in the repo tree by default:
//   RELOCATE_SHOT_DIR=/abs/dir npx playwright test -c playwright-ct.config.ts \
//     relocate-progress-720
import { test, expect } from "@playwright/experimental-ct-react";
import type { Locator, Page } from "@playwright/test";
import {
  MemberRelocateProgressStory,
  WorkerRelocateProgressStory,
} from "./stories/RelocateProgressStory";

const WIDTH = 720;
const SHOT_DIR = process.env.RELOCATE_SHOT_DIR ?? "test-results/relocate-720";
/** Must stay >= RELOCATE_TIMEOUT_MS in useRelocateMachine.tsx. */
const PAST_TIMEOUT_MS = 31_000;

// This guard intentionally waits for the production 30s timeout. Playwright's
// default per-test ceiling is also 30s, which made the assertion unreachable.
test.setTimeout(60_000);

/** The row must not push anything past the viewport, and the page must not gain
 * a horizontal scrollbar. Measured on the ROW ELEMENT, not a parent (T-d451). */
async function expectRowFits(page: Page, row: Locator) {
  const box = await row.boundingBox();
  expect(box, "機器 label row box").not.toBeNull();
  expect(box!.x + box!.width).toBeLessThanOrEqual(WIDTH + 1);
  const overflow = await page.evaluate(
    () =>
      document.documentElement.scrollWidth -
      document.documentElement.clientWidth,
  );
  expect(overflow).toBeLessThanOrEqual(1);
}

/** Drive one panel through 更換中 → timeout, asserting the row holds and
 * capturing the owner-facing screenshot at each state. */
async function driveAndShoot(
  cmp: Locator,
  page: Page,
  panel: "member" | "worker",
  testId: string,
  progressObservable = true,
) {
  const button = cmp.getByTestId(testId);
  // The machine registry has to report an online machine before the control is
  // live; without this the guard would silently measure a disabled button.
  await expect(button).toBeEnabled({ timeout: 10_000 });
  // `has:` must be relative to the outer locator, so both come from `page`
  // (a locator rooted at the mount wrapper resolves to nothing here).
  const row = page
    .locator(".mp-field__head")
    .filter({ has: page.getByTestId(testId) });

  // ── state 1: 更換中… ──────────────────────────────────────────────────────
  await button.click();
  // 0/1/2+ online rule: with 2+ machines online the click opens the picker and
  // the relocate fires on confirm. The mock registry's size is not this guard's
  // business, so handle either path.
  const confirm = cmp.getByTestId("machine-picker-confirm");
  if (await confirm.isVisible()) await confirm.click();
  if (!progressObservable) {
    await expect(button).toContainText("編輯");
    await expect(cmp.getByTestId(`${testId}-sent`)).toBeVisible();
    await expectRowFits(page, row);
    await page.screenshot({ path: `${SHOT_DIR}/${panel}-panel-sent-720.png` });
    return;
  }
  await expect(button).toContainText("更換中");
  await expect(button).toBeDisabled();
  await expectRowFits(page, row);
  await page.screenshot({
    path: `${SHOT_DIR}/${panel}-panel-relocating-720.png`,
  });

  // ── state 2: the timeout notice, in the same row ─────────────────────────
  // Real time, not a faked clock: the clock API replaces page timers wholesale
  // and this guard is about what the owner's browser actually paints.
  const notice = cmp.getByTestId(`${testId}-notice`);
  await expect(notice).toBeVisible({ timeout: PAST_TIMEOUT_MS + 10_000 });
  await expect(button).toBeEnabled(); // retryable
  await expectRowFits(page, row);
  await page.screenshot({
    path: `${SHOT_DIR}/${panel}-panel-timeout-720.png`,
  });
}

test(`width ${WIDTH}: MEMBER panel 改機器 row holds through 更換中 and the timeout notice`, async ({
  mount,
  page,
}) => {
  await page.setViewportSize({ width: WIDTH, height: 900 });
  const cmp = await mount(<MemberRelocateProgressStory />);
  await driveAndShoot(cmp, page, "member", "mp-relocate");
});

test(`width ${WIDTH}: WORKER panel 改機器 row holds through 更換中 and the timeout notice`, async ({
  mount,
  page,
}) => {
  await page.setViewportSize({ width: WIDTH, height: 900 });
  const cmp = await mount(<WorkerRelocateProgressStory />);
  // Worker wire data has no raw observed machine id yet. Its control must show
  // a truthful sent signal rather than a progress state that can only time out.
  await driveAndShoot(cmp, page, "worker", "worker-detail-relocate", false);
});
