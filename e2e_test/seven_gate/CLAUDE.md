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
4. **每次 run 的 log 與磁碟產物要保留**（見〈產物〉），而且**任何一支對 server 的呼叫都不准把回應丟掉**
   ——method / path / **HTTP 狀態碼** / **回應內容**四樣一起進 log（`lib/http.sh` 是唯一的呼叫入口）。
   理由見〈為什麼每一通呼叫都要留狀態碼〉。
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

## 為什麼每一通呼叫都要留狀態碼

第一次 baseline（`runs/baseline-20260810T065500Z`）①⑤⑦ 三步紅，而現場**查不出原因**：
當時每一通呼叫都寫成 `curl … >/dev/null`，於是**兩種完全不同的病長得一模一樣**——

- **(a) 呼叫失敗**：server 拒收（4xx/5xx）或根本沒回答。什麼都沒寫進去。
- **(b) 呼叫成功但事實沒落地**：HTTP 200，而 server 上就是沒有那個事實。

(b) 才是危險的那個，因為它正是**API 契約寫錯**時的樣子。所以規則是硬的：
**每一通呼叫的 method、path、HTTP 狀態碼、回應內容都要落進 log**（`run.log` 與 `http.log` 各一份），
而唯一的呼叫入口是 `lib/http.sh` 的 `sg_http`。這件事不靠自律：`tests_guard` 案例 21f 要求
`run.sh` 與 `actors/*.sh` 裡**一通裸 curl 都沒有**，並且 `lib/http.sh` 自己要抓 `%{http_code}`、
不准把回應送進 `/dev/null`。

⚠️ 被禁的是 `curl … >/dev/null`，**不是** `sg_http … >/dev/null`：`sg_http` 回來的時候狀態碼與
內容已經在 log 裡了，呼叫端不需要 body 就可以丟掉那份 stdout 複本。

### 那三步當初到底錯在哪（實測，錯誤原文照抄）

三個都是**契約讀錯**，而且原文一直都在碼裡：

| 步 | 真正的原因 | server 回的原文 |
|---|---|---|
| ① 報到 | `presence=waking` 需要 **desired_state==online ∧ 新鮮的 waking_since**（`domain.go` `PresenceState`），**兩個都要**。剛僱進來的 member 是 `desired_state=offline`，所以 `report_waking` 回 **200**、`waking_since` 也真的蓋了，投影出來還是 `offline`——教科書級的 (b)。 | （無錯誤：`POST /api/self/waking` → **HTTP 200**，body 裡 `"desired_state":"offline","presence":"offline"`） |
| ⑤ 報一步完成 | `pending → done` 不是合法的 agent transition（`domain.go` `agentStepTransitions`），要 `pending → in_progress → done`。 | `HTTP 409 {"error":{"code":"conflict","message":"illegal step transition 'pending' -> 'done'"}}` |
| ⑦ 回報收尾 | closeout **只收 terminal 的票**（`api_tasks.go`），而票是由 steps 推導成 done 的——所以最後一步得真的走到 done，交棒宣告也騎在**那一通**上，不是騎在 closeout 上。 | `HTTP 409 {"error":{"code":"conflict","message":"task 't-…' is still open (not_started) — close-out is reported after the task ends"}}` |

修 ① 的動作在 **owner 那一側**（`run.sh` 的 2b 打 `activate`），不在 actor 裡：那是 owner 把人打開，
不是七步裡的任何一步。順序不能反——`activate` 會把 `waking_since` 歸零。

### 修好⑤之後才浮出來的第四件事：⑥的卡會把步驟鎖住

⑥ 開的卡若由「正在執行某張 active task 的人」開出，會**自動 bind 到那張票的當前步驟**並把它推進
`waiting_owner`（`api_replycards.go` `inferCardTaskStep` → `armStepWithCard`），而 `waiting_owner`
**只有一個出口：owner 回答**。而且那時候得**真的有一個 in_progress 的步驟**，否則卡根本開不出來：

