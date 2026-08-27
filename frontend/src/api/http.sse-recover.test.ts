// Pins the recovery of the shared SSE downlink from a PERMANENT failure —
// the case the browser explicitly does NOT handle, and the one nothing in this
// app could see.
//
// The shape of the bug this file exists to keep dead:
//   `EventSource` retries by itself only while it can. On a non-200 response, a
//   401, or a wrong `Content-Type` the spec has it FAIL the connection and move
//   it to CLOSED — permanently, no retry, ever. The old code had no `onerror`
//   at all, so `sseSource` stayed non-null pointing at that corpse; every later
//   `ensureSseSource()` early-returned on it, `onopen` never fired again, and
//   the reconnect resync never ran. The cockpit froze, in silence, until a
//   manual reload. Owner's report (2026-08-21): 「有時候…要 refresh page 才會更
//   新」.
//
// Two things are therefore asserted TOGETHER throughout, and neither is
// sufficient alone:
//   (1) it comes BACK — a new connection is built; and
//   (2) coming back REFETCHES — the rebuilt connection's first open fans the
//       full resync, because the stream has no replay (spec/sse.md §2.1) and
//       everything emitted during the outage is otherwise lost forever.
// A "fix" that satisfies (1) without (2) is strictly worse than the bug: it
// replaces a visible stall with an invisible hole.

// MEASURED MUTANTS — every guard below was made to fail on purpose, and each
// one is re-runnable by hand: apply the edit to api/http.ts, run this file, put
// it back. The named assertion in the right column is the ONE that reddened
// (measured 2026-08-27; the sweep was run with each file's own afterEach reset
// in place, so these are single-assertion attributions, not cascades).
//
//   the edit                                          | the assertion that catches it
//   --------------------------------------------------|------------------------------
//   delete the whole `es.onerror = …` handler          | "a permanently CLOSED stream is REBUILT"
//     (this IS the original bug)                       |
//   `let opened = opts?.reconnect === true`  →  `false`| "the rebuilt connection's FIRST open fans a FULL resync"
//   `if (status === 401 || status === 403)`  →  `if (false)` | "401 STOPS the retry loop…"
//   `if (es.readyState !== 2 …)`             →  `if (false)` | "a TRANSIENT error … does NOT tear the connection down"
//   `const idx = Math.min(sseRetryAttempt, …)` → `= 0`  | "repeated failures BACK OFF"
//   drop `&& sseVisibilityHandler === null`            | "rebuilding does NOT stack a second foreground listener"
//   restore `&& sseSource` on the last-unsubscribe test| "the LAST unsubscribe during the retry window cancels the pending retry"

import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import {
  httpApi,
  SSE_RESYNC_TOPICS,
  sseConnectionState,
  __resetSseDownlinkForTests,
} from "./http";
import { AUTH_EXPIRED_EVENT } from "./client";
import { TOKEN_KEY } from "./auth";

const CONNECTING = 1;
const CLOSED = 2;

class FakeEventSource {
  static instances: FakeEventSource[] = [];
  url: string;
  closed = false;
  readyState = CONNECTING;
  onmessage: ((e: MessageEvent) => void) | null = null;
  onopen: (() => void) | null = null;
  onerror: (() => void) | null = null;
  constructor(url: string) {
    this.url = url;
    FakeEventSource.instances.push(this);
  }
  close(): void {
    this.closed = true;
    this.readyState = CLOSED;
  }
  /** The browser's native `open` event. */
  open(): void {
    this.readyState = 0; // OPEN
    this.onopen?.();
  }
  /** A TRANSIENT drop: the browser is retrying this same connection itself. */
  transientError(): void {
    this.readyState = CONNECTING;
    this.onerror?.();
  }
  /** A PERMANENT failure: the browser has given up on this connection for good
   * (non-200 / 401 / wrong content-type). Nothing will reopen it. */
  permanentError(): void {
    this.readyState = CLOSED;
    this.onerror?.();
  }
  emit(data: unknown): void {
    this.onmessage?.({ data: JSON.stringify(data) } as MessageEvent);
  }
}

/** The `/api/events` status the auth probe will read on the next retry. */
let probeStatus = 200;
let probeCalls = 0;
/** Set to make the probe reject (offline: no answer at all, NOT a 401). */
let probeThrows = false;

beforeEach(() => {
  FakeEventSource.instances = [];
  probeStatus = 200;
  probeCalls = 0;
  probeThrows = false;
  vi.stubGlobal("EventSource", FakeEventSource);
  vi.stubGlobal("fetch", (url: string) => {
    probeCalls += 1;
    expect(String(url)).toContain("/api/events?token=");
    if (probeThrows) return Promise.reject(new Error("offline"));
    return Promise.resolve({ status: probeStatus } as Response);
  });
  vi.useFakeTimers();
  localStorage.setItem(TOKEN_KEY, "test-owner-jwt");
});

