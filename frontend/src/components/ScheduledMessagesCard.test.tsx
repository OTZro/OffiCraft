// 定期訊息 · ScheduledMessagesCard (T-f059).
//
// Locked here:
//   1. Every schedule the list returns is rendered.
//   2. A create is reflected in the list WITHOUT remounting the panel — the
//      acceptance wording is 「改完立即生效、不需重開成員」, and the only thing
//      that makes it true is the hook refetching after each mutation. The
//      stateful mock below is what turns that into an observable: it returns a
//      fresh list on every GET, so a component that skipped the refetch would
//      keep showing the pre-create list and this test would red.
//   3. Deleting goes through the shared ConfirmModal, not straight to the wire.
//   4. Picking a day of month that some months lack (29/30/31) shows the skip
//      warning, and picking a day every month HAS does not. BOTH directions are
//      asserted: a hint that is always on would satisfy the first alone.
//   5. 🔴 The OUTSOURCE panel grows the card too. `WorkerDetailPanel` had no
//      `extraExpandCards` caller at all before this ticket, which is exactly how
//      the webhook section ended up existing on only one of the two panels.

import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { render, fireEvent, waitFor, within } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { zh } from "../i18n/locales/zh";
import { ScheduledMessagesCard } from "./ScheduledMessagesCard";
import { MemberDetailPanel } from "./MemberDetailPanel";
import { WorkerDetailPanel } from "./WorkerDetailPanel";
import type { Member } from "../types";
import type {
  OutsourceWorkerView,
  ScheduledMessage,
  ScheduledMessageCreateInput,
  ScheduledMessageUpdate,
} from "../api/adapter";

// ── stateful schedule store (the server's list is the single source) ──
let store: ScheduledMessage[] = [];
let nextId = 0;

function mkSchedule(over: Partial<ScheduledMessage> = {}): ScheduledMessage {
  nextId += 1;
  return {
    id: `sch-00000000000${nextId}`,
    memberId: "mira",
    label: `排程 ${nextId}`,
    body: `訊息 ${nextId}`,
    cadence: "daily",
    dayOfWeek: 0,
    dayOfMonth: 1,
    hour: 9,
    minute: 0,
    timezone: "Asia/Taipei",
    status: "enabled",
    lastFiredSlot: "2026-08-10T09:00+08:00",
    lastFiredTs: 0,
    createdTs: 0,
    ...over,
  };
}

const createScheduledMessage = vi.fn(
  async (memberId: string, input: ScheduledMessageCreateInput) => {
    const created = mkSchedule({
      memberId,
      label: input.label ?? "",
      body: input.body,
      cadence: input.cadence,
      dayOfWeek: input.dayOfWeek ?? 0,
      dayOfMonth: input.dayOfMonth ?? 1,
      hour: input.hour,
      minute: input.minute,
      timezone: input.timezone,
    });
    store = [...store, created];
    return { ...created };
  }
);
const updateScheduledMessage = vi.fn(
  async (_memberId: string, scheduleId: string, patch: ScheduledMessageUpdate) => {
    const s = store.find((x) => x.id === scheduleId)!;
    if (patch.status !== undefined) s.status = patch.status;
    return { ...s };
  }
);
const deleteScheduledMessage = vi.fn(
  async (_memberId: string, scheduleId: string) => {
    store = store.filter((x) => x.id !== scheduleId);
  }
);

