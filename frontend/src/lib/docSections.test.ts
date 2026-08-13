// lib/docSections.test.ts
//
// The round trip is the assertion that matters. Every other property of this
// module is a convenience; `join(split(t)) === t` is a safety property, because
// the documents it cuts are the ones every agent in the studio boots from, and
// a splitter that quietly re-spaces a heading or drops a trailing newline turns
// "open the page and save one section" into an unreviewed rewrite of the other
// forty. So the round trip is pinned on the REAL seed files rather than on toy
// strings — those are the exact bytes the surface will be handed.

import { describe, it, expect } from "vitest";
import { splitDocSections, joinDocSections } from "./docSections";
import {
  SEED_SYSTEM_INTERACTION_MD,
  SEED_BOOT_SEQUENCE_MD,
  SEED_BOOT_SEQUENCE_CODEX_MD,
} from "../api/seeds";

const SEEDS: [string, string][] = [
  ["system_interaction", SEED_SYSTEM_INTERACTION_MD],
  ["boot_sequence", SEED_BOOT_SEQUENCE_MD],
  ["boot_sequence_codex", SEED_BOOT_SEQUENCE_CODEX_MD],
];

describe("splitDocSections", () => {
  it.each(SEEDS)("round-trips %s byte for byte", (_name, text) => {
    expect(joinDocSections(splitDocSections(text))).toBe(text);
  });

  it("round-trips the shapes a hand-written string gets wrong", () => {
    for (const text of [
      "",
      "no headings at all",
      "trailing newline\n",
      "\n\nleading blanks\n## a\nbody",
      "## only a heading",
      "# a\n\n## b\n\n### c\n",
      "1. one\n2. two\n",
      "preamble\n1. one\n",
    ]) {
      expect(joinDocSections(splitDocSections(text))).toBe(text);
    }
  });

  it("cuts the boot sequence into its numbered STEPS, not one block", () => {
    // The whole reason the grammar admits top-level ordered items. Both boot
    // sequences put every step inside a single `##` block, so a heading-only
    // splitter gives them two sections — the second holding all four steps —
    // and the owner is back to an all-or-nothing paste on the document this
    // ticket is most afraid of.
    for (const text of [SEED_BOOT_SEQUENCE_MD, SEED_BOOT_SEQUENCE_CODEX_MD]) {
      const sections = splitDocSections(text);
      expect(sections.length).toBeGreaterThanOrEqual(6);
      // Each numbered step is its own section, in order.
      const stepLabels = sections
        .map((s) => s.label)
        .filter((l) => /^\d+\. /.test(l));
      expect(stepLabels.length).toBe(4);
    }
  });

  it("cuts system_interaction into many independently pasteable pieces", () => {
    const sections = splitDocSections(SEED_SYSTEM_INTERACTION_MD);
    expect(sections.length).toBeGreaterThan(20);
    // Every section carries content — an empty piece would render an edit
    // affordance over nothing.
    for (const s of sections) expect(s.text.length).toBeGreaterThan(0);
  });

  it("does not cut inside a fenced code block", () => {
    // The seeds contain fenced shell blocks whose lines start with `#`. Cutting
    // there produces two sections, NEITHER of which is valid markdown, and the
    // damage only becomes visible after a save.
    const text = "## a\n```sh\n# not a heading\n1. not a step\n```\n## b\nx\n";
    const sections = splitDocSections(text);
    expect(sections.map((s) => s.label)).toEqual(["a", "b"]);
    expect(sections[0].text).toContain("# not a heading");
    expect(joinDocSections(sections)).toBe(text);
  });

  it("marks only the opening piece as having no boundary of its own", () => {
    const sections = splitDocSections("intro\n## a\nbody\n");
    expect(sections.map((s) => s.hasBoundary)).toEqual([false, true]);
    expect(sections[0].label).toBe("");
  });
});
