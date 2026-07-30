# 開發指南

一般使用者請看 [docs/guide/](../guide/)（產品說明，也是控制台主導覽「使用說明」分頁的來源）；這裡是給改 code 的人。repo-wide 憲章與 land 紀律的權威在根目錄 [CLAUDE.md](../../CLAUDE.md)，各域（`server/` `cli/` `frontend/` `conformance/` `e2e_test/`）另有自己的 `CLAUDE.md`，本文不重複，只給地圖與跑法。

## 技術棧

| 面向 | 技術 |
| --- | --- |
| server | Go（`ocserverd`：REST + SSE + MCP + reconcile，goose migration，SPA 以 go:embed 內嵌，SQLite） |
| frontend | React / TypeScript（Vite） |
| cli | Go —— `ocwarden`（執行手）、`ocagent`（agent runtime） |

（歷史：原 Python backend（FastAPI + alembic）已退役移除；**永久回滾錨點 = git tag `py-final`**。）

## Repo 結構

```
server/       Go server daemon：ocserverd（route 表 / handlers / SSE hub / goose migrations）
frontend/     React/TS web UI（Vite）；build 產物由 go:embed 進 ocserverd
cli/          Go 模組：ocwarden（push-executor）、ocagent（agent runtime）
spec/         凍結的 wire 契約（openapi.json / mcp-catalog.json）——動 wire 先改 spec
seeds/        語言中立 seed .md 資產（boot context；ocserverd runtime 直讀）
conformance/  語言無關黑箱套件：server wire 行為的可執行定義（HTTP-only 回歸權威）
e2e_test/     Playwright 端到端（隔離 port，絕不碰 prod）
bin/          維運指令：ocserver / ocwarden / serve / migrate / build / ci.sh …
docs/         設計文件
oc.toml.example  server 設定範本
```

runtime 落點統一在 `~/.officraft/`：`server/`（canonical 安裝）、`warden/`（token / 設定）、`agents/`（各 agent 工作區）。

## 怎麼跑

```bash
# Go server
cd server/ocserverd && go build && go test ./...
bash bin/build           # 部署 binary：npm webdist + go build → .deploy/ocserverd（SPA 內嵌）

# frontend
cd frontend && npm install && npm run dev

# conformance（語言無關黑箱：wire 行為回歸權威；隔離 :8795）
conformance/run.sh --target go

# e2e（Playwright，隔離 :8791，絕不碰 prod）
cd e2e_test && bash run_all.sh
```

## CI

```bash
bin/ci.sh          # 綠的判準是「rc == 0 且整份輸出的最後一行精確為 [ci] all green」，兩個條件都要
```

判準為什麼是 **AND**（T-d3e3）：兩半各自都不夠。

- **寬鬆 grep 完全無效**：step 0 的 `e2e_test/tests_guard` 第一步就印自己的 `all green`，所以**任何**中途爆掉的 log 裡都已經含有那個子字串。
- **只看最後一行會被 dispatch 的 lane 偽造**：ci.sh 不是這份 log 的唯一寫入者。一個被 dispatch 的 lane 只要 `echo "[ci] all green"; exit 1`，ci.sh 的 `set -e` 就在那裡中止，偽造的權威剛好留在最後一行——這個假綠是真的被做出來過的。
- **只看 rc 也不夠**：這個 repo 有前例，`bin/common.sh` 的 `set -e` 打敗了 `run_all.sh` 刻意的 rc 捕獲，讓失敗訊號靜默消失。舊文所以寫「判準是 marker、**不是** exit 0」——那句話講的是 **rc 不足以單獨判綠**，不是「rc 不該被檢查」。要求兩者同時成立比任一半都嚴格，與原意相容。

`bin/tests/ci-success-marker.sh` 是這條規則的可執行形式：它同時掃描 **ci.sh 以及每一個被 dispatch 的 lane 腳本**，要求除了 ci.sh 之外沒有任何 shell 腳本「有能力」印出這個權威字串。

CI 跑在本地、`bin/ci.sh` 是 land 權威，從第一個非零步驟就 fail-fast；push 前請自己跑到綠。gate 內容：go gate / 黑箱 lint / gitleaks / FE typecheck+drift。

