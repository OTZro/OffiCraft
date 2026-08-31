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

進房時先在 render 同步 snapshot member.unreadCount，再由 listChat 清 watermark；第一則未讀是對方送給 owner 的未讀訊息中最早的一則，顯示 divider 並保留於本 session。房內新訊息只有在 owner 不在 near-bottom 時顯示浮條；點擊只跳到第一則，必須真的捲到底才清除。

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

hash route #office/chat/<id>/msg/<msgId> 只做一次定位與 highlight；若訊息不在最近窗口就誠實落到底。產生它的是「請示」頁的**跳到原訊息**與任務卡內嵌回覆卡的**在聊天室回覆**（外加使用者自己留存的舊 URL）；聊天氣泡引用列的**看原訊息**不走這條，它撈那一則開覆蓋層（見下方「看原訊息」一節）。⚠️ T-0b78 曾把那兩顆也改成覆蓋層，owner 2026-08-29 裁定「1 跟 2 變回去原本那樣」，**知情接受**「目標不在已載入視窗時會靜默落在最新那一則」這個代價，並把修法另案（「無法跳回去很久以前訊息的問題我們改天再說」）—— 所以不要順手替這兩顆補往回分頁、補提示或改走覆蓋層。回覆卡的 red badge 與聊天未讀互不清除；任務關聯卡共用卡身，只顯示任務標題與查看詳情連結。

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
