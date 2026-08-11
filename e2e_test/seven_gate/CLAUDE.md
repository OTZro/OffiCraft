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
| ⑤ | 報一步完成 | 那張票**曾經被觀察到**「已有步驟 done、而收尾還沒回報」 | journal 的**時序**（見〈⑤ 為什麼是一個時間事實〉） |
| ⑥ | 開一張等我回覆卡 | 有一張卡 `from == agent` | `GET /api/reply-cards` |
| ⑦ | 回報收尾 | 那張票 `closeout_reported == true` | 同上票的 full DTO |
| ⑧ | 回覆另一個 agent | 有一則 `from == agent` **且 `to == peer`** 的訊息，且帶回 peer 的 nonce | `GET /api/chat` |
| ⑨ | 看得到圖 | agent 自己發出的訊息裡有那個**只存在於圖片像素**的號碼 | `GET /api/chat` |

③④⑤⑦ 綁在**同一張票**上（③找到的那張，多張時取最早）。不綁的話，載體會被 server 上任何
一張碰巧存在的票餵飽。

### ⑤ 為什麼是一個時間事實——前兩個問法各自壞在哪

**第一個問法：「這張票有沒有任一 step 到 done」——不可能答錯。** ⑦（closeout）只收 terminal 的票，
而票是**由 steps 推導**成 terminal 的（`domain.go` `DeriveTaskStatus`：`allDone → done`）
⇒ **⑦綠必然蘊含⑤綠**，構造上不存在「⑤紅而⑦綠」的世界。**實測（`7233fa3`）**：
`OC_SG_SKIP_STEP=step_done` ⇒ ⑤仍然 PASS、⑦FAIL——收尾那一下把最後一步推到了 done，
而舊的⑤只要求「有一個 done」。

**第二個問法：「done 的步驟是計畫的前綴，且 `finished_ts` 沿 order 非遞減」——比零還糟。**
它對**兩種 server 刻意產生的正常行為**確定性假紅，而且紅在點名 agent：

- **replan。** `submit_plan` 把沒重列的節點凍結成 `superseded`，而 `ReplaceTaskPlan`
  **把它留在原位重編**；`DeriveTaskStatus` 與 `TaskProgress` 兩邊都**跳過**它。於是一列 superseded
  必然排在後續 done 節點之前 ⇒ 前綴判定在**任何一次 replan 之後必敗**。而開機說明本身就在教
  replan（`seeds/system_interaction.md`：「重新規劃——用 `submit_plan` 重交 plan」）。
- **並行。** SPEC §3.1：每一列 step 就是一個 leaf、並行項是各自的列，所以並行段的節點本來就
  可能亂序完成。順序判定會把那叫做「back-filled」。

而它換來的鑑別力幾乎是零：**⑦綠 ⇒ 所有非 superseded 節點都 done ⇒ 前綴那半恆真**，
真正還在做事的只剩順序那半——也就是唯一會誤判的那半。

**現在⑤問的是一個時間事實，而它從 journal 讀：**

> **曾經存在一個時刻，這張票已經有步驟 `done`、而收尾（`closeout_reported`）還沒回報。**

**⑦綠的時候鑑別力從哪裡來**：⑦只讀這張票的**最後一個狀態**，對它**經過**哪些狀態一個字都說不出來。
一個從頭到尾不動步驟、最後一口氣把全部翻成 done 再收尾的 agent，最後一張快照跟真的走完
**長得一模一樣**，而這一格紅——因為沒有任何一次取樣抓到它在半路。這跟①靠的是同一個形狀
（`presence=waking` 幾秒後就消失），也正是這個檔案讀 journal 而不是讀最終快照的理由。

**而且它對上面兩種假紅免疫**：superseded 的 status 不是 `done`，不會被誤認成進度；
步驟之間的**順序完全不看**，並行段倒著完成照樣留下「某個節點 done、票還開著」的那一刻。

⚠️ **誠實的界線**：
- **這一格的失效方式是取樣**：如果**所有**步驟回報加上收尾全部落在**同一個 collector 輪詢間隔內**
  （`OC_SG_INTERVAL`，預設 1 秒），journal 就沒抓到中間態，這一格會為了**載體的理由**變紅。
  失敗訊息**逐字寫著這件事**，要讀的人先去看 `journal.ndjson` 與輪詢間隔，不要先怪 agent。
  緩解不是靠祈禱：collector 從開機前就起、整場都在輪詢，而路徑本身在步驟與收尾之間夾著一次
  ⑥的卡（owner 回卡才解得開 `waiting_owner`）。**但這一條沒有在真跑上實測過**（本輪沒跑）。
