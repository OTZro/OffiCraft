---
paths:
  - "src/components/Chat*"
  - "frontend/src/components/Chat*"
  - "src/components/Reply*"
  - "frontend/src/components/Reply*"
  - "src/components/RepliesPage*"
  - "frontend/src/components/RepliesPage*"
  - "src/components/TaskReplyCard*"
  - "frontend/src/components/TaskReplyCard*"
  - "src/components/TaskCardMessageBox*"
  - "frontend/src/components/TaskCardMessageBox*"
  - "src/components/ScheduledMessagesCard*"
  - "frontend/src/components/ScheduledMessagesCard*"
  - "src/components/replies.css"
  - "frontend/src/components/replies.css"
  - "src/hooks/useChat*"
  - "frontend/src/hooks/useChat*"
  - "src/hooks/useReplyCard*"
  - "frontend/src/hooks/useReplyCard*"
  - "src/hooks/useScheduledMessages.ts"
  - "frontend/src/hooks/useScheduledMessages.ts"
  - "src/lib/composerKeys.ts"
  - "frontend/src/lib/composerKeys.ts"
  - "src/lib/autosize.ts"
  - "frontend/src/lib/autosize.ts"
  - "src/lib/chatDraftStore.ts"
  - "frontend/src/lib/chatDraftStore.ts"
  - "src/lib/hashRoute.ts"
  - "frontend/src/lib/hashRoute.ts"
  - "src/api/mock.scheduled-messages.test.ts"
  - "frontend/src/api/mock.scheduled-messages.test.ts"
  - "visual-guards/scheduled-message-*"
  - "frontend/visual-guards/scheduled-message-*"
---

# 聊天、composer、定期訊息、回覆卡、請示↔任務跳轉

> 本檔由 `frontend/CLAUDE.md` 拆出(T-9b5d)。`paths:` 的 glob **相對 `frontend/`**(rules 檔所在目錄),另外並列一組 `frontend/` 開頭的同義 glob 當保險。
> 標 📎 的段落表示「當時的量測證據已搬回該票」,本檔只留規則。

## 定期訊息 · `custom` 頻率 = 四個 EXPLICIT 集合的交集(T-49e7,第二輪加月)

`cadence` 多一個 `custom`,配 `custom_months`(1–12)/`custom_days`(1–31)/
`custom_hours`(0–23)/`custom_minutes`(0–59) 四個整數陣列;**四組的交集**就是送出的
牆鐘時刻,所以它是唯一一天可以送超過一次的頻率。daily/weekly/monthly **一個字都沒動**。

- 🔴 **空集合是伺服器的 422,不是「全部」也不是「永遠不送」**。所以「全選」= 把每一項
  都列出來(12 / 31 / 24 個數字真的送上去),**不是**送一個特殊值。`BLANK_FORM` 的四組
  刻意是**空的**:預設塞滿等於把一條沒人選過的排程放在按鈕旁邊一下就上線。
- 🔴 **交集為空也是 422,而且座艙擋不住它**:四組全部非空、全部在範圍內,交集仍然可以是
  空的(`months{2}×days{31}`——每個值都合法、摘要畫成「每年 2 月 · 每月 31 號」每個字都對、
  一則都不會送)。`incomplete()` 只看四組非不非空,**看不到這件事**,所以這一條純粹由伺服器
  與 `mock.ts` 的 `requireAPossibleDate` 擋。⚠️ 二月一律以 **29** 天計:`months{2}×days{29}`
  是刻意的閏年排程,必須放行——要在畫面上先攔的話,界線也只能畫在同一個地方。
