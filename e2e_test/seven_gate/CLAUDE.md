# seven_gate/ — 任務路徑關卡（資料夾名是歷史遺跡：格子已經是九個）

進入 `e2e_test/seven_gate/` 時 nested-load。上層規則見 `e2e_test/CLAUDE.md` 與 root `CLAUDE.md`；
本檔記這個載體專屬的設計。

## 這是在保護什麼

開機說明（`seeds/*.md` 折成的 global context）**就是 agent 的操作手冊**。砍錯一段，後果不是
「文件變差」，是**新的 agent 根本不會開票、或開了推不動——而且它自己不會知道**。它不會報錯、
不會求救，它會很有禮貌地做別的事，然後回報一切正常。

所以要驗的不是文件好不好讀，是：**一個沒讀過舊版說明的全新 agent，在隔離環境裡，能不能把
一條真實路徑走完**。那條路徑固定，不可增刪：

報到 → 接回現場 → 開票 → 提出計畫 → 報一步完成 → 開一張等我回覆卡 → 回報收尾 →
**回覆另一個 agent** → **看得到圖**

⚠️ **資料夾叫 `seven_gate`，但格子現在是九個。** 第一次 baseline 之後 owner 補了兩項：

- 「跟其他 agent 溝通」（逐字：「包含 chat / reply card / task」「他要知道怎麼透過這三個元件跟
  owner 溝通」「或是跟其他 agent 溝通」）。原本那六步把 chat／卡／任務三個元件都練過了，但
  **收件人永遠是 owner**；跟同事講話是另一件事——不同的收件人，而且對面沒有一個有耐心的人。
- 「看得到圖」（源自一支已經不在這個 repo 裡的舊測試：跟 agent 玩猜數字，偷偷給它一張寫著答案的圖）。

名字是歷史遺跡，**`judge.py` 的 `STEPS` 才是契約**（別在別的文件裡複製一份步驟清單，那種清單會過期
——資料夾名裡的「七」就是這樣過期的）。

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

八個事實裡有一個是**會消失的**：①的 `presence=waking` 在 agent 掛上 SSE 之後就翻成 online。
只看最後一張快照的載體，分不出「報過到」和「根本沒開機」。

於是切成兩半：

- `collect.py` —— 只做 I/O。從 agent 開機**之前**就開始輪詢隔離 server，每次一行 JSON
  append 進 `journal.ndjson`。它不判定任何事。
- `judge.py` —— 純函數。吃一個 run 目錄，每格一行判定 ＋ 一個 rc。**不開 socket、不改任何東西**。

這個切法唯一的目的是：**判定邏輯不需要 server 就能測**。`tests_guard` 案例 (21) 餵它捏造的
bundle，跑在 `bin/ci.sh` 的第 (0) 階，不起任何服務。

## 每一格各自讀 server 上的哪一個事實

| # | 步驟 | server 事實 | 讀哪裡 |
|---|---|---|---|
| ① | 報到 | `presence == "waking"` 曾經出現過 | `GET /api/members/{id}`，journal 任一 sample |
| ② | 接回現場 | agent 發出的訊息裡含 scene nonce | `GET /api/chat`，`from == agent` |
| ③ | 開票 | 有一張 task 的 `creator_id == agent` | `GET /api/tasks` → 逐張 `GET /api/tasks/{id}` |
| ④ | 提出計畫 | 那張票 `steps[]` 非空 | 同上（`submit_plan` 是 `steps[]` 唯一的寫入者） |
| ⑤ | 報一步完成 | 那張票有 step `status == "done"` | 同上 |
| ⑥ | 開一張等我回覆卡 | 有一張卡 `from == agent` | `GET /api/reply-cards` |
| ⑦ | 回報收尾 | 那張票 `closeout_reported == true` | 同上票的 full DTO |
| ⑧ | 回覆另一個 agent | 有一則 `from == agent` **且 `to == peer`** 的訊息，且帶回 peer 的 nonce | `GET /api/chat` |
| ⑨ | 看得到圖 | agent 自己發出的訊息裡有那個**只存在於圖片像素**的號碼 | `GET /api/chat` |

