// The mock adapter's retained-revision path (T-7d33): the offline cockpit must
// exercise the same shape the server keeps — a write retains what it replaced,
// the list is newest-first and bounded at 3, and a restore puts the old content
// back over the live document (itself retaining what it overwrote).

import { describe, it, expect, beforeEach } from "vitest";
import { mockApi, __resetMock } from "./mock";
import { isHttpStatus } from "./errors";

beforeEach(() => {
  __resetMock();
});

describe("mockApi · document history", () => {
  it("has no revisions for a document nobody has edited", async () => {
    expect(await mockApi.listDocumentHistory("global_context", "global")).toEqual(
      []
    );
  });

  it("retains the state each write replaced, newest first", async () => {
    await mockApi.saveGlobalContext("first");
    await mockApi.saveGlobalContext("second");

    const versions = await mockApi.listDocumentHistory(
      "global_context",
      "global"
    );
    expect(versions.map((v) => v.content.text)).toEqual(["first", ""]);
    // The oldest one is what the doc looked like before it was ever edited —
    // the tombstone flag is why a restore of it can honestly go back to seed.
    expect(versions[1].content.tombstoned).toBe("true");
    expect(versions[0].actorId).toBeTruthy();
    expect(versions[0].createdTs).toBeGreaterThan(0);
  });

  it("keeps at most 3 revisions per document", async () => {
    for (const text of ["a", "b", "c", "d", "e"]) {
      await mockApi.saveGlobalContext(text);
    }
    const versions = await mockApi.listDocumentHistory(
      "global_context",
      "global"
    );
    expect(versions.map((v) => v.content.text)).toEqual(["d", "c", "b"]);
  });

  it("scopes history per document, not per kind", async () => {
    await mockApi.saveLessons("assistant", "general", "assistant learnings");
    expect(
      await mockApi.listDocumentHistory("lessons", "researcher::general")
    ).toEqual([]);
    expect(
      await mockApi.listDocumentHistory("lessons", "assistant::general")
    ).toHaveLength(1);
  });

  it("restore puts the old text back and retains what it overwrote", async () => {
    await mockApi.saveGlobalContext("original");
    await mockApi.saveGlobalContext("replacement");
    const [target] = await mockApi.listDocumentHistory(
      "global_context",
      "global"
    );
    expect(target.content.text).toBe("original");

    const restored = await mockApi.restoreDocumentHistory(
      "global_context",
      "global",
      target.id
    );
    expect(restored.id).toBe(target.id);
    expect((await mockApi.getGlobalContext()).text).toBe("original");

    const after = await mockApi.listDocumentHistory("global_context", "global");
    expect(after[0].content.text).toBe("replacement");
  });

  it("restoring a tombstoned revision puts the document back on its seed", async () => {
    await mockApi.saveRole("assistant", { definitionMd: "owner rewrite" });
    const [seedVersion] = await mockApi.listDocumentHistory(
      "role_definition",
      "assistant"
    );
    expect(seedVersion.content.tombstoned).toBe("true");

    await mockApi.restoreDocumentHistory(
      "role_definition",
      "assistant",
      seedVersion.id
    );
    const role = await mockApi.getRole("assistant");
    expect(role.isDefault).toBe(true);
    expect(role.definitionMd).toBe(seedVersion.content.definition_md);
  });

  it("restores a task manual's whole field set, fields included", async () => {
    const manual = await mockApi.createTaskManual("Review PR");
    await mockApi.updateTaskManual(manual.typeKey, {
      purpose: "review pull requests",
      sopMd: "## steps",
      learnings: "keep diffs small",
      fields: [{ name: "pr_url", required: true, isKey: true }],
    });
    await mockApi.updateTaskManual(manual.typeKey, {
      purpose: "",
      sopMd: "",
      learnings: "",
      fields: [],
    });

    const [previous] = await mockApi.listDocumentHistory(
      "task_manual",
      manual.typeKey
    );
    await mockApi.restoreDocumentHistory(
      "task_manual",
      manual.typeKey,
      previous.id
    );

    const back = await mockApi.getTaskManual(manual.typeKey);
    expect(back.purpose).toBe("review pull requests");
    expect(back.sopMd).toBe("## steps");
    expect(back.learnings).toBe("keep diffs small");
    expect(back.fields).toEqual([
      { name: "pr_url", required: true, isKey: true },
    ]);
  });

  it("rejects a revision id this document does not have", async () => {
    await mockApi.saveGlobalContext("only edit");
    await expect(
      mockApi.restoreDocumentHistory("global_context", "global", 9999)
    ).rejects.toSatisfy((e) => isHttpStatus(e, 404));
  });
});
