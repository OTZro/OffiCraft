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

  // T-10: this case's real subject is the SINGLE-FLIGHT rule — an event burst
  // arriving during an in-flight request must not fan out into concurrent
  // duplicates, and must collapse into exactly ONE follow-up afterwards. Both
  // call-count assertions below are unchanged and still carry that.
  // What changed is the middle assertion. It used to demand the in-flight
  // answer be THROWN AWAY (`machines` still empty after it resolved), which is
  // the T-10 defect: an event frame does not make an in-flight response wrong,
  // and discarding it leaves the view stranded on older data — here, on nothing
  // at all — with no request outstanding to fix it. The answer now lands, and
  // the follow-up still supersedes it.
  // MUTANT (proven 2026-08-27): restore the bump → red on "the in-flight answer
  // must land rather than be discarded by the burst: expected [] to deeply equal
  // [ { id: 'first' } ]". The two call-count assertions stay green under it —
  // they guard single-flight, which the defect never broke.
  it("waits for an in-flight request, shows its answer, then refreshes exactly once more", async () => {
    vi.useFakeTimers();
    const first = deferred<unknown>();
    h.listMachines.mockImplementationOnce(() => first.promise);
    const { result } = renderHook(() => useMachines({ refreshSeconds: 12 }));
    expect(h.listMachines).toHaveBeenCalledTimes(1);

    act(() => { h.handler?.("monitoring"); h.handler?.("member"); });
    await act(async () => { await vi.advanceTimersByTimeAsync(12_000); });
    expect(h.listMachines, "a burst must not fan out while a request is open").toHaveBeenCalledTimes(1);

    await act(async () => { first.resolve([{ id: "first" }]); await Promise.resolve(); });
    expect(
      result.current.machines,
      "the in-flight answer must land rather than be discarded by the burst",
    ).toEqual([{ id: "first" }]);
    h.listMachines.mockResolvedValueOnce([{ id: "fresh" }]);
    await act(async () => { await vi.advanceTimersByTimeAsync(0); await Promise.resolve(); });
    expect(h.listMachines, "the whole burst collapses into ONE follow-up").toHaveBeenCalledTimes(2);
    expect(result.current.machines).toEqual([{ id: "fresh" }]);
  });

  // ── T-10 ────────────────────────────────────────────────────────────────
  // This case used to assert that a bare `member`/`monitoring` frame CANCELS an
  // in-flight refetch: it resolved the manual request, expected its result to be
  // thrown away, and expected the view to stay stale until the 5s trailing poll.
  // That is the T-10 defect written down as a specification, and it is what the
  // 監控 inline-row e2e was intermittently red on.
  //
  // The INTENT behind the case is correct and is kept: a request that was ISSUED
  // EARLIER must never overwrite the result of one issued LATER, whatever order
  // the two resolve in. What changed is how that intent is exercised — the later
  // refresh is now a REAL request (the trailing refresh actually issuing a GET)
  // rather than a bare version bump that issued nothing. The precedence rule is
  // the same monotonic `requestVersion`, and this case still fails if it breaks.
  it("does not let an older manual refresh overwrite a later event refresh", async () => {
    vi.useFakeTimers();
    const manual = deferred<unknown>();
    h.listMachines.mockResolvedValueOnce([{ id: "initial" }]);
    const { result } = renderHook(() => useMachines());
    await act(async () => { await Promise.resolve(); });

    // (1) a manual refetch goes out and stays open…
    h.listMachines.mockImplementationOnce(() => manual.promise);
    const older = result.current.refetch();

    // (2) …an event lands, and its trailing refresh is issued AFTER the manual
    //     and answers FIRST.
    act(() => { h.handler?.("monitoring"); });
    h.listMachines.mockResolvedValueOnce([{ id: "event" }]);
    await act(async () => { await vi.advanceTimersByTimeAsync(5_000); await Promise.resolve(); });
    expect(h.listMachines, "the event's trailing refresh must be a real request").toHaveBeenCalledTimes(3);
    expect(result.current.machines).toEqual([{ id: "event" }]);

    // (3) the older manual now answers last — and must NOT win. This is the
    //     guard rail: issue order decides, not resolve order.
    await act(async () => { manual.resolve([{ id: "stale-manual" }]); await older; });
    expect(
      result.current.machines,
      "a refetch issued BEFORE the event refresh must never overwrite it, however late it resolves",
    ).toEqual([{ id: "event" }]);
  });

  // ── T-10 regression guard ───────────────────────────────────────────────
  // The bug this replaces was invisible to a "the row shows up eventually"
  // assertion: with the 5s trailing poll running, the broken hook and the fixed
  // hook reach the SAME final state — the e2e only ever caught it by having a
  // timeout that happened to be shorter than the delay. So asserting the final
  // value alone asserts nothing.
  //
  // The anchor here is causal, not eventual: `refreshSeconds` is pushed an hour
  // out and NO timer is advanced, so the trailing poll provably cannot be what
  // repaired the view — the call-count assertion pins that down. The only thing
  // that can put the new machine on screen is the in-flight response itself.
  // MUTANT (proven 2026-08-27): put `requestVersion.current += 1;` back at the
  // top of the monitor/member branch in useMachines.ts → this reddens on the
  // named assertion below, "the mid-flight member frame must not discard the
  // onboard refetch's own (correct) answer: expected [ { id: 'm-server-self' } ]
  // to deeply equal [ { id: 'm-server-self' }, …(1) ]" — not on a timeout.
  it("applies an in-flight refetch's own answer when a member frame lands mid-flight (never via the poll)", async () => {
    vi.useFakeTimers();
    const ONE_HOUR = 3_600;
    const inflight = deferred<unknown>();
    h.listMachines.mockResolvedValueOnce([{ id: "m-server-self" }]);
    const { result } = renderHook(() => useMachines({ refreshSeconds: ONE_HOUR }));
    await act(async () => { await Promise.resolve(); });
    expect(result.current.machines).toEqual([{ id: "m-server-self" }]);

    // MonitorPage's onboard: POST /api/machines, then `await refetchMachines()`.
    h.listMachines.mockImplementationOnce(() => inflight.promise);
    const reconcile = result.current.refetch();

    // The POST's OWN `member` frame arrives while that GET is still open —
    // measured in a real browser at +127ms into a 400ms GET (T-10 step 1).
    act(() => { h.handler?.("member"); });

    // The GET answers, and its answer already carries the onboarded machine.
    await act(async () => {
      inflight.resolve([{ id: "m-server-self" }, { id: "e2e-box" }]);
      await reconcile;
    });

    expect(
      result.current.machines,
      "the mid-flight member frame must not discard the onboard refetch's own (correct) answer",
    ).toEqual([{ id: "m-server-self" }, { id: "e2e-box" }]);
    expect(
      h.listMachines,
      "and it must be THAT request that did it — no trailing poll has run, nor could it have",
    ).toHaveBeenCalledTimes(2);
  });
});
