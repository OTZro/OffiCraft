import { useCallback, useRef, useState } from "react";

/** 🔴 THE OTHER HALF OF `useKeyedRecord`: PER-KEY REACT STATE (T-48, R5-1).
 *
 * `useKeyedRecord` took the hand-written "did the conversation change? then
 * zero these out" list away from the MUTABLE-REF half of the state. The REACT
 * half was left behind as six `setX(...)` lines inside a `peerIdRef` block —
 * a hand-written list with exactly the two holes the ref half used to have:
 *
 *  · Somebody adds a seventh per-conversation `useState` and forgets the
 *    seventh reset line. Nothing fails to compile; nothing goes red.
 *  · A setter obtained by an async job belonging to the PREVIOUS key still
 *    writes into the CURRENT one after the switch — the shape every review
 *    from F2 through R5-1 has found again. R5-1 is exactly this: an anchor
 *    fetch that ended `unreachable` after the owner had moved on pasted the
 *    old conversation's failure banner onto the new conversation's room.
 *
 * So this hook does for state what the record does for refs:
 *
 *  1. THE VALUE IS REBUILT WHEN THE KEY REALLY CHANGES — render-phase, per the
 *     React docs' "adjusting state when props change" pattern, so the value is
 *     already the new key's before any effect runs. There is no reset line for
 *     anybody to forget, because the reset IS the declaration's initial value.
 *  2. THE SETTER IS BOUND TO THE KEY IT WAS TAKEN FOR, exactly like a latch
 *     lease handle. A closure that captured the setter under key A keeps that
 *     setter, and a write it makes after the switch is DROPPED rather than
 *     landing in B's room. Late writers cannot be enumerated, so they are not
 *     asked to check: the setter checks.
 *
 * ⚠️ What this does NOT cover, and must stay a human decision: whether a given
 * piece of state is per-conversation at all. A per-COMPONENT state that used
 * this hook would be wrongly wiped on every switch, and a per-conversation one
 * declared with plain `useState` still leaks across. See latch-inventory §2.4
 * for the current census of `ChatArea`'s React state and why each one is where
 * it is.
 */
export function useKeyedState<T>(
  key: string,
  initial: T | (() => T),
): [T, (next: T | ((prev: T) => T)) => void] {
  // `initial` is read only when a record is built, never on an ordinary render,
  // so an inline object literal at the call site costs nothing.
  const initialRef = useRef(initial);
  initialRef.current = initial;
  const build = (): T =>
    typeof initialRef.current === "function"
      ? (initialRef.current as () => T)()
      : initialRef.current;

  const [slot, setSlot] = useState<{ key: string; value: T }>(() => ({
    key,
    value: build(),
  }));
  let current = slot;
  if (slot.key !== key) {
    current = { key, value: build() };
    // Render-phase update of THIS component's own state: React re-renders
    // immediately and discards this pass's output, so nothing downstream ever
    // observes the previous key's value. `current` is returned anyway so the
    // discarded pass is still self-consistent.
    setSlot(current);
  }

  const set = useCallback(
    (next: T | ((prev: T) => T)) => {
      setSlot((s) => {
        // 🔴 THE WHOLE POINT. `key` here is the one this setter was taken for;
        // `s.key` is the one on screen. A late writer from the conversation the
        // owner has left is a no-op instead of a banner in somebody else's room.
        if (s.key !== key) return s;
        const value =
          typeof next === "function" ? (next as (prev: T) => T)(s.value) : next;
        // 🔴 A NO-OP WRITE MUST STAY A NO-OP. Wrapping the value in a slot
        // object costs the bail-out `useState` gives for free — and several
        // callers here re-assert the same value on every commit
        // (`setLatestInView(distance <= AT_LATEST_PX)` from the scroll
        // reactor). Returning a fresh object each time turns those into an
        // endless render loop; measured as a 4GB OOM in ChatArea.image.
        return Object.is(s.value, value) ? s : { key, value };
      });
    },
    [key],
  );

  return [current.value, set];
}
