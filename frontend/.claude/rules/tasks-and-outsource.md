---
paths:
  - "src/components/Task*"
  - "src/components/Outsource*"
  - "src/components/WorkerDetailPanel*"
  - "src/components/OfficePage*"
  - "src/components/tasks.css"
  - "src/hooks/useTask*"
  - "src/hooks/useOutsourceWorkers*"
  - "src/hooks/useWorkerCodenames*"
  - "src/lib/stepBadge.ts"
  - "src/lib/taskNo.ts"
  - "src/lib/duration.ts"
---

# 任務頁 + 任務卡、外包面板 + 外包聊天、設定 › 任務手冊

> 本檔由 `frontend/CLAUDE.md` 拆出(T-9b5d)。`paths:` 的 glob **相對 `frontend/`**(rules 檔所在目錄),所以一律寫成 `src/…`;**不要寫 `frontend/…`**——那種寫法在這個位置永遠不命中(實測 89 條對 565 個真檔零命中,已整批刪除)。
> 標 📎 的段落表示「當時的量測證據已搬回該票」,本檔只留規則。

## 任務頁 + 任務卡(M3 Phase 3)
主導航第四頁「任務」(`#tasks`);badge = 非終態任務數(`GET /api/tasks/count`,
`useTaskCount` 訂 `task` topic,接法同等我回覆 badge)。資料流 = `useTasks`
(mount fetch + SSE `task`/`outsource_worker`/`task_manual` refetch);
🔴 **清單以「使用者勾的那組狀態」向 server 要(T-a3e4,owner 拍板「不是應該以狀態
filter 嗎」)**:`useTasks(initialStatuses)` 把 TasksPage 的 `statusFilter` 送成
重複的 `?statuses=`,**執行者/類型兩軸仍在 FE**(它們不是 payload 病灶)。
舊寫法是「不帶 query 拉全量、全部在 FE 篩」,T-2b9d 加了 `?open=true` 想救,
但 T-1d82 又補了一條「只要任何未結案任務帶 dep 就把 includeClosed 打開」——
實務上恆真,所以**每一則 task SSE 都在重抓整部歷史**(實測 408,482 B vs 17,295 B)。
現在 dep 顯示資料由 server 附在列上(見下方 dep chips),那條 clause 已整個刪除。
**只有一個**視圖真的需要全狀態,靠**送空集合**表達:清除篩選——那時使用者要的就是
全部,下載全部是答案、不是缺陷,別「順手」把它也優化掉。
🔴 **`#tasks/<id>` 跳轉錨點曾是第二個,而那個是缺陷**(owner 2026-08-01 指名要修):
錨點可能指向沒被勾的狀態(預設篩選下的已結案任務就是日常),舊碼因此**整個放掉篩選、
改抓不帶條件的全量**,只為了讓那一張出現在畫面上(實測 432 kB / 706 列)。現在
`useTasks(initialStatuses, anchorTaskId)` 走 **`GET /api/tasks/{id}` 只補抓那一張**再
併進 `tasks`,清單那一問一個字都不動。三條配套是一體的,拆掉任何一條就是把病裝回去:
(a) **anchor id 是參數、不是 effect**——跳轉可能是首次 mount,晚一個 commit 就等於
mount 那一發又拉了全量;(b) **`anchorPending`**——單張補抓在自己的 request 上,所以
存在「清單到了、那一張還沒到」的幀,自癒邏輯(未知 id → 退回 `#tasks`)與兩個空狀態
文案都必須等它,否則連結會在使用者眼前把自己抹掉;它**成功與失敗都會落定**,所以補抓
失敗(500/離線)是誠實退回一般清單,不是空白也不是轉不停;(c) **合併時清單列優先**
——`TaskDTO` 沒有 `dep_tasks` 欄(那個 join 只掛在輕量列上,spec 已凍結),讓單張版蓋掉
清單列會讓被連到的卡片 dep chips 掉回「還不知道」。⚠️ 已知取捨:錨點指向**沒被勾的狀態**
時,那張卡的 dep chips 就是「還不知道」態(`depTasks === undefined`)——誠實的第三態,
不是謊,但要修得動凍結 wire,不在本批。
護欄:`hooks/useTasks.anchor.test.ts`(斷言實際送出的 `statuses` 永不為 undefined)+
`components/TasksPage.anchor-fetch.test.tsx`(已結案錨點仍顯示、in-flight 不自癒、
補抓失敗誠實落地)。
**空狀態文案的判準改讀 `GET /api/tasks/count` 的 `total`**(未篩選總數):
「目前沒有任務」是對整個工作室的主張,篩選過的清單答不出這件事——而它是一個
grouped COUNT,不是把清單重新拉寬。
分區:未結束(非終態一清單,高→中→低→凍結、同級 created 新→舊,不分狀態子組)
/已結束(可摺疊預設收合,同 RepliesPage answered-toggle)。卡(`TaskCard`)
無詳情頁、**預設摺疊**(owner 照 mockup 拍板 2026-07-13):卡頭(標題+
「type · 負責人 · 模型 · 投入度」副標,成員執行者帶「· 成員」)+優先權/狀態
徽章+kebab+chevron;#T 代號 chip+識別鍵 chip+「等 T-xxxx」dep chips(**dep 的編號/標題/狀態讀
`task.depTasks`(wire `dep_tasks`)——server 對整張 task 表 join 好的,T-a3e4;
不要改回 `allTasks.find`,那個查找就是上面那條 payload 病灶的來源。三態要分清:
有 status = 解析到、status 為空 = 查無此任務、整個欄位 undefined = 這個 server
不解 dep(還不知道,不可宣稱不存在))、進度條
「步驟 N/M · 已歷時 X」、等待外部紫 banner、訊息框**摺疊時也顯示**;chevron
展開才給 description+內嵌回覆卡+工作流程(每步名稱+狀態徽章+DoD+右上耗時);
負責人、建立者與前任負責人的身分 chip 會依 stable member id 顯示個人頭像，
無個人圖時沿用 role/theme → glyph fallback；Avatar 本身仍不畫 presence 點。
§3.6 跳轉目標自動展開:
- **進度/狀態全 passthrough**:`progress_done/total` 用 server 算好的,UI 不自算;
  狀態推進 agent 回報、owner 只有「終止」這一個直接狀態動作(ConfirmModal 二次確認)
  + 優先權調整(含凍結/解凍,同一 `/priority` knob)。
