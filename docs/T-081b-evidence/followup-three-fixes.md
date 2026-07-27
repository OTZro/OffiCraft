# T-081b 追加三項修正 — 做法、實測數值與「測試會紅」證據

分支 `feat/T-081b-theme-token-split`（base `origin/main@8545b8e`），全部未 commit。

---

## (a) 未讀徽章對比不足

### 做法

新增 `--color-danger-badge: #ba5953`（`frontend/src/styles/theme.css`），**只**當未讀徽章的底色。
`--color-danger` 一個字都沒動——它同時是錯誤文字、登出列的**前景**色，把它壓深會讓那些字在
深底上更難讀；底色與前景色對深淺的要求相反，所以各自一槽（與本票 `--color-overlay` /
`--color-shadow` / `--color-indigo` 的拆槽理由同構）。

三處使用點（以 grep 確認，非依行號）：

| 檔案 | 選擇器 |
|---|---|
| `frontend/src/components/chrome.css` | `.nav-tab__badge` |
| `frontend/src/components/office.css` | `.office__tab-badge` |
| `frontend/src/components/office.css` | `.member-card__unread` |

`--color-on-danger`（白，`var(--color-overlay)`）維持不動，現在壓在新槽上。
新 token 進了 `theme.css` → `npm run gen:tokens` 已把它掃進前後端白名單
（`themeTokens.generated.ts` 72 個 token、`theme_colornames_gen.go`），並在
`lib/themeTokenMeta.ts` 補了主題編輯器的友善名稱（`未讀徽章底` / `Unread badge fill`）。

### 實測數值（WCAG 相對亮度公式，直接計算）

| 前景 / 背景 | 比值 | 判定 |
|---|---|---|
| `#ffffff` on `#f0736b`（原 `--color-danger`） | **2.85:1** | ✗ 低於 AA 4.5:1 |
| `#ffffff` on `#ba5953`（新 `--color-danger-badge`） | **4.52:1** | ✓ 過 AA |
| `#ba5953` vs `#191c24`（`--color-bg`） | **3.76:1** | ✓ 仍看得出是一顆藥丸 |
| `#f0736b` vs `#191c24` | 5.98:1 | （原本徽章對頁面的對比，供對照） |

**外觀確實會變**：三顆未讀徽章的紅由亮轉深。這是 owner 明確要的。

### 測試

放在 `frontend/scripts/check-token-roles.mjs`（`npm run lint:token-roles`，`bin/ci.sh` 4b 之前
已經在跑）。**為什麼不是 vitest**：這個檢查必須讀 `theme.css` 的字面宣告值，而
(1) vitest 的 jsdom 不解析 `var()`，(2) `import "...css?raw"` 在 vitest 下回傳空字串（實測
`LEN string 0`），(3) `frontend` 沒有 `@types/node`，測試檔 `import "node:fs"` 過不了
`tsc --noEmit`。而 `check-token-roles.mjs` 本來就是「T-081b 拆出來的槽要各守一個角色」的
不變式閘門，且已有完整的 CSS brace-aware parser + token graph 展開——對比下限正是同一類
不變式，放這裡是最少新機制的做法。

三條斷言：白字對徽章底 ≥ 4.5:1、徽章底對頁面底 ≥ 3:1、三個選擇器都必須畫
`var(--color-danger-badge)`。

綠燈輸出（含實測值）：

```
[token-roles] ok — 3 split tokens keep to one role each; 7 carved-out tokens defined
independently and in use; unread badge 4.52:1 on text / 3.76:1 on page.
```

### 「會紅」實測

```
### RED (a)-1: --color-danger-badge 退回 #f0736b
  styles/theme.css:114  :root { --color-danger-badge: #f0736b
      --color-danger-badge vs --color-on-danger is 2.85:1, below the 4.5:1 floor
      — the unread count on the pill fails WCAG AA.
exit=1

### RED (a)-2: .member-card__unread 退回 var(--color-danger)
  components/office.css:379  .member-card__unread { background: var(--color-danger)
      .member-card__unread is not painted with --color-danger-badge; white on
      --color-danger is 2.85:1 and fails WCAG AA.
exit=1

### GREEN after restore
[token-roles] ok — ... unread badge 4.52:1 on text / 3.76:1 on page.
```

---

## (b) 主題名稱驗證太鬆

### 做法

前後端孿生，字對字同構：

* `frontend/src/lib/themeBundle.ts` — `hasBidiFormatChar()` + 既有的 `hasControlChar()`，
  以及 `isBuiltinThemeName()`。
* `server/ocserverd/theme_bundle.go` — `hasInvisibleNameRune()`（`unicode.IsControl` +
  `bidiFormatRunes`）與 `isBuiltinThemeName()`。

擋掉的字元：控制字元（Cc：U+0000–U+001F、U+007F–U+009F，沿用既有 `hasControlChar` /
`unicode.IsControl` 那一套）＋ 方向格式字元 U+202A–U+202E、U+2066–U+2069、U+200E、U+200F。