- 🔴 **`custom_months` 是四組裡唯一「省略也有意義」的**:請求裡**整個省略** = 全年
  (第二輪之前的 client 從來不送這個欄位,而它們的排程本來就是每個月都送);明確傳
  **空陣列**仍是 422。`undefined` 與 `[]` 是**兩個不同的請求**,從 `adapter.ts` 的型別
  到 `http.ts` 的 body 組裝到 `mock.ts` 的 `resolveMockMonths` 一路不准塌成一個
  ——路徑上任何一個 `?? []` 都會把每一條「舊形狀」的 create 變成伺服器拒收。
  **回應面沒有這條規則**:server 在 handler 就把它解析掉,送出來的每一列都列出自己的
  月份,所以 `mappers.ts` 的 `?? []` 只是 additive-optional 的地板,不是「省略 = 全年」。
  ⚠️ **座艙自己永遠明確送月份**(`wirePayload`):畫面已經問過了,省略會讓「勾滿十二個」
  與「根本沒被問」變成同一個請求。
- **前端也擋空集合**(`incomplete()`,四組都要非空)。伺服器那道是後盾;讓人按了「建立」
  才知道,那個形狀本身就是缺陷。護欄:`ScheduledMessagesCard.test.tsx` 的
  「blocks a custom schedule with an empty set…」(月刻意排在最後一個才勾——它是 wire 上
  可以省略的那一個,一個「順手」繼承了那個許可的表單會提早放行)。
- 🔴 **列摘要 `cadenceText` 的 `custom` 分支不是裝飾**。它原本是 if-weekly /
  if-monthly / **else-daily**,所以少了那一支,一條一天送 72 次的排程會被畫成
  「每天」。**第二輪多守一半:四組的任何組合都不可以被畫成「每天」**——一條
  {3,6,9,12}×{1}×{9}×{0} 的季度排程,日/時/分那三組看起來再普通不過,只有月份說得出
  它一年只送四次。護欄:同檔的「states a custom schedule's own times in the list
  instead of drawing it as 每天」,**帶一條 daily 對照列 + 一條季度列**。
- **`custom` 不讀 `hour`/`minute`/`day_of_week`/`day_of_month`**,所以那四個
  **不上線**(`wirePayload`),列上也**不印**那個 `HH:mm`。
- 🔴 **四排的標籤是 owner 第二輪逐字選定的:「幾月」「幾號」「幾點」「幾分」**
  (`customMonthsLabel` / `customDaysLabel` / `customHoursLabel` / `customMinutesLabel`)。
  「月份／日期／小時／分鐘」那一組是**被否掉的建議**,不要「順手」改回去。
- 🔴 **分鐘那組預設就是 0、5、10 … 55 十二格,沒有任何要展開的東西**。第一輪把 60 格
  藏在「細部選擇」後面、上面擺一排「每 5/10/15/20/30 分」的間隔捷徑,owner 因此讀成
  「這東西只能設間隔、不能選第幾分」。**兩者都已刪除**(連同 `__quickrow` /
  `__quickbtn` / `__setmore` 三組 CSS):那些捷徑指名的每一個分鐘都在這十二格裡,點數字
  本身就到得了。
  ⚠️ **這是「畫面提供什麼」變窄,不是 wire 變窄**——`custom_minutes` 兩側都還是 0–59
  的閉集,契約一個字沒動。
- 🔴 **既有值不可以被這十二格吃掉**:DB 裡有停在第 7 分的排程(第二輪之前建的、agent 建的、
  或任何直接打 wire 的)。`minuteOptions()` 把不在十二格裡的既有值**多長一格**接上去,
  而且 `NumberSetPicker` 把「提供哪些格子」**在 mount 當下凍結**(`const [offered] =
  useState(options)`)——不凍結的話,把第 7 分取消勾選的那一瞬間那一格會從游標底下消失、
  再也放不回去(**這是 CT 抓到的真缺陷,不是假想**)。護欄:同檔的
  「keeps a stored minute the twelve cells do not offer and saves it back unchanged」
  ——載入 → **什麼都不動** → 存檔 → 逐欄相同。
