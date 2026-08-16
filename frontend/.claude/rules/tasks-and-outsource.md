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

# 任務頁、TaskCard、外包面板與任務手冊

## 任務清單與錨點

useTasks 把 statusFilter 轉成重複的 ?statuses=；執行者與類型篩選仍在前端。清除狀態篩選才送空集合，代表使用者要完整清單。不要為 dependencies 再拉全歷史，也不要把每個 task SSE 變成全歷史下載。

跳到 #tasks/<id> 時，清單仍保留原篩選，另以 GET /api/tasks/{id} 補單張錨點。anchor id 是 effect 的參數；anchorPending 在補抓成功或失敗前都阻止自癒與空狀態誤判，失敗後誠實回一般清單。合併時清單列優先，因為輕量列才有 dep_tasks；單張 DTO 沒有時不可覆蓋它。篩選未包含錨點時，depTasks===undefined 表示未知，不表示沒有依賴。

## TaskCard

卡片預設收合；標題與 description 是唯讀 UI，owner 沒有要求編輯入口。進度與 gate 狀態直接使用 server 值；stepBadge 由 lib/stepBadge.ts 統一，superseded 不計入 progress。gate 預告、內嵌 TaskReplyCard、等待外部 banner 與任務訊息框都要維持原 wire 語意。

依賴 chip 讀 task.depTasks，必須區分已解析、查無此任務與 undefined 未提供。

**卡上有兩種收合，owner 對它們的裁定相反，不要把其中一條套到另一條（T-6630）：**

**① 步驟備註（`task-step__note-toggle`）— 畫面不動。** 備註預設收起，只有有備註的步驟才顯示 toggle；展開與收合都不得改變捲動位置（owner：往下展開、向上收合，畫面不動），所以備註 toggle 不做任何 scroll 校正——不寫 `.tasks` 的 scrollTop，也絕不用 `scrollIntoView`（它會重新對整條祖先鏈的每個 scrollport 下手，等於把整個畫面搬走；備註 toggle 一律禁用，等我回覆／等待外部那兩個「跳轉」入口才是它的合法用途）。唯一做不到「完全不動」的是收合比目前捲動量還高的備註：捲動範圍縮短、scrollTop 被夾在 0，殘餘位移等於 `備註高度 − 收合前 scrollTop`，護欄用這個算式做嚴格斷言，不放寬。
**這條規則買到的與放棄的，兩邊都要講**：放棄的是 T-4e39 的「展開後把放得下的部分露出來」——**展開一則很長的備註後，內文可能停在畫面外，要自己往下捲**。這是 owner 知情的取捨，不是待修的 bug；若他回報「點開備註看不到內容」，那是這個取捨在說話，要回去請他重新裁定，不是自己補一個捲動校正。
這顆 toggle 是**整列**、至少 44px 高、有自己的底色與邊框（底色與邊框由主題的 `--color-overlay` 混出來。**外觀不設護欄**——owner 2026-08-16 裁定：「不需要驗證什麼顏色好不好，這種都是負責人一開始確認沒問題就好」。所以這兩個百分比只靠他的一次確認撐著：**調淡不會有任何東西變紅**，要改就回去問他，不要當成順手的視覺整理），而且**必須維持是 `<button>`**：整張 `.task-card` 本身是 `role=button`，卡內只有互動元素才被 `closest()` 濾網放行，降級成 `div` 會讓「點備註列收掉整張任務」——正是這條規則要治的病。

**② 整張任務卡收合 — 要定位到那則任務。** owner：「收和整個任務時，最後應該要定位到那則任務」。收合當下卡片頂端若已捲到畫面上方，把它的頂端拉回捲動區頂端（寫**真正在捲的那個祖先**的 scrollTop，見 `lib/scrollPort.ts`；仍不用 `scrollIntoView`）。校正是**單向**的：頂端還看得到就一 px 都不動，**展開方向完全不校正**（那個方向仍歸①管，護欄兩邊都釘）。定位選「把卡頂對齊捲動區頂端」而不是「最小移動」：任務頁沒有任何 sticky 表頭（實掃 `tasks.css` 無 `position: sticky`），所以頂端不會被別的東西蓋住，對齊頂端就是「這張卡從頭給你看」。物理上限：收合清單最後一張時捲動範圍在它下面已無餘裕，卡片停在畫面中下方而非頂端，但必須整張仍在視窗內——護欄斷言的是「看得到」，不是「在頂端」。

外包 chip 在任務卡描述 launch intent；監控頁的自報 runtime/model/effort 是另一條規則，不可混用。worker 已 release 時不捏造代號；未指派與零節點狀態要分別顯示等待指派與規劃中。

## 外包面板與聊天

useOutsourceWorkers 只讀 /api/outsource-workers 與 settings，並訂 outsource_worker、task、chat、chat_read；不可加回 tasks 或 task-manuals 全歷史 join。server DTO 已帶 task_no、created_ts、type key/name。

外包列顯示 O- 代號、task type 加真實 presence 點、可點的 T- 任務代號與 unread badge；不顯模型、標題、識別鍵或狀態字。排序以 task created_ts 為準，終態 worker 從 live list 消失。聊天使用 ow- id；header 可用 synthetic member，但不要在 chat header 重複 rail presence。上限 -1 是無限、0 是暫停指派；settings 未載入時只顯目前數，不捏上限。

## 任務手冊

GET /api/task-manuals 的列表只靠 type_key。partial POST 中 null 是 no-op，assignee:{} 才是清除；非終態手冊不可刪。詳細頁可讀 definition 與 learnings，但不顯示內部檔名。

欄位要標 required/key；指派可為 member 或 outsource，並保留 model、effort、machine、copies 語意：copies=0 表示無限，machine 必須是實際機器，不自動 fallback，也不要送空 machine。離線時不自動改派。
