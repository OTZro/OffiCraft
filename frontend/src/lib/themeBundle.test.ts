// Unit coverage for the client theme-bundle validator (the twin of the server
// grammar in server/ocserverd/theme_bundle.go). The colour-value grammar is the
// security boundary, so the illegal-value table is the load-bearing case.

import { describe, it, expect } from "vitest";
import {
  isValidColorValue,
  isValidFontValue,
  isValidAvatarValue,
  validateAvatars,
  validateLogo,
  validateNavIcons,
  validateBackgrounds,
  validateBackgroundModes,
  validateThemeBundle,
  validateThemeBundles,
  validateWording,
  isBuiltinThemeName,
  normalizeThemeName,
  RESERVED_THEME_IDS,
  validateFonts,
  isValidDisplayTheme,
  MAX_AVATAR_BYTES,
  MAX_WORDING_ENTRIES_PER_LANG,
} from "./themeBundle";
import { THEME_COLOR_TOKENS } from "../styles/themeTokens.generated";
import { SAFE_FONT_FAMILIES } from "../styles/themeFonts.generated";
import {
  MESSAGE_KEYS,
  THEME_IDENTITY_NAMES,
} from "../i18n/messageKeys.generated";

const aFontStack = SAFE_FONT_FAMILIES[0].stack;

const aKey = MESSAGE_KEYS[0];

const aToken = THEME_COLOR_TOKENS[0];

describe("isValidColorValue", () => {
  it("accepts concrete hex / rgb / rgba / hsl / transparent", () => {
    for (const v of [
      "#fff",
      "#ffff",
      "#101018",
      "#101018ff",
      "rgb(1, 2, 3)",
      "rgba(1, 2, 3, 0.5)",
      "rgba(1 2 3 / 40%)",
      "hsl(120deg, 50%, 40%)",
      "hsla(120, 50%, 40%, 0.5)",
      "transparent",
    ]) {
      expect(isValidColorValue(v)).toBe(true);
    }
  });

  it("rejects CSS-injection and non-concrete values", () => {
    for (const v of [
      "",
      "url(https://evil)",
      "red;}",
      "<script>",
      "expression(1)",
      "var(--x)",
      "color-mix(in srgb, red, blue)",
      "#fff;background:url(x)",
      "red", // a named colour other than transparent
      "f".repeat(70), // over the 64-char cap
    ]) {
      expect(isValidColorValue(v)).toBe(false);
    }
  });
});

