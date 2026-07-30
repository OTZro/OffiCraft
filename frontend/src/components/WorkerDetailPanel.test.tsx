// The outsource-worker detail panel. Since T-f190 it MIRRORS the member detail
// panel: the SAME machine / Claude account / context% / est.$ / 最近操作 cards,
// fed by the worker DTO's runtime fold, PLUS the worker-specific bits — the
// anonymous codename, the ONE delegated task (clickable → #tasks/<id>) with its
// REAL delegator, the owner 改機器 operation, and the worker-<id> tmux command.
//
// Rendered through OfficePage so the hash → resolve → panel chain is the REAL
// wiring, not a stub. Runtime facts and the four honest spawn states are driven
// by fixtures injected into the mock adapter (the same wire→view mapper the http
// adapter uses).
//
// jsdom scope note: these assertions are text/DOM presence + the real
// mock-adapter relocate round-trip. Pure visual styling (the stuck warn tint,
// the picker's dark theme) is NOT asserted here — jsdom does not compute it.

import { describe, it, expect, afterEach, beforeEach, vi } from "vitest";
import { render, fireEvent, waitFor } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { api } from "../api";
import { zh } from "../i18n/locales/zh";
import { ApiError } from "../api/errors";
import { OfficePage } from "./OfficePage";
import {
  __resetMock,
  __injectMockTask,
  __injectMockOutsourceWorker,
  __injectMockTaskType,
  __setMockMemberOnline,
} from "../api/mock";
import type { TaskView, OutsourceWorkerView } from "../api/adapter";

let seq = 0;

function mkTask(over: Partial<TaskView>): TaskView {
  seq += 1;
  return {
    id: `task-${seq}`,
    taskNo: `T-${1000 + seq}`,
    title: `任務 ${seq}`,
    typeKey: "",
    description: "",
    status: "in_progress",
    priority: "mid",
    executorKind: "outsource",
    executorId: `ow-${seq}`,
    creatorId: "",
    dedupeKey: "",
    deps: [],
    waitingReason: "",
    duplicateOf: "",
    createdTs: Date.now() / 1000 - 3600,
    updatedTs: Date.now() / 1000 - 60,
    closedTs: null,
    progressDone: 0,
    progressTotal: 0,
    steps: [],
    ...over,
  };
}