```
HTTP 409 cannot bind this ask to a step: no step of task 't-…' is in_progress,
so the ask can place no 等我回覆 hold and the task would keep running past it. …
```

所以載體多了一個 **owner 端的回卡人**（`run.sh` 步驟 4b，背景跑、只回這個 run 的 agent 開的卡、
收尾時按**確切 PID** 收掉）。那不是為了讓測試過關而加的方便門——**那就是對面那個人**。沒有他，
⑥成功反而讓⑦不可能成立。

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
| `http.log` | **每一通對 server 的呼叫**：method / path / HTTP 狀態碼 / 回應內容 |
| `verdict.json` | 七步逐項判定 |
| `rc` | judge 的 rc（0 全綠 / 1 有紅），不經管線取 |
| `friction.txt` | 追問的回答原文（stub run 由人貼進去；live run 由 `live.sh` 把 **agent 自己發出的訊息**原樣寫入。**載體不代寫、不摘要、不評分**——沒回答就寫「沒回答」） |
| `warden.log` | 只有 live run 才有：那一顆 `ocwarden run` 的輸出 |

`OC_SG_RUN_DIR` 可指定別的位置。**不覆蓋、不輪替**：一次 run 一個目錄，要清是人的決定。

## friction 追問

問法**逐字寫死在 `friction.md`**，`run.sh` 從那個檔案裡把問句原樣印出來——不在 shell 裡再寫
一份，兩份會漂。兩題：

- 哪一步你猶豫了／翻回去重讀了／用猜的？
- 你有沒有做出後來才發現做錯的事？

兩個讀者，**一份問句**：`run.sh` 把它印出來給人問，`actors/live.sh` 把同樣的字送給真 agent。
抽取那兩行的程式只有一份（`lib/friction.sh` 的 `sg_friction_questions`）——第二份 sed 就是
第二份問句，而會漂的永遠是被問出去的那一份。live run 的 `friction.txt` 裡的答案是
**agent 自己發出的訊息原文**；沒收到就明寫沒收到，**載體不代寫**。

**不准問「順不順」那一類。** 理由寫在 `friction.md` 裡，`tests_guard` 案例 (21) 把兩題逐字釘住、
並且把那一類措辭列為必須不出現。綠的 run 也照問——關卡只知道事實有沒有落地，不知道對方是不是
繞了三圈才做到。

## actor 的合約（誰來扮 agent）

`run.sh` 不在乎 agent 那一端是什麼，只透過 env 交接：
`OC_SG_BASE` / `OC_SG_AGENT` / `OC_SG_AGENT_TOKEN` / `OC_SG_SCENE_NONCE` / `OC_SG_RUN_DIR` /
`OC_SG_OWNER` / `OC_SG_OWNER_TOKEN`。
**actor 的 rc 被記錄但不被採信**——一個 exit 0 卻什麼都沒做的 actor 照樣得紅，而它確實會紅，
因為判定來自 server。

⚠️ `OC_SG_OWNER_TOKEN`（owner 那一側）在契約裡，是因為**真 agent 需要對面有個人**：得有人交辦、
得有人回卡、得有人事後問那兩題。它**偽造不了任何一個被判定的事實**——①是綁 caller 自己 token 的
self-report（owner 拿去報只會蓋到 owner 頭上），②⑥比對 `from == agent`、③比對 `creator_id == agent`、
④⑤⑦掛在**那張**票上。所以拿著它的 actor 一樣沒辦法把紅的 run 弄綠。

- `actors/stub.sh`（預設）：用 member token 直接打 REST 走完七步。**它不是 agent。**
  `OC_SG_SKIP_STEP=<key>` 讓其中一步不發生——載體要能說「不」，而只在成功的 run 上跑過的關卡
  是沒人看過它說不的關卡。
