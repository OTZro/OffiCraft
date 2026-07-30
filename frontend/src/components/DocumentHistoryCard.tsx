// components/DocumentHistoryCard.tsx — the ONE shared retained-revision card
// (T-7d33). It sits directly under the editor of every editable long-form
// document (使用者自訂 global context / 角色定義 / 學習經驗 / 任務手冊), so all
// four surfaces get the same list and the same restore flow by construction.
//
// Honest states, in the house style: a REJECTED load says so (it is not the
// same screen as "this doc has never been edited"), and a restore that fails
// keeps its confirm dialog open with the server's own message.
//
// Restore is DESTRUCTIVE (it overwrites the live doc), so it always goes
// through ConfirmModal, and on success it refreshes BOTH the history list (the
// hook does that) and the visible document (`onRestored`, owned by the host —
// this card never guesses which doc hook is on screen).

import { useState } from "react";
import { useI18n } from "../i18n";
import type { DocumentHistoryView, DocumentKind } from "../types";
import { useDocumentHistory } from "../hooks/useDocumentHistory";
import { DOC_CAP_CHARS, docCapBlockedFields } from "../api/docCap";
import { ApiError } from "../api/errors";
import { ConfirmModal } from "./ConfirmModal";
import { formatAbsolute } from "../lib/dateFormat";
import { LayersIcon } from "./icons";
import "./settings.css";

/** Display order per kind, so two revisions of the same doc always lay their
 * fields out the same way (the wire map's own key order is not a contract).
 * `tombstoned` is never a preview field — it is rendered as a badge. */
const FIELD_ORDER: Record<DocumentKind, string[]> = {
  global_context: ["text"],
  role_definition: ["name", "definition_md"],
  lessons: ["text"],
  task_manual: ["purpose", "fields", "sop_md", "learnings"],
};

/** The revision's fields in display order, unknown ones appended verbatim so a
 * future server-side field is shown rather than silently dropped. */
function previewFields(
  kind: DocumentKind,
  content: Record<string, string>
): [string, string][] {
  const known = FIELD_ORDER[kind];
  const names = [
    ...known.filter((f) => f in content),
    ...Object.keys(content).filter(
      (f) => f !== "tombstoned" && !known.includes(f)
    ),
  ];
  return names
    .map((name): [string, string] => [name, content[name] ?? ""])
    .filter(([, value]) => value.trim() !== "");
}

