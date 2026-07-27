# T-081b — 獨立審查 round 2 · lens B(安全 / 使用者可見行為 / i18n / 可近用)

## 版本快照

| 項目 | 值 |
| --- | --- |
| `git rev-parse HEAD` | `8545b8e117d9553bfa27a90f2c51819ea375084b`(base，全部改動未 commit) |
| 開始時間 | 2026-07-27 17:02:18 CST |
| 開始時 `git diff \| shasum -a 256` 前 12 碼 | `b52736477b30` |
| **收尾時間** | 2026-07-27 17:18:20 CST |
| **收尾時 `git diff \| shasum -a 256` 前 12 碼** | `af5767eb58d9` |

> ⚠️ **工作區在我審查期間持續被改動**（雜湊 3 次變化：`b52736477b30` → `589b011435a4` →
> `64c070eb7534` → `af5767eb58d9`）。`frontend/src/i18n/index.tsx`、`styles/global.css`、
> `styles/theme.css`、`components/chrome.css`、`lib/themeBundle.ts` 的 mtime 落在 17:09–17:17，
> 也就是在我讀完它們之後。**D 系列發現是這個移動標的在 17:18 快照下的實測結果**；若實作者
> 已在其後的編輯中處理，請以覆核為準，但請注意快照當下產品是**紅燈**（見 D-0）。
> A/B/C 系列(注入面、寬容政策、i18n、對比度)在整段期間內容未變，結論穩定。

---

## 一句話總評

**注入面我攻不進去(23 種構造全數被擋，client/server 逐條一致)，寬容政策也確實只裁到用詞這一
案；但 (1) 為了修 round-1 SF-5 而新增的 server log 追蹤本身可被主題包偽造，(2) 新增的外框背景
圖在「寬版」偏好下 100% 看不見而 UI 完全沒提示，(3) 快照當下 `sides` 語意剛被改掉且新增了
第三個模式 `cover`，前端/後端/wire spec/i18n 四邊不同步、測試紅燈 —— 以此快照不可 land。**

---

## 分級清單

| # | 級別 | 一句話 |
| --- | --- | --- |
| D-0 | **BLOCKER** | 快照當下 `vitest` 紅燈 2 條；`cover` 模式只加在 TS 白名單，Go / openapi / i18n / UI 四邊都不認得 |
| D-1 | **BLOCKER** | `sides` 的實際行為(no-repeat + fixed + bottom)與凍結 wire spec、Go 註解、theme.css 註解、**兩個語系的使用者可見說明**全部相反 |
| B-1 | **SHOULD** | 被丟棄的用詞代碼原封不動進 `log.Printf` → 主題包可**偽造 server log 行**(round-1 SF-5 的修法自帶的洞) |
| C-1 | **SHOULD** | 「寬版」偏好下外框畫布寬度恆為 0 → 背景圖完全看不見，UI 提示只寫「手機版不受影響」 |
| C-2 | **SHOULD** | 交付項 6「寬版保留左右外區」實際已被 owner 當日撤回、樹上沒有；只在 CSS 註解與 mapping 文件裡有紀錄 |
| B-2 | **SHOULD** | 主題名稱 `name` 不檢查控制字元/雙向覆寫字元，且可自稱「辦公室」；`ProfileDropdown` 的下拉沒有「內建」標記 |
| B-3 | NIT | 匯入警告把攻擊者提供的代碼字串原樣(無長度上限、無字元過濾)渲染到畫面 |
| B-4 | NIT | `applyWording()` 自身不查白名單 —— 主題身分的防線只有 validator 一層(縱深不足) |
| C-3 | NIT | `importSkipped` 警告在離開/返回列表後仍殘留;英文版警告無測試 |
| C-4 | NIT | 外框底圖的預覽用 `UserIcon`(人像) 當佔位圖;英文警告尾巴 `…` 脫離逗號序列 |
| — | CLEAN | 注入面(23 構造)、寬容政策邊界、themeIdentity 修復、對比度無回歸 —— 逐項實跑，見「查證為乾淨」 |

