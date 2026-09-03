// lib/chatDraftStore.ts — the composer DRAFT layer for the office chat, and
// since T-48 (R13-2) the one and only home of a chat composer's staged files.
//
// The bug (T-8aaa): the座艙 chat composer's draft (typed text AND staged image
// attachments) lived ONLY in ChatArea's component state. Navigating to another
// page unmounts OfficePage (and with it the single ChatArea instance), so the
// draft was dropped; coming back showed an empty composer.
//
// The layer: a per-peer, module-level in-memory store. Module state outlives a
// component unmount/remount, so a 跳頁-then-return restores the draft. It is
// deliberately NOT persisted to localStorage:
//   • image attachments are `data:…;base64,…` URIs — potentially MBs each;
//     serializing every keystroke into the ~5 MB localStorage quota is a real
//     cost and a real eviction risk, for a payload that is already sitting in JS
//     memory anyway.
//   • the owner's reported scenario is "跳到別頁再回來" (an in-app SPA
//     navigation), which never tears down the module — only a full page reload
//     does, and losing an in-progress draft across a hard reload is acceptable
//     (documented tradeoff).
//
// Keyed by CHAT PEER id (member id / outsource worker id), matching how chat
// history and the compose seed are keyed — so each conversation carries its own
// independent draft.
//
// 🔴 IT IS SUBSCRIBABLE, AND THAT IS WHAT RETIRED THE PARALLEL REGISTRY (T-48,
// R13-2). This file used to be read once per mount (`useState(() =>
// getChatDraft(id))`), which made it a place a file could WAIT but never a
// place a file could ARRIVE. So a second module-level, peer-keyed table grew up
// beside it — `liveComposers` in ChatArea — whose whole job was to tell a
// mounted composer that a late `FileReader` had filed something into the draft
// it had already read. The comment there said "this is the half chatDraftStore
// cannot be". It could: it was not that the store was the wrong shape, it was
// that nobody could subscribe to it.
//
// With `subscribeChatDraft` the composer's staged list IS this store's per-peer
// slice, read through `useSyncExternalStore`. A late landing is one write here
// and every composer showing that room repaints — mounted or not, first visit or
// tenth. There is no registry, no adoption, no restore, and no "was this row
// picked for the room on screen?" stamp on the row, because a row cannot be in
// a room it was not written into.

import type { PendingAttachment } from "../hooks/useAttachmentStaging";

/** A saved composer draft for one peer: the text plus the staged attachments
 * (held as the same fully-serializable `PendingAttachment` the composer uses). */
export interface ChatDraft {
  text: string;
  attachments: PendingAttachment[];
  /** The id of the message this draft is REPLYING TO, when the composer is in
   * reply mode. Part of the draft because it is part of what the owner has
   * composed but not yet sent: leaving 跳頁-and-back with the text restored but
   * the reply target silently dropped would send the message to the wrong
   * place while looking exactly like a normal restore. */
  replyTo?: string;
}

const drafts = new Map<string, ChatDraft>();
/** The transient staging rejection (「圖片太大」/「最多 N 個檔案」) for one peer.
 * Per peer for the same reason the file is: a size refusal is a sentence about
 * something somebody did in ONE room, and it has to survive until that room is
 * on screen to read it (R11-4 / R12-1). Kept out of `ChatDraft` because it is
 * not part of what the owner composed — an empty composer with a notice is
 * still an empty draft.
 *
 * 🔴 IT IS PER PEER, BUT IT IS NOT PER SESSION (T-48, R14-2.1). Being a
 * module-level table means it outlives an unmount, and that is right for the
 * one thing it was moved here for — a refusal raised by a read that finished
 * while the owner was in another room has to be readable when they come back.
 * It is NOT right across the whole app: before this table existed the notice
 * was component state on the composer, so leaving the office page took it with
 * it, and the fourteenth review measured the difference — refuse a 30 MB image,
 * walk to 任務, come back ten minutes later, and the red sentence is still
 * there describing something the owner did ten minutes ago. The draft survives
 * that navigation on purpose; a transient rejection does not. `OfficePage`
 * drops these on unmount (`clearChatAttachErrors`), which is what puts the
 * notice back on the lifetime it had. */
const attachErrors = new Map<string, string>();
const listeners = new Map<string, Set<() => void>>();

/** The snapshot handed back for a peer with no staged files. One frozen
 * instance, because `useSyncExternalStore` re-renders whenever the snapshot's
 * identity changes and a fresh `[]` per read is an infinite loop. */
const NO_ATTACHMENTS: readonly PendingAttachment[] = Object.freeze([]);

