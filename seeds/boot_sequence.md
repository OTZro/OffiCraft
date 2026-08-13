# 啟動程序（Boot Sequence）

剛醒過來、開機當下依序做這四步（原理見 §5 兩條 liveness，這裡只給動作）。**四步順序不可換，掛 SSE 一律排在最後**——`ocagent listen` 一掛上，server 就把你標成 online，前兩步沒 ready 就掛 = 假 online。

## Claude Code 執行環境

- `AskUserQuestion` 已禁用；不要用任何 terminal 互動選單。需要 owner 決策或動作時，用 OffiCraft `create_reply_card`。
- context 使用量由 Claude Code `statusLine` 自動上報；不要手動跑 `context-report`。
- `report_waking.model` 填 Claude Code 提供的真實 model id，不要猜值。

1. **報 waking（不掛 SSE）。** 用 MCP `report_waking()` 回報你已經開機。
2. **接回脈絡（兩步：先 peek 再決定）。** 先用 MCP `peek_resume_summary_size` 探大小——它只回 counts／字數（`overview` ＋ `estimated_total_chars`）、**不含任何內容全文**，幾百 byte 而已。看 `estimated_total_chars`：小（經驗法則的門檻：**小於 20000 字元、約 5k tokens**）就直接在主 session 用 MCP `resume_summary` 把身分快照／指派／待辦接回來；大就**派一個便宜 model（如 haiku）的 sub-agent** 去呼叫 `resume_summary`、回你一份壓縮摘要，別讓整包全文燒你自己的主 session context。接回、確認就緒。
3. **全部就緒後，才掛 `ocagent listen`。** 用內建 **Monitor 工具**在背景掛住（bare 指令即可，spawn 已把 `ocagent` 放進 cwd 且 prepend 進 PATH）。**不要**寫前景空轉死迴圈。
4. **啟動後任務盤點與排程（僅 member）。** SSE 已連上、liveness 就緒後立刻盤點自己手上的非終態任務：先接續上一代交接或已開始的任務；再把尚未開始的任務依優先權與可否並行排程（優先權是「凍結」的擱著不動），能並行的分派各自隔離的 sub-agent，受共用資源限制的才排隊。完成盤點後才執行，不要因為只看見一張快照就閒等。外包 worker 只綁一張任務：第 2 步接回脈絡就會告訴你是哪一張，要完整計畫再用 `get_task` 讀回來，然後照同一條規則推進。
