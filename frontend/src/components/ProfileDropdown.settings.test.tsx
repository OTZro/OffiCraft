// ProfileDropdown change-password (B3): the main menu owns the account
// sub-views while preferences keeps only appearance controls.
//
// The /api/settings parameter knobs (登入有效期 / 自動換手門檻) MOVED to the
// 設定 page's 參數調整 entry (owner 2026-07-12), and theme MANAGEMENT (import /
// export / edit / delete) MOVED to 設定/主題 (T-16a1 P3b →
// ThemeSettings.test.tsx). Here we pin that the dropdown kept only selection.

import { describe, it, expect, beforeEach, vi } from "vitest";
import { render, fireEvent, waitFor } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { zh } from "../i18n/locales/zh";
import { ProfileDropdown } from "./ProfileDropdown";
import { __resetMock } from "../api/mock";
import { api } from "../api";
import { setToken, clearToken } from "../api/auth";

const p = zh.profile;

async function openPreferences() {
  const utils = render(
    <I18nProvider>
      <ProfileDropdown
        open
        onClose={vi.fn()}
        userName="使用者"
        setOwnerName={vi.fn()}
      />
    </I18nProvider>
  );
  fireEvent.click(utils.getByText(p.preferences));
  await utils.findByText(p.theme);
  return utils;
}

beforeEach(() => {
  __resetMock();
  clearToken();
  // The theme-selector test writes oc.theme to localStorage; clear it so a
  // later test's first paint is not tinted (and stays on the zh default dict).
  localStorage.clear();
  delete document.documentElement.dataset.theme;
  delete document.documentElement.dataset.layout;
});

describe("ProfileDropdown · preferences scope", () => {
  it("no longer renders the server parameter knobs (they live in 設定/參數調整)", async () => {
    const utils = await openPreferences();
    const text = utils.container.textContent ?? "";
    expect(text).not.toContain(zh.settings.sessionTtl);
    expect(text).not.toContain(zh.settings.handover);
    // Theme selector + language remain.
    expect(utils.getByText(p.theme)).toBeTruthy();
    expect(utils.getByText(p.language)).toBeTruthy();
  });

  it("keeps only the theme SELECTOR — no management affordances (moved to 設定/主題)", async () => {
    setToken("owner-token");
    await api.patchServerSettings({
      customThemes: [
        { id: "midnight", name: "午夜藍", colors: { "--color-bg": "#101018" } },
      ],
    });
    const utils = await openPreferences();
    const select = utils.getByLabelText(p.theme);
    const custom = await waitFor(() => {
      const o = Array.from(select.querySelectorAll("option")).find(
        (x) => x.value === "midnight"
      );
      expect(o).toBeTruthy();
      return o!;
    });
    // 內建 / 自訂 is STRUCTURE: each option sits in the <optgroup> that says what
    // it is, and each option's own text is just the theme's name.
    const groupOf = (o: HTMLOptionElement) =>
      (o.parentElement as HTMLOptGroupElement).label;
    const builtin = Array.from(select.querySelectorAll("option")).find(
      (o) => o.value === "office"
    )!;
    expect(builtin.textContent).toBe(zh.themeIdentity.office);
    expect(groupOf(builtin)).toBe(zh.themeMarkers.builtinGroup);
    expect(custom.textContent).toBe("午夜藍");
    expect(groupOf(custom)).toBe(zh.themeMarkers.customGroup);
    // Management chips no longer live in the quick menu.
    expect(utils.queryByText(p.themeConfirmImport)).toBeNull();
    // A hint points the owner to the settings page instead.
    expect(utils.getByText(p.themeManageHint)).toBeTruthy();
  });

  it("cannot be made to show two identical built-in rows by a theme's NAME", async () => {
    // The owner's original symptom, re-entered through the other door (T-081b
    // review round 3, BLOCKER-2): while the marker was TEXT — 「辦公室」 + 「(內建)」 —
    // a pack simply naming itself 「辦公室(內建)」 produced two byte-identical rows
    // and the shipped theme became unfindable again. Both names are legal (neither
    // equals the reserved 「辦公室」), so the fix cannot be a name rule: the marker
    // has to be structure the name cannot reach.
    setToken("owner-token");
    const spoof = `${zh.themeIdentity.office}(${zh.themeMarkers.builtinGroup})`;
    await api.patchServerSettings({
      customThemes: [
        { id: "spoofpack", name: spoof, colors: { "--color-bg": "#101018" } },
      ],
    });
    const utils = await openPreferences();
    const select = utils.getByLabelText(p.theme);
    await waitFor(() => {
      expect(
        Array.from(select.querySelectorAll("option")).some((o) => o.value === "spoofpack")
      ).toBe(true);
    });
    const options = Array.from(select.querySelectorAll("option"));
    const builtin = options.find((o) => o.value === "office")!;
    const forged = options.find((o) => o.value === "spoofpack")!;
    // The spoof keeps its own name (it is legal), but it is NOT the built-in row…
    expect(forged.textContent).toBe(spoof);
    expect(builtin.textContent).not.toBe(forged.textContent);
    // …and the thing that says which is which is the group, not the text.
    expect((builtin.parentElement as HTMLOptGroupElement).label).toBe(
      zh.themeMarkers.builtinGroup
    );
    expect((forged.parentElement as HTMLOptGroupElement).label).toBe(
      zh.themeMarkers.customGroup
    );
    // The built-in group holds exactly the built-in.
    const builtinGroup = Array.from(select.querySelectorAll("optgroup")).find(
      (g) => g.label === zh.themeMarkers.builtinGroup
    )!;
    expect(
      Array.from(builtinGroup.querySelectorAll("option")).map((o) => o.value)
    ).toEqual(["office"]);
  });

  it("selects the built-in office theme from the quick picker", async () => {
    const utils = await openPreferences();
    fireEvent.change(utils.getByLabelText(p.theme), { target: { value: "office" } });
    expect(document.documentElement.dataset.theme).toBe("office");
  });

  it("offers the 版面 segmented control and flips the layout both ways (T-756f)", async () => {
    const utils = await openPreferences();
    expect(utils.getByText(p.layout)).toBeTruthy();
    // Narrow is the default, so nothing is applied to <html> yet.
    expect(document.documentElement.hasAttribute("data-layout")).toBe(false);

    fireEvent.click(utils.getByText(p.layoutWide));
    expect(document.documentElement.dataset.layout).toBe("wide");

    fireEvent.click(utils.getByText(p.layoutNarrow));
    expect(document.documentElement.hasAttribute("data-layout")).toBe(false);
  });
});

