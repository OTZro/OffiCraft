// useRelocateMachine — the 改機器 control's PROGRESS state.
//
// A relocate lands asynchronously: the POST answering 200 only writes the pin;
// the agent actually arriving on the new machine comes back later as an SSE
// delta (which the caller feeds in as `currentMachineId`). The control therefore
// has to hold a visible 「更換中…」 across that gap — owner: 「一直狀態沒變，按鈕
// 沒變，會讓使用者誤以為沒有按到」 — and needs a ceiling on it
// (RELOCATE_TIMEOUT_MS) so a move that never lands ends in an honest, retryable
// timeout instead of spinning forever.

import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { render, fireEvent, act } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { zh } from "../i18n/locales/zh";
import {
  useRelocateMachine,
  RELOCATE_TIMEOUT_MS,
  RELOCATE_SENT_NOTICE_MS,
} from "./useRelocateMachine";
import type { MachineView, MemberRelocateResult } from "../types";

const machine = (id: string, online = true): MachineView => ({
  machineId: id,
  displayName: id.toUpperCase(),
  online,
  isSelf: false,
  binStatus: null,
  claudeVersion: null,
  claudeCredSource: null,
  claudeSubReadable: null,
});

const TWO_ONLINE = [machine("mach-a"), machine("mach-b")];

/** The contract the tests advance the clock against, written out INDEPENDENTLY
 * of the constant under test — a test that advanced by RELOCATE_TIMEOUT_MS
 * itself would follow the constant wherever it moved and could never catch a
 * change to it. */
const TIMEOUT_MS = 30_000;
/** Same discipline for the one-shot chip's window. */
const SENT_MS = 2_000;

interface HarnessProps {
  subjectId?: string;
  machines?: MachineView[];
  boundMachineId?: string | null;
  currentMachineId?: string | null;
  onRelocate?: (machineId: string) => void | Promise<MemberRelocateResult | void>;
  dispatchReceipt?: {
    at?: number | null;
    ok?: boolean | null;
    reason?: string;
  } | null;
}

function Harness({
  subjectId = "mira",
  machines = TWO_ONLINE,
  boundMachineId = "mach-a",
  currentMachineId = null,
  onRelocate,
  dispatchReceipt = null,
}: HarnessProps) {
  const { relocateAction, relocatePicker, relocateUndispatched } =
    useRelocateMachine({
      subjectId,
      machines,
      boundMachineId,
      currentMachineId,
      onRelocate,
      dispatchReceipt,
      testId: "rl",
      pickerTitle: zh.machine.picker.relocateTitle,
      pickerConfirmLabel: zh.machine.picker.relocateConfirm,
      noOnlineTitle: zh.machine.noOnlineMachine,
    });
  return (
    <div>
      {relocateAction}
      {relocatePicker}
      {relocateUndispatched && <div data-testid="undispatched" />}
    </div>
  );
}

function mount(props: HarnessProps = {}) {
  const utils = render(
    <I18nProvider>
      <Harness {...props} />
    </I18nProvider>,
  );
  const rerender = (next: HarnessProps) =>
    utils.rerender(
      <I18nProvider>
        <Harness {...props} {...next} />
      </I18nProvider>,
    );
  const button = () => utils.getByTestId("rl") as HTMLButtonElement;
  const notice = () => utils.queryByTestId("rl-notice");
  const reason = () => utils.queryByTestId("rl-reason");
  const sent = () => utils.queryByTestId("rl-sent");
  return { ...utils, rerender, button, notice, reason, sent };
}

/** A relocate whose promise the test resolves/rejects by hand — the real gap
 * between "fired" and "landed". */
function deferredRelocate() {
  let settle!: (r?: MemberRelocateResult) => void;
  let fail!: (e: unknown) => void;
  const calls: string[] = [];
  const onRelocate = vi.fn((machineId: string) => {
    calls.push(machineId);
    return new Promise<MemberRelocateResult | void>((res, rej) => {
      settle = res as (r?: MemberRelocateResult) => void;
      fail = rej;
    });
  });
  return {
    onRelocate,
    calls,
    resolve: (r?: MemberRelocateResult) => settle(r),
    reject: (e: unknown) => fail(e),
  };
}

beforeEach(() => {
  vi.useFakeTimers({ shouldAdvanceTime: true });
});
afterEach(() => {
  vi.useRealTimers();
});

