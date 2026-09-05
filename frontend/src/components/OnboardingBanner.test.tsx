// T-ba62 — the first-run onboarding banner is the ONLY place a fresh install
// can read WHY it is not working. These tests pin both directions:
//   - onboarding failed → the banner names the step AND its reason;
//   - onboarding ok / never ran / still running → nothing renders at all.
// The negative cases are not decoration: a banner that renders unconditionally
// would satisfy the failure test on its own, and a "why is my studio broken"
// nag on a perfectly healthy install is its own regression.

import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { render, screen, waitFor, act } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import {
  OnboardingBanner,
  ONBOARDING_POLL_MS,
  ONBOARDING_POLL_CEILING_MS,
} from "./OnboardingBanner";

const getServerSettings = vi.fn();
const patchServerSettings = vi.fn();

vi.mock("../api", () => ({
  api: {
    getServerSettings: () => getServerSettings(),
    patchServerSettings: (patch: unknown) => patchServerSettings(patch),
  },
}));

function settingsWith(onboarding: unknown) {
  return { outsourceMaxParallel: 0, onboarding };
}

function renderBanner() {
  return render(
    <I18nProvider>
      <OnboardingBanner />
    </I18nProvider>
  );
}

describe("OnboardingBanner", () => {
  beforeEach(() => {
    sessionStorage.clear();
    getServerSettings.mockReset();
    patchServerSettings.mockReset();
    patchServerSettings.mockImplementation(async () => settingsWith(null));
  });

  it("shows the failed step and its REASON", async () => {
    getServerSettings.mockResolvedValue(
      settingsWith({
        state: "failed",
        startedAt: 1,
        finishedAt: 2,
        steps: [
          {
            name: "install_warden",
            ok: false,
            reason: "installing this machine's warden failed (exit 1)",
            detail: "[ocwarden install] FATAL: claude_bin_unresolved",
          },
        ],
      })
    );
    renderBanner();
    const banner = await screen.findByTestId("onboarding-banner");
    // The step is named…
    expect(banner.textContent).toContain("安裝這台機器");
    // …and, crucially, WHY. A step name with no cause is the same silence.
    expect(banner.textContent).toContain(
      "installing this machine's warden failed (exit 1)"
    );
    // owner 2026-07-31 (rc-b7d1c642f2d2): ONE verb. This intro said 叫醒助理
    // while the step right beside it said 喚醒助理 — two words for one act, on
    // one banner. The phrase carries its neighbouring punctuation so the step
    // label cannot satisfy this assertion by itself.
    expect(banner.textContent).toContain("、喚醒助理。");
  });

  // T-0648: the banner was Chinese everywhere EXCEPT the one sentence that says
  // what actually broke — that arrived as the server's English engineer-facing
  // `reason`. The closed `code` vocabulary is what the cockpit translates (the
  // same shape backupHealth already uses); the raw `reason` stays only as the
  // fallback for a code this build does not know.
  it("renders the localized sentence for a coded reason, not the server English", async () => {
    getServerSettings.mockResolvedValue(
      settingsWith({
        state: "failed",
        startedAt: 1,
        finishedAt: 2,
        steps: [
          {
            name: "install_warden",
            ok: false,
            code: "install_failed",
            reason: "installing this machine's warden failed (exit 1)",
            detail: "[ocwarden install] FATAL: launchctl kickstart failed",
          },
        ],
      })
    );
    renderBanner();
    const banner = await screen.findByTestId("onboarding-banner");
    expect(banner.textContent).toContain("這台機器沒有安裝成功");
    // The English engineer sentence must be GONE from the banner body — a
    // translation that merely sits beside the original has not translated it.
    expect(banner.textContent).not.toContain(
      "installing this machine's warden failed"
    );
    // …and the engineer payload it carried (the exit code) must not have been
    // literal-translated into the owner's sentence either.
    expect(banner.textContent).not.toContain("exit 1");
  });

  it("falls back to the server reason for a code this build does not know", async () => {
    getServerSettings.mockResolvedValue(
      settingsWith({
        state: "failed",
        startedAt: 1,
        finishedAt: 2,
        steps: [
          {
            name: "install_warden",
            ok: false,
            code: "a_code_from_a_newer_server",
            reason: "something this cockpit has no wording for",
            detail: "",
          },
        ],
      })
    );
    renderBanner();
    const banner = await screen.findByTestId("onboarding-banner");
    expect(banner.textContent).toContain(
      "something this cockpit has no wording for"
    );
  });

  // A code that collides with a name on Object.prototype must take the SAME
  // fallback. Looked up unguarded, `reasons["toString"]` answers an inherited
  // FUNCTION, `??` sees a non-nullish hit and keeps it, and React renders a
  // function child as nothing — the banner would go blank exactly where it is
  // supposed to say what broke.
  //
  // The two inherited names are NOT the same shape, and one case cannot stand
  // for the other: `toString` is a data property holding a function, while
  // `__proto__` is an ACCESSOR whose getter returns an OBJECT. A guard written
  // as "reject functions" passes `__proto__` straight through to React, which
  // throws on an object child. Both names ride in the same report so the guard
  // has to answer for the whole chain, not for the function half of it.
  it("falls back to the server reason for a code that names an Object.prototype member", async () => {
    getServerSettings.mockResolvedValue(
      settingsWith({
        state: "failed",
        startedAt: 1,
        finishedAt: 2,
        steps: [
          {
            name: "install_warden",
            ok: false,
            code: "toString",
            reason: "a reason only the server knows how to word",
            detail: "",
          },
          {
            name: "wake_assistant",
            ok: false,
            code: "__proto__",
            reason: "a second reason only the server knows how to word",
            detail: "",
          },
        ],
      })
    );
    renderBanner();
    const banner = await screen.findByTestId("onboarding-banner");
    expect(banner.textContent).toContain(
      "a reason only the server knows how to word"
    );
    expect(banner.textContent).toContain(
      "a second reason only the server knows how to word"
    );
  });

  it("hides the raw tool log behind a toggle, then reveals it", async () => {
    getServerSettings.mockResolvedValue(
      settingsWith({
        state: "failed",
        startedAt: 1,
        finishedAt: 2,
        steps: [
          {
            name: "install_warden",
            ok: false,
            reason: "installing this machine's warden failed (exit 1)",
            detail: "[ocwarden install] FATAL: claude_bin_unresolved",
          },
        ],
      })
    );
    renderBanner();
    const toggle = await screen.findByTestId("onboarding-detail-toggle");
    expect(screen.queryByTestId("onboarding-detail")).toBeNull();
    toggle.click();
    await waitFor(() => {
      expect(screen.getByTestId("onboarding-detail").textContent).toContain(
        "claude_bin_unresolved"
      );
    });
  });

  it("renders NOTHING when onboarding succeeded", async () => {
    getServerSettings.mockResolvedValue(
      settingsWith({
        state: "ok",
        startedAt: 1,
        finishedAt: 2,
        steps: [{ name: "install_warden", ok: true, reason: "installed", detail: "" }],
      })
    );
    const { container } = renderBanner();
    await waitFor(() => expect(getServerSettings).toHaveBeenCalled());
    expect(screen.queryByTestId("onboarding-banner")).toBeNull();
    expect(container.textContent).toBe("");
  });

  it("renders NOTHING while onboarding is still running", async () => {
    getServerSettings.mockResolvedValue(
      settingsWith({ state: "running", startedAt: 1, finishedAt: 0, steps: [] })
    );
    renderBanner();
    await waitFor(() => expect(getServerSettings).toHaveBeenCalled());
    expect(screen.queryByTestId("onboarding-banner")).toBeNull();
  });

  // The STATE gate, isolated. The case above cannot pin it: an unfinished run
  // has no steps yet, so "no failed steps" alone would already suppress the
  // banner and a mutant that dropped the state check entirely would stay green
  // (it did — this test exists because that mutant survived). Here the report
  // is mid-run AND already carries a failed step: the banner must still hold
  // its tongue, because a run in progress can still recover, and a warning
  // that appears and then vanishes on its own teaches the owner to ignore it.
  it("renders NOTHING mid-run even when a step has already failed", async () => {
    getServerSettings.mockResolvedValue(
      settingsWith({
        state: "running",
        startedAt: 1,
        finishedAt: 0,
        steps: [
          { name: "install_warden", ok: false, reason: "still retrying", detail: "" },
        ],
      })
    );
    renderBanner();
    await waitFor(() => expect(getServerSettings).toHaveBeenCalled());
    expect(screen.queryByTestId("onboarding-banner")).toBeNull();
  });

  it("renders NOTHING when onboarding never ran (null report)", async () => {
    getServerSettings.mockResolvedValue(settingsWith(null));
    renderBanner();
    await waitFor(() => expect(getServerSettings).toHaveBeenCalled());
    expect(screen.queryByTestId("onboarding-banner")).toBeNull();
  });

  it("renders NOTHING when the settings read itself fails (asserts no fiction)", async () => {
    getServerSettings.mockRejectedValue(new Error("network down"));
    renderBanner();
    await waitFor(() => expect(getServerSettings).toHaveBeenCalled());
    expect(screen.queryByTestId("onboarding-banner")).toBeNull();
  });

  // ── 🔴 T-0648: 「不再顯示」 IS PERMANENT, AND THE SERVER IS WHERE IT LIVES ────
  //
  // Owner ruling rc-45eb8652b17f (「永久關閉，不需另外開任務」), reported after
  // hitting the old behaviour himself: 「為什麼我重新點進網址又出現了？」 The
  // dismissal used to be a sessionStorage key, which is scoped to ONE TAB — a
  // second tab, or the same URL opened again, brought the banner straight back.
  // These three cases pin the whole contract: the press writes to the server,
  // a session that remembers nothing locally still stays quiet, and a report
  // row that carries no stamp at all still speaks.
  const dismissibleReport = {
    state: "failed",
    startedAt: 1,
    finishedAt: 2,
    dismissedAt: 0,
    steps: [{ name: "wake_assistant", ok: false, reason: "no warden yet", detail: "" }],
  };

  it("writes the dismissal to the SERVER when 「不再顯示」 is pressed", async () => {
    getServerSettings.mockResolvedValue(settingsWith(dismissibleReport));
    renderBanner();
    (await screen.findByTestId("onboarding-dismiss")).click();
    await waitFor(() =>
      expect(patchServerSettings).toHaveBeenCalledWith({ onboardingDismissed: true })
    );
    await waitFor(() => expect(screen.queryByTestId("onboarding-banner")).toBeNull());
  });

  it("stays quiet in a brand-new frontend session that remembers nothing locally", async () => {
    // A NEW session: no sessionStorage, no cached snapshot, nothing but what
    // the server says. Under the old per-tab dismissal this rendered the banner
    // all over again — the whole bug.
    sessionStorage.clear();
    getServerSettings.mockResolvedValue(
      settingsWith({ ...dismissibleReport, dismissedAt: 1750000000 })
    );
    renderBanner();
    await waitFor(() => expect(getServerSettings).toHaveBeenCalled());
    expect(screen.queryByTestId("onboarding-banner")).toBeNull();
  });

  it("still speaks for a report row that carries no dismissal stamp at all", async () => {
    // There is no migration: every report written before T-0648 has no
    // dismissed_at. Absent must read as "nobody dismissed this" — the other
    // reading would swallow the warning on every pre-existing install.
    const { dismissedAt: _omitted, ...legacyReport } = dismissibleReport;
    getServerSettings.mockResolvedValue(settingsWith(legacyReport));
    renderBanner();
    const step = await screen.findByTestId("onboarding-step-wake_assistant");
    expect(step.querySelector(".onboarding-banner__reason")?.textContent).toBe(
      "no warden yet"
    );
  });
});

