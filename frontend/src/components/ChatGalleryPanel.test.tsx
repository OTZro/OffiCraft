// M2-3 member file & image gallery panel (batch-16 upgrade: member-perspective
// scope + 圖片/檔案 tabs).
//
// Covers: the dedicated gallery fetch (listChatAttachments — the server
// flattens + sender-labels the rows; no client aggregation), the 圖片/檔案 tab
// split with per-tab honest empty states, inter-agent rows surfacing with their
// server-resolved sender names, sender + time per row, the preview/download
// split (previewable mime → open in a new tab; opaque binary → download), and
// closing.

import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, fireEvent, waitFor, screen } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { ChatGalleryPanel, isPreviewableMime } from "./ChatGalleryPanel";
import type { Member } from "../types";
import type { GalleryAttachment } from "../api/adapter";

let galleryRows: GalleryAttachment[] = [];
const listChatAttachments = vi.fn(
  async (_withId: string): Promise<GalleryAttachment[]> => galleryRows,
);
const getChatAttachmentShareLink = vi.fn(
  async (id: string): Promise<string> => `/api/chat/attachment/${id}?sig=test-sig`,
);

vi.mock("../api", () => ({
  api: {
    listChatAttachments: (withId: string) => listChatAttachments(withId),
    getChatAttachmentShareLink: (id: string) => getChatAttachmentShareLink(id),
    subscribeEvents: () => () => {},
  },
}));

function mkMember(): Member {
  return {
    id: "m1",
    name: "Mira",
    role: "assistant",
    status: "online",
    lifecycle: "online",
    model: "opus",
    effort: "medium",
    kind: "assistant",
    desiredMachineId: "",
    machine: null,
    account: null,
    contextPct: null,
    estimatedCost: null,
    bankedCost: null,
    tmuxSession: "member-m1",
    refocusSince: null,
    lastOp: "",
    lastOpOk: null,
    lastOpLog: "",
    lastOpAt: null,
    unreadCount: 0,
  };
}

function row(
  id: string,
  mime: string,
  from: string,
  fromName: string,
  ts: number,
  filename = `${id}.bin`,
): GalleryAttachment {
  return {
    id,
    url: `/api/chat/attachment/${id}`,
    filename,
    mime,
    isImage: mime.startsWith("image/"),
    messageId: `msg-${id}`,
    from,
    fromName,
    to: from === "owner" ? "m1" : "owner",
    ts,
  };
}

function renderPanel(
  onClose: () => void = () => {},
  resolveSender?: (id: string) => string,
) {
  return render(
    <I18nProvider>
      <ChatGalleryPanel
        member={mkMember()}
        resolveSender={resolveSender}
        onClose={onClose}
      />
    </I18nProvider>,
  );
}

const itemsIn = (container: HTMLElement) => [
  ...container.querySelectorAll<HTMLElement>(".chat__gallery-item"),
];

