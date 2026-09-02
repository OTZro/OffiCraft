// hooks/useAttachmentStaging.ts — the ONE composer attachment-staging state
// machine, extracted from ChatArea so every reply surface (chat composer, the
// 等我回覆 reply cards, B3's inline chat card) stages uploads identically:
// same size/count caps, same paste/pick funnels, same preview shape.

import { useEffect, useMemo, useRef, useState } from "react";
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
  /** 🔴 WHOSE FILE THIS IS — the conversation/surface it was picked for, stamped
   * at PICK time (T-48, owner ruling). The composer renders only the staged
   * files whose `target` is the room on screen, so a read that lands seconds
   * later, in another room, has nothing to render into: the leak R9-1 described
   * is not blocked by a line somebody remembered to write, it has no shape to
   * happen in. Same shape as `useChat`'s `messagesPeer` — the value carries its
   * owner, and answering "whose is this?" is the only way to use it. */
  target: string;
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

/** WHOSE staging this is — REQUIRED and FIRST, because every file staged here
 * is STAMPED with it and the composer shows only the files stamped with the
 * target on screen (T-48, owner ruling on R9-1).
 *
 * 🔴 THIS HOOK HAS NO `await`, AND THAT IS WHY THE ANSWER HAD TO MOVE INTO THE
 * DATA. `stageFile` hands the file to `FileReader` and returns; the commit
 * happens later, inside `reader.onload`. Reading a 100 MB drop or a large
 * pasted screenshot takes SECONDS, and the surface that picked it can be gone
 * by then. In `ChatArea` "gone" does not mean unmounted — `OfficePage` mounts
 * it WITHOUT a `key`, so a conversation switch only swaps props and this
 * hook's state survives it. The measured result of the original shape: a file
 * picked in A appeared in B's composer, was persisted into B's DRAFT, lit B's
 * send button, and vanished from A.
 *
 * The first fix was a commit guard on a visit token — a line the writer had to
 * remember. This is the second: the attachment says which room it is for, and
 * the reader (`pendingAttachments`, and therefore the previews, the send
 * button, and the draft the composer persists) cannot see a file belonging to
 * anybody else. There is no guard to forget because there is no unfiltered
 * value to reach.
 *
 * A caller mounted under a key that changes with the thing it belongs to
 * (`TaskCard` under `key={task.id}`, `ReplyComposer` under `key={card.id}`)
 * has exactly one target for its whole life and passes this constant, which
 * says so out loud. Both of those call sites import it — the sentence above
 * was written while they still spelled the literal out, and R11-8 caught the
 * claim before the code caught up. */
export const STAGING_TARGET_PER_MOUNT = "remounts-per-conversation";

/** Where a file that landed for a room OTHER than the one on screen is handed
 * for safe-keeping. The composer already cannot show it; this is about not
 * LOSING it — the staged list is wiped on the next conversation switch, so a
 * file with nowhere durable to live would be destroyed by the switch after
 * next. `ChatArea` writes it into that room's own draft, which is what the
 * composer restores from on the next entry.
 *
 * ⚠️ §3 rule 4's counter-example, second instance: the COMMIT must be blocked,
 * the SAVE must not. Omitting this callback is legal for a per-mount caller,
 * which cannot produce a foreign landing while it is alive — but it is NOT
 * free (R11-8). On a CONVERSATION SWITCH the file is merely KEPT in state,
 * invisible; on an UNMOUNT there is no state left to keep it in and the file
 * is dropped, silently. `TaskCard` and `ReplyComposer` both accept that: they
 * are torn down with the thing they stage for, so a landing after their
 * unmount has no surface left to return to either. */
export type KeepElsewhere = (
  target: string,
  attachments: PendingAttachment[],
) => void;