---

## BLOCKER

### D-0 — 快照當下產品是紅燈，且 `cover` 模式只做了四分之一

```
$ cd frontend && npx vitest run
 Test Files  1 failed | 162 passed (163)
      Tests  2 failed | 1283 passed (1285)
 FAIL  src/lib/themeBundle.test.ts > validateBackgroundModes > rejects a non-object, an unknown zone and an unknown mode
 FAIL  src/lib/themeBundle.test.ts > validateBackgroundModes > flows through validateThemeBundle
```

原因：`frontend/src/lib/themeBundle.ts:90` 在 17:15–17:17 之間被改成

```ts
export const BACKGROUND_MODES = ["tile", "sides", "cover"] as const;
```

而同一個快照裡：

| 應同步的地方 | 實測 | 後果 |
| --- | --- | --- |
| `server/ocserverd/avatar_bundle.go:231` | `map[string]bool{"tile": true, "sides": true}` | client 收、server **422** —— 「離線拒收 ⇔ 線上拒收」的孿生保證斷了 |
| `spec/openapi.json:5348` | 只寫 `tile` / `sides`(closed set of two) | 凍結 wire 契約與實作不符 |
| `frontend/src/i18n/locales/{zh,en}.ts` | 沒有 `themeCanvasBgModeCover` | 兩語系都沒有這個選項的字 |
| `frontend/src/components/ThemeSettings.tsx:906-912` | `mode === "tile" ? …Tile : …Sides` | 下拉會出現**兩個字面相同的「貼邊」選項** |
| `frontend/src/i18n/index.tsx:257` | `const sides = …?.canvas === "sides"` | 選了 `cover` 實際渲染成 `tile` |

重現：`grep -n "BACKGROUND_MODES = " frontend/src/lib/themeBundle.ts`；
`grep -n "backgroundModeAllowed = " server/ocserverd/avatar_bundle.go`；
`grep -n "BACKGROUND_MODES.map" -A 8 frontend/src/components/ThemeSettings.tsx`。

`bin/ci.sh` 在 17:05–17:10(舊快照)是 `[ci] all green`；現快照未再跑全套，但單跑 vitest 已紅。

### D-1 — `sides` 的行為改了，四份「說明」沒跟著改(其中兩份是使用者看得到的字)

17:09 之後 `frontend/src/i18n/index.tsx` 的 `sides` 分支變成：

```ts
root.style.setProperty("--canvas-bg-repeat",     sides ? "no-repeat, no-repeat"       : "repeat");
root.style.setProperty("--canvas-bg-position",   sides ? "left bottom, right bottom"  : "0 0");
root.style.setProperty("--canvas-bg-attachment", sides ? "fixed, fixed"               : "scroll");
```

⇒ 現在是「**兩軸都不重複、貼視窗底、隨視窗固定**」。但仍宣稱「只縱向重複 / repeat-y」的地方：

| 檔案:行 | 現有文字 |
| --- | --- |
| `spec/openapi.json:5348` | “pin one copy … each repeating vertically (`repeat-y`) at its natural size” |
| `server/ocserverd/avatar_bundle.go:227` | “pins one copy against each viewport edge, **repeating vertically only**” |
| `frontend/src/styles/theme.css:18` | 「各貼一邊、**只縱向重複**」 |
| `frontend/src/i18n/locales/zh.ts:1044` | 「貼邊 — 左右各貼一張,**只向下重複**」 ← **使用者在下拉選單看到的字** |
| `frontend/src/i18n/locales/en.ts:973` | “Sides — one copy against each edge, **repeating downwards**” ← **同上** |

只有 `docs/T-081b-token-split-mapping.md:276`(17:12 更新)寫對:「兩軸都不重複」。

這正是 round-1 BLOCKER-2(「凍結 wire spec 說了與行為相反的話」)的**再犯**，而且這次還多了
兩條使用者可見文案。

