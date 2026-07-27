# T-081b — 第三輪審查修復報告(BLOCKER ×2 / SHOULD ×4 / NIT ×2)

- 工作區:`/Users/eva/Desktop/gofreight/OffiCraft-wt-T081b`,分支 `feat/T-081b-theme-token-split`,**全部未 commit**
- 依據:`docs/T-081b-evidence/review-round3.md`,每條照定案修法實作
- log 目錄:`docs/T-081b-evidence/round3-fix-run/`
  - `redproof-sabotage-restore.log` — 11 次「弄壞→紅→還原→綠」的完整輸出
  - `ci-full-run.log` — 最終 `bash bin/ci.sh`

「實測會紅」不是人工回想:`redproof` 逐條把**產品碼**改壞、跑對應測試、還原、再跑一次,
每一條都印出 `RED-then-GREEN`;下表的失敗測試名稱與訊息都可在該 log 裡逐字對照。

---

## BLOCKER-1 — 前後端名稱判決不一致(`﻿辦公室` 可從 API 側繞過)

**修法(照定案)**:兩端把 **U+200B / U+200C / U+200D / U+2060 / U+FEFF / U+061C** 加進「隱形字元」
拒收集合,擴充既有的 `hasInvisibleNameRune`(Go)/ 控制字元那條路徑(TS),**不依賴 trim 的差異**。

| 檔案 | 改了什麼 |
|---|---|
| `server/ocserverd/theme_bundle.go` | 新增 `zeroWidthNameRunes`(6 個碼位);`hasInvisibleNameRune` 一併檢查控制字元 / bidi / 零寬 |
| `frontend/src/lib/themeBundle.ts` | 新增 `ZERO_WIDTH_NAME_CODEPOINTS` + `hasInvisibleNameRune`(取代原本 `hasControlChar \|\| hasBidiFormatChar` 的組合);`validateThemeBundle` 改用它 |

兩端錯誤訊息同步改為 `name must not contain control, bidirectional or zero-width characters`。
U+061C 本身就是 Unicode 正式的 bidi 格式字元(原清單漏掉),一併補上。

**測試**
- Go:`TestValidateThemeBundles/rejects a name carrying control, bidirectional or zero-width characters`(`server/ocserverd/theme_bundle_test.go`)— 表格加入 `﻿辦公室`、`辦公室﻿`、`﻿Office`、`Office﻿`、ZWSP/ZWNJ/ZWJ/WORD JOINER/ALM 共 10 例
- TS:`validateThemeBundle > rejects a name carrying control, bidirectional or zero-width characters`(`frontend/src/lib/themeBundle.test.ts`)— 同一份表格(互為 twin,註解互指)

**實測會紅**
- 弄壞:Go `hasInvisibleNameRune` 拿掉 `|| zeroWidthNameRunes[r]`
  → 紅:`theme_bundle_test.go:52: name "﻿辦公室" must be rejected, got <nil>` → 還原後 `ok ocserverd`
- 弄壞:TS `hasInvisibleNameRune` 裡 `ZERO_WIDTH_NAME_CODEPOINTS.has(cp)` 改成 `false`
  → 紅:`validateThemeBundle > rejects a name carrying control, bidirectional or zero-width characters` → 還原後 43/43 綠

---

## BLOCKER-2 — 內建標記可偽造(兩個一模一樣的「辦公室(內建)」)

**修法(照定案)**:標記改為**結構化**。`<select>` 用 **`<optgroup>`** 分「內建 / 自訂」兩組,
移除 select 裡 `themeBuiltinOption` 的字串拼接;設定頁的 `ts-tag` **未動**;optgroup 兩個標籤走 i18n(中英都有)。

| 檔案 | 改了什麼 |
|---|---|
| `frontend/src/components/ProfileDropdown.tsx` | select 內容改成兩個 `<optgroup>`;內建 option 的文字只有主題名本身;自訂組在沒有自訂主題時不渲染 |
| `frontend/src/i18n/compose.ts` | 刪掉 `themeBuiltinOption`(唯一使用者已消失,留著就是留一條可偽造的拼接路徑) |
| `frontend/src/i18n/locales/{zh,en}.ts` | 新增 `themeMarkers.builtinGroup` / `customGroup`(「內建」/「自訂」、Built-in / Custom) |

