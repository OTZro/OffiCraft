# T-081b round 6 — 拿掉每列的「內建/自訂」標籤

owner ruling：分區保留，標籤拿掉。群組標題已經講了同一件事，每列再掛一顆同義標籤是重複。

## 改了哪些檔

| 檔案 | 用途 |
|---|---|
| `frontend/src/components/ThemeSettings.tsx` | 刪掉兩處 `<span className="ts-tag">` / `ts-tag--custom`；分區、群組標題、`aria-labelledby` 全部不動。註解改成「內建/自訂 只由 GROUP STRUCTURE 承擔」 |
| `frontend/src/components/theme-settings.css` | 刪掉 `.ts-tag` / `.ts-tag--custom` 兩個規則與其 WCAG 註解；`.ts-group-head` 保留，把 WCAG AA(11px = 一般字級)的理由搬到它頭上——它現在是螢幕上唯一的標記 |
| `frontend/src/styles/theme.css` | 刪掉 `--color-marker-builtin` / `--color-marker-custom`(只有 chip 底色在用)。`--color-marker-surface` / `--color-marker-fg` 保留（群組標題字色仍在用），註解改寫 |
| `frontend/scripts/gen-theme-tokens.mjs` | `NON_OVERRIDABLE_TOKENS` 縮成兩顆；前綴守衛(prefix ⇄ 清單雙向不一致就 exit 1)維持，錯誤訊息由「chip 顏色」改為「標題顏色」 |
| `frontend/scripts/gen-theme-tokens.test.ts` | 期望清單改兩顆；「listed slot 從 theme.css 消失」用例改用 `--color-marker-surface` |
| `frontend/src/components/ThemeSettings.test.tsx` | 兩條斷言 chip 的測試改成斷言**群組歸屬**（詳下）；白名單用例改用 `--color-marker-fg` |
| `frontend/visual-guards/theme-settings-list.ct.spec.tsx` | 兩條 chip 的 WCAG AA 對比測試改成量**群組標題**的對比；「用詞 badge 不存在」改成「任何 `.ts-tag` 都不存在」 |
| `frontend/visual-guards/theme-settings-add.ct.spec.tsx` | 新增流程改斷言新主題落在 `自訂` 群組，且畫面上 `.ts-tag` 為 0 |

`npm run gen:tokens` 重跑：`themeTokens.generated.ts` / `theme_colornames_gen.go` **零 diff**（marker 家族本來就不在白名單內），無 drift。

## i18n

`themeMarkers.builtinGroup` / `customGroup` **仍在用**（設定頁群組標題 + ProfileDropdown 的 `<optgroup>`），不可刪。
`settings.themeBuiltinTag` / `themeCustomTag` 在第 4 輪就已刪除，全 repo 無殘留 key。**本輪沒有字串變成孤兒。**

## 測試（實測會紅 → 還原轉綠）

### 1. `ThemeSettings · import > cannot be made to show two identical built-in rows by a theme's wording, colours or name`
繼續存在。原本斷言 `.ts-tag` 的文字，現在斷言：
* 畫面上 `.ts-tag` 數量為 0（標記只剩結構）
* 兩個群組標題文字未被 wording 覆寫
* 偽造列 `forged.closest(".ts-list")` 的標題是「自訂」
* `#ts-group-builtin` 那一群**只有一列**，內容是辦公室，且不含偽造列

**紅證 1（自訂主題被放進內建群組）**：把兩個 `.ts-list` 的群組標題身分對調（自訂那群掛 `ts-group-builtin`／「內建」），等同「自訂主題出現在內建區」。
log：`round6-fix-run/redproof-1-custom-in-builtin-group.txt`
```
× imports a pasted bundle, lists it, and lands it on the server        → expected '自訂' to be '內建'   (:84)
× puts the built-in and the custom rows in separate labelled groups     → expected '自訂' to be '內建'   (:115)
× cannot be made to show two identical built-in rows ...                → expected '內建' to be '自訂'   (:175)
Tests  3 failed | 15 passed (18)
```
還原後 `18 passed`。

**紅證 2（chip 復活）**：把內建列的 `<span className="ts-tag">` 加回去。
log：`round6-fix-run/redproof-2-chip-reinstated.txt` → `2 failed | 16 passed`，兩條都是 `expected 1 to be +0`（chip 數量斷言）。還原後全綠。

### 2. `ThemeSettings · import > imports a pasted bundle, lists it, and lands it on the server`
原本斷言「列的文字含 內建/自訂」，改為斷言「列所屬群組的標題」＋「畫面上 0 顆 chip」。紅證同上。

