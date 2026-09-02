import { describe, it, expect } from "vitest";
import { renderHook, act } from "@testing-library/react";
import { useKeyedState } from "./useKeyedState";

/** One visit to a conversation. `useKeyedRecord` hands back exactly this: a
 * fresh object every time the conversation is (re-)entered, so A→B→A yields
 * three of them and the second visit to A is not mistaken for the first. */
const visit = () => ({});

describe("useKeyedState", () => {
  it("a new visit resets to the initial value without anybody writing a reset line", () => {
    // 🔴 The React half of the reset list (R5-1). It used to be six
    // `setX(null)` calls in a `peerIdRef` block — a hand-written list, with
    // the same hole the ref half had: a seventh state added without a seventh
    // line compiles, goes green, and leaks into the next conversation.
    const [a, b] = [visit(), visit()];
    const { result, rerender } = renderHook(
      ({ v }: { v: object }) => useKeyedState<string | null>(v, null),
      { initialProps: { v: a } },
    );
    act(() => result.current[1]("a's notice"));
    expect(result.current[0]).toBe("a's notice");

    rerender({ v: b });
    expect(result.current[0]).toBe(null);
  });

  it("the value survives re-renders that are not a new visit", () => {
    const a = visit();
    const { result, rerender } = renderHook(
      ({ v }: { v: object }) => useKeyedState(v, 0),
      { initialProps: { v: a } },
    );
    act(() => result.current[1]((n) => n + 1));
    rerender({ v: a });
    expect(result.current[0]).toBe(1);
  });

  it("a late write from an earlier visit to the SAME conversation neither lands nor wipes what this visit put there", () => {
    // 🔴 R6-1, at the hook. The first four rounds of this bug were "A's late
    // work writes into B"; the sixth was "A's late work writes into A's NEXT
    // VISIT" — reached by entering A at an anchor, leaving the 502 in the air,
    // clicking B and clicking A again. Identity, not the peer id, is what tells
    // those two visits apart, and this test is red in BOTH directions:
    //
    //  · bind the hook to `member.id` again (a string) and the stale setter's
    //    key equals the live one ⇒ its 「讀不到那則訊息」 lands on this visit;
    //  · drop the `s.visit !== visit` guard and the stale write stamps the OLD
    //    visit onto the slot, so the very next render rebuilds — silently
    //    throwing away the value THIS visit had legitimately set.
    //
    // The second half is why the guard is load-bearing rather than the
    // redundant assertion it was under string keys (fifth-round P4).
    const [a1, b, a2] = [visit(), visit(), visit()];
    const { result, rerender } = renderHook(
      ({ v }: { v: object }) => useKeyedState<string | null>(v, null),
      { initialProps: { v: a1 } },
    );
    const staleSetter = result.current[1];

    rerender({ v: b });
    rerender({ v: a2 });
    act(() => result.current[1]("this visit's own notice"));

    act(() => staleSetter("the first visit's late failure"));
    expect(result.current[0]).toBe("this visit's own notice");

    // …and the live setter still works, so the guard is not just "never write".
    act(() => result.current[1]("and it still moves"));
    expect(result.current[0]).toBe("and it still moves");
  });

  it("re-asserting the same value does not re-render — the useState bail-out survives the wrapper", () => {
    // 🔴 Measured, not theoretical. Wrapping the value in a `{visit, value}`
    // slot costs the bail-out `useState` gives for free, and ChatArea has
    // callers that re-assert the same value on every commit (the scroll
    // reactor's `setLatestInView(distance <= AT_LATEST_PX)`). Without this the
    // component renders forever: a 4GB heap OOM in `ChatArea.image.test.tsx`.
    const a = visit();
    let renders = 0;
    const { result } = renderHook(() => {
      renders += 1;
      return useKeyedState(a, true);
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

  it("a lazy initial is evaluated once per visit, not once per render", () => {
    const [a, b] = [visit(), visit()];
    const names = new Map<object, string>([
      [a, "a"],
      [b, "b"],
    ]);
    let built = 0;
    const { result, rerender } = renderHook(
      ({ v }: { v: object }) =>
        useKeyedState(v, () => {
          built += 1;
          return names.get(v)!;
        }),
      { initialProps: { v: a } },
    );
    rerender({ v: a });
    rerender({ v: a });
    expect(built).toBe(1);
    rerender({ v: b });
    expect(result.current[0]).toBe("b");
    expect(built).toBe(2);
  });
});
