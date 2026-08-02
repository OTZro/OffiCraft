// The always-mounted 備份健康 indicator in the topbar (T-da06).
//
// 🔴 WHY IT IS IN THE TOPBAR AND NOT ONLY ON THE MONITOR PAGE (owner ruling,
// rc-61414359d85a): the failure this ticket removes is one nobody goes looking
// for. A backup that quietly stopped is only discovered by someone who already
// needed the retreat point — by then the discovery is worthless. Putting the
// verdict on a page you have to visit reproduces the same defect one click
// further away, so the indicator is mounted app-wide and the monitor card only
// answers WHY.
//
// Locked here: it is present on every page, it never shows green unless the
// server actually said healthy, and clicking it lands on the monitor page.

import { describe, it, expect, beforeEach, vi } from "vitest";
import { render, screen, fireEvent, waitFor } from "@testing-library/react";
import { I18nProvider } from "./i18n";
import type { BackupHealthView } from "./types";

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

const state = { health: null as BackupHealthView | null, error: false };
vi.mock("./hooks/useBackupHealth", () => ({
  useBackupHealth: () => ({
    health: state.health,
    loading: false,
    error: state.error,
    refresh: () => {},
  }),
}));

import App from "./App";

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

function renderApp() {
  return render(
    <I18nProvider>
      <App />
    </I18nProvider>,
  );
}

describe("topbar · backup health indicator", () => {
  beforeEach(() => {
    window.location.hash = "";
    state.health = healthy();
    state.error = false;
  });

  it("goes red when the server says the backup is failing", async () => {
    state.health = {
      ...healthy(),
      status: "unhealthy",
      code: "never_ran",
      detail: "no scheduled backup has ever landed",
      newestBackupTs: null,
      newestBackupAgeSecs: null,
      sinceTs: 1785600000,
    };
    renderApp();
    const btn = await screen.findByTestId("topbar-backup-health");
    expect(btn.dataset.backupState).toBe("unhealthy");
    // The accessible name must carry the alarm too: an icon-only red square is
    // invisible to a screen reader, and this indicator's entire job is to be
    // noticed.
    expect(btn.getAttribute("aria-label")?.trim()).not.toBe("");
  });

  it("is green ONLY when the server said healthy", async () => {
    renderApp();
    const btn = await screen.findByTestId("topbar-backup-health");
    expect(btn.dataset.backupState).toBe("healthy");
  });

  it("shows 'cannot tell' — not green — when the verdict is unknown or unavailable", async () => {
    state.health = null;
    state.error = true;
    const { unmount } = renderApp();
    let btn = await screen.findByTestId("topbar-backup-health");
    expect(btn.dataset.backupState).toBe("unknown");
    unmount();

    // The server answering "unknown" is the other half of the same rule.
    state.error = false;
    state.health = { ...healthy(), status: "unknown" };
    renderApp();
    btn = await screen.findByTestId("topbar-backup-health");
    expect(btn.dataset.backupState).toBe("unknown");
  });

  it("takes the owner to the monitor page, where the card says why", async () => {
    state.health = { ...healthy(), status: "unhealthy", code: "stale" };
    renderApp();
    fireEvent.click(screen.getByTestId("topbar-backup-health"));
    await waitFor(() => {
      expect(screen.getByTestId("monitor-page-body")).toBeTruthy();
    });
  });
});
