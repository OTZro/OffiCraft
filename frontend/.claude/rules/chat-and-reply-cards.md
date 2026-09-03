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
  - "src/hooks/useReplyCard*"
  - "src/hooks/useScheduledMessages.ts"
  - "src/lib/composerKeys.ts"
  - "src/lib/autosize.ts"
  - "src/lib/chatDraftStore.ts"
  - "src/lib/hashRoute.ts"
  - "src/api/mock.scheduled-messages.test.ts"
  # 🔴 THE WIRE LAYER IS IN SCOPE, and it was not. The T-4e95 rule below —
  # "the quote content is assembled by the SERVER; the mock says the same thing;
  # the frontend never shortens it again" — is IMPLEMENTED in these three files
  # and nowhere in src/components. Whoever edits the mock is exactly the person
  # this rule is written for, and they could not see it.
  - "src/api/mappers.ts"
  - "src/api/mock.ts"
  - "src/api/adapter.ts"
  # 🔴 THE WAKE SNAPSHOT IS A RENDERING SURFACE FOR THE QUOTE TOO, and it was
  # not listed either — which is exactly why it shipped for months billing the
  # quote's characters to the chat budget and drawing none of them (T-9871).
  # Whoever edits this card is one of the people the quote rules below are
  # written for.
  - "src/components/ResumeSummaryCard*"
  - "src/components/member-detail.css"
  - "visual-guards/resume-chat-quote*"
  # The guard's fixture and the mock's own witness are edited by the same people
  # and are just as easy to get wrong; a rule nobody sees while editing them is
  # the failure this whole block is about.
  - "visual-guards/stories/ResumeChatQuoteStory*"
  - "src/api/mock.reply-to.test.ts"
  - "visual-guards/scheduled-message-*"
  # 多選卡的版面護欄與它的 story —— 改晶片/暫存態/送出列的人就是這條規則要找的人。
  - "visual-guards/reply-multi-select*"
  - "visual-guards/stories/ReplyMultiSelectStory*"
---

# 聊天、composer、定期訊息、回覆卡

## 定期訊息

custom cadence 的 custom_months、custom_days、custom_hours、custom_minutes 是四個顯式集合的交集。每組都要有合法值，交集為空由 server 與 mock 拒絕；前端也要先擋空集合。custom_months 省略代表全年，明確空陣列仍是 422，所以表單送出時要明確帶目前選中的月份。

custom 不讀 hour、minute、day_of_week、day_of_month，也不把自己摘要成 daily。顯示標籤固定為「幾月」「幾號」「幾點」「幾分」；分鐘預設提供 0、5…55，但既有不在選項中的值要保留並可原樣存回。摘要只對從 0 開始、固定間距且整除 60 的分鐘集合說「每 N 分鐘」，零散值列出前幾項後用「另 N 個」。

這組分鐘格的護欄要用真瀏覽器量實際格線、中心點與頁面／面板 overflow；只數 class 或元素數量會在壞版面上照樣綠。mock 與 server 的存取規則也要分開對帳；新增非 generated 的閉集副本時，先由 source query 找全，不要把會過期的手抄清單寫進本檔。

## 聊天與 composer

進房時先在 render 同步 snapshot member.unreadCount，再由明確的 mark-read 清 watermark（listChat 不再有這個副作用，T-48）；第一則未讀是對方送給 owner 的未讀訊息中最早的一則，顯示 divider 並保留於本 session。房內新訊息只有在 owner 不在 near-bottom 時顯示 LINE 式預覽列（寄件者＋一行內容），點擊落在**最新那一則**（T-48 前是落在第一則未見，下面還壓著沒看到的訊息）；落點**不做事後校正**（T-48 owner rc-6c27f486ef9d 具名接受「上方晚載入的內容把目標擠走」，`scrollToLatest` 只捲一次），但那句裁定點名的圖片與請示卡今天都在**來源**擋掉了——縮圖有固定框，等待中的卡在 commit 之前就握在手上（`lib/threadCommit` → `lib/replyCardCache`），被接受的是其餘晚到內容的位移；必須真的捲到底才清除。**最新那一則不在視野內**且**沒有**新訊息時顯示回到最新箭頭，兩者不同時出現（箭頭讓位給預覽列，owner rc-72054864ff88）。⚠️ 判準量的是**最新那一列的底邊**（`lib/scrollToLatest` 的 `isLatestRowInView`），不是「容器捲到底了沒」：`.chat__messages` 是 gap 的 flex 欄且最後一列下面還有零高哨兵，所以容器底部永遠在最新那一列底下一個 gap，用容器問會在最新訊息完整可見時答「不在」（T-48：按了箭頭它不會消失，每一次）。任何把它換回 `scrollHeight - scrollTop - clientHeight <= 某個常數` 的寫法都會把那個 bug 帶回來，而且 gap 一改就沒有人會紅。

