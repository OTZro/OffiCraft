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
});
