# T-081b 第八輪修正報告

owner 這一輪連續收斂了範圍,最終界線是他自己的一句話:

> 「這是大家自己用的,自己要怎麼搞我們不用特別管,**我們只要確定主題名稱不會隨著主題改變就好**」

⇒ **唯一保留的保障:`themeIdentity` 子樹(內建主題自己的顯示名)不可被主題包的
`wording` 覆寫。** 其餘三項「防偽」限制全部拆除:

| 原限制 | 第八輪狀態 |
| --- | --- |
| 分組標題的**顏色**吃不可覆寫的 `--color-marker-*` | **拆除** —— 改吃可覆寫的 `--color-text-muted` |
| 分組標題的**文字**(`themeMarkers` 子樹)不可覆寫 | **拆除** —— 回到一般可覆寫用詞 |
| 自訂主題**不得取用內建主題顯示名** | **拆除** —— 自訂主題可以叫「辦公室」 |
| `themeIdentity`(內建主題自己的名字)不可覆寫 | **保留**(owner 原始 bug 的那條) |

未動:群組結構(哪一列屬於哪一群由渲染決定)、下拉「內建永遠第一」。兩者是單純的
UI 行為,沒有為它們新增守衛。名稱驗證的其餘規則(長度、Unicode 類別拒收、空白
正規化、前後端 61 組比對)全部照留 —— 那些擋的是壞資料與前後端判決不一致,不是防偽。

---

## 一、分組標題顏色改回可覆寫色槽

**改了什麼**

* `frontend/src/components/theme-settings.css` — `.ts-group-head` 的 `color` 從
  `color-mix(in srgb, var(--color-marker-fg) 65%, var(--color-marker-surface))`
  改成 `var(--color-text-muted)`(沿用 repo 既有色槽,沒有新增)。
* `frontend/src/styles/theme.css` — `--color-marker-surface` / `--color-marker-fg`
  已無任何使用點,連同說明一併移除(不留死碼)。
* `frontend/scripts/gen-theme-tokens.mjs` — `NON_OVERRIDABLE_TOKENS` 排除清單、
  `NON_OVERRIDABLE_PREFIX` 前綴 tripwire、以及兩道守衛(未登記的前綴 token、
  清單裡有但 theme.css 沒有)**整組移除**。清單變空之後那兩道守衛永遠不會觸發,
  留著就是死機制。白名單現在就是 theme.css 的 `--color-*` 全集,零排除。

**drift**:`npm run gen:tokens` 重跑,72 個 token,兩個產出檔 byte 不變(marker
色槽本來就在白名單之外,拿掉它們不影響輸出)。

**測試(改,不刪)**

* `frontend/src/components/ThemeSettings.test.tsx` — 原本斷言
  「`.ts-group-head` 只讀 `--color-marker-*`、marker 色槽被排除在白名單外」的兩個
  案例,合併改寫成
  `paints the group headings with a pack-settable colour token`:斷言標題吃
  `--color-text-muted`、它讀到的每一顆 `var()` 都在 `THEME_COLOR_TOKENS` 裡、
  且 marker 家族已從樣式表與白名單雙雙消失(不留半套)。
* `frontend/scripts/gen-theme-tokens.test.ts` — 原本三個案例都在看「排除機制不會
  靜默丟 token」。機制沒了,改寫成看相反的性質:產生器把 theme.css 定義的**每一顆**
  `--color-*` 都寫進兩份白名單、新加的 token 不必改第二份清單就會跟著出去、
  白名單為空時必須報錯。丟 token 仍然是這個檔在抓的失敗,只是理由換了。

**紅證**

* `round8-fix-run/redproof-1-group-head-colour.txt` — 把 CSS 與 theme.css 還原成
  marker 版 → `paints the group headings with a pack-settable colour token` 紅。
* `round8-fix-run/redproof-2-generator-drops-a-token.txt` — 讓產生器悄悄濾掉
  `--color-card` → `puts every --color-* token theme.css defines into both whitelists` 紅。

---

## 二、`themeMarkers` 改為可覆寫

**改了什麼**

* `frontend/scripts/gen-message-keys.mjs` — `NON_OVERRIDABLE_SUBTREES` 從
  `{themeIdentity, themeMarkers}` 縮成 `{themeIdentity}`。
* `frontend/src/i18n/locales/{zh,en}.ts` — `themeMarkers` 的說明改寫成現況。
* `frontend/src/i18n/compose.ts`、`frontend/src/components/ThemeSettings.tsx` —
  移除「非可覆寫」的過時註解。

**結果**:`themeMarkers.builtinGroup` / `customGroup` / `copyTag` 三顆進入
`MESSAGE_KEYS` 與 `messageKeys`(785 keys)。