- **gate 狀態**:`is_gate` + `reply_card_id==""` = 虛線「等我回覆」預告;非空 = 生效
  → 內嵌 `TaskReplyCard`(可多張),內裡**絕對重用** M2 `ReplyCardBody.tsx`
  (單卡 refetch + `reply_card` topic,同 ChatReplyCard 模式——回覆同步反映到
  等我回覆頁)。**H4 配套**:gate step 仍 `waiting_owner` 而綁卡已 answered →
  step 徽章顯「已回覆 · 等待接手」(子卡經 `onCard` 回報卡態給 TaskCard)。
  step 徽章單一判斷源 = `lib/stepBadge.ts`(T-d64f);**superseded(T-1aea)**:
  re-plan 凍結的已答卡節點 → 「已取代」徽章 + `task-step--superseded` 灰階,
  問答內容仍由內嵌卡承載;gate 預告分支對終態(done/superseded)不再虛線預告。
  superseded 不算 `progress_done/total`(server 除名)→ 「全 superseded」任務誠實
  報 0/0 但 steps 非空:TaskCard 的 hydrate loading gate 不再要求 progressTotal>0
  (未指派例外,等待指派可從輕量摘要直接推導),避免落「等待建立 Steps」謊態。
- **外包顯示誠實線**:TaskCard 的「外包 代號 · 模型 · 投入度」只從 LIVE
  `GET /api/outsource-workers` 解析;worker 已 release(結案)→ 誠實退回裸「外包」,
  永不捏代號。⚠️ **這條講的是任務卡上的 chip,它描述的是「這張任務要用什麼開外包」
  ——launch intent,刻意留設定值**;監控台那張表的外包列走的是**另一條**(自報值,
  見下方「effort」節),兩者別互抄。未指派(kind=outsource, executor_id="")→「未指派」+ 訊息框 disabled
  (server 會 409)。過渡態:未指派→「等待指派」、有執行者零節點→「規劃中」。
- 訊息框 → `POST /api/tasks/{id}/message`(server 幫掛 task context meta 成普通聊天
  訊息)。已歷時自 created_ts ticking(`lib/duration.ts` 的 `formatDuration`,與
  RepliesPage 已等你共用)、終態凍結在 closed_ts。狀態文案照 spec 六態
  (尚未執行/進行中/等我回覆/等待外部/已完成/終止),不用 mockup 的變體。

