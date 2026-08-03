// ThemeSettings · import from a LINK (T-29c7).
//
// The claim: paste one link, the theme is imported. So the assertions run past
// "the fetch was called" all the way to "the theme is listed AND on the server"
// — an implementation that fetched perfectly and then dropped the bundle on the
// floor would satisfy a call-count assertion on its own.
//
// 🔴 One test here pins an ABSENCE: the cockpit must NOT pre-screen where the
// link points. The owner ruled on 2026-08-03, after the timing of the risk was
// put to him, that link origin is unconstrained. A client-side origin rule
// would refuse links the server accepts, and every other test in this file
// would stay green while it did.

import { describe, it, expect, beforeEach, vi } from "vitest";
import { render, fireEvent, act, waitFor } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { zh } from "../i18n/locales/zh";
import { ThemeSettings } from "./ThemeSettings";
import { __resetMock } from "../api/mock";
import { api } from "../api";
import { setToken, clearToken } from "../api/auth";
import { ApiError } from "../api/errors";

const p = zh.profile;

const LINKED_BUNDLE = JSON.stringify({
  id: "linked-1",
  name: "連結來的主題",
  colors: { "--color-accent": "#0b1020" },
});

async function renderManage() {
  let utils!: ReturnType<typeof render>;
  await act(async () => {
    utils = render(
      <I18nProvider>
        <ThemeSettings crumbs={[{ label: zh.settings.title }]} />
      </I18nProvider>
    );
    await new Promise((r) => setTimeout(r, 0));
  });
  return utils;
}

/** Open the import view and paste `url` into the link field. */
function openImportWithLink(
  utils: ReturnType<typeof render>,
  url: string
): HTMLElement {
  fireEvent.click(utils.getByText(p.themeImport));
  const field = utils.getByLabelText(p.themeImportLinkLabel);
  fireEvent.change(field, { target: { value: url } });
  return utils.getByText(p.themeImportFromLink);
}

beforeEach(() => {
  __resetMock();
  clearToken();
  document.documentElement.removeAttribute("style");
  delete document.documentElement.dataset.theme;
  vi.restoreAllMocks();
});

describe("ThemeSettings · import from a link", () => {
  it("pastes a link, fetches the JSON, and the theme is imported", async () => {
    setToken("owner-token");
    const fetchSpy = vi
      .spyOn(api, "fetchThemeFromLink")
      .mockResolvedValue(LINKED_BUNDLE);

    const utils = await renderManage();
    const button = openImportWithLink(
      utils,
      "https://studio.example/api/chat/attachment/att-1?sig=abc"
    );
    await act(async () => {
      fireEvent.click(button);
      await new Promise((r) => setTimeout(r, 0));
    });

    // 1. the link went to the server verbatim…
    expect(fetchSpy).toHaveBeenCalledWith(
      "https://studio.example/api/chat/attachment/att-1?sig=abc"
    );
    // 2. …the theme is on the list…
    expect(await utils.findByText("連結來的主題")).toBeTruthy();
    // 3. …and it actually LANDED on the server, which is the difference
    //    between "imported" and "rendered once".
    await waitFor(async () => {
      const srv = await api.getServerSettings();
      expect(srv.customThemes.map((b) => b.id)).toContain("linked-1");
    });
  });

  it("does not pre-screen where the link points (owner ruling 2026-08-03)", async () => {
    // Loopback, a private range and a raw IP: all sent as-is. If somebody adds
    // a client-side origin blocklist, this is the test that says it was decided
    // against rather than forgotten.
    setToken("owner-token");
    const fetchSpy = vi
      .spyOn(api, "fetchThemeFromLink")
      .mockResolvedValue(LINKED_BUNDLE);

    for (const url of [
      "http://127.0.0.1:8080/theme.json",
      "http://192.168.1.9/theme.json",
      "http://169.254.169.254/latest/meta-data",
    ]) {
      const utils = await renderManage();
      const button = openImportWithLink(utils, url);
      await act(async () => {
        fireEvent.click(button);
        await new Promise((r) => setTimeout(r, 0));
      });
      expect(fetchSpy).toHaveBeenCalledWith(url);
      utils.unmount();
    }
  });

  it("shows the server's own refusal rather than a generic message", async () => {
    // The server names WHAT is wrong (bad url / too large / not a theme). A
    // component that swallowed that and printed its own line would leave the
    // owner guessing which of the three it was.
    setToken("owner-token");
    vi.spyOn(api, "fetchThemeFromLink").mockRejectedValue(
      new ApiError(
        "http 422",
        422,
        "validation_error",
        "that link's content is not a valid theme: custom_themes[0]: id must match"
      )
    );

    const utils = await renderManage();
    const button = openImportWithLink(utils, "https://studio.example/x.json");
    await act(async () => {
      fireEvent.click(button);
      await new Promise((r) => setTimeout(r, 0));
    });

    const err = utils.container.querySelector(".set-error");
    expect(err?.textContent).toContain("not a valid theme");
    // …and nothing was imported.
    const srv = await api.getServerSettings();
    expect(srv.customThemes).toHaveLength(0);
  });

  it("falls back to its own message when the failure carries none", async () => {
    setToken("owner-token");
    vi.spyOn(api, "fetchThemeFromLink").mockRejectedValue(new Error("boom"));
    const utils = await renderManage();
    const button = openImportWithLink(utils, "https://studio.example/x.json");
    await act(async () => {
      fireEvent.click(button);
      await new Promise((r) => setTimeout(r, 0));
    });
    expect(utils.container.querySelector(".set-error")?.textContent).toBe(
      p.themeImportLinkFailed
    );
  });

  it("keeps the button inert until there is a link to fetch", async () => {
    setToken("owner-token");
    const fetchSpy = vi.spyOn(api, "fetchThemeFromLink");
    const utils = await renderManage();
    fireEvent.click(utils.getByText(p.themeImport));
    const button = utils.getByText(p.themeImportFromLink) as HTMLButtonElement;
    expect(button.disabled).toBe(true);
    // POSITIVE CONTROL: it is the empty field that disables it, not the button
    // being permanently dead.
    fireEvent.change(utils.getByLabelText(p.themeImportLinkLabel), {
      target: { value: "https://studio.example/x.json" },
    });
    expect(button.disabled).toBe(false);
    expect(fetchSpy).not.toHaveBeenCalled();
  });

  it("warns that a share link is identity-less, permanent and irrevocable", async () => {
    // The person pasting a link here is the one who can still decide whether
    // the theme should be readable by anyone holding the URL, so this warning
    // has to be on THIS screen — it is worth nothing in a doc they will not
    // open. Pinned by its own text so deleting it goes red.
    setToken("owner-token");
    const utils = await renderManage();
    fireEvent.click(utils.getByText(p.themeImport));
    const note = utils.container.querySelector(".ts-link-note");
    expect(note?.textContent).toBe(p.themeImportLinkShareNote);
    expect(p.themeImportLinkShareNote).toContain("撤不回來");
  });
});
