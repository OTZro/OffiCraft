/**
 * The identity card's action row, for BOTH actor kinds — MemberDetailPanel
 * (正職) and WorkerDetailPanel (外包) render this one component, which is what
 * keeps the labels, the order and WHICH RUNGS EXIST from drifting apart.
 *
 * Two inputs decide the row: `status` (the five-state presence, a LOCAL UI-only
 * union — not the frozen `MemberStatus` contract) picks the non-ladder buttons,
 * and `stage` says how far up 停止 → 加速停止 → 強制停止 this actor already is.
 * Both panels derive `stage` from `stopLadderStageOf` over the same wire fields.
 *
 * Honesty (no dead affordances): every action handler is an OPTIONAL prop. A
 * button is interactive ONLY when its handler is supplied; otherwise it renders
 * disabled and carries no click behaviour. A rung that is not REACHABLE yet is a
 * different thing again — it is not rendered at all (owner 2026-08-21
 * 「按了才出現」).
 */
import { useEffect, useState } from "react";
import { useI18n } from "../i18n";
import type { LifecycleVisualStatus } from "./LifecycleDot";

/** UI-only lifecycle status (shares the five-state visual union). */
export type LifecycleStatus = LifecycleVisualStatus;

type ActionKey = "spawn" | "cancel" | "stop" | "accelerated-stop" | "force-stop";

/** How far up the escalation 停止 → 加速停止 → 強制停止 this actor already is.
 *
 * 🔴 OWNER 2026-08-21, and it REPLACES the fixed three-slot ladder this file
 * shipped hours earlier: 「加速停止應該是已經按下停止的狀態下才可以按，強制停止應該
 * 是加速停止按下的狀態才可以按，不是一開始就顯示三個按鈕」「按了才出現」. A rung
 * that is not reachable yet is NOT rendered disabled — it is not rendered.
 *
 *  - `none`         — nothing is winding down. Only 停止 exists.
 *  - `soft`         — a soft wind-down is OPEN. 加速停止 appears.
 *  - `accelerated`  — that wind-down is on the clock. 強制停止 appears.
 */
export type StopLadderStage = "none" | "soft" | "accelerated";

/** The observable wind-down facts both actor kinds carry on the wire. Members
 * and outsource workers publish the SAME four (mappers' `toMember` /
 * `toOutsourceWorker`), which is what lets one function answer for both. */
export interface StopLadderFacts {
  /** Wire `presence`, the five-state word — under the name the MEMBER view
   * model gives it. */
  lifecycle?: string;
  /** The same wire `presence`, under the name the WORKER view model gives it.
   * One field, two view-model spellings (`toMember` renames it, the outsource
   * mapper does not); accepting both is what lets one call site per panel pass
   * its own object and still be the same reading. */
  presence?: string;
  /** Wire `desired_state` ("offline" once the owner has asked for a stop). */
  desiredState?: string;
  /** Wire `refocus_since` > 0 → epoch, else null. */
  refocusSince?: number | null;
  /** Wire `refocus_op` — the CAUSE of the open wind-down, "" when none. */
  refocusOp?: string;
}

/** The two `refocus_op` causes the server puts on a clock — winddownKindFor's
 * membership test, verbatim (`context_high` = the SECOND context threshold the
 * system opens by itself; `accelerated_stop` = the owner pressed the button).
 * They are exactly the arm 強制停止 is allowed to appear on. */
const CLOCKED_OPS = new Set(["context_high", "accelerated_stop"]);

