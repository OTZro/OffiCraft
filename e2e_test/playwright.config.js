// e2e_test/playwright.config.js — config for the isolated officraft e2e suite.
// The service is brought up by setup.sh (NOT by playwright's webServer), so specs
// just point at OC_E2E_BASE. Browser-based render specs (B group) are added later
// and will require `npx playwright install chromium`.
const { defineConfig } = require('@playwright/test');

// The whole suite talks to ONE server process and ONE SQLite file. fullyParallel
// was already false, but that only serialises tests WITHIN a file — playwright
// still runs FILES concurrently, one worker per core, and those workers then
// fight over the same station. Measured 2026-08-05 on this suite: 7 workers → 7
// red, the same tree serialised → 4 red. Three of those reds were the harness
// eating itself, and a gate that reddens for reasons unrelated to the code under
// test gets switched off within a week. One worker, always.
const WORKERS = 1;

// The real-fleet spec (05, "machine onboarding") is the one spec that is not a
// browser test: it needs `claude` on PATH, spawns a real warden and burns real
// API quota. That makes it unrunnable on a hosted runner, so an automated caller
// sets OC_E2E_EXCLUDE_REAL_FLEET=1 and gets the browser specs only. Excluded by
// FILE rather than by a title regex on purpose: a regex silently widens the day
// someone reuses those words, and what is being excluded here is a file-level
// prerequisite, not a phrase. e2e_test/assert-specs-ran.sh asserts AFTERWARDS
// that it really stayed out — an exclusion nobody checks is an exclusion that
// quietly stops excluding.
const EXCLUDE_REAL_FLEET = process.env.OC_E2E_EXCLUDE_REAL_FLEET === '1';

module.exports = defineConfig({
  testDir: './tests',
  ...(EXCLUDE_REAL_FLEET ? { testIgnore: ['**/05_machine_onboarding_spawn.spec.js'] } : {}),
  fullyParallel: false,
  workers: WORKERS,
  forbidOnly: !!process.env.CI,
  retries: 0,
  reporter: [['list'], ['html', { open: 'never' }]],
  use: {
    baseURL: process.env.OC_E2E_BASE || 'http://127.0.0.1:8791',
    extraHTTPHeaders: {},
  },
});
