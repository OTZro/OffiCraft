// A TOMBSTONED revision reads, and above all DIFFS, as the shipped default
// (T-40f0 node 11 — owner caught this on screen, 2026-08-05).
//
// `tombstoned="true"` means "this document was following its shipped default";
// the row's text column is empty in the database because the text lives in the
// seed file. The cockpit read that empty column as literal content, so the diff
// pane against such a revision announced that EVERY line of the live document
// would be deleted — 「還原＝清空」 printed next to a destructive button, while
// `restoreDocumentHistory` actually writes the tombstone back and the fold puts
// the document ON the default.
//
// 🔴 WHY THESE FIXTURES AND NOT THE OBVIOUS ONES. Every pre-existing test of
// this behaviour used a `global_context` seed pseudo-version, and under THAT
// kind 「empty == the default」 is true by construction (`documentSeedContent`
// answers `{"text":"", "tombstoned":"true"}` for it). So the whole suite was
// pinning the bug's own premise as an invariant. The cases below therefore use
// a RETAINED revision (id 42, an author, a timestamp — not the seed row) of a
// FILE-SEEDED kind (`role_definition`, whose default is a real document), which
// is the only shape where the two can disagree. The global block keeps a case
// of its own further down, because its default really IS empty and that path
// must not regress.
//
// The criterion every assertion here serves: WHAT THE DIFF SAYS MUST EQUAL THE
// STATE A RESTORE LEAVES BEHIND. So the assertions are on the rows the diff
// actually draws — marker counts — not on whether some constant is referenced.

import { describe, it, expect, beforeEach, vi } from "vitest";
import { render, fireEvent, waitFor } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { zh } from "../i18n/locales/zh";
import { DocumentHistoryEntry } from "./DocumentHistoryEntry";
import { __resetMock, mockApi } from "../api/mock";
import type { DocumentHistoryView, DocumentKind } from "../types";

const s = zh.settings;

const REVISION_TS = 1753776180;

/** One RETAINED revision — an id, an author and a timestamp. Deliberately not
 * the 初始版本 pseudo-row: that row already carried the seed, so it cannot tell
 * a fixed reader from a broken one. */
function retained(content: Record<string, string>): DocumentHistoryView {
  return { id: 42, content, createdTs: REVISION_TS, actorId: "owner-1" };
}

async function openReader(opts: {
  kind: DocumentKind;
  docKey: string;
  revision: DocumentHistoryView;
  currentContent: Record<string, string>;
  /** A document that ships a default — the reset's presence is what grows the
   * 初始版本 row AND what makes the host fetch the seed at all. */
  hasSeed?: boolean;
}) {
  vi.spyOn(mockApi, "listDocumentHistory").mockResolvedValue([opts.revision]);
  const utils = render(
    <I18nProvider>
      <DocumentHistoryEntry
        kind={opts.kind}
        docKey={opts.docKey}
        title="版本紀錄"
        currentContent={opts.currentContent}
        onReset={opts.hasSeed === false ? undefined : async () => {}}
      />
    </I18nProvider>
  );
  fireEvent.click(utils.getByTestId(`doc-history-entry-${opts.kind}`));
  await utils.findByTestId("doc-history-list");
  await utils.findByTestId(`doc-history-item-${opts.revision.id}`);
  return utils;
}

/** The markers of the rows the diff actually drew: "-", "+" or a NBSP for an
 * unchanged line. Counting these is the only way to state "restoring this would
 * not wipe the document" in the terms the reader sees. */
function markers(container: HTMLElement): string[] {
  return Array.from(container.querySelectorAll(".diff-view__row")).map(
    (row) => (row.querySelectorAll("td")[2]?.textContent ?? "")
  );
}

beforeEach(() => {
  __resetMock();
  vi.restoreAllMocks();
});

