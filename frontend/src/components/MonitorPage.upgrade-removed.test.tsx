// The one-click upgrade button is GONE from the machine row — a reverse test.
//
// It was removed along with the shape badge, and its whole test file went with
// it, which left the removal itself with zero protection: nothing would notice
// a revert, a merge resurrecting the button, or a well-meaning "restore the
// upgrade action" patch. A deletion is a behaviour like any other, and the only
// way to defend one is to assert the absence.
//
// The row rendered here is the exact one that USED to grow a button: online,
// and with the server's fingerprint verdict saying a newer build exists
// ("stale"). Asserting on a machine that would never have shown one would be a
// test that passes for the wrong reason.

import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { MonitorPage } from "./MonitorPage";
import { mockApi } from "../api/mock";
import type { Member, MachineView, BinStatus } from "../types";

const listMembers = vi.fn(async (): Promise<Member[]> => []);
const listMachines = vi.fn(async (): Promise<MachineView[]> => []);

vi.mock("../api", () => ({
  api: {
    listMembers: () => listMembers(),
    listMachines: () => listMachines(),
    getMonitoring: () =>
      Promise.resolve({ accounts: [], sessions: [], machines: [] }),
    listOutsourceWorkers: () => Promise.resolve([]),
    listTasks: () => Promise.resolve([]),
    listTaskTypes: () => Promise.resolve([]),
    getServerSettings: () => Promise.resolve({ outsourceMaxParallel: 0 }),
    subscribeEvents: () => () => {},
  },
}));

const machine = (binStatus: BinStatus, online: boolean): MachineView => ({
  machineId: "m-upgradable",
  displayName: "m-upgradable",
  online,
  isSelf: false,
  binStatus,
  wardenShape: null,
  cutoverEffect: "effective",
  claudeVersion: null,
  claudeCredSource: null,
  claudeSubReadable: null,
});

describe("MonitorPage machine actions", () => {
  beforeEach(() => {
    listMembers.mockResolvedValue([]);
  });

  it("offers no upgrade action on the row that used to have one", async () => {
    listMachines.mockResolvedValue([machine("stale", true)]);
    render(
      <I18nProvider>
        <MonitorPage />
      </I18nProvider>
    );
    await screen.findByTestId("mon-machine-id");
    expect(screen.queryByTestId("mon-upgrade-btn")).toBeNull();
    // Also by name, so a button that comes back under a new testid is still
    // caught. /i because the label's case is not the point.
    expect(screen.queryByRole("button", { name: /upgrade/i })).toBeNull();
  });

  it("has no upgrade entry point left on the client at all", async () => {
    // The button's absence from the page is not enough: an unreachable
    // `upgradeMachine` on the client keeps the wire call, its mapper and its
    // fixture alive as dead weight that reads like a supported feature.
    expect(Object.keys(mockApi)).not.toContain("upgradeMachine");
  });
});
