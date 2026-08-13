// HOTSPOT — T-791e: the boot-context block page rendering the REAL
// `seeds/system_interaction.md`, at phone widths.
//
// The gap this closes. `boot-doc-section-row.ct.spec.tsx` measures a
// hand-written FOUR-section document. The system-interaction seed splits into
// seventy-odd sections and carries what a synthetic fixture does not: fenced
// blocks, tables, long CJK headings, cited routes and ids. That page — the one
// an owner actually opens — had never been laid out by a browser. Four rows
// staying inside the card says nothing about seventy-four.
//
// 🔴 MEASURE THE WHOLE CHAIN, NOT ONE ELEMENT. An element that refuses to
// shrink does not overflow ITSELF — it simply grows, and the width it took has
// to end up somewhere: an ancestor is dragged wide, and if any ancestor
// scrolls it absorbs the spill and every number above it reads 0. So every
// level from the section row up to the scrolling element is measured, and
// `.settings` is named explicitly because it is `overflow-y: auto`, which the
// overflow spec coerces into `overflow-x: auto` — exactly the silent
// absorption that let an earlier page-only assertion sail over a broken phone
// (frontend/CLAUDE.md 〈浮層寬度不可用 vw 夾〉, and the repo paid for it again
// in T-ee17).
//
// LEGITIMATE SCROLL REGIONS ARE EXEMPT, AND ONLY THOSE. `.doc-md pre` and
// `.doc-md table` declare `overflow-x: auto` on purpose (frontend/CLAUDE.md
// 〈長 token 溢出〉 — flattening them to kill a page-level spill is the
// specific over-correction that section forbids). The subtree sweep therefore
// skips any element whose computed `overflow-x` is auto/scroll, and anything
// inside one. The NAMED chain above is not exempted from anything: a scroll
// region is allowed to hold wider content, it is not allowed to make the card
// or the page pan.
//
// Non-vacuity. The rendered row count is asserted against the story's own
// count, derived from the same seed bytes and the same splitter — a hard-coded
// 74 here would go stale on the next seed edit and would go stale quietly. A
// floor of 40 is asserted too, so the guard cannot degrade into measuring a
// four-row page and still read green.
//
// CONTROL: 1040 (the desktop content column's max width) is expected green and
// is NOT counted as coverage — it is there to say a fix did not simply move
// the breakage to desktop. The measured set is 320 / 375 / 390, matching this
// suite's existing widths.
//
// MEASURED ON THIS TREE (74 sections at all four widths; every level of the
// chain reads 0, so the page is NOT broken today — this guard was written to
// find out, and that is the answer):
//   drop `overflow-wrap: anywhere` from `.doc-md` (settings.css)
//     → RED at 320 only: "section 21 horizontal overflow (+45px)", doc card
//       +25, settings surface +24 — AND `.app__main`, `.app` and the PAGE all
//       read 0. A page-level assertion is green under this mutant. That is the
//       whole argument for walking the chain, measured rather than asserted:
//       `.settings` (overflow-y:auto) absorbs the spill, and the level that
//       actually spills is two below the first one anyone would think to
//       check. Green at 375/390/1040.
//   drop `overflow-wrap: anywhere` from `.boot-doc-sec__label` (boot-doc.css)
//     → GREEN at all four widths. Reported because it did not bite: this seed's
//       headings carry no unbreakable token, so the LABEL path is not covered
//       here. It is covered by `boot-doc-section-row.ct.spec.tsx`, whose
//       synthetic document exists partly to carry one — do not read this file
//       as making that one redundant, and do not delete that fixture's long
//       token thinking the real seed now supplies it.
import { test, expect } from "@playwright/experimental-ct-react";
import { BootDocRealSeedStory } from "./stories/BootDocRealSeedStory";

const FLOOR = 40;

