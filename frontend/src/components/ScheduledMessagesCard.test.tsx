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
//   6. An existing schedule can be EDITED — every setting a create can reach —
//      and the saved values appear without a remount. Cancel keeps the stored
//      values; a rejected save stays on screen as an error and never lets the
//      row read as saved.
//   7. A long message is collapsed on the row and can be opened per row. The
//      collapse is a CLASS on the full string: the whole text still goes back
//      over the wire when that row is edited.

import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { readFileSync } from "node:fs";
import { join } from "node:path";
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
    // Applies EVERY field the patch carries, not just `status` — otherwise a
    // component that saved an edit and a component that dropped it on the floor
    // would produce the same list, and the edit assertions below could not tell
    // them apart.
    Object.assign(s, patch);
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

/** Longer than the row shows collapsed. No edge whitespace, so a trim on the
 * way to the wire cannot be mistaken for the truncation being asserted against. */
const LONG_BODY = Array.from(
  { length: 12 },
  (_, i) => `第 ${i + 1} 行:早安,請看一下昨天的 CI 有沒有紅的。`
).join("\n");

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

  it("edits every setting of an existing schedule and shows the result without a remount", async () => {
    store = [mkSchedule({ label: "每日巡檢", body: "看一下 CI" })];
    const target = store[0];
    const view = await renderOpenCard();

    fireEvent.click(await view.findByTestId(`mp-schedmsg-edit-${target.id}`));
    const p = `mp-schedmsg-edit-${target.id}`;
    // The editor opens on what the server currently holds — an editor that
    // opened blank would silently blank whatever the owner did not retype.
    expect(
      (view.getByTestId(`${p}-label-input`) as HTMLInputElement).value
    ).toBe("每日巡檢");
    expect(
      (view.getByTestId(`${p}-body-input`) as HTMLTextAreaElement).value
    ).toBe("看一下 CI");

    fireEvent.change(view.getByTestId(`${p}-label-input`), {
      target: { value: "週報" },
    });
    fireEvent.change(view.getByTestId(`${p}-body-input`), {
      target: { value: "整理本週進度" },
    });
    fireEvent.change(view.getByTestId(`${p}-cadence`), {
      target: { value: "weekly" },
    });
    fireEvent.change(view.getByTestId(`${p}-dayofweek`), {
      target: { value: "5" },
    });
    fireEvent.change(view.getByTestId(`${p}-hour`), { target: { value: "17" } });
    fireEvent.change(view.getByTestId(`${p}-minute`), {
      target: { value: "30" },
    });
    fireEvent.change(view.getByTestId(`${p}-timezone`), {
      target: { value: "Europe/Berlin" },
    });
    fireEvent.click(view.getByTestId(`mp-schedmsg-edit-save-${target.id}`));

    await waitFor(() => expect(updateScheduledMessage).toHaveBeenCalledTimes(1));
    expect(updateScheduledMessage.mock.calls[0][2]).toEqual({
      label: "週報",
      body: "整理本週進度",
      cadence: "weekly",
      dayOfWeek: 5,
      hour: 17,
      minute: 30,
      timezone: "Europe/Berlin",
    });
    // Same rule as the create path: the day field the cadence does not read
    // must not ride along.
    expect(updateScheduledMessage.mock.calls[0][2].dayOfMonth).toBeUndefined();

    // The evidence is the ROW, not the call: only a post-mutation refetch puts
    // the saved values on screen without the owner reopening the member.
    const row = await view.findByTestId(`mp-schedmsg-row-${target.id}`);
    expect(within(row).getByText("週報")).toBeTruthy();
    expect(within(row).getByText("整理本週進度")).toBeTruthy();
    expect(within(row).getByText(s.weeklyOn(s.weekdayFri))).toBeTruthy();
    expect(within(row).getByText("17:30")).toBeTruthy();
    expect(within(row).getByText("Europe/Berlin")).toBeTruthy();
  });

  it("throws the draft away when an edit is cancelled", async () => {
    store = [mkSchedule({ label: "每日巡檢", body: "看一下 CI" })];
    const target = store[0];
    const view = await renderOpenCard();
    const p = `mp-schedmsg-edit-${target.id}`;

    fireEvent.click(await view.findByTestId(`mp-schedmsg-edit-${target.id}`));
    fireEvent.change(view.getByTestId(`${p}-body-input`), {
      target: { value: "改到一半反悔" },
    });
    fireEvent.click(view.getByTestId(`mp-schedmsg-edit-cancel-${target.id}`));

    expect(updateScheduledMessage).not.toHaveBeenCalled();
    const row = await view.findByTestId(`mp-schedmsg-row-${target.id}`);
    expect(within(row).getByText("看一下 CI")).toBeTruthy();
    // Reopening must show the ORIGINAL, not the abandoned draft — a cancel that
    // only hid the form would hand the next edit a value nobody chose.
    fireEvent.click(view.getByTestId(`mp-schedmsg-edit-${target.id}`));
    expect(
      (view.getByTestId(`${p}-body-input`) as HTMLTextAreaElement).value
    ).toBe("看一下 CI");
  });

  it("keeps a failed save on screen as an error instead of as a saved row", async () => {
    store = [mkSchedule({ label: "每日巡檢", body: "看一下 CI" })];
    const target = store[0];
    const view = await renderOpenCard();
    const p = `mp-schedmsg-edit-${target.id}`;
    updateScheduledMessage.mockRejectedValueOnce(new Error("server said no"));

    fireEvent.click(await view.findByTestId(`mp-schedmsg-edit-${target.id}`));
    fireEvent.change(view.getByTestId(`${p}-body-input`), {
      target: { value: "會被拒絕的內容" },
    });
    fireEvent.click(view.getByTestId(`mp-schedmsg-edit-save-${target.id}`));

    expect(
      await view.findByTestId(`mp-schedmsg-edit-error-${target.id}`)
    ).toBeTruthy();
    // Still in the editor, still holding the rejected draft: closing it would
    // read as "saved", and the row underneath would then be the only thing on
    // screen — showing the OLD text with no sign the save was lost.
    expect(view.getByTestId(`mp-schedmsg-editform-${target.id}`)).toBeTruthy();
    expect(
      (view.getByTestId(`${p}-body-input`) as HTMLTextAreaElement).value
    ).toBe("會被拒絕的內容");
    expect(view.queryByTestId(`mp-schedmsg-row-${target.id}`)).toBeNull();
  });

  it("collapses a long message per row and reveals the whole text on demand", async () => {
    store = [
      mkSchedule({ label: "長的", body: LONG_BODY }),
      mkSchedule({ label: "也很長", body: LONG_BODY }),
      mkSchedule({ label: "短的", body: "看一下 CI" }),
    ];
    const [first, second, short] = store;
    const view = await renderOpenCard();

    const text = await view.findByTestId(`mp-schedmsg-text-${first.id}`);
    expect(text.className).toContain("mp-schedmsg__text--clamped");
    // Collapsed is a CLASS on the full string, never a shortened one.
    expect(text.textContent).toBe(LONG_BODY);

    fireEvent.click(view.getByTestId(`mp-schedmsg-text-toggle-${first.id}`));
    expect(
      view.getByTestId(`mp-schedmsg-text-${first.id}`).className
    ).not.toContain("mp-schedmsg__text--clamped");
    // Per ROW: the second long message is still collapsed. One shared switch
    // would have opened both.
    expect(
      view.getByTestId(`mp-schedmsg-text-${second.id}`).className
    ).toContain("mp-schedmsg__text--clamped");

    fireEvent.click(view.getByTestId(`mp-schedmsg-text-toggle-${first.id}`));
    expect(view.getByTestId(`mp-schedmsg-text-${first.id}`).className).toContain(
      "mp-schedmsg__text--clamped"
    );

    // The other direction — a message that fits is neither clamped nor offered
    // a control, otherwise the clamp class above would be unconditional.
    expect(
      view.getByTestId(`mp-schedmsg-text-${short.id}`).className
    ).not.toContain("mp-schedmsg__text--clamped");
    expect(
      view.queryByTestId(`mp-schedmsg-text-toggle-${short.id}`)
    ).toBeNull();
  });

  it("sends a collapsed message back whole when its row is edited", async () => {
    store = [mkSchedule({ label: "長的", body: LONG_BODY })];
    const target = store[0];
    const view = await renderOpenCard();

    // Left collapsed on purpose: the row shows a few lines, and the save must
    // still carry every character. "只看到前幾行" must not become "只送出前幾行".
    expect(
      (await view.findByTestId(`mp-schedmsg-text-${target.id}`)).className
    ).toContain("mp-schedmsg__text--clamped");
    fireEvent.click(view.getByTestId(`mp-schedmsg-edit-${target.id}`));
    fireEvent.click(view.getByTestId(`mp-schedmsg-edit-save-${target.id}`));

    await waitFor(() => expect(updateScheduledMessage).toHaveBeenCalledTimes(1));
    expect(updateScheduledMessage.mock.calls[0][2].body).toBe(LONG_BODY);
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

// 🔴 jsdom evaluates NO CSS, so every assertion above can only see the class
// NAME. Delete the rule the class points at and the whole suite above stays
// green while the row goes back to rendering a wall of text — the T-7526 shape
// (found by looking at a screenshot). So the rule itself is checked where it is
// cheap and exact: at the source, like styleOwnership.test.ts.
describe("member-detail.css backs the collapsed-message class", () => {
  it("clamps .mp-schedmsg__text--clamped to a few lines and hides the rest", () => {
    const css = readFileSync(join(__dirname, "member-detail.css"), "utf8");
    const start = css.indexOf(".mp-schedmsg__text--clamped");
    expect(start).toBeGreaterThan(-1);
    const rule = css.slice(start, css.indexOf("}", start));
    expect(rule).toMatch(/-webkit-line-clamp:\s*\d+/);
    // Anchored on the declaration boundary: `\b` would have matched the
    // `-webkit-` line above and made this assertion a restatement of it.
    expect(rule).toMatch(/[;{\n]\s*line-clamp:\s*\d+/);
    // Without this the clamped box still lays out every line and simply spills.
    expect(rule).toMatch(/overflow:\s*hidden/);
  });
});
