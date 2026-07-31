// useTasks — 勾什麼就問什麼 (T-a3e4) black-box pins.
//
// REPLACES useTasks.open.test.ts, which pinned T-2b9d's boolean fast path
// (`?open=true` by default, the FULL population once `includeClosed` flipped).
// That contract is gone, and keeping its tests would have pinned the payload bug
// itself: open-only removed the archive but still shipped every live task, and
// the flag was re-derived from the rendered list (TasksPage's needClosed), which
// is what let one dep-carrying task widen every subsequent fetch to the whole
// history. The hook now asks for the STATUS SET the page states.
//
// What each test would have to see fail: the assertions read the ARGUMENT VALUES
// (which statuses), not the call count — "it refetched less" is satisfiable by a
// hook that asks for the wrong rows.

import { describe, it, expect, vi, beforeEach } from "vitest";
import { renderHook, waitFor, act } from "@testing-library/react";

const h = vi.hoisted(() => ({
  listTasks: vi.fn<
    (opts?: { open?: boolean; statuses?: string[] }) => Promise<unknown[]>
  >(),
  listOutsourceWorkers: vi.fn<() => Promise<unknown[]>>(),
  listTaskTypes: vi.fn<() => Promise<unknown[]>>(),
  sseHandler: null as ((topic: string) => void) | null,
}));

vi.mock("../api", () => ({
  api: {
    listTasks: h.listTasks,
    listOutsourceWorkers: h.listOutsourceWorkers,
    listTaskTypes: h.listTaskTypes,
    subscribeEvents: (cb: (topic: string) => void) => {
      h.sseHandler = cb;
      return () => {
        h.sseHandler = null;
      };
    },
  },
}));

import { useTasks } from "./useTasks";

const NON_TERMINAL = [
  "not_started",
  "in_progress",
  "waiting_owner",
  "waiting_external",
  "reassigning",
];

beforeEach(() => {
  h.listTasks.mockReset().mockResolvedValue([]);
  h.listOutsourceWorkers.mockReset().mockResolvedValue([]);
  h.listTaskTypes.mockReset().mockResolvedValue([]);
  h.sseHandler = null;
});

function statusesOf(call: number): string[] | undefined {
  return h.listTasks.mock.calls[call][0]?.statuses;
}

describe("useTasks (T-a3e4)", () => {
  it("the MOUNT fetch already carries the caller's default set", async () => {
    // The expensive frame: the mount fetch precedes every effect, so a hook that
    // opened unconstrained would pull the whole archive once per page open and
    // only then narrow. MUTANT: default statusKey to undefined → red.
    renderHook(() => useTasks(NON_TERMINAL));
    await waitFor(() => expect(h.listTasks).toHaveBeenCalledTimes(1));
    expect(statusesOf(0)).toEqual([...NON_TERMINAL].sort());
    // …and it is NOT the old open-only flag: that shipped every live task
    // regardless of what the 狀態 dropdown had ticked.
    expect(h.listTasks.mock.calls[0][0]?.open).toBeUndefined();
  });

  it("asks for EXACTLY the set it is given, and re-asks when the set changes", async () => {
    const { result } = renderHook(() => useTasks(NON_TERMINAL));
    await waitFor(() => expect(h.listTasks).toHaveBeenCalledTimes(1));

    act(() => result.current.setStatuses(["done", "in_progress"]));
    await waitFor(() => expect(h.listTasks).toHaveBeenCalledTimes(2));
    expect(statusesOf(1)).toEqual(["done", "in_progress"]);

    act(() => result.current.setStatuses(["waiting_owner"]));
    await waitFor(() => expect(h.listTasks).toHaveBeenCalledTimes(3));
    expect(statusesOf(2)).toEqual(["waiting_owner"]);
  });

  it("an EMPTY set (清除篩選 = 所有狀態) drops the constraint entirely", async () => {
    const { result } = renderHook(() => useTasks(NON_TERMINAL));
    await waitFor(() => expect(h.listTasks).toHaveBeenCalledTimes(1));
    act(() => result.current.setStatuses([]));
    await waitFor(() => expect(h.listTasks).toHaveBeenCalledTimes(2));
    // undefined opts = the unfiltered list, byte-for-byte the old full fetch —
    // the ONE view that genuinely needs it must still be able to ask.
    expect(h.listTasks.mock.calls[1][0]).toBeUndefined();
    act(() => result.current.setStatuses(undefined));
    // Same ask ⇒ no refetch: the key is what changed, not the array identity.
    await new Promise((r) => setTimeout(r, 0));
    expect(h.listTasks).toHaveBeenCalledTimes(2);
  });

  it("an SSE task delta re-asks with the SAME set — it never widens", async () => {
    // 🔴 The regression this hook exists to prevent: the old flag was derived
    // from the loaded rows, so one dep-carrying task turned every delta into a
    // full-history download (measured 408 KB vs 17 KB).
    renderHook(() => useTasks(NON_TERMINAL));
    await waitFor(() => expect(h.listTasks).toHaveBeenCalledTimes(1));
    act(() => h.sseHandler?.("task"));
    await waitFor(() => expect(h.listTasks).toHaveBeenCalledTimes(2));
    expect(statusesOf(1)).toEqual([...NON_TERMINAL].sort());
    expect(
      h.listTasks.mock.calls.every((c) => c[0]?.statuses !== undefined)
    ).toBe(true);
  });

  it("an equal-but-new array does not re-fire the fetch", async () => {
    // The page rebuilds its Set on every render (a 30s clock tick is enough), so
    // identity-based deps would re-download the list for nothing.
    const { result } = renderHook(() => useTasks(NON_TERMINAL));
    await waitFor(() => expect(h.listTasks).toHaveBeenCalledTimes(1));
    act(() => result.current.setStatuses([...NON_TERMINAL]));
    act(() => result.current.setStatuses([...NON_TERMINAL].reverse()));
    await new Promise((r) => setTimeout(r, 0));
    expect(h.listTasks).toHaveBeenCalledTimes(1);
  });
});