**⚠️ 一個要講清楚的連帶效果**:`themeMarkers.copyTag`(匯出副本檔名裡的「副本」)
跟兩顆標籤在同一個子樹,一起被放開了。主題包若把它塞成 200 字,按內建主題的
「下載」鈕會產出一個名字超過 80 字上限、產品自己匯入時會拒收的檔案。這是把整個子樹
交還給主題包的直接後果 —— owner 說使用者自己怎麼搞不用管,所以照做,但這件事列在
這裡,不含糊帶過。

**測試(改,不刪)**

* `frontend/src/i18n/messageKeys.theme-identity.test.ts` —
  `does not let a theme bundle forge the markers...` 改成
  `does let a theme bundle re-word the 內建 / 自訂 labels`,斷言三顆 key 現在**在**
  白名單裡(確認前後端都真的放開,不是只拆一邊)。
* `frontend/src/lib/themeBundle.test.ts` —
  `drops an override of the theme structural markers` 改成
  `keeps a themeMarkers override and still drops a theme's identity`:
  `themeMarkers.*` 的覆寫存活,`themeIdentity.office` 仍被丟棄並回報。

**紅證**:`round8-fix-run/redproof-3-thememarkers-overridable.txt` —
把 `themeMarkers` 放回排除清單重跑產生器 → 上述兩個案例都紅。

---

## 三、拆掉「自訂主題不得取用內建主題顯示名」

**改了什麼**

* `frontend/src/lib/themeBundle.ts` — 移除 `isBuiltinThemeName()`、
  `BUILTIN_THEME_NAME_SET`、`normalizeThemeName()`(ASCII case-fold,唯一用途就是
  這條名稱比對)與 `validateThemeBundle` 裡那道檢查。`trimThemeName()` 改為
  export(空白正規化仍然決定長度判決,是前後端同構的那一半)。
* `server/ocserverd/theme_bundle.go` — 同上,移除 `isBuiltinThemeName()`、
  `normalizeThemeName()` 與 `validateThemeBundles` 裡那道檢查。
* `frontend/scripts/gen-message-keys.mjs` — 只為這條規則而生的
  `THEME_IDENTITY_NAMES` / `themeIdentityNames` 兩份產出**孤兒資料**一併移除
  (`zh.ts` 也不再需要被載入)。

**保留不動**:`RESERVED_THEME_IDS` / `reservedThemeIDs`(**id** `office` 仍然保留)、
名稱長度上限、Unicode 類別拒收(Cc/Cf/Co/Cs/Zl/Zp)、Zs 正規化為 U+0020。

**drift**:`npm run gen:msgkeys` 重跑,兩份產出的差異就是「多三顆 themeMarkers、
少一份 identityNames」,別無其他。

**測試(改,不刪)**

* TS `themeBundle.test.ts`:`rejects a name that claims the built-in theme's
  display name` → `accepts a name that matches...`,五個拼法全部接受,並補一句
  **id 仍然保留**的斷言;Zs padding 那組從「被當保留名拒收」改成「接受,且 trim
  後不留非 ASCII 空白」;`describe("normalizeThemeName")` → `describe("trimThemeName")`,
  同一張表改成 pin 存活的那顆正規化器(不再折大小寫);
  `describe("isBuiltinThemeName")` 隨函式移除。
* Go `theme_bundle_test.go`:對稱地改同樣三處,`TestNormalizeThemeName` →
  `TestTrimThemeName`,`TestIsBuiltinThemeName` 隨函式與資料移除。
* `frontend/src/lib/themeName.parity.test.ts`:**61 組語料一顆不動**,只把判決期望
  從 REJECT-reserved 改成 ACCEPT(`builtin_zh` / `builtin_en` / `builtin_upper` /
  `builtin_pad_ascii` / `builtin_dotted_I` / `builtin_fullwidth` / `builtin_kelvin` /
  `nfd_office` / `spoof_builtin_marker_*` / 三組 Zs padding)。前後端同判的那道
  比對照跑。
* `frontend/src/lib/themeExport.test.ts`:內建主題下載仍命名為「辦公室(副本)」,
  但「裸名會被拒」改成「裸名也匯得回來」。

**紅證**

* `round8-fix-run/redproof-4a-reserved-name-removed-ts.txt` — 把保留名檢查加回 TS
  → 5 個案例紅(含前後端 parity 兩個)。
* `round8-fix-run/redproof-4b-reserved-name-removed-go.txt` — 加回 Go → 3 個
  子測試紅。

---

## 四、`themeIdentity` 仍然鎖住(唯一保留的保障)

`frontend/src/components/ThemeSettings.test.tsx` 的
`cannot be made to show two identical built-in rows...` 改寫成
`keeps the built-in row's own name when a pack forges everything else`:
主題包把 `MESSAGE_KEYS` 每一顆都改成哨兵、額外直攻 `themeIdentity.office`、
自己取名叫「辦公室」、並覆寫標題讀的色槽 —— 全部允許,清單上真的出現兩列「辦公室」。
唯一仍成立的是:**內建那一列還是叫「辦公室」**,哨兵沒碰到它。

