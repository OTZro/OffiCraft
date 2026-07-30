# 外包詳情面板 ↔ 正職成員詳情面板 逐項落差盤點（T-7526 步驟 1）

盤點基準 = `origin/main` @ `acac15a0ea56d28bac4f19f2fe4791459e79074e`，**逐行讀原碼核對**，不採信任何二手描述。

涉及三個檔：

- 共用面板 `frontend/src/components/AgentDetailPanel.tsx`（卡片骨架 + 統一 view model `AgentDetailVM`）
- 正職 wrapper `frontend/src/components/MemberDetailPanel.tsx`（T-927a 已改：面板唯讀、設定收進喚醒區）
- 外包 wrapper `frontend/src/components/WorkerDetailPanel.tsx`（未動）

「共用面板的鍵由 wrapper 傳不傳 callback 決定」屬實：
`AgentDetailVM.onSaveModelEffort` 未傳 ⇒ 模型格不長編輯鈕（`AgentDetailPanel.tsx` 的
`{vm.onSaveModelEffort && (<button …model-effort-edit>)}`）；`vm.machineAction` 未傳 ⇒
機器格標題右側無任何控制項。正職兩個都不傳，外包兩個都傳。

狀態欄位說明：**同**＝行為與外觀已一致｜**差**＝需要對齊｜**外包獨有**｜**正職獨有**｜
**待裁定**＝說不出明確期望，交回 owner。

---

## A. 身分卡（identity slot）

| # | 項目 | 正職有什麼 | 外包有什麼 | 差在哪 | 期望行為 |
|---|------|-----------|-----------|--------|---------|
| A1 | 返回鍵 | `mp__back`（共用面板畫） | 同 | 同 | 維持共用，不動 |
| A2 | 頭像上傳／移除 | `AvatarEditor`，未傳 handler 時降級成唯讀 `Avatar` | 同（kind=`outsource`） | 同 | 維持 |
| A3 | 名字 | `InlineEdit` 就地改名 → `onRename` | 無；顯示系統鑄造的代號 `msg.outsourceLabel(codename)` | 差（結構性） | **保留現狀**。外包代號是系統鑄造的匿名識別，不是人取的名字；給外包改名等於發明一個後端沒有的欄位 |
| A4 | 成員編號 chip | `member.memberId` badge | 無 | 差（結構性） | **保留現狀**，理由同 A3（代號本身就是識別） |
| A5 | presence 指示 | `PresenceBadge`（點＋角色名） | `LifecycleDot` + `presenceVisual`（同一份映射） | 視覺元件不同、映射同源 | **保留現狀**：`frontend/CLAUDE.md` 明文「presence→視覺的推導只有一份」，兩者都走 `presenceVisual`，未漂移；外包沒有角色名可顯示，套 `PresenceBadge` 會多出一個空欄 |
| A6 | 任務 chip（`T-xxxx`）+ 任務類型 | 無 | 有，可點 → `#tasks/<id>` | 外包獨有 | **保留**。外包的「角色」就是它綁的任務類型，這是 rail 列形的同一條裁定（`frontend/CLAUDE.md` 外包面板節），移除等於拔掉外包唯一的身分線索 |
| A7 | 動作鍵列（喚醒／取消／停止／強制停止） | `MemberActionButtons`，依 `visual` 五態切換按鈕集合 | **無此列**（停止鍵改長在 C1 狀態卡裡） | 差 | 見 C1／D 區逐項 |
| A8 | 「更改」鍵 | `mp-change`，`online` 時出現，開啟動設定 dialog | **無** | 差 🔴 | **外包要有等價入口**：開一份同形狀的設定 dialog（執行環境／模型／投入度／機器）。這是步驟 2 的主改動 |
| A9 | 未派送警示 | `DispatchAlert`（`mp-wake-undispatched` / `mp-relocate-undispatched`） | **無** | 差 | **待裁定**：外包的 `relocateWorker` wire 回傳 `OutsourceWorkerView`，**沒有** member 那個 `relocation_pending` 欄位，所以外包端根本沒有訊號可投影。要對齊得改 `spec/openapi.json`（wire 已凍結，§13）。本票不動 |

