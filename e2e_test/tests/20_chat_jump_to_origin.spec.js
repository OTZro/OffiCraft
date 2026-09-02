// e2e_test/tests/20_chat_jump_to_origin.spec.js
// T-48 ③ · 跳到原訊息 —— 定位到一則「已經捲出載入視窗之外」的訊息。
//
// 這是這張票對使用者最直接的那一件，owner 逐字：「請示卡的跳到原訊息功能，以及
// 有訊息的通知時，我希望都可以正確定位到該訊息」。
//
// 🔴 修之前是這樣壞的：聊天室開起來只載最新 30 則,「跳到原訊息」在**已經載進
// DOM 的那些列裡** querySelector，找不到就**不出聲、直接捲到最下面** —— 跟
// 「跳對了、剛好那一則在最下面」長得一模一樣。目標只要比 30 則舊就必定跳錯。
//
// 修完是這樣：以訊息 id 開窗（GET /api/chat?end_id= 往舊、?start_id= 往新，
// 兩端都含），兩頁撈回來就停在那一則上。**不是把整條歷史拉下來** —— owner 的
// 另一半交辦是「要注意 performance issue…向上向下滑再另打 API 去撈」，所以這支
// 也量「載進來的列數遠少於整條線」，以及往下捲會再撈一頁。
//
// 真瀏覽器才量得到的部分：落點是不是真的在視窗裡（jsdom 沒有版面，
// scrollIntoView 只能記錄有沒有被呼叫），以及窄寬兩寬下都要成立。
const { test, expect } = require('@playwright/test');
const {
  BASE,
  ownerToken,
  hireMember,
  mintMemberToken,
  postChatAs,
  unreadCountOf,
  bootAuthedSpa,
  uniqueName,
} = require('../lib/fixtures');

const TOTAL = 80; // ≫ 一頁 30，目標挑第 3 則:往舊往新都還有東西
const TARGET_INDEX = 3;
const PAD = '— 墊長一點，讓每一列都有高度，整條線真的會溢出視窗';

const WIDTHS = [
  { name: '窄 (390)', size: { width: 390, height: 780 } },
  { name: '寬 (1280)', size: { width: 1280, height: 900 } },
];

