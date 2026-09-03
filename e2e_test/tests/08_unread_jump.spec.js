// e2e_test/tests/08_unread_jump.spec.js
// B9 · unread badge → 進房 divider 錨定 → 進房 mark-read 歸零 → SSE 新訊息浮條
// (M2 batch 19, 31e4e96 + 1473ff1).
//
// The race this spec exists to cover (vitest can't): the FE snapshots
// `member.unreadCount` at conversation entry STRICTLY BEFORE the entry read
// receipt goes out — since 8cd4fff9 the LISTING marks nothing, but ChatArea
// fires POST /api/chat/mark-read as soon as the newest page lands on a focused
// window, and the roster refetches to 0 right after. The clearer changed; the
// ordering hazard did not. Only a real server + real HTTP ordering exercises it.
//
// ⚠ ordering is load-bearing throughout: every unread_count sample happens
// BEFORE anything lists M's thread. The spec hires its OWN member (M is never
// roster[0] — the seed Mira is — so the office auto-open never touches M's
// watermark before the badge assertion).
const { test, expect } = require('@playwright/test');
const {
  authHeaders,
  BASE,
  ownerToken,
  hireMember,
  mintMemberToken,
  postChatAs,
  markChatRead,
  unreadCountOf,
  bootAuthedSpa,
  uniqueName,
  PNG_400x300_B64,
} = require('../lib/fixtures');

const NAME_M = uniqueName('Unread M');
const NAME_DECOY = uniqueName('Unread Decoy');
const OLD_COUNT = 14; // read context — enough to overflow one screen
const NEW_COUNT = 5; // the unread tail

const PAD =
  '— padding line so the thread overflows one screen height and the entry position is a real scroll decision';

