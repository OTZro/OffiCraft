// components/BootDocPage.tsx — the editable surface for ONE boot-context block
// (T-791e): 系統互動, 啟動程序（Claude Code）, 啟動程序（Codex CLI）.
//
// It replaces three read-only seed previews. The owner's ask was「跟銀月的
// insight 一樣是有 history / restore to default」＋「這樣我們可以先在我們這裡體驗
// 修改的結果」「不用每次都改 code」, so the three things this page owes are
// edit / version history / restore-to-factory. What it owes ON TOP of those,
// and what most of the code below is about, comes from one sentence: THE PERSON
// PRESSING THE BUTTON IS THE OWNER, NOT US.
//
//   1. A proposal arrives as whole, pasteable blocks — never a diff, never
//      "change line 3 to…". So the paste target is a block-shaped textarea.
//   2. He will agree with three of seven. So the EDITING UNIT HAS TO BE ABLE TO
//      BE SMALLER THAN THE DOCUMENT. A single whole-document textarea cannot
//      serve that no matter how it is styled — hence lib/docSections.ts and the
//      per-section apply below. The WRITE is still one whole-document replace;
//      it is the editing unit that is smaller, not the wire.
//   3. He picked this direction so he could「自己改、當場看結果」— so a pasted
//      block is PREVIEWED (rendered as the agent will read it) before it is
//      applied, and applied sections are visible on the page before anything is
//      saved.
//
// 🔴 THREE THINGS THIS PAGE MUST SAY OUT LOUD, because a cockpit that stays
// quiet about them makes correct behaviour look broken:
//   * WHEN IT TAKES EFFECT — only agents that boot after the save; a member
//     already running does not change. Without this line, saving and then
//     watching a live agent behave the old way reads as "the save did nothing".
//   * VERSION HISTORY KEEPS TEN, COUNTED IN SAVES — not in time. A run of small
//     saves pushes the older ones out, which is alarming if you thought it kept
//     a week. 還原出廠版 is never consumed by any of that, and says so.
//   * THE CHARACTER LIMIT — over-limit is refused with both numbers on screen,
//     never silently truncated.
//
// 🔴 THE FAILURE THIS SURFACE RISKS IS SILENT. A broken boot sequence means the
// agent never attaches to SSE, so it never comes online, so there is NOBODY
// ONLINE TO FIX IT. Two consequences are load-bearing here and must not be
// "tidied up":
//   * 還原出廠版 is a TOP-LEVEL button on the page, rendered before the document
//     and independent of it. It is not behind edit mode, not behind a
//     successful read, and not behind any agent being up. (It ALSO exists as
//     the history list's 初始版本 row, which is where the rest of the cockpit
//     puts it — that one is the comparison path, this one is the recovery
//     path, and the recovery path may not have prerequisites.)
//   * Saving goes through a confirmation that STATES that consequence.
//
// 🔴 The claude and codex boot sequences are TWO DIFFERENT DOCUMENTS — their
// third step means opposite things (one attaches `ocagent listen` itself, the
// other must NOT and hands that to the sidecar). So each opens its own page
// from its own list row; there is no "apply this text to both runtimes"
// affordance and the two are never rendered side by side, because a side-by-side
// invites exactly the copy this page exists to prevent.

import { useEffect, useMemo, useState } from "react";
import { useI18n } from "../i18n";
import type { BootDocKind } from "../types";
import { useBootDoc } from "../hooks/useBootDoc";
import { splitDocSections, joinDocSections } from "../lib/docSections";
import { Markdown } from "./Markdown";
import { Breadcrumbs, type Crumb } from "./Breadcrumbs";
import { ConfirmModal } from "./ConfirmModal";
import { DocumentHistoryEntry } from "./DocumentHistoryEntry";
import { BOOT_DOC_HISTORY_KEPT, runeLength } from "../api/docCap";
import "./settings.css";
import "./boot-doc.css";

/** Which confirmation is open. A union rather than two booleans: "both at once"
 * is not a state this page can be in, so it must not be representable. */
type Pending = { kind: "save" } | { kind: "reset" } | null;

