// HOTSPOT — 「檔案與圖片」面板的寄件者篩選把檔案清單擠出畫面 (T-51 ②).
//
// Bug, measured on a real server with a 2,200-attachment / 114-uploader corpus
// before this change: `.chat__gallery-senders` was a wrapping chip row with no
// max-height and no scroll container, so it grew a line per uploader and stood
// 1,168px tall inside a 696px panel. `.chat__gallery-list` was crushed to 20px
// and the first file row landed at y=1473 in a 900px viewport — the panel that
// exists to show files showed none of them.
//
// WHY A REAL BROWSER: this is a pure layout fact. jsdom applies no CSS, so the
// unit suite can only pin the structural cause (how many controls the closed
// filter renders — `ChatGalleryPanel.test.tsx`, "stays one line high when
// closed"); the pixels are only knowable here.
//
// ⚠️ WHAT IS AND IS NOT REAL HERE. The component under test is the shipped
// `GallerySenderFilter`, mounted inside the panel's REAL ancestor chain
// (`.chat__gallery` — absolute, `top:64px; bottom:0`, a flex column) with the
// real stylesheet, because a bare mount gives the filter no bounded parent to
// be squeezed out of and would be green on the broken code. What is a stand-in
// is only the DATA PATH: the panel fetches through `api`, which resolves to the
// mock adapter in this harness and cannot be handed 60 uploaders, so the rows
// and the sibling list are fixtures with the panel's own class names. The
// geometry being asserted belongs to the CSS, not to the fetch.
//
// MUTANTS (each verified red — see the task's step note):
//   - give `.chat__gallery-senders` `flex-wrap: wrap` and render one button per
//     uploader beside the toggle → (1) goes red: the old bug exactly.
//   - drop `max-height` from `.chat__gallery-sender-options` → (3) goes red.
//   - make `.chat__gallery-sender-menu` `position: static` (the popover renders
//     in flow) → the list-unchanged pair at the end goes red.
import { test, expect } from "@playwright/experimental-ct-react";
import { GalleryFilterStory } from "./stories/GalleryFilterStory";

const SHOT_DIR = process.env.T51_SHOT_DIR ?? "test-results/t51-gallery";

test("1440px: the uploader filter stays one line and leaves the file list its height", async ({
  mount,
  page,
}) => {
  await page.setViewportSize({ width: 1440, height: 900 });
  await mount(<GalleryFilterStory uploaders={60} />);

  const panel = page.locator(".chat__gallery");
  const filter = page.locator(".chat__gallery-senders");
  const list = page.locator(".chat__gallery-list");
  await expect(list.locator(".chat__gallery-item").first()).toBeVisible();

  const panelBox = (await panel.boundingBox())!;
  const filterBox = (await filter.boundingBox())!;
  const listBox = (await list.boundingBox())!;

  // (1) CORE red→green. Closed, the filter is ONE control high, for any number
  // of uploaders — it measured 1,168px before this change.
  expect(
    filterBox.height,
    "the closed filter must stay one line for 60 uploaders",
  ).toBeLessThan(48);

  // (2) The list starts inside the panel and keeps real height — the symptom
  // the owner actually reported was that the files were not on screen at all.
  expect(listBox.y, "the list starts inside the panel").toBeLessThan(
    panelBox.y + panelBox.height,
  );
  expect(
    listBox.height,
    "the list keeps real height (it was crushed to 20px)",
  ).toBeGreaterThan(200);

  await page.screenshot({ path: `${SHOT_DIR}/filter-closed-1440.png` });

  // (3) Open: the popover is bounded and scrolls its own overflow, and the
  // panel underneath does not grow by a pixel.
  await page.getByRole("button", { name: "依上傳者篩選" }).click();
  const options = page.locator(".chat__gallery-sender-options");
  await expect(options).toBeVisible();
  expect(
    (await options.boundingBox())!.height,
    "the option list is bounded — the cap the chip row never had",
  ).toBeLessThanOrEqual(260);
  expect(
    await options.evaluate((el) => el.scrollHeight > el.clientHeight + 1),
    "and it scrolls its own overflow rather than growing",
  ).toBe(true);
  // ⚠️ NOT "the panel did not resize": the panel is `absolute; top:64px;
  // bottom:0`, so its height is pinned by its parent and that assertion is
  // unfalsifiable — no mutation can redden it (the independent review caught it
  // asserting nothing). What IS falsifiable is that the popover OVERLAYS rather
  // than displaces: if it ever rendered in flow, the file list below would be
  // pushed down and shortened, which is the old chip row's bug in a new shape.
  const listAfter = (await list.boundingBox())!;
  expect(listAfter.y, "the list does not move when the filter opens").toBe(
    listBox.y,
  );
  expect(
    listAfter.height,
    "…and it keeps its height: the popover floats over it, it does not push it",
  ).toBe(listBox.height);

  await page.screenshot({ path: `${SHOT_DIR}/filter-open-1440.png` });
});

test("390px: the same two facts hold on a phone-width panel", async ({
  mount,
  page,
}) => {
  await page.setViewportSize({ width: 390, height: 844 });
  await mount(<GalleryFilterStory uploaders={60} />);

  const filter = page.locator(".chat__gallery-senders");
  const list = page.locator(".chat__gallery-list");
  await expect(list.locator(".chat__gallery-item").first()).toBeVisible();
  expect((await filter.boundingBox())!.height).toBeLessThan(48);
  expect((await list.boundingBox())!.height).toBeGreaterThan(100);
  await page.screenshot({ path: `${SHOT_DIR}/filter-closed-390.png` });

  await page.getByRole("button", { name: "依上傳者篩選" }).click();
  const options = page.locator(".chat__gallery-sender-options");
  await expect(options).toBeVisible();
  // The popover is bounded by the panel's own width, not the viewport's: this
  // panel is a 340px rail and a vw-clamped popover would hang off it.
  const panelBox = (await page.locator(".chat__gallery").boundingBox())!;
  const optionsBox = (await options.boundingBox())!;
  expect(optionsBox.x).toBeGreaterThanOrEqual(panelBox.x - 1);
  expect(optionsBox.x + optionsBox.width).toBeLessThanOrEqual(
    panelBox.x + panelBox.width + 1,
  );
  await page.screenshot({ path: `${SHOT_DIR}/filter-open-390.png` });
});
