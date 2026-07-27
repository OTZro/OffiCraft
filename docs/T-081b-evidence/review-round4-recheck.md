# T-081b — 第四輪修正的複驗(round4 recheck)

**複驗版本快照(tracked diff sha256 前綴)**:`dfd611a02fa1c317`
(完整:`dfd611a02fa1c317ed6c86c9256a4c0e09a0e80113b39dd1ff473d53a6f43799`,76 個變動項)

分支 `feat/T-081b-theme-token-split`,全部未 commit。
複驗者即 `review-round4.md` 的作者,未參與本批修正的實作。
實作者自述:`docs/T-081b-evidence/round4-fix-report.md`。
證據:`docs/T-081b-evidence/round4-review/recheck-probes.md`。

**結論:四條修正全部通過複驗(A/B/D 成立,C 有條件成立),沒有新的 BLOCKER。
新增 3 條 SHOULD、2 條 NIT,均不擋出貨。**

---

## 1. A(設定頁偽造)—— **成立**。我再攻了一次,攻不進去。

上一輪我是靠**文字 + 顏色 + 名字**三條路湊出「兩列一模一樣的『辦公室 [內建]』」。
這輪逐條再試,三條全斷:

| 攻擊 | 手法 | 結果 |
|---|---|---|
| **文字** | wording 覆寫 `themeMarkers.builtinGroup`/`customGroup` 對調成「自訂」/「內建」 | **被 drop**。`validateWording` 回 `null`(無錯),但覆寫物件被清空:`AFTER: {"zh":{}}`。上一輪同形狀的 `settings.themeBuiltinTag` 覆寫是**原封保留**的。 |
| **顏色** | bundle 的 `colors` 指定 `--color-marker-custom` / `--color-marker-builtin` | **被拒**:`"--color-marker-custom" is not a theme colour token (see theme.css)`。四顆槽在 `themeTokens.generated.ts` 與 `theme_colornames_gen.go` 中的出現次數皆為 **0**。 |
| **名字** | `「　辦公室　」`(U+3000)、`「辦公室 」`(U+00A0)、`辦公室`+ZWSP、`辦公室`+TAG(U+E0041) | **全部被拒**:`name must not contain control, formatting, private-use, surrogate, separator or non-ASCII space characters` |

舊 key 已徹底消失:`themeBuiltinTag` / `themeCustomTag` 在 `messageKeys.generated.ts` 與
`message_keys_gen.go` 的出現次數皆為 **0**。

### 我另外試的新路徑

| # | 新路徑 | 結果 |
|---|---|---|
| N1 | **分組標題本身可否被覆寫** | 不行。標題讀 `t.themeMarkers.builtinGroup`,整棵 `themeMarkers` 不在可覆寫白名單(次數 0),且 wording 覆寫實測被 drop。 |
| N2 | **`role="group"` 的無障礙標籤** | 乾淨。`aria-labelledby` 指向 `id="ts-group-builtin"` / `ts-group-custom` 的標題節點,標題文字來源同上;`aria-label` 只出現在下載/編輯/刪除鈕上,內容是 `${t.profile.themeExport} ${b.name}` —— 名字進了 aria-label,但那是**該列自己的**名字,不涉及內建/自訂的判定。螢幕閱讀器讀到的分組歸屬與視覺一致。 |
| N3 | **惡意字型**(供一個把「自訂」畫成「內建」字形的字型) | 關死。`validateFonts` 的值是**封閉允許清單**(`SAFE_FONT_FAMILIES`,全是系統既有字族),不接受任意字串、不接受 `url(`/`@font-face`,鍵也限 `--font-sans`/`--font-title`。 |
| N4 | **排序** | 乾淨。全 repo 對 `customThemes` **沒有任何 `sort()`/`reverse()`**;內建列是**寫死**在內建組裡的單一列,自訂主題進不去那個 DOM 區塊。 |
| N5 | **其他出現主題名的畫面** | 只有兩處:`ProfileDropdown.tsx:283`(optgroup 內)與 `ThemeSettings.tsx:1172`(自訂組內)。兩處都已結構化分組。 |
| N6 | **匯出檔** | 乾淨。檔名只由 `id` 推導,名字不進檔名;內建主題匯出用 `id: "office-base"` + 不可覆寫的 `themeMarkers.copyTag`。 |