describe("useRelocateMachine", () => {
  it("gives the move 30 seconds before calling it a timeout", () => {
    expect(RELOCATE_TIMEOUT_MS).toBe(TIMEOUT_MS);
  });

  it("shows 更換中 on the button as soon as the relocate is fired", async () => {
    const { onRelocate } = deferredRelocate();
    const { button } = mount({ onRelocate });

    fireEvent.click(button());
    fireEvent.change(
      document.querySelector<HTMLSelectElement>(
        "[data-testid=machine-picker-select]",
      )!,
      { target: { value: "mach-b" } },
    );
    fireEvent.click(document.querySelector("[data-testid=machine-picker-confirm]")!);

    expect(onRelocate).toHaveBeenCalledWith("mach-b");
    // Visible, not merely disabled: the label itself changes.
    expect(button().textContent).toContain(zh.machine.relocating);
    expect(button().disabled).toBe(true);
  });

  it("keeps 更換中 up after the relocate promise resolves (it ends on landing, not on the 200)", async () => {
    const { onRelocate, resolve } = deferredRelocate();
    const { button } = mount({ onRelocate });

    fireEvent.click(button()); // 2+ online → picker
    fireEvent.click(document.querySelector("[data-testid=machine-picker-confirm]")!);
    await act(async () => {
      resolve();
    });

    expect(button().textContent).toContain(zh.machine.relocating);
    expect(button().disabled).toBe(true);
  });

  it("does not fire a second relocate while 更換中", async () => {
    const { onRelocate, resolve } = deferredRelocate();
    const { button } = mount({ onRelocate, machines: [machine("mach-a")] });

    // ONE online machine → the click relocates straight to it, no picker.
    fireEvent.click(button());
    await act(async () => {
      resolve();
    });

    fireEvent.click(button());
    fireEvent.click(button());
    expect(onRelocate).toHaveBeenCalledTimes(1);
    // …and the picker cannot be reopened either.
    expect(document.querySelector("[data-testid=machine-picker]")).toBeNull();
  });

  it("ends 更換中 when the move lands", async () => {
    const { onRelocate, resolve } = deferredRelocate();
    const { button, rerender } = mount({ onRelocate, boundMachineId: "mach-b" });

    fireEvent.click(button());
    fireEvent.click(document.querySelector("[data-testid=machine-picker-confirm]")!);
    await act(async () => {
      resolve();
    });
    expect(button().textContent).toContain(zh.machine.relocating);

    // The SSE delta arrives: the agent is now OBSERVED on the pinned machine.
    rerender({ currentMachineId: "mach-b" });

    expect(button().textContent).toContain(zh.settings.edit);
    expect(button().disabled).toBe(false);
  });

  it("times out into a retryable notice when the move never lands", async () => {
    const { onRelocate, resolve, calls } = deferredRelocate();
    const { button, notice } = mount({
      onRelocate,
      machines: [machine("mach-a")],
    });

    fireEvent.click(button());
    await act(async () => {
      resolve();
    });

    act(() => {
      vi.advanceTimersByTime(TIMEOUT_MS - 1);
    });
    expect(button().textContent).toContain(zh.machine.relocating);

    act(() => {
      vi.advanceTimersByTime(1);
    });
    expect(notice()?.textContent).toBe(zh.machine.relocateTimeout);
    // Retryable: the button is live again and a second press really fires.
    expect(button().disabled).toBe(false);
    fireEvent.click(button());
    expect(calls).toEqual(["mach-a", "mach-a"]);
    expect(notice()).toBeNull();
  });

  it("does not time out after the move landed", async () => {
    const { onRelocate, resolve } = deferredRelocate();
    const { button, notice, rerender } = mount({
      onRelocate,
      machines: [machine("mach-a")],
      boundMachineId: "mach-a",
    });

    fireEvent.click(button());
    await act(async () => {
      resolve();
    });
    rerender({ currentMachineId: "mach-a" });

    act(() => {
      vi.advanceTimersByTime(TIMEOUT_MS * 2);
    });
    expect(notice()).toBeNull();
    expect(button().textContent).toContain(zh.settings.edit);
  });

  it("fires the relocate WITHOUT entering 更換中 when the target is where the agent already is, and says 已送出", async () => {
    const { onRelocate, resolve, calls } = deferredRelocate();
    const { button, notice, sent } = mount({
      onRelocate,
      machines: [machine("mach-a")],
      boundMachineId: "mach-a",
      currentMachineId: "mach-a",
    });

    fireEvent.click(button());
    await act(async () => {
      resolve();
    });

    // Still a real relocate — (a) skips the unwinnable WAIT, not the request.
    expect(calls).toEqual(["mach-a"]);
    // No observable transition exists for a same-machine move, so the only exit
    // from 「更換中…」 would be the timeout — which would then claim, falsely,
    // that nothing had happened.
    expect(button().textContent).toContain(zh.settings.edit);
    expect(button().disabled).toBe(false);
    // …but this branch changes neither the status nor the button, which is the
    // very 「以為沒按到」 complaint. It gets the one honest fact instead.
    expect(sent()?.textContent).toBe(zh.machine.relocateSent);

    act(() => {
      vi.advanceTimersByTime(TIMEOUT_MS * 2);
    });
    expect(notice()).toBeNull();
  });

  it("drops the 已送出 chip by itself, so it can never read as a status", async () => {
    const { onRelocate, resolve } = deferredRelocate();
    const { button, sent } = mount({
      onRelocate,
      machines: [machine("mach-a")],
      boundMachineId: "mach-a",
      currentMachineId: "mach-a",
    });

    expect(RELOCATE_SENT_NOTICE_MS).toBe(SENT_MS);
    fireEvent.click(button());
    await act(async () => {
      resolve();
    });

    act(() => {
      vi.advanceTimersByTime(SENT_MS - 1);
    });
    expect(sent()).toBeTruthy();
    act(() => {
      vi.advanceTimersByTime(1);
    });
    expect(sent()).toBeNull();
  });

  it("still enters 更換中 and times out when the target is a DIFFERENT machine", async () => {
    const { onRelocate, resolve } = deferredRelocate();
    const { button, notice, sent } = mount({
      onRelocate,
      boundMachineId: "mach-a",
      currentMachineId: "mach-a", // observed on A, moving to B
    });

    fireEvent.click(button());
    fireEvent.change(
      document.querySelector<HTMLSelectElement>(
        "[data-testid=machine-picker-select]",
      )!,
      { target: { value: "mach-b" } },
    );
    fireEvent.click(document.querySelector("[data-testid=machine-picker-confirm]")!);
    await act(async () => {
      resolve();
    });

    expect(button().textContent).toContain(zh.machine.relocating);
    // …and the 已送出 chip belongs ONLY to the branch with no observable
    // landing: 「更換中…」 already says everything it would.
    expect(sent()).toBeNull();
    act(() => {
      vi.advanceTimersByTime(TIMEOUT_MS);
    });
    expect(notice()?.textContent).toBe(zh.machine.relocateTimeout);
    expect(sent()).toBeNull();
  });

  it("resets 更換中 when the panel switches to another agent", async () => {
    const { onRelocate, resolve } = deferredRelocate();
    const { button, rerender } = mount({
      onRelocate,
      machines: [machine("mach-a")],
    });

    fireEvent.click(button());
    await act(async () => {
      resolve();
    });
    expect(button().textContent).toContain(zh.machine.relocating);

    // Neither detail panel remounts on the switch (no `key` at either call
    // site), so the hook itself has to drop the previous agent's progress.
    rerender({ subjectId: "other" });

    expect(button().textContent).toContain(zh.settings.edit);
    expect(button().disabled).toBe(false);
  });

  it("resets a timed-out notice when the panel switches to another agent", async () => {
    const { onRelocate, resolve } = deferredRelocate();
    const { button, notice, rerender } = mount({
      onRelocate,
      machines: [machine("mach-a")],
    });

    fireEvent.click(button());
    await act(async () => {
      resolve();
    });
    act(() => {
      vi.advanceTimersByTime(TIMEOUT_MS);
    });
    expect(notice()?.textContent).toBe(zh.machine.relocateTimeout);

    rerender({ subjectId: "other" });

    expect(notice()).toBeNull();
    expect(button().textContent).toContain(zh.settings.edit);
  });

  it("surfaces a rejected relocate as a retryable failure", async () => {
    const { onRelocate, reject, calls } = deferredRelocate();
    const { button, notice, queryByTestId } = mount({
      onRelocate,
      machines: [machine("mach-a")],
    });

    fireEvent.click(button());
    await act(async () => {
      reject(new Error("http 500"));
    });

    expect(notice()?.textContent).toBe(zh.machine.relocateFailed);
    // Pre-existing property kept: no stale pending verdict survives a reject,
    // and the owner can retry.
    expect(queryByTestId("undispatched")).toBeNull();
    expect(button().disabled).toBe(false);
    fireEvent.click(button());
    expect(calls).toEqual(["mach-a", "mach-a"]);
  });

  it("hands a relocation_pending verdict to the caller instead of holding 更換中", async () => {
    const { onRelocate, resolve } = deferredRelocate();
    const { button, notice, queryByTestId } = mount({
      onRelocate,
      machines: [machine("mach-a")],
    });

    fireEvent.click(button());
    await act(async () => {
      resolve({ relocationPending: true });
    });

    // T-7fa1: the DispatchAlert REPLACES the spinner (it exists precisely to
    // take the place of a progress state that would never finish).
    expect(queryByTestId("undispatched")).toBeTruthy();
    expect(button().textContent).toContain(zh.settings.edit);
    expect(button().disabled).toBe(false);
    expect(notice()).toBeNull();
  });

  it("stays disabled with the no-online-machine tooltip when nothing is online", () => {
    const { onRelocate } = deferredRelocate();
    const { button } = mount({
      onRelocate,
      machines: [machine("mach-a", false)],
    });

    expect(button().disabled).toBe(true);
    expect(button().title).toBe(zh.machine.noOnlineMachine);
    expect(button().textContent).toContain(zh.settings.edit);
  });
});

