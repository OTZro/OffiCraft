import { describe, it, expect } from "vitest";
import { GROUP_ORDER, tokenMeta } from "./themeTokenMeta";
import { THEME_COLOR_TOKENS } from "../styles/themeTokens.generated";

describe("tokenMeta", () => {
  it("gives every theme.css token a friendly label in a real purpose group", () => {
    // The file's own stated requirement: the theme editor never shows a raw
    // --color-* name. tokenMeta degrades unmapped tokens to {group:"other",
    // label:token} so the editor never HIDES a colour — but that fallback is a
    // safety net, not a shipping state, and it is invisible without this check
    // (T-081b added 10 tokens and all 10 sat in "other" showing raw CSS names).
    const raw: string[] = [];
    for (const tok of THEME_COLOR_TOKENS) {
      for (const lang of ["zh", "en"] as const) {
        const meta = tokenMeta(tok, lang);
        if (meta.group === "other" || meta.label === tok) raw.push(`${tok}/${lang}`);
      }
    }
    expect(raw).toEqual([]);
  });

  it("puts every token in a group the editor actually renders", () => {
    const order = new Set(GROUP_ORDER);
    for (const tok of THEME_COLOR_TOKENS) {
      expect(order.has(tokenMeta(tok, "zh").group)).toBe(true);
    }
  });

  it("labels the two languages differently enough to be real translations", () => {
    // A copy-paste of the zh label into en (or vice versa) would satisfy the
    // coverage check above while shipping Chinese to an English cockpit.
    for (const tok of THEME_COLOR_TOKENS) {
      const zh = tokenMeta(tok, "zh").label;
      const en = tokenMeta(tok, "en").label;
      expect(zh).not.toBe("");
      expect(en).not.toBe("");
      expect(/^[\x20-\x7e]+$/.test(en)).toBe(true);
    }
  });
});
