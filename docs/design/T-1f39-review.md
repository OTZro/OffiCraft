# T-1f39 獨立審查（review only — 我沒有改任何一行碼）

審查人：獨立審查者（未參與本批實作）
日期：2026-07-31
範圍：`t-1f39-doc-history-ux` 分支上**全部未提交**的改動（`git status --short` ＋ `git diff` ＋ untracked）
權威規格：`docs/design/T-1f39-document-history-ux.md`（全 650 行已讀，含每一條裁定與 mutant A–K）

**驗證動作（實跑，非閱讀推論）**

- `go test ./server/ocserverd -timeout 900s` → `ok ocserverd 91.9s`（全綠）
- `npx vitest run`（27 檔 / 391 條，涵蓋 lib、DiffView、Modal、Entry.actor、SettingsPage×3、mock.document-history、compose、styleOwnership）→ **391 passed**
- `npx tsc --noEmit` → 無輸出（乾淨）
- 設計檔 §C 的四條 grep（G1／G2／G3a／G3b）→ **四條皆回 0 行**（G3b 因本批已改 `spec/mcp-catalog.json` 而關閉）
- 重放 mutant **B-H3** 與 **A-D1**（見下 §2），皆以 `cp` 還原並 `shasum -a 256 -c` 驗證逐位元組相同

---

## 0. 結論

**沒有 blocking 發現。** 每一條 owner 裁定我都找到了實作它的碼，而且判定它真的做到了（逐條見 §1）。
下面全部是 should-fix 與 note。

---

## 1. 裁定符合度（逐條）

| 裁定 | 判定 | 依據 |
| --- | --- | --- |
| 手冊 SOP／學習經驗兩條獨立序列 | ✅ | `api_taskmanuals.go:22-51`（兩個 kind 常數＋兩支單欄 snapshot）、`api_document_history.go:130-152`（`taskManualHistoryStreams` 只在該欄真的變了才產生 stream）、`dal.go:465-523`（`SaveWithDocumentHistories` 逐 stream 各自 insert＋各自修剪到 3） |
| purpose／識別鍵／display_name／assignee 不版控 | ✅ | `api_taskmanuals.go:370-372` 只記 `sopBefore/learningsBefore`；只改 purpose ⇒ `streams` 為空 ⇒ `SaveWithDocumentHistories` 走「合法但不版控的寫入」路徑 |
| 角色名稱不版控、restore 絕不改名 | ✅ | `api_document_history.go:58-70`（snapshot 不帶 `name`）、`:273-284`（restore 用 `folded.Name`，忽略舊列殘留的 `name`）、`api_roles.go:249-253`（純改名 ⇒ `roleDefHistoryStreams` 回 nil ⇒ 不佔保留名額） |
| 舊 kind `task_manual` → 400 且指名兩個新 kind | ✅ | `api_document_history.go:13-17`（訊息字面同時含兩個新名字）、`:179-185`（在 `default:` **之前**攔截，回 400） |
| 版本入口在編輯列、取代 重置 | ✅ | 五個編輯面各掛一顆 `DocumentHistoryEntry`：`SettingsPage.tsx:1636-1650`、`LessonsCard.tsx:81-93`、`TaskManualsPage.tsx:864-878`（SOP）、`:1468-1479`（手冊學習經驗）；全樹沒有任何一顆殘留的 重置 鈕（`SettingsPage.roles.test.tsx` 的負面斷言看著） |
| 「初始版本」只在有 seed 時、且在清單最後 | ✅ | `DocumentHistoryEntry.tsx:306-342`，`{onReset && …}` 且緊接在 `versions.map(...)` 之後、`</ul>` 之前；`SettingsPage.document-history.test.tsx` 有「必須是最後一個 `.doc-hist__item`」的位置斷言（設計檔 §F-G1 補的那條） |
| 清單是 PICKER，不預覽內容 | ✅ | `DocumentHistoryEntry.tsx:252-282` 只有時間／修改者／徽章／「檢視這個版本」；「無法還原」原因仍留在列上（`:272-282`），符合「點進去之前就該知道」 |
| 修改者顯示 名字（代號）／owner 顯示 CEO 標籤 | ✅ | `DocumentHistoryEntry.tsx:114-119`（`actorId === OWNER_ACTOR_ID ? t.user : msg.docHistoryActor(...)`）、`lib/actorLabel.ts:25-28`（查不到回 `""`）、`compose.ts:213-216`（空名字輸出裸代號，**不產生空括號**） |
| 任務定義頁三塊各自編輯、同時只開一塊 | ✅ | `TaskManualsPage.tsx:674`（`otherEditing`）、`:542-543`（disabled ＋ `title={manualEditBusyHint}`，按鈕留在原位）、`:637-652`（`commit(block)` 只帶當下那一塊的 key，被放棄的草稿進不了 payload）、`:861-879`（版本入口只掛在③） |
| diff 方向：該版本 `-`／目前 `+`，且比的是伺服器內容 | ✅ | `DocumentHistoryModal.tsx:244-247`（`before = version.content`、`after = currentContent`）、`:228-230` 明寫「與伺服器上目前存著的內容比較；編輯框裡尚未儲存的修改不算在內」；四個 host 傳的都是伺服器狀態（`TaskManualsPage.tsx:869` 用 `manual.sopMd` 而非 `sopDraft`） |
| 破壞性動作一律過確認框 | ✅ | 還原：`DocumentHistoryModal.tsx:293-296` 只 `setConfirming(true)`，`commitRestore` 僅由 `ConfirmModal.onConfirm`（`:317`）呼叫。重置：`DocumentHistoryEntry.tsx:317-327` → `:370`。清單 overlay 的點擊關閉被 `!resetting` 擋住（`:172-174`）。**沒有找到任何繞過路徑。** |