describe("ChatGalleryPanel", () => {
  beforeEach(() => {
    galleryRows = [];
    listChatAttachments.mockClear();
    localStorage.clear();
  });

  it("fetches the member's flattened gallery (listChatAttachments)", async () => {
    renderPanel();
    await waitFor(() => expect(listChatAttachments).toHaveBeenCalledWith("m1"));
  });

  it("splits 圖片/檔案 tabs and renders sender + time per row (incl. inter-agent)", async () => {
    galleryRows = [
      // Server order: newest→oldest. An inter-agent row (Bob→Mira) rides the
      // SAME list — it must surface, labelled with the SERVER-resolved name.
      row("a3", "application/pdf", "m2", "Bob", 300, "from-bob.pdf"),
      row("a2", "application/pdf", "m1", "Mira", 200, "r.pdf"),
      row("a1", "image/png", "owner", "", 100, "shot.png"),
    ];
    const { container } = renderPanel();
    // Default tab: 圖片 — only the image shows.
    await waitFor(() => expect(itemsIn(container).length).toBe(1));
    expect(itemsIn(container)[0].querySelector("img")).toBeTruthy();
    // The owner's row reads 我 (zh default locale); time from the message ts.
    expect(itemsIn(container)[0].textContent).toContain("我");
    expect(
      itemsIn(container)[0].querySelector(".chat__gallery-sub")?.textContent,
    ).toMatch(/\d{1,2}:\d{2}/);
    expect(container.textContent).not.toContain("r.pdf");
    // Switch to 檔案 — both files show, newest first, sender names shown; the
    // raw member ids never render (only display names).
    fireEvent.click(screen.getByRole("tab", { name: "檔案" }));
    await waitFor(() => expect(itemsIn(container).length).toBe(2));
    expect(itemsIn(container)[0].textContent).toContain("from-bob.pdf");
    expect(itemsIn(container)[0].textContent).toContain("Bob");
    expect(itemsIn(container)[1].textContent).toContain("Mira");
    expect(container.textContent).not.toContain("m2");
    expect(container.textContent).not.toContain("shot.png");
  });

  it("opens every stored attachment in the shared modal", async () => {
    galleryRows = [
      row("a1", "image/png", "owner", "", 100, "img.png"),
      row("a2", "text/markdown", "owner", "", 100, "notes.md"),
      row("a3", "application/pdf", "owner", "", 100, "doc.pdf"),
      row("a4", "application/zip", "owner", "", 100, "bundle.zip"),
    ];
    const { container } = renderPanel();
    fireEvent.click(screen.getByRole("tab", { name: "檔案" }));
    await waitFor(() => expect(itemsIn(container).length).toBe(3));
    const byName = (name: string) => itemsIn(container).find((item) => item.textContent?.includes(name))!;
    fireEvent.click(byName("notes.md"));
    expect(await screen.findByRole("dialog", { name: "notes.md" })).toBeTruthy();
    fireEvent.click(screen.getByLabelText("關閉預覽"));
    fireEvent.click(byName("doc.pdf"));
    expect(await screen.findByRole("dialog", { name: "doc.pdf" })).toBeTruthy();
    // T-36 (B1) — the pdf still cannot be DRAWN in the panel, but the header
    // now carries 「在新頁面顯示」 for it, so the body line points at that button
    // instead of back at 下載 once the share link has been minted.
    expect(
      await screen.findByText("此檔案無法在這裡預覽，請用上方的「在新頁面顯示」開啟。"),
    ).toBeTruthy();
    fireEvent.click(screen.getByLabelText("關閉預覽"));
    fireEvent.click(byName("bundle.zip"));
    expect(await screen.findByRole("dialog", { name: "bundle.zip" })).toBeTruthy();
    fireEvent.click(screen.getByRole("tab", { name: "圖片" }));
    await waitFor(() => expect(itemsIn(container).length).toBe(1));
    fireEvent.click(itemsIn(container)[0]);
    expect(await screen.findByRole("dialog", { name: "img.png" })).toBeTruthy();
  });

  it("opens PDF and binaries only from their row, not a duplicate share button", async () => {
    galleryRows = [
      row("pdf-att", "application/pdf", "owner", "", 100, "doc.pdf"),
      row("zip-att", "application/zip", "owner", "", 99, "bundle.zip"),
    ];
    const { container } = renderPanel();
    fireEvent.click(screen.getByRole("tab", { name: "檔案" }));
    await waitFor(() => expect(itemsIn(container).length).toBe(2));
    const pdf = itemsIn(container).find((item) => item.textContent?.includes("doc.pdf"))!;
    const zip = itemsIn(container).find((item) => item.textContent?.includes("bundle.zip"))!;
    expect(container.querySelector(".chat__gallery-share")).toBeNull();
    fireEvent.click(pdf);
    expect(await screen.findByRole("dialog", { name: "doc.pdf" })).toBeTruthy();
    fireEvent.click(screen.getByLabelText("關閉預覽"));
    fireEvent.click(zip);
    expect(await screen.findByRole("dialog", { name: "bundle.zip" })).toBeTruthy();
  });

  it("opens a previewable row from Enter and Space without letting the nested share button open it", async () => {
    galleryRows = [row("image-att", "image/png", "owner", "", 100, "shot.png")];
    const { container } = renderPanel();
    await waitFor(() => expect(itemsIn(container).length).toBe(1));
    const galleryRow = itemsIn(container)[0];
    expect(galleryRow.getAttribute("role")).toBe("button");
    expect(galleryRow.getAttribute("tabindex")).toBe("0");
    fireEvent.keyDown(galleryRow, { key: "Enter" });
    expect(await screen.findByRole("dialog", { name: "shot.png" })).toBeTruthy();
    fireEvent.click(screen.getByLabelText("關閉預覽"));
    fireEvent.keyDown(galleryRow, { key: " " });
    expect(await screen.findByRole("dialog", { name: "shot.png" })).toBeTruthy();
  });

  it("shows per-tab honest empty states once loaded", async () => {
    galleryRows = [row("a1", "application/pdf", "m1", "Mira", 100, "only.pdf")];
    const first = renderPanel();
    // 圖片 tab (default) is empty even though a FILE exists.
    expect(await screen.findByText("還沒有圖片")).toBeTruthy();
    fireEvent.click(screen.getByRole("tab", { name: "檔案" }));
    await waitFor(() =>
      expect(screen.queryByText("還沒有圖片")).toBeNull(),
    );
    expect(screen.getByText("only.pdf")).toBeTruthy();
    first.unmount();
    // And the 檔案 tab's own empty state when nothing at all exists.
    galleryRows = [];
    renderPanel();
    await screen.findByRole("dialog");
    fireEvent.click(screen.getByRole("tab", { name: "檔案" }));
    expect(await screen.findByText("還沒有檔案")).toBeTruthy();
  });

  // ── Uploader filter (batch 18, reshaped by T-51 ②) ────────────────────────
  // ONE control under the tabs; the uploaders live behind it in a fixed-height
  // scrolling popover with a checkbox each. What it replaced was a wrapping
  // chip row with no cap: measured on a 2,200-file corpus it stood 1,168px tall
  // inside a 696px panel and pushed the file list off the screen entirely.
  //
  // 🔴 THE OPTIONS ARE CUT FROM THE TAB BEING SHOWN. They used to come from
  // every row in both tabs while the list applied the tab, so 圖片 offered
  // uploaders who had only ever sent non-images and ticking one answered with
  // an empty gallery — 66 of the owner's 114 uploaders were dead options that
  // way (Kyle, 2026-09-02). The old test that asserted the empty state for
  // exactly that combination is gone BECAUSE the combination is now
  // unreachable, not because the empty state was dropped: it is still asserted
  // above, in "shows per-tab honest empty states once loaded".

  const filterToggle = () =>
    screen.getByRole("button", { name: "依上傳者篩選" });
  /** Open the popover (a real click sends mousedown too, and the popover's
   * dismiss-on-outside-click listens for it — firing only `click` here would
   * describe a product that does not exist). */
  const openFilter = () => {
    fireEvent.mouseDown(filterToggle());
    fireEvent.click(filterToggle());
  };
  const optionLabels = () =>
    [...document.querySelectorAll(".chat__gallery-sender-option-name")].map(
      (n) => n.textContent,
    );
  const tickOption = (label: string) => {
    const opt = [
      ...document.querySelectorAll<HTMLLabelElement>(
        ".chat__gallery-sender-option",
      ),
    ].find((l) => l.textContent?.startsWith(label))!;
    fireEvent.click(opt.querySelector("input")!);
  };

  it("offers one option per actual uploader of the tab being shown, named not id'd", async () => {
    galleryRows = [
      row("a4", "image/png", "m2", "Bob", 400, "bob.png"),
      row("a3", "image/png", "owner", "", 300, "mine.png"),
      row("a2", "image/png", "m1", "Mira", 200, "mira.png"),
      row("a1", "application/pdf", "m2", "Bob", 100, "bob.pdf"),
    ];
    const { container } = renderPanel();
    await waitFor(() => expect(itemsIn(container).length).toBe(3));
    openFilter();
    // The three uploaders WITH AN IMAGE — owner reads 「我」, others by their
    // server-resolved names; no raw internal ids leak.
    expect(optionLabels()).toEqual(["Bob", "我", "Mira"]);
    // Tick Bob → only Bob's image remains on the 圖片 tab.
    tickOption("Bob");
    await waitFor(() => expect(itemsIn(container).length).toBe(1));
    expect(itemsIn(container)[0].textContent).toContain("bob.png");
    expect(container.textContent).not.toContain("mine.png");
    expect(container.textContent).not.toContain("mira.png");
  });

  it("offers no uploader whose only files are on the other tab", async () => {
    galleryRows = [
      row("a2", "image/png", "m1", "Mira", 200, "mira.png"),
      row("a1", "application/pdf", "m2", "Bob", 100, "bob.pdf"),
    ];
    const { container } = renderPanel();
    await waitFor(() => expect(itemsIn(container).length).toBe(1));
    openFilter();
    expect(
      optionLabels(),
      "Bob has no image, so 圖片 must not offer a tick that answers with nothing",
    ).toEqual(["Mira"]);
    // His file is one tab away, and there he IS an option. (mousedown too: an
    // open popover dismisses on an outside mousedown, and a click that skipped
    // it would leave the popover open and turn the openFilter() below into a
    // close.)
    fireEvent.mouseDown(screen.getByRole("tab", { name: "檔案" }));
    fireEvent.click(screen.getByRole("tab", { name: "檔案" }));
    await waitFor(() => expect(itemsIn(container).length).toBe(1));
    openFilter();
    expect(optionLabels()).toEqual(["Bob"]);
  });

  it("keeps a departed member in the list so their old files stay reachable", async () => {
    // The server resolves a sender's name at ANY roster status on purpose
    // (api_chat.go: "ANY roster status — dismissed still reads by name").
    // Folding the row into a dropdown must not become "removed from the list":
    // the gallery is where an old file is found, and old files come from people
    // who have since left.
    galleryRows = [
      row("a2", "image/png", "m9", "已解僱的同事", 200, "old.png"),
      row("a1", "image/png", "m1", "Mira", 100, "mira.png"),
    ];
    renderPanel();
    await waitFor(() => expect(screen.queryByText("old.png")).toBeTruthy());
    openFilter();
    expect(optionLabels()).toContain("已解僱的同事");
  });

  it("resolves an unnamed outsource sender through resolveSender, not the raw id", async () => {
    // The server leaves from_name "" for an outsource sender: the gallery
    // handler's names table comes from `dal.ListMembers`, which is `WHERE kind
    // != 'outsource'` (api_chat.go) — so the caller-provided resolver
    // (ChatArea's nameOf codename chain) is what names the row and its
    // uploader option; without a resolver hit the raw id would show.
    galleryRows = [
      row("a1", "image/png", "ow-533c0c4f9dba", "", 100, "work.png"),
    ];
    const { container } = renderPanel(undefined, (id) =>
      id === "ow-533c0c4f9dba" ? "外包 · X-1" : id,
    );
    await waitFor(() => expect(itemsIn(container).length).toBe(1));
    expect(itemsIn(container)[0].textContent).toContain("外包 · X-1");
    openFilter();
    expect(optionLabels()).toEqual(["外包 · X-1"]);
    expect(container.textContent).not.toContain("ow-533c0c4f9dba");
  });

  it("narrows to the union of every ticked uploader and says how many are ticked", async () => {
    galleryRows = [
      row("a3", "image/png", "m2", "Bob", 300, "bob.png"),
      row("a2", "image/png", "m1", "Mira", 200, "mira.png"),
      row("a1", "image/png", "m3", "Cy", 100, "cy.png"),
    ];
    const { container } = renderPanel();
    await waitFor(() => expect(itemsIn(container).length).toBe(3));
    openFilter();
    tickOption("Bob");
    await waitFor(() => expect(itemsIn(container).length).toBe(1));
    expect(filterToggle().textContent).toContain("已選 1 位");
    // A SECOND tick widens the answer. The chip row this replaced could only
    // ever hold one uploader — picking a second one dropped the first.
    tickOption("Mira");
    await waitFor(() => expect(itemsIn(container).length).toBe(2));
    expect(container.textContent).toContain("bob.png");
    expect(container.textContent).toContain("mira.png");
    // Clearing goes back to 全部, not to "one of them".
    fireEvent.click(screen.getByRole("button", { name: "清除選取" }));
    await waitFor(() => expect(itemsIn(container).length).toBe(3));
    expect(filterToggle().textContent).toContain("全部");
  });

  it("stays one line high when closed, however many uploaders there are", async () => {
    // 🔴 THE HEIGHT IS THE BUG. jsdom applies no CSS, so this cannot measure
    // pixels — what it CAN pin is the structural cause of those pixels: how
    // many controls the closed filter puts on the page. The chip row rendered
    // one per uploader (plus 全部); this must render exactly one whatever the
    // count, with the uploaders behind it. The pixel geometry is measured in a
    // real browser by the visual guard.
    galleryRows = Array.from({ length: 60 }, (_, i) =>
      row(`a${i}`, "image/png", `m${i}`, `Sender ${i}`, 100 + i, `s${i}.png`),
    );
    const { container } = renderPanel();
    await waitFor(() => expect(itemsIn(container).length).toBe(60));
    const filter = container.querySelector(".chat__gallery-senders")!;
    expect(filter.querySelectorAll("button").length).toBe(1);
    expect(
      document.querySelectorAll(".chat__gallery-sender-option").length,
      "the 60 uploaders are behind the toggle until it is opened",
    ).toBe(0);
    openFilter();
    expect(document.querySelectorAll(".chat__gallery-sender-option").length).toBe(60);
  });

  it("pages the preview across the list the reader is looking at, not every attachment", async () => {
    galleryRows = [
      row("a3", "image/png", "m2", "Bob", 300, "bob.png"),
      row("a2", "image/png", "m1", "Mira", 200, "mira.png"),
      row("a1", "application/pdf", "m1", "Mira", 100, "mira.pdf"),
    ];
    const { container } = renderPanel();
    await waitFor(() => expect(itemsIn(container).length).toBe(2));
    fireEvent.click(itemsIn(container)[0]);
    // Two images on this tab; the pdf is not one of them, and the counter is
    // the pager's own statement of what it will walk.
    expect(
      document.querySelector(".md-preview__pager-count")?.textContent,
    ).toBe("1 / 2");
    fireEvent.click(screen.getByRole("button", { name: "下一個" }));
    expect(
      document.querySelector(".md-preview__pager-count")?.textContent,
    ).toBe("2 / 2");
    expect(
      (screen.getByRole("button", { name: "下一個" }) as HTMLButtonElement)
        .disabled,
      "the last item has nothing after it, and the control says so",
    ).toBe(true);
    expect(
      (screen.getByRole("button", { name: "上一個" }) as HTMLButtonElement)
        .disabled,
    ).toBe(false);
  });

  it("keeps gallery rows free of a duplicate share control", async () => {
    galleryRows = [row("a1", "image/png", "owner", "", 100, "shot.png")];
    const { container } = renderPanel();
    await waitFor(() => expect(itemsIn(container).length).toBe(1));
    expect(container.querySelector(".chat__gallery-share")).toBeNull();
    fireEvent.click(itemsIn(container)[0]);
    const popup = await screen.findByRole("dialog", { name: "shot.png" });
    expect(popup.querySelector("button.md-preview__share")).toBeTruthy();
  });

  it("closes via the close button and via Escape", async () => {
    const onClose = vi.fn();
    renderPanel(onClose);
    await screen.findByRole("dialog");
    fireEvent.click(screen.getByLabelText("關閉檔案庫"));
    expect(onClose).toHaveBeenCalledTimes(1);
    fireEvent.keyDown(window, { key: "Escape" });
    expect(onClose).toHaveBeenCalledTimes(2);
  });

  it("lets Escape close an open preview without also closing the gallery", async () => {
    galleryRows = [row("a1", "image/png", "owner", "", 100, "shot.png")];
    const onClose = vi.fn();
    const { container } = renderPanel(onClose);
    await waitFor(() => expect(itemsIn(container).length).toBe(1));
    fireEvent.click(itemsIn(container)[0]);
    expect(await screen.findByRole("dialog", { name: "shot.png" })).toBeTruthy();
    fireEvent.keyDown(window, { key: "Escape" });
    expect(screen.queryByRole("dialog", { name: "shot.png" })).toBeNull();
    expect(container.querySelector(".chat__gallery")).toBeTruthy();
    expect(onClose).not.toHaveBeenCalled();
  });
});

describe("isPreviewableMime (pure)", () => {
  it("mirrors the server's preview table", () => {
    expect(isPreviewableMime("image/webp")).toBe(true);
    expect(isPreviewableMime("text/html")).toBe(true);
    expect(isPreviewableMime("text/markdown")).toBe(true);
    expect(isPreviewableMime("application/pdf")).toBe(true);
    expect(isPreviewableMime("application/zip")).toBe(false);
    expect(isPreviewableMime("application/octet-stream")).toBe(false);
  });
});
