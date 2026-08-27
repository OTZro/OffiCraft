// T-b0bb — the bar that makes a dead downlink VISIBLE.
//
// The whole point of the component is negative space: it must be absent when
// the stream is healthy, absent while a routine blip is being retried, and
// PRESENT once the page has genuinely stopped receiving. A banner that is
// always there is as useless as one that is never there, so both halves are
// asserted here — and the "still absent during the grace window" case is the
// one that keeps a well-meaning simplification (drop the delay, show it
// instantly) from turning the bar into wallpaper.
//
// Real ConnectionBanner + real i18n; only the api seam is faked, so the
// component is driven through exactly the `subscribeConnection` contract the
// transport implements.

// MEASURED MUTANTS (re-runnable by hand against ConnectionBanner.tsx):
//   replace the grace `setTimeout` with a bare `setShowing(true)`
//     → reddens "stays silent through a SHORT drop"
//   delete the `setShowing(false)` in the non-"connecting" arm
//     → reddens "clears the moment the stream comes back"

import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { render, act } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import type { SseConnectionState } from "../api/adapter";

let push: ((s: SseConnectionState) => void) | null = null;
let current: SseConnectionState = "live";

vi.mock("../api", () => ({
  api: {
    subscribeConnection(cb: (s: SseConnectionState) => void) {
      push = cb;
      cb(current);
      return () => {
        push = null;
      };
    },
  },
}));

import { ConnectionBanner, CONNECTION_BANNER_GRACE_MS } from "./ConnectionBanner";

function mount() {
  return render(
    <I18nProvider>
      <ConnectionBanner />
    </I18nProvider>
  );
}

function set(state: SseConnectionState): void {
  act(() => {
    push?.(state);
  });
}

/** Push time past the grace window and let the state settle. */
function waitOutGrace(): void {
  act(() => {
    vi.advanceTimersByTime(CONNECTION_BANNER_GRACE_MS + 1);
  });
}

beforeEach(() => {
  vi.useFakeTimers();
  current = "live";
  push = null;
});

afterEach(() => {
  vi.useRealTimers();
});

describe("ConnectionBanner", () => {
  it("says NOTHING while the stream is live — a healthy cockpit carries no chrome", () => {
    const { container } = mount();
    waitOutGrace();
    expect(container.querySelector(".connection-banner")).toBe(null);
  });

  it("stays silent through a SHORT drop: the browser's own retry must not strobe a warning at the owner", () => {
    const { container } = mount();
    set("connecting");
    act(() => {
      vi.advanceTimersByTime(CONNECTION_BANNER_GRACE_MS - 1);
    });
    expect(container.querySelector(".connection-banner")).toBe(null);

    set("live");
    waitOutGrace();
    expect(container.querySelector(".connection-banner")).toBe(null);
  });

  it("SHOWS the bar once the page has really stopped receiving, and says what that means for what is on screen", () => {
    const { container, getByRole } = mount();
    set("connecting");
    waitOutGrace();

    const bar = container.querySelector(".connection-banner");
    expect(bar).not.toBe(null);
    // The consequence, not just the fact: "disconnected" alone leaves the owner
    // to guess whether the page in front of him is still trustworthy.
    expect(bar?.textContent).toContain("即時更新已中斷");
    expect(bar?.textContent).toContain("畫面上的內容可能不是最新的");
    // Announced, not merely drawn.
    expect(getByRole("status")).toBe(bar);
  });

  it("clears the moment the stream comes back — recovery is as visible as the loss", () => {
    const { container } = mount();
    set("connecting");
    waitOutGrace();
    expect(container.querySelector(".connection-banner")).not.toBe(null);

    set("live");
    expect(container.querySelector(".connection-banner")).toBe(null);
  });

  it("says nothing when the session is dead: the login wall is already the message", () => {
    const { container } = mount();
    set("unauthorized");
    waitOutGrace();
    expect(container.querySelector(".connection-banner")).toBe(null);
  });

  it("says nothing when idle (logged out / torn down) — no subscriber is not a fault", () => {
    const { container } = mount();
    set("idle");
    waitOutGrace();
    expect(container.querySelector(".connection-banner")).toBe(null);
  });

  it("keeps the manual escape hatch one click away while the automatic one is still trying", () => {
    const reload = vi.fn();
    const original = window.location;
    Object.defineProperty(window, "location", {
      configurable: true,
      value: { ...original, reload },
    });
    const { getByText } = mount();
    set("connecting");
    waitOutGrace();

    act(() => {
      getByText("重新整理").click();
    });
    expect(reload).toHaveBeenCalledTimes(1);

    Object.defineProperty(window, "location", {
      configurable: true,
      value: original,
    });
  });
});