`themeMarkers` 是**不可覆寫**子樹(見 SHOULD-5):可覆寫的分組標題等於允許主題包把「自訂」改成「內建」,
偽造就從另一扇門回來。

**測試**(`frontend/src/components/ProfileDropdown.settings.test.tsx`,延用既有 describe)
- 既有的 `keeps only the theme SELECTOR …` 改為斷言 option 文字 = 主題名、且各自落在正確的 optgroup
- 新增 `cannot be made to show two identical built-in rows by a theme's NAME`:實際把
  `{id:"spoofpack", name:"辦公室(內建)"}` 寫進 `customThemes` 後渲染,斷言兩列文字不同、內建組只含 `office`

**實測會紅**
- 弄壞:把 optgroup 換回 `<option>{`${name}(${themeBuiltinTag})`}</option>` + 裸名稱列表
  → 紅:`ProfileDropdown · preferences scope > cannot be made to show two identical built-in rows by a theme's NAME` → 還原後 9/9 綠

---

## SHOULD-3 — `check-token-roles.mjs` 的三個繞法

**修法(照定案)**:守衛改成只認 `:root` 區塊裡的定義、同名取**最後一條**(`.at(-1)`)、並把**前景色**一併納入。

`frontend/scripts/check-token-roles.mjs`:
- 新增 `rootDefs`:只收 `rel === styles/theme.css && selector === ":root"` 的定義。selector 是整條
  prelude 鏈,所以 `@media print :root` 自然被排除(繞法 C)。`concreteValue` 從 `rootDefs` 取 `.at(-1)`。
- 徽章選擇器迴圈由 `find` 改 `filter`,檢查**每一條**規則(繞法 B),且三個屬性都要:
  `background`→`--color-danger-badge`、`color`→`--color-on-danger`(繞法 A)、`outline`→`--color-bg`(SHOULD-4)。
- 新增 `TOKEN_ROLES_SRC` 環境變數(只為讓測試把它指到臨時樹上;沒有它就無法實測守衛會紅)。

**測試**(新檔 `frontend/scripts/check-token-roles.test.ts`,與被測腳本同層,符合本 repo 的 co-located 慣例)
把真實 stylesheet 複製到 temp 樹、就地弄壞、跑腳本、斷言 exit=1 與訊息:
- `passes on the tree as shipped`
- `fails when a badge's text colour stops using the measured token`(繞法 A)
- `fails when a later declaration re-paints a badge with --color-danger`(繞法 B)
- `fails when the badge fill drops below AA in :root, however it is patched elsewhere`(繞法 C)
- `fails when a badge loses its page-colour ring`(SHOULD-4)
- `fails when a badge token is defined outside the theme's :root`(NIT-9)

也就是:三個繞法的「弄壞」現在是**測試本身在做**,每次 CI 都重跑一遍。

**實測會紅**(額外把守衛自己改回舊行為)
- 弄壞:移除 `color` 那條 required → 紅:`fails when a badge's text colour stops using the measured token`
- 弄壞:徽章規則只取前幾條(模擬 `find`)→ 紅:`fails when a later declaration re-paints a badge with --color-danger`
- 弄壞:`concreteValue` 改讀全樹 `defs` → 紅:`fails when the badge fill drops below AA in :root, however it is patched elsewhere`
- 三者還原後皆 6/6 綠

---

## SHOULD-4 — 徽章在 active 分頁上只有 2.74:1

**修法(照定案)**:**沒有**再試著換色(審查已窮舉證明不存在合格色)。三顆徽章各加**一圈 1px 的頁底色外框**
(`outline: 1px solid var(--color-bg)`),把徽章從任何底色上分離;守衛的報錯訊息明講量的是哪個底色。

| 檔案 | 改了什麼 |
|---|---|
| `frontend/src/components/chrome.css` | `.nav-tab__badge` 加外框(完整理由寫在這裡) |
| `frontend/src/components/office.css` | `.office__tab-badge`、`.member-card__unread` 加外框(一行註解指回 chrome.css) |
| `frontend/src/styles/theme.css` | `--color-danger-badge` 註解補上「3.76:1 只在底色是 --color-bg 時成立,故有外框」 |
| `frontend/scripts/check-token-roles.mjs` | 第二道門檻的對象改名為 `BADGE_RING`;訊息寫明「量的是外框的 `--color-bg`,**不是**徽章背後的東西:active 頁籤是 `--color-indigo`、選中卡片是 `--color-card`,外框才是把它跟兩者分開的東西」;`ok` 那行也改成 `X:1 vs --color-on-danger / Y:1 vs --color-bg (its 1px ring)`,並註明只描述內建主題 |