export interface AttachmentStaging {
  /** The staged files FOR THE TARGET ON SCREEN, and nothing else. */
  pendingAttachments: PendingAttachment[];
  /** Transient rejection reason (too large / too many) raised in THIS target;
   * null when none, and null while the reason belongs to another room. */
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
  /** Clear THIS target's staged files (and the visible error). Another room's
   * pending landing is not this room's to throw away. */
  clearAttachments: () => void;
  /** A file that finished reading while NO composer was mounted for its room,
   * arriving at the composer that is showing that room now (T-48, R11-2).
   * APPENDED, never replacing, and deduped by `key` — the mount-time draft
   * restore may already hold the same row. The rows keep the target they were
   * stamped with at pick time; if the owner has moved on again by the time
   * this lands, they are simply invisible and the handoff effect above files
   * them back into their own draft. */
  adoptAttachments: (arriving: PendingAttachment[]) => void;
  /** Send-failure restore: put a snapshot back UNLESS the user already staged
   * new content while the send was in flight (never clobber fresh work). The
   * snapshot is re-stamped with the CURRENT target — it comes out of that
   * room's own draft, so that room is whose it is. */
  restoreAttachments: (snapshot: PendingAttachment[]) => void;
}

/** 🔴 THE TYPE ASKS THE QUESTION BACK (T-48, R11-8). `target: string` alone
 * compiles for every caller and every mistake: pass a task id where a peer id
 * belongs and the files cross rooms, which is this family's whole shape. The
 * two overloads put the choice back where the caller must answer it —
 *
 *   · a surface that is REMOUNTED with the thing it stages for says exactly
 *     that, by passing `STAGING_TARGET_PER_MOUNT` and nothing else;
 *   · a surface that OUTLIVES the thing it stages for (a `ChatArea` that is
 *     swapped between peers without remounting) must name its target AND say
 *     where a file that lands too late is to be kept.
 *
 * Forgetting `keepElsewhere` on the second kind is now a compile error rather
 * than a file destroyed at run time by an unmount nobody was watching for. */
