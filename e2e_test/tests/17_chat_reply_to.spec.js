// e2e_test/tests/17_chat_reply_to.spec.js
// T-4e95 · 回覆這則 — the whole spine in ONE real browser against ONE real server:
//   點回覆 → 橫幅指名對象 → 送出 → 對方那一列出現引用列 → 點引用列把原訊息撈回來
//   在放大閱讀的覆蓋層裡看全文（owner 2026-08-21 改設計：不再捲動）。
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
// Enough filler that the target is OFF SCREEN — not so the jump can make a
// "scroll decision" (it no longer scrolls anything; see the 看原訊息 test below,
// which asserts the thread does NOT move), but so that "the overlay shows the
// original" cannot be satisfied by text the page was already displaying. There
// is deliberately NO viewport assertion left in this file; the precondition that
// IS asserted is that the overlay's body is longer than the 60-rune excerpt.
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
  test('reply to a message: the banner names the sender, the send carries the link, the reply shows a quote row, and it opens the original in full', async ({
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

    // ── 看原訊息
    //
    // 🔴 THIS USED TO BE A SCROLL, AND IT IS NOT ONE ANY MORE (owner ruling
    // 2026-08-21: 「全部統一就撈那一則顯示出來就好」). The control reads that one
    // message back from the server and opens it in the shared full-view overlay.
    // The old assertions (`not.toBeInViewport()` then `toBeInViewport()`) are
    // deleted rather than kept: the thread deliberately does not move now, so a
    // viewport assertion would either be vacuous or wrong.
    //
    // The precondition that survives is a different one, and it is what makes
    // the overlay assertion non-vacuous: the FULL body must be longer than the
    // 60-rune excerpt the row already shows, so "the overlay opened on the
    // original" cannot be satisfied by re-displaying text the page already had.
    const jump = quote.getByTestId('msg-quote-jump');
    await jump.focus();
    await jump.click();

    const overlay = page.locator('.md-preview');
    await expect(overlay).toBeVisible();
    await expect(overlay.locator('.md-preview__title')).toContainText(NAME_M);
    await expect(overlay).toContainText(TARGET);

    // 🔴 FOCUS FOLLOWS THE DIALOG, BOTH WAYS. `aria-modal` is a promise, not a
    // behaviour: before this shipped, opening the overlay left focus on the
    // button OUTSIDE the portal, so a keyboard user who pressed 看原訊息 got a
    // dialog they were not in and a Tab that walked the page behind it. Measured
    // in this browser, not asserted from the source.
    expect(
      await page.evaluate(() =>
        Boolean(document.activeElement?.closest('.md-preview')),
      ),
      'focus must land inside the overlay',
    ).toBe(true);

    // Esc closes it and focus comes back to the control that opened it — not to
    // <body>, which would restart the next Tab at the top of the cockpit.
    await page.keyboard.press('Escape');
    await expect(overlay).toHaveCount(0);
    expect(
      await page.evaluate(
        () => document.activeElement?.getAttribute('data-testid') ?? null,
      ),
      'closing must hand focus back to the button that opened it',
    ).toBe('msg-quote-jump');
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

    // 🔴 THE INSTRUMENT. Every by-ids read is counted BEFORE the SPA boots, so a
    // read fired during the first paint cannot slip past. Nothing is blocked —
    // blocking would prove the row renders without the answer, which is a weaker
    // claim than proving nothing asked.
    //
    // ⚠️ COUNTED IN THE PAGE, NOT IN `page.route`. This used to intercept with a
    // route handler that did `byIdsCalls += 1; route.continue()`, which is
    // adequate for asserting ZERO and is NOT adequate for asserting ONE:
    // measured on this harness, a single click was counted twice on some runs
    // and once on others, because the continued request can re-enter the
    // handler. A guard whose whole job is "exactly one" cannot be built on a
    // counter that sometimes says two — it would have been dismissed as flake
    // and deleted. Wrapping `fetch` in an init script counts the calls the
    // application actually makes, once each, and runs before any page script on
    // every navigation.
    await page.addInitScript(() => {
      window.__ocByIds = 0;
      const orig = window.fetch;
      window.fetch = function (input, ...rest) {
        try {
          const raw = typeof input === 'string' ? input : input.url;
          const u = new URL(raw, location.href);
          if (
            u.pathname === '/api/chat' &&
            u.searchParams.getAll('ids').length > 0
          ) {
            window.__ocByIds += 1;
          }
        } catch {
          /* a request whose url will not parse is not a by-ids read */
        }
        return orig.call(this, input, ...rest);
      };
    });
    const byIds = () => page.evaluate(() => window.__ocByIds);

    await bootAuthedSpa(page, token);
    await page.locator('.member-card', { hasText: NAME_FAR }).click();
    const thread = page.locator('.chat__messages');
    const replyRow = thread.locator('.chat__msg', { hasText: FAR_ANSWER }).first();
    const quote = replyRow.getByTestId('msg-quote');
    await expect(quote).toBeVisible();

    // PRECONDITION, asserted rather than assumed: the quoted row really is NOT
    // in the loaded window.
    //
    // 🔴 THE OLD WITNESS FOR THIS WAS THE ABSENCE OF THE JUMP CONTROL, AND IT IS
    // GONE. The control used to be offered only for a target the client already
    // held, so `toHaveCount(0)` doubled as proof the target was out of the
    // window. Since 2026-08-21 it is offered for EVERY reply — it reads the one
    // message back rather than scrolling to it — so its presence says nothing
    // about the window and its absence would now be a defect. Asserted the other
    // way round below, and the window fact is carried by the id check that
    // follows.
    await expect(replyRow.getByTestId('msg-quote-jump')).toHaveCount(1);
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
      await byIds(),
      'the quote must arrive with the reply — nothing may fetch it in the background',
    ).toBe(0);

    // ── ③ 🔴 ONE CLICK, ONE REQUEST — IN A REAL BROWSER ────────────────────
    //
    // This is the same promise `ChatArea.quote-no-fetch.test.tsx` pins in jsdom,
    // asserted here where the request is a real one over the network. It is the
    // guard that stands between the redesigned control and the background
    // refetcher that was deleted: that machine kept a list of ids it still owed,
    // retried them, and repaired earlier answers when events arrived. Every one
    // of those behaviours shows up here as a second increment.
    await replyRow.getByTestId('msg-quote-jump').click();
    await expect(page.locator('.md-preview')).toBeVisible();
    await expect(page.locator('.md-preview')).toContainText(FAR_TARGET);
    expect(await byIds(), 'one click must cost exactly one by-id read').toBe(1);

    // …and it stays at one through a real repaint. Another message arrives, the
    // thread refetches, the overlay is still up — nothing may ask again.
    await postChatAs(request, tokM, 'owner', 'and one more while the overlay is open');
    await expect(thread).toContainText('and one more while the overlay is open', {
      timeout: 20000,
    });
    expect(
      await byIds(),
      'a repaint after the click must not ask again — that was the deleted collector',
    ).toBe(1);
  });

  // ── 🔴 THE PRODUCTION SHELL IS THE ONLY PLACE THIS CAN BE SEEN ────────────
  //
  // The quote row's excerpt is the whole value of the row since 2026-08-21, and
  // it was measured EMPTY across a wide band of window sizes — with the window
  // WIDER than sizes where it was fine. Cause: the app shell brings in a 264px
  // roster column at 721px, so `.chat__messages` drops from 628px to 347px in one
  // step, while the collapse rule for the jump label was written against the
  // VIEWPORT (`@media (max-width: 560px)`) and therefore handed the label back at
  // exactly the width where the pane was smallest. Measured, English:
  //
  //   vw=560  pane=468  43/61 chars      vw=721  pane=347   0/61
  //   vw=720  pane=628  48/61            vw=800  pane=426   0/61
  //                                      vw=880  pane=506   3/61
  //
  // ⚠️ AND NO COMPONENT TEST CAN SEE IT. `chat-reply-to.ct.spec.tsx` mounts these
  // rows with no app shell — no 1040px cap, no page padding, no roster column —
  // so the 281px discontinuity does not exist there at any viewport. Measured:
  // with the viewport rule in place, mutating `560px` to `400px` (weaker) and to
  // `900px` (stronger) each left all 27 CT tests green. ⚠️ That run cannot be
  // repeated from this tree — the rule it mutated is gone — and 27 was that
  // file's test count then, not now (32 today). Under the CONTAINER rule the
  // same two mutations each turn exactly one CT test red; that is the repair.
  // The CT file had even
  // written the discontinuity down in a comment; it just had no way to fail on
  // it. This spec is that way.
  //
  // The fix moves the condition onto the pane (`@container chat-pane
  // (max-width: 520px)`); this test is what makes the number mean something in
  // the shell it has to survive.
  test('the quoted sentence is never squeezed to nothing at any window width (English)', async ({
    page,
  }) => {
    test.setTimeout(120000);
    const request = page.request;
    const token = await ownerToken(request);
    // 🔴 A SHORT DISPLAY NAME, ON PURPOSE, AND IT IS NOT A CONVENIENCE. The quote
    // row's shrink order gives the excerpt away 10000× more eagerly than the
    // sender's name (office.css, and `chat-reply-to.ct.spec.tsx` pins that
    // ordering deliberately: the name says WHO is being answered). So a LONG
    // name starves the excerpt to zero on its own, at pane widths where the jump
    // label has nothing to do with it — measured here with the harness's usual
    // `uniqueName('Reply Wide')` (20 chars): 0 visible characters at vw=721 even
    // with the label correctly collapsed. That is a different question about a
    // different declaration, and letting it ride in this test would make the
    // test red for a reason it does not name.
    //
    // What this test is about is the BREAKPOINT'S AXIS, so the name is kept SHORT
    // (~5 chars, ~21px) and the label's ~133px is the only variable left. Still
    // unique — `uniqueName` produces a 20-character name, which is exactly the
    // pressure this test must not measure, so it is built here instead of reused.
    const NAME_W = 'A' + Date.now().toString(36).slice(-4);
    const M = await hireMember(request, token, NAME_W);
    const tokM = await mintMemberToken(request, token, M.id, 1);

    const WIDE_TARGET =
      'a sentence long enough that the quote row has to give something up';
    const original = await postChatAs(request, tokM, 'owner', WIDE_TARGET);
    const posted = await request.post(`${BASE}/api/chat`, {
      headers: authHeaders(tokM),
      data: { to: 'owner', body: 'answering it', reply_to: original.id },
    });
    expect(posted.status(), await posted.text()).toBe(200);

    // ENGLISH IS THE FIXTURE, not a variant. The Chinese CONTROL is ~58px
    // (「看原訊息」; the retired 「跳到原訊息」 was ~69px), the English one is
    // ~150px, and that width is the whole mechanism. A Chinese-only run walks
    // straight past this.
    //
    // ⚠️ SAY WHICH BOX EACH FIGURE IS. The ~150px here and the ~133px above are
    // not a disagreement: ~150px is the whole BUTTON (label + 2px gap + 12px
    // chevron), ~133px is the LABEL alone. Measured in the CT harness at the
    // same 11px: label 137px, button 151px.
    //
    // 🔴 localStorage ONLY, AND THAT IS DELIBERATE. The obvious alternative is to
    // PATCH `/api/settings { display_language: "en" }`, since the login reconcile
    // adopts the server value over the local cache — and it works. It also leaves
    // the WHOLE STUDIO in English for every test that runs afterwards: doing it
    // that way turned the first test in this very file red, because it looks for
    // a button named 「回覆這則」. Measured, not guessed.
    //
    // The reconcile adopts the server value only when it is literally "zh" or
    // "en" (i18n/index.tsx), and an untouched studio has "", so the local cache
    // stands. The assertion below is what makes that a checked fact rather than
    // an assumption: if this ever stops working the test says so instead of
    // quietly measuring the 69px Chinese label.
    await page.goto('/');
    await page.evaluate((t) => {
      localStorage.setItem('oc_token', t);
      localStorage.setItem('oc.language', 'en');
    }, token);
    await page.reload();
    await page.locator('.member-card', { hasText: NAME_W }).click();

    const quote = page.locator('.chat__messages').getByTestId('msg-quote');
    await expect(quote).toBeVisible();
    // The language really took — otherwise this whole test runs on the short
    // Chinese label and measures a layout nobody is complaining about.
    await expect(
      page.getByTestId('msg-quote-jump').first(),
    ).toHaveAttribute('aria-label', /original message/i);

    // Measured on this server, English, name 'Ada', with the pane the shell
    // actually hands the thread — and this is the whole point, because the pane
    // is NOT a monotonic function of the viewport:
    //
    // ⚠️ READ THIS TABLE AS A SHAPE, NOT AS EXPECTED VALUES, AND FOR TWO REASONS
    // that are both about the table not matching this test's own run:
    //   · it was taken with the display name 'Ada' (3 chars). This test builds a
    //     5-character name instead (see the note above `NAME_W` — 'Ada' is not
    //     unique across runs), so the excerpt widths it actually sees are a few
    //     px short of the column below.
    //   · the vw=560 row is NOT one of the widths this loop drives. It is here
    //     because it is the far side of the OLD viewport breakpoint and shows
    //     that the two rules agree there.
    // The only assertion below is `chars > 0`. Nothing here is compared against
    // these figures, and nothing should be: the point they carry is the
    // 721/800/880 band dropping to zero under the viewport rule.
    //
    //   vw   pane   label      excerpt px      excerpt px
    //                          (this fix)      (viewport rule, the mutant)
    //   560   468   collapsed      278             278
    //   720   628   whole          302             302
    //   721   347   collapsed       91               0   ← the band
    //   800   426   collapsed      153               0
    //   880   506   collapsed      215               0
    //  1280   666   whole          205             205
    //
    // 720 and 1280 are the controls on either side — they were never broken, so
    // a change that "fixes" the band by wrecking everything else still fails
    // here. 560 is the far side of the old viewport breakpoint.
    //
    // MUTANT (run): put the rule back on the viewport
    // (`@media (max-width: 560px)`) and this test goes red at vw=721 with 0 of
    // 61 characters, then again at 800 and 880. Nothing else in the repo moves —
    // that is what "the CT harness has no app shell" means in practice.
    for (const width of [720, 721, 800, 880, 1280]) {
      await page.setViewportSize({ width, height: 900 });
      const seen = await quote
        .locator('.chat__msg-quote__body')
        .evaluate((el) => {
          const pane = document.querySelector('.chat__messages');
          const node = el.firstChild;
          const clip = el.getBoundingClientRect();
          let chars = 0;
          if (node && node.nodeType === 3) {
            const range = document.createRange();
            const text = node.textContent ?? '';
            for (let i = 0; i < text.length; i++) {
              range.setStart(node, i);
              range.setEnd(node, i + 1);
              const r = range.getBoundingClientRect();
              // Fully inside the element's own clip box — a character whose
              // right edge is past it is painted away by `overflow: hidden`.
              if (r.width > 0 && r.right <= clip.right + 0.5) chars++;
            }
          }
          return { chars, paneW: pane ? pane.clientWidth : -1 };
        });
      // 🔴 COUNT CHARACTERS, NOT PIXELS OF BOX. The element keeps its box while
      // showing nothing: `overflow: hidden` is PAINT, so `clientWidth` can be a
      // healthy number over an empty row. Measuring per-character rects against
      // the clip box is the only thing in this repo that sees the real defect.
      expect(
        seen.chars,
        `vw=${width} (pane ${seen.paneW}px): the quoted sentence must not be ` +
          `squeezed to nothing — this is the band where a viewport-based ` +
          `breakpoint showed 0 of 61 characters while the window got WIDER`,
      ).toBeGreaterThan(0);
    }
  });
});