用 `outline` 而非 `border`:不影響 flex 佈局,且跟著 `border-radius` 走。

**測試**:`check-token-roles.test.ts > fails when a badge loses its page-colour ring`,
外加 `passes on the tree as shipped` 斷言輸出必須指名量的是哪些 token。

**實測會紅**
- 弄壞:從 `chrome.css` 刪掉 `outline: 1px solid var(--color-bg);`
  → 紅:`.nav-tab__badge has no outline declaration — without the page-colour ring the pill is measured against the wrong background (--color-indigo on an active tab is 2.74:1).`(`passes on the tree as shipped` 失敗)→ 還原後 6/6 綠

---

## SHOULD-5 — `themeCopyTag` 是可覆寫字串

**修法(照定案)**:用 themeIdentity 那套**既有結構機制**排除,**由產生器執行**,沒有第二份手維清單。

| 檔案 | 改了什麼 |
|---|---|
| `frontend/src/i18n/locales/{zh,en}.ts` | 刪掉 `settings.themeCopyTag`;新增不可覆寫子樹 `themeMarkers`(`builtinGroup` / `customGroup` / `copyTag`) |
| `frontend/scripts/gen-message-keys.mjs` | 原本「`key === "themeIdentity"` 就整棵跳過」擴充為 `NON_OVERRIDABLE_SUBTREES = {themeIdentity, themeMarkers}`——同一條**結構**規則,不是 key 清單;`identityNames` 仍只讀 `themeIdentity`,所以 `THEME_IDENTITY_NAMES` 沒有被污染 |
| `frontend/src/i18n/compose.ts` | `themeCopyName` 改讀 `t.themeMarkers.copyTag` |
| 產生檔 | 重跑 `npm run gen:msgkeys`:`frontend/src/i18n/messageKeys.generated.ts`、`server/ocserverd/message_keys_gen.go`(784 keys,`settings.themeCopyTag` 消失、`themeMarkers.*` 從未進入) |

**測試**
- `frontend/src/i18n/messageKeys.theme-identity.test.ts`(延用既有 `describe("MESSAGE_KEYS")`):
  `does not let a theme bundle forge the markers that tell themes apart`(整棵 `themeMarkers` 都不得在白名單裡,並明確點名舊位置 `settings.themeCopyTag`)、
  `keeps the theme markers present and non-empty in both languages`
- `frontend/src/lib/themeBundle.test.ts > validateWording > drops an override of the theme structural markers`:
  帶 bidi 的 `settings.themeCopyTag`、200 字的 `themeMarkers.copyTag`、互換的兩個分組標題,全部被 drop 並回報

**實測會紅**
- 弄壞:`NON_OVERRIDABLE_SUBTREES` 拿掉 `THEME_MARKERS_SUBTREE` 並重跑產生器
  → 紅:`validateWording > drops an override of the theme structural markers`(以及 messageKeys 那兩條)→ 還原後全綠

---

## SHOULD-6 — `OFFİCE`(U+0130)Go 拒 / TS 收

**修法(照定案)**:兩端改用**同一套 ASCII-only 折疊**——只 trim ASCII 空白、只小寫 A–Z,不再呼叫
`strings.ToLower/TrimSpace` 或 `.trim()/.toLowerCase()`。

| 檔案 | 改了什麼 |
|---|---|
| `server/ocserverd/theme_bundle.go` | 新增 `trimThemeName`(`strings.Trim(s, "\t\n\v\f\r ")`)與 `normalizeThemeName`(只折 A–Z);`isBuiltinThemeName` 與長度檢查都改用它 |
| `frontend/src/lib/themeBundle.ts` | 同名 twin(`ASCII_SPACE_EDGES_RE` + 逐碼位折 0x41–0x5A);長度檢查與保留名比對都改用它 |

`normalizeThemeName` 在 TS 側 `export`,原因寫在註解裡:**沒有**任何經由 `validateThemeBundle`
可觀察的行為能區分 ASCII 折疊與 `toLowerCase()`(折得更多只會拒得更多,而不存在「full case mapping 會折到
office、ASCII 折疊不會」的字串),所以兩端的一致性只能釘在 normalizer 本身。

