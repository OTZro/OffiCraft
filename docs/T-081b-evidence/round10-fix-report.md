# T-081b 第十輪修正報告

基底 HEAD `fc1af1d`　依據:`review-round9.md`(BLOCKER 0 條)+ owner 追加的兩條裁定
證據目錄:`docs/T-081b-evidence/round10-fix-run/`

> 一項做完寫一項,順序即施工順序。

---

## 1. 主題包撐長 `copyTag` → 內建主題的下載檔匯不回來(review round 9 SHOULD-2)

### 定案(以 owner 施工中給的第二版為準)

先做的是「在組名字那一端清理片段」(`sanitizeExportNameFragment`),已完成並取得紅證。
施工中 owner 給了更簡單的裁定 ——「**我覺得檔名不用附註副本**」—— 於是**整條換掉**:
下載內建主題時,名字**只用內建主題自己的名字**,不再附註「(副本)」。
第一版的清理器**已整個移除**(理由見下方「為什麼可以移除」)。

### 改了什麼

| 檔 | 改動 |
|---|---|
| `frontend/src/components/ThemeSettings.tsx:1127` | `exportOfficeBaseTheme("office-base", msg.themeCopyName(t.themeIdentity.office))` → `exportOfficeBaseTheme("office-base", t.themeIdentity.office)` |
| `frontend/src/i18n/compose.ts` | 移除 `themeCopyName`(介面宣告 + 實作)—— 沒有其他呼叫者 |
| `frontend/src/i18n/locales/zh.ts` / `en.ts` | 移除 `themeMarkers.copyTag`(「副本」/「copy」),並改寫該子樹上方那段說明(它原本寫「以及匯出副本檔名裡的『副本』」) |
| `frontend/src/lib/themeBundle.ts` | **移除**第一版加的 `sanitizeExportNameFragment` 與 `MAX_EXPORT_NAME_FRAGMENT_LEN` |
| 產生檔 | 重跑產生器:用詞白名單 **791 → 790** 顆(`messageKeys.generated.ts` + `message_keys_gen.go` 兩端同步),`gen:tokens` 仍 72 顆 |

**零孤兒**:全 repo(`frontend` / `server` / `bin`)grep `themeCopyName` / `copyTag` / `副本`
的結果只剩兩類 —— ①四行**歷史說明註解**(`ThemeSettings.test.tsx`、`themeExport.test.ts`、
`themeBundle.test.ts`,說明這個字樣為何退休);②`themeBundle.test.ts` 裡兩條**否定斷言**
(`settings.themeCopyTag` 與 `themeMarkers.copyTag` 現在都必須被當成未知碼丟進 `skipped`)。
後者是刻意的絆線,不是孤兒;`themeCopyName` 這個函式與其型別宣告已完全不存在。

### 為什麼可以把清理器一併移除(owner 要求先自己確認的那一點)

匯出名字的來源共兩條,逐條查過:

1. **內建列下載** → `t.themeIdentity.office`。`themeIdentity.*` 是產生器**結構性排除**
   的子樹(`gen-message-keys.mjs`),不在白名單裡,寫入時 `validateWording` 就地刪碼、
   讀取時 `loadSettings` 再 prune 一次。⇒ **主題包填不進任何字**。
2. **自訂列下載** → `b.name`,也就是那個主題自己的名字。它**在匯入/存檔時就已經過
   `validateThemeBundle`**:長度 1..80(trim 後,rune 計)、且不得含 Cc/Cf/Co/Cs/Zl/Zp。
   ⇒ 它必然已經滿足匯入端的同一套規則,再清理一次是多餘的。
   (下載的**檔名**由 `bundleFilename()` 用 `bundle.id` 組成,而 id 受
   `^[a-z0-9][a-z0-9-]{1,63}$` 限制,路徑分隔符進不去。)

⇒ 拿掉 `copyTag` 之後,匯出名字裡**不再有任何主題包可填的字串**,清理器沒有守護對象。
本票已經因為「留死機制」被審查抓過一次(round 8 的排除機制),所以一併移除。
**沒有保留任何殘件**。

### 測試