### 殘餘缺口(判 NIT,不是 BLOCKER)

**NIT-1 — 分組標題的顏色仍是主題可控的。**
`theme-settings.css:67-73` 的 `.ts-group-head { color: var(--color-text-muted); }`,
而 `--color-text-muted` **是**可覆寫的主題色 token(我實測 `validateThemeBundle` 對它回 `null`,通過)。
一個包把它設成等於底色,兩個分組標題就消失。

程式碼註解寫「A heading a theme cannot **claim**」—— 就**文字內容**而言完全正確,
但就**視覺存在**而言,標題是可以被主題調到看不見的。

**為什麼仍判 NIT 而不是 SHOULD/BLOCKER**:
1. 標題消失後,每一列**自己**的 chip 還在,而 chip 的**文字**(themeMarkers)與**顏色**
   (`--color-marker-*`)兩者皆不可覆寫 —— 內建列仍是藍底「內建」、自訂列仍是紫底「自訂」。
   偽造**沒有成立**,只是少了一層冗餘標示。
2. `--color-text-muted` 是全 app 通用的次級文字色,把它設成底色會讓整個介面大量文字消失,
   是極度顯眼的自曝行為,不具隱蔽性。
3. 順帶記錄:`ThemeSettings.test.tsx:183-195` 的 CSS 守衛只掃 `.ts-tag {` 到 `\n.ts-icon-btn` 之間,
   `.ts-group-head`(在 `.ts-tag` **之前**)不在守衛範圍內,所以這個缺口目前沒有測試釘住。
   若要收乾淨,`.ts-group-head` 的 `color` 也應改讀 `--color-marker-fg` 之類的不可覆寫槽。

### 判決

**A 成立**。owner 親自回報過的症狀 —— 「兩列一模一樣的『辦公室 [內建]』」—— 我用上一輪的三條路
加六條新路都做不出來。chip 的文字與顏色雙雙不可覆寫,是這次真正把門關上的東西;
結構化分組是第二道。**沒有第三次重現。**

---

## 2. 產生器「按命名整族排除」的反效果 —— **有條件成立(附 NIT-2)**

作法:`gen-theme-tokens.mjs:48` 的 `NON_OVERRIDABLE_PREFIX = "--color-marker-"`,
`.filter((t) => !t.startsWith(NON_OVERRIDABLE_PREFIX))`。

### 反方向(改名字繞出去)—— **關住了**

若有人把 `--color-marker-builtin` 改名成 `--color-chip-builtin`(theme.css + theme-settings.css 一起改),
它就會重新進入可覆寫白名單,BLOCKER-A 復活。這條**有測試釘住**:
`ThemeSettings.test.tsx:193-195` 斷言 `.ts-tag` 區塊裡**每一個** `var()` 都必須符合 `/^--color-marker-/`。
改名 → 該斷言紅。方向正確,而且是**結構性**斷言(不是列舉四個名字),不會因為新增第五顆槽就過期。

### 正方向(未來有人新增 `--color-marker-xxx` 當一般主題色)—— **NIT-2**

這是這個作法真正的代價:任何未來以 `--color-marker-` 開頭的 token 會被**靜默**排除出白名單。
沒有任何錯誤、警告或測試會提醒 —— 主題編輯器不顯示它、主題包設它會被拒,而開發者只會覺得「怪」。

判 NIT 的理由:
- 命名本身有很強的語意暗示(`marker` = 標示),誤用的機率低;
- `theme.css:166-180` 與 `gen-theme-tokens.mjs:40-47` 都有整段說明;
- 相同的結構性紀律(`NON_OVERRIDABLE_SUBTREES`)已經在 `gen-message-keys.mjs` 用了一輪,是 repo 既有慣例,不是新發明;
- 真要更保險,產生器可以在排除時 `console.log` 印出被排除的名單(現在完全靜默),
  讓 `npm run gen:tokens` 的輸出自己說明發生了什麼。這是低成本的改善,不是缺陷。

**權衡結論**:按前綴排除比「第二份手維清單」好 —— 手維清單會漂移,前綴不會。
代價(靜默排除)比收益(新增標示色自動受保護、兩端不必各維一份)小得多。**方向正確。**

