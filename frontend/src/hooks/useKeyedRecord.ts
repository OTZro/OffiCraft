import { useRef } from "react";

/** 🔴 ONE RECORD PER KEY, REBUILT ONLY WHEN THE KEY REALLY CHANGES (T-48).
 *
 * The same defect shipped four times in this feature (second review F2, third
 * review R3-1, fourth review R4-1/R4-2): a per-conversation flag was left set
 * by a conversation the owner had already left, and the new conversation never
 * loaded — a permanently blank room, with nothing on screen to say so. Every
 * one of those was a hand-written "did the key change? then zero these out"
 * block, and every one of them was missing a field somebody forgot to add.
 *
 * So the list stops being hand-maintained. The caller names ONE record holding
 * every per-key value; `make` is an object literal of the full type, so a field
 * added without a reset value does not compile, and there is no per-field reset
 * line for anybody to forget.
 *
 * Two properties this shape buys, which the reset block could not:
 *  · A closure that captured the record keeps writing to THAT record. An
 *    in-flight async job whose key has since changed settles into an orphan
 *    nobody reads — which is right: its debts died with its key. The reset
 *    block instead zeroed the LIVE record, so a late finally cleared the new
 *    key's latch (R4-1's shape).
 *  · The rebuild is keyed on the KEY, not on how many times a render or effect
 *    ran. Under StrictMode's double-invoke, and under any unrelated re-render,
 *    the record survives — an unconditional rebuild would re-arm a latch behind
 *    a job that already ran (R4-2's shape).
 *
 * Render-phase, per the React docs' "adjusting state when props change"
 * pattern: the record is already the new key's by the time effects run.
 * `make` must be pure — it is invoked during render and may be invoked twice
 * for one commit under StrictMode's double render (once per key, still).
 */
export function useKeyedRecord<T>(key: string, make: (key: string) => T): T {
  const slot = useRef<{ key: string; record: T } | null>(null);
  if (slot.current === null || slot.current.key !== key) {
    slot.current = { key, record: make(key) };
  }
  return slot.current.record;
}
