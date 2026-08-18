// Dual-layer theme/language (T-0b41-p2): the localStorage pre-auth CACHE drives
// the first paint (zero-flash, server unreachable pre-auth), and the server is
// the cross-device TRUTH reconciled in at login — applied to state + written
// back to the cache. These jsdom tests cover the state/localStorage/<html
// data-theme> reconcile wiring; the geometry-free bits a real browser is not
// needed for (the visual pre-auth guard lives in the Playwright CT suite).

import { describe, it, expect, beforeEach } from "vitest";
import { render, screen, waitFor, act } from "@testing-library/react";
import { I18nProvider, useI18n } from "./index";
import { zh } from "./locales/zh";
import { readDictMessage } from "./wording";
import { mockApi, __resetMock } from "../api/mock";
import { setToken, TOKEN_KEY } from "../api/auth";

function Probe() {
  const { theme, language } = useI18n();
  return <div data-testid="probe" data-theme={theme} data-lang={language} />;
}

let ctx = null as unknown as ReturnType<typeof useI18n>;
function Capture() {
  ctx = useI18n();
  return null;
}

const MIDNIGHT = {
  id: "midnight",
  name: "Midnight",
  colors: { "--color-accent": "#010203", "--color-bg": "#040506" },
};
const SUNRISE = {
  id: "sunrise",
  name: "Sunrise",
  colors: { "--color-accent": "#ffaa00" },
};

describe("I18nProvider dual-layer theme/language", () => {
  beforeEach(() => {
    __resetMock();
    localStorage.clear();
    delete document.documentElement.dataset.theme;
  });

  it("first paint uses the localStorage cache when pre-auth (no server call)", () => {
    // A cached custom-theme id (office is the only built-in now; a non-default
    // theme is a custom bundle id). Pre-reconcile the bundle isn't loaded yet,
    // so the apply effect paints the neutral office base — but the theme STATE
    // (the cache) is what drives the value.
    localStorage.setItem("oc.theme", "midnight");
    localStorage.setItem("oc.language", "en");
    // No token → /api/settings is unreachable; the cache is the only truth.
    render(
      <I18nProvider>
        <Probe />
      </I18nProvider>
    );
    expect(screen.getByTestId("probe").dataset.theme).toBe("midnight");
    expect(screen.getByTestId("probe").dataset.lang).toBe("en");
    // A dangling custom id (bundle not yet reconciled) paints the office base.
    expect(document.documentElement.dataset.theme).toBe("office");
  });

  it("adopts the server value on mount when a token already exists, writing it back to the cache", async () => {
    // Cache is empty (defaults office/zh); the server holds the owner's choice —
    // a custom theme plus display.theme pointing at it (so the id is selectable).
    await mockApi.putTheme(MIDNIGHT);
    await mockApi.patchServerSettings({
      displayTheme: "midnight",
      displayLanguage: "en",
    });
    localStorage.setItem(TOKEN_KEY, "live-owner-token");
    render(
      <I18nProvider>
        <Probe />
      </I18nProvider>
    );
    await waitFor(() =>
      expect(screen.getByTestId("probe").dataset.theme).toBe("midnight")
    );
    expect(screen.getByTestId("probe").dataset.lang).toBe("en");
    // Written back so the NEXT pre-auth first paint is already correct.
    expect(localStorage.getItem("oc.theme")).toBe("midnight");
    expect(localStorage.getItem("oc.language")).toBe("en");
  });

  it("reconciles when a login mints a token mid-session (oc-auth-login)", async () => {
    // Pre-auth first paint: default office/zh, server not yet reachable.
    await mockApi.putTheme(MIDNIGHT);
    await mockApi.patchServerSettings({
      displayTheme: "midnight",
      displayLanguage: "en",
    });
    render(
      <I18nProvider>
        <Probe />
      </I18nProvider>
    );
    expect(screen.getByTestId("probe").dataset.theme).toBe("office");
    // A login mints the token, which fires oc-auth-login from setToken → reconcile.
    await act(async () => {
      setToken("fresh-owner-token");
    });
    await waitFor(() =>
      expect(screen.getByTestId("probe").dataset.theme).toBe("midnight")
    );
    expect(screen.getByTestId("probe").dataset.lang).toBe("en");
  });

  it("keeps the cache when the server pref is unset (\"\")", async () => {
    localStorage.setItem("oc.theme", "midnight");
    localStorage.setItem(TOKEN_KEY, "live-owner-token");
    // Server default is "" for both — an unset server value must NOT clobber the
    // cache back to a default.
    render(
      <I18nProvider>
        <Probe />
      </I18nProvider>
    );
    // Give the reconcile a chance to (wrongly) run, then assert it left the cache.
    await Promise.resolve();
    await waitFor(() =>
      expect(screen.getByTestId("probe").dataset.theme).toBe("midnight")
    );
    expect(localStorage.getItem("oc.theme")).toBe("midnight");
  });
});