describe("validateThemeBundle", () => {
  const ok = { id: "midnight", name: "Midnight", colors: { [aToken]: "#101018" } };

  it("accepts a well-formed bundle", () => {
    expect(validateThemeBundle(ok)).toBeNull();
  });

  it("rejects a bad id, a reserved id, an empty name, and an unknown token", () => {
    expect(validateThemeBundle({ ...ok, id: "Bad Id" })).toMatch(/id must match/);
    expect(validateThemeBundle({ ...ok, id: "office" })).toMatch(/reserved/);
    expect(validateThemeBundle({ ...ok, name: "  " })).toMatch(/name must be/);
    expect(
      validateThemeBundle({ ...ok, colors: { "--color-bogus": "#fff" } })
    ).toMatch(/not a theme colour token/);
    expect(validateThemeBundle({ ...ok, colors: {} })).toMatch(/colors must hold/);
  });

  it("rejects a name carrying control, formatting, private-use, surrogate or line/paragraph separator characters", () => {
    // Written as escapes on purpose: these characters are INVISIBLE, and a
    // reviewer must be able to see which one each case is testing.
    for (const name of [
      "Mid\u0000night", // NUL
      "Mid\u000Anight", // newline
      "Mid\u007Fnight", // DEL
      "Mid\u009Fnight", // C1 control
      "\u202EMidnight", // RIGHT-TO-LEFT OVERRIDE
      "Mid\u202Dnight", // LEFT-TO-RIGHT OVERRIDE
      "Mid\u202Anight", // LEFT-TO-RIGHT EMBEDDING
      "Mid\u2066night", // LEFT-TO-RIGHT ISOLATE
      "Mid\u2069night", // POP DIRECTIONAL ISOLATE
      "Mid\u200Enight", // LEFT-TO-RIGHT MARK
      "Mid\u200Fnight", // RIGHT-TO-LEFT MARK
      // ZERO-WIDTH class (T-081b review round 3, BLOCKER-1). U+FEFF is the
      // load-bearing one: it is the ONE codepoint String.prototype.trim() strips
      // and Go's strings.TrimSpace does not, so while it was left to the trim the
      // AUTHORITATIVE server accepted 「\uFEFF辦公室」 and only this client rejected
      // it. The twin table lives in server/ocserverd/theme_bundle_test.go.
      "\uFEFF辦公室", // BOM prefix — renders as 「辦公室」
      "辦公室\uFEFF", // BOM suffix
      "\uFEFFOffice", // BOM prefix, en spelling
      "Office\uFEFF", // BOM suffix, en spelling
      "辦\u200B公室", // ZERO WIDTH SPACE
      "Off\u200Bice",
      "Off\u200Cice", // ZERO WIDTH NON-JOINER
      "Off\u200Dice", // ZERO WIDTH JOINER
      "Office\u2060", // WORD JOINER
      "Off\u061Cice", // ARABIC LETTER MARK (a bidi char the first list missed)
      // ── round 4, SHOULD-C: the members of the SAME categories the round-3
      // codepoint list never thought of. Listing codepoints is what missed them;
      // the rule is now the CATEGORY (Cc/Cf/Co/Cs/Zl/Zp).
      "Off\u00ADice", // SOFT HYPHEN (Cf) — renders as 「Office」
      "Off\u180Eice", // MONGOLIAN VOWEL SEPARATOR (Cf)
      "Office\u{E0041}", // TAG LATIN CAPITAL A (Cf) — the classic invisible payload
      "Office\uE000", // PRIVATE USE (Co) — renders as whatever the font decides
      "Mid\u2028night", // LINE SEPARATOR (Zl)
      "Mid\u2029night", // PARAGRAPH SEPARATOR (Zp)
    ]) {
      expect(
        validateThemeBundle({ ...ok, name }),
        JSON.stringify(name)
      ).toMatch(
        /control, formatting, private-use, surrogate or line\/paragraph separator/
      );
    }
    // Zs is NOT in that set — every space separator is NORMALISED to U+0020
    // first (round 4 recheck, SHOULD-3). A Zs-padded built-in name is still
    // refused, but now by the rule that can name the actual reason: a user who
    // typed a full-width space is told 「辦公室」 is reserved rather than that
    // their name carries "non-ASCII space characters".
    for (const name of [
      "\u00A0Office\u00A0", // NO-BREAK SPACE (Zs) — renders as 「Office」
      "\u3000辦公室\u3000", // IDEOGRAPHIC SPACE (Zs) — renders as 「辦公室」
      "\u1680Office", // OGHAM SPACE MARK (Zs) — blank in most fonts
    ]) {
      expect(validateThemeBundle({ ...ok, name }), JSON.stringify(name)).toMatch(
        /reserved for a built-in theme/
      );
    }
    // …and a name that is nothing BUT spaces has no name left after the
    // normalise + trim, in every Zs spelling.
    for (const name of ["\u3000", "\u00A0", " \u3000 ", "\u1680\u2000"]) {
      expect(validateThemeBundle({ ...ok, name }), JSON.stringify(name)).toMatch(
        /name must be 1\.\./
      );
    }
  });

  it("rejects a name that claims the built-in theme's display name", () => {
    // Language-independent: both spellings of the ONE built-in are blocked, and
    // trimming + case-folding closes the trivial dodges. The id is guarded
    // separately (RESERVED_THEME_IDS) — this is the guard on what the owner SEES.
    // The fold is ASCII-ONLY on BOTH sides on purpose (T-081b review round 3,
    // SHOULD-6): Go's simple case mapping sends U+0130 (İ) to 'i' while JS's full
    // mapping sends it to "i\u0307", so "OFF\u0130CE" was rejected by the server and
    // accepted by the client. Neither side folds it now, so 「OFFİCE」 is an
    // ordinary claimable name (see the accept table below) and the two agree.
    for (const name of ["辦公室", "Office", "office", "  OFFICE  ", " 辦公室 "]) {
      expect(validateThemeBundle({ ...ok, name }), name).toMatch(
        /reserved for a built-in theme/
      );
    }
  });

  it("accepts every legitimate name shape, including the new-theme default", () => {
    // The rule must not become a general-purpose name filter: CJK, emoji,
    // spaces and punctuation are all ordinary theme names. `新主題` / `New theme`
    // live in the SAME themeIdentity subtree as the built-in's name but are the
    // default name a NEW custom theme is created with — banning them would
    // reject the app's own create-theme flow.
    for (const name of [
      "精靈村",
      "深海の夜",
      "밤하늘",
      "🌙 Midnight 🌙",
      "Mid night — v2 (beta)!",
      "新主題",
      "New theme",
      "Officescape",
      "辦公室的夜",
      "OFF\u0130CE", // LATIN CAPITAL LETTER I WITH DOT ABOVE — folded by neither side
      // Not a general-purpose filter: the categories rejected above are the
      // invisible ones only. Scripts, emoji (variation selectors included —
      // U+FE0F is Mn and stays legal on purpose), ordinary spaces and
      // punctuation all pass (T-081b review round 4, SHOULD-C).
      "Heart \u2764\uFE0F", // VARIATION SELECTOR-16 — how an emoji name is spelled
      "سمة داكنة", // Arabic, ordinary letters + ASCII space
      "ערכת נושא כהה", // Hebrew, ordinary letters + ASCII space
      "Tiếng Việt", // combining marks (Mn) in an ordinary Latin name
      // Zs is NORMALISED, not rejected (round 4 recheck, SHOULD-3): a full-width
      // space is what a Chinese IME emits for the space bar, and a NO-BREAK
      // SPACE is what a paste out of a document carries. Both are ordinary
      // names, and rejecting them told the user nothing they could act on.
      "深海\u3000之夜", // IDEOGRAPHIC SPACE inside a perfectly legitimate name
      "深\u3000海\u3000之\u3000夜", // …several of them
      "Deep\u00A0Ocean", // NO-BREAK SPACE inside an ordinary name
      "\u3000深海之夜\u3000", // padded — but not with a built-in's name
    ]) {
      expect(validateThemeBundle({ ...ok, name }), name).toBeNull();
    }
  });

  it("accepts a bundle with a legal wording overlay and rejects an illegal one", () => {
    expect(
      validateThemeBundle({ ...ok, wording: { zh: { [aKey]: "覆蓋" } } })
    ).toBeNull();
    expect(
      validateThemeBundle({ ...ok, wording: { fr: { [aKey]: "x" } } })
    ).toMatch(/language/);
  });

  it("accepts a bundle with a legal fonts overlay and rejects an illegal one", () => {
    expect(
      validateThemeBundle({ ...ok, fonts: { "--font-sans": aFontStack } })
    ).toBeNull();
    expect(
      validateThemeBundle({ ...ok, fonts: { "--font-bogus": aFontStack } })
    ).toMatch(/not a theme font token/);
    expect(
      validateThemeBundle({ ...ok, fonts: { "--font-sans": "Comic Sans, sans-serif" } })
    ).toMatch(/invalid font value/);
  });
});

