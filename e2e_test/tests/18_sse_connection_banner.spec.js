// e2e_test/tests/18_sse_connection_banner.spec.js
// T-b0bb · a DEAD delta downlink must be visible, and must heal itself.
//
// WHY THIS SPEC EXISTS AND A UNIT TEST IS NOT ENOUGH
// The unit guards (frontend/src/api/http.sse-recover.test.ts) drive a FAKE
// EventSource: they prove the state machine is right, and they prove it against
// an object this repo wrote. What they cannot exercise is the whole stack the
// owner actually looks at — real Chromium, a real non-200 on `/api/events`, the
// real SPA — and it is that stack, not the state machine, that the report was
// about.
//
// ⚠️ WHAT THIS SPEC DOES NOT PROVE, said plainly. It does NOT prove the premise
// underneath the fix — that Chromium never retries a permanently-failed
// EventSource on its own. Proving a negative about the browser needs an
// observation window with our own retry disabled, which this spec does not set
// up. What it proves is the positive half: with the fix in, recovery runs (the
// probe `fetch` below is a fingerprint only this code path leaves), the outage
// is on screen while it lasts, and the page heals without a reload. If the
// premise were false the fix would be redundant, not wrong.
//
// The bug in one sentence: the cockpit is entirely delta-driven, so a downlink
// that has died renders EXACTLY like an office where nothing is happening —
// the owner's own report was that he could not tell, and reached for F5
// (2026-08-21: 「有時候…要 refresh page 才會更新」).
//
// Both halves are asserted, and the second is the one that matters most:
//   1. while the stream is dead, the page SAYS SO;
//   2. when the server comes back, the page reconnects on its own AND the bar
//      goes away — no reload. A recovery that stayed silent would have traded a
//      stall the owner can see for a hole he cannot.
const { test, expect } = require('@playwright/test');
const { ownerToken, bootAuthedSpa } = require('../lib/fixtures');

// The bar deliberately waits out short blips before speaking (see
// CONNECTION_BANNER_GRACE_MS). These timeouts must clear that window plus the
// reconnect backoff, so they are generous on purpose: a tight bound here would
// buy nothing and cost an intermittent red.
const APPEAR_MS = 20000;
const RECOVER_MS = 60000;

test.describe('T-b0bb · SSE downlink death is visible, and self-heals', () => {
  test('a permanently failing /api/events raises the banner; restoring the stream clears it without a reload', async ({
    page,
  }) => {
    const token = await ownerToken(page.request);

    // ── who is doing the retrying, measured rather than assumed ────────────
    // Requests to /api/events arrive in two distinguishable kinds, and the
    // difference is the evidence:
    //   resourceType "eventsource" — a real stream attempt;
    //   resourceType "fetch"       — OUR auth probe, which exists only inside
    //                                the permanent-failure recovery path.
    // A browser retrying on its own could only ever produce the first kind. So
    // a non-zero count of the second is a positive fingerprint of this fix
    // running, not an inference from "something happened more than once".
    const kinds = { eventsource: 0, fetch: 0, other: 0 };
    await page.route('**/api/events*', async (route) => {
      const t = route.request().resourceType();
      kinds[t === 'eventsource' || t === 'fetch' ? t : 'other'] += 1;
      // A non-200 with a non-stream content type: exactly the shape the spec
      // says the browser must FAIL the connection on, with no retry.
      await route.fulfill({
        status: 503,
        contentType: 'text/plain',
        body: 'downlink intentionally severed by 18_sse_connection_banner',
      });
    });

    await bootAuthedSpa(page, token);

    // ── 1. the page admits it is not receiving ──────────────────────────────
    const banner = page.locator('.connection-banner');
    await expect(banner, 'a dead downlink must be visible, not silent').toBeVisible({
      timeout: APPEAR_MS,
    });
    // It must say what the outage MEANS for what is on screen. "Disconnected"
    // on its own leaves the owner to guess whether the page still tells the
    // truth — which is the guess he was already making before this bar existed.
    await expect(banner).toContainText('即時更新已中斷');
    await expect(banner).toContainText('畫面上的內容可能不是最新的');

    // Evidence a human can look at, kept whether or not the run is red.
    await page.screenshot({
      path: 'test-results/18_sse_connection_banner-severed.png',
      fullPage: false,
    });

    // The recovery path really ran: the auth probe is a `fetch`, and nothing
    // but this fix issues one against /api/events.
    expect(
      kinds.fetch,
      'no probe fetch means the permanent-failure recovery path never ran',
    ).toBeGreaterThan(0);
    expect(kinds.eventsource, 'the stream itself was retried too').toBeGreaterThan(1);

    // ── 2. it comes back by itself ──────────────────────────────────────────
    // Remove the interception entirely rather than proxying the stream through
    // a route handler: an SSE body relayed by the router is not the thing under
    // test, and buffering inside the proxy would redden this for a reason that
    // has nothing to do with the product.
    await page.unroute('**/api/events*');
    await expect(
      banner,
      'once the stream is reachable again the page must recover on its own — no F5',
    ).toBeHidden({ timeout: RECOVER_MS });

    await page.screenshot({
      path: 'test-results/18_sse_connection_banner-recovered.png',
      fullPage: false,
    });
  });

  test('a healthy station shows no banner at all', async ({ page }) => {
    const token = await ownerToken(page.request);
    await bootAuthedSpa(page, token);
    // Wait out the grace window and then some: the bar must not appear on a
    // station that is working. A warning that shows up in normal operation is a
    // warning the owner learns to ignore, which would cost us the first test.
    await page.waitForTimeout(8000);
    await expect(page.locator('.connection-banner')).toHaveCount(0);
  });
});
