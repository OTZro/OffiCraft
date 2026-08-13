// Pins the boot-context block WIRE contract of the real-backend adapter
// (T-791e).
//
// 🔴 THIS FILE CARRIES MORE WEIGHT THAN ITS SIBLINGS, and the reason is worth
// stating: every other httpApi method rides the schema-typed openapi-fetch
// client, so a BE verb/path rename is a tsc error before anything runs. These
// three routes are not in the frozen spec yet (root CLAUDE.md §13 — the spec
// change needs owner sign-off), so they ride a bare fetch and get NO
// compile-time protection at all. What is asserted here is the only thing
// standing between a path typo and a runtime 404.
//
// The contract, shaped after /api/global-context:
//   GET  /api/system-interaction/{key}        — read
//   POST /api/system-interaction/{key}        — whole-document replace {text}
//   POST /api/system-interaction/{key}/reset  — restore the factory version
//   …and the same three under /api/boot-sequence/{key}.

import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { httpApi } from "./http";

const WIRE_DOC = {
  kind: "boot_sequence",
  key: "claude",
  text: "# 啟動程序",
  size_chars: 6,
  cap_chars: 15000,
  is_default: false,
  has_seed: true,
};

const fetchMock = vi.fn(
  async () =>
    new Response(JSON.stringify(WIRE_DOC), {
      status: 200,
      headers: { "Content-Type": "application/json" },
    })
);

beforeEach(() => {
  fetchMock.mockClear();
  vi.stubGlobal("fetch", fetchMock);
});

afterEach(() => {
  vi.unstubAllGlobals();
});

/** These three ride a BARE fetch (path, init) rather than the client's single
 * `Request`, so the call shape read here is different from its siblings'. */
function lastCall(): { url: string; method: string; body: string | undefined } {
  const calls = fetchMock.mock.calls as unknown as [string, RequestInit][];
  const [path, init] = calls[calls.length - 1];
  return {
    url: path,
    method: String(init.method),
    body: init.body === undefined ? undefined : String(init.body),
  };
}

describe("httpApi · boot-context block wire methods", () => {
  it("getBootDoc GETs the kind's own route with the key in the path", async () => {
    const view = await httpApi.getBootDoc("boot_sequence", "claude");
    expect(lastCall()).toMatchObject({
      url: "/api/boot-sequence/claude",
      method: "GET",
      body: undefined,
    });
    expect(view.text).toBe(WIRE_DOC.text);
    expect(view.capChars).toBe(15000);
    expect(view.hasSeed).toBe(true);
  });

  it("routes system_interaction to its OWN path, not the boot-sequence one", async () => {
    // The two kinds are separate document families with separate key spaces.
    // One shared path with the kind as a segment would work too — what must
    // never happen is one kind's read landing on the other's route, which is
    // exactly what a single hand-composed template string invites.
    await httpApi.getBootDoc("system_interaction", "global");
    expect(lastCall().url).toBe("/api/system-interaction/global");
  });

  it("saveBootDoc POSTs {text} to the document's path (NOT PUT)", async () => {
    await httpApi.saveBootDoc("boot_sequence", "codex", "新的內容");
    const { url, method, body } = lastCall();
    expect(url).toBe("/api/boot-sequence/codex");
    expect(method).toBe("POST");
    expect(JSON.parse(String(body))).toEqual({ text: "新的內容" });
  });

  it("resetBootDoc POSTs …/reset with no body (NOT DELETE on the doc path)", async () => {
    await httpApi.resetBootDoc("boot_sequence", "claude");
    const { url, method, body } = lastCall();
    expect(url).toBe("/api/boot-sequence/claude/reset");
    expect(method).toBe("POST");
    expect(body).toBeUndefined();
  });

  it("keeps the claude and codex keys apart across all three verbs", async () => {
    // The paired control for the ticket's headline risk: the two boot sequences
    // are different documents, so no verb may reach one while addressing the
    // other. Every URL below carries exactly the key it was handed.
    for (const key of ["claude", "codex"] as const) {
      const other = key === "claude" ? "codex" : "claude";
      await httpApi.getBootDoc("boot_sequence", key);
      expect(lastCall().url).toBe(`/api/boot-sequence/${key}`);
      expect(lastCall().url).not.toContain(other);
      await httpApi.saveBootDoc("boot_sequence", key, "x");
      expect(lastCall().url).toBe(`/api/boot-sequence/${key}`);
      expect(lastCall().url).not.toContain(other);
      await httpApi.resetBootDoc("boot_sequence", key);
      expect(lastCall().url).toBe(`/api/boot-sequence/${key}/reset`);
      expect(lastCall().url).not.toContain(other);
    }
  });

  it("throws the shared ApiError off the unified envelope, with .status", async () => {
    // Callers branch on `.status` (isHttpStatus), never on the message — so the
    // bare-fetch path has to reproduce what the client middleware gives every
    // other method rather than rejecting with a naked Error.
    fetchMock.mockResolvedValueOnce(
      new Response(
        JSON.stringify({
          error: { code: "bad_request", message: "over the character limit" },
        }),
        { status: 400, headers: { "Content-Type": "application/json" } }
      )
    );
    await expect(
      httpApi.saveBootDoc("boot_sequence", "claude", "x")
    ).rejects.toMatchObject({
      status: 400,
      code: "bad_request",
      serverMessage: "over the character limit",
    });
  });
});