（舊文寫「不付 GitHub Actions」——repo 轉 PUBLIC 後那個理由已不成立，公開 repo 用標準 runner 是免費的。真正的理由是這份 gate 裡有大量 host-shaped 與「重生後逐位元組比對」的步驟，我們不想把那些的權威搬到雲上。）

**PR 上的雲端 check（`.github/workflows/ci.yml`）**：`pull_request` 觸發，跑「雲端跑得動的全部」——**單元測試**（`e2e_test` 的 hermetic isolation-guard、Go 各模組的格式／靜態／編譯／測試、FE typecheck/vitest）、**hygiene**（tracked-file path denylist）、**一致性檢查**（gen-ocapi / FE schema.ts / 主題色票 / 訊息鍵 / 字型白名單的 regenerate-and-diff 漂移閘 + 兩個 token lint）、**黑箱行為**（完整 conformance 套件，起真 ocserverd 綁隔離 port）。它是在乾淨 Linux 機器上的 cross-check，**不是 land 權威**——`bin/ci.sh` 才是。

⚠️ 子集的定義只有一份、寫在 `bin/ci-cloud.sh`（repo 內的 bash）；workflow YAML 只負責裝釘好版本的 toolchain 然後呼叫它，**裡面沒有、也不准有第二份模組清單或閘門清單**——要加請加進 `bin/ci-cloud.sh`。

⚠️ **workflow 裡的 go / node 版本釘選是承重的、不是衛生習慣**：一致性檢查斷言的是「重生的位元組與 committed 完全相同」，runner 的 toolchain 一旦浮動超前開發機，這一類就會在「碼完全沒問題」的情況下變紅。

**留在本機的**：`bin/tests/run.sh`（Linux 上目前有 16 條 assertion 失敗；根因是 BSD/GNU `mktemp -t` 語意、SIGPIPE 與 macOS 形狀的 `install.sh` fixture，尚未移植）、Playwright CT（真瀏覽器版面守衛；macOS↔Linux 的字型與光柵化差異會讓紅燈的意思從「版面壞了」變成「runner 字型不同」）、gitleaks（內容級機密掃描）、`e2e_test` 的真機端到端測試（要真的 fleet host）。tracked-file path denylist 與 `e2e_test` 的 hermetic isolation-guard 已在雲端流程執行。整條雲端流程不用任何 secret，所以 fork PR 也能跑完整。

**Go 測試一律 `-count=1`（T-bedc）**：CI step 1e 是 `go test -count=1 ./...`，`-count=1` 是「不吃 go 的測試結果快取」，**不可省**。省掉的後果是實測過的——log 裡出現 `ok  ocwarden  (cached)`，那格綠燈認證的是一次**根本沒執行**的跑。兩個獨立理由：(a) 快取 key 只涵蓋 package 的**輸入**，不涵蓋測試真正碰的世界（port、時鐘、launchd、host fleet、staged embed assets 的**效果**），所以今天會紅的 package 照樣報 ok；(b) 它**結構性地藏 flake**——一個 suite 只在「第一個改到它輸入的 commit」上跑過一次，間歇性失敗於是被攤平到近乎零觀測機率，`[ci] all green` 變成在講快取而不是在講碼。可執行形式是 `bin/tests/go-test-nocache-guard.sh`（CI step 0b 派出）：它以**命令位置解析**（不是 substring grep——那會匹配到 ci.sh 與守衛自己的說明文字）掃全 repo 的 shell 腳本，任何 `go test` 呼叫點少了 `-count=1` 就紅。注意 `go build` / `go vet` 的快取**刻意不管**：那是對編譯本身做 content-addressed，命中等價於未命中；只有**測試結果**快取會宣稱「行為被觀察過」而其實沒有。

改 Go 後只需 fresh build 驗證；`bin/ocagent`、`bin/ocwarden`、`bin/ocserverd` 若出現都是 gitignored build artifact，**永不 commit**。CI 一律編譯 source；只有本機恰有 prebuilt 時才做 parity dryrun。部署 binary 由 `bin/release` / GitHub Release fresh build 產出。

## wire freeze

