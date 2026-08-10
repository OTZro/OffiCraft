# 定期訊息（scheduled message）— T-f059

一條排程到期時，server 把一則**預先寫好的訊息**送給一位成員（正職或外包），
走的是**現有聊天訊息那一條路**：在線即時收到、離線進持久信箱等下次上線、
由收件的 agent 自己決定何時處理。owner 2026-08-10 對投遞語意的原話是
「不能跟現有聊天機制一樣就好嘛」——所以這裡**不發明新的投遞語意**。

它是「回呼端點（webhook）」的孿生兄弟：webhook 把觸發者放在外部系統，
定期訊息把觸發者換成**時鐘**，其餘完全相同。

## 為什麼不做成「定期開票」

那會把「排程」跟「排程的其中一種用途」綁在一起。定期送一則訊息是**更小、更通用的原語**：
提醒、巡檢、盤點都用得上，而收到訊息的 agent 若判斷該開票，它自己就會開。

⚠️ **這個形狀有一條天然界線**：訊息送出去之後，「他做了沒」在畫面上看不到，
除非他自己開了票。**這不是缺陷，是選擇。** 哪天發現「排程有送、事情沒發生」，
那就是把某幾種用途升級成開票的訊號。

## 資料模型

`scheduled_message`（migration `00050_scheduled_message.sql`）

| 欄位 | 型別 | 說明 |
|---|---|---|
| `id` | TEXT PK | `sch-` + 12 hex |
| `member_id` | TEXT | 收件者。**可以是正職，也可以是 `ow-` 外包**（見下方「收件對象」）。**不下外鍵**（照 00001 decree） |
| `label` | TEXT | 給人看的名稱；也放進 meta，讓收件端知道是哪一條 |
| `body` | TEXT | 要送出的訊息內容 |
| `cadence` | TEXT | `daily` / `weekly` / `monthly`（CHECK 閉集） |
| `day_of_week` | INTEGER | weekly 用，0=週日 … 6=週六 |
| `day_of_month` | INTEGER | monthly 用，**1–28** |
| `hour` / `minute` | INTEGER | 0–23 / 0–59 |
| `timezone` | TEXT | IANA 時區名，預設 `Asia/Taipei` |
| `status` | TEXT | `enabled` / `disabled`。**是撤銷開關、不是生命週期**；`DELETE` 才是永久移除 |
| `last_fired_slot` | TEXT | 🔴 **已送出的那個時間槽的識別字串**（見下） |
| `last_fired_ts` | REAL | 上次真的送出的時刻（給人看的，不參與判定） |
| `created_ts` | REAL | epoch secs |

索引：`idx_scheduled_message_member ON scheduled_message (member_id)`。

### `day_of_month` 為什麼上限是 28

29／30／31 在某些月份不存在。允許它們會讓「這個月沒送」變成一個**沒有任何東西會叫**的
靜默失敗，而且只在二月出現一次——最難發現的那種。上限 28 讓「每個月都會送」成為構造上為真。

## 🔴 排程游標：存「時間槽」，不是存「上次執行時間」

`last_fired_slot` 存的是**那一格的識別字串**，例如 `2026-08-10T09:00+08:00`。

每個 tick：
1. 依 `cadence` / `day_of_*` / `hour` / `minute` / `timezone`，算出**現在之前最近的一個到期時間槽**。
2. 那個槽的字串**若不等於** `last_fired_slot` → 送出，然後把它寫回 `last_fired_slot`。
3. 相等 → 什麼都不做。

三個驗收條件因此變成**構造上為真**，而不是靠小心：

- **server 重啟不重送同一次** —— 重啟後重算出同一個槽字串，比對相等就跳過。
  不依賴任何「上次跑到哪」這種活在記憶體、換版就遺失的狀態。
- **錯過的不補送** —— 永遠只看「最近的一個槽」。停機三天，上線後只補送最近那一格，
  不會一次湧十則。
- **剛建立的排程不會當場就送** —— 建立時 `last_fired_slot` 初始化成**建立當下的最近一個槽**，
  所以早上十點建一條「每天 09:00」不會立刻送出今天的那一則。

> 反例，別抄：`receipt_watch.go` 的 `armReceiptWatch` 把 deadline 放在記憶體 map 裡，
> 重啟即遺忘。那個取捨在它自己的檔頭有寫明，但**排程游標不能這樣做**——
> 重送的樣子跟正常送出一模一樣，沒有任何東西會叫。

## 收件對象：正職與外包都可以

