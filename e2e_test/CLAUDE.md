# e2e_test/ — Playwright 端到端

進入 `e2e_test/` 時 nested-load。repo-wide 憲章見 root `CLAUDE.md`;本檔記 e2e 專屬。

## target:Go(唯一)
Go(ocserverd)是唯一 target(py leg 已隨 Python backend 退役;那段歷史不在本 repo,沒有回滾錨點):`bash run_all.sh`(`OC_E2E_TARGET=go` 仍可顯式指定;其他值 fail loud)。:8791 / repo-root oc.toml / fresh-DB 生命週期 / EXACT-PID teardown;流程 = stage SPA→webdist + docs→docsdist + seeds→seedsdist + binaries/MCP catalog→bindist → go build 進 `.state/` → goose migrate → serve。四種 embed asset 一律先 stage；漏 seedsdist 會讓真 agent boot persona 缺檔，漏 bindist 會讓 MCP `tools/list` 回 catalog unavailable 且 warden binary route 503。

## 誰會自動跑這套(T-ff8a)
`bin/ci.sh` **不跑 playwright spec**(只跑 `tests_guard/run.sh` 那套 hermetic 守衛)。⚠️ **但別把這句
讀成「本機那一輪跟 `run_all.sh` 無關」**:`tests_guard` 會把 `run_all.sh` **複製進拋棄式樹實際執行**
(走 record-only seam),並靜態釘住它的 **wiring shape**——`RC=$?` 必須緊接在 `playwright test` 那行、
`[run_all] specs exit=$RC` 又緊接其後,以及 EXIT trap 必須經 `oc_e2e_teardown_on_exit` 這道閘而不是直接
叫 `teardown.sh`。**實測**:在 `playwright test` 與 `RC=$?` 中間插一行 ⇒ FAIL=2 rc=1;把 trap 改成直接叫
`teardown.sh` ⇒ FAIL=4 rc=1。<br>
⇒ **本機綠證明的是那幾條 wiring,不證明任何一支 spec 跑過**(那一輪一支都沒跑)。**spec 面的驗收是 PR 上
`macos-e2e` 那一輪與它的 log。** 至於 `assert-specs-ran.sh`,本機**完全碰不到**(唯一呼叫者是 `ci.yml`),
改它就真的只有雲端那一輪能驗。自動關卡在
**`.github/workflows/ci.yml` 的 `macos-e2e` job**:macOS runner、`pull_request` **與 push-to-`main`**
兩個觸發都跑(T-ab2a 補上後者;`main` 上不 cancel-in-progress,見那個檔的註解),
**那個 job 什麼旗標都不用設**(T-c329):要**活的 agent** 的 spec 是**預設不跑**的,所以雲端不必
「記得排除」——它只是從來沒有要求花錢。🔴 **要跑那一類得自己帶 `OC_E2E_LIVE_AGENT=1`,而那會
spawn 真 agent、燒真 API 額度(真的花錢)。** 成員資格由 spec **自己用檔名宣告**
(`*.live-agent.spec.js`),`playwright.config.js` 裡**沒有檔名清單**——清單會讓下一支忘記登記的
spec 預設偷偷跑、偷偷花錢。判定是嚴格 `=== '1'`,所以 `true`/`yes` 這種打錯字一律落到
「沒跑、沒花錢」。
⚠️ **所以這一類 e2e 沒有任何自動守衛在跑它——而那是 owner 看過選項後刻意否決的決定,不是待補的缺口**
(卡 `rc-d51e755d3207`)。要「補上」它就是在推翻一個已經做過的裁定,不是補一個洞。**這句話刻意不提
這一類今天有幾支 spec**——成員資格由上面那個檔名後綴自己宣告,所以它不隨數量變。
⚠️ **舊做法反過來、而且是這張票要修的 bug**:以前是 `OC_E2E_EXCLUDE_REAL_FLEET=1` 這個**排除**旗標、
且**只設在 `ci.yml`** ⇒ 雲端有防護、每台筆電都沒有,本機跑一輪就靜靜 spawn 真 agent 並付錢
(2026-08-05 實際發生過)。**防護只存在於某一條路徑上,等於另一條路徑從來沒有防護。**
兩個前提由 repo 裡的具名腳本負責,**不靠人記得**:
- `gen-oc-toml.sh` 生 gitignored 的 `oc.toml`(port 8791 ＋ repo-local DSN,也就是 setup.sh
  兩道 prod guard 要的東西);**已存在就拒絕覆蓋**——那可能是開發者指向正式 DB 的真設定。
- `assert-specs-ran.sh` 在 job 綠之後再問一次「到底有沒有跑」:沒有 `N passed` 統計、低於下限、
  或**那一類 live-agent spec 在沒被要求的情況下竟然跑了**,都判紅。**rc == 0 只回答「沒有東西失敗」,
  不回答「有東西跑過」。** 它比對的是**檔名後綴**(不是某一支的標題——標題會被改寫,守衛就會盯著一個
  沒人再寫的字串而什麼都不報);而**帶了 `OC_E2E_LIVE_AGENT=1` 的人它會放行**,不然主動選擇花錢的人
  會撞到一道對他報 FATAL、還引用一個他從沒設過的旗標的守衛。
`workers` 已釘死 **1**(見 `playwright.config.js` 註解:整套共用一台 server／一顆 SQLite,
並行 7 → 7 紅、序列 → 4 紅,假紅會讓一個新閘一週內被關掉)。

## 鐵律:絕不碰 prod
e2e 一律跑**隔離 port / 隔離 server**(如 `:8791`),**絕不**碰 prod(officraft live 現跑 `:7755`,`:8766` vibe;`:8770` 是 2026-07-20 退役的舊 prod 埠)。造真實素材(真 `ocwarden run`、真 claude spawn)但全在隔離環境。spec 進 repo = 永久回歸守衛。

## 造 online agent 的機制知識(給需要真 online member 的 e2e)
- **online = 純由 SSE 連線判定**(`GET /api/events`),**無 TTL / heartbeat、綁連線生命週期**——只要 listen 掛著就恆 online、穩定。
- **建議做法(繞開真 claude 掛 listen 的 flaky)**:tmux session 內手動持長駐 `ocagent listen &`(持 member token)→ `is_online=True`。
- `observed_host` 靠 POST presence 設;member token 靠 `POST /api/mint`。

## precondition 誠實(root §13 verify 誠實線)
有些鏈需**真 online agent** 才觸發(STOP robust-stop 需 online 的 session_id;relocate 需 `observed_host≠desired_host`,靠 online 回報)。這類 runtime 實測下來 **flaky + 燒 token**——若機制已由單元測試 + 決策探針驗證過,runtime 是**額外封印非必須**:隔離難穩定就**誠實標 `precondition-blocked`**,別硬燒 token。
- ⚠️ **relocate 無乾淨 runtime 可觀測信號**:reconcile decision 的 phase 翻在 reconcile-store 內部、不落 member row(member DTO 的 `phase` 是 presence phase,非 reconcile phase);唯一乾淨的完成信號 = warden 執行後 report 的 `last_op*`(command_result projection)。