describe("isValidFontValue", () => {
  it("accepts every curated safe family stack", () => {
    for (const f of SAFE_FONT_FAMILIES) {
      expect(isValidFontValue(f.stack)).toBe(true);
    }
  });

  it("rejects arbitrary strings and CSS/url/@font-face injection", () => {
    for (const v of [
      "",
      "Arial", // not on the allowlist
      "Comic Sans MS, sans-serif", // plausible but not curated
      "sans-serif", // bare generic, not a curated stack
      'url("https://evil/x.woff2")',
      "@font-face{font-family:x;src:url(y)}",
      "system-ui;}",
      "system-ui, <script>",
      "var(--x)",
      "javascript:alert(1)",
      SAFE_FONT_FAMILIES[0].stack + " ", // trailing space defeats exact match
      "f".repeat(200), // over the length cap
    ]) {
      expect(isValidFontValue(v)).toBe(false);
    }
  });
});

describe("validateFonts", () => {
  it("accepts undefined (optional) and a legal token→stack overlay", () => {
    expect(validateFonts(undefined)).toBeNull();
    expect(
      validateFonts({ "--font-sans": aFontStack, "--font-title": aFontStack })
    ).toBeNull();
  });

  it("rejects a non-object, an unknown token, and an off-allowlist value", () => {
    expect(validateFonts([])).toMatch(/must be an object/);
    expect(validateFonts({ "--color-bg": aFontStack })).toMatch(
      /not a theme font token/
    );
    expect(validateFonts({ "--font-sans": "url(https://evil)" })).toMatch(
      /invalid font value/
    );
    expect(validateFonts({ "--font-title": "Times New Roman" })).toMatch(
      /invalid font value/
    );
  });
});

