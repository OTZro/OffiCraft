# T-081b — round 5 修正報告(依 review-round4-recheck.md 的四條定案修法)

分支 `feat/T-081b-theme-token-split`,全部未 commit。
本輪 log 目錄:`docs/T-081b-evidence/round5-fix-run/`。

---

## 1. 名稱驗證:`Zs` 改為「正規化成 U+0020」,不再拒收(SHOULD-3)

**改法(照定案)**:拒收集合只留 `Cc/Cf/Co/Cs/Zl/Zp`;所有 `Zs` 在 trim / 長度 /
保留名比對「之前」先折成 U+0020。兩端同構。

**改了哪些檔**

| 檔 | 用途 |
|---|---|
| `frontend/src/lib/themeBundle.ts` | `INVISIBLE_NAME_CLASS_RE` 拿掉 `\p{Zs}`;新增 `SPACE_SEPARATOR_RE` + `normalizeThemeSpaces()`;`trimThemeName()` 改成「先正規化再 ASCII trim」;`hasInvisibleNameRune` 不再需要 U+0020 例外;錯誤訊息改為 `…control, formatting, private-use, surrogate or line/paragraph separator characters`。 |
| `server/ocserverd/theme_bundle.go` | 同構的 Go 半邊:`invisibleNameCategories` 移除 `unicode.Zs`;新增 `normalizeThemeSpaces()`;`trimThemeName()` 先正規化再 `strings.Trim` ASCII;移除 `plainSpace`;錯誤訊息同字串。 |
| `frontend/src/lib/themeName.cases.json` | 語料 57 → **61**,補 4 條「合法但含非 ASCII 空白」:`ideographic_space_inner`(深海　之夜)、`ideographic_space_inner_many`、`nbsp_inner`(含 U+00A0)、`ideographic_space_pad_custom`(全形空白包夾的非保留名)。 |
| `frontend/src/lib/themeName.parity.test.ts` | 語料下限 57 → 61;Zs 案例的期望判決從「不合法字元」改成各自真正的原因(保留名 / 長度 0);新增 4 條合法案例進 ACCEPT 清單;覆蓋度清單納入新 key。 |
| `frontend/src/lib/themeBundle.test.ts` | 拒收表拿掉 Zs;新增兩段:Zs 包夾的內建名 → `reserved for a built-in theme`,純空白名(`　` / ` ` / ` 　 ` / `  `)→ `name must be 1..`;ACCEPT 表新增四個含非 ASCII 空白的合法名;`normalizeThemeName` 對照表更新(`　辦公室` → `辦公室`、`深海　之夜` → `深海 之夜`)。 |
| `server/ocserverd/theme_bundle_test.go` | 與上表逐條同構的 Go 孿生表。 |

**行為對照(實測)**

| 輸入 | 之前 | 現在 |
|---|---|---|
| `深海　之夜`(U+3000) | REJECT「含不合法字元」 | **ACCEPT** |
| `Deep Ocean` | REJECT | **ACCEPT** |
| `　辦公室　` | REJECT「含不合法字元」 | REJECT **「辦公室 is reserved for a built-in theme」**(可行動) |
| ` Office ` / ` Office` | REJECT「含不合法字元」 | REJECT「reserved for a built-in theme」 |
| `　` / ` ` / `  `(純空白) | REJECT | REJECT「name must be 1..」 |
| `Off­ice`(Cf)、`Mid night`(Zl) | REJECT | REJECT(不變) |

**測試名稱**

- `frontend/src/lib/themeBundle.test.ts`
  - `validateThemeBundle > rejects a name carrying control, formatting, private-use, surrogate or line/paragraph separator characters`(含新增的 Zs→保留名、Zs→長度 0 兩段)
  - `validateThemeBundle > accepts every legitimate name shape, including the new-theme default`(含中日韓/韓文/emoji/阿拉伯文/希伯來文/越南文 + 4 條非 ASCII 空白)
  - `normalizeThemeName > trims ASCII whitespace only and folds A–Z only`
- `server/ocserverd/theme_bundle_test.go`:`TestValidateThemeBundles`(三個 subtest)、`TestNormalizeThemeName`
- `frontend/src/lib/themeName.parity.test.ts`:三個 case 全部維持綠,61 組逐條比對

