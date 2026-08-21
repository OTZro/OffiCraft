// The escalation ladder 停止 → 加速停止 → 強制停止 (owner 2026-08-21).
//
// 🔴 THIS FILE'S PREMISE CHANGED TWICE, and the second change is the one that
// stands. It first asserted that `stopping` REPLACES 停止 with 強制停止 (a
// mis-click hazard: same slot, irreversible action swapped under the finger).
// It then asserted the fixed three-slot ladder with the unreachable rungs
// DISABLED IN PLACE. Owner 2026-08-21 overruled that: 「不是一開始就顯示三個按鈕」
// 「按了才出現」— a rung the owner has not unlocked is ABSENT, not greyed out.
//
// So the assertions below are two-sided on purpose. Absent where the condition
// does not hold (`queryByTestId(...)` is null — NOT "disabled"), present where
// it does. A build that renders all three from the start fails the absence
// checks; a build that never reveals them fails the presence checks.
//
// And the condition is NOT "the owner pressed 停止". Owner: 「應該說已經觸發軟
// 下線的人可以被觸發加速下線」 — a wind-down the SYSTEM opened at a context
// threshold counts, and that world is `refocusSince` + `refocusOp` with
// presence still plain `online`. The four fixtures below are exactly those four
// worlds, and the SAME button reads differently in each.

import { describe, it, expect, vi, afterEach } from "vitest";
import { render, fireEvent, act } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { zh } from "../i18n/locales/zh";
import {
  MemberActionButtons,
  stopLadderStageOf,
  LADDER_ARM_MS,
  type StopLadderFacts,
  type StopLadderStage,
} from "./MemberActionButtons";

const forceLabel = zh.lifecycle.action["force-stop"];
const accelLabel = zh.lifecycle.action["accelerated-stop"];
const stopLabel = zh.lifecycle.action.stop;

/** The four worlds the ruling distinguishes, as the wire actually shapes them.
 * They are deliberately NOT four spellings of the same row — presence, the
 * owner's intent and the CAUSE all differ, so a predicate that collapses any
 * two of them cannot pass this table. */
const WORLDS: {
  name: string;
  facts: StopLadderFacts;
  stage: StopLadderStage;
  /** The visual status the panels derive from the same presence word. */
  status: "online-awake" | "stopping";
}[] = [
  {
    // Awake, nobody has asked for anything. One rung exists.
    name: "online, nothing winding down",
    facts: { lifecycle: "online", desiredState: "online", refocusSince: null, refocusOp: "" },
    stage: "none",
    status: "online-awake",
  },
  {
    // The owner pressed 停止: desired_state=offline + a live session ⇒ presence
    // `stopping`. This is the 下線 arm.
    name: "soft wind-down the OWNER opened with 停止",
    facts: { lifecycle: "stopping", desiredState: "offline", refocusSince: null, refocusOp: "" },
    stage: "soft",
    status: "stopping",
  },
  {
    // 🔴 The world the ruling was CORRECTING for. Context pressure crossed the
    // FIRST threshold, so the server stamped a refocus epoch by itself —
    // desired_state is still online and PresenceState therefore projects plain
    // `online`. Presence alone cannot tell this apart from the first row, which
    // is why presence alone is not the test.
    name: "soft wind-down the SYSTEM opened at the context threshold",
    facts: {
      lifecycle: "online",
      desiredState: "online",
      refocusSince: 1_770_000_000,
      refocusOp: "context_notice",
    },
    stage: "soft",
    status: "online-awake",
  },
  {
    // On the clock: refocus_op is one of winddownKindFor's two clocked causes.
    name: "accelerated arm",
    facts: {
      lifecycle: "stopping",
      desiredState: "offline",
      refocusSince: null,
      refocusOp: "accelerated_stop",
    },
    stage: "accelerated",
    status: "stopping",
  },
];

function renderRow(
  world: (typeof WORLDS)[number],
  over: Partial<React.ComponentProps<typeof MemberActionButtons>> = {},
) {
  return render(
    <I18nProvider>
      <MemberActionButtons
        status={world.status}
        stage={stopLadderStageOf(world.facts)}
        onSpawn={vi.fn()}
        onStop={vi.fn()}
        onAcceleratedStop={vi.fn()}
        onForceStop={vi.fn()}
        {...over}
      />
    </I18nProvider>,
  );
}

afterEach(() => {
  vi.useRealTimers();
});

describe("stopLadderStageOf", () => {
  it("reads each wind-down world as its own stage", () => {
    for (const w of WORLDS) {
      expect(stopLadderStageOf(w.facts), w.name).toBe(w.stage);
    }
  });

  it("counts the system-opened wind-down, not just the one the owner pressed", () => {
    // Owner 2026-08-21: 「應該說已經觸發軟下線的人可以被觸發加速下線」. Same
    // presence word as a plain awake member — only refocus_since separates them.
    const awake = { lifecycle: "online", desiredState: "online" };
    expect(stopLadderStageOf(awake)).toBe("none");
    expect(stopLadderStageOf({ ...awake, refocusSince: 1_770_000_000, refocusOp: "context_notice" }))
      .toBe("soft");
  });

  it("reads the worker view model's presence field under its own name", () => {
    // 外包 keeps the wire name `presence`; 正職 renames it `lifecycle`. Both
    // panels call THIS function, so both spellings have to land on one answer.
    expect(stopLadderStageOf({ presence: "stopping", desiredState: "offline" })).toBe("soft");
  });

  it("never reports the accelerated arm on an actor with no wind-down open", () => {
    // A stale cause with nothing open must not conjure 強制停止 out of nothing.
    expect(
      stopLadderStageOf({ lifecycle: "online", desiredState: "online", refocusOp: "accelerated_stop" }),
    ).toBe("none");
  });
});

