// HOTSPOT — T-53 成本歸零. Two things a unit test structurally cannot see:
//
//   1. the 歸零 pill shares a row with 換手, but they live in SEPARATE grid
//      cells (.mp-cell heads). jsdom resolves no grid and reports both at the
//      same x, so a misaligned or wrongly-sized destructive button reads as
//      green there. Mutant: change .mp-cost-reset's height from 28px → the
//      same-row assertion reddens.
//   2. the confirm is position:fixed over the panel. Whether it actually COVERS
//      the figure it is about to destroy — rather than rendering somewhere off
//      the visible area — is a real-browser question.
import { test, expect } from "@playwright/experimental-ct-react";
import {
  CostResetButtonStory,
  CostResetNothingToClearStory,
  CostResetWorkerStory,
} from "./stories/CostResetStory";

test("the 歸零 pill lines up with 換手 in the sibling cell head", async ({
  mount,
  page,
}) => {
  await page.setViewportSize({ width: 1200, height: 900 });
  const cmp = await mount(<CostResetButtonStory />);

  const reset = cmp.getByTestId("mp-cost-reset");
  const refocus = cmp.getByTestId("mp-refocus");
  await expect(reset).toBeVisible();
  await expect(refocus).toBeVisible();

  const r = await reset.boundingBox();
  const f = await refocus.boundingBox();
  expect(r, "reset pill box").not.toBeNull();
  expect(f, "refocus pill box").not.toBeNull();
  // Same row: the two cell heads sit side by side, so their buttons share a
  // baseline. A stacked or differently-sized pill breaks the card's top edge.
  expect(Math.abs(r!.y - f!.y)).toBeLessThan(4);
  expect(Math.abs(r!.height - f!.height)).toBeLessThan(2);
  // And it is to the RIGHT — the cost cell follows the context cell.
  expect(r!.x).toBeGreaterThan(f!.x);

  await page.screenshot({ path: "cost-reset-1-button.png" });
});

test("the confirm covers the figure it is about to destroy, and names the amount", async ({
  mount,
  page,
}) => {
  await page.setViewportSize({ width: 1200, height: 900 });
  const cmp = await mount(<CostResetButtonStory />);

  const cell = cmp.getByTestId("mp-cost");
  await expect(cell).toHaveText("$37");
  const before = await cell.boundingBox();

  await cmp.getByTestId("mp-cost-reset").click();
  const dialog = cmp.getByTestId("mp-cost-reset-confirm");
  await expect(dialog).toBeVisible();
  // The owner is told what he is destroying, in the confirm itself.
  await expect(dialog).toContainText("$37");
  await expect(dialog).toContainText("清掉就回不來了");

  const box = await dialog.boundingBox();
  expect(box, "confirm box").not.toBeNull();
  // It really is over the panel, not parked below the fold.
  expect(box!.y).toBeLessThan(before!.y + before!.height);
  expect(box!.width).toBeGreaterThan(200);

  await page.screenshot({ path: "cost-reset-2-confirm.png" });
});

test("nothing measured: the cell reads the dash and the button is dead", async ({
  mount,
  page,
}) => {
  await page.setViewportSize({ width: 1200, height: 900 });
  const cmp = await mount(<CostResetNothingToClearStory />);

  // The pair that must never disagree — both come off the same null test.
  await expect(cmp.getByTestId("mp-cost")).toHaveText("—");
  await expect(cmp.getByTestId("mp-cost-reset")).toBeDisabled();

  await page.screenshot({ path: "cost-reset-3-nothing.png" });
});

test("the outsource panel gets the same button from the same component", async ({
  mount,
  page,
}) => {
  await page.setViewportSize({ width: 1200, height: 900 });
  const cmp = await mount(<CostResetWorkerStory />);

  await expect(cmp.getByTestId("worker-detail-cost-reset")).toBeVisible();
  await expect(cmp.getByTestId("worker-detail-cost")).toHaveText("$37");

  await page.screenshot({ path: "cost-reset-4-worker.png" });
});
