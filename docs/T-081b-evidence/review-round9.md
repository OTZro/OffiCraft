# T-081b 第九輪獨立審查(round 5–8 區段)

**HEAD `fc1af1d`**　基底 `12b84d1`　分支 `feat/T-081b-theme-token-split`
審查者:獨立審查者(未參與 round 5–8 任何實作)
審查範圍:`review-round4-recheck.md` **之後**的四輪 —— round 5 / 6 / 7 / 8
證據目錄:`docs/T-081b-evidence/round9-review/`

> 本檔採「做一塊寫一塊」,順序即調查順序。

---

## 出版負責人點名的三面 —— 逐項判決

### 面 1:產生檔重跑後是否真的等價 —— ✅ 通過

在乾淨工作區(`git status --porcelain` 為空)重跑 `npm run gen:msgkeys`:

```
[gen-message-keys] wrote 791 message keys →
  frontend/src/i18n/messageKeys.generated.ts
  server/ocserverd/message_keys_gen.go
git status --porcelain  →  (空)
```

**兩份產出 byte 不變。** 這一項是我自己重跑的,不是讀他們的 log。

主線的 key 有沒有掉:把 `12b84d1`(主線基底)與 `fc1af1d` 的 `message_keys_gen.go`
key 集合取出比對(`round9-review/keys-base.txt` / `keys-head.txt`):

| | 數量 |
|---|---|
| base `12b84d1` | 726 |
| head `fc1af1d` | 791 |
| **只在 base(被分支拿掉)** | **4** |
| 只在 head(分支新增) | 69 |

被拿掉的 4 顆全部是本票**刻意**移除的,無一是 rebase 事故:

* `profile.themeOffice` / `profile.themeNewName` —— 搬進 `themeIdentity` 子樹(本票的唯一保障)。
* `settings.themeBuiltinTag` / `themeCustomTag` —— round 4 拿掉每列標籤時刪除。

也就是說 **base 上其餘 722 顆一顆不少**;主線那 10 顆帶進來的 key 也在其中(base 的
全集都在 head 裡)。另外核對 TS 白名單與 Go 白名單 **791 顆逐字相同**
(`diff keys-head-ts.txt keys-head.txt` 無輸出)。

`themeIdentity.*` 在白名單中 **0 顆**(鎖住),`themeMarkers.{builtinGroup,customGroup,copyTag}`
**3 顆都在**(round 8 放開,兩端一致)。

**判決:等價,沒有掉 key,沒有靜默出錯。**

---

### 面 2:未讀徽章 2.74:1 —— ⚠️ **通過但有一條 SHOULD**

拆成三個子問題,逐一自己量:

**(a) 外框的處理是否真的有效 —— ✅ 有效。**
守衛 `check-token-roles.mjs` 對三個徽章選擇器 `.nav-tab__badge` /
`.office__tab-badge` / `.member-card__unread` 各要求:`outline` 簡寫**必須存在**,
且 `{outline, outline-color}` 家族**每一條**都得帶 `--color-bg`。我在隔離副本裡把
`.nav-tab__badge` 的 `outline: 1px solid var(--color-bg)` 整行刪掉:

```
[token-roles] 1 violation(s) — a T-081b token split has been undone.
  styles/theme.css:0  .nav-tab__badge { outline: (missing)
      .nav-tab__badge has no outline declaration — without the page-colour ring the
      pill is measured against the wrong background (--color-indigo on an active tab is 2.74:1).
```
(`round9-review/redproof-D-badge-ring.txt`)

**(b) 守衛的錯誤訊息是否誠實說明它量的是哪個底色 —— ✅ 誠實。**
`checkRatio(BADGE_RING, 3, …)` 的訊息原文:
「Measured against `--color-bg` — the colour of the pill's **1px ring, NOT** of whatever
is behind the pill: it sits on `--color-indigo` on an active nav tab and on
`--color-card` on a selected member card」。它主動點名自己量的不是背後那層。

**(c) 「已窮舉無解」是否成立 —— 部分成立,見 SHOULD-1。**

`chrome.css` 的原文措辭是精確的:「No colour exists that clears 4.5:1 **against white**
AND 3:1 against all three」。我用解析法覆核,這句**為真**:
* 白字要 AA ⇒ 徽章底相對亮度 `Lf ≤ 0.1833`
* 對 `--color-indigo #2c3350`(L=0.0348)要 ≥3:1 ⇒ `Lf ≥ 0.2044`
* 兩者不相交 ⇒ 白字下確實無解(不需窮舉,直接矛盾)。

