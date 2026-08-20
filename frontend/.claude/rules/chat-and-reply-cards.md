---
paths:
  - "src/components/Chat*"
  - "src/components/Reply*"
  - "src/components/RepliesPage*"
  - "src/components/TaskReplyCard*"
  - "src/components/TaskCardMessageBox*"
  - "src/components/ScheduledMessagesCard*"
  - "src/components/replies.css"
  - "src/hooks/useChat*"
  - "src/hooks/useQuotedMessages*"
  - "src/hooks/useReplyCard*"
  - "src/hooks/useScheduledMessages.ts"
  - "src/lib/composerKeys.ts"
  - "src/lib/autosize.ts"
  - "src/lib/chatDraftStore.ts"
  - "src/lib/hashRoute.ts"
  - "src/api/mock.scheduled-messages.test.ts"
  - "visual-guards/scheduled-message-*"
---

# 聊天、composer、定期訊息、回覆卡

## 定期訊息

custom cadence 的 custom_months、custom_days、custom_hours、custom_minutes 是四個顯式集合的交集。每組都要有合法值，交集為空由 server 與 mock 拒絕；前端也要先擋空集合。custom_months 省略代表全年，明確空陣列仍是 422，所以表單送出時要明確帶目前選中的月份。

custom 不讀 hour、minute、day_of_week、day_of_month，也不把自己摘要成 daily。顯示標籤固定為「幾月」「幾號」「幾點」「幾分」；分鐘預設提供 0、5…55，但既有不在選項中的值要保留並可原樣存回。摘要只對從 0 開始、固定間距且整除 60 的分鐘集合說「每 N 分鐘」，零散值列出前幾項後用「另 N 個」。

這組分鐘格的護欄要用真瀏覽器量實際格線、中心點與頁面／面板 overflow；只數 class 或元素數量會在壞版面上照樣綠。mock 與 server 的存取規則也要分開對帳；新增非 generated 的閉集副本時，先由 source query 找全，不要把會過期的手抄清單寫進本檔。

## 聊天與 composer

進房時先在 render 同步 snapshot member.unreadCount，再由 listChat 清 watermark；第一則未讀是對方送給 owner 的未讀訊息中最早的一則，顯示 divider 並保留於本 session。房內新訊息只有在 owner 不在 near-bottom 時顯示浮條；點擊只跳到第一則，必須真的捲到底才清除。

ChatArea、ReplyComposer、TaskCard 訊息框都是 textarea，送出決策只由 lib/composerKeys.ts 的 enterShouldSend 提供：桌面 Enter 送出、Shift+Enter 換行；手機 Enter 換行、按鈕送出；IME composing 永不送出。autosize 上限 132px，超出由 textarea 自己捲動。

## 回覆卡

RepliesPage 與 ChatReplyCard 共用 ReplyCardBody、ReplyComposer；兩者訂 reply_card，inline 卡另做單卡 refetch。answer、expire 與 waiting-pane 的 owner 動作採用寫入端點回傳的新卡，不再重抓；採用後按 id 保留，直到 waiting 快照不再列出它，或 handled 快照帶著不舊於新狀態的 handled 戳記。其他卡照常採用，不能丟整份快照。refresh() 仍無條件 refetch。

ChatReplyCard 的 doReanswer 保留單卡 refetch：終態 delta 可能被刻意丟棄，拿掉會留下舊答案。不要把它和 doAnswer 對齊，也不要把「delta 是唯一 reconcile trigger」寫回規則。

view=full 只在 HTTP list seam 表示整個 pane 的一次請求，不上提到 adapter，也不向 agent 的 MCP tools/list 宣傳；否則 agent 會拿到一次拉整個 pane 的昂貴把手，抵消輕量摘要契約。light/default 行為不變、未知 view 回 400。等待卡的 expire 規則以 server 為準：owner/admin 或卡片作者可過期自己的 waiting 卡；其他人 403，已回答 409。

hash route #office/chat/<id>/msg/<msgId> 只做一次定位與 highlight；若訊息不在最近窗口就誠實落到底。回覆卡的 red badge 與聊天未讀互不清除；任務關聯卡共用卡身，只顯示任務標題與查看詳情連結。

## 「回覆這則」（T-4e95）

**回覆關係只有 id，沒有任何拷貝。** owner 裁定：被引用那句話已經在它自己的
message id 底下，複製一份在每則回覆旁邊就是同一句話的第二個住處。要顯示引用
內容，先從已載入的 `messages` 找，找不到才用 `useQuotedMessages` 走 by-id 讀
回來 —— 不要為了省一次請求就在 wire 上加摘要欄位。

**引用有三態，不准壓成兩態**：還沒解決（`undefined`）、問過但沒拿到
（`null`）、拿到（物件）。第二態要顯示「較早的一則訊息」這種誠實落空，第一態
才是暫時的。把兩者畫成同一個樣子，使用者就分不出「還在找」和「找不到」。

**取消回覆的 x 只清回覆對象。** 不准順手清 `draft`、不准清
`pendingAttachments`：取消回覆不是取消訊息。這條有測試釘住
（`ChatArea.reply-to.test.tsx`）。

**送出後一定要清掉回覆對象**，否則它會默默黏在下一則訊息上。

**`useQuotedMessages` 的 effect 不准用 cleanup 取消飛行中的請求。** 「已問過」
記在 ref 裡而且不觸發 re-render，所以下一次 render（composer 打一個字就夠）會
把 effect 的 key 算成空、React 跑上一個 effect 的 cleanup —— 取消掉的請求永遠
不會再發一次，引用就卡在「…」。要防的只有 unmount 後寫 state（T-4e95 review
B1，已有會紅的測試）。

**只要這一則回覆得動，就要有回覆入口**，包含你自己發的與只有附件的。氣泡的右上
角是共用的 action slot（回覆＋放大閱讀），寬度依控制項數量預留，不要改成 hover
才進版面 —— 那會讓滑過去時氣泡橫向跳動。

**例外有兩個，形狀不一樣，不要混為一談：**

1. **請示卡那種訊息 —— 入口還在，只是換位置。** 它的氣泡被 `<ChatReplyCard>`
   換掉了，那是一整片有自己 header 控制項的表面，浮一顆按鈕上去會撞在一起，所以
   入口留在列上。回得動，只是不在角落。
2. **agent 之間的訊息與 server 寫的「系統」訊息 —— 連入口都沒有。** 這是
   `replyable` 擋掉的：一則回覆只寄得到 {owner, peer}，server 會 400 掉指向別的
   會話的 `reply_to`，而 composer 失敗時只有一行 console.warn —— 訊息會消失、
   橫幅還留著、什麼都不解釋。**必定失敗的入口比沒有入口更糟**，所以這裡是真的
   不給，不是換位置。

兩條都有測試釘住（`ChatArea.reply-to.test.tsx`）。
