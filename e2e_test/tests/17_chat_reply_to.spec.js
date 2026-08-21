// e2e_test/tests/17_chat_reply_to.spec.js
// T-4e95 · 回覆這則 — the whole spine in ONE real browser against ONE real server:
//   點回覆 → 橫幅指名對象 → 送出 → 對方那一列出現引用列 → 點引用列跳回原訊息。
//
// WHY THIS EXISTS AND WHAT ONLY IT CAN SEE
// Every jsdom test in this feature stops at a seam. ChatArea's tests mock
// `useChat`, so the hook's third argument is invisible to them; useChat's tests
// mock `api`, so the wire body is invisible to them; http.mutations mocks
// `fetch`, so the SERVER is invisible to it; the Go conformance suite drives
// HTTP from Python, so the BROWSER is invisible to it. Nothing in the tree joins
// them. r16 measured the consequence: deleting `replyTo` from useChat's postChat
// call, and separately forcing `reply_to: ""` in http.ts, each left the whole
// 2258-test frontend suite green while 「回覆這則」 was dead in the real app.
// This spec is the only thing that fails on either.
const { test, expect } = require('@playwright/test');
const {
  BASE,
  authHeaders,
  ownerToken,
  hireMember,
  mintMemberToken,
  postChatAs,
  bootAuthedSpa,
  uniqueName,
} = require('../lib/fixtures');

const NAME_M = uniqueName('Reply M');
const TARGET = 'the sentence that gets quoted back';
const ANSWER = 'answering that one';
// Enough filler that the target scrolls out of view — the jump has to be a real
// scroll decision, not a no-op on a row already on screen. (The spec asserts
// that precondition rather than trusting it: see `not.toBeInViewport()` below.)
//
// It stays comfortably INSIDE the client's page size (useChat loads
// CHAT_PAGE_SIZE = 30 and only grows backwards when the owner scrolls up, which
// this test never does): 1 target + 24 filler + 1 reply = 26 rows, so the target
// is loaded, off screen, and the jump is offered. That combination is what the
// first test below needs.
//
// The SECOND test needs the opposite and says so with its own number.
const FILLER = 24;
// 🔴 FAR MORE THAN THE PAGE SIZE, ON PURPOSE. Until 2026-08-21 this spec had a
// comment warning that pushing the filler past 28 would drop the target out of
// the loaded window and break the quote assertion — because the client resolved
// the quote by looking the id up in what it had loaded, and fell back to a
// second HTTP read that could fail. The owner replaced that design: the server
// now ships the quoted message WITH every reply, on every read.
//
// So the case the old spec was carefully avoiding is now the case worth testing,
// and it is also the COMMON one — the owner's replies almost always reach far
// back. 200 rows guarantees the target is nowhere near the window.
const FAR_FILLER = 200;