// ── avatar images (T-16a1 P5) — the security boundary is the image VALUE ──
function b64(bytes: number[]): string {
  return btoa(String.fromCharCode(...bytes));
}
function avatarURI(mime: string, bytes: number[]): string {
  return `data:${mime};base64,${b64(bytes)}`;
}
const PNG_MAGIC = [0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a, 0x00, 0x01];
const JPEG_MAGIC = [0xff, 0xd8, 0xff, 0xe0, 0x00, 0x10];
const WEBP_MAGIC = [0x52, 0x49, 0x46, 0x46, 0x10, 0, 0, 0, 0x57, 0x45, 0x42, 0x50, 0];
const okPng = avatarURI("image/png", PNG_MAGIC);
const okJpeg = avatarURI("image/jpeg", JPEG_MAGIC);
const okWebp = avatarURI("image/webp", WEBP_MAGIC);

describe("isValidAvatarValue", () => {
  it("accepts a valid PNG / JPEG / WEBP base64 data URI", () => {
    expect(isValidAvatarValue(okPng)).toBe(true);
    expect(isValidAvatarValue(okJpeg)).toBe(true);
    expect(isValidAvatarValue(okWebp)).toBe(true);
  });

  it("rejects SVG, foreign schemes, bad base64, magic mismatch, oversize, non-data-URI", () => {
    const oversize = avatarURI(
      "image/png",
      PNG_MAGIC.concat(new Array(MAX_AVATAR_BYTES).fill(0))
    );
    for (const v of [
      "", // empty
      "https://evil/x.png", // not a data URI
      "javascript:alert(1)", // foreign scheme
      "data:text/html,<script>alert(1)</script>", // not base64, not image
      avatarURI("image/svg+xml", [0x3c, 0x73, 0x76, 0x67]), // SVG rejected outright
      avatarURI("text/html", [0x3c, 0x73]), // non-image mime
      avatarURI("image/gif", [0x47, 0x49, 0x46, 0x38]), // gif not whitelisted
      "data:image/png;base64,!!!!notbase64!!!!", // bad base64
      avatarURI("image/png", JPEG_MAGIC), // declares png, carries jpeg bytes
      avatarURI("image/png", [0x3c, 0x73, 0x76, 0x67, 0x20]), // png claim, svg payload → magic fail
      avatarURI("image/jpeg", PNG_MAGIC), // jpeg claim, png bytes
      "data:image/png,iVBOR", // missing ;base64
      oversize, // decoded bytes over the 64 KiB cap
    ]) {
      expect(isValidAvatarValue(v), `must reject: ${v.slice(0, 40)}`).toBe(false);
    }
  });
});