ChatArea、ReplyComposer、TaskCard 訊息框都是 textarea，送出決策只由 lib/composerKeys.ts 的 enterShouldSend 提供：桌面 Enter 送出、Shift+Enter 換行；手機 Enter 換行、按鈕送出；IME composing 永不送出。autosize 上限 132px，超出由 textarea 自己捲動。

## 回覆卡

RepliesPage 與 ChatReplyCard 共用 ReplyCardBody、ReplyComposer；兩者訂 reply_card，inline 卡另做單卡 refetch。answer、expire 與 waiting-pane 的 owner 動作採用寫入端點回傳的新卡，不再重抓；採用後按 id 保留，直到 waiting 快照不再列出它，或 handled 快照帶著不舊於新狀態的 handled 戳記。其他卡照常採用，不能丟整份快照。refresh() 仍無條件 refetch。

ChatReplyCard 的 doReanswer 保留單卡 refetch：終態 delta 可能被刻意丟棄，拿掉會留下舊答案。不要把它和 doAnswer 對齊，也不要把「delta 是唯一 reconcile trigger」寫回規則。

## 選項是一組集合,不是一個位置(T-40)

**「AI 建議」由每個選項自己的 `aiPick` 攜帶,位置不帶任何意義。** 舊碼在三處寫死
`idx === 0`(晶片、最終答案列、`ResumeSummaryCard` 的履歷卡),那是一個碼上沒有
任何東西在執行的約定 —— 改一次選項順序就悄悄改掉了 AI 的建議是哪一個。**測資把
`ai_pick` 放在第一個就等於沒測**:位置讀法在那種測資上永遠蒙對。

**答覆是一份清單。** `ReplyCard.selectMode` 是 `single`|`multi`(與 `kind` 正交:
`kind` 說 owner 要做什麼,`selectMode` 說可以圈幾個)。晶片是**暫存**的 ——
single 點第二個取代第一個、multi toggle,再點同一個一律取消(「什麼都沒勾」必須
到得了,因為那正是送出鍵拒絕送的狀態)。**選項與自由文字由同一顆送出鍵合成同一次
送出**:卡片是一次性關閉,分兩條各自送的第二條必吃 409。

⚠️ **`optionIdxs: []` 不是「沒圈」,它是 server 刻意的 400。** 沒圈就**省略**這個
欄位;把空清單攤平成 `null` 會讓那個 400 永遠打不到,把它照原樣送出則會讓每一次
純文字答覆變成錯誤。索引在 `http.ts` 的 seam 去重＋升冪 —— 勾選順序不同不可以變成
兩份不同的 body。

⚠️ **有兩條互不相干的答案線,而且不共用任何型別。** 卡片本體走
`ReplyCard.answer.optionIdxs`;履歷摘要卡走完全獨立的
`ChatInlineReplyCardView.answerOptionIdxs`(`adapter.ts` → `mappers.ts` →
`ResumeSummaryCard.tsx`)。**只改一條,tsc 一聲不吭,履歷摘要卡會安靜地顯示零個
「已選」** —— 這和上面「引文有不只一個渲染面」是同一張卡上的同一個陷阱,而且它已經
發生過一次。顯示「你選的」的面共五個:最終答案列、「目前」標記(這兩面由三個 wrapper
共用同一份 `ReplyCardBody`)、`TaskReplyCard` 的收合一行、`ResumeSummaryCard` 履歷卡;
第五個 `ChatReplyCard` 收合列只印 summary,不讀答案。

🔴 **前端這半沒有任何機械保護**(2026-08-31 配陽性對照確認):`api/dtoParity.ts`
不含 ReplyCard、style ownership 的 `OWNED_SHEETS` 不含 `replies.css`、payload
parity 的 roll-call 不列卡片內部欄位。守著這一切的只有
`ReplyCardBody.multi-select.test.tsx`、`TaskReplyCard.test.tsx`、
`ResumeSummaryCard.payload-parity.test.tsx` 與 `http.mutations.test.ts` 裡那幾條。

class 名 `.reply-tag--ai` / `.reply-option--ai` **不要改**:`TaskReplyCard` 借用前者
畫自己的徽章。

