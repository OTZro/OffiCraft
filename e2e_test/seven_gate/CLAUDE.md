# seven_gate/ — 七步關卡

進入 `e2e_test/seven_gate/` 時 nested-load。上層規則見 `e2e_test/CLAUDE.md` 與 root `CLAUDE.md`；
本檔記這個載體專屬的設計。

## 這是在保護什麼

開機說明（`seeds/*.md` 折成的 global context）**就是 agent 的操作手冊**。砍錯一段，後果不是
「文件變差」，是**新的 agent 根本不會開票、或開了推不動——而且它自己不會知道**。它不會報錯、
不會求救，它會很有禮貌地做別的事，然後回報一切正常。

所以要驗的不是文件好不好讀，是：**一個沒讀過舊版說明的全新 agent，在隔離環境裡，能不能把
一條真實路徑走完**。那條路徑固定七步，不可增刪：

報到 → 接回現場 → 開票 → 提出計畫 → 報一步完成 → 開一張等我回覆卡 → 回報收尾

## 判定規則（這是本載體的核心，不是實作細節）

1. **只看 server 上的事實。** 票真的在、步驟真的轉了狀態、卡真的開出來、收尾回條真的有。
   agent 的自述、log 裡的漂亮句子、actor 腳本的 rc，一律不算證據。
2. **不要問 agent「你會不會」。** 它會說會。所以載體裡沒有任何一句是問 agent 做了什麼——
   `judge.py` 從頭到尾不跟 agent 說話，它只讀 `journal.ndjson`。
3. **必須能用沒讀過舊版說明的全新 agent。** 候選開機說明由 `OC_SEEDS_SRC` 指定
   （`bin/build-seedsdist` 的既有開關，phase 1 已落地）；`setup.sh` 每次都重跑那支腳本再
   `go build`，所以「換一份開機說明」＝換一個環境變數，**不必碰被追蹤的 `seeds/`**。
   member 也每次現僱一個新的——重用舊 member 會讓它帶著開機說明沒教過的知識進場。
4. **每次 run 的 log 與磁碟產物要保留**（見〈產物〉）。
5. **跑完固定追問 friction**，問法逐字寫死在 `friction.md`，見〈friction〉。

## 為什麼是「journal + 純判定」這個形狀

七個事實裡有一個是**會消失的**：①的 `presence=waking` 在 agent 掛上 SSE 之後就翻成 online。
只看最後一張快照的載體，分不出「報過到」和「根本沒開機」。

於是切成兩半：

- `collect.py` —— 只做 I/O。從 agent 開機**之前**就開始輪詢隔離 server，每次一行 JSON
  append 進 `journal.ndjson`。它不判定任何事。
- `judge.py` —— 純函數。吃一個 run 目錄，吐七行判定 ＋ 一個 rc。**不開 socket、不改任何東西**。

這個切法唯一的目的是：**判定邏輯不需要 server 就能測**。`tests_guard` 案例 (21) 餵它捏造的
bundle，跑在 `bin/ci.sh` 的第 (0) 階，不起任何服務。

## 七步各自讀 server 上的哪一個事實

| # | 步驟 | server 事實 | 讀哪裡 |
|---|---|---|---|
| ① | 報到 | `presence == "waking"` 曾經出現過 | `GET /api/members/{id}`，journal 任一 sample |
| ② | 接回現場 | agent 發出的訊息裡含 scene nonce | `GET /api/chat`，`from == agent` |
| ③ | 開票 | 有一張 task 的 `creator_id == agent` | `GET /api/tasks` → 逐張 `GET /api/tasks/{id}` |
| ④ | 提出計畫 | 那張票 `steps[]` 非空 | 同上（`submit_plan` 是 `steps[]` 唯一的寫入者） |
| ⑤ | 報一步完成 | 那張票有 step `status == "done"` | 同上 |
| ⑥ | 開一張等我回覆卡 | 有一張卡 `from == agent` | `GET /api/reply-cards` |
| ⑦ | 回報收尾 | 那張票 `closeout_reported == true` | 同上票的 full DTO |

③④⑤⑦ 綁在**同一張票**上（③找到的那張，多張時取最早）。不綁的話，載體會被 server 上任何
一張碰巧存在的票餵飽。

### ②「接回現場」為什麼要靠一個 nonce——這是本設計最需要被質疑的一格

`resume_summary` 是一個 **GET**，server 端**什麼都不 stamp**（實查 `api_chat.go` 的
`HandleResumeSummary…` 與 `HandleGetMemberResumeSummary…`：只組 payload、只 `writeJSON`）。
也就是說**「這個 agent 有沒有接回現場」今天在 server 上沒有任何一個欄位可讀**。

所以載體改讀**後果**：開機**之前**先由 owner 身分種一則 chat 訊息，裡面帶一個隨機 nonce；
那則訊息只會從 resume 快照裡浮出來。②通過的條件是**agent 自己發出的、存在 server 上的訊息
把那個 nonce 帶回來**。沒接回現場的 agent 生不出那個字串。

⚠️ 這一格的兩個誠實限制，別讓它們被下一個人讀成「已經驗過了」：

