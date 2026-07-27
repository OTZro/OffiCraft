// gen-theme-tokens.test.ts — the generator's refusal to drop a token SILENTLY
// (T-081b review round 4 recheck, NIT-2).
//
// The marker slots are kept out of the pack-settable whitelist on purpose. When
// that exclusion was a bare `--color-marker-` prefix filter it was silent in the
// direction nobody watches: a future --color-marker-* token meant as an ordinary
// theme colour would disappear from the whitelist with no error and no warning,
// and the author would only find the result odd. The exclusion is an explicit
// list now, and the prefix is the tripwire around it — so both halves of the
// disagreement have to be watched failing.

import { describe, it, expect } from "vitest";
import { execFileSync } from "node:child_process";
import { mkdtempSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

const HERE = dirname(fileURLToPath(import.meta.url));
const SCRIPT = join(HERE, "gen-theme-tokens.mjs");
const REAL_THEME_CSS = join(HERE, "..", "src", "styles", "theme.css");

/** Run the generator over a copy of the real theme.css, after `sabotage` has had
 *  its way with it, writing its outputs into a temp dir. */
function run(sabotage?: (css: string) => string) {
  const dir = mkdtempSync(join(tmpdir(), "gen-theme-tokens-"));
  const src = join(dir, "theme.css");
  const css = readFileSync(REAL_THEME_CSS, "utf8");
  writeFileSync(src, sabotage ? sabotage(css) : css);
  try {
    const stdout = execFileSync("node", [SCRIPT], {
      encoding: "utf8",
      env: { ...process.env, GEN_THEME_TOKENS_SRC: src, GEN_THEME_TOKENS_OUT_DIR: dir },
      stdio: ["ignore", "pipe", "pipe"],
    });
    return { code: 0, out: stdout, dir };
  } catch (e) {
    const err = e as { status: number; stdout: string; stderr: string };
    return { code: err.status, out: `${err.stdout}${err.stderr}`, dir };
  } finally {
    rmSync(dir, { recursive: true, force: true });
  }
}

describe("gen-theme-tokens", () => {
  it("names the excluded marker slots in its output instead of dropping them quietly", () => {
    const { code, out } = run();
    expect(code).toBe(0);
    expect(out, out).toContain("excluded as non-overridable marker slots");
    for (const token of [
      "--color-marker-builtin",
      "--color-marker-custom",
      "--color-marker-surface",
      "--color-marker-fg",
    ]) {
      expect(out, out).toContain(token);
    }
  });

  it("fails when theme.css adds a --color-marker-* token the exclusion list does not name", () => {
    // The silent case: prefix-only exclusion dropped this from the whitelist
    // with no error at all. It must be a decision someone makes, not a side
    // effect of the name.
    const { code, out } = run((css) =>
      css.replace("--color-marker-fg: #e7e8ee;", "--color-marker-fg: #e7e8ee;\n  --color-marker-halo: #223;")
    );
    expect(out, out).toMatch(/--color-marker-halo/);
    expect(out, out).toMatch(/NON_OVERRIDABLE_TOKENS/);
    expect(code).toBe(1);
  });

  it("fails when a listed marker slot disappears from theme.css", () => {
    const { code, out } = run((css) => css.replace("--color-marker-custom: #8b7ae8;", ""));
    expect(out, out).toMatch(/--color-marker-custom/);
    expect(out, out).toMatch(/no longer defined in theme\.css/);
    expect(code).toBe(1);
  });
});
