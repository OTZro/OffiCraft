// api/diff.ts — the ONE place that speaks GET /api/diff.
//
// T-59: the compare screen asks for both sides in a SINGLE request, and the
// server answers each side's resolved text, the heading to put over its column,
// and an honest "this side is gone" marker when the address resolves to nothing
// (a pruned revision, a reclaimed blob, a field the document no longer carries).
// The reader resolves nothing itself — one round trip, one authority.
//
// One route for the PAIR is also the security shape, not a convenience: the
// ?sig= credential signs exactly what one request returns, so a holder of an
// external link cannot swap an address or relabel a column and still present a
// server-minted signature.
//
// PERMANENTLY HAND-WRITTEN, joining auth.ts's login() on http.ts's list of calls
// that deliberately skip the typed openapi-fetch client. The reason is the
// client's auth middleware, which is exactly wrong for THIS route: it turns any
// 401 into "the session died" (clear the token, fire oc-auth-expired). The
// signed flavour of this route is answered WITHOUT a session, so a bad ?sig=
// would log the owner out of the studio over someone else's stale link, and log
// out a tab that never had a session at all. A 401 is bounced to the auth layer
// ONLY when the call was made as the session (no ?sig=), which is when it means
// what it says.
//
// Skipping the typed CLIENT is not skipping the typed CONTRACT: the wire types
// below are the GENERATED ones, so a DTO renamed in spec/openapi.json is a tsc
// error here exactly as it is everywhere else.

import type { DiffPairView, DiffSideView } from "../types";
import type { DiffParams } from "../lib/diffLink";
import type { components } from "./generated/schema";
import { ownerToken } from "./auth";
import { handleUnauthorized } from "./client";
import { ApiError } from "./errors";

type WireDiffSide = components["schemas"]["DiffSideDTO"];
type WireDiffPair = components["schemas"]["DiffPairDTO"];

function toDiffSide(w: WireDiffSide): DiffSideView {
  return {
    address: w.address,
    // A GONE side carries no text, and "" is not a text either: drawing "" against
    // the other side would mark every one of its lines as added — a confident
    // wrong answer to "what changed". The screen branches on `gone` before it
    // draws anything, so this default is never what gets compared.
    text: w.text ?? "",
    // The server sends "" for a side the url gave no heading — deliberately, so
    // that the READER names a document side in its own language rather than
    // inheriting one language's label from mint time. Empty and absent are
    // therefore the same fact here, and both mean "use your own words".
    label: w.label ? w.label : undefined,
    gone: w.gone,
    goneReason: w.gone_reason ? w.gone_reason : undefined,
  };
}

export function toDiffPair(w: WireDiffPair): DiffPairView {
  return { before: toDiffSide(w.before), after: toDiffSide(w.after) };
}

/** The query this call puts on the wire. */
export function diffQuery(params: DiffParams): string {
  const q = new URLSearchParams();
  q.set("before", params.before);
  q.set("after", params.after);
  if (params.labelBefore) q.set("label_before", params.labelBefore);
  if (params.labelAfter) q.set("label_after", params.labelAfter);
  if (params.sig) q.set("sig", params.sig);
  return q.toString();
}

export async function fetchDiffPair(params: DiffParams): Promise<DiffPairView> {
  const headers: Record<string, string> = { Accept: "application/json" };
  // The signature is the credential in the external flavour; the session token
  // is the credential in the internal one. Sending the token when there is one
  // is right in both — a present-but-invalid bearer credential stays a 401 and
  // never falls through to the sig, which is the server's rule, not ours.
  const token = ownerToken();
  if (token) headers.Authorization = `Bearer ${token}`;

  const res = await fetch(`/api/diff?${diffQuery(params)}`, { headers });
  if (!res.ok) {
    let code = "";
    let serverMessage = "";
    try {
      const body: unknown = await res.json();
      const err = (body as { error?: { code?: unknown; message?: unknown } })?.error;
      if (typeof err?.code === "string") code = err.code;
      if (typeof err?.message === "string") serverMessage = err.message;
    } catch {
      // Not JSON (a proxy error page) — keep the honest empties.
    }
    // See the header: only a SESSION call's 401 is a dead session.
    if (res.status === 401 && params.sig === undefined) handleUnauthorized();
    throw new ApiError(
      `http ${res.status} for GET /api/diff`,
      res.status,
      code,
      serverMessage,
    );
  }
  return toDiffPair((await res.json()) as WireDiffPair);
}