附帶(疑慮，未證實)：`background-attachment: fixed` 掛在 `body` 上，在 iOS Safari 上歷來有
已知渲染異常，且捲動時會強制重繪整個視窗背景。`docs/T-081b-evidence/canvas-sides.mjs` 我在
17:07 跑過(當時是 repeat-y 版本，`ALL OK`)，**該證據檔已對應不到現在的實作**，需重跑。

---

## SHOULD

### B-1 — 主題包可以偽造 server 的「已丟棄」log 行

`server/ocserverd/wording_bundle.go:112-125` 把被丟棄的**代碼字串本身**直接餵進 `log.Printf`：

```go
log.Printf("[theme] %s: dropped %d unrecognised wording code(s): %s%s",
    where, len(codes), strings.Join(shown, ", "), more)
```

代碼是 JSON 物件的 key，完全沒有長度或字元檢查(白名單比對失敗就直接進 `dropped`)。

**實跑(暫存 `server/ocserverd/zz_lensb_probe_test.go`，已刪除)：**

```go
evil := "a\n2026/07/27 99:99:99 [theme] custom_themes[0]: dropped 0 unrecognised wording code(s): (forged)"
w := map[string]map[string]string{"zh": {evil: "x", strings.Repeat("L", 300): "y"}}
validateWording(&w, "custom_themes[0]")   // → nil (接受)
```

輸出：

```
[theme] custom_themes[0]: dropped 2 unrecognised wording code(s): LLLL…(300 個 L), a
2026/07/27 99:99:99 [theme] custom_themes[0]: dropped 0 unrecognised wording code(s): (forged)
log line count = 2
```

第二行是**偽造的**。這條 log 是這個檔案自己寫的目的所在——註解說「本 repo 的解碼器原則……
所以每次丟棄都留下一條 server log 追蹤」；一條可被被追蹤者任意偽造(以及用 1000×任意長度
代碼灌爆)的追蹤，等於沒有追蹤。`maxLoggedDroppedCodes = 10` 只限筆數，不限每筆長度。

建議：`%q` 引號化 + 單筆長度截斷(例如 80 字元)。不需要改行為，只改輸出。

### C-1 — 「寬版」使用者上傳的外框底圖 100% 看不到，UI 不說

實跑 `node docs/T-081b-evidence/canvas-sides.mjs`(真瀏覽器讀像素)與
`node docs/T-081b-evidence/zonecheck.mjs`：

```
narrow 1920px  gutter 440px/side   ← 看得到
narrow 1440px  gutter 200px/side   ← 看得到
narrow 1040px  gutter   0px/side   (no gutter — invisible)
wide   1920px  gutter   0px/side   (no gutter — invisible)
wide   1440px  gutter   0px/side   (no gutter — invisible)
wide    375px  gutter   0px/side   (no gutter — invisible)
```

即：這個功能只在「**窄版偏好 + 視窗 > 1040px**」時可見。但編輯器裡唯一的提醒是

- zh `settings.themeCanvasBgHint`：「……**手機版沒有左右外框,不受影響**。」
- en：“… **Phones have no side canvas and are unaffected.**”

只點名手機。一個把版面設成「寬版」的 owner 上傳圖之後會看到**完全沒變化**，而畫面上沒有任
何線索告訴他為什麼。這是 lens B 的典型缺陷：驗證通過、儲存成功、畫面無事發生。

建議(擇一)：文案補上「寬版版面沒有外框，看不到此圖」；或編輯器在目前偏好是寬版時就地提示。

### C-2 — 交付項 6「寬版保留左右外區」實際上沒有交付

`frontend/src/components/chrome.css:23-32` 的註解記載：owner 2026-07-27 先要求每側保留 48px，
**同日看過效果後改回吃滿**(「寬版好像真的看不出什麼，不然寬版就不要留白好了」)。樹上
`:root[data-layout="wide"]` 只有 `max-width: none`，實測 gutter = 0(上表)。

