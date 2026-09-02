import { describe, it, expect } from "vitest";
import { renderHook, act } from "@testing-library/react";
import { useKeyedState } from "./useKeyedState";

describe("useKeyedState", () => {
  it("a key change resets to the initial value without anybody writing a reset line", () => {
    // 🔴 The React half of the reset list (R5-1). It used to be six
    // `setX(null)` calls in a `peerIdRef` block — a hand-written list, with
    // the same hole the ref half had: a seventh state added without a seventh
    // line compiles, goes green, and leaks into the next conversation.
    const { result, rerender } = renderHook(
      ({ key }: { key: string }) => useKeyedState<string | null>(key, null),
      { initialProps: { key: "a" } },
    );
    act(() => result.current[1]("a's notice"));
    expect(result.current[0]).toBe("a's notice");

    rerender({ key: "b" });
    expect(result.current[0]).toBe(null);
  });

  it("the value survives re-renders that are not key changes", () => {
    const { result, rerender } = renderHook(
      ({ key }: { key: string }) => useKeyedState(key, 0),
      { initialProps: { key: "a" } },
    );
    act(() => result.current[1]((n) => n + 1));
    rerender({ key: "a" });
    expect(result.current[0]).toBe(1);
  });

  it("a setter taken under the old key cannot write into the new one", () => {
    // 🔴 R5-1 itself, in one line. The anchor fetch that ends `unreachable`
    // holds the setter it captured before the owner switched conversations;
    // without this the banner it paints lands in somebody else's room.
    const { result, rerender } = renderHook(
      ({ key }: { key: string }) => useKeyedState<string | null>(key, null),
      { initialProps: { key: "a" } },
    );
    const staleSetter = result.current[1];

    rerender({ key: "b" });
    act(() => staleSetter("a's late failure"));
    expect(result.current[0]).toBe(null);

    // …and the live setter still works, so the guard is not just "never write".
    act(() => result.current[1]("b's own"));
    expect(result.current[0]).toBe("b's own");
  });

  it("re-asserting the same value does not re-render — the useState bail-out survives the wrapper", () => {
    // 🔴 Measured, not theoretical. Wrapping the value in a `{key, value}` slot
    // costs the bail-out `useState` gives for free, and ChatArea has callers
    // that re-assert the same value on every commit (the scroll reactor's
    // `setLatestInView(distance <= AT_LATEST_PX)`). Without this the component
    // renders forever: a 4GB heap OOM in `ChatArea.image.test.tsx`.
    let renders = 0;
    const { result } = renderHook(() => {
      renders += 1;
      return useKeyedState("a", true);
    });
    const after = renders;
    act(() => result.current[1](true));
    act(() => result.current[1](() => true));
    expect(renders).toBe(after);
    // …and a real change still lands.
    act(() => result.current[1](false));
    expect(result.current[0]).toBe(false);
    expect(renders).toBeGreaterThan(after);
  });

  it("a lazy initial is evaluated once per key, not once per render", () => {
    let built = 0;
    const { result, rerender } = renderHook(
      ({ key }: { key: string }) =>
        useKeyedState(key, () => {
          built += 1;
          return key;
        }),
      { initialProps: { key: "a" } },
    );
    rerender({ key: "a" });
    rerender({ key: "a" });
    expect(built).toBe(1);
    rerender({ key: "b" });
    expect(result.current[0]).toBe("b");
    expect(built).toBe(2);
  });
});
