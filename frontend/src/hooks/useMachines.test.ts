import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, renderHook } from "@testing-library/react";

const h = vi.hoisted(() => ({
  listMachines: vi.fn<() => Promise<unknown>>(),
  handler: null as ((topic: string) => void) | null,
}));

vi.mock("../api", () => ({
  api: {
    listMachines: h.listMachines,
    subscribeEvents: (handler: (topic: string) => void) => { h.handler = handler; return () => { h.handler = null; }; },
  },
}));

import { useMachines } from "./useMachines";

beforeEach(() => { h.listMachines.mockReset().mockResolvedValue([]); h.handler = null; });
afterEach(() => vi.useRealTimers());

function deferred<T>() {
  let resolve!: (value: T) => void;
  const promise = new Promise<T>((r) => { resolve = r; });
  return { promise, resolve };
}

describe("useMachines", () => {
  it("coalesces monitoring events into one trailing machine refresh", async () => {
    vi.useFakeTimers();
    renderHook(() => useMachines());
    await act(async () => { await Promise.resolve(); });
    expect(h.listMachines).toHaveBeenCalledTimes(1);
    act(() => { h.handler?.("monitoring"); h.handler?.("monitoring"); h.handler?.("member"); });
    expect(h.listMachines).toHaveBeenCalledTimes(1);
    await act(async () => { await vi.advanceTimersByTimeAsync(5000); });
    expect(h.listMachines).toHaveBeenCalledTimes(2);
  });

  it("waits for an in-flight request, drops its stale result, then refreshes once", async () => {
    vi.useFakeTimers();
    const first = deferred<unknown>();
    h.listMachines.mockImplementationOnce(() => first.promise);
    const { result } = renderHook(() => useMachines({ refreshSeconds: 12 }));
    expect(h.listMachines).toHaveBeenCalledTimes(1);

    act(() => { h.handler?.("monitoring"); h.handler?.("member"); });
    await act(async () => { await vi.advanceTimersByTimeAsync(12_000); });
    expect(h.listMachines).toHaveBeenCalledTimes(1);

    await act(async () => { first.resolve([{ id: "stale" }]); await Promise.resolve(); });
    expect(result.current.machines).toEqual([]);
    h.listMachines.mockResolvedValueOnce([{ id: "fresh" }]);
    await act(async () => { await vi.advanceTimersByTimeAsync(0); await Promise.resolve(); });
    expect(h.listMachines).toHaveBeenCalledTimes(2);
    expect(result.current.machines).toEqual([{ id: "fresh" }]);
  });

  it("does not let an older manual refresh overwrite a later event refresh", async () => {
    vi.useFakeTimers();
    const manual = deferred<unknown>();
    h.listMachines.mockResolvedValueOnce([{ id: "initial" }]);
    const { result } = renderHook(() => useMachines());
    await act(async () => { await Promise.resolve(); });

    h.listMachines.mockImplementationOnce(() => manual.promise);
    void result.current.refetch();
    act(() => { h.handler?.("monitoring"); });
    await act(async () => { manual.resolve([{ id: "stale-manual" }]); await Promise.resolve(); });
    expect(result.current.machines).toEqual([{ id: "initial" }]);

    await act(async () => { await vi.advanceTimersByTimeAsync(5_000); await Promise.resolve(); });
    expect(h.listMachines).toHaveBeenCalledTimes(3);
    expect(result.current.machines).toEqual([]);
  });
});
