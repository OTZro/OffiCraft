// HOTSPOT — 兩欄對照 (split) mode's colour contract (T-40f0).
//
// The sibling guard `diff-view.ct.spec.tsx` measures the UNIFIED view, where
// the tint lives on the <tr> and one `data-kind` per row says everything. Split
// mode is a different mechanism and had no colour guard at all: the tint is
// per SIDE (one visual row holds the removed half AND the added half that
// replaced it), so it hangs off `td:nth-child(...)` selected by the row's
// `data-left-kind` / `data-right-kind`.
//
// 🔴 The only coverage split had was `DiffView.test.tsx` asserting those two
// ATTRIBUTE STRINGS in jsdom. That has ZERO discriminating power for the defect
// class that actually breaks the screen: the attribute is set correctly, the
// rule is out-cascaded (or its selector stops matching the cell layout), and
// the two columns render identically while every jsdom assertion stays green.
// jsdom also hands `color-mix(...)` straight back as source text, so it cannot
// tell "danger" from "success" even in principle. This file therefore asserts
// only what a reader can OBSERVE — resolved background colours — in a real
// engine, and never a CSS property string.
//
// Colours are never hardcoded here: the assertions are channel DISTANCE and
// ALPHA, so a theme is free to repaint both sides and this guard still holds.
//
// MUTANTS (each RUN and verified red — see the task report for the messages):
//   delete the four `.diff-view__row--split[data-*-kind=...]` background rules
//     → "the two columns must not be the same fill" red (distance 0)
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

/** The resolved background of one cell, by 1-based column, of the first row
 * matching `rowSel`. Split cells are 1-3 = old version, 4-6 = current. */
async function cellBg(cmp: any, rowSel: string, nth: number): Promise<Rgba> {
  const row = cmp.locator(rowSel).first();
  await expect(row, `no split row matched ${rowSel}`).toBeAttached();
  const cell = row.locator(`td:nth-child(${nth})`);
  await expect(cell, `${rowSel} has no cell ${nth}`).toBeAttached();
  return parseRgba(
    await cell.evaluate((n: HTMLElement) => getComputedStyle(n).backgroundColor)
  );
}

const REPLACED =
  '[data-testid="diff-view-split-row"][data-left-kind="removed"][data-right-kind="added"]';
const UNCHANGED =
  '[data-testid="diff-view-split-row"][data-left-kind="context"][data-right-kind="context"]';

/** Enter 兩欄對照 through the real control the reader uses. */
async function mountSplit(mount: any, page: any) {
  await page.setViewportSize({ width: 1000, height: 900 });
  const cmp = await mount(<DiffViewStory width={900} longLine={false} />);
  await cmp.getByTestId("diff-view-mode-split").click();
  await expect(cmp.locator(REPLACED).first()).toBeAttached();
  return cmp;
}

test("split: the removed half and the added half render different backgrounds", async ({
  mount,
  page,
}) => {
  const cmp = await mountSplit(mount, page);

  // The pair the reader compares, on ONE visual row: left text cell (old) vs
  // right text cell (current). Same threshold as the unified guard — 30 is far
  // above sub-pixel noise and far below the ~270 the shipped pairing gives.
  const left = await cellBg(cmp, REPLACED, 3);
  const right = await cellBg(cmp, REPLACED, 6);
  expect(
    distance(left, right),
    `left ${JSON.stringify(left)} vs right ${JSON.stringify(right)} must not be the same fill`
  ).toBeGreaterThan(30);

  // A tint nobody can see is as good as no tint.
  expect(left.a, "the removed half must carry a visible tint").toBeGreaterThan(0.05);
  expect(right.a, "the added half must carry a visible tint").toBeGreaterThan(0.05);
});

test("split: unchanged rows stay bare on BOTH sides", async ({ mount, page }) => {
  const cmp = await mountSplit(mount, page);

  // The positive control for the test above: it is the TINT that sets the two
  // halves apart, not something inherited by every cell in the table. If this
  // ever goes non-zero the distance assertion above could pass on a table that
  // is uniformly painted.
  const left = await cellBg(cmp, UNCHANGED, 3);
  const right = await cellBg(cmp, UNCHANGED, 6);
  expect(left.a, "unchanged left half must stay untinted").toBe(0);
  expect(right.a, "unchanged right half must stay untinted").toBe(0);
});

test("split: each line-number gutter is tinted STRONGER than its own text cell", async ({
  mount,
  page,
}) => {
  const cmp = await mountSplit(mount, page);

  // The `nth-child(1)` / `nth-child(4)` rules (32% vs the body's 18%). Losing
  // them leaves a grey notch in an otherwise coloured row — the exact defect
  // the unified sheet calls out and fixes for its own gutter. Asserting the
  // RELATION (stronger than its own side) keeps this theme-agnostic and, unlike
  // reading the declaration, survives the rule being out-cascaded.
  const leftGutter = await cellBg(cmp, REPLACED, 1);
  const leftText = await cellBg(cmp, REPLACED, 3);
  const rightGutter = await cellBg(cmp, REPLACED, 4);
  const rightText = await cellBg(cmp, REPLACED, 6);

  expect(
    leftGutter.a,
    `removed gutter ${JSON.stringify(leftGutter)} must be stronger than its text cell ${JSON.stringify(leftText)}`
  ).toBeGreaterThan(leftText.a);
  expect(
    rightGutter.a,
    `added gutter ${JSON.stringify(rightGutter)} must be stronger than its text cell ${JSON.stringify(rightText)}`
  ).toBeGreaterThan(rightText.a);

  // …and each gutter must still be its OWN side's colour, not the other's.
  expect(
    distance(leftGutter, rightGutter),
    "the two gutters must not collapse to one colour"
  ).toBeGreaterThan(30);
});
