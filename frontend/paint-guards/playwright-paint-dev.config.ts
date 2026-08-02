// Playwright config for the DEV-MODE paint measurement (devModeFlash.devpaint.spec.ts).
//
// Separate from playwright-paint.config.ts on purpose:
//   * that one builds dist/ and serves it from settingsStub.mjs — the PRODUCTION
//     artifact;
//   * this one runs the actual `vite` dev server, which is a different
//     first-paint waterfall (unbundled ES modules, on-demand transforms).
// The globs are disjoint (`*.paint.spec.ts` vs `*.devpaint.spec.ts`) so neither
// runner can silently sweep in the other's tests.
//
// VITE_USE_MOCK=false is not optional: the shipped default mock adapter answers
// getServerSettings() from memory in ~0 ms with custom_themes: [], which both
// removes the network wait under measurement AND makes reconcile delete the
// cached record. Same flag bin/build ships with.
import { defineConfig, devices } from "@playwright/test";
import { fileURLToPath } from "node:url";

const PORT = Number(process.env.PAINT_DEV_PORT ?? "4320");

export default defineConfig({
  testDir: ".",
  testMatch: "**/*.devpaint.spec.ts",
  // Frame timing IS the measurement; two pages sampling rAF on one machine
  // perturb each other.
  fullyParallel: false,
  workers: 1,
  forbidOnly: !!process.env.CI,
  // A flake here is real signal about first-paint timing. Retrying hides it.
  retries: 0,
  reporter: [["list"]],
  timeout: 90_000,
  use: {
    trace: "off",
    ...devices["Desktop Chrome"],
    baseURL: `http://localhost:${PORT}`,
  },
  projects: [{ name: "paint-guards-dev" }],
  webServer: {
    command: `npm run dev -- --port ${PORT} --strictPort`,
    url: `http://localhost:${PORT}/`,
    cwd: fileURLToPath(new URL("..", import.meta.url)),
    env: { VITE_USE_MOCK: "false" },
    reuseExistingServer: false,
    stdout: "pipe",
    stderr: "pipe",
    timeout: 120_000,
  },
});