⇒ 交派給我的七項清單第 6 項與樹上狀態不符。撤回本身有 owner 依據且被誠實記錄
(`chrome.css` + `docs/T-081b-token-split-mapping.md:222-230`)，**我不認為這是實作缺陷**，
但票面/交付清單需要同步，否則驗收時會對不上。也請注意這個撤回直接造成 C-1。

### B-2 — 主題名稱仍可冒充內建主題，且不擋控制字元/雙向覆寫字元

實跑(TS `validateThemeBundle` / Go `validateThemeBundles`，探針已刪除)：

```
[name] "office"                          => ACCEPT
[name] "辦公室"                            => ACCEPT
[name] "a\nb"                            => ACCEPT
[name] "a b"                        => ACCEPT
[name] "‮gnp.exe"  (U+202E RLO)          => ACCEPT
[name] "<img src=x onerror=alert(1)>"    => ACCEPT
Go: name = "‮office\n"                   => validateThemeBundles(...) = <nil>
```

`themeBundle.ts:470-475` 只查 `runeCount(name.trim())` 落在 1..N，沒有 `hasControlChar`
(用詞值有、名稱沒有)；Go 端同樣。

XSS 沒有(名稱以 React children 渲染，會被跳脫)，所以不是 BLOCKER。但本票 bug #3 的原始症狀
正是「畫面上出現兩列同名主題、找不到回去內建那一列」。修好的是「主題包**改名別人**」這條路，
**「主題包把自己取名叫辦公室」這條路仍然開著**：

- `ThemeSettings.tsx:1046` 內建那列有 `<span class="ts-tag">內建</span>` → **可分辨**
- `ProfileDropdown.tsx:271` 是 `<select>`，內建 option 只有 `{t.themeIdentity.office}`，
  自訂 option 只有 `{b.name}`，**沒有任何標記** → 兩個 `辦公室` option 無法分辨

比原 bug 輕(內建永遠是第一個 option，切得回去)，但同一類風險殘留。建議：`name` 加控制字元/
雙向字元檢查(與用詞值同一把尺)，`ProfileDropdown` 的內建 option 補上與 `ts-tag` 等價的字樣。

---

## NIT

### B-3 — 匯入警告直接渲染攻擊者字串

`ThemeSettings.tsx:1024-1031` → `compose.ts` `themeImportSkipped(count, sample)`。`sample` 是
攻擊者控制的 key，無長度上限、無字元過濾(與 B-1 同源)。實跑：

```
EN2 : Imported, but 2 wording code(s) were not recognised and were skipped: a.b, c.d
ZH2 : 已匯入,但有2個用詞代碼不認得、已略過:a.b、c.d
EVIL: "Imported, but 1 wording code(s) … skipped: ‮xxxxxxxx…(120 字)‭"
```

React 會跳脫，沒有 XSS；問題是版面(單行 banner 被 120+ 字元撐爆)與 U+202E 造成的視覺順序反轉。
`IMPORT_SKIPPED_SAMPLE = 3` 只限筆數。建議每筆截斷。

### B-4 — `applyWording()` 不查白名單，主題身分只有一層防線

`frontend/src/i18n/wording.ts:26-43` 的 `setPath` 只確認「目標是既有的 string leaf」，
**不比對 `MESSAGE_KEYS`**。`themeIdentity.office` 正是一個既有 string leaf，所以只要有任何
一條路徑讓未經 validator 的 overlay 走到 `applyWording`，bug #3 就會復活。

我找過所有實際路徑，**目前沒有可達的繞道**：`api/mock.ts:2567` 寫入前呼叫
`validateThemeBundles`；Go 寫入前 `validateThemeBundles`、讀取時 `settings.go:351-355`
`dropUnknownWordingCodes` 修剪；`customThemes` 不進 localStorage(`grep` 無命中)。
所以列為 NIT / 縱深不足，而非缺陷。round-1 說的「三層獨立防線」指的是「被丟的 key 不會存活」，
不等於「套用層自己會擋」——套用層不會。

### C-3 — 警告殘留與英文版無測試