for (const width of [320, 375, 390, 1040]) {
  test(`width ${width}: the real system_interaction seed lays out with no level of the chain panning`, async ({
    mount,
    page,
  }) => {
    await page.setViewportSize({ width, height: 1200 });
    const cmp = await mount(<BootDocRealSeedStory />);

    // The page reads its document through the adapter, so the first paint has
    // no rows. Wait on the row count itself rather than a timer — and assert
    // it against the story's derived expectation, so "found nothing to check"
    // can never read as "found nothing wrong".
    const expected = Number(
      await cmp.getByTestId("story-expected-sections").innerText()
    );
    expect(
      expected,
      "the seed must still split into a genuinely long document"
    ).toBeGreaterThanOrEqual(FLOOR);
    const sections = cmp.locator(".boot-doc-sec");
    await expect(sections).toHaveCount(expected);
    await expect(cmp.getByTestId("boot-doc-reset")).toBeVisible();

    const m = await page.evaluate(() => {
      const over = (el: Element) => el.scrollWidth - el.clientWidth;
      const scrolls = (el: Element) => {
        const ox = getComputedStyle(el).overflowX;
        return ox === "auto" || ox === "scroll";
      };
      const named: { where: string; over: number }[] = [];
      const push = (where: string, sel: string) => {
        const el = document.querySelector(sel);
        // -1 would PASS a `<= 1` assertion, so a level that vanished has to be
        // reported as a distinct, failing value rather than as an absence.
        named.push({ where, over: el ? over(el) : 9999 });
      };
      document
        .querySelectorAll(".boot-doc-sec")
        .forEach((el, i) => named.push({ where: `section ${i}`, over: over(el) }));
      document
        .querySelectorAll(".boot-doc-sec__head")
        .forEach((el, i) =>
          named.push({ where: `section ${i} head`, over: over(el) })
        );
      document
        .querySelectorAll(".boot-doc-sec__body")
        .forEach((el, i) =>
          named.push({ where: `section ${i} body`, over: over(el) })
        );
      push("doc card body", ".doc-card__body");
      push("doc card", ".doc-card");
      push("settings surface", ".settings");
      push("app main column", ".app__main");
      push("app shell", ".app");
      const se = document.scrollingElement!;
      named.push({ where: "page", over: se.scrollWidth - se.clientWidth });

      // Worst offender anywhere under the card, for a message that names what
      // to cap — "section 41 overflowed" alone is useless at this length.
      // Deliberate scroll regions and their descendants are skipped.
      let worst = { where: "(none)", over: 0 };
      const card = document.querySelector(".doc-card");
      if (card) {
        const walk = (el: Element) => {
          if (scrolls(el)) return;
          const o = over(el);
          if (o > worst.over) {
            const cls =
              typeof el.className === "string" && el.className.trim()
                ? "." + el.className.trim().split(/\s+/).join(".")
                : "";
            worst = { where: `${el.tagName.toLowerCase()}${cls}`, over: o };
          }
          for (const c of Array.from(el.children)) walk(c);
        };
        walk(card);
      }

      // The boxes the reader sees must also stay inside the viewport: a
      // scrollWidth check alone is satisfied by a card that grew and got
      // clipped by something upstream.
      const widthOf = (sel: string) =>
        document.querySelector(sel)?.getBoundingClientRect().width ?? -1;
      return {
        named,
        worst,
        boxes: {
          card: widthOf(".doc-card"),
          settings: widthOf(".settings"),
          main: widthOf(".app__main"),
        },
      };
    });

    for (const { where, over } of m.named) {
      expect(
        over,
        `${where} horizontal overflow at ${width}px — widest offender under the card: ${m.worst.where} (+${m.worst.over}px)`
      ).toBeLessThanOrEqual(1);
    }
    expect(
      m.worst.over,
      `something under the card is wider than itself at ${width}px: ${m.worst.where}`
    ).toBeLessThanOrEqual(1);
    for (const [name, w] of Object.entries(m.boxes)) {
      expect(w, `${name} box width at ${width}px`).toBeGreaterThan(0);
      expect(w, `${name} must stay within the viewport`).toBeLessThanOrEqual(
        width + 1
      );
    }
  });
}
