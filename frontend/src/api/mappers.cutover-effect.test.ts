// wire→view narrowing of `cutover_effect` — whether the anchor cutover actually
// reached the processes that carry agents.
//
// The trap here is one notch sharper than `warden_shape`'s: "unproven" SOUNDS
// like a fallback and is not one. It is a REPORTED verdict — the machine ran
// the check and could not settle it — so narrowing an unrecognised string to it
// would assert a check that may never have run. And narrowing toward
// "effective" would re-create the defect this field exists to retire: a machine
// whose cutover had not taken effect reading as healthy.
//
// Both mappers are covered — the registry row (toMachine) and the monitoring
// projection (toMonitoring → toMonMachine) — because one narrowing and the
// other passing the raw string through would put two disagreeing answers about
// the same machine on the same screen.

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

const monEffect = (over: Partial<WireMonMachineRow> = {}) =>
  toMonitoring({ sessions: [], accounts: [], machines: [wireMonMachine(over)] })
    .machines[0].cutoverEffect;

describe("toCutoverEffect", () => {
  it("carries each legal verdict through both mappers verbatim", () => {
    for (const legal of ["effective", "not_effective", "unproven"] as const) {
      expect(
        toMachine(wireMachine({ cutover_effect: legal })).cutoverEffect
      ).toBe(legal);
      expect(monEffect({ cutover_effect: legal })).toBe(legal);
    }
  });

  it("narrows anything unrecognised to null rather than to a verdict", () => {
    // Every one of these is a string a future server, a proxy, or a typo could
    // put on the wire. None of them is evidence of anything, so none may
    // survive as a verdict — least of all as the green one.
    for (const junk of [
      "EFFECTIVE",
      "effective ",
      "not-effective",
      "noteffective",
      "proven",
      "true",
      "",
      "unknown",
    ]) {
      expect(
        toMachine(wireMachine({ cutover_effect: junk })).cutoverEffect,
        `wire value ${JSON.stringify(junk)} survived the narrowing`
      ).toBeNull();
      expect(monEffect({ cutover_effect: junk })).toBeNull();
    }
  });

  it("maps an ABSENT field to null, which is not the reported 'unproven'", () => {
    const absent = wireMachine();
    delete (absent as Record<string, unknown>).cutover_effect;
    expect(toMachine(absent).cutoverEffect).toBeNull();
    expect(toMachine(wireMachine({ cutover_effect: null })).cutoverEffect).toBeNull();

    const absentMon = wireMonMachine();
    delete (absentMon as Record<string, unknown>).cutover_effect;
    expect(
      toMonitoring({ sessions: [], accounts: [], machines: [absentMon] })
        .machines[0].cutoverEffect
    ).toBeNull();

    // The point of the field, at the value seam: "this build ran and could not
    // prove it" and "this build never reported" come out as two different
    // values, and the UI shows two different sentences for them.
    expect(
      toMachine(wireMachine({ cutover_effect: "unproven" })).cutoverEffect
    ).not.toBe(toMachine(absent).cutoverEffect);
  });

  it("keeps the shape and the effect independent", () => {
    // The pair being able to DISAGREE is the whole reason the second field
    // exists — "anchor" plus "not_effective" is the state the incident actually
    // had, and a mapper that derived one from the other would erase it.
    const row = toMachine(
      wireMachine({ warden_shape: "anchor", cutover_effect: "not_effective" })
    );
    expect(row.wardenShape).toBe("anchor");
    expect(row.cutoverEffect).toBe("not_effective");
  });
});
