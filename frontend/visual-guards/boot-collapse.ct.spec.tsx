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
// ⚠️ THE LAST TEST HAS NO MUTANT, and that is reported rather than hidden. The
// correction it would have caught (keepAnchored, as the task steps use) was
// written, measured as a no-op — scrollTop 0 → 0, heading y 159.5 → 159.5 with
// it removed — and deleted. What a collapse removes sits BELOW the heading, and
// the heading must be on screen to be pressed, so nothing moves with no code at
// all. The test stays as a regression guard on the owner's rule (T-6630:
// 「畫面也應該停在原處，直接向上收合」) for the day someone adds an animation, a
// sticky heading, or a focus scroll here.
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

  // Owner's rule for this family of controls (2026-08-15, T-6630):「收合時…
  // 畫面也應該停在原處，直接向上收合」. The heading is the thing pressed, so it
  // is the thing that must not move. Today no code buys this — see the header
  // for why, and do not read a green here as a correction working.
  expect(Math.abs(after - before), "heading moved on collapse").toBeLessThanOrEqual(2);
});