---

## 2. 測試有效性

### 重放的 mutant（兩支，皆已還原並驗雜湊）

**B-H3 — 舊 kind 的 400 訊息不再指名兩個新 kind**
`server/ocserverd/api_document_history.go:15-16`，`legacyTaskManualKindMsg` 改成
`"this document history kind is no longer supported"`。
`go clean -testcache && go test . -run TestDocumentHistoryRefusesTheRetiredTaskManualKind` → **紅 4 條**
（`api_document_history_roundtrip_test.go:512`，list／restore × `task_manual_sop`／`task_manual_learnings`）。
與設計檔記載一致（行號從 `:428` 位移到 `:512`，是後續 §G 加測試造成的，不是失準）。
還原：`cp` 自 scratchpad 備份，`shasum -a 256` = `7d7dbfba…de89` 相符。

**A-D1 — migration 的 WHERE 放寬成前綴**
`migrations/00044_drop_legacy_task_manual_history.sql`，`= 'task_manual'` → `LIKE 'task_manual%'`。
→ **紅**：`migrate_test.go:1103` 逐列點名 4 個被誤刪的鄰居（`task_manual_sop/tm-alpha`、`/tm-beta`、
`task_manual_learnings/tm-alpha`、`/tm-beta`）＋`:1108`「holds 3 rows … want the 7 non-legacy rows」
＋`:1123` 回滾列數。fixture 確實放了「會被錯誤刪掉的東西」，不是自證。
還原：`shasum` = `f5fa5f69…16d5c` 相符（與設計檔記載一致）。

**兩支都紅在該紅的地方、訊息點名到列。設計檔對這兩支的記載屬實。**

### 後端測試品質（實讀 `api_document_history_roundtrip_test.go`）

強。具體強在哪：

- `assertUnmoved` 同時釘「幾版」與「最新那版的 id」（`:~300`），不是只數數量——只數數量的話「新增一版同時被 trim 掉一版」看不出來。
- `TestTaskManualStreamsAreTrimmedToThreeIndependently` 寫**超過**保留窗（5 次 SOP）才驗學習經驗沒動；三次以內證明不了共用序列。
- `TestRoleNameIsNotVersionedAndRestoreLeavesItAlone` 用**自訂角色**（種子角色名字鎖死，改名根本試不了），且改名**連改三次**＝翻過整個保留窗。
- 舊 kind 的拒絕測試尾端有**陽性對照**（同一本手冊上兩個新 kind 照常 list／restore），排除「整條路都壞了所以也回 400」的假綠。

### should-fix — 多欄位顯示邏輯只被一個**已死的 kind** 覆蓋

`frontend/src/components/DocumentHistoryModal.test.tsx:173`、`:193`、`:222` 三處用 `kind: "task_manual"` 當 fixture。
拆包後 `DOC_FIELD_ORDER`（`lib/docHistoryFields.ts:19-28`）裡**每一個活著的 kind 都只有一個欄位**，
所以 `comparedFieldNames` 的「取兩側欄位聯集」（`docHistoryFields.ts:76-90`）、多欄位渲染、欄位排序
這幾件事，**唯一的覆蓋來源是一個伺服器已回 400、資料列已被 00044 刪光的 kind**。

具體後果：任何人做「把 `task_manual` 從 `DocumentKind` 拿掉」這個自然的清理，會同時撞掉這三條測試，
而且拿不到任何等價替代——因為現實中已經沒有多欄位的 kind 了。等於這段邏輯**實質上沒有活的護欄**。
（設計檔 §F-E 記錄了改用 `task_manual` 重寫這兩條的理由，但沒有指出這個副作用。）