describe("MemberActionButtons", () => {
  it("reveals one more rung per stage and renders NO unreachable rung", () => {
    // 🔴 THE MUTANT GUARD. Making the rungs unconditional (all three from the
    // first render, however they are styled) fails the null checks here — this
    // is the assertion that goes red, by name.
    const expected: Record<StopLadderStage, string[]> = {
      none: ["member-action-stop"],
      soft: ["member-action-stop", "member-action-accelerated-stop"],
      accelerated: [
        "member-action-stop",
        "member-action-accelerated-stop",
        "member-action-force-stop",
      ],
    };
    const LADDER = expected.accelerated;
    for (const world of WORLDS) {
      const { container, queryByTestId, unmount } = renderRow(world);
      const shown = Array.from(
        container.querySelectorAll("[data-testid^='member-action-']"),
      )
        .map((el) => el.getAttribute("data-testid") ?? "")
        .filter((id) => LADDER.includes(id));
      // Present, and in the owner's order.
      expect(shown, `${world.name}: rungs revealed`).toEqual(expected[world.stage]);
      // Absent — not disabled. 「按了才出現」 is a statement about existence.
      for (const id of LADDER.filter((k) => !expected[world.stage].includes(k))) {
        expect(queryByTestId(id), `${world.name}: ${id} must not be in the DOM`).toBeNull();
      }
      unmount();
    }
  });

  it("only 加速停止 and 強制停止 can escalate, and only where they are revealed", () => {
    const onAcceleratedStop = vi.fn();
    const onForceStop = vi.fn();
    const accelerated = WORLDS[3];
    const { getByText } = renderRow(accelerated, { onAcceleratedStop, onForceStop });
    fireEvent.click(getByText(accelLabel));
    expect(onAcceleratedStop).toHaveBeenCalledTimes(1);
    fireEvent.click(getByText(forceLabel));
    expect(onForceStop).toHaveBeenCalledTimes(1);
  });

  it("keeps 停止 in its own slot once the wind-down it opened is running", () => {
    // Guard 1 against the double click: the pressed rung is spent, not removed,
    // so the rung that appears takes a NEW slot and never the one the finger
    // was aiming at. It says why rather than going silent.
    const onStop = vi.fn();
    const { getByTestId } = renderRow(WORLDS[1], { onStop });
    const stop = getByTestId("member-action-stop");
    expect((stop as HTMLButtonElement).disabled).toBe(true);
    expect(stop.getAttribute("title")).toBe(zh.lifecycle.reason.alreadyStopping);
    fireEvent.click(stop);
    expect(onStop).not.toHaveBeenCalled();
  });

  it("leaves 停止 live on a wind-down the owner never asked for", () => {
    // The system-opened arm is still desired-online: 停止 there is a REAL,
    // different action (ask for the shutdown), not a repeat of one.
    const onStop = vi.fn();
    const { getByText } = renderRow(WORLDS[2], { onStop });
    fireEvent.click(getByText(stopLabel));
    expect(onStop).toHaveBeenCalledTimes(1);
  });

  it("holds a rung that JUST appeared inert until it arms", () => {
    // Guard 2 against the double click, and the load-bearing one: slot
    // separation is only as good as the layout, and the layout reflows.
    vi.useFakeTimers();
    const onAcceleratedStop = vi.fn();
    const { getByTestId, rerender } = render(
      <I18nProvider>
        <MemberActionButtons status="online-awake" stage="none" onStop={vi.fn()}
          onAcceleratedStop={onAcceleratedStop} onForceStop={vi.fn()} />
      </I18nProvider>,
    );
    rerender(
      <I18nProvider>
        <MemberActionButtons status="stopping" stage="soft" onStop={vi.fn()}
          onAcceleratedStop={onAcceleratedStop} onForceStop={vi.fn()} />
      </I18nProvider>,
    );
    const accel = getByTestId("member-action-accelerated-stop");
    expect((accel as HTMLButtonElement).disabled).toBe(true);
    expect(accel.getAttribute("title")).toBe(zh.lifecycle.reason.justAppeared);
    fireEvent.click(accel);
    expect(onAcceleratedStop).not.toHaveBeenCalled();

    act(() => {
      vi.advanceTimersByTime(LADDER_ARM_MS + 1);
    });
    expect((getByTestId("member-action-accelerated-stop") as HTMLButtonElement).disabled).toBe(false);
    fireEvent.click(getByTestId("member-action-accelerated-stop"));
    expect(onAcceleratedStop).toHaveBeenCalledTimes(1);
  });

  it("arms immediately when the panel OPENS on a rung that was already there", () => {
    // Nothing appeared under anybody's finger — the owner navigated to it. A
    // window here would be a delay with no hazard to prevent.
    const onForceStop = vi.fn();
    const { getByText } = renderRow(WORLDS[3], { onForceStop });
    fireEvent.click(getByText(forceLabel));
    expect(onForceStop).toHaveBeenCalledTimes(1);
  });

  it("offers no rung at all where there is no live session to wind down", () => {
    for (const status of ["offline", "stopped", "waking"] as const) {
      const { queryByTestId, unmount } = render(
        <I18nProvider>
          <MemberActionButtons status={status} stage="accelerated" onSpawn={vi.fn()}
            onCancel={vi.fn()} onStop={vi.fn()} onAcceleratedStop={vi.fn()} onForceStop={vi.fn()} />
        </I18nProvider>,
      );
      for (const key of ["stop", "accelerated-stop", "force-stop"]) {
        expect(queryByTestId(`member-action-${key}`), `${status}/${key}`).toBeNull();
      }
      unmount();
    }
  });
});