view=full 只在 HTTP list seam 表示整個 pane 的一次請求，不上提到 adapter，也不向 agent 的 MCP tools/list 宣傳；否則 agent 會拿到一次拉整個 pane 的昂貴把手，抵消輕量摘要契約。light/default 行為不變、未知 view 回 400。等待卡的 expire 規則以 server 為準：owner/admin 或卡片作者可過期自己的 waiting 卡；其他人 403，已回答 409。

hash route #office/chat/<id>/msg/<msgId> 只做一次定位與 highlight。產生它的是「請示」頁的**跳到原訊息**與任務卡內嵌回覆卡的**在聊天室回覆**（外加使用者自己留存的舊 URL）；聊天氣泡引用列的**看原訊息**不走這條，它撈那一則開覆蓋層（見下方「看原訊息」一節）。⚠️ T-0b78 曾把那兩顆也改成覆蓋層，owner 2026-08-29 裁定「1 跟 2 變回去原本那樣」—— 所以不要順手把它們改回覆蓋層。

**目標不在最近視窗時是撈，不是落到底（T-48，取代上面那一段原本的「知情接受」）。** owner 後來把那個暫緩解掉了（「都可以正確定位到該訊息」），所以這條路現在是：**進房當下就以那個 id 開窗**——`useChat(peer, jumpToMsgId)` 收到 anchor 就**完全不載最新那一頁**，ChatArea 的 jump reactor 從一個空 thread 直接打 `loadAround`（`?end_id=` 往舊、`?start_id=` 往新，兩端都含，兩頁而已，不是整條歷史）。⚠️ 這條鏈有一個**沒有機械保護的不變量**：anchor 被指定時**一定要有人真的去撈**，否則房間永遠空白；今天唯一的撈家是那個 reactor，它的 miss 分支再退回 `resetToLatest`。

`loadAround` 回的是**三態**（`JumpOutcome`），不是 bool：`found` / `missing`（404、失敗、或**存在但屬於別條對話**——server 解析錨點不套 participant 過濾，那種 id 兩個請求都回 200＋空陣列，採用它會把聊天室寫成空白）/ `superseded`（被更晚的載入超車，**訊息還在**）。**不要把 superseded 併回 missing**：那會對著一則還在的訊息說「可能已經被清掉了」，而且跳轉閂已經用掉，沒有重試也沒有按鈕。三態各有自己的畫面語言（`chat.jumpTargetMissing` / `chat.jumpTargetInterrupted`），重排有上限。

錨點視窗期間 `hasNewer=true`，這時**不標已讀**（`mayMarkRead`）、**不跑週期性/SSE 的最新頁載入**（把活尾巴併進歷史視窗會造出一段沒人撈過卻被畫成相鄰的縫）。往新的那條鏈是 **level-triggered**：捲到底由捲動事件**起頭**，但每一頁落地之後由一個 effect **重新評估**（`forwardWalkArmed` 且 `hasNewer`）決定要不要再撈一頁——不能只靠捲動事件，因為一頁貼上去之後畫面已經在底部，不會再有事件（T-48：實測停在 61/80 列、空等 10 秒不動、補一次捲動立刻補齊）。⚠️ **那個 effect 不准再量一次幾何**：往新的一頁是**貼在下面**的，30 列一落地底部就退開一個螢幕以上，所以「還在底部嗎」在每一頁落地當下必然答不是；它之所以大多還是走得完，只是因為 auto-follow 的 `scrollIntoView` 通常在同一拍先把畫面拉回底部。那個順序沒發生時走廊就死在半路，而且沒有 spinner、沒有結束標記（CI run 33794983804、macos-e2e 390 寬：rows 32 → 61、`scrollTop` 凍在貼上去之前的最大值 2702、一個 `?start_id=` 之後五秒內再無請求）。**「人不想走了」的訊號是方向，不是距離**：畫面**往上**走只有人做得到（貼一頁只會把底部往下推），所以停手由 `onMessagesScroll` 比對上一次的 `scrollTop` 決定。⚠️ jsdom 的每一個長度都是 0，量幾何的寫法在單元測試裡永遠是綠的；`ChatArea.anchor-entry.test.tsx` 的「一頁貼上去把底部推遠時」自己鋪了一份 81/369 的假版面，就是為了讓那個寫法紅。它的界是「整頁都是已有列的那個錨點不再問第二次」——**只涵蓋這一種**沒有進展的結局。**請求失敗不走這個界**：失敗只是沒有東西落地、沒有 re-render，所以鏈自己停下來（`catch` 一個字都沒寫進那個 ref）。捲動事件那條路帶 `human: true`，**進來清掉這個界，但每 400ms 最多一次**，所以「人再捲一次就是重試」是真的——這句話在 2026-09-03 之前是假的（獨立審查 #17 F-1 實測：停住之後再捲三次，零個新請求），當時捲動與 effect 走的是同一道門。那個節流不是門檻而是速率上限：一次滾輪是每秒約 60 個事件，不節流實測會變成**每秒 20 個請求**（獨立審查 #18 A-2），單次滾輪仍然立刻重試。回到活尾巴的路有兩條：往下捲 `loadNewer`（有世代票，晚到的一頁要丟掉，不然歷史會被接在最新後面而且 `hasNewer` 會翻回 true ⇒ 那條對話從此不標已讀）、或 `resetToLatest`（**取代**不是合併，並且負責解除 anchor 的載入 hold-off——不解除的話那間房從此不再刷新）。

