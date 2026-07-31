// components/DiffView.tsx — the shared git-style unified diff surface.
//
// Purely presentational: it owns nothing but "show me how these two texts
// differ". It takes the two TEXTS rather than a precomputed LineDiffResult
// because the pairing is one-to-one — every caller that has a `before`/`after`
// pair would otherwise have to import lib/lineDiff, remember which options the
// cockpit standardises on, and memoise the call itself. Keeping diffLines in
// here means one call site can drift instead of many. `options` is still
// forwarded so a host can widen the context radius or lower the ceiling.
//
// A "too-large" diff and an all-context diff are DIFFERENT screens: the first
// says the comparison was refused, the second says the two versions match.
// Collapsing them into one blank panel would hide a refusal behind good news.

import { useMemo } from "react";
import { useI18n } from "../i18n";
import { diffLines, type DiffRowKind, type LineDiffOptions } from "../lib/lineDiff";
import "./diff-view.css";

// An empty cell collapses the row's height, so the context marker and a
// blank line both render a non-breaking space rather than nothing.
const NBSP = "\u00a0";

const MARKER: Record<DiffRowKind, string> = {
  context: NBSP,
  added: "+",
  removed: "-",
};

export function DiffView({
  before,
  after,
  beforeLabel,
  afterLabel,
  options,
  testId = "diff-view",
}: {
  /** The historical version — the `-` side. */
  before: string;
  /** The current stored content — the `+` side. */
  after: string;
  beforeLabel?: string;
  afterLabel?: string;
  options?: LineDiffOptions;
  testId?: string;
}) {
  const { t, msg } = useI18n();
  const { collapseUnchanged, contextRadius, maxLines } = options ?? {};
  const result = useMemo(
    () => diffLines(before, after, { collapseUnchanged, contextRadius, maxLines }),
    [before, after, collapseUnchanged, contextRadius, maxLines]
  );

  const kindLabel: Record<DiffRowKind, string> = {
    context: t.diff.contextLine,
    added: t.diff.addedLine,
    removed: t.diff.removedLine,
  };

  if (result.status === "too-large") {
    return (
      <div className="diff-view" data-testid={testId}>
        <p className="diff-view__notice" data-testid="diff-view-too-large">
          {msg.diffTooLarge(
            Math.max(result.beforeLineCount, result.afterLineCount)
          )}
        </p>
      </div>
    );
  }

  // Asked of the ROWS, not the hunks: with collapsing off an identical pair
  // still produces one all-context hunk.
  if (result.rows.every((row) => row.kind === "context")) {
    return (
      <div className="diff-view" data-testid={testId}>
        <p className="diff-view__notice" data-testid="diff-view-empty">
          {t.diff.noChanges}
        </p>
      </div>
    );
  }

  return (
    <div className="diff-view" data-testid={testId}>
      <div className="diff-view__head">
        <span className="diff-view__label diff-view__label--before">
          <span aria-hidden="true">{MARKER.removed}</span>
          {beforeLabel ?? t.diff.beforeLabel}
        </span>
        <span className="diff-view__label diff-view__label--after">
          <span aria-hidden="true">{MARKER.added}</span>
          {afterLabel ?? t.diff.afterLabel}
        </span>
      </div>
      <div className="diff-view__scroll">
        <table className="diff-view__table" aria-label={t.diff.ariaLabel}>
          {result.hunks.map((hunk, h) => (
            <tbody key={h}>
              {hunk.skippedBefore > 0 && (
                <tr
                  className="diff-view__skip"
                  data-testid="diff-view-skip"
                  data-skipped={hunk.skippedBefore}
                >
                  <td className="diff-view__skip-cell" colSpan={4}>
                    <span className="diff-view__skip-range" aria-hidden="true">
                      {`@@ -${hunk.beforeStart},${hunk.beforeCount} +${hunk.afterStart},${hunk.afterCount} @@`}
                    </span>
                    <span className="diff-view__skip-note">
                      {msg.diffSkipped(hunk.skippedBefore)}
                    </span>
                  </td>
                </tr>
              )}
              {hunk.rows.map((row, r) => (
                <tr
                  key={r}
                  className={`diff-view__row diff-view__row--${row.kind}`}
                  data-testid="diff-view-row"
                  data-kind={row.kind}
                >
                  <td className="diff-view__ln">{row.beforeLine ?? ""}</td>
                  <td className="diff-view__ln">{row.afterLine ?? ""}</td>
                  {/* The glyph carries the meaning the colour also carries, and
                    * its label carries it again for a reader who gets neither. */}
                  <td className="diff-view__marker" aria-label={kindLabel[row.kind]}>
                    {MARKER[row.kind]}
                  </td>
                  <td className="diff-view__text">{row.text || NBSP}</td>
                </tr>
              ))}
            </tbody>
          ))}
        </table>
      </div>
    </div>
  );
}
