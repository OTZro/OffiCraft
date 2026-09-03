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
// MUTANTS (each RUN and verified red; restored from a scratchpad backup with a
// shasum reconcile, never `git checkout --`):
//   ① delete all four `.diff-view__row--split[data-*-kind=…]` background rules
//      → red 2/3. "must not be the same fill" reports distance 0 with BOTH
//        halves at `{"r":0,"g":0,"b":0,"a":0}`, and the two gutters collapse to
//        one colour. The bare-context test stays green — correct, it is the
//        positive control and context rows are legitimately bare either way.
//   ② delete only the two `nth-child(1)` / `nth-child(4)` 32% gutter rules
//      → red 1/3, and ONLY the gutter test: `removed gutter {…"a":0.18} must be
//        stronger than its text cell {…"a":0.18}`. The distance test stays green
//        (the 18% side fills survive), which is what makes ② prove that the
//        gutter assertion carries its own weight rather than riding on ①.
// ⇒ each of the two colour tests has a mutant that kills it ALONE.
//
// NOT defended, deliberately noted: under ① the gutters still resolve to the
// `.diff-view__ln` sunken fill (that rule is untouched), so "gutter stronger
// than its text cell" passes there on 0.22 > 0 — the collapse assertion is what
// catches ① in that test. This guard pins the split TINTS, not `.diff-view__ln`.
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

/* ── 兩欄對照 must actually SHOW two columns (owner 2026-09-03, c-f56f272e19e2)
 *
 * The owner found this himself, on his own material: he pressed 兩欄對照 and got
 * a screen indistinguishable from 單欄. Nothing errored. The table is
 * `width: max-content`, so ONE line longer than the panel widens the whole
 * table and the right half queues up past the edge — measured at 5024px of
 * table inside a 1332px box, with the right column starting at x=2566 on a
 * 1440-wide viewport. A wider screen does not rescue it (the host has a
 * max-width of its own).
 *
 * The tests above cannot catch this and never could: they mount with
 * `longLine={false}` ON PURPOSE (the colour assertions need a table that is not
 * wide by force), so the one input that triggers the defect is excluded from
 * them by design. This guard is the mirror — it mounts WITH the long line and
 * asserts the only thing the reader actually cares about: can I see the second
 * column.
 *
 * It asserts POSITION, not the CSS. `table-layout: fixed` is today's fix; a
 * later one may split the panel differently. What must not change is that the
 * right half lands inside the box.
 *
 * MUTANT (run, verified red): delete the `.diff-view__table--split` rule block
 * from diff-view.css → "right column starts at 2557.5px but the scroll box ends
 * at 918px" — this test alone goes red, the three colour tests above stay green
 * (they mount without the long line). */
test("split: the right column is INSIDE the panel even with an unwrappable line", async ({
  mount,
  page,
}) => {
  await page.setViewportSize({ width: 1000, height: 900 });
  const cmp = await mount(<DiffViewStory width={900} />);
  await cmp.getByTestId("diff-view-mode-split").click();
  await expect(cmp.locator(REPLACED).first()).toBeAttached();

  const box = await cmp.locator(".diff-view__scroll").boundingBox();
  const right = await cmp.locator(`${REPLACED} td:nth-child(4)`).first().boundingBox();
  if (box === null || right === null) throw new Error("no layout box");

  expect(
    right.x,
    `right column starts at ${right.x}px but the scroll box ends at ${box.x + box.width}px`
  ).toBeLessThan(box.x + box.width);
  // …and it must not merely peek in: the reader has to be able to READ it.
  expect(
    box.x + box.width - right.x,
    "the right column must have real width inside the panel, not a sliver"
  ).toBeGreaterThan(80);

  // The two sides stay row-aligned — the reason the unified sheet refuses to
  // wrap. Here they share one <tr>, so a wrapped line grows the row for BOTH.
  const l = await cmp.locator(`${REPLACED} td:nth-child(1)`).first().boundingBox();
  const r = await cmp.locator(`${REPLACED} td:nth-child(4)`).first().boundingBox();
  if (l === null || r === null) throw new Error("no gutter box");
  expect(Math.abs(l.y - r.y), "the two line-number gutters must stay level").toBeLessThan(2);
});

/* The other half of the contract: 單欄 must NOT be changed by the fix above. It
 * keeps `pre` and its horizontal scroll — that is where a long line is allowed
 * to run off the side, because there is only one column and nothing gets hidden
 * BEHIND it. Without this, "make split fit" could be satisfied by wrapping
 * everywhere, which would quietly rewrite the unified view the owner has been
 * reading since 7/31. */
test("unified keeps its horizontal scroll for a long line", async ({ mount, page }) => {
  await page.setViewportSize({ width: 1000, height: 900 });
  const cmp = await mount(<DiffViewStory width={900} />);
  const scroll = cmp.locator(".diff-view__scroll");
  const overflow = await scroll.evaluate((el) => el.scrollWidth - el.clientWidth);
  expect(overflow, "單欄 must still scroll sideways for an unwrappable line").toBeGreaterThan(0);
});
