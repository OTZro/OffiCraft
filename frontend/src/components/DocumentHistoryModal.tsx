// components/DocumentHistoryModal.tsx — reading ONE retained revision (T-1f39).
//
// The list row used to be the whole story: a clamped preview per field and a
// restore button. You could not read a version, and you could not see what
// restoring it would actually change. Both now live here, behind a click on the
// row.
//
// TWO PANES, one surface (owner 2026-07-31):
//   - DEFAULT — the version's own content, RENDERED as markdown through the
//     shared Markdown.tsx. These documents are written in markdown and read
//     everywhere else in the cockpit as markdown; the raw-source view would be
//     a different document from the one the owner wrote.
//   - TOP-RIGHT toggle → the line-by-line diff against the CURRENT content
//     (DiffView, raw text). Raw is the point on this side: a rendered diff
//     cannot show which LINE moved, and the line numbers are the answer to
//     "what would restoring this undo".
//
// The diff's `after` is what the SERVER currently stores, not the draft in the
// editor above — `historyDiffNote` says so on screen, because the two differ
// exactly when the owner is mid-edit and that is when the diff is read.
//
// RESTORE MOVED IN HERE and is reachable nowhere else. Everything the row-level
// button carried came with it, unchanged: it is DESTRUCTIVE so it still goes
// through ConfirmModal; a failure surfaces the SERVER's own message and leaves
// both dialogs open; and a revision the server would refuse on size is inert
// here too — marked, told why, and its control dead.

import { useRef, useState } from "react";
import { useI18n } from "../i18n";
import type { DocumentHistoryView, DocumentKind } from "../types";
import { DOC_CAP_CHARS, docCapBlockedFields } from "../api/docCap";
import { ApiError } from "../api/errors";
import { comparedFieldNames, documentFields } from "../lib/docHistoryFields";
import { formatAbsolute } from "../lib/dateFormat";
import { useEscapeLayer } from "../lib/useEscapeLayer";
import { ConfirmModal } from "./ConfirmModal";
import { DiffView } from "./DiffView";
import { Markdown } from "./Markdown";
import { CloseIcon } from "./icons";
// Its own shell sheet, plus settings.css for the two SHARED atoms it wears —
// `.doc-btn` (the cockpit's document button) and `.set-badge`. Importing the
// sheet whose classes it draws with is the rule styleOwnership.test.ts exists
// for: free-riding on the card's import would leave this modal unstyled the
// moment it is mounted from anywhere else (the CT story already is).
import "./doc-hist-modal.css";
import "./settings.css";

type Pane = "content" | "diff";

