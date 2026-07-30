// lib/runtime.ts — client-side join of live session telemetry onto a roster
// member.
//
// A member DTO carries NO runtime telemetry: `contextPct` / `estimatedCost` are
// honest-null (see mappers.toMember). The real source is the monitoring session
// — the SAME feed the Monitor page's "AI 會話" rows read (GET /api/monitoring →
// MonitoringDTO.sessions). The member-detail panel opened from either page must
// show the SAME value the monitor row shows, so we join from that ONE source
// (never a second, divergent one), matched by the stable member id.
//
// Honest: if there is no matching session (or its field is null/empty) we pass
// the member's own value through unchanged — we never fabricate a number.
// `machine` / `account` are joined the same way (the detail panel's runtime
// header reads both): an empty "" session field falls back to the member's own.
// `bankedCost` (persistent cumulative cost) is joined the same way as the live
// `estimatedCost` — via `??`, since 0 is a valid banked value.

import type { Member, MonSessionView } from "../types";

export function joinSessionRuntime(
  member: Member,
  sessions: MonSessionView[]
): Member {
  const s = sessions.find((x) => x.id === member.id);
  if (!s) return member;
  return {
    ...member,
    machine: s.machine || member.machine,
    account: s.account || member.account,
    contextPct: s.contextPct ?? member.contextPct,
    compactionCount: s.compactionCount ?? member.compactionCount,
    estimatedCost: s.cost ?? member.estimatedCost,
    bankedCost: s.bankedCost ?? member.bankedCost,
    // The REPORTED effort has no member wire field at all — the session is its
    // only source, so it is joined here rather than falling back to
    // `member.effort` (that one is the owner's launch intent; showing it as the
    // running state is the exact lie this join exists to stop).
    actualEffort: s.effort || member.actualEffort,
  };
}

/** The telemetry row belonging to one agent id. Outsource workers ride the SAME
 * `sessions` array as salaried members (their rows carry the `ow-` id), so both
 * kinds resolve their live model / effort / context / cost through this one
 * lookup. `undefined` = nothing reported under that id — the callers render the
 * honest dash, NEVER the configured launch value. */
export function findSessionFor(
  id: string,
  sessions: MonSessionView[]
): MonSessionView | undefined {
  return sessions.find((s) => s.id === id);
}

/** Whether this session is demonstrably REPORTING telemetry right now.
 *
 * Why it exists: a blank effort cell has two very different causes that look
 * identical on screen — "nothing has reported yet" and "something is reporting
 * and this one field never arrives". The second is a defect (the agent's effort
 * was silently absent from the telemetry payload for a long time and nobody
 * could see it); the first is normal. A session that is online AND has landed
 * at least one other telemetry-only value is provably reporting, so a missing
 * effort there is a statement, not an absence. */
export function isReportingTelemetry(s: MonSessionView | undefined): boolean {
  if (!s || s.status !== "online") return false;
  return s.contextPct != null || s.cost != null || s.account !== "";
}