`importSkipped` 只在 `openImport()`(重開匯入畫面)與下一次匯入時被覆寫。進入「編輯主題」再
返回列表，上一次匯入的警告仍在(`openEdit` 不清)。
另外 `ThemeSettings.test.tsx` 的兩條警告測試都只斷言 zh；英文文案存在但無測試覆蓋。

### C-4 — 外觀小事

- `ThemeSettings.tsx:855-864` 外框底圖的空狀態預覽沿用頭像的 `UserIcon`(人像圖示)，
  對「背景底圖」語意不對。
- 英文警告的省略記號是獨立 token 且前面補空白：`skipped: a, b, c …`(zh 是「a、b、c**等**」)。
  慣例應為 `a, b, c, …`。

---

## 查證為乾淨(逐項實跑，非讀碼推論)

### 1. 注入面 — 23 種構造，全數被擋，client/server 一致

暫存探針(`frontend/lensb-probe.test.ts`，已刪除)對 `backgrounds.canvas` 餵入：

```
[bg] quote-breakout           reject   data:image/png;base64,<png>"),url(javascript:alert(1)
[bg] paren-breakout           reject
[bg] backslash                reject
[bg] newline                  reject
[bg] CRLF-mid                 reject   data:image/png;\r\nbase64,<png>
[bg] svg-plain                reject   data:image/svg+xml;base64,<含 <script> 的 svg>
[bg] svg-mime-png-magic       reject
[bg] png-mime-svg-payload     reject   宣告 png、載荷是 <svg onload=alert(1)>
[bg] uppercase-mime           reject   data:IMAGE/PNG;base64,
[bg] mime-trailing-space      reject   data:image/png ;base64,
[bg] mime-leading-space       reject   data: image/png;base64,
[bg] double-base64            reject   data:image/png;base64;base64,
[bg] charset-param            reject   data:image/png;charset=utf-8;base64,
[bg] unicode-fullwidth-mime   reject   data:ｉmage/png;base64,   (U+FF49)
[bg] whitespace-in-b64        reject
[bg] percent-encoded-quote    reject   …%22
[bg] rtl-override             reject   …U+202E
[bg] url-scheme               reject   url("data:…")
[bg] javascript-scheme        reject
[bg] http-url                 reject   https://evil.example/x.png
[bg] valid-png                ACCEPT   ← 唯一通過的
```

零位元組破口。根因正確且值得記下：**標準 base64 字母表 `[A-Za-z0-9+/=]` 不含引號、括號、
反斜線、分號**，所以 `url("<data-uri>")` 的字串拼接在文法上無法被逃逸；mime 必須
`endsWith(";base64")` 且**全等**比對白名單(不容大小寫、不容參數)；再加 magic byte 交叉比對。
`image/svg+xml` 兩條路(宣告 svg、宣告 png 夾帶 svg)都擋住。

zone key 與 mode 值(白名單路徑)：

```
[bgkey] "canvas" ACCEPT / "topbar" "__proto__" "constructor" "CANVAS" "canvas " "toString" 全 reject
[bgkey] JSON 解析出的真 __proto__ own-property → reject
[mode]  "tile" "sides" ACCEPT / "cover"(當時) "TILE" "tile;background:red" "toString"
        "constructor" ["sides"] {toString:()=>"sides"} null 1 → 全 reject
[mode]  JSON __proto__ key → reject
[mode]  有 mode 無 image → reject
[mode]  backgrounds 是字串時 → 先被 "backgrounds must be an object" 擋下(順序正確)
```

Go 端同構造同結論(`validateBackgrounds` / `validateBackgroundModes` 直接呼叫)：
`bogus zone` → `background zone "bogus" is not allowed`；`cover` → `is not a valid mode`；
quote-breakout → `invalid base64 image data`。

**用詞覆寫路徑**：值以 React children 渲染(`applyWording` → dict → JSX 文字節點)，
全樹 `grep -rn "dangerouslySetInnerHTML" frontend/src` 無命中；值另有控制字元檢查與
1..200 rune 上限。無注入面。