### note — `compose.test.ts` 的 overridable 清單漏了四個新代碼

`frontend/src/i18n/compose.test.ts:235-241` 的「必須可被覆寫」斷言清單加進了
`settings.historyVersionLabelLead/Tail` 與四個 `diff.*`，但**沒有**加
`settings.historyActorLead/Tail`、`settings.manualEditSectionLead/Tail`。
四者都在兩份 generated 白名單裡，所以不是 i18n 破洞，只是那條守衛的覆蓋面缺一角。

---

## 3. Wire freeze

**「沒有路由改動所以不需要重新產生」這句話：查證屬實。**

- `spec/openapi.json` **未改**，且確實不需要改：兩條 document-history 路由的 `kind` 是
  `{"type": "string"}` 無 enum（`spec/openapi.json:7155`、`:7167`），退場一個 kind 在 openapi 那側
  本來就無物可改。路由集合、operationId、參數集合全部不變。
- `server/ocserverd/ocapi_gen.go` 與 `frontend/src/api/schema.ts` **皆未出現在 diff 中**——與
  openapi.json 未改一致，三者仍然對得上。
- `conformance/routes_manifest.json` **未改**：路由集合沒變（`:851`、`:853`、`:860` 三處仍是同樣的
  path 與 `mcp_tool`）。與 `test_auth_matrix.py` 同步——後者只改了**探測用的 path 字面值**
  （`:721`，`task_manual` → `task_manual_sop`），route key 不變，所以兩份仍然對齊。
  該探測期望全部 404：`task_manual_sop` 過得了 `documentHistoryAllowed`，然後在找不到版本 id 時回 404，
  行為正確；若沿用舊值會變成 400，探測會紅——**這一行是必須改的，改對了**。
- `spec/mcp-catalog.json` 只改了 `list_document_history` 的 `description`（`:2889`）。查證其安全性：
  - `spec_catalog_conformance_test.go` 是**參數名稱的集合比對**，不看敘述（檔頭 `:33-36` 明寫），不會誤紅也不會漏；
  - `catalogHashOf`（`assets.go:338-340`）雜湊的是 **RouteSpec 集合**，不是敘述 ⇒ 敘述改動**不會**動到
    `catalog_hash`，因此不會觸發不必要的 agent 重啟訊號。
  - 這一步同時把設計檔 §C 的 G3b 從「恰好一行」關到 **0 行**。

**結論：wire freeze 這一關乾淨。**

---

## 4. i18n

**齊全。**（以 esbuild 載入兩份字典做集合比對，並照 `frontend/scripts/gen-message-keys.mjs` 的推導規則
在 scratchpad 重算 key 集，未執行產生器、未動任何 tracked 檔。）

- `en` / `zh` 各 **886** 個 leaf，only-in-en = `[]`、only-in-zh = `[]`、型別不一致 = `[]`。
  38 個新 key（11 個 `diff.*`、24 個 `settings.history*`、3 個 `settings.manualEdit*`）**兩語皆有**。
- 五支新 compose 函式（`compose.ts:199/204/212/221/223`）**每一支都有 zh＋en 兩例測試**
  （`compose.test.ts:65-70` / `:113-118`），含 `docHistoryActor` 空名字回裸代號那一例。
- 兩份 generated 檔**皆為 871 個 string leaf，與字典推導結果集合相等且排序正確**
  （`messageKeys.generated.ts`、`server/ocserverd/message_keys_gen.go`）。886−871 = 既有的 function leaf
  與 `themeIdentity` 子樹，正是產生器刻意排除的。
- 新 `.tsx` 內**沒有**硬寫的可見字串。`DiffView.tsx:24-28` 的 `+`／`-` 字形帶 `aria-label`（`:127`），
  `:108` 的 `@@ -a,b +c,d @@` 是 `aria-hidden` 的機器記號，兩者都不是應翻譯的散文。

---

## 5. 會咬到 owner 的東西

### should-fix ①：還原成功卻回報失敗，重按會還原第二次

`frontend/src/hooks/useDocumentHistory.ts:74-80`

```ts
const restore = useCallback(async (id: number) => {
  await api.restoreDocumentHistory(kind, key, id);
  setVersions(await api.listDocumentHistory(kind, key));   // ← 同一個 promise，未 catch
}, [kind, key]);
```

