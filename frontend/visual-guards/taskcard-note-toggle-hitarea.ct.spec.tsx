// HOTSPOT — T-6630 ②: the step-note disclosure is a whole ROW, and it is
// visibly the note's own row.
//
// Owner 2026-08-16:「展開備注或收和備注的按鈕太小,應該要是整列,而不是一個小按
// 鈕,會讓人誤以為是收和備注,實際收合了整個任務」.
//
// WHY THE COMPLAINT IS A HIT-AREA FACT AND NOT A TASTE ONE: the whole
// <article class="task-card"> carries role="button", and its click handler
// vetoes only hits that land on an interactive element (`closest("button, a,
// textarea, …")`). The note disclosure was a 66×16px button with `padding: 0`
// inside that surface — a tiny exempt island in a field where every other pixel
// collapses the ENTIRE task. Missing it by a few px did not do nothing; it did
// the other, bigger thing, and the two controls speak the same chevron
// vocabulary. So this file measures the two properties that make the mistake
// hard: how much of the row answers to the control, and whether the row still
// goes through the note's handler rather than the card's.
//
// SHAPE (owner 2026-08-16, first acceptance round:「展開備注的時候,備註不是應該
// 要在匡裡面嗎?」): the panel is the WRAP — this row is its header, the note is
// its body. So the row is measured against the wrap's INNER width; the border
// belongs to the panel, not to the row.
//
// WHY CT AND NOT JSDOM: every claim is a box, a hit test, or a composited
// colour. jsdom has no layout engine, so a jsdom version passes on a 0×0
// control painted in nothing.
//
// 🔴 WHAT THIS FILE DELIBERATELY DOES NOT MEASURE: how the row LOOKS. There is
// no contrast assertion and no per-theme run, by owner ruling (2026-08-16):
// (and since that ruling the panel stopped being a hand-mixed tint anyway — it
// takes the theme's own `--color-bg` / `--color-border`, so "what colour is it"
// is a question for the theme pack, not for this file):
// 「不需要驗證什麼顏色好不好,這種都是負責人一開始確認沒問題就好,我們不會去改
// 這種東西」. An earlier revision of this file did pin a composited contrast
// floor across both themes; it was removed on that ruling, not because it did
// not work. Appearance is signed off once, by eye, by him — so if the row's
// tint is ever changed, that is a change to take back to him, and no test here
// will notice it. Everything below is about SIZE, REACH and SEMANTICS, which
// are not matters of taste.
//
// WHY THE STORY IS MOUNTED BARE (no `.app__main` / `.tasks` ancestor chain,
// unlike the two anchor stories): nothing here is a scroll or an available-width
// measurement. Every assertion is either relative (the control against the row
// it sits in) or local (its own colours, its own semantics), and those do not
// change when the card gets a scrollport. The repo's warning about bare mounts
// under-reporting real layout applies to the overflow/scroll family of guards —
// which is why the ③ story does reproduce the chain.
//
// MUTANT REGISTER (each planted IN PLACE on the declaration named, run, and
// observed — counts are against the 10 tests below and expire if the case list
// changes):
//   N1 · tasks.css: the row back to the shipped-before inline shape
//        (`display:inline-flex; width:auto; min-height:0; padding:0`) AND the
//        wrap back to `align-items: flex-start`
//        ⇒ 4 failed / 4 passed — "the whole row, and a 44px touch target" AND
//        "every edge of the row answers to the note's control", both widths.
//        🔴 BOTH HALVES ARE REQUIRED TO PLANT IT. The row spanning its panel is
//        now over-determined: `width:100%` on the row and `align-items:stretch`
//        on the wrap each achieve it alone. Reverting only one leaves the row
//        full width and the file 8/8 GREEN — measured, twice — which reads
//        exactly like "the guard has no teeth". A mutant that does not change
//        the thing you are measuring proves nothing about the guard.
//   (An N2 that dropped the row's background and border used to live here and
//    is gone with the contrast test it reddened — see the ruling above. Nothing
//    in this repo now watches how the row is painted; that is deliberate, not an
//    oversight, and it is written down so the next reader does not "restore
//    coverage" the owner declined.)
//   N3 · TaskCard.tsx: render the control as a <div> instead of a <button>
//        ⇒ 4 failed / 6 passed — "clicking the far edge opens the note and
//        leaves the card open" and "did not cost the keyboard or a screen
//        reader", both widths. The card-toggle filter stops exempting it, so
//        the note row collapses the WHOLE card: the reported bug made worse.
//        This is why a widened control must stay a real button.
import { test, expect } from "@playwright/experimental-ct-react";
import { TaskCardNoteDisclosureStory } from "./stories/TaskCardNoteDisclosureStory";

