// T-76cd — STACKING, not geometry.
//
// owner, on his iPhone, after the scrolling fix landed: 「看不到按關閉的按鈕,
// 被擋住了 且上面的 tab 全部都不能按 為什麼那個預覽畫面好像不是在最上面 而是在
// 裡面一層的感覺」. "裡面一層" is literally right, and it is a stacking-context
// bug, not a layout one.
//
// 🔴 WHY FOUR ROUNDS OF MEASUREMENT MISSED IT. Every earlier probe measured
// rects, computed max-height and scrollHeight/clientHeight. None of them
// measured PAINT ORDER, and the ancestor scan that did run checked
// transform/filter/backdrop-filter/perspective/contain/will-change/
// container-type — the containing-block traps — and never checked `z-index`,
// `opacity` or `isolation`, which are the stacking-context ones. The overlay's
// nearest stacking-context ancestor was `.task-artifacts`, caught only by the
// z-index rule. Measuring the wrong dimension is why it was green every time.
//
// The mechanism: MarkdownPreviewOverlay renders INSIDE the popover (no portal —
// it must stay within `anchorRef` or a backdrop click dismisses the popover,
// owner's 2026-07-20 ruling, guarded verbatim in TaskArtifactsPopover.tsx). So
// `.task-artifacts { z-index: 40 }` scoped the overlay's own `z-index: 1100` to
// that box, and the whole overlay subtree competed against the page as 40.
import { test, expect } from "@playwright/experimental-ct-react";
import { ArtifactsStackingStory } from "./stories/ArtifactsStackingStory";

/** The competitor condition. The shipped chrome is static/`z-index: auto`, so
 *  today the confinement is LATENT here — 40 happens to win. That is not a
 *  reason to leave the overlay confined: the bug appears the moment anything in
 *  the chrome outranks 40, which is a one-line change anyone can make without
 *  ever touching this file. These tests therefore state the competition
 *  explicitly rather than waiting for someone to introduce it.
 *
 *  ⚠️ This is NOT a claim that owner's build has exactly this rule. His actual
 *  ancestor chain and his chrome's z-index values have never been read — no one
 *  has inspected the running station's DOM. What is guarded is the invariant:
 *  the overlay must win against chrome regardless of the chrome's z-index.
 *
 *  (An earlier revision of this comment argued from his nav labels
 *  「市政廳/待核准/案件/調度」 not appearing anywhere in the repo that he must be
 *  running a different codebase. That inference was WRONG and is recorded here
 *  so it is not made again: `nav.office`/`nav.replies`/`nav.tasks`/`nav.monitor`
 *  are on the theme wording-override whitelist in i18n/messageKeys.generated.ts,
 *  so custom labels are SUPPOSED to be absent from the repo — they live in the
 *  owner's theme pack. The shipped defaults 「辦公室」/「請示」 do appear, in
 *  README.md, docs/design/SPEC.md and conformance/test_*.py. The observation was
 *  true; the conclusion drawn from it was not.) */
const CHROME_OUTRANKS = ".topbar, .nav-tabs { position: relative; z-index: 50; }";

// eslint-disable-next-line @typescript-eslint/no-explicit-any
async function openPreview(cmp: any) {
  await cmp.getByTestId("task-artifacts-badge").click();
  await expect(cmp.locator(".task-artifacts")).toBeVisible();
  await cmp.getByRole("button", { name: "Global Context.md" }).click();
  await expect(cmp.locator(".md-preview")).toBeVisible();
}

/** `page.evaluate` with a STRING body returns `unknown` — there is no source for
 *  tsc to infer from — so the shape is declared here and applied at the call
 *  sites. Without it every `seen.x` is a TS18046, which nothing in this repo
 *  would have told us: `visual-guards/` is in NO tsconfig (tsconfig.json
 *  includes only `src`, tsconfig.guards.json only `paint-guards`). */
type Probe = {
  overTopbar: string;
  overFirstTab: string;
  overClose: string;
  nearestStackingContextOfOverlay: string;
};

/** What is actually on top at a point — the hit-test oracle. Paint order and
 *  hit-testing follow the same rules, so this answers "who is painted above". */