## B. 模型／機器 資訊卡（共用面板 `mp-info2`）

| # | 項目 | 正職有什麼 | 外包有什麼 | 差在哪 | 期望行為 |
|---|------|-----------|-----------|--------|---------|
| B1 | AI 執行環境 / 模型 / 投入度 顯示 | 唯讀。`model` 餵 `awake ? member.actualModel : ""`，並掛 `modelIsReported: true` ⇒ 值旁標「最近一次開機回報」 | 唯讀顯示 + **一顆鉛筆「編輯」鍵**（`worker-detail-model-effort-edit`），就地展開 `ModelEffortEditor` | 差 🔴 | **拿掉就地編輯**：wrapper 不再傳 `onSaveModelEffort`；改設定一律走 A8 的 dialog |
| B2 | 模型值的語意 | REPORTED（agent 開機回報的實際值） | CONFIGURED（`worker.model`，owner 意圖值） | 差 | **待裁定**：外包 DTO 沒有 `actual_model` 對應欄，無法投影「回報值」。硬掛 `modelIsReported` 會是假話。是否要在 wire 加欄，交 owner |
| B3 | 機器格 | 唯讀。`machineText = awake ? machineName : ""`（未喚醒一律 dash，T-2860 presence 契約） | 唯讀值 `worker.machine \|\| 尚未分配`，**加一顆「編輯」鍵**（`worker-detail-relocate`，`useRelocateMachine`） | 差 🔴 | **拿掉就地「編輯」鍵**：機器改為在 A8 dialog 內選。顯示文字維持 `尚未分配`（外包的 `machine` 是「最後一次派工目標」，語意與 member 的 observed 不同，落 dash 反而更不誠實） |
| B4 | 遷移中提示 | `machineTransition`（`→ 要換到 ○○`），`awake && machine !== desiredMachineId` 時顯示 | **無**（wrapper 沒傳 `machineTransition`） | 差 | **補上**：外包同時有 `machine`（最後派工目標）與 `desiredMachineId`（owner 釘選），兩者不同就是移動中，資料齊備。⚠️ 標**待裁定**：這是新增一個畫面元素，且外包的 `machine` 語意是派工目標而非觀測位置，提示文案「現在在 ○○」可能過度宣稱。要不要做、文案怎麼寫，交 owner |
| B5 | 「更換中…」／逾時／失敗回執 | **無**（正職 T-927a 已改走 dialog，`useRelocateMachine` 不再驅動正職面板） | 有（`useRelocateMachine` 的 `phase`：relocating / timeout / failed + 伺服器回執原文） | 外包多 | 🔴 **待裁定**。拿掉 B3 的就地鍵＝連帶拿掉這整組進度／逾時／回執投影，這是**外包目前獨有、正職沒有**的可觀測性。步驟 2 會用 dialog 內的錯誤行（同正職 `settingsError`，顯示 `ApiError.serverMessage`）承接**失敗**那一半，但**非同步落地的「更換中…」與 30s 逾時判定會消失**。這是「與正職同一套形狀」的直接後果，仍請 owner 明示認可 |
| B6 | Claude / Codex Account | 唯讀，`awake && member.account` 才顯示 | 唯讀，`worker.account \|\| ""` | 同（gate 條件不同但都誠實） | 維持 |

## C. 外包獨有卡片

