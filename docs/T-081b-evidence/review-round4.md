# T-081b — 第四輪獨立審查

**審查版本快照(tracked diff sha256 前綴)**:`11607ba83d783392`
(完整:`11607ba83d783392ff2c5a2551d8b2dc91c222a48e5836caeb8e21a1058a6b86`,60 檔 +3880/-1044,另有 11 個未追蹤新檔)

分支 `feat/T-081b-theme-token-split`,全部未 commit。審查者未參與實作。
證據:`docs/T-081b-evidence/round4-review/`(`guard-bypass-probe.md`、`redproof-round4.md`)。

---

## 0. 前置:名稱同構資料的複核

`round4-review/{name-cases.json,go-verdicts.json,ts-verdicts.json}` 為前一位審查者產出。
本輪重跑逐條 diff:57 組中有 28 組字串不同,但差異**全部只在錯誤訊息的路徑前綴**
(Go `custom_themes[0]: ` vs TS `theme: `),那是兩邊 harness 呼叫位置不同所致,不是判決差異。
ACCEPT/REJECT 分類與理由字串 **57/57 一致**。同構結論**成立**,不再重證。

---

## 1. 發現

### BLOCKER-A — BLOCKER-2 的偽造症狀在「設定頁 → 主題」清單上原封不動地重現

**為什麼是問題**:round3 的 BLOCKER-2 定義為「畫面上出現兩個一模一樣的『辦公室(內建)』」。
optgroup 修法只蓋住 **ProfileDropdown 快速選單**這一個介面。設定頁的主題清單
(`frontend/src/components/ThemeSettings.tsx:1101-1185`)**沒有 optgroup、沒有群組標題**,
內建/自訂的唯一可見區別是一顆文字 chip:

- `ThemeSettings.tsx:1108` — `<span className="ts-tag">{t.settings.themeBuiltinTag}</span>`
- `ThemeSettings.tsx:1154` — `<span className="ts-tag ts-tag--custom">{t.settings.themeCustomTag}</span>`

而這兩個 key **都還在可覆寫白名單裡**:
`frontend/src/i18n/messageKeys.generated.ts:573,587`、`server/ocserverd/message_keys_gen.go:574,588`。

**怎麼重現**(我已實測,不是推論):

1. 覆寫可被接受 —— 暫時放一支 probe 測試呼叫 `validateWording`:
   ```
   validateWording({ zh: { "settings.themeBuiltinTag": "自訂",
                          "settings.themeCustomTag":  "內建" } })
   → 回傳 null(通過,且物件未被 drop,覆寫原封不動保留)
   ```
   對照組:SHOULD-5 把 `themeMarkers.copyTag` 移進不可覆寫子樹後,同樣形狀的覆寫**會**被 drop。
   也就是說機制存在、就是沒套用到這兩顆 key 上。
2. 名字可以長得跟內建一樣 —— `isBuiltinThemeName` 只做 ASCII 折疊,所以
   `「　辦公室　」`(U+3000 包夾)與 `「辦公室 」` 都是 **ACCEPT**
   (見 `go-verdicts.json` 的 `ideographic_space_pad` / `nbsp_pad_office`),
   但畫面上就是「辦公室」。
3. 最後一個結構線索(chip 顏色:藍=內建 / 紫=自訂)也是主題自己控制的 ——
   `theme-settings.css:69-79` 用 `--color-seg-fill` 與 `--color-icon-violet-bg`,
   兩顆都列在 `frontend/src/styles/themeTokens.generated.ts:41,61`,即**主題包可覆寫的 token**。
   把 `--color-icon-violet-bg` 設成等於 `--color-seg-fill`,兩顆 chip 連顏色都一樣。

三步疊起來,設定頁主題清單出現**兩列文字與顏色都相同的「辦公室 [內建]」** —— 正是 BLOCKER-2 的原始症狀。

**殘餘防線**:內建列永遠排第一、且它的編輯/刪除鈕是 `disabled`。這兩點主題包動不了,
所以不是「完全無法分辨」;但要靠使用者去比對按鈕的可用性,遠弱於 optgroup 給的結構性保證。

