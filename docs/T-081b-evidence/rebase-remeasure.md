# T-081b — 換基底之後的重量(對比度實算 + 對照截圖)

**新主線基底:`12b84d1`　分支 HEAD:`c32400b`　分支:`feat/T-081b-theme-token-split`**

換基底之後,舊基底上量到的所有「比對型」數字一律作廢。完整 CI(視覺守衛、五道產生器
drift gate、跨語言名稱比對)由 owner 自行跑過、全綠;**本檔只重做 CI 蓋不到的那一塊:
對比度實算與對照截圖。**

跑法(同一支腳本、同一組能吃 `rgb()` 與 `color(srgb …)` 兩種格式的解析器):

```
cd frontend && npm run dev -- --port 5199
cd docs/T-081b-evidence/shots && node shoot.mjs remeasure
```

主題包:`docs/T-081b-evidence/shots-pack/smurf-village.theme.json`(匯入零警告,
腳本會在有警告時直接 throw)。原始輸出逐筆附在 `shots/measurements.jsonl`。

---

## 總表:舊基底 → 新基底

| # | 量測項 | 舊基底 | 新基底 `12b84d1` | 有無變化 |
| --- | --- | --- | --- | --- |
| 1 | `.ts-group-head` · 內建深色(辦公室) | 6.49 | **6.49** | 無 |
| 1 | `.ts-group-head` · 套用精靈村 | 8.33 | **8.33** | 無 |
| 2 | 未讀徽章 · 白字 vs 徽章底 | 4.52 | **4.52** | 無 |
| 2 | 未讀徽章 · 徽章底 vs 頁底 | 3.76 | **3.76** | 無 |
| 2 | 未讀徽章 · 徽章底 vs active 分頁 | 2.74 | **2.74** | 無(已知取捨) |
| 3 | `.login__hint` · 內建深色 | 5.62 | **5.62** | 無 |
| 3 | `.login__hint` · 精靈村(淺) | 10.53 | **10.53** | 無 |
| 4 | 手機 375 橫向捲動 | 無 | **無** | 無 |

**七項數字全部與舊基底一字不差,沒有任何一項變差,也沒有任何一項不合格。**
(唯一低於門檻的 2.74 是舊基底就記錄在案的已知取捨,見下方第 2 項。)

---

## 1. `.ts-group-head`(設定 › 主題 的「內建」「自訂」群組標題)

量的是**計算色 vs 該塊區域實際畫出來的背景像素眾數**(不是只算 CSS,alpha 分區底疊在
主題背景圖上的結果也一起算進去)。

| 情境 | 標題 | 前景(計算色) | 背景像素 | 舊基底 | 新基底 | AA(4.5) |
| --- | --- | --- | --- | --- | --- | --- |
| 內建深色(辦公室)· 窄版 1440 | 內建 / 自訂 | `#9aa0ad` | `#191c24`(佔比 0.99) | 6.49 | **6.49** | ✅ |
| 精靈村(自訂,淺色)· 窄版 1440 | 內建 / 自訂 | `#403d2c` | `#e4e3bb` | 8.33 | **8.33** | ✅ |

* 字級 11px / 字重 600,依 WCAG 屬一般字級,門檻取 4.5:1。兩組都遠高於門檻。
* 精靈村底下背景的 CSS 層次仍是 `main.app__main = rgba(241, 234, 209, 0.8)` 疊在
  `body = rgb(194, 212, 146)` 上,合成後 `#e4e3bb` —— 與舊基底相同。
* 顏色來源仍是可覆寫的 `--color-text-muted`(第八輪 owner 拍板的做法),換基底沒有動到它。

## 2. 未讀徽章(`.nav-tab__badge` / `.office__tab-badge` / `.member-card__unread`)

三個選擇器共用同一組配方(`background: var(--color-danger-badge)`、
`color: var(--color-on-danger)`、`outline: 1px solid var(--color-bg)`)。

**內建深色主題**:徽章底 `#ba5953`、字 `#ffffff`、外框 `#191c24`(1px)

| 對比對象 | 舊基底 | 新基底 | 判定 |
| --- | --- | --- | --- |
| 白字 vs 徽章底 | 4.52 | **4.52** | ✅ 過 AA 4.5 |
| 徽章底 vs 頁底 `--color-bg` `#191c24` | 3.76 | **3.76** | ✅ ≥3 |
| 徽章底 vs 卡片 `--color-card` `#242832` | 3.26 | **3.26** | ✅ ≥3 |
| 徽章底 vs active 分頁 `--color-indigo` `#2c3350` | 2.74 | **2.74** | ❌ **低於 3:1** |

