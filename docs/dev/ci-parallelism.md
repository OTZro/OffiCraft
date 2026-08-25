# 同時最多能跑幾輪 CI、受什麼限制

**一句話**：一份工作副本一輪，硬性；要多輪就多開副本，**跨副本並跑的天花板未量到**。

本檔只寫**實測到的數字**。沒量到的地方一律寫「未量到」，不推估、不外推。

---

## 硬性上限：每份工作副本 1 輪

`bin/ci.sh` 對它所在的那份工作副本上鎖（`<clone>/.ci-lock`，atomic `mkdir` 仲裁）。
同一份副本起第二輪，會拿到明確拒絕並以**非零 rc** 結束。

**實測**（T-70c9）：在一份副本上讓一個持有者拿著鎖，接著在同一份副本跑 `bash bin/ci.sh`
→ **rc=1**，stderr 第一行逐字：

```
[ci] REFUSED — this working copy is already running CI.
```

**為什麼是硬性的**：ci.sh 全程原地寫這份副本 —— `npm ci` 刪掉重建
`frontend/node_modules`、三支 `build-*dist` staging 到固定路徑、4b1/4b2/4b3 把五個
committed 生成檔就地重生再跟自己的備份逐位元組比對。兩輪交錯時**失效方向不固定**：
可能假紅，也可能**在一棵沒驗過的樹上變綠**，所以直接拒絕。⚠️ 這裡原本寫「後者是偽造 land 權威」——**那個前提在 2026-08-11 已經改掉**（owner 卡 `rc-c16ac4679fab`：合併判準改看雲端那一輪）。**危險本身沒變**：那一行仍是開發者用來判斷「我這塊乾淨了、可以開 PR」的依據。

實作 `bin/lib/ci-lock.sh`，守衛 `bin/tests/ci-lock-guard.sh`，背景見 `docs/dev/README.md`。

---

## 跨副本（多個 clone）：天花板 **未量到**

**量到的**：兩份獨立 clone × 各一輪完整 CI，全程重疊約 7 分鐘，兩邊 rc=0
且末行精確 `[ci] all green`。

⚠️ **這只是一對、一次**。三輪、四輪、五輪**沒有量過**；冷快取競態**沒有設計對照實驗**。
正確讀法是「**天花板未量到**」，不是「沒問題」。**不要把 2 當成上限，也不要把它當成保證。**

已知的共用資源，是天花板真正落在哪裡的候選（**都未量測**）：

| 共用的東西 | 跨副本共用嗎 | 量過嗎 |
|---|---|---|
| Go build / module cache（`GOCACHE`、`GOMODCACHE`） | 是，整台機器一份 | 未量 |
| npm cache | 是 | 未量 |
| Playwright 瀏覽器快取（`PLAYWRIGHT_BROWSERS_PATH`） | 是 | 未量 |
| CPU / 記憶體 / 檔案描述子 | 是 | 未量 |
| `frontend/node_modules`、`*dist` staging、五個生成檔 | **否**，各副本一份 | — |

### 一個 load-bearing 的隱形依賴：Playwright CT 的埠

跨副本並跑之所以成立，靠的是 **Vite 在 `ctPort: 5241` 被佔用時「默默換一個埠繼續跑」**。

**實測**（T-70c9）：把 `127.0.0.1:5241` 用另一個 listener 佔住，再跑
`npx playwright test -c playwright-ct.config.ts <一支 spec>` → **14 passed、rc=0**。

⇒ **誰替 `frontend/playwright-ct.config.ts` 補上 `strictPort: true`，就會把跨副本並跑的
能力關掉。** 那份 config 的註解原本寫的正好相反（自稱是 strictPort equivalent、
「寧可大聲失敗也不要默默換埠」），已在 T-70c9 改成實話。

---

## git worktree：期望行為（**鎖那一格已驗，並跑那一格未驗**）

owner 明確**不做 worktree-safe**，但「不做」不等於「不必知道會發生什麼」。

**已驗**：鎖路徑從 `$ROOT` 推導，所以同一個 clone 的兩個 git worktree 拿到的是
**兩把不同的鎖** ⇒ 這個鎖**不會**擋下它們，兩輪**起得來**。
（實測：主副本與 `git worktree add` 出來的第二棵樹各自 acquire 成功，鎖檔各在自己樹下。）