export function DocumentHistoryCard({
  kind,
  docKey,
  currentContent,
  docDeletable,
  onRestored,
}: {
  kind: DocumentKind;
  /** "global" | role key | "<role_key>::<task_type>" | type_key. */
  docKey: string;
  /**
   * The LIVE document values, under the same wire field names the revisions
   * use — the `before` side of the server size cap. Only the capped kinds need
   * it (lessons: `text`; task_manual: `learnings` + `sop_md`); the uncapped
   * ones pass nothing. Pass `undefined` while the doc is still loading: the
   * card then abstains from marking rather than judging every revision against
   * an empty document. See api/docCap.ts.
   */
  currentContent?: Record<string, string>;
  /**
   * True when THIS document can be deleted whole from the cockpit (a task
   * manual, a custom role). Such a delete keeps no history, so the card states
   * that limit — otherwise it reads as a general undo. Left false where no
   * delete flow exists (global context, seed roles, lessons): a footnote that
   * is false for the document on screen is worse than no footnote.
   */
  docDeletable?: boolean;
  /** Re-read the document this card sits under, after a successful restore. */
  onRestored?: () => Promise<unknown> | void;
}) {
  const { t, msg } = useI18n();
  const { versions, loading, error, restore } = useDocumentHistory(kind, docKey);

  const [confirming, setConfirming] = useState<DocumentHistoryView | null>(null);
  const [busy, setBusy] = useState(false);
  const [restoreError, setRestoreError] = useState<string | null>(null);

  const nowSecs = Date.now() / 1000;
  const fieldLabel = (name: string) =>
    (t.settings.historyField as Record<string, string>)[name] ?? name;

  async function commitRestore(version: DocumentHistoryView) {
    setBusy(true);
    setRestoreError(null);
    try {
      await restore(version.id);
      await onRestored?.();
      setConfirming(null);
    } catch (e) {
      // The server's own message when it has one (404 pruned id / 400 size
      // cap); the generic line otherwise. The dialog stays open either way.
      setRestoreError(
        e instanceof ApiError && e.serverMessage
          ? e.serverMessage
          : t.settings.historyRestoreError
      );
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="doc-card doc-hist">
      <div className="doc-card__head">
        <span className="doc-hist__title">
          <LayersIcon size={15} className="doc-hist__icon" />
          <span>{t.settings.historyTitle}</span>
        </span>
      </div>
      <div className="doc-card__body">
        <div className="doc-hist__note">{t.settings.historySub}</div>
        {loading ? (
          <div className="doc-hist__empty">{t.settings.historyLoading}</div>
        ) : error ? (
          <div className="set-error">{t.settings.historyError}</div>
        ) : versions.length === 0 ? (
          <div className="doc-hist__empty">{t.settings.historyEmpty}</div>
        ) : (
          <ul className="doc-hist__list">
            {versions.map((v) => {
              const when = formatAbsolute(v.createdTs, nowSecs);
              const fields = previewFields(kind, v.content);
              // A revision the server WOULD refuse is still listed — hiding it
              // would deny the owner the one place that content still exists.
              // It is marked, told why, and its restore control is dead.
              const blockedFields = docCapBlockedFields(
                kind,
                v.content,
                currentContent
              );
              const blocked = blockedFields.length > 0;
              return (
                <li
                  className={`doc-hist__item${blocked ? " doc-hist__item--blocked" : ""}`}
                  key={v.id}
                  data-testid={`doc-history-item-${v.id}`}
                  data-blocked={blocked ? "true" : undefined}
                >
                  <div className="doc-hist__meta">
                    <span className="doc-hist__when">{when}</span>
                    <span className="doc-hist__actor">
                      {t.settings.historyByLabel} {v.actorId}
                    </span>
                    {v.content.tombstoned === "true" && (
                      <span className="set-badge">
                        {t.settings.historyDefaultBadge}
                      </span>
                    )}
                    {blocked && (
                      <span className="set-badge set-badge--blocked">
                        {t.settings.historyBlockedBadge}
                      </span>
                    )}
                    <button
                      type="button"
                      className="doc-btn doc-hist__restore"
                      data-testid={`doc-history-restore-${v.id}`}
                      disabled={blocked}
                      onClick={() => {
                        setConfirming(v);
                        setRestoreError(null);
                      }}
                    >
                      {t.settings.historyRestore}
                    </button>
                  </div>
                  {blocked && (
                    <div
                      className="doc-hist__blocked"
                      data-testid={`doc-history-blocked-${v.id}`}
                    >
                      {msg.docHistoryBlockedReason(
                        blockedFields.map(fieldLabel),
                        DOC_CAP_CHARS
                      )}
                    </div>
                  )}
                  {fields.length === 0 ? (
                    <div className="doc-hist__empty">
                      {t.settings.historyNoContent}
                    </div>
                  ) : (
                    <dl className="doc-hist__fields">
                      {fields.map(([name, value]) => (
                        <div className="doc-hist__field" key={name}>
                          <dt className="doc-hist__field-name">
                            {fieldLabel(name)}
                          </dt>
                          {/* The preview is CLAMPED and scrolls inside itself:
                           * a 10k-char revision must never set the page's
                           * width or height (see the long-token rule). */}
                          <dd className="doc-hist__field-value">{value}</dd>
                        </div>
                      ))}
                    </dl>
                  )}
                </li>
              );
            })}
          </ul>
        )}
        {docDeletable && (
          <p className="doc-hist__scope" data-testid="doc-history-scope-note">
            {t.settings.historyDeleteNote}
          </p>
        )}
      </div>

      {confirming && (
        <ConfirmModal
          testId="doc-history-restore-confirm"
          confirmTestId="doc-history-restore-confirm-btn"
          danger
          body={msg.docHistoryRestoreConfirm(
            formatAbsolute(confirming.createdTs, nowSecs)
          )}
          error={restoreError}
          busy={busy}
          cancelLabel={t.settings.cancel}
          confirmLabel={t.settings.historyRestoreConfirmAction}
          onCancel={() => {
            setConfirming(null);
            setRestoreError(null);
          }}
          onConfirm={() => void commitRestore(confirming)}
        />
      )}
    </div>
  );
}