describe("validateAvatars", () => {
  it("accepts undefined (optional) and a legal member/outsource/owner/assistant overlay", () => {
    expect(validateAvatars(undefined)).toBeNull();
    expect(
      validateAvatars({ member: okPng, outsource: okWebp, owner: okJpeg, assistant: okPng })
    ).toBeNull();
  });

  it("rejects a non-object, an unknown kind, and an invalid image", () => {
    expect(validateAvatars([])).toMatch(/must be an object/);
    expect(validateAvatars({ boss: okPng })).toMatch(
      /not allowed \(only member, outsource, owner, assistant\)/
    );
    expect(
      validateAvatars({ member: avatarURI("image/svg+xml", [0x3c]) })
    ).toMatch(/not a valid image/);
  });

  it("flows through validateThemeBundle", () => {
    const good = {
      id: "midnight",
      name: "Midnight",
      colors: { [aToken]: "#111111" },
      avatars: { owner: okPng, assistant: okWebp },
    };
    expect(validateThemeBundle(good)).toBeNull();
    const bad = { ...good, avatars: { member: avatarURI("image/svg+xml", [0x3c]) } };
    expect(validateThemeBundle(bad)).toMatch(/not a valid image/);
  });
});

describe("validateLogo", () => {
  it("accepts undefined/null (optional) and a legal raster image", () => {
    expect(validateLogo(undefined)).toBeNull();
    expect(validateLogo(null)).toBeNull();
    expect(validateLogo(okPng)).toBeNull();
  });

  it("rejects an SVG and any non-image value", () => {
    expect(validateLogo(avatarURI("image/svg+xml", [0x3c]))).toMatch(
      /logo is not a valid image/
    );
    expect(validateLogo("https://evil/x.png")).toMatch(/logo is not a valid image/);
    expect(validateLogo(42)).toMatch(/logo is not a valid image/);
  });

  it("flows through validateThemeBundle", () => {
    const good = { id: "midnight", name: "Midnight", colors: { [aToken]: "#111111" }, logo: okPng };
    expect(validateThemeBundle(good)).toBeNull();
    expect(
      validateThemeBundle({ ...good, logo: avatarURI("image/svg+xml", [0x3c]) })
    ).toMatch(/logo is not a valid image/);
  });
});

describe("validateNavIcons", () => {
  it("accepts undefined (optional) and the five legal nav-tab keys", () => {
    expect(validateNavIcons(undefined)).toBeNull();
    expect(
      validateNavIcons({
        office: okPng,
        replies: okJpeg,
        tasks: okWebp,
        monitor: okPng,
        guide: okJpeg,
      })
    ).toBeNull();
  });

  it("rejects a non-object, an unknown key, and an image that fails the gate", () => {
    expect(validateNavIcons([])).toMatch(/must be an object/);
    expect(validateNavIcons({ settings: okPng })).toMatch(
      /nav icon key "settings" is not allowed \(only office, replies, tasks, monitor, guide\)/
    );
    expect(
      validateNavIcons({ office: avatarURI("image/svg+xml", [0x3c]) })
    ).toMatch(/not a valid image/);
  });

  it("flows through validateThemeBundle", () => {
    const good = {
      id: "midnight",
      name: "Midnight",
      colors: { [aToken]: "#111111" },
      navIcons: { tasks: okPng },
    };
    expect(validateThemeBundle(good)).toBeNull();
    expect(
      validateThemeBundle({ ...good, navIcons: { nope: okPng } })
    ).toMatch(/not allowed/);
  });
});

