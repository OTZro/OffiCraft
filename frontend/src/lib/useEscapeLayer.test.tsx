// useEscapeLayer — the React binding's lifecycle: a surface holds a layer for
// exactly as long as it is on screen, and a nested surface outranks its host
// even when React registers it first.

import { describe, it, expect, vi } from "vitest";
import { useRef, useState } from "react";
import type { ReactNode } from "react";
import { render, fireEvent, screen } from "@testing-library/react";
import { useEscapeLayer } from "./useEscapeLayer";
import { escapeLayerCount } from "./escapeLayers";

/** A dismissible surface with a real root element, so nesting is readable. */
function Surface({
  onEscape,
  active,
  children,
}: {
  onEscape: () => void;
  active?: boolean;
  children?: ReactNode;
}) {
  const ref = useRef<HTMLDivElement>(null);
  useEscapeLayer(onEscape, ref, active);
  return <div ref={ref}>{children}</div>;
}

const escape = () => fireEvent.keyDown(window, { key: "Escape" });

describe("useEscapeLayer", () => {
  it("gives Escape to a nested surface that mounts in the SAME commit as its host", () => {
    // React runs the child's effect BEFORE the parent's, so the nested surface
    // registers first. A deep link that opens a dialog with its preview already
    // up is exactly this shape, and registration order gets it backwards.
    const host = vi.fn();
    const nested = vi.fn();
    render(
      <Surface onEscape={host}>
        <Surface onEscape={nested} />
      </Surface>,
    );

    escape();

    expect(nested).toHaveBeenCalledTimes(1);
    expect(host).not.toHaveBeenCalled();
  });

  it("peels three same-commit layers off innermost first", () => {
    const calls: string[] = [];
    render(
      <Surface onEscape={() => calls.push("outer")}>
        <Surface onEscape={() => calls.push("middle")}>
          <Surface onEscape={() => calls.push("inner")} />
        </Surface>
      </Surface>,
    );

    escape();

    expect(calls).toEqual(["inner"]);
  });

  it("gives Escape to a nested surface opened by a later interaction", () => {
    const host = vi.fn();
    const nested = vi.fn();
    function Host() {
      const [open, setOpen] = useState(false);
      return (
        <Surface onEscape={host}>
          <button onClick={() => setOpen(true)}>open</button>
          {open && <Surface onEscape={nested} />}
        </Surface>
      );
    }
    render(<Host />);

    fireEvent.click(screen.getByText("open"));
    escape();

    expect(nested).toHaveBeenCalledTimes(1);
    expect(host).not.toHaveBeenCalled();
  });

  it("releases its layer on unmount, including a forced one", () => {
    const gone = vi.fn();
    const behind = vi.fn();
    function Host() {
      const [open, setOpen] = useState(true);
      return (
        <Surface onEscape={behind}>
          {/* the host tears the nested surface down without it closing
              itself — a route change, a parent that stopped rendering it */}
          <button onClick={() => setOpen(false)}>tear down</button>
          {open && <Surface onEscape={gone} />}
        </Surface>
      );
    }
    render(<Host />);

    fireEvent.click(screen.getByText("tear down"));
    escape();

    expect(gone).not.toHaveBeenCalled();
    expect(behind).toHaveBeenCalledTimes(1);
  });

  it("holds no layer while inactive and takes the key on activation", () => {
    const host = vi.fn();
    const gated = vi.fn();
    const before = escapeLayerCount();
    function Host() {
      const [active, setActive] = useState(false);
      return (
        <Surface onEscape={host}>
          <button onClick={() => setActive(true)}>activate</button>
          <Surface onEscape={gated} active={active} />
        </Surface>
      );
    }
    render(<Host />);
    expect(escapeLayerCount()).toBe(before + 1);

    escape();
    expect(host).toHaveBeenCalledTimes(1);
    expect(gated).not.toHaveBeenCalled();

    fireEvent.click(screen.getByText("activate"));
    escape();

    expect(gated).toHaveBeenCalledTimes(1);
    expect(host).toHaveBeenCalledTimes(1);
  });

  it("keeps its place when the handler identity changes", () => {
    // The regression this pins: the host re-renders while its nested surface is
    // open (it just took a state update, which is exactly what the old "is a
    // preview open?" flag did). Re-registering on every handler identity would
    // move the host and hand it the key.
    const outcomes: string[] = [];
    function Host() {
      const [ticks, setTicks] = useState(0);
      return (
        <Surface onEscape={() => outcomes.push(`host-${ticks}`)}>
          <button onClick={() => setTicks((n) => n + 1)}>tick</button>
          <Surface onEscape={() => outcomes.push("nested")} />
        </Surface>
      );
    }
    render(<Host />);

    fireEvent.click(screen.getByText("tick"));
    escape();

    expect(outcomes).toEqual(["nested"]);
  });
});