for (const w of WIDTHS) {
  test.describe(`T-48 ③ · 跳到原訊息 — ${w.name}`, () => {
    test('定位到一則載入視窗之外的訊息，而且沒有把整條歷史拉下來', async ({
      page,
    }) => {
      await page.setViewportSize(w.size);
      const request = page.request;
      const token = await ownerToken(request);
      const NAME = uniqueName('JumpOrigin M');
      const M = await hireMember(request, token, NAME);

      const ids = [];
      for (let i = 1; i <= TOTAL; i++) {
        const msg = await postChatAs(request, token, M.id, `line ${i} ${PAD}`);
        ids.push(msg.id);
      }
      const targetId = ids[TARGET_INDEX - 1];

      await bootAuthedSpa(page, token);
      // 這就是請示卡與通知用的那條路由 —— 只帶得出一個訊息 id，帶不出游標，
      // 正是舊實作沒辦法定位的原因。
      await page.evaluate(
        ([mid, cid]) => {
          window.location.hash = `#office/chat/${cid}/msg/${mid}`;
        },
        [targetId, M.id],
      );

      const thread = page.locator('.chat__messages');
      await expect(thread).toBeVisible();
      const target = thread.locator(`[data-msg-id="${targetId}"]`);

      // ① 那一則真的被撈回來了 —— 它比最新 30 則舊得多，舊實作連 DOM 裡都沒有。
      await expect(target).toBeAttached();
      await expect(target).toContainText(`line ${TARGET_INDEX} `);
      // ② 而且真的**停在畫面裡**（jsdom 量不到這一格）。
      await expect(target).toBeInViewport();
      // ③ 定位閃光落在那一列上，不是別人身上。
      await expect(target).toHaveClass(/chat__msg--located/);

      // ④ 效能那一半：撈回來的是「那一則附近的一個視窗」，不是整條線。
      const loaded = await thread.locator('.chat__msg').count();
      expect(
        loaded,
        '跳到原訊息只該撈目標附近的視窗，不是整條歷史',
      ).toBeLessThan(TOTAL);
      // 最新那一則不在載入的視窗裡 —— 這正是舊實作會誤降落的地方。
      await expect(
        thread.locator(`[data-msg-id="${ids[TOTAL - 1]}"]`),
      ).toHaveCount(0);

      // ⑤ 箭頭要在：最新那一則既不在視窗裡、也還沒被撈進來。
      await expect(page.getByTestId('chat-jump-latest')).toBeVisible();

      // ⑥ 點箭頭 → 回到真的最新那一則。這一步是箭頭在「錨點視窗」下的新義務：
      // 只是捲到 DOM 的最後一列，會停在一則根本不是最新的訊息上。
      await page.getByTestId('chat-jump-latest').click();
      const newest = thread.locator(`[data-msg-id="${ids[TOTAL - 1]}"]`);
      await expect(newest).toBeAttached();
      await expect(newest).toBeInViewport();
    });

    test('捲到錨點視窗底部會往新再撈一頁，一路走回最新那一則', async ({
      page,
    }) => {
      // owner 逐字：「向上向下滑再另打 API 去撈，就像聊天室的向上卷一樣」。
      // 往舊那條路 16_chat_scrollback 已經釘住；這裡釘的是**往新**那條，
      // 那是舊 API 根本表達不出來的方向（before_ts/before_id 只會往回走）。
      await page.setViewportSize(w.size);
      const request = page.request;
      const token = await ownerToken(request);
      const NAME = uniqueName('JumpForward M');
      const M = await hireMember(request, token, NAME);

      const ids = [];
      for (let i = 1; i <= TOTAL; i++) {
        const msg = await postChatAs(request, token, M.id, `line ${i} ${PAD}`);
        ids.push(msg.id);
      }

      await bootAuthedSpa(page, token);
      await page.evaluate(
        ([mid, cid]) => {
          window.location.hash = `#office/chat/${cid}/msg/${mid}`;
        },
        [ids[TARGET_INDEX - 1], M.id],
      );

      const thread = page.locator('.chat__messages');
      await expect(
        thread.locator(`[data-msg-id="${ids[TARGET_INDEX - 1]}"]`),
      ).toBeAttached();
      const before = await thread.locator('.chat__msg').count();
      expect(before).toBeLessThan(TOTAL);

      await thread.evaluate((el) => {
        el.scrollTop = el.scrollHeight;
        el.dispatchEvent(new Event('scroll'));
      });

      await expect
        .poll(async () => thread.locator('.chat__msg').count(), {
          message: '捲到視窗底部要往新再撈一頁',
        })
        .toBeGreaterThan(before);
      // 走到底就是真的最新那一則 —— 到這裡整條線才接回活的尾巴。
      await expect(
        thread.locator(`[data-msg-id="${ids[TOTAL - 1]}"]`),
      ).toBeAttached();
    });
  });
}