/**
 * Where on the ladder this actor stands — the ONE reading of the ruling, shared
 * by 正職 and 外包 so the two panels can never disagree about which rung exists.
 *
 * 🔴 WHY THESE FIELDS (implementer's reading — the owner ruled the BEHAVIOUR,
 * not the column):
 *
 *  - "已經觸發軟下線" is NOT "the owner pressed 停止". He said so explicitly
 *    (「應該說已經觸發軟下線的人可以被觸發加速下線」), and the difference is a
 *    whole arm: a wind-down the SYSTEM opens at a context threshold stamps
 *    `refocus_since` with `desired_state` still online, so `PresenceState`
 *    projects it as plain `online` — presence alone cannot see it, and a
 *    `status === "stopping"` test (what this file used to do) hides 加速停止 on
 *    exactly the members the owner named. So the test is the SERVER'S OWN
 *    acceptance gate, mirrored: `HandleAcceleratedStop…` takes the 下線 arm
 *    (`desired_state == offline && stopping_since > 0`) or the 換手 arm
 *    (`refocus_since > 0`). `stopping_since` is deliberately not on the wire, so
 *    the cockpit reads the projection derived from it — presence `stopping` —
 *    which is the same substitution `api/mock.ts` already makes for its 409 gate.
 *
 *  - "已進入加速臂" is `refocus_op`, not `refocus_deadline`. Both answer the same
 *    question today (the deadline is DERIVED from the op through
 *    `winddownKindFor`), but the op is the CAUSE and the deadline folds in two
 *    unrelated conditions (`forcedEpochLive`, a zero `stopping_since`) that would
 *    silently change which buttons exist for reasons that have nothing to do with
 *    which arm we are on. The wire doc names the op as the field that "tells the
 *    two apart"; this is that sentence, executed.
 *
 * Monotone by construction: `accelerated` is only ever returned from inside the
 * open-wind-down branch, so 強制停止 can never appear on an actor that has no
 * wind-down at all — whatever a stale `refocus_op` says.
 */
export function stopLadderStageOf(f: StopLadderFacts): StopLadderStage {
  const presence = f.lifecycle ?? f.presence;
  const open =
    (f.desiredState === "offline" && presence === "stopping") ||
    (f.refocusSince ?? 0) > 0;
  if (!open) return "none";
  return CLOCKED_OPS.has(f.refocusOp ?? "") ? "accelerated" : "soft";
}

/** The rungs that EXIST at each stage, in the owner's order. Growing to the
 * RIGHT is load-bearing, not cosmetic — see LADDER_ARM_MS. */
const LADDER_BY_STAGE: Record<StopLadderStage, ActionKey[]> = {
  none: ["stop"],
  soft: ["stop", "accelerated-stop"],
  accelerated: ["stop", "accelerated-stop", "force-stop"],
};

/**
 * 🔴 THE DOUBLE-CLICK GUARD, and it is the whole reason this component owns a
 * timer instead of staying a pure function of its props.
 *
 * 「按了才出現」 means the escalation grows under the cursor that just pressed
 * the rung below it, and this ladder gets LESS reversible as it grows (強制停止
 * sends nothing and cuts the session dead). Nothing must be able to carry an
 * owner from 停止 to 強制停止 on a burst of clicks. Two things stop it:
 *
 *  1. THE PRESSED RUNG STAYS PUT. 停止 is never removed when the wind-down
 *     opens — it stays in slot 1, spent (disabled, with `alreadyStopping`), so
 *     the rung that appears takes a NEW slot to its right and no slot is ever
 *     recycled into a harsher action. This is the protection the fixed
 *     three-slot version was built around; it survives the ruling intact.
 *  2. A RUNG THAT JUST APPEARED IS INERT. Slot separation is only as good as
 *     the layout, and the layout reflows (`stopping` also gains the 喚醒 rescue,
 *     and at ≤720px the whole cluster re-wraps). So a newly revealed rung
 *     renders disabled for this window and arms itself after it — a click that
 *     was already travelling when the button materialised lands on nothing.
 *
 * COST, stated plainly: an owner who deliberately climbs the ladder waits this
 * long at each step, and a presentational component now holds state. 350ms is
 * chosen to cover a double-click (the platform threshold is ~250–500ms) while
 * staying under the round trip that reveals the rung in the first place, so in
 * practice it is spent, not waited.
 */
export const LADDER_ARM_MS = 350;

/** Destructive actions get the danger-ghost styling. */
const DANGER_ACTIONS = new Set<ActionKey>([
  "stop",
  "accelerated-stop",
  "force-stop",
]);

/** The non-ladder part of each status's button set, in display order. The
 * winding-down states (`stopping` / `waking`) can WEDGE — a member can get stuck
 * `stopping` (still alive, SSE holding, pinned by a stale stop marker) or
 * mid-`waking` if the old stop command never lands (crashed warden, lost
 * signal) — so both ALSO offer Spawn (=wake) as a FORCE-REVIVE rescue, backed by
 * the same activate endpoint that unconditionally clears the winding-down
 * anchors. Spawn leads in those states: rescue first.
 * (Refocus is deliberately NOT a header action — it lives with the context cell
 * in MemberDetailPanel. Dismiss is not offered either: owner acceptance removed
 * the UI entry and DELETE /api/members stays a pure backend seam.) */
