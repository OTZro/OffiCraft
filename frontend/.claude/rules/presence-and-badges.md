---
paths:
  - "src/components/LifecycleDot.tsx"
  - "frontend/src/components/LifecycleDot.tsx"
  - "src/components/PresenceBadge.tsx"
  - "frontend/src/components/PresenceBadge.tsx"
  - "src/components/MemberCard*"
  - "frontend/src/components/MemberCard*"
  - "src/components/MemberActionButtons*"
  - "frontend/src/components/MemberActionButtons*"
  - "src/components/*DetailPanel*"
  - "frontend/src/components/*DetailPanel*"
  - "src/components/MonitorPage*"
  - "frontend/src/components/MonitorPage*"
  - "src/components/ModelEffortEditor*"
  - "frontend/src/components/ModelEffortEditor*"
  - "src/components/OfficeSidebarTabs*"
  - "frontend/src/components/OfficeSidebarTabs*"
  - "src/components/badgeRing*"
  - "frontend/src/components/badgeRing*"
  - "src/components/office.css"
  - "frontend/src/components/office.css"
  - "src/components/chrome.css"
  - "frontend/src/components/chrome.css"
  - "src/components/monitor.css"
  - "frontend/src/components/monitor.css"
  - "src/api/mappers*"
  - "frontend/src/api/mappers*"
  - "src/lib/runtime.ts"
  - "frontend/src/lib/runtime.ts"
  - "scripts/check-token-roles.mjs"
  - "frontend/scripts/check-token-roles.mjs"
  - "visual-guards/badge-ring-token.ct.spec.tsx"
  - "frontend/visual-guards/badge-ring-token.ct.spec.tsx"
---

# presence 點、未讀 badge、自報值 vs 設定值、機器表時效

> 本檔由 `frontend/CLAUDE.md` 拆出(T-9b5d)。`paths:` 的 glob **相對 `frontend/`**(rules 檔所在目錄),另外並列一組 `frontend/` 開頭的同義 glob 當保險。
> 標 📎 的段落表示「當時的量測證據已搬回該票」,本檔只留規則。

## presence
三畫面(roster MemberCard / MonitorPage / MemberDetailPanel)顯示走**同一個共用 `PresenceBadge`**(5 態:offline / waking / online / stopping / stopped),display 一律傳 `hub.is_online`(realtime 活線)。DB `member.online` 欄是 vestigial(唯一 reader = reconcile fallback),別當 display 真相。

**presence→視覺的推導只有一份(T-59d6)**:`LifecycleDot.tsx` 的 `presenceVisual(presence)`
是**唯一**的 5 態→視覺映射,`PresenceBadge`(正職)、**兩個外包面**(rail 的
`OutsourceTaskLine`、`WorkerDetailPanel` header)與 `MemberDetailPanel`
(它不畫點,是餵 `MemberActionButtons status=`,但那也是同一個 lifecycle→visual 映射,
一樣不准自己手寫一份)全部走它 + 同一顆 `LifecycleDot`。
顏色只准來自 `--color-dot-offline/waking/online/stopping/stopped` 五個 token
(**不准在 JS 寫 inline colour literal**——`npm run lint:tokens` 只掃 CSS,一個
`style={{background:"#6b7280"}}` 會整個繞過那道 gate,而且會讓四個非 online 態塌成
同一色,違反「點的顏色是 roster 上唯一的 presence 訊號」)。型別面:worker 的
`OutsourceWorkerView.presence` 是 **`MemberLifecycle` 五態 union**(不是裸 string),
因此打錯字或漏處理新態都是 compile error,不是默默漂移。

**wire 字串的統一只有一個 seam**:`mappers.ts::toPresence`(**不是 worker 專用**——
member 與 outsource worker 共用同一套 presence 詞彙,所以共用同一個 narrower)。
wire 那頭是裸 `string`(spec 已凍結),不認得的字 → `undefined`,再由各 caller 落到
自己誠實的地板:**member** 的 `lifecycle` 不可為空 → 落 `offline`(`status` 同源同落,
兩邊不會各說各話);**worker** 保留 `undefined`(released / 從未派工本身就是資訊)。
兩者最後都由 `presenceVisual` 畫成 offline 點,**永不假綠**。⚠️ 別把這個 narrower 拿掉:
沒被統一過的字會直接掉出 `presenceVisual` 的 no-default switch,渲染成
`lifecycle-dot--undefined`——**沒有顏色、`role="img"` 卻沒有 aria-label**,對讀屏是整個
消失的元素,而且不會有任何其他測試變紅。護欄:`api/mappers.presence.test.tsx`。

