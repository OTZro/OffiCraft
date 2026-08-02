// zeroFlash.paint.spec.ts — T-1500 gate 4c (hosted inside the existing
// `npm run test:ct` step; see package.json).
//
// The headline contract: on a reload by a logged-in owner whose server-stored
// theme the server still recognises, NO frame is anything but the cached colour.
//
// THE PRECONDITION IS ASSERTED, NOT ASSUMED. Two earlier versions of this guard
// were measured in a session with no auth token, where reconcileFromServer() is
// never called (it is gated on hasToken()), so `themesLoaded` stayed false
// forever and the only branch ever exercised was "keep the cache". That reads
// BAD_FRAMES=0 on a build whose reconcile handoff is completely untested — and
// the very same build reads BAD_FRAMES=231 the moment a token is present. So
// every test here proves, in-band:
//   1. GET /api/settings really answered 200, and
//   2. its body really carried this theme, and
//   3. the app really adopted the server's copy — the seeded record is written
//      with a DIFFERENT `name`, and the record on disk afterwards must carry the
//      SERVER's name. That is only true if reconcile ran to completion.
// If any of those is false the test fails as a setup error rather than passing
// vacuously.

import { expect, test } from "@playwright/test";
import {
  CACHED_BG_RGB,
  EXPECT_APPLIED,
  PAINT_THEME_ID,
  VALID_RICH_BUNDLE,
  paintRecordJSON,
} from "../src/lib/paintFixtures";
import {
  MIN_SAMPLES,
  applyNetProfile,
  badFrames,
  captureSettingsResponses,
  collect,
  collectPageErrors,
  frameCarrying,
  installFrameSampler,
  readStoredPaint,
  seedSession,
  summarize,
  type NetProfile,
} from "./frameProbe";

const TOKEN = "paint-guard-owner-token";
/** The seeded cache is byte-identical to the server's copy EXCEPT the name, so
 * adopting the server's copy is visually a no-op but observably distinct. */
const STALE_NAME = "STALE-CACHE-NAME";
const STALE_RECORD = paintRecordJSON({ ...VALID_RICH_BUNDLE, name: STALE_NAME });

const OK_SERVER = process.env.PAINT_GUARD_OK_URL ?? "http://localhost:4318";
const UNKNOWN_SERVER = process.env.PAINT_GUARD_UNKNOWN_URL ?? "http://localhost:4319";

for (const profile of ["fourg", "loopback"] as NetProfile[]) {
  test(`no frame is anything but the cached colour — authenticated, server knows the theme (${profile})`, async ({
    page,
  }) => {
    const pageErrors = collectPageErrors(page);
    const settingsBodies = captureSettingsResponses(page);

    await page.goto(OK_SERVER);
    await seedSession(page, {
      token: TOKEN,
      themeId: PAINT_THEME_ID,
      paintRecord: STALE_RECORD,
    });

    await installFrameSampler(page);
    await applyNetProfile(page, profile);
    await page.reload({ waitUntil: "load" });
    await page.waitForTimeout(3000);

    const { samples, prototypePolluted } = await collect(page);
    const storedPaint = await readStoredPaint(page);

    // ---- preconditions: the scenario really is the one we meant to measure ----
    expect(settingsBodies.length, "GET /api/settings never answered 200").toBeGreaterThan(0);
    const settings = (await settingsBodies[0]) as {
      custom_themes?: { id: string }[];
      display_theme?: string;
    } | null;
    expect(settings?.display_theme, "server did not report this theme as active").toBe(
      PAINT_THEME_ID
    );
    expect(
      (settings?.custom_themes ?? []).map((b) => b.id),
      "server did not report the theme as existing"
    ).toContain(PAINT_THEME_ID);

    expect(storedPaint, "the paint record was removed — reconcile did not confirm it").not.toBeNull();
    const stored = JSON.parse(storedPaint as string) as { bundle: { name: string } };
    expect(
      stored.bundle.name,
      "the record still carries the SEEDED name, so reconcile never adopted the server copy — " +
        "this run proves nothing about the authenticated path"
    ).not.toBe(STALE_NAME);
    expect(stored.bundle.name).toBe(VALID_RICH_BUNDLE.name);

    // ---- the probe itself must have run ----
    expect(
      samples.length,
      `only ${samples.length} frames sampled — the per-frame sampler did not run`
    ).toBeGreaterThanOrEqual(MIN_SAMPLES);
    expect(
      samples.some((f) => f.mounted),
      "React never mounted — the page did not actually render"
    ).toBe(true);
    expect(pageErrors, "uncaught page errors").toEqual([]);
    expect(prototypePolluted).toBe(false);

    // ---- the contract ----
    const bad = badFrames(samples, CACHED_BG_RGB);
    expect(
      bad.length,
      `${bad.length}/${samples.length} frames were not the cached colour; first at ` +
        `${bad[0]?.t}ms bg=${bad[0]?.bg}\n${summarize(samples)}`
    ).toBe(0);

    // ---- POSITIVE: the rich record's values actually landed ----
    // Without this, an applier that silently stops applying fonts or the canvas
    // background passes every "forbidden string is absent" check.
    for (const needle of EXPECT_APPLIED) {
      expect(
        frameCarrying(samples, needle),
        `no frame ever carried ${needle}\n${summarize(samples)}`
      ).toBeTruthy();
    }
  });
}

test("server no longer knows the theme → the stale picture is dropped (documented behaviour)", async ({
  page,
}) => {
  // NOT a failure mode, and deliberately pinned: the server owns the theme set,
  // so a cached picture it no longer recognises MUST go. This is the same code
  // path a returning owner hits after deleting the theme on another device. It is
  // asserted separately precisely so nobody reads the zero-flash test above as
  // covering it, and so nobody "fixes" this into keeping a deleted theme.
  const pageErrors = collectPageErrors(page);
  const settingsBodies = captureSettingsResponses(page);

  await page.goto(UNKNOWN_SERVER);
  await seedSession(page, {
    token: TOKEN,
    themeId: PAINT_THEME_ID,
    paintRecord: STALE_RECORD,
  });

  await installFrameSampler(page);
  await page.reload({ waitUntil: "load" });
  await page.waitForTimeout(3000);

  const { samples } = await collect(page);
  const storedPaint = await readStoredPaint(page);

  expect(settingsBodies.length, "GET /api/settings never answered 200").toBeGreaterThan(0);
  const settings = (await settingsBodies[0]) as { custom_themes?: unknown[] } | null;
  expect(settings?.custom_themes, "this server was supposed to know no themes").toEqual([]);

  expect(samples.length).toBeGreaterThanOrEqual(MIN_SAMPLES);
  expect(pageErrors).toEqual([]);

  // The record is gone…
  expect(storedPaint, "a theme the server no longer has must not stay cached").toBeNull();
  // …and the first frames DID show the cached colour (the pre-paint ran), which is
  // what makes this "cache, then corrected" rather than "cache never applied".
  expect(
    frameCarrying(samples, `--color-bg: ${VALID_RICH_BUNDLE.colors["--color-bg"]}`),
    "the pre-paint script never applied the cached colour at all"
  ).toBeTruthy();
  // …and the page settles on something other than the cached colour.
  expect(samples[samples.length - 1].bg).not.toBe(CACHED_BG_RGB);
});