| # | 項目 | 正職有什麼 | 外包有什麼 | 差在哪 | 期望行為 |
|---|------|-----------|-----------|--------|---------|
| C1 | 狀態欄 + 停止／重新啟動 切換鍵 | 狀態字收在 `PresenceBadge`；停止走 A7 的 `MemberActionButtons`（`stop` / `force-stop` / `cancel`） | `worker-detail-status` 欄 + `worker-detail-stop-toggle` 一顆鍵，`stopped` 時文字翻成「重新啟動」 | 位置不同、能力**兩邊都有** | **保留能力，位置待裁定**。⚠️ 派工單說「『下班』是外包自己畫的、正職沒有這顆」——**與原碼不符**：正職有 `MemberActionButtons` 的 `stop`（`t.lifecycle.action.stop`），外包這顆的字面也不是「下班」而是 `t.workerDetail.stop` =「停止」。要不要把它搬進身分卡動作列（＝正職的位置），交 owner |
| C2 | 離線原因 | 無對應（正職走 `最近操作` 卡） | `worker-detail-stuck-reason`：presence=offline 時把 `lastOpReason` 攤在狀態欄下 | 外包多 | **保留**。理由：spawn 靜默失敗時，光一個「離線」對 owner 無資訊；正職沒有這個病是因為正職的停止是 owner 自己按的 |
| C3 | 委託人 | 無 | `worker-detail-delegator`（真實建票人／系統排程 fallback） | 外包獨有 | **保留**。外包是系統代 owner 生出來的，「誰委託的」是外包才有的來歷資訊，正職沒有對應概念 |
| C4 | 委託任務卡 | 無 | `worker-detail-task`，可點 → `#tasks/<id>` | 外包獨有 | **保留**。外包與任務一對一綁定（任務終態即 release），這是外包存在的理由本身 |

## D. 正職獨有

| # | 項目 | 正職有什麼 | 外包有什麼 | 差在哪 | 期望行為 |
|---|------|-----------|-----------|--------|---------|
| D1 | 喚醒（`spawn`／`t.lifecycle.action.spawn`） | 有：離線／已停止／waking／stopping 皆提供，開設定 dialog 後 `activateMember` | 只有 `restartWorker`（**wire 規定「非 stopped 即 409」**） | 差 | **待裁定**。一個 `offline`（spawn 失敗／session 死掉）的外包在 UI 上**無法被叫起來**——`restart` 會 409。要補等價能力得動 wire。不自行發明 |
| D2 | 取消喚醒（`cancel`） | `waking` 時提供 → `deactivateMember` | 無 | 差 | **待裁定**，同 D1：外包的 `stop` 端點是否吃 `waking` 態，wire 沒明說 |
| D3 | 強制停止 + 二次確認 | `stopping` 時 Stop 升級為 force-stop，`mp-force-stop-confirm` modal | **無此端點**（`spec/openapi.json` 只有 `/api/members/{id}/force-stop`） | 差 | **待裁定**。要對齊得新增 `/api/outsource-workers/{id}/force-stop`，屬 wire 變更（§13 先改 spec + owner 過目） |
| D4 | 「只儲存，不喚醒」 | `mp-settings-save-only`，未喚醒時出現 | 無 | 差 | **不需要**（可自裁定）：外包 dialog 本來就**不啟動任何東西**（只打 `model` 與 `relocate` 兩個端點），所以整份 dialog 就是「只儲存」，多一顆同義鍵反而製造「另一顆會啟動」的錯覺 |
| D5 | 設定意圖註記 | `mp-settings-intent-note`（＋回報值對照的第二句） | 無 | 差 | 外包 dialog 改用 `t.workerDetail.modelNextSpawnNote`（「工作中立即生效；已指派則下次啟動生效」）——那才是外包端點的真語意；照抄正職的「下次啟動要用哪一個」會是假話 |
| D6 | 回呼端點 · WEBHOOK 卡 | `extraExpandCards` 整張卡（列表／啟停／建立／刪除／簽章輪替／事件統計） | 無 | 差 | **保留現狀**。webhook 綁的是常駐 member id；外包是任務結束即 release 的短命身分，掛外部長期入口沒有可對應的生命週期 |
| D7 | 改名 | 有 | 無 | 見 A3 | 同 A3 |

## E. 共用卡片（已一致，列出以示涵蓋完整）

| # | 項目 | 狀態 |
|---|------|------|
| E1 | 運行狀況 · context% | 同（`vm.contextPct`；codex 另帶壓縮次數） |
| E2 | 重新聚焦鍵（`*-refocus`） | 同（兩邊都傳 `onRefocus`，皆 online-only，皆有送出註記與上次時間） |
| E3 | 估計 $（live + banked 合計） | 同（兩邊 wrapper 都用同一口徑折算） |
| E4 | 最近操作回執卡 | 同（含失敗原因、記錄展開） |
| E5 | 終端 · TMUX 複製鍵 | 同（session 名分別為 `member.tmuxSession` / `member-<workerId>`） |
| E6 | 初始 PROMPT 展開卡 | 同（正職按 role 抓 `/api/bootstrap`；外包按 id 抓 boot-context，另帶誠實 caveat 註記） |