**但「白字」不是外部條件,是這張票自己剛拆出來的一顆色槽。**
`--color-on-danger` 在 round 8 之後的全 repo 使用點只有三處
(`chrome.css:271`、`office.css:275`、`office.css:383`)—— **就是那三顆徽章的字色,
別無他處**(`grep -rn 'color-on-danger' frontend/src`)。把它一起放開來搜:

| 搜尋空間 | 同時滿足 字/底 ≥4.5 且 對三個底色各 ≥3 的解 |
|---|---|
| 字色固定白 | **0** |
| 字色自由(深字 + 淺底) | **152,663**(8-bit sRGB,步長 4 取樣) |
| 字色自由且底色限定**紅色系**(r > g+40 且 r > b+40) | **207,800**(另一次獨立、更細的步長 2 掃描) |

具體一個:徽章底 `#ff8f88`(仍是紅色系)+ 深字 ——
對黑 9.54,對 `#191c24` 7.73 / `#242832` 6.69 / `#2c3350` **5.62**,四項全過,
**根本不需要外框**。腳本:`round9-review/exhaustion-recheck.mjs`。

⇒ 這條取捨的真實形狀是:**「維持白字」是一個設計選擇,不是技術限制。**
把它記成「窮舉無解」會讓下一個接手的人以為此路已封死。

**SHOULD-1(誠實度 / 記錄):`T-081b-final-delivery.md` §四.2 的措辭比 `chrome.css`
更糟,它把「白字」這個前提整個拿掉了:**

> 「已窮舉整個色域,**不存在**任何單一顏色能同時滿足三個底色的要求。」

這句話**是假的** —— 滿足三個底色的顏色有 152,663 個(我上面數的)。
這是 owner 會讀的那份交付摘要,也是這張票唯一一項自承不合格的取捨。
**怎麼重現**:`node docs/T-081b-evidence/round9-review/exhaustion-recheck.mjs`。
**為什麼是問題**:本票自己的 commit 08f778a 就是為了同一類毛病而修的 ——
「標著『實測』的紀錄若與實測不符,比沒有註記更糟」。這裡的形狀一模一樣。
**建議**:把 §四.2 改成 `chrome.css` 那句精確的措辭(「在**白字**前提下無解」),
並加一句「放開 `--color-on-danger` 有解,但那會把徽章翻成淺底深字,是設計取捨」。

**NIT-1:外框自身對它所坐的底色是 1.38:1。**
`--color-bg #191c24` vs `--color-indigo #2c3350` = **1.38:1**。也就是說在 active
分頁上那圈外框對分頁底幾乎不可見;徽章的可辨識性完全靠「填色 vs 外框」的 3.76:1,
形成的是一圈看不見的暗暈。以 WCAG 1.4.11「相鄰顏色」論證是可辯護的,但
1px、且外框本身與相鄰面不可分,是這條論證最薄的一環。現況已記錄、我不主張改,
只主張別把它寫成「已解決」。

---

### 面 3:有沒有測試在「配合實作」而不是「驗實作」

我逐條讀了 round 5–8 新增/改寫的測試,並自己弄壞 5 處看紅(全部在**隔離副本**跑,
見文末說明)。**總體結論:沒有發現斷言恆真、sentinel 失效或 mock 蓋掉被測邏輯的
測試。** 抽驗結果與他們的宣稱一致。但有一條「改寫時把唯一守著某個真實失敗模式的
斷言拿掉,而那個失敗模式現在是活的」—— 見 SHOULD-2。

#### 我自己弄壞、自己看到紅、自己還原的 5 條(不是讀他們的 log)

| # | 弄壞什麼 | 預期守著它的是誰 | 實測 | log |
|---|---|---|---|---|
| A | `gen-message-keys.mjs` 的 `NON_OVERRIDABLE_SUBTREES` 清空(把**唯一**保留的保障拆掉)+ 重跑產生器 | `messageKeys.theme-identity.test.ts`、`ThemeSettings.test.tsx`、`themeBundle.test.ts` | **紅 3 檔 3 條**;關鍵那條印出 `expected '偽造' to contain '辦公室'` —— 內建列真的被主題包改名了 | `redproof-A-identity-exclusion-removed.txt` |
| B | `ProfileDropdown.tsx` 把 `customThemes.map(...)` 搬到內建 `<option>` **之前** | round 7 新增的排序釘子 | **紅 2 條**(`…two identical built-in rows by a theme's NAME`、`keeps the built-in first and the packs after`) | `redproof-B-picker-order.txt` |
| C | `.ts-group-head` 的 `color` 從 `var(--color-text-muted)` 換成寫死的 `#9aa0ad`(**視覺上完全相同**的顏色) | round 8 改寫的 `paints the group headings with a pack-settable colour token` | **紅 1 條** —— 這條確實在驗「必須是可覆寫色槽」這個**行為**,不是在驗像素 | `redproof-C-group-head-token.txt` |
| D | 刪掉 `.nav-tab__badge` 的 `outline: 1px solid var(--color-bg)` | `check-token-roles.mjs` | **紅**,訊息指名選擇器並附上 2.74 的理由 | `redproof-D-badge-ring.txt` |
| E | **只**拿掉 Go 端 `normalizeThemeSpaces` 的 Zs 折疊(單邊漂移,TS 不動) | 61 組前後端 parity + Go 孿生表 | **紅**:Go `TestTrimThemeName` / `TestValidateThemeBundles` fail;parity 逐條印出 `go: ACCEPT / ts: REJECT: name must be 1..80…` | `redproof-E-go-zs-drift.txt` |

