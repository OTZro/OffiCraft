# 啟動程序（Boot Sequence · Codex）

剛醒過來依序做這四步（原理見 §5 兩條 liveness，這裡只給動作）。**四步順序不可換**——sidecar 提前掛上，server 就把你標成 online，前兩步沒 ready 就掛 = 假 online。⚠️ **開機這個 turn 只做得完前三步**：「完成這個 boot turn」本身就是其中一步，所以**任務盤點那一步必然落在 sidecar 接手 SSE 之後的後續 turn**，不是同一個 turn 裡接著往下做（見該步）。

## Codex App Server 執行環境

- 權限模式是 `danger-full-access`，approval policy 是 `never`。
- 互動式 `request_user_input` 已禁用；不要等待 terminal 鍵盤。需要 owner 決策或動作時，用 OffiCraft `create_reply_card`；若需要密碼、金鑰等機密資訊，請 owner 自行完成該動作，不要要求他把機密貼進卡片內容。
- context 使用量由 App Server token-usage 事件自動上報；不要手動跑 `context-report`。

1. **報 waking（尚未掛 SSE）。** 用 MCP `report_waking()` 回報你已經開機。`model` 參數嚴格照 sidecar 的 developer instruction：OffiCraft launch model 空白就省略，絕不猜值寫回。
2. **接回脈絡（兩步：先 peek 再決定）。** 先用 MCP `peek_resume_summary_size` 探大小——它只回 counts／字數（`overview` ＋ `estimated_total_chars`）、**不含任何內容全文**，幾百 byte 而已。看 `estimated_total_chars`：小（經驗法則的門檻：**小於 20000 字元、約 5k tokens**）就直接在主 session 用 MCP `resume_summary` 把身分快照／指派／待辦接回來；大就**派一個便宜 model 的 sub-agent** 去呼叫 `resume_summary`、回你一份壓縮摘要，別讓整包全文燒你自己的主 session context。接回、確認就緒。
3. **全部就緒後，完成這個 boot turn。** **不要**自行啟動 `ocagent listen`、Monitor 或前景空轉迴圈。你結束 boot turn 後，OffiCraft 的 App Server sidecar 才會啟動並持有 `ocagent listen`；`turn/completed` 是 ready 邊界，之後 SSE 上線才會把你標示成 online。
4. **啟動後任務盤點與排程（member；外包 worker 見本步末句）。** ⚠️ **這一步不在 boot turn 之內**——你的 boot turn 在「完成這個 boot turn」那一步就結束了，sidecar 之後才接手 SSE；所以這一步是在**後續的 turn** 做，不是接著上一步往下寫。sidecar 的 SSE 已連上、liveness 就緒後立刻盤點自己手上的非終態任務：先接續上一代交接或已開始的任務；再把尚未開始的任務依優先權與可否並行排程（優先權是「凍結」的擱著不動），能並行的分派各自隔離的 sub-agent，受共用資源限制的才排隊。完成盤點後才執行，不要因為只看見一張快照就閒等。外包 worker 只綁一張任務：接回脈絡那一步就會告訴你是哪一張，要完整計畫再用 `get_task` 讀回來，然後照同一條規則推進。
