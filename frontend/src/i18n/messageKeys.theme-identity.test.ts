// messageKeys.theme-identity.test.ts — T-081b §6 regression guard.
//
// A theme bundle's `wording` overlay may re-word the product; it may NOT rename
// another THEME. `profile.themeOffice` used to be overridable, and it is the
// built-in theme's identity: the row in the theme picker, the theme-settings
// heading, and the `name` written into the file when the built-in theme is
// exported. Importing a 「精靈村」 pack therefore renamed the built-in theme to
// 「精靈村」 too — two identically named rows in the picker and no way back to
// the shipped one (owner report 2026-07-27).
//
// The fix is a rule in the whitelist generator (scripts/gen-message-keys.mjs):
// the `themeIdentity` subtree is skipped wholesale. This test fails the moment
// any theme name reappears in MESSAGE_KEYS — via a reverted generator rule, a
// theme name moved back out of the subtree, or a new built-in theme whose name
// was put somewhere overridable.

import { describe, it, expect } from "vitest";
import { MESSAGE_KEYS } from "./messageKeys.generated";
import { zh } from "./locales/zh";
import { en } from "./locales/en";

const keys = new Set(MESSAGE_KEYS);

describe("MESSAGE_KEYS", () => {
  it("does not let a theme bundle rename a theme", () => {
    for (const name of Object.keys(zh.themeIdentity)) {
      expect(
        keys.has(`themeIdentity.${name}`),
        `themeIdentity.${name} is a THEME'S OWN NAME — a theme pack that can ` +
          `re-word it renames the built-in theme and the owner loses the way back`
      ).toBe(false);
    }
    // The pre-fix location, named explicitly: a revert that moves the built-in
    // theme's name back under profile.* must fail here too, not pass silently.
    expect(keys.has("profile.themeOffice")).toBe(false);
    expect(keys.has("profile.themeNewName")).toBe(false);
  });

  it("does not let a theme bundle forge the markers that tell themes apart", () => {
    // T-081b review round 3, SHOULD-5. `settings.themeCopyTag` WAS overridable,
    // and it is what the built-in theme's download button names the file with —
    // a pack could set it to a bidi-bearing or 200-character value and the
    // product then emitted a bundle its own importer refuses. The picker's
    // built-in / custom group headings are the same class of string one door
    // along: overridable headings can be swapped so the grouping lies.
    for (const name of Object.keys(zh.themeMarkers)) {
      expect(
        keys.has(`themeMarkers.${name}`),
        `themeMarkers.${name} is a STRUCTURAL marker — a theme pack that can ` +
          `re-word it forges the very distinction the marker exists to draw`
      ).toBe(false);
    }
    // The pre-fix location, named explicitly, so moving the tag back under
    // settings.* fails here rather than silently becoming overridable again.
    expect(keys.has("settings.themeCopyTag")).toBe(false);
  });

  it("keeps the theme markers present and non-empty in both languages", () => {
    for (const dict of [zh, en]) {
      for (const value of Object.values(dict.themeMarkers)) {
        expect(value.length).toBeGreaterThan(0);
      }
    }
  });

  it("still lets a theme bundle rename the PLACE", () => {
    // nav.office is the 辦公室 place name on the nav tab — re-wording the world
    // is the whole point of a theme pack, so this one must stay reachable.
    expect(keys.has("nav.office")).toBe(true);
  });

  it("keeps the theme-identity names present and non-empty in both languages", () => {
    // Excluding them from the overlay must not have made them disappear: the
    // picker still has to render a name for the built-in theme.
    for (const dict of [zh, en]) {
      for (const value of Object.values(dict.themeIdentity)) {
        expect(value.length).toBeGreaterThan(0);
      }
    }
  });
});