---

## 3. B 的 parity 測試可不可信 —— **成立**。四種讓它靜默變綠的手法全部失敗。

`frontend/src/lib/themeName.parity.test.ts` 由 TS 端 spawn `go test -run ^TestThemeNameVerdictsEmit$`,
Go 端把判決寫成 JSON 檔,TS 端讀回逐條比對。我針對「會不會靜默永遠綠」實際弄壞了四次:

| # | 弄壞什麼 | 預期的危險 | 實際結果 |
|---|---|---|---|
| **S1** | 把 Go 端 `TestThemeNameVerdictsEmit` **改名** | `-run` 比對不到 → `go test` 仍 **exit 0**(「no tests to run」)→ 若程式碼寬容就會靜默略過 | **紅**:`ENOENT: no such file or directory, open '…/verdicts.json'` |
| **S2** | 把 Go 端的守門改成 `if true { t.Skip(...) }`(無條件 skip) | skip 不算失敗,`go test` exit 0 | **紅**:同樣 `ENOENT` |
| **S3** | 把 `go` 執行檔移出 `PATH` | 環境缺 Go 時可能被當成「跳過」 | **紅**:`spawnSync go ENOENT` |
| **S4** | 真實單邊漂移:TS 端從類別集合拿掉 `\p{Cf}`(Go 不動) | 這才是這道網要接的東西 | **紅**,並逐條印出分歧:`go: REJECT: name must not contain… / ts: ACCEPT` |

還原後 3/3 綠。

**為什麼它 fail-closed**:關鍵設計是「Go 端寫**檔**、TS 端**必須讀到**那個檔」,
而且檔案放在**每次執行新建的 `mkdtempSync` 暫存目錄**裡。
所以「Go 沒跑 / 跑了但沒產出 / 產出被快取」三種情況都會變成讀檔失敗,而不是「拿到空結果然後零筆比對通過」。
`-count=1` 擋掉 Go 測試快取,`expect(Object.keys(go).length).toBe(cases.length)` 擋掉半截檔案,
`expect(cases.length).toBeGreaterThanOrEqual(57)` 擋掉語料被悄悄縮水 —— 三道都在,設計是想過的。

### Unicode 版本落差的風險有沒有被接住 —— **部分接住,這是這道網的固有邊界**

接住的:語料裡那 57 個碼位,任一端的 Unicode 表變動導致判決改變,S4 那種形狀立刻紅。
**接不住的**:**不在語料裡**的碼位。兩端的表是兩個 runtime 的獨立資料
(Go 的 `unicode` 套件 vs JS 引擎的 property escapes),Unicode 版本一升,
某個語料沒涵蓋的碼位就可能一端算 `Cf`、一端不算,而這道網不會知道。

這不是實作沒做好 —— 檔頭註解自己就寫明了這件事,而且「窮舉全部 0x10FFFF 個碼位」也不是合理的測試。
但它界定了保證的範圍:**這道網保證的是「這 57 個形狀不會漂移」,不是「兩端的 Unicode 表永遠一致」。**
列為範圍說明,不是缺陷。若要再收緊,成本最低的做法是把兩端各自的 Unicode 版本
(Go 的 `unicode.Version`、JS 的可推導值)也納入比對,版本不同就警告。

---

## 4. C 的新排序邏輯 —— **有條件成立**。5 條舊繞法全堵,但新模型有 3 個新盲區。

### 舊繞法:全部堵住,且沒有賠掉上一輪的修法

| # | 上一輪的繞法 | 這輪 |
|---|---|---|
| R1 | `:root:root`(theme.css) | **exit=1 ✅** |
| R2 | 別檔的 `:root`(global.css) | **exit=1 ✅** |
| R3 | 複合選擇器 `.nav-tab__badge.is-hot` | **exit=1 ✅** |
| R4 | selector list `.nav-tab__badge, .zz` | **exit=1 ✅** |
| R5 | `outline-color` longhand | **exit=1 ✅** |
| R6 | **第三輪 SHOULD-3 C 回歸測**:合規值停在 `@media print` | **exit=1 ✅**(掃描範圍放寬到全樹後,`atRuleFree` 仍守住,沒有把舊修法賠掉) |

