// Pins the /api/document-history WIRE contract of the real-backend adapter
// (T-7d33). The frozen route surface (spec/openapi.json) registers:
//   GET  /api/document-history/{kind}/{key}              — retained revisions
//   POST /api/document-history/{kind}/{key}/{id}/restore — restore one
//
// The two things that can silently drift here are the METHOD (a restore is a
// POST on its own sub-path, not a PUT on the revision) and how the composite
// lessons key "<role_key>::<task_type>" is placed — it is ONE path segment, so
// a naive split would address a route that does not exist.

import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { httpApi } from "./http";

const WIRE_VERSION = {
  id: 7,
  content: { text: "an earlier draft", tombstoned: "false" },
  created_ts: 1_753_000_000,
  actor_id: "owner",
};

let body: unknown = [WIRE_VERSION];

const fetchMock = vi.fn(
  async () =>
    new Response(JSON.stringify(body), {
      status: 200,
      headers: { "Content-Type": "application/json" },
    })
);

beforeEach(() => {
  body = [WIRE_VERSION];
  fetchMock.mockClear();
  vi.stubGlobal("fetch", fetchMock);
});

afterEach(() => {
  vi.unstubAllGlobals();
});

/** Normalise the last fetch call — every httpApi method rides the
 * openapi-fetch client, which calls fetch with ONE `Request` argument. */
async function lastCall(): Promise<{
  url: string;
  method: string;
  body: string | undefined;
}> {
  const calls = fetchMock.mock.calls as unknown as [Request][];
  const req = calls[calls.length - 1][0];
  const u = new URL(req.url);
  return {
    url: u.pathname + u.search,
    method: req.method,
    body: (await req.clone().text()) || undefined,
  };
}

describe("httpApi · document-history wire methods", () => {
  it("listDocumentHistory GETs the kind/key path and maps snake→camel", async () => {
    const versions = await httpApi.listDocumentHistory(
      "global_context",
      "global"
    );
    const { url, method } = await lastCall();
    expect(url).toBe("/api/document-history/global_context/global");
    expect(method).toBe("GET");
    expect(versions).toEqual([
      {
        id: 7,
        content: { text: "an earlier draft", tombstoned: "false" },
        createdTs: 1_753_000_000,
        actorId: "owner",
      },
    ]);
  });

  it("listDocumentHistory keeps a lessons composite key in one path segment", async () => {
    await httpApi.listDocumentHistory("lessons", "assistant::general");
    const { url } = await lastCall();
    // "::" is not a path separator here — the server splits the key itself
    // (historyKeyParts), so the cockpit must not split it into two segments.
    expect(decodeURIComponent(url)).toBe(
      "/api/document-history/lessons/assistant::general"
    );
  });

  it("restoreDocumentHistory POSTs the /{id}/restore sub-path with no body", async () => {
    body = WIRE_VERSION;
    const restored = await httpApi.restoreDocumentHistory(
      "task_manual",
      "tm-abc123",
      7
    );
    const { url, method, body: sent } = await lastCall();
    expect(url).toBe("/api/document-history/task_manual/tm-abc123/7/restore");
    expect(method).toBe("POST");
    expect(sent).toBeUndefined();
    expect(restored.id).toBe(7);
  });
});
