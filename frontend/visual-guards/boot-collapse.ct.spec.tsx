// HOTSPOT — 設定 › 角色誌 › 啟動程序 on a phone (T-6278).
//
// THE DEFECT THIS MEASURES WAS REPORTED FROM A PHONE, AND IT IS A GEOMETRY
// DEFECT: both documents were rendered in full, stacked, so 啟動程序（Codex
// CLI）sat thousands of pixels below the fold. The owner scrolled to the end of
// the first card, read the card's bottom edge as the end of the page, and
// reported the second document as missing. Nothing was broken in the DOM, which
// is exactly why jsdom cannot see it: the sibling test
// (src/components/SettingsPage.boot-collapse.test.tsx) pins the STATE — no body
// renders until a heading is pressed — and this file pins the consequence the
// owner actually met, that BOTH DOCUMENTS FIT ON ONE SCREEN.
//
// MUTANTS, measured on this browser and reported as measured:
//   drop `collapsible` from either BootDocPage in SettingsPage's boot view
//     → RED at every width, all four tests.
//   drop the `.settings-stack` wrapper (back to a bare fragment)
//     → RED at every width, all four tests: codex heading y = 860 on an
//       844-tall screen. 🔴 This is the half that collapsing does NOT buy —
//       each card is its own full-height scroller, so the second document
//       starts a screen below the first however short the first one is.
// ⚠️ THE COLLAPSE TESTS HAVE NO MUTANT, and what they pin is NOT what an
// earlier version of this header claimed. It said a collapse never moves the
// pressed heading; the independent review measured otherwise and it is now
// pinned here in BOTH directions:
//   * collapsing the FIRST card really does not move its heading (its own
//     heading is at the top of everything the collapse removes);
//   * collapsing the LAST card from a scrolled-to-the-bottom page moves it
//     389.9 → 753.9 (364px, same at 390 and 1040) — the page becomes shorter
//     than the scroll position and the browser clamps scrollTop. No scroll
//     correction can undo that (it would need scrollTop 7330 against a new
//     maximum of 6966), which is why DocCard carries none. The test asserts the
//     heading stays ON SCREEN, which is the part that is actually defensible.
// So the owner's rule for this family (T-6630:「畫面也應該停在原處，直接向上收
// 合」) holds for the first card and is BOUNDED for the last one. Fixing that
// properly is a layout change (reserved height / sticky heading) and belongs to
// the owner, not to a patch here.
// CONTROL: 1040 (the desktop content column) is expected green throughout and
// is NOT counted as coverage — it says the fix did not push the breakage onto
// desktop.
import { test, expect } from "@playwright/experimental-ct-react";
import { BootCollapseStory } from "./stories/BootCollapseStory";
import { zh } from "../src/i18n/locales/zh";

const s = zh.settings;

/** Walk in the way a person does: 設定 landing → 角色誌 → 啟動程序. */
async function openBootPage(page: import("@playwright/test").Page) {
  await page.getByText(s.roles).first().click();
  await page.getByText(s.bootName).first().click();
  await expect(page.getByText(s.bootClaudeName)).toBeVisible();
}

