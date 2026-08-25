// Pins for deriveTaskNo (taskNo.ts) — the frontend statement of the same rule
// as `TaskNo` in server/ocserverd/domain.go.
//
// The cases are COPIED VERBATIM from the server's own pin (api_tasks_test.go,
// TestTaskNoIsTheIDItself). That is deliberate: the two sides state the same
// rule with no shared code, so the only thing keeping them honest is that both
// are nailed to the SAME FACTS. Keep them in sync — change a case here, change
// it there too.

import { describe, it, expect } from "vitest";
import { deriveTaskNo } from "./taskNo";

describe("deriveTaskNo", () => {
  it("is the id itself — no prefix swap, no case change, no truncation", () => {
    for (const id of ["t-72dd79b666d0", "t-7d40aabbccdd", "t-ab", ""]) {
      expect(deriveTaskNo(id)).toBe(id);
    }
  });

  it("does not upper-case the prefix", () => {
    // The character that killed the intermediate "T-…" version: lookup is
    // byte-exact, so a re-cased number cannot be pasted back.
    expect(deriveTaskNo("t-72dd79b666d0")).not.toMatch(/^T-/);
  });

  it("passes a prefixless id through untouched", () => {
    expect(deriveTaskNo("abc123")).toBe("abc123");
  });
});