// ─────────────────────────────────────────────────────────────────────────────
// 這一票剩下的兩個「安靜地做錯事」。兩件都只在真瀏覽器 + 真 server 才算數:
// 一件要看畫面上有沒有真的出現那句話,一件要看 server 端的未讀數有沒有被動到。
// 只跑一個寬度就夠 —— 這兩件都不是版面問題。
test.describe('T-48 · 剩下的靜默失敗', () => {
  test('定位失敗時,畫面上真的講一句話 —— 不是只有 console', async ({ page }) => {
    // 🔴 接上以訊息 id 開窗之後,「那則訊息真的不存在」變成 server 的 404。
    // 前端退回底部 —— 光是這樣,跟「跳成功、剛好那則在最下面」長得一模一樣,
    // 正是這張票要拿掉的那個病。所以要在畫面上說出來。
    await page.setViewportSize({ width: 1280, height: 900 });
    const request = page.request;
    const token = await ownerToken(request);
    const M = await hireMember(request, token, uniqueName('JumpMiss M'));
    await postChatAs(request, token, M.id, `only line ${PAD}`);

    await bootAuthedSpa(page, token);
    // 格式合法(c-<hex>)但 server 上沒有這一則 —— 空白頁與 404 的差別就在這裡。
    await page.evaluate(
      (cid) => {
        window.location.hash = `#office/chat/${cid}/msg/c-00000000000000000000000000000000`;
      },
      M.id,
    );

    const notice = page.locator('.chat__jump-miss');
    await expect(notice).toBeVisible();
    await expect(notice).toContainText('找不到那則訊息');
    // 而且是關得掉的,不是永遠賴在那裡。
    await notice.locator('button').click();
    await expect(notice).toHaveCount(0);
  });

  test('跳到舊訊息不會把中間沒看過的標成已讀,回到最新那一端才標', async ({
    page,
  }) => {
    // owner 裁定逐字:mark-read 表達的意圖是「我看過了」,不是「我跳過來過」。
    // 這裡量的是 server 端的未讀數 —— 前端旗標可以說謊,未讀數不會。
    await page.setViewportSize({ width: 1280, height: 900 });
    const request = page.request;
    const token = await ownerToken(request);
    const M = await hireMember(request, token, uniqueName('JumpRead M'));
    const tokM = await mintMemberToken(request, token, M.id, 1);

    const ids = [];
    for (let i = 1; i <= TOTAL; i++) {
      const msg = await postChatAs(request, tokM, 'owner', `line ${i} ${PAD}`);
      ids.push(msg.id);
    }
    expect(await unreadCountOf(request, token, M.id)).toBe(TOTAL);

    await bootAuthedSpa(page, token);
    await page.evaluate(
      ([mid, cid]) => {
        window.location.hash = `#office/chat/${cid}/msg/${mid}`;
      },
      [ids[TARGET_INDEX - 1], M.id],
    );

    const thread = page.locator('.chat__messages');
    const target = thread.locator(`[data-msg-id="${ids[TARGET_INDEX - 1]}"]`);
    await expect(target).toBeInViewport();

    // ① 停在錨點視窗上 —— 中間那一大段誰都沒看過,未讀數一則都不准少。
    // 給它時間去做錯事:mark-read 是 fire-and-forget,不等它就等於沒量到。
    await page.waitForTimeout(1500);
    expect(
      await unreadCountOf(request, token, M.id),
      '停在錨點視窗時不該送出 mark-read',
    ).toBe(TOTAL);

    // ② 🔑 另一個方向,而且不能省:只釘①的話,「整條路壞掉、永遠不標」也會過,
    // 那本身就是另一個靜默失敗。按下回到最新 → 真的到了活的尾巴 → 才標。
    await page.getByTestId('chat-jump-latest').click();
    await expect(
      thread.locator(`[data-msg-id="${ids[TOTAL - 1]}"]`),
    ).toBeInViewport();
    await expect
      .poll(async () => unreadCountOf(request, token, M.id), {
        message: '回到最新那一端之後就要標已讀',
      })
      .toBe(0);
  });
});

