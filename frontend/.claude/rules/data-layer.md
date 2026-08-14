---
paths:
  - "src/api/**"
  - "src/hooks/**"
  - "src/lib/deltaSink.ts"
  - "src/lib/ownerUnread.ts"
  - "src/lib/sharedSnapshot.ts"
  - "src/components/OnboardingBanner*"
  - "src/types.ts"
---

# frontend 資料層:seam、API 錯誤、SSE reconcile、共享設定快取

> 本檔由 `frontend/CLAUDE.md` 拆出(T-9b5d)。`paths:` 的 glob **相對 `frontend/`**(rules 檔所在目錄),所以一律寫成 `src/…`;**不要寫 `frontend/…`**——那種寫法在這個位置永遠不命中(實測 89 條對 565 個真檔零命中,已整批刪除)。
> 標 📎 的段落表示「當時的量測證據已搬回該票」,本檔只留規則。

## seam 分層(單向)
`wire → mappers → types → adapter → mock → http → hooks → component`。`api/index.ts` 的 `USE_MOCK` 是**單一 swap 點**(mock ↔ 真 http)。加一個 API:順著 seam 從 wire 到 component 各補一層,別跳層在 component 直接 fetch。

## API 錯誤(統一 envelope;見 `docs/design/api-error-envelope.md`)
非 2xx 一律 reject `ApiError`(`api/errors.ts`;mock ↔ http 同一 class):`.status`/`.code`/`.serverMessage` 來自 server envelope `{"error":{"code","message"}}`;讀 status 用 `isHttpStatus(e, n)`(同檔),**別 regex error message**(message 保形 `http <status> for <METHOD> <path>` 只供 log/legacy)。

## 一則通知 = 一次「只抓它碰到的那一項」(T-8115)

reconcile-by-refetch 的規則沒變(**永遠不 merge payload**),但「refetch 什麼」與「一則通知
算幾次」重新定義過。三個機制,全在 client,**wire 一個位元都沒動**。
📎 **當時的量測(請求數/位元組表、mutant kill 表、逐 hook 行號)已搬回 T-8115 的票**,
這裡只留規則;要重驗數字去讀那張票。

- **`SseDelta`(`api/http.ts` 的 `toSseDelta`)= payload 的 identity-only 投影**:只留
  `id`/`from`/`to`/`reader`/`peer` 五個欄位,`status`/`priority`/`last_read_ts`/`codename`
  這些**在 seam 就被丟掉**,下游 hook **拿不到**、因此不可能不小心 merge——「不准 merge」
  變成型別性質,不是要記得的規矩。護欄:`api/http.sse-delta.test.ts`。
- 🔴 **names 為空 = 「什麼都可能漏了」,一律全量重抓**。resync 名不指任何一項(串流沒有
  replay,漏了什麼本質上不可知),mock 更是連第二個參數都不傳。**空名字絕不可讀成
  「沒事發生」**,那會把 mock 座艙與每次重連後的自癒一起凍住。
- **`lib/deltaSink.ts`:一「陣」delta 只做一次決定**。`resyncAll` 把 topic **同步**扇給每個
  訂閱者,coalesce 只能發生在「決定要不要重抓」那一層——傳輸層不知道某個訂閱者對哪些
  topic 有反應。它靠的是「那個扇出是同步的」:累積到下一個 microtask 剛好抓到整陣。
  ⚠️ **這不是 debounce**,跨 tick 不合併(那等於刻意讓畫面慢);`deltaSink.test.ts` 有一條
  專門釘這件事。
- **`narrowToHeld` 是三態,而中間那態是關鍵**:`null`=名不指任何一項⇒全量;**非空陣列**=
  指到我手上的項⇒逐項重讀;**空陣列**=指了別人、一個都不是我的。第三態**不等於**第一態:
  對「不可能改變我這份清單成員資格」的 topic(chat/chat_read 對 roster 與外包 rail)它就是
  **真的沒事做**;對可能新增列的 topic(task)則必須當全量辦——新的一列只有清單看得到。
  把兩者合成一個 falsy 判斷,就會丟掉其中一半的修補。
