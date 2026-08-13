// Every `/api/...` path this adapter spells must exist VERBATIM in the frozen
// spec (T-791e).
//
// 🔴 WHY THIS EXISTS. The boot-context blocks shipped with three hand-typed
// path strings on a bare fetch — `/api/system-interaction/${key}` — against a
// backend that serves `/api/system-interaction` with no key segment. Nothing
// went red: tsc cannot see inside a template string, and every test in the
// suite runs on a mock adapter that answers whatever path it is handed. The
// failure only exists against the real server, as a runtime 404, on a screen
// whose whole job is editing the documents that decide whether agents boot.
//
// The typed client removes that class for the calls that ride it, but it
// removes it only for as long as they ride it: the next route that lands
// before the spec catches up gets the same bare-fetch escape hatch and the
// same silence. This assertion is what stays red in that window — it reads the
// path strings out of http.ts itself and requires each one to be a path the
// spec actually declares.
//
// It is deliberately a source scan, not a call-site scan: a path that is
// written down but not yet called is exactly the shape that ships broken.

import { describe, it, expect } from "vitest";
import { readFileSync } from "node:fs";
import { resolve } from "node:path";

const FE_ROOT = resolve(__dirname, "../..");

const httpSource = readFileSync(resolve(FE_ROOT, "src/api/http.ts"), "utf8");

const spec = JSON.parse(
  readFileSync(resolve(FE_ROOT, "../spec/openapi.json"), "utf8")
) as { paths: Record<string, unknown> };

/** Paths this adapter spells that the OpenAPI contract does NOT declare, each
 * with the reason it is outside the contract. A WRITTEN-DOWN ROLL CALL, not a
 * pattern: the only way past this assertion is to edit this list, and that
 * edit is the review it is asking for. Adding a route here means claiming it
 * can never be typed — say why. */
const OUTSIDE_THE_CONTRACT = new Map<string, string>([
  // Empty, and that is the current fact rather than an oversight: every
  // `/api/...` string this file spells today is a declared path. The two
  // permanently hand-written seams do not put a path literal here — the SSE
  // downlink and the attachment rewrite compose their URLs from values, so
  // they never reach this scan.
]);

function pathLiterals(source: string): string[] {
  return [...new Set(source.match(/"\/api\/[^"]*"/g) ?? [])].map((s) =>
    s.slice(1, -1)
  );
}

describe("httpApi path strings", () => {
  it("names only paths the frozen spec declares", () => {
    const used = pathLiterals(httpSource);
    // Anti-vacuity: a regex that stopped matching would leave an empty set and
    // an empty set satisfies every assertion below. The adapter covers most of
    // the surface, so the real number is far above this floor.
    expect(used.length).toBeGreaterThan(50);
    expect(Object.keys(spec.paths).length).toBeGreaterThan(50);

    const declared = new Set(Object.keys(spec.paths));
    const unknown = used.filter(
      (p) => !declared.has(p) && !OUTSIDE_THE_CONTRACT.has(p)
    );
    expect(unknown).toEqual([]);

    // An exemption that outlived its call site is how a roll call rots into a
    // list nobody reads: it keeps granting permission for a path that is gone.
    const stale = [...OUTSIDE_THE_CONTRACT.keys()].filter(
      (p) => !used.includes(p)
    );
    expect(stale).toEqual([]);
  });

  it("spells each boot-context route the way the backend serves it", () => {
    // The six routes this ticket added, asserted one by one rather than folded
    // into the sweep above: the sweep proves each string is SOME declared
    // path, and `/api/system-interaction/global` failing that is the whole
    // point — but only naming them individually says which six must be there,
    // so deleting a call site cannot quietly shrink the coverage.
    const used = new Set(pathLiterals(httpSource));
    for (const route of [
      "/api/system-interaction",
      "/api/system-interaction/reset",
      "/api/boot-sequence/{runtime_key}",
      "/api/boot-sequence/{runtime_key}/reset",
      "/api/document-history/{kind}/{key}",
      "/api/document-history/{kind}/{key}/{id}/restore",
    ]) {
      expect(used, `http.ts no longer spells ${route}`).toContain(route);
      expect(spec.paths, `spec does not declare ${route}`).toHaveProperty([
        route,
      ]);
    }
  });
});