`targets()` 的作法是對的:逐一走 selector list 的每一段、取 subject compound、再拆成 simple selector 比對。
這比 `.split(" ").at(-1)` 是**質**的改進,不是再打一個補丁 —— R3/R4 兩種形狀由同一個函式一次解決。

### 新盲區(3 條,實測 exit=0)

排序核心是:
```js
const cascadeRank = (d) => (compoundRoot(d) ? 2 : 0) + (d.rel === THEME ? 0 : 1);
```

| # | 構造 | 守衛 | 螢幕實際 | 未被模型涵蓋的軸 |
|---|---|---|---|---|
| **N1** | theme.css `:root { …: #f0736b !important; }` + global.css `:root { …: #ba5953; }` | **exit=0,印 4.52:1** | **2.85:1** | `!important` 完全不在模型裡 |
| **N2** | theme.css `:root:root:root { …: #f0736b; }` + global.css `:root:root { …: #ba5953; }` | **exit=0,印 4.52:1** | **2.85:1** | `compoundRoot` 只是**二元**判斷,不算真正的 specificity,(0,3,0) 與 (0,2,0) 同級 |
| **N3** | theme.css 放不合規值、global.css 放合規值,**再對調 `main.tsx` 的兩行 import** | **exit=0,印 4.52:1**(與未對調時**逐字相同**) | **2.85:1** | 載入順序是**寫死的推導**,不是讀來的 |

**N3 是三者中最值得處理的**,因為它是**現有前提的靜默失效**,不需要有人寫奇怪的 CSS:

```
$ grep -c "readFileSync.*main" frontend/scripts/check-token-roles.mjs
0                       ← 守衛從未讀取 main.tsx
```

註解寫「Load order is NOT the file-walk order: main.tsx imports styles/theme.css FIRST」——
這句話**是對的**,但它是一句**寫在註解裡的假設**,程式沒有驗證它,也沒有任何測試釘住它。
任何人重排 `main.tsx` 的兩行 import(完全合理的重構,CI 全綠),守衛的答案**不會變**,
但螢幕的真相會反過來 —— 而且不會有任何人知道。

`!important`(N1)則是三者中**最可能真的被寫出來**的:在 token 區塊寫 `!important` 是常見手法。

### 判決與建議

**有條件成立**:我上一輪回報的 5 條確實都關了,每條都有測試釘住,第三輪的修法也沒被賠掉 ——
這批修正本身是紮實的。但這已經是**連續第三輪**在同一支手寫 CSS parser 上追 cascade 的語意
(round3 抓 3 條 → round4 抓 5 條 → 本輪再抓 3 條),而每一輪的修法都是「再多模擬 cascade 的一個軸」。

值得認真考慮換方向。有一個**便宜且 fail-closed** 的選項,不需要真正實作 CSS 引擎:
> 與其「猜哪一條定義會贏」,不如**斷言不存在需要猜的情況** ——
> 要求 `--color-danger-badge` / `--color-on-danger` / `--color-bg` 在整棵樹的 at-rule 外
> **有且只有一條** `:root` 定義,且**不帶 `!important`**;多於一條或帶 `!important` 就直接違規。

這樣 N1/N2/N3 連同所有未來的 cascade 花招一次全關,因為守衛不再需要知道誰贏 —— 它只允許一個候選者。
現行樹本來就滿足這個條件(baseline 就是每顆 token 一條定義),所以改動成本接近零。

---

## 5. D(型別閘)—— **成立**,兩個方向都實測會紅。

```
$ npm run typecheck            # "tsc --noEmit && tsc --noEmit -p tsconfig.scripts.json"
exit=0

# D1 在 scripts/check-token-roles.test.ts 塞型別錯 → 這是上一輪完全不在型別閘內的那支檔案
exit=2
scripts/check-token-roles.test.ts(202,7): error TS2322: Type 'string' is not assignable to type 'number'.

# D2 在 src/lib/themeName.parity.test.ts 塞型別錯(用到 node 內建模組的新檔)
src/lib/themeName.parity.test.ts(155,7): error TS2322: Type 'NonSharedBuffer' is not assignable to type 'string'.

# 還原後 exit=0
```