wire（HTTP OpenAPI 面、MCP tool 面）已凍結：**動 wire 一律 spec 先行**——先改 `spec/openapi.json` / `spec/mcp-catalog.json`（+ owner 過目），再 `bash bin/gen-ocapi` 重生、動碼。CI 的 wire-freeze gate 擋任何未過 spec 的漂移；行為面由 `conformance/run.sh --target go` 收官。完整紀律見 [CLAUDE.md](../../CLAUDE.md) §13。

## 發版指令(T-588c)

發版只有兩條指令,`bin/release` 全包,**不再有「印一行 `gh release create` 給人貼」的半套形式**(舊的 `bash bin/release <tag>` 已移除,打它會拿到非零退出 + 正確替代指令):

```
bin/release publish --beta <tag> --target <sha> [--sign] [--dry-run]
bin/release promote <tag>                       [--dry-run]
```

- `publish` 從 `<sha>` 切一個**丟棄式 detached staging worktree**(不是「當前 tree 乾淨就好」——bytes 來自你指名的那個 commit),在裡面 build、打包、**上傳前先驗 artifact**(tarball member list、三顆 binary 的 arm64 mach-o、從 `go version -m` 讀 ocserverd 真正被 link 進去的 `appVersion`/`buildSHA`、`shasum -c`),然後**一次** `gh release create --prerelease --target <sha>` 帶齊三個 asset(所以不存在「release 已建立但 asset 只上傳一半」的視窗)。
- `promote` 把**既有且已驗過**的 prerelease 翻成正式版,**不重 build**——大家測的 bytes 就是出貨的 bytes。翻完回讀,若 asset 集合在翻的過程中變了(有人偷偷重傳)那是**失敗**,不是警告。
- `--dry-run`:build + 驗完就停,印出它本來會跑的上傳指令,**什麼都不上傳**。彩排用這個。

### 回讀坐實(publish 的第 7、8 步)

發完不靠人記得手動確認。`publish` 會**問 GitHub 它到底存了什麼**並逐項要求:每個預期 asset 都在且 `state=uploaded`、size 非零、沒有多餘 asset、`targetCommitish == <sha>`、`isDraft == false`、`isPrerelease == true`;然後 poll 線上站台的 `GET /api/version` 直到 `git_sha` 對得上 `<sha>`(prefix,至少 7 字元)、且 `GET /api/health` 答 ok。**任何一項不合就 exit 6 並指名是哪一項**:`[release] VERIFY-FAILED [asset-uploaded]: …`。這條的可執行守衛是 `bin/tests/release-guard.sh`(由 `bin/tests/run.sh` 派出,即 CI step 0b;PATH-shim 假 `gh` + 假 `curl`,完全不碰網路、不建任何 release、不連任何站台)。

**回讀的 payload 形狀是「量過的」,不是猜的**(2026-07-26,對 `pkyosx/OffiCraft` 的 `v0.5.38` 實測 `gh release view --json assets,isDraft,isPrerelease,targetCommitish`):

```
isDraft False | isPrerelease True | targetCommitish fb89a69aad8c
{'name': 'checksums.txt',                         'state': 'uploaded', 'size': 181}
{'name': 'install.sh',                            'state': 'uploaded', 'size': 70730}
{'name': 'officraft-v0.5.38-darwin-arm64.tar.gz', 'state': 'uploaded', 'size': 16842394}
```

也就是 asset 子欄位 `name`/`state`/`size` 確實存在、`state` 就是字串 `"uploaded"`、`size` 是非零整數——正好是回讀真正依賴的三件事。**為什麼要特別量**:同一張票裡,`verify_artifacts` 的架構檢查就是因為「猜 `file -b` 的輸出順序」而寫成永不可能命中的 pattern(`file` 實際輸出 `Mach-O 64-bit executable arm64`,架構在最後),導致每次 publish 都死在 `[artifact-arch]`。假設外部工具的輸出格式是同一類 bug,所以這裡改成量。要改形狀前**先重量一次**。