| 檔 | 測試名 | 這條釘什麼 |
|---|---|---|
| `frontend/src/components/ThemeSettings.test.tsx` | `ThemeSettings · export > office 列下載鈕可用,下載一個非保留 id 的 office 包(可再匯入)`(**擴充既有那條**,未新開) | 真的按下載鈕、真的讀回 Blob:`payload.name` **必須等於** `zh.themeIdentity.office`,不含任何附註 |
| `frontend/src/lib/themeExport.test.ts` | `exportOfficeBaseTheme > downloads the built-in under a name no theme bundle can reach`(取代原本的 `downloads the built-in as a COPY…`) | 把 **`MESSAGE_KEYS` 全部 790 顆**都覆寫成 200 字哨兵,名字仍逐字等於內建名,且匯得回來 |
| `frontend/src/components/ThemeSettings.test.tsx` | 既有的 `keeps the built-in row's own name when a pack forges everything else` | 從「主題包那一側」釘住 `themeIdentity` 碰不到(第九輪確認過它的哨兵是活的) |
| `frontend/src/lib/themeBundle.test.ts` | `validateWording > keeps a themeMarkers override and still drops a theme's identity` | **200 字那條涵蓋沒有刪,是改寫**:改成 `themeMarkers.builtinGroup` 撐到 200 rune **仍被接受**(驗證器對可覆寫用詞的上限沒有跟著縮),同時 `themeMarkers.copyTag` 現在必須被列入 `skipped` |

### 紅證(弄壞 → 紅 → 還原 → 綠)

**紅證 1(現行定案)**:把主題包可填的字串拼回下載名字 ——
`ThemeSettings.tsx` 改成 `${t.themeIdentity.office}(${t.themeMarkers.builtinGroup})`
(`builtinGroup` 是可覆寫用詞,與 `copyTag` 同性質):
log `round10-fix-run/redproof-1-name-splices-pack-wording.txt`

```
× ThemeSettings · export > office 列下載鈕可用,下載一個非保留 id 的 office 包(可再匯入)
  → expected '辦公室(內建)' to be '辦公室'
Tests  1 failed | 17 passed (18)
```

還原後綠:`round10-fix-run/green-1-after-restore.txt` ——
`Test Files 17 passed / Tests 282 passed`(`src/lib` + `src/i18n` + `ThemeSettings.test.tsx`)。

**第一版(清理器)的紅證仍保留在證據目錄**,因為它證明的失敗模式是真的:
* `redproof-1-copytag-no-sanitize.txt` —— 清理全拿掉 → 200 字 copyTag 讓內建下載檔匯不回來
  (`AssertionError: "xxx…" → "辦公室(xxx…)": expected false to be true`)。
  **這就是第九輪 SHOULD-2 描述的那個 145 字檔名,我自己重現到紅。**
* `redproof-1b-no-class-strip.txt` —— 只拿掉字元清理 → 控制字元那幾列紅。

**有無偏離定案**:無。沒有對主題包加任何新限制;`themeMarkers.builtinGroup` /
`customGroup` 仍然可覆寫(round 8 的放寬原封不動),只有 `copyTag` 因為**沒有使用點**
而退休。

---

## 2. 過期的註解與說明(review round 9 SHOULD-3,共 8 處)

逐處改寫成現況(不是刪掉了事,而是把「現在為真的事」寫清楚):

| 檔:行 | 原本寫的 | 改成 |
|---|---|---|
| `frontend/scripts/gen-theme-tokens.mjs:60-61` | 「標題的 TEXT 住在**不可覆寫**的 `themeMarkers` 子樹」(產生器的**設計說明**,顏色放開的整個論證建立在這句上) | 直說 `themeMarkers.*` 同一輪就變回可覆寫用詞;**真正不可偽造的是「哪一列落在哪一組」**(由渲染決定),外加 `themeIdentity.*` 這唯一一個排除子樹 |
| `frontend/src/styles/theme.css:168` | 「標題的『文字』與『屬於哪一組』仍不可偽造」(半假) | 「同一輪也把標題文字(`themeMarkers.*`)還給主題包;仍不可偽造的是**哪一列屬於哪一組**」 |
| `frontend/src/components/theme-settings.css:65` | 「the twin of the quick picker's `<optgroup>`」 | 改寫成「round 7 移除了下拉的 `<optgroup>`(owner:下拉不必再標一次)」 |
| `frontend/src/components/theme-settings.css:68` | 「…and in the **non-overridable** `themeMarkers` i18n subtree」 | 保障**完全**落在 render 上,與顏色、用詞都無關 |
| `frontend/src/components/ThemeSettings.test.tsx:93-94` | 「…from the non-overridable themeMarkers subtree — the same source the quick picker's `<optgroup>` uses」(兩句都假) | 兩句都改:round 8 把標題文字還給主題包、round 7 移除 `<optgroup>`,這條測試釘的是 render |
| `frontend/src/components/ThemeSettings.test.tsx:546-550` | 「since T-081b a bundle **may not claim a built-in display name**…」(round 8 正是拆掉這條的那輪) | 改成第 1 條的新事實:名字整個來自 `themeIdentity`,主題包碰不到 |
| `frontend/src/i18n/locales/zh.ts` / `en.ts` `themeMarkers` 上方說明 | 「以及匯出副本檔名裡的『副本』」 | 第 1 條把 `copyTag` 移除後,說明只剩分組標籤 |

