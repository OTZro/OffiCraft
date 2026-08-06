// GUARD (T-a706) — 請示頁卡片 header avatar is a real click + keyboard target.
//
// Owner (2026-07-21 screenshot): the 請示 page card avatar was the one place
// in the cockpit whose avatar did NOT open the member panel. jsdom (vitest)
// already proves the routing logic (which hash the click produces — see
// RepliesPage.test.tsx); what it CANNOT prove is whether a real browser
// actually delivers the click/keyboard event once real CSS/layout is in
// play, and whether Enter/Space native button activation actually fires (no
// JS keydown handler backs it — this is the browser's own default action for
// a focused <button>, which jsdom does not implement).
//
// MUTANT (verified red→green, see task conclusion doc): delete the `onClick`
// prop wiring in ReplyCardAvatarButton.tsx → every test below goes red
// (click and keyboard alike, since both ultimately fire the same handler).
// Deleting `aria-label` alone (title still set) leaves click/keyboard green
// and reddens ONLY the aria-label assertion below — checked via the raw
// attribute, NOT getByRole name matching: Chromium's accname algorithm falls
// back to `title` when aria-label is absent, so a role/name query alone
// would have stayed green on that mutant (caught while authoring this guard
// — a role-name assertion here would have been unfalsifiable against exactly
// the regression it claims to catch).
import { test, expect } from "@playwright/experimental-ct-react";
import { ReplyCardAvatarStory } from "./stories/ReplyCardAvatarStory";
import { zh } from "../src/i18n/locales/zh";

for (const width of [1280, 390]) {
  test(`width ${width}: clicking the avatar opens the profile`, async ({
    mount,
    page,
  }) => {
    await page.setViewportSize({ width, height: 800 });
    const cmp = await mount(<ReplyCardAvatarStory />);
    await expect(cmp.getByTestId("open-count")).toHaveText("0");
    await cmp.locator(".reply-card__avatar").click();
    await expect(cmp.getByTestId("open-count")).toHaveText("1");
  });

  // STOPPED (owner 2026-08-06, reply card rc-ebba69d22977, option 4 "把這兩條停掉").
  //
  // This assertion flaked twice within one hour on CI, both times on the very
  // first line: `Tab` then toBeFocused(). The proof it is the test and not the
  // product: on commit 1777030b the SAME suite ran in two jobs of the SAME
  // round — macos-host-gates went green while auto-beta's internal CI went red
  // on it. Identical code cannot be both.
  //
  // What is NO LONGER GUARDED, stated plainly because a skip is easy to read as
  // "there was never anything here": that this button is reachable by keyboard
  // at all, and that Enter/Space fire its native activation (jsdom does not
  // implement that default action, so the cheaper test layer cannot cover it).
  // The MOUSE path is still guarded by the sibling test above, which has never
  // flaked.
  //
  // The maintainer recommended narrowing the assertion instead (keep Enter/Space,
  // drop the "first Tab lands here" positional assumption); the owner ruled to
  // stop it. Re-enabling is a fresh decision, not a revert.
  // eslint-disable-next-line playwright/no-skipped-test
  test.skip(`width ${width}: the avatar is keyboard-reachable (Tab) and Enter/Space both activate it`, async ({
    mount,
    page,
  }) => {
    await page.setViewportSize({ width, height: 800 });
    const cmp = await mount(<ReplyCardAvatarStory />);
    const avatarBtn = cmp.locator(".reply-card__avatar");

    await page.keyboard.press("Tab");
    await expect(avatarBtn).toBeFocused();

    await page.keyboard.press("Enter");
    await expect(cmp.getByTestId("open-count")).toHaveText("1");

    await page.keyboard.press("Space");
    await expect(cmp.getByTestId("open-count")).toHaveText("2");
  });
}

test("the avatar carries aria-label (Avatar's inner glyphs are aria-hidden, so the button's OWN label is the only accessible name)", async ({
  mount,
}) => {
  const cmp = await mount(<ReplyCardAvatarStory />);
  const ariaLabel = await cmp
    .locator(".reply-card__avatar")
    .getAttribute("aria-label");
  expect(ariaLabel).toBe(zh.office.viewProfile);
  // Independent confirmation the real accessibility tree resolves the SAME
  // name (not required for the mutant to redden — see comment above — but a
  // second, real-Chromium-computed signal that the name is actually exposed).
  await expect(
    cmp.getByRole("button", { name: zh.office.viewProfile })
  ).toBeVisible();
});

test("focus-visible paints a non-transparent ring on the avatar button", async ({
  mount,
  page,
}) => {
  const cmp = await mount(<ReplyCardAvatarStory />);
  await page.keyboard.press("Tab");
  const shadow = await cmp
    .locator(".reply-card__avatar")
    .evaluate((el) => getComputedStyle(el).boxShadow);
  expect(shadow, "focused avatar paints a box-shadow ring").not.toBe("none");
});