還原的 POST 成功之後，**緊接著那一發 GET 若失敗，整個 `restore` 就 reject**。這個 reject 經
`DocumentHistoryEntry.tsx:386-389` 傳進 `DocumentHistoryModal.tsx:114-122` 的 catch，畫面顯示
「還原失敗」並**讓兩層對話框都留著**（`:118-120` 的註解說明那是刻意的）。此時：

- 文件**其實已經被覆寫**；
- `onRestored?.()` 沒跑（`Entry:388` 在 await 之後），編輯面不會刷新、不會退出編輯 ⇒ 畫面停在舊內容；
- owner 看到「舊內容 ＋ 還原失敗」，**理性反應就是再按一次確認還原**——第二次還原會再吃掉一個保留名額，
  可能把一版真的版本擠出三格窗外。

觸發時機不是理論值：還原剛剛 fan 了 SSE，那一刻正是後續 GET 最可能失敗的時候。
這正是設計檔 §E 前言說本批要防的「內容說謊」那一類。

**修法**：把刷新從還原的 promise 裡拆出去單獨 `.catch`——還原成功就是成功。

**歸屬**：`restore` 這個形狀是 T-7d33 就有的（本批 diff 未改這五行）。但本批把還原**改成只有這一個入口**，
並新增了 `onRestored` 這條依賴它的鏈，所以後果是這批放大的。

### should-fix ②：歷史清單載入失敗時，**重置沒有任何入口**

`frontend/src/components/DocumentHistoryEntry.tsx:206-209`

`error` 分支只渲染 `historyError` 一行；「初始版本」那一列（`:306`）在 `else` 分支裡。
改動前 重置 是編輯列上一顆**獨立**按鈕，不依賴 `GET /api/document-history/...`；
裁定把它併進版本清單之後，**只要那一發 GET 失敗，全域情境與種子角色就完全沒有辦法回到預設值**。
「初始版本是重置的唯一入口」這件事讓重置變成一個不相干請求的人質。

seed 列不需要任何伺服器資料，應該在 error 分支裡照樣渲染。

### should-fix ③：三處 production 註解直接寫反了 Q2 裁定

- `frontend/src/types.ts:546-547`：「the server still lists and restores existing rows, so the kind stays on the wire」
  ——伺服器兩條路徑都回 400（`api_document_history.go:180-184`），列也被 00044 刪光了。
- `frontend/src/api/mock.ts:2469-2470`：「existing rows stay readable」——同上。
- `frontend/src/api/docCap.ts:63-65`：「The legacy bundle restores all four fields at once」——已無此路徑。

下一個讀到這三段的人會拿它當授權，繼續餵養這個已退場的 kind。註解在這裡不是裝飾，是後續維護的依據。

**連帶的具體事實**：`frontend/src/api/mock.ts:3327-3335`／`:3337-3351` **不拒絕** `task_manual`——
mock 在真伺服器回 400 的地方回 200／404。mock 還完整保留了該 kind 的 snapshot（`:861-870`）與
restore-apply（`:963-980`）。設計檔 H2 那支 mutant 打的正是「錯誤被吞掉」的形狀，而那個形狀
**在 mock 這一側今天就是現況**，只被 Go 那側的測試釘住。

### should-fix ④：`DocumentHistoryCard` 刪除後留下的死 CSS

`frontend/src/components/settings.css`：`.doc-hist`（`:1363`，卡片外框）、
`.doc-hist__fields`（`:1459`）、`.doc-hist__field`（`:1466`）、`.doc-hist__field-name`（`:1469`）、
`.doc-hist__field-value`（`:1479`）——逐 class grep 過 `src` 與 `visual-guards`，
**五個都沒有任何 `.tsx` 產生**（`.doc-hist__fields` 那一組是裁定 I 拿掉的逐欄預覽的遺骸）。
同段的區塊註解（`:1359-1362`）也還在描述「a card that sits under every editable long-form doc」，
正是 owner 移除掉的那個形狀。

### should-fix ⑤：使用者手冊仍在描述已刪除的卡

`docs/guide/settings.md:57`：「每個編輯器底下有一張「版本紀錄」卡」。
沒有這張卡了——入口是編輯工具列裡的一顆按鈕，而且**只在編輯模式下看得到**。
同一節也從頭到尾沒提「重置」已經變成清單最後的「初始版本」——
一個照著手冊找 重置 的讀者，會找不到、也得不到任何指示。這一節的其餘段落（modal／diff）是準確的。

### note ⑥：SSE 觸發的重載失敗是靜默的

`useDocumentHistory.ts:106-110` 只 `console.warn`，不設 `error`；清單會繼續顯示寫入前的版本而不吭聲。
與初次載入（`:98-100` 會顯示）不一致。清單只是挑版本用，嚴重度低，但確實是吞掉的失敗。

