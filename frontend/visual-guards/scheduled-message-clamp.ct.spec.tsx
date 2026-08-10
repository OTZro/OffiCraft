// HOTSPOT — T-f059 定期訊息: a long message body is collapsed to THREE LINES on
// the row until the owner expands it.
//
// ae18d65 shipped the collapse as `-webkit-line-clamp: 3` on
// `.mp-schedmsg__text--clamped` (member-detail.css) plus a per-row 展開/收合
// control. What guarded it was a source-level pin (the vitest suite reads
// member-detail.css and asserts the two clamp lines are present) and jsdom
// component tests that can see the CLASS NAME and nothing else.
//
// 🔴 Neither is evidence that anything is three lines high. jsdom applies no
// stylesheet and resolves no layout — offsetHeight is 0 there — so "the class is
// on the node" and "the node is clamped" are different claims and only the
// second one is the feature. The source-level pin is worse than it looks: it
// goes green for any file that still CONTAINS those two lines, so moving the
// rule behind a media query, letting a later `.mp-schedmsg__text` rule win the
// cascade, or dropping `display: -webkit-box` (without which -webkit-line-clamp
// does nothing at all) all leave it perfectly happy.
//
// So this measures the real thing in real Chromium against the real sheet:
// box height, hidden overflow, and the height BEFORE vs AFTER the 展開 click.
// Width is an INPUT — how many lines a paragraph wraps to is a property of the
// panel's width — so every assertion runs at a narrow panel and a wide one.
//
// MUTANTS, all three measured (T-f059 round report). Collapsed height at
// 320px / 900px, against a 22px line:
//
//   mutant on .mp-schedmsg__text--clamped        this guard   source-level pin
//   drop -webkit-line-clamp:3 + line-clamp:3     420 / 133 ✗  ✗
//   drop `display: -webkit-box`                  420 / 133 ✗  ✓ 18/18 green
//   a LATER equal-specificity `.mp-schedmsg__text
//     { line-clamp: none }` wins the cascade     420 / 133 ✗  ✓ 18/18 green
//
// The last two are the point: the collapse is gone, the row is a wall of text
// again, and every existing test — the source pin and all 18 jsdom cases —
// stays green. Only a measured box catches them.
import { test, expect } from "@playwright/experimental-ct-react";
import { ScheduledMessagesClampStory } from "./stories/ScheduledMessagesClampStory";

// Spelled out rather than imported from the story: the CT bundler rewrites a
// spec's imports into component handles, so a value import sharing that module
// collides with the component's own binding ("already been declared").
const LONG_ID = "sch-long";
const SHORT_ID = "sch-short";

/** The two ends of the detail column's real range: the phone-width panel and a
 * desktop one. A paragraph wraps to ~4x as many lines at the narrow end, so the
 * clamp is asked a genuinely different question at each. */
const PANELS = [
  { name: "narrow panel", panel: 320, viewport: 390 },
  { name: "wide panel", panel: 900, viewport: 1280 },
];

/** Box geometry of one element, read from the live layout. */
async function boxOf(page: any, testId: string) {
  const got = await page.evaluate((id: string) => {
    const el = document.querySelector(
      `[data-testid="${id}"]`
    ) as HTMLElement | null;
    if (!el) return null;
    return { client: el.clientHeight, scroll: el.scrollHeight };
  }, testId);
  expect(got, `[${testId}] never rendered`).not.toBeNull();
  return got as { client: number; scroll: number };
}