根因也修對了:`@types/node@^22.20.1` 進了 devDependencies(上一輪我指出「單改 include 不會成功」),
`tsconfig.json` 的 `types` 補上 `"node"`,新增 `tsconfig.scripts.json`(`include: ["scripts/**/*.ts"]`),
且 `build` 改成 `npm run typecheck && vite build` —— **兩份 tsconfig 都在 build 路徑上**,
不是只掛在一個容易被繞過的獨立指令上。避開 project reference(TS6310 與 `noEmit` 衝突)的判斷也正確。

上一輪 SHOULD-D 的「靜默例外」形狀(編輯器紅、CI 綠)已消除。

---

## 6. 既有行為回歸與實作者自承的取捨

### 內建深色主題外觀:**逐像素不變**,已驗證

四顆新色槽的值與它們取代的 token **完全相同**,`color-mix` 算式與百分比原封不動:

| 新槽 | 值 | 原本讀的 token | 值 |
|---|---|---|---|
| `--color-marker-builtin` | `#6076ba` | `--color-seg-fill` | `#6076ba` |
| `--color-marker-custom` | `#8b7ae8` | `--color-icon-violet-bg` | `#8b7ae8` |
| `--color-marker-surface` | `#191c24` | `--color-bg` | `#191c24` |
| `--color-marker-fg` | `#e7e8ee` | `--color-text` | `#e7e8ee` |

所以內建深色主題的計算值一個都沒動。142 個 Playwright CT 視覺守衛全綠佐證。
未讀徽章仍是上一輪確認過的那一處刻意變更。

### 取捨 1:變體選擇符(`Mn`)仍接受 —— **可接受**

實測:`"辦公室"+U+FE0F` → ACCEPT,渲染成「辦公室」。所以「保留內建名」對 `Mn` 這一類仍可繞。

**我同意這個取捨**,理由不只是實作者寫的那些:
- `Mn` 整個類別裝著越南文聲調、希伯來文母音點、天城文組合符、以及 emoji 必用的 U+FE0F。
  擋掉 `Mn` 會大面積誤傷合法名稱 —— 而且測試 `accepts every legitimate name shape` 裡的
  `emoji_vs16_only`(「Heart ❤️」)就是活生生的例子。
- 更重要的是:**保留名檢查本來就不該是最後一道防線**。本輪把防線改成
  「結構化分組 + 不可覆寫的 chip 文字與顏色」,那才是正確的位置 ——
  即使名字長得一模一樣,它仍然掛在「自訂」組、帶著紫色的「自訂」chip。我實測確認了這一點。
- 取捨在報告裡被明白寫出來,不是被藏起來。

### 取捨 2:所有非 U+0020 的空白一律拒收 —— **SHOULD-3(唯一一條我認為該改的)**

這條的安全面我同意,但**執行方式選錯了**,而且代價落在本產品的主要使用族群身上。

實測:
```
"深海　之夜"(全形空白,中文 IME 全形模式按空白鍵的預設產物)
  -> REJECT: name must not contain control, formatting, private-use, surrogate,
             separator or non-ASCII space characters
"深海 之夜"(半形)  -> ACCEPT
"午夜　藍"          -> REJECT
```

**為什麼是問題**:
1. 這是一個**完全正當**的中文主題名。全形空白不是攻擊,是中文輸入法的日常輸出。
2. 錯誤訊息對使用者**不可行動**。一個打了「深海　之夜」的使用者看到
   「must not contain control, formatting, private-use, surrogate, separator or
   non-ASCII space characters」,不會知道問題出在那個看起來就是空白的空白上,更不會知道要改成半形。
   這個訊息是寫給實作者看的,不是寫給使用者看的。
3. 產品介面本身是繁體中文 —— 這不是邊角族群。

**存在嚴格更好的方案,而且正是我上一輪寫的那個**:
> 「在長度/保留名比對前把所有 `Zs` **正規化**為 U+0020」

比較兩者:

| 輸入 | 現行(拒收 `Zs`) | 正規化 `Zs`→U+0020 |
|---|---|---|
| `「深海　之夜」` | ❌ 拒收,訊息難懂 | ✅ 收下,存成「深海 之夜」 |
| `「　辦公室　」` | ❌ 拒收,訊息說「有非 ASCII 空白」 | ❌ 拒收,訊息說**「辦公室」是內建主題的保留名** ← 更準確 |
| `「　」`(純全形空白) | ❌ 拒收 | ❌ 拒收(trim 後長度 0) |