C 這條特別值得記:我刻意換成**肉眼與現況一模一樣**的寫死色,測試仍然紅 —— 代表它
斷言的是「顏色必須來自主題包可設定的 token」這個**行為**,不是在對某個色值打卡。
這正是「驗實作」而非「配合實作」的形狀。

#### 逐條複核 round 5–8 的測試(讀,不只跑)

* `gen-theme-tokens.test.ts`(round 5 新增 → round 8 整檔改寫):三條都在驗**性質**,
  且 `tokensIn()` 是把抽取規則**獨立重述**一次,不是呼叫產生器自己的函式。
  `refuses to emit an empty whitelist` 是真正的 sentinel(白名單為空會讓每個主題包被拒
  卻看起來像乾淨跑完)。**不是恆真。**
* `messageKeys.theme-identity.test.ts`:`does not let a theme bundle rename a theme` 遍歷
  `zh.themeIdentity` 的**每一個 key**(不是硬寫 `office`),再加兩顆「改動前的舊位置」
  的顯式否定。round 8 新加的 `does let …re-word the 內建/自訂 labels` 是斷言**相反**
  的性質,兩端都放開 —— 這是把一個「不能」測試改成「能」測試時最容易寫成空殼的地方,
  這裡沒有:它遍歷 `zh.themeMarkers` 全部 key 且斷言 `true`,清空子樹就會紅。
* `ThemeSettings.test.tsx > keeps the built-in row's own name when a pack forges everything else`:
  **round 8 報告自己承認的那條「假性通過」已經修好且修對了。** 第 163 行
  `expect(getByTestId("ts-group-builtin").textContent).toBe(SENTINEL)` 是真的哨兵活性檢查
  —— 我在紅證 A 裡看到它先通過、然後才是第 171 行倒下,證明順序是對的
  (overlay 真的生效,而內建列的名字真的守住了)。
* `themeName.parity.test.ts`:三條各司其職 ——
  ①語料下限 + 16 個必備 key 逐一存在(防語料被悄悄縮水);②61 組兩端逐條同判,
  外加 `Object.keys(go).length === cases.length`(防 Go 端少吐);③**絕對判決**逐條釘死
  (防「兩端一起錯」)。round 8 只把 12 個 key 從 REJECT 清單搬到 ACCEPT 清單,
  **沒有**把它們搬進一個「其餘皆可」的兜底,所以規則若倒回來會紅(紅證 E 佐證這張網有牙)。
* `theme-contrast.ct.spec.tsx` + `ThemeContrastStory.tsx`(round 6 新增):
  我核對過 `LIGHT_PACK` 的 11 顆色**逐字等於**真實的 `shots-pack/smurf-village.theme.json`
  (不是拼出來的最壞情況)。CSS 也真的載入 —— `ThemeSettings.tsx` / `InlineEdit.tsx`
  各自 `import "./*.css"`,所以量到的是產品樣式,不是瀏覽器預設。
  `built-in dark` 那條還額外釘住「不得低於出貨時的 4.72」,是迴歸門檻不是浮動門檻。
* `check-token-roles.test.ts`(round 5 改寫成「唯一 `:root` 定義且無 `!important`」):
  14 條全綠;新增的 `fails on a second :root definition whichever file holds which value`
  **兩個方向都跑**,所以守衛裡再也沒有任何「載入順序」假設可以被弄錯 —— 這是把
  round 4 那個靠 `.at(-1)` 猜勝負的模型整類關掉,不是補一個特例。

#### SHOULD-2:round 8 改寫測試時,把唯一守著一個**現在是活的**失敗模式的斷言拿掉了

`themeBundle.test.ts` 的 `drops an override of the theme structural markers`
改寫成 `keeps a themeMarkers override and still drops a theme's identity` 時,
原本的 `"themeMarkers.copyTag": "x".repeat(200)` 被換成了 `"備份"`(2 字)。
被拿掉的正是這條測試當初存在的理由。

