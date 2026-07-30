// 版本紀錄 (T-7d33) on the Settings doc pages: the retained revisions of each
// editable long-form document, and the owner's restore.
//
// The three things worth pinning:
//   1. the list renders one row per retained revision, with WHEN, WHO and a
//      preview of the content that tells two versions apart;
//   2. restore is CONFIRMED first (it overwrites the live doc), then rides the
//      adapter and refreshes BOTH the visible document and the list;
//   3. every string comes from the dictionary — the card holds no literals.

import { describe, it, expect, beforeEach, vi } from "vitest";
import { render, fireEvent, waitFor, within } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { zh } from "../i18n/locales/zh";
import { SettingsPage } from "./SettingsPage";
import { __resetMock, mockApi } from "../api/mock";
import { DOC_CAP_CHARS } from "../api/docCap";

const s = zh.settings;

/** Render Settings and land on 角色誌 › 使用者自訂 — the global-context block,
 * the simplest of the four documents that carry a history card. */
async function openUserCustomDoc() {
  const utils = render(
    <I18nProvider>
      <SettingsPage />
    </I18nProvider>
  );
  fireEvent.click(utils.getByText(s.roles));
  await utils.findByText(s.systemName);
  fireEvent.click(utils.getByText(s.customName));
  await utils.findByText(s.edit);
  return utils;
}

beforeEach(() => {
  __resetMock();
});

