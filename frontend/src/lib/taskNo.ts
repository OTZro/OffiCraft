// The task "number" shown in the UI IS the task id — there is no projection
// left to mirror. This helper survives only as the ONE named place the rule is
// stated on the frontend, matching `TaskNo` in server/ocserverd/domain.go.
//
// HISTORY, because the shape of this file only makes sense with it: the number
// used to be the first four hex chars after the "t-" prefix (kyle ruling H3 —
// display-only, collisions accepted). Owner ruled on 2026-08-25 that the UI
// should show the long identifier instead 「不用讓他吃短碼,讓我們顯示長碼」,
// and 「Make it simple, no need complicated mechanism unless my approval」.
//
// An intermediate version returned "T-" + the whole hex. It did not survive
// review: task lookup is byte-exact (`WHERE id = ?`, no COLLATE NOCASE), so a
// number displayed as "T-72dd79b666d0" against an id of "t-72dd79b666d0" still
// 404s when pasted back. Re-casing one character bought nothing, so nothing is
// re-cased now.
//
// WHY the fallback that made this file necessary still needs it: when a dep
// cannot be resolved, the row has no server-supplied `task_no`. It prints the
// id — which is now exactly what every resolved row prints too, so the two
// surfaces agree by construction rather than by two implementations staying
// in step. `task_no` remains a field the server sends; this is the client-side
// stand-in for the one case where it is absent.

/** The display number for a task id: the id, unchanged. */
export function deriveTaskNo(taskId: string): string {
  return taskId;
}