**怎麼重現**(我實測過,`round9-review/probe-copytag.txt`):

1. 一個主題包把 `wording.zh["themeMarkers.copyTag"]` 設成 140 字。
   → `validateThemeBundle` **接受**(round 8 把整個 `themeMarkers` 子樹放開了)。
2. 套用它,然後按**內建主題「辦公室」那一列**的下載鈕。
   `ThemeSettings.tsx:1127` 走 `msg.themeCopyName(t.themeIdentity.office)`,
   產出的 `name` 長 **145** 字。
3. 把那個檔案匯回產品 → `theme: name must be 1..80 characters after trimming`。

**為什麼是問題**:這不是「使用者把自己的包搞壞」,而是**主題包讓內建主題的下載鈕
產出產品自己拒收的檔案**。內建主題的匯出正是「找得回出貨那一份」的逃生口,
而「保住回得去的路」是 owner 唯一保留的那條保障的立意。
round 8 報告第 §2 節**有**用文字揭露這件事(值得肯定,沒有含糊帶過),
但它把後果描述成主題包自己的事;實際落點是內建主題那一列。

**這是 SHOULD 而不是 BLOCKER**,因為:owner 的裁定字面上涵蓋它、後果可逆
(刪掉那個包就恢復)、且已書面揭露。
**建議**(擇一,都不違背 owner 裁定):
(a) `themeCopyName` 組出的檔名在超長時退回內建的 `副本` 字樣;或
(b) 匯出時對組出的 `name` 做一次和匯入相同的長度裁切;或
(c) 把 `copyTag` 從 `themeMarkers` 移進 `themeIdentity`(它本來就不是「內建/自訂」標籤,
    是檔名構件,和分組標籤放在同一子樹只是歷史巧合)。
無論選哪個,補回一條「主題包把 copyTag 撐長 → 內建主題的下載檔仍匯得回來」的測試。

---

## 其他必查項

### 1. `themeIdentity` 是否真的仍然鎖住 —— ✅ 是(我自己造包實測)

造了一份**專門來覆寫它**的包(`round9-review/probe-identity-and-copytag.test.ts.txt`,
跑完即刪,輸出留 `probe-identity-and-copytag.txt`):把 `MESSAGE_KEYS` 791 顆全部
設成哨兵,外加直接瞄準 `themeIdentity.office` / `themeIdentity.newTheme` /
`profile.themeOffice`(改動前的舊路徑),自己取名叫「辦公室」。

```
verdict: null                              ← 包被接受(round 8 的放寬)
themeIdentity.office survives?  false      ← 被丟棄
themeIdentity.newTheme survives? false     ← 被丟棄
profile.themeOffice survives?    false     ← 舊路徑也丟
themeMarkers.builtinGroup survives? true  value = 自訂   ← 放開了,如設計
```

前後端都查過:TS 端 `validateThemeBundle` 就地刪碼;Go 端
`dropUnknownWordingCodes` 走同一份 `messageKeys`,而且 **`settings.go` 在
讀取路徑上也會再 prune 一次**(`loadSettings`,註解寫明「a row written BEFORE the
whitelist shrank」)—— 舊資料列不會把已退休的 code 端回前端。這一層我認為做得對。

**NIT-2(縱深,不是缺陷)**:`i18n/wording.ts` 的 `applyWording()` **本身不查白名單**
—— 它會套用任何解析得到既有字串葉的點路徑。目前唯一擋住 `themeIdentity.*` 的是
「寫入時驗證 + 讀取時 prune」這兩道,渲染層沒有第三道。今天沒有漏洞
(所有寫入路徑都經過驗證,讀取路徑也 prune),但未來任何一條新的「把 bundle 存進
customThemes」的路徑若忘了驗證,owner 最初回報的那個 bug 會**原封不動**復活,
而且不會有任何測試變紅。在 `applyWording` 裡加一行 `MESSAGE_KEY_SET.has(code)`
是零成本的第三道門。

### 2. 名稱驗證的其餘規則是否完好 —— ✅ 完好