describe("I18nProvider layout width (T-756f)", () => {
  const root = document.documentElement;

  beforeEach(() => {
    __resetMock();
    localStorage.clear();
    delete root.dataset.layout;
  });

  it("defaults to narrow and leaves the DOM exactly as it was", () => {
    render(
      <I18nProvider>
        <Capture />
      </I18nProvider>
    );
    expect(ctx.wide).toBe(false);
    // The narrow default must be the ABSENCE of the attribute — that is what
    // guarantees the shipped chrome is untouched for anyone who never opted in.
    expect(root.hasAttribute("data-layout")).toBe(false);
  });

  it("applies <html data-layout=\"wide\"> when switched on, and removes it again", () => {
    render(
      <I18nProvider>
        <Capture />
      </I18nProvider>
    );
    act(() => ctx.setWide(true));
    expect(root.dataset.layout).toBe("wide");
    act(() => ctx.setWide(false));
    expect(root.hasAttribute("data-layout")).toBe(false);
  });

  it("caches the choice to localStorage for the pre-auth first paint", () => {
    render(
      <I18nProvider>
        <Capture />
      </I18nProvider>
    );
    act(() => ctx.setWide(true));
    expect(localStorage.getItem("oc.wide")).toBe("true");
  });

  it("first paint uses the localStorage cache when pre-auth", () => {
    localStorage.setItem("oc.wide", "true");
    render(
      <I18nProvider>
        <Capture />
      </I18nProvider>
    );
    expect(ctx.wide).toBe(true);
    expect(root.dataset.layout).toBe("wide");
  });

  it("adopts the server value at login, writing it back to the cache", async () => {
    await mockApi.patchServerSettings({ displayWide: true });
    localStorage.setItem(TOKEN_KEY, "live-owner-token");
    render(
      <I18nProvider>
        <Capture />
      </I18nProvider>
    );
    await waitFor(() => expect(ctx.wide).toBe(true));
    expect(root.dataset.layout).toBe("wide");
    expect(localStorage.getItem("oc.wide")).toBe("true");
  });

  it("lets the server turn wide back OFF across devices (no \"\" third state)", async () => {
    // This device cached wide; the owner turned it off elsewhere, so the server
    // says false. Unlike theme/language there is no unset value meaning "keep
    // the cache" — the server bool is simply adopted.
    localStorage.setItem("oc.wide", "true");
    localStorage.setItem(TOKEN_KEY, "live-owner-token");
    render(
      <I18nProvider>
        <Capture />
      </I18nProvider>
    );
    await waitFor(() => expect(ctx.wide).toBe(false));
    expect(localStorage.getItem("oc.wide")).toBe("false");
    expect(root.hasAttribute("data-layout")).toBe(false);
  });

  it("resetPreferences drops back to narrow (logout must not tint the next owner)", () => {
    localStorage.setItem("oc.wide", "true");
    render(
      <I18nProvider>
        <Capture />
      </I18nProvider>
    );
    expect(ctx.wide).toBe(true);
    act(() => ctx.resetPreferences());
    expect(ctx.wide).toBe(false);
    expect(root.hasAttribute("data-layout")).toBe(false);
  });
});


// [T-83ef] The provider no longer holds every bundle, so the door these claims
// happen through changed: a theme is SAVED to its own resource, and switching to
// it FETCHES that one bundle (`api.getTheme`). Every claim below is the same one
// the whole-set `commitCustomThemes` used to assert — only the door moved.
// Fetching is gated on hasToken(), which is why these mount with a token.
const PNG =
  "data:image/png;base64," +
  btoa(
    String.fromCharCode(0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a, 0x00, 0x01)
  );

/** Mount the provider and WAIT FOR THE LOGIN RECONCILE to land — it is what
 * fills `themeList`, and it also settles the active bundle (null here: the
 * server's display_theme is unset). Racing it would let a late reconcile clear
 * the bundle a switch had just fetched, i.e. the test would be measuring the
 * race rather than the switch. `saved` is how many themes were seeded. */
async function mountCapture(saved: number) {
  await act(async () => {
    render(
      <I18nProvider>
        <Capture />
      </I18nProvider>
    );
  });
  await waitFor(() => expect(ctx.themeList).toHaveLength(saved));
}

/** Switch the cockpit to a SAVED theme and wait for its one-bundle fetch to
 * land — the fetch is what the whole-set provider used to do without a round
 * trip, so it is the step the assertions now have to wait on. */