③④⑤⑦ 綁在**同一張票**上（③找到的那張，多張時取最早）。不綁的話，載體會被 server 上任何
一張碰巧存在的票餵飽。

### ⑧「回覆另一個 agent」為什麼要另外坐一個人進來

前六格把 chat／卡／任務三個元件都練過了，但**收件人一律是 owner**。所以載體在開機前先僱第二個
member（peer），**用 peer 自己的 token**（不是 owner 的——owner 講話是②⑥已經涵蓋的那半）
發一則訊息給受測 agent，裡面帶 peer 自己的 nonce。⑦讀的是**一則 `from == agent`、
`to == peer` 的訊息**，而且**帶回那個 nonce**。

兩個條件是兩件不同的事，別把它們讀成一件：

- `to == peer`：它**真的對同事講話了**（不是對 owner、不是對自己）。這是 owner 要的那半，
  也是最低標。
- 帶回 nonce：那是**回覆**，不是自說自話——同事說的話被讀進去了。與②同一招，也有**同一個
  誠實限制**：它證明內容回來了，不證明是用哪一支工具讀到的。

⚠️ **本輪判定是雙向的**（收到→回覆兩半都要），因為載體起得動第二個對象。代價要講明白：
若哪天 peer 那則訊息從 agent 讀得到的地方消失，⑧會為了**載體的理由**變紅——`actors/stub.sh`
因此在回覆前先檢查 nonce 在不在，不在就先喊出來。

### ⑨「看得到圖」——這一格的成敗全在「答案不准出現在任何文字裡」

開機前種一則**帶圖片附件**的訊息給 agent，號碼**只畫在像素上**：不在訊息正文、不在檔名
（`handover-note.png`）、不在 mime、不在票面或計畫、不在任何它讀得到的檔案；PNG 也**不寫
`tEXt` 中繼資料**（那是文字，`strings` 就讀得到）。判定＝**agent 自己發出的訊息裡有沒有那個號碼**。

**號碼每次 run 重抽六位數，不是 42**：寫死的答案是模型可能背過的答案，而一個能靠記憶通過的格子
什麼都沒量到。

🔴 **這一格唯一的失效方式，是答案漏進文字**——那時候一個看不見圖的 agent 照樣過關，而**那個綠跟
真的綠一模一樣**。所以 `run.sh` 3d 有一道**洩漏掃描**：把 agent 讀得到的文字面（`/api/chat`、
`/api/tasks`、兩個卡 pane、`/api/members`、以及**它自己的 resume 快照**）全撈回來搜那個號碼，
**命中 ≠ 0 就拒跑**。而且先跑一次**陽性對照**——用同一支掃描器去搜 scene nonce（那個**確實**在
文字裡），對照找不到就代表掃描器壞了，「零命中」也就沒有意義，同樣拒跑。實跑那一輪印的是：
`leak scan: answer 0 hits in readable text (positive control: scene nonce 2 hit(s) — the scanner works)`。

⚠️ **誠實的界線**：
- **stub 綠不代表「模型看得見」**。stub 不會 OCR，號碼是**用 env 告訴它的**（跟 scene nonce 一樣）；
  它真正做的是**把附件的 bytes 從 `/api/chat/attachment/<id>` 抓下來**。所以 stub 綠＝「關卡讀得對、
  而且附件那條路真的通」，**看不看得見只有 `actors/live.sh` 答得了**。
- 答案會寫進 `runs/<stamp>/scene.json`（judge 要用）。那是**載體的目錄**，不是 agent 的工作區，
  但 live run 的 agent 跟它在同一台機器上——這條沒有被構造擋住，只是沒有理由去翻。
- ⚠️ **這一格順帶是附件路徑（上傳→列出→抓 bytes）在這個 repo 裡的第一個會跑的測試**：
  `tests/06_chat_attachments.spec.js` 與 `11_attachment_content_fidelity.spec.js` 測的是 UI 那一端，
  **沒有任何測試在守「agent 這一側真的把檔案抓下來」**這條路。

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