---

## 「待裁定」清單（交回 owner）

| 代號 | 一句話 |
|------|--------|
| A9 | 外包 relocate 的 wire 回傳沒有 `relocation_pending`，無法對齊正職的「已釘選但沒派出去」警示。要對齊＝改凍結 wire |
| B2 | 外包 DTO 無 `actual_model`，模型格無法像正職那樣標「最近一次開機回報」。要不要加欄？ |
| B4 | 要不要補「→ 要換到 ○○」遷移提示？外包的 `machine` 是派工目標而非觀測位置，文案有過度宣稱風險 |
| B5 | 🔴 拿掉機器格就地「編輯」鍵，會一併失去外包目前獨有的「更換中…／30s 逾時／伺服器回執原文」進度投影（失敗那一半會由 dialog 錯誤行承接）。請明示認可 |
| C1 | 「停止／重新啟動」要不要從狀態卡搬到身分卡動作列（＝正職的位置）？能力兩邊都有，只差擺放 |
| D1 | `offline` 的外包無法被叫起來（`restart` 非 stopped 即 409）。要不要補等價「喚醒」能力？ |
| D2 | `waking` 的外包無「取消喚醒」。`stop` 端點是否吃 waking 態，wire 未明說 |
| D3 | 外包無「強制停止」端點。要不要新增 `/api/outsource-workers/{id}/force-stop`？ |

### 連帶後果（不是新裁定，是 B5 的下游）

- `frontend/visual-guards/relocate-progress-720.ct.spec.tsx` 與它的
  `WorkerRelocateProgressStory` 已移除（檔案 `mv` 進 `trash/T-7526/`）。理由與 T-927a 移除
  **正職那一半**時逐字相同：外包面板不再有 改機器 鍵、「更換中…」字樣與 30s 逾時提示，
  這份量測不是紅、是**無從表達**；而外包的 機器 標題列現在完全沒有控制項，沒有寬度風險可量。
- `useRelocateMachine.tsx`（連同 `MachinePicker.tsx`）在本次改動後**已無任何 production
  importer**（僅剩自己的測試與 `MemberDetailPanel` 註解裡的 twin-implementation 交叉引用）。
  依 §9(a) 這是該清的 legacy，但刪掉一個 hook ＋ 它整份測試 ＋ 一個元件，範圍遠大於本票，
  **列為 follow-up 交 owner 裁定，本票不刪**。

**沒有任何一格因為我判斷「該移除」而被移除。** B1 / B3 兩項就地編輯鍵的移除，是派工單步驟 2
DoD 第 1 條明文指定的改動，且**能力本身沒有消失**（改模型、改機器都改由 A8 的 dialog 承接）。

---

## 步驟 2 實作範圍（依上表推導）

1. `WorkerDetailPanel` 不再傳 `onSaveModelEffort`（B1）與 `machineAction`（B3）給共用面板。
2. 身分卡動作列新增「更改」鍵（A8），開一份與正職同形狀的設定 dialog：
   `ModelEffortEditor`（執行環境／模型／投入度）＋ 機器 `<select>`（線上機器 ＋ 自己那台離線釘選，
   照 `MachinePicker` 的規則標「離線」且 disabled），底部 取消／更改。
3. 確認送出：`launchChanged` → `api.setWorkerModel`；`machineChanged` → `api.relocateWorker`。
   PATCH 先於 relocate（正職 `saveSettings` 的同一條理由：relocate 會重生 session，
   設定必須先落地，否則新 session 用舊模型起來）。
4. 失敗顯示 `ApiError.serverMessage`，fallback `t.mp.modelEffortError`（正職同一條）。
5. 切換到另一個外包時關掉 dialog、清錯誤（正職 `[member.id]` effect 的同一個理由：
   兩個呼叫端都沒傳 `key`，dialog 會帶著上一個外包的草稿活下來）。
6. `docs/guide/members.md`「你能做的幾個動作」逐條標明適用面板，讓每一句對外包為真。
