# T-081b round 7 — 偏好設定主題下拉:拿掉 optgroup 分區,固定「內建在前、自訂在後」

## 規格沿革(本輪 owner 改了兩次,以最後一次為準)

1. 原派工單:主題選擇器換成**自製下拉**(不再用原生 `<select>`),分區呈現內建/自訂。
2. 中途修正:自製下拉**不分區**,改成每個選項自帶「內建」/「自訂」小 label。
3. **最終(採用)**:owner 說「原本的下拉式選單我覺得就很好了」「就算真的沒有顯示內建或
   自訂也沒關係,只要設定有標示出來就好」⇒ **維持原生 `<select>`**,只做兩件事:
   - 移除 `<optgroup>` 分區,改平面清單;
   - 排序固定**內建在前、自訂在後**,並用測試釘住。
   下拉裡**不放任何內建/自訂標示**;設定 › 主題 的分組標示**維持現狀不動**。

第 1、2 版做到一半的自製下拉元件(`ThemeSelect.tsx` / `theme-select.css`)已**完全刪除**,
未進入任何 commit,也沒有新增任何 i18n key(所以沒有孤兒字串要清)。

## 改了哪些檔

| 檔案 | 改了什麼 |
| --- | --- |
| `frontend/src/components/ProfileDropdown.tsx` | 主題 `<select>` 的兩個 `<optgroup>` 拿掉,改成平面 `<option>` 清單;內建 `office` 那一顆**寫在 JSX 最前面**,`customThemes.map(...)` 接在後面 —— 順序由渲染決定,主題包任何欄位碰不到。註解換成說明「為什麼不能把內建/自訂拼回選項文字」。 |
| `frontend/src/components/ProfileDropdown.settings.test.tsx` | 原本斷言 `<optgroup>` 歸屬的兩條測試改成斷言**排序 + 選項文字純淨**(沒有直接刪);另加一條新測試釘住排序不受主題名稱/匯入順序影響。 |

`frontend/src/components/ThemeSettings.tsx` / `theme-settings.css` / `ThemeSettings.test.tsx`
**一行未動** —— 設定頁的 `role="group"` + `.ts-group-head`(顏色吃不可覆寫的
`--color-marker-*`)那套分組標示原樣保留,防偽的主陣地在那裡。

## 測試(全部實測過會紅)

檔案:`frontend/src/components/ProfileDropdown.settings.test.tsx`

| 測試名稱 | 釘住什麼 | 紅的證據 |
| --- | --- | --- |
| `keeps only the theme SELECTOR — no management affordances (moved to 設定/主題)` | 下拉裡沒有 `<optgroup>`;內建選項的文字**就是**主題身分名、不多不少 | `round7-fix-run/red-proof-optgroup-back.txt`(把 optgroup 加回去 → 紅)、`red-proof-marker-in-name.txt`(把「內建」拼進選項文字 → 紅) |
| `cannot be made to show two identical built-in rows by a theme's NAME` | 取名「辦公室(內建)」的包**不會**變成與內建列位元組相同的一列;內建列文字純淨;內建列是第 0 顆 | `red-proof-marker-in-name.txt`、`red-proof-order.txt` |
| `keeps the built-in first and the packs after, whatever they are named`(新增) | 兩個名字排序在前的自訂包(「AAA 最前面」「000 更前面」)也擠不到內建前面;option value 順序 = `["office","aaa","zzz"]` | `red-proof-order.txt`(把 `customThemes.map` 移到內建之前 → 紅) |

還原後三條全綠(`ProfileDropdown.settings.test.tsx` 10 passed)。

## 截圖

- `docs/T-081b-evidence/shots/23-preferences-panel-flat-select.png` — 偏好設定面板正常
  (收合)狀態,窄版 1440,內建深色主題,清單裡同時有內建「辦公室」與自訂「精靈村」。
  展開態不補拍(原生下拉的彈出層由作業系統畫,已有第 22 張)。

## CI

`bash bin/ci.sh` → `[ci] all green`(exit 0)。log:`docs/T-081b-evidence/round7-fix-run/ci.log`。
frontend vitest 166 檔 / 1323 測試全過,已知不穩定的 `useRelocateMachine.test.tsx` 這次**綠**(18 tests, 195ms)。
