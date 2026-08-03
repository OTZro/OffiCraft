// T-ae38: the cockpit half — the two usage readouts that did not exist, and the
// four independent knobs that replaced one.
//
// Each block below pins something that can fail SILENTLY:
//
//  1. THE LEARNING CARD SHOWS ITS USAGE. The wire has carried size_chars /
//     cap_chars on the lessons doc since T-3aeb; `toLessons` was throwing both
//     away. So Learning was the one journal block whose remaining budget nobody
//     could see, and the way you found out you were full was being refused —
//     which happens in the last minutes before a handover, taking the round's
//     learnings with it. A mapper that drops two fields breaks nothing loudly.
//  2. THE ROLE-DEFINITION EDITOR SHOWS ITS USAGE. Duty carried NEITHER field on
//     the wire before this ticket, so an agent that had just condensed its own
//     role definition had to ask someone else to measure the doc.
//  3. EACH SETTINGS ROW WRITES ITS OWN KEY, and Duty's local floor is its own
//     smaller default. A row that reads one setting and PATCHes another is
//     invisible until someone notices the wrong number moved; sharing the other
//     three's floor would make the field locally reject the value the server
//     ships with.
//
// 🔴 THE THREE CAPS ARE SET TO THREE DIFFERENT NUMBERS throughout. Before
// T-ae38 there was ONE setting, so every "the cap shown is N" assertion was
// equally true of all three documents — with equal numbers this whole file
// would pass against the old single-cap cockpit.

import { describe, it, expect, beforeEach } from "vitest";
import { render, fireEvent } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { zh } from "../i18n/locales/zh";
import { SettingsPage } from "./SettingsPage";
import { __resetMock, mockApi } from "../api/mock";
import { DOC_CAP_CHARS_DEFAULTS, capForKind } from "../api/docCap";

const s = zh.settings;
const mp = zh.mp;

// Deliberately far apart, and none of them equal to another (see the header).
// Insight/Learning stay at or above their shipped defaults because each floor
// IS that segment's own default — the server (and the mock) 422s anything
// lower. They are written relative to DOC_CAP_CHARS_DEFAULTS so that raising a
// default cannot silently push a fixture below its own floor.
const DUTY_CAP = DOC_CAP_CHARS_DEFAULTS.duty + 500;
const INSIGHT_CAP = DOC_CAP_CHARS_DEFAULTS.insight + 2000;
const LEARNING_CAP = DOC_CAP_CHARS_DEFAULTS.learning * 2;

beforeEach(() => {
  __resetMock();
});

async function openRolePage(roleLabel: string) {
  const utils = render(
    <I18nProvider>
      <SettingsPage />
    </I18nProvider>
  );
  fireEvent.click(utils.getByText(s.roles));
  fireEvent.click(await utils.findByText(roleLabel));
  await utils.findAllByText(s.edit);
  return utils;
}

/** Walk to 參數調整 the way an owner does, and WAIT for the settings fetch: the
 * card renders nothing at all until it lands, so a synchronous query would fail
 * on an empty page rather than on a missing row. */
async function openParamsPage() {
  const utils = render(
    <I18nProvider>
      <SettingsPage />
    </I18nProvider>
  );
  fireEvent.click(utils.getByTestId("settings-params-entry"));
  await utils.findByLabelText(zh.settings.docCapDuty);
  return utils;
}

const cardWithTitle = (utils: { container: HTMLElement }, title: string) =>
  Array.from(utils.container.querySelectorAll<HTMLElement>(".mp-lessons")).find(
    (el) => el.querySelector(".mp-lessons__title")?.textContent?.includes(title)
  );

async function setCaps() {
  await mockApi.patchServerSettings({
    docCapCharsDuty: DUTY_CAP,
    docCapCharsInsight: INSIGHT_CAP,
    docCapCharsLearning: LEARNING_CAP,
  });
}

describe("T-ae38 — the three journal blocks each show their OWN budget", () => {
  it("the Learning card shows size / cap, and it is the LEARNING cap", async () => {
    await setCaps();
    // Multi-byte on purpose: the server counts RUNES, and `String.length` would
    // agree with a rune count on ASCII. "環境筆記" is 4 runes / 12 bytes.
    await mockApi.saveLessons("assistant", "general", "環境筆記");

    const utils = await openRolePage(zh.office.role.assistant);
    const card = cardWithTitle(utils, mp.lessons);
    expect(card).toBeTruthy();
    const size = card!.querySelector(".mp-insight__size");
    expect(size?.textContent?.replace(/\s+/g, " ").trim()).toBe(
      `4 / ${LEARNING_CAP}`
    );
  });

  it("the Insight card still shows the INSIGHT cap, not Learning's", async () => {
    // The negative half of the pair: if the two cards ever read one shared
    // number again, one of these two assertions has to move.
    await setCaps();
    await mockApi.saveInsight("assistant", "判準");

    const utils = await openRolePage(zh.office.role.assistant);
    const card = cardWithTitle(utils, mp.insight);
    const size = card!.querySelector(".mp-insight__size");
    expect(size?.textContent?.replace(/\s+/g, " ").trim()).toBe(
      `2 / ${INSIGHT_CAP}`
    );
  });

  it("the role-definition editor shows its length against the DUTY cap", async () => {
    await setCaps();
    await mockApi.saveRole("assistant", { definitionMd: "職責：守門" });

    const utils = await openRolePage(zh.office.role.assistant);
    const usage = utils.getByTestId("doc-card-usage");
    expect(usage.textContent?.replace(/\s+/g, " ").trim()).toBe(
      `5 / ${DUTY_CAP}`
    );
  });

  it("the Duty readout stays up while EDITING — that is when it is wanted", async () => {
    // A readout that vanishes the moment you open the editor answers the
    // question everywhere except where it is asked.
    await setCaps();
    await mockApi.saveRole("assistant", { definitionMd: "職責：守門" });

    const utils = await openRolePage(zh.office.role.assistant);
    fireEvent.click(utils.getAllByText(s.edit)[0]);
    expect(
      utils.getByTestId("doc-card-usage").textContent?.replace(/\s+/g, " ").trim()
    ).toBe(`5 / ${DUTY_CAP}`);
  });

  it("the global-context editor shows NO budget — it genuinely has no cap", async () => {
    // T-ae38's scope line: global_context is still uncapped on purpose. A
    // "0 / 0" here would invent a limit the server does not enforce, and the
    // owner has been promised in docs/guide/settings.md that it has none.
    const utils = render(
      <I18nProvider>
        <SettingsPage />
      </I18nProvider>
    );
    fireEvent.click(utils.getByText(s.roles));
    fireEvent.click(await utils.findByText(s.customName));
    await utils.findAllByText(s.edit);
    expect(utils.queryByTestId("doc-card-usage")).toBeNull();
  });
});

