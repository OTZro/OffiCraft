// HOTSPOT — T-6630: 展開與收合都不改變捲動位置.
//
// Owner (2026-08-15, superseding T-4e39): 「我要整個畫面不移動,只是單純往下展開,
// 而收合時,就是向上收合,整個畫面不能移動」. T-4e39 had shipped the opposite
// bargain — a keepAnchored() correction that re-scrolled `.tasks` on OPEN so the
// clicked row kept its viewport y. Re-scrolling IS the screen moving, so this
// ticket deleted the correction (and src/lib/scrollAnchor.ts with it).
//
// WHAT THIS FILE ASSERTS:
//   * `.tasks`' scrollTop is IDENTICAL across an open and across a close. Not
//     "close to" — exactly equal, everywhere the browser is not forced (see the
//     clamp test at the bottom). That is the whole feature.
//   * geometry, not a call log: nothing here checks that scrollIntoView was or
//     was not called. That API re-targets every scrollport on the ancestor
//     chain, so its absence is what the numbers below prove, not the reverse.
//   * the scrollport is the INNER container (`.tasks`) — measured on the live
//     site, `document.scrollHeight` equals the window height at every width, so
//     asserting on `document.scrollingElement` would be measuring a box that
//     never scrolls and would stay green against ANY implementation. Every test
//     proves that premise before it measures anything.
//   * a click is only a click if the button is already on screen. Playwright's
//     `.click()` SCROLLS AN OFF-SCREEN TARGET INTO VIEW FIRST, which moves the
//     scrollport by hundreds of px and has nothing to do with the component —
//     T-4e39's header reported "collapsing step 1 moves step 5 from 375 to 907,
//     532px off screen" and that number is exactly that harness scroll
//     (measured here: 713→11 scrollTop, minus the 132px the note gave back,
//     = the 570 the row appeared to fall). So every toggle this file clicks is
//     asserted to be inside the scrollport first, and off-screen toggles are
//     driven through `element.click()`, which does not scroll.
//
// WHAT "不移動" DOES AND DOES NOT MEAN, stated because it is the one thing a
// reader can get wrong here: the SCROLLPORT does not move. The rows below the
// toggle do — that is the note opening and closing. Collapsing a note lifts
// everything under it by exactly the height that went away; there is no
// alternative unless the fold leaves a hole. Each test below pins that lift to
// the EXACT height, so a change that lifts by more or less (or pushes down)
// reddens.
//
// THE ONE PLACE THE SCROLLPORT REALLY DOES MOVE, and it is not a bug we can
// code away: closing a note shortens the scrollable range, and if the reader is
// near the end of that range the offset they are at stops existing — the
// browser clamps scrollTop to the new maximum and the content slides DOWN under
// a viewport that cannot follow it. MEASURED here (close 節點 9's note with its
// toggle at the top of the scrollport), 收合後那一列被往下推的量:
//   1280×800 短備註  scrollTop 815 → 683   forced −132   row 586.4 → 718.4
//   390×844  短備註  scrollTop 902 → 680   forced −222   row 541.6 → 763.6
//   1280×800 長備註  scrollTop 1359 → 683  forced −676   row  42.4 → 718.4
//   390×844  長備註  scrollTop 1399 → 680  forced −719   row  44.6 → 763.6
// In the 長備註 rows the note that went away (916 / 1636 px) is TALLER than the
// scroll offset the reader had, so the clamp stops at 0-headroom rather than at
// the full height: the forced move is `min(scrollTop, removed)` and the rest of
// the height simply has nowhere to come from. "完全不動" is therefore false in
// this corner, and the last test asserts the corner EXACTLY — `min(before,
// newMax)`, not a widened tolerance — so a real regression cannot hide in it.
//
// MUTANTS MEASURED (each planted IN PLACE, on the declaration named):
//   M1 · TaskCard.tsx `toggleNote` — put T-4e39's correction back, inline:
//        record the wrap's top before the state change and, in a
//        useLayoutEffect on `openNotes`, write
//        `container.scrollTop += wrap.getBoundingClientRect().top - prevTop`.
//        ⇒ 8 failed / 9 passed. Reported e.g. "1280×800 desktop · dark:
//        opening the note scrolled `.tasks` (815 → 947)" — expected 947 to be
//        815 — and the phone's tall case 2316 → 3952.
//   M2 · TaskCard.tsx `toggleNote` — collapse pushes the rows below away
//        instead of leaving the scrollport alone: on a close, walk up to
//        `.tasks` and do `container.scrollTop -= 120`.
//        ⇒ 12 failed / 5 passed. Reported e.g. "1280×800 desktop · dark:
//        collapsing moved `.tasks` (815 → 695)", and on the clamp test
//        "the browser's clamp is the only movement allowed" 695 vs 815.
import { test, expect } from "@playwright/experimental-ct-react";
import { TaskCardNoteAnchorStory } from "./stories/TaskCardNoteAnchorStory";