## unread 計數 badge(M2-1 紅點升級;與 presence 各自獨立)
roster MemberCard 成員列**右側(flex 尾端)的紅色計數 badge**(>99 顯示 99+、count=0 完全不渲染)= server 算好的 `member.unreadCount`(MemberDTO `unread_count`,chat_read watermark 的反相計數;只算成員→owner 訊息,agent↔agent 不計;舊純紅點 boolean 已整顆換掉)——FE 純 passthrough、**不自己算**。清除即既有已讀 choke:進對話的 `listChat` auto-mark / `markChatRead`;`useMembers` 的 ROSTER_TOPICS 含 `chat` / `chat_read` 讓 badge 即時亮/滅;開著的那個對話卡片以 `selected` 壓掉 badge(對話中新訊息永不累積)。badge 在整列(聊天入口)內,點 badge = 點列 = 進聊天,無獨立 handler。mock 以同一規則 live 計算(`unreadCountOf`)、行為與 http 一致;測試用 `__injectMockChat` 注入 inbound 訊息。

**三個顏色槽,外框自 T-d593 起獨立且無下限。** 這顆紅圓圈在座艙有 **7 個 render site
但只有 3 條 CSS 規則**(`.nav-tab__badge` in chrome.css、`.office__tab-badge` 與
`.member-card__unread` in office.css;⚠️ 側欄那兩個 site 是**同一段 JSX**——
`SidebarTab` 只有一個 `className` 字面、被呼叫兩次)。三條規則吃同一組槽:
底 `--color-danger-badge`、數字 `--color-on-danger`、**外框 `--color-danger-badge-ring`**。
- 外框那一槽的**預設是 alias `var(--color-bg)`**,不是烘死的 `#191c24`。這不是隨手寫的:
  外框本來就是借用頁面底色,而**主題可以改 `--color-bg`**;烘實色會讓「改過 `--color-bg`
  的既有主題」外框停在內建深藍、與它的頁面底分家 = 把舊主題的外觀改掉。alias 也讓
  `gen-theme-tokens.mjs` 把它收進 `THEME_ALIAS_DEFAULT_TOKENS`(匯出不烘值、編輯器補一列
  空值 placeholder 顯「跟隨 <頁面底>」)。
- 🔴 **`outline` 的對比下限已經沒有了**(owner 2026-08-01 `rc-1d57d0adc87d` 選②:
  「外框完全自由,不留下限(主題調到看不見也算你的選擇)」)。`check-token-roles.mjs` 的
  `MIN_PILL_VS_PAGE` 隨這顆退場;**主題把外框設成跟填色同色是被支持的選擇,不是缺陷**,
  別再把那條 checkRatio 加回去。**留下的只有「數字 vs 填色 ≥ 4.5:1」**(owner 沒裁到它)。
  lint 印出的 ring 比值自此**只是資訊、不是保證**。
- **護欄兩層,守的不是同一件事**:`visual-guards/badge-ring-token.ct.spec.tsx`(真 Chromium)
  量 `getComputedStyle().outlineColor` 的**實際顏色** ——jsdom 不算 CSS、解析不出 `var()`,
  這半在那裡做不到;`src/components/badgeRing.test.ts`(vitest)是來源掃描,盯
  「7 個 site 都戴那 3 個 class」＋「3 條規則都吃 ring token」＋標籤/產生器三件套。
  ⚠️ **後者當初存在的理由是「`test:ct` 不在雲端 gate 裡,只放 CT 等於回歸在 GitHub 上是綠的」
  —— 那個前提自 T-0fef 起已經不成立**(CT 現在跑在雲端自己一格,
  `make test-frontend-ct`)。**但那不是拿掉 vitest 那半的理由**:兩層守的本來就
  不是同一件事(真顏色 vs 來源掃描),理由過期不等於守衛過期。

## effort / model:自報值 vs 設定值(兩個來源別混;T-e12c 之後界線更硬)

owner 2026-07-31:「成員面板以及監控台,一定要顯示回報回來的狀態,不能顯示設定值」。