describe("DocumentHistoryEntry · a tombstoned revision is the shipped default", () => {
  it("diffs a RETAINED tombstone against the live doc as the SEED, not as a wipe", async () => {
    // The document currently differs from its default by exactly one line. The
    // truthful diff therefore has one `-` and one `+`; the defect drew one `+`
    // per line of the whole document (and a lone `-` for the empty string),
    // i.e. "restoring this deletes everything you have".
    const seed = (await mockApi.getDocumentSeed("role_definition", "assistant"))
      .content.definition_md;
    const seedLines = seed.split("\n");
    expect(seedLines.length).toBeGreaterThan(2); // the fixture must be able to lie

    const utils = await openReader({
      kind: "role_definition",
      docKey: "assistant",
      // What the server stores for a reset: the field is EMPTY and the tombstone
      // is the whole message (api_roles.go → `RoleDef{RoleKey: role,
      // Tombstoned: true}`).
      revision: retained({ definition_md: "", tombstoned: "true" }),
      currentContent: {
        definition_md: ["owner 改寫的第一行", ...seedLines.slice(1)].join("\n"),
      },
    });

    fireEvent.click(utils.getByTestId("doc-history-open-42"));
    fireEvent.click(await utils.findByTestId("doc-history-pane-diff"));
    const drawn = await waitFor(() => {
      const found = markers(utils.container);
      expect(found.length).toBeGreaterThan(0);
      return found;
    });

    expect(drawn.filter((m) => m === "+")).toHaveLength(1);
    expect(drawn.filter((m) => m === "-")).toHaveLength(1);
    // …and the rest of the document is shown as UNTOUCHED, which is the half
    // that says "restoring this is safe".
    expect(drawn.filter((m) => m !== "+" && m !== "-").length).toBeGreaterThan(0);
  });

  it("says there is nothing to change when the doc already sits on its default", async () => {
    // The state right after a reset: restoring the tombstone is a no-op, and
    // the reader must say so rather than paint the document red.
    const seed = (await mockApi.getDocumentSeed("role_definition", "assistant"))
      .content.definition_md;
    const utils = await openReader({
      kind: "role_definition",
      docKey: "assistant",
      revision: retained({ definition_md: "", tombstoned: "true" }),
      currentContent: { definition_md: seed },
    });

    fireEvent.click(utils.getByTestId("doc-history-open-42"));
    fireEvent.click(await utils.findByTestId("doc-history-pane-diff"));

    expect((await utils.findByTestId("diff-view-empty")).textContent).toBe(
      zh.diff.noChanges
    );
    expect(markers(utils.container)).toHaveLength(0);
  });

  it("renders the default's own text in the content pane", async () => {
    const seed = (await mockApi.getDocumentSeed("role_definition", "assistant"))
      .content.definition_md;
    const utils = await openReader({
      kind: "role_definition",
      docKey: "assistant",
      revision: retained({ definition_md: "", tombstoned: "true" }),
      currentContent: { definition_md: "owner 的版本" },
    });

    fireEvent.click(utils.getByTestId("doc-history-open-42"));
    const body = (await utils.findByTestId("doc-history-modal")).querySelector(
      ".doc-hist-modal__body"
    ) as HTMLElement;
    // A distinctive phrase from the shipped default, not 「這個版本沒有內容」.
    await waitFor(() =>
      expect(body.textContent).toContain(seedPhrase(seed))
    );
    expect(body.textContent).not.toContain(s.historyModalEmpty);
  });

  it("says the row was on the DEFAULT, never that it was blank", async () => {
    // The list row's own line. 「（當時是空白內容）」 is a claim about content and
    // it is false here — the revision has content, just not its own.
    const utils = await openReader({
      kind: "role_definition",
      docKey: "assistant",
      revision: retained({ definition_md: "", tombstoned: "true" }),
      currentContent: { definition_md: "owner 的版本" },
    });
    const row = utils.getByTestId("doc-history-item-42");
    expect(row.textContent).toContain(s.historyDefaultContent);
    expect(row.textContent).not.toContain(s.historyNoContent);
  });

  // ── the reverse: a version that REALLY stored an empty string ──────────────
  // Without this, the fix above could quietly collapse two different states into
  // one and nothing would notice.
  it("still calls a genuinely blank revision blank", async () => {
    const utils = await openReader({
      kind: "global_context",
      docKey: "global",
      revision: retained({ text: "", tombstoned: "false" }),
      currentContent: { text: "owner 寫的區塊" },
    });

    const row = utils.getByTestId("doc-history-item-42");
    expect(row.textContent).toContain(s.historyNoContent);
    expect(row.textContent).not.toContain(s.historyDefaultContent);

    fireEvent.click(utils.getByTestId("doc-history-open-42"));
    const modal = await utils.findByTestId("doc-history-modal");
    await waitFor(() =>
      expect(modal.textContent).toContain(s.historyModalEmpty)
    );
    expect(modal.textContent).not.toContain(s.historyModalDefaultContent);
  });

  // ── global_context must not regress ────────────────────────────────────────
  // Its default IS the empty document (`documentSeedContent` says so), so the
  // substitution above changes nothing here — and that is worth pinning,
  // because this is the one kind where the old, wrong reading looked right.
  it("keeps telling the truth for the document whose default is empty", async () => {
    const utils = await openReader({
      kind: "global_context",
      docKey: "global",
      revision: retained({ text: "", tombstoned: "true" }),
      currentContent: { text: "owner 寫的區塊" },
    });

    // The row says 「當時採用出廠預設內容」 — which for this document happens to
    // also be empty, but the reason is the tombstone, not emptiness.
    expect(utils.getByTestId("doc-history-item-42").textContent).toContain(
      s.historyDefaultContent
    );

    fireEvent.click(utils.getByTestId("doc-history-open-42"));
    fireEvent.click(await utils.findByTestId("doc-history-pane-diff"));
    const drawn = await waitFor(() => {
      const found = markers(utils.container);
      expect(found.length).toBeGreaterThan(0);
      return found;
    });
    // Restoring it really WOULD take the owner's block away, and here saying so
    // is CORRECT — the whole document is one line, and the default it goes back
    // to is genuinely the empty one. Nothing above changed that.
    expect(drawn).toEqual(["+"]);
    const rows = utils.container.querySelectorAll(".diff-view__row");
    expect(rows[0]?.querySelectorAll("td")[3]?.textContent).toBe(
      "owner 寫的區塊"
    );
  });
});

/** A phrase from the shipped default that can only be on screen if the seed
 * itself is what got rendered. The markdown SYNTAX is stripped off, because the
 * content pane renders markdown — asserting on the raw `# ` would fail on a
 * correctly rendered heading. */
function seedPhrase(seed: string): string {
  const line = seed.split("\n").find((l) => l.trim() !== "") ?? "";
  return line.replace(/^[#\-*>\s]+/, "").trim();
}