回覆卡的 red badge 與聊天未讀互不清除；任務關聯卡共用卡身，只顯示任務標題與查看詳情連結。

## 「回覆這則」（T-4e95，owner 2026-08-21 改設計）

**引用內容由 server 隨每次讀取現組，前端只讀不找。** 每則 `reply_to` 非空的
訊息，server 會在**每一個**讀取出口（listing、history page、`?ids=`、POST 回
應、wake snapshot）附上
`reply_to_chat = {id, from, from_name, to, to_name, content}`。前端
畫引用列就是讀 `m.replyToChat`，**沒有查表、沒有 fallback、沒有補撈**。

**引用列畫的是「寄件者 → 收件者」，而那個收件者是被引訊息自己的 `to`。**
不是這條線的對方 —— 引用可以跨對話（2026-08-21 裁定），兩者剛好在那個情況下不
一樣，而那正是這個欄位存在的理由。`nameOf` 本來就會退到原始 id，所以兩邊永遠有
字可印：**沒有空白態、沒有「未知」佔位字**。`from_name` 與 `to_name` 同一組規
則：只有 wake snapshot 那條讀法會填，其餘一律 `""`，要指認人一律用 id。
composer 上方的橫幅走同一個形狀（它從已載入視窗解析，是第二條到同一句話的路）。

**引用列與橫幅都是兩行：第一行「寄件者 → 收件者」，第二行被引的那句話。**
（owner 2026-08-22 裁定）原本兩者擠在同一行，而 flex 的收縮順序讓句子先讓位，所以
「→ 收件者」一加上去就等於直接從句子身上扣寬度：實測 vw=721（app shell 的成員欄
把 pane 砍到 347px）時名字那半 101px、句子那半只剩 18px —— 本機 3/61 字、CI runner
0/61 字。**兩行是把競爭拿掉，不是去仲裁它**：句子獨占一整行之後，任何長度的名字都
拿不走它的寬度。代價是垂直空間（引用列 20.8px→36.7px，橫幅 34px→42.4px）。
名字那半在極窄 pane 仍然只是 `text-overflow: ellipsis` 截短（同一個 span、同一個
箭頭、同一個順序），**不是第三種畫法**。

**引文有不只一個渲染面，改動要先把它們找齊**——它們是各自獨立的碼路（訊息列與
喚醒快照卡各自讀 `replyToChat`，composer 橫幅讀 `messageById`），只改其中一面
會讓同一句引文在不同地方長得不一樣，或在某一面乾脆不出現。
**不要記數量，也不要照抄清單**：這一行原本寫死「兩個渲染面」，而當時已經有第三
個（`ResumeSummaryCard` 的快照卡）——那張卡的預算**一直在為引文的字計費**，畫面
上卻一個字都沒有，而規則的 `paths:` 也沒有涵蓋它，所以改引文的人看不到這條規則，
被漏掉的那一面也不會有任何東西變紅（T-9871）。
**找齊的方法是從資料查回來，而不是相信這裡的列舉**：以 view model 的欄位為起點
`grep -rn "replyToChat\|messageById" frontend/src`，命中的每一個 component 都是
一個面；新增一個面時，把它加進本檔的 `paths:`，否則下一個人同樣看不到。

