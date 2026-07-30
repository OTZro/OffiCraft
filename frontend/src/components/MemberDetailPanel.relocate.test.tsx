// MemberDetailPanel · 改機器 (relocate) control.
//
// Locked here (mirrors the worker panel's 改機器, but placement-only for a
// roster member):
//   1. The 機器 label carries a 改機器 button (data-testid mp-relocate) whenever
//      onRelocate is wired.
//   2. With 2+ online machines the button opens the machine picker; confirming a
//      pick calls onRelocate with the chosen machineId (→ relocateMember at the
//      call site). It NEVER goes through activateMember — a relocate is not a wake.

import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, fireEvent, waitFor } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { MemberDetailPanel } from "./MemberDetailPanel";
import type { Member } from "../types";
import type { MachineView } from "../types";

const machine = (id: string, displayName: string): MachineView => ({
  machineId: id,
  displayName,
  online: true,
  isSelf: false,
  binStatus: null,
  claudeVersion: null,
  claudeCredSource: null,
  claudeSubReadable: null,
});
const listMachines = vi.fn<() => Promise<MachineView[]>>(() =>
  Promise.resolve([machine("mach-a", "Machine A"), machine("mach-b", "Machine B")]),
);
const relocateMember = vi.fn(async (_id: string, _machineId: string) => {});

vi.mock("../api", () => ({
  api: {
    listMachines: () => listMachines(),
    relocateMember: (id: string, machineId: string) =>
      relocateMember(id, machineId),
    activateMember: vi.fn(),
    patchMember: vi.fn(),
    getBootstrap: () =>
      Promise.resolve({ role: "assistant", name: "", taskType: "", context: "" }),
    listWebhooks: () => Promise.resolve([]),
    subscribeEvents: () => () => {},
  },
}));

function mkMember(over: Partial<Member> = {}): Member {
  return {
    id: "mira",
    memberId: "MB-AST001",
    name: "Mira",
    role: "assistant",
    status: "offline",
    lifecycle: "offline",
    model: "opus",
    effort: "medium",
    kind: "assistant",
    desiredMachineId: "mach-a",
    machine: null,
    account: null,
    contextPct: null,
    estimatedCost: null,
    bankedCost: null,
    tmuxSession: "member-mira",
    refocusSince: null,
    lastOp: "",
    lastOpOk: null,
    lastOpLog: "",
    lastOpAt: null,
    unreadCount: 0,
    ...over,
  };
}

function renderPanel(over: Partial<Member> = {}) {
  const onRelocate = vi.fn(async (machineId: string) => {
    await relocateMember("mira", machineId);
  });
  const onActivate = vi.fn(async () => ({ activationPending: false }));
  const utils = render(
    <I18nProvider>
      <MemberDetailPanel
        member={mkMember(over)}
        onBack={vi.fn()}
        onActivate={onActivate}
        onRelocate={onRelocate}
      />
    </I18nProvider>,
  );
  return { ...utils, onActivate, onRelocate };
}

beforeEach(() => {
  listMachines.mockClear();
  relocateMember.mockClear();
  Element.prototype.scrollIntoView = vi.fn();
});

describe("MemberDetailPanel — unified wake/change settings", () => {
  it("keeps the detail fields read-only and sends an offline setting once through activate", async () => {
    const { getByTestId, queryByTestId, onActivate } = renderPanel();
    expect(queryByTestId("mp-relocate")).toBeNull();
    expect(queryByTestId("mp-model-effort-edit")).toBeNull();

    fireEvent.click(getByTestId("member-action-spawn"));
    const dialog = getByTestId("me-runtime-select").closest("[role=dialog]")!;
    const select = dialog.querySelector("select.machine-picker__select") as HTMLSelectElement;
    await waitFor(() => expect(select.options).toHaveLength(2));
    fireEvent.change(select, { target: { value: "mach-b" } });
    fireEvent.click(dialog.querySelector(".btn--accent")!);

    await waitFor(() => expect(onActivate).toHaveBeenCalledWith("mach-b"));
  });

  it("shows the observed machine plus a pending target, then removes the target after arrival", async () => {
    const initial = mkMember({
      status: "online", lifecycle: "online", machine: "mach-a", desiredMachineId: "mach-b",
    });
    const { getByTestId, queryByTestId, rerender } = render(
      <I18nProvider><MemberDetailPanel member={initial} onBack={vi.fn()} /></I18nProvider>,
    );
    await waitFor(() => expect(getByTestId("mp-machine").textContent).toContain("Machine A"));
    expect(getByTestId("mp-machine-transition").textContent).toContain("→ 要換到 Machine B");

    rerender(
      <I18nProvider><MemberDetailPanel member={mkMember({
        status: "online", lifecycle: "online", machine: "mach-b", desiredMachineId: "mach-b",
      })} onBack={vi.fn()} /></I18nProvider>,
    );
    expect(queryByTestId("mp-machine-transition")).toBeNull();
  });

  it("uses Change to apply an awake member's settings through one relocate", async () => {
    const { getByTestId, onActivate, onRelocate } = renderPanel({
      status: "online",
      lifecycle: "online",
      machine: "mach-a",
    });
    fireEvent.click(getByTestId("mp-change"));
    const dialog = getByTestId("me-runtime-select").closest("[role=dialog]")!;
    const select = dialog.querySelector("select.machine-picker__select") as HTMLSelectElement;
    await waitFor(() => expect(select.options).toHaveLength(2));
    fireEvent.change(select, { target: { value: "mach-b" } });
    fireEvent.click(dialog.querySelector(".btn--accent")!);

    await waitFor(() => expect(onRelocate).toHaveBeenCalledWith("mach-b"));
    expect(onActivate).not.toHaveBeenCalled();
  });
});