// The two widths the ticket measured, plus the two shipped themes. 390×844 is
// the owner's phone, 1280×800 the desktop window.
//
// `tall: true` is not a synthetic worst case: real step notes on this site have
// a 515-character median, t-fc23's longest is 1790 and T-e5b1's own step 4 runs
// to ~3.5k, so a row taller than the whole scrollport is the ordinary case, not
// the corner. It is also where a correction would have the most room to move
// the screen, and where the clamp above bites hardest (−1636 on the phone).
const CASES = [
  { name: "390×844 phone · dark", w: 390, h: 844, theme: "dark" as const, tall: false },
  { name: "390×844 phone · light", w: 390, h: 844, theme: "light" as const, tall: false },
  { name: "1280×800 desktop · dark", w: 1280, h: 800, theme: "dark" as const, tall: false },
  { name: "1280×800 desktop · light", w: 1280, h: 800, theme: "light" as const, tall: false },
  { name: "390×844 phone · dark · 備註比視窗高", w: 390, h: 844, theme: "dark" as const, tall: true },
  { name: "390×844 phone · light · 備註比視窗高", w: 390, h: 844, theme: "light" as const, tall: true },
  { name: "1280×800 desktop · dark · 備註比視窗高", w: 1280, h: 800, theme: "dark" as const, tall: true },
  { name: "1280×800 desktop · light · 備註比視窗高", w: 1280, h: 800, theme: "light" as const, tall: true },
];

async function mountExpanded(mount: any, page: any, c: (typeof CASES)[number]) {
  await page.setViewportSize({ width: c.w, height: c.h });
  const cmp = await mount(
    <TaskCardNoteAnchorStory theme={c.theme} noteRepeat={c.tall ? 8 : 1} />
  );
  await cmp.locator(".task-card__head").first().click();
  await expect(cmp.locator(".task-card__workflow")).toBeVisible();
  return cmp;
}

/** The scrollport's state plus the viewport box of every step row asked for. */
async function measure(page: any, ids: string[]) {
  return page.evaluate((ids: string[]) => {
    const sc = document.querySelector(".tasks") as HTMLElement;
    if (!sc) return null;
    const doc = document.scrollingElement!;
    const rows: Record<string, { top: number; height: number }> = {};
    for (const id of ids) {
      const wrap = document.querySelector(
        `[data-step-id='${id}'] .task-step__notewrap`
      ) as HTMLElement | null;
      if (!wrap) return null;
      const r = wrap.getBoundingClientRect();
      rows[id] = { top: r.top, height: r.height };
    }
    const sr = sc.getBoundingClientRect();
    return {
      scrollTop: sc.scrollTop,
      maxScroll: sc.scrollHeight - sc.clientHeight,
      viewTop: sr.top,
      viewBottom: sr.top + sc.clientHeight,
      viewHeight: sc.clientHeight,
      docScroll: doc.scrollHeight - doc.clientHeight,
      rows,
    };
  }, ids);
}

/** `.tasks` is the box that scrolls and the document is not — the shape the
 * live site has. Without it every assertion below would be vacuous. */
function assertScrollportPremise(m: any, label: string) {
  expect(m, `${label}: the fixture must render the note rows`).not.toBeNull();
  expect(m.maxScroll, `${label}: \`.tasks\` must be the scrollport`).toBeGreaterThan(50);
  expect(
    m.docScroll,
    `${label}: the document must not be scrolling`
  ).toBeLessThanOrEqual(1);
}

/** Put a step's disclosure row at `frac` of the way down the scrollport — the
 * state a reader is in when they reach a note by scrolling and tap it. */
async function park(page: any, stepId: string, frac: number) {
  await page.evaluate(
    ({ id, frac }: { id: string; frac: number }) => {
      const sc = document.querySelector(".tasks") as HTMLElement;
      const wrap = document.querySelector(
        `[data-step-id='${id}'] .task-step__notewrap`
      ) as HTMLElement;
      const want = sc.getBoundingClientRect().top + sc.clientHeight * frac;
      sc.scrollTop += wrap.getBoundingClientRect().top - want;
    },
    { id: stepId, frac }
  );
}

