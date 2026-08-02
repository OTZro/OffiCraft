// The topbar carries NO 備份健康 indicator (T-5e71, owner 2026-08-02).
//
// T-da06 mounted a backup light in the topbar app-wide, on an earlier owner
// ruling (rc-61414359d85a) that a verdict on a page you have to visit is one
// nobody reads. The owner then SAW it and reversed himself
// (rc-5ef6f1319f27, option ①): the verdict moves under 設定 › 系統更新與備份
// and the topbar icon goes away. The earlier reasoning is not refuted — the
// discoverability cost is real, and accepting it is his call, not ours.
//
// Locked here: no backup control in the topbar, on any tab. This is an absence
// test, so it asserts the RENDERED topbar, not an import list — put the button
// back and it goes red.

import { describe, it, expect, beforeEach, vi } from "vitest";
import { render, screen, fireEvent, waitFor } from "@testing-library/react";
import { I18nProvider } from "./i18n";
import { zh } from "./i18n/locales/zh";

// Sibling mount-fetch hooks and heavy page bodies are irrelevant to the topbar
// under test — same seam as App.topbar-version.test.tsx.
vi.mock("./hooks/useChatUnread", () => ({ useChatUnread: () => 0 }));
vi.mock("./hooks/useReplyCardCount", () => ({ useReplyCardCount: () => 0 }));
vi.mock("./hooks/useTaskCount", () => ({ useTaskCount: () => 0 }));
vi.mock("./hooks/useOrgName", () => ({
  useOrgName: (fallback: string) => ({ orgName: fallback, setOrgName: () => {} }),
}));
vi.mock("./hooks/useVersion", () => ({
  useVersion: () => ({ version: null, loading: false, error: false, refresh: () => {} }),
}));
vi.mock("./components/OfficePage", () => ({ OfficePage: () => null }));
vi.mock("./components/RepliesPage", () => ({ RepliesPage: () => null }));
vi.mock("./components/TasksPage", () => ({ TasksPage: () => null }));
vi.mock("./components/MonitorPage", () => ({
  MonitorPage: () => <div data-testid="monitor-page-body" />,
}));
vi.mock("./components/SettingsPage", () => ({ SettingsPage: () => null }));

import App from "./App";

function renderApp() {
  return render(
    <I18nProvider>
      <App />
    </I18nProvider>,
  );
}

describe("topbar · no backup indicator", () => {
  beforeEach(() => {
    window.location.hash = "";
  });

  it("renders no backup control in the topbar", () => {
    const utils = renderApp();
    expect(screen.queryByTestId("topbar-backup-health")).toBeNull();
    const actions = utils.container.querySelector(".topbar__actions");
    expect(actions).toBeTruthy();
    expect(actions?.querySelector(".backup-indicator")).toBeNull();
    // Nothing in the topbar spells the verdict out either — the accessible
    // names of the removed light were exactly these three status strings.
    expect(screen.queryByLabelText(zh.backupHealth.statusHealthy)).toBeNull();
    expect(screen.queryByLabelText(zh.backupHealth.statusUnhealthy)).toBeNull();
    expect(screen.queryByLabelText(zh.backupHealth.statusUnknown)).toBeNull();
  });

  it("still renders no backup control after navigating to the monitor tab", async () => {
    const utils = renderApp();
    fireEvent.click(screen.getByText(zh.nav.monitor));
    // Navigation goes through the URL hash, so the new page lands on a later
    // tick — assert it arrived, otherwise "no indicator" would pass on a page
    // that never changed.
    await waitFor(() =>
      expect(screen.getByTestId("monitor-page-body")).toBeTruthy(),
    );
    expect(screen.queryByTestId("topbar-backup-health")).toBeNull();
    expect(
      utils.container.querySelector(".topbar__actions .backup-indicator"),
    ).toBeNull();
  });
});
