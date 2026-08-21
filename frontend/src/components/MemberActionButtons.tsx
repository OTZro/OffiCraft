/**
 * Layer-4 handover UI skeleton — PURE PRESENTATION shell.
 *
 * Given a lifecycle status, renders the status-appropriate set of action
 * buttons. This is an ISOLATED component: it is NOT wired to a real member and
 * is NOT connected to the frozen `MemberStatus` data contract — the status
 * union below is a LOCAL, UI-only type. Wiring to MemberDetailPanel happens in
 * a later phase.
 *
 * Honesty (no dead affordances): every action handler is an OPTIONAL prop. A
 * button is interactive ONLY when its handler is supplied; otherwise it renders
 * disabled and carries no click behaviour.
 */
import { useI18n } from "../i18n";
import type { LifecycleVisualStatus } from "./LifecycleDot";

/** UI-only lifecycle status (shares the five-state visual union). */
export type LifecycleStatus = LifecycleVisualStatus;

type ActionKey = "spawn" | "cancel" | "stop" | "accelerated-stop" | "force-stop";

/** Per-status button set (order = display order), aligned to the backend's real
 * five-state presence and its real mutation endpoints (activate / deactivate).
 * (Refocus is deliberately NOT a header action — it lives with the
 * context cell in MemberDetailPanel, so the header never duplicates it. Dismiss
 * is deliberately NOT offered here either — owner acceptance removed the UI
 * entry; DELETE /api/members stays a pure backend seam with no button.) The
 * winding-down states (`stopping` / `waking`) can WEDGE: a
 * member can get stuck in `stopping` (still alive — SSE holding — yet pinned by a
 * stale stop marker: the survived-stop / SSE-reconnect case) or mid-`waking` if the
 * old stop command never lands (crashed warden / lost signal). So both states now
 * ALSO offer Spawn (=wake) as a FORCE-REVIVE rescue path — backed by the same
 * activate endpoint, which unconditionally clears the winding-down anchors and always
 * revives the member ("always revive from a wrong state"). Spawn is listed FIRST in
 * these wedged states = rescue-first. Both live states now carry the FULL
 * escalation ladder 停止 → 加速停止 → 強制停止 (owner 2026-08-21) — see
 * LADDER_REASONS below for which rung is live where, and why the unavailable one
 * is disabled in place instead of removed.
 * 🔴 Since T-7723 the soft window is not bounded by the anchor's age: a member
 * that is still filing context reports stays `stopping` for as long as its
 * session lives, so this set is what the owner sees for that whole time, not for
 * ten minutes. That is exactly why the middle rung had to exist: before it, the
 * only thing that could end a wait of unknown length was an immediate kill.
 * ⚠️ The non-destructive way out is NOT the Spawn button in this set: a
 * `stopping` member maps to tri-state `status: "online"` (api/mappers.ts), the
 * detail panel derives `online` from that, and its Spawn opens the settings
 * dialog whose save both short-circuits on an unchanged form and gates
 * runActivate behind `!online` — so activate is never sent from here. The entry
 * that DOES send it is the chat's 就地喚醒 row (ChatArea `onWake` →
 * `api.activateMember`), which is ungated and clears the anchor server-side.
 * `online-awake` keeps the ordinary graceful
 * Stop (=deactivate); `waking` keeps Cancel (deactivate / cancel-wake) alongside the
 * Spawn rescue. All backed by real endpoints (no dead affordance). */
const BUTTON_SETS: Record<LifecycleStatus, ActionKey[]> = {
  offline: ["spawn"],
  waking: ["cancel", "spawn"],
  "online-awake": ["stop", "accelerated-stop", "force-stop"],
  stopping: ["spawn", "stop", "accelerated-stop", "force-stop"],
  stopped: ["spawn"],
};

/** THE ESCALATION LADDER, and its ORDER is the ruling (owner 2026-08-21:
 * 「停止 → 加速停止 → 強制停止」). All three rungs render in both live states, in
 * this order, in these positions — the rung that is not available right now is
 * DISABLED IN PLACE rather than removed.
 *
 * 🔴 That "in place" is a deliberate choice about MIS-CLICKS, and it is the
 * OPPOSITE of what this file used to do. `stopping` used to REPLACE the Stop
 * button with Force stop — same slot, same position, different and irreversible
 * action — so an owner who pressed 停止 and pressed again in the same place
 * killed the session outright. With a fixed three-slot ladder the second press
 * lands on 加速停止, which is the rung he actually wants, and 強制停止 keeps its
 * own position from the first render so it is never where a repeat click goes.
 *
 * ⚠️ THIS PARTICULAR CHOICE IS NOT AN OWNER RULING. He ruled the ORDER
 * ("停止 → 加速停止 → 強制停止") and asked for the buttons; what a rung does
 * while it is unavailable was not covered, and this is the implementer's answer.
 *
 * Which rungs are live follows what the SERVER will actually accept, so no
 * button here is a dead affordance:
 *  - `online-awake` — nothing is winding down, so 加速停止 and 強制停止 are both
 *    disabled with a reason: the server answers 409 for 加速停止 (it escalates a
 *    wind-down, it does not open one) and 強制停止 belongs after a stop was
 *    asked for, not instead of asking.
 *  - `stopping` — 停止 has been pressed, so IT is the disabled one (pressing it
 *    again re-stamps an anchor and changes nothing the owner can see), and both
 *    escalations are live. Spawn stays first as the rescue path this state has
 *    always offered.
 * A rung whose handler the parent does not supply renders disabled either way —
 * the pre-existing honesty rule, unchanged. */