- 單步計畫**照樣過**：只要那一步 done 的當下收尾還沒回報。
- 它擋不住「邊做邊報但其實是照抄」這一類語意問題——這一格量的是時序，不是內容。
- `tests_guard` 案例 21b-i 釘「⑤紅⑦綠造得出來」，**而且釘那份 fixture 是 server 產得出來的狀態**
  （舊的那份是 `step0=todo` ＋ `task=done`，`DeriveTaskStatus` 產不出來——它證明的是判定式可被
  證偽，不是那個世界存在）；21b-iii 反過來釘**replan ＋ 並行亂序必須是綠的**。

### ③ 取最早的那張票，是這個檔案裡**唯一的一次猜**

③找到的票決定④⑤⑦讀哪一張。多張時**取 `created_ts` 最早的那張**，而 server 上
**沒有任何一個事實**能說「這一張才是這一輪要驗的票」。

**實測（改之前那棵樹）**：讓 stub 在③之前多開一張空的草稿票 ⇒ ③**PASS（指著草稿票）**、
**④⑤⑦全 FAIL**，首紅是「提出計畫 FAIL — task … has an empty steps[]」。**三格假紅，
而且指著 agent。**

**為什麼不改成「認出哪一張才是這一輪的」（原本的選項 a）——做不到，理由是結構性的：**
載體確實會種 nonce（`scene_nonce` 種在 chat、`peer_nonce` 由 peer 發話），但那些 nonce 只會
出現在**agent 自己寫的訊息**裡；票是 agent 自己開的，**票面上會不會出現 nonce，只取決於
交辦內容有沒有叫它寫**。而 `assignment.md` 的 header 就寫著它**刻意一個字都不提票**——
提了，量到的就只剩「會不會照清單做」。所以要讓票帶記號，就得先毀掉③在測的東西。
（stub 的票面確實帶著 nonce，但那是 stub 照判定寫的；`actors/live.sh` 那條路上不成立，
而那條路才是這個載體存在的理由。）

**另一條更糟的路是「挑那張滿足④⑤⑦的票」**——那會讓④⑤⑦**構造上不可能紅**，
剛好是這一輪在⑤身上修掉的那個病。

**所以選的是（b）：維持取最早，但不准它安靜。** `judge.py` 在 agent 開了不只一張票時，
把警語接在③的證據**以及④⑤⑦每一句 FAIL 理由後面**（逐字：`⚠️ THIS AGENT OPENED N TASKS
(…) AND THE GATE JUDGES THE EARLIEST …`），所以**呼叫者真正會讀的那最後一行**就帶著
「可能是多開了票，不是 agent 沒做」。案例 21b-ii 釘住三件事：兩張票時警語出現且點名兩張、
**最後一行**也帶警語、以及**只有一張票時不准印**（永遠亮著的警語沒有人讀）。

⚠️ 這一格留下的曝險是**真的還在**，只是不再沉默：一個先開草稿票的 agent 仍然會拿到
④⑤⑦的紅。要真的關掉它，得有一個「這一輪的票」在 server 上的事實——那是 server 側的改動。

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

🔴 **這一格唯一的失效方式，是答案漏進 agent 拿得到的地方**——那時候一個看不見圖的 agent 照樣
過關，而**那個綠跟真的綠一模一樣**。三個面，各自被誰擋住，逐條寫在這裡，**因為上一版只擋住第一個
而註解宣稱三個都擋住了**：

1. **server 上的文字。** `run.sh` 3d 的洩漏掃描：`/api/chat`、`/api/tasks`、兩個卡 pane、
   `/api/members`、以及**它自己的 resume 快照**，命中 ≠ 0 就拒跑。先跑**陽性對照**（用同一支掃描器
   搜 scene nonce，那個確實在文字裡），對照找不到＝掃描器壞了，同樣拒跑。