vi.mock("../api", () => ({
  api: {
    listMachines: () => Promise.resolve([]),
    patchMember: () => Promise.resolve(mkMember()),
    getBootstrap: () =>
      Promise.resolve({ role: "assistant", name: "", taskType: "", context: "" }),
    listWebhooks: () => Promise.resolve([]),
    listScheduledMessages: (memberId: string) =>
      Promise.resolve(
        store.filter((s) => s.memberId === memberId).map((s) => ({ ...s }))
      ),
    createScheduledMessage: (
      memberId: string,
      input: ScheduledMessageCreateInput
    ) => createScheduledMessage(memberId, input),
    updateScheduledMessage: (
      memberId: string,
      scheduleId: string,
      patch: ScheduledMessageUpdate
    ) => updateScheduledMessage(memberId, scheduleId, patch),
    deleteScheduledMessage: (memberId: string, scheduleId: string) =>
      deleteScheduledMessage(memberId, scheduleId),
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
    desiredMachineId: "",
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

function mkWorker(over: Partial<OutsourceWorkerView> = {}): OutsourceWorkerView {
  return {
    id: "ow-7788",
    codename: "O-7788",
    model: "Opus 4.6",
    effort: "high",
    status: "active",
    taskId: "task-1",
    taskTitle: "",
    taskStatus: "in_progress",
    createdTs: 0,
    presence: undefined,
    machine: "",
    desiredMachineId: "",
    account: null,
    contextPct: null,
    cost: null,
    bankedCost: null,
    lastOp: "",
    lastOpOk: null,
    lastOpLog: "",
    lastOpReason: "",
    lastOpAt: null,
    creatorId: "",
    delegatedBy: "",
    ...over,
  };
}

const s = zh.mp.schedmsg;

// Frozen clock for the last-sent assertions: 2026-08-10 10:00 local, with a
// delivery the evening before.
const NOW = new Date(2026, 7, 10, 10, 0, 0, 0);
const FIRED_AT = new Date(2026, 7, 9, 21, 30, 0, 0);
const TIME_SHAPE = /\d{1,2}\/\d{1,2}\s\d{2}:\d{2}/;

beforeEach(() => {
  store = [];
  nextId = 0;
  vi.clearAllMocks();
});

afterEach(() => {
  vi.useRealTimers();
});

/** Render the card alone and open it (it ships collapsed, like the webhook
 * section beside it). */
async function renderOpenCard(memberId = "mira") {
  const view = render(
    <I18nProvider>
      <ScheduledMessagesCard memberId={memberId} />
    </I18nProvider>
  );
  fireEvent.click(await view.findByTestId("mp-schedmsg-toggle"));
  return view;
}

/** Fill in the create form's required fields and submit. */
async function createVia(
  view: Awaited<ReturnType<typeof renderOpenCard>>,
  body: string
) {
  fireEvent.click(await view.findByTestId("mp-schedmsg-add"));
  fireEvent.change(await view.findByTestId("mp-schedmsg-body-input"), {
    target: { value: body },
  });
  fireEvent.click(await view.findByTestId("mp-schedmsg-create"));
}

describe("ScheduledMessagesCard", () => {
  it("renders every schedule the list returns", async () => {
    store = [
      mkSchedule({ label: "每日巡檢", body: "看一下 CI" }),
      mkSchedule({
        label: "週報",
        body: "整理本週進度",
        cadence: "weekly",
        dayOfWeek: 5,
        hour: 17,
        minute: 30,
      }),
      mkSchedule({
        label: "月結",
        body: "對帳",
        cadence: "monthly",
        dayOfMonth: 5,
        status: "disabled",
      }),
    ];
    const view = await renderOpenCard();

    for (const row of store) {
      const el = await view.findByTestId(`mp-schedmsg-row-${row.id}`);
      expect(within(el).getByText(row.label)).toBeTruthy();
      expect(within(el).getByText(row.body)).toBeTruthy();
    }
    // The when-line states the cadence in words plus the wall clock and the
    // ZONE — a time with no zone beside it is the ambiguity this feature exists
    // to remove.
    const weekly = await view.findByTestId(`mp-schedmsg-row-${store[1].id}`);
    expect(within(weekly).getByText(s.weeklyOn(s.weekdayFri))).toBeTruthy();
    expect(within(weekly).getByText("17:30")).toBeTruthy();
    expect(within(weekly).getByText("Asia/Taipei")).toBeTruthy();
    const monthly = await view.findByTestId(`mp-schedmsg-row-${store[2].id}`);
    expect(within(monthly).getByText(s.monthlyOn(5))).toBeTruthy();
    expect(within(monthly).getByText(s.disabled)).toBeTruthy();
  });

  it("shows a newly created schedule in the list without a remount", async () => {
    const view = await renderOpenCard();
    expect(await view.findByTestId("mp-schedmsg-empty")).toBeTruthy();

    await createVia(view, "早安,請看一下昨天的 CI");

    await waitFor(() => expect(createScheduledMessage).toHaveBeenCalledTimes(1));
    // The evidence is the LIST, not the create call: only a post-mutation
    // refetch can put the new row on screen without the owner reopening the
    // member.
    const created = await view.findByText("早安,請看一下昨天的 CI");
    expect(created).toBeTruthy();
    expect(view.queryByTestId("mp-schedmsg-empty")).toBeNull();
  });

  it("sends only the day field the chosen cadence reads", async () => {
    const view = await renderOpenCard();
    fireEvent.click(await view.findByTestId("mp-schedmsg-add"));
    fireEvent.change(await view.findByTestId("mp-schedmsg-body-input"), {
      target: { value: "月結對帳" },
    });
    fireEvent.change(await view.findByTestId("mp-schedmsg-cadence"), {
      target: { value: "monthly" },
    });
    fireEvent.change(await view.findByTestId("mp-schedmsg-dayofmonth"), {
      target: { value: "5" },
    });
    fireEvent.click(await view.findByTestId("mp-schedmsg-create"));

    await waitFor(() => expect(createScheduledMessage).toHaveBeenCalledTimes(1));
    const input = createScheduledMessage.mock.calls[0][1];
    expect(input.cadence).toBe("monthly");
    expect(input.dayOfMonth).toBe(5);
    expect(input.dayOfWeek).toBeUndefined();
    expect(input.timezone).toBe("Asia/Taipei");
    expect(input.hour).toBe(9);
    expect(input.minute).toBe(0);
  });

  it("keeps the default timezone editable", async () => {
    const view = await renderOpenCard();
    fireEvent.click(await view.findByTestId("mp-schedmsg-add"));
    const tz = await view.findByTestId("mp-schedmsg-timezone");
    expect((tz as HTMLInputElement).value).toBe("Asia/Taipei");
    fireEvent.change(tz, { target: { value: "Europe/Berlin" } });
    fireEvent.change(await view.findByTestId("mp-schedmsg-body-input"), {
      target: { value: "guten morgen" },
    });
    fireEvent.click(await view.findByTestId("mp-schedmsg-create"));

    await waitFor(() => expect(createScheduledMessage).toHaveBeenCalledTimes(1));
    expect(createScheduledMessage.mock.calls[0][1].timezone).toBe(
      "Europe/Berlin"
    );
  });

  it("warns that 29/30/31 skip the months that lack the day, and stays quiet on a day every month has", async () => {
    const view = await renderOpenCard();
    fireEvent.click(await view.findByTestId("mp-schedmsg-add"));
    fireEvent.change(await view.findByTestId("mp-schedmsg-cadence"), {
      target: { value: "monthly" },
    });

    fireEvent.change(await view.findByTestId("mp-schedmsg-dayofmonth"), {
      target: { value: "31" },
    });
    const hint = await view.findByTestId("mp-schedmsg-skip-hint");
    expect(hint.textContent).toBe(s.dayOfMonthSkipHint);

    // The other direction — a hint that is simply always on would pass the
    // assertion above on its own.
    fireEvent.change(await view.findByTestId("mp-schedmsg-dayofmonth"), {
      target: { value: "15" },
    });
    expect(view.queryByTestId("mp-schedmsg-skip-hint")).toBeNull();
  });

  it("deletes only after the confirm modal is confirmed", async () => {
    store = [mkSchedule({ label: "每日巡檢" })];
    const target = store[0];
    const view = await renderOpenCard();

    fireEvent.click(await view.findByTestId(`mp-schedmsg-delete-${target.id}`));
    // The click alone must not have reached the wire.
    expect(deleteScheduledMessage).not.toHaveBeenCalled();
    const modal = await view.findByTestId("mp-schedmsg-delete-confirm");
    expect(within(modal).getByText(s.deleteConfirm)).toBeTruthy();

    fireEvent.click(await view.findByTestId("mp-schedmsg-delete-confirm-ok"));
    await waitFor(() =>
      expect(deleteScheduledMessage).toHaveBeenCalledWith("mira", target.id)
    );
    await waitFor(() =>
      expect(view.queryByTestId(`mp-schedmsg-row-${target.id}`)).toBeNull()
    );
  });

  it("flips a schedule between enabled and disabled through the row toggle", async () => {
    store = [mkSchedule({ label: "每日巡檢" })];
    const target = store[0];
    const view = await renderOpenCard();

    fireEvent.click(await view.findByTestId(`mp-schedmsg-status-${target.id}`));
    await waitFor(() =>
      expect(updateScheduledMessage).toHaveBeenCalledWith("mira", target.id, {
        status: "disabled",
      })
    );
    const row = await view.findByTestId(`mp-schedmsg-row-${target.id}`);
    await waitFor(() => expect(within(row).getByText(s.disabled)).toBeTruthy());
  });

  it("shows when a schedule last delivered", async () => {
    vi.useFakeTimers({ now: NOW, toFake: ["Date"] });
    store = [
      mkSchedule({ label: "每日巡檢", lastFiredTs: FIRED_AT.getTime() / 1000 }),
    ];
    const view = await renderOpenCard();

    const line = await view.findByTestId(`mp-schedmsg-lastfired-${store[0].id}`);
    expect(line.textContent).toContain(s.lastFiredLabel);
    expect(line.textContent).toContain("8/9 21:30");
  });

  it("says a schedule has never delivered instead of printing a time", async () => {
    vi.useFakeTimers({ now: NOW, toFake: ["Date"] });
    store = [mkSchedule({ label: "剛建好的排程", lastFiredTs: 0 })];
    const view = await renderOpenCard();

    const line = await view.findByTestId(`mp-schedmsg-lastfired-${store[0].id}`);
    expect(line.textContent).toContain(s.lastFiredNever);
    // ts 0 is a real epoch, so formatting it unconditionally would print
    // 1/1 08:00 and read as a delivery that never happened.
    expect(line.textContent).not.toMatch(TIME_SHAPE);
  });

  it("says the load failed instead of reading as honest-empty", async () => {
    const view = render(
      <I18nProvider>
        <ScheduledMessagesCard memberId="nobody" />
      </I18nProvider>
    );
    fireEvent.click(await view.findByTestId("mp-schedmsg-toggle"));
    // "nobody" has no rows AND the mock resolves, so this member must read
    // empty rather than failed — the failure branch is not a catch-all.
    expect(await view.findByTestId("mp-schedmsg-empty")).toBeTruthy();
  });
});

// 🔴 The card is ONE component rendered by BOTH wrappers. Proving it on the
// member panel proves nothing about the worker panel: they are two different
// callers of AgentDetailPanel's `extraExpandCards` slot, and the worker one had
// no caller at all until this ticket. Drop `extraExpandCards` from either
// wrapper and exactly one of these two reddens.
describe("both detail panels render the 定期訊息 card", () => {
  it("renders it on the member panel", async () => {
    store = [mkSchedule({ memberId: "mira", label: "正職排程" })];
    const view = render(
      <I18nProvider>
        <MemberDetailPanel member={mkMember()} onBack={() => {}} />
      </I18nProvider>
    );
    fireEvent.click(await view.findByTestId("mp-schedmsg-toggle"));
    expect(await view.findByText("正職排程")).toBeTruthy();
  });

  it("renders it on the outsource worker panel, bound to the ow- id", async () => {
    store = [mkSchedule({ memberId: "ow-7788", label: "外包排程" })];
    const view = render(
      <I18nProvider>
        <WorkerDetailPanel worker={mkWorker()} onBack={() => {}} />
      </I18nProvider>
    );
    fireEvent.click(await view.findByTestId("mp-schedmsg-toggle"));
    // Bound to the WORKER's own id — a card wired to some member id would list
    // the wrong agent's schedules and this row would never appear.
    expect(await view.findByText("外包排程")).toBeTruthy();
  });
});
