// HOTSPOT — 用了翻頁按鈕之後鍵盤就翻不動了 (T-51 ①).
//
// The bug this guard exists for was found by review, not by the suite, and it
// is invisible to jsdom by construction:
//
//   the paging keys were bound as a React `onKeyDown` on the overlay's portal
//   root, so they only fired while focus was still inside the overlay — and the
//   surest way to lose that focus is to USE the feature. Stepping to the last
//   item disables the very button under the pointer, and a real browser BLURS a
//   focused element when it is disabled (to `<body>`, outside the portal). The
//   keyboard then went dead until the reader reached for the mouse.
//
// jsdom does NOT blur on disable, so the unit tests are green either way; this
// file is the only thing that can tell the two bindings apart. The same trap is
// documented one screen above the change that re-introduced it — see the T-4e95
// note about a caller that disabled its own button and broke the focus restore.
//
// MUTANT (verified red): move the handler back onto the root element as
// `onKeyDown` → assertion (2) fails, because focus is on `<body>` by then.
import { test, expect } from "@playwright/experimental-ct-react";
import { PreviewPagerStory } from "./stories/PreviewPagerStory";

test("the keyboard still pages after the buttons have been used", async ({
  mount,
  page,
}) => {
  await page.setViewportSize({ width: 1100, height: 760 });
  await mount(<PreviewPagerStory />);

  const counter = page.locator(".md-preview__pager-count");
  const next = page.getByRole("button", { name: "下一個" });
  await expect(counter).toHaveText("1 / 3");

  // (1) The buttons walk to the end, and the last step disables the control the
  // pointer is on — the real blur happens here.
  await next.click();
  await expect(counter).toHaveText("2 / 3");
  await next.click();
  await expect(counter).toHaveText("3 / 3");
  await expect(next).toBeDisabled();

  // (2) CORE red→green: the keyboard is still alive. Bound to the overlay root,
  // this key went nowhere — `document.activeElement` is `<body>` after the
  // disable, and the root never sees it.
  expect(
    await page.evaluate(() => document.activeElement?.tagName ?? ""),
    "the disable really did drop focus out of the overlay (else this guard proves nothing)",
  ).toBe("BODY");
  await page.keyboard.press("ArrowLeft");
  await expect(counter).toHaveText("2 / 3");
  await page.keyboard.press("ArrowLeft");
  await expect(counter).toHaveText("1 / 3");
});

test("a zoomed image keeps the arrow keys for panning", async ({
  mount,
  page,
}) => {
  await page.setViewportSize({ width: 1100, height: 760 });
  await mount(<PreviewPagerStory start={1} />);

  const counter = page.locator(".md-preview__pager-count");
  await expect(counter).toHaveText("2 / 3");
  await page.getByRole("button", { name: "放大" }).click();
  await expect(page.getByText("125%")).toBeVisible();

  await page.keyboard.press("ArrowRight");
  await page.keyboard.press("ArrowLeft");
  await expect(
    counter,
    "zoomed, the arrows belong to the image — paging must not take them",
  ).toHaveText("2 / 3");

  // …and the buttons still work, which is what makes that refusal affordable.
  await page.getByRole("button", { name: "下一個" }).click();
  await expect(counter).toHaveText("3 / 3");
});