/** A toggle a user could not see is a toggle a user could not press — and
 * Playwright would scroll it into view, which is a scrollport move this file
 * would then blame on the component. Every real click is gated on this. */
async function assertToggleOnScreen(page: any, stepId: string, label: string) {
  const box = await page.evaluate((id: string) => {
    const sc = document.querySelector(".tasks") as HTMLElement;
    const btn = document.querySelector(
      `[data-step-id='${id}'] [data-testid='step-note-toggle']`
    ) as HTMLElement;
    const r = btn.getBoundingClientRect();
    const sr = sc.getBoundingClientRect();
    return { top: r.top, bottom: r.bottom, viewTop: sr.top, viewBottom: sr.top + sc.clientHeight };
  }, stepId);
  expect(
    box.top,
    `${label}: the ${stepId} toggle is above the scrollport — Playwright would scroll it into view and this test would measure the harness`
  ).toBeGreaterThanOrEqual(box.viewTop);
  expect(
    box.bottom,
    `${label}: the ${stepId} toggle is below the fold — same problem`
  ).toBeLessThanOrEqual(box.viewBottom);
}

function clickToggle(cmp: any, stepId: string) {
  return cmp
    .locator(`[data-step-id='${stepId}'] [data-testid='step-note-toggle']`)
    .click();
}

/** Toggle without any scrolling by the harness — for rows deliberately left
 * off screen, where a real click is impossible anyway. */
async function jsToggle(page: any, stepId: string) {
  await page.evaluate((id: string) => {
    (
      document.querySelector(
        `[data-step-id='${id}'] [data-testid='step-note-toggle']`
      ) as HTMLElement
    ).click();
  }, stepId);
}

/** How tall the row becomes once its note is open. Opened and closed again, so
 * the caller can park it at a position that GUARANTEES the overflow the ticket
 * reported instead of guessing a fraction — a guess that is right at one width
 * is wrong at the other (measured: 243px tall at 390, 153px at 1280). */
async function openedHeight(page: any, stepId: string) {
  await jsToggle(page, stepId);
  const m = await measure(page, [stepId]);
  await jsToggle(page, stepId);
  return m!.rows[stepId].height;
}

/** Park the row so that opening it pushes its bottom `overflowBy` px past the
 * fold — the reported defect, reproduced deliberately at every width, while
 * leaving the toggle itself comfortably on screen. */
async function parkForOverflow(
  page: any,
  stepId: string,
  height: number,
  overflowBy: number
) {
  await page.evaluate(
    ({ id, height, overflowBy }: any) => {
      const sc = document.querySelector(".tasks") as HTMLElement;
      const wrap = document.querySelector(
        `[data-step-id='${id}'] .task-step__notewrap`
      ) as HTMLElement;
      const viewBottom = sc.getBoundingClientRect().top + sc.clientHeight;
      const want = viewBottom - height + overflowBy;
      sc.scrollTop += wrap.getBoundingClientRect().top - want;
    },
    { id: stepId, height, overflowBy }
  );
}

/** Put the clicked row where opening it CANNOT fit below it. A row taller than
 * the whole scrollport has no such position — parking it near the top is
 * already the overflow case — so the two branches are genuinely different
 * setups, not a tuning knob. */
async function parkForTest(page: any, c: (typeof CASES)[number], stepId: string) {
  if (c.tall) {
    await park(page, stepId, 0.15);
  } else {
    const h = await openedHeight(page, stepId);
    await parkForOverflow(page, stepId, h, 60);
  }
}