- 🔴 **逐項重讀只有在「單筆回應是清單列的超集」時才成立,而三個端點只有一個是**
  (見 `api/dtoParity.ts` — 這是那份表存在的全部理由):
  - `useOutsourceWorkers` — **可以**逐項,但先過 `burstMovesNoOwnerUnread`(T-b17f):
    `m-other → ow-1` 那則雖然指名了 rail 上的 worker,那一列的 badge 卻**不可能動**
    (recipient 是 ow-1、不是 owner)⇒ 0 個請求。⚠️ **`ow-1 → owner` 是完全相反的一格,
    它的重抓是正當的**——述詞問的是 `to`,不是「有沒有指名一個 worker」。排序鍵是**綁定
    任務的 created_ts**,聊天碰不到 ⇒ 不准重排(否則一則訊息會讓列跳位)。
  - `useMembers` — **可以**逐項(roster 由 server 按 name 排序,chat/chat_read 碰不到 name
    ⇒ 不重排)。🔴 **但這條路一度是錯的,而且錯得很安靜**:單筆 handler 原本把 **literal 0**
    交給 `newMemberDTO`,逐項重讀因此把 delta 正在宣告的紅點**歸零**——方向只有一邊,
    **badge 只降不升**。修在源頭:兩支 handler 現在共用 `unreadCountsForRequest`。
    ⚠️ **「兩支 handler 共用」的範圍就是那兩支,不是「每個回 MemberDTO 的端點」**——其餘
    呼叫點仍有多處傳 literal 0,今天沒有使用者可見後果,但別讀成「到處都是真的」。
    🔴 **這條行為沒有被 conformance 釘住**(`unread_count` 在 `conformance/` 零命中),
    那道是 Go 單元測試,不是同一件事。
    🔴 **逐項只給「恰好指到一個」;指到兩個以上一律重抓清單,而交叉點正好在 2、是推導出來的
    不是可調旋鈕**(owner 2026-08-01 裁定):根因是 `unreadCountsForRequest` **每個請求各跑
    一次 `ListChat()` 全表掃描**,所以逐項的成本是 k 倍、不是 k 個小請求;清單恆為 1+1。
  - `useTasks` — **不可以**逐項,走清單。`GET /api/tasks/{id}` **整個 wire 上沒有
    `dep_tasks`**(凍結 spec 只把那個 server-side dep join 放在 `TaskListItemDTO`),而
    `TaskCard` 把「沒有人解析這個 dep」與「查無此任務」畫成**兩種不同的東西**。
    🔴 **同一個回歸在 render 層有第二條路,而且它承重、反直覺**:展開的任務卡手上**同時有
    兩個 TaskView**(`const view = hasDetail ? detail : task`)——`task` 是清單列(**有**
    `depTasks`)、`view` hydrate 後**沒有**。dep 那段刻意讀 `task`,周圍欄位讀 `view`。
    **把那一行改成 `view.depTasks` 就等於把回歸從 hook 層搬到 render 層,使用者看到的東西
    一模一樣**,而**一條沒展開卡片的 dep 測試對這類 bug 完全是盲的**。哨兵
    `TaskCard.dep-after-hydrate.test.tsx`。
  - `useChatUnread` — 一個總數,沒有「只抓一項」的版本;吃 coalescing **與**
    `burstMovesNoOwnerUnread`。**但跳過只在整陣的 topic 全是 chat/chat_read 時才成立**:
    `member` / `outsource_worker` 改的是**活著的那個集合本身**,那兩個 topic 照舊無條件重抓。
  ⚠️ **剩下的那個缺口補不在 client**:`dep_tasks` 是凍結 wire 沒有的欄位,要它就得**動 spec**
  (additive-optional;root §12 DTO 條),**還在等裁定**。在那之前**不要「順手」把
  `narrowToHeld` 接回 `useTasks`** —— 那個編譯期 pin(`TaskDTO` 沒有 `dep_tasks`)就是為了
  讓「以為加好了」立刻變成 tsc 紅。
  ⚠️ **members 那格的教訓要留著**:單筆端點「有宣告這個欄位」不等於「它會算」。加任何一條
  逐項路徑之前,先讀 `api/dtoParity.ts`,並且**去看那支 Go handler 真的填了什麼**。
- 🔴 **自激路徑:讀取本身是一次寫入。** `GET /api/chat?with=`(列表即讀)會推進 watermark,
  server 於是**把 `chat_read` 扇回同一個 client**;把這則 echo 當「別人的新資料」處理,公司裡
  任何一條聊天訊息都會**無中生有製造第二輪事件**。⇒ `useChat` 的 `chat` 分支**先看 delta 指的
  from/to 是不是這個 peer**;`chat_read` 分支**只認 `reader === peer`**。名字空的照舊無條件
  重抓。⚠️ 它**本來就會停**(那一輪裡沒有人再打一次列表即讀),所以不是無窮迴圈,是**每則
  訊息固定多一輪**的放大。