- **摘要的收合規則(owner 第二輪裁定),三種形狀依序**:①整組全選 → 說人話
  (每個月 / 每天 / 每小時);②**只有分鐘**做等間隔收合 → 「每 N 分鐘」(勾 0、20、40
  就是每 20 分鐘)。判準在 `compose.ts::evenMinuteStep`:**必須從 0 開始、間距固定、
  且整除 60**——{15,35,55} 不算(「每 20 分鐘」講不出偏移量),單一值也不算;
  ③零散 → 最多列 4 個,其餘由「等,另 N 個」承載(**N 是沒被列出來的那幾個**,en 是
  「and N more」)。⚠️ **不要縮回「等 N 個」**(owner 裁定):中文那個慣用法的 N 是**總數**
  (「北京、上海等 3 個城市」= 共 3 個),所以「列了 4 個卻說 2 個」讀起來自相矛盾,而
  英文的「and 2 more」毫無歧義——同一個 N 兩種語言兩種意思。「另」把它釘成餘數,兩語同義。⚠️ **月/日/時刻意不做間隔收合**,owner 給的例子只有分鐘,而日的
  「每 7 天」跨月界根本不成立。
- **版面護欄只有真瀏覽器答得出來**:`visual-guards/scheduled-message-custom-sets.ct.spec.tsx`
  在 320 / 900 兩個面板寬**量矩形**——十二格分鐘在**預設狀態、不展開任何東西**就完整落在
  自己的格線可視框內(320px 實測 grid clientHeight == scrollHeight == 118,4 列;900px
  26,1 列)、每一格的中心點 `elementFromPoint` 回到自己、四組的 top 嚴格遞增、面板與
  頁面橫向溢出皆 0。⚠️ **同一顆壞掉的 sheet 下,把那些斷言換成 class 名 + 數量會全綠**
  ——這類守衛的鑑別力**全部**來自量到的幾何。
- **mock ↔ server parity 有自己的護欄**:`api/mock.scheduled-messages.test.ts`
  (省略 = 全年、`[]` = 422 且不半執行、1–12 範圍、非 custom 不套空集合規則、
  切到 custom 不帶月份 = 全年、切離 custom 保留月份)。
  ⚠️ **刻意沒測的**:月份改動是否 re-aim 游標——mock 的游標字串只由 hour/minute/timezone
  推出、又沒有 tick loop,兩個分支的值逐位元組相同,寫了也只是看起來像覆蓋。那條由
  server 側的 `TestPatchingMonthsReAimsTheCursorOnlyWhenTheyChange` 守。
- ⚠️ **一處已知的 mock↔server 落差,本輪沒動**:非 custom 的 create,server 把送來的
  `custom_days`/`custom_hours`/`custom_minutes` **逐字存下**(`intSliceOrNil`),
  mock 則一律存成 `[]`。月份這一欄本輪照 server 做(走 resolver、非 custom 也存送來的值),
  所以四組在 mock 裡目前**不對稱**。這是既有缺陷、不是本輪造成的,要修是另一件事。
- **這個閉集在前端有很多份手抄副本,加值時每一份都要動**(漏一份就是一條路徑不認得新值,
  而**沒有任何東西會紅**)。刻意不在這裡列它們——清單會過期而不會變色。**當下的完整清單
  自己跑出來**(`api/` 之外也有,例如手寫的 `<option>` 選單):
  ```
  grep -rn daily frontend/src --include=*.ts --include=*.tsx \
    | grep -v '\.test\.\|/stories/\|/visual-guards/\|/generated/'
  ```
  `/generated/` 那幾筆由 spec 重生、不手改;其餘每一筆都要看(散文命中順手排掉)。
- ⚠️ **改 `i18n/locales/*.ts` 的葉子會連帶重生 `server/ocserverd/message_keys_gen.go`**
  (`npm run gen:msgkeys` 一次寫兩個檔,那份 Go 是主題包 wording 白名單的機械孿生、
  標著 DO NOT EDIT)。它跟著 FE 改動一起 commit 是正常的,不是動到後端。

