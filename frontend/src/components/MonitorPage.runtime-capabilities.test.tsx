// Runtime readiness column — Monitor §2 machine panel (T-90be ⑤ + T-b36a).
//
// Why this column exists: `machineSupportsRuntime` fail-closes when it cannot
// read `installed`/`logged_in`, so a machine whose codex probe says "not logged
// in" silently stops accepting codex work and its worker sits stamped
// machine_unavailable. Until now the server carried that map to the browser and
// the UI dropped it on the floor, so the ONLY explanation for a stuck worker
// existed nowhere on screen.
//
// Why it is rendered WITH its age: telemetry is never cleared on disconnect
// (only on dismissal), so this map outlives the machine that reported it. A
// capability rendered plain would be the same lie the hardware numbers used to
// tell — "as of some unknown moment" presented as "right now". Showing the old
// values MARKED is the deliberate middle: they are the only diagnosis available
// for an offline box, so they are kept, but never as a current fact.

import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, within } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { MonitorPage, runtimeCapabilityText } from "./MonitorPage";
import type { Member, MachineView, MonMachineView } from "../types";

const listMembers = vi.fn(async (): Promise<Member[]> => []);
const listMachines = vi.fn(async (): Promise<MachineView[]> => []);
const getMonitoring = vi.fn(async () => ({
  accounts: [],
  sessions: [],
  machines: [] as MonMachineView[],
}));

vi.mock("../api", () => ({
  api: {
    listMembers: () => listMembers(),
    listMachines: () => listMachines(),
    getMonitoring: () => getMonitoring(),
    listOutsourceWorkers: () => Promise.resolve([]),
    listTasks: () => Promise.resolve([]),
    listTaskTypes: () => Promise.resolve([]),
    getServerSettings: () => Promise.resolve({ outsourceMaxParallel: 0 }),
    subscribeEvents: () => () => {},
  },
}));

const registryRow = (id: string): MachineView => ({
  machineId: id,
  displayName: "seth-m5",
  online: true,
  isSelf: false,
  binStatus: null,
  claudeVersion: null,
  claudeCredSource: null,
  claudeSubReadable: null,
});

/** One monitoring card. `stale` is the SERVER's verdict (the freshness window
 * lives there, not here) — null means the machine never probed. */
const card = (
  stale: boolean | null,
  capabilities: MonMachineView["runtimeCapabilities"]
): MonMachineView => ({
  machine: "m-server-self",
  displayName: "seth-m5",
  agents: 0,
  accounts: [],
  cpuPct: null,
  ramPct: null,
  batteryPct: null,
  acPower: null,
  hardwareTs: null,
  runtimeCapabilities: capabilities,
  runtimeCapabilitiesTs: stale == null ? null : 1_700_000_000,
  runtimeCapabilitiesStale: stale,
  binStatus: null,
  claudeVersion: null,
  claudeCredSource: null,
  claudeSubReadable: null,
});

function mount(machineCard: MonMachineView) {
  listMembers.mockResolvedValue([
    {
      id: "m-server-self",
      name: "warden",
      kind: "warden",
      desiredMachineId: "",
    } as unknown as Member,
  ]);
  listMachines.mockResolvedValue([registryRow("m-server-self")]);
  getMonitoring.mockResolvedValue({
    accounts: [],
    sessions: [],
    machines: [machineCard],
  });
  return render(
    <I18nProvider>
      <MonitorPage />
    </I18nProvider>
  );
}

const FRESH = {
  claude: { installed: true, loggedIn: true, version: "2.1.211" },
  codex: { installed: true, loggedIn: false, version: "0.52.0" },
};

describe("MonitorPage runtime capabilities", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it("renders a fresh capability map, with a reported false as an answer", async () => {
    mount(card(false, FRESH));
    const cell = await screen.findByTestId("mon-runtimes");
    // codex is INSTALLED but NOT logged in — the exact state that makes
    // placement refuse this machine. It must read as a refusal (✗), never as
    // the same "—" a machine that has told us nothing gets.
    expect(cell.textContent).toContain("claude ✓");
    expect(cell.textContent).toContain("codex ✗");
    // Fresh means fresh: no stale marker anywhere on the row.
    expect(within(cell).queryByTestId("mon-runtimes-stale")).toBeNull();
  });

  it("marks a stale capability map instead of showing it as current", async () => {
    mount(card(true, FRESH));
    const cell = await screen.findByTestId("mon-runtimes");
    // The values SURVIVE — they are the only diagnosis for a worker that will
    // not place on this box…
    expect(cell.textContent).toContain("codex ✗");
    // …but they are explicitly no longer presented as current.
    const marker = within(cell).getByTestId("mon-runtimes-stale");
    expect(marker.textContent).toBeTruthy();
  });

  it("does not present a map of unknown age as current", async () => {
    // stale=null with values present is the fail-closed case (the server could
    // not date the probe). Unknown age is not freshness.
    mount(card(null, FRESH));
    const cell = await screen.findByTestId("mon-runtimes");
    expect(within(cell).getByTestId("mon-runtimes-stale")).toBeTruthy();
  });

  it("shows an honest dash when the machine never reported a capability map", async () => {
    mount(card(null, {}));
    const cell = await screen.findByTestId("mon-runtimes");
    expect(cell.textContent).toBe("—");
    // "Never told us" must not wear the stale badge either — nothing expired.
    expect(within(cell).queryByTestId("mon-runtimes-stale")).toBeNull();
  });
});

describe("runtimeCapabilityText", () => {
  it("distinguishes a reported false from an unreported unknown", () => {
    expect(
      runtimeCapabilityText({
        codex: { installed: false, loggedIn: null, version: null },
      })
    ).toBe("codex ✗");
    expect(
      runtimeCapabilityText({
        codex: { installed: true, loggedIn: false, version: null },
      })
    ).toBe("codex ✗");
    expect(
      runtimeCapabilityText({
        codex: { installed: null, loggedIn: null, version: null },
      })
    ).toBe("codex ?");
    // An empty map is "never probed", which the caller renders as the dash —
    // NOT as a runtime that is not ready.
    expect(runtimeCapabilityText({})).toBeNull();
    expect(runtimeCapabilityText(undefined)).toBeNull();
  });
});
