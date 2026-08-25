// Task display number ("T-<hex>") derived on the frontend — a MIRROR of
// `TaskNo` in server/ocserverd/domain.go (owner ruling 2026-08-25: the number
// carries the WHOLE id, so it names exactly one task and can be pasted back
// into a lookup; this SUPERSEDES kyle ruling H3, under which the number was
// only the first four hex chars and collisions were accepted).
//
// WHY a mirror exists at all: the dep row on a task card normally prints the
// server-supplied `dep.task_no`. When a dep cannot be resolved against the
// frontend's task list, the fallback branches had no `task_no` to print and
// fell back to the RAW id (`t-1d8292a2f8db`). Deriving the number here makes
// the fallback print the same `T-1d8292a2f8db` as every other surface.
//
// WHY the comment matters: this is a rule COPIED ACROSS the wire boundary.
// If someone changes TaskNo on the server, nothing notifies this file — this
// comment and the shared test cases (see taskNo.test.ts) are the only trail
// back to the original.
//
// This is a SYMPTOM-level fix. `task_no` is a pure projection of the id and
// needs no lookup, so the server has no real reason to omit it when a dep
// fails to resolve — returning it unconditionally is the actual cure. That
// touches the wire spec, so it is out of scope here; Kyle has recorded it and
// will handle it separately. When that lands, this helper can RETIRE — please
// delete it rather than treating it as permanent design to build on.

/**
 * Derive the display number ("T-<hex>") from a task id ("t-<hex12>").
 *
 * Mirrors the Go original's one easy-to-miss edge: the prefix is TRIMMED (an
 * id without "t-" is passed through whole, not blindly sliced by two). Nothing
 * is truncated on either side — the number is the id, re-cased.
 */
export function deriveTaskNo(taskId: string): string {
  const hex = taskId.startsWith("t-") ? taskId.slice(2) : taskId;
  return "T-" + hex;
}
