// The compare read (`GET /api/diff`, T-59) is one of the few calls that skip the
// typed openapi-fetch client, so the cross-cutting behaviour that client owns
// has to be pinned HERE instead of inherited:
//
//   1. the query it puts on the wire, including the two optional labels and the
//      signature;
//   2. the owner token rides as `Authorization` when there is one;
//   3. a SIGNED call's 401 must NOT clear the session — the signature failed,
//      not the login, and a reader following a link may have no session to lose;
//      an unsigned call's 401 must still bounce to the auth layer;
//   4. every non-2xx becomes an ApiError carrying the unified error envelope;
//   5. a side the server marks `gone` survives the mapping as `gone`, not as an
//      empty text — the compare screen branches on exactly that.

import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { httpApi } from "./http";
import { ApiError } from "./errors";
import { codeForStatus } from "./errorCodes";
import { AUTH_EXPIRED_EVENT } from "./client";
import { TOKEN_KEY } from "./auth";

const PARAMS = {
  before: "att-0123456789ab",
  after: "doc:global_context/global/current/text",
};

const BODY = {
  before: { address: "att-0123456789ab", text: "alpha", label: "改動前", gone: false },
  after: { address: "doc:x", text: "beta", label: "改動後", gone: false },
};

function stubFetch(response: Response) {
  const fetchMock = vi.fn(async () => response.clone());
  vi.stubGlobal("fetch", fetchMock);
  return fetchMock;
}

const ok = (body: unknown) =>
  new Response(JSON.stringify(body), {
    status: 200,
    headers: { "Content-Type": "application/json" },
  });

const refused = (status: number) =>
  new Response(
    JSON.stringify({ error: { code: codeForStatus(status), message: "nope" } }),
    { status, headers: { "Content-Type": "application/json" } },
  );

beforeEach(() => {
  localStorage.clear();
});

afterEach(() => {
  vi.unstubAllGlobals();
  localStorage.clear();
});

describe("getDiff", () => {
  it("asks for both sides in one request, with the labels and signature the url carried", async () => {
    const fetchMock = stubFetch(ok(BODY));
    localStorage.setItem(TOKEN_KEY, "owner-token");

    await httpApi.getDiff({
      ...PARAMS,
      labelBefore: "改動前",
      labelAfter: "改動後",
      sig: "s+g/1",
    });

    expect(fetchMock).toHaveBeenCalledTimes(1);
    const [url, init] = fetchMock.mock.calls[0] as unknown as [string, RequestInit];
    const query = new URL(url, "http://localhost").searchParams;
    expect(query.get("before")).toBe(PARAMS.before);
    expect(query.get("after")).toBe(PARAMS.after);
    expect(query.get("label_before")).toBe("改動前");
    expect(query.get("label_after")).toBe("改動後");
    expect(query.get("sig")).toBe("s+g/1");
    expect((init.headers as Record<string, string>).Authorization).toBe(
      "Bearer owner-token",
    );
  });

  it("keeps a GONE side gone instead of comparing against an empty text", async () => {
    stubFetch(
      ok({
        before: { address: PARAMS.before, gone: true, gone_reason: "pruned" },
        after: { address: PARAMS.after, text: "beta", label: "", gone: false },
      }),
    );
    const pair = await httpApi.getDiff(PARAMS);
    expect(pair.before.gone).toBe(true);
    expect(pair.before.goneReason).toBe("pruned");
    // An EMPTY label is the server saying "the url gave none" — it must reach
    // the screen as absent, so the reader writes its own heading rather than
    // drawing a blank one.
    expect(pair.after).toEqual({
      address: PARAMS.after,
      text: "beta",
      label: undefined,
      gone: false,
      goneReason: undefined,
    });
  });

  it("rejects with the server's error envelope", async () => {
    stubFetch(refused(404));
    await expect(httpApi.getDiff(PARAMS)).rejects.toBeInstanceOf(ApiError);
    stubFetch(refused(404));
    await expect(httpApi.getDiff(PARAMS)).rejects.toMatchObject({
      status: 404,
      code: codeForStatus(404),
      serverMessage: "nope",
    });
  });

  it("does NOT end the session when a SIGNED link is refused", async () => {
    stubFetch(refused(401));
    localStorage.setItem(TOKEN_KEY, "owner-token");
    const expired = vi.fn();
    window.addEventListener(AUTH_EXPIRED_EVENT, expired);

    await expect(httpApi.getDiff({ ...PARAMS, sig: "bad" })).rejects.toBeInstanceOf(
      ApiError,
    );

    window.removeEventListener(AUTH_EXPIRED_EVENT, expired);
    // The SIGNATURE was refused. Logging the owner out of a tab over someone
    // else's stale link — or out of a tab that never had a session — would be
    // blaming the wrong credential.
    expect(expired).not.toHaveBeenCalled();
    expect(localStorage.getItem(TOKEN_KEY)).toBe("owner-token");
  });

  it("DOES end the session when the same 401 answers a session call", async () => {
    stubFetch(refused(401));
    localStorage.setItem(TOKEN_KEY, "owner-token");
    const expired = vi.fn();
    window.addEventListener(AUTH_EXPIRED_EVENT, expired);

    await expect(httpApi.getDiff(PARAMS)).rejects.toBeInstanceOf(ApiError);

    window.removeEventListener(AUTH_EXPIRED_EVENT, expired);
    expect(expired).toHaveBeenCalledTimes(1);
    expect(localStorage.getItem(TOKEN_KEY)).toBeNull();
  });
});
