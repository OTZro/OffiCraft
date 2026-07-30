// MemberDetailPanel · 喚醒/更改 的文案不得說謊 (T-927a spec §3.4).
//
// The unified settings dialog is shared by three presences, and its confirm
// reaches TWO different backends: an online member is applied gracefully
// (relocate / PATCH — one handover), while offline AND `waking` both reach
// activate (a start, no graceful wrap-up). So the wording follows `online`, not
// `awake` — 「更改」 promises a graceful handover and may only appear where one
// actually happens. Both directions are pinned here: flipping the predicate
// back to `awake` reddens the waking case, and dropping 「更改」 for online
// reddens the other.

import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, fireEvent, waitFor } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { zh } from "../i18n/locales/zh";
import { MemberDetailPanel } from "./MemberDetailPanel";
import type { Member, MachineView } from "../types";

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

vi.mock("../api", () => ({
  api: {
    listMachines: () =>
      Promise.resolve([machine("mach-a", "Machine A"), machine("mach-b", "Machine B")]),
    relocateMember: vi.fn(),
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
  const onActivate = vi.fn(async () => ({ activationPending: false }));
  const onRelocate = vi.fn(async () => ({ relocationPending: false }));
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

/** The wake/change entry is gated on the machine registry (0 online machines ⇒
 * dead affordance), and that registry arrives asynchronously — clicking before
 * it lands hits a disabled button and opens nothing. */
async function armedAction(
  getByTestId: (id: string) => HTMLElement,
  testId: string,
) {
  await waitFor(() =>
    expect((getByTestId(testId) as HTMLButtonElement).disabled).toBe(false),
  );
  return getByTestId(testId);
}

async function openedDialog(getByTestId: (id: string) => HTMLElement) {
  const dialog = getByTestId("me-runtime-select").closest("[role=dialog]")!;
  const select = dialog.querySelector(
    "select.machine-picker__select",
  ) as HTMLSelectElement;
  await waitFor(() => expect(select.options).toHaveLength(2));
  return {
    dialog,
    select,
    title: dialog.querySelector(".machine-picker__title")!,
    confirm: dialog.querySelector(".btn--accent")! as HTMLButtonElement,
  };
}

beforeEach(() => {
  Element.prototype.scrollIntoView = vi.fn();
});

describe("MemberDetailPanel — 喚醒/更改 wording matches what is dispatched", () => {
  it("says 更改 for an online member, and that confirm really is the graceful relocate", async () => {
    const { getByTestId, onActivate, onRelocate } = renderPanel({
      status: "online",
      lifecycle: "online",
      machine: "mach-a",
    });
    fireEvent.click(await armedAction(getByTestId, "mp-change"));
    const { title, confirm, select } = await openedDialog(getByTestId);

    expect(title.textContent).toBe(zh.mp.change);
    expect(confirm.textContent).toBe(zh.mp.change);

    fireEvent.change(select, { target: { value: "mach-b" } });
    fireEvent.click(confirm);
    await waitFor(() => expect(onRelocate).toHaveBeenCalledWith("mach-b"));
    expect(onActivate).not.toHaveBeenCalled();
  });

  it("says 喚醒 for a waking member, because that confirm dispatches an activate", async () => {
    const { getByTestId, onActivate, onRelocate } = renderPanel({
      status: "waking",
      lifecycle: "waking",
      machine: "mach-a",
    });
    fireEvent.click(await armedAction(getByTestId, "member-action-spawn"));
    const { title, confirm, select } = await openedDialog(getByTestId);

    expect(title.textContent).toBe(zh.lifecycle.action.spawn);
    expect(confirm.textContent).toBe(zh.lifecycle.action.spawn);
    expect(title.textContent).not.toBe(zh.mp.change);

    fireEvent.change(select, { target: { value: "mach-b" } });
    fireEvent.click(confirm);
    await waitFor(() => expect(onActivate).toHaveBeenCalledWith("mach-b"));
    expect(onRelocate).not.toHaveBeenCalled();
  });
});