| 規則 | 狀態 | 憑據 |
|---|---|---|
| 長度上限(1..80,trim 後) | 在 | 語料含 `len_80_bmp` / `len_81_bmp` / `len_80_astral` / `len_81_astral`;probe 2 實際踩到它 |
| Unicode 類別拒收(Cc/Cf/Co/Cs/Zl/Zp) | 在 | parity 測試③逐條釘 5 個代表(`tag_char`/`soft_hyphen`/`mongolian_vowel_sep`/`line_sep_u2028`/`para_sep_u2029`) |
| Zs 正規化成 U+0020(不拒收) | 在 | `trimThemeName` 對照表 12 列;紅證 E 證明單邊拿掉會紅 |
| 61 組前後端 parity | 在,**語料一顆不動** | `themeName.cases.json` 實測 61 筆;測試有 `>= 61` 下限 + `Object.keys(go).length === cases.length` 雙保險 |
| `RESERVED_THEME_IDS`(**id** `office` 仍保留) | 在 | round 8 明說只拆「名稱」不拆 id;`themeBundle.test.ts` 補了 `id: "office"` 仍 REJECT 的斷言 |

round 8 拆的是**名稱比對**那一條,沒有連帶拆到上面任何一項。

### 3. 死碼與孤兒 —— ✅ 沒有死碼,但**註解有 SHOULD-3**

已移除且全 repo 零殘留(`grep -rIn` 逐一確認):`isBuiltinThemeName`、
`BUILTIN_THEME_NAME_SET`、`normalizeThemeName`(TS 與 Go 兩端)、
`THEME_IDENTITY_NAMES` / `themeIdentityNames`、`NON_OVERRIDABLE_TOKENS`、
`NON_OVERRIDABLE_PREFIX`、`--color-marker-*` 色槽、`.ts-tag` / `.ts-tag--custom` 規則、
`settings.themeBuiltinTag` / `themeCustomTag` 兩顆 i18n key、`<optgroup>`。

* `gen-theme-tokens.mjs` 現在只剩**一道**守衛(白名單為空 → exit 1),
  沒有留下永遠不會觸發的排除機制。round 8 說「清單空掉後那兩道守衛永遠不會觸發,
  留著就是死機制」—— 這個判斷是對的,而且真的做乾淨了。
* `.ts-tag` 還出現 3 次,全部是**否定斷言**(`toHaveCount(0)` / `.length).toBe(0)`)
  —— 那是刻意的絆線,不是孤兒。
* i18n key 沒有孤兒:791 顆白名單 = `en.ts` 的字串葉全集(扣除 `themeIdentity`),
  重跑產生器 byte 不變。

**SHOULD-3:round 8 拆掉 `themeMarkers` 的保護,但把「它不可覆寫」寫成事實的註解
留在原地 —— 而且就留在 round 8 自己改的那幾個檔裡。**

| 位置 | 現存文字 | 事實 |
|---|---|---|
| `frontend/scripts/gen-theme-tokens.mjs:60-61` | 「the heading's TEXT lives in the **non-overridable** `themeMarkers` i18n subtree」 | **假**。`themeMarkers.*` 三顆自 round 8 起就在 `MESSAGE_KEYS` 裡,我的 probe 1 實際把 `builtinGroup` 改成了「自訂」 |
| `frontend/src/styles/theme.css:168` | 「標題的『文字』與『屬於哪一組』**仍不可偽造**」 | **半假**。分組不可偽造(真);文字可以(假) |
| `frontend/src/components/theme-settings.css:68` | 「…and in the **non-overridable** `themeMarkers` i18n subtree (what the heading says)」 | **假**,同上 |
| `frontend/src/components/theme-settings.css:65` | 「the twin of the quick picker's `<optgroup>`」 | **過時**。`<optgroup>` 在 round 7 就被移除了 |
| `frontend/src/components/ThemeSettings.test.tsx:93-94` | 「…from the non-overridable themeMarkers subtree — the same source the quick picker's `<optgroup>` uses」 | **兩句都假** |
| `frontend/src/components/ThemeSettings.test.tsx:548-550` | 「since T-081b a bundle **may not claim a built-in display name**, so exporting under it would hand the owner a file the product then refuses to import back」 | **假**。round 8 正是拆掉這條規則的那一輪 |

`zh.ts` / `en.ts` / `gen-message-keys.mjs` / `messageKeys.theme-identity.test.ts` 的
對應說明**都已正確更新**,所以這不是「來不及改」,是漏掉了六處。

**怎麼重現**:
`grep -rn -i 'non-overridable\|不可偽造\|optgroup' frontend/src frontend/scripts`。
**為什麼是問題**:`gen-theme-tokens.mjs` 那一段是產生器的**設計說明**,下一個維護者
讀到它會相信「標題文字受保護,所以顏色放開沒關係」—— 而顏色放開的整個論證正是
建立在那句話上。這與 commit 08f778a 修的那類缺陷同型(寫著保證、實際沒有),
只是這次在註解而不是在紅證表格。**修法是刪/改六行字,零風險。**