第一次 baseline（`runs/baseline-20260810T065500Z`）三步紅：報到、報一步完成、回報收尾
（那時候還是七格，編號是①⑤⑦；⑦現在是「回覆另一個 agent」，回報收尾已經是⑧——**這一段講的是
那三件事，不是今天的編號**），而現場**查不出原因**：
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

| 哪一件 | 真正的原因 | server 回的原文 |
|---|---|---|
| 報到（①） | `presence=waking` 需要 **desired_state==online ∧ 新鮮的 waking_since**（`domain.go` `PresenceState`），**兩個都要**。剛僱進來的 member 是 `desired_state=offline`，所以 `report_waking` 回 **200**、`waking_since` 也真的蓋了，投影出來還是 `offline`——教科書級的 (b)。 | （無錯誤：`POST /api/self/waking` → **HTTP 200**，body 裡 `"desired_state":"offline","presence":"offline"`） |
| 報一步完成（⑤） | `pending → done` 不是合法的 agent transition（`domain.go` `agentStepTransitions`），要 `pending → in_progress → done`。 | `HTTP 409 {"error":{"code":"conflict","message":"illegal step transition 'pending' -> 'done'"}}` |
| 回報收尾（今天的⑧） | closeout **只收 terminal 的票**（`api_tasks.go`），而票是由 steps 推導成 done 的——所以最後一步得真的走到 done，交棒宣告也騎在**那一通**上，不是騎在 closeout 上。 | `HTTP 409 {"error":{"code":"conflict","message":"task 't-…' is still open (not_started) — close-out is reported after the task ends"}}` |

修 ① 的動作在 **owner 那一側**（`run.sh` 的 2b 打 `activate`），不在 actor 裡：那是 owner 把人打開，
不是那條路徑上的任何一格。順序不能反——`activate` 會把 `waking_since` 歸零。

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
⑥成功反而讓收尾那一格不可能成立。

## 收集窗必須活得比 actor 久（不是一個數字，是一條關係）

第一版把兩個常數分開寫：collector 起在 `--seconds 900`，而 `actors/live.sh` 最久會等
`30 + 120 + 1800 + 300 ≈ 2250` 秒。**照預設跑，collector 會比 actor 早二十幾分鐘收工**，之後落地的
每一個事實 judge 都看不到——而它吐出來的是「回報收尾 FAIL」。**又是一個載體的坑，卻讓紅指著 agent。**
第一個踩到的人是靠手動調 `OC_SG_MAX_SECONDS` 繞開的，而**下一個人不會知道有那個旗標**。

所以現在釘的是**關係**，不是數字（`lib/window.sh`）：

```
collector 收集窗  ≥  actor 預算（machine + spawn + live + friction + card×2）
```

- 那些等待時間的**預設值只有一個家**（`lib/window.sh`）。`run.sh` 與兩支 actor 都 source 它、直接用
  `$OC_SG_LIVE_WAIT`，**不准再寫第二個 `:-1800`**——兩個常數靠人維持一致，就是這個 bug 的形狀。
- collector 的秒數是**推導出來的**（`sg_collect_seconds`），不是另外設的。
- `run.sh` 起 collector 之前先 `sg_assert_collection_window`，不成立就**拒跑**：窗口破了的那一輪，
  判定本身不可信。
- CI：`tests_guard` 案例 22 釘四件事——預設成立且窗**嚴格大於**預算（不是兩邊都零的巧合）、
  拉長 `OC_SG_LIVE_WAIT` 窗會跟著長（證明它是推導的）、**mutant 把推導換回常數 900 必須轉紅**、
  以及那些旗標在別處沒有第二份預設值。實測：預算 2310s、窗 2433s；mutant rc=1。

## 鎖定 runtime / model / effort（regression 要能比較兩組）

`OC_SG_RUNTIME` / `OC_SG_MODEL` / `OC_SG_EFFORT` 在 hire 之後、activate **之前**寫進成員列
（`PATCH /api/members/{id}`）。**不動 spawn 那條鏈**：server 自己會把這三個欄位放進 START frame
（`reconcile.go` buildStartFrame 的 `Runtime/Model/Effort`），warden 那邊本來就照著它挑 runtime。
順序不能反——activate 才是觸發 reconcile 的那一下，設定必須先在列上。

