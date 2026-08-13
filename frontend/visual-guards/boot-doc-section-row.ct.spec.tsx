// HOTSPOT — T-791e: the boot-context block page at phone widths.
//
// This page is new layout, so it gets a real-browser guard per the repo SOP.
// What it guards is not "does the markup exist" — jsdom already covers that,
// and covers it blindly: it applies no layout engine, so a section's action
// buttons are in the DOM whether they are on screen or half a viewport to the
// right of it. Every assertion below is GEOMETRY the owner can see.
//
// The two things worth measuring, and why they are the two:
//
//   1. 還原出廠版 MUST BE ON SCREEN. It is the recovery path for a failure that
//      is silent by construction — a broken boot sequence means agents never
//      attach to SSE, so they never come online, so nobody is left to fix it.
//      A recovery button that is present but pushed off a phone viewport is not
//      a recovery path, and nothing else in the suite can tell the difference.
//   2. THE SECTION ROW MUST NOT SPILL. Its label is owner/agent prose — long
//      CJK headings, and these documents cite routes and ids, so sometimes a
//      long unbreakable token. Without `overflow-wrap: anywhere` on the label
//      that token sets the row's min-content width and pushes 貼上新版本 out
//      past the card — the frontend/CLAUDE.md 〈長 token 溢出〉 family, on a
//      fresh surface.
//
// Ancestor chain is reproduced by class in the story (see its header): mounting
// a bare card at x≈0 buys ~22px of slack the app does not have, which is how
// earlier 390px guards stayed green on a broken phone.
//
// MUTANTS, measured on the real sheet in this browser — reported as measured,
// including the one that did NOT bite:
//   drop `overflow-wrap: anywhere` from `.boot-doc-sec__label` (boot-doc.css)
//     → RED at 320 ("section row 2 horizontal overflow"); green at 375/390/1040.
//       So this is a 320-only defect here, the same narrow-only shape T-5e79
//       measured — do not read the three greens as the guard being weak, and do
//       not "strengthen" it by loosening the tolerance.
//   drop `min-width: 0` from the same rule
//     → GREEN at all four widths, because `overflow-wrap: anywhere` already
//       collapses min-content on its own. That declaration was therefore
//       REDUNDANT AND UNGUARDABLE and has been deleted rather than left in with
//       a comment claiming it protects something.
// CONTROL: 1040 (the desktop content column's max width) is expected green for
// every mutant and is NOT counted as coverage — it is there to say the fix did
// not simply move the breakage to desktop.
import { test, expect } from "@playwright/experimental-ct-react";
import { BootDocSectionRowStory } from "./stories/BootDocSectionRowStory";

for (const width of [320, 375, 390, 1040]) {
  test(`width ${width}: the factory-restore button is on screen and no section row spills`, async ({
    mount,
    page,
  }) => {
    await page.setViewportSize({ width, height: 1200 });
    const cmp = await mount(<BootDocSectionRowStory />);

    // The story seeds the document through the real adapter before mounting the
    // page. If that ever stops resolving, fail loudly rather than measure an
    // empty surface — a guard that finds nothing to check must not read as a
    // guard that found nothing wrong.
    const reset = cmp.getByTestId("boot-doc-reset");
    await expect(reset).toBeVisible();
    const sections = cmp.locator(".boot-doc-sec");
    await expect(sections).toHaveCount(4);

    // (1) The recovery path is really reachable: its box lies inside the
    // viewport horizontally, with room to be pressed.
    const resetBox = (await reset.boundingBox())!;
    expect(resetBox.x, "還原出廠版 left edge").toBeGreaterThanOrEqual(-0.5);
    expect(
      resetBox.x + resetBox.width,
      "還原出廠版 right edge vs viewport"
    ).toBeLessThanOrEqual(width + 0.5);
    expect(resetBox.width, "還原出廠版 tappable width").toBeGreaterThan(40);

    // (2) Nothing spills: not a section row, not the card, not the scrollable
    // settings surface, not the page. The surface is measured as well as the
    // page because `.settings` is overflow-y:auto, which coerces overflow-x to
    // auto — it silently absorbs the overflow as an internal pan and leaves the
    // page-level number at 0 (the T-23df lesson).
    const spill = await page.evaluate(() => {
      const over = (el: Element) => el.scrollWidth - el.clientWidth;
      return {
        rows: [...document.querySelectorAll(".boot-doc-sec__head")].map(over),
        card: over(document.querySelector(".doc-card")!),
        surface: over(document.querySelector(".settings")!),
        page:
          document.documentElement.scrollWidth -
          document.documentElement.clientWidth,
      };
    });
    for (const [i, o] of spill.rows.entries()) {
      expect(o, `section row ${i} horizontal overflow`).toBeLessThanOrEqual(1);
    }
    expect(spill.card, "doc card horizontal overflow").toBeLessThanOrEqual(1);
    expect(
      spill.surface,
      "settings surface horizontal overflow"
    ).toBeLessThanOrEqual(1);
    expect(spill.page, "page horizontal overflow").toBeLessThanOrEqual(1);

    // …and the buttons the row holds really are inside the card, not merely
    // inside a row that grew to fit them. `.doc-card` is the box the reader
    // sees; a button beyond its right edge is off the card even when every
    // scrollWidth above is happy.
    const cardBox = (await cmp.locator(".doc-card").boundingBox())!;
    const paste = cmp.locator('[data-testid^="boot-doc-sec-paste-"]');
    const n = await paste.count();
    expect(n).toBe(4);
    for (let i = 0; i < n; i++) {
      const b = (await paste.nth(i).boundingBox())!;
      expect(
        b.x + b.width,
        `貼上新版本 #${i} right edge vs card`
      ).toBeLessThanOrEqual(cardBox.x + cardBox.width + 1);
      expect(b.x, `貼上新版本 #${i} left edge vs card`).toBeGreaterThanOrEqual(
        cardBox.x - 1
      );
    }
  });
}