### 4. round 6 拿掉 chip 的論證,在 round 8 之後只剩一條腿(SHOULD-3 的延伸)

round 6 報告用三個理由說「拿掉 chip 沒有弱化防偽」:①分組由渲染決定;
②標題**文字**來自不可覆寫的 `themeMarkers`;③標題**顏色**來自白名單外的 marker 槽。
round 8 把 ②③ 都拆了。**剩下的 ① 仍然成立且足夠**(我在 probe 1 裡確認:
包自己叫「辦公室」、把 `builtinGroup` 改成「自訂」,那一列**仍然**在自訂群組裡,
內建群組裡仍然只有一列且寫著「辦公室」),所以結論沒錯 ——
但寫在檔案裡的**論證**已經有兩條腿是假的。這正是 SHOULD-3 要修的東西。

### 5. 既有行為回歸 —— ✅ 內建深色主題只有已知的兩處變化

逐條比對 `12b84d1..fc1af1d` 的所有 CSS 改動,並回查 `theme.css` 的種子值:

* 本票新拆出的每一顆色槽都是 **alias default**,值就是原本那顆 ——
  `--color-surface-sunken`/`--color-backdrop` = `var(--color-shadow)`;
  `--color-on-backdrop`/`--color-on-danger`/`--color-on-indigo`/`--color-knob` = `var(--color-overlay)`;
  `--color-topbar-bg`/`--color-nav-bg`/`--color-main-bg` = `var(--color-bg)`;
  `--color-scrollbar-thumb` = `var(--color-indigo)`。⇒ 內建主題下逐像素不變。
* 唯二寫死新值的:`--color-danger-badge: #ba5953`(**已知變化 1**,owner 指示要修的)
  與 `--color-callout-warning: #f08a8a`(只給本票新增的 `.set-warn` 用,無既有元件受影響)。
* `.login__hint` `color-mix(--color-text 55%)` → `var(--color-text-muted)`
  (**已知變化 2**,`#8f9299` → `#9aa0ad`,對比 4.72 → 5.62,只升不降)。
* `.ts-group-head`:round 5 改吃 marker 混色、round 8 又改回 `--color-text-muted`
  —— **相對主線淨零變化**(主線上這顆標題本來就是 muted 色)。
* 每列的「內建/自訂」chip 消失是**結構性**改動(owner 指定),不是配色回歸。

146 個 Playwright CT 視覺守衛在我自己的完整 CI 跑裡全綠,沒有位移。
既有主題包相容性:`legacy-pack-compat-report.txt` 之外我另外用真實的
`shots-pack/smurf-village.theme.json` 跑過 probe(matching CT story 的 11 顆色與
該包**逐字相同**),匯入生效無警告。

### 6. `T-081b-final-delivery.md` 已與現況脫節(SHOULD-4)

除了 §四.2 的窮舉宣稱(SHOULD-1),這份「交付摘要」還停在第五輪:

* §四.3「設定頁『內建／自訂』**小標籤固定成深藍紫**」—— chip 在 round 6 就整個拿掉了,
  marker 色槽在 round 8 也刪了。整條敘述指向一個已不存在的機制。
* §四.6「防線改由**結構化分組**承擔」—— 這句現在反而是對的(見上面第 4 點),
  但它下面接的 §五「各輪審查結論:`review-round{1,2,3,4}.md`」與
  「各輪修正報告:`round{3,4,5}-fix-report.md`」**漏掉 round 6/7/8 與本檔**。
* §六「拍板後的動作 2. 通知主題包同事(新白名單要用 71 槽 + 徽章底 + **marker 族**)」
  —— marker 族已不存在;正確數字是 `theme.css` 的 `--color-*` 全集 **72 顆,零排除**。

**為什麼是問題**:這是唯一一份寫給「不讀四輪報告的人」看的摘要,而它三處指向已刪機制、
一處數字錯誤、一處宣稱不實。**建議在合併前就地更新。**

---

## 我自己跑的完整 CI(乾淨樹、隔離副本、HEAD `fc1af1d`)

```
[ci] commit fc1af1d56729d421c83be25862ca5d5e278bbb7c (HEAD, tree clean) — started 2026-07-27T16:15:22Z
…
frontend:    Test Files 166 passed (166) / Tests 1327 passed (1327)
Playwright:  146 passed (33.5s)
conformance: [conformance] all green (target=go base=http://127.0.0.1:8795)
[ci] all green
CI_EXIT=0
```
log:`round9-review/ci-round9-clone.log`(第 3564 行 `[ci] all green`)。
⚠️ 這一跑正是後面那節「我造成的環境破壞」的來源;**權威綠證請看
`ci-round9-after-node-modules-restore.log`**(原樹、修復後、同樣全綠)。

