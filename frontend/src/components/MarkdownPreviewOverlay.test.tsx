// MarkdownPreviewOverlay + isMarkdownAttachment (T-a1c4). The overlay fetches
// the .md blob text and renders it through the shared Markdown.tsx (NOT the
// browser's raw-source tab); preview and download are two distinct actions, so
// the header keeps a 下載 link alongside the render. isMarkdownAttachment gates
// which attachments get the 預覽 action.

import { describe, it, expect, vi, afterEach } from "vitest";
import { render, fireEvent, waitFor, screen } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { api } from "../api";
import {
  MarkdownPreviewOverlay,
  isMarkdownAttachment,
} from "./MarkdownPreviewOverlay";

describe("isMarkdownAttachment", () => {
  it("accepts markdown mimes and .md/.markdown filenames", () => {
    expect(isMarkdownAttachment("text/markdown", "x")).toBe(true);
    expect(isMarkdownAttachment("text/x-markdown", "x")).toBe(true);
    expect(isMarkdownAttachment("text/plain", "design.md")).toBe(true);
    expect(isMarkdownAttachment("application/octet-stream", "NOTES.MARKDOWN")).toBe(true);
  });
  it("rejects non-markdown", () => {
    expect(isMarkdownAttachment("application/pdf", "report.pdf")).toBe(false);
    expect(isMarkdownAttachment("image/png", "shot.png")).toBe(false);
    expect(isMarkdownAttachment("text/plain", "notes.txt")).toBe(false);
  });
});

describe("MarkdownPreviewOverlay", () => {
  const realFetch = globalThis.fetch;
  afterEach(() => {
    globalThis.fetch = realFetch;
    vi.restoreAllMocks();
  });

  it("fetches the blob text and renders it as markdown", async () => {
    globalThis.fetch = vi.fn(async () => ({
      ok: true,
      text: async () => "# Hello\n\nsome **body**",
    })) as unknown as typeof fetch;

    render(
      <I18nProvider>
        <MarkdownPreviewOverlay
          title="design.md"
          url="/api/chat/attachment/att-1"
          attachmentId="att-1"
          onClose={() => {}}
        />
      </I18nProvider>,
    );
    // Heading rendered as an element (Markdown.tsx builds React elements).
    await waitFor(() => expect(screen.getByRole("heading", { name: "Hello" })).toBeTruthy());
    expect(screen.getByText("body")).toBeTruthy();
    // The download action is present and separate from the preview render.
    const dl = screen.getByText("下載").closest("a.md-preview__download")!;
    expect(dl.getAttribute("download")).toBe("design.md");
    expect(dl.getAttribute("href")).toContain("/api/chat/attachment/att-1");
  });

  it("shows an honest error state on a failed fetch (never a blank render)", async () => {
    globalThis.fetch = vi.fn(async () => ({ ok: false, status: 404 })) as unknown as typeof fetch;
    render(
      <I18nProvider>
        <MarkdownPreviewOverlay
          title="gone.md"
          url="/api/chat/attachment/att-x"
          attachmentId="att-x"
          onClose={() => {}}
        />
      </I18nProvider>,
    );
    await waitFor(() => expect(screen.getByText("無法載入預覽")).toBeTruthy());
  });

  it("closes on the × button and on Esc", async () => {
    globalThis.fetch = vi.fn(async () => ({ ok: true, text: async () => "# x" })) as unknown as typeof fetch;
    const onClose = vi.fn();
    render(
      <I18nProvider>
        <MarkdownPreviewOverlay
          title="x.md"
          url="/api/chat/attachment/att-1"
          attachmentId="att-1"
          onClose={onClose}
        />
      </I18nProvider>,
    );
    // Let the fetch settle so the close click is the only state change asserted.
    await waitFor(() => expect(screen.getByRole("heading", { name: "x" })).toBeTruthy());
    fireEvent.click(screen.getByLabelText("關閉預覽"));
    expect(onClose).toHaveBeenCalledTimes(1);
    fireEvent.keyDown(window, { key: "Escape" });
    expect(onClose).toHaveBeenCalledTimes(2);
  });
});

// T-d10b: the header's THIRD action. 產生分享連結 already lived on the chat
// bubble and the gallery row; this panel was the gap the owner hit ("點開
// preview 的時候只能下載"). The button must reuse lib/shareLink.ts — a fourth
// parallel mint is the drift this repo keeps paying for.
describe("MarkdownPreviewOverlay 複製分享連結 (T-d10b)", () => {
  const realFetch = globalThis.fetch;
  afterEach(() => {
    globalThis.fetch = realFetch;
    vi.restoreAllMocks();
  });

  function renderOverlay() {
    globalThis.fetch = vi.fn(async () => ({
      ok: true,
      text: async () => "# doc",
    })) as unknown as typeof fetch;
    return render(
      <I18nProvider>
        <MarkdownPreviewOverlay
          title="design.md"
          url="/api/chat/attachment/att-7"
          attachmentId="att-7"
          onClose={() => {}}
        />
      </I18nProvider>,
    );
  }

  it("sits LEFT of 下載 in the header action row", async () => {
    const { container } = renderOverlay();
    await waitFor(() => expect(screen.getByRole("heading", { name: "doc" })).toBeTruthy());
    const actions = container.querySelector(".md-preview__actions")!;
    const share = screen.getByRole("button", { name: "複製分享連結" });
    const dl = screen.getByText("下載").closest("a.md-preview__download")!;
    expect(actions.contains(share)).toBe(true);
    // Node.DOCUMENT_POSITION_FOLLOWING === 4: `dl` comes after `share`.
    expect(share.compareDocumentPosition(dl) & 4).toBe(4);
  });

  it("mints THIS attachment's share link and flashes 已複製連結", async () => {
    const mint = vi
      .spyOn(api, "getChatAttachmentShareLink")
      .mockResolvedValue("/api/chat/attachment/att-7?sig=test-sig");
    const writeText = vi.fn(async () => {});
    Object.defineProperty(navigator, "clipboard", {
      value: { writeText },
      configurable: true,
    });
    renderOverlay();
    await waitFor(() => expect(screen.getByRole("heading", { name: "doc" })).toBeTruthy());
    fireEvent.click(screen.getByRole("button", { name: "複製分享連結" }));

    await waitFor(() => expect(mint).toHaveBeenCalledWith("att-7"));
    // Absolutised against the page origin by lib/shareLink.ts — the server
    // mints a server-RELATIVE path, so a bare path on the clipboard would be
    // an unusable link.
    await waitFor(() =>
      expect(writeText).toHaveBeenCalledWith(
        `${window.location.origin}/api/chat/attachment/att-7?sig=test-sig`,
      ),
    );
    // Feedback only AFTER both the mint and the clipboard write succeeded.
    await waitFor(() =>
      expect(screen.getByRole("button", { name: "已複製連結" })).toBeTruthy(),
    );
  });

  it("never fakes 已複製連結 when the mint fails", async () => {
    vi.spyOn(api, "getChatAttachmentShareLink").mockRejectedValue(new Error("boom"));
    vi.spyOn(console, "warn").mockImplementation(() => {});
    const writeText = vi.fn(async () => {});
    Object.defineProperty(navigator, "clipboard", {
      value: { writeText },
      configurable: true,
    });
    renderOverlay();
    await waitFor(() => expect(screen.getByRole("heading", { name: "doc" })).toBeTruthy());
    fireEvent.click(screen.getByRole("button", { name: "複製分享連結" }));
    await waitFor(() => expect(writeText).not.toHaveBeenCalled());
    expect(screen.queryByRole("button", { name: "已複製連結" })).toBeNull();
  });
});
