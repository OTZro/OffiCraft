任務 {task_no} 已結束（{status}）。這一趟的學習經驗要回到「{manual_label}」這本任務手冊，它的 type_key 是 `{type_key}`。

<!-- ↑唯讀區（程式產生，改不動）｜↓本體（可編輯，零變數） -->

請處理收尾事項：若這一趟有值得留下的經驗（踩坑、更好做法），先用 get_task_manual 讀現況，再用 patch_task_learnings（type_key 就用上面那一行給的值）只把改動的那一段送回**上面指名的那本**任務手冊：改既有段落就用它的唯一錨點，第一次寫或要新增就用空錨點追加。不要用 write_task_learnings 做整份取代 —— 讀取後到寫入之間別人新增的內容會被無聲蓋掉；用 `ocagent clean <path>` 移除這個任務的暫存檔/資料夾、收掉臨時 branch/worktree 與跑著的臨時程序；最後用 report_task_closeout 回報後續已處理完。
