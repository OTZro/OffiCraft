// HOTSPOT — T-4e39: 點開哪一則步驟備註,畫面就停在那一則.
//
// Owner (2026-08-15, via T-e5b1's screenshot gate, on a real server at a real
// window height): opening a note left the row he had just opened off screen —
// 「像在一疊紙中間插進去十張」. MEASURED then: at 390×844 one open note already
// ended at y 853 against a 844-tall window, and at 1280×800 the third one left
// the viewport entirely.
//
// WHAT THIS FILE ASSERTS, and what it deliberately does not:
//   * the invariant is GEOMETRY THE OWNER CAN SEE — where the clicked note's
//     box lands inside the scrollport after the reflow. It is NOT "a class was
//     added", and it is NOT "scrollIntoView was called": that API's block /
//     nearest handling differs between engines and it re-targets every
//     scrollport on the ancestor chain, so calling it proves nothing about
//     where anything ended up.
//   * the scrollport is the INNER container (`.tasks`), which is why the story
//     reproduces the real ancestor chain. Each test proves that premise before
//     it measures anything — a document-level scroll would mean the guard is
//     measuring some other layout than the one that ships.
//   * only the note that was JUST CLICKED is claimed. Notes opened by earlier
//     clicks are allowed to drift: one click can only anchor one row, so this
//     is the unavoidable price of anchoring at all, not something the ticket
//     scoped out.
//   * 🔴 COLLAPSE IS UNTOUCHED, AND THAT IS A KNOWN HOLE, NOT A PROOF.
//     The test at the bottom shows the row YOU CLICKED does not move when it
//     collapses — but that row is precisely the only one that cannot move.
//     Collapsing a note ABOVE the one you are reading drags the one you are
//     reading upward: MEASURED at 390×844 in a real Chromium, open step 1, open
//     step 5, read step 5, then collapse step 1 ⇒ step 5 goes from top 375 to
//     top 907, i.e. 532px and completely off screen. ⚠️ The DISTANCE is not a
//     constant — it moves with the scroll position and the note's length; this
//     story's own fixture gives 506px at noteRepeat=1 and 903px at noteRepeat=8.
//     What is stable is that it leaves the window. That is the same defect as
//     this ticket's, on the other half. Leaving it is this ticket's scope
//     ruling, and the follow-up is T-6630 — which is NOT automatically a "yes":
//     it has to be judged on how often the situation really arises, and closing
//     it with a written reason is a legitimate outcome. Do not read the collapse
//     test below as saying collapse is fine.
// MUTANTS MEASURED (this file is their home; each was planted IN PLACE — on the
// declaration named, not appended at the end of the file, which would be a
// different program):
// ⚠️ THESE COUNTS ARE BOUND TO THE CASE LIST BELOW. Add or remove a sample and
// they expire — re-plant and re-measure rather than editing the prose. (They
// already went stale once here: M1/M3 were recorded at 6 cases and left
// unchanged when the `tall` samples took the list to 10.)
//   M1 · TaskCard.tsx, inside the openNotes useLayoutEffect, the statement
//        `if (wrap) keepAnchored(wrap, anchor.top);` → `if (wrap) void wrap;`
//        (i.e. the pre-T-4e39 tree). ⇒ 9 failed / 1 passed. Reported e.g.
//        "390×844 phone · dark: only 183px of a 243px row is on screen, and
//        243px would fit"; desktop 93 of 153.
//   M2 · scrollAnchor.ts, the `delta +=` line inside `anchorDelta`:
//        `Math.min(bottom - view.bottom, top - view.top)` → `bottom -
//        view.bottom` (reveal the bottom at any cost). ⇒ 4 failed / 6 passed —
//        every `tall: true` case, reporting "clicked row's top edge above the
//        scrollport": afterTop -833 at 390 wide, -157 at 1280 (expected >= 3).
//        Also killed by src/lib/scrollAnchor.test.ts ("keeps the row's top edge
//        on screen…", expected 200 to be 100), 1 failed / 6 passed.
//        ⚠️ It was NOT killed here until the tall sample existed: with only the
//        `tall: false` cases this file was 6/6 GREEN on M2, because no row was
//        ever taller than the scrollport. The first version of this header
//        wrote that off as a property of the two widths — it is a property of
//        the FIXTURE, and real notes break it daily (515-char median on this
//        site, 1790 in t-fc23, ~3.5k in T-e5b1's own step 4). Under M2 a reader
//        would watch the button and the note's first lines slide up to 577px
//        off the top of the screen.
//   M3 · scrollAnchor.ts, the initialiser inside `scrollParent`:
//        `let node = el.parentElement` → `let node = null`, so the correction is
//        applied to the DOCUMENT scrollport instead of `.tasks` — the exact
//        wrong-scrollport mistake the ticket warns about. ⇒ 9 failed / 1 passed,
//        same messages as M1: writing scrollTop on a box that does not scroll
//        moves nothing.
//   Under M1 and M3 the four `tall: false` cases stay red alongside the new
//   ones — the added samples widened the net without diluting it. The 1 that
//   stays green is the collapse test, correctly: it asserts a property that
//   holds with no correction code at all.
import { test, expect } from "@playwright/experimental-ct-react";
import { TaskCardNoteAnchorStory } from "./stories/TaskCardNoteAnchorStory";