**實測會紅的證據**(`round5-fix-run/redproof-1-name-normalisation.txt`)

| # | 弄壞什麼 | 結果 |
|---|---|---|
| 1a | TS 端 `normalizeThemeSpaces` 改成 no-op 且 `Zs` 放回拒收集合 | **紅**:`Test Files 2 failed`、`Tests 6 failed | 40 passed` — `themeBundle.test.ts` 的 ACCEPT 表與 parity 兩支同時紅 |
| 1b | 只弄壞 Go 端(拿掉 `unicode.Is(unicode.Zs, r)` 那三行)→ 單邊漂移 | **紅**:Go `TestNormalizeThemeName` fail;parity 逐條印出分歧 `go: ACCEPT / ts: REJECT: name "Office" is reserved…` |
| 1c | TS `trimThemeName` 不做 trim(純空白名會被收下) | **紅**:`Tests 4 failed | 39 passed`,含純空白名那段與 `normalizeThemeName` 對照表 |

還原後 3/3 綠(`=== RESTORED ===`:go `ok`,vitest `46 passed`)。

**偏離定案**:無。
**取捨**:正規化只作用在「驗證/比對」的計算形上,**儲存的仍是使用者輸入的原字串**(這與既有的
ASCII trim 行為一致 — 「  Foo  」一直是原樣存下)。保留名的判定走正規化後的比較形,所以偽造路徑
不受影響。複驗表格裡「存成『深海 之夜』」是說明語意,不是儲存層需求。

---

## 2. 守衛不再模擬 cascade:斷言「每顆受測 token 只有唯一一條 `:root` 定義且不帶 `!important`」(SHOULD-1 / SHOULD-2)

**改法(照定案)**:採審查者的建議一次關掉整類。守衛不再回答「誰會贏」,而是**不允許出現需要
猜的情況**。**寫死的載入順序推導全部拿掉**。

**改了哪些檔**

| 檔 | 用途 |
|---|---|
| `frontend/scripts/check-token-roles.mjs` | 刪掉 `compoundRoot` / `cascadeRank` 與那段基於 `main.tsx` import 順序的排序推導(守衛從未讀過 main.tsx,那是一句寫在註解裡、沒有任何東西驗證的假設)。改為:`MEASURED_TOKENS = [--color-danger-badge, --color-on-danger, --color-bg]` 加上 `aliasClosure()` 沿 `:root` 值的 `var()` 展開,對閉包中每顆 token 斷言 **at-rule 外的 `:root` 定義恰好一條** 且 **不含 `!important`**,違反就列出每一條定義的 `檔:行 { 選擇器 }`。 |
| `frontend/scripts/check-token-roles.test.ts` | 兩條舊的 `:root:root` / 跨檔 `:root` 測試改成斷言新的重複定義訊息;新增三條 |

**保留的既有能力(全部仍有測試且仍綠)**

- 第三輪 `@media print` 排除:`fails when the badge fill drops below AA in :root, however it is patched elsewhere` 與 `still ignores a compliant value parked in an at-rule`(`atRuleFree` 未動)
- 第四輪 selector-list / 複合選擇器:`fails when a selector LIST re-paints a badge`、`fails when a compound selector re-paints a badge`(`targets()` 未動)
- 第四輪 `outline-color` longhand:`fails when the outline-color longhand removes the badge's ring`
- `fails when a badge token is defined outside the theme's :root`

**新增測試名稱**(`frontend/scripts/check-token-roles.test.ts`)

- `fails when a higher-specificity :root:root also defines the badge fill`(改寫)
- `fails when a DEEPER compound :root:root:root also defines the badge fill`(N2)
- `fails when a badge token's :root definition carries !important`(N1)
- `fails on a second :root definition whichever file holds which value`(N3 — 兩個方向都跑,所以守衛不再有任何載入順序假設可以被弄錯)

**實測會紅的證據**(`round5-fix-run/redproof-2-guard-single-root-def.txt`)

把守衛還原成第四輪的 `cascadeRank` 模型(拿掉新斷言、放回排序):

