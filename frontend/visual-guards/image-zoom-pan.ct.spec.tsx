// T-7e68 — the zoomed image must actually be REACHABLE, in a real browser.
//
// The bug this guards against shipped because "the wrap has `overflow: auto`"
// was read as "the user can scroll it". It could not: the zoom lived entirely
// in the image's `transform`, which paints bigger pixels without changing the
// layout box, so there was never any scrollable content and every magnified
// edge was clipped away. Nothing in the CSS says that — only geometry does.
//
// So every assertion below measures WHERE THE PIXELS ARE: the painted rect of
// the <img> (a transform is reflected in getBoundingClientRect) against the
// frame's own client box. "Corner reachable" means that corner's coordinates
// land inside the visible box. No assertion here may be satisfied by the
// presence of a property, a class or an element.
import { test, expect } from "@playwright/experimental-ct-react";
import type { Locator, Page } from "@playwright/test";
import { ImageZoomPanStory } from "./stories/ImageZoomPanStory";

type Geometry = {
  image: { left: number; top: number; right: number; bottom: number };
  frame: { left: number; top: number; right: number; bottom: number };
  scrollLeft: number;
  scrollTop: number;
  overflowX: number;
  overflowY: number;
};

/** The painted image rect and the frame's VISIBLE box (client box, so a
 * classic scrollbar's gutter is excluded rather than fudged with a tolerance). */
async function geometry(wrap: Locator): Promise<Geometry> {
  return wrap.evaluate((el) => {
    const img = el.querySelector("img.md-preview__image") as HTMLImageElement;
    const i = img.getBoundingClientRect();
    const w = el.getBoundingClientRect();
    const left = w.left + el.clientLeft;
    const top = w.top + el.clientTop;
    return {
      image: { left: i.left, top: i.top, right: i.right, bottom: i.bottom },
      frame: { left, top, right: left + el.clientWidth, bottom: top + el.clientHeight },
      scrollLeft: el.scrollLeft,
      scrollTop: el.scrollTop,
      overflowX: el.scrollWidth - el.clientWidth,
      overflowY: el.scrollHeight - el.clientHeight,
    };
  });
}

const EPS = 1.5;
const inside = (p: { x: number; y: number }, f: Geometry["frame"]) =>
  p.x >= f.left - EPS && p.x <= f.right + EPS && p.y >= f.top - EPS && p.y <= f.bottom + EPS;

async function zoomTo400(cmp: Locator) {
  for (let i = 0; i < 12; i++) await cmp.getByRole("button", { name: "放大" }).click();
  await expect(cmp.getByText("400%")).toBeVisible();
}

/** A real mouse drag on the frame, started away from the floating zoom cluster. */
async function drag(page: Page, wrap: Locator, dx: number, dy: number) {
  const box = (await wrap.boundingBox())!;
  const x = box.x + box.width / 2;
  const y = box.y + box.height / 3;
  await page.mouse.move(x, y);
  await page.mouse.down();
  await page.mouse.move(x + dx, y + dy, { steps: 8 });
  await page.mouse.up();
}

async function mountStory(mount: (c: JSX.Element) => Promise<Locator>, page: Page) {
  await page.setViewportSize({ width: 900, height: 700 });
  const cmp = await mount(<ImageZoomPanStory />);
  const wrap = cmp.locator(".md-preview__image-wrap");
  await expect(wrap).toBeVisible();
  // The fit box is measured on load; wait for a settled non-zero image rect.
  await expect
    .poll(async () => (await geometry(wrap)).image.right - (await geometry(wrap)).image.left)
    .toBeGreaterThan(100);
  return { cmp, wrap };
}

test("at 100% the image sits wholly inside the frame with nothing to scroll", async ({ mount, page }) => {
  const { wrap } = await mountStory(mount, page);
  const g = await geometry(wrap);
  expect(g.image.left).toBeGreaterThanOrEqual(g.frame.left - EPS);
  expect(g.image.right).toBeLessThanOrEqual(g.frame.right + EPS);
  expect(g.image.top).toBeGreaterThanOrEqual(g.frame.top - EPS);
  expect(g.image.bottom).toBeLessThanOrEqual(g.frame.bottom + EPS);
  expect(g.overflowX).toBeLessThanOrEqual(1);
  expect(g.overflowY).toBeLessThanOrEqual(1);
});

test("zooming to 400% grows the LAYOUT, so the frame has real overflow to travel", async ({ mount, page }) => {
  const { cmp, wrap } = await mountStory(mount, page);
  const before = await geometry(wrap);
  const fitW = before.image.right - before.image.left;
  const fitH = before.image.bottom - before.image.top;

  await zoomTo400(cmp);
  const after = await geometry(wrap);

  // The painted image really is four times the fitted box…
  expect(after.image.right - after.image.left).toBeGreaterThan(fitW * 3.9);
  expect(after.image.bottom - after.image.top).toBeGreaterThan(fitH * 3.9);
  // …and the frame knows about it. This is the number that was 0 before the
  // fix: a pure `transform: scale()` leaves scrollWidth === clientWidth, and
  // then there is nowhere for any pan — drag, scrollbar or key — to go.
  expect(after.overflowX).toBeGreaterThan(fitW * 2);
  expect(after.overflowY).toBeGreaterThan(100);
});

