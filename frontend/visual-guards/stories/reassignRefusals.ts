// The reassign refusal sentences the CT guard measures (T-b9f6).
//
// Kept out of the story module on purpose: a CT story file that exports both a
// component and a value trips playwright-ct's component transform. Same split
// as avatarKindImages.ts.

/** Verbatim from server/ocserverd/api_tasks.go — the 一般正職 member-target 403.
 *
 * ⚠️ It is A LONG one, not provably THE longest: an earlier version claimed the
 * latter and independent review measured it at 105 characters against ~102 for
 * the 發包-gate refusal — a 3-character margin — while `target member '<id>' is
 * not an active roster member` interpolates a caller-supplied id with no length
 * bound at all, so the true maximum is unbounded. What the guard needs is a
 * sentence long enough to wrap at 390px, which this is; nothing downstream
 * depends on it being maximal, and no assertion pretends otherwise. */
export const LONGEST_REFUSAL =
  "only the owner or an admin agent may reassign a task to another member; 發包 to an outsource worker instead";
