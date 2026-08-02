// The monitor page carries NO 備份健康 card any more (T-5e71).
//
// T-da06 put the card at the very top of the monitor page. The owner saw it,
// said it was not what he expected, and ruled (rc-5ef6f1319f27, option ①) that
// the backup verdict belongs under 設定 › 系統更新與備份 instead. The
// distinguishability behaviour the card was built for did not die with it — it
// moved, and is locked in SettingsPage.backup-health.test.tsx.
//
// This is an absence-AND-position test: it asserts what the monitor page
// actually renders first, so putting the card back — anywhere on the page —
// goes red.

import { describe, it, expect, vi } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { zh } from "../i18n/locales/zh";
import { MonitorPage } from "./MonitorPage";

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
    // Deliberately still stubbed even though the page no longer calls it: the
    // mutation that matters here is "someone put the card back", and if the
    // mock omitted this the restored card would blow up on an undefined
    // function — the test would go red by CRASH instead of by assertion, which
    // proves nothing about position or wording.
    getBackupHealth: () =>
      Promise.resolve({
        status: "healthy",
        code: "",
        detail: "",
        newestBackupTs: 1785600000,
        newestBackupAgeSecs: 3600,
        staleAfterSecs: 43200,
        sinceTs: null,
        checkedTs: 1785603600,
      }),
    subscribeEvents: () => () => {},
  },
}));

function renderPage() {
  return render(
    <I18nProvider>
      <MonitorPage />
    </I18nProvider>,
  );
}

describe("monitor page · no backup health card", () => {
  it("renders no backup health section at all", async () => {
    const utils = renderPage();
    // Wait for the page's own first section so this is not a green-because-
    // nothing-rendered-yet pass.
    await waitFor(() =>
      expect(utils.getByText(zh.monitor.accountsTitle)).toBeTruthy(),
    );
    expect(screen.queryByTestId("mon-backup-health")).toBeNull();
    expect(screen.queryByTestId("mon-backup-status")).toBeNull();
    expect(utils.container.textContent).not.toContain(zh.backupHealth.title);
  });

  it("leads with 帳號資訊 — the backup card no longer sits above it", async () => {
    const utils = renderPage();
    await waitFor(() =>
      expect(utils.getByText(zh.monitor.accountsTitle)).toBeTruthy(),
    );
    // The FIRST section title on the page is the accounts one. A card put back
    // at the top would take this slot, so this assertion is about position,
    // not merely about absence.
    const firstTitle = utils.container.querySelector(".mon-section__title");
    expect(firstTitle?.textContent).toBe(zh.monitor.accountsTitle);
  });
});