test.describe('T-4e95 · reply-to — banner, wire, quote row, jump', () => {
  test('reply to a message: the banner names the sender, the send carries the link, the reply shows a quote row, and it jumps back', async ({
    page,
  }) => {
    const request = page.request;
    const token = await ownerToken(request);
    const M = await hireMember(request, token, NAME_M);
    const tokM = await mintMemberToken(request, token, M.id, 1);

    // The message that will be replied TO comes from the member, so the banner
    // has a name to print that is NOT the owner's — a banner that printed the
    // wrong one of the two people is the r14 bug, and only a real name catches it.
    const targetMsg = await postChatAs(request, tokM, 'owner', TARGET);
    for (let i = 1; i <= FILLER; i++) {
      await postChatAs(request, tokM, 'owner', `filler ${i}`);
    }

    await bootAuthedSpa(page, token);
    await page.locator('.member-card', { hasText: NAME_M }).click();
    const thread = page.locator('.chat__messages');
    // 🔴 BY ID, NOT BY TEXT. `hasText: TARGET` matches TWO rows once the reply
    // exists — the original, and the reply itself, whose quote row carries the
    // original's words INSIDE its own `.chat__msg`. `.first()` happened to land
    // on the original (a reply's ts is necessarily later than what it quotes and
    // the thread is ordered by ts), but that is a property of the data, not
    // something this locator says, and an earlier comment here claimed the count
    // "can only ever be 0 or 1", which was simply false. The id is unambiguous
    // and it is what the second test already uses.
    const target = thread.locator(`[data-msg-id="${targetMsg.id}"]`);
    await expect(target).toHaveCount(1);
    await expect(target).toBeVisible();

    // ── 點回覆 (the entry is hover-revealed but always occupies layout)
    await target.scrollIntoViewIfNeeded();
    await target.hover();
    await target.getByRole('button', { name: '回覆這則' }).click();

    // ── 橫幅 names the real sender, not a coin flip between the two people.
    const banner = page.getByTestId('chat-reply-banner');
    await expect(banner).toBeVisible();
    await expect(banner.locator('.chat__reply-banner__who')).toHaveText(
      `正在回覆 ${NAME_M}`,
    );
    await expect(banner.locator('.chat__reply-banner__body')).toContainText(
      TARGET.slice(0, 12),
    );

    // ── 送出
    const composer = page.locator('.chat__composer-row textarea');
    await composer.fill(ANSWER);
    await composer.press('Enter');
    await expect(banner).toHaveCount(0);

    // ── THE WIRE ACTUALLY CARRIED THE LINK. Read it back from the SERVER, not
    // from the DOM: this is the assertion that dies when `replyTo` is dropped
    // anywhere between the composer and the POST body.
    const res = await request.get(`${BASE}/api/chat?with=${M.id}&limit=100`, {
      headers: authHeaders(token),
    });
    const payload = await res.json();
    const rows = Array.isArray(payload) ? payload : payload.messages;
    const original = rows.find((m) => m.body === TARGET);
    const reply = rows.find((m) => m.body === ANSWER);
    expect(original, 'the quoted message must exist').toBeTruthy();
    expect(reply, 'the reply must have been stored').toBeTruthy();
    expect(
      reply.reply_to,
      'the stored reply must point at the message the composer was aimed at',
    ).toBe(original.id);

    // ── 引用列 on the reply's own row, carrying the quoted text.
    const replyRow = thread.locator(`[data-msg-id="${reply.id}"]`);
    const quote = replyRow.getByTestId('msg-quote');
    await expect(quote).toBeVisible();
    await expect(quote).toContainText(TARGET.slice(0, 12));
    // …and NOT the miss sentence. 🔴 THE STRING HERE MUST BE ONE THE PRODUCT CAN
    // ACTUALLY PRINT ON THIS ROW, or this line is a tautology defended by a
    // comment. It was 「較早的一則訊息」 for one round, after the row's copy had
    // already been changed away from it — the phrase existed nowhere else in the
    // repo, so the assertion could never go red. The quote ROW's miss sentence is
    // `chat.replyQuoteGone`; 「較早的一則訊息」 now belongs to the composer BANNER
    // (`chat.replyingToEarlier`), which is a different element and a different
    // claim. Same string as the second test below, on purpose.
    await expect(quote).not.toContainText('這則訊息已不存在');

    // ── 跳回原訊息
    //
    // 🔴 THE PRECONDITION IS PART OF THE TEST. Without it the final
    // `toBeInViewport()` is satisfied by a target that simply never left the
    // screen, and this assertion is vacuously true for a jump button that does
    // nothing at all. It passes today only because the filler above happens to
    // have pushed the target out — a layout coincidence, not a checked fact.
    // Assert it, so a future change to FILLER or to the row height fails HERE,
    // loudly, rather than quietly turning the assertion below into a no-op.
    await expect(target).not.toBeInViewport();

    await quote.getByTestId('msg-quote-jump').click();
    await expect(target).toBeInViewport();
  });

  // ── the case the old spec deliberately avoided ────────────────────────────
  //
  // 🔴 THIS IS THE ONE THAT WOULD HAVE BEEN RED BEFORE 2026-08-21 FOR A REASON
  // THAT WAS NOT A BUG, and is the reason the design changed. The quoted message
  // is 200 rows above the loaded window, so under the old shape the browser had
  // to go and fetch it: a request that could fail, a placeholder drawn while it
  // had failed, and a repair on the next inbound event. Now the quote arrives
  // attached to the reply and there is nothing to fetch.
  //
  // Adapted from the R20-B review probe `90_t4e95_r20b.spec.js`, which drove
  // exactly this setup to demonstrate the blip→lie→event→heal cycle. That cycle
  // does not exist any more, so the probe is not ported: what is kept is its
  // SETUP (push the target far out of the window in a real browser against a
  // real server) and its INSTRUMENT (route interception counting requests),
  // pointed at the opposite claim — the row is correct AND the browser asked for
  // nothing.
  test('a quote whose original is far outside the loaded window still renders — and costs no extra request', async ({
    page,
  }) => {
    // 200 sequential seed posts against a real server, then a full SPA boot.
    // The default 30s budget is not enough and a timeout here would read as a
    // product failure rather than as a slow fixture.
    test.setTimeout(180000);
    const request = page.request;
    const token = await ownerToken(request);
    const NAME_FAR = uniqueName('Reply Far');
    const M = await hireMember(request, token, NAME_FAR);
    const tokM = await mintMemberToken(request, token, M.id, 1);

    const FAR_TARGET = 'the sentence 200 rows above the window';
    const original = await postChatAs(request, tokM, 'owner', FAR_TARGET);
    for (let i = 1; i <= FAR_FILLER; i++) {
      await postChatAs(request, tokM, 'owner', `far filler ${i}`);
    }
    // Posted through the API with reply_to — the composer half of the spine is
    // covered by the test above; this one is about what a READ carries.
    const FAR_ANSWER = 'answering the one far above';
    const posted = await request.post(`${BASE}/api/chat`, {
      headers: authHeaders(tokM),
      data: { to: 'owner', body: FAR_ANSWER, reply_to: original.id },
    });
    expect(posted.status(), await posted.text()).toBe(200);

    // 🔴 THE INSTRUMENT. Every by-ids read is counted BEFORE the SPA boots, so
    // a read fired during the first paint cannot slip past. It is not blocked —
    // blocking would prove the row renders without the answer, which is a
    // weaker claim than proving nothing asked.
    let byIdsCalls = 0;
    await page.route(
      (url) =>
        url.pathname === '/api/chat' && url.searchParams.getAll('ids').length > 0,
      async (route) => {
        byIdsCalls += 1;
        return route.continue();
      },
    );

    await bootAuthedSpa(page, token);
    await page.locator('.member-card', { hasText: NAME_FAR }).click();
    const thread = page.locator('.chat__messages');
    const replyRow = thread.locator('.chat__msg', { hasText: FAR_ANSWER }).first();
    const quote = replyRow.getByTestId('msg-quote');
    await expect(quote).toBeVisible();

    // PRECONDITION, asserted rather than assumed: the quoted row really is NOT
    // in the loaded window. The jump control is the honest witness of that — it
    // is offered only for a target the client actually holds. Without this the
    // assertion below is satisfied by a target that never left the window, and
    // the test measures nothing.
    await expect(replyRow.getByTestId('msg-quote-jump')).toHaveCount(0);
    // …and by id, which is the unambiguous half. NOT `hasText: FAR_TARGET`:
    // measured, that matches ONE element — the reply itself, because the quote
    // row now carries the original's words INSIDE the reply's own `.chat__msg`.
    // That is the feature working, and it made the text-based precondition read
    // as "the original is on screen" and fail. Ask for the row's identity.
    await expect(
      thread.locator(`[data-msg-id="${original.id}"]`),
    ).toHaveCount(0);

    // ① THE ROW IS CORRECT ANYWAY — the whole point of the redesign.
    await expect(quote).toContainText(FAR_TARGET.slice(0, 20));
    await expect(quote).not.toContainText('這則訊息已不存在');

    // ② …AND THE BROWSER ASKED FOR NOTHING. Sit through a real event too: a new
    // message arrives, the thread refetches and repaints, and the count still
    // does not move. The deleted design's debt collector fired exactly here.
    await postChatAs(request, tokM, 'owner', 'a new sentence that wakes the stream');
    await expect(thread).toContainText('a new sentence that wakes the stream', {
      timeout: 20000,
    });
    await expect(quote).toContainText(FAR_TARGET.slice(0, 20));
    expect(
      byIdsCalls,
      'the quote must arrive with the reply — no by-ids read, ever',
    ).toBe(0);
  });
});