describe("ProfileDropdown · change password", () => {
  it("changes the password through the seam and confirms inline", async () => {
    const utils = render(
      <I18nProvider><ProfileDropdown open onClose={vi.fn()} userName="使用者" setOwnerName={vi.fn()} /></I18nProvider>,
    );
    fireEvent.click(utils.getByText(p.changePassword));
    fireEvent.change(utils.getByLabelText(p.currentPasswordPlaceholder), {
      target: { value: "mock-password" },
    });
    fireEvent.change(utils.getByLabelText(p.newPasswordPlaceholder), {
      target: { value: "next-password" },
    });
    fireEvent.change(utils.getByLabelText(p.confirmPasswordPlaceholder), {
      target: { value: "next-password" },
    });
    fireEvent.click(utils.getByText(p.save));
    await utils.findByText(p.pwdChanged);
    // The mock credential really rotated: the old current password now fails.
    await expect(api.changePassword("mock-password", "another-pass-1")).rejects.toThrow();
    await expect(api.changePassword("next-password", "another-pass-1")).resolves.toBeUndefined();
  });

  it("keeps a wrong current password an inline error (no logout bounce)", async () => {
    const utils = render(
      <I18nProvider><ProfileDropdown open onClose={vi.fn()} userName="使用者" setOwnerName={vi.fn()} /></I18nProvider>,
    );
    fireEvent.click(utils.getByText(p.changePassword));
    fireEvent.change(utils.getByLabelText(p.currentPasswordPlaceholder), {
      target: { value: "wrong-password" },
    });
    fireEvent.change(utils.getByLabelText(p.newPasswordPlaceholder), {
      target: { value: "next-password" },
    });
    fireEvent.change(utils.getByLabelText(p.confirmPasswordPlaceholder), {
      target: { value: "next-password" },
    });
    fireEvent.click(utils.getByText(p.save));
    await utils.findByText(p.pwdErrorCurrent);
  });

  it("rejects a short or mismatched new password locally", async () => {
    const utils = render(
      <I18nProvider><ProfileDropdown open onClose={vi.fn()} userName="使用者" setOwnerName={vi.fn()} /></I18nProvider>,
    );
    fireEvent.click(utils.getByText(p.changePassword));
    fireEvent.change(utils.getByLabelText(p.currentPasswordPlaceholder), {
      target: { value: "mock-password" },
    });
    fireEvent.change(utils.getByLabelText(p.newPasswordPlaceholder), {
      target: { value: "short" },
    });
    fireEvent.change(utils.getByLabelText(p.confirmPasswordPlaceholder), {
      target: { value: "short" },
    });
    fireEvent.click(utils.getByText(p.save));
    await utils.findByText(p.pwdErrorTooShort);

    fireEvent.change(utils.getByLabelText(p.newPasswordPlaceholder), {
      target: { value: "long-enough-pass" },
    });
    fireEvent.change(utils.getByLabelText(p.confirmPasswordPlaceholder), {
      target: { value: "different-pass" },
    });
    fireEvent.click(utils.getByText(p.save));
    await utils.findByText(p.pwdErrorMismatch);
  });
});

describe("ProfileDropdown · notification email", () => {
  it("enables Save only for unsaved edits and disables it again after saving", async () => {
    const utils = render(
      <I18nProvider><ProfileDropdown open onClose={vi.fn()} userName="使用者" setOwnerName={vi.fn()} /></I18nProvider>,
    );
    fireEvent.click(utils.getByText(p.pushContactEmail));
    const input = await utils.findByLabelText(p.pushContactEmail);
    const save = utils.getByRole("button", { name: p.save });
    expect((save as HTMLButtonElement).disabled).toBe(true);

    fireEvent.change(input, { target: { value: "push@example.com" } });
    expect((save as HTMLButtonElement).disabled).toBe(false);

    fireEvent.click(save);
    await vi.waitFor(() => expect((save as HTMLButtonElement).disabled).toBe(true));
  });
});