// ─────────────────────────────────────────────────────────────────────────────
// 讀取失敗 ≠ 訊息不見了。這一件只有真瀏覽器算數:要真的讓那兩個開窗請求失敗
// (route.abort(),等同斷線/5xx),再看畫面上到底講了哪一句、以及那顆重試鈕按下去
// 有沒有真的再撈一次。
//
// 🔴 為什麼這是產品而不是文案潤飾:「已經被清掉了」會讓使用者**不再試**,
// 「現在讀不到」會讓他**再試一次**。訊息躺在 502 後面時說前者,就是這張票開票的
// 那個病 —— 對使用者說一句不成立的話 —— 換了一個地方重演。
test.describe('T-48 · 讀取失敗要說「現在讀不到」,而且真的給得出重試', () => {
  test('開窗請求失敗時說的是新那句、附重試鈕;按下去真的再撈一次並落在那一則', async ({
    page,
  }) => {
    await page.setViewportSize({ width: 1280, height: 900 });
    const request = page.request;
    const token = await ownerToken(request);
    const M = await hireMember(request, token, uniqueName('JumpUnreach M'));

    const ids = [];
    for (let i = 1; i <= TOTAL; i++) {
      const msg = await postChatAs(request, token, M.id, `line ${i} ${PAD}`);
      ids.push(msg.id);
    }
    const targetId = ids[TARGET_INDEX - 1];

    // 只打掉「開窗」那兩個請求(帶 start_id / end_id 的),一般的載入照常 ——
    // 這樣量到的才是跳轉這條路的失敗,不是整個座艙壞掉。
    const isAnchorWindow = (url) =>
      url.pathname === '/api/chat' &&
      (url.searchParams.has('start_id') || url.searchParams.has('end_id'));
    await page.route(isAnchorWindow, (route) => route.abort('failed'));

    await bootAuthedSpa(page, token);
    await page.goto(`/#office/chat/${M.id}/msg/${targetId}`);
    await page.reload();

    const notice = page.locator('.chat__jump-miss');
    await expect(notice).toBeVisible({ timeout: 15_000 });
    // ① 說的是「現在讀不到」,不是「被清掉了」。兩個方向都斷言 —— 只釘一句的話,
    //    把兩句合成一句照樣會過。
    await expect(notice).toContainText('現在讀不到那則訊息');
    await expect(notice).not.toContainText('可能已經被清掉了');
    // ② 而且真的有一條再試一次的路。
    const retry = page.getByTestId('jump-miss-retry');
    await expect(retry).toBeVisible();

    // ③ 辦公室回來了 —— 按下去要真的再撈一次,而且這次要落在那一則身上。
    //    ⚠️ 這一格是 F3 的形狀最容易復發的地方:鈕在、按得下去、什麼都沒發生。
    await page.unroute(isAnchorWindow);
    await retry.click();

    const thread = page.locator('.chat__messages');
    const target = thread.locator(`[data-msg-id="${targetId}"]`);
    await expect(target).toBeAttached({ timeout: 15_000 });
    await expect(target).toBeInViewport();
    await expect(target).toHaveClass(/chat__msg--located/);
    await expect(notice, '撈到了就不該還掛著提示').toHaveCount(0);
  });
});