const PREFIX_SETS: Record<LifecycleStatus, ActionKey[]> = {
  offline: ["spawn"],
  waking: ["cancel", "spawn"],
  "online-awake": [],
  stopping: ["spawn"],
  stopped: ["spawn"],
};

/** Which statuses carry the ladder at all. `offline` / `stopped` / `waking`
 * have no live session to wind down, so none of the three rungs belongs there —
 * `waking` keeps Cancel, which IS its stop. */
const LADDER_STATUSES = new Set<LifecycleStatus>(["online-awake", "stopping"]);

/** The one rung that renders and is deliberately NOT pressable: 停止 once the
 * wind-down it opened is already running. It stays put (guard 1 above) and says
 * why — pressing it again only re-stamps an anchor nothing on screen reflects,
 * and 加速停止 is the way on. */
type LadderReasonKey = "alreadyStopping" | "justAppeared";

// (Refocus was removed as a header action — see MemberDetailPanel's context cell.)

interface MemberActionButtonsProps {
  status: LifecycleStatus;
  /** How far up 停止 → 加速停止 → 強制停止 this actor already is. Both panels
   * derive it from the SAME `stopLadderStageOf` over the SAME wire fields.
   * Defaults to `none`: a caller that knows nothing about wind-downs (the
   * IdentityRealIdStory fixture) gets the un-escalated row, never a kill button.
   */
  stage?: StopLadderStage;
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
  stage = "none",
  onSpawn,
  onCancel,
  onStop,
  onAcceleratedStop,
  onForceStop,
  reasons,
  labels,
}: MemberActionButtonsProps) {
  const { t } = useI18n();

  // The stage the row is allowed to ACT on. It starts wherever the row first
  // mounted — opening a panel on an already-accelerated member arms everything
  // immediately, because nothing appeared under anybody's finger — and then
  // follows `stage` one LADDER_ARM_MS behind, which is the inert window a rung
  // that just materialised spends before it will take a click.
  const [armedStage, setArmedStage] = useState<StopLadderStage>(stage);
  useEffect(() => {
    if (stage === armedStage) return;
    const id = setTimeout(() => setArmedStage(stage), LADDER_ARM_MS);
    return () => clearTimeout(id);
  }, [stage, armedStage]);

  const shown = LADDER_STATUSES.has(status) ? LADDER_BY_STAGE[stage] : [];
  // Set membership, not an ordinal comparison, so a stage that goes DOWN
  // (the wind-down was collected) leaves the smaller surviving row fully live
  // instead of waiting out a window for buttons that were there all along.
  const armed = new Set(
    LADDER_STATUSES.has(status) ? LADDER_BY_STAGE[armedStage] : [],
  );

  const ladderReason: Partial<Record<ActionKey, LadderReasonKey>> = {};
  const handlers: Record<ActionKey, (() => void) | undefined> = {
    spawn: onSpawn,
    cancel: onCancel,
    stop: onStop,
    "accelerated-stop": onAcceleratedStop,
    "force-stop": onForceStop,
  };
  // 停止 is spent once the stop it asked for is visibly running. It is NOT
  // removed: its slot is what keeps 加速停止 from landing where the finger was.
  if (status === "stopping") {
    handlers.stop = undefined;
    ladderReason.stop = "alreadyStopping";
  }
  for (const key of shown) {
    if (!armed.has(key)) {
      handlers[key] = undefined;
      ladderReason[key] = "justAppeared";
    }
  }

  const keys = [...PREFIX_SETS[status], ...shown];

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
        const ladderKey = ladderReason[key];
        const reason = handler
          ? undefined
          : reasons?.[key] ?? (ladderKey ? t.lifecycle.reason[ladderKey] : undefined);
        return (
          <button
            key={key}
            type="button"
            // Address actions by IDENTITY, not by position (review r1 SHOULD-3):
            // PREFIX_SETS puts spawn FIRST only for offline/stopped — in
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
