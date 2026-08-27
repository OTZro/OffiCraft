// T-8 — the cockpit half: N (how many backups are kept) is an adjustable
// setting, and the copy beside it tells the truth about what N is.
//
// Three things here fail silently without a test:
//
//  1. THE ROW EXISTS AND READS THE LIVE VALUE. N was a Go constant, so "the
//     settings page can move it" is half the ticket; a row that renders the
//     shipped 5 no matter what the server says looks identical on a fresh
//     install and only diverges once somebody changes it.
//  2. THE SAVE CARRIES backup_retain AND NOTHING ELSE. This is the one row on
//     the page whose value DELETES files, so a row wired to a neighbouring
//     field would quietly destroy backups while looking correct.
//  3. THE COPY SAYS THE TWO THINGS THE INTEGER CANNOT. "N is versions, not
//     days" and "N is per pool, not per directory" are the two readings that
//     have already been got wrong, and they are only ever wrong in the
//     direction of someone believing they bought more coverage than they did.
//     A label that omits them is not a smaller label, it is a misleading one —
//     so the assertions below are on the RENDERED text, not on a constant.

import { describe, it, expect, beforeEach } from "vitest";
import { render, fireEvent } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { zh } from "../i18n/locales/zh";
import { en } from "../i18n/locales/en";
import { SettingsPage } from "./SettingsPage";
import { __resetMock, mockApi } from "../api/mock";
import {
  BACKUP_RETAIN_DEFAULT,
  BACKUP_RETAIN_MAX,
  BACKUP_RETAIN_MIN,
} from "../api/backupRetain";

const s = zh.settings;

beforeEach(() => {
  __resetMock();
});

async function openParamsPage() {
  const utils = render(
    <I18nProvider>
      <SettingsPage />
    </I18nProvider>
  );
  fireEvent.click(utils.getByTestId("settings-params-entry"));
  await utils.findByLabelText(s.backupRetain);
  return utils;
}

describe("T-8 — backup retention N is an adjustable setting", () => {
  it("the row shows the LIVE value, not the shipped default", async () => {
    // Set it to something nobody ships first: a row hardcoded to the default is
    // indistinguishable from a correct one on a fresh install.
    await mockApi.patchServerSettings({ backupRetain: 9 });
    expect(BACKUP_RETAIN_DEFAULT).not.toBe(9);
    const utils = await openParamsPage();
    expect(
      (utils.getByLabelText(s.backupRetain) as HTMLInputElement).value
    ).toBe("9");
  });

  it("editing the row moves ONLY the retention count", async () => {
    const utils = await openParamsPage();
    const before = await mockApi.getServerSettings();

    const input = utils.getByLabelText(s.backupRetain);
    fireEvent.change(input, { target: { value: "3" } });
    fireEvent.blur(input);

    const after = await mockApi.getServerSettings();
    expect(after.backupRetain).toBe(3);
    // Everything else on the settings object is untouched. Named as a whole
    // object comparison so a row wired to the wrong field says WHICH field
    // moved instead of merely "3 !== 5".
    expect({ ...after, backupRetain: before.backupRetain }).toEqual(before);
  });

  it("refuses a value outside 1..20 without saving anything", async () => {
    const utils = await openParamsPage();
    const before = await mockApi.getServerSettings();

    for (const bad of [
      String(BACKUP_RETAIN_MIN - 1),
      String(BACKUP_RETAIN_MAX + 1),
    ]) {
      const input = utils.getByLabelText(s.backupRetain);
      fireEvent.change(input, { target: { value: bad } });
      fireEvent.blur(input);
      const after = await mockApi.getServerSettings();
      expect(after.backupRetain).toBe(before.backupRetain);
    }
    // 0 is refused deliberately: it would mean "keep nothing", i.e. delete the
    // snapshot that was just taken.
    expect(BACKUP_RETAIN_MIN).toBe(1);
  });

  it("the copy beside the field says N is VERSIONS, not days", async () => {
    // The failure: a reader sets 5 and believes they bought a fixed number of
    // days of history. They did not — the same 5 covered under three days on
    // this machine's busiest day and over a week on a quiet one.
    const utils = await openParamsPage();
    const sub = utils.getByText(s.backupRetainSub);
    expect(sub.textContent).toContain("它算的是「份數」，不是「天數」");
    expect(en.settings.backupRetainSub).toContain("VERSIONS, NOT DAYS");
  });

  it("the copy beside the field says N is PER POOL, not per directory", async () => {
    // The failure: a reader sets 5 and believes the directory holds 5 files. It
    // holds up to 10 — routine and pre-migration backups keep separate quotas —
    // so both the disk cost and the depth are double what they assumed.
    const utils = await openParamsPage();
    const sub = utils.getByText(s.backupRetainSub);
    expect(sub.textContent).toContain("它也是「每一池」而不是「每個資料夾」");
    expect(sub.textContent).toContain("十份");
    expect(en.settings.backupRetainSub).toContain("PER POOL, NOT PER DIRECTORY");
    expect(en.settings.backupRetainSub).toContain("TEN files");
  });

  it("the copy says the excess is DELETED, not moved aside", async () => {
    // The whole point of the ticket. Someone lowering this number is deleting
    // backups, and the page must say so before they do it, not after.
    const utils = await openParamsPage();
    const sub = utils.getByText(s.backupRetainSub);
    expect(sub.textContent).toContain("刪掉");
    expect(sub.textContent).toContain("救不回來");
    expect(en.settings.backupRetainSub).toContain("DELETED");
  });
});
