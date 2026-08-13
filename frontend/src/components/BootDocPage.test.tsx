// components/BootDocPage.test.tsx — the seven assertions this surface is
// bought on (T-791e). Each is written to fail on its own: they bind to seven
// different pieces of behaviour, so one of them going green cannot be mistaken
// for the others still being true.
//
//   1. Saving calls the REPLACE endpoint and the new content comes back onto
//      the page.
//   2. Version history lists the past versions.
//   3. The restore button calls the RESTORE endpoint, not the replace one.
//   4. Editing the claude document never sends the codex key (paired control:
//      the codex page shows the codex document).
//   5. Over the character limit is blocked IN THE COCKPIT, showing both the
//      current size and the limit — never a silent truncation.
//   6. seeds.ts's `?raw` text is used only as the FACTORY version; the page
//      body renders what the API currently holds (control: the API answers
//      with a string that is not the seed, and that string is what is on
//      screen).
//   7. A retained revision the server WOULD refuse is marked un-restorable
//      before the click — the cap these blocks answer to is the
//      `doc.cap_chars.*` SETTING, so the marking has to follow the owner's
//      live value the way every other document's does.
//
// Everything runs against `api/mock.ts` — the shared adapter, never a
// hand-rolled fake. A fake that answered these calls itself would be measuring
// a server that does not exist, which is the failure mode api/dtoParity.ts was
// written about.

import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { render, fireEvent, waitFor, within } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { zh } from "../i18n/locales/zh";
import { BootDocPage } from "./BootDocPage";
import { api } from "../api";
import { __resetMock } from "../api/mock";
import {
  SEED_BOOT_SEQUENCE_MD,
  SEED_BOOT_SEQUENCE_CODEX_MD,
  SEED_SYSTEM_INTERACTION_MD,
} from "../api/seeds";
import { BOOT_DOC_CAP_CHARS_DEFAULTS } from "../api/docCap";

const s = zh.settings;

function renderClaude() {
  return render(
    <I18nProvider>
      <BootDocPage
        kind="boot_sequence"
        docKey="claude"
        title={s.bootClaudeName}
        historyTitle={s.historyBootClaudeTitle}
        crumbs={[{ label: s.title }]}
      />
    </I18nProvider>
  );
}

function renderCodex() {
  return render(
    <I18nProvider>
      <BootDocPage
        kind="boot_sequence"
        docKey="codex"
        title={s.bootCodexName}
        historyTitle={s.historyBootCodexTitle}
        crumbs={[{ label: s.title }]}
      />
    </I18nProvider>
  );
}

function renderSystem() {
  return render(
    <I18nProvider>
      <BootDocPage
        kind="system_interaction"
        docKey="global"
        title={s.systemName}
        historyTitle={s.historyBootSystemTitle}
        crumbs={[{ label: s.title }]}
      />
    </I18nProvider>
  );
}

/** Paste `text` over section `i` and apply it. */
async function pasteSection(
  utils: ReturnType<typeof renderClaude>,
  i: number,
  text: string
) {
  fireEvent.click(await utils.findByTestId(`boot-doc-sec-paste-${i}`));
  fireEvent.change(utils.getByTestId(`boot-doc-sec-editor-${i}`), {
    target: { value: text },
  });
  fireEvent.click(utils.getByTestId(`boot-doc-sec-apply-${i}`));
}

beforeEach(() => {
  __resetMock();
});

afterEach(() => {
  vi.restoreAllMocks();
});