* ⚠️ 已知會偶爾紅的 `useRelocateMachine.test.tsx` **這次綠**(18 tests,149ms,log 第 1172 行),
  **沒有重跑帶過**(整份 CI 只跑了一次)。
* 這一跑是在**隔離副本**(`git clone --local` 到 scratchpad、checkout `fc1af1d`、
  `frontend/node_modules` symlink 回原樹)上做的,樹是乾淨的 —— 原因見下一節。
* 跑 CI 期間本機上還有:另一位 agent 的 vite dev server(:5199)與其截圖工作、
  多個編輯器 language server(tsserver / pyright)。CI 用的 port(CT 5241、
  conformance 8795)與之無衝突,實測全綠。

---

## ⚠️ 環境事故通報:審查期間有另一位 agent 正在同一個工作區裡動工

我開始時 `git status --porcelain` 是**空的**。做到一半再看,出現了三個我沒有建立的
未追蹤檔:

```
?? docs/T-081b-evidence/shots/30-nav-frame-current-smurf-narrow-1440.png   (00:10:46)
?? docs/T-081b-evidence/shots/31-nav-frame-planA-navbg-eq-topbarbg.png     (00:12:22)
?? docs/T-081b-evidence/shots/32-nav-frame-planB-seg-transparent.png       (00:11:02)
```

第 31 張的**檔名**在我兩次 `git status` 之間還改過一次
(`…-eq-mainbg.png` → `…-eq-topbarbg.png`),而且 `ps` 顯示有一個已跑 3 分 43 秒的
`vite --port 5199` 在這棵樹上。**有另一位 agent 正在對 nav 區塊做即時截圖比對。**

我在發現之前已經做過一次會動到原樹的紅證(紅證 A,約 40 秒,改了
`gen-message-keys.mjs` 與兩份產出檔),已完整還原。發現之後**立刻改成在隔離副本
上做所有後續破壞性動作**(紅證 B/C/D/E、兩個 probe、完整 CI 都在副本上),
原樹自此**只讀**。

**要請出版負責人注意的兩件事:**
1. 我的紅證 A 有 ~40 秒的視窗與對方的 dev server 重疊。若對方在那段時間拍了截圖,
   那張的 i18n 白名單是被我改過的(`themeMarkers` 會多三顆)。時間戳可對照:
   我的改動在 00:12:1x–00:12:3x。
2. 那三張 30/31/32 **不是我的產物,我沒有動它們**,它們在我結束時仍是未追蹤狀態。

---

## ⚠️ 我造成的一次環境破壞(已修復,誠實記錄)

**發生什麼**:為了不去動另一位 agent 正在使用的工作區,我把 repo `git clone --local`
到 scratchpad,並用 **symlink** 把副本的 `frontend/node_modules` 指回原樹
(想省一次安裝)。我在副本上跑 `bash bin/ci.sh`,而 ci.sh 第 371 行會執行
**`npm ci`** —— npm 沿著那條 symlink 動手,結果把**原樹**的
`frontend/node_modules` 清空(0 個項目),副本那邊變成一個真實目錄(158 項)。
coordinator 在編輯器裡看到整片 `Cannot find module 'react'` / `'vitest'` 因此而來。

**時間**:2026-07-28 00:18(兩邊目錄的 mtime 都是 00:18,與我那次 CI 的時間吻合)。

**修法**:
1. `cd frontend && npm ci` → 恢復,157 個項目,`require.resolve("react")` 與
   `node_modules/.bin/{vitest,tsc}` 都正常。
2. **刪掉整個 scratch 副本**(`rm -rf …/scratchpad/oc`),把這條 symlink 隱患連根拔掉。
3. 在原樹重跑完整 `bash bin/ci.sh` 覆核 —— **`[ci] all green` / `CI_EXIT=0`**
   (166 檔 / 1327 測試 / 146 CT / conformance 全綠;`useRelocateMachine.test.tsx`
   18 tests 綠)。log:`round9-review/ci-round9-after-node-modules-restore.log`
   (第 3562 行)。**這一份才是本輪的權威綠證** —— 它跑在原樹、乾淨樹、修復後。

**這次事故有沒有污染我的結論**:**沒有。**
* 我所有的「紅」都發生在 **00:12–00:14**,`npm ci` 事故在 **00:18**,全部在事故之前。
* 而且每一條紅都是**指名的斷言**倒下(例如
  `expected '偽造' to contain '辦公室'`、`go: ACCEPT / ts: REJECT`),
  不是 `Cannot find module` 這種環境紅 —— 我逐條回讀過五份 redproof log,
  沒有任何一條是模組解析失敗。
