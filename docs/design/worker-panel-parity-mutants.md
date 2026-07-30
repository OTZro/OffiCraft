# T-7526 mutant 驗證紀錄

**為什麼這份要落檔**：獨立審查回來時，這包宣稱的 mutant 驗證在 repo 裡找不到任何紀錄，
審查者只好自己重跑一遍。**沒落檔等於不存在。** 下面每一列都可以被獨立重放。

## 方法

1. 把要驗的檔案複製一份到 scratchpad 當備份，`shasum -a 256` 記下雜湊。
2. 施加 mutant（單一、明確、可讀的一處改動 —— 描述欄寫的就是那一處）。
3. 跑該範圍的測試，記下**哪幾條紅**。
4. **從 scratchpad 備份 `cp` 回來**（🔴 **不准 `git checkout --`** —— 那會吃掉別人未提交的編輯），
   再 `shasum -a 256 -c` 驗還原後**逐位元組相同**。
5. Go 的每一支 mutant 前先 `go clean -testcache`，否則會讀到上一支的快取結果。

全部 13 支的還原檢查都是 `OK`。

## 第一批：外包面板對齊（`WorkerDetailPanel.tsx`）

| # | Mutant（改了什麼） | 紅了哪條 |
|---|---|---|
| M1 | vm 裡把 `onSaveModelEffort` 加回去（就地模型編輯器復活） | `renders the 模型 and 機器 cells with NO in-place editor on either` |
| M2 | vm 裡把 `machineAction` 加回去（機器格就地控制項復活） | 同上 |
| M3 | 拿掉身分卡的「更改」入口（`{false && (…)}`） | 上述 + `更改 → changing the machine…` + `a rejected submit…` + `換 model: 更改 → save…`（共 5 條） |
| M4 | dialog 送出時不打 relocate（`if (false)`） | `更改 → changing the machine REACHES relocateWorker…` |
| M5 | 送出失敗時吞掉錯誤並關掉 dialog | `a rejected submit keeps the dialog open…` |
| M6 | relocate 排到 model op **之前**（順序反轉） | `saves the launch settings BEFORE relocating…` |
| M7 | 拿掉切換 worker 時的 dialog reset effect | `closes an open dialog when the panel switches to another worker` |

⚠️ **M6 / M7 第一輪是綠的** —— 代表當時**沒有任何斷言**釘住這兩個行為。補了測試才紅。
先寫 mutant、再看它殺不殺得死，是唯一能發現「斷言不存在」的辦法。

## 第二批：初始 PROMPT 卡（`AgentDetailPanel.tsx`）

先做**整體重現**：把**未修補的** `AgentDetailPanel.tsx` 還原回去跑新測試 —— **5 條全紅**
（正職 3 條、外包 2 條；MC-3 後外包補到 3 條）。再逐項：

| # | Mutant | 對應的修補要求 | 紅了哪條 |
|---|---|---|---|
| P1′ | deps 放回 `promptFetch` **並且**加回取消式 cleanup | ① 重繪不該取消進行中的讀取 | 正職＋外包的「重繪中仍顯示得出內容」兩條 |
| P2 | 已載入的章改回在 fetch **開始**時蓋 | ② 章要等讀到才蓋 | `re-expanding after a failed read…` |
| P3 | `.catch` 不落錯誤態（停在載入中） | ③ 失敗要顯示錯誤與重試 | 錯誤/重試相關 3 條 |

⚠️ **P1 的第一版只加了 cleanup、沒把 `promptFetch` 放回 deps —— 它是綠的。**
deps 沒變，cleanup 根本不會執行。**兩個成因是耦合的，mutant 必須兩個一起下。**

⚠️ 另一個坑（是這次真正差點放過去的）：`rerender` 若傳**同一個 element 物件**，React 會 bail out、
**根本不重繪**，於是「重繪中」那條測試**在未修補的碼上就是綠的**（恆真斷言）。
每次都要產生**新的** element。已寫進 `frontend/CLAUDE.md`。

## 第三批：取消喚醒（A）與復活死掉的 session（B）

**先做重現**（先紅才有資格談修好）：3 條重現測試在修補前全紅、3 條負向對照在修補前後都綠。

| # | Mutant | 紅了哪條 |
|---|---|---|
| A1 | deactivate 不派 STOP（`_ = cancellingWake`） | `TestHandleDeactivateMember_CancellingAWakeDispatchesAStop` |
| A1b | 每一次停止都立刻派（過度修正：`cancellingWake := true`） | `_OnlineMemberKeepsTheGracefulGrace` + `_OfflineMemberDispatchesNothing` |
| A2 | `report_waking` 又無條件清 `stopping_since` | `TestHandleReportWaking_KeepsTheStopTraceOfACancelledWake` |
| A2b | 永遠不清（過度修正） | `_ClearsTheStopTraceOnAnOrdinaryBoot` |
| B1 | restart 守衛退回 INTENT-only | `TestRestartWorker_RevivesAWorkerWhoseSessionDiedOnItsOwn` |
| B1b | restart 守衛整個拿掉（`if false`） | `TestRestartWorker_ClearsAndRedispatches`（活著的 worker 該 409） |
| B2 | 座艙 toggle 退回 `stopped`-only | `a worker whose session died on its own offers 重新啟動…` |
| B2b | toggle 永遠顯示重啟（過度修正） | 既有的 `stop → the worker reads 已停止 and the toggle flips to 重啟` |
| B3 | mock adapter 漂回 INTENT-only 守衛 | `a worker whose session died on its own offers 重新啟動…` |

**B1 / B1b 是雙向的**：一支證明「死掉的要救得回來」，另一支證明「活著的仍要 409」——
守衛的兩半都承重，少任何一半都會被抓到。A1b / A2b / B2b 同理，是防過度修正的哨兵。

⚠️ **A1 的第一版是 `if false {`，Go 編譯不過**（`cancellingWake` 變成未使用）。
**編譯失敗不是「紅」** —— 它什麼都沒證明，得換成 `_ = cancellingWake` 重跑。