**正規化在安全上一分不輸(仿冒案例全部照樣擋下,而且錯誤訊息更準確地說出真正的原因),
在可用性上明顯較優。** 這條建議上一輪就寫在 `review-round4.md` 裡,本輪的修正報告採用了拒收,
但**沒有說明為何不採正規化** —— 我判為 SHOULD:不擋出貨,但應該回頭換掉,成本很小。

(附帶好處:換成正規化後,`Zl`/`Zp` 也可以順勢當成空白正規化,而不是拒收。)

### 取捨 3:舊淺色包的 chip 會固定成深色 —— **可接受**

實作者說「外觀完全不變」與「色槽不可覆寫」互斥,這個判斷**正確** —— 可覆寫就是偽造的來源,沒有第三條路。

我另外驗算了一件報告沒提、但讓這個取捨更站得住腳的事:
chip 的**前景與背景現在都來自 marker 槽**(`color-mix(marker-builtin 55%, marker-surface)` 上的 `marker-fg`),
所以 chip 的內部對比度算出來約 **#404e77 上的 #e7e8ee ≈ 7:1**,而且**與主題無關、恆定**。
換句話說,這個改動順帶消除了「某個主題包把 chip 調到看不清」的整類 WCAG 風險 ——
在無障礙上是**淨改善**,不只是「不變差」。

殘餘代價僅止於:淺色包上會出現一顆深色藥丸,風格突兀但完全可讀。可接受。

### 取捨 4:lone surrogate 兩端不完全同構 —— **可接受**

TS 端 `\p{Cs}` 擋下;Go 的 `encoding/json` 在**解碼時**就把落單代理換成 U+FFFD,所以 Go 端看到的已不是代理。
方向是「前端先拒、後端收下並存成 U+FFFD」—— U+FFFD(替換字元,`So`)渲染成「�」,
**不冒充任何東西**,反而醒目。這是安全方向的不對稱,不構成問題。
57 組語料裡沒有這個案例,所以 parity 測試在該語料上仍是 57/57 —— 報告誠實記錄了這件事,沒有假裝覆蓋到。

### 既有主題包仍能匯入生效

拆分出來的 token 預設仍 alias 母 token(`check-token-roles.mjs` 中那段 round-2 註解的設計未動),
所以只覆寫 `--color-overlay` 的舊淺色包仍會帶動所有拆出的子 token。
conformance 975 全綠、`legacy-pack-compat-report.txt` 的既有證據未失效。
唯一的行為變化是上面取捨 3 的 chip 顏色。

### 關於 `useRelocateMachine.test.tsx` 的不穩定測試

我在複驗的完整 `bin/ci.sh` 中**沒有**遇到它。我另外檢查了它與本批改動的關係:
`useRelocateMachine` 及其測試檔**不在本票的 76 個變動項裡**,
本批改動觸及的是主題 token、i18n 標示子樹、名稱驗證與兩支 lint/型別設定,
與 machine relocate 的狀態流沒有共用模組。
**我找不到任何證據指向它與本票有關**,支持協調者「既有不穩定測試」的判定。
(此結論的強度僅止於「無關聯證據」,不是「已證明無關」。)

---

## 7. 逐條判決

| 條 | 判決 | 一句話 |
|---|---|---|
| **A** 設定頁偽造 | **成立** | 上一輪的三條路(文字/顏色/名字)加我這輪新想的六條(分組標題覆寫、`role="group"` 標籤、惡意字型、排序、其他畫面、匯出檔)**全部攻不進去**。chip 的文字與顏色雙雙不可覆寫是真正把門關上的東西。**沒有第三次重現。** |
| **B** 隱形字元類別規則 | **成立** | 兩端改用 Unicode 類別;parity 測試我用四種手法嘗試讓它靜默變綠,**全部 fail-closed**。唯一該回頭的是「拒收」應改為「正規化」(SHOULD-3),那是執行方式而非方向的問題。 |
| **C** 守衛 5 條繞法 | **有條件成立** | 5 條全堵、第三輪修法沒賠掉、`targets()` 是質的改進;但新的 cascade 模型留下 3 個新盲區(`!important`、多層複合 specificity、寫死的載入順序)。 |
| **D** 型別閘 | **成立** | 根因(缺 `@types/node`)修對,兩份 tsconfig 都在 `build` 路徑上,兩個方向實測會紅。 |

