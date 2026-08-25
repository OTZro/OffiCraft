// Pins for deriveTaskNo (taskNo.ts) — the frontend mirror of `TaskNo` in
// server/ocserverd/domain.go.
//
// The first two cases are COPIED VERBATIM from the server's own pin
// (server/ocserverd/api_tasks_test.go, TestTaskNo*). That is deliberate: the two
// sides derive the same number from the same id with no shared code, so the
// only thing keeping them honest is that both are nailed to the SAME FACTS.
// If the rule ever changes on the server, the two test files disagree loudly
// instead of the two implementations drifting apart in silence. Keep them in
// sync — if you change a case here, change it there too.
//
// The remaining cases are malformed / boundary ids. Each asserts what the Go
// original does for that input, not what would be "smarter" — a mirror that
// invents its own handling for the ugly inputs is no longer a mirror.

import { describe, it, expect } from "vitest";
import { deriveTaskNo } from "./taskNo";

describe("deriveTaskNo", () => {
  // ── shared with the server pin ─────────────────────────────────────────────

  it("keeps the whole hex body after the prefix", () => {
    expect(deriveTaskNo("t-7d40aabbccdd")).toBe("T-7d40aabbccdd");
  });

  it("keeps a short id intact too — nothing is padded or cut", () => {
    expect(deriveTaskNo("t-ab")).toBe("T-ab");
  });

  it("keeps the whole id (owner ruling 2026-08-25)", () => {
    // The number is the id, re-cased: read it off the UI, paste it back, and
    // it names exactly one task. Any four-char take answers "T-72dd" here.
    expect(deriveTaskNo("t-72dd79b666d0")).toBe("T-72dd79b666d0");
  });

  // ── real-world shape ──────────────────────────────────────────────────────

  it("prints a real t-<hex12> id as the number shown on the card", () => {
    // The bug this helper exists for: the dep fallback used to print the raw
    // id form (lowercase "t-") instead of the "T-" display form.
    expect(deriveTaskNo("t-1d8292a2f8db")).toBe("T-1d8292a2f8db");
  });

  // ── malformed / boundary ids ──────────────────────────────────────────────

  it("trims the prefix rather than dropping two chars unconditionally", () => {
    // strings.TrimPrefix returns the string UNCHANGED when the prefix is
    // absent. A hand-rolled slice(2) would answer "T-c123" here.
    expect(deriveTaskNo("abc123")).toBe("T-abc123");
  });

  it("keeps a prefixless id shorter than the prefix itself intact", () => {
    expect(deriveTaskNo("x")).toBe("T-x");
  });

  it("yields the bare 'T-' for an empty id, matching the server", () => {
    // Not a fabricated placeholder and not an exception: this is literally
    // what Go's TaskNo("") returns. An id is never empty in practice, so the
    // only job of this pin is to keep the mirror faithful if it ever is.
    expect(deriveTaskNo("")).toBe("T-");
  });

  it("yields the bare 'T-' for a prefix with no hex body", () => {
    expect(deriveTaskNo("t-")).toBe("T-");
  });

  it("does not truncate even an implausibly long id", () => {
    expect(deriveTaskNo("t-" + "f".repeat(64))).toBe("T-" + "f".repeat(64));
  });
});