**佐證**:`grep -rn -i 'non-overridable\|不可偽造\|optgroup' frontend/src frontend/scripts`
之後剩下的每一筆都**指向 `themeIdentity`**(那個確實仍不可覆寫)、或是過去式敘述
(`Rounds 3–4 held them non-overridable…`)、或是否定斷言
(`ProfileDropdown.settings.test.tsx:76` 的 `optgroup … toBe(0)`,那是刻意的絆線)。
沒有留下任何會誤導下一個人的敘述。

**測試**:註解改動本身無行為,佐證是上面那道 grep 加整份 CI 全綠(見文末)。

---

## 3. 交付說明整份更新(review round 9 SHOULD-1 / SHOULD-4)

`docs/T-081b-evidence/T-081b-final-delivery.md` **整份重寫**,逐條對上審查點名的問題:

* 🔴 **§四.2 的假宣稱已改成誠實版本**(最重要的一條)。原文「已窮舉整個色域,
  **不存在**任何單一顏色能同時滿足三個底色」——**是錯的**。新版寫的是:
  * 「無解」**只在「數字維持白色」的前提下成立**(白字要 AA ⇒ 底色亮度 ≤ 0.1833;
    對 `--color-indigo` 要 ≥3:1 ⇒ ≥ 0.2044,兩者不相交)。
  * **放開字色有 152,663 個解**;例:`#ff8f88` + 深色數字,對三底色 7.73 / 6.69 / 5.62,
    **不需要外框**。重現指令一併寫進文件。
  * ⇒ 結論改寫成:**它有解,我們選擇了「數字維持白色」這個約束,因此採用外框**。
    並保留第九輪的 NIT-1(外框自身對分頁底只有 1.38:1),明講**不宣稱「已解決」**。
* **三處指向已刪機制**:§四.3 的「小標籤固定成深藍紫」→ 改成「chip 第六輪整個移除、
  marker 色槽第八輪刪除,現在由分組標題表示」;§四.6 的「防線改由結構化分組承擔」
  → 補上它現在是**唯一**一條腿以及為什麼仍然足夠;§六 的「marker 族」→ 刪除。
* **白名單顆數**:71 槽 + 徽章底 + marker 族 → **`--color-*` 全集 72 顆,零排除**
  (實測:`node scripts/gen-theme-tokens.mjs` 印 72;`theme.css` distinct `--color-*` 也是 72)。
  另補上用詞白名單 **790** 顆(第 1 條移除 `copyTag` 後的新數字)。
* **證據清單補齊**:補上 round 6/7/8 的 fix-report 與 fix-run、round 9 的
  `review-round9.md` + `round9-review/`,以及本輪的 `round10-fix-report.md` / `round10-fix-run/`。
* 另補:§一~§三 更新到第十輪(九輪審查的表格)、§四.1 的內建外觀變動從 1 處改成
  **3 處**(徽章底色、登入頁提示字、導覽列 padding)。

---

## 4. 導覽列頁籤帶的上下留白改對稱(owner 第十輪追加)

**定案**:`.nav-tabs` 的 `padding: 2px 22px 12px` → `padding: 7px 22px`(左右不變)。

### 改了什麼

* `frontend/src/components/chrome.css` —— `.nav-tabs` 的 padding,加上一段說明
  「為什麼是對稱的、為什麼總和必須是 14px、由誰釘住」。
* `@media (max-width: 720px)` 那塊只改 `padding-left/right`(longhand),**不影響上下**,
  所以窄版與寬版一致 —— 已實測(下表兩個寬度)。

### 實測:總高度不變、下面的東西沒位移

在**真實 Chromium**(Playwright CT)量 `.nav-tabs` 與其中 `.nav-tabs__seg` 的實際盒子:

| 狀態 | 寬度 | padTop | padBottom | 框上方留白 | 框下方留白 | **帶總高** | 框高 |
|---|---|---|---|---|---|---|---|
| 改前(`2px/12px`) | 390 / 1280 | 2 | 12 | 2 | 12 | **58** | 44 |
| 改後(`7px/7px`) | 390 / 1280 | 7 | 7 | 7 | 7 | **58** | 44 |

