// HOTSPOT — 任務卡描述編輯面的版面約束 (T-e271).
//
// WHY THIS EXISTS: the description editor added a textarea and a flex ROW of
// controls (儲存 / 取消 / 版本紀錄) inside the task card. jsdom cannot see any
// of it — no layout engine, offsetWidth 0 — so the component tests that cover
// the editor's BEHAVIOUR say nothing about whether it fits a phone. Without
// this file the new CSS has no guard at all, which is a thing worth being
// explicit about rather than discovering later.
//
// WHAT THIS GUARD IS MEASURED TO DETECT — every line below is a number that was
// actually taken, because the first draft of this header claimed a mutant that
// turned out NOT to redden, and a confident but unmeasured "verified" is what
// stops the next reader from re-measuring:
//
//   ✅ MUTANT E (verified red): `.task-card__desc-input { width: 900px }`
//      (i.e. losing `width:100%` / `box-sizing:border-box`) → assertion (1)
//      fails at 390px, `got +525px` of page horizontal scroll. So the editor
//      IS pinned against bursting a phone viewport — the owner's original
//      T-4974 symptom, reachable again through a different element.
//
//   ❌ MUTANT D (measured, did NOT redden): deleting `flex-wrap: wrap` from
//      `.task-card__desc-actions` leaves all three tests green. The reason is
//      simply that 儲存 / 取消 / 版本紀錄 currently FIT one 390px line, so the
//      wrap is defensive rather than load-bearing today. Recorded as a
//      NEGATIVE result on purpose: without it, a reader would reasonably
//      assume this file pins that rule, and would be wrong. Add a control and
//      the wrap becomes load-bearing — at which point this guard starts
//      covering it for free.
//
// DELIBERATELY NOT CLAIMED: the hint is static i18n prose with no unbreakable
// token, so its `overflow-wrap` rule is NOT proven here — an assertion aimed at
// it would look like coverage while being unable to fail. It is measured for
// FIT like every other surface, and nothing more is claimed for it.
import { test, expect } from "@playwright/experimental-ct-react";
import { TaskCardDescEditorStory } from "./stories/TaskCardDescEditorStory";

async function mountWithEditorOpen(mount: any, page: any, width: number) {
  await page.setViewportSize({ width, height: 1000 });
  const cmp = await mount(<TaskCardDescEditorStory />);
  await cmp.locator(".task-card__head").first().click();
  // The read-mode affordance must be there before it can be opened — if the
  // edit control ever stops rendering, this fails HERE with a clear message
  // rather than leaving the assertions below to pass over an absent editor.
  await expect(cmp.getByTestId("task-desc-edit")).toBeVisible();
  await cmp.getByTestId("task-desc-edit").click();
  await expect(cmp.getByTestId("task-desc-editor")).toBeVisible();
  return cmp;
}

async function assertEditorFits(page: any, width: number) {
  // (1) CORE red→green: opening the editor must never make the page scroll
  // sideways. This is the assertion MUTANT E reddens (+525px at 390px); see
  // the header for what it does and does NOT pin.
  const overflow = await page.evaluate(
    () =>
      document.scrollingElement!.scrollWidth -
      document.scrollingElement!.clientWidth
  );
  expect(
    overflow,
    `[${width}px] page must have no horizontal scroll with the description editor open (got +${overflow}px)`
  ).toBeLessThanOrEqual(1);

  // (2) each editor surface fits its own box. The -2 sentinel is scored as a
  // FAILURE, not a pass — the lesson taken verbatim from
  // taskcard-longtoken-wrap.ct.spec.tsx, where a rotted selector would have
  // retired an assertion silently instead of reddening.
  for (const sel of [
    ".task-card__desc-input",
    ".task-card__desc-actions",
    ".task-card__desc-hint",
  ]) {
    const over = await page.evaluate((s: string) => {
      const el = document.querySelector(s) as HTMLElement | null;
      return el ? el.scrollWidth - el.clientWidth : -2;
    }, sel);
    expect(over, `[${width}px] ${sel} missing (never rendered)`).not.toBe(-2);
    expect(over, `[${width}px] ${sel} content overflow`).toBeLessThanOrEqual(1);
  }
}

test("description editor fits a 390px phone with no page hscroll", async ({
  mount,
  page,
}) => {
  await mountWithEditorOpen(mount, page, 390);
  await assertEditorFits(page, 390);
});

test("description editor fits a 1280px desktop", async ({ mount, page }) => {
  // Width is an INPUT dimension, so both ends are asserted — a rule that only
  // holds at one width is a rule that will break at the other.
  await mountWithEditorOpen(mount, page, 1280);
  await assertEditorFits(page, 1280);
});

test("the editor is seeded with the stored description", async ({
  mount,
  page,
}) => {
  // Non-vacuity for the two tests above: they measure an editor that is
  // supposed to be holding real text. An editor that opened EMPTY would fit
  // any viewport trivially and both would pass while measuring nothing.
  const cmp = await mountWithEditorOpen(mount, page, 390);
  await expect(cmp.getByTestId("task-desc-input")).toHaveValue(
    /twin\(desired_state/
  );
});
