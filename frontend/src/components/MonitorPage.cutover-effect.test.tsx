// The cutover-effect line on a machine row — Monitor §2.
//
// FOUR states, and the whole ticket is about which of them is allowed to be
// SILENT:
//
//   "effective"     proven: the agents running on that box were started under
//                   the new identity → nothing is shown, and nothing may be
//   "not_effective" proven otherwise → the amber sentence
//   "unproven"      the machine checked and could not tell → a grey line
//   null            the machine has never reported → a grey line of its own
//
// Before this row, the last three were all the same nothing on screen: a
// machine that had never been measured looked exactly like one that had been
// measured and passed. That is the defect, and "no news" reading as "good news"
// is precisely how a machine whose cutover had NOT taken effect stayed green
// for three hours.
//
// So the assertions here are not four "renders X" checks — those stay green
// when two states quietly converge. They are:
//   (1) the healthy state's cell must be EXACTLY what it was with no line at
//       all, character for character (⛔ not one word more);
//   (2) every other state must ADD text to that same cell;
//   (3) the three that speak must be pairwise distinguishable;
//   (4) none of the copy may carry internal vocabulary or tell anyone to
//       restart something.

import { readFile } from "node:fs/promises";
import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, cleanup } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { en } from "../i18n/locales/en";
import { zh } from "../i18n/locales/zh";
import { MonitorPage } from "./MonitorPage";
import type { Member, MachineView, CutoverEffect } from "../types";

const listMembers = vi.fn(async (): Promise<Member[]> => []);
const listMachines = vi.fn(async (): Promise<MachineView[]> => []);

vi.mock("../api", () => ({
  api: {
    listMembers: () => listMembers(),
    listMachines: () => listMachines(),
    getMonitoring: () =>
      Promise.resolve({ accounts: [], sessions: [], machines: [] }),
    listOutsourceWorkers: () => Promise.resolve([]),
    listTasks: () => Promise.resolve([]),
    listTaskTypes: () => Promise.resolve([]),
    getServerSettings: () => Promise.resolve({ outsourceMaxParallel: 0 }),
    subscribeEvents: () => () => {},
  },
}));

const machine = (cutoverEffect: CutoverEffect): MachineView => ({
  machineId: "m-under-test",
  displayName: "m-under-test",
  online: true,
  isSelf: false,
  binStatus: null,
  wardenShape: null,
  cutoverEffect,
  claudeVersion: null,
  claudeCredSource: null,
  claudeSubReadable: null,
});

/** Every state the field has, named for the failure messages below. null is a
 * peer entry, not a default — "this warden never reported" is a fact about the
 * machine, not a gap in the fixture. */
const STATES: ReadonlyArray<readonly [name: string, effect: CutoverEffect]> = [
  ["proven effective", "effective"],
  ["proven not effective", "not_effective"],
  ["checked, could not prove", "unproven"],
  ["never reported (null)", null],
];

/** Render ONE machine in the given state and return the whole text of the cell
 * the line lives in. The cell, not the line: a test that reads only the line's
 * own element cannot see the state that renders no line at all, which is the
 * one this ticket is about. Rendered one at a time so a row can never borrow a
 * sibling row's markup. */
async function cellTextFor(effect: CutoverEffect) {
  listMachines.mockResolvedValue([machine(effect)]);
  render(
    <I18nProvider>
      <MonitorPage />
    </I18nProvider>
  );
  const idBadge = await screen.findByTestId("mon-machine-id");
  const cell = idBadge.closest("td");
  if (cell === null) throw new Error("the machine id badge is not inside a cell");
  const text = (cell.textContent ?? "").trim();
  cleanup();
  return text;
}