### 2. 寬容政策確實只裁到用詞這一案

```
[lenient] unknown color token        => REJECT
[lenient] unknown font token         => REJECT
[lenient] unknown avatar kind        => REJECT
[lenient] unknown navIcon key        => REJECT
[lenient] unknown background zone    => REJECT
[lenient] unknown wording lang       => REJECT   (只有 code 寬容，語言不寬容)
[lenient] unknown wording code       => accept skipped=["not.a.code"]
[lenient] themeIdentity.office       => accept skipped=["themeIdentity.office"]
[lenient] profile.themeOffice        => accept skipped=["profile.themeOffice"]
```

寬容範圍**沒有外溢**。每語言 1000 筆上限在修剪**之前**量測(TS `themeBundle.ts:423`、
Go `wording_bundle.go:67`)，實測 300 筆 junk 全被丟棄且不影響上限判定；
被丟後 `wording` 物件就地只剩存活 code(`remaining={"zh":{"nav.office":"辦"}}`)，
所以儲存與再匯出都不帶死 code。

`settings.go:344-355` 的**讀取端修剪**方向正確(舊 row 不會被服到前端)，且只修剪不拒收。
(疑慮，未證實：顏色 token 白名單縮小時，`validateThemeBundles` 在讀取路徑仍會直接讓
`loadAuthSettings` 回錯 —— 這是既有行為，不是本票造成，但與本票「白名單縮小不得 brick
settings load」的理由不一致，值得日後一併處理。)

### 3. 主題名稱 bug(#3)確實修好

- `gen-message-keys.mjs:78-91` 以**結構規則**整棵跳過 `themeIdentity` 子樹(非手維 key 清單)。
- 全樹只剩 `t.themeIdentity.office` 一個來源，兩個 picker 都改到位：
  `ProfileDropdown.tsx:271`、`ThemeSettings.tsx:1045/1051/1055/1066/1076`(含三個 aria-label
  與匯出檔的 `name`)。`grep -rn "themeOffice\|themeNewName" frontend/src --include=*.tsx` 只剩測試。
- 實測：帶 `profile.themeOffice` / `themeIdentity.office` 的包**匯得進去**，該 code 被丟，
  內建主題名稱不變、仍可切回(`ThemeSettings.test.tsx` 新增的 elfvillage 案 + 我的探針雙重確認)。
- `nav.office`(場所名)仍可覆寫 —— 分寸正確。

### 4. i18n

- 新字串 **zh / en 兩份都有**且非空：`themeIdentity.{office,newTheme}`、
  `profile.themeImportSkipped{Lead,Mid,More}`、`settings.themeCanvasBg{Section,Hint,,Mode,ModeTile,ModeSides,ModeHint}`。
- 兩語系警告實跑：
  `已匯入,但有2個用詞代碼不認得、已略過:a.b、c.d` /
  `Imported, but 2 wording code(s) were not recognised and were skipped: a.b, c.d`；
  超過取樣數時 zh 加「等」、en 加「…」。**確實列出被略過的項目**(取樣 3 筆 + 總數)。
- 該進白名單的進了(`profile.*` / `settings.*` 產品文案)，**不該進的沒進**
  (`themeIdentity.*` 被結構性排除，`messageKeys.theme-identity.test.ts` 釘住)。
- generator 無 drift：17:05–17:10 的 `bash bin/ci.sh` 全綠(含 gen-theme-tokens /
  gen-message-keys / gen-ocapi / openapi-typescript 四道 drift gate、974 條 conformance)。
  ⚠️ 該次全綠是舊快照；D-0/D-1 之後**必須重跑**。

### 5. 可近用

- 鋪法下拉：`<label htmlFor="ts-canvas-bg-mode">` ↔ `<select id="ts-canvas-bg-mode">`，
  原生 `<select>`，鍵盤可操作，`getByLabelText(s.themeCanvasBgMode)` 在測試中取得到 → 標籤關聯成立。
