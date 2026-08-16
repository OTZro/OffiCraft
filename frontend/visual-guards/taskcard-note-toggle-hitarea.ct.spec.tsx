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
// WHY CT AND NOT JSDOM: every claim is a box, a hit test, or a computed colour.
// jsdom has no layout engine, so a jsdom version passes on a 0×0 control.
//
// MUTANT REGISTER (each planted IN PLACE on the declaration named, run, and
// observed — counts are against the 10 tests below and expire if the case list
// changes):
//   N1 · tasks.css `.task-step__note-toggle` back to the shipped-before shape
//        (`display:inline-flex; width:auto; min-height:0; padding:0`)
//        ⇒ 4 failed / 6 passed — "the whole row, and a 44px touch target" AND
//        "every edge of the row answers to the note's control" at both widths.
//        ⚠️ The edge test only kills this because it probes the ROW's box. The
//        first version probed the control's OWN box and stayed GREEN under N1
//        (measured), because every point inside a 66×16 button is still that
//        button. A hit test is only a hit test relative to the area you claim.
//   N2 · drop the row's own background + border (`border:none; background:none`)
//        ⇒ 2 failed / 8 passed — "the row is visibly the note's own band" at
//        both widths. Geometry survives, correctly: N2 is a claim about looking
//        different, not about being big.
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
        wrapW: w.width,
        stepW: s.width,
      };
    });

    // The row, not a fragment of it. Measured against the container it sits in
    // rather than a pinned pixel count, so it holds at any width.
    expect(
      m.btn.w,
      "the control must span its whole row"
    ).toBeCloseTo(m.wrapW, 0);
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

  test(`[${width}] the row is visibly the note's own band, not bare card surface`, async ({
    mount,
    page,
  }) => {
    // 「一眼看得出來」, made measurable: the control paints a surface of its own.
    // Sharing the step's background is exactly the state the owner reported —
    // a control that looks like the card body, where clicking does the other,
    // bigger thing.
    await mountExpanded(mount, page, width);
    const paint = await page.evaluate(() => {
      const step = document.querySelector(
        "[data-step-id='s-note']"
      ) as HTMLElement;
      const btn = step.querySelector(
        "[data-testid='step-note-toggle']"
      ) as HTMLElement;
      const cs = getComputedStyle(btn);
      const parent = getComputedStyle(step);
      return {
        bg: cs.backgroundColor,
        stepBg: parent.backgroundColor,
        borderW: parseFloat(cs.borderTopWidth),
        borderColor: cs.borderTopColor,
      };
    });
    // Transparent is spelled two ways depending on how the value was authored
    // (a keyword vs a colour with a zero alpha channel), and `color-mix()`
    // resolves to a `color(srgb …)` string rather than `rgba(…)` — so the test
    // asks the question by NAME rather than by parsing a channel out of it.
    const TRANSPARENT = ["transparent", "rgba(0, 0, 0, 0)"];
    expect(
      TRANSPARENT,
      "the row must paint a background of its own"
    ).not.toContain(paint.bg);
    expect(paint.bg, "…and one the step's surface does not already have").not.toBe(
      paint.stepBg
    );
    expect(paint.borderW, "the row must have a visible boundary").toBeGreaterThan(0);
    expect(TRANSPARENT).not.toContain(paint.borderColor);
  });

  test(`[${width}] widening it did not cost the keyboard or a screen reader`, async ({
    mount,
    page,
  }) => {
    const cmp = await mountExpanded(mount, page, width);
    const toggle = cmp.locator("[data-testid='step-note-toggle']");

    const semantics = await page.evaluate(() => {
      const btn = document.querySelector(
        "[data-testid='step-note-toggle']"
      ) as HTMLElement;
      const controls = btn.getAttribute("aria-controls");
      return {
        tag: btn.tagName,
        role: btn.getAttribute("role"),
        expanded: btn.getAttribute("aria-expanded"),
        controls,
        // no nested interactive element inside the row (a button in a button is
        // both invalid and unreachable for a screen reader)
        nestedInteractive: btn.querySelectorAll(
          "button, a, input, select, textarea, [role='button']"
        ).length,
      };
    });
    expect(semantics.tag).toBe("BUTTON");
    expect(semantics.expanded).toBe("false");
    expect(semantics.nestedInteractive).toBe(0);

    await toggle.focus();
    expect(
      await page.evaluate(
        () => document.activeElement?.getAttribute("data-testid") ?? "NONE"
      ),
      "the row must be reachable by keyboard"
    ).toBe("step-note-toggle");

    await page.keyboard.press("Enter");
    await expect(cmp.locator("[data-testid='step-note']")).toBeVisible();
    await expect(
      cmp.locator(".task-card__workflow"),
      "Enter on the note row must not collapse the card"
    ).toBeVisible();

    // aria-controls has to POINT AT the note it opened — an attribute that
    // names nothing announces a relationship that does not exist.
    const resolves = await page.evaluate((id: string) => {
      const el = document.getElementById(id);
      return !!el && el.getAttribute("data-testid") === "step-note";
    }, semantics.controls!);
    expect(resolves, "aria-controls must resolve to the note").toBe(true);
    await expect(toggle).toHaveAttribute("aria-expanded", "true");
  });
}