async function activate(id: string) {
  // SYNC act on purpose: in the browser the switch comes from a click, so React
  // has already committed the new active id by the time the bundle fetch
  // resolves. An async act would let the fetch land first and the provider's
  // "ignore a fetch that lost a race to a later switch" guard would (correctly)
  // drop it — the test would then be measuring an ordering the product does not
  // have.
  act(() => {
    ctx.setTheme(id);
  });
  await waitFor(() => expect(ctx.activeThemeBundle?.id).toBe(id));
}

/** Switch to a theme that resolves to NO bundle (the built-in, or an id the
 * server does not know) — there is nothing to wait for. */
async function activateBundleless(id: string) {
  act(() => {
    ctx.setTheme(id);
  });
  await waitFor(() => expect(ctx.theme).toBe(id));
}

describe("I18nProvider custom theme apply", () => {
  const root = document.documentElement;

  beforeEach(() => {
    __resetMock();
    localStorage.clear();
    root.removeAttribute("style");
    delete root.dataset.theme;
    localStorage.setItem(TOKEN_KEY, "live-owner-token");
  });

  it("applies a custom bundle as inline vars over the neutral office base", async () => {
    await mockApi.putTheme(MIDNIGHT);
    await mountCapture(1);
    await activate("midnight");

    expect(root.dataset.theme).toBe("office");
    expect(root.style.getPropertyValue("--color-accent")).toBe("#010203");
    expect(root.style.getPropertyValue("--color-bg")).toBe("#040506");
  });

  it("clears the previous custom vars when switching to the built-in office", async () => {
    await mockApi.putTheme(MIDNIGHT);
    await mountCapture(1);
    await activate("midnight");
    await activateBundleless("office");

    expect(root.dataset.theme).toBe("office");
    expect(root.style.getPropertyValue("--color-accent")).toBe("");
    expect(root.style.getPropertyValue("--color-bg")).toBe("");
  });

  it("drops vars not carried by the next custom theme when switching between them", async () => {
    await mockApi.putTheme(MIDNIGHT);
    await mockApi.putTheme(SUNRISE);
    await mountCapture(2);
    await activate("midnight");
    await activate("sunrise");

    expect(root.style.getPropertyValue("--color-accent")).toBe("#ffaa00");
    expect(root.style.getPropertyValue("--color-bg")).toBe("");
  });

  it("falls back to the office base for a dangling custom id", async () => {
    await mountCapture(0);
    // Nothing is saved under this id — the fetch 404s and the picker keeps the
    // office base rather than painting something invented.
    await activateBundleless("ghost");

    expect(root.dataset.theme).toBe("office");
    expect(root.style.getPropertyValue("--color-accent")).toBe("");
  });

  it("applies the canvas background image alongside the canvas colour (T-081b)", async () => {
    await mockApi.putTheme({ ...MIDNIGHT, backgrounds: { canvas: PNG } });
    await mountCapture(1);
    await activate("midnight");

    expect(root.style.getPropertyValue("--canvas-bg-image")).toBe(`url("${PNG}")`);
    expect(root.style.getPropertyValue("--color-bg")).toBe("#040506");
    // no mode named ⇒ the tiling values theme.css already defaults to
    expect(root.style.getPropertyValue("--canvas-bg-repeat")).toBe("repeat");
    expect(root.style.getPropertyValue("--canvas-bg-position")).toBe("0 0");
    expect(root.style.getPropertyValue("--canvas-bg-size")).toBe("auto");
    expect(root.style.getPropertyValue("--canvas-bg-attachment")).toBe("scroll");

    // "sides" lays the same url twice — once against each viewport edge, ONE
    // copy each (a repeat would put a second tree down a long page). The editor's
    // save path is saveTheme: same id, new content, repaint because it is active.
    await act(async () => {
      await ctx.saveTheme({
        ...MIDNIGHT,
        backgrounds: { canvas: PNG },
        backgroundModes: { canvas: "sides" },
      });
    });
    expect(root.style.getPropertyValue("--canvas-bg-image")).toBe(
      `url("${PNG}"), url("${PNG}")`
    );
    expect(root.style.getPropertyValue("--canvas-bg-repeat")).toBe(
      "no-repeat, no-repeat"
    );
    expect(root.style.getPropertyValue("--canvas-bg-position")).toBe(
      "left bottom, right bottom"
    );
    // pinned to the viewport, so a long page cannot scroll a second copy in
    expect(root.style.getPropertyValue("--canvas-bg-attachment")).toBe(
      "fixed, fixed"
    );

    // "cover" scales ONE copy to the whole viewport
    await act(async () => {
      await ctx.saveTheme({
        ...MIDNIGHT,
        backgrounds: { canvas: PNG },
        backgroundModes: { canvas: "cover" },
      });
    });
    expect(root.style.getPropertyValue("--canvas-bg-image")).toBe(`url("${PNG}")`);
    expect(root.style.getPropertyValue("--canvas-bg-repeat")).toBe("no-repeat");
    expect(root.style.getPropertyValue("--canvas-bg-size")).toBe("cover");
    expect(root.style.getPropertyValue("--canvas-bg-attachment")).toBe("fixed");

    // and all five are cleared again when the next theme carries no image
    await activateBundleless("office");
    expect(root.style.getPropertyValue("--canvas-bg-image")).toBe("");
    expect(root.style.getPropertyValue("--canvas-bg-repeat")).toBe("");
    expect(root.style.getPropertyValue("--canvas-bg-position")).toBe("");
    expect(root.style.getPropertyValue("--canvas-bg-size")).toBe("");
    expect(root.style.getPropertyValue("--canvas-bg-attachment")).toBe("");
  });

  it("caches a custom active id to localStorage", async () => {
    await mockApi.putTheme(MIDNIGHT);
    await mountCapture(1);
    await activate("midnight");
    expect(localStorage.getItem("oc.theme")).toBe("midnight");
  });

  it("keeps a custom theme visual-only — copy follows the language toggle", async () => {
    await mockApi.putTheme(MIDNIGHT);
    await mountCapture(1);
    await act(async () => ctx.setLanguage("en"));
    await activate("midnight");
    expect(ctx.locale).toBe("en");
  });

  it("keeps the locale following the language toggle regardless of theme (theme↔locale decoupled)", async () => {
    await mountCapture(0);
    await act(async () => ctx.setLanguage("zh"));
    await activateBundleless("office");
    expect(ctx.locale).toBe("zh");
    await act(async () => ctx.setLanguage("en"));
    expect(ctx.locale).toBe("en");
  });
});