const PROBE = `
(() => {
  const at = (x, y) => {
    const el = document.elementFromPoint(x, y);
    if (!el) return "nothing";
    const cls = (typeof el.className === "string" ? el.className : "") || "";
    const name = el.tagName.toLowerCase() + (cls ? "." + cls.split(/\\s+/)[0] : "");
    const where = el.closest(".md-preview__close") ? "CLOSE:"
      : el.closest(".md-preview") ? "OVERLAY:" : "BLOCKED:";
    return where + name;
  };
  const box = (s) => { const e = document.querySelector(s); return e && e.getBoundingClientRect(); };
  const tb = box(".topbar"), nb = box(".nav-tabs"), cb = box(".md-preview__close");
  return {
    overTopbar: tb ? at(tb.left + tb.width / 2, tb.top + tb.height / 2) : "no topbar",
    overFirstTab: nb ? at(nb.left + 40, nb.top + nb.height / 2) : "no tabs",
    overClose: cb ? at(cb.left + cb.width / 2, cb.top + cb.height / 2) : "no close",
    nearestStackingContextOfOverlay: (() => {
      let n = document.querySelector(".md-preview");
      if (!n) return "no overlay";
      for (n = n.parentElement; n; n = n.parentElement) {
        const cs = getComputedStyle(n);
        const pos = cs.position, z = cs.zIndex;
        const sc =
          ((pos === "absolute" || pos === "relative") && z !== "auto") ||
          pos === "fixed" || pos === "sticky" ||
          cs.opacity !== "1" || cs.isolation === "isolate" ||
          cs.transform !== "none" || cs.filter !== "none" ||
          n === document.documentElement;
        if (sc) {
          const cls = (typeof n.className === "string" ? n.className : "") || "";
          return n.tagName.toLowerCase() + (cls ? "." + cls.split(/\\s+/)[0] : "") + " z=" + z;
        }
      }
      return "none";
    })(),
  };
})()
`;

// DoD ① — the core assertion of this fix, with its before/after built in.
//
// MUTANTS (measured, `npx playwright test -c playwright-ct.config.ts
// visual-guards/artifacts-stacking.ct.spec.tsx`, 3 tests in this file):
//   · `z-index: 40` back on `.task-artifacts` — the BEFORE state — ⇒ 2 failed /
//     1 passed, and the failure message is owner's report verbatim:
//     overTopbar "BLOCKED:header.topbar" (this test) and nearest stacking
//     context "div.task-artifacts z=40" (the next one).
//   · `z-index: 1200` ⇒ 1 failed / 2 passed. It beats today's chrome, so THIS
//     test goes green — and the next one still reddens with
//     "div.task-artifacts z=1200". That pair is the whole reason the next test
//     exists: outranking the current competitor is not the same as not being
//     confined, and only the second one can tell them apart.
test("narrow 390: with chrome outranking the popover, the preview overlay still wins", async ({
  mount,
  page,
}) => {
  await page.setViewportSize({ width: 390, height: 780 });
  const cmp = await mount(<ArtifactsStackingStory />);
  await page.addStyleTag({ content: CHROME_OUTRANKS });
  await openPreview(cmp);

  const seen = (await page.evaluate(PROBE)) as Probe;

  // The overlay is on top where the chrome is — the tabs owner could not use.
  expect(seen.overTopbar).toContain("OVERLAY:");
  expect(seen.overFirstTab).toContain("OVERLAY:");
  // …and the close button is reachable, which is the affordance he lost.
  // (The hit lands on the icon's <path>, which is INSIDE the button — hence
  // containment, not element identity. Asserting the exact tag here made this
  // red on a working build.)
  expect(seen.overClose).toContain("CLOSE:");
  await cmp.locator(".md-preview__close").click();
  await expect(cmp.locator(".md-preview")).toHaveCount(0);
});

// DoD ① (structural half) — the overlay must not be CONFINED at all, which is
// strictly stronger than "it beats today's chrome". A z-index of 1200 on the
// popover passes the test above and fails this one.
test("the preview overlay participates in the root stacking context, not the popover's", async ({
  mount,
  page,
}) => {
  await page.setViewportSize({ width: 390, height: 780 });
  const cmp = await mount(<ArtifactsStackingStory />);
  await openPreview(cmp);

  const seen = (await page.evaluate(PROBE)) as Probe;
  // `.md-preview` is itself `position: fixed`, so the nearest stacking context
  // ABOVE it is what decides where its 1100 is resolved. Anything other than
  // the root means the overlay is scoped inside a box again.
  expect(seen.nearestStackingContextOfOverlay).toContain("html");
});

// DoD ② — the owner ruling this fix must not break. The overlay still renders
// inside the anchor, so a backdrop click stays inside it and the popover
// survives. This is the pairing control for the whole change: if a future
// "fix" reaches for createPortal, this reddens.
//
// MUTANT (measured): none needed for direction — `TaskArtifactsPopover.test.tsx`
// already covers the handler. This is the real-browser half: it asserts the
// popover is still THERE after a real backdrop click at real coordinates.
test("clicking the preview backdrop closes nothing — the popover survives (owner 2026-07-20)", async ({
  mount,
  page,
}) => {
  await page.setViewportSize({ width: 390, height: 780 });
  const cmp = await mount(<ArtifactsStackingStory />);
  await openPreview(cmp);

  // A point on the backdrop: beside the panel, well clear of it.
  const panel = (await cmp.locator(".md-preview__panel").boundingBox())!;
  await page.mouse.click(Math.max(3, panel.x / 2), panel.y + panel.height / 2);

  // The preview closes (backdrop dismissal is its own contract)…
  await expect(cmp.locator(".md-preview")).toHaveCount(0);
  // …and the popover behind it does NOT. This is the ruling.
  await expect(cmp.locator(".task-artifacts")).toBeVisible();
});