**但「一起改」不等於「長一樣」。** 快照卡不在 `chat-pane` 這個 container 裡，所以
聊天面靠 `@container chat-pane` 收掉 jump label 的那條規則在它身上**永遠不會觸發**；
它也沒有 `openQuotedMessage` 那套 overlay 與再撈，而它本身已經是一張有邊框的卡。
所以那一面是**沒有控制項、靠換行與 `overflow-wrap` 自己撐住**的另一種畫法，幾何由
`visual-guards/resume-chat-quote.ct.spec.tsx` 在寬窄兩端量著。要對齊的是**內容與兩
態規則**（有快照就畫、沒有就畫固定的 `chat.replyQuoteGone`；`content` 為空是合法的
第一態，不可折進第二態），不是版面。

**這一段取代了原本的「只帶 id，前端自己撈」設計，理由要記住：** 舊設計裡撈得到
／撈不到／還沒撈到是三個狀態，而它們在畫面上長得一模一樣，所以出錯時沒有人看得
出來 —— 二十輪審查裡最多阻擋項就長在那台狀態機上。**不要因為「反正那則就在畫面
上，省一次查詢」而把查表加回來**：有時會夾、有時不夾的優化，代價是 client 必須
為「沒夾」的情況準備一條後路，而那條後路就是被刪掉的那台機器。

**引用只有兩態，不准生出第三態**：server 給了快照（畫出來），或 server 沒給
（畫**固定**的 `chat.replyQuoteGone`「這則訊息已不存在」）。**不重試、不補撈、
不自癒**，也不准出現「…」這種還在等的樣子 —— 沒有任何東西在飛。
`content` 是空字串是**合法**的（被引用的那則只有附件），要畫成「有名字、內容空
白」，不可以折進「已不存在」。

**橫幅有自己的文案，不准借用訊息列那一句。** composer 上方的「正在回覆」橫幅沒有
`reply_to_chat` 可讀（那是回覆**送出後**才存在的東西），它只認**已載入視窗**裡的
那則（`messageById`）。而「不在已載入視窗」跟「訊息不存在」是兩件事：往上捲載入
scrollback → 瞄準一則舊訊息 → 切到別的成員再切回來（草稿連同對象一起還原，但視窗
只重載最新一頁）⇒ 橫幅認不出對象，**而那則訊息還在、照送也會成功**（`reply_to`
存得對，讀回來的 `reply_to_chat` 內容完整）。
所以橫幅畫的是**與狀態無關的實話** `chat.replyingToEarlier`「正在回覆較早的一則
訊息」；斷定句 `chat.replyQuoteGone`「這則訊息已不存在」**只屬於訊息列**，因為那
一格的資料來源是 server 這次讀取的答案，有資格做這個斷定。
**不要把兩個 key 指到同一句，也不要為了這一格把查詢或補撈加回來。**

**截短是 server 做的，前端不准再切一次。** `chatReplyQuoteMaxChars`（60 runes）
＋收斂空白都在 server 做完才上線（原本的 `QUOTE_EXCERPT_CHARS` 已刪）。畫面上每一行的
長度限制交給 CSS `text-overflow: ellipsis`（引用列有兩行，各自裁各自的）。

⚠️ **但那個數字有第二份副本**：`frontend/src/api/mock.ts` 的
`MOCK_REPLY_QUOTE_MAX_CHARS`，離線預覽用（mock 沒有 server 可以問）。這一行以前
寫「截短長度只有 server 有」，那是假的，而且兩邊的測試各自寫死 60，所以改 server
不會弄紅前端。現在 `mock.reply-to.test.ts` 會去讀 `server/ocserverd/wire.go` 那一
行，兩個數字不一致就紅 —— 要改長度，兩份一起改。

**引用內容只從 wire 來，「看原訊息」是點下去才撈 —— 兩者都不准在 render 或
effect 裡發請求。** 引用列畫的是 server 這次隨訊息一起送來的 `reply_to_chat`，
沒有就畫 `chat.replyQuoteGone`，**不准為了補齊它去發任何請求**（那台背景補撈的
機器 `useQuotedMessages` 已於 2026-08-21 刪除；`ChatArea.quote-no-fetch.test.tsx`
的第一條就是「畫一則有引用、一則引用不見，api 一次都沒被碰」）。

「看原訊息」按鈕則是 owner 2026-08-21 的裁定（`rc-8559fd6d3c94`：「全部統一就撈
那一則顯示出來就好」）：**每一則有引用的列都給，不問那則在不在已載入視窗** ——
`ChatArea.tsx` 的 render 條件是 `m.replyTo && quoted`，完全不查 `messages`。點下去
用 `api.getChatMessage(replyTo)` 撈那一則，開跟 放大閱讀 同一片覆蓋層，**不捲動**。
這一段是刻意合成的：以前它問「那則在不在視窗」來決定給不給鈕，owner 把它改成一律
給、按了才撈，所以**不要再把窗口成員資格的判斷加回去**。