### 3. 產生器守衛
**紅證 3**：把 `--color-marker-builtin` 加回 `theme.css` 而不登記 → `npm run gen:tokens` exit 1，訊息指名該 token 與 `NON_OVERRIDABLE_TOKENS`。
log：`round6-fix-run/redproof-3-generator-guard.txt`。還原後 exit 0。

## 第 1 點：現在還能不能偽造？（實測）

自己寫了一支拋棄式 probe（跑完已刪，輸出留在 `round6-fix-run/forgery-probe.txt`），從主題包這一側能碰的每個面向各打一發：

| 攻擊 | 結果 |
|---|---|
| `id: "office"` | `validateThemeBundle` 直接擋：`id "office" is reserved for a built-in theme` |
| `name: "辦公室 "`（含空白，大小寫/trim 折疊） | 擋：`name "辦公室" is reserved for a built-in theme` |
| `name: "辦公室(內建)"` + `wording.zh["themeMarkers.customGroup"] = "內建"` | 匯入成功，但落在**自訂**群組；wording 對 `themeMarkers` 子樹的覆寫被 drop，標題仍是「自訂」 |
| `name: "內建"` | 匯入成功，落在**自訂**群組 |
| `colors: { --color-text-muted / --color-bg 塗成同色 }` | 標題不吃這兩顆，仍可見；列仍在**自訂**群組 |

實測輸出：
```
GROUP "內建" id= ts-group-builtin ROWS= ["辦公室"]
GROUP "自訂" id= ts-group-custom ROWS= ["辦公室(內建)","內建","x"]
chips on screen: 0
```

**結論：沒有。** 一列屬於哪一群完全由渲染程式決定——內建那個 `.ts-list` 裡是寫死的辦公室單列，`customThemes` 只會被 map 進自訂那個 `.ts-list`，主題包的 JSON 沒有任何欄位能影響這件事。標題文字來自不可覆寫的 `themeMarkers` 子樹（產生器整棵跳過，wording 覆寫會被清掉），標題顏色來自不在白名單內的 `--color-marker-fg/surface`。拿掉 chip **沒有**弱化防偽：chip 是三個訊號都可偽造的那個弱環節，移除後只剩結構這一個不可偽造的來源。

## 偏離範圍

無。只動了「拿掉 chip」直接牽連到的檔案（元件、CSS、色槽、產生器、對應測試）。已知既有不穩定測試 `useRelocateMachine.test.tsx > drops the 已送出 chip by itself` 未觸碰。

---

# 追加 1 — 頂列輸入框在淺色主題下的邊界

## 實測(先量,不推論)

探針：`round6-fix-run/probe-boundaries.mjs`(起真站 vite dev :5199 + Playwright，
量計算色；淺色主題用**既有的**精靈村主題包 `shots-pack/smurf-village.theme.json`)。

頂列裡唯一的輸入框是 `.inline-edit__input`(組織名 InlineEdit)。量到：

| 主題 | 分區底(topbar) | 輸入框填色 | 填色 vs 分區 | 邊框計算色 | 邊框 vs 分區 | 邊框 vs 填色 |
|---|---|---|---|---|---|---|
| 內建(深) | `#191c24` | `#191c24` | **1.00** | `#6fd6b0` 1px solid | 9.67 | 9.67 |
| 精靈村(淺) | `#b6ca88` | `#c2d492` | **1.11** | `#2b450b` 1px solid | **6.04** | **6.70** |

票面的 1.11:1 復現了。但它量的是**填色對填色**——`.inline-edit__input` 現況
**本來就有** `border: 1px solid var(--color-accent)`，而那道邊框在淺色主題下是 6.04:1、
在內建深色下是 9.67:1，遠高於非文字元素的 3:1 門檻。

依定案修法「**若現況本來就有邊框就沿用**」：**CSS 不動**(內建深色外觀零變化)，
本輪交付的是**把這道邊界釘住的測試**——否則哪天有人把 `border` 拿掉，剩下的就是
1.11:1 的隱形方框，而現在沒有任何東西會紅。

## 新增測試

`frontend/visual-guards/theme-contrast.ct.spec.tsx`
+ `frontend/visual-guards/stories/ThemeContrastStory.tsx`

* `built-in dark / light pack: the topbar input is bounded against the bar it sits on`
  真瀏覽器量計算色：邊框 style ≠ none、寬度 > 0，且邊框對「分區底」與對「輸入框填色」
  **雙向都 ≥ 3:1**。淺色主題就是把精靈村那份調色盤照產品的方式(在 documentElement 上
  設 `--color-*`)套上去。