**第 8 步的語意:publish 不觸發升級,它只「觀察」升級發生**。站台是靠 owner 帳號上的 **auto_update** 自己去撿新 release 的,而 **prerelease 也算**:2026-07-26 實測,`v0.5.38`(`isPrerelease=true`)建立後約 **2–3 分鐘**站台自動升上去、`/api/version` 的 git_sha 回讀坐實。預設等待預算 60 × 5s = 5 分鐘,約為實測延遲的兩倍。所以「發完等站台升上來」是**正確的流程期待,不是設計缺陷**;但若哪天 auto_update 被關掉,這一步就會**合理地**失敗,而失敗訊息會明講「只有這一項沒達成、asset 與 release 本身都對」,以免下一個人跑去查 artifact。

## 發佈簽章(穩定 codesign 身分,T-33d5;**T-588c 起預設不跑**)

⚠️ **簽章預設關閉**(owner 裁示 T-588c:「可以保留但是我們預設都不跑」/「以後我們想要拿回來繼續 sign 我們再說吧」)。**理由是作業面的**:簽章**卡測試又卡發佈**,先跳過讓開發順利,機制保留不刪。`bin/build` / `bin/build-bindist` / `bin/ci.sh` / `bin/autodeploy` / `bin/release publish` 的**預設路徑一律不碰 keychain**——`bin/codesign-artifact` 在 `security find-identity` **之前**就 no-op 返回(閘門必須在探測之上:重點不是「沒簽」,而是「不需要那把共用的 login keychain」,所以兩份 CI 才能同時跑)。要簽只有兩種明確方式:`OC_CODESIGN_ENABLE=1`,或 `bin/release publish --sign`(委派 `bin/build-release`,`OC_CODESIGN_REQUIRE=1`,憑證不在就硬擋、沒有 adhoc 降級)。

⚠️ **兩句話都禁止寫,兩個方向都是過度宣稱**:不要寫「不簽章的代價是 macOS 會重問 TCC 權限」,也不要寫「已證實 self-signed 對 TCC 完全無用」。現況是:self-signed codesign 對 macOS TCC 授權是否有效,**目前只是高度懷疑無效、沒有 100% 結論**(owner 2026-07-26;依據:**在有簽章的那段期間,他仍碰過不只一次授權詢問**——那是有觀察支持的懷疑,不是證明)。前一句曾被當成既定代價寫進碼裡,而它從來沒被坐實。真的需要答案就在真機上量,拿證據回來。

**唯一權威表述在 `bin/codesign-artifact` 檔頭**;`bin/release`、`bin/build`、`bin/build-bindist`、`bin/build-release`、`bin/setup-codesign-cert`、`cli/CLAUDE.md`、`cli/ocwarden/selfupdate.go` 一律指向它、不自行改寫。同一條規則不留兩份不同表述——本 repo 已有同型事故(兩份複本漂開,註解變得比證據自信)。

以下描述的是**保留但預設不執行**的機制。T-33d5 當初的主張是:macOS TCC 以 code-signing 身分記權限、Go 預設 adhoc 簽章每 build cdhash 都變,所以以長效 self-signed 憑證(CN 預設 `OffiCraft Code Signing`)簽署可讓 fleet self-update 後仍延續授權——**該主張未被坐實**(見上)。機制本身:

- `bin/build-bindist` → 簽 bindist 的 `ocwarden`(`com.officraft.ocwarden`)與 `ocagent`(`com.officraft.ocagent`)——即 ocserverd 內嵌、經 `/api/{warden,agent}/binary` 發給 fleet self-update 的 binary。
- `bin/build` → 簽 `.deploy/ocserverd`(`com.officraft.ocserverd`)——autodeploy / `bin/release` 打包(GitHub Release 出貨)的 artifact。
- 簽署 seam 是 `bin/codesign-artifact`:**T-588c 起預設連 keychain 都不看**(`OC_CODESIGN_ENABLE=1` / `OC_CODESIGN_REQUIRE=1` 才啟動);被要求簽時才是「keychain 有憑證才簽,沒有就警告照舊」,簽完 `codesign --verify --strict`,失敗保留原 binary。`OC_CODESIGN_DISABLE=1` 是硬否決,勝過 ENABLE/REQUIRE。
- **發版走 `bin/build-release`,憑證不在就硬擋(T-da4b,owner 裁示 `rc-e43a3aae0912`)**:上面兩個簽署點**都不是發版專用**——`bin/build` 也跑在 autodeploy(prod 主機)、`bin/ocserver install`、和任何 dev Mac 手動 build;`bin/build-bindist` 更是**每次 `bin/ci.sh` 都跑**。所以 `OC_CODESIGN_REQUIRE=1` 沒有「發佈路徑」可以掛——掛進 `bin/build`/`bin/build-bindist` = 連沒憑證的 dev Mac 和 CI 一起擋死(**owner 明確沒選這個**)。`bin/build-release` 就是補上的那個點:**發版者跑它、不跑 `bin/build`**,它 export `OC_CODESIGN_REQUIRE=1` 再委派,三個簽署呼叫(bindist ocwarden/ocagent + `.deploy/ocserverd`)全部繼承 → 憑證不在 = `FAIL-IDENTITY-MISSING` exit 4,**沒有 artifact 可出貨**。`bin/build` 本身**未改、維持預設 off**。
  - 發版:見上面的〈發版指令〉。`bin/build-release` **只在 `bin/release publish --sign` 時被進入**(T-588c 起預設不簽),它仍是「憑證不在就硬擋」的那個點。
  - **代價(owner 已知並接受)**:憑證過期／keychain 沒解鎖／發佈機重灌 → **加了 `--sign` 的發版會停,而且不會自己好**,要人去跑 `bash bin/setup-codesign-cert` 重佈。這正是「要求簽章」的意思:要嘛拿到簽好的,要嘛什麼都拿不到。⚠️ 不要把這條代價改寫成「否則 fleet 會靜默掉 TCC 授權」——那個因果**未被坐實**(見本節開頭)。
  - ⚠️ **`--sign` 未對真 keychain 驗過(已知邊界,owner 接受)**:T-588c 為止沒有任何一顆簽章版 release 被真的做出來過。測試證明的是**路由**(`--sign` 確實走到 `bin/build-release` 且 `OC_CODESIGN_REQUIRE=1` 傳達到 `codesign-artifact`,`security`/`codesign` 都是 PATH-shim),**不是**真簽章可用。綠燈不等於這條路走通過。
  - **T-588c 已補上原本的缺口**:當年這裡是**選用的入口點,不是出貨閘**——`gh release create` 上傳什麼檔案完全不看,所以有人跑 `bin/build` 再拿那顆去發 release 擋不住。現在 `bin/release publish` 全包整條 arc:**上傳前**驗 artifact(arch / tarball member / ocserverd 的 version+commit stamp / checksums)、**上傳後**回讀 GitHub 實際存了什麼 + 站台是否真的跑在那個 commit。(簽章身分本身仍不是出貨閘,因為預設不簽。)
  - **未決**:`bin/autodeploy`(prod 主機)也會經 `bin/build` → `build-bindist` 產**發給 fleet 的** ocwarden/ocagent,但它**維持不擋**(擋它 = prod deploy 停擺,那不是 owner 被問到的那題)。要不要一併 require,需要 owner 再裁一次。