type LadderReasonKey = "pressStopFirst" | "alreadyStopping";

const LADDER_REASONS: Partial<
  Record<LifecycleStatus, Partial<Record<ActionKey, LadderReasonKey>>>
> = {
  "online-awake": {
    "accelerated-stop": "pressStopFirst",
    "force-stop": "pressStopFirst",
  },
  stopping: { stop: "alreadyStopping" },
};

/** Destructive actions get the danger-ghost styling. */
const DANGER_ACTIONS = new Set<ActionKey>([
  "stop",
  "accelerated-stop",
  "force-stop",
]);

// (Refocus was removed as a header action — see MemberDetailPanel's context cell.)

interface MemberActionButtonsProps {
  status: LifecycleStatus;
  onSpawn?: () => void;
  onCancel?: () => void;
  onStop?: () => void;
  /** 加速停止 — the MIDDLE rung: put the wind-down that is already open on the
   * server's clock and TELL the member. Not a kill, so it needs no confirm; the
   * member is still given the grace and can still finish early. */
  onAcceleratedStop?: () => void;
  /** Force-stop (immediate kill) — the TOP rung. The parent should gate it
   * behind a confirm. */
  onForceStop?: () => void;
  /** Optional per-action hint shown as the button `title` when that action is
   * DISABLED (no handler) — e.g. "no online machine" on spawn. Honest: it only
   * annotates an already-dead affordance, never enables one. */
  reasons?: Partial<Record<ActionKey, string>>;
  /** Optional per-action label override — the in-progress presentation the
   * Monitor machine table uses ("安裝中…" swapped into the disabled install
   * button): the parent swaps e.g. the spawn label to "喚醒中…" while a wake is
   * pending, keeping the feedback INSIDE the button instead of a side note. */
  labels?: Partial<Record<ActionKey, string>>;
}

export function MemberActionButtons({
  status,
  onSpawn,
  onCancel,
  onStop,
  onAcceleratedStop,
  onForceStop,
  reasons,
  labels,
}: MemberActionButtonsProps) {
  const { t } = useI18n();

  const handlers: Record<ActionKey, (() => void) | undefined> = {
    spawn: onSpawn,
    cancel: onCancel,
    stop: status === "stopping" ? undefined : onStop,
    "accelerated-stop":
      status === "stopping" ? onAcceleratedStop : undefined,
    "force-stop": status === "stopping" ? onForceStop : undefined,
  };

  const keys = BUTTON_SETS[status];

  return (
    <div className="member-actions">
      {keys.map((key) => {
        const handler = handlers[key];
        const variant = DANGER_ACTIONS.has(key)
          ? "btn--danger-ghost"
          : "btn--accent-ghost";
        // The parent's own reason wins (it knows things this component cannot,
        // e.g. "no online machine"); the ladder reason is the fallback that
        // explains a rung disabled by the ESCALATION ORDER rather than by a
        // missing handler.
        const ladderKey = LADDER_REASONS[status]?.[key];
        const reason = handler
          ? undefined
          : reasons?.[key] ?? (ladderKey ? t.lifecycle.reason[ladderKey] : undefined);
        return (
          <button
            key={key}
            type="button"
            // Address actions by IDENTITY, not by position (review r1 SHOULD-3):
            // BUTTON_SETS puts spawn FIRST only for offline/stopped — in
            // `waking` the first button is Cancel. A test that reaches for
            // ".member-actions button" therefore silently clicks the wrong
            // action the moment its fixture is not offline.
            data-testid={`member-action-${key}`}
            className={`btn ${variant}`}
            disabled={!handler}
            {...(reason ? { title: reason } : {})}
            {...(handler ? { onClick: handler } : {})}
          >
            {labels?.[key] ?? t.lifecycle.action[key]}
          </button>
        );
      })}
    </div>
  );
}