- 🔴 **任務卡是唯讀的標題與敘述,而且這是裁定不是遺漏(T-e5b1,owner 2026-08-15
  「UI 不需要提供編輯標題或敘述的功能」)**。T-2ebe 的就地標題編輯器與 T-e271 的敘述
  編輯器、它們的 `onUpdateTitle`/`onUpdateDescription` prop、共用的 hint/input/actions
  CSS 家族、以及那兩個編輯面裡的 `DocumentHistoryEntry` 入口,**整族已移除**。
  ⛔ **不要「順手」加回一顆編輯鈕**——`useTasks` 的 `updateTitle`/`updateDescription`
  與 `api.updateTaskTitle`/`updateTaskDescription` 仍在(seam 完整、mock 與 http 測試
  照舊全綠),所以接回去只要一行,而那一行會推翻 owner 的裁定。
  ⚠️ **拿掉的是畫面入口,不是能力**:MCP `update_task_title` / `update_task_description`
  與它們的路由一個位元組沒動,agent 照樣改得動;版本紀錄(`task_title`/`task_description`
  兩個 kind)也還在 server 上,只是座艙目前沒有面去開它。
- 🔴 **步驟備註預設收起,每一步一顆展開開關(T-e5b1,owner:「不然太長了」)**。
  state 是 `TaskCard` 自己的 `openNotes` map、**刻意不跨頁記憶**(owner 指定最簡形狀);
  形狀沿用 `AgentDetailPanel` 的 `mp-lastop__toggle`(chevron + 文字的 `<button>`),
  **不是新造的折疊機制**——`<button>` 本來就在卡片 toggle 的 `closest()` 白名單裡,
  所以開備註不會把卡片收起來。
  🔴 **那顆按鈕只長在「有備註」的步驟上,而這是承重的、不是裝飾**:owner 靠這條時間軸
  看「第 4 步做到哪」,收起之後「沒人寫」與「寫了但你看不到」必須分得出來,而**按鈕在不在
  就是唯一的差別**。護欄:`TaskCard.step-note.test.tsx`(逐步斷言 toggle 的有無)+
  `visual-guards/taskcard-note-disclosure.ct.spec.tsx`(真瀏覽器量兩列的高度差)。