- 🔴 **Owner-unread 述詞(T-b17f):一陣 delta 只有在 `chat.to === owner` 或
  `chat_read.reader === owner` 時才可能動 owner 的 roster badge。** owner **不是名冊列**
  (single-owner schema),所以 member↔member 聊天、owner 自己送出的訊息、別人讀了 owner
  訊息的回執都**證明**動不了任何一個 badge ⇒ **直接 return,一個請求都不發**。述詞只有一個
  家:`lib/ownerUnread.ts` 的 `couldMoveOwnerUnread` / `burstMovesNoOwnerUnread`,由
  `useMembers` / `useChatUnread` / `useOutsourceWorkers` 三個 hook 共用——同一條不變量抄三份
  必然會各自漂移。
  ⚠️ **兩個 topic 的述詞欄位不同**(`chat` 是 `from`/`to`、`chat_read` 是 `reader`/`peer`),
  只檢查一對會把另一個 topic 真的有事做的那些也跳過。
  ⚠️ **反過來不成立,別讀成雙條件**:`k = 1` **不**蘊含「有事做」。
  🔴 **`k > 1 → full()` 沒有刪 —— 而且它是混合陣的熱路徑,不是 fail-safe。** 那個推理
  (「有一端是 owner ⇒ k ≤ 1」)**對「一則 delta」成立,對「一陣」不成立**:`narrowToHeld`
  讀的是**整陣的聯集**,一則 agent↔agent + 一則給 owner 落在同一個 microtask 就是 k = 3。
  🔴 **這個坑會反覆出現,記住它**:每次推理 k,先問手上拿的是哪一個——**per-delta** 的述詞
  (`couldMoveOwnerUnread`)還是 **per-burst** 的聯集(`touched`)。
  **跳過是整陣判斷、不是逐則過濾**:混合陣仍帶**全部** ids 走下面的分支,所以真的有事做的
  那一半永遠不會被吃掉;代價是**永遠正確,偶爾不是最省**,而那是安全的方向。
- ⚠️ **兩件已知的誠實性瑕疵,刻意不修**:①哨兵的 fixture 讓 agent↔agent 之後 badge 變成真
  server 產不出來的值——它現在反而是那條測試值斷言的鑑別力來源,但**別讀成「badge 應該長
  這樣」**;②那條 `PREMISE` 斷言(`delta.ids ∩ held`)是**文件,不是守衛**(`delta` 是測試裡的
  區域字面值、`held` 來自 mount,兩者都不依賴那個 k 分支)——**不要把它算進覆蓋。**
- 🔴 **判準是請求數,不是 badge 值**——「badge 沒變」在改動前後**都**成立,拿它當判準會寫出
  一條恆真的斷言。**每條成本斷言都配一條值斷言**:「請求變少」對一個乾脆不更新的 hook 也成立。
- 🔴 **而值斷言只有在假 api 不比真 server 慷慨時才算數——這條是這批修補的真正教訓。**
  第一版的兩個回歸通過了 tsc、整套 jsdom、CT 與 frame 探針,因為手寫的假 api 拿**清單列**回答
  `GET /{id}`:那個 wire 不存在,於是值斷言量的是一台不存在的 server。**繞過共用 mock 自己
  手寫假貨,就是繞過那份已經校準好的知識。** ⇒ 單筆端點的落差集中在 `api/dtoParity.ts`
  **一份表**,`projectSingleItem()` 供測試建假貨用。
  🔴 **但「三個 getter 都走 `projectSingleItem` ⇒ 構造上不可能比 wire 慷慨」這句話,對 member
  與 task 兩格目前是空的——別把它當成現行防線**(member 的 gap 已清空 ⇒ 那支是 identity;
  task 根本不再呼叫 `getTask` ⇒ 沒有消費者)。真正擋得住的是**三道**:
  1. `server/ocserverd/api_members_unread_parity_test.go`(斷言 **response body 裡的數值**);
  2. `api/dtoParity.test.ts` 對 `api/mock.ts` 的 parity;
  3. `api/dtoParity.test.ts` 的**編譯期 pin**(`TaskDTO` 沒有 `dep_tasks`、`TaskListItemDTO`
     有)——dep join 那半目前**只**靠這一道。
  ⇒ **要加任何一條新的逐項路徑,先把對應那格的 `PER_ITEM_DTO_GAPS` 與上面三道一起看**:
  fake 那層的保護會隨著 gap 清空 / 消費者消失而自動失效,**它不會有人通知你**。
  ⚠️ **三道都抓不到的方向**:server **自己**改了。整體只有跑真 ocserverd 的 conformance 級
  對帳才守得住,**那是還沒做的事,別把這份 guard 當成它。**
- 護欄:`hooks/sseFanout.test.tsx`、`lib/deltaSink.test.ts`、`api/http.sse-delta.test.ts`、
  `api/dtoParity.test.ts`。

## /api/settings 只讀一份;`onboarding: null` 是終態(T-8115)