describe("MonitorPage cutover-effect line", () => {
  beforeEach(() => {
    listMembers.mockResolvedValue([]);
  });

  it("says nothing at all on a machine that is proven effective", async () => {
    const healthy = await cellTextFor("effective");
    // Every other state's cell is this string PLUS its own sentence, so the
    // healthy one is the common prefix and can be pinned as "the cell with
    // nothing added" without hard-coding what the other cells contain.
    for (const [name, effect] of STATES) {
      if (effect === "effective") continue;
      const speaking = await cellTextFor(effect);
      expect(
        speaking.startsWith(healthy),
        `"${name}" changed the healthy part of the cell rather than adding to it`
      ).toBe(true);
      expect(
        speaking.length,
        `"${name}" added nothing — it is indistinguishable from a healthy machine`
      ).toBeGreaterThan(healthy.length);
    }
  });

  it("gives the three non-effective states three different sentences", async () => {
    const seen: Array<{ name: string; text: string }> = [];
    for (const [name, effect] of STATES) {
      if (effect === "effective") continue;
      seen.push({ name, text: await cellTextFor(effect) });
    }
    for (let i = 0; i < seen.length; i++) {
      for (let j = i + 1; j < seen.length; j++) {
        expect(
          seen[i].text,
          `"${seen[i].name}" and "${seen[j].name}" read identically — a reader ` +
            `cannot tell "we measured and it failed" from "we never measured"`
        ).not.toBe(seen[j].text);
      }
    }
  });

  it("binds each state to ITS OWN sentence, not merely to a different one", async () => {
    // Pairwise inequality above proves the three sentences differ. It does NOT
    // prove each state gets the RIGHT one: swapping "could not tell" with
    // "never reported" keeps all three distinct and passes every other test in
    // this file. Those two imply opposite next steps — go look at that machine
    // vs go ship it the release — so the binding is the substance, and it has
    // to be asserted against the actual copy.
    //
    // I18nProvider defaults to zh (i18n/index.tsx), so zh is what renders here.
    const m = zh.monitor.machine;
    for (const [effect, mine, theirs] of [
      ["not_effective", m.cutoverNotInEffect, m.cutoverUnproven],
      ["unproven", m.cutoverUnproven, m.cutoverUnreported],
      [null, m.cutoverUnreported, m.cutoverUnproven],
    ] as ReadonlyArray<readonly [CutoverEffect, string, string]>) {
      const text = await cellTextFor(effect);
      expect(text, `${effect ?? "null"} did not render its own sentence`).toContain(
        mine
      );
      expect(
        text,
        `${effect ?? "null"} rendered another state's sentence`
      ).not.toContain(theirs);
    }
  });

  it("keeps the proven failure visually apart from the two grey ones", async () => {
    // Colour is the second channel, and it carries the only distinction that
    // matters at a glance: one machine has a problem, the other two merely have
    // no answer. Sharing a class would put them back together.
    listMachines.mockResolvedValue([machine("not_effective")]);
    render(
      <I18nProvider>
        <MonitorPage />
      </I18nProvider>
    );
    const warn = await screen.findByTestId("mon-cutover-warning");
    const warnClass = warn.className;
    cleanup();

    for (const quiet of ["unproven", null] as const) {
      listMachines.mockResolvedValue([machine(quiet)]);
      render(
        <I18nProvider>
          <MonitorPage />
        </I18nProvider>
      );
      const note = await screen.findByTestId("mon-cutover-note");
      expect(
        note.className,
        `the ${quiet ?? "never reported"} line is styled like the proven failure`
      ).not.toBe(warnClass);
      cleanup();
    }
  });

  it("paints the two quiet states grey and the failure amber, in the stylesheet", async () => {
    // The class-name check above only proves the two are DIFFERENT. The ticket's
    // requirement is stronger and directional: the states with no answer get
    // muted grey, and the alarm colour stays reserved for the machine that
    // actually has a problem. Reading the stylesheet is the only place that
    // distinction exists — jsdom does not apply the imported CSS.
    // Read from the repo path rather than through `import.meta.url`: vitest
    // does not hand test modules a file: URL, so resolving against it throws.
    const css = await readFile("src/components/monitor.css", "utf8");
    const ruleFor = (cls: string) => {
      const m = css.match(new RegExp(`\\.${cls}\\s*\\{([^}]*)\\}`));
      if (m === null) throw new Error(`no .${cls} rule in monitor.css`);
      return m[1];
    };
    expect(ruleFor("mon-cutover-note")).toContain("var(--color-text-muted)");
    expect(ruleFor("mon-cutover-note")).not.toContain("var(--color-warn-fg)");
    expect(ruleFor("mon-cutover-warn")).toContain("var(--color-warn-fg)");
  });

  it("renders no cutover element whatsoever on a proven-effective machine", async () => {
    // The cell-text check above would still pass if a zero-width placeholder
    // held the space. Nothing may be there at all.
    listMachines.mockResolvedValue([machine("effective")]);
    render(
      <I18nProvider>
        <MonitorPage />
      </I18nProvider>
    );
    await screen.findByTestId("mon-machine-id");
    expect(screen.queryByTestId("mon-cutover-warning")).toBeNull();
    expect(screen.queryByTestId("mon-cutover-note")).toBeNull();
  });

});

describe("cutover-effect copy", () => {
  // Checked against the DICTIONARIES rather than the rendered page, because the
  // page renders one locale and the rule applies to every one of them — a zh
  // string that leaks the vocabulary would sail past a render-only check.
  const strings = [en, zh].flatMap((dict) => [
    dict.monitor.machine.cutoverNotInEffect,
    dict.monitor.machine.cutoverUnproven,
    dict.monitor.machine.cutoverUnreported,
  ]);

  it("carries no internal vocabulary", () => {
    // Owner, verbatim: nobody outside this codebase knows what "anchor" is. A
    // warning nobody can read is not a warning.
    for (const text of strings) {
      for (const word of ["anchor", "legacy", "plist", "launchd", "tmux"]) {
        expect(text.toLowerCase(), `"${text}" leaks "${word}"`).not.toContain(word);
      }
    }
  });

  it("tells nobody to restart anything", () => {
    // This surface makes the state VISIBLE and stops there — deciding when to
    // act is a person's call, deliberately, and an automatic-repair suggestion
    // is the option the owner ruled out.
    for (const text of strings) {
      for (const word of ["restart", "reboot", "kill ", "重啟", "重新啟動"]) {
        expect(
          text.toLowerCase(),
          `"${text}" instructs the reader to ${word.trim()} something`
        ).not.toContain(word);
      }
    }
  });

  it("says something different in each state, in each locale", () => {
    // The same check as the rendered one above, one layer down: two dictionary
    // entries that happen to be equal would render as two identical lines.
    for (const dict of [en, zh]) {
      const m = dict.monitor.machine;
      const trio = [m.cutoverNotInEffect, m.cutoverUnproven, m.cutoverUnreported];
      expect(new Set(trio).size).toBe(trio.length);
      for (const text of trio) expect(text.trim()).not.toBe("");
    }
  });
});
