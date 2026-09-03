// TaskArtifactVersionsModal — reading a pinned deliverable's retained versions
// (T-60).
//
// 🔴 The load-bearing assertion is the one the document reader states as its
// only criterion: what the diff says must equal the actual state. Here that
// means the 「目前版本」 side is the artifact the SERVER hands back when the modal
// opens, never a row this client was already holding — the stale-cache case
// below hands the two different content and requires the server's to win.
//
// The rest pins the three shapes a "difference" takes, because they are three
// different screens rather than one screen with holes: two texts go to the
// shared DiffView, a link prints its old and new url, and anything else becomes
// a 前/後 toggle over one viewing area. A non-text response is never read as
// text — the body is dropped unread.

import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { render, screen, waitFor, fireEvent } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { api } from "../api";
import { TaskArtifactVersionsModal } from "./TaskArtifactVersionsModal";
import type {
  TaskArtifactView,
  TaskArtifactVersionView,
  TaskView,
} from "../api/adapter";

vi.mock("../api", () => ({
  api: {
    getTask: vi.fn(),
    listTaskArtifactVersions: vi.fn(),
    subscribeEvents: () => () => {},
  },
}));

const mockedApi = api as unknown as {
  getTask: ReturnType<typeof vi.fn>;
  listTaskArtifactVersions: ReturnType<typeof vi.fn>;
};

const realFetch = globalThis.fetch;

function mkArtifact(over: Partial<TaskArtifactView>): TaskArtifactView {
  return {
    id: "ta-1",
    kind: "file",
    url: "/api/chat/attachment/att-live",
    label: "spec.txt",
    filename: "spec.txt",
    mime: "text/plain",
    isImage: false,
    attachmentId: "att-live",
    createdTs: 2000,
    createdBy: "mira",
    versionCount: 2,
    ...over,
  };
}

function mkVersion(over: Partial<TaskArtifactVersionView>): TaskArtifactVersionView {
  return {
    id: 1,
    kind: "file",
    url: "/api/chat/attachment/att-old",
    label: "spec.txt",
    attachmentId: "att-old",
    createdTs: 1000,
    createdBy: "mira",
    ...over,
  };
}

function mkTask(artifacts: TaskArtifactView[]): TaskView {
  return { id: "t-1", artifacts, artifactCount: artifacts.length } as TaskView;
}

/** A fetch that answers per blob path with a declared content type. `cancel`
 * records that a body was dropped unread; `text` records that one was read. */
function stubFetch(blobs: Record<string, { mime: string; text?: string }>) {
  const cancel = vi.fn(async () => {});
  const readText = vi.fn();
  globalThis.fetch = vi.fn(async (input: RequestInfo | URL) => {
    const path = String(input).split("?")[0]!;
    const blob = blobs[path];
    if (!blob) {
      return { ok: false, status: 404, headers: new Headers() } as unknown as Response;
    }
    return {
      ok: true,
      status: 200,
      headers: new Headers({ "content-type": blob.mime }),
      text: async () => {
        readText(path);
        return blob.text ?? "";
      },
      body: { cancel },
    } as unknown as Response;
  }) as unknown as typeof fetch;
  return { cancel, readText };
}

function openModal() {
  return render(
    <I18nProvider>
      <TaskArtifactVersionsModal taskId="t-1" artifactId="ta-1" onClose={() => {}} />
    </I18nProvider>,
  );
}

/** The rendered diff's text cells, in order — the comparison as DATA, not as
 * a keyword search over the panel's prose. */
function diffLinesOnScreen(): string[] {
  return [...screen.getByTestId("ta-versions-diff").querySelectorAll(".diff-view__text")]
    .map((cell) => cell.textContent ?? "")
    // The unified view renders one text cell per row; a blank line is an NBSP.
    .map((s) => s.replace(/\u00a0/g, ""));
}

beforeEach(() => {
  vi.clearAllMocks();
});

afterEach(() => {
  globalThis.fetch = realFetch;
});

