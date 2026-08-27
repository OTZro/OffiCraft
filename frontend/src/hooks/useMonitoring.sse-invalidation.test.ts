// useMonitoring · T-10 SSE version-invalidation guard.
//
// useMonitoring carried the same defect as useMachines: the "monitor" SSE branch
// bumped `requestVersion` — cancelling every request already in flight — while
// issuing no request of its own, leaving the trailing refresh up to
// `refreshSeconds` (5s) later as the only repair.
//
// The trigger differs from useMachines'. Nothing here self-triggers the way
// POST /api/machines does (that publishes `member`, which this hook ignores).
// What collides instead is background telemetry: the server publishes
// `monitoring` on every agent signal (api_monitoring.go), while MonitorPage
// reconciles a rename with `patchAccount(...).then(() => refetch())`. A
// heartbeat landing inside that refetch discarded the renamed label and left the
// panel showing the OLD name for up to 5 seconds.
//
// Both cases below are anchored causally rather than eventually. What excludes
// the trailing poll is that they run on FAKE TIMERS THAT ARE NEVER ADVANCED —
// no scheduled callback can fire at all — and the request-count assertions pin
// that down. `refreshSeconds: ONE_HOUR` is belt-and-braces for a reader, not the
// mechanism; these stay non-tautological at the default 5. Asserting only "the
// new label shows up eventually" would pass on the broken hook too, since the
// poll reaches the same final state.

import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { renderHook, act } from "@testing-library/react";

const h = vi.hoisted(() => ({
  getMonitoring: vi.fn<() => Promise<unknown>>(),
  sseHandler: null as ((topic: string) => void) | null,
}));

vi.mock("../api", () => ({
  api: {
    getMonitoring: h.getMonitoring,
    subscribeEvents: (cb: (topic: string) => void) => {
      h.sseHandler = cb;
      return () => { h.sseHandler = null; };
    },
  },
}));

import { useMonitoring } from "./useMonitoring";

beforeEach(() => {
  h.getMonitoring.mockReset().mockResolvedValue({ sessions: [] });
  h.sseHandler = null;
});
afterEach(() => vi.useRealTimers());

function deferred<T>() {
  let resolve!: (value: T) => void;
  const promise = new Promise<T>((r) => { resolve = r; });
  return { promise, resolve };
}

const ONE_HOUR = 3_600;

describe("useMonitoring · a mid-flight monitoring frame must not cancel the request", () => {
  // MUTANT (proven 2026-08-27): restore `requestVersion.current += 1;` in
  // useMonitoring.ts → red on "the mid-flight monitoring frame must not discard
  // the rename refetch's own answer: expected { label: 'old-name' } to deeply
  // equal { label: 'new-name' }".
  it("applies an in-flight refetch's own answer when a monitoring frame lands mid-flight (never via the poll)", async () => {
    vi.useFakeTimers();
    const inflight = deferred<unknown>();
    h.getMonitoring.mockResolvedValueOnce({ label: "old-name" });
    const { result } = renderHook(() => useMonitoring({ refreshSeconds: ONE_HOUR }));
    await act(async () => { await Promise.resolve(); });
    expect(result.current.monitoring).toEqual({ label: "old-name" });

    // MonitorPage's renameAccount: PATCH, then `refetch()` for the fresh label.
    h.getMonitoring.mockImplementationOnce(() => inflight.promise);
    const reconcile = result.current.refetch();

    // An agent telemetry heartbeat lands while that GET is still open.
    act(() => { h.sseHandler?.("monitoring"); });

    await act(async () => {
      inflight.resolve({ label: "new-name" });
      await reconcile;
    });

    expect(
      result.current.monitoring,
      "the mid-flight monitoring frame must not discard the rename refetch's own answer",
    ).toEqual({ label: "new-name" });
    expect(
      h.getMonitoring,
      "and it must be THAT request that did it — no trailing poll has run, nor could it have",
    ).toHaveBeenCalledTimes(2);
  });

  // The guard rail the old useMachines case existed to protect, mirrored here:
  // issue order decides, not resolve order. Kept so the fix above cannot be
  // "simplified" into dropping version precedence altogether.
  it("does not let an older manual refresh overwrite a later event refresh", async () => {
    vi.useFakeTimers();
    const older = deferred<unknown>();
    h.getMonitoring.mockResolvedValueOnce({ label: "initial" });
    const { result } = renderHook(() => useMonitoring({ refreshSeconds: 5 }));
    await act(async () => { await Promise.resolve(); });

    h.getMonitoring.mockImplementationOnce(() => older.promise);
    const olderCall = result.current.refetch();

    act(() => { h.sseHandler?.("monitoring"); });
    h.getMonitoring.mockResolvedValueOnce({ label: "event" });
    await act(async () => { await vi.advanceTimersByTimeAsync(5_000); await Promise.resolve(); });
    expect(h.getMonitoring, "the event's trailing refresh must be a real request").toHaveBeenCalledTimes(3);
    expect(result.current.monitoring).toEqual({ label: "event" });

    await act(async () => { older.resolve({ label: "stale-manual" }); await olderCall; });
    expect(
      result.current.monitoring,
      "a refetch issued BEFORE the event refresh must never overwrite it, however late it resolves",
    ).toEqual({ label: "event" });
  });
});
