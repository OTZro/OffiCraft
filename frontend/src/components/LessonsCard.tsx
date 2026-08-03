// components/LessonsCard.tsx — the ONE shared, owner-editable per-role lessons
// card. Extracted from MemberDetailPanel so the persona (role-definition) page
// and any future host render the SAME editor by construction (no copy-paste
// drift). Per-role-learnings step1: the doc is scoped to `roleKey` + `taskType`.
//
// Behaviour mirrors the role_def DocDetail card: Edit → textarea → Cancel/Save,
// button-commit (so no IME composition guard is needed). Always a titled card
// (no collapse). Owner scope (this UI) may write any role; the SSE "lessons"
// topic reconciles by refetch inside useLessons.

import { useState } from "react";
import { useI18n } from "../i18n";
import { serverMessageOf } from "../api/errors";
import { useLessons } from "../hooks/useLessons";
import { Markdown } from "./Markdown";
import { DocumentHistoryEntry } from "./DocumentHistoryEntry";
import { LayersIcon, PencilIcon } from "./icons";
import "./member-detail.css";

interface LessonsCardProps {
  /** Role this lessons doc belongs to (per-role-learnings step1). */
  roleKey: string;
  /** The single fixed task_type key today is "general". */
  taskType?: string;
}

export function LessonsCard({ roleKey, taskType = "general" }: LessonsCardProps) {
  const { t } = useI18n();
  const {
    lessons,
    loading,
    error,
    refetch,
    save: saveLessons,
  } = useLessons(roleKey, taskType);

  const [editing, setEditing] = useState(false);
  const [draft, setDraft] = useState("");
  const [busy, setBusy] = useState(false);
  // 🔴 The server's REASON, not a flag (owner ruling 2026-08-03). `null` = no
  // failure; a string = failed, and that string is what the server said. The
  // doc-cap refusal carries instructions the person needs (how far over, what
  // the cap is, that stored text is NOT truncated, delete stale lines first) —
  // as a boolean, none of it could reach the screen. `""` is a real state: the
  // call failed with nothing quotable, and the render falls back to the i18n
  // copy rather than showing an empty error line.
  const [saveError, setSaveError] = useState<string | null>(null);
  const text = lessons?.text ?? "";

  function startEdit() {
    setDraft(text);
    setSaveError(null);
    setEditing(true);
  }

  function cancelEdit() {
    setEditing(false);
    setDraft("");
    setSaveError(null);
  }

  async function commit() {
    setBusy(true);
    setSaveError(null);
    try {
      await saveLessons(draft);
      setEditing(false);
      setDraft("");
    } catch (e) {
      setSaveError(serverMessageOf(e));
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="mp-card mp-lessons">
      <div className="mp-lessons__head">
        <span className="mp-lessons__title">
          <LayersIcon size={15} className="mp-lessons__icon" />
          <span>{t.mp.lessons}</span>
          {/* T-ae38: the same size/cap readout the Insight card has carried
            * since T-3809, on the same class so the two headers cannot drift
            * apart visually. The wire has served both numbers since T-3aeb —
            * the mapper was throwing them away, so Learning was the one journal
            * block whose remaining budget an agent could only discover by being
            * refused, which happens in the last minutes before a handover.
            *
            * Rendered as soon as the doc loads, INCLUDING at 0 chars: hiding it
            * while empty removes the cap exactly when someone is about to write
            * the first thing into the document. */}
          {lessons && (
            <span className="mp-insight__size">
              {lessons.sizeChars} / {lessons.capChars}
            </span>
          )}
        </span>
        {editing ? (
          <div className="mp-lessons__actions">
            {/* 版本紀錄 (T-1f39) — in the edit toolbar, like every other
              * long-form document. This doc has NO file seed, so its list
              * carries no 初始版本 row. The doc's own key is the composite
              * "<role_key>::<task_type>" the wire uses. */}
            <DocumentHistoryEntry
              kind="lessons"
              docKey={`${roleKey}::${taskType}`}
              title={t.settings.historyLessonsTitle}
              currentContent={lessons ? { text: lessons.text } : undefined}
              // A restore rewrote the doc under the editor — leaving the draft
              // up would turn 完成編輯 into an undo of the restore.
              onRestored={async () => {
                await refetch();
                cancelEdit();
              }}
              disabled={busy}
            />
            <button
              type="button"
              className="doc-btn"
              onClick={cancelEdit}
              disabled={busy}
            >
              {t.settings.cancel}
            </button>
            <button
              type="button"
              className="doc-btn doc-btn--accent"
              onClick={commit}
              disabled={busy}
            >
              {t.settings.doneEdit}
            </button>
          </div>
        ) : (
          <button
            type="button"
            className="doc-btn doc-btn--edit"
            onClick={startEdit}
            disabled={loading || error}
          >
            <PencilIcon size={14} />
            <span>{t.settings.edit}</span>
          </button>
        )}
      </div>
      <div className="mp-lessons__note">{t.mp.lessonsShared}</div>
      <div className="mp-lessons__body">
        {editing ? (
          <>
            <textarea
              className="doc-editor"
              value={draft}
              autoFocus
              spellCheck={false}
              placeholder={t.settings.editorPlaceholder}
              onChange={(e) => setDraft(e.target.value)}
            />
            {saveError !== null && (
              <div className="mp-lessons__error">
                {saveError || t.mp.lessonsSaveError}
              </div>
            )}
          </>
        ) : loading ? (
          <span className="mp-expand__empty">{t.mp.lessonsLoading}</span>
        ) : error ? (
          <span className="mp-expand__empty">{t.mp.lessonsError}</span>
        ) : text.trim() ? (
          <Markdown source={text} className="doc-md" />
        ) : (
          <span className="mp-expand__empty">{t.mp.lessonsEmpty}</span>
        )}
      </div>
    </div>
  );
}