- 檔案輸入 `.ts-file { display: none }`(`theme-settings.css:181`)，由可聚焦的 `<button>` 代打，
  且帶 `aria-label={t.settings.themeCanvasBg}` —— 與既有頭像/logo 欄位同構，無新退步。
- 預覽 `<img alt="">` 為裝飾性，正確。
- (D-0 若不修，下拉會出現兩個同字面的 option —— 螢幕閱讀器使用者完全無法區分。)

### 6. 對比度 / 可讀性 — 無新的低對比組合

重跑既有量測腳本：

`python3 docs/T-081b-evidence/after_split_proof.py`
→ 淺色主題 8/8 通過；內建深色 7/8，唯一未達標是 `--color-on-danger` #fff on
`--color-danger` #f0736b = **2.85:1**。我獨立核對：拆分前該處是 `--color-overlay`(同為 #fff)
壓在同一個紅底 → **數值完全相同**，屬既有缺口，非本票引入。驗收報告 B 節已誠實揭露。

`docs/T-081b-evidence/{baseline,audit-after}-report.txt` 逐行比對：

| | baseline | after |
| --- | --- | --- |
| 抽出真實配對 | 163 | 146 |
| < 3.0:1 | 2 組 | 2 組 — **同樣兩組**(`--color-on-accent` 1.04 / 1.16，`task-reassign__check--on::after`) |
| ΔL* < 3.0 | 4 組 | 4 組 |
| ΔL* < 1.0 | 2 組 | 2 組 — 同樣兩組(`--color-card`) |

⇒ 驗收報告 D 節「沒有引入任何新的低對比組合」**成立**。

`node docs/T-081b-evidence/zonecheck.mjs` → 窄版 6 寬度 + 寬版 3 寬度：
不填分區 token 時四層同色(既有主題包零變化)、填了則四層分明 —— 全部通過。

### 7. 票面說法 vs 實測 — 明講的落差

| 說法 | 實測 |
| --- | --- |
| 交付項 6「寬版保留左右外區」 | **樹上沒有**，owner 當日撤回；寬版 gutter = 0(C-2) |
| `sides`「只向下重複」(spec / Go / theme.css / zh / en) | 現在是 no-repeat + fixed + bottom(D-1) |
| `BACKGROUND_MODES` 是 tile/sides 兩個的閉集(spec) | TS 已有第三個 `cover`(D-0) |
| 驗收報告「三個色槽必然打架」修正為「只有 `--color-overlay` 成立」 | 我未重跑 636,056 色掃描，**採信**(疑慮，未證實)；A 節結論不影響本 lens |
| `docs/T-081b-evidence/canvas-sides-report.txt` | 產生於 17:00，我 17:07 重跑一致；**但 17:09 實作變更後已失效**，需重跑 |

---

## 我試過但沒找出問題的

- 用引號/括號/反斜線/換行/CR/百分比編碼/全形 unicode/RTL 覆寫去逃逸 `url("<data-uri>")` —— 23 種全敗。
- 用 `image/svg+xml`、宣告 png 夾帶 svg、宣告 svg 夾帶 png magic —— 全被擋。
- `__proto__` / `constructor` / `toString` 當 zone key 與 mode 值(TS Set、Go map) —— 全被擋。
- 讓 `backgroundModes` 在 `backgrounds` 不合法時先被評估 —— 順序正確，先擋 backgrounds。
- 找未經驗證就能到達 `applyWording` 的路徑(localStorage 快取 / mapper / mock PATCH) —— 找不到。
- 在 wording 值裡塞控制字元/超長/空字串 —— 全被擋(1..200 rune、`hasControlChar`)。
- 讓被丟棄的 code 進到儲存 / 再匯出 / 已套用 overlay —— 三處都拿不到。

---

*本報告未修改任何產品碼。三個暫存探針
(`frontend/lensb-probe.test.ts`、`frontend/lensb-msg.test.ts`、
`server/ocserverd/zz_lensb_probe_test.go`)已於使用後刪除;`git status` 已核對無殘留。*