- 它證明的是「**那份內容被讀到並帶回來了**」，不是「**`resume_summary` 這個工具被呼叫過**」。
  一個從 chat 直接翻到那則訊息的 agent 同樣會通過。要真正釘住工具呼叫，得在 server 上留下
  痕跡（例如 resume 讀取留一筆 last_resume_at），**那是 server 側的改動，本輪沒有做**。
- nonce 是種在 chat 裡的，而 resume 快照包含 chat——若哪天快照的組成變了，②會為了**載體的
  理由**而變紅。`actors/stub.sh` 因此在拉完快照後檢查 nonce 在不在，不在就先喊出來，讓人
  不要把載體的紅誤讀成 agent 的紅。

### ①為什麼不放寬

`presence=waking` 抓不到的時候，正確的動作是**把 `--interval` 調密、確認 collector 比 actor 早
起**，不是把判定放寬成「presence 曾經不是 offline」——那條放寬會讓一個從沒報到、只是掛著
SSE 的 agent 過關，而那正是要抓的病。

## 失敗時怎麼指出是哪一步

`judge.py` **七行都印**，每行 `PASS`/`FAIL` ＋ 一句「在 server 上找什麼、實際看到什麼」；最後
一行是 `[seven_gate] RED — failed at stepN <key> (<中文>): <原因>`，rc=1。全綠時最後一行逐字
是 `[seven_gate] all green`，rc=0。判定同時落成 `verdict.json`（機器可讀）。

「先失敗的那一步」放在最後一行，是因為那是呼叫者真正要的答案；但七行全印，因為第一個紅之後
的步驟往往也紅，而它們是紅在「前一步沒發生」還是紅在自己，要看得見才分得出來。

## 產物與 log（每次 run 都留）

`e2e_test/seven_gate/runs/<UTC stamp>/`（gitignored）：

| 檔 | 內容 |
|---|---|
| `run.log` | 整場 stdout/stderr（`tee`，含 setup/teardown） |
| `scene.json` | `agent_id` ＋ `scene_nonce` ＋ stamp（判定的輸入之一） |
| `journal.ndjson` | 每次輪詢一行的 server 事實時間序列——**這就是證據本體** |
| `collect.log` | collector 自己的 stderr |
| `actor.log` | agent 那一端的輸出（stub 或真 agent） |
| `verdict.json` | 七步逐項判定 |
| `friction.txt` | 追問的回答原文（人貼進去，載體不代寫、不摘要） |

`OC_SG_RUN_DIR` 可指定別的位置。**不覆蓋、不輪替**：一次 run 一個目錄，要清是人的決定。

## friction 追問

問法**逐字寫死在 `friction.md`**，`run.sh` 從那個檔案裡把問句原樣印出來——不在 shell 裡再寫
一份，兩份會漂。兩題：

- 哪一步你猶豫了／翻回去重讀了／用猜的？
- 你有沒有做出後來才發現做錯的事？

**不准問「順不順」那一類。** 理由寫在 `friction.md` 裡，`tests_guard` 案例 (21) 把兩題逐字釘住、
並且把那一類措辭列為必須不出現。綠的 run 也照問——關卡只知道事實有沒有落地，不知道對方是不是
繞了三圈才做到。

## actor 的合約（誰來扮 agent）

`run.sh` 不在乎 agent 那一端是什麼，只透過 env 交接：
`OC_SG_BASE` / `OC_SG_AGENT` / `OC_SG_AGENT_TOKEN` / `OC_SG_SCENE_NONCE` / `OC_SG_RUN_DIR` / `OC_SG_OWNER`。
**actor 的 rc 被記錄但不被採信**——一個 exit 0 卻什麼都沒做的 actor 照樣得紅，而它確實會紅，
因為判定來自 server。

- `actors/stub.sh`（預設）：用 member token 直接打 REST 走完七步。**它不是 agent。**
  `OC_SG_SKIP_STEP=<key>` 讓其中一步不發生——載體要能說「不」，而只在成功的 run 上跑過的關卡
  是沒人看過它說不的關卡。
- `actors/live.sh`：**尚未存在**。那才是真 agent，會 spawn claude、燒真 API 額度。

## 🔴 本輪明確沒做到的界線

- **從來沒有真 agent 走過這條關卡。** 本輪唯一跑過的 actor 是 stub，而 stub 是照著判定寫的，
  所以它證明「事實落地時關卡讀得對」，**完全不證明**「一個只讀開機說明的 agent 會決定去做這七件事」。
  這是整張票的目的，它還沒被回答。
- `②` 讀的是後果不是工具呼叫（見上）。
- `run.sh` 本身**未在隔離站上實跑過**（本輪只跑了 `judge.py` 與它的 hermetic 測試）。setup/hire/mint/
  plant 這一段是照 API 契約寫的，**未經執行驗證**。
- 這支不在 `run_all.sh` 裡、也不在 `bin/ci.sh` 裡。CI 守的是**判定邏輯**（`tests_guard` 案例 21），
  不是任何一次真的 run。
