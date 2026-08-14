# frontend/ — React SPA

進入 `frontend/` 時 nested-load。repo-wide 憲章見 root `CLAUDE.md`;本檔記 FE 專屬。棧:React18 + Vite5 + TS5。

## 本檔只留索引 + 每次 FE 改動都適用的兩條(T-9b5d)

原本這一份是 90k 字元的單體,**只要碰 `frontend/` 任何一個檔就整份載入**。現在按主題拆成 7 份
**path-scoped 規則檔**,放在 `frontend/.claude/rules/`,各自帶 `paths:` frontmatter,**只有在你
真的動到那一塊的檔案時才載入**。

🔴 **`paths:` 少寫一行的後果不對稱,改這幾份檔時要記得**:
- **整個 `paths:` 忘了寫** ⇒ 那份規則變成**無條件全域載入**(實測),context 又漲回來。
- **glob 寫窄了 / 寫錯 base** ⇒ 那份規則對真正需要它的人**完全隱形,而且不會有任何東西發出
  警告**。所以判不準的時候**一律往寬的寫**。
- **glob 的 base 是「rules 檔所在目錄」,不是 repo 根**(實測:放在 `frontend/.claude/rules/`
  的檔,`src/api/**` 會命中、`frontend/src/api/**` 不會;放在 repo 根 `.claude/rules/` 的檔則相反)。
  現行七份檔**兩種寫法都列上去**當保險,加新 glob 時照做。

| 規則檔 | 收了什麼 | 大致觸發面 |
|---|---|---|
| `data-layer.md` | seam 分層、API 錯誤 envelope、SSE delta / reconcile(narrowToHeld、owner-unread 述詞、dtoParity)、`/api/settings` 共享快取 | `src/api/**`、`src/hooks/**`、`deltaSink`/`ownerUnread`/`sharedSnapshot` |
| `presence-and-badges.md` | presence→視覺的唯一映射、unread 計數 badge 的三個顏色槽、自報值 vs 設定值、監控機器表的時效 | `LifecycleDot`/`PresenceBadge`/`MemberCard`/`MonitorPage`/兩個詳情面板、`office.css`/`chrome.css`/`monitor.css` |
| `chat-and-reply-cards.md` | 定期訊息 custom 頻率、聊天未讀跳轉、多行 composer、回覆卡(採用寫入回應 + 按 id 保留)、請示↔任務跳轉 | `Chat*`/`Reply*`/`ScheduledMessagesCard`/`useChat*`/`useReplyCard*`/`composerKeys` |
| `tasks-and-outsource.md` | 任務頁的狀態篩選與錨點補抓、任務卡、外包面板列形、設定›任務手冊 | `Task*`/`Outsource*`/`WorkerDetailPanel`/`useTasks`/`useOutsourceWorkers` |
| `overlays-and-modals.md` | 全幅閱覽 overlay(三種來源、縮放=真 layout、雙指手勢)、Esc 分層、DocCard、差異呈現 | `MarkdownPreviewOverlay`/`escapeLayers`/`DocCard`/`DiffView`/各 Modal·Popover |
| `css-layout-traps.md` | 長 token 溢出、CJK nowrap、浮層不可用 `vw` 夾、用了 class 就要自己 import CSS、lazy fetch 的 effect deps、兩個面板的動作列 | 任何 `.css`、`src/components/**`、`visual-guards/**` |
| `theming-and-i18n.md` | 首設密碼 / 伺服器設定、i18n 可覆寫片段與 `compose.ts`、主題包匯入/匯出、用詞清單 866 列、pre-paint 三道守衛 | `src/i18n/**`、`src/paint/**`、`paint-guards/**`、`scripts/**`、`ThemeSettings*`、`FirstRunPage`/`LoginPage`/`ProfileDropdown` |

**當時的量測證據(請求數/位元組表、毫秒數、mutant kill 表)已搬回產生它的票**——T-8115 /
T-a3e4 / T-3f31 / T-f014 / T-7e68 / T-043e / T-e2e9。規則檔在對應位置標 📎 指回去,要重驗數字
讀那張票,不要把表搬回規則檔。

## verify(root §13)

純 FE UI 改動:headless build → `preview:4173` → Playwright,**開 PR 讓雲端跑,雲端那一輪全綠
才可以合併**、**不上 prod 驗**。
⚠️ **這句原本寫的是「CI 綠即 land」**,與 root `CLAUDE.md` §13 記載的 owner 2026-08-11 裁定
(卡 `rc-c16ac4679fab`:**合併判準改看雲端那一輪**)相牴觸——「CI 綠」會被讀成「本機
`bin/ci.sh` 綠就可以 land」,正是根檔花了幾段在更正的讀法。**依 owner 2026-08-14 於卡
`rc-baef8db570a2` 的裁定(選①:以 8/11 那條為準)改成上面這句。** 本機自己先跑動到的那部分
仍然要做,那是你開 PR 前確認自己那塊沒壞的動作,不是 land 判準。「不上 prod 驗」不在該次裁定
範圍內,原樣保留。

公開 URL https://officraft.hardcoretech.link/。`Monitor.tsx` 的 mock 部分無 telemetry backend
(純前端 mock)。