function mkWorker(over: Partial<OutsourceWorkerView>): OutsourceWorkerView {
  seq += 1;
  return {
    id: `ow-${seq}`,
    codename: `O-${seq}`,
    model: "Opus 4.6",
    effort: "high",
    status: "active",
    taskId: `task-${seq}`,
    taskTitle: "",
    taskStatus: "in_progress",
    createdTs: Date.now() / 1000 - 600,
    // T-f190 runtime fold — honest defaults (nothing reported): the mapper's
    // null/"" shape. Individual tests override the fields under test.
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

function renderOfficeAt(hash: string) {
  window.location.hash = hash;
  return render(
    <I18nProvider>
      <OfficePage />
    </I18nProvider>,
  );
}

beforeEach(() => {
  __resetMock();
  window.location.hash = "";
  Element.prototype.scrollIntoView = vi.fn();
});

// Restore adapter spies even when the assertion that follows them throws — a
// per-test mockRestore() is skipped on failure and leaks a mocked rejection
// into the next test, turning one red into two and hiding which one is real.
afterEach(() => {
  vi.restoreAllMocks();
});

describe("WorkerDetailPanel — aligned real info (T-f190 item 1)", () => {
  it("shows the worker's REAL machine / account / context% / est.$ when reported", async () => {
    __injectMockTask(mkTask({ id: "t-1", taskNo: "T-9c21", title: "查帳單" }));
    __injectMockOutsourceWorker(
      mkWorker({
        id: "ow-1",
        codename: "O-7",
        taskId: "t-1",
        taskTitle: "查帳單",
        presence: "online",
        machine: "Warden · mbp5",
        account: "team-a@corp",
        contextPct: 42,
        cost: 3.5,
      }),
    );

    const { findByTestId, container } = renderOfficeAt("#office/worker/ow-1");
    await findByTestId("worker-detail-task");
    const text = container.textContent ?? "";
    expect(text).toContain("O-7");
    expect(text).toContain("Opus 4.6");
    // The aligned member-parity fields are now PRESENT (reversing the old
    // lean-panel design where they were intentionally absent).
    expect(text).toContain("Claude Account");
    expect((await findByTestId("worker-detail-machine")).textContent).toBe(
      "Warden · mbp5",
    );
    expect(text).toContain("team-a@corp");
    expect((await findByTestId("worker-detail-context")).textContent).toBe(
      "42%",
    );
    expect((await findByTestId("worker-detail-cost")).textContent).toBe("$4");
  });

  it("shows honest dashes / 尚未分配, never fabricated values, when nothing reported", async () => {
    __injectMockTask(mkTask({ id: "t-1" }));
    __injectMockOutsourceWorker(
      mkWorker({ id: "ow-1", taskId: "t-1" }), // all runtime fields at honest empty
    );

    const { findByTestId } = renderOfficeAt("#office/worker/ow-1");
    expect((await findByTestId("worker-detail-machine")).textContent).toBe(
      "尚未分配",
    );
    expect((await findByTestId("worker-detail-context")).textContent).toBe("—");
    expect((await findByTestId("worker-detail-cost")).textContent).toBe("—");
  });
});

describe("WorkerDetailPanel — honest presence states (A案 P6 member vocabulary)", () => {
  async function statusTextFor(over: Partial<OutsourceWorkerView>) {
    __injectMockTask(mkTask({ id: "t-1" }));
    __injectMockOutsourceWorker(
      mkWorker({ id: "ow-1", taskId: "t-1", ...over }),
    );
    const { findByTestId } = renderOfficeAt("#office/worker/ow-1");
    return (await findByTestId("worker-detail-status")).textContent ?? "";
  }

  it("未分配機器: machine cell shows 尚未分配 (presence waking, never dispatched)", async () => {
    __injectMockTask(mkTask({ id: "t-1" }));
    __injectMockOutsourceWorker(
      mkWorker({
        id: "ow-1",
        taskId: "t-1",
        status: "assigned",
        presence: "waking",
        machine: "",
      }),
    );
    const { findByTestId } = renderOfficeAt("#office/worker/ow-1");
    expect((await findByTestId("worker-detail-machine")).textContent).toBe(
      "尚未分配",
    );
    expect((await findByTestId("worker-detail-status")).textContent).toBe(
      "啟動中",
    );
  });

  it("離線: presence offline renders 離線 + the structured reason", async () => {
    __injectMockTask(mkTask({ id: "t-1" }));
    __injectMockOutsourceWorker(
      mkWorker({
        id: "ow-1",
        taskId: "t-1",
        status: "assigned",
        presence: "offline",
        machine: "Warden · mbp5",
        lastOpReason: "spawn_timeout: no active flip in 270s",
      }),
    );
    const { findByTestId } = renderOfficeAt("#office/worker/ow-1");
    expect((await findByTestId("worker-detail-status")).textContent).toBe(
      "離線",
    );
    expect(
      (await findByTestId("worker-detail-stuck-reason")).textContent,
    ).toContain("spawn_timeout");
  });

  it("運行中: presence online renders 工作中", async () => {
    expect(
      await statusTextFor({ status: "active", presence: "online" }),
    ).toBe("工作中");
  });

  it("released (no presence): falls back to the lifecycle status label", async () => {
    expect(
      await statusTextFor({ status: "released", presence: undefined }),
    ).toBe(
      "已釋放",
    );
  });
});

describe("WorkerDetailPanel — real delegator (T-f190 item 2)", () => {
  it("shows the RESOLVED member name when the creator is a member", async () => {
    __injectMockTask(mkTask({ id: "t-1", creatorId: "m-xiao" }));
    __injectMockOutsourceWorker(
      mkWorker({
        id: "ow-1",
        taskId: "t-1",
        creatorId: "m-xiao",
        delegatedBy: "小明",
      }),
    );
    const { findByTestId } = renderOfficeAt("#office/worker/ow-1");
    expect((await findByTestId("worker-detail-delegator")).textContent).toBe(
      "小明",
    );
  });

  it("shows the owner label when the owner created the task", async () => {
    __injectMockTask(mkTask({ id: "t-1", creatorId: "owner" }));
    __injectMockOutsourceWorker(
      mkWorker({
        id: "ow-1",
        taskId: "t-1",
        creatorId: "owner",
        delegatedBy: "",
      }),
    );
    const { findByTestId } = renderOfficeAt("#office/worker/ow-1");
    expect((await findByTestId("worker-detail-delegator")).textContent).toBe(
      "系統 Owner",
    );
  });

  it("falls back to 系統排程 (not a fabricated name) for a blank creator", async () => {
    __injectMockTask(mkTask({ id: "t-1", creatorId: "" }));
    __injectMockOutsourceWorker(
      mkWorker({ id: "ow-1", taskId: "t-1", creatorId: "", delegatedBy: "" }),
    );
    const { findByTestId } = renderOfficeAt("#office/worker/ow-1");
    expect((await findByTestId("worker-detail-delegator")).textContent).toBe(
      "系統排程",
    );
  });
});

// ── T-7526: the panel is READ-ONLY and every setting goes through the 更改
// dialog (the member panel's shape since T-927a). These replace the old 改機器
// in-place-button suite: that control no longer exists, so its assertions are
// not merely red, they are unrepresentable.
describe("WorkerDetailPanel — 設定改走喚醒區 (T-7526 parity)", () => {
  it("renders the 模型 and 機器 cells with NO in-place editor on either", async () => {
    __setMockMemberOnline("warden-mbp5", true);
    __injectMockTask(mkTask({ id: "t-1" }));
    __injectMockOutsourceWorker(
      mkWorker({ id: "ow-1", taskId: "t-1", model: "Opus 4.6" }),
    );
    const { findByTestId, queryByTestId } = renderOfficeAt("#office/worker/ow-1");
    // Positive control FIRST: both cells really are on screen holding real
    // values. Without it "no edit button" would also pass on a panel that
    // failed to render the cells at all.
    const cell = await findByTestId("worker-detail-model-effort-cell");
    expect(cell.textContent).toContain("Opus 4.6");
    expect(await findByTestId("worker-detail-machine")).toBeTruthy();
    // …and the settings entry that replaced them is live.
    await findByTestId("worker-detail-change");
    // The two in-place editors are gone.
    expect(queryByTestId("worker-detail-model-effort-edit")).toBeNull();
    expect(queryByTestId("worker-detail-relocate")).toBeNull();
  });

  it("更改 → changing the machine REACHES relocateWorker and the 機器 cell adopts it", async () => {
    __setMockMemberOnline("warden-mbp5", true);
    __injectMockTask(mkTask({ id: "t-1" }));
    __injectMockOutsourceWorker(
      mkWorker({ id: "ow-1", taskId: "t-1", machine: "", desiredMachineId: "" }),
    );
    const relocate = vi.spyOn(api, "relocateWorker");
    const { findByTestId } = renderOfficeAt("#office/worker/ow-1");
    fireEvent.click(await findByTestId("worker-detail-change"));
    const select = (await findByTestId(
      "worker-detail-settings-machine",
    )) as HTMLSelectElement;
    await waitFor(() =>
      expect(
        Array.from(select.options).map((o) => o.value),
      ).toContain("warden-mbp5"),
    );
    fireEvent.change(select, { target: { value: "warden-mbp5" } });
    fireEvent.click(await findByTestId("worker-detail-settings-confirm"));
    // FIRED, not merely rendered: the adapter saw the call with the chosen id…
    await waitFor(() =>
      expect(relocate).toHaveBeenCalledWith("ow-1", "warden-mbp5"),
    );
    // …and the round-trip lands on the cell.
    await waitFor(async () =>
      expect((await findByTestId("worker-detail-machine")).textContent).toBe(
        "Warden · mbp5",
      ),
    );
  });

  it("a rejected submit keeps the dialog open and shows the server's own message", async () => {
    __injectMockTask(mkTask({ id: "t-1" }));
    __injectMockOutsourceWorker(
      mkWorker({ id: "ow-1", taskId: "t-1", model: "Opus 4.6" }),
    );
    vi.spyOn(api, "setWorkerModel").mockRejectedValue(
      new ApiError(
        "http 409 for POST /api/outsource-workers/ow-1/model",
        409,
        "conflict",
        "這個外包已經被釋放了",
      ),
    );
    const { findByTestId } = renderOfficeAt("#office/worker/ow-1");
    fireEvent.click(await findByTestId("worker-detail-change"));
    const input = (await findByTestId("me-model-input")) as HTMLInputElement;
    fireEvent.change(input, { target: { value: "claude-opus-4-8" } });
    fireEvent.click(await findByTestId("worker-detail-settings-confirm"));
    expect((await findByTestId("worker-detail-settings-error")).textContent).toBe(
      "這個外包已經被釋放了",
    );
    // The dialog stays up so the owner can retry — a closed dialog would read
    // as a save that worked.
    await findByTestId("worker-detail-settings-dialog");
  });

  it("saves the launch settings BEFORE relocating, so the respawn uses the new model", async () => {
    __setMockMemberOnline("warden-mbp5", true);
    __injectMockTask(mkTask({ id: "t-1" }));
    __injectMockOutsourceWorker(
      mkWorker({ id: "ow-1", taskId: "t-1", model: "Opus 4.6", desiredMachineId: "" }),
    );
    // A relocate kills the session and re-dispatches on the new machine, so a
    // relocate that goes first spawns on the OLD model and the owner's edit only
    // lands one respawn later.
    const order: string[] = [];
    vi.spyOn(api, "setWorkerModel").mockImplementation(async (id) => {
      order.push("model");
      return (await api.getOutsourceWorker(id)) as never;
    });
    vi.spyOn(api, "relocateWorker").mockImplementation(async (id) => {
      order.push("relocate");
      return (await api.getOutsourceWorker(id)) as never;
    });
    const { findByTestId } = renderOfficeAt("#office/worker/ow-1");
    fireEvent.click(await findByTestId("worker-detail-change"));
    const input = (await findByTestId("me-model-input")) as HTMLInputElement;
    fireEvent.change(input, { target: { value: "claude-opus-4-8" } });
    const select = (await findByTestId(
      "worker-detail-settings-machine",
    )) as HTMLSelectElement;
    await waitFor(() =>
      expect(Array.from(select.options).map((o) => o.value)).toContain(
        "warden-mbp5",
      ),
    );
    fireEvent.change(select, { target: { value: "warden-mbp5" } });
    fireEvent.click(await findByTestId("worker-detail-settings-confirm"));
    await waitFor(() => expect(order).toHaveLength(2));
    expect(order).toEqual(["model", "relocate"]);
  });

  it("closes an open dialog when the panel switches to another worker", async () => {
    __injectMockTask(mkTask({ id: "t-1" }));
    __injectMockTask(mkTask({ id: "t-2" }));
    __injectMockOutsourceWorker(
      mkWorker({ id: "ow-1", taskId: "t-1", model: "Opus 4.6" }),
    );
    __injectMockOutsourceWorker(
      mkWorker({ id: "ow-2", taskId: "t-2", model: "Sonnet 4.6" }),
    );
    const { findByTestId, queryByTestId, rerender } =
      renderOfficeAt("#office/worker/ow-1");
    fireEvent.click(await findByTestId("worker-detail-change"));
    await findByTestId("worker-detail-settings-dialog");

    // Neither caller passes a `key`, so this is a PROP change, not a remount: a
    // surviving dialog would still hold ow-1's draft and one confirm would write
    // those values onto ow-2.
    window.location.hash = "#office/worker/ow-2";
    window.dispatchEvent(new HashChangeEvent("hashchange"));
    rerender(
      <I18nProvider>
        <OfficePage />
      </I18nProvider>,
    );
    // Positive control: the panel really did move to ow-2 …
    await waitFor(async () =>
      expect(
        (await findByTestId("worker-detail-model-effort-cell")).textContent,
      ).toContain("Sonnet 4.6"),
    );
    // … and it moved there with the dialog closed.
    expect(queryByTestId("worker-detail-settings-dialog")).toBeNull();
  });
});

describe("WorkerDetailPanel — worker-specific bits carry over", () => {
  it("clicking the delegated task navigates to that task", async () => {
    __injectMockTask(mkTask({ id: "t-1", taskNo: "T-9c21", title: "查帳單" }));
    __injectMockOutsourceWorker(
      mkWorker({ id: "ow-1", taskId: "t-1", taskTitle: "查帳單" }),
    );
    const { findByTestId } = renderOfficeAt("#office/worker/ow-1");
    fireEvent.click(await findByTestId("worker-detail-task"));
    await waitFor(() => expect(window.location.hash).toBe("#tasks/t-1"));
  });

  it("shows the member-<id> tmux attach command (P5b session naming)", async () => {
    __injectMockTask(mkTask({ id: "t-1" }));
    __injectMockOutsourceWorker(mkWorker({ id: "ow-1", taskId: "t-1" }));
    const { findByTestId } = renderOfficeAt("#office/worker/ow-1");
    const copy = await findByTestId("worker-detail-copy");
    const cmd = copy
      .closest(".mp-terminal__row")
      ?.querySelector(".mp-terminal__cmd");
    expect(cmd?.textContent).toContain("member-ow-1");
  });
});

// ── T-32e1/T-f190 lifecycle ops (換手 / 停止・重啟 / 換 model) ──────────────────
// Rendered through OfficePage → the real mock-adapter round-trip (the mock models
// the server's observable outcome). jsdom scope: DOM presence + state transitions
// are asserted; pure styling (the refocus pulse tint) is NOT (jsdom does not
// compute it) — an honest "測不到" gap, not a decorative assertion.
describe("WorkerDetailPanel — header matches the sidebar 外包 row (T-f190 UI spec)", () => {
  it("header shows codename + clickable task chip + a SHORT task-type label (same as the roster row, T-b0e3) + a real (online) dot", async () => {
    // The detail page's `worker` comes from the SAME useOutsourceWorkers() join
    // the roster reads (OfficePage.tsx) — taskTypeName/Key are derived there
    // from the task's typeKey + the registered manual, overwriting anything set
    // directly on the injected worker fixture. So exercising the real label
    // means registering the manual, not stubbing taskTypeName on the worker.
    __injectMockTaskType({
      typeKey: "tm-officraft-dev",
      displayName: "OffiCraft 開發",
      purpose: "",
    });
    __injectMockTask(
      mkTask({
        id: "t-1",
        taskNo: "T-e9f4",
        title: "Planning for big change",
        typeKey: "tm-officraft-dev",
      }),
    );
    __injectMockOutsourceWorker(
      mkWorker({
        id: "ow-1",
        codename: "O-19",
        taskId: "t-1",
        taskNo: "T-e9f4",
        taskTitle: "Planning for big change",
        presence: "online",
      }),
    );
    const { findByTestId, findByText } = renderOfficeAt("#office/worker/ow-1");
    const header = await findByTestId("worker-detail-header-task");
    // The codename line shows the outsource identity label 「外包 · 代號」,
    // matching the sidebar 外包 row (T-3ed8, owner 2026-07-20 完全一致).
    await findByText("外包 · O-19");
    expect((await findByTestId("worker-detail-header-chip")).textContent).toBe("T-e9f4");
    // T-b0e3: the slot that used to hold the FULL task title now renders the
    // SAME short type label the roster row shows (taskTypeName), never the
    // full title/description sentence.
    expect(header.textContent).toContain("OffiCraft 開發");
    expect(header.textContent).not.toContain("Planning for big change");
    // The old raw ow-id chip is gone (the header no longer renders worker.id).
    expect(header.textContent).not.toContain("ow-1");
    // Real presence: online → the shared lifecycle dot's ONLINE class (the
    // colour comes from --color-dot-online, never an inline literal).
    const dot = await findByTestId("worker-detail-header-dot");
    expect(dot.className).toBe("lifecycle-dot lifecycle-dot--online-awake");
    expect(dot.getAttribute("style")).toBeNull();
    // Clicking the chip routes to the bound task.
    fireEvent.click(await findByTestId("worker-detail-header-chip"));
    await waitFor(() => expect(window.location.hash).toBe("#tasks/t-1"));
  });

  it("header falls back to 自由代辦 when the task has no type (adhoc, blank typeKey)", async () => {
    __injectMockTask(
      mkTask({ id: "t-1", taskNo: "T-adhc", title: "隨手需求", typeKey: "" }),
    );
    __injectMockOutsourceWorker(
      mkWorker({
        id: "ow-1",
        taskId: "t-1",
        taskNo: "T-adhc",
        taskTitle: "隨手需求",
        presence: "online",
      }),
    );
    const { findByTestId } = renderOfficeAt("#office/worker/ow-1");
    const header = await findByTestId("worker-detail-header-task");
    expect(header.textContent).toContain("自由代辦");
    expect(header.textContent).not.toContain("隨手需求");
  });

  // T-59d6: the header dot is the SAME shared LifecycleDot the rail row and the
  // 正職 roster render — so every non-online state gets its OWN colour class
  // (not one shared grey) and its own label. Asserting the exact class is what
  // makes a mutant that repaints any of these green go RED here too.
  it.each([
    ["online", "online-awake"],
    ["waking", "waking"],
    ["stopping", "stopping"],
    ["stopped", "stopped"],
    ["offline", "offline"],
    [undefined, "offline"],
  ] as ReadonlyArray<[OutsourceWorkerView["presence"], string]>)(
    "header dot for presence %s is the %s lifecycle dot",
    async (presence, visual) => {
      __injectMockTask(mkTask({ id: "t-1" }));
      __injectMockOutsourceWorker(
        mkWorker({ id: "ow-1", taskId: "t-1", presence }),
      );
      const { findByTestId } = renderOfficeAt("#office/worker/ow-1");
      const dot = await findByTestId("worker-detail-header-dot");
      expect(dot.className).toBe(`lifecycle-dot lifecycle-dot--${visual}`);
      expect(dot.getAttribute("style")).toBeNull();
      if (presence !== "online") {
        expect(dot.className).not.toContain("online-awake");
      }
    },
  );
});

describe("WorkerDetailPanel — lifecycle ops (T-32e1/T-f190)", () => {
  it("refocus is disabled off-line and enabled online", async () => {
    __injectMockTask(mkTask({ id: "t-1" }));
    __injectMockOutsourceWorker(
      mkWorker({ id: "ow-1", taskId: "t-1", presence: "offline" }),
    );
    const { findByTestId, rerender } = renderOfficeAt("#office/worker/ow-1");
    const btn = (await findByTestId("worker-detail-refocus")) as HTMLButtonElement;
    expect(btn.disabled).toBe(true); // offline: online-only gate mirrored client-side

    __resetMock();
    __injectMockTask(mkTask({ id: "t-2" }));
    __injectMockOutsourceWorker(
      mkWorker({ id: "ow-2", taskId: "t-2", presence: "online" }),
    );
    window.location.hash = "#office/worker/ow-2";
    rerender(
      <I18nProvider>
        <OfficePage />
      </I18nProvider>,
    );
    const btn2 = (await findByTestId("worker-detail-refocus")) as HTMLButtonElement;
    expect(btn2.disabled).toBe(false);
  });

  it("refocus round-trips: clicking online surfaces the sent acknowledgement", async () => {
    __injectMockTask(mkTask({ id: "t-1" }));
    __injectMockOutsourceWorker(
      mkWorker({ id: "ow-1", taskId: "t-1", presence: "online" }),
    );
    const { findByTestId } = renderOfficeAt("#office/worker/ow-1");
    fireEvent.click(await findByTestId("worker-detail-refocus"));
    // The mock stamps refocus_since; the panel keeps the persistent "sent" note.
    await findByTestId("worker-detail-refocus-note");
  });

  it("stop → the worker reads 已停止 and the toggle flips to 重啟", async () => {
    __injectMockTask(mkTask({ id: "t-1" }));
    __injectMockOutsourceWorker(
      mkWorker({ id: "ow-1", taskId: "t-1", presence: "online" }),
    );
    const { findByTestId } = renderOfficeAt("#office/worker/ow-1");
    const toggle = await findByTestId("worker-detail-stop-toggle");
    expect(toggle.textContent).toBe("停止");
    fireEvent.click(toggle);
    await waitFor(async () =>
      expect(
        (await findByTestId("worker-detail-status")).textContent,
      ).toBe("已停止"),
    );
    expect((await findByTestId("worker-detail-stop-toggle")).textContent).toBe(
      "重新啟動",
    );
  });

  it("a stopped worker shows 已停止 and restart flips it back to a live state", async () => {
    __injectMockTask(mkTask({ id: "t-1" }));
    __injectMockOutsourceWorker(
      mkWorker({
        id: "ow-1",
        taskId: "t-1",
        presence: "stopped",
        desiredState: "offline",
      }),
    );
    const { findByTestId } = renderOfficeAt("#office/worker/ow-1");
    expect((await findByTestId("worker-detail-status")).textContent).toBe(
      "已停止",
    );
    fireEvent.click(await findByTestId("worker-detail-stop-toggle"));
    // restart → the mock reflects the re-spawn as presence "waking".
    await waitFor(async () =>
      expect(
        (await findByTestId("worker-detail-status")).textContent,
      ).toBe("啟動中"),
    );
  });

  it("a worker whose session died on its own offers 重新啟動, and it revives it", async () => {
    // presence offline + desired_state online = the session died on its own;
    // nobody pressed 停止. The panel used to show 停止 here — a button with
    // nothing to stop — and no way back at all.
    __injectMockTask(mkTask({ id: "t-1" }));
    __injectMockOutsourceWorker(
      mkWorker({
        id: "ow-1",
        taskId: "t-1",
        presence: "offline",
        desiredState: "online",
      }),
    );
    const restart = vi.spyOn(api, "restartWorker");
    const { findByTestId } = renderOfficeAt("#office/worker/ow-1");
    const toggle = await findByTestId("worker-detail-stop-toggle");
    expect(toggle.textContent).toBe("重新啟動");
    expect((toggle as HTMLButtonElement).disabled).toBe(false);

    // PRESSED, not merely rendered: the revive endpoint is actually reached …
    fireEvent.click(toggle);
    await waitFor(() => expect(restart).toHaveBeenCalledWith("ow-1"));
    // … and the panel adopts the revived state.
    await waitFor(async () =>
      expect(
        (await findByTestId("worker-detail-status")).textContent,
      ).toBe("啟動中"),
    );
  });

  it("換 model: 更改 → save persists the new model via the adapter", async () => {
    __injectMockTask(mkTask({ id: "t-1" }));
    __injectMockOutsourceWorker(
      mkWorker({ id: "ow-1", taskId: "t-1", model: "Opus 4.6", presence: "online" }),
    );
    const setModel = vi.spyOn(api, "setWorkerModel");
    const { findByTestId } = renderOfficeAt("#office/worker/ow-1");
    // The ONE settings entry (T-7526) — the model cell itself is read-only.
    fireEvent.click(await findByTestId("worker-detail-change"));
    // The shared ModelEffortEditor's free custom-model input (data-testid pinned).
    const input = (await findByTestId("me-model-input")) as HTMLInputElement;
    fireEvent.change(input, { target: { value: "claude-opus-4-8" } });
    fireEvent.click(await findByTestId("worker-detail-settings-confirm"));
    // The endpoint was actually REACHED with the edited value…
    await waitFor(() =>
      expect(setModel).toHaveBeenCalledWith(
        "ow-1",
        expect.objectContaining({ model: "claude-opus-4-8" }),
      ),
    );
    // …and the cell reflects it after the outsource_worker refetch.
    await waitFor(async () =>
      expect(
        (await findByTestId("worker-detail-model-effort-cell")).textContent,
      ).toContain("claude-opus-4-8"),
    );
  });
});

// ── T-ba6b convergence: the worker now renders through the SHARED
// AgentDetailPanel (same component + view model as the member), so these three
// assert the convergence-specific behaviour: the readable Claude account (with a
// negative control that no raw internal identifier reaches the page), the
// live+banked cost 口徑, and the initial-prompt preview with its honest caveat.
describe("WorkerDetailPanel — Claude Account is readable, raw keys never leak (T-ba6b)", () => {
  it("shows the resolved account name AND never renders a raw credential key / internal id", async () => {
    __injectMockTask(mkTask({ id: "t-1", taskNo: "T-9c21", title: "查帳單" }));
    __injectMockOutsourceWorker(
      mkWorker({
        // A distinctive raw-key-shaped id: if any card fell back to an internal
        // identifier for the account, this string would surface on the page.
        id: "ow-5e163893-a1b2-4c3d-raw-key",
        codename: "O-7",
        taskId: "t-1",
        taskTitle: "查帳單",
        presence: "online",
        account: "shawn-claude", // the server-resolved readable alias
      }),
    );
    const { findByTestId, container } = renderOfficeAt(
      "#office/worker/ow-5e163893-a1b2-4c3d-raw-key",
    );
    // POSITIVE CONTROL: the readable account name is present in its cell.
    expect((await findByTestId("worker-detail-account")).textContent).toBe(
      "shawn-claude",
    );
    // NEGATIVE: the raw internal identifier appears NOWHERE on the page — not in
    // the account cell, not the header, not the tmux command's rendered id chip.
    // (The tmux attach line legitimately contains worker-<id>; scope the raw-key
    // check to everything OUTSIDE the terminal command.)
    const text = container.textContent ?? "";
    expect(text).toContain("shawn-claude");
    const account = await findByTestId("worker-detail-account");
    expect(account.textContent).not.toContain("raw-key");
    const header = await findByTestId("worker-detail-header-task");
    expect(header.textContent ?? "").not.toContain("raw-key");
  });

  it("renders an honest dash (never a raw key) when the account is unresolved", async () => {
    __injectMockTask(mkTask({ id: "t-1" }));
    __injectMockOutsourceWorker(
      // account null = the server could resolve no alias/label; the panel must
      // show a bare dash, NEVER the raw telemetry key the server withheld.
      mkWorker({ id: "ow-1", taskId: "t-1", presence: "online", account: null }),
    );
    const { findByTestId } = renderOfficeAt("#office/worker/ow-1");
    expect((await findByTestId("worker-detail-account")).textContent).toBe("—");
  });
});

describe("WorkerDetailPanel — cost 口徑 = live + banked (T-ba6b, member parity)", () => {
  it("sums the live session cost and the banked historical cost", async () => {
    __injectMockTask(mkTask({ id: "t-1" }));
    __injectMockOutsourceWorker(
      mkWorker({
        id: "ow-1",
        taskId: "t-1",
        presence: "online",
        cost: 2, // current live session
        bankedCost: 5, // banked across prior kill+respawn handovers
      }),
    );
    const { findByTestId } = renderOfficeAt("#office/worker/ow-1");
    // 2 + 5 = 7 → formatCost → "$7" (NOT "$2": a converged panel must not drop
    // the banked spend — the pre-convergence worker panel showed live only).
    expect((await findByTestId("worker-detail-cost")).textContent).toBe("$7");
  });

  it("shows banked-only cost when there is no live session (handed-over worker)", async () => {
    __injectMockTask(mkTask({ id: "t-1" }));
    __injectMockOutsourceWorker(
      mkWorker({
        id: "ow-1",
        taskId: "t-1",
        presence: "waking",
        cost: null, // no live session yet
        bankedCost: 4,
      }),
    );
    const { findByTestId } = renderOfficeAt("#office/worker/ow-1");
    expect((await findByTestId("worker-detail-cost")).textContent).toBe("$4");
  });
});

describe("WorkerDetailPanel — initial-prompt preview (T-ba6b)", () => {
  it("expands to the boot-context preview and carries the honest re-assembly caveat", async () => {
    __injectMockTask(
      mkTask({ id: "t-1", taskNo: "T-9c21", title: "查帳單對帳" }),
    );
    __injectMockOutsourceWorker(
      mkWorker({
        id: "ow-1",
        codename: "O-42",
        taskId: "t-1",
        taskTitle: "查帳單對帳",
        presence: "online",
      }),
    );
    const { findByTestId } = renderOfficeAt("#office/worker/ow-1");
    // Lazy-fetched on first expand (the mock re-assembles from current rows).
    fireEvent.click(await findByTestId("worker-detail-prompt-toggle"));
    const body = await findByTestId("worker-detail-prompt-body");
    await waitFor(() => expect(body.textContent ?? "").toContain("O-42"));
    expect(body.textContent ?? "").toContain("查帳單對帳");
    // The honesty caveat is present (目前版本重組, 非派工當下逐字版).
    const note = await findByTestId("worker-detail-prompt-note");
    expect(note.textContent ?? "").toContain("非派工當下");
  });

  // T-7526: the shared card's load lifecycle. `vm.prompt.fetch` is an inline
  // arrow OfficePage rebuilds on every render, so a repaint mid-read used to
  // cancel the read AND leave a "loaded" stamp behind — the card sat on
  // 「載入中…」 for good. The member half of this proof lives in
  // MemberDetailPanel.initial-prompt.test.tsx.
  it("still shows the prompt when the panel repaints while the read is in flight", async () => {
    __injectMockTask(mkTask({ id: "t-1" }));
    __injectMockOutsourceWorker(mkWorker({ id: "ow-1", taskId: "t-1" }));
    let land: (v: string) => void = () => {};
    const boot = vi
      .spyOn(api, "getWorkerBootContext")
      .mockImplementation(
        () => new Promise<string>((resolve) => (land = resolve)),
      );

    const { findByTestId, rerender } = renderOfficeAt("#office/worker/ow-1");
    fireEvent.click(await findByTestId("worker-detail-prompt-toggle"));
    // Positive control: the read really is under way, so the repaint below has
    // something to interrupt.
    expect(
      (await findByTestId("worker-detail-prompt-body")).textContent,
    ).toContain(zh.mp.promptLoading);

    // A repaint — an ordinary SSE delta is enough in the running app.
    rerender(
      <I18nProvider>
        <OfficePage />
      </I18nProvider>,
    );
    land("外包啟動指示");

    await waitFor(async () =>
      expect(
        (await findByTestId("worker-detail-prompt-body")).textContent,
      ).toContain("外包啟動指示"),
    );
    // A repaint is not a reason to re-read either — the ONE read that was
    // already under way is the one that lands.
    expect(boot).toHaveBeenCalledTimes(1);
  });

  it("a failed read shows the error with a retry that actually re-reads", async () => {
    __injectMockTask(mkTask({ id: "t-1" }));
    __injectMockOutsourceWorker(mkWorker({ id: "ow-1", taskId: "t-1" }));
    const boot = vi
      .spyOn(api, "getWorkerBootContext")
      .mockRejectedValueOnce(new Error("boom"))
      .mockResolvedValueOnce("外包啟動指示");

    const { findByTestId } = renderOfficeAt("#office/worker/ow-1");
    fireEvent.click(await findByTestId("worker-detail-prompt-toggle"));
    const err = await findByTestId("worker-detail-prompt-error");
    expect(err.textContent).toContain(zh.mp.promptError);

    fireEvent.click(await findByTestId("worker-detail-prompt-retry"));
    await waitFor(async () =>
      expect(
        (await findByTestId("worker-detail-prompt-body")).textContent,
      ).toContain("外包啟動指示"),
    );
    expect(boot).toHaveBeenCalledTimes(2);
  });
});