**判定 BLOCKER 的理由**:同一個缺陷、同一個症狀、同一張票,只修了兩個介面中的一個;
`round3-fix-report.md` 寫「設定頁的 `ts-tag` **未動**」但沒有論證該介面為何安全。
修法很便宜:把這兩顆 key 併進既有的 `themeMarkers` 不可覆寫子樹(SHOULD-5 已經鋪好路),
或在該清單也採用與 optgroup 同級的結構化分組。

---

### SHOULD-B — `check-token-roles.mjs` 的守衛仍有 5 條可繞路徑(A–E 全部實測 exit=0)

證據:`round4-review/guard-bypass-probe.md`(方法:複製 `frontend/src` 到 temp 樹,
用 `TOKEN_ROLES_SRC` 指過去,**真實工作區未被改動**)。

| # | 追加的 CSS | 位置 | 守衛 | 在瀏覽器上是否真的生效 |
|---|---|---|---|---|
| A | `:root:root { --color-danger-badge: #f0736b; }` | `styles/theme.css` | **exit=0** | 生效。specificity (0,2,0) > (0,1,0);螢幕是 2.85:1,守衛照印 4.52:1 |
| B | `:root { --color-danger-badge: #f0736b; }` | `styles/global.css` | **exit=0** | 生效。`main.tsx:5-6` 先 theme.css 後 global.css,同 specificity 由後者勝 |
| C | `.nav-tab__badge.is-hot { background: var(--color-danger); }` | `components/chrome.css` | **exit=0** | 生效(需 markup 加 class)。`selector.split(" ").at(-1)` 得 `.nav-tab__badge.is-hot` ≠ `.nav-tab__badge` |
| D | `.nav-tab__badge, .zz { background: var(--color-danger); }` | `components/chrome.css` | **exit=0** | 生效。selector list 的 `.at(-1)` 取到 `.zz`,整條規則被跳過 |
| E | `.nav-tab__badge { outline-color: transparent; }` | `components/chrome.css` | **exit=0** | 生效。shorthand 先設、longhand 後蓋 → 環消失;守衛只比對 `prop === "outline"` |
| F(對照) | `.nav-tab__badge { outline: none; }` | — | exit=1 ✅ | — |
| G(對照) | `.nav-tab__badge { background: var(--color-danger) !important; }` | — | exit=1 ✅ | — |

F/G 證明守衛是活的,A–E 是比對邏輯的具體漏洞:

- **A、B 是 SHOULD-3 C 的鏡像**。C 修的是「合規值被停在不生效的地方(`@media print`)」,
  作法是「只認 `rel === theme.css && selector === ":root"`」。這個條件擋住了「藏在 at-rule 裡的假合規值」,
  卻同時讓「**不**合規但**確實生效**的值」(更高 specificity 的 `:root:root`、後載入檔案的 `:root`)
  整個從量測視野中消失。守衛的模型是「`:root` 那一行就是螢幕真相」,而 CSS 的真相是 specificity + 順序。
- **D 是 SHOULD-3 B 沒補完的部分**。`find` → `filter` 解決了「多條規則」,
  但沒解決「一條規則裡的多個選擇器」——同一個 `.at(-1)` 假設在兩個地方都不成立。
- **E 是 SHOULD-4 自帶的漏洞**。環只被 `outline` 一個 prop 名釘住,longhand 就繞過去了。

**為什麼是 SHOULD 而不是 BLOCKER**:這些都需要**未來有人寫出**那樣的 CSS 才會出事,
現行程式碼沒有任何一條踩到(baseline exit=0 且值是對的)。守衛的價值是防未來回歸,
它比宣稱的弱,但沒有現行的壞畫面。

---

### SHOULD-C — 隱形字元用「碼位黑名單」而非 Unicode 類別,漏掉同類字元;保留名檢查因此可被繞過

BLOCKER-1 的修法是把 6 個零寬碼位加進一份**手列清單**。結果同一個 Unicode 類別裡沒被想到的成員全部漏網:

| 名稱 | 碼位 | Unicode 類別 | 判決 | 畫面上長什麼樣 |
|---|---|---|---|---|
| `soft_hyphen` | U+00AD | **Cf**(與已擋的 ZWSP/ZWNJ 同類) | ACCEPT | `Office` |
| `mongolian_vowel_sep` | U+180E | **Cf** | ACCEPT | `Office` |
| `tag_char` | U+E0041 | **Cf**(TAG,經典隱形仿冒向量) | ACCEPT | `Office` |
| `variation_sel_office` | U+FE0F | Mn | ACCEPT | `Office` |
| `line_sep_u2028` / `para_sep_u2029` | U+2028/9 | Zl / Zp | ACCEPT | `Mid night` |
| `nbsp_pad_office` | U+00A0 | Zs | ACCEPT | `Office`(前後看似空白) |
| `ideographic_space_pad` | U+3000 | Zs | ACCEPT | `辦公室` |
| `ogham_space` | U+1680 | Zs | ACCEPT | `Office`(多數字型為空白) |

**後果**:`isBuiltinThemeName`(保留內建名)這道檢查的**目的**是「自訂主題不得顯示成內建主題的名字」。
上表每一列都**渲染成內建主題的名字**卻通過檢查 —— 這道規則實質被繞過。
在 optgroup 之下,快速選單裡它只造成「內建組和自訂組各有一個看起來一樣的 Office」(困惑);
但它同時是 **BLOCKER-A 的第 2 步**,在設定頁就升級成真正的仿冒。

**建議修法**:改成 Unicode 類別規則(拒 Cf/Cc/Zl/Zp,並在長度/保留名比對前把所有 Zs 正規化為 U+0020),
而不是繼續往清單裡加碼位。否則下一個沒想到的隱形字元就是下一輪。

---

### SHOULD-D — 新測試檔完全在型別檢查範圍外,且此 repo 根本沒有 node 型別

`frontend/scripts/check-token-roles.test.ts` 不在 `frontend/tsconfig.json` 的 `include`(只有 `["src"]`),
也不在 `tsconfig.node.json`(只有 `["vite.config.ts"]`)。`npm run typecheck` / `npm run build`
(`tsc --noEmit`)**完全不看它**,但 vitest 會跑它。

我確認了根因**不只是路徑**:`@types/node` **不在 `frontend/package.json` 的 devDependencies 裡**。
把該檔強制丟給 tsc 會炸出一整片:

```
node_modules/vite/dist/node/index.d.ts(1,23): error TS2688: Cannot find type definition file for 'node'.
... error TS2307: Cannot find module 'node:fs' / 'node:url' / 'node:http' ...
... error TS2591: Cannot find name 'Buffer'. Try `npm i --save-dev @types/node`
```

也就是說「把它加進 include」單獨做**不會**成功,必須同時 `npm i -D @types/node` 並在
`tsconfig.node.json` 加 `"types": ["node"]` 之類的設定。

**嚴重性判定:SHOULD(不是 BLOCKER)**。理由:
- 它是**測試**碼不是產品碼;測試的正確性由它自己會不會紅來保障,而那件事我已實測成立(見 §2)。
- 影響是「這支測試的型別錯誤不會被 CI 擋下」,不是「產品行為錯誤」。
- 但也**不能算 NIT**:此 repo 其餘 `.ts` 全在型別閘內,這是第一個例外,而且是**靜默**的例外
  —— 編輯器對開發者報 6 個紅,CI 卻綠,這正是最容易讓人開始無視編輯器警告的形狀。
  最低限度應該在檔案裡寫明「本檔不在 tsc 範圍內、原因是 repo 無 @types/node」,
  否則下一個人只會困惑。

---

### NIT-E — 「全空白名稱」會被收下

