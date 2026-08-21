// The escalation ladder 停止 → 加速停止 → 強制停止 (owner 2026-08-21).
//
// 🔴 THIS FILE'S PREMISE CHANGED IN T-ed79, and the change is the point. It used
// to assert that `stopping` REPLACES the Stop button with Force stop, and that
// `online-awake` renders no force-stop button AT ALL. The first was a mis-click
// hazard — same slot, same position, an irreversible action swapped under the
// finger — and the second stated the protection ("no button") in a way that
// could only survive while the ladder had two rungs.
//
// What actually has to hold is UNCHANGED and is what these tests now assert:
// an owner looking at a member nobody has asked to stop must not be able to KILL
// it from this row. The button is rendered — in its own fixed position, so the
// ladder never shifts under the cursor — but it is disabled, carries no click
// handler, and says why.

import { describe, it, expect, vi } from "vitest";
import { render, fireEvent } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { zh } from "../i18n/locales/zh";
import { MemberActionButtons } from "./MemberActionButtons";

const forceLabel = zh.lifecycle.action["force-stop"];
const accelLabel = zh.lifecycle.action["accelerated-stop"];
const stopLabel = zh.lifecycle.action.stop;

describe("MemberActionButtons", () => {
  it("stopping offers both escalations and invokes neither onStop", () => {
    const onForceStop = vi.fn();
    const onAcceleratedStop = vi.fn();
    const onStop = vi.fn();
    const { getByText } = render(
      <I18nProvider>
        <MemberActionButtons
          status="stopping"
          onStop={onStop}
          onAcceleratedStop={onAcceleratedStop}
          onForceStop={onForceStop}
        />
      </I18nProvider>,
    );
    fireEvent.click(getByText(accelLabel));
    expect(onAcceleratedStop).toHaveBeenCalledTimes(1);
    fireEvent.click(getByText(forceLabel));
    expect(onForceStop).toHaveBeenCalledTimes(1);
    expect(onStop).not.toHaveBeenCalled();
  });

  it("stopping disables Stop rather than swapping it for an escalation", () => {
    const onStop = vi.fn();
    const { getByTestId } = render(
      <I18nProvider>
        <MemberActionButtons
          status="stopping"
          onStop={onStop}
          onAcceleratedStop={vi.fn()}
          onForceStop={vi.fn()}
        />
      </I18nProvider>,
    );
    const stop = getByTestId("member-action-stop");
    expect((stop as HTMLButtonElement).disabled).toBe(true);
    expect(stop.getAttribute("title")).toBe(zh.lifecycle.reason.alreadyStopping);
    fireEvent.click(stop);
    expect(onStop).not.toHaveBeenCalled();
  });

  it("online-awake keeps the graceful Stop and CANNOT reach either escalation", () => {
    const onStop = vi.fn();
    const onAcceleratedStop = vi.fn();
    const onForceStop = vi.fn();
    const { getByText, getByTestId } = render(
      <I18nProvider>
        <MemberActionButtons
          status="online-awake"
          onStop={onStop}
          onAcceleratedStop={onAcceleratedStop}
          onForceStop={onForceStop}
        />
      </I18nProvider>,
    );
    for (const key of ["accelerated-stop", "force-stop"] as const) {
      const button = getByTestId(`member-action-${key}`);
      expect((button as HTMLButtonElement).disabled).toBe(true);
      expect(button.getAttribute("title")).toBe(zh.lifecycle.reason.pressStopFirst);
      fireEvent.click(button);
    }
    expect(onAcceleratedStop).not.toHaveBeenCalled();
    expect(onForceStop).not.toHaveBeenCalled();

    fireEvent.click(getByText(stopLabel));
    expect(onStop).toHaveBeenCalledTimes(1);
  });

  it("renders the three rungs in the owner's escalation order, in both live states", () => {
    // The ORDER is the ruling, and the POSITIONS are what stop a repeat click
    // from landing on a harsher rung than the one just pressed. Read by
    // data-testid rather than by index into the labels, so a renamed label
    // cannot silently pass a reordered ladder.
    for (const status of ["online-awake", "stopping"] as const) {
      const { container, unmount } = render(
        <I18nProvider>
          <MemberActionButtons
            status={status}
            onSpawn={vi.fn()}
            onStop={vi.fn()}
            onAcceleratedStop={vi.fn()}
            onForceStop={vi.fn()}
          />
        </I18nProvider>,
      );
      const ids = Array.from(
        container.querySelectorAll("[data-testid^='member-action-']"),
      ).map((el) => el.getAttribute("data-testid"));
      const ladder = ids.filter((id) =>
        [
          "member-action-stop",
          "member-action-accelerated-stop",
          "member-action-force-stop",
        ].includes(id ?? ""),
      );
      expect(ladder).toEqual([
        "member-action-stop",
        "member-action-accelerated-stop",
        "member-action-force-stop",
      ]);
      unmount();
    }
  });
});