## 聊天未讀跳轉(M2 批次 19;LINE/FB 式,純 FE)
ChatArea 兩個行為,皆不動 server:
- **進房跳第一則未讀**:進對話時 snapshot `member.unreadCount`(**render 同步取**,搶在 listChat「list 即讀」清 watermark 之前——這是 race-free 的關鍵;server 清掉後 roster unreadCount 才歸 0)。第一則未讀 = thread 中 `from===peer && to===owner` 訊息的**最後 count 則之最早者**;其上渲染 `.chat__unread-divider`(「以下是未讀訊息」細線)並 `scrollIntoView({block:"start"})` 頂到視野頂;divider 整個 session 保留(如 LINE)。無未讀照舊落底。ChatArea 換 peer 不 remount → render-time guard 重置 session 追蹤;useChat 於 withId 換時**立即清空 messages**(防舊 thread 殘影 + 防未讀定位錨錯舊訊息)。
- **房內新訊息浮條**:owner 上滾(沿用既有 `nearBottomRef` 判定,80px 帶)時新進 `to===owner` 訊息 → `.chat__new-msg-chip` 灰底 pill 浮在 `.chat__body` 底部;錨點 = 浮條出現後**第一則**未看訊息(session 內以 message-id diff 追蹤,不動 server);點擊 smooth 捲到該則(`[data-msg-id]`);**捲到底才消失**(onScroll near-bottom 清除),點擊本身不清。在底部時維持原自動跟底、永不出浮條。i18n key:`chat.newMessages` / `chat.unreadBelow`(三語)。

## 聊天/回覆輸入框(多行 composer)
三個多行 composer——聊天(ChatArea)、回覆卡(ReplyComposer)、TaskCard 任務訊息
框——都是 **textarea**(共用 `.chat__input`)。**送出決策統一到單一 `lib/composerKeys.ts`
的 `enterShouldSend`**(T-6bad),三處 onKeyDown 都走它、行為永不漂移:
- **桌面**(視窗 >720px):**Enter=送出、Shift+Enter=換行**(不變)。
- **手機**(視窗 ≤720px,`useIsMobile`):**Enter=換行、送出走送出鈕**——手機沒有
  實體鍵盤、shift+enter 不可行,一個裸 Enter 當送出會讓使用者打不了多行(owner
  2026-07-24 回報)。手機 Enter 由 `enterShouldSend` 回 false、handler **不**
  preventDefault,落回 textarea 原生換行。
- **IME 確認 Enter 永不送出**(native isComposing / 229 keyCode / 自家
  isComposingRef 三重 guard,收在 `enterShouldSend` 內),兩環境皆然。

高度隨草稿 auto-grow(`lib/autosize.ts`,useLayoutEffect 綁 draft——打字/送出清空/
失敗還原三路都會重算),CSS max-height(132px ≈ 5 行)封頂、超過走 textarea 自己的
overflow-y 滾動——長草稿永遠看得到全部。

## 回覆卡(等我回覆卡,M2 B2+B3)

兩個入口、一套內裡:`RepliesPage`(等我回覆頁)與 `ChatReplyCard`(聊天串內 inline 卡,訊息帶
`replyCardId` = wire `meta.reply_card_id` 時取代 bubble)都渲染**共用的 `ReplyCardBody.tsx`**
+ 共用 `ReplyComposer`——兩面永不漂移。同步 = reconcile-by-refetch:兩側都訂 `reply_card`
topic;聊天卡另走 `GET /api/reply-cards/{id}` 單卡 refetch。
📎 **請求數/位元組對照表、DOM 逐位元組對照、改前改後的量測母體已搬回 T-a3e4;list wire
輕量化那半的 MCP/conformance 細節搬回 T-3f31。** 這裡只留規則。

