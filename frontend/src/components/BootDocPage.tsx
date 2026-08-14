// components/BootDocPage.tsx — the editable surface for ONE boot-context block
// (T-791e): 系統互動, 啟動程序（Claude Code）, 啟動程序（Codex CLI）.
//
// 🔴 THIS FILE HOLDS NO EDITOR. It is the three blocks' wiring — which document,
// which words, which slots — and nothing else: no draft state, no textarea, no
// button group, no usage readout, no confirmation of its own. All of that is
// <DocCard>, the one shell every editable document in 設定 draws itself with
// (T-c33e). The version this replaced hand-copied that markup, which is how the
// two surfaces drifted into two different shapes of the same card; the owner's
// ruling is that they must be ONE component, so the only thing this page may
// own is what is genuinely特有 about a boot-context block.
//
// What it owes on top of "edit / version history / restore to factory" comes
// from one sentence: THE PERSON PRESSING THE BUTTON IS THE OWNER, NOT US.
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
// ONLINE TO FIX IT. Two consequences are load-bearing and must not be
// "tidied up":
//   * 還原出廠版 is a TOP-LEVEL button, rendered before the document and
//     independent of it — not behind edit mode, not behind a successful read,
//     not behind any agent being up. (It ALSO exists as the history list's
//     初始版本 row: that one is the comparison path, this one is the recovery
//     path, and the recovery path may not have prerequisites.) DocCard renders
//     it from `factoryReset`.
//   * Saving goes through a confirmation that STATES that consequence.
//
// 🔴 SAVING REPLACES THE WHOLE DOCUMENT, and this page says so on screen
// (`replaceNote`). The editor here used to be per-section — paste one block,
// apply it, leave the rest alone — and the wire was a whole-document replace
// underneath it either way. With one editor over the whole text, the thing that
// was implicit in the section rows has to be stated, because the failure it
// prevents (pasting one proposed block over a 45,000-character document and
// saving the rest away) is silent and unrecoverable except through history.
//
// 🔴 The claude and codex boot sequences are TWO DIFFERENT DOCUMENTS — their
// third step means opposite things (one attaches `ocagent listen` itself, the
// other must NOT and hands that to the sidecar). So each opens its own page
// from its own list row; there is no "apply this text to both runtimes"
// affordance and the two are never rendered side by side, because a
// side-by-side invites exactly the copy this page exists to prevent.

import { useI18n } from "../i18n";
import type { BootDocKind } from "../types";
import { useBootDoc } from "../hooks/useBootDoc";
import { DocCard } from "./DocCard";
import { type Crumb } from "./Breadcrumbs";
import { BOOT_DOC_HISTORY_KEPT, runeLength } from "../api/docCap";
import "./settings.css";
import "./boot-doc.css";

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
   * one versioned document, but the list is the same component every editable
   * document shares, and 「版本紀錄」 alone cannot say which runtime it holds. */
  historyTitle: string;
  crumbs: Crumb[];
}) {
  const { t, msg } = useI18n();
  const { doc, error, refetch, save, reset } = useBootDoc(kind, docKey);

  return (
    <DocCard
      title={title}
      crumbs={crumbs}
      doc={doc}
      onSave={save}
      onReset={reset}
      // The STORED size; DocCard follows the draft once the editor is open.
      // `doc === null` passes none rather than "0 / 0", which would read as a
      // real budget of zero.
      usage={doc ? { size: runeLength(doc.text), cap: doc.capChars } : undefined}
      replaceNote={t.settings.docReplaceNote}
      // A no-op save would flip the document out of 預設 for ever, and these
      // three are the documents where "is this still the factory version" is
      // the question people ask about them.
      requireDirty
      confirmSave={{
        // The boot sequences get the sentence about the silent failure; the
        // system-interaction block does not, because it is not true of it — an
        // agent with a mangled system block still boots. A warning that is
        // false for the document on screen is worse than none: it teaches the
        // reader to dismiss the real one.
        body:
          kind === "boot_sequence"
            ? t.settings.bootDocSaveConfirmBoot
            : t.settings.bootDocSaveConfirmSystem,
        confirmLabel: t.settings.bootDocSaveConfirmAction,
      }}
      factoryReset={{
        label: t.settings.bootDocReset,
        note: t.settings.bootDocResetNote,
        confirmBody: t.settings.bootDocResetConfirm,
        confirmLabel: t.settings.bootDocResetConfirmAction,
      }}
      // No explanatory notes block above the card. There used to be three
      // bullets here (what a save affects, how many revisions are kept, what
      // the cap does). The owner asked for them out on 2026-08-14 with an
      // argument that generalises: if that explanation were needed, EVERY
      // editable context block would need it — and none of the others carry
      // one. So it was not this document being special, it was noise.
      errorNote={error ? <div className="set-error">{t.settings.loadError}</div> : null}
      history={{
        kind,
        docKey,
        title: historyTitle,
        // The list's default note says the cockpit keeps the last THREE — true
        // everywhere else, false here. Overriding it is not decoration: an
        // owner who reads "3" under a list of ten will assume something is
        // broken, and one who reads "10" without "counted in saves" will assume
        // a run of small edits lost nothing.
        note: msg.bootDocNoteHistory(BOOT_DOC_HISTORY_KEPT),
        // The live document under its WIRE field name: the modal diffs a
        // revision against what the server currently stores.
        currentContent: doc ? { text: doc.text } : undefined,
        onRestored: refetch,
      }}
    />
  );
}