test("dragging brings every magnified corner of the image into the visible frame", async ({ mount, page }) => {
  const { cmp, wrap } = await mountStory(mount, page);
  await zoomTo400(cmp);

  // At 400% the far corner is off the frame — that IS the owner's complaint.
  const zoomed = await geometry(wrap);
  expect(inside({ x: zoomed.image.right, y: zoomed.image.bottom }, zoomed.frame)).toBe(false);

  // Drag the content up-left → the BOTTOM-RIGHT corner pixel travels into view.
  for (let i = 0; i < 10; i++) await drag(page, wrap, -300, -200);
  const atBottomRight = await geometry(wrap);
  expect(
    inside({ x: atBottomRight.image.right, y: atBottomRight.image.bottom }, atBottomRight.frame),
    "the image's bottom-right corner must be draggable into the visible frame",
  ).toBe(true);
  // …and the drag genuinely travelled: the opposite corner is now off-frame.
  expect(inside({ x: atBottomRight.image.left, y: atBottomRight.image.top }, atBottomRight.frame)).toBe(false);

  // Drag back the other way → the TOP-LEFT corner pixel comes back into view.
  for (let i = 0; i < 10; i++) await drag(page, wrap, 300, 200);
  const atTopLeft = await geometry(wrap);
  expect(
    inside({ x: atTopLeft.image.left, y: atTopLeft.image.top }, atTopLeft.frame),
    "the image's top-left corner must be draggable back into the visible frame",
  ).toBe(true);
});

// The scrollbar route. Setting scrollLeft/scrollTop is exactly what dragging
// the frame's scrollbar does — and it only moves anything because the zoom is
// carried as layout. The assertion is still geometric: after travelling to the
// far end, the corner pixel must be inside the visible box.
test("the frame's own scroll travel reaches the far corner", async ({ mount, page }) => {
  const { cmp, wrap } = await mountStory(mount, page);
  await zoomTo400(cmp);
  await wrap.evaluate((el) => {
    el.scrollLeft = el.scrollWidth;
    el.scrollTop = el.scrollHeight;
  });
  const g = await geometry(wrap);
  expect(g.scrollLeft).toBeGreaterThan(0);
  expect(g.scrollTop).toBeGreaterThan(0);
  expect(
    inside({ x: g.image.right, y: g.image.bottom }, g.frame),
    "scrolling the frame to its end must put the bottom-right corner in view",
  ).toBe(true);
});

// Keyboard-only, at 150% so the travel fits in a handful of presses (Chromium
// animates key scrolling, so presses need to be paced or they coalesce).
test("the keyboard reaches the far corner — panning is not mouse-only", async ({ mount, page }) => {
  const { cmp, wrap } = await mountStory(mount, page);
  for (let i = 0; i < 2; i++) await cmp.getByRole("button", { name: "放大" }).click();
  await expect(cmp.getByText("150%")).toBeVisible();

  const zoomed = await geometry(wrap);
  expect(inside({ x: zoomed.image.right, y: zoomed.image.bottom }, zoomed.frame)).toBe(false);

  await wrap.focus();
  for (let i = 0; i < 16; i++) {
    await page.keyboard.press("ArrowRight");
    await page.waitForTimeout(80);
  }
  for (let i = 0; i < 4; i++) {
    await page.keyboard.press("PageDown");
    await page.waitForTimeout(120);
  }

  const g = await geometry(wrap);
  expect(g.scrollLeft).toBeGreaterThan(0);
  expect(
    inside({ x: g.image.right, y: g.image.bottom }, g.frame),
    "arrow keys on the focused frame must reach the bottom-right corner",
  ).toBe(true);
});

test("returning to 100% recentres the image with no leftover pan offset", async ({ mount, page }) => {
  const { cmp, wrap } = await mountStory(mount, page);
  const at100 = await geometry(wrap);
  await zoomTo400(cmp);
  for (let i = 0; i < 6; i++) await drag(page, wrap, -300, -200);
  expect((await geometry(wrap)).scrollLeft).toBeGreaterThan(0);

  for (let i = 0; i < 12; i++) await cmp.getByRole("button", { name: "縮小" }).click();
  await expect(cmp.getByText("100%")).toBeVisible();

  const back = await geometry(wrap);
  expect(back.scrollLeft).toBe(0);
  expect(back.scrollTop).toBe(0);
  expect(back.image.left).toBeCloseTo(at100.image.left, 0);
  expect(back.image.top).toBeCloseTo(at100.image.top, 0);
  expect(back.image.right).toBeCloseTo(at100.image.right, 0);
});

test("wheel-zoom over the image does not scroll the page behind the overlay", async ({ mount, page }) => {
  const { cmp, wrap } = await mountStory(mount, page);
  const box = (await wrap.boundingBox())!;
  await page.mouse.move(box.x + box.width / 2, box.y + box.height / 3);
  for (let i = 0; i < 4; i++) await page.mouse.wheel(0, -120);

  await expect(cmp.getByText("200%")).toBeVisible();
  expect(await page.evaluate(() => window.scrollY), "the page must stay put while the image zooms").toBe(0);
});
