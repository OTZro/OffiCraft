// Warden launchd shape on the machine panel — Monitor §2 (anchor cutover).
//
// A warden now reports which shape it is actually running under. There are FOUR
// facts, not three, because the absence of a report is itself a fact:
//
//   "anchor"      converted to the new shape
//   "legacy"      still on the old shape (never cut over, or rolled back)
//   "unknown"     the cutover build ran and could not read its own parent
//   null          this warden does not report a shape at all — it has not
//                 received the cutover release
//
// Before this row, all four rendered as the same nothing, which is what the
// team means by flying blind. The dangerous pair is the last two: "unknown"
// sends you to that machine, "not reported" sends you to the release pipeline,
// and a UI that folds them together silently sends you to the wrong one.
//
// So the assertion that matters here is NOT four "renders X" checks — those all
// stay green when two states quietly converge on one output. It is a PAIRWISE
// distinctness check across all four, so collapsing any pair turns it red.

import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, cleanup } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { MonitorPage } from "./MonitorPage";
import type { Member, MachineView, WardenShape } from "../types";

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

const machine = (id: string, wardenShape: WardenShape): MachineView => ({
  machineId: id,
  displayName: id,
  online: true,
  isSelf: false,
  binStatus: null,
  wardenShape,
  claudeVersion: null,
  claudeCredSource: null,
  claudeSubReadable: null,
});

/** Every state the field has, named for the failure messages below. The null
 * entry is listed as a peer, not as a default — it is a state, not a gap. */
const STATES: ReadonlyArray<readonly [name: string, shape: WardenShape]> = [
  ["anchor", "anchor"],
  ["legacy", "legacy"],
  ["unknown", "unknown"],
  ["not reported (null)", null],
];

function renderMonitor() {
  return render(
    <I18nProvider>
      <MonitorPage />
    </I18nProvider>
  );
}

/** Render ONE machine in the given state and return what the operator can
 * actually tell apart about its badge. Rendered one at a time on purpose: a
 * single row cannot borrow a sibling row's markup, so each signature is that
 * state's own output and nothing else's. */
async function badgeOf(shape: WardenShape) {
  listMachines.mockResolvedValue([machine("m-under-test", shape)]);
  renderMonitor();
  const badge = await screen.findByTestId("mon-warden-shape");
  const signature = {
    text: (badge.textContent ?? "").trim(),
    className: badge.className,
    title: badge.getAttribute("title") ?? "",
  };
  cleanup();
  return signature;
}

describe("MonitorPage warden shape badge", () => {
  beforeEach(() => {
    listMembers.mockResolvedValue([]);
  });

  it("renders every one of the four states distinguishably from every other", async () => {
    const seen: Array<{
      name: string;
      text: string;
      className: string;
      title: string;
    }> = [];
    for (const [name, shape] of STATES) {
      seen.push({ name, ...(await badgeOf(shape)) });
    }

    // Every state must produce SOMETHING — a state that renders nothing is
    // indistinguishable from a state whose badge failed to render.
    for (const s of seen) {
      expect(s.text, `${s.name} rendered an empty badge`).not.toBe("");
    }

    // The pairwise sweep. All six pairs, each channel separately, so the
    // failure names the two states that collapsed and the channel they
    // collapsed on rather than just reporting "expected 4, got 3".
    for (let i = 0; i < seen.length; i++) {
      for (let j = i + 1; j < seen.length; j++) {
        const a = seen[i];
        const b = seen[j];
        expect(
          a.text,
          `"${a.name}" and "${b.name}" show the same label "${a.text}" — an operator cannot tell them apart`
        ).not.toBe(b.text);
        expect(
          a.className,
          `"${a.name}" and "${b.name}" share the class "${a.className}" — they will be styled identically`
        ).not.toBe(b.className);
        expect(
          a.title,
          `"${a.name}" and "${b.name}" share the same hover explanation`
        ).not.toBe(b.title);
      }
    }
  });

  it("keeps the reported 'unknown' and the never-reported null apart in words", async () => {
    // Called out on its own because this is the pair the field exists for, and
    // because a future "simplification" of the badge is most likely to reach
    // for a shared "unknown" label for both.
    const reported = await badgeOf("unknown");
    const never = await badgeOf(null);
    expect(reported.text).not.toBe(never.text);
    expect(reported.title).not.toBe(never.title);
  });

  it("falls back to the not-reported face rather than dropping the badge", async () => {
    const never = await badgeOf(null);
    const anchor = await badgeOf("anchor");
    expect(never.className).toContain("mon-shape");
    expect(anchor.className).toContain("mon-shape");
  });
});
