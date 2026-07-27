// ThemeSettings (T-16a1 P3b): the 設定/主題 management surface that theme
// management MOVED to from the profile dropdown. Covers import (moved verbatim
// + injection block), friendly grouped colour editing, and the 用詞 (wording)
// overlay editor round-trip.

import { describe, it, expect, beforeEach, vi, afterEach } from "vitest";
import { readFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";
import { render, fireEvent, within, act } from "@testing-library/react";
import { THEME_COLOR_TOKENS } from "../styles/themeTokens.generated";
import { MESSAGE_KEYS } from "../i18n/messageKeys.generated";
import { validateThemeBundle } from "../lib/themeBundle";
import { I18nProvider } from "../i18n";
import { zh } from "../i18n/locales/zh";
import { makeMessages } from "../i18n/compose";
import { ThemeSettings } from "./ThemeSettings";
import { tokenMeta } from "../lib/themeTokenMeta";
import { __resetMock } from "../api/mock";
import { api } from "../api";
import { setToken, clearToken } from "../api/auth";

const SENTINEL = "偽造";

const p = zh.profile;
const s = zh.settings;

// Let the provider's mount reconcile (getServerSettings) settle BEFORE we touch
// the custom-theme set — otherwise its late-resolving GET overwrites an import
// with the (still-empty) server value.
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

beforeEach(() => {
  __resetMock();
  clearToken();
  document.documentElement.removeAttribute("style");
  delete document.documentElement.dataset.theme;
});

async function importBundle(
  utils: Awaited<ReturnType<typeof renderManage>>,
  bundle: unknown
) {
  fireEvent.click(utils.getByText(p.themeImport));
  fireEvent.change(utils.getByLabelText(p.themeImportTitle), {
    target: { value: JSON.stringify(bundle) },
  });
  fireEvent.click(utils.getByText(p.themeConfirmImport));
}

describe("ThemeSettings · import", () => {
  it("imports a pasted bundle, lists it, and lands it on the server", async () => {
    setToken("owner-token");
    const utils = await renderManage();
    await importBundle(utils, {
      id: "midnight",
      name: "午夜藍",
      colors: { "--color-accent": "#0b1020" },
    });
    expect(await utils.findByText("午夜藍")).toBeTruthy();
    const srv = await api.getServerSettings();
    expect(srv.customThemes.map((b) => b.id)).toContain("midnight");

    // WHICH KIND a theme is comes from the group it sits in — the rows
    // themselves carry no 內建/自訂 chip (the heading already says it).
    expect(utils.container.querySelectorAll(".ts-tag").length).toBe(0);
    const rows = Array.from(utils.container.querySelectorAll(".ts-row"));
    const rowOf = (name: string) =>
      rows.find((r) => r.textContent?.includes(name));
    const headOf = (name: string) =>
      rowOf(name)?.closest(".ts-list")?.querySelector(".ts-group-head")
        ?.textContent;
    expect(headOf(zh.themeIdentity.office)).toBe(zh.themeMarkers.builtinGroup);
    expect(headOf("午夜藍")).toBe(zh.themeMarkers.customGroup);
  });

  it("puts the built-in and the custom rows in separate labelled groups", async () => {
    // Round 4 (BLOCKER-A): the row CHIP alone was forgeable — a pack controlled
    // its text (settings.themeBuiltinTag was overridable), its colour
    // (--color-seg-fill / --color-icon-violet-bg) and the row's own name, so two
    // identical 「辦公室 [內建]」 rows could be produced. The grouping is what a
    // theme cannot reach: it is structure, and its labels come from the
    // non-overridable themeMarkers subtree — the same source the quick picker's
    // <optgroup> uses.
    setToken("owner-token");
    const utils = await renderManage();
    await importBundle(utils, {
      id: "midnight",
      name: "午夜藍",
      colors: { "--color-accent": "#0b1020" },
    });
    await utils.findByText("午夜藍");

    const groupOf = (name: string) => {
      const row = Array.from(utils.container.querySelectorAll(".ts-row")).find(
        (r) => r.textContent?.includes(name)
      );
      const list = row?.closest(".ts-list");
      const head = list?.querySelector(".ts-group-head");
      return { head: head?.textContent, labelled: list?.getAttribute("aria-labelledby"), id: head?.id };
    };

    const builtin = groupOf(zh.themeIdentity.office);
    expect(builtin.head).toBe(zh.themeMarkers.builtinGroup);
    expect(builtin.labelled).toBe(builtin.id);

    const custom = groupOf("午夜藍");
    expect(custom.head).toBe(zh.themeMarkers.customGroup);
    expect(custom.labelled).toBe(custom.id);

    // Two DIFFERENT groups — a custom row never lands under the built-in
    // heading, whatever it is called.
    expect(custom.id).not.toBe(builtin.id);
  });

  it("cannot be made to show two identical built-in rows by a theme's wording, colours or name", async () => {
    // The whole round-3 BLOCKER-2 / round-4 BLOCKER-A recipe in one go: forge
    // the marker TEXT through `wording`, forge the marker COLOUR through the
    // tokens the markers used to read, and name the pack so it renders as the
    // built-in.
    setToken("owner-token");
    const utils = await renderManage();
    await importBundle(utils, {
      id: "forge",
      name: `${zh.themeIdentity.office}(${zh.themeMarkers.builtinGroup})`,
      colors: {
        "--color-seg-fill": "#8b7ae8",
        "--color-icon-violet-bg": "#8b7ae8",
      },
      // EVERY overridable message code re-valued to a sentinel, plus a direct
      // shot at the marker subtree. If the headings or the chips read any key a
      // `wording` overlay can reach, the sentinel shows up on screen.
      wording: {
        zh: {
          ...Object.fromEntries(MESSAGE_KEYS.map((k) => [k, SENTINEL])),
          "themeMarkers.builtinGroup": zh.themeMarkers.customGroup,
          "themeMarkers.customGroup": zh.themeMarkers.builtinGroup,
        },
      },
    });
    const forged = await utils.findByText(
      `${zh.themeIdentity.office}(${zh.themeMarkers.builtinGroup})`
    );
    // Make the forging pack the ACTIVE theme, so its wording overlay is live.
    await act(async () => {
      fireEvent.click(forged);
      await new Promise((r) => setTimeout(r, 0));
    });

    // Only the STRUCTURE marks a row now, so the forgery has exactly one thing
    // left to beat: which group the row landed in.
    expect(utils.container.querySelectorAll(".ts-tag").length).toBe(0);

    // The headings are UNCHANGED — the wording overlay was dropped, so 內建 still
    // means 內建 …
    expect(utils.getByTestId("ts-group-builtin").textContent).toBe(
      zh.themeMarkers.builtinGroup
    );
    expect(utils.getByTestId("ts-group-custom").textContent).toBe(
      zh.themeMarkers.customGroup
    );
    // … and the forged row sits under 自訂, not under 內建, whatever it called
    // itself.
    expect(forged.closest(".ts-list")?.querySelector(".ts-group-head")?.textContent).toBe(
      zh.themeMarkers.customGroup
    );
    // The 內建 group holds the shipped office row and NOTHING else — a pack
    // cannot add a row to it.
    const builtinList = utils.getByTestId("ts-group-builtin").closest(".ts-list");
    const builtinRows = Array.from(builtinList?.querySelectorAll(".ts-row") ?? []);
    expect(builtinRows.length).toBe(1);
    expect(builtinRows[0].textContent).toContain(zh.themeIdentity.office);
    expect(builtinRows[0].contains(forged)).toBe(false);

    // The group headings draw their colour from the non-overridable slots, so
    // the tokens a pack can re-value cannot reach them (round 4 recheck,
    // NIT-1): the heading read the pack-settable --color-text-muted, so a pack
    // could set that to the page colour and make BOTH 內建/自訂 headings
    // disappear.
    const css = readFileSync(
      join(dirname(fileURLToPath(import.meta.url)), "theme-settings.css"),
      "utf8"
    );
    const blockOf = (selector: string) => {
      const at = css.indexOf(`${selector} {`);
      expect(at, selector).toBeGreaterThan(-1);
      return css.slice(at, css.indexOf("}", at) + 1);
    };
    const marked = blockOf(".ts-group-head");
    for (const token of [
      "--color-seg-fill",
      "--color-icon-violet-bg",
      "--color-bg",
      "--color-text",
      "--color-text-muted",
    ]) {
      expect(marked, marked).not.toContain(`var(${token})`);
    }
    for (const token of marked.match(/var\(\s*(--[\w-]+)/g) ?? []) {
      expect(token.replace(/var\(\s*/, ""), marked).toMatch(/^--color-marker-/);
    }
  });

  it("keeps the marker colour slots out of the pack-settable token whitelist", async () => {
    // The colour half of BLOCKER-A. --color-marker-* is excluded by NAME at the
    // generator, so a bundle naming one is not a theme colour token at all.
    for (const token of THEME_COLOR_TOKENS) {
      expect(token.startsWith("--color-marker-")).toBe(false);
    }
    expect(
      validateThemeBundle({
        id: "forge",
        name: "Forge",
        colors: { "--color-marker-fg": "#6076ba" },
      })
    ).toMatch(/is not a theme colour token/);
  });

  it("imports a pack with unrecognised wording codes and warns which were skipped", async () => {
    setToken("owner-token");
    const utils = await renderManage();
    await importBundle(utils, {
      id: "elfvillage",
      name: "精靈村",
      colors: { "--color-accent": "#0b1020" },
      wording: {
        zh: { "nav.tasks": "任務榜", "profile.themeOffice": "精靈村", "typo.not.a.key": "x" },
      },
    });
    // The import SUCCEEDED — the pack is listed and landed on the server.
    expect(await utils.findByText("精靈村")).toBeTruthy();
    const srv = await api.getServerSettings();
    expect(srv.customThemes.map((b) => b.id)).toContain("elfvillage");
    // …and the recognised override survived while the unknown ones did not.
    expect(srv.customThemes[0].wording).toEqual({ zh: { "nav.tasks": "任務榜" } });
    // …and the drop is named on screen instead of being silent.
    expect(utils.getByTestId("theme-import-skipped").textContent).toBe(
      makeMessages(zh, "zh").themeImportSkipped(2, [
        "profile.themeOffice",
        "typo.not.a.key",
      ])
    );
  });

  it("names only the first few skipped codes and lets the count carry the rest", async () => {
    setToken("owner-token");
    const utils = await renderManage();
    const junk: Record<string, string> = {};
    for (let i = 1; i <= 30; i++) junk[`junk.key.${i}`] = "x";
    await importBundle(utils, {
      id: "noisy",
      name: "吵雜",
      colors: { "--color-accent": "#0b1020" },
      wording: { zh: junk },
    });
    expect(await utils.findByText("吵雜")).toBeTruthy();
    expect(utils.getByTestId("theme-import-skipped").textContent).toBe(
      makeMessages(zh, "zh").themeImportSkipped(30, [
        "junk.key.1",
        "junk.key.2",
        "junk.key.3",
      ])
    );
  });

  it("blocks an injection-shaped bundle inline and never reaches the server", async () => {
    const utils = await renderManage();
    await importBundle(utils, {
      id: "evil",
      name: "Evil",
      colors: { "--color-bg": "red; } body { background: url(x)" },
    });
    expect(utils.getByLabelText(p.themeImportTitle)).toBeTruthy();
    expect(utils.container.querySelector(".set-error")).toBeTruthy();
    const srv = await api.getServerSettings();
    expect(srv.customThemes).toHaveLength(0);
  });
});

describe("ThemeSettings · colour editing", () => {
  it("shows friendly names grouped by purpose — never the raw --color-* token", async () => {
    const utils = await renderManage();
    await importBundle(utils, {
      id: "midnight",
      name: "午夜藍",
      colors: { "--color-accent": "#0b1020", "--color-bg": "#040506" },
    });
    fireEvent.click(await utils.findByLabelText(`${p.themeEdit} 午夜藍`));

    // The friendly group + label are shown; the raw token is not visible text.
    const colorSection = utils.container.querySelector(".ts-color-group__label");
    expect(colorSection?.textContent).toBe("主色"); // brand group heading
    expect(utils.getAllByText("主色").length).toBeGreaterThan(0); // group + accent label
    expect(utils.queryByText("--color-accent")).toBeNull();
  });

  it("round-trips an edited colour value through save", async () => {
    setToken("owner-token");
    const utils = await renderManage();
    await importBundle(utils, {
      id: "midnight",
      name: "午夜藍",
      colors: { "--color-accent": "#0b1020" },
    });
    fireEvent.click(await utils.findByLabelText(`${p.themeEdit} 午夜藍`));
    // The value text field carries the friendly label as its accessible name.
    fireEvent.change(utils.getByLabelText("主色"), {
      target: { value: "#ffffff" },
    });
    fireEvent.click(utils.getByRole("button", { name: p.save }));

    const srv = await api.getServerSettings();
    const b = srv.customThemes.find((x) => x.id === "midnight");
    expect(b?.colors["--color-accent"]).toBe("#ffffff");
  });
});

describe("ThemeSettings · wording overlay", () => {
  it("stores a wording override and lands it on the server bundle", async () => {
    setToken("owner-token");
    const utils = await renderManage();
    await importBundle(utils, {
      id: "midnight",
      name: "午夜藍",
      colors: { "--color-accent": "#0b1020" },
    });
    fireEvent.click(await utils.findByLabelText(`${p.themeEdit} 午夜藍`));

    // Narrow the (large) wording list to exactly one code by searching the code.
    fireEvent.change(utils.getByLabelText(s.themeWordingSearch), {
      target: { value: "common.apply" },
    });
    const list = utils.container.querySelector(
      ".ts-wording-list"
    ) as HTMLElement;
    const input = within(list).getByRole("textbox");
    fireEvent.change(input, { target: { value: "套用替代" } });
    fireEvent.click(utils.getByRole("button", { name: p.save }));

    const srv = await api.getServerSettings();
    const b = srv.customThemes.find((x) => x.id === "midnight");
    expect(b?.wording?.zh?.["common.apply"]).toBe("套用替代");
  });

  it("keeps the boundary spaces of a sentence-fragment override", async () => {
    // Several codes T-081b made overridable are sentence FRAGMENTS whose
    // leading/trailing space is load-bearing: uninstallWarnBody2 is 「」上還有 」
    // and Body3 opens with a space, so the composed sentence reads
    // 「Alpha」上還有 3 位成員…. Trimming what the owner typed would make the
    // product's own editor render 「上還有3位成員」 — the editor corrupting the
    // very strings the ticket just opened up.
    setToken("owner-token");
    const utils = await renderManage();
    await importBundle(utils, {
      id: "midnight",
      name: "午夜藍",
      colors: { "--color-accent": "#0b1020" },
    });
    fireEvent.click(await utils.findByLabelText(`${p.themeEdit} 午夜藍`));

    fireEvent.change(utils.getByLabelText(s.themeWordingSearch), {
      target: { value: "uninstallWarnBody2" },
    });
    const list = utils.container.querySelector(
      ".ts-wording-list"
    ) as HTMLElement;
    fireEvent.change(within(list).getByRole("textbox"), {
      target: { value: "」上頭還有 " },
    });
    fireEvent.click(utils.getByRole("button", { name: p.save }));

    const srv = await api.getServerSettings();
    const b = srv.customThemes.find((x) => x.id === "midnight");
    const stored = b?.wording?.zh?.["monitor.machine.uninstallWarnBody2"];
    expect(stored).toBe("」上頭還有 ");

    // …and the fragment composes into a sentence that still has its spaces.
    const themed = { ...zh, monitor: { ...zh.monitor, machine: { ...zh.monitor.machine, uninstallWarnBody2: stored! } } };
    expect(makeMessages(themed, "zh").machineUninstallWarnBody("Alpha", 3)).toBe(
      "「Alpha」上頭還有 3 位成員在線上。現在解除安裝會在成員仍在這台機器上時把 warden 拆除 —— 建議先將相關成員下線。仍要繼續嗎?"
    );
  });
});

describe("ThemeSettings · alias-default colours", () => {
  it("offers the zone/split tokens a bundle never carries, and saves only the touched one", async () => {
    setToken("owner-token");
    const utils = await renderManage();
    await importBundle(utils, {
      id: "midnight",
      name: "午夜藍",
      colors: { "--color-accent": "#0b1020" },
    });
    fireEvent.click(await utils.findByLabelText(`${p.themeEdit} 午夜藍`));

    // The content-area background follows --color-bg, so no bundle ever exports
    // it — yet it must be reachable here, or only a hand-edited JSON can set it.
    const mainBg = utils.getByLabelText(tokenMeta("--color-main-bg", "zh").label);
    expect((mainBg as HTMLInputElement).value).toBe("");
    fireEvent.change(mainBg, { target: { value: "#12345680" } });
    fireEvent.click(utils.getByRole("button", { name: p.save }));

    const b = (await api.getServerSettings()).customThemes.find(
      (x) => x.id === "midnight"
    );
    expect(b?.colors["--color-main-bg"]).toBe("#12345680");
    // …and the ones left alone stay ABSENT rather than baked to a literal —
    // that is what keeps them following their parent.
    expect("--color-nav-bg" in (b?.colors ?? {})).toBe(false);
    expect("--color-knob" in (b?.colors ?? {})).toBe(false);
  });

  it("edits opacity through a slider, not only through hand-typed #RRGGBBAA", async () => {
    setToken("owner-token");
    const utils = await renderManage();
    await importBundle(utils, {
      id: "midnight",
      name: "午夜藍",
      colors: { "--color-card": "#242832" },
    });
    fireEvent.click(await utils.findByLabelText(`${p.themeEdit} 午夜藍`));

    const label = tokenMeta("--color-card", "zh").label;
    const slider = utils.getByLabelText(`${label} ${s.themeColorOpacity}`);
    expect((slider as HTMLInputElement).value).toBe("100");
    fireEvent.change(slider, { target: { value: "40" } });
    fireEvent.click(utils.getByRole("button", { name: p.save }));

    const b = (await api.getServerSettings()).customThemes.find(
      (x) => x.id === "midnight"
    );
    expect(b?.colors["--color-card"]).toBe("#24283266");
  });
});

describe("ThemeSettings · outer-canvas background", () => {
  const png =
    "data:image/png;base64," +
    btoa(
      String.fromCharCode(0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a, 0x00, 0x01)
    );

  it("stores a non-default lay-down mode and drops it again on tile", async () => {
    setToken("owner-token");
    const utils = await renderManage();
    await importBundle(utils, {
      id: "midnight",
      name: "午夜藍",
      colors: { "--color-accent": "#0b1020" },
      backgrounds: { canvas: png },
    });
    fireEvent.click(await utils.findByLabelText(`${p.themeEdit} 午夜藍`));

    const mode = utils.getByLabelText(s.themeCanvasBgMode);
    fireEvent.change(mode, { target: { value: "sides" } });
    fireEvent.click(utils.getByRole("button", { name: p.save }));

    let b = (await api.getServerSettings()).customThemes.find(
      (x) => x.id === "midnight"
    );
    expect(b?.backgroundModes).toEqual({ canvas: "sides" });

    // Back to the default and the field disappears entirely — a tiling theme
    // stays byte-identical to one authored before the field existed.
    fireEvent.click(await utils.findByLabelText(`${p.themeEdit} 午夜藍`));
    fireEvent.change(utils.getByLabelText(s.themeCanvasBgMode), {
      target: { value: "tile" },
    });
    fireEvent.click(utils.getByRole("button", { name: p.save }));

    b = (await api.getServerSettings()).customThemes.find(
      (x) => x.id === "midnight"
    );
    expect(b?.backgroundModes).toBeUndefined();
    expect(b?.backgrounds).toEqual({ canvas: png });
  });

  it("offers no lay-down mode until there is an image to lay down", async () => {
    setToken("owner-token");
    const utils = await renderManage();
    await importBundle(utils, {
      id: "plain",
      name: "純色",
      colors: { "--color-accent": "#0b1020" },
    });
    fireEvent.click(await utils.findByLabelText(`${p.themeEdit} 純色`));

    expect(utils.queryByLabelText(s.themeCanvasBgMode)).toBeNull();
  });
});

describe("ThemeSettings · delete", () => {
  it("deletes a custom theme via the confirm modal", async () => {
    setToken("owner-token");
    const utils = await renderManage();
    await importBundle(utils, {
      id: "midnight",
      name: "午夜藍",
      colors: { "--color-accent": "#0b1020" },
    });
    fireEvent.click(await utils.findByLabelText(`${p.themeDelete} 午夜藍`));
    fireEvent.click(utils.getByTestId("theme-delete-confirm-btn"));

    expect(utils.queryByText("午夜藍")).toBeNull();
    const srv = await api.getServerSettings();
    expect(srv.customThemes).toHaveLength(0);
  });
});

describe("ThemeSettings · export", () => {
  // jsdom's URL has no object-URL helpers; downloadBundle needs them, so provide
  // stubs and clean them up.
  afterEach(() => {
    vi.restoreAllMocks();
    delete (URL as { createObjectURL?: unknown }).createObjectURL;
    delete (URL as { revokeObjectURL?: unknown }).revokeObjectURL;
  });

  it("has no toolbar 匯出 button — export is per-row download only", async () => {
    const utils = await renderManage();
    // The toolbar keeps 新增 + 匯入; the standalone 匯出 button is gone.
    expect(utils.getByText(p.themeAdd)).toBeTruthy();
    expect(utils.getByText(p.themeImport)).toBeTruthy();
    expect(utils.queryByText(p.themeExport)).toBeNull();
  });

  it("office 列下載鈕可用,下載一個非保留 id 的 office 包(可再匯入)", async () => {
    const utils = await renderManage();
    const btn = utils.getByLabelText(
      `${p.themeExport} ${zh.themeIdentity.office}`
    ) as HTMLButtonElement;
    expect(btn.disabled).toBe(false);

    const createFn = vi.fn().mockReturnValue("blob:office");
    (URL as { createObjectURL: unknown }).createObjectURL = createFn;
    (URL as { revokeObjectURL: unknown }).revokeObjectURL = vi.fn();
    let downloadName = "";
    vi.spyOn(HTMLAnchorElement.prototype, "click").mockImplementation(function (
      this: HTMLAnchorElement
    ) {
      downloadName = this.download;
    });

    fireEvent.click(btn);

    expect(createFn).toHaveBeenCalledTimes(1);
    // The download uses id "office-base", NOT the reserved built-in "office"
    // (which validateThemeBundle rejects), so the bundle re-imports.
    expect(downloadName).toBe("officraft-theme-office-base.json");
    const text = await new Promise<string>((resolve) => {
      const reader = new FileReader();
      reader.onload = () => resolve(String(reader.result));
      reader.readAsText(createFn.mock.calls[0][0] as Blob);
    });
    const payload = JSON.parse(text);
    expect(payload.id).toBe("office-base");
    // …and under a COPY name, not the built-in's own: since T-081b a bundle may
    // not claim a built-in display name, so exporting under it would hand the
    // owner a file the product then refuses to import back.
    expect(payload.name).toBe(
      makeMessages(zh, "zh").themeCopyName(zh.themeIdentity.office)
    );
    expect(payload.name).not.toBe(zh.themeIdentity.office);
    // (that this name actually re-imports is pinned in themeExport.test.ts —
    // jsdom has no stylesheet, so the payload here carries no colours to import)
  });
});
