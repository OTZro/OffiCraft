// components/MarkdownPreviewOverlay.tsx — the in-cockpit .md preview overlay
// (T-a1c4). A markdown attachment is PREVIEWED here — fetched as text and
// rendered through the shared Markdown.tsx (React elements, XSS-safe) — instead
// of the browser's raw-source new tab. Preview and download are TWO separate
// actions: the header keeps a 下載 button (the same authed blob URL with a
// download attribute) alongside the render.
//
// Self-contained like Lightbox (click backdrop / × / Esc closes; a click on the
// panel does not dismiss): the caller holds the open state and passes the blob's
// serve url + display title. Shared by the chat attachment strip AND the task
// artifact popover — one preview surface, not two.
//
// THREE SOURCE MODES (one surface, still not three):
//   - `url`      — a stored blob, fetched as text (or rendered as an image when
//                  its mime says so). It carries a REQUIRED `attachmentId`, so
//                  the header keeps both blob actions: the copyable share link
//                  and the 下載 link.
//   - `source`   — text the caller ALREADY holds (a chat message body). Nothing
//                  to fetch, nothing to download and nothing to share: the bytes
//                  never were a file, so both links are absent rather than
//                  pointing at a fabricated blob url. Single newlines are HARD
//                  breaks here, matching the chat bubble the text came from.
//   - `imageSrc` — image bytes the caller ALREADY holds as a `data:` URI: a
//                  STAGED composer attachment that has not been sent yet
//                  (T-f014, retiring the separate Lightbox overlay). It is a
//                  real file, so 下載 is honest — but it has no blob id yet, so
//                  there is nothing for a share link to point at and none is
//                  rendered.
//
// T-f014 — this is now the ONLY full-size image surface in the cockpit. The old
// Lightbox overlay and its stylesheet block are gone; every image (stored or staged
// composer preview) opens here and therefore gets the same shell: filename in
// the header, 下載, close, Esc/backdrop dismissal and the zoom controls.

import { useEffect, useRef, useState } from "react";
import { useI18n } from "../i18n";
import { authedAttachmentUrl } from "../api/http";
import { copyAttachmentShareLink } from "../lib/shareLink";
import { useEscapeLayer } from "../lib/useEscapeLayer";
import { Markdown } from "./Markdown";
import "./md-preview.css";
import {
  CheckIcon,
  CloseIcon,
  CopyIcon,
  DownloadIcon,
  FileTextIcon,
} from "./icons";

type MarkdownPreviewOverlayProps = {
  /** Display name shown in the header (the blob's filename, or the sender of
   * the message being read). */
  title: string;
  onClose: () => void;
} & (
  | {
      /** The blob's serve path (`/api/chat/attachment/<id>`); fetched as text. */
      url: string;
      /** The blob's own id — the subject of the copyable share link. REQUIRED
       * with `url`: a stored blob always has one, and the union is what stops a
       * caller from opening a file view with no way to share it. */
      attachmentId: string;
      /** The stored blob kind decides the body renderer. Omitted keeps the
       * original markdown-file contract for existing callers. */
      mime?: string;
      source?: never;
      imageSrc?: never;
    }
  | {
      /** Markdown text the caller already holds — rendered as-is, no fetch. */
      source: string;
      url?: never;
      /** No blob, so no share link: there is nothing for a link to point at. */
      attachmentId?: never;
      mime?: never;
      imageSrc?: never;
    }
  | {
      /** Image bytes the caller already holds (`data:` URI) — a staged composer
       * attachment. Downloadable (the bytes ARE the file) but not shareable:
       * nothing has been uploaded, so no blob id exists to mint a link from. */
      imageSrc: string;
      url?: never;
      attachmentId?: never;
      mime?: never;
      source?: never;
    }
);

