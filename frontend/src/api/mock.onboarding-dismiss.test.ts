// Mock adapter parity for `onboarding_dismissed` (T-0648). The server refuses a
// 知道了 that has no banner behind it with 409 (setOnboardingDismissed: no
// report at all, or a report that is not `failed`) — mock mode's standing state
// is no report, so this is the branch the mock can actually reach. It used to
// absorb the same call as a silent success while its own comment claimed
// server parity.

import { describe, it, expect, beforeEach } from "vitest";
import { mockApi, __resetMock } from "./mock";
import { codeForStatus } from "./errorCodes";
import { ApiError } from "./errors";

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