describe("validateBackgrounds", () => {
  it("accepts undefined (optional) and the canvas zone", () => {
    expect(validateBackgrounds(undefined)).toBeNull();
    expect(validateBackgrounds({ canvas: okPng })).toBeNull();
  });

  it("rejects a non-object and every zone but the outer canvas", () => {
    expect(validateBackgrounds([])).toMatch(/must be an object/);
    for (const zone of ["topbar", "nav", "main"]) {
      expect(validateBackgrounds({ [zone]: okPng })).toMatch(
        /is not allowed \(only canvas\)/
      );
    }
  });

  it("runs the same image gate as an avatar — SVG, bad magic and oversize", () => {
    const oversize = avatarURI(
      "image/png",
      PNG_MAGIC.concat(new Array(MAX_AVATAR_BYTES).fill(0))
    );
    for (const v of [
      avatarURI("image/svg+xml", [0x3c, 0x73, 0x76, 0x67]),
      avatarURI("image/png", JPEG_MAGIC),
      oversize,
    ]) {
      expect(validateBackgrounds({ canvas: v })).toMatch(/not a valid image/);
    }
  });

  it("flows through validateThemeBundle", () => {
    const good = {
      id: "midnight",
      name: "Midnight",
      colors: { [aToken]: "#111111" },
      backgrounds: { canvas: okWebp },
    };
    expect(validateThemeBundle(good)).toBeNull();
    expect(
      validateThemeBundle({ ...good, backgrounds: { topbar: okWebp } })
    ).toMatch(/only canvas/);
  });
});

describe("validateBackgroundModes", () => {
  const images = { canvas: okPng };

  it("accepts undefined (every zone tiles) and both modes on an imaged zone", () => {
    expect(validateBackgroundModes(undefined, images)).toBeNull();
    expect(validateBackgroundModes({ canvas: "tile" }, images)).toBeNull();
    expect(validateBackgroundModes({ canvas: "sides" }, images)).toBeNull();
    expect(validateBackgroundModes({ canvas: "cover" }, images)).toBeNull();
  });

  it("rejects a non-object, an unknown zone and an unknown mode", () => {
    expect(validateBackgroundModes([], images)).toMatch(/must be an object/);
    expect(validateBackgroundModes({ topbar: "tile" }, images)).toMatch(
      /is not allowed \(only canvas\)/
    );
    for (const mode of ["Tile", "contain", "", "sides "]) {
      expect(validateBackgroundModes({ canvas: mode }, images)).toMatch(
        /not a valid mode/
      );
    }
  });

  it("rejects a mode on a zone that carries no image", () => {
    expect(validateBackgroundModes({ canvas: "sides" }, undefined)).toMatch(
      /has no image in backgrounds/
    );
    expect(validateBackgroundModes({ canvas: "sides" }, { canvas: "" })).toMatch(
      /has no image in backgrounds/
    );
  });

  it("flows through validateThemeBundle", () => {
    const good = {
      id: "midnight",
      name: "Midnight",
      colors: { [aToken]: "#111111" },
      backgrounds: { canvas: okWebp },
      backgroundModes: { canvas: "sides" },
    };
    expect(validateThemeBundle(good)).toBeNull();
    expect(
      validateThemeBundle({ ...good, backgroundModes: { canvas: "contain" } })
    ).toMatch(/not a valid mode/);
    expect(
      validateThemeBundle({
        id: "midnight",
        name: "Midnight",
        colors: { [aToken]: "#111111" },
        backgroundModes: { canvas: "sides" },
      })
    ).toMatch(/has no image in backgrounds/);
  });
});

describe("validateThemeBundle backward compatibility", () => {
  it("accepts a legacy member/outsource-only bundle with no logo/navIcons/backgrounds", () => {
    expect(
      validateThemeBundle({
        id: "legacy",
        name: "Legacy",
        colors: { [aToken]: "#101018" },
        avatars: { member: okPng, outsource: okWebp },
      })
    ).toBeNull();
  });
});