* 那一輪在副本上的完整 CI 本身是 **`[ci] all green` / `CI_EXIT=0`**
  (166 檔 / 1327 測試 / 146 CT),也就是說事故發生時環境是好的,壞的是事後的殘留狀態。

**教訓(寫給下一個審查者)**:要在別人正在用的工作區上做破壞性驗證,可以複製 repo,
但 **`node_modules` 不能用 symlink 共用** —— 目標 repo 的 CI 會跑 `npm ci`,
它會穿過 symlink 破壞來源。要嘛在副本裡老實裝一份,要嘛用
`git worktree` + 各自的 `node_modules`。

---

## 工作區還原確認

結束時 `git status --porcelain`:

```
?? docs/T-081b-evidence/review-round9.md          ← 本檔(依指示新增)
?? docs/T-081b-evidence/round9-review/            ← 本輪證據目錄(依指示新增)
?? docs/T-081b-evidence/nav-frame-diagnosis.md    ← 另一位 agent 的,非我所建
?? docs/T-081b-evidence/shots/30-…png             ← 同上
?? docs/T-081b-evidence/shots/31-…png             ← 同上
?? docs/T-081b-evidence/shots/32-…png             ← 同上
?? docs/T-081b-evidence/shots/33-…png             ← 同上(在我寫這段的當下又多一個)
```

(對方的未追蹤檔在我審查期間持續增加,以最後一次觀察為準。)

**已追蹤檔案零改動**(`git diff` 與 `git diff --cached` 皆空,結束前實測)。
所有紅證的破壞都已還原或根本沒發生在這棵樹上。

`frontend/node_modules` 也已還原(`npm ci`,157 項,`require.resolve("react")` 正常),
隔離副本已整個刪除 —— 見上一節。修復後的完整 CI 全綠。

---

## 發現總表

| # | 等級 | 一句話 |
|---|---|---|
| SHOULD-1 | SHOULD | `T-081b-final-delivery.md` §四.2 的「已窮舉整個色域,不存在任何單一顏色能同時滿足三個底色」**是假的**(這種顏色有 152,663 個);真正的限制是「白字前提下無解」,而白字(`--color-on-danger`)是本票自己拆出來、只有那三顆徽章在讀的一顆槽。 |
| SHOULD-2 | SHOULD | round 8 改寫 `themeBundle.test.ts` 時把 `copyTag = "x".repeat(200)` 換成 2 字,而那個失敗模式現在是**活的**:主題包把 `themeMarkers.copyTag` 撐長 → 按**內建主題**那列的下載鈕產出 145 字的檔名 → 產品自己拒收(實測)。 |
| SHOULD-3 | SHOULD | round 8 拆掉 `themeMarkers` 的保護,卻在自己改的檔案裡留下 6 處寫著「它不可覆寫 / 不可偽造」的註解(含產生器的設計說明),以及 2 處指向 round 7 已刪的 `<optgroup>`。 |
| SHOULD-4 | SHOULD | `T-081b-final-delivery.md` 整份停在第五輪:三處描述已刪機制(chip 固定色、marker 族)、白名單顆數錯(應為 72 顆零排除)、證據清單漏 round 6/7/8。 |
| NIT-1 | NIT | 徽章那圈 1px 外框對 active 分頁底只有 **1.38:1** —— 分離完全靠「填色 vs 外框」的 3.76,外框自身在分頁上不可見。已記錄的取捨,不主張改,但別寫成「已解決」。 |
| NIT-2 | NIT | `applyWording()` 渲染層本身不查白名單,`themeIdentity` 的保障完全靠寫入驗證 + 讀取 prune 兩道。今天沒漏洞,但加一行 `MESSAGE_KEY_SET.has(code)` 是零成本的第三道門。 |

**BLOCKER:0 條。**

三面的判決:面 1(產生檔等價)**通過**;面 2(2.74 取捨)**外框與守衛訊息通過,
窮舉宣稱不成立 → SHOULD-1**;面 3(測試是否在配合實作)**沒有找到恆真斷言、
失效 sentinel 或被 mock 蓋掉的邏輯;5 條紅證我自己弄壞自己看到紅自己還原,
全部與宣稱一致 —— 唯一一條是 SHOULD-2 的「拿掉了守著真實失敗模式的斷言」。**

四條 SHOULD 全部是**文件與註解的誠實度**加**一個已揭露但落點被說小了的產品行為**,
沒有一條要求改架構。我的建議是:SHOULD-1/3/4 合併成一次文字修正(半小時),
SHOULD-2 用 round 8 報告裡提的任一種做法補一個測試,即可落地。