const WIDTHS = [1280, 390];
const MIN_TOUCH = 44;
async function mountExpanded(mount: any, page: any, width: number) {
  await page.setViewportSize({ width, height: 1000 });
  const cmp = await mount(<TaskCardNoteDisclosureStory />);
  await cmp.locator(".task-card__head").first().click();
  await expect(cmp.locator(".task-card__workflow")).toBeVisible();
  return cmp;
}

for (const width of WIDTHS) {
  test(`[${width}] the note disclosure is the whole row, and a 44px touch target`, async ({
    mount,
    page,
  }) => {
    await mountExpanded(mount, page, width);

    const m = await page.evaluate(() => {
      const step = document.querySelector(
        "[data-step-id='s-note']"
      ) as HTMLElement;
      const wrap = step.querySelector(".task-step__notewrap") as HTMLElement;
      const btn = step.querySelector(
        "[data-testid='step-note-toggle']"
      ) as HTMLElement;
      const b = btn.getBoundingClientRect();
      const w = wrap.getBoundingClientRect();
      const s = step.getBoundingClientRect();
      return {
        btn: { w: b.width, h: b.height },
        // The row is compared against the wrap's INNER width: since the panel's
        // border moved out to the wrap (owner's 「備註要在框裡面」), the row is
        // its content, and a border-box comparison would be off by the border
        // and read as "the row no longer spans its container".
        wrapInnerW: wrap.clientWidth,
        wrapW: w.width,
        stepW: s.width,
      };
    });

    // The row, not a fragment of it. Measured against the container it sits in
    // rather than a pinned pixel count, so it holds at any width.
    expect(
      m.btn.w,
      "the control must span its whole row"
    ).toBeCloseTo(m.wrapInnerW, 0);
    // …and that row is really the step's width, not a shrunken column (a wrap
    // that collapsed to fit-content would make the line above trivially true).
    expect(
      m.wrapW / m.stepW,
      "the row itself must span the step, not fit its own content"
    ).toBeGreaterThan(0.85);
    expect(
      m.btn.h,
      "a thumb has to be able to hit it"
    ).toBeGreaterThanOrEqual(MIN_TOUCH);
  });

  test(`[${width}] every edge of the row answers to the note's control, not the card's`, async ({
    mount,
    page,
  }) => {
    await mountExpanded(mount, page, width);

    const hits = await page.evaluate(() => {
      const step = document.querySelector(
        "[data-step-id='s-note']"
      ) as HTMLElement;
      const btn = step.querySelector(
        "[data-testid='step-note-toggle']"
      ) as HTMLElement;
      const wrap = step.querySelector(".task-step__notewrap") as HTMLElement;
      const b = btn.getBoundingClientRect();
      const w = wrap.getBoundingClientRect();
      // 🔴 The probe box is the ROW (the wrap's full width at the control's own
      // height), NOT the control's own box. Probing the control's own box is
      // the version of this test that cannot fail: every point inside a 66×16
      // button still answers as that button, so it would stay green on exactly
      // the shape this ticket removed. MEASURED — that weaker form passed under
      // mutant N1.
      const r = { left: w.left, right: w.right, top: b.top, bottom: b.bottom, width: w.width, height: b.height };
      const at = (x: number, y: number) => {
        const el = document.elementFromPoint(x, y) as HTMLElement | null;
        if (!el) return "NONE";
        if (el.closest("[data-testid='step-note-toggle']")) return "TOGGLE";
        // anything else in this card is the card's own toggle surface
        return el.closest("[data-testid='task-card']") ? "CARD" : "OUTSIDE";
      };
      return {
        left: at(r.left + 3, r.top + r.height / 2),
        right: at(r.right - 3, r.top + r.height / 2),
        top: at(r.left + r.width / 2, r.top + 2),
        bottom: at(r.left + r.width / 2, r.bottom - 2),
      };
    });
    // A POSITIVE-CONTROL note: "CARD" is a value this probe really can return —
    // it is what the pixels just outside the row return, and what every one of
    // these four returned before this ticket.
    expect(hits).toEqual({
      left: "TOGGLE",
      right: "TOGGLE",
      top: "TOGGLE",
      bottom: "TOGGLE",
    });
  });

  test(`[${width}] clicking the far edge of the row opens the note and leaves the card open`, async ({
    mount,
    page,
  }) => {
    const cmp = await mountExpanded(mount, page, width);
    const box = (await cmp
      .locator("[data-testid='step-note-toggle']")
      .boundingBox())!;

    // 3px from the right edge — the part of the row that did not exist before,
    // and that used to belong to "collapse the whole task".
    await page.mouse.click(box.x + box.width - 3, box.y + box.height / 2);

    await expect(cmp.locator("[data-testid='step-note']")).toBeVisible();
    await expect(
      cmp.locator(".task-card__workflow"),
      "the card must NOT have collapsed — this is the bug the widening must not make worse"
    ).toBeVisible();
  });

  test(`[${width}] widening it did not cost the keyboard or a screen reader`, async ({
    mount,
    page,
  }) => {
    const cmp = await mountExpanded(mount, page, width);
    const toggle = cmp.locator("[data-testid='step-note-toggle']");

    const closed = await page.evaluate(() => {
      const btn = document.querySelector(
        "[data-testid='step-note-toggle']"
      ) as HTMLElement;
      return {
        tag: btn.tagName,
        expanded: btn.getAttribute("aria-expanded"),
        // Collapsed, the note is not in the DOM at all — so the attribute must
        // NOT be there either. An aria-controls naming an id that does not
        // exist announces a relationship the page does not have.
        controls: btn.getAttribute("aria-controls"),
        nestedInteractive: btn.querySelectorAll(
          "button, a, input, select, textarea, [role='button']"
        ).length,
      };
    });
    expect(closed.tag).toBe("BUTTON");
    expect(closed.expanded).toBe("false");
    expect(closed.controls, "no dangling aria-controls while collapsed").toBeNull();
    expect(closed.nestedInteractive).toBe(0);

    await toggle.focus();
    expect(
      await page.evaluate(
        () => document.activeElement?.getAttribute("data-testid") ?? "NONE"
      ),
      "the row must be reachable by keyboard"
    ).toBe("step-note-toggle");

    // BOTH activation keys, not just Enter. A <button> answers to Space as
    // well, and Space is also the key that scrolls a page — if the card's own
    // keydown handler ever stops checking that the event started on the card
    // itself, Space is the route that collapses the whole task while Enter
    // still looks fine.
    await page.keyboard.press("Enter");
    await expect(cmp.locator("[data-testid='step-note']")).toBeVisible();
    await expect(
      cmp.locator(".task-card__workflow"),
      "Enter on the note row must not collapse the card"
    ).toBeVisible();

    // aria-controls appears with the note and POINTS AT it.
    const open = await page.evaluate(() => {
      const btn = document.querySelector(
        "[data-testid='step-note-toggle']"
      ) as HTMLElement;
      const id = btn.getAttribute("aria-controls");
      const el = id ? document.getElementById(id) : null;
      return {
        id,
        resolves: !!el && el.getAttribute("data-testid") === "step-note",
      };
    });
    expect(open.id, "aria-controls must be present once the note is open").not.toBeNull();
    expect(open.resolves, "aria-controls must resolve to the note").toBe(true);
    await expect(toggle).toHaveAttribute("aria-expanded", "true");

    await page.keyboard.press(" ");
    await expect(cmp.locator("[data-testid='step-note']")).toHaveCount(0);
    await expect(
      cmp.locator(".task-card__workflow"),
      "Space on the note row must not collapse the card either"
    ).toBeVisible();
  });
}
