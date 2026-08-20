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
// 🔴 THIS NUMBER IS COUPLED TO THE CLIENT'S PAGE SIZE AND HAS ONLY 4 SPARE.
// useChat loads CHAT_PAGE_SIZE = 30 messages and grows backwards only when the
// owner scrolls up, which this spec never does. The thread here is
// 1 target + FILLER (24) + 1 reply = 26 messages. Push FILLER past 28 and the
// TARGET falls out of the loaded window: the quote row then renders the honest
// 「較早的一則訊息」 miss with no jump control at all, and BOTH the quote
// assertion and the jump would go red for a reason that has nothing to do with
// 「回覆這則」 being broken. If the page size changes, this number moves with it.

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
    await postChatAs(request, tokM, 'owner', TARGET);
    for (let i = 1; i <= FILLER; i++) {
      await postChatAs(request, tokM, 'owner', `filler ${i}`);
    }

    await bootAuthedSpa(page, token);
    await page.locator('.member-card', { hasText: NAME_M }).click();
    const thread = page.locator('.chat__messages');
    // EXISTENCE, not uniqueness: `target` is already `.first()`, so its count
    // can only ever be 0 or 1 and a `toHaveCount(1)` here was dressing an
    // existence check up as a uniqueness check it cannot perform.
    const target = thread.locator('.chat__msg', { hasText: TARGET }).first();
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
    const replyRow = thread.locator('.chat__msg', { hasText: ANSWER }).first();
    const quote = replyRow.getByTestId('msg-quote');
    await expect(quote).toBeVisible();
    await expect(quote).toContainText(TARGET.slice(0, 12));
    // …and NOT the honest-miss label: the quoted row is in the loaded window.
    await expect(quote).not.toContainText('較早的一則訊息');

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
});
