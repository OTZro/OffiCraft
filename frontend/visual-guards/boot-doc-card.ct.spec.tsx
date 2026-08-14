// HOTSPOT — the boot-context block page at phone widths.
//
// SUCCESSOR TO `boot-doc-section-row.ct.spec.tsx` (T-c33e). That file measured
// a surface that no longer exists — the per-section rows — but the two things
// it was BOUGHT for are unchanged, and each is carried over here one for one:
//
//   1. 還原出廠版 MUST BE ON SCREEN. It is the recovery path for a failure that
//      is silent by construction — a broken boot sequence means agents never
//      attach to SSE, so they never come online, so nobody is left to fix it.
//      A recovery button that is present but pushed off a phone viewport is not
//      a recovery path, and nothing else in the suite can tell the difference.
//      → the same assertion, on `doc-card-reset` (DocCard draws the button now;
//        the testid moved with it, the geometry claim did not change).
//   2. THE CARD MUST NOT SPILL on owner/agent prose carrying a long unbreakable
//      token. The old guard measured that on the section-row LABEL; the token
//      now lands in the rendered document (`.doc-md`), so the same fixture text
//      is measured on the same chain — head, card, `.settings`, page.
//      → the mutant that used to bite (`overflow-wrap: anywhere` off
//        `.boot-doc-sec__label`) is replaced by the same declaration on
//        `.doc-md` in settings.css, which is where that class of defect has
//        lived for every other document surface since T-d451.
//
// Ancestor chain is reproduced by class in the story (see its header): mounting
// a bare card at x≈0 buys ~22px of slack the app does not have, which is how
// earlier 390px guards stayed green on a broken phone.
//
// MUTANTS, measured on the real sheet in this browser — reported as measured:
//   drop `overflow-wrap: anywhere` from `.doc-md` (settings.css)
//     → RED at 320 AND 375; green at 390/1040. (The same mutant reddens
//       boot-doc-real-seed.ct.spec.tsx at 320 only, +45px — this fixture's
//       token is longer relative to the column, so it bites one width wider.)
//       Do not read the two greens as the guard being weak, and do not
//       "strengthen" it by loosening the tolerance.
// CONTROL: 1040 (the desktop content column's max width) is expected green for
// every mutant and is NOT counted as coverage — it is there to say the fix did
// not simply move the breakage to desktop.
import { test, expect } from "@playwright/experimental-ct-react";
import { BootDocCardStory } from "./stories/BootDocCardStory";

for (const width of [320, 375, 390, 1040]) {
  test(`width ${width}: the factory-restore button is on screen and the card does not spill`, async ({
    mount,
    page,
  }) => {
    await page.setViewportSize({ width, height: 1200 });
    const cmp = await mount(<BootDocCardStory />);

    // The story seeds the document through the real adapter before mounting the
    // page. If that ever stops resolving, fail loudly rather than measure an
    // empty surface — a guard that finds nothing to check must not read as a
    // guard that found nothing wrong.
    const reset = cmp.getByTestId("doc-card-reset");
    await expect(reset).toBeVisible();
    // The document really arrived: the heading carrying the unbreakable token
    // is the whole reason this fixture exists, so measuring before it lands
    // would measure an empty card.
    await expect(cmp.locator(".doc-md")).toContainText("abcdef0123456789");

    // (1) The recovery path is really reachable: its box lies inside the
    // viewport horizontally, with room to be pressed.
    const resetBox = (await reset.boundingBox())!;
    expect(resetBox.x, "還原出廠版 left edge").toBeGreaterThanOrEqual(-0.5);
    expect(
      resetBox.x + resetBox.width,
      "還原出廠版 right edge vs viewport"
    ).toBeLessThanOrEqual(width + 0.5);
    expect(resetBox.width, "還原出廠版 tappable width").toBeGreaterThan(40);

    // (2) Nothing spills: not the card head, not the card, not the scrollable
    // settings surface, not the page. The surface is measured as well as the
    // page because `.settings` is overflow-y:auto, which coerces overflow-x to
    // auto — it silently absorbs the overflow as an internal pan and leaves the
    // page-level number at 0 (the T-23df lesson).
    const spill = await page.evaluate(() => {
      const over = (el: Element) => el.scrollWidth - el.clientWidth;
      return {
        head: over(document.querySelector(".doc-card__head")!),
        recover: over(document.querySelector(".doc-card__recover")!),
        note: over(document.querySelector(".doc-card__note")!),
        card: over(document.querySelector(".doc-card")!),
        surface: over(document.querySelector(".settings")!),
        page:
          document.documentElement.scrollWidth -
          document.documentElement.clientWidth,
      };
    });
    for (const [where, o] of Object.entries(spill)) {
      expect(o, `${where} horizontal overflow`).toBeLessThanOrEqual(1);
    }

    // …and the head's own controls really are inside the card, not merely
    // inside a head that grew to fit them. `.doc-card` is the box the reader
    // sees; a button beyond its right edge is off the card even when every
    // scrollWidth above is happy.
    const cardBox = (await cmp.locator(".doc-card").boundingBox())!;
    for (const id of ["doc-card-usage", "doc-card-edit"]) {
      const b = (await cmp.getByTestId(id).boundingBox())!;
      expect(
        b.x + b.width,
        `${id} right edge vs card`
      ).toBeLessThanOrEqual(cardBox.x + cardBox.width + 1);
      expect(b.x, `${id} left edge vs card`).toBeGreaterThanOrEqual(
        cardBox.x - 1
      );
    }

    // (3) And the editor the section rows were replaced by is itself inside the
    // card — a whole-document textarea is new geometry on this page, and a
    // fixed-width one would pan the surface exactly like a long token does.
    await cmp.getByTestId("doc-card-edit").click();
    const editorBox = (await cmp.getByTestId("doc-card-editor").boundingBox())!;
    expect(editorBox.width, "editor width").toBeGreaterThan(80);
    expect(
      editorBox.x + editorBox.width,
      "editor right edge vs card"
    ).toBeLessThanOrEqual(cardBox.x + cardBox.width + 1);
    const editSpill = await page.evaluate(() => {
      const over = (el: Element) => el.scrollWidth - el.clientWidth;
      return {
        card: over(document.querySelector(".doc-card")!),
        surface: over(document.querySelector(".settings")!),
        page:
          document.documentElement.scrollWidth -
          document.documentElement.clientWidth,
      };
    });
    for (const [where, o] of Object.entries(editSpill)) {
      expect(o, `${where} horizontal overflow while editing`).toBeLessThanOrEqual(1);
    }
    // The recovery path survives edit mode: it is not behind it, and a phone
    // that can only reach it from the read view is a phone that cannot reach it
    // when the document is half-rewritten.
    const resetWhileEditing = (await reset.boundingBox())!;
    expect(
      resetWhileEditing.x + resetWhileEditing.width,
      "還原出廠版 right edge vs viewport, while editing"
    ).toBeLessThanOrEqual(width + 0.5);
  });
}