function notify(peerId: string): void {
  const subs = listeners.get(peerId);
  if (!subs) return;
  for (const cb of [...subs]) cb();
}

/** Watch ONE peer's draft slice. Returns the unsubscribe. */
export function subscribeChatDraft(peerId: string, cb: () => void): () => void {
  let subs = listeners.get(peerId);
  if (!subs) {
    subs = new Set();
    listeners.set(peerId, subs);
  }
  subs.add(cb);
  return () => {
    const live = listeners.get(peerId);
    if (!live) return;
    live.delete(cb);
    if (live.size === 0) listeners.delete(peerId);
  };
}

/** The saved draft for a peer, or undefined when none (never typed, no staged
 * file, no reply target). */
export function getChatDraft(peerId: string): ChatDraft | undefined {
  return drafts.get(peerId);
}

/** A peer's staged files — the composer's live list, not a copy of it.
 * Referentially stable between writes, which is what makes it a legal
 * `useSyncExternalStore` snapshot. */
export function getChatDraftAttachments(peerId: string): PendingAttachment[] {
  return (drafts.get(peerId)?.attachments ??
    NO_ATTACHMENTS) as PendingAttachment[];
}

/** A peer's staging rejection notice, or null. */
export function getChatAttachError(peerId: string): string | null {
  return attachErrors.get(peerId) ?? null;
}

function write(peerId: string, next: ChatDraft): void {
  // "Empty" includes the reply target: a composer holding ONLY a reply target
  // (no text, no attachments) is still a composer the owner has put into a
  // state, and dropping it on 跳頁 would silently cancel the reply. An empty
  // draft is DELETED rather than stored blank — the "送出 / 手動清空後歸零"
  // path, so a later return finds nothing to restore (and the compose seed is
  // free to inject into the genuinely-empty composer).
  if (
    next.text.length === 0 &&
    next.attachments.length === 0 &&
    !next.replyTo
  ) {
    if (drafts.delete(peerId)) notify(peerId);
    return;
  }
  drafts.set(peerId, next);
  notify(peerId);
}

/** Persist the TYPED half only, leaving the staged files exactly as they are.
 *
 * 🔴 THIS SPLIT IS R11-2's FIX MADE STRUCTURAL (T-48, R13-2). The composer used
 * to persist text and attachments together, from its own component state — so a
 * file filed into the draft by a late read was overwritten by the next
 * keystroke, which saved the composer's own (file-less) list over the top. The
 * files are no longer the composer's to write back: this function cannot touch them. */
export function saveChatDraftText(
  peerId: string,
  text: string,
  replyTo?: string,
): void {
  const cur = drafts.get(peerId);
  write(peerId, { text, attachments: cur?.attachments ?? [], replyTo });
}

/** Functionally update a peer's staged files. The updater sees the room's own
 * list and nobody else's, which is the whole reason the count cap and the dedup
 * can be written without asking whose row is whose. */
export function updateChatDraftAttachments(
  peerId: string,
  fn: (prev: PendingAttachment[]) => PendingAttachment[],
): void {
  const cur = drafts.get(peerId);
  const prev = cur?.attachments ?? [];
  const next = fn(prev);
  if (next === prev) return;
  write(peerId, {
    text: cur?.text ?? "",
    attachments: next,
    replyTo: cur?.replyTo,
  });
}

/** Raise or clear a peer's staging rejection notice. */
export function setChatAttachError(
  peerId: string,
  message: string | null,
): void {
  const had = attachErrors.has(peerId);
  if (message === null) {
    if (!had) return;
    attachErrors.delete(peerId);
  } else {
    if (attachErrors.get(peerId) === message) return;
    attachErrors.set(peerId, message);
  }
  notify(peerId);
}

/** Drop EVERY room's staging rejection notice — the chat surface itself is
 * going away. Called from `OfficePage`'s unmount, so the notice's lifetime is
 * the page's, the way it was when it was component state (T-48, R14-2.1). The
 * drafts and their staged files are deliberately untouched: those survive the
 * navigation because that is what a draft is for. */
export function clearChatAttachErrors(): void {
  if (attachErrors.size === 0) return;
  const touched = [...attachErrors.keys()];
  attachErrors.clear();
  for (const peerId of touched) notify(peerId);
}

/** Test-only reset so a module-level store never leaks state across tests. */
export function resetChatDrafts(): void {
  const touched = new Set([...drafts.keys(), ...attachErrors.keys()]);
  drafts.clear();
  attachErrors.clear();
  for (const peerId of touched) notify(peerId);
}
