// dtoParity.ts — WHAT A ONE-ITEM REFETCH CANNOT SERVE (T-8115 follow-up).
//
// T-8115 made an SSE delta re-read the ONE entity it named (`GET /{id}`) instead
// of re-downloading the whole list. That is only sound where the single-item
// response is a SUPERSET of the list row for every field the screen renders.
// Two of the three endpoints it used are NOT:
//
//  - `GET /api/members/{id}` — FIXED at the source (T-8115 review, team-lead
//    approved 2026-08-01). It used to hand `newMemberDTO` a literal 0 for
//    `unread_count` while `GET /api/members` computed the real number, so
//    re-reading one member ZEROED the roster badge the delta was announcing — the
//    value could only ever go DOWN through that path. Both handlers now call the
//    SAME `unreadCountsForRequest` (`server/ocserverd/api_helpers.go`), so the
//    per-item path is faithful again. No schema change was involved: MemberDTO has
//    always declared the field. Pinned server-side by
//    `api_members_unread_parity_test.go` (single vs list, on the response body).
//  - `GET /api/tasks/{id}` — `dep_tasks` IS NOT ON THE WIRE AT ALL. The frozen
//    spec declares it on `TaskListItemDTO` only, never on `TaskDTO`
//    (`spec/openapi.json`; `toTask()` in `api/mappers.ts` therefore sets no
//    `depTasks`, while `toTaskListItem()` passes it through verbatim). Absence is
//    not `[]`: the card renders "nobody resolved this dep" vs "查無此任務"
//    DIFFERENTLY on purpose (`components/TaskCard.tsx`), so a per-item refetch
//    silently degrades every dep row on that card to a bare short id.
//  - `GET /api/outsource-workers/{id}` — SAFE, and the reason is worth copying:
//    the single-item handler calls the SAME `projectWorker` with the same real
//    `unread[worker.ID]` as the list handler
//    (`server/ocserverd/api_outsource.go`). Nothing is dropped.
//
// The remaining gap cannot be closed on the client at all: `dep_tasks` is a field
// the frozen wire does not carry, so closing it is an additive spec change and is
// waiting on the owner. Until then `useTasks` re-pulls its list — the request
// count is the same either way (one GET), only the payload is bigger.
//
// 🔴 THE POINT OF THIS FILE. The regression shipped green because the hook
// tests' hand-rolled fake `getMember` / `getTask` returned the LIST row
// unchanged — the fake was more generous than the real server, so the value
// assertions were measured against a wire that does not exist. `api/mock.ts` had
// it right all along (its `getTask` explicitly drops `depTasks`). So the gaps
// live here, ONE place, and `dtoParity.test.ts` pins this table against the mock
// adapter: make either of them generous again and that test goes red.

/** The fields a single-item GET does NOT carry, per list-bearing endpoint. */
export const PER_ITEM_DTO_GAPS = {
  /** `GET /api/members/{id}`: same computation as the list — nothing dropped. */
  member: [] as string[],
  /** `GET /api/tasks/{id}`: dep_tasks is not a field of TaskDTO. */
  task: ["depTasks"],
  /** `GET /api/outsource-workers/{id}`: same projection as the list. */
  outsourceWorker: [] as string[],
} as const;

export type PerItemKind = keyof typeof PER_ITEM_DTO_GAPS;

/** True when a one-item refetch can stand in for a list re-pull on this
 * endpoint — i.e. nothing the list row carries is lost. */
export function perItemRefetchIsFaithful(kind: PerItemKind): boolean {
  return PER_ITEM_DTO_GAPS[kind].length === 0;
}

/**
 * Project a LIST row down to what the SINGLE-ITEM endpoint would really return.
 *
 * Test support, deliberately shipped next to the table it reads: a fake that
 * answers `GET /{id}` out of list data is exactly the mistake this file exists
 * to stop, so the fakes go through here instead of hand-copying the row. The
 * gapped fields are dropped the way the wire drops them — `unreadCount` back to
 * its `default: 0` (MemberDTO declares the field, the handler just never fills
 * it), `depTasks` to `undefined` (the field is absent from TaskDTO).
 */
export function projectSingleItem<T extends Record<string, unknown>>(
  kind: PerItemKind,
  listRow: T
): T {
  const out = { ...listRow } as Record<string, unknown>;
  for (const field of PER_ITEM_DTO_GAPS[kind]) {
    // MemberDTO declares unread_count with `default: 0`, so the honest stand-in
    // for "not computed" is 0, not absence. Everything else in the table is a
    // field the wire does not carry at all.
    out[field] = field === "unreadCount" ? 0 : undefined;
  }
  return out as T;
}