2. **檔案。** 同一支掃描器現在**也走 `$RUN_DIR` 底下每一個檔**（`scene-image.png` 除外——答案本來
   就該在那些像素裡，而一份壓縮 PNG 偶然命中六個 ASCII 數字會是一個沒人能處理的拒跑）。
   **檔案那半有自己的陽性對照**：scene.json 明文帶著 scene nonce，走不到檔案的 walk 會被抓出來。
   而且 `scene.json` **不再存明文答案**，改存 `image_answer_salt` ＋ `image_answer_sha256`
   ＋ `image_answer_len`，judge 把 agent 訊息裡每一段等長數字拿去雜湊比對。
   ⚠️ **那不是保密**：salt 就在同一個檔裡，10^6 個候選對一支程式是瞬間的事。它把門檻從 `grep`
   抬到「刻意去破」，僅此而已，別把它讀成別的。
3. **環境變數——這一面上一版整個沒有。** 鏈路是 `run.sh` 5（把答案 export 給 actor，因為 **stub
   需要**它）→ `live.sh` 起 warden（原本只 `env -u OC_WARDEN_TOKFILE` 一個）→ ocwarden
   `exec.Command` 沒設 `cmd.Env` ⇒ 繼承 `os.Environ()` → tmux `new-session` 再繼承一次
   （**最後一跳實測過**：拋棄式 socket 上 `new-session -d 'env > f'`，值真的在 f 裡）。
   ⇒ 真 agent 一句 `env` 就拿到②⑧⑨三個答案，外加 `SG_TOKEN`（owner 的 token）。
   現在 `lib/scrub.sh` 在 **actor → warden** 那一跳把載體**整個 `OC_SG_*` / `SG_*` 命名空間**
   拿掉（不是三個點名的變數——下一個秘密會由沒讀過這段的人加進 run.sh），並且 `sg_scrub_assert`
   **在 spawn 之前先證明**它真的拿掉了，連陽性對照都有（控制組找不到秘密＝這道檢查是空的，
   同樣拒絕）。**這一段解在載體側，`cli/ocwarden` 一個字都沒動**——那是全 fleet 共用的 spawn 路徑。
   守衛在 `tests_guard` 案例 26（hermetic，含一顆把 scrub 變成 no-op 的 mutant）。

⚠️ **仍然擋不住的**：live run 的 agent 跟載體**同一個使用者、同一台機器**。以上三件事拿掉的是
「答案被**交到**它手上」那條路；一個主動去翻 repo 樹、或拿 salt 去暴力搜的行程，沒有任何構造擋它。

⚠️ **誠實的界線**：
- **stub 綠不代表「模型看得見」**。stub 不會 OCR，號碼是**用 env 告訴它的**（跟 scene nonce 一樣）；
  它真正做的是**把附件的 bytes 從 `/api/chat/attachment/<id>` 抓下來**。所以 stub 綠＝「關卡讀得對、
  而且附件那條路真的通」，**看不看得見只有 `actors/live.sh` 答得了**。
- `runs/<stamp>/scene.json` 只帶 salt＋sha256＋長度，**不帶明文答案**（judge 用雜湊比對）。
  那是**載體的目錄**，不是 agent 的工作區，但 live run 的 agent 跟它在同一台機器上——
  **明文拿掉了，可達性沒有**。
- **stub 仍然是用 env 拿到答案的**，那是刻意的（它不會 OCR）。被構造擋掉的是**再往下一跳**：
  `live.sh` 交給 warden 的環境裡沒有它。
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
- **collector 自己沒有窗**：`collect.py` 的 `--seconds` **required、沒有 default**。原本那裡寫著
  `default=900.0`——那就是這個 bug 的另外半邊，而且**上面每一條斷言都看不到它**：22d 只掃 shell 檔裡
  有沒有第二個 `${KNOB:-…}`，掃不到 argparse。**實測**（在 52f6a4b 那棵樹上）：把 run.sh 的
  `--seconds "$COLLECT_SECONDS"` 整段刪掉——也就是讓那顆 900 重新生效、原病照原樣復發——
  `tests_guard` 仍然 **PASS=251 FAIL=0 rc=0**。現在忘記傳的人拿到的是 argparse 當場拒絕，不是安靜的 900。
- `run.sh` 起 collector 之前先 `sg_assert_collection_window`，不成立就**拒跑**：窗口破了的那一輪，
  判定本身不可信。
