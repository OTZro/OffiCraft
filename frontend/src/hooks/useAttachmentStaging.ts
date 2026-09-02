// hooks/useAttachmentStaging.ts — the ONE composer attachment-staging state
// machine, extracted from ChatArea so every reply surface (chat composer, the
// 等我回覆 reply cards, B3's inline chat card) stages uploads identically:
// same size/count caps, same paste/pick funnels, same preview shape.

import { useRef, useState } from "react";
import { useI18n } from "../i18n";

// Client-side size guards — mirror the backend (handlers): an image/*
// attachment is capped at 20 MB, any other file at 100 MB. We fail fast in the
// UI before uploading; the server re-checks authoritatively.
const CHAT_IMAGE_MAX_BYTES = 20 * 1024 * 1024;
const CHAT_FILE_MAX_BYTES = 100 * 1024 * 1024;
// Per-message ATTACHMENT COUNT cap — mirrors the backend's
// _CHAT_ATTACHMENTS_MAX_COUNT (a safety default, not a product decision). Over
// the cap → the extra files are refused with a visible notice; the ones that
// fit stay staged.
export const CHAT_MAX_ATTACHMENTS = 10;

/** `accept` for the file picker: images plus common office/doc/text/archive
 * types (an allow-anything wildcard is avoided — an explicit list is
 * friendlier on iOS, but we keep it broad). */
export const ATTACH_ACCEPT =
  "image/*,.pdf,.txt,.log,.csv,.json,.md,.zip,.doc,.docx,.xls,.xlsx,.ppt,.pptx";

/** ONE staged attachment held in a composer until the message is sent (or
 * cleared/removed). The clipboard-paste, attach-button and drag-drop paths all
 * funnel into this ONE shape; several may be staged at once (files + images
 * mixed) and are sent together on the SAME message. `dataUri` is a
 * `data:<mime>;base64,…` string (what FileReader.readAsDataURL yields), `size`
 * is the raw decoded byte estimate, `key` is a client-side list identity (for
 * React keys + per-item removal — duplicate filenames are legal). */
export interface PendingAttachment {
  key: string;
  dataUri: string;
  filename: string;
  mime: string;
  size: number;
  isImage: boolean;
}

// Monotonic client-side key mint for staged attachments.
let pendingAttachmentSeq = 0;

/** Estimate raw decoded byte size from a data-URI's base64 body. */
function estimateDataUriBytes(dataUri: string): number {
  const b64 = dataUri.split(",", 2)[1] ?? "";
  const padding = b64.endsWith("==") ? 2 : b64.endsWith("=") ? 1 : 0;
  return Math.floor((b64.length * 3) / 4) - padding;
}

