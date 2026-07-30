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
import { WorkerRelocateProgressStory } from "./stories/RelocateProgressStory";

const WIDTH = 720;
const SHOT_DIR = process.env.RELOCATE_SHOT_DIR ?? "test-results/relocate-720";
/** Must stay >= RELOCATE_TIMEOUT_MS in useRelocateMachine.tsx. */
const PAST_TIMEOUT_MS = 31_000;

/** ⚠️ WITHOUT THIS THE GUARD CANNOT PASS, EVER — it is not a flake allowance.
 *
 * State 2 is the RELOCATE_TIMEOUT_MS (30s) notice, waited for in real time on
 * purpose (a faked clock replaces page timers wholesale and this guard is about
 * what the owner's browser actually paints). Playwright's per-test budget is
 * 30_000ms by default and playwright-ct.config.ts sets none, so a 30s wait ran
 * out of test before it ran out of timer: both cases failed deterministically,
 * every run, since the day they were written.
 *
 * Sized from the thing being waited for, not padded by feel: the 30s timer, plus
 * the mount/machine-registry warm-up, plus the two screenshots, doubled for a
 * loaded CI box. Keep it strictly greater than PAST_TIMEOUT_MS + the assertion's
 * own 10s slack below, or this comes straight back. */
const TEST_BUDGET_MS = 120_000;
test.describe.configure({ timeout: TEST_BUDGET_MS });

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

// ⚠️ The MEMBER half of this guard MOVED, it was not dropped (T-927a). The member
// panel no longer has a 改機器 button, a 「更換中…」 label, or the 30s timeout notice
// — relocate folded into the unified settings submit — so the old measurement is
// not merely red, it is unrepresentable. Its replacement is
// member-machine-transition.ct.spec.tsx, which measures the row that now carries
// the same class of width risk. The WORKER half below is untouched: the worker
// panel still drives useRelocateMachine with its own 改機器 control.
test(`width ${WIDTH}: WORKER panel 改機器 row holds through 更換中 and the timeout notice`, async ({
  mount,
  page,
}) => {
  await page.setViewportSize({ width: WIDTH, height: 900 });
  const cmp = await mount(<WorkerRelocateProgressStory />);
  await driveAndShoot(cmp, page, "worker", "worker-detail-relocate");
});
