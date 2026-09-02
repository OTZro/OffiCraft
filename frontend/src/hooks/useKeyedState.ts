import { useCallback, useRef, useState } from "react";

/** 🔴 THE OTHER HALF OF `useKeyedRecord`: PER-VISIT REACT STATE (T-48, R5-1,
 * rebound to the visit in R6-1).
 *
 * `useKeyedRecord` took the hand-written "did the conversation change? then
 * zero these out" list away from the MUTABLE-REF half of the state. The REACT
 * half was left behind as six `setX(...)` lines inside a `peerIdRef` block —
 * a hand-written list with exactly the two holes the ref half used to have:
 *
 *  · Somebody adds a seventh per-conversation `useState` and forgets the
 *    seventh reset line. Nothing fails to compile; nothing goes red.
 *  · A setter obtained by an async job belonging to the PREVIOUS visit still
 *    writes into the CURRENT one after the switch — the shape every review
 *    from F2 through R6-1 has found again. R5-1 is exactly this: an anchor
 *    fetch that ended `unreachable` after the owner had moved on pasted the
 *    old conversation's failure banner onto the new conversation's room.
 *
 * So this hook does for state what the record does for refs:
 *
 *  1. THE VALUE IS REBUILT WHEN THE VISIT REALLY CHANGES — render-phase, per
 *     the React docs' "adjusting state when props change" pattern, so the value
 *     is already the new visit's before any effect runs. There is no reset line
 *     for anybody to forget, because the reset IS the declaration's initial
 *     value.
 *  2. THE SETTER IS BOUND TO THE VISIT IT WAS TAKEN FOR, exactly like a latch
 *     lease handle. A closure that captured the setter on one visit keeps that
 *     setter, and a write it makes after the owner has moved on is DROPPED
 *     rather than landing in the room on screen. Late writers cannot be
 *     enumerated, so they are not asked to check: the setter checks.
 *
 * 🔴 WHY THE PARAMETER IS AN OBJECT AND NOT `member.id` (R6-1). This hook used
 * to take the peer id — a STRING — and both halves above then asked "is it the
 * same PERSON?", when the invariant they exist to enforce is "is it the same
 * VISIT?". A→B→**A** makes those two questions disagree: the string is equal
 * again, so the rebuild does not fire AND the setter guard passes, and the
 * first visit's late failure banner lands on the second visit's screen — with
 * a retry button that does nothing, because this visit has no jump target.
 * Both defences were open at once, in the one scenario `useChat.scrollback`
 * had already ruled reachable-and-must-be-guarded.
 *
 * The fix is to pass a token whose IDENTITY changes on every visit, which is
 * precisely what `useKeyedRecord` already hands back — so callers pass their
 * record: `useKeyedState(session, null)`. One token, one visit, one answer to
 * "is this still mine" for the record half, the state half and the explicit
 * DOM guards alike.
 *
 * ⚠️ What this does NOT cover, and must stay a human decision: whether a given
 * piece of state is per-conversation at all. A per-COMPONENT state that used
 * this hook would be wrongly wiped on every switch, and a per-conversation one
 * declared with plain `useState` still leaks across. See latch-inventory §2.4
 * for the current census of `ChatArea`'s React state and why each one is where
 * it is.
 */
export function useKeyedState<T>(
  visit: object,
  initial: T | (() => T),
): [T, (next: T | ((prev: T) => T)) => void] {
  // `initial` is read only when a slot is built, never on an ordinary render,
  // so an inline object literal at the call site costs nothing.
  const initialRef = useRef(initial);
  initialRef.current = initial;
  // ⚠️ Same wart `useState` itself has: a T that IS a function type would be
  // mistaken for a lazy initializer. None of the callers store a function.
  const build = (): T =>
    typeof initialRef.current === "function"
      ? (initialRef.current as () => T)()
      : initialRef.current;

  const [slot, setSlot] = useState<{ visit: object; value: T }>(() => ({
    visit,
    value: build(),
  }));
  let current = slot;
  if (slot.visit !== visit) {
    current = { visit, value: build() };
    // Render-phase update of THIS component's own state: React re-renders
    // immediately and discards this pass's output, so nothing downstream ever
    // observes the previous visit's value. `current` is returned anyway so the
    // discarded pass is still self-consistent.
    setSlot(current);
  }

  const set = useCallback(
    (next: T | ((prev: T) => T)) => {
      setSlot((s) => {
        // 🔴 THE WHOLE POINT, AND SINCE R6-1 IT IS LOAD-BEARING RATHER THAN
        // REDUNDANT. `visit` here is the one this setter was taken for;
        // `s.visit` is the one on screen. Under the old STRING key the rebuild
        // covered every case this line covered (P4 was an equivalent mutant);
        // bound to the visit it covers one the rebuild cannot — a return to the
        // SAME conversation, where the rebuild has already happened and would
        // not fire again for the late write.
        if (s.visit !== visit) return s;
        const value =
          typeof next === "function" ? (next as (prev: T) => T)(s.value) : next;
        // 🔴 A NO-OP WRITE MUST STAY A NO-OP. Wrapping the value in a slot
        // object costs the bail-out `useState` gives for free — and several
        // callers here re-assert the same value on every commit
        // (`setLatestInView(distance <= AT_LATEST_PX)` from the scroll
        // reactor). Returning a fresh object each time turns those into an
        // endless render loop; measured as a 4GB OOM in ChatArea.image.
        return Object.is(s.value, value) ? s : { visit, value };
      });
    },
    [visit],
  );

  return [current.value, set];
}
