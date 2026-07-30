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
  it("has no revisions for a document nobody has edited, nor after its first write", async () => {
    expect(await mockApi.listDocumentHistory("global_context", "global")).toEqual(
      []
    );

    // The product boundary: a seed/default document has no previous version, so
    // the FIRST customization replaces nothing and retains nothing. The server
    // skips the empty snapshot (dal.go); a synthesized "was default" revision
    // here would show the cockpit a version the real server never kept.
    await mockApi.saveGlobalContext("first customization");
    await mockApi.saveRole("assistant", { definitionMd: "first rewrite" });
    await mockApi.saveLessons("assistant", "general", "first learnings");

    expect(
      await mockApi.listDocumentHistory("global_context", "global")
    ).toEqual([]);
    expect(
      await mockApi.listDocumentHistory("role_definition", "assistant")
    ).toEqual([]);
    expect(
      await mockApi.listDocumentHistory("lessons", "assistant::general")
    ).toEqual([]);
  });

  it("retains the state each write replaced, newest first", async () => {
    await mockApi.saveGlobalContext("first");
    await mockApi.saveGlobalContext("second");
    await mockApi.saveGlobalContext("third");
    // A reset is a write too — it persists a tombstoned row, which the write
    // after it retains.
    await mockApi.resetGlobalContext();
    await mockApi.saveGlobalContext("fourth");

    const versions = await mockApi.listDocumentHistory(
      "global_context",
      "global"
    );
    expect(versions.map((v) => v.content.text)).toEqual(["", "third", "second"]);
    // The newest one is the doc as the reset left it — the tombstone flag is
    // why a restore of it can honestly go back to seed.
    expect(versions[0].content.tombstoned).toBe("true");
    expect(versions[1].content.tombstoned).toBe("false");
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
    await mockApi.saveLessons("assistant", "general", "assistant learnings v2");
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
    // The reset puts the role back on its seed (a tombstoned row); the write
    // after it is what retains that state as a revision.
    await mockApi.resetRole("assistant");
    await mockApi.saveRole("assistant", { definitionMd: "second rewrite" });
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

  // Deleting a document takes its retained revisions with it, in the same
  // transaction (dal.go DeleteRoleDef / DeleteLessonsOfRole / DeleteTaskManual):
  // history is readable by any authenticated caller, so a leftover revision is a
  // readable echo of a deleted document and makes the guide's 「永久移除」 false.
  // No live cockpit path reaches a stale row today — role keys and manual
  // type_keys are randomly minted, so a deleted key is never seen again — but
  // the mock is the cockpit's stand-in for the contract: one that still lists
  // history for a deleted document teaches the UI, and the next reader of this
  // file, a behaviour the server does not have.
  it("deleting a role drops its own history and the history of ALL its lessons", async () => {
    const { role } = await mockApi.createRole({ name: "臨時角色" });
    await mockApi.saveRole(role.key, { definitionMd: "改寫" });
    // TWO task types: the lessons history key is compound (`<role>::<type>`),
    // so a delete that only matched one exact key would leave the other behind.
    for (const taskType of ["general", "planning"]) {
      await mockApi.saveLessons(role.key, taskType, "第一版");
      await mockApi.saveLessons(role.key, taskType, "第二版");
      expect(
        await mockApi.listDocumentHistory("lessons", `${role.key}::${taskType}`)
      ).toHaveLength(1);
    }
    expect(
      await mockApi.listDocumentHistory("role_definition", role.key)
    ).toHaveLength(1);

    await mockApi.deleteRole(role.key);

    expect(
      await mockApi.listDocumentHistory("role_definition", role.key)
    ).toEqual([]);
    expect(
      await mockApi.listDocumentHistory("lessons", `${role.key}::general`)
    ).toEqual([]);
    expect(
      await mockApi.listDocumentHistory("lessons", `${role.key}::planning`)
    ).toEqual([]);
  });

  it("deleting a task manual drops its history", async () => {
    const manual = await mockApi.createTaskManual("Review PR");
    await mockApi.updateTaskManual(manual.typeKey, { purpose: "第一版" });
    await mockApi.updateTaskManual(manual.typeKey, { purpose: "第二版" });
    expect(
      await mockApi.listDocumentHistory("task_manual", manual.typeKey)
    ).toHaveLength(2);

    await mockApi.deleteTaskManual(manual.typeKey);

    expect(
      await mockApi.listDocumentHistory("task_manual", manual.typeKey)
    ).toEqual([]);
  });

  it("rejects a revision id this document does not have", async () => {
    await mockApi.saveGlobalContext("only edit");
    await expect(
      mockApi.restoreDocumentHistory("global_context", "global", 9999)
    ).rejects.toSatisfy((e) => isHttpStatus(e, 404));
  });
});