**測試**
- `frontend/src/lib/themeBundle.test.ts > normalizeThemeName > trims ASCII whitespace only and folds A–Z only`
- `server/ocserverd/theme_bundle_test.go > TestNormalizeThemeName`(同一份表格,互為 twin)
  兩份表格都涵蓋:`OFFİCE` 不折、全形 `ＯＦＦＩＣＥ` 不折、`U+212A` 只折 ASCII 部分、`U+3000`/`U+00A0` 不 trim
- 另外 `OFFİCE`、`　辦公室`、`辦公室　` 進了**接受**表(兩端都必須收),`  OFFICE  ` 仍在拒收表

**實測會紅**
- 弄壞:Go 改回 `strings.ToLower(strings.TrimSpace(...))`
  → 紅:`theme_bundle_test.go:95: name "OFFİCE" must be accepted, got custom_themes[0]: name "OFFİCE" is reserved for a built-in theme` → 還原後 ok
- 弄壞:TS 改回 `s.trim().toLowerCase()`
  → 紅:`normalizeThemeName > trims ASCII whitespace only and folds A–Z only` → 還原後 43/43 綠

---

## NIT-7(有餘力的兩條,都做了)

**NIT-8 — 前端補上 Go 那條推導守衛**
`frontend/src/lib/themeBundle.test.ts > isBuiltinThemeName > derives the banned set from the locales, not from two literals`
——與 `TestIsBuiltinThemeName` 對稱:每個 `RESERVED_THEME_IDS` 都必須在 `THEME_IDENTITY_NAMES` 裡有非空名稱
(點名 `?? []` 這個靜默失效形狀),且保留集外的 id(`newTheme`)必須仍可被自訂主題使用。

**NIT-9 — 釘住徽章 token 的定義位置**
守衛新增:`--color-danger-badge` 與 `--color-on-danger` 必須定義在 `styles/theme.css` 的 `:root` 區塊,
否則違規(訊息說明「比值是在那份定義上量的」)。測試:
`check-token-roles.test.ts > fails when a badge token is defined outside the theme's :root`。
弄壞守衛的這段檢查 → 該測試紅 → 還原後 6/6 綠。

---

## 不誤擋合法名稱

兩端的**接受**表都涵蓋:中日韓(`精靈村`、`深海の夜`、`밤하늘`)、emoji(`🌙 Midnight 🌙`)、
含空格與標點(`Mid night — v2 (beta)!`)、新主題預設名(`新主題` / `New theme`)、
`Officescape` / `辦公室的夜`,以及本輪新增的 `OFFİCE`、`　辦公室`、`辦公室　`。

一個刻意接受的取捨:因為 trim 改成 ASCII-only,一個**只由 U+3000 組成**的名字長度算 1、會被收下
(以前 Go/TS 各自的 trim 會把它清空而拒收)。它不是冒充任何主題,只是一個看起來空白的名字;
換來的是兩端由構造上一致——這正是定案修法選的方向。

## 產生器 drift

`npm run gen:msgkeys` 已重跑並提交進工作區(兩個產生檔);`gen:tokens` / `gen:fonts` / `gen:api` 由
`bin/ci.sh` 的四道 drift gate 驗證,全部無 drift。

## 偏離定案修法之處

一處,方向是**加嚴**而非改方案:optgroup 的兩個標籤(以及 `themeCopyTag`)放進同一個
**不可覆寫**的 `themeMarkers` 子樹,而不是重用可覆寫的 `settings.themeBuiltinTag` / `themeCustomTag`。
定案只要求「optgroup 的兩個標籤要走 i18n(中英都要)」——這仍然是 i18n,只是同時關掉了
「主題包把『自訂』改成『內建』」這個把偽造從另一扇門帶回來的路徑,並讓 SHOULD-5 的排除機制有一個
語意正確的家(不必把「副本」塞進 `themeIdentity`、污染 `THEME_IDENTITY_NAMES`)。
其餘各條皆照定案修法,無替代方案。

## 最終驗證

`bash bin/ci.sh` → **`[ci] all green`**(log:`docs/T-081b-evidence/round3-fix-run/ci-full-run.log`)
其中 frontend 164 檔 / 1305 測全綠、`go test ./...` 綠、`lint:token-roles` 綠、四道產生器 drift gate 綠、
conformance 975 passed。