🔴 **一次 owner 動作只准觸發一輪重抓(T-a3e4)**:`useReplyCards` 的 answer / reanswer /
expire **不自己 refetch**——但**它們一定要自己對帳**:三個寫入端點都回傳那張新鮮的卡,所以
動作路徑**採用自己寫入的回應**(`adoptWrite`,**零請求**)。
🔴 **光採用回應還不夠——in-flight 的 PRE-WRITE 快照會把卡片畫回去,所以採用之後那個 id 要被
「按住」直到某個 server 快照同意。兩個 pane 各有一份保留、release 規則相反**:waiting
(`heldFromWaitingRef`)= 快照**不再列出**該 id 才放行;handled(`adoptedHandledRef`)=
快照列出它**且 handled 戳記不比我們的舊**才放行(重新決定會重新蓋戳,所以「有出現」不等於
確認)。**一條 release 規則服務不了兩個 pane。** 觸發條件只需要「點擊前不久有一則 delta 到過」
= **串流剛剛還活著、然後掉了**;T-e862 的 generation guard **救不了這一格**(它只在「有更新的
refetch」時丟掉舊快照,串流斷掉時根本沒有更新的那一個)。
⛔ **不准用「把那份 in-flight 快照整個丟掉」換綠**:那會連同快照裡**別人剛開的新卡**一起丟掉,
而串流已斷 ⇒ 那張卡可能一直不出現、**而且沒有任何訊號** = 用一個靜默失敗換另一個靜默失敗。
按 id 保留才對:快照其餘內容照常採用。`handledCount` 因此要把「被按住的張數」加回去。
🔴 **本檔上一版寫「delta 是唯一的 reconcile trigger」,那句對動作路徑是錯的、而且是個
production blocker**:它把座艙的正確性押在一個**可有可無的即時事件**上——EventSource 斷線或
漏一帧時,server 已經收下答覆,等我回覆頁與導覽列徽章卻還把那張卡畫成等待中,owner 再點一次
就吃 409。**「SSE 斷線時卡片不再就地翻面」不是已接受的交換,別再引用它。** delta 仍然是
**別人**的寫入的 reconcile trigger,而它對 owner 自己的寫入**也會來**。
⚠️ **`refresh()` 不在此列、仍無條件重抓**:它的 caller 是 409(卡已被別處處理),那是別人的
寫入,沒有自己的 delta 會來。
⚠️ 舊註解說動作路徑的 refetch 是「為了讓 mock 行為一致」——mock 長出 `emitTopic` 之後那句就
不成立了,**過時的理由正是這個重複活下來的原因**。

🔴 **同一條規則的第三個站點:`ChatReplyCard` 的單卡重複。** `doAnswer` 現在**採用
`answerReplyCard` 的回應**(zero request),不再自己 refetch。
⚠️ **但 `doReanswer` 的 refetch 保留,而且不准「順手對齊」成同一個形狀** —— 這個不對稱是
T-cdf4 guard 逼出來的:重新決定作用在**已回覆**卡上,而那道 guard **刻意**把終態卡的 delta
丟掉(那正是「70+ 張歷史卡不會每張都重抓」的來源),所以 SSE 路徑**不會**觸發,動作路徑是
那張卡唯一的更新來源。拿掉它 = owner 被留在**舊答案**的畫面上。
🔴 **而「斷線時仍就地翻面」是有條件的,別再寫成無條件定論**:它成立要**兩件事同時在**——
(a) `doAnswer` 採用寫入回應,(b) 那次採用會讓**所有還在飛的讀取失效**(`readGenRef`,
`commitCard` 推進世代)。缺一即假。而且這句只講**這個元件的這張卡**,等我回覆頁那兩個 pane
要靠自己的機制。
🔴 **同一個類別在回覆卡一共五個站點**:`useReplyCards.answer/expire`(pane+徽章)、採用之後的
in-flight waiting 快照、inline `ChatReplyCard`、`TaskReplyCard`(兩者都是單卡 `getReplyCard`
沒有世代守衛)、handled pane。**判準只有一句:某個 async 讀完之後寫回本地狀態,而那次寫入
可能比某個本地已採用的真相舊 ⇒ 它需要世代守衛或按 id 保留。**
🔴 **理由要寫對**:one-round 那條斷言是**精確等於 1**,**0 不滿足它**。真正的原因是**射程**:
那個預算量的是「串流暢通時花幾輪」,所以它對「串流斷線」這個情境**無話可說**,斷線那半必須
另有證人。成本與正確性各一個證人,任一條都不能代替另一條。
護欄兩層、守的不是同一件事:`components/ChatReplyCard.one-round.test.tsx`(**數呼叫**、不看
畫面)+ `hooks/useReplyCards.sse-loss.test.tsx`(**看畫面**、把 `subscribeEvents` 換成 no-op)。
🔴 **「寫入回的是整張卡」這個前提有 server 側證人**:`api_replycards_writeecho_test.go` 斷言
三個動詞的**回應 body** 與那張卡自己的 `GET /api/reply-cards/{id}` **逐位元組相同**(identity
而非欄位清單——清單會在 DTO 長新欄位的那天過期)。**語料必須有 body + options + 綁任務的卡**,
否則兩個投影長得一樣、比較等於沒比。

