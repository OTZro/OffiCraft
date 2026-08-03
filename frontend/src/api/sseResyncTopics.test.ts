// The frontend's closed SSE topic set, bound to the contract that declares it.
//
// WHY this exists (T-05db node 4): `SSE_RESYNC_TOPICS` in api/http.ts is the
// list http.ts replays after a reconnect — one synthetic delta per topic, which
// is what makes every hook refetch the snapshot it may have missed while the
// stream was down (spec/sse.md §2.1: there is NO replay). A topic MISSING from
// that array fails in the worst possible shape: after a reconnect that topic
// silently never refetches, so the data is right, the screen is stale, and
// there is no error, no console warning and no red test. The user sees it; the
// suite does not. Until this file existed nothing in the frontend checked the
// array against anything — it was a hand transcription of a table in a
// markdown file, and there were in fact TWO such transcriptions (the second
// lived in hooks/sseFanout.test.tsx and, measured, had zero discriminating
// power: deleting a topic from it left the whole suite green).
//
// So: ONE copy (http.ts's, now exported and replayed by sseFanout.test.tsx too)
// and this guard pins it to spec/sse.md §3.1 — the same table the two Go/Python
// guards on the backend already bind (server/ocserverd/sse_topics_spec_test.go
// binds spec↔hub.go, conformance/test_sse.py binds spec↔conformance).
//
// The table is parsed HERE AT RUN TIME, from the repo checkout. Reading a
// repo file from vitest is established practice in this tree (lib/themePaint,
// lib/paintArtifact, components/styleOwnership all readFileSync the sources
// they guard) — vitest runs in Node, `node:fs` is real.
//
// EQUALITY, not subset, and every offender is NAMED: a topic in the spec that
// the frontend never resyncs is the silent-staleness bug above; a topic in the
// frontend that the spec does not declare is a phantom the server can never
// send (hub.go drops it at the publish seam), i.e. a resync fan-out that costs
// every hook a refetch for nothing.
//
// 🔴 Do NOT "fix" a failure here by transcribing the spec table into this file.
// The whole point is that this file contains no topic names at all.

import { describe, it, expect } from "vitest";
import { readFileSync } from "node:fs";
import { resolve } from "node:path";

// __dirname (= frontend/src/api) rather than import.meta.url: under the jsdom
// environment import.meta.url is not a file: URL, so fileURLToPath throws.
// Same resolution style as lib/themePaint.test.ts, which reads a server asset.
const SPEC_PATH = resolve(__dirname, "../../..", "spec/sse.md");

/** One row of the §3.1 table: `| \`<topic>\` | <trigger> | <op> |`. The header
 * row (`| topic | trigger | op |`) carries no backticks, so it is not a row. */
const TOPIC_ROW = /^\|\s*`([a-z_]+)`\s*\|/gm;

/** Parse the closed topic set out of spec/sse.md's text.
 *
 * FAIL-CLOSED by construction: every way this can stop finding topics THROWS
 * rather than returning a short or empty list. A parser that silently returns
 * [] would turn this guard green forever the day someone renames the heading —
 * the exact failure mode the guard is supposed to make impossible. The three
 * throws below are each exercised by a test at the bottom of this file. */
export function parseSpecTopics(raw: string): string[] {
  const startIdx = raw.indexOf("### 3.1");
  if (startIdx < 0) {
    throw new Error(
      "spec/sse.md: heading '### 3.1' not found — the closed-topic table was " +
        "renamed or moved. This guard finds the table by heading; refusing to " +
        "report an empty topic set as agreement.",
    );
  }
  const afterStart = raw.slice(startIdx);
  const endIdx = afterStart.indexOf("### 3.2");
  if (endIdx < 0) {
    throw new Error(
      "spec/sse.md: heading '### 3.2' not found after §3.1 — cannot bound the " +
        "§3.1 topic table, and an unbounded slice would swallow the §4.1 " +
        "audience table too. Refusing to guess.",
    );
  }
  const section = afterStart.slice(0, endIdx);
  const topics = [...section.matchAll(TOPIC_ROW)].map((m) => m[1]);
  if (topics.length === 0) {
    throw new Error(
      "spec/sse.md §3.1: parsed ZERO topics — the table's shape changed. Fix " +
        "the parser; an empty set would make this guard assert nothing.",
    );
  }
  return topics;
}

/** Read + parse the real spec. Fails loudly (never empty) if the file is gone
 * or unreadable — a missing spec must not read as "no topics, all agreed". */
export function readSpecTopics(path: string = SPEC_PATH): string[] {
  let raw: string;
  try {
    raw = readFileSync(path, "utf8");
  } catch (e) {
    throw new Error(
      `spec/sse.md unreadable at ${path}: ${String(e)} — this guard reads the ` +
        "wire contract at run time and cannot pass without it.",
    );
  }
  return parseSpecTopics(raw);
}

