// T-c645: owner-facing Chromium evidence for the one attachment modal shell.
// The fixture stays component-only: it never starts the real server or touches
// the destructive e2e suite. Each screen captures one stored-blob body type.
import { test, expect } from "@playwright/experimental-ct-react";
import { I18nProvider } from "../src/i18n";
import { AttachmentStrip } from "../src/components/AttachmentStrip";
import { MarkdownPreviewOverlay } from "../src/components/MarkdownPreviewOverlay";

const SHOT_DIR = process.env.T_C645_SHOT_DIR ?? "test-results/t-c645";
const PNG =
  "data:image/svg+xml,%3Csvg xmlns='http://www.w3.org/2000/svg' width='900' height='520'%3E%3Crect width='100%25' height='100%25' fill='%23163a5f'/%3E%3Ccircle cx='450' cy='260' r='155' fill='%235cc8ff'/%3E%3Ctext x='450' y='275' text-anchor='middle' font-size='48' fill='white'%3EAttachment image%3C/text%3E%3C/svg%3E";

async function mountBlob(
  mount: any,
  props: { title: string; url: string; attachmentId: string; mime: string },
) {
  return mount(
    <I18nProvider>
      <MarkdownPreviewOverlay {...props} onClose={() => {}} />
    </I18nProvider>,
  );
}

test("image: shared header and zoomed image render in Chromium", async ({ mount, page }) => {
  await page.setViewportSize({ width: 1100, height: 760 });
  const cmp = await mountBlob(mount, {
    title: "preview.png",
    url: PNG,
    attachmentId: "att-image",
    mime: "image/png",
  });
  await expect(page.getByRole("dialog", { name: "preview.png" })).toBeVisible();
  await expect(cmp.locator(".md-preview__image")).toHaveCSS("transform", "matrix(1, 0, 0, 1, 0, 0)");
  await page.screenshot({ path: `${SHOT_DIR}/image-modal.png`, fullPage: true });
});

test("markdown: shared header and rendered document render in Chromium", async ({ mount, page }) => {
  await page.route("**/api/chat/attachment/att-markdown*", (route) =>
    route.fulfill({ contentType: "text/markdown", body: "# Preview document\n\nThis is **Markdown**." }),
  );
  await page.setViewportSize({ width: 1100, height: 760 });
  const cmp = await mountBlob(mount, {
    title: "notes.md",
    url: "/api/chat/attachment/att-markdown",
    attachmentId: "att-markdown",
    mime: "text/markdown",
  });
  await expect(cmp.getByRole("heading", { name: "Preview document" })).toBeVisible();
  await page.screenshot({ path: `${SHOT_DIR}/markdown-modal.png`, fullPage: true });
});

test("text: shared header and literal txt/log content render in Chromium", async ({ mount, page }) => {
  await page.route("**/api/chat/attachment/att-log*", (route) =>
    route.fulfill({ contentType: "text/plain", body: "2026-07-29 INFO preview loaded\n2026-07-29 WARN sample line" }),
  );
  await page.setViewportSize({ width: 1100, height: 760 });
  const cmp = await mountBlob(mount, {
    title: "agent.log",
    url: "/api/chat/attachment/att-log",
    attachmentId: "att-log",
    mime: "text/plain",
  });
  await expect(cmp.locator(".md-preview__text")).toContainText("preview loaded");
  await page.screenshot({ path: `${SHOT_DIR}/text-modal.png`, fullPage: true });
});

test("390px: popup header keeps filename space while actions become labelled icons", async ({ mount, page }) => {
  await page.setViewportSize({ width: 390, height: 844 });
  const cmp = await mountBlob(mount, {
    title: "a-very-long-stored-attachment-filename-for-mobile.pdf",
    url: "/api/chat/attachment/att-mobile-pdf",
    attachmentId: "att-mobile-pdf",
    mime: "application/pdf",
  });
  await expect(cmp.getByRole("button", { name: "複製分享連結" })).toBeVisible();
  await expect(cmp.getByRole("link", { name: "下載" })).toBeVisible();
  await expect(cmp.locator(".md-preview__action-label").first()).toBeHidden();
  await page.screenshot({ path: `${SHOT_DIR}/mobile-popup-header.png`, fullPage: true });
});

// Each stored-attachment entrance owns its surrounding layout, but delegates
// its actual controls to AttachmentStrip. These are deliberately separate
// screenshots so the owner can inspect the four real entry shapes, not merely
// infer coverage from the common component.
const ENTRY_FIXTURES = [
  ["chat-attachment-row", "聊天室附件列", "chat__msg-attachments", "chat__msg-attachment"],
  ["gallery-attachment-row", "聊天室總覽", "chat__gallery-item", undefined],
  ["answered-reply-row", "回覆卡（已答覆側）", "reply-card__answer-atts", undefined],
  ["task-artifact-row", "任務產物", "task-artifacts__item", undefined],
] as const;

for (const [shotName, label, className, itemClassName] of ENTRY_FIXTURES) {
  test(`${label}: stored attachment opens the popup that owns share and download`, async ({ mount, page }) => {
    await page.setViewportSize({ width: 760, height: 380 });
    const cmp = await mount(
      <I18nProvider>
        <main style={{ padding: 28, maxWidth: 680 }}>
          <h1 style={{ fontSize: 18 }}>{label}</h1>
          <AttachmentStrip
            attachments={[{
              id: `att-${shotName}`,
              url: `/api/chat/attachment/att-${shotName}`,
              filename: "stored-document.pdf",
              mime: "application/pdf",
              isImage: false,
            }]}
            className={className}
            itemClassName={itemClassName}
            imageClassName="chat__msg-image"
            fileChipClassName={shotName === "task-artifact-row" ? "task-artifacts__chip" : "chat__msg-file"}
            fileNameClassName={shotName === "task-artifact-row" ? "task-artifacts__chip-name" : "chat__msg-file-name"}
          />
        </main>
      </I18nProvider>,
    );
    await page.screenshot({ path: `${SHOT_DIR}/${shotName}-outer.png`, fullPage: true });
    await cmp.getByRole("button", { name: "stored-document.pdf" }).click();
    const popup = page.getByRole("dialog", { name: "stored-document.pdf" });
    await expect(popup).toBeVisible();
    await expect(popup.getByRole("button", { name: "複製分享連結" })).toBeVisible();
    await expect(popup.getByRole("link", { name: "下載" })).toBeVisible();
    await expect(popup.getByText("此檔案無法預覽，請下載")).toBeVisible();
    await page.screenshot({ path: `${SHOT_DIR}/${shotName}.png`, fullPage: true });
  });
}
