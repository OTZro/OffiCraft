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
// T-d10b: the header carries THREE actions, not two — 複製分享連結 sits left of
// 下載. 產生分享連結 already existed on the thread bubble (ChatArea) and the
// gallery row (ChatGalleryPanel); this surface was the one place it was missing,
// so the owner could only download what he had opened. It reuses the SAME
// `lib/shareLink.ts` mint + the SAME `chat.copyShareLink` / `chat.shareLinkCopied`
// keys as those two — a fourth parallel implementation is exactly the drift this
// repo keeps paying for. `attachmentId` is REQUIRED (not optional) so a new call
// site cannot quietly re-open the same hole.

import { useEffect, useState } from "react";
import { useI18n } from "../i18n";
import { authedAttachmentUrl } from "../api/http";
import { copyAttachmentShareLink } from "../lib/shareLink";
import { Markdown } from "./Markdown";
import {
  CheckIcon,
  CloseIcon,
  CopyIcon,
  DownloadIcon,
  FileTextIcon,
} from "./icons";

export function MarkdownPreviewOverlay({
  title,
  url,
  attachmentId,
  onClose,
}: {
  /** Display name shown in the header (the blob's filename). */
  title: string;
  /** The blob's serve path (`/api/chat/attachment/<id>`); fetched as text. */
  url: string;
  /** The blob id the share link is minted for — the SAME id the serve url
   * carries. Required: every caller already holds it, and making it optional
   * would let a call site silently render a preview with no share action. */
  attachmentId: string;
  onClose: () => void;
}) {
  const { t } = useI18n();
  const [source, setSource] = useState<string | null>(null);
  const [failed, setFailed] = useState(false);
  // Transient 「已複製」 feedback — set ONLY after the mint + clipboard write
  // both succeeded (same honesty rule as ChatArea / ChatGalleryPanel).
  const [copied, setCopied] = useState(false);

  // Fetch the markdown text once (the authed blob URL — same ?token= gate the
  // download/thumbnail paths use). A non-ok response / network error surfaces
  // the honest error state, never a blank render.
  useEffect(() => {
    let alive = true;
    setSource(null);
    setFailed(false);
    fetch(authedAttachmentUrl(url))
      .then((r) => {
        if (!r.ok) throw new Error(`http ${r.status}`);
        return r.text();
      })
      .then((text) => {
        if (alive) setSource(text);
      })
      .catch((e) => {
        if (alive) setFailed(true);
        console.warn("MarkdownPreviewOverlay: load failed", e);
      });
    return () => {
      alive = false;
    };
  }, [url]);

  async function onCopyShareLink() {
    try {
      await copyAttachmentShareLink(attachmentId);
      setCopied(true);
      window.setTimeout(() => setCopied(false), 2000);
    } catch (e) {
      console.warn("MarkdownPreviewOverlay: copy share link failed", e);
    }
  }

  // Esc closes — bound only while mounted (the overlay only mounts open).
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") onClose();
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [onClose]);

  return (
    <div
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
            {/* 複製分享連結 — mints the permanent ?sig= link for THIS blob via
             * the shared lib/shareLink.ts (same call the thread bubble and the
             * gallery row make). Sits left of 下載 (T-d10b). */}
            <button
              type="button"
              className="md-preview__download md-preview__share"
              aria-label={copied ? t.chat.shareLinkCopied : t.chat.copyShareLink}
              title={copied ? t.chat.shareLinkCopied : t.chat.copyShareLink}
              onClick={() => void onCopyShareLink()}
            >
              {copied ? <CheckIcon size={14} /> : <CopyIcon size={14} />}
              {copied ? t.chat.shareLinkCopied : t.chat.copyShareLink}
            </button>
            {/* Download — the SECOND action, distinct from preview: the authed
             * blob URL with a download attribute (server forces the bytes). */}
            <a
              className="md-preview__download"
              href={authedAttachmentUrl(url)}
              download={title || undefined}
            >
              <DownloadIcon size={14} />
              {t.chat.mdPreview.download}
            </a>
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
          {failed ? (
            <div className="md-preview__status">{t.chat.mdPreview.error}</div>
          ) : source === null ? (
            <div className="md-preview__status">{t.chat.mdPreview.loading}</div>
          ) : (
            <Markdown source={source} className="md-preview__md" />
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
