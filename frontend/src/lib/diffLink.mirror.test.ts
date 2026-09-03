// The cockpit's half of the comparison-address mirror confrontation (T-59).
//
// `server/ocserverd/diffaddr.go` is THE AUTHORITY on how one side of a
// comparison is spelled. `cli/ocagent/diff.go` carries a pre-flight copy, and
// `lib/diffLink.ts` carries a reader's copy — three transcriptions of one rule,
// in three languages, with no import path between any two of them.
//
// The two Go copies are already driven against
// bin/tests/fixtures/diff-side-addresses.tsv rather than against each other, so
// a drift reddens the copy that drifted BY NAME. This file puts the cockpit's
// copy on the same table. Its header records the frontend as an uncovered gap;
// that note is what this test closes, and it is the note's owner who should
// strike it.
//
// 🔴 A MISSING OR UNREADABLE FIXTURE IS A FAILURE, NEVER A SKIP. A mirror guard
// that quietly passes when it cannot find its own table means nothing was
// checked, which is worse than no guard at all.

import { describe, it, expect } from "vitest";
import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { parseDiffSideAddress } from "./diffLink";

// Resolved from the vitest ROOT (frontend/), not from import.meta.url: the
// suite runs the transformed module, whose URL is not a file: one.
const FIXTURE = resolve(process.cwd(), "../bin/tests/fixtures/diff-side-addresses.tsv");

interface Row {
  line: number;
  address: string;
  ok: boolean;
  about: string;
}

function rows(): Row[] {
  const text = readFileSync(FIXTURE, "utf8");
  const out: Row[] = [];
  text.split("\n").forEach((raw, i) => {
    const line = i + 1;
    if (raw.trim() === "" || raw.startsWith("#")) return;
    const cells = raw.split("\t");
    if (cells.length !== 3) {
      throw new Error(`${FIXTURE}:${line}: expected 3 tab-separated cells, got ${cells.length}`);
    }
    const [address, verdict, about] = cells;
    if (verdict !== "ok" && verdict !== "bad") {
      throw new Error(`${FIXTURE}:${line}: verdict must be ok|bad, got ${verdict}`);
    }
    // The table spells a literal space as <space>, because a bare one between
    // two tabs is too easy for an editor to eat — and the padded rows are the
    // ones whose exactness matters most.
    out.push({ line, address: address.split("<space>").join(" "), ok: verdict === "ok", about });
  });
  return out;
}

describe("parseDiffSideAddress", () => {
  const table = rows();

  it("reads a table with rows in it", () => {
    // A fixture that parsed to nothing would make every assertion below vacuous.
    expect(table.length).toBeGreaterThan(20);
  });

  it.each(table.map((r) => [r.line, r.address, r.ok, r.about] as const))(
    "line %i: %s → %s (%s)",
    (line, address, ok) => {
      expect(parseDiffSideAddress(address) !== null, `${FIXTURE}:${line}`).toBe(ok);
    },
  );
});
