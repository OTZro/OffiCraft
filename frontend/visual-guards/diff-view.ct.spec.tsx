// HOTSPOT — DiffView's two purely-visual contracts (T-1f39).
//
// The vitest suite pins the MODEL (which rows, which line numbers, which
// strings). Neither of the facts below survives jsdom:
//
//   ① ADDED vs REMOVED must be told apart at a glance. Both backgrounds are
//      `color-mix(in srgb, var(--token) 18%, transparent)`; jsdom hands that
//      string straight back, so a mutant that points both at the SAME token
//      stays green there. Only a real engine resolves the mix, and only then
//      can the two be compared. The +/- gutter is the non-colour channel and
//      is asserted alongside, so "distinguishable by more than colour" is
//      measured as the conjunction it actually is.
//   ② A line with no break opportunity must scroll INSIDE
//      .diff-view__scroll and leave the page unscrollable — the long-token
//      rule (T-d451). jsdom has no layout engine, so this is invisible to it.
//
// MUTANTS (each RUN and verified red):
//   point .diff-view__row--added at --color-danger-soft  → ① red (identical fill)
//   blank the `+` in DiffView's MARKER map               → ① red (colour-only)
//   drop `overflow-x: auto` from .diff-view__scroll      → ② red (page drags)
//   drop `min-width: 100%` from .diff-view__table        → ③ red (tints stop short)
//
// NOT defended, deliberately noted: dropping `white-space: pre` from
// .diff-view__text leaves all three green — the story's long line has no break
// opportunity, so it overflows either way. `pre` is there for indentation
// fidelity, and this guard does not pretend to pin it.
import { test, expect } from "@playwright/experimental-ct-react";
import { DiffViewStory } from "./stories/DiffViewStory";

type Rgba = { r: number; g: number; b: number; a: number };

/** Chromium reports a resolved color-mix as `color(srgb r g b / a)`, and a
 * plain declaration as `rgba(...)` — both shapes reach this guard. */
function parseRgba(s: string): Rgba {
  const rgb = s.match(/rgba?\(([^)]+)\)/i);
  if (rgb) {
    const p = rgb[1].split(/[,/]/).map((x) => parseFloat(x.trim()));
    return { r: p[0], g: p[1], b: p[2], a: p[3] === undefined ? 1 : p[3] };
  }
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
  throw new Error(`unresolved colour: ${s}`);
}

/** Channel-wise hue distance — a fill that merely shifted a shade fails this. */
function distance(a: Rgba, b: Rgba): number {
  return Math.abs(a.r - b.r) + Math.abs(a.g - b.g) + Math.abs(a.b - b.b);
}

async function bgOf(cmp: any, kind: string): Promise<Rgba> {
  const row = cmp.locator(`[data-kind="${kind}"]`).first();
  await expect(row, `no ${kind} row rendered`).toBeAttached();
  return parseRgba(
    await row.evaluate((n: HTMLElement) => getComputedStyle(n).backgroundColor)
  );
}

test("added and removed rows render different backgrounds AND different markers", async ({
  mount,
  page,
}) => {
  await page.setViewportSize({ width: 900, height: 900 });
  const cmp = await mount(<DiffViewStory width={800} longLine={false} />);

  const added = await bgOf(cmp, "added");
  const removed = await bgOf(cmp, "removed");
  const context = await bgOf(cmp, "context");

  // The pair the reader compares. 30 is far above sub-pixel noise and far
  // below the ~270 the shipped success/danger pairing produces.
  expect(
    distance(added, removed),
    `added ${JSON.stringify(added)} vs removed ${JSON.stringify(removed)} must not be the same fill`
  ).toBeGreaterThan(30);
  // A tint nobody can see is as good as no tint: both fills must be really
  // painted, and the UNCHANGED rows must really be bare (the positive control
  // that the tint — not something inherited — is what sets the rows apart).
  expect(added.a, "added rows must carry a visible tint").toBeGreaterThan(0.05);
  expect(removed.a, "removed rows must carry a visible tint").toBeGreaterThan(0.05);
  expect(context.a, "context rows must stay untinted").toBe(0);

  // The non-colour channel: the gutter glyph says the same thing the fill does.
  await expect(
    cmp.locator('[data-kind="added"] .diff-view__marker').first()
  ).toHaveText("+");
  await expect(
    cmp.locator('[data-kind="removed"] .diff-view__marker').first()
  ).toHaveText("-");
});

test("360px: a long unbreakable line scrolls inside the diff, never the page", async ({
  mount,
  page,
}) => {
  await page.setViewportSize({ width: 375, height: 900 });
  const cmp = await mount(<DiffViewStory width={360} />);
  await expect(cmp.locator(".diff-view__table")).toBeVisible();

  // CORE red→green — the owner's symptom class: the page drags sideways.
  const pageOver = await page.evaluate(
    () =>
      document.scrollingElement!.scrollWidth -
      document.scrollingElement!.clientWidth
  );
  expect(
    pageOver,
    `page must have no horizontal scroll (got +${pageOver}px)`
  ).toBeLessThanOrEqual(1);

  // The outer component fits its host…
  const viewOver = await cmp
    .locator(".diff-view")
    .evaluate((n: HTMLElement) => n.scrollWidth - n.clientWidth);
  expect(viewOver, "the diff box itself must not overflow").toBeLessThanOrEqual(1);

  // …and the scroll is REAL, not merely declared: the long line genuinely
  // exceeds the box, inside .diff-view__scroll.
  const scroll = cmp.locator(".diff-view__scroll");
  await expect(scroll).toHaveCSS("overflow-x", "auto");
  const scrollOver = await scroll.evaluate(
    (n: HTMLElement) => n.scrollWidth - n.clientWidth
  );
  expect(
    scrollOver,
    `the long line must still scroll INSIDE the diff (got +${scrollOver}px)`
  ).toBeGreaterThan(1);
});

test("the row tints span the full box even when every line is short", async ({
  mount,
  page,
}) => {
  await page.setViewportSize({ width: 900, height: 900 });
  const cmp = await mount(<DiffViewStory width={800} longLine={false} />);

  // ③ `min-width: 100%` on the table. Without it a short diff leaves the
  // added/removed fills stopping mid-panel, which reads as a rendering fault.
  const [rowWidth, boxWidth] = await Promise.all([
    cmp
      .locator('[data-kind="added"]')
      .first()
      .evaluate((n: HTMLElement) => n.getBoundingClientRect().width),
    cmp
      .locator(".diff-view__scroll")
      .evaluate((n: HTMLElement) => n.clientWidth),
  ]);
  expect(rowWidth).toBeGreaterThanOrEqual(boxWidth - 1);
});
