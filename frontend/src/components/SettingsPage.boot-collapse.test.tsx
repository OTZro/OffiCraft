// 設定 › 角色誌 › 啟動程序 stacks TWO whole documents on one page, and this
// pins that BOTH of them arrive CLOSED (T-6278).
//
// The defect it stands against was met by the owner on a phone: he scrolled
// through 啟動程序（Claude Code）to the end of its card, read that as the end of
// the PAGE, and reported that 啟動程序（Codex CLI）was missing. It was rendered
// the whole time, thousands of pixels below the fold. His instruction was
// 「你可以改成兩個都先收疊，我點選時才展開嗎？」
//
// What jsdom can pin is the STATE — that nothing renders a document body until
// a heading is pressed. Whether the two headings then actually share one phone
// screen is geometry, and that is measured in the CT guard
// (visual-guards/boot-collapse.ct.spec.tsx). Neither half is the whole claim.

import { describe, it, expect, beforeEach } from "vitest";
import { render, fireEvent } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { zh } from "../i18n/locales/zh";
import { SettingsPage } from "./SettingsPage";
import { __resetMock } from "../api/mock";

const s = zh.settings;

async function openBootPage() {
  const utils = render(
    <I18nProvider>
      <SettingsPage />
    </I18nProvider>
  );
  fireEvent.click(utils.getByText(s.roles));
  await utils.findByText(s.systemName);
  fireEvent.click(utils.getByText(s.bootName));
  await utils.findByText(s.bootClaudeName);
  return utils;
}

beforeEach(() => {
  __resetMock();
});

describe("SettingsPage · 啟動程序 both documents collapsed", () => {
  it("shows both runtimes' headings and neither body until one is pressed", async () => {
    const { container, getByText, findByText } = await openBootPage();

    // Both documents are announced by name. When both are closed the NAME is
    // the only thing telling them apart, so an unnamed collapsed card would be
    // the same defect in a shorter page.
    expect(getByText(s.bootClaudeName)).toBeTruthy();
    expect(getByText(s.bootCodexName)).toBeTruthy();

    // Closed means the body is NOT RENDERED, not merely hidden: this is the
    // assertion that fails on the version the owner met, where both documents
    // rendered in full.
    expect(container.querySelectorAll(".doc-md").length).toBe(0);
    expect(container.querySelectorAll('[data-testid="doc-card-edit"]').length).toBe(
      0
    );

    // Pressing ONE opens ONE. The second staying closed is half the point — two
    // open documents is the page he reported.
    fireEvent.click(getByText(s.bootClaudeName));
    await findByText(s.bootCodexName);
    expect(container.querySelectorAll(".doc-md").length).toBe(1);

    fireEvent.click(getByText(s.bootCodexName));
    expect(container.querySelectorAll(".doc-md").length).toBe(2);

    // And pressing again closes it, so the page can be put back the way it
    // opened without leaving the page.
    fireEvent.click(getByText(s.bootClaudeName));
    expect(container.querySelectorAll(".doc-md").length).toBe(1);
  });

  it("keeps 系統互動 — the page carrying ONE document — open as it always was", async () => {
    const utils = render(
      <I18nProvider>
        <SettingsPage />
      </I18nProvider>
    );
    fireEvent.click(utils.getByText(s.roles));
    await utils.findByText(s.systemName);
    fireEvent.click(utils.getByText(s.systemName));

    // Collapsing is for the page that stacks two documents. A page whose whole
    // content is one document would be asking for a click to show what the
    // reader already asked for.
    await utils.findByTestId("doc-card-edit");
    expect(utils.container.querySelectorAll(".doc-md").length).toBe(1);
    expect(
      utils.container.querySelectorAll('[data-testid="doc-card-collapse"]').length
    ).toBe(0);
  });
});