export function BootDocPage({
  kind,
  docKey,
  title,
  historyTitle,
  crumbs,
}: {
  kind: BootDocKind;
  /** "global" for system_interaction; the RUNTIME ("claude" / "codex") for
   * boot_sequence. Required, never defaulted — see the header. */
  docKey: string;
  title: string;
  /** Names the document inside its own history list. This page carries exactly
   * one versioned document, but the list is the same component three pages
   * share, and 「版本紀錄」 alone cannot say which runtime it holds. */
  historyTitle: string;
  crumbs: Crumb[];
}) {
  const { t, msg } = useI18n();
  const { doc, error, refetch, save, reset } = useBootDoc(kind, docKey);
  const [busy, setBusy] = useState(false);
  const [pending, setPending] = useState<Pending>(null);
  const [failed, setFailed] = useState<string | null>(null);
  /** Applied section replacements, keyed by section id. Empty = the page shows
   * exactly what the server holds. */
  const [applied, setApplied] = useState<Record<string, string>>({});
  /** The one section whose paste box is open, if any. */
  const [editing, setEditing] = useState<{
    id: string;
    draft: string;
    preview: boolean;
  } | null>(null);

  const serverText = doc?.text ?? "";
  const base = useMemo(() => splitDocSections(serverText), [serverText]);

  // A save, a restore or an SSE-driven refetch replaces the document under the
  // page, so anything still pending was written against text that is gone.
  // Dropping it is the honest move: keeping it would silently re-apply an edit
  // on top of a version the owner never saw.
  useEffect(() => {
    setApplied({});
    setEditing(null);
  }, [serverText]);

  const shown = base.map((s) => ({ ...s, text: applied[s.id] ?? s.text }));
  const composed = joinDocSections(shown);
  const size = runeLength(composed);
  const cap = doc?.capChars ?? 0;
  // Mirrors the server's own rule (docCapBlocked): over the cap is refused
  // unless the document is getting shorter, so an already-over-cap document can
  // still be edited downward instead of being frozen.
  const overCap = doc !== null && size > cap && size >= runeLength(serverText);
  const dirty = Object.keys(applied).length > 0;

  function applySection(id: string, text: string) {
    setApplied((prev) => ({ ...prev, [id]: text }));
    setEditing(null);
  }

  function discardSection(id: string) {
    setApplied((prev) => {
      const next = { ...prev };
      delete next[id];
      return next;
    });
  }

  async function run(action: () => Promise<void>) {
    setBusy(true);
    setFailed(null);
    try {
      await action();
      setPending(null);
      setApplied({});
      setEditing(null);
    } catch {
      setFailed(t.settings.bootDocActionFailed);
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="settings">
      <Breadcrumbs items={crumbs} />
      <h1 className="settings__title settings__title--doc">{title}</h1>

      {/* The three things a quiet cockpit would let the owner misread. Rendered
       * unconditionally — before the document, and regardless of whether it
       * loaded — because all three are true of a page whose read failed too. */}
      <ul className="boot-doc__notes" data-testid="boot-doc-notes">
        <li>{t.settings.bootDocNoteEffective}</li>
        <li>{msg.bootDocNoteHistory(BOOT_DOC_HISTORY_KEPT)}</li>
        <li>{t.settings.bootDocNoteCap}</li>
      </ul>

      {/* The recovery path. Top level, no prerequisites — see the header. */}
      <div className="boot-doc__recover">
        <button
          type="button"
          className="doc-btn doc-btn--danger"
          data-testid="boot-doc-reset"
          disabled={busy}
          onClick={() => {
            setFailed(null);
            setPending({ kind: "reset" });
          }}
        >
          {t.settings.bootDocReset}
        </button>
        <span className="boot-doc__recover-note">
          {t.settings.bootDocResetNote}
        </span>
      </div>

      {error && <div className="set-error">{t.settings.loadError}</div>}

      <div className="doc-card">
        <div className="doc-card__head">
          <span className="doc-card__file">
            {doc?.isDefault && (
              <span className="set-badge">{t.settings.defaultBadge}</span>
            )}
            {/* Always on screen, editing included — that is precisely when the
              * number is wanted. `doc === null` renders nothing rather than
              * "0 / 0", which would read as a real budget of zero. */}
            {doc && (
              <span
                className="doc-card__usage"
                data-testid="boot-doc-usage"
                title={t.settings.docUsage}
              >
                {size} / {cap}
              </span>
            )}
          </span>
          <div className="doc-card__actions">
            <DocumentHistoryEntry
              kind={kind}
              docKey={docKey}
              title={historyTitle}
              // The list's default note says the cockpit keeps the last THREE
              // — true everywhere else, false here. Overriding it with the
              // page's own sentence is not decoration: an owner who reads "3"
              // under a list of ten will assume something is broken, and one
              // who reads "10" without "counted in saves" will assume a run of
              // small edits lost nothing.
              note={msg.bootDocNoteHistory(BOOT_DOC_HISTORY_KEPT)}
              // The live document under its WIRE field name: the modal diffs a
              // revision against what the server currently stores, which is the
              // SERVER text — not `composed`, which includes edits nobody has
              // saved and would make the diff describe a version that does not
              // exist anywhere.
              currentContent={doc ? { text: doc.text } : undefined}
              onRestored={refetch}
              onReset={reset}
              disabled={busy}
            />
            <button
              type="button"
              className="doc-btn doc-btn--accent"
              data-testid="boot-doc-save"
              disabled={busy || !dirty || overCap}
              onClick={() => {
                setFailed(null);
                setPending({ kind: "save" });
              }}
            >
              {t.settings.bootDocSave}
            </button>
          </div>
        </div>

        {/* Blocked in the cockpit, with BOTH numbers, before anything is sent —
          * the alternative the owner ruled out is a silent truncation. */}
        {overCap && (
          <div className="set-error" data-testid="boot-doc-over-cap">
            {msg.bootDocOverCap(size, cap)}
          </div>
        )}
        {dirty && !overCap && (
          <div className="boot-doc__dirty" data-testid="boot-doc-dirty">
            {t.settings.bootDocDirtyNote}
          </div>
        )}

        <div className="doc-card__body">
          {shown.map((section, i) => {
            const open = editing?.id === section.id;
            const changed = applied[section.id] !== undefined;
            const label = section.hasBoundary
              ? section.label
              : t.settings.bootDocPreamble;
            return (
              <section
                className="boot-doc-sec"
                key={section.id}
                data-testid={`boot-doc-sec-${i}`}
              >
                <div className="boot-doc-sec__head">
                  <span className="boot-doc-sec__label">{label}</span>
                  {changed && (
                    <span
                      className="set-badge"
                      data-testid={`boot-doc-sec-pending-${i}`}
                    >
                      {t.settings.bootDocPending}
                    </span>
                  )}
                  <span className="boot-doc-sec__actions">
                    {open ? (
                      <>
                        <button
                          type="button"
                          className="doc-btn"
                          data-testid={`boot-doc-sec-preview-${i}`}
                          onClick={() =>
                            setEditing({ ...editing, preview: !editing.preview })
                          }
                        >
                          {editing.preview
                            ? t.settings.bootDocBackToEdit
                            : t.settings.bootDocPreview}
                        </button>
                        <button
                          type="button"
                          className="doc-btn"
                          onClick={() => setEditing(null)}
                        >
                          {t.settings.bootDocCancelSection}
                        </button>
                        <button
                          type="button"
                          className="doc-btn doc-btn--accent"
                          data-testid={`boot-doc-sec-apply-${i}`}
                          onClick={() =>
                            applySection(section.id, editing.draft)
                          }
                        >
                          {t.settings.bootDocApplySection}
                        </button>
                      </>
                    ) : (
                      <>
                        {changed && (
                          <button
                            type="button"
                            className="doc-btn"
                            data-testid={`boot-doc-sec-discard-${i}`}
                            onClick={() => discardSection(section.id)}
                          >
                            {t.settings.bootDocDiscardSection}
                          </button>
                        )}
                        <button
                          type="button"
                          className="doc-btn doc-btn--edit"
                          data-testid={`boot-doc-sec-paste-${i}`}
                          onClick={() =>
                            setEditing({
                              id: section.id,
                              draft: section.text,
                              preview: false,
                            })
                          }
                        >
                          {t.settings.bootDocPasteSection}
                        </button>
                      </>
                    )}
                  </span>
                </div>
                <div className="boot-doc-sec__body">
                  {open && !editing.preview ? (
                    <textarea
                      className="doc-editor"
                      data-testid={`boot-doc-sec-editor-${i}`}
                      value={editing.draft}
                      autoFocus
                      spellCheck={false}
                      placeholder={t.settings.bootDocEditorPlaceholder}
                      onChange={(e) =>
                        setEditing({ ...editing, draft: e.target.value })
                      }
                    />
                  ) : (
                    <Markdown
                      source={open ? editing.draft : section.text}
                      className="doc-md"
                    />
                  )}
                </div>
              </section>
            );
          })}
        </div>
      </div>

      {pending && (
        <ConfirmModal
          testId={
            pending.kind === "save"
              ? "boot-doc-save-confirm"
              : "boot-doc-reset-confirm"
          }
          confirmTestId={
            pending.kind === "save"
              ? "boot-doc-save-confirm-btn"
              : "boot-doc-reset-confirm-btn"
          }
          danger
          body={
            pending.kind === "reset"
              ? t.settings.bootDocResetConfirm
              : // The boot sequences get the sentence about the silent failure;
                // the system-interaction block does not, because it is not true
                // of it — an agent with a mangled system block still boots. A
                // warning that is false for the document on screen is worse
                // than none: it teaches the reader to dismiss the real one.
                kind === "boot_sequence"
                ? t.settings.bootDocSaveConfirmBoot
                : t.settings.bootDocSaveConfirmSystem
          }
          error={failed}
          busy={busy}
          cancelLabel={t.settings.addRoleCancel}
          confirmLabel={
            pending.kind === "save"
              ? t.settings.bootDocSaveConfirmAction
              : t.settings.bootDocResetConfirmAction
          }
          onCancel={() => {
            setPending(null);
            setFailed(null);
          }}
          onConfirm={() =>
            void run(
              pending.kind === "save" ? () => save(composed) : () => reset()
            )
          }
        />
      )}
    </div>
  );
}