**期望**：worktree 對這份 gate 而言比較像「另一份副本」而不是「同一份副本」——會撞的那些
路徑（`node_modules`、`*dist` staging、五個就地重生的生成檔）**每棵樹各有一份**；
共用的是 `.git` 的物件庫與 refs，而 CI 對 git 只做讀取（`status` / `rev-parse` / `ls-files`）。

⚠️ **未驗**：**從來沒有真的在兩棵 worktree 上並跑過完整 CI。** 上面那段是從共用資源清單
推出來的期望，不是實測。特別未驗的是 `.git` 上的鎖競爭（index.lock）與 worktree 專屬的
`.git` 檔案佈局。**要靠它之前先量。**

ℹ️ **`bin/release publish` 曾經恰好構成這個未量測情境，現在不再**：T-b65e 讓它在 staging
worktree 裡跑一次完整 `bin/ci.sh`（發版前的行為驗證閘），所以有人在主副本跑 CI 的同時發版就會
撞上這一格。T-7e6c 把那道閘換成**讀該 commit 自己那一輪 GitHub Actions 的判決**（不再本地重
跑），publish 因此不再取任何 CI 鎖、也不再在 worktree 裡跑 CI。這一格於是**回到純理論**——
仍然未量測，只是沒有已知的觸發者了。要靠它之前還是先量。

---

## 反例：`conformance/run.sh` 在**同一份副本**裡 4 輪並跑**不是全綠**

這條推翻了一個先前被當成「已驗證」的說法（「conformance 階段 4 輪並行全綠、各自不同埠、
零降速」）。**在同一份副本裡**不成立。

**實測**（T-70c9，同一份 clone 同時起 4 輪 `bash conformance/run.sh --target go`）：

| 輪 | rc | 結果 |
|---|---|---|
| 1 | 1 | `cp: …/server/ocserverd/seedsdist: Not a directory` |
| 2 | 1 | `cp: …/seedsdist/boot_sequence_codex.md: No such file or directory` |
| 3 | **0** | `1011 passed in 16.37s` → `[conformance] all green` |
| 4 | 1 | `cp: …/server/ocserverd/seedsdist: Not a directory` |

**根因是 check-then-act 競態，不只是「共用固定路徑」**——這個區別很重要，因為它決定了
解法不是加一把鎖：

```
conformance/run.sh:108   CVENV="$HERE/.venv"                       # 四輪共用同一個 venv
conformance/run.sh:320   if ! ls "$REPO_ROOT"/server/ocserverd/seedsdist/*.md >/dev/null 2>&1; then
conformance/run.sh:322     bash "$REPO_ROOT/bin/build-seedsdist"   # ← 先判斷、再動作
conformance/run.sh:324   if ! ls "$REPO_ROOT"/server/ocserverd/docsdist/*.md >/dev/null 2>&1; then
conformance/run.sh:326     bash "$REPO_ROOT/bin/build-docsdist"
```

四輪**同時**判定「seedsdist 不存在」，於是四輪**同時**跑 `build-seedsdist`，後到的撞在
別人的半成品上。**這正好解釋了為什麼是 1 綠 3 紅而不是全紅**：第一個把 staging 跑完的那輪贏，
其餘死在中間狀態。⇒ **要讓它真的能並跑，得改這段邏輯本身（check-then-act 那一段），
不是在外面加一把鎖。**

埠**沒有**撞（每輪自己拿 kernel 配的埠，綠的那輪跑在 `127.0.0.1:59791`）——**撞的是
共用檔案，不是埠**。

⚠️ 兩點誠實邊界：
1. 那個「4 輪全綠」的原始說法**可能是在不同副本上量的**；本檔只推翻「同一份副本」這個讀法，
   **跨副本 4 輪 conformance 未量**。
2. **本票沒有對 `conformance/run.sh` 加鎖**——owner 的裁定範圍是 `bin/ci.sh`。
   這裡只記錄，不修（`bin/tests/ci-lock-guard.sh` 有一條斷言在盯著「這把鎖沒有蔓延到
   conformance 的入口」）。