`nbsp_only`(僅 U+00A0)、`ideographic_space_only`(僅 U+3000)判決 ACCEPT,渲染成**完全空白**的一列。
`round3-fix-report.md` 已明確把它記為「刻意接受的取捨」(換兩端構造一致),我同意這個取捨方向。
它不冒充任何主題,只是一列看不見的項目,仍可選取/刪除。列為 NIT 只是提醒它與 SHOULD-C
的修法(Zs 正規化)是同一顆按鈕:一旦改成類別規則,這條會順帶自動變成 REJECT。

---

### 沒問題的部分(明確記錄,避免下一輪重查)

- **外框(SHOULD-4)的副作用:沒問題。**
  - 用 `outline` 而非 `border`:不參與佈局、不撐開 flex,`.nav-tab` 的 `padding: 0 16px`
    讓 1px 環離 tab 邊界還有 16px,`.nav-tabs__seg` 的 `overflow-x: auto` 不會裁到它。
  - 圓角:`outline` 自 CSS UI 3 起跟隨 `border-radius`,徽章 `border-radius: 9px` 上不會出現方角。
  - `box-shadow`:三個徽章都沒有自己的 box-shadow,無疊加問題。
  - 三處 (`chrome.css:282`、`office.css:281`、`office.css:389`) 寫法一致,註解互指。
  - 焦點環衝突:三個徽章都是非可聚焦的 `<span>`,`global.css` 沒有全域 `outline` 規則,
    不會與 `:focus-visible` 打架。
  - 一點值得記錄但不構成問題:環色是 `--color-bg`,而 nav bar 的底是 `--color-nav-bg`
    (預設 `var(--color-bg)`,相同)。若某主題把兩者設成不同色,環會在 nav bar 上顯出一圈細邊 ——
    純視覺,且 3:1「讀得出是徽章」的保證仍由環本身成立,不受背後顏色影響。
- **匯出路徑沒問題**:`themeExport.ts:160-163` 的檔名只由 `id` 推導(`[^a-z0-9-]` 全清),
  名稱不進檔名;內建主題的匯出用 `id: "office-base"` + 非可覆寫的 `themeMarkers.copyTag` 後綴,
  產出的檔案自己能被自己的 validator 收下(不會產生「產品自己拒收的檔案」)。
- **伺服器端沒有用名字判身分**:`settings.go` 一路用 `displayThemeAllowed{"office": true}` 這個 **id** enum,
  名稱只出現在 422 錯誤訊息裡。名稱仿冒進不了後端的授權決策。
- **排序沒問題**:兩個介面都是內建固定第一、自訂維持陣列順序,沒有任何 `sort()`,
  自訂主題無法把自己排到內建之前。

---

## 2. 我親手抽驗的「實測會紅」(全部成立)

規則:自己弄壞產品碼 → 自己看到紅 → 自己還原 → 再確認綠。不看實作者的 log。
完整輸出:`round4-review/redproof-round4.md`。

| 抽驗 | 弄壞的東西 | 結果 |
|---|---|---|
| **第 2 條**(optgroup) | `ProfileDropdown.tsx` 的兩個 `<optgroup>` 換回扁平 `<option>` + 字串拼接 `辦公室(內建)` | **紅 2 條**:`cannot be made to show two identical built-in rows by a theme's NAME` + `keeps only the theme SELECTOR…`;還原後 9/9 綠 |
| **第 3 條 A**(前景色納入) | 從 `required` 拿掉 `["color", BADGE_TEXT, …]` 那列 | **紅**:`fails when a badge's text colour stops using the measured token`;還原後 6/6 綠 |
| **第 3 條 B**(取每一條規則) | `decls.filter(...)` 後接 `.slice(0, 1)`(模擬舊的 `find`) | **紅 2 條**:`fails when a later declaration re-paints a badge with --color-danger` + `passes on the tree as shipped`;還原後 6/6 綠 |
| **第 3 條 C**(只認 `:root`) | `concreteValue` 的 `rootDefs.get(token)` 改回全樹 `defs.get(token)` | **紅**:`fails when the badge fill drops below AA in :root, however it is patched elsewhere`;還原後 6/6 綠 |
| **第 4 條**(1px 外框) | 從 `chrome.css` 刪掉 `outline: 1px solid var(--color-bg);` | **紅**:`.nav-tab__badge has no outline declaration — without the page-colour ring the pill is measured against the wrong background (--color-indigo on an active tab is 2.74:1).`;還原後 6/6 綠 |