for (const c of CASES) {
  test(`[${c.name}] opening a note does not move the scrollport`, async ({
    mount,
    page,
  }) => {
    const cmp = await mountExpanded(mount, page, c);
    // Open the notes below so the column has scroll headroom past 節點 5 — a
    // row parked against the very end of the range would make "scrollTop did
    // not change" true for the wrong reason.
    for (const id of ["s-8", "s-9"]) await jsToggle(page, id);
    await parkForTest(page, c, "s-5");

    const before = await measure(page, ["s-5", "s-6"]);
    assertScrollportPremise(before, c.name);
    await assertToggleOnScreen(page, "s-5", c.name);

    await clickToggle(cmp, "s-5");
    const after = await measure(page, ["s-5", "s-6"]);

    const grew = after!.rows["s-5"].height - before!.rows["s-5"].height;
    expect(grew, `${c.name}: opening must add real height to the row`).toBeGreaterThan(20);
    // NON-VACUITY: the note must not fit in what was left below the toggle —
    // this IS the situation the owner reported. A fixture or font change that
    // quietly makes it fit turns everything below into a tautology, so it fails
    // here instead, loudly.
    expect(
      before!.rows["s-5"].top + after!.rows["s-5"].height,
      `${c.name}: the fixture no longer overflows the fold — nothing is being tested`
    ).toBeGreaterThan(after!.viewBottom + 5);
    if (c.tall) {
      expect(
        after!.rows["s-5"].height,
        `${c.name}: the row must exceed the scrollport for this case to mean anything`
      ).toBeGreaterThan(after!.viewHeight + 50);
    }

    // ① THE FEATURE: the scrollport did not move. Exactly, not approximately.
    expect(
      after!.scrollTop,
      `${c.name}: opening the note scrolled \`.tasks\` (${before!.scrollTop} → ${after!.scrollTop})`
    ).toBe(before!.scrollTop);
    // ② 「單純往下展開」: the row grows downward out of its own toggle, so the
    //    toggle the user pressed is still under their finger.
    expect(
      after!.rows["s-5"].top - before!.rows["s-5"].top,
      `${c.name}: the clicked row's top edge moved`
    ).toBeLessThanOrEqual(0.5);
    expect(
      after!.rows["s-5"].top - before!.rows["s-5"].top,
      `${c.name}: the clicked row's top edge moved`
    ).toBeGreaterThanOrEqual(-0.5);
    // ③ …and the row below moved down by EXACTLY the height that appeared —
    //    nothing more (a scroll correction) and nothing less.
    expect(
      after!.rows["s-6"].top - before!.rows["s-6"].top,
      `${c.name}: the row below did not follow the growth exactly`
    ).toBeCloseTo(grew, 0);
  });

  test(`[${c.name}] collapsing a note does not move the scrollport, and lifts the rows below by exactly what went away`, async ({
    mount,
    page,
  }) => {
    // 🔴 THIS REPLACES T-4e39's collapse test, which measured the row that was
    // CLICKED. That row is the only one a collapse cannot move (everything a
    // collapse removes sits below its own toggle), so the old test read like
    // "collapse is already safe" while having no power at all: it stayed green
    // with the correction, without it, and under every mutant. What is measured
    // here instead is 節點 6 — the row BELOW the one being closed.
    const cmp = await mountExpanded(mount, page, c);
    // 同時開多則: 節點 4/5 above, 8/9 below for scroll headroom.
    for (const id of ["s-4", "s-5", "s-8", "s-9"]) await jsToggle(page, id);
    await park(page, "s-5", 0.15);

    const before = await measure(page, ["s-5", "s-6"]);
    assertScrollportPremise(before, c.name);
    await assertToggleOnScreen(page, "s-5", c.name);

    await clickToggle(cmp, "s-5");
    const after = await measure(page, ["s-5", "s-6"]);

    const removed = before!.rows["s-5"].height - after!.rows["s-5"].height;
    expect(removed, `${c.name}: the note must actually have closed`).toBeGreaterThan(20);
    // This case is deliberately NOT the clamped one — the range still reaches
    // past the current offset after the collapse, so the browser has no excuse
    // to move anything and the equality below is strict. (The clamped case is
    // the last test in this file, and it is asserted just as exactly.)
    expect(
      before!.scrollTop,
      `${c.name}: this case must have scroll headroom left after the collapse, or it is the clamp test in disguise`
    ).toBeLessThanOrEqual(after!.maxScroll);

    // ① THE FEATURE.
    expect(
      after!.scrollTop,
      `${c.name}: collapsing moved \`.tasks\` (${before!.scrollTop} → ${after!.scrollTop})`
    ).toBe(before!.scrollTop);
    // ② 「向上收合」: the row below rises by exactly the height that went away.
    //    More than that would be a scroll correction dragging it; less (or a
    //    push downward) would be the screen being scrolled the other way.
    expect(
      after!.rows["s-6"].top - before!.rows["s-6"].top,
      `${c.name}: the row below the collapse did not rise by exactly the note's height (${removed}px)`
    ).toBeCloseTo(-removed, 0);
  });
}

