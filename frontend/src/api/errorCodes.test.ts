// The frontend edge of the ONE status→code table (spec/error-codes.json).
//
// Two guards, and neither is about a single wrong string:
//
//   1. `codeForStatus` answers what the spec table says, cell by cell, in both
//      fallback buckets — the same rows server/ocserverd's
//      TestErrorCodeForStatusMatchesSpec pins `errorCodeForStatus` against and
//      conformance/test_error_envelope.py pins CODE_BY_STATUS against. Edit the
//      JSON on its own and the Go side reddens; edit the Go map on its own and
//      it reddens too. Nobody can move one side quietly.
//
//   2. No source file under src/ writes an error code the server cannot emit.
//      This is the guard that would have caught the original defect: seven mock
//      refusals and five hand-built fake responses all said `bad_request`, a
//      code `errorCodeForStatus` has never produced, so any component test
//      branching on the CODE agreed with a server that does not exist.
//      Structurally the mock can no longer write one at all (it imports
//      `mockApiError`, not `ApiError`) — this catches the hand-built fakes in
//      test files, which the structure cannot reach.

import { describe, it, expect } from "vitest";
import { readFileSync, readdirSync } from "node:fs";
import { join } from "node:path";
import { codeForStatus, ERROR_CODE_VOCABULARY } from "./errorCodes";
import spec from "../../../spec/error-codes.json";

const SRC = join(__dirname, "..");

function sources(dir: string, out: string[] = []): string[] {
  for (const entry of readdirSync(dir, { withFileTypes: true })) {
    const path = join(dir, entry.name);
    if (entry.isDirectory()) sources(path, out);
    else if (/\.tsx?$/.test(entry.name)) out.push(path);
  }
  return out;
}

function stripComments(source: string): string {
  return source.replace(/\/\*[\s\S]*?\*\//g, "").replace(/^\s*\/\/.*$/gm, "");
}

/** Every place a source file TYPES an error code: the `code` key of a
 * hand-built `{error: {…}}` envelope, a `.code` assertion, and the third
 * `new ApiError(...)` argument. The narrow envelope shape matters — a bare
 * `code:` key also names unrelated vocabularies (backup health, doc-cap save
 * reasons) that this table has no say over. */
function codeLiterals(source: string): Array<{ code: string; status: number | null }> {
  const found: Array<{ code: string; status: number | null }> = [];
  for (const m of source.matchAll(/\berror:\s*\{\s*code:\s*"([a-z_]+)"/g)) {
    found.push({ code: m[1], status: null });
  }
  for (const m of source.matchAll(/\.code\)?\s*\)?\.toBe\("([a-z_]+)"\)/g)) {
    found.push({ code: m[1], status: null });
  }
  for (const m of source.matchAll(/\bnew ApiError\(/g)) {
    const tail = source.slice(m.index! + m[0].length, m.index! + m[0].length + 400);
    const args = tail.match(/,\s*(\d{3}),\s*"([a-z_]+)"/);
    if (args) found.push({ code: args[2], status: Number(args[1]) });
  }
  return found;
}

describe("codeForStatus", () => {
  it("answers the spec table for every mapped status", () => {
    const rows = Object.entries(spec.by_status);
    expect(rows.length).toBeGreaterThan(5);
    for (const [status, code] of rows) {
      expect(codeForStatus(Number(status))).toBe(code);
    }
  });

  it("falls into the spec's two honest buckets for unmapped statuses", () => {
    const mapped = new Set(Object.keys(spec.by_status).map(Number));
    let unmapped5xx = 0;
    let unmapped4xx = 0;
    for (let status = 100; status < 600; status++) {
      if (mapped.has(status)) continue;
      if (status >= 500) {
        expect(codeForStatus(status)).toBe(spec.fallback_5xx);
        unmapped5xx++;
      } else {
        expect(codeForStatus(status)).toBe(spec.fallback_other);
        unmapped4xx++;
      }
    }
    // A mis-rooted loop would pass this vacuously.
    expect(unmapped5xx).toBeGreaterThan(50);
    expect(unmapped4xx).toBeGreaterThan(50);
  });
});

describe("error codes typed in the frontend", () => {
  it("only ever names codes the server can actually emit", () => {
    const files = sources(SRC);
    expect(files.length).toBeGreaterThan(50);
    const offenders: string[] = [];
    let seen = 0;
    for (const file of files) {
      if (file === __filename) continue;
      for (const hit of codeLiterals(stripComments(readFileSync(file, "utf8")))) {
        seen++;
        if (!ERROR_CODE_VOCABULARY.has(hit.code)) {
          offenders.push(`${file}: "${hit.code}" is not a code the server emits`);
        } else if (hit.status !== null && codeForStatus(hit.status) !== hit.code) {
          offenders.push(
            `${file}: http ${hit.status} carries "${codeForStatus(hit.status)}", not "${hit.code}"`
          );
        }
      }
    }
    // A dead regex would pass this vacuously.
    expect(seen).toBeGreaterThan(0);
    expect(offenders).toEqual([]);
  });

  it("leaves the mock adapter unable to type a code at all", () => {
    const mock = readFileSync(join(SRC, "api/mock.ts"), "utf8");
    expect(mock).toContain('import { mockApiError } from "./errorCodes"');
    expect(stripComments(mock)).not.toContain("new ApiError(");
  });
});