測試裡另外釘了一句「哨兵確實已經生效」(`ts-group-builtin` 的文字**是**哨兵),
否則「內建列沒有哨兵」在一個根本沒套用 overlay 的畫面上會假性通過 —— 這一點在
撰寫時實測過,補上之後紅證才成立。

**紅證**:`round8-fix-run/redproof-5-themeidentity-still-locked.txt` —
把 `NON_OVERRIDABLE_SUBTREES` 清空 → 這個案例與
`does not let a theme bundle rename a theme` 一起紅。

---

## 五、對比度實測(真實瀏覽器,計算色 vs 實際背景像素)

量法:`docs/T-081b-evidence/shots/shoot.mjs` 的 `markers2` 群組 —— 沿用第七輪已修好的
解析器(`getComputedStyle` 對 `color-mix()` 會回 `color(srgb 0.62 0.63 0.66)` 這種
0–1 浮點,不是 `rgb()`;解析器兩種格式都吃)。背景不是讀 CSS,而是**截該區域的圖、
取出現最多的像素色**,所以 alpha 分區底色疊在背景圖上的結果一併算進去。

| 情境 | 改動前 | 改動後 | 判定 |
| --- | --- | --- | --- |
| 內建深色主題(辦公室) | 6.57:1(`#9fa0a7` on `#191c24`) | **6.49:1**(`#9aa0ad` on `#191c24`) | ✅ AA 合格 |
| 套用精靈村(淺色自訂包) | **1.98:1**(`#9fa1a7` on `#e4e3bb`) | **8.33:1**(`#403d2c` on `#e4e3bb`) | ✅ AA 合格 |

* 內建深色從 6.57 微降到 6.49:標題不再是 marker 混色的 `#9fa0a7`,而是主題本來的
  `#9aa0ad` —— 差幾階、仍遠高於 4.5。
* 精靈村從 1.98 → 8.33:該包自己的 `--color-text-muted` 是 `#403d2c`(深色),
  在自家底色 `#e4e3bb` 上非常清楚。**不需要打折說明,這一格是真的合格。**
* 兩組數字都用獨立的 WCAG 公式覆算過一次,與瀏覽器量到的值一致(6.49 / 8.33)。
* `marker slot users: []` —— 量測時掃過所有樣式表的每一條規則,確認沒有任何規則
  還在讀 `--color-marker-*`。

**截圖**:
`/Users/eva/Desktop/gofreight/OffiCraft-wt-T081b/docs/T-081b-evidence/shots/27-smurf-applied-theme-list-round8.png`
—— 與 `24-smurf-applied-theme-list.png` 同視窗(窄版 1440)、同流程、同位置,可直接
對照。24 那張的「內建 / 自訂」是灰得幾乎看不到;27 這張是深色、清楚。內建那一列
仍然寫著「辦公室」(themeIdentity 沒被主題包蓋掉)。

---

## 六、偏離定案

無。owner 收斂後的最終界線(只鎖 `themeIdentity`)照做,沒有為「防偽」新增任何
守衛或限制;群組分區與「內建排第一」原樣保留,也沒有加測試去強化它們。
唯一主動加碼的是第四節那句「哨兵確實生效」的斷言,理由是不加的話該測試會假性通過。

---

## 七、CI

```
[ci] commit ffb502d3feae5bbaeb61987f921ec6de96fe8fb3 (feat/T-081b-theme-token-split, tree DIRTY) — started 2026-07-27T15:37:56Z
...
[ci] all green
```

log:`docs/T-081b-evidence/round8-fix-run/ci.log`(第 3515 行 `[ci] all green`)。

* 已知不穩定的 `useRelocateMachine.test.tsx` 這一輪 **綠**(18 tests,第 1097 行),
  沒有重跑帶過。
* 真實瀏覽器的 CT 視覺守衛 `theme-settings-list.ct.spec.tsx` 在 390 / 1280 兩個寬度
  下,「內建」「自訂」兩個標題的 WCAG AA 檢查全部通過(第 132–141 號案例)——
  那一組量的是**內建主題**下我們自己出的顏色,不是主題包對自己做了什麼。
* 紅證 log 全部在 `docs/T-081b-evidence/round8-fix-run/`:
  `redproof-1-group-head-colour.txt`、`redproof-2-generator-drops-a-token.txt`、
  `redproof-3-thememarkers-overridable.txt`、
  `redproof-4a-reserved-name-removed-ts.txt`、`redproof-4b-reserved-name-removed-go.txt`、
  `redproof-5-themeidentity-still-locked.txt`。
