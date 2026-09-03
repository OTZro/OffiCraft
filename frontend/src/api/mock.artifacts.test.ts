// Mock adapter parity for the task artifact set (T-3dc5). The mock is the FE's
// dev/test server, so it must reproduce the real handler's OBSERVABLE effects:
// listTasks strips the artifact rows but keeps the count (the light-list badge
// source), getTask folds the full set, and removeTaskArtifact un-pins one row
// (404 on an unknown id) leaving the count consistent.

import { describe, it, expect, beforeEach } from "vitest";
import {
  mockApi,
  __resetMock,
  __injectMockTask,
  __injectMockArtifactVersions,
} from "./mock";
import type {
  TaskArtifactView,
  TaskArtifactVersionView,
  TaskView,
} from "./adapter";
import { ApiError } from "./errors";

function mkArtifact(over: Partial<TaskArtifactView>): TaskArtifactView {
  return {
    id: "ta-1",
    kind: "link",
    url: "https://x/pr/1",
    label: "PR #1",
    filename: "",
    mime: "",
    isImage: false,
    attachmentId: "",
    createdTs: 0,
    createdBy: "mira",
    versionCount: 1,
    ...over,
  };
}

function mkVersion(over: Partial<TaskArtifactVersionView>): TaskArtifactVersionView {
  return {
    id: 1,
    kind: "link",
    url: "https://x/pr/0",
    label: "PR #0",
    filename: "",
    attachmentId: "",
    createdTs: 0,
    createdBy: "mira",
    ...over,
  };
}

function mkTask(over: Partial<TaskView>): TaskView {
  return {
    id: "task-art",
    taskNo: "T-9001",
    title: "artifact task",
    typeKey: "",
    description: "",
    status: "in_progress",
    priority: "mid",
    executorKind: "member",
    executorId: "mira",
    creatorId: "owner",
    dedupeKey: "",
    deps: [],
    waitingReason: "",
    duplicateOf: "",
    createdTs: 1000,
    updatedTs: 2000,
    closedTs: null,
    progressDone: 0,
    progressTotal: 0,
    steps: [],
    artifacts: [],
    artifactCount: 0,
    ...over,
  };
}

beforeEach(() => __resetMock());

describe("mock task artifacts", () => {
  it("listTasks strips the artifact rows but keeps the count", async () => {
    __injectMockTask(
      mkTask({ artifacts: [mkArtifact({ id: "ta-1" }), mkArtifact({ id: "ta-2" })] }),
    );
    const rows = await mockApi.listTasks();
    const row = rows.find((t) => t.id === "task-art")!;
    expect(row.artifacts).toEqual([]);
    expect(row.artifactCount).toBe(2);
  });

  it("getTask folds the full artifact set with count == length", async () => {
    __injectMockTask(mkTask({ artifacts: [mkArtifact({ id: "ta-1" })] }));
    const full = await mockApi.getTask("task-art");
    expect(full.artifacts?.length).toBe(1);
    expect(full.artifactCount).toBe(1);
  });

  it("removeTaskArtifact un-pins one row and reports the fresh count", async () => {
    __injectMockTask(
      mkTask({ artifacts: [mkArtifact({ id: "ta-1" }), mkArtifact({ id: "ta-2" })] }),
    );
    await mockApi.removeTaskArtifact("task-art", "ta-1");
    // The write itself is a bounded receipt (T-a98d), so the fresh set is read
    // back the way the cockpit reads it.
    const after = await mockApi.getTask("task-art");
    expect(after.artifacts?.map((a) => a.id)).toEqual(["ta-2"]);
    expect(after.artifactCount).toBe(1);
  });

  it("removeTaskArtifact on an unknown artifact is a 404", async () => {
    __injectMockTask(mkTask({ artifacts: [mkArtifact({ id: "ta-1" })] }));
    await expect(
      mockApi.removeTaskArtifact("task-art", "ta-nope"),
    ).rejects.toMatchObject({ status: 404 } as Partial<ApiError>);
  });

  // T-2654 — the mock must REFUSE where production refuses. It used to delete
  // on a closed task while the server 409s, and the mock cockpit is how UI
  // changes get checked, so the parity gap made a broken flow look correct.
  // The 409 comes before the artifact lookup, same order as the server.
  it.each(["done", "terminated", "duplicated"] as const)(
    "removeTaskArtifact on a %s task is a 409 and leaves the artifact pinned",
    async (status) => {
      __injectMockTask(
        mkTask({ status, closedTs: 3000, artifacts: [mkArtifact({ id: "ta-1" })] }),
      );
      await expect(
        mockApi.removeTaskArtifact("task-art", "ta-1"),
      ).rejects.toMatchObject({ status: 409 } as Partial<ApiError>);
      const still = await mockApi.getTask("task-art");
      expect(still.artifacts?.map((a) => a.id)).toEqual(["ta-1"]);
    },
  );
});

describe("mock task artifact versions (T-60)", () => {
  it("lists the retained versions of a replaced deliverable, newest first", async () => {
    __injectMockTask(mkTask({ artifacts: [mkArtifact({ id: "ta-1", versionCount: 3 })] }));
    __injectMockArtifactVersions("ta-1", [
      mkVersion({ id: 2, url: "https://x/pr/2" }),
      mkVersion({ id: 1, url: "https://x/pr/1" }),
    ]);
    const versions = await mockApi.listTaskArtifactVersions("task-art", "ta-1");
    expect(versions.map((v) => v.id)).toEqual([2, 1]);
    expect(versions.map((v) => v.url)).toEqual(["https://x/pr/2", "https://x/pr/1"]);
  });

  it("answers an empty list for an artifact that was never replaced", async () => {
    __injectMockTask(mkTask({ artifacts: [mkArtifact({ id: "ta-1" })] }));
    expect(await mockApi.listTaskArtifactVersions("task-art", "ta-1")).toEqual([]);
  });

  it("is a 404 for an artifact that is not pinned on the task", async () => {
    __injectMockTask(mkTask({ artifacts: [mkArtifact({ id: "ta-1" })] }));
    await expect(
      mockApi.listTaskArtifactVersions("task-art", "ta-nope"),
    ).rejects.toMatchObject({ status: 404 } as Partial<ApiError>);
  });

  // Server parity: un-pinning deletes the versions in the same transaction, so
  // a version list can never outlive the artifact it belongs to.
  it("drops the retained versions when the artifact is un-pinned", async () => {
    __injectMockTask(mkTask({ artifacts: [mkArtifact({ id: "ta-1" }), mkArtifact({ id: "ta-2" })] }));
    __injectMockArtifactVersions("ta-1", [mkVersion({ id: 1 })]);
    await mockApi.removeTaskArtifact("task-art", "ta-1");
    __injectMockTask(mkTask({ id: "task-art-2", artifacts: [mkArtifact({ id: "ta-1" })] }));
    expect(await mockApi.listTaskArtifactVersions("task-art-2", "ta-1")).toEqual([]);
  });
});