**設完一定讀回來，不一致就拒跑。** 一個回 200 卻存成別的值的 PATCH，會產生「宣稱跑 A、其實量到 B」
的一輪，而那正是這個載體存在的理由要消滅的東西。讀回來的值也寫進 `scene.json`
（`agent_runtime` / `agent_model` / `agent_effort`）並印在 log 上，所以每一輪都記得住自己是誰。
⚠️ 沒指定時**照樣讀回來**——記錄的是「實際跑成什麼」，不是「本來想跑什麼」。

**實測（stub，兩組各一輪，九格全綠 rc=0）**：
`runtime=claude model=opus effort=medium` 與 `runtime=codex model=gpt-5.6-luna effort=max`，
兩組的 `member config (read back from the server)` 都與指定值逐字相同。
**陽性對照**：`OC_SG_EFFORT=bogus` ⇒ server 回
`HTTP 422 effort must be one of [high low max medium]; got 'bogus'`，載體當場拒跑（rc=2）——
所以「讀回來相符」不是一句空話，這道檢查真的會說不。

## 為什麼不是拿 `task_system_e2e.sh`（那支「寫 42」的）來當 live actor

repo 裡**已經有**一支會起真 claude 的 e2e：`e2e_test/task_system_e2e.sh`——建一張 outsource 任務、
`inputs {output_file, number:42}`，然後**用磁碟產物驗收**（`poll_file_eq "$SYNTH_OUT" "42"`，
註解寫著 "Disk truth only"；stage D 還有 3/5/7 → 15 的 fork-join）。查過了，**它跟圖片無關**：
祕密是走 `manual.sop_md` ＋ `task.inputs`，不是藏在圖裡；整個 repo 目前**沒有任何**「給 agent 看圖」
的測試素材或案例。

**結論：那支的 spawn 機制不能直接拿來當本關卡的 live actor，理由是結構性的，不是懶：**

1. 它起的是 **outsource worker（`ow-…`）**，由 server 的排程器（`outsource_sched.go` →
   `worker_spawn.go`）決定並 mint token。而本關卡判的是一個 **member（`m-…`）**：③要
   `creator_id == agent`，而**外包 worker 依規定一張票都不能開（403）**——用那條路徑，③在構造上
   就不可能綠。
2. 它的隔離是**整套 namespaced 安裝**（`oc_resolve_instance` ＋ 真的 `bin/ocserver install` ＋
   `oc_bootstrap_warden`），本關卡用的是 `e2e_test/setup.sh` 的 `:8791`（`go build` ＋ serve，
   不安裝）。硬接會變成同一個 harness 裡兩套隔離機制。
3. 它是**全站重置型**、要 `OC_TASK_SYSTEM_YES=1`，canonical 模式甚至會動正式安裝。

**有被沿用的**：`actors/live.sh` 走的 member spawn 那條鏈（onboard → `ocwarden run` → activate →
tmux＋claude）就是 `tests/05_machine_onboarding_spawn.live-agent.spec.js` 已經跑過的那一條，
沒有另造第二套。**值得抄的還有**（本輪沒抄，記在這裡給下一個人）：`task_system_e2e.sh` 的
`poll_file_eq`（磁碟產物驗收的原語）與它「每通呼叫都記狀態碼」的 `api_post_logged`——後者跟
本載體的 `lib/http.sh` 是同一個教訓，兩邊各自學過一次。

## 失敗時怎麼指出是哪一步

`judge.py` **每一格都印一行**，每行 `PASS`/`FAIL` ＋ 一句「在 server 上找什麼、實際看到什麼」；最後
一行是 `[seven_gate] RED — failed at stepN <key> (<中文>): <原因>`，rc=1。全綠時最後一行逐字
是 `[seven_gate] all green`，rc=0。判定同時落成 `verdict.json`（機器可讀）。

