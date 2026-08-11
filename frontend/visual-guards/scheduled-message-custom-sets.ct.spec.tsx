// HOTSPOT — T-49e7 定期訊息 `custom` cadence: the minute set is SIXTY
// checkboxes, and the detail panel is 320px wide on a phone.
//
// jsdom evaluates no CSS and resolves no layout, so every assertion the vitest
// suite can make about these three groups is about class names and checked
// states. "The grid has class `mp-schedmsg__setgrid`" and "sixty boxes fit
// inside the panel" are different claims, and only the second one is the
// feature: drop the `grid-template-columns: repeat(auto-fill, …)` and the row
// becomes one 60-wide line that pushes the whole detail column sideways —
// every jsdom case stays green, and the owner gets a panel that scrolls left
// and right on his phone.
//
// So this measures the real thing in real Chromium against the real sheet:
// the panel's own horizontal overflow, the grid's rendered height, and the
// on-screen rectangle of every single checkbox. Width is an INPUT (how a grid
// wraps is a property of the column's width), so every assertion runs at the
// phone-width panel and a desktop one.
//
// MUTANTS, measured (T-49e7; restored from a scratchpad backup + shasum, never
// `git checkout --`):
//
//   mutant on .mp-schedmsg__setgrid                    this guard  jsdom (23)
//   grid-template-columns → repeat(60, 46px) AND       6/6 red ✗   23/23 green ✓
//     max-height/overflow-y dropped
//   …same mutant, but with THIS FILE's assertions      6/6 green   23/23 green
//     rewritten to read class names + counts
//
// The second row is the point and it was really run: against a grid that is one
// 60-wide flat row, `toHaveClass(/mp-schedmsg__setgrid--minutes/)` and
// `toHaveCount(60)` are both perfectly true. A guard written that way is green
// on the broken sheet in the real browser as well as in jsdom, i.e. it has no
// discrimination at all — the rectangles below are the entire reason this file
// catches anything.
import { test, expect } from "@playwright/experimental-ct-react";
import { ScheduledMessagesCustomStory } from "./stories/ScheduledMessagesCustomStory";

// Spelled out rather than imported from the story: the CT bundler rewrites a
// spec's imports into component handles, so a value import sharing that module
// collides with the component's own binding ("already been declared").
const CUSTOM_ID = "sch-custom";
const EDIT = `mp-schedmsg-edit-${CUSTOM_ID}`;

/** The two ends of the detail column's real range. */
const PANELS = [
  { name: "narrow panel", panel: 320, viewport: 390 },
  { name: "wide panel", panel: 900, viewport: 1280 },
];

/** Horizontal overflow of one element, read from the live layout. */
async function hOverflow(page: any, selector: string) {
  const got = await page.evaluate((sel: string) => {
    const el =
      sel === ":root"
        ? document.documentElement
        : (document.querySelector(sel) as HTMLElement | null);
    if (!el) return null;
    return { over: el.scrollWidth - el.clientWidth, width: el.clientWidth };
  }, selector);
  expect(got, `[${selector}] never rendered`).not.toBeNull();
  return got as { over: number; width: number };
}

/** Open the card, open the stored custom schedule's editor, and hand back the
 * measurements the assertions below share. */
async function openCustomEditor(cmp: any, page: any) {
  await cmp.locator(".mp-expand__head").click();
  await cmp.locator(`[data-testid="${EDIT}"]`).click();
  const grid = cmp.locator(`[data-testid="${EDIT}-custom-minutes-grid"]`);
  await expect(grid).toBeVisible();
  return grid;
}