```
[N1-important]     exit=0 :: [token-roles] ok — … 4.52:1 vs --color-on-danger …
[N3-load-order]    exit=0 :: [token-roles] ok — … 4.52:1 vs --color-on-danger …
[N2-deep-compound] exit=1   (我這次的探針把不合規值放在最後一條宣告,舊模型並列後 .at(-1)
                             剛好挑到它;舊模型的答案取決於宣告順序而非歧義本身)
Failed Tests 4:
  fails when a higher-specificity :root:root also defines the badge fill
  fails when a DEEPER compound :root:root:root also defines the badge fill
  fails when a badge token's :root definition carries !important
  fails on a second :root definition whichever file holds which value
Test Files 1 failed | Tests 4 failed | 10 passed (14)
```

還原後:守衛 `exit=0`、`14 passed (14)`。

**偏離定案**:無。
**取捨**:新斷言只涵蓋「受測 token + 其 `:root` 別名閉包」,不是全部 token — 這是定案的字面
範圍(「每一顆受測 token」),也讓一般主題 token 仍可自由多處定義。

---

## 3. 分組標題顏色改讀不可覆寫的 marker 色槽(NIT-1)

**改法(照定案)**:`.ts-group-head` 不再讀可覆寫的 `--color-text-muted`,改讀既有的
marker 族(**沒有新增第五顆**),並納入 `ThemeSettings.test.tsx` 的 CSS 守衛掃描範圍。

**改了哪些檔**

| 檔 | 用途 |
|---|---|
| `frontend/src/components/theme-settings.css` | `.ts-group-head { color: color-mix(in srgb, var(--color-marker-fg) 65%, var(--color-marker-surface)); }` — 兩端都是不可覆寫槽,主題包無法把標題調到看不見。 |
| `frontend/src/components/ThemeSettings.test.tsx` | 既有的 CSS 守衛從「只掃 `.ts-tag` 到 `.ts-icon-btn` 之間」改成用 `blockOf()` 逐一取出 `.ts-tag` / `.ts-tag--custom` / **`.ts-group-head`** 三個宣告區塊一起掃;禁用 token 清單加上 `--color-text-muted`;既有的「區塊裡每個 `var()` 都必須 `^--color-marker-`」結構性斷言範圍隨之涵蓋標題。 |

**測試名稱**:`ThemeSettings · import > cannot be made to show two identical built-in rows by a theme's wording, colours or name`(既有測試,依 CLAUDE.md「先擴充既有測試」擴大掃描範圍,不另開新 case)。

**實測會紅的證據**(`round5-fix-run/redproof-3-group-head-marker-slot.txt`)

把 `.ts-group-head` 改回 `var(--color-text-muted)`:

```
FAIL  src/components/ThemeSettings.test.tsx > ThemeSettings · import >
      cannot be made to show two identical built-in rows by a theme's wording, colours or name
AssertionError: expected '.ts-tag {…' not to contain 'var(--color-text-muted)'
Test Files 1 failed | Tests 1 failed | 17 passed (18)
```

還原後 `18 passed (18)`。

**偏離定案**:無。
**取捨**:marker 族沒有「muted」那一顆,所以標題色是用 `marker-fg`/`marker-surface` 混出來的
近似值 — 內建深色主題下算得 `#9fa0a7`,對照原本的 `--color-text-muted: #9aa0ad`,每個通道差
幾階、肉眼同一個灰。要逐像素相同就得新增第五顆槽,而定案明確要求維持四顆。

---

## 4. 產生器的排除改成顯式清單,並在兩個方向都發出可見錯誤(NIT-2)

**改法(照定案)**:排除**明確列出 token 名**;前綴退居**警戒線**。

**改了哪些檔**

