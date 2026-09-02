// components/ChatGalleryPanel.tsx — the member's file & image gallery (M2-3,
// upgraded by Seth M2 acceptance batch 16). Opened from the chat header's
// gallery icon; collects EVERY attachment of the member's WHOLE conversation
// perspective — owner↔member BOTH directions AND the member's inter-agent
// threads (member↔other agent, both ways) — newest→oldest, split into an
// 「圖片」 and a 「檔案」 tab, each row labelled with its sender's display name
// + send time. Batch 18 adds an uploader filter chip row under the tabs —
// options derived from the ACTUAL rows' senders (never hardcoded), stacking
// with the image/file tab split.
//
// DATA SOURCE: the dedicated gallery query `listChatAttachments(memberId)`
// (`GET /api/chat/attachments?with=`) — the server flattens the rows and
// resolves each sender's display name from the roster (any status, so a
// dismissed sender still reads by name), so this component does no roster
// lookup and no client-side aggregation. READ-ONLY: opening the gallery never
// advances a read watermark — which since T-48 is true of every read door on
// this API, so this is no longer a contrast with the thread's own listing.
//
// OPEN BEHAVIOR (preview/download split, mirroring the server's disposition
// table on the server): a previewable mime
// (image/*, text/* — plain/markdown/html —, application/pdf) opens in a NEW TAB
// (the server serves those inline); anything else (zip and other opaque
// binaries) downloads (the server forces Content-Disposition: attachment).

import { useEffect, useRef, useState } from "react";
import { useI18n } from "../i18n";
import { useEscapeLayer } from "../lib/useEscapeLayer";
import type { Member } from "../types";
import type { GalleryAttachment, SseDelta } from "../api/adapter";
import { api } from "../api";
import { createDeltaSink } from "../lib/deltaSink";
import { authedAttachmentUrl } from "../api/http";
import { CloseIcon, FileTextIcon } from "./icons";
import { MarkdownPreviewOverlay } from "./MarkdownPreviewOverlay";

// The owner's sender id — the real backend stamps `from` from the verified JWT
// sub ("owner"); same constant as ChatArea's OWNER_ID (kept local to avoid an
// import cycle: ChatArea imports this component).
const OWNER_ID = "owner";

/** FE mirror of the server's preview/download split
 * (the server previewable-mime table): previewable mimes are served
 * inline → open in a new tab; the rest are forced downloads. */
export function isPreviewableMime(mime: string): boolean {
  return (
    mime.startsWith("image/") ||
    mime.startsWith("text/") ||
    mime === "application/pdf"
  );
}

/**
 * Could THIS ONE chat delta change what the gallery renders?
 *
 * 🔴 THE INVARIANT IT ENCODES. The gallery query
 * (`HandleListChatAttachmentsApiChatAttachmentsGet`, `server/ocserverd/api_chat.go`)
 * keeps a message when — and only when — `m.Sender == with || m.Recipient == with`,
 * where `with` is the member id this panel was opened on. Every row it returns is
 * an attachment of one of those messages. ⇒ a CHAT DELTA naming this member at
 * NEITHER end cannot add, remove or re-order a single row here, and refetching
 * for it buys not a smaller answer but the SAME answer. That is the ordinary
 * case in this product: every agent↔agent line in the whole company used to cost
 * this open panel one `GET /api/chat/attachments`.
 *
 * ⚠️ THIS IS NOT THE OWNER PREDICATE — do not reach for `lib/ownerUnread.ts`.
 * That one asks `to === "owner"` because `UnreadCounts` counts only
 * `m.Recipient == reader` and the cockpit's reader is always the owner. This
 * endpoint is a DIFFERENT fold: BOTH ends count, and the end that matters is
 * THIS MEMBER, not the owner. An agent↔agent line moves no owner unread number
 * yet absolutely does change this gallery when one of those agents IS this
 * member — applying the owner predicate here would SKIP REAL WORK and leave the
 * panel stale.
 *
 * ⚠️ Attachment-awareness would be tighter still ("a message with no files
 * changes nothing here"), but `SseDelta` carries identity only — there is no
 * way to tell, so we do not guess.
 *
 * ⚠️ SCOPE OF THE CLAIM: this is about CHAT deltas only. The handler also
 * resolves each sender's display name from `ListMembers()`, so a RENAME changes
 * what it answers — and that arrives on the `member` topic, which this panel
 * subscribes to neither before nor after this change. Unchanged behaviour, not
 * something this predicate covers; do not read the paragraph above as "nothing
 * else can ever change this view".
 */
export function chatDeltaTouchesMember(d: SseDelta, memberId: string): boolean {
  if (d.topic !== "chat") return false;
  return d.names.from === memberId || d.names.to === memberId;
}

