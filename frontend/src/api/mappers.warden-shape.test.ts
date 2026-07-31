// wire→view narrowing of `warden_shape` (anchor cutover) — the field has FOUR
// meanings and the fourth is its own absence, so the seam has to keep them
// apart rather than collapse the two "no shape" ones the way a `??` would.
//
// Both mapper functions are covered: the machine registry row (toMachine) and
// the monitoring projection (toMonitoring → toMonMachine). One of them
// narrowing and the other passing the raw string through would put two
// disagreeing answers on the same screen.

import { describe, it, expect } from "vitest";
import { toMachine, toMonitoring } from "./mappers";
import type { WireMachine, WireMonitoring } from "./wire";

type WireMonMachineRow = NonNullable<WireMonitoring["machines"]>[number];

const wireMachine = (over: Partial<WireMachine> = {}): WireMachine => ({
  machine_id: "warden-mbp5",
  display_name: "MBP5",
  online: true,
  is_self: false,
  bin_status: null,
  ...over,
});

const wireMonMachine = (
  over: Partial<WireMonMachineRow> = {}
): WireMonMachineRow => ({
  machine: "warden-mbp5",
  display_name: "MBP5",
  agents: 0,
  ...over,
});

const monMachineShape = (over: Partial<WireMonMachineRow> = {}) =>
  toMonitoring({ sessions: [], accounts: [], machines: [wireMonMachine(over)] })
    .machines[0].wardenShape;

describe("toWardenShape", () => {
  it("carries each legal value through both mappers verbatim", () => {
    for (const legal of ["anchor", "legacy", "unknown"] as const) {
      expect(toMachine(wireMachine({ warden_shape: legal })).wardenShape).toBe(
        legal
      );
      expect(monMachineShape({ warden_shape: legal })).toBe(legal);
    }
  });

  it("narrows an unrecognised string to null, never inventing 'unknown'", () => {
    // "unknown" is a REPORTED value meaning "the cutover build ran and could
    // not read its own parent". Landing a string we do not recognise there
    // would assert that build is on the machine — evidence we do not have.
    expect(toMachine(wireMachine({ warden_shape: "anchored" })).wardenShape).toBeNull();
    expect(monMachineShape({ warden_shape: "ANCHOR" })).toBeNull();
    expect(monMachineShape({ warden_shape: "" })).toBeNull();
  });

  it("maps an ABSENT field to the not-reported case, distinct from 'unknown'", () => {
    const absent = wireMachine();
    delete (absent as Record<string, unknown>).warden_shape;
    expect(toMachine(absent).wardenShape).toBeNull();
    expect(toMachine(wireMachine({ warden_shape: null })).wardenShape).toBeNull();

    const absentMon = wireMonMachine();
    delete (absentMon as Record<string, unknown>).warden_shape;
    expect(
      toMonitoring({ sessions: [], accounts: [], machines: [absentMon] })
        .machines[0].wardenShape
    ).toBeNull();

    // The point of the ticket, at the type/value seam: the reported "unknown"
    // and the not-reported null are two different values out of the mapper.
    expect(toMachine(wireMachine({ warden_shape: "unknown" })).wardenShape).not.toBe(
      toMachine(absent).wardenShape
    );
  });
});