/** Human-readable size for a staged file chip (e.g. "12 KB", "3.4 MB"). */
export function formatAttachmentSize(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`;
  if (bytes < 1024 * 1024) return `${Math.round(bytes / 1024)} KB`;
  return `${(bytes / (1024 * 1024)).toFixed(1)} MB`;
}

/** WHOSE staging this is — REQUIRED and FIRST, because the answer decides
 * whether a FileReader completion is still allowed to land in the composer it
 * was picked in (T-48, R9-1).
 *
 * 🔴 THIS HOOK HAS NO `await`, AND THAT IS EXACTLY WHY IT NEEDED A GUARD.
 * `stageFile` hands the file to `FileReader` and returns; the commit happens
 * later, inside `reader.onload`. Reading a 100 MB drop or a large pasted
 * screenshot takes SECONDS, and the surface that picked it can be gone by
 * then. In `ChatArea` "gone" does not mean unmounted — `OfficePage` mounts it
 * WITHOUT a `key`, so a conversation switch only swaps props and this hook's
 * `pendingAttachments` survives it. The measured result of having no guard:
 * a file picked in A appeared in B's composer, was persisted into B's DRAFT,
 * lit B's send button, and vanished from A. That is not a stale frame; it
 * hands a file to somebody who was never meant to receive it.
 *
 * Two shapes, because there are genuinely two kinds of caller:
 *
 * * `"remounts-per-conversation"` — the caller is mounted under a `key` that
 *   changes with the thing it belongs to (`TaskCard` under `key={task.id}`,
 *   `ReplyComposer` under `key={card.id}` / `key={m.id}`), so a switch
 *   UNMOUNTS it and this state dies with it. Nothing can cross. Say so out
 *   loud rather than passing nothing: a caller that later loses its key has to
 *   come back here and change this literal.
 * * `{ token, stashLate }` — the caller OUTLIVES the conversation. `token` is
 *   its visit token (the `useKeyedRecord` record, whose identity changes on
 *   every entry INCLUDING A→B→A, T-48 R6-1); `stashLate` is where a file that
 *   finished reading after that visit ended must go instead. */
export type AttachmentStagingVisit =
  | "remounts-per-conversation"
  | {
      token: object;
      /** A file whose read landed after its visit ended. Put it back in the
       * room it was picked for and return `true`; return `false` to hand it
       * back to the live composer (which is what the ONE caller does when the
       * later visit is to the SAME peer — the room on screen IS the file's
       * room, so staging it there is right).
       *
       * ⚠️ §3 rule 4's counter-example, second instance: the COMMIT must be
       * blocked, the SAVE must not. A bare `return` would silently destroy the
       * file — a pasted screenshot may exist nowhere else. */
      stashLate: (attachment: PendingAttachment) => boolean;
    };

export interface AttachmentStaging {
  pendingAttachments: PendingAttachment[];
  /** Transient rejection reason (too large / too many); null when none. */
  attachError: string | null;
  /** The ONE multi-file funnel: paste, picker and drag-drop all go through
   * here, one FileReader per file. */
  stageFiles: (files: File[]) => void;
  /** Paste handler: stage EVERY image/* item on the clipboard (a multi-image
   * paste stages them all). A paste with no image falls through untouched. */
  onPaste: (e: React.ClipboardEvent<HTMLTextAreaElement>) => void;
  /** Hidden-file-input onChange: stage every selected file, then clear the
   * input's value so picking the SAME file again still fires onChange. */
  onPickFile: (e: React.ChangeEvent<HTMLInputElement>) => void;
  removeAttachment: (key: string) => void;
  clearAttachments: () => void;
  /** Send-failure restore: put a snapshot back UNLESS the user already staged
   * new content while the send was in flight (never clobber fresh work). */
  restoreAttachments: (snapshot: PendingAttachment[]) => void;
}

export function useAttachmentStaging(
  visit: AttachmentStagingVisit,
): AttachmentStaging {
  const { t } = useI18n();
  // The visit mirror — same shape as `ChatArea`'s `visitRef` and
  // `useQuotedMessageOverlay`'s: written during render, read by the deferred
  // callback to ask "is the visit that picked this file still the one on
  // screen?". `null` for a keyed caller, which therefore never goes stale.
  const visitTokenRef = useRef<object | null>(null);
  visitTokenRef.current =
    visit === "remounts-per-conversation" ? null : visit.token;
  const [pendingAttachments, setPendingAttachments] = useState<
    PendingAttachment[]
  >([]);
  const [attachError, setAttachError] = useState<string | null>(null);

  // Read a File → data-URI, size-check (image ≤ 20 MB, other ≤ 100 MB, mirroring
  // the backend), and APPEND it to the staged attachments. Over-size → surface
  // an error, skip the file; over the COUNT cap → surface an error, drop the
  // overflow (the ones that fit stay). The count guard lives INSIDE the
  // functional setState because FileReader completions land asynchronously —
  // checking a stale snapshot would race a multi-file batch past the cap.
  function stageFile(file: File) {
    // Captured at PICK time, not at read time: both the visit this file was
    // picked in and the "put it back" callback belong to the room the owner was
    // looking at when they chose it.
    const firedFor = visitTokenRef.current;
    const stashLate =
      visit === "remounts-per-conversation" ? null : visit.stashLate;
    const reader = new FileReader();
    reader.onload = () => {
      const dataUri = typeof reader.result === "string" ? reader.result : "";
      if (!dataUri) return;
      const mime = file.type || "application/octet-stream";
      const isImage = mime.startsWith("image/");
      const size = estimateDataUriBytes(dataUri);
      const limit = isImage ? CHAT_IMAGE_MAX_BYTES : CHAT_FILE_MAX_BYTES;
      const stale = visitTokenRef.current !== firedFor;
      if (size > limit) {
        // A stale rejection is dropped rather than shown: 「檔案太大」 in a room
        // where nobody picked a file is a sentence about somebody else's
        // action. There is nowhere honest to put it — the draft store holds
        // attachments, not notices — so it is lost with the file it describes.
        if (stale) return;
        setAttachError(
          isImage
            ? t.chat.imageTooLarge
            : t.chat.attachTooLarge(Math.round(limit / (1024 * 1024)))
        );
        return;
      }
      const attachment: PendingAttachment = {
        key: `pa-${++pendingAttachmentSeq}`,
        // A pasted screenshot has no filename — leave it empty and let the
        // backend default it; a picked file keeps its real name.
        filename: file.name || "",
        dataUri,
        mime,
        size,
        isImage,
      };
      // 🔴 THE COMMIT GUARD (T-48, R9-1, §3 rule 4). This file was picked in a
      // visit that has ended: it must not be staged into whoever is on screen
      // now, must not reach that room's draft, and must not light that room's
      // send button. It is handed back to the room it was picked for instead —
      // blocking the commit is not a licence to destroy the file.
      if (stale && stashLate?.(attachment)) return;
      setPendingAttachments((prev) => {
        if (prev.length >= CHAT_MAX_ATTACHMENTS) {
          setAttachError(t.chat.attachTooMany(CHAT_MAX_ATTACHMENTS));
          return prev;
        }
        setAttachError(null);
        return [...prev, attachment];
      });
    };
    reader.readAsDataURL(file);
  }

  function stageFiles(files: File[]) {
    for (const file of files) stageFile(file);
  }

  function onPaste(e: React.ClipboardEvent<HTMLTextAreaElement>) {
    const files = Array.from(e.clipboardData.items)
      .filter((it) => it.type.startsWith("image/"))
      .map((it) => it.getAsFile())
      .filter((f): f is File => f !== null);
    if (files.length === 0) return; // no image → default text paste happens
    e.preventDefault();
    stageFiles(files);
  }

  function onPickFile(e: React.ChangeEvent<HTMLInputElement>) {
    const files = Array.from(e.target.files ?? []);
    e.target.value = "";
    if (files.length > 0) stageFiles(files);
  }

  function removeAttachment(key: string) {
    setPendingAttachments((prev) => prev.filter((a) => a.key !== key));
    setAttachError(null);
  }

  function clearAttachments() {
    setPendingAttachments([]);
    setAttachError(null);
  }

  function restoreAttachments(snapshot: PendingAttachment[]) {
    if (snapshot.length === 0) return;
    setPendingAttachments((cur) => (cur.length > 0 ? cur : snapshot));
  }

  return {
    pendingAttachments,
    attachError,
    stageFiles,
    onPaste,
    onPickFile,
    removeAttachment,
    clearAttachments,
    restoreAttachments,
  };
}