describe("BootDocPage", () => {
  it("save calls the REPLACE endpoint and renders the new content back", async () => {
    const save = vi.spyOn(api, "saveBootDoc");
    const utils = renderClaude();
    await utils.findByTestId("boot-doc-sec-0");

    await pasteSection(utils, 0, "# 全新的啟動程序標題\n\n");
    fireEvent.click(utils.getByTestId("boot-doc-save"));
    fireEvent.click(await utils.findByTestId("boot-doc-save-confirm-btn"));

    await waitFor(() => expect(save).toHaveBeenCalledTimes(1));
    const [kind, key, text] = save.mock.calls[0];
    expect(kind).toBe("boot_sequence");
    expect(key).toBe("claude");
    expect(text.startsWith("# 全新的啟動程序標題")).toBe(true);
    // The rest of the document rode along untouched — a whole-document replace
    // built from one edited section must not drop the sections nobody touched.
    expect(text).toContain("Claude Code 執行環境");

    // And the page now RENDERS the saved document: the heading came back from
    // the adapter's response, not from local state that was never confirmed.
    // findAllByText: the section LABEL and the rendered heading are both this
    // string — the label is derived from the heading, so one match would mean
    // the body did not re-render.
    expect((await utils.findAllByText("全新的啟動程序標題")).length).toBe(2);
    await waitFor(() =>
      expect(utils.queryByTestId("boot-doc-dirty")).toBeNull()
    );
  });

  it("version history lists the past versions", async () => {
    // Two writes ⇒ the state each replaced is retained. (The FIRST write to a
    // document that has never been customised replaces nothing and retains
    // nothing — server parity, see recordDocumentHistory — so a single save
    // would list zero and the assertion would be measuring that rule instead.)
    await api.saveBootDoc("boot_sequence", "claude", "第一版\n");
    await api.saveBootDoc("boot_sequence", "claude", "第二版\n");

    const utils = renderClaude();
    fireEvent.click(await utils.findByText(s.historyTitle));
    const list = await utils.findByTestId("doc-history-list");
    await waitFor(() =>
      expect(within(list).queryByText(s.historyLoading)).toBeNull()
    );
    // The list is a PICKER (owner 2026-07-31: no content preview on the rows),
    // so "lists the versions" is a claim about ROWS: the revision the second
    // write retained, plus the 初始版本 row that is always there.
    const rows = within(list).getAllByTestId(/^doc-history-open-\d+$/);
    expect(rows.length).toBe(1);
    expect(within(list).getByTestId("doc-history-seed")).toBeTruthy();

    // And the row really holds the version it claims to: the text the second
    // write replaced, readable one click deeper.
    fireEvent.click(rows[0]);
    const modal = await utils.findByTestId("doc-history-modal");
    await waitFor(() =>
      expect(modal.textContent ?? "").toContain("第一版")
    );

    // The note states this document's OWN retention — the default sentence
    // says three, which is true of every other document and false of this one.
    expect(list.textContent ?? "").toContain("10");
  });

  it("the restore button calls the RESTORE endpoint, not the replace one", async () => {
    await api.saveBootDoc("boot_sequence", "claude", "被改壞的啟動程序\n");
    const reset = vi.spyOn(api, "resetBootDoc");
    const save = vi.spyOn(api, "saveBootDoc");

    const utils = renderClaude();
    await utils.findAllByText(/被改壞的啟動程序/);

    fireEvent.click(utils.getByTestId("boot-doc-reset"));
    fireEvent.click(await utils.findByTestId("boot-doc-reset-confirm-btn"));

    await waitFor(() => expect(reset).toHaveBeenCalledTimes(1));
    expect(reset.mock.calls[0]).toEqual(["boot_sequence", "claude"]);
    // 🔴 The whole point: a "restore" implemented as a replace carrying the
    // seed text would look identical on screen and would leave the document
    // marked as owner-edited forever — and, on a server whose seed file has
    // since changed, would write back the WRONG text.
    expect(save).not.toHaveBeenCalled();

    // Back on the factory version, and the page says so.
    await utils.findByText(s.defaultBadge);
  });

  it("editing the claude document never sends the codex key", async () => {
    const save = vi.spyOn(api, "saveBootDoc");
    const claude = renderClaude();
    await claude.findByTestId("boot-doc-sec-0");
    await pasteSection(claude, 0, "# 只改 claude 這一份\n\n");
    fireEvent.click(claude.getByTestId("boot-doc-save"));
    fireEvent.click(await claude.findByTestId("boot-doc-save-confirm-btn"));
    await waitFor(() => expect(save).toHaveBeenCalledTimes(1));

    expect(save.mock.calls.map((c) => c[1])).toEqual(["claude"]);
    expect(save.mock.calls.some((c) => c[1] === "codex")).toBe(false);
    claude.unmount();

    // PAIRED CONTROL. Asserting only "codex was not written" is satisfied by a
    // page that writes nothing at all, so the other half has to hold too: the
    // codex document is untouched AND it is a different document — its own
    // text, not the claude one it sits next to in the settings list.
    const codex = renderCodex();
    await codex.findAllByText(/Codex App Server 執行環境/);
    expect(codex.container.textContent).not.toContain("只改 claude 這一份");
    expect(await api.getBootDoc("boot_sequence", "codex")).toMatchObject({
      text: SEED_BOOT_SEQUENCE_CODEX_MD.trim(),
      isDefault: true,
    });
  });

  it("blocks an over-limit document in the cockpit, naming the size and the limit", async () => {
    const save = vi.spyOn(api, "saveBootDoc");
    const cap = BOOT_DOC_CAP_CHARS_DEFAULTS.boot_sequence;
    const utils = renderClaude();
    await utils.findByTestId("boot-doc-sec-0");

    await pasteSection(utils, 0, "超".repeat(cap + 50));

    const notice = await utils.findByTestId("boot-doc-over-cap");
    const size = [...SEED_BOOT_SEQUENCE_MD.trim()].length; // untouched sections
    // BOTH numbers on screen. "Too long" alone leaves the owner with nothing to
    // act on, and a cockpit that trimmed the text to fit would be worse still:
    // the agent would boot from a document the owner never wrote.
    expect(notice.textContent).toContain(String(cap));
    expect(notice.textContent).toMatch(/\d{4,}/);
    const shown = Number(/\d{4,}/.exec(notice.textContent ?? "")?.[0]);
    expect(shown).toBeGreaterThan(cap);
    expect(shown).toBeGreaterThan(size);

    // Refused, not truncated: the save door is shut and clicking it sends
    // nothing.
    const saveBtn = utils.getByTestId("boot-doc-save") as HTMLButtonElement;
    expect(saveBtn.disabled).toBe(true);
    fireEvent.click(saveBtn);
    expect(utils.queryByTestId("boot-doc-save-confirm")).toBeNull();
    expect(save).not.toHaveBeenCalled();

    // The usage readout carries the same pair, so the number is on screen
    // before anyone crosses the line as well as after.
    expect(utils.getByTestId("boot-doc-usage").textContent).toBe(
      `${shown} / ${cap}`
    );
  });

  it("marks a revision the raised-then-lowered cap now refuses as un-restorable", async () => {
    // The only way an over-cap revision exists at all: the owner RAISED the
    // cap, wrote a long version, then put the cap back. Which is why the
    // marking cannot judge by the shipped default — it has to read the
    // `doc.cap_chars.system_interaction` setting that is in force NOW.
    const shipped = BOOT_DOC_CAP_CHARS_DEFAULTS.system_interaction;
    await api.patchServerSettings({
      docCapCharsSystemInteraction: shipped + 10000,
    });
    const overCap = "字".repeat(shipped + 5000);
    // The first write to an untouched document retains nothing, so the long
    // text has to be the one the SECOND write replaces.
    await api.saveBootDoc("system_interaction", "global", overCap);
    await api.saveBootDoc("system_interaction", "global", "短\n");
    await api.patchServerSettings({ docCapCharsSystemInteraction: shipped });

    const utils = renderSystem();
    fireEvent.click(
      await utils.findByTestId("doc-history-entry-system_interaction")
    );
    const list = await utils.findByTestId("doc-history-list");
    await waitFor(() =>
      expect(within(list).queryByText(s.historyLoading)).toBeNull()
    );

    const [target] = await api.listDocumentHistory(
      "system_interaction",
      "global"
    );
    expect(target.content.text).toBe(overCap);

    const row = await utils.findByTestId(`doc-history-item-${target.id}`);
    expect(within(row).getByText(s.historyBlockedBadge)).toBeTruthy();
    const reason = utils.getByTestId(`doc-history-blocked-${target.id}`);
    expect(reason.textContent).toContain(s.historyField.text);
    expect(reason.textContent).toContain(String(shipped));

    // …and opening it is not a way around the verdict either.
    fireEvent.click(utils.getByTestId(`doc-history-open-${target.id}`));
    expect(
      (utils.getByTestId("doc-history-modal-restore") as HTMLButtonElement)
        .disabled
    ).toBe(true);
  });

  it("renders the API's current version; the ?raw seed is only the factory version", async () => {
    // The control: make the API answer with a document that is NOT the seed,
    // then look at what the page body actually renders. Before T-791e these
    // pages rendered SEED_SYSTEM_INTERACTION_MD directly, and that version of
    // this page would pass every other assertion in this file while showing
    // the owner a document no agent boots from.
    const OWNER_TEXT = "# 這是 owner 現在的版本\n\n只有 API 知道這一段。\n";
    await api.saveBootDoc("system_interaction", "global", OWNER_TEXT);

    const utils = renderSystem();
    await utils.findAllByText("這是 owner 現在的版本");
    expect(utils.container.textContent).toContain("只有 API 知道這一段。");
    // The seed's own opening heading is nowhere on the page.
    const seedHeading = SEED_SYSTEM_INTERACTION_MD.split("\n")[0].replace(
      /^#+ /,
      ""
    );
    expect(utils.container.textContent).not.toContain(seedHeading);
    // …and it is not the default any more, which is the same claim said in the
    // cockpit's own vocabulary.
    expect(utils.queryByText(s.defaultBadge)).toBeNull();

    // The seed did not stop existing — it is what 還原出廠版 goes back to.
    expect(
      await api.getDocumentSeed("system_interaction", "global")
    ).toMatchObject({ content: { text: SEED_SYSTEM_INTERACTION_MD.trim() } });
  });

  it("states the three things a quiet page would let the owner misread", async () => {
    // Effective-from, retention-in-saves, and the limit. All three are shown
    // BEFORE the document and without waiting for it: a page whose read failed
    // is exactly when an owner most needs to know the factory restore is still
    // there and does not need anything to have loaded.
    const utils = renderSystem();
    const notes = utils.getByTestId("boot-doc-notes").textContent ?? "";
    expect(notes).toContain(s.bootDocNoteEffective);
    expect(notes).toContain(s.bootDocNoteCap);
    expect(notes).toContain("10");
    expect(notes).toContain("存檔次數");
    expect(utils.getByTestId("boot-doc-reset")).toBeTruthy();
  });

  it("warns about the silent boot failure before saving a boot sequence, and does not cry wolf on the system block", async () => {
    const claude = renderClaude();
    await claude.findByTestId("boot-doc-sec-0");
    await pasteSection(claude, 0, "# x\n\n");
    fireEvent.click(claude.getByTestId("boot-doc-save"));
    const bootConfirm = await claude.findByTestId("boot-doc-save-confirm");
    expect(bootConfirm.textContent).toContain(s.bootDocSaveConfirmBoot);
    claude.unmount();

    // The system-interaction block gets its OWN copy, because the boot-failure
    // sentence is not true of it — an agent with a mangled system block still
    // comes online. A warning that is false for the document on screen teaches
    // the reader to dismiss the one that is true.
    const system = renderSystem();
    await system.findByTestId("boot-doc-sec-0");
    await pasteSection(system, 0, "# y\n\n");
    fireEvent.click(system.getByTestId("boot-doc-save"));
    const systemConfirm = await system.findByTestId("boot-doc-save-confirm");
    expect(systemConfirm.textContent).toContain(s.bootDocSaveConfirmSystem);
    expect(systemConfirm.textContent).not.toContain(s.bootDocSaveConfirmBoot);
  });

  it("edits one section without disturbing the others", async () => {
    // The requirement behind the whole sectioned surface: the owner agrees with
    // some of a proposal and not the rest, so applying one block must leave
    // every other block exactly as it was.
    const utils = renderClaude();
    await utils.findByTestId("boot-doc-sec-0");
    const before = await api.getBootDoc("boot_sequence", "claude");
    const others = utils.getAllByTestId(/^boot-doc-sec-\d+$/).length;

    await pasteSection(utils, 1, "## 換掉的執行環境標題\n\n");
    expect(utils.getByTestId("boot-doc-sec-pending-1")).toBeTruthy();
    expect(utils.queryByTestId("boot-doc-sec-pending-0")).toBeNull();
    expect(utils.getAllByTestId(/^boot-doc-sec-\d+$/).length).toBe(others);

    // Nothing has been written yet — applying is not saving.
    expect(await api.getBootDoc("boot_sequence", "claude")).toEqual(before);

    // And it can be taken back one section at a time.
    fireEvent.click(utils.getByTestId("boot-doc-sec-discard-1"));
    expect(utils.queryByTestId("boot-doc-sec-pending-1")).toBeNull();
    expect(utils.queryByTestId("boot-doc-save-confirm")).toBeNull();
    expect((utils.getByTestId("boot-doc-save") as HTMLButtonElement).disabled).toBe(
      true
    );
  });

  it("previews a pasted section as rendered markdown before it is applied", async () => {
    // 「自己改、當場看結果」 — the reason the owner chose this direction over a
    // code change. The preview shows the block the way the agent will read it,
    // and it is reachable from inside the editor rather than after a save.
    const utils = renderClaude();
    await utils.findByTestId("boot-doc-sec-0");
    fireEvent.click(utils.getByTestId("boot-doc-sec-paste-0"));
    fireEvent.change(utils.getByTestId("boot-doc-sec-editor-0"), {
      target: { value: "# 預覽得到的標題\n" },
    });
    // Still raw text while editing…
    expect(utils.queryByText("預覽得到的標題")).toBeNull();

    fireEvent.click(utils.getByTestId("boot-doc-sec-preview-0"));
    expect(utils.queryByTestId("boot-doc-sec-editor-0")).toBeNull();
    expect(utils.getByText("預覽得到的標題").tagName).toBe("H1");
    // …and nothing was written to get there.
    expect(utils.queryByTestId("boot-doc-sec-pending-0")).toBeNull();
  });
});