- **憑證檢查是「先收集再比對」,不可改回 pipeline(T-da4b)**:`security find-identity | grep -Fq` 會在第一行命中就關 pipe,`security` 吃 SIGPIPE(141),`set -o pipefail` 讓整條 pipeline 取 141 → **憑證明明在,卻被判成不在,靜默降級出 adhoc**。務必把輸出整個收進變數再比對。`bin/setup-codesign-cert` 的同款檢查也已一併改掉。
- **哨兵與陽性訊號(T-da4b)**:憑證確認存在時會印 `identity CONFIRMED present in keychain` —— 只有失敗才叫的哨兵,沒叫時分不出「正常」還是「哨兵自己壞了」,所以好路徑也要留證。**檢查本身壞掉**(`security` 讀不到、輸出沒有 `N valid identities found` trailer)→ `FAIL-CHECK-BROKEN` **exit 3 硬擋**,絕不當成「憑證不在」降級。**`OC_CODESIGN_REQUIRE=1`** → 憑證不在時 `FAIL-IDENTITY-MISSING` **exit 4 硬擋**;預設 off,所以沒憑證的 dev/CI 機照常 build。
- **同類掃描:`pipefail` + 提前關 pipe 的消費者(T-da4b,已掃全 repo,結論=不動)**:全 repo 24 個帶 `set -o pipefail` 的 shell script 都掃過 `| grep -q` / `| head` / `| sed -n Np` 這種「讀夠就關 pipe」的組合。**除了已修的 `codesign-artifact` / `setup-codesign-cert`,其餘一律不改**,因為判準不是「構造在不在」,而是**誤判往哪個方向倒**:
  - **rc 根本沒被消費** → `| head -1 || true`(`bin/ocserver:103`)。`|| true` 吃掉 141,無害。(原本這裡還列了 `conformance/run.sh` 的 listener 查詢;**T-a3ba 已把它整段刪掉**——那行 `lsof … | head -1 || true` 換成「候選數 ≠ 1 就 FATAL」的 while-read 迴圈,`head -1` 的靜默取第一個本來就是這張票要殺的東西,不再是本節的案例。)
  - **倒向假紅(誤報失敗)** → `e2e_test/a1_zombie_e2e.sh:506/510/512`(`sed`/`head`/`tail | grep -qE`)、`e2e_test/tests_guard/run.sh`(`run_snippet` 的 `grep -q` 斷言)、`e2e_test/setup.sh:121`(`printf '%s' "$RESP" | py -c …`,T-a3ba 後 writer 是 builtin `printf` 而非 `curl`,窗更小)、`e2e_test/setup.sh:186`(登入取 token)。SIGPIPE → 141 → 測試紅／腳本中止。**吵,但不會騙人**,而且這些檔案 `bin/ci.sh` 只跑 tests_guard,其餘沒有活體證據可驗 —— 改了也證不了,是純 churn。
    > 行號是寫作當下的快照,會漂;以構造(不是行號)為準。上一版這兩行的行號在寫下時就已經對不上了。
  - **倒向「看得見的 skip」** → `bin/tests/run.sh:427`(`openssl version | grep -q '^OpenSSL 3'` 守著一個 red control)。**這是唯一另一個「檢查可能不執行」的方向**,但 else 分支會**印出 `skip — red control needs OpenSSL 3.x`**,不是靜默消失;且 `openssl version` 一口氣寫 ~25 bytes 就退出,窗開不起來。
  - **`echo "$VAR" | grep -q` 一律低risk**:writer 是 builtin、字串遠小於 64KB pipe buffer,grep 收到 EOF 前 write 早已完成。
  - **通則(給後面的人):`pipefail` + 早關 pipe 只有在「141 會把某個 `if`/`if !` 翻成『壞事不存在』並讓流程靜默往下走」時才是地雷。** `codesign-artifact` 之所以是地雷,是因為它是唯一一個誤判會**靜默翻轉「出不出貨」**的點。倒向紅、倒向可見 skip、rc 被 `|| true` 吃掉的,都不是同一種病。
- **gitignored 本地 prebuilt(`bin/ocwarden` 等)不需要簽章**，維持素 `go build` 產物——CI parity dryrun 與任何 dev 機 build 都不需要 keychain,repo/CI 完全不受影響;簽章只活在發佈 artifact 上。
- self-update 側**不驗簽**(self-signed 憑證在未信任機器上 verify 必非零,硬擋會 brick fleet),只在 swap 後 log 新 binary 的簽章身分(`cli/ocwarden/selfupdate.go` 的 signature observability)。

發佈機一次性佈署:`bash bin/setup-codesign-cert`(冪等;產憑證 → 匯入 login keychain → sudo 信任 codeSign policy → 預授權 codesign 用 key → smoke test)。注意:換新憑證 = 新 TCC 身分,fleet 會再被問一輪權限。簽章腳本的 hermetic 測試在 `bin/tests/run.sh`(CI step 0b)。

## 安裝器內部

`bin/ocserver install` 的逐步細節（canonical layout、oc.toml 渲染、launchd plists、health check、首設啟用碼 banner、env override `OC_SERVER_ROOT` / `OC_SERVE_PORT` / `OC_CLOUDFLARED_CONFIG`）都寫在 `bin/ocserver` 檔頭註解與各 step 註解裡，那份是權威；tunnel 一律不代 provision，config + tunnel id + cloudflared binary 三者齊全才會掛 tunnel job。