afterEach(() => {
  // The downlink is module-level singleton state. A test that fails part way
  // through leaves a live fake connection and an armed retry behind, and every
  // later test then measures that debris — which is how one real failure turns
  // into a dozen fake ones and a mutant sweep stops naming the guard that
  // actually caught it.
  __resetSseDownlinkForTests();
  vi.useRealTimers();
  vi.unstubAllGlobals();
  localStorage.removeItem(TOKEN_KEY);
});

/** Run the pending retry timer AND let the probe's promise chain settle. */
async function runRetry(): Promise<void> {
  await vi.runOnlyPendingTimersAsync();
  await vi.runOnlyPendingTimersAsync();
}

const latest = () => FakeEventSource.instances[FakeEventSource.instances.length - 1];

describe("httpApi · SSE downlink recovery from a PERMANENT failure", () => {
  it("a permanently CLOSED stream is REBUILT — the old code left the corpse in place and never reconnected", async () => {
    const off = httpApi.subscribeEvents(() => {});
    expect(FakeEventSource.instances).toHaveLength(1);
    const dead = FakeEventSource.instances[0];
    dead.open();

    dead.permanentError();
    // Nothing is retried synchronously — the backoff owns the timing.
    expect(FakeEventSource.instances).toHaveLength(1);

    await runRetry();

    expect(FakeEventSource.instances).toHaveLength(2);
    expect(latest()).not.toBe(dead);
    expect(latest().url).toBe("/api/events?token=test-owner-jwt");
    off();
  });

  it("the rebuilt connection's FIRST open fans a FULL resync — recovery that skipped this would silently drop everything emitted during the outage", async () => {
    const seen: string[] = [];
    const off = httpApi.subscribeEvents((t) => seen.push(t));
    FakeEventSource.instances[0].open();
    // First open of a FRESH connection: no resync (every hook fetched on mount).
    expect(seen).toEqual([]);

    FakeEventSource.instances[0].permanentError();
    await runRetry();

    // Still nothing: the new socket has not opened yet. The resync is tied to a
    // real open, never to the mere intent to reconnect.
    expect(seen).toEqual([]);

    latest().open();
    expect(seen).toEqual([...SSE_RESYNC_TOPICS]);
    off();
  });

  it("a TRANSIENT error (browser retrying by itself) does NOT tear the connection down or build a second one", async () => {
    const off = httpApi.subscribeEvents(() => {});
    const es = FakeEventSource.instances[0];
    es.open();

    es.transientError();
    await runRetry();

    expect(FakeEventSource.instances).toHaveLength(1);
    expect(es.closed).toBe(false);
    // No probe either: nothing has failed permanently, so there is nothing to
    // ask the server about.
    expect(probeCalls).toBe(0);
    off();
  });

  it("401 STOPS the retry loop, clears the token and fires oc-auth-expired — it never hammers a server that already said no", async () => {
    probeStatus = 401;
    const expired = vi.fn();
    window.addEventListener(AUTH_EXPIRED_EVENT, expired);
    const off = httpApi.subscribeEvents(() => {});
    FakeEventSource.instances[0].open();
    FakeEventSource.instances[0].permanentError();

    await runRetry();

    expect(expired).toHaveBeenCalledTimes(1);
    expect(localStorage.getItem(TOKEN_KEY)).toBe(null);
    expect(sseConnectionState()).toBe("unauthorized");
    // No new connection, and no further attempts however long we wait.
    expect(FakeEventSource.instances).toHaveLength(1);
    await runRetry();
    await runRetry();
    expect(FakeEventSource.instances).toHaveLength(1);
    expect(probeCalls).toBe(1);

    window.removeEventListener(AUTH_EXPIRED_EVENT, expired);
    off();
  });

  it("a 5xx is a transport failure, not an auth one: it reconnects and does NOT log anyone out", async () => {
    probeStatus = 503;
    const expired = vi.fn();
    window.addEventListener(AUTH_EXPIRED_EVENT, expired);
    const off = httpApi.subscribeEvents(() => {});
    FakeEventSource.instances[0].open();
    FakeEventSource.instances[0].permanentError();

    await runRetry();

    expect(FakeEventSource.instances).toHaveLength(2);
    expect(expired).not.toHaveBeenCalled();
    expect(localStorage.getItem(TOKEN_KEY)).toBe("test-owner-jwt");
    window.removeEventListener(AUTH_EXPIRED_EVENT, expired);
    off();
  });

  it("an unanswered probe (offline) reconnects too — 'no answer' must never be read as 'not authorized'", async () => {
    probeThrows = true;
    const expired = vi.fn();
    window.addEventListener(AUTH_EXPIRED_EVENT, expired);
    const off = httpApi.subscribeEvents(() => {});
    FakeEventSource.instances[0].open();
    FakeEventSource.instances[0].permanentError();

    await runRetry();

    expect(FakeEventSource.instances).toHaveLength(2);
    expect(expired).not.toHaveBeenCalled();
    window.removeEventListener(AUTH_EXPIRED_EVENT, expired);
    off();
  });

  it("repeated failures BACK OFF: the second wait is longer than the first, and a good open resets it", async () => {
    const off = httpApi.subscribeEvents(() => {});
    FakeEventSource.instances[0].open();

    // Failure #1 — retried after the first (shortest) delay.
    FakeEventSource.instances[0].permanentError();
    await vi.advanceTimersByTimeAsync(1000);
    await vi.advanceTimersByTimeAsync(0);
    expect(FakeEventSource.instances).toHaveLength(2);

    // Failure #2, WITHOUT a successful open in between — the same first delay
    // must NOT be enough this time.
    latest().permanentError();
    await vi.advanceTimersByTimeAsync(1000);
    await vi.advanceTimersByTimeAsync(0);
    expect(FakeEventSource.instances).toHaveLength(2); // still waiting
    await vi.advanceTimersByTimeAsync(1000);
    await vi.advanceTimersByTimeAsync(0);
    expect(FakeEventSource.instances).toHaveLength(3);

    // A good open resets the backoff, so the NEXT outage is short again.
    latest().open();
    latest().permanentError();
    await vi.advanceTimersByTimeAsync(1000);
    await vi.advanceTimersByTimeAsync(0);
    expect(FakeEventSource.instances).toHaveLength(4);
    off();
  });

  it("the LAST unsubscribe during the retry window cancels the pending retry — no connection is opened for nobody", async () => {
    const off = httpApi.subscribeEvents(() => {});
    FakeEventSource.instances[0].open();
    FakeEventSource.instances[0].permanentError();

    off(); // the retry timer is still armed at this point

    await runRetry();
    await runRetry();

    expect(FakeEventSource.instances).toHaveLength(1);
    expect(probeCalls).toBe(0);
    expect(sseConnectionState()).toBe("idle");
  });

  it("rebuilding does NOT stack a second foreground listener: one visibilitychange still fans exactly ONE resync", async () => {
    const seen: string[] = [];
    const off = httpApi.subscribeEvents((t) => seen.push(t));
    FakeEventSource.instances[0].open();
    FakeEventSource.instances[0].permanentError();
    await runRetry();
    latest().open();
    seen.length = 0;

    Object.defineProperty(document, "visibilityState", {
      configurable: true,
      get: () => "visible",
    });
    document.dispatchEvent(new Event("visibilitychange"));

    expect(seen).toEqual([...SSE_RESYNC_TOPICS]);
    off();
  });
});