for (const width of [320, 390, 1040]) {
  test(`width ${width}: both boot documents are reachable on one screen`, async ({
    mount,
    page,
  }) => {
    await page.setViewportSize({ width, height: 844 });
    await mount(<BootCollapseStory />);
    await openBootPage(page);

    // (1) The claim the owner's report reduces to: he can SEE that a second
    // document exists without discovering it by scrolling. Both headings, both
    // inside the first screen.
    const claude = page.getByText(s.bootClaudeName);
    const codex = page.getByText(s.bootCodexName);
    await expect(codex).toBeVisible();
    const codexBox = (await codex.boundingBox())!;
    expect(codexBox.y, "codex heading top vs viewport").toBeLessThan(844);
    expect(
      codexBox.y + codexBox.height,
      "codex heading bottom vs viewport"
    ).toBeLessThanOrEqual(844);

    // (1b) …and the names reach a screen reader, not just the eye. An
    // aria-label on the toggle used to override both, so BOTH buttons reported
    // 展開這份文件 and this lookup matched nothing — this ticket's own defect
    // (two documents you cannot tell apart) rebuilt in the accessibility tree.
    for (const name of [s.bootClaudeName, s.bootCodexName]) {
      await expect(
        page.getByRole("button", { name, exact: true }),
        `accessible name: ${name}`
      ).toHaveCount(1);
    }

    // (2) Closed, and closed means nothing of either document is on screen —
    // otherwise "both fit" would only be true of this fixture's length.
    expect(await page.locator(".doc-md").count(), "open documents").toBe(0);

    // (3) Nothing spills sideways. The headings carry the longest words on the
    // page (「啟動程序（Claude Code）」) and the toggle is a full-width button,
    // which is new geometry on the narrowest phone.
    const spill = await page.evaluate(() => {
      const over = (el: Element) => el.scrollWidth - el.clientWidth;
      return {
        stack: over(document.querySelector(".settings-stack")!),
        page:
          document.documentElement.scrollWidth -
          document.documentElement.clientWidth,
      };
    });
    for (const [where, o] of Object.entries(spill)) {
      expect(o, `${where} horizontal overflow`).toBeLessThanOrEqual(1);
    }

    // (4) Pressing one opens THAT one. The other staying closed is what keeps
    // the second heading findable while the first is being read.
    await claude.click();
    await expect(page.locator(".doc-md")).toHaveCount(1);
    await expect(codex).toBeVisible();
  });
}

test("collapsing a document leaves its heading where it was pressed", async ({
  mount,
  page,
}) => {
  await page.setViewportSize({ width: 390, height: 844 });
  await mount(<BootCollapseStory />);
  await openBootPage(page);

  const claude = page.getByText(s.bootClaudeName);
  await claude.click();
  await expect(page.locator(".doc-md")).toHaveCount(1);

  // Read some way into the open document, then reach back for its heading —
  // which is how a reader closes it. The page is about to get thousands of
  // pixels shorter under them.
  await page.locator(".settings-stack").evaluate((el) => {
    el.scrollTop = 1200;
  });
  await expect
    .poll(async () => page.locator(".settings-stack").evaluate((el) => el.scrollTop))
    .toBeGreaterThan(0);
  await claude.scrollIntoViewIfNeeded();
  const before = (await claude.boundingBox())!.y;

  await claude.click();
  await expect(page.locator(".doc-md")).toHaveCount(0);
  const after = (await claude.boundingBox())!.y;

  // The FIRST card's heading sits above everything its own collapse removes, so
  // it holds still with no code at all — see the header, and do not read this
  // green as a scroll correction working.
  expect(Math.abs(after - before), "heading moved on collapse").toBeLessThanOrEqual(2);
});

test("collapsing the LAST document pushes its heading down but never off screen", async ({
  mount,
  page,
}) => {
  // The other half of the collapse story, and the one that is NOT free: with
  // both documents open and the page scrolled to its end, collapsing the last
  // one shortens the page under the reader. The browser clamps scrollTop and
  // the heading slides down — MEASURED 389.9 → 753.9 at both widths.
  await page.setViewportSize({ width: 390, height: 844 });
  await mount(<BootCollapseStory />);
  await openBootPage(page);

  const codex = page.getByText(s.bootCodexName);
  await page.getByText(s.bootClaudeName).click();
  await codex.click();
  await expect(page.locator(".doc-md")).toHaveCount(2);

  await page.locator(".settings-stack").evaluate((el) => {
    el.scrollTop = el.scrollHeight;
  });
  await codex.scrollIntoViewIfNeeded();
  const before = (await codex.boundingBox())!.y;

  await codex.click();
  await expect(page.locator(".doc-md")).toHaveCount(1);
  const after = (await codex.boundingBox())!.y;

  // What is actually defensible: the control the reader just pressed is still
  // on screen, so the page they are left looking at is the one they asked for.
  // The movement itself is recorded rather than asserted away — a future layout
  // fix should make this shrink, and this test should be updated when it does.
  expect(after, "heading still on screen after collapse").toBeLessThan(844);
  expect(after, "heading not scrolled above the viewport").toBeGreaterThanOrEqual(0);
  expect(after - before, "heading movement (known, unfixable by scrolling)")
    .toBeGreaterThan(0);
});