- CI：`tests_guard` 案例 22 釘的是**這條關係的每一段**，不是任何一個數字——預設成立且窗**嚴格大於**
  預算（不是兩邊都零的巧合，22a）、拉長 `OC_SG_LIVE_WAIT` 窗會跟著長（證明它是推導的，22b）、
  **mutant 把推導換回常數 900 必須轉紅**（22c）、那些旗標在別處沒有第二份預設值（22d）、
  **collector 自己沒有窗**（22e，行為面：不給就拒絕啟動並點名 `--seconds`）、
  以及**推導真的走到 collector**（22f：`--seconds` 後面必須是一個變數，而那個變數必須來自
  `sg_collect_seconds`；寫任何字面數字都紅，不只 900）。
  實測：預算 2310s、窗 2433s；`tests_guard` **PASS=254 FAIL=0 rc=0**。
  三顆 mutant 各自單獨一輪（每顆都 rc=1、各 1 條 FAIL）：
  ① run.sh 把 `--seconds "$COLLECT_SECONDS"` 整段刪掉、② 改成字面 `--seconds 900`、
  ③ `collect.py` 把 `default=900.0` 放回去。
  **鑑別力對照**：同樣三顆打在 **52f6a4b（本次改動之前）**上——①② 各 **PASS=251 FAIL=0 rc=0 全靜默**，
  ③ 在那棵樹上是 no-op（它本來就是 `default=900.0`，而那一輪同樣 251/0 rc=0）。
  ⚠️ 22e 的斷言**不是「rc != 0」**：一個還帶著 default 的 collect.py 會走過 argparse、再死在讀不到的
  token 檔，**同樣 rc != 0**（實測 mutant ③ rc=1、FileNotFoundError）。所以釘的是**它有沒有為了「沒人給窗」
  而拒絕**（訊息點名 `--seconds`），並配一格陽性對照：給了窗之後，同一通呼叫必須改成死在 token 檔、
  而且**不再提** `--seconds`。

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

## 一個字母花掉一次真跑：為什麼守衛不能是「跑跑看」

第一次真跑,agent **已經被 spawn 起來**、①報到 PASS,然後 actor 當場死在:

```
actors/live.sh: line 213: OC_SG_LIVE_WAITs: unbound variable
```

——就是上面那個 `window.sh` 重構,把 `${OC_SG_LIVE_WAIT:-1800}s` 改寫成 `$OC_SG_LIVE_WAIT` 時,
**把「秒」的那個 `s` 黏進了變數名**。actor 死掉 → 它的 trap 殺掉 tmux session → ②～⑨ 全紅。
判定寫的是「agent 什麼都沒做」,**事實是載體把它殺了**。錢照花,資訊零。

⚠️ **CI 從頭到尾是綠的,而且它沒有做錯**:CI 永遠不執行 `live.sh`——那支只在真跑時執行,而真跑正是
CI 絕對不能做的事。所以守衛不能是「跑跑看」,必須是**在不 spawn、不花錢的前提下,把每一個變數
引用走一遍**。那就是 `lib/varcheck.py`(案例 23):`set -u` 底下,一個引用只有兩種安全來源——
自己帶預設(`${V:-x}` 那一族),或名字真的在某處被綁。兩者皆非 = 沒人設過的名字 = 打錯字 = 半路暴斃。

- **mutant**:把那個錯字放回去,守衛 rc=1 並**逐字點名** `OC_SG_LIVE_WAITs`;同一份沒有錯字的複本
  必須綠(否則紅的是 fixture 不是錯字)。fixture 樹**刻意做成跟真實一樣的 `actors/` ＋ `lib/` 佈局**
  ——varcheck 是靠往上走找到 `lib/window.sh` 的,丟進裸的暫存目錄會讓每個旋鈕都看起來沒綁。
- 🔴 **射程按後果劃,不是一份檔名點名冊**——這一格自己也犯過同一個病。案例 23 原本寫死八個路徑,
  而它守的曝險屬於一個**性質**、不屬於那八個:`seven_gate` 底下任何一支 `.sh` 都是 **CI 從不執行、
  只有花過錢的真跑才會執行**的檔。**實測**:`lib/scrub.sh` 加進來(而且它跑在 **spawn 之前**那一跳,
  死在那裡最貴),一個 `$VARs` 錯字**完全靜默**,因為沒有人想到要去加第九行。現在它跟案例 24 用
  **同一條目錄查詢** ＋ **掃到檔數下限**(走到零個檔的掃描會安靜全過),並額外點名三支只有真跑會
  執行的檔(`actors/live.sh`、`lib/ownedkill.sh`、`lib/scrub.sh`)——**點名是為了讓查詢被縮小時會紅**,
  不是回頭當清單用。案例 23c 用植進 `lib/scrub.sh` 的同型錯字釘住這條射程。
