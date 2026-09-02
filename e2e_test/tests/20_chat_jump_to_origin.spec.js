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
  postChatAs,
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