- **自報值(狀態)**:`MonitoringSessionDTO.model` / `.effort` → `session.model` / `session.effort`。
  鏈路 = Claude Code statusLine payload 的 **`model.id`** / **`effort.level`** →
  `ocagent context-report` → `POST /api/monitoring/telemetry` → server 的 telemetry
  entry(key = token sub)→ monitoring session 列。兩者取的都是 **live** 值(跟得上中途
  `/effort` 與換模型),**不是** `OC_EFFORT` / `OC_MODEL` 那個啟動意圖,**而且沒有 fallback**。
  honest-empty `""` → UI 顯示「—」。
  - `model` 取 **`model.id`** 而**不是** `display_name`(狀態列上畫的那個):id 是 boot seed
    已經教成員回報的詞彙,也是**唯一**帶 `[1m]` 1M-context 標記的那個——`display_name` 對
    1M 與標準版都寫「Opus 4.5」,送它等於把兩種 session 併成同一個字串。
  - 🔴 **`model` / `runtime` / `effort` 三者都有持久層(T-7f28 起對稱)**:server 除了寫
    telemetry entry,還會在值改變時落進 roster row 的 `actual_model` / `actual_runtime` /
    `actual_effort`(`stampReportedLaunchFacts`)。telemetry 是 in-memory,只靠它的話
    server 每次 re-exec 就把全 fleet 清空。**所以正職與外包的三欄都是「上一次回報的值」,
    活得比 session 久**,而且**都不退回設定值**——退回設定值會讓「改了還沒生效」與
    「已經生效」長得一模一樣,那正是 T-7f28 要修的東西。
  - **codex runtime 由 sidecar 送**(`cli/ocwarden/codex_session.go`),不是 statusLine ——
    那條 runtime 沒有 Claude Code 的狀態列。