/** The two gallery tabs: images vs every other file kind. */
type GalleryTab = "images" | "files";

/** Uploader filter sentinel: 「全部」 = no sender filtering. A real sender id
 * is never empty (the backend stamps `from` from the verified JWT sub). */
const ALL_SENDERS = "";

/** Format an epoch-second ts as a local "M/D hh:mm" — gallery history spans
 * days, so the bare hh:mm of the thread is not enough. Never fabricated. */
function formatDateTime(ts: number): string {
  return new Date(ts * 1000).toLocaleString([], {
    month: "numeric",
    day: "numeric",
    hour: "2-digit",
    minute: "2-digit",
  });
}

export function ChatGalleryPanel({
  member,
  resolveSender,
  onClose,
}: {
  member: Member;
  // ChatArea's nameOf: resolves an id the server left unnamed (an outsource
  // sender, whose from_name is "") to the SAME codename label the thread
  // bubbles show. Why from_name is blank: the gallery handler builds its names
  // table from `dal.ListMembers` (api_chat.go), which is `WHERE kind !=
  // 'outsource'` — so NO outsource sender is ever named there, live or
  // released. That, not the GET /api/members roster, is the reason this
  // resolver exists. Optional — absent keeps the raw-id fallback.
  resolveSender?: (id: string) => string;
  onClose: () => void;
}) {
  const { t } = useI18n();
  const [entries, setEntries] = useState<GalleryAttachment[]>([]);
  const [tab, setTab] = useState<GalleryTab>("images");
  // Uploader filter (batch 18): the selected sender id, ALL_SENDERS = 「全部」.
  const [sender, setSender] = useState<string>(ALL_SENDERS);
  // Honest empty state: 「還沒有…」 only AFTER the fetch settles — never
  // flash it while loading.
  const [loaded, setLoaded] = useState(false);
  const [preview, setPreview] = useState<GalleryAttachment | null>(null);

  useEffect(() => {
    let alive = true;
    const refetch = () => {
      // The server-flattened member gallery: every conversation the member is
      // in (owner↔member + inter-agent), sender-labelled, newest→oldest.
      api
        .listChatAttachments(member.id)
        .then((rows) => {
          if (!alive) return;
          setEntries(rows);
          setLoaded(true);
          // If the selected uploader vanished from the fresh rows (e.g. after
          // a member switch), fall back to 「全部」 — never a stuck-blank filter.
          setSender((cur) =>
            cur !== ALL_SENDERS && !rows.some((r) => r.from === cur)
              ? ALL_SENDERS
              : cur,
          );
        })
        .catch((e) => console.warn("ChatGalleryPanel: load failed", e));
    };
    refetch();
    // Keep the open panel live: a new message may carry new attachments — but
    // ONLY a CHAT DELTA naming this member can change what the server answers
    // for it (a rename does too, but that is the `member` topic — see the SCOPE
    // note there, and note this panel subscribes to neither before nor after).
    // See
    // `chatDeltaTouchesMember` above.
    const unsubscribe = api.subscribeEvents(
      createDeltaSink((batch) => {
        if (!batch.topics.has("chat")) return;
        // Named NOTHING (a resync, a null payload, or a transport that supplies
        // no delta at all) is the honest "you may have missed anything" — never
        // reason about names there, just re-pull.
        if (batch.unnamed) {
          refetch();
          return;
        }
        // Whole-burst, not per-delta: one refetch answers "what is the gallery
        // now", so a mixed burst (one unrelated line AND one of ours, same
        // microtask) still refetches exactly once.
        if (batch.deltas.some((d) => chatDeltaTouchesMember(d, member.id))) {
          refetch();
        }
      }),
    );
    return () => {
      alive = false;
      unsubscribe();
    };
  }, [member.id]);

  // Esc closes the panel — while it is the TOP layer. The preview overlay it
  // renders registers above it, so an open preview takes the first Esc and the
  // gallery is not asked to guess whether one is up.
  const rootRef = useRef<HTMLDivElement>(null);
  useEscapeLayer(onClose, rootRef);

  // Sender label: the owner reads as 「我」; everyone else by the SERVER-resolved
  // display name (fromName). A sender the server left unnamed (an outsource
  // worker — never in the members roster) resolves through the caller's
  // resolveSender (codename chain), then falls back to its id — mirrors the
  // thread's roster fallback, never fabricated.
  const senderLabel = (e: GalleryAttachment): string =>
    e.from === OWNER_ID
      ? t.chat.me
      : e.fromName || resolveSender?.(e.from) || e.from;

  // Uploader filter options — derived from the ACTUAL rows' senders (never
  // hardcoded), deduped in row order (rows are newest→oldest), labelled with
  // the same senderLabel the list rows use (owner → 「我」, others → fromName,
  // fallback id — the raw internal id never renders when a name exists).
  const senders: { id: string; label: string }[] = [];
  for (const e of entries) {
    if (!senders.some((s) => s.id === e.from)) {
      senders.push({ id: e.from, label: senderLabel(e) });
    }
  }

  // The two dimensions STACK: the 圖片/檔案 tab split (same server-derived
  // isImage flag the thread bubbles use) AND the uploader filter.
  const shown = entries.filter(
    (e) =>
      (tab === "images" ? e.isImage : !e.isImage) &&
      (sender === ALL_SENDERS || e.from === sender),
  );

  return (
    <div
      ref={rootRef}
      className="chat__gallery"
      role="dialog"
      aria-label={t.chat.galleryLabel}
    >
      <div className="chat__gallery-header">
        <span className="chat__gallery-title">{t.chat.galleryLabel}</span>
        <button
          type="button"
          className="chat__gallery-close"
          aria-label={t.chat.galleryClose}
          onClick={onClose}
        >
          <CloseIcon size={16} />
        </button>
      </div>
      {/* 圖片 / 檔案 segmented tabs — same seg pattern as the preferences
       * switches (profile-dd__seg), muted by default, active gets the card
       * highlight. */}
      <div className="chat__gallery-tabs" role="tablist">
        <button
          type="button"
          role="tab"
          aria-selected={tab === "images"}
          className={`chat__gallery-tab${
            tab === "images" ? " chat__gallery-tab--active" : ""
          }`}
          onClick={() => setTab("images")}
        >
          {t.chat.galleryTabImages}
        </button>
        <button
          type="button"
          role="tab"
          aria-selected={tab === "files"}
          className={`chat__gallery-tab${
            tab === "files" ? " chat__gallery-tab--active" : ""
          }`}
          onClick={() => setTab("files")}
        >
          {t.chat.galleryTabFiles}
        </button>
      </div>
      {/* Uploader filter chips (batch 18) — grey chips under the tabs, same
       * muted seg vocabulary; only rendered once loaded (never flash while
       * loading) and only when there is something to filter. Stacks with the
       * tab split above. */}
      {loaded && senders.length > 0 && (
        <div
          className="chat__gallery-senders"
          role="group"
          aria-label={t.chat.gallerySenderFilterLabel}
        >
          <button
            type="button"
            aria-pressed={sender === ALL_SENDERS}
            className={`chat__gallery-sender-chip${
              sender === ALL_SENDERS ? " chat__gallery-sender-chip--active" : ""
            }`}
            onClick={() => setSender(ALL_SENDERS)}
          >
            {t.chat.gallerySenderAll}
          </button>
          {senders.map((s) => (
            <button
              key={s.id}
              type="button"
              aria-pressed={sender === s.id}
              className={`chat__gallery-sender-chip${
                sender === s.id ? " chat__gallery-sender-chip--active" : ""
              }`}
              onClick={() => setSender(s.id)}
            >
              {s.label}
            </button>
          ))}
        </div>
      )}
      {!loaded ? null : shown.length === 0 ? (
        <div className="chat__gallery-empty">
          {tab === "images" ? t.chat.galleryEmptyImages : t.chat.galleryEmptyFiles}
        </div>
      ) : (
        <div className="chat__gallery-list">
          {shown.map((e) => {
            const href = authedAttachmentUrl(e.url);
            return (
              <div
                key={`${e.messageId}-${e.id}`}
                className="chat__gallery-item"
                role="button"
                tabIndex={0}
                title={t.chat.galleryPreviewHint}
                onClick={() => setPreview(e)}
                onKeyDown={(event) => {
                  if (event.key === "Enter" || event.key === " ") {
                    event.preventDefault();
                    setPreview(e);
                  }
                }}
              >
                {e.isImage ? (
                  <img
                    className="chat__gallery-thumb"
                    src={href}
                    alt={e.filename || t.chat.imageAlt}
                  />
                ) : (
                  <span className="chat__gallery-fileicon" aria-hidden>
                    <FileTextIcon size={20} />
                  </span>
                )}
                <div className="chat__gallery-meta">
                  <span className="chat__gallery-name">
                    {e.filename || t.chat.downloadAttachment}
                  </span>
                  <span className="chat__gallery-sub">
                    {senderLabel(e)} · {formatDateTime(e.ts)}
                  </span>
                </div>
              </div>
            );
          })}
        </div>
      )}
      {preview && <MarkdownPreviewOverlay title={preview.filename || t.chat.downloadAttachment} url={preview.url} attachmentId={preview.id} mime={preview.mime} onClose={() => setPreview(null)} />}
    </div>
  );
}