| 檔 | 用途 |
|---|---|
| `frontend/scripts/gen-theme-tokens.mjs` | 新增 `NON_OVERRIDABLE_TOKENS`(四個名字的顯式清單),排除改用它;前綴只用來偵錯。兩個方向都會 `exit 1` 並印出可行動訊息:①theme.css 出現清單沒登記的 `--color-marker-*`(就是 NIT-2 描述的「靜默踢出白名單」),②清單裡的槽從 theme.css 消失。成功時 stdout 明確印出 `excluded as non-overridable marker slots: …`。另加 `GEN_THEME_TOKENS_SRC` / `GEN_THEME_TOKENS_OUT_DIR` 兩個環境變數(僅供測試改指輸入/輸出,沿用 `TOKEN_ROLES_SRC` 的既有慣例)。 |
| `frontend/scripts/gen-theme-tokens.test.ts` | **新檔**,與 `check-token-roles.test.ts` 同一層(repo 既有慣例:`scripts/*.mjs` 的測試同層 `scripts/*.test.ts`)。 |

**測試名稱**(`frontend/scripts/gen-theme-tokens.test.ts`)

- `names the excluded marker slots in its output instead of dropping them quietly`
- `fails when theme.css adds a --color-marker-* token the exclusion list does not name`
- `fails when a listed marker slot disappears from theme.css`

**實測會紅的證據**(`round5-fix-run/redproof-4-generator-explicit-exclusion.txt`)

把只靠前綴的靜默排除放回去,並在 theme.css 加一顆未登記的 `--color-marker-halo`:

```
exit=0  stdout='[gen-theme-tokens] wrote 72 tokens →…'  stderr=''
--color-marker-halo in generated whitelist: False        ← 無聲被踢掉,零錯誤零警告
Failed Tests 3 (三條全紅)
```

還原後 `3 passed (3)`。重跑 `npm run gen:tokens` 無 drift(產物 byte 不變)。

**偏離定案**:無。

---

## 已知取捨(獨立一節)

1. **名稱正規化只作用在計算形,不改儲存**。`validateThemeBundle` / `validateThemeBundles` 仍是純
   驗證器,存下的是使用者輸入的原字串;長度、trim 與保留名比對走正規化後的形。這與既有的
   ASCII trim 行為一致(「  Foo  」一直是原樣存下),偽造路徑不受影響,因為保留名判定用的是
   正規化後的比較形。
2. **分組標題色是近似值**。`color-mix(marker-fg 65%, marker-surface)` 在內建深色主題下算得
   `#9fa0a7`,原本的 `--color-text-muted` 是 `#9aa0ad`。逐像素相同需要第五顆 marker 槽,
   而定案明確不加。142 個 Playwright CT 視覺守衛全綠(見 ci.log)。
3. **守衛的唯一定義斷言範圍**是「受測 token + 其 `:root` 值的別名閉包」,不是全部 token。
   這是定案的字面範圍;一般主題 token 仍可在多處定義。
4. **at-rule 內的定義仍一律不算**(第三輪的決定未改)。所以「`@media screen` 裡藏一顆壞值」
   這一類仍不在守衛的模型內 —— 但它從第三輪起就是明確的設計取捨(at-rule 下的宣告不能當成
   「螢幕的真相」),本輪沒有賠掉、也沒有擴大。
5. **`Mn`(變體選擇符)仍然接受**,與第四輪相同、複驗也同意的取捨,本輪未動。
6. **兩端 Unicode 表的版本落差**仍只由 61 組語料涵蓋,不是全碼位證明 —— 複驗第 3 節已界定
   這道網的保證範圍,本輪未改變它。

---

## CI

```
$ bash bin/ci.sh
[ci] all green            (exit 0)

go:          ok ocagent 0.6s / ok ocwarden 34.5s / ok ocserverd
frontend:    Test Files 166 passed (166) / Tests 1322 passed (1322)   ← 前一輪 165/1317
             (兩份 tsconfig 的 tsc --noEmit 皆綠)
Playwright:  142 passed (34.1s) — 內建深色主題視覺守衛無位移
[token-roles] ok — … 4.52:1 vs --color-on-danger / 3.76:1 vs --color-bg (its 1px ring)
五道產生器 drift gate:全部無 drift
conformance: 975 passed in 15.89s,[conformance] all green
```

**沒有遇到 `useRelocateMachine.test.tsx > drops the 已送出 chip by itself` 的不穩定失敗**(單次完整跑一次過)。

log:`docs/T-081b-evidence/round5-fix-run/ci.log`
(另有 `ci-interim.log` — 修正過程中途的一次完整跑,同樣 `[ci] all green`,保留作參考)