describe("validateWording", () => {
  it("accepts undefined (optional) and a legal zh/en overlay", () => {
    expect(validateWording(undefined)).toBeNull();
    expect(validateWording({ zh: { [aKey]: "文字" }, en: { [aKey]: "text" } })).toBeNull();
  });

  it("drops an unknown message code and keeps the known ones", () => {
    // T-081b removed the theme-identity keys from the whitelist, so shipped
    // packs carry "profile.themeOffice" — such a pack must stay importable.
    const wording = {
      zh: { [aKey]: "文字", "profile.themeOffice": "精靈村", "typo.not.a.key": "x" },
    };
    const skipped: string[] = [];
    expect(validateWording(wording, "theme", skipped)).toBeNull();
    expect(wording.zh["profile.themeOffice"]).toBeUndefined();
    expect(wording.zh["typo.not.a.key"]).toBeUndefined();
    expect(wording.zh[aKey]).toBe("文字");
    // The drop is reported, not silent — that channel is what the import UI warns from.
    expect(skipped.sort()).toEqual(["profile.themeOffice", "typo.not.a.key"]);
  });

  it("drops an override of the theme structural markers", () => {
    // SHOULD-5: while `settings.themeCopyTag` was overridable, a pack could put
    // bidi or 200 characters into it and the built-in theme's DOWNLOAD button
    // composed a file name the product's own importer then refused. The tag moved
    // into the non-overridable `themeMarkers` subtree, so an overlay aiming at
    // either the old or the new path is dropped like any unknown code.
    const wording = {
      zh: {
        [aKey]: "文字",
        "settings.themeCopyTag": "副本\u202E",
        "themeMarkers.copyTag": "x".repeat(200),
        "themeMarkers.builtinGroup": "自訂",
        "themeMarkers.customGroup": "內建",
      },
    };
    const skipped: string[] = [];
    expect(validateWording(wording, "theme", skipped)).toBeNull();
    expect(skipped.sort()).toEqual([
      "settings.themeCopyTag",
      "themeMarkers.builtinGroup",
      "themeMarkers.copyTag",
      "themeMarkers.customGroup",
    ]);
    expect(wording.zh[aKey]).toBe("文字");
  });

  it("names a code dropped from several languages only once", () => {
    const skipped: string[] = [];
    expect(
      validateWording(
        { zh: { "profile.themeOffice": "精靈村" }, en: { "profile.themeOffice": "Elf" } },
        "theme",
        skipped
      )
    ).toBeNull();
    expect(skipped).toEqual(["profile.themeOffice"]);
  });

  it("rejects a bad language, an over-cap entry count, and illegal values", () => {
    expect(validateWording({ xian: { [aKey]: "仙" } })).toMatch(/language/);
    expect(validateWording({ zh: { [aKey]: "a\nb" } })).toMatch(/control/);
    expect(validateWording({ zh: { [aKey]: "   " } })).toMatch(/1\.\.200/);
    expect(validateWording({ zh: { [aKey]: "字".repeat(201) } })).toMatch(/1\.\.200/);
    // The cap counts the RAW submitted entries, so unknown keys cannot be
    // smuggled past it in bulk behind the new leniency.
    const over: Record<string, string> = {};
    for (let i = 0; i <= MAX_WORDING_ENTRIES_PER_LANG; i++) over[`junk.key.${i}`] = "x";
    expect(validateWording({ zh: over })).toMatch(/more than 1000 entries/);
  });
});

describe("validateThemeBundles", () => {
  it("rejects a non-array and duplicate ids", () => {
    expect(validateThemeBundles({})).toMatch(/must be an array/);
    const b = { id: "dup", name: "D", colors: { [aToken]: "#111111" } };
    expect(validateThemeBundles([b, b])).toMatch(/duplicate id/);
  });

  it("accepts an empty array and a unique set", () => {
    expect(validateThemeBundles([])).toBeNull();
    expect(
      validateThemeBundles([
        { id: "aa", name: "A", colors: { [aToken]: "#111111" } },
        { id: "bb", name: "B", colors: { [aToken]: "#222222" } },
      ])
    ).toBeNull();
  });
});