// ─────────────────────────────────────────────────────────────────────────────
// owner 交辦逐字:「也要測試如果有新訊息跳進來,點選預覽畫面跳下去時,運作會正常」。
//
// 這是這一票最容易壞的接縫,因為它同時要滿足兩件相反的事:**停在錨點**(不准被
// 新訊息拉走、不准把中間那段標成看過)與**跳到最新**(點下去要真的到活的尾巴)。
// 而錨點視窗期間 useChat 刻意不跑最新頁的載入,所以新訊息**進不了那個 thread**
// —— 預覽列在錨點視窗下是不會出現的,讓位給箭頭(rc-72054864ff88 的互斥規則)。
// 這支把整條脊椎走完:錨點 →(新訊息)→ 箭頭回到活尾巴 → 捲上去 →(再一則新訊息)
// → 預覽列 → 點它 → 落在最新那一則 → 未讀歸零。
//
// 🔴 只有真瀏覽器算數:jsdom 沒有版面,量不到「那一則真的在視窗裡」;而未讀數要
// 問 server,前端旗標可以說謊。
test.describe('T-48 · 錨點視窗中有新訊息進來,點預覽列跳下去', () => {
  test('停在錨點時讓位給箭頭,回到活尾巴之後預覽列照常運作,點下去落在最新那一則', async ({
    page,
  }) => {
    await page.setViewportSize({ width: 1280, height: 900 });
    const request = page.request;
    const token = await ownerToken(request);
    const M = await hireMember(request, token, uniqueName('JumpPreview M'));
    const tokM = await mintMemberToken(request, token, M.id, 1);

    const ids = [];
    for (let i = 1; i <= TOTAL; i++) {
      const msg = await postChatAs(request, tokM, 'owner', `line ${i} ${PAD}`);
      ids.push(msg.id);
    }

    // 🔴 這一支**必須從一個已經帶著 msgId 的 URL 開機**,不能像上面幾支那樣先開
    // 座艙再改 hash。實測(cold run,加了 [REQ] 追蹤):辦公室會自己選一間房,於是
    // ChatArea 先以「沒有跳轉目標」掛載一次 —— `GET /api/chat?with=M` +
    // `POST /api/chat/mark-read` 都已經發出去了,65ms 後 end_id/start_id 才進來。
    // 未讀 81 因此變成 1,而那與被測行為無關,是測試自己把房間先打開的。
    // 帶著 hash 開機就是通知/保存連結真正的那條路,也讓第一次掛載就拿到 anchor。
    await bootAuthedSpa(page, token);
    await page.goto(`/#office/chat/${M.id}/msg/${ids[TARGET_INDEX - 1]}`);
    await page.reload();

    const thread = page.locator('.chat__messages');
    const target = thread.locator(`[data-msg-id="${ids[TARGET_INDEX - 1]}"]`);
    await expect(target).toBeInViewport();

    // ① 新訊息在錨點視窗期間進來 —— 畫面必須**留在原地**,而且是箭頭在場,
    //    不是預覽列(錨點視窗下 thread 不吃最新頁,所以沒有東西可以預覽)。
    const duringBody = `during-anchor ${PAD}`;
    const during = await postChatAs(request, tokM, 'owner', duringBody);
    await page.waitForTimeout(1500);
    await expect(target, '新訊息不准把讀者從錨點拉走').toBeInViewport();
    await expect(page.getByTestId('chat-jump-latest')).toBeVisible();
    await expect(page.getByTestId('chat-new-msg-preview')).toBeHidden();
    // 中間那一大段誰都沒看過 —— server 端的未讀數一則都不准少。
    expect(
      await unreadCountOf(request, token, M.id),
      '停在錨點視窗時不該送出 mark-read',
    ).toBe(TOTAL + 1);

    // ② 按箭頭回到活的尾巴 —— 錨點期間那則新訊息也在這裡出現。
    await page.getByTestId('chat-jump-latest').click();
    await expect(thread.locator(`[data-msg-id="${during.id}"]`)).toBeInViewport();
    await expect
      .poll(async () => unreadCountOf(request, token, M.id), {
        message: '回到活的尾巴之後就要標已讀',
      })
      .toBe(0);

    // ③ 🔑 接回活尾巴之後,普通的預覽列路徑要**完全正常** —— 這一格才是 owner
    //    問的那句話。捲上去(讓最新那一則離開視窗)、對方再開口。
    //
    // ⚠️ 要先等 scrollToLatest 的 2600ms 修正窗關掉:那段期間任何一次 reflow
    // (往上捲觸發 loadOlder、圖片解碼)都會把畫面**再拉回底部**,於是新訊息落地
    // 時 near-bottom 仍為真、走的是自動跟隨而不是預覽列 —— 測試會因為一個與被測
    // 行為無關的理由變紅。捲的幅度也刻意只夠讓最新那一則離開視窗,不捲到頂,
    // 免得順帶把 loadOlder 也牽進來。
    await page.waitForTimeout(3000);
    await thread.evaluate((el) => {
      el.scrollTop = el.scrollHeight - el.clientHeight - 600;
    });
    await expect(
      page.getByTestId('chat-jump-latest'),
      '最新那一則離開視窗就該有箭頭 —— 沒有的話下面那半是白等的',
    ).toBeVisible({ timeout: 10_000 });
    const lateBody = `after-return ${PAD}`;
    const late = await postChatAs(request, tokM, 'owner', lateBody);
    const strip = page.getByTestId('chat-new-msg-preview');
    await expect(
      strip,
      '接回活尾巴之後,新訊息必須進得來(SSE 載入不能還被錨點擋著)',
    ).toBeVisible({ timeout: 15_000 });
    await expect(strip).toContainText(lateBody);
    await expect(
      page.getByTestId('chat-jump-latest'),
      '箭頭要讓位給預覽列',
    ).toBeHidden();

    // ④ 點預覽列跳下去 —— 落在**最新那一則**,提示消失,未讀再次歸零。
    await page.getByTestId('chat-new-msg-jump').click();
    await expect(thread.locator(`[data-msg-id="${late.id}"]`)).toBeInViewport();
    await expect(strip).toBeHidden({ timeout: 10_000 });
    await expect
      .poll(async () => unreadCountOf(request, token, M.id), {
        message: '點預覽列跳到最新之後要標已讀',
      })
      .toBe(0);
  });
});