describe("SettingsPage · 版本紀錄", () => {
  it("says a never-edited document has no retained revisions", async () => {
    const utils = await openUserCustomDoc();
    expect(utils.getByText(s.historyTitle)).toBeTruthy();
    await utils.findByText(s.historyEmpty);
  });

  it("lists each retained revision with when, who and a content preview", async () => {
    await mockApi.saveGlobalContext("第一版：多用 emoji");
    await mockApi.saveGlobalContext("第二版：少用 emoji");

    const utils = await openUserCustomDoc();
    const rows = await waitFor(() => {
      const found = utils.container.querySelectorAll(".doc-hist__item");
      expect(found).toHaveLength(2);
      return found;
    });

    // Newest first: the top row is the text the LAST write replaced. The
    // preview is what makes two revisions of one doc distinguishable.
    expect(within(rows[0] as HTMLElement).getByText("第一版：多用 emoji")).toBeTruthy();
    expect(within(rows[0] as HTMLElement).getByText(s.historyField.text)).toBeTruthy();
    // Who wrote it, through the dictionary label (never a bare id on its own).
    expect((rows[0] as HTMLElement).textContent).toContain(s.historyByLabel);
    // The oldest revision predates any owner edit — the seed-state badge says
    // so instead of showing a misleading empty preview.
    expect(within(rows[1] as HTMLElement).getByText(s.historyDefaultBadge)).toBeTruthy();
  });

  it("restore asks first, then rides the adapter and refreshes doc and list", async () => {
    await mockApi.saveGlobalContext("原本的內容");
    await mockApi.saveGlobalContext("後來改壞的內容");
    const restore = vi.spyOn(mockApi, "restoreDocumentHistory");

    const utils = await openUserCustomDoc();
    await utils.findByText("後來改壞的內容");

    const [target] = await mockApi.listDocumentHistory(
      "global_context",
      "global"
    );
    fireEvent.click(utils.getByTestId(`doc-history-restore-${target.id}`));

    // Nothing has fired yet — the confirmation is the gate, not a courtesy.
    expect(restore).not.toHaveBeenCalled();
    const modal = utils.getByTestId("doc-history-restore-confirm");
    expect(within(modal).getByText(s.historyRestoreConfirmAction)).toBeTruthy();

    fireEvent.click(utils.getByTestId("doc-history-restore-confirm-btn"));

    await waitFor(() =>
      expect(restore).toHaveBeenCalledWith("global_context", "global", target.id)
    );
    // The visible document is re-read, not assumed. Scoped to the doc body:
    // the restored text is ALSO in the history preview, and asserting on the
    // page as a whole would pass even if the editor never refreshed.
    await waitFor(() =>
      expect(
        utils.container.querySelector(".doc-card__body .doc-md")?.textContent
      ).toContain("原本的內容")
    );
    // …and the list now holds the content the restore itself overwrote.
    await waitFor(() =>
      expect(
        utils.container.querySelector(".doc-hist__list")?.textContent
      ).toContain("後來改壞的內容")
    );
    expect(utils.queryByTestId("doc-history-restore-confirm")).toBeNull();
    restore.mockRestore();
  });

  it("cancelling the confirmation leaves the document alone", async () => {
    await mockApi.saveGlobalContext("目前的內容");
    const restore = vi.spyOn(mockApi, "restoreDocumentHistory");

    const utils = await openUserCustomDoc();
    const [target] = await mockApi.listDocumentHistory(
      "global_context",
      "global"
    );
    fireEvent.click(await utils.findByTestId(`doc-history-restore-${target.id}`));
    fireEvent.click(
      within(utils.getByTestId("doc-history-restore-confirm")).getByText(
        s.cancel
      )
    );

    expect(restore).not.toHaveBeenCalled();
    expect(utils.queryByTestId("doc-history-restore-confirm")).toBeNull();
    expect((await mockApi.getGlobalContext()).text).toBe("目前的內容");
    restore.mockRestore();
  });

  it("keeps a failed restore honest: the dialog stays open with the reason", async () => {
    await mockApi.saveGlobalContext("目前的內容");
    const restore = vi
      .spyOn(mockApi, "restoreDocumentHistory")
      .mockRejectedValue(new Error("boom"));

    const utils = await openUserCustomDoc();
    const [target] = await mockApi.listDocumentHistory(
      "global_context",
      "global"
    );
    fireEvent.click(await utils.findByTestId(`doc-history-restore-${target.id}`));
    fireEvent.click(utils.getByTestId("doc-history-restore-confirm-btn"));

    await utils.findByText(s.historyRestoreError);
    expect(utils.getByTestId("doc-history-restore-confirm")).toBeTruthy();
    restore.mockRestore();
  });

  it("marks an over-cap revision un-restorable up front, with the reason", async () => {
    // The revision the server WOULD refuse with a 400: a lessons doc that is
    // over the cap and not shrinking. Before this, the owner only found out by
    // clicking — which reads as a broken system rather than a stated limit.
    const overCap = "字".repeat(DOC_CAP_CHARS + 1);
    await mockApi.saveLessons("assistant", "general", overCap);
    await mockApi.saveLessons("assistant", "general", "短");

    const utils = render(
      <I18nProvider>
        <SettingsPage />
      </I18nProvider>
    );
    fireEvent.click(utils.getByText(s.roles));
    fireEvent.click(await utils.findByText(zh.office.role.assistant));

    const [target] = await mockApi.listDocumentHistory(
      "lessons",
      "assistant::general"
    );
    expect(target.content.text).toBe(overCap);

    const row = await utils.findByTestId(`doc-history-item-${target.id}`);
    // Listed, never hidden — this row is the only place that text still exists.
    expect(row).toBeTruthy();
    expect(within(row).getByText(s.historyBlockedBadge)).toBeTruthy();
    // The reason is IN the row, and names the field and the cap.
    const reason = utils.getByTestId(`doc-history-blocked-${target.id}`);
    expect(reason.textContent).toContain(s.historyField.text);
    expect(reason.textContent).toContain(String(DOC_CAP_CHARS));
    // …and the control is dead, so the 400 can never be reached from here.
    expect(
      (utils.getByTestId(`doc-history-restore-${target.id}`) as HTMLButtonElement)
        .disabled
    ).toBe(true);
  });

  it("leaves an ordinary revision restorable — the mark is not blanket", async () => {
    await mockApi.saveLessons("assistant", "general", "第一版經驗");
    await mockApi.saveLessons("assistant", "general", "第二版經驗");

    const utils = render(
      <I18nProvider>
        <SettingsPage />
      </I18nProvider>
    );
    fireEvent.click(utils.getByText(s.roles));
    fireEvent.click(await utils.findByText(zh.office.role.assistant));

    const [target] = await mockApi.listDocumentHistory(
      "lessons",
      "assistant::general"
    );
    const button = (await utils.findByTestId(
      `doc-history-restore-${target.id}`
    )) as HTMLButtonElement;
    expect(button.disabled).toBe(false);
    expect(utils.queryByTestId(`doc-history-blocked-${target.id}`)).toBeNull();
  });

  it("shows the same card on a role definition, keyed to that role", async () => {
    await mockApi.saveRole("assistant", { definitionMd: "角色定義改寫" });

    const utils = render(
      <I18nProvider>
        <SettingsPage />
      </I18nProvider>
    );
    fireEvent.click(utils.getByText(s.roles));
    fireEvent.click(await utils.findByText(zh.office.role.assistant));

    await waitFor(() =>
      expect(
        utils.container.querySelectorAll(".doc-hist__item").length
      ).toBeGreaterThan(0)
    );
    expect(utils.getAllByText(s.historyTitle).length).toBeGreaterThan(0);
  });
});