「先失敗的那一步」放在最後一行，是因為那是呼叫者真正要的答案；但每一格全印，因為第一個紅之後
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
| `verdict.json` | 每一格逐項判定 |
| `rc` | judge 的 rc（0 全綠 / 1 有紅），不經管線取 |
| `friction.txt` | 追問的回答原文（stub run 由人貼進去；live run 由 `live.sh` 把 **agent 自己發出的訊息**原樣寫入。**載體不代寫、不摘要、不評分**——沒回答就寫「沒回答」） |
| `scene-image.png` | ⑨種下去的那張圖（號碼只在像素裡） |
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
`OC_SG_OWNER` / `OC_SG_OWNER_TOKEN` / `OC_SG_PEER` / `OC_SG_PEER_NONCE` / `OC_SG_IMAGE_ANSWER` /
`OC_SG_RUNTIME`（本輪釘的 runtime，已由 server 讀回過）。
**actor 的 rc 被記錄但不被採信**——一個 exit 0 卻什麼都沒做的 actor 照樣得紅，而它確實會紅，
因為判定來自 server。

⚠️ `OC_SG_OWNER_TOKEN`（owner 那一側）在契約裡，是因為**真 agent 需要對面有個人**：得有人交辦、
得有人回卡、得有人事後問那兩題。它**偽造不了任何一個被判定的事實**——①是綁 caller 自己 token 的
self-report（owner 拿去報只會蓋到 owner 頭上），②⑥比對 `from == agent`、③比對 `creator_id == agent`、
④⑤⑦掛在**那張**票上。所以拿著它的 actor 一樣沒辦法把紅的 run 弄綠。

- `actors/stub.sh`（預設）：用 member token 直接打 REST 走完整條路徑。**它不是 agent。**
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
  ⚠️ 特別點名兩個**仍然**沒驗過的假設：(a) `run.sh` 先 activate（無機器）留下的 reconcile 狀態，
  會不會讓 live.sh 第二次 activate（帶 machine_id）的 START 被 backoff 延後；(b) tmux socket 名沿用
  `cli/ocwarden/tmux.go` 的 `officraft`，namespaced 安裝下不是這個名字。
  ✅ **已經驗掉的**（這一輪實測，沒有起真 agent）：codex 那條路**在這台機器上三個相依項都解得到**——
  `codex login status` 回 `Logged in using ChatGPT`、`/Users/eva/.local/bin/codex` 可執行、
  `ocwarden` build 得出來且跑得動（⇒ `WardenBin = os.Executable()` 解得到，那是 codex sidecar 的前提）。
  `live.sh` 現在也在**自己這一端**先問一次 `codex login status`，不通就當場拒絕——warden 的同一道
  preflight 會失敗成 wake timeout，而那個逾時看起來像 agent 的錯。
  ⚠️ **但那只證明相依項齊備，不證明 codex 真的 spawn 得起來**：那需要起真 agent。
  ⚠️ 也要分清楚：站上那個起不來的 codex 外包（`wake_timeout … not logged in`）是**另一台/另一個
  daemon context**（launchd PATH、可能不同使用者），跟這裡量到的「我這個 shell 登入著」是兩件事。
- **stub 證明的只有「事實落地時關卡讀得對」**，它是照著判定寫的，**完全不證明**
  「一個只讀開機說明的 agent 會決定去做這七件事」。那是整張票的目的，還沒被回答。
- `②` 讀的是後果不是工具呼叫（見上）。
- `run.sh` 本身**已在隔離站上實跑過**：stub actor **九格全綠 rc=0**（`runs/nine-20260810T081142Z/`）；
  另外三次刻意讓一格不發生——`OC_SG_SKIP_STEP=` `reply_card` / `peer_message` / `image_answer`——
  都 rc=1 並精準點名那一格（`runs/sayno-*`、`runs/saynopeer-*`、`runs/saynoimage-*`），而且
  **其餘格子照樣各自判**（跳過⑨時⑧仍綠）。live actor 那條路徑一次都沒跑過。
- 這支不在 `run_all.sh` 裡、也不在 `bin/ci.sh` 裡。CI 守的是**判定邏輯**與載體的幾條靜態不變式
  （`tests_guard` 案例 21：21a–21e 判定與 friction 措辭、21f 沒有裸 curl／狀態碼有被抓、
  21g live actor 的花錢開關是嚴格 include flag），**不是任何一次真的 run**。