**2.74 這一項在新基底上仍然不合格,和舊基底一樣。** 這是舊基底就明載的已知取捨
(`T-081b-final-delivery.md` 第 2 點、`chrome.css` 的註記):整個色域窮舉過,不存在
任何單一顏色能同時滿足白字 ≥4.5 與三種底色各 ≥3,因此改以 1px 頁底色外框把徽章從底色上
分離 —— 實際相鄰的是那圈 `--color-bg` 外框,而不是分頁底色。**換基底沒有改善它,也沒有
惡化它;它就是原樣留著。**

順帶量的精靈村(淺色包)一組(舊基底無對應數字,列為新增參考):
白字 vs 徽章底 `#a8342b` = 6.58;對頁底 4.11 / 對卡片 6.34 / 對 active 分頁 5.04 —— 全過。

## 3. 登入頁 `.login__hint`

依 `FirstRunPage` 的結構(`.login > .login__card > .login__hint`)在真實執行期頁面注入探針,
量計算色對卡片底(半透明的話先與底層合成)。

| 主題 | 卡片底 | 計算色 | 舊基底 | 新基底 | AA(4.5) |
| --- | --- | --- | --- | --- | --- |
| 內建(深) | `#242832` | `#9aa0ad` | 5.62 | **5.62** | ✅ |
| 精靈村(淺) | `#fdfbf1` | `#403d2c` | 10.53 | **10.53** | ✅ |

`.login__hint` 的顏色仍是第六輪改成的 `var(--color-text-muted)`(不是寫死的
`color-mix(--color-text 55%)`),換基底沒有把它退回去。內建深色也仍高於出貨時的 4.72 迴歸門檻。

## 4. 手機 375 橫向捲動

套用精靈村、窄版、viewport 375×812、停在監控頁:

```
viewport 375 / column 375 / outerPerSide 0 / scrollWidth 375 / clientWidth 375 / hScroll false
```

**無橫向捲動。** 內容欄吃滿 375,外區 0(窄版在手機寬度下不留外區,與舊基底一致)。

---

## 截圖(編號接續 28、29)

**28** — `/Users/eva/Desktop/gofreight/OffiCraft-wt-T081b/docs/T-081b-evidence/shots/28-smurf-applied-theme-list-rebase.png`
設定 › 主題 清單,已套用精靈村,窄版 1440,與第 27 張同視窗、同流程、同位置。畫面上兩個
群組標題(內建 / 自訂)在奶油色底上是深墨綠、清楚可讀,內建那一列仍寫著「辦公室」。
**這張與 `27-smurf-applied-theme-list-round8.png` 的 SHA-256 完全相同
(`8fa11d21…f32`)—— 換基底後這一頁連一個像素都沒有變。**

**29** — `/Users/eva/Desktop/gofreight/OffiCraft-wt-T081b/docs/T-081b-evidence/shots/29-smurf-applied-preferences-panel-rebase.png`
偏好設定面板(收合態:主題 / 語言 / 版面),已套用精靈村,窄版 1440,背後停在監控頁。
主題下拉是平面清單(`optgroupCount: 0`)、內建「辦公室」排在第 0 顆、目前值是「精靈村」,
面板本身跟著主題包換了整套淺色。
(與舊的第 25 張不是同一狀態:25 拍在第七輪拿掉 `<optgroup>` 之前,像素本來就不會一樣。)

---

## 環境註記

* 量測期間機器上沒有任何測試在跑 —— 只有 vite dev server(port 5199)與編輯器的
  language server(tsserver / typescript-language-server / pyright-langserver)。
  `useRelocateMachine.test.tsx` 之類的前端測試**本輪完全沒有觸發**,無紅可報。
* 本輪未改任何產品程式碼、未 commit、未 push。改動只有兩處證據檔:
  `shots/shoot.mjs` 新增 `remeasure` 群組(並把原本鎖在 `markers` 區塊內的色彩解析器與
  `measureHeads` 提到模組頂層共用),以及 `shots/README.md`、`shots/measurements.jsonl`
  的追加。