✅ **逐張 hydrate 的 N+1 已經沒了(owner 2026-08-02 核准 `?view=full`)**——`GET
/api/reply-cards?view=full` 一個請求回**整個 pane 的全卡**(逐位元組等於每張卡自己的單卡
GET,由 server 端測試釘住)。
- 🔴 **價值在往返次數,不在流量,別講錯**:所以 `http.view-full.test.ts` **只數請求、沒有任何
  位元組斷言**(那會暗示一個幾乎不存在的節省)。
- **①② 是同一個修改點**:waiting pane 與近期已處理 pane 都走 `listReplyCards`。**收合時零成本
  那個 gate(`handledLoaded`)沒有被碰**,別動它。
- ⚠️ **`view` 只活在 http seam,不是 adapter 概念**:mock 本來就出全卡,parity 在 adapter 層
  不變。**別把 `view` 提到 adapter 簽章上。**
- 🔴 **agent 面一個位元組沒變**:`view` **刻意不在** `list_reply_cards` 的 MCP inputSchema 裡
  (登記在 `deliberatelyOffMCP`,不是 `knownCatalogDrift`——那份是「該補的債」,填錯欄會招來
  下一個人「把它廣告出去」)。理由:輕量列就是 owner 裁定的 agent 契約(T-3f31),給 agent 一個
  一次拉整個 pane 全卡的把手,等於把 T-3f31 縮掉的還回去。conformance
  `test_view_is_not_advertised_to_agents` 對**線上** tools/list 釘這條。
- **回應 schema 是聯集**(light 列 | 全卡),因為同一條路由服務兩種投影——owner 在知情下選了
  「規格誠實」這一邊,代價是 `ocapi_gen.go` 多一個沒有 caller 的 union wrapper。

**list wire 輕量化(T-3f31)**:`GET /api/reply-cards` 的**預設**仍只回輕量摘要
(summary+決策 digest,無 body/options 全文),`?view=light` 與不帶參數逐位元組相同;不認得的
`view` 值回 **400 並點名兩個合法值**(默默落回 light 會讓一個打錯的字**無聲**恢復逐列 fan-out
——那正是這顆在修的成本;這是刻意偏離 `?view=list` / `?fields=light` 兩個先例的靜默落回,
owner 知情未反對)。

**跳到原訊息** = `#office/chat/<id>/msg/<msgId>`(hashRoute `msgId`)→ ChatArea `jumpToMsgId`
定位(center scroll)+ `chat__msg--located` 高亮 flash;one-shot、消費掉 entry positioning
(不與未讀 divider 打架);目標超出載入窗(recent 30)誠實 fallback 落底。
**徽章(待回覆數)與聊天未讀紅點是兩個獨立訊號**:回卡不清紅點,紅點只有進對話才清。

