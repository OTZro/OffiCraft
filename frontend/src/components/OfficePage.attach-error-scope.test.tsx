// T-48 R14-2.1 — the staging refusal is scoped to the CHAT PAGE, the draft is not.
//
// 「圖片太大」/「最多 10 個檔案」 was component state on the composer until the
// draft store took it, so leaving the office page took it with it. Moving it to
// a module-level table gave it one lifetime too many: refuse a 30 MB image,
// walk to 任務, come back ten minutes later, and the red sentence is still on
// screen describing a drop from ten minutes ago.
//
// The draft and its staged files must NOT follow it out — surviving the
// navigation is the whole reason they live outside the composer.

import { describe, it, expect, beforeEach, vi } from "vitest";
import { render, waitFor } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { OfficePage } from "./OfficePage";
import { __resetMock } from "../api/mock";
import {
  getChatAttachError,
  getChatDraft,
  resetChatDrafts,
  saveChatDraftText,
  setChatAttachError,
  updateChatDraftAttachments,
} from "../lib/chatDraftStore";

const PEER = "m-1";

function renderOffice() {
  return render(
    <I18nProvider>
      <OfficePage />
    </I18nProvider>,
  );
}

beforeEach(() => {
  __resetMock();
  resetChatDrafts();
  window.location.hash = "";
  Element.prototype.scrollIntoView = vi.fn();
});

describe("OfficePage", () => {
  it("drops a staging refusal when the chat page is left, and keeps the draft", async () => {
    const view = renderOffice();
    await waitFor(() => expect(view.container.querySelector(".office")).not.toBeNull());

    saveChatDraftText(PEER, "沒送完的字");
    updateChatDraftAttachments(PEER, () => [
      { key: "pa-1", filename: "shot.png" } as never,
    ]);
    setChatAttachError(PEER, "圖片太大（上限 20 MB）");

    // 跳到「任務」/「監控」: the whole office page unmounts.
    view.unmount();

    expect(getChatAttachError(PEER)).toBeNull();
    expect(getChatDraft(PEER)?.text).toBe("沒送完的字");
    expect(getChatDraft(PEER)?.attachments).toHaveLength(1);

    // …and coming back shows no sentence about something ten minutes old.
    const back = renderOffice();
    await waitFor(() => expect(back.container.querySelector(".office")).not.toBeNull());
    expect(getChatAttachError(PEER)).toBeNull();
  });
});