**結論:實作者「實測會紅」的宣稱,在我抽驗的 5 個切面上全部屬實**,測試確實咬住了它們宣稱咬住的東西。
需要並列的但書是 §SHOULD-B:這些測試證明的是「**這 5 種**弄壞法會被抓」,
不等於「守衛擋得住 CSS 優先序」—— A–E 五條繞法就是在同一支守衛下 exit=0 通過的。

---

## 3. 八條修正逐條判決

| # | 修正 | 判決 | 理由 |
|---|---|---|---|
| 1 | 名稱驗證前後端不一致(隱形字元)→ 兩端加拒收集合 | **有條件成立** | 前後端**同構**這件事成立(57/57 一致,已複核)。但拒收集合是**手列碼位黑名單**,U+00AD / U+180E / U+E00xx / U+2028-9 / U+00A0 / U+3000 / U+1680 全數漏網,`isBuiltinThemeName` 實質可繞(SHOULD-C)。原題目「兩端不一致」已解決,底層「什麼算隱形字元」沒解決。 |
| 2 | 內建標記可被取名偽造 → `<optgroup>` 結構化分組 | **有條件成立** | 在 ProfileDropdown **完全成立**,已實測會紅。但只覆蓋兩個介面中的一個:設定頁主題清單仍靠可覆寫的文字 chip + 主題可控的 chip 顏色,原症狀原封重現(BLOCKER-A)。 |
| 3 | 守衛三個繞法 → 只認 `:root`、取最後一條、前景色納入 | **有條件成立** | 三個**原繞法**確實都堵住了,三條都親手驗過會紅。但修法引入/留下 5 條新繞法,其中 A/B 正是「只認 `:root`」這個修法本身造成的盲區(SHOULD-B)。 |
| 4 | 未讀徽章 active 分頁 2.74:1 → 加 1px 頁底色外框 | **成立** | 已實測會紅。三處寫法一致,`outline` 選得對(不擠佈局、跟隨圓角),無 overflow/box-shadow/焦點環副作用。唯一附帶問題是守衛對它的釘法可被 `outline-color` longhand 繞過(併入 SHOULD-B E)。 |
| 5 | `themeCopyTag` 可被覆寫 → 移入不可覆寫子樹 | **成立(但範圍選得太窄)** | 機制正確且由產生器執行,沒有第二份手維清單,實作乾淨。問題是同一個 `themeMarkers` 子樹旁邊,`settings.themeBuiltinTag` / `themeCustomTag` 這兩顆**風險高得多**的 key 仍留在可覆寫白名單 —— 修法建好了門,最該進去的兩個沒進去(BLOCKER-A)。 |
| 6 | `OFFİCE` 兩端不一致 → ASCII-only 折疊 | **成立** | 兩端由構造上一致,twin 表格互指。`OFFİCE`/`ＯＦＦＩＣＥ` 被收下是**正確**取捨:兩者都不渲染成內建名(見 §4)。註解誠實說明了「此選擇無法由 `validateThemeBundle` 的外部行為觀察,只能釘 normalizer 本身」,這個自我認識是對的。 |
| 7-8 | 兩條 NIT(前端補推導守衛 / 釘住徽章 token 定義位置) | **成立** | NIT-8 與 Go 端對稱,點名了 `?? []` 的靜默失效形狀。NIT-9 的 `:root` 定義位置檢查有效(但與 SHOULD-B A/B 是一體兩面:它要求定義**在** `:root`,卻不管有沒有**更強的** `:root:root` 蓋過去)。 |

---

## 4. 29 個 ACCEPT 名稱 — 逐條判定

判準:**在 optgroup 已就位的前提下**,收下這個名字實際造成什麼後果。
三種後果分開看:(a) 只是難看 (b) 真能誤導使用者以為那是內建 (c) 產出產品自己拒收的檔案。
**(c) 一個都沒有** —— 所有 ACCEPT 名稱匯出後都能被自己的 validator 重新收下,匯出檔名不含名稱。

