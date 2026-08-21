// 🔴 T-0648 — HOW AN ABSENT `dismissed_at` IS READ, pinned at the mapper.
//
// The onboarding banner's 「知道了」 is a durable, server-side dismissal: a stamp
// on the ONE onboarding report row. It shipped WITHOUT a migration and without
// a backfill (owner ruling rc-45eb8652b17f), so every report row written before
// the field existed arrives with no `dismissed_at` at all.
//
// Absent must read as NEVER DISMISSED. The other reading silently swallows the
// "your studio did not finish setting itself up" warning on every install that
// predates this change — and it would do so invisibly, because no fixture and
// no mock omits the field. That invisibility is why this needs a test.

import { describe, it, expect } from "vitest";
import { toOnboardingReport } from "./mappers";
import type { WireServerSettings } from "./wire";

type WireOnboarding = NonNullable<WireServerSettings["onboarding"]>;

const step = { name: "install_warden", ok: false, reason: "exit 1", detail: "log" };

describe("toOnboardingReport", () => {
  it("reads a report row that carries no dismissed_at as never dismissed", () => {
    const legacy = {
      state: "failed",
      started_at: 1,
      finished_at: 2,
      steps: [step],
    } as unknown as WireOnboarding;
    expect(toOnboardingReport(legacy)).toEqual({
      state: "failed",
      startedAt: 1,
      finishedAt: 2,
      dismissedAt: 0,
      steps: [
        { name: "install_warden", ok: false, code: "", reason: "exit 1", detail: "log" },
      ],
    });
  });

  it("carries a dismissal stamp through unchanged", () => {
    const dismissed: WireOnboarding = {
      state: "failed",
      started_at: 1,
      finished_at: 2,
      dismissed_at: 1750000000,
      steps: [{ ...step, code: "" }],
    };
    expect(toOnboardingReport(dismissed)).toEqual({
      state: "failed",
      startedAt: 1,
      finishedAt: 2,
      dismissedAt: 1750000000,
      steps: [
        { name: "install_warden", ok: false, code: "", reason: "exit 1", detail: "log" },
      ],
    });
  });

  // T-0648 — the same absence question one field over. A row written before
  // `code` existed has none, and "" is what makes the banner fall back to the
  // server's own English `reason` instead of showing nothing.
  it("carries a failure code through, and reads an absent one as no code", () => {
    const coded = {
      state: "failed",
      started_at: 1,
      finished_at: 2,
      steps: [{ ...step, code: "install_failed" }],
    } as unknown as WireOnboarding;
    // toMatchObject, not `.code).toBe(...)`: errorCodes.test.ts scans the tree
    // for that exact spelling to catch an API error code the server cannot
    // emit, and this is a DIFFERENT vocabulary (onboarding failures). Writing
    // it the other way makes that guard report a false offender.
    expect(toOnboardingReport(coded).steps[0]).toMatchObject({
      code: "install_failed",
    });
    expect(
      toOnboardingReport({
        state: "failed",
        started_at: 1,
        finished_at: 2,
        steps: [step],
      } as unknown as WireOnboarding).steps[0]
    ).toMatchObject({ code: "" });
  });
});