⚠️ 但「點下去才撈」有兩條紀律：**一次點擊只准一次請求**（`quoteBusyRef` 這個
in-flight latch，不是 state —— 同一個 tick 的兩次點擊都會讀到更新前的 state），
失敗只記**一個** id、原地說一句，**不重試、不排隊、不在下一個 SSE 事件自癒**。
第一條由 `ChatArea.quote-no-fetch.test.tsx` 的
「a click on the quote costs exactly one request, and repainting costs none」釘住；
第二條由 `ChatArea.reply-to.test.tsx` 的
「says so, in place and once, when that one read fails」釘住 —— 失敗路徑的證人在
`reply-to` 那個檔案，不在 `quote-no-fetch`（後者的 api proxy 刻意不註冊失敗態）。

⚠️ **但要精確：第二條那個測試釘住的比這句話列舉的少。** 它實際斷言四件事——
①失敗訊息說在原地、②引用列文字不變（不會變成「這則訊息已不存在」）、③不開覆蓋
層、④`getChatMessage` 只被呼叫一次（不重試、不排隊）。**「只記一個 id」（單值
語意，last click wins）與「不在下一個 SSE 事件自癒」這兩件今天沒有任何測試守
著**：全庫的測試檔裡，只有 `ChatArea.reply-to.test.tsx` 這一個檔碰得到
`msg-quote-error`（陽性對照：`msg-quote-jump` 命中 4 個測試檔）。它們
目前只由碼本身保證——`quoteOpenFailedId` 是單一 state、`openQuotedMessage` 是唯
一寫入點。**動這兩件事不會有測試變紅，請自己看碼。**

⚠️ 還有第三條：**讀取途中不准把那顆按鈕 `disabled`**。有過一個 loading 態這樣
做，實測在真 Chromium 裡 disable 一顆**正被聚焦**的按鈕會讓它 blur，
`MarkdownPreviewOverlay` 掛載時抓到的 opener 就成了 `<body>`，關掉覆蓋層時鍵盤
使用者被丟回頁面最上面。防連點是 `quoteBusyRef` 的工作，不是 `disabled` 的。
這條由 `ChatArea.quote-no-fetch.test.tsx` 的
「stays enabled while its read is in flight」釘住；注意 **jsdom 不會**因為
disabled 而 blur，所以那條測試裡真正有牙的是那句 `jump.disabled` 斷言本身，焦點
那兩條在這一層自己不會紅。

`messageById` 還活著，但它只回答**一個**問題：composer 上方的橫幅認不認得回覆
對象。它**不**回答「能不能看原訊息」。

**取消回覆的 x 只清回覆對象。** 不准順手清 `draft`、不准清
`pendingAttachments`：取消回覆不是取消訊息。這條有測試釘住
（`ChatArea.reply-to.test.tsx`）。

**送出後一定要清掉回覆對象**，否則它會默默黏在下一則訊息上。送出**失敗**時的還
原不准蓋掉使用者飛行中重新瞄準的對象。

**每一則都有回覆入口 —— 包含 agent 之間的訊息。** server 端「必須同一場對話」
的檢查已於 2026-08-21 拿掉，owner 的原話是要能「引用另外兩個人對話裡的一句話來
介入詢問」。所以 `replyable` 這個 gate 已刪：入口出現在視窗裡的每一則上，回覆
仍然寄給這條線的 peer，只是引用的是 owner 指到的那一句。

**唯一的例外是位置，不是有無：請示卡那種訊息**的氣泡被 `<ChatReplyCard>` 換掉
（那是一整片有自己 header 控制項的表面，浮一顆按鈕上去會撞在一起），所以入口留
在列上。回得動，只是不在角落。

以上都有測試釘住（`ChatArea.reply-to.test.tsx`、`ChatArea.quote-no-fetch.test.tsx`、
`ChatArea.reply-card-quote.test.tsx`、`mock.reply-to.test.ts`、`mappers.reply-to.test.ts`）。
其中 `ChatArea.quote-no-fetch.test.tsx` 是「那台狀態機真的不在了」的證人：它把
api client 換成記錄用的 proxy，畫一則有引用、一則引用不見的訊息，然後斷言**一次
呼叫都沒有**。
