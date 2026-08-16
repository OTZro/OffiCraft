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
// WHY CT AND NOT JSDOM: every claim is a box, a hit test, or a composited
// colour. jsdom has no layout engine, so a jsdom version passes on a 0×0
// control painted in nothing.
//
// WHY BOTH THEMES: the row's surface is mixed from `--color-overlay`, and a
// theme pack re-values that. "Can you see this row" is therefore a per-theme
// question — light is the WEAKER of the two here, measured, so testing dark
// alone would guard the easy case.
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
//   N4 · tasks.css: fade the row's two mixes to nothing (20%/12% → 0.2%/0.2%),
//        leaving geometry, DOM and a11y untouched
//        ⇒ "the row is visibly the note's own band" FAILS in BOTH themes. This
//        mutant is why that test measures a COMPOSITED CONTRAST RATIO and not
//        the presence of a declaration: the first version of it asked only
//        "is a background declared, and is it not the step's own value", and an
//        independent review planted N4 against it for 10 passed / 0 failed. A
//        row nobody can see is exactly the state the owner reported.
//   N3 · TaskCard.tsx: render the control as a <div> instead of a <button>
//        ⇒ 4 failed / 6 passed — "clicking the far edge opens the note and
//        leaves the card open" and "did not cost the keyboard or a screen
//        reader", both widths. The card-toggle filter stops exempting it, so
//        the note row collapses the WHOLE card: the reported bug made worse.
//        This is why a widened control must stay a real button.
import { test, expect } from "@playwright/experimental-ct-react";
import { TaskCardNoteDisclosureStory } from "./stories/TaskCardNoteDisclosureStory";

const WIDTHS = [1280, 390];
const THEMES = ["dark", "light"] as const;
const MIN_TOUCH = 44;
// Regression floors, not a conformance claim. They sit just under what the
// shipped values measure in the WEAKER theme (light: 1.24 background, 1.44
// border; dark: 1.46 / 1.85), so the N4 "faded to nothing" mutant (≈1.00) dies
// while ordinary palette work does not trip on noise.
// 🔴 They are NOT WCAG 1.4.11 (which asks 3:1 of a control's visual boundary):
// reaching that here would need an outline several times heavier than anything
// else in this cockpit, and that is a look the owner has not been asked about.
// Raising these is a design decision, not a test edit.
const MIN_BG_CONTRAST = 1.18;
const MIN_BORDER_CONTRAST = 1.35;

const CASES = WIDTHS.flatMap((width) => THEMES.map((theme) => ({ width, theme })));

async function mountExpanded(
  mount: any,
  page: any,
  width: number,
  theme: (typeof THEMES)[number] = "dark"
) {
  await page.setViewportSize({ width, height: 1000 });
  const cmp = await mount(<TaskCardNoteDisclosureStory theme={theme} />);
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

// The colour half runs over widths × THEMES; the geometry, hit-test and
// keyboard halves above run per width only, because none of them can change
// with a palette — stating that rather than paying twice for the same answer.
for (const c of CASES) {
  test(`[${c.width} · ${c.theme}] the row is visibly the note's own band, not bare card surface`, async ({
    mount,
    page,
  }) => {
    // 「一眼看得出來」, made measurable. The earlier version of this test asked
    // whether a background was DECLARED, and an independent review killed it
    // with a row faded to 0.2% — declared, invisible, 10/10 green. So the
    // question asked here is the one the owner actually asked: how far does
    // this row stand out from the surface behind it.
    await mountExpanded(mount, page, c.width, c.theme);
    const paint = await page.evaluate(() => {
      // Composite each element's background down its ancestor chain until an
      // opaque one is reached — a `color-mix(… / 5%)` reports its own alpha,
      // and comparing two translucent declarations tells you nothing about
      // what the eye receives.
      const parse = (v: string): [number, number, number, number] | null => {
        let m = v.match(/^rgba?\(([^)]+)\)/);
        if (m) {
          const p = m[1].split(",").map((n) => parseFloat(n));
          return [p[0], p[1], p[2], p.length > 3 ? p[3] : 1];
        }
        // color(srgb r g b / a) — what color-mix() computes to
        m = v.match(/^color\(srgb ([^)]+)\)/);
        if (m) {
          const [rgb, a] = m[1].split("/");
          const p = rgb.trim().split(/\s+/).map((n) => parseFloat(n));
          return [p[0] * 255, p[1] * 255, p[2] * 255, a === undefined ? 1 : parseFloat(a)];
        }
        return null;
      };
      const overlay = (
        fg: [number, number, number, number],
        bg: [number, number, number]
      ): [number, number, number] => [
        fg[0] * fg[3] + bg[0] * (1 - fg[3]),
        fg[1] * fg[3] + bg[1] * (1 - fg[3]),
        fg[2] * fg[3] + bg[2] * (1 - fg[3]),
      ];
      /** The colour actually painted where `el` sits, own background included. */
      const composited = (el: HTMLElement, includeSelf: boolean): [number, number, number] => {
        const stack: [number, number, number, number][] = [];
        let n: HTMLElement | null = includeSelf ? el : el.parentElement;
        while (n) {
          const c = parse(getComputedStyle(n).backgroundColor);
          if (c && c[3] > 0) {
            stack.push(c);
            if (c[3] === 1) break;
          }
          n = n.parentElement;
        }
        let out: [number, number, number] = [255, 255, 255];
        for (let i = stack.length - 1; i >= 0; i--) out = overlay(stack[i], out);
        return out;
      };
      const lum = (rgb: [number, number, number]) => {
        const f = (v: number) => {
          const c = v / 255;
          return c <= 0.04045 ? c / 12.92 : Math.pow((c + 0.055) / 1.055, 2.4);
        };
        return 0.2126 * f(rgb[0]) + 0.7152 * f(rgb[1]) + 0.0722 * f(rgb[2]);
      };
      const contrast = (a: [number, number, number], b: [number, number, number]) => {
        const [hi, lo] = [lum(a), lum(b)].sort((x, y) => y - x);
        return (hi + 0.05) / (lo + 0.05);
      };
      const step = document.querySelector("[data-step-id='s-note']") as HTMLElement;
      const btn = step.querySelector("[data-testid='step-note-toggle']") as HTMLElement;
      const behind = composited(btn, false);
      const row = composited(btn, true);
      const bc = parse(getComputedStyle(btn).borderTopColor);
      const border = bc ? overlay(bc, row) : row;
      return {
        bgContrast: contrast(row, behind),
        borderContrast: contrast(border, row),
        borderWidth: parseFloat(getComputedStyle(btn).borderTopWidth),
        row,
        behind,
      };
    });
    expect(
      paint.bgContrast,
      `the row's surface must stand out from what is behind it (row ${paint.row} vs ${paint.behind})`
    ).toBeGreaterThanOrEqual(MIN_BG_CONTRAST);
    expect(paint.borderWidth, "the row must have a boundary").toBeGreaterThan(0);
    expect(
      paint.borderContrast,
      "…and that boundary must be visible against the row it encloses"
    ).toBeGreaterThanOrEqual(MIN_BORDER_CONTRAST);
  });
}
