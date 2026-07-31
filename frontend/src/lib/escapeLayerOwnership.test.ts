// The Esc mechanism's ONLY cheap guard. Everything else about it is invisible
// to jsdom: with the popover reverted to its old per-surface listener the whole
// 1490-case suite stayed green, and the one guard that did catch it
// (visual-guards/artifacts-badge.ct.spec.tsx) only reddens on about a third of
// runs because the bug is a race. A single CI pass therefore detected the
// regression roughly one time in three. This turns that into every time, by
// checking the STRUCTURE the fix rests on rather than racing it:
//
//   there is exactly one window keydown listener in the app, and it is the
//   dispatcher.
//
// A second one is the bug — it is how a surface starts deciding for itself
// whether to close, which is what nobody can do correctly.

import { describe, it, expect } from "vitest";
import { readFileSync, readdirSync } from "node:fs";
import { join } from "node:path";

const SRC = join(__dirname, "..");
const OWNER = "lib/escapeLayers.ts";

function productionSources(dir: string, out: string[] = []): string[] {
  for (const entry of readdirSync(dir, { withFileTypes: true })) {
    const path = join(dir, entry.name);
    if (entry.isDirectory()) productionSources(path, out);
    else if (/\.tsx?$/.test(entry.name) && !/\.test\.tsx?$/.test(entry.name)) out.push(path);
  }
  return out;
}

/** Comments talk ABOUT the old pattern; only real code counts as a binding. */
function stripComments(source: string): string {
  return source.replace(/\/\*[\s\S]*?\*\//g, "").replace(/^\s*\/\/.*$/gm, "");
}

describe("escapeLayers ownership", () => {
  it("leaves lib/escapeLayers.ts as the only window keydown listener in the app", () => {
    const files = productionSources(SRC);
    // A dead regex or a mis-rooted walk would pass this vacuously.
    expect(files.length).toBeGreaterThan(50);
    expect(files.map((f) => f.slice(SRC.length + 1))).toContain(OWNER);

    const binders = files
      .filter((f) => /window\.addEventListener\(\s*["'`]keydown["'`]/.test(stripComments(readFileSync(f, "utf8"))))
      .map((f) => f.slice(SRC.length + 1))
      .sort();

    expect(binders).toEqual([OWNER]);
  });

  it("matches the binding it is looking for", () => {
    // The positive control for the regex above: if it stopped matching, the
    // list-is-empty case would read as compliance.
    expect(
      /window\.addEventListener\(\s*["'`]keydown["'`]/.test(
        stripComments(readFileSync(join(SRC, OWNER), "utf8")),
      ),
    ).toBe(true);
  });
});