describe("T-ae38 — four knobs, each writing its own key", () => {
  it("renders one row per cap, each seeded from its own setting", async () => {
    await setCaps();
    const utils = await openParamsPage();

    const read = (label: string) =>
      (utils.getByLabelText(label) as HTMLInputElement).value;
    expect(read(s.docCapDuty)).toBe(String(DUTY_CAP));
    expect(read(s.docCapInsight)).toBe(String(INSIGHT_CAP));
    expect(read(s.docCapLearning)).toBe(String(LEARNING_CAP));
    // Untouched by setCaps → still the shipped default, which proves the four
    // rows are not all reading one value.
    expect(read(s.docCapManual)).toBe(String(DOC_CAP_CHARS_DEFAULTS.manual));
  });

  it("editing the Duty row moves ONLY the Duty setting", async () => {
    // The failure this catches is a row wired to the wrong field: it looks
    // right, saves without error, and moves someone else's cap.
    const utils = await openParamsPage();
    const input = utils.getByLabelText(s.docCapDuty);
    fireEvent.change(input, { target: { value: "2500" } });
    fireEvent.blur(input);

    const after = await mockApi.getServerSettings();
    expect(after.docCapCharsDuty).toBe(2500);
    expect(after.docCapCharsInsight).toBe(DOC_CAP_CHARS_DEFAULTS.insight);
    expect(after.docCapCharsLearning).toBe(DOC_CAP_CHARS_DEFAULTS.learning);
    expect(after.docCapCharsManual).toBe(DOC_CAP_CHARS_DEFAULTS.manual);
  });

  it("the Duty row's local floor is its own, NOT the other three's", async () => {
    // 🔴 Sharing the other three's floor would make this field locally reject
    // every value between the shipped Duty default and that floor — including
    // the value it is sitting on. 1200 is a legal Duty cap; the row must
    // accept it.
    const utils = await openParamsPage();
    const input = utils.getByLabelText(s.docCapDuty);
    fireEvent.change(input, { target: { value: "1200" } });
    fireEvent.blur(input);
    expect((await mockApi.getServerSettings()).docCapCharsDuty).toBe(1200);

    // CONTROL, so the pass above is not just "this row never validates":
    // below Duty's OWN floor is still refused locally and writes nothing.
    fireEvent.change(input, { target: { value: "999" } });
    fireEvent.blur(input);
    expect((await mockApi.getServerSettings()).docCapCharsDuty).toBe(1200);
  });

  it("the Learning row still refuses 1200 — its floor is its own default", async () => {
    // The other side of the same coin: per-row floors, not one relaxed number.
    const utils = await openParamsPage();
    const input = utils.getByLabelText(s.docCapLearning);
    fireEvent.change(input, { target: { value: "1200" } });
    fireEvent.blur(input);
    expect((await mockApi.getServerSettings()).docCapCharsLearning).toBe(
      DOC_CAP_CHARS_DEFAULTS.learning
    );
  });
});

describe("T-ae38 — capForKind routes each document kind to its own cap", () => {
  it("maps every kind, and gives the uncapped ones no number at all", () => {
    // Transcribed from restoreDocumentHistory's switch. `undefined` for the
    // uncapped kinds is load-bearing: falling back to "whichever number is
    // nearest" would let the cockpit mark a global-context revision
    // un-restorable when the server would accept it.
    const caps = { duty: 1, insight: 2, learning: 3, manual: 4 };
    expect(capForKind("role_definition", caps)).toBe(1);
    expect(capForKind("insight", caps)).toBe(2);
    expect(capForKind("lessons", caps)).toBe(3);
    expect(capForKind("task_manual_sop", caps)).toBe(4);
    expect(capForKind("task_manual_learnings", caps)).toBe(4);
    expect(capForKind("task_manual", caps)).toBe(4);
    expect(capForKind("global_context", caps)).toBeUndefined();
    expect(capForKind("task_description", caps)).toBeUndefined();
  });
});