- **同型掃描**結果:同一次改寫製造了**兩個**,第二個在 friction 段(`$OC_SG_FRICTION_WAITs`),
  真跑那次還沒走到就先死在第一個。兩個都修了。
- ⚠️ **守衛的射程**:它是**名字層級**的,而且**不做完整的 bash 引號解析**——所以它適用於這個載體的
  腳本,**不適合指著 `tests_guard/run.sh` 那種「內容大多是在談論別的腳本」的檔**(那裡的 `$x` 住在
  單引號 fixture 與 sed 程式裡,看起來跟真的展開一模一樣)。單引號區段**刻意不剝除**:剝了會讓
  一句 `"⑦'s fact"` 的撇號吃掉整行剩下的部分,而**藏起一個真引用的檢查器比多叫一次更糟**。

## 檢查要排在花錢之前

這次的形狀是**先花錢、再死在載體**。所以 `live.sh` 在 activate(＝spawn＝開始計費)那一行**之前**多了
一段 **PRE-SPEND PREFLIGHT**:把後半段會用到的等待變數全部逼出來展開一次(`set -u` 會在「提到」的
當下就炸),並且先把 friction 問句 parse 一遍(那個檔是整場最後才讀的,壞在那裡等於整輪白花)。
案例 23d 釘住「preflight 的行號 < 花錢那一行的行號」。

⚠️ **做不到全部**:preflight 只能保證「這些名字現在展開得出來」,擋不掉後半段的邏輯錯、server 中途
改行為、或環境在跑的過程中變化。真正把這一類擋在花錢之前的是 CI 裡那道靜態檢查——它**根本不用跑**。

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
| `scene.json` | `agent_id` ＋ `scene_nonce` ＋ `peer_*` ＋ stamp ＋ 圖片答案的 **salt/sha256/長度**（判定的輸入之一；**明文答案不在裡面**，見〈⑨〉） |
| `journal.ndjson` | 每次輪詢一行的 server 事實時間序列——**這就是證據本體** |
| `collect.log` | collector 自己的 stderr |
| `actor.log` | agent 那一端的輸出（stub 或真 agent） |
| `http.log` | **每一通對 server 的呼叫**：method / path / HTTP 狀態碼 / 回應內容 |
| `verdict.json` | 每一格逐項判定 |
| `rc` | judge 的 rc（0 全綠 / 1 有紅），不經管線取 |
| `outer.rc` | **載體自己的**終局訊號——不管怎麼死都會出現（見〈載體必須活過…〉）。**等待者要盯的是這一個**：`rc` 只有走到判定那一步才會有，`outer.rc` 連「半路被殺」都有 |
| `outer.status` | 一行說明它**為什麼**結束（`exit` / `signal:TERM` / `vanished:…`），給讀 log 的人用 |
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

## 載體必須活過「起它的那個 session」，而且不准無聲死掉（`lib/carrier.sh`）

一次**已經花掉錢**的真跑是這樣死的：`run.sh` 是某個 agent session 用背景指令起的，那個 session 被
relocate 收掉時，**載體被連坐殺死**——log 停在輪詢到一半，沒有錯誤、沒有 teardown、沒有判定。
而**它起的 agent 沒死**（agent 住在 tmux、是 detached daemon）。於是拿到最糟的組合：
**沒人判定、沒人 teardown、真 agent 繼續燒錢空轉**。

更貴的是第二半：**等的人永遠等不到**。等待者盯的是那一輪呼叫的 rc，而那個 rc 是由**已經死掉的那個
shell** 負責寫的，所以它從來沒被寫出來。**「死了」跟「還在跑」外觀完全一樣**（都是沉默），
直到幾小時後逾時。

而且查過：改之前 `git grep -nE 'nohup|setsid|disown' -- e2e_test/seven_gate` **零命中**——
**載體活不活得下來，完全取決於起它的人記不記得加 `nohup`**。「靠人記得」正是這個關卡要消滅的東西。