## 8. 新發現

**BLOCKER — 無。**

| 級別 | 編號 | 摘要 |
|---|---|---|
| **SHOULD** | **SHOULD-1** | 守衛的 cascade 模型漏了 `!important`(N1)與多層複合 specificity(N2),兩者實測 exit=0 而螢幕是 2.85:1。 |
| **SHOULD** | **SHOULD-2** | 守衛把「theme.css 最先載入」寫死在 `cascadeRank` 裡,**從未讀取 `main.tsx`**;對調 import 兩行後守衛答案逐字不變但螢幕真相反轉,且無測試釘住這個前提。建議改成「斷言每顆受測 token 在 at-rule 外只有唯一一條 `:root` 定義且不帶 `!important`」,一次關掉整類問題。 |
| **SHOULD** | **SHOULD-3** | 非 U+0020 空白改為**拒收**,使「深海　之夜」這類完全正當的中文名被擋,且錯誤訊息對使用者不可行動。改為**正規化 `Zs`→U+0020** 在安全上不輸(仿冒照樣擋下,訊息還更準確地指出「辦公室是保留名」)、可用性明顯較優。 |
| **NIT** | **NIT-1** | `.ts-group-head` 的顏色讀可覆寫的 `--color-text-muted`,主題包可把兩個分組標題調到看不見(chip 仍在,偽造不成立);且該處不在 `ThemeSettings.test.tsx` 的 CSS 守衛掃描範圍內。 |
| **NIT** | **NIT-2** | 產生器按 `--color-marker-` 前綴排除是**靜默**的:未來有人以此開頭新增一般主題色會被無聲踢出白名單。反方向(改名繞出去)已被 `.ts-tag` 的 `var()` 結構性斷言關住。建議 `gen:tokens` 把被排除的名單印出來。 |

## 9. CI 與工作區還原確認

我自己跑了一次完整 `bash bin/ci.sh` → **`[ci] all green`**(exit 0):

- go:`ok ocserverd 49.363s` / `ok ocwarden 36.424s` / `ok ocagent 0.807s`
- frontend:`Test Files 165 passed (165)` / `Tests 1317 passed (1317)`,兩份 tsconfig 的 `tsc --noEmit` 皆綠
- Playwright CT 視覺守衛:`142 passed (34.1s)` —— 內建深色主題外觀無位移
- `[token-roles] ok — … 4.52:1 vs --color-on-danger / 3.76:1 vs --color-bg (its 1px ring)`
- 五道產生器 drift gate 全部無 drift
- conformance:`975 passed in 15.87s`,`[conformance] all green`

**沒有遇到 `useRelocateMachine.test.tsx` 的不穩定失敗**(單次完整跑,一次過)。

還原:守衛實驗全程在 temp 複本上進行(`TOKEN_ROLES_SRC`),真實工作區未被寫入;
sabotage(`theme_bundle_test.go`、`themeBundle.ts`、`check-token-roles.test.ts`、
`themeName.parity.test.ts`)每次皆以 `cp` 備份還原;
一次性 probe 檔 `frontend/src/lib/__rc.test.ts` / `__rc2.test.ts` 已刪(確認無殘留)。

```
$ git status --porcelain | wc -l
76                       # 與複驗開始時相同

$ git diff | shasum -a 256
dfd611a02fa1c317ed6c86c9256a4c0e09a0e80113b39dd1ff473d53a6f43799
```

**tracked diff 的 sha256 前綴仍為 `dfd611a02fa1c317`,與複驗版本快照一致 —— 工作區已還原乾淨。**
本輪新增的檔案只有兩個文件檔:`docs/T-081b-evidence/review-round4-recheck.md` 與
`round4-review/recheck-probes.md`(皆在未追蹤的 `docs/` 之下)。
`round4-review/` 既有的五個檔(`gen-cases.py`、`name-cases.json`、`go-verdicts.json`、
`ts-verdicts.json`、`guard-bypass-probe.md`、`redproof-round4.md`)未刪未改。