export function MarkdownPreviewOverlay({
  title,
  url,
  attachmentId,
  mime,
  imageSrc,
  source: inlineSource,
  onClose,
}: MarkdownPreviewOverlayProps) {
  const { t } = useI18n();
  const [fetched, setFetched] = useState<string | null>(null);
  const [failed, setFailed] = useState(false);
  const [copied, setCopied] = useState(false);
  const [copyFailed, setCopyFailed] = useState(false);
  // The text to render. An inline source is authoritative and synchronous — it
  // never passes through the loading/error states, which only describe a fetch.
  const image = imageSrc !== undefined || (mime?.startsWith("image/") ?? false);
  const previewableText = isPreviewableTextAttachment(mime ?? "text/markdown", title);
  const unavailable = url !== undefined && !image && !previewableText;
  const plainText = previewableText && !isMarkdownAttachment(mime ?? "text/markdown", title);
  const source = inlineSource ?? fetched;
  const [zoom, setZoom] = useState(1);
  // The bytes the header's 下載 link points at. A stored blob needs the ?token=
  // gate; a staged data: URI already IS the bytes. An inline text `source` has
  // neither — nothing is fabricated for it.
  const downloadHref =
    imageSrc ?? (url !== undefined ? authedAttachmentUrl(url) : undefined);
  // The <img> src, for whichever of the two image modes is in play. Same value
  // as the download href by construction — the preview and the download must
  // never be able to point at different bytes.
  const imageBytes = image ? downloadHref : undefined;

  // Fetch the markdown text once (the authed blob URL — same ?token= gate the
  // download/thumbnail paths use). A non-ok response / network error surfaces
  // the honest error state, never a blank render.
  useEffect(() => {
    if (url === undefined || image || unavailable) return;
    let alive = true;
    setFetched(null);
    setFailed(false);
    fetch(authedAttachmentUrl(url))
      .then((r) => {
        if (!r.ok) throw new Error(`http ${r.status}`);
        return r.text();
      })
      .then((text) => {
        if (alive) setFetched(text);
      })
      .catch((e) => {
        if (alive) setFailed(true);
        console.warn("MarkdownPreviewOverlay: load failed", e);
      });
    return () => {
      alive = false;
    };
  }, [url, image, unavailable]);

  useEffect(() => setZoom(1), [url, imageSrc]);

  async function onCopyShareLink() {
    // Only a stored blob has an id to share; the button below is not rendered
    // for an inline source, so this guard is the type-level echo of that.
    if (attachmentId === undefined) return;
    setCopyFailed(false);
    try {
      await copyAttachmentShareLink(attachmentId);
      setCopied(true);
      window.setTimeout(() => setCopied(false), 2000);
    } catch (e) {
      console.warn("MarkdownPreviewOverlay: copy share link failed", e);
      setCopyFailed(true);
      window.setTimeout(() => setCopyFailed(false), 2000);
    }
  }

  // Esc closes — as the TOP layer. The overlay only mounts open, so it holds a
  // layer for exactly its lifetime; whatever opened it (a popover, a gallery,
  // a chat thread) sits below and does not see the key.
  const rootRef = useRef<HTMLDivElement>(null);
  useEscapeLayer(onClose, rootRef);

  return (
    <div
      ref={rootRef}
      className="md-preview"
      role="dialog"
      aria-modal="true"
      aria-label={title}
      onClick={onClose}
    >
      <div className="md-preview__panel" onClick={(e) => e.stopPropagation()}>
        <div className="md-preview__header">
          <span className="md-preview__title">
            <FileTextIcon size={16} />
            {title}
          </span>
          <div className="md-preview__actions">
            {/* Share needs a STORED blob id. Download only needs bytes, so it
             * also serves a staged `imageSrc`. An inline text source has
             * neither — nothing is fabricated for it. */}
            {attachmentId !== undefined && (
              <button
                type="button"
                className="md-preview__download md-preview__share"
                aria-label={
                  copyFailed
                    ? t.chat.shareLinkCopyFailed
                    : copied
                      ? t.chat.shareLinkCopied
                      : t.chat.copyShareLink
                }
                title={
                  copyFailed
                    ? t.chat.shareLinkCopyFailed
                    : copied
                      ? t.chat.shareLinkCopied
                      : t.chat.copyShareLink
                }
                onClick={() => void onCopyShareLink()}
              >
                {copied ? <CheckIcon size={14} /> : <CopyIcon size={14} />}
                {copyFailed
                  ? <span className="md-preview__action-label">{t.chat.shareLinkCopyFailed}</span>
                  : copied
                    ? <span className="md-preview__action-label">{t.chat.shareLinkCopied}</span>
                    : <span className="md-preview__action-label">{t.chat.copyShareLink}</span>}
              </button>
            )}
            {/* Download — the SECOND action, distinct from preview: the authed
             * blob URL (or the staged data: URI) with a download attribute. */}
            {downloadHref !== undefined && (
              <a
                className="md-preview__download"
                href={downloadHref}
                download={title || undefined}
                aria-label={t.chat.mdPreview.download}
                title={t.chat.mdPreview.download}
              >
                <DownloadIcon size={14} />
                <span className="md-preview__action-label">{t.chat.mdPreview.download}</span>
              </a>
            )}
            <button
              type="button"
              className="md-preview__close"
              aria-label={t.chat.mdPreview.close}
              onClick={onClose}
            >
              <CloseIcon size={16} />
            </button>
          </div>
        </div>
        <div className="md-preview__body">
          {image && imageBytes !== undefined ? (
            <div className="md-preview__image-wrap">
              <img
                className="md-preview__image"
                src={imageBytes}
                /* The filename IS the alt text: it is the only thing known
                 * about these bytes, and a generic 「圖片」 would tell a screen
                 * reader nothing that the surrounding dialog did not. */
                alt={title || t.chat.imageAlt}
                style={{ transform: `scale(${zoom})` }}
                onWheel={(e) => {
                  e.preventDefault();
                  setZoom((current) => Math.min(4, Math.max(0.5, current + (e.deltaY < 0 ? 0.25 : -0.25))));
                }}
              />
              {/* The zoom cluster is a labelled group, and each control names
               * itself: the bare −/+ glyphs announce as "minus"/"plus" with no
               * hint of what they act on, and the hard-coded English label the
               * first version carried was invisible to the wording overlay. */}
              <div className="md-preview__zoom" role="group" aria-label={t.chat.mdPreview.zoomControls}>
                <button
                  type="button"
                  aria-label={t.chat.mdPreview.zoomOut}
                  title={t.chat.mdPreview.zoomOut}
                  onClick={() => setZoom((value) => Math.max(0.5, value - 0.25))}
                >
                  −
                </button>
                <span>{Math.round(zoom * 100)}%</span>
                <button
                  type="button"
                  aria-label={t.chat.mdPreview.zoomIn}
                  title={t.chat.mdPreview.zoomIn}
                  onClick={() => setZoom((value) => Math.min(4, value + 0.25))}
                >
                  +
                </button>
              </div>
            </div>
          ) : unavailable ? (
            <div className="md-preview__status">{t.chat.mdPreview.unavailable}</div>
          ) : failed ? (
            <div className="md-preview__status">{t.chat.mdPreview.error}</div>
          ) : source === null ? (
            <div className="md-preview__status">{t.chat.mdPreview.loading}</div>
          ) : (
            /* `.doc-md` is the SHARED markdown skin (headings, code, tables,
             * links, callouts) every other render site wears — the task manual,
             * the role doc, the reply card, the chat bubble. Rendering without
             * it left this overlay on bare UA defaults: black-on-card headings,
             * unstyled code, no callout colour. One class, same document look
             * as the surface the file was opened from. */
            plainText ? (
              <pre className="md-preview__text">{source}</pre>
            ) : <Markdown
              source={source}
              className="md-preview__md doc-md"
              /* An inline source is a CHAT message: Enter meant "new line"
               * when it was typed and the bubble renders it that way, so the
               * full-view read of the same text must keep those newlines.
               * Standard markdown folds them into spaces, which reflowed a
               * plain multi-line message into one run-on line. A fetched .md
               * blob is a document, not a chat line — it keeps standard
               * soft-wrap, same as every other document surface. */
              breaks={inlineSource !== undefined}
            />
          )}
        </div>
      </div>
    </div>
  );
}

/** Whether an attachment (by mime / filename) is a markdown doc the preview
 * overlay can render. Mirrors the server's text/markdown handling; also accepts
 * a `.md`/`.markdown` filename when the mime is a generic text/plain. */
export function isMarkdownAttachment(mime: string, filename: string): boolean {
  if (mime === "text/markdown" || mime === "text/x-markdown") return true;
  const name = filename.toLowerCase();
  return name.endsWith(".md") || name.endsWith(".markdown");
}

/** The stored text formats supported by the shared attachment modal. Keep this
 * narrow: HTML/PDF and arbitrary text/* remain outside this preview contract. */
export function isPreviewableTextAttachment(mime: string, filename: string): boolean {
  return isMarkdownAttachment(mime, filename) || /\.(txt|log)$/i.test(filename);
}