### 判 SHOULD 的(渲染成內建主題的名字 → 保留名檢查被繞過)

| 名稱 | 渲染結果 | 判定 | 理由 |
|---|---|---|---|
| `tag_char` (U+E0041) | `Office` | **SHOULD** | 經典隱形仿冒向量。與內建英文名**逐像素相同**。在 optgroup 下只造成兩組各一個 Office(困惑),但它讓「保留內建名」這條規則形同虛設,且是 BLOCKER-A 的組件。 |
| `soft_hyphen` (U+00AD) | `Office` | **SHOULD** | 同上。且 U+00AD 與已被擋的 ZWSP 同屬 Cf,漏擋純粹是清單沒寫到。 |
| `mongolian_vowel_sep` (U+180E) | `Office` | **SHOULD** | 同上,亦為 Cf。 |
| `variation_sel_office` (U+FE0F) | `Office` | **SHOULD** | 同上(Mn,零寬)。 |
| `nbsp_pad_office` (U+00A0) | ` Office `(看似 `Office`) | **SHOULD** | 前後是不可見的空白;在窄欄位/置中排版下與 `Office` 難以區分。 |
| `ideographic_space_pad` (U+3000) | `　辦公室　` | **SHOULD** | **最嚴重的一個**:渲染成中文內建名「辦公室」。BLOCKER-A 重現步驟的第 2 步用的就是它。 |
| `ogham_space` (U+1680) | 多數字型為空白 | **SHOULD** | 同 NBSP;僅在 Ogham 字型下才顯出線條,一般環境等同空白填充。 |

> 這 7 條合起來就是 SHOULD-C,修法是同一個:改用 Unicode 類別規則 + Zs 正規化,而不是繼續補碼位。

| 名稱 | 判定 | 理由 |
|---|---|---|
| `spoof_builtin_marker_zh` 「辦公室(內建)」 / `spoof_builtin_marker_en` | **SHOULD** | optgroup 讓它在快速選單裡變成「掛在**自訂**群組底下、自稱(內建)」的自相矛盾列 —— 已大幅削弱,但沒有消除:瀏覽器把 optgroup 標題渲染成灰色小字,option 文字才是視覺重心,掃一眼仍會誤讀。**在設定頁則完全沒有被削弱**(無群組結構),是 BLOCKER-A 的直接材料。註:它無法直接叫「辦公室」(保留名擋住),必須配合上面的隱形字元才能做出完整的兩列相同。 |

### 判 NIT 的

| 名稱 | 判定 | 理由 |
|---|---|---|
| `nbsp_only` (只有 U+00A0) | **NIT** | 渲染成完全空白的一列。不冒充任何主題,只是難看/難指認,仍可選取與刪除。`round3-fix-report.md` 已明列為刻意取捨,我同意。 |
| `ideographic_space_only` (只有 U+3000) | **NIT** | 同上。 |
| `line_sep_u2028` / `para_sep_u2029` | **NIT** | 渲染成 `Mid night`,不像任何內建名,無仿冒價值。列 NIT 只因它們是 Zl/Zp 控制類字元,語意上屬「不該出現在單行名稱裡」,且在 `<option>` 的換行行為跨瀏覽器不一致。無實際危害。 |
| `len_80_astral`(80 個 emoji) | **NIT** | 兩端都以碼位計長,判決一致(這正是要驗的點)。後果純屬版面:選單/設定列被撐寬或被截斷。80 emoji = 320 bytes UTF-8,遠低於任何儲存疑慮。**不是**問題,列 NIT 僅記錄「長度上限以碼位而非顯示寬度計」這個已知取捨。 |

### 判「可接受」的(明確認定沒問題)