describe("httpApi · the downlink's health is PUBLISHED (a frozen page must not look like a calm one)", () => {
  it("reports live → connecting → live across a permanent failure and its recovery", async () => {
    const states: string[] = [];
    const offState = httpApi.subscribeConnection((s) => states.push(s));
    // Fires immediately with the current state — a subscriber mounting mid-
    // outage must not have to wait for the next transition to learn about it.
    expect(states).toEqual(["idle"]);

    const off = httpApi.subscribeEvents(() => {});
    FakeEventSource.instances[0].open();
    expect(states).toEqual(["idle", "connecting", "live"]);

    FakeEventSource.instances[0].permanentError();
    expect(states).toEqual(["idle", "connecting", "live", "connecting"]);

    await runRetry();
    latest().open();
    expect(states).toEqual([
      "idle",
      "connecting",
      "live",
      "connecting",
      "live",
    ]);

    off();
    offState();
  });

  it("a TRANSIENT drop is reported too — the browser retrying is still a window in which the page is not receiving", () => {
    const states: string[] = [];
    const off = httpApi.subscribeEvents(() => {});
    FakeEventSource.instances[0].open();
    const offState = httpApi.subscribeConnection((s) => states.push(s));
    expect(states).toEqual(["live"]);

    FakeEventSource.instances[0].transientError();
    expect(states).toEqual(["live", "connecting"]);

    FakeEventSource.instances[0].open();
    expect(states).toEqual(["live", "connecting", "live"]);
    offState();
    off();
  });
});
