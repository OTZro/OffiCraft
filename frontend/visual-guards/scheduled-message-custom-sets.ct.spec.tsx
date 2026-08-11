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

// ── The tick has to be visible, and only a real browser can say so ───────────
// A 60-cell grid whose entire purpose is "which ones did I tick" is useless if
// checked and unchecked cannot be told apart. That distinction is drawn by the
// browser from `accent-color`, which jsdom does not compute and no class-name
// assertion can see: `.mp-schedmsg__setbox input` had the same class either way
// while sitting at ~1.1:1 against the card under the built-in dark palette.
//
// LIGHT_PACK is the palette of a REAL shipped theme pack, copied verbatim from
// visual-guards/stories/ThemeContrastStory.tsx (value-imported here rather than
// shared, because the CT bundler rewrites a spec's imports into component
// handles). Do not hand-tune these values: tuned, the guard stops reporting a
// fact about anything a user can ship.
const LIGHT_PACK: Record<string, string> = {
  "--color-bg": "#c2d492",
  "--color-card": "#fdfbf1",
  "--color-text": "#33301f",
  "--color-text-strong": "#1e1c10",
  "--color-text-muted": "#403d2c",
  "--color-border": "#b0ae83",
  "--color-accent": "#2b450b",
  "--color-overlay": "#241f0d",
};

type Rgb = { r: number; g: number; b: number; a: number };

function parseRgb(s: string): Rgb {
  const m = s.match(/rgba?\(([^)]+)\)/i);
  if (m) {
    const p = m[1].split(/[,/]/).map((x) => parseFloat(x.trim()));
    return { r: p[0], g: p[1], b: p[2], a: p[3] === undefined ? 1 : p[3] };
  }
  // Chromium reports some resolved colours in the CSS Color 4 form.
  const srgb = s.match(/color\(\s*srgb\s+([^)]+)\)/i);
  if (srgb) {
    const [chans, alpha] = srgb[1].split("/").map((x) => x.trim());
    const c = chans.split(/\s+/).map((x) => parseFloat(x));
    return {
      r: c[0] * 255,
      g: c[1] * 255,
      b: c[2] * 255,
      a: alpha === undefined ? 1 : parseFloat(alpha),
    };
  }
  throw new Error(`unparseable colour: ${s}`);
}

function over(fg: Rgb, bg: Rgb): Rgb {
  return {
    r: fg.r * fg.a + bg.r * (1 - fg.a),
    g: fg.g * fg.a + bg.g * (1 - fg.a),
    b: fg.b * fg.a + bg.b * (1 - fg.a),
    a: 1,
  };
}

function contrast(a: Rgb, b: Rgb): number {
  const lin = (v: number) => {
    const c = v / 255;
    return c <= 0.03928 ? c / 12.92 : Math.pow((c + 0.055) / 1.055, 2.4);
  };
  const lum = (c: Rgb) =>
    0.2126 * lin(c.r) + 0.7152 * lin(c.g) + 0.0722 * lin(c.b);
  const [hi, lo] = [lum(a), lum(b)].sort((x, y) => y - x);
  return (hi + 0.05) / (lo + 0.05);
}

for (const theme of ["built-in dark", "light pack"] as const) {
  test(`${theme}: a ticked minute box is distinguishable from an unticked one`, async ({
    mount,
    page,
  }) => {
    await page.setViewportSize({ width: 1280, height: 900 });
    const cmp = await mount(<ScheduledMessagesCustomStory width={900} />);
    if (theme === "light pack") {
      await page.evaluate((pack: Record<string, string>) => {
        for (const [k, v] of Object.entries(pack))
          document.documentElement.style.setProperty(k, v);
      }, LIGHT_PACK);
    }
    await openCustomEditor(cmp, page);

    const box = cmp.locator(`[data-testid="${EDIT}-custom-minutes-3"]`);
    await expect(box).toBeChecked();

    // The tick's colour, and every background painted behind the cell folded
    // down to the pixels actually on screen.
    const read = await box.evaluate((el: HTMLElement) => {
      const layers: string[] = [];
      let node: HTMLElement | null = el;
      while (node) {
        layers.push(getComputedStyle(node).backgroundColor);
        node = node.parentElement;
      }
      return { accent: getComputedStyle(el).accentColor, layers };
    });

    let bg: Rgb = { r: 255, g: 255, b: 255, a: 1 };
    for (let i = read.layers.length - 1; i >= 0; i--) {
      const layer = parseRgb(read.layers[i]);
      if (layer.a === 0) continue;
      bg = over(layer, bg);
    }
    // `auto` would mean the sheet says nothing and the browser picks — which is
    // not a fact this guard can measure, and not what the control declares.
    expect(read.accent).not.toBe("auto");
    const accent = over(parseRgb(read.accent), bg);

    // 3:1, the non-text threshold for a UI component's state.
    const ratio = contrast(accent, bg);
    expect(
      ratio,
      `the checked fill sits at ${ratio.toFixed(2)}:1 against the cell it is painted on under the ${theme} — an owner cannot see which minutes are selected`
    ).toBeGreaterThanOrEqual(3);
  });
}