- `actors/live.sh`：**真 agent 那一端**。onboard 一台機器 → 跑真的 `ocwarden run` → owner 把 agent
  activate 到那台機器上 → warden 在 tmux 裡 spawn 真的 claude（與
  `tests/05_machine_onboarding_spawn.live-agent.spec.js` 同一條鏈，那支是這條鏈唯一真的被執行過的地方）。
  **🔴 會燒真 API 額度**，所以雙重 default-off：`run.sh` 的預設 actor 是 stub，而這支自己在
  `OC_SG_LIVE_AGENT` **嚴格等於 `"1"`** 之前什麼都不做（打錯字一律落到「沒跑、沒花錢」；21g 釘住）。

  啟動方式一句話：`OC_SG_LIVE_AGENT=1 OC_SG_ACTOR=actors/live.sh bash e2e_test/seven_gate/run.sh`

  它**只做 owner 會做的事**：交辦、給機器、回卡、事後逐字問那兩題。開票／提計畫／報步驟／開卡／
  回報收尾一件都不代做——agent 不做就是紅，而**那個紅就是答案**，不是載體的 bug。
  交辦原文寫死在 `assignment.md`（讀那個檔的 header：**它刻意不提那七件事**，否則量到的只是
  「會不會照清單做」）；那段話裡刻意留了一個**必須問人的岔路**，⑥才有真的理由存在。
  headless 安全面：warden 的 launch line 本來就帶 `--dangerously-skip-permissions` 並
  `--disallowedTools AskUserQuestion`（`cli/ocwarden/spawn.go`），所以 spawn 出來的 claude
  不會停在一個沒人看得到的選單上；live.sh 自己則是「缺什麼就當場大聲拒絕」，沒有任何互動確認。
  收尾只殺**自己記下的**：那一個 tmux session 名 ＋ 那一顆 warden PID，不用 `pkill -f`。

## 🔴 明確沒做到的界線

- **從來沒有真 agent 走過這條關卡——`actors/live.sh` 已經寫好，但一次都沒被執行過。**
  寫它的人沒有按那顆按鈕（起真 agent 會產生實際花費，那一按是 owner 的）。所以它的**每一段**
  ——onboard、`ocwarden run`、activate 帶 machine_id、等 tmux、逐字追問、寫 `friction.txt`——
  都只**照契約與 `tests/05_*.live-agent.spec.js` 寫過，未經執行驗證**。第一個按下去的人請預期要 debug；
  好消息是每一通呼叫的狀態碼與內容都在 `http.log`，debug 是「讀」不是「猜」。
  ⚠️ 特別點名兩個沒驗過的假設：(a) `run.sh` 先 activate（無機器）留下的 reconcile 狀態，會不會讓
  live.sh 第二次 activate（帶 machine_id）的 START 被 backoff 延後；(b) tmux socket 名沿用
  `cli/ocwarden/tmux.go` 的 `officraft`，namespaced 安裝下不是這個名字。
- **stub 證明的只有「事實落地時關卡讀得對」**，它是照著判定寫的，**完全不證明**
  「一個只讀開機說明的 agent 會決定去做這七件事」。那是整張票的目的，還沒被回答。
- `②` 讀的是後果不是工具呼叫（見上）。
- `run.sh` 本身**已在隔離站上實跑過**：stub actor 七步全綠 rc=0（`runs/green-20260810T073637Z/`），
  另跑一次 `OC_SG_SKIP_STEP=reply_card` 得到 rc=1 並精準點名⑥（`runs/sayno-*`）——關卡說得出「不」。
  live actor 那條路徑一次都沒跑過。
- 這支不在 `run_all.sh` 裡、也不在 `bin/ci.sh` 裡。CI 守的是**判定邏輯**與載體的幾條靜態不變式
  （`tests_guard` 案例 21：21a–21e 判定與 friction 措辭、21f 沒有裸 curl／狀態碼有被抓、
  21g live actor 的花錢開關是嚴格 include flag），**不是任何一次真的 run**。
