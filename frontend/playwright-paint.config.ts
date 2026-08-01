// T-1500 — Playwright config for the PAINT GUARDS (pre-React theme paint).
//
// Separate from playwright-ct.config.ts because these are not component tests:
// they load the REAL built artifact (dist/) over HTTP and sample real animation
// frames. The CT runner mounts components against a dev server and never
// produces a dist/index.html, so it cannot host them.
//
// It is NOT a new CI gate. `npm run test:ct` runs the CT config and then this
// one, so both live inside bin/ci.sh's existing step 4c. That is deliberate: the
// three guards this ticket adds (validator / artifact shape / zero flash) each
// sit in a DIFFERENT existing host —
//   * validator + artifact shape → vitest (step 4b), no browser needed;
//   * zero flash + injection     → here, inside step 4c.
// so no single gate being dropped for cost can take all three with it.
//
// Two servers, because the two scenarios differ only in what the SERVER knows:
//   :4318 mode=ok            — the server recognises the owner's theme (happy path)
//   :4319 mode=unknown-theme — it does not, so the stale picture must be dropped
// The 400 ms settings delay is not padding: the flash this ticket fixes IS the
// wait for /api/settings, and a zero-latency answer would remove the very window
// under test.
import { defineConfig, devices } from "@playwright/test";

const DIST = process.env.PAINT_GUARD_DIST ?? "dist";

export default defineConfig({
  testDir: "./paint-guards",
  testMatch: "**/*.paint.spec.ts",
  // Frame timing is the measurement. Two pages sampling rAF on one machine
  // perturb each other, so these run one at a time.
  fullyParallel: false,
  workers: 1,
  forbidOnly: !!process.env.CI,
  // A flake here is a real signal about first-paint timing; retrying would hide
  // exactly what the guard exists to see.
  retries: 0,
  reporter: [["list"]],
  timeout: 60_000,
  use: { trace: "off", ...devices["Desktop Chrome"] },
  projects: [{ name: "paint-guards" }],
  webServer: [
    {
      command: `node paint-guards/settingsStub.mjs --port 4318 --dist ${DIST} --mode ok --delay 400`,
      url: "http://localhost:4318/api/settings",
      reuseExistingServer: false,
      stdout: "pipe",
    },
    {
      command: `node paint-guards/settingsStub.mjs --port 4319 --dist ${DIST} --mode unknown-theme --delay 400`,
      url: "http://localhost:4319/api/settings",
      reuseExistingServer: false,
      stdout: "pipe",
    },
  ],
});