// ── the server-recorded outcome: the FAILURE exit from 更換中 (T-e0e3) ────────
// `landed` only ever says "it worked". Before this, a relocate the server had
// already REFUSED still had exactly one exit — the 30s ceiling — so the cockpit
// span for half a minute on an answer that was sitting on the row the whole
// time. These pin the edge-detection: a NEW receipt reporting ok===false ends
// the wait and shows the server's own words; an OLD one does not.
describe("useRelocateMachine — the dispatch receipt", () => {
  it("a NEW ok:false receipt ends 更換中, shows the reason, and re-arms retry", async () => {
    const { onRelocate } = deferredRelocate();
    const { button, notice, reason, rerender } = mount({
      onRelocate,
      machines: [machine("mach-a")],
      dispatchReceipt: { at: 100, ok: null, reason: "" },
    });

    fireEvent.click(button());
    expect(button().textContent).toContain(zh.machine.relocating);

    await act(async () => {
      rerender({
        dispatchReceipt: {
          at: 200,
          ok: false,
          reason: "machine_unavailable: machine 'mach-a' is offline right now",
        },
      });
    });

    expect(button().textContent).not.toContain(zh.machine.relocating);
    expect(button().disabled).toBe(false); // retryable
    expect(notice()?.textContent).toBe(zh.machine.relocateFailed);
    expect(reason()?.textContent).toContain("machine_unavailable");
  });

  it("a receipt that did NOT change leaves 更換中 alone (stale is not an answer)", async () => {
    const { onRelocate } = deferredRelocate();
    const stale = {
      at: 100,
      ok: false,
      reason: "machine_unavailable: from an hour ago",
    };
    const { button, notice, rerender } = mount({
      onRelocate,
      machines: [machine("mach-a")],
      dispatchReceipt: stale,
    });

    fireEvent.click(button());
    expect(button().textContent).toContain(zh.machine.relocating);
    // Same `at` — a re-render, not news.
    await act(async () => {
      rerender({ dispatchReceipt: { ...stale } });
    });
    expect(button().textContent).toContain(zh.machine.relocating);
    expect(notice()).toBeNull();
  });

  it("a NEW ok:true receipt does NOT end the wait — a dispatch is not an arrival", async () => {
    const { onRelocate } = deferredRelocate();
    const { button, notice, rerender } = mount({
      onRelocate,
      machines: [machine("mach-a")],
      dispatchReceipt: { at: 100, ok: null, reason: "" },
    });

    fireEvent.click(button());
    expect(button().textContent).toContain(zh.machine.relocating);
    await act(async () => {
      rerender({ dispatchReceipt: { at: 200, ok: true, reason: "" } });
    });
    // Success is `landed`'s job. Reading a successful DISPATCH receipt as
    // arrival would repeat the exact conflation this whole change removes.
    expect(button().textContent).toContain(zh.machine.relocating);
    expect(notice()).toBeNull();
  });
});