export function DocumentHistoryModal({
  kind,
  version,
  actorLine,
  currentContent,
  onBack,
  onClose,
  onRestore,
}: {
  kind: DocumentKind;
  version: DocumentHistoryView;
  /** Who wrote this revision, already resolved to "name (id)" — or the bare id
   * when the roster cannot name them. Resolved by the host card, which is the
   * one holding the roster: a modal that pulled its own would fetch the whole
   * roster every time a row is clicked. */
  actorLine: string;
  /** The LIVE document under the same wire field names — the diff's `after`
   * side AND the size cap's `before`. `undefined` while the host's document is
   * still loading: the diff then says so instead of comparing against nothing,
   * and the cap verdict abstains exactly as it does on the list. */
  currentContent?: Record<string, string>;
  /** Step back to the version LIST this reader was opened from (T-1f39, owner
   * 2026-07-31). Omitted where there is no list behind it. Distinct from
   * `onClose`, which leaves the history altogether: a reader you can only exit
   * by closing makes comparing two versions a round trip through the editor. */
  onBack?: () => void;
  onClose: () => void;
  /** Restore THIS revision over the live document. Rejects on failure — the
   * modal maps the rejection to the message it shows and stays open. */
  onRestore: () => Promise<void>;
}) {
  const { t, msg } = useI18n();
  const [pane, setPane] = useState<Pane>("content");
  const [confirming, setConfirming] = useState(false);
  const [busy, setBusy] = useState(false);
  const [restoreError, setRestoreError] = useState<string | null>(null);

  // Esc closes — as a LAYER, so the confirm dialog rendered inside this root
  // takes the key while it is open and this surface only sees it once that
  // dialog is gone. While a restore is in flight the key is swallowed: a
  // committed destructive action must not lose its dialog mid-request.
  const rootRef = useRef<HTMLDivElement>(null);
  useEscapeLayer(() => {
    if (!busy) onClose();
  }, rootRef);

  const when = formatAbsolute(version.createdTs, Date.now() / 1000);
  const fieldLabel = (name: string) =>
    (t.settings.historyField as Record<string, string>)[name] ?? name;

  const blockedFields = docCapBlockedFields(kind, version.content, currentContent);
  const blocked = blockedFields.length > 0;
  const fields = documentFields(kind, version.content);
  const compared = currentContent
    ? comparedFieldNames(kind, version.content, currentContent)
    : [];

  async function commitRestore() {
    setBusy(true);
    setRestoreError(null);
    try {
      await onRestore();
      setConfirming(false);
      onClose();
    } catch (e) {
      // The server's own message when it has one (404 pruned id / 400 size
      // cap); the generic line otherwise. BOTH dialogs stay open — closing the
      // reader on a failed restore would take the reason with it.
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
    <div
      ref={rootRef}
      className="doc-hist-modal"
      data-testid="doc-history-modal"
      role="dialog"
      aria-modal="true"
      aria-label={t.settings.historyTitle}
      onClick={() => {
        if (!busy && !confirming) onClose();
      }}
    >
      <div
        className="doc-hist-modal__panel"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="doc-hist-modal__header">
          {/* WHICH version this is — the timestamp and the actor, the same two
            * facts the row carries, so the modal is never an anonymous body of
            * text the reader has to trace back to a row. */}
          <div className="doc-hist-modal__ident">
            <span className="doc-hist-modal__when">{when}</span>
            <span className="doc-hist-modal__actor">
              {t.settings.historyByLabel} {actorLine}
            </span>
            {version.content.tombstoned === "true" && (
              <span className="set-badge">{t.settings.historyDefaultBadge}</span>
            )}
            {blocked && (
              <span className="set-badge set-badge--blocked">
                {t.settings.historyBlockedBadge}
              </span>
            )}
          </div>
          <div className="doc-hist-modal__actions">
            <div
              className="doc-hist-modal__tabs"
              role="group"
              aria-label={t.settings.historyPaneLabel}
            >
              {(["content", "diff"] as Pane[]).map((which) => (
                <button
                  key={which}
                  type="button"
                  className={`doc-hist-modal__tab${pane === which ? " doc-hist-modal__tab--on" : ""}`}
                  data-testid={`doc-history-pane-${which}`}
                  aria-pressed={pane === which}
                  onClick={() => setPane(which)}
                >
                  {which === "content"
                    ? t.settings.historyPaneContent
                    : t.settings.historyPaneDiff}
                </button>
              ))}
            </div>
            <button
              type="button"
              className="doc-hist-modal__close"
              data-testid="doc-history-modal-close"
              aria-label={t.settings.historyClose}
              onClick={onClose}
            >
              <CloseIcon size={16} />
            </button>
          </div>
        </div>

        <div className="doc-hist-modal__body" data-pane={pane}>
          {pane === "content" ? (
            fields.length === 0 ? (
              <p className="doc-hist-modal__notice">
                {t.settings.historyModalEmpty}
              </p>
            ) : (
              fields.map(([name, value]) => (
                <section className="doc-hist-modal__field" key={name}>
                  {/* A single-field kind (SOP, 學習經驗, 全域情境) needs no
                    * label — the modal's own document IS that field. A kind
                    * that carries several keeps them named and apart. */}
                  {fields.length > 1 && (
                    <h3 className="doc-hist-modal__field-name">
                      {fieldLabel(name)}
                    </h3>
                  )}
                  <Markdown
                    source={value}
                    className="doc-hist-modal__md doc-md"
                  />
                </section>
              ))
            )
          ) : currentContent === undefined ? (
            <p
              className="doc-hist-modal__notice"
              data-testid="doc-history-diff-pending"
            >
              {t.settings.historyDiffPending}
            </p>
          ) : (
            <>
              <p className="doc-hist-modal__diff-note">
                {t.settings.historyDiffNote}
              </p>
              {compared.length === 0 ? (
                <p className="doc-hist-modal__notice">
                  {t.settings.historyModalEmpty}
                </p>
              ) : (
                compared.map((name) => (
                  <section className="doc-hist-modal__field" key={name}>
                    {compared.length > 1 && (
                      <h3 className="doc-hist-modal__field-name">
                        {fieldLabel(name)}
                      </h3>
                    )}
                    <DiffView
                      before={version.content[name] ?? ""}
                      after={currentContent[name] ?? ""}
                      beforeLabel={msg.docHistoryVersionLabel(when)}
                      afterLabel={t.settings.historyCurrentLabel}
                      testId={`doc-history-diff-${name}`}
                    />
                  </section>
                ))
              )}
            </>
          )}
        </div>

        {blocked && (
          <div
            className="doc-hist-modal__blocked"
            data-testid="doc-history-modal-blocked"
          >
            {msg.docHistoryBlockedReason(
              blockedFields.map(fieldLabel),
              DOC_CAP_CHARS
            )}
          </div>
        )}

        <div className="doc-hist-modal__footer">
          {onBack && (
            <button
              type="button"
              className="doc-btn doc-hist-modal__back"
              data-testid="doc-history-modal-back"
              onClick={onBack}
            >
              {t.settings.historyBack}
            </button>
          )}
          <button
            type="button"
            className="doc-btn"
            data-testid="doc-history-modal-close-footer"
            onClick={onClose}
          >
            {t.settings.historyClose}
          </button>
          <button
            type="button"
            className="doc-btn doc-hist-modal__restore"
            data-testid="doc-history-modal-restore"
            disabled={blocked}
            onClick={() => {
              setConfirming(true);
              setRestoreError(null);
            }}
          >
            {t.settings.historyRestore}
          </button>
        </div>
      </div>

      {confirming && (
        <ConfirmModal
          testId="doc-history-restore-confirm"
          confirmTestId="doc-history-restore-confirm-btn"
          danger
          body={msg.docHistoryRestoreConfirm(when)}
          error={restoreError}
          busy={busy}
          cancelLabel={t.settings.cancel}
          confirmLabel={t.settings.historyRestoreConfirmAction}
          onCancel={() => {
            setConfirming(false);
            setRestoreError(null);
          }}
          onConfirm={() => void commitRestore()}
        />
      )}
    </div>
  );
}