test("[390×844 phone · dark] three notes opened in turn never move the scrollport", async ({
  mount,
  page,
}) => {
  // The owner's follow-up shape: 第 2、第 5、第 9 則 opened one after another.
  // Under T-4e39 every one of these clicks re-scrolled the container; the point
  // of the sequence is that now none of them does.
  const cmp = await mountExpanded(mount, page, CASES[0]);
  const overflowed: string[] = [];
  for (const id of ["s-2", "s-5", "s-9"]) {
    await parkForTest(page, CASES[0], id);
    const before = await measure(page, [id]);
    assertScrollportPremise(before, `${CASES[0].name} · ${id}`);
    await assertToggleOnScreen(page, id, `${CASES[0].name} · ${id}`);
    await clickToggle(cmp, id);
    const after = await measure(page, [id]);
    if (before!.rows[id].top + after!.rows[id].height > after!.viewBottom + 5) {
      overflowed.push(id);
    }
    expect(
      after!.scrollTop,
      `${CASES[0].name} · ${id}: opening scrolled \`.tasks\` (${before!.scrollTop} → ${after!.scrollTop})`
    ).toBe(before!.scrollTop);
    expect(
      after!.rows[id].top - before!.rows[id].top,
      `${CASES[0].name} · ${id}: the clicked row's top edge moved`
    ).toBeCloseTo(0, 0);
  }
  // Which of the three actually opens past the fold is a fact about the
  // fixture, pinned rather than assumed: s-2 sits so near the top of the column
  // that parking it against the bottom edge would need a NEGATIVE scrollTop, so
  // it is the trivially-safe member of the sequence and carries no proof — it
  // is here to describe the sequence the owner asked about. If a fixture edit
  // ever changes which ones reproduce, this line reddens instead of the guard
  // going quietly weak.
  expect(overflowed).toEqual(["s-5", "s-9"]);
  await expect(cmp.locator("[data-testid='step-note']")).toHaveCount(3);
});

for (const c of [CASES[0], CASES[2], CASES[4], CASES[6]]) {
  test(`[${c.name}] collapsing at the end of the scroll range — the browser's clamp is the only movement allowed`, async ({
    mount,
    page,
  }) => {
    // 🔴 THE PHYSICAL LIMIT, MEASURED, NOT WAVED AWAY. Closing a note shortens
    // the scrollable range. Parked at the end of that range, the offset the
    // reader is at no longer exists, and the browser clamps scrollTop to the
    // new maximum — the scrollport moves, and no code in the component did it.
    // "Do not move" is therefore not literally achievable here, and rather than
    // widen the tolerance until a real defect fits through, this pins the
    // clamped value EXACTLY: min(before, newMax). A component that scrolls by
    // even one px more or less than the clamp fails.
    //
    // The forced movement MEASURED on this fixture — see the header table for
    // all four rows; the short cases give −132 / −222 and the tall ones −676 /
    // −719 (there the note is taller than the offset, so the clamp runs out of
    // headroom before it has undone the whole height). The failure messages
    // print the live numbers, so a fixture change updates the evidence rather
    // than rotting it.
    //
    // 節點 9 is the LAST step on purpose: closing the last note is the only way
    // to shorten the range under the offset while the toggle you press is still
    // on screen. Its own row is what gets measured, because there is no row
    // below it — and its own row is the honest victim here: the reader pressed
    // 收合 and the text slid DOWN the screen anyway.
    const cmp = await mountExpanded(mount, page, c);
    await jsToggle(page, "s-9");
    await park(page, "s-9", 0.05);

    const before = await measure(page, ["s-9"]);
    assertScrollportPremise(before, c.name);
    await assertToggleOnScreen(page, "s-9", c.name);

    await clickToggle(cmp, "s-9");
    const after = await measure(page, ["s-9"]);

    const removed = before!.rows["s-9"].height - after!.rows["s-9"].height;
    expect(removed, `${c.name}: the note must actually have closed`).toBeGreaterThan(20);
    const forced = before!.scrollTop - after!.scrollTop;
    // NON-VACUITY: this test only means something while the clamp really bites.
    expect(
      forced,
      `${c.name}: nothing was clamped — this case has stopped exercising the limit it exists for`
    ).toBeGreaterThan(20);
    expect(
      after!.scrollTop,
      `${c.name}: the browser's clamp is the only movement allowed (forced ${forced}px; expected min(${before!.scrollTop}, ${after!.maxScroll}))`
    ).toBe(Math.min(before!.scrollTop, after!.maxScroll));
    // Nothing ABOVE the collapsed row changed, so the row moves down by exactly
    // the clamp and by nothing else. A component that added its own scroll on
    // top of the clamp fails here.
    expect(
      after!.rows["s-9"].top - before!.rows["s-9"].top,
      `${c.name}: the collapsed row moved by more than the clamp accounts for`
    ).toBeCloseTo(forced, 0);
  });
}
