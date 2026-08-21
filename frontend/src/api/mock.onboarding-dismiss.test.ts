// Mock adapter parity for `onboarding_dismissed` (T-0648). The server refuses a
// 「不再顯示」 that has no banner behind it with 409 (setOnboardingDismissed: no
// report at all, or a report that is not `failed`) — mock mode's standing state
// is no report, so this is the branch the mock can actually reach. It used to
// absorb the same call as a silent success while its own comment claimed
// server parity.

import { describe, it, expect, beforeEach } from "vitest";
import { mockApi, __resetMock, __injectMockOnboardingReport } from "./mock";
import type { WireServerSettings } from "./wire";
import { codeForStatus } from "./errorCodes";
import { ApiError } from "./errors";

type WireOnboarding = NonNullable<WireServerSettings["onboarding"]>;

function reportIn(state: string): WireOnboarding {
  return {
    state,
    started_at: 1,
    finished_at: state === "running" ? 0 : 2,
    dismissed_at: 0,
    steps: [
      {
        name: "install_warden",
        ok: false,
        code: "install_failed",
        reason: "installing this machine's warden failed (exit 1)",
        detail: "",
      },
    ],
  };
}

describe("mock settings — onboarding dismissal", () => {
  beforeEach(() => __resetMock());

  it("refuses a dismissal with 409 when no onboarding report is present", async () => {
    const err = await mockApi
      .patchServerSettings({ onboardingDismissed: true })
      .then(
        () => null,
        (e: unknown) => e
      );
    expect(err).toBeInstanceOf(ApiError);
    expect((err as ApiError).status).toBe(409);
    // The code is DERIVED from the status, never hand-written by the mock.
    expect((err as ApiError).code).toBe(codeForStatus(409));
  });

  it("refuses an UN-dismissal the same way — the guard is about the banner, not the direction", async () => {
    await expect(
      mockApi.patchServerSettings({ onboardingDismissed: false })
    ).rejects.toMatchObject({ status: 409 });
  });

  // 🔴 THE OTHER HALF OF THE GUARD. The three cases above all reach it through
  // "no report at all", which is mock mode's standing state — so on its own the
  // guard could be written as `!onboarding` and stay green while quietly letting
  // a stamp land on a run that is STILL RUNNING. That is the branch the server
  // comment calls the whole point, and these two cases are what stand on it:
  // `running` refused, `failed` let through.
  it("refuses a dismissal while the report is still running", async () => {
    __injectMockOnboardingReport(reportIn("running"));
    await expect(
      mockApi.patchServerSettings({ onboardingDismissed: true })
    ).rejects.toMatchObject({ status: 409 });
    const s = await mockApi.getServerSettings();
    expect(s.onboarding?.state).toBe("running");
    expect(s.onboarding?.dismissedAt).toBe(0);
  });

  it("stamps and un-stamps the report once it has failed", async () => {
    __injectMockOnboardingReport(reportIn("failed"));
    const stamped = await mockApi.patchServerSettings({ onboardingDismissed: true });
    expect(stamped.onboarding?.dismissedAt).toBeGreaterThan(0);
    expect((await mockApi.getServerSettings()).onboarding?.dismissedAt).toBeGreaterThan(0);

    const cleared = await mockApi.patchServerSettings({ onboardingDismissed: false });
    expect(cleared.onboarding?.dismissedAt).toBe(0);
    expect((await mockApi.getServerSettings()).onboarding?.dismissedAt).toBe(0);
  });

  it("applies the rest of the patch before refusing, as the server does", async () => {
    await expect(
      mockApi.patchServerSettings({ displayWide: true, onboardingDismissed: true })
    ).rejects.toMatchObject({ status: 409 });
    // onboarding_dismissed is handled LAST on both sides, so the fields ahead of
    // it are already committed when the refusal is thrown. Pinned so the comment
    // saying so cannot quietly stop being true.
    const s = await mockApi.getServerSettings();
    expect(s.displayWide).toBe(true);
  });
});