// The two widths the ticket measured, plus the two shipped themes. 390×844 is
// the owner's phone (the width where the defect showed up on the SECOND note);
// 1280×800 is the desktop window (where it took three).
//
// `tall: true` is the SECOND sample, and it is not a synthetic worst case: real
// step notes on this site have a 515-character median, t-fc23's longest is 1790
// and T-e5b1's own step 4 runs to ~3.5k — every one of those makes a row taller
// than the window at 390 wide. In that branch the whole row cannot be shown, so
// the top edge has to win over revealing the bottom, and that is a DIFFERENT
// line of code (see M2 above).
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

/** The clicked row's box and its scrollport's, in viewport coordinates. */
async function measure(page: any, stepId: string) {
  return page.evaluate((id: string) => {
    const step = document.querySelector(`[data-step-id='${id}']`);
    const wrap = step?.querySelector(".task-step__notewrap") as HTMLElement;
    const note = step?.querySelector(
      "[data-testid='step-note']"
    ) as HTMLElement | null;
    const sc = document.querySelector(".tasks") as HTMLElement;
    if (!wrap || !sc) return null;
    const r = wrap.getBoundingClientRect();
    const sr = sc.getBoundingClientRect();
    const doc = document.scrollingElement!;
    return {
      top: r.top,
      bottom: r.bottom,
      height: r.height,
      noteHeight: note ? note.getBoundingClientRect().height : 0,
      viewTop: sr.top,
      viewBottom: sr.top + sc.clientHeight,
      viewHeight: sc.clientHeight,
      scrollable: sc.scrollHeight - sc.clientHeight,
      docScroll: doc.scrollHeight - doc.clientHeight,
    };
  }, stepId);
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

function clickToggle(cmp: any, stepId: string) {
  return cmp
    .locator(`[data-step-id='${stepId}'] [data-testid='step-note-toggle']`)
    .click();
}

/** How tall the row becomes once its note is open. Opened and closed again, so
 * the caller can park the row at a position that GUARANTEES the overflow the
 * ticket reported instead of guessing a fraction — a guess that is right at one
 * width is wrong at the other, because the same note wraps to fewer lines on a
 * 1280 card (measured: 243px at 390, ~half that at 1280). */
async function openedHeight(cmp: any, page: any, stepId: string) {
  await clickToggle(cmp, stepId);
  const m = await measure(page, stepId);
  await clickToggle(cmp, stepId);
  return m!.height;
}

/** Park the row so that opening it would push its bottom `overflowBy` px past
 * the fold — the defect, reproduced deliberately at every width. */
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

/**
 * The invariant, in one place so every case states it identically:
 *   ① the note really opened (otherwise everything below is vacuous);
 *   ② the clicked row's TOP edge is inside the scrollport — the control the
 *      reader pressed and the note's first line are where they can see them;
 *   ③ the reflow never pushed the clicked row DOWN;
 *   ④ as much of the row as the scrollport can hold IS on screen. This is the
 *      one the defect broke: the note opened below the fold and nothing moved.
 */
function assertLandedInView(
  before: any,
  after: any,
  label: string
) {
  expect(after.noteHeight, `${label}: the note must actually be open`).toBeGreaterThan(10);
  expect(
    after.top,
    `${label}: clicked row's top edge above the scrollport`
  ).toBeGreaterThanOrEqual(after.viewTop - 1);
  expect(
    after.top,
    `${label}: clicked row's top edge below the fold`
  ).toBeLessThanOrEqual(after.viewBottom - 20);
  expect(
    after.top,
    `${label}: the reflow pushed the clicked row further down`
  ).toBeLessThanOrEqual(before.top + 1);

  const visible =
    Math.min(after.bottom, after.viewBottom) - Math.max(after.top, after.viewTop);
  const couldFit = Math.min(after.height, after.viewHeight);
  expect(
    visible,
    `${label}: only ${Math.round(visible)}px of a ${Math.round(
      after.height
    )}px row is on screen, and ${Math.round(couldFit)}px would fit`
  ).toBeGreaterThanOrEqual(couldFit - 1);
}

for (const c of CASES) {
  test(`[${c.name}] opening a note leaves that note where it can be read`, async ({
    mount,
    page,
  }) => {
    const cmp = await mountExpanded(mount, page, c);

    // PREMISE. `.tasks` is the box that scrolls and the document is not — the
    // same shape measured on the live site. Without this the corrections below
    // would have nowhere to happen and the test would pass on any code.
    await park(page, "s-5", 0.5);
    const probe = await measure(page, "s-5");
    expect(probe, "fixture step s-5 must render a note row").not.toBeNull();
    expect(probe!.scrollable, "`.tasks` must be the scrollport").toBeGreaterThan(50);
    expect(probe!.docScroll, "the document must not be scrolling").toBeLessThanOrEqual(1);

    if (c.tall) {
      // A row that is TALLER than the scrollport cannot be parked against the
      // bottom edge — there is no position where its bottom fits. It is parked
      // a little way down instead, and what the correction has to do is bring
      // its TOP to the top of the scrollport and stop there.
      await park(page, "s-5", 0.25);
    } else {
      const h = await openedHeight(cmp, page, "s-5");
      await parkForOverflow(page, "s-5", h, 60);
    }
    const before = await measure(page, "s-5");

    // The row is parked so that the note it opens cannot fit below it — the
    // exact situation the owner hit.
    await clickToggle(cmp, "s-5");
    const after = await measure(page, "s-5");
    expect(
      after!.height,
      "opening must add real height to the row"
    ).toBeGreaterThan(before!.height + 20);
    // The tall case must really be tall, or it is just another copy of the
    // short one and M2 has nowhere to show up.
    if (c.tall) {
      expect(
        after!.height,
        `${c.name}: the row must exceed the scrollport for this case to mean anything`
      ).toBeGreaterThan(after!.viewHeight + 50);
    } else {
      expect(
        after!.height,
        `${c.name}: this case is supposed to FIT the scrollport`
      ).toBeLessThan(after!.viewHeight);
    }
    // NON-VACUITY for the whole case: had nothing moved, the opened row would
    // have run past the fold — this IS the reported situation, not a viewport
    // that happened to have room. A fixture or a font change that quietly makes
    // the note fit turns the assertions below into a tautology, so it fails
    // here instead, loudly.
    expect(
      before!.top + after!.height,
      `${c.name}: the fixture no longer overflows the fold — nothing is being tested`
    ).toBeGreaterThan(after!.viewBottom + 5);
    assertLandedInView(before, after, c.name);
  });
}

test("[390×844 phone · dark] each of three notes opened in turn lands where it can be read", async ({
  mount,
  page,
}) => {
  // The owner's follow-up question: 第 2、第 5、第 9 則 opened one after another.
  // Every click re-anchors on the row that click was on; the earlier ones are
  // free to move, which is the documented behaviour choice, not an oversight.
  const cmp = await mountExpanded(mount, page, CASES[0]);
  const reproduced: string[] = [];
  for (const id of ["s-2", "s-5", "s-9"]) {
    await park(page, id, 0.5);
    const h = await openedHeight(cmp, page, id);
    await parkForOverflow(page, id, h, 60);
    const before = await measure(page, id);
    await clickToggle(cmp, id);
    const after = await measure(page, id);
    if (before!.top + after!.height > after!.viewBottom + 5) reproduced.push(id);
    assertLandedInView(before, after, `${CASES[0].name} · ${id}`);
  }
  // Which of the three could be pushed to the fold at all is a fact about the
  // fixture, and it is pinned rather than assumed: s-2 sits so near the top of
  // the column that parking it against the bottom edge would need a NEGATIVE
  // scrollTop, so it is the trivially-safe member of the sequence and carries
  // no proof — it is here to describe the sequence the owner asked about. The
  // other two do reproduce the defect, and if a fixture edit ever changes that,
  // this line reddens instead of the guard going quietly vacuous.
  expect(reproduced).toEqual(["s-5", "s-9"]);

  // …and the earlier ones are still OPEN — the correction moves the scrollport,
  // it does not close anything.
  await expect(cmp.locator("[data-testid='step-note']")).toHaveCount(3);
});

test("[1280×800 desktop · dark] collapsing keeps the row that was clicked exactly where it was", async ({
  mount,
  page,
}) => {
  // 🔴 READ THE SCOPE NOTE IN THE HEADER BEFORE QUOTING THIS TEST.
  // It pins ONE narrow thing: the row you clicked keeps its y when it
  // collapses, because the content that goes away is below its toggle. That is
  // the ONLY row a collapse cannot move, so this test says nothing about
  // collapse being safe — collapsing a note above the one you are reading moves
  // that one 532px and off screen (measured; follow-up T-6630). This exists so
  // that if a later change breaks even this narrow property, it reddens.
  const cmp = await mountExpanded(mount, page, CASES[2]);
  await park(page, "s-5", 0.4);
  await clickToggle(cmp, "s-5");
  const open = await measure(page, "s-5");
  await clickToggle(cmp, "s-5");
  const closed = await measure(page, "s-5");
  expect(closed!.noteHeight, "the note must be closed again").toBe(0);
  expect(
    Math.abs(closed!.top - open!.top),
    "collapsing moved the row the reader clicked"
  ).toBeLessThanOrEqual(1);
});