## 外包面板 + 外包聊天(M3 Phase 4,SPEC §4;列形 2026-07-14 owner 截圖回報重裁)
辦公室左欄的第二組(`OutsourcePanel`;左欄照 mockup 分「正職/外包」兩組——
正職 header=標籤+計數+摺疊 chevron(OfficePage `staffOpen`),成員卡=名字+
離線徽章+PresenceBadge+未讀數(**聊聊鈕已移除**——Seth 2026-07-13 拍板、蓋過
mockup 與同日「恢復聊聊鈕」舊裁定:該 flex-end 位置只剩未讀 badge,有未讀才
顯示;整列本身仍是聊天入口,行為不變)。**外包列也有未讀 badge**(owner
2026-07-14 截圖回報,蓋過舊「外包無未讀資料源」誠實線):wire
OutsourceWorkerDTO 新增 optional `unread_count`(server 用與 member roster
同一個 UnreadCounts watermark 反相計數注入,spec 已凍結入 openapi.json),
FE 純 passthrough、渲染同 member-card 的紅 pill(>99 顯 99+、count=0 不渲染、
selected+windowActive 壓掉),mock 以 `unreadCountOf` 同規則 live 計算。
資料 = `useOutsourceWorkers`:**只有** `GET /api/outsource-workers` + settings,
訂 `outsource_worker`/`task`/`chat`/`chat_read` topic refetch(四個 topic 同一條
路徑)。**T-a3e4 之前它還會拉 `GET /api/tasks`(不帶 query = 整部歷史)與
`/api/task-manuals`,只為了 join 排序鍵與兩個 label**;T-ec2c 那個「chat delta 只
重抓 workers」的雙路徑就是為了繞過那次下載。現在 `task_no`/`task_created_ts`/
`task_type_key`/`task_type_name` 由 server 附在 worker DTO 上,join 與雙路徑一起
拿掉了——**別再把 task list 的 fetch 加回這個 hook**。**列形(owner
2026-07-14 截圖回報,對齊正職成員卡三行、蓋過 2026-07-13「代號·狀態+識別鍵
chip」舊裁定)**:第一行 **代號 (O-7 式)**(外包唯一的名字);第二行 **接到的
task type + presence 點**(外包沒有角色名,綁定任務的 typeKey 就是它的角色行;
typeKey 空 = 自由代辦字樣);行首那顆點是 **worker 真實 presence**(共用
`LifecycleDot` + `presenceVisual`,五態五色)——**舊的「live worker 恆 online」不變量已於
2026-07-26 由 owner 廢除**(owner 截圖回報:server presence=offline、task=not_started、
無機器的 X-46 被畫成綠點 = 錯的。「在列上」只代表任務未終態,不代表 session 起來了);第三行 **任務代號 (T-xxxx) chip,可點 → `#tasks/<taskId>`
任務頁定位**(同回覆卡「查看任務詳情」的 locate-anchor 路由)。**不顯模型名、
不顯任務標題、不顯識別鍵、不顯狀態字**(狀態看任務頁);排序 = **綁定任務的
created_ts 新→舊**(join 不到才 fallback worker 自己的 mint stamp);任務終態
→ worker 從 wire list 掉出 → 列消失(誠實,不快取)。**左欄空間分配**(owner
2026-07-14:外包區至少同時可見 2-3 列):`.office__members-list` `flex:1` 自身
捲動、`.outsource-panel` `flex:none`、其 list `max-height: min(42vh, 276px)`
內部捲動——正職永遠佔較大比例、外包不再被擠到剩一列。標題列帶「N / 上限」
(-1 顯 ∞)+ 齒輪 → **外包上限設定 popover**(標題+說明+「最多雇用」−/＋
stepper+無限鈕+完成,照 seth-member-2):上限 = `settings.outsource_max_parallel`
(PATCH /api/settings,**-1..20;-1 = 無限、0 = 暫停指派**,面板明示「已暫停
指派」;settings 沒載到 → 誠實只顯 N,不捏上限)。**點列 = 開聊天頻道**:worker 的 `ow-` id 直接
走 `#office/chat/<id>` 同一個 chatId 槽(OfficePage 先查 workers 再 fallback
roster,released 自癒回預設成員聊天);ChatArea 完整重用,以 synthetic Member
+ `headerSub` prop **替換** PresenceBadge——**理由不是「worker 沒有 presence 可顯示」**
(那句已隨上面的不變量一起廢除:worker 有真的 wire `presence`),而是版面裁定
**presence 只在 rail 那一個地方顯示**,chat header 不長第二個 presence 來源,改顯任務行;
worker 詳情 header 則走與 rail 同一顆 `LifecycleDot`;
標題「外包 · 代號」;無詳情面板(不傳 onOpenDetail)、無 unread 計數。

## 設定 › 任務手冊(M3 Phase 4,SPEC §5)
設定 landing 新增「任務手冊」與角色誌並列(`TaskManualsPage.tsx` 的
List/Detail,資料 = `useTaskManuals`:`/api/task-manuals` CRUD,訂 `task_manual`
topic;**手冊編輯 = POST /{type_key} 部分更新**,wire null=不動、assignee
`{}`=解除)。列表 = 類型列(**只顯 type_key**,照 mockup;owner 2026-07-13),**出廠全空**;
新增 = inline row 填 type_key 建**空白手冊**(重複 → 409「這個類型已存在」);
刪除 = 確認 modal,**有非終態任務 → 409** 顯「先讓它們結束才能刪除」。詳情 =
**hub 式層級**(owner 2026-07-13 照 mockup 重裁,取代舊單頁 tabs):breadcrumb
「設定›任務手冊›type」+ 大標題 + **負責成員摘要卡**(icon+「負責成員 · 同類型
所有任務由他負責」+一行設定摘要+編輯)→「任務規劃」段兩張**子頁入口卡**
(任務定義/學習經驗)→ 各自子頁,子頁頂 pill 頁籤可互切;**不顯內部檔名**
(舊裁定仍有效,mockup 的 review-pr.md chip 刻意不做)。任務定義 = 三題引導
(Q1 用途文字/Q2 欄位清單:名稱+必填切換+識別鍵標記**可複合**、可增刪(空名列
commit 時丟棄)/Q3 SOP markdown),編輯模式比照角色誌(編輯/取消/完成;
**無重置**——手冊無 seed,同 custom role 先例);學習經驗可編輯(agent 結案
回寫面);**負責成員編輯 = 成員面板式**(照 seth-ui-3):指定成員/外包全寬
segmented(成員 pick row 右側顯示該成員的**角色 label**,解析順序同
PresenceBadge:i18n seed key → server roleName → raw key;無角色資料誠實省略
——owner 2026-07-13,選人時看得出誰是什麼角色)、模型 = 成員面板同源 MODEL_QUICK_PICKS chips+自由輸入、投入程度 = 低/中/高/最高 segmented、**機器段**(**純機器清單、無「自動分配」列**,狀態字 =
machines.online × monitoring agents 誠實映射:閒置/忙碌/離線;說明「沒選機器或
該機器離線一律不啟動,原因顯示在該外包上」——**離線自動 fallback 的承諾已廢**)、
**雇用數量 = −/＋ stepper+無限鈕**(wire `copies:0` = 無限、`machine:<machine id>`
——必須解析到真機器,`"auto"` 已廢、送了 400;**沒選機器就整個 key 省略、不送 `""`**
——wire 只認非空 id,spec TaskManualDTO)、解除設定 = wire `{}`
→ assignee patch(指派本身一律 server 執行,卡上只設定)。