| 名稱 | 判定 | 理由 |
|---|---|---|
| `builtin_dotted_I` 「OFFİCE」 | **可接受** | 這是 SHOULD-6 **刻意**換來的兩端一致性,方向正確。且它根本不冒充內建:內建顯示名是「辦公室」(zh)或「Office」(en),沒有一個是全大寫的「OFFICE」,更不是帶點的「OFFİCE」。İ 的上點在任何字型下都可見。 |
| `builtin_fullwidth` 「ＯＦＦＩＣＥ」 | **可接受** | 全形字寬約為半形兩倍,與 `Office` 視覺差異極大,不可能誤認。 |
| `builtin_kelvin` 「OFFICEK」(U+212A) | **可接受** | 字串本身就不是內建名(內建是 `Office`,不是 `OFFICEK`)。此案例真正驗的是「沒有做 NFKC 折疊」,而不做 NFKC 是對的 —— 折得更多只會拒得更多,不會漏掉仿冒。 |
| `nfd_office` 「Offíce」(NFD) | **可接受** | 帶銳音符,渲染成 `Offíce`,與 `Office` 明顯不同(多一個可見的重音)。附帶記錄:全程沒有 NFC 正規化,所以兩個只差正規化形式的名字可以並存 —— 但兩者都不等於內建名,無仿冒後果。 |
| `plain_ascii`, `cjk`, `korean`, `arabic_plain`, `hebrew_plain`, `emoji_simple`, `emoji_vs16_only`, `len_80_bmp`, `new_theme_zh`, `new_theme_en`, `ideographic_space_office` | **可接受** | 正常合法名稱,本來就必須收(不誤擋)。`ideographic_space_office`「　Office　」嚴格說與 `ideographic_space_pad` 同類,但它渲染成 `Office` 而非「辦公室」,危害等級同 `nbsp_pad_office`,已含在上面的 SHOULD-C 裡一併處理。 |

**小結**:29 個 ACCEPT 裡,**沒有一個**造成「產出產品自己拒收的檔案」;
**8 個**(7 個隱形/空白 + spoof_builtin_marker)真的具備誤導能力,構成 SHOULD-C 並支撐 BLOCKER-A;
**4 個**只是難看(NIT);其餘 **17 個**是本來就該收的正常名稱或刻意換來的一致性成本,**可接受**。
判準上我特意沒有把 `OFFİCE` / 全形 / NFD 這類「長得有點像」的一律喊嚴 ——
它們渲染出來就是不一樣,喊嚴只會把 SHOULD-6 好不容易換到的兩端一致性又賠掉。

---

## 5. 既有行為回歸

我自己跑了一次完整 `bash bin/ci.sh` → **`[ci] all green`**(exit 0):

- `go test ./...`:`ok ocserverd 49.152s` / `ok ocwarden 35.882s`
- frontend:`Test Files 164 passed (164)` / `Tests 1305 passed (1305)`,`tsc --noEmit` 綠
- Playwright CT 視覺守衛:`142 passed (33.5s)`
- `[token-roles] ok — … 4.52:1 vs --color-on-danger / 3.76:1 vs --color-bg (its 1px ring)`
- 四道產生器 drift gate(theme-token / message-key / font / contract / gen-ocapi)全部無 drift
- conformance:`975 passed in 16.00s`,`[conformance] all green`

回歸面的具體確認:

- **內建深色主題的外觀變化只有未讀徽章**。三顆徽章加的是 `outline`,不進入佈局(見 §1「沒問題的部分」);
  `git diff` 在 CSS 上的其餘改動都是 token 換名(`--color-overlay` → `--color-on-*` 等),
  而那些 token 在內建深色主題裡預設 `var(--color-overlay)`,計算值不變 ——
  這也正是 `check-token-roles.mjs` 註解裡說明的「預設刻意 alias 母 token」的設計。
  142 個 Playwright 視覺守衛全綠佐證了沒有其他外觀位移。
- **既有主題包仍能匯入並生效**:`docs/T-081b-evidence/legacy-pack-compat-report.txt` 的既有證據 +
  本輪 conformance 975 全綠。拆分後的 token 預設 alias 母 token,所以一個只覆寫 `--color-overlay`
  的舊淺色包仍會同時帶動所有拆出來的子 token(這正是不用字面值當預設的理由)。
  本輪未發現此路徑有新問題。