**帶總高兩邊都是 58px(= 框 44 + 上下 14)**,所以底下的 `.app__main` 起點不動、
頁面其他元素零位移。頁籤本身**往下移 5px**(框上方留白 2 → 7)——
**這是 owner 知情並拍板接受的第三處內建外觀變動**(前兩處:未讀徽章底色 `#ba5953`、
登入頁提示字 `#8f9299` → `#9aa0ad`)。
log:改後 `round10-fix-run/green-4-navband-after.txt`、改前
`round10-fix-run/redproof-4-navband-asymmetric.txt`(兩份都印出上表的量測 JSON)。

### 視覺回歸守衛:**沒有任何一條因此變紅,也沒有更新任何基準**

* 全套 Playwright CT **148 條全綠**(146 條既有 + 本輪新增 2 條)。
* 原因不是繞過:`frontend/visual-guards/` 底下**沒有任何截圖基準**
  (`grep -rn 'toHaveScreenshot|toMatchSnapshot' visual-guards/` 無輸出,
  `visual-guards/__snapshots__` 不存在)——這些守衛斷言的是**幾何不變式**
  (相對位置、可見寬度、對比度),而這次改動讓總高不變,所以沒有一條的前提被動到。
  ⇒ **沒有基準需要更新,我也沒有更新任何基準。**

### 釘住「上下對稱」的守衛 + 紅證

新增於既有的 `frontend/visual-guards/nav-tabs-narrow.ct.spec.tsx`(**擴充,未新開檔**):
`nav band padding is symmetric and its height is unchanged @390` / `@1280`,四條斷言:
① `padTop === padBottom` ② 框的上下實際留白相等(量的是盒子,不是宣告值)
③ `padTop + padBottom === 14`(所以「8/8」這種對稱但改變總高的寫法也會紅)
④ 帶高 === 框高 + 14。

**紅證**:把 `padding` 改回 `2px 22px 12px` →
log `round10-fix-run/redproof-4-navband-asymmetric.txt`

```
@390  nav band {"padTop":2,"padBottom":12,"above":2,"below":12,"bandH":58,"segH":44}
@1280 nav band {"padTop":2,"padBottom":12,"above":2,"below":12,"bandH":58,"segH":44}
✘ nav band padding is symmetric and its height is unchanged @390
  Error: the band's top and bottom padding must be equal
✘ … @1280(同上)
2 failed | 3 passed
```

還原後綠:`round10-fix-run/green-4-navband-after.txt`(5 passed)。

**有無偏離定案**:無。左右 22px 未動、總高未動、沒有為了讓守衛綠而改守衛。

---

## 產生器與 drift

動到 `gen-theme-tokens.mjs`(註解)與 i18n 字典(移除 `copyTag`)後**兩支產生器都重跑**:

```
[gen-theme-tokens] wrote 72 tokens   → themeTokens.generated.ts / theme_colornames_gen.go
[gen-message-keys] wrote 790 message keys → messageKeys.generated.ts / message_keys_gen.go
```

重跑後 `git status --porcelain` 對產生檔**零差異**(drift 歸零);TS 白名單與 Go 白名單同步。

## 完整 CI

```
[ci] commit fc1af1d (工作樹有本輪改動) — 原樹、非副本
frontend:    Test Files 166 passed (166) / Tests 1325 passed (1325)
Playwright:  148 passed (34.2s)      ← 146 既有 + 本輪新增 2 條
conformance: [conformance] all green (target=go base=http://127.0.0.1:8795)
[ci] all green
CI_EXIT=0
```

log:`docs/T-081b-evidence/round10-fix-run/ci-round10.log`(第 3566 行 `[ci] all green`)。

* ⚠️ 已知會偶爾紅的 `useRelocateMachine.test.tsx` **這次綠**(18 tests,261ms,log 第 1222 行),
  **沒有重跑帶過**(整份 CI 只跑了一次)。
* 起跑前 `ps` 確認本機**沒有**任何 vite / playwright / pytest / go test 在跑
  (第九輪那位審查者遇到的 dev server 已經結束);只有編輯器的 language server
  (tsserver / pyright)。CI 用的 port(CT 5241、conformance 8795)無衝突。
* 全程在**原樹**上跑,**沒有**建立隔離副本、**沒有**碰 `node_modules`、
  **沒有**跑 `npm ci`(第九輪的事故教訓)。所有紅證都是單檔小改、當場還原,
  每次不超過一分鐘。

## 工作區狀態

`git diff`(已追蹤檔)只含本輪的 11 個檔;產生檔零 drift;沒有動任何
`docs/T-081b-evidence/shots/*` 或 `nav-frame-diagnosis.md`(另一位 agent 的未追蹤檔)。