for (const { name, panel, viewport } of PANELS) {
  test(`${name} (${panel}px): sixty minute checkboxes stay inside the panel instead of widening it`, async ({
    mount,
    page,
  }) => {
    await page.setViewportSize({ width: viewport, height: 900 });
    const cmp = await mount(<ScheduledMessagesCustomStory width={panel} />);
    const grid = await openCustomEditor(cmp, page);

    // (1) NON-VACUITY: all sixty boxes are really on screen. Everything below
    // passes trivially on a grid that quietly renders twenty.
    const boxes = grid.locator("input[type=checkbox]");
    await expect(boxes).toHaveCount(60);

    // (2) NON-VACUITY: they really WRAP at this width, so "does it wrap
    // correctly" is a question this panel is actually being asked.
    const rows = await page.evaluate((sel: string) => {
      const g = document.querySelector(sel) as HTMLElement;
      const tops = new Set<number>();
      g.querySelectorAll("input[type=checkbox]").forEach((el) =>
        tops.add(Math.round(el.getBoundingClientRect().top))
      );
      return tops.size;
    }, `[data-testid="${EDIT}-custom-minutes-grid"]`);
    expect(
      rows,
      `the 60 minute boxes occupy ${rows} row(s) at ${panel}px — a single row means nothing was ever asked to wrap`
    ).toBeGreaterThan(1);

    // (3) CORE red→green: the panel gained no horizontal scroll, and neither
    // did the page. Pixels, not a class name.
    const panelBox = await hOverflow(page, '[data-testid="panel"]');
    expect(
      panelBox.over,
      `the detail panel hides ${panelBox.over}px horizontally at ${panel}px — the minute grid widened the column`
    ).toBeLessThanOrEqual(1);
    const doc = await hOverflow(page, ":root");
    expect(
      doc.over,
      `the page scrolls ${doc.over}px sideways at ${panel}px`
    ).toBeLessThanOrEqual(1);

    // (4) …and each box is really WITHIN the panel's content box. A grid whose
    // overflow was absorbed by some ancestor's scroll container would report no
    // overflow above while the boxes sit off to the right (T-49fb).
    const worst = await page.evaluate((sel: string) => {
      const g = document.querySelector(sel) as HTMLElement;
      const p = document.querySelector(
        '[data-testid="panel"]'
      ) as HTMLElement;
      const pr = p.getBoundingClientRect();
      let out = 0;
      g.querySelectorAll("input[type=checkbox]").forEach((el) => {
        const r = el.getBoundingClientRect();
        out = Math.max(out, r.right - pr.right, pr.left - r.left);
      });
      return Math.round(out);
    }, `[data-testid="${EDIT}-custom-minutes-grid"]`);
    expect(
      worst,
      `a minute checkbox sits ${worst}px outside the panel's box at ${panel}px`
    ).toBeLessThanOrEqual(1);
  });

  test(`${name} (${panel}px): the minute grid is bounded and scrolls itself rather than taking over the panel`, async ({
    mount,
    page,
  }) => {
    await page.setViewportSize({ width: viewport, height: 900 });
    const cmp = await mount(<ScheduledMessagesCustomStory width={panel} />);
    await openCustomEditor(cmp, page);

    const sizes = await page.evaluate(
      ({ gridSel }: { gridSel: string }) => {
        const g = document.querySelector(gridSel) as HTMLElement;
        const p = document.querySelector(
          '[data-testid="panel"]'
        ) as HTMLElement;
        return {
          grid: g.clientHeight,
          gridFull: g.scrollHeight,
          panel: p.clientHeight,
        };
      },
      { gridSel: `[data-testid="${EDIT}-custom-minutes-grid"]` }
    );

    // The whole point of capping it: sixty boxes may not eat the column. The
    // number is generous — this is a ceiling on what the user sees, not a
    // restatement of the sheet's exact max-height.
    expect(
      sizes.grid,
      `the minute grid is ${sizes.grid}px tall at ${panel}px — it has taken over the panel`
    ).toBeLessThanOrEqual(220);
    expect(
      sizes.grid,
      `the minute grid is only ${sizes.grid}px tall at ${panel}px — nothing is readable in it`
    ).toBeGreaterThan(40);
    expect(
      sizes.grid,
      `the minute grid (${sizes.grid}px) is most of the ${sizes.panel}px panel at ${panel}px`
    ).toBeLessThan(sizes.panel * 0.6);
    // Bounded means CONTAINED, not clipped: whatever the cap hides has to be
    // reachable inside the grid's own scroll box.
    if (sizes.gridFull > sizes.grid) {
      const reachable = await page.evaluate((sel: string) => {
        const g = document.querySelector(sel) as HTMLElement;
        g.scrollTop = g.scrollHeight;
        return g.scrollTop > 0;
      }, `[data-testid="${EDIT}-custom-minutes-grid"]`);
      expect(
        reachable,
        `the minute grid clips ${sizes.gridFull - sizes.grid}px that nothing can scroll to at ${panel}px`
      ).toBe(true);
    }
  });

  test(`${name} (${panel}px): collapsing the minute detail really gives the panel its height back`, async ({
    mount,
    page,
  }) => {
    await page.setViewportSize({ width: viewport, height: 900 });
    const cmp = await mount(<ScheduledMessagesCustomStory width={panel} />);
    await openCustomEditor(cmp, page);

    const formSel = `[data-testid="mp-schedmsg-editform-${CUSTOM_ID}"]`;
    const heightOf = async () =>
      (await page.evaluate((sel: string) => {
        const el = document.querySelector(sel) as HTMLElement;
        return el.getBoundingClientRect().height;
      }, formSel)) as number;

    const opened = await heightOf();
    await cmp
      .locator(`[data-testid="${EDIT}-custom-minutes-detail-toggle"]`)
      .click();
    await expect(
      cmp.locator(`[data-testid="${EDIT}-custom-minutes-grid"]`)
    ).toHaveCount(0);
    const collapsed = await heightOf();

    // A control that hid the boxes without reclaiming their space would be a
    // control that did nothing the owner can see.
    expect(
      opened - collapsed,
      `collapsing the minute detail changed the form height by ${(opened - collapsed).toFixed(0)}px at ${panel}px — the boxes were never taking any room`
    ).toBeGreaterThan(40);
    // And the shortcut row survives the collapse: hiding the boxes must not
    // leave the group with no way to choose anything.
    await expect(
      cmp.locator(`[data-testid="${EDIT}-custom-minutes-step-20"]`)
    ).toBeVisible();
  });
}