- 需要注意的是 §BLOCKER-A / §SHOULD-C 都**不是**回歸 —— 它們是本輪修法留下的缺口,不影響既有行為。

## 6. 工作區還原確認

所有實驗都已還原:

- 守衛繞法(A–G)全程在 `frontend/src` 的 **temp 複本**上進行,透過 `TOKEN_ROLES_SRC` 指過去,
  真實工作區從未被寫入。
- 5 個紅證抽驗改的是真實檔案(`ProfileDropdown.tsx`、`check-token-roles.mjs`、`chrome.css`),
  每次都以改動前的 `cp` 備份還原。
- 一次性 probe 檔 `frontend/src/lib/__round4probe.test.ts` 已 `rm`。

結束確認:

```
$ git status --porcelain | wc -l
71                       # 與審查開始時相同(60 modified + 11 untracked)

$ git diff | shasum -a 256
11607ba83d783392ff2c5a2551d8b2dc91c222a48e5836caeb8e21a1058a6b86
```

**tracked diff 的 sha256 前綴仍為 `11607ba83d783392`,與審查版本快照一致 —— 工作區已還原乾淨,無未還原改動。**
本輪新增的檔案只有三個文件檔:`docs/T-081b-evidence/review-round4.md`、
`round4-review/guard-bypass-probe.md`、`round4-review/redproof-round4.md`
(均在未追蹤的 `docs/T-081b-evidence/` 之下,不影響 tracked diff)。
`round4-review/` 既有的四個檔未刪未改。

---

## 7. 總結

**BLOCKER ×1**
- **BLOCKER-A** — BLOCKER-2 的「兩列一模一樣的『辦公室(內建)』」在設定頁主題清單原封重現:
  該清單沒有 optgroup,只有一顆 chip,而 chip 的**文字**(`settings.themeBuiltinTag`/`themeCustomTag`,
  仍在可覆寫白名單)、**顏色**(`--color-seg-fill`/`--color-icon-violet-bg`,皆為可覆寫 token)、
  以及**主題名**(U+3000 包夾的「　辦公室　」被 ACCEPT)三者全部由主題包控制。

**SHOULD ×3**
- **SHOULD-B** — `check-token-roles.mjs` 仍有 5 條實測可繞的路徑(`:root:root`、別檔的 `:root`、
  複合選擇器、selector list、`outline-color` longhand),其中兩條正是「只認 `:root`」這個修法造成的新盲區。
- **SHOULD-C** — 隱形字元用手列碼位黑名單而非 Unicode 類別,漏掉 U+00AD/U+180E/U+E00xx/U+2028-9/
  U+00A0/U+3000/U+1680,使「自訂主題不得顯示成內建主題名」這條規則實質可繞。
- **SHOULD-D** — `frontend/scripts/check-token-roles.test.ts` 完全在 `tsc` 範圍外
  (`tsconfig.json` 的 `include` 只有 `["src"]`),且根因是 repo 沒有 `@types/node`,
  單改 include 無法修好;CI 綠而編輯器紅的靜默落差需要處理或至少在檔內註明。

**NIT ×1**
- **NIT-E** — 全空白名稱(僅 U+00A0 / 僅 U+3000)被收下,渲染成看不見的一列。已被實作者列為刻意取捨,
  且會隨 SHOULD-C 的修法自動消失。

**整體評價**:八條修正沒有一條是假的 —— 我親手抽驗的 5 個切面全部真的會紅,
`bin/ci.sh` 全綠,兩端名稱判決 57/57 同構。第 4、6、7-8 條**乾淨成立**,
第 1、2、3 條**方向對但只走完一半**:各自解決了被點名的那個具體實例,卻沒有處理實例背後的那一類
(黑名單 vs 類別、一個介面 vs 兩個介面、三個繞法 vs CSS 優先序本身)。
第 5 條機制做得好,但沒把最該保護的兩顆 key 放進去 —— 那個遺漏直接構成本輪唯一的 BLOCKER。