for (const { name, panel, viewport } of PANELS) {
  test(`${name} (${panel}px): a long body is clamped to ~3 lines and the 展開 control restores it whole`, async ({
    mount,
    page,
  }) => {
    await page.setViewportSize({ width: viewport, height: 900 });
    const cmp = await mount(<ScheduledMessagesClampStory width={panel} />);
    // The card ships collapsed; the rows only exist once it is opened.
    await cmp.locator(".mp-expand__head").click();
    await expect(
      cmp.locator(`[data-testid="mp-schedmsg-row-${LONG_ID}"]`)
    ).toBeVisible();

    // The RULER. The short body occupies exactly one line at the narrowest
    // panel under test, so its own box height is one line of this surface —
    // measured, never assumed, because `.mp-schedmsg__text` sets no
    // line-height and "normal" is the font's business, not the sheet's.
    const short = await boxOf(page, `mp-schedmsg-text-${SHORT_ID}`);
    const line = short.client;
    expect(
      line,
      `the one-line ruler measured ${line}px — that is not a single 13px line, so every multiple below is fiction`
    ).toBeGreaterThan(12);
    expect(line).toBeLessThan(26);
    expect(
      short.scroll - short.client,
      `the short body hides ${short.scroll - short.client}px — it was supposed to fit on one line, so the ruler is measuring a wrapped paragraph`
    ).toBeLessThanOrEqual(1);

    // (1) NON-VACUITY: the long fixture must really overflow three lines at
    // THIS width, or everything below passes on a body that was never long
    // enough to clamp.
    const collapsed = await boxOf(page, `mp-schedmsg-text-${LONG_ID}`);
    expect(
      collapsed.scroll,
      `the long body's full height is ${collapsed.scroll}px (${(collapsed.scroll / line).toFixed(1)} lines) at ${panel}px — the fixture is too short to test a 3-line clamp`
    ).toBeGreaterThan(line * 5);

    // (2) CORE red→green: collapsed, the box is BOUNDED at about three lines.
    // Height in pixels against a measured line — not a class name, not a CSS
    // string.
    expect(
      collapsed.client,
      `collapsed height is ${collapsed.client}px = ${(collapsed.client / line).toFixed(1)} lines (one line = ${line}px) at ${panel}px — the row is not clamped to three`
    ).toBeLessThanOrEqual(line * 3.6);
    expect(
      collapsed.client,
      `collapsed height is only ${collapsed.client}px = ${(collapsed.client / line).toFixed(1)} lines at ${panel}px — three lines were supposed to stay readable`
    ).toBeGreaterThanOrEqual(line * 2.4);

    // (3) …and the rest is really HIDDEN, not merely absent. A box that just
    // happened to be short would report no overflow.
    expect(
      collapsed.scroll - collapsed.client,
      `the collapsed row hides nothing (scrollHeight ${collapsed.scroll} vs clientHeight ${collapsed.client}) at ${panel}px — the whole body is on screen`
    ).toBeGreaterThan(line);

    // (4) 展開 gives the whole body back: the box grows, and nothing is left
    // outside it.
    await cmp.locator(`[data-testid="mp-schedmsg-text-toggle-${LONG_ID}"]`).click();
    const opened = await boxOf(page, `mp-schedmsg-text-${LONG_ID}`);
    expect(
      opened.client,
      `expanded height ${opened.client}px is not meaningfully taller than the collapsed ${collapsed.client}px at ${panel}px — 展開 did nothing`
    ).toBeGreaterThan(collapsed.client * 1.8);
    expect(
      opened.scroll - opened.client,
      `expanded, the row still hides ${opened.scroll - opened.client}px at ${panel}px — 展開 must show the body WHOLE`
    ).toBeLessThanOrEqual(1);

    // (5) 收合 puts it back. Without this the control could be one-way and the
    // panel would stay a wall of text after a single read.
    await cmp.locator(`[data-testid="mp-schedmsg-text-toggle-${LONG_ID}"]`).click();
    const reclosed = await boxOf(page, `mp-schedmsg-text-${LONG_ID}`);
    expect(
      reclosed.client,
      `after 收合 the row is still ${reclosed.client}px tall at ${panel}px`
    ).toBeLessThanOrEqual(line * 3.6);
  });

  test(`${name} (${panel}px): a short body gets no 展開 control and hides nothing`, async ({
    mount,
    page,
  }) => {
    await page.setViewportSize({ width: viewport, height: 900 });
    const cmp = await mount(<ScheduledMessagesClampStory width={panel} />);
    await cmp.locator(".mp-expand__head").click();
    await expect(
      cmp.locator(`[data-testid="mp-schedmsg-row-${SHORT_ID}"]`)
    ).toBeVisible();

    // Non-vacuity: the LONG row's control is on screen, so a card that stopped
    // rendering the control at all cannot pass this by absence.
    await expect(
      cmp.locator(`[data-testid="mp-schedmsg-text-toggle-${LONG_ID}"]`)
    ).toHaveCount(1);
    await expect(
      cmp.locator(`[data-testid="mp-schedmsg-text-toggle-${SHORT_ID}"]`)
    ).toHaveCount(0);

    const short = await boxOf(page, `mp-schedmsg-text-${SHORT_ID}`);
    expect(
      short.scroll - short.client,
      `a body that fits hides ${short.scroll - short.client}px at ${panel}px — nothing may be clipped without a way to see it`
    ).toBeLessThanOrEqual(1);
  });
}