🔴 **但上面那個開場故事，這一包擋不住——先講清楚，別把它讀成「已經修好了」。**
warden 收掉一個 member 時走的是 **PPID 樹**（`cli/ocwarden/kill.go` 的 `descendantPIDs`，
`ps -eo pid=,ppid=`，接在 `escalateKill` 的殺前快照與 `snapshotMemberPIDs` 的 kill list 兩處；
那裡有一支測試逐字叫 `TestEscalateKill_TreeWalkReapsDetachedOrphans`，註解寫明目的就是收
「DETACHED to a new pgroup (setsid / double-fork orphans killpg can't reach)」）。而
**`setsid` 改的是 session/pgroup，不改 PPID**（實測：`fork()` + `os.setsid()` 之後 `ps` 報的
PPID 仍是父行程）⇒ **兩者正交，detach 對它無效**；連 watchdog 也在同一份 kill list 裡，所以
「終局訊號」那一層對這個死法**同樣**寫不出來。
⇒ **這一包治的是「呼叫者自己死掉、warden 沒介入」那一類**（shell 被關、上游 pipeline 斷、
`kill` 打在呼叫者的 process group 上）。**relocate／refocus／stop 那條路要靠別的辦法**——
正解是**別讓載體待在那棵樹下**（從另一台機器起它）：**站在外面的觀察者，本來就不會跟被觀察者一起死。**

所以現在是兩件事，而**第二件比第一件重要**：

1. **自己 detach**。`run.sh` 一開頭就把自己 re-exec 進**新的 session**（`os.setsid()`；macOS 沒有
   `setsid(1)`，而 `nohup` 只擋 SIGHUP、不改 process group，所以打在呼叫者 group 上的 kill 照樣收得到）。
   python 那一層**不是立刻退場**：它 fork，**原本那顆 pid 留下來等**那個 detached 的孩子並用它的 rc 結束——
   所以 `bash run.sh; echo $?` 這個前景合約**一個字都沒變**，只有「呼叫者死掉」那條路徑不一樣。
2. **一定寫得出終局訊號**。不管怎麼結束——正常、`exit 2` 拒跑、TERM／HUP／INT、stdout 斷管、
   甚至擋不住的 KILL——`<run dir>/outer.rc` 都會出現，另有一行 `outer.status` 寫**為什麼**。
   因為萬一 detach 因為某個沒人預料到的理由失效，**失敗的樣子必須是「看得見的死」，不是沉默**。

三層刻意重疊：**EXIT trap**（所有正常結束，含每一個 `exit 2` 拒跑）、**signal traps**（bash 看得見的
死法，各自記下自己的 reason，然後**經由 EXIT trap** 離開，所以 teardown 照跑）、**watchdog**
（一個被記下 pid 的小孩，發現載體消失而且沒有 rc 檔，就補寫 `137 / vanished`——這層專治 SIGKILL）。

⚠️ **誠實的界線**（別把綠讀得比它大）：
- **trap 不是即時的**：bash 只在**它正在等的那個前景指令**回來之後才跑 handler，所以 TERM 打在
  setup 或 actor 中間，訊號檔會在**那個指令結束時**才出現。保證的是「**一定會出現、rc 與 reason 正確**」，
  不是「立刻出現」。即時的那一格是 SIGKILL，由 watchdog 回答。
- **detach 擋的是「打在呼叫者 group 上」的 kill**，不是「指名這一輪自己」的 kill：載體與 watchdog 若
  被同一發打掉，就沒有人還活著能寫。**最重要的實例是 warden 收 member**——它走 PPID 樹、專門收
  setsid 出去的子孫（見上面那段），所以 **relocate 這條路 detach 與終局訊號都無效**。這不是可以靠
  調參數補的，是兩個機制正交；要活下來只能不在那棵樹下。
- 這一層**不殺任何東西、不指名任何 session**：唯一的動詞是 `kill -0`（探活、不送訊號），對象是
  這個 shell 自己記下的那一顆 pid。`lib/ownedkill.sh` 的隔離（自己的 socket ＋ 只殺 ledger 上的確切名字、
  fail-closed）**一個字都沒動**。

**守衛在 `tests_guard` 案例 25**（hermetic，不起服務、不花錢）：用一份「只有 carrier 接線 ＋ 一個 sleep」
的 fixture，把它起在**自己的 session** 裡，然後**對它的 process group 送 SIGKILL**——載體必須**跑完**
而且 `outer.rc` 必須存在；**對照組**（`OC_SG_NO_DETACH=1`）在同一發 kill 底下必須重現病徵
（沒跑完、**而且完全沒有訊號檔**），否則 25a 的綠什麼都不證明。另外三格分別是 TERM（143 ＋ reason）、
SIGKILL（137 ＋ `vanished`，watchdog 那一層）、以及一般結束**帶著自己的 rc**（`exit 2` 仍然是 2，
不會被抹平成 0）。最後一格是**接線**：`run.sh` 真的呼叫那三個函式，而且 detach **排在 setup.sh 之前**。

## 載體只准殺自己建立的東西（兩層，`lib/ownedkill.sh`）

`actors/live.sh` 起真 agent，就得收自己起的東西。它原本收在 `cli/ocwarden/tmux.go` 的
`officraft` 上——**那是正式 fleet 的 socket，服役中 agent 的 session 就住在上面**。它只殺確切名字，
所以什麼壞事都沒發生；但「什麼壞事都沒發生」是**載體剛好沒寫錯**的結果，而上一節那個少一個字母的
錯字，它的 trap 就殺掉了剛 spawn 起來的 agent。同一類 bug 發生在 fleet socket 上，不是一次紅的 run，
是**別人**的 agent 被不可逆地殺掉。

於是中間隔兩層，任何一層單獨都離 fleet 只差一個 bug：

1. **物理層**——本輪自己抽一個 warden instance namespace（`OC_NAMESPACE` → `tmuxSocketFor` →
   `officraft-<ns>`，`cli/ocwarden/namespace.go`）。打在 `officraft-<ns>` 的 kill **構造上就到不了**
   `officraft` 上的 session，不是因為小心，是因為那是兩個不同的 socket。`sg_own_socket_assert`
   另外明文拒絕 `officraft` 與**空字串**（空的 namespace 解出來就是 fleet）。
2. **所有權層**——只殺**建立當下寫進 ledger** 的 session 名與 pid。沒有 ledger、或 ledger 是空的，
   就**殺零個**（fail-closed）：漏掉一筆紀錄的後果必須是「漏殺、可回收、看得見」，不能是「殺錯、
   不可逆」。

**守衛在 `tests_guard` 案例 24**（hermetic，不起服務、不起 agent），三顆 mutant 各改一處、各套在一份
**複本**上：拿掉 socket 隔離（socket 解回 `officraft`）、把 pid kill 放寬成 `pgrep -f` 樣式比對、
把 session kill 放寬成「列出來挑像我們的」。三顆都實際套到**真檔**驗過會轉紅並點名
（rc=1，各 4 / 2 / 2 條 FAIL）。
🔴 **還有一格陽性對照，而且它是這一案最重要的一格**：上面每一條斷言都是「**沒有**殺到誰」，而那個綠
用「什麼都不殺」就達得到，跟真的綠一模一樣。所以案例 24 起**真的行程**、記進 ledger、要求它**真的死**，
同時旁邊放一個**位元組相同、沒被記下**的行程，要求它**活著**。

### 🔴 `pkill / killall / pgrep` 的禁令，射程要按**後果**劃，不是按檔名點名

那道文字掃描原本只掃兩個**點名**的檔（`lib/ownedkill.sh`、`actors/live.sh`）。禁令的爆炸半徑跟
「誰被寫進那份清單」毫無關係：**這個資料夾底下每一支 `.sh` 都跑在一台正式 fleet 的 ocserverd／
ocwarden／agent 用同一支 binary、同一組 argv 跑著的機器上。**

**實測（同一顆 mutant、三個落點，都打在改之前那棵樹 `7233fa3` 上）**：
打 `actors/live.sh` ⇒ **rc=1、點名**（PASS=253 FAIL=1）；打 `run.sh` 的 `cleanup()` ⇒ **rc=0 全綠**
（PASS=254 FAIL=0）；打 `lib/carrier.sh` ⇒ **rc=0 全綠**（PASS=254 FAIL=0）。

🔴 **最後那一格不是假設，2026-08-11 它放倒了正式站**：`tests_guard` **案例 25 會真的執行**
`lib/carrier.sh`（那份 fixture source 它），有人為了做陽性對照把 `pkill -f "ocserverd serve"`
寫進去再跑 tests_guard ⇒ **正式站的 ocserverd 真的被殺、中斷 27 秒**。
⇒ **任何危險形狀（`pkill`／`killall`／`rm -rf`／改埠）只准寫在「不會被 harness 執行」的拋棄式副本裡。
動手前先確認你要改的那個檔會不會被某個案例執行。**

現在射程是**`e2e_test/seven_gate/` 底下每一支 `.sh`**——那是一條**查詢**（下一支新增的檔自動進來），
不是一份會過期的點名清單。配套兩件事：**掃到的檔數有下限**（走到零個檔的掃描會安靜地全過），
以及 run.sh／lib/carrier.sh／actors/live.sh **各被逐字點名**在射程內，讓下一次縮小射程紅出來。
註解裡「這是被禁止的形狀」那種說明文字**必須維持綠**（掃描先剝掉整行註解），這幾個檔的檔頭
本來就在解釋這條禁令——一個對自己的說明文件叫紅的掃描，是一個會被刪掉的掃描。

## 🔴 明確沒做到的界線

- **從來沒有真 agent 走過這條關卡——`actors/live.sh` 已經寫好，但一次都沒被執行過。**
  寫它的人沒有按那顆按鈕（起真 agent 會產生實際花費，那一按是 owner 的）。所以它的**每一段**
  ——onboard、`ocwarden run`、activate 帶 machine_id、等 tmux、逐字追問、寫 `friction.txt`——
  都只**照契約與 `tests/05_*.live-agent.spec.js` 寫過，未經執行驗證**。第一個按下去的人請預期要 debug；
  好消息是每一通呼叫的狀態碼與內容都在 `http.log`，debug 是「讀」不是「猜」。
  ⚠️ 特別點名一個**仍然**沒驗過的假設：`run.sh` 先 activate（無機器）留下的 reconcile 狀態，
  會不會讓 live.sh 第二次 activate（帶 machine_id）的 START 被 backoff 延後。
  ⚠️ 原本列在這裡的第二個假設（tmux socket 名沿用 `cli/ocwarden/tmux.go` 的 `officraft`）**已經不是
  假設，而是被拿掉的東西**：那個名字就是**正式 fleet 的 socket**，服役中的 agent 就住在上面。現在
  live.sh 每一輪自己抽一個 `OC_NAMESPACE`，socket 是 `officraft-<ns>`，而 `lib/ownedkill.sh` 明文拒絕
  `officraft`——見〈載體只准殺自己建立的東西〉。
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
  ⚠️ **⑤改判定那一輪又在隔離站上跑了四次**（stub，`:8791`，沒有起任何 agent、沒有花錢）：
  baseline **九格全綠 rc=0**（⑤的證據逐字是「advanced through its plan: step(s)
  ['走完七步', '回報收尾'] reached done, in plan order (finished_ts […]), and they are the
  plan's first 2」）；`OC_SG_SKIP_STEP=step_done` ⇒ **rc=1、⑤FAIL 且首紅點名⑤**
  （同一輪⑦也紅，但⑤排在前面——**改之前這一輪是⑤PASS、首紅點名⑦**）；
  另外兩輪確認沒有誤傷：`skip=closeout` ⇒ ⑤**仍綠**、首紅點名⑦，
  `skip=reply_card` ⇒ ⑤**仍綠**、首紅點名⑥。
- 這支不在 `run_all.sh` 裡、也不在 `bin/ci.sh` 裡。CI 守的是**判定邏輯**與載體的幾條靜態不變式
  （`tests_guard` 案例 21：21a–21e 判定與 friction 措辭、21b-i ⑤紅⑦綠造得出來**且那份 fixture 可達**、
  21b-ii 多開票時警語會出現在最後一行、21b-iii replan ＋ 並行亂序必須是綠的、
  21b-iv fixture 自己帶著第三方票／owner 的卡／對 owner 講的那則帶 peer nonce 的訊息
  （這三列不在，③⑧⑥ 三格的判定放鬆都是靜默的——實測過）、21f 沒有裸 curl／狀態碼有被抓、
  21g live actor 的花錢開關是嚴格 include flag；案例 24：隔離與所有權那兩層；
  案例 26：交給 warden 的環境裡沒有②⑧⑨的答案），
  **不是任何一次真的 run**。
