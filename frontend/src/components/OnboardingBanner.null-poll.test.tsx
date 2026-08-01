// T-8115 — 「永久是空的」不再被當成「還沒寫好、再等等」.
//
// MEASURED PREMISE (production install, GET /api/settings): 639,270 bytes
// uncompressed, 373 kB gzipped, and `onboarding` is null — which the DTO itself
// declares NORMAL ("Null on the settings read when onboarding never ran (an
// install that predates it, or a database that already had a password)"). The
// banner treated null as non-terminal, so every cockpit open polled that
// payload once every 3 s for the full 180 s ceiling: ~61 downloads, ~22 MB, for
// a row that no code path on that install will ever write.
//
// The fix must not buy that back by making the banner fetch once: the ONLY
// timeline it exists for is the fresh install where the verdict lands ~30 s
// after the password is set. Both halves are pinned here, and the counts are
// the assertions — "was called" would pass either way.

import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { render, screen, act } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import {
  OnboardingBanner,
  markOnboardingFirstRun,
  ONBOARDING_POLL_MS,
  ONBOARDING_POLL_CEILING_MS,
} from "./OnboardingBanner";
import { resetAllSharedSnapshots } from "../lib/sharedSnapshot";

const getServerSettings = vi.fn();

vi.mock("../api", () => ({
  api: { getServerSettings: () => getServerSettings() },
}));

function settingsWith(onboarding: unknown) {
  return { outsourceMaxParallel: 0, onboarding };
}

const runningReport = {
  state: "running",
  startedAt: 1,
  finishedAt: 0,
  steps: [],
};

const failedReport = {
  state: "failed",
  startedAt: 1,
  finishedAt: 2,
  steps: [
    {
      name: "install_warden",
      ok: false,
      reason: "installing this machine's warden failed (exit 1)",
      detail: "",
    },
  ],
};

function renderBanner() {
  return render(
    <I18nProvider>
      <OnboardingBanner />
    </I18nProvider>
  );
}

/** Drive the poll past its own ceiling. Each step flushes the microtask queue
 * so a settled fetch can schedule the next timer before the clock moves on. */
async function runPastCeiling() {
  const steps = Math.ceil(ONBOARDING_POLL_CEILING_MS / ONBOARDING_POLL_MS) + 5;
  for (let i = 0; i < steps; i++) {
    await act(async () => {
      await vi.advanceTimersByTimeAsync(ONBOARDING_POLL_MS);
    });
  }
}

describe("OnboardingBanner — the two kinds of null", () => {
  beforeEach(() => {
    sessionStorage.clear();
    getServerSettings.mockReset();
    vi.useFakeTimers();
  });
  afterEach(() => {
    vi.useRealTimers();
  });

  // ── half 1: an ordinary cockpit open on an install where onboarding never
  // ran. Exactly ONE read, for the whole three minutes.
  it("stops after ONE read when onboarding never ran (no first-run session)", async () => {
    getServerSettings.mockResolvedValue(settingsWith(null));

    renderBanner();
    await act(async () => {});
    expect(getServerSettings).toHaveBeenCalledTimes(1);

    await runPastCeiling();

    // Before the fix this was ~61. The number is the point: a "was called"
    // assertion is satisfied by the broken behaviour too.
    expect(getServerSettings).toHaveBeenCalledTimes(1);
    expect(screen.queryByTestId("onboarding-banner")).toBeNull();
  });

  // ── half 2: THE case the banner exists for. This browser session set the
  // initial password, so onboarding really was kicked; the report is not
  // written the instant we ask, and the verdict lands tens of seconds later.
  // The poll must survive all of that.
  it("keeps polling through a null in the session that set the password, and shows the ~30s verdict", async () => {
    markOnboardingFirstRun();
    getServerSettings
      // t=0: kicked, row not visible to us yet — the transient null.
      .mockResolvedValueOnce(settingsWith(null))
      // t=3s..~30s: the run is under way.
      .mockResolvedValueOnce(settingsWith(runningReport))
      .mockResolvedValueOnce(settingsWith(runningReport))
      .mockResolvedValueOnce(settingsWith(runningReport))
      // and then it fails, with nobody reloading the page.
      .mockResolvedValue(settingsWith(failedReport));

    renderBanner();
    await act(async () => {});
    // The transient null did NOT stop the poll…
    expect(getServerSettings).toHaveBeenCalledTimes(1);
    expect(screen.queryByTestId("onboarding-banner")).toBeNull();

    for (let i = 0; i < 4; i++) {
      await act(async () => {
        await vi.advanceTimersByTimeAsync(ONBOARDING_POLL_MS);
      });
    }

    // …and the verdict reached the owner without a reload.
    expect(getServerSettings.mock.calls.length).toBeGreaterThanOrEqual(5);
    const banner = screen.getByTestId("onboarding-banner");
    expect(banner.textContent).toContain(
      "installing this machine's warden failed (exit 1)"
    );
  });

  // The flag is about ONE run, not about the browser forever: once a report
  // row exists the question "was it kicked?" is settled, so a later cockpit
  // open in the same tab is back to one read.
  it("clears the first-run flag once a report exists, so a later mount reads once", async () => {
    markOnboardingFirstRun();
    getServerSettings.mockResolvedValue(settingsWith(failedReport));

    const first = renderBanner();
    await act(async () => {});
    expect(getServerSettings).toHaveBeenCalledTimes(1);
    first.unmount();

    // Simulate a fresh page load: a new document starts with an empty module
    // cache, so the shared /api/settings snapshot is gone too.
    resetAllSharedSnapshots();
    // Now the install looks like the production one: null report, no flag.
    getServerSettings.mockReset();
    getServerSettings.mockResolvedValue(settingsWith(null));
    renderBanner();
    await act(async () => {});
    await runPastCeiling();
    // Not 61: the previous run's flag did not survive its own report.
    expect(getServerSettings).toHaveBeenCalledTimes(1);
  });
});