**紅證 4**：把 `.inline-edit__input` 的 `border: 1px solid var(--color-accent)` 改成
`border: none`。log：`round6-fix-run/redproof-4-topbar-input-border-removed.txt`
→ 兩條(深/淺)都紅，login hint 兩條仍綠。還原後 4 passed。

---

# 追加 2 — 登入頁提示文字 `.login__hint`

## 改法

`frontend/src/components/login.css`：
`color: color-mix(in srgb, var(--color-text) 55%, transparent)` → `color: var(--color-text-muted)`。

寫死的 55% 正是問題本身：它把提示色**從正文色推導出來**，於是淺色主題的深墨水被往卡片
洗掉 45%，怎麼設都上不去(即使正文壓成純黑，精靈村卡片 `#fdfbf1` 上也只有 4.71)。
`--color-text-muted` 是產品既有的**次要文字色槽**，由每個主題自己宣告。

## 實測(同一支探針)

| 主題 | 卡片底 | 改前計算色 / 對比 | 改後計算色 / 對比 |
|---|---|---|---|
| 內建(深) | `#242832` | `#8f9299` / **4.72** | `#9aa0ad` / **5.62** ✅ 不低於現況 |
| 精靈村(淺) | `#fdfbf1` | `#8e8b7e` / **3.29** ❌ | `#403d2c` / **10.53** ✅ ≥4.5 |

改後數字：`round6-fix-run/probe-after.txt`。

## 新增測試

同一支 spec：
* `built-in dark / light pack: the login hint clears WCAG AA (≥4.5:1)`
  兩個主題都要 ≥4.5；內建深色另外釘「**不得低於出貨時的 4.72**」。

**紅證 5**：把 `.login__hint` 改回寫死的 `color-mix(--color-text 55%, transparent)`。
log：`round6-fix-run/redproof-5-login-hint-hardcoded-mix.txt`
```
✘ built-in dark: the login hint clears WCAG AA   Received: 4.718525932900097   (< 4.72 迴歸門檻)
✘ light pack:    the login hint clears WCAG AA   Received: 3.2863876762741864  (< 4.5)
2 failed | 2 passed（topbar 兩條仍綠）
```
還原後 4 passed。

## 內建深色主題的外觀變化盤點

以計算色佐證，本輪只有兩處變：
1. 設定頁主題清單每列的 `內建/自訂` chip 消失(本輪的主體，owner 指定)；
2. `.login__hint` 由 `#8f9299` → `#9aa0ad`(追加 2 的修法本身，票面明訂只要求「對比不低於現況」)。

其餘皆零變化：`.inline-edit__input` 一個字元都沒動；被刪的
`--color-marker-builtin` / `--color-marker-custom` 全 repo 只有已刪的 `.ts-tag` 在用；
`.ts-group-head` 的算式與兩顆來源色槽原封不動(CT 守衛量到的對比未變)。

---

## 型別檢查回報(coordinator 指出的 `theme-settings-list.ct.spec.tsx` 型別錯誤)

那 3 個錯誤(`mountSeeded(mount: any, page: any)`、`evaluateAll` 回呼的隱含型別)
**是既有的**，不是本輪新加——`git show HEAD:frontend/visual-guards/theme-settings-list.ct.spec.tsx`
同樣三行原封不動。

原因：**`visual-guards/` 目前不在任何 tsconfig 的 `include` 裡**
(`tsconfig.json` = `["src"]`、`tsconfig.scripts.json` = `["scripts/**/*.ts"]`)，
所以 `npm run typecheck` 看不到它——編輯器紅、CI 綠，正是第四輪 SHOULD-D 對 `scripts/` 修掉的同一個洞。

**該不該納入：該。** 但不是這一票能順手做的：實測把 `visual-guards/**` 丟給 tsc 會噴 **24 個錯**，
其中一半來自缺 `vite/client` 型別(`import.meta.env`、`?raw` 模組)，另一半散在
`software-update-status.ct.spec.tsx` 等既有 spec。需要新開一份
`tsconfig.visual-guards.json`(比照 `tsconfig.scripts.json`)並逐檔修，屬獨立工項。
**本輪新增的 `theme-contrast.ct.spec.tsx` / `ThemeContrastStory.tsx` 已自帶完整型別標註，
不引入新的型別漏洞。**

## CI

`bash bin/ci.sh` → **[ci] all green**（exit 0）。log：`docs/T-081b-evidence/round6-fix-run/ci.log`