- **`GET /api/monitoring` 的 sessions 現在同時含正職與外包**(T-e12c);外包列靠 **`ow-` id
  前綴**辨識(server 沒有、也不該新增 kind 欄位——凍結 wire)。`MonitorPage` 的外包列因此
  用 `findSessionFor(worker.id, sessions)` 取 model/effort/context/cost/**machine/account**,
  **join 不到就一律誠實留白**,絕不退回 `GET /api/outsource-workers` 的設定值。⚠️ machine/account
  那兩欄 owner 2026-07-31 在卡上**明知代價仍選擇這樣**(`rc-4a83a5723896` ①):worker 剛被派出去、
  還沒連上的那段,機器欄就是**空白**——那不是 bug,**不要「修」成退回 worker DTO**,那份 machine
  是 in-memory 的 dispatch target(意圖),不是觀測到的落點。member 那條 lane 同時用
  `ow-` 前綴排除,否則同一個 session 會畫兩列。
- **設定值(啟動意圖)**:roster 的 `member.model` / `member.effort`、外包 DTO 的
  `worker.model` / `worker.effort`。它只活在**兩個地方**:model/effort **編輯器**(seed 與
  存回都是它),以及描述「這張任務要用什麼開外包」的 TaskCard chip 與任務手冊預設值。
  🔴 ⚠️ **這兩個欄位名今天已經不存在**:`configuredModel` / `configuredEffort` 在
  `frontend/src` 全樹**零命中**(實查 2026-08-12,T-0b4f 的獨立審查抓到)。**它要防的那件事
  仍然成立、只是家換了**——設定值住在各自的 設定／更改 對話框(見下一條),不再是共用面板上
  的 prop。原句留在下面是為了保住那個理由,讀的時候別去找那兩個欄位:
  ~~**`AgentDetailPanel` 的 `configuredModel`/`configuredEffort` 是 required、且刻意不對
  readout 做 fallback**~~:readout 是遙測(或 `""`),讓它當退路 = 一次儲存就把自報值寫回
  owner 的設定,未回報時甚至寫進空值而被 closed vocabulary 422。
- **兩個詳情面板資訊卡的 模型/投入度 都是自報值,且都唯讀**:成員走
  `actualModel`/`actualEffort`(awake 才顯示,T-927a),外包走 `session?.model`/`.effort`
  (`findSessionFor(worker.id, sessions)`,與監控台同一條 join,T-7526 之後 OfficePage 也
  在開外包面板時才拉 monitoring)。設定值只出現在各自的 設定／更改 對話框,那裡 seed 自
  member/worker DTO、存回也是它。⚠️ ~~兩個面板現在都**不傳** `onSaveModelEffort`,所以
  `AgentDetailPanel` 裡那顆 in-place 編輯器沒有 production caller~~ —— **這句已不成立**
  (T-0b4f 獨立審查抓到,2026-08-12):**那顆編輯器連同 `onSaveModelEffort` 這個 prop 本身,
  已於 T-7f28 整個移除** ⇒ 不是「沒有 caller」,是「不存在」,你**沒辦法**靠傳那個 prop 把它
  復活。要重做就是新寫一顆,而動手前先想清楚 T-7526 拆掉它的理由(同一個畫面出現兩個改同一
  設定的地方)。
- **缺值守衛(T-e12c)**:「還沒回報任何東西」與「正在回報別的東西、卻獨缺 effort」以前
  長得一模一樣(都是空白),故障因此偽裝成設計躺了很久。`isReportingTelemetry(session)`
  (online ∧ 至少有一個純遙測值:context% / cost / account)為真而 effort 為空時,
  `EffortBadge` 改渲染 `mon-stale` 那顆「這個空白有原因」的 chip(既有視覺語彙,不是警告色
  ——沒有東西壞掉,只是欠一個值);什麼都沒回報則維持乾淨空白。

## 監控 › 機器表:硬體與 Runtime 的時效(T-90be ⑤ + T-b36a)
`MonitorPage` 機器表有兩組會過期的 telemetry 欄,**兩組都必須連時效一起顯示**,理由是
telemetry 只在成員被解僱時清、**斷線不清**,所以資料會比回報它的機器活得久。
- **新鮮度的裁決在 server**(`hardware_stale` / `runtime_capabilities_stale` 兩個 wire
  bool)。FE **不准**拿 `hardwareTs` / `runtimeCapabilitiesTs` 跟自己的時鐘比去重推那個
  90s 窗——門檻只有一個家(server 的 `telemetryFreshSecs`),第二份必然會跟第一份各說各話。
  戳記欄留在 view model 是給人看的時間點,不是給 UI 算 verdict 的輸入。
- **CPU / RAM / 電源**:過期時 server 已把數值收回,所以格子落回 dash——但那跟「這台從來
  沒回報過硬體」是**同一個 dash**。`hardwareStale === true` 時三格各掛一枚 `mon-stale`
  標記(`data-testid="mon-hardware-stale"`)講清楚 dash 的原因。判斷式只准是 `=== true`:
  `false` 是活樣本裡誠實的缺值、`null` 是從沒量過,兩者都不准被標成過期。
- **Claude / Codex 兩欄各印版本(T-674d,取代舊的單一 Runtimes ✓/✗ 欄)**:共用
  `RuntimeVersionCell`,兩欄讀的是**同一份** `runtimeCapabilities`,**沒有新採集、沒有
  合成版本號**。舊的 ✗ 不准被「空格子」吃掉——空格子讀起來是「不知道」,那是另一個
  (而且錯的)主張;所以 `installed:false` → 「未安裝」、`loggedIn:false` → 版本號旁掛
  「未登入」chip(`mon-bad`)。四個誠實輸出:從未回報 → dash(title 從未探測)/
  `installed:false` → 未安裝 / 有版本 → 原樣印 / 已安裝但版本探測沒回話 → 「已安裝」。
  **Claude 多一條 registry fallback**:沒有 capability entry 時落回 `MachineView.claudeVersion`
  (registry 自 T-97ee/T-7c5b 就有的欄),讓舊 warden 的 Claude 欄語意不變;**Codex 沒有
  對應的 registry 欄,唯一來源就是 capability map**,缺就是誠實的 dash,不准借 claude 的值。
  時效紀律照舊:capability 來源的值在 `stale !== false` 時掛 `mon-stale`、**值不收回**
  ——「codex 三小時前沒登入」是 worker 卡在 `machine_unavailable` 唯一的解釋;registry
  fallback 不是 telemetry,不掛時效標記。
- **機器 + 狀態是同一欄(T-674d)**:兩欄拆開時,名字格窄到每一列的 machine-id chip 都
  被擠到第二行。合併後 name / id chip / online badge 同一個 `<td>`;`.mon-machine-name`
  只在 **desktop(min-width:721px)** `flex-wrap: nowrap`——≤720px 的卡片模式**不能**
  nowrap,那個模式刻意拿掉了 `.mon-table-wrap` 的 `overflow-x: auto`(見長 token 那節),
  沒有捲軸吸收的長 machine id 會把整頁推歪。
- 護欄:`MonitorPage.hardware-freshness.test.tsx`(過期標記 / 從未回報不標 / 真 0 與真
  false 仍正確顯示)、`MonitorPage.runtime-capabilities.test.tsx`(兩欄版本 / 未安裝 /
  未登入 / 過期 / registry fallback)、`MonitorPage.machine-id.test.tsx`(合併欄的結構
  不變量)。
