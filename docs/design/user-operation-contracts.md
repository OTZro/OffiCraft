# 使用者操作契約清單

這份清單的每一條 `sentence` 都只描述一個外部可觀察的「使用者動作 → 結果」。
`scope` 是明確的畫面集合；集合中的每一個畫面都必須在同一條目列出一個具名
e2e 斷言，不能把涵蓋範圍留給讀者推測。`surface_set=reply-card-body-callers`
表示 scope 不是手數出來的：守衛會反向掃 production tree 中所有 import
`ReplyCardBody` 的 caller，再和
[`user-operation-contract-surfaces.json`](user-operation-contract-surfaces.json)
逐項比對。`ruling` 是 owner 裁定卡的出處，`evidence` 只列出目前產品說明中
可重讀的行。

任何條目內容（包括句子、scope、surface_set、斷言綁定與出處）改動，都必須在該條目的
metadata 行同時帶上 owner 的新裁定出處；把句子改窄也算改動。守衛會檢查這個
變更邊界，並把每個 scope 對到 e2e 裡的 `UOC_ASSERT` marker。

<!-- user-operation-contract: id=UOC-RC-SINGLE-TAP scope=replies-page,chat-page,tasks-page surface_set=reply-card-body-callers ruling=rc-06bc715358c2 evidence=docs/guide/glossary.md:63,docs/guide/quickstart.md:88,docs/design/SPEC.md:319 -->
- sentence: 單選請示卡在請示列表頁、聊天頁與任務頁展開後的內嵌請示卡上，點一下選項就直接送出，不需要第二次按送出。
- assertion: screen=replies-page marker=single_option_tap_answers_on_replies_page
- assertion: screen=chat-page marker=single_option_tap_answers_in_chat
- assertion: screen=tasks-page marker=single_option_tap_answers_on_tasks_page
<!-- /user-operation-contract -->

<!-- user-operation-contract: id=UOC-RC-SINGLE-DRAFT scope=replies-page,chat-page,tasks-page surface_set=reply-card-body-callers ruling=rc-06bc715358c2 evidence=docs/guide/quickstart.md:88,docs/design/SPEC.md:253,docs/design/SPEC.md:319 -->
- sentence: 單選請示卡在請示列表頁、聊天頁與任務頁展開後的內嵌請示卡上，點一下選項時，已打在框裡的字會跟著同一次送出，不會被丟掉。
- assertion: screen=replies-page marker=single_option_keeps_draft_on_replies_page
- assertion: screen=chat-page marker=single_option_keeps_draft_in_chat
- assertion: screen=tasks-page marker=single_option_keeps_draft_on_tasks_page
<!-- /user-operation-contract -->