⚠️ **不要用 `resolveMember`**（`api_helpers.go`）：它**明確排除 `kind == outsource`**，
所以今天 webhook 端點根本綁不到 `ow-` worker。

聊天可以送給外包（`resolveChatRecipient` 同時允許 `KindAssistant` 與 `KindOutsource`），
而定期訊息就是一則聊天訊息 ⇒ **用聊天那一套的收件者判準**。

⚠️ **一句實話**：外包 worker 綁一張任務、任務結束就作廢。掛在外包身上的排程
**會跟著那個 worker 一起消失**。這是外包這個角色的性質，不是本功能的缺陷。

## 投遞：收件端怎麼分辨這是定期訊息

完全對稱 webhook 的做法：

| | webhook | 定期訊息 |
|---|---|---|
| 寄件者 | `hook:<endpoint_id>` | `sched:<schedule_id>` |
| meta | `meta.webhook = {endpoint_id, purpose}` | `meta.scheduled = {schedule_id, label, slot}` |

`chat_message.meta` 是 open map（TEXT 欄存 JSON、`newChatMessageDTO` 原樣上 wire），
新增 key 不必改 schema。系統裡的合成寄件者原本只有兩個先例：`hook:` 與 `system`。

⚠️ **server 端不讀 `meta.scheduled`**，跟 `meta.webhook` 一樣——它純粹是給收件的 agent 讀的。

## 對外介面

四條 REST，全部 `MCPExclude`（**不新增 MCP 工具**）：

| Method | Path | 說明 |
|---|---|---|
| GET | `/api/members/{member_id}/scheduled-messages` | 列出這位成員的排程 |
| POST | `/api/members/{member_id}/scheduled-messages` | 新增一條 |
| PATCH | `/api/members/{member_id}/scheduled-messages/{schedule_id}` | 修改（含啟用／停用） |
| DELETE | `/api/members/{member_id}/scheduled-messages/{schedule_id}` | 永久移除 |

`Auth: gated`、`Requires: admin_agent` —— 與隔壁 webhook CRUD 同級，
也就是 owner 與 admin 助理設得動。

**為什麼不上 MCP 工具面**：既有先例就是這樣——webhook 的四個 CRUD 全是 `MCPExclude`，
只有除錯用的 `list_webhook_requests` 上了工具面。**設定類 CRUD 走座艙，不走工具目錄。**
（webhook CRUD 升到 `admin_agent` 的理由是它的 DTO 帶明文 token；
定期訊息的 DTO **不帶任何祕密**，門檻同級是為了與鄰居一致，不是因為有祕密。）

### DTO 欄位

`ScheduledMessageDTO`（回應）：
`id` · `member_id` · `label` · `body` · `cadence` · `day_of_week` · `day_of_month` ·
`hour` · `minute` · `timezone` · `status` · `last_fired_slot` · `last_fired_ts` · `created_ts`

`ScheduledMessageCreateDTO`（POST body）：
`label` · `body` · `cadence` · `day_of_week` · `day_of_month` · `hour` · `minute` · `timezone`
（`cadence` 與 `body` 必填，其餘有預設）

`ScheduledMessageUpdateDTO`（PATCH body）：以上每一欄都是 **optional**（照憲章 §12「對外 DTO 加欄一律 optional」），
只送要改的那幾欄；`status` 也在這裡，用來啟用／停用。

## 背景迴圈

照這個 repo 既有的形狀（`reconcile.go` / `auto_update.go`）：
`startScheduledMessageCadence(period)` 只負責掛 goroutine，
**一個 tick 拆成獨立可測的 `runScheduledMessageTick(now float64)`**，
測試直接呼叫 tick、不等時鐘。間隔 60 秒（與 auto-update 同級；
最小排程粒度是「分鐘」，60 秒足夠）。

與既有五支 cadence 一致：sleep-then-tick、沒有 `time.Ticker`、沒有 context cancel。

## 這個功能對現有 agent 是隱形的

本包**不動 `seeds/`**（票面範圍邊界；另一條 seeds 規則衝突正等 owner 裁定）。
連帶結果：agent 收到 `sched:` 開頭的訊息時，開機說明裡**沒有任何一段告訴他那是什麼**。

⚠️ 這與 webhook 那一段（`seeds/system_interaction.md` §4.2）的差別要講清楚：
那一段的主要工作是教「**外部不可信輸入**的用途守衛」，
而定期訊息是 owner／admin 在座艙裡自己設的，**不是外部不可信輸入** ⇒ 那條守衛的理由不轉移。
剩下的缺口只是「不認得這個寄件者」，而訊息內容本身就會說明要做什麼。
