// The global-context block list and the 使用者自訂 block.
//
// T-791e turned the two read-only seed cards into editable documents, so the
// list is now FOUR rows in boot-assembly order — 系統互動 → 使用者自訂 →
// 啟動程序（Claude Code）→ 啟動程序（Codex CLI）— and the two boot sequences
// have a row EACH, never one row for "the boot sequence". The editable
// behaviour of the three new pages lives in BootDocPage.test.tsx; what is
// pinned here is the list itself, the routing into each page, and the
// unchanged 使用者自訂 block — "unchanged" now including through T-c33e, which
// gave the three boot blocks the same <DocCard> shell this one has always used
// and must not have moved anything under this one.
//
// Plus the presentation rule: no block surfaces a .md filename (blocks are
// content, not files).

import { describe, it, expect, beforeEach } from "vitest";
import { render, fireEvent, waitFor } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { zh } from "../i18n/locales/zh";
import { SettingsPage } from "./SettingsPage";
import { __resetMock } from "../api/mock";

const s = zh.settings;

/** Render Settings and navigate landing → 角色誌 (the roles/blocks list). */
async function openRolesLog() {
  const utils = render(
    <I18nProvider>
      <SettingsPage />
    </I18nProvider>
  );
  fireEvent.click(utils.getByText(s.roles));
  // The three block entries render synchronously; the role list is async.
  await utils.findByText(s.systemName);
  return utils;
}

beforeEach(() => {
  __resetMock();
});

describe("SettingsPage · global-context 3 blocks", () => {
  it("lists the four blocks in boot-assembly order, without .md filenames", async () => {
    const { container, getByText } = await openRolesLog();
    const text = container.textContent ?? "";
    const iSystem = text.indexOf(s.systemName);
    const iCustom = text.indexOf(s.customName);
    const iClaude = text.indexOf(s.bootClaudeName);
    const iCodex = text.indexOf(s.bootCodexName);
    expect(iSystem).toBeGreaterThanOrEqual(0);
    expect(iCustom).toBeGreaterThan(iSystem);
    expect(iClaude).toBeGreaterThan(iCustom);
    expect(iCodex).toBeGreaterThan(iClaude);
    expect(getByText(s.globalSection)).toBeTruthy();
    // Presentation rule: the blocks never expose their backing filenames.
    expect(text).not.toContain("global-context.md");
    expect(text).not.toContain("boot_sequence.md");
    expect(text).not.toContain("system_interaction.md");
  });

  it("系統互動 opens its own editable page instead of a read-only seed card", async () => {
    const utils = await openRolesLog();
    fireEvent.click(utils.getByText(s.systemName));
    // The live document arrives from the api seam, and the page offers the
    // affordances the read-only card had none of. Since T-c33e it draws them
    // with the SHARED <DocCard> — same testids as 角色定義 — plus the one thing
    // that is genuinely its own: a top-level factory restore.
    await utils.findByTestId("doc-card-usage");
    expect(utils.getByTestId("doc-card-reset")).toBeTruthy();
    expect(utils.getByTestId("doc-card-edit")).toBeTruthy();
    expect(utils.container.querySelector(".doc-card__file code")).toBeNull();
  });

  it("使用者自訂 is editable: starts empty/default, save persists via the api", async () => {
    const utils = await openRolesLog();
    fireEvent.click(utils.getByText(s.customName));
    // Empty seed → the default badge shows and the edit affordance exists.
    await utils.findByText(s.edit);
    expect(utils.getByText(s.defaultBadge)).toBeTruthy();
    expect(utils.container.querySelector(".doc-card__file code")).toBeNull();

    fireEvent.click(utils.getByText(s.edit));
    const editor = utils.container.querySelector("textarea");
    expect(editor).toBeTruthy();
    fireEvent.change(editor!, { target: { value: "多用 emoji 回覆 owner" } });
    fireEvent.click(utils.getByText(s.doneEdit));

    // The save response folds back: owner text rendered, default badge gone.
    await utils.findByText("多用 emoji 回覆 owner");
    await waitFor(() => expect(utils.queryByText(s.defaultBadge)).toBeNull());
  });
});