### note ⑦：每次打開會閃一格「還沒有保留任何版本」

`useDocumentHistory.ts:67` 的 `loading` 初值是 `enabled`（關著時為 false），要到 effect（`:82-85`）
才翻成 true——而 effect 在 paint 之後。所以點開的第一幀是空清單／只有 seed 列的畫面。

### note ⑧：死 export

`useDocumentHistory` 仍回傳 `refetch`（`:50`、`:70-72`、`:119`），唯一的消費者只解構
`versions/loading/error/restore`（`DocumentHistoryEntry.tsx:102-106`）。全樹沒有第二處 mount 這個 hook。

### note ⑨：`settings.reset` 這個 key 已無 production 消費者

`zh.ts:1168` / `en.ts:1094`，只剩 `SettingsPage.roles.test.tsx:387` 的**負面**斷言在讀它。
留著是對的（那條斷言需要它），但值得補一行註解說明，否則讀起來像孤兒 key。

### note ⑩：`task_manual_sop` 顯示 `learnings` 這個洞只是**不可達**，不是**關上**

`lib/docHistoryFields.ts:40-57`：`IGNORED_FIELDS` 只擋 `role_definition.name`，其餘未知欄位一律照樣附加。
今天不可達（伺服器與 mock 的 snapshot 都只寫一個欄位），設計檔 §J 也刻意不釘「未知欄位附加」這條
（它是寫明的前向相容行為）。記在這裡是因為 §E4 的 F1 那個 GREEN 是靠**欄位表本身**的測試補上的，
不是靠顯示層——這個區別下一個人要知道。

### note ⑪：`DocDetail` 在沒有 `history` 時會靜默吞掉 `onReset`

`SettingsPage.tsx:1636` 用 `history &&` 包住整顆 entry，而 `onReset` 只從那裡接出去（`:1647`）。
未來有人只傳 `onReset` 不傳 `history`，會得到一個沒有重置、也不報錯的文件。兩個現有 call site 都兩者皆傳。

### note ⑫：`frontend/.shot.mjs`

審查開始時 `git status` 列出這支 untracked 檔（實機截圖用的臨時腳本，設計檔 §K-D 提到的那一套）。
我複查時它已不存在。若它在提交前又出現，那是不該進版控的臨時產物。

---

## 6. 我**沒有**審查的部分（明列）

- **未跑 `bin/ci.sh`**（依交辦指示禁止），因此沒有驗證 lint、format、`gen:api` drift check、
  以及 CI 才跑的任何 gate。上面的判斷來自 `go test ./server/ocserverd`、範圍內 `vitest`、`tsc --noEmit` 三者。
- **未跑 conformance 套件**（`conformance/test_auth_matrix.py` 等）——需要起真的 server。
  我只做了靜態一致性核對（route key、路徑字面值、期望碼的推理）。
- **未跑 Playwright／visual-guards**：`visual-guards/diff-view.ct.spec.tsx`、
  `visual-guards/doc-history-modal.ct.spec.tsx` 與兩支 story 我**只讀了檔名與掛載形狀，沒有執行**，
  也沒有看任何截圖。設計檔 §K-D 說版面只靠實機截圖把關——**這一層我完全沒有覆蓋**。
- **`lib/lineDiff.ts` 的 LCS 演算法本身我沒有逐行驗證正確性**，只確認 `lineDiff.test.ts` 通過、
  且設計檔 §H 補上的合併門檻邊界測試存在。收合演算法在極端輸入（大量交錯小改動）下的行為未探。
- **未重放設計檔的其餘 mutant**（A-D2/D3、B-H1/H2、C2-A1~A3、E1~E5、F1~F6/G1、G-R1~R3、I-P1、K1~K6）。
  依交辦只 spot-check 兩支：B-H3 與 A-D1，兩支都屬實。其餘各節的紅燈記載我是**採信**設計檔的，
  沒有獨立坐實。
- **未查證 migration 00044 在真實 production 資料上的效果**（設計檔說是 9 列）。我只驗了
  `migrate_test.go` 的合成 fixture。
- **`frontend/src/components/settings.css` 我只掃了 `.doc-hist*` 家族**，其餘 62 行 diff 未逐條審。
- **`server/CLAUDE.md`、`frontend/CLAUDE.md` 的散文我讀了 diff 但未去核對它引用的每一個事實**
  （例如 `seedRoleKeys()` 那一段的正確性）。
- **`SettingsPage.tsx` 與 `TaskManualsPage.tsx` 的非本任務區塊**（這兩支各數千行）我沒有通讀，
  只讀了 diff 命中的區段與其上下文。
