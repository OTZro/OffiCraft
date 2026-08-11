// The reassign refusal sentences the CT guard measures (T-b9f6).
//
// Kept out of the story module on purpose: a CT story file that exports both a
// component and a value trips playwright-ct's component transform. Same split
// as avatarKindImages.ts.

/** Verbatim from server/ocserverd/api_tasks.go — the 一般正職 member-target 403,
 * the LONGEST sentence the reassign guards emit, so the layout guard measures
 * the worst case rather than a comfortable one. */
export const LONGEST_REFUSAL =
  "only the owner or an admin agent may reassign a task to another member; 發包 to an outsource worker instead";