describe("TaskArtifactVersionsModal", () => {
  it("diffs against the artifact the SERVER holds, not a row handed in from elsewhere", async () => {
    // The task read is the ONLY source of the 「目前版本」 side. If this modal ever
    // learns to accept the popover's artifact row instead, this stays the
    // assertion that reddens: the popover's row is stale by construction.
    mockedApi.listTaskArtifactVersions.mockResolvedValue([mkVersion({})]);
    mockedApi.getTask.mockResolvedValue(
      mkTask([mkArtifact({ url: "/api/chat/attachment/att-fresh" })]),
    );
    stubFetch({
      "/api/chat/attachment/att-old": { mime: "text/plain", text: "one\n" },
      "/api/chat/attachment/att-fresh": { mime: "text/plain", text: "two\n" },
      "/api/chat/attachment/att-live": { mime: "text/plain", text: "STALE\n" },
    });
    openModal();

    fireEvent.click(await screen.findByTestId("ta-versions-pane-diff"));
    await waitFor(() => expect(screen.getByTestId("ta-versions-diff")).toBeTruthy());
    expect(diffLinesOnScreen()).toEqual(["one", "two"]);
    expect(mockedApi.getTask).toHaveBeenCalledWith("t-1");
  });

  it("says the artifact is gone rather than diffing against the last thing it knew", async () => {
    mockedApi.listTaskArtifactVersions.mockResolvedValue([mkVersion({})]);
    mockedApi.getTask.mockResolvedValue(mkTask([]));
    stubFetch({ "/api/chat/attachment/att-old": { mime: "text/plain", text: "one\n" } });
    openModal();

    fireEvent.click(await screen.findByTestId("ta-versions-pane-diff"));
    await waitFor(() => expect(screen.getByTestId("ta-versions-unpinned")).toBeTruthy());
    expect(screen.queryByTestId("ta-versions-diff")).toBeNull();
  });

  it("compares two text files through the shared DiffView", async () => {
    mockedApi.listTaskArtifactVersions.mockResolvedValue([mkVersion({})]);
    mockedApi.getTask.mockResolvedValue(mkTask([mkArtifact({})]));
    stubFetch({
      "/api/chat/attachment/att-old": { mime: "text/plain", text: "alpha\nbeta\n" },
      "/api/chat/attachment/att-live": { mime: "text/plain", text: "alpha\ngamma\n" },
    });
    openModal();

    fireEvent.click(await screen.findByTestId("ta-versions-pane-diff"));
    await waitFor(() => expect(screen.getByTestId("ta-versions-diff")).toBeTruthy());
    expect(diffLinesOnScreen()).toEqual(["alpha", "beta", "gamma"]);
  });

  it("prints the old url and the new one for a link artifact", async () => {
    mockedApi.listTaskArtifactVersions.mockResolvedValue([
      mkVersion({ kind: "link", url: "https://x/pr/1", attachmentId: "" }),
    ]);
    mockedApi.getTask.mockResolvedValue(
      mkTask([
        mkArtifact({
          kind: "link",
          url: "https://x/pr/2",
          attachmentId: "",
          mime: "",
          filename: "",
        }),
      ]),
    );
    const { cancel } = stubFetch({});
    openModal();

    fireEvent.click(await screen.findByTestId("ta-versions-pane-diff"));
    await waitFor(() => expect(screen.getByTestId("ta-versions-diff-urls")).toBeTruthy());
    expect(screen.getByTestId("ta-versions-before-link").textContent).toBe("https://x/pr/1");
    expect(screen.getByTestId("ta-versions-after-link").textContent).toBe("https://x/pr/2");
    // A link is not a blob: nothing was fetched to answer this.
    expect(globalThis.fetch).not.toHaveBeenCalled();
    expect(cancel).not.toHaveBeenCalled();
  });

  it("gives a non-text file a before/after toggle and never reads its bytes as text", async () => {
    mockedApi.listTaskArtifactVersions.mockResolvedValue([mkVersion({})]);
    mockedApi.getTask.mockResolvedValue(mkTask([mkArtifact({})]));
    const { cancel, readText } = stubFetch({
      "/api/chat/attachment/att-old": { mime: "application/pdf" },
      "/api/chat/attachment/att-live": { mime: "application/pdf" },
    });
    openModal();

    fireEvent.click(await screen.findByTestId("ta-versions-pane-diff"));
    await waitFor(() => expect(screen.getByTestId("ta-versions-diff-sides")).toBeTruthy());
    expect(screen.getByTestId("ta-versions-before-opaque")).toBeTruthy();
    expect(screen.queryByTestId("ta-versions-diff")).toBeNull();

    fireEvent.click(screen.getByTestId("ta-versions-side-after"));
    expect(screen.getByTestId("ta-versions-after-opaque")).toBeTruthy();

    expect(readText).not.toHaveBeenCalled();
    expect(cancel).toHaveBeenCalled();
  });

  it("shows an image version as an image on both sides of the toggle", async () => {
    mockedApi.listTaskArtifactVersions.mockResolvedValue([
      mkVersion({ kind: "image", url: "/api/chat/attachment/att-shot1" }),
    ]);
    mockedApi.getTask.mockResolvedValue(
      mkTask([
        mkArtifact({
          kind: "image",
          isImage: true,
          mime: "image/png",
          url: "/api/chat/attachment/att-shot2",
        }),
      ]),
    );
    stubFetch({});
    openModal();

    fireEvent.click(await screen.findByTestId("ta-versions-pane-diff"));
    await waitFor(() => expect(screen.getByTestId("ta-versions-before-image")).toBeTruthy());
    fireEvent.click(screen.getByTestId("ta-versions-side-after"));
    expect(
      screen.getByTestId("ta-versions-after-image").getAttribute("src"),
    ).toBe("/api/chat/attachment/att-shot2");
    // An image is displayed by the browser; this panel does not fetch it itself.
    expect(globalThis.fetch).not.toHaveBeenCalled();
  });

  it("selects the newest retained version first and can switch to the current one", async () => {
    mockedApi.listTaskArtifactVersions.mockResolvedValue([
      mkVersion({ id: 2, url: "/api/chat/attachment/att-v2" }),
      mkVersion({ id: 1, url: "/api/chat/attachment/att-v1" }),
    ]);
    mockedApi.getTask.mockResolvedValue(mkTask([mkArtifact({})]));
    stubFetch({
      "/api/chat/attachment/att-v2": { mime: "text/plain", text: "v2" },
      "/api/chat/attachment/att-v1": { mime: "text/plain", text: "v1" },
      "/api/chat/attachment/att-live": { mime: "text/plain", text: "live" },
    });
    openModal();

    await waitFor(() =>
      expect(screen.getByTestId("ta-versions-content-text").textContent).toBe("v2"),
    );
    fireEvent.click(screen.getByTestId("ta-versions-row-1"));
    await waitFor(() =>
      expect(screen.getByTestId("ta-versions-content-text").textContent).toBe("v1"),
    );
    fireEvent.click(screen.getByTestId("ta-versions-row-live"));
    await waitFor(() =>
      expect(screen.getByTestId("ta-versions-content-text").textContent).toBe("live"),
    );
    // The current version has nothing to be compared with — and says so instead
    // of diffing it against itself.
    fireEvent.click(screen.getByTestId("ta-versions-pane-diff"));
    expect(screen.getByTestId("ta-versions-diff-live")).toBeTruthy();
  });

  it("says the version history could not be read rather than showing an empty list", async () => {
    mockedApi.listTaskArtifactVersions.mockRejectedValue(new Error("boom"));
    mockedApi.getTask.mockResolvedValue(mkTask([mkArtifact({})]));
    stubFetch({});
    vi.spyOn(console, "warn").mockImplementation(() => {});
    openModal();

    await waitFor(() => expect(screen.getByTestId("ta-versions-load-error")).toBeTruthy());
    expect(screen.queryByTestId("ta-versions-empty")).toBeNull();
  });
});