describe("SSE_RESYNC_TOPICS vs spec/sse.md §3.1", () => {
  it("equals the closed topic set the wire contract declares", async () => {
    const spec = new Set(readSpecTopics());
    // Imported lazily so a parse/read failure above reports as ITSELF rather
    // than as a confusing module-load error from the adapter.
    const { SSE_RESYNC_TOPICS } = await import("./http");
    const code = new Set<string>(SSE_RESYNC_TOPICS);

    const missing = [...spec].filter((t) => !code.has(t)).sort();
    const extra = [...code].filter((t) => !spec.has(t)).sort();

    expect(
      { missing, extra },
      "api/http.ts SSE_RESYNC_TOPICS MUST equal the closed topic set in " +
        "spec/sse.md §3.1.\n" +
        "  `missing` = declared by the spec but NEVER resynced by the client: " +
        "after a reconnect that topic silently stops refetching — right data, " +
        "stale screen, zero errors. Add it to SSE_RESYNC_TOPICS.\n" +
        "  `extra` = resynced by the client but NOT in the contract: a phantom " +
        "topic the server can never emit (hub.go drops it at the publish " +
        "seam). Add it to spec §3.1 first — spec-first — or drop it here.\n" +
        "  🔴 Do NOT silence this by copying the table into the test.",
    ).toEqual({ missing: [], extra: [] });
  });

  // The array's own SHAPE — the one thing no comparison against the spec can
  // check. The confrontation above compares SETS, and a Set de-duplicates by
  // definition; the two deep-equality guards (api/http.sse-pool.test.ts,
  // hooks/sseFanout.test.tsx) now build their expectation FROM this array, so a
  // duplicate lands on both sides of the equality and cancels. Measured by
  // review round 2: duplicating "chat" in SSE_RESYNC_TOPICS left all 218 files
  // / 1820 tests green, while the hand-copy those guards replaced had caught it
  // (5 failed). That is the one axis the fold lost, and this is where it comes
  // back — a duplicate costs every subscriber of that topic an extra refetch on
  // every reconnect, silently and with no red anywhere.
  it("declares each topic exactly once (a duplicate is invisible to set/self comparisons)", async () => {
    const { SSE_RESYNC_TOPICS } = await import("./http");
    const seen = new Map<string, number>();
    for (const t of SSE_RESYNC_TOPICS) seen.set(t, (seen.get(t) ?? 0) + 1);
    const duplicated = [...seen.entries()].filter(([, n]) => n > 1).map(([t, n]) => `${t}×${n}`);
    expect(
      duplicated,
      "SSE_RESYNC_TOPICS must list each topic exactly once: resyncAll fans one " +
        "synthetic delta per ELEMENT, so a repeated element makes every hook " +
        "subscribed to it refetch again on every reconnect. Nothing else in the " +
        "suite can see this — the spec comparison above is set-based, and the " +
        "fan-out guards compare the fan against this same array.",
    ).toEqual([]);
    expect(new Set(SSE_RESYNC_TOPICS).size).toBe(SSE_RESYNC_TOPICS.length);
  });

  // ——— fail-closed: every way the parser can stop seeing the table must be
  // LOUD. Constructed inputs, so these hold regardless of what the real
  // spec file currently looks like.
  // NOTE the renumbering (3.x → 9.x) rather than a suffix: "### 3.1bis" still
  // CONTAINS "### 3.1", so a suffix does not actually remove the heading and
  // these two tests passed vacuously when first written (measured).
  //
  // ⚠️ And the note alone is not enough — it was already written here, and the
  // self-check was STILL missing (review round 2, MAJOR-3): the Python edge of
  // this same round asserts `<delimiter> not in <mutated>` and this edge did
  // not. A comment cannot fail; the assertion below can. Note also that JS
  // `String.replace(string, …)` replaces only the FIRST occurrence (Python's
  // str.replace replaces all), so replace-ALL semantics are used deliberately:
  // with plain `replace`, a second `### 3.1` anywhere in the spec would leave
  // the heading present and turn these into FALSE REDS (the parser would not
  // throw). ⚠️ Spelled `split(…).join(…)` and NOT `String.replaceAll`: this
  // project's tsconfig targets `lib: ES2020`, where `replaceAll` does not exist
  // on `string` — vitest passes (Node has it at run time) but `npm run
  // typecheck` fails with TS2550, which is how it reached CI. Do not "tidy"
  // this back to replaceAll, and do not bump the lib target for one test. A
  // global RegExp would work too but needs the delimiter escaped (`.` is a
  // metacharacter) — split/join needs no escaping at all.
  it("throws when the §3.1 heading is gone (not: passes with zero topics)", () => {
    const moved = readFileSync(SPEC_PATH, "utf8").split("### 3.1").join("### 9.1");
    expect(moved, "the mutation must really remove the delimiter, else this test is vacuous").not.toContain(
      "### 3.1",
    );
    expect(() => parseSpecTopics(moved)).toThrow(/'### 3\.1' not found/);
  });

  it("throws when the section's end boundary is gone", () => {
    const unbounded = readFileSync(SPEC_PATH, "utf8").split("### 3.2").join("### 9.2");
    expect(unbounded, "the mutation must really remove the delimiter, else this test is vacuous").not.toContain(
      "### 3.2",
    );
    expect(() => parseSpecTopics(unbounded)).toThrow(/'### 3\.2' not found/);
  });

  it("throws when §3.1 exists but yields zero topic rows", () => {
    expect(() => parseSpecTopics("### 3.1\n\n| topic | trigger | op |\n|---|---|---|\n\n### 3.2\n")).toThrow(
      /parsed ZERO topics/,
    );
  });

  it("throws when the spec file cannot be read", () => {
    expect(() => readSpecTopics(`${SPEC_PATH}.does-not-exist`)).toThrow(/unreadable/);
  });
});