describe("I18nProvider custom theme wording overlay", () => {
  const WORDED = {
    id: "worded",
    name: "Worded",
    colors: { "--color-accent": "#123456" },
    wording: {
      zh: { "nav.tasks": "任務榜" },
      en: { "nav.tasks": "Quest Board" },
    },
  };

  beforeEach(() => {
    __resetMock();
    localStorage.clear();
    document.documentElement.removeAttribute("style");
    delete document.documentElement.dataset.theme;
    localStorage.setItem(TOKEN_KEY, "live-owner-token");
  });

  it("overrides an overridden code in the active language, leaving others intact", async () => {
    await mockApi.putTheme(WORDED);
    await mountCapture(1);
    await activate("worded");
    // zh is the default language → the zh override applies.
    expect(ctx.t.nav.tasks).toBe("任務榜");
    // A non-overridden code keeps its base value (fallback = original language).
    expect(ctx.t.nav.monitor).toBe(zh.nav.monitor);
  });

  it("follows the language toggle for the overlay language", async () => {
    await mockApi.putTheme(WORDED);
    await mountCapture(1);
    await act(async () => ctx.setLanguage("en"));
    await activate("worded");
    expect(ctx.t.nav.tasks).toBe("Quest Board");
  });

  it("restores the base wording when switching away from the theme", async () => {
    await mockApi.putTheme(WORDED);
    await mountCapture(1);
    await activate("worded");
    expect(ctx.t.nav.tasks).toBe("任務榜");
    await activateBundleless("office");
    expect(ctx.t.nav.tasks).toBe(zh.nav.tasks);
  });

  it("keeps applying an already-stored pack whose overlay holds an unrecognised code", async () => {
    // Owner requirement 2026-07-27: 已匯入的主題包還是要能夠運作,只是不認得的會失效.
    // A pack imported BEFORE T-081b de-whitelisted the theme-identity keys still
    // sits in the store with those codes in it — the recognised overrides must
    // all still land, and the unrecognised one must simply do nothing.
    await mockApi.putTheme({
      ...WORDED,
      id: "legacy",
      wording: {
        zh: {
          "nav.tasks": "任務榜",
          "nav.replies": "傳訊台",
          "profile.themeOffice": "精靈村",
          "typo.not.a.key": "x",
        },
      },
    });
    await mountCapture(1);
    await activate("legacy");
    expect(ctx.t.nav.tasks).toBe("任務榜");
    expect(ctx.t.nav.replies).toBe("傳訊台");
    // The unrecognised codes are inert: no leaf is invented for them, and the
    // built-in theme keeps its own name.
    expect(readDictMessage(ctx.t, "profile.themeOffice")).toBeNull();
    expect(readDictMessage(ctx.t, "typo.not.a.key")).toBeNull();
    expect(ctx.t.themeIdentity.office).toBe(zh.themeIdentity.office);
  });

  it("leaves a custom theme without wording on the base dict", async () => {
    await mockApi.putTheme(SUNRISE);
    await mountCapture(1);
    await activate("sunrise");
    expect(ctx.t.nav.tasks).toBe(zh.nav.tasks);
  });
});