**內建顯示名由既有來源推導，沒有第二份手寫清單**：

1. `frontend/scripts/gen-message-keys.mjs` 現在同時讀 `locales/en.ts` 與 `locales/zh.ts`，
   把 T-081b 已經用來「排除覆寫」的同一個 `themeIdentity` 子樹**反過來當資料輸出**：
   * `messageKeys.generated.ts` → `THEME_IDENTITY_NAMES`
   * `server/ocserverd/message_keys_gen.go` → `themeIdentityNames`

   ```go
   var themeIdentityNames = map[string][]string{
       "newTheme": {"New theme", "新主題"},
       "office":   {"Office", "辦公室"},
   }
   ```

2. 兩邊的驗證器各自把它與**自己的** reserved id 集合（`RESERVED_THEME_IDS` /
   `reservedThemeIDs`）取交集。這一步有兩個作用：
   * 產生器不需要知道誰是內建 → 規則掛在結構上，未來多一個內建主題只要把名字放進
     `themeIdentity`、id 放進 reserved 集合就自動生效；
   * `themeIdentity.newTheme`（「新主題」/「New theme」）**不會**被誤擋——那是新建自訂主題的
     預設名，擋掉會讓 app 自己的新建流程壞掉。

比對用 trim + case-fold（Go `strings.ToLower(strings.TrimSpace(...))`），所以
`Office` / `office` / `  OFFICE  ` / ` 辦公室 ` 全部擋下。與語言無關：兩種語言的拼法都在
同一份 map 裡。

沒有動 `bin/ci.sh`：新資料寫進**既有的**兩個 generated 檔，所以 4b2 的 drift gate 原封不動
就涵蓋了它們（改了 `zh.ts` 的內建主題名而沒跑 `gen:msgkeys` → CI 紅）。

### ⚠️ 一個必須讓 owner 知道的副作用

設定›主題的「內建列下載」是用 `t.themeIdentity.office`（也就是「辦公室」）當匯出檔的 `name`。
新規則生效後，**那個檔原封不動再匯入會被擋**，錯誤訊息是
`name "辦公室" is reserved for a built-in theme`，使用者要先改名。

這其實正是規則要防的情境（匯回去就會出現兩列「辦公室」），所以我**照 owner 指示保留擋下**、
沒有去改匯出的檔名或既有匯出行為，並把它釘進
`frontend/src/lib/themeExport.test.ts` 的
`downloads the built-in under its own NAME, which import then asks to be changed`，讓這個
取捨是白紙黑字的決定而不是驚喜。若 owner 想要「下載→直接匯入」可用，那要另外決定匯出時用
什麼名字（會是新的使用者可見字串，本次沒做）。

### 測試

* 前端：`frontend/src/lib/themeBundle.test.ts`，延伸既有的
  `describe("validateThemeBundle")`（同一個受測函式不另開 describe），三個 case：
  控制/方向字元被擋（11 種，全部寫成 `\uXXXX` escape，因為這些字元是隱形的）、內建顯示名
  被擋（5 種拼法）、合法名稱不被誤擋（中日韓 `精靈村` `深海の夜` `밤하늘`、emoji
  `🌙 Midnight 🌙`、空白與標點 `Mid night — v2 (beta)!`、以及 `新主題` / `New theme` /
  `Officescape` / `辦公室的夜`）。
* 後端：新增 `server/ocserverd/theme_bundle_test.go`（鏡射 source 檔名的既有慣例），
  `TestValidateThemeBundles` 三個 subtest（與前端逐條對應）＋ `TestIsBuiltinThemeName`
  ——後者釘的是**推導本身**：每個 reserved id 都必須在 `themeIdentityNames` 裡有名字
  （否則守門會對它靜默放行），而不在 reserved 集合的 id 必須保持可用。

### 「會紅」實測

前端（`npx vitest run src/lib/themeBundle.test.ts`）:

```
### RED (b)-FE-1: 拿掉控制字元/方向字元檢查
   × validateThemeBundle > rejects a name carrying control or bidirectional formatting characters
     → .toMatch() expects to receive a string, but got object      (回傳 null = 沒擋)
      Tests  1 failed | 39 passed (40)

### 還原後轉綠
      Tests  40 passed (40)

### RED (b)-FE-2: 拿掉內建顯示名檢查
   × validateThemeBundle > rejects a name that claims the built-in theme's display name
      Tests  1 failed | 39 passed (40)

### 還原後轉綠
      Tests  40 passed (40)
```

後端（`go test -count=1 -run 'TestValidateThemeBundles$|TestIsBuiltinThemeName' .`）:

```
### RED (b)-GO-1: 拿掉 hasInvisibleNameRune 檢查
    --- FAIL: .../rejects_a_name_carrying_control_or_bidirectional_formatting_characters
        theme_bundle_test.go:37: name "Mid\x00night" must be rejected, got <nil>

### RED (b)-GO-2: 拿掉 isBuiltinThemeName 檢查
    --- FAIL: .../rejects_a_name_that_claims_the_built-in_theme's_display_name
        theme_bundle_test.go:50: name "辦公室" must be rejected, got <nil>

### RED (b)-GO-3: isBuiltinThemeName 改成不分 reserved（連 newTheme 一起擋）
    --- FAIL: .../accepts_every_legitimate_name_shape,_including_the_new-theme_default
        theme_bundle_test.go:73: name "新主題" must be accepted, got custom_themes[0]:
        name "新主題" is reserved for a built-in theme
    --- FAIL: TestIsBuiltinThemeName
        theme_bundle_test.go:102: "New theme" is themeIdentity.newTheme, not a built-in
        theme's name — it must stay claimable by a custom theme

### GREEN after restore
ok  	ocserverd	0.323s
```

第三個紅燈特別重要：它證明「與 reserved id 取交集」這一步不是裝飾——拿掉它就會把
`新主題` 一起擋死。

---

## (c) 主題清單看不出哪個是內建

### 做法

* **設定›主題的清單**（`ThemeSettings.tsx`）本來就已經在內建列畫
  `<span className="ts-tag">{t.settings.themeBuiltinTag}</span>`、自訂列畫
  `ts-tag--custom` + `themeCustomTag`。這一項缺的是**測試**，已補上。
* **ProfileDropdown 的主題選擇器**是一個 `<select>`，`<option>` 吃不下標記元素，所以標記走
  文字：新增 composed message `themeBuiltinOption`（`i18n/compose.ts`）

  ```ts
  themeBuiltinOption: (name) => `${name}${sp}(${set.themeBuiltinTag})`,
  ```

  zh → `辦公室(內建)`、en → `Office (Built-in)`。

**沒有引入蓋不到的字串**：唯一的詞彙片段是**既有的、可覆寫的** `settings.themeBuiltinTag`
（zh「內建」/ en「Built-in」，兩種語言本來就都有），括號是 join（標點）不是詞彙，主題名本身
刻意**不是**片段——那是主題的身分，wording 覆寫不准碰（T-081b §6）。
`compose.test.ts` 的「每個 composed message 都要有」與「這些片段必須可覆寫」兩張表都已補上
對應項目，兩種語言的預期輸出都寫死在表裡。

### 測試

* `frontend/src/components/ProfileDropdown.settings.test.tsx` — 延伸既有的
  「keeps only the theme SELECTOR」：先用 `api.patchServerSettings` 種一個自訂主題「午夜藍」，
  再斷言 `value="office"` 那個 option 的文字**等於** `makeMessages(zh,"zh").themeBuiltinOption(...)`
  且含「內建」，而 `value="midnight"` 那個 option 的文字**就是**「午夜藍」——標記落在正確
  的那一列。
* `frontend/src/components/ThemeSettings.test.tsx` — 延伸既有的
  「imports a pasted bundle, lists it, ...」：找出 `.ts-row`，內建列必須含「內建」，自訂列必須
  含「自訂」且**不含**「內建」。

### 「會紅」實測

```
### RED (c)-1: ProfileDropdown 拿掉內建標記
   × ProfileDropdown · preferences scope > keeps only the theme SELECTOR ...
     AssertionError: expected '辦公室' to be '辦公室(內建)' // Object.is equality

### RED (c)-2: ThemeSettings 內建列拿掉 ts-tag
   × ThemeSettings · import > imports a pasted bundle, lists it, and lands it on the server
     AssertionError: expected '辦公室' to contain '內建'
      Tests  1 failed | 14 passed (15)

### GREEN after restore
 ✓ src/components/ProfileDropdown.settings.test.tsx (8 tests)
 ✓ src/components/ThemeSettings.test.tsx (15 tests)
      Tests  23 passed (23)
```

---

## 收尾閘門

| 閘門 | 結果 |
|---|---|
| `cd frontend && npx tsc --noEmit` | 乾淨 |
| `cd frontend && npx vitest run` | 163 files / 1293 tests passed |
| `cd server/ocserverd && gofmt -l .` | 無輸出 |
| `cd server/ocserverd && go vet ./...` | 通過 |
| `cd server/ocserverd && go test ./...` | `ok ocserverd` |
| `bash bin/ci.sh` | `[ci] all green`（log：`scratchpad/fix3-badge-name-tag/ci.log`） |

產生器成套跑過：`npm run gen:tokens && npm run gen:msgkeys && npm run gen:api`。
spec 沒動，所以沒有跑 `bin/gen-ocapi`。

### 觀察到的既有 flake

跑完整 vitest 時，`src/components/ChatArea.gallery.test.tsx` 曾偶發失敗一次
（`renders a member-sent image as a thumbnail and file as a chip, token-authed`），
重跑即綠，與本次三項修正無關（沒碰 ChatArea 或附件路徑）。