**已過期終態(T-1aa4)**:waiting 卡 head 有「標為過期」次要鈕(`ConfirmModal` 二次確認——
終態、不可復原、不算回答)。⚠️ **座艙這顆鈕是 owner 面的入口,但「owner 專用」這個字面已經
不成立**:T-6020(owner 2026-07-26)起 admin 助理也按得動,owner 2026-08-07 於卡
`rc-3ff94b116970`(T-1b88)又把**該卡的作者本人**加進 API 層(expire 的 floor 降到 `agent`,
handler 的 `callerMayExpireCard` 認 owner／admin 助理／`ReplyCard.FromMember` 本人)——所以
一般 agent 現在能自己撤回**它開的、還沒被回答的**卡;別人的卡 403、已被回答的卡 409(包含
owner)。`ReplyCardBody` 第三個內裡 `ReplyCardExpiredBody`(灰 tag + 選項靜態 review,無 chips
可點、無重新決定),三個渲染面(RepliesPage/ChatReplyCard/TaskReplyCard)共用,collapsed stub
的 tag 分「已回覆/已過期」。
等我回覆頁第二 pane 是**「近期已處理」**(answered+expired 併列、各自 24h 窗、handledTs
新→舊;header N = count.answered+count.expired);`useReplyCards` 出
`handled/handledCount/expire`,`api.expireReplyCard(id)`(mock 鏡像含 step/task hold 釋放)。
status union 全線(adapter/mappers join)= waiting|answered|expired。

## 請示 ↔ 任務跳轉(M3 Phase 4,SPEC §3.6)
`ReplyCard.task`(wire `ReplyCardDTO.task` = TaskRefDTO,mapper 恆置
null-when-absent;view 欄位 OPTIONAL 保測試 fixture,先例 Member.roleName)。
任務衍生的請示卡(task 非 null)在 RepliesPage 與 ChatReplyCard 都顯**精簡任務
資訊 row**:任務標題 +「查看任務詳情」——**不露任務編號/識別鍵**(裁定);點 →
`#tasks/<taskId>`(hashRoute 新 `taskId` 段)。純聊天請示無此 row。
🔴 **類型 badge(typeKey;"" → 自由代辦)已在 T-ee17 驗收整顆移除**(owner
2026-08-14 圈住那顆 chip:螢幕上一串 `tm-05f7c776d6ff` 這種內部代號答不出任何
標題答不出的事)。`task.typeKey` **仍留在 DTO 上**(別人可能在用)——這只是不再
顯示,不是把欄位拿掉;`t.replies.taskBadge` 因此成孤兒 key、已刪(`t.tasks.adhoc`
有別的使用者,留著)。
🔴 **這一列在卡片最上面,就在卡頭底下、正文之前**(同一輪驗收,owner:「這個不能
夠放到最一開始嗎?」)——擺在底部時,要讀完整張卡才知道這是哪件工作的問題。
**兩個畫面同一批搬**(這正是這個元件被抽出來的理由);順序由 **DOM 順序**斷言守
(`RepliesPage.test.tsx` + `TasksPage.jump.test.tsx`),版面那半由真瀏覽器的
`visual-guards/reply-task-title-truncate.ct.spec.tsx` 在 320/390 量幾何——兩層
mount 的都是真元件,所以把它搬回底部兩層都會紅(實測 jsdom 紅 2、CT 紅 2)。TasksPage 端 = **settle loop**(每個 effect pass 修一件事再
re-run):終態目標 → 自動展開已結束;**錨點直接壓過三個篩選軸**(`matches` 對
`taskIdFilter` 短路成 `task.id === taskIdFilter`,不是逐維度去清)、那一張則由
`useTasks` 的 `anchorTaskId` 單張補抓進來(見上方任務頁節);card 進 DOM →
scrollIntoView + `task-card--located` 高亮 flash(2.6s)→ **消費 anchor**(route
退回 `#tasks`,one-shot,可重跳);未知/過期 id 誠實自癒(消費 anchor、不高亮
——但**必須等 `anchorPending` 落定**,不然「還沒載到」會被當成「不存在」)。
