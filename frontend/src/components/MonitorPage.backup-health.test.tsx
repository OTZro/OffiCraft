// The 備份健康 card on the monitor page — Monitor §backup (T-da06).
//
// The backup engine has always reported failure, staleness and "never ran" —
// into the server log, which nothing on this machine aggregates and the cockpit
// cannot read. So the studio could go days without a retreat point while every
// screen a human looks at said nothing. This card is where the owner finds out
// WHY the topbar indicator went red; the indicator only says THAT it did.
//
// The assertions below are deliberately about DISTINGUISHABILITY, not about
// pretty output: a card that renders the same thing for "broken", "cannot tell"
// and "fine" has reintroduced the original defect while looking implemented.

import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { MonitorPage } from "./MonitorPage";
import type { BackupHealthView } from "../types";

const getBackupHealth = vi.fn(async (): Promise<BackupHealthView> => healthy());

function healthy(): BackupHealthView {
  return {
    status: "healthy",
    code: "",
    detail: "",
    newestBackupTs: 1785600000,
    newestBackupAgeSecs: 3600,
    staleAfterSecs: 43200,
    sinceTs: null,
    checkedTs: 1785603600,
  };
}

vi.mock("../api", () => ({
  api: {
    listMembers: () => Promise.resolve([]),
    listMachines: () => Promise.resolve([]),
    getMonitoring: () =>
      Promise.resolve({ accounts: [], sessions: [], machines: [] }),
    listOutsourceWorkers: () => Promise.resolve([]),
    listTasks: () => Promise.resolve([]),
    listTaskTypes: () => Promise.resolve([]),
    getServerSettings: () => Promise.resolve({ outsourceMaxParallel: 0 }),
    getBackupHealth: () => getBackupHealth(),
    subscribeEvents: () => () => {},
  },
}));

function renderPage() {
  render(
    <I18nProvider>
      <MonitorPage />
    </I18nProvider>,
  );
}

async function status(): Promise<HTMLElement> {
  return await waitFor(() => screen.getByTestId("mon-backup-status"));
}

describe("monitor page · backup health card", () => {
  beforeEach(() => {
    getBackupHealth.mockReset();
    getBackupHealth.mockResolvedValue(healthy());
  });

  it("shows a healthy backup with the evidence behind it, and explains nothing", async () => {
    renderPage();
    const el = await status();
    expect(el.dataset.backupState).toBe("healthy");
    // A green light has no failure to explain — filler there would dilute the
    // red case it exists to make noticeable.
    expect(screen.queryByTestId("mon-backup-reason")).toBeNull();
    // But it must name the backup it is standing on, or it is a bare assertion.
    expect(screen.getByTestId("mon-backup-newest").textContent).not.toBe("—");
  });

  it("shows a STALE backup — a previous one exists, but the schedule went quiet", async () => {
    // 🔴 This is the branch that had never been executed anywhere before this
    // ticket: staleness was only ever exercised as "there is no backup at all",
    // which is the unavoidable first-run state. A schedule that worked and then
    // died leaves a real, recent-looking file behind, and THAT is the case the
    // owner needs told.
    getBackupHealth.mockResolvedValue({
      ...healthy(),
      status: "unhealthy",
      code: "stale",
      detail: "newest scheduled backup is 30h0m0s old (alarm after 12h0m0s)",
      newestBackupAgeSecs: 108000,
      sinceTs: Math.floor(Date.now() / 1000) - 7200,
    });
    renderPage();
    const el = await status();
    expect(el.dataset.backupState).toBe("unhealthy");
    expect(screen.getByTestId("mon-backup-reason").textContent?.trim()).not.toBe(
      "",
    );
    // How long it has been broken must be visible: an outage that reads as
    // brand new every time nobody can judge.
    expect(screen.getByTestId("mon-backup-since")).toBeTruthy();
  });

  it("says NEVER, in words, when no scheduled backup has ever landed", async () => {
    getBackupHealth.mockResolvedValue({
      ...healthy(),
      status: "unhealthy",
      code: "never_ran",
      detail: "no scheduled backup has ever landed (watching for 30h0m0s)",
      newestBackupTs: null,
      newestBackupAgeSecs: null,
      sinceTs: Math.floor(Date.now() / 1000) - 3600,
    });
    renderPage();
    const el = await status();
    expect(el.dataset.backupState).toBe("unhealthy");
    // "never" is a FACT, not a missing value — rendering it as the same dash
    // used for "not measured" is exactly how this alarm would go unnoticed.
    const newest = screen.getByTestId("mon-backup-newest").textContent ?? "";
    expect(newest.trim()).not.toBe("—");
    expect(newest.trim()).not.toBe("");
  });

  it("renders a server that cannot tell as UNKNOWN, never as healthy", async () => {
    getBackupHealth.mockResolvedValue({
      ...healthy(),
      status: "unknown",
      code: "",
      detail: "the backup watchdog has not reported yet",
      newestBackupTs: null,
      newestBackupAgeSecs: null,
    });
    renderPage();
    const el = await status();
    expect(el.dataset.backupState).toBe("unknown");
    expect(el.dataset.backupState).not.toBe("healthy");
    expect(screen.getByTestId("mon-backup-reason").textContent?.trim()).not.toBe(
      "",
    );
  });

  it("renders a FAILED load as unknown too — an unanswerable question is not good news", async () => {
    getBackupHealth.mockRejectedValue(new Error("network down"));
    renderPage();
    const el = await status();
    expect(el.dataset.backupState).toBe("unknown");
  });

  it("shows the server's diagnostic as secondary text, not as the headline", async () => {
    getBackupHealth.mockResolvedValue({
      ...healthy(),
      status: "unhealthy",
      code: "failed",
      detail: "scheduled backup failed: disk went away",
    });
    renderPage();
    await status();
    const reason = screen.getByTestId("mon-backup-reason").textContent ?? "";
    // The sentence the owner reads is translated, derived from the code; the
    // server's English diagnostic sits in its own labelled row so a wording
    // change on the server can never silently rewrite the UI.
    expect(reason).not.toContain("disk went away");
    expect(screen.getByTestId("mon-backup-detail").textContent).toContain(
      "disk went away",
    );
  });
});