// ── 🔴 the transition, which is the ONLY timeline that actually happens ──────
//
// Every test above is a static snapshot: the report already reads its final
// value at mount. The real first run does not look like that — the cockpit
// mounts while onboarding is still running, and the report turns "failed" tens
// of seconds later. A mount-only fetch passes all eight snapshots and still
// never shows the owner anything, which is how this shipped the first time.
describe("OnboardingBanner — running → failed transition", () => {
  beforeEach(() => {
    sessionStorage.clear();
    getServerSettings.mockReset();
    vi.useFakeTimers();
  });
  afterEach(() => {
    vi.useRealTimers();
  });

  const failedReport = {
    state: "failed",
    startedAt: 1,
    finishedAt: 2,
    steps: [
      {
        name: "install_warden",
        ok: false,
        reason: "installing this machine's warden failed (exit 1)",
        detail: "[ocwarden install] FATAL: claude_bin_unresolved",
      },
    ],
  };

  it("appears once a still-running onboarding turns failed — with no reload", async () => {
    getServerSettings
      .mockResolvedValueOnce(settingsWith({ state: "running", startedAt: 1, finishedAt: 0, steps: [] }))
      .mockResolvedValueOnce(settingsWith({ state: "running", startedAt: 1, finishedAt: 0, steps: [] }))
      .mockResolvedValue(settingsWith(failedReport));

    render(
      <I18nProvider>
        <OnboardingBanner />
      </I18nProvider>
    );
    // mount read: still running → correctly silent
    await act(async () => {});
    expect(screen.queryByTestId("onboarding-banner")).toBeNull();

    // ...and it keeps asking until the answer changes.
    await act(async () => {
      await vi.advanceTimersByTimeAsync(ONBOARDING_POLL_MS * 3);
    });
    const banner = screen.getByTestId("onboarding-banner");
    expect(banner.textContent).toContain(
      "installing this machine's warden failed (exit 1)"
    );
  });

  it("stops polling once the report is terminal (no unbounded traffic)", async () => {
    getServerSettings.mockResolvedValue(settingsWith(failedReport));
    render(
      <I18nProvider>
        <OnboardingBanner />
      </I18nProvider>
    );
    await act(async () => {});
    expect(getServerSettings).toHaveBeenCalledTimes(1);
    await act(async () => {
      await vi.advanceTimersByTimeAsync(ONBOARDING_POLL_MS * 5);
    });
    // Still one: a terminal answer ends the loop.
    expect(getServerSettings).toHaveBeenCalledTimes(1);
  });

  it("gives up after the ceiling instead of polling a wedged report forever", async () => {
    getServerSettings.mockResolvedValue(
      settingsWith({ state: "running", startedAt: 1, finishedAt: 0, steps: [] })
    );
    render(
      <I18nProvider>
        <OnboardingBanner />
      </I18nProvider>
    );
    await act(async () => {
      await vi.advanceTimersByTimeAsync(ONBOARDING_POLL_CEILING_MS + ONBOARDING_POLL_MS * 5);
    });
    const atCeiling = getServerSettings.mock.calls.length;
    await act(async () => {
      await vi.advanceTimersByTimeAsync(ONBOARDING_POLL_MS * 10);
    });
    expect(getServerSettings.mock.calls.length).toBe(atCeiling);
  });

  it("keeps polling through a transient settings-read failure", async () => {
    getServerSettings
      .mockRejectedValueOnce(new Error("server still booting"))
      .mockResolvedValue(settingsWith(failedReport));
    render(
      <I18nProvider>
        <OnboardingBanner />
      </I18nProvider>
    );
    await act(async () => {});
    expect(screen.queryByTestId("onboarding-banner")).toBeNull();
    await act(async () => {
      await vi.advanceTimersByTimeAsync(ONBOARDING_POLL_MS * 2);
    });
    expect(screen.getByTestId("onboarding-banner")).toBeTruthy();
  });
});