describe("normalizeThemeName", () => {
  it("trims ASCII whitespace only and folds A–Z only", () => {
    // The twin table lives in server/ocserverd/theme_bundle_test.go
    // (TestNormalizeThemeName). The two validators disagreed on 「\uFEFF辦公室」 and on
    // 「OFF\u0130CE」 because each called its own language's trim + lowercase; the
    // fix is a normaliser identical BY CONSTRUCTION, so it is pinned character by
    // character rather than through a validator verdict.
    for (const [input, want] of [
      ["Office", "office"],
      ["  OFFICE  ", "office"],
      ["\tOFFICE\r\n", "office"],
      ["辦公室", "辦公室"],
      // toLowerCase() would fold these; an ASCII fold must not.
      ["OFF\u0130CE", "off\u0130ce"],
      ["\uFF2F\uFF26\uFF26\uFF29\uFF23\uFF25", "\uFF2F\uFF26\uFF26\uFF29\uFF23\uFF25"],
      ["\u212ANIGHT", "\u212Anight"],
      // Every Zs is folded onto U+0020 BEFORE the ASCII trim, so a full-width
      // padded name normalises exactly like an ASCII-padded one (round 4
      // recheck, SHOULD-3) — which is what makes 「　辦公室　」 collide with the
      // built-in and be told so.
      ["\u3000辦公室", "辦公室"],
      ["辦公室\u3000", "辦公室"],
      ["\u00A0Office", "office"],
      ["深海\u3000之夜", "深海 之夜"],
      ["\u1680Deep\u2000Ocean\u3000", "deep ocean"],
    ] as const) {
      expect(normalizeThemeName(input), JSON.stringify(input)).toBe(want);
    }
  });
});

describe("isBuiltinThemeName", () => {
  it("derives the banned set from the locales, not from two literals", () => {
    // The twin of TestIsBuiltinThemeName in server/ocserverd/theme_bundle_test.go
    // (NIT-8). The banned set is THEME_IDENTITY_NAMES intersected with
    // RESERVED_THEME_IDS, and `THEME_IDENTITY_NAMES[id] ?? []` is a SILENT
    // failure shape: give a future built-in a kebab-case theme id while its i18n
    // key stays camelCase and the intersection is empty, so the client guard
    // stops guarding while nothing else changes.
    for (const id of RESERVED_THEME_IDS) {
      const names = THEME_IDENTITY_NAMES[id] ?? [];
      expect(
        names.length,
        `built-in theme "${id}" has no display name in THEME_IDENTITY_NAMES — ` +
          `the name guard would silently pass for it`
      ).toBeGreaterThan(0);
      for (const name of names) {
        expect(isBuiltinThemeName(name), name).toBe(true);
      }
    }
    // …and an id OUTSIDE the reserved set stays claimable: themeIdentity.newTheme
    // is the default name a NEW custom theme is created with.
    for (const [id, names] of Object.entries(THEME_IDENTITY_NAMES)) {
      if ((RESERVED_THEME_IDS as readonly string[]).includes(id)) continue;
      for (const name of names) {
        expect(isBuiltinThemeName(name), `themeIdentity.${id} = ${name}`).toBe(false);
      }
    }
  });
});

describe("isValidDisplayTheme", () => {
  it("admits \"\", the office built-in, and an existing custom id only", () => {
    const ids = new Set(["midnight"]);
    expect(isValidDisplayTheme("", ids)).toBe(true);
    expect(isValidDisplayTheme("office", ids)).toBe(true);
    expect(isValidDisplayTheme("midnight", ids)).toBe(true);
    // "xian" is no longer a built-in — it is only admissible as a custom id.
    expect(isValidDisplayTheme("xian", ids)).toBe(false);
    expect(isValidDisplayTheme("xian", new Set(["xian"]))).toBe(true);
    expect(isValidDisplayTheme("ghost", ids)).toBe(false);
  });
});
