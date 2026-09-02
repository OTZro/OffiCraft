import { describe, it, expect } from "vitest";
import { renderHook } from "@testing-library/react";
import { useKeyedRecord } from "./useKeyedRecord";

describe("useKeyedRecord", () => {
  it("hands back the SAME record until the key really changes", () => {
    let made = 0;
    const { result, rerender } = renderHook(
      ({ key }: { key: string }) =>
        useKeyedRecord(key, (k) => {
          made += 1;
          return { key: k, flag: false };
        }),
      { initialProps: { key: "a" } },
    );
    const first = result.current;
    first.flag = true;

    // 🔴 R4-2. The rebuild used to sit in an effect's setup body, which re-runs
    // for reasons that have nothing to do with the key — StrictMode's
    // setup→cleanup→setup on every mount, and any dependency somebody adds
    // later. Rebuilding then re-arms a latch behind a one-shot caller that is
    // never coming back, and the room never refreshes again.
    rerender({ key: "a" });
    expect(result.current).toBe(first);
    expect(result.current.flag).toBe(true);
    expect(made).toBe(1);
  });

  it("a key change builds a fresh record, and the old one keeps its own state", () => {
    // 🔴 R4-1/F2/R3-1. An in-flight job holds the record it captured: writing
    // into it after the key moved on writes into an orphan nobody reads, which
    // is exactly right — its debts died with its key. The hand-written reset
    // block instead zeroed the LIVE record, so a late finally cleared the NEW
    // key's latch.
    const { result, rerender } = renderHook(
      ({ key }: { key: string }) =>
        useKeyedRecord(key, (k) => ({ key: k, flag: false })),
      { initialProps: { key: "a" } },
    );
    const forA = result.current;
    forA.flag = true;

    rerender({ key: "b" });
    const forB = result.current;
    expect(forB).not.toBe(forA);
    expect(forB.key).toBe("b");
    expect(forB.flag).toBe(false);

    // The late writer still holds A's record.
    forA.flag = false;
    expect(forB.flag).toBe(false);
  });

  it("a changed `make` on the same key is ignored — the key is the only trigger", () => {
    const { result, rerender } = renderHook(
      ({ key, seed }: { key: string; seed: number }) =>
        useKeyedRecord(key, () => ({ seed })),
      { initialProps: { key: "a", seed: 1 } },
    );
    rerender({ key: "a", seed: 2 });
    expect(result.current.seed).toBe(1);
    rerender({ key: "b", seed: 2 });
    expect(result.current.seed).toBe(2);
  });
});
