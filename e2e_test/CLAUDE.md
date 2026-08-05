# e2e_test/ — Playwright 端到端

進入 `e2e_test/` 時 nested-load。repo-wide 憲章見 root `CLAUDE.md`;本檔記 e2e 專屬。

## target:Go(唯一)
Go(ocserverd)是唯一 target(py leg 已隨 Python backend 退役;歷史回滾 = git tag `py-final`):`bash run_all.sh`(`OC_E2E_TARGET=go` 仍可顯式指定;其他值 fail loud)。:8791 / repo-root oc.toml / fresh-DB 生命週期 / EXACT-PID teardown;流程 = stage SPA→webdist + docs→docsdist + seeds→seedsdist + binaries/MCP catalog→bindist → go build 進 `.state/` → goose migrate → serve。四種 embed asset 一律先 stage；漏 seedsdist 會讓真 agent boot persona 缺檔，漏 bindist 會讓 MCP `tools/list` 回 catalog unavailable 且 warden binary route 503。

## 誰會自動跑這套(T-ff8a)
`bin/ci.sh` **不**跑 `run_all.sh`(只跑 `tests_guard/run.sh` 那套 hermetic 守衛)。自動關卡在
**`.github/workflows/ci.yml` 的 `macos-e2e` job**:macOS runner、`pull_request` 觸發,
`OC_E2E_EXCLUDE_REAL_FLEET=1` ⇒ 跑瀏覽器情境、排掉 `05_machine_onboarding_spawn`
(要 `claude` 在 PATH、spawn 真 warden、燒真 API 額度)。
兩個前提由 repo 裡的具名腳本負責,**不靠人記得**:
- `gen-oc-toml.sh` 生 gitignored 的 `oc.toml`(port 8791 ＋ repo-local DSN,也就是 setup.sh
  兩道 prod guard 要的東西);**已存在就拒絕覆蓋**——那可能是開發者指向正式 DB 的真設定。
- `assert-specs-ran.sh` 在 job 綠之後再問一次「到底有沒有跑」:沒有 `N passed` 統計、低於下限、
  或那支 real-fleet spec 竟然跑了,都判紅。**rc == 0 只回答「沒有東西失敗」,不回答「有東西跑過」。**
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
