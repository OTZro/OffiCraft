// useMonitoring enabled-gating (T-ec2c) black-box pins.
//
// The monitoring fold is a large payload the server re-signals on EVERY agent
// telemetry heartbeat (topic "monitoring" ⊂ "monitor"). The office page only
// needs it while a member detail panel is open, so it passes enabled=false
// otherwise — and a disabled hook must make ZERO requests and hold NO
// subscription (merely being on the office page must not stream monitoring).

import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { renderHook, waitFor, act } from "@testing-library/react";

const h = vi.hoisted(() => ({
  getMonitoring: vi.fn<() => Promise<unknown>>(),
  subscribed: 0,
  sseHandler: null as ((topic: string) => void) | null,
}));

vi.mock("../api", () => ({
  api: {
    getMonitoring: h.getMonitoring,
    subscribeEvents: (cb: (topic: string) => void) => {
      h.subscribed += 1;
      h.sseHandler = cb;
      return () => {
        h.sseHandler = null;
      };
    },
  },
}));

import { useMonitoring } from "./useMonitoring";

beforeEach(() => {
  h.getMonitoring.mockReset().mockResolvedValue({ sessions: [] });
  h.subscribed = 0;
  h.sseHandler = null;
});

afterEach(() => vi.useRealTimers());

function emit(topic: string) {
  act(() => {
    h.sseHandler?.(topic);
  });
}

function deferred<T>() {
  let resolve!: (value: T) => void;
  const promise = new Promise<T>((r) => { resolve = r; });
  return { promise, resolve };
}

describe("useMonitoring enabled (default, e.g. Monitor page)", () => {
  it("coalesces a burst into one trailing refresh", async () => {
    vi.useFakeTimers();
    renderHook(() => useMonitoring());
    await act(async () => { await Promise.resolve(); });
    expect(h.getMonitoring).toHaveBeenCalledTimes(1);
    emit("monitoring");
    emit("monitoring");
    emit("monitoring");
    expect(h.getMonitoring).toHaveBeenCalledTimes(1);
    await act(async () => { await vi.advanceTimersByTimeAsync(5000); });
    expect(h.getMonitoring).toHaveBeenCalledTimes(2);
  });

  it("waits for an in-flight request, drops its stale result, then refreshes once", async () => {
    vi.useFakeTimers();
    const first = deferred<{ sessions: string[] }>();
    h.getMonitoring.mockImplementationOnce(() => first.promise);
    const { result } = renderHook(() => useMonitoring({ refreshSeconds: 12 }));
    expect(h.getMonitoring).toHaveBeenCalledTimes(1);

    emit("monitoring");
    emit("monitoring");
    await act(async () => { await vi.advanceTimersByTimeAsync(12_000); });
    expect(h.getMonitoring).toHaveBeenCalledTimes(1);

    await act(async () => { first.resolve({ sessions: ["stale"] }); await Promise.resolve(); });
    expect(result.current.monitoring).toBeNull();
    h.getMonitoring.mockResolvedValueOnce({ sessions: ["fresh"] });
    await act(async () => { await vi.advanceTimersByTimeAsync(0); });
    expect(h.getMonitoring).toHaveBeenCalledTimes(2);
    await act(async () => { await Promise.resolve(); });
    expect(result.current.monitoring).toEqual({ sessions: ["fresh"] });
  });

  it("does not let an older manual refresh overwrite a later event refresh", async () => {
    vi.useFakeTimers();
    const manual = deferred<{ sessions: string[] }>();
    h.getMonitoring.mockResolvedValueOnce({ sessions: ["initial"] });
    const { result } = renderHook(() => useMonitoring());
    await act(async () => { await Promise.resolve(); });

    h.getMonitoring.mockImplementationOnce(() => manual.promise);
    void result.current.refetch();
    emit("monitoring");
    await act(async () => { manual.resolve({ sessions: ["stale-manual"] }); await Promise.resolve(); });
    expect(result.current.monitoring).toEqual({ sessions: ["initial"] });

    await act(async () => { await vi.advanceTimersByTimeAsync(5_000); });
    await act(async () => { await Promise.resolve(); });
    expect(h.getMonitoring).toHaveBeenCalledTimes(3);
    expect(result.current.monitoring).toEqual({ sessions: [] });
  });
});

describe("useMonitoring disabled (T-ec2c, office page w/ no detail panel)", () => {
  it("makes NO request and holds NO subscription", async () => {
    const { result } = renderHook(() => useMonitoring({ enabled: false }));
    // Let any errant mount fetch settle, then assert none happened.
    await Promise.resolve();
    // LOAD-BEARING negatives. MUTANT: make the effect ignore `enabled` (always
    // fetch/subscribe) and BOTH of these go red.
    expect(h.getMonitoring).not.toHaveBeenCalled();
    expect(h.subscribed).toBe(0);
    // A telemetry heartbeat cannot reach a hook that never subscribed.
    emit("monitoring");
    expect(h.getMonitoring).not.toHaveBeenCalled();
    // Disabled must not hang on a spinner.
    expect(result.current.loading).toBe(false);
  });

  it("starts fetching when it flips enabled (detail panel opened)", async () => {
    const { rerender } = renderHook(
      ({ on }: { on: boolean }) => useMonitoring({ enabled: on }),
      { initialProps: { on: false } }
    );
    await Promise.resolve();
    expect(h.getMonitoring).not.toHaveBeenCalled();

    rerender({ on: true });
    await waitFor(() => expect(h.getMonitoring).toHaveBeenCalledTimes(1));
    expect(h.subscribed).toBe(1);
  });
});