`GET /api/settings` 在正式站是 **639,270 bytes**(gzip 後 373 kB;`custom_themes`
一欄佔 626,721 = 98%,其餘 15 欄合計約 2.5 kB)。它同時是**六個**互不相識的
mount-fetch 消費者的來源,所以那份 payload 一次座艙載入被下載六遍。

- **唯一入口 `hooks/sharedServerSettings.ts`**(核心在 `lib/sharedSnapshot.ts`):
  合併(single-flight)+ 快取 + 世代守衛。**mount-fetch 一律走 `loadServerSettings()`,
  不要在新的地方直接叫 `api.getServerSettings()`** —— 那正是這張票在收的東西。
  現有六個消費者:`useOrgName` / `useOwnerName` / `useServerSettings`
  (`SettingsPage`、`MonitorPage`、`DocumentHistoryEntry` 三處 mount)/
  `useOutsourceWorkers` / `i18n` 的登入 reconcile / `OnboardingBanner` 首讀 /
  `PushNotifications`、`ProfileDropdown` 的 mount 路徑。
- 🔴 **快取什麼時候失效,只有三個答案**:(a) **本分頁自己存檔成功** →
  每個 `patchServerSettings` 的 echo 都要 `adoptServerSettings(echo)`(新增 PATCH
  呼叫點時**一起加**,漏掉就是畫面停在存檔前的值);(b) **身分改變**(登入 /
  auth-expired 事件)自動 invalidate;(c) `refreshServerSettings()` —— 給
  **不准讀記憶**的兩個呼叫點:onboarding 輪詢(它就是要看值變)與 設定 的
  存檔測連通 read-back(它就是要證明 server 同意)。**沒有 TTL。**
- ⚠️ **已知且 owner 已知情的邊界**:settings 改變時 server **不發任何即時通知**,
  所以另一個分頁 / 另一台裝置的存檔在這裡看不到,直到重新載入。**不要假裝解決了**;
  要真解決得先加 SSE topic(動凍結 wire)。
- **世代守衛不是裝飾**:存檔前發出、存檔後才回來的那個 GET 會把剛存的值蓋回去。
  request 記住自己出發時的 generation,`adopt`/`invalidate` 把 generation 推進,
  過期的回應只回給自己的 caller、不寫快取。
- **測試面**:`src/test/setup.ts` 在每個 test 之間 `resetAllSharedSnapshots()`
  ——module-level 快取會把上一條 test 的 fixture 餵給下一條。它從 `lib/` 匯入
  (**不是**從 hooks 那支),因為 setup 檔跑在測試檔自己的 `vi.mock("../api")`
  註冊**之前**,從 hooks 匯入會把 api 層先拉進 registry。

**`onboarding: null` 是終態——而這是與 server 的成對契約(T-8115)。** DTO 明訂 null 是
「onboarding never ran」的**正常值**(舊安裝、或建庫時就有密碼),正式站正是 null;
舊碼的 `isTerminal(null) === false` 讓每次開座艙輪滿 3 分鐘 = **61 次** × 373 kB
(這個數字是 mutant 讓測試自己印出來的,不是推的)。
- **憑什麼敢把 null 當終態**:`kickFirstRunOnboardingWith`(`server/ocserverd/onboarding.go`)
  在**開 goroutine 之前**就把 `running` 報告寫進 DB,所以那一列在
  `POST /api/auth/set-password` 的 handler **return 之前**就存在,而該回應是 return 才
  flush ⇒ **拿到那個 200 的 client 之後讀 settings 一定看得到報告**。它其餘四條 early
  return 代表 onboarding 根本不會跑、而且不重試 ⇒ **client 看得到的 null 只有一種**。
- 🔴 **這是成對的,改一邊要看另一邊**。server 那半的護欄是
  `server/ocserverd/onboarding_contract_test.go`(`TestOnboardingClaimIsPersistedBeforeKickReturns`
  + `TestSetPasswordLeavesNoNullOnboardingWindow`);FE 這半是
  `OnboardingBanner.null-poll.test.tsx`。把認領搬進 goroutine,server 那條會紅——
  那正是它存在的理由。
- 🔴 **不准改成只抓一次**:首次安裝的失敗結果在 t≈30s(`wardenOnlineWait`)才落地,
  那正是這個橫幅唯一存在的理由。首讀讀到的是 `running`(見上),而 `running` 是非終態,
  輪詢照舊跑到 180 s 天花板。
- **讀取失敗 ≠ null 報告**:catch 分支必須繼續輪——首啟開機期的短暫失敗正是它想有用的時候。
  三條測試各釘一件事(一次讀 / running→failed / 讀失敗仍續輪),三個 mutant 各紅一條。
