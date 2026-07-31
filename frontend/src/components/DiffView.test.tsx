// DiffView — the git-style unified diff surface.
//
//   1. A one-line edit is exactly one `-` row and one `+` row, each carrying
//      its own side's line number (the pair a reader lines up).
//   2. A collapsed run announces HOW MANY lines it hid, not just that it hid.
//   3. "identical" and "refused because too large" are two different screens.
//   4. Every visible string comes from the dictionary, in both languages.

import { describe, it, expect, beforeEach } from "vitest";
import { render } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { zh } from "../i18n/locales/zh";
import { en } from "../i18n/locales/en";
import { DiffView } from "./DiffView";
import type { LineDiffOptions } from "../lib/lineDiff";

function renderDiff(before: string, after: string, options?: LineDiffOptions) {
  return render(
    <I18nProvider>
      <DiffView before={before} after={after} options={options} />
    </I18nProvider>
  );
}

/** [beforeLine, afterLine, marker] per row, in render order. */
function rowCells(container: HTMLElement): string[][] {
  return Array.from(container.querySelectorAll(".diff-view__row")).map((row) =>
    Array.from(row.querySelectorAll("td")).map((td) => td.textContent ?? "")
  );
}

/** The context marker — a non-breaking space, so the row keeps its height. */
const NBSP = "\u00a0";

const NUMBERED = (count: number) =>
  Array.from({ length: count }, (_, i) => `line ${i + 1}`).join("\n");

describe("DiffView", () => {
  beforeEach(() => localStorage.clear());

  it("renders a one-line edit as one removed row and one added row with both side's line numbers", () => {
    const { container } = renderDiff(
      "alpha\nbravo\ncharlie",
      "alpha\nBRAVO\ncharlie"
    );

    const removed = container.querySelectorAll('[data-kind="removed"]');
    const added = container.querySelectorAll('[data-kind="added"]');
    expect(removed.length).toBe(1);
    expect(added.length).toBe(1);

    // The removed line is #2 on the before side and nowhere on the after side;
    // the added line is the mirror. Getting these backwards is the classic
    // unified-diff bug, so they are asserted cell by cell.
    expect(rowCells(container)).toEqual([
      ["1", "1", NBSP, "alpha"],
      ["2", "", "-", "bravo"],
      ["", "2", "+", "BRAVO"],
      ["3", "3", NBSP, "charlie"],
    ]);
  });

  it("labels the marker cell so added/removed is not carried by colour alone", () => {
    const { container } = renderDiff("bravo", "BRAVO");
    const marker = (kind: string) =>
      container
        .querySelector(`[data-kind="${kind}"] .diff-view__marker`)
        ?.getAttribute("aria-label");
    expect(marker("removed")).toBe(zh.diff.removedLine);
    expect(marker("added")).toBe(zh.diff.addedLine);
  });

  it("renders a separator carrying the number of unchanged lines it collapsed", () => {
    const before = NUMBERED(20);
    const after = before.replace("line 15", "line 15 edited");
    const { container, getByTestId } = renderDiff(before, after);

    const skip = getByTestId("diff-view-skip");
    // 14 leading context lines minus the 3-line radius the hunk keeps.
    expect(skip.getAttribute("data-skipped")).toBe("11");
    expect(skip.textContent).toContain("11");
    expect(skip.textContent).toContain(zh.diff.skippedTail);
    // The @@ header still reports the hunk's extents on both sides.
    expect(skip.textContent).toContain("@@ -12,7 +12,7 @@");
    // The collapse is real: the first rendered row is line 12, not line 1.
    expect(rowCells(container)[0][0]).toBe("12");
  });

  it("renders the no-difference message and no rows for identical inputs", () => {
    const { container, getByTestId, queryByTestId } = renderDiff(
      "alpha\nbravo",
      "alpha\nbravo"
    );
    expect(getByTestId("diff-view-empty").textContent).toBe(zh.diff.noChanges);
    expect(container.querySelectorAll(".diff-view__row").length).toBe(0);
    expect(queryByTestId("diff-view-too-large")).toBeNull();
  });

  it("renders the too-large message and no rows when the diff is refused", () => {
    const { container, getByTestId, queryByTestId } = renderDiff(
      NUMBERED(5),
      NUMBERED(6),
      { maxLines: 2 }
    );
    // The longer side's count is named — a bare "too long" leaves the owner
    // unable to tell how far past the ceiling the document is.
    expect(getByTestId("diff-view-too-large").textContent).toBe(
      `${zh.diff.tooLargeLead}6${zh.diff.tooLargeTail}`
    );
    expect(container.querySelectorAll(".diff-view__row").length).toBe(0);
    // A refusal must never read as "the two versions match".
    expect(queryByTestId("diff-view-empty")).toBeNull();
  });

  it.each([
    ["zh", zh],
    ["en", en],
  ])("resolves its strings from the %s dictionary", (language, dict) => {
    localStorage.setItem("oc.language", language);

    const identical = renderDiff("same", "same");
    expect(identical.getByTestId("diff-view-empty").textContent).toBe(
      dict.diff.noChanges
    );
    identical.unmount();

    const before = NUMBERED(20);
    const { container, getByTestId } = renderDiff(
      before,
      before.replace("line 15", "line 15 edited")
    );
    expect(getByTestId("diff-view-skip").textContent).toBe(
      `@@ -12,7 +12,7 @@${dict.diff.skippedLead}11${dict.diff.skippedTail}`
    );
    expect(
      container.querySelector(".diff-view__table")?.getAttribute("aria-label")
    ).toBe(dict.diff.ariaLabel);
    expect(
      container.querySelector(".diff-view__label--before")?.textContent
    ).toBe(`-${dict.diff.beforeLabel}`);
    expect(
      container.querySelector(".diff-view__label--after")?.textContent
    ).toBe(`+${dict.diff.afterLabel}`);
  });

  it("uses caller-supplied side labels over the dictionary defaults", () => {
    const { container } = renderDiff("bravo", "BRAVO", undefined);
    expect(
      container.querySelector(".diff-view__label--before")?.textContent
    ).toBe(`-${zh.diff.beforeLabel}`);

    const labelled = render(
      <I18nProvider>
        <DiffView
          before="bravo"
          after="BRAVO"
          beforeLabel="2026-07-30 14:02"
          afterLabel="現在"
        />
      </I18nProvider>
    );
    expect(
      labelled.container.querySelector(".diff-view__label--before")?.textContent
    ).toBe("-2026-07-30 14:02");
    expect(
      labelled.container.querySelector(".diff-view__label--after")?.textContent
    ).toBe("+現在");
  });
});