test.describe('B9 · unread — badge, entry divider anchor, 進房 mark-read, floating chip', () => {
  test('badge shows the server count; entering anchors at the divider; entering clears it; SSE chip on new inbound', async ({
    page,
  }) => {
    const request = page.request;
    const token = await ownerToken(request);
    const M = await hireMember(request, token, NAME_M);
    // A second member with a NON-EMPTY thread: the entry into M's room below
    // deliberately happens FROM this thread — the stale-switch regression
    // (see the note at the hop).
    const decoy = await hireMember(request, token, NAME_DECOY);
    const tokM = await mintMemberToken(request, token, M.id, 1);
    // Seed the decoy's thread (owner → decoy; posting as the owner never
    // touches M's watermark).
    await postChatAs(request, token, decoy.id, `hello decoy ${PAD}`);

    // ── fixture: OLD read context (M → owner ×14, read up to the last of
    // them) + NEW unread tail (M → owner ×5).
    //
    // 🔴 THE READ IS REPORTED EXPLICITLY. It used to be produced by LISTING the
    // thread — `GET /api/chat?with=` advanced the watermark as a side effect —
    // and commit 8cd4fff9 removed that write from every path. A fixture still
    // built on the listing quietly leaves all 19 messages unread, and this
    // spec then fails 60 lines later on a count it never talks about.
    let lastOld;
    for (let i = 1; i <= OLD_COUNT; i++) {
      lastOld = await postChatAs(request, tokM, 'owner', `old read message ${i} ${PAD}`);
    }
    await markChatRead(request, token, M.id, lastOld.ts);
    const newMsgs = [];
    for (let i = 1; i <= NEW_COUNT; i++) {
      newMsgs.push(
        await postChatAs(request, tokM, 'owner', `NEW unread message ${i} ${PAD}`),
      );
    }
    const firstUnread = newMsgs[0];

    // ── API contract: unread_count == 5, sampled BEFORE any further list ──
    expect(
      await unreadCountOf(request, token, M.id),
      'the owner-perspective unread count must be exactly the new tail',
    ).toBe(NEW_COUNT);

    // ── browser: roster badge BEFORE entering the conversation ──
    await bootAuthedSpa(page, token);
    const card = page.locator('.member-card', { hasText: NAME_M });
    await expect(card).toBeVisible();
    // Precondition honesty: the office auto-opens roster[0]; if that were M,
    // its watermark would already be cleared and the badge trivially gone.
    await expect(
      card,
      'M must NOT be the auto-opened roster[0] (else the badge assertion is meaningless)',
    ).not.toHaveClass(/member-card--selected/);
    await expect(
      card.getByTestId('unread-badge'),
      'the roster badge must show the server-computed count',
    ).toHaveText(String(NEW_COUNT));

    // STALE-SWITCH REGRESSION (the old decoy workaround, inverted): ChatArea's
    // entry-positioning effect used to fire on a peer SWITCH while `messages`
    // was still the PREVIOUS peer's loaded thread (useChat cleared it one
    // commit later), latching the one-shot against stale data — switching from
    // a NON-EMPTY thread meant the divider never rendered. That frame is gone
    // since T-48 R13-5: OfficePage mounts ChatArea under `key={peerId}`, so
    // entering a room builds a fresh component whose one-shot has not been
    // spent. We still deliberately enter M's room FROM a settled NON-EMPTY
    // thread, because that is the path the defect lived on.
    await page.locator('.member-card', { hasText: NAME_DECOY }).click();
    await expect(
      page.locator('.chat__messages .chat__msg'),
      "the decoy's thread must be NON-empty (the stale-switch precondition)",
    ).not.toHaveCount(0);

    // ── enter the conversation: divider anchoring ──
    await card.click();
    const thread = page.locator('.chat__messages');
    await expect(thread).toBeVisible();
    const divider = thread.locator('.chat__unread-divider');
    await expect(divider, 'the unread divider must render').toBeVisible();
    await expect(divider).toContainText('以下為尚未閱讀的訊息');

    // The divider sits immediately ABOVE the FIRST unread message (the 5th-
    // from-last peer message — id known from the API fixture).
    const anchorId = await divider.evaluate(
      (el) => el.nextElementSibling?.getAttribute('data-msg-id') ?? '',
    );
    expect(
      anchorId,
      'the divider must anchor exactly at the first unread message',
    ).toBe(firstUnread.id);

    // Entry position: NOT at the bottom (the unread tail continues below)…
    const metrics = await thread.evaluate((el) => ({
      scrollTop: el.scrollTop,
      clientHeight: el.clientHeight,
      scrollHeight: el.scrollHeight,
    }));
    expect(
      metrics.scrollHeight,
      'the thread must overflow for entry positioning to be meaningful',
    ).toBeGreaterThan(metrics.clientHeight + 1);
    expect(
      metrics.scrollHeight - (metrics.scrollTop + metrics.clientHeight),
      'entry must land at the divider, not the bottom',
    ).toBeGreaterThan(4);
    // …and the DIVIDER ITSELF is what sits flush with the top of the thread
    // viewport. ChatArea does `divider?.scrollIntoView({ block: "start" })` on
    // purpose: leaving read context above it can push the first unread row out
    // of a compact viewport, which is the opposite of what entry positioning is
    // for. The earlier shape of this spec demanded a visible already-read
    // message above the divider (batch 19, LINE ref); owner ruled on 2026-08-05
    // at rc-8687b78cdbbb, option ①: the current screen is the contract — the
    // divider pins to the top — so the assertion is inverted here rather than
    // the product being changed back.
    const dividerOffset = await divider.evaluate((el) => {
      const box = el.closest('.chat__messages');
      if (!box) return null;
      return el.getBoundingClientRect().top - box.getBoundingClientRect().top;
    });
    expect(dividerOffset, 'the divider must be measurable inside the thread box').not.toBeNull();
    expect(
      Math.abs(dividerOffset),
      `the divider's top must sit flush with the thread's top, got ${dividerOffset}px off`,
    ).toBeLessThanOrEqual(2);

    // ── read convergence: entering the room IS reading. It is the COCKPIT
    // that reports it now (ChatArea's entry read receipt → POST
    // /api/chat/mark-read), not the listing — see the fixture note above. ──
    await expect
      .poll(async () => unreadCountOf(request, token, M.id), {
        message: 'the unread count must converge to 0 after the room lists the thread',
      })
      .toBe(0);
    await expect(
      card.getByTestId('unread-badge'),
      'the roster badge must be gone once read',
    ).toHaveCount(0);

    // ── T-48 新訊息預覽列: owner scrolled up + new inbound via SSE. The
    // 「有新訊息」 pill this replaces said one fixed sentence; the strip names
    // the sender and quotes the line, and clicking it lands on the LATEST
    // message rather than the first unseen one.
    await thread.evaluate((el) => {
      el.scrollTop = 0;
    });
    // ── T-48 ①: 捲上去，什麼都還沒發生 —— 圓形箭頭就該在了。owner 的條件是
    // 「最新那一則不在視窗內」（rc-72054864ff88），不是「有新訊息」。退場的
    // 「有新訊息」藥丸用的是後者，所以一個往回讀歷史的人在別人開口之前沒有任何
    // 路可以回到底部。這一條在真 server、真瀏覽器上釘住那個差別。
    await expect(
      page.getByTestId('chat-jump-latest'),
      'scrolling up alone must raise the arrow — no arrival required',
    ).toBeVisible({ timeout: 10_000 });
    await expect(
      page.getByTestId('chat-new-msg-preview'),
      'nothing arrived, so there is nothing to preview',
    ).toBeHidden();

    // 🔴 窄視窗,而且這不是「順便也測一下手機」。
    //
    // 這一段量的是「上方的內容晚長高之後,最新那一列還在不在視窗裡」。在 1280x720
    // 的預設視窗上這條路自己會癒合:內容長高時瀏覽器的 scroll anchoring 會補
    // `scrollTop`,補了就發 scroll 事件,而 `onMessagesScroll` 是 `latestInView`
    // 的七個寫入點之一 —— 箭頭因此碰巧回來了(實測 mutant 上 st 2314、arrowBack
    // true)。窄視窗上瀏覽器選的錨點不同,`scrollTop` 一動也不動、零個 scroll 事件,
    // `latestInView` 就停在落地當時那個過期的 true。設計者的實測也是在 390x844
    // 量到的(st 1101 → 1101、sh 1546 → 1964、gap 418.31、箭頭不回來)。
    // ⇒ 換寬度不是加測一個裝置,是這條護欄有沒有牙齒的差別。
    await page.setViewportSize({ width: 390, height: 844 });

    // ── 🔴 G-1 護欄的燃料:三張還在載入的圖片,落在**最新那一列的上方**。
    //
    // 這個 fixture 在 T-48 之前全是純文字,所以下面那一組斷言(最新那一列貼齊
    // 底部、而且沒有箭頭)是**碰巧**綠的 —— 沒有任何東西會在落地之後改變版面,
    // 所以它守不住任何事。兩個落點修正迴圈刪掉之後(owner rc-6c27f486ef9d),
    // 「上方晚載入的內容把最新那一列推到摺線下」變成一條真的會發生的路,而唯一
    // 還在替讀者說實話的東西是那顆「回到最新」箭頭。加三張圖就是把那條路接進來。
    //
    // bytes 擋到**落地之後**才放行:圖片沒解碼時 `.chat__msg-image` 是零高
    // (width/height:auto),解碼後每張撐開 225px。它們必須在最新那一列的上方,
    // 因為視窗**下方**長高對讀者是 0px 位移(實測),推不動任何東西。
    let releaseImages;
    const imagesHeld = new Promise((r) => {
      releaseImages = r;
    });
    await page.route('**/api/chat/attachment/**', async (route) => {
      await imagesHeld;
      await route.continue();
    });
    for (let i = 1; i <= 3; i++) {
      await postChatAs(request, tokM, 'owner', `image above the target ${i}`, [
        { data_b64: PNG_400x300_B64, filename: `above-${i}.png`, mime: 'image/png' },
      ]);
    }

    const lateBody = `late-breaking message ${PAD}`;
    await postChatAs(request, tokM, 'owner', lateBody);
    const strip = page.getByTestId('chat-new-msg-preview');
    await expect(strip, 'the preview strip must appear (SSE-pushed inbound)').toBeVisible({
      timeout: 15_000,
    });
    await expect(strip).toContainText(lateBody);
    // Mutually exclusive with the round jump-to-latest arrow.
    await expect(
      page.getByTestId('chat-jump-latest'),
      'the arrow must give way to the strip',
    ).toBeHidden();
    await page.getByTestId('chat-new-msg-jump').click();
    // The jump lands on the latest message ⇒ the strip is consumed and the
    // arrow stays away (nothing is off screen any more).
    await expect(strip, 'reaching the latest message must dismiss the strip').toBeHidden({
      timeout: 10_000,
    });
    await expect(page.getByTestId('chat-jump-latest')).toBeHidden();

    // 🔴 而且要**待得住**,而「待得住」在有東西還在載入的時候不等於「不會動」。
    //
    // 上面那兩行是輪詢的:只要在某一格取樣到「不在」就 PASS,所以在一個「箭頭消失
    // 10–40ms 又長回來」的產品上,它有時候會綠 —— 實測 30 次跑紅 16 次。所以這裡
    // 讓版面完全靜止之後再問一次。
    //
    // 但斷言的形狀變了,而且是**因為 owner 簽了字**才變的。他在 rc-6c27f486ef9d
    // 圈了「拿掉。圖片／卡片展開把目標擠走我接受」—— 所以「最新那一列被推到摺線
    // 下」本身不再是 bug,不可以再無條件斷言 `lastRowBottomGap <= 1`。
    //
    // 他**沒有**簽的是介面說謊。這個 app 的既有規則是「不在最新訊息時有個向下
    // 箭頭」,所以真正的不變量是這兩件**永不同時為假**:
    //     ① 最新那一列完整在視窗裡(gap <= 1),或
    //     ② 「回到最新」箭頭在畫面上。
    // 位移是代價,無聲的位移是 bug。刪掉迴圈之後量到的正是兩個都假
    // (gap 418.31、arrowBack=false):讀者按了「回到最新」,人不在最新,而且畫面上
    // 沒有任何東西告訴他 —— 那就是這張票存在的理由本身。
    releaseImages();
    await page.waitForTimeout(3000);
    const settled = await thread.evaluate((el) => {
      const rows = el.querySelectorAll('[data-msg-id]');
      const r = rows[rows.length - 1].getBoundingClientRect();
      const imgs = [...el.querySelectorAll('img.chat__msg-image')];
      return {
        distance: Math.round(el.scrollHeight - el.scrollTop - el.clientHeight),
        lastRowBottomGap: Number((r.bottom - el.getBoundingClientRect().bottom).toFixed(2)),
        imagesDecoded: imgs.filter((i) => i.naturalHeight > 0).length,
        imageHeights: imgs.map((i) => Math.round(i.getBoundingClientRect().height)),
      };
    });
    // 前提誠實:圖片真的解碼了。沒有這一行,上面整段可能只是「圖沒載到,所以沒有
    // 任何東西動過」的空綠。
    expect(
      settled.imagesDecoded,
      `三張圖必須真的解碼完成,否則這條護欄什麼都沒測到(量到 ${JSON.stringify(settled)})`,
    ).toBe(3);
    const arrowBack = await page.getByTestId('chat-jump-latest').isVisible();
    expect(
      settled.lastRowBottomGap <= 1 || arrowBack,
      `版面靜止之後,最新那一列要嘛還在視窗裡、要嘛畫面上有「回到最新」箭頭 —— ` +
        `兩個都不成立就是介面在說謊(量到 ${JSON.stringify({ ...settled, arrowBack })})`,
    ).toBe(true);
  });
});