export function useAttachmentStaging(
  target: typeof STAGING_TARGET_PER_MOUNT,
): AttachmentStaging;
export function useAttachmentStaging(
  target: string,
  keepElsewhere: KeepElsewhere,
): AttachmentStaging;
export function useAttachmentStaging(
  target: string,
  keepElsewhere?: KeepElsewhere,
): AttachmentStaging {
  const { t } = useI18n();
  // The live target, read by the deferred `FileReader` callback so a file is
  // stamped with the room the owner was looking at when they PICKED it.
  const targetRef = useRef(target);
  targetRef.current = target;
  // Mirrored because the unmount handoff below reads it from a callback that
  // outlives the render it was created in.
  const keepElsewhereRef = useRef(keepElsewhere);
  keepElsewhereRef.current = keepElsewhere;
  // EVERY staged file, of every target. Nothing outside this hook sees it:
  // what is exposed is the slice belonging to the target on screen.
  const [staged, setStaged] = useState<PendingAttachment[]>([]);
  // The rejection notice carries its room for the same reason the file does —
  // 「檔案太大」 in a room where nobody picked a file is a sentence about
  // somebody else's action.
  const [attachError, setAttachError] = useState<{
    target: string;
    message: string;
  } | null>(null);

  // 🔴 THE PAGE ITSELF CAN BE THE ROOM THE OWNER LEFT (T-48, R10-4). An unmount
  // does not change the target — `OfficePage` tearing `ChatArea` down leaves
  // `member` exactly as it was — so nothing about the file's identity says
  // "there is no composer to land in any more". React swallows the resulting
  // `setState` silently and the effect below never runs again, so the file was
  // destroyed by nobody's decision. This mirror is the one thing the DATA
  // cannot say about itself, and it is read at exactly one place: the commit.
  const mountedRef = useRef(true);
  useEffect(() => {
    mountedRef.current = true;
    return () => {
      mountedRef.current = false;
    };
  }, []);

  const pendingAttachments = useMemo(
    () => staged.filter((a) => a.target === target),
    [staged, target],
  );

  // A file that finished reading for a room that is NOT on screen: it is
  // already invisible (the filter above), and this hands it to that room's own
  // storage before the next conversation switch wipes the staged list. Driven
  // entirely by what the attachments SAY they are for — there is no "was the
  // visit stale?" question here, and deliberately so: the answer is in the row.
  useEffect(() => {
    const foreign = staged.filter((a) => a.target !== target);
    if (foreign.length === 0 || !keepElsewhere) return;
    const byTarget = new Map<string, PendingAttachment[]>();
    for (const a of foreign) {
      const list = byTarget.get(a.target);
      if (list) list.push(a);
      else byTarget.set(a.target, [a]);
    }
    for (const [owner, list] of byTarget) keepElsewhere(owner, list);
    setStaged((prev) => prev.filter((a) => a.target === target));
  }, [staged, target, keepElsewhere]);

  // Read a File → data-URI, size-check (image ≤ 20 MB, other ≤ 100 MB, mirroring
  // the backend), and APPEND it to the staged attachments. Over-size → surface
  // an error, skip the file; over the COUNT cap → surface an error, drop the
  // overflow (the ones that fit stay). The count guard lives INSIDE the
  // functional setState because FileReader completions land asynchronously —
  // checking a stale snapshot would race a multi-file batch past the cap, and
  // it counts only the rows belonging to the SAME target (the cap is
  // per-message, and two rooms do not share a message).
  function stageFile(file: File) {
    // Captured at PICK time, not at read time: this is the room the owner was
    // looking at when they chose the file, and it rides the row from here on.
    const pickedFor = targetRef.current;
    const reader = new FileReader();
    reader.onload = () => {
      const dataUri = typeof reader.result === "string" ? reader.result : "";
      if (!dataUri) return;
      const mime = file.type || "application/octet-stream";
      const isImage = mime.startsWith("image/");
      const size = estimateDataUriBytes(dataUri);
      const limit = isImage ? CHAT_IMAGE_MAX_BYTES : CHAT_FILE_MAX_BYTES;
      if (size > limit) {
        setAttachError({
          target: pickedFor,
          message: isImage
            ? t.chat.imageTooLarge
            : t.chat.attachTooLarge(Math.round(limit / (1024 * 1024))),
        });
        return;
      }
      const attachment: PendingAttachment = {
        key: `pa-${++pendingAttachmentSeq}`,
        target: pickedFor,
        // A pasted screenshot has no filename — leave it empty and let the
        // backend default it; a picked file keeps its real name.
        filename: file.name || "",
        dataUri,
        mime,
        size,
        isImage,
      };
      // Nothing is mounted to hold this any more — hand it straight to its own
      // room's storage rather than into a setState React will drop (R10-4).
      if (!mountedRef.current) {
        keepElsewhereRef.current?.(pickedFor, [attachment]);
        return;
      }
      setStaged((prev) => {
        if (
          prev.filter((a) => a.target === pickedFor).length >=
          CHAT_MAX_ATTACHMENTS
        ) {
          setAttachError({
            target: pickedFor,
            message: t.chat.attachTooMany(CHAT_MAX_ATTACHMENTS),
          });
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
    setStaged((prev) => prev.filter((a) => a.key !== key));
    setAttachError(null);
  }

  function clearAttachments() {
    setStaged((prev) => prev.filter((a) => a.target !== targetRef.current));
    // 🔴 THIS ROOM'S NOTICE, THE SAME WAY IT CLEARS THIS ROOM'S FILES (T-48,
    // R11-4). This used to be an unconditional `setAttachError(null)`, and the
    // conversation-switch block in `ChatArea` calls it on every switch — so a
    // notice stamped for the room the owner had LEFT was wiped before that
    // room could ever show it. Stamping the message with its room only means
    // something if the message survives long enough to reach it.
    setAttachError((prev) =>
      prev && prev.target !== targetRef.current ? prev : null,
    );
  }

  function adoptAttachments(arriving: PendingAttachment[]) {
    setStaged((prev) => {
      const fresh = arriving.filter((a) => !prev.some((p) => p.key === a.key));
      return fresh.length === 0 ? prev : [...prev, ...fresh];
    });
  }

  function restoreAttachments(snapshot: PendingAttachment[]) {
    if (snapshot.length === 0) return;
    const owner = targetRef.current;
    setStaged((prev) =>
      prev.some((a) => a.target === owner)
        ? prev
        : [...prev, ...snapshot.map((a) => ({ ...a, target: owner }))],
    );
  }

  return {
    pendingAttachments,
    attachError:
      attachError && attachError.target === target ? attachError.message : null,
    stageFiles,
    onPaste,
    onPickFile,
    removeAttachment,
    clearAttachments,
    adoptAttachments,
    restoreAttachments,
  };
}
